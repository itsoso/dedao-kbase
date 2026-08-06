package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
)

const (
	sourceAgentUpgradeNoAttemptCleanupFileName = ".source-agent-upgrade-no-attempt-cleanup.json"
	sourceAgentUpgradeNoAttemptCleanupSchema   = "source-agent-upgrade-no-attempt-cleanup.v1"
)

type sourceAgentUpgradeNoAttemptCleanup struct {
	SchemaVersion      string `json:"schema_version"`
	CommandID          string `json:"command_id"`
	CommandFingerprint string `json:"command_fingerprint"`
	ServerState        string `json:"server_state"`
	ServerResultCode   string `json:"server_result_code"`
}

// prepareNoAttemptTerminalCleanup persists narrowly scoped permission to
// remove fixed preparation files only when the updater has provably never
// started. It deliberately excludes successful and rolled-back commands,
// which always require an exact local updater outcome.
func (s *FileSourceAgentUpdateReceiptStore) prepareNoAttemptTerminalCleanup(
	ctx context.Context,
) (sourceAgentUpgradeNoAttemptCleanup, error) {
	if err := ctx.Err(); err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, err
	}
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, err
	}
	defer release()

	observation, found, err := s.loadUpgradeTerminalObservation()
	if err != nil || !found || !sourceAgentNoAttemptServerTerminal(observation) {
		return sourceAgentUpgradeNoAttemptCleanup{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	checkpoint, found, err := s.LoadUpgradeCommandCheckpoint()
	if err != nil || !found {
		return sourceAgentUpgradeNoAttemptCleanup{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	if observation.CommandID != checkpoint.Command.ID || observation.Fingerprint != checkpoint.Fingerprint {
		return sourceAgentUpgradeNoAttemptCleanup{}, ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	if err := s.requireNoUpdaterAttemptEvidence(observation.CommandID); err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, err
	}
	cleanup := sourceAgentUpgradeNoAttemptCleanup{
		SchemaVersion: sourceAgentUpgradeNoAttemptCleanupSchema,
		CommandID:     observation.CommandID, CommandFingerprint: observation.Fingerprint,
		ServerState: observation.State, ServerResultCode: observation.ResultCode,
	}
	if !validSourceAgentUpgradeNoAttemptCleanup(cleanup) {
		return sourceAgentUpgradeNoAttemptCleanup{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	existing, found, err := s.loadNoAttemptTerminalCleanup()
	if err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, err
	}
	if found {
		if existing != cleanup {
			return sourceAgentUpgradeNoAttemptCleanup{}, ErrSourceAgentUpgradeTerminalResolutionConflict
		}
		return cleanup, nil
	}
	payload, err := marshalSourceAgentUpdateJSON(cleanup)
	if err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	if err := s.directory.writeImmutable(sourceAgentUpgradeNoAttemptCleanupFileName, payload); err != nil {
		return sourceAgentUpgradeNoAttemptCleanup{}, ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	return cleanup, nil
}

func (s *FileSourceAgentUpdateReceiptStore) finishNoAttemptTerminalCleanup(
	ctx context.Context,
	want sourceAgentUpgradeNoAttemptCleanup,
) error {
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	cleanup, found, err := s.loadNoAttemptTerminalCleanup()
	if err != nil || !found || cleanup != want {
		return ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	observation, found, err := s.loadUpgradeTerminalObservation()
	if err != nil || !found || observation.CommandID != cleanup.CommandID ||
		observation.Fingerprint != cleanup.CommandFingerprint || observation.State != cleanup.ServerState ||
		observation.ResultCode != cleanup.ServerResultCode {
		return ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	checkpoint, found, err := s.LoadUpgradeCommandCheckpoint()
	if err != nil || !found || checkpoint.Command.ID != cleanup.CommandID ||
		checkpoint.Fingerprint != cleanup.CommandFingerprint {
		return ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	if err := s.requireNoUpdaterAttemptEvidence(cleanup.CommandID); err != nil {
		return err
	}
	for _, name := range []string{
		sourceAgentUpdateHandoffFileName,
		sourceAgentUpgradeTerminalObservationFileName,
		sourceAgentUpgradeNoAttemptCleanupFileName,
		sourceAgentUpgradeCheckpointFileName,
	} {
		if err := s.directory.remove(name); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileSourceAgentUpdateReceiptStore) requireNoUpdaterAttemptEvidence(commandID string) error {
	if _, found, err := s.LoadPending(); err != nil || found {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	if _, found, err := s.loadJournal(); err != nil || found {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	if _, found, err := s.LoadOutcome(commandID); err != nil || found {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	if _, found, err := s.LoadUpgradeTerminalResolution(); err != nil || found {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	return nil
}

func (s *FileSourceAgentUpdateReceiptStore) loadNoAttemptTerminalCleanup() (
	sourceAgentUpgradeNoAttemptCleanup,
	bool,
	error,
) {
	payload, err := s.directory.read(sourceAgentUpgradeNoAttemptCleanupFileName, sourceAgentUpdateReceiptMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return sourceAgentUpgradeNoAttemptCleanup{}, false, nil
	}
	var cleanup sourceAgentUpgradeNoAttemptCleanup
	if err != nil || decodeStrictSourceAgentUpdateJSON(payload, &cleanup) != nil ||
		!validSourceAgentUpgradeNoAttemptCleanup(cleanup) {
		return sourceAgentUpgradeNoAttemptCleanup{}, true, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	return cleanup, true, nil
}

func sourceAgentNoAttemptServerTerminal(observation SourceAgentUpgradeTerminalObservation) bool {
	switch observation.State {
	case SourceAgentCommandFailed, SourceAgentCommandCanceled, SourceAgentCommandExpired:
		return true
	default:
		return false
	}
}

func validSourceAgentUpgradeNoAttemptCleanup(cleanup sourceAgentUpgradeNoAttemptCleanup) bool {
	observation := SourceAgentUpgradeTerminalObservation{
		SchemaVersion: sourceAgentUpgradeTerminalObservationSchema,
		CommandID:     cleanup.CommandID, Fingerprint: cleanup.CommandFingerprint,
		State: cleanup.ServerState, ResultCode: cleanup.ServerResultCode,
	}
	return cleanup.SchemaVersion == sourceAgentUpgradeNoAttemptCleanupSchema &&
		isExactLowerHex(cleanup.CommandFingerprint, sha256.Size*2) &&
		validSourceAgentUpgradeTerminalObservation(observation) && sourceAgentNoAttemptServerTerminal(observation)
}
