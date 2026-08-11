package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

const (
	evolutionControlDBName      = "evolution_control.sqlite3"
	evolutionStoreSchemaVersion = 3
	evolutionEventDefaultLimit  = 100
	evolutionEventMaxLimit      = 500
	evolutionDefaultBusyTimeout = 5 * time.Second
)

var (
	ErrEvolutionRunNotFound          = errors.New("evolution run not found")
	ErrEvolutionIdempotencyConflict  = errors.New("evolution idempotency key conflicts with existing input")
	ErrEvolutionEventCursorNotFound  = errors.New("evolution event cursor not found")
	ErrEvolutionUnsupportedDBVersion = errors.New("unsupported evolution database schema version")
	ErrEvolutionTransitionConflict   = errors.New("evolution run transition conflicts with current state")
	ErrEvolutionWriteConflict        = errors.New("evolution store write lock unavailable")
	ErrEvolutionPendingOutbox        = errors.New("evolution run has pending outbox delivery")
)

type EvolutionRunInput struct {
	IdempotencyKey         string           `json:"idempotency_key"`
	RunType                EvolutionRunType `json:"run_type"`
	PackageID              string           `json:"package_id"`
	BaselinePackageVersion string           `json:"baseline_package_version"`
	BaselineReleaseIDs     []string         `json:"baseline_release_ids"`
	RiskLevel              string           `json:"risk_level"`
	PriorityScore          float64          `json:"priority_score"`
	TriggerSignalIDs       []string         `json:"trigger_signal_ids"`
	Actor                  string           `json:"actor"`
	Code                   string           `json:"code"`
	Message                string           `json:"message"`
}

type EvolutionTransitionInput struct {
	Actor        string   `json:"actor"`
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	ArtifactRefs []string `json:"artifact_refs"`
}

type EvolutionControlStore struct {
	dbPath string
	now    func() time.Time
	db     *sql.DB
	hooks  evolutionStoreHooks
}

type evolutionStoreHooks struct {
	beforeBeginTx         func() error
	afterBeginTx          func() error
	beforeEventInsert     func(EvolutionEvent) error
	beforeOutboxInsert    func() error
	afterMigrationVersion func(int) error
	wrapMigrationRows     func(string, evolutionMigrationRows) evolutionMigrationRows
}

const (
	evolutionMigrationRowsRuns          = "v2_runs"
	evolutionMigrationRowsMappingEvents = "v1_mapping_events"
	evolutionMigrationRowsSignalKeys    = "v1_signal_keys"
)

type evolutionMigrationRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

type evolutionStoreOpenOptions struct {
	hooks       evolutionStoreHooks
	busyTimeout time.Duration
}

func OpenEvolutionControlStore(root string, now func() time.Time) (*EvolutionControlStore, error) {
	return openEvolutionControlStoreWithOptions(root, now, evolutionStoreOpenOptions{})
}

func openEvolutionControlStore(root string, now func() time.Time, hooks evolutionStoreHooks) (*EvolutionControlStore, error) {
	return openEvolutionControlStoreWithOptions(root, now, evolutionStoreOpenOptions{hooks: hooks})
}

func openEvolutionControlStoreWithOptions(root string, now func() time.Time, options evolutionStoreOpenOptions) (*EvolutionControlStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("evolution control root is required")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create evolution control root: %w", err)
	}
	dbPath, err := filepath.Abs(filepath.Join(root, evolutionControlDBName))
	if err != nil {
		return nil, fmt.Errorf("resolve evolution control database path: %w", err)
	}
	busyTimeout := options.busyTimeout
	if busyTimeout <= 0 {
		busyTimeout = evolutionDefaultBusyTimeout
	}
	db, err := sql.Open("sqlite3", evolutionSQLiteDSNWithTimeout(dbPath, busyTimeout))
	if err != nil {
		return nil, fmt.Errorf("open evolution control database: %w", err)
	}
	// One connection serializes mutations within a store. The immediate transaction
	// lock in the DSN provides the corresponding boundary across store instances.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrateEvolutionControlDB(db, options.hooks); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &EvolutionControlStore{dbPath: dbPath, now: now, db: db, hooks: options.hooks}, nil
}

func evolutionSQLiteDSN(dbPath string) string {
	return evolutionSQLiteDSNWithTimeout(dbPath, evolutionDefaultBusyTimeout)
}

func evolutionSQLiteDSNWithTimeout(dbPath string, busyTimeout time.Duration) string {
	dsn := url.URL{Scheme: "file", Path: dbPath}
	query := dsn.Query()
	query.Set("_busy_timeout", strconv.FormatInt(busyTimeout.Milliseconds(), 10))
	query.Set("_foreign_keys", "on")
	query.Set("_txlock", "immediate")
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

func (s *EvolutionControlStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func migrateEvolutionControlDB(db *sql.DB, hooks evolutionStoreHooks) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return wrapEvolutionSQLiteWriteError("begin evolution control migration", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS evolution_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("bootstrap evolution schema metadata: %w", err)
	}
	currentVersion := 0
	var storedVersion string
	err = tx.QueryRow(`SELECT value FROM evolution_meta WHERE key = 'schema_version'`).Scan(&storedVersion)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("read evolution schema version: %w", err)
	default:
		currentVersion, err = strconv.Atoi(storedVersion)
		if err != nil || currentVersion < 0 {
			return fmt.Errorf("%w: invalid version %q", ErrEvolutionUnsupportedDBVersion, storedVersion)
		}
	}
	if currentVersion > evolutionStoreSchemaVersion {
		return fmt.Errorf("%w: found %d, support through %d", ErrEvolutionUnsupportedDBVersion, currentVersion, evolutionStoreSchemaVersion)
	}
	for version := currentVersion + 1; version <= evolutionStoreSchemaVersion; version++ {
		switch version {
		case 1:
			if err := applyEvolutionMigrationV1(tx); err != nil {
				return err
			}
		case 2:
			if err := applyEvolutionMigrationV2(tx, hooks); err != nil {
				return err
			}
		case 3:
			if err := applyEvolutionMigrationV3(tx); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: no migration for version %d", ErrEvolutionUnsupportedDBVersion, version)
		}
		if hook := hooks.afterMigrationVersion; hook != nil {
			if err := hook(version); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`
			INSERT INTO evolution_meta(key, value) VALUES ('schema_version', ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, strconv.Itoa(version)); err != nil {
			return fmt.Errorf("record evolution schema version %d: %w", version, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapEvolutionSQLiteWriteError("commit evolution control migration", err)
	}
	return nil
}

func applyEvolutionMigrationV1(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS evolution_signals (
			signal_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			signal_type TEXT NOT NULL,
			source_type TEXT NOT NULL,
			source_id TEXT NOT NULL DEFAULT '',
			package_id TEXT NOT NULL DEFAULT '',
			release_id TEXT NOT NULL DEFAULT '',
			severity TEXT NOT NULL,
			observed_value REAL NOT NULL,
			baseline_value REAL NOT NULL,
			deduplication_key TEXT NOT NULL UNIQUE,
			evidence_refs_json TEXT NOT NULL DEFAULT '[]',
			observed_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_evolution_signals_package_updated
			ON evolution_signals(package_id, updated_at DESC);
		CREATE INDEX IF NOT EXISTS idx_evolution_signals_severity_updated
			ON evolution_signals(severity, updated_at DESC);

		CREATE TABLE IF NOT EXISTS evolution_runs (
			run_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			input_hash TEXT NOT NULL,
			attempt INTEGER NOT NULL,
			retry_of_run_id TEXT NOT NULL DEFAULT '',
			run_type TEXT NOT NULL,
			package_id TEXT NOT NULL DEFAULT '',
			baseline_package_version TEXT NOT NULL DEFAULT '',
			baseline_release_ids_json TEXT NOT NULL DEFAULT '[]',
			risk_level TEXT NOT NULL,
			priority_score REAL NOT NULL,
			status TEXT NOT NULL,
			trigger_signal_ids_json TEXT NOT NULL DEFAULT '[]',
			current_candidate_id TEXT NOT NULL DEFAULT '',
			failure_code TEXT NOT NULL DEFAULT '',
			failure_message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_evolution_runs_status_updated
			ON evolution_runs(status, updated_at DESC, run_id);
		CREATE INDEX IF NOT EXISTS idx_evolution_runs_package_updated
			ON evolution_runs(package_id, updated_at DESC, run_id);
		CREATE INDEX IF NOT EXISTS idx_evolution_runs_risk_updated
			ON evolution_runs(risk_level, updated_at DESC, run_id);
		CREATE INDEX IF NOT EXISTS idx_evolution_runs_updated
			ON evolution_runs(updated_at DESC, run_id);

		CREATE TABLE IF NOT EXISTS evolution_candidates (
			candidate_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			run_id TEXT NOT NULL,
			candidate_type TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			artifact_ref TEXT NOT NULL,
			baseline_identity TEXT NOT NULL,
			change_summary TEXT NOT NULL,
			generator_version TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(run_id, content_hash),
			FOREIGN KEY(run_id) REFERENCES evolution_runs(run_id)
		);
		CREATE INDEX IF NOT EXISTS idx_evolution_candidates_run_created
			ON evolution_candidates(run_id, created_at DESC);

		CREATE TABLE IF NOT EXISTS evolution_scorecards (
			scorecard_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			candidate_id TEXT NOT NULL,
			baseline_identity TEXT NOT NULL,
			suite_version TEXT NOT NULL,
			scorer_version TEXT NOT NULL,
			hard_gates_json TEXT NOT NULL,
			metrics_json TEXT NOT NULL,
			weighted_score REAL NOT NULL,
			baseline_score REAL NOT NULL,
			delta REAL NOT NULL,
			decision TEXT NOT NULL,
			failure_case_refs_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			UNIQUE(candidate_id, suite_version, scorer_version),
			FOREIGN KEY(candidate_id) REFERENCES evolution_candidates(candidate_id)
		);

		CREATE TABLE IF NOT EXISTS evolution_approvals (
			approval_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			run_id TEXT NOT NULL,
			candidate_id TEXT NOT NULL,
			candidate_content_hash TEXT NOT NULL,
			baseline_identity TEXT NOT NULL,
			scorecard_id TEXT NOT NULL,
			decision TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			approved_by TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolution_runs(run_id),
			FOREIGN KEY(candidate_id) REFERENCES evolution_candidates(candidate_id),
			FOREIGN KEY(scorecard_id) REFERENCES evolution_scorecards(scorecard_id)
		);
		CREATE INDEX IF NOT EXISTS idx_evolution_approvals_run_created
			ON evolution_approvals(run_id, created_at DESC);

		CREATE TABLE IF NOT EXISTS evolution_observations (
			observation_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			run_id TEXT NOT NULL,
			published_identity TEXT NOT NULL,
			window_start TEXT NOT NULL,
			window_end TEXT NOT NULL,
			metrics_json TEXT NOT NULL,
			hard_gate_incidents_json TEXT NOT NULL DEFAULT '[]',
			outcome TEXT NOT NULL,
			rollback_identity TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolution_runs(run_id)
		);
		CREATE INDEX IF NOT EXISTS idx_evolution_observations_run_updated
			ON evolution_observations(run_id, updated_at DESC);

		CREATE TABLE IF NOT EXISTS evolution_events (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL UNIQUE,
			run_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			actor TEXT NOT NULL,
			from_status TEXT NOT NULL DEFAULT '',
			to_status TEXT NOT NULL DEFAULT '',
			code TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			artifact_refs_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolution_runs(run_id)
		);
		CREATE INDEX IF NOT EXISTS idx_evolution_events_run_sequence
			ON evolution_events(run_id, sequence);

		CREATE TABLE IF NOT EXISTS evolution_worker_leases (
			lease_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			run_id TEXT NOT NULL,
			work_type TEXT NOT NULL,
			worker_id TEXT NOT NULL,
			status TEXT NOT NULL,
			attempt INTEGER NOT NULL,
			lease_expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolution_runs(run_id)
		);
		CREATE INDEX IF NOT EXISTS idx_evolution_worker_leases_status_expiry
			ON evolution_worker_leases(status, lease_expires_at);

		CREATE TABLE IF NOT EXISTS evolution_outbox (
			outbox_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			run_id TEXT NOT NULL,
			topic TEXT NOT NULL,
			payload_ref TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			attempt INTEGER NOT NULL DEFAULT 0,
			available_at TEXT NOT NULL,
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_expires_at TEXT NOT NULL DEFAULT '',
			delivered_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES evolution_runs(run_id)
		);
		CREATE INDEX IF NOT EXISTS idx_evolution_outbox_pending_delivery
			ON evolution_outbox(status, available_at, created_at)
			WHERE status = 'pending';
	`); err != nil {
		return fmt.Errorf("apply evolution schema version 1: %w", err)
	}
	return nil
}

func applyEvolutionMigrationV2(tx *sql.Tx, hooks evolutionStoreHooks) error {
	if _, err := tx.Exec(`
		ALTER TABLE evolution_runs ADD COLUMN created_at_unix_nano INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE evolution_runs ADD COLUMN updated_at_unix_nano INTEGER NOT NULL DEFAULT 0;

		CREATE TABLE evolution_signal_observations (
			observation_id TEXT PRIMARY KEY,
			request_key_hash TEXT NOT NULL,
			input_hash TEXT NOT NULL,
			signal_id TEXT NOT NULL,
			run_id TEXT NOT NULL,
			payload_fidelity TEXT NOT NULL CHECK(payload_fidelity IN ('complete', 'legacy_hash_only')),
			signal_type TEXT,
			source_type TEXT,
			source_id TEXT,
			package_id TEXT,
			release_id TEXT,
			severity TEXT,
			observed_value REAL,
			baseline_value REAL,
			evidence_refs_json TEXT,
			observed_at TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY(signal_id) REFERENCES evolution_signals(signal_id),
			FOREIGN KEY(run_id) REFERENCES evolution_runs(run_id)
		);
		CREATE UNIQUE INDEX idx_evolution_signal_observations_request
			ON evolution_signal_observations(request_key_hash);
		CREATE INDEX idx_evolution_signal_observations_signal_created
			ON evolution_signal_observations(signal_id, created_at, observation_id);

		CREATE TABLE evolution_run_scopes (
			run_id TEXT NOT NULL,
			scope_type TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			PRIMARY KEY(run_id, scope_type, scope_id),
			FOREIGN KEY(run_id) REFERENCES evolution_runs(run_id)
		);
		CREATE INDEX idx_evolution_run_scopes_lookup
			ON evolution_run_scopes(scope_type, scope_id, run_id);
		CREATE INDEX idx_evolution_runs_created
			ON evolution_runs(created_at DESC, run_id ASC);
		CREATE INDEX idx_evolution_runs_created_ns
			ON evolution_runs(created_at_unix_nano DESC, run_id ASC);
		CREATE INDEX idx_evolution_runs_updated_ns
			ON evolution_runs(updated_at_unix_nano DESC, run_id ASC);
	`); err != nil {
		return fmt.Errorf("apply evolution schema version 2: %w", err)
	}

	type storedRunScope struct {
		runID       string
		packageID   string
		releaseJSON string
		createdAt   string
		updatedAt   string
	}
	rows, err := tx.Query(`SELECT run_id, package_id, baseline_release_ids_json, created_at, updated_at FROM evolution_runs`)
	if err != nil {
		return fmt.Errorf("read v1 evolution runs for v2 backfill: %w", err)
	}
	migrationRows := wrapEvolutionMigrationRows(hooks, evolutionMigrationRowsRuns, rows)
	stored := make([]storedRunScope, 0)
	if err := consumeEvolutionMigrationRows(migrationRows, "v1 evolution runs for v2 backfill", func(rows evolutionMigrationRows) error {
		var run storedRunScope
		if err := rows.Scan(&run.runID, &run.packageID, &run.releaseJSON, &run.createdAt, &run.updatedAt); err != nil {
			return err
		}
		stored = append(stored, run)
		return nil
	}); err != nil {
		return err
	}
	for _, storedRun := range stored {
		createdAt, err := parseEvolutionTimestamp("created_at", storedRun.createdAt)
		if err != nil {
			return fmt.Errorf("backfill evolution run %q: %w", storedRun.runID, err)
		}
		updatedAt, err := parseEvolutionTimestamp("updated_at", storedRun.updatedAt)
		if err != nil {
			return fmt.Errorf("backfill evolution run %q: %w", storedRun.runID, err)
		}
		if _, err := tx.Exec(`UPDATE evolution_runs SET created_at_unix_nano = ?, updated_at_unix_nano = ? WHERE run_id = ?`, createdAt.UnixNano(), updatedAt.UnixNano(), storedRun.runID); err != nil {
			return fmt.Errorf("backfill evolution run timestamp: %w", err)
		}
		if storedRun.packageID != "" {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO evolution_run_scopes(run_id, scope_type, scope_id) VALUES (?, 'package', ?)`, storedRun.runID, storedRun.packageID); err != nil {
				return fmt.Errorf("backfill evolution package scope: %w", err)
			}
		}
		var releaseIDs []string
		if err := json.Unmarshal([]byte(storedRun.releaseJSON), &releaseIDs); err != nil {
			return fmt.Errorf("decode v1 evolution release scopes: %w", err)
		}
		for _, releaseID := range releaseIDs {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO evolution_run_scopes(run_id, scope_type, scope_id) VALUES (?, 'release', ?)`, storedRun.runID, releaseID); err != nil {
				return fmt.Errorf("backfill evolution release scope: %w", err)
			}
		}
	}
	if err := migrateEvolutionV1SignalObservationsTx(tx, hooks); err != nil {
		return err
	}
	return nil
}

func applyEvolutionMigrationV3(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE evolution_work_items (
			work_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			input_hash TEXT NOT NULL,
			run_id TEXT NOT NULL,
			capability TEXT NOT NULL CHECK(capability IN (
				'knowledge_evolution', 'agent_evolution', 'evaluation', 'release', 'observation'
			)),
			artifact_ref TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('pending', 'leased', 'completed', 'blocked')),
			attempt INTEGER NOT NULL DEFAULT 0 CHECK(attempt >= 0),
			max_attempts INTEGER NOT NULL CHECK(max_attempts >= 1),
			available_at TEXT NOT NULL,
			available_at_unix_nano INTEGER NOT NULL,
			lease_id TEXT NOT NULL DEFAULT '',
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_expires_at TEXT NOT NULL DEFAULT '',
			lease_expires_at_unix_nano INTEGER NOT NULL DEFAULT 0,
			result_idempotency_key TEXT NOT NULL DEFAULT '',
			result_hash TEXT NOT NULL DEFAULT '',
			result_artifact_ref TEXT NOT NULL DEFAULT '',
			result_worker_id TEXT NOT NULL DEFAULT '',
			result_lease_id TEXT NOT NULL DEFAULT '',
			result_attempt INTEGER NOT NULL DEFAULT 0 CHECK(result_attempt >= 0),
			failure_code TEXT NOT NULL DEFAULT '',
			failure_message TEXT NOT NULL DEFAULT '',
			failure_idempotency_key TEXT NOT NULL DEFAULT '',
			failure_hash TEXT NOT NULL DEFAULT '',
			failure_worker_id TEXT NOT NULL DEFAULT '',
			failure_lease_id TEXT NOT NULL DEFAULT '',
			failure_attempt INTEGER NOT NULL DEFAULT 0 CHECK(failure_attempt >= 0),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			CHECK(status <> 'completed' OR (
				result_idempotency_key <> '' AND result_artifact_ref <> '' AND
				result_worker_id <> '' AND result_lease_id <> '' AND result_attempt = attempt
			)),
			FOREIGN KEY(run_id) REFERENCES evolution_runs(run_id)
		);
		CREATE INDEX idx_evolution_work_pending_capability
			ON evolution_work_items(status, capability, available_at_unix_nano, created_at, work_id)
			WHERE status = 'pending';
		CREATE INDEX idx_evolution_work_lease_expiry
			ON evolution_work_items(status, lease_expires_at_unix_nano, work_id)
			WHERE status = 'leased';
		CREATE UNIQUE INDEX idx_evolution_work_result_idempotency
			ON evolution_work_items(result_idempotency_key)
			WHERE result_idempotency_key <> '';

		ALTER TABLE evolution_outbox ADD COLUMN lease_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN input_hash TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN available_at_unix_nano INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE evolution_outbox ADD COLUMN lease_expires_at_unix_nano INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE evolution_outbox ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 3 CHECK(max_attempts >= 1);
		ALTER TABLE evolution_outbox ADD COLUMN failure_code TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN failure_message TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN failure_idempotency_key TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN failure_hash TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN failure_worker_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN failure_lease_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN failure_attempt INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE evolution_outbox ADD COLUMN receipt_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN delivery_worker_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN delivery_lease_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE evolution_outbox ADD COLUMN delivery_attempt INTEGER NOT NULL DEFAULT 0;
	`); err != nil {
		return fmt.Errorf("apply evolution schema version 3: %w", err)
	}
	rows, err := tx.Query(`SELECT outbox_id, available_at, lease_expires_at FROM evolution_outbox`)
	if err != nil {
		return fmt.Errorf("read v2 evolution outbox times for v3 backfill: %w", err)
	}
	type storedOutboxTime struct{ outboxID, availableAt, leaseExpiresAt string }
	stored := make([]storedOutboxTime, 0)
	for rows.Next() {
		var item storedOutboxTime
		if err := rows.Scan(&item.outboxID, &item.availableAt, &item.leaseExpiresAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan v2 evolution outbox times: %w", err)
		}
		stored = append(stored, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate v2 evolution outbox times: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close v2 evolution outbox times: %w", err)
	}
	for _, item := range stored {
		availableAt, err := parseEvolutionTimestamp("available_at", item.availableAt)
		if err != nil {
			return fmt.Errorf("backfill evolution outbox %q: %w", item.outboxID, err)
		}
		availableAtNS, err := evolutionWorkerUnixNano(availableAt)
		if err != nil {
			return fmt.Errorf("backfill evolution outbox %q available_at: %w", item.outboxID, err)
		}
		leaseExpiresAtNS := int64(0)
		if item.leaseExpiresAt != "" {
			leaseExpiresAt, err := parseEvolutionTimestamp("lease_expires_at", item.leaseExpiresAt)
			if err != nil {
				return fmt.Errorf("backfill evolution outbox %q: %w", item.outboxID, err)
			}
			leaseExpiresAtNS, err = evolutionWorkerUnixNano(leaseExpiresAt)
			if err != nil {
				return fmt.Errorf("backfill evolution outbox %q lease_expires_at: %w", item.outboxID, err)
			}
		}
		if _, err := tx.Exec(`UPDATE evolution_outbox SET available_at_unix_nano = ?, lease_expires_at_unix_nano = ? WHERE outbox_id = ?`, availableAtNS, leaseExpiresAtNS, item.outboxID); err != nil {
			return fmt.Errorf("backfill evolution outbox integer times: %w", err)
		}
	}
	if _, err := tx.Exec(`
		DROP INDEX idx_evolution_outbox_pending_delivery;
		CREATE INDEX idx_evolution_outbox_pending_delivery
			ON evolution_outbox(status, available_at_unix_nano, created_at, outbox_id)
			WHERE status = 'pending';
		CREATE INDEX idx_evolution_outbox_lease_expiry
			ON evolution_outbox(status, lease_expires_at_unix_nano, outbox_id)
			WHERE status = 'leased';
		CREATE UNIQUE INDEX idx_evolution_outbox_receipt
			ON evolution_outbox(receipt_id) WHERE receipt_id <> '';
	`); err != nil {
		return fmt.Errorf("index evolution schema version 3: %w", err)
	}
	return nil
}

func migrateEvolutionV1SignalObservationsTx(tx *sql.Tx, hooks evolutionStoreHooks) error {
	type storedMappingEvent struct {
		eventID      string
		runID        string
		artifactJSON string
		createdAt    string
	}
	rows, err := tx.Query(`
		SELECT event_id, run_id, artifact_refs_json, created_at
		FROM evolution_events WHERE event_id LIKE 'event-signal-%'
	`)
	if err != nil {
		return fmt.Errorf("read v1 evolution signal mappings: %w", err)
	}
	migrationRows := wrapEvolutionMigrationRows(hooks, evolutionMigrationRowsMappingEvents, rows)
	events := make([]storedMappingEvent, 0)
	if err := consumeEvolutionMigrationRows(migrationRows, "v1 evolution signal mappings", func(rows evolutionMigrationRows) error {
		var event storedMappingEvent
		if err := rows.Scan(&event.eventID, &event.runID, &event.artifactJSON, &event.createdAt); err != nil {
			return err
		}
		events = append(events, event)
		return nil
	}); err != nil {
		return err
	}
	for _, event := range events {
		var refs []string
		if err := json.Unmarshal([]byte(event.artifactJSON), &refs); err != nil {
			return fmt.Errorf("decode v1 evolution signal mapping %q: %w", event.eventID, err)
		}
		inputHash, signalID := "", ""
		publicRefs := make([]string, 0, len(refs))
		for _, ref := range refs {
			switch {
			case strings.HasPrefix(ref, "input-sha256:"):
				inputHash = strings.TrimPrefix(ref, "input-sha256:")
			case strings.HasPrefix(ref, "signal-id:"):
				signalID = strings.TrimPrefix(ref, "signal-id:")
			default:
				publicRefs = append(publicRefs, ref)
			}
		}
		requestDigest := strings.TrimPrefix(event.eventID, "event-signal-")
		if len(requestDigest) != sha256.Size*2 || !isEvolutionLowerHex(requestDigest) ||
			len(inputHash) != sha256.Size*2 || !isEvolutionLowerHex(inputHash) || signalID == "" {
			return fmt.Errorf("v1 evolution signal mapping %q is incomplete", event.eventID)
		}
		var legacyRequestKey, signalType, sourceType, sourceID, packageID, releaseID, severity string
		var observedValue, baselineValue float64
		var evidenceJSON, observedAt string
		if err := tx.QueryRow(`
			SELECT idempotency_key, signal_type, source_type, source_id, package_id, release_id, severity,
				observed_value, baseline_value, evidence_refs_json, observed_at
			FROM evolution_signals WHERE signal_id = ?
		`, signalID).Scan(
			&legacyRequestKey, &signalType, &sourceType, &sourceID, &packageID, &releaseID, &severity,
			&observedValue, &baselineValue, &evidenceJSON, &observedAt,
		); err != nil {
			return fmt.Errorf("load v1 evolution signal %q for observation migration: %w", signalID, err)
		}
		requestKeyHash := "sha256:" + requestDigest
		payloadFidelity := EvolutionObservationPayloadLegacyHashOnly
		var signalTypeValue, sourceTypeValue, sourceIDValue, packageIDValue, releaseIDValue, severityValue any
		var observedValueValue, baselineValueValue, evidenceJSONValue, observedAtValue any
		if evolutionSignalRequestKeyHash(legacyRequestKey) == requestKeyHash {
			var evidenceRefs []string
			parsedObservedAt, parseErr := parseEvolutionTimestamp("observed_at", observedAt)
			if err := json.Unmarshal([]byte(evidenceJSON), &evidenceRefs); err != nil {
				return fmt.Errorf("decode v1 primary evolution signal evidence: %w", err)
			}
			if parseErr != nil {
				return fmt.Errorf("parse v1 primary evolution signal observed_at: %w", parseErr)
			}
			_, reconstructedHash, _, _, normalizeErr := normalizeEvolutionSignalInput(EvolutionSignalInput{
				IdempotencyKey: legacyRequestKey, SignalType: signalType, SourceType: sourceType,
				SourceID: sourceID, PackageID: packageID, ReleaseID: releaseID, Severity: severity,
				ObservedValue: observedValue, BaselineValue: baselineValue,
				EvidenceRefs: evidenceRefs, ObservedAt: parsedObservedAt,
			})
			if normalizeErr != nil {
				return fmt.Errorf("reconstruct v1 primary evolution signal observation: %w", normalizeErr)
			}
			if reconstructedHash == inputHash {
				payloadFidelity = EvolutionObservationPayloadComplete
				signalTypeValue, sourceTypeValue, sourceIDValue = signalType, sourceType, sourceID
				packageIDValue, releaseIDValue, severityValue = packageID, releaseID, severity
				observedValueValue, baselineValueValue = observedValue, baselineValue
				evidenceJSONValue, observedAtValue = evidenceJSON, observedAt
			}
		}
		if _, err := tx.Exec(`
			INSERT INTO evolution_signal_observations (
				observation_id, request_key_hash, input_hash, signal_id, run_id,
				payload_fidelity, signal_type, source_type, source_id, package_id, release_id, severity,
				observed_value, baseline_value, evidence_refs_json, observed_at, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, "observation-migrated-"+requestDigest, requestKeyHash, inputHash, signalID,
			event.runID, payloadFidelity, signalTypeValue, sourceTypeValue, sourceIDValue,
			packageIDValue, releaseIDValue, severityValue, observedValueValue, baselineValueValue,
			evidenceJSONValue, observedAtValue, event.createdAt); err != nil {
			return fmt.Errorf("migrate v1 evolution signal observation: %w", err)
		}
		publicRefsJSON, err := json.Marshal(publicRefs)
		if err != nil {
			return fmt.Errorf("encode migrated evolution event refs: %w", err)
		}
		if _, err := tx.Exec(`UPDATE evolution_events SET artifact_refs_json = ? WHERE event_id = ?`, string(publicRefsJSON), event.eventID); err != nil {
			return fmt.Errorf("remove v1 internal evolution event refs: %w", err)
		}
	}

	rows, err = tx.Query(`SELECT signal_id, idempotency_key FROM evolution_signals`)
	if err != nil {
		return fmt.Errorf("read v1 evolution signal request keys: %w", err)
	}
	migrationRows = wrapEvolutionMigrationRows(hooks, evolutionMigrationRowsSignalKeys, rows)
	type storedSignalKey struct{ signalID, requestKey string }
	keys := make([]storedSignalKey, 0)
	if err := consumeEvolutionMigrationRows(migrationRows, "v1 evolution signal request keys", func(rows evolutionMigrationRows) error {
		var key storedSignalKey
		if err := rows.Scan(&key.signalID, &key.requestKey); err != nil {
			return err
		}
		keys = append(keys, key)
		return nil
	}); err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := tx.Exec(`UPDATE evolution_signals SET idempotency_key = ? WHERE signal_id = ?`, evolutionSignalRequestKeyHash(key.requestKey), key.signalID); err != nil {
			return fmt.Errorf("hash v1 evolution signal request key: %w", err)
		}
	}
	return nil
}

func wrapEvolutionMigrationRows(hooks evolutionStoreHooks, stage string, rows evolutionMigrationRows) evolutionMigrationRows {
	if hooks.wrapMigrationRows != nil {
		return hooks.wrapMigrationRows(stage, rows)
	}
	return rows
}

func consumeEvolutionMigrationRows(rows evolutionMigrationRows, context string, scan func(evolutionMigrationRows) error) error {
	for rows.Next() {
		if err := scan(rows); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan %s: %w", context, err)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate %s: %w", context, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s: %w", context, err)
	}
	return nil
}

// CreateRun returns created=false when the same idempotency key and immutable input already exist.
func (s *EvolutionControlStore) CreateRun(input EvolutionRunInput) (*EvolutionRun, bool, error) {
	normalized, inputHash, err := normalizeEvolutionRunInput(input)
	if err != nil {
		return nil, false, err
	}
	timestamp := s.timestamp()
	runID, err := newEvolutionStoreID("run")
	if err != nil {
		return nil, false, err
	}
	eventID, err := newEvolutionStoreID("event")
	if err != nil {
		return nil, false, err
	}
	run := EvolutionRun{
		RunID:                  runID,
		Attempt:                1,
		RunType:                normalized.RunType,
		PackageID:              normalized.PackageID,
		BaselinePackageVersion: normalized.BaselinePackageVersion,
		BaselineReleaseIDs:     append([]string{}, normalized.BaselineReleaseIDs...),
		RiskLevel:              normalized.RiskLevel,
		PriorityScore:          normalized.PriorityScore,
		Status:                 EvolutionDetected,
		TriggerSignalIDs:       append([]string{}, normalized.TriggerSignalIDs...),
		CreatedAt:              timestamp,
		UpdatedAt:              timestamp,
	}
	if err := run.Validate(); err != nil {
		return nil, false, fmt.Errorf("validate evolution run: %w", err)
	}
	event := EvolutionEvent{
		EventID:      eventID,
		RunID:        runID,
		EventType:    "created",
		Actor:        normalized.Actor,
		ToStatus:     EvolutionDetected,
		Code:         normalized.Code,
		Message:      normalized.Message,
		ArtifactRefs: []string{},
		CreatedAt:    timestamp,
	}
	if err := event.Validate(); err != nil {
		return nil, false, fmt.Errorf("validate evolution created event: %w", err)
	}

	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin create evolution run", err)
	}
	defer func() { _ = tx.Rollback() }()

	existing, existingHash, err := loadEvolutionRunByIdempotencyKey(tx, normalized.IdempotencyKey)
	if err == nil {
		if existingHash != inputHash {
			return nil, false, ErrEvolutionIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("commit evolution run replay: %w", err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("find evolution run replay: %w", err)
	}
	if err := s.insertEvolutionRunTx(tx, normalized.IdempotencyKey, inputHash, &run, event); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit evolution run", err)
	}
	return cloneEvolutionRun(&run), true, nil
}

func (s *EvolutionControlStore) insertEvolutionRunTx(tx *sql.Tx, idempotencyKey, inputHash string, run *EvolutionRun, event EvolutionEvent) error {
	baselineReleasesJSON, err := json.Marshal(run.BaselineReleaseIDs)
	if err != nil {
		return fmt.Errorf("encode baseline release IDs: %w", err)
	}
	triggerSignalsJSON, err := json.Marshal(run.TriggerSignalIDs)
	if err != nil {
		return fmt.Errorf("encode trigger signal IDs: %w", err)
	}
	createdAt, err := parseEvolutionTimestamp("created_at", run.CreatedAt)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_runs (
			run_id, idempotency_key, input_hash, attempt, retry_of_run_id, run_type,
			package_id, baseline_package_version, baseline_release_ids_json, risk_level,
			priority_score, status, trigger_signal_ids_json, current_candidate_id,
			failure_code, failure_message, created_at, updated_at, created_at_unix_nano,
			updated_at_unix_nano
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.RunID, idempotencyKey, inputHash, run.Attempt, run.RetryOfRunID, run.RunType,
		run.PackageID, run.BaselinePackageVersion, string(baselineReleasesJSON), run.RiskLevel,
		run.PriorityScore, run.Status, string(triggerSignalsJSON), run.CurrentCandidateID,
		run.FailureCode, run.FailureMessage, run.CreatedAt, run.UpdatedAt, createdAt.UnixNano(), createdAt.UnixNano()); err != nil {
		return fmt.Errorf("insert evolution run: %w", err)
	}
	if err := insertEvolutionRunScopesTx(tx, run); err != nil {
		return err
	}
	return s.insertEventTx(tx, event)
}

func insertEvolutionRunScopesTx(tx *sql.Tx, run *EvolutionRun) error {
	if run.PackageID != "" {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO evolution_run_scopes(run_id, scope_type, scope_id) VALUES (?, 'package', ?)`, run.RunID, run.PackageID); err != nil {
			return fmt.Errorf("insert evolution package scope: %w", err)
		}
	}
	for _, releaseID := range run.BaselineReleaseIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO evolution_run_scopes(run_id, scope_type, scope_id) VALUES (?, 'release', ?)`, run.RunID, releaseID); err != nil {
			return fmt.Errorf("insert evolution release scope: %w", err)
		}
	}
	return nil
}

func (s *EvolutionControlStore) LoadRun(runID string) (*EvolutionRun, error) {
	if err := validateEvolutionIdentity("run_id", runID); err != nil {
		return nil, err
	}
	run, err := scanEvolutionRun(s.db.QueryRow(evolutionRunSelect+` WHERE run_id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEvolutionRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load evolution run: %w", err)
	}
	return run, nil
}

func (s *EvolutionControlStore) TransitionRun(runID string, to EvolutionRunStatus, input EvolutionTransitionInput) (*EvolutionRun, error) {
	if err := validateEvolutionTransitionInput(runID, to, input); err != nil {
		return nil, err
	}
	eventID, err := newEvolutionStoreID("event")
	if err != nil {
		return nil, err
	}
	timestamp := s.timestamp()

	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, wrapEvolutionSQLiteWriteError("begin evolution transition", err)
	}
	defer func() { _ = tx.Rollback() }()
	run, err := scanEvolutionRun(tx.QueryRow(evolutionRunSelect+` WHERE run_id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEvolutionRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load evolution run for transition: %w", err)
	}
	if err := ValidateEvolutionTransition(run.Status, to); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEvolutionTransitionConflict, err)
	}
	if evolutionTerminalRequiresOutboxDrain(to) {
		var pending int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM evolution_outbox WHERE run_id = ? AND status IN ('pending', 'leased')`, runID).Scan(&pending); err != nil {
			return nil, fmt.Errorf("check pending evolution outbox: %w", err)
		}
		if pending > 0 {
			return nil, ErrEvolutionPendingOutbox
		}
	}
	from := run.Status
	run.Status = to
	run.UpdatedAt = timestamp
	if err := run.Validate(); err != nil {
		return nil, fmt.Errorf("validate transitioned evolution run: %w", err)
	}
	event := EvolutionEvent{
		EventID:      eventID,
		RunID:        run.RunID,
		EventType:    "transition",
		Actor:        input.Actor,
		FromStatus:   from,
		ToStatus:     to,
		Code:         input.Code,
		Message:      input.Message,
		ArtifactRefs: append([]string{}, input.ArtifactRefs...),
		CreatedAt:    timestamp,
	}
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("validate evolution transition event: %w", err)
	}
	updatedAt, err := parseEvolutionTimestamp("updated_at", timestamp)
	if err != nil {
		return nil, err
	}
	result, err := tx.Exec(`UPDATE evolution_runs SET status = ?, updated_at = ?, updated_at_unix_nano = ? WHERE run_id = ? AND status = ?`, to, timestamp, updatedAt.UnixNano(), run.RunID, from)
	if err != nil {
		return nil, fmt.Errorf("update evolution run: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("check evolution run update: %w", err)
	}
	if rowsAffected != 1 {
		return nil, ErrEvolutionTransitionConflict
	}
	if err := s.insertEventTx(tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapEvolutionSQLiteWriteError("commit evolution transition", err)
	}
	return cloneEvolutionRun(run), nil
}

func evolutionTerminalRequiresOutboxDrain(status EvolutionRunStatus) bool {
	switch status {
	case EvolutionCompleted, EvolutionRejected, EvolutionFailed, EvolutionSuperseded, EvolutionRolledBack:
		return true
	default:
		return false
	}
}

// ListEvents returns events after the exclusive event-ID cursor in insertion order.
func (s *EvolutionControlStore) ListEvents(runID, after string, limit int) ([]EvolutionEvent, error) {
	if err := validateEvolutionIdentity("run_id", runID); err != nil {
		return nil, err
	}
	if after != "" {
		if err := validateEvolutionIdentity("after", after); err != nil {
			return nil, err
		}
	}
	if limit < 0 {
		return nil, fmt.Errorf("event limit must not be negative")
	}
	if limit == 0 {
		limit = evolutionEventDefaultLimit
	}
	if limit > evolutionEventMaxLimit {
		return nil, fmt.Errorf("event limit exceeds %d", evolutionEventMaxLimit)
	}
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM evolution_runs WHERE run_id = ?`, runID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEvolutionRunNotFound
	} else if err != nil {
		return nil, fmt.Errorf("find evolution run events: %w", err)
	}
	afterSequence := int64(0)
	if after != "" {
		if err := s.db.QueryRow(`SELECT sequence FROM evolution_events WHERE run_id = ? AND event_id = ?`, runID, after).Scan(&afterSequence); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEvolutionEventCursorNotFound
		} else if err != nil {
			return nil, fmt.Errorf("resolve evolution event cursor: %w", err)
		}
	}
	rows, err := s.db.Query(`
		SELECT event_id, run_id, event_type, actor, from_status, to_status, code,
			message, artifact_refs_json, created_at
		FROM evolution_events
		WHERE run_id = ? AND sequence > ?
		ORDER BY sequence ASC
		LIMIT ?
	`, runID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list evolution events: %w", err)
	}
	defer rows.Close()
	events := make([]EvolutionEvent, 0)
	for rows.Next() {
		event, err := scanEvolutionEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan evolution event: %w", err)
		}
		events = append(events, *event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list evolution events: %w", err)
	}
	return events, nil
}

func (s *EvolutionControlStore) insertEventTx(tx *sql.Tx, event EvolutionEvent) error {
	if hook := s.hooks.beforeEventInsert; hook != nil {
		if err := hook(event); err != nil {
			return err
		}
	}
	artifactRefsJSON, err := json.Marshal(event.ArtifactRefs)
	if err != nil {
		return fmt.Errorf("encode evolution event artifact refs: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_events (
			event_id, run_id, event_type, actor, from_status, to_status, code,
			message, artifact_refs_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.EventID, event.RunID, event.EventType, event.Actor, event.FromStatus, event.ToStatus,
		event.Code, event.Message, string(artifactRefsJSON), event.CreatedAt); err != nil {
		return fmt.Errorf("insert evolution event: %w", err)
	}
	return nil
}

func (s *EvolutionControlStore) beginTx(ctx context.Context) (*sql.Tx, error) {
	if hook := s.hooks.beforeBeginTx; hook != nil {
		if err := hook(); err != nil {
			return nil, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if hook := s.hooks.afterBeginTx; hook != nil {
		if err := hook(); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	return tx, nil
}

func wrapEvolutionSQLiteWriteError(operation string, err error) error {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked) {
		return fmt.Errorf("%w: %s: %w", ErrEvolutionWriteConflict, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func (s *EvolutionControlStore) timestamp() string {
	return s.now().UTC().Format(time.RFC3339Nano)
}

func normalizeEvolutionRunInput(input EvolutionRunInput) (EvolutionRunInput, string, error) {
	input.BaselineReleaseIDs = append([]string{}, input.BaselineReleaseIDs...)
	input.TriggerSignalIDs = append([]string{}, input.TriggerSignalIDs...)
	if err := validateEvolutionReference("idempotency_key", input.IdempotencyKey); err != nil {
		return EvolutionRunInput{}, "", err
	}
	if err := validateEvolutionIdentity("actor", input.Actor); err != nil {
		return EvolutionRunInput{}, "", err
	}
	if err := validateEvolutionCode("code", input.Code); err != nil {
		return EvolutionRunInput{}, "", err
	}
	if err := validateEvolutionText("message", input.Message, EvolutionEventMessageMaxRunes); err != nil {
		return EvolutionRunInput{}, "", err
	}
	probe := EvolutionRun{
		RunID:                  "validation-run",
		Attempt:                1,
		RunType:                input.RunType,
		PackageID:              input.PackageID,
		BaselinePackageVersion: input.BaselinePackageVersion,
		BaselineReleaseIDs:     input.BaselineReleaseIDs,
		RiskLevel:              input.RiskLevel,
		PriorityScore:          input.PriorityScore,
		Status:                 EvolutionDetected,
		TriggerSignalIDs:       input.TriggerSignalIDs,
		CreatedAt:              "2000-01-01T00:00:00Z",
		UpdatedAt:              "2000-01-01T00:00:00Z",
	}
	if err := probe.Validate(); err != nil {
		return EvolutionRunInput{}, "", err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return EvolutionRunInput{}, "", fmt.Errorf("encode evolution run input: %w", err)
	}
	digest := sha256.Sum256(payload)
	return input, hex.EncodeToString(digest[:]), nil
}

func validateEvolutionTransitionInput(runID string, to EvolutionRunStatus, input EvolutionTransitionInput) error {
	if err := validateEvolutionIdentity("run_id", runID); err != nil {
		return err
	}
	if !isKnownEvolutionRunStatus(to) {
		return fmt.Errorf("unknown evolution run status %q", to)
	}
	if err := validateEvolutionIdentity("actor", input.Actor); err != nil {
		return err
	}
	if err := validateEvolutionCode("code", input.Code); err != nil {
		return err
	}
	if err := validateEvolutionText("message", input.Message, EvolutionEventMessageMaxRunes); err != nil {
		return err
	}
	return validateEvolutionReferences("artifact_refs", input.ArtifactRefs, false)
}

const evolutionRunSelect = `
	SELECT run_id, attempt, retry_of_run_id, run_type, package_id,
		baseline_package_version, baseline_release_ids_json, risk_level,
		priority_score, status, trigger_signal_ids_json, current_candidate_id,
		failure_code, failure_message, created_at, updated_at
	FROM evolution_runs`

type evolutionRowScanner interface {
	Scan(dest ...any) error
}

func scanEvolutionRun(scanner evolutionRowScanner) (*EvolutionRun, error) {
	var run EvolutionRun
	var baselineReleaseIDsJSON, triggerSignalIDsJSON string
	if err := scanner.Scan(
		&run.RunID, &run.Attempt, &run.RetryOfRunID, &run.RunType, &run.PackageID,
		&run.BaselinePackageVersion, &baselineReleaseIDsJSON, &run.RiskLevel,
		&run.PriorityScore, &run.Status, &triggerSignalIDsJSON, &run.CurrentCandidateID,
		&run.FailureCode, &run.FailureMessage, &run.CreatedAt, &run.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(baselineReleaseIDsJSON), &run.BaselineReleaseIDs); err != nil {
		return nil, fmt.Errorf("decode baseline release IDs: %w", err)
	}
	if err := json.Unmarshal([]byte(triggerSignalIDsJSON), &run.TriggerSignalIDs); err != nil {
		return nil, fmt.Errorf("decode trigger signal IDs: %w", err)
	}
	if run.BaselineReleaseIDs == nil {
		run.BaselineReleaseIDs = []string{}
	}
	if run.TriggerSignalIDs == nil {
		run.TriggerSignalIDs = []string{}
	}
	if err := run.Validate(); err != nil {
		return nil, fmt.Errorf("invalid stored evolution run: %w", err)
	}
	return &run, nil
}

func loadEvolutionRunByIdempotencyKey(tx *sql.Tx, key string) (*EvolutionRun, string, error) {
	row := tx.QueryRow(evolutionRunSelectWithInputHash+` WHERE idempotency_key = ?`, key)
	run, err := scanEvolutionRunWithTrailingHash(row)
	if err != nil {
		return nil, "", err
	}
	return run.run, run.inputHash, nil
}

const evolutionRunSelectWithInputHash = `
	SELECT run_id, attempt, retry_of_run_id, run_type, package_id,
		baseline_package_version, baseline_release_ids_json, risk_level,
		priority_score, status, trigger_signal_ids_json, current_candidate_id,
		failure_code, failure_message, created_at, updated_at, input_hash
	FROM evolution_runs`

type evolutionRunWithHash struct {
	run       *EvolutionRun
	inputHash string
}

func scanEvolutionRunWithTrailingHash(row *sql.Row) (*evolutionRunWithHash, error) {
	var run EvolutionRun
	var baselineReleaseIDsJSON, triggerSignalIDsJSON, inputHash string
	if err := row.Scan(
		&run.RunID, &run.Attempt, &run.RetryOfRunID, &run.RunType, &run.PackageID,
		&run.BaselinePackageVersion, &baselineReleaseIDsJSON, &run.RiskLevel,
		&run.PriorityScore, &run.Status, &triggerSignalIDsJSON, &run.CurrentCandidateID,
		&run.FailureCode, &run.FailureMessage, &run.CreatedAt, &run.UpdatedAt,
		&inputHash,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(baselineReleaseIDsJSON), &run.BaselineReleaseIDs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(triggerSignalIDsJSON), &run.TriggerSignalIDs); err != nil {
		return nil, err
	}
	if err := run.Validate(); err != nil {
		return nil, err
	}
	return &evolutionRunWithHash{run: &run, inputHash: inputHash}, nil
}

func scanEvolutionEvent(scanner evolutionRowScanner) (*EvolutionEvent, error) {
	var event EvolutionEvent
	var artifactRefsJSON string
	if err := scanner.Scan(
		&event.EventID, &event.RunID, &event.EventType, &event.Actor,
		&event.FromStatus, &event.ToStatus, &event.Code, &event.Message,
		&artifactRefsJSON, &event.CreatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(artifactRefsJSON), &event.ArtifactRefs); err != nil {
		return nil, fmt.Errorf("decode evolution event artifact refs: %w", err)
	}
	if event.ArtifactRefs == nil {
		event.ArtifactRefs = []string{}
	}
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("invalid stored evolution event: %w", err)
	}
	return &event, nil
}

func newEvolutionStoreID(prefix string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate evolution %s ID: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(random[:]), nil
}

func cloneEvolutionRun(run *EvolutionRun) *EvolutionRun {
	cloned := *run
	cloned.BaselineReleaseIDs = append([]string{}, run.BaselineReleaseIDs...)
	cloned.TriggerSignalIDs = append([]string{}, run.TriggerSignalIDs...)
	return &cloned
}
