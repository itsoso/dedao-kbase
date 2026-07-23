package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const evidenceAuditModelOutputSchema = "evidence-audit-model.v1"

type EvidenceAuditRunnerConfig struct {
	ModelConfig BookTokenPlanConfig
	Timeout     time.Duration
	Now         func() time.Time
}

type evidenceAuditModelDecision struct {
	CandidateVerdict string                       `json:"candidate_verdict"`
	Rationale        string                       `json:"rationale"`
	Evidence         []evidenceAuditModelEvidence `json:"evidence"`
	Limitations      []string                     `json:"limitations"`
	KnowledgeGaps    []string                     `json:"knowledge_gaps"`
	ReviewActions    []string                     `json:"review_actions"`
}

type evidenceAuditModelEvidence struct {
	ReleaseID  string `json:"release_id"`
	CitationID string `json:"citation_id"`
	Stance     string `json:"stance"`
}

type evidenceAuditRetrievedItem struct {
	Evidence AgentPackageEvidence
	Ref      EvidenceAuditEvidenceRef
}

func PrepareEvidenceAuditInput(
	store *BookKnowledgeStore,
	packageID, version, subject, scope string,
) (EvidenceAuditInput, error) {
	pkg, releases, err := loadEvidenceAuditPackageSnapshot(store, packageID, version)
	if err != nil {
		return EvidenceAuditInput{}, err
	}
	primary, ok := evidenceAuditReleaseByRole(pkg, releases, AgentEvidenceReleasePrimary)
	if !ok || primary.Analysis == nil {
		return EvidenceAuditInput{}, fmt.Errorf("evidence audit primary release has no structured claims")
	}
	selectedClaims := make([]string, 0, pkg.EvidencePolicy.MaxClaims)
	for _, claim := range primary.Analysis.Claims {
		statement := strings.TrimSpace(claim.Statement)
		if statement == "" {
			continue
		}
		selectedClaims = append(selectedClaims, statement)
		if len(selectedClaims) == pkg.EvidencePolicy.MaxClaims {
			break
		}
	}
	if len(selectedClaims) == 0 {
		return EvidenceAuditInput{}, fmt.Errorf("evidence audit primary release has no selectable claims")
	}
	releaseRefs, err := evidenceAuditInputReleaseRefs(pkg, releases)
	if err != nil {
		return EvidenceAuditInput{}, err
	}
	model := normalizeBookTokenPlanModel(firstAgentPackageModel(pkg.ModelPolicy))
	if model == "" {
		return EvidenceAuditInput{}, fmt.Errorf("model_policy has no executable fallback model")
	}
	input := EvidenceAuditInput{
		SchemaVersion: EvidenceAuditSchemaVersion,
		Package: EvidenceAuditPackageRef{
			PackageID: pkg.PackageID, Version: pkg.Version, ContentHash: pkg.ContentHash,
		},
		EvidencePolicy: EvidenceAuditPolicySnapshot{
			MinimumIndependentSources: pkg.EvidencePolicy.MinimumIndependentSources,
			MaxClaims:                 pkg.EvidencePolicy.MaxClaims,
			MaxEvidencePerClaim:       pkg.EvidencePolicy.MaxEvidencePerClaim,
		},
		Model: EvidenceAuditModelIdentity{
			Provider: "tokenplan", Model: model, Route: "evidence-audit",
		},
		Retrieval: EvidenceAuditRetrievalIdentity{
			Strategy:         pkg.RetrievalPolicy.Strategy,
			IndexVersion:     "package-" + strings.TrimPrefix(pkg.ContentHash, "sha256:")[:16],
			RerankerVersion:  pkg.RetrievalPolicy.RerankerVersion,
			EmbeddingVersion: pkg.RetrievalPolicy.EmbeddingVersion,
		},
		Releases:       releaseRefs,
		Subject:        strings.TrimSpace(subject),
		Scope:          strings.TrimSpace(scope),
		SelectedClaims: selectedClaims,
	}
	if _, err := EvidenceAuditInputHash(input); err != nil {
		return EvidenceAuditInput{}, err
	}
	return input, nil
}

func RunEvidenceAudit(
	ctx context.Context,
	store *BookKnowledgeStore,
	auditID string,
	client BookKnowledgeLLMClient,
	config EvidenceAuditRunnerConfig,
) (*EvidenceAudit, error) {
	if store == nil {
		return nil, fmt.Errorf("evidence audit store is required")
	}
	audit, err := store.LoadEvidenceAudit(strings.TrimSpace(auditID))
	if err != nil {
		return nil, err
	}
	if audit.Status != EvidenceAuditQueued {
		return nil, fmt.Errorf("evidence audit %q must be queued, got %q", audit.AuditID, audit.Status)
	}
	traceID, err := newAgentRuntimeTraceID()
	if err != nil {
		return nil, err
	}
	now := evidenceAuditRunnerNow(config)
	if _, err := StartEvidenceAudit(store, audit.AuditID, traceID, now); err != nil {
		return nil, err
	}

	pkg, releases, runErr := loadEvidenceAuditPackageSnapshot(
		store, audit.Package.PackageID, audit.Package.Version,
	)
	if runErr == nil && pkg.ContentHash != audit.Package.ContentHash {
		runErr = fmt.Errorf("published package hash changed")
	}
	if runErr == nil {
		runErr = validateEvidenceAuditRunnerInput(*audit, pkg, releases)
	}
	if runErr != nil {
		return nil, failEvidenceAuditRun(store, *audit, nil, nil, traceID, config, runErr)
	}
	if evidenceAuditRequestsMedicalAdvice(audit.Subject + "\n" + audit.Scope) {
		claims := make([]EvidenceAuditClaim, 0, len(audit.SelectedClaims))
		for _, claim := range audit.SelectedClaims {
			claims = append(claims, EvidenceAuditClaim{
				SourceClaim: claim, NormalizedStatement: claim,
				Verdict: EvidenceAuditVerdictInsufficient, ComputedConfidence: 0,
				Limitations:   []string{"Individual diagnosis or treatment advice is outside this evidence audit."},
				KnowledgeGaps: []string{"A licensed clinician must assess individual context."},
				ReviewActions: []string{"Use the audit only as evidence review, not medical advice."},
			})
		}
		return completeEvidenceAuditRun(
			store, *audit, pkg, nil, claims, traceID, config, AgentTraceOutcomeAbstained,
		)
	}
	if client == nil {
		client = NewTokenPlanChatClient(nil)
	}
	modelConfig, err := evidenceAuditRunnerModelConfig(pkg, config)
	if err != nil {
		return nil, failEvidenceAuditRun(store, *audit, pkg, nil, traceID, config, err)
	}
	allRetrieved := make([]evidenceAuditRetrievedItem, 0)
	claimAudits := make([]EvidenceAuditClaim, 0, len(audit.SelectedClaims))
	for _, sourceClaim := range audit.SelectedClaims {
		retrieved, retrievalErr := retrieveEvidenceAuditSupportingEvidence(store, pkg, releases, sourceClaim)
		if retrievalErr != nil {
			return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, retrievalErr)
		}
		allRetrieved = append(allRetrieved, retrieved...)
		messages := buildEvidenceAuditModelMessages(pkg, sourceClaim, retrieved)
		if err := applyAgentRuntimeCostBudget(&modelConfig, messages, pkg.ModelPolicy.MaxCostUSD); err != nil {
			return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, err)
		}
		callCtx := ctx
		timeout := config.Timeout
		if timeout <= 0 && pkg.ModelPolicy.TimeoutMS > 0 {
			timeout = time.Duration(pkg.ModelPolicy.TimeoutMS) * time.Millisecond
		}
		var cancel context.CancelFunc
		if timeout > 0 {
			callCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		raw, modelErr := client.Chat(callCtx, modelConfig, messages)
		if cancel != nil {
			cancel()
		}
		if modelErr != nil {
			return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, modelErr)
		}
		decision, parseErr := parseEvidenceAuditModelDecision(raw)
		if parseErr != nil {
			return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, parseErr)
		}
		claimAudit, decisionErr := decideEvidenceAuditClaim(pkg, sourceClaim, retrieved, decision)
		if decisionErr != nil {
			return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, decisionErr)
		}
		claimAudits = append(claimAudits, claimAudit)
	}
	return completeEvidenceAuditRun(
		store, *audit, pkg, allRetrieved, claimAudits, traceID, config, AgentTraceOutcomeCompleted,
	)
}

func loadEvidenceAuditPackageSnapshot(
	store *BookKnowledgeStore,
	packageID, version string,
) (AgentPackage, map[string]KnowledgeRelease, error) {
	pkg, err := loadRunnableAgentPackage(store, packageID, version, "evidence")
	if err != nil {
		return AgentPackage{}, nil, err
	}
	if pkg.SchemaVersion != AgentPackageSchemaVersionV2 || pkg.EvidencePolicy == nil {
		return AgentPackage{}, nil, fmt.Errorf("evidence audit requires a published agent-package.v2 with evidence_policy")
	}
	releases := make(map[string]KnowledgeRelease, len(pkg.Releases))
	for _, ref := range pkg.Releases {
		release, loadErr := store.LoadKnowledgeRelease(ref.ReleaseID)
		if loadErr != nil {
			return AgentPackage{}, nil, fmt.Errorf("load pinned release %q: %w", ref.ReleaseID, loadErr)
		}
		if agentTraceReleaseContentHash(release.ContentHash) != agentTraceReleaseContentHash(ref.ContentHash) {
			return AgentPackage{}, nil, fmt.Errorf("pinned release %q content hash changed", ref.ReleaseID)
		}
		releases[ref.ReleaseID] = *release
	}
	return *pkg, releases, nil
}

func evidenceAuditInputReleaseRefs(
	pkg AgentPackage,
	releases map[string]KnowledgeRelease,
) ([]EvidenceAuditReleaseRef, error) {
	roles := make(map[string]string, len(pkg.EvidencePolicy.ReleaseRoles))
	for _, role := range pkg.EvidencePolicy.ReleaseRoles {
		roles[role.ReleaseID] = role.Role
	}
	result := make([]EvidenceAuditReleaseRef, 0, len(pkg.Releases))
	for _, pkgRef := range pkg.Releases {
		release := releases[pkgRef.ReleaseID]
		sourceType := strings.ToLower(strings.TrimSpace(release.Book.SourceType))
		ref := EvidenceAuditReleaseRef{
			ReleaseID: pkgRef.ReleaseID, ContentHash: agentTraceReleaseContentHash(pkgRef.ContentHash),
			Role: roles[pkgRef.ReleaseID], SourceType: sourceType,
			PublicationIdentity: sha256Fingerprint([]byte(sourceType)),
		}
		allowed := stringBoolSet(pkgRef.CitationIDs...)
		seen := map[string]bool{}
		for _, claim := range release.Analysis.Claims {
			for _, citationID := range resolveAgentClaimCitationIDs(release.Citations, claim.CitationIDs) {
				if !allowed[citationID] || seen[citationID] {
					continue
				}
				citation, ok := evidenceAuditCitationByID(release, citationID)
				if !ok || strings.TrimSpace(citation.ChunkID) == "" {
					return nil, fmt.Errorf("pinned citation %q in release %q is unresolved", citationID, release.ReleaseID)
				}
				seen[citationID] = true
				ref.Citations = append(ref.Citations, EvidenceAuditCitationRef{
					CitationID: citationID, ClaimID: claim.ID, ChunkID: citation.ChunkID,
				})
			}
		}
		if len(ref.Citations) == 0 {
			return nil, fmt.Errorf("pinned release %q has no resolvable citation identity", release.ReleaseID)
		}
		result = append(result, ref)
	}
	return result, nil
}

func evidenceAuditReleaseByRole(
	pkg AgentPackage,
	releases map[string]KnowledgeRelease,
	role string,
) (KnowledgeRelease, bool) {
	for _, item := range pkg.EvidencePolicy.ReleaseRoles {
		if item.Role == role {
			release, ok := releases[item.ReleaseID]
			return release, ok
		}
	}
	return KnowledgeRelease{}, false
}

func validateEvidenceAuditRunnerInput(
	audit EvidenceAudit,
	pkg AgentPackage,
	releases map[string]KnowledgeRelease,
) error {
	expected, err := evidenceAuditInputReleaseRefs(pkg, releases)
	if err != nil {
		return err
	}
	if len(expected) != len(audit.Releases) {
		return fmt.Errorf("audit pinned release set does not match package")
	}
	expectedByID := make(map[string]EvidenceAuditReleaseRef, len(expected))
	for _, release := range expected {
		expectedByID[release.ReleaseID] = release
	}
	for _, release := range audit.Releases {
		expectedRelease, ok := expectedByID[release.ReleaseID]
		if !ok || expectedRelease.ContentHash != release.ContentHash ||
			expectedRelease.Role != release.Role ||
			expectedRelease.PublicationIdentity != release.PublicationIdentity {
			return fmt.Errorf("audit pinned release %q no longer matches package snapshot", release.ReleaseID)
		}
	}
	primary, ok := evidenceAuditReleaseByRole(pkg, releases, AgentEvidenceReleasePrimary)
	if !ok || primary.Analysis == nil {
		return fmt.Errorf("primary release is unavailable")
	}
	allowedClaims := map[string]bool{}
	for _, claim := range primary.Analysis.Claims {
		allowedClaims[strings.TrimSpace(claim.Statement)] = true
	}
	for _, selected := range audit.SelectedClaims {
		if !allowedClaims[selected] {
			return fmt.Errorf("selected claim is outside the pinned primary release")
		}
	}
	return nil
}

func retrieveEvidenceAuditSupportingEvidence(
	store *BookKnowledgeStore,
	pkg AgentPackage,
	releases map[string]KnowledgeRelease,
	sourceClaim string,
) ([]evidenceAuditRetrievedItem, error) {
	roles := make(map[string]string, len(pkg.EvidencePolicy.ReleaseRoles))
	for _, role := range pkg.EvidencePolicy.ReleaseRoles {
		roles[role.ReleaseID] = role.Role
	}
	result := make([]evidenceAuditRetrievedItem, 0)
	seen := map[string]bool{}
	for _, role := range pkg.EvidencePolicy.ReleaseRoles {
		if role.Role != AgentEvidenceReleaseSupporting {
			continue
		}
		release := releases[role.ReleaseID]
		found, err := searchAgentPackageReleaseEvidence(
			store, pkg, release.ReleaseID, sourceClaim, pkg.EvidencePolicy.MaxEvidencePerClaim,
		)
		if err != nil {
			return nil, err
		}
		for _, item := range found {
			for _, citationID := range item.CitationIDs {
				citation, err := resolveAgentPackageReleaseCitation(
					store, pkg, release.ReleaseID, item.ClaimID, citationID,
				)
				if err != nil || strings.TrimSpace(citation.ChunkID) == "" {
					if err == nil {
						err = fmt.Errorf("citation has no immutable chunk identity")
					}
					return nil, fmt.Errorf(
						"citation %q in release %q cannot be resolved: %w",
						citationID, release.ReleaseID, err,
					)
				}
				ref := EvidenceAuditEvidenceRef{
					ReleaseID: release.ReleaseID, ContentHash: agentTraceReleaseContentHash(release.ContentHash),
					Role: EvidenceAuditReleaseSupporting, SourceType: strings.ToLower(strings.TrimSpace(release.Book.SourceType)),
					PublicationIdentity: sha256Fingerprint([]byte(strings.ToLower(strings.TrimSpace(release.Book.SourceType)))),
					ClaimID:             item.ClaimID, ChunkID: citation.ChunkID, CitationID: citationID,
				}
				key := evidenceAuditEvidenceIdentity(ref)
				if seen[key] {
					continue
				}
				seen[key] = true
				result = append(result, evidenceAuditRetrievedItem{
					Evidence: AgentPackageEvidence{
						ReleaseID: release.ReleaseID, ClaimID: item.ClaimID, Statement: item.Statement,
						CitationIDs: []string{citationID}, Score: item.Score,
					},
					Ref: ref,
				})
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Ref.PublicationIdentity != result[j].Ref.PublicationIdentity {
			return result[i].Ref.PublicationIdentity < result[j].Ref.PublicationIdentity
		}
		if result[i].Ref.SourceType != result[j].Ref.SourceType {
			return result[i].Ref.SourceType < result[j].Ref.SourceType
		}
		return evidenceAuditEvidenceIdentity(result[i].Ref) < evidenceAuditEvidenceIdentity(result[j].Ref)
	})
	return capEvidenceAuditSupportingGroups(result, pkg.EvidencePolicy.MaxEvidencePerClaim), nil
}

func capEvidenceAuditSupportingGroups(
	evidence []evidenceAuditRetrievedItem,
	limit int,
) []evidenceAuditRetrievedItem {
	if limit <= 0 || len(evidence) <= limit {
		return evidence
	}
	groups := map[string][]evidenceAuditRetrievedItem{}
	keys := make([]string, 0)
	for _, item := range evidence {
		key := item.Ref.PublicationIdentity + "\x00" + item.Ref.SourceType
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], item)
	}
	sort.Strings(keys)
	result := make([]evidenceAuditRetrievedItem, 0, limit)
	for offset := 0; len(result) < limit; offset++ {
		added := false
		for _, key := range keys {
			if offset >= len(groups[key]) {
				continue
			}
			result = append(result, groups[key][offset])
			added = true
			if len(result) == limit {
				break
			}
		}
		if !added {
			break
		}
	}
	return result
}

func evidenceAuditCitationByID(release KnowledgeRelease, citationID string) (BookKnowledgeCitation, bool) {
	for _, citation := range release.Citations {
		if citation.CitationID == citationID {
			return citation, true
		}
	}
	return BookKnowledgeCitation{}, false
}

func buildEvidenceAuditModelMessages(
	pkg AgentPackage,
	sourceClaim string,
	evidence []evidenceAuditRetrievedItem,
) []BookKnowledgeMessage {
	var builder strings.Builder
	builder.WriteString("Source claim: ")
	builder.WriteString(sourceClaim)
	builder.WriteString("\nPinned supporting evidence (metadata and statements only):\n")
	for _, item := range evidence {
		fmt.Fprintf(
			&builder,
			"- release_id=%s citation_id=%s claim_id=%s publication_identity=%s source_type=%s\n  %s\n",
			item.Ref.ReleaseID, item.Ref.CitationID, item.Ref.ClaimID,
			item.Ref.PublicationIdentity, item.Ref.SourceType, item.Evidence.Statement,
		)
	}
	return []BookKnowledgeMessage{
		{
			Role: "system",
			Content: "Return one strict JSON object for schema " + evidenceAuditModelOutputSchema +
				". Use only the listed pinned evidence. candidate_verdict is advisory; code decides the final verdict. " +
				"Evidence stance must be supports or contradicts. Do not provide diagnosis, treatment, or individual medical advice.",
		},
		{Role: "user", Content: builder.String()},
	}
}

func parseEvidenceAuditModelDecision(raw string) (evidenceAuditModelDecision, error) {
	var decision evidenceAuditModelDecision
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		return decision, fmt.Errorf("invalid evidence audit model JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return decision, fmt.Errorf("invalid evidence audit model JSON: %w", err)
	}
	switch decision.CandidateVerdict {
	case EvidenceAuditVerdictSupported, EvidenceAuditVerdictContradicted,
		EvidenceAuditVerdictMixed, EvidenceAuditVerdictInsufficient:
	default:
		return decision, fmt.Errorf("invalid candidate verdict %q", decision.CandidateVerdict)
	}
	if strings.TrimSpace(decision.Rationale) == "" {
		return decision, fmt.Errorf("model rationale is required")
	}
	for index, item := range decision.Evidence {
		if strings.TrimSpace(item.ReleaseID) == "" || strings.TrimSpace(item.CitationID) == "" {
			return decision, fmt.Errorf("model evidence[%d] requires release_id and citation_id", index)
		}
		if item.Stance != "supports" && item.Stance != "contradicts" {
			return decision, fmt.Errorf("model evidence[%d] has invalid stance %q", index, item.Stance)
		}
	}
	return decision, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple JSON values are not allowed")
	}
	return err
}

func decideEvidenceAuditClaim(
	pkg AgentPackage,
	sourceClaim string,
	retrieved []evidenceAuditRetrievedItem,
	decision evidenceAuditModelDecision,
) (EvidenceAuditClaim, error) {
	available := make(map[string]evidenceAuditRetrievedItem, len(retrieved))
	for _, item := range retrieved {
		available[item.Ref.ReleaseID+"\x00"+item.Ref.CitationID] = item
	}
	selected := make([]EvidenceAuditEvidenceRef, 0, len(decision.Evidence))
	seen := map[string]bool{}
	supports, contradicts := 0, 0
	for _, modelRef := range decision.Evidence {
		item, ok := available[modelRef.ReleaseID+"\x00"+modelRef.CitationID]
		if !ok {
			return EvidenceAuditClaim{}, fmt.Errorf(
				"model citation %q is outside retrieved pinned evidence", modelRef.CitationID,
			)
		}
		key := evidenceAuditEvidenceIdentity(item.Ref)
		if seen[key] {
			continue
		}
		seen[key] = true
		ref := item.Ref
		if modelRef.Stance == "contradicts" {
			ref.Conflict = true
			contradicts++
		} else {
			supports++
		}
		selected = append(selected, ref)
	}
	if len(selected) > pkg.EvidencePolicy.MaxEvidencePerClaim {
		selected = selected[:pkg.EvidencePolicy.MaxEvidencePerClaim]
	}
	supports, contradicts = 0, 0
	for _, ref := range selected {
		if ref.Conflict {
			contradicts++
		} else {
			supports++
		}
	}
	verdict := EvidenceAuditVerdictInsufficient
	switch {
	case supports > 0 && contradicts > 0:
		verdict = EvidenceAuditVerdictMixed
	case supports > 0:
		verdict = EvidenceAuditVerdictSupported
	case contradicts > 0:
		verdict = EvidenceAuditVerdictContradicted
	}
	publications := map[string]bool{}
	for _, ref := range selected {
		publications[ref.PublicationIdentity] = true
	}
	if verdict != EvidenceAuditVerdictInsufficient &&
		len(publications) < pkg.EvidencePolicy.MinimumIndependentSources {
		verdict = EvidenceAuditVerdictInsufficient
		selected = nil
	}
	conflicts := evidenceConflictCount(selected)
	return EvidenceAuditClaim{
		SourceClaim: sourceClaim, NormalizedStatement: sourceClaim, Verdict: verdict,
		Evidence: selected, ComputedConfidence: ComputeEvidenceAuditConfidence(selected, conflicts),
		Limitations:   evidenceAuditBoundedStrings(decision.Limitations),
		KnowledgeGaps: evidenceAuditBoundedStrings(decision.KnowledgeGaps),
		ReviewActions: evidenceAuditBoundedStrings(decision.ReviewActions),
	}, nil
}

func evidenceConflictCount(evidence []EvidenceAuditEvidenceRef) int {
	count := 0
	for _, ref := range evidence {
		if ref.Conflict {
			count++
		}
	}
	return count
}

func evidenceAuditBoundedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > evidenceAuditMaxTextBytes {
			value = value[:evidenceAuditMaxTextBytes]
		}
		result = append(result, value)
		if len(result) == evidenceAuditMaxListItems {
			break
		}
	}
	return result
}

func evidenceAuditRunnerModelConfig(
	pkg AgentPackage,
	config EvidenceAuditRunnerConfig,
) (BookTokenPlanConfig, error) {
	cfg := config.ModelConfig
	var err error
	if strings.TrimSpace(cfg.APIKey) == "" {
		cfg, err = LoadBookTokenPlanConfig()
		if err != nil {
			return BookTokenPlanConfig{}, err
		}
	}
	cfg.Model = normalizeBookTokenPlanModel(firstAgentPackageModel(pkg.ModelPolicy))
	return cfg, nil
}

func completeEvidenceAuditRun(
	store *BookKnowledgeStore,
	audit EvidenceAudit,
	pkg AgentPackage,
	retrieved []evidenceAuditRetrievedItem,
	claims []EvidenceAuditClaim,
	traceID string,
	config EvidenceAuditRunnerConfig,
	traceOutcome string,
) (*EvidenceAudit, error) {
	counts := map[string]int{}
	limitations := []string{"Audit is limited to immutable Package-pinned releases."}
	reviewItems := make([]string, 0)
	for _, claim := range claims {
		counts[claim.Verdict]++
		reviewItems = append(reviewItems, claim.ReviewActions...)
	}
	if len(reviewItems) == 0 {
		reviewItems = []string{"Review evidence applicability and unresolved gaps."}
	}
	report := EvidenceAudit{
		AuditID:     audit.AuditID,
		ClaimAudits: claims,
		Summary: EvidenceAuditSummary{
			Conclusion:    "Deterministic audit completed within the pinned evidence scope.",
			VerdictCounts: counts, Limitations: limitations,
		},
		Proofroom: EvidenceAuditProofroomProjection{
			SchemaVersion: "proofroom-evidence-task.v1",
			Title:         "Review clinical evidence audit",
			ReviewItems:   evidenceAuditBoundedStrings(reviewItems),
		},
		TraceID: traceID,
	}
	fingerprint, err := evidenceAuditReportFingerprint(report)
	if err != nil {
		return nil, failEvidenceAuditRun(store, audit, pkg, retrieved, traceID, config, err)
	}
	if err := saveEvidenceAuditTrace(
		store, audit, pkg, retrieved, traceID, traceOutcome, fingerprint, config,
	); err != nil {
		return nil, failEvidenceAuditRun(store, audit, pkg, retrieved, traceID, config, err)
	}
	completed, err := CompleteEvidenceAudit(store, report, evidenceAuditRunnerNow(config))
	if err != nil {
		return nil, err
	}
	return completed, nil
}

func failEvidenceAuditRun(
	store *BookKnowledgeStore,
	audit EvidenceAudit,
	pkg any,
	retrieved []evidenceAuditRetrievedItem,
	traceID string,
	config EvidenceAuditRunnerConfig,
	runErr error,
) error {
	var packageValue AgentPackage
	if concrete, ok := pkg.(AgentPackage); ok {
		packageValue = concrete
	}
	if concrete, ok := pkg.(*AgentPackage); ok && concrete != nil {
		packageValue = *concrete
	}
	_ = saveEvidenceAuditTrace(
		store, audit, packageValue, retrieved, traceID, AgentTraceOutcomeFailed,
		sha256Fingerprint([]byte(evidenceAuditFailureCode(runErr))), config,
	)
	_, failErr := FailEvidenceAudit(
		store, audit.AuditID, evidenceAuditFailureCode(runErr), "Evidence audit failed closed.", evidenceAuditRunnerNow(config),
	)
	if failErr != nil {
		return fmt.Errorf("%v; persist failed audit: %w", runErr, failErr)
	}
	return runErr
}

func evidenceAuditFailureCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "model_timeout"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "content hash changed"), strings.Contains(message, "no longer matches"):
		return "release_changed"
	case strings.Contains(message, "json"), strings.Contains(message, "candidate verdict"):
		return "invalid_model_output"
	case strings.Contains(message, "citation"):
		return "unresolved_citation"
	default:
		return "runner_failed"
	}
}

func saveEvidenceAuditTrace(
	store *BookKnowledgeStore,
	audit EvidenceAudit,
	pkg AgentPackage,
	retrieved []evidenceAuditRetrievedItem,
	traceID, outcome, responseFingerprint string,
	config EvidenceAuditRunnerConfig,
) error {
	releases := make([]AgentTraceReleaseRef, 0, len(audit.Releases))
	for _, release := range audit.Releases {
		version := "unknown"
		if loaded, err := store.LoadKnowledgeRelease(release.ReleaseID); err == nil && strings.TrimSpace(loaded.Version) != "" {
			version = loaded.Version
		}
		releases = append(releases, AgentTraceReleaseRef{
			ReleaseID: release.ReleaseID, Version: version, ContentHash: release.ContentHash,
		})
	}
	retrievals := make([]AgentTraceRetrieval, 0, len(retrieved))
	citations := make([]AgentTraceCitation, 0, len(retrieved))
	seenEvidence := map[string]bool{}
	for _, item := range retrieved {
		evidenceID := agentRuntimeEvidenceID(item.Evidence) + ":" + item.Ref.CitationID
		if seenEvidence[evidenceID] {
			continue
		}
		seenEvidence[evidenceID] = true
		retrievals = append(retrievals, AgentTraceRetrieval{
			EvidenceID: evidenceID, ReleaseID: item.Ref.ReleaseID,
			Score: item.Evidence.Score, Rank: len(retrievals) + 1,
		})
		citations = append(citations, AgentTraceCitation{
			CitationID: item.Ref.CitationID, ReleaseID: item.Ref.ReleaseID, EvidenceID: evidenceID,
		})
	}
	if outcome != AgentTraceOutcomeCompleted {
		citations = nil
	}
	strategy := audit.Retrieval.Strategy
	route := AgentTraceRetrievalRoute{Strategy: strategy}
	if strategy == "vector" || strategy == "hybrid" {
		route.EmbeddingIdentity = agentPackageSemanticEmbedderIdentity(pkg.RetrievalPolicy)
		route.RerankerVersion = audit.Retrieval.RerankerVersion
	}
	model := audit.Model.Model
	capability := "evidence-audit"
	if strings.TrimSpace(pkg.ModelPolicy.PreferredCapability) != "" {
		capability = pkg.ModelPolicy.PreferredCapability
	}
	now := evidenceAuditRunnerNow(config)
	startedAt := now
	if parsed, err := time.Parse(time.RFC3339Nano, audit.StartedAt); err == nil {
		startedAt = parsed
	}
	trace := AgentTrace{
		SchemaVersion: AgentTraceSchemaVersion,
		TraceID:       traceID,
		Package: AgentTracePackageRef{
			PackageID: audit.Package.PackageID, Version: audit.Package.Version,
			ContentHash: audit.Package.ContentHash,
		},
		EvidenceAudit:  &AgentTraceEvidenceAuditRef{AuditID: audit.AuditID, InputHash: audit.InputHash},
		Releases:       releases,
		RetrievalRoute: route,
		Retrievals:     retrievals,
		ModelRoute: AgentTraceModelRoute{
			Provider: audit.Model.Provider, Model: model, Capability: capability,
		},
		ToolCalls: []AgentTraceToolCall{},
		Final: AgentTraceFinal{
			Outcome: outcome, ResponseFingerprint: responseFingerprint, Citations: citations,
		},
		StartedAt:   startedAt.UTC().Format(time.RFC3339Nano),
		CompletedAt: now.UTC().Format(time.RFC3339Nano),
	}
	return store.SaveAgentTrace(trace)
}

func evidenceAuditReportFingerprint(report EvidenceAudit) (string, error) {
	payload, err := json.Marshal(struct {
		Claims    []EvidenceAuditClaim             `json:"claims"`
		Summary   EvidenceAuditSummary             `json:"summary"`
		Proofroom EvidenceAuditProofroomProjection `json:"proofroom"`
	}{report.ClaimAudits, report.Summary, report.Proofroom})
	if err != nil {
		return "", err
	}
	return sha256Fingerprint(payload), nil
}

func evidenceAuditRequestsMedicalAdvice(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{
		"diagnose me", "diagnosis for me", "recommend a treatment", "individual treatment",
		"individual medical advice", "prescribe", "给我诊断", "诊断我", "治疗方案", "用药建议", "开药",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func evidenceAuditRunnerNow(config EvidenceAuditRunnerConfig) time.Time {
	if config.Now != nil {
		return config.Now().UTC()
	}
	return time.Now().UTC()
}
