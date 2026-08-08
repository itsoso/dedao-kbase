package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
	"github.com/yann0917/dedao-gui/internal/sourceagentsecret"
)

type sourceEnvironmentLookup func(string) (string, bool)

const (
	sourceAgentWorkerType      = "wechat-worker"
	sourceAgentVersion         = "0.2.1"
	sourceAgentProtocolVersion = "2026-08-01"
)

var sourceAgentTransportTokenLoader = sourceagentsecret.LoadTransportToken
var sourceAgentLegacyTransportTokenLoader = loadLegacySourceTransportToken
var sourceAgentRevision = "0000000000000000000000000000000000000000"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runSourceAgentCLI(ctx, os.Args[1:], os.LookupEnv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func loadSourceAgentConfig(ctx context.Context, lookup sourceEnvironmentLookup) (app.SourceAgentConfig, error) {
	return loadSourceAgentConfigWithTransportTokenFallback(ctx, lookup, sourceAgentTransportTokenLoader, sourceAgentLegacyTransportTokenLoader)
}

func loadSourceAgentConfigWithTransportToken(ctx context.Context, lookup sourceEnvironmentLookup, loader sourceagentsecret.Loader) (app.SourceAgentConfig, error) {
	return loadSourceAgentConfigWithTransportTokenFallback(ctx, lookup, loader, nil)
}

func loadSourceAgentConfigWithTransportTokenFallback(ctx context.Context, lookup sourceEnvironmentLookup, loader sourceagentsecret.Loader, legacy sourceagentsecret.AgentLoader) (app.SourceAgentConfig, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	cfg, err := loadSourceAgentConfigOnly(lookup)
	if err != nil {
		return app.SourceAgentConfig{}, err
	}
	rawToken, provided := lookup("KBASE_SOURCE_AGENT_TOKEN")
	token, err := sourceagentsecret.ResolveSourceTransportToken(ctx, rawToken, provided, cfg.AgentID, loader, legacy)
	if err != nil {
		return app.SourceAgentConfig{}, err
	}
	cfg.AgentToken = token
	return cfg, nil
}

func loadSourceAgentConfigOnly(lookup sourceEnvironmentLookup) (app.SourceAgentConfig, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value := func(key string) string { v, _ := lookup(key); return strings.TrimSpace(v) }
	cfg, err := (app.SourceAgentConfig{RemoteURL: value("KBASE_REMOTE_URL"), AgentToken: "pending-transport-token", AgentID: value("KBASE_SOURCE_AGENT_ID"), StateDir: value("SOURCE_AGENT_STATE_DIR")}).Normalized()
	if err != nil {
		return app.SourceAgentConfig{}, err
	}
	return cfg, nil
}

func loadLegacySourceTransportToken(ctx context.Context, agentID string) (string, error) {
	value, err := newKeychainSecretStore(agentID, nil).Load(ctx, "transport-token")
	if err != nil {
		return "", err
	}
	return string(value), nil
}
func runSourceAgentCLI(ctx context.Context, args []string, lookup sourceEnvironmentLookup) (returnErr error) {
	if len(args) != 1 {
		return fmt.Errorf("usage: source-agent build-info, check-config, doctor, once, run, or enroll")
	}
	if args[0] == "build-info" {
		return writeSourceAgentBuildInfo(os.Stdout)
	}
	if args[0] == "check-config" {
		if _, err := loadSourceAgentConfigOnly(lookup); err != nil {
			return err
		}
		value := ""
		if lookup != nil {
			value, _ = lookup("SOURCE_AGENT_ENROLL_ADDR")
		}
		_, err := normalizeEnrollmentAddress(value)
		return err
	}
	cfg, err := loadSourceAgentConfig(ctx, lookup)
	if err != nil {
		return err
	}
	store := newKeychainSecretStore(cfg.AgentID, nil)
	sessions := storedSessionProvider{store: store}
	if args[0] == "enroll" {
		return runEnrollmentServer(ctx, lookup, store)
	}
	client, err := app.NewSourceAgentClient(cfg)
	if err != nil {
		return err
	}
	outbox, err := app.NewSourceAgentOutbox(cfg.StateDir)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, outbox.Close()) }()
	if args[0] == "doctor" {
		report, doctorErr := inspectSourceAgent(ctx, client, sessions)
		if doctorErr != nil {
			return doctorErr
		}
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	mpBase, _ := lookup("WECHAT_MP_BASE_URL")
	mpBase = strings.TrimSpace(mpBase)
	if mpBase == "" {
		mpBase = "https://mp.weixin.qq.com"
	}
	discovery, err := app.NewWeChatDiscovery(app.WeChatDiscoveryConfig{BaseURL: mpBase, SessionProvider: sessions})
	if err != nil {
		return err
	}
	source := app.NewWeChatSourceService(app.WeChatSourceConfig{SessionProvider: sessions})
	adapter, err := app.NewWeChatSourceAdapter(app.WeChatSourceAdapterConfig{
		Sessions:  sessions,
		Discovery: discovery,
		Source:    source,
		Media:     app.NewWeChatMediaDownloader(app.WeChatMediaConfig{}),
		Assets:    client,
	})
	if err != nil {
		return err
	}
	upgradeBridge, err := newWeChatProductionUpgradeBridge(client)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, upgradeBridge.Close()) }()
	runner, err := newWeChatSourceAgentRunner(client, outbox, adapter, upgradeBridge, upgradeBridge)
	if err != nil {
		return err
	}
	if args[0] == "once" {
		_, err = runner.RunOnce(ctx)
		return err
	}
	if args[0] != "run" {
		return fmt.Errorf("unknown source-agent command")
	}
	return runSourceAgentRuntime(ctx, runner, 15*time.Second, func(runtimeCtx context.Context) error {
		return runEnrollmentServer(runtimeCtx, lookup, store)
	}, func(cycleErr error) {
		fmt.Fprintf(os.Stderr, "source-agent cycle failed: %v\n", cycleErr)
	})
}

func writeSourceAgentBuildInfo(output io.Writer) error {
	if output == nil || !validSourceAgentCompiledRevision(sourceAgentRevision) {
		return errors.New("source-agent build identity is invalid")
	}
	return json.NewEncoder(output).Encode(map[string]string{
		"worker_type": sourceAgentWorkerType, "version": sourceAgentVersion,
		"protocol_version": sourceAgentProtocolVersion, "platform": runtime.GOOS,
		"architecture": runtime.GOARCH, "revision": sourceAgentRevision,
	})
}

func validSourceAgentCompiledRevision(value string) bool {
	if (len(value) != 40 && len(value) != 64) || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func newWeChatSourceAgentRunner(
	client *app.SourceAgentClient,
	outbox *app.SourceAgentOutbox,
	adapter *app.WeChatSourceAdapter,
	updater app.SourceAgentUpdater,
	upgradeState app.SourceAgentProtectedUpgradeState,
) (*app.SourceAgentRunner, error) {
	revision := ""
	if upgradeState != nil {
		revision = sourceAgentRevision
	}
	return app.NewSourceAgentRunner(app.SourceAgentRunnerConfig{
		Client: client, Outbox: outbox, Adapter: adapter, Diagnoser: adapter,
		Updater: updater, UpgradeState: upgradeState, WorkerType: sourceAgentWorkerType,
		Version: sourceAgentVersion, ProtocolVersion: sourceAgentProtocolVersion,
		Revision: revision,
	})
}

func newWeChatProductionUpgradeBridge(client *app.SourceAgentClient) (*app.SourceAgentUpdateBridge, error) {
	workerExecutable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve source-agent executable: %w", err)
	}
	workerExecutable, err = filepath.EvalSymlinks(filepath.Clean(workerExecutable))
	if err != nil || filepath.Base(workerExecutable) != "source-agent" {
		return nil, fmt.Errorf("source-agent must run from its fixed installed executable")
	}
	activator, err := app.NewSourceAgentUpdaterActivator(sourceAgentWorkerType, os.Getuid(), nil)
	if err != nil {
		return nil, err
	}
	return newWeChatWorkerUpgradeBridge(client, workerExecutable, activator)
}

func newWeChatWorkerUpgradeBridge(
	client *app.SourceAgentClient,
	workerExecutable string,
	activator app.SourceAgentUpdaterActivator,
) (*app.SourceAgentUpdateBridge, error) {
	if client == nil || filepath.Base(workerExecutable) != "source-agent" {
		return nil, fmt.Errorf("invalid fixed source-agent upgrade runtime")
	}
	return app.NewSourceAgentUpdateBridge(app.SourceAgentUpdateBridgeConfig{
		Downloader: client, UpdaterExecutable: filepath.Join(filepath.Dir(workerExecutable), "source-agent-updater"),
		WorkerType: sourceAgentWorkerType, CurrentVersion: sourceAgentVersion,
		Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		ProtocolVersion: sourceAgentProtocolVersion, Revision: sourceAgentRevision,
		Activator: activator,
	})
}

// Retained only for narrow tests that intentionally omit an installed updater.
type sourceAgentFailClosedUpdater struct{}

func (*sourceAgentFailClosedUpdater) Upgrade(context.Context, app.SourceAgentCommand) app.SourceAgentUpgradeResult {
	return app.SourceAgentUpgradeResult{
		State: app.SourceAgentCommandFailed, Code: app.SourceAgentCommandCodeUpgradeFailed,
		Message: "The local updater handoff is unavailable.",
	}
}

type sourceAgentCycleRunner interface {
	RunOnce(context.Context) (app.SourceAgentCycleResult, error)
}

type sourceAgentAuthChecker interface {
	CheckAuth(context.Context) error
}

type sourceAgentDoctorReport struct {
	RemoteAuth    bool   `json:"remote_auth"`
	StateReady    bool   `json:"state_ready"`
	WeChatSession string `json:"wechat_session"`
}

func inspectSourceAgent(ctx context.Context, auth sourceAgentAuthChecker, sessions app.WeChatMPSessionProvider) (sourceAgentDoctorReport, error) {
	report := sourceAgentDoctorReport{StateReady: true, WeChatSession: "login_required"}
	if auth == nil || sessions == nil {
		return report, fmt.Errorf("source-agent doctor dependencies are required")
	}
	if err := auth.CheckAuth(ctx); err != nil {
		return report, fmt.Errorf("source-agent remote authentication failed: %w", err)
	}
	report.RemoteAuth = true
	session, err := sessions.Session(ctx)
	if err == nil && session.Validate(time.Now()) == nil {
		report.WeChatSession = "ready"
	}
	return report, nil
}

func runSourceAgentLoop(ctx context.Context, runner sourceAgentCycleRunner, interval time.Duration, report func(error)) error {
	if runner == nil {
		return fmt.Errorf("source-agent cycle runner is required")
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}
	for {
		if _, cycleErr := runner.RunOnce(ctx); cycleErr != nil && report != nil {
			report(cycleErr)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil
		case <-timer.C:
		}
	}
}

func runSourceAgentRuntime(ctx context.Context, runner sourceAgentCycleRunner, interval time.Duration, enrollment func(context.Context) error, report func(error)) error {
	if enrollment == nil {
		return fmt.Errorf("source-agent enrollment runtime is required")
	}
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errors := make(chan error, 2)
	go func() { errors <- enrollment(runtimeCtx) }()
	go func() { errors <- runSourceAgentLoop(runtimeCtx, runner, interval, report) }()
	select {
	case <-ctx.Done():
		return nil
	case runtimeErr := <-errors:
		if runtimeErr == nil {
			return fmt.Errorf("source-agent runtime component stopped unexpectedly")
		}
		return runtimeErr
	}
}

type enrollmentClientAdapter struct{ client *app.WeChatMPSessionClient }

func (a enrollmentClientAdapter) StartLogin(ctx context.Context) error {
	return a.client.StartLogin(ctx)
}
func (a enrollmentClientAdapter) QRImage(ctx context.Context) ([]byte, string, error) {
	data, err := a.client.QRImage(ctx)
	return data, "image/png", err
}
func (a enrollmentClientAdapter) LoginStatus(ctx context.Context) (any, error) {
	return a.client.PollLogin(ctx)
}
func (a enrollmentClientAdapter) Logout(ctx context.Context) error { return a.client.Logout(ctx) }

type enrollmentDiscoveryAdapter struct {
	search  *app.WeChatSourceService
	history *app.WeChatDiscovery
}

func (a enrollmentDiscoveryAdapter) SearchOfficialAccounts(ctx context.Context, query string) ([]app.WeChatOfficialAccount, error) {
	return a.search.SearchOfficialAccounts(ctx, query)
}

func (a enrollmentDiscoveryAdapter) ListOfficialAccountArticles(ctx context.Context, fakeID string, begin, count int) ([]app.WeChatOfficialArticle, error) {
	page, err := a.history.Discover(ctx, fakeID, app.WeChatDiscoveryCursor{Begin: begin}, count, "")
	if err != nil {
		return nil, err
	}
	articles := make([]app.WeChatOfficialArticle, 0, len(page.Articles))
	for _, item := range page.Articles {
		articles = append(articles, item.WeChatOfficialArticle)
	}
	return articles, nil
}

func normalizeEnrollmentAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "127.0.0.1:8765"
	}
	host, portValue, err := net.SplitHostPort(value)
	if err != nil || !isExactEnrollmentLoopbackHost(host) || !decimalPort(portValue) {
		return "", fmt.Errorf("SOURCE_AGENT_ENROLL_ADDR must bind loopback")
	}
	return value, nil
}

func isExactEnrollmentLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func decimalPort(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	port, err := strconv.Atoi(value)
	return err == nil && port >= 1 && port <= 65535
}
func runEnrollmentServer(ctx context.Context, lookup sourceEnvironmentLookup, store app.SourceSecretStore) error {
	value := func(key string) string { v, _ := lookup(key); return strings.TrimSpace(v) }
	address, err := normalizeEnrollmentAddress(value("SOURCE_AGENT_ENROLL_ADDR"))
	if err != nil {
		return err
	}
	base := value("WECHAT_MP_BASE_URL")
	if base == "" {
		base = "https://mp.weixin.qq.com"
	}
	client, err := app.NewWeChatMPSessionClient(app.WeChatMPSessionConfig{BaseURL: base, SecretStore: store, SecretKey: "wechat-mp-session"})
	if err != nil {
		return err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return fmt.Errorf("generate enrollment CSRF secret failed")
	}
	sessions := storedSessionProvider{store: store}
	search := app.NewWeChatSourceService(app.WeChatSourceConfig{MPBaseURL: base, SessionProvider: sessions})
	history, err := app.NewWeChatDiscovery(app.WeChatDiscoveryConfig{BaseURL: base, SessionProvider: sessions})
	if err != nil {
		return err
	}
	discovery := enrollmentDiscoveryAdapter{search: search, history: history}
	handler, err := newEnrollmentHandler(enrollmentClientAdapter{client: client}, discovery, enrollmentHandlerConfig{
		CSRFToken: hex.EncodeToString(secret),
		RemoteURL: value("KBASE_REMOTE_URL"),
		AgentID:   value("KBASE_SOURCE_AGENT_ID"),
		ReportError: func(stage string, reportErr error) {
			log.Printf("source-agent enrollment %s failed: %v", stage, reportErr)
		},
	})
	if err != nil {
		return err
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("start enrollment listener: %w", err)
	}
	fmt.Printf("source-agent enrollment: http://%s\n", listener.Addr().String())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case err := <-done:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

type storedSessionProvider struct{ store app.SourceSecretStore }

func (p storedSessionProvider) Session(ctx context.Context) (app.WeChatMPSession, error) {
	raw, err := p.store.Load(ctx, "wechat-mp-session")
	if err != nil {
		return app.WeChatMPSession{}, err
	}
	var session app.WeChatMPSession
	if json.Unmarshal(raw, &session) != nil {
		return app.WeChatMPSession{}, fmt.Errorf("stored wechat MP session is invalid")
	}
	if err := session.Validate(time.Now()); err != nil {
		return app.WeChatMPSession{}, err
	}
	return session, nil
}
