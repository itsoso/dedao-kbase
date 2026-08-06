package app

import (
	"context"
	"errors"
	"os"
)

const (
	sourceAgentUpdatePendingFileName = "updater.pending"
	sourceAgentUpdatePendingSchema   = "source-agent-update-pending.v1"

	SourceAgentPendingUpdateActive          = "active"
	SourceAgentPendingUpdateCleanupComplete = "cleanup_complete"
)

type SourceAgentPendingUpdate struct {
	SchemaVersion      string `json:"schema_version"`
	CommandID          string `json:"command_id"`
	RequestFingerprint string `json:"request_fingerprint"`
	State              string `json:"state"`
}

func (s *FileSourceAgentUpdateReceiptStore) PublishPending(
	ctx context.Context,
	request SourceAgentUpdateRequest,
) error {
	if !validSourceAgentPendingUpdateRequest(request) {
		return ErrSourceAgentUpgradeCheckpointInvalid
	}
	pending := SourceAgentPendingUpdate{
		SchemaVersion:      sourceAgentUpdatePendingSchema,
		CommandID:          request.CommandID,
		RequestFingerprint: sourceAgentUpdateRequestFingerprint(request),
		State:              SourceAgentPendingUpdateActive,
	}
	payload, err := marshalSourceAgentUpdateJSON(pending)
	if err != nil {
		return ErrSourceAgentUpgradeCheckpointInvalid
	}
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	existing, found, err := s.LoadPending()
	if err != nil {
		return err
	}
	if found {
		if existing != pending {
			return ErrSourceAgentUpgradeCheckpointConflict
		}
		return nil
	}
	return s.directory.writeImmutable(sourceAgentUpdatePendingFileName, payload)
}

func (s *FileSourceAgentUpdateReceiptStore) LoadPending() (SourceAgentPendingUpdate, bool, error) {
	payload, err := s.directory.read(sourceAgentUpdatePendingFileName, sourceAgentUpdateReceiptMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return SourceAgentPendingUpdate{}, false, nil
	}
	var pending SourceAgentPendingUpdate
	if err != nil || decodeStrictSourceAgentUpdateJSON(payload, &pending) != nil ||
		!validSourceAgentPendingUpdate(pending) {
		return SourceAgentPendingUpdate{}, true, ErrSourceAgentUpgradeCheckpointInvalid
	}
	return pending, true, nil
}

func validSourceAgentPendingUpdateRequest(request SourceAgentUpdateRequest) bool {
	check := SourceAgentUpdateGuardCheck{
		CommandID: request.CommandID, ArtifactID: request.ArtifactID,
		WorkerType: request.WorkerType, CurrentVersion: request.CurrentVersion,
		Version: request.TargetVersion, Revision: request.Revision, Channel: request.Channel,
		Size: request.ExpectedSize, SHA256: request.ExpectedSHA256,
		Platform: request.Platform, Architecture: request.Architecture,
		ProtocolVersion: request.ProtocolVersion,
	}
	return validSourceAgentUpdateGuardCheck(check) && isCleanAbsoluteSourceAgentUpdatePath(request.StagedBinary)
}

func validSourceAgentPendingUpdate(pending SourceAgentPendingUpdate) bool {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", pending.CommandID, true)
	return pending.SchemaVersion == sourceAgentUpdatePendingSchema && err == nil && commandID == pending.CommandID &&
		isExactLowerHex(pending.RequestFingerprint, 64) &&
		(pending.State == SourceAgentPendingUpdateActive || pending.State == SourceAgentPendingUpdateCleanupComplete)
}

func (s *FileSourceAgentUpdateReceiptStore) MarkPendingCleanupComplete(
	ctx context.Context,
	commandID, requestFingerprint string,
) error {
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	pending, found, err := s.LoadPending()
	if err != nil || !found {
		if err != nil {
			return err
		}
		return ErrSourceAgentUpgradeCheckpointInvalid
	}
	if pending.CommandID != commandID || pending.RequestFingerprint != requestFingerprint {
		return ErrSourceAgentUpgradeCheckpointConflict
	}
	if pending.State == SourceAgentPendingUpdateCleanupComplete {
		return nil
	}
	pending.State = SourceAgentPendingUpdateCleanupComplete
	payload, err := marshalSourceAgentUpdateJSON(pending)
	if err != nil {
		return ErrSourceAgentUpgradeCheckpointInvalid
	}
	return s.directory.writeAtomic(sourceAgentUpdatePendingFileName, payload)
}

func (s *FileSourceAgentUpdateReceiptStore) ClearCompletedPending(
	ctx context.Context,
	commandID, requestFingerprint string,
) error {
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	pending, found, err := s.LoadPending()
	if err != nil || !found {
		return err
	}
	if pending.CommandID != commandID || pending.RequestFingerprint != requestFingerprint {
		return ErrSourceAgentUpgradeCheckpointConflict
	}
	if pending.State != SourceAgentPendingUpdateCleanupComplete {
		return ErrSourceAgentUpgradeCheckpointInvalid
	}
	return s.directory.remove(sourceAgentUpdatePendingFileName)
}

func (s *FileSourceAgentUpdateReceiptStore) FinishPendingCleanup(
	ctx context.Context,
	commandID, requestFingerprint string,
) error {
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	pending, found, err := s.LoadPending()
	if err != nil || !found {
		return err
	}
	if pending.CommandID != commandID || pending.RequestFingerprint != requestFingerprint {
		return ErrSourceAgentUpgradeCheckpointConflict
	}
	if pending.State != SourceAgentPendingUpdateCleanupComplete {
		return ErrSourceAgentUpgradeCheckpointInvalid
	}
	journal, journalFound, err := s.loadJournal()
	if err != nil {
		return err
	}
	if journalFound {
		if journal.CommandID != commandID || journal.RequestFingerprint != requestFingerprint ||
			journal.Stage != "terminal_cleanup" {
			return ErrSourceAgentUpgradeCheckpointConflict
		}
		if err := s.directory.remove(sourceAgentUpdateReceiptName("ready", journal.CommandID, journal.AttemptNonce)); err != nil {
			return err
		}
	}
	for _, name := range []string{
		sourceAgentUpdateHandoffFileName,
		sourceAgentUpdateReceiptName("outcome", commandID, ""),
		sourceAgentUpgradeTerminalObservationFileName,
		sourceAgentUpgradeCheckpointFileName,
		sourceAgentUpgradeTerminalResolutionFileName,
		sourceAgentUpdateJournalFileName,
	} {
		if err := s.directory.remove(name); err != nil {
			return err
		}
	}
	return s.directory.remove(sourceAgentUpdatePendingFileName)
}

func (s *FileSourceAgentUpdateReceiptStore) hasMatchingReady(journal sourceAgentUpdateJournal) (bool, error) {
	if !sourceAgentUpdateJournalStageIs(journal.Stage, "restart_requested", "restarted", "ready") {
		return false, nil
	}
	name := sourceAgentUpdateReceiptName("ready", journal.CommandID, journal.AttemptNonce)
	payload, err := s.directory.read(name, sourceAgentUpdateReceiptMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	var receipt SourceAgentReadyReceipt
	if err != nil || decodeStrictSourceAgentUpdateJSON(payload, &receipt) != nil || !validSourceAgentReadyReceipt(receipt) {
		return false, ErrSourceAgentReadyInvalid
	}
	expected := SourceAgentReadyExpectation{
		CommandID: journal.CommandID, AttemptNonce: journal.AttemptNonce,
		WorkerType: journal.WorkerType, Version: journal.TargetVersion,
		Platform: journal.Platform, Architecture: journal.Architecture,
		ProtocolVersion: journal.ProtocolVersion, Revision: journal.Revision,
	}
	if !sourceAgentReadyReceiptMatches(receipt, expected) {
		return false, ErrSourceAgentReadyMismatch
	}
	return true, nil
}
