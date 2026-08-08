package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
)

const (
	SourceAgentUpgradeTerminalAcknowledge = "acknowledge"
	SourceAgentUpgradeTerminalRollback    = "rollback"

	SourceAgentUpgradeResolutionServerTerminal      = "server_terminal"
	SourceAgentUpgradeResolutionFingerprintConflict = "server_fingerprint_conflict"

	sourceAgentUpgradeTerminalResolutionFileName = ".source-agent-upgrade-terminal-resolution.json"
	sourceAgentUpgradeTerminalResolutionSchema   = "source-agent-upgrade-terminal-resolution.v1"
)

var (
	ErrSourceAgentUpgradeTerminalResolutionInvalid  = errors.New("source agent upgrade terminal resolution is invalid")
	ErrSourceAgentUpgradeTerminalResolutionConflict = errors.New("source agent upgrade terminal resolution conflicts")
)

// SourceAgentUpgradeTerminalResolution is bounded local evidence. It does not
// perform or authorize arbitrary work: acknowledge permits a later exact
// cleanup transaction, while rollback requires the fixed updater recovery
// path to restore safety before cleanup can be considered.
type SourceAgentUpgradeTerminalResolution struct {
	SchemaVersion       string `json:"schema_version"`
	Reason              string `json:"reason"`
	CommandID           string `json:"command_id"`
	CommandFingerprint  string `json:"command_fingerprint"`
	RequestFingerprint  string `json:"request_fingerprint"`
	Action              string `json:"action"`
	ServerState         string `json:"server_state"`
	ServerResultCode    string `json:"server_result_code"`
	ServerActualVersion string `json:"server_actual_version,omitempty"`
	LocalOutcome        string `json:"local_outcome"`
	LocalCode           string `json:"local_code"`
	LocalRuntimeVersion string `json:"local_runtime_version"`
	BinaryRestored      bool   `json:"binary_restored,omitempty"`
	ReplacementOccurred bool   `json:"replacement_occurred"`
}

// ResolveUpgradeTerminalObservation compares the immutable server terminal
// observation with the protected command checkpoint and exact local updater
// journal/outcome. It only persists a resolution when all evidence agrees.
func (s *FileSourceAgentUpdateReceiptStore) ResolveUpgradeTerminalObservation(
	ctx context.Context,
) (SourceAgentUpgradeTerminalResolution, error) {
	if err := ctx.Err(); err != nil {
		return SourceAgentUpgradeTerminalResolution{}, err
	}
	release, err := s.directory.acquire(ctx)
	if err != nil {
		return SourceAgentUpgradeTerminalResolution{}, err
	}
	defer release()

	observation, found, err := s.loadUpgradeTerminalObservation()
	if err != nil || !found {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	checkpoint, found, err := s.LoadUpgradeCommandCheckpoint()
	if err != nil || !found {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	if observation.CommandID != checkpoint.Command.ID || observation.Fingerprint != checkpoint.Fingerprint {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionConflict
	}
	journal, found, err := s.loadJournal()
	if err != nil || !found || journal.CommandID != observation.CommandID {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	outcome, found, err := s.LoadOutcome(observation.CommandID)
	if err != nil || !found || !sourceAgentUpdateOutcomeMatchesJournal(outcome, journal) {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	if outcome.Code == SourceAgentCommandCodeRollbackFailed {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	replacementOccurred, conclusive := sourceAgentTerminalReplacementEvidence(journal.Stage, outcome)
	if !conclusive {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	action, ok := sourceAgentTerminalResolutionAction(observation, outcome, replacementOccurred)
	if !ok {
		return SourceAgentUpgradeTerminalResolution{}, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	resolution := SourceAgentUpgradeTerminalResolution{
		SchemaVersion: sourceAgentUpgradeTerminalResolutionSchema,
		Reason:        SourceAgentUpgradeResolutionServerTerminal,
		CommandID:     observation.CommandID, CommandFingerprint: observation.Fingerprint,
		RequestFingerprint: journal.RequestFingerprint, Action: action,
		ServerState: observation.State, ServerResultCode: observation.ResultCode,
		ServerActualVersion: observation.ActualVersion,
		LocalOutcome:        outcome.Outcome, LocalCode: outcome.Code, BinaryRestored: outcome.BinaryRestored,
		LocalRuntimeVersion: outcome.RuntimeVersion,
		ReplacementOccurred: replacementOccurred,
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

// LoadUpgradeTerminalResolution exposes only the strict bounded resolution to
// the updater. Corrupt or non-canonical state is never treated as permission.
func (s *FileSourceAgentUpdateReceiptStore) LoadUpgradeTerminalResolution() (
	SourceAgentUpgradeTerminalResolution,
	bool,
	error,
) {
	payload, err := s.directory.read(sourceAgentUpgradeTerminalResolutionFileName, sourceAgentUpdateReceiptMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return SourceAgentUpgradeTerminalResolution{}, false, nil
	}
	var resolution SourceAgentUpgradeTerminalResolution
	if err != nil || decodeStrictSourceAgentUpdateJSON(payload, &resolution) != nil ||
		!validSourceAgentUpgradeTerminalResolution(resolution) || !s.upgradeTerminalResolutionMatchesEvidence(resolution) {
		return SourceAgentUpgradeTerminalResolution{}, true, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	return resolution, true, nil
}

func (s *FileSourceAgentUpdateReceiptStore) upgradeTerminalResolutionMatchesEvidence(
	resolution SourceAgentUpgradeTerminalResolution,
) bool {
	var observation SourceAgentUpgradeTerminalObservation
	if resolution.Reason == SourceAgentUpgradeResolutionServerTerminal {
		var found bool
		var err error
		observation, found, err = s.loadUpgradeTerminalObservation()
		if err != nil || !found || observation.CommandID != resolution.CommandID ||
			observation.Fingerprint != resolution.CommandFingerprint || observation.State != resolution.ServerState ||
			observation.ResultCode != resolution.ServerResultCode || observation.ActualVersion != resolution.ServerActualVersion {
			return false
		}
	} else {
		conflict, found, err := s.LoadUpgradeConflict()
		if err != nil || !found || conflict.CommandID != resolution.CommandID ||
			conflict.CommandFingerprint != resolution.CommandFingerprint ||
			conflict.Reason != SourceAgentUpgradeResolutionFingerprintConflict {
			return false
		}
	}
	checkpoint, found, err := s.LoadUpgradeCommandCheckpoint()
	if err != nil || !found || checkpoint.Command.ID != resolution.CommandID ||
		checkpoint.Fingerprint != resolution.CommandFingerprint {
		return false
	}
	journal, found, err := s.loadJournal()
	if err != nil || !found || journal.CommandID != resolution.CommandID ||
		journal.RequestFingerprint != resolution.RequestFingerprint {
		return false
	}
	outcome, found, err := s.LoadOutcome(resolution.CommandID)
	if err != nil || !found || !sourceAgentUpdateOutcomeMatchesJournal(outcome, journal) ||
		outcome.Code == SourceAgentCommandCodeRollbackFailed || outcome.Outcome != resolution.LocalOutcome ||
		outcome.Code != resolution.LocalCode || outcome.BinaryRestored != resolution.BinaryRestored {
		return false
	}
	if outcome.RuntimeVersion != resolution.LocalRuntimeVersion {
		return false
	}
	replacementOccurred, conclusive := sourceAgentTerminalReplacementEvidence(journal.Stage, outcome)
	if !conclusive || replacementOccurred != resolution.ReplacementOccurred {
		return false
	}
	if resolution.Reason == SourceAgentUpgradeResolutionFingerprintConflict {
		return replacementOccurred && resolution.Action == SourceAgentUpgradeTerminalRollback
	}
	action, ok := sourceAgentTerminalResolutionAction(observation, outcome, replacementOccurred)
	return ok && action == resolution.Action
}

func (s *FileSourceAgentUpdateReceiptStore) loadUpgradeTerminalObservation() (
	SourceAgentUpgradeTerminalObservation,
	bool,
	error,
) {
	payload, err := s.directory.read(sourceAgentUpgradeTerminalObservationFileName, sourceAgentUpdateReceiptMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return SourceAgentUpgradeTerminalObservation{}, false, nil
	}
	var observation SourceAgentUpgradeTerminalObservation
	if err != nil || decodeStrictSourceAgentUpdateJSON(payload, &observation) != nil ||
		!validSourceAgentUpgradeTerminalObservation(observation) {
		return SourceAgentUpgradeTerminalObservation{}, true, ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	return observation, true, nil
}

func sourceAgentTerminalReplacementEvidence(stage string, outcome SourceAgentUpdateResult) (bool, bool) {
	switch stage {
	case "started", "backup_durable":
		return false, true
	case "replaced", "restart_requested", "restarted", "ready", "rollback_restored":
		return true, true
	case "terminal_cleanup":
		// This legacy phase does not itself reveal whether replacement happened.
		// Only outcomes that prove replacement/restoration make it conclusive.
		if outcome.Outcome == SourceAgentUpdateOutcomeSucceeded ||
			outcome.Outcome == SourceAgentUpdateOutcomeRolledBack || outcome.Code == SourceAgentCommandCodeRollbackFailed {
			return true, true
		}
	}
	return false, false
}

func sourceAgentTerminalResolutionAction(
	observation SourceAgentUpgradeTerminalObservation,
	outcome SourceAgentUpdateResult,
	replacementOccurred bool,
) (string, bool) {
	switch observation.State {
	case SourceAgentCommandSucceeded:
		if outcome.Outcome == SourceAgentUpdateOutcomeSucceeded && outcome.Code == SourceAgentCommandCodeUpgradeComplete &&
			observation.ActualVersion == outcome.RuntimeVersion {
			return SourceAgentUpgradeTerminalAcknowledge, true
		}
	case SourceAgentCommandRolledBack:
		if outcome.Outcome == SourceAgentUpdateOutcomeRolledBack && outcome.Code == SourceAgentCommandCodeRollbackComplete && outcome.BinaryRestored {
			return SourceAgentUpgradeTerminalAcknowledge, true
		}
	case SourceAgentCommandFailed, SourceAgentCommandExpired, SourceAgentCommandCanceled:
		if !replacementOccurred && outcome.Outcome == SourceAgentUpdateOutcomeFailed && !outcome.BinaryRestored {
			return SourceAgentUpgradeTerminalAcknowledge, true
		}
	}
	if replacementOccurred {
		return SourceAgentUpgradeTerminalRollback, true
	}
	return "", false
}

func validSourceAgentUpgradeTerminalResolution(value SourceAgentUpgradeTerminalResolution) bool {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", value.CommandID, true)
	if value.SchemaVersion != sourceAgentUpgradeTerminalResolutionSchema || err != nil || commandID != value.CommandID ||
		!isExactLowerHex(value.CommandFingerprint, sha256.Size*2) ||
		!isExactLowerHex(value.RequestFingerprint, sha256.Size*2) ||
		(value.Action != SourceAgentUpgradeTerminalAcknowledge && value.Action != SourceAgentUpgradeTerminalRollback) ||
		!validSourceAgentTerminalLocalSummary(value) ||
		value.LocalCode == SourceAgentCommandCodeRollbackFailed ||
		(value.LocalOutcome == SourceAgentUpdateOutcomeFailed && value.ReplacementOccurred) {
		return false
	}
	if value.Reason == SourceAgentUpgradeResolutionFingerprintConflict {
		return value.Action == SourceAgentUpgradeTerminalRollback && value.ReplacementOccurred &&
			value.ServerState == "" && value.ServerResultCode == "" && value.ServerActualVersion == ""
	}
	if value.Reason != SourceAgentUpgradeResolutionServerTerminal {
		return false
	}
	observation := SourceAgentUpgradeTerminalObservation{
		SchemaVersion: sourceAgentUpgradeTerminalObservationSchema,
		CommandID:     value.CommandID, Fingerprint: value.CommandFingerprint,
		State: value.ServerState, ResultCode: value.ServerResultCode, ActualVersion: value.ServerActualVersion,
	}
	if !validSourceAgentUpgradeTerminalObservation(observation) {
		return false
	}
	expected, ok := sourceAgentTerminalResolutionAction(observation, SourceAgentUpdateResult{
		Outcome: value.LocalOutcome, Code: value.LocalCode, RuntimeVersion: value.LocalRuntimeVersion,
		BinaryRestored: value.BinaryRestored,
	}, value.ReplacementOccurred)
	return ok && expected == value.Action
}

func validSourceAgentTerminalLocalSummary(value SourceAgentUpgradeTerminalResolution) bool {
	if !isSourceAgentArtifactVersion(value.LocalRuntimeVersion) {
		return false
	}
	switch value.LocalOutcome {
	case SourceAgentUpdateOutcomeSucceeded:
		return value.LocalCode == SourceAgentCommandCodeUpgradeComplete && !value.BinaryRestored
	case SourceAgentUpdateOutcomeRolledBack:
		return value.LocalCode == SourceAgentCommandCodeRollbackComplete && value.BinaryRestored
	case SourceAgentUpdateOutcomeFailed:
		switch value.LocalCode {
		case SourceAgentCommandCodeUpgradeFailed, SourceAgentCommandCodeDownloadFailed,
			SourceAgentCommandCodeVerificationFailed, SourceAgentCommandCodeInstallFailed,
			SourceAgentCommandCodeRestartFailed, SourceAgentUpdateCodeCanceled:
			return !value.BinaryRestored
		}
	}
	return false
}
