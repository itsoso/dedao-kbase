package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
)

const (
	bookKnowledgeJobsFileName = "jobs.json"
	defaultDedaoDownloadDir   = "downloads"
)

type BookKnowledgeJobStatus string

const (
	BookKnowledgeJobStatusQueued    BookKnowledgeJobStatus = "queued"
	BookKnowledgeJobStatusRunning   BookKnowledgeJobStatus = "running"
	BookKnowledgeJobStatusSucceeded BookKnowledgeJobStatus = "succeeded"
	BookKnowledgeJobStatusFailed    BookKnowledgeJobStatus = "failed"
)

const (
	BookKnowledgeJobTypeDedaoEbookDownload  = "dedao_ebook_download"
	BookKnowledgeJobTypeDedaoEbookSyncKBase = "dedao_ebook_sync_kbase"
)

type BookKnowledgeJob struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Status       BookKnowledgeJobStatus `json:"status"`
	EbookID      int                    `json:"ebook_id"`
	EbookEnID    string                 `json:"ebook_enid"`
	DownloadType int                    `json:"download_type"`
	Result       map[string]any         `json:"result,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Logs         []string               `json:"logs,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
	StartedAt    string                 `json:"started_at,omitempty"`
	FinishedAt   string                 `json:"finished_at,omitempty"`
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
		Logs: []string{"queued"}, CreatedAt: now, UpdatedAt: now,
	}

	bookKnowledgeJobsMu.Lock()
	defer bookKnowledgeJobsMu.Unlock()
	file, err := s.readBookKnowledgeJobsFileLocked()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	file.Jobs = append(file.Jobs, job)
	if err := s.writeBookKnowledgeJobsFileLocked(file); err != nil {
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
	file, err := s.readBookKnowledgeJobsFileLocked()
	if err != nil {
		return nil, err
	}
	jobs := append([]BookKnowledgeJob(nil), file.Jobs...)
	sort.SliceStable(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt != jobs[j].CreatedAt {
			return jobs[i].CreatedAt > jobs[j].CreatedAt
		}
		return jobs[i].ID > jobs[j].ID
	})
	if len(jobs) > limit {
		jobs = jobs[:limit]
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
	file, err := s.readBookKnowledgeJobsFileLocked()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	for _, job := range file.Jobs {
		if job.ID == jobID {
			return job, nil
		}
	}
	return BookKnowledgeJob{}, fmt.Errorf("job not found")
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
	file, err := s.readBookKnowledgeJobsFileLocked()
	if err != nil {
		return 0, err
	}
	count := 0
	for i, job := range file.Jobs {
		if job.Status != BookKnowledgeJobStatusQueued && job.Status != BookKnowledgeJobStatusRunning {
			continue
		}
		job.Status = BookKnowledgeJobStatusFailed
		job.Error = sanitizeBookKnowledgeJobError(reason)
		job.UpdatedAt, job.FinishedAt = now, now
		job.Logs = append(job.Logs, "failed: interrupted")
		file.Jobs[i] = job
		count++
	}
	if count == 0 {
		return 0, nil
	}
	if err := s.writeBookKnowledgeJobsFileLocked(file); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *BookKnowledgeStore) RunBookKnowledgeJob(jobID string) error {
	return s.RunBookKnowledgeJobWithService(jobID, getService())
}

func (s *BookKnowledgeStore) RunBookKnowledgeJobWithService(jobID string, service *services.Service) error {
	job, err := s.updateBookKnowledgeJob(jobID, func(job BookKnowledgeJob) BookKnowledgeJob {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		job.Status, job.StartedAt, job.UpdatedAt = BookKnowledgeJobStatusRunning, now, now
		job.Logs = append(job.Logs, "running")
		return job
	})
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
	file, err := s.readBookKnowledgeJobsFileLocked()
	if err != nil {
		return BookKnowledgeJob{}, err
	}
	for i, job := range file.Jobs {
		if job.ID != jobID {
			continue
		}
		updated := mutate(job)
		file.Jobs[i] = updated
		if err := s.writeBookKnowledgeJobsFileLocked(file); err != nil {
			return BookKnowledgeJob{}, err
		}
		return updated, nil
	}
	return BookKnowledgeJob{}, fmt.Errorf("job not found")
}

func (s *BookKnowledgeStore) readBookKnowledgeJobsFileLocked() (bookKnowledgeJobsFile, error) {
	file := bookKnowledgeJobsFile{Jobs: []BookKnowledgeJob{}}
	if err := readJSONFile(s.JobsPath(), &file); err != nil {
		if os.IsNotExist(err) {
			return file, nil
		}
		return file, err
	}
	if file.Jobs == nil {
		file.Jobs = []BookKnowledgeJob{}
	}
	return file, nil
}

func (s *BookKnowledgeStore) writeBookKnowledgeJobsFileLocked(file bookKnowledgeJobsFile) error {
	if err := os.MkdirAll(s.root, 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomically(s.JobsPath(), append(data, '\n')); err != nil {
		return err
	}
	return os.Chmod(s.JobsPath(), 0o600)
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
