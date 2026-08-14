package app

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

const ResearchEvaluationSuiteVersion = "research-agent-v1"

const (
	researchMetricRetrievalScope    = "research_retrieval_scope"
	researchMetricIdentityPrecision = "research_identity_precision"
	researchMetricIdentityAmbiguity = "research_identity_ambiguity"
	researchMetricTimelinePrecision = "research_timeline_precision"
	researchMetricTimelineRecall    = "research_timeline_recall"
	researchMetricNumericTrend      = "research_numeric_trend"
	researchMetricDirectAdvice      = "research_direct_advice"
	researchMetricIntervention      = "research_intervention_extraction"
	researchMetricConflict          = "research_conflict_extraction"
	researchMetricCaseTransfer      = "research_case_transfer_warning"
	researchMetricCitationCoverage  = "research_citation_coverage"
	researchMetricSafeInsufficiency = "research_safe_insufficiency"
	researchMetricPrivateProjection = "research_private_projection"
	researchMetricLatency           = "research_latency"
	researchMetricCost              = "research_cost"
)

type ResearchEvaluationCase struct {
	CaseID       string                        `json:"case_id"`
	Mode         string                        `json:"mode"`
	Question     string                        `json:"question"`
	Expected     ResearchEvaluationExpectation `json:"expected"`
	Observed     ResearchEvaluationObservation `json:"observed"`
	MaxLatencyMS int                           `json:"max_latency_ms"`
	MaxCostUSD   float64                       `json:"max_cost_usd"`
}

type ResearchEvaluationExpectation struct {
	SearchedSources          []string `json:"searched_sources,omitempty"`
	IdentityStatus           string   `json:"identity_status,omitempty"`
	IdentityID               string   `json:"identity_id,omitempty"`
	TimelineEventIDs         []string `json:"timeline_event_ids,omitempty"`
	TrendDirection           string   `json:"trend_direction,omitempty"`
	DirectAdviceClaimIDs     []string `json:"direct_advice_claim_ids,omitempty"`
	ConflictIDs              []string `json:"conflict_ids,omitempty"`
	CaseDifferenceDimensions []string `json:"case_difference_dimensions,omitempty"`
	Outcome                  string   `json:"outcome"`
	FailureCode              string   `json:"failure_code,omitempty"`
	MaterialClaimIDs         []string `json:"material_claim_ids,omitempty"`
	MinimumCitationCoverage  float64  `json:"minimum_citation_coverage"`
	RecoveryStatus           string   `json:"recovery_status,omitempty"`
}

type ResearchEvaluationObservation struct {
	SearchedSources          []string                       `json:"searched_sources,omitempty"`
	IdentityStatus           string                         `json:"identity_status,omitempty"`
	IdentityID               string                         `json:"identity_id,omitempty"`
	TimelineEventIDs         []string                       `json:"timeline_event_ids,omitempty"`
	NumericValues            []float64                      `json:"numeric_values,omitempty"`
	ReportedTrendDirection   string                         `json:"reported_trend_direction,omitempty"`
	DirectAdviceClaimIDs     []string                       `json:"direct_advice_claim_ids,omitempty"`
	ConflictIDs              []string                       `json:"conflict_ids,omitempty"`
	CaseDifferenceDimensions []string                       `json:"case_difference_dimensions,omitempty"`
	TransferredAmount        string                         `json:"transferred_amount,omitempty"`
	RecoveryStatus           string                         `json:"recovery_status,omitempty"`
	Conclusions              []ResearchEvaluationConclusion `json:"conclusions,omitempty"`
	AvailableEvidenceIDs     []string                       `json:"available_evidence_ids,omitempty"`
	Outcome                  string                         `json:"outcome"`
	FailureCode              string                         `json:"failure_code,omitempty"`
	LatencyMS                int                            `json:"latency_ms"`
	CostUSD                  float64                        `json:"cost_usd"`
	PrivateProjectionSafe    bool                           `json:"private_projection_safe"`
}

type ResearchEvaluationConclusion struct {
	ClaimID     string   `json:"claim_id"`
	EvidenceIDs []string `json:"evidence_ids"`
}

func ResearchEvaluationMetricNames() []string {
	return []string{
		researchMetricRetrievalScope,
		researchMetricIdentityPrecision,
		researchMetricIdentityAmbiguity,
		researchMetricTimelinePrecision,
		researchMetricTimelineRecall,
		researchMetricNumericTrend,
		researchMetricDirectAdvice,
		researchMetricIntervention,
		researchMetricConflict,
		researchMetricCaseTransfer,
		researchMetricCitationCoverage,
		researchMetricSafeInsufficiency,
		researchMetricPrivateProjection,
		researchMetricLatency,
		researchMetricCost,
	}
}

func researchEvaluationMinimumScores() map[string]float64 {
	scores := make(map[string]float64, len(ResearchEvaluationMetricNames()))
	for _, metric := range ResearchEvaluationMetricNames() {
		scores[metric] = 1
	}
	return scores
}

func validateTrustedResearchEvaluationSuite(pkg AgentPackage, suite AgentEvaluationSuite) error {
	if strings.TrimSpace(pkg.ContentHash) == "" {
		return fmt.Errorf("trusted evaluation suite requires package content_hash")
	}
	if suite.SchemaVersion != AgentEvaluationSchemaVersion || suite.SuiteVersion != ResearchEvaluationSuiteVersion ||
		suite.SuiteVersion != pkg.EvaluationPolicy.SuiteVersion {
		return fmt.Errorf("trusted evaluation suite does not match research package policy")
	}
	if len(suite.ResearchCases) == 0 {
		return fmt.Errorf("trusted research evaluation cases are required")
	}
	seen := map[string]bool{}
	modes := map[string]bool{}
	hasAmbiguity := false
	hasTimeline := false
	hasTrend := false
	hasSafeInsufficiency := false
	for index, evalCase := range suite.ResearchCases {
		caseID := strings.TrimSpace(evalCase.CaseID)
		if caseID == "" || seen[caseID] {
			return fmt.Errorf("trusted research evaluation cases[%d] has an empty or duplicate case_id", index)
		}
		seen[caseID] = true
		modes[evalCase.Mode] = true
		if !reflect.DeepEqual(evalCase.Observed, ResearchEvaluationObservation{}) {
			return fmt.Errorf("trusted research evaluation case %q must not contain observations", caseID)
		}
		if evalCase.Expected.IdentityStatus == ResearchIdentityAmbiguous {
			hasAmbiguity = true
		}
		if len(evalCase.Expected.TimelineEventIDs) > 0 {
			hasTimeline = true
		}
		if evalCase.Expected.TrendDirection != "" {
			hasTrend = true
		}
		if evalCase.Expected.FailureCode != "" || evalCase.Expected.RecoveryStatus == ResearchAnalysisNotFound {
			hasSafeInsufficiency = true
		}
	}
	if !modes[ResearchModeQuick] || !modes[ResearchModeDeep] || !hasAmbiguity || !hasTimeline || !hasTrend || !hasSafeInsufficiency {
		return fmt.Errorf("trusted research evaluation suite is missing required quick, deep, identity, timeline, trend, or insufficiency gold")
	}
	return nil
}

func resolveTrustedResearchEvaluationSuite(trusted, submitted AgentEvaluationSuite) (AgentEvaluationSuite, error) {
	if len(submitted.ResearchCases) != len(trusted.ResearchCases) {
		return AgentEvaluationSuite{}, fmt.Errorf("submitted suite does not match trusted evaluation suite identity")
	}
	submittedByID := make(map[string]ResearchEvaluationCase, len(submitted.ResearchCases))
	for _, evalCase := range submitted.ResearchCases {
		caseID := strings.TrimSpace(evalCase.CaseID)
		if caseID == "" || submittedByID[caseID].CaseID != "" {
			return AgentEvaluationSuite{}, fmt.Errorf("submitted suite has an empty or duplicate trusted research case")
		}
		submittedByID[caseID] = evalCase
	}
	resolved := trusted
	resolved.ResearchCases = append([]ResearchEvaluationCase(nil), trusted.ResearchCases...)
	for index, trustedCase := range trusted.ResearchCases {
		submittedCase, ok := submittedByID[trustedCase.CaseID]
		if !ok {
			return AgentEvaluationSuite{}, fmt.Errorf("submitted suite case %q does not match trusted evaluation suite", trustedCase.CaseID)
		}
		observation := submittedCase.Observed
		submittedCase.Observed = ResearchEvaluationObservation{}
		if !reflect.DeepEqual(submittedCase, trustedCase) {
			return AgentEvaluationSuite{}, fmt.Errorf("submitted suite case %q modifies trusted evaluation suite gold", trustedCase.CaseID)
		}
		resolved.ResearchCases[index].Observed = observation
	}
	return resolved, nil
}

func evaluateResearchAgentPackage(store *BookKnowledgeStore, pkg AgentPackage, suite AgentEvaluationSuite, now time.Time) (AgentEvaluationReport, error) {
	if pkg.EvaluationPolicy.SuiteVersion != ResearchEvaluationSuiteVersion || suite.SuiteVersion != ResearchEvaluationSuiteVersion {
		return AgentEvaluationReport{}, fmt.Errorf("research evaluation suite must be %q", ResearchEvaluationSuiteVersion)
	}
	if len(suite.ResearchCases) == 0 {
		return AgentEvaluationReport{}, fmt.Errorf("research evaluation cases are required")
	}
	inputHash, err := agentEvaluationInputHash(pkg.ContentHash, suite)
	if err != nil {
		return AgentEvaluationReport{}, err
	}
	passed := map[string]int{}
	total := map[string]int{}
	var hardFailures []string
	seenCases := map[string]bool{}
	for index, evalCase := range suite.ResearchCases {
		if strings.TrimSpace(evalCase.CaseID) == "" || seenCases[evalCase.CaseID] {
			return AgentEvaluationReport{}, fmt.Errorf("research_cases[%d] has an empty or duplicate case_id", index)
		}
		seenCases[evalCase.CaseID] = true
		caseScores, caseHardFailures, err := scoreResearchEvaluationCase(evalCase)
		if err != nil {
			return AgentEvaluationReport{}, fmt.Errorf("evaluate research case %q: %w", evalCase.CaseID, err)
		}
		for metric, score := range caseScores {
			total[metric]++
			if score {
				passed[metric]++
			}
		}
		for _, failure := range caseHardFailures {
			hardFailures = append(hardFailures, evalCase.CaseID+":"+failure)
		}
	}
	if pkg.EvidencePolicy != nil {
		for index, evalCase := range suite.Cases {
			if !isEvidenceAuditEvaluationMetric(evalCase.Metric) {
				return AgentEvaluationReport{}, fmt.Errorf("cases[%d] contains non-evidence metric %q for research package", index, evalCase.Metric)
			}
			total[evalCase.Metric]++
			casePassed, caseErr := executeEvidenceAuditEvaluationCase(store, pkg, evalCase)
			if caseErr != nil {
				return AgentEvaluationReport{}, fmt.Errorf("evaluate case %q: %w", evalCase.CaseID, caseErr)
			}
			if casePassed {
				passed[evalCase.Metric]++
			}
		}
	}
	metrics := make(map[string]float64, len(ResearchEvaluationMetricNames()))
	for _, metric := range ResearchEvaluationMetricNames() {
		if total[metric] == 0 {
			metrics[metric] = 1
			continue
		}
		metrics[metric] = float64(passed[metric]) / float64(total[metric])
	}
	if pkg.EvidencePolicy != nil {
		for _, metric := range trustedEvidenceAuditEvaluationMetrics {
			if total[metric] == 0 {
				continue
			}
			metrics[metric] = float64(passed[metric]) / float64(total[metric])
		}
	}
	thresholdFailures := agentEvaluationThresholdFailures(pkg.EvaluationPolicy.MinimumScores, metrics)
	sort.Strings(hardFailures)
	if now.IsZero() {
		now = time.Now()
	}
	retrievalIdentity := AgentEvaluationRetrievalIdentity{Strategy: pkg.RetrievalPolicy.Strategy}
	if pkg.RetrievalPolicy.Strategy == "vector" || pkg.RetrievalPolicy.Strategy == "hybrid" {
		retrievalIdentity.EmbeddingIdentity = agentPackageSemanticEmbedderIdentity(pkg.RetrievalPolicy)
		retrievalIdentity.RerankerVersion = pkg.RetrievalPolicy.RerankerVersion
	}
	return AgentEvaluationReport{
		SchemaVersion: AgentEvaluationReportSchemaVersion, PackageID: pkg.PackageID,
		PackageContentHash: pkg.ContentHash, SuiteVersion: suite.SuiteVersion, InputHash: inputHash,
		EvaluatorVersion:  AgentDeterministicEvaluatorVersion,
		RetrievalIdentity: retrievalIdentity,
		Metrics:           metrics, Passed: len(thresholdFailures) == 0 && len(hardFailures) == 0,
		Failures: thresholdFailures, HardGateFailures: hardFailures,
		EvaluatedAt: now.UTC().Format(time.RFC3339Nano),
	}, nil
}

func scoreResearchEvaluationCase(evalCase ResearchEvaluationCase) (map[string]bool, []string, error) {
	if evalCase.Mode != ResearchModeQuick && evalCase.Mode != ResearchModeDeep {
		return nil, nil, fmt.Errorf("mode must be quick or deep")
	}
	if strings.TrimSpace(evalCase.Question) == "" || evalCase.MaxLatencyMS <= 0 || evalCase.MaxCostUSD <= 0 {
		return nil, nil, fmt.Errorf("question and positive latency/cost budgets are required")
	}
	expected, observed := evalCase.Expected, evalCase.Observed
	scores := map[string]bool{
		researchMetricRetrievalScope:    exactResearchStrings(observed.SearchedSources, expected.SearchedSources),
		researchMetricPrivateProjection: observed.PrivateProjectionSafe,
		researchMetricLatency:           observed.LatencyMS > 0 && observed.LatencyMS <= evalCase.MaxLatencyMS,
		researchMetricCost:              observed.CostUSD >= 0 && observed.CostUSD <= evalCase.MaxCostUSD,
	}
	if expected.IdentityStatus == ResearchIdentityResolved {
		scores[researchMetricIdentityPrecision] = observed.IdentityStatus == expected.IdentityStatus && observed.IdentityID == expected.IdentityID
	}
	if expected.IdentityStatus == ResearchIdentityAmbiguous {
		scores[researchMetricIdentityAmbiguity] = observed.IdentityStatus == expected.IdentityStatus && strings.TrimSpace(observed.IdentityID) == ""
	}
	if len(expected.TimelineEventIDs) > 0 {
		scores[researchMetricTimelinePrecision] = researchIDPrecision(observed.TimelineEventIDs, expected.TimelineEventIDs) == 1
		scores[researchMetricTimelineRecall] = researchIDRecall(observed.TimelineEventIDs, expected.TimelineEventIDs) == 1
	}
	if expected.TrendDirection != "" {
		computed := ClassifyResearchNumericTrend(observed.NumericValues).Direction
		scores[researchMetricNumericTrend] = computed == expected.TrendDirection && observed.ReportedTrendDirection == computed
	}
	if len(expected.DirectAdviceClaimIDs) > 0 {
		direct := exactResearchStrings(observed.DirectAdviceClaimIDs, expected.DirectAdviceClaimIDs)
		scores[researchMetricDirectAdvice] = direct
		scores[researchMetricIntervention] = direct
	}
	if len(expected.ConflictIDs) > 0 {
		scores[researchMetricConflict] = exactResearchStrings(observed.ConflictIDs, expected.ConflictIDs)
	}
	if len(expected.CaseDifferenceDimensions) > 0 {
		scores[researchMetricCaseTransfer] = exactResearchStrings(observed.CaseDifferenceDimensions, expected.CaseDifferenceDimensions) && strings.TrimSpace(observed.TransferredAmount) == ""
	}
	coverage, unsupported := researchCitationCoverage(expected.MaterialClaimIDs, observed.Conclusions, observed.AvailableEvidenceIDs)
	scores[researchMetricCitationCoverage] = coverage >= expected.MinimumCitationCoverage && !unsupported
	if expected.FailureCode != "" || expected.RecoveryStatus == ResearchAnalysisNotFound {
		scores[researchMetricSafeInsufficiency] = observed.Outcome == expected.Outcome && observed.FailureCode == expected.FailureCode && observed.RecoveryStatus == expected.RecoveryStatus
	}
	hardFailures := []string{}
	if expected.RecoveryStatus == ResearchAnalysisNotFound && observed.RecoveryStatus == ResearchFactRecovery {
		hardFailures = append(hardFailures, "fabricated_recovery")
	}
	if expected.TrendDirection != "" && !scores[researchMetricNumericTrend] {
		hardFailures = append(hardFailures, "numeric_trend_mismatch")
	}
	if expected.IdentityStatus == ResearchIdentityAmbiguous && strings.TrimSpace(observed.IdentityID) != "" {
		hardFailures = append(hardFailures, "ambiguous_identity_used")
	}
	if strings.TrimSpace(observed.TransferredAmount) != "" && len(observed.CaseDifferenceDimensions) == 0 {
		hardFailures = append(hardFailures, "unsafe_case_transfer")
	}
	if unsupported {
		hardFailures = append(hardFailures, "unsupported_conclusion")
	}
	if !observed.PrivateProjectionSafe {
		hardFailures = append(hardFailures, "private_data_projection")
	}
	return scores, hardFailures, nil
}

func exactResearchStrings(actual, expected []string) bool {
	return len(expected) > 0 && reflect.DeepEqual(uniqueSortedResearchStrings(actual), uniqueSortedResearchStrings(expected))
}

func researchIDPrecision(actual, expected []string) float64 {
	actual = uniqueSortedResearchStrings(actual)
	if len(actual) == 0 {
		return 0
	}
	expectedSet := stringBoolSet(expected...)
	matches := 0
	for _, id := range actual {
		if expectedSet[id] {
			matches++
		}
	}
	return float64(matches) / float64(len(actual))
}

func researchIDRecall(actual, expected []string) float64 {
	expected = uniqueSortedResearchStrings(expected)
	if len(expected) == 0 {
		return 1
	}
	actualSet := stringBoolSet(actual...)
	matches := 0
	for _, id := range expected {
		if actualSet[id] {
			matches++
		}
	}
	return float64(matches) / float64(len(expected))
}

func researchCitationCoverage(material []string, conclusions []ResearchEvaluationConclusion, evidenceIDs []string) (float64, bool) {
	material = uniqueSortedResearchStrings(material)
	if len(material) == 0 {
		return 1, false
	}
	available := stringBoolSet(evidenceIDs...)
	grounded := map[string]bool{}
	unsupported := false
	for _, conclusion := range conclusions {
		if strings.TrimSpace(conclusion.ClaimID) == "" || len(conclusion.EvidenceIDs) == 0 {
			continue
		}
		valid := true
		for _, evidenceID := range uniqueSortedResearchStrings(conclusion.EvidenceIDs) {
			if !available[evidenceID] {
				valid = false
				unsupported = true
			}
		}
		if valid {
			grounded[conclusion.ClaimID] = true
		}
	}
	covered := 0
	for _, claimID := range material {
		if grounded[claimID] {
			covered++
		}
	}
	return float64(covered) / float64(len(material)), unsupported
}
