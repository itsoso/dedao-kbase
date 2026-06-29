package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEbookHTMLPath(t *testing.T) {
	got, err := ebookHTMLPath("/tmp/down-dedao", "123_测试: 电子书_作者")
	if err != nil {
		t.Fatalf("ebookHTMLPath returned error: %v", err)
	}

	want := "/tmp/down-dedao/Ebook/123_测试：电子书_作者.html"
	if got != want {
		t.Fatalf("ebookHTMLPath() = %q, want %q", got, want)
	}
}

func TestDefaultEbookWikiSyncConfigUsesServerWritableRepo(t *testing.T) {
	t.Setenv("DEDAO_WIKI_REPO", "")
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	actualCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}

	cfg := DefaultEbookWikiSyncConfig()
	want := filepath.Join(actualCwd, "down-dedao")
	if cfg.RepoDir != want {
		t.Fatalf("RepoDir = %q, want %q", cfg.RepoDir, want)
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

func TestEbookWikiCommand(t *testing.T) {
	cfg := EbookWikiSyncConfig{
		RepoDir:      "/tmp/down-dedao",
		WikisCommand: "llms-wikis",
		Python:       "python3",
	}

	got := ebookWikiIngestCommand(cfg, "/tmp/down-dedao/Ebook/book.html", 42, "42_书名_作者")

	if got.Dir != "/tmp/down-dedao" {
		t.Fatalf("Dir = %q, want repo dir", got.Dir)
	}
	if got.Name != "llms-wikis" {
		t.Fatalf("Name = %q, want llms-wikis", got.Name)
	}
	wantArgs := []string{
		"ingest-ebook",
		"--repo", "/tmp/down-dedao",
		"--input", "/tmp/down-dedao/Ebook/book.html",
		"--book-id", "42",
		"--title", "42_书名_作者",
	}
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("Args = %#v, want %#v", got.Args, wantArgs)
	}
}

func TestEbookCompilerCommand(t *testing.T) {
	cfg := EbookWikiSyncConfig{
		RepoDir:      "/tmp/down-dedao",
		WikisCommand: "llms-wikis",
		Python:       "python3",
	}

	got := ebookWikiCompilerCommand(cfg)

	if got.Dir != "/tmp/down-dedao" {
		t.Fatalf("Dir = %q, want repo dir", got.Dir)
	}
	if got.Name != "python3" {
		t.Fatalf("Name = %q, want python3", got.Name)
	}
	wantArgs := []string{"pipeline/compiler.py", "--changed-only"}
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("Args = %#v, want %#v", got.Args, wantArgs)
	}
}

func TestSyncEbookToWikiRunsIngestThenCompiler(t *testing.T) {
	runner := &fakeEbookWikiRunner{}
	cfg := EbookWikiSyncConfig{
		RepoDir:      "/tmp/down-dedao",
		WikisCommand: "llms-wikis",
		Python:       "python3",
	}
	input := EbookWikiInput{
		BookID:   42,
		Title:    "42_书名_作者",
		HTMLPath: "/tmp/down-dedao/Ebook/book.html",
	}

	if err := runEbookWikiPipeline(context.Background(), cfg, runner, input); err != nil {
		t.Fatalf("runEbookWikiPipeline returned error: %v", err)
	}

	got := runner.commands
	want := []string{
		"llms-wikis ingest-ebook --repo /tmp/down-dedao --input /tmp/down-dedao/Ebook/book.html --book-id 42 --title 42_书名_作者",
		"python3 pipeline/compiler.py --changed-only",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestSyncEbookToWikiReturnsCommandOutputOnIngestFailure(t *testing.T) {
	runner := &fakeEbookWikiRunner{
		failAt: 1,
		output: "missing llms-wikis",
		runErr: errors.New("exit status 127"),
	}
	cfg := EbookWikiSyncConfig{
		RepoDir:      "/tmp/down-dedao",
		WikisCommand: "llms-wikis",
		Python:       "python3",
	}
	input := EbookWikiInput{
		BookID:   42,
		Title:    "42_书名_作者",
		HTMLPath: "/tmp/down-dedao/Ebook/book.html",
	}

	err := runEbookWikiPipeline(context.Background(), cfg, runner, input)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing llms-wikis") {
		t.Fatalf("error = %q, want command output", err.Error())
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %#v, want only ingest command", runner.commands)
	}
}

func TestSyncEbookToWikiReturnsCommandOutputOnCompilerFailure(t *testing.T) {
	runner := &fakeEbookWikiRunner{
		failAt: 2,
		output: "compiler failed",
		runErr: errors.New("exit status 1"),
	}
	cfg := EbookWikiSyncConfig{
		RepoDir:      "/tmp/down-dedao",
		WikisCommand: "llms-wikis",
		Python:       "python3",
	}
	input := EbookWikiInput{
		BookID:   42,
		Title:    "42_书名_作者",
		HTMLPath: "/tmp/down-dedao/Ebook/book.html",
	}

	err := runEbookWikiPipeline(context.Background(), cfg, runner, input)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "compiler failed") {
		t.Fatalf("error = %q, want command output", err.Error())
	}
	if len(runner.commands) != 2 {
		t.Fatalf("commands = %#v, want ingest and compiler commands", runner.commands)
	}
}

type fakeEbookWikiRunner struct {
	commands []string
	failAt   int
	output   string
	runErr   error
}

func (r *fakeEbookWikiRunner) Run(_ context.Context, cmd ebookWikiCommand) ([]byte, error) {
	r.commands = append(r.commands, strings.Join(append([]string{cmd.Name}, cmd.Args...), " "))
	if r.failAt > 0 && len(r.commands) == r.failAt {
		return []byte(r.output), r.runErr
	}
	return nil, nil
}
