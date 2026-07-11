package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

type sourceEnvironmentLookup func(string) (string, bool)

func main() {
	if err := runSourceAgentCLI(context.Background(), os.Args[1:], os.LookupEnv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func loadSourceAgentConfig(lookup sourceEnvironmentLookup) (app.SourceAgentConfig, error) {
	value := func(key string) string { v, _ := lookup(key); return strings.TrimSpace(v) }
	cfg := app.SourceAgentConfig{RemoteURL: value("KBASE_REMOTE_URL"), AgentToken: value("KBASE_SOURCE_AGENT_TOKEN"), AgentID: value("KBASE_SOURCE_AGENT_ID"), StateDir: value("SOURCE_AGENT_STATE_DIR")}
	if cfg.AgentToken == "" && cfg.AgentID != "" {
		if raw, err := newKeychainSecretStore(cfg.AgentID, nil).Load(context.Background(), "transport-token"); err == nil {
			cfg.AgentToken = string(raw)
		}
	}
	return cfg.Normalized()
}
func runSourceAgentCLI(ctx context.Context, args []string, lookup sourceEnvironmentLookup) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: source-agent doctor, once, run, or enroll")
	}
	cfg, err := loadSourceAgentConfig(lookup)
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
	defer outbox.Close()
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
	runner, err := app.NewSourceAgentRunner(app.SourceAgentRunnerConfig{Client: client, Outbox: outbox, Adapter: adapter, Version: "0.1.0"})
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

func normalizeEnrollmentAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "127.0.0.1:8765"
	}
	host, _, err := net.SplitHostPort(value)
	if err != nil || !isLoopbackHost(host) {
		return "", fmt.Errorf("SOURCE_AGENT_ENROLL_ADDR must bind loopback")
	}
	return value, nil
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
	discovery := app.NewWeChatSourceService(app.WeChatSourceConfig{MPBaseURL: base, SessionProvider: sessions})
	handler, err := newEnrollmentHandler(enrollmentClientAdapter{client: client}, discovery, enrollmentHandlerConfig{
		CSRFToken: hex.EncodeToString(secret),
		RemoteURL: value("KBASE_REMOTE_URL"),
		AgentID:   value("KBASE_SOURCE_AGENT_ID"),
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
