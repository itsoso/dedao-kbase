package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
	"github.com/yann0917/dedao-gui/internal/sourceagentsecret"
)

const wcplusAgentVersion = "0.1.0"

type environmentLookup func(string) (string, bool)

var wcplusTransportTokenLoader = sourceagentsecret.LoadTransportToken

type wcplusAgentRuntime struct {
	client  *app.SourceAgentClient
	wcplus  *app.WCPlusSourceService
	adapter *app.WCPlusSourceAdapter
	runner  *app.SourceAgentRunner
	outbox  *app.SourceAgentOutbox
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runCLI(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, args []string, lookup environmentLookup, stdout, stderr io.Writer) error {
	if len(args) != 1 || (args[0] != "check-config" && args[0] != "doctor" && args[0] != "once" && args[0] != "run") {
		return fmt.Errorf("usage: wcplus-agent must be check-config, doctor, once, or run")
	}
	if args[0] == "check-config" {
		_, err := loadWCPlusAgentConfigOnly(lookup)
		return err
	}
	config, err := loadWCPlusAgentConfig(ctx, lookup)
	if err != nil {
		return err
	}
	runtime, err := newWCPlusAgentRuntime(config, args[0] != "doctor")
	if err != nil {
		return err
	}
	defer runtime.close()

	switch args[0] {
	case "doctor":
		return runtime.doctor(ctx, stdout)
	case "once":
		result, err := runtime.once(ctx)
		if err != nil {
			return err
		}
		return writeCLIJSON(stdout, result)
	case "run":
		interval := wcplusAgentPollInterval(lookup)
		for {
			result, err := runtime.once(ctx)
			if err != nil {
				fmt.Fprintf(stderr, "wcplus-agent cycle failed: %v\n", err)
			} else if err := writeCLIJSON(stdout, result); err != nil {
				return err
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
	}
	return nil
}

func loadWCPlusAgentConfig(ctx context.Context, lookup environmentLookup) (app.SourceAgentConfig, error) {
	return loadWCPlusAgentConfigWithTransportToken(ctx, lookup, wcplusTransportTokenLoader)
}

func loadWCPlusAgentConfigWithTransportToken(ctx context.Context, lookup environmentLookup, loader sourceagentsecret.Loader) (app.SourceAgentConfig, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	normalized, err := loadWCPlusAgentConfigOnly(lookup)
	if err != nil {
		return app.SourceAgentConfig{}, err
	}
	rawToken, provided := lookup("KBASE_SOURCE_AGENT_TOKEN")
	token, err := sourceagentsecret.ResolveTransportToken(ctx, rawToken, provided, loader)
	if err != nil {
		return app.SourceAgentConfig{}, err
	}
	normalized.AgentToken = token
	return normalized, nil
}

func loadWCPlusAgentConfigOnly(lookup environmentLookup) (app.SourceAgentConfig, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	config := app.SourceAgentConfig{
		RemoteURL:     lookupValue(lookup, "KBASE_REMOTE_URL"),
		AgentToken:    "pending-transport-token",
		AgentID:       lookupValue(lookup, "KBASE_SOURCE_AGENT_ID"),
		StateDir:      lookupValue(lookup, "WCPLUS_AGENT_STATE_DIR"),
		WCPlusBaseURL: lookupValue(lookup, "WCPLUSPRO_BASE_URL"),
	}
	if config.StateDir == "" {
		return app.SourceAgentConfig{}, fmt.Errorf("WCPLUS_AGENT_STATE_DIR is required")
	}
	if config.WCPlusBaseURL == "" {
		config.WCPlusBaseURL = lookupValue(lookup, "WCPLUS_BASE_URL")
	}
	normalized, err := config.Normalized()
	if err != nil {
		return app.SourceAgentConfig{}, err
	}
	return normalized, nil
}

func newWCPlusAgentRuntime(config app.SourceAgentConfig, withOutbox bool) (*wcplusAgentRuntime, error) {
	client, err := app.NewSourceAgentClient(config)
	if err != nil {
		return nil, err
	}
	runtime := &wcplusAgentRuntime{
		client: client,
		wcplus: app.NewWCPlusSourceService(app.WCPlusSourceConfig{
			BaseURL:    config.WCPlusBaseURL,
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
		}),
	}
	if withOutbox {
		outbox, outboxErr := app.NewSourceAgentOutbox(config.StateDir)
		if outboxErr != nil {
			return nil, outboxErr
		}
		runtime.adapter, err = app.NewWCPlusSourceAdapter(app.WCPlusSourceAdapterConfig{
			WCPlus:           runtime.wcplus,
			TaskPollAttempts: 30,
			TaskPollInterval: 2 * time.Second,
		})
		if err != nil {
			_ = outbox.Close()
			return nil, err
		}
		runtime.outbox = outbox
		runtime.runner, err = app.NewSourceAgentRunner(app.SourceAgentRunnerConfig{
			Client: client, Outbox: outbox, Adapter: runtime.adapter, Diagnoser: runtime.adapter,
			Updater: &wcplusAgentFailClosedUpdater{}, WorkerType: "wcplus-worker",
			Version: wcplusAgentVersion, ProtocolVersion: "2026-08-01", LeaseDuration: 2 * time.Minute,
		})
		if err != nil {
			_ = outbox.Close()
			return nil, err
		}
	}
	return runtime, nil
}

func (r *wcplusAgentRuntime) close() {
	if r != nil && r.outbox != nil {
		_ = r.outbox.Close()
	}
}

func (r *wcplusAgentRuntime) doctor(ctx context.Context, output io.Writer) error {
	status, err := r.wcplus.Status(ctx)
	if err != nil {
		return fmt.Errorf("check local WC Plus: %w", err)
	}
	if status == nil || !status.OK {
		message := "unavailable"
		if status != nil && strings.TrimSpace(status.Message) != "" {
			message = status.Message
		}
		return fmt.Errorf("local WC Plus is unavailable: %s", message)
	}
	if err := r.client.CheckAuth(ctx); err != nil {
		return fmt.Errorf("check remote source-agent authentication: %w", err)
	}
	return writeCLIJSON(output, map[string]any{
		"ok":            true,
		"wcplus":        true,
		"remote_auth":   true,
		"agent_version": wcplusAgentVersion,
	})
}

func (r *wcplusAgentRuntime) once(ctx context.Context) (app.SourceAgentCycleResult, error) {
	if r.runner == nil {
		return app.SourceAgentCycleResult{}, fmt.Errorf("WC Plus agent executor is not configured")
	}
	return r.runner.RunOnce(ctx)
}

// Do not infer trusted artifact metadata from a remote command. Until the fixed
// local updater handoff is wired, upgrades fail closed.
type wcplusAgentFailClosedUpdater struct{}

func (*wcplusAgentFailClosedUpdater) Upgrade(context.Context, app.SourceAgentCommand) app.SourceAgentUpgradeResult {
	return app.SourceAgentUpgradeResult{
		State: app.SourceAgentCommandFailed, Code: app.SourceAgentCommandCodeUpgradeFailed,
		Message: "The local updater handoff is unavailable.",
	}
}

func lookupValue(lookup environmentLookup, key string) string {
	value, _ := lookup(key)
	return strings.TrimSpace(value)
}

func wcplusAgentPollInterval(lookup environmentLookup) time.Duration {
	seconds, err := strconv.Atoi(lookupValue(lookup, "WCPLUS_AGENT_POLL_SECONDS"))
	if err != nil || seconds <= 0 {
		seconds = 15
	}
	if seconds < 1 {
		seconds = 1
	}
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func writeCLIJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
