package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
