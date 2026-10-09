package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

func main() {
	configFile := flag.String("config", "", "explicit private JSON map of account keys to feed/export configuration")
	once := flag.Bool("once", false, "process one leased run and exit")
	flag.Parse()
	if *configFile == "" {
		fmt.Fprintln(os.Stderr, "--config is required")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *configFile, *once); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, filename string, once bool) (returnErr error) {
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return fmt.Errorf("source config must be a private regular file under 64 KiB")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("source config unavailable")
	}
	var sources map[string]app.CuratedSourceConfig
	if json.Unmarshal(raw, &sources) != nil {
		return fmt.Errorf("invalid source config")
	}
	adapter, err := app.NewCuratedSourceAdapter(sources)
	if err != nil {
		return err
	}
	cfg := app.SourceAgentConfig{RemoteURL: os.Getenv("KBASE_REMOTE_URL"), AgentToken: os.Getenv("KBASE_SOURCE_AGENT_TOKEN"), AgentID: os.Getenv("KBASE_SOURCE_AGENT_ID"), StateDir: os.Getenv("SOURCE_AGENT_STATE_DIR")}
	client, err := app.NewSourceAgentClient(cfg)
	if err != nil {
		return fmt.Errorf("invalid explicit source agent transport configuration")
	}
	outbox, err := app.NewSourceAgentOutbox(cfg.StateDir)
	if err != nil {
		return fmt.Errorf("source outbox unavailable")
	}
	defer func() { returnErr = errors.Join(returnErr, outbox.Close()) }()
	runner, err := app.NewSourceAgentRunner(app.SourceAgentRunnerConfig{Client: client, Outbox: outbox, Adapter: adapter, WorkerType: "curated-worker", Version: "0.1.0"})
	if err != nil {
		return fmt.Errorf("source runner initialization failed")
	}
	for {
		result, err := runner.RunOnce(ctx)
		if err != nil {
			if once {
				return fmt.Errorf("curated source cycle failed; inspect the authorized source run")
			}
			fmt.Fprintln(os.Stderr, "curated source cycle failed")
		} else {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				return fmt.Errorf("cannot write source run result")
			}
		}
		if once {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(30 * time.Second):
		}
	}
}
