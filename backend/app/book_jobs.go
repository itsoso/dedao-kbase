package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-sqlite3"
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

const (
	BookKnowledgeJobFailureAuthenticationRequired = "authentication_required"
	BookKnowledgeJobFailureDownloadFailed         = "download_failed"
	BookKnowledgeJobFailureKnowledgeBuildFailed   = "knowledge_build_failed"
	BookKnowledgeJobFailureWorkerInterrupted      = "worker_interrupted"
	BookKnowledgeJobFailureSourceChanged          = "source_changed"
	BookKnowledgeJobFailureUnknownFailure         = "unknown_failure"
)

const bookKnowledgeJobInterruptedMessage = "job execution interrupted"

var (
	ErrBookKnowledgeJobConflict     = errors.New("book knowledge job conflict")
	ErrBookKnowledgeJobLeaseLost    = errors.New("book knowledge job lease lost")
	ErrBookKnowledgeJobInvalidState = errors.New("book knowledge job invalid state")
	ErrBookKnowledgeJobNotFound     = errors.New("job not found")
	errBookKnowledgeJobCASConflict  = errors.New("book knowledge job compare-and-swap conflict")
)

var bookKnowledgeJobFailureMessages = map[string]string{
	BookKnowledgeJobFailureAuthenticationRequired: "登录已失效，请重新登录",
	BookKnowledgeJobFailureDownloadFailed:         "电子书下载失败，可以重新执行",
	BookKnowledgeJobFailureKnowledgeBuildFailed:   "下载完成，但知识包生成失败",
	BookKnowledgeJobFailureWorkerInterrupted:      "Worker 升级或异常退出，任务已中断",
	BookKnowledgeJobFailureSourceChanged:          "任务参数或书籍权限已经变化",
	BookKnowledgeJobFailureUnknownFailure:         "任务执行失败，请查看诊断并重试",
}

const (
	legacyBookKnowledgeJobPathBoundaryDelimiters = "\"'`()[]{}=,:;<>"
	legacyBookKnowledgeJobPathTokenDelimiters    = "/\\\"'`()[]{}=,:;<>"
)

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

type bookKnowledgeJobCommitReceipt struct {
	JobID        string
	WorkerID     string
	BookID       string
	ContentHash  string
	PublishNonce string
	PreparedAt   string
}

var (
	runDedaoEbookDownloadJob            = executeDedaoEbookDownloadJob
	runDedaoEbookSyncKBaseJob           = executeDedaoEbookSyncKBaseJob
	runDedaoEbookSyncKBaseJobWithStages = executeDedaoEbookSyncKBaseJobWithStages
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
	db, err := s.openBookJobsDB()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	defer db.Close()
	job, err := scanBookKnowledgeJob(db.QueryRow(bookKnowledgeJobSelect+` WHERE job_id = ?`, jobID))
	if err == sql.ErrNoRows {
		return BookKnowledgeJob{}, ErrBookKnowledgeJobNotFound
	}
	return job, err
}

func (s *BookKnowledgeStore) ClaimNextBookKnowledgeJob(workerID string, lease time.Duration) (*BookKnowledgeJob, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	if strings.TrimSpace(workerID) == "" {
		return nil, fmt.Errorf("%w: worker_id is required", ErrBookKnowledgeJobInvalidState)
	}
	if lease <= 0 {
		return nil, fmt.Errorf("%w: lease must be positive", ErrBookKnowledgeJobInvalidState)
	}
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	job, err := scanBookKnowledgeJob(tx.QueryRow(bookKnowledgeJobSelect+
		` WHERE status = ? ORDER BY created_at ASC, job_id ASC LIMIT 1`, BookKnowledgeJobStatusQueued))
	if err == sql.ErrNoRows {
		tx.Rollback()
		return nil, nil
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	now := time.Now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	job.Status = BookKnowledgeJobStatusRunning
	job.Stage = defaultBookKnowledgeJobStage(job.Status)
	job.Error = ""
	job.FailureCode = ""
	job.Result = nil
	job.LeaseOwner = workerID
	job.LeaseExpiresAt = now.Add(lease).Format(time.RFC3339Nano)
	job.StartedAt = timestamp
	job.FinishedAt = ""
	job.UpdatedAt = timestamp
	job.Logs = append(job.Logs, "running")
	if err := updateBookKnowledgeJobRowFromStatus(tx, job, BookKnowledgeJobStatusQueued); err != nil {
		tx.Rollback()
		if errors.Is(err, errBookKnowledgeJobCASConflict) {
			return nil, fmt.Errorf("%w: claim job %q", ErrBookKnowledgeJobConflict, job.ID)
		}
		return nil, err
	}
	if err := appendBookKnowledgeJobEvent(tx, job); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *BookKnowledgeStore) RenewBookKnowledgeJobLease(jobID, workerID string, lease time.Duration) (BookKnowledgeJob, error) {
	if lease <= 0 {
		return BookKnowledgeJob{}, fmt.Errorf("%w: lease must be positive", ErrBookKnowledgeJobInvalidState)
	}
	return s.updateOwnedRunningBookKnowledgeJob(jobID, workerID, func(job *BookKnowledgeJob, now time.Time) error {
		currentExpiry, err := time.Parse(time.RFC3339Nano, job.LeaseExpiresAt)
		if err != nil {
			return fmt.Errorf("%w: invalid current lease", ErrBookKnowledgeJobLeaseLost)
		}
		newExpiry := now.Add(lease)
		if newExpiry.After(currentExpiry) {
			job.LeaseExpiresAt = newExpiry.Format(time.RFC3339Nano)
		}
		job.UpdatedAt = now.Format(time.RFC3339Nano)
		job.Logs = append(job.Logs, "lease renewed")
		return nil
	})
}

func (s *BookKnowledgeStore) fenceBookKnowledgeJobPackageCommit(
	jobID, workerID, bookID, contentHash string,
	minimumWindow time.Duration,
) (BookKnowledgeJob, string, error) {
	jobID = strings.TrimSpace(jobID)
	workerID = strings.TrimSpace(workerID)
	bookID = strings.TrimSpace(bookID)
	contentHash = strings.TrimSpace(contentHash)
	if jobID == "" || workerID == "" || bookID == "" || contentHash == "" || minimumWindow <= 0 {
		return BookKnowledgeJob{}, "", fmt.Errorf("%w: invalid package commit fence", ErrBookKnowledgeJobInvalidState)
	}
	publishNonce, err := newBookKnowledgeJobPublishNonce()
	if err != nil {
		return BookKnowledgeJob{}, "", err
	}
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return BookKnowledgeJob{}, "", err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return BookKnowledgeJob{}, "", err
	}
	job, err := scanBookKnowledgeJob(tx.QueryRow(bookKnowledgeJobSelect+` WHERE job_id = ?`, jobID))
	if err == sql.ErrNoRows {
		tx.Rollback()
		return BookKnowledgeJob{}, "", ErrBookKnowledgeJobNotFound
	}
	if err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, "", err
	}
	now := time.Now().UTC()
	expiry, parseErr := time.Parse(time.RFC3339Nano, job.LeaseExpiresAt)
	if job.Status != BookKnowledgeJobStatusRunning || job.LeaseOwner != workerID || parseErr != nil || !expiry.After(now) {
		tx.Rollback()
		return BookKnowledgeJob{}, "", fmt.Errorf("%w: job %q is not leased by worker", ErrBookKnowledgeJobLeaseLost, job.ID)
	}
	original := job
	minimumExpiry := now.Add(minimumWindow)
	if minimumExpiry.After(expiry) {
		job.LeaseExpiresAt = minimumExpiry.Format(time.RFC3339Nano)
	}
	job.UpdatedAt = now.Format(time.RFC3339Nano)
	job.Logs = append(job.Logs, "package commit fenced")
	if err := updateOwnedBookKnowledgeJobRow(tx, job, original); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, "", err
	}
	if _, err := tx.Exec(`
		INSERT INTO book_job_commits(job_id, worker_id, book_id, content_hash, publish_nonce, prepared_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET
			worker_id = excluded.worker_id,
			book_id = excluded.book_id,
			content_hash = excluded.content_hash,
			publish_nonce = excluded.publish_nonce,
			prepared_at = excluded.prepared_at`,
		job.ID, workerID, bookID, contentHash, publishNonce, now.Format(time.RFC3339Nano),
	); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, "", err
	}
	if err := appendBookKnowledgeJobEvent(tx, job); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return BookKnowledgeJob{}, "", err
	}
	return job, publishNonce, nil
}

func (s *BookKnowledgeStore) discardBookKnowledgeJobCommitReceipt(
	jobID, workerID, bookID, contentHash, publishNonce string,
) error {
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`
		DELETE FROM book_job_commits
		WHERE job_id = ? AND worker_id = ? AND book_id = ? AND content_hash = ? AND publish_nonce = ?`,
		strings.TrimSpace(jobID), strings.TrimSpace(workerID), strings.TrimSpace(bookID),
		strings.TrimSpace(contentHash), strings.TrimSpace(publishNonce),
	)
	return err
}

func (s *BookKnowledgeStore) UpdateBookKnowledgeJobStage(jobID, workerID, stage string) (BookKnowledgeJob, error) {
	stage = strings.TrimSpace(stage)
	if stage != "downloading" && stage != "building_knowledge" {
		return BookKnowledgeJob{}, fmt.Errorf("%w: unsupported job stage %q", ErrBookKnowledgeJobInvalidState, stage)
	}
	return s.updateOwnedRunningBookKnowledgeJob(jobID, workerID, func(job *BookKnowledgeJob, now time.Time) error {
		job.Stage = stage
		job.UpdatedAt = now.Format(time.RFC3339Nano)
		job.Logs = append(job.Logs, stage)
		return nil
	})
}

func (s *BookKnowledgeStore) CompleteBookKnowledgeJob(jobID, workerID string, result map[string]any) (BookKnowledgeJob, error) {
	return s.updateOwnedRunningBookKnowledgeJobWithTx(jobID, workerID, func(job *BookKnowledgeJob, now time.Time) error {
		timestamp := now.Format(time.RFC3339Nano)
		job.Status = BookKnowledgeJobStatusSucceeded
		job.Stage = "completed"
		job.Result = safeBookKnowledgeJobResult(result)
		job.Error = ""
		job.FailureCode = ""
		job.LeaseOwner = ""
		job.LeaseExpiresAt = ""
		job.FinishedAt = timestamp
		job.UpdatedAt = timestamp
		job.Logs = append(job.Logs, "succeeded")
		return nil
	}, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM book_job_commits WHERE job_id = ?`, strings.TrimSpace(jobID))
		return err
	})
}

func (s *BookKnowledgeStore) FailBookKnowledgeJob(jobID, workerID, code string) (BookKnowledgeJob, error) {
	code = strings.TrimSpace(code)
	message, ok := bookKnowledgeJobFailureMessages[code]
	if !ok {
		return BookKnowledgeJob{}, fmt.Errorf("%w: unsupported failure code %q", ErrBookKnowledgeJobInvalidState, code)
	}
	return s.updateOwnedRunningBookKnowledgeJob(jobID, workerID, func(job *BookKnowledgeJob, now time.Time) error {
		timestamp := now.Format(time.RFC3339Nano)
		job.Status = BookKnowledgeJobStatusFailed
		job.Stage = "failed"
		job.Result = nil
		job.FailureCode = code
		job.Error = message
		job.LeaseOwner = ""
		job.LeaseExpiresAt = ""
		job.FinishedAt = timestamp
		job.UpdatedAt = timestamp
		job.Logs = append(job.Logs, "failed")
		return nil
	})
}

func (s *BookKnowledgeStore) InterruptBookKnowledgeJob(jobID, workerID string) (BookKnowledgeJob, error) {
	return s.updateOwnedRunningBookKnowledgeJob(jobID, workerID, func(job *BookKnowledgeJob, now time.Time) error {
		interruptBookKnowledgeJob(job, now)
		return nil
	})
}

func (s *BookKnowledgeStore) markBookKnowledgeJobRecoveryRequired(jobID, workerID string) (BookKnowledgeJob, error) {
	return s.updateOwnedRunningBookKnowledgeJob(jobID, workerID, func(job *BookKnowledgeJob, now time.Time) error {
		job.Stage = "recovery_required"
		job.LeaseExpiresAt = now.Add(-time.Nanosecond).Format(time.RFC3339Nano)
		job.UpdatedAt = now.Format(time.RFC3339Nano)
		job.Logs = append(job.Logs, "recovery_required")
		return nil
	})
}

func (s *BookKnowledgeStore) ReconcileExpiredBookKnowledgeJobs() (int, error) {
	return s.ReconcileExpiredBookKnowledgeJobsContext(context.Background())
}

func (s *BookKnowledgeStore) ReconcileExpiredBookKnowledgeJobsContext(ctx context.Context) (int, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rootLock, err := s.acquireBookKnowledgeRootLock(ctx)
	if err != nil {
		return 0, err
	}
	defer rootLock.Close()
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(bookKnowledgeJobSelect+` WHERE status = ? ORDER BY created_at ASC, job_id ASC`, BookKnowledgeJobStatusRunning)
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
	now := time.Now().UTC()
	count := 0
	for _, job := range jobs {
		expiry, parseErr := time.Parse(time.RFC3339Nano, job.LeaseExpiresAt)
		if job.LeaseExpiresAt != "" && parseErr == nil && expiry.After(now) {
			continue
		}
		receipt, hasReceipt, receiptErr := loadBookKnowledgeJobCommitReceipt(tx, job.ID)
		if receiptErr != nil {
			tx.Rollback()
			return 0, receiptErr
		}
		verifiedResult := map[string]any(nil)
		if hasReceipt {
			var verified bool
			verifiedResult, verified, receiptErr = s.verifyBookKnowledgeJobCommitReceipt(job, receipt)
			if receiptErr != nil {
				tx.Rollback()
				return 0, receiptErr
			}
			if !verified {
				verifiedResult = nil
			}
		}
		if verifiedResult != nil {
			completeRecoveredBookKnowledgeJob(&job, now, verifiedResult)
		} else {
			interruptBookKnowledgeJob(&job, now)
		}
		if err := updateBookKnowledgeJobRowFromStatus(tx, job, BookKnowledgeJobStatusRunning); err != nil {
			tx.Rollback()
			return 0, err
		}
		if hasReceipt {
			if _, err := tx.Exec(`DELETE FROM book_job_commits WHERE job_id = ?`, job.ID); err != nil {
				tx.Rollback()
				return 0, err
			}
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

func loadBookKnowledgeJobCommitReceipt(tx *sql.Tx, jobID string) (bookKnowledgeJobCommitReceipt, bool, error) {
	var receipt bookKnowledgeJobCommitReceipt
	err := tx.QueryRow(`
		SELECT job_id, worker_id, book_id, content_hash, publish_nonce, prepared_at
		FROM book_job_commits WHERE job_id = ?`, strings.TrimSpace(jobID),
	).Scan(
		&receipt.JobID, &receipt.WorkerID, &receipt.BookID, &receipt.ContentHash,
		&receipt.PublishNonce, &receipt.PreparedAt,
	)
	if err == sql.ErrNoRows {
		return bookKnowledgeJobCommitReceipt{}, false, nil
	}
	if err != nil {
		return bookKnowledgeJobCommitReceipt{}, false, err
	}
	return receipt, true, nil
}

func (s *BookKnowledgeStore) verifyBookKnowledgeJobCommitReceipt(
	job BookKnowledgeJob,
	receipt bookKnowledgeJobCommitReceipt,
) (map[string]any, bool, error) {
	if job.Type != BookKnowledgeJobTypeDedaoEbookSyncKBase || receipt.JobID != job.ID ||
		strings.TrimSpace(receipt.WorkerID) == "" || receipt.BookID != strconv.Itoa(job.EbookID) ||
		strings.TrimSpace(receipt.ContentHash) == "" || strings.TrimSpace(receipt.PublishNonce) == "" {
		return nil, false, nil
	}
	if _, err := time.Parse(time.RFC3339Nano, receipt.PreparedAt); err != nil {
		return nil, false, nil
	}
	if _, err := s.recoverBookKnowledgePublishTransaction(job, receipt); err != nil {
		return nil, false, err
	}
	marker, err := readBookKnowledgeJobCommitMarker(s.BookDir(receipt.BookID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errBookKnowledgeJobCommitMarkerInvalid) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if marker.Version != bookKnowledgeJobCommitMarkerVersion || marker.JobID != receipt.JobID ||
		marker.PublishNonce != receipt.PublishNonce || marker.BookID != receipt.BookID ||
		marker.ContentHash != receipt.ContentHash {
		return nil, false, nil
	}
	pkg, err := s.loadPackageUnlocked(receipt.BookID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if pkg.Book.BookID != receipt.BookID || pkg.Book.DedaoID != job.EbookID || pkg.Book.EnID != job.EbookEnID ||
		pkg.Book.ContentHash != receipt.ContentHash {
		return nil, false, nil
	}
	computedHash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		return nil, false, err
	}
	if computedHash != receipt.ContentHash {
		return nil, false, nil
	}
	manifest, err := s.loadManifest()
	if err != nil {
		return nil, false, err
	}
	manifestMatches := false
	for _, book := range manifest.Books {
		if book.BookID == receipt.BookID && book.ContentHash == receipt.ContentHash &&
			book.DedaoID == job.EbookID && book.EnID == job.EbookEnID {
			manifestMatches = true
			break
		}
	}
	if !manifestMatches {
		return nil, false, nil
	}
	return safeBookKnowledgeJobResult(map[string]any{
		"ebook_id":          job.EbookID,
		"ebook_enid":        job.EbookEnID,
		"download_type":     1,
		"knowledge_book_id": pkg.Book.BookID,
		"title":             pkg.Book.Title,
	}), true, nil
}

func completeRecoveredBookKnowledgeJob(job *BookKnowledgeJob, now time.Time, result map[string]any) {
	timestamp := now.UTC().Format(time.RFC3339Nano)
	job.Status = BookKnowledgeJobStatusSucceeded
	job.Stage = "completed"
	job.Result = safeBookKnowledgeJobResult(result)
	job.Error = ""
	job.FailureCode = ""
	job.LeaseOwner = ""
	job.LeaseExpiresAt = ""
	job.FinishedAt = timestamp
	job.UpdatedAt = timestamp
	job.Logs = append(job.Logs, "succeeded: recovered committed package")
}

func (s *BookKnowledgeStore) RetryBookKnowledgeJob(jobID string) (BookKnowledgeJob, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return BookKnowledgeJob{}, fmt.Errorf("%w: job_id is required", ErrBookKnowledgeJobInvalidState)
	}
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	original, err := scanBookKnowledgeJob(tx.QueryRow(bookKnowledgeJobSelect+` WHERE job_id = ?`, jobID))
	if err == sql.ErrNoRows {
		tx.Rollback()
		return BookKnowledgeJob{}, ErrBookKnowledgeJobNotFound
	}
	if err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if original.Status != BookKnowledgeJobStatusFailed && original.Status != BookKnowledgeJobStatusInterrupted {
		tx.Rollback()
		return BookKnowledgeJob{}, fmt.Errorf("%w: job %q cannot retry from status %s", ErrBookKnowledgeJobInvalidState, original.ID, original.Status)
	}
	var activeRetryID string
	err = tx.QueryRow(`
		SELECT job_id FROM book_jobs
		WHERE retry_of = ? AND status IN (?, ?)
		ORDER BY created_at ASC, job_id ASC LIMIT 1`,
		original.ID, BookKnowledgeJobStatusQueued, BookKnowledgeJobStatusRunning,
	).Scan(&activeRetryID)
	if err == nil {
		tx.Rollback()
		return BookKnowledgeJob{}, fmt.Errorf("%w: active retry %q already exists", ErrBookKnowledgeJobConflict, activeRetryID)
	}
	if err != sql.ErrNoRows {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	retry := BookKnowledgeJob{
		ID: newBookKnowledgeJobID(), Type: original.Type, Status: BookKnowledgeJobStatusQueued,
		EbookID: original.EbookID, EbookEnID: original.EbookEnID, DownloadType: original.DownloadType,
		RetryOf: original.ID, Stage: "queued", Logs: []string{"queued"}, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := insertBookKnowledgeJob(tx, retry, false); err != nil {
		activeRetryConflict := isBookKnowledgeJobActiveRetryConstraint(tx, original.ID, err)
		tx.Rollback()
		if activeRetryConflict {
			return BookKnowledgeJob{}, fmt.Errorf("%w: active retry already exists", ErrBookKnowledgeJobConflict)
		}
		return BookKnowledgeJob{}, err
	}
	if err := appendBookKnowledgeJobEvent(tx, retry); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return BookKnowledgeJob{}, err
	}
	return retry, nil
}

func (s *BookKnowledgeStore) ExportLegacyBookKnowledgeJobs(path string) error {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("export path is required")
	}
	db, err := s.openBookJobsDB()
	if err != nil {
		return err
	}
	if err := validateBookKnowledgeJobExportPath(path, s.BookJobsDBPath()); err != nil {
		db.Close()
		return err
	}
	rows, err := db.Query(bookKnowledgeJobSelect + ` ORDER BY created_at ASC, job_id ASC`)
	if err != nil {
		db.Close()
		return err
	}
	legacy := bookKnowledgeJobsFile{Jobs: make([]BookKnowledgeJob, 0)}
	for rows.Next() {
		job, scanErr := scanBookKnowledgeJob(rows)
		if scanErr != nil {
			rows.Close()
			db.Close()
			return scanErr
		}
		legacy.Jobs = append(legacy.Jobs, legacyBookKnowledgeJobForExport(job))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		db.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".book-jobs-export-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			temporary.Close()
			os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return syncBookKnowledgeJobExportDirectory(directory, runtime.GOOS)
}

func syncBookKnowledgeJobExportDirectory(directory, goos string) error {
	if goos == "windows" {
		return nil
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}

func validateBookKnowledgeJobExportPath(exportPath, databasePath string) error {
	resolvedExport, err := resolveBookKnowledgeJobPath(exportPath)
	if err != nil {
		return err
	}
	exportInfo, exportInfoErr := os.Stat(exportPath)
	if exportInfoErr != nil && !os.IsNotExist(exportInfoErr) {
		return exportInfoErr
	}
	for _, protectedPath := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		resolvedProtected, err := resolveBookKnowledgeJobPath(protectedPath)
		if err != nil {
			return err
		}
		if bookKnowledgeJobPathsEqual(resolvedExport, resolvedProtected) {
			return fmt.Errorf("%w: export target overlaps book jobs database", ErrBookKnowledgeJobInvalidState)
		}
		protectedInfo, protectedInfoErr := os.Stat(protectedPath)
		if protectedInfoErr != nil && !os.IsNotExist(protectedInfoErr) {
			return protectedInfoErr
		}
		if exportInfoErr == nil && protectedInfoErr == nil && os.SameFile(exportInfo, protectedInfo) {
			return fmt.Errorf("%w: export target overlaps book jobs database", ErrBookKnowledgeJobInvalidState)
		}
	}
	return nil
}

func resolveBookKnowledgeJobPath(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	current := absolute
	missing := make([]string, 0)
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absolute, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func bookKnowledgeJobPathsEqual(first, second string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(first, second)
	}
	return first == second
}

func (s *BookKnowledgeStore) updateOwnedRunningBookKnowledgeJob(
	jobID string,
	workerID string,
	mutate func(*BookKnowledgeJob, time.Time) error,
) (BookKnowledgeJob, error) {
	return s.updateOwnedRunningBookKnowledgeJobWithTx(jobID, workerID, mutate, nil)
}

func (s *BookKnowledgeStore) updateOwnedRunningBookKnowledgeJobWithTx(
	jobID string,
	workerID string,
	mutate func(*BookKnowledgeJob, time.Time) error,
	afterUpdate func(*sql.Tx) error,
) (BookKnowledgeJob, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return BookKnowledgeJob{}, fmt.Errorf("%w: job_id is required", ErrBookKnowledgeJobInvalidState)
	}
	if strings.TrimSpace(workerID) == "" {
		return BookKnowledgeJob{}, fmt.Errorf("%w: worker_id is required", ErrBookKnowledgeJobInvalidState)
	}
	db, err := s.openBookJobsWriteDB()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	job, err := scanBookKnowledgeJob(tx.QueryRow(bookKnowledgeJobSelect+` WHERE job_id = ?`, jobID))
	if err == sql.ErrNoRows {
		tx.Rollback()
		return BookKnowledgeJob{}, ErrBookKnowledgeJobNotFound
	}
	if err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if job.Status != BookKnowledgeJobStatusRunning {
		tx.Rollback()
		return BookKnowledgeJob{}, fmt.Errorf("%w: job %q is %s", ErrBookKnowledgeJobInvalidState, job.ID, job.Status)
	}
	now := time.Now().UTC()
	expiry, err := time.Parse(time.RFC3339Nano, job.LeaseExpiresAt)
	if job.LeaseOwner != workerID || err != nil || !expiry.After(now) {
		tx.Rollback()
		return BookKnowledgeJob{}, fmt.Errorf("%w: job %q is not leased by worker", ErrBookKnowledgeJobLeaseLost, job.ID)
	}
	original := job
	if err := mutate(&job, now); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if err := updateOwnedBookKnowledgeJobRow(tx, job, original); err != nil {
		tx.Rollback()
		return BookKnowledgeJob{}, err
	}
	if afterUpdate != nil {
		if err := afterUpdate(tx); err != nil {
			tx.Rollback()
			return BookKnowledgeJob{}, err
		}
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

func (s *BookKnowledgeStore) FailInterruptedBookKnowledgeJobs(reason string) (int, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "interrupted"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
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
	if err := ensureBookKnowledgePrivateRoot(s.root); err != nil {
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
	CREATE UNIQUE INDEX IF NOT EXISTS idx_book_jobs_one_active_retry
		ON book_jobs(retry_of)
		WHERE retry_of IS NOT NULL AND retry_of <> '' AND status IN ('queued', 'running');
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
	CREATE TABLE IF NOT EXISTS book_job_commits (
		job_id TEXT PRIMARY KEY,
		worker_id TEXT NOT NULL,
		book_id TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		publish_nonce TEXT NOT NULL DEFAULT '',
		prepared_at TEXT NOT NULL,
		FOREIGN KEY(job_id) REFERENCES book_jobs(job_id) ON DELETE CASCADE
	);
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
	if err := ensureBookKnowledgeJobCommitReceiptSchema(tx); err != nil {
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

func ensureBookKnowledgeJobCommitReceiptSchema(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(book_job_commits)`)
	if err != nil {
		return err
	}
	hasPublishNonce := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "publish_nonce" {
			hasPublishNonce = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if hasPublishNonce {
		return nil
	}
	_, err = tx.Exec(`ALTER TABLE book_job_commits ADD COLUMN publish_nonce TEXT NOT NULL DEFAULT ''`)
	return err
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
			return fmt.Errorf("%w: job %q changed from status %s", errBookKnowledgeJobCASConflict, job.ID, expectedStatus)
		}
		return fmt.Errorf("job not found")
	}
	return nil
}

func updateOwnedBookKnowledgeJobRow(tx *sql.Tx, job, original BookKnowledgeJob) error {
	resultJSON, logsJSON, err := marshalBookKnowledgeJobJSON(job)
	if err != nil {
		return err
	}
	result, err := tx.Exec(`
		UPDATE book_jobs SET
			job_type = ?, status = ?, ebook_id = ?, ebook_enid = ?, download_type = ?,
			result_json = ?, logs_json = ?, retry_of = NULLIF(?, ''), stage = ?, failure_code = ?,
			lease_owner = ?, lease_expires_at = ?, failure_message = ?, created_at = ?,
			updated_at = ?, started_at = ?, finished_at = ?
		WHERE job_id = ? AND status = ? AND lease_owner = ? AND lease_expires_at = ?`,
		job.Type, job.Status, job.EbookID, job.EbookEnID, job.DownloadType,
		resultJSON, logsJSON, job.RetryOf, bookKnowledgeJobStage(job), job.FailureCode,
		job.LeaseOwner, job.LeaseExpiresAt, job.Error, job.CreatedAt, job.UpdatedAt,
		job.StartedAt, job.FinishedAt, job.ID, BookKnowledgeJobStatusRunning,
		original.LeaseOwner, original.LeaseExpiresAt,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%w: job %q lease changed", ErrBookKnowledgeJobLeaseLost, job.ID)
	}
	return nil
}

func interruptBookKnowledgeJob(job *BookKnowledgeJob, now time.Time) {
	timestamp := now.UTC().Format(time.RFC3339Nano)
	job.Status = BookKnowledgeJobStatusInterrupted
	job.Stage = "interrupted"
	job.Result = nil
	job.FailureCode = BookKnowledgeJobFailureWorkerInterrupted
	job.Error = bookKnowledgeJobFailureMessages[BookKnowledgeJobFailureWorkerInterrupted]
	job.LeaseOwner = ""
	job.LeaseExpiresAt = ""
	job.FinishedAt = timestamp
	job.UpdatedAt = timestamp
	job.Logs = append(job.Logs, "interrupted")
}

func isBookKnowledgeJobActiveRetryConstraint(tx *sql.Tx, parentJobID string, err error) bool {
	var sqliteErr sqlite3.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
		return false
	}
	var activeRetryID string
	err = tx.QueryRow(`
		SELECT job_id FROM book_jobs
		WHERE retry_of = ? AND status IN (?, ?)
		ORDER BY created_at ASC, job_id ASC LIMIT 1`,
		parentJobID, BookKnowledgeJobStatusQueued, BookKnowledgeJobStatusRunning,
	).Scan(&activeRetryID)
	return err == nil && activeRetryID != ""
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

func legacyBookKnowledgeJobForExport(job BookKnowledgeJob) BookKnowledgeJob {
	legacyStatus := job.Status
	logs := safeLegacyBookKnowledgeJobLogs(job.Status, job.Logs)
	errorMessage := ""
	if job.Status == BookKnowledgeJobStatusInterrupted {
		legacyStatus = BookKnowledgeJobStatusFailed
		errorMessage = bookKnowledgeJobInterruptedMessage
		legacyLogs := make([]string, 0, len(logs)+1)
		for _, entry := range logs {
			if entry != "interrupted" && entry != "failed" && entry != "failed: interrupted" {
				legacyLogs = append(legacyLogs, entry)
			}
		}
		logs = append(legacyLogs, "failed: interrupted")
	} else if job.Status == BookKnowledgeJobStatusFailed {
		errorMessage = sanitizeBookKnowledgeJobError(job.Error)
	}
	return BookKnowledgeJob{
		ID:           job.ID,
		Type:         job.Type,
		Status:       legacyStatus,
		EbookID:      job.EbookID,
		EbookEnID:    job.EbookEnID,
		DownloadType: job.DownloadType,
		Result:       safeLegacyBookKnowledgeJobResult(job, job.Result),
		Error:        errorMessage,
		Logs:         logs,
		CreatedAt:    job.CreatedAt,
		UpdatedAt:    job.UpdatedAt,
		StartedAt:    job.StartedAt,
		FinishedAt:   job.FinishedAt,
	}
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
	if legacyBookKnowledgeJobUnixAbsolutePath(value) || legacyBookKnowledgeJobWindowsAbsolutePath(value) || legacyBookKnowledgeJobUNCPath(value) {
		return true
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

func legacyBookKnowledgeJobUnixAbsolutePath(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '/' || !legacyBookKnowledgeJobPathBoundary(value, index) || index+1 == len(value) {
			continue
		}
		next, _ := utf8.DecodeRuneInString(value[index+1:])
		if !unicode.IsSpace(next) && !strings.ContainsRune(legacyBookKnowledgeJobPathTokenDelimiters, next) {
			return true
		}
	}
	return false
}

func legacyBookKnowledgeJobPathBoundary(value string, index int) bool {
	if index == 0 {
		return true
	}
	previous, _ := utf8.DecodeLastRuneInString(value[:index])
	if previous == '/' || previous == '\\' {
		return false
	}
	return unicode.IsSpace(previous) || unicode.IsPunct(previous) ||
		strings.ContainsRune(legacyBookKnowledgeJobPathBoundaryDelimiters, previous)
}

func legacyBookKnowledgeJobWindowsAbsolutePath(value string) bool {
	for index := 0; index+2 < len(value); index++ {
		letter := value[index]
		if ((letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')) &&
			value[index+1] == ':' && (value[index+2] == '\\' || value[index+2] == '/') &&
			legacyBookKnowledgeJobPathBoundary(value, index) {
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
	return dedaoEbookSyncKBaseJobResult(job, result), nil
}

func executeDedaoEbookSyncKBaseJobWithStages(
	ctx context.Context,
	store *BookKnowledgeStore,
	job BookKnowledgeJob,
	setStage func(string) error,
) (map[string]any, error) {
	result, err := syncEbookToBookKnowledgeStoreWithStages(
		ctx, job.EbookID, job.EbookEnID, store, DefaultDedaoDownloadRoot(), setStage,
	)
	if err != nil {
		return nil, err
	}
	return dedaoEbookSyncKBaseJobResult(job, result), nil
}

func dedaoEbookSyncKBaseJobResult(job BookKnowledgeJob, result *EbookWikiSyncResult) map[string]any {
	return map[string]any{
		"ebook_id": job.EbookID, "ebook_enid": job.EbookEnID, "download_type": 1,
		"knowledge_book_id": result.KnowledgeBookID, "title": result.Title,
	}
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

func newBookKnowledgeJobPublishNonce() (string, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", fmt.Errorf("generate package publish nonce: %w", err)
	}
	return hex.EncodeToString(randomBytes[:]), nil
}
