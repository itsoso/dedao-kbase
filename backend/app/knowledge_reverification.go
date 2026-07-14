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

	"github.com/gofrs/flock"
)

const (
	knowledgeReverificationVersion     = "1"
	knowledgeReverificationLockWait    = 2 * time.Second
	knowledgeReverificationMaxAttempts = 5
	knowledgeReverificationBaseBackoff = 30 * time.Second
	knowledgeReverificationMaxBackoff  = 15 * time.Minute

	KnowledgeReverificationQueued         = "queued"
	KnowledgeReverificationRunning        = "running"
	KnowledgeReverificationCandidateReady = "candidate_ready"
	KnowledgeReverificationFailed         = "failed"
	KnowledgeReverificationPublished      = "published"

	KnowledgeReverificationErrorReleaseUnavailable = "release_unavailable"
	KnowledgeReverificationErrorPackageUnavailable = "package_unavailable"
	KnowledgeReverificationErrorAnalysisFailed     = "analysis_failed"
	KnowledgeReverificationErrorQualityFailed      = "quality_evaluation_failed"
	KnowledgeReverificationErrorRetryExhausted     = "retry_exhausted"
)

type KnowledgeReverificationTask struct {
	Version               string   `json:"version"`
	TaskID                string   `json:"task_id"`
	ReleaseID             string   `json:"release_id"`
	BookID                string   `json:"book_id"`
	TriggerOutcomes       []string `json:"trigger_outcomes"`
	AssessmentAt          string   `json:"assessment_at"`
	AssessmentFingerprint string   `json:"assessment_fingerprint"`
	Status                string   `json:"status"`
	Attempts              int      `json:"attempts"`
	AvailableAt           string   `json:"available_at"`
	ReleaseContentHash    string   `json:"release_content_hash,omitempty"`
	CandidateContentHash  string   `json:"candidate_content_hash,omitempty"`
	ContentChanged        bool     `json:"content_changed,omitempty"`
	CandidateAnalysisHash string   `json:"candidate_analysis_hash,omitempty"`
	QualityDecision       string   `json:"quality_decision,omitempty"`
	ErrorCode             string   `json:"error_code,omitempty"`
	CreatedAt             string   `json:"created_at"`
	UpdatedAt             string   `json:"updated_at"`
	StartedAt             string   `json:"started_at,omitempty"`
	CompletedAt           string   `json:"completed_at,omitempty"`
	PublishedReleaseID    string   `json:"published_release_id,omitempty"`
}

type KnowledgeReverificationCandidate struct {
	ReleaseContentHash   string
	CandidateContentHash string
	ContentChanged       bool
	AnalysisHash         string
	QualityDecision      string
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
		if ctx.Err() != nil {
			return r.requeueTask(task, 0)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return r.requeueTask(task, knowledgeReverificationRetryDelay(task.Attempts))
		}
		return r.failTask(task, KnowledgeReverificationErrorAnalysisFailed)
	}
	if ctx.Err() != nil {
		return r.requeueTask(task, 0)
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
		return r.requeueTask(task, knowledgeReverificationRetryDelay(task.Attempts))
	}
	completed, err := r.store.CompleteKnowledgeReverification(task.TaskID, task.AssessmentAt, task.AssessmentFingerprint, KnowledgeReverificationCandidate{
		ReleaseContentHash:   release.ContentHash,
		CandidateContentHash: pkgAfter.Book.ContentHash,
		ContentChanged:       release.ContentHash != pkgAfter.Book.ContentHash,
		AnalysisHash:         analysisHash,
		QualityDecision:      quality.Decision,
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
	failed, err := r.store.FailKnowledgeReverification(task.TaskID, task.AssessmentAt, task.AssessmentFingerprint, errorCode, r.now())
	if err != nil {
		return result, err
	}
	result.Status = failed.Status
	result.ErrorCode = failed.ErrorCode
	return result, nil
}

func (r *KnowledgeReverificationRunner) requeueTask(task *KnowledgeReverificationTask, delay time.Duration) (KnowledgeReverificationTickResult, error) {
	result := KnowledgeReverificationTickResult{Processed: true, TaskID: task.TaskID, ReleaseID: task.ReleaseID}
	requeued, err := r.store.RequeueKnowledgeReverification(task.TaskID, r.now(), delay)
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
	assessmentAt := strings.TrimSpace(assessment.ReverificationAt)
	assessmentFingerprint := strings.TrimSpace(assessment.ReverificationFingerprint)
	if !assessment.ReverifyRequired || assessmentAt == "" || assessmentFingerprint == "" {
		return nil, fmt.Errorf("reverification requires an invalidating feedback assessment")
	}
	release, err := s.LoadKnowledgeRelease(releaseID)
	if err != nil {
		return nil, err
	}
	now = now.UTC()

	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, err := s.listKnowledgeReverificationsUnlocked(release.ReleaseID)
	if err != nil {
		return nil, err
	}
	for index := len(tasks) - 1; index >= 0; index-- {
		if tasks[index].AssessmentFingerprint == assessmentFingerprint {
			return &tasks[index], nil
		}
	}
	for index := len(tasks) - 1; index >= 0; index-- {
		if !isActiveKnowledgeReverificationStatus(tasks[index].Status) {
			continue
		}
		task := tasks[index]
		task.AssessmentAt = assessmentAt
		task.AssessmentFingerprint = assessmentFingerprint
		task.TriggerOutcomes = append([]string(nil), assessment.TriggerOutcomes...)
		task.UpdatedAt = now.Format(time.RFC3339Nano)
		if err := s.saveKnowledgeReverificationUnlocked(task); err != nil {
			return nil, err
		}
		return &task, nil
	}

	taskID := knowledgeReverificationTaskID(release.ReleaseID, assessmentFingerprint)
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
		AssessmentAt:    assessmentAt, AssessmentFingerprint: assessmentFingerprint,
		Status:             KnowledgeReverificationQueued,
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
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, false, err
	}
	defer releaseQueueLock()
	s.mu.Lock()
	defer s.mu.Unlock()
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
			requeueKnowledgeReverificationTask(&tasks[index], now, knowledgeReverificationRetryDelay(tasks[index].Attempts))
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
	processedAssessmentFingerprint string,
	candidate KnowledgeReverificationCandidate,
	now time.Time,
) (*KnowledgeReverificationTask, error) {
	now = now.UTC()
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.loadKnowledgeReverificationUnlocked(taskID)
	if err != nil {
		return nil, err
	}
	if task.AssessmentAt != processedAssessmentAt || task.AssessmentFingerprint != processedAssessmentFingerprint {
		requeueKnowledgeReverificationTask(task, now, knowledgeReverificationRetryDelay(task.Attempts))
	} else {
		timestamp := now.Format(time.RFC3339Nano)
		task.Status = KnowledgeReverificationCandidateReady
		task.ReleaseContentHash = candidate.ReleaseContentHash
		task.CandidateContentHash = candidate.CandidateContentHash
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
	processedAssessmentFingerprint string,
	errorCode string,
	now time.Time,
) (*KnowledgeReverificationTask, error) {
	now = now.UTC()
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.loadKnowledgeReverificationUnlocked(taskID)
	if err != nil {
		return nil, err
	}
	if task.AssessmentAt != processedAssessmentAt || task.AssessmentFingerprint != processedAssessmentFingerprint {
		requeueKnowledgeReverificationTask(task, now, knowledgeReverificationRetryDelay(task.Attempts))
	} else {
		timestamp := now.Format(time.RFC3339Nano)
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

func (s *BookKnowledgeStore) RequeueKnowledgeReverification(taskID string, now time.Time, delay time.Duration) (*KnowledgeReverificationTask, error) {
	now = now.UTC()
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.loadKnowledgeReverificationUnlocked(taskID)
	if err != nil {
		return nil, err
	}
	if delay <= 0 && task.Attempts > 0 {
		task.Attempts--
	}
	requeueKnowledgeReverificationTask(task, now, delay)
	if err := s.saveKnowledgeReverificationUnlocked(*task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *BookKnowledgeStore) RetryKnowledgeReverification(releaseID string, now time.Time) (*KnowledgeReverificationTask, error) {
	releaseQueueLock, err := s.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()

	assessment, err := s.AssessKnowledgeFeedback(releaseID)
	if err != nil {
		return nil, err
	}
	if !assessment.ReverifyRequired || strings.TrimSpace(assessment.ReverificationFingerprint) == "" {
		return nil, fmt.Errorf("reverification retry requires invalidating feedback")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, err := s.listKnowledgeReverificationsUnlocked(releaseID)
	if err != nil {
		return nil, err
	}
	var task *KnowledgeReverificationTask
	for index := len(tasks) - 1; index >= 0; index-- {
		if tasks[index].AssessmentFingerprint == assessment.ReverificationFingerprint {
			copy := tasks[index]
			task = &copy
			break
		}
	}
	if task == nil {
		if len(tasks) > 0 {
			return nil, fmt.Errorf("reverification retry task was superseded by newer feedback")
		}
		return nil, fmt.Errorf("reverification retry task not found")
	}
	if task.Status != KnowledgeReverificationFailed {
		return nil, fmt.Errorf("reverification retry requires failed task, got %q", task.Status)
	}
	task.Attempts = 0
	requeueKnowledgeReverificationTask(task, now.UTC(), 0)
	if err := s.saveKnowledgeReverificationUnlocked(*task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *BookKnowledgeStore) ValidateKnowledgeReverificationPublication(bookID, analysisHash string) (*KnowledgeReverificationTask, error) {
	manifest, err := s.loadKnowledgeReleaseManifest()
	if err != nil {
		return nil, err
	}
	latestAssessmentAt := ""
	latestAssessmentFingerprint := ""
	latestReleaseID := ""
	for _, record := range manifest.Releases {
		if record.BookID != bookID {
			continue
		}
		assessment, err := s.AssessKnowledgeFeedback(record.ReleaseID)
		if err != nil {
			return nil, err
		}
		if assessment.ReverifyRequired && (knowledgeFeedbackTimestampAfter(assessment.ReverificationAt, latestAssessmentAt) ||
			(assessment.ReverificationAt == latestAssessmentAt && record.ReleaseID > latestReleaseID)) {
			latestAssessmentAt = assessment.ReverificationAt
			latestAssessmentFingerprint = assessment.ReverificationFingerprint
			latestReleaseID = record.ReleaseID
		}
	}
	if latestAssessmentAt == "" {
		return nil, nil
	}
	tasks, err := s.ListKnowledgeReverifications("")
	if err != nil {
		return nil, err
	}
	var latest *KnowledgeReverificationTask
	for index := range tasks {
		if tasks[index].BookID == bookID && tasks[index].ReleaseID == latestReleaseID &&
			tasks[index].AssessmentAt == latestAssessmentAt && tasks[index].AssessmentFingerprint == latestAssessmentFingerprint {
			copy := tasks[index]
			latest = &copy
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("knowledge release blocked by missing reverification task")
	}
	if latest.Status == KnowledgeReverificationPublished {
		return nil, nil
	}
	if latest.Status != KnowledgeReverificationCandidateReady {
		return nil, fmt.Errorf("knowledge release blocked by reverification status %q", latest.Status)
	}
	if strings.TrimSpace(latest.CandidateAnalysisHash) == "" || latest.CandidateAnalysisHash != analysisHash {
		return nil, fmt.Errorf("knowledge release candidate is stale against reverification")
	}
	return latest, nil
}

func (s *BookKnowledgeStore) acquireKnowledgeReverificationFileLock() (func(), error) {
	if err := os.MkdirAll(s.KnowledgeReverificationDir(), os.ModePerm); err != nil {
		return nil, err
	}
	fileLock := flock.New(filepath.Join(s.KnowledgeReverificationDir(), ".queue.lock"))
	ctx, cancel := context.WithTimeout(context.Background(), knowledgeReverificationLockWait)
	locked, err := fileLock.TryLockContext(ctx, 10*time.Millisecond)
	cancel()
	if err != nil || !locked {
		_ = fileLock.Close()
		if err == nil {
			err = fmt.Errorf("timed out acquiring reverification queue lock")
		}
		return nil, err
	}
	return func() { _ = fileLock.Close() }, nil
}

func requeueKnowledgeReverificationTask(task *KnowledgeReverificationTask, now time.Time, delay time.Duration) {
	timestamp := now.UTC().Format(time.RFC3339Nano)
	if delay > 0 && task.Attempts >= knowledgeReverificationMaxAttempts {
		task.Status = KnowledgeReverificationFailed
		task.ErrorCode = KnowledgeReverificationErrorRetryExhausted
		task.CompletedAt = timestamp
		task.UpdatedAt = timestamp
		return
	}
	task.Status = KnowledgeReverificationQueued
	task.AvailableAt = now.UTC().Add(delay).Format(time.RFC3339Nano)
	task.StartedAt = ""
	task.CompletedAt = ""
	task.CandidateContentHash = ""
	task.ContentChanged = false
	task.CandidateAnalysisHash = ""
	task.QualityDecision = ""
	task.ErrorCode = ""
	task.PublishedReleaseID = ""
	task.UpdatedAt = timestamp
}

func knowledgeReverificationRetryDelay(attempts int) time.Duration {
	delay := knowledgeReverificationBaseBackoff
	for index := 1; index < attempts && delay < knowledgeReverificationMaxBackoff; index++ {
		delay *= 2
	}
	if delay > knowledgeReverificationMaxBackoff {
		return knowledgeReverificationMaxBackoff
	}
	return delay
}

func (s *BookKnowledgeStore) markKnowledgeReverificationPublished(taskID, releaseID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.loadKnowledgeReverificationUnlocked(taskID)
	if err != nil {
		return err
	}
	task.Status = KnowledgeReverificationPublished
	task.PublishedReleaseID = strings.TrimSpace(releaseID)
	task.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	return s.saveKnowledgeReverificationUnlocked(*task)
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

func knowledgeReverificationTaskID(releaseID, assessmentFingerprint string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(releaseID) + "\x00" + strings.TrimSpace(assessmentFingerprint)))
	return "reverify-" + hex.EncodeToString(sum[:])
}

func isActiveKnowledgeReverificationStatus(status string) bool {
	return status == KnowledgeReverificationQueued || status == KnowledgeReverificationRunning
}
