package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
)

func TestBookJobWorkerRejectsInvalidConfiguration(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	valid := BookJobWorkerConfig{
		Store: store, WorkerID: "worker-1", LeaseDuration: time.Second,
		RenewInterval: 100 * time.Millisecond, PollInterval: time.Second,
		Execute: func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error) { return nil, nil },
	}
	for _, test := range []struct {
		name   string
		mutate func(*BookJobWorkerConfig)
	}{
		{name: "store", mutate: func(cfg *BookJobWorkerConfig) { cfg.Store = nil }},
		{name: "blank worker", mutate: func(cfg *BookJobWorkerConfig) { cfg.WorkerID = "  " }},
		{name: "unsafe worker", mutate: func(cfg *BookJobWorkerConfig) { cfg.WorkerID = "../worker" }},
		{name: "token-like worker", mutate: func(cfg *BookJobWorkerConfig) { cfg.WorkerID = "sk-secret-token-value" }},
		{name: "long worker", mutate: func(cfg *BookJobWorkerConfig) { cfg.WorkerID = strings.Repeat("w", 129) }},
		{name: "lease", mutate: func(cfg *BookJobWorkerConfig) { cfg.LeaseDuration = 0 }},
		{name: "renew", mutate: func(cfg *BookJobWorkerConfig) { cfg.RenewInterval = 0 }},
		{name: "renew equals lease", mutate: func(cfg *BookJobWorkerConfig) { cfg.RenewInterval = cfg.LeaseDuration }},
		{name: "poll", mutate: func(cfg *BookJobWorkerConfig) { cfg.PollInterval = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.mutate(&cfg)
			if _, err := NewBookJobWorker(cfg); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
	valid.WorkerID = " worker.safe_1:local "
	if _, err := NewBookJobWorker(valid); err != nil {
		t.Fatalf("safe worker id rejected: %v", err)
	}
}

func TestBookJobWorkerRunOnceClaimsOldestAndCompletesWithSafeStages(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldest := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 201)
	newer := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 202)
	db, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE book_jobs SET created_at = CASE job_id WHEN ? THEN ? WHEN ? THEN ? END WHERE job_id IN (?, ?)`,
		oldest.ID, "2000-01-01T00:00:00Z", newer.ID, "2001-01-01T00:00:00Z", oldest.ID, newer.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	worker := newWorkerForTest(t, store, func(_ context.Context, job BookKnowledgeJob, setStage func(string) error) (map[string]any, error) {
		if job.ID != oldest.ID {
			t.Fatalf("claimed %q, want oldest %q", job.ID, oldest.ID)
		}
		if err := setStage("downloading"); err != nil {
			return nil, err
		}
		return map[string]any{"ebook_id": job.EbookID, "title": "safe", "private_path": "/private/raw"}, nil
	})
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(oldest.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusSucceeded || loaded.Stage != "completed" || loaded.Result["title"] != "safe" {
		t.Fatalf("completed job=%#v err=%v", loaded, err)
	}
	if _, exists := loaded.Result["private_path"]; exists {
		t.Fatalf("unsafe result persisted: %#v", loaded.Result)
	}
	queued, err := store.LoadBookKnowledgeJob(newer.ID)
	if err != nil || queued.Status != BookKnowledgeJobStatusQueued {
		t.Fatalf("newer job=%#v err=%v", queued, err)
	}
	assertWorkerEventStages(t, store, oldest.ID, []string{"queued", "running", "downloading", "completed"})
}

func TestBookJobWorkerRunOnceReturnsFalseWithoutQueuedJob(t *testing.T) {
	worker := newWorkerForTest(t, NewBookKnowledgeStore(t.TempDir()), nil)
	processed, err := worker.RunOnce(context.Background())
	if err != nil || processed {
		t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
	}
}

func TestBookJobWorkerClassifiesFailuresWithoutPersistingRawErrors(t *testing.T) {
	codes := []string{
		BookKnowledgeJobFailureAuthenticationRequired,
		BookKnowledgeJobFailureDownloadFailed,
		BookKnowledgeJobFailureKnowledgeBuildFailed,
		BookKnowledgeJobFailureSourceChanged,
		BookKnowledgeJobFailureUnknownFailure,
	}
	for _, code := range codes {
		t.Run("typed_"+code, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 210)
			worker := newWorkerForTest(t, store, func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error) {
				return nil, NewBookJobExecutionFailure(code, errors.New("raw /private/secret token=abc"))
			})
			if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
				t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
			}
			assertWorkerFailedSafely(t, store, job.ID, code)
		})
	}
	for _, test := range []struct {
		name  string
		stage string
		code  string
	}{
		{name: "download", stage: "downloading", code: BookKnowledgeJobFailureDownloadFailed},
		{name: "knowledge", stage: "building_knowledge", code: BookKnowledgeJobFailureKnowledgeBuildFailed},
		{name: "unknown", stage: "", code: BookKnowledgeJobFailureUnknownFailure},
	} {
		t.Run("default_"+test.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 211)
			worker := newWorkerForTest(t, store, func(_ context.Context, _ BookKnowledgeJob, setStage func(string) error) (map[string]any, error) {
				if test.stage != "" {
					if err := setStage(test.stage); err != nil {
						return nil, err
					}
				}
				return nil, errors.New("provider failed at /private/secret token=abc")
			})
			if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
				t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
			}
			assertWorkerFailedSafely(t, store, job.ID, test.code)
		})
	}
}

func TestBookJobWorkerDefaultExecutionClassifiesRemoteFailures(t *testing.T) {
	oldRunner := runDedaoEbookDownloadJob
	defer func() { runDedaoEbookDownloadJob = oldRunner }()
	for _, test := range []struct {
		name string
		kind services.RemoteErrorKind
		code string
	}{
		{name: "authentication", kind: services.RemoteErrorAuthentication, code: BookKnowledgeJobFailureAuthenticationRequired},
		{name: "source", kind: services.RemoteErrorSourceChanged, code: BookKnowledgeJobFailureSourceChanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 212)
			runDedaoEbookDownloadJob = func(context.Context, BookKnowledgeJob) (map[string]any, error) {
				return nil, &services.RemoteError{Kind: test.kind, StatusCode: 401}
			}
			worker, err := NewBookJobWorker(BookJobWorkerConfig{
				Store: store, WorkerID: "worker-default", LeaseDuration: time.Second,
				RenewInterval: 100 * time.Millisecond, PollInterval: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
				t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
			}
			assertWorkerFailedSafely(t, store, job.ID, test.code)
		})
	}
}

func TestBookJobWorkerRecoversExecutorPanicAsSafeUnknownFailure(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 220)
	worker := newWorkerForTest(t, store, func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error) {
		panic("raw panic /private/secret token=abc")
	})
	if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
		t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
	}
	assertWorkerFailedSafely(t, store, job.ID, BookKnowledgeJobFailureUnknownFailure)
}

func TestBookJobWorkerRenewsLeaseAndStopsRenewingBeforeCompletion(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 230)
	started := make(chan struct{})
	release := make(chan struct{})
	worker := newWorkerWithDurationsForTest(t, store, 250*time.Millisecond, 25*time.Millisecond, 20*time.Millisecond,
		func(ctx context.Context, _ BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
			close(started)
			select {
			case <-release:
				return map[string]any{"ebook_id": 230}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
	done := make(chan error, 1)
	go func() { _, err := worker.RunOnce(context.Background()); done <- err }()
	<-started
	waitForWorkerEventMessage(t, store, job.ID, "lease renewed")
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	events := countBookKnowledgeJobEvents(t, store, job.ID)
	time.Sleep(75 * time.Millisecond)
	if got := countBookKnowledgeJobEvents(t, store, job.ID); got != events {
		t.Fatalf("renew loop continued after completion: events=%d want=%d", got, events)
	}
}

func TestBookJobWorkerLeaseLossCancelsExecutorWithoutTerminalTransition(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 240)
	started := make(chan struct{})
	canceled := make(chan struct{})
	worker := newWorkerWithDurationsForTest(t, store, time.Second, 20*time.Millisecond, 20*time.Millisecond,
		func(ctx context.Context, _ BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		})
	done := make(chan error, 1)
	go func() { _, err := worker.RunOnce(context.Background()); done <- err }()
	<-started
	db, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE book_jobs SET lease_owner = ? WHERE job_id = ?`, "stolen-worker", job.ID)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = <-done
	if err == nil || strings.Contains(err.Error(), job.ID) || strings.Contains(err.Error(), "stolen-worker") {
		t.Fatalf("unsafe or missing infrastructure error: %v", err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("executor did not observe cancellation")
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusRunning {
		t.Fatalf("lease-lost job=%#v err=%v", loaded, err)
	}
	if got := countWorkerTerminalEvents(t, store, job.ID); got != 0 {
		t.Fatalf("terminal events=%d want=0", got)
	}
}

func TestBookJobWorkerStageFailureIsInfrastructureError(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 245)
	worker := newWorkerForTest(t, store, func(ctx context.Context, _ BookKnowledgeJob, setStage func(string) error) (map[string]any, error) {
		_ = setStage("unsafe-stage")
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if processed, err := worker.RunOnce(context.Background()); !processed || err == nil || strings.Contains(err.Error(), "unsafe-stage") {
		t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
	}
	if got := countWorkerTerminalEvents(t, store, job.ID); got != 0 {
		t.Fatalf("terminal events=%d want=0", got)
	}
}

func TestBookJobWorkerCancellationInterruptsExactlyOnce(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 250)
	started := make(chan struct{})
	worker := newWorkerForTest(t, store, func(ctx context.Context, _ BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		processed bool
		err       error
	}, 1)
	go func() {
		processed, err := worker.RunOnce(ctx)
		done <- struct {
			processed bool
			err       error
		}{processed, err}
	}()
	<-started
	cancel()
	result := <-done
	if !result.processed || result.err != nil {
		t.Fatalf("RunOnce() processed=%t err=%v", result.processed, result.err)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusInterrupted || loaded.FailureCode != BookKnowledgeJobFailureWorkerInterrupted {
		t.Fatalf("interrupted job=%#v err=%v", loaded, err)
	}
	if got := countWorkerTerminalEvents(t, store, job.ID); got != 1 {
		t.Fatalf("terminal events=%d want=1", got)
	}
}

func TestBookJobWorkerExecutorContextCanceledWithoutWorkerCancellationUsesStageFailure(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 251)
	worker := newWorkerForTest(t, store, func(_ context.Context, _ BookKnowledgeJob, setStage func(string) error) (map[string]any, error) {
		if err := setStage("downloading"); err != nil {
			return nil, err
		}
		return nil, context.Canceled
	})
	if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
		t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
	}
	assertWorkerFailedSafely(t, store, job.ID, BookKnowledgeJobFailureDownloadFailed)
}

func TestBookJobWorkerTypedWorkerInterruptedUsesInterruptedStatus(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 252)
	worker := newWorkerForTest(t, store, func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error) {
		return nil, NewBookJobExecutionFailure(BookKnowledgeJobFailureWorkerInterrupted, errors.New("worker stopped"))
	})
	if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
		t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusInterrupted || loaded.FailureCode != BookKnowledgeJobFailureWorkerInterrupted {
		t.Fatalf("job=%#v err=%v", loaded, err)
	}
	if got := countWorkerTerminalEvents(t, store, job.ID); got != 1 {
		t.Fatalf("terminal events=%d want=1", got)
	}
}

func TestBookJobWorkerReconcilesExpiredBeforeClaim(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	expired := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 260)
	if _, err := store.ClaimNextBookKnowledgeJob("dead-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	setBookKnowledgeJobLeaseExpiry(t, store, expired.ID, time.Now().Add(-time.Second))
	queued := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 261)
	worker := newWorkerForTest(t, store, func(_ context.Context, job BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
		if job.ID != queued.ID {
			t.Fatalf("claimed=%q want=%q", job.ID, queued.ID)
		}
		return map[string]any{"ebook_id": job.EbookID}, nil
	})
	if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
		t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(expired.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("expired job=%#v err=%v", loaded, err)
	}
}

func TestBookJobWorkerRunPollsAndExitsCleanlyOnCancel(t *testing.T) {
	worker := newWorkerWithDurationsForTest(t, NewBookKnowledgeStore(t.TempDir()), time.Second, 100*time.Millisecond, 30*time.Millisecond, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if elapsed < 60*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("Run elapsed=%s, want polling wait without busy exit", elapsed)
	}
}

func TestBookJobWorkerRunOnceTreatsReconcileCancellationAsCleanStop(t *testing.T) {
	root := t.TempDir()
	holder := NewBookKnowledgeStore(root)
	rootLock, err := holder.acquireBookKnowledgeRootLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rootLock.Close()
	store := NewBookKnowledgeStore(root)
	attempted := make(chan struct{})
	store.beforePackageRootLock = func() { close(attempted) }
	worker := newWorkerForTest(t, store, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		processed bool
		err       error
	}, 1)
	go func() {
		processed, runErr := worker.RunOnce(ctx)
		done <- struct {
			processed bool
			err       error
		}{processed: processed, err: runErr}
	}()
	<-attempted
	cancel()
	select {
	case result := <-done:
		if result.processed || result.err != nil {
			t.Fatalf("RunOnce processed=%t err=%v, want clean canceled stop", result.processed, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunOnce did not stop after reconcile cancellation")
	}
}

func TestBookJobWorkerDefaultExecutorEmitsExpectedStages(t *testing.T) {
	previousDownload := runDedaoEbookDownloadJob
	previousSync := runDedaoEbookSyncKBaseJobWithStages
	defer func() {
		runDedaoEbookDownloadJob = previousDownload
		runDedaoEbookSyncKBaseJobWithStages = previousSync
	}()
	runDedaoEbookDownloadJob = func(context.Context, BookKnowledgeJob) (map[string]any, error) {
		return map[string]any{"ebook_id": 270}, nil
	}
	runDedaoEbookSyncKBaseJobWithStages = func(_ context.Context, _ *BookKnowledgeStore, _ BookKnowledgeJob, setStage func(string) error) (map[string]any, error) {
		if err := setStage("downloading"); err != nil {
			return nil, err
		}
		if err := setStage("building_knowledge"); err != nil {
			return nil, err
		}
		return map[string]any{"ebook_id": 271}, nil
	}
	for _, test := range []struct {
		jobType string
		ebookID int
		stages  []string
	}{
		{jobType: BookKnowledgeJobTypeDedaoEbookDownload, ebookID: 270, stages: []string{"queued", "running", "downloading", "completed"}},
		{jobType: BookKnowledgeJobTypeDedaoEbookSyncKBase, ebookID: 271, stages: []string{"queued", "running", "downloading", "building_knowledge", "completed"}},
	} {
		store := NewBookKnowledgeStore(t.TempDir())
		job := createWorkerTestJob(t, store, test.jobType, test.ebookID)
		worker := newWorkerForTest(t, store, nil)
		if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
			t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
		}
		assertWorkerEventStages(t, store, job.ID, test.stages)
	}
}

func TestBookJobWorkerSyncDownloadFailureStaysInDownloadingStage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 272)
	oldDownload := downloadEbookForKnowledgeSync
	defer func() { downloadEbookForKnowledgeSync = oldDownload }()
	stageDuringDownload := ""
	downloadEbookForKnowledgeSync = func(context.Context, int, string, string) (*EBookDownloadResult, error) {
		loaded, err := store.LoadBookKnowledgeJob(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		stageDuringDownload = loaded.Stage
		return nil, errors.New("raw download failure /private/token-secret")
	}
	worker := newWorkerForTest(t, store, nil)
	if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
		t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
	}
	if stageDuringDownload != "downloading" {
		t.Fatalf("stage during download=%q want=downloading", stageDuringDownload)
	}
	assertWorkerFailedSafely(t, store, job.ID, BookKnowledgeJobFailureDownloadFailed)
}

func TestBookJobWorkerSyncBuildFailureUsesKnowledgeFailureCode(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 273)
	downloadRoot := t.TempDir()
	htmlPath := filepath.Join(downloadRoot, "book.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><body><p>构建失败正文。</p></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldDownload := downloadEbookForKnowledgeSync
	defer func() { downloadEbookForKnowledgeSync = oldDownload }()
	downloadEbookForKnowledgeSync = func(context.Context, int, string, string) (*EBookDownloadResult, error) {
		return &EBookDownloadResult{BookID: job.EbookID, Title: "构建失败", HTMLPath: htmlPath}, nil
	}
	if err := os.WriteFile(filepath.Join(store.Root(), "books"), []byte("block knowledge directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker := newWorkerForTest(t, store, nil)
	if processed, err := worker.RunOnce(context.Background()); err != nil || !processed {
		t.Fatalf("RunOnce() processed=%t err=%v", processed, err)
	}
	assertWorkerFailedSafely(t, store, job.ID, BookKnowledgeJobFailureKnowledgeBuildFailed)
}

func createWorkerTestJob(t *testing.T, store *BookKnowledgeStore, jobType string, ebookID int) BookKnowledgeJob {
	t.Helper()
	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: jobType, EbookID: ebookID, EbookEnID: fmt.Sprintf("worker-%d", ebookID), DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func newWorkerForTest(t *testing.T, store *BookKnowledgeStore, execute func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error)) *BookJobWorker {
	t.Helper()
	return newWorkerWithDurationsForTest(t, store, time.Second, 100*time.Millisecond, 20*time.Millisecond, execute)
}

func newWorkerWithDurationsForTest(t *testing.T, store *BookKnowledgeStore, lease, renew, poll time.Duration, execute func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error)) *BookJobWorker {
	t.Helper()
	worker, err := NewBookJobWorker(BookJobWorkerConfig{
		Store: store, WorkerID: "test-worker", LeaseDuration: lease,
		RenewInterval: renew, PollInterval: poll, Execute: execute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func assertWorkerFailedSafely(t *testing.T, store *BookKnowledgeStore, jobID, code string) {
	t.Helper()
	job, err := store.LoadBookKnowledgeJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	wantMessage := bookKnowledgeJobFailureMessages[code]
	if job.Status != BookKnowledgeJobStatusFailed || job.FailureCode != code || job.Error != wantMessage {
		t.Fatalf("failed job=%#v", job)
	}
	serialized := fmt.Sprintf("%#v", job)
	if strings.Contains(serialized, "/private/secret") || strings.Contains(serialized, "token=abc") {
		t.Fatalf("raw failure persisted: %s", serialized)
	}
	db, err := store.openBookJobsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var message string
	if err := db.QueryRow(`SELECT message FROM book_job_events WHERE job_id = ? ORDER BY event_id DESC LIMIT 1`, jobID).Scan(&message); err != nil {
		t.Fatal(err)
	}
	if message != wantMessage {
		t.Fatalf("event message=%q want=%q", message, wantMessage)
	}
}

func assertWorkerEventStages(t *testing.T, store *BookKnowledgeStore, jobID string, want []string) {
	t.Helper()
	db, err := store.openBookJobsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT stage FROM book_job_events WHERE job_id = ? ORDER BY event_id`, jobID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var stage string
		if err := rows.Scan(&stage); err != nil {
			t.Fatal(err)
		}
		got = append(got, stage)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("stages=%v want=%v", got, want)
	}
}

func countWorkerTerminalEvents(t *testing.T, store *BookKnowledgeStore, jobID string) int {
	t.Helper()
	db, err := store.openBookJobsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM book_job_events WHERE job_id = ? AND status IN (?, ?, ?)`,
		jobID, BookKnowledgeJobStatusSucceeded, BookKnowledgeJobStatusFailed, BookKnowledgeJobStatusInterrupted).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func waitForWorkerEventMessage(t *testing.T, store *BookKnowledgeStore, jobID, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		db, err := store.openBookJobsDB()
		if err == nil {
			var count int
			err = db.QueryRow(`SELECT COUNT(*) FROM book_job_events WHERE job_id = ? AND message = ?`, jobID, want).Scan(&count)
			db.Close()
			if err == nil && count > 0 {
				return
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Fatal(err)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for event %q", want)
}

func TestBookJobWorkerRunDoesNotExecuteAfterCancellation(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 280)
	var calls atomic.Int32
	worker := newWorkerForTest(t, store, func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error) {
		calls.Add(1)
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("executor calls=%d want=0", calls.Load())
	}
}

func TestBookJobWorkerCancellationBetweenReconcileAndClaimLeavesJobQueued(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 281)
	var calls atomic.Int32
	worker := newWorkerForTest(t, store, func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error) {
		calls.Add(1)
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	reached := make(chan struct{})
	release := make(chan struct{})
	worker.beforeClaim = func() {
		close(reached)
		<-release
	}
	done := make(chan struct {
		processed bool
		err       error
	}, 1)
	go func() {
		processed, runErr := worker.RunOnce(ctx)
		done <- struct {
			processed bool
			err       error
		}{processed, runErr}
	}()
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("worker did not reach pre-claim barrier")
	}
	cancel()
	close(release)
	result := <-done
	if result.err != nil || result.processed {
		t.Fatalf("RunOnce() processed=%t err=%v", result.processed, result.err)
	}
	if calls.Load() != 0 {
		t.Fatalf("executor calls=%d want=0", calls.Load())
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusQueued {
		t.Fatalf("job=%#v err=%v", loaded, err)
	}
}

func TestBookJobWorkerSuccessfulCommitWinsConcurrentParentCancellation(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 282)
	ctx, cancel := context.WithCancel(context.Background())
	worker := newWorkerForTest(t, store, func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error) {
		cancel()
		return map[string]any{"ebook_id": 282, "title": "committed"}, nil
	})
	processed, err := worker.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("RunOnce processed=%t err=%v", processed, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusSucceeded || loaded.Result["title"] != "committed" {
		t.Fatalf("successful committed job=%#v err=%v", loaded, err)
	}
}

func TestBookJobWorkerCompletedPackageWinsConcurrentRenewFailureWhenLeaseStillOwned(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 283)
	published := make(chan struct{})
	worker := newWorkerWithDurationsForTest(t, store, time.Second, 10*time.Millisecond, time.Second,
		func(ctx context.Context, _ BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
			pkg := sampleBookKnowledgePackageForExport()
			pkg.Book.BookID = "worker-published-package"
			pkg.Book.Title = "Worker Published Package"
			if err := store.SavePackageContext(ctx, pkg); err != nil {
				return nil, err
			}
			close(published)
			<-ctx.Done()
			return map[string]any{"ebook_id": 283, "title": "committed"}, nil
		})
	worker.renewLease = func(BookKnowledgeJob) error {
		<-published
		return errors.New("injected renewal transport failure")
	}
	done := make(chan error, 1)
	go func() { _, err := worker.RunOnce(context.Background()); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOnce after committed package: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not finish after renewal canceled the executor")
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusSucceeded {
		t.Fatalf("completed job=%#v err=%v", loaded, err)
	}
	if _, err := store.LoadPackage("worker-published-package"); err != nil {
		t.Fatalf("published package missing after successful terminal transition: %v", err)
	}
}

func TestBookJobWorkerLeaseLossCancelsBlockingEbookGeneratorAndReturns(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookDownload, 284)
	started := make(chan struct{})
	outputDir := t.TempDir()
	worker := newWorkerWithDurationsForTest(t, store, time.Second, 10*time.Millisecond, time.Second,
		func(ctx context.Context, _ BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
			_, err := generateEbookFileAtomically(ctx, outputDir, "blocked", "html", func(ctx context.Context, _ string) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			})
			return nil, err
		})
	done := make(chan error, 1)
	go func() { _, err := worker.RunOnce(context.Background()); done <- err }()
	<-started
	db, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE book_jobs SET lease_owner = ? WHERE job_id = ?`, "replacement-worker", job.ID)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrBookJobWorkerInfrastructure) {
			t.Fatalf("RunOnce error=%v, want infrastructure error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not return after lease loss canceled blocking generator")
	}
	finalPath, err := ebookGeneratedFilePath(outputDir, "blocked", "html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
		t.Fatalf("blocked generator published final file: %v", err)
	}
}

func TestBookJobWorkerReconcileCompletesVerifiedPackageAfterLeaseLossPastPublishFence(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 285)
	store.afterPackagePublishGate = func() {
		db, err := store.openBookJobsWriteDB()
		if err != nil {
			t.Errorf("open jobs database: %v", err)
			return
		}
		defer db.Close()
		if _, err := db.Exec(`UPDATE book_jobs SET lease_owner = ?, lease_expires_at = ? WHERE job_id = ?`,
			"replacement-worker", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), job.ID); err != nil {
			t.Errorf("replace lease owner: %v", err)
		}
	}
	worker := newWorkerForTest(t, store, func(ctx context.Context, claimed BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
		pkg := sampleBookKnowledgePackageForExport()
		pkg.Book.BookID = "285"
		pkg.Book.DedaoID = claimed.EbookID
		pkg.Book.EnID = claimed.EbookEnID
		pkg.Book.Title = "Recovered Commit"
		pkg.Book.ContentHash = ""
		if err := store.SavePackageContext(ctx, pkg); err != nil {
			return nil, err
		}
		return map[string]any{
			"ebook_id": claimed.EbookID, "ebook_enid": claimed.EbookEnID,
			"download_type": 1, "knowledge_book_id": pkg.Book.BookID, "title": pkg.Book.Title,
		}, nil
	})
	processed, runErr := worker.RunOnce(context.Background())
	if !processed || !errors.Is(runErr, ErrBookJobWorkerInfrastructure) {
		t.Fatalf("RunOnce processed=%t err=%v, want completion CAS infrastructure failure", processed, runErr)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusRunning {
		t.Fatalf("job before reconcile=%#v err=%v", loaded, err)
	}
	markerInfo, err := os.Stat(bookKnowledgeJobCommitMarkerPath(store.BookDir("285")))
	if err != nil {
		t.Fatal(err)
	}
	if markerInfo.Mode().Perm() != 0o600 {
		t.Fatalf("commit marker mode=%v, want 0600", markerInfo.Mode().Perm())
	}
	count, err := store.ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	loaded, err = store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusSucceeded || loaded.Stage != "completed" {
		t.Fatalf("recovered job=%#v err=%v", loaded, err)
	}
	if len(loaded.Result) != 5 || loaded.Result["knowledge_book_id"] != "285" || loaded.Result["title"] != "Recovered Commit" {
		t.Fatalf("safe recovered result=%#v", loaded.Result)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
}

func TestReconcileCommitIntentWithoutFullyPublishedPackageInterruptsJob(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 286)
	claimed, err := store.ClaimNextBookKnowledgeJob("intent-worker", time.Second)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
	}
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "286"
	pkg.Book.DedaoID = job.EbookID
	pkg.Book.EnID = job.EbookEnID
	pkg.Book.ContentHash = ""
	hash, err := BookKnowledgeContentHash(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.fenceBookKnowledgeJobPackageCommit(
		job.ID, "intent-worker", pkg.Book.BookID, hash, bookJobWorkerCommitLeaseWindow,
	); err != nil {
		t.Fatal(err)
	}
	db, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE book_jobs SET lease_expires_at = ? WHERE job_id = ?`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), job.ID)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	count, err := store.ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("intent-only job=%#v err=%v", loaded, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
}

func TestReconcileCommitIntentCannotReuseSameHashPackageFromOlderPublish(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 291)
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "291"
	pkg.Book.DedaoID = job.EbookID
	pkg.Book.EnID = job.EbookEnID
	pkg.Book.ContentHash = ""
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	published, err := store.LoadPackage(pkg.Book.BookID)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextBookKnowledgeJob("same-hash-worker", time.Second)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
	}
	if _, _, err := store.fenceBookKnowledgeJobPackageCommit(
		job.ID, "same-hash-worker", published.Book.BookID, published.Book.ContentHash, bookJobWorkerCommitLeaseWindow,
	); err != nil {
		t.Fatal(err)
	}
	setBookKnowledgeJobLeaseExpiry(t, store, job.ID, time.Now().UTC().Add(-time.Minute))
	count, err := store.ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("same-hash intent-only job=%#v err=%v", loaded, err)
	}
}

func TestReconcileRejectsTamperedPackageCommitMarker(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 292)
	store.afterPackagePublishGate = func() {
		db, err := store.openBookJobsWriteDB()
		if err != nil {
			t.Errorf("open jobs database: %v", err)
			return
		}
		defer db.Close()
		_, err = db.Exec(`UPDATE book_jobs SET lease_owner = ?, lease_expires_at = ? WHERE job_id = ?`,
			"replacement-worker", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), job.ID)
		if err != nil {
			t.Errorf("replace lease owner: %v", err)
		}
	}
	worker := newWorkerForTest(t, store, func(ctx context.Context, claimed BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
		pkg := sampleBookKnowledgePackageForExport()
		pkg.Book.BookID = "292"
		pkg.Book.DedaoID = claimed.EbookID
		pkg.Book.EnID = claimed.EbookEnID
		pkg.Book.ContentHash = ""
		if err := store.SavePackageContext(ctx, pkg); err != nil {
			return nil, err
		}
		return map[string]any{"ebook_id": claimed.EbookID}, nil
	})
	processed, runErr := worker.RunOnce(context.Background())
	if !processed || !errors.Is(runErr, ErrBookJobWorkerInfrastructure) {
		t.Fatalf("RunOnce processed=%t err=%v", processed, runErr)
	}
	marker, err := readBookKnowledgeJobCommitMarker(store.BookDir("292"))
	if err != nil {
		t.Fatal(err)
	}
	marker.PublishNonce = strings.Repeat("0", len(marker.PublishNonce))
	if err := writeBookKnowledgeJobCommitMarker(store.BookDir("292"), marker); err != nil {
		t.Fatal(err)
	}
	count, err := store.ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("tampered marker job=%#v err=%v", loaded, err)
	}
}

func TestReconcileRejectsCommitReceiptWhenPublishedPackageVerificationFails(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *BookKnowledgeStore, BookKnowledgePackage)
	}{
		{
			name: "root manifest missing",
			mutate: func(t *testing.T, store *BookKnowledgeStore, _ BookKnowledgePackage) {
				t.Helper()
				if err := os.Remove(store.ManifestPath()); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "durable content hash mismatch",
			mutate: func(t *testing.T, store *BookKnowledgeStore, pkg BookKnowledgePackage) {
				t.Helper()
				pkg.Chunks[0].Text += " tampered"
				if err := writeJSONLFile(store.BookJSONLPath(pkg.Book.BookID, "chunks"), pkg.Chunks); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 289)
			claimed, err := store.ClaimNextBookKnowledgeJob("verification-worker", time.Second)
			if err != nil || claimed == nil || claimed.ID != job.ID {
				t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
			}
			pkg := sampleBookKnowledgePackageForExport()
			pkg.Book.BookID = "289"
			pkg.Book.DedaoID = job.EbookID
			pkg.Book.EnID = job.EbookEnID
			pkg.Book.ContentHash = ""
			if err := store.SavePackage(pkg); err != nil {
				t.Fatal(err)
			}
			published, err := store.LoadPackage(pkg.Book.BookID)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.fenceBookKnowledgeJobPackageCommit(
				job.ID, "verification-worker", published.Book.BookID, published.Book.ContentHash, bookJobWorkerCommitLeaseWindow,
			); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, store, *published)
			db, err := store.openBookJobsWriteDB()
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(`UPDATE book_jobs SET lease_expires_at = ? WHERE job_id = ?`,
				time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), job.ID)
			db.Close()
			if err != nil {
				t.Fatal(err)
			}
			count, err := store.ReconcileExpiredBookKnowledgeJobsContext(context.Background())
			if err != nil || count != 1 {
				t.Fatalf("reconcile count=%d err=%v", count, err)
			}
			loaded, err := store.LoadBookKnowledgeJob(job.ID)
			if err != nil || loaded.Status != BookKnowledgeJobStatusInterrupted {
				t.Fatalf("unverified job=%#v err=%v", loaded, err)
			}
		})
	}
}

func TestBookJobWorkerDiscardsCommitReceiptWhenPackagePublishRollsBack(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 287)
	store.afterPackageBookInstall = func() error { return errors.New("injected package publish failure") }
	worker := newWorkerForTest(t, store, func(ctx context.Context, claimed BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
		pkg := sampleBookKnowledgePackageForExport()
		pkg.Book.BookID = "287"
		pkg.Book.DedaoID = claimed.EbookID
		pkg.Book.EnID = claimed.EbookEnID
		pkg.Book.ContentHash = ""
		return nil, store.SavePackageContext(ctx, pkg)
	})
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("RunOnce processed=%t err=%v", processed, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusFailed {
		t.Fatalf("failed job=%#v err=%v", loaded, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
	assertNoBookKnowledgePublishResidue(t, root)
}

func TestBookJobWorkerCompletesFencedPackageAndDeletesReceipt(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 288)
	worker := newWorkerForTest(t, store, func(ctx context.Context, claimed BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
		pkg := sampleBookKnowledgePackageForExport()
		pkg.Book.BookID = "288"
		pkg.Book.DedaoID = claimed.EbookID
		pkg.Book.EnID = claimed.EbookEnID
		pkg.Book.Title = "Normal Commit"
		pkg.Book.ContentHash = ""
		if err := store.SavePackageContext(ctx, pkg); err != nil {
			return nil, err
		}
		return map[string]any{
			"ebook_id": claimed.EbookID, "ebook_enid": claimed.EbookEnID,
			"download_type": 1, "knowledge_book_id": pkg.Book.BookID, "title": pkg.Book.Title,
		}, nil
	})
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("RunOnce processed=%t err=%v", processed, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusSucceeded {
		t.Fatalf("completed job=%#v err=%v", loaded, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
	assertNoBookKnowledgePublishResidue(t, root)
}

func TestCommittedTransactionResidueSelfCleansAfterCleanupFailure(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 299)
	cleanupCalls := 0
	store.cleanupPackageTransaction = func(string) error {
		cleanupCalls++
		return errors.New("injected committed transaction cleanup failure")
	}
	worker := newWorkerForTest(t, store, func(ctx context.Context, claimed BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
		pkg := publishCrashTestPackage(claimed, "Committed With Cleanup Residue")
		if err := store.SavePackageContext(ctx, pkg); err != nil {
			return nil, err
		}
		return map[string]any{
			"ebook_id": claimed.EbookID, "ebook_enid": claimed.EbookEnID,
			"download_type": 1, "knowledge_book_id": pkg.Book.BookID, "title": pkg.Book.Title,
		}, nil
	})
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || cleanupCalls != 1 {
		t.Fatalf("RunOnce processed=%t cleanupCalls=%d err=%v", processed, cleanupCalls, err)
	}
	completed, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || completed.Status != BookKnowledgeJobStatusSucceeded {
		t.Fatalf("completed job=%#v err=%v", completed, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
	residue, err := filepath.Glob(filepath.Join(root, bookKnowledgePublishTransactionsDir, "*"))
	if err != nil || len(residue) != 1 {
		t.Fatalf("committed transaction residue=%v err=%v", residue, err)
	}

	store.cleanupPackageTransaction = nil
	committedPackage, err := NewBookKnowledgeStore(root).LoadPackage("299")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	manifest.Books[0].Title = "Mismatched Metadata With Same Hash"
	if err := writeJSONFile(store.ManifestPath(), manifest); err != nil {
		t.Fatal(err)
	}
	updated := publishCrashTestPackage(job, "Updated After Cleanup Residue")
	if err := NewBookKnowledgeStore(root).SavePackage(updated); err == nil ||
		!strings.Contains(err.Error(), "pending book publish transaction") {
		t.Fatalf("SavePackage with mismatched manifest error=%v, want pending transaction", err)
	}
	residue, err = filepath.Glob(filepath.Join(root, bookKnowledgePublishTransactionsDir, "*"))
	if err != nil || len(residue) != 1 {
		t.Fatalf("metadata mismatch removed transaction residue=%v err=%v", residue, err)
	}
	manifest.Books[0] = committedPackage.Book
	if err := writeJSONFile(store.ManifestPath(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := NewBookKnowledgeStore(root).SavePackage(updated); err != nil {
		t.Fatalf("SavePackage after committed residue: %v", err)
	}
	got, err := NewBookKnowledgeStore(root).LoadPackage("299")
	if err != nil || got.Book.Title != updated.Book.Title {
		t.Fatalf("updated package=%#v err=%v", got, err)
	}
	assertNoBookKnowledgePublishResidue(t, root)
}

func TestBookJobWorkerRetainsTransactionWhenPublishRollbackCannotRestoreBackup(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 300)
	original := publishCrashTestPackage(job, "Original Before Failed Rollback")
	if err := store.SavePackage(original); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store.afterPackageBookInstall = func() error {
		matches, err := filepath.Glob(filepath.Join(root, bookKnowledgePublishTransactionsDir, "*"))
		if err != nil || len(matches) != 1 {
			return fmt.Errorf("transaction matches=%v err=%v", matches, err)
		}
		if err := os.RemoveAll(filepath.Join(matches[0], "backup-book")); err != nil {
			return err
		}
		cancel()
		return errors.New("injected publish failure after deleting backup")
	}
	worker := newWorkerForTest(t, store, func(ctx context.Context, claimed BookKnowledgeJob, _ func(string) error) (map[string]any, error) {
		replacement := publishCrashTestPackage(claimed, "Replacement With Failed Rollback")
		return nil, store.SavePackageContext(ctx, replacement)
	})
	processed, runErr := worker.RunOnce(ctx)
	if !processed || !errors.Is(runErr, ErrBookJobWorkerInfrastructure) {
		t.Fatalf("RunOnce processed=%t err=%v, want recovery-required infrastructure error", processed, runErr)
	}
	recoveryJob, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || recoveryJob.Status != BookKnowledgeJobStatusRunning || recoveryJob.Stage != "recovery_required" {
		t.Fatalf("recovery-required job=%#v err=%v", recoveryJob, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 1)
	transactions, err := filepath.Glob(filepath.Join(root, bookKnowledgePublishTransactionsDir, "*"))
	if err != nil || len(transactions) != 1 {
		t.Fatalf("retained transaction=%v err=%v", transactions, err)
	}
	if _, err := os.Stat(filepath.Join(transactions[0], bookKnowledgePublishJournalFileName)); err != nil {
		t.Fatalf("retained journal: %v", err)
	}

	restoreStore := NewBookKnowledgeStore(t.TempDir())
	if err := restoreStore.SavePackage(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(restoreStore.BookDir(original.Book.BookID), filepath.Join(transactions[0], "backup-book")); err != nil {
		t.Fatal(err)
	}
	count, err := NewBookKnowledgeStore(root).ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	finalJob, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || finalJob.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("reconciled job=%#v err=%v", finalJob, err)
	}
	restored, err := NewBookKnowledgeStore(root).LoadPackage(original.Book.BookID)
	if err != nil || restored.Book.Title != original.Book.Title {
		t.Fatalf("restored package=%#v err=%v", restored, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
	assertNoBookKnowledgePublishResidue(t, root)
}

func TestBookKnowledgeJobRenewalRetainsLongerCommitFenceLease(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 290)
	claimed, err := store.ClaimNextBookKnowledgeJob("short-lease-worker", 2*time.Second)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
	}
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "290"
	pkg.Book.DedaoID = job.EbookID
	pkg.Book.EnID = job.EbookEnID
	hash, err := BookKnowledgeContentHash(pkg)
	if err != nil {
		t.Fatal(err)
	}
	fenced, _, err := store.fenceBookKnowledgeJobPackageCommit(
		job.ID, "short-lease-worker", pkg.Book.BookID, hash, bookJobWorkerCommitLeaseWindow,
	)
	if err != nil {
		t.Fatal(err)
	}
	fencedExpiry, err := time.Parse(time.RFC3339Nano, fenced.LeaseExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := store.RenewBookKnowledgeJobLease(job.ID, "short-lease-worker", 2*time.Second)
	if err != nil {
		t.Fatalf("short heartbeat after commit fence: %v", err)
	}
	renewedExpiry, err := time.Parse(time.RFC3339Nano, renewed.LeaseExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if renewedExpiry.Before(fencedExpiry) {
		t.Fatalf("heartbeat shortened fenced lease from %s to %s", fencedExpiry, renewedExpiry)
	}
}

func assertBookJobCommitReceiptCount(t *testing.T, store *BookKnowledgeStore, jobID string, want int) {
	t.Helper()
	db, err := store.openBookJobsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM book_job_commits WHERE job_id = ?`, jobID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("commit receipt count=%d want=%d", count, want)
	}
}
