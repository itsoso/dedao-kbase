package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type EvolutionEvaluationEvidence struct {
	HardGates              map[string]bool    `json:"hard_gates"`
	BaselineMetrics        map[string]float64 `json:"baseline_metrics"`
	CandidateMetrics       map[string]float64 `json:"candidate_metrics"`
	FailureCaseRefs        []string           `json:"failure_case_refs"`
	ComponentContributions map[string]float64 `json:"component_contributions,omitempty"`
	SuiteIdentity          string             `json:"suite_identity"`
}

type EvolutionEvaluationConfig struct {
	ControlStore   *EvolutionControlStore
	KnowledgeStore *BookKnowledgeStore
	SuiteVersion   string
	ScorerVersion  string
	WeightVersion  string
	MetricWeights  map[string]float64
	Evaluate       func(context.Context, EvolutionRun, EvolutionCandidate, []byte) (EvolutionEvaluationEvidence, error)
}

type EvolutionEvaluationService struct {
	control       *EvolutionControlStore
	knowledge     *BookKnowledgeStore
	suiteVersion  string
	scorerVersion string
	weightVersion string
	metricWeights map[string]float64
	evaluate      func(context.Context, EvolutionRun, EvolutionCandidate, []byte) (EvolutionEvaluationEvidence, error)
}

type EvolutionEvaluationResult struct {
	Scorecard         *EvolutionScorecard `json:"scorecard"`
	RunStatus         EvolutionRunStatus  `json:"run_status"`
	ResultArtifactRef string              `json:"result_artifact_ref"`
}

func NewEvolutionEvaluationService(config EvolutionEvaluationConfig) (*EvolutionEvaluationService, error) {
	if config.ControlStore == nil || config.KnowledgeStore == nil {
		return nil, fmt.Errorf("evolution control and knowledge stores are required")
	}
	config.SuiteVersion = strings.TrimSpace(config.SuiteVersion)
	config.ScorerVersion = strings.TrimSpace(config.ScorerVersion)
	config.WeightVersion = strings.TrimSpace(config.WeightVersion)
	if config.WeightVersion == "" {
		config.WeightVersion = DefaultEvolutionWeightVersion
	}
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "suite_version", value: config.SuiteVersion},
		evolutionStringField{name: "scorer_version", value: config.ScorerVersion},
		evolutionStringField{name: "weight_version", value: config.WeightVersion},
	); err != nil {
		return nil, err
	}
	if len(config.MetricWeights) == 0 {
		config.MetricWeights = DefaultEvolutionMetricWeights
	}
	if config.Evaluate == nil {
		config.Evaluate = func(ctx context.Context, run EvolutionRun, candidate EvolutionCandidate, payload []byte) (EvolutionEvaluationEvidence, error) {
			return defaultEvolutionEvaluation(ctx, config.KnowledgeStore, run, candidate, payload)
		}
	}
	return &EvolutionEvaluationService{
		control: config.ControlStore, knowledge: config.KnowledgeStore,
		suiteVersion: config.SuiteVersion, scorerVersion: config.ScorerVersion,
		weightVersion: config.WeightVersion, metricWeights: cloneEvolutionMetrics(config.MetricWeights),
		evaluate: config.Evaluate,
	}, nil
}

func (service *EvolutionEvaluationService) Evaluate(ctx context.Context, work EvolutionWork) (*EvolutionEvaluationResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("evaluation context is required")
	}
	if work.Capability != EvolutionCapabilityEvaluation || work.Attempt < 1 {
		return nil, fmt.Errorf("evaluation work must hold an evaluation lease")
	}
	run, err := service.control.LoadRunContext(ctx, work.RunID)
	if err != nil {
		return nil, err
	}
	if (run.Status == EvolutionAwaitingApproval || run.Status == EvolutionBlocked) && run.CurrentCandidateID != "" {
		scorecard, loadErr := service.control.LoadEvolutionScorecardForCandidate(run.CurrentCandidateID)
		if loadErr != nil {
			return nil, loadErr
		}
		return newEvolutionEvaluationResult(scorecard, run.Status), nil
	}
	if run.Status != EvolutionEvaluating || run.CurrentCandidateID == "" {
		return nil, fmt.Errorf("%w: evaluation requires an evaluating run with a candidate", ErrEvolutionTransitionConflict)
	}
	candidate, payload, err := service.control.LoadEvolutionCandidate(run.CurrentCandidateID)
	if err != nil {
		return nil, err
	}
	evidence, err := service.evaluate(ctx, *run, *candidate, payload)
	if err != nil {
		return nil, fmt.Errorf("deterministic evolution evaluation failed: %w", err)
	}
	if err := validateEvolutionEvaluationEvidence(*run, *candidate, evidence); err != nil {
		return nil, err
	}
	scorecard, _, err := service.control.SaveEvolutionScorecard(
		"sha256:"+evolutionWorkerPayloadHash("scorecard:"+candidate.ContentHash+":"+service.suiteVersion+":"+service.scorerVersion+":"+service.weightVersion+":"+evidence.SuiteIdentity),
		EvolutionScorecardInput{
			CandidateID: candidate.CandidateID, BaselineIdentity: candidate.BaselineIdentity,
			SuiteVersion: service.suiteVersion, ScorerVersion: service.scorerVersion, WeightVersion: service.weightVersion,
			SuiteIdentity: evidence.SuiteIdentity,
			HardGates:     evidence.HardGates, BaselineMetrics: evidence.BaselineMetrics,
			CandidateMetrics: evidence.CandidateMetrics, MetricWeights: service.metricWeights,
			ComponentContributions: evidence.ComponentContributions, FailureCaseRefs: evidence.FailureCaseRefs,
		},
	)
	if err != nil {
		return nil, err
	}
	artifactRef := "scorecard:" + scorecard.ScorecardID
	completion := EvolutionWorkCompletion{
		WorkID: work.WorkID, WorkerID: work.WorkerID, LeaseID: work.LeaseID, Attempt: work.Attempt,
		ResultIdempotencyKey: evolutionWorkResultIdempotencyKey(work, artifactRef), ResultArtifactRef: artifactRef,
	}
	targetStatus := EvolutionAwaitingApproval
	transitionCode := "scorecard_passed"
	transitionMessage := "candidate met the minimum deterministic gain and awaits human approval"
	if scorecard.Decision != EvolutionScorecardAwaitingApproval {
		targetStatus = EvolutionBlocked
		transitionCode = "scorecard_blocked"
		transitionMessage = "candidate was automatically stopped by deterministic evaluation"
	}
	if _, _, _, err := service.control.FinalizeEvolutionEvaluation(completion, targetStatus, EvolutionTransitionInput{
		Actor: service.scorerVersion, Code: transitionCode, Message: transitionMessage, ArtifactRefs: []string{artifactRef},
	}); err != nil {
		return nil, err
	}
	return newEvolutionEvaluationResult(scorecard, targetStatus), nil
}

func newEvolutionEvaluationResult(scorecard *EvolutionScorecard, status EvolutionRunStatus) *EvolutionEvaluationResult {
	return &EvolutionEvaluationResult{Scorecard: scorecard, RunStatus: status, ResultArtifactRef: "scorecard:" + scorecard.ScorecardID}
}

func validateEvolutionEvaluationEvidence(run EvolutionRun, candidate EvolutionCandidate, evidence EvolutionEvaluationEvidence) error {
	if err := validateEvolutionHardGates(evidence.HardGates); err != nil {
		return err
	}
	if err := validateEvolutionReferences("failure_case_refs", evidence.FailureCaseRefs, false); err != nil {
		return err
	}
	if err := validateEvolutionReference("suite_identity", evidence.SuiteIdentity); err != nil {
		return err
	}
	if run.RunType == EvolutionRunCombined {
		if candidate.CandidateType != EvolutionCandidateCombined {
			return fmt.Errorf("combined evaluation requires a combined candidate artifact")
		}
		for _, component := range []string{"agent", "knowledge"} {
			value, found := evidence.ComponentContributions[component]
			if !found || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("combined evaluation requires finite %s contribution", component)
			}
		}
	}
	return nil
}

func defaultEvolutionEvaluation(ctx context.Context, store *BookKnowledgeStore, run EvolutionRun, candidate EvolutionCandidate, payload []byte) (EvolutionEvaluationEvidence, error) {
	var envelope evolutionCandidateArtifact
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	switch candidate.CandidateType {
	case EvolutionCandidateAgentCompilation:
		return evaluateEvolutionAgentCompilation(ctx, store, run, envelope.Artifact)
	case EvolutionCandidateKnowledgeRelease:
		return evaluateEvolutionKnowledgeCandidate(store, run, envelope.Artifact)
	case EvolutionCandidateCombined:
		return evaluateEvolutionCombinedCandidate(ctx, store, run, envelope.Artifact)
	default:
		return EvolutionEvaluationEvidence{}, fmt.Errorf("unsupported evolution candidate type %q", candidate.CandidateType)
	}
}

func evaluateEvolutionAgentCompilation(ctx context.Context, store *BookKnowledgeStore, run EvolutionRun, artifact []byte) (EvolutionEvaluationEvidence, error) {
	var compilation AgentCompilation
	if err := json.Unmarshal(artifact, &compilation); err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	if err := ValidateAgentCompilation(compilation); err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	var candidatePackage *AgentPackage
	for index := range compilation.Candidates {
		if compilation.Candidates[index].Status == AgentCompilationCandidateReady && compilation.Candidates[index].Package != nil {
			candidatePackage = compilation.Candidates[index].Package
			break
		}
	}
	if candidatePackage == nil {
		return EvolutionEvaluationEvidence{}, fmt.Errorf("agent compilation contains no ready candidate")
	}
	baselinePackage, err := store.LoadAgentPackageContext(ctx, run.PackageID, run.BaselinePackageVersion)
	if err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	suite, err := store.LoadTrustedAgentEvaluationSuite(*baselinePackage)
	if err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	baselineReport, err := EvaluateAgentPackageDeterministically(store, *baselinePackage, *suite, timeFromEvolutionRun(run))
	if err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	candidateReport, err := EvaluateAgentPackageDeterministically(store, *candidatePackage, *suite, timeFromEvolutionRun(run))
	if err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	baselineMetrics := evolutionMetricsFromAgentReport(baselineReport)
	candidateMetrics := evolutionMetricsFromAgentReport(candidateReport)
	privacyPassed, privacyIdentity, privacyRefs := evaluateEvolutionCandidatePrivacy(compilation)
	suitePayload, err := encodeJSONFile(*suite)
	if err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	suiteIdentity := combinedEvolutionEvidenceIdentity(sha256Fingerprint(suitePayload), privacyIdentity)
	evidence := EvolutionEvaluationEvidence{
		HardGates:       map[string]bool{"trusted_suite": candidateReport.Passed, "citations": candidateMetrics["evidence_quality"] > 0, "privacy": privacyPassed},
		BaselineMetrics: baselineMetrics, CandidateMetrics: candidateMetrics,
		FailureCaseRefs:        append(evolutionFailureRefs(candidateReport.Failures), privacyRefs...),
		ComponentContributions: map[string]float64{"agent": weightedEvolutionScore(candidateMetrics, DefaultEvolutionMetricWeights) - weightedEvolutionScore(baselineMetrics, DefaultEvolutionMetricWeights)},
		SuiteIdentity:          suiteIdentity,
	}
	return evidence, nil
}

func evaluateEvolutionKnowledgeCandidate(store *BookKnowledgeStore, run EvolutionRun, artifact []byte) (EvolutionEvaluationEvidence, error) {
	var snapshot EvolutionKnowledgeCandidateArtifact
	if err := json.Unmarshal(artifact, &snapshot); err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	if err := validateEvolutionKnowledgeSnapshot(snapshot); err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	baseline := evolutionKnowledgeSnapshotBaselineMetrics(snapshot)
	candidateScore := evolutionKnowledgeDecisionScore(snapshot.Task.QualityDecision)
	candidateMetrics := uniformEvolutionMetrics(candidateScore)
	privacyPassed, privacyIdentity, privacyRefs := evaluateEvolutionCandidatePrivacy(snapshot)
	return EvolutionEvaluationEvidence{
		HardGates:       map[string]bool{"candidate_quality": snapshot.Task.QualityDecision == BookQualityPass, "citations": snapshot.Task.CandidateAnalysisHash != "", "privacy": privacyPassed},
		BaselineMetrics: baseline, CandidateMetrics: candidateMetrics, FailureCaseRefs: privacyRefs,
		ComponentContributions: map[string]float64{"knowledge": weightedEvolutionScore(candidateMetrics, DefaultEvolutionMetricWeights) - weightedEvolutionScore(baseline, DefaultEvolutionMetricWeights)},
		SuiteIdentity:          combinedEvolutionEvidenceIdentity(snapshot.SnapshotIdentity, privacyIdentity),
	}, nil
}

func evaluateEvolutionCombinedCandidate(ctx context.Context, store *BookKnowledgeStore, run EvolutionRun, artifact []byte) (EvolutionEvaluationEvidence, error) {
	var combined EvolutionCombinedCandidateArtifact
	if err := json.Unmarshal(artifact, &combined); err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	if combined.SchemaVersion != evolutionCombinedCandidateSchema {
		return EvolutionEvaluationEvidence{}, fmt.Errorf("combined candidate schema is invalid")
	}
	agentPayload, err := json.Marshal(combined.Agent)
	if err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	knowledgePayload, err := json.Marshal(combined.Knowledge)
	if err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	agent, err := evaluateEvolutionAgentCompilation(ctx, store, run, agentPayload)
	if err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	knowledge, err := evaluateEvolutionKnowledgeCandidate(store, run, knowledgePayload)
	if err != nil {
		return EvolutionEvaluationEvidence{}, err
	}
	result := EvolutionEvaluationEvidence{
		HardGates: map[string]bool{}, BaselineMetrics: map[string]float64{}, CandidateMetrics: map[string]float64{},
		FailureCaseRefs: append(append([]string{}, agent.FailureCaseRefs...), knowledge.FailureCaseRefs...),
		ComponentContributions: map[string]float64{
			"agent": agent.ComponentContributions["agent"], "knowledge": knowledge.ComponentContributions["knowledge"],
		},
		SuiteIdentity: combinedEvolutionEvidenceIdentity(agent.SuiteIdentity, knowledge.SuiteIdentity),
	}
	for name, passed := range agent.HardGates {
		result.HardGates["agent_"+name] = passed
	}
	for name, passed := range knowledge.HardGates {
		result.HardGates["knowledge_"+name] = passed
	}
	for metric := range DefaultEvolutionMetricWeights {
		result.BaselineMetrics[metric] = roundEvolutionScore((agent.BaselineMetrics[metric] + knowledge.BaselineMetrics[metric]) / 2)
		result.CandidateMetrics[metric] = roundEvolutionScore((agent.CandidateMetrics[metric] + knowledge.CandidateMetrics[metric]) / 2)
	}
	return result, nil
}

func validateEvolutionKnowledgeSnapshot(snapshot EvolutionKnowledgeCandidateArtifact) error {
	if snapshot.SchemaVersion != evolutionKnowledgeCandidateSchema || snapshot.Task.Status != KnowledgeReverificationCandidateReady {
		return fmt.Errorf("knowledge candidate snapshot is not ready")
	}
	if snapshot.Task.ReleaseID != snapshot.FeedbackAssessment.ReleaseID ||
		snapshot.Task.AssessmentFingerprint != snapshot.FeedbackAssessment.ReverificationFingerprint {
		return fmt.Errorf("knowledge candidate snapshot evidence does not match")
	}
	if !snapshot.FeedbackAssessment.ReverifyRequired || snapshot.Task.ReleaseContentHash == "" ||
		snapshot.Task.CandidateContentHash == "" || snapshot.Task.CandidateAnalysisHash == "" ||
		snapshot.Task.ReleaseContentHash != snapshot.BaselineQuality.ContentHash {
		return fmt.Errorf("knowledge candidate snapshot evidence is incomplete")
	}
	identity := snapshot.SnapshotIdentity
	snapshot.SnapshotIdentity = ""
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	expected := "sha256:" + evolutionWorkerPayloadHash(string(payload))
	if identity != expected {
		return fmt.Errorf("knowledge candidate snapshot identity does not match")
	}
	return nil
}

func evolutionKnowledgeSnapshotBaselineMetrics(snapshot EvolutionKnowledgeCandidateArtifact) map[string]float64 {
	metrics := uniformEvolutionMetrics(evolutionKnowledgeQualityScore(snapshot.BaselineQuality))
	if snapshot.FeedbackAssessment.ReverifyRequired {
		metrics["reliability"] = math.Min(metrics["reliability"], 50)
		metrics["evidence_quality"] = math.Min(metrics["evidence_quality"], 75)
	}
	return metrics
}

func evolutionKnowledgeQualityScore(report BookQualityReport) float64 {
	if len(report.Rules) == 0 {
		return evolutionKnowledgeDecisionScore(report.Decision)
	}
	passed := 0
	for _, rule := range report.Rules {
		if rule.Passed {
			passed++
		}
	}
	return roundEvolutionScore(float64(passed) * 100 / float64(len(report.Rules)))
}

func evolutionKnowledgeDecisionScore(decision string) float64 {
	switch decision {
	case BookQualityPass:
		return 100
	case BookQualityQuarantine:
		return 50
	default:
		return 0
	}
}

func evolutionMetricsFromAgentReport(report AgentEvaluationReport) map[string]float64 {
	average := 0.0
	if len(report.Metrics) > 0 {
		for _, value := range report.Metrics {
			average += value * 100
		}
		average /= float64(len(report.Metrics))
	}
	metric := func(names ...string) float64 {
		values := make([]float64, 0, len(names))
		for _, name := range names {
			if value, found := report.Metrics[name]; found {
				values = append(values, value*100)
			}
		}
		if len(values) == 0 {
			return roundEvolutionScore(average)
		}
		total := 0.0
		for _, value := range values {
			total += value
		}
		return roundEvolutionScore(total / float64(len(values)))
	}
	return map[string]float64{
		"answer_quality":   metric("faithfulness", "abstention", "safe_insufficiency"),
		"evidence_quality": metric("citations", "retrieval", "retrieval_precision", "report_citation_completeness"),
		"task_completion":  roundEvolutionScore(average), "reliability": roundEvolutionScore(average),
		"cost": metric("cost"), "latency": metric("latency"),
	}
}

func uniformEvolutionMetrics(value float64) map[string]float64 {
	metrics := make(map[string]float64, len(DefaultEvolutionMetricWeights))
	for metric := range DefaultEvolutionMetricWeights {
		metrics[metric] = value
	}
	return metrics
}

func evolutionFailureRefs(failures []string) []string {
	refs := make([]string, 0, len(failures))
	for _, failure := range failures {
		refs = append(refs, "artifact:sha256:"+evolutionWorkerPayloadHash(failure))
	}
	sort.Strings(refs)
	return refs
}

func evaluateEvolutionCandidatePrivacy(artifact any) (bool, string, []string) {
	payload, err := json.Marshal(artifact)
	if err != nil {
		return false, "sha256:" + evolutionWorkerPayloadHash("privacy-audit-encode-failed"), []string{"artifact:sha256:" + evolutionWorkerPayloadHash("privacy-audit-encode-failed")}
	}
	identity := "sha256:" + evolutionWorkerPayloadHash("privacy-audit.v1:"+string(payload))
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return false, identity, []string{"artifact:sha256:" + evolutionWorkerPayloadHash("privacy-audit-decode-failed")}
	}
	violations := make([]string, 0)
	var inspect func(any, string)
	inspect = func(value any, key string) {
		switch current := value.(type) {
		case map[string]any:
			for childKey, child := range current {
				inspect(child, childKey)
			}
		case []any:
			for _, child := range current {
				inspect(child, key)
			}
		case string:
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			lowerValue := strings.ToLower(strings.TrimSpace(current))
			probe := lowerKey + "=" + current
			credentialEvidence := sanitizeEvidenceAuditHTTPLogCause(probe) != probe
			privateEvidence := legacyBookKnowledgeJobTextContainsSensitiveData(current) ||
				strings.Contains(lowerValue, "kbase_auth_token") || strings.Contains(lowerValue, "kbase_source_agent_token")
			if credentialEvidence || privateEvidence {
				violations = append(violations, lowerKey)
			}
		}
	}
	inspect(decoded, "")
	if len(violations) == 0 {
		return true, identity, nil
	}
	sort.Strings(violations)
	return false, identity, []string{"artifact:sha256:" + evolutionWorkerPayloadHash("privacy-violation:"+strings.Join(violations, ","))}
}

func combinedEvolutionEvidenceIdentity(identities ...string) string {
	normalized := sortedUniqueStrings(identities)
	payload, _ := json.Marshal(normalized)
	return "sha256:" + evolutionWorkerPayloadHash(string(payload))
}

func timeFromEvolutionRun(run EvolutionRun) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, run.UpdatedAt)
	if err != nil {
		return time.Unix(0, 0).UTC()
	}
	return parsed.UTC()
}
