package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSourceAgentUpdateRecoversEveryInterruptedPostBackupStage(t *testing.T) {
	for _, stage := range []string{
		sourceAgentUpdateFaultAfterBackup,
		sourceAgentUpdateFaultAfterReplace,
		sourceAgentUpdateFaultAfterRestart,
		sourceAgentUpdateFaultAfterReady,
		sourceAgentUpdateFaultBeforeOutcome,
	} {
		t.Run(stage, func(t *testing.T) {
			fixture := newDurableSourceAgentUpdateFixture(t, stage)
			firstResult := applyDurableSourceAgentUpdate(t, fixture)
			if firstResult.Code != SourceAgentUpdateCodeInterrupted {
				t.Fatalf("first result=%#v", firstResult)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())); err != nil {
				t.Fatalf("durable backup missing after interruption: %v", err)
			}

			retry, err := NewSourceAgentUpdateTransaction(fixture.config)
			if err != nil {
				t.Fatal(err)
			}
			recovered := retry.Apply(context.Background(), fixture.request)
			if recovered.Outcome != SourceAgentUpdateOutcomeRolledBack || !recovered.BinaryRestored {
				t.Fatalf("recovered=%#v", recovered)
			}
			assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
			journal, found, err := fixture.store.loadJournal()
			if err != nil || !found || journal.Stage != "rollback_restored" {
				t.Fatalf("retained journal=%#v found=%v err=%v", journal, found, err)
			}
			assertSourceAgentExecutable(t, filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName()), fixture.oldBinary)
		})
	}
}

func TestSourceAgentUpdateAttemptNoncePreventsOldReadyReplay(t *testing.T) {
	fixture := newDurableSourceAgentUpdateFixture(t, sourceAgentUpdateFaultAfterReady)
	first := applyDurableSourceAgentUpdate(t, fixture)
	if first.Code != SourceAgentUpdateCodeInterrupted {
		t.Fatalf("first=%#v", first)
	}
	journal, found, err := fixture.store.loadJournal()
	if err != nil || !found || len(journal.AttemptNonce) != sha256.Size*2 {
		t.Fatalf("journal=%#v found=%v err=%v", journal, found, err)
	}
	for _, character := range journal.AttemptNonce {
		if !strings.ContainsRune("0123456789abcdef", character) {
			t.Fatalf("nonce is not lowercase hex: %q", journal.AttemptNonce)
		}
	}
	challenge, err := fixture.store.ReadyChallenge(fixture.request.CommandID)
	if err != nil || challenge.AttemptNonce != journal.AttemptNonce {
		t.Fatalf("challenge=%#v err=%v", challenge, err)
	}

	retry, err := NewSourceAgentUpdateTransaction(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	recovered := retry.Apply(context.Background(), fixture.request)
	if recovered.Outcome != SourceAgentUpdateOutcomeRolledBack || recovered.Outcome == SourceAgentUpdateOutcomeSucceeded {
		t.Fatalf("old ready was replayed: %#v", recovered)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
}

func TestSourceAgentUpdateFinalGuardRunsAfterDurableBackup(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.fs.afterBackup = func() { fixture.guard.failCall = 2 }
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Code != SourceAgentCommandCodeInstallFailed || fixture.guard.calls != 2 || fixture.process.calls != 0 {
		t.Fatalf("result=%#v guard=%d process=%d", result, fixture.guard.calls, fixture.process.calls)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())); err != nil {
		t.Fatalf("backup was removed before server terminal observation: %v", err)
	}
	if !fixture.receipts.journalFound || fixture.receipts.journal.Stage != "backup_durable" {
		t.Fatalf("pre-replace terminal journal=%#v found=%t", fixture.receipts.journal, fixture.receipts.journalFound)
	}
}

func TestSourceAgentUpdatePreservesRollbackFailureWhenOutcomePersistenceFails(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.receipts.waitErr = ErrSourceAgentReadyTimeout
	fixture.process.failCall = 2
	fixture.receipts.saveErr = errors.New("private persistence path")
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Code != SourceAgentCommandCodeRollbackFailed || result.Outcome != SourceAgentUpdateOutcomeFailed ||
		result.PersistenceCode != SourceAgentUpdateCodeOutcomePersistenceFailed || !result.BinaryRestored {
		t.Fatalf("result=%#v", result)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
}

func TestSourceAgentUpdateCorruptJournalFailsClosed(t *testing.T) {
	fixture := newDurableSourceAgentUpdateFixture(t, "")
	if err := fixture.store.directory.writeAtomic(sourceAgentUpdateJournalFileName, []byte("not-json")); err != nil {
		t.Fatal(err)
	}
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Code != SourceAgentUpdateCodeRecoveryFailed || fixture.process.calls != 0 {
		t.Fatalf("result=%#v process=%d", result, fixture.process.calls)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
}

func TestSourceAgentUpdateCorruptJournalRestoresDurableBackup(t *testing.T) {
	fixture := newDurableSourceAgentUpdateFixture(t, sourceAgentUpdateFaultAfterReplace)
	first := applyDurableSourceAgentUpdate(t, fixture)
	if first.Code != SourceAgentUpdateCodeInterrupted {
		t.Fatalf("first=%#v", first)
	}
	assertSourceAgentExecutable(t, fixture.executable, []byte("#!/bin/sh\necho new\n"))
	if err := fixture.store.directory.writeAtomic(sourceAgentUpdateJournalFileName, []byte("not-json")); err != nil {
		t.Fatal(err)
	}
	retry, err := NewSourceAgentUpdateTransaction(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	recovered := retry.Apply(context.Background(), fixture.request)
	if recovered.Code != SourceAgentUpdateCodeRecoveryFailed || !recovered.BinaryRestored || fixture.process.calls != 1 {
		t.Fatalf("recovered=%#v process=%d", recovered, fixture.process.calls)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
}

func TestSourceAgentUpdateRecoveryCleansIncompleteBackupPublish(t *testing.T) {
	fixture := newDurableSourceAgentUpdateFixture(t, "")
	nonce := strings.Repeat("d", sha256.Size*2)
	journal := fixture.transaction.newJournal(fixture.request, nonce, "started")
	if err := fixture.store.saveJournal(journal); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupPendingName())
	if err := os.WriteFile(pending, []byte("partial backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := applyDurableSourceAgentUpdate(t, fixture)
	if result.Outcome != SourceAgentUpdateOutcomeSucceeded {
		t.Fatalf("result=%#v", result)
	}
	if _, err := os.Stat(pending); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending backup remains: %v", err)
	}
}

func TestSourceAgentUpdateTerminalReplayRetainsPostReplaceRecoveryMaterial(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	first := fixture.transaction.Apply(context.Background(), fixture.request)
	if first.Outcome != SourceAgentUpdateOutcomeSucceeded {
		t.Fatalf("first=%#v", first)
	}
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
	if !fixture.receipts.journalFound {
		t.Fatal("successful update cleared journal before server terminal acknowledgement")
	}

	replayed := fixture.transaction.Apply(context.Background(), fixture.request)
	if replayed != first || fixture.process.calls != 1 {
		t.Fatalf("replayed=%#v process=%d", replayed, fixture.process.calls)
	}
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
	if !fixture.receipts.journalFound {
		t.Fatal("terminal replay cleared recovery material without server acknowledgement")
	}

	other := fixture.request
	other.CommandID = "command-other"
	other.TargetVersion = "3.0.0"
	blocked := fixture.transaction.Apply(context.Background(), other)
	if blocked.Code != SourceAgentUpdateCodeRecoveryFailed {
		t.Fatalf("different update was not blocked by unacknowledged terminal: %#v", blocked)
	}
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
}

func TestSourceAgentUpdateUnacknowledgedSuccessBlocksNextUpdate(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.fs.backupRemoveFailures = 1
	first := fixture.transaction.Apply(context.Background(), fixture.request)
	if first.Outcome != SourceAgentUpdateOutcomeSucceeded {
		t.Fatalf("first=%#v", first)
	}
	if !fixture.receipts.journalFound {
		t.Fatal("cleanup failure cleared the journal")
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.newBinary)

	next := fixture.request
	next.CommandID = "command-2"
	next.TargetVersion = "3.0.0"
	fixture.guard.failCall = fixture.guard.calls + 1
	replayed := fixture.transaction.Apply(context.Background(), next)
	if replayed.Code != SourceAgentUpdateCodeRecoveryFailed {
		t.Fatalf("unacknowledged success did not block next update: %#v", replayed)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.newBinary)
	assertSourceAgentExecutable(t, filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName()), fixture.oldBinary)
	if fixture.process.calls != 1 || !fixture.receipts.journalFound {
		t.Fatalf("process=%d journal=%v", fixture.process.calls, fixture.receipts.journalFound)
	}
}

func TestSourceAgentUpdateRollbackSyncFailureRetainsRecoveryStateUntilReplay(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.receipts.waitErr = ErrSourceAgentReadyTimeout
	fixture.fs.dirSyncFrom = 4
	first := fixture.transaction.Apply(context.Background(), fixture.request)
	if first.Code != SourceAgentCommandCodeRollbackFailed || !first.BinaryRestored {
		t.Fatalf("first=%#v", first)
	}
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
	if !fixture.receipts.journalFound {
		t.Fatal("rollback sync failure cleared the journal")
	}

	fixture.fs.dirSyncFrom = 0
	fixture.receipts.waitErr = nil
	retry, err := NewSourceAgentUpdateTransaction(fixture.transaction.config)
	if err != nil {
		t.Fatal(err)
	}
	recovered := retry.Apply(context.Background(), fixture.request)
	if recovered != first {
		t.Fatalf("terminal outcome changed: recovered=%#v first=%#v", recovered, first)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup was removed before terminal acknowledgement: %v", err)
	}
	if !fixture.receipts.journalFound {
		t.Fatal("journal was removed before terminal acknowledgement")
	}
}

func TestSourceAgentUpdateDoesNotAttemptJournalClearBeforeTerminalAck(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.receipts.clearErr = errors.New("private journal cleanup path")
	first := fixture.transaction.Apply(context.Background(), fixture.request)
	if first.Outcome != SourceAgentUpdateOutcomeSucceeded || !fixture.receipts.journalFound {
		t.Fatalf("first=%#v journal=%v", first, fixture.receipts.journalFound)
	}
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup should remain before acknowledgement: %v", err)
	}

	fixture.receipts.clearErr = nil
	next := fixture.request
	next.CommandID = "command-2"
	next.TargetVersion = "3.0.0"
	fixture.guard.failCall = fixture.guard.calls + 1
	replayed := fixture.transaction.Apply(context.Background(), next)
	if replayed.Code != SourceAgentUpdateCodeRecoveryFailed {
		t.Fatalf("unacknowledged journal did not block next update: %#v", replayed)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.newBinary)
	if fixture.process.calls != 1 || !fixture.receipts.journalFound {
		t.Fatalf("process=%d journal=%v", fixture.process.calls, fixture.receipts.journalFound)
	}
}

func TestSourceAgentUpdateDoesNotAttemptCleanupSyncBeforeTerminalAck(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.fs.dirSyncFrom = 4
	first := fixture.transaction.Apply(context.Background(), fixture.request)
	if first.Outcome != SourceAgentUpdateOutcomeSucceeded || !fixture.receipts.journalFound {
		t.Fatalf("first=%#v journal=%v", first, fixture.receipts.journalFound)
	}
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup should remain before acknowledgement: %v", err)
	}

	fixture.fs.dirSyncFrom = 0
	next := fixture.request
	next.CommandID = "command-2"
	next.TargetVersion = "3.0.0"
	fixture.guard.failCall = fixture.guard.calls + 1
	replayed := fixture.transaction.Apply(context.Background(), next)
	if replayed.Code != SourceAgentUpdateCodeRecoveryFailed {
		t.Fatalf("unacknowledged terminal did not block next update: %#v", replayed)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.newBinary)
	if fixture.process.calls != 1 || !fixture.receipts.journalFound {
		t.Fatalf("process=%d journal=%v", fixture.process.calls, fixture.receipts.journalFound)
	}
}

func TestSourceAgentUpdatePreReplaceCrashWindowsReplayAsCleanupOnly(t *testing.T) {
	for _, failure := range []string{"final_guard", "replace"} {
		for _, fault := range []string{
			sourceAgentUpdateFaultPreReplaceAfterOutcome,
			sourceAgentUpdateFaultAfterCleanupBackupRemove,
			sourceAgentUpdateFaultAfterCleanupSync,
		} {
			for _, retryKind := range []string{"same_command", "different_command"} {
				t.Run(failure+"/"+fault+"/"+retryKind, func(t *testing.T) {
					fixture := newSourceAgentUpdateFixture(t)
					switch failure {
					case "final_guard":
						fixture.guard.failCall = 2
					case "replace":
						fixture.fs.fail = "replace"
					}
					fixture.transaction.faultStage = fault
					first := fixture.transaction.Apply(context.Background(), fixture.request)
					if first.Code != SourceAgentCommandCodeInstallFailed || !fixture.receipts.journalFound {
						t.Fatalf("first=%#v journal=%v", first, fixture.receipts.journalFound)
					}
					assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)

					fixture.transaction.faultStage = ""
					if retryKind == "different_command" {
						fixture.fs.fail = ""
						fixture.guard.failCall = fixture.guard.calls + 1
					}
					retry := fixture.request
					if retryKind == "different_command" {
						retry.CommandID = "command-2"
						retry.TargetVersion = "3.0.0"
					}
					replayed := fixture.transaction.Apply(context.Background(), retry)
					if retryKind == "same_command" && replayed.Code == SourceAgentUpdateCodeRecoveryFailed {
						t.Fatalf("same pre-replace terminal was not replayed: %#v", replayed)
					}
					if retryKind == "different_command" && replayed.Code != SourceAgentUpdateCodeRecoveryFailed {
						t.Fatalf("different command bypassed unacknowledged terminal: %#v", replayed)
					}
					assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
					if fixture.process.calls != 0 {
						t.Fatalf("pre-replace replay restarted worker %d times", fixture.process.calls)
					}
					if !fixture.receipts.journalFound {
						t.Fatal("pre-replace terminal journal was cleared before acknowledgement")
					}
				})
			}
		}
	}
}

func TestSourceAgentUpdatePreReplaceOutcomePersistenceFailureRetainsRecoveryState(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.guard.failCall = 2
	fixture.receipts.saveErr = errors.New("private outcome persistence path")
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Code != SourceAgentCommandCodeInstallFailed || result.PersistenceCode != SourceAgentUpdateCodeOutcomePersistenceFailed {
		t.Fatalf("result=%#v", result)
	}
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
	assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	if !fixture.receipts.journalFound || fixture.receipts.journal.Stage != "backup_durable" {
		t.Fatalf("journal=%#v found=%v", fixture.receipts.journal, fixture.receipts.journalFound)
	}
}

func TestSourceAgentUpdateRejectsImpossibleDurableTerminalCombination(t *testing.T) {
	fixture := newDurableSourceAgentUpdateFixture(t, "")
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	identity, err := fixture.transaction.fs.BackupExecutable(fixture.executable, backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.transaction.fs.SyncDirectory(filepath.Dir(fixture.executable)); err != nil {
		t.Fatal(err)
	}
	journal := fixture.transaction.newJournal(fixture.request, strings.Repeat("e", sha256.Size*2), "backup_durable")
	journal.Backup = identity
	if err := fixture.store.saveJournal(journal); err != nil {
		t.Fatal(err)
	}
	impossible := fixture.transaction.finishResult(time.Now(), fixture.request, SourceAgentUpdateOutcomeSucceeded, SourceAgentCommandCodeUpgradeComplete, false)
	impossible.RuntimeVersion = fixture.request.TargetVersion
	if err := fixture.store.SaveOutcome(impossible); err != nil {
		t.Fatal(err)
	}

	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Code != SourceAgentUpdateCodeRecoveryFailed {
		t.Fatalf("result=%#v", result)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
	if _, found, err := fixture.store.loadJournal(); err != nil || !found {
		t.Fatalf("journal found=%v err=%v", found, err)
	}
}

func TestSourceAgentUpdateRejectsRollbackRestoredWithoutBinaryRestored(t *testing.T) {
	fixture := newDurableSourceAgentUpdateFixture(t, "")
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	identity, err := fixture.transaction.fs.BackupExecutable(fixture.executable, backup)
	if err != nil {
		t.Fatal(err)
	}
	journal := fixture.transaction.newJournal(fixture.request, strings.Repeat("9", sha256.Size*2), "rollback_restored")
	journal.Backup = identity
	if err := fixture.store.saveJournal(journal); err != nil {
		t.Fatal(err)
	}
	impossible := fixture.transaction.finishResult(time.Now(), fixture.request, SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeRollbackFailed, false)
	if err := fixture.store.SaveOutcome(impossible); err != nil {
		t.Fatal(err)
	}

	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Code != SourceAgentUpdateCodeRecoveryFailed || result.BinaryRestored {
		t.Fatalf("result=%#v", result)
	}
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
	if _, found, err := fixture.store.loadJournal(); err != nil || !found {
		t.Fatalf("journal found=%v err=%v", found, err)
	}
	if fixture.process.calls != 0 {
		t.Fatalf("impossible state triggered recovery restart: %d", fixture.process.calls)
	}
}

func TestSourceAgentUpdateTerminalCombinationRejectsInvalidMatrix(t *testing.T) {
	baseJournal := sourceAgentUpdateJournal{Stage: "ready", CurrentVersion: "1.0.0", TargetVersion: "2.0.0"}
	baseResult := SourceAgentUpdateResult{
		Outcome: SourceAgentUpdateOutcomeSucceeded, Code: SourceAgentCommandCodeUpgradeComplete,
		RuntimeVersion: "2.0.0",
	}
	tests := []struct {
		name   string
		mutate func(*SourceAgentUpdateResult, *sourceAgentUpdateJournal)
	}{
		{name: "success wrong stage", mutate: func(_ *SourceAgentUpdateResult, journal *sourceAgentUpdateJournal) { journal.Stage = "replaced" }},
		{name: "success wrong code", mutate: func(result *SourceAgentUpdateResult, _ *sourceAgentUpdateJournal) {
			result.Code = SourceAgentCommandCodeInstallFailed
		}},
		{name: "success wrong runtime", mutate: func(result *SourceAgentUpdateResult, _ *sourceAgentUpdateJournal) { result.RuntimeVersion = "1.0.0" }},
		{name: "success restored", mutate: func(result *SourceAgentUpdateResult, _ *sourceAgentUpdateJournal) { result.BinaryRestored = true }},
		{name: "rolled back wrong stage", mutate: func(result *SourceAgentUpdateResult, journal *sourceAgentUpdateJournal) {
			result.Outcome, result.Code, result.RuntimeVersion, result.BinaryRestored = SourceAgentUpdateOutcomeRolledBack, SourceAgentCommandCodeRollbackComplete, "1.0.0", true
			journal.Stage = "ready"
		}},
		{name: "rolled back not restored", mutate: func(result *SourceAgentUpdateResult, journal *sourceAgentUpdateJournal) {
			result.Outcome, result.Code, result.RuntimeVersion = SourceAgentUpdateOutcomeRolledBack, SourceAgentCommandCodeRollbackComplete, "1.0.0"
			journal.Stage = "rollback_restored"
		}},
		{name: "failed wrong runtime", mutate: func(result *SourceAgentUpdateResult, journal *sourceAgentUpdateJournal) {
			result.Outcome, result.Code = SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed
			journal.Stage = "backup_durable"
		}},
		{name: "failed wrong stage", mutate: func(result *SourceAgentUpdateResult, journal *sourceAgentUpdateJournal) {
			result.Outcome, result.Code, result.RuntimeVersion = SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, "1.0.0"
			journal.Stage = "replaced"
		}},
		{name: "failed restored", mutate: func(result *SourceAgentUpdateResult, journal *sourceAgentUpdateJournal) {
			result.Outcome, result.Code, result.RuntimeVersion, result.BinaryRestored = SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, "1.0.0", true
			journal.Stage = "backup_durable"
		}},
		{name: "rollback failed stage says restored", mutate: func(result *SourceAgentUpdateResult, journal *sourceAgentUpdateJournal) {
			result.Outcome, result.Code, result.RuntimeVersion = SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeRollbackFailed, "1.0.0"
			journal.Stage = "rollback_restored"
		}},
		{name: "persistence marker stored", mutate: func(result *SourceAgentUpdateResult, _ *sourceAgentUpdateJournal) {
			result.PersistenceCode = SourceAgentUpdateCodeOutcomePersistenceFailed
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, journal := baseResult, baseJournal
			test.mutate(&result, &journal)
			if validSourceAgentUpdateTerminalCombination(result, journal) {
				t.Fatalf("accepted result=%#v journal=%#v", result, journal)
			}
		})
	}
}

type publishedErrorSourceAgentUpdateReceipts struct {
	*fakeSourceAgentUpdateReceipts
	publishErrors bool
	saveCalls     int
}

func (r *publishedErrorSourceAgentUpdateReceipts) SaveOutcome(result SourceAgentUpdateResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saveCalls++
	if r.loaded && r.load != result {
		return errors.New("immutable outcome conflict")
	}
	r.load, r.loaded = result, true
	if r.publishErrors {
		return errors.New("published but fsync status unavailable")
	}
	return nil
}

func TestSourceAgentUpdatePublishedOutcomeErrorNeverRollsBack(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	receipts := &publishedErrorSourceAgentUpdateReceipts{fakeSourceAgentUpdateReceipts: fixture.receipts, publishErrors: true}
	fixture.transaction.receipts = receipts
	fixture.transaction.config.Receipts = receipts
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Outcome != SourceAgentUpdateOutcomeSucceeded || result.PersistenceCode != SourceAgentUpdateCodeOutcomePersistenceFailed {
		t.Fatalf("result=%#v", result)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.newBinary)
	if !fixture.receipts.journalFound {
		t.Fatal("ambiguous outcome publication discarded recovery journal")
	}

	retry, err := NewSourceAgentUpdateTransaction(fixture.transaction.config)
	if err != nil {
		t.Fatal(err)
	}
	stillUnknown := retry.Apply(context.Background(), fixture.request)
	if stillUnknown.Outcome != SourceAgentUpdateOutcomeSucceeded ||
		stillUnknown.PersistenceCode != SourceAgentUpdateCodeOutcomePersistenceFailed {
		t.Fatalf("stillUnknown=%#v", stillUnknown)
	}
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
	if !fixture.receipts.journalFound {
		t.Fatal("replay cleaned recovery material before outcome fsync confirmation")
	}

	receipts.publishErrors = false
	replayed := retry.Apply(context.Background(), fixture.request)
	if replayed.Outcome != SourceAgentUpdateOutcomeSucceeded || fixture.process.calls != 1 {
		t.Fatalf("replayed=%#v process=%d", replayed, fixture.process.calls)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.newBinary)
	if _, err := os.Stat(backup); err != nil || !fixture.receipts.journalFound {
		t.Fatalf("durable replay cleared unacknowledged backup/journal: backupErr=%v journal=%v", err, fixture.receipts.journalFound)
	}
}

func TestSourceAgentUpdateEveryTerminalReplayConfirmsOutcomeBeforeMutation(t *testing.T) {
	terminals := []struct {
		name     string
		stage    string
		outcome  string
		code     string
		restored bool
		target   bool
	}{
		{name: "succeeded", stage: "ready", outcome: SourceAgentUpdateOutcomeSucceeded, code: SourceAgentCommandCodeUpgradeComplete, target: true},
		{name: "rolled_back", stage: "rollback_restored", outcome: SourceAgentUpdateOutcomeRolledBack, code: SourceAgentCommandCodeRollbackComplete, restored: true},
		{name: "failed", stage: "backup_durable", outcome: SourceAgentUpdateOutcomeFailed, code: SourceAgentCommandCodeInstallFailed},
		{name: "rollback_failed", stage: "rollback_restored", outcome: SourceAgentUpdateOutcomeFailed, code: SourceAgentCommandCodeRollbackFailed, restored: true},
	}
	for _, terminalCase := range terminals {
		for _, requestKind := range []string{"same_command", "different_command"} {
			t.Run(terminalCase.name+"/"+requestKind, func(t *testing.T) {
				fixture := newSourceAgentUpdateFixture(t)
				backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
				identity, err := fixture.fs.BackupExecutable(fixture.executable, backup)
				if err != nil {
					t.Fatal(err)
				}
				journal := fixture.transaction.newJournal(fixture.request, strings.Repeat("a", sha256.Size*2), terminalCase.stage)
				journal.Backup = identity
				terminal := fixture.transaction.finishResult(time.Now(), fixture.request, terminalCase.outcome, terminalCase.code, terminalCase.restored)
				if terminalCase.target {
					terminal.RuntimeVersion = fixture.request.TargetVersion
				}
				fixture.receipts.journal, fixture.receipts.journalFound = journal, true
				fixture.receipts.load, fixture.receipts.loaded = terminal, true
				receipts := &publishedErrorSourceAgentUpdateReceipts{
					fakeSourceAgentUpdateReceipts: fixture.receipts,
					publishErrors:                 true,
				}
				fixture.transaction.receipts = receipts
				request := fixture.request
				if requestKind == "different_command" {
					request.CommandID = "command-2"
					request.TargetVersion = "3.0.0"
				}

				result := fixture.transaction.Apply(context.Background(), request)
				if result != withSourceAgentPersistenceFailure(terminal) || receipts.saveCalls != 1 {
					t.Fatalf("result=%#v terminal=%#v saveCalls=%d", result, terminal, receipts.saveCalls)
				}
				assertSourceAgentExecutable(t, backup, fixture.oldBinary)
				if !fixture.receipts.journalFound || fixture.process.calls != 0 {
					t.Fatalf("journal=%v process=%d", fixture.receipts.journalFound, fixture.process.calls)
				}
			})
		}
	}
}

func withSourceAgentPersistenceFailure(result SourceAgentUpdateResult) SourceAgentUpdateResult {
	result.PersistenceCode = SourceAgentUpdateCodeOutcomePersistenceFailed
	return result
}

func TestSourceAgentUpdatePublishedSuccessWithCorruptJournalNeverRollsBack(t *testing.T) {
	fixture := newDurableSourceAgentUpdateFixture(t, "")
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	identity, err := fixture.transaction.fs.BackupExecutable(fixture.executable, backup)
	if err != nil {
		t.Fatal(err)
	}
	journal := fixture.transaction.newJournal(fixture.request, strings.Repeat("f", sha256.Size*2), "ready")
	journal.Backup = identity
	if err := fixture.store.saveJournal(journal); err != nil {
		t.Fatal(err)
	}
	newBinary := []byte("#!/bin/sh\necho new\n")
	if err := os.WriteFile(fixture.executable, newBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	succeeded := fixture.transaction.finishResult(time.Now(), fixture.request, SourceAgentUpdateOutcomeSucceeded, SourceAgentCommandCodeUpgradeComplete, false)
	succeeded.RuntimeVersion = fixture.request.TargetVersion
	if err := fixture.store.SaveOutcome(succeeded); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.directory.writeAtomic(sourceAgentUpdateJournalFileName, []byte("not-json")); err != nil {
		t.Fatal(err)
	}

	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Code != SourceAgentUpdateCodeRecoveryFailed || result.BinaryRestored {
		t.Fatalf("result=%#v", result)
	}
	assertSourceAgentExecutable(t, fixture.executable, newBinary)
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
	if fixture.process.calls != 0 {
		t.Fatalf("process calls=%d", fixture.process.calls)
	}
}

func TestSourceAgentUpdateRevalidatesCurrentBinaryImmediatelyBeforeReplace(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	replacement := []byte("#!/bin/sh\necho externally-replaced\n")
	fixture.fs.afterBackup = func() {
		if err := os.WriteFile(fixture.executable, replacement, 0o755); err != nil {
			t.Errorf("replace current executable: %v", err)
		}
	}
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Outcome != SourceAgentUpdateOutcomeFailed || result.Code != SourceAgentCommandCodeInstallFailed {
		t.Fatalf("result=%#v", result)
	}
	assertSourceAgentExecutable(t, fixture.executable, replacement)
	if fixture.process.calls != 0 {
		t.Fatalf("process calls=%d", fixture.process.calls)
	}
}

func TestSourceAgentUpdateRestartIsBounded(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.transaction.config.RestartTimeout = 20 * time.Millisecond
	fixture.process.blockUntilDone = true
	started := time.Now()
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("restart path was not bounded: %s", elapsed)
	}
	if result.Code != SourceAgentCommandCodeRollbackFailed {
		t.Fatalf("result=%#v", result)
	}
}

func TestSourceAgentUpdateOrphanRecoveryReportsRestartFailure(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	if _, err := fixture.fs.BackupExecutable(fixture.executable, backup); err != nil {
		t.Fatal(err)
	}
	fixture.process.failCall = 1
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Code != SourceAgentUpdateCodeRecoveryFailed || result.BinaryRestored {
		t.Fatalf("result=%#v", result)
	}
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
}

func TestSourceAgentUpdateTransactionCloseOwnershipAndIdempotency(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	if err := fixture.transaction.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.transaction.Close(); err != nil {
		t.Fatal(err)
	}
	if fixture.receipts.closeCalls != 0 {
		t.Fatalf("injected receipt store closed %d times", fixture.receipts.closeCalls)
	}
	closed := fixture.transaction.Apply(context.Background(), fixture.request)
	if closed.Code != SourceAgentUpdateCodeClosed {
		t.Fatalf("closed apply=%#v", closed)
	}

	ownedConfig := fixture.transaction.config
	ownedConfig.FileSystem = nil
	ownedConfig.Receipts = nil
	owned, err := NewSourceAgentUpdateTransaction(ownedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceAgentUpdateCoreUsesFixedWorkerAllowlist(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	config := fixture.transaction.config
	config.WorkerType = "custom-worker"
	if _, err := NewSourceAgentUpdateTransaction(config); err == nil {
		t.Fatal("format-valid custom worker type should be rejected")
	}
}

type durableSourceAgentUpdateFixture struct {
	transaction *SourceAgentUpdateTransaction
	config      SourceAgentUpdateConfig
	request     SourceAgentUpdateRequest
	store       *FileSourceAgentUpdateReceiptStore
	process     *fakeSourceAgentUpdateProcess
	executable  string
	oldBinary   []byte
}

func newDurableSourceAgentUpdateFixture(t *testing.T, faultStage string) durableSourceAgentUpdateFixture {
	t.Helper()
	root := t.TempDir()
	binRoot := filepath.Join(root, "bin")
	stageRoot := filepath.Join(root, "stage")
	receiptRoot := filepath.Join(root, "receipts")
	for _, directory := range []string{binRoot, stageRoot, receiptRoot} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	oldBinary := []byte("#!/bin/sh\necho old\n")
	newBinary := []byte("#!/bin/sh\necho new\n")
	executable := filepath.Join(binRoot, "source-worker")
	staged := filepath.Join(stageRoot, "artifact")
	if err := os.WriteFile(executable, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, newBinary, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(receiptRoot, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	process := &fakeSourceAgentUpdateProcess{}
	config := SourceAgentUpdateConfig{
		WorkerType: "wechat-worker", Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		CurrentVersion: "1.0.0", ProtocolVersion: "2026-08-01",
		CurrentExecutable: executable, StagingRoot: stageRoot, BackupRoot: binRoot,
		ReceiptRoot: receiptRoot, ReadyTimeout: 100 * time.Millisecond,
		ProcessControl: process, Guard: &fakeSourceAgentUpdateGuard{}, Receipts: store,
	}
	digest := sha256.Sum256(newBinary)
	request := SourceAgentUpdateRequest{
		CommandID: "command-1", ArtifactID: "artifact-1", WorkerType: "wechat-worker", CurrentVersion: "1.0.0", TargetVersion: "2.0.0",
		ExpectedSHA256: fmt.Sprintf("%x", digest), ExpectedSize: int64(len(newBinary)), StagedBinary: staged,
		Platform: runtime.GOOS, Architecture: runtime.GOARCH, ProtocolVersion: "2026-08-01",
		Revision: sourceAgentUpdateTestRevision, Channel: "staging",
	}
	transaction, err := NewSourceAgentUpdateTransaction(config)
	if err != nil {
		t.Fatal(err)
	}
	transaction.faultStage = faultStage
	return durableSourceAgentUpdateFixture{
		transaction: transaction, config: config, request: request, store: store, process: process,
		executable: executable, oldBinary: oldBinary,
	}
}

func applyDurableSourceAgentUpdate(t *testing.T, fixture durableSourceAgentUpdateFixture) SourceAgentUpdateResult {
	t.Helper()
	resultChannel := make(chan SourceAgentUpdateResult, 1)
	go func() { resultChannel <- fixture.transaction.Apply(context.Background(), fixture.request) }()
	for {
		select {
		case result := <-resultChannel:
			return result
		default:
		}
		challenge, err := fixture.store.ReadyChallenge(fixture.request.CommandID)
		if err == nil {
			receipt := SourceAgentReadyReceipt{
				CommandID: challenge.CommandID, AttemptNonce: challenge.AttemptNonce,
				WorkerType: challenge.WorkerType, Version: challenge.Version,
				Platform: challenge.Platform, Architecture: challenge.Architecture,
				ProtocolVersion: challenge.ProtocolVersion, Revision: challenge.Revision,
				HeartbeatAuthenticated: true,
			}
			_ = fixture.store.WriteReady(receipt)
		}
		time.Sleep(time.Millisecond)
	}
}
