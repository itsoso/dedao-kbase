package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
	"github.com/yann0917/dedao-gui/internal/sourceagentsecret"
)

func TestSourceAgentBuildInfoIsCredentialFreeAndReportsCompiledIdentity(t *testing.T) {
	var output bytes.Buffer
	if err := writeSourceAgentBuildInfo(&output); err != nil {
		t.Fatal(err)
	}
	var info struct {
		WorkerType      string `json:"worker_type"`
		Version         string `json:"version"`
		ProtocolVersion string `json:"protocol_version"`
		Platform        string `json:"platform"`
		Architecture    string `json:"architecture"`
		Revision        string `json:"revision"`
	}
	if err := json.Unmarshal(output.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.WorkerType != sourceAgentWorkerType || info.Version != sourceAgentVersion ||
		info.ProtocolVersion != sourceAgentProtocolVersion || info.Platform != runtime.GOOS ||
		info.Architecture != runtime.GOARCH || info.Revision != sourceAgentRevision {
		t.Fatalf("build info=%#v", info)
	}
	lookupCalled := false
	if err := runSourceAgentCLI(context.Background(), []string{"build-info"}, func(string) (string, bool) {
		lookupCalled = true
		return "", false
	}); err != nil {
		t.Fatal(err)
	}
	if lookupCalled {
		t.Fatal("build-info loaded environment configuration")
	}
}

func TestSourceAgentProtocolCLIUsesSharedControlRunner(t *testing.T) {
	var calls []string
	var heartbeat app.SourceAgentHeartbeat
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
				t.Fatal(err)
			}
			fmt.Fprint(w, `{"agent":{"agent_id":"source-agent-a","desired_state":"active"}}`)
		case "/api/source-agent/commands/claim":
			calls = append(calls, "command")
			fmt.Fprint(w, `{"command":null}`)
		case "/api/source-agent/lease":
			calls = append(calls, "lease")
			fmt.Fprint(w, `{"run":null}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer remote.Close()
	client, err := app.NewSourceAgentClient(app.SourceAgentConfig{
		RemoteURL: remote.URL, AgentToken: "agent-secret", AgentID: "source-agent-a", StateDir: t.TempDir(), HTTPClient: remote.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := app.NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	adapter, err := app.NewWeChatSourceAdapter(app.WeChatSourceAdapterConfig{Sessions: fakeSessionHealthProviderForCLI{session: app.WeChatMPSession{Token: "session-secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := newWeChatSourceAgentRunner(client, outbox, adapter, &sourceAgentFailClosedUpdater{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "heartbeat,command,lease" {
		t.Fatalf("calls=%#v", calls)
	}
	if heartbeat.WorkerType != "wechat-worker" || heartbeat.Platform != runtime.GOOS || heartbeat.Architecture != runtime.GOARCH ||
		heartbeat.Version != sourceAgentVersion || heartbeat.ProtocolVersion != "2026-08-01" {
		t.Fatalf("heartbeat=%#v", heartbeat)
	}
}

type sourceAgentCLIUpdaterActivator struct{}

func (sourceAgentCLIUpdaterActivator) StartUpdater(context.Context) error { return nil }

func TestSourceAgentCLIConstructsRealWorkerUpgradeBridge(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installRoot := filepath.Join(root, "installed")
	if err := os.Mkdir(installRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(installRoot, "source-agent")
	updater := filepath.Join(installRoot, "source-agent-updater")
	for _, path := range []string{worker, updater} {
		if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	client, err := app.NewSourceAgentClient(app.SourceAgentConfig{
		RemoteURL: "https://kbase.example.invalid", AgentToken: "agent-secret",
		AgentID: "source-agent-a", StateDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := newWeChatWorkerUpgradeBridge(client, worker, sourceAgentCLIUpdaterActivator{})
	if err != nil {
		t.Fatalf("newWeChatWorkerUpgradeBridge() error = %v", err)
	}
	if bridge == nil {
		t.Fatal("real worker upgrade bridge is nil")
	}
	if err := bridge.Close(); err != nil {
		t.Fatal(err)
	}
}

type sourceAgentTestRoundTripper func(*http.Request) (*http.Response, error)

func (transport sourceAgentTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestSourceAgentCLITransportTokenPrecedenceAndFailClosedErrors(t *testing.T) {
	base := map[string]string{
		"KBASE_REMOTE_URL":       "https://kbase.example.invalid",
		"KBASE_SOURCE_AGENT_ID":  "source-agent-a",
		"SOURCE_AGENT_STATE_DIR": "state",
	}
	env := func(values map[string]string) sourceEnvironmentLookup {
		return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
	}
	t.Run("explicit non-empty environment wins", func(t *testing.T) {
		values := map[string]string{}
		for key, value := range base {
			values[key] = value
		}
		values["KBASE_SOURCE_AGENT_TOKEN"] = "env-token"
		called := false
		cfg, err := loadSourceAgentConfigWithTransportToken(context.Background(), env(values), func(context.Context) (string, error) {
			called = true
			return "stored-token", nil
		})
		if err != nil || cfg.AgentToken != "env-token" || called {
			t.Fatalf("token=%q loader_called=%t error=%v", cfg.AgentToken, called, err)
		}
	})
	t.Run("missing environment loads shared token", func(t *testing.T) {
		cfg, err := loadSourceAgentConfigWithTransportToken(context.Background(), env(base), func(context.Context) (string, error) {
			return "stored-token", nil
		})
		if err != nil || cfg.AgentToken != "stored-token" {
			t.Fatalf("token=%q error=%v", cfg.AgentToken, err)
		}
	})
	for _, test := range []struct {
		name   string
		values map[string]string
		loader func(context.Context) (string, error)
	}{
		{name: "blank environment", values: map[string]string{"KBASE_SOURCE_AGENT_TOKEN": "  "}, loader: func(context.Context) (string, error) { return "fallback", nil }},
		{name: "oversize environment", values: map[string]string{"KBASE_SOURCE_AGENT_TOKEN": strings.Repeat("x", 1025)}, loader: func(context.Context) (string, error) { return "fallback", nil }},
		{name: "missing secret", loader: func(context.Context) (string, error) { return "", errors.New("raw /Users/private token-sentinel") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			for key, value := range test.values {
				values[key] = value
			}
			_, err := loadSourceAgentConfigWithTransportToken(context.Background(), env(values), test.loader)
			if err == nil {
				t.Fatal("expected token error")
			}
			for _, forbidden := range []string{"fallback", "/Users/", "token-sentinel"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("error leaked %q: %v", forbidden, err)
				}
			}
		})
	}
}

func TestSourceAgentConfigFallsBackToValidatedLegacyAccount(t *testing.T) {
	values := map[string]string{
		"KBASE_REMOTE_URL":       "https://kbase.example.invalid/base",
		"KBASE_SOURCE_AGENT_ID":  "source-agent-a",
		"SOURCE_AGENT_STATE_DIR": "state",
	}
	legacyCalls := 0
	cfg, err := loadSourceAgentConfigWithTransportTokenFallback(
		context.Background(),
		func(key string) (string, bool) { value, ok := values[key]; return value, ok },
		func(context.Context) (string, error) { return "", sourceagentsecret.ErrTransportTokenNotFound },
		func(_ context.Context, agentID string) (string, error) {
			legacyCalls++
			if agentID != "source-agent-a" {
				t.Fatalf("agentID=%q", agentID)
			}
			return "legacy-token", nil
		},
	)
	if err != nil || cfg.AgentToken != "legacy-token" || legacyCalls != 1 {
		t.Fatalf("config=%#v legacy_calls=%d error=%v", cfg, legacyCalls, err)
	}
}

func TestSourceAgentConfigLoaderStopsWhenContextIsCanceled(t *testing.T) {
	previousFixed := sourceAgentTransportTokenLoader
	previousLegacy := sourceAgentLegacyTransportTokenLoader
	defer func() {
		sourceAgentTransportTokenLoader = previousFixed
		sourceAgentLegacyTransportTokenLoader = previousLegacy
	}()
	loaderStopped := make(chan struct{})
	sourceAgentTransportTokenLoader = func(ctx context.Context) (string, error) {
		<-ctx.Done()
		close(loaderStopped)
		return "", ctx.Err()
	}
	sourceAgentLegacyTransportTokenLoader = func(context.Context, string) (string, error) {
		t.Fatal("legacy loader must not run for a canceled fixed loader")
		return "", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	values := map[string]string{
		"KBASE_REMOTE_URL":       "https://kbase.example.invalid",
		"KBASE_SOURCE_AGENT_ID":  "source-agent-a",
		"SOURCE_AGENT_STATE_DIR": "state",
	}
	_, err := loadSourceAgentConfig(ctx, func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if !errors.Is(err, sourceagentsecret.ErrTransportTokenUnavailable) {
		t.Fatalf("error=%v", err)
	}
	select {
	case <-loaderStopped:
	case <-time.After(time.Second):
		t.Fatal("fixed loader did not observe context cancellation")
	}
}

type fakeSourceAgentCycleRunner struct {
	calls  int
	errors []error
	cancel context.CancelFunc
}

type fakeSourceAgentAuthChecker struct{ err error }

func (c fakeSourceAgentAuthChecker) CheckAuth(context.Context) error { return c.err }

func (r *fakeSourceAgentCycleRunner) RunOnce(context.Context) (app.SourceAgentCycleResult, error) {
	r.calls++
	if r.calls >= 3 && r.cancel != nil {
		r.cancel()
	}
	if r.calls <= len(r.errors) {
		return app.SourceAgentCycleResult{}, r.errors[r.calls-1]
	}
	return app.SourceAgentCycleResult{OK: true}, nil
}

func TestSourceAgentCLIConfigPrefersGenericStateDirectory(t *testing.T) {
	values := map[string]string{"KBASE_REMOTE_URL": "https://kbase.example.invalid", "KBASE_SOURCE_AGENT_TOKEN": "agent-value", "KBASE_SOURCE_AGENT_ID": "agent-a", "SOURCE_AGENT_STATE_DIR": "state"}
	cfg, err := loadSourceAgentConfig(context.Background(), func(key string) (string, bool) { v, ok := values[key]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != "state" {
		t.Fatalf("state=%q", cfg.StateDir)
	}
}

func TestSourceAgentProtocolCLIStateDirectoryDoesNotUseWCPlusDirectory(t *testing.T) {
	values := map[string]string{
		"KBASE_REMOTE_URL":         "https://kbase.example.invalid",
		"KBASE_SOURCE_AGENT_ID":    "source-agent-a",
		"SOURCE_AGENT_STATE_DIR":   "wechat-state",
		"WCPLUS_AGENT_STATE_DIR":   "wcplus-state",
		"KBASE_SOURCE_AGENT_TOKEN": "agent-value",
	}
	cfg, err := loadSourceAgentConfigOnly(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != "wechat-state" {
		t.Fatalf("state=%q", cfg.StateDir)
	}
}

func TestSourceAgentEnrollmentAddressIsLoopbackOnly(t *testing.T) {
	for _, value := range []string{"127.0.0.1:8765", "localhost:9000", "[::1]:65535"} {
		if _, err := normalizeEnrollmentAddress(value); err != nil {
			t.Fatalf("%s: %v", value, err)
		}
	}
	for _, value := range []string{"0.0.0.0:8765", "127.0.0.2:8765", "localhost:http", "localhost:0", "localhost:65536", "localhost:"} {
		if _, err := normalizeEnrollmentAddress(value); err == nil {
			t.Fatalf("accepted invalid enrollment address %q", value)
		}
	}
}

func TestSourceAgentCheckConfigDoesNotLoadSecretsOrCreateVendorClients(t *testing.T) {
	previousFixed := sourceAgentTransportTokenLoader
	previousLegacy := sourceAgentLegacyTransportTokenLoader
	defer func() {
		sourceAgentTransportTokenLoader = previousFixed
		sourceAgentLegacyTransportTokenLoader = previousLegacy
	}()
	loaderCalled := false
	sourceAgentTransportTokenLoader = func(context.Context) (string, error) {
		loaderCalled = true
		return "stored-token", nil
	}
	sourceAgentLegacyTransportTokenLoader = func(context.Context, string) (string, error) {
		loaderCalled = true
		return "legacy-token", nil
	}
	values := map[string]string{
		"KBASE_REMOTE_URL":         "https://kbase.example.invalid/base",
		"KBASE_SOURCE_AGENT_ID":    "source-agent-a",
		"SOURCE_AGENT_STATE_DIR":   t.TempDir(),
		"SOURCE_AGENT_ENROLL_ADDR": "127.0.0.1:8765",
		"WECHAT_MP_BASE_URL":       "https://blocked-vendor.example.invalid",
	}
	tokenLookupCalled := false
	err := runSourceAgentCLI(context.Background(), []string{"check-config"}, func(key string) (string, bool) {
		if key == "KBASE_SOURCE_AGENT_TOKEN" {
			tokenLookupCalled = true
		}
		value, ok := values[key]
		return value, ok
	})
	if err != nil || loaderCalled || tokenLookupCalled {
		t.Fatalf("loader_called=%t token_lookup_called=%t error=%v", loaderCalled, tokenLookupCalled, err)
	}
}

func TestSourceAgentCheckConfigRejectsInvalidAgentIDOffline(t *testing.T) {
	previousFixed := sourceAgentTransportTokenLoader
	previousLegacy := sourceAgentLegacyTransportTokenLoader
	defer func() {
		sourceAgentTransportTokenLoader = previousFixed
		sourceAgentLegacyTransportTokenLoader = previousLegacy
	}()
	previousTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = previousTransport }()

	for _, test := range []struct {
		name    string
		agentID string
		want    string
	}{
		{name: "invalid character", agentID: "source/agent", want: "invalid characters"},
		{name: "too long", agentID: strings.Repeat("a", 129), want: "exceeds 128"},
	} {
		t.Run(test.name, func(t *testing.T) {
			networkCalled := false
			http.DefaultTransport = sourceAgentTestRoundTripper(func(*http.Request) (*http.Response, error) {
				networkCalled = true
				return nil, errors.New("network access is forbidden during check-config")
			})
			loaderCalled := false
			sourceAgentTransportTokenLoader = func(context.Context) (string, error) {
				loaderCalled = true
				return "stored-token", nil
			}
			sourceAgentLegacyTransportTokenLoader = func(context.Context, string) (string, error) {
				loaderCalled = true
				return "legacy-token", nil
			}
			values := map[string]string{
				"KBASE_REMOTE_URL":         "http://127.0.0.1:1",
				"KBASE_SOURCE_AGENT_ID":    test.agentID,
				"SOURCE_AGENT_STATE_DIR":   t.TempDir(),
				"SOURCE_AGENT_ENROLL_ADDR": "127.0.0.1:8765",
			}
			tokenLookupCalled := false
			err := runSourceAgentCLI(context.Background(), []string{"check-config"}, func(key string) (string, bool) {
				if key == "KBASE_SOURCE_AGENT_TOKEN" {
					tokenLookupCalled = true
				}
				value, ok := values[key]
				return value, ok
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("check-config error = %v, want %q", err, test.want)
			}
			if loaderCalled || tokenLookupCalled {
				t.Errorf("loader_called=%t token_lookup_called=%t", loaderCalled, tokenLookupCalled)
			}
			if networkCalled {
				t.Error("check-config contacted the configured remote service")
			}
		})
	}
}

func TestStoredSessionProviderRejectsExpiredSession(t *testing.T) {
	store := app.NewMemorySourceSecretStore()
	raw, err := json.Marshal(app.WeChatMPSession{Token: "expired", ObservedExpiry: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "wechat-mp-session", raw); err != nil {
		t.Fatal(err)
	}
	_, err = (storedSessionProvider{store: store}).Session(context.Background())
	if !errors.Is(err, app.ErrWeChatMPSessionExpired) {
		t.Fatalf("Session() error=%v", err)
	}
}

func TestSourceAgentRunLoopContinuesAfterCycleFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeSourceAgentCycleRunner{errors: []error{errors.New("temporary-1"), errors.New("temporary-2")}, cancel: cancel}
	var reported []string
	if err := runSourceAgentLoop(ctx, runner, time.Millisecond, func(err error) {
		reported = append(reported, err.Error())
	}); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 3 || len(reported) != 2 {
		t.Fatalf("calls=%d reported=%v", runner.calls, reported)
	}
}

func TestSourceAgentRuntimeStopsWhenEnrollmentFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &fakeSourceAgentCycleRunner{}
	err := runSourceAgentRuntime(ctx, runner, time.Hour, func(context.Context) error {
		return errors.New("enrollment bind failed")
	}, nil)
	if err == nil || err.Error() != "enrollment bind failed" {
		t.Fatalf("runtime error=%v", err)
	}
}

func TestSourceAgentDoctorReportsLoginRequiredWithoutFailingTransport(t *testing.T) {
	report, err := inspectSourceAgent(context.Background(), fakeSourceAgentAuthChecker{}, fakeSessionHealthProviderForCLI{err: app.ErrSourceSecretNotFound})
	if err != nil {
		t.Fatal(err)
	}
	if !report.RemoteAuth || report.WeChatSession != "login_required" {
		t.Fatalf("report=%#v", report)
	}
}

type fakeSessionHealthProviderForCLI struct {
	session app.WeChatMPSession
	err     error
}

func (p fakeSessionHealthProviderForCLI) Session(context.Context) (app.WeChatMPSession, error) {
	return p.session, p.err
}
