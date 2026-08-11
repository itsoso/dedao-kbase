package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvolutionStoreCreatesSchemaAndPersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()
	now := fixedEvolutionStoreClock()
	store, err := OpenEvolutionControlStore(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := store.dbPath, filepath.Join(root, "evolution_control.sqlite3"); got != want {
		t.Fatalf("db path = %q, want %q", got, want)
	}

	wantTables := []string{
		"evolution_approvals",
		"evolution_candidates",
		"evolution_events",
		"evolution_meta",
		"evolution_observations",
		"evolution_outbox",
		"evolution_runs",
		"evolution_scorecards",
		"evolution_signals",
		"evolution_worker_leases",
	}
	rows, err := store.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'evolution_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var gotTables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		gotTables = append(gotTables, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(gotTables) != fmt.Sprint(wantTables) {
		t.Fatalf("tables = %v, want %v", gotTables, wantTables)
	}
	wantIndexes := []string{
		"idx_evolution_outbox_pending_delivery",
		"idx_evolution_runs_package_updated",
		"idx_evolution_runs_risk_updated",
		"idx_evolution_runs_status_updated",
		"idx_evolution_runs_updated",
	}
	for _, index := range wantIndexes {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("required index %q count = %d", index, count)
		}
	}

	run, created, err := store.CreateRun(validEvolutionRunInput("persist-run"))
	if err != nil || !created {
		t.Fatalf("CreateRun = %#v, %v, %v", run, created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenEvolutionControlStore(root, now)
	if err != nil {
		t.Fatalf("reopen/migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	loaded, err := store.LoadRun(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(loaded) != fmt.Sprint(run) {
		t.Fatalf("loaded = %#v, want %#v", loaded, run)
	}
	var version string
	if err := store.db.QueryRow(`SELECT value FROM evolution_meta WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != "1" {
		t.Fatalf("schema version = %q", version)
	}
}

func TestEvolutionStoreCreateRunWritesEventAndIsIdempotent(t *testing.T) {
	store := newEvolutionTestStore(t)
	input := validEvolutionRunInput("create-once")
	run, created, err := store.CreateRun(input)
	if err != nil || !created {
		t.Fatalf("CreateRun = %#v, %v, %v", run, created, err)
	}
	if run.Status != EvolutionDetected || run.Attempt != 1 || run.CreatedAt != "2026-08-11T18:00:00.123456789Z" {
		t.Fatalf("run = %#v", run)
	}
	events, err := store.ListEvents(run.RunID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "created" || events[0].FromStatus != "" || events[0].ToStatus != EvolutionDetected {
		t.Fatalf("created events = %#v", events)
	}

	replayed, created, err := store.CreateRun(input)
	if err != nil || created || replayed.RunID != run.RunID {
		t.Fatalf("replay = %#v, %v, %v", replayed, created, err)
	}
	events, err = store.ListEvents(run.RunID, "", 100)
	if err != nil || len(events) != 1 {
		t.Fatalf("events after replay = %#v, %v", events, err)
	}

	conflict := input
	conflict.RiskLevel = "p0"
	if _, _, err := store.CreateRun(conflict); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
}

func TestEvolutionStoreConcurrentCreateIsIdempotent(t *testing.T) {
	store := newEvolutionTestStore(t)
	input := validEvolutionRunInput("concurrent-create")
	const callers = 8
	var wg sync.WaitGroup
	results := make(chan struct {
		run     *EvolutionRun
		created bool
		err     error
	}, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, created, err := store.CreateRun(input)
			results <- struct {
				run     *EvolutionRun
				created bool
				err     error
			}{run, created, err}
		}()
	}
	wg.Wait()
	close(results)

	createdCount := 0
	var runID string
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.created {
			createdCount++
		}
		if runID == "" {
			runID = result.run.RunID
		} else if result.run.RunID != runID {
			t.Fatalf("run IDs differ: %q and %q", runID, result.run.RunID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want 1", createdCount)
	}
	events, err := store.ListEvents(runID, "", 100)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestEvolutionStoreTransitionsRunAndEventAtomically(t *testing.T) {
	store := newEvolutionTestStore(t)
	run := createEvolutionTestRun(t, store, "atomic-transition")
	updated, err := store.TransitionRun(run.RunID, EvolutionTriaged, EvolutionTransitionInput{
		Actor: "operator",
		Code:  "triaged",
	})
	if err != nil || updated.Status != EvolutionTriaged {
		t.Fatalf("transition = %#v, %v", updated, err)
	}
	events, err := store.ListEvents(run.RunID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].EventType != "transition" || events[1].FromStatus != EvolutionDetected || events[1].ToStatus != EvolutionTriaged {
		t.Fatalf("events = %#v", events)
	}

	injected := errors.New("event insert failed")
	store.testHooks.beforeEventInsert = func(event EvolutionEvent) error {
		if event.ToStatus == EvolutionGenerating {
			return injected
		}
		return nil
	}
	if _, err := store.TransitionRun(run.RunID, EvolutionGenerating, EvolutionTransitionInput{
		Actor: "operator",
		Code:  "generate",
	}); !errors.Is(err, injected) {
		t.Fatalf("injected transition error = %v", err)
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != EvolutionTriaged {
		t.Fatalf("status committed despite event failure: %q", loaded.Status)
	}
	events, err = store.ListEvents(run.RunID, "", 100)
	if err != nil || len(events) != 2 {
		t.Fatalf("events after rollback = %#v, %v", events, err)
	}
}

func TestEvolutionStoreRollsBackCreateWhenEventInsertFails(t *testing.T) {
	store := newEvolutionTestStore(t)
	injected := errors.New("event insert failed")
	store.testHooks.beforeEventInsert = func(EvolutionEvent) error { return injected }
	if _, _, err := store.CreateRun(validEvolutionRunInput("failed-create")); !errors.Is(err, injected) {
		t.Fatalf("CreateRun error = %v", err)
	}
	var runCount, eventCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_runs`).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 0 || eventCount != 0 {
		t.Fatalf("failed create committed runs=%d events=%d", runCount, eventCount)
	}
}

func TestEvolutionStoreRejectsInvalidTransitionInputBeforeTransaction(t *testing.T) {
	store := newEvolutionTestStore(t)
	run := createEvolutionTestRun(t, store, "invalid-transition")
	beginCalls := 0
	store.testHooks.beforeBeginTx = func() error {
		beginCalls++
		return nil
	}
	tests := []struct {
		name  string
		to    EvolutionRunStatus
		input EvolutionTransitionInput
		want  string
	}{
		{name: "unknown status", to: EvolutionRunStatus("invented"), input: EvolutionTransitionInput{Actor: "operator", Code: "triaged"}, want: "unknown evolution run status"},
		{name: "invalid actor", to: EvolutionTriaged, input: EvolutionTransitionInput{Actor: "bad actor", Code: "triaged"}, want: "actor contains unsupported characters"},
		{name: "invalid code", to: EvolutionTriaged, input: EvolutionTransitionInput{Actor: "operator", Code: "bad code"}, want: "code contains unsupported characters"},
		{name: "long public message", to: EvolutionTriaged, input: EvolutionTransitionInput{Actor: "operator", Code: "triaged", Message: strings.Repeat("界", EvolutionEventMessageMaxRunes+1)}, want: "message exceeds 512 characters"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.TransitionRun(run.RunID, test.to, test.input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
	if beginCalls != 0 {
		t.Fatalf("begin hook called %d times for pre-transaction validation failures", beginCalls)
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionDetected {
		t.Fatalf("run changed = %#v, %v", loaded, err)
	}
	if _, err := store.TransitionRun(run.RunID, EvolutionTriaged, EvolutionTransitionInput{Actor: "operator", Code: "triaged"}); err != nil {
		t.Fatalf("valid transition: %v", err)
	}
	if beginCalls != 1 {
		t.Fatalf("begin hook calls after valid transition = %d, want 1", beginCalls)
	}
}

func TestEvolutionStoreRejectsInvalidCreateInputBeforeTransaction(t *testing.T) {
	store := newEvolutionTestStore(t)
	beginCalls := 0
	store.testHooks.beforeBeginTx = func() error {
		beginCalls++
		return nil
	}
	tests := []struct {
		name   string
		mutate func(*EvolutionRunInput)
		want   string
	}{
		{name: "invalid run type", mutate: func(input *EvolutionRunInput) { input.RunType = EvolutionRunType("invented") }, want: "unknown evolution run type"},
		{name: "invalid actor", mutate: func(input *EvolutionRunInput) { input.Actor = "bad actor" }, want: "actor contains unsupported characters"},
		{name: "invalid code", mutate: func(input *EvolutionRunInput) { input.Code = "bad code" }, want: "code contains unsupported characters"},
		{name: "long public message", mutate: func(input *EvolutionRunInput) { input.Message = strings.Repeat("界", EvolutionEventMessageMaxRunes+1) }, want: "message exceeds 512 characters"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validEvolutionRunInput("invalid-create-" + strings.ReplaceAll(test.name, " ", "-"))
			test.mutate(&input)
			if _, _, err := store.CreateRun(input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
	if beginCalls != 0 {
		t.Fatalf("begin hook called %d times for invalid creates", beginCalls)
	}
	if _, created, err := store.CreateRun(validEvolutionRunInput("valid-create-after-validation")); err != nil || !created {
		t.Fatalf("valid CreateRun = created %v, error %v", created, err)
	}
	if beginCalls != 1 {
		t.Fatalf("begin hook calls after valid create = %d, want 1", beginCalls)
	}
}

func TestEvolutionStoreReturnsBeforeBeginHookError(t *testing.T) {
	store := newEvolutionTestStore(t)
	injected := errors.New("before begin failed")
	store.testHooks.beforeBeginTx = func() error { return injected }
	if _, _, err := store.CreateRun(validEvolutionRunInput("before-begin-failure")); !errors.Is(err, injected) {
		t.Fatalf("CreateRun error = %v, want %v", err, injected)
	}
	var runCount, eventCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_runs`).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 0 || eventCount != 0 {
		t.Fatalf("hook failure committed runs=%d events=%d", runCount, eventCount)
	}
}

func TestEvolutionStoreListEventsUsesStableBoundedCursorPagination(t *testing.T) {
	store := newEvolutionTestStore(t)
	run := createEvolutionTestRun(t, store, "event-pagination")
	for _, step := range []struct {
		to   EvolutionRunStatus
		code string
	}{
		{EvolutionTriaged, "triaged"},
		{EvolutionGenerating, "generating"},
		{EvolutionEvaluating, "evaluating"},
	} {
		if _, err := store.TransitionRun(run.RunID, step.to, EvolutionTransitionInput{Actor: "worker", Code: step.code}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ListEvents(run.RunID, "", 2)
	if err != nil || len(first) != 2 {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	second, err := store.ListEvents(run.RunID, first[1].EventID, 2)
	if err != nil || len(second) != 2 {
		t.Fatalf("second page = %#v, %v", second, err)
	}
	if first[0].EventID == first[1].EventID || first[1].EventID == second[0].EventID {
		t.Fatalf("cursor pages overlap: %#v %#v", first, second)
	}
	all, err := store.ListEvents(run.RunID, "", 0)
	if err != nil || len(all) != 4 {
		t.Fatalf("default-limit events = %#v, %v", all, err)
	}
	if _, err := store.ListEvents(run.RunID, "", -1); err == nil {
		t.Fatal("negative limit accepted")
	}
	if _, err := store.ListEvents(run.RunID, "", evolutionEventMaxLimit+1); err == nil {
		t.Fatal("oversized limit accepted")
	}
	if _, err := store.ListEvents(run.RunID, "missing-event", 2); !errors.Is(err, ErrEvolutionEventCursorNotFound) {
		t.Fatalf("missing cursor error = %v", err)
	}
	if _, err := store.ListEvents("bad id", "", 2); err == nil {
		t.Fatal("invalid run ID accepted")
	}
}

func newEvolutionTestStore(t *testing.T) *EvolutionControlStore {
	t.Helper()
	store, err := OpenEvolutionControlStore(t.TempDir(), fixedEvolutionStoreClock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close evolution store: %v", err)
		}
	})
	return store
}

func fixedEvolutionStoreClock() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 8, 11, 14, 0, 0, 123456789, time.FixedZone("offset", -4*60*60))
	}
}

func validEvolutionRunInput(key string) EvolutionRunInput {
	return EvolutionRunInput{
		IdempotencyKey:         key,
		RunType:                EvolutionRunCombined,
		PackageID:              "research-assistant",
		BaselinePackageVersion: "1.0.1",
		BaselineReleaseIDs:     []string{"release-1"},
		RiskLevel:              "p1",
		PriorityScore:          91.25,
		TriggerSignalIDs:       []string{"signal-1"},
		Actor:                  "control-plane",
		Code:                   "detected",
		Message:                "A bounded public diagnostic.",
	}
}

func createEvolutionTestRun(t *testing.T, store *EvolutionControlStore, key string) *EvolutionRun {
	t.Helper()
	run, created, err := store.CreateRun(validEvolutionRunInput(key))
	if err != nil || !created {
		t.Fatalf("CreateRun = %#v, %v, %v", run, created, err)
	}
	return run
}
