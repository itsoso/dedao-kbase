package app

import (
	"strings"
	"testing"
)

func TestBookKnowledgeQualityReportGeneratedOnSave(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleQualityBookKnowledgePackage("quality-usable")

	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	report, err := store.LoadBookQualityReport("quality-usable")
	if err != nil {
		t.Fatalf("LoadBookQualityReport returned error: %v", err)
	}
	if report.BookID != "quality-usable" {
		t.Fatalf("BookID = %q, want quality-usable", report.BookID)
	}
	if report.Status != BookKnowledgeQualityStatusUsable {
		t.Fatalf("Status = %q, want usable; issues=%#v", report.Status, report.Issues)
	}
	if report.Score < 0.75 {
		t.Fatalf("Score = %.2f, want >= 0.75", report.Score)
	}
	if report.Metrics.Chapters != len(pkg.Chapters) || report.Metrics.Chunks != len(pkg.Chunks) ||
		report.Metrics.Claims != len(pkg.Claims) || report.Metrics.Citations != len(pkg.Citations) {
		t.Fatalf("metrics = %#v", report.Metrics)
	}

	books, err := store.ListBooks()
	if err != nil {
		t.Fatalf("ListBooks returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("books = %#v, want one book", books)
	}
	if books[0].QualityStatus != BookKnowledgeQualityStatusUsable || books[0].QualityScore < 0.75 ||
		books[0].QualityUpdatedAt == "" {
		t.Fatalf("book quality summary = %#v", books[0])
	}

	loaded, err := store.LoadPackage("quality-usable")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if loaded.QualityReport == nil || loaded.QualityReport.Status != BookKnowledgeQualityStatusUsable {
		t.Fatalf("loaded quality report = %#v", loaded.QualityReport)
	}
}

func TestBookKnowledgeQualityReportRejectsEmptyPackage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID: "empty-quality",
			Title:  "Empty Quality",
		},
	}

	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	report, err := store.LoadBookQualityReport("empty-quality")
	if err != nil {
		t.Fatalf("LoadBookQualityReport returned error: %v", err)
	}
	if report.Status != BookKnowledgeQualityStatusRejected {
		t.Fatalf("Status = %q, want rejected", report.Status)
	}
	if !qualityIssuesContain(report.Issues, "missing_chunks") {
		t.Fatalf("issues = %#v, want missing_chunks", report.Issues)
	}
	if !qualityIssuesContain(report.Issues, "missing_chapters") {
		t.Fatalf("issues = %#v, want missing_chapters", report.Issues)
	}
	if len(report.AllowedUses) != 0 {
		t.Fatalf("AllowedUses = %#v, want none", report.AllowedUses)
	}
	if !containsString(report.BlockedUses, "all_downstream_use") {
		t.Fatalf("BlockedUses = %#v, want all_downstream_use", report.BlockedUses)
	}
}

func TestBookKnowledgeQualityReportMarksMissingClaimsForReview(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleQualityBookKnowledgePackage("quality-needs-review")
	pkg.Claims = nil
	pkg.Citations = nil

	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	report, err := store.LoadBookQualityReport("quality-needs-review")
	if err != nil {
		t.Fatalf("LoadBookQualityReport returned error: %v", err)
	}
	if report.Status != BookKnowledgeQualityStatusNeedsReview {
		t.Fatalf("Status = %q, want needs_review; issues=%#v", report.Status, report.Issues)
	}
	if !qualityIssuesContain(report.Issues, "missing_claims") {
		t.Fatalf("issues = %#v, want missing_claims", report.Issues)
	}
}

func qualityIssuesContain(issues []BookKnowledgeQualityIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func sampleQualityBookKnowledgePackage(bookID string) BookKnowledgePackage {
	longText := strings.Repeat("这是一段用于质量治理测试的长文本，包含足够的上下文、概念说明和证据线索。", 8)
	return BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:     bookID,
			Title:      "质量治理测试书",
			Author:     "测试作者",
			SourceHTML: "dedao://quality-test",
			Status:     "draft",
			Extractor:  "dedao-gui-fallback",
		},
		Chapters: []BookKnowledgeChapter{
			{
				ChapterID: bookID + "-chapter-1",
				BookID:    bookID,
				Order:     1,
				Title:     "第一章",
				Summary:   "第一章摘要",
				ChunkIDs:  []string{bookID + "-chunk-1"},
			},
		},
		Chunks: []BookKnowledgeChunk{
			{
				ChunkID:   bookID + "-chunk-1",
				BookID:    bookID,
				ChapterID: bookID + "-chapter-1",
				Order:     1,
				Text:      longText,
				Tokens:    120,
			},
		},
		Claims: []BookKnowledgeClaim{
			{
				ClaimID:       bookID + "-claim-1",
				BookID:        bookID,
				ChapterID:     bookID + "-chapter-1",
				Title:         "质量治理需要稳定来源",
				Summary:       "入库内容应该包含章节、chunks、claims 和 citations。",
				Body:          "稳定来源能帮助下游系统判断材料是否可用。",
				EvidenceLevel: "B",
				Confidence:    0.85,
				ReviewStatus:  "draft",
				Citations:     []string{bookID + "-citation-1"},
			},
		},
		Citations: []BookKnowledgeCitation{
			{
				CitationID: bookID + "-citation-1",
				BookID:     bookID,
				ChapterID:  bookID + "-chapter-1",
				ChunkID:    bookID + "-chunk-1",
				SourceHTML: "dedao://quality-test",
				Anchor:     "第一章",
				Note:       "自动提取",
			},
		},
	}
}
