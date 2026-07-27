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
	if err := validateKBaseTokenSeparation(*authToken, *sourceAgentToken, *agentPublisherToken); err != nil {
		log.Fatal(err)
	}
	if err := validateBrowserSessionConfiguration(
		*addr,
		browserSessionSecret,
		*authToken,
		*sourceAgentToken,
		*agentPublisherToken,
	); err != nil {
		log.Fatal(err)
	}
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
	retrySigningKey, retrySigningErr := evidenceAuditRetrySigningKey()
	if retrySigningErr == nil {
		retrySigningErr = validateEvidenceAuditRetryKeySeparation(
			retrySigningKey, *authToken, *sourceAgentToken, *agentPublisherToken,
		)
	}
	if retrySigningErr != nil {
		log.Printf("evidence audit retry disabled: %v", retrySigningErr)
		retrySigningKey = nil
	}
	proofroomDelivery, proofroomUnavailableReason := newProofroomDeliveryRuntime()

	handler := app.NewKBaseHTTPHandler(app.KBaseHTTPConfig{
		Store:                  bookStore,
		AuthToken:              *authToken,
		BrowserSessionSecret:   browserSessionSecret,
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
	})

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

func validateBrowserSessionConfiguration(
	addr string,
	browserSessionSecret string,
	reservedTokens ...string,
) error {
	rawSecret := browserSessionSecret
	browserSessionSecret = strings.TrimSpace(browserSessionSecret)
	if browserSessionSecret == "" {
		return nil
	}
	if rawSecret != browserSessionSecret ||
		len(browserSessionSecret) < 32 ||
		len(browserSessionSecret) > 128 {
		return errors.New("KBASE_BROWSER_SESSION_SECRET must contain 32-128 URL-safe ASCII characters")
	}
	for _, char := range []byte(browserSessionSecret) {
		isAlphaNumeric := char >= 'a' && char <= 'z' ||
			char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9'
		if !isAlphaNumeric && char != '_' && char != '-' {
			return errors.New("KBASE_BROWSER_SESSION_SECRET must contain 32-128 URL-safe ASCII characters")
		}
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
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("KBASE_BROWSER_SESSION_SECRET requires a valid loopback listen address: %w", err)
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
