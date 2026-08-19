package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	researchPreflightProbeMaxReleases = 8
	researchPreflightProbeMaxUnits    = 64
	researchPreflightProbeMaxResults  = 8
	researchPreflightWorkerFreshness  = 5 * time.Minute
)

type ResearchPreflightService struct {
	Knowledge   *BookKnowledgeStore
	Research    *ResearchStore
	SourceSync  *SourceSyncStore
	QuickBudget ResearchBudget
	DeepBudget  ResearchBudget
	Now         func() time.Time
}

type researchPreflightCandidateDetail struct {
	coverage   ResearchPreflightCoverage
	budget     ResearchPreflightBudget
	budgetFits bool
}

type researchPreflightServiceSignals struct {
	packageInvalid    int
	evaluationInvalid int
	scopeRejected     int
	coverageInvalid   int
}

func (s *ResearchPreflightService) Evaluate(
	ctx context.Context,
	ownerHash string,
	request ResearchPreflightRequest,
) (*ResearchPreflight, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalized, err := NormalizeResearchPreflightRequest(request)
	if err != nil {
		return nil, err
	}
	if s == nil || s.Knowledge == nil {
		return nil, fmt.Errorf("%w: knowledge store is required", ErrResearchPreflightUnavailable)
	}
	if s.Research == nil {
		return nil, fmt.Errorf("%w: research store is required", ErrResearchPreflightUnavailable)
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	resolvedMode, _, err := RouteResearchMode(ResearchRunRequest{
		Mode: normalized.Mode, Question: normalized.Question,
		PackageID: "preflight", PackageVersion: "1",
		RequestedSources: normalized.RequestedSources,
	})
	if err != nil {
		return nil, err
	}

	workerObserved, workerRankState, err := s.workerReadiness(normalized, now)
	if err != nil {
		return nil, err
	}
	facts, details, signals, err := s.packageFacts(ctx, normalized, resolvedMode, workerRankState)
	if err != nil {
		return nil, err
	}
	researchPreflightMarkLatestPublished(facts)
	candidates, gaps := RankResearchPreflightCandidates(normalized, facts)
	for index := range candidates {
		identity, ok := researchPreflightIdentity(AgentPackage{
			PackageID: candidates[index].PackageID, Version: candidates[index].PackageVersion,
			ContentHash: candidates[index].ContentHash,
		})
		if !ok {
			continue
		}
		detail := details[identity]
		candidates[index].Coverage = detail.coverage
		candidates[index].Budget = detail.budget
	}
	gaps = researchPreflightServiceGaps(gaps)
	status := ResearchPreflightStatusBlocked
	for _, candidate := range candidates {
		if candidate.Readiness == ResearchPreflightCheckPass || candidate.Readiness == ResearchPreflightCheckWarning {
			status = ResearchPreflightStatusReady
			break
		}
	}
	checks := researchPreflightServiceChecks(
		status, candidates, details, signals, normalized.RequestedSources, workerObserved, resolvedMode,
	)
	result := ResearchPreflight{
		Status: status, Candidates: candidates, Checks: checks, Gaps: gaps,
		ParentRunID: normalized.ParentRunID,
	}
	return s.Research.SaveResearchPreflight(
		ownerHash, normalized, result, researchPreflightStoreTTL,
	)
}

func (s *ResearchPreflightService) packageFacts(
	ctx context.Context,
	request ResearchPreflightRequest,
	resolvedMode string,
	workerRankState string,
) ([]ResearchPreflightPackageFacts, map[researchPreflightPackageIdentity]researchPreflightCandidateDetail, researchPreflightServiceSignals, error) {
	facts := make([]ResearchPreflightPackageFacts, 0)
	details := make(map[researchPreflightPackageIdentity]researchPreflightCandidateDetail)
	signals := researchPreflightServiceSignals{}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, signals, err
		}
		records, err := s.Knowledge.ListAgentPackages(after, 200)
		if err != nil {
			if researchPreflightDependencyRetryable(err) {
				return nil, nil, signals, fmt.Errorf("%w: package store unavailable", ErrResearchPreflightUnavailable)
			}
			signals.packageInvalid++
			break
		}
		for _, record := range records {
			if record.LifecycleState != AgentPackagePublished ||
				(request.PackageConstraint != "" && record.PackageID != request.PackageConstraint) {
				continue
			}
			pkg, loadErr := s.Knowledge.LoadAgentPackageContext(ctx, record.PackageID, record.Version)
			if loadErr != nil {
				if researchPreflightDependencyRetryable(loadErr) {
					return nil, nil, signals, fmt.Errorf("%w: package artifact unavailable", ErrResearchPreflightUnavailable)
				}
				signals.packageInvalid++
				continue
			}
			if _, _, ok := parseResearchPreflightTime(pkg.PublishedAt); !ok {
				signals.packageInvalid++
				continue
			}
			if evaluationErr := ValidateAgentPackageEvaluationGate(s.Knowledge, *pkg); evaluationErr != nil {
				if researchPreflightDependencyRetryable(evaluationErr) {
					return nil, nil, signals, fmt.Errorf("%w: evaluation store unavailable", ErrResearchPreflightUnavailable)
				}
				signals.evaluationInvalid++
				continue
			}
			// LoadAgentPackageContext has already validated the exact published
			// artifact, content hash, runtime descriptor, releases, and package
			// contract. Keep the remaining runnable checks local so a second
			// Store read cannot erase a retryable I/O cause.
			if pkg.LifecycleState != AgentPackagePublished ||
				!agentPackageHasCapability(*pkg, "search") ||
				validateResearchAgentPackageScope(*pkg, resolvedMode, request.RequestedSources) != nil {
				signals.scopeRejected++
				continue
			}
			runnable := pkg
			probe, searchErr := researchPreflightProbeEvidence(
				ctx, s.Knowledge, *runnable, request.Question,
			)
			if searchErr != nil {
				if researchPreflightDependencyRetryable(searchErr) {
					return nil, nil, signals, fmt.Errorf("%w: knowledge search unavailable", ErrResearchPreflightUnavailable)
				}
				signals.coverageInvalid++
				continue
			}
			coverage := researchPreflightCoverage(probe)
			probe = nil
			serverBudget := s.QuickBudget
			if resolvedMode == ResearchModeDeep {
				serverBudget = s.DeepBudget
			}
			boundedBudget := boundResearchBudgetByPolicy(serverBudget, *runnable.ResearchPolicy)
			budget := ResearchPreflightBudget{ResolvedMode: resolvedMode, Limits: boundedBudget}
			budgetFits := researchPreflightBudgetFits(budget)
			identity, ok := researchPreflightIdentity(*runnable)
			if !ok {
				signals.packageInvalid++
				continue
			}
			details[identity] = researchPreflightCandidateDetail{
				coverage: coverage, budget: budget, budgetFits: budgetFits,
			}
			facts = append(facts, ResearchPreflightPackageFacts{
				Package: *runnable, TopicHits: researchPreflightTopicHits(request.Question, *runnable),
				EvidenceHits: coverage.EvidenceCount, LatestPublishedAt: runnable.PublishedAt,
				RunnablePackageValidated: true, EvaluationPassed: true,
				WorkerState: workerRankState, BudgetFits: budgetFits,
			})
		}
		if len(records) < 200 {
			break
		}
		next := agentPackageReference(records[len(records)-1].PackageID, records[len(records)-1].Version)
		if next == after {
			return nil, nil, signals, fmt.Errorf("%w: package pagination did not advance", ErrResearchPreflightUnavailable)
		}
		after = next
	}
	return facts, details, signals, nil
}

// researchPreflightProbeEvidence performs a local lexical-only probe. V4
// Research Packages pin releases whose searchable units are claims; both the
// number of releases and claims inspected are bounded before invoking the
// existing local text matcher. Package vector/hybrid settings are deliberately
// ignored here so preflight never calls an embedding or generation model.
func researchPreflightProbeEvidence(
	ctx context.Context,
	store *BookKnowledgeStore,
	pkg AgentPackage,
	query string,
) ([]AgentPackageEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	refs := append([]AgentPackageReleaseRef(nil), pkg.Releases...)
	sort.Slice(refs, func(left, right int) bool { return refs[left].ReleaseID < refs[right].ReleaseID })
	if len(refs) > researchPreflightProbeMaxReleases {
		refs = refs[:researchPreflightProbeMaxReleases]
	}
	policy := pkg.RetrievalPolicy
	policy.Strategy = "lexical"
	policy.EmbeddingProvider = ""
	policy.EmbeddingModel = ""
	policy.EmbeddingVersion = ""
	policy.EmbeddingEndpointHash = ""
	policy.RerankerVersion = ""
	results := make([]AgentPackageEvidence, 0, researchPreflightProbeMaxResults)
	scannedUnits := 0
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if scannedUnits >= researchPreflightProbeMaxUnits {
			break
		}
		release, err := store.LoadKnowledgeRelease(ref.ReleaseID)
		if err != nil {
			return nil, fmt.Errorf("load pinned release %q: %w", ref.ReleaseID, err)
		}
		if agentTraceReleaseContentHash(release.ContentHash) != agentTraceReleaseContentHash(ref.ContentHash) {
			return nil, fmt.Errorf("pinned release %q content hash changed", ref.ReleaseID)
		}
		if release.Analysis == nil {
			continue
		}
		remaining := researchPreflightProbeMaxUnits - scannedUnits
		unitCount := len(release.Analysis.Claims)
		if unitCount > remaining {
			unitCount = remaining
		}
		if unitCount == 0 {
			continue
		}
		scannedUnits += unitCount
		boundedRelease := *release
		boundedAnalysis := *release.Analysis
		boundedAnalysis.Claims = append([]BookAnalysisClaim(nil), release.Analysis.Claims[:unitCount]...)
		boundedRelease.Analysis = &boundedAnalysis
		matches, err := searchAgentReleaseClaimsWithStrategyContext(
			ctx, store, boundedRelease, query, researchPreflightProbeMaxResults, policy,
		)
		if err != nil {
			return nil, err
		}
		allowedCitations := stringBoolSet(ref.CitationIDs...)
		for _, match := range matches {
			citationIDs := make([]string, 0, len(match.CitationIDs))
			for _, citationID := range match.CitationIDs {
				if allowedCitations[citationID] {
					citationIDs = append(citationIDs, citationID)
				}
			}
			if pkg.RetrievalPolicy.RequireCitations && len(citationIDs) == 0 {
				continue
			}
			results = append(results, AgentPackageEvidence{
				ReleaseID: ref.ReleaseID, ClaimID: match.ClaimID,
				Statement: match.Statement, CitationIDs: citationIDs, Score: match.Score,
			})
		}
		sort.SliceStable(results, func(left, right int) bool {
			if results[left].Score != results[right].Score {
				return results[left].Score > results[right].Score
			}
			if results[left].ReleaseID != results[right].ReleaseID {
				return results[left].ReleaseID < results[right].ReleaseID
			}
			return results[left].ClaimID < results[right].ClaimID
		})
		if len(results) > researchPreflightProbeMaxResults {
			results = results[:researchPreflightProbeMaxResults]
		}
	}
	return results, nil
}

func (s *ResearchPreflightService) workerReadiness(
	request ResearchPreflightRequest,
	now time.Time,
) (string, string, error) {
	if !containsResearchString(request.RequestedSources, ResearchSourceChatlog) {
		return ResearchPreflightWorkerNotRequired, SourceAgentObservedOnline, nil
	}
	if s.SourceSync == nil {
		return SourceAgentObservedOffline, SourceAgentObservedOffline, nil
	}
	agent, err := s.SourceSync.GetSourceAgent("chatlog-agent")
	if errors.Is(err, ErrSourceAgentNotFound) {
		return SourceAgentObservedOffline, SourceAgentObservedOffline, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("%w: Worker state unavailable", ErrResearchPreflightUnavailable)
	}
	observed := DeriveSourceAgentObservedState(
		agent, now, researchPreflightWorkerFreshness, false,
	)
	if observed != SourceAgentObservedOnline {
		return observed, SourceAgentObservedOffline, nil
	}
	health, hasHealth := agent.CapabilityHealth["chatlog_read"]
	if agent.WorkerType != "chatlog-worker" ||
		!containsResearchString(agent.Capabilities, "chatlog_read") ||
		!hasHealth || !health.Healthy {
		return SourceAgentObservedDegraded, SourceAgentObservedOffline, nil
	}
	return SourceAgentObservedOnline, SourceAgentObservedOnline, nil
}

func researchPreflightCoverage(results []AgentPackageEvidence) ResearchPreflightCoverage {
	releases := make(map[string]bool)
	citations := make(map[string]bool)
	for _, evidence := range results {
		if len(releases) < researchPreflightCoverageMax {
			releases[strings.TrimSpace(evidence.ReleaseID)] = true
		}
		for _, citationID := range evidence.CitationIDs {
			if len(citations) >= researchPreflightCoverageMax {
				break
			}
			citations[strings.TrimSpace(citationID)] = true
		}
	}
	delete(releases, "")
	delete(citations, "")
	releaseIDs := make([]string, 0, len(releases))
	for releaseID := range releases {
		releaseIDs = append(releaseIDs, releaseID)
	}
	sort.Strings(releaseIDs)
	return ResearchPreflightCoverage{
		EvidenceCount: len(results), ReleaseCount: len(releaseIDs),
		CitationCount: len(citations), ReleaseIDs: releaseIDs,
	}
}

func researchPreflightTopicHits(question string, pkg AgentPackage) int {
	haystack := strings.ToLower(strings.Join(append(
		[]string{pkg.PackageID}, pkg.RetrievalPolicy.AllowedSourceTypes...,
	), " "))
	hits := 0
	for _, term := range splitSearchTerms(question) {
		if strings.Contains(haystack, term) {
			hits++
		}
	}
	return hits
}

func researchPreflightMarkLatestPublished(facts []ResearchPreflightPackageFacts) {
	latest := make(map[string]time.Time)
	for _, fact := range facts {
		_, publishedAt, ok := parseResearchPreflightTime(fact.Package.PublishedAt)
		if !ok {
			continue
		}
		family := strings.ToLower(strings.TrimSpace(fact.Package.PackageID))
		if current, found := latest[family]; !found || publishedAt.After(current) {
			latest[family] = publishedAt
		}
	}
	for index := range facts {
		_, publishedAt, ok := parseResearchPreflightTime(facts[index].Package.PublishedAt)
		family := strings.ToLower(strings.TrimSpace(facts[index].Package.PackageID))
		facts[index].FreshRelease = ok && publishedAt.Equal(latest[family])
	}
}

func researchPreflightServiceChecks(
	status string,
	candidates []ResearchPreflightCandidate,
	details map[researchPreflightPackageIdentity]researchPreflightCandidateDetail,
	signals researchPreflightServiceSignals,
	requestedSources []string,
	workerObserved string,
	resolvedMode string,
) []ResearchPreflightCheck {
	ready := status == ResearchPreflightStatusReady
	checks := []ResearchPreflightCheck{
		researchPreflightIntegrityCheck("package_integrity", signals.packageInvalid, ready, "repair_agent_package"),
		researchPreflightIntegrityCheck("trusted_evaluation", signals.evaluationInvalid, ready, "rerun_agent_evaluation"),
	}
	if len(candidates) == 0 {
		checks = append(checks, ResearchPreflightCheck{
			Code: "agent_package", Status: ResearchPreflightCheckBlocked,
			ResultCode: "none", NextAction: "complete_agent_package",
		})
	} else {
		coverageStatus := ResearchPreflightCheckWarning
		coverageResult := "zero_hits"
		if candidates[0].Coverage.EvidenceCount > 0 {
			coverageStatus = ResearchPreflightCheckPass
			coverageResult = "covered"
		}
		checks = append(checks, ResearchPreflightCheck{
			Code: "knowledge_coverage", Status: coverageStatus, ResultCode: coverageResult,
			NextAction: "review_knowledge_scope",
		})
	}
	worker := ResearchPreflightCheck{
		Code: "worker", Status: ResearchPreflightCheckPass, ResultCode: workerObserved,
	}
	if containsResearchString(requestedSources, ResearchSourceChatlog) && workerObserved != SourceAgentObservedOnline {
		worker.Status = ResearchPreflightCheckBlocked
		worker.NextAction = "start_chatlog_worker"
	}
	checks = append(checks, worker)
	sourceStatus := ResearchPreflightCheckPass
	sourceResult := "allowed"
	if len(candidates) == 0 && signals.scopeRejected > 0 {
		sourceStatus = ResearchPreflightCheckBlocked
		sourceResult = "denied"
	}
	checks = append(checks, ResearchPreflightCheck{
		Code: "source_permissions", Status: sourceStatus, ResultCode: sourceResult,
		NextAction: "review_source_scope",
	})
	budgetStatus := ResearchPreflightCheckBlocked
	budgetResult := "insufficient"
	if len(candidates) > 0 {
		identity, ok := researchPreflightIdentity(AgentPackage{
			PackageID: candidates[0].PackageID, Version: candidates[0].PackageVersion,
			ContentHash: candidates[0].ContentHash,
		})
		if ok && details[identity].budgetFits {
			budgetStatus = ResearchPreflightCheckPass
			budgetResult = "resolved_" + resolvedMode
		}
	}
	checks = append(checks, ResearchPreflightCheck{
		Code: "budget", Status: budgetStatus, ResultCode: budgetResult,
		NextAction: "review_research_budget",
	})
	if signals.coverageInvalid > 0 && ready {
		checks = append(checks, ResearchPreflightCheck{
			Code: "coverage_integrity", Status: ResearchPreflightCheckWarning,
			ResultCode: "excluded", NextAction: "repair_knowledge_release",
		})
	}
	return checks
}

func researchPreflightIntegrityCheck(code string, invalid int, ready bool, action string) ResearchPreflightCheck {
	check := ResearchPreflightCheck{Code: code, Status: ResearchPreflightCheckPass, ResultCode: "validated"}
	if invalid == 0 {
		return check
	}
	check.Status = ResearchPreflightCheckBlocked
	if ready {
		check.Status = ResearchPreflightCheckWarning
	}
	check.ResultCode = "invalid"
	check.NextAction = action
	return check
}

func researchPreflightServiceGaps(gaps []ResearchPreflightGap) []ResearchPreflightGap {
	result := make([]ResearchPreflightGap, 0, len(gaps))
	for _, gap := range gaps {
		switch gap.Code {
		case "no_eligible_package":
			gap.Message = "No eligible Agent Package is available."
			gap.NextAction = "complete_agent_package"
		case "worker_offline":
			gap.Message = "The required local Worker is not ready."
			gap.NextAction = "start_chatlog_worker"
		case "budget_insufficient":
			gap.Message = "The resolved research budget is insufficient."
			gap.NextAction = "review_research_budget"
		}
		result = append(result, gap)
	}
	return result
}

func researchPreflightBudgetFits(budget ResearchPreflightBudget) bool {
	limits := budget.Limits
	return (budget.ResolvedMode == ResearchModeQuick || budget.ResolvedMode == ResearchModeDeep) &&
		limits.MaxIterations > 0 && limits.MaxEvidenceItems > 0 && limits.MaxQuotedChars > 0 &&
		limits.MaxModelCalls > 0 && limits.MaxCostUSD > 0
}

func researchPreflightDependencyRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return !errors.Is(err, os.ErrNotExist)
	}
	var syntaxError *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	return !errors.As(err, &syntaxError) && !errors.As(err, &typeError) &&
		(errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrClosed))
}
