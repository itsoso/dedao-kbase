package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
	"github.com/yann0917/dedao-gui/internal/sourceagentupdate"
	"golang.org/x/sys/unix"
)

type sourceAgentLaunchctlCall struct {
	path string
	args []string
}

type fakeSourceAgentLaunchctlRunner struct {
	calls []sourceAgentLaunchctlCall
	err   error
}

func (r *fakeSourceAgentLaunchctlRunner) Run(_ context.Context, path string, args ...string) error {
	r.calls = append(r.calls, sourceAgentLaunchctlCall{path: path, args: append([]string(nil), args...)})
	return r.err
}

func TestSourceAgentUpdaterCLIOnlyAcceptsFixedModesAndWorkerIdentity(t *testing.T) {
	tests := []struct {
		args           []string
		buildInfo      bool
		check          bool
		checkConfig    bool
		checkUninstall bool
		installConfig  bool
		runPending     bool
		holdLifecycle  bool
		workerType     string
	}{
		{args: []string{"--check", "--worker-type", "wechat-worker"}, check: true, workerType: "wechat-worker"},
		{args: []string{"--check-config", "--worker-type", "wechat-worker"}, checkConfig: true, workerType: "wechat-worker"},
		{args: []string{"--check-uninstall", "--worker-type", "wechat-worker"}, checkUninstall: true, workerType: "wechat-worker"},
		{args: []string{"--install-config", "--worker-type", "wcplus-worker"}, installConfig: true, workerType: "wcplus-worker"},
		{args: []string{"--build-info", "--worker-type", "wcplus-worker"}, buildInfo: true, workerType: "wcplus-worker"},
		{args: []string{"--run-pending", "--worker-type", "wcplus-worker"}, runPending: true, workerType: "wcplus-worker"},
		{args: []string{"--hold-lifecycle-lock", "--worker-type", "wcplus-worker"}, holdLifecycle: true, workerType: "wcplus-worker"},
		{args: []string{"--worker-type", "wechat-worker", "--run-pending"}, runPending: true, workerType: "wechat-worker"},
	}
	for _, tt := range tests {
		invocation, err := parseSourceAgentUpdaterArgs(tt.args)
		if err != nil {
			t.Fatalf("args=%#v err=%v", tt.args, err)
		}
		if invocation.BuildInfo != tt.buildInfo || invocation.Check != tt.check || invocation.CheckConfig != tt.checkConfig ||
			invocation.CheckUninstall != tt.checkUninstall ||
			invocation.InstallConfig != tt.installConfig || invocation.RunPending != tt.runPending ||
			invocation.HoldLifecycle != tt.holdLifecycle ||
			invocation.WorkerType != tt.workerType {
			t.Fatalf("args=%#v invocation=%#v", tt.args, invocation)
		}
	}
}

func TestSourceAgentUpdaterUninstallCheckRefusesUnresolvedLocalUpdateState(t *testing.T) {
	previousExecutable := sourceAgentUpdaterExecutable
	defer func() { sourceAgentUpdaterExecutable = previousExecutable }()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "source-agent-updater")
	if err := os.WriteFile(executable, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{".source-agent-staging", ".source-agent-handoff"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sourceAgentUpdaterExecutable = func() (string, error) { return executable, nil }
	if err := checkSourceAgentUninstallSafe(); err != nil {
		t.Fatalf("empty protected state: %v", err)
	}
	blocked := filepath.Join(root, ".source-agent-handoff", "updater.pending")
	if err := os.WriteFile(blocked, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkSourceAgentUninstallSafe(); err == nil {
		t.Fatal("uninstall check accepted unresolved pending state")
	}
}

func TestSourceAgentUpdaterPendingRefusesDurableMaintenanceBeforeLoadingToken(t *testing.T) {
	previousExecutable := sourceAgentUpdaterExecutable
	previousConfigLoader := sourceAgentUpdaterConfigLoader
	previousTokenLoader := sourceAgentUpdaterTokenLoader
	defer func() {
		sourceAgentUpdaterExecutable = previousExecutable
		sourceAgentUpdaterConfigLoader = previousConfigLoader
		sourceAgentUpdaterTokenLoader = previousTokenLoader
	}()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "source-agent-updater")
	if err := os.WriteFile(executable, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".managed-worker-maintenance"), []byte("maintenance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceAgentUpdaterExecutable = func() (string, error) { return executable, nil }
	sourceAgentUpdaterConfigLoader = func(string) (sourceagentupdate.Config, error) {
		t.Fatal("maintenance gate loaded protected config")
		return sourceagentupdate.Config{}, nil
	}
	sourceAgentUpdaterTokenLoader = func(context.Context) (string, error) {
		t.Fatal("maintenance gate loaded shared token")
		return "", nil
	}
	if err := runSourceAgentPendingFromProtectedState(context.Background(), "wechat-worker", nil); !errors.Is(err, errSourceAgentPendingExecutionUnavailable) {
		t.Fatalf("runSourceAgentPendingFromProtectedState() error=%v", err)
	}
}

func TestSourceAgentUpdaterCLIRejectsVariableExecutionInputs(t *testing.T) {
	tests := [][]string{
		{},
		{"--worker-type", "wechat-worker"},
		{"--check"},
		{"--check", "--run-pending", "--worker-type", "wechat-worker"},
		{"--check", "--check-config", "--worker-type", "wechat-worker"},
		{"--check-config", "--install-config", "--worker-type", "wechat-worker"},
		{"--check", "--check", "--worker-type", "wechat-worker"},
		{"--run-pending", "--run-pending", "--worker-type", "wechat-worker"},
		{"--check", "--worker-type", "wechat-worker", "--worker-type", "wcplus-worker"},
		{"--check", "--worker-type", "unknown"},
		{"--check", "--worker-type", "wechat-worker", "extra"},
		{"--check=true", "--worker-type", "wechat-worker"},
		{"--check", "--worker-type=wechat-worker"},
		{"--check", "--worker-type", "wechat-worker", "--label", "other.label"},
		{"--check", "--worker-type", "wechat-worker", "--path", "/tmp/worker"},
		{"--check", "--worker-type", "wechat-worker", "--url", "https://example.invalid"},
		{"--check", "--worker-type", "wechat-worker", "--token", "secret-token"},
		{"--check", "--worker-type", "wechat-worker", "--env", "TOKEN=secret"},
		{"--check", "--worker-type", "wechat-worker", "--command", "echo"},
		{"--check", "--worker-type", "wechat-worker", "--executable", "/tmp/worker"},
		{"--check", "--worker-type", "wechat-worker", "--launchctl", "/tmp/launchctl"},
	}
	for _, args := range tests {
		if _, err := parseSourceAgentUpdaterArgs(args); err == nil {
			t.Fatalf("args %#v should fail", args)
		}
	}
}

func TestSourceAgentUpdaterBuildInfoIsCredentialFreeAndPathFree(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runSourceAgentUpdater([]string{"--build-info", "--worker-type", "wechat-worker"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var info struct {
		WorkerType      string `json:"worker_type"`
		Version         string `json:"version"`
		ProtocolVersion string `json:"protocol_version"`
		Platform        string `json:"platform"`
		Architecture    string `json:"architecture"`
		Revision        string `json:"revision"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.WorkerType != "wechat-worker" || info.Version != sourceAgentUpdaterVersion ||
		info.ProtocolVersion != sourceAgentUpdaterProtocolVersion || info.Platform != runtime.GOOS ||
		info.Architecture != runtime.GOARCH || info.Revision != sourceAgentUpdaterRevision {
		t.Fatalf("build info=%#v", info)
	}
	if stderr.Len() != 0 || strings.Contains(stdout.String(), "/") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestSourceAgentUpdaterInstallAndCheckConfigUseFixedSiblingPath(t *testing.T) {
	previousExecutable := sourceAgentUpdaterExecutable
	previousLoader := sourceAgentUpdaterConfigLoader
	previousSaver := sourceAgentUpdaterConfigSaver
	previousGetenv := sourceAgentUpdaterGetenv
	defer func() {
		sourceAgentUpdaterExecutable = previousExecutable
		sourceAgentUpdaterConfigLoader = previousLoader
		sourceAgentUpdaterConfigSaver = previousSaver
		sourceAgentUpdaterGetenv = previousGetenv
	}()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "source-agent-updater")
	if err := os.WriteFile(executable, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceAgentUpdaterExecutable = func() (string, error) { return executable, nil }
	sourceAgentUpdaterGetenv = func(name string) string {
		switch name {
		case "KBASE_REMOTE_URL":
			return "https://kbase.example.invalid"
		case "KBASE_SOURCE_AGENT_ID":
			return "agent-a"
		default:
			return ""
		}
	}
	wantPath := filepath.Join(root, sourceAgentUpdaterConfigBasename)
	saved := false
	sourceAgentUpdaterConfigSaver = func(path string, config sourceagentupdate.Config) error {
		if path != wantPath || config.Schema != sourceagentupdate.ConfigSchemaV1 ||
			config.KBaseURL != "https://kbase.example.invalid" || config.AgentID != "agent-a" {
			t.Fatalf("path=%q config=%#v", path, config)
		}
		saved = true
		return nil
	}
	sourceAgentUpdaterConfigLoader = func(path string) (sourceagentupdate.Config, error) {
		if path != wantPath {
			t.Fatalf("path=%q", path)
		}
		return sourceagentupdate.Config{Schema: sourceagentupdate.ConfigSchemaV1, KBaseURL: "https://kbase.example.invalid", AgentID: "agent-a"}, nil
	}

	for _, args := range [][]string{
		{"--install-config", "--worker-type", "wechat-worker"},
		{"--check-config", "--worker-type", "wechat-worker"},
	} {
		if err := runSourceAgentUpdater(args, io.Discard, io.Discard); runtime.GOOS == "darwin" && err != nil {
			t.Fatalf("args=%#v err=%v", args, err)
		} else if runtime.GOOS != "darwin" && !errors.Is(err, errSourceAgentUpdaterUnsupportedPlatform) {
			t.Fatalf("args=%#v err=%v", args, err)
		}
	}
	if runtime.GOOS == "darwin" && !saved {
		t.Fatal("protected config was not saved")
	}
}

func TestSourceAgentUpdaterRunPendingUsesInjectedFixedEntrypoint(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin process control")
	}
	var gotWorkerType string
	called := 0
	runPending := func(_ context.Context, workerType string, control sourceAgentProcessControl) error {
		called++
		gotWorkerType = workerType
		if control == nil {
			t.Fatal("process control is nil")
		}
		return nil
	}
	var stdout, stderr bytes.Buffer
	err := runSourceAgentUpdaterWithPendingRunner(
		[]string{"--run-pending", "--worker-type", "wcplus-worker"},
		&stdout,
		&stderr,
		runPending,
	)
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 || gotWorkerType != "wcplus-worker" {
		t.Fatalf("called=%d workerType=%q", called, gotWorkerType)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestSourceAgentUpdaterRunPendingPropagatesShutdownContext(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin process control")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runPending := func(got context.Context, _ string, _ sourceAgentProcessControl) error {
		if !errors.Is(got.Err(), context.Canceled) {
			t.Fatalf("pending context error=%v", got.Err())
		}
		return got.Err()
	}
	err := runSourceAgentUpdaterWithContextAndPendingRunner(
		ctx,
		[]string{"--run-pending", "--worker-type", "wechat-worker"},
		io.Discard,
		io.Discard,
		runPending,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestSourceAgentUpdaterDefaultRunPendingUsesProtectedConfigAndKeychain(t *testing.T) {
	previousExecutable := sourceAgentUpdaterExecutable
	previousConfigLoader := sourceAgentUpdaterConfigLoader
	previousTokenLoader := sourceAgentUpdaterTokenLoader
	previousExecutor := sourceAgentUpdaterPendingExecutor
	defer func() {
		sourceAgentUpdaterExecutable = previousExecutable
		sourceAgentUpdaterConfigLoader = previousConfigLoader
		sourceAgentUpdaterTokenLoader = previousTokenLoader
		sourceAgentUpdaterPendingExecutor = previousExecutor
	}()
	if runtime.GOOS == "darwin" {
		root, resolveErr := filepath.EvalSymlinks(t.TempDir())
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		executable := filepath.Join(root, "source-agent-updater")
		if writeErr := os.WriteFile(executable, []byte("fixture"), 0o755); writeErr != nil {
			t.Fatal(writeErr)
		}
		sourceAgentUpdaterExecutable = func() (string, error) {
			return executable, nil
		}
		sourceAgentUpdaterConfigLoader = func(path string) (sourceagentupdate.Config, error) {
			if path != filepath.Join(root, sourceAgentUpdaterConfigBasename) {
				t.Fatalf("config path=%q", path)
			}
			return sourceagentupdate.Config{
				Schema:   sourceagentupdate.ConfigSchemaV1,
				KBaseURL: "https://kbase.example.invalid",
				AgentID:  "agent-a",
			}, nil
		}
		sourceAgentUpdaterTokenLoader = func(context.Context) (string, error) {
			return "stored-token", nil
		}
		sourceAgentUpdaterPendingExecutor = func(_ context.Context, config app.SourceAgentPendingUpdateRunnerConfig) error {
			if config.UpdaterExecutable != executable ||
				config.WorkerType != "wechat-worker" || config.Guard == nil || config.ProcessControl == nil {
				t.Fatalf("pending config=%#v", config)
			}
			return nil
		}
	}
	var stdout, stderr bytes.Buffer
	err := runSourceAgentUpdater([]string{"--run-pending", "--worker-type", "wechat-worker"}, &stdout, &stderr)
	if runtime.GOOS == "darwin" {
		if err != nil {
			t.Fatalf("err=%v", err)
		}
	} else if !errors.Is(err, errSourceAgentUpdaterUnsupportedPlatform) {
		t.Fatalf("err=%v", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestSourceAgentUpdaterCheckDoesNotPrintLocalPaths(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runSourceAgentUpdater([]string{"--check", "--worker-type", "wechat-worker"}, &stdout, &stderr)
	if runtime.GOOS == "darwin" {
		if err != nil || strings.TrimSpace(stdout.String()) != "source agent updater configuration is valid" {
			t.Fatalf("stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
		}
	} else if !errors.Is(err, errSourceAgentUpdaterUnsupportedPlatform) {
		t.Fatalf("err=%v", err)
	}
	for _, output := range []string{stdout.String(), stderr.String()} {
		if strings.Contains(output, "/Users/") || strings.Contains(output, "/tmp/") || strings.Contains(output, "Library/") {
			t.Fatalf("output leaks a local path: %q", output)
		}
	}
}

func TestSourceAgentUpdaterFixedLaunchAgentLabels(t *testing.T) {
	tests := []struct {
		workerType   string
		workerLabel  string
		updaterLabel string
	}{
		{"wechat-worker", "life.executor.kbase.source-agent", "life.executor.kbase.source-agent.updater"},
		{"wcplus-worker", "life.executor.kbase.wcplus-agent", "life.executor.kbase.wcplus-agent.updater"},
	}
	for _, tt := range tests {
		labels, ok := sourceAgentLaunchAgentLabels(tt.workerType)
		if !ok || labels.Worker != tt.workerLabel || labels.Updater != tt.updaterLabel {
			t.Fatalf("worker=%q labels=%#v ok=%v", tt.workerType, labels, ok)
		}
	}
	if _, ok := sourceAgentLaunchAgentLabels("unknown"); ok {
		t.Fatal("unknown worker should not have labels")
	}
}

func TestSourceAgentUpdaterPlatformControlUsesSeparateFixedLaunchctlOperations(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin process control")
	}
	tests := []struct {
		workerType    string
		workerTarget  string
		updaterTarget string
	}{
		{"wechat-worker", "gui/501/life.executor.kbase.source-agent", "gui/501/life.executor.kbase.source-agent.updater"},
		{"wcplus-worker", "gui/501/life.executor.kbase.wcplus-agent", "gui/501/life.executor.kbase.wcplus-agent.updater"},
	}
	for _, tt := range tests {
		runner := &fakeSourceAgentLaunchctlRunner{}
		control, err := newSourceAgentPlatformProcessControl(sourceAgentPlatformProcessConfig{
			WorkerType: tt.workerType,
			UID:        501,
			Runner:     runner,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := control.StartUpdater(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := control.RestartWorker(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := []sourceAgentLaunchctlCall{
			{path: "/bin/launchctl", args: []string{"kickstart", tt.updaterTarget}},
			{path: "/bin/launchctl", args: []string{"kickstart", "-k", tt.workerTarget}},
		}
		if len(runner.calls) != len(want) {
			t.Fatalf("calls=%#v", runner.calls)
		}
		for i := range want {
			if runner.calls[i].path != want[i].path || strings.Join(runner.calls[i].args, " ") != strings.Join(want[i].args, " ") {
				t.Fatalf("call[%d]=%#v want=%#v", i, runner.calls[i], want[i])
			}
		}
	}
}

func TestSourceAgentUpdaterPlatformControlRejectsUnfixedIdentity(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin validation")
	}
	for _, config := range []sourceAgentPlatformProcessConfig{
		{WorkerType: "unknown", UID: 501},
		{WorkerType: "wechat-worker;echo", UID: 501},
		{WorkerType: "wechat-worker", UID: -1},
	} {
		if _, err := newSourceAgentPlatformProcessControl(config); err == nil {
			t.Fatalf("config %#v should fail", config)
		}
	}
}

func TestSourceAgentUpdaterProcessErrorsDoNotLeakRunnerDetails(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin process control")
	}
	secret := "secret-token-at-/private/tmp/worker"
	runner := &fakeSourceAgentLaunchctlRunner{err: errors.New(secret)}
	control, err := newSourceAgentPlatformProcessControl(sourceAgentPlatformProcessConfig{
		WorkerType: "wechat-worker",
		UID:        501,
		Runner:     runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func(context.Context) error{control.StartUpdater, control.RestartWorker} {
		err := operation(context.Background())
		if err == nil {
			t.Fatal("operation should fail")
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(sourceAgentUpdaterPublicError(err), secret) {
			t.Fatalf("error leaked runner details: %q", err)
		}
	}
}

func TestSourceAgentUpdaterLifecycleHolderProtocol(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin lifecycle holder")
	}
	directory := t.TempDir()
	var output bytes.Buffer
	err := runSourceAgentLifecycleHolder(context.Background(), directory, strings.NewReader("begin-mutation\ncommit\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "locked\nbegun\ncommitted\n"; got != want {
		t.Fatalf("protocol output = %q, want %q", got, want)
	}
	if _, err := os.Lstat(filepath.Join(directory, ".managed-worker-maintenance")); !os.IsNotExist(err) {
		t.Fatalf("maintenance marker remains after commit: %v", err)
	}
}

func TestSourceAgentUpdaterLifecycleHolderLeavesMarkerOnEOF(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin lifecycle holder")
	}
	directory := t.TempDir()
	var output bytes.Buffer
	err := runSourceAgentLifecycleHolder(context.Background(), directory, strings.NewReader("begin-mutation\n"), &output)
	if err == nil {
		t.Fatal("EOF after mutation unexpectedly succeeded")
	}
	data, readErr := os.ReadFile(filepath.Join(directory, ".managed-worker-maintenance"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "begin-mutation\n" {
		t.Fatalf("maintenance marker = %q", data)
	}
}

func TestSourceAgentUpdaterLifecycleHolderCanAbortBeforeMutation(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin lifecycle holder")
	}
	directory := t.TempDir()
	var output bytes.Buffer
	if err := runSourceAgentLifecycleHolder(context.Background(), directory, strings.NewReader("abort-before-mutation\n"), &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "locked\naborted\n"; got != want {
		t.Fatalf("protocol output = %q, want %q", got, want)
	}
}

func TestSourceAgentUpdaterLifecycleHolderPreservesBegunMarker(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin lifecycle holder")
	}
	directory := t.TempDir()
	marker := filepath.Join(directory, ".managed-worker-maintenance")
	if err := os.WriteFile(marker, []byte("begin-mutation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := runSourceAgentLifecycleHolder(context.Background(), directory, strings.NewReader("abort-before-mutation\n"), &output)
	if err == nil {
		t.Fatal("begun marker accepted pre-mutation abort")
	}
	data, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "begin-mutation\n" {
		t.Fatalf("begun marker was changed to %q", data)
	}
}

func TestSourceAgentUpdaterLifecycleHolderSurvivesProtocolProcessCrash(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin lifecycle lock")
	}
	if os.Getenv("SOURCE_AGENT_LIFECYCLE_HELPER") == "1" {
		if err := runSourceAgentLifecycleHolder(context.Background(), os.Getenv("SOURCE_AGENT_LIFECYCLE_DIRECTORY"), os.Stdin, os.Stdout); err != nil {
			os.Exit(91)
		}
		os.Exit(0)
	}
	directory := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=TestSourceAgentUpdaterLifecycleHolderSurvivesProtocolProcessCrash")
	command.Env = append(os.Environ(), "SOURCE_AGENT_LIFECYCLE_HELPER=1", "SOURCE_AGENT_LIFECYCLE_DIRECTORY="+directory)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	if line, err := reader.ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("lock acknowledgement = %q, %v", line, err)
	}
	lock, err := os.OpenFile(filepath.Join(directory, lifecycleLockName), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		lock.Close()
		t.Fatal("second process acquired lifecycle lock")
	}
	lock.Close()
	if _, err := io.WriteString(stdin, "begin-mutation\n"); err != nil {
		t.Fatal(err)
	}
	if line, err := reader.ReadString('\n'); err != nil || line != "begun\n" {
		t.Fatalf("begin acknowledgement = %q, %v", line, err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	time.Sleep(20 * time.Millisecond)
	data, err := os.ReadFile(filepath.Join(directory, lifecycleMaintenanceName))
	if err != nil || string(data) != "begin-mutation\n" {
		t.Fatalf("durable crash marker = %q, %v", data, err)
	}
}
