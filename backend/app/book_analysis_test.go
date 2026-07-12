package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestBookAnalysisManifestRoundTrip(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	want := BookAnalysisManifest{
		Version:     "1",
		BookID:      "source-article-1",
		ContentHash: "hash-1",
		Status:      BookAnalysisPending,
		UpdatedAt:   "2026-07-12T12:00:00Z",
	}
	if err := store.SaveAnalysisManifest(want); err != nil {
		t.Fatalf("SaveAnalysisManifest returned error: %v", err)
	}
	got, err := store.LoadAnalysisManifest(want.BookID)
	if err != nil {
		t.Fatalf("LoadAnalysisManifest returned error: %v", err)
	}
	if got.BookID != want.BookID || got.ContentHash != want.ContentHash || got.Status != BookAnalysisPending {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
}

func TestBookAnalysisManifestMissing(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	_, err := store.LoadAnalysisManifest("missing")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadAnalysisManifest error = %v, want os.ErrNotExist", err)
	}
}

func TestGenerateBookAnalysisManifestPersistsGroundedResult(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "https://token-plan.example.test/compatible-mode/v1")
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: sampleStructuredBookAnalysisJSON()}

	manifest, err := GenerateBookAnalysisManifestWithClient(context.Background(), store, BookAnalysisGenerateRequest{
		BookID: "42",
		Model:  "Qwen-3.7-Max",
	}, client)
	if err != nil {
		t.Fatalf("GenerateBookAnalysisManifestWithClient returned error: %v", err)
	}
	if manifest.Status != BookAnalysisReady || manifest.Model != "qwen3.7-max" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest.Payload == nil || manifest.Payload.Summary != "这是基于证据的分析。" || len(manifest.Payload.Claims) != 1 {
		t.Fatalf("structured payload = %#v", manifest.Payload)
	}
	if !strings.Contains(manifest.Answer, "核心摘要") || !strings.Contains(manifest.Answer, "42-chunk-1") {
		t.Fatalf("rendered answer = %q", manifest.Answer)
	}
	if manifest.ContentHash != pkg.Book.ContentHash || manifest.CompletedAt == "" {
		t.Fatalf("manifest provenance = %#v", manifest)
	}
	combined := client.messages[0].Content + "\n" + client.messages[1].Content
	for _, marker := range []string{"结构化分析", "核心摘要", "可验证结论", "风险与局限", "阅读或验证行动", "来源 ID"} {
		if !strings.Contains(combined, marker) {
			t.Fatalf("analysis prompt missing %q:\n%s", marker, combined)
		}
	}
	stored, err := store.LoadAnalysisManifest("42")
	if err != nil || stored.Payload == nil || stored.Status != BookAnalysisReady {
		t.Fatalf("stored manifest = %#v, err=%v", stored, err)
	}
}

func TestGenerateBookAnalysisManifestParsesStructuredPayload(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: "```json\n" + sampleStructuredBookAnalysisJSON() + "\n```"}

	manifest, err := GenerateBookAnalysisManifestWithClient(context.Background(), store, BookAnalysisGenerateRequest{BookID: "42"}, client)
	if err != nil {
		t.Fatalf("GenerateBookAnalysisManifestWithClient returned error: %v", err)
	}
	claim := manifest.Payload.Claims[0]
	if claim.ID != "claim-1" || claim.Statement == "" || len(claim.CitationIDs) != 1 || claim.CitationIDs[0] != "42-chunk-1" {
		t.Fatalf("claim = %#v", claim)
	}
	if claim.Confidence != 0.86 || claim.RiskLevel != "medium" || len(claim.Scope) != 1 {
		t.Fatalf("claim metadata = %#v", claim)
	}
	if len(manifest.Payload.Risks) != 1 || len(manifest.Payload.Actions) != 1 {
		t.Fatalf("payload = %#v", manifest.Payload)
	}
}

func TestGenerateBookAnalysisManifestRejectsMalformedPayload(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	previous := BookAnalysisManifest{
		Version: "1", BookID: "42", ContentHash: pkg.Book.ContentHash, Status: BookAnalysisReady,
		Answer: "previous answer", Payload: &BookAnalysisPayload{Summary: "previous summary"}, UpdatedAt: "2026-07-12T10:00:00Z",
	}
	if err := store.SaveAnalysisManifest(previous); err != nil {
		t.Fatal(err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: "not-json"}

	_, err := GenerateBookAnalysisManifestWithClient(context.Background(), store, BookAnalysisGenerateRequest{BookID: "42"}, client)
	if err == nil || !strings.Contains(err.Error(), "structured analysis") {
		t.Fatalf("generation error = %v", err)
	}
	stored, loadErr := store.LoadAnalysisManifest("42")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if stored.Status != BookAnalysisFailed || stored.Answer != previous.Answer || stored.Payload == nil || stored.Payload.Summary != "previous summary" {
		t.Fatalf("stored manifest = %#v", stored)
	}
}

func sampleStructuredBookAnalysisJSON() string {
	return `{
  "summary":"这是基于证据的分析。",
  "claims":[{"id":"claim-1","statement":"趋势过滤是该方法的前置条件。","citation_ids":["42-chunk-1"],"confidence":0.86,"scope":["示例策略"],"risk_level":"medium"}],
  "risks":[{"id":"risk-1","description":"需要外部数据验证。","citation_ids":["42-chunk-1"],"severity":"medium"}],
  "actions":[{"id":"action-1","description":"核对原始样本。","citation_ids":["42-chunk-1"],"kind":"verify"}]
}`
}

func TestGenerateBookAnalysisManifestPreservesPreviousAnswerOnFailure(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	if err := store.SaveAnalysisManifest(BookAnalysisManifest{
		Version: "1", BookID: "42", ContentHash: pkg.Book.ContentHash,
		Status: BookAnalysisReady, Model: "old-model", Answer: "previous answer", UpdatedAt: "2026-07-12T10:00:00Z",
	}); err != nil {
		t.Fatalf("SaveAnalysisManifest returned error: %v", err)
	}
	client := &fakeBookKnowledgeLLMClient{err: errors.New("model unavailable")}

	_, err := GenerateBookAnalysisManifestWithClient(context.Background(), store, BookAnalysisGenerateRequest{BookID: "42"}, client)
	if err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("generation error = %v", err)
	}
	stored, loadErr := store.LoadAnalysisManifest("42")
	if loadErr != nil {
		t.Fatalf("LoadAnalysisManifest returned error: %v", loadErr)
	}
	if stored.Status != BookAnalysisFailed || stored.Answer != "previous answer" || !strings.Contains(stored.Error, "model unavailable") {
		t.Fatalf("failed manifest = %#v", stored)
	}
}
