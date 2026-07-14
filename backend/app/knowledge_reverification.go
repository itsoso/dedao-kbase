package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	knowledgeReverificationVersion        = "1"
	knowledgeReverificationLockWait       = 2 * time.Second
	knowledgeReverificationLockStaleAfter = 30 * time.Second

	KnowledgeReverificationQueued         = "queued"
	KnowledgeReverificationRunning        = "running"
	KnowledgeReverificationCandidateReady = "candidate_ready"
	KnowledgeReverificationFailed         = "failed"

	KnowledgeReverificationErrorReleaseUnavailable = "release_unavailable"
	KnowledgeReverificationErrorPackageUnavailable = "package_unavailable"
	KnowledgeReverificationErrorAnalysisFailed     = "analysis_failed"
	KnowledgeReverificationErrorQualityFailed      = "quality_evaluation_failed"
)

type KnowledgeReverificationTask struct {
	Version               string   `json:"version"`
	TaskID                string   `json:"task_id"`
	ReleaseID             string   `json:"release_id"`
	BookID                string   `json:"book_id"`
	TriggerOutcomes       []string `json:"trigger_outcomes"`
	AssessmentAt          string   `json:"assessment_at"`
	Status                string   `json:"status"`
	Attempts              int      `json:"attempts"`
	AvailableAt           string   `json:"available_at"`
	ReleaseContentHash    string   `json:"release_content_hash,omitempty"`
	CurrentContentHash    string   `json:"current_content_hash,omitempty"`
	ContentChanged        bool     `json:"content_changed,omitempty"`
	CandidateAnalysisHash string   `json:"candidate_analysis_hash,omitempty"`
	QualityDecision       string   `json:"quality_decision,omitempty"`
	ErrorCode             string   `json:"error_code,omitempty"`
	CreatedAt             string   `json:"created_at"`
	UpdatedAt             string   `json:"updated_at"`
	StartedAt             string   `json:"started_at,omitempty"`
	CompletedAt           string   `json:"completed_at,omitempty"`
}

type KnowledgeReverificationCandidate struct {
	ReleaseContentHash string
	CurrentContentHash string
	ContentChanged     bool
	AnalysisHash       string
	QualityDecision    string
}

type KnowledgeReverificationTickResult struct {
	Processed bool   `json:"processed"`
	TaskID    string `json:"task_id,omitempty"`
	ReleaseID string `json:"release_id,omitempty"`
	Status    string `json:"status,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

type KnowledgeReverificationRunner struct {
	store             *BookKnowledgeStore
	analysisGenerator BookAnalysisGenerator
	now               func() time.Time
	staleAfter        time.Duration
}

func NewKnowledgeReverificationRunner(
	store *BookKnowledgeStore,
	analysisGenerator BookAnalysisGenerator,
	now func() time.Time,
	staleAfter time.Duration,
) *KnowledgeReverificationRunner {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if analysisGenerator == nil {
		analysisGenerator = GenerateBookAnalysisManifest
	}
	if now == nil {
		now = time.Now
	}
	if staleAfter <= 0 {
		staleAfter = 15 * time.Minute
	}
	return &KnowledgeReverificationRunner{
		store: store, analysisGenerator: analysisGenerator, now: now, staleAfter: staleAfter,
	}
}

func (r *KnowledgeReverificationRunner) Tick(ctx context.Context) (KnowledgeReverificationTickResult, error) {
	var result KnowledgeReverificationTickResult
	task, found, err := r.store.ClaimNextKnowledgeReverification(r.now(), r.staleAfter)
	if err != nil || !found {
		return result, err
	}
	result.Processed = true
	result.TaskID = task.TaskID
	result.ReleaseID = task.ReleaseID

	release, err := r.store.LoadKnowledgeRelease(task.ReleaseID)
	if err != nil {
		return r.failTask(task, KnowledgeReverificationErrorReleaseUnavailable)
	}
	pkgBefore, err := r.store.LoadPackage(task.BookID)
	if err != nil {
		return r.failTask(task, KnowledgeReverificationErrorPackageUnavailable)
	}
	manifest, err := r.analysisGenerator(ctx, r.store, BookAnalysisGenerateRequest{BookID: task.BookID})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return r.requeueTask(task)
		}
		return r.failTask(task, KnowledgeReverificationErrorAnalysisFailed)
	}
	if ctx.Err() != nil {
		return r.requeueTask(task)
	}
	quality, err := EvaluateBookAnalysisQuality(r.store, task.BookID)
	if err != nil {
		return r.failTask(task, KnowledgeReverificationErrorQualityFailed)
	}
	analysisHash := quality.AnalysisHash
	if strings.TrimSpace(analysisHash) == "" && manifest != nil {
		analysisHash, err = bookAnalysisHash(*manifest)
		if err != nil {
			return r.failTask(task, KnowledgeReverificationErrorQualityFailed)
		}
	}
	pkgAfter, err := r.store.LoadPackage(task.BookID)
	if err != nil {
		return r.failTask(task, KnowledgeReverificationErrorPackageUnavailable)
	}
	if pkgBefore.Book.ContentHash != pkgAfter.Book.ContentHash || quality.ContentHash != pkgAfter.Book.ContentHash {
		return r.requeueTask(task)
	}
	completed, err := r.store.CompleteKnowledgeReverification(task.TaskID, task.AssessmentAt, KnowledgeReverificationCandidate{
		ReleaseContentHash: release.ContentHash,
		CurrentContentHash: pkgAfter.Book.ContentHash,
		ContentChanged:     release.ContentHash != pkgAfter.Book.ContentHash,
		AnalysisHash:       analysisHash,
		QualityDecision:    quality.Decision,
	}, r.now())
	if err != nil {
		return result, err
	}
	result.Status = completed.Status
	return result, nil
}

func (r *KnowledgeReverificationRunner) Run(
	ctx context.Context,
	interval time.Duration,
	onTick func(KnowledgeReverificationTickResult, error),
) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	runTick := func() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		result, err := r.Tick(ctx)
		if onTick != nil {
			onTick(result, err)
		}
	}
	runTick()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runTick()
		}
	}
}

func (r *KnowledgeReverificationRunner) failTask(task *KnowledgeReverificationTask, errorCode string) (KnowledgeReverificationTickResult, error) {
	result := KnowledgeReverificationTickResult{
		Processed: true, TaskID: task.TaskID, ReleaseID: task.ReleaseID,
	}
	failed, err := r.store.FailKnowledgeReverification(task.TaskID, task.AssessmentAt, errorCode, r.now())
	if err != nil {
		return result, err
	}
	result.Status = failed.Status
	result.ErrorCode = failed.ErrorCode
	return result, nil
}

func (r *KnowledgeReverificationRunner) requeueTask(task *KnowledgeReverificationTask) (KnowledgeReverificationTickResult, error) {
	result := KnowledgeReverificationTickResult{Processed: true, TaskID: task.TaskID, ReleaseID: task.ReleaseID}
	requeued, err := r.store.RequeueKnowledgeReverification(task.TaskID, r.now())
	if err != nil {
		return result, err
	}
	result.Status = requeued.Status
	return result, nil
}

func (s *BookKnowledgeStore) KnowledgeReverificationDir() string {
	return filepath.Join(s.KnowledgeReleaseDir(), "reverification")
}

func (s *BookKnowledgeStore) KnowledgeReverificationPath(taskID string) string {
	return filepath.Join(s.KnowledgeReverificationDir(), sanitizeBookKnowledgeID(taskID)+".json")
}

func (s *BookKnowledgeStore) EnqueueKnowledgeReverification(
	releaseID string,
	assessment KnowledgeFeedbackAssessment,
	now time.Time,
	cooldown time.Duration,
) (*KnowledgeReverificationTask, error) {
	if !assessment.ReverifyRequired || strings.TrimSpace(assessment.LatestFeedbackAt) == "" {
		return nil, fmt.Errorf("reverification requires an invalidating feedback assessment")
	}
	release, err := s.LoadKnowledgeRelease(releaseID)
	if err != nil {
		return nil, err
	}
	now = now.UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()
	tasks, err := s.listKnowledgeReverificationsUnlocked(release.ReleaseID)
	if err != nil {
		return nil, err
	}
	for index := len(tasks) - 1; index >= 0; index-- {
		if !isActiveKnowledgeReverificationStatus(tasks[index].Status) {
			continue
		}
		task := tasks[index]
		task.AssessmentAt = assessment.LatestFeedbackAt
		task.TriggerOutcomes = append([]string(nil), assessment.TriggerOutcomes...)
		task.UpdatedAt = now.Format(time.RFC3339Nano)
		if err := s.saveKnowledgeReverificationUnlocked(task); err != nil {
			return nil, err
		}
		return &task, nil
	}

	taskID := knowledgeReverificationTaskID(release.ReleaseID, assessment.LatestFeedbackAt)
	if existing, err := s.loadKnowledgeReverificationUnlocked(taskID); err == nil {
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	availableAt := now
	if cooldown > 0 {
		for index := len(tasks) - 1; index >= 0; index-- {
			completedAt, parseErr := time.Parse(time.RFC3339Nano, tasks[index].CompletedAt)
			if parseErr != nil {
				continue
			}
			candidate := completedAt.Add(cooldown)
			if candidate.After(availableAt) {
				availableAt = candidate
			}
			break
		}
	}
	timestamp := now.Format(time.RFC3339Nano)
	task := KnowledgeReverificationTask{
		Version: knowledgeReverificationVersion, TaskID: taskID,
		ReleaseID: release.ReleaseID, BookID: release.BookID,
		TriggerOutcomes: append([]string(nil), assessment.TriggerOutcomes...),
		AssessmentAt:    assessment.LatestFeedbackAt, Status: KnowledgeReverificationQueued,
		AvailableAt:        availableAt.Format(time.RFC3339Nano),
		ReleaseContentHash: release.ContentHash,
		CreatedAt:          timestamp, UpdatedAt: timestamp,
	}
	if err := s.saveKnowledgeReverificationUnlocked(task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *BookKnowledgeStore) ListKnowledgeReverifications(releaseID string) ([]KnowledgeReverificationTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listKnowledgeReverificationsUnlocked(releaseID)
}

func (s *BookKnowledgeStore) ClaimNextKnowledgeReverification(now time.Time, staleAfter time.Duration) (*KnowledgeReverificationTask, bool, error) {
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, false, err
	}
	defer releaseQueueLock()
	tasks, err := s.listKnowledgeReverificationsUnlocked("")
	if err != nil {
		return nil, false, err
	}
	for index := range tasks {
		if tasks[index].Status != KnowledgeReverificationRunning || staleAfter <= 0 {
			continue
		}
		startedAt, parseErr := time.Parse(time.RFC3339Nano, tasks[index].StartedAt)
		if parseErr == nil && !startedAt.Add(staleAfter).After(now) {
			tasks[index].Status = KnowledgeReverificationQueued
			tasks[index].AvailableAt = now.Format(time.RFC3339Nano)
			tasks[index].StartedAt = ""
			tasks[index].UpdatedAt = now.Format(time.RFC3339Nano)
			if err := s.saveKnowledgeReverificationUnlocked(tasks[index]); err != nil {
				return nil, false, err
			}
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].AvailableAt != tasks[j].AvailableAt {
			return tasks[i].AvailableAt < tasks[j].AvailableAt
		}
		return tasks[i].CreatedAt < tasks[j].CreatedAt
	})
	for _, task := range tasks {
		if task.Status != KnowledgeReverificationQueued {
			continue
		}
		availableAt, parseErr := time.Parse(time.RFC3339Nano, task.AvailableAt)
		if parseErr != nil || availableAt.After(now) {
			continue
		}
		task.Status = KnowledgeReverificationRunning
		task.Attempts++
		task.StartedAt = now.Format(time.RFC3339Nano)
		task.UpdatedAt = task.StartedAt
		if err := s.saveKnowledgeReverificationUnlocked(task); err != nil {
			return nil, false, err
		}
		return &task, true, nil
	}
	return nil, false, nil
}

func (s *BookKnowledgeStore) CompleteKnowledgeReverification(
	taskID string,
	processedAssessmentAt string,
	candidate KnowledgeReverificationCandidate,
	now time.Time,
) (*KnowledgeReverificationTask, error) {
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()
	task, err := s.loadKnowledgeReverificationUnlocked(taskID)
	if err != nil {
		return nil, err
	}
	timestamp := now.Format(time.RFC3339Nano)
	if task.AssessmentAt != processedAssessmentAt {
		task.Status = KnowledgeReverificationQueued
		task.AvailableAt = timestamp
		task.StartedAt = ""
		task.UpdatedAt = timestamp
	} else {
		task.Status = KnowledgeReverificationCandidateReady
		task.ReleaseContentHash = candidate.ReleaseContentHash
		task.CurrentContentHash = candidate.CurrentContentHash
		task.ContentChanged = candidate.ContentChanged
		task.CandidateAnalysisHash = candidate.AnalysisHash
		task.QualityDecision = candidate.QualityDecision
		task.ErrorCode = ""
		task.UpdatedAt = timestamp
		task.CompletedAt = timestamp
	}
	if err := s.saveKnowledgeReverificationUnlocked(*task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *BookKnowledgeStore) FailKnowledgeReverification(
	taskID string,
	processedAssessmentAt string,
	errorCode string,
	now time.Time,
) (*KnowledgeReverificationTask, error) {
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()
	task, err := s.loadKnowledgeReverificationUnlocked(taskID)
	if err != nil {
		return nil, err
	}
	timestamp := now.Format(time.RFC3339Nano)
	if task.AssessmentAt != processedAssessmentAt {
		task.Status = KnowledgeReverificationQueued
		task.AvailableAt = timestamp
		task.StartedAt = ""
		task.ErrorCode = ""
		task.UpdatedAt = timestamp
	} else {
		task.Status = KnowledgeReverificationFailed
		task.ErrorCode = strings.TrimSpace(errorCode)
		task.UpdatedAt = timestamp
		task.CompletedAt = timestamp
	}
	if err := s.saveKnowledgeReverificationUnlocked(*task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *BookKnowledgeStore) RequeueKnowledgeReverification(taskID string, now time.Time) (*KnowledgeReverificationTask, error) {
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()
	task, err := s.loadKnowledgeReverificationUnlocked(taskID)
	if err != nil {
		return nil, err
	}
	timestamp := now.Format(time.RFC3339Nano)
	task.Status = KnowledgeReverificationQueued
	task.AvailableAt = timestamp
	task.StartedAt = ""
	task.CompletedAt = ""
	task.CandidateAnalysisHash = ""
	task.QualityDecision = ""
	task.ErrorCode = ""
	task.UpdatedAt = timestamp
	if err := s.saveKnowledgeReverificationUnlocked(*task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *BookKnowledgeStore) ValidateKnowledgeReverificationPublication(bookID, analysisHash string) error {
	tasks, err := s.ListKnowledgeReverifications("")
	if err != nil {
		return err
	}
	var latest *KnowledgeReverificationTask
	for index := range tasks {
		if tasks[index].BookID == bookID {
			copy := tasks[index]
			latest = &copy
		}
	}
	if latest == nil {
		return nil
	}
	if latest.Status != KnowledgeReverificationCandidateReady {
		return fmt.Errorf("knowledge release blocked by reverification status %q", latest.Status)
	}
	if strings.TrimSpace(latest.CandidateAnalysisHash) == "" || latest.CandidateAnalysisHash != analysisHash {
		return fmt.Errorf("knowledge release candidate is stale against reverification")
	}
	return nil
}

func (s *BookKnowledgeStore) acquireKnowledgeReverificationFileLock() (func(), error) {
	if err := os.MkdirAll(s.KnowledgeReverificationDir(), os.ModePerm); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(s.KnowledgeReverificationDir(), ".queue.lock")
	ownerPath := filepath.Join(lockPath, "owner")
	owner := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	deadline := time.Now().Add(knowledgeReverificationLockWait)
	for {
		if err := os.Mkdir(lockPath, 0o700); err == nil {
			if err := os.WriteFile(ownerPath, []byte(owner), 0o600); err != nil {
				_ = os.RemoveAll(lockPath)
				return nil, err
			}
			return func() {
				current, err := os.ReadFile(ownerPath)
				if err == nil && string(current) == owner {
					_ = os.RemoveAll(lockPath)
				}
			}, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, err := os.Stat(lockPath); err == nil && time.Since(info.ModTime()) > knowledgeReverificationLockStaleAfter {
			_ = os.RemoveAll(lockPath)
			continue
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("timed out acquiring reverification queue lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *BookKnowledgeStore) listKnowledgeReverificationsUnlocked(releaseID string) ([]KnowledgeReverificationTask, error) {
	entries, err := os.ReadDir(s.KnowledgeReverificationDir())
	if errors.Is(err, os.ErrNotExist) {
		return []KnowledgeReverificationTask{}, nil
	}
	if err != nil {
		return nil, err
	}
	tasks := make([]KnowledgeReverificationTask, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var task KnowledgeReverificationTask
		if err := readJSONFile(filepath.Join(s.KnowledgeReverificationDir(), entry.Name()), &task); err != nil {
			return nil, err
		}
		if strings.TrimSpace(releaseID) == "" || task.ReleaseID == releaseID {
			tasks = append(tasks, task)
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt != tasks[j].CreatedAt {
			return tasks[i].CreatedAt < tasks[j].CreatedAt
		}
		return tasks[i].TaskID < tasks[j].TaskID
	})
	return tasks, nil
}

func (s *BookKnowledgeStore) loadKnowledgeReverificationUnlocked(taskID string) (*KnowledgeReverificationTask, error) {
	var task KnowledgeReverificationTask
	if err := readJSONFile(s.KnowledgeReverificationPath(taskID), &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *BookKnowledgeStore) saveKnowledgeReverificationUnlocked(task KnowledgeReverificationTask) error {
	if strings.TrimSpace(task.TaskID) == "" || strings.TrimSpace(task.ReleaseID) == "" || strings.TrimSpace(task.BookID) == "" {
		return fmt.Errorf("reverification task requires task_id, release_id, and book_id")
	}
	payload, err := encodeJSONFile(task)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.KnowledgeReverificationDir(), os.ModePerm); err != nil {
		return err
	}
	return writeFileAtomically(s.KnowledgeReverificationPath(task.TaskID), payload)
}

func knowledgeReverificationTaskID(releaseID, assessmentAt string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(releaseID) + "\x00" + strings.TrimSpace(assessmentAt)))
	return "reverify-" + hex.EncodeToString(sum[:])
}

func isActiveKnowledgeReverificationStatus(status string) bool {
	return status == KnowledgeReverificationQueued || status == KnowledgeReverificationRunning
}
