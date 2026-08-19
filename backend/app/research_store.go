package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	researchStoreDBName          = "research_control.sqlite3"
	researchIdempotencyKeyMax    = 128
	researchRouteReasonsMax      = 16
	researchEventFieldMaxRunes   = 1000
	researchEventListDefault     = 100
	researchEventListMax         = 500
	researchBudgetMaxIterations  = 20
	researchBudgetMaxEvidence    = 1000
	researchBudgetMaxQuotedChars = 200000
	researchBudgetMaxModelCalls  = 100
	researchBudgetMaxCostUSD     = 100
)

var (
	ErrResearchRunNotFound            = errors.New("research run not found")
	ErrResearchRunIdempotencyConflict = errors.New("research run idempotency conflict")
	ErrResearchRunVersionConflict     = errors.New("research run version conflict")
	ErrResearchRunLeaseOwner          = errors.New("research run belongs to another coordinator")
	ErrResearchRunStaleLease          = errors.New("research run lease is stale")

	researchStoreTransitionFault = func(string) error { return nil }
)

type ResearchRunInput struct {
	IdempotencyKey string             `json:"idempotency_key"`
	Request        ResearchRunRequest `json:"request"`
	Mode           string             `json:"mode"`
	RouteReasons   []string           `json:"route_reasons"`
	Budget         ResearchBudget     `json:"budget"`
}

type ResearchEvent struct {
	Sequence   int64             `json:"sequence"`
	RunID      string            `json:"run_id"`
	FromStatus ResearchRunStatus `json:"from_status,omitempty"`
	ToStatus   ResearchRunStatus `json:"to_status"`
	Code       string            `json:"code"`
	Actor      string            `json:"actor"`
	Summary    string            `json:"summary,omitempty"`
	CreatedAt  string            `json:"created_at"`
}

type ResearchStore struct {
	dbPath string
	now    func() time.Time
	random io.Reader
	db     *sql.DB
}

func OpenResearchStore(root string, now func() time.Time) (*ResearchStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("research store root is required")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(root, researchStoreDBName)
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     dbPath,
		RawQuery: "_busy_timeout=5000&_foreign_keys=on&_txlock=immediate",
	}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{`PRAGMA busy_timeout = 5000`, `PRAGMA foreign_keys = ON`} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := migrateResearchStore(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &ResearchStore{dbPath: dbPath, now: now, random: rand.Reader, db: db}, nil
}

func (s *ResearchStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func migrateResearchStore(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS research_runs (
			run_id TEXT PRIMARY KEY,
			parent_run_id TEXT NOT NULL DEFAULT '',
			preflight_id TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL UNIQUE,
			request_hash TEXT NOT NULL,
			schema_version TEXT NOT NULL,
			mode TEXT NOT NULL,
			question TEXT NOT NULL,
			status TEXT NOT NULL,
			package_id TEXT NOT NULL DEFAULT '',
			package_version TEXT NOT NULL DEFAULT '',
			subject_ids_json TEXT NOT NULL DEFAULT '[]',
			requested_sources_json TEXT NOT NULL DEFAULT '[]',
			route_reasons_json TEXT NOT NULL DEFAULT '[]',
			actual_scope_json TEXT NOT NULL DEFAULT '{}',
			budget_json TEXT NOT NULL DEFAULT '{}',
			wait_reason TEXT NOT NULL DEFAULT '',
			failure_json TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_epoch TEXT NOT NULL DEFAULT '',
			lease_expires_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_runs_status_updated ON research_runs(status, updated_at, run_id)`,
		`CREATE TABLE IF NOT EXISTS research_events (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			from_status TEXT NOT NULL DEFAULT '',
			to_status TEXT NOT NULL,
			code TEXT NOT NULL,
			actor TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_events_run_sequence ON research_events(run_id, sequence)`,
		`CREATE TABLE IF NOT EXISTS research_http_owners (
			run_id TEXT PRIMARY KEY REFERENCES research_runs(run_id) ON DELETE CASCADE,
			owner_hash TEXT NOT NULL, created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_http_owners_owner ON research_http_owners(owner_hash, run_id)`,
		`CREATE TABLE IF NOT EXISTS research_steps (
			step_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			stage TEXT NOT NULL, status TEXT NOT NULL, decision_summary TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '', completed_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS research_evidence (
			evidence_id TEXT NOT NULL, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			source_type TEXT NOT NULL, source_role TEXT NOT NULL, author_identity_id TEXT NOT NULL DEFAULT '',
			subject_identity_ids_json TEXT NOT NULL DEFAULT '[]', occurred_at TEXT NOT NULL DEFAULT '',
			content_excerpt TEXT NOT NULL DEFAULT '', locator_json TEXT NOT NULL, locator_hash TEXT NOT NULL,
			content_hash TEXT NOT NULL, privacy TEXT NOT NULL, selected INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			PRIMARY KEY (run_id, evidence_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_evidence_hash ON research_evidence(run_id, content_hash)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_research_evidence_locator_hash ON research_evidence(run_id, locator_hash, content_hash)`,
		`CREATE TABLE IF NOT EXISTS research_identity_bindings (
			binding_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			identity_id TEXT NOT NULL, source_type TEXT NOT NULL, source_identity_hash TEXT NOT NULL,
			confidence REAL NOT NULL, created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS research_timeline_events (
			timeline_event_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			occurred_at TEXT NOT NULL, summary TEXT NOT NULL, evidence_ids_json TEXT NOT NULL DEFAULT '[]'
		)`,
		`CREATE TABLE IF NOT EXISTS research_claims (
			claim_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			claim_text TEXT NOT NULL, evidence_ids_json TEXT NOT NULL DEFAULT '[]', confidence REAL NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS research_conflicts (
			conflict_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			claim_ids_json TEXT NOT NULL DEFAULT '[]', summary TEXT NOT NULL, resolution TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS research_conclusions (
			conclusion_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			conclusion_text TEXT NOT NULL, evidence_ids_json TEXT NOT NULL DEFAULT '[]',
			citation_ids_json TEXT NOT NULL DEFAULT '[]', confidence REAL NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS research_worker_jobs (
			job_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			target_agent_id TEXT NOT NULL, tool TEXT NOT NULL, arguments_json TEXT NOT NULL, state TEXT NOT NULL,
			attempt INTEGER NOT NULL DEFAULT 0, max_attempts INTEGER NOT NULL,
			lease_owner TEXT NOT NULL DEFAULT '', lease_id TEXT NOT NULL DEFAULT '', lease_expires_at TEXT NOT NULL DEFAULT '', request_hash TEXT NOT NULL,
			result_fingerprint TEXT NOT NULL DEFAULT '', failure_code TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT NOT NULL DEFAULT '',
			UNIQUE (run_id, request_hash)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_worker_jobs_state_lease ON research_worker_jobs(target_agent_id, state, lease_expires_at, created_at)`,
		`CREATE TABLE IF NOT EXISTS research_worker_candidates (
			run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			job_id TEXT NOT NULL REFERENCES research_worker_jobs(job_id) ON DELETE CASCADE,
			candidate_ref TEXT NOT NULL, source_type TEXT NOT NULL, source_role TEXT NOT NULL,
			occurred_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
			PRIMARY KEY (run_id, candidate_ref)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_worker_candidates_job ON research_worker_candidates(job_id, candidate_ref)`,
		`CREATE TABLE IF NOT EXISTS research_model_invocations (
			invocation_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			request_identity TEXT NOT NULL UNIQUE, model TEXT NOT NULL, purpose TEXT NOT NULL,
			status TEXT NOT NULL, attempt INTEGER NOT NULL DEFAULT 0, lease_epoch TEXT NOT NULL DEFAULT '',
			failure_code TEXT NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
			estimated_cost_usd REAL NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_model_request_identity ON research_model_invocations(request_identity)`,
		`CREATE TABLE IF NOT EXISTS research_model_invocation_attempts (
			request_identity TEXT NOT NULL, attempt INTEGER NOT NULL, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			lease_epoch TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
			reserved_cost_usd REAL NOT NULL DEFAULT 0, actual_cost_usd REAL NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
			PRIMARY KEY (request_identity, attempt)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_model_attempts_run ON research_model_invocation_attempts(run_id, status, updated_at)`,
		`CREATE TABLE IF NOT EXISTS research_preflights (
			preflight_id TEXT PRIMARY KEY,
			owner_hash TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			status TEXT NOT NULL,
			candidates_json TEXT NOT NULL,
			checks_json TEXT NOT NULL,
			gaps_json TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT '',
			requested_sources_json TEXT NOT NULL DEFAULT '[]',
			package_constraint TEXT NOT NULL DEFAULT '',
			parent_run_id TEXT NOT NULL DEFAULT '',
			bound_run_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_preflights_owner_expiry ON research_preflights(owner_hash, expires_at, preflight_id)`,
		`CREATE INDEX IF NOT EXISTS idx_research_preflights_expiry ON research_preflights(expires_at, preflight_id)`,
		`CREATE TABLE IF NOT EXISTS research_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT OR IGNORE INTO research_meta(key, value) VALUES ('schema_version', '1')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureResearchStoreColumn(db, "research_conclusions", "citation_ids_json", `TEXT NOT NULL DEFAULT '[]'`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_worker_jobs", "lease_id", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_runs", "lease_epoch", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_runs", "parent_run_id", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_runs", "preflight_id", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_preflights", "mode", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_preflights", "requested_sources_json", `TEXT NOT NULL DEFAULT '[]'`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_preflights", "package_constraint", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_preflights", "bound_run_id", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_model_invocations", "attempt", `INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := ensureResearchStoreColumn(db, "research_model_invocations", "lease_epoch", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	return ensureResearchStoreColumn(db, "research_model_invocations", "failure_code", `TEXT NOT NULL DEFAULT ''`)
}

func ensureResearchStoreColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

func (s *ResearchStore) CreateRun(input ResearchRunInput) (*ResearchRun, bool, error) {
	if err := validateResearchRunInput(input); err != nil {
		return nil, false, err
	}
	requestHash, err := hashResearchRunInput(input)
	if err != nil {
		return nil, false, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingID, existingHash string
	err = tx.QueryRow(`SELECT run_id, request_hash FROM research_runs WHERE idempotency_key = ?`, input.IdempotencyKey).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash != requestHash {
			return nil, false, ErrResearchRunIdempotencyConflict
		}
		run, err := loadResearchRun(tx, existingID)
		return run, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	now := s.now().UTC().Format(time.RFC3339Nano)
	run := &ResearchRun{
		SchemaVersion: ResearchRunSchemaVersion, RunID: newResearchRunID(), Mode: input.Mode,
		PreflightID: strings.TrimSpace(input.Request.PreflightID),
		Question:    strings.TrimSpace(input.Request.Question), Status: ResearchPlanning,
		PackageID: strings.TrimSpace(input.Request.PackageID), PackageVersion: strings.TrimSpace(input.Request.PackageVersion),
		SubjectIDs:       append([]string(nil), input.Request.SubjectIDs...),
		RequestedSources: append([]string(nil), input.Request.RequestedSources...),
		RouteReasons:     append([]string(nil), input.RouteReasons...), Budget: input.Budget,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	values, err := marshalResearchRunValues(run)
	if err != nil {
		return nil, false, err
	}
	_, err = tx.Exec(`INSERT INTO research_runs (
		run_id, parent_run_id, preflight_id, idempotency_key, request_hash, schema_version, mode, question, status,
		package_id, package_version, subject_ids_json, requested_sources_json, route_reasons_json,
		actual_scope_json, budget_json, wait_reason, failure_json, version, created_at, updated_at,
		lease_owner, lease_epoch, lease_expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunID, run.ParentRunID, run.PreflightID, input.IdempotencyKey, requestHash, run.SchemaVersion, run.Mode, run.Question, run.Status,
		run.PackageID, run.PackageVersion, values.subjectIDs, values.requestedSources, values.routeReasons,
		values.actualScope, values.budget, run.WaitReason, values.failure, run.Version, run.CreatedAt, run.UpdatedAt,
		run.LeaseOwner, run.LeaseEpoch, run.LeaseExpiresAt)
	if err != nil {
		return nil, false, err
	}
	if err := insertResearchEvent(tx, run.RunID, "", run.Status, ResearchTransition{Code: "run_created", Actor: "orchestrator"}, now); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return run, true, nil
}

func (s *ResearchStore) LoadRun(runID string) (*ResearchRun, error) {
	return loadResearchRun(s.db, strings.TrimSpace(runID))
}

func (s *ResearchStore) TransitionRun(runID string, expectedVersion int64, to ResearchRunStatus, transition ResearchTransition) (*ResearchRun, error) {
	return s.transitionRun(runID, expectedVersion, to, transition, "", "", false)
}

func (s *ResearchStore) TransitionRunWithLease(runID string, expectedVersion int64, to ResearchRunStatus, transition ResearchTransition, owner, epoch string) (*ResearchRun, error) {
	return s.transitionRun(runID, expectedVersion, to, transition, owner, epoch, true)
}

func (s *ResearchStore) transitionRun(runID string, expectedVersion int64, to ResearchRunStatus, transition ResearchTransition, owner, epoch string, guarded bool) (*ResearchRun, error) {
	if err := validateResearchTransitionInput(transition); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := loadResearchRun(tx, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	if run.Version != expectedVersion {
		return nil, ErrResearchRunVersionConflict
	}
	if guarded {
		if err := assertResearchRunLeaseTx(tx, run.RunID, owner, epoch, s.now()); err != nil {
			return nil, err
		}
	}
	if err := ValidateResearchTransition(run.Status, to); err != nil {
		return nil, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	query := `UPDATE research_runs SET status = ?, version = version + 1, updated_at = ?`
	args := []any{to, now}
	if !guarded {
		query += `, lease_owner = '', lease_epoch = '', lease_expires_at = ''`
	}
	query += ` WHERE run_id = ? AND version = ?`
	args = append(args, run.RunID, expectedVersion)
	if guarded {
		query += ` AND lease_owner = ? AND lease_epoch = ?`
		args = append(args, owner, epoch)
	}
	result, err := tx.Exec(query, args...)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrResearchRunVersionConflict
	}
	if err := researchStoreTransitionFault("before_event"); err != nil {
		return nil, err
	}
	if err := insertResearchEvent(tx, run.RunID, run.Status, to, transition, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	run.Status = to
	run.Version++
	run.UpdatedAt = now
	return run, nil
}

func assertResearchRunLeaseTx(tx *sql.Tx, runID, owner, epoch string, now time.Time) error {
	var storedOwner, storedEpoch, expiresAt string
	if err := tx.QueryRow(`SELECT lease_owner, lease_epoch, lease_expires_at FROM research_runs WHERE run_id = ?`, strings.TrimSpace(runID)).Scan(&storedOwner, &storedEpoch, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrResearchRunNotFound
		}
		return err
	}
	owner = strings.TrimSpace(owner)
	epoch = strings.TrimSpace(epoch)
	if owner == "" && epoch == "" && storedOwner == "" && storedEpoch == "" {
		return nil
	}
	if owner == "" || epoch == "" || storedOwner != owner || storedEpoch != epoch || researchWorkerLeaseExpired(expiresAt, now) {
		return ErrResearchRunStaleLease
	}
	return nil
}

func (s *ResearchStore) ListEvents(runID string, after int64, limit int) ([]ResearchEvent, error) {
	if limit <= 0 {
		limit = researchEventListDefault
	}
	if limit > researchEventListMax {
		limit = researchEventListMax
	}
	rows, err := s.db.Query(`SELECT sequence, run_id, from_status, to_status, code, actor, summary, created_at
		FROM research_events WHERE run_id = ? AND sequence > ? ORDER BY sequence ASC LIMIT ?`, strings.TrimSpace(runID), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]ResearchEvent, 0)
	for rows.Next() {
		var event ResearchEvent
		if err := rows.Scan(&event.Sequence, &event.RunID, &event.FromStatus, &event.ToStatus, &event.Code, &event.Actor, &event.Summary, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *ResearchStore) ClaimRunnableRun(owner string, lease time.Duration) (*ResearchRun, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || lease <= 0 {
		return nil, fmt.Errorf("lease owner and positive duration are required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	nowTime := s.now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	row := tx.QueryRow(`SELECT run_id FROM research_runs
		WHERE status NOT IN (?, ?, ?, ?) AND (lease_owner = '' OR lease_expires_at <= ?)
		ORDER BY created_at ASC, run_id ASC LIMIT 1`,
		ResearchCompleted, ResearchInsufficient, ResearchFailed, ResearchCanceled, now)
	var runID string
	if err := row.Scan(&runID); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	run, err := loadResearchRun(tx, runID)
	if err != nil {
		return nil, err
	}
	expires := nowTime.Add(lease).Format(time.RFC3339Nano)
	epoch := strings.Replace(newResearchRunID(), "research-run-", "research-lease-", 1)
	if _, err := tx.Exec(`UPDATE research_runs SET lease_owner = ?, lease_epoch = ?, lease_expires_at = ?, updated_at = ? WHERE run_id = ?`, owner, epoch, expires, now, runID); err != nil {
		return nil, err
	}
	if err := insertResearchEvent(tx, runID, run.Status, run.Status,
		ResearchTransition{Code: "lease_claimed", Actor: owner}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	run.LeaseOwner = owner
	run.LeaseEpoch = epoch
	run.LeaseExpiresAt = expires
	run.UpdatedAt = now
	return run, nil
}

func (s *ResearchStore) RenewRunLease(runID, owner, epoch string, lease time.Duration) error {
	owner = strings.TrimSpace(owner)
	if owner == "" || lease <= 0 {
		return fmt.Errorf("lease owner and positive duration are required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := loadResearchRun(tx, strings.TrimSpace(runID))
	if err != nil {
		return err
	}
	if run.LeaseOwner != owner {
		return ErrResearchRunLeaseOwner
	}
	if strings.TrimSpace(epoch) == "" || run.LeaseEpoch != strings.TrimSpace(epoch) {
		return ErrResearchRunStaleLease
	}
	nowTime := s.now().UTC()
	if researchWorkerLeaseExpired(run.LeaseExpiresAt, nowTime) {
		return ErrResearchRunStaleLease
	}
	now := nowTime.Format(time.RFC3339Nano)
	expires := nowTime.Add(lease).Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE research_runs SET lease_expires_at = ?, updated_at = ?
		WHERE run_id = ? AND lease_owner = ? AND lease_epoch = ?`, expires, now, run.RunID, owner, epoch)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return ErrResearchRunStaleLease
	}
	if err := insertResearchEvent(tx, run.RunID, run.Status, run.Status,
		ResearchTransition{Code: "lease_renewed", Actor: owner}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ResearchStore) StoreEvidenceBundle(runID string, expectedVersion int64, bundle ResearchEvidenceBundle) (*ResearchRun, error) {
	return s.storeEvidenceBundle(runID, expectedVersion, bundle, "", "")
}

func (s *ResearchStore) StoreEvidenceBundleWithLease(runID string, expectedVersion int64, bundle ResearchEvidenceBundle, owner, epoch string) (*ResearchRun, error) {
	return s.storeEvidenceBundle(runID, expectedVersion, bundle, owner, epoch)
}

func (s *ResearchStore) storeEvidenceBundle(runID string, expectedVersion int64, bundle ResearchEvidenceBundle, owner, epoch string) (*ResearchRun, error) {
	if err := validateResearchEvidenceBundle(bundle); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := loadResearchRun(tx, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	if run.Version != expectedVersion {
		return nil, ErrResearchRunVersionConflict
	}
	if err := assertResearchRunLeaseTx(tx, run.RunID, owner, epoch, s.now()); err != nil {
		return nil, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	if err := storeResearchEvidenceBundleTx(tx, run, bundle, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return run, nil
}

func validateResearchEvidenceBundle(bundle ResearchEvidenceBundle) error {
	searchedSources, err := normalizeResearchScopeSources(bundle.SearchedSources)
	if err != nil {
		return err
	}
	citedSources, err := normalizeResearchScopeSources(bundle.CitedSources)
	if err != nil {
		return err
	}
	if len(searchedSources) != len(bundle.SearchedSources) || len(citedSources) != len(bundle.CitedSources) {
		return fmt.Errorf("evidence scope sources must be normalized and unique")
	}
	if len(bundle.Evidence) > researchEvidenceItemsMax {
		return fmt.Errorf("evidence bundle exceeds %d items", researchEvidenceItemsMax)
	}
	evidenceSources := make(map[string]bool, len(bundle.Evidence))
	for _, evidence := range bundle.Evidence {
		if err := validateNormalizedResearchEvidence(evidence); err != nil {
			return err
		}
		source, err := researchScopeSourceForEvidence(evidence.SourceType)
		if err != nil {
			return err
		}
		evidenceSources[source] = true
	}
	if len(evidenceSources) != len(citedSources) {
		return fmt.Errorf("cited_sources must exactly match selected evidence sources")
	}
	for _, source := range citedSources {
		if !evidenceSources[source] {
			return fmt.Errorf("cited source %q has no selected evidence", source)
		}
	}
	return nil
}

func storeResearchEvidenceBundleTx(tx *sql.Tx, run *ResearchRun, bundle ResearchEvidenceBundle, now string) error {
	if err := validateResearchEvidenceBundle(bundle); err != nil {
		return err
	}
	if err := enforceResearchEvidenceBudgetTx(tx, run, bundle.Evidence); err != nil {
		return err
	}
	for _, evidence := range bundle.Evidence {
		var existingHash string
		err := tx.QueryRow(`SELECT content_hash FROM research_evidence WHERE run_id = ? AND locator_hash = ? LIMIT 1`,
			run.RunID, evidence.LocatorHash).Scan(&existingHash)
		if err == nil && existingHash != evidence.ContentHash {
			return ErrResearchEvidenceSourceChanged
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		subjectIDs, err := json.Marshal(evidence.SubjectIdentityIDs)
		if err != nil {
			return err
		}
		locator, err := json.Marshal(evidence.Locator)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO research_evidence (
			evidence_id, run_id, source_type, source_role, author_identity_id, subject_identity_ids_json,
			occurred_at, content_excerpt, locator_json, locator_hash, content_hash, privacy, selected, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			evidence.EvidenceID, run.RunID, evidence.SourceType, evidence.SourceRole, evidence.AuthorIdentityID,
			string(subjectIDs), evidence.OccurredAt, evidence.ContentExcerpt, string(locator), evidence.LocatorHash,
			evidence.ContentHash, evidence.Privacy, 1, now); err != nil {
			return err
		}
	}
	run.ActualScope.SearchedSources = mergeResearchScopeSources(run.ActualScope.SearchedSources, bundle.SearchedSources)
	run.ActualScope.CitedSources = mergeResearchScopeSources(run.ActualScope.CitedSources, bundle.CitedSources)
	scopeJSON, err := json.Marshal(run.ActualScope)
	if err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE research_runs SET actual_scope_json = ?, version = version + 1, updated_at = ?
		WHERE run_id = ? AND version = ?`, string(scopeJSON), now, run.RunID, run.Version)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrResearchRunVersionConflict
	}
	if err := insertResearchEvent(tx, run.RunID, run.Status, run.Status,
		ResearchTransition{Code: "evidence_stored", Actor: "orchestrator", Summary: fmt.Sprintf("stored %d selected evidence items", len(bundle.Evidence))}, now); err != nil {
		return err
	}
	run.Version++
	run.UpdatedAt = now
	return nil
}

func enforceResearchEvidenceBudgetTx(tx *sql.Tx, run *ResearchRun, incoming []ResearchEvidence) error {
	rows, err := tx.Query(`SELECT evidence_id, content_excerpt FROM research_evidence WHERE run_id = ?`, run.RunID)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	count := 0
	quotedRunes := 0
	for rows.Next() {
		var evidenceID, excerpt string
		if err := rows.Scan(&evidenceID, &excerpt); err != nil {
			_ = rows.Close()
			return err
		}
		existing[evidenceID] = true
		count++
		quotedRunes += len([]rune(excerpt))
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, evidence := range incoming {
		if existing[evidence.EvidenceID] {
			continue
		}
		existing[evidence.EvidenceID] = true
		count++
		quotedRunes += len([]rune(evidence.ContentExcerpt))
	}
	if count > run.Budget.MaxEvidenceItems || quotedRunes > run.Budget.MaxQuotedChars {
		return ErrResearchBudgetExhausted
	}
	return nil
}

func (s *ResearchStore) ListEvidence(runID string) ([]ResearchEvidence, error) {
	rows, err := s.db.Query(`SELECT evidence_id, source_type, source_role, author_identity_id,
		subject_identity_ids_json, occurred_at, content_excerpt, locator_json, locator_hash, content_hash,
		privacy, selected FROM research_evidence WHERE run_id = ? ORDER BY created_at ASC, evidence_id ASC`, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evidence := make([]ResearchEvidence, 0)
	for rows.Next() {
		var item ResearchEvidence
		var subjectIDs, locator string
		var selected int
		if err := rows.Scan(&item.EvidenceID, &item.SourceType, &item.SourceRole, &item.AuthorIdentityID,
			&subjectIDs, &item.OccurredAt, &item.ContentExcerpt, &locator, &item.LocatorHash, &item.ContentHash,
			&item.Privacy, &selected); err != nil {
			return nil, err
		}
		item.SchemaVersion = ResearchEvidenceSchemaVersion
		item.Selected = selected == 1
		if err := json.Unmarshal([]byte(subjectIDs), &item.SubjectIdentityIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(locator), &item.Locator); err != nil {
			return nil, err
		}
		evidence = append(evidence, item)
	}
	return evidence, rows.Err()
}

func mergeResearchScopeSources(existing, added []string) []string {
	merged := append([]string(nil), existing...)
	seen := make(map[string]bool, len(merged)+len(added))
	for _, source := range merged {
		seen[source] = true
	}
	for _, source := range added {
		if !seen[source] {
			seen[source] = true
			merged = append(merged, source)
		}
	}
	return merged
}

type researchRunValues struct {
	subjectIDs, requestedSources, routeReasons, actualScope, budget, failure string
}

func marshalResearchRunValues(run *ResearchRun) (researchRunValues, error) {
	var values researchRunValues
	fields := []struct {
		value  any
		target *string
	}{
		{run.SubjectIDs, &values.subjectIDs}, {run.RequestedSources, &values.requestedSources},
		{run.RouteReasons, &values.routeReasons}, {run.ActualScope, &values.actualScope}, {run.Budget, &values.budget},
	}
	for _, field := range fields {
		encoded, err := json.Marshal(field.value)
		if err != nil {
			return values, err
		}
		*field.target = string(encoded)
	}
	if run.Failure != nil {
		encoded, err := json.Marshal(run.Failure)
		if err != nil {
			return values, err
		}
		values.failure = string(encoded)
	}
	return values, nil
}

type researchRowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func loadResearchRun(queryer researchRowQuerier, runID string) (*ResearchRun, error) {
	var run ResearchRun
	var subjectIDs, requestedSources, routeReasons, actualScope, budget, failure string
	err := queryer.QueryRow(`SELECT schema_version, run_id, parent_run_id, preflight_id, mode, question, status, package_id, package_version,
		subject_ids_json, requested_sources_json, route_reasons_json, actual_scope_json, budget_json,
		wait_reason, failure_json, version, created_at, updated_at, lease_owner, lease_epoch, lease_expires_at
		FROM research_runs WHERE run_id = ?`, runID).Scan(
		&run.SchemaVersion, &run.RunID, &run.ParentRunID, &run.PreflightID, &run.Mode, &run.Question, &run.Status, &run.PackageID, &run.PackageVersion,
		&subjectIDs, &requestedSources, &routeReasons, &actualScope, &budget,
		&run.WaitReason, &failure, &run.Version, &run.CreatedAt, &run.UpdatedAt, &run.LeaseOwner, &run.LeaseEpoch, &run.LeaseExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrResearchRunNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, field := range []struct {
		data  string
		value any
	}{
		{subjectIDs, &run.SubjectIDs}, {requestedSources, &run.RequestedSources}, {routeReasons, &run.RouteReasons},
		{actualScope, &run.ActualScope}, {budget, &run.Budget},
	} {
		if err := json.Unmarshal([]byte(field.data), field.value); err != nil {
			return nil, err
		}
	}
	if failure != "" {
		run.Failure = &ResearchFailure{}
		if err := json.Unmarshal([]byte(failure), run.Failure); err != nil {
			return nil, err
		}
	}
	return &run, nil
}

func insertResearchEvent(tx *sql.Tx, runID string, from, to ResearchRunStatus, transition ResearchTransition, createdAt string) error {
	_, err := tx.Exec(`INSERT INTO research_events(run_id, from_status, to_status, code, actor, summary, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, runID, from, to, strings.TrimSpace(transition.Code), strings.TrimSpace(transition.Actor),
		strings.TrimSpace(transition.Summary), createdAt)
	return err
}

func validateResearchRunInput(input ResearchRunInput) error {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" || len([]rune(input.IdempotencyKey)) > researchIdempotencyKeyMax {
		return fmt.Errorf("idempotency_key is required and must not exceed %d characters", researchIdempotencyKeyMax)
	}
	if err := ValidateResearchRunRequest(input.Request); err != nil {
		return err
	}
	if input.Mode != ResearchModeQuick && input.Mode != ResearchModeDeep {
		return fmt.Errorf("resolved mode must be quick or deep")
	}
	if err := ValidateResearchModeScope(input.Request, input.Mode); err != nil {
		return err
	}
	if len(input.RouteReasons) == 0 || len(input.RouteReasons) > researchRouteReasonsMax {
		return fmt.Errorf("route_reasons must contain 1 to %d items", researchRouteReasonsMax)
	}
	budget := input.Budget
	if budget.MaxIterations <= 0 || budget.MaxIterations > researchBudgetMaxIterations ||
		budget.MaxEvidenceItems <= 0 || budget.MaxEvidenceItems > researchBudgetMaxEvidence ||
		budget.MaxQuotedChars <= 0 || budget.MaxQuotedChars > researchBudgetMaxQuotedChars ||
		budget.MaxModelCalls <= 0 || budget.MaxModelCalls > researchBudgetMaxModelCalls ||
		budget.MaxCostUSD <= 0 || budget.MaxCostUSD > researchBudgetMaxCostUSD {
		return fmt.Errorf("research budget is outside supported bounds")
	}
	return nil
}

func validateResearchTransitionInput(transition ResearchTransition) error {
	if strings.TrimSpace(transition.Code) == "" || strings.TrimSpace(transition.Actor) == "" {
		return fmt.Errorf("transition code and actor are required")
	}
	for name, value := range map[string]string{"code": transition.Code, "actor": transition.Actor, "summary": transition.Summary} {
		if len([]rune(value)) > researchEventFieldMaxRunes {
			return fmt.Errorf("transition %s exceeds %d characters", name, researchEventFieldMaxRunes)
		}
	}
	return nil
}

func hashResearchRunInput(input ResearchRunInput) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func newResearchRunID() string {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		digest := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
		copy(randomBytes[:], digest[:16])
	}
	return "research-run-" + hex.EncodeToString(randomBytes[:])
}
