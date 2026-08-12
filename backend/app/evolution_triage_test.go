package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEvolutionTriageFreezesPublishedBaselineAndQueuesGenerationAtomically(t *testing.T) {
	knowledge := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, knowledge)
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, knowledge, pkg)
	if _, _, err := PublishAgentPackage(knowledge, pkg, "triage-baseline", AgentReadOnlyToolIDs(), time.Date(2026, 8, 11, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	control := newEvolutionTestStore(t)
	signalInput := validEvolutionSignalInput("triage-production-entry")
	signalInput.PackageID = pkg.PackageID
	_, detected, _, err := control.IngestSignal(signalInput)
	if err != nil || detected.Status != EvolutionDetected || detected.BaselinePackageVersion != evolutionUnresolvedBaseline {
		t.Fatalf("detected run = %#v, %v", detected, err)
	}
	result, err := TriageEvolutionRun(context.Background(), control, knowledge, detected.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != EvolutionGenerating || result.Run.BaselinePackageVersion != pkg.Version ||
		len(result.Run.BaselineReleaseIDs) != 1 || result.Run.BaselineReleaseIDs[0] != "release-1" {
		t.Fatalf("triaged run = %#v", result.Run)
	}
	if result.Work == nil || result.Work.Capability != EvolutionCapabilityAgent || result.Work.Status != EvolutionWorkPending || result.Work.Attempt != 0 {
		t.Fatalf("generation work = %#v", result.Work)
	}
	newPackageInput := validAgentPackage()
	newPackageInput.Version = "1.1.0"
	newPackageInput.UIManifest.Capabilities = append(newPackageInput.UIManifest.Capabilities, "quiz")
	newPackage, err := FinalizeAgentPackage(newPackageInput)
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, knowledge, newPackage)
	if _, _, err := PublishAgentPackage(knowledge, newPackage, "triage-new-published-head", AgentReadOnlyToolIDs(), time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	replayed, err := TriageEvolutionRun(context.Background(), control, knowledge, detected.RunID)
	if err != nil || replayed.Work.WorkID != result.Work.WorkID || replayed.Run.BaselinePackageVersion != pkg.Version {
		t.Fatalf("triage replay = %#v, %v", replayed, err)
	}
	leased, ok, err := control.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{
		WorkerID: "agent-worker", Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityAgent}, LeaseDuration: time.Minute,
	})
	if err != nil || !ok || leased.WorkID != result.Work.WorkID {
		t.Fatalf("production worker lease = %#v, %v, %v", leased, ok, err)
	}
}

func TestEvolutionTriageEventFailureRollsBackRunEventsAndWork(t *testing.T) {
	control := newEvolutionTestStore(t)
	_, detected, _, err := control.IngestSignal(validEvolutionSignalInput("triage-rollback"))
	if err != nil {
		t.Fatal(err)
	}
	var eventsBefore int
	if err := control.db.QueryRow(`SELECT COUNT(*) FROM evolution_events WHERE run_id = ?`, detected.RunID).Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("triage event insert failed")
	control.hooks.beforeEventInsert = func(event EvolutionEvent) error {
		if event.Code == "generation_queued" {
			return injected
		}
		return nil
	}
	_, _, _, err = control.TriageEvolutionRun(EvolutionTriageInput{
		RunID: detected.RunID, BaselinePackageVersion: "1.0.0",
		BaselineReleaseIDs: detected.BaselineReleaseIDs, Actor: "human-operator",
	})
	if !errors.Is(err, injected) {
		t.Fatalf("triage error = %v", err)
	}
	after, err := control.LoadRun(detected.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != EvolutionDetected || after.BaselinePackageVersion != evolutionUnresolvedBaseline {
		t.Fatalf("run changed after rollback = %#v", after)
	}
	var eventsAfter, workAfter int
	if err := control.db.QueryRow(`SELECT COUNT(*) FROM evolution_events WHERE run_id = ?`, detected.RunID).Scan(&eventsAfter); err != nil {
		t.Fatal(err)
	}
	if err := control.db.QueryRow(`SELECT COUNT(*) FROM evolution_work_items WHERE run_id = ?`, detected.RunID).Scan(&workAfter); err != nil {
		t.Fatal(err)
	}
	if eventsAfter != eventsBefore || workAfter != 0 {
		t.Fatalf("rollback counts = events %d (want %d), work %d", eventsAfter, eventsBefore, workAfter)
	}
}
