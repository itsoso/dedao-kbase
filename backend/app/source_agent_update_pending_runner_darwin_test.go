//go:build darwin

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type sourceAgentPendingRunnerReadyProcess struct {
	receiptRoot string
	request     SourceAgentUpdateRequest
}

type sourceAgentPendingRunnerBlockingProcess struct {
	entered chan struct{}
	once    sync.Once
	calls   int32
}

func (p *sourceAgentPendingRunnerBlockingProcess) Restart(ctx context.Context) error {
	if atomic.AddInt32(&p.calls, 1) != 1 {
		return errors.New("fixture rollback restart failed")
	}
	p.once.Do(func() { close(p.entered) })
	<-ctx.Done()
	return ctx.Err()
}

func (p *sourceAgentPendingRunnerReadyProcess) Restart(ctx context.Context) error {
	store, err := NewFileSourceAgentUpdateReceiptStore(p.receiptRoot, time.Millisecond)
	if err != nil {
		return err
	}
	defer store.Close()
	_, err = store.PublishAuthenticatedReady(ctx, SourceAgentRuntimeIdentity{
		WorkerType: p.request.WorkerType, Version: p.request.TargetVersion,
		Platform: p.request.Platform, Architecture: p.request.Architecture,
		ProtocolVersion: p.request.ProtocolVersion, Revision: p.request.Revision,
	})
	return err
}

func TestSourceAgentPendingRunnerFinishesDurableCleanupMarkerWithoutRemoteInput(t *testing.T) {
	temporaryRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installRoot := filepath.Join(temporaryRoot, "installed")
	if err := os.Mkdir(installRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(installRoot, "source-agent")
	updater := filepath.Join(installRoot, sourceAgentUpdateUpdaterBasename)
	for _, path := range []string{worker, updater} {
		if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	storage, err := newSourceAgentUpdateBridgeStorage(updater, "wechat-worker")
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := storage.StagedPath()
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(
		filepath.Join(installRoot, sourceAgentUpdateHandoffDirectoryName),
		time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := sourceAgentUpdatePhaseRequest()
	request.StagedBinary = stagedPath
	if err := store.PublishPending(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	fingerprint := sourceAgentUpdateRequestFingerprint(request)
	if err := store.MarkPendingCleanupComplete(context.Background(), request.CommandID, fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	err = RunSourceAgentPendingUpdate(context.Background(), SourceAgentPendingUpdateRunnerConfig{
		UpdaterExecutable: updater,
		WorkerType:        "wechat-worker",
		Guard:             &fakeSourceAgentUpdateGuard{},
		ProcessControl:    &fakeSourceAgentUpdateProcess{},
	})
	if err != nil {
		t.Fatalf("RunSourceAgentPendingUpdate() error = %v", err)
	}
	store, err = NewFileSourceAgentUpdateReceiptStore(
		filepath.Join(installRoot, sourceAgentUpdateHandoffDirectoryName),
		time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, found, err := store.LoadPending(); err != nil || found {
		t.Fatalf("completed marker survived: found=%t err=%v", found, err)
	}
}

func TestSourceAgentPendingRunnerAppliesAndCleansAcknowledgedUpdate(t *testing.T) {
	fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
	command := sourceAgentTerminalResolutionCommand()
	command.ID = "command-1"
	command.UpgradeSpec = &SourceAgentUpgradeSpec{
		ArtifactID: fixture.downloader.metadata.ID, ExpectedCurrentVersion: "1.0.0",
	}
	command.ExpectedCurrentVersion = "1.0.0"
	command.State = SourceAgentCommandDownloading
	fixture.command = command
	bridge := fixture.open(t)
	request, err := bridge.Prepare(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := command
	checkpoint.State = SourceAgentCommandInstalling
	if err := bridge.receipts.SaveUpgradeCommandCheckpoint(context.Background(), checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := bridge.receipts.PublishPending(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	probe, err := newSourceAgentUpdateBridgeStorage(fixture.updater, fixture.workerType)
	if err != nil {
		t.Fatalf("pending runner storage preflight: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	receiptRoot := filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName)
	done := make(chan error, 1)
	go func() {
		done <- RunSourceAgentPendingUpdate(context.Background(), SourceAgentPendingUpdateRunnerConfig{
			UpdaterExecutable: fixture.updater,
			WorkerType:        fixture.workerType,
			Guard:             &fakeSourceAgentUpdateGuard{},
			ProcessControl: &sourceAgentPendingRunnerReadyProcess{
				receiptRoot: receiptRoot, request: request,
			},
			PollInterval: time.Millisecond,
		})
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		outcome, found, loadErr := bridge.receipts.LoadOutcome(command.ID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if found {
			if outcome.Outcome != SourceAgentUpdateOutcomeSucceeded {
				t.Fatalf("outcome=%#v", outcome)
			}
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatal("pending updater did not publish outcome")
		}
		time.Sleep(time.Millisecond)
	}
	terminal := checkpoint
	terminal.State = SourceAgentCommandSucceeded
	terminal.ResultCode = SourceAgentCommandCodeUpgradeComplete
	terminal.ActualVersion = request.TargetVersion
	terminal.UpdatedAt = "2026-08-01T12:00:03.000000000Z"
	terminal.CompletedAt = terminal.UpdatedAt
	if err := bridge.receipts.RecordServerTerminalObservation(context.Background(), terminal); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.receipts.ResolveUpgradeTerminalObservation(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunSourceAgentPendingUpdate() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending updater did not finish terminal cleanup")
	}
	workerName, _ := sourceAgentUpdateWorkerBasename(fixture.workerType)
	workerPayload, err := os.ReadFile(filepath.Join(fixture.root, workerName))
	if err != nil || string(workerPayload) != string(fixture.artifact) {
		t.Fatalf("worker=%q error=%v", workerPayload, err)
	}
	for _, path := range []string{
		fixture.stagedPath(), fixture.handoffPath(),
		filepath.Join(receiptRoot, sourceAgentUpdatePendingFileName),
		filepath.Join(receiptRoot, sourceAgentUpdateJournalFileName),
		filepath.Join(receiptRoot, sourceAgentUpgradeCheckpointFileName),
		filepath.Join(receiptRoot, sourceAgentUpgradeTerminalObservationFileName),
		filepath.Join(receiptRoot, sourceAgentUpgradeTerminalResolutionFileName),
		filepath.Join(receiptRoot, sourceAgentUpdateReceiptName("outcome", command.ID, "")),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("cleanup retained %s: %v", filepath.Base(path), err)
		}
	}
}

func TestSourceAgentPendingRunnerHoldsLifecycleLockThroughoutAttempt(t *testing.T) {
	fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
	command := sourceAgentTerminalResolutionCommand()
	command.ID = "command-lifecycle"
	command.UpgradeSpec = &SourceAgentUpgradeSpec{
		ArtifactID: fixture.downloader.metadata.ID, ExpectedCurrentVersion: "1.0.0",
	}
	command.ExpectedCurrentVersion = "1.0.0"
	command.State = SourceAgentCommandDownloading
	fixture.command = command
	bridge := fixture.open(t)
	request, err := bridge.Prepare(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := command
	checkpoint.State = SourceAgentCommandInstalling
	if err := bridge.receipts.SaveUpgradeCommandCheckpoint(context.Background(), checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := bridge.receipts.PublishPending(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	process := &sourceAgentPendingRunnerBlockingProcess{entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- RunSourceAgentPendingUpdate(ctx, SourceAgentPendingUpdateRunnerConfig{
			UpdaterExecutable: fixture.updater,
			WorkerType:        fixture.workerType,
			Guard:             &fakeSourceAgentUpdateGuard{},
			ProcessControl:    process,
			PollInterval:      time.Millisecond,
		})
	}()
	select {
	case <-process.entered:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("pending updater did not reach the replacement attempt")
	}

	fd, err := unix.Open(
		filepath.Join(fixture.root, sourceAgentUpdateLifecycleFileName),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	lockErr := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if lockErr == nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
	}
	if closeErr := unix.Close(fd); closeErr != nil {
		cancel()
		t.Fatal(closeErr)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pending updater did not stop after cancellation")
	}
	if !errors.Is(lockErr, unix.EWOULDBLOCK) {
		t.Fatalf("exclusive installer lock error=%v, want EWOULDBLOCK", lockErr)
	}
}

func TestSourceAgentPendingRunnerRollsBackAuthenticatedFingerprintConflict(t *testing.T) {
	fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
	command := sourceAgentTerminalResolutionCommand()
	command.ID = "command-1"
	command.UpgradeSpec = &SourceAgentUpgradeSpec{
		ArtifactID: fixture.downloader.metadata.ID, ExpectedCurrentVersion: "1.0.0",
	}
	command.ExpectedCurrentVersion = "1.0.0"
	command.State = SourceAgentCommandDownloading
	fixture.command = command
	bridge := fixture.open(t)
	request, err := bridge.Prepare(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := command
	checkpoint.State = SourceAgentCommandInstalling
	if err := bridge.receipts.SaveUpgradeCommandCheckpoint(context.Background(), checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := bridge.receipts.PublishPending(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	receiptRoot := filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunSourceAgentPendingUpdate(ctx, SourceAgentPendingUpdateRunnerConfig{
			UpdaterExecutable: fixture.updater,
			WorkerType:        fixture.workerType,
			Guard:             &fakeSourceAgentUpdateGuard{},
			ProcessControl: &sourceAgentPendingRunnerReadyProcess{
				receiptRoot: receiptRoot, request: request,
			},
			PollInterval: time.Millisecond,
		})
	}()
	waitSourceAgentPendingOutcomeForTest(t, bridge.receipts, command.ID, SourceAgentUpdateOutcomeSucceeded)
	fingerprint := sourceAgentUpgradeCommandFingerprint(checkpoint)
	if err := bridge.RecordAuthenticatedUpgradeConflict(context.Background(), command.ID, fingerprint); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunSourceAgentPendingUpdate() error=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending updater did not resolve fingerprint conflict")
	}
	workerName, _ := sourceAgentUpdateWorkerBasename(fixture.workerType)
	workerPayload, err := os.ReadFile(filepath.Join(fixture.root, workerName))
	if err != nil || string(workerPayload) != "current worker binary" {
		t.Fatalf("worker=%q error=%v", workerPayload, err)
	}
	marker, found, err := bridge.receipts.LoadUpgradeConflict()
	if err != nil || !found || marker.State != sourceAgentUpgradeConflictRestored {
		t.Fatalf("conflict marker=%#v found=%t error=%v", marker, found, err)
	}
	if _, found, err := bridge.receipts.LoadUpgradeCommandCheckpoint(); err != nil || !found {
		t.Fatalf("checkpoint found=%t error=%v", found, err)
	}
	for _, path := range []string{
		fixture.stagedPath(), fixture.handoffPath(),
		filepath.Join(receiptRoot, sourceAgentUpdatePendingFileName),
		filepath.Join(receiptRoot, sourceAgentUpdateJournalFileName),
		filepath.Join(receiptRoot, sourceAgentUpgradeTerminalResolutionFileName),
		filepath.Join(receiptRoot, sourceAgentUpdateReceiptName("outcome", command.ID, "")),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("conflict cleanup retained %s: %v", filepath.Base(path), err)
		}
	}
}

func waitSourceAgentPendingOutcomeForTest(
	t *testing.T,
	store *FileSourceAgentUpdateReceiptStore,
	commandID, wantOutcome string,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		outcome, found, err := store.LoadOutcome(commandID)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			if outcome.Outcome != wantOutcome {
				t.Fatalf("outcome=%#v", outcome)
			}
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatal("pending updater did not publish outcome")
		}
		time.Sleep(time.Millisecond)
	}
}
