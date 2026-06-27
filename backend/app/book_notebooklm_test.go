package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportNotebookLMBridgePackageWritesMarkdownAndMetadata(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	bridge, err := ExportNotebookLMBridgePackage(store, "42")
	if err != nil {
		t.Fatalf("ExportNotebookLMBridgePackage returned error: %v", err)
	}
	if bridge.BookID != "42" {
		t.Fatalf("BookID = %q, want 42", bridge.BookID)
	}
	if bridge.LastExportDir == "" {
		t.Fatalf("LastExportDir is empty")
	}
	if len(bridge.LastExportFiles) != 4 {
		t.Fatalf("LastExportFiles = %#v, want 4 markdown files", bridge.LastExportFiles)
	}

	bookMarkdown := filepath.Join(bridge.LastExportDir, "book.md")
	assertFileContains(t, bookMarkdown, "# 42_量化分析_作者")
	assertFileContains(t, bookMarkdown, "MACD 背离需要趋势过滤。")
	assertFileContains(t, filepath.Join(bridge.LastExportDir, "claims.md"), "MACD 规则需要趋势过滤")
	assertFileContains(t, filepath.Join(bridge.LastExportDir, "notebooklm-prompt.md"), "NotebookLM")
	assertFileContains(t, filepath.Join(bridge.LastExportDir, "upload-guide.md"), "上传到 NotebookLM")
	assertFileContains(t, filepath.Join(bridge.LastExportDir, "upload-guide.md"), "book.md")

	loaded, err := store.LoadNotebookLMBridge("42")
	if err != nil {
		t.Fatalf("LoadNotebookLMBridge returned error: %v", err)
	}
	if loaded.LastExportDir != bridge.LastExportDir {
		t.Fatalf("loaded LastExportDir = %q, want %q", loaded.LastExportDir, bridge.LastExportDir)
	}
	if loaded.UpdatedAt == "" {
		t.Fatalf("UpdatedAt is empty")
	}
}

func TestSaveNotebookLMLinkPersistsAndValidatesURL(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	bridge, err := store.SaveNotebookLMLink("42", "https://notebooklm.google.com/notebook/test")
	if err != nil {
		t.Fatalf("SaveNotebookLMLink returned error: %v", err)
	}
	if bridge.NotebookURL != "https://notebooklm.google.com/notebook/test" {
		t.Fatalf("NotebookURL = %q", bridge.NotebookURL)
	}

	loaded, err := store.LoadNotebookLMBridge("42")
	if err != nil {
		t.Fatalf("LoadNotebookLMBridge returned error: %v", err)
	}
	if loaded.NotebookURL != bridge.NotebookURL {
		t.Fatalf("loaded NotebookURL = %q, want %q", loaded.NotebookURL, bridge.NotebookURL)
	}

	if _, err := store.SaveNotebookLMLink("42", "not a url"); err == nil || !strings.Contains(err.Error(), "notebook url") {
		t.Fatalf("SaveNotebookLMLink invalid URL error = %v, want notebook url validation", err)
	}

	if _, err := os.Stat(store.NotebookLMBridgePath("42")); err != nil {
		t.Fatalf("notebook bridge metadata was not written: %v", err)
	}
}
