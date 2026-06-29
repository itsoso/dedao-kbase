package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSystemKBExportPathUsesServerWritablePath(t *testing.T) {
	t.Setenv("KBASE_SYSTEM_KB_EXPORT_PATH", "")
	t.Setenv("DEDAO_KBASE_ROOT", "")
	t.Setenv("DEDAO_WIKI_REPO", "")
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	actualCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}

	got := defaultSystemKBExportPath()
	want := filepath.Join(actualCwd, "artifacts", "system_kb_export.json")
	if got != want {
		t.Fatalf("defaultSystemKBExportPath() = %q, want %q", got, want)
	}
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err = os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q) returned error: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore Chdir(%q) returned error: %v", previous, err)
		}
	})
}

func TestDefaultSystemKBExportPathUsesDedaoWikiRepo(t *testing.T) {
	t.Setenv("KBASE_SYSTEM_KB_EXPORT_PATH", "")
	t.Setenv("DEDAO_KBASE_ROOT", "")
	t.Setenv("DEDAO_WIKI_REPO", "/srv/dedao-kbase")

	got := defaultSystemKBExportPath()
	want := "/srv/dedao-kbase/artifacts/system_kb_export.json"
	if got != want {
		t.Fatalf("defaultSystemKBExportPath() = %q, want %q", got, want)
	}
}
