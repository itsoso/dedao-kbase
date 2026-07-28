package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

func main() {
	addr := flag.String("addr", envDefault("KBASE_HTTP_ADDR", "127.0.0.1:8719"), "HTTP listen address")
	root := flag.String("root", envDefault("KBASE_BOOK_KNOWLEDGE_ROOT", app.DefaultBookKnowledgeRoot()), "book_knowledge root directory")
	exportPath := flag.String("system-kb-export", defaultSystemKBExportPath(), "system_kb_export.json path")
	webDir := flag.String("web-dir", defaultWebDir(), "static web UI directory")
	authToken := flag.String("auth-token", os.Getenv("KBASE_AUTH_TOKEN"), "bearer token for /api/* routes")
	agentPublisherToken := flag.String("agent-publisher-token", defaultAgentPublisherToken(), "dedicated bearer token for Agent Package publication")
	sourceAgentToken := flag.String("source-agent-token", defaultSourceAgentToken(), "bearer token for /api/source-agent/* routes")
	flag.Parse()
	browserSessionSecret := defaultBrowserSessionSecret()
	sessionConfig := defaultSessionServerConfig()
	sessionConfig.ListenAddr = *addr
	sessionConfig.BrowserProxySecret = browserSessionSecret
	retrySigningKey, retrySigningErr := evidenceAuditRetrySigningKey()
	if err := validateKBaseTokenSeparation(*authToken, *sourceAgentToken, *agentPublisherToken); err != nil {
		log.Fatal(err)
	}
	if err := validateSessionConfiguration(
		sessionConfig,
		*authToken,
		*sourceAgentToken,
		*agentPublisherToken,
		string(retrySigningKey),
	); err != nil {
		log.Fatal(err)
	}
	if retrySigningErr == nil {
		retrySigningErr = validateEvidenceAuditRetryKeySeparation(
			retrySigningKey,
			*authToken,
			*sourceAgentToken,
			*agentPublisherToken,
			sessionConfig.AdminToken,
			sessionConfig.BrowserProxySecret,
		)
	}
	browserSessions, err := newBrowserSessionRuntime(sessionConfig)
	if err != nil {
		log.Fatalf("initialize browser session store: %v", err)
	}
	defer func() {
		if err := browserSessions.Close(); err != nil {
			log.Printf("close browser session store: %v", err)
		}
	}()
	sourceSync, err := app.NewSourceSyncStore(*root)
	if err != nil {
		log.Fatalf("initialize source sync store: %v", err)
	}
	defer sourceSync.Close()
	bookStore := app.NewBookKnowledgeStore(*root)
	knowledgeCatalog, err := app.NewKnowledgeCatalogStore(*root, time.Now)
	if err != nil {
		log.Fatalf("initialize knowledge catalog: %v", err)
	}
	defer knowledgeCatalog.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	auditRuntime := newEvidenceAuditServerRuntime(ctx, bookStore)
	if retrySigningErr != nil {
		log.Printf("evidence audit retry disabled: %v", retrySigningErr)
		retrySigningKey = nil
	}
	proofroomDelivery, proofroomUnavailableReason := newProofroomDeliveryRuntime()

	handlerConfig := app.KBaseHTTPConfig{
		Store:                  bookStore,
		AuthToken:              *authToken,
		AgentPublisherToken:    *agentPublisherToken,
		SystemKBExportPath:     *exportPath,
		StaticDir:              *webDir,
		WeChat:                 app.NewWeChatSourceService(app.WeChatSourceConfigFromEnv()),
		WCPlus:                 app.NewWCPlusSourceService(app.WCPlusSourceConfigFromEnv()),
		SourceSync:             sourceSync,
		SourceAgentToken:       *sourceAgentToken,
		ReverificationCooldown: knowledgeReverificationCooldown(),
		AuditCoordinator:       auditRuntime.Coordinator,
		AuditUnavailableReason: auditRuntime.UnavailableReason,
		AuditMaxBodyBytes:      evidenceAuditMaxBodyBytes(),
		AuditRetrySigningKey:   retrySigningKey,
		AuditLogger: func(event app.EvidenceAuditHTTPLogEvent) {
			log.Printf("evidence audit HTTP error: operation=%s code=%s cause=%s",
				event.Operation, event.Code, event.Cause)
		},
		ProofroomDelivery: proofroomDelivery,
	}
	handler := app.NewKBaseHTTPHandler(applyBrowserSessionRuntime(handlerConfig, browserSessions))

	log.Printf("dedao kbase server listening on %s", *addr)
	log.Printf("book knowledge root: %s", *root)
	log.Printf("system kb export: %s", *exportPath)
	if strings.TrimSpace(*webDir) != "" {
		log.Printf("web dir: %s", *webDir)
	}
	if strings.TrimSpace(*authToken) == "" {
		log.Printf("warning: KBASE_AUTH_TOKEN is empty; /api/* routes will reject requests")
	}
	if strings.TrimSpace(*sourceAgentToken) == "" {
		log.Printf("source agent API disabled until KBASE_SOURCE_AGENT_TOKEN is configured")
	} else {
		log.Printf("source agent API enabled")
	}
	if strings.TrimSpace(*agentPublisherToken) == "" {
		log.Printf("agent package publisher API disabled until KBASE_AGENT_PUBLISHER_TOKEN is configured")
	} else {
		log.Printf("agent package publisher API enabled with a dedicated token")
	}
	if strings.TrimSpace(os.Getenv("WECHAT_MP_TOKEN")) == "" || strings.TrimSpace(os.Getenv("WECHAT_MP_COOKIE")) == "" {
		log.Printf("wechat source: official account search/list disabled until WECHAT_MP_TOKEN and WECHAT_MP_COOKIE are configured")
	}
	if !wcplusBaseURLConfiguredFromEnv() {
		log.Printf("wcplus source: using default local API base http://127.0.0.1:5001")
	}
	if auditRuntime.Coordinator == nil {
		log.Printf("evidence audits disabled: %s", auditRuntime.UnavailableReason)
	} else {
		log.Printf("evidence audits enabled: workers=%d queue=%d", evidenceAuditWorkerCount(), evidenceAuditQueueSize())
	}
	if proofroomDelivery == nil {
		log.Printf("Proofroom delivery disabled: %s", proofroomUnavailableReason)
	} else {
		log.Printf("Proofroom explicit delivery enabled")
	}
	var schedulerDone <-chan struct{}
	if scheduler, schedulerErr := app.NewSourceScheduler(sourceSync, time.Now); schedulerErr != nil {
		log.Fatalf("initialize source scheduler: %v", schedulerErr)
	} else {
		_, schedulerDone = startSourceScheduler(ctx, *sourceAgentToken, sourceSchedulerTickInterval(), scheduler, log.Printf)
	}
	reverificationRunner := app.NewKnowledgeReverificationRunner(bookStore, nil, time.Now, knowledgeReverificationStaleAfter())
	_, reverificationDone := startKnowledgeReverificationRunner(ctx, knowledgeReverificationTickInterval(), reverificationRunner, log.Printf)

	server, err := newKBaseHTTPServer(*addr, handler)
	if err != nil {
		log.Fatalf("invalid HTTP server configuration: %v", err)
	}
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			log.Printf("kbase server shutdown failed: %v", err)
		}
	}()
	listenErr := server.ListenAndServe()
	stop()
	if schedulerDone != nil {
		<-schedulerDone
	}
	<-reverificationDone
	if auditRuntime.Coordinator != nil {
		shutdownContext, cancel := context.WithTimeout(context.Background(), evidenceAuditShutdownTimeout())
		if err := auditRuntime.Coordinator.Shutdown(shutdownContext); err != nil {
			log.Printf("evidence audit coordinator shutdown failed: %v", err)
		}
		cancel()
	}
	if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
		log.Fatal(listenErr)
	}
}

type evidenceAuditServerRuntime struct {
	Coordinator       *app.EvidenceAuditCoordinator
	UnavailableReason string
}

func newProofroomDeliveryRuntime() (*app.ProofroomDeliveryService, string) {
	endpoint := strings.TrimSpace(os.Getenv("KBASE_PROOFROOM_ENDPOINT"))
	token := strings.TrimSpace(os.Getenv("KBASE_PROOFROOM_TOKEN"))
	if endpoint == "" || token == "" {
		return nil, "configure KBASE_PROOFROOM_ENDPOINT and KBASE_PROOFROOM_TOKEN"
	}
	timeout, err := proofroomDeliveryTimeout()
	if err != nil {
		return nil, err.Error()
	}
	service, err := app.NewProofroomDeliveryService(app.ProofroomDeliveryConfig{
		Endpoint: endpoint,
		Token:    token,
		Timeout:  timeout,
	})
	if err != nil {
		return nil, err.Error()
	}
	return service, ""
}

func proofroomDeliveryTimeout() (time.Duration, error) {
	return strictDurationEnvironment("KBASE_PROOFROOM_TIMEOUT_SECONDS", 20, 120)
}

func newEvidenceAuditServerRuntime(
	ctx context.Context,
	store *app.BookKnowledgeStore,
) evidenceAuditServerRuntime {
	modelConfig, err := app.LoadBookTokenPlanConfig()
	if err != nil || strings.TrimSpace(modelConfig.APIKey) == "" {
		return evidenceAuditServerRuntime{
			UnavailableReason: "TokenPlan configuration is unavailable; configure DEDAO_TOKENPLAN_API_KEY",
		}
	}
	coordinator, err := app.NewEvidenceAuditCoordinator(app.EvidenceAuditCoordinatorConfig{
		Store: store, Client: app.NewTokenPlanChatClient(nil),
		RunnerConfig: app.EvidenceAuditRunnerConfig{ModelConfig: modelConfig},
		Workers:      evidenceAuditWorkerCount(), QueueSize: evidenceAuditQueueSize(),
		PollInterval: time.Second,
		Metrics: func(event app.EvidenceAuditCoordinatorEvent) {
			log.Printf("evidence audit coordinator: event=%s audit=%s code=%s attempt=%d retry_after=%s",
				event.Type, event.AuditID, event.ErrorCode, event.Attempt, event.RetryAfter)
		},
	})
	if err != nil {
		return evidenceAuditServerRuntime{UnavailableReason: "evidence audit coordinator initialization failed: " + err.Error()}
	}
	if err := coordinator.Start(ctx); err != nil {
		return evidenceAuditServerRuntime{UnavailableReason: "evidence audit coordinator startup failed: " + err.Error()}
	}
	return evidenceAuditServerRuntime{Coordinator: coordinator}
}

func validateKBaseTokenSeparation(adminToken, sourceAgentToken, agentPublisherToken string) error {
	adminToken = strings.TrimSpace(adminToken)
	sourceAgentToken = strings.TrimSpace(sourceAgentToken)
	agentPublisherToken = strings.TrimSpace(agentPublisherToken)
	if adminToken != "" && sourceAgentToken != "" && adminToken == sourceAgentToken {
		return errors.New("KBASE_SOURCE_AGENT_TOKEN must differ from KBASE_AUTH_TOKEN")
	}
	if agentPublisherToken != "" && adminToken != "" && agentPublisherToken == adminToken {
		return errors.New("KBASE_AGENT_PUBLISHER_TOKEN must differ from KBASE_AUTH_TOKEN")
	}
	if agentPublisherToken != "" && sourceAgentToken != "" && agentPublisherToken == sourceAgentToken {
		return errors.New("KBASE_AGENT_PUBLISHER_TOKEN must differ from KBASE_SOURCE_AGENT_TOKEN")
	}
	return nil
}

func defaultAgentPublisherToken() string {
	return strings.TrimSpace(os.Getenv("KBASE_AGENT_PUBLISHER_TOKEN"))
}

func defaultBrowserSessionSecret() string {
	return strings.TrimSpace(os.Getenv("KBASE_BROWSER_SESSION_SECRET"))
}

const (
	defaultSessionTTL             = 30 * 24 * time.Hour
	defaultSessionRenewalInterval = 5 * time.Minute
	defaultSessionMaxActive       = 10
)

type sessionServerConfig struct {
	ListenAddr         string
	BrowserProxySecret string
	DBPath             string
	PublicOrigin       string
	AdminToken         string
	TTL                time.Duration
	RenewalInterval    time.Duration
	MaxActive          int
}

type browserSessionRuntime struct {
	Store              *app.BrowserSessionStore
	BrowserProxySecret string
	PublicOrigin       string
	AdminToken         string
	TTL                time.Duration
	RenewalInterval    time.Duration
	MaxActive          int
}

func defaultBrowserSessionDBPath() string {
	return envDefault("KBASE_BROWSER_SESSION_DB_PATH", filepath.Join("state", "browser_sessions.sqlite3"))
}

func defaultPublicOrigin() string {
	return os.Getenv("KBASE_PUBLIC_ORIGIN")
}

func defaultSessionAdminToken() string {
	return os.Getenv("KBASE_SESSION_ADMIN_TOKEN")
}

func defaultSessionServerConfig() sessionServerConfig {
	return sessionServerConfig{
		BrowserProxySecret: defaultBrowserSessionSecret(),
		DBPath:             defaultBrowserSessionDBPath(),
		PublicOrigin:       defaultPublicOrigin(),
		AdminToken:         defaultSessionAdminToken(),
		TTL:                defaultSessionTTL,
		RenewalInterval:    defaultSessionRenewalInterval,
		MaxActive:          defaultSessionMaxActive,
	}
}

func newBrowserSessionRuntime(cfg sessionServerConfig) (browserSessionRuntime, error) {
	runtime := browserSessionRuntime{
		BrowserProxySecret: strings.TrimSpace(cfg.BrowserProxySecret),
		PublicOrigin:       strings.TrimSpace(cfg.PublicOrigin),
		AdminToken:         strings.TrimSpace(cfg.AdminToken),
		TTL:                cfg.TTL,
		RenewalInterval:    cfg.RenewalInterval,
		MaxActive:          cfg.MaxActive,
	}
	if runtime.BrowserProxySecret == "" {
		return runtime, nil
	}
	store, err := app.NewBrowserSessionStore(app.BrowserSessionStoreConfig{
		Path:            cfg.DBPath,
		TTL:             cfg.TTL,
		RenewalInterval: cfg.RenewalInterval,
		MaxActive:       cfg.MaxActive,
	})
	if err != nil {
		return browserSessionRuntime{}, fmt.Errorf("open browser session database: %w", err)
	}
	runtime.Store = store
	return runtime, nil
}

func (runtime browserSessionRuntime) Close() error {
	if runtime.Store == nil {
		return nil
	}
	return runtime.Store.Close()
}

func applyBrowserSessionRuntime(cfg app.KBaseHTTPConfig, runtime browserSessionRuntime) app.KBaseHTTPConfig {
	cfg.BrowserSessionSecret = runtime.BrowserProxySecret
	cfg.BrowserSessions = app.BrowserSessionHTTPConfig{
		Store:           runtime.Store,
		AdminToken:      runtime.AdminToken,
		PublicOrigin:    runtime.PublicOrigin,
		TTL:             runtime.TTL,
		RenewalInterval: runtime.RenewalInterval,
		MaxActive:       runtime.MaxActive,
	}
	return cfg
}

func validateSessionConfiguration(cfg sessionServerConfig, reservedSecrets ...string) error {
	browserProxySecret := strings.TrimSpace(cfg.BrowserProxySecret)
	if browserProxySecret == "" {
		return nil
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		return errors.New("KBASE_BROWSER_SESSION_DB_PATH is required when browser sessions are enabled")
	}
	if cfg.TTL <= 0 || cfg.RenewalInterval <= 0 || cfg.RenewalInterval >= cfg.TTL || cfg.MaxActive <= 0 {
		return errors.New("browser session lifecycle configuration is invalid")
	}
	if err := validateBrowserSessionConfiguration(
		cfg.ListenAddr,
		cfg.BrowserProxySecret,
		append(reservedSecrets, cfg.AdminToken)...,
	); err != nil {
		return err
	}
	if err := validatePublicOrigin(cfg.PublicOrigin); err != nil {
		return err
	}
	if err := validateURLSafeSessionSecret("KBASE_SESSION_ADMIN_TOKEN", cfg.AdminToken); err != nil {
		return err
	}
	for _, secret := range append(reservedSecrets, cfg.BrowserProxySecret) {
		secret = strings.TrimSpace(secret)
		if secret != "" && subtle.ConstantTimeCompare([]byte(cfg.AdminToken), []byte(secret)) == 1 {
			return errors.New("KBASE_SESSION_ADMIN_TOKEN must differ from every reserved secret")
		}
	}
	return nil
}

func validatePublicOrigin(rawOrigin string) error {
	origin := strings.TrimSpace(rawOrigin)
	if origin == "" || rawOrigin != origin {
		return errors.New("KBASE_PUBLIC_ORIGIN must be an exact origin without surrounding whitespace")
	}
	parsed, err := url.Parse(origin)
	if err != nil ||
		parsed.Scheme == "" ||
		parsed.Host == "" ||
		parsed.Opaque != "" ||
		parsed.User != nil ||
		parsed.Path != "" ||
		parsed.RawPath != "" ||
		parsed.RawQuery != "" ||
		parsed.ForceQuery ||
		parsed.Fragment != "" {
		return errors.New("KBASE_PUBLIC_ORIGIN must contain only scheme and host with an optional port")
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "https":
	case "http":
	default:
		return errors.New("KBASE_PUBLIC_ORIGIN must use HTTPS, or HTTP only for loopback development")
	}

	host := parsed.Hostname()
	canonicalHost, ip, err := canonicalOriginHost(host)
	if err != nil {
		return err
	}
	port := parsed.Port()
	canonicalAuthority := canonicalHost
	if strings.Contains(canonicalHost, ":") {
		canonicalAuthority = "[" + canonicalHost + "]"
	}
	if port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return errors.New("KBASE_PUBLIC_ORIGIN port must be between 1 and 65535")
		}
		if (scheme == "https" && portNumber == 443) ||
			(scheme == "http" && portNumber == 80) {
			return errors.New("KBASE_PUBLIC_ORIGIN must omit the default port")
		}
		canonicalAuthority = net.JoinHostPort(canonicalHost, strconv.Itoa(portNumber))
	}
	if origin != scheme+"://"+canonicalAuthority {
		return errors.New("KBASE_PUBLIC_ORIGIN must use canonical browser Origin serialization")
	}
	if scheme == "http" &&
		!strings.EqualFold(canonicalHost, "localhost") &&
		(ip == nil || !ip.IsLoopback()) {
		return errors.New("KBASE_PUBLIC_ORIGIN must use HTTPS for a public host")
	}
	return nil
}

func canonicalOriginHost(host string) (string, net.IP, error) {
	if host == "" {
		return "", nil, errors.New("KBASE_PUBLIC_ORIGIN host is required")
	}
	if ip := net.ParseIP(host); ip != nil {
		if strings.Contains(host, ":") {
			return serializeCanonicalIPv6(ip), ip, nil
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String(), ip, nil
		}
		return "", nil, errors.New("KBASE_PUBLIC_ORIGIN host must use canonical browser form")
	}
	for _, char := range host {
		if char > 127 {
			return "", nil, errors.New("KBASE_PUBLIC_ORIGIN host must use canonical ASCII form")
		}
	}
	lowerHost := strings.ToLower(host)
	if strings.Contains(lowerHost, ":") || !isCanonicalDNSHost(lowerHost) {
		return "", nil, errors.New("KBASE_PUBLIC_ORIGIN host must use canonical browser form")
	}
	return lowerHost, nil, nil
}

func serializeCanonicalIPv6(ip net.IP) string {
	ip = ip.To16()
	pieces := make([]string, 8)
	values := make([]uint16, 8)
	for i := range pieces {
		values[i] = uint16(ip[i*2])<<8 | uint16(ip[i*2+1])
		pieces[i] = strconv.FormatUint(uint64(values[i]), 16)
	}
	longestStart, longestLength := -1, 0
	for i := 0; i < len(values); i++ {
		if values[i] != 0 {
			continue
		}
		runEnd := i + 1
		for runEnd < len(values) && values[runEnd] == 0 {
			runEnd++
		}
		if runEnd-i > longestLength {
			longestStart = i
			longestLength = runEnd - i
		}
		i = runEnd - 1
	}
	if longestLength < 2 {
		return strings.Join(pieces, ":")
	}
	left := strings.Join(pieces[:longestStart], ":")
	right := strings.Join(pieces[longestStart+longestLength:], ":")
	return left + "::" + right
}

func isCanonicalDNSHost(host string) bool {
	if len(host) > 253 {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 ||
			label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') &&
				(char < '0' || char > '9') &&
				char != '-' {
				return false
			}
		}
	}
	return !isBrowserNumericLabel(labels[len(labels)-1])
}

func isBrowserNumericLabel(label string) bool {
	allDecimal := true
	for _, char := range label {
		if char < '0' || char > '9' {
			allDecimal = false
			break
		}
	}
	if allDecimal {
		return true
	}
	if !strings.HasPrefix(label, "0x") || len(label) == 2 {
		return false
	}
	for _, char := range label[2:] {
		if (char < '0' || char > '9') &&
			(char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validateURLSafeSessionSecret(name, rawSecret string) error {
	secret := strings.TrimSpace(rawSecret)
	if rawSecret != secret || len(secret) < 32 || len(secret) > 128 {
		return fmt.Errorf("%s must contain 32-128 URL-safe ASCII characters", name)
	}
	for _, char := range []byte(secret) {
		isAlphaNumeric := char >= 'a' && char <= 'z' ||
			char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9'
		if !isAlphaNumeric && char != '_' && char != '-' {
			return fmt.Errorf("%s must contain 32-128 URL-safe ASCII characters", name)
		}
	}
	return nil
}

func validateBrowserSessionConfiguration(
	addr string,
	browserSessionSecret string,
	reservedTokens ...string,
) error {
	if strings.TrimSpace(browserSessionSecret) == "" {
		return nil
	}
	if err := validateURLSafeSessionSecret("KBASE_BROWSER_SESSION_SECRET", browserSessionSecret); err != nil {
		return err
	}
	for _, token := range reservedTokens {
		token = strings.TrimSpace(token)
		if token != "" && subtle.ConstantTimeCompare(
			[]byte(browserSessionSecret),
			[]byte(token),
		) == 1 {
			return errors.New("KBASE_BROWSER_SESSION_SECRET must differ from every API token")
		}
	}
	listenAddr := strings.TrimSpace(addr)
	if listenAddr == "" || addr != listenAddr {
		return errors.New("KBASE_BROWSER_SESSION_SECRET requires an exact loopback listen address")
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return errors.New("KBASE_BROWSER_SESSION_SECRET requires a valid loopback listen address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("KBASE_BROWSER_SESSION_SECRET requires a listen port between 1 and 65535")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("KBASE_BROWSER_SESSION_SECRET requires a loopback listen address")
	}
	return nil
}

type sourceSchedulerRunner interface {
	Run(context.Context, time.Duration, func(app.SourceSchedulerTickResult, error))
}

type sourceSchedulerRunFunc func(context.Context, time.Duration, func(app.SourceSchedulerTickResult, error))

func (f sourceSchedulerRunFunc) Run(ctx context.Context, interval time.Duration, onTick func(app.SourceSchedulerTickResult, error)) {
	f(ctx, interval, onTick)
}

func startSourceScheduler(
	ctx context.Context,
	sourceAgentToken string,
	interval time.Duration,
	runner sourceSchedulerRunner,
	logf func(string, ...any),
) (bool, <-chan struct{}) {
	done := make(chan struct{})
	if strings.TrimSpace(sourceAgentToken) == "" || runner == nil {
		close(done)
		return false, done
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	go func() {
		defer close(done)
		runner.Run(ctx, interval, func(result app.SourceSchedulerTickResult, err error) {
			if err != nil {
				logf("source scheduler tick failed: %v", err)
				return
			}
			logf("source scheduler tick: evaluated=%d queued=%d retried=%d disabled=%d manual=%d future=%d active=%d blocked=%d invalid=%d",
				result.Evaluated, result.Queued, result.Retried, result.SkippedDisabled,
				result.SkippedManual, result.SkippedFuture, result.SkippedActive,
				result.SkippedBlocked, result.InvalidSchedule)
		})
	}()
	return true, done
}

func sourceSchedulerTickInterval() time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv("KBASE_SOURCE_SCHEDULER_TICK_SECONDS")))
	if err != nil || seconds <= 0 {
		seconds = 30
	}
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

type knowledgeReverificationRunner interface {
	Run(context.Context, time.Duration, func(app.KnowledgeReverificationTickResult, error))
}

type knowledgeReverificationRunFunc func(context.Context, time.Duration, func(app.KnowledgeReverificationTickResult, error))

func (f knowledgeReverificationRunFunc) Run(ctx context.Context, interval time.Duration, onTick func(app.KnowledgeReverificationTickResult, error)) {
	f(ctx, interval, onTick)
}

func startKnowledgeReverificationRunner(
	ctx context.Context,
	interval time.Duration,
	runner knowledgeReverificationRunner,
	logf func(string, ...any),
) (bool, <-chan struct{}) {
	done := make(chan struct{})
	if runner == nil {
		close(done)
		return false, done
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	go func() {
		defer close(done)
		runner.Run(ctx, interval, func(result app.KnowledgeReverificationTickResult, err error) {
			if err != nil {
				logf("knowledge reverification tick failed: %v", err)
				return
			}
			if result.Processed {
				logf("knowledge reverification tick: task=%s release=%s status=%s error_code=%s",
					result.TaskID, result.ReleaseID, result.Status, result.ErrorCode)
			}
		})
	}()
	return true, done
}

func knowledgeReverificationTickInterval() time.Duration {
	return boundedSecondsEnvironment("KBASE_REVERIFICATION_TICK_SECONDS", 30, 300)
}

func knowledgeReverificationCooldown() time.Duration {
	return boundedSecondsEnvironment("KBASE_REVERIFICATION_COOLDOWN_SECONDS", 300, 86400)
}

func knowledgeReverificationStaleAfter() time.Duration {
	return boundedSecondsEnvironment("KBASE_REVERIFICATION_STALE_SECONDS", 900, 86400)
}

func boundedSecondsEnvironment(key string, fallback, maximum int) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || seconds <= 0 {
		seconds = fallback
	}
	if seconds > maximum {
		seconds = maximum
	}
	return time.Duration(seconds) * time.Second
}

func evidenceAuditWorkerCount() int {
	return boundedIntegerEnvironment("KBASE_AUDIT_WORKERS", 2, 32)
}

func evidenceAuditQueueSize() int {
	return boundedIntegerEnvironment("KBASE_AUDIT_QUEUE_SIZE", 64, 4096)
}

func evidenceAuditMaxBodyBytes() int64 {
	return int64(boundedIntegerEnvironment("KBASE_AUDIT_MAX_BODY_BYTES", 64<<10, 1<<20))
}

func evidenceAuditShutdownTimeout() time.Duration {
	return time.Duration(boundedIntegerEnvironment("KBASE_AUDIT_SHUTDOWN_SECONDS", 10, 60)) * time.Second
}

func evidenceAuditRetrySigningKey() ([]byte, error) {
	value := strings.TrimSpace(os.Getenv("KBASE_AUDIT_RETRY_SIGNING_KEY"))
	if len(value) < 32 {
		return nil, errors.New("KBASE_AUDIT_RETRY_SIGNING_KEY must contain at least 32 bytes")
	}
	return []byte(value), nil
}

func newKBaseHTTPServer(addr string, handler http.Handler) (*http.Server, error) {
	readHeaderTimeout, err := strictDurationEnvironment(
		"KBASE_HTTP_READ_HEADER_TIMEOUT_SECONDS", 5, 60,
	)
	if err != nil {
		return nil, err
	}
	readTimeout, err := strictDurationEnvironment("KBASE_HTTP_READ_TIMEOUT_SECONDS", 30, 300)
	if err != nil {
		return nil, err
	}
	writeTimeout, err := strictDurationEnvironment("KBASE_HTTP_WRITE_TIMEOUT_SECONDS", 120, 300)
	if err != nil {
		return nil, err
	}
	idleTimeout, err := strictDurationEnvironment("KBASE_HTTP_IDLE_TIMEOUT_SECONDS", 60, 600)
	if err != nil {
		return nil, err
	}
	maxHeaderBytes, err := strictIntegerEnvironment(
		"KBASE_HTTP_MAX_HEADER_BYTES", 1<<20, 8<<10, 4<<20,
	)
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}, nil
}

func strictDurationEnvironment(key string, fallback, maximum int) (time.Duration, error) {
	value, err := strictIntegerEnvironment(key, fallback, 1, maximum)
	if err != nil {
		return 0, err
	}
	return time.Duration(value) * time.Second, nil
}

func strictIntegerEnvironment(key string, fallback, minimum, maximum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", key, minimum, maximum)
	}
	return value, nil
}

func validateEvidenceAuditRetryKeySeparation(key []byte, bearerTokens ...string) error {
	for _, token := range bearerTokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if subtle.ConstantTimeCompare(key, []byte(token)) == 1 {
			return errors.New("KBASE_AUDIT_RETRY_SIGNING_KEY must differ from bearer tokens")
		}
	}
	return nil
}

func boundedIntegerEnvironment(key string, fallback, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		value = fallback
	}
	if value > maximum {
		value = maximum
	}
	return value
}

func envDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value != "" {
		return value
	}
	return fallback
}

func defaultSystemKBExportPath() string {
	if value := strings.TrimSpace(os.Getenv("KBASE_SYSTEM_KB_EXPORT_PATH")); value != "" {
		return value
	}
	if root := strings.TrimSpace(os.Getenv("DEDAO_KBASE_ROOT")); root != "" {
		return filepath.Join(root, "artifacts", "system_kb_export.json")
	}
	if root := strings.TrimSpace(os.Getenv("DEDAO_WIKI_REPO_DIR")); root != "" {
		return filepath.Join(root, "artifacts", "system_kb_export.json")
	}
	if root := strings.TrimSpace(os.Getenv("DEDAO_WIKI_REPO")); root != "" {
		return filepath.Join(root, "artifacts", "system_kb_export.json")
	}
	return filepath.Join("artifacts", "system_kb_export.json")
}

func defaultWebDir() string {
	if value := strings.TrimSpace(os.Getenv("KBASE_WEB_DIR")); value != "" {
		return value
	}
	for _, candidate := range []string{
		filepath.Join("frontend-web", "dist"),
		"frontend-web",
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func wcplusBaseURLConfiguredFromEnv() bool {
	return strings.TrimSpace(os.Getenv("WCPLUS_BASE_URL")) != "" || strings.TrimSpace(os.Getenv("WCPLUSPRO_BASE_URL")) != ""
}

func defaultSourceAgentToken() string {
	return strings.TrimSpace(os.Getenv("KBASE_SOURCE_AGENT_TOKEN"))
}
