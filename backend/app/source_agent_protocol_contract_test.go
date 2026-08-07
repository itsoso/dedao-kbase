package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type sourceAgentProtocolFixture struct {
	name           string
	workerType     string
	capabilityName string
	adapter        SourceAdapter
	privateValues  []string
}

func newSourceAgentProtocolFixtures(t *testing.T) []sourceAgentProtocolFixture {
	t.Helper()
	wechat, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: fakeSessionHealthProvider{session: WeChatMPSession{
		Token:   "wechat-credential-sentinel",
		Cookies: []WeChatMPCookie{{Name: "session", Value: "wechat-cookie-sentinel"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	wcplusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<title>wcplusPro 9.483</title>source-body-sentinel`)
	}))
	t.Cleanup(wcplusServer.Close)
	wcplus, err := NewWCPlusSourceAdapter(WCPlusSourceAdapterConfig{
		WCPlus: NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: wcplusServer.URL, HTTPClient: wcplusServer.Client()}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return []sourceAgentProtocolFixture{
		{name: "wechat", workerType: "wechat-worker", capabilityName: "wechat_mp", adapter: wechat, privateValues: []string{"wechat-credential-sentinel", "wechat-cookie-sentinel"}},
		{name: "wcplus", workerType: "wcplus-worker", capabilityName: "wcplus", adapter: wcplus, privateValues: []string{"source-body-sentinel", wcplusServer.URL}},
	}
}

func TestSourceAgentProtocolReportsStableBoundedMetadataWithoutPrivateState(t *testing.T) {
	fixtures := newSourceAgentProtocolFixtures(t)
	paths := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			log := &sourceAgentRunnerCallLog{}
			harness := &sourceAgentRunnerHTTPHarness{log: log, desiredState: SourceAgentDesiredPaused}
			commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{nil}}
			stateDir := t.TempDir()
			outbox, err := NewSourceAgentOutbox(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = outbox.Close() })
			paths[fixture.name] = outbox.DBPath()
			runner := newSourceAgentProtocolRunner(t, harness, outbox, fixture, commands)
			if _, err := runner.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			heartbeats, leases := harness.snapshot()
			if len(heartbeats) != 1 || leases != 0 {
				t.Fatalf("heartbeats=%#v leases=%d", heartbeats, leases)
			}
			heartbeat := heartbeats[0]
			if heartbeat.WorkerType != fixture.workerType || heartbeat.Platform != runtime.GOOS || heartbeat.Architecture != runtime.GOARCH ||
				heartbeat.Version != "0.1.0" || heartbeat.ProtocolVersion != "2026-08-01" {
				t.Fatalf("runtime metadata=%#v", heartbeat)
			}
			if len(heartbeat.Capabilities) == 0 || len(heartbeat.Capabilities) > sourceAgentMaxCapabilities || len(heartbeat.CapabilityHealth) != 1 {
				t.Fatalf("bounded capabilities=%#v", heartbeat)
			}
			health, ok := heartbeat.CapabilityHealth[fixture.capabilityName]
			if !ok || !health.Healthy || len([]rune(health.Version)) > sourceAgentVersionMaxRunes ||
				len([]rune(health.LastError)) > sourceDiagnosticMaxRunes || len([]rune(health.RequiresAction)) > sourceDiagnosticMaxRunes {
				t.Fatalf("capability health=%#v", heartbeat.CapabilityHealth)
			}
			if fixture.name == "wcplus" && health.Version != "9.483" {
				t.Fatalf("WC Plus vendor version=%q", health.Version)
			}
			encoded, err := json.Marshal(heartbeat)
			if err != nil {
				t.Fatal(err)
			}
			privateValues := append([]string{"agent-secret", "source_body", outbox.DBPath(), filepath.Base(outbox.DBPath())}, fixture.privateValues...)
			for _, private := range privateValues {
				if private != "" && strings.Contains(string(encoded), private) {
					t.Fatalf("heartbeat leaked %q: %s", private, encoded)
				}
			}
		})
	}
	if paths["wechat"] == "" || paths["wcplus"] == "" || paths["wechat"] == paths["wcplus"] || filepath.Dir(paths["wechat"]) == filepath.Dir(paths["wcplus"]) {
		t.Fatalf("worker outbox paths are not independent: %#v", paths)
	}
}

func TestSourceAgentProtocolAcceptsBoundedTwoSegmentVendorVersion(t *testing.T) {
	for _, valid := range []string{"9.483", "1.2"} {
		if !validSourceAgentCapabilityVersion(valid) {
			t.Fatalf("bounded two-segment version %q was rejected", valid)
		}
	}
	for _, invalid := range []string{
		"09.483", "9.0483", "9.483-token", "9.483\n", "9." + strings.Repeat("1", sourceAgentVersionMaxRunes),
	} {
		if validSourceAgentCapabilityVersion(invalid) {
			t.Fatalf("unsafe two-segment version %q was accepted", invalid)
		}
	}
}

func TestSourceAgentProtocolUsesSameDiagnosticCommandStateEnum(t *testing.T) {
	for _, fixture := range newSourceAgentProtocolFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			log := &sourceAgentRunnerCallLog{}
			harness := &sourceAgentRunnerHTTPHarness{log: log}
			command := &SourceAgentCommand{
				ID: "cmd-diagnose", TargetAgentID: "agent-a", Type: SourceAgentCommandDiagnose,
				State: SourceAgentCommandClaimed, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
			}
			commands := &fakeSourceAgentCommandClient{log: log, claims: []*SourceAgentCommand{command}}
			outbox := newSourceAgentCommandTestOutbox(t)
			runner := newSourceAgentProtocolRunner(t, harness, outbox, fixture, commands)
			if _, err := runner.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, reports := commands.counts()
			if len(reports) != 1 || reports[0].State != SourceAgentCommandSucceeded || reports[0].Code != SourceAgentCommandCodeDiagnosticComplete {
				t.Fatalf("diagnostic reports=%#v", reports)
			}
			encoded, err := json.Marshal(reports)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range append([]string{"source-body-sentinel", "wechat-credential-sentinel", "wechat-cookie-sentinel"}, fixture.privateValues...) {
				if private != "" && strings.Contains(string(encoded), private) {
					t.Fatalf("diagnostic leaked %q: %s", private, encoded)
				}
			}
		})
	}
}

func TestSourceAgentProtocolPausedAndUpgradeStatesPreventLease(t *testing.T) {
	for _, fixture := range newSourceAgentProtocolFixtures(t) {
		for _, mode := range []string{"paused", "upgrade"} {
			t.Run(fixture.name+"/"+mode, func(t *testing.T) {
				log := &sourceAgentRunnerCallLog{}
				harness := &sourceAgentRunnerHTTPHarness{log: log}
				var claims []*SourceAgentCommand
				if mode == "paused" {
					harness.desiredState = SourceAgentDesiredPaused
					claims = []*SourceAgentCommand{nil}
				} else {
					claims = []*SourceAgentCommand{{
						ID: "cmd-upgrade", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade, State: SourceAgentCommandClaimed,
						UpgradeSpec:            &SourceAgentUpgradeSpec{ArtifactID: "artifact-1", ExpectedCurrentVersion: "0.1.0"},
						ExpectedCurrentVersion: "0.1.0", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
					}}
				}
				commands := &fakeSourceAgentCommandClient{log: log, claims: claims}
				runner := newSourceAgentProtocolRunner(t, harness, newSourceAgentCommandTestOutbox(t), fixture, commands)
				if _, err := runner.RunOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
				_, leases := harness.snapshot()
				if leases != 0 {
					t.Fatalf("lease calls=%d", leases)
				}
				_, reports := commands.counts()
				if mode == "upgrade" && (len(reports) != 1 || reports[0].State != SourceAgentCommandDownloading) {
					t.Fatalf("upgrade reports=%#v", reports)
				}
			})
		}
	}
}

func newSourceAgentProtocolRunner(
	t *testing.T,
	harness *sourceAgentRunnerHTTPHarness,
	outbox *SourceAgentOutbox,
	fixture sourceAgentProtocolFixture,
	commands SourceAgentCommandClient,
) *SourceAgentRunner {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(harness.handler))
	t.Cleanup(server.Close)
	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a", StateDir: t.TempDir(), HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	diagnoser, ok := fixture.adapter.(SourceAgentDiagnoser)
	if !ok {
		t.Fatalf("%s adapter does not implement SourceAgentDiagnoser", fixture.name)
	}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{
		Client: client, CommandClient: commands, Outbox: outbox, Adapter: fixture.adapter, Diagnoser: diagnoser,
		Updater: &protocolFailClosedUpdater{}, WorkerType: fixture.workerType, Version: "0.1.0", ProtocolVersion: "2026-08-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

type protocolFailClosedUpdater struct{}

func (*protocolFailClosedUpdater) Upgrade(context.Context, SourceAgentCommand) SourceAgentUpgradeResult {
	return SourceAgentUpgradeResult{State: SourceAgentCommandFailed, Code: SourceAgentCommandCodeUpgradeFailed}
}
