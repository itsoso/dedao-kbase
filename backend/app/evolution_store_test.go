package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
		"evolution_run_scopes",
		"evolution_runs",
		"evolution_scorecards",
		"evolution_signal_observations",
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
		"idx_evolution_run_scopes_lookup",
		"idx_evolution_runs_created",
		"idx_evolution_runs_created_ns",
		"idx_evolution_runs_package_updated",
		"idx_evolution_runs_risk_updated",
		"idx_evolution_runs_status_updated",
		"idx_evolution_runs_updated",
		"idx_evolution_runs_updated_ns",
		"idx_evolution_signal_observations_request",
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
	if version != "2" {
		t.Fatalf("schema version = %q", version)
	}
}

func TestEvolutionStoreMigratesV1RunsIntoIndexedScopesAndTimeKeys(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, evolutionControlDBName)
	db, err := sql.Open("sqlite3", evolutionSQLiteDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`CREATE TABLE evolution_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := applyEvolutionMigrationV1(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO evolution_meta(key, value) VALUES ('schema_version', '1')`); err != nil {
		t.Fatal(err)
	}
	releaseID := "release-" + strings.Repeat("a", 64)
	createdAt := "2026-08-11T12:00:00.123456789+02:00"
	updatedAt := "2026-08-11T10:30:00.987654321Z"
	requestKey := "11111111-1111-4111-8111-111111111111"
	sourceID := "22222222-2222-4222-8222-222222222222"
	observedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	legacyInput := EvolutionSignalInput{
		IdempotencyKey: requestKey,
		SignalType:     EvolutionSignalRegressionFailure,
		SourceType:     EvolutionSignalSourceEvaluation,
		SourceID:       sourceID,
		PackageID:      "agent-v1",
		Severity:       EvolutionSignalSeverityHigh,
		ObservedValue:  0.4,
		BaselineValue:  0.8,
		EvidenceRefs:   []string{"evaluation:" + sourceID},
		ObservedAt:     observedAt,
	}
	normalized, inputHash, deduplicationKey, _, err := normalizeEvolutionSignalInput(legacyInput)
	if err != nil {
		t.Fatal(err)
	}
	secondaryInput := legacyInput
	secondaryInput.IdempotencyKey = "33333333-3333-4333-8333-333333333333"
	secondaryInput.SourceID = "44444444-4444-4444-8444-444444444444"
	secondaryInput.Severity = EvolutionSignalSeverityCritical
	secondaryInput.ObservedValue = 0.55
	secondaryInput.EvidenceRefs = []string{"evaluation:" + secondaryInput.SourceID}
	_, secondaryInputHash, _, _, err := normalizeEvolutionSignalInput(secondaryInput)
	if err != nil {
		t.Fatal(err)
	}
	requestDigest := sha256.Sum256([]byte(requestKey))
	requestDigestHex := hex.EncodeToString(requestDigest[:])
	secondaryRequestDigest := sha256.Sum256([]byte(secondaryInput.IdempotencyKey))
	secondaryRequestDigestHex := hex.EncodeToString(secondaryRequestDigest[:])
	if _, err := tx.Exec(`
		INSERT INTO evolution_signals (
			signal_id, idempotency_key, signal_type, source_type, source_id, package_id,
			release_id, severity, observed_value, baseline_value, deduplication_key,
			evidence_refs_json, observed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, '', ?, 0.4, 0.8, ?, ?, ?, ?, ?)
	`, "signal-v1", requestKey, EvolutionSignalRegressionFailure, EvolutionSignalSourceEvaluation,
		sourceID, "agent-v1", EvolutionSignalSeverityHigh, deduplicationKey,
		`["evaluation:`+sourceID+`"]`, normalized.ObservedAt, createdAt, updatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_runs (
			run_id, idempotency_key, input_hash, attempt, retry_of_run_id, run_type,
			package_id, baseline_package_version, baseline_release_ids_json, risk_level,
			priority_score, status, trigger_signal_ids_json, current_candidate_id,
			failure_code, failure_message, created_at, updated_at
		) VALUES (?, ?, ?, 1, '', ?, ?, ?, ?, ?, 50, ?, '["signal-v1"]', '', '', '', ?, ?)
	`, "run-v1", "request-v1", "input-v1", EvolutionRunCombined, "agent-v1", "1.0.0",
		`["`+releaseID+`"]`, EvolutionSignalSeverityHigh, EvolutionDetected, createdAt, updatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_events (
			event_id, run_id, event_type, actor, from_status, to_status, code,
			message, artifact_refs_json, created_at
		) VALUES (?, 'run-v1', 'created', 'control-plane', '', ?, 'signal_ingested', '', ?, ?)
	`, "event-signal-"+requestDigestHex, EvolutionDetected,
		`["input-sha256:`+inputHash+`","signal-id:signal-v1"]`, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_events (
			event_id, run_id, event_type, actor, from_status, to_status, code,
			message, artifact_refs_json, created_at
		) VALUES (?, 'run-v1', 'signal_aggregated', 'control-plane', '', '', 'signal_aggregated', '', ?, ?)
	`, "event-signal-"+secondaryRequestDigestHex,
		`["input-sha256:`+secondaryInputHash+`","signal-id:signal-v1"]`, updatedAt); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenEvolutionControlStore(root, fixedEvolutionStoreClock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var version string
	if err := store.db.QueryRow(`SELECT value FROM evolution_meta WHERE key = 'schema_version'`).Scan(&version); err != nil || version != "2" {
		t.Fatalf("schema version = %q, %v", version, err)
	}
	wantCreated, _ := time.Parse(time.RFC3339Nano, createdAt)
	wantUpdated, _ := time.Parse(time.RFC3339Nano, updatedAt)
	var gotCreated, gotUpdated int64
	if err := store.db.QueryRow(`SELECT created_at_unix_nano, updated_at_unix_nano FROM evolution_runs WHERE run_id = 'run-v1'`).Scan(&gotCreated, &gotUpdated); err != nil {
		t.Fatal(err)
	}
	if gotCreated != wantCreated.UnixNano() || gotUpdated != wantUpdated.UnixNano() {
		t.Fatalf("backfilled times = %d/%d, want %d/%d", gotCreated, gotUpdated, wantCreated.UnixNano(), wantUpdated.UnixNano())
	}
	rows, err := store.db.Query(`SELECT scope_type, scope_id FROM evolution_run_scopes WHERE run_id = 'run-v1' ORDER BY scope_type, scope_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var scopes []string
	for rows.Next() {
		var scopeType, scopeID string
		if err := rows.Scan(&scopeType, &scopeID); err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, scopeType+":"+scopeID)
	}
	if got, want := fmt.Sprint(scopes), fmt.Sprintf("[package:agent-v1 release:%s]", releaseID); got != want {
		t.Fatalf("backfilled scopes = %s, want %s", got, want)
	}
	readTx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	primaryObservation, err := loadEvolutionSignalObservationByRequestHashTx(readTx, "sha256:"+requestDigestHex)
	if err != nil {
		_ = readTx.Rollback()
		t.Fatal(err)
	}
	secondaryObservation, err := loadEvolutionSignalObservationByRequestHashTx(readTx, "sha256:"+secondaryRequestDigestHex)
	_ = readTx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if primaryObservation.PayloadFidelity != EvolutionObservationPayloadComplete ||
		primaryObservation.InputHash != inputHash || primaryObservation.SignalID != "signal-v1" || primaryObservation.RunID != "run-v1" ||
		primaryObservation.Severity == nil || *primaryObservation.Severity != legacyInput.Severity ||
		primaryObservation.ObservedValue == nil || *primaryObservation.ObservedValue != legacyInput.ObservedValue {
		t.Fatalf("primary migrated observation = %#v", primaryObservation)
	}
	if secondaryObservation.PayloadFidelity != EvolutionObservationPayloadLegacyHashOnly ||
		secondaryObservation.InputHash != secondaryInputHash || secondaryObservation.SignalID != "signal-v1" || secondaryObservation.RunID != "run-v1" ||
		secondaryObservation.SignalType != nil || secondaryObservation.SourceType != nil || secondaryObservation.SourceID != nil ||
		secondaryObservation.PackageID != nil || secondaryObservation.ReleaseID != nil || secondaryObservation.Severity != nil ||
		secondaryObservation.ObservedValue != nil || secondaryObservation.BaselineValue != nil || secondaryObservation.EvidenceRefs != nil || secondaryObservation.ObservedAt != nil {
		t.Fatalf("secondary migrated observation fabricated payload: %#v", secondaryObservation)
	}
	rows, err = store.db.Query(`SELECT artifact_refs_json FROM evolution_events WHERE event_id IN (?, ?) ORDER BY event_id`,
		"event-signal-"+requestDigestHex, "event-signal-"+secondaryRequestDigestHex)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var gotEventRefs string
		if err := rows.Scan(&gotEventRefs); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if gotEventRefs != "[]" {
			rows.Close()
			t.Fatalf("migrated event refs = %s", gotEventRefs)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	var gotSignalRequestKey string
	if err := store.db.QueryRow(`SELECT idempotency_key FROM evolution_signals WHERE signal_id = 'signal-v1'`).Scan(&gotSignalRequestKey); err != nil {
		t.Fatal(err)
	}
	if gotSignalRequestKey != evolutionSignalRequestKeyHash(requestKey) {
		t.Fatalf("migrated signal request key = %q", gotSignalRequestKey)
	}
	replayedSignal, replayedRun, created, err := store.IngestSignal(legacyInput)
	if err != nil || created || replayedSignal.SignalID != "signal-v1" || replayedRun.RunID != "run-v1" {
		t.Fatalf("migrated replay = %#v/%#v, created %v, error %v", replayedSignal, replayedRun, created, err)
	}
	replayedSignal, replayedRun, created, err = store.IngestSignal(secondaryInput)
	if err != nil || created || replayedSignal.SignalID != "signal-v1" || replayedRun.RunID != "run-v1" {
		t.Fatalf("migrated secondary replay = %#v/%#v, created %v, error %v", replayedSignal, replayedRun, created, err)
	}
	var observationCount, eventCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_signal_observations`).Scan(&observationCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if observationCount != 2 || eventCount != 2 {
		t.Fatalf("migrated replay wrote observations/events = %d/%d", observationCount, eventCount)
	}
}

func TestEvolutionStoreMigrationRowsErrorRollsBackVersion(t *testing.T) {
	for _, stage := range []string{
		evolutionMigrationRowsRuns,
		evolutionMigrationRowsMappingEvents,
		evolutionMigrationRowsSignalKeys,
	} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			seedEvolutionV1MigrationRows(t, root)
			injected := fmt.Errorf("%s interrupted: %w", stage, errEvolutionTestRowsInterrupted)
			wrapped := false
			store, err := openEvolutionControlStoreWithOptions(root, fixedEvolutionStoreClock(), evolutionStoreOpenOptions{
				hooks: evolutionStoreHooks{wrapMigrationRows: func(gotStage string, rows evolutionMigrationRows) evolutionMigrationRows {
					if gotStage != stage {
						return rows
					}
					wrapped = true
					return &interruptingEvolutionMigrationRows{evolutionMigrationRows: rows, err: injected}
				}},
			})
			if store != nil {
				_ = store.Close()
				t.Fatal("migration unexpectedly opened store")
			}
			if !wrapped || !errors.Is(err, errEvolutionTestRowsInterrupted) {
				t.Fatalf("migration error = %v, wrapped %v", err, wrapped)
			}

			db, openErr := sql.Open("sqlite3", evolutionSQLiteDSN(filepath.Join(root, evolutionControlDBName)))
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer db.Close()
			var version string
			if err := db.QueryRow(`SELECT value FROM evolution_meta WHERE key = 'schema_version'`).Scan(&version); err != nil || version != "1" {
				t.Fatalf("schema version after failed migration = %q, %v", version, err)
			}
			var v2Tables int
			if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'evolution_signal_observations'`).Scan(&v2Tables); err != nil || v2Tables != 0 {
				t.Fatalf("v2 table count after rollback = %d, %v", v2Tables, err)
			}
		})
	}
}

var errEvolutionTestRowsInterrupted = errors.New("migration rows interrupted")

type interruptingEvolutionMigrationRows struct {
	evolutionMigrationRows
	err       error
	triggered bool
}

func (r *interruptingEvolutionMigrationRows) Next() bool {
	if r.triggered || !r.evolutionMigrationRows.Next() {
		return false
	}
	r.triggered = true
	return false
}

func (r *interruptingEvolutionMigrationRows) Err() error {
	if r.triggered {
		return r.err
	}
	return r.evolutionMigrationRows.Err()
}

func seedEvolutionV1MigrationRows(t *testing.T, root string) {
	t.Helper()
	db, err := sql.Open("sqlite3", evolutionSQLiteDSN(filepath.Join(root, evolutionControlDBName)))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := tx.Exec(`CREATE TABLE evolution_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := applyEvolutionMigrationV1(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO evolution_meta(key, value) VALUES ('schema_version', '1')`); err != nil {
		t.Fatal(err)
	}
	requestKey := "55555555-5555-4555-8555-555555555555"
	mappingRequestKey := "77777777-7777-4777-8777-777777777777"
	requestDigest := sha256.Sum256([]byte(mappingRequestKey))
	if _, err := tx.Exec(`
		INSERT INTO evolution_signals (
			signal_id, idempotency_key, signal_type, source_type, source_id, package_id,
			release_id, severity, observed_value, baseline_value, deduplication_key,
			evidence_refs_json, observed_at, created_at, updated_at
		) VALUES ('signal-rows', ?, ?, ?, ?, 'rows-agent', '', ?, 0.4, 0.8, ?, '[]', ?, ?, ?)
	`, requestKey, EvolutionSignalRegressionFailure, EvolutionSignalSourceEvaluation,
		"66666666-6666-4666-8666-666666666666", EvolutionSignalSeverityHigh,
		"signal:"+strings.Repeat("d", 64), "2026-08-11T10:00:00Z", "2026-08-11T10:00:00Z", "2026-08-11T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_runs (
			run_id, idempotency_key, input_hash, attempt, retry_of_run_id, run_type,
			package_id, baseline_package_version, baseline_release_ids_json, risk_level,
			priority_score, status, trigger_signal_ids_json, current_candidate_id,
			failure_code, failure_message, created_at, updated_at
		) VALUES ('run-rows', 'request-rows', 'input-rows', 1, '', ?, 'rows-agent', '1.0.0', '[]', ?, 50, ?, '["signal-rows"]', '', '', '', ?, ?)
	`, EvolutionRunAgentPolicy, EvolutionSignalSeverityHigh, EvolutionDetected, "2026-08-11T10:00:00Z", "2026-08-11T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_events (
			event_id, run_id, event_type, actor, from_status, to_status, code,
			message, artifact_refs_json, created_at
		) VALUES (?, 'run-rows', 'created', 'control-plane', '', ?, 'signal_ingested', '', ?, ?)
	`, "event-signal-"+hex.EncodeToString(requestDigest[:]), EvolutionDetected,
		`["input-sha256:`+strings.Repeat("e", 64)+`","signal-id:signal-rows"]`, "2026-08-11T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
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
	if _, err := store.db.Exec(`UPDATE evolution_meta SET value = '3' WHERE key = 'schema_version'`); err != nil {
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
	if version != "3" || beforeObjects != afterObjects {
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
