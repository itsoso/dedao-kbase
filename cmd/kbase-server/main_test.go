package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

func TestDefaultSystemKBExportPathUsesRepoDirEnv(t *testing.T) {
	t.Setenv("KBASE_SYSTEM_KB_EXPORT_PATH", "")
	t.Setenv("DEDAO_KBASE_ROOT", "")
	t.Setenv("DEDAO_WIKI_REPO", "")
	t.Setenv("DEDAO_WIKI_REPO_DIR", "/tmp/wiki-root")

	got := defaultSystemKBExportPath()
	want := filepath.Join("/tmp/wiki-root", "artifacts", "system_kb_export.json")
	if got != want {
		t.Fatalf("defaultSystemKBExportPath() = %q, want %q", got, want)
	}
}

func TestDefaultSystemKBExportPathHasNoPrivateFallback(t *testing.T) {
	t.Setenv("KBASE_SYSTEM_KB_EXPORT_PATH", "")
	t.Setenv("DEDAO_KBASE_ROOT", "")
	t.Setenv("DEDAO_WIKI_REPO", "")
	t.Setenv("DEDAO_WIKI_REPO_DIR", "")

	got := defaultSystemKBExportPath()
	privatePathToken := "/" + "Users" + "/"
	privateUserToken := "li" + "qiuhua"
	if strings.Contains(got, privatePathToken) || strings.Contains(got, privateUserToken) {
		t.Fatalf("defaultSystemKBExportPath leaks a private fallback path: %q", got)
	}
}

func TestDefaultWebDirUsesEnv(t *testing.T) {
	t.Setenv("KBASE_WEB_DIR", "/tmp/kbase-web")

	if got := defaultWebDir(); got != "/tmp/kbase-web" {
		t.Fatalf("defaultWebDir() = %q, want env value", got)
	}
}

func TestDefaultWebDirUsesRepoLocalFrontendWeb(t *testing.T) {
	t.Setenv("KBASE_WEB_DIR", "")
	root := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("Chdir cleanup returned error: %v", err)
		}
	})
	if err := os.Mkdir(filepath.Join(root, "frontend-web"), os.ModePerm); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	if got := defaultWebDir(); got != "frontend-web" {
		t.Fatalf("defaultWebDir() = %q, want frontend-web", got)
	}
}

func TestWCPlusBaseURLConfiguredFromEnvSupportsWCPlusPro(t *testing.T) {
	t.Setenv("WCPLUS_BASE_URL", "")
	t.Setenv("WCPLUSPRO_BASE_URL", "http://127.0.0.1:5999")

	if !wcplusBaseURLConfiguredFromEnv() {
		t.Fatalf("wcplusBaseURLConfiguredFromEnv() = false, want true for WCPLUSPRO_BASE_URL")
	}

	t.Setenv("WCPLUS_BASE_URL", "")
	t.Setenv("WCPLUSPRO_BASE_URL", "")
	if wcplusBaseURLConfiguredFromEnv() {
		t.Fatalf("wcplusBaseURLConfiguredFromEnv() = true, want false without WC Plus env")
	}
}

func TestDefaultSourceAgentTokenUsesTrimmedEnv(t *testing.T) {
	t.Setenv("KBASE_SOURCE_AGENT_TOKEN", "  source-agent-secret  ")
	if got := defaultSourceAgentToken(); got != "source-agent-secret" {
		t.Fatalf("defaultSourceAgentToken() = %q", got)
	}
}

func TestStartSourceSchedulerRequiresSourceAgentTokenAndStopsWithContext(t *testing.T) {
	runnerStarted := make(chan struct{}, 1)
	runner := sourceSchedulerRunFunc(func(ctx context.Context, interval time.Duration, onTick func(app.SourceSchedulerTickResult, error)) {
		runnerStarted <- struct{}{}
		<-ctx.Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	started, done := startSourceScheduler(ctx, "", time.Second, runner, func(string, ...any) {})
	if started {
		t.Fatal("scheduler started without source-agent token")
	}
	select {
	case <-done:
	default:
		t.Fatal("disabled scheduler completion signal is not closed")
	}
	started, done = startSourceScheduler(ctx, "source-agent-secret", time.Second, runner, func(string, ...any) {})
	if !started {
		t.Fatal("scheduler did not start with source-agent token")
	}
	select {
	case <-runnerStarted:
	case <-time.After(time.Second):
		t.Fatal("scheduler runner did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler runner did not stop with context")
	}
}

func TestSourceSchedulerTickIntervalUsesBoundedEnvironmentValue(t *testing.T) {
	t.Setenv("KBASE_SOURCE_SCHEDULER_TICK_SECONDS", "45")
	if got := sourceSchedulerTickInterval(); got != 45*time.Second {
		t.Fatalf("sourceSchedulerTickInterval() = %s", got)
	}
	t.Setenv("KBASE_SOURCE_SCHEDULER_TICK_SECONDS", "0")
	if got := sourceSchedulerTickInterval(); got != 30*time.Second {
		t.Fatalf("zero sourceSchedulerTickInterval() = %s", got)
	}
	t.Setenv("KBASE_SOURCE_SCHEDULER_TICK_SECONDS", "9999")
	if got := sourceSchedulerTickInterval(); got != 5*time.Minute {
		t.Fatalf("bounded sourceSchedulerTickInterval() = %s", got)
	}
}
