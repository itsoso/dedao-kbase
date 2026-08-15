package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
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
	"golang.org/x/net/idna"
)

var buildRevision = "development"

type kBaseServerConfig struct {
	Addr                    string
	Root                    string
	ExportPath              string
	WebDir                  string
	AuthToken               string
	AgentPublisherToken     string
	SourceAgentToken        string
	SourceAgentArtifactRoot string
	EvolutionEnabled        bool
	Research                researchServerConfig
	Session                 sessionServerConfig
	RetrySigningKey         []byte
	RetrySigningErr         error
}

type startupSecretSet struct {
	API            string
	SourceAgent    string
	AgentPublisher string
	AuditRetry     string
}

type configCheckResult struct {
	SchemaVersion int    `json:"schema_version"`
	Status        string `json:"status"`
}

func main() {
	if err := run(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
}

func run() error {
	return runCommand(os.Args[1:], os.Stdout)
}

func runCommand(args []string, stdout io.Writer) error {
	return runCommandWithServerRunner(args, stdout, runKBaseServer)
}

func runCommandWithServerRunner(
	args []string,
	stdout io.Writer,
	runServer func(kBaseServerConfig) error,
) error {
	flags := flag.NewFlagSet("kbase-server", flag.ContinueOnError)
	addr := flags.String("addr", envDefault("KBASE_HTTP_ADDR", "127.0.0.1:8719"), "HTTP listen address")
	root := flags.String("root", envDefault("KBASE_BOOK_KNOWLEDGE_ROOT", app.DefaultBookKnowledgeRoot()), "book_knowledge root directory")
	exportPath := flags.String("system-kb-export", defaultSystemKBExportPath(), "system_kb_export.json path")
	webDir := flags.String("web-dir", defaultWebDir(), "static web UI directory")
	authToken := flags.String("auth-token", os.Getenv("KBASE_AUTH_TOKEN"), "bearer token for /api/* routes")
	agentPublisherToken := flags.String("agent-publisher-token", defaultAgentPublisherToken(), "dedicated bearer token for Agent Package publication")
	sourceAgentToken := flags.String("source-agent-token", defaultSourceAgentToken(), "bearer token for /api/source-agent/* routes")
	evolutionEnabled := flags.Bool("evolution-enabled", true, "enable the Agent evolution control plane")
	checkConfig := flags.Bool("check-config", false, "validate server configuration without starting")
	if err := flags.Parse(args); err != nil {
		return err
	}
	evolutionFlagSet := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == "evolution-enabled" {
			evolutionFlagSet = true
		}
	})
	if !evolutionFlagSet {
		enabled, err := evolutionEnabledFromEnvironment()
		if err != nil {
			return err
		}
		*evolutionEnabled = enabled
	}
	researchConfig, err := researchServerConfigFromEnvironment()
	if err != nil {
		return err
	}

	sessionConfig := defaultSessionServerConfig()
	sessionConfig.ListenAddr = *addr
	sessionConfig.BrowserProxySecret = defaultBrowserSessionSecret()
	retrySigningKey, retrySigningErr := evidenceAuditRetrySigningKey()
	config := kBaseServerConfig{
		Addr:                    *addr,
		Root:                    *root,
		ExportPath:              *exportPath,
		WebDir:                  *webDir,
		AuthToken:               *authToken,
		AgentPublisherToken:     *agentPublisherToken,
		SourceAgentToken:        *sourceAgentToken,
		SourceAgentArtifactRoot: defaultSourceAgentArtifactRoot(),
		EvolutionEnabled:        *evolutionEnabled,
		Research:                researchConfig,
		Session:                 sessionConfig,
		RetrySigningKey:         retrySigningKey,
		RetrySigningErr:         retrySigningErr,
	}
	if !*checkConfig {
		return runServer(config)
	}
	if _, _, err := preflightKBaseServer(config); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(configCheckResult{
		SchemaVersion: 1,
		Status:        "ok",
	})
}

func browserSessionReservedSecrets(secrets startupSecretSet) []string {
	return []string{
		secrets.API,
		secrets.SourceAgent,
		secrets.AgentPublisher,
		secrets.AuditRetry,
	}
}

func runKBaseServer(config kBaseServerConfig) error {
	config, server, err := preflightKBaseServer(config)
	if err != nil {
		return err
	}

	return withBrowserSessionRuntime(config.Session, func(browserSessions browserSessionRuntime) error {
		return serveKBaseServer(config, server, browserSessions)
	})
}

func preflightKBaseServer(config kBaseServerConfig) (kBaseServerConfig, *http.Server, error) {
	if err := validateKBaseTokenSeparation(
		config.AuthToken,
		config.SourceAgentToken,
		config.AgentPublisherToken,
	); err != nil {
		return kBaseServerConfig{}, nil, err
	}
	reservedSecrets := browserSessionReservedSecrets(startupSecretSet{
		API:            config.AuthToken,
		SourceAgent:    config.SourceAgentToken,
		AgentPublisher: config.AgentPublisherToken,
		AuditRetry:     string(config.RetrySigningKey),
	})
	if err := validateSessionConfiguration(
		config.Session,
		reservedSecrets...,
	); err != nil {
		return kBaseServerConfig{}, nil, err
	}
	if config.RetrySigningErr == nil {
		if err := validateEvidenceAuditRetryKeySeparation(
			config.RetrySigningKey,
			config.AuthToken,
			config.SourceAgentToken,
			config.AgentPublisherToken,
			config.Session.AdminToken,
			config.Session.BrowserProxySecret,
		); err != nil {
			return kBaseServerConfig{}, nil, err
		}
	}
	if config.SourceAgentArtifactRoot != "" {
		if _, err := app.NewSourceAgentArtifactCatalog(config.SourceAgentArtifactRoot); err != nil {
			return kBaseServerConfig{}, nil, errors.New("invalid source agent artifact catalog configuration")
		}
	}
	if config.Research.Enabled {
		modelConfig, err := app.LoadBookTokenPlanConfig()
		if err != nil || strings.TrimSpace(modelConfig.APIKey) == "" {
			return kBaseServerConfig{}, nil, errors.New("research requires DEDAO_TOKENPLAN_API_KEY")
		}
	}
	server, err := newKBaseHTTPServer(config.Addr, nil)
	if err != nil {
		return kBaseServerConfig{}, nil, fmt.Errorf("invalid HTTP server configuration: %w", err)
	}
	return config, server, nil
}

func withBrowserSessionRuntime(
	config sessionServerConfig,
	run func(browserSessionRuntime) error,
) error {
	browserSessions, err := newBrowserSessionRuntime(config)
	if err != nil {
		return fmt.Errorf("initialize browser session store: %w", err)
	}
	defer func() {
		if err := browserSessions.Close(); err != nil {
			log.Printf("close browser session store: %v", err)
		}
	}()
	return run(browserSessions)
}

func serveKBaseServer(
	config kBaseServerConfig,
	server *http.Server,
	browserSessions browserSessionRuntime,
) error {
	sourceSync, err := app.NewSourceSyncStore(config.Root)
	if err != nil {
		return fmt.Errorf("initialize source sync store: %w", err)
	}
	defer func() {
		if err := sourceSync.Close(); err != nil {
			log.Printf("close source sync store: %v", err)
		}
	}()
	bookStore := app.NewBookKnowledgeStore(config.Root)
	if err := initializeBookKnowledgeJobsControlPlane(bookStore); err != nil {
		return fmt.Errorf("initialize book knowledge jobs: %w", err)
	}
	evolutionStore, err := openEvolutionControlStoreForServer(config.EvolutionEnabled, config.Root)
	if err != nil {
		return err
	}
	if evolutionStore != nil {
		defer func() {
			if err := evolutionStore.Close(); err != nil {
				log.Printf("close evolution control store: %v", err)
			}
		}()
	}
	knowledgeCatalog, err := app.NewKnowledgeCatalogStore(config.Root, time.Now)
	if err != nil {
		return fmt.Errorf("initialize knowledge catalog: %w", err)
	}
	defer func() {
		if err := knowledgeCatalog.Close(); err != nil {
			log.Printf("close knowledge catalog: %v", err)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	researchRuntime, err := newResearchServerRuntime(ctx, config.Root, bookStore, config.Research)
	if err != nil {
		return fmt.Errorf("initialize research runtime: %w", err)
	}
	if researchRuntime.Store != nil {
		defer func() {
			shutdownContext, cancel := context.WithTimeout(context.Background(), config.Research.ShutdownTimeout)
			defer cancel()
			if err := researchRuntime.Shutdown(shutdownContext); err != nil {
				log.Printf("research coordinator shutdown failed: %v", err)
			}
			if err := researchRuntime.Close(); err != nil {
				log.Printf("close research store: %v", err)
			}
		}()
	}
	auditRuntime := newEvidenceAuditServerRuntime(ctx, bookStore)
	if auditRuntime.Coordinator != nil {
		defer func() {
			shutdownContext, cancel := context.WithTimeout(
				context.Background(),
				evidenceAuditShutdownTimeout(),
			)
			defer cancel()
			if err := auditRuntime.Coordinator.Shutdown(shutdownContext); err != nil {
				log.Printf("evidence audit coordinator shutdown failed: %v", err)
			}
		}()
	}
	if config.RetrySigningErr != nil {
		log.Printf("evidence audit retry disabled: %v", config.RetrySigningErr)
		config.RetrySigningKey = nil
	}
	proofroomDelivery, proofroomUnavailableReason := newProofroomDeliveryRuntime()
	var sourceArtifacts *app.SourceAgentArtifactCatalog
	if config.SourceAgentArtifactRoot != "" {
		sourceArtifacts, err = app.NewSourceAgentArtifactCatalog(config.SourceAgentArtifactRoot)
		if err != nil {
			return errors.New("initialize source agent artifact catalog failed")
		}
	}

	handlerConfig := app.KBaseHTTPConfig{
		Store:                  bookStore,
		EvolutionStore:         evolutionStore,
		EvolutionEnabled:       config.EvolutionEnabled,
		AuthToken:              config.AuthToken,
		ReleaseRevision:        buildRevision,
		AgentPublisherToken:    config.AgentPublisherToken,
		SystemKBExportPath:     config.ExportPath,
		StaticDir:              config.WebDir,
		WeChat:                 app.NewWeChatSourceService(app.WeChatSourceConfigFromEnv()),
		WCPlus:                 app.NewWCPlusSourceService(app.WCPlusSourceConfigFromEnv()),
		SourceSync:             sourceSync,
		SourceAgentToken:       config.SourceAgentToken,
		ResearchStore:          researchRuntime.Store,
		ResearchQuickBudget:    config.Research.QuickBudget,
		ResearchDeepBudget:     config.Research.DeepBudget,
		SourceArtifacts:        sourceArtifacts,
		ReverificationCooldown: knowledgeReverificationCooldown(),
		AuditCoordinator:       auditRuntime.Coordinator,
		AuditUnavailableReason: auditRuntime.UnavailableReason,
		AuditMaxBodyBytes:      evidenceAuditMaxBodyBytes(),
		AuditRetrySigningKey:   config.RetrySigningKey,
		AuditLogger: func(event app.EvidenceAuditHTTPLogEvent) {
			log.Printf("evidence audit HTTP error: operation=%s code=%s cause=%s",
				event.Operation, event.Code, event.Cause)
		},
		ProofroomDelivery: proofroomDelivery,
	}
	server.Handler = app.NewKBaseHTTPHandler(applyBrowserSessionRuntime(handlerConfig, browserSessions))

	log.Printf("dedao kbase server listening on %s", config.Addr)
	log.Printf("book knowledge root: %s", config.Root)
	log.Printf("system kb export: %s", config.ExportPath)
	if strings.TrimSpace(config.WebDir) != "" {
		log.Printf("web dir: %s", config.WebDir)
	}
	if strings.TrimSpace(config.AuthToken) == "" {
		log.Printf("warning: KBASE_AUTH_TOKEN is empty; /api/* routes will reject requests")
	}
	if strings.TrimSpace(config.SourceAgentToken) == "" {
		log.Printf("source agent API disabled until KBASE_SOURCE_AGENT_TOKEN is configured")
	} else {
		log.Printf("source agent API enabled")
	}
	if sourceArtifacts == nil {
		log.Printf("source agent artifact catalog disabled until KBASE_SOURCE_AGENT_ARTIFACT_ROOT is configured")
	} else {
		log.Printf("source agent artifact catalog enabled")
	}
	if strings.TrimSpace(config.AgentPublisherToken) == "" {
		log.Printf("agent package publisher API disabled until KBASE_AGENT_PUBLISHER_TOKEN is configured")
	} else {
		log.Printf("agent package publisher API enabled with a dedicated token")
	}
	if evolutionStore == nil {
		log.Printf("Agent evolution control plane disabled")
	} else {
		log.Printf("Agent evolution control plane enabled")
	}
	if researchRuntime.Store == nil {
		log.Printf("research runtime disabled")
	} else {
		log.Printf("research runtime enabled: workers=%d queue=%d", config.Research.Workers, config.Research.QueueSize)
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
		return fmt.Errorf("initialize source scheduler: %w", schedulerErr)
	} else {
		_, schedulerDone = startSourceScheduler(
			ctx,
			config.SourceAgentToken,
			sourceSchedulerTickInterval(),
			scheduler,
			log.Printf,
		)
	}
	reverificationRunner := app.NewKnowledgeReverificationRunner(bookStore, nil, time.Now, knowledgeReverificationStaleAfter())
	_, reverificationDone := startKnowledgeReverificationRunner(ctx, knowledgeReverificationTickInterval(), reverificationRunner, log.Printf)
	_, browserSessionCleanupDone := startBrowserSessionCleanup(
		ctx,
		browserSessionCleanupInterval(),
		browserSessionRetention(),
		browserSessions.Store,
		log.Printf,
	)
	defer func() {
		stop()
		if schedulerDone != nil {
			<-schedulerDone
		}
		<-reverificationDone
		<-browserSessionCleanupDone
	}()

	return serveHTTPServer(ctx, stop, server, 10*time.Second)
}

type httpServerLifecycle interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

func serveHTTPServer(
	ctx context.Context,
	stop func(),
	server httpServerLifecycle,
	shutdownTimeout time.Duration,
) error {
	shutdownResult := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownResult <- server.Shutdown(shutdownContext)
	}()

	listenErr := server.ListenAndServe()
	stop()
	shutdownErr := <-shutdownResult
	if shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
		return fmt.Errorf("kbase server shutdown failed: %w", shutdownErr)
	}
	if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
		return listenErr
	}
	return nil
}

type evidenceAuditServerRuntime struct {
	Coordinator       *app.EvidenceAuditCoordinator
	UnavailableReason string
}

type researchServerConfig struct {
	Enabled         bool
	Workers         int
	QueueSize       int
	PollInterval    time.Duration
	LeaseDuration   time.Duration
	ShutdownTimeout time.Duration
	RoleModels      map[app.ResearchModelRole]string
	QuickBudget     app.ResearchBudget
	DeepBudget      app.ResearchBudget
}

type researchServerRuntime struct {
	Store       *app.ResearchStore
	Coordinator *app.ResearchCoordinator
}

func (runtime researchServerRuntime) Shutdown(ctx context.Context) error {
	if runtime.Coordinator == nil {
		return nil
	}
	return runtime.Coordinator.Shutdown(ctx)
}

func (runtime researchServerRuntime) Close() error {
	if runtime.Store == nil {
		return nil
	}
	return runtime.Store.Close()
}

func defaultResearchServerConfig() researchServerConfig {
	return researchServerConfig{
		Workers: 2, QueueSize: 64, PollInterval: time.Second, LeaseDuration: 30 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		RoleModels: map[app.ResearchModelRole]string{
			app.ResearchRolePlanner:     "qwen3.8-max-preview",
			app.ResearchRoleExtractor:   "qwen3.8-max-preview",
			app.ResearchRoleSynthesizer: "qwen3.8-max-preview",
			app.ResearchRoleVerifier:    "qwen3.8-max-preview",
		},
		QuickBudget: app.ResearchBudget{
			MaxIterations: 2, MaxEvidenceItems: 40, MaxQuotedChars: 12000, MaxModelCalls: 4, MaxCostUSD: 1,
		},
		DeepBudget: app.ResearchBudget{
			MaxIterations: 8, MaxEvidenceItems: 300, MaxQuotedChars: 80000, MaxModelCalls: 24, MaxCostUSD: 10,
		},
	}
}

func researchServerConfigFromEnvironment() (researchServerConfig, error) {
	config := defaultResearchServerConfig()
	var err error
	config.Enabled, err = strictBooleanEnvironment("KBASE_RESEARCH_ENABLED", false)
	if err != nil {
		return researchServerConfig{}, err
	}
	if config.Workers, err = strictIntegerEnvironment("KBASE_RESEARCH_WORKERS", config.Workers, 1, 32); err != nil {
		return researchServerConfig{}, err
	}
	if config.QueueSize, err = strictIntegerEnvironment("KBASE_RESEARCH_QUEUE_SIZE", config.QueueSize, 1, 4096); err != nil {
		return researchServerConfig{}, err
	}
	pollMilliseconds, err := strictIntegerEnvironment("KBASE_RESEARCH_POLL_MILLISECONDS", int(config.PollInterval/time.Millisecond), 100, 60000)
	if err != nil {
		return researchServerConfig{}, err
	}
	config.PollInterval = time.Duration(pollMilliseconds) * time.Millisecond
	leaseSeconds, err := strictIntegerEnvironment("KBASE_RESEARCH_LEASE_SECONDS", int(config.LeaseDuration/time.Second), 5, 3600)
	if err != nil {
		return researchServerConfig{}, err
	}
	config.LeaseDuration = time.Duration(leaseSeconds) * time.Second
	shutdownSeconds, err := strictIntegerEnvironment("KBASE_RESEARCH_SHUTDOWN_SECONDS", int(config.ShutdownTimeout/time.Second), 1, 60)
	if err != nil {
		return researchServerConfig{}, err
	}
	config.ShutdownTimeout = time.Duration(shutdownSeconds) * time.Second
	for role, key := range map[app.ResearchModelRole]string{
		app.ResearchRolePlanner:     "KBASE_RESEARCH_PLANNER_MODEL",
		app.ResearchRoleExtractor:   "KBASE_RESEARCH_EXTRACTOR_MODEL",
		app.ResearchRoleSynthesizer: "KBASE_RESEARCH_SYNTHESIZER_MODEL",
		app.ResearchRoleVerifier:    "KBASE_RESEARCH_VERIFIER_MODEL",
	} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		if len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
			return researchServerConfig{}, fmt.Errorf("%s must be a bounded model identifier", key)
		}
		config.RoleModels[role] = value
	}
	if err := loadResearchBudgetEnvironment("KBASE_RESEARCH_QUICK", &config.QuickBudget); err != nil {
		return researchServerConfig{}, err
	}
	if err := loadResearchBudgetEnvironment("KBASE_RESEARCH_DEEP", &config.DeepBudget); err != nil {
		return researchServerConfig{}, err
	}
	return config, nil
}

func loadResearchBudgetEnvironment(prefix string, budget *app.ResearchBudget) error {
	var err error
	if budget.MaxIterations, err = strictIntegerEnvironment(prefix+"_MAX_ITERATIONS", budget.MaxIterations, 1, 20); err != nil {
		return err
	}
	if budget.MaxEvidenceItems, err = strictIntegerEnvironment(prefix+"_MAX_EVIDENCE", budget.MaxEvidenceItems, 1, 1000); err != nil {
		return err
	}
	if budget.MaxQuotedChars, err = strictIntegerEnvironment(prefix+"_MAX_QUOTED_CHARS", budget.MaxQuotedChars, 1, 200000); err != nil {
		return err
	}
	if budget.MaxModelCalls, err = strictIntegerEnvironment(prefix+"_MAX_MODEL_CALLS", budget.MaxModelCalls, 1, 100); err != nil {
		return err
	}
	budget.MaxCostUSD, err = strictFloatEnvironment(prefix+"_MAX_COST_USD", budget.MaxCostUSD, 0.01, 100)
	return err
}

func strictBooleanEnvironment(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", key)
	}
}

func strictFloatEnvironment(key string, fallback, minimum, maximum float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be a number between %.2f and %.2f", key, minimum, maximum)
	}
	return value, nil
}

func newResearchServerRuntime(
	ctx context.Context,
	root string,
	knowledge *app.BookKnowledgeStore,
	config researchServerConfig,
) (researchServerRuntime, error) {
	if !config.Enabled {
		return researchServerRuntime{}, nil
	}
	store, err := app.OpenResearchStore(root, time.Now)
	if err != nil {
		return researchServerRuntime{}, err
	}
	fail := func(err error) (researchServerRuntime, error) {
		_ = store.Close()
		return researchServerRuntime{}, err
	}
	tools, err := app.NewResearchToolRegistry(knowledge, store)
	if err != nil {
		return fail(err)
	}
	modelConfig, err := app.LoadBookTokenPlanConfig()
	if err != nil || strings.TrimSpace(modelConfig.APIKey) == "" {
		return fail(errors.New("research requires DEDAO_TOKENPLAN_API_KEY"))
	}
	baseModel := app.NewResearchStageModel(app.NewTokenPlanChatClient(nil))
	orchestrator, err := app.NewResearchOrchestrator(app.ResearchOrchestratorConfig{
		KnowledgeStore: knowledge, ResearchStore: store, Tools: tools,
		Model: baseModel, ModelConfig: modelConfig, RoleModels: config.RoleModels,
		WorkerAgentID: "chatlog-agent",
	})
	if err != nil {
		return fail(err)
	}
	coordinator, err := app.NewResearchCoordinator(app.ResearchCoordinatorConfig{
		Store: store, Orchestrator: orchestrator, Workers: config.Workers, QueueSize: config.QueueSize,
		PollInterval: config.PollInterval, LeaseDuration: config.LeaseDuration,
	})
	if err != nil {
		return fail(err)
	}
	if err := coordinator.Start(ctx); err != nil {
		return fail(err)
	}
	return researchServerRuntime{Store: store, Coordinator: coordinator}, nil
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

func initializeBookKnowledgeJobsControlPlane(store *app.BookKnowledgeStore) error {
	if store == nil {
		return errors.New("book knowledge store is required")
	}
	_, err := store.ListBookKnowledgeJobs(1)
	return err
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

func defaultSourceAgentArtifactRoot() string {
	return strings.TrimSpace(os.Getenv("KBASE_SOURCE_AGENT_ARTIFACT_ROOT"))
}

func evolutionEnabledFromEnvironment() (bool, error) {
	raw := strings.TrimSpace(os.Getenv("KBASE_EVOLUTION_ENABLED"))
	if raw == "" {
		return true, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, errors.New("KBASE_EVOLUTION_ENABLED must be a boolean")
	}
}

func openEvolutionControlStoreForServer(enabled bool, root string) (*app.EvolutionControlStore, error) {
	if !enabled {
		return nil, nil
	}
	store, err := app.OpenEvolutionControlStore(root, time.Now)
	if err != nil {
		return nil, fmt.Errorf("initialize evolution control store: %w", err)
	}
	return store, nil
}

func defaultBrowserSessionSecret() string {
	return strings.TrimSpace(os.Getenv("KBASE_BROWSER_SESSION_SECRET"))
}

const (
	defaultSessionTTL             = 30 * 24 * time.Hour
	defaultSessionRenewalInterval = 5 * time.Minute
	defaultSessionMaxActive       = 10
)

var browserIDNAProfile = idna.New(
	idna.MapForLookup(),
	idna.Transitional(false),
	idna.StrictDomainName(false),
	idna.CheckHyphens(false),
	idna.VerifyDNSLength(false),
	idna.BidiRule(),
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
		canonicalHost != "localhost" &&
		!strings.HasSuffix(canonicalHost, ".localhost") &&
		(ip == nil || !ip.IsLoopback()) {
		return errors.New("KBASE_PUBLIC_ORIGIN must use HTTPS for a public host")
	}
	return nil
}

// canonicalOriginHost mirrors WHATWG host serialization so configured origins
// exactly match browser Origin headers. See https://url.spec.whatwg.org/#host-serializing.
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
	if strings.Contains(lowerHost, ":") || !isCanonicalDomainHost(lowerHost) {
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

func isCanonicalDomainHost(host string) bool {
	labels := strings.Split(host, ".")
	lastLabel := ""
	for _, label := range labels {
		for _, char := range []byte(label) {
			if isForbiddenDomainCodePoint(char) {
				return false
			}
		}
		if strings.HasPrefix(label, "xn--") && !isCanonicalALabel(label) {
			return false
		}
		if label != "" {
			lastLabel = label
		}
	}
	return lastLabel != "" && !isBrowserNumericLabel(lastLabel)
}

func isForbiddenDomainCodePoint(char byte) bool {
	if char <= 0x20 || char == 0x7f {
		return true
	}
	switch char {
	case '#', '%', '/', ':', '<', '>', '?', '@', '[', '\\', ']', '^', '|':
		return true
	default:
		return false
	}
}

func isCanonicalALabel(label string) bool {
	decoded, err := browserIDNAProfile.ToUnicode(label)
	if err != nil {
		return false
	}
	encoded, err := browserIDNAProfile.ToASCII(decoded)
	return err == nil && encoded == label
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
	if !strings.HasPrefix(label, "0x") {
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

type browserSessionCleaner interface {
	Cleanup(time.Duration) (int64, error)
}

func startBrowserSessionCleanup(
	ctx context.Context,
	interval time.Duration,
	retention time.Duration,
	cleaner browserSessionCleaner,
	logf func(string, ...any),
) (bool, <-chan struct{}) {
	done := make(chan struct{})
	if cleaner == nil {
		close(done)
		return false, done
	}
	if interval <= 0 {
		interval = time.Hour
	}
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	go func() {
		defer close(done)
		runCleanup := func() {
			deleted, err := cleaner.Cleanup(retention)
			if err != nil {
				logf("browser session cleanup failed: %v", err)
				return
			}
			if deleted > 0 {
				logf("browser session cleanup: deleted=%d retention=%s", deleted, retention)
			}
		}
		select {
		case <-ctx.Done():
			return
		default:
			runCleanup()
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runCleanup()
			}
		}
	}()
	return true, done
}

func browserSessionCleanupInterval() time.Duration {
	return boundedBrowserSessionDuration(
		"KBASE_BROWSER_SESSION_CLEANUP_INTERVAL_SECONDS",
		3600,
		300,
		86400,
	)
}

func browserSessionRetention() time.Duration {
	return boundedBrowserSessionDuration(
		"KBASE_BROWSER_SESSION_RETENTION_SECONDS",
		30*24*60*60,
		24*60*60,
		365*24*60*60,
	)
}

func boundedBrowserSessionDuration(key string, fallback, minimum, maximum int) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || seconds < minimum {
		seconds = fallback
	}
	if seconds > maximum {
		seconds = maximum
	}
	return time.Duration(seconds) * time.Second
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
	if err := validateHTTPServerAddress(addr); err != nil {
		return nil, err
	}
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

func validateHTTPServerAddress(addr string) error {
	listenAddr := strings.TrimSpace(addr)
	if listenAddr == "" || listenAddr != addr {
		return errors.New("KBASE_HTTP_ADDR must be an exact host:port listen address")
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return errors.New("KBASE_HTTP_ADDR must be a valid host:port listen address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return errors.New("KBASE_HTTP_ADDR port must be between 0 and 65535")
	}
	if strings.ContainsAny(host, " \t\r\n/") {
		return errors.New("KBASE_HTTP_ADDR host is invalid")
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return errors.New("KBASE_HTTP_ADDR host is invalid")
	}
	return nil
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
