package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"path/filepath"
	"sync"
)

const (
	sourceAgentUpdateUpdaterBasename      = "source-agent-updater"
	sourceAgentUpdateLifecycleFileName    = ".source-agent-lifecycle.lock"
	sourceAgentUpdateMaintenanceFileName  = ".managed-worker-maintenance"
	sourceAgentUpdateStagingDirectoryName = ".source-agent-staging"
	sourceAgentUpdateHandoffDirectoryName = ".source-agent-handoff"
	sourceAgentUpdateStagedBasename       = "worker.staged"
	sourceAgentUpdateHandoffFileName      = "handoff.json"
	sourceAgentUpdateStagedPendingName    = ".worker.staged.pending"
	sourceAgentUpdateHandoffPendingName   = ".handoff.json.pending"
)

var (
	errSourceAgentUpdateBridgeUnavailable = errors.New("source agent update bridge is unavailable")
	errSourceAgentUpdateHandoffConflict   = errors.New("source agent update handoff conflicts")
)

type SourceAgentArtifactDownloader interface {
	DownloadArtifact(
		context.Context,
		SourceAgentCommand,
		SourceAgentArtifactTarget,
		string,
	) (SourceAgentArtifactPublic, io.ReadCloser, error)
}

type SourceAgentUpdateBridgeConfig struct {
	Downloader        SourceAgentArtifactDownloader
	UpdaterExecutable string
	WorkerType        string
	CurrentVersion    string
	Platform          string
	Architecture      string
	ProtocolVersion   string
	Revision          string
	Activator         SourceAgentUpdaterActivator
}

type sourceAgentUpdateBridgeStorage interface {
	AcquireLifecycleShared() (func() error, error)
	Lock() error
	Unlock() error
	LoadHandoff() ([]byte, bool, error)
	Stage(context.Context, io.Reader, int64, string) error
	VerifyStaged(int64, string) error
	PublishHandoff([]byte) error
	StagedPath() string
	RemoveStaged() error
	Close() error
}

type SourceAgentUpdateBridge struct {
	lifecycle sync.Mutex
	operation sync.Mutex
	mu        sync.Mutex
	config    SourceAgentUpdateBridgeConfig
	storage   sourceAgentUpdateBridgeStorage
	receipts  *FileSourceAgentUpdateReceiptStore
	closed    bool
	faultAt   string
}

type sourceAgentUpdateHandoff struct {
	CommandID          string `json:"command_id"`
	ArtifactID         string `json:"artifact_id"`
	WorkerType         string `json:"worker_type"`
	CurrentVersion     string `json:"current_version"`
	TargetVersion      string `json:"target_version"`
	SHA256             string `json:"sha256"`
	Size               int64  `json:"size"`
	StagedBasename     string `json:"staged_basename"`
	Platform           string `json:"platform"`
	Architecture       string `json:"architecture"`
	ProtocolVersion    string `json:"protocol_version"`
	Revision           string `json:"revision"`
	Channel            string `json:"channel"`
	RequestFingerprint string `json:"request_fingerprint"`
}

func NewSourceAgentUpdateBridge(config SourceAgentUpdateBridgeConfig) (*SourceAgentUpdateBridge, error) {
	if config.Downloader == nil || !isCleanAbsoluteSourceAgentUpdatePath(config.UpdaterExecutable) ||
		filepath.Base(config.UpdaterExecutable) != sourceAgentUpdateUpdaterBasename ||
		!isAllowedSourceAgentUpdateWorkerType(config.WorkerType) ||
		!isSourceAgentArtifactVersion(config.CurrentVersion) ||
		!isExactSourceAgentArtifactName("platform", config.Platform) || config.Platform != "darwin" ||
		!isExactSourceAgentArtifactName("architecture", config.Architecture) ||
		!isExactSourceAgentProtocolVersion(config.ProtocolVersion) ||
		(config.Activator != nil && !isExactLowerHex(config.Revision, 40) && !isExactLowerHex(config.Revision, 64)) {
		return nil, errSourceAgentUpdateBridgeUnavailable
	}
	storage, err := newSourceAgentUpdateBridgeStorage(config.UpdaterExecutable, config.WorkerType)
	if err != nil && !errors.Is(err, ErrSourceAgentUpdateBusy) {
		return nil, errSourceAgentUpdateBridgeUnavailable
	}
	receipts, err := NewFileSourceAgentUpdateReceiptStore(
		filepath.Join(filepath.Dir(config.UpdaterExecutable), sourceAgentUpdateHandoffDirectoryName),
		sourceAgentUpdateDefaultPoll,
	)
	if err != nil {
		if storage != nil {
			_ = storage.Close()
		}
		return nil, errSourceAgentUpdateBridgeUnavailable
	}
	return &SourceAgentUpdateBridge{config: config, storage: storage, receipts: receipts}, nil
}

// WithSourceAgentLifecycle keeps installer maintenance mutually exclusive with
// one bounded Worker control operation. The callback must not outlive this call.
func (b *SourceAgentUpdateBridge) WithSourceAgentLifecycle(
	ctx context.Context,
	operation func() error,
) (returnErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if operation == nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errSourceAgentUpdateBridgeUnavailable
	}
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errSourceAgentUpdateBridgeUnavailable
	}
	if err := b.ensureStorageLocked(); err != nil {
		b.mu.Unlock()
		if errors.Is(err, ErrSourceAgentUpdateBusy) {
			return ErrSourceAgentUpdateBusy
		}
		return errSourceAgentUpdateBridgeUnavailable
	}
	storage := b.storage
	b.mu.Unlock()
	release, err := storage.AcquireLifecycleShared()
	if err != nil {
		if errors.Is(err, ErrSourceAgentUpdateBusy) {
			return ErrSourceAgentUpdateBusy
		}
		return errSourceAgentUpdateBridgeUnavailable
	}
	defer func() { returnErr = errors.Join(returnErr, release()) }()
	return operation()
}

func (b *SourceAgentUpdateBridge) Prepare(
	ctx context.Context,
	command SourceAgentCommand,
) (request SourceAgentUpdateRequest, returnErr error) {
	returnErr = b.WithSourceAgentLifecycle(ctx, func() error {
		b.operation.Lock()
		defer b.operation.Unlock()
		var err error
		request, err = b.prepare(ctx, command)
		return err
	})
	return request, returnErr
}

func (b *SourceAgentUpdateBridge) prepare(
	ctx context.Context,
	command SourceAgentCommand,
) (request SourceAgentUpdateRequest, returnErr error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || ctx.Err() != nil {
		if ctx.Err() != nil {
			return SourceAgentUpdateRequest{}, ctx.Err()
		}
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateBridgeUnavailable
	}
	if err := b.ensureStorageLocked(); err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	if !b.validCommand(command) {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	if err := b.storage.Lock(); err != nil {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateBridgeUnavailable
	}
	defer func() {
		if err := b.storage.Unlock(); err != nil {
			request = SourceAgentUpdateRequest{}
			returnErr = errSourceAgentUpdateBridgeUnavailable
		}
	}()
	if payload, found, err := b.storage.LoadHandoff(); err != nil {
		return SourceAgentUpdateRequest{}, err
	} else if found {
		request, err := b.requestFromHandoff(payload)
		if err != nil || !b.requestMatchesCommand(request, command) ||
			b.storage.VerifyStaged(request.ExpectedSize, request.ExpectedSHA256) != nil {
			return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
		}
		return request, nil
	}

	target := SourceAgentArtifactTarget{
		WorkerType: b.config.WorkerType, Platform: b.config.Platform,
		Architecture: b.config.Architecture, CurrentVersion: b.config.CurrentVersion,
	}
	metadata, body, err := b.config.Downloader.DownloadArtifact(ctx, command, target, b.config.ProtocolVersion)
	if err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	if body == nil {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	defer body.Close()
	request, err = b.requestFromMetadata(command, metadata)
	if err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	if err := b.storage.Stage(ctx, body, request.ExpectedSize, request.ExpectedSHA256); err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	if b.faultAt == "after_stage" {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateBridgeUnavailable
	}
	handoff := sourceAgentUpdateHandoffFromRequest(request)
	payload, err := marshalSourceAgentUpdateJSON(handoff)
	if err != nil {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	if err := b.storage.PublishHandoff(payload); err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	if b.faultAt == "after_handoff" {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateBridgeUnavailable
	}
	return request, nil
}

func (b *SourceAgentUpdateBridge) Close() error {
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	b.operation.Lock()
	defer b.operation.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	var closeErrors []error
	if b.receipts != nil {
		closeErrors = append(closeErrors, b.receipts.Close())
	}
	if b.storage != nil {
		closeErrors = append(closeErrors, b.storage.Close())
	}
	return errors.Join(closeErrors...)
}

func (b *SourceAgentUpdateBridge) validCommand(command SourceAgentCommand) bool {
	return command.State == SourceAgentCommandDownloading && command.Type == SourceAgentCommandUpgrade &&
		validSourceAgentUpgradeHandoff(command) && command.UpgradeSpec.ExpectedCurrentVersion == b.config.CurrentVersion &&
		(command.ExpectedCurrentVersion == "" || command.ExpectedCurrentVersion == b.config.CurrentVersion)
}

func (b *SourceAgentUpdateBridge) requestFromMetadata(
	command SourceAgentCommand,
	metadata SourceAgentArtifactPublic,
) (SourceAgentUpdateRequest, error) {
	check := SourceAgentUpdateGuardCheck{
		CommandID: command.ID, ArtifactID: metadata.ID, WorkerType: metadata.WorkerType,
		CurrentVersion: b.config.CurrentVersion, Version: metadata.Version,
		Revision: metadata.Revision, Channel: metadata.Channel, Size: metadata.Size, SHA256: metadata.SHA256,
		Platform: metadata.Platform, Architecture: metadata.Architecture, ProtocolVersion: metadata.ProtocolVersion,
	}
	if !validSourceAgentUpdateGuardCheck(check) || metadata.ID != command.UpgradeSpec.ArtifactID ||
		metadata.WorkerType != b.config.WorkerType || metadata.Platform != b.config.Platform ||
		metadata.Architecture != b.config.Architecture || metadata.ProtocolVersion != b.config.ProtocolVersion {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	return SourceAgentUpdateRequest{
		CommandID: command.ID, ArtifactID: metadata.ID,
		WorkerType: b.config.WorkerType, CurrentVersion: b.config.CurrentVersion, TargetVersion: metadata.Version,
		ExpectedSHA256: metadata.SHA256, ExpectedSize: metadata.Size, StagedBinary: b.storage.StagedPath(),
		Platform: b.config.Platform, Architecture: b.config.Architecture, ProtocolVersion: b.config.ProtocolVersion,
		Revision: metadata.Revision, Channel: metadata.Channel,
	}, nil
}

func sourceAgentUpdateHandoffFromRequest(request SourceAgentUpdateRequest) sourceAgentUpdateHandoff {
	return sourceAgentUpdateHandoff{
		CommandID: request.CommandID, ArtifactID: request.ArtifactID,
		WorkerType: request.WorkerType, CurrentVersion: request.CurrentVersion, TargetVersion: request.TargetVersion,
		SHA256: request.ExpectedSHA256, Size: request.ExpectedSize, StagedBasename: sourceAgentUpdateStagedBasename,
		Platform: request.Platform, Architecture: request.Architecture, ProtocolVersion: request.ProtocolVersion,
		Revision: request.Revision, Channel: request.Channel,
		RequestFingerprint: sourceAgentUpdateRequestFingerprint(request),
	}
}

func (b *SourceAgentUpdateBridge) requestFromHandoff(payload []byte) (SourceAgentUpdateRequest, error) {
	return sourceAgentUpdateRequestFromHandoff(payload, b.storage.StagedPath())
}

func sourceAgentUpdateRequestFromHandoff(payload []byte, stagedPath string) (SourceAgentUpdateRequest, error) {
	var handoff sourceAgentUpdateHandoff
	if len(payload) == 0 || len(payload) > sourceAgentUpdateReceiptMaxBytes ||
		decodeStrictSourceAgentUpdateJSON(payload, &handoff) != nil || handoff.StagedBasename != sourceAgentUpdateStagedBasename ||
		!isCleanAbsoluteSourceAgentUpdatePath(stagedPath) {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	request := SourceAgentUpdateRequest{
		CommandID: handoff.CommandID, ArtifactID: handoff.ArtifactID,
		WorkerType: handoff.WorkerType, CurrentVersion: handoff.CurrentVersion, TargetVersion: handoff.TargetVersion,
		ExpectedSHA256: handoff.SHA256, ExpectedSize: handoff.Size, StagedBinary: stagedPath,
		Platform: handoff.Platform, Architecture: handoff.Architecture, ProtocolVersion: handoff.ProtocolVersion,
		Revision: handoff.Revision, Channel: handoff.Channel,
	}
	check := SourceAgentUpdateGuardCheck{
		CommandID: request.CommandID, ArtifactID: request.ArtifactID, WorkerType: request.WorkerType,
		CurrentVersion: request.CurrentVersion, Version: request.TargetVersion,
		Revision: request.Revision, Channel: request.Channel, Size: request.ExpectedSize, SHA256: request.ExpectedSHA256,
		Platform: request.Platform, Architecture: request.Architecture, ProtocolVersion: request.ProtocolVersion,
	}
	if !validSourceAgentUpdateGuardCheck(check) || !isExactLowerHex(handoff.RequestFingerprint, sha256.Size*2) ||
		handoff.RequestFingerprint != sourceAgentUpdateRequestFingerprint(request) {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	return request, nil
}

func (b *SourceAgentUpdateBridge) Upgrade(ctx context.Context, command SourceAgentCommand) SourceAgentUpgradeResult {
	var result SourceAgentUpgradeResult
	if err := b.WithSourceAgentLifecycle(ctx, func() error {
		result = b.upgrade(ctx, command)
		return nil
	}); err != nil {
		return SourceAgentUpgradeResult{Waiting: true}
	}
	return result
}

func (b *SourceAgentUpdateBridge) upgrade(ctx context.Context, command SourceAgentCommand) SourceAgentUpgradeResult {
	b.operation.Lock()
	defer b.operation.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if command.State == SourceAgentCommandDownloading {
		if _, err := b.prepare(ctx, command); err != nil {
			if sourceAgentUpdateErrorIsRetryable(err) {
				return SourceAgentUpgradeResult{Waiting: true}
			}
			return SourceAgentUpgradeResult{
				State: SourceAgentCommandFailed, Code: sourceAgentUpdateDownloadFailureCode(err),
			}
		}
	}

	request, found, err := b.loadPreparedRequest(command)
	if err != nil {
		if errors.Is(err, ErrSourceAgentUpdateBusy) {
			return SourceAgentUpgradeResult{Waiting: true}
		}
		return sourceAgentUpgradeResultFromInvalidEvidence(command.State)
	}
	evidence := sourceAgentUpdatePhaseEvidence{HandoffPublished: found}
	if command.State == SourceAgentCommandInstalling && found {
		if b.receipts == nil || b.config.Activator == nil {
			return SourceAgentUpgradeResult{
				State: SourceAgentCommandFailed, Code: SourceAgentCommandCodeInstallFailed,
			}
		}
		if err := b.receipts.PublishPending(ctx, request); err != nil {
			return sourceAgentUpgradeResultFromInvalidEvidence(command.State)
		}
		if err := b.config.Activator.StartUpdater(ctx); err != nil {
			return SourceAgentUpgradeResult{Waiting: true}
		}
	}
	if found && b.receipts != nil {
		evidence, err = b.loadPhaseEvidence(request)
		if err != nil {
			return sourceAgentUpgradeResultFromInvalidEvidence(command.State)
		}
	}
	decision, err := mapSourceAgentUpdatePhase(command.State, evidence)
	if err != nil {
		return sourceAgentUpgradeResultFromInvalidEvidence(command.State)
	}
	if decision.Waiting {
		return SourceAgentUpgradeResult{Waiting: true}
	}
	return decision.Report
}

func (b *SourceAgentUpdateBridge) loadPreparedRequest(
	command SourceAgentCommand,
) (request SourceAgentUpdateRequest, found bool, returnErr error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || command.UpgradeSpec == nil {
		return SourceAgentUpdateRequest{}, false, errSourceAgentUpdateBridgeUnavailable
	}
	if err := b.ensureStorageLocked(); err != nil {
		return SourceAgentUpdateRequest{}, false, err
	}
	if err := b.storage.Lock(); err != nil {
		return SourceAgentUpdateRequest{}, false, err
	}
	defer func() {
		if err := b.storage.Unlock(); err != nil {
			request, found, returnErr = SourceAgentUpdateRequest{}, false, err
		}
	}()
	payload, found, err := b.storage.LoadHandoff()
	if err != nil || !found {
		return SourceAgentUpdateRequest{}, found, err
	}
	request, err = b.requestFromHandoff(payload)
	if err != nil || request.CommandID != command.ID || request.ArtifactID != command.UpgradeSpec.ArtifactID ||
		request.CurrentVersion != command.UpgradeSpec.ExpectedCurrentVersion ||
		(command.ExpectedCurrentVersion != "" && request.CurrentVersion != command.ExpectedCurrentVersion) ||
		b.storage.VerifyStaged(request.ExpectedSize, request.ExpectedSHA256) != nil {
		return SourceAgentUpdateRequest{}, true, errSourceAgentUpdateHandoffConflict
	}
	return request, true, nil
}

func (b *SourceAgentUpdateBridge) loadPhaseEvidence(request SourceAgentUpdateRequest) (sourceAgentUpdatePhaseEvidence, error) {
	evidence := sourceAgentUpdatePhaseEvidence{HandoffPublished: true}
	pending, found, err := b.receipts.LoadPending()
	if err != nil {
		return sourceAgentUpdatePhaseEvidence{}, err
	}
	if found {
		if pending.CommandID != request.CommandID ||
			pending.RequestFingerprint != sourceAgentUpdateRequestFingerprint(request) {
			return sourceAgentUpdatePhaseEvidence{}, ErrSourceAgentUpgradeCheckpointConflict
		}
		evidence.UpdaterRequested = true
	}
	journal, journalFound, err := b.receipts.loadJournal()
	if err != nil {
		return sourceAgentUpdatePhaseEvidence{}, err
	}
	if journalFound {
		if !sourceAgentUpdateJournalMatches(journal, request) {
			return sourceAgentUpdatePhaseEvidence{}, ErrSourceAgentUpgradeCheckpointConflict
		}
		evidence.JournalStage = journal.Stage
		evidence.ReadyAfterHeartbeat, err = b.receipts.hasMatchingReady(journal)
		if err != nil {
			return sourceAgentUpdatePhaseEvidence{}, err
		}
	}
	outcome, outcomeFound, err := b.receipts.LoadOutcome(request.CommandID)
	if err != nil {
		return sourceAgentUpdatePhaseEvidence{}, err
	}
	if outcomeFound {
		if !sourceAgentUpdateOutcomeMatches(outcome, request) {
			return sourceAgentUpdatePhaseEvidence{}, ErrSourceAgentUpgradeCheckpointConflict
		}
		evidence.Outcome = &outcome
		evidence.RuntimeIdentityMatches = b.runtimeIdentityMatches(outcome)
	}
	resolution, resolutionFound, err := b.receipts.LoadUpgradeTerminalResolution()
	if err != nil {
		return sourceAgentUpdatePhaseEvidence{}, err
	}
	if resolutionFound {
		if resolution.CommandID != request.CommandID ||
			resolution.RequestFingerprint != sourceAgentUpdateRequestFingerprint(request) {
			return sourceAgentUpdatePhaseEvidence{}, ErrSourceAgentUpgradeCheckpointConflict
		}
		evidence.RollbackRequested = resolution.Action == SourceAgentUpgradeTerminalRollback
	}
	return evidence, nil
}

func (b *SourceAgentUpdateBridge) runtimeIdentityMatches(outcome SourceAgentUpdateResult) bool {
	return outcome.WorkerType == b.config.WorkerType && outcome.RuntimeVersion == b.config.CurrentVersion &&
		outcome.Platform == b.config.Platform && outcome.Architecture == b.config.Architecture &&
		outcome.ProtocolVersion == b.config.ProtocolVersion && outcome.Revision == b.config.Revision
}

func sourceAgentUpgradeResultFromInvalidEvidence(serverState string) SourceAgentUpgradeResult {
	report := sourceAgentInvalidUpgradeReport(serverState)
	return SourceAgentUpgradeResult{State: report.state, Code: report.code}
}

func sourceAgentUpdateErrorIsRetryable(err error) bool {
	if errors.Is(err, ErrSourceAgentUpdateBusy) {
		return true
	}
	type retryable interface{ Retryable() bool }
	var classified retryable
	return errors.As(err, &classified) && classified.Retryable()
}

func sourceAgentUpdateDownloadFailureCode(err error) string {
	var response *SourceAgentHTTPError
	if errors.As(err, &response) {
		return SourceAgentCommandCodeDownloadFailed
	}
	return SourceAgentCommandCodeVerificationFailed
}

func (b *SourceAgentUpdateBridge) PublishAuthenticatedReady(
	ctx context.Context,
	identity SourceAgentRuntimeIdentity,
) (bool, error) {
	if b.receipts == nil {
		return false, errSourceAgentUpdateBridgeUnavailable
	}
	return b.receipts.PublishAuthenticatedReady(ctx, identity)
}

func (b *SourceAgentUpdateBridge) LoadUpgradeCommandCheckpoint() (
	SourceAgentUpgradeCommandCheckpoint,
	bool,
	error,
) {
	if b.receipts == nil {
		return SourceAgentUpgradeCommandCheckpoint{}, false, errSourceAgentUpdateBridgeUnavailable
	}
	return b.receipts.LoadUpgradeCommandCheckpoint()
}

func (b *SourceAgentUpdateBridge) SaveUpgradeCommandCheckpoint(
	ctx context.Context,
	command SourceAgentCommand,
) error {
	if b.receipts == nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	return b.receipts.SaveUpgradeCommandCheckpoint(ctx, command)
}

func (b *SourceAgentUpdateBridge) RecordAuthenticatedUpgradeConflict(
	ctx context.Context,
	commandID, fingerprint string,
) error {
	b.mu.Lock()
	closed := b.closed
	receipts := b.receipts
	b.mu.Unlock()
	if closed || receipts == nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	return receipts.RecordAuthenticatedUpgradeConflict(ctx, commandID, fingerprint)
}

func (b *SourceAgentUpdateBridge) RecordServerTerminalObservation(
	ctx context.Context,
	command SourceAgentCommand,
) error {
	b.operation.Lock()
	defer b.operation.Unlock()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errSourceAgentUpdateBridgeUnavailable
	}
	b.mu.Unlock()
	if b.receipts == nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	if err := b.receipts.RecordServerTerminalObservation(ctx, command); err != nil {
		return err
	}
	if _, err := b.receipts.ResolveUpgradeTerminalObservation(ctx); err == nil {
		return nil
	} else if !errors.Is(err, ErrSourceAgentUpgradeTerminalResolutionInvalid) {
		return err
	}
	cleanup, err := b.prepareNoAttemptTerminalCleanup(ctx, command)
	if err != nil {
		return err
	}
	return b.receipts.finishNoAttemptTerminalCleanup(ctx, cleanup)
}

func (b *SourceAgentUpdateBridge) prepareNoAttemptTerminalCleanup(
	ctx context.Context,
	command SourceAgentCommand,
) (cleanup sourceAgentUpgradeNoAttemptCleanup, returnErr error) {
	b.mu.Lock()
	if err := b.ensureStorageLocked(); err != nil {
		b.mu.Unlock()
		return sourceAgentUpgradeNoAttemptCleanup{}, err
	}
	storage := b.storage
	b.mu.Unlock()
	if err := storage.Lock(); err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, storage.Unlock())
	}()
	payload, found, err := storage.LoadHandoff()
	if err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, err
	}
	if found {
		request, requestErr := b.requestFromHandoff(payload)
		if requestErr != nil || !b.requestMatchesCommand(request, command) {
			return sourceAgentUpgradeNoAttemptCleanup{}, errSourceAgentUpdateHandoffConflict
		}
	}
	cleanup, err = b.receipts.prepareNoAttemptTerminalCleanup(ctx)
	if err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, err
	}
	if err := storage.RemoveStaged(); err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, err
	}
	return cleanup, nil
}

func (b *SourceAgentUpdateBridge) ensureStorageLocked() error {
	if b.storage != nil {
		return nil
	}
	storage, err := newSourceAgentUpdateBridgeStorage(b.config.UpdaterExecutable, b.config.WorkerType)
	if err != nil {
		return err
	}
	b.storage = storage
	return nil
}

func (b *SourceAgentUpdateBridge) requestMatchesCommand(request SourceAgentUpdateRequest, command SourceAgentCommand) bool {
	return request.CommandID == command.ID && request.ArtifactID == command.UpgradeSpec.ArtifactID &&
		request.WorkerType == b.config.WorkerType && request.CurrentVersion == b.config.CurrentVersion &&
		request.Platform == b.config.Platform && request.Architecture == b.config.Architecture &&
		request.ProtocolVersion == b.config.ProtocolVersion && request.StagedBinary == b.storage.StagedPath()
}

func sourceAgentUpdateWorkerBasename(workerType string) (string, bool) {
	switch workerType {
	case "wechat-worker":
		return "source-agent", true
	case "wcplus-worker":
		return "wcplus-agent", true
	case "chatlog-worker":
		return "chatlog-agent", true
	default:
		return "", false
	}
}
