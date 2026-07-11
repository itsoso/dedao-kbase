package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/yann0917/dedao-gui/backend/app"
	"os"
	"strings"
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
	if args[0] == "doctor" {
		_, err := sessions.Session(ctx)
		return err
	}
	if args[0] == "enroll" {
		return fmt.Errorf("enrollment requires the local loopback server")
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
	adapter, _ := app.NewWeChatSourceAdapter(app.WeChatSourceAdapterConfig{Sessions: sessions})
	mpBase := strings.TrimSpace(os.Getenv("WECHAT_MP_BASE_URL"))
	if mpBase == "" {
		mpBase = "https://mp.weixin.qq.com"
	}
	discovery, err := app.NewWeChatDiscovery(app.WeChatDiscoveryConfig{BaseURL: mpBase, SessionProvider: sessions})
	if err != nil {
		return err
	}
	source := app.NewWeChatSourceService(app.WeChatSourceConfig{SessionProvider: sessions})
	adapter, _ = app.NewWeChatSourceAdapter(app.WeChatSourceAdapterConfig{Sessions: sessions, Discovery: discovery, Source: source})
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
	for {
		if _, err = runner.RunOnce(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		default:
			return nil
		}
	}
}

type storedSessionProvider struct{ store app.SourceSecretStore }

func (p storedSessionProvider) Session(ctx context.Context) (app.WeChatMPSession, error) {
	raw, err := p.store.Load(ctx, "wechat-mp-session")
	if err != nil {
		return app.WeChatMPSession{}, err
	}
	var session app.WeChatMPSession
	if json.Unmarshal(raw, &session) != nil || session.Token == "" {
		return app.WeChatMPSession{}, fmt.Errorf("stored wechat MP session is invalid")
	}
	return session, nil
}
