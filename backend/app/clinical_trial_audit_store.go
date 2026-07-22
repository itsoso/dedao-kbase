package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
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
	clinicalTrialAuditDBName            = "clinical_trial_audits.sqlite3"
	clinicalTrialAuditJSONMaxBytes      = 1 << 20
	clinicalTrialAuditRequestMaxBytes   = 64 << 10
	clinicalTrialAuditErrorCodeMaxBytes = 64

	ClinicalTrialAuditErrorIdentifierInvalid   = "identifier_invalid"
	ClinicalTrialAuditErrorSourceNotFound      = "source_not_found"
	ClinicalTrialAuditErrorSourceRateLimited   = "source_rate_limited"
	ClinicalTrialAuditErrorSourceTimeout       = "source_timeout"
	ClinicalTrialAuditErrorSourceSchemaInvalid = "source_schema_invalid"
	ClinicalTrialAuditErrorModelTimeout        = "model_timeout"
	ClinicalTrialAuditErrorModelInvalidOutput  = "model_invalid_output"
	ClinicalTrialAuditErrorEvidenceInvalid     = "evidence_invalid"
	ClinicalTrialAuditErrorRetryExhausted      = "retry_exhausted"
	ClinicalTrialAuditErrorInternal            = "internal_error"
)

var (
	ErrClinicalTrialAuditRunNotFound         = errors.New("clinical trial audit run not found")
	ErrClinicalTrialAuditIdempotencyConflict = errors.New("clinical trial audit idempotency conflict")
	ErrClinicalTrialAuditLeaseOwner          = errors.New("clinical trial audit run belongs to another worker")
	ErrClinicalTrialAuditLeaseFence          = errors.New("clinical trial audit lease token is stale")
	ErrClinicalTrialAuditLeaseExpired        = errors.New("clinical trial audit run lease expired")
	ErrClinicalTrialAuditTerminal            = errors.New("clinical trial audit run is terminal")
	ErrClinicalTrialAuditInvalidTransition   = errors.New("invalid clinical trial audit transition")
	ErrClinicalTrialAuditNotRetryable        = errors.New("clinical trial audit run is not retryable")
)

type ClinicalTrialAuditStoredRun struct {
	SchemaVersion   string                        `json:"schema_version"`
	RunID           string                        `json:"run_id"`
	PackageID       string                        `json:"package_id"`
	PackageVersion  string                        `json:"package_version"`
	IdempotencyKey  string                        `json:"idempotency_key"`
	State           string                        `json:"state"`
	Request         ClinicalTrialAuditRequest     `json:"request"`
	Sources         []ClinicalTrialSourceSnapshot `json:"sources,omitempty"`
	Findings        []ClinicalTrialFinding        `json:"findings,omitempty"`
	Citations       []ClinicalTrialAuditCitation  `json:"citations,omitempty"`
	Audit           *ClinicalTrialAudit           `json:"audit,omitempty"`
	Attempt         int                           `json:"attempt"`
	Retryable       bool                          `json:"retryable"`
	ErrorCode       string                        `json:"error_code,omitempty"`
	LeaseOwner      string                        `json:"lease_owner,omitempty"`
	LeaseToken      string                        `json:"-"`
	LeaseGeneration int                           `json:"lease_generation,omitempty"`
	LeaseExpiresAt  string                        `json:"lease_expires_at,omitempty"`
	CreatedAt       string                        `json:"created_at"`
	UpdatedAt       string                        `json:"updated_at"`
	leaseTokenHash  string
}

type ClinicalTrialAuditCheckpoint struct {
	State     string                        `json:"state"`
	Sources   []ClinicalTrialSourceSnapshot `json:"sources,omitempty"`
	Findings  []ClinicalTrialFinding        `json:"findings,omitempty"`
	Citations []ClinicalTrialAuditCitation  `json:"citations,omitempty"`
	Audit     *ClinicalTrialAudit           `json:"audit,omitempty"`
	ErrorCode string                        `json:"error_code,omitempty"`
	Retryable bool                          `json:"retryable"`
}

type ClinicalTrialAuditListOptions struct {
	PackageID      string
	PackageVersion string
	Cursor         string
	Limit          int
}

type ClinicalTrialAuditPage struct {
	Runs       []ClinicalTrialAuditStoredRun `json:"runs"`
	NextCursor string                        `json:"next_cursor,omitempty"`
}

type ClinicalTrialAuditStore struct {
	root   string
	dbPath string
	now    func() time.Time
	db     *sql.DB
}

func NewClinicalTrialAuditStore(root string) (*ClinicalTrialAuditStore, error) {
	return newClinicalTrialAuditStore(root, time.Now)
}

func newClinicalTrialAuditStore(root string, now func() time.Time) (*ClinicalTrialAuditStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("clinical trial audit root is required")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(root, clinicalTrialAuditDBName)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := migrateClinicalTrialAuditDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return &ClinicalTrialAuditStore{root: root, dbPath: dbPath, now: now, db: db}, nil
}

func (s *ClinicalTrialAuditStore) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *ClinicalTrialAuditStore) DBPath() string {
	if s == nil {
		return ""
	}
	return s.dbPath
}

func (s *ClinicalTrialAuditStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func migrateClinicalTrialAuditDB(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS clinical_trial_audit_runs (
			run_id TEXT PRIMARY KEY,
			package_id TEXT NOT NULL,
			package_version TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_json TEXT NOT NULL,
			input_hash TEXT NOT NULL,
			state TEXT NOT NULL,
			resume_state TEXT NOT NULL DEFAULT '',
			attempt INTEGER NOT NULL DEFAULT 1,
			retryable INTEGER NOT NULL DEFAULT 0,
			error_code TEXT NOT NULL DEFAULT '',
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_token_hash TEXT NOT NULL DEFAULT '',
			lease_generation INTEGER NOT NULL DEFAULT 0,
			lease_expires_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_clinical_trial_audit_runs_queue
			ON clinical_trial_audit_runs(state, created_at, run_id);
		CREATE INDEX IF NOT EXISTS idx_clinical_trial_audit_runs_package
			ON clinical_trial_audit_runs(package_id, package_version, created_at DESC, run_id DESC);

		CREATE TABLE IF NOT EXISTS clinical_trial_audit_idempotency_keys (
			idempotency_key TEXT PRIMARY KEY,
			run_id TEXT NOT NULL UNIQUE,
			package_id TEXT NOT NULL,
			package_version TEXT NOT NULL,
			input_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES clinical_trial_audit_runs(run_id)
		);

		CREATE TABLE IF NOT EXISTS clinical_trial_audit_snapshots (
			run_id TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			snapshot_json TEXT NOT NULL,
			PRIMARY KEY(run_id, fingerprint),
			FOREIGN KEY(run_id) REFERENCES clinical_trial_audit_runs(run_id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS clinical_trial_audit_findings (
			run_id TEXT NOT NULL,
			finding_id TEXT NOT NULL,
			finding_json TEXT NOT NULL,
			PRIMARY KEY(run_id, finding_id),
			FOREIGN KEY(run_id) REFERENCES clinical_trial_audit_runs(run_id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS clinical_trial_audit_results (
			run_id TEXT PRIMARY KEY,
			citations_json TEXT NOT NULL DEFAULT '[]',
			audit_json TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(run_id) REFERENCES clinical_trial_audit_runs(run_id) ON DELETE CASCADE
		);
	`)
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"lease_token_hash", "TEXT NOT NULL DEFAULT ''"},
		{"lease_generation", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureClinicalTrialAuditColumn(db, "clinical_trial_audit_runs", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func ensureClinicalTrialAuditColumn(db *sql.DB, table, column, definition string) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	_, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

func (s *ClinicalTrialAuditStore) CreateRun(packageID, packageVersion string, request ClinicalTrialAuditRequest, idempotencyKey string) (ClinicalTrialAuditStoredRun, error) {
	packageID = strings.TrimSpace(packageID)
	packageVersion = strings.TrimSpace(packageVersion)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if packageID == "" || packageVersion == "" || idempotencyKey == "" {
		return ClinicalTrialAuditStoredRun{}, fmt.Errorf("package_id, package_version, and idempotency_key are required")
	}
	if len(packageID) > 200 || len(packageVersion) > 100 || len(idempotencyKey) > 256 {
		return ClinicalTrialAuditStoredRun{}, fmt.Errorf("clinical trial audit identity exceeds bounds")
	}
	idempotencyKey = hashClinicalTrialValue(idempotencyKey)
	finalized, err := FinalizeClinicalTrialAuditRequest(request)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	requestJSON, err := marshalBoundedClinicalTrialAuditJSON(finalized, clinicalTrialAuditRequestMaxBytes)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	defer tx.Rollback()

	existing, err := clinicalTrialAuditIdempotency(tx, idempotencyKey)
	if err == nil {
		if existing.packageID != packageID || existing.packageVersion != packageVersion || existing.inputHash != finalized.InputHash {
			return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return ClinicalTrialAuditStoredRun{}, err
		}
		return s.GetRun(existing.runID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ClinicalTrialAuditStoredRun{}, err
	}

	now := s.timestamp()
	runID := newClinicalTrialAuditStoreID(s.now())
	if _, err := tx.Exec(`
		INSERT INTO clinical_trial_audit_runs (
			run_id, package_id, package_version, idempotency_key, request_json,
			input_hash, state, attempt, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, runID, packageID, packageVersion, idempotencyKey, string(requestJSON), finalized.InputHash,
		ClinicalTrialAuditRunQueued, now, now); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if _, err := tx.Exec(`
		INSERT INTO clinical_trial_audit_idempotency_keys (
			idempotency_key, run_id, package_id, package_version, input_hash, created_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, idempotencyKey, runID, packageID, packageVersion, finalized.InputHash, now); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	return s.GetRun(runID)
}

func (s *ClinicalTrialAuditStore) GetRun(runID string) (ClinicalTrialAuditStoredRun, error) {
	run, err := scanClinicalTrialAuditStoredRun(s.db.QueryRow(clinicalTrialAuditRunSelect+` WHERE run_id = ?`, strings.TrimSpace(runID)))
	if errors.Is(err, sql.ErrNoRows) {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditRunNotFound
	}
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if err := s.loadClinicalTrialAuditRunData(&run); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	return run, nil
}

func (s *ClinicalTrialAuditStore) ListRuns(options ClinicalTrialAuditListOptions) (ClinicalTrialAuditPage, error) {
	limit := options.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 7)
	if packageID := strings.TrimSpace(options.PackageID); packageID != "" {
		conditions = append(conditions, "package_id = ?")
		args = append(args, packageID)
	}
	if version := strings.TrimSpace(options.PackageVersion); version != "" {
		conditions = append(conditions, "package_version = ?")
		args = append(args, version)
	}
	if strings.TrimSpace(options.Cursor) != "" {
		cursor, err := decodeClinicalTrialAuditCursor(options.Cursor)
		if err != nil {
			return ClinicalTrialAuditPage{}, err
		}
		conditions = append(conditions, "(created_at < ? OR (created_at = ? AND run_id < ?))")
		args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.RunID)
	}
	args = append(args, limit+1)
	rows, err := s.db.Query(clinicalTrialAuditRunSelect+` WHERE `+strings.Join(conditions, " AND ")+` ORDER BY created_at DESC, run_id DESC LIMIT ?`, args...)
	if err != nil {
		return ClinicalTrialAuditPage{}, err
	}
	defer rows.Close()
	runs := make([]ClinicalTrialAuditStoredRun, 0, limit+1)
	for rows.Next() {
		run, err := scanClinicalTrialAuditStoredRun(rows)
		if err != nil {
			return ClinicalTrialAuditPage{}, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return ClinicalTrialAuditPage{}, err
	}
	page := ClinicalTrialAuditPage{Runs: runs}
	if len(page.Runs) > limit {
		page.Runs = page.Runs[:limit]
		last := page.Runs[len(page.Runs)-1]
		page.NextCursor = encodeClinicalTrialAuditCursor(last.CreatedAt, last.RunID)
	}
	return page, nil
}

func (s *ClinicalTrialAuditStore) LeaseNextRun(workerID string, leaseDuration time.Duration) (*ClinicalTrialAuditStoredRun, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, fmt.Errorf("worker_id is required")
	}
	if len(workerID) > 200 {
		return nil, fmt.Errorf("worker_id exceeds bounds")
	}
	if leaseDuration <= 0 {
		leaseDuration = 2 * time.Minute
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := recoverExpiredClinicalTrialAuditLeases(tx, s.timestamp()); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	leaseToken := newClinicalTrialAuditLeaseToken(workerID, now)
	leaseTokenHash := hashClinicalTrialValue(leaseToken)
	var runID string
	err = tx.QueryRow(`
		UPDATE clinical_trial_audit_runs SET
			state = CASE WHEN resume_state = '' THEN ? ELSE resume_state END,
			resume_state = '', lease_owner = ?, lease_token_hash = ?,
			lease_generation = lease_generation + 1, lease_expires_at = ?, updated_at = ?
		WHERE run_id = (
			SELECT run_id FROM clinical_trial_audit_runs
			WHERE state = ? AND lease_owner = ''
			ORDER BY created_at, run_id LIMIT 1
		) AND state = ? AND lease_owner = ''
		RETURNING run_id
	`, ClinicalTrialAuditRunCollecting, workerID, leaseTokenHash, now.Add(leaseDuration).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), ClinicalTrialAuditRunQueued, ClinicalTrialAuditRunQueued).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	run, err := s.GetRun(runID)
	if err != nil {
		return nil, err
	}
	run.LeaseToken = leaseToken
	return &run, nil
}

func (s *ClinicalTrialAuditStore) CheckpointRun(runID, workerID, leaseToken string, checkpoint ClinicalTrialAuditCheckpoint) (ClinicalTrialAuditStoredRun, error) {
	runID = strings.TrimSpace(runID)
	workerID = strings.TrimSpace(workerID)
	leaseToken = strings.TrimSpace(leaseToken)
	checkpoint.State = strings.TrimSpace(checkpoint.State)
	if runID == "" || workerID == "" || leaseToken == "" || checkpoint.State == "" {
		return ClinicalTrialAuditStoredRun{}, fmt.Errorf("run_id, worker_id, lease_token, and checkpoint state are required")
	}
	if checkpoint.State == ClinicalTrialAuditRunFailed {
		if err := validateClinicalTrialAuditErrorCode(checkpoint.ErrorCode); err != nil {
			return ClinicalTrialAuditStoredRun{}, err
		}
	} else if strings.TrimSpace(checkpoint.ErrorCode) != "" {
		return ClinicalTrialAuditStoredRun{}, fmt.Errorf("error_code is only valid for failed checkpoints")
	}
	if err := validateClinicalTrialAuditCheckpointBounds(checkpoint); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if err := validateClinicalTrialAuditCheckpointEvidence(checkpoint); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	defer tx.Rollback()
	run, err := getClinicalTrialAuditRunTx(tx, runID)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if isTerminalClinicalTrialAuditState(run.State) {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditTerminal
	}
	if run.LeaseOwner != workerID {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditLeaseOwner
	}
	leaseTokenHash := hashClinicalTrialValue(leaseToken)
	if run.leaseTokenHash == "" || run.leaseTokenHash != leaseTokenHash {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditLeaseFence
	}
	if clinicalTrialAuditLeaseExpired(run.LeaseExpiresAt, s.now()) {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditLeaseExpired
	}
	if !validClinicalTrialAuditTransition(run.State, checkpoint.State) {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditInvalidTransition
	}

	if checkpoint.Audit != nil {
		audit := *checkpoint.Audit
		completedAt, err := canonicalClinicalTrialTimestamp("completed_at", audit.CompletedAt, true)
		if err != nil {
			return ClinicalTrialAuditStoredRun{}, err
		}
		audit.CompletedAt = completedAt
		contractRun := ClinicalTrialAuditRun{
			SchemaVersion: ClinicalTrialAuditRunSchemaVersion,
			RunID:         run.RunID, State: checkpoint.State, Request: run.Request, Audit: &audit,
			CreatedAt: run.CreatedAt, UpdatedAt: s.timestamp(),
		}
		if _, err := FinalizeClinicalTrialAuditRun(contractRun); err != nil {
			return ClinicalTrialAuditStoredRun{}, err
		}
		checkpoint.Audit = &audit
		checkpoint.Sources = audit.Sources
		checkpoint.Findings = audit.Findings
		checkpoint.Citations = audit.Citations
	} else if checkpoint.State == ClinicalTrialAuditRunCompleted || checkpoint.State == ClinicalTrialAuditRunAbstained {
		return ClinicalTrialAuditStoredRun{}, fmt.Errorf("terminal audit checkpoint requires audit")
	}

	if err := persistClinicalTrialAuditCheckpoint(tx, runID, checkpoint); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	now := s.timestamp()
	leaseOwner := run.LeaseOwner
	storedLeaseTokenHash := run.leaseTokenHash
	leaseExpiresAt := run.LeaseExpiresAt
	if isTerminalClinicalTrialAuditState(checkpoint.State) {
		leaseOwner = ""
		storedLeaseTokenHash = ""
		leaseExpiresAt = ""
	}
	result, err := tx.Exec(`
		UPDATE clinical_trial_audit_runs SET state = ?, retryable = ?, error_code = ?,
			lease_owner = ?, lease_token_hash = ?, lease_expires_at = ?, updated_at = ?
		WHERE run_id = ? AND state = ? AND lease_owner = ? AND lease_token_hash = ?
	`, checkpoint.State, boolToClinicalTrialAuditInt(checkpoint.Retryable), strings.TrimSpace(checkpoint.ErrorCode),
		leaseOwner, storedLeaseTokenHash, leaseExpiresAt, now, runID, run.State, workerID, leaseTokenHash)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditInvalidTransition
	}
	if err := tx.Commit(); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	return s.GetRun(runID)
}

func (s *ClinicalTrialAuditStore) RecoverExpiredLeases() (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	count, err := recoverExpiredClinicalTrialAuditLeases(tx, s.timestamp())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *ClinicalTrialAuditStore) RetryRun(runID string) (ClinicalTrialAuditStoredRun, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	defer tx.Rollback()
	run, err := getClinicalTrialAuditRunTx(tx, strings.TrimSpace(runID))
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if run.State != ClinicalTrialAuditRunFailed || !run.Retryable {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditNotRetryable
	}
	now := s.timestamp()
	result, err := tx.Exec(`
		UPDATE clinical_trial_audit_runs SET state = ?, resume_state = '', attempt = attempt + 1,
			error_code = '', lease_owner = '', lease_token_hash = '', lease_expires_at = '', updated_at = ?
		WHERE run_id = ? AND state = ? AND retryable = 1
	`, ClinicalTrialAuditRunQueued, now, run.RunID, ClinicalTrialAuditRunFailed)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditNotRetryable
	}
	if err := tx.Commit(); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	return s.GetRun(run.RunID)
}

const clinicalTrialAuditRunSelect = `
	SELECT run_id, package_id, package_version, idempotency_key, request_json,
		state, attempt, retryable, error_code, lease_owner, lease_token_hash,
		lease_generation, lease_expires_at,
		created_at, updated_at
	FROM clinical_trial_audit_runs`

type clinicalTrialAuditScanner interface {
	Scan(dest ...any) error
}

func scanClinicalTrialAuditStoredRun(row clinicalTrialAuditScanner) (ClinicalTrialAuditStoredRun, error) {
	var run ClinicalTrialAuditStoredRun
	var requestJSON string
	var retryable int
	err := row.Scan(&run.RunID, &run.PackageID, &run.PackageVersion, &run.IdempotencyKey, &requestJSON,
		&run.State, &run.Attempt, &retryable, &run.ErrorCode, &run.LeaseOwner, &run.leaseTokenHash,
		&run.LeaseGeneration, &run.LeaseExpiresAt,
		&run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if err := json.Unmarshal([]byte(requestJSON), &run.Request); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	run.SchemaVersion = ClinicalTrialAuditRunSchemaVersion
	run.Retryable = retryable == 1
	return run, nil
}

func getClinicalTrialAuditRunTx(tx *sql.Tx, runID string) (ClinicalTrialAuditStoredRun, error) {
	run, err := scanClinicalTrialAuditStoredRun(tx.QueryRow(clinicalTrialAuditRunSelect+` WHERE run_id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditRunNotFound
	}
	return run, err
}

func (s *ClinicalTrialAuditStore) loadClinicalTrialAuditRunData(run *ClinicalTrialAuditStoredRun) error {
	rows, err := s.db.Query(`SELECT snapshot_json FROM clinical_trial_audit_snapshots WHERE run_id = ? ORDER BY fingerprint`, run.RunID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			rows.Close()
			return err
		}
		var snapshot ClinicalTrialSourceSnapshot
		if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
			rows.Close()
			return err
		}
		run.Sources = append(run.Sources, snapshot)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = s.db.Query(`SELECT finding_json FROM clinical_trial_audit_findings WHERE run_id = ? ORDER BY finding_id`, run.RunID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			rows.Close()
			return err
		}
		var finding ClinicalTrialFinding
		if err := json.Unmarshal([]byte(encoded), &finding); err != nil {
			rows.Close()
			return err
		}
		run.Findings = append(run.Findings, finding)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var citationsJSON, auditJSON string
	err = s.db.QueryRow(`SELECT citations_json, audit_json FROM clinical_trial_audit_results WHERE run_id = ?`, run.RunID).Scan(&citationsJSON, &auditJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(citationsJSON), &run.Citations); err != nil {
		return err
	}
	if auditJSON != "" {
		var audit ClinicalTrialAudit
		if err := json.Unmarshal([]byte(auditJSON), &audit); err != nil {
			return err
		}
		run.Audit = &audit
	}
	return nil
}

func persistClinicalTrialAuditCheckpoint(tx *sql.Tx, runID string, checkpoint ClinicalTrialAuditCheckpoint) error {
	if checkpoint.Sources != nil {
		if _, err := tx.Exec(`DELETE FROM clinical_trial_audit_snapshots WHERE run_id = ?`, runID); err != nil {
			return err
		}
		for _, snapshot := range checkpoint.Sources {
			finalized, err := FinalizeClinicalTrialSourceSnapshot(snapshot)
			if err != nil || finalized.Fingerprint != snapshot.Fingerprint {
				if err == nil {
					err = fmt.Errorf("source fingerprint does not match source content")
				}
				return err
			}
			encoded, err := marshalBoundedClinicalTrialAuditJSON(finalized, clinicalTrialAuditJSONMaxBytes)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO clinical_trial_audit_snapshots (run_id, fingerprint, snapshot_json) VALUES (?, ?, ?)`, runID, finalized.Fingerprint, string(encoded)); err != nil {
				return err
			}
		}
	}
	if checkpoint.Findings != nil {
		if _, err := tx.Exec(`DELETE FROM clinical_trial_audit_findings WHERE run_id = ?`, runID); err != nil {
			return err
		}
		for _, finding := range checkpoint.Findings {
			if strings.TrimSpace(finding.FindingID) == "" || strings.TrimSpace(finding.Summary) == "" || !isClinicalTrialFindingClass(finding.Class) {
				return fmt.Errorf("invalid clinical trial finding checkpoint")
			}
			encoded, err := marshalBoundedClinicalTrialAuditJSON(finding, clinicalTrialAuditJSONMaxBytes)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO clinical_trial_audit_findings (run_id, finding_id, finding_json) VALUES (?, ?, ?)`, runID, finding.FindingID, string(encoded)); err != nil {
				return err
			}
		}
	}
	if checkpoint.Citations != nil || checkpoint.Audit != nil {
		citationsJSON, err := marshalBoundedClinicalTrialAuditJSON(checkpoint.Citations, clinicalTrialAuditJSONMaxBytes)
		if err != nil {
			return err
		}
		auditJSON := ""
		if checkpoint.Audit != nil {
			encoded, err := marshalBoundedClinicalTrialAuditJSON(checkpoint.Audit, clinicalTrialAuditJSONMaxBytes)
			if err != nil {
				return err
			}
			auditJSON = string(encoded)
		}
		if _, err := tx.Exec(`
			INSERT INTO clinical_trial_audit_results (run_id, citations_json, audit_json)
			VALUES (?, ?, ?)
			ON CONFLICT(run_id) DO UPDATE SET citations_json = excluded.citations_json,
				audit_json = CASE WHEN excluded.audit_json = '' THEN clinical_trial_audit_results.audit_json ELSE excluded.audit_json END
		`, runID, string(citationsJSON), auditJSON); err != nil {
			return err
		}
	}
	return nil
}

func validateClinicalTrialAuditCheckpointBounds(checkpoint ClinicalTrialAuditCheckpoint) error {
	for _, value := range []any{checkpoint.Sources, checkpoint.Findings, checkpoint.Citations, checkpoint.Audit} {
		if _, err := marshalBoundedClinicalTrialAuditJSON(value, clinicalTrialAuditJSONMaxBytes); err != nil {
			return err
		}
	}
	return nil
}

func validateClinicalTrialAuditCheckpointEvidence(checkpoint ClinicalTrialAuditCheckpoint) error {
	if checkpoint.State == ClinicalTrialAuditRunComparing && len(checkpoint.Sources) == 0 {
		return fmt.Errorf("comparing checkpoint requires source snapshots")
	}
	if checkpoint.State != ClinicalTrialAuditRunReasoning {
		return nil
	}
	if len(checkpoint.Sources) == 0 || len(checkpoint.Findings) == 0 || len(checkpoint.Citations) == 0 {
		return fmt.Errorf("reasoning checkpoint requires sources, findings, and citations")
	}
	sources := make(map[string]struct{}, len(checkpoint.Sources))
	for _, snapshot := range checkpoint.Sources {
		finalized, err := FinalizeClinicalTrialSourceSnapshot(snapshot)
		if err != nil || finalized.Fingerprint != snapshot.Fingerprint {
			return fmt.Errorf("invalid source snapshot in reasoning checkpoint")
		}
		if _, exists := sources[snapshot.Fingerprint]; exists {
			return fmt.Errorf("duplicate source snapshot in reasoning checkpoint")
		}
		sources[snapshot.Fingerprint] = struct{}{}
	}
	citations := make(map[string]struct{}, len(checkpoint.Citations))
	for _, citation := range checkpoint.Citations {
		if strings.TrimSpace(citation.CitationID) == "" || strings.TrimSpace(citation.Locator) == "" {
			return fmt.Errorf("invalid citation in reasoning checkpoint")
		}
		if _, exists := sources[citation.SourceFingerprint]; !exists {
			return fmt.Errorf("citation references unknown source snapshot")
		}
		if _, exists := citations[citation.CitationID]; exists {
			return fmt.Errorf("duplicate citation in reasoning checkpoint")
		}
		citations[citation.CitationID] = struct{}{}
	}
	findings := make(map[string]struct{}, len(checkpoint.Findings))
	for _, finding := range checkpoint.Findings {
		if strings.TrimSpace(finding.FindingID) == "" || strings.TrimSpace(finding.Summary) == "" ||
			!isClinicalTrialFindingClass(finding.Class) || len(finding.CitationIDs) == 0 {
			return fmt.Errorf("invalid finding in reasoning checkpoint")
		}
		if _, exists := findings[finding.FindingID]; exists {
			return fmt.Errorf("duplicate finding in reasoning checkpoint")
		}
		findings[finding.FindingID] = struct{}{}
		seen := make(map[string]struct{}, len(finding.CitationIDs))
		for _, citationID := range finding.CitationIDs {
			if _, exists := citations[citationID]; !exists {
				return fmt.Errorf("finding references unknown citation")
			}
			if _, exists := seen[citationID]; exists {
				return fmt.Errorf("finding contains duplicate citation")
			}
			seen[citationID] = struct{}{}
		}
	}
	return nil
}

func validClinicalTrialAuditTransition(from, to string) bool {
	if to == ClinicalTrialAuditRunFailed {
		return isActiveClinicalTrialAuditState(from)
	}
	switch from {
	case ClinicalTrialAuditRunCollecting:
		return to == ClinicalTrialAuditRunComparing
	case ClinicalTrialAuditRunComparing:
		return to == ClinicalTrialAuditRunReasoning
	case ClinicalTrialAuditRunReasoning:
		return to == ClinicalTrialAuditRunAwaitingReview
	case ClinicalTrialAuditRunAwaitingReview:
		return to == ClinicalTrialAuditRunCompleted || to == ClinicalTrialAuditRunAbstained
	default:
		return false
	}
}

func isActiveClinicalTrialAuditState(state string) bool {
	switch state {
	case ClinicalTrialAuditRunCollecting, ClinicalTrialAuditRunComparing, ClinicalTrialAuditRunReasoning, ClinicalTrialAuditRunAwaitingReview:
		return true
	default:
		return false
	}
}

func isTerminalClinicalTrialAuditState(state string) bool {
	switch state {
	case ClinicalTrialAuditRunCompleted, ClinicalTrialAuditRunFailed, ClinicalTrialAuditRunAbstained:
		return true
	default:
		return false
	}
}

func recoverExpiredClinicalTrialAuditLeases(tx *sql.Tx, now string) (int64, error) {
	result, err := tx.Exec(`
		UPDATE clinical_trial_audit_runs SET resume_state = state, state = ?, lease_owner = '',
			lease_token_hash = '', lease_expires_at = '', updated_at = ?
		WHERE state IN (?, ?, ?, ?) AND lease_owner != '' AND lease_expires_at != ''
			AND julianday(lease_expires_at) <= julianday(?)
	`, ClinicalTrialAuditRunQueued, now, ClinicalTrialAuditRunCollecting, ClinicalTrialAuditRunComparing,
		ClinicalTrialAuditRunReasoning, ClinicalTrialAuditRunAwaitingReview, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func clinicalTrialAuditLeaseExpired(value string, now time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return err != nil || !expiresAt.After(now.UTC())
}

func validateClinicalTrialAuditErrorCode(code string) error {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > clinicalTrialAuditErrorCodeMaxBytes {
		return fmt.Errorf("clinical trial audit error code must be an allowlisted bounded identifier")
	}
	switch code {
	case ClinicalTrialAuditErrorIdentifierInvalid,
		ClinicalTrialAuditErrorSourceNotFound,
		ClinicalTrialAuditErrorSourceRateLimited,
		ClinicalTrialAuditErrorSourceTimeout,
		ClinicalTrialAuditErrorSourceSchemaInvalid,
		ClinicalTrialAuditErrorModelTimeout,
		ClinicalTrialAuditErrorModelInvalidOutput,
		ClinicalTrialAuditErrorEvidenceInvalid,
		ClinicalTrialAuditErrorRetryExhausted,
		ClinicalTrialAuditErrorInternal:
		return nil
	default:
		return fmt.Errorf("unsupported clinical trial audit error code %q", code)
	}
}

func marshalBoundedClinicalTrialAuditJSON(value any, limit int) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(encoded) > limit {
		return nil, fmt.Errorf("clinical trial audit structured data exceeds %d bytes", limit)
	}
	return encoded, nil
}

type clinicalTrialAuditIdempotencyRecord struct {
	runID, packageID, packageVersion, inputHash string
}

func clinicalTrialAuditIdempotency(tx *sql.Tx, key string) (clinicalTrialAuditIdempotencyRecord, error) {
	var record clinicalTrialAuditIdempotencyRecord
	err := tx.QueryRow(`
		SELECT run_id, package_id, package_version, input_hash
		FROM clinical_trial_audit_idempotency_keys WHERE idempotency_key = ?
	`, key).Scan(&record.runID, &record.packageID, &record.packageVersion, &record.inputHash)
	return record, err
}

type clinicalTrialAuditCursor struct {
	CreatedAt string `json:"created_at"`
	RunID     string `json:"run_id"`
}

func encodeClinicalTrialAuditCursor(createdAt, runID string) string {
	encoded, _ := json.Marshal(clinicalTrialAuditCursor{CreatedAt: createdAt, RunID: runID})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeClinicalTrialAuditCursor(value string) (clinicalTrialAuditCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return clinicalTrialAuditCursor{}, fmt.Errorf("invalid clinical trial audit cursor")
	}
	var cursor clinicalTrialAuditCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || strings.TrimSpace(cursor.CreatedAt) == "" || strings.TrimSpace(cursor.RunID) == "" {
		return clinicalTrialAuditCursor{}, fmt.Errorf("invalid clinical trial audit cursor")
	}
	if _, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt); err != nil {
		return clinicalTrialAuditCursor{}, fmt.Errorf("invalid clinical trial audit cursor")
	}
	return cursor, nil
}

func (s *ClinicalTrialAuditStore) timestamp() string {
	return s.now().UTC().Format(time.RFC3339Nano)
}

func newClinicalTrialAuditStoreID(now time.Time) string {
	var randomBytes [8]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "clinical-audit-" + now.UTC().Format("20060102T150405.000000000Z")
	}
	return "clinical-audit-" + hex.EncodeToString(randomBytes[:])
}

func newClinicalTrialAuditLeaseToken(workerID string, now time.Time) string {
	var randomBytes [24]byte
	if _, err := rand.Read(randomBytes[:]); err == nil {
		return base64.RawURLEncoding.EncodeToString(randomBytes[:])
	}
	return hashClinicalTrialValue("lease\x00" + workerID + "\x00" + now.UTC().Format(time.RFC3339Nano))
}

func boolToClinicalTrialAuditInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
