package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestEvolutionGenerationBuildsAgentCandidateWithoutPublishing(t *testing.T) {
	knowledge := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease("release-primary", "book-primary", "2026-08-11T10:00:00Z", "共同结论", "Publisher A", "dedao_ebook")
	support := agentCompilerTestRelease("release-support", "book-support", "2026-08-11T11:00:00Z", "共同结论", "Publisher B", "wechat_mp_article")
	saveKnowledgeAssemblyRelease(t, knowledge, primary)
	saveKnowledgeAssemblyRelease(t, knowledge, support)
	prepareEvolutionKnowledgeCandidate(t, knowledge, primary.ReleaseID, time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))

	control := newEvolutionTestStore(t)
	run := createGeneratingEvolutionRunForType(t, control, EvolutionRunCombined, "agent-generate", "research-assistant", "1.0.1", []string{primary.ReleaseID, support.ReleaseID})
	work := leaseEvolutionGenerationWork(t, control, run.RunID, EvolutionCapabilityAgent)
	service, err := NewEvolutionGenerationService(EvolutionGenerationConfig{
		ControlStore: control, KnowledgeStore: knowledge, GeneratorVersion: "agent-generator.v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Generate(context.Background(), *work)
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidate == nil || result.Candidate.CandidateType != EvolutionCandidateCombined || result.EvaluationWork == nil {
		t.Fatalf("generation result = %#v", result)
	}
	_, payload, err := control.LoadEvolutionCandidate(result.Candidate.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	var envelope evolutionCandidateArtifact
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	var combined EvolutionCombinedCandidateArtifact
	if err := json.Unmarshal(envelope.Artifact, &combined); err != nil {
		t.Fatal(err)
	}
	if combined.Agent.CompilationID == "" || combined.Knowledge.Task.TaskID == "" || combined.Knowledge.SnapshotIdentity == "" {
		t.Fatalf("combined artifact is incomplete: %#v", combined)
	}
	updated, err := control.LoadRun(run.RunID)
	if err != nil || updated.Status != EvolutionEvaluating || updated.CurrentCandidateID != result.Candidate.CandidateID {
		t.Fatalf("updated run = %#v, %v", updated, err)
	}
	records, err := knowledge.ListAgentPackages("", 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("generation published package records = %#v, %v", records, err)
	}

	replayed, err := service.Generate(context.Background(), *work)
	if err != nil || replayed.Candidate.CandidateID != result.Candidate.CandidateID {
		t.Fatalf("generation replay = %#v, %v", replayed, err)
	}
}

func TestEvolutionGenerationEnqueuesAndWaitsForKnowledgeReverification(t *testing.T) {
	knowledge, release := feedbackTestStore(t)
	saveReverificationFeedback(t, knowledge, release.ReleaseID, "event-wait-evolution", KnowledgeFeedbackStale)
	control := newEvolutionTestStore(t)
	run := createGeneratingEvolutionRunForType(t, control, EvolutionRunKnowledgeRelease, "knowledge-wait", "", "", []string{release.ReleaseID})
	work := leaseEvolutionGenerationWork(t, control, run.RunID, EvolutionCapabilityKnowledge)
	service, err := NewEvolutionGenerationService(EvolutionGenerationConfig{
		ControlStore: control, KnowledgeStore: knowledge, GeneratorVersion: "knowledge-generator.v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Generate(context.Background(), *work)
	var failure *EvolutionGenerationFailure
	if !errors.As(err, &failure) || failure.Code != "knowledge_candidate_waiting" || failure.RetryAfter <= 0 {
		t.Fatalf("waiting failure = %#v, %v", failure, err)
	}
	tasks, listErr := knowledge.ListKnowledgeReverifications(release.ReleaseID)
	if listErr != nil || len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationQueued {
		t.Fatalf("reverification tasks = %#v, %v", tasks, listErr)
	}
}

func TestEvolutionGenerationReturnsBoundedCompilerFailure(t *testing.T) {
	control := newEvolutionTestStore(t)
	run := createGeneratingEvolutionRunForType(t, control, EvolutionRunAgentPolicy, "agent-blocked", "research-assistant", "1.0.1", []string{"release-primary"})
	work := leaseEvolutionGenerationWork(t, control, run.RunID, EvolutionCapabilityAgent)
	service, err := NewEvolutionGenerationService(EvolutionGenerationConfig{
		ControlStore: control, KnowledgeStore: NewBookKnowledgeStore(t.TempDir()), GeneratorVersion: "agent-generator.v1",
		CompileAgent: func(*BookKnowledgeStore, AgentCompilationRequest) (*AgentCompilation, error) {
			return &AgentCompilation{
				SchemaVersion: AgentCompilationSchemaVersion, CompilerVersion: AgentCompilerVersion,
				CompilationID: "compilation-blocked", Mode: AgentCompilationModeStudy,
				AssemblyID: "assembly-empty", ReleaseIDs: []string{"release-primary"}, Status: AgentCompilationStatusBlocked,
				Candidates: []AgentCompilationCandidate{{Kind: AgentCompilationCandidateStudy, Status: AgentCompilationCandidateBlocked, Issues: []AgentCompilationIssue{{Code: AgentCompilationIssueMissingCitations, Message: "Citations are required."}}}},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Generate(context.Background(), *work)
	var failure *EvolutionGenerationFailure
	if !errors.As(err, &failure) || failure.Code != AgentCompilationIssueMissingCitations || failure.Message != "candidate generation is blocked" {
		t.Fatalf("generation failure = %#v, %v", failure, err)
	}
	updated, loadErr := control.LoadRun(run.RunID)
	if loadErr != nil || updated.Status != EvolutionGenerating || updated.CurrentCandidateID != "" {
		t.Fatalf("blocked generation mutated run = %#v, %v", updated, loadErr)
	}
}

func TestEvolutionGenerationRecordsReadyKnowledgeCandidateWithoutPublishing(t *testing.T) {
	knowledge, release := feedbackTestStore(t)
	now := time.Date(2026, 8, 11, 15, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, knowledge, release.ReleaseID, "event-stale-evolution", KnowledgeFeedbackStale)
	task, err := knowledge.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := knowledge.ClaimNextKnowledgeReverification(now, time.Hour)
	if err != nil || !ok || claimed.TaskID != task.TaskID {
		t.Fatalf("claim reverification = %#v, %v, %v", claimed, ok, err)
	}
	ready := KnowledgeReverificationCandidate{
		ReleaseContentHash: release.ContentHash, CandidateContentHash: release.ContentHash,
		AnalysisHash: "analysis-hash-ready", QualityDecision: BookQualityPass,
	}
	if _, err := knowledge.CompleteKnowledgeReverification(task.TaskID, task.AssessmentAt, task.AssessmentFingerprint, ready, now); err != nil {
		t.Fatal(err)
	}

	control := newEvolutionTestStore(t)
	run := createGeneratingEvolutionRunForType(t, control, EvolutionRunKnowledgeRelease, "knowledge-generate", "", "", []string{release.ReleaseID})
	work := leaseEvolutionGenerationWork(t, control, run.RunID, EvolutionCapabilityKnowledge)
	service, err := NewEvolutionGenerationService(EvolutionGenerationConfig{
		ControlStore: control, KnowledgeStore: knowledge, GeneratorVersion: "knowledge-generator.v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Generate(context.Background(), *work)
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidate == nil || result.Candidate.CandidateType != EvolutionCandidateKnowledgeRelease || result.EvaluationWork == nil {
		t.Fatalf("knowledge generation result = %#v", result)
	}
	releases, err := knowledge.ListKnowledgeReleases("", 10)
	if err != nil || len(releases) != 1 || releases[0].ReleaseID != release.ReleaseID {
		t.Fatalf("knowledge generation published release = %#v, %v", releases, err)
	}
}

func prepareEvolutionKnowledgeCandidate(t *testing.T, knowledge *BookKnowledgeStore, releaseID string, now time.Time) *KnowledgeReverificationTask {
	t.Helper()
	assessment := saveReverificationFeedback(t, knowledge, releaseID, "event-ready-"+releaseID, KnowledgeFeedbackStale)
	task, err := knowledge.EnqueueKnowledgeReverification(releaseID, *assessment, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := knowledge.ClaimNextKnowledgeReverification(now, time.Hour)
	if err != nil || !ok || claimed.TaskID != task.TaskID {
		t.Fatalf("claim reverification = %#v, %v, %v", claimed, ok, err)
	}
	release, err := knowledge.LoadKnowledgeRelease(releaseID)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := knowledge.CompleteKnowledgeReverification(task.TaskID, task.AssessmentAt, task.AssessmentFingerprint, KnowledgeReverificationCandidate{
		ReleaseContentHash: release.ContentHash, CandidateContentHash: release.ContentHash,
		AnalysisHash: "analysis-ready-" + releaseID, QualityDecision: BookQualityPass,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return ready
}

func createGeneratingEvolutionRunForType(t *testing.T, store *EvolutionControlStore, runType EvolutionRunType, key, packageID, packageVersion string, releaseIDs []string) *EvolutionRun {
	t.Helper()
	input := validEvolutionRunInput(key)
	input.RunType = runType
	input.PackageID = packageID
	input.BaselinePackageVersion = packageVersion
	input.BaselineReleaseIDs = append([]string(nil), releaseIDs...)
	if runType == EvolutionRunKnowledgeRelease {
		input.PackageID = ""
		input.BaselinePackageVersion = ""
	}
	run, created, err := store.CreateRun(input)
	if err != nil || !created {
		t.Fatalf("create run = %#v, %v, %v", run, created, err)
	}
	for _, status := range []EvolutionRunStatus{EvolutionTriaged, EvolutionGenerating} {
		run, err = store.TransitionRun(run.RunID, status, EvolutionTransitionInput{Actor: "test", Code: string(status)})
		if err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
	}
	return run
}

func leaseEvolutionGenerationWork(t *testing.T, store *EvolutionControlStore, runID string, capability EvolutionWorkerCapability) *EvolutionWork {
	t.Helper()
	work, created, err := store.EnqueueEvolutionWork(EvolutionWorkInput{
		IdempotencyKey: "sha256:" + evolutionWorkerPayloadHash("generation:"+string(capability)+":"+runID),
		RunID:          runID, Capability: capability,
		ArtifactRef: "artifact:sha256:" + evolutionWorkerPayloadHash("generation-input:"+runID), MaxAttempts: 3,
	})
	if err != nil || !created {
		t.Fatalf("enqueue generation work = %#v, %v, %v", work, created, err)
	}
	leased, ok, err := store.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{
		WorkerID: "generation-worker", Capabilities: []EvolutionWorkerCapability{capability}, LeaseDuration: time.Minute,
	})
	if err != nil || !ok {
		t.Fatalf("lease generation work = %#v, %v, %v", leased, ok, err)
	}
	return leased
}
