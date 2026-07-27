package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

func TestDefaultSystemKBExportPathUsesRepoDirEnv(t *testing.T) {
	t.Setenv("KBASE_SYSTEM_KB_EXPORT_PATH", "")
	t.Setenv("DEDAO_KBASE_ROOT", "")
	t.Setenv("DEDAO_WIKI_REPO", "")
	t.Setenv("DEDAO_WIKI_REPO_DIR", "/tmp/wiki-root")

	got := defaultSystemKBExportPath()
	want := filepath.Join("/tmp/wiki-root", "artifacts", "system_kb_export.json")
	if got != want {
		t.Fatalf("defaultSystemKBExportPath() = %q, want %q", got, want)
	}
}

func TestDefaultSystemKBExportPathHasNoPrivateFallback(t *testing.T) {
	t.Setenv("KBASE_SYSTEM_KB_EXPORT_PATH", "")
	t.Setenv("DEDAO_KBASE_ROOT", "")
	t.Setenv("DEDAO_WIKI_REPO", "")
	t.Setenv("DEDAO_WIKI_REPO_DIR", "")

	got := defaultSystemKBExportPath()
	privatePathToken := "/" + "Users" + "/"
	privateUserToken := "li" + "qiuhua"
	if strings.Contains(got, privatePathToken) || strings.Contains(got, privateUserToken) {
		t.Fatalf("defaultSystemKBExportPath leaks a private fallback path: %q", got)
	}
}

func TestDefaultWebDirUsesEnv(t *testing.T) {
	t.Setenv("KBASE_WEB_DIR", "/tmp/kbase-web")

	if got := defaultWebDir(); got != "/tmp/kbase-web" {
		t.Fatalf("defaultWebDir() = %q, want env value", got)
	}
}

func TestDefaultWebDirUsesRepoLocalFrontendWeb(t *testing.T) {
	t.Setenv("KBASE_WEB_DIR", "")
	root := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("Chdir cleanup returned error: %v", err)
		}
	})
	if err := os.Mkdir(filepath.Join(root, "frontend-web"), os.ModePerm); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	if got := defaultWebDir(); got != "frontend-web" {
		t.Fatalf("defaultWebDir() = %q, want frontend-web", got)
	}
}

func TestWCPlusBaseURLConfiguredFromEnvSupportsWCPlusPro(t *testing.T) {
	t.Setenv("WCPLUS_BASE_URL", "")
	t.Setenv("WCPLUSPRO_BASE_URL", "http://127.0.0.1:5999")

	if !wcplusBaseURLConfiguredFromEnv() {
		t.Fatalf("wcplusBaseURLConfiguredFromEnv() = false, want true for WCPLUSPRO_BASE_URL")
	}

	t.Setenv("WCPLUS_BASE_URL", "")
	t.Setenv("WCPLUSPRO_BASE_URL", "")
	if wcplusBaseURLConfiguredFromEnv() {
		t.Fatalf("wcplusBaseURLConfiguredFromEnv() = true, want false without WC Plus env")
	}
}

func TestDefaultSourceAgentTokenUsesTrimmedEnv(t *testing.T) {
	t.Setenv("KBASE_SOURCE_AGENT_TOKEN", "  source-agent-secret  ")
	if got := defaultSourceAgentToken(); got != "source-agent-secret" {
		t.Fatalf("defaultSourceAgentToken() = %q", got)
	}
}

func TestDefaultAgentPublisherTokenUsesTrimmedEnv(t *testing.T) {
	t.Setenv("KBASE_AGENT_PUBLISHER_TOKEN", "  publisher-secret  ")
	if got := defaultAgentPublisherToken(); got != "publisher-secret" {
		t.Fatalf("defaultAgentPublisherToken() = %q", got)
	}
}

func TestValidateKBaseTokenSeparation(t *testing.T) {
	for _, test := range []struct {
		name           string
		adminToken     string
		agentToken     string
		publisherToken string
		wantError      bool
	}{
		{name: "distinct", adminToken: "admin-secret", agentToken: "agent-secret", publisherToken: "publisher-secret"},
		{name: "agent disabled", adminToken: "admin-secret"},
		{name: "admin disabled", agentToken: "agent-secret"},
		{name: "shared", adminToken: "shared-secret", agentToken: "shared-secret", wantError: true},
		{name: "publisher shares consumer", adminToken: "shared-secret", publisherToken: "shared-secret", wantError: true},
		{name: "publisher shares source agent", agentToken: "shared-secret", publisherToken: "shared-secret", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateKBaseTokenSeparation(test.adminToken, test.agentToken, test.publisherToken)
			if (err != nil) != test.wantError {
				t.Fatalf("validateKBaseTokenSeparation() error = %v, wantError=%v", err, test.wantError)
			}
		})
	}
}

func TestValidateBrowserSessionConfiguration(t *testing.T) {
	browserProxyValue := strings.Repeat("browser-proxy-", 3)
	for _, test := range []struct {
		name      string
		addr      string
		secret    string
		tokens    []string
		wantError bool
	}{
		{name: "disabled", addr: "0.0.0.0:8719"},
		{name: "IPv4 loopback", addr: "127.0.0.1:8719", secret: browserProxyValue},
		{name: "IPv6 loopback", addr: "[::1]:8719", secret: browserProxyValue},
		{name: "localhost", addr: "localhost:8719", secret: browserProxyValue},
		{name: "zero port", addr: "127.0.0.1:0", secret: browserProxyValue, wantError: true},
		{name: "port above range", addr: "127.0.0.1:65536", secret: browserProxyValue, wantError: true},
		{name: "wildcard", addr: ":8719", secret: browserProxyValue, wantError: true},
		{name: "IPv4 wildcard", addr: "0.0.0.0:8719", secret: browserProxyValue, wantError: true},
		{name: "public address", addr: "192.0.2.10:8719", secret: browserProxyValue, wantError: true},
		{name: "short secret", addr: "127.0.0.1:8719", secret: "short", wantError: true},
		{name: "too long", addr: "127.0.0.1:8719", secret: strings.Repeat("x", 129), wantError: true},
		{name: "whitespace secret", addr: "127.0.0.1:8719", secret: strings.Repeat("x", 31) + " ", wantError: true},
		{name: "dollar expansion", addr: "127.0.0.1:8719", secret: strings.Repeat("x", 31) + "$", wantError: true},
		{name: "quoted syntax", addr: "127.0.0.1:8719", secret: strings.Repeat("x", 31) + `"`, wantError: true},
		{name: "path separator", addr: "127.0.0.1:8719", secret: strings.Repeat("x", 31) + "/", wantError: true},
		{name: "shared admin token", addr: "127.0.0.1:8719", secret: browserProxyValue, tokens: []string{browserProxyValue}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateBrowserSessionConfiguration(test.addr, test.secret, test.tokens...)
			if (err != nil) != test.wantError {
				t.Fatalf("validateBrowserSessionConfiguration() error = %v, wantError=%v", err, test.wantError)
			}
		})
	}
}

func TestDefaultBrowserSessionSecretUsesTrimmedEnvironment(t *testing.T) {
	t.Setenv("KBASE_BROWSER_SESSION_SECRET", "  browser-proxy-secret-0123456789abcdef  ")
	if got := defaultBrowserSessionSecret(); got != "browser-proxy-secret-0123456789abcdef" {
		t.Fatalf("defaultBrowserSessionSecret() = %q", got)
	}
}

func TestStartSourceSchedulerRequiresSourceAgentTokenAndStopsWithContext(t *testing.T) {
	runnerStarted := make(chan struct{}, 1)
	runner := sourceSchedulerRunFunc(func(ctx context.Context, interval time.Duration, onTick func(app.SourceSchedulerTickResult, error)) {
		runnerStarted <- struct{}{}
		<-ctx.Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	started, done := startSourceScheduler(ctx, "", time.Second, runner, func(string, ...any) {})
	if started {
		t.Fatal("scheduler started without source-agent token")
	}
	select {
	case <-done:
	default:
		t.Fatal("disabled scheduler completion signal is not closed")
	}
	started, done = startSourceScheduler(ctx, "source-agent-secret", time.Second, runner, func(string, ...any) {})
	if !started {
		t.Fatal("scheduler did not start with source-agent token")
	}
	select {
	case <-runnerStarted:
	case <-time.After(time.Second):
		t.Fatal("scheduler runner did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler runner did not stop with context")
	}
}

func TestSourceSchedulerTickIntervalUsesBoundedEnvironmentValue(t *testing.T) {
	t.Setenv("KBASE_SOURCE_SCHEDULER_TICK_SECONDS", "45")
	if got := sourceSchedulerTickInterval(); got != 45*time.Second {
		t.Fatalf("sourceSchedulerTickInterval() = %s", got)
	}
	t.Setenv("KBASE_SOURCE_SCHEDULER_TICK_SECONDS", "0")
	if got := sourceSchedulerTickInterval(); got != 30*time.Second {
		t.Fatalf("zero sourceSchedulerTickInterval() = %s", got)
	}
	t.Setenv("KBASE_SOURCE_SCHEDULER_TICK_SECONDS", "9999")
	if got := sourceSchedulerTickInterval(); got != 5*time.Minute {
		t.Fatalf("bounded sourceSchedulerTickInterval() = %s", got)
	}
}

func TestStartKnowledgeReverificationRunnerStopsWithContext(t *testing.T) {
	runnerStarted := make(chan struct{}, 1)
	runner := knowledgeReverificationRunFunc(func(ctx context.Context, interval time.Duration, onTick func(app.KnowledgeReverificationTickResult, error)) {
		runnerStarted <- struct{}{}
		<-ctx.Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	started, done := startKnowledgeReverificationRunner(ctx, time.Second, runner, func(string, ...any) {})
	if !started {
		t.Fatal("reverification runner did not start")
	}
	select {
	case <-runnerStarted:
	case <-time.After(time.Second):
		t.Fatal("reverification runner did not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reverification runner did not stop with context")
	}
	started, done = startKnowledgeReverificationRunner(context.Background(), time.Second, nil, nil)
	if started {
		t.Fatal("nil reverification runner started")
	}
	select {
	case <-done:
	default:
		t.Fatal("nil runner completion signal is not closed")
	}
}

func TestKnowledgeReverificationDurationsUseBoundedEnvironmentValues(t *testing.T) {
	t.Setenv("KBASE_REVERIFICATION_TICK_SECONDS", "45")
	t.Setenv("KBASE_REVERIFICATION_COOLDOWN_SECONDS", "600")
	t.Setenv("KBASE_REVERIFICATION_STALE_SECONDS", "1200")
	if got := knowledgeReverificationTickInterval(); got != 45*time.Second {
		t.Fatalf("tick interval = %s", got)
	}
	if got := knowledgeReverificationCooldown(); got != 10*time.Minute {
		t.Fatalf("cooldown = %s", got)
	}
	if got := knowledgeReverificationStaleAfter(); got != 20*time.Minute {
		t.Fatalf("stale after = %s", got)
	}

	t.Setenv("KBASE_REVERIFICATION_TICK_SECONDS", "0")
	t.Setenv("KBASE_REVERIFICATION_COOLDOWN_SECONDS", "-1")
	t.Setenv("KBASE_REVERIFICATION_STALE_SECONDS", "999999")
	if got := knowledgeReverificationTickInterval(); got != 30*time.Second {
		t.Fatalf("default tick interval = %s", got)
	}
	if got := knowledgeReverificationCooldown(); got != 5*time.Minute {
		t.Fatalf("default cooldown = %s", got)
	}
	if got := knowledgeReverificationStaleAfter(); got != 24*time.Hour {
		t.Fatalf("bounded stale after = %s", got)
	}
}

func TestEvidenceAuditServerLimitsUseBoundedEnvironmentValues(t *testing.T) {
	t.Setenv("KBASE_AUDIT_WORKERS", "4")
	t.Setenv("KBASE_AUDIT_QUEUE_SIZE", "128")
	t.Setenv("KBASE_AUDIT_MAX_BODY_BYTES", "131072")
	t.Setenv("KBASE_AUDIT_SHUTDOWN_SECONDS", "20")
	if got := evidenceAuditWorkerCount(); got != 4 {
		t.Fatalf("worker count = %d", got)
	}
	if got := evidenceAuditQueueSize(); got != 128 {
		t.Fatalf("queue size = %d", got)
	}
	if got := evidenceAuditMaxBodyBytes(); got != 131072 {
		t.Fatalf("max body = %d", got)
	}
	if got := evidenceAuditShutdownTimeout(); got != 20*time.Second {
		t.Fatalf("shutdown timeout = %s", got)
	}

	t.Setenv("KBASE_AUDIT_WORKERS", "999")
	t.Setenv("KBASE_AUDIT_QUEUE_SIZE", "999999")
	t.Setenv("KBASE_AUDIT_MAX_BODY_BYTES", "99999999")
	t.Setenv("KBASE_AUDIT_SHUTDOWN_SECONDS", "999")
	if got := evidenceAuditWorkerCount(); got != 32 {
		t.Fatalf("bounded worker count = %d", got)
	}
	if got := evidenceAuditQueueSize(); got != 4096 {
		t.Fatalf("bounded queue size = %d", got)
	}
	if got := evidenceAuditMaxBodyBytes(); got != 1<<20 {
		t.Fatalf("bounded max body = %d", got)
	}
	if got := evidenceAuditShutdownTimeout(); got != time.Minute {
		t.Fatalf("bounded shutdown timeout = %s", got)
	}
}

func TestKBaseHTTPServerUsesSafeTimeoutDefaults(t *testing.T) {
	for _, key := range []string{
		"KBASE_HTTP_READ_HEADER_TIMEOUT_SECONDS",
		"KBASE_HTTP_READ_TIMEOUT_SECONDS",
		"KBASE_HTTP_WRITE_TIMEOUT_SECONDS",
		"KBASE_HTTP_IDLE_TIMEOUT_SECONDS",
		"KBASE_HTTP_MAX_HEADER_BYTES",
	} {
		t.Setenv(key, "")
	}
	server, err := newKBaseHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if err != nil {
		t.Fatalf("newKBaseHTTPServer() error = %v", err)
	}
	if server.ReadHeaderTimeout != 5*time.Second ||
		server.ReadTimeout != 30*time.Second ||
		server.WriteTimeout != 2*time.Minute ||
		server.IdleTimeout != time.Minute ||
		server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("unsafe server defaults: %+v", server)
	}
}

func TestKBaseHTTPServerReadsStrictBoundedEnvironment(t *testing.T) {
	t.Setenv("KBASE_HTTP_READ_HEADER_TIMEOUT_SECONDS", "7")
	t.Setenv("KBASE_HTTP_READ_TIMEOUT_SECONDS", "31")
	t.Setenv("KBASE_HTTP_WRITE_TIMEOUT_SECONDS", "121")
	t.Setenv("KBASE_HTTP_IDLE_TIMEOUT_SECONDS", "61")
	t.Setenv("KBASE_HTTP_MAX_HEADER_BYTES", "65536")
	server, err := newKBaseHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if err != nil {
		t.Fatalf("newKBaseHTTPServer() error = %v", err)
	}
	if server.ReadHeaderTimeout != 7*time.Second ||
		server.ReadTimeout != 31*time.Second ||
		server.WriteTimeout != 121*time.Second ||
		server.IdleTimeout != 61*time.Second ||
		server.MaxHeaderBytes != 65536 {
		t.Fatalf("configured server = %+v", server)
	}

	for _, test := range []struct {
		key   string
		value string
	}{
		{key: "KBASE_HTTP_READ_HEADER_TIMEOUT_SECONDS", value: "0"},
		{key: "KBASE_HTTP_READ_TIMEOUT_SECONDS", value: "invalid"},
		{key: "KBASE_HTTP_WRITE_TIMEOUT_SECONDS", value: "301"},
		{key: "KBASE_HTTP_IDLE_TIMEOUT_SECONDS", value: "-1"},
		{key: "KBASE_HTTP_MAX_HEADER_BYTES", value: "4096"},
	} {
		t.Run(test.key, func(t *testing.T) {
			for _, key := range []string{
				"KBASE_HTTP_READ_HEADER_TIMEOUT_SECONDS",
				"KBASE_HTTP_READ_TIMEOUT_SECONDS",
				"KBASE_HTTP_WRITE_TIMEOUT_SECONDS",
				"KBASE_HTTP_IDLE_TIMEOUT_SECONDS",
				"KBASE_HTTP_MAX_HEADER_BYTES",
			} {
				t.Setenv(key, "")
			}
			t.Setenv(test.key, test.value)
			if _, err := newKBaseHTTPServer("127.0.0.1:0", http.NotFoundHandler()); err == nil {
				t.Fatalf("newKBaseHTTPServer() accepted %s=%q", test.key, test.value)
			}
		})
	}
}

func TestEvidenceAuditRetrySigningKeyRequiresServerSecret(t *testing.T) {
	t.Setenv("KBASE_AUDIT_RETRY_SIGNING_KEY", "short")
	if _, err := evidenceAuditRetrySigningKey(); err == nil {
		t.Fatal("short retry signing key accepted")
	}
	t.Setenv("KBASE_AUDIT_RETRY_SIGNING_KEY", "a-server-secret-with-at-least-32-bytes")
	key, err := evidenceAuditRetrySigningKey()
	if err != nil || string(key) != "a-server-secret-with-at-least-32-bytes" {
		t.Fatalf("retry signing key = %q err=%v", key, err)
	}
	if err := validateEvidenceAuditRetryKeySeparation(
		key, "a-server-secret-with-at-least-32-bytes", "source-token", "publisher-token",
	); err == nil {
		t.Fatal("retry signing key was allowed to match bearer token")
	}
	if err := validateEvidenceAuditRetryKeySeparation(
		key, "admin-token", "source-token", "publisher-token",
	); err != nil {
		t.Fatalf("distinct retry signing key rejected: %v", err)
	}
}

func TestNewEvidenceAuditServerRuntimeReturnsDiagnosticWhenTokenPlanMissing(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "")
	t.Setenv("TOKENPLAN_API_KEY", "")
	t.Setenv("DEDAO_TOKENPLAN_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	runtime := newEvidenceAuditServerRuntime(context.Background(), app.NewBookKnowledgeStore(t.TempDir()))
	if runtime.Coordinator != nil {
		t.Fatal("coordinator initialized without TokenPlan credentials")
	}
	if !strings.Contains(runtime.UnavailableReason, "TokenPlan") {
		t.Fatalf("unavailable reason = %q", runtime.UnavailableReason)
	}
}

func TestProofroomDeliveryRuntimeUsesEnvironmentOnlyConfiguration(t *testing.T) {
	t.Setenv("KBASE_PROOFROOM_ENDPOINT", "")
	t.Setenv("KBASE_PROOFROOM_TOKEN", "")
	service, reason := newProofroomDeliveryRuntime()
	if service != nil || !strings.Contains(reason, "KBASE_PROOFROOM_ENDPOINT") {
		t.Fatalf("missing runtime service=%v reason=%q", service, reason)
	}

	t.Setenv("KBASE_PROOFROOM_ENDPOINT", "http://proofroom.example.test/deliver")
	t.Setenv("KBASE_PROOFROOM_TOKEN", "remote-secret")
	service, reason = newProofroomDeliveryRuntime()
	if service != nil || !strings.Contains(reason, "https") {
		t.Fatalf("unsafe runtime service=%v reason=%q", service, reason)
	}

	t.Setenv("KBASE_PROOFROOM_ENDPOINT", "https://proofroom.example.test/deliver")
	t.Setenv("KBASE_PROOFROOM_TOKEN", "remote-secret")
	t.Setenv("KBASE_PROOFROOM_TIMEOUT_SECONDS", "17")
	service, reason = newProofroomDeliveryRuntime()
	if service == nil || reason != "" {
		t.Fatalf("configured runtime service=%v reason=%q", service, reason)
	}
}

func TestProofroomDeliveryTimeoutEnvironmentIsStrictAndBounded(t *testing.T) {
	t.Setenv("KBASE_PROOFROOM_TIMEOUT_SECONDS", "")
	timeout, err := proofroomDeliveryTimeout()
	if err != nil || timeout != 20*time.Second {
		t.Fatalf("default timeout=%s err=%v", timeout, err)
	}
	for _, value := range []string{"0", "invalid", "121"} {
		t.Setenv("KBASE_PROOFROOM_TIMEOUT_SECONDS", value)
		if _, err := proofroomDeliveryTimeout(); err == nil {
			t.Fatalf("timeout accepted %q", value)
		}
	}
}
