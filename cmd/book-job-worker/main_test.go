package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

type bookJobWorkerTestEnv map[string]string

func (env bookJobWorkerTestEnv) Lookup(key string) (string, bool) {
	value, ok := env[key]
	return value, ok
}

type fakeBookJobWorkerRuntime struct {
	once func(context.Context) (bool, error)
	run  func(context.Context) error
}

func (runtime fakeBookJobWorkerRuntime) RunOnce(ctx context.Context) (bool, error) {
	return runtime.once(ctx)
}

func (runtime fakeBookJobWorkerRuntime) Run(ctx context.Context) error {
	return runtime.run(ctx)
}

func TestBookJobWorkerCLIBuildInfoDoesNotReadEnvironment(t *testing.T) {
	lookupCalled := false
	var stdout bytes.Buffer
	if err := runBookJobWorkerCLI(context.Background(), []string{"build-info"}, func(string) (string, bool) {
		lookupCalled = true
		return "secret", true
	}, &stdout); err != nil {
		t.Fatal(err)
	}
	if lookupCalled {
		t.Fatal("build-info read environment")
	}
	var info struct {
		SchemaVersion int    `json:"schema_version"`
		Component     string `json:"component"`
		Version       string `json:"version"`
		Revision      string `json:"revision"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.SchemaVersion != 1 || info.Component != "book-job-worker" || info.Version == "" || info.Revision == "" {
		t.Fatalf("build info=%#v", info)
	}
	if strings.Contains(stdout.String(), "/") || strings.Contains(stdout.String(), "secret") {
		t.Fatalf("unsafe build info=%s", stdout.String())
	}
}

func TestBookJobWorkerCLICheckConfigHasNoFilesystemSideEffects(t *testing.T) {
	previousStoreFactory := bookJobWorkerStoreFactory
	defer func() { bookJobWorkerStoreFactory = previousStoreFactory }()
	storeCreated := false
	bookJobWorkerStoreFactory = func(string) *app.BookKnowledgeStore {
		storeCreated = true
		return app.NewBookKnowledgeStore(t.TempDir())
	}
	root := filepath.Join(t.TempDir(), "missing", "book-root")
	var stdout bytes.Buffer
	env := bookJobWorkerTestEnv{
		"KBASE_BOOK_KNOWLEDGE_ROOT":    root,
		"KBASE_BOOK_JOB_WORKER_ID":     "worker-check",
		"KBASE_BOOK_JOB_LEASE_SECONDS": "60",
		"KBASE_BOOK_JOB_RENEW_SECONDS": "20",
		"KBASE_BOOK_JOB_POLL_SECONDS":  "2",
	}
	if err := runBookJobWorkerCLI(context.Background(), []string{"check-config"}, env.Lookup, &stdout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("check-config created root: %v", err)
	}
	if storeCreated {
		t.Fatal("check-config constructed a store")
	}
	if stdout.String() != "{\"schema_version\":1,\"status\":\"ok\"}\n" {
		t.Fatalf("check-config output=%q", stdout.String())
	}
}

func TestBookJobWorkerReadOnlyCommandsDoNotInitializeDesktopConfig(t *testing.T) {
	binPath := filepath.Join(t.TempDir(), "book-job-worker")
	build := exec.Command("go", "build", "-o", binPath, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real worker binary: %v: %s", err, output)
	}
	configRoot := filepath.Join(t.TempDir(), "missing-config")
	bookRoot := filepath.Join(t.TempDir(), "missing-books")
	env := appendEnvironmentWithout(os.Environ(),
		"DEDAO_GO_CONFIG_DIR", "KBASE_BOOK_KNOWLEDGE_ROOT", "KBASE_BOOK_JOB_WORKER_ID",
	)
	env = append(env,
		"DEDAO_GO_CONFIG_DIR="+configRoot,
		"KBASE_BOOK_KNOWLEDGE_ROOT="+bookRoot,
		"KBASE_BOOK_JOB_WORKER_ID=worker-read-only",
	)
	for _, command := range []string{"build-info", "check-config"} {
		run := exec.Command(binPath, command)
		run.Env = env
		if output, err := run.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v: %s", command, err, output)
		}
		if _, err := os.Stat(configRoot); !os.IsNotExist(err) {
			t.Fatalf("%s created desktop config directory: %v", command, err)
		}
		if _, err := os.Stat(bookRoot); !os.IsNotExist(err) {
			t.Fatalf("%s created book root: %v", command, err)
		}
	}
}

func appendEnvironmentWithout(environment []string, keys ...string) []string {
	blocked := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		blocked[key] = struct{}{}
	}
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if _, exists := blocked[key]; !exists {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func TestBookJobWorkerCLIRejectsInvalidEnvironmentWithoutLeakingValues(t *testing.T) {
	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "worker", key: "KBASE_BOOK_JOB_WORKER_ID", value: "../private/worker-token"},
		{name: "token-like worker", key: "KBASE_BOOK_JOB_WORKER_ID", value: "sk-secret-token-value"},
		{name: "lease syntax", key: "KBASE_BOOK_JOB_LEASE_SECONDS", value: "token-secret"},
		{name: "lease range", key: "KBASE_BOOK_JOB_LEASE_SECONDS", value: "999999999"},
		{name: "renew relation", key: "KBASE_BOOK_JOB_RENEW_SECONDS", value: "60"},
		{name: "poll range", key: "KBASE_BOOK_JOB_POLL_SECONDS", value: "999999999"},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := bookJobWorkerTestEnv{
				"KBASE_BOOK_KNOWLEDGE_ROOT":    t.TempDir(),
				"KBASE_BOOK_JOB_WORKER_ID":     "worker-valid",
				"KBASE_BOOK_JOB_LEASE_SECONDS": "60",
				"KBASE_BOOK_JOB_RENEW_SECONDS": "20",
				"KBASE_BOOK_JOB_POLL_SECONDS":  "2",
			}
			env[test.key] = test.value
			var stdout bytes.Buffer
			err := runBookJobWorkerCLI(context.Background(), []string{"check-config"}, env.Lookup, &stdout)
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if strings.Contains(err.Error(), test.value) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("configuration error leaked value: %v", err)
			}
		})
	}
}

func TestBookJobWorkerCLIOnceUsesRootPrecedenceAndSafeGeneratedWorkerID(t *testing.T) {
	previousFactory := bookJobWorkerRuntimeFactory
	defer func() { bookJobWorkerRuntimeFactory = previousFactory }()
	var captured app.BookJobWorkerConfig
	bookJobWorkerRuntimeFactory = func(cfg app.BookJobWorkerConfig) (bookJobWorkerRuntime, error) {
		captured = cfg
		return fakeBookJobWorkerRuntime{
			once: func(context.Context) (bool, error) { return true, nil },
			run:  func(context.Context) error { return nil },
		}, nil
	}
	preferred := filepath.Join(t.TempDir(), "preferred")
	fallback := filepath.Join(t.TempDir(), "fallback")
	env := bookJobWorkerTestEnv{
		"KBASE_BOOK_KNOWLEDGE_ROOT": preferred,
		"DEDAO_BOOK_KNOWLEDGE_ROOT": fallback,
	}
	var stdout bytes.Buffer
	if err := runBookJobWorkerCLI(context.Background(), []string{"once"}, env.Lookup, &stdout); err != nil {
		t.Fatal(err)
	}
	if captured.Store == nil || captured.Store.Root() != preferred {
		t.Fatalf("configured root=%q want=%q", captured.Store.Root(), preferred)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`).MatchString(captured.WorkerID) || strings.Contains(captured.WorkerID, "localhost") {
		t.Fatalf("unsafe generated worker id=%q", captured.WorkerID)
	}
	if captured.LeaseDuration != 60*time.Second || captured.RenewInterval != 20*time.Second || captured.PollInterval != 2*time.Second {
		t.Fatalf("durations lease=%s renew=%s poll=%s", captured.LeaseDuration, captured.RenewInterval, captured.PollInterval)
	}
	if stdout.String() != "{\"processed\":true,\"schema_version\":1}\n" && stdout.String() != "{\"schema_version\":1,\"processed\":true}\n" {
		t.Fatalf("once output=%q", stdout.String())
	}
}

func TestBookJobWorkerCLIConfiguresSharedTokenSourceAgentControl(t *testing.T) {
	previousRuntimeFactory := bookJobWorkerRuntimeFactory
	previousClientFactory := bookJobWorkerSourceAgentClientFactory
	defer func() {
		bookJobWorkerRuntimeFactory = previousRuntimeFactory
		bookJobWorkerSourceAgentClientFactory = previousClientFactory
	}()
	client := &app.SourceAgentClient{}
	var sourceConfig app.SourceAgentConfig
	bookJobWorkerSourceAgentClientFactory = func(cfg app.SourceAgentConfig) (app.BookJobWorkerSourceAgentClient, error) {
		sourceConfig = cfg
		return client, nil
	}
	var workerConfig app.BookJobWorkerConfig
	bookJobWorkerRuntimeFactory = func(cfg app.BookJobWorkerConfig) (bookJobWorkerRuntime, error) {
		workerConfig = cfg
		return fakeBookJobWorkerRuntime{
			once: func(context.Context) (bool, error) { return false, nil },
			run:  func(context.Context) error { return nil },
		}, nil
	}
	env := bookJobWorkerTestEnv{
		"KBASE_BOOK_KNOWLEDGE_ROOT": t.TempDir(), "KBASE_BOOK_JOB_WORKER_ID": "worker-controlled",
		"KBASE_REMOTE_URL": "https://kbase.example.invalid", "KBASE_SOURCE_AGENT_TOKEN": "shared-agent-token",
		"KBASE_SOURCE_AGENT_ID": "book-worker-agent",
	}
	if err := runBookJobWorkerCLI(context.Background(), []string{"once"}, env.Lookup, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if sourceConfig.RemoteURL != "https://kbase.example.invalid" || sourceConfig.AgentToken != "shared-agent-token" || sourceConfig.AgentID != "book-worker-agent" {
		t.Fatalf("source config=%#v", sourceConfig)
	}
	if workerConfig.SourceAgentClient != client || workerConfig.Version != bookJobWorkerVersion || workerConfig.ProtocolVersion != bookJobWorkerProtocolVersion {
		t.Fatalf("worker source control config=%#v", workerConfig)
	}
	for _, partial := range []bookJobWorkerTestEnv{
		{"KBASE_REMOTE_URL": "https://kbase.example.invalid"},
		{"KBASE_SOURCE_AGENT_TOKEN": "shared-agent-token"},
	} {
		partial["KBASE_BOOK_JOB_WORKER_ID"] = "worker-partial"
		if err := runBookJobWorkerCLI(context.Background(), []string{"check-config"}, partial.Lookup, &bytes.Buffer{}); err == nil {
			t.Fatalf("partial source agent configuration accepted: %#v", partial)
		}
	}
}

func TestBookJobWorkerCLIOnceTreatsControlledRestartAsCleanExit(t *testing.T) {
	previousFactory := bookJobWorkerRuntimeFactory
	defer func() { bookJobWorkerRuntimeFactory = previousFactory }()
	bookJobWorkerRuntimeFactory = func(app.BookJobWorkerConfig) (bookJobWorkerRuntime, error) {
		return fakeBookJobWorkerRuntime{
			once: func(context.Context) (bool, error) { return true, app.ErrBookJobWorkerRestartRequested },
			run:  func(context.Context) error { return nil },
		}, nil
	}
	var stdout bytes.Buffer
	if err := runBookJobWorkerCLI(context.Background(), []string{"once"}, bookJobWorkerTestEnv{
		"KBASE_BOOK_JOB_WORKER_ID": "worker-restart-clean",
	}.Lookup, &stdout); err != nil {
		t.Fatalf("controlled restart returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), `"processed":true`) {
		t.Fatalf("controlled restart output=%q", stdout.String())
	}
}

func TestBookJobWorkerCLIRunIsSilentAndCancellationIsGraceful(t *testing.T) {
	previousFactory := bookJobWorkerRuntimeFactory
	defer func() { bookJobWorkerRuntimeFactory = previousFactory }()
	observedCancel := make(chan struct{})
	bookJobWorkerRuntimeFactory = func(app.BookJobWorkerConfig) (bookJobWorkerRuntime, error) {
		return fakeBookJobWorkerRuntime{
			once: func(context.Context) (bool, error) { return false, nil },
			run: func(ctx context.Context) error {
				<-ctx.Done()
				close(observedCancel)
				return nil
			},
		}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	var stdout bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runBookJobWorkerCLI(ctx, []string{"run"}, bookJobWorkerTestEnv{"KBASE_BOOK_JOB_WORKER_ID": "worker-run"}.Lookup, &stdout)
	}()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-observedCancel:
	default:
		t.Fatal("runtime did not observe cancellation")
	}
	if stdout.Len() != 0 {
		t.Fatalf("run output=%q", stdout.String())
	}
}

func TestBookJobWorkerCLIDoesNotHideRuntimeFailureAfterCancellation(t *testing.T) {
	previousFactory := bookJobWorkerRuntimeFactory
	defer func() { bookJobWorkerRuntimeFactory = previousFactory }()
	bookJobWorkerRuntimeFactory = func(app.BookJobWorkerConfig) (bookJobWorkerRuntime, error) {
		return fakeBookJobWorkerRuntime{
			once: func(context.Context) (bool, error) { return false, nil },
			run: func(ctx context.Context) error {
				<-ctx.Done()
				return errors.New("raw interrupt persistence failure /private/token-secret")
			},
		}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runBookJobWorkerCLI(ctx, []string{"run"}, bookJobWorkerTestEnv{"KBASE_BOOK_JOB_WORKER_ID": "worker-run-error"}.Lookup, &bytes.Buffer{})
	}()
	cancel()
	err := <-done
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "token-secret") {
		t.Fatalf("unsafe or hidden runtime error=%v", err)
	}
}

func TestBookJobWorkerCLIOnceDoesNotHideFailureAfterCancellation(t *testing.T) {
	previousFactory := bookJobWorkerRuntimeFactory
	defer func() { bookJobWorkerRuntimeFactory = previousFactory }()
	bookJobWorkerRuntimeFactory = func(app.BookJobWorkerConfig) (bookJobWorkerRuntime, error) {
		return fakeBookJobWorkerRuntime{
			once: func(context.Context) (bool, error) {
				return true, errors.New("raw interrupt persistence failure /private/token-secret")
			},
			run: func(context.Context) error { return nil },
		}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runBookJobWorkerCLI(ctx, []string{"once"}, bookJobWorkerTestEnv{"KBASE_BOOK_JOB_WORKER_ID": "worker-once-error"}.Lookup, &bytes.Buffer{})
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "token-secret") {
		t.Fatalf("unsafe or hidden once error=%v", err)
	}
}

func TestBookJobWorkerSignalContextCancelsThenForcesExit(t *testing.T) {
	signals := make(chan os.Signal, 2)
	forced := make(chan int, 1)
	ctx, stop := newBookJobWorkerSignalContext(context.Background(), signals, func(code int) {
		forced <- code
	})
	defer stop()

	signals <- os.Interrupt
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("first signal did not cancel context")
	}
	select {
	case code := <-forced:
		t.Fatalf("first signal forced exit with code %d", code)
	default:
	}

	signals <- os.Interrupt
	select {
	case code := <-forced:
		if code != 130 {
			t.Fatalf("forced exit code=%d want=130", code)
		}
	case <-time.After(time.Second):
		t.Fatal("second signal did not force exit")
	}
}

func TestBookJobWorkerCLIOnceReturnsSafeInfrastructureError(t *testing.T) {
	previousFactory := bookJobWorkerRuntimeFactory
	defer func() { bookJobWorkerRuntimeFactory = previousFactory }()
	bookJobWorkerRuntimeFactory = func(app.BookJobWorkerConfig) (bookJobWorkerRuntime, error) {
		return fakeBookJobWorkerRuntime{
			once: func(context.Context) (bool, error) { return true, errors.New("raw /private/token-secret") },
			run:  func(context.Context) error { return nil },
		}, nil
	}
	var stdout bytes.Buffer
	err := runBookJobWorkerCLI(context.Background(), []string{"once"}, bookJobWorkerTestEnv{"KBASE_BOOK_JOB_WORKER_ID": "worker-once"}.Lookup, &stdout)
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "token-secret") {
		t.Fatalf("unsafe error=%v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed once output=%q", stdout.String())
	}
}

func TestBookJobWorkerCLIExportLegacyRequiresExactOutAndDoesNotEchoPath(t *testing.T) {
	root := t.TempDir()
	store := app.NewBookKnowledgeStore(root)
	if _, err := store.CreateBookKnowledgeJob(app.BookKnowledgeJobRequest{
		Type: app.BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 301, EbookEnID: "export-worker", DownloadType: 1,
	}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "private-export.json")
	var stdout bytes.Buffer
	env := bookJobWorkerTestEnv{
		"KBASE_BOOK_KNOWLEDGE_ROOT":    root,
		"KBASE_BOOK_JOB_WORKER_ID":     "../unrelated-worker-secret",
		"KBASE_BOOK_JOB_LEASE_SECONDS": "not-needed",
		"KBASE_BOOK_JOB_RENEW_SECONDS": "not-needed",
		"KBASE_BOOK_JOB_POLL_SECONDS":  "not-needed",
	}
	if err := runBookJobWorkerCLI(context.Background(), []string{"export-legacy", "--out", out}, env.Lookup, &stdout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), out) || strings.Contains(stdout.String(), root) || !strings.Contains(stdout.String(), `"exported":true`) {
		t.Fatalf("unsafe export output=%q", stdout.String())
	}
	for _, args := range [][]string{
		{"export-legacy"},
		{"export-legacy", "--out"},
		{"export-legacy", "--out", ""},
		{"export-legacy", "--out", out, "extra"},
		{"export-legacy", "--other", out},
	} {
		if err := runBookJobWorkerCLI(context.Background(), args, env.Lookup, &bytes.Buffer{}); err == nil {
			t.Fatalf("invalid args accepted: %#v", args)
		}
	}
}

func TestBookJobWorkerCLIRejectsUnknownOrExtraArguments(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"unknown"},
		{"build-info", "extra"},
		{"check-config", "extra"},
		{"once", "extra"},
		{"run", "extra"},
	} {
		var stdout bytes.Buffer
		if err := runBookJobWorkerCLI(context.Background(), args, bookJobWorkerTestEnv{}.Lookup, &stdout); err == nil {
			t.Fatalf("invalid args accepted: %#v", args)
		}
	}
}

func TestBookJobWorkerMainPrintsOnlyFixedFailureSummary(t *testing.T) {
	previousFactory := bookJobWorkerRuntimeFactory
	defer func() { bookJobWorkerRuntimeFactory = previousFactory }()
	bookJobWorkerRuntimeFactory = func(app.BookJobWorkerConfig) (bookJobWorkerRuntime, error) {
		return nil, errors.New("raw /private/token-secret")
	}
	var stdout, stderr bytes.Buffer
	exitCode := runBookJobWorkerMain(context.Background(), []string{"once"}, bookJobWorkerTestEnv{"KBASE_BOOK_JOB_WORKER_ID": "worker-main"}.Lookup, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit code=%d want=1", exitCode)
	}
	if stderr.String() != "book-job-worker failed\n" || strings.Contains(stderr.String(), "private") || strings.Contains(stderr.String(), "secret") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}
