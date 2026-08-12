package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestEvolutionEvaluationSavesScorecardAndAwaitsHumanApproval(t *testing.T) {
	control, work, candidate := evolutionEvaluationFixture(t, EvolutionRunAgentPolicy)
	service, err := NewEvolutionEvaluationService(EvolutionEvaluationConfig{
		ControlStore: control, KnowledgeStore: NewBookKnowledgeStore(t.TempDir()),
		SuiteVersion: "evolution-suite.v1", ScorerVersion: "evolution-scorer.v1",
		Evaluate: func(context.Context, EvolutionRun, EvolutionCandidate, []byte) (EvolutionEvaluationEvidence, error) {
			return evolutionEvaluationEvidence(80, 84), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Evaluate(context.Background(), *work)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scorecard == nil || result.Scorecard.Decision != EvolutionScorecardAwaitingApproval || result.RunStatus != EvolutionAwaitingApproval || result.ResultArtifactRef != "scorecard:"+result.Scorecard.ScorecardID {
		t.Fatalf("evaluation result = %#v", result)
	}
	updated, err := control.LoadRun(candidate.RunID)
	if err != nil || updated.Status != EvolutionAwaitingApproval {
		t.Fatalf("updated run = %#v, %v", updated, err)
	}
	completed, err := control.LoadEvolutionWork(work.WorkID)
	if err != nil || completed.Status != EvolutionWorkCompleted {
		t.Fatalf("completed work = %#v, %v", completed, err)
	}
	replayed, err := service.Evaluate(context.Background(), *work)
	if err != nil || replayed.Scorecard.ScorecardID != result.Scorecard.ScorecardID {
		t.Fatalf("replay = %#v, %v", replayed, err)
	}
}

func TestEvolutionEvaluationBlocksFailedHardGateWithoutPublishing(t *testing.T) {
	control, work, candidate := evolutionEvaluationFixture(t, EvolutionRunAgentPolicy)
	knowledge := NewBookKnowledgeStore(t.TempDir())
	service, err := NewEvolutionEvaluationService(EvolutionEvaluationConfig{
		ControlStore: control, KnowledgeStore: knowledge, SuiteVersion: "evolution-suite.v1", ScorerVersion: "evolution-scorer.v1",
		Evaluate: func(context.Context, EvolutionRun, EvolutionCandidate, []byte) (EvolutionEvaluationEvidence, error) {
			evidence := evolutionEvaluationEvidence(70, 95)
			evidence.HardGates["privacy"] = false
			return evidence, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Evaluate(context.Background(), *work)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scorecard.Decision != EvolutionScorecardBlocked || result.RunStatus != EvolutionBlocked {
		t.Fatalf("blocked result = %#v", result)
	}
	updated, _ := control.LoadRun(candidate.RunID)
	if updated.Status != EvolutionBlocked {
		t.Fatalf("run status = %s", updated.Status)
	}
	packages, packageErr := knowledge.ListAgentPackages("", 10)
	if packageErr != nil || len(packages) != 0 {
		t.Fatalf("evaluation published packages = %#v, %v", packages, packageErr)
	}
}

func TestEvolutionEvaluationFinalizationRollsBackWorkAndRunTogether(t *testing.T) {
	control, work, _ := evolutionEvaluationFixture(t, EvolutionRunAgentPolicy)
	injected := errors.New("injected transition failure")
	control.hooks.beforeEventInsert = func(event EvolutionEvent) error {
		if event.EventType == "transition" && event.Code == "scorecard_passed" {
			return injected
		}
		return nil
	}
	service, err := NewEvolutionEvaluationService(EvolutionEvaluationConfig{
		ControlStore: control, KnowledgeStore: NewBookKnowledgeStore(t.TempDir()),
		SuiteVersion: "evolution-suite.v1", ScorerVersion: "evolution-scorer.v1",
		Evaluate: func(context.Context, EvolutionRun, EvolutionCandidate, []byte) (EvolutionEvaluationEvidence, error) {
			return evolutionEvaluationEvidence(80, 84), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Evaluate(context.Background(), *work); !errors.Is(err, injected) {
		t.Fatalf("evaluation error = %v", err)
	}
	storedWork, _ := control.LoadEvolutionWork(work.WorkID)
	storedRun, _ := control.LoadRun(work.RunID)
	if storedWork.Status != EvolutionWorkLeased || storedRun.Status != EvolutionEvaluating {
		t.Fatalf("partial finalization leaked: work=%s run=%s", storedWork.Status, storedRun.Status)
	}
	var outbox int
	if err := control.db.QueryRow(`SELECT COUNT(*) FROM evolution_outbox WHERE run_id = ?`, work.RunID).Scan(&outbox); err != nil || outbox != 0 {
		t.Fatalf("partial finalization outbox=%d err=%v", outbox, err)
	}
	control.hooks.beforeEventInsert = nil
	if result, err := service.Evaluate(context.Background(), *work); err != nil || result.RunStatus != EvolutionAwaitingApproval {
		t.Fatalf("retry result=%#v err=%v", result, err)
	}
}

func TestEvolutionEvaluationCombinedRequiresBothContributions(t *testing.T) {
	control, work, candidate := evolutionEvaluationFixture(t, EvolutionRunCombined)
	service, err := NewEvolutionEvaluationService(EvolutionEvaluationConfig{
		ControlStore: control, KnowledgeStore: NewBookKnowledgeStore(t.TempDir()),
		SuiteVersion: "evolution-suite.v1", ScorerVersion: "evolution-scorer.v1",
		Evaluate: func(context.Context, EvolutionRun, EvolutionCandidate, []byte) (EvolutionEvaluationEvidence, error) {
			evidence := evolutionEvaluationEvidence(80, 90)
			evidence.ComponentContributions = map[string]float64{"agent": 10}
			return evidence, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Evaluate(context.Background(), *work); err == nil {
		t.Fatal("combined evaluation without knowledge contribution should fail")
	}
	updated, _ := control.LoadRun(candidate.RunID)
	if updated.Status != EvolutionEvaluating {
		t.Fatalf("invalid combined evaluation changed status to %s", updated.Status)
	}
}

func TestEvolutionEvaluationCombinedRejectsSingleComponentCandidate(t *testing.T) {
	control, work, candidate := evolutionEvaluationFixture(t, EvolutionRunCombined)
	candidate.CandidateType = EvolutionCandidateAgentCompilation
	if _, err := control.db.Exec(`UPDATE evolution_candidates SET candidate_type = ? WHERE candidate_id = ?`, candidate.CandidateType, candidate.CandidateID); err != nil {
		t.Fatal(err)
	}
	service, err := NewEvolutionEvaluationService(EvolutionEvaluationConfig{
		ControlStore: control, KnowledgeStore: NewBookKnowledgeStore(t.TempDir()),
		SuiteVersion: "evolution-suite.v1", ScorerVersion: "evolution-scorer.v1",
		Evaluate: func(context.Context, EvolutionRun, EvolutionCandidate, []byte) (EvolutionEvaluationEvidence, error) {
			return evolutionEvaluationEvidence(80, 90), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Evaluate(context.Background(), *work); err == nil {
		t.Fatal("combined run accepted a single-component candidate")
	}
}

func TestEvolutionPrivacyGateFailsClosedOnCredentialEvidence(t *testing.T) {
	for name, artifact := range map[string]any{
		"api token key":    map[string]any{"api_token": "secret-value"},
		"api key":          map[string]any{"api_key": "private-value"},
		"apikey":           map[string]any{"apikey": "private-value"},
		"client secret":    map[string]any{"client_secret": "private-value"},
		"embedded api key": map[string]any{"summary": "api_key=private-value"},
		"embedded secret":  map[string]any{"summary": "client_secret=private-value"},
	} {
		passed, identity, refs := evaluateEvolutionCandidatePrivacy(artifact)
		if passed || identity == "" || len(refs) != 1 {
			t.Fatalf("%s privacy result passed=%v identity=%q refs=%v", name, passed, identity, refs)
		}
	}
	passed, identity, refs := evaluateEvolutionCandidatePrivacy(map[string]any{"summary": "bounded public evidence"})
	if !passed || identity == "" || len(refs) != 0 {
		t.Fatalf("safe privacy result passed=%v identity=%q refs=%v", passed, identity, refs)
	}
}

func TestEvolutionEvaluationReusesKnowledgeFeedbackAndQualityContracts(t *testing.T) {
	knowledge, release := feedbackTestStore(t)
	now := time.Date(2026, 8, 11, 16, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, knowledge, release.ReleaseID, "event-stale-scorecard", KnowledgeFeedbackStale)
	task, err := knowledge.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := knowledge.ClaimNextKnowledgeReverification(now, time.Hour)
	if err != nil || !ok {
		t.Fatalf("claim = %#v, %v, %v", claimed, ok, err)
	}
	if _, err := knowledge.CompleteKnowledgeReverification(task.TaskID, task.AssessmentAt, task.AssessmentFingerprint, KnowledgeReverificationCandidate{
		ReleaseContentHash: release.ContentHash, CandidateContentHash: release.ContentHash,
		AnalysisHash: "analysis-ready", QualityDecision: BookQualityPass,
	}, now); err != nil {
		t.Fatal(err)
	}

	control := newEvolutionTestStore(t)
	run := createGeneratingEvolutionRunForType(t, control, EvolutionRunKnowledgeRelease, "knowledge-scorecard", "", "", []string{release.ReleaseID})
	generationWork := leaseEvolutionGenerationWork(t, control, run.RunID, EvolutionCapabilityKnowledge)
	generator, err := NewEvolutionGenerationService(EvolutionGenerationConfig{
		ControlStore: control, KnowledgeStore: knowledge, GeneratorVersion: "knowledge-generator.v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := generator.Generate(context.Background(), *generationWork)
	if err != nil {
		t.Fatal(err)
	}
	generatedCandidate, candidatePayload, err := control.LoadEvolutionCandidate(generated.Candidate.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	beforeFeedback, err := defaultEvolutionEvaluation(context.Background(), knowledge, *run, *generatedCandidate, candidatePayload)
	if err != nil {
		t.Fatal(err)
	}
	saveReverificationFeedback(t, knowledge, release.ReleaseID, "event-conflict-after-candidate", KnowledgeFeedbackConflict)
	afterFeedback, err := defaultEvolutionEvaluation(context.Background(), knowledge, *run, *generatedCandidate, candidatePayload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeFeedback, afterFeedback) {
		t.Fatalf("immutable candidate evaluation changed with later feedback: before=%#v after=%#v", beforeFeedback, afterFeedback)
	}
	if _, _, err := control.CompleteEvolutionWork(EvolutionWorkCompletion{
		WorkID: generationWork.WorkID, WorkerID: generationWork.WorkerID, LeaseID: generationWork.LeaseID, Attempt: generationWork.Attempt,
		ResultIdempotencyKey: evolutionWorkResultIdempotencyKey(*generationWork, generated.Candidate.ArtifactRef), ResultArtifactRef: generated.Candidate.ArtifactRef,
	}); err != nil {
		t.Fatal(err)
	}
	evaluationWork, ok, err := control.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{
		WorkerID: "evaluation-worker", Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityEvaluation}, LeaseDuration: time.Minute,
	})
	if err != nil || !ok || evaluationWork.WorkID != generated.EvaluationWork.WorkID {
		t.Fatalf("evaluation lease = %#v, %v, %v", evaluationWork, ok, err)
	}
	evaluator, err := NewEvolutionEvaluationService(EvolutionEvaluationConfig{
		ControlStore: control, KnowledgeStore: knowledge,
		SuiteVersion: "evolution-suite.v1", ScorerVersion: "evolution-scorer.v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := evaluator.Evaluate(context.Background(), *evaluationWork)
	if err != nil {
		t.Fatal(err)
	}
	if result.RunStatus != EvolutionAwaitingApproval || result.Scorecard.Delta < EvolutionScorecardMinimumGain {
		t.Fatalf("knowledge evaluation = %#v", result)
	}
	releases, err := knowledge.ListKnowledgeReleases("", 10)
	if err != nil || len(releases) != 1 || releases[0].ReleaseID != release.ReleaseID {
		t.Fatalf("evaluation published release = %#v, %v", releases, err)
	}
}

func evolutionEvaluationFixture(t *testing.T, runType EvolutionRunType) (*EvolutionControlStore, *EvolutionWork, *EvolutionCandidate) {
	t.Helper()
	control := newEvolutionTestStore(t)
	run := createGeneratingEvolutionRunForType(t, control, runType, "evaluation-fixture-"+string(runType), "agent-a", "1.0.0", []string{"release-a"})
	candidate, _, err := control.SaveEvolutionCandidate(EvolutionCandidateInput{
		IdempotencyKey: "sha256:" + evolutionWorkerPayloadHash("evaluation-candidate:"+string(runType)), RunID: run.RunID,
		CandidateType: map[bool]string{true: EvolutionCandidateCombined, false: EvolutionCandidateAgentCompilation}[runType == EvolutionRunCombined], BaselineIdentity: "sha256:" + workerHex('a'),
		ChangeSummary: "候选", GeneratorVersion: "generator.v1", Artifact: map[string]any{"ready": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.TransitionRun(run.RunID, EvolutionEvaluating, EvolutionTransitionInput{Actor: "generator", Code: "candidate_generated"}); err != nil {
		t.Fatal(err)
	}
	queued, _, err := control.EnqueueEvolutionWork(EvolutionWorkInput{
		IdempotencyKey: "sha256:" + evolutionWorkerPayloadHash("evaluation-work:"+string(runType)), RunID: run.RunID,
		Capability: EvolutionCapabilityEvaluation, ArtifactRef: candidate.ArtifactRef, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	leased, ok, err := control.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{
		WorkerID: "evaluation-worker", Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityEvaluation}, LeaseDuration: time.Minute,
	})
	if err != nil || !ok || leased.WorkID != queued.WorkID {
		t.Fatalf("lease = %#v, %v, %v", leased, ok, err)
	}
	return control, leased, candidate
}

func evolutionEvaluationEvidence(baseline, candidate float64) EvolutionEvaluationEvidence {
	baselineMetrics := map[string]float64{}
	candidateMetrics := map[string]float64{}
	for metric := range DefaultEvolutionMetricWeights {
		baselineMetrics[metric] = baseline
		candidateMetrics[metric] = candidate
	}
	return EvolutionEvaluationEvidence{
		HardGates:       map[string]bool{"privacy": true, "citations": true},
		BaselineMetrics: baselineMetrics, CandidateMetrics: candidateMetrics,
		FailureCaseRefs: []string{}, ComponentContributions: map[string]float64{"agent": candidate - baseline, "knowledge": candidate - baseline},
		SuiteIdentity: "sha256:" + workerHex('f'),
	}
}
