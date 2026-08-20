package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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

	run := insertResearchRunFixtureForTest(t, store, researchStoreTestInput("request-one"))
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

func TestResearchStoreMigrationAddsModelInvocationFailureCode(t *testing.T) {
	store := newResearchStoreForTest(t)
	rows, err := store.db.Query(`PRAGMA table_info(research_model_invocations)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "failure_code" {
			found = true
			if columnType != "TEXT" || notNull != 1 || fmt.Sprint(defaultValue) != "''" {
				t.Fatalf("failure_code type=%q notNull=%d default=%v", columnType, notNull, defaultValue)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("research_model_invocations.failure_code is missing")
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
	if err := store.RenewRunLease(run.RunID, "coordinator-a", claimed.LeaseEpoch, time.Minute); !errors.Is(err, ErrResearchRunLeaseOwner) {
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
	return insertResearchRunFixtureForTest(t, store, researchStoreTestInput(key))
}

func researchStoreTestInput(key string) ResearchRunInput {
	preflightDigest := sha256.Sum256([]byte(key))
	return ResearchRunInput{
		IdempotencyKey: key,
		Request: ResearchRunRequest{
			PreflightID: "research-preflight-fixture-" + hex.EncodeToString(preflightDigest[:16]),
			Mode:        ResearchModeQuick, Question: fmt.Sprintf("Question for %s", key),
			PackageID: "package-fixture", PackageVersion: "1.0.0",
			RequestedSources: []string{ResearchSourceKnowledge},
		},
		Mode:         ResearchModeQuick,
		RouteReasons: []string{ResearchRouteExplicitQuick},
		Budget: ResearchBudget{
			MaxIterations: 2, MaxEvidenceItems: 16, MaxQuotedChars: 4000,
			MaxModelCalls: 4, MaxCostUSD: 0.5,
		},
	}
}

func insertResearchRunFixtureForTest(t *testing.T, store *ResearchStore, input ResearchRunInput) ResearchRun {
	t.Helper()
	if err := validateResearchRunInput(input); err != nil {
		t.Fatal(err)
	}
	requestHash, err := hashResearchRunInput(input)
	if err != nil {
		t.Fatal(err)
	}
	now := store.now().UTC().Format(time.RFC3339Nano)
	run := ResearchRun{
		SchemaVersion: ResearchRunSchemaVersion, RunID: newResearchRunID(),
		PreflightID: strings.TrimSpace(input.Request.PreflightID), Mode: input.Mode,
		Question: strings.TrimSpace(input.Request.Question), Status: ResearchPlanning,
		PackageID: strings.TrimSpace(input.Request.PackageID), PackageVersion: strings.TrimSpace(input.Request.PackageVersion),
		SubjectIDs: append([]string(nil), input.Request.SubjectIDs...), RequestedSources: append([]string(nil), input.Request.RequestedSources...),
		RouteReasons: append([]string(nil), input.RouteReasons...), Budget: input.Budget,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	values, err := marshalResearchRunValues(&run)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO research_runs (
		run_id, parent_run_id, preflight_id, idempotency_key, request_hash, schema_version, mode, question, status,
		package_id, package_version, subject_ids_json, requested_sources_json, route_reasons_json,
		actual_scope_json, budget_json, wait_reason, failure_json, version, created_at, updated_at,
		lease_owner, lease_epoch, lease_expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunID, run.ParentRunID, run.PreflightID, input.IdempotencyKey, requestHash, run.SchemaVersion, run.Mode, run.Question, run.Status,
		run.PackageID, run.PackageVersion, values.subjectIDs, values.requestedSources, values.routeReasons,
		values.actualScope, values.budget, run.WaitReason, values.failure, run.Version, run.CreatedAt, run.UpdatedAt,
		run.LeaseOwner, run.LeaseEpoch, run.LeaseExpiresAt); err != nil {
		t.Fatal(err)
	}
	if err := insertResearchEvent(tx, run.RunID, "", run.Status,
		ResearchTransition{Code: "run_created", Actor: "test_fixture"}, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return run
}
