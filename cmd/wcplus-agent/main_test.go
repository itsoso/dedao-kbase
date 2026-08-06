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

func TestMain(m *testing.M) {
	previous := wcplusAgentUpgradeFactory
	wcplusAgentUpgradeFactory = func(*app.SourceAgentClient) (wcplusWorkerUpgradeRuntime, error) {
		return wcplusWorkerUpgradeRuntime{updater: &wcplusAgentFailClosedUpdater{}}, nil
	}
	code := m.Run()
	wcplusAgentUpgradeFactory = previous
	os.Exit(code)
}

type wcplusAgentTestRoundTripper func(*http.Request) (*http.Response, error)

func (transport wcplusAgentTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type wcplusAgentCLIUpdaterActivator struct{}

func (wcplusAgentCLIUpdaterActivator) StartUpdater(context.Context) error { return nil }

type wcplusAgentCloseErrorForTest struct{ err error }

func (c wcplusAgentCloseErrorForTest) Close() error { return c.err }

func TestWCPlusAgentRuntimeReturnsUpgradeCloseError(t *testing.T) {
	want := errors.New("close failed")
	runtime := &wcplusAgentRuntime{upgrade: wcplusAgentCloseErrorForTest{err: want}}
	if err := runtime.close(); !errors.Is(err, want) {
		t.Fatalf("close() error=%v", err)
	}
}

func TestWCPlusAgentConstructsRealWorkerUpgradeBridge(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installRoot := filepath.Join(root, "installed")
	if err := os.Mkdir(installRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(installRoot, "wcplus-agent")
	updater := filepath.Join(installRoot, "source-agent-updater")
	for _, path := range []string{worker, updater} {
		if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	client, err := app.NewSourceAgentClient(app.SourceAgentConfig{
		RemoteURL: "https://kbase.example.invalid", AgentToken: "agent-secret",
		AgentID: "wcplus-agent-a", StateDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := newWCPlusWorkerUpgradeBridge(client, worker, wcplusAgentCLIUpdaterActivator{})
	if err != nil {
		t.Fatalf("newWCPlusWorkerUpgradeBridge() error = %v", err)
	}
	if bridge == nil {
		t.Fatal("real WC Plus worker upgrade bridge is nil")
	}
	if err := bridge.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWCPlusAgentCLITransportTokenPrecedenceAndFailClosedErrors(t *testing.T) {
	base := map[string]string{
		"KBASE_REMOTE_URL":       "https://kbase.example.invalid",
		"KBASE_SOURCE_AGENT_ID":  "wcplus-agent-a",
		"WCPLUS_AGENT_STATE_DIR": "state",
	}
	t.Run("explicit non-empty environment wins", func(t *testing.T) {
		values := testEnv{}
		for key, value := range base {
			values[key] = value
		}
		values["KBASE_SOURCE_AGENT_TOKEN"] = "env-token"
		called := false
		cfg, err := loadWCPlusAgentConfigWithTransportToken(context.Background(), values.Lookup, func(context.Context) (string, error) {
			called = true
			return "stored-token", nil
		})
		if err != nil || cfg.AgentToken != "env-token" || called {
			t.Fatalf("token=%q loader_called=%t error=%v", cfg.AgentToken, called, err)
		}
	})
	t.Run("missing environment loads shared token", func(t *testing.T) {
		cfg, err := loadWCPlusAgentConfigWithTransportToken(context.Background(), testEnv(base).Lookup, func(context.Context) (string, error) {
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
		{name: "blank environment", values: map[string]string{"KBASE_SOURCE_AGENT_TOKEN": "\t"}, loader: func(context.Context) (string, error) { return "fallback", nil }},
		{name: "oversize environment", values: map[string]string{"KBASE_SOURCE_AGENT_TOKEN": strings.Repeat("x", 1025)}, loader: func(context.Context) (string, error) { return "fallback", nil }},
		{name: "missing secret", loader: func(context.Context) (string, error) { return "", errors.New("raw /private/tmp token-sentinel") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := testEnv{}
			for key, value := range base {
				values[key] = value
			}
			for key, value := range test.values {
				values[key] = value
			}
			_, err := loadWCPlusAgentConfigWithTransportToken(context.Background(), values.Lookup, test.loader)
			if err == nil {
				t.Fatal("expected token error")
			}
			for _, forbidden := range []string{"fallback", "/private/tmp", "token-sentinel"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("error leaked %q: %v", forbidden, err)
				}
			}
		})
	}
}

func TestWCPlusConfigLoaderStopsWhenContextIsCanceledWithoutLegacyFallback(t *testing.T) {
	previous := wcplusTransportTokenLoader
	defer func() { wcplusTransportTokenLoader = previous }()
	loaderStopped := make(chan struct{})
	wcplusTransportTokenLoader = func(ctx context.Context) (string, error) {
		<-ctx.Done()
		close(loaderStopped)
		return "", ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	values := testEnv{
		"KBASE_REMOTE_URL":       "https://kbase.example.invalid",
		"KBASE_SOURCE_AGENT_ID":  "wcplus-agent-a",
		"WCPLUS_AGENT_STATE_DIR": "state",
	}
	_, err := loadWCPlusAgentConfig(ctx, values.Lookup)
	if !errors.Is(err, sourceagentsecret.ErrTransportTokenUnavailable) {
		t.Fatalf("error=%v", err)
	}
	select {
	case <-loaderStopped:
	case <-time.After(time.Second):
		t.Fatal("WC Plus loader did not observe context cancellation")
	}
}

func TestWCPlusAgentDoctorChecksLocalAndRemoteWithoutLeasing(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
		fmt.Fprint(w, "wcplus")
	}))
	defer local.Close()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/source-agent/lease" {
			t.Fatalf("unexpected remote path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer agent-secret" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var payload struct {
			Capabilities []string `json:"capabilities"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(payload.Capabilities) != 0 {
			t.Fatalf("doctor leased capabilities: %#v", payload.Capabilities)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"run":null}`)
	}))
	defer remote.Close()

	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"doctor"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI doctor: %v, stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ok":true`) || strings.Contains(stdout.String(), "agent-secret") {
		t.Fatalf("doctor output = %s", stdout.String())
	}
}

func TestWCPlusAgentOnceHeartbeatsFlushesAndPolls(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "wcplus")
	}))
	defer local.Close()
	var calls []string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","wcplus_healthy":true}}`)
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

	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"once"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI once: %v, stderr=%s", err, stderr.String())
	}
	if strings.Join(calls, ",") != "heartbeat,command,lease" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestWCPlusAgentCapabilityRuntimeUsesSharedControlRunner(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<title>wcplusPro 2.3.4</title>`)
	}))
	defer local.Close()
	var calls []string
	var heartbeat struct {
		WorkerType      string         `json:"worker_type"`
		Platform        string         `json:"platform"`
		Architecture    string         `json:"architecture"`
		Version         string         `json:"version"`
		ProtocolVersion string         `json:"protocol_version"`
		Health          map[string]any `json:"capability_health"`
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
				t.Fatal(err)
			}
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","desired_state":"active"}}`)
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
	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"once"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI once: %v, stderr=%s", err, stderr.String())
	}
	if strings.Join(calls, ",") != "heartbeat,command,lease" {
		t.Fatalf("calls=%#v", calls)
	}
	if heartbeat.WorkerType != "wcplus-worker" || heartbeat.Platform == "" || heartbeat.Architecture == "" ||
		heartbeat.Version != wcplusAgentVersion || heartbeat.ProtocolVersion != "2026-08-01" || heartbeat.Health["wcplus"] == nil {
		t.Fatalf("heartbeat=%#v", heartbeat)
	}
}

func TestWCPlusAgentBlockedCapabilityOnceDoesNotReportSuccess(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "vendor blocked", http.StatusForbidden)
	}))
	defer local.Close()
	var calls []string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","desired_state":"active"}}`)
		case "/api/source-agent/commands/claim":
			calls = append(calls, "command")
			fmt.Fprint(w, `{"command":null}`)
		case "/api/source-agent/lease":
			t.Fatal("blocked capability must not lease source work")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer remote.Close()

	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"once"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI once: %v, stderr=%s", err, stderr.String())
	}
	if strings.Join(calls, ",") != "heartbeat,command" {
		t.Fatalf("calls=%#v", calls)
	}
	if !strings.Contains(stdout.String(), `"ok":false`) || strings.Contains(stdout.String(), `"status":"succeeded"`) {
		t.Fatalf("once output=%s", stdout.String())
	}
}

func TestWCPlusAgentOnceExecutesLeasedArticleRun(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"gzh":{"Biz":"biz-med","Nickname":"医学参考"},"articles":[{"ID":"article-1","Title":"CLI 同步","URL":"https://mp.weixin.qq.com/s/article-1"}],"total":1}`)
		case "/api/article/content":
			fmt.Fprint(w, `{"ID":"article-1","Title":"CLI 同步","Nickname":"医学参考","URL":"https://mp.weixin.qq.com/s/article-1","Content":"这是一篇由 CLI once 模式读取并发送到远端知识库的完整测试文章正文。"}`)
		default:
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
	}))
	defer local.Close()
	var calls []string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","wcplus_healthy":true}}`)
		case r.URL.Path == "/api/source-agent/commands/claim":
			calls = append(calls, "command")
			fmt.Fprint(w, `{"command":null}`)
		case r.URL.Path == "/api/source-agent/lease":
			calls = append(calls, "lease")
			fmt.Fprint(w, `{"run":{"id":"run-1","status":"running","requested_operation":"existing_articles","subscription":{"id":"sub-1","source_type":"wcplus_wechat_article","source_account_key":"biz-med","source_account":"医学参考","operation":"existing_articles","enabled":true,"options":{"limit":10}}}}`)
		case strings.HasSuffix(r.URL.Path, "/items"):
			calls = append(calls, "item")
			fmt.Fprint(w, `{"receipt":{"idempotency_key":"idem","run_id":"run-1","item_id":"item-1","source_item_key":"article-1","outcome":"new","target_book_id":"book-1","content_hash":"hash","accepted_at":"2026-07-10T00:00:00Z"}}`)
		case strings.HasSuffix(r.URL.Path, "/complete"):
			calls = append(calls, "complete")
			fmt.Fprint(w, `{"run":{"id":"run-1","status":"succeeded","new_count":1}}`)
		default:
			t.Fatalf("unexpected remote path: %s", r.URL.Path)
		}
	}))
	defer remote.Close()

	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"once"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI once: %v, stderr=%s", err, stderr.String())
	}
	if strings.Join(calls, ",") != "heartbeat,command,lease,item,complete" {
		t.Fatalf("calls = %#v", calls)
	}
	if !strings.Contains(stdout.String(), `"status":"succeeded"`) {
		t.Fatalf("once output = %s", stdout.String())
	}
}

func TestWCPlusAgentRequiresKnownModeAndConfiguration(t *testing.T) {
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"unknown"}, mapLookup(nil), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "doctor, once, or run") {
		t.Fatalf("unknown mode error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI(context.Background(), []string{"doctor"}, mapLookup(nil), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "WCPLUS_AGENT_STATE_DIR") {
		t.Fatalf("missing config error = %v", err)
	}
}

func TestWCPlusAgentBuildInfoIsCredentialFreeAndReportsCompiledIdentity(t *testing.T) {
	var stdout, stderr bytes.Buffer
	lookupCalled := false
	err := runCLI(context.Background(), []string{"build-info"}, func(string) (string, bool) {
		lookupCalled = true
		return "", false
	}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if lookupCalled {
		t.Fatal("build-info loaded environment configuration")
	}
	var info struct {
		WorkerType      string `json:"worker_type"`
		Version         string `json:"version"`
		ProtocolVersion string `json:"protocol_version"`
		Platform        string `json:"platform"`
		Architecture    string `json:"architecture"`
		Revision        string `json:"revision"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.WorkerType != "wcplus-worker" || info.Version != wcplusAgentVersion ||
		info.ProtocolVersion != "2026-08-01" || info.Platform != runtime.GOOS ||
		info.Architecture != runtime.GOARCH || info.Revision != wcplusAgentRevision {
		t.Fatalf("build info=%#v", info)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestWCPlusAgentCheckConfigDoesNotLoadSecretsOrContactServices(t *testing.T) {
	previous := wcplusTransportTokenLoader
	defer func() { wcplusTransportTokenLoader = previous }()
	loaderCalled := false
	wcplusTransportTokenLoader = func(context.Context) (string, error) {
		loaderCalled = true
		return "stored-token", nil
	}
	env := testEnv{
		"KBASE_REMOTE_URL":       "https://kbase.example.invalid/base",
		"KBASE_SOURCE_AGENT_ID":  "wcplus-agent-a",
		"WCPLUS_AGENT_STATE_DIR": t.TempDir(),
		"WCPLUSPRO_BASE_URL":     "http://127.0.0.1:5001/api",
	}
	var stdout, stderr strings.Builder
	tokenLookupCalled := false
	err := runCLI(context.Background(), []string{"check-config"}, func(key string) (string, bool) {
		if key == "KBASE_SOURCE_AGENT_TOKEN" {
			tokenLookupCalled = true
		}
		return env.Lookup(key)
	}, &stdout, &stderr)
	if err != nil || loaderCalled || tokenLookupCalled || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("loader_called=%t token_lookup_called=%t stdout=%q stderr=%q error=%v", loaderCalled, tokenLookupCalled, stdout.String(), stderr.String(), err)
	}
}

func TestWCPlusAgentCapabilityStateDirectoryPrefersWorkerSpecificPath(t *testing.T) {
	env := testEnv{
		"KBASE_REMOTE_URL":       "https://kbase.example.invalid",
		"KBASE_SOURCE_AGENT_ID":  "wcplus-agent-a",
		"SOURCE_AGENT_STATE_DIR": "wechat-state",
		"WCPLUS_AGENT_STATE_DIR": "wcplus-state",
		"WCPLUSPRO_BASE_URL":     "http://127.0.0.1:5001",
	}
	cfg, err := loadWCPlusAgentConfigOnly(env.Lookup)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != "wcplus-state" {
		t.Fatalf("state=%q", cfg.StateDir)
	}
}

func TestWCPlusAgentCapabilityStateDirectoryRequiresWorkerSpecificPath(t *testing.T) {
	env := testEnv{
		"KBASE_REMOTE_URL":       "https://kbase.example.invalid",
		"KBASE_SOURCE_AGENT_ID":  "wcplus-agent-a",
		"SOURCE_AGENT_STATE_DIR": t.TempDir(),
		"WCPLUSPRO_BASE_URL":     "http://127.0.0.1:5001",
	}
	_, err := loadWCPlusAgentConfigOnly(env.Lookup)
	if err == nil || !strings.Contains(err.Error(), "WCPLUS_AGENT_STATE_DIR is required") {
		t.Fatalf("generic-only state directory error=%v", err)
	}
}

func TestWCPlusAgentCheckConfigRejectsInvalidAgentIDOffline(t *testing.T) {
	previous := wcplusTransportTokenLoader
	defer func() { wcplusTransportTokenLoader = previous }()
	previousTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = previousTransport }()

	for _, test := range []struct {
		name    string
		agentID string
		want    string
	}{
		{name: "invalid character", agentID: "wcplus/agent", want: "invalid characters"},
		{name: "too long", agentID: strings.Repeat("a", 129), want: "exceeds 128"},
	} {
		t.Run(test.name, func(t *testing.T) {
			networkCalled := false
			http.DefaultTransport = wcplusAgentTestRoundTripper(func(*http.Request) (*http.Response, error) {
				networkCalled = true
				return nil, errors.New("network access is forbidden during check-config")
			})
			loaderCalled := false
			wcplusTransportTokenLoader = func(context.Context) (string, error) {
				loaderCalled = true
				return "stored-token", nil
			}
			env := testEnv{
				"KBASE_REMOTE_URL":       "http://127.0.0.1:1",
				"KBASE_SOURCE_AGENT_ID":  test.agentID,
				"WCPLUS_AGENT_STATE_DIR": t.TempDir(),
				"WCPLUSPRO_BASE_URL":     "http://127.0.0.1:5001",
			}
			var stdout, stderr strings.Builder
			tokenLookupCalled := false
			err := runCLI(context.Background(), []string{"check-config"}, func(key string) (string, bool) {
				if key == "KBASE_SOURCE_AGENT_TOKEN" {
					tokenLookupCalled = true
				}
				return env.Lookup(key)
			}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("check-config error = %v, want %q", err, test.want)
			}
			if loaderCalled || tokenLookupCalled || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("loader_called=%t token_lookup_called=%t stdout=%q stderr=%q", loaderCalled, tokenLookupCalled, stdout.String(), stderr.String())
			}
			if networkCalled {
				t.Error("check-config contacted the configured remote service")
			}
		})
	}
}

type testEnv map[string]string

func (e testEnv) Lookup(key string) (string, bool) {
	value, ok := e[key]
	return value, ok
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return testEnv(values).Lookup
}

func wcplusAgentTestEnv(remoteURL, localURL, stateDir string) testEnv {
	return testEnv{
		"KBASE_REMOTE_URL":         remoteURL,
		"KBASE_SOURCE_AGENT_TOKEN": "agent-secret",
		"KBASE_SOURCE_AGENT_ID":    "agent-a",
		"WCPLUS_AGENT_STATE_DIR":   stateDir,
		"WCPLUSPRO_BASE_URL":       localURL,
	}
}
