package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
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

func TestBrowserSessionEnvironmentHelpers(t *testing.T) {
	t.Setenv("KBASE_BROWSER_SESSION_DB_PATH", "")
	t.Setenv("KBASE_PUBLIC_ORIGIN", "")
	t.Setenv("KBASE_SESSION_ADMIN_TOKEN", "")
	if got := defaultBrowserSessionDBPath(); got != filepath.Join("state", "browser_sessions.sqlite3") {
		t.Fatalf("defaultBrowserSessionDBPath() = %q", got)
	}
	if got := defaultPublicOrigin(); got != "" {
		t.Fatalf("defaultPublicOrigin() = %q", got)
	}
	if got := defaultSessionAdminToken(); got != "" {
		t.Fatalf("defaultSessionAdminToken() = %q", got)
	}

	t.Setenv("KBASE_BROWSER_SESSION_DB_PATH", "  state/custom.sqlite3  ")
	t.Setenv("KBASE_PUBLIC_ORIGIN", "  https://kbase.example.test  ")
	t.Setenv("KBASE_SESSION_ADMIN_TOKEN", "  "+strings.Repeat("a", 32)+"  ")
	if got := defaultBrowserSessionDBPath(); got != filepath.Join("state", "custom.sqlite3") {
		t.Fatalf("configured browser session path = %q", got)
	}
	if got := defaultPublicOrigin(); got != "  https://kbase.example.test  " {
		t.Fatalf("configured public origin = %q", got)
	}
	if got := defaultSessionAdminToken(); got != "  "+strings.Repeat("a", 32)+"  " {
		t.Fatalf("configured session admin token = %q", got)
	}
}

func TestBrowserSessionEnvironmentWhitespaceFailsWhenEnabled(t *testing.T) {
	t.Setenv("KBASE_PUBLIC_ORIGIN", " https://kbase.example.test")
	t.Setenv("KBASE_SESSION_ADMIN_TOKEN", strings.Repeat("a", 32))
	cfg := defaultSessionServerConfig()
	cfg.ListenAddr = "127.0.0.1:8719"
	cfg.BrowserProxySecret = strings.Repeat("p", 32)
	if err := validateSessionConfiguration(cfg); err == nil {
		t.Fatal("enabled browser sessions accepted whitespace around KBASE_PUBLIC_ORIGIN")
	}

	t.Setenv("KBASE_PUBLIC_ORIGIN", "https://kbase.example.test")
	t.Setenv("KBASE_SESSION_ADMIN_TOKEN", strings.Repeat("a", 32)+" ")
	cfg = defaultSessionServerConfig()
	cfg.ListenAddr = "127.0.0.1:8719"
	cfg.BrowserProxySecret = strings.Repeat("p", 32)
	if err := validateSessionConfiguration(cfg); err == nil {
		t.Fatal("enabled browser sessions accepted whitespace around KBASE_SESSION_ADMIN_TOKEN")
	}
}

func TestBrowserSessionDefaults(t *testing.T) {
	cfg := defaultSessionServerConfig()
	if cfg.TTL != 30*24*time.Hour {
		t.Fatalf("TTL = %s", cfg.TTL)
	}
	if cfg.RenewalInterval != 5*time.Minute {
		t.Fatalf("renewal interval = %s", cfg.RenewalInterval)
	}
	if cfg.MaxActive != 10 {
		t.Fatalf("max active = %d", cfg.MaxActive)
	}
}

func TestValidateBrowserSessionConfiguration(t *testing.T) {
	proxySecret := strings.Repeat("p", 32)
	adminToken := strings.Repeat("a", 32)
	valid := sessionServerConfig{
		ListenAddr:         "127.0.0.1:8719",
		BrowserProxySecret: proxySecret,
		DBPath:             filepath.Join("state", "browser_sessions.sqlite3"),
		PublicOrigin:       "https://kbase.example.test",
		AdminToken:         adminToken,
		TTL:                30 * 24 * time.Hour,
		RenewalInterval:    5 * time.Minute,
		MaxActive:          10,
	}

	disabled := valid
	disabled.ListenAddr = "0.0.0.0:8719"
	disabled.BrowserProxySecret = ""
	disabled.PublicOrigin = ""
	disabled.AdminToken = ""
	if err := validateSessionConfiguration(disabled); err != nil {
		t.Fatalf("disabled browser sessions rejected: %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*sessionServerConfig)
	}{
		{name: "empty origin", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "" }},
		{name: "empty database path", mutate: func(cfg *sessionServerConfig) { cfg.DBPath = "" }},
		{name: "zero port", mutate: func(cfg *sessionServerConfig) { cfg.ListenAddr = "127.0.0.1:0" }},
		{name: "malformed listen", mutate: func(cfg *sessionServerConfig) { cfg.ListenAddr = "127.0.0.1" }},
		{name: "listen leading whitespace", mutate: func(cfg *sessionServerConfig) { cfg.ListenAddr = " 127.0.0.1:8719" }},
		{name: "listen trailing whitespace", mutate: func(cfg *sessionServerConfig) { cfg.ListenAddr = "127.0.0.1:8719 " }},
		{name: "public listen", mutate: func(cfg *sessionServerConfig) { cfg.ListenAddr = "192.0.2.10:8719" }},
		{name: "short proxy", mutate: func(cfg *sessionServerConfig) { cfg.BrowserProxySecret = "short" }},
		{name: "long proxy", mutate: func(cfg *sessionServerConfig) { cfg.BrowserProxySecret = strings.Repeat("p", 129) }},
		{name: "proxy whitespace", mutate: func(cfg *sessionServerConfig) { cfg.BrowserProxySecret = " " + strings.Repeat("p", 32) }},
		{name: "proxy non URL safe", mutate: func(cfg *sessionServerConfig) { cfg.BrowserProxySecret = strings.Repeat("p", 31) + "$" }},
		{name: "public HTTP", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "http://kbase.example.test" }},
		{name: "origin path", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test/app" }},
		{name: "origin query", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test?x=1" }},
		{name: "origin fragment", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test#fragment" }},
		{name: "origin userinfo", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://user@kbase.example.test" }},
		{name: "origin trailing slash", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test/" }},
		{name: "origin missing scheme", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "kbase.example.test" }},
		{name: "origin missing host", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://" }},
		{name: "origin malformed port", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test:bad" }},
		{name: "origin uppercase scheme", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "HTTPS://kbase.example.test" }},
		{name: "origin uppercase host", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://KBASE.example.test" }},
		{name: "origin empty port", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test:" }},
		{name: "origin zero port", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test:0" }},
		{name: "origin port above range", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test:65536" }},
		{name: "origin HTTPS default port", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test:443" }},
		{name: "origin HTTP default port", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "http://localhost:80" }},
		{name: "origin leading zero port", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase.example.test:08443" }},
		{name: "origin expanded IPv6", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://[0:0:0:0:0:0:0:1]:8443" }},
		{name: "origin legacy numeric IPv4", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://127.0.0.0x1" }},
		{name: "origin numeric final domain label", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://example.127" }},
		{name: "origin invalid octal final domain label", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://example.09" }},
		{name: "origin hexadecimal final domain label", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://example.0x7f" }},
		{name: "origin invalid DNS underscore", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase_test.example" }},
		{name: "origin invalid DNS leading hyphen", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://-kbase.example" }},
		{name: "origin invalid DNS trailing hyphen", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase-.example" }},
		{name: "origin invalid empty DNS label", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://kbase..example" }},
		{name: "origin dotted mapped IPv6", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://[::ffff:192.0.2.1]" }},
		{name: "origin expanded mapped IPv6", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://[0:0:0:0:0:ffff:c000:201]" }},
		{name: "origin noncanonical tied IPv6 compression", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "https://[1:0:0:2::3:4]" }},
		{name: "origin unsupported scheme", mutate: func(cfg *sessionServerConfig) { cfg.PublicOrigin = "ftp://kbase.example.test" }},
		{name: "empty admin", mutate: func(cfg *sessionServerConfig) { cfg.AdminToken = "" }},
		{name: "short admin", mutate: func(cfg *sessionServerConfig) { cfg.AdminToken = "short" }},
		{name: "long admin", mutate: func(cfg *sessionServerConfig) { cfg.AdminToken = strings.Repeat("a", 129) }},
		{name: "admin whitespace", mutate: func(cfg *sessionServerConfig) { cfg.AdminToken = " " + strings.Repeat("a", 32) }},
		{name: "admin non URL safe", mutate: func(cfg *sessionServerConfig) { cfg.AdminToken = strings.Repeat("a", 31) + "$" }},
		{name: "admin equals proxy", mutate: func(cfg *sessionServerConfig) { cfg.AdminToken = cfg.BrowserProxySecret }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.mutate(&cfg)
			if err := validateSessionConfiguration(cfg); err == nil {
				t.Fatalf("validateSessionConfiguration() accepted %+v", cfg)
			}
		})
	}

	for _, origin := range []string{
		"https://kbase.example.test",
		"https://kbase.example.test:8443",
		"https://[::1]:8443",
		"https://[::ffff:c000:201]",
		"https://[1:0:2:3:4:5:6:7]",
		"https://[1::2:0:0:3:4]",
		"http://localhost:8719",
		"http://127.0.0.1:8719",
		"http://[::1]:8719",
	} {
		t.Run("accept "+origin, func(t *testing.T) {
			cfg := valid
			cfg.PublicOrigin = origin
			if err := validateSessionConfiguration(cfg); err != nil {
				t.Fatalf("validateSessionConfiguration(%q) error = %v", origin, err)
			}
		})
	}
}

func TestBrowserSessionValidationErrorsRedactSecrets(t *testing.T) {
	adminSecret := "AdminLeakSentinel_" + strings.Repeat("a", 32)
	proxySecret := "ProxyLeakSentinel_" + strings.Repeat("p", 32)
	reservedSecret := "ReservedLeakSentinel_" + strings.Repeat("r", 32)
	base := sessionServerConfig{
		ListenAddr:         "127.0.0.1:8719",
		BrowserProxySecret: proxySecret,
		DBPath:             filepath.Join("state", "browser_sessions.sqlite3"),
		PublicOrigin:       "https://kbase.example.test",
		AdminToken:         adminSecret,
		TTL:                30 * 24 * time.Hour,
		RenewalInterval:    5 * time.Minute,
		MaxActive:          10,
	}
	tests := []struct {
		name     string
		config   sessionServerConfig
		reserved []string
	}{
		{
			name: "invalid admin",
			config: func() sessionServerConfig {
				cfg := base
				cfg.AdminToken += "$"
				return cfg
			}(),
		},
		{
			name: "invalid proxy",
			config: func() sessionServerConfig {
				cfg := base
				cfg.BrowserProxySecret += "$"
				return cfg
			}(),
		},
		{
			name:     "admin collision",
			config:   base,
			reserved: []string{adminSecret},
		},
		{
			name:     "proxy collision",
			config:   base,
			reserved: []string{proxySecret},
		},
		{
			name:     "distinct reserved remains private",
			config:   base,
			reserved: []string{reservedSecret, adminSecret},
		},
		{
			name: "malformed listen remains private",
			config: func() sessionServerConfig {
				cfg := base
				cfg.ListenAddr = "ListenLeakSentinel"
				return cfg
			}(),
		},
		{
			name: "malformed origin remains private",
			config: func() sessionServerConfig {
				cfg := base
				cfg.PublicOrigin = "https://OriginLeakSentinel.example/path"
				return cfg
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSessionConfiguration(test.config, test.reserved...)
			if err == nil {
				t.Fatal("validateSessionConfiguration() error = nil")
			}
			for _, secret := range []string{
				test.config.AdminToken,
				test.config.BrowserProxySecret,
				adminSecret,
				proxySecret,
				reservedSecret,
				test.config.ListenAddr,
				test.config.PublicOrigin,
			} {
				if secret != "" && strings.Contains(err.Error(), secret) {
					t.Fatalf("validation error leaked a secret: %q", err)
				}
			}
		})
	}
}

func TestBrowserSessionTokenSeparation(t *testing.T) {
	proxySecret := strings.Repeat("p", 32)
	adminToken := strings.Repeat("a", 32)
	base := sessionServerConfig{
		ListenAddr:         "127.0.0.1:8719",
		BrowserProxySecret: proxySecret,
		DBPath:             filepath.Join("state", "browser_sessions.sqlite3"),
		PublicOrigin:       "https://kbase.example.test",
		AdminToken:         adminToken,
		TTL:                30 * 24 * time.Hour,
		RenewalInterval:    5 * time.Minute,
		MaxActive:          10,
	}
	for _, name := range []string{"API", "publisher", "source agent", "retry signing"} {
		t.Run("admin differs from "+name, func(t *testing.T) {
			if err := validateSessionConfiguration(base, adminToken); err == nil {
				t.Fatalf("admin token was allowed to equal %s token", name)
			}
		})
		t.Run("proxy differs from "+name, func(t *testing.T) {
			if err := validateSessionConfiguration(base, proxySecret); err == nil {
				t.Fatalf("browser proxy secret was allowed to equal %s token", name)
			}
		})
	}
	if err := validateSessionConfiguration(
		base,
		strings.Repeat("b", 32),
		strings.Repeat("c", 32),
		strings.Repeat("d", 32),
		strings.Repeat("e", 32),
	); err != nil {
		t.Fatalf("distinct session secrets rejected: %v", err)
	}
}

func TestBrowserSessionRuntimeRejectsUnusableDatabasePath(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	dbPath := filepath.Join(parent, "database-is-a-directory")
	if err := os.Mkdir(dbPath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	cfg := defaultSessionServerConfig()
	cfg.ListenAddr = "127.0.0.1:8719"
	cfg.BrowserProxySecret = strings.Repeat("p", 32)
	cfg.DBPath = dbPath
	cfg.PublicOrigin = "https://kbase.example.test"
	cfg.AdminToken = strings.Repeat("a", 32)
	if _, err := newBrowserSessionRuntime(cfg); err == nil {
		t.Fatal("newBrowserSessionRuntime() accepted a directory as the SQLite database")
	}
}

func TestBrowserSessionRuntimeDisabledRemainsCompatible(t *testing.T) {
	cfg := defaultSessionServerConfig()
	cfg.ListenAddr = "0.0.0.0:8719"
	cfg.BrowserProxySecret = ""
	cfg.DBPath = filepath.Join(t.TempDir(), "missing", "browser_sessions.sqlite3")
	cfg.PublicOrigin = ""
	cfg.AdminToken = ""
	runtime, err := newBrowserSessionRuntime(cfg)
	if err != nil {
		t.Fatalf("newBrowserSessionRuntime() disabled error = %v", err)
	}
	if runtime.Store != nil {
		t.Fatal("disabled browser session runtime initialized a store")
	}
	if _, err := os.Stat(cfg.DBPath); !os.IsNotExist(err) {
		t.Fatalf("disabled browser session runtime touched database path: %v", err)
	}
}

func TestBrowserSessionRuntimeConfiguresHTTPHandler(t *testing.T) {
	cfg := defaultSessionServerConfig()
	cfg.ListenAddr = "127.0.0.1:8719"
	cfg.BrowserProxySecret = strings.Repeat("p", 32)
	cfg.DBPath = filepath.Join(t.TempDir(), "sessions", "browser_sessions.sqlite3")
	cfg.PublicOrigin = "https://kbase.example.test"
	cfg.AdminToken = strings.Repeat("a", 32)
	runtime, err := newBrowserSessionRuntime(cfg)
	if err != nil {
		t.Fatalf("newBrowserSessionRuntime() error = %v", err)
	}
	t.Cleanup(func() { runtime.Close() })

	handlerConfig := applyBrowserSessionRuntime(app.KBaseHTTPConfig{}, runtime)
	if handlerConfig.BrowserSessions.Store != runtime.Store ||
		handlerConfig.BrowserSessionSecret != cfg.BrowserProxySecret ||
		handlerConfig.BrowserSessions.AdminToken != cfg.AdminToken ||
		handlerConfig.BrowserSessions.PublicOrigin != cfg.PublicOrigin ||
		handlerConfig.BrowserSessions.TTL != 30*24*time.Hour ||
		handlerConfig.BrowserSessions.RenewalInterval != 5*time.Minute ||
		handlerConfig.BrowserSessions.MaxActive != 10 {
		t.Fatalf("handler session config = %+v", handlerConfig)
	}

	handlerValue := reflect.ValueOf(app.NewKBaseHTTPHandler(handlerConfig)).Elem()
	sessionValue := handlerValue.FieldByName("browserSessions")
	if got := sessionValue.FieldByName("Store").Pointer(); got != reflect.ValueOf(runtime.Store).Pointer() {
		t.Fatalf("handler browser session store pointer = %x", got)
	}
	if got := handlerValue.FieldByName("browserSessionSecret").String(); got != cfg.BrowserProxySecret {
		t.Fatalf("handler browser proxy secret = %q", got)
	}
	if got := sessionValue.FieldByName("AdminToken").String(); got != cfg.AdminToken {
		t.Fatalf("handler session admin token = %q", got)
	}
	if got := sessionValue.FieldByName("PublicOrigin").String(); got != cfg.PublicOrigin {
		t.Fatalf("handler public origin = %q", got)
	}
	if got := time.Duration(sessionValue.FieldByName("TTL").Int()); got != 30*24*time.Hour {
		t.Fatalf("handler TTL = %s", got)
	}
	if got := time.Duration(sessionValue.FieldByName("RenewalInterval").Int()); got != 5*time.Minute {
		t.Fatalf("handler renewal interval = %s", got)
	}
	if got := int(sessionValue.FieldByName("MaxActive").Int()); got != 10 {
		t.Fatalf("handler max active = %d", got)
	}

	defaultedHandler := reflect.ValueOf(app.NewKBaseHTTPHandler(app.KBaseHTTPConfig{
		Store: app.NewBookKnowledgeStore(t.TempDir()),
	})).Elem()
	defaultedSession := defaultedHandler.FieldByName("browserSessions")
	if got := time.Duration(defaultedSession.FieldByName("TTL").Int()); got != 30*24*time.Hour {
		t.Fatalf("default handler TTL = %s", got)
	}
	if got := time.Duration(defaultedSession.FieldByName("RenewalInterval").Int()); got != 5*time.Minute {
		t.Fatalf("default handler renewal interval = %s", got)
	}
	if got := int(defaultedSession.FieldByName("MaxActive").Int()); got != 10 {
		t.Fatalf("default handler max active = %d", got)
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
