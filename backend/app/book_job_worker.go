package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
)

var ErrBookJobWorkerInfrastructure = errors.New("book job worker infrastructure failure")
var ErrBookJobWorkerRestartRequested = errors.New("book job worker controlled restart requested")

const bookJobWorkerCommitLeaseWindow = 30 * time.Second

var bookJobWorkerIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

type BookJobWorkerConfig struct {
	Store             *BookKnowledgeStore
	WorkerID          string
	LeaseDuration     time.Duration
	RenewInterval     time.Duration
	PollInterval      time.Duration
	Execute           func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error)
	SourceAgentClient BookJobWorkerSourceAgentClient
	Version           string
	ProtocolVersion   string
}

type BookJobWorkerSourceAgentClient interface {
	Heartbeat(context.Context, SourceAgentHeartbeat) (SourceAgent, error)
	ClaimCommand(context.Context) (*SourceAgentCommand, error)
	ReportCommand(context.Context, string, string, string, string, string) (SourceAgentCommand, error)
}

type BookJobWorker struct {
	store             *BookKnowledgeStore
	workerID          string
	leaseDuration     time.Duration
	renewInterval     time.Duration
	pollInterval      time.Duration
	execute           func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error)
	sourceAgentClient BookJobWorkerSourceAgentClient
	version           string
	protocolVersion   string
	beforeClaim       func()
	renewLease        func(BookKnowledgeJob) error
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
	case cfg.SourceAgentClient != nil && strings.TrimSpace(cfg.Version) == "":
		return nil, errors.New("book job worker version is required with source agent control")
	case cfg.SourceAgentClient != nil && strings.TrimSpace(cfg.ProtocolVersion) == "":
		return nil, errors.New("book job worker protocol version is required with source agent control")
	}
	if err := ValidateBookJobWorkerID(workerID); err != nil {
		return nil, err
	}
	worker := &BookJobWorker{
		store: cfg.Store, workerID: workerID, leaseDuration: cfg.LeaseDuration,
		renewInterval: cfg.RenewInterval, pollInterval: cfg.PollInterval, execute: cfg.Execute,
		sourceAgentClient: cfg.SourceAgentClient, version: strings.TrimSpace(cfg.Version),
		protocolVersion: strings.TrimSpace(cfg.ProtocolVersion),
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
	if err := w.heartbeat(ctx, ""); err != nil {
		return false, err
	}
	processedControl := false
	command, err := w.claimSourceAgentCommand(ctx)
	if err != nil {
		return false, err
	}
	if command != nil {
		processedControl = true
		restart, commandErr := w.processSourceAgentCommand(ctx, command)
		if commandErr != nil {
			return true, commandErr
		}
		if restart {
			if err := w.reportRestartSuccess(ctx, command.ID); err != nil {
				return true, err
			}
			return true, ErrBookJobWorkerRestartRequested
		}
	}
	if _, err := w.store.ReconcileExpiredBookKnowledgeJobsContext(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return false, nil
		}
		return false, bookJobWorkerInfrastructureError("reconcile jobs")
	}
	if w.beforeClaim != nil {
		w.beforeClaim()
	}
	if ctx.Err() != nil {
		return false, nil
	}
	job, err := w.store.ClaimNextBookKnowledgeJob(w.workerID, w.leaseDuration)
	if err != nil {
		return false, bookJobWorkerInfrastructureError("claim job")
	}
	currentRunID := ""
	if job != nil {
		currentRunID = job.ID
	}
	var activeControlErr error
	if err := w.heartbeat(ctx, currentRunID); err != nil {
		if job == nil {
			return processedControl, err
		}
		activeControlErr = err
	}
	if job == nil {
		return processedControl, nil
	}
	if ctx.Err() != nil {
		if _, err := w.store.InterruptBookKnowledgeJob(job.ID, w.workerID); err != nil {
			return true, bookJobWorkerInfrastructureError("interrupt job")
		}
		return true, nil
	}
	return true, w.executeClaimed(ctx, *job, activeControlErr)
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
		if errors.Is(err, ErrBookJobWorkerRestartRequested) {
			return nil
		}
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

func (w *BookJobWorker) heartbeat(ctx context.Context, currentRunID string) error {
	if w.sourceAgentClient == nil {
		return nil
	}
	_, err := w.sourceAgentClient.Heartbeat(ctx, SourceAgentHeartbeat{
		WorkerType: "book-job-worker", Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		Version: w.version, ProtocolVersion: w.protocolVersion,
		Capabilities: []string{"book_jobs", "diagnose", "controlled_restart"}, CurrentRunID: currentRunID,
	})
	if err != nil {
		return bookJobWorkerInfrastructureError("source agent heartbeat")
	}
	return nil
}

func (w *BookJobWorker) claimSourceAgentCommand(ctx context.Context) (*SourceAgentCommand, error) {
	if w.sourceAgentClient == nil {
		return nil, nil
	}
	command, err := w.sourceAgentClient.ClaimCommand(ctx)
	if err != nil {
		return nil, bookJobWorkerInfrastructureError("claim source agent command")
	}
	return command, nil
}

func (w *BookJobWorker) processSourceAgentCommand(ctx context.Context, command *SourceAgentCommand) (bool, error) {
	if command == nil || strings.TrimSpace(command.ID) == "" || command.State != SourceAgentCommandClaimed {
		return false, bookJobWorkerInfrastructureError("invalid source agent command")
	}
	switch command.Type {
	case SourceAgentCommandRestart:
		if command.UpgradeSpec != nil {
			if _, err := w.sourceAgentClient.ReportCommand(
				ctx, command.ID, SourceAgentCommandFailed, SourceAgentCommandCodeRestartFailed,
				"controlled restart command is invalid", "",
			); err != nil {
				return false, bookJobWorkerInfrastructureError("report invalid controlled restart command")
			}
			return false, nil
		}
		return true, nil
	case SourceAgentCommandDiagnose:
		if command.UpgradeSpec != nil {
			if _, err := w.sourceAgentClient.ReportCommand(
				ctx, command.ID, SourceAgentCommandFailed, SourceAgentCommandCodeDiagnosticFailed,
				"diagnose command is invalid", "",
			); err != nil {
				return false, bookJobWorkerInfrastructureError("report invalid diagnose command")
			}
			return false, nil
		}
		if _, err := w.sourceAgentClient.ReportCommand(
			ctx, command.ID, SourceAgentCommandSucceeded, SourceAgentCommandCodeDiagnosticComplete, "book job worker is healthy", "",
		); err != nil {
			return false, bookJobWorkerInfrastructureError("report source agent diagnosis")
		}
		return false, nil
	default:
		code := SourceAgentCommandCodeUpgradeFailed
		if _, err := w.sourceAgentClient.ReportCommand(ctx, command.ID, SourceAgentCommandFailed, code, "command is not supported by this worker", ""); err != nil {
			return false, bookJobWorkerInfrastructureError("report unsupported source agent command")
		}
		return false, nil
	}
}

func (w *BookJobWorker) reportRestartSuccess(ctx context.Context, commandID string) error {
	if _, err := w.sourceAgentClient.ReportCommand(
		ctx, commandID, SourceAgentCommandSucceeded, SourceAgentCommandCodeRestartComplete, "", "",
	); err != nil {
		return bookJobWorkerInfrastructureError("report controlled restart")
	}
	return nil
}

type bookJobWorkerExecutionResult struct {
	result map[string]any
	err    error
}

func (w *BookJobWorker) executeClaimed(parent context.Context, job BookKnowledgeJob, initialControlErr error) error {
	if parent.Err() != nil {
		if _, err := w.store.InterruptBookKnowledgeJob(job.ID, w.workerID); err != nil {
			return bookJobWorkerInfrastructureError("interrupt job")
		}
		return nil
	}
	runCtx, cancel := context.WithCancel(parent)
	defer cancel()
	var leaseOperationMu sync.Mutex
	renewOwnedLease := func() error {
		leaseOperationMu.Lock()
		defer leaseOperationMu.Unlock()
		if w.renewLease != nil {
			return w.renewLease(job)
		}
		_, err := w.store.RenewBookKnowledgeJobLease(job.ID, w.workerID, w.leaseDuration)
		return err
	}
	if job.Type == BookKnowledgeJobTypeDedaoEbookSyncKBase {
		runCtx = contextWithBookKnowledgePackageCommitFence(runCtx, bookKnowledgePackageCommitFence{
			prepare: func(ctx context.Context, pkg BookKnowledgePackage) (bookKnowledgeJobCommitMarker, error) {
				if err := ctx.Err(); err != nil {
					return bookKnowledgeJobCommitMarker{}, err
				}
				leaseOperationMu.Lock()
				defer leaseOperationMu.Unlock()
				_, publishNonce, err := w.store.fenceBookKnowledgeJobPackageCommit(
					job.ID, w.workerID, pkg.Book.BookID, pkg.Book.ContentHash, bookJobWorkerCommitLeaseWindow,
				)
				if err != nil {
					return bookKnowledgeJobCommitMarker{}, err
				}
				return bookKnowledgeJobCommitMarker{
					Version: bookKnowledgeJobCommitMarkerVersion, JobID: job.ID, PublishNonce: publishNonce,
					BookID: pkg.Book.BookID, ContentHash: pkg.Book.ContentHash,
				}, nil
			},
			discard: func(pkg BookKnowledgePackage, marker bookKnowledgeJobCommitMarker) error {
				return w.store.discardBookKnowledgeJobCommitReceipt(
					job.ID, w.workerID, pkg.Book.BookID, pkg.Book.ContentHash, marker.PublishNonce,
				)
			},
		})
	}

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
				err := renewOwnedLease()
				if err != nil {
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

	controlStop := make(chan struct{})
	controlDone := make(chan struct{})
	restartCommand := make(chan *SourceAgentCommand, 1)
	controlFailure := make(chan error, 1)
	if w.sourceAgentClient == nil {
		close(controlDone)
	} else {
		go w.monitorSourceAgentCommands(runCtx, job.ID, controlStop, controlDone, restartCommand, controlFailure)
	}

	var execution bookJobWorkerExecutionResult
	var requestedRestart *SourceAgentCommand
	controlErr := initialControlErr
	controlFailureChannel := (<-chan error)(controlFailure)
	waitingForExecution := true
	for waitingForExecution {
		select {
		case execution = <-executionDone:
			waitingForExecution = false
		case requestedRestart = <-restartCommand:
			cancel()
			execution = <-executionDone
			waitingForExecution = false
		case controlErr = <-controlFailureChannel:
			// Control-plane outages are isolated from a running book job. Stop
			// selecting this one-shot failure and let the owned job finish.
			controlFailureChannel = nil
		}
	}
	close(stopRenew)
	close(controlStop)
	cancel()
	<-renewDone
	<-controlDone
	if requestedRestart == nil {
		select {
		case requestedRestart = <-restartCommand:
		default:
		}
	}
	if controlErr == nil {
		select {
		case controlErr = <-controlFailure:
		default:
		}
	}

	if requestedRestart != nil {
		if _, err := w.store.InterruptBookKnowledgeJob(job.ID, w.workerID); err != nil {
			_, _ = w.sourceAgentClient.ReportCommand(
				parent, requestedRestart.ID, SourceAgentCommandFailed, SourceAgentCommandCodeRestartFailed,
				"book job interruption failed", "",
			)
			return bookJobWorkerInfrastructureError("interrupt job for controlled restart")
		}
		if err := w.reportRestartSuccess(parent, requestedRestart.ID); err != nil {
			return err
		}
		return ErrBookJobWorkerRestartRequested
	}
	var renewErr error
	select {
	case received := <-renewFailure:
		renewErr = received
	default:
	}
	stageMu.Lock()
	infraErr := stageFailure
	stage := lastStage
	stageMu.Unlock()
	if infraErr != nil {
		return infraErr
	}

	if execution.err == nil {
		if _, err := w.store.CompleteBookKnowledgeJob(job.ID, w.workerID, execution.result); err != nil {
			return bookJobWorkerInfrastructureError("complete job")
		}
		if controlErr != nil {
			return controlErr
		}
		return nil
	}
	if errors.Is(execution.err, errBookKnowledgePublishRecoveryRequired) {
		if _, err := w.store.markBookKnowledgeJobRecoveryRequired(job.ID, w.workerID); err != nil {
			return bookJobWorkerInfrastructureError("mark package publish recovery required")
		}
		return bookJobWorkerInfrastructureError("package publish recovery required")
	}
	if renewErr != nil {
		return renewErr
	}
	if parent.Err() != nil {
		if _, err := w.store.InterruptBookKnowledgeJob(job.ID, w.workerID); err != nil {
			return bookJobWorkerInfrastructureError("interrupt job")
		}
		return nil
	}
	code := bookJobWorkerFailureCode(execution.err, stage)
	if code == BookKnowledgeJobFailureWorkerInterrupted {
		if _, err := w.store.InterruptBookKnowledgeJob(job.ID, w.workerID); err != nil {
			return bookJobWorkerInfrastructureError("interrupt job")
		}
		return nil
	}
	if _, err := w.store.FailBookKnowledgeJob(job.ID, w.workerID, code); err != nil {
		return bookJobWorkerInfrastructureError("fail job")
	}
	return nil
}

func (w *BookJobWorker) monitorSourceAgentCommands(
	ctx context.Context,
	currentRunID string,
	stop <-chan struct{},
	done chan<- struct{},
	restart chan<- *SourceAgentCommand,
	failure chan<- error,
) {
	defer close(done)
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		if err := w.heartbeat(ctx, currentRunID); err != nil {
			if ctx.Err() != nil {
				return
			}
			select {
			case failure <- err:
			default:
			}
			return
		}
		command, err := w.claimSourceAgentCommand(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			select {
			case failure <- err:
			default:
			}
			return
		}
		if command != nil {
			restartRequested, err := w.processSourceAgentCommand(ctx, command)
			if err != nil {
				select {
				case failure <- err:
				default:
				}
				return
			}
			if restartRequested {
				restart <- command
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
		}
	}
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
		return runDedaoEbookSyncKBaseJobWithStages(ctx, w.store, job, setStage)
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
	var remoteErr *services.RemoteError
	if errors.As(err, &remoteErr) {
		switch remoteErr.Kind {
		case services.RemoteErrorAuthentication:
			return BookKnowledgeJobFailureAuthenticationRequired
		case services.RemoteErrorSourceChanged:
			return BookKnowledgeJobFailureSourceChanged
		}
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
