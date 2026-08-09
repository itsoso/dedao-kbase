package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/yann0917/dedao-gui/backend/services"
)

const (
	bookKnowledgeJobsFileName          = "jobs.json"
	bookKnowledgeJobsDBFileName        = "book_jobs.sqlite3"
	bookKnowledgeLegacyJobsImportedV1  = "legacy_jobs_imported_v1"
	bookKnowledgeJobsSQLiteBusyTimeout = 5000
	defaultDedaoDownloadDir            = "downloads"
)

type BookKnowledgeJobStatus string

const (
	BookKnowledgeJobStatusQueued      BookKnowledgeJobStatus = "queued"
	BookKnowledgeJobStatusRunning     BookKnowledgeJobStatus = "running"
	BookKnowledgeJobStatusSucceeded   BookKnowledgeJobStatus = "succeeded"
	BookKnowledgeJobStatusFailed      BookKnowledgeJobStatus = "failed"
	BookKnowledgeJobStatusInterrupted BookKnowledgeJobStatus = "interrupted"
)

const BookKnowledgeJobFailureWorkerInterrupted = "worker_interrupted"

const bookKnowledgeJobInterruptedMessage = "job execution interrupted"

const (
	BookKnowledgeJobTypeDedaoEbookDownload  = "dedao_ebook_download"
	BookKnowledgeJobTypeDedaoEbookSyncKBase = "dedao_ebook_sync_kbase"
)

type BookKnowledgeJob struct {
	ID             string                 `json:"id"`
	Type           string                 `json:"type"`
	Status         BookKnowledgeJobStatus `json:"status"`
	EbookID        int                    `json:"ebook_id"`
	EbookEnID      string                 `json:"ebook_enid"`
	DownloadType   int                    `json:"download_type"`
	Result         map[string]any         `json:"result,omitempty"`
	Error          string                 `json:"error,omitempty"`
	Logs           []string               `json:"logs,omitempty"`
	RetryOf        string                 `json:"retry_of,omitempty"`
	Stage          string                 `json:"stage,omitempty"`
	FailureCode    string                 `json:"failure_code,omitempty"`
	LeaseOwner     string                 `json:"lease_owner,omitempty"`
	LeaseExpiresAt string                 `json:"lease_expires_at,omitempty"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
	StartedAt      string                 `json:"started_at,omitempty"`
	FinishedAt     string                 `json:"finished_at,omitempty"`
}

type BookKnowledgeJobRequest struct {
	Type         string `json:"type"`
	EbookID      int    `json:"ebook_id"`
	EbookEnID    string `json:"ebook_enid"`
	DownloadType int    `json:"download_type,omitempty"`
}

type bookKnowledgeJobsFile struct {
	Jobs []BookKnowledgeJob `json:"jobs"`
}

var bookKnowledgeJobsMu sync.Mutex

var (
	runDedaoEbookDownloadJob  = executeDedaoEbookDownloadJob
	runDedaoEbookSyncKBaseJob = executeDedaoEbookSyncKBaseJob
)

type dedaoServiceContextKey struct{}

func contextWithDedaoService(ctx context.Context, service *services.Service) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if service == nil {
		return ctx
	}
	return context.WithValue(ctx, dedaoServiceContextKey{}, service)
}

func dedaoServiceFromContext(ctx context.Context) *services.Service {
	if ctx != nil {
		if service, ok := ctx.Value(dedaoServiceContextKey{}).(*services.Service); ok && service != nil {
			return service
		}
	}
	return getService()
}

func (s *BookKnowledgeStore) JobsPath() string {
	return s.LegacyJobsPath()
}

func (s *BookKnowledgeStore) BookJobsDBPath() string {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	return filepath.Join(s.root, bookKnowledgeJobsDBFileName)
}

func (s *BookKnowledgeStore) LegacyJobsPath() string {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	return filepath.Join(s.root, bookKnowledgeJobsFileName)
}

func (s *BookKnowledgeStore) CreateBookKnowledgeJob(request BookKnowledgeJobRequest) (BookKnowledgeJob, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	normalized, err := normalizeBookKnowledgeJobRequest(request)
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	job := BookKnowledgeJob{
		ID: newBookKnowledgeJobID(), Type: normalized.Type, Status: BookKnowledgeJobStatusQueued,
		EbookID: normalized.EbookID, EbookEnID: normalized.EbookEnID, DownloadType: normalized.DownloadType,
		Logs: []string{"queued"}, Stage: "queued", CreatedAt: now, UpdatedAt: now,
	}

	bookKnowledgeJobsMu.Lock()
	defer bookKnowledgeJobsMu.Unlock()
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	if _, err := insertBookKnowledgeJob(tx, job, false); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if err := appendBookKnowledgeJobEvent(tx, job); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return BookKnowledgeJob{}, err
	}
	return job, nil
}

func (s *BookKnowledgeStore) ListBookKnowledgeJobs(limit int) ([]BookKnowledgeJob, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	bookKnowledgeJobsMu.Lock()
	defer bookKnowledgeJobsMu.Unlock()
	db, err := s.openBookJobsDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(bookKnowledgeJobSelect+` ORDER BY created_at DESC, job_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]BookKnowledgeJob, 0)
	for rows.Next() {
		job, err := scanBookKnowledgeJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *BookKnowledgeStore) LoadBookKnowledgeJob(jobID string) (BookKnowledgeJob, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return BookKnowledgeJob{}, fmt.Errorf("job_id is required")
	}
	bookKnowledgeJobsMu.Lock()
	defer bookKnowledgeJobsMu.Unlock()
	db, err := s.openBookJobsDB()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	defer db.Close()
	job, err := scanBookKnowledgeJob(db.QueryRow(bookKnowledgeJobSelect+` WHERE job_id = ?`, jobID))
	if err == sql.ErrNoRows {
		return BookKnowledgeJob{}, fmt.Errorf("job not found")
	}
	return job, err
}

func (s *BookKnowledgeStore) FailInterruptedBookKnowledgeJobs(reason string) (int, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "interrupted"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	bookKnowledgeJobsMu.Lock()
	defer bookKnowledgeJobsMu.Unlock()
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(bookKnowledgeJobSelect+` WHERE status IN (?, ?)`, BookKnowledgeJobStatusQueued, BookKnowledgeJobStatusRunning)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	jobs := make([]BookKnowledgeJob, 0)
	for rows.Next() {
		job, scanErr := scanBookKnowledgeJob(rows)
		if scanErr != nil {
			rows.Close()
			tx.Rollback()
			return 0, scanErr
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		tx.Rollback()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		tx.Rollback()
		return 0, err
	}
	count := 0
	for _, job := range jobs {
		job.Status = BookKnowledgeJobStatusFailed
		job.Stage = "failed"
		job.Error = sanitizeBookKnowledgeJobError(reason)
		job.UpdatedAt, job.FinishedAt = now, now
		job.Logs = append(job.Logs, "failed: interrupted")
		if err := updateBookKnowledgeJobRow(tx, job); err != nil {
			tx.Rollback()
			return 0, err
		}
		if err := appendBookKnowledgeJobEvent(tx, job); err != nil {
			tx.Rollback()
			return 0, err
		}
		count++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *BookKnowledgeStore) RunBookKnowledgeJob(jobID string) error {
	return s.RunBookKnowledgeJobWithService(jobID, getService())
}

func (s *BookKnowledgeStore) RunBookKnowledgeJobWithService(jobID string, service *services.Service) error {
	job, err := s.startBookKnowledgeJob(jobID)
	if err != nil {
		return err
	}
	ctx := contextWithDedaoService(context.Background(), service)
	result, runErr := s.executeBookKnowledgeJobSafely(ctx, job)
	_, err = s.updateBookKnowledgeJob(job.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		job.UpdatedAt, job.FinishedAt = now, now
		if runErr != nil {
			job.Status = BookKnowledgeJobStatusFailed
			job.Error = sanitizeBookKnowledgeJobError(runErr.Error())
			job.Logs = append(job.Logs, "failed")
			return job
		}
		job.Status = BookKnowledgeJobStatusSucceeded
		job.Result = safeBookKnowledgeJobResult(result)
		job.Logs = append(job.Logs, "succeeded")
		return job
	})
	return err
}

func (s *BookKnowledgeStore) startBookKnowledgeJob(jobID string) (BookKnowledgeJob, error) {
	bookKnowledgeJobsMu.Lock()
	defer bookKnowledgeJobsMu.Unlock()
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	job, err := startBookKnowledgeJobInTx(tx, jobID)
	if err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return BookKnowledgeJob{}, err
	}
	return job, nil
}

func startBookKnowledgeJobInTx(tx *sql.Tx, jobID string) (BookKnowledgeJob, error) {
	job, err := scanBookKnowledgeJob(tx.QueryRow(bookKnowledgeJobSelect+` WHERE job_id = ?`, strings.TrimSpace(jobID)))
	if err == sql.ErrNoRows {
		return BookKnowledgeJob{}, fmt.Errorf("job not found")
	}
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	if job.Status != BookKnowledgeJobStatusQueued {
		return BookKnowledgeJob{}, fmt.Errorf("job %q cannot run from status %s", job.ID, job.Status)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	job.Status = BookKnowledgeJobStatusRunning
	job.Stage = defaultBookKnowledgeJobStage(job.Status)
	job.Error = ""
	job.FailureCode = ""
	job.Result = nil
	job.LeaseOwner = ""
	job.LeaseExpiresAt = ""
	job.StartedAt = now
	job.FinishedAt = ""
	job.UpdatedAt = now
	job.Logs = append(job.Logs, "running")
	if err := updateBookKnowledgeJobRowFromStatus(tx, job, BookKnowledgeJobStatusQueued); err != nil {
		return BookKnowledgeJob{}, err
	}
	if err := appendBookKnowledgeJobEvent(tx, job); err != nil {
		return BookKnowledgeJob{}, err
	}
	return job, nil
}

func (s *BookKnowledgeStore) executeBookKnowledgeJobSafely(ctx context.Context, job BookKnowledgeJob) (result map[string]any, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = fmt.Errorf("job executor panic")
		}
	}()
	return s.executeBookKnowledgeJob(ctx, job)
}

func (s *BookKnowledgeStore) executeBookKnowledgeJob(ctx context.Context, job BookKnowledgeJob) (map[string]any, error) {
	switch job.Type {
	case BookKnowledgeJobTypeDedaoEbookDownload:
		return runDedaoEbookDownloadJob(ctx, job)
	case BookKnowledgeJobTypeDedaoEbookSyncKBase:
		return runDedaoEbookSyncKBaseJob(ctx, s, job)
	default:
		return nil, fmt.Errorf("unsupported job type: %s", job.Type)
	}
}

func (s *BookKnowledgeStore) updateBookKnowledgeJob(jobID string, mutate func(BookKnowledgeJob) BookKnowledgeJob) (BookKnowledgeJob, error) {
	bookKnowledgeJobsMu.Lock()
	defer bookKnowledgeJobsMu.Unlock()
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	job, err := scanBookKnowledgeJob(tx.QueryRow(bookKnowledgeJobSelect+` WHERE job_id = ?`, strings.TrimSpace(jobID)))
	if err == sql.ErrNoRows {
		tx.Rollback()
		return BookKnowledgeJob{}, fmt.Errorf("job not found")
	}
	if err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	updated := mutate(job)
	updated.ID = job.ID
	if updated.Status != job.Status && updated.Stage == job.Stage {
		updated.Stage = defaultBookKnowledgeJobStage(updated.Status)
	}
	if err := updateBookKnowledgeJobRow(tx, updated); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if err := appendBookKnowledgeJobEvent(tx, updated); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return BookKnowledgeJob{}, err
	}
	return updated, nil
}

func (s *BookKnowledgeStore) openBookJobsDB() (*sql.DB, error) {
	return s.openBookJobsDBWithTxLock(false)
}

func (s *BookKnowledgeStore) openBookJobsWriteDB() (*sql.DB, error) {
	return s.openBookJobsDBWithTxLock(true)
}

func (s *BookKnowledgeStore) openBookJobsDBWithTxLock(immediate bool) (*sql.DB, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	if err := os.MkdirAll(s.root, 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", bookKnowledgeJobsSQLiteDSN(s.BookJobsDBPath(), immediate))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA busy_timeout = %d`, bookKnowledgeJobsSQLiteBusyTimeout)); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil {
		db.Close()
		return nil, err
	}
	if !strings.EqualFold(journalMode, "wal") {
		db.Close()
		return nil, fmt.Errorf("enable book jobs WAL: journal_mode=%s", journalMode)
	}
	if err := s.migrateBookJobsDB(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(s.BookJobsDBPath(), 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

const bookKnowledgeJobSelect = `
	SELECT job_id, job_type, status, ebook_id, ebook_enid, download_type,
		result_json, logs_json, COALESCE(retry_of, ''), stage, failure_code,
		lease_owner, lease_expires_at, failure_message, created_at, updated_at,
		started_at, finished_at
	FROM book_jobs`

const bookKnowledgeJobsSchema = `
	CREATE TABLE IF NOT EXISTS book_jobs (
		job_id TEXT PRIMARY KEY,
		job_type TEXT NOT NULL,
		status TEXT NOT NULL,
		ebook_id INTEGER NOT NULL,
		ebook_enid TEXT NOT NULL,
		download_type INTEGER NOT NULL,
		result_json TEXT NOT NULL DEFAULT '{}',
		logs_json TEXT NOT NULL DEFAULT '[]',
		retry_of TEXT DEFAULT NULL,
		stage TEXT NOT NULL DEFAULT 'queued',
		failure_code TEXT NOT NULL DEFAULT '',
		lease_owner TEXT NOT NULL DEFAULT '',
		lease_expires_at TEXT NOT NULL DEFAULT '',
		failure_message TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		started_at TEXT NOT NULL DEFAULT '',
		finished_at TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(retry_of) REFERENCES book_jobs(job_id) DEFERRABLE INITIALLY DEFERRED
	);
	CREATE INDEX IF NOT EXISTS idx_book_jobs_created
		ON book_jobs(created_at DESC, job_id DESC);
	CREATE TABLE IF NOT EXISTS book_job_events (
		event_id INTEGER PRIMARY KEY AUTOINCREMENT,
		job_id TEXT NOT NULL,
		status TEXT NOT NULL,
		stage TEXT NOT NULL,
		code TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		FOREIGN KEY(job_id) REFERENCES book_jobs(job_id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_book_job_events_job
		ON book_job_events(job_id, event_id);
	CREATE TABLE IF NOT EXISTS book_job_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`

func (s *BookKnowledgeStore) migrateBookJobsDB(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(bookKnowledgeJobsSchema); err != nil {
		tx.Rollback()
		return err
	}
	var marker string
	err = tx.QueryRow(`SELECT value FROM book_job_meta WHERE key = ?`, bookKnowledgeLegacyJobsImportedV1).Scan(&marker)
	if err == nil {
		if marker != "1" {
			tx.Rollback()
			return fmt.Errorf("invalid book jobs migration marker %q", marker)
		}
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		tx.Rollback()
		return err
	}

	legacy, err := readLegacyBookKnowledgeJobs(s.LegacyJobsPath())
	if err != nil && !os.IsNotExist(err) {
		tx.Rollback()
		return fmt.Errorf("import legacy book jobs: %w", err)
	}
	jobs := make([]BookKnowledgeJob, len(legacy.Jobs))
	for index, job := range legacy.Jobs {
		normalized, err := validateAndNormalizeLegacyBookKnowledgeJob(job)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("validate legacy book job %d: %w", index, err)
		}
		jobs[index] = normalized
	}
	for _, job := range jobs {
		inserted, err := insertBookKnowledgeJob(tx, job, true)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("import legacy book job %q: %w", job.ID, err)
		}
		if inserted {
			if err := appendBookKnowledgeJobEvent(tx, job); err != nil {
				tx.Rollback()
				return fmt.Errorf("import legacy book job event %q: %w", job.ID, err)
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO book_job_meta(key, value) VALUES (?, '1')`, bookKnowledgeLegacyJobsImportedV1); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func insertBookKnowledgeJob(tx *sql.Tx, job BookKnowledgeJob, ignoreConflict bool) (bool, error) {
	resultJSON, logsJSON, err := marshalBookKnowledgeJobJSON(job)
	if err != nil {
		return false, err
	}
	query := `
		INSERT INTO book_jobs (
			job_id, job_type, status, ebook_id, ebook_enid, download_type,
			result_json, logs_json, retry_of, stage, failure_code, lease_owner,
			lease_expires_at, failure_message, created_at, updated_at, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if ignoreConflict {
		query += ` ON CONFLICT(job_id) DO NOTHING`
	}
	result, err := tx.Exec(query,
		job.ID, job.Type, job.Status, job.EbookID, job.EbookEnID, job.DownloadType,
		resultJSON, logsJSON, job.RetryOf, bookKnowledgeJobStage(job), job.FailureCode,
		job.LeaseOwner, job.LeaseExpiresAt, job.Error, job.CreatedAt, job.UpdatedAt,
		job.StartedAt, job.FinishedAt,
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func updateBookKnowledgeJobRow(tx *sql.Tx, job BookKnowledgeJob) error {
	return updateBookKnowledgeJobRowFromStatus(tx, job, "")
}

func updateBookKnowledgeJobRowFromStatus(tx *sql.Tx, job BookKnowledgeJob, expectedStatus BookKnowledgeJobStatus) error {
	resultJSON, logsJSON, err := marshalBookKnowledgeJobJSON(job)
	if err != nil {
		return err
	}
	query := `
		UPDATE book_jobs SET
			job_type = ?, status = ?, ebook_id = ?, ebook_enid = ?, download_type = ?,
			result_json = ?, logs_json = ?, retry_of = NULLIF(?, ''), stage = ?, failure_code = ?,
			lease_owner = ?, lease_expires_at = ?, failure_message = ?, created_at = ?,
			updated_at = ?, started_at = ?, finished_at = ?
		WHERE job_id = ?`
	args := []any{
		job.Type, job.Status, job.EbookID, job.EbookEnID, job.DownloadType,
		resultJSON, logsJSON, job.RetryOf, bookKnowledgeJobStage(job), job.FailureCode,
		job.LeaseOwner, job.LeaseExpiresAt, job.Error, job.CreatedAt, job.UpdatedAt,
		job.StartedAt, job.FinishedAt, job.ID,
	}
	if expectedStatus != "" {
		query += ` AND status = ?`
		args = append(args, expectedStatus)
	}
	result, err := tx.Exec(query, args...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		if expectedStatus != "" {
			return fmt.Errorf("job %q cannot run from status other than %s", job.ID, expectedStatus)
		}
		return fmt.Errorf("job not found")
	}
	return nil
}

func appendBookKnowledgeJobEvent(tx *sql.Tx, job BookKnowledgeJob) error {
	_, err := tx.Exec(`
		INSERT INTO book_job_events(job_id, status, stage, code, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		job.ID, job.Status, bookKnowledgeJobStage(job), job.FailureCode,
		bookKnowledgeJobEventMessage(job), bookKnowledgeJobEventTime(job),
	)
	return err
}

type bookKnowledgeJobScanner interface {
	Scan(dest ...any) error
}

func scanBookKnowledgeJob(scanner bookKnowledgeJobScanner) (BookKnowledgeJob, error) {
	var job BookKnowledgeJob
	var resultJSON, logsJSON string
	if err := scanner.Scan(
		&job.ID, &job.Type, &job.Status, &job.EbookID, &job.EbookEnID, &job.DownloadType,
		&resultJSON, &logsJSON, &job.RetryOf, &job.Stage, &job.FailureCode,
		&job.LeaseOwner, &job.LeaseExpiresAt, &job.Error, &job.CreatedAt, &job.UpdatedAt,
		&job.StartedAt, &job.FinishedAt,
	); err != nil {
		return job, err
	}
	if err := json.Unmarshal([]byte(resultJSON), &job.Result); err != nil {
		return job, fmt.Errorf("decode book job result: %w", err)
	}
	if err := json.Unmarshal([]byte(logsJSON), &job.Logs); err != nil {
		return job, fmt.Errorf("decode book job logs: %w", err)
	}
	return job, nil
}

func marshalBookKnowledgeJobJSON(job BookKnowledgeJob) (string, string, error) {
	resultJSON, err := json.Marshal(job.Result)
	if err != nil {
		return "", "", fmt.Errorf("encode book job result: %w", err)
	}
	logsJSON, err := json.Marshal(job.Logs)
	if err != nil {
		return "", "", fmt.Errorf("encode book job logs: %w", err)
	}
	return string(resultJSON), string(logsJSON), nil
}

func readLegacyBookKnowledgeJobs(path string) (bookKnowledgeJobsFile, error) {
	file := bookKnowledgeJobsFile{Jobs: []BookKnowledgeJob{}}
	reader, err := os.Open(path)
	if err != nil {
		return file, err
	}
	defer reader.Close()
	document := struct {
		Jobs *[]BookKnowledgeJob `json:"jobs"`
	}{}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return file, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return file, fmt.Errorf("legacy jobs contains trailing JSON")
		}
		return file, fmt.Errorf("legacy jobs contains trailing data: %w", err)
	}
	if document.Jobs == nil {
		return file, fmt.Errorf("legacy jobs array is required")
	}
	file.Jobs = *document.Jobs
	return file, nil
}

func validateAndNormalizeLegacyBookKnowledgeJob(job BookKnowledgeJob) (BookKnowledgeJob, error) {
	if strings.TrimSpace(job.ID) == "" {
		return job, fmt.Errorf("id is required")
	}
	switch job.Type {
	case BookKnowledgeJobTypeDedaoEbookDownload, BookKnowledgeJobTypeDedaoEbookSyncKBase:
	default:
		return job, fmt.Errorf("unsupported job type: %s", job.Type)
	}
	switch job.Status {
	case BookKnowledgeJobStatusQueued, BookKnowledgeJobStatusRunning, BookKnowledgeJobStatusSucceeded,
		BookKnowledgeJobStatusFailed, BookKnowledgeJobStatusInterrupted:
	default:
		return job, fmt.Errorf("unsupported job status: %s", job.Status)
	}
	if job.EbookID <= 0 {
		return job, fmt.Errorf("ebook_id is required")
	}
	if strings.TrimSpace(job.EbookEnID) == "" {
		return job, fmt.Errorf("ebook_enid is required")
	}
	if job.Type == BookKnowledgeJobTypeDedaoEbookSyncKBase {
		job.DownloadType = 1
	} else if job.DownloadType < 1 || job.DownloadType > 3 {
		return job, fmt.Errorf("download_type must be 1, 2, or 3")
	}
	for name, value := range map[string]string{"created_at": job.CreatedAt, "updated_at": job.UpdatedAt} {
		if strings.TrimSpace(value) == "" {
			return job, fmt.Errorf("%s is required", name)
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return job, fmt.Errorf("%s must be RFC3339: %w", name, err)
		}
	}
	for name, value := range map[string]string{"started_at": job.StartedAt, "finished_at": job.FinishedAt} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return job, fmt.Errorf("%s must be RFC3339: %w", name, err)
		}
	}
	return normalizeLegacyBookKnowledgeJob(job), nil
}

func normalizeLegacyBookKnowledgeJob(job BookKnowledgeJob) BookKnowledgeJob {
	interrupted := job.Status == BookKnowledgeJobStatusInterrupted ||
		(job.Status == BookKnowledgeJobStatusFailed && legacyBookKnowledgeJobWasInterrupted(job))
	job.Result = safeLegacyBookKnowledgeJobResult(job, job.Result)
	job.LeaseOwner = ""
	job.LeaseExpiresAt = ""
	job.Stage = defaultBookKnowledgeJobStage(job.Status)
	if interrupted {
		job.Status = BookKnowledgeJobStatusInterrupted
		job.Stage = defaultBookKnowledgeJobStage(job.Status)
		job.FailureCode = BookKnowledgeJobFailureWorkerInterrupted
		job.Error = bookKnowledgeJobInterruptedMessage
		job.Logs = safeLegacyBookKnowledgeJobLogs(job.Status, job.Logs)
		return job
	}
	job.Logs = safeLegacyBookKnowledgeJobLogs(job.Status, job.Logs)
	job.FailureCode = ""
	if job.Status == BookKnowledgeJobStatusFailed {
		job.Error = sanitizeBookKnowledgeJobError(job.Error)
		return job
	}
	job.Error = ""
	return job
}

func legacyBookKnowledgeJobWasInterrupted(job BookKnowledgeJob) bool {
	switch strings.ToLower(strings.TrimSpace(job.Error)) {
	case "interrupted", "interrupted by server restart", "interrupted by kbase-server restart":
		return true
	}
	for _, entry := range job.Logs {
		if strings.ToLower(strings.TrimSpace(entry)) == "failed: interrupted" {
			return true
		}
	}
	return false
}

func safeLegacyBookKnowledgeJobResult(job BookKnowledgeJob, result map[string]any) map[string]any {
	filtered := safeBookKnowledgeJobResult(result)
	if len(filtered) == 0 {
		return nil
	}
	safe := make(map[string]any, len(filtered))
	for key, value := range filtered {
		switch key {
		case "ebook_id", "download_type":
			if legacyBookKnowledgeJobResultNumber(value) {
				safe[key] = value
			}
		case "ebook_enid":
			if text, ok := value.(string); ok && text == job.EbookEnID && legacyBookKnowledgeJobResultTextIsSafe(text) {
				safe[key] = text
			}
		case "knowledge_book_id", "title":
			if text, ok := value.(string); ok && legacyBookKnowledgeJobResultTextIsSafe(text) {
				safe[key] = text
			}
		}
	}
	if len(safe) == 0 {
		return nil
	}
	return safe
}

func legacyBookKnowledgeJobResultNumber(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	default:
		return false
	}
}

func legacyBookKnowledgeJobResultTextIsSafe(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return !legacyBookKnowledgeJobTextContainsSensitiveData(value)
}

func safeLegacyBookKnowledgeJobLogs(status BookKnowledgeJobStatus, logs []string) []string {
	safe := make([]string, 0, len(logs))
	for _, entry := range logs {
		var normalized string
		switch strings.ToLower(strings.TrimSpace(entry)) {
		case "queued":
			normalized = "queued"
		case "running":
			normalized = "running"
		case "succeeded":
			normalized = "succeeded"
		case "failed":
			normalized = "failed"
		case "failed: interrupted", "interrupted":
			normalized = "interrupted"
		default:
			continue
		}
		if status == BookKnowledgeJobStatusInterrupted && normalized == "failed" {
			normalized = "interrupted"
		}
		safe = append(safe, normalized)
	}
	if len(safe) == 0 {
		return []string{string(status)}
	}
	if status == BookKnowledgeJobStatusInterrupted && safe[len(safe)-1] != "interrupted" {
		safe = append(safe, "interrupted")
	}
	return safe
}

func legacyBookKnowledgeJobTextContainsSensitiveData(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if strings.HasPrefix(value, "/") || legacyBookKnowledgeJobWindowsAbsolutePath(value) || legacyBookKnowledgeJobUNCPath(value) {
		return true
	}
	for _, marker := range []string{
		"/users/", "/home/", "/root/", "/volumes/", "/var/folders/", "/private/var/",
		"/applications/", "/library/", "/etc/", "/opt/", "/srv/", "/tmp/", "/usr/",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, marker := range []string{
		"token=", "access_token=", "secret=", "api_key=", "apikey=", "cookie=", "authorization:", "bearer ",
		"-----begin private key-----", "-----begin rsa private key-----", "-----begin ec private key-----",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if strings.Contains(lower, "-----begin ") && strings.Contains(lower, " private key-----") {
		return true
	}
	return false
}

func legacyBookKnowledgeJobWindowsAbsolutePath(value string) bool {
	for index := 0; index+2 < len(value); index++ {
		letter := value[index]
		if ((letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')) &&
			value[index+1] == ':' && (value[index+2] == '\\' || value[index+2] == '/') {
			return true
		}
	}
	return false

}

func legacyBookKnowledgeJobUNCPath(value string) bool {
	for start := 0; start < len(value); {
		offset := strings.Index(value[start:], `\\`)
		if offset < 0 {
			return false
		}
		remainder := value[start+offset+2:]
		serverEnd := strings.IndexByte(remainder, '\\')
		if serverEnd > 0 && serverEnd+1 < len(remainder) && remainder[serverEnd+1] != '\\' && !strings.ContainsAny(remainder[:serverEnd], " \t\r\n") {
			return true
		}
		start += offset + 2
	}
	return false
}

func bookKnowledgeJobStage(job BookKnowledgeJob) string {
	if stage := strings.TrimSpace(job.Stage); stage != "" {
		return stage
	}
	return defaultBookKnowledgeJobStage(job.Status)
}

func defaultBookKnowledgeJobStage(status BookKnowledgeJobStatus) string {
	switch status {
	case BookKnowledgeJobStatusQueued:
		return "queued"
	case BookKnowledgeJobStatusRunning:
		return "running"
	case BookKnowledgeJobStatusSucceeded:
		return "completed"
	case BookKnowledgeJobStatusFailed:
		return "failed"
	case BookKnowledgeJobStatusInterrupted:
		return "interrupted"
	default:
		return "queued"
	}
}

func bookKnowledgeJobsSQLiteDSN(path string, immediate bool) string {
	dsn := &url.URL{Scheme: "file", Path: path}
	query := dsn.Query()
	query.Set("_busy_timeout", fmt.Sprintf("%d", bookKnowledgeJobsSQLiteBusyTimeout))
	query.Set("_foreign_keys", "on")
	query.Set("_journal_mode", "WAL")
	if immediate {
		query.Set("_txlock", "immediate")
	}
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

func bookKnowledgeJobEventMessage(job BookKnowledgeJob) string {
	if strings.TrimSpace(job.Error) != "" {
		return job.Error
	}
	if len(job.Logs) > 0 {
		return job.Logs[len(job.Logs)-1]
	}
	return ""
}

func bookKnowledgeJobEventTime(job BookKnowledgeJob) string {
	if strings.TrimSpace(job.UpdatedAt) != "" {
		return job.UpdatedAt
	}
	return job.CreatedAt
}

func normalizeBookKnowledgeJobRequest(request BookKnowledgeJobRequest) (BookKnowledgeJobRequest, error) {
	request.Type = strings.TrimSpace(request.Type)
	request.EbookEnID = strings.TrimSpace(request.EbookEnID)
	switch request.Type {
	case BookKnowledgeJobTypeDedaoEbookDownload, BookKnowledgeJobTypeDedaoEbookSyncKBase:
	default:
		return request, fmt.Errorf("unsupported job type: %s", request.Type)
	}
	if request.EbookID <= 0 {
		return request, fmt.Errorf("ebook_id is required")
	}
	if request.EbookEnID == "" {
		return request, fmt.Errorf("ebook_enid is required")
	}
	if request.Type == BookKnowledgeJobTypeDedaoEbookSyncKBase {
		request.DownloadType = 1
		return request, nil
	}
	if request.DownloadType == 0 {
		request.DownloadType = 1
	}
	if request.DownloadType < 1 || request.DownloadType > 3 {
		return request, fmt.Errorf("download_type must be 1, 2, or 3")
	}
	return request, nil
}

func executeDedaoEbookDownloadJob(ctx context.Context, job BookKnowledgeJob) (map[string]any, error) {
	download := EBookDownload{Ctx: ctx, DownloadType: job.DownloadType, ID: job.EbookID, EnID: job.EbookEnID, OutputDir: DefaultDedaoDownloadRoot()}
	result, err := download.DownloadWithResult()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ebook_id": job.EbookID, "ebook_enid": job.EbookEnID, "download_type": job.DownloadType, "title": result.Title,
	}, nil
}

func executeDedaoEbookSyncKBaseJob(ctx context.Context, store *BookKnowledgeStore, job BookKnowledgeJob) (map[string]any, error) {
	result, err := SyncEbookToBookKnowledgeStore(ctx, job.EbookID, job.EbookEnID, store, DefaultDedaoDownloadRoot())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ebook_id": job.EbookID, "ebook_enid": job.EbookEnID, "download_type": 1,
		"knowledge_book_id": result.KnowledgeBookID, "title": result.Title,
	}, nil
}

func safeBookKnowledgeJobResult(result map[string]any) map[string]any {
	if len(result) == 0 {
		return nil
	}
	allowed := map[string]bool{
		"ebook_id": true, "ebook_enid": true, "download_type": true, "knowledge_book_id": true, "title": true,
	}
	safe := make(map[string]any, len(allowed))
	for key, value := range result {
		if allowed[key] {
			safe[key] = value
		}
	}
	return safe
}

func sanitizeBookKnowledgeJobError(_ string) string {
	return "job execution failed"
}

func DefaultDedaoDownloadRoot() string {
	if value := strings.TrimSpace(os.Getenv("DEDAO_DOWNLOAD_ROOT")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("DEDAO_KBASE_DOWNLOAD_ROOT")); value != "" {
		return value
	}
	if root := strings.TrimSpace(os.Getenv("DEDAO_KBASE_ROOT")); root != "" {
		return filepath.Join(root, defaultDedaoDownloadDir)
	}
	if root := strings.TrimSpace(os.Getenv("DEDAO_BOOK_KNOWLEDGE_ROOT")); root != "" {
		return filepath.Join(filepath.Dir(root), defaultDedaoDownloadDir)
	}
	if root := strings.TrimSpace(os.Getenv("KBASE_BOOK_KNOWLEDGE_ROOT")); root != "" {
		return filepath.Join(filepath.Dir(root), defaultDedaoDownloadDir)
	}
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		return filepath.Join(cwd, "dedao-"+defaultDedaoDownloadDir)
	}
	return filepath.Join(os.TempDir(), "dedao-kbase", defaultDedaoDownloadDir)
}

func newBookKnowledgeJobID() string {
	var randomBytes [6]byte
	_, _ = rand.Read(randomBytes[:])
	return "job_" + time.Now().UTC().Format("20060102T150405.000000000Z") + "_" + hex.EncodeToString(randomBytes[:])
}
