package app

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

type SourceAgentPendingUpdateRunnerConfig struct {
	UpdaterExecutable string
	WorkerType        string
	Guard             SourceAgentUpdateGuard
	ProcessControl    SourceAgentUpdateProcessControl
	PollInterval      time.Duration
}

func RunSourceAgentPendingUpdate(
	ctx context.Context,
	config SourceAgentPendingUpdateRunnerConfig,
) (returnErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	workerName, ok := sourceAgentUpdateWorkerBasename(config.WorkerType)
	if !ok || filepath.Base(config.UpdaterExecutable) != sourceAgentUpdateUpdaterBasename ||
		!isCleanAbsoluteSourceAgentUpdatePath(config.UpdaterExecutable) ||
		config.Guard == nil || config.ProcessControl == nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	installRoot := filepath.Dir(config.UpdaterExecutable)
	currentExecutable := filepath.Join(installRoot, workerName)
	storage, err := newSourceAgentUpdateBridgeStorage(config.UpdaterExecutable, config.WorkerType)
	if err != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	storageOpen := true
	defer func() {
		if storageOpen {
			returnErr = errors.Join(returnErr, storage.Close())
		}
	}()
	releaseLifecycle, err := storage.AcquireLifecycleShared()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, releaseLifecycle()) }()
	receiptRoot := filepath.Join(installRoot, sourceAgentUpdateHandoffDirectoryName)
	receipts, err := NewFileSourceAgentUpdateReceiptStore(receiptRoot, config.PollInterval)
	if err != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	defer func() { returnErr = errors.Join(returnErr, receipts.Close()) }()

	pending, found, err := receipts.LoadPending()
	if err != nil || !found {
		return ErrSourceAgentUpgradeCheckpointInvalid
	}
	if pending.State == SourceAgentPendingUpdateCleanupComplete {
		if err := storage.Close(); err != nil {
			return err
		}
		storageOpen = false
		if resolution, found, err := receipts.LoadUpgradeTerminalResolution(); err != nil {
			return err
		} else if found && resolution.Reason == SourceAgentUpgradeResolutionFingerprintConflict {
			return receipts.FinishUpgradeConflictCleanup(ctx, pending.CommandID, pending.RequestFingerprint)
		}
		return receipts.FinishPendingCleanup(ctx, pending.CommandID, pending.RequestFingerprint)
	}
	request, err := loadSourceAgentPendingRequest(storage)
	if err != nil || pending.CommandID != request.CommandID ||
		pending.RequestFingerprint != sourceAgentUpdateRequestFingerprint(request) {
		return ErrSourceAgentUpgradeCheckpointConflict
	}
	if err := storage.Close(); err != nil {
		return err
	}
	storageOpen = false

	transaction, err := NewSourceAgentUpdateTransaction(SourceAgentUpdateConfig{
		WorkerType: config.WorkerType, Platform: request.Platform, Architecture: request.Architecture,
		CurrentVersion: request.CurrentVersion, ProtocolVersion: request.ProtocolVersion,
		CurrentExecutable: currentExecutable,
		StagingRoot:       filepath.Join(installRoot, sourceAgentUpdateStagingDirectoryName),
		BackupRoot:        installRoot,
		ReceiptRoot:       receiptRoot,
		Guard:             config.Guard,
		ProcessControl:    config.ProcessControl,
		Receipts:          receipts,
	})
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, transaction.Close()) }()
	result := transaction.Apply(ctx, request)
	if result.PersistenceCode != "" {
		return errors.New("source agent pending update outcome is not durable")
	}
	stored, outcomeFound, err := receipts.LoadOutcome(request.CommandID)
	if err != nil || !outcomeFound || stored != result {
		return errors.New("source agent pending update outcome is unavailable")
	}

	poll := config.PollInterval
	if poll <= 0 {
		poll = sourceAgentUpdateDefaultPoll
	}
	for {
		if marker, found, err := receipts.LoadUpgradeConflict(); err != nil {
			return err
		} else if found && marker.State == sourceAgentUpgradeConflictDetected {
			if _, err := receipts.ResolveUpgradeConflict(ctx); err != nil {
				return err
			}
		}
		resolution, resolutionFound, err := receipts.LoadUpgradeTerminalResolution()
		if err != nil {
			return err
		}
		if resolutionFound {
			if err := transaction.PrepareTerminalResolution(ctx, request, resolution); err != nil {
				return err
			}
			if err := removeSourceAgentPendingStaged(config.UpdaterExecutable, config.WorkerType); err != nil {
				return err
			}
			if resolution.Reason == SourceAgentUpgradeResolutionFingerprintConflict {
				if err := receipts.MarkUpgradeConflictRestored(ctx, resolution); err != nil {
					return err
				}
			}
			if err := receipts.MarkPendingCleanupComplete(ctx, request.CommandID, pending.RequestFingerprint); err != nil {
				return err
			}
			if resolution.Reason == SourceAgentUpgradeResolutionFingerprintConflict {
				return receipts.FinishUpgradeConflictCleanup(ctx, request.CommandID, pending.RequestFingerprint)
			}
			return receipts.FinishPendingCleanup(ctx, request.CommandID, pending.RequestFingerprint)
		}
		if err := waitSourceAgentUpdatePoll(ctx, time.Hour, poll); err != nil {
			return err
		}
	}
}

func removeSourceAgentPendingStaged(updaterExecutable, workerType string) (returnErr error) {
	storage, err := newSourceAgentUpdateBridgeStorage(updaterExecutable, workerType)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, storage.Close()) }()
	if err := storage.Lock(); err != nil {
		return err
	}
	defer func() {
		if err := storage.Unlock(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	return storage.RemoveStaged()
}

func loadSourceAgentPendingRequest(storage sourceAgentUpdateBridgeStorage) (
	request SourceAgentUpdateRequest,
	returnErr error,
) {
	if err := storage.Lock(); err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	defer func() {
		if err := storage.Unlock(); err != nil {
			request, returnErr = SourceAgentUpdateRequest{}, err
		}
	}()
	payload, found, err := storage.LoadHandoff()
	if err != nil || !found {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	request, err = sourceAgentUpdateRequestFromHandoff(payload, storage.StagedPath())
	if err != nil || storage.VerifyStaged(request.ExpectedSize, request.ExpectedSHA256) != nil {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	return request, nil
}
