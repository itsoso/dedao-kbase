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
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	evolutionControlDBName      = "evolution_control.sqlite3"
	evolutionStoreSchemaVersion = "1"
	evolutionEventDefaultLimit  = 100
	evolutionEventMaxLimit      = 500
)

var (
	ErrEvolutionRunNotFound          = errors.New("evolution run not found")
	ErrEvolutionIdempotencyConflict  = errors.New("evolution idempotency key conflicts with existing input")
	ErrEvolutionEventCursorNotFound  = errors.New("evolution event cursor not found")
	ErrEvolutionUnsupportedDBVersion = errors.New("unsupported evolution database schema version")
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
	dbPath    string
	now       func() time.Time
	db        *sql.DB
	testHooks evolutionStoreTestHooks
}

type evolutionStoreTestHooks struct {
	beforeEventInsert func(EvolutionEvent) error
}

func OpenEvolutionControlStore(root string, now func() time.Time) (*EvolutionControlStore, error) {
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
	dbPath := filepath.Join(root, evolutionControlDBName)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open evolution control database: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure evolution control database: %w", err)
		}
	}
	if err := migrateEvolutionControlDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &EvolutionControlStore{dbPath: dbPath, now: now, db: db}, nil
}

func (s *EvolutionControlStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func migrateEvolutionControlDB(db *sql.DB) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin evolution control migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS evolution_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

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
		return fmt.Errorf("migrate evolution control database: %w", err)
	}

	var version string
	err = tx.QueryRow(`SELECT value FROM evolution_meta WHERE key = 'schema_version'`).Scan(&version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.Exec(`INSERT INTO evolution_meta(key, value) VALUES ('schema_version', ?)`, evolutionStoreSchemaVersion); err != nil {
			return fmt.Errorf("record evolution schema version: %w", err)
		}
	case err != nil:
		return fmt.Errorf("read evolution schema version: %w", err)
	case version != evolutionStoreSchemaVersion:
		return fmt.Errorf("%w: found %q, require %q", ErrEvolutionUnsupportedDBVersion, version, evolutionStoreSchemaVersion)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit evolution control migration: %w", err)
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

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin create evolution run: %w", err)
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
	baselineReleasesJSON, err := json.Marshal(run.BaselineReleaseIDs)
	if err != nil {
		return nil, false, fmt.Errorf("encode baseline release IDs: %w", err)
	}
	triggerSignalsJSON, err := json.Marshal(run.TriggerSignalIDs)
	if err != nil {
		return nil, false, fmt.Errorf("encode trigger signal IDs: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_runs (
			run_id, idempotency_key, input_hash, attempt, retry_of_run_id, run_type,
			package_id, baseline_package_version, baseline_release_ids_json, risk_level,
			priority_score, status, trigger_signal_ids_json, current_candidate_id,
			failure_code, failure_message, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.RunID, normalized.IdempotencyKey, inputHash, run.Attempt, run.RetryOfRunID, run.RunType,
		run.PackageID, run.BaselinePackageVersion, string(baselineReleasesJSON), run.RiskLevel,
		run.PriorityScore, run.Status, string(triggerSignalsJSON), run.CurrentCandidateID,
		run.FailureCode, run.FailureMessage, run.CreatedAt, run.UpdatedAt); err != nil {
		return nil, false, fmt.Errorf("insert evolution run: %w", err)
	}
	if err := s.insertEventTx(tx, event); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit evolution run: %w", err)
	}
	return cloneEvolutionRun(&run), true, nil
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

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("begin evolution transition: %w", err)
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
		return nil, err
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
	result, err := tx.Exec(`UPDATE evolution_runs SET status = ?, updated_at = ? WHERE run_id = ? AND status = ?`, to, timestamp, run.RunID, from)
	if err != nil {
		return nil, fmt.Errorf("update evolution run: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("check evolution run update: %w", err)
	}
	if rowsAffected != 1 {
		return nil, fmt.Errorf("evolution run changed concurrently")
	}
	if err := s.insertEventTx(tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit evolution transition: %w", err)
	}
	return cloneEvolutionRun(run), nil
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
	if hook := s.testHooks.beforeEventInsert; hook != nil {
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
