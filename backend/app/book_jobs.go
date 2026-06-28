package app

import (
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
)

const bookKnowledgeJobsFileName = "jobs.json"

type BookKnowledgeJobStatus string

const (
	BookKnowledgeJobStatusQueued    BookKnowledgeJobStatus = "queued"
	BookKnowledgeJobStatusRunning   BookKnowledgeJobStatus = "running"
	BookKnowledgeJobStatusSucceeded BookKnowledgeJobStatus = "succeeded"
	BookKnowledgeJobStatusFailed    BookKnowledgeJobStatus = "failed"
)

const (
	BookKnowledgeJobTypeNotebookLMExport = "notebooklm_export"
	BookKnowledgeJobTypeBookExport       = "book_export"
)

type BookKnowledgeJob struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Status     BookKnowledgeJobStatus `json:"status"`
	BookID     string                 `json:"book_id,omitempty"`
	Target     string                 `json:"target,omitempty"`
	Result     map[string]any         `json:"result,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Logs       []string               `json:"logs,omitempty"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
	StartedAt  string                 `json:"started_at,omitempty"`
	FinishedAt string                 `json:"finished_at,omitempty"`
}

type BookKnowledgeJobRequest struct {
	Type   string `json:"type"`
	BookID string `json:"book_id,omitempty"`
	Target string `json:"target,omitempty"`
}

type bookKnowledgeJobsFile struct {
	Jobs []BookKnowledgeJob `json:"jobs"`
}

var bookKnowledgeJobsMu sync.Mutex

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
		ID:        newBookKnowledgeJobID(),
		Type:      normalized.Type,
		Status:    BookKnowledgeJobStatusQueued,
		BookID:    normalized.BookID,
		Target:    normalized.Target,
		Logs:      []string{"queued"},
		CreatedAt: now,
		UpdatedAt: now,
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
	default:
		return request, fmt.Errorf("unsupported job type: %s", request.Type)
	}
	return request, nil
}

func newBookKnowledgeJobID() string {
	var randomBytes [6]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "job_" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return "job_" + time.Now().UTC().Format("20060102T150405.000000000Z") + "_" + hex.EncodeToString(randomBytes[:])
}
