//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSourceAgentUpdateReceiptStorePublishesReadyFromCompiledIdentity(t *testing.T) {
	newStore := func(t *testing.T) (*FileSourceAgentUpdateReceiptStore, SourceAgentReadyReceipt) {
		t.Helper()
		root := t.TempDir()
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatal(err)
		}
		store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		receipt := sourceAgentUpdateTestReadyReceipt()
		seedSourceAgentReadyJournal(t, store, receipt)
		journal, found, err := store.loadJournal()
		if err != nil || !found {
			t.Fatalf("load journal found=%t err=%v", found, err)
		}
		journal.Stage = "restart_requested"
		if err := store.saveJournal(journal); err != nil {
			t.Fatal(err)
		}
		return store, receipt
	}
	identityFor := func(receipt SourceAgentReadyReceipt) SourceAgentRuntimeIdentity {
		return SourceAgentRuntimeIdentity{
			WorkerType: receipt.WorkerType, Version: receipt.Version, Platform: receipt.Platform,
			Architecture: receipt.Architecture, ProtocolVersion: receipt.ProtocolVersion, Revision: receipt.Revision,
		}
	}

	store, receipt := newStore(t)
	published, err := store.PublishAuthenticatedReady(context.Background(), identityFor(receipt))
	if err != nil || !published {
		t.Fatalf("PublishAuthenticatedReady() = %t, %v", published, err)
	}
	expected := SourceAgentReadyExpectation{
		CommandID: receipt.CommandID, AttemptNonce: receipt.AttemptNonce, WorkerType: receipt.WorkerType,
		Version: receipt.Version, Platform: receipt.Platform, Architecture: receipt.Architecture,
		ProtocolVersion: receipt.ProtocolVersion, Revision: receipt.Revision,
	}
	if err := store.WaitReady(context.Background(), expected, 20*time.Millisecond); err != nil {
		t.Fatalf("published ready receipt: %v", err)
	}
	if replayed, err := store.PublishAuthenticatedReady(context.Background(), identityFor(receipt)); err != nil || !replayed {
		t.Fatalf("ready replay = %t, %v", replayed, err)
	}

	mismatchStore, mismatchReceipt := newStore(t)
	mismatch := identityFor(mismatchReceipt)
	mismatch.Revision = strings.Repeat("d", 40)
	if published, err := mismatchStore.PublishAuthenticatedReady(context.Background(), mismatch); published ||
		!errors.Is(err, ErrSourceAgentReadyMismatch) {
		t.Fatalf("mismatched compiled identity = %t, %v", published, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if published, err := mismatchStore.PublishAuthenticatedReady(canceled, identityFor(mismatchReceipt)); published ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publish = %t, %v", published, err)
	}
}

func TestSourceAgentUpdateReceiptStorePersistsProtectedUpgradeCheckpoint(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	command := sourceAgentUpdateTestCheckpointCommand()
	if err := store.SaveUpgradeCommandCheckpoint(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := store.LoadUpgradeCommandCheckpoint()
	if err != nil || !found || !reflect.DeepEqual(loaded.Command, command) ||
		loaded.Fingerprint != sourceAgentUpgradeCommandFingerprint(command) {
		t.Fatalf("checkpoint=%#v found=%t err=%v", loaded, found, err)
	}
	info, err := os.Lstat(filepath.Join(root, sourceAgentUpgradeCheckpointFileName))
	if err != nil || info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
		t.Fatalf("checkpoint mode=%v err=%v", info, err)
	}

	advanced := command
	advanced.State = SourceAgentCommandInstalling
	advanced.UpdatedAt = "2026-08-01T12:00:02.000000000Z"
	if err := store.SaveUpgradeCommandCheckpoint(context.Background(), advanced); err != nil {
		t.Fatalf("advance checkpoint: %v", err)
	}
	loaded, found, err = store.LoadUpgradeCommandCheckpoint()
	if err != nil || !found || loaded.Command.State != SourceAgentCommandInstalling {
		t.Fatalf("advanced checkpoint=%#v found=%t err=%v", loaded, found, err)
	}

	conflict := advanced
	conflict.UpgradeSpec = &SourceAgentUpgradeSpec{ArtifactID: "artifact-other", ExpectedCurrentVersion: "1.0.0"}
	if err := store.SaveUpgradeCommandCheckpoint(context.Background(), conflict); !errors.Is(err, ErrSourceAgentUpgradeCheckpointConflict) {
		t.Fatalf("conflicting checkpoint error=%v", err)
	}
	if err := store.ClearUpgradeCommandCheckpoint(context.Background(), command.ID, strings.Repeat("f", 64)); !errors.Is(err, ErrSourceAgentUpgradeCheckpointConflict) {
		t.Fatalf("wrong fingerprint clear error=%v", err)
	}
	if err := store.ClearUpgradeCommandCheckpoint(context.Background(), command.ID, loaded.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.LoadUpgradeCommandCheckpoint(); err != nil || found {
		t.Fatalf("checkpoint remained found=%t err=%v", found, err)
	}
}

func TestSourceAgentUpdateReceiptStoreRecordsStrictTerminalObservation(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	active := sourceAgentUpdateTestCheckpointCommand()
	if err := store.SaveUpgradeCommandCheckpoint(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	terminal := active
	terminal.State = SourceAgentCommandSucceeded
	terminal.ResultCode = SourceAgentCommandCodeUpgradeComplete
	terminal.ActualVersion = "2.0.0"
	terminal.UpdatedAt = "2026-08-01T12:00:03.000000000Z"
	terminal.CompletedAt = terminal.UpdatedAt
	if err := store.RecordServerTerminalObservation(context.Background(), terminal); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordServerTerminalObservation(context.Background(), terminal); err != nil {
		t.Fatalf("terminal observation replay: %v", err)
	}
	payload, err := store.directory.read(sourceAgentUpgradeTerminalObservationFileName, sourceAgentUpdateReceiptMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	var observation SourceAgentUpgradeTerminalObservation
	if decodeStrictSourceAgentUpdateJSON(payload, &observation) != nil ||
		observation.CommandID != terminal.ID || observation.State != SourceAgentCommandSucceeded ||
		observation.Fingerprint != sourceAgentUpgradeCommandFingerprint(terminal) {
		t.Fatalf("terminal observation=%#v payload=%s", observation, payload)
	}
	for _, forbidden := range []string{"token", "cookie", "/Users/", "script", "environment"} {
		if strings.Contains(strings.ToLower(string(payload)), strings.ToLower(forbidden)) {
			t.Fatalf("terminal observation leaked %q: %s", forbidden, payload)
		}
	}
	conflict := terminal
	conflict.UpgradeSpec = &SourceAgentUpgradeSpec{ArtifactID: "artifact-other", ExpectedCurrentVersion: "1.0.0"}
	if err := store.RecordServerTerminalObservation(context.Background(), conflict); !errors.Is(err, ErrSourceAgentUpgradeCheckpointConflict) {
		t.Fatalf("conflicting terminal observation error=%v", err)
	}
	if _, found, err := store.LoadUpgradeCommandCheckpoint(); err != nil || !found {
		t.Fatalf("terminal acknowledgement cleared checkpoint found=%t err=%v", found, err)
	}
	invalidObservation := SourceAgentUpgradeTerminalObservation{
		SchemaVersion: sourceAgentUpgradeTerminalObservationSchema, CommandID: terminal.ID,
		Fingerprint: sourceAgentUpgradeCommandFingerprint(terminal), State: SourceAgentCommandSucceeded,
		ResultCode: SourceAgentCommandCodeCanceled, ActualVersion: terminal.ActualVersion,
	}
	if validSourceAgentUpgradeTerminalObservation(invalidObservation) {
		t.Fatal("terminal observation accepted a result code from another state")
	}
}

func sourceAgentUpdateTestCheckpointCommand() SourceAgentCommand {
	return SourceAgentCommand{
		ID: "command-checkpoint", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade,
		UpgradeSpec: &SourceAgentUpgradeSpec{ArtifactID: "artifact-2", ExpectedCurrentVersion: "1.0.0"},
		State:       SourceAgentCommandClaimed, IdempotencyKey: "upgrade-checkpoint",
		ExpectedCurrentVersion: "1.0.0", ClaimOwner: "agent-a",
		CreatedAt: "2026-08-01T12:00:00.000000000Z", UpdatedAt: "2026-08-01T12:00:01.000000000Z",
		ClaimedAt: "2026-08-01T12:00:01.000000000Z", ExpiresAt: "2026-08-01T13:00:00.000000000Z",
	}
}

func TestSourceAgentUpdateReceiptStorePinsRootDirectory(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "receipts")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	receipt := sourceAgentUpdateTestReadyReceipt()
	seedSourceAgentReadyJournal(t, store, receipt)

	pinnedRoot := root + ".pinned"
	if err := os.Rename(root, pinnedRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, sourceAgentUpdateJournalFileName), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	journal, found, err := store.loadJournal()
	if err != nil || !found || journal.AttemptNonce != receipt.AttemptNonce {
		t.Fatalf("store followed replaced root: journal=%#v found=%v err=%v", journal, found, err)
	}
	if err := store.WriteReady(receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pinnedRoot, filepath.Base(store.readyPath(receipt.CommandID, receipt.AttemptNonce)))); err != nil {
		t.Fatalf("ready receipt was not published under pinned root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.Base(store.readyPath(receipt.CommandID, receipt.AttemptNonce)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement root was modified: %v", err)
	}
}

func TestSourceAgentUpdateReceiptStoreRejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	receipt := sourceAgentUpdateTestReadyReceipt()
	expected := SourceAgentReadyExpectation{
		CommandID: receipt.CommandID, AttemptNonce: receipt.AttemptNonce,
		WorkerType: receipt.WorkerType, Version: receipt.Version,
		Platform: receipt.Platform, Architecture: receipt.Architecture,
		ProtocolVersion: receipt.ProtocolVersion, Revision: receipt.Revision,
	}
	path := store.readyPath(receipt.CommandID, receipt.AttemptNonce)
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = store.WaitReady(context.Background(), expected, 25*time.Millisecond)
	if !errors.Is(err, ErrSourceAgentReadyInvalid) {
		t.Fatalf("FIFO error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("FIFO read blocked for %s", elapsed)
	}
}

func TestSourceAgentUpdateReceiptStoreJournalIsBoundedAndNoFollow(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(string) error
	}{
		{name: "symlink", setup: func(path string) error { return os.Symlink(filepath.Join(filepath.Dir(path), "missing"), path) }},
		{name: "oversized", setup: func(path string) error {
			return os.WriteFile(path, []byte(strings.Repeat("x", sourceAgentUpdateReceiptMaxBytes+1)), 0o600)
		}},
		{name: "duplicate", setup: func(path string) error {
			return os.WriteFile(path, []byte(`{"schema_version":"source-agent-update-journal.v1","schema_version":"source-agent-update-journal.v1"}`), 0o600)
		}},
		{name: "unknown", setup: func(path string) error {
			return os.WriteFile(path, []byte(`{"unknown":true}`), 0o600)
		}},
		{name: "trailing", setup: func(path string) error {
			return os.WriteFile(path, []byte(`{} {}`), 0o600)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if err := test.setup(filepath.Join(root, sourceAgentUpdateJournalFileName)); err != nil {
				t.Fatal(err)
			}
			if _, found, err := store.loadJournal(); err == nil || !found {
				t.Fatalf("loadJournal found=%v err=%v", found, err)
			}
		})
	}
}

func TestSourceAgentUpdateReceiptStoreSerializesAcrossInstances(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	release, err := first.Acquire(context.Background(), "command-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Acquire(context.Background(), "command-2"); !errors.Is(err, ErrSourceAgentUpdateBusy) {
		t.Fatalf("second acquire error=%v", err)
	}
	release()
	releaseSecond, err := second.Acquire(context.Background(), "command-2")
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond()
}

func TestSourceAgentUpdateReceiptStoreRequiresProtectedRealDirectory(t *testing.T) {
	parent := t.TempDir()
	unsafe := filepath.Join(parent, "unsafe")
	if err := os.Mkdir(unsafe, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileSourceAgentUpdateReceiptStore(unsafe, time.Millisecond); err == nil {
		t.Fatal("group/world accessible receipt root should fail")
	}
	special := filepath.Join(parent, "special")
	if err := os.Mkdir(special, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(special, os.ModeSticky|0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileSourceAgentUpdateReceiptStore(special, time.Millisecond); err == nil {
		t.Fatal("receipt root with special permission bits should fail")
	}
	realRoot := filepath.Join(parent, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(parent, "linked")
	if err := os.Symlink(realRoot, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileSourceAgentUpdateReceiptStore(linked, time.Millisecond); err == nil {
		t.Fatal("symlink receipt root should fail")
	}
}

func TestSourceAgentUpgradeCheckpointRejectsSpecialPermissionBits(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveUpgradeCommandCheckpoint(context.Background(), sourceAgentUpdateTestCheckpointCommand()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, sourceAgentUpgradeCheckpointFileName)
	if err := unix.Chmod(path, 0o1600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.LoadUpgradeCommandCheckpoint(); err == nil || !found {
		t.Fatalf("special-bit checkpoint found=%t err=%v", found, err)
	}
}

func TestSourceAgentUpdateInstallLockSurvivesReceiptRootReplacement(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	config := fixture.transaction.config
	config.FileSystem = nil
	config.Receipts = nil
	first, err := NewSourceAgentUpdateTransaction(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })

	receiptRoot := config.ReceiptRoot
	oldRoot := receiptRoot + ".old"
	if err := os.Rename(receiptRoot, oldRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(receiptRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	second, err := NewSourceAgentUpdateTransaction(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	release, err := first.fs.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := second.fs.Acquire(context.Background()); !errors.Is(err, ErrSourceAgentUpdateBusy) {
		t.Fatalf("second install lock error=%v", err)
	}
}

func TestSourceAgentUpdateExecutableFIFOIsRejectedWithoutBlocking(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	config := fixture.transaction.config
	config.FileSystem = nil
	config.Receipts = nil
	transaction, err := NewSourceAgentUpdateTransaction(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transaction.Close() })
	if err := os.Remove(fixture.executable); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(fixture.executable, 0o700); err != nil {
		t.Fatal(err)
	}

	resultChannel := make(chan SourceAgentUpdateResult, 1)
	go func() { resultChannel <- transaction.Apply(context.Background(), fixture.request) }()
	select {
	case result := <-resultChannel:
		if result.Code != SourceAgentCommandCodeInstallFailed {
			t.Fatalf("result=%#v", result)
		}
	case <-time.After(2 * time.Second):
		writer, openErr := unix.Open(fixture.executable, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if openErr == nil {
			_ = unix.Close(writer)
		}
		<-resultChannel
		t.Fatal("FIFO executable open blocked updater")
	}
}

func TestSourceAgentUpdateRealPublishedOutcomeRetainsRecoveryMaterialAfterFsync(t *testing.T) {
	fixture := newDurableSourceAgentUpdateFixture(t, "")
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	identity, err := fixture.transaction.fs.BackupExecutable(fixture.executable, backup)
	if err != nil {
		t.Fatal(err)
	}
	journal := fixture.transaction.newJournal(fixture.request, strings.Repeat("b", 64), "ready")
	journal.Backup = identity
	if err := fixture.store.saveJournal(journal); err != nil {
		t.Fatal(err)
	}
	newBinary := []byte("#!/bin/sh\necho new\n")
	if err := os.WriteFile(fixture.executable, newBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	terminal := fixture.transaction.finishResult(time.Now(), fixture.request, SourceAgentUpdateOutcomeSucceeded, SourceAgentCommandCodeUpgradeComplete, false)
	terminal.RuntimeVersion = fixture.request.TargetVersion
	directory := fixture.store.directory.(*unixSourceAgentUpdateDirectory)
	directory.syncFD = func(int) error { return errors.New("injected receipt directory fsync failure") }
	if err := fixture.store.SaveOutcome(terminal); !isSourceAgentUpdatePublishedError(err) {
		t.Fatalf("SaveOutcome() error=%v", err)
	}

	unknown := fixture.transaction.Apply(context.Background(), fixture.request)
	if unknown != withSourceAgentPersistenceFailure(terminal) {
		t.Fatalf("unknown=%#v", unknown)
	}
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
	if _, found, err := fixture.store.loadJournal(); err != nil || !found {
		t.Fatalf("journal found=%v err=%v", found, err)
	}

	directory.syncFD = unix.Fsync
	durable := fixture.transaction.Apply(context.Background(), fixture.request)
	if durable != terminal {
		t.Fatalf("durable=%#v", durable)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup was removed before terminal acknowledgement: %v", err)
	}
	if _, found, err := fixture.store.loadJournal(); err != nil || !found {
		t.Fatalf("journal found=%v err=%v", found, err)
	}
}

func sourceAgentUpdateTestReadyReceipt() SourceAgentReadyReceipt {
	return SourceAgentReadyReceipt{
		CommandID: "command-1", AttemptNonce: strings.Repeat("a", 64),
		WorkerType: "wechat-worker", Version: "2.0.0",
		Platform: "darwin", Architecture: "arm64", ProtocolVersion: "2026-08-01",
		Revision: sourceAgentUpdateTestRevision, HeartbeatAuthenticated: true,
	}
}
