package app

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestResearchStoreCreatesSchemaAndPersistsRunAcrossReopen(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{
		"research_runs", "research_events", "research_steps", "research_evidence",
		"research_identity_bindings", "research_timeline_events", "research_claims",
		"research_conflicts", "research_conclusions", "research_worker_jobs",
		"research_model_invocations", "research_meta",
	} {
		var name string
		if err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("table %q: %v", table, err)
		}
	}

	run, created, err := store.CreateRun(researchStoreTestInput("request-one"))
	if err != nil || !created {
		t.Fatalf("run=%#v created=%v err=%v", run, created, err)
	}
	if run.SchemaVersion != ResearchRunSchemaVersion || run.Status != ResearchPlanning || run.Version != 1 {
		t.Fatalf("run=%#v", run)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenResearchStore(root, func() time.Time { return now.Add(time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	loaded, err := reopened.LoadRun(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RunID != run.RunID || loaded.Question != run.Question || loaded.Version != run.Version {
		t.Fatalf("loaded=%#v want=%#v", loaded, run)
	}
}

func TestResearchStoreCreatesRunIdempotentlyAndRejectsPayloadConflict(t *testing.T) {
	store := newResearchStoreForTest(t)
	input := researchStoreTestInput("same-request")

	first, created, err := store.CreateRun(input)
	if err != nil || !created {
		t.Fatalf("first=%#v created=%v err=%v", first, created, err)
	}
	second, created, err := store.CreateRun(input)
	if err != nil || created || second.RunID != first.RunID {
		t.Fatalf("second=%#v created=%v err=%v", second, created, err)
	}

	conflict := input
	conflict.Request.Question = "A different question"
	if _, _, err := store.CreateRun(conflict); !errors.Is(err, ErrResearchRunIdempotencyConflict) {
		t.Fatalf("conflict error=%v", err)
	}
}

func TestResearchStoreTransitionsRunAndEventAtomically(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "transition-request")

	updated, err := store.TransitionRun(run.RunID, run.Version, ResearchRetrieving,
		ResearchTransition{Code: "plan_ready", Actor: "orchestrator"})
	if err != nil || updated.Status != ResearchRetrieving || updated.Version != 2 {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	events, err := store.ListEvents(run.RunID, 0, 10)
	if err != nil || len(events) != 2 || events[1].ToStatus != ResearchRetrieving {
		t.Fatalf("events=%#v err=%v", events, err)
	}

	previousFault := researchStoreTransitionFault
	researchStoreTransitionFault = func(stage string) error {
		if stage == "before_event" {
			return errors.New("event insert failed")
		}
		return nil
	}
	t.Cleanup(func() { researchStoreTransitionFault = previousFault })
	if _, err := store.TransitionRun(updated.RunID, updated.Version, ResearchResolvingIdentity,
		ResearchTransition{Code: "evidence_ready", Actor: "orchestrator"}); err == nil {
		t.Fatal("transition succeeded despite event fault")
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != ResearchRetrieving || loaded.Version != 2 {
		t.Fatalf("run changed after rollback: %#v", loaded)
	}
	events, err = store.ListEvents(run.RunID, 0, 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events after rollback=%#v err=%v", events, err)
	}
}

func TestResearchStoreRejectsStaleVersionAndTerminalMutation(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "version-request")
	updated, err := store.TransitionRun(run.RunID, run.Version, ResearchFailed,
		ResearchTransition{Code: "model_failed", Actor: "orchestrator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionRun(run.RunID, run.Version, ResearchRetrieving,
		ResearchTransition{Code: "stale", Actor: "orchestrator"}); !errors.Is(err, ErrResearchRunVersionConflict) {
		t.Fatalf("stale error=%v", err)
	}
	if _, err := store.TransitionRun(updated.RunID, updated.Version, ResearchPlanning,
		ResearchTransition{Code: "resume", Actor: "orchestrator"}); err == nil {
		t.Fatal("terminal run resumed")
	}
}

func TestResearchStoreListsEventsAfterSequenceInStableOrder(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "event-request")
	updated, err := store.TransitionRun(run.RunID, run.Version, ResearchRetrieving,
		ResearchTransition{Code: "plan_ready", Actor: "orchestrator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionRun(updated.RunID, updated.Version, ResearchSynthesizing,
		ResearchTransition{Code: "quick_evidence_ready", Actor: "orchestrator"}); err != nil {
		t.Fatal(err)
	}
	all, err := store.ListEvents(run.RunID, 0, 10)
	if err != nil || len(all) != 3 {
		t.Fatalf("events=%#v err=%v", all, err)
	}
	page, err := store.ListEvents(run.RunID, all[0].Sequence, 1)
	if err != nil || len(page) != 1 || page[0].Sequence != all[1].Sequence {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}

func TestResearchStoreRecoversExpiredRunLease(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	run := createResearchRunForTest(t, store, "lease-request")

	claimed, err := store.ClaimRunnableRun("coordinator-a", time.Minute)
	if err != nil || claimed == nil || claimed.RunID != run.RunID || claimed.LeaseOwner != "coordinator-a" {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	if next, err := store.ClaimRunnableRun("coordinator-b", time.Minute); err != nil || next != nil {
		t.Fatalf("unexpected second claim=%#v err=%v", next, err)
	}

	now = now.Add(2 * time.Minute)
	recovered, err := store.ClaimRunnableRun("coordinator-b", time.Minute)
	if err != nil || recovered == nil || recovered.RunID != run.RunID || recovered.LeaseOwner != "coordinator-b" {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	if err := store.RenewRunLease(run.RunID, "coordinator-a", time.Minute); !errors.Is(err, ErrResearchRunLeaseOwner) {
		t.Fatalf("old owner renewal error=%v", err)
	}
}

func newResearchStoreForTest(t *testing.T) *ResearchStore {
	t.Helper()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createResearchRunForTest(t *testing.T, store *ResearchStore, key string) ResearchRun {
	t.Helper()
	run, created, err := store.CreateRun(researchStoreTestInput(key))
	if err != nil || !created {
		t.Fatalf("run=%#v created=%v err=%v", run, created, err)
	}
	return *run
}

func researchStoreTestInput(key string) ResearchRunInput {
	return ResearchRunInput{
		IdempotencyKey: key,
		Request: ResearchRunRequest{
			Mode: ResearchModeQuick, Question: fmt.Sprintf("Question for %s", key),
			PackageID: "package-fixture", PackageVersion: "1.0.0",
			RequestedSources: []string{ResearchSourceKnowledge},
		},
		Mode:         ResearchModeQuick,
		RouteReasons: []string{ResearchRouteExplicitQuick},
		Budget: ResearchBudget{
			MaxIterations: 2, MaxEvidenceItems: 16, MaxQuotedChars: 4000,
			MaxModelCalls: 4, MaxCostUSD: 0.25,
		},
	}
}
