package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sourceAgentRunnerCallLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *sourceAgentRunnerCallLog) add(call string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call)
}

func (l *sourceAgentRunnerCallLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

type sourceAgentCommandReportCall struct {
	CommandID     string
	State         string
	Code          string
	Message       string
	ActualVersion string
}

type fakeSourceAgentCommandClient struct {
	mu          sync.Mutex
	log         *sourceAgentRunnerCallLog
	claims      []*SourceAgentCommand
	claimErr    error
	reportErrs  []error
	claimCalls  int
	reportCalls []sourceAgentCommandReportCall
	current     SourceAgentCommand
}

type blockingSourceAgentCommandClient struct {
	log     *sourceAgentRunnerCallLog
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingSourceAgentCommandClient) ClaimCommand(context.Context) (*SourceAgentCommand, error) {
	c.log.add("claim")
	c.once.Do(func() { close(c.started) })
	<-c.release
	return nil, nil
}

func (c *blockingSourceAgentCommandClient) ReportCommand(context.Context, string, string, string, string, string) (SourceAgentCommand, error) {
	return SourceAgentCommand{}, errors.New("unexpected command report")
}

func (c *fakeSourceAgentCommandClient) ClaimCommand(context.Context) (*SourceAgentCommand, error) {
	c.log.add("claim")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.claimCalls++
	if c.claimErr != nil {
		return nil, c.claimErr
	}
	if len(c.claims) == 0 {
		return nil, nil
	}
	command := c.claims[0]
	c.claims = c.claims[1:]
	if command == nil {
		return nil, nil
	}
	copy := *command
	c.current = copy
	return &copy, nil
}

func (c *fakeSourceAgentCommandClient) ReportCommand(_ context.Context, commandID, state, code, message, actualVersion string) (SourceAgentCommand, error) {
	c.log.add("report:" + state)
	c.mu.Lock()
	defer c.mu.Unlock()
	call := sourceAgentCommandReportCall{CommandID: commandID, State: state, Code: code, Message: message, ActualVersion: actualVersion}
	c.reportCalls = append(c.reportCalls, call)
	if len(c.reportErrs) > 0 {
		err := c.reportErrs[0]
		c.reportErrs = c.reportErrs[1:]
		if err != nil {
			return SourceAgentCommand{}, err
		}
	}
	if c.current.ID == "" {
		return SourceAgentCommand{}, errors.New("no claimed command")
	}
	c.current.State = state
	c.current.ResultCode = code
	c.current.Message = message
	c.current.ActualVersion = actualVersion
	return c.current, nil
}

func (c *fakeSourceAgentCommandClient) counts() (int, []sourceAgentCommandReportCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.claimCalls, append([]sourceAgentCommandReportCall(nil), c.reportCalls...)
}

type fakeSourceAgentDiagnoser struct {
	log    *sourceAgentRunnerCallLog
	report SourceAgentDiagnosticReport
	calls  atomic.Int32
}

func (d *fakeSourceAgentDiagnoser) Diagnose(context.Context) SourceAgentDiagnosticReport {
	d.log.add("diagnose")
	d.calls.Add(1)
	return d.report
}

type fakeSourceAgentUpdater struct {
	log     *sourceAgentRunnerCallLog
	result  SourceAgentUpgradeResult
	calls   atomic.Int32
	overlap atomic.Bool
	runBusy *atomic.Bool
}

func (u *fakeSourceAgentUpdater) Upgrade(context.Context, SourceAgentCommand) SourceAgentUpgradeResult {
	u.log.add("upgrade")
	u.calls.Add(1)
	if u.runBusy != nil && u.runBusy.Load() {
		u.overlap.Store(true)
	}
	return u.result
}

type sourceAgentRunnerHTTPHarness struct {
	mu              sync.Mutex
	log             *sourceAgentRunnerCallLog
	desiredState    string
	heartbeatStatus int
	heartbeats      []SourceAgentHeartbeat
	leaseRun        *SourceSyncRun
	leaseCalls      int
}

func (h *sourceAgentRunnerHTTPHarness) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/source-agent/heartbeat":
		h.log.add("heartbeat")
		var heartbeat SourceAgentHeartbeat
		if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
			http.Error(w, "bad heartbeat", http.StatusBadRequest)
			return
		}
		h.mu.Lock()
		h.heartbeats = append(h.heartbeats, heartbeat)
		status := h.heartbeatStatus
		desired := h.desiredState
		h.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":"heartbeat rejected"}`)
			return
		}
		if desired == "" {
			desired = SourceAgentDesiredActive
		}
		fmt.Fprintf(w, `{"agent":{"agent_id":"agent-a","desired_state":%q}}`, desired)
	case "/api/source-agent/lease":
		h.log.add("lease")
		h.mu.Lock()
		h.leaseCalls++
		run := h.leaseRun
		h.leaseRun = nil
		h.mu.Unlock()
		if run == nil {
			fmt.Fprint(w, `{"run":null}`)
			return
		}
		fmt.Fprintf(w, `{"run":{"id":%q,"status":"running","requested_operation":"sync_fake","subscription":{"id":"sub-1","source_account_key":"account-key","source_account":"Account"}}}`, run.ID)
	case "/api/source-agent/runs/run-active/complete":
		h.log.add("complete")
		fmt.Fprint(w, `{"run":{"id":"run-active","status":"succeeded"}}`)
	default:
		http.NotFound(w, r)
	}
}

func (h *sourceAgentRunnerHTTPHarness) snapshot() ([]SourceAgentHeartbeat, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]SourceAgentHeartbeat(nil), h.heartbeats...), h.leaseCalls
}

func newSourceAgentCommandTestRunner(t *testing.T, harness *sourceAgentRunnerHTTPHarness, outbox *SourceAgentOutbox, adapter SourceAdapter, commands SourceAgentCommandClient, diagnoser SourceAgentDiagnoser, updater SourceAgentUpdater) *SourceAgentRunner {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(harness.handler))
	t.Cleanup(server.Close)
	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
		StateDir: t.TempDir(), HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{
		Client: client, Outbox: outbox, Adapter: adapter, CommandClient: commands,
		Diagnoser: diagnoser, Updater: updater, Version: "1.0.0",
		WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64", ProtocolVersion: "2026-08-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func newSourceAgentCommandTestOutbox(t *testing.T) *SourceAgentOutbox {
	t.Helper()
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outbox.Close() })
	return outbox
}

func TestSourceAgentCommandRunnerClaimsBeforeLeasingAndStopsOnClaimError(t *testing.T) {
	t.Run("no command leases at most one run after claim", func(t *testing.T) {
		log := &sourceAgentRunnerCallLog{}
		harness := &sourceAgentRunnerHTTPHarness{log: log}
		commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{nil}}
		runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, nil, nil)

		if _, err := runner.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(log.snapshot(), ","); got != "heartbeat,claim,lease" {
			t.Fatalf("call order=%q", got)
		}
	})

	t.Run("claim failure prevents lease", func(t *testing.T) {
		log := &sourceAgentRunnerCallLog{}
		harness := &sourceAgentRunnerHTTPHarness{log: log}
		commands := &fakeSourceAgentCommandClient{log: log, claimErr: errors.New("claim unavailable")}
		runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, nil, nil)

		if _, err := runner.RunOnce(context.Background()); err == nil {
			t.Fatal("RunOnce() succeeded after claim failure")
		}
		if got := strings.Join(log.snapshot(), ","); got != "heartbeat,claim" {
			t.Fatalf("call order=%q", got)
		}
	})
}

func TestSourceAgentCommandRunnerDiagnoseIsTerminalAndNeverLeasesSameCycle(t *testing.T) {
	for _, test := range []struct {
		name    string
		report  SourceAgentDiagnosticReport
		state   string
		code    string
		message string
	}{
		{name: "success", report: SourceAgentDiagnosticReport{State: SourceAgentCommandSucceeded, Code: SourceAgentCommandCodeDiagnosticComplete, Message: "cookie=secret article body excerpt"}, state: SourceAgentCommandSucceeded, code: SourceAgentCommandCodeDiagnosticComplete, message: "Diagnostics completed."},
		{name: "failure", report: SourceAgentDiagnosticReport{State: SourceAgentCommandFailed, Code: SourceAgentCommandCodeDiagnosticFailed, Message: "raw local diagnostic error"}, state: SourceAgentCommandFailed, code: SourceAgentCommandCodeDiagnosticFailed, message: "Diagnostics failed."},
	} {
		t.Run(test.name, func(t *testing.T) {
			log := &sourceAgentRunnerCallLog{}
			harness := &sourceAgentRunnerHTTPHarness{log: log}
			commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{{ID: "cmd-diagnose", TargetAgentID: "agent-a", Type: SourceAgentCommandDiagnose, State: SourceAgentCommandClaimed}}}
			diagnoser := &fakeSourceAgentDiagnoser{log: log, report: test.report}
			runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, diagnoser, nil)

			if _, err := runner.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, reports := commands.counts()
			if len(reports) != 1 || reports[0].State != test.state || reports[0].Code != test.code || reports[0].Message != test.message {
				t.Fatalf("reports=%#v", reports)
			}
			if got := strings.Join(log.snapshot(), ","); got != "heartbeat,claim,diagnose,report:"+test.state {
				t.Fatalf("call order=%q", got)
			}
		})
	}
}

func TestSourceAgentCommandRunnerRetriesReportWithoutDiagnosingOrLeasing(t *testing.T) {
	log := &sourceAgentRunnerCallLog{}
	harness := &sourceAgentRunnerHTTPHarness{log: log}
	commands := &fakeSourceAgentCommandClient{
		log:        log,
		claims:     []*SourceAgentCommand{{ID: "cmd-diagnose", TargetAgentID: "agent-a", Type: SourceAgentCommandDiagnose, State: SourceAgentCommandClaimed}},
		reportErrs: []error{errors.New("report unavailable"), nil},
	}
	diagnoser := &fakeSourceAgentDiagnoser{log: log, report: SourceAgentDiagnosticReport{State: SourceAgentCommandSucceeded, Code: SourceAgentCommandCodeDiagnosticComplete, Message: "Checks passed."}}
	runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, diagnoser, nil)

	if _, err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("first RunOnce() succeeded after report failure")
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("retry RunOnce(): %v", err)
	}
	claims, reports := commands.counts()
	if claims != 1 || len(reports) != 2 || diagnoser.calls.Load() != 1 {
		t.Fatalf("claims=%d reports=%d diagnose=%d", claims, len(reports), diagnoser.calls.Load())
	}
	if got := strings.Join(log.snapshot(), ","); got != "heartbeat,claim,diagnose,report:succeeded,heartbeat,report:succeeded" {
		t.Fatalf("call order=%q", got)
	}
}

func TestSourceAgentCommandRunnerMapsUpgradeResultsToLegalTransitions(t *testing.T) {
	for _, test := range []struct {
		name        string
		result      SourceAgentUpgradeResult
		wantStates  []string
		wantMessage string
	}{
		{
			name:        "success",
			result:      SourceAgentUpgradeResult{State: SourceAgentCommandSucceeded, Code: SourceAgentCommandCodeUpgradeComplete, Message: "cookie=secret article body excerpt", ActualVersion: "2.0.0"},
			wantStates:  []string{SourceAgentCommandDownloading, SourceAgentCommandVerified, SourceAgentCommandInstalling, SourceAgentCommandRestarting, SourceAgentCommandVerifying, SourceAgentCommandSucceeded},
			wantMessage: "Upgrade completed.",
		},
		{
			name:        "failure",
			result:      SourceAgentUpgradeResult{State: SourceAgentCommandFailed, Code: SourceAgentCommandCodeUpgradeFailed, Message: "raw local updater error"},
			wantStates:  []string{SourceAgentCommandDownloading, SourceAgentCommandFailed},
			wantMessage: "Upgrade failed.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			log := &sourceAgentRunnerCallLog{}
			harness := &sourceAgentRunnerHTTPHarness{log: log}
			commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{{ID: "cmd-upgrade", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade, State: SourceAgentCommandClaimed, UpgradeSpec: &SourceAgentUpgradeSpec{ArtifactID: "artifact-1", ExpectedCurrentVersion: "1.0.0"}}}}
			updater := &fakeSourceAgentUpdater{log: log, result: test.result}
			runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, nil, updater)

			if _, err := runner.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, firstCycleReports := commands.counts()
			if len(firstCycleReports) != 1 || firstCycleReports[0].State != SourceAgentCommandDownloading || updater.calls.Load() != 0 {
				t.Fatalf("first cycle reports=%#v updater calls=%d", firstCycleReports, updater.calls.Load())
			}
			if got := strings.Join(log.snapshot(), ","); got != "heartbeat,claim,report:downloading" {
				t.Fatalf("first cycle call order=%q", got)
			}
			if _, err := runner.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, reports := commands.counts()
			states := make([]string, len(reports))
			for index := range reports {
				states[index] = reports[index].State
			}
			if strings.Join(states, ",") != strings.Join(test.wantStates, ",") {
				t.Fatalf("states=%v, want %v", states, test.wantStates)
			}
			if len(reports) > 0 {
				last := reports[len(reports)-1]
				if last.Code != test.result.Code || last.Message != test.wantMessage || last.ActualVersion != test.result.ActualVersion {
					t.Fatalf("terminal report=%#v", last)
				}
			}
			_, leases := harness.snapshot()
			if leases != 0 || updater.calls.Load() != 1 {
				t.Fatalf("leases=%d updater calls=%d", leases, updater.calls.Load())
			}
			calls := log.snapshot()
			downloadIndex, upgradeIndex := -1, -1
			for index, call := range calls {
				if call == "report:"+SourceAgentCommandDownloading {
					downloadIndex = index
				}
				if call == "upgrade" {
					upgradeIndex = index
				}
			}
			secondHeartbeatIndex := -1
			for index := downloadIndex + 1; index < len(calls); index++ {
				if calls[index] == "heartbeat" {
					secondHeartbeatIndex = index
					break
				}
			}
			if downloadIndex < 0 || secondHeartbeatIndex <= downloadIndex || upgradeIndex <= secondHeartbeatIndex {
				t.Fatalf("updater handoff must wait for the next heartbeat cycle: %v", calls)
			}
		})
	}
}

func TestSourceAgentCommandRunnerDoesNotUpgradeUntilDownloadingReportSucceeds(t *testing.T) {
	log := &sourceAgentRunnerCallLog{}
	harness := &sourceAgentRunnerHTTPHarness{log: log}
	commands := &fakeSourceAgentCommandClient{
		log:        log,
		claims:     []*SourceAgentCommand{{ID: "cmd-upgrade", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade, State: SourceAgentCommandClaimed, UpgradeSpec: &SourceAgentUpgradeSpec{ArtifactID: "artifact-1", ExpectedCurrentVersion: "1.0.0"}}},
		reportErrs: []error{errors.New("progress unavailable"), nil, nil},
	}
	updater := &fakeSourceAgentUpdater{log: log, result: SourceAgentUpgradeResult{State: SourceAgentCommandFailed, Code: SourceAgentCommandCodeUpgradeFailed, Message: "Upgrade failed safely."}}
	runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, nil, updater)

	if _, err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() succeeded after downloading report failure")
	}
	if updater.calls.Load() != 0 {
		t.Fatalf("updater called before progress was durable: %d", updater.calls.Load())
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("retry downloading report: %v", err)
	}
	if updater.calls.Load() != 0 {
		t.Fatalf("updater called in report-retry cycle: %d", updater.calls.Load())
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("upgrade after durable downloading report: %v", err)
	}
	claims, reports := commands.counts()
	if claims != 1 || updater.calls.Load() != 1 {
		t.Fatalf("claims=%d updater calls=%d", claims, updater.calls.Load())
	}
	states := make([]string, len(reports))
	for index := range reports {
		states[index] = reports[index].State
	}
	if got := strings.Join(states, ","); got != "downloading,downloading,failed" {
		t.Fatalf("report states=%q", got)
	}
}

func TestSourceAgentCommandRunnerRejectsInvalidUpgradeBeforeProgress(t *testing.T) {
	log := &sourceAgentRunnerCallLog{}
	harness := &sourceAgentRunnerHTTPHarness{log: log}
	commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{{
		ID: "cmd-upgrade", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade, State: SourceAgentCommandClaimed,
	}}}
	updater := &fakeSourceAgentUpdater{log: log, result: SourceAgentUpgradeResult{State: SourceAgentCommandSucceeded, Code: SourceAgentCommandCodeUpgradeComplete, ActualVersion: "2.0.0"}}
	runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, nil, updater)

	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, reports := commands.counts()
	if len(reports) != 1 || reports[0].State != SourceAgentCommandFailed || reports[0].Code != SourceAgentCommandCodeUpgradeFailed {
		t.Fatalf("reports=%#v", reports)
	}
	if updater.calls.Load() != 0 {
		t.Fatalf("updater calls=%d", updater.calls.Load())
	}
	if got := strings.Join(log.snapshot(), ","); got != "heartbeat,claim,report:failed" {
		t.Fatalf("call order=%q", got)
	}
}

func TestSourceAgentCommandRunnerBoundsInvalidDiagnosticAndUpgradeResults(t *testing.T) {
	privateMessage := "See https://invalid.example/private and /private/source-state"
	diagnostic := sourceAgentDiagnosticCommandReports(SourceAgentDiagnosticReport{
		State: SourceAgentCommandSucceeded, Code: SourceAgentCommandCodeDiagnosticComplete, Message: privateMessage,
	})
	if len(diagnostic) != 1 || diagnostic[0].state != SourceAgentCommandSucceeded || diagnostic[0].code != SourceAgentCommandCodeDiagnosticComplete || diagnostic[0].message != "Diagnostics completed." {
		t.Fatalf("diagnostic reports=%#v", diagnostic)
	}
	upgrade := sourceAgentUpgradeCommandReports(SourceAgentUpgradeResult{
		State: SourceAgentCommandSucceeded, Code: SourceAgentCommandCodeUpgradeComplete,
		Message: privateMessage, ActualVersion: strings.Repeat("v", sourceAgentVersionMaxRunes+1),
	})
	if len(upgrade) != 1 || upgrade[0].state != SourceAgentCommandFailed || upgrade[0].code != SourceAgentCommandCodeUpgradeFailed {
		t.Fatalf("upgrade reports=%#v", upgrade)
	}
	for _, report := range append(diagnostic, upgrade...) {
		if strings.Contains(report.message, "invalid.example") || strings.Contains(report.message, "/private/source-state") || len([]rune(report.message)) > sourceAgentCommandMessageMaxRunes {
			t.Fatalf("unbounded or private command report=%#v", report)
		}
	}
}

func TestSourceAgentCommandRunnerUsesStableUpgradeMessages(t *testing.T) {
	for _, test := range []struct {
		state   string
		code    string
		version string
		message string
	}{
		{state: SourceAgentCommandSucceeded, code: SourceAgentCommandCodeUpgradeComplete, version: "2.0.0", message: "Upgrade completed."},
		{state: SourceAgentCommandFailed, code: SourceAgentCommandCodeUpgradeFailed, message: "Upgrade failed."},
		{state: SourceAgentCommandFailed, code: SourceAgentCommandCodeDownloadFailed, message: "Upgrade download failed."},
		{state: SourceAgentCommandFailed, code: SourceAgentCommandCodeVerificationFailed, message: "Upgrade verification failed."},
		{state: SourceAgentCommandFailed, code: SourceAgentCommandCodeInstallFailed, message: "Upgrade installation failed."},
		{state: SourceAgentCommandFailed, code: SourceAgentCommandCodeRestartFailed, message: "Upgrade restart failed."},
		{state: SourceAgentCommandRolledBack, code: SourceAgentCommandCodeRollbackComplete, message: "Upgrade rolled back."},
		{state: SourceAgentCommandFailed, code: SourceAgentCommandCodeRollbackFailed, message: "Upgrade rollback failed."},
	} {
		reports := sourceAgentUpgradeCommandReports(SourceAgentUpgradeResult{
			State: test.state, Code: test.code, Message: "cookie=secret article body excerpt raw error", ActualVersion: test.version,
		})
		if len(reports) == 0 {
			t.Fatalf("state=%q code=%q produced no reports", test.state, test.code)
		}
		terminal := reports[len(reports)-1]
		if terminal.state != test.state || terminal.code != test.code || terminal.message != test.message {
			t.Fatalf("state=%q code=%q terminal=%#v", test.state, test.code, terminal)
		}
	}
}

func TestSourceAgentCommandRunnerRejectsNonTerminalComponentResultsAndClearsCommand(t *testing.T) {
	for _, test := range []struct {
		name      string
		command   SourceAgentCommand
		diagnoser SourceAgentDiagnoser
		updater   SourceAgentUpdater
		wantCode  string
	}{
		{
			name:      "diagnose verified",
			command:   SourceAgentCommand{ID: "cmd-diagnose", TargetAgentID: "agent-a", Type: SourceAgentCommandDiagnose, State: SourceAgentCommandClaimed},
			diagnoser: &fakeSourceAgentDiagnoser{log: &sourceAgentRunnerCallLog{}, report: SourceAgentDiagnosticReport{State: SourceAgentCommandVerified}},
			wantCode:  SourceAgentCommandCodeDiagnosticFailed,
		},
		{
			name:     "upgrade installing",
			command:  SourceAgentCommand{ID: "cmd-upgrade", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade, State: SourceAgentCommandClaimed, UpgradeSpec: &SourceAgentUpgradeSpec{ArtifactID: "artifact-1", ExpectedCurrentVersion: "1.0.0"}},
			updater:  &fakeSourceAgentUpdater{log: &sourceAgentRunnerCallLog{}, result: SourceAgentUpgradeResult{State: SourceAgentCommandInstalling}},
			wantCode: SourceAgentCommandCodeUpgradeFailed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			log := &sourceAgentRunnerCallLog{}
			harness := &sourceAgentRunnerHTTPHarness{log: log}
			commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{&test.command, nil}}
			if diagnoser, ok := test.diagnoser.(*fakeSourceAgentDiagnoser); ok {
				diagnoser.log = log
			}
			if updater, ok := test.updater.(*fakeSourceAgentUpdater); ok {
				updater.log = log
			}
			runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, test.diagnoser, test.updater)

			if _, err := runner.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if runner.hasCurrentCommand() {
				if _, err := runner.RunOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if runner.hasCurrentCommand() {
				t.Fatal("invalid terminal result left command active")
			}
			if _, err := runner.RunOnce(context.Background()); err != nil {
				t.Fatalf("cycle after invalid terminal result: %v", err)
			}
			claims, reports := commands.counts()
			if claims != 2 {
				t.Fatalf("claim calls=%d, command did not clear", claims)
			}
			if len(reports) == 0 {
				t.Fatal("no command reports")
			}
			terminal := reports[len(reports)-1]
			if terminal.State != SourceAgentCommandFailed || terminal.Code != test.wantCode {
				t.Fatalf("terminal report=%#v", terminal)
			}
		})
	}

	for _, state := range []string{"", SourceAgentCommandVerified, SourceAgentCommandInstalling, "unknown"} {
		diagnostic := sourceAgentDiagnosticCommandReports(SourceAgentDiagnosticReport{State: state})
		if len(diagnostic) != 1 || diagnostic[0].state != SourceAgentCommandFailed || diagnostic[0].code != SourceAgentCommandCodeDiagnosticFailed {
			t.Fatalf("diagnostic state %q reports=%#v", state, diagnostic)
		}
		upgrade := sourceAgentUpgradeCommandReports(SourceAgentUpgradeResult{State: state})
		if len(upgrade) != 1 || upgrade[0].state != SourceAgentCommandFailed || upgrade[0].code != SourceAgentCommandCodeUpgradeFailed {
			t.Fatalf("upgrade state %q reports=%#v", state, upgrade)
		}
	}
	mismatchedDiagnostic := sourceAgentDiagnosticCommandReports(SourceAgentDiagnosticReport{
		State: SourceAgentCommandSucceeded, Code: SourceAgentCommandCodeDiagnosticFailed,
	})
	if len(mismatchedDiagnostic) != 1 || mismatchedDiagnostic[0].state != SourceAgentCommandFailed || mismatchedDiagnostic[0].code != SourceAgentCommandCodeDiagnosticFailed || mismatchedDiagnostic[0].message != "Diagnostic result was invalid." {
		t.Fatalf("mismatched diagnostic reports=%#v", mismatchedDiagnostic)
	}
	mismatchedUpgrade := sourceAgentUpgradeCommandReports(SourceAgentUpgradeResult{
		State: SourceAgentCommandSucceeded, Code: SourceAgentCommandCodeRollbackComplete, ActualVersion: "2.0.0",
	})
	if len(mismatchedUpgrade) != 1 || mismatchedUpgrade[0].state != SourceAgentCommandFailed || mismatchedUpgrade[0].code != SourceAgentCommandCodeUpgradeFailed || mismatchedUpgrade[0].message != "Upgrade result was invalid." {
		t.Fatalf("mismatched upgrade reports=%#v", mismatchedUpgrade)
	}
}

func TestSourceAgentCommandRunnerPropagatesCanceledContextBeforeNetwork(t *testing.T) {
	log := &sourceAgentRunnerCallLog{}
	harness := &sourceAgentRunnerHTTPHarness{log: log}
	commands := &fakeSourceAgentCommandClient{log: log}
	runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runner.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce() error=%v, want context.Canceled", err)
	}
	if calls := log.snapshot(); len(calls) != 0 {
		t.Fatalf("network calls after cancellation=%v", calls)
	}
}

func TestSourceAgentCommandRunnerCancelsWhileWaitingForControlGate(t *testing.T) {
	log := &sourceAgentRunnerCallLog{}
	harness := &sourceAgentRunnerHTTPHarness{log: log}
	commands := &blockingSourceAgentCommandClient{
		log: log, started: make(chan struct{}), release: make(chan struct{}),
	}
	runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, nil, nil)

	firstDone := make(chan error, 1)
	go func() {
		_, err := runner.RunOnce(context.Background())
		firstDone <- err
	}()
	select {
	case <-commands.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first cycle did not acquire the control gate")
	}
	heartbeatsBefore, _ := harness.snapshot()

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, err := runner.RunOnce(secondCtx)
		secondDone <- err
	}()
	cancelSecond()

	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			close(commands.release)
			<-firstDone
			t.Fatalf("second RunOnce() error=%v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		close(commands.release)
		<-firstDone
		<-secondDone
		t.Fatal("canceled RunOnce() remained blocked on the control gate")
	}
	heartbeatsAfter, _ := harness.snapshot()
	if len(heartbeatsAfter) != len(heartbeatsBefore) {
		close(commands.release)
		<-firstDone
		t.Fatalf("waiting canceled cycle sent network traffic: before=%d after=%d", len(heartbeatsBefore), len(heartbeatsAfter))
	}
	close(commands.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RunOnce(): %v", err)
	}
}

type blockingSourceAgentAdapter struct {
	started chan struct{}
	release chan struct{}
	busy    atomic.Bool
}

func (a *blockingSourceAgentAdapter) Name() string         { return "fake" }
func (a *blockingSourceAgentAdapter) Operations() []string { return []string{"sync_fake"} }
func (a *blockingSourceAgentAdapter) Status(context.Context) SourceCapabilityHealth {
	return SourceCapabilityHealth{Healthy: true}
}
func (a *blockingSourceAgentAdapter) Execute(ctx context.Context, _ SourceSyncRun, _ SourceEnvelopeSink) (SourceAdapterResult, error) {
	a.busy.Store(true)
	defer a.busy.Store(false)
	close(a.started)
	select {
	case <-ctx.Done():
		return SourceAdapterResult{}, ctx.Err()
	case <-a.release:
		return SourceAdapterResult{Cursor: "cursor-active"}, nil
	}
}

func TestSourceAgentCommandRunnerKeepsClaimedUpgradeWaitingForActiveRun(t *testing.T) {
	log := &sourceAgentRunnerCallLog{}
	harness := &sourceAgentRunnerHTTPHarness{log: log, leaseRun: &SourceSyncRun{ID: "run-active"}}
	commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{
		nil,
		{ID: "cmd-upgrade", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade, State: SourceAgentCommandClaimed, UpgradeSpec: &SourceAgentUpgradeSpec{ArtifactID: "artifact-1", ExpectedCurrentVersion: "1.0.0"}},
	}}
	adapter := &blockingSourceAgentAdapter{started: make(chan struct{}), release: make(chan struct{})}
	updater := &fakeSourceAgentUpdater{log: log, result: SourceAgentUpgradeResult{State: SourceAgentCommandSucceeded, Code: SourceAgentCommandCodeUpgradeComplete, Message: "Upgrade completed.", ActualVersion: "2.0.0"}, runBusy: &adapter.busy}
	runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), adapter, commands, nil, updater)

	runDone := make(chan error, 1)
	go func() {
		_, err := runner.RunOnce(context.Background())
		runDone <- err
	}()
	select {
	case <-adapter.started:
	case <-time.After(5 * time.Second):
		t.Fatal("source run did not start")
	}

	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("claim waiting upgrade: %v", err)
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("report waiting heartbeat: %v", err)
	}
	_, reports := commands.counts()
	if len(reports) != 0 || updater.calls.Load() != 0 {
		t.Fatalf("waiting upgrade reports=%#v updater calls=%d", reports, updater.calls.Load())
	}
	heartbeats, leases := harness.snapshot()
	foundWaiting := false
	for _, heartbeat := range heartbeats {
		if heartbeat.CurrentRunID == "run-active" && heartbeat.CurrentCommandID == "cmd-upgrade" {
			foundWaiting = true
		}
	}
	if !foundWaiting {
		t.Fatalf("heartbeats did not expose bounded waiting state: %#v", heartbeats)
	}
	if leases != 1 {
		t.Fatalf("lease calls=%d, want 1", leases)
	}

	close(adapter.release)
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("source run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("source run did not finish")
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("report waiting upgrade download: %v", err)
	}
	if updater.calls.Load() != 0 {
		t.Fatalf("updater ran before the next heartbeat cycle: %d", updater.calls.Load())
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("apply waiting upgrade: %v", err)
	}
	if updater.calls.Load() != 1 || updater.overlap.Load() {
		t.Fatalf("updater calls=%d overlap=%t", updater.calls.Load(), updater.overlap.Load())
	}
	_, reports = commands.counts()
	if len(reports) != 6 || reports[0].State != SourceAgentCommandDownloading || reports[len(reports)-1].State != SourceAgentCommandSucceeded {
		t.Fatalf("upgrade reports=%#v", reports)
	}
}

func TestSourceAgentCommandRunnerRespectsPausedAndUnhealthyHeartbeat(t *testing.T) {
	for _, test := range []struct {
		name    string
		desired string
		health  SourceCapabilityHealth
	}{
		{name: "paused", desired: SourceAgentDesiredPaused, health: SourceCapabilityHealth{Healthy: true}},
		{name: "unhealthy", desired: SourceAgentDesiredActive, health: SourceCapabilityHealth{Healthy: false, Code: "login_required"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			log := &sourceAgentRunnerCallLog{}
			harness := &sourceAgentRunnerHTTPHarness{log: log, desiredState: test.desired}
			commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{nil}}
			runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: test.health}, commands, nil, nil)
			if _, err := runner.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, leases := harness.snapshot(); leases != 0 {
				t.Fatalf("lease calls=%d", leases)
			}
		})
	}
}

func TestSourceAgentRunnerHeartbeatIncludesBoundedLocalMetadataAndCounts(t *testing.T) {
	log := &sourceAgentRunnerCallLog{}
	harness := &sourceAgentRunnerHTTPHarness{log: log}
	outbox := newSourceAgentCommandTestOutbox(t)
	if _, err := outbox.Enqueue("run-pending", sourceAgentOutboxEnvelope("heartbeat-pending", "pending", "Pending")); err != nil {
		t.Fatal(err)
	}
	dead, err := outbox.Enqueue("run-dead", sourceAgentOutboxEnvelope("heartbeat-dead", "dead", "Dead"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.RecordFailure(dead.ID, http.StatusBadRequest, errors.New("delivery failed")); err != nil {
		t.Fatal(err)
	}
	commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{nil}}
	adapter := &fakeSourceAdapter{status: SourceCapabilityHealth{
		Healthy: false, Code: "dependency_unavailable", Version: "adapter-2",
		LastError:      "cookie=secret raw local capability error",
		RequiresAction: "article body excerpt requires local operator action",
	}}
	runner := newSourceAgentCommandTestRunner(t, harness, outbox, adapter, commands, nil, nil)

	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	heartbeats, _ := harness.snapshot()
	if len(heartbeats) != 1 {
		t.Fatalf("heartbeats=%#v", heartbeats)
	}
	heartbeat := heartbeats[0]
	if heartbeat.WorkerType != "wechat-worker" || heartbeat.Platform != "darwin" || heartbeat.Architecture != "arm64" || heartbeat.Version != "1.0.0" || heartbeat.ProtocolVersion != "2026-08-01" {
		t.Fatalf("runtime metadata=%#v", heartbeat)
	}
	capabilityHealth := heartbeat.CapabilityHealth["fake"]
	if heartbeat.OutboxPending != 1 || heartbeat.DeadLetterCount != 1 || len(heartbeat.Capabilities) != 1 ||
		capabilityHealth.Healthy || capabilityHealth.Code != "dependency_unavailable" || capabilityHealth.Version != "adapter-2" ||
		capabilityHealth.LastError != "Capability check failed." || capabilityHealth.RequiresAction != "Operator action is required." {
		t.Fatalf("health metadata=%#v", heartbeat)
	}
	encoded, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{
		"heartbeat-pending", "heartbeat-dead", sourceAgentOutboxDBName, outbox.DBPath(), "agent-secret",
		"cookie=secret", "raw local capability error", "article body excerpt requires local operator action",
	} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("heartbeat leaked %q: %s", private, encoded)
		}
	}
}

func TestSourceAgentRunnerStopsBeforeNetworkOnCountErrorAndBeforeClaimOnHeartbeatError(t *testing.T) {
	t.Run("count error", func(t *testing.T) {
		log := &sourceAgentRunnerCallLog{}
		harness := &sourceAgentRunnerHTTPHarness{log: log}
		outbox := newSourceAgentCommandTestOutbox(t)
		if err := outbox.Close(); err != nil {
			t.Fatal(err)
		}
		commands := &fakeSourceAgentCommandClient{log: log}
		runner := newSourceAgentCommandTestRunner(t, harness, outbox, &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, nil, nil)
		if _, err := runner.RunOnce(context.Background()); err == nil {
			t.Fatal("RunOnce() succeeded with closed outbox")
		}
		if got := log.snapshot(); len(got) != 0 {
			t.Fatalf("network calls=%v", got)
		}
	})

	t.Run("heartbeat error", func(t *testing.T) {
		log := &sourceAgentRunnerCallLog{}
		harness := &sourceAgentRunnerHTTPHarness{log: log, heartbeatStatus: http.StatusServiceUnavailable}
		commands := &fakeSourceAgentCommandClient{log: log}
		runner := newSourceAgentCommandTestRunner(t, harness, newSourceAgentCommandTestOutbox(t), &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}}, commands, nil, nil)
		if _, err := runner.RunOnce(context.Background()); err == nil {
			t.Fatal("RunOnce() succeeded after heartbeat error")
		}
		if got := strings.Join(log.snapshot(), ","); got != "heartbeat" {
			t.Fatalf("call order=%q", got)
		}
	})
}
