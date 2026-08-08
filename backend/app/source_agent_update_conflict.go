package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
)

const (
	sourceAgentUpgradeConflictFileName = ".source-agent-upgrade-conflict.json"
	sourceAgentUpgradeConflictSchema   = "source-agent-upgrade-conflict.v1"

	sourceAgentUpgradeConflictDetected = "detected"
	sourceAgentUpgradeConflictRestored = "terminal_conflict_restored"
)

type SourceAgentUpgradeConflict struct {
	SchemaVersion      string `json:"schema_version"`
	CommandID          string `json:"command_id"`
	CommandFingerprint string `json:"command_fingerprint"`
	Reason             string `json:"reason"`
	State              string `json:"state"`
}

func (s *FileSourceAgentUpdateReceiptStore) RecordAuthenticatedUpgradeConflict(
	ctx context.Context,
	commandID, commandFingerprint string,
) error {
	marker := SourceAgentUpgradeConflict{
		SchemaVersion: sourceAgentUpgradeConflictSchema,
		CommandID:     commandID, CommandFingerprint: commandFingerprint,
		Reason: SourceAgentUpgradeResolutionFingerprintConflict,
		State:  sourceAgentUpgradeConflictDetected,
	}
	if !validSourceAgentUpgradeConflict(marker) {
		return ErrSourceAgentUpgradeCheckpointInvalid
	}
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	checkpoint, found, err := s.LoadUpgradeCommandCheckpoint()
	if err != nil || !found || checkpoint.Command.ID != marker.CommandID || checkpoint.Fingerprint != marker.CommandFingerprint {
		return ErrSourceAgentUpgradeCheckpointConflict
	}
	existing, found, err := s.LoadUpgradeConflict()
	if err != nil {
		return err
	}
	if found {
		if existing.CommandID != marker.CommandID || existing.CommandFingerprint != marker.CommandFingerprint ||
			existing.Reason != marker.Reason {
			return ErrSourceAgentUpgradeCheckpointConflict
		}
		return nil
	}
	payload, err := marshalSourceAgentUpdateJSON(marker)
	if err != nil {
		return ErrSourceAgentUpgradeCheckpointInvalid
	}
	return s.directory.writeImmutable(sourceAgentUpgradeConflictFileName, payload)
}

func (s *FileSourceAgentUpdateReceiptStore) LoadUpgradeConflict() (SourceAgentUpgradeConflict, bool, error) {
	payload, err := s.directory.read(sourceAgentUpgradeConflictFileName, sourceAgentUpdateReceiptMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return SourceAgentUpgradeConflict{}, false, nil
	}
	var marker SourceAgentUpgradeConflict
	if err != nil || decodeStrictSourceAgentUpdateJSON(payload, &marker) != nil || !validSourceAgentUpgradeConflict(marker) {
		return SourceAgentUpgradeConflict{}, true, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	return marker, true, nil
}

func (s *FileSourceAgentUpdateReceiptStore) ResolveUpgradeConflict(
	ctx context.Context,
) (SourceAgentUpgradeTerminalResolution, error) {
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return SourceAgentUpgradeTerminalResolution{}, err
	}
	defer release()
	marker, found, err := s.LoadUpgradeConflict()
	if err != nil || !found || marker.State != sourceAgentUpgradeConflictDetected {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	checkpoint, found, err := s.LoadUpgradeCommandCheckpoint()
	if err != nil || !found || checkpoint.Command.ID != marker.CommandID || checkpoint.Fingerprint != marker.CommandFingerprint {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	journal, found, err := s.loadJournal()
	if err != nil || !found || journal.CommandID != marker.CommandID {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	outcome, found, err := s.LoadOutcome(marker.CommandID)
	if err != nil || !found || !sourceAgentUpdateOutcomeMatchesJournal(outcome, journal) ||
		outcome.Code == SourceAgentCommandCodeRollbackFailed {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	replaced, conclusive := sourceAgentTerminalReplacementEvidence(journal.Stage, outcome)
	if !conclusive || !replaced {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	resolution := SourceAgentUpgradeTerminalResolution{
		SchemaVersion: sourceAgentUpgradeTerminalResolutionSchema,
		Reason:        SourceAgentUpgradeResolutionFingerprintConflict,
		CommandID:     marker.CommandID, CommandFingerprint: marker.CommandFingerprint,
		RequestFingerprint: journal.RequestFingerprint,
		Action:             SourceAgentUpgradeTerminalRollback,
		LocalOutcome:       outcome.Outcome, LocalCode: outcome.Code,
		LocalRuntimeVersion: outcome.RuntimeVersion, BinaryRestored: outcome.BinaryRestored,
		ReplacementOccurred: true,
	}
	if !validSourceAgentUpgradeTerminalResolution(resolution) {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	payload, err := marshalSourceAgentUpdateJSON(resolution)
	if err != nil {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	if err := s.directory.writeImmutable(sourceAgentUpgradeTerminalResolutionFileName, payload); err != nil {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	return resolution, nil
}

func (s *FileSourceAgentUpdateReceiptStore) MarkUpgradeConflictRestored(
	ctx context.Context,
	resolution SourceAgentUpgradeTerminalResolution,
) error {
	if resolution.Reason != SourceAgentUpgradeResolutionFingerprintConflict ||
		resolution.Action != SourceAgentUpgradeTerminalRollback || !resolution.ReplacementOccurred {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	marker, found, err := s.LoadUpgradeConflict()
	if err != nil || !found || marker.CommandID != resolution.CommandID ||
		marker.CommandFingerprint != resolution.CommandFingerprint {
		return ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	if marker.State == sourceAgentUpgradeConflictRestored {
		return nil
	}
	journal, found, err := s.loadJournal()
	if err != nil || !found || journal.CommandID != marker.CommandID || journal.Stage != "terminal_cleanup" ||
		journal.RequestFingerprint != resolution.RequestFingerprint {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	marker.State = sourceAgentUpgradeConflictRestored
	payload, err := marshalSourceAgentUpdateJSON(marker)
	if err != nil {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	return s.directory.writeAtomic(sourceAgentUpgradeConflictFileName, payload)
}

func (s *FileSourceAgentUpdateReceiptStore) FinishUpgradeConflictCleanup(
	ctx context.Context,
	commandID, requestFingerprint string,
) error {
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	pending, found, err := s.LoadPending()
	if err != nil || !found || pending.CommandID != commandID ||
		pending.RequestFingerprint != requestFingerprint || pending.State != SourceAgentPendingUpdateCleanupComplete {
		return ErrSourceAgentUpgradeCheckpointConflict
	}
	marker, found, err := s.LoadUpgradeConflict()
	if err != nil || !found || marker.CommandID != commandID || marker.State != sourceAgentUpgradeConflictRestored {
		return ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	checkpoint, found, err := s.LoadUpgradeCommandCheckpoint()
	if err != nil || !found || checkpoint.Command.ID != commandID ||
		checkpoint.Fingerprint != marker.CommandFingerprint {
		return ErrSourceAgentUpgradeCheckpointConflict
	}
	journal, found, err := s.loadJournal()
	if err != nil || !found || journal.CommandID != commandID ||
		journal.RequestFingerprint != requestFingerprint || journal.Stage != "terminal_cleanup" {
		return ErrSourceAgentUpgradeCheckpointConflict
	}
	if err := s.directory.remove(sourceAgentUpdateReceiptName("ready", journal.CommandID, journal.AttemptNonce)); err != nil {
		return err
	}
	for _, name := range []string{
		sourceAgentUpdateHandoffFileName,
		sourceAgentUpdateReceiptName("outcome", commandID, ""),
		sourceAgentUpgradeTerminalResolutionFileName,
		sourceAgentUpdateJournalFileName,
		sourceAgentUpdatePendingFileName,
	} {
		if err := s.directory.remove(name); err != nil {
			return err
		}
	}
	return nil
}

func validSourceAgentUpgradeConflict(marker SourceAgentUpgradeConflict) bool {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", marker.CommandID, true)
	return marker.SchemaVersion == sourceAgentUpgradeConflictSchema && err == nil && commandID == marker.CommandID &&
		isExactLowerHex(marker.CommandFingerprint, sha256.Size*2) &&
		marker.Reason == SourceAgentUpgradeResolutionFingerprintConflict &&
		(marker.State == sourceAgentUpgradeConflictDetected || marker.State == sourceAgentUpgradeConflictRestored)
}
