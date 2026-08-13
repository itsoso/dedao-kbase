package app

import (
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
	db, err := sql.Open("sqlite3", dbPath)
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
	return &ResearchStore{dbPath: dbPath, now: now, db: db}, nil
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
		`CREATE TABLE IF NOT EXISTS research_steps (
			step_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			stage TEXT NOT NULL, status TEXT NOT NULL, decision_summary TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '', completed_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS research_evidence (
			evidence_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			source_type TEXT NOT NULL, source_role TEXT NOT NULL, author_identity_id TEXT NOT NULL DEFAULT '',
			subject_identity_ids_json TEXT NOT NULL DEFAULT '[]', occurred_at TEXT NOT NULL DEFAULT '',
			content_excerpt TEXT NOT NULL DEFAULT '', locator_json TEXT NOT NULL, locator_hash TEXT NOT NULL,
			content_hash TEXT NOT NULL, privacy TEXT NOT NULL, selected INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
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
			conclusion_text TEXT NOT NULL, evidence_ids_json TEXT NOT NULL DEFAULT '[]', confidence REAL NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS research_worker_jobs (
			job_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			tool TEXT NOT NULL, arguments_json TEXT NOT NULL, state TEXT NOT NULL, attempt INTEGER NOT NULL DEFAULT 0,
			lease_owner TEXT NOT NULL DEFAULT '', lease_expires_at TEXT NOT NULL DEFAULT '', request_hash TEXT NOT NULL,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_worker_jobs_state_lease ON research_worker_jobs(state, lease_expires_at, created_at)`,
		`CREATE TABLE IF NOT EXISTS research_model_invocations (
			invocation_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			request_identity TEXT NOT NULL UNIQUE, model TEXT NOT NULL, purpose TEXT NOT NULL,
			status TEXT NOT NULL, input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
			estimated_cost_usd REAL NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_model_request_identity ON research_model_invocations(request_identity)`,
		`CREATE TABLE IF NOT EXISTS research_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT OR IGNORE INTO research_meta(key, value) VALUES ('schema_version', '1')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
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
		Question: strings.TrimSpace(input.Request.Question), Status: ResearchPlanning,
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
		run_id, idempotency_key, request_hash, schema_version, mode, question, status,
		package_id, package_version, subject_ids_json, requested_sources_json, route_reasons_json,
		actual_scope_json, budget_json, wait_reason, failure_json, version, created_at, updated_at,
		lease_owner, lease_expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunID, input.IdempotencyKey, requestHash, run.SchemaVersion, run.Mode, run.Question, run.Status,
		run.PackageID, run.PackageVersion, values.subjectIDs, values.requestedSources, values.routeReasons,
		values.actualScope, values.budget, run.WaitReason, values.failure, run.Version, run.CreatedAt, run.UpdatedAt,
		run.LeaseOwner, run.LeaseExpiresAt)
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
	if err := ValidateResearchTransition(run.Status, to); err != nil {
		return nil, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE research_runs SET status = ?, version = version + 1, updated_at = ? WHERE run_id = ? AND version = ?`,
		to, now, run.RunID, expectedVersion)
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
	if _, err := tx.Exec(`UPDATE research_runs SET lease_owner = ?, lease_expires_at = ?, updated_at = ? WHERE run_id = ?`, owner, expires, now, runID); err != nil {
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
	run.LeaseExpiresAt = expires
	run.UpdatedAt = now
	return run, nil
}

func (s *ResearchStore) RenewRunLease(runID, owner string, lease time.Duration) error {
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
	nowTime := s.now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	expires := nowTime.Add(lease).Format(time.RFC3339Nano)
	if _, err := tx.Exec(`UPDATE research_runs SET lease_expires_at = ?, updated_at = ? WHERE run_id = ? AND lease_owner = ?`, expires, now, run.RunID, owner); err != nil {
		return err
	}
	if err := insertResearchEvent(tx, run.RunID, run.Status, run.Status,
		ResearchTransition{Code: "lease_renewed", Actor: owner}, now); err != nil {
		return err
	}
	return tx.Commit()
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
	err := queryer.QueryRow(`SELECT schema_version, run_id, mode, question, status, package_id, package_version,
		subject_ids_json, requested_sources_json, route_reasons_json, actual_scope_json, budget_json,
		wait_reason, failure_json, version, created_at, updated_at, lease_owner, lease_expires_at
		FROM research_runs WHERE run_id = ?`, runID).Scan(
		&run.SchemaVersion, &run.RunID, &run.Mode, &run.Question, &run.Status, &run.PackageID, &run.PackageVersion,
		&subjectIDs, &requestedSources, &routeReasons, &actualScope, &budget,
		&run.WaitReason, &failure, &run.Version, &run.CreatedAt, &run.UpdatedAt, &run.LeaseOwner, &run.LeaseExpiresAt)
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
