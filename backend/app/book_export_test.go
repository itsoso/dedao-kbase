package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportBookKnowledgeForHealthSystemKB(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	result, err := ExportBookKnowledgePackage(store, "42", "health_system_kb_v2")
	if err != nil {
		t.Fatalf("ExportBookKnowledgePackage returned error: %v", err)
	}
	if result.Target != "health_system_kb_v2" {
		t.Fatalf("target = %q, want health_system_kb_v2", result.Target)
	}
	assertFileContains(t, filepath.Join(result.OutputDir, "claims.jsonl"), `"review_status":"draft"`)
	assertFileContains(t, filepath.Join(result.OutputDir, "pages.jsonl"), `"source":"dedao-gui"`)
	assertFileContains(t, filepath.Join(result.OutputDir, "entities.jsonl"), `"entity_type":"book"`)
}

func TestExportBookKnowledgeForQuantRuleCards(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	result, err := ExportBookKnowledgePackage(store, "42", "quant_rule_cards")
	if err != nil {
		t.Fatalf("ExportBookKnowledgePackage returned error: %v", err)
	}
	assertFileContains(t, filepath.Join(result.OutputDir, "rule_cards.jsonl"), `"execution_mode":"paper_only"`)
	assertFileContains(t, filepath.Join(result.OutputDir, "rule_cards.jsonl"), `"review_status":"draft"`)
}

func sampleBookKnowledgePackageForExport() BookKnowledgePackage {
	return BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:     "42",
			Title:      "42_量化分析_作者",
			SourceHTML: "/tmp/book.html",
			Status:     "draft",
		},
		Chapters: []BookKnowledgeChapter{
			{ChapterID: "42-chapter-1", BookID: "42", Order: 1, Title: "趋势过滤", Summary: "趋势过滤摘要"},
		},
		Chunks: []BookKnowledgeChunk{
			{ChunkID: "42-chunk-1", BookID: "42", ChapterID: "42-chapter-1", Text: "MACD 背离需要趋势过滤。"},
		},
		Claims: []BookKnowledgeClaim{
			{ClaimID: "42-claim-1", BookID: "42", ChapterID: "42-chapter-1", Title: "趋势过滤", Summary: "MACD 规则需要趋势过滤。", ReviewStatus: "draft"},
		},
		Citations: []BookKnowledgeCitation{
			{CitationID: "42-citation-1", BookID: "42", ChapterID: "42-chapter-1", ChunkID: "42-chunk-1", SourceHTML: "/tmp/book.html"},
		},
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) returned error: %v", path, err)
	}
	if !strings.Contains(string(content), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, string(content))
	}
}
