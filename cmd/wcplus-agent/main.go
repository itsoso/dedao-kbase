package main

import (
	"context"
	"encoding/json"
	"errors"
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
	config *app.SourceAgentConfig
	client *app.SourceAgentClient
	wcplus *app.WCPlusSourceService
	outbox *app.SourceAgentOutbox
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
		config: &config,
		client: client,
		wcplus: app.NewWCPlusSourceService(app.WCPlusSourceConfig{
			BaseURL:    config.WCPlusBaseURL,
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
		}),
	}
	if withOutbox {
		runtime.outbox, err = app.NewSourceAgentOutbox(config.StateDir)
		if err != nil {
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

func (r *wcplusAgentRuntime) once(ctx context.Context) (map[string]any, error) {
	status, err := r.wcplus.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("check local WC Plus: %w", err)
	}
	healthy := status != nil && status.OK
	lastError := ""
	if !healthy && status != nil {
		lastError = status.Message
	}
	if _, err := r.client.Heartbeat(ctx, app.SourceAgentHeartbeat{
		Version:       wcplusAgentVersion,
		Capabilities:  wcplusAgentCapabilities,
		WCPlusHealthy: healthy,
		LastError:     lastError,
	}); err != nil {
		return nil, fmt.Errorf("send source-agent heartbeat: %w", err)
	}
	flushed, err := r.flushOutbox(ctx)
	if err != nil {
		return nil, err
	}
	leased := false
	if healthy {
		run, err := r.client.Lease(ctx, wcplusAgentCapabilities, 2*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("lease source sync run: %w", err)
		}
		if run != nil {
			leased = true
			message := "leased run execution is not available until the WC Plus executor is installed"
			_, failErr := r.client.FailRun(ctx, run.ID, message)
			if failErr != nil {
				return nil, fmt.Errorf("%s; report failure: %w", message, failErr)
			}
			return nil, errors.New(message)
		}
	}
	return map[string]any{
		"ok":             true,
		"wcplus_healthy": healthy,
		"outbox_flushed": flushed,
		"leased":         leased,
	}, nil
}

func (r *wcplusAgentRuntime) flushOutbox(ctx context.Context) (int, error) {
	if r.outbox == nil {
		return 0, nil
	}
	items, err := r.outbox.PeekReady(50)
	if err != nil {
		return 0, fmt.Errorf("read source-agent outbox: %w", err)
	}
	flushed := 0
	for _, item := range items {
		if _, err := r.client.UploadArticle(ctx, item.RunID, item.Envelope); err != nil {
			statusCode := 0
			var httpErr *app.SourceAgentHTTPError
			if errors.As(err, &httpErr) {
				statusCode = httpErr.StatusCode
			}
			updated, recordErr := r.outbox.RecordFailure(item.ID, statusCode, err)
			if recordErr != nil {
				return flushed, fmt.Errorf("record outbox delivery failure: %w", recordErr)
			}
			if updated.State == app.SourceOutboxDead {
				return flushed, fmt.Errorf("source outbox item %s moved to dead letter: %w", item.ID, err)
			}
			continue
		}
		if err := r.outbox.Acknowledge(item.ID); err != nil {
			return flushed, fmt.Errorf("acknowledge source outbox item: %w", err)
		}
		flushed++
	}
	return flushed, nil
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
