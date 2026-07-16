package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKnowledgeEvalScoresRetrievalCitationAndGaps(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:      "eval-book",
			Title:       "合成评估资料",
			SourceType:  "synthetic",
			ContentHash: "eval-hash",
			UpdatedAt:   "2026-07-16T10:00:00Z",
			Status:      "ready",
		},
		Chapters: []BookKnowledgeChapter{{ChapterID: "eval-chapter-1", BookID: "eval-book", Order: 1, Title: "证据"}},
		Chunks: []BookKnowledgeChunk{
			{ChunkID: "eval-chunk-1", BookID: "eval-book", ChapterID: "eval-chapter-1", Order: 1, Text: "疫苗证据需要同时关注试验设计、终点和安全性。"},
			{ChunkID: "eval-chunk-2", BookID: "eval-book", ChapterID: "eval-chapter-1", Order: 2, Text: "行动建议必须标注适用边界，避免直接替代专业判断。"},
		},
		Claims: []BookKnowledgeClaim{
			{ClaimID: "eval-claim-1", BookID: "eval-book", ChapterID: "eval-chapter-1", Title: "证据边界", Summary: "疫苗证据需要关注安全性。", Citations: []string{"eval-chunk-1"}},
		},
	}
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("save package: %v", err)
	}
	index, err := NewKnowledgeSearchIndex(store.Root())
	if err != nil {
		t.Fatalf("new index: %v", err)
	}
	defer index.Close()
	if _, err := index.RebuildFromBookStore(store); err != nil {
		t.Fatalf("rebuild index: %v", err)
	}
	suite := KnowledgeEvalSuite{
		SchemaVersion: KnowledgeEvalSchemaVersion,
		Cases: []KnowledgeEvalCase{
			{
				ID:               "hit",
				Query:            "疫苗 安全性",
				ExpectedChunkIDs: []string{"eval-chunk-1"},
				Answer:           "结论应引用安全性证据。[eval-chunk-1]",
				CitationIDs:      []string{"eval-chunk-1"},
			},
			{
				ID:               "gap",
				Query:            "不存在的主题",
				ExpectedChunkIDs: []string{"missing-chunk"},
				Answer:           "unsupported: 没有检索依据。",
				CitationIDs:      []string{"missing-chunk"},
				Domain:           "health",
			},
		},
	}

	report, err := EvaluateKnowledgeSuite(index, suite)
	if err != nil {
		t.Fatalf("evaluate suite: %v", err)
	}
	if report.SchemaVersion != KnowledgeEvalReportSchemaVersion || report.TotalCases != 2 {
		t.Fatalf("report header = %#v", report)
	}
	if report.RetrievalHitRate != 0.5 || report.CitationCoverage != 0.5 || report.UnsupportedAnswerCount != 1 || report.ZeroHitGapCount != 1 {
		t.Fatalf("scores = %#v", report)
	}
	if len(report.Gaps) != 1 || report.Gaps[0].Kind != "zero_hit" || report.Gaps[0].Fingerprint == "" {
		t.Fatalf("gaps = %#v", report.Gaps)
	}
}

func TestKnowledgeEvalSuiteFixtureLoads(t *testing.T) {
	suite, err := LoadKnowledgeEvalSuite(filepath.Join("..", "..", "contracts", "fixtures", "knowledge-eval-suite.json"))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if suite.SchemaVersion != KnowledgeEvalSchemaVersion || len(suite.Cases) == 0 {
		t.Fatalf("suite = %#v", suite)
	}
}

func TestKnowledgeEvalSmokeCommandFixturePathExists(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "contracts", "fixtures", "knowledge-eval-suite.json")); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
}
