package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sourceAgentUpdatePhaseStorage struct {
	payload []byte
	staged  string
}

func (*sourceAgentUpdatePhaseStorage) AcquireLifecycleShared() (func() error, error) {
	return func() error { return nil }, nil
}

func (*sourceAgentUpdatePhaseStorage) Lock() error   { return nil }
func (*sourceAgentUpdatePhaseStorage) Unlock() error { return nil }
func (s *sourceAgentUpdatePhaseStorage) LoadHandoff() ([]byte, bool, error) {
	return append([]byte(nil), s.payload...), len(s.payload) != 0, nil
}
func (*sourceAgentUpdatePhaseStorage) Stage(context.Context, io.Reader, int64, string) error {
	return nil
}
func (*sourceAgentUpdatePhaseStorage) VerifyStaged(int64, string) error { return nil }
func (*sourceAgentUpdatePhaseStorage) PublishHandoff([]byte) error      { return nil }
func (s *sourceAgentUpdatePhaseStorage) StagedPath() string             { return s.staged }
func (*sourceAgentUpdatePhaseStorage) RemoveStaged() error              { return nil }
func (*sourceAgentUpdatePhaseStorage) Close() error                     { return nil }

type sourceAgentUpdatePhaseActivator struct {
	calls int
	err   error
}

func (a *sourceAgentUpdatePhaseActivator) StartUpdater(context.Context) error {
	a.calls++
	return a.err
}

func TestSourceAgentUpdatePhaseMappingWaitsWithoutInventingProgress(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		evidence sourceAgentUpdatePhaseEvidence
	}{
		{name: "download has no handoff", state: SourceAgentCommandDownloading},
		{name: "installer requested without restart", state: SourceAgentCommandInstalling, evidence: sourceAgentUpdatePhaseEvidence{UpdaterRequested: true}},
		{name: "restart has no outcome", state: SourceAgentCommandRestarting, evidence: sourceAgentUpdatePhaseEvidence{JournalStage: "restarted"}},
		{name: "authenticated ready precedes outcome", state: SourceAgentCommandRestarting, evidence: sourceAgentUpdatePhaseEvidence{JournalStage: "restart_requested", ReadyAfterHeartbeat: true}},
		{name: "success lacks runtime proof", state: SourceAgentCommandVerifying, evidence: sourceAgentUpdatePhaseEvidence{Outcome: sourceAgentUpdatePhaseSuccess("2.0.0")}},
		{name: "rollback has no outcome", state: SourceAgentCommandRollback, evidence: sourceAgentUpdatePhaseEvidence{RollbackRequested: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := mapSourceAgentUpdatePhase(test.state, test.evidence)
			if err != nil {
				t.Fatalf("mapSourceAgentUpdatePhase() error = %v", err)
			}
			if !decision.Waiting || decision.Report != (SourceAgentUpgradeResult{}) {
				t.Fatalf("decision = %#v, want waiting without report", decision)
			}
		})
	}
}

func TestSourceAgentUpdatePhaseMappingAdvancesOnlyFromDurableEvidence(t *testing.T) {
	tests := []struct {
		name        string
		state       string
		evidence    sourceAgentUpdatePhaseEvidence
		wantState   string
		wantCode    string
		wantVersion string
	}{
		{name: "claimed", state: SourceAgentCommandClaimed, wantState: SourceAgentCommandDownloading},
		{name: "handoff", state: SourceAgentCommandDownloading, evidence: sourceAgentUpdatePhaseEvidence{HandoffPublished: true}, wantState: SourceAgentCommandVerified},
		{name: "permanent download rejection", state: SourceAgentCommandDownloading, evidence: sourceAgentUpdatePhaseEvidence{PermanentFailureCode: SourceAgentCommandCodeVerificationFailed}, wantState: SourceAgentCommandFailed, wantCode: SourceAgentCommandCodeVerificationFailed},
		{name: "verified handoff", state: SourceAgentCommandVerified, evidence: sourceAgentUpdatePhaseEvidence{HandoffPublished: true}, wantState: SourceAgentCommandInstalling},
		{name: "verified missing handoff", state: SourceAgentCommandVerified, wantState: SourceAgentCommandFailed, wantCode: SourceAgentCommandCodeVerificationFailed},
		{name: "restart requested", state: SourceAgentCommandInstalling, evidence: sourceAgentUpdatePhaseEvidence{UpdaterRequested: true, JournalStage: "restart_requested"}, wantState: SourceAgentCommandRestarting},
		{name: "restarted", state: SourceAgentCommandInstalling, evidence: sourceAgentUpdatePhaseEvidence{UpdaterRequested: true, JournalStage: "restarted"}, wantState: SourceAgentCommandRestarting},
		{name: "ready", state: SourceAgentCommandInstalling, evidence: sourceAgentUpdatePhaseEvidence{UpdaterRequested: true, JournalStage: "ready"}, wantState: SourceAgentCommandRestarting},
		{name: "success raced ahead", state: SourceAgentCommandInstalling, evidence: sourceAgentUpdatePhaseEvidence{UpdaterRequested: true, Outcome: sourceAgentUpdatePhaseSuccess("2.0.0")}, wantState: SourceAgentCommandRestarting},
		{name: "durable success", state: SourceAgentCommandRestarting, evidence: sourceAgentUpdatePhaseEvidence{Outcome: sourceAgentUpdatePhaseSuccess("2.0.0")}, wantState: SourceAgentCommandVerifying},
		{name: "authenticated runtime", state: SourceAgentCommandVerifying, evidence: sourceAgentUpdatePhaseEvidence{Outcome: sourceAgentUpdatePhaseSuccess("2.0.0"), RuntimeIdentityMatches: true, ReadyAfterHeartbeat: true}, wantState: SourceAgentCommandSucceeded, wantCode: SourceAgentCommandCodeUpgradeComplete, wantVersion: "2.0.0"},
		{name: "restored", state: SourceAgentCommandRollback, evidence: sourceAgentUpdatePhaseEvidence{Outcome: sourceAgentUpdatePhaseRestored()}, wantState: SourceAgentCommandRolledBack, wantCode: SourceAgentCommandCodeRollbackComplete},
		{name: "rollback failed", state: SourceAgentCommandRollback, evidence: sourceAgentUpdatePhaseEvidence{Outcome: sourceAgentUpdatePhaseRollbackFailed()}, wantState: SourceAgentCommandFailed, wantCode: SourceAgentCommandCodeRollbackFailed},
		{name: "rollback failed after restore", state: SourceAgentCommandRollback, evidence: sourceAgentUpdatePhaseEvidence{Outcome: &SourceAgentUpdateResult{Outcome: SourceAgentUpdateOutcomeFailed, Code: SourceAgentCommandCodeRollbackFailed, BinaryRestored: true}}, wantState: SourceAgentCommandFailed, wantCode: SourceAgentCommandCodeRollbackFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := mapSourceAgentUpdatePhase(test.state, test.evidence)
			if err != nil {
				t.Fatalf("mapSourceAgentUpdatePhase() error = %v", err)
			}
			if decision.Waiting || decision.Report.State != test.wantState || decision.Report.Code != test.wantCode || decision.Report.ActualVersion != test.wantVersion {
				t.Fatalf("decision = %#v, want state=%q code=%q version=%q", decision, test.wantState, test.wantCode, test.wantVersion)
			}
		})
	}
}

func TestSourceAgentUpdatePhaseMappingRoutesReplacementFailuresThroughRollback(t *testing.T) {
	tests := []struct {
		name      string
		state     string
		evidence  sourceAgentUpdatePhaseEvidence
		wantState string
		wantCode  string
	}{
		{name: "pre replace failure", state: SourceAgentCommandInstalling, evidence: sourceAgentUpdatePhaseEvidence{UpdaterRequested: true, JournalStage: "started", Outcome: sourceAgentUpdatePhaseInstallFailed()}, wantState: SourceAgentCommandFailed, wantCode: SourceAgentCommandCodeInstallFailed},
		{name: "replacement rollback requested", state: SourceAgentCommandInstalling, evidence: sourceAgentUpdatePhaseEvidence{UpdaterRequested: true, JournalStage: "replaced", RollbackRequested: true}, wantState: SourceAgentCommandRollback},
		{name: "restart recovery", state: SourceAgentCommandRestarting, evidence: sourceAgentUpdatePhaseEvidence{JournalStage: "rollback_restored", Outcome: sourceAgentUpdatePhaseRestored()}, wantState: SourceAgentCommandRollback},
		{name: "verify recovery", state: SourceAgentCommandVerifying, evidence: sourceAgentUpdatePhaseEvidence{RollbackRequested: true}, wantState: SourceAgentCommandRollback},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := mapSourceAgentUpdatePhase(test.state, test.evidence)
			if err != nil {
				t.Fatalf("mapSourceAgentUpdatePhase() error = %v", err)
			}
			if decision.Waiting || decision.Report.State != test.wantState || decision.Report.Code != test.wantCode {
				t.Fatalf("decision = %#v, want state=%q code=%q", decision, test.wantState, test.wantCode)
			}
		})
	}
}

func TestSourceAgentUpdatePhaseMappingRejectsMissingOrConflictingEvidence(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		evidence sourceAgentUpdatePhaseEvidence
	}{
		{name: "unknown server state", state: "unknown"},
		{name: "unknown journal stage", state: SourceAgentCommandInstalling, evidence: sourceAgentUpdatePhaseEvidence{UpdaterRequested: true, JournalStage: "unknown"}},
		{name: "success and rollback request conflict", state: SourceAgentCommandVerifying, evidence: sourceAgentUpdatePhaseEvidence{Outcome: sourceAgentUpdatePhaseSuccess("2.0.0"), RollbackRequested: true, RuntimeIdentityMatches: true, ReadyAfterHeartbeat: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mapSourceAgentUpdatePhase(test.state, test.evidence); err == nil {
				t.Fatal("mapSourceAgentUpdatePhase() accepted conflicting evidence")
			}
		})
	}
}

func TestSourceAgentCommandRunnerWaitingUpgradeMakesNoServerTransition(t *testing.T) {
	log := &sourceAgentRunnerCallLog{}
	harness := &sourceAgentRunnerHTTPHarness{log: log}
	commands := &fakeSourceAgentCommandClient{
		log: log,
		claims: []*SourceAgentCommand{{
			ID: "cmd-upgrade", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade,
			State: SourceAgentCommandInstalling,
			UpgradeSpec: &SourceAgentUpgradeSpec{
				ArtifactID: "artifact-1", ExpectedCurrentVersion: "1.0.0",
			},
		}},
	}
	updater := &fakeSourceAgentUpdater{
		log: log, result: SourceAgentUpgradeResult{Waiting: true},
	}
	runner := newSourceAgentCommandTestRunner(
		t,
		harness,
		newSourceAgentCommandTestOutbox(t),
		&fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}},
		commands,
		nil,
		updater,
	)

	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() waiting error = %v", err)
	}
	claims, reports := commands.counts()
	if claims != 1 || len(reports) != 0 || updater.calls.Load() != 1 {
		t.Fatalf("claims=%d reports=%#v updater calls=%d", claims, reports, updater.calls.Load())
	}
}

func TestSourceAgentUpdatePendingMarkerIsPathFreeAndImmutable(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	request := sourceAgentUpdatePhaseRequest()
	if err := store.PublishPending(context.Background(), request); err != nil {
		t.Fatalf("PublishPending() error = %v", err)
	}
	pending, found, err := store.LoadPending()
	if err != nil || !found {
		t.Fatalf("LoadPending() = %#v, %t, %v", pending, found, err)
	}
	if pending.CommandID != request.CommandID || pending.RequestFingerprint != sourceAgentUpdateRequestFingerprint(request) {
		t.Fatalf("pending = %#v", pending)
	}
	if pending.State != SourceAgentPendingUpdateActive {
		t.Fatalf("pending state = %q", pending.State)
	}
	payload, err := os.ReadFile(filepath.Join(root, sourceAgentUpdatePendingFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{request.StagedBinary, "token", "url", "path", "label", "command_line", "environment"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("pending marker leaked forbidden value %q: %s", forbidden, payload)
		}
	}
	conflict := request
	conflict.CommandID = "cmd-other"
	if err := store.PublishPending(context.Background(), conflict); !errors.Is(err, ErrSourceAgentUpgradeCheckpointConflict) {
		t.Fatalf("PublishPending(conflict) error = %v", err)
	}
	if err := store.MarkPendingCleanupComplete(context.Background(), request.CommandID, pending.RequestFingerprint); err != nil {
		t.Fatalf("MarkPendingCleanupComplete() error = %v", err)
	}
	completed, found, err := store.LoadPending()
	if err != nil || !found || completed.State != SourceAgentPendingUpdateCleanupComplete {
		t.Fatalf("completed pending = %#v, %t, %v", completed, found, err)
	}
	if err := store.ClearCompletedPending(context.Background(), request.CommandID, pending.RequestFingerprint); err != nil {
		t.Fatalf("ClearCompletedPending() error = %v", err)
	}
	if _, found, err := store.LoadPending(); err != nil || found {
		t.Fatalf("pending survived completed cleanup: found=%t err=%v", found, err)
	}
}

func TestSourceAgentUpdateBridgePublishesPendingBeforeStartingUpdater(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	request := sourceAgentUpdatePhaseRequest()
	handoff, err := marshalSourceAgentUpdateJSON(sourceAgentUpdateHandoffFromRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	storage := &sourceAgentUpdatePhaseStorage{payload: handoff, staged: request.StagedBinary}
	activator := &sourceAgentUpdatePhaseActivator{}
	bridge := &SourceAgentUpdateBridge{
		config: SourceAgentUpdateBridgeConfig{
			WorkerType: request.WorkerType, CurrentVersion: request.CurrentVersion,
			Platform: request.Platform, Architecture: request.Architecture,
			ProtocolVersion: request.ProtocolVersion, Revision: request.Revision,
			Activator: activator,
		},
		storage:  storage,
		receipts: store,
	}
	command := SourceAgentCommand{
		ID: request.CommandID, Type: SourceAgentCommandUpgrade, State: SourceAgentCommandInstalling,
		UpgradeSpec: &SourceAgentUpgradeSpec{
			ArtifactID: request.ArtifactID, ExpectedCurrentVersion: request.CurrentVersion,
		},
		ExpectedCurrentVersion: request.CurrentVersion,
	}
	result := bridge.Upgrade(context.Background(), command)
	if !result.Waiting || activator.calls != 1 {
		t.Fatalf("result=%#v activator calls=%d", result, activator.calls)
	}
	pending, found, err := store.LoadPending()
	if err != nil || !found || pending.CommandID != command.ID {
		t.Fatalf("pending=%#v found=%t error=%v", pending, found, err)
	}
}

func sourceAgentUpdatePhaseSuccess(version string) *SourceAgentUpdateResult {
	return &SourceAgentUpdateResult{Outcome: SourceAgentUpdateOutcomeSucceeded, Code: SourceAgentCommandCodeUpgradeComplete, RuntimeVersion: version}
}

func sourceAgentUpdatePhaseRestored() *SourceAgentUpdateResult {
	return &SourceAgentUpdateResult{Outcome: SourceAgentUpdateOutcomeRolledBack, Code: SourceAgentCommandCodeRollbackComplete, BinaryRestored: true}
}

func sourceAgentUpdatePhaseRollbackFailed() *SourceAgentUpdateResult {
	return &SourceAgentUpdateResult{Outcome: SourceAgentUpdateOutcomeFailed, Code: SourceAgentCommandCodeRollbackFailed, BinaryRestored: false}
}

func sourceAgentUpdatePhaseInstallFailed() *SourceAgentUpdateResult {
	return &SourceAgentUpdateResult{Outcome: SourceAgentUpdateOutcomeFailed, Code: SourceAgentCommandCodeInstallFailed}
}

func sourceAgentUpdatePhaseRequest() SourceAgentUpdateRequest {
	return SourceAgentUpdateRequest{
		CommandID: "cmd-upgrade", ArtifactID: "artifact-1", WorkerType: "wechat-worker",
		CurrentVersion: "1.0.0", TargetVersion: "2.0.0",
		ExpectedSHA256: strings.Repeat("a", 64), ExpectedSize: 7,
		StagedBinary: "/private/tmp/source-agent-staged", Platform: "darwin", Architecture: "arm64",
		ProtocolVersion: "2026-08-01", Revision: strings.Repeat("b", 40), Channel: "production",
	}
}
