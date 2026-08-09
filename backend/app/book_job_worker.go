package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ErrBookJobWorkerInfrastructure = errors.New("book job worker infrastructure failure")

var bookJobWorkerIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

type BookJobWorkerConfig struct {
	Store         *BookKnowledgeStore
	WorkerID      string
	LeaseDuration time.Duration
	RenewInterval time.Duration
	PollInterval  time.Duration
	Execute       func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error)
}

type BookJobWorker struct {
	store         *BookKnowledgeStore
	workerID      string
	leaseDuration time.Duration
	renewInterval time.Duration
	pollInterval  time.Duration
	execute       func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error)
}

type BookJobExecutionFailure struct {
	Code string
	Err  error
}

func (failure BookJobExecutionFailure) Error() string {
	return "book job execution failed"
}

func (failure BookJobExecutionFailure) Unwrap() error {
	return failure.Err
}

func (failure BookJobExecutionFailure) BookJobFailureCode() string {
	return failure.Code
}

func NewBookJobExecutionFailure(code string, err error) error {
	if !validBookJobFailureCode(code) {
		code = BookKnowledgeJobFailureUnknownFailure
	}
	if err == nil {
		err = errors.New("book job execution failed")
	}
	return &BookJobExecutionFailure{Code: code, Err: err}
}

func NewBookJobWorker(cfg BookJobWorkerConfig) (*BookJobWorker, error) {
	workerID := strings.TrimSpace(cfg.WorkerID)
	switch {
	case cfg.Store == nil:
		return nil, errors.New("book job worker store is required")
	case cfg.LeaseDuration <= 0:
		return nil, errors.New("book job worker lease duration must be positive")
	case cfg.RenewInterval <= 0 || cfg.RenewInterval >= cfg.LeaseDuration:
		return nil, errors.New("book job worker renew interval must be positive and shorter than the lease")
	case cfg.PollInterval <= 0:
		return nil, errors.New("book job worker poll interval must be positive")
	}
	if err := ValidateBookJobWorkerID(workerID); err != nil {
		return nil, err
	}
	worker := &BookJobWorker{
		store: cfg.Store, workerID: workerID, leaseDuration: cfg.LeaseDuration,
		renewInterval: cfg.RenewInterval, pollInterval: cfg.PollInterval, execute: cfg.Execute,
	}
	if worker.execute == nil {
		worker.execute = worker.executeDefault
	}
	return worker, nil
}

func ValidateBookJobWorkerID(workerID string) error {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return errors.New("book job worker id is required")
	}
	if len(workerID) > 128 || !bookJobWorkerIDPattern.MatchString(workerID) || strings.Contains(workerID, "..") {
		return errors.New("book job worker id is invalid")
	}
	lower := strings.ToLower(workerID)
	for _, marker := range []string{"token", "secret", "bearer", "cookie", "credential", "authorization"} {
		if strings.Contains(lower, marker) {
			return errors.New("book job worker id is invalid")
		}
	}
	for _, prefix := range []string{"sk-", "ghp_", "github_pat_", "xoxb-", "xoxp-"} {
		if strings.HasPrefix(lower, prefix) {
			return errors.New("book job worker id is invalid")
		}
	}
	return nil
}

func (w *BookJobWorker) RunOnce(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false, nil
	}
	if _, err := w.store.ReconcileExpiredBookKnowledgeJobs(); err != nil {
		return false, bookJobWorkerInfrastructureError("reconcile jobs")
	}
	if ctx.Err() != nil {
		return false, nil
	}
	job, err := w.store.ClaimNextBookKnowledgeJob(w.workerID, w.leaseDuration)
	if err != nil {
		return false, bookJobWorkerInfrastructureError("claim job")
	}
	if job == nil {
		return false, nil
	}
	if ctx.Err() != nil {
		if _, err := w.store.InterruptBookKnowledgeJob(job.ID, w.workerID); err != nil {
			return true, bookJobWorkerInfrastructureError("interrupt job")
		}
		return true, nil
	}
	return true, w.executeClaimed(ctx, *job)
}

func (w *BookJobWorker) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		processed, err := w.RunOnce(ctx)
		if err != nil {
			return err
		}
		if processed {
			continue
		}
		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

type bookJobWorkerExecutionResult struct {
	result map[string]any
	err    error
}

func (w *BookJobWorker) executeClaimed(parent context.Context, job BookKnowledgeJob) error {
	if parent.Err() != nil {
		if _, err := w.store.InterruptBookKnowledgeJob(job.ID, w.workerID); err != nil {
			return bookJobWorkerInfrastructureError("interrupt job")
		}
		return nil
	}
	runCtx, cancel := context.WithCancel(parent)
	defer cancel()

	stopRenew := make(chan struct{})
	renewDone := make(chan struct{})
	renewFailure := make(chan error, 1)
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(w.renewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopRenew:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if _, err := w.store.RenewBookKnowledgeJobLease(job.ID, w.workerID, w.leaseDuration); err != nil {
					select {
					case renewFailure <- bookJobWorkerInfrastructureError("renew lease"):
					default:
					}
					cancel()
					return
				}
			}
		}
	}()

	var stageMu sync.Mutex
	var stageFailure error
	lastStage := strings.TrimSpace(job.Stage)
	setStage := func(stage string) error {
		if err := runCtx.Err(); err != nil {
			return err
		}
		if _, err := w.store.UpdateBookKnowledgeJobStage(job.ID, w.workerID, stage); err != nil {
			safeErr := bookJobWorkerInfrastructureError("update job stage")
			stageMu.Lock()
			if stageFailure == nil {
				stageFailure = safeErr
			}
			stageMu.Unlock()
			cancel()
			return safeErr
		}
		stageMu.Lock()
		lastStage = strings.TrimSpace(stage)
		stageMu.Unlock()
		return nil
	}

	executionDone := make(chan bookJobWorkerExecutionResult, 1)
	go func() {
		result, executeErr := w.executeSafely(runCtx, job, setStage)
		executionDone <- bookJobWorkerExecutionResult{result: result, err: executeErr}
	}()
	execution := <-executionDone
	close(stopRenew)
	cancel()
	<-renewDone

	select {
	case renewErr := <-renewFailure:
		return renewErr
	default:
	}
	stageMu.Lock()
	infraErr := stageFailure
	stage := lastStage
	stageMu.Unlock()
	if infraErr != nil {
		return infraErr
	}

	if parent.Err() != nil || errors.Is(execution.err, context.Canceled) || errors.Is(execution.err, context.DeadlineExceeded) {
		if _, err := w.store.InterruptBookKnowledgeJob(job.ID, w.workerID); err != nil {
			return bookJobWorkerInfrastructureError("interrupt job")
		}
		return nil
	}
	if execution.err != nil {
		code := bookJobWorkerFailureCode(execution.err, stage)
		if _, err := w.store.FailBookKnowledgeJob(job.ID, w.workerID, code); err != nil {
			return bookJobWorkerInfrastructureError("fail job")
		}
		return nil
	}
	if _, err := w.store.CompleteBookKnowledgeJob(job.ID, w.workerID, execution.result); err != nil {
		return bookJobWorkerInfrastructureError("complete job")
	}
	return nil
}

func (w *BookJobWorker) executeSafely(ctx context.Context, job BookKnowledgeJob, setStage func(string) error) (result map[string]any, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = NewBookJobExecutionFailure(BookKnowledgeJobFailureUnknownFailure, errors.New("book job executor panic"))
		}
	}()
	return w.execute(ctx, job, setStage)
}

func (w *BookJobWorker) executeDefault(ctx context.Context, job BookKnowledgeJob, setStage func(string) error) (map[string]any, error) {
	switch job.Type {
	case BookKnowledgeJobTypeDedaoEbookDownload:
		if err := setStage("downloading"); err != nil {
			return nil, err
		}
		return runDedaoEbookDownloadJob(ctx, job)
	case BookKnowledgeJobTypeDedaoEbookSyncKBase:
		if err := setStage("downloading"); err != nil {
			return nil, err
		}
		if err := setStage("building_knowledge"); err != nil {
			return nil, err
		}
		return runDedaoEbookSyncKBaseJob(ctx, w.store, job)
	default:
		return nil, NewBookJobExecutionFailure(BookKnowledgeJobFailureUnknownFailure, errors.New("unsupported book job type"))
	}
}

type bookJobFailureCoder interface {
	BookJobFailureCode() string
}

func bookJobWorkerFailureCode(err error, stage string) string {
	var coded bookJobFailureCoder
	if errors.As(err, &coded) && validBookJobFailureCode(coded.BookJobFailureCode()) {
		return coded.BookJobFailureCode()
	}
	switch stage {
	case "downloading":
		return BookKnowledgeJobFailureDownloadFailed
	case "building_knowledge":
		return BookKnowledgeJobFailureKnowledgeBuildFailed
	default:
		return BookKnowledgeJobFailureUnknownFailure
	}
}

func validBookJobFailureCode(code string) bool {
	_, ok := bookKnowledgeJobFailureMessages[strings.TrimSpace(code)]
	return ok
}

func bookJobWorkerInfrastructureError(operation string) error {
	return fmt.Errorf("%w: %s", ErrBookJobWorkerInfrastructure, operation)
}
