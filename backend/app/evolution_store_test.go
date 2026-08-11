package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
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
	injected := errors.New("event insert failed")
	store := newEvolutionTestStoreWithHooks(t, evolutionStoreHooks{
		beforeEventInsert: func(event EvolutionEvent) error {
			if event.ToStatus == EvolutionGenerating {
				return injected
			}
			return nil
		},
	})
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
	injected := errors.New("event insert failed")
	store := newEvolutionTestStoreWithHooks(t, evolutionStoreHooks{
		beforeEventInsert: func(EvolutionEvent) error { return injected },
	})
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
	beginCalls := 0
	store := newEvolutionTestStoreWithHooks(t, evolutionStoreHooks{
		beforeBeginTx: func() error {
			beginCalls++
			return nil
		},
	})
	run := createEvolutionTestRun(t, store, "invalid-transition")
	beginCalls = 0
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
	beginCalls := 0
	store := newEvolutionTestStoreWithHooks(t, evolutionStoreHooks{
		beforeBeginTx: func() error {
			beginCalls++
			return nil
		},
	})
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
	injected := errors.New("before begin failed")
	store := newEvolutionTestStoreWithHooks(t, evolutionStoreHooks{
		beforeBeginTx: func() error { return injected },
	})
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

func TestEvolutionStoreConnectionConfigurationSurvivesReconnect(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root with spaces # and %")
	store := openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{})
	if _, err := os.Stat(filepath.Join(root, evolutionControlDBName)); err != nil {
		t.Fatalf("encoded database path: %v", err)
	}
	if dsn := evolutionSQLiteDSN(store.dbPath); strings.Contains(dsn, " ") || strings.Contains(strings.TrimPrefix(dsn, "file:"), "#") {
		t.Fatalf("database DSN is not URL encoded: %q", dsn)
	}
	store.db.SetMaxIdleConns(0)
	for attempt := 1; attempt <= 2; attempt++ {
		conn, err := store.db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var busyTimeout, foreignKeys int
		if err := conn.QueryRowContext(context.Background(), `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		if busyTimeout != 5000 || foreignKeys != 1 {
			t.Fatalf("connection %d pragmas = busy_timeout %d, foreign_keys %d", attempt, busyTimeout, foreignKeys)
		}
	}
	_, err := store.db.Exec(`
		INSERT INTO evolution_candidates (
			candidate_id, idempotency_key, run_id, candidate_type, content_hash,
			artifact_ref, baseline_identity, change_summary, generator_version, created_at
		) VALUES ('orphan', 'orphan', 'missing-run', 'agent', 'sha256:orphan',
			'artifact:orphan', 'baseline:orphan', 'orphan', 'generator-1', '2026-08-11T18:00:00Z')
	`)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("orphan candidate error = %v", err)
	}
}

func TestEvolutionStoreCrossStoreCreateSameInputIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared evolution root")
	stores := []*EvolutionControlStore{
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
	}
	input := validEvolutionRunInput("cross-store-same")
	start := make(chan struct{})
	type result struct {
		run     *EvolutionRun
		created bool
		err     error
	}
	results := make(chan result, len(stores))
	for _, store := range stores {
		go func(store *EvolutionControlStore) {
			<-start
			run, created, err := store.CreateRun(input)
			results <- result{run: run, created: created, err: err}
		}(store)
	}
	close(start)
	first, second := <-results, <-results
	for _, got := range []result{first, second} {
		if got.err != nil {
			t.Fatalf("CreateRun error = %v", got.err)
		}
	}
	if first.created == second.created {
		t.Fatalf("created flags = %v, %v; want exactly one true", first.created, second.created)
	}
	if first.run.RunID != second.run.RunID {
		t.Fatalf("run IDs = %q, %q", first.run.RunID, second.run.RunID)
	}
	assertEvolutionStoreRunEventCounts(t, stores[0], 1, 1)
}

func TestEvolutionStoreCrossStoreCreateConflictingInputIsStable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared conflict root")
	stores := []*EvolutionControlStore{
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
	}
	inputs := []EvolutionRunInput{
		validEvolutionRunInput("cross-store-conflict"),
		validEvolutionRunInput("cross-store-conflict"),
	}
	inputs[1].RiskLevel = "p0"
	start := make(chan struct{})
	errorsByCall := make(chan error, len(stores))
	for index, store := range stores {
		go func(store *EvolutionControlStore, input EvolutionRunInput) {
			<-start
			_, _, err := store.CreateRun(input)
			errorsByCall <- err
		}(store, inputs[index])
	}
	close(start)
	err1, err2 := <-errorsByCall, <-errorsByCall
	if (err1 == nil) == (err2 == nil) {
		t.Fatalf("errors = %v, %v; want one success", err1, err2)
	}
	conflict := err1
	if conflict == nil {
		conflict = err2
	}
	if !errors.Is(conflict, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("conflict error = %v", conflict)
	}
	assertEvolutionStoreRunEventCounts(t, stores[0], 1, 1)
}

func TestEvolutionStoreCrossStoreSameTransitionHasStableConflict(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared transition root")
	stores := []*EvolutionControlStore{
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
	}
	run := createEvolutionTestRun(t, stores[0], "cross-store-transition")
	start := make(chan struct{})
	errorsByCall := make(chan error, len(stores))
	for _, store := range stores {
		go func(store *EvolutionControlStore) {
			<-start
			_, err := store.TransitionRun(run.RunID, EvolutionTriaged, EvolutionTransitionInput{Actor: "operator", Code: "triaged"})
			errorsByCall <- err
		}(store)
	}
	close(start)
	err1, err2 := <-errorsByCall, <-errorsByCall
	if (err1 == nil) == (err2 == nil) {
		t.Fatalf("transition errors = %v, %v; want one success", err1, err2)
	}
	conflict := err1
	if conflict == nil {
		conflict = err2
	}
	if !errors.Is(conflict, ErrEvolutionTransitionConflict) {
		t.Fatalf("transition conflict = %v", conflict)
	}
	loaded, err := stores[0].LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionTriaged {
		t.Fatalf("loaded run = %#v, %v", loaded, err)
	}
	events, err := stores[0].ListEvents(run.RunID, "", 100)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestEvolutionStoreCrossStoreDifferentTransitionsDoNotLoseUpdate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared different transitions")
	stores := []*EvolutionControlStore{
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
	}
	run := createEvolutionTestRun(t, stores[0], "cross-store-different-transition")
	start := make(chan struct{})
	errorsByCall := make(chan error, len(stores))
	targets := []EvolutionRunStatus{EvolutionTriaged, EvolutionBlocked}
	for index, store := range stores {
		go func(store *EvolutionControlStore, target EvolutionRunStatus) {
			<-start
			_, err := store.TransitionRun(run.RunID, target, EvolutionTransitionInput{Actor: "operator", Code: string(target)})
			errorsByCall <- err
		}(store, targets[index])
	}
	close(start)
	errs := []error{<-errorsByCall, <-errorsByCall}
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrEvolutionTransitionConflict) {
			t.Fatalf("transition error = %v", err)
		}
	}
	loaded, err := stores[0].LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionBlocked {
		t.Fatalf("loaded run = %#v, %v", loaded, err)
	}
	events, err := stores[0].ListEvents(run.RunID, "", 100)
	if err != nil || len(events) != successes+1 {
		t.Fatalf("events = %#v, successes = %d, error = %v", events, successes, err)
	}
}

func TestEvolutionStoreDeterministicCreateLockConflictAndReplay(t *testing.T) {
	root := filepath.Join(t.TempDir(), "deterministic create lock")
	lockAcquired := make(chan struct{})
	releaseLock := make(chan struct{})
	var releaseOnce sync.Once
	storeA := openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{
		afterBeginTx: func() error {
			close(lockAcquired)
			select {
			case <-releaseLock:
				return nil
			case <-time.After(5 * time.Second):
				return errors.New("timed out waiting to release create lock")
			}
		},
	})
	storeB := openEvolutionTestStoreAtRootWithOptions(t, root, evolutionStoreOpenOptions{
		busyTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseLock) }) })
	input := validEvolutionRunInput("deterministic-create-lock")
	type createResult struct {
		run     *EvolutionRun
		created bool
		err     error
	}
	resultA := make(chan createResult, 1)
	go func() {
		run, created, err := storeA.CreateRun(input)
		resultA <- createResult{run: run, created: created, err: err}
	}()
	waitEvolutionTestSignal(t, lockAcquired, "store A create lock")
	if _, _, err := storeB.CreateRun(input); !errors.Is(err, ErrEvolutionWriteConflict) {
		t.Fatalf("store B locked CreateRun error = %v", err)
	}
	releaseOnce.Do(func() { close(releaseLock) })
	var first createResult
	select {
	case first = <-resultA:
	case <-time.After(5 * time.Second):
		t.Fatal("store A CreateRun did not finish")
	}
	if first.err != nil || !first.created {
		t.Fatalf("store A CreateRun = %#v, created %v, error %v", first.run, first.created, first.err)
	}
	replayed, created, err := storeB.CreateRun(input)
	if err != nil || created || replayed.RunID != first.run.RunID {
		t.Fatalf("store B replay = %#v, created %v, error %v", replayed, created, err)
	}
	conflicting := input
	conflicting.RiskLevel = "p0"
	if _, _, err := storeB.CreateRun(conflicting); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("store B conflicting replay error = %v", err)
	}
	assertEvolutionStoreRunEventCounts(t, storeB, 1, 1)
}

func TestEvolutionStoreDeterministicTransitionLockConflictAndRetry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "deterministic transition lock")
	seedStore := openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{})
	run := createEvolutionTestRun(t, seedStore, "deterministic-transition-lock")
	lockAcquired := make(chan struct{})
	releaseLock := make(chan struct{})
	var releaseOnce sync.Once
	storeA := openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{
		afterBeginTx: func() error {
			close(lockAcquired)
			select {
			case <-releaseLock:
				return nil
			case <-time.After(5 * time.Second):
				return errors.New("timed out waiting to release transition lock")
			}
		},
	})
	storeB := openEvolutionTestStoreAtRootWithOptions(t, root, evolutionStoreOpenOptions{
		busyTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseLock) }) })
	resultA := make(chan error, 1)
	transition := EvolutionTransitionInput{Actor: "operator", Code: "triaged"}
	go func() {
		_, err := storeA.TransitionRun(run.RunID, EvolutionTriaged, transition)
		resultA <- err
	}()
	waitEvolutionTestSignal(t, lockAcquired, "store A transition lock")
	if _, err := storeB.TransitionRun(run.RunID, EvolutionTriaged, transition); !errors.Is(err, ErrEvolutionWriteConflict) {
		t.Fatalf("store B locked TransitionRun error = %v", err)
	}
	releaseOnce.Do(func() { close(releaseLock) })
	select {
	case err := <-resultA:
		if err != nil {
			t.Fatalf("store A TransitionRun: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("store A TransitionRun did not finish")
	}
	if _, err := storeB.TransitionRun(run.RunID, EvolutionTriaged, transition); !errors.Is(err, ErrEvolutionTransitionConflict) {
		t.Fatalf("store B transition retry error = %v", err)
	}
	loaded, err := storeB.LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionTriaged {
		t.Fatalf("loaded run = %#v, %v", loaded, err)
	}
	events, err := storeB.ListEvents(run.RunID, "", 100)
	if err != nil || len(events) != 2 || events[1].ToStatus != EvolutionTriaged {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestEvolutionStoreWriteConflictPreservesSQLiteCause(t *testing.T) {
	tests := []struct {
		name string
		err  sqlite3.Error
	}{
		{name: "busy", err: sqlite3.Error{Code: sqlite3.ErrBusy, ExtendedCode: sqlite3.ErrBusyRecovery}},
		{name: "locked", err: sqlite3.Error{Code: sqlite3.ErrLocked, ExtendedCode: sqlite3.ErrLockedSharedCache}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wrapped := wrapEvolutionSQLiteWriteError("test write", test.err)
			if !errors.Is(wrapped, ErrEvolutionWriteConflict) {
				t.Fatalf("error = %v, want write conflict", wrapped)
			}
			var cause sqlite3.Error
			if !errors.As(wrapped, &cause) || cause.Code != test.err.Code || cause.ExtendedCode != test.err.ExtendedCode {
				t.Fatalf("SQLite cause = %#v, want %#v", cause, test.err)
			}
		})
	}
	constraint := sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintForeignKey}
	wrapped := wrapEvolutionSQLiteWriteError("test constraint", constraint)
	if errors.Is(wrapped, ErrEvolutionWriteConflict) {
		t.Fatalf("constraint normalized as write conflict: %v", wrapped)
	}
	var cause sqlite3.Error
	if !errors.As(wrapped, &cause) || cause.ExtendedCode != sqlite3.ErrConstraintForeignKey {
		t.Fatalf("constraint cause not preserved: %#v", cause)
	}
}

func TestEvolutionStoreRejectsFutureSchemaWithoutModification(t *testing.T) {
	root := t.TempDir()
	store := openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{})
	if _, err := store.db.Exec(`UPDATE evolution_meta SET value = '2' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	var beforeObjects int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'evolution_%'`).Scan(&beforeObjects); err != nil {
		t.Fatal(err)
	}
	reopened, err := openEvolutionControlStore(root, fixedEvolutionStoreClock(), evolutionStoreHooks{})
	if reopened != nil {
		_ = reopened.Close()
	}
	if !errors.Is(err, ErrEvolutionUnsupportedDBVersion) {
		t.Fatalf("future schema error = %v", err)
	}
	var version string
	var afterObjects int
	if err := store.db.QueryRow(`SELECT value FROM evolution_meta WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'evolution_%'`).Scan(&afterObjects); err != nil {
		t.Fatal(err)
	}
	if version != "2" || beforeObjects != afterObjects {
		t.Fatalf("future schema modified: version=%q objects=%d->%d", version, beforeObjects, afterObjects)
	}
}

func TestEvolutionStoreMigrationFailureRollsBackWholeVersion(t *testing.T) {
	root := t.TempDir()
	injected := errors.New("migration v1 failed")
	store, err := openEvolutionControlStore(root, fixedEvolutionStoreClock(), evolutionStoreHooks{
		afterMigrationVersion: func(version int) error {
			if version == 1 {
				return injected
			}
			return nil
		},
	})
	if store != nil {
		_ = store.Close()
	}
	if !errors.Is(err, injected) {
		t.Fatalf("migration error = %v", err)
	}
	dbPath := filepath.Join(root, evolutionControlDBName)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var evolutionObjects int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'evolution_%'`).Scan(&evolutionObjects); err != nil {
		t.Fatal(err)
	}
	if evolutionObjects != 0 {
		t.Fatalf("failed migration left %d evolution objects", evolutionObjects)
	}
	recovered, err := OpenEvolutionControlStore(root, fixedEvolutionStoreClock())
	if err != nil {
		t.Fatalf("open after rolled back migration: %v", err)
	}
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertEvolutionStoreRunEventCounts(t *testing.T, store *EvolutionControlStore, wantRuns, wantEvents int) {
	t.Helper()
	var runs, events int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if runs != wantRuns || events != wantEvents {
		t.Fatalf("counts = runs %d, events %d; want %d, %d", runs, events, wantRuns, wantEvents)
	}
}

func newEvolutionTestStore(t *testing.T) *EvolutionControlStore {
	t.Helper()
	return newEvolutionTestStoreWithHooks(t, evolutionStoreHooks{})
}

func newEvolutionTestStoreWithHooks(t *testing.T, hooks evolutionStoreHooks) *EvolutionControlStore {
	t.Helper()
	return openEvolutionTestStoreAtRoot(t, t.TempDir(), hooks)
}

func openEvolutionTestStoreAtRoot(t *testing.T, root string, hooks evolutionStoreHooks) *EvolutionControlStore {
	t.Helper()
	return openEvolutionTestStoreAtRootWithOptions(t, root, evolutionStoreOpenOptions{hooks: hooks})
}

func openEvolutionTestStoreAtRootWithOptions(t *testing.T, root string, options evolutionStoreOpenOptions) *EvolutionControlStore {
	t.Helper()
	store, err := openEvolutionControlStoreWithOptions(root, fixedEvolutionStoreClock(), options)
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

func waitEvolutionTestSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
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
