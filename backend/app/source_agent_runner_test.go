package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakeSourceAdapter struct {
	status  SourceCapabilityHealth
	run     SourceSyncRun
	called  bool
	result  SourceAdapterResult
	err     error
	enqueue *SourceArticleEnvelope
}

type sourceAgentRunnerProtectedStateFake struct {
	calls      *[]string
	checkpoint SourceAgentUpgradeCommandCheckpoint
	found      bool
	readyErr   error
	saveErr    error
	ackErr     error
	identity   SourceAgentRuntimeIdentity
}

func (s *sourceAgentRunnerProtectedStateFake) PublishAuthenticatedReady(_ context.Context, identity SourceAgentRuntimeIdentity) (bool, error) {
	*s.calls = append(*s.calls, "ready")
	s.identity = identity
	return s.readyErr == nil, s.readyErr
}

func (s *sourceAgentRunnerProtectedStateFake) LoadUpgradeCommandCheckpoint() (SourceAgentUpgradeCommandCheckpoint, bool, error) {
	*s.calls = append(*s.calls, "load")
	return s.checkpoint, s.found, nil
}

func (s *sourceAgentRunnerProtectedStateFake) SaveUpgradeCommandCheckpoint(_ context.Context, command SourceAgentCommand) error {
	*s.calls = append(*s.calls, "save")
	if s.saveErr != nil {
		return s.saveErr
	}
	s.checkpoint = SourceAgentUpgradeCommandCheckpoint{
		SchemaVersion: sourceAgentUpgradeCheckpointSchema, AgentID: command.TargetAgentID,
		ClaimOwner: command.ClaimOwner, Fingerprint: sourceAgentUpgradeCommandFingerprint(command), Command: command,
	}
	s.found = true
	return nil
}

func (s *sourceAgentRunnerProtectedStateFake) RecordServerTerminalObservation(_ context.Context, command SourceAgentCommand) error {
	*s.calls = append(*s.calls, "observe")
	return s.ackErr
}

type sourceAgentRunnerRecoveryClientFake struct {
	calls     *[]string
	claimed   *SourceAgentCommand
	recovered *SourceAgentCommand
	resumed   *SourceAgentCommand
	reportErr error
}

func (c *sourceAgentRunnerRecoveryClientFake) ClaimCommand(context.Context) (*SourceAgentCommand, error) {
	*c.calls = append(*c.calls, "claim")
	return c.claimed, nil
}

func TestSourceAgentRunnerDoesNotExecuteClaimedUpgradeBeforeCheckpoint(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "heartbeat")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","desired_state":"active"}}`)
	}))
	defer server.Close()
	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
		StateDir: t.TempDir(), HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outbox.Close() })
	command := sourceAgentRunnerRecoveryCommand(SourceAgentCommandClaimed)
	recovery := &sourceAgentRunnerRecoveryClientFake{calls: &calls, claimed: &command}
	protected := &sourceAgentRunnerProtectedStateFake{
		calls: &calls, saveErr: errors.New("checkpoint unavailable"),
	}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{
		Client: client, CommandClient: recovery, UpgradeRecoveryClient: recovery,
		UpgradeState: protected, Outbox: outbox,
		Adapter:    &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}},
		WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
		Version: "1.0.0", ProtocolVersion: "2026-08-01", Revision: sourceAgentUpdateTestRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "checkpoint") {
		t.Fatalf("RunOnce() error=%v", err)
	}
	if got, want := strings.Join(calls, ","), "heartbeat,ready,load,recover,claim,save"; got != want {
		t.Fatalf("calls=%s, want %s", got, want)
	}
	if runner.hasCurrentCommand() {
		t.Fatal("upgrade became current before durable checkpoint")
	}
}

func (c *sourceAgentRunnerRecoveryClientFake) ReportCommand(
	_ context.Context,
	_ string,
	state, code, message, actualVersion string,
) (SourceAgentCommand, error) {
	*c.calls = append(*c.calls, "report")
	if c.reportErr != nil {
		return SourceAgentCommand{}, c.reportErr
	}
	command := *c.recovered
	if c.resumed != nil {
		command = *c.resumed
	}
	command.State, command.ResultCode, command.Message, command.ActualVersion = state, code, message, actualVersion
	command.UpdatedAt = "2026-08-01T12:00:02.000000000Z"
	return command, nil
}

func (c *sourceAgentRunnerRecoveryClientFake) ResumeUpgradeCommand(_ context.Context, _ string) (*SourceAgentCommand, error) {
	*c.calls = append(*c.calls, "resume")
	return c.resumed, nil
}

func (c *sourceAgentRunnerRecoveryClientFake) RecoverOwnedUpgrade(context.Context) (*SourceAgentCommand, error) {
	*c.calls = append(*c.calls, "recover")
	return c.recovered, nil
}

func sourceAgentRunnerRecoveryCommand(state string) SourceAgentCommand {
	command := SourceAgentCommand{
		ID: "command-recovery", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade,
		UpgradeSpec: &SourceAgentUpgradeSpec{ArtifactID: "artifact-2", ExpectedCurrentVersion: "1.0.0"},
		State:       state, IdempotencyKey: "upgrade-recovery", ExpectedCurrentVersion: "1.0.0",
		ClaimOwner: "agent-a", CreatedAt: "2026-08-01T12:00:00.000000000Z",
		UpdatedAt: "2026-08-01T12:00:01.000000000Z", ClaimedAt: "2026-08-01T12:00:01.000000000Z",
		ExpiresAt: formatSourceAgentCommandTime(time.Now().UTC().Add(time.Hour)),
	}
	if isTerminalSourceAgentCommandState(state) {
		command.CompletedAt = "2026-08-01T12:00:03.000000000Z"
	}
	return command
}

func TestSourceAgentRunnerRecoversUpgradeBeforeClaimOrLease(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","desired_state":"active"}}`)
		default:
			calls = append(calls, "unexpected:"+r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
		StateDir: t.TempDir(), HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outbox.Close() })
	command := sourceAgentRunnerRecoveryCommand(SourceAgentCommandClaimed)
	recovery := &sourceAgentRunnerRecoveryClientFake{calls: &calls, recovered: &command}
	protected := &sourceAgentRunnerProtectedStateFake{calls: &calls}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{
		Client: client, CommandClient: recovery, UpgradeRecoveryClient: recovery,
		UpgradeState: protected, Outbox: outbox,
		Adapter:    &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}},
		WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
		Version: "1.0.0", ProtocolVersion: "2026-08-01", Revision: sourceAgentUpdateTestRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(calls, ","), "heartbeat,ready,load,recover,save,report,save"; got != want {
		t.Fatalf("calls=%s, want %s", got, want)
	}
	current, _ := runner.currentCommandSnapshot()
	if current.ID != command.ID || current.State != SourceAgentCommandDownloading {
		t.Fatalf("current command=%#v", current)
	}
	if protected.identity.Revision != sourceAgentUpdateTestRevision {
		t.Fatalf("ready identity=%#v", protected.identity)
	}
}

func TestSourceAgentRunnerRejectsForeignRecoveredUpgrade(t *testing.T) {
	var calls []string
	command := sourceAgentRunnerRecoveryCommand(SourceAgentCommandClaimed)
	command.TargetAgentID = "agent-b"
	command.ClaimOwner = "agent-b"
	recovery := &sourceAgentRunnerRecoveryClientFake{calls: &calls, recovered: &command}
	protected := &sourceAgentRunnerProtectedStateFake{calls: &calls}
	runner := &SourceAgentRunner{
		client: &SourceAgentClient{agentID: "agent-a"}, upgradeRecoveryClient: recovery,
		upgradeState: protected, workerType: "wechat-worker", platform: "darwin",
		architecture: "arm64", version: "1.0.0", protocolVersion: "2026-08-01",
		revision: sourceAgentUpdateTestRevision,
	}
	if _, err := runner.restoreProtectedUpgrade(context.Background()); !errors.Is(err, ErrSourceAgentUpgradeCheckpointInvalid) {
		t.Fatalf("restoreProtectedUpgrade() error=%v", err)
	}
	if got, want := strings.Join(calls, ","), "ready,load,recover"; got != want {
		t.Fatalf("calls=%s, want %s", got, want)
	}
}

func TestSourceAgentRunnerDoesNotPublishReadyBeforeAuthenticatedHeartbeat(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "heartbeat")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
		StateDir: t.TempDir(), HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outbox.Close() })
	recovery := &sourceAgentRunnerRecoveryClientFake{calls: &calls}
	protected := &sourceAgentRunnerProtectedStateFake{calls: &calls}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{
		Client: client, CommandClient: recovery, UpgradeRecoveryClient: recovery,
		UpgradeState: protected, Outbox: outbox, Adapter: &fakeSourceAdapter{},
		WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
		Version: "1.0.0", ProtocolVersion: "2026-08-01", Revision: sourceAgentUpdateTestRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce succeeded after rejected heartbeat")
	}
	if got := strings.Join(calls, ","); got != "heartbeat" {
		t.Fatalf("calls=%s", got)
	}
}

func TestSourceAgentRunnerReconcilesTerminalUpgradeWithoutExecutingIt(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "heartbeat")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","desired_state":"active"}}`)
	}))
	defer server.Close()
	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
		StateDir: t.TempDir(), HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outbox.Close() })
	active := sourceAgentRunnerRecoveryCommand(SourceAgentCommandVerifying)
	terminal := active
	terminal.State = SourceAgentCommandSucceeded
	terminal.ResultCode = SourceAgentCommandCodeUpgradeComplete
	terminal.ActualVersion = "2.0.0"
	terminal.CompletedAt = "2026-08-01T12:00:03.000000000Z"
	recovery := &sourceAgentRunnerRecoveryClientFake{calls: &calls, resumed: &terminal}
	protected := &sourceAgentRunnerProtectedStateFake{calls: &calls, found: true}
	protected.checkpoint = SourceAgentUpgradeCommandCheckpoint{
		SchemaVersion: sourceAgentUpgradeCheckpointSchema, AgentID: active.TargetAgentID,
		ClaimOwner: active.ClaimOwner, Fingerprint: sourceAgentUpgradeCommandFingerprint(active), Command: active,
	}
	updater := &fakeSourceAgentUpdater{log: &sourceAgentRunnerCallLog{}}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{
		Client: client, CommandClient: recovery, UpgradeRecoveryClient: recovery,
		UpgradeState: protected, Outbox: outbox, Adapter: &fakeSourceAdapter{},
		Updater:    updater,
		WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
		Version: "2.0.0", ProtocolVersion: "2026-08-01", Revision: sourceAgentUpdateTestRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	runner.setCurrentCommand(active)
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(calls, ","), "heartbeat,ready,load,resume,observe"; got != want {
		t.Fatalf("calls=%s, want %s", got, want)
	}
	if runner.hasCurrentCommand() || updater.calls.Load() != 0 {
		t.Fatalf("terminal recovery left current=%t updater_calls=%d", runner.hasCurrentCommand(), updater.calls.Load())
	}
}

func (a *fakeSourceAdapter) Name() string                                  { return "fake" }
func (a *fakeSourceAdapter) Operations() []string                          { return []string{"sync_fake"} }
func (a *fakeSourceAdapter) Status(context.Context) SourceCapabilityHealth { return a.status }
func (a *fakeSourceAdapter) Execute(_ context.Context, run SourceSyncRun, sink SourceEnvelopeSink) (SourceAdapterResult, error) {
	a.called = true
	a.run = run
	if a.enqueue != nil {
		if _, err := sink.Enqueue(run.ID, *a.enqueue); err != nil {
			return SourceAdapterResult{}, err
		}
	}
	if a.err != nil || a.result.Cursor != "" {
		return a.result, a.err
	}
	return SourceAdapterResult{Cursor: "cursor-2"}, nil
}

func TestSourceAgentRunnerRejectsMissingDependencies(t *testing.T) {
	_, err := NewSourceAgentRunner(SourceAgentRunnerConfig{Adapter: &fakeSourceAdapter{}, LeaseDuration: time.Minute})
	if err == nil {
		t.Fatal("NewSourceAgentRunner succeeded without client and outbox")
	}
}

func TestSourceAgentRunnerUsesAdapterContract(t *testing.T) {
	var _ SourceAdapter = (*fakeSourceAdapter)(nil)
	var _ SourceEnvelopeSink = (*SourceAgentOutbox)(nil)
}

func TestSourceAgentRunnerDefaultsLocalRuntimeMetadataCompatibly(t *testing.T) {
	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL: "http://127.0.0.1:1", AgentToken: "agent-secret", AgentID: "agent-a", StateDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{
		Client: client, Outbox: outbox, Adapter: &fakeSourceAdapter{}, Version: "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.workerType != "fake" || runner.platform != runtime.GOOS || runner.architecture != runtime.GOARCH || runner.protocolVersion != defaultSourceAgentProtocolVersion {
		t.Fatalf("runner metadata=%q/%q/%q protocol=%q", runner.workerType, runner.platform, runner.architecture, runner.protocolVersion)
	}
}

func TestSourceAgentRunnerPersistsAdapterFailureCheckpoint(t *testing.T) {
	var failedCursor string
	var leaseSeconds int
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a"}}`)
		case "/api/source-agent/commands/claim":
			fmt.Fprint(w, `{"command":null}`)
		case "/api/source-agent/lease":
			var payload struct {
				LeaseSeconds int `json:"lease_seconds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode lease payload: %v", err)
			}
			leaseSeconds = payload.LeaseSeconds
			fmt.Fprint(w, `{"run":{"id":"run-1","status":"running","requested_operation":"sync_fake","subscription":{"id":"sub-1","source_account_key":"account-key","source_account":"Account"}}}`)
		case "/api/source-agent/runs/run-1/items":
			calls = append(calls, "upload")
			fmt.Fprint(w, `{"receipt":{"run_id":"run-1","idempotency_key":"idem-1"}}`)
		case "/api/source-agent/runs/run-1/fail":
			calls = append(calls, "fail")
			var payload struct {
				Cursor string `json:"cursor"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode fail payload: %v", err)
			}
			failedCursor = payload.Cursor
			fmt.Fprint(w, `{"run":{"id":"run-1","status":"failed"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL:  server.URL,
		AgentToken: "agent-secret",
		AgentID:    "agent-a",
		StateDir:   t.TempDir(),
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outbox.Close() })
	cause := errors.New("article download failed")
	adapter := &fakeSourceAdapter{
		status: SourceCapabilityHealth{Healthy: true},
		result: SourceAdapterResult{Cursor: "safe-cursor"},
		err:    &SourceAdapterExecutionError{Cursor: "safe-cursor", Err: cause},
		enqueue: &SourceArticleEnvelope{
			IdempotencyKey:  "idem-1",
			SourceType:      "wechat_mp_article",
			SourceAccountID: "account-key",
			SourceAccount:   "Account",
			SourceItemID:    "article-1",
			Title:           "Article 1",
			SourceURL:       "https://mp.weixin.qq.com/s/article-1",
			Content:         "# Article 1\n\nThis source article contains enough deterministic content for the runner checkpoint test.",
			ContentFormat:   "markdown",
		},
	}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{Client: client, Outbox: outbox, Adapter: adapter})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.RunOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("RunOnce() error=%v", err)
	}
	if result.OK {
		t.Fatalf("RunOnce() result=%#v, want OK=false on execution error", result)
	}
	if failedCursor != "safe-cursor" {
		t.Fatalf("failed cursor=%q", failedCursor)
	}
	if leaseSeconds != 5400 {
		t.Fatalf("lease seconds=%d, want 5400", leaseSeconds)
	}
	if strings.Join(calls, ",") != "upload,fail" {
		t.Fatalf("calls=%v, want upload before fail", calls)
	}
}

func TestSourceAgentRunnerReportsAdapterItemFailuresBeforeCompletion(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a"}}`)
		case "/api/source-agent/commands/claim":
			fmt.Fprint(w, `{"command":null}`)
		case "/api/source-agent/lease":
			fmt.Fprint(w, `{"run":{"id":"run-partial","status":"running","requested_operation":"sync_fake","subscription":{"id":"sub-1","source_account_key":"account-key","source_account":"Account"}}}`)
		case "/api/source-agent/runs/run-partial/items":
			var payload struct {
				SourceItemKey string `json:"source_item_key"`
				Error         string `json:"error"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode item failure: %v", err)
			}
			if payload.SourceItemKey != "article-media" || payload.Error != "media archival failed" {
				t.Errorf("failure payload=%#v", payload)
			}
			calls = append(calls, "failure")
			fmt.Fprint(w, `{"item":{"source_item_key":"article-media","outcome":"failed"}}`)
		case "/api/source-agent/runs/run-partial/complete":
			calls = append(calls, "complete")
			fmt.Fprint(w, `{"run":{"id":"run-partial","status":"partial"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewSourceAgentClient(SourceAgentConfig{RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a", StateDir: t.TempDir(), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	adapter := &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}, result: SourceAdapterResult{
		Cursor: "cursor-partial",
		Failures: []SourceAdapterItemFailure{{
			SourceItemKey:  "article-media",
			IdempotencyKey: "failure-idem",
			Error:          "media archival failed",
		}},
	}}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{Client: client, Outbox: outbox, Adapter: adapter})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SourceRunPartial || strings.Join(calls, ",") != "failure,complete" {
		t.Fatalf("result=%#v calls=%v", result, calls)
	}
}

func TestSourceAgentRunnerDrainsOutboxLargerThanUploadPage(t *testing.T) {
	uploaded := 0
	completed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a"}}`)
		case "/api/source-agent/commands/claim":
			fmt.Fprint(w, `{"command":null}`)
		case "/api/source-agent/lease":
			fmt.Fprint(w, `{"run":{"id":"run-bulk","status":"running","requested_operation":"sync_fake","subscription":{"id":"sub-1","source_account_key":"account-key","source_account":"Account"}}}`)
		case "/api/source-agent/runs/run-bulk/items":
			uploaded++
			fmt.Fprint(w, `{"receipt":{"run_id":"run-bulk"}}`)
		case "/api/source-agent/runs/run-bulk/complete":
			completed = true
			fmt.Fprint(w, `{"run":{"id":"run-bulk","status":"succeeded"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewSourceAgentClient(SourceAgentConfig{RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a", StateDir: t.TempDir(), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	for index := 0; index < 205; index++ {
		itemKey := fmt.Sprintf("article-%03d", index)
		if _, err := outbox.Enqueue("run-bulk", SourceArticleEnvelope{
			IdempotencyKey:  "idem-" + itemKey,
			SourceType:      "wechat_mp_article",
			SourceAccountID: "account-key",
			SourceAccount:   "Account",
			SourceItemID:    itemKey,
			Title:           "Article " + itemKey,
			SourceURL:       "https://mp.weixin.qq.com/s/" + itemKey,
			Content:         "# Article\n\nThis article contains enough deterministic content for the bulk outbox drain test.",
			ContentFormat:   "markdown",
		}); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{Client: client, Outbox: outbox, Adapter: adapter})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if uploaded != 205 || !completed || result.OutboxRemaining != 0 {
		t.Fatalf("uploaded=%d completed=%t result=%#v", uploaded, completed, result)
	}
}
