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
	BookKnowledgeJobTypeNotebookLMExport    = "notebooklm_export"
	BookKnowledgeJobTypeBookExport          = "book_export"
	BookKnowledgeJobTypeDedaoEbookDownload  = "dedao_ebook_download"
	BookKnowledgeJobTypeDedaoEbookSyncKBase = "dedao_ebook_sync_kbase"
	BookKnowledgeJobTypeDedaoOdobDownload   = "dedao_odob_download"
	BookKnowledgeJobTypeDedaoOdobSyncKBase  = "dedao_odob_sync_kbase"
)

type BookKnowledgeJob struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Status       BookKnowledgeJobStatus `json:"status"`
	BookID       string                 `json:"book_id,omitempty"`
	Target       string                 `json:"target,omitempty"`
	EbookID      int                    `json:"ebook_id,omitempty"`
	EbookEnID    string                 `json:"ebook_enid,omitempty"`
	OdobID       int                    `json:"odob_id,omitempty"`
	OdobEnID     string                 `json:"odob_enid,omitempty"`
	OdobTitle    string                 `json:"odob_title,omitempty"`
	OdobAliasID  string                 `json:"odob_alias_id,omitempty"`
	OdobCanPlay  bool                   `json:"odob_can_play,omitempty"`
	DownloadType int                    `json:"download_type,omitempty"`
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
	BookID       string `json:"book_id,omitempty"`
	Target       string `json:"target,omitempty"`
	EbookID      int    `json:"ebook_id,omitempty"`
	EbookEnID    string `json:"ebook_enid,omitempty"`
	OdobID       int    `json:"odob_id,omitempty"`
	OdobEnID     string `json:"odob_enid,omitempty"`
	OdobTitle    string `json:"odob_title,omitempty"`
	OdobAliasID  string `json:"odob_alias_id,omitempty"`
	OdobCanPlay  bool   `json:"odob_can_play,omitempty"`
	DownloadType int    `json:"download_type,omitempty"`
}

type bookKnowledgeJobsFile struct {
	Jobs []BookKnowledgeJob `json:"jobs"`
}

var bookKnowledgeJobsMu sync.Mutex

var (
	runDedaoEbookDownloadJob  = executeDedaoEbookDownloadJob
	runDedaoEbookSyncKBaseJob = executeDedaoEbookSyncKBaseJob
	runDedaoOdobDownloadJob   = executeDedaoOdobDownloadJob
	runDedaoOdobSyncKBaseJob  = executeDedaoOdobSyncKBaseJob
)

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
		ID:           newBookKnowledgeJobID(),
		Type:         normalized.Type,
		Status:       BookKnowledgeJobStatusQueued,
		BookID:       normalized.BookID,
		Target:       normalized.Target,
		EbookID:      normalized.EbookID,
		EbookEnID:    normalized.EbookEnID,
		OdobID:       normalized.OdobID,
		OdobEnID:     normalized.OdobEnID,
		OdobTitle:    normalized.OdobTitle,
		OdobAliasID:  normalized.OdobAliasID,
		OdobCanPlay:  normalized.OdobCanPlay,
		DownloadType: normalized.DownloadType,
		Logs:         []string{"queued"},
		CreatedAt:    now,
		UpdatedAt:    now,
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
	return BookKnowledgeJob{}, fmt.Errorf("job not found: %s", jobID)
}

func (s *BookKnowledgeStore) FailRunningBookKnowledgeJobs(reason string) (int, error) {
	return s.failBookKnowledgeJobs(reason, func(job BookKnowledgeJob) bool {
		return job.Status == BookKnowledgeJobStatusRunning
	})
}

func (s *BookKnowledgeStore) FailInterruptedBookKnowledgeJobs(reason string) (int, error) {
	return s.failBookKnowledgeJobs(reason, func(job BookKnowledgeJob) bool {
		return job.Status == BookKnowledgeJobStatusQueued || job.Status == BookKnowledgeJobStatusRunning
	})
}

func (s *BookKnowledgeStore) failBookKnowledgeJobs(reason string, shouldFail func(BookKnowledgeJob) bool) (int, error) {
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
		if !shouldFail(job) {
			continue
		}
		job.Status = BookKnowledgeJobStatusFailed
		job.Error = reason
		job.UpdatedAt = now
		job.FinishedAt = now
		job.Logs = append(job.Logs, "failed: "+reason)
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

func (s *BookKnowledgeStore) RunBookKnowledgeJob(jobID string) {
	job, err := s.updateBookKnowledgeJob(jobID, func(job BookKnowledgeJob) BookKnowledgeJob {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		job.Status = BookKnowledgeJobStatusRunning
		job.StartedAt = now
		job.UpdatedAt = now
		job.Logs = append(job.Logs, "running")
		return job
	})
	if err != nil {
		return
	}

	result, runErr := s.executeBookKnowledgeJob(job)
	_, _ = s.updateBookKnowledgeJob(job.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		job.UpdatedAt = now
		job.FinishedAt = now
		if runErr != nil {
			job.Status = BookKnowledgeJobStatusFailed
			job.Error = runErr.Error()
			job.Logs = append(job.Logs, "failed: "+runErr.Error())
			return job
		}
		job.Status = BookKnowledgeJobStatusSucceeded
		job.Result = result
		job.Logs = append(job.Logs, "succeeded")
		return job
	})
}

func (s *BookKnowledgeStore) executeBookKnowledgeJob(job BookKnowledgeJob) (map[string]any, error) {
	switch job.Type {
	case BookKnowledgeJobTypeNotebookLMExport:
		bridge, err := ExportNotebookLMBridgePackage(s, job.BookID)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"book_id":           bridge.BookID,
			"notebook_url":      bridge.NotebookURL,
			"last_export_dir":   bridge.LastExportDir,
			"last_export_files": bridge.LastExportFiles,
			"updated_at":        bridge.UpdatedAt,
		}, nil
	case BookKnowledgeJobTypeBookExport:
		result, err := ExportBookKnowledgePackage(s, job.BookID, job.Target)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"book_id":    result.BookID,
			"target":     result.Target,
			"output_dir": result.OutputDir,
			"files":      result.Files,
		}, nil
	case BookKnowledgeJobTypeDedaoEbookDownload:
		return runDedaoEbookDownloadJob(context.Background(), job)
	case BookKnowledgeJobTypeDedaoEbookSyncKBase:
		return runDedaoEbookSyncKBaseJob(context.Background(), s, job)
	case BookKnowledgeJobTypeDedaoOdobDownload:
		return runDedaoOdobDownloadJob(context.Background(), job)
	case BookKnowledgeJobTypeDedaoOdobSyncKBase:
		return runDedaoOdobSyncKBaseJob(context.Background(), s, job)
	default:
		return nil, fmt.Errorf("unknown job type: %s", job.Type)
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
	return BookKnowledgeJob{}, fmt.Errorf("job not found: %s", jobID)
}

func (s *BookKnowledgeStore) readBookKnowledgeJobsFileLocked() (bookKnowledgeJobsFile, error) {
	var file bookKnowledgeJobsFile
	path := s.JobsPath()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			file.Jobs = []BookKnowledgeJob{}
			return file, nil
		}
		return file, err
	}
	if err := readJSONFile(path, &file); err != nil {
		return file, err
	}
	if file.Jobs == nil {
		file.Jobs = []BookKnowledgeJob{}
	}
	return file, nil
}

func (s *BookKnowledgeStore) writeBookKnowledgeJobsFileLocked(file bookKnowledgeJobsFile) error {
	if err := os.MkdirAll(s.root, os.ModePerm); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.JobsPath(), append(data, '\n'), 0o600)
}

func normalizeBookKnowledgeJobRequest(request BookKnowledgeJobRequest) (BookKnowledgeJobRequest, error) {
	request.Type = strings.TrimSpace(request.Type)
	request.BookID = strings.TrimSpace(request.BookID)
	request.Target = strings.TrimSpace(request.Target)
	request.EbookEnID = strings.TrimSpace(request.EbookEnID)
	request.OdobEnID = strings.TrimSpace(request.OdobEnID)
	request.OdobTitle = strings.TrimSpace(request.OdobTitle)
	request.OdobAliasID = strings.TrimSpace(request.OdobAliasID)
	switch request.Type {
	case BookKnowledgeJobTypeNotebookLMExport:
		if request.BookID == "" {
			return request, fmt.Errorf("book_id is required")
		}
	case BookKnowledgeJobTypeBookExport:
		if request.BookID == "" {
			return request, fmt.Errorf("book_id is required")
		}
		switch request.Target {
		case BookKnowledgeExportHealthSystemKBV2, BookKnowledgeExportQuantRuleCards:
		default:
			return request, fmt.Errorf("unsupported book export target: %s", request.Target)
		}
	case BookKnowledgeJobTypeDedaoEbookDownload:
		return normalizeDedaoEbookJobRequest(request, true)
	case BookKnowledgeJobTypeDedaoEbookSyncKBase:
		return normalizeDedaoEbookJobRequest(request, false)
	case BookKnowledgeJobTypeDedaoOdobDownload:
		return normalizeDedaoOdobJobRequest(request)
	case BookKnowledgeJobTypeDedaoOdobSyncKBase:
		request.DownloadType = 3
		return normalizeDedaoOdobJobRequest(request)
	default:
		return request, fmt.Errorf("unsupported job type: %s", request.Type)
	}
	return request, nil
}

func normalizeDedaoEbookJobRequest(request BookKnowledgeJobRequest, allowDownloadType bool) (BookKnowledgeJobRequest, error) {
	if request.EbookID <= 0 {
		return request, fmt.Errorf("ebook_id is required")
	}
	if request.EbookEnID == "" {
		return request, fmt.Errorf("ebook_enid is required")
	}
	if !allowDownloadType {
		request.DownloadType = 1
		return request, nil
	}
	if request.DownloadType == 0 {
		request.DownloadType = 1
	}
	switch request.DownloadType {
	case 1, 2, 3:
		return request, nil
	default:
		return request, fmt.Errorf("download_type must be 1, 2, or 3")
	}
}

func normalizeDedaoOdobJobRequest(request BookKnowledgeJobRequest) (BookKnowledgeJobRequest, error) {
	if request.OdobID <= 0 {
		return request, fmt.Errorf("odob_id is required")
	}
	if request.OdobEnID == "" {
		return request, fmt.Errorf("odob_enid is required")
	}
	if request.OdobAliasID == "" {
		return request, fmt.Errorf("odob_alias_id is required")
	}
	if request.DownloadType == 0 {
		request.DownloadType = 1
	}
	switch request.DownloadType {
	case 1, 2, 3:
		return request, nil
	default:
		return request, fmt.Errorf("download_type must be 1, 2, or 3")
	}
}

func executeDedaoEbookDownloadJob(ctx context.Context, job BookKnowledgeJob) (map[string]any, error) {
	downloadRoot := DefaultDedaoDownloadRoot()
	download := EBookDownload{
		Ctx:          ctx,
		DownloadType: job.DownloadType,
		ID:           job.EbookID,
		EnID:         job.EbookEnID,
		OutputDir:    downloadRoot,
	}
	result, err := download.DownloadWithResult()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"book_id":       fmt.Sprintf("%d", job.EbookID),
		"ebook_id":      job.EbookID,
		"ebook_enid":    job.EbookEnID,
		"download_type": job.DownloadType,
		"output_dir":    downloadRoot,
		"title":         result.Title,
		"html_path":     result.HTMLPath,
	}, nil
}

func executeDedaoOdobDownloadJob(ctx context.Context, job BookKnowledgeJob) (map[string]any, error) {
	downloadRoot := DefaultDedaoDownloadRoot()
	title := firstNonEmpty(job.OdobTitle, fmt.Sprintf("%d", job.OdobID))
	download := OdobDownload{
		Ctx:          ctx,
		DownloadType: job.DownloadType,
		ID:           job.OdobID,
		OutputDir:    downloadRoot,
		Data: &services.Course{
			Enid:        job.OdobEnID,
			ID:          job.OdobID,
			ClassID:     job.OdobID,
			Title:       title,
			HasPlayAuth: job.OdobCanPlay,
			AudioDetail: services.Audio{
				AliasID: job.OdobAliasID,
				Title:   title,
			},
		},
	}
	if err := download.Download(); err != nil {
		return nil, err
	}
	return map[string]any{
		"odob_id":       job.OdobID,
		"odob_enid":     job.OdobEnID,
		"odob_alias_id": job.OdobAliasID,
		"download_type": job.DownloadType,
		"output_dir":    downloadRoot,
		"title":         title,
	}, nil
}

func executeDedaoOdobSyncKBaseJob(ctx context.Context, store *BookKnowledgeStore, job BookKnowledgeJob) (map[string]any, error) {
	result, err := SyncOdobToWikiStore(ctx, store, job)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"book_id":             result.KnowledgeBookID,
		"odob_id":             job.OdobID,
		"odob_enid":           job.OdobEnID,
		"odob_alias_id":       job.OdobAliasID,
		"download_type":       3,
		"knowledge_book_id":   result.KnowledgeBookID,
		"title":               result.Title,
		"chapters":            result.Chapters,
		"chunks":              result.Chunks,
		"claims":              result.Claims,
		"book_knowledge_root": result.BookKnowledgeRoot,
	}, nil
}

func executeDedaoEbookSyncKBaseJob(ctx context.Context, store *BookKnowledgeStore, job BookKnowledgeJob) (map[string]any, error) {
	result, err := SyncEbookToBookKnowledgeStore(ctx, job.EbookID, job.EbookEnID, store, DefaultDedaoDownloadRoot())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"book_id":             fmt.Sprintf("%d", job.EbookID),
		"ebook_id":            job.EbookID,
		"ebook_enid":          job.EbookEnID,
		"download_type":       1,
		"knowledge_book_id":   result.KnowledgeBookID,
		"title":               result.Title,
		"html_path":           result.HTMLPath,
		"repo_dir":            result.RepoDir,
		"book_knowledge_root": result.BookKnowledgeRoot,
	}, nil
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
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "job_" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return "job_" + time.Now().UTC().Format("20060102T150405.000000000Z") + "_" + hex.EncodeToString(randomBytes[:])
}
