package app

import (
	"context"
	"errors"
	"path/filepath"
)

func (u *SourceAgentUpdateTransaction) PrepareTerminalResolution(
	ctx context.Context,
	request SourceAgentUpdateRequest,
	resolution SourceAgentUpgradeTerminalResolution,
) error {
	u.lifecycle.RLock()
	defer u.lifecycle.RUnlock()
	if u.closed || u.validateRequest(request) != "" ||
		!validSourceAgentUpgradeTerminalResolution(resolution) ||
		resolution.CommandID != request.CommandID ||
		resolution.RequestFingerprint != sourceAgentUpdateRequestFingerprint(request) {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	release, err := u.fs.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	journal, found, err := u.receipts.loadJournal()
	if err != nil || !found || !sourceAgentUpdateJournalMatches(journal, request) {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	outcome, found, err := u.receipts.LoadOutcome(request.CommandID)
	if err != nil || !found || !sourceAgentUpdateOutcomeMatchesJournal(outcome, journal) ||
		outcome.Outcome != resolution.LocalOutcome || outcome.Code != resolution.LocalCode ||
		outcome.BinaryRestored != resolution.BinaryRestored {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}
	replaced, conclusive := sourceAgentTerminalReplacementEvidence(journal.Stage, outcome)
	if !conclusive || replaced != resolution.ReplacementOccurred {
		return ErrSourceAgentUpgradeTerminalResolutionInvalid
	}

	backupPath := filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupName())
	if resolution.Action == SourceAgentUpgradeTerminalRollback && journal.Stage != "terminal_cleanup" {
		if !replaced || !validSourceAgentBinaryIdentity(journal.Backup) {
			return ErrSourceAgentUpgradeTerminalResolutionInvalid
		}
		backupExists, err := u.fs.RegularFileExists(backupPath)
		if err != nil || !backupExists {
			return errors.New("source agent terminal rollback backup is unavailable")
		}
		if err := u.fs.RestoreExecutable(
			backupPath,
			u.config.CurrentExecutable,
			journal.AttemptNonce,
			journal.Backup,
		); err != nil {
			return errors.New("source agent terminal rollback failed")
		}
		if err := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable)); err != nil {
			return errors.New("source agent terminal rollback failed")
		}
		if err := u.restart(context.WithoutCancel(ctx), true); err != nil {
			return errors.New("source agent terminal rollback restart failed")
		}
	}
	if journal.Stage != "terminal_cleanup" {
		journal = u.advanceJournal(journal, "terminal_cleanup")
		if err := u.receipts.saveJournal(journal); err != nil {
			return err
		}
	}
	return u.cleanupDurableFilesRetainingJournal(journal, backupPath)
}

func (u *SourceAgentUpdateTransaction) cleanupDurableFilesRetainingJournal(
	journal sourceAgentUpdateJournal,
	backupPath string,
) error {
	for _, path := range []string{
		backupPath,
		filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupPendingName()),
		sourceAgentUpdatePreparedPath(u.config.CurrentExecutable, journal.AttemptNonce),
	} {
		if err := u.fs.Remove(path); err != nil {
			return err
		}
	}
	return u.fs.SyncDirectory(u.config.BackupRoot)
}
