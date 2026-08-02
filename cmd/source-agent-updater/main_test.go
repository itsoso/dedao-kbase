package main

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

type fakeSourceAgentLaunchctlRunner struct {
	path string
	args []string
}

func (r *fakeSourceAgentLaunchctlRunner) Run(_ context.Context, path string, args ...string) error {
	r.path = path
	r.args = append([]string(nil), args...)
	return nil
}

func TestSourceAgentUpdaterCLIOnlyAcceptsInstallerCheckIdentity(t *testing.T) {
	invocation, err := parseSourceAgentUpdaterArgs([]string{"--check", "--worker-type", "wechat-worker"})
	if err != nil || !invocation.Check || invocation.WorkerType != "wechat-worker" {
		t.Fatalf("invocation=%#v err=%v", invocation, err)
	}
	for _, args := range [][]string{
		{},
		{"--worker-type", "wechat-worker"},
		{"--check", "--worker-type", "unknown"},
		{"--check", "--worker-type", "wechat-worker", "--path", "/tmp/worker"},
		{"--check", "--worker-type", "wechat-worker", "--url", "https://example.invalid"},
		{"--check", "--worker-type", "wechat-worker", "--command", "echo"},
		{"--check", "--worker-type", "wechat-worker", "--label", "other.label"},
	} {
		if _, err := parseSourceAgentUpdaterArgs(args); err == nil {
			t.Fatalf("args %#v should fail", args)
		}
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

func TestSourceAgentUpdaterPlatformControlUsesFixedLaunchctlArguments(t *testing.T) {
	runner := &fakeSourceAgentLaunchctlRunner{}
	control, err := newSourceAgentPlatformProcessControl(sourceAgentPlatformProcessConfig{
		WorkerType: "wechat-worker", Label: "life.executor.kbase.source-agent",
		Domain: "gui", UID: 501, Runner: runner,
	})
	if runtime.GOOS != "darwin" {
		if !errors.Is(err, errSourceAgentUpdaterUnsupportedPlatform) {
			t.Fatalf("err=%v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := control.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.path != "/bin/launchctl" || strings.Join(runner.args, " ") != "kickstart -k gui/501/life.executor.kbase.source-agent" {
		t.Fatalf("path=%q args=%#v", runner.path, runner.args)
	}
}

func TestSourceAgentUpdaterPlatformControlRejectsUnfixedIdentity(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin validation")
	}
	tests := []sourceAgentPlatformProcessConfig{
		{WorkerType: "wechat-worker", Label: "other.label", Domain: "gui", UID: 501},
		{WorkerType: "wcplus-worker", Label: "life.executor.kbase.source-agent", Domain: "gui", UID: 501},
		{WorkerType: "wechat-worker", Label: "life.executor.kbase.source-agent", Domain: "system", UID: 501},
		{WorkerType: "wechat-worker", Label: "life.executor.kbase.source-agent", Domain: "gui", UID: -1},
		{WorkerType: "wechat-worker;echo", Label: "life.executor.kbase.source-agent", Domain: "gui", UID: 501},
	}
	for _, config := range tests {
		if _, err := newSourceAgentPlatformProcessControl(config); err == nil {
			t.Fatalf("config %#v should fail", config)
		}
	}
}
