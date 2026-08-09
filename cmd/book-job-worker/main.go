package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

const (
	defaultBookJobLeaseSeconds = 60
	defaultBookJobRenewSeconds = 20
	defaultBookJobPollSeconds  = 2
)

var (
	bookJobWorkerVersion  = "development"
	bookJobWorkerRevision = "development"
)

type bookJobWorkerEnvironmentLookup func(string) (string, bool)

type bookJobWorkerRuntime interface {
	RunOnce(context.Context) (bool, error)
	Run(context.Context) error
}

var bookJobWorkerRuntimeFactory = func(cfg app.BookJobWorkerConfig) (bookJobWorkerRuntime, error) {
	return app.NewBookJobWorker(cfg)
}

var bookJobWorkerStoreFactory = app.NewBookKnowledgeStore

type bookJobWorkerParsedConfig struct {
	root          string
	workerID      string
	leaseDuration time.Duration
	renewInterval time.Duration
	pollInterval  time.Duration
}

func main() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	ctx, stopContext := newBookJobWorkerSignalContext(context.Background(), signals, os.Exit)
	code := runBookJobWorkerMain(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr)
	signal.Stop(signals)
	stopContext()
	if code != 0 {
		os.Exit(code)
	}
}

func newBookJobWorkerSignalContext(
	parent context.Context,
	signals <-chan os.Signal,
	forceExit func(int),
) (context.Context, func()) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			close(done)
			cancel()
		})
	}
	go func() {
		seen := 0
		for {
			select {
			case <-done:
				return
			case received, ok := <-signals:
				if !ok {
					return
				}
				seen++
				if seen == 1 {
					cancel()
					continue
				}
				if forceExit != nil {
					code := 1
					if value, ok := received.(syscall.Signal); ok {
						code = 128 + int(value)
					}
					forceExit(code)
				}
				return
			}
		}
	}()
	return ctx, stop
}

func runBookJobWorkerMain(
	ctx context.Context,
	args []string,
	getenv bookJobWorkerEnvironmentLookup,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if err := runBookJobWorkerCLI(ctx, args, getenv, stdout); err != nil {
		if stderr != nil {
			fmt.Fprintln(stderr, "book-job-worker failed")
		}
		return 1
	}
	return 0
}

func runBookJobWorkerCLI(
	ctx context.Context,
	args []string,
	getenv bookJobWorkerEnvironmentLookup,
	stdout io.Writer,
) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if len(args) == 1 && args[0] == "build-info" {
		return json.NewEncoder(stdout).Encode(struct {
			SchemaVersion int    `json:"schema_version"`
			Component     string `json:"component"`
			Version       string `json:"version"`
			Revision      string `json:"revision"`
		}{1, "book-job-worker", bookJobWorkerVersion, bookJobWorkerRevision})
	}
	if len(args) == 0 {
		return bookJobWorkerUsageError()
	}
	if getenv == nil {
		getenv = os.LookupEnv
	}

	switch args[0] {
	case "build-info":
		return errors.New("build-info accepts no arguments")
	case "check-config":
		if len(args) != 1 {
			return errors.New("check-config accepts no arguments")
		}
		_, err := parseBookJobWorkerConfig(getenv)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(struct {
			SchemaVersion int    `json:"schema_version"`
			Status        string `json:"status"`
		}{1, "ok"})
	case "once", "run":
		if len(args) != 1 {
			return fmt.Errorf("%s accepts no arguments", args[0])
		}
		parsed, err := parseBookJobWorkerConfig(getenv)
		if err != nil {
			return err
		}
		cfg := parsed.workerConfig()
		worker, err := bookJobWorkerRuntimeFactory(cfg)
		if err != nil || worker == nil {
			return errors.New("initialize book job worker")
		}
		if args[0] == "run" {
			if err := worker.Run(ctx); err != nil {
				return errors.New("book job worker runtime failed")
			}
			return nil
		}
		processed, err := worker.RunOnce(ctx)
		if err != nil {
			return errors.New("book job worker cycle failed")
		}
		return json.NewEncoder(stdout).Encode(struct {
			Processed     bool `json:"processed"`
			SchemaVersion int  `json:"schema_version"`
		}{processed, 1})
	case "export-legacy":
		if len(args) != 3 || args[1] != "--out" || strings.TrimSpace(args[2]) == "" {
			return errors.New("export-legacy requires --out <path>")
		}
		store := bookJobWorkerStoreFactory(bookJobWorkerRoot(getenv))
		if err := store.ExportLegacyBookKnowledgeJobs(args[2]); err != nil {
			return errors.New("export legacy book jobs failed")
		}
		return json.NewEncoder(stdout).Encode(struct {
			Exported      bool `json:"exported"`
			SchemaVersion int  `json:"schema_version"`
		}{true, 1})
	default:
		return bookJobWorkerUsageError()
	}
}

func parseBookJobWorkerConfig(getenv bookJobWorkerEnvironmentLookup) (bookJobWorkerParsedConfig, error) {
	if getenv == nil {
		getenv = os.LookupEnv
	}
	root := bookJobWorkerRoot(getenv)
	workerID := lookupTrimmedBookJobWorkerEnvironment(getenv, "KBASE_BOOK_JOB_WORKER_ID")
	if workerID == "" {
		var err error
		workerID, err = newBookJobWorkerProcessID()
		if err != nil {
			return bookJobWorkerParsedConfig{}, errors.New("generate book job worker id")
		}
	}
	if err := app.ValidateBookJobWorkerID(workerID); err != nil {
		return bookJobWorkerParsedConfig{}, errors.New("invalid KBASE_BOOK_JOB_WORKER_ID")
	}
	lease, err := bookJobWorkerDuration(getenv, "KBASE_BOOK_JOB_LEASE_SECONDS", defaultBookJobLeaseSeconds, 2, 3600)
	if err != nil {
		return bookJobWorkerParsedConfig{}, err
	}
	renew, err := bookJobWorkerDuration(getenv, "KBASE_BOOK_JOB_RENEW_SECONDS", defaultBookJobRenewSeconds, 1, 3599)
	if err != nil {
		return bookJobWorkerParsedConfig{}, err
	}
	poll, err := bookJobWorkerDuration(getenv, "KBASE_BOOK_JOB_POLL_SECONDS", defaultBookJobPollSeconds, 1, 300)
	if err != nil {
		return bookJobWorkerParsedConfig{}, err
	}
	if renew >= lease {
		return bookJobWorkerParsedConfig{}, errors.New("book job worker renew interval must be shorter than the lease")
	}
	return bookJobWorkerParsedConfig{
		root: root, workerID: workerID, leaseDuration: lease,
		renewInterval: renew, pollInterval: poll,
	}, nil
}

func bookJobWorkerRoot(getenv bookJobWorkerEnvironmentLookup) string {
	root := lookupTrimmedBookJobWorkerEnvironment(getenv, "KBASE_BOOK_KNOWLEDGE_ROOT")
	if root == "" {
		root = lookupTrimmedBookJobWorkerEnvironment(getenv, "DEDAO_BOOK_KNOWLEDGE_ROOT")
	}
	if root == "" {
		root = app.DefaultBookKnowledgeRoot()
	}
	return root
}

func (cfg bookJobWorkerParsedConfig) workerConfig() app.BookJobWorkerConfig {
	return app.BookJobWorkerConfig{
		Store: bookJobWorkerStoreFactory(cfg.root), WorkerID: cfg.workerID,
		LeaseDuration: cfg.leaseDuration, RenewInterval: cfg.renewInterval, PollInterval: cfg.pollInterval,
	}
}

func bookJobWorkerDuration(
	getenv bookJobWorkerEnvironmentLookup,
	key string,
	fallback int,
	minimum int,
	maximum int,
) (time.Duration, error) {
	raw, exists := getenv(key)
	seconds := fallback
	if exists {
		parsed, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || parsed < minimum || parsed > maximum {
			return 0, fmt.Errorf("invalid %s", key)
		}
		seconds = parsed
	}
	return time.Duration(seconds) * time.Second, nil
}

func lookupTrimmedBookJobWorkerEnvironment(getenv bookJobWorkerEnvironmentLookup, key string) string {
	value, _ := getenv(key)
	return strings.TrimSpace(value)
}

func newBookJobWorkerProcessID() (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "worker-" + hex.EncodeToString(random[:]), nil
}

func bookJobWorkerUsageError() error {
	return errors.New("usage: book-job-worker build-info|check-config|once|run|export-legacy --out <path>")
}
