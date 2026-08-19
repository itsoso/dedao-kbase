package app

import (
	"fmt"
	"sort"
	"strings"
)

const (
	ResearchPreflightStatusReady   = "ready"
	ResearchPreflightStatusBlocked = "blocked"

	ResearchPreflightMatchHigh   = "high"
	ResearchPreflightMatchMedium = "medium"
	ResearchPreflightMatchLow    = "low"

	ResearchPreflightCheckPass    = "pass"
	ResearchPreflightCheckWarning = "warning"
	ResearchPreflightCheckBlocked = "blocked"

	researchPreflightCandidateMax = 3
)

const (
	researchPreflightReasonTopicMatch        = "topic_match"
	researchPreflightReasonEvidenceCoverage  = "evidence_coverage"
	researchPreflightReasonFreshRelease      = "fresh_release"
	researchPreflightReasonTrustedEvaluation = "trusted_evaluation"
	researchPreflightReasonWorkerReady       = "worker_ready"
)

type ResearchPreflightRequest struct {
	Mode              string   `json:"mode"`
	Question          string   `json:"question"`
	RequestedSources  []string `json:"requested_sources,omitempty"`
	PackageConstraint string   `json:"package_constraint,omitempty"`
	ParentRunID       string   `json:"parent_run_id,omitempty"`
}

type ResearchPreflight struct {
	PreflightID string                       `json:"preflight_id"`
	RequestHash string                       `json:"-"`
	Status      string                       `json:"status"`
	Candidates  []ResearchPreflightCandidate `json:"candidates"`
	Checks      []ResearchPreflightCheck     `json:"checks"`
	Gaps        []ResearchPreflightGap       `json:"gaps,omitempty"`
	ParentRunID string                       `json:"parent_run_id,omitempty"`
	CreatedAt   string                       `json:"created_at"`
	ExpiresAt   string                       `json:"expires_at"`
}

type ResearchPreflightCandidate struct {
	PackageID        string   `json:"package_id"`
	PackageVersion   string   `json:"package_version"`
	ContentHash      string   `json:"content_hash"`
	DisplayName      string   `json:"display_name,omitempty"`
	MatchLevel       string   `json:"match_level"`
	ReasonCodes      []string `json:"reason_codes,omitempty"`
	KnowledgeScope   []string `json:"knowledge_scope,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
	EvaluationStatus string   `json:"evaluation_status,omitempty"`
	SupportedSources []string `json:"supported_sources,omitempty"`
	Readiness        string   `json:"readiness"`
}

type ResearchPreflightCheck struct {
	Code       string `json:"code"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

type ResearchPreflightGap struct {
	Code       string `json:"code"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

// ResearchPreflightPackageFacts contains precomputed, bounded signals for the
// pure recommendation ranker. RunnablePackageValidated and EvaluationPassed
// must only be set by trusted upstream package and evaluation gates; neither
// value may be accepted from a request.
type ResearchPreflightPackageFacts struct {
	Package                  AgentPackage
	TopicHits                int
	EvidenceHits             int
	LatestPublishedAt        string
	RunnablePackageValidated bool
	EvaluationPassed         bool
	WorkerState              string
	BudgetFits               bool
}

type researchPreflightRankedCandidate struct {
	candidate      ResearchPreflightCandidate
	evidenceBucket int
	topicBucket    int
	readinessRank  int
	freshness      string
}

type PublicResearchPreflightResult struct {
	PreflightID string                             `json:"preflight_id"`
	Status      string                             `json:"status"`
	Candidates  []PublicResearchPreflightCandidate `json:"candidates"`
	Checks      []PublicResearchPreflightCheck     `json:"checks"`
	Gaps        []PublicResearchPreflightGap       `json:"gaps,omitempty"`
	ParentRunID string                             `json:"parent_run_id,omitempty"`
	CreatedAt   string                             `json:"created_at"`
	ExpiresAt   string                             `json:"expires_at"`
}

type PublicResearchPreflightCandidate struct {
	PackageID        string   `json:"package_id"`
	PackageVersion   string   `json:"package_version"`
	DisplayName      string   `json:"display_name,omitempty"`
	MatchLevel       string   `json:"match_level"`
	ReasonCodes      []string `json:"reason_codes,omitempty"`
	KnowledgeScope   []string `json:"knowledge_scope,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
	EvaluationStatus string   `json:"evaluation_status,omitempty"`
	SupportedSources []string `json:"supported_sources,omitempty"`
	Readiness        string   `json:"readiness"`
}

type PublicResearchPreflightCheck struct {
	Code       string `json:"code"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

type PublicResearchPreflightGap struct {
	Code       string `json:"code"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

func NormalizeResearchPreflightRequest(request ResearchPreflightRequest) (ResearchPreflightRequest, error) {
	request.Mode = strings.TrimSpace(request.Mode)
	if request.Mode == "" {
		request.Mode = ResearchModeAuto
	}
	switch request.Mode {
	case ResearchModeAuto, ResearchModeQuick, ResearchModeDeep:
	default:
		return ResearchPreflightRequest{}, fmt.Errorf("mode must be auto, quick, or deep")
	}

	request.Question = strings.TrimSpace(request.Question)
	if request.Question == "" {
		return ResearchPreflightRequest{}, fmt.Errorf("question is required")
	}
	if len([]rune(request.Question)) > researchQuestionMaxRunes {
		return ResearchPreflightRequest{}, fmt.Errorf("question exceeds %d characters", researchQuestionMaxRunes)
	}

	request.PackageConstraint = strings.TrimSpace(request.PackageConstraint)
	if len([]rune(request.PackageConstraint)) > researchPackageIDMaxRunes {
		return ResearchPreflightRequest{}, fmt.Errorf("package_constraint exceeds %d characters", researchPackageIDMaxRunes)
	}
	request.ParentRunID = strings.TrimSpace(request.ParentRunID)
	if len([]rune(request.ParentRunID)) > researchPackageIDMaxRunes {
		return ResearchPreflightRequest{}, fmt.Errorf("parent_run_id exceeds %d characters", researchPackageIDMaxRunes)
	}

	if len(request.RequestedSources) > researchRequestedSourcesMax {
		return ResearchPreflightRequest{}, fmt.Errorf("requested_sources exceeds %d items", researchRequestedSourcesMax)
	}
	seenSources := make(map[string]bool, len(request.RequestedSources))
	normalizedSources := make([]string, 0, len(request.RequestedSources))
	for _, rawSource := range request.RequestedSources {
		source := strings.TrimSpace(rawSource)
		switch source {
		case ResearchSourceKnowledge, ResearchSourceChatlog, ResearchSourcePriorRuns:
		default:
			return ResearchPreflightRequest{}, fmt.Errorf("unsupported requested source %q", source)
		}
		if seenSources[source] {
			return ResearchPreflightRequest{}, fmt.Errorf("duplicate requested source %q", source)
		}
		seenSources[source] = true
		normalizedSources = append(normalizedSources, source)
	}
	sort.Slice(normalizedSources, func(left, right int) bool {
		return researchPreflightSourceOrder(normalizedSources[left]) < researchPreflightSourceOrder(normalizedSources[right])
	})
	request.RequestedSources = normalizedSources

	return request, nil
}

func ValidateResearchPreflight(preflight ResearchPreflight) error {
	switch preflight.Status {
	case ResearchPreflightStatusReady, ResearchPreflightStatusBlocked:
	default:
		return fmt.Errorf("unsupported preflight status %q", preflight.Status)
	}
	if len(preflight.Candidates) > researchPreflightCandidateMax {
		return fmt.Errorf("candidates exceeds %d items", researchPreflightCandidateMax)
	}
	confirmableCandidate := false
	blockedCandidates := 0
	for index, candidate := range preflight.Candidates {
		if strings.TrimSpace(candidate.PackageID) == "" {
			return fmt.Errorf("candidate %d package_id is required", index)
		}
		if strings.TrimSpace(candidate.PackageVersion) == "" {
			return fmt.Errorf("candidate %d package_version is required", index)
		}
		if strings.TrimSpace(candidate.ContentHash) == "" {
			return fmt.Errorf("candidate %d content_hash is required", index)
		}
		if !isResearchPreflightMatchLevel(candidate.MatchLevel) {
			return fmt.Errorf("candidate %d has unsupported match level %q", index, candidate.MatchLevel)
		}
		if !isResearchPreflightCheckStatus(candidate.Readiness) {
			return fmt.Errorf("candidate %d has unsupported check status %q", index, candidate.Readiness)
		}
		switch candidate.Readiness {
		case ResearchPreflightCheckPass, ResearchPreflightCheckWarning:
			confirmableCandidate = true
		case ResearchPreflightCheckBlocked:
			blockedCandidates++
		}
	}
	if preflight.Status == ResearchPreflightStatusReady {
		if len(preflight.Candidates) > 0 && blockedCandidates == len(preflight.Candidates) {
			return fmt.Errorf("ready preflight cannot have all candidates blocked")
		}
		if !confirmableCandidate {
			return fmt.Errorf("ready preflight requires a confirmable candidate")
		}
	}
	for index, check := range preflight.Checks {
		if !isResearchPreflightCheckStatus(check.Status) {
			return fmt.Errorf("check %d has unsupported check status %q", index, check.Status)
		}
		if preflight.Status == ResearchPreflightStatusReady && check.Status == ResearchPreflightCheckBlocked {
			return fmt.Errorf("ready preflight cannot contain blocked check %d", index)
		}
	}
	return nil
}

func PublicResearchPreflight(preflight ResearchPreflight) PublicResearchPreflightResult {
	public := PublicResearchPreflightResult{
		PreflightID: preflight.PreflightID,
		Status:      preflight.Status,
		Candidates:  make([]PublicResearchPreflightCandidate, 0, len(preflight.Candidates)),
		Checks:      make([]PublicResearchPreflightCheck, 0, len(preflight.Checks)),
		Gaps:        make([]PublicResearchPreflightGap, 0, len(preflight.Gaps)),
		ParentRunID: preflight.ParentRunID,
		CreatedAt:   preflight.CreatedAt,
		ExpiresAt:   preflight.ExpiresAt,
	}
	for _, candidate := range preflight.Candidates {
		public.Candidates = append(public.Candidates, PublicResearchPreflightCandidate{
			PackageID:        candidate.PackageID,
			PackageVersion:   candidate.PackageVersion,
			DisplayName:      candidate.DisplayName,
			MatchLevel:       candidate.MatchLevel,
			ReasonCodes:      append([]string(nil), candidate.ReasonCodes...),
			KnowledgeScope:   append([]string(nil), candidate.KnowledgeScope...),
			UpdatedAt:        candidate.UpdatedAt,
			EvaluationStatus: candidate.EvaluationStatus,
			SupportedSources: append([]string(nil), candidate.SupportedSources...),
			Readiness:        candidate.Readiness,
		})
	}
	for _, check := range preflight.Checks {
		public.Checks = append(public.Checks, PublicResearchPreflightCheck{
			Code:       check.Code,
			Status:     check.Status,
			Message:    check.Message,
			NextAction: check.NextAction,
		})
	}
	for _, gap := range preflight.Gaps {
		public.Gaps = append(public.Gaps, PublicResearchPreflightGap{
			Code:       gap.Code,
			Message:    gap.Message,
			NextAction: gap.NextAction,
		})
	}
	return public
}

// RankResearchPreflightCandidates filters policy eligibility and ranks only
// precomputed facts. It performs no store, network, Worker, or model I/O.
func RankResearchPreflightCandidates(
	request ResearchPreflightRequest,
	facts []ResearchPreflightPackageFacts,
) ([]ResearchPreflightCandidate, []ResearchPreflightGap) {
	normalized, err := NormalizeResearchPreflightRequest(request)
	if err != nil {
		return nil, []ResearchPreflightGap{{Code: "no_eligible_package"}}
	}

	requiresWorker := containsResearchString(normalized.RequestedSources, ResearchSourceChatlog)
	ranked := make([]researchPreflightRankedCandidate, 0, len(facts))
	workerBlocked := false
	budgetBlocked := false
	for _, packageFacts := range facts {
		pkg := packageFacts.Package
		if normalized.PackageConstraint != "" && strings.TrimSpace(pkg.PackageID) != normalized.PackageConstraint {
			continue
		}
		if !researchPreflightPackageEligible(normalized, packageFacts) {
			continue
		}

		readiness := ResearchPreflightCheckPass
		readinessRank := 2
		workerReady := false
		if requiresWorker {
			switch strings.TrimSpace(packageFacts.WorkerState) {
			case SourceAgentObservedOnline:
				workerReady = true
			case SourceAgentObservedDegraded, SourceAgentObservedUpgrading:
				readiness = ResearchPreflightCheckWarning
				readinessRank = 1
			default:
				readiness = ResearchPreflightCheckBlocked
				readinessRank = 0
				workerBlocked = true
			}
		}
		if !packageFacts.BudgetFits {
			readiness = ResearchPreflightCheckBlocked
			readinessRank = 0
			budgetBlocked = true
		}

		evidenceBucket := researchPreflightSignalBucket(packageFacts.EvidenceHits)
		topicBucket := researchPreflightSignalBucket(packageFacts.TopicHits)
		latestPublishedAt := strings.TrimSpace(packageFacts.LatestPublishedAt)
		updatedAt := latestPublishedAt
		if updatedAt == "" {
			updatedAt = strings.TrimSpace(pkg.PublishedAt)
		}
		reasons := make([]string, 0, 5)
		if evidenceBucket > 0 {
			reasons = append(reasons, researchPreflightReasonEvidenceCoverage)
		}
		if topicBucket > 0 {
			reasons = append(reasons, researchPreflightReasonTopicMatch)
		}
		if latestPublishedAt != "" {
			reasons = append(reasons, researchPreflightReasonFreshRelease)
		}
		reasons = append(reasons, researchPreflightReasonTrustedEvaluation)
		if workerReady {
			reasons = append(reasons, researchPreflightReasonWorkerReady)
		}

		matchLevel := ResearchPreflightMatchLow
		if evidenceBucket > 0 {
			matchLevel = ResearchPreflightMatchHigh
		} else if topicBucket > 0 {
			matchLevel = ResearchPreflightMatchMedium
		}
		ranked = append(ranked, researchPreflightRankedCandidate{
			candidate: ResearchPreflightCandidate{
				PackageID:        strings.TrimSpace(pkg.PackageID),
				PackageVersion:   strings.TrimSpace(pkg.Version),
				ContentHash:      strings.TrimSpace(pkg.ContentHash),
				DisplayName:      strings.TrimSpace(pkg.PackageID),
				MatchLevel:       matchLevel,
				ReasonCodes:      reasons,
				KnowledgeScope:   sortedUniqueStrings(pkg.RetrievalPolicy.AllowedSourceTypes),
				UpdatedAt:        updatedAt,
				EvaluationStatus: "passed",
				SupportedSources: researchPreflightSortedSources(pkg.ResearchPolicy.AllowedSources),
				Readiness:        readiness,
			},
			evidenceBucket: evidenceBucket,
			topicBucket:    topicBucket,
			readinessRank:  readinessRank,
			freshness:      updatedAt,
		})
	}

	if len(ranked) == 0 {
		return nil, []ResearchPreflightGap{{Code: "no_eligible_package"}}
	}
	sort.Slice(ranked, func(left, right int) bool {
		return researchPreflightCandidateLess(ranked[left], ranked[right])
	})
	limit := len(ranked)
	if limit > researchPreflightCandidateMax {
		limit = researchPreflightCandidateMax
	}
	candidates := make([]ResearchPreflightCandidate, 0, limit)
	for _, item := range ranked[:limit] {
		candidates = append(candidates, item.candidate)
	}

	gaps := make([]ResearchPreflightGap, 0, 2)
	if workerBlocked {
		gaps = append(gaps, ResearchPreflightGap{Code: "worker_offline"})
	}
	if budgetBlocked {
		gaps = append(gaps, ResearchPreflightGap{Code: "budget_insufficient"})
	}
	return candidates, gaps
}

func researchPreflightPackageEligible(request ResearchPreflightRequest, facts ResearchPreflightPackageFacts) bool {
	pkg := facts.Package
	if pkg.LifecycleState != AgentPackagePublished || !facts.RunnablePackageValidated || !facts.EvaluationPassed ||
		strings.TrimSpace(pkg.PackageID) == "" || strings.TrimSpace(pkg.Version) == "" ||
		strings.TrimSpace(pkg.ContentHash) == "" || !agentPackageIDPattern.MatchString(pkg.PackageID) {
		return false
	}
	wantContentHash, err := AgentPackageContentHash(pkg)
	if err != nil || pkg.ContentHash != wantContentHash {
		return false
	}
	if err := validateResearchAgentPackageScope(pkg, request.Mode, request.RequestedSources); err != nil {
		return false
	}
	if err := validateAgentPackageResearch(*pkg.ResearchPolicy, pkg.ToolPolicy); err != nil {
		return false
	}
	return true
}

func researchPreflightSignalBucket(hits int) int {
	switch {
	case hits >= 3:
		return 2
	case hits > 0:
		return 1
	default:
		return 0
	}
}

func researchPreflightCandidateLess(left, right researchPreflightRankedCandidate) bool {
	if left.evidenceBucket != right.evidenceBucket {
		return left.evidenceBucket > right.evidenceBucket
	}
	if left.topicBucket != right.topicBucket {
		return left.topicBucket > right.topicBucket
	}
	if left.readinessRank != right.readinessRank {
		return left.readinessRank > right.readinessRank
	}
	if left.freshness != right.freshness {
		return left.freshness > right.freshness
	}
	leftPackageID := strings.ToLower(strings.TrimSpace(left.candidate.PackageID))
	rightPackageID := strings.ToLower(strings.TrimSpace(right.candidate.PackageID))
	if leftPackageID != rightPackageID {
		return leftPackageID < rightPackageID
	}
	if left.candidate.PackageVersion != right.candidate.PackageVersion {
		return left.candidate.PackageVersion < right.candidate.PackageVersion
	}
	if left.candidate.ContentHash != right.candidate.ContentHash {
		return left.candidate.ContentHash < right.candidate.ContentHash
	}
	return left.candidate.PackageID < right.candidate.PackageID
}

func researchPreflightSortedSources(sources []string) []string {
	result := uniqueTrimmedStrings(sources)
	sort.Slice(result, func(left, right int) bool {
		leftOrder := researchPreflightSourceOrder(result[left])
		rightOrder := researchPreflightSourceOrder(result[right])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return result[left] < result[right]
	})
	return result
}

func researchPreflightSourceOrder(source string) int {
	switch source {
	case ResearchSourceKnowledge:
		return 0
	case ResearchSourceChatlog:
		return 1
	case ResearchSourcePriorRuns:
		return 2
	default:
		return 3
	}
}

func isResearchPreflightMatchLevel(value string) bool {
	switch value {
	case ResearchPreflightMatchHigh, ResearchPreflightMatchMedium, ResearchPreflightMatchLow:
		return true
	default:
		return false
	}
}

func isResearchPreflightCheckStatus(value string) bool {
	switch value {
	case ResearchPreflightCheckPass, ResearchPreflightCheckWarning, ResearchPreflightCheckBlocked:
		return true
	default:
		return false
	}
}
