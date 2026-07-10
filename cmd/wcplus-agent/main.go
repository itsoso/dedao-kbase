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
)

const wcplusAgentVersion = "0.1.0"

var wcplusAgentCapabilities = []string{
	"existing_articles",
	"sync_content",
	"sync_links",
	"sync_reading_data",
}

type environmentLookup func(string) (string, bool)

type wcplusAgentRuntime struct {
	client *app.SourceAgentClient
	wcplus *app.WCPlusSourceService
	agent  *app.WCPlusAgent
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
	if len(args) != 1 || (args[0] != "doctor" && args[0] != "once" && args[0] != "run") {
		return fmt.Errorf("usage: wcplus-agent must be doctor, once, or run")
	}
	config, err := loadWCPlusAgentConfig(lookup)
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

func loadWCPlusAgentConfig(lookup environmentLookup) (app.SourceAgentConfig, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	config := app.SourceAgentConfig{
		RemoteURL:     lookupValue(lookup, "KBASE_REMOTE_URL"),
		AgentToken:    lookupValue(lookup, "KBASE_SOURCE_AGENT_TOKEN"),
		AgentID:       lookupValue(lookup, "KBASE_SOURCE_AGENT_ID"),
		StateDir:      lookupValue(lookup, "WCPLUS_AGENT_STATE_DIR"),
		WCPlusBaseURL: lookupValue(lookup, "WCPLUSPRO_BASE_URL"),
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
		runtime.agent, err = app.NewWCPlusAgent(app.WCPlusAgentConfig{
			Client:           client,
			WCPlus:           runtime.wcplus,
			Outbox:           outbox,
			Version:          wcplusAgentVersion,
			Capabilities:     wcplusAgentCapabilities,
			LeaseDuration:    2 * time.Minute,
			TaskPollAttempts: 30,
			TaskPollInterval: 2 * time.Second,
		})
		if err != nil {
			_ = outbox.Close()
			return nil, err
		}
	}
	return runtime, nil
}

func (r *wcplusAgentRuntime) close() {
	if r != nil && r.agent != nil {
		_ = r.agent.Close()
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

func (r *wcplusAgentRuntime) once(ctx context.Context) (app.WCPlusAgentCycleResult, error) {
	if r.agent == nil {
		return app.WCPlusAgentCycleResult{}, fmt.Errorf("WC Plus agent executor is not configured")
	}
	return r.agent.RunOnce(ctx)
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
