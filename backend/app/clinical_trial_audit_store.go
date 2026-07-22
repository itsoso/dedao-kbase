package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	clinicalTrialAuditDBName                 = "clinical_trial_audits.sqlite3"
	clinicalTrialAuditJSONMaxBytes           = 1 << 20
	clinicalTrialAuditRequestMaxBytes        = 64 << 10
	clinicalTrialAuditErrorCodeMaxBytes      = 64
	clinicalTrialAuditCursorEncodedMaxBytes  = 1024
	clinicalTrialAuditCursorDecodedMaxBytes  = 512
	clinicalTrialAuditSchemaVersion          = 4
	ClinicalTrialAuditStoredRunSchemaVersion = "clinical-trial-audit-stored-run.v1"
	ClinicalTrialAuditEvidenceSchemaVersion  = "clinical-trial-normalized-evidence.v1"
	clinicalTrialAuditEvidenceV1Incompatible = "evidence_v1_incompatible"

	ClinicalTrialAuditErrorIdentifierInvalid        = "identifier_invalid"
	ClinicalTrialAuditErrorSourceNotFound           = "source_not_found"
	ClinicalTrialAuditErrorSourceRateLimited        = "source_rate_limited"
	ClinicalTrialAuditErrorSourceTimeout            = "source_timeout"
	ClinicalTrialAuditErrorSourceCanceled           = "source_canceled"
	ClinicalTrialAuditErrorSourceResponseTooLarge   = "source_response_too_large"
	ClinicalTrialAuditErrorSourceMalformedJSON      = "source_malformed_json"
	ClinicalTrialAuditErrorSourceIdentifierMismatch = "source_identifier_mismatch"
	ClinicalTrialAuditErrorSourceSchemaInvalid      = "source_schema_invalid"
	ClinicalTrialAuditErrorSourceUpstreamPermanent  = "source_upstream_permanent"
	ClinicalTrialAuditErrorSourceUpstreamTransient  = "source_upstream_transient"
	ClinicalTrialAuditErrorModelTimeout             = "model_timeout"
	ClinicalTrialAuditErrorModelInvalidOutput       = "model_invalid_output"
	ClinicalTrialAuditErrorEvidenceInvalid          = "evidence_invalid"
	ClinicalTrialAuditErrorRetryExhausted           = "retry_exhausted"
	ClinicalTrialAuditErrorInternal                 = "internal_error"
)

var (
	ErrClinicalTrialAuditRunNotFound          = errors.New("clinical trial audit run not found")
	ErrClinicalTrialAuditIdempotencyConflict  = errors.New("clinical trial audit idempotency conflict")
	ErrClinicalTrialAuditLeaseOwner           = errors.New("clinical trial audit run belongs to another worker")
	ErrClinicalTrialAuditLeaseFence           = errors.New("clinical trial audit lease token is stale")
	ErrClinicalTrialAuditLeaseExpired         = errors.New("clinical trial audit run lease expired")
	ErrClinicalTrialAuditTerminal             = errors.New("clinical trial audit run is terminal")
	ErrClinicalTrialAuditInvalidTransition    = errors.New("invalid clinical trial audit transition")
	ErrClinicalTrialAuditNotRetryable         = errors.New("clinical trial audit run is not retryable")
	ErrClinicalTrialAuditEvidenceIncompatible = errors.New("clinical trial audit evidence is incompatible")
	errClinicalTrialAuditRandomUnavailable    = errors.New("clinical trial audit secure randomness unavailable")
)

type ClinicalTrialAuditStoredRun struct {
	SchemaVersion   string                              `json:"schema_version"`
	RunID           string                              `json:"run_id"`
	PackageID       string                              `json:"package_id"`
	PackageVersion  string                              `json:"package_version"`
	IdempotencyKey  string                              `json:"idempotency_key"`
	State           string                              `json:"state"`
	Request         ClinicalTrialAuditRequest           `json:"request"`
	Sources         []ClinicalTrialSourceSnapshot       `json:"sources,omitempty"`
	Findings        []ClinicalTrialFinding              `json:"findings,omitempty"`
	Citations       []ClinicalTrialAuditCitation        `json:"citations,omitempty"`
	Evidence        []ClinicalTrialAuditEvidencePayload `json:"evidence,omitempty"`
	Audit           *ClinicalTrialAudit                 `json:"audit,omitempty"`
	Attempt         int                                 `json:"attempt"`
	Retryable       bool                                `json:"retryable"`
	ErrorCode       string                              `json:"error_code,omitempty"`
	LeaseOwner      string                              `json:"lease_owner,omitempty"`
	LeaseToken      string                              `json:"-"`
	LeaseGeneration int                                 `json:"lease_generation,omitempty"`
	LeaseExpiresAt  string                              `json:"lease_expires_at,omitempty"`
	CreatedAt       string                              `json:"created_at"`
	UpdatedAt       string                              `json:"updated_at"`
	leaseTokenHash  string
}

type ClinicalTrialAuditEvidencePayload struct {
	SchemaVersion string          `json:"schema_version"`
	SourceType    string          `json:"source_type"`
	ContentHash   string          `json:"content_hash"`
	Data          json.RawMessage `json:"data"`
}

type ClinicalTrialAuditFailure struct {
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

func ProjectClinicalTrialAuditRun(stored ClinicalTrialAuditStoredRun) (ClinicalTrialAuditRun, error) {
	if stored.SchemaVersion != ClinicalTrialAuditStoredRunSchemaVersion {
		return ClinicalTrialAuditRun{}, fmt.Errorf("stored schema_version must be %q", ClinicalTrialAuditStoredRunSchemaVersion)
	}
	publicRun := ClinicalTrialAuditRun{
		SchemaVersion: ClinicalTrialAuditRunSchemaVersion,
		RunID:         stored.RunID,
		State:         stored.State,
		Request:       stored.Request,
		Audit:         stored.Audit,
		CreatedAt:     stored.CreatedAt,
		UpdatedAt:     stored.UpdatedAt,
	}
	if stored.State == ClinicalTrialAuditRunFailed {
		if err := validateClinicalTrialAuditFailure(stored.ErrorCode, stored.Retryable); err != nil {
			return ClinicalTrialAuditRun{}, err
		}
		errorCode := stored.ErrorCode
		publicRun.Error = &errorCode
	}
	return FinalizeClinicalTrialAuditRun(publicRun)
}

type ClinicalTrialAuditCheckpoint struct {
	State     string                              `json:"state"`
	Sources   []ClinicalTrialSourceSnapshot       `json:"sources,omitempty"`
	Findings  []ClinicalTrialFinding              `json:"findings,omitempty"`
	Citations []ClinicalTrialAuditCitation        `json:"citations,omitempty"`
	Evidence  []ClinicalTrialAuditEvidencePayload `json:"evidence,omitempty"`
	Audit     *ClinicalTrialAudit                 `json:"audit,omitempty"`
	ErrorCode string                              `json:"error_code,omitempty"`
	Retryable bool                                `json:"retryable"`
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
	root                   string
	dbPath                 string
	now                    func() time.Time
	random                 io.Reader
	db                     *sql.DB
	afterRunRowRead        func()
	beforeLeaseFinalUpdate func()
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
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateClinicalTrialAuditDB(db); err != nil {
		db.Close()
		return nil, err
	}
	for _, statement := range []string{`PRAGMA foreign_keys = ON`, `PRAGMA journal_mode = WAL`} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &ClinicalTrialAuditStore{root: root, dbPath: dbPath, now: now, random: rand.Reader, db: db}, nil
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
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN EXCLUSIVE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()
	var currentVersion int
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&currentVersion); err != nil {
		return err
	}
	if currentVersion > clinicalTrialAuditSchemaVersion {
		return fmt.Errorf("clinical trial audit schema version %d is newer than supported version %d", currentVersion, clinicalTrialAuditSchemaVersion)
	}
	_, err = conn.ExecContext(ctx, `
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

		CREATE TABLE IF NOT EXISTS clinical_trial_audit_incompatible_runs (
			run_id TEXT PRIMARY KEY,
			incompatibility_code TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES clinical_trial_audit_runs(run_id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS clinical_trial_audit_evidence (
			content_hash TEXT PRIMARY KEY,
			source_type TEXT NOT NULL,
			evidence_json TEXT NOT NULL,
			compatible INTEGER NOT NULL DEFAULT 1,
			incompatibility_code TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS clinical_trial_audit_snapshot_evidence (
			run_id TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			retrieved_at TEXT NOT NULL DEFAULT '',
			data_timestamp TEXT NOT NULL DEFAULT '',
			provenance_digest TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(run_id, fingerprint),
			FOREIGN KEY(run_id, fingerprint) REFERENCES clinical_trial_audit_snapshots(run_id, fingerprint) ON DELETE CASCADE,
			FOREIGN KEY(content_hash) REFERENCES clinical_trial_audit_evidence(content_hash)
		);
	`)
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"lease_token_hash", "TEXT NOT NULL DEFAULT ''"},
		{"lease_generation", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureClinicalTrialAuditColumn(ctx, conn, "clinical_trial_audit_runs", column.name, column.definition); err != nil {
			return err
		}
	}
	for _, column := range []struct{ table, name, definition string }{
		{"clinical_trial_audit_evidence", "compatible", "INTEGER NOT NULL DEFAULT 1"},
		{"clinical_trial_audit_evidence", "incompatibility_code", "TEXT NOT NULL DEFAULT ''"},
		{"clinical_trial_audit_snapshot_evidence", "retrieved_at", "TEXT NOT NULL DEFAULT ''"},
		{"clinical_trial_audit_snapshot_evidence", "data_timestamp", "TEXT NOT NULL DEFAULT ''"},
		{"clinical_trial_audit_snapshot_evidence", "provenance_digest", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureClinicalTrialAuditColumn(ctx, conn, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	if currentVersion == 3 {
		if err := migrateClinicalTrialAuditV3Evidence(ctx, conn); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, clinicalTrialAuditSchemaVersion)); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

type clinicalTrialAuditV3EvidenceMigration struct {
	oldHash, newHash, encoded, dataTimestamp string
	compatible                               bool
}

type clinicalTrialAuditV3SnapshotMigration struct {
	oldFingerprint, newFingerprint, snapshotJSON string
	oldContentHash, newContentHash               string
	retrievedAt, dataTimestamp, provenanceDigest string
	linked                                       bool
}

type clinicalTrialAuditV3RunMigration struct {
	runID                    string
	snapshots                []clinicalTrialAuditV3SnapshotMigration
	citationsJSON, auditJSON string
	hasResults               bool
}

func migrateClinicalTrialAuditV3Evidence(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `SELECT content_hash, evidence_json FROM clinical_trial_audit_evidence`)
	if err != nil {
		return err
	}
	evidenceByHash := make(map[string]clinicalTrialAuditV3EvidenceMigration)
	for rows.Next() {
		var contentHash, encoded string
		if err := rows.Scan(&contentHash, &encoded); err != nil {
			rows.Close()
			return err
		}
		var legacy struct {
			SchemaVersion string `json:"schema_version"`
			SourceType    string `json:"source_type"`
			ContentHash   string `json:"content_hash"`
			Provenance    struct {
				DataTimestamp string `json:"data_timestamp"`
			} `json:"provenance"`
			Data json.RawMessage `json:"data"`
		}
		item := clinicalTrialAuditV3EvidenceMigration{oldHash: contentHash}
		if json.Unmarshal([]byte(encoded), &legacy) == nil {
			timestamp, timestampErr := canonicalClinicalTrialTimestamp("data_timestamp", legacy.Provenance.DataTimestamp, true)
			translated, legacyHash, translatedHash, translateErr := migrateClinicalTrialsGovV1EvidenceData(legacy.Data)
			current := ClinicalTrialAuditEvidencePayload{SchemaVersion: legacy.SchemaVersion, SourceType: legacy.SourceType, ContentHash: translatedHash, Data: translated}
			finalized, finalizeErr := finalizeClinicalTrialAuditEvidencePayload(current)
			canonical, marshalErr := marshalBoundedClinicalTrialAuditJSON(finalized, clinicalTrialAuditJSONMaxBytes)
			if legacy.SourceType == ClinicalTrialsGovStudySourceType && timestampErr == nil && timestamp == legacy.Provenance.DataTimestamp && translateErr == nil &&
				legacy.ContentHash == contentHash && legacyHash == contentHash && finalizeErr == nil && marshalErr == nil && finalized.ContentHash == translatedHash {
				item.newHash, item.encoded, item.dataTimestamp, item.compatible = translatedHash, string(canonical), timestamp, true
			}
		}
		evidenceByHash[contentHash] = item
	}
	if err := rows.Close(); err != nil {
		return err
	}
	runRows, err := conn.QueryContext(ctx, `SELECT DISTINCT run_id FROM clinical_trial_audit_snapshots ORDER BY run_id`)
	if err != nil {
		return err
	}
	var runIDs []string
	for runRows.Next() {
		var runID string
		if err := runRows.Scan(&runID); err != nil {
			runRows.Close()
			return err
		}
		runIDs = append(runIDs, runID)
	}
	if err := runRows.Close(); err != nil {
		return err
	}
	var plans []clinicalTrialAuditV3RunMigration
	for _, runID := range runIDs {
		plan, err := planClinicalTrialAuditV3RunMigration(ctx, conn, runID, evidenceByHash)
		if err != nil {
			if _, markErr := conn.ExecContext(ctx, `INSERT OR REPLACE INTO clinical_trial_audit_incompatible_runs(run_id, incompatibility_code) VALUES (?, ?)`, runID, clinicalTrialAuditEvidenceV1Incompatible); markErr != nil {
				return markErr
			}
			continue
		}
		plans = append(plans, plan)
	}
	for _, plan := range plans {
		if err := applyClinicalTrialAuditV3RunMigration(ctx, conn, plan, evidenceByHash); err != nil {
			return err
		}
	}
	for _, item := range evidenceByHash {
		if !item.compatible {
			if _, err := conn.ExecContext(ctx, `UPDATE clinical_trial_audit_evidence SET compatible = 0, incompatibility_code = ? WHERE content_hash = ?`, clinicalTrialAuditEvidenceV1Incompatible, item.oldHash); err != nil {
				return err
			}
			continue
		}
		var remainingLinks int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM clinical_trial_audit_snapshot_evidence WHERE content_hash = ?`, item.oldHash).Scan(&remainingLinks); err != nil {
			return err
		}
		if remainingLinks == 0 && item.oldHash != item.newHash {
			if _, err := conn.ExecContext(ctx, `DELETE FROM clinical_trial_audit_evidence WHERE content_hash = ?`, item.oldHash); err != nil {
				return err
			}
		}
	}
	return nil
}

func planClinicalTrialAuditV3RunMigration(ctx context.Context, conn *sql.Conn, runID string, evidenceByHash map[string]clinicalTrialAuditV3EvidenceMigration) (clinicalTrialAuditV3RunMigration, error) {
	plan := clinicalTrialAuditV3RunMigration{runID: runID}
	rows, err := conn.QueryContext(ctx, `
		SELECT snapshot.fingerprint, snapshot.snapshot_json, link.content_hash
		FROM clinical_trial_audit_snapshots AS snapshot
		LEFT JOIN clinical_trial_audit_snapshot_evidence AS link ON link.run_id = snapshot.run_id AND link.fingerprint = snapshot.fingerprint
		WHERE snapshot.run_id = ? ORDER BY snapshot.fingerprint
	`, runID)
	if err != nil {
		return plan, err
	}
	fingerprintMap := make(map[string]string)
	snapshotMap := make(map[string]ClinicalTrialSourceSnapshot)
	legacySnapshotMap := make(map[string]ClinicalTrialSourceSnapshot)
	for rows.Next() {
		var oldFingerprint, snapshotJSON string
		var linkedHash sql.NullString
		if err := rows.Scan(&oldFingerprint, &snapshotJSON, &linkedHash); err != nil {
			rows.Close()
			return plan, err
		}
		var snapshot ClinicalTrialSourceSnapshot
		if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
			rows.Close()
			return plan, err
		}
		legacySnapshotMap[oldFingerprint] = snapshot
		migration := clinicalTrialAuditV3SnapshotMigration{oldFingerprint: oldFingerprint, oldContentHash: snapshot.ContentHash, linked: linkedHash.Valid}
		switch snapshot.SourceType {
		case ClinicalTrialsGovStudySourceType:
			if !linkedHash.Valid || linkedHash.String != snapshot.ContentHash {
				rows.Close()
				return plan, fmt.Errorf("ClinicalTrials.gov v3 snapshot has no matching evidence link")
			}
			evidence, exists := evidenceByHash[linkedHash.String]
			if !exists || !evidence.compatible {
				rows.Close()
				return plan, fmt.Errorf("ClinicalTrials.gov v3 evidence is incompatible")
			}
			snapshot.DataTimestamp = evidence.dataTimestamp
			legacyFinalized, err := FinalizeClinicalTrialSourceSnapshot(snapshot)
			if err != nil || legacyFinalized.Fingerprint != oldFingerprint {
				rows.Close()
				return plan, fmt.Errorf("ClinicalTrials.gov v3 snapshot identity mismatch")
			}
			snapshot.ContentHash = evidence.newHash
			migration.newContentHash = evidence.newHash
		case "pubmed":
			if linkedHash.Valid {
				rows.Close()
				return plan, fmt.Errorf("publication v3 snapshot unexpectedly has an evidence link")
			}
			legacyFinalized, err := FinalizeClinicalTrialSourceSnapshot(snapshot)
			if err != nil || legacyFinalized.Fingerprint != oldFingerprint {
				rows.Close()
				return plan, fmt.Errorf("publication v3 snapshot identity mismatch")
			}
			migration.newContentHash = snapshot.ContentHash
		default:
			rows.Close()
			return plan, fmt.Errorf("unsupported v3 source_type %q", snapshot.SourceType)
		}
		snapshot.Fingerprint = ""
		snapshot.ProvenanceDigest = ""
		finalized, err := FinalizeClinicalTrialSourceSnapshot(snapshot)
		if err != nil {
			rows.Close()
			return plan, err
		}
		canonical, err := marshalBoundedClinicalTrialAuditJSON(finalized, clinicalTrialAuditJSONMaxBytes)
		if err != nil {
			rows.Close()
			return plan, err
		}
		migration.newFingerprint, migration.snapshotJSON = finalized.Fingerprint, string(canonical)
		migration.retrievedAt, migration.dataTimestamp, migration.provenanceDigest = finalized.RetrievedAt, finalized.DataTimestamp, finalized.ProvenanceDigest
		plan.snapshots = append(plan.snapshots, migration)
		fingerprintMap[oldFingerprint] = finalized.Fingerprint
		snapshotMap[oldFingerprint] = finalized
	}
	if err := rows.Close(); err != nil {
		return plan, err
	}
	if len(plan.snapshots) == 0 {
		return plan, fmt.Errorf("v3 run has no source snapshots")
	}
	var citationsJSON, auditJSON string
	err = conn.QueryRowContext(ctx, `SELECT citations_json, audit_json FROM clinical_trial_audit_results WHERE run_id = ?`, runID).Scan(&citationsJSON, &auditJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return plan, nil
	}
	if err != nil {
		return plan, err
	}
	plan.hasResults = true
	var citations []ClinicalTrialAuditCitation
	if err := json.Unmarshal([]byte(citationsJSON), &citations); err != nil {
		return plan, err
	}
	for index := range citations {
		newFingerprint, exists := fingerprintMap[citations[index].SourceFingerprint]
		if !exists {
			return plan, fmt.Errorf("v3 citation references unknown source fingerprint")
		}
		citations[index].SourceFingerprint = newFingerprint
	}
	canonicalCitations, err := marshalBoundedClinicalTrialAuditJSON(citations, clinicalTrialAuditJSONMaxBytes)
	if err != nil {
		return plan, err
	}
	plan.citationsJSON, plan.auditJSON = string(canonicalCitations), auditJSON
	if auditJSON != "" {
		var audit ClinicalTrialAudit
		if err := json.Unmarshal([]byte(auditJSON), &audit); err != nil {
			return plan, err
		}
		matched := make(map[string]bool)
		for index := range audit.Sources {
			oldFingerprint := audit.Sources[index].Fingerprint
			storedLegacy, exists := legacySnapshotMap[oldFingerprint]
			if !exists {
				return plan, fmt.Errorf("terminal audit contains unknown v3 source snapshot")
			}
			storedLegacyJSON, _ := json.Marshal(storedLegacy)
			embeddedLegacyJSON, _ := json.Marshal(audit.Sources[index])
			if string(storedLegacyJSON) != string(embeddedLegacyJSON) {
				return plan, fmt.Errorf("terminal audit v3 source does not match persisted snapshot")
			}
			audit.Sources[index] = snapshotMap[oldFingerprint]
			matched[oldFingerprint] = true
		}
		if len(matched) != len(snapshotMap) {
			return plan, fmt.Errorf("terminal audit does not contain every persisted v3 source snapshot")
		}
		for index := range audit.Citations {
			newFingerprint, exists := fingerprintMap[audit.Citations[index].SourceFingerprint]
			if !exists {
				return plan, fmt.Errorf("terminal audit citation references unknown v3 source")
			}
			audit.Citations[index].SourceFingerprint = newFingerprint
		}
		completedAt, err := canonicalClinicalTrialTimestamp("completed_at", audit.CompletedAt, true)
		if err != nil {
			return plan, err
		}
		audit.CompletedAt = completedAt
		var runState string
		if err := conn.QueryRowContext(ctx, `SELECT state FROM clinical_trial_audit_runs WHERE run_id = ?`, runID).Scan(&runState); err != nil {
			return plan, err
		}
		switch runState {
		case ClinicalTrialAuditRunCompleted:
			err = validateClinicalTrialAudit(audit, true)
		case ClinicalTrialAuditRunAbstained:
			err = validateClinicalTrialAudit(audit, false)
		default:
			err = fmt.Errorf("persisted audit_json belongs to nonterminal run state %q", runState)
		}
		if err != nil {
			return plan, err
		}
		encoded, err := marshalBoundedClinicalTrialAuditJSON(audit, clinicalTrialAuditJSONMaxBytes)
		if err != nil {
			return plan, err
		}
		plan.auditJSON = string(encoded)
	}
	return plan, nil
}

func applyClinicalTrialAuditV3RunMigration(ctx context.Context, conn *sql.Conn, plan clinicalTrialAuditV3RunMigration, evidenceByHash map[string]clinicalTrialAuditV3EvidenceMigration) error {
	for _, snapshot := range plan.snapshots {
		if snapshot.linked {
			evidence := evidenceByHash[snapshot.oldContentHash]
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO clinical_trial_audit_evidence(content_hash, source_type, evidence_json, compatible, incompatibility_code)
				VALUES (?, ?, ?, 1, '') ON CONFLICT(content_hash) DO NOTHING
			`, evidence.newHash, ClinicalTrialsGovStudySourceType, evidence.encoded); err != nil {
				return err
			}
			var storedSourceType, storedJSON string
			if err := conn.QueryRowContext(ctx, `SELECT source_type, evidence_json FROM clinical_trial_audit_evidence WHERE content_hash = ?`, evidence.newHash).Scan(&storedSourceType, &storedJSON); err != nil {
				return err
			}
			if storedSourceType != ClinicalTrialsGovStudySourceType || storedJSON != evidence.encoded {
				return fmt.Errorf("migrated clinical trial evidence content hash collision")
			}
		}
	}
	for _, snapshot := range plan.snapshots {
		if _, err := conn.ExecContext(ctx, `UPDATE clinical_trial_audit_snapshots SET fingerprint = ?, snapshot_json = ? WHERE run_id = ? AND fingerprint = ?`, snapshot.newFingerprint, snapshot.snapshotJSON, plan.runID, snapshot.oldFingerprint); err != nil {
			return err
		}
		if snapshot.linked {
			if _, err := conn.ExecContext(ctx, `UPDATE clinical_trial_audit_snapshot_evidence SET fingerprint = ?, content_hash = ?, retrieved_at = ?, data_timestamp = ?, provenance_digest = ? WHERE run_id = ? AND fingerprint = ?`, snapshot.newFingerprint, snapshot.newContentHash, snapshot.retrievedAt, snapshot.dataTimestamp, snapshot.provenanceDigest, plan.runID, snapshot.oldFingerprint); err != nil {
				return err
			}
		}
	}
	if plan.hasResults {
		if _, err := conn.ExecContext(ctx, `UPDATE clinical_trial_audit_results SET citations_json = ?, audit_json = ? WHERE run_id = ?`, plan.citationsJSON, plan.auditJSON, plan.runID); err != nil {
			return err
		}
	}
	return nil
}

func ensureClinicalTrialAuditColumn(ctx context.Context, conn *sql.Conn, table, column, definition string) error {
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	_, err := conn.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition)
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
	operationNow := s.now().UTC()
	runID, err := newClinicalTrialAuditStoreID(s.random)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	defer tx.Rollback()

	now := operationNow.Format(time.RFC3339Nano)
	if _, err := tx.Exec(`
		INSERT INTO clinical_trial_audit_runs (
			run_id, package_id, package_version, idempotency_key, request_json,
			input_hash, state, attempt, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, runID, packageID, packageVersion, idempotencyKey, string(requestJSON), finalized.InputHash,
		ClinicalTrialAuditRunQueued, now, now); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	result, err := tx.Exec(`
		INSERT INTO clinical_trial_audit_idempotency_keys (
			idempotency_key, run_id, package_id, package_version, input_hash, created_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING
	`, idempotencyKey, runID, packageID, packageVersion, finalized.InputHash, now)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if inserted == 0 {
		existing, err := clinicalTrialAuditIdempotency(tx, idempotencyKey)
		if err != nil {
			return ClinicalTrialAuditStoredRun{}, err
		}
		if existing.packageID != packageID || existing.packageVersion != packageVersion || existing.inputHash != finalized.InputHash {
			return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditIdempotencyConflict
		}
		if err := tx.Rollback(); err != nil {
			return ClinicalTrialAuditStoredRun{}, err
		}
		return s.GetRun(existing.runID)
	}
	if err := tx.Commit(); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	return s.GetRun(runID)
}

func (s *ClinicalTrialAuditStore) GetRun(runID string) (ClinicalTrialAuditStoredRun, error) {
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	defer tx.Rollback()
	run, err := scanClinicalTrialAuditStoredRun(tx.QueryRow(clinicalTrialAuditRunSelect+` WHERE run_id = ?`, strings.TrimSpace(runID)))
	if errors.Is(err, sql.ErrNoRows) {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditRunNotFound
	}
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if s.afterRunRowRead != nil {
		s.afterRunRowRead()
	}
	if err := loadClinicalTrialAuditRunData(tx, &run); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if err := tx.Commit(); err != nil {
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
	operationNow := s.now().UTC()
	leaseToken, err := newClinicalTrialAuditLeaseToken(s.random)
	if err != nil {
		return nil, err
	}
	leaseTokenHash := hashClinicalTrialValue(leaseToken)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	operationTimestamp := operationNow.Format(time.RFC3339Nano)
	if _, err := recoverExpiredClinicalTrialAuditLeases(tx, operationTimestamp); err != nil {
		return nil, err
	}
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
	`, ClinicalTrialAuditRunCollecting, workerID, leaseTokenHash, operationNow.Add(leaseDuration).Format(time.RFC3339Nano),
		operationTimestamp, ClinicalTrialAuditRunQueued, ClinicalTrialAuditRunQueued).Scan(&runID)
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
		if err := validateClinicalTrialAuditFailure(checkpoint.ErrorCode, checkpoint.Retryable); err != nil {
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
	validationNow := s.now().UTC()
	validationTimestamp := validationNow.Format(time.RFC3339Nano)

	tx, err := s.db.Begin()
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE clinical_trial_audit_runs SET updated_at = updated_at WHERE run_id = ?`, runID); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
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
	if clinicalTrialAuditLeaseExpired(run.LeaseExpiresAt, validationNow) {
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
			CreatedAt: run.CreatedAt, UpdatedAt: validationTimestamp,
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
	if err := acquireClinicalTrialAuditLeaseWriteLock(tx, runID, run.State, workerID, leaseTokenHash); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	leaseOwner := run.LeaseOwner
	storedLeaseTokenHash := run.leaseTokenHash
	leaseExpiresAt := run.LeaseExpiresAt
	if isTerminalClinicalTrialAuditState(checkpoint.State) {
		leaseOwner = ""
		storedLeaseTokenHash = ""
		leaseExpiresAt = ""
	}
	if s.beforeLeaseFinalUpdate != nil {
		s.beforeLeaseFinalUpdate()
	}
	freshTimestamp := s.now().UTC().Format(time.RFC3339Nano)
	result, err := tx.Exec(`
		UPDATE clinical_trial_audit_runs SET state = ?, retryable = ?, error_code = ?,
			lease_owner = ?, lease_token_hash = ?, lease_expires_at = ?, updated_at = ?
		WHERE run_id = ? AND state = ? AND lease_owner = ? AND lease_token_hash = ?
			AND julianday(lease_expires_at) > julianday(?)
	`, checkpoint.State, boolToClinicalTrialAuditInt(checkpoint.Retryable), strings.TrimSpace(checkpoint.ErrorCode),
		leaseOwner, storedLeaseTokenHash, leaseExpiresAt, freshTimestamp, runID, run.State, workerID, leaseTokenHash, freshTimestamp)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditLeaseExpired
	}
	if err := tx.Commit(); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	return s.GetRun(runID)
}

func (s *ClinicalTrialAuditStore) RenewLease(runID, workerID, leaseToken string, leaseDuration time.Duration) (ClinicalTrialAuditStoredRun, error) {
	runID = strings.TrimSpace(runID)
	workerID = strings.TrimSpace(workerID)
	leaseToken = strings.TrimSpace(leaseToken)
	if runID == "" || workerID == "" || leaseToken == "" {
		return ClinicalTrialAuditStoredRun{}, fmt.Errorf("run_id, worker_id, and lease_token are required")
	}
	if leaseDuration <= 0 {
		leaseDuration = 2 * time.Minute
	}
	validationNow := s.now().UTC()
	leaseTokenHash := hashClinicalTrialValue(leaseToken)
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
	if run.leaseTokenHash == "" || run.leaseTokenHash != leaseTokenHash {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditLeaseFence
	}
	if clinicalTrialAuditLeaseExpired(run.LeaseExpiresAt, validationNow) {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditLeaseExpired
	}
	if !isActiveClinicalTrialAuditState(run.State) {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditInvalidTransition
	}
	existingExpiry, err := time.Parse(time.RFC3339Nano, run.LeaseExpiresAt)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditLeaseExpired
	}
	if err := acquireClinicalTrialAuditLeaseWriteLock(tx, runID, run.State, workerID, leaseTokenHash); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if s.beforeLeaseFinalUpdate != nil {
		s.beforeLeaseFinalUpdate()
	}
	freshNow := s.now().UTC()
	freshTimestamp := freshNow.Format(time.RFC3339Nano)
	newExpiry := freshNow.Add(leaseDuration)
	if existingExpiry.After(newExpiry) {
		newExpiry = existingExpiry
	}
	result, err := tx.Exec(`
		UPDATE clinical_trial_audit_runs SET lease_expires_at = ?, updated_at = ?
		WHERE run_id = ? AND state = ? AND lease_owner = ? AND lease_token_hash = ?
			AND julianday(lease_expires_at) > julianday(?)
	`, newExpiry.UTC().Format(time.RFC3339Nano), freshTimestamp, run.RunID, run.State, workerID, leaseTokenHash, freshTimestamp)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ClinicalTrialAuditStoredRun{}, ErrClinicalTrialAuditLeaseExpired
	}
	if err := tx.Commit(); err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	renewed, err := s.GetRun(run.RunID)
	if err != nil {
		return ClinicalTrialAuditStoredRun{}, err
	}
	renewed.LeaseToken = leaseToken
	return renewed, nil
}

func (s *ClinicalTrialAuditStore) RecoverExpiredLeases() (int64, error) {
	operationTimestamp := s.now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	count, err := recoverExpiredClinicalTrialAuditLeases(tx, operationTimestamp)
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
	run.SchemaVersion = ClinicalTrialAuditStoredRunSchemaVersion
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

type clinicalTrialAuditQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func loadClinicalTrialAuditRunData(queryer clinicalTrialAuditQueryer, run *ClinicalTrialAuditStoredRun) error {
	var incompatibilityCode string
	err := queryer.QueryRow(`SELECT incompatibility_code FROM clinical_trial_audit_incompatible_runs WHERE run_id = ?`, run.RunID).Scan(&incompatibilityCode)
	if err == nil {
		return fmt.Errorf("%w: %s", ErrClinicalTrialAuditEvidenceIncompatible, incompatibilityCode)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	rows, err := queryer.Query(`
		SELECT snapshot.fingerprint, snapshot.snapshot_json,
			link.content_hash, link.retrieved_at, link.data_timestamp, link.provenance_digest,
			evidence.content_hash, evidence.source_type, evidence.evidence_json,
			evidence.compatible, evidence.incompatibility_code
		FROM clinical_trial_audit_snapshots AS snapshot
		LEFT JOIN clinical_trial_audit_snapshot_evidence AS link
			ON link.run_id = snapshot.run_id AND link.fingerprint = snapshot.fingerprint
		LEFT JOIN clinical_trial_audit_evidence AS evidence
			ON evidence.content_hash = link.content_hash
		WHERE snapshot.run_id = ? ORDER BY snapshot.fingerprint
	`, run.RunID)
	if err != nil {
		return err
	}
	linkedRows := 0
	evidenceSeen := make(map[string]struct{})
	for rows.Next() {
		var storedFingerprint, snapshotJSON string
		var linkHash, linkRetrievedAt, linkDataTimestamp, linkProvenanceDigest sql.NullString
		var evidenceHash, evidenceSource, evidenceJSON, incompatibilityCode sql.NullString
		var compatible sql.NullInt64
		if err := rows.Scan(&storedFingerprint, &snapshotJSON, &linkHash, &linkRetrievedAt, &linkDataTimestamp, &linkProvenanceDigest,
			&evidenceHash, &evidenceSource, &evidenceJSON, &compatible, &incompatibilityCode); err != nil {
			rows.Close()
			return err
		}
		if len(snapshotJSON) > clinicalTrialAuditJSONMaxBytes {
			rows.Close()
			return fmt.Errorf("persisted clinical trial snapshot exceeds bounds")
		}
		var snapshot ClinicalTrialSourceSnapshot
		if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
			rows.Close()
			return err
		}
		linked := linkHash.Valid || linkRetrievedAt.Valid || linkDataTimestamp.Valid || linkProvenanceDigest.Valid ||
			evidenceHash.Valid || evidenceSource.Valid || evidenceJSON.Valid || compatible.Valid || incompatibilityCode.Valid
		if !linked {
			if snapshot.SourceType == ClinicalTrialsGovStudySourceType {
				rows.Close()
				return fmt.Errorf("ClinicalTrials.gov snapshot is missing normalized evidence")
			}
			finalizedSnapshot, err := FinalizeClinicalTrialSourceSnapshot(snapshot)
			if err != nil || finalizedSnapshot.Fingerprint != snapshot.Fingerprint || storedFingerprint != snapshot.Fingerprint {
				rows.Close()
				return fmt.Errorf("persisted clinical trial snapshot fingerprint mismatch")
			}
			run.Sources = append(run.Sources, snapshot)
			continue
		}
		if !linkHash.Valid || !linkRetrievedAt.Valid || !linkDataTimestamp.Valid || !linkProvenanceDigest.Valid ||
			!evidenceHash.Valid || !evidenceSource.Valid || !evidenceJSON.Valid || !compatible.Valid || !incompatibilityCode.Valid {
			rows.Close()
			return fmt.Errorf("persisted clinical trial evidence link is incomplete")
		}
		if compatible.Int64 != 1 {
			rows.Close()
			return fmt.Errorf("%w: %s", ErrClinicalTrialAuditEvidenceIncompatible, incompatibilityCode.String)
		}
		finalizedSnapshot, err := FinalizeClinicalTrialSourceSnapshot(snapshot)
		if err != nil || finalizedSnapshot.Fingerprint != snapshot.Fingerprint || storedFingerprint != snapshot.Fingerprint {
			rows.Close()
			return fmt.Errorf("persisted clinical trial snapshot fingerprint mismatch")
		}
		if linkRetrievedAt.String != snapshot.RetrievedAt || linkDataTimestamp.String != snapshot.DataTimestamp || linkProvenanceDigest.String != snapshot.ProvenanceDigest {
			rows.Close()
			return fmt.Errorf("persisted clinical trial snapshot provenance mismatch")
		}
		run.Sources = append(run.Sources, snapshot)
		linkedRows++
		if linkHash.String != evidenceHash.String || linkHash.String != snapshot.ContentHash || evidenceSource.String != snapshot.SourceType {
			rows.Close()
			return fmt.Errorf("persisted clinical trial snapshot and evidence identity mismatch")
		}
		if len(evidenceJSON.String) > clinicalTrialAuditJSONMaxBytes {
			rows.Close()
			return fmt.Errorf("persisted clinical trial evidence exceeds bounds")
		}
		var evidence ClinicalTrialAuditEvidencePayload
		if err := json.Unmarshal([]byte(evidenceJSON.String), &evidence); err != nil {
			rows.Close()
			return err
		}
		finalizedEvidence, err := finalizeClinicalTrialAuditEvidencePayload(evidence)
		if err != nil || finalizedEvidence.ContentHash != evidenceHash.String || finalizedEvidence.SourceType != evidenceSource.String {
			rows.Close()
			return fmt.Errorf("persisted clinical trial evidence failed validation")
		}
		canonicalEvidence, err := marshalBoundedClinicalTrialAuditJSON(finalizedEvidence, clinicalTrialAuditJSONMaxBytes)
		if err != nil || string(canonicalEvidence) != evidenceJSON.String {
			rows.Close()
			return fmt.Errorf("persisted clinical trial evidence is not canonical")
		}
		if _, exists := evidenceSeen[evidenceHash.String]; !exists {
			run.Evidence = append(run.Evidence, finalizedEvidence)
			evidenceSeen[evidenceHash.String] = struct{}{}
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var persistedLinks int
	if err := queryer.QueryRow(`SELECT COUNT(*) FROM clinical_trial_audit_snapshot_evidence WHERE run_id = ?`, run.RunID).Scan(&persistedLinks); err != nil {
		return err
	}
	if persistedLinks != linkedRows {
		return fmt.Errorf("persisted clinical trial evidence contains orphaned links")
	}
	rows, err = queryer.Query(`SELECT finding_json FROM clinical_trial_audit_findings WHERE run_id = ? ORDER BY finding_id`, run.RunID)
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
	err = queryer.QueryRow(`SELECT citations_json, audit_json FROM clinical_trial_audit_results WHERE run_id = ?`, run.RunID).Scan(&citationsJSON, &auditJSON)
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
		availableEvidence := make(map[string]ClinicalTrialAuditEvidencePayload)
		rows, err := tx.Query(`
			SELECT evidence.evidence_json
			FROM clinical_trial_audit_snapshot_evidence AS link
			JOIN clinical_trial_audit_evidence AS evidence ON evidence.content_hash = link.content_hash
			WHERE link.run_id = ?
		`, runID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var encoded string
			if err := rows.Scan(&encoded); err != nil {
				rows.Close()
				return err
			}
			var evidence ClinicalTrialAuditEvidencePayload
			if err := json.Unmarshal([]byte(encoded), &evidence); err != nil {
				rows.Close()
				return err
			}
			finalized, err := finalizeClinicalTrialAuditEvidencePayload(evidence)
			if err != nil {
				rows.Close()
				return err
			}
			canonical, err := marshalBoundedClinicalTrialAuditJSON(finalized, clinicalTrialAuditJSONMaxBytes)
			if err != nil || string(canonical) != encoded {
				rows.Close()
				return fmt.Errorf("persisted clinical trial evidence is not canonical")
			}
			availableEvidence[finalized.SourceType+"\x00"+finalized.ContentHash] = finalized
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, evidence := range checkpoint.Evidence {
			finalized, err := finalizeClinicalTrialAuditEvidencePayload(evidence)
			if err != nil {
				return err
			}
			evidence = finalized
			availableEvidence[evidence.SourceType+"\x00"+evidence.ContentHash] = evidence
			encoded, err := marshalBoundedClinicalTrialAuditJSON(evidence, clinicalTrialAuditJSONMaxBytes)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`
				INSERT INTO clinical_trial_audit_evidence (content_hash, source_type, evidence_json, compatible, incompatibility_code)
				VALUES (?, ?, ?, 1, '') ON CONFLICT(content_hash) DO NOTHING
			`, evidence.ContentHash, evidence.SourceType, string(encoded)); err != nil {
				return err
			}
			var storedSourceType, storedJSON string
			if err := tx.QueryRow(`SELECT source_type, evidence_json FROM clinical_trial_audit_evidence WHERE content_hash = ?`, evidence.ContentHash).Scan(&storedSourceType, &storedJSON); err != nil {
				return err
			}
			if storedSourceType != evidence.SourceType || storedJSON != string(encoded) {
				return fmt.Errorf("clinical trial evidence content hash collision or payload mismatch")
			}
		}
		for _, snapshot := range checkpoint.Sources {
			if snapshot.SourceType == ClinicalTrialsGovStudySourceType {
				if _, exists := availableEvidence[snapshot.SourceType+"\x00"+snapshot.ContentHash]; !exists {
					return fmt.Errorf("ClinicalTrials.gov snapshot requires matching normalized evidence")
				}
			}
		}
		if _, err := tx.Exec(`DELETE FROM clinical_trial_audit_snapshot_evidence WHERE run_id = ?`, runID); err != nil {
			return err
		}
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
			if evidence, exists := availableEvidence[snapshot.SourceType+"\x00"+snapshot.ContentHash]; exists {
				if _, err := tx.Exec(`
					INSERT INTO clinical_trial_audit_snapshot_evidence
						(run_id, fingerprint, content_hash, retrieved_at, data_timestamp, provenance_digest)
					VALUES (?, ?, ?, ?, ?, ?)
				`, runID, finalized.Fingerprint, evidence.ContentHash, finalized.RetrievedAt, finalized.DataTimestamp, finalized.ProvenanceDigest); err != nil {
					return err
				}
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
	for _, value := range []any{checkpoint.Sources, checkpoint.Evidence, checkpoint.Findings, checkpoint.Citations, checkpoint.Audit} {
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
	evidenceByIdentity := make(map[string]struct{}, len(checkpoint.Evidence))
	for _, evidence := range checkpoint.Evidence {
		if err := validateClinicalTrialAuditEvidencePayload(evidence); err != nil {
			return err
		}
		identity := evidence.SourceType + "\x00" + evidence.ContentHash
		if _, exists := evidenceByIdentity[identity]; exists {
			return fmt.Errorf("duplicate clinical trial evidence payload")
		}
		evidenceByIdentity[identity] = struct{}{}
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

func validateClinicalTrialAuditEvidencePayload(evidence ClinicalTrialAuditEvidencePayload) error {
	_, err := finalizeClinicalTrialAuditEvidencePayload(evidence)
	return err
}

func finalizeClinicalTrialAuditEvidencePayload(evidence ClinicalTrialAuditEvidencePayload) (ClinicalTrialAuditEvidencePayload, error) {
	if evidence.SchemaVersion != ClinicalTrialAuditEvidenceSchemaVersion {
		return ClinicalTrialAuditEvidencePayload{}, fmt.Errorf("evidence schema_version must be %q", ClinicalTrialAuditEvidenceSchemaVersion)
	}
	switch evidence.SourceType {
	case ClinicalTrialsGovStudySourceType:
		return finalizeClinicalTrialsGovEvidencePayload(evidence)
	default:
		return ClinicalTrialAuditEvidencePayload{}, fmt.Errorf("unsupported clinical trial evidence source_type %q", evidence.SourceType)
	}
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

func acquireClinicalTrialAuditLeaseWriteLock(tx *sql.Tx, runID, state, workerID, leaseTokenHash string) error {
	result, err := tx.Exec(`
		UPDATE clinical_trial_audit_runs SET lease_generation = lease_generation
		WHERE run_id = ? AND state = ? AND lease_owner = ? AND lease_token_hash = ?
	`, runID, state, workerID, leaseTokenHash)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrClinicalTrialAuditLeaseFence
	}
	return nil
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
		ClinicalTrialAuditErrorSourceCanceled,
		ClinicalTrialAuditErrorSourceResponseTooLarge,
		ClinicalTrialAuditErrorSourceMalformedJSON,
		ClinicalTrialAuditErrorSourceIdentifierMismatch,
		ClinicalTrialAuditErrorSourceSchemaInvalid,
		ClinicalTrialAuditErrorSourceUpstreamPermanent,
		ClinicalTrialAuditErrorSourceUpstreamTransient,
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

func validateClinicalTrialAuditFailure(code string, retryable bool) error {
	if err := validateClinicalTrialAuditErrorCode(code); err != nil {
		return err
	}
	policy := map[string]bool{
		ClinicalTrialAuditErrorIdentifierInvalid:        false,
		ClinicalTrialAuditErrorSourceNotFound:           false,
		ClinicalTrialAuditErrorSourceRateLimited:        true,
		ClinicalTrialAuditErrorSourceTimeout:            true,
		ClinicalTrialAuditErrorSourceCanceled:           false,
		ClinicalTrialAuditErrorSourceResponseTooLarge:   false,
		ClinicalTrialAuditErrorSourceMalformedJSON:      false,
		ClinicalTrialAuditErrorSourceIdentifierMismatch: false,
		ClinicalTrialAuditErrorSourceSchemaInvalid:      false,
		ClinicalTrialAuditErrorSourceUpstreamPermanent:  false,
		ClinicalTrialAuditErrorSourceUpstreamTransient:  true,
		ClinicalTrialAuditErrorModelTimeout:             true,
		ClinicalTrialAuditErrorModelInvalidOutput:       false,
		ClinicalTrialAuditErrorEvidenceInvalid:          false,
		ClinicalTrialAuditErrorRetryExhausted:           false,
		ClinicalTrialAuditErrorInternal:                 false,
	}
	if policy[code] != retryable {
		return fmt.Errorf("clinical trial audit error code %q requires retryable=%v", code, policy[code])
	}
	return nil
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
	value = strings.TrimSpace(value)
	if len(value) > clinicalTrialAuditCursorEncodedMaxBytes {
		return clinicalTrialAuditCursor{}, fmt.Errorf("invalid clinical trial audit cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return clinicalTrialAuditCursor{}, fmt.Errorf("invalid clinical trial audit cursor")
	}
	if len(decoded) > clinicalTrialAuditCursorDecodedMaxBytes {
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

func newClinicalTrialAuditStoreID(random io.Reader) (string, error) {
	var randomBytes [8]byte
	if _, err := io.ReadFull(random, randomBytes[:]); err != nil {
		return "", fmt.Errorf("%w: generate run ID", errClinicalTrialAuditRandomUnavailable)
	}
	return "clinical-audit-" + hex.EncodeToString(randomBytes[:]), nil
}

func newClinicalTrialAuditLeaseToken(random io.Reader) (string, error) {
	var randomBytes [24]byte
	if _, err := io.ReadFull(random, randomBytes[:]); err != nil {
		return "", fmt.Errorf("%w: generate lease token", errClinicalTrialAuditRandomUnavailable)
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes[:]), nil
}

func boolToClinicalTrialAuditInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
