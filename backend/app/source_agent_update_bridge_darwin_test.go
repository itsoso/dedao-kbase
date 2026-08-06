//go:build darwin

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSourceAgentUpdateBridgeStagesExactRequestAndPathFreeHandoff(t *testing.T) {
	artifactBytes := []byte("fixed staged worker artifact")
	command := SourceAgentCommand{
		ID: "command-1", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade,
		State: SourceAgentCommandDownloading, ExpectedCurrentVersion: "1.0.0",
		UpgradeSpec: &SourceAgentUpgradeSpec{ArtifactID: "artifact-1", ExpectedCurrentVersion: "1.0.0"},
	}
	target := SourceAgentArtifactTarget{
		WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64", CurrentVersion: "1.0.0",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/source-agent/artifacts/artifact-1/download" ||
			r.URL.Query().Get("agent_id") != "agent-a" || r.URL.Query().Get("command_id") != command.ID {
			t.Fatalf("unexpected artifact request: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer agent-secret" {
			t.Fatalf("Authorization=%q", r.Header.Get("Authorization"))
		}
		headers := map[string]string{
			"Content-Type":                      "application/octet-stream",
			"Content-Length":                    "28",
			sourceAgentHeaderCommandID:          command.ID,
			sourceAgentHeaderArtifactID:         command.UpgradeSpec.ArtifactID,
			sourceAgentHeaderArtifactVersion:    "2.0.0",
			sourceAgentHeaderArtifactWorkerType: target.WorkerType,
			sourceAgentHeaderArtifactPlatform:   target.Platform,
			sourceAgentHeaderArtifactArch:       target.Architecture,
			sourceAgentHeaderArtifactProtocol:   "2026-08-01",
			sourceAgentHeaderArtifactRevision:   sourceAgentArtifactTestRevision,
			sourceAgentHeaderArtifactChannel:    "staging",
			sourceAgentHeaderArtifactSize:       "28",
			sourceAgentHeaderArtifactSHA256:     sha256HexForTest(artifactBytes),
		}
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(artifactBytes)
	}))
	defer server.Close()
	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
		StateDir: t.TempDir(), HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	installRoot, updaterExecutable := sourceAgentUpdateBridgeInstallFixture(t, "wechat-worker")
	bridge, err := NewSourceAgentUpdateBridge(SourceAgentUpdateBridgeConfig{
		Downloader: client, UpdaterExecutable: updaterExecutable,
		WorkerType: target.WorkerType, CurrentVersion: target.CurrentVersion,
		Platform: target.Platform, Architecture: target.Architecture, ProtocolVersion: "2026-08-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close() })

	request, err := bridge.Prepare(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(installRoot, sourceAgentUpdateStagingDirectoryName, sourceAgentUpdateStagedBasename)
	wantRequest := SourceAgentUpdateRequest{
		CommandID: command.ID, ArtifactID: command.UpgradeSpec.ArtifactID,
		WorkerType: target.WorkerType, CurrentVersion: target.CurrentVersion, TargetVersion: "2.0.0",
		ExpectedSHA256: sha256HexForTest(artifactBytes), ExpectedSize: int64(len(artifactBytes)), StagedBinary: stagedPath,
		Platform: target.Platform, Architecture: target.Architecture, ProtocolVersion: "2026-08-01",
		Revision: sourceAgentArtifactTestRevision, Channel: "staging",
	}
	if !reflect.DeepEqual(request, wantRequest) {
		t.Fatalf("request=%#v want=%#v", request, wantRequest)
	}
	stagedBytes, err := os.ReadFile(stagedPath)
	if err != nil || !reflect.DeepEqual(stagedBytes, artifactBytes) {
		t.Fatalf("staged bytes=%q err=%v", stagedBytes, err)
	}
	stagedInfo, err := os.Lstat(stagedPath)
	if err != nil || !stagedInfo.Mode().IsRegular() || stagedInfo.Mode().Perm() != 0o755 {
		t.Fatalf("staged info=%#v err=%v", stagedInfo, err)
	}
	for _, directory := range []string{
		filepath.Join(installRoot, sourceAgentUpdateStagingDirectoryName),
		filepath.Join(installRoot, sourceAgentUpdateHandoffDirectoryName),
	} {
		info, statErr := os.Lstat(directory)
		if statErr != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("private directory %q info=%#v err=%v", directory, info, statErr)
		}
	}
	handoffPath := filepath.Join(installRoot, sourceAgentUpdateHandoffDirectoryName, sourceAgentUpdateHandoffFileName)
	handoffPayload, err := os.ReadFile(handoffPath)
	if err != nil {
		t.Fatal(err)
	}
	var handoff map[string]any
	if err := json.Unmarshal(handoffPayload, &handoff); err != nil {
		t.Fatal(err)
	}
	gotKeys := make([]string, 0, len(handoff))
	for key := range handoff {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	wantKeys := []string{
		"architecture", "artifact_id", "channel", "command_id", "current_version", "platform",
		"protocol_version", "request_fingerprint", "revision", "sha256", "size", "staged_basename",
		"target_version", "worker_type",
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("handoff keys=%#v want=%#v", gotKeys, wantKeys)
	}
	if handoff["staged_basename"] != sourceAgentUpdateStagedBasename ||
		handoff["request_fingerprint"] != sourceAgentUpdateRequestFingerprint(request) {
		t.Fatalf("handoff=%#v", handoff)
	}
	for _, forbidden := range []string{
		installRoot, updaterExecutable, server.URL, "agent-secret", string(artifactBytes),
		"path", "url", "token", "argv", "command_line", "script", "environment", "label", "source",
		"cookie", "credential", "secret", "authorization", "bearer",
	} {
		if strings.Contains(strings.ToLower(string(handoffPayload)), strings.ToLower(forbidden)) {
			t.Fatalf("handoff leaked forbidden value %q: %s", forbidden, handoffPayload)
		}
	}
}

func TestSourceAgentUpdateBridgeCleansTerminalUpgradeThatNeverStartedUpdater(t *testing.T) {
	t.Run("failed after prepare", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		bridge := fixture.open(t)
		command := sourceAgentUpdateNoAttemptCommandForTest(fixture)
		if err := bridge.SaveUpgradeCommandCheckpoint(context.Background(), command); err != nil {
			t.Fatal(err)
		}
		if _, err := bridge.Prepare(context.Background(), command); err != nil {
			t.Fatal(err)
		}
		terminal := sourceAgentUpdateTerminalCommandForTest(
			command, SourceAgentCommandFailed, SourceAgentCommandCodeDownloadFailed, "",
		)
		if err := bridge.RecordServerTerminalObservation(context.Background(), terminal); err != nil {
			t.Fatalf("RecordServerTerminalObservation() error=%v", err)
		}
		assertSourceAgentNoAttemptStateCleaned(t, bridge, fixture)
	})

	t.Run("canceled before prepare", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wcplus-worker")
		bridge := fixture.open(t)
		command := sourceAgentUpdateNoAttemptCommandForTest(fixture)
		if err := bridge.SaveUpgradeCommandCheckpoint(context.Background(), command); err != nil {
			t.Fatal(err)
		}
		terminal := sourceAgentUpdateTerminalCommandForTest(
			command, SourceAgentCommandCanceled, SourceAgentCommandCodeCanceled, "",
		)
		if err := bridge.RecordServerTerminalObservation(context.Background(), terminal); err != nil {
			t.Fatalf("RecordServerTerminalObservation() error=%v", err)
		}
		assertSourceAgentNoAttemptStateCleaned(t, bridge, fixture)
	})
}

func TestSourceAgentUpdateBridgeRefusesNoAttemptCleanupWithAmbiguousEvidence(t *testing.T) {
	t.Run("pending updater", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		bridge := fixture.open(t)
		command := sourceAgentUpdateNoAttemptCommandForTest(fixture)
		if err := bridge.SaveUpgradeCommandCheckpoint(context.Background(), command); err != nil {
			t.Fatal(err)
		}
		request, err := bridge.Prepare(context.Background(), command)
		if err != nil {
			t.Fatal(err)
		}
		if err := bridge.receipts.PublishPending(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		terminal := sourceAgentUpdateTerminalCommandForTest(
			command, SourceAgentCommandCanceled, SourceAgentCommandCodeCanceled, "",
		)
		if err := bridge.RecordServerTerminalObservation(context.Background(), terminal); !errors.Is(err, ErrSourceAgentUpgradeTerminalResolutionInvalid) {
			t.Fatalf("RecordServerTerminalObservation() error=%v", err)
		}
		for _, path := range []string{fixture.stagedPath(), fixture.handoffPath()} {
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("recovery evidence %q was removed: %v", path, err)
			}
		}
		if _, found, err := bridge.receipts.LoadPending(); err != nil || !found {
			t.Fatalf("pending found=%t error=%v", found, err)
		}
		if _, found, err := bridge.LoadUpgradeCommandCheckpoint(); err != nil || !found {
			t.Fatalf("checkpoint found=%t error=%v", found, err)
		}
	})

	t.Run("server success without local outcome", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		bridge := fixture.open(t)
		command := sourceAgentUpdateNoAttemptCommandForTest(fixture)
		if err := bridge.SaveUpgradeCommandCheckpoint(context.Background(), command); err != nil {
			t.Fatal(err)
		}
		terminal := sourceAgentUpdateTerminalCommandForTest(
			command, SourceAgentCommandSucceeded, SourceAgentCommandCodeUpgradeComplete, "2.0.0",
		)
		if err := bridge.RecordServerTerminalObservation(context.Background(), terminal); !errors.Is(err, ErrSourceAgentUpgradeTerminalResolutionInvalid) {
			t.Fatalf("RecordServerTerminalObservation() error=%v", err)
		}
		if _, found, err := bridge.LoadUpgradeCommandCheckpoint(); err != nil || !found {
			t.Fatalf("checkpoint found=%t error=%v", found, err)
		}
	})
}

type sourceAgentBlockingUpdaterActivatorForTest struct {
	started chan struct{}
	release chan struct{}
}

func (a *sourceAgentBlockingUpdaterActivatorForTest) StartUpdater(ctx context.Context) error {
	close(a.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.release:
		return nil
	}
}

func TestSourceAgentUpdateBridgeSerializesPendingPublicationAgainstTerminalCleanup(t *testing.T) {
	fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
	activator := &sourceAgentBlockingUpdaterActivatorForTest{
		started: make(chan struct{}), release: make(chan struct{}),
	}
	config := fixture.config()
	config.Revision = sourceAgentArtifactTestRevision
	config.Activator = activator
	bridge, err := NewSourceAgentUpdateBridge(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close() })
	command := sourceAgentUpdateNoAttemptCommandForTest(fixture)
	if _, err := bridge.Prepare(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	command.State = SourceAgentCommandInstalling
	if err := bridge.SaveUpgradeCommandCheckpoint(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	upgradeDone := make(chan SourceAgentUpgradeResult, 1)
	go func() { upgradeDone <- bridge.Upgrade(context.Background(), command) }()
	select {
	case <-activator.started:
	case <-time.After(time.Second):
		t.Fatal("updater activation did not start")
	}
	terminal := sourceAgentUpdateTerminalCommandForTest(
		command, SourceAgentCommandCanceled, SourceAgentCommandCodeCanceled, "",
	)
	terminalDone := make(chan error, 1)
	go func() { terminalDone <- bridge.RecordServerTerminalObservation(context.Background(), terminal) }()
	select {
	case err := <-terminalDone:
		t.Fatalf("terminal cleanup raced pending activation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(activator.release)
	select {
	case result := <-upgradeDone:
		if !result.Waiting {
			t.Fatalf("upgrade result=%#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("upgrade did not finish")
	}
	select {
	case err := <-terminalDone:
		if !errors.Is(err, ErrSourceAgentUpgradeTerminalResolutionInvalid) {
			t.Fatalf("terminal error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal reconciliation did not resume")
	}
	if _, err := os.Lstat(fixture.stagedPath()); err != nil {
		t.Fatalf("staged artifact was removed: %v", err)
	}
}

func TestSourceAgentUpdateBridgePublishesReadyWhileUpdaterOwnsInstallLock(t *testing.T) {
	fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
	first := fixture.open(t)
	request, err := first.Prepare(context.Background(), fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	journal := sourceAgentUpdateJournal{
		SchemaVersion: sourceAgentUpdateJournalSchema, CommandID: request.CommandID,
		AttemptNonce: strings.Repeat("b", 64), RequestFingerprint: sourceAgentUpdateRequestFingerprint(request),
		WorkerType: request.WorkerType, CurrentVersion: request.CurrentVersion, TargetVersion: request.TargetVersion,
		Platform: request.Platform, Architecture: request.Architecture, ProtocolVersion: request.ProtocolVersion,
		Revision: request.Revision, Channel: request.Channel, Stage: "restart_requested",
		Backup:    SourceAgentBinaryIdentity{Size: 64, SHA256: strings.Repeat("c", 64), Device: 1, Inode: 1},
		StartedAt: "2026-08-01T12:00:01.000000000Z", UpdatedAt: "2026-08-01T12:00:01.000000000Z",
	}
	if err := first.receipts.saveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	lockFD, err := unix.Open(fixture.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(lockFD)
	if err := unix.Flock(lockFD, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewSourceAgentUpdateBridge(fixture.config())
	if err != nil {
		t.Fatalf("restarted worker bridge blocked by updater lock: %v", err)
	}
	defer restarted.Close()
	published, err := restarted.PublishAuthenticatedReady(context.Background(), SourceAgentRuntimeIdentity{
		WorkerType: request.WorkerType, Version: request.TargetVersion,
		Platform: request.Platform, Architecture: request.Architecture,
		ProtocolVersion: request.ProtocolVersion, Revision: request.Revision,
	})
	if err != nil || !published {
		t.Fatalf("ready published=%t error=%v", published, err)
	}
	active := fixture.command
	active.State = SourceAgentCommandRestarting
	if result := restarted.Upgrade(context.Background(), active); !result.Waiting {
		t.Fatalf("locked full bridge result=%#v", result)
	}
	if err := unix.Flock(lockFD, unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if result := restarted.Upgrade(context.Background(), active); !result.Waiting {
		t.Fatalf("unlocked full bridge result=%#v", result)
	}
	if restarted.storage == nil {
		t.Fatal("full bridge did not initialize after updater released install lock")
	}
}

func sourceAgentUpdateNoAttemptCommandForTest(fixture *sourceAgentUpdateBridgeFixtureForTest) SourceAgentCommand {
	command := sourceAgentTerminalResolutionCommand()
	command.ID = fixture.command.ID
	command.State = SourceAgentCommandDownloading
	command.UpgradeSpec.ArtifactID = fixture.downloader.metadata.ID
	return command
}

func sourceAgentUpdateTerminalCommandForTest(command SourceAgentCommand, state, code, actualVersion string) SourceAgentCommand {
	command.State = state
	command.ResultCode = code
	command.ActualVersion = actualVersion
	command.UpdatedAt = "2026-08-01T12:00:03.000000000Z"
	command.CompletedAt = command.UpdatedAt
	return command
}

func assertSourceAgentNoAttemptStateCleaned(
	t *testing.T,
	bridge *SourceAgentUpdateBridge,
	fixture *sourceAgentUpdateBridgeFixtureForTest,
) {
	t.Helper()
	if _, found, err := bridge.LoadUpgradeCommandCheckpoint(); err != nil || found {
		t.Fatalf("checkpoint found=%t error=%v", found, err)
	}
	for _, path := range []string{
		fixture.stagedPath(),
		fixture.handoffPath(),
		filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName, sourceAgentUpgradeTerminalObservationFileName),
		filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName, sourceAgentUpgradeNoAttemptCleanupFileName),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("no-attempt evidence %q survives cleanup: %v", path, err)
		}
	}
	workerName, _ := sourceAgentUpdateWorkerBasename(fixture.workerType)
	worker, err := os.ReadFile(filepath.Join(fixture.root, workerName))
	if err != nil || string(worker) != "current worker binary" {
		t.Fatalf("worker changed during no-attempt cleanup: %q error=%v", worker, err)
	}
}

func TestSourceAgentUpdateBridgeRetryAndCrashBoundaries(t *testing.T) {
	t.Run("exact retry reuses durable handoff without download", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		first := fixture.open(t)
		want, err := first.Prepare(context.Background(), fixture.command)
		if err != nil {
			t.Fatal(err)
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		second := fixture.open(t)
		got, err := second.Prepare(context.Background(), fixture.command)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("retry request=%#v want=%#v err=%v", got, want, err)
		}
		if fixture.downloader.callCount() != 1 {
			t.Fatalf("downloads=%d want=1", fixture.downloader.callCount())
		}
	})

	t.Run("crash after stage reuses only exact staged identity", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wcplus-worker")
		first := fixture.open(t)
		first.faultAt = "after_stage"
		if _, err := first.Prepare(context.Background(), fixture.command); !errors.Is(err, errSourceAgentUpdateBridgeUnavailable) {
			t.Fatalf("Prepare() error=%v", err)
		}
		_ = first.Close()
		if _, err := os.Lstat(fixture.handoffPath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("handoff exists after stage fault: %v", err)
		}
		second := fixture.open(t)
		if _, err := second.Prepare(context.Background(), fixture.command); err != nil {
			t.Fatal(err)
		}
		if fixture.downloader.callCount() != 2 {
			t.Fatalf("downloads=%d want=2", fixture.downloader.callCount())
		}
	})

	t.Run("crash after handoff reuses committed handoff", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		first := fixture.open(t)
		first.faultAt = "after_handoff"
		if _, err := first.Prepare(context.Background(), fixture.command); !errors.Is(err, errSourceAgentUpdateBridgeUnavailable) {
			t.Fatalf("Prepare() error=%v", err)
		}
		_ = first.Close()
		second := fixture.open(t)
		if _, err := second.Prepare(context.Background(), fixture.command); err != nil {
			t.Fatal(err)
		}
		if fixture.downloader.callCount() != 1 {
			t.Fatalf("downloads=%d want=1", fixture.downloader.callCount())
		}
	})

	t.Run("mutated staged file fails closed without redownload", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		first := fixture.open(t)
		if _, err := first.Prepare(context.Background(), fixture.command); err != nil {
			t.Fatal(err)
		}
		_ = first.Close()
		mutated := bytes.Repeat([]byte{'x'}, len(fixture.artifact))
		if err := os.WriteFile(fixture.stagedPath(), mutated, 0o755); err != nil {
			t.Fatal(err)
		}
		second := fixture.open(t)
		if _, err := second.Prepare(context.Background(), fixture.command); !errors.Is(err, errSourceAgentUpdateHandoffConflict) {
			t.Fatalf("Prepare() error=%v", err)
		}
		if fixture.downloader.callCount() != 1 {
			t.Fatalf("downloads=%d want=1", fixture.downloader.callCount())
		}
	})

	t.Run("durable handoff cannot be reused for another command", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		first := fixture.open(t)
		if _, err := first.Prepare(context.Background(), fixture.command); err != nil {
			t.Fatal(err)
		}
		_ = first.Close()
		conflicting := fixture.command
		conflicting.ID = "command-2"
		second := fixture.open(t)
		if _, err := second.Prepare(context.Background(), conflicting); !errors.Is(err, errSourceAgentUpdateHandoffConflict) {
			t.Fatalf("Prepare() error=%v", err)
		}
		if fixture.downloader.callCount() != 1 {
			t.Fatalf("downloads=%d want=1", fixture.downloader.callCount())
		}
	})

	t.Run("staged-only retry requires exact downloaded identity", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wcplus-worker")
		first := fixture.open(t)
		first.faultAt = "after_stage"
		if _, err := first.Prepare(context.Background(), fixture.command); err == nil {
			t.Fatal("faulted Prepare() succeeded")
		}
		_ = first.Close()
		fixture.downloader.mu.Lock()
		fixture.downloader.body = []byte("different staged artifact")
		fixture.downloader.metadata.Size = int64(len(fixture.downloader.body))
		fixture.downloader.metadata.SHA256 = sha256HexForTest(fixture.downloader.body)
		fixture.downloader.metadata.Version = "2.1.0"
		fixture.downloader.mu.Unlock()
		second := fixture.open(t)
		if _, err := second.Prepare(context.Background(), fixture.command); !errors.Is(err, errSourceAgentUpdateHandoffConflict) {
			t.Fatalf("Prepare() error=%v", err)
		}
		if _, err := os.Lstat(fixture.handoffPath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("conflicting retry published handoff: %v", err)
		}
	})
}

func TestSourceAgentUpdateBridgeRejectsNonExactHandoff(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "unknown field", mutate: func(payload []byte) []byte {
			return append(payload[:len(payload)-1], []byte(`,"path":"/tmp/attacker"}`)...)
		}},
		{name: "duplicate field", mutate: func(payload []byte) []byte {
			return append(payload[:len(payload)-1], []byte(`,"command_id":"other"}`)...)
		}},
		{name: "trailing document", mutate: func(payload []byte) []byte { return append(payload, []byte(` {}`)...) }},
		{name: "oversized", mutate: func([]byte) []byte { return bytes.Repeat([]byte{'x'}, sourceAgentUpdateReceiptMaxBytes+1) }},
		{name: "wrong staged basename", mutate: mutateSourceAgentUpdateHandoffForTest("staged_basename", "source-agent-updater")},
		{name: "wrong fingerprint", mutate: mutateSourceAgentUpdateHandoffForTest("request_fingerprint", strings.Repeat("f", 64))},
		{name: "embedded staged path", mutate: mutateSourceAgentUpdateHandoffForTest("staged_basename", "/tmp/worker.staged")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			first := fixture.open(t)
			if _, err := first.Prepare(context.Background(), fixture.command); err != nil {
				t.Fatal(err)
			}
			_ = first.Close()
			payload, err := os.ReadFile(fixture.handoffPath())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture.handoffPath(), test.mutate(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			second := fixture.open(t)
			if _, err := second.Prepare(context.Background(), fixture.command); !errors.Is(err, errSourceAgentUpdateHandoffConflict) {
				t.Fatalf("Prepare() error=%v", err)
			}
			if fixture.downloader.callCount() != 1 {
				t.Fatalf("downloads=%d want=1", fixture.downloader.callCount())
			}
		})
	}
}

func TestSourceAgentUpdateBridgeRepairsPublishedTargetsAfterFaults(t *testing.T) {
	tests := []struct {
		name          string
		target        string
		injectFailure func(*darwinSourceAgentUpdateBridgeStorage)
		wantDownloads int
		wantPending   bool
	}{
		{
			name: "staged directory fsync", target: "stage", wantDownloads: 2,
			injectFailure: func(storage *darwinSourceAgentUpdateBridgeStorage) {
				failed := false
				storage.syncFD = func(fd int) error {
					if fd == storage.stagingFD && !failed {
						failed = true
						return unix.EIO
					}
					return unix.Fsync(fd)
				}
			},
		},
		{
			name: "handoff directory fsync", target: "handoff", wantDownloads: 1,
			injectFailure: func(storage *darwinSourceAgentUpdateBridgeStorage) {
				failed := false
				storage.syncFD = func(fd int) error {
					if fd == storage.handoffFD && !failed {
						failed = true
						return unix.EIO
					}
					return unix.Fsync(fd)
				}
			},
		},
		{
			name: "staged temporary unlink", target: "stage", wantDownloads: 2, wantPending: true,
			injectFailure: func(storage *darwinSourceAgentUpdateBridgeStorage) {
				storage.unlinkat = func(fd int, name string, flags int) error {
					if fd == storage.stagingFD && name == sourceAgentUpdateStagedPendingName {
						return unix.EIO
					}
					return unix.Unlinkat(fd, name, flags)
				}
			},
		},
		{
			name: "handoff temporary unlink", target: "handoff", wantDownloads: 1, wantPending: true,
			injectFailure: func(storage *darwinSourceAgentUpdateBridgeStorage) {
				storage.unlinkat = func(fd int, name string, flags int) error {
					if fd == storage.handoffFD && name == sourceAgentUpdateHandoffPendingName {
						return unix.EIO
					}
					return unix.Unlinkat(fd, name, flags)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			bridge := fixture.open(t)
			storage := bridge.storage.(*darwinSourceAgentUpdateBridgeStorage)
			test.injectFailure(storage)
			if _, err := bridge.Prepare(context.Background(), fixture.command); !errors.Is(err, errSourceAgentUpdateBridgeUnavailable) {
				t.Fatalf("faulted Prepare() error=%v", err)
			}
			if _, err := os.Lstat(fixture.stagedPath()); err != nil {
				t.Fatalf("published staged target missing: %v", err)
			}
			if test.target == "handoff" {
				if _, err := os.Lstat(fixture.handoffPath()); err != nil {
					t.Fatalf("published handoff target missing: %v", err)
				}
			}
			pendingName := sourceAgentUpdateStagedPendingName
			pendingDirectory := sourceAgentUpdateStagingDirectoryName
			if test.target == "handoff" {
				pendingName = sourceAgentUpdateHandoffPendingName
				pendingDirectory = sourceAgentUpdateHandoffDirectoryName
			}
			pendingPath := filepath.Join(fixture.root, pendingDirectory, pendingName)
			if test.wantPending {
				if _, err := os.Lstat(pendingPath); err != nil {
					t.Fatalf("fault did not preserve pending file: %v", err)
				}
			}
			storage.syncFD = unix.Fsync
			storage.unlinkat = unix.Unlinkat
			if _, err := bridge.Prepare(context.Background(), fixture.command); err != nil {
				t.Fatalf("retry Prepare(): %v", err)
			}
			if fixture.downloader.callCount() != test.wantDownloads {
				t.Fatalf("downloads=%d want=%d", fixture.downloader.callCount(), test.wantDownloads)
			}
			if _, err := os.Lstat(pendingPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("retry did not clean pending file: %v", err)
			}
		})
	}
}

func TestSourceAgentUpdateBridgeDetectsDirectoryReplacementAfterFixedFileRead(t *testing.T) {
	for _, target := range []string{"handoff", "staging"} {
		t.Run(target, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			first := fixture.open(t)
			if _, err := first.Prepare(context.Background(), fixture.command); err != nil {
				t.Fatal(err)
			}
			_ = first.Close()
			second := fixture.open(t)
			storage := second.storage.(*darwinSourceAgentUpdateBridgeStorage)
			originalRead := storage.readFixedFile
			originalVerify := storage.verifyStagedFile
			replaced := false
			storage.readFixedFile = func(
				fd int, name string, mode uint16, maximum int64, device uint64, afterRead func(int),
			) ([]byte, bool, error) {
				payload, found, err := originalRead(fd, name, mode, maximum, device, afterRead)
				shouldReplace := (target == "handoff" && name == sourceAgentUpdateHandoffFileName) ||
					(target == "staging" && name == sourceAgentUpdateStagedBasename)
				if shouldReplace && !replaced && err == nil && found {
					replaced = true
					directoryName := sourceAgentUpdateHandoffDirectoryName
					if target == "staging" {
						directoryName = sourceAgentUpdateStagingDirectoryName
					}
					current := filepath.Join(fixture.root, directoryName)
					if renameErr := os.Rename(current, current+".replaced"); renameErr != nil {
						t.Fatal(renameErr)
					}
					if mkdirErr := os.Mkdir(current, 0o700); mkdirErr != nil {
						t.Fatal(mkdirErr)
					}
				}
				return payload, found, err
			}
			storage.verifyStagedFile = func(
				fd int, name string, size int64, digest string, device uint64, afterRead func(int),
			) (bool, error) {
				found, err := originalVerify(fd, name, size, digest, device, afterRead)
				if target == "staging" && !replaced && err == nil && found {
					replaced = true
					current := filepath.Join(fixture.root, sourceAgentUpdateStagingDirectoryName)
					if renameErr := os.Rename(current, current+".replaced"); renameErr != nil {
						t.Fatal(renameErr)
					}
					if mkdirErr := os.Mkdir(current, 0o700); mkdirErr != nil {
						t.Fatal(mkdirErr)
					}
				}
				return found, err
			}
			if _, err := second.Prepare(context.Background(), fixture.command); err == nil {
				t.Fatal("Prepare() accepted a replaced fixed directory")
			}
			if !replaced {
				t.Fatal("test did not replace directory after fixed-file read")
			}
		})
	}
}

func TestSourceAgentUpdateBridgeDetectsSameInodeHandoffMutationDuringRead(t *testing.T) {
	fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
	first := fixture.open(t)
	if _, err := first.Prepare(context.Background(), fixture.command); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	second := fixture.open(t)
	storage := second.storage.(*darwinSourceAgentUpdateBridgeStorage)
	mutated := false
	storage.afterFixedRead = func(name string, _ int) {
		if name != sourceAgentUpdateHandoffFileName || mutated {
			return
		}
		mutated = true
		file, err := os.OpenFile(fixture.handoffPath(), os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		payload, err := os.ReadFile(fixture.handoffPath())
		if err != nil {
			t.Fatal(err)
		}
		offset := int64(len(payload) / 2)
		replacement := byte('x')
		if payload[offset] == replacement {
			replacement = 'y'
		}
		if _, err := file.WriteAt([]byte{replacement}, offset); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := second.Prepare(context.Background(), fixture.command); !errors.Is(err, errSourceAgentUpdateHandoffConflict) {
		t.Fatalf("Prepare() error=%v", err)
	}
	if !mutated {
		t.Fatal("test did not mutate handoff between content read and final fstat")
	}
	if fixture.downloader.callCount() != 1 {
		t.Fatalf("downloads=%d want=1", fixture.downloader.callCount())
	}
}

func TestSourceAgentUpdateBridgeDetectsFixedBasenameReplacementDuringRead(t *testing.T) {
	for _, target := range []string{"handoff", "staged"} {
		t.Run(target, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			first := fixture.open(t)
			if _, err := first.Prepare(context.Background(), fixture.command); err != nil {
				t.Fatal(err)
			}
			_ = first.Close()
			second := fixture.open(t)
			storage := second.storage.(*darwinSourceAgentUpdateBridgeStorage)
			replaced := false
			replace := func(path string, mode os.FileMode) {
				if replaced {
					return
				}
				replaced = true
				payload, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(path, path+".replaced"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, len(payload)), mode); err != nil {
					t.Fatal(err)
				}
			}
			if target == "handoff" {
				storage.afterFixedRead = func(name string, _ int) {
					if name == sourceAgentUpdateHandoffFileName {
						replace(fixture.handoffPath(), 0o600)
					}
				}
			} else {
				storage.afterStagedRead = func(name string, _ int) {
					if name == sourceAgentUpdateStagedBasename {
						replace(fixture.stagedPath(), 0o755)
					}
				}
			}
			if _, err := second.Prepare(context.Background(), fixture.command); err == nil {
				t.Fatal("Prepare() accepted a fixed basename replacement during verification")
			}
			if !replaced {
				t.Fatal("test did not replace the fixed basename")
			}
			if fixture.downloader.callCount() != 1 {
				t.Fatalf("downloads=%d want=1", fixture.downloader.callCount())
			}
		})
	}
}

func TestSourceAgentUpdateBridgeConstructorRequiresPrivatePinnedInstall(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *sourceAgentUpdateBridgeFixtureForTest) string
	}{
		{name: "install root mode", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) string {
			if err := os.Chmod(fixture.root, 0o755); err != nil {
				t.Fatal(err)
			}
			return fixture.updater
		}},
		{name: "worker group writable", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) string {
			worker, _ := sourceAgentUpdateWorkerBasename(fixture.workerType)
			if err := os.Chmod(filepath.Join(fixture.root, worker), 0o775); err != nil {
				t.Fatal(err)
			}
			return fixture.updater
		}},
		{name: "updater world writable", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) string {
			if err := os.Chmod(fixture.updater, 0o757); err != nil {
				t.Fatal(err)
			}
			return fixture.updater
		}},
		{name: "worker symlink", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) string {
			worker, _ := sourceAgentUpdateWorkerBasename(fixture.workerType)
			workerPath := filepath.Join(fixture.root, worker)
			if err := os.Rename(workerPath, workerPath+".real"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(workerPath+".real", workerPath); err != nil {
				t.Fatal(err)
			}
			return fixture.updater
		}},
		{name: "updater symlink", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) string {
			if err := os.Rename(fixture.updater, fixture.updater+".real"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(fixture.updater+".real", fixture.updater); err != nil {
				t.Fatal(err)
			}
			return fixture.updater
		}},
		{name: "staging directory symlink", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) string {
			target := filepath.Join(fixture.root, "staging-target")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(fixture.root, sourceAgentUpdateStagingDirectoryName)); err != nil {
				t.Fatal(err)
			}
			return fixture.updater
		}},
		{name: "handoff directory symlink", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) string {
			target := filepath.Join(fixture.root, "handoff-target")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName)); err != nil {
				t.Fatal(err)
			}
			return fixture.updater
		}},
		{name: "ancestor component symlink", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) string {
			alias := filepath.Join(filepath.Dir(fixture.root), "install-alias")
			if err := os.Symlink(fixture.root, alias); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(alias, sourceAgentUpdateUpdaterBasename)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			updater := test.mutate(t, fixture)
			config := fixture.config()
			config.UpdaterExecutable = updater
			bridge, err := NewSourceAgentUpdateBridge(config)
			if err == nil {
				_ = bridge.Close()
				t.Fatal("constructor accepted unsafe install layout")
			}
		})
	}
}

func TestSourceAgentUpdateBridgeRejectsPinnedDeviceAndWholeRootReplacement(t *testing.T) {
	t.Run("device identity", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		bridge := fixture.open(t)
		storage := bridge.storage.(*darwinSourceAgentUpdateBridgeStorage)
		storage.workerDevice++
		if _, err := bridge.Prepare(context.Background(), fixture.command); err == nil {
			t.Fatal("Prepare() accepted a device identity mismatch")
		}
		if fixture.downloader.callCount() != 0 {
			t.Fatalf("downloads=%d want=0", fixture.downloader.callCount())
		}
	})

	t.Run("whole install root", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wcplus-worker")
		bridge := fixture.open(t)
		moved := fixture.root + ".moved"
		if err := os.Rename(fixture.root, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.root, 0o700); err != nil {
			t.Fatal(err)
		}
		worker, _ := sourceAgentUpdateWorkerBasename(fixture.workerType)
		for _, name := range []string{worker, sourceAgentUpdateUpdaterBasename} {
			if err := os.WriteFile(filepath.Join(fixture.root, name), []byte("replacement"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		sentinel := filepath.Join(fixture.root, "replacement-sentinel")
		if err := os.WriteFile(sentinel, []byte("untouched"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := bridge.Prepare(context.Background(), fixture.command); err == nil {
			t.Fatal("Prepare() accepted a replaced install root")
		}
		got, err := os.ReadFile(sentinel)
		if err != nil || string(got) != "untouched" {
			t.Fatalf("replacement root was modified: %q err=%v", got, err)
		}
	})
}

func TestSourceAgentUpdateBridgeUsesBoundedPrepareLockAndCleansOrphans(t *testing.T) {
	t.Run("Prepare lock is released before handoff", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		bridge := fixture.open(t)
		baseDownloader := fixture.downloader
		lockObserved := false
		bridge.config.Downloader = sourceAgentArtifactDownloaderFunc(func(
			ctx context.Context,
			command SourceAgentCommand,
			target SourceAgentArtifactTarget,
			protocol string,
		) (SourceAgentArtifactPublic, io.ReadCloser, error) {
			probeFD, err := unix.Open(
				filepath.Join(fixture.root, sourceAgentUpdateLifecycleFileName),
				unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
				0,
			)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(probeFD)
			lockErr := unix.Flock(probeFD, unix.LOCK_EX|unix.LOCK_NB)
			lockObserved = errors.Is(lockErr, unix.EWOULDBLOCK)
			if lockErr == nil {
				_ = unix.Flock(probeFD, unix.LOCK_UN)
			}
			return baseDownloader.DownloadArtifact(ctx, command, target, protocol)
		})
		if _, err := bridge.Prepare(context.Background(), fixture.command); err != nil {
			t.Fatal(err)
		}
		if !lockObserved {
			t.Fatal("Prepare did not hold the lifecycle lock during download/publish")
		}
		probeFD, err := unix.Open(
			filepath.Join(fixture.root, sourceAgentUpdateLifecycleFileName),
			unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(probeFD)
		if err := unix.Flock(probeFD, unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatalf("updater cannot acquire lock after Prepare: %v", err)
		}
		_ = unix.Flock(probeFD, unix.LOCK_UN)
	})

	t.Run("fixed orphan pending files are removed on reopen", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		first := fixture.open(t)
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		pendingPaths := []string{
			filepath.Join(fixture.root, sourceAgentUpdateStagingDirectoryName, sourceAgentUpdateStagedPendingName),
			filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName, sourceAgentUpdateHandoffPendingName),
		}
		for _, pending := range pendingPaths {
			if err := os.WriteFile(pending, []byte("crash orphan"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		second := fixture.open(t)
		for _, pending := range pendingPaths {
			if _, err := os.Lstat(pending); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("pending file %q survives reopen: %v", pending, err)
			}
		}
		if _, err := second.Prepare(context.Background(), fixture.command); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unsafe pending symlink fails closed", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		first := fixture.open(t)
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(fixture.root, "pending-target")
		if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
			t.Fatal(err)
		}
		pending := filepath.Join(fixture.root, sourceAgentUpdateStagingDirectoryName, sourceAgentUpdateStagedPendingName)
		if err := os.Symlink(target, pending); err != nil {
			t.Fatal(err)
		}
		if bridge, err := NewSourceAgentUpdateBridge(fixture.config()); err == nil {
			_ = bridge.Close()
			t.Fatal("constructor removed an unsafe pending symlink")
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != "untouched" {
			t.Fatalf("pending target was modified: %q err=%v", got, err)
		}
	})
}

func TestSourceAgentUpdateBridgeRejectsInexactOrInterruptedArtifactBodies(t *testing.T) {
	injectedReadError := errors.New("injected artifact read failure")
	tests := []struct {
		name      string
		configure func(context.Context, context.CancelFunc, *sourceAgentUpdateBridgeFixtureForTest) io.ReadCloser
		mutate    func(*SourceAgentArtifactPublic)
	}{
		{name: "short", configure: func(_ context.Context, _ context.CancelFunc, fixture *sourceAgentUpdateBridgeFixtureForTest) io.ReadCloser {
			return io.NopCloser(bytes.NewReader(fixture.artifact[:len(fixture.artifact)-1]))
		}},
		{name: "long", configure: func(_ context.Context, _ context.CancelFunc, fixture *sourceAgentUpdateBridgeFixtureForTest) io.ReadCloser {
			return io.NopCloser(bytes.NewReader(append(append([]byte(nil), fixture.artifact...), 'x')))
		}},
		{name: "wrong sha", configure: func(_ context.Context, _ context.CancelFunc, fixture *sourceAgentUpdateBridgeFixtureForTest) io.ReadCloser {
			return io.NopCloser(bytes.NewReader(fixture.artifact))
		}, mutate: func(metadata *SourceAgentArtifactPublic) { metadata.SHA256 = strings.Repeat("0", 64) }},
		{name: "reader error", configure: func(_ context.Context, _ context.CancelFunc, fixture *sourceAgentUpdateBridgeFixtureForTest) io.ReadCloser {
			return io.NopCloser(&sourceAgentUpdateFailingReaderForTest{payload: fixture.artifact[:4], err: injectedReadError})
		}},
		{name: "canceled during read", configure: func(_ context.Context, cancel context.CancelFunc, fixture *sourceAgentUpdateBridgeFixtureForTest) io.ReadCloser {
			return io.NopCloser(&sourceAgentUpdateCancelingReaderForTest{payload: fixture.artifact, cancel: cancel})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			metadata := fixture.downloader.metadata
			if test.mutate != nil {
				test.mutate(&metadata)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := test.configure(ctx, cancel, fixture)
			bridge, err := NewSourceAgentUpdateBridge(func() SourceAgentUpdateBridgeConfig {
				config := fixture.config()
				config.Downloader = sourceAgentArtifactDownloaderFunc(func(
					context.Context, SourceAgentCommand, SourceAgentArtifactTarget, string,
				) (SourceAgentArtifactPublic, io.ReadCloser, error) {
					return metadata, body, nil
				})
				return config
			}())
			if err != nil {
				t.Fatal(err)
			}
			defer bridge.Close()
			if _, err := bridge.Prepare(ctx, fixture.command); err == nil {
				t.Fatal("Prepare() accepted an inexact or interrupted artifact")
			}
			for _, fixed := range []string{fixture.stagedPath(), fixture.handoffPath()} {
				if _, err := os.Lstat(fixed); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("fixed target %q was published: %v", fixed, err)
				}
			}
			for _, directory := range []string{
				filepath.Join(fixture.root, sourceAgentUpdateStagingDirectoryName),
				filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName),
			} {
				entries, err := os.ReadDir(directory)
				if err != nil || len(entries) != 0 {
					t.Fatalf("partial entries in %q: %#v err=%v", directory, entries, err)
				}
			}
		})
	}

	t.Run("reader is bounded to declared size plus one byte", func(t *testing.T) {
		fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
		reader := &sourceAgentUpdateCountingReaderForTest{}
		metadata := fixture.downloader.metadata
		metadata.SHA256 = sha256HexForTest(bytes.Repeat([]byte{'a'}, int(metadata.Size)))
		config := fixture.config()
		config.Downloader = sourceAgentArtifactDownloaderFunc(func(
			context.Context, SourceAgentCommand, SourceAgentArtifactTarget, string,
		) (SourceAgentArtifactPublic, io.ReadCloser, error) {
			return metadata, io.NopCloser(reader), nil
		})
		bridge, err := NewSourceAgentUpdateBridge(config)
		if err != nil {
			t.Fatal(err)
		}
		defer bridge.Close()
		if _, err := bridge.Prepare(context.Background(), fixture.command); err == nil {
			t.Fatal("Prepare() accepted an unbounded long body")
		}
		if reader.read != metadata.Size+1 {
			t.Fatalf("reader consumed %d bytes want=%d", reader.read, metadata.Size+1)
		}
	})
}

func TestSourceAgentUpdateBridgeNeverTargetsArtifactNamedPaths(t *testing.T) {
	for _, artifactID := range []string{
		"source-agent-updater", "source-agent", "wcplus-agent", "config.json", "worker-state.db", "com.example.worker.plist",
	} {
		t.Run(artifactID, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			fixture.command.UpgradeSpec.ArtifactID = artifactID
			fixture.downloader.metadata.ID = artifactID
			worker, _ := sourceAgentUpdateWorkerBasename(fixture.workerType)
			paths := []string{
				filepath.Join(fixture.root, worker),
				fixture.updater,
				filepath.Join(fixture.root, "wcplus-agent"),
				filepath.Join(fixture.root, "config.json"),
				filepath.Join(fixture.root, "worker-state.db"),
				filepath.Join(fixture.root, "com.example.worker.plist"),
			}
			for _, path := range paths[2:] {
				if err := os.WriteFile(path, []byte("protected:"+filepath.Base(path)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := make(map[string][]byte, len(paths))
			for _, path := range paths {
				payload, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = payload
			}
			bridge := fixture.open(t)
			request, err := bridge.Prepare(context.Background(), fixture.command)
			if err != nil {
				t.Fatal(err)
			}
			if request.ArtifactID != artifactID || request.StagedBinary != fixture.stagedPath() {
				t.Fatalf("request=%#v", request)
			}
			for path, want := range before {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("protected path %q changed: %q want=%q err=%v", path, got, want, err)
				}
			}
		})
	}
}

func TestSourceAgentUpdateBridgeRejectsUnsafeFixedFilesWithoutFollowing(t *testing.T) {
	for _, target := range []string{"staged symlink", "staged fifo", "handoff symlink"} {
		t.Run(target, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			first := fixture.open(t)
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(fixture.root, "external-target")
			if err := os.WriteFile(external, []byte("untouched"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch target {
			case "staged symlink":
				if err := os.Symlink(external, fixture.stagedPath()); err != nil {
					t.Fatal(err)
				}
			case "staged fifo":
				if err := unix.Mkfifo(fixture.stagedPath(), 0o600); err != nil {
					t.Fatal(err)
				}
			case "handoff symlink":
				if err := os.Symlink(external, fixture.handoffPath()); err != nil {
					t.Fatal(err)
				}
			}
			second := fixture.open(t)
			if _, err := second.Prepare(context.Background(), fixture.command); err == nil {
				t.Fatal("Prepare() accepted an unsafe fixed entry")
			}
			got, err := os.ReadFile(external)
			if err != nil || string(got) != "untouched" {
				t.Fatalf("external target changed: %q err=%v", got, err)
			}
		})
	}
}

func TestSourceAgentUpdateBridgeRejectsPendingReplacementAtPublish(t *testing.T) {
	for _, target := range []string{"staged", "handoff"} {
		t.Run(target, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			bridge := fixture.open(t)
			storage := bridge.storage.(*darwinSourceAgentUpdateBridgeStorage)
			external := filepath.Join(fixture.root, "publish-target")
			if err := os.WriteFile(external, []byte("untouched"), 0o600); err != nil {
				t.Fatal(err)
			}
			originalLink := storage.linkat
			replaced := false
			storage.linkat = func(oldDirectoryFD int, oldName string, newDirectoryFD int, newName string, flags int) error {
				pendingName := sourceAgentUpdateStagedPendingName
				if target == "handoff" {
					pendingName = sourceAgentUpdateHandoffPendingName
				}
				if oldName == pendingName && !replaced {
					replaced = true
					if err := unix.Unlinkat(oldDirectoryFD, oldName, 0); err != nil {
						t.Fatal(err)
					}
					if err := unix.Symlinkat(external, oldDirectoryFD, oldName); err != nil {
						t.Fatal(err)
					}
				}
				return originalLink(oldDirectoryFD, oldName, newDirectoryFD, newName, flags)
			}
			if _, err := bridge.Prepare(context.Background(), fixture.command); err == nil {
				t.Fatal("Prepare() accepted a publish-time pending replacement")
			}
			if !replaced {
				t.Fatal("test did not replace the pending entry at publish")
			}
			got, err := os.ReadFile(external)
			if err != nil || string(got) != "untouched" {
				t.Fatalf("external target changed: %q err=%v", got, err)
			}
			fixed := fixture.stagedPath()
			if target == "handoff" {
				fixed = fixture.handoffPath()
			}
			if _, err := os.Lstat(fixed); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe fixed target survived: %v", err)
			}
		})
	}
}

func TestSourceAgentUpdateBridgeStageDoesNotLetFinalizerCloseReusedFD(t *testing.T) {
	fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
	fixture.downloader.mu.Lock()
	fixture.downloader.body = fixture.artifact[:len(fixture.artifact)-1]
	fixture.downloader.mu.Unlock()
	bridge := fixture.open(t)

	gcPercent := debug.SetGCPercent(-1)
	gcRestored := false
	defer func() {
		if !gcRestored {
			debug.SetGCPercent(gcPercent)
		}
	}()
	candidateFD, err := unix.Open(fixture.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Close(candidateFD); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Prepare(context.Background(), fixture.command); err == nil {
		t.Fatal("Prepare() accepted a truncated artifact")
	}
	probeFD, err := unix.Open(fixture.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(probeFD)
	if probeFD != candidateFD {
		t.Fatalf("test setup did not reuse staged fd: probe=%d candidate=%d", probeFD, candidateFD)
	}
	if err := unix.Flock(probeFD, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	debug.SetGCPercent(gcPercent)
	gcRestored = true
	for attempt := 0; attempt < 10; attempt++ {
		runtime.GC()
		runtime.Gosched()
	}
	var stat unix.Stat_t
	if err := unix.Fstat(probeFD, &stat); err != nil {
		t.Fatalf("probe fd was closed by a stale os.File finalizer: %v", err)
	}
	if err := unix.Flock(probeFD, unix.LOCK_UN); err != nil {
		t.Fatalf("probe lock was invalidated by a stale os.File finalizer: %v", err)
	}
}

func TestSourceAgentUpdateBridgeRejectsSpecialPermissionBits(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *sourceAgentUpdateBridgeFixtureForTest) bool
	}{
		{name: "install root sticky", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) bool {
			return sourceAgentUpdateSetSpecialModeForTest(t, fixture.root, 0o1700)
		}},
		{name: "install root setgid", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) bool {
			return sourceAgentUpdateSetSpecialModeForTest(t, fixture.root, 0o2700)
		}},
		{name: "worker setuid", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) bool {
			worker, _ := sourceAgentUpdateWorkerBasename(fixture.workerType)
			return sourceAgentUpdateSetSpecialModeForTest(t, filepath.Join(fixture.root, worker), 0o4755)
		}},
		{name: "updater setgid", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) bool {
			return sourceAgentUpdateSetSpecialModeForTest(t, fixture.updater, 0o2755)
		}},
		{name: "staging directory sticky", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) bool {
			if err := os.Mkdir(filepath.Join(fixture.root, sourceAgentUpdateStagingDirectoryName), 0o700); err != nil {
				t.Fatal(err)
			}
			return sourceAgentUpdateSetSpecialModeForTest(
				t, filepath.Join(fixture.root, sourceAgentUpdateStagingDirectoryName), 0o1700,
			)
		}},
		{name: "handoff directory setgid", mutate: func(t *testing.T, fixture *sourceAgentUpdateBridgeFixtureForTest) bool {
			if err := os.Mkdir(filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName), 0o700); err != nil {
				t.Fatal(err)
			}
			return sourceAgentUpdateSetSpecialModeForTest(
				t, filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName), 0o2700,
			)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			if !test.mutate(t, fixture) {
				t.Skip("filesystem did not preserve requested special permission bit")
			}
			if bridge, err := NewSourceAgentUpdateBridge(fixture.config()); err == nil {
				_ = bridge.Close()
				t.Fatal("constructor accepted special permission bits")
			}
		})
	}

	for _, target := range []string{"staged pending setuid", "handoff pending setgid", "staged fixed setuid", "handoff fixed setgid"} {
		t.Run(target, func(t *testing.T) {
			fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
			first := fixture.open(t)
			if strings.Contains(target, "fixed") {
				if _, err := first.Prepare(context.Background(), fixture.command); err != nil {
					t.Fatal(err)
				}
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			path := fixture.stagedPath()
			mode := uint32(0o4755)
			if target == "staged pending setuid" {
				path = filepath.Join(fixture.root, sourceAgentUpdateStagingDirectoryName, sourceAgentUpdateStagedPendingName)
				mode = 0o4600
				if err := os.WriteFile(path, []byte("pending"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if target == "handoff pending setgid" {
				path = filepath.Join(fixture.root, sourceAgentUpdateHandoffDirectoryName, sourceAgentUpdateHandoffPendingName)
				mode = 0o2600
				if err := os.WriteFile(path, []byte("pending"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if target == "handoff fixed setgid" {
				path = fixture.handoffPath()
				mode = 0o2600
			}
			if !sourceAgentUpdateSetSpecialModeForTest(t, path, mode) {
				t.Skip("filesystem did not preserve requested special permission bit")
			}
			second, err := NewSourceAgentUpdateBridge(fixture.config())
			if strings.Contains(target, "pending") {
				if err == nil {
					_ = second.Close()
					t.Fatal("constructor accepted a special-mode pending file")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			if _, err := second.Prepare(context.Background(), fixture.command); err == nil {
				t.Fatal("Prepare() accepted a special-mode fixed file")
			}
		})
	}
}

func sourceAgentUpdateSetSpecialModeForTest(t *testing.T, path string, mode uint32) bool {
	t.Helper()
	if err := unix.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		t.Fatal(err)
	}
	return stat.Mode&uint16(unix.S_ISUID|unix.S_ISGID|unix.S_ISVTX) != 0
}

type sourceAgentUpdateFailingReaderForTest struct {
	payload []byte
	err     error
}

func (r *sourceAgentUpdateFailingReaderForTest) Read(buffer []byte) (int, error) {
	if len(r.payload) == 0 {
		return 0, r.err
	}
	written := copy(buffer, r.payload)
	r.payload = r.payload[written:]
	return written, nil
}

type sourceAgentUpdateCancelingReaderForTest struct {
	payload []byte
	cancel  context.CancelFunc
	read    bool
}

func (r *sourceAgentUpdateCancelingReaderForTest) Read(buffer []byte) (int, error) {
	if !r.read {
		r.read = true
		r.cancel()
		buffer[0] = r.payload[0]
		r.payload = r.payload[1:]
		return 1, nil
	}
	return copy(buffer, r.payload), nil
}

type sourceAgentUpdateCountingReaderForTest struct {
	read int64
}

func (r *sourceAgentUpdateCountingReaderForTest) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 'a'
	}
	r.read += int64(len(buffer))
	return len(buffer), nil
}

type sourceAgentArtifactDownloaderFunc func(
	context.Context,
	SourceAgentCommand,
	SourceAgentArtifactTarget,
	string,
) (SourceAgentArtifactPublic, io.ReadCloser, error)

func (f sourceAgentArtifactDownloaderFunc) DownloadArtifact(
	ctx context.Context,
	command SourceAgentCommand,
	target SourceAgentArtifactTarget,
	protocol string,
) (SourceAgentArtifactPublic, io.ReadCloser, error) {
	return f(ctx, command, target, protocol)
}

type sourceAgentUpdateBridgeDownloaderForTest struct {
	mu       sync.Mutex
	metadata SourceAgentArtifactPublic
	body     []byte
	calls    int
}

func (d *sourceAgentUpdateBridgeDownloaderForTest) DownloadArtifact(
	_ context.Context,
	_ SourceAgentCommand,
	_ SourceAgentArtifactTarget,
	_ string,
) (SourceAgentArtifactPublic, io.ReadCloser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return d.metadata, io.NopCloser(bytes.NewReader(d.body)), nil
}

func (d *sourceAgentUpdateBridgeDownloaderForTest) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

type sourceAgentUpdateBridgeFixtureForTest struct {
	root         string
	updater      string
	command      SourceAgentCommand
	artifact     []byte
	downloader   *sourceAgentUpdateBridgeDownloaderForTest
	workerType   string
	architecture string
}

func newSourceAgentUpdateBridgeFixture(t *testing.T, workerType string) *sourceAgentUpdateBridgeFixtureForTest {
	t.Helper()
	root, updater := sourceAgentUpdateBridgeInstallFixture(t, workerType)
	body := []byte("durable staged artifact")
	metadata := SourceAgentArtifactPublic{
		ID: "artifact-1", WorkerType: workerType, Platform: "darwin", Architecture: "arm64",
		Revision: sourceAgentArtifactTestRevision, Version: "2.0.0", ProtocolVersion: "2026-08-01",
		Channel: "staging", Size: int64(len(body)), SHA256: sha256HexForTest(body),
	}
	return &sourceAgentUpdateBridgeFixtureForTest{
		root: root, updater: updater, workerType: workerType, architecture: "arm64", artifact: body,
		downloader: &sourceAgentUpdateBridgeDownloaderForTest{metadata: metadata, body: body},
		command: SourceAgentCommand{
			ID: "command-1", Type: SourceAgentCommandUpgrade, State: SourceAgentCommandDownloading,
			ExpectedCurrentVersion: "1.0.0",
			UpgradeSpec:            &SourceAgentUpgradeSpec{ArtifactID: metadata.ID, ExpectedCurrentVersion: "1.0.0"},
		},
	}
}

func (f *sourceAgentUpdateBridgeFixtureForTest) open(t *testing.T) *SourceAgentUpdateBridge {
	t.Helper()
	bridge, err := NewSourceAgentUpdateBridge(f.config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close() })
	return bridge
}

func (f *sourceAgentUpdateBridgeFixtureForTest) config() SourceAgentUpdateBridgeConfig {
	return SourceAgentUpdateBridgeConfig{
		Downloader: f.downloader, UpdaterExecutable: f.updater, WorkerType: f.workerType,
		CurrentVersion: "1.0.0", Platform: "darwin", Architecture: f.architecture,
		ProtocolVersion: "2026-08-01",
	}
}

func (f *sourceAgentUpdateBridgeFixtureForTest) stagedPath() string {
	return filepath.Join(f.root, sourceAgentUpdateStagingDirectoryName, sourceAgentUpdateStagedBasename)
}

func (f *sourceAgentUpdateBridgeFixtureForTest) handoffPath() string {
	return filepath.Join(f.root, sourceAgentUpdateHandoffDirectoryName, sourceAgentUpdateHandoffFileName)
}

func mutateSourceAgentUpdateHandoffForTest(key string, value any) func([]byte) []byte {
	return func(payload []byte) []byte {
		var document map[string]any
		if err := json.Unmarshal(payload, &document); err != nil {
			panic(err)
		}
		document[key] = value
		mutated, err := json.Marshal(document)
		if err != nil {
			panic(err)
		}
		return mutated
	}
}

func sourceAgentUpdateBridgeInstallFixture(t *testing.T, workerType string) (string, string) {
	t.Helper()
	temporaryRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(temporaryRoot, "installed")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	workerName, ok := sourceAgentUpdateWorkerBasename(workerType)
	if !ok {
		t.Fatalf("unsupported worker type %q", workerType)
	}
	for name, body := range map[string][]byte{
		workerName:                       []byte("current worker binary"),
		sourceAgentUpdateUpdaterBasename: []byte("fixed updater binary"),
	} {
		if err := os.WriteFile(filepath.Join(root, name), body, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root, filepath.Join(root, sourceAgentUpdateUpdaterBasename)
}

func TestSourceAgentUpdateBridgeLifecycleRefusesMaintenanceMarker(t *testing.T) {
	fixture := newSourceAgentUpdateBridgeFixture(t, "wechat-worker")
	bridge := fixture.open(t)
	t.Cleanup(func() { _ = bridge.Close() })
	marker := filepath.Join(fixture.root, sourceAgentUpdateMaintenanceFileName)
	if err := os.WriteFile(marker, []byte("maintenance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := bridge.WithSourceAgentLifecycle(context.Background(), func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrSourceAgentUpdateBusy) {
		t.Fatalf("WithSourceAgentLifecycle() error=%v", err)
	}
	if called {
		t.Fatal("maintenance-blocked lifecycle operation ran")
	}
}

var _ SourceAgentArtifactDownloader = (*SourceAgentClient)(nil)
