package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKBaseHTTPHandlerRequiresBearerTokenForAPI(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books", "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status without token = %d, want 401", resp.Code)
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "wrong-token")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status with wrong token = %d, want 401", resp.Code)
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("status with correct token = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"42"`) {
		t.Fatalf("books response missing sample book: %s", resp.Body.String())
	}
}

func TestKBaseHTTPHandlerServesSearchAndSystemKBExport(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	exportPath := filepath.Join(root, "artifacts", "system_kb_export.json")
	if err := os.MkdirAll(filepath.Dir(exportPath), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	exportPayload := map[string]any{
		"type":        "system_kb_v2_export",
		"schema_id":   "llm-wiki-v2-system-kb-export",
		"version":     "test-version",
		"source":      "dedao-kbase",
		"compiled_at": "2026-06-27T10:00:00Z",
		"stats":       map[string]any{"claim_count": 1},
		"pages":       []any{},
		"entities":    []any{},
		"claims":      []any{},
		"relations":   []any{},
	}
	data, err := json.Marshal(exportPayload)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if err := os.WriteFile(exportPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:              store,
		AuthToken:          "secret-token",
		SystemKBExportPath: exportPath,
	})

	searchResp := requestKBase(handler, http.MethodGet, "/api/search?q=MACD&limit=5", "secret-token")
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", searchResp.Code, searchResp.Body.String())
	}
	if !strings.Contains(searchResp.Body.String(), `"results"`) || !strings.Contains(searchResp.Body.String(), `"42"`) {
		t.Fatalf("search response missing results: %s", searchResp.Body.String())
	}

	manifestResp := requestKBase(handler, http.MethodGet, "/api/system-kb/manifest", "secret-token")
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, body=%s", manifestResp.Code, manifestResp.Body.String())
	}
	if !strings.Contains(manifestResp.Body.String(), `"version":"test-version"`) {
		t.Fatalf("manifest response missing version: %s", manifestResp.Body.String())
	}

	exportResp := requestKBase(handler, http.MethodGet, "/api/system-kb/export", "secret-token")
	if exportResp.Code != http.StatusOK {
		t.Fatalf("export status = %d, body=%s", exportResp.Code, exportResp.Body.String())
	}
	if !strings.Contains(exportResp.Body.String(), `"type":"system_kb_v2_export"`) {
		t.Fatalf("export response missing payload: %s", exportResp.Body.String())
	}
}

func TestKBaseHTTPHandlerListsBooksWithPagination(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	for i, title := range []string{"Book Alpha", "Book Beta", "Book Gamma"} {
		pkg := sampleBookKnowledgePackageWithID(fmt.Sprintf("book-%d", i+1), title)
		if err := store.SavePackage(pkg); err != nil {
			t.Fatalf("SavePackage returned error: %v", err)
		}
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books?page=2&page_size=1&q=Book&sort=title_asc", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("books status = %d, body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Books      []BookKnowledgeBook `json:"books"`
		Page       int                 `json:"page"`
		PageSize   int                 `json:"page_size"`
		Total      int                 `json:"total"`
		TotalPages int                 `json:"total_pages"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Page != 2 || payload.PageSize != 1 || payload.Total != 3 || payload.TotalPages != 3 {
		t.Fatalf("pagination payload = %#v", payload)
	}
	if len(payload.Books) != 1 || payload.Books[0].BookID != "book-2" {
		t.Fatalf("books page = %#v, want second title-sorted book", payload.Books)
	}
}

func TestKBaseHTTPHandlerServesBookPromptsChatAndHistory(t *testing.T) {
	var gotTokenPlanAuth string
	tokenPlanServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTokenPlanAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/compatible-mode/v1/chat/completions" {
			t.Fatalf("TokenPlan path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Web 对话回答"}}]}`))
	}))
	defer tokenPlanServer.Close()

	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte(strings.Join([]string{
		"TOKENPLAN_API_KEY=sk-web-test",
		"TOKENPLAN_BASE_URL=" + tokenPlanServer.URL + "/compatible-mode/v1",
		"TOKENPLAN_MODEL=MiniMax-M2.5",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "")
	t.Setenv("DEDAO_TOKENPLAN_MODEL", "")
	t.Setenv("TOKENPLAN_API_KEY", "")
	t.Setenv("TOKENPLAN_BASE_URL", "")
	t.Setenv("TOKENPLAN_MODEL", "")
	t.Setenv("DEDAO_TOKENPLAN_ENV_FILE", envFile)

	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	promptsResp := requestKBase(handler, http.MethodGet, "/api/books/42/prompts", "secret-token")
	if promptsResp.Code != http.StatusOK {
		t.Fatalf("prompts status = %d, body=%s", promptsResp.Code, promptsResp.Body.String())
	}
	if !strings.Contains(promptsResp.Body.String(), `"prompts"`) ||
		!strings.Contains(promptsResp.Body.String(), `"understand-core"`) {
		t.Fatalf("prompts response missing templates: %s", promptsResp.Body.String())
	}

	chatResp := requestKBaseJSON(handler, http.MethodPost, "/api/books/42/chat", "secret-token", `{"mode":"chat","question":"如何理解 MACD？","model":"qwen3.7-max"}`)
	if chatResp.Code != http.StatusOK {
		t.Fatalf("chat status = %d, body=%s", chatResp.Code, chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"answer":"Web 对话回答"`) ||
		!strings.Contains(chatResp.Body.String(), `"model":"qwen3.7-max"`) ||
		!strings.Contains(chatResp.Body.String(), `"sources"`) ||
		!strings.Contains(chatResp.Body.String(), `"context_stats"`) {
		t.Fatalf("chat response missing answer metadata: %s", chatResp.Body.String())
	}
	if gotTokenPlanAuth != "Bearer sk-web-test" {
		t.Fatalf("TokenPlan auth = %q, want fake bearer", gotTokenPlanAuth)
	}

	historyResp := requestKBase(handler, http.MethodGet, "/api/books/42/chat-history?limit=10", "secret-token")
	if historyResp.Code != http.StatusOK {
		t.Fatalf("history status = %d, body=%s", historyResp.Code, historyResp.Body.String())
	}
	if !strings.Contains(historyResp.Body.String(), `"history"`) ||
		!strings.Contains(historyResp.Body.String(), `"Web 对话回答"`) {
		t.Fatalf("history response missing saved chat: %s", historyResp.Body.String())
	}
}

func TestKBaseHTTPHandlerServesProjectKnowledgeHub(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	projectsResp := requestKBase(handler, http.MethodGet, "/api/projects", "secret-token")
	if projectsResp.Code != http.StatusOK {
		t.Fatalf("projects status = %d, body=%s", projectsResp.Code, projectsResp.Body.String())
	}
	if !strings.Contains(projectsResp.Body.String(), `"project_id":"health"`) ||
		!strings.Contains(projectsResp.Body.String(), `"project_id":"proofroom"`) ||
		!strings.Contains(projectsResp.Body.String(), `"requires_review":true`) {
		t.Fatalf("projects response missing governed project descriptors: %s", projectsResp.Body.String())
	}

	queueResp := requestKBase(handler, http.MethodGet, "/api/projects/health/review-queue?limit=5", "secret-token")
	if queueResp.Code != http.StatusOK {
		t.Fatalf("review queue status = %d, body=%s", queueResp.Code, queueResp.Body.String())
	}
	if !strings.Contains(queueResp.Body.String(), `"project_id":"health"`) ||
		!strings.Contains(queueResp.Body.String(), `"review_status":"needs_review"`) ||
		!strings.Contains(queueResp.Body.String(), `"book_id":"42"`) ||
		!strings.Contains(queueResp.Body.String(), `"claim_id":"42-claim-1"`) {
		t.Fatalf("review queue response missing source IDs and review status: %s", queueResp.Body.String())
	}

	previewResp := requestKBase(handler, http.MethodGet, "/api/projects/proofroom/export-preview?limit=5", "secret-token")
	if previewResp.Code != http.StatusOK {
		t.Fatalf("export preview status = %d, body=%s", previewResp.Code, previewResp.Body.String())
	}
	if !strings.Contains(previewResp.Body.String(), `"project_id":"proofroom"`) ||
		!strings.Contains(previewResp.Body.String(), `"export_type":"proofroom_source_pack"`) ||
		!strings.Contains(previewResp.Body.String(), `"source_policy":"draft_source_material"`) ||
		!strings.Contains(previewResp.Body.String(), `"claim_count":1`) {
		t.Fatalf("export preview response missing proofroom source-pack semantics: %s", previewResp.Body.String())
	}

	unknownResp := requestKBase(handler, http.MethodGet, "/api/projects/unknown/review-queue", "secret-token")
	if unknownResp.Code != http.StatusNotFound {
		t.Fatalf("unknown project status = %d, want 404", unknownResp.Code)
	}
}

func TestKBaseHTTPHandlerServesProjectVerificationReport(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForVerification()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	healthResp := requestKBase(handler, http.MethodGet, "/api/projects/health/verification-report?limit=10", "secret-token")
	if healthResp.Code != http.StatusOK {
		t.Fatalf("health verification status = %d, body=%s", healthResp.Code, healthResp.Body.String())
	}
	healthBody := healthResp.Body.String()
	for _, want := range []string{
		`"project_id":"health"`,
		`"autonomy_mode":"machine_verified_async_audit"`,
		`"human_loop":"async_audit_only"`,
		`"verification_score"`,
		`"source_hash"`,
		`"risk_tier":"assistive_only"`,
		`"risk_tier":"needs_human"`,
		`"decision":"assist"`,
		`"decision":"queue"`,
		`"not_medical_advice"`,
	} {
		if !strings.Contains(healthBody, want) {
			t.Fatalf("health verification response missing %q: %s", want, healthBody)
		}
	}
	if strings.Contains(healthBody, `"claim_id":"verify-claim-medication","risk_tier":"auto_usable"`) {
		t.Fatalf("health-sensitive claim must not be auto usable: %s", healthBody)
	}

	proofroomResp := requestKBase(handler, http.MethodGet, "/api/projects/proofroom/verification-report?limit=10", "secret-token")
	if proofroomResp.Code != http.StatusOK {
		t.Fatalf("proofroom verification status = %d, body=%s", proofroomResp.Code, proofroomResp.Body.String())
	}
	proofroomBody := proofroomResp.Body.String()
	for _, want := range []string{
		`"project_id":"proofroom"`,
		`"risk_tier":"auto_usable"`,
		`"decision":"allow"`,
		`"argument_draft"`,
		`"citation_presence"`,
		`"review_sampling":"async_sample"`,
	} {
		if !strings.Contains(proofroomBody, want) {
			t.Fatalf("proofroom verification response missing %q: %s", want, proofroomBody)
		}
	}

	unknownResp := requestKBase(handler, http.MethodGet, "/api/projects/unknown/verification-report", "secret-token")
	if unknownResp.Code != http.StatusNotFound {
		t.Fatalf("unknown project verification status = %d, want 404", unknownResp.Code)
	}
}

func sampleBookKnowledgePackageForVerification() BookKnowledgePackage {
	return BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:     "verify-book",
			Title:      "验证能力测试书",
			SourceHTML: "/tmp/verify-book.html",
			Status:     "draft",
		},
		Chapters: []BookKnowledgeChapter{
			{ChapterID: "verify-chapter-1", BookID: "verify-book", Order: 1, Title: "验证章节", Summary: "验证章节摘要"},
		},
		Chunks: []BookKnowledgeChunk{
			{ChunkID: "verify-chunk-1", BookID: "verify-book", ChapterID: "verify-chapter-1", Order: 1, Text: "稳定复盘可以帮助学习者识别错误模式。"},
			{ChunkID: "verify-chunk-2", BookID: "verify-book", ChapterID: "verify-chapter-1", Order: 2, Text: "具体用药剂量必须由医生结合个体情况判断。"},
		},
		Claims: []BookKnowledgeClaim{
			{
				ClaimID:       "verify-claim-study",
				BookID:        "verify-book",
				ChapterID:     "verify-chapter-1",
				Title:         "复盘提高学习质量",
				Summary:       "稳定复盘可以帮助学习者识别错误模式。",
				EvidenceLevel: "B",
				Confidence:    0.92,
				ReviewStatus:  "draft",
				Citations:     []string{"verify-citation-1"},
			},
			{
				ClaimID:       "verify-claim-medication",
				BookID:        "verify-book",
				ChapterID:     "verify-chapter-1",
				Title:         "用药剂量需要个体判断",
				Summary:       "具体用药剂量必须由医生结合个体情况判断。",
				EvidenceLevel: "B",
				Confidence:    0.88,
				ReviewStatus:  "draft",
				Citations:     []string{"verify-citation-2"},
			},
			{
				ClaimID:       "verify-claim-unsupported",
				BookID:        "verify-book",
				ChapterID:     "verify-chapter-1",
				Title:         "缺少引用的观点",
				Summary:       "这条观点没有引用来源。",
				EvidenceLevel: "D",
				Confidence:    0.35,
				ReviewStatus:  "draft",
			},
		},
		Citations: []BookKnowledgeCitation{
			{CitationID: "verify-citation-1", BookID: "verify-book", ChapterID: "verify-chapter-1", ChunkID: "verify-chunk-1", SourceHTML: "/tmp/verify-book.html"},
			{CitationID: "verify-citation-2", BookID: "verify-book", ChapterID: "verify-chapter-1", ChunkID: "verify-chunk-2", SourceHTML: "/tmp/verify-book.html"},
		},
	}
}

func TestKBaseHTTPHandlerPersistsProjectCollectionAndAuditQueue(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForVerification()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	refreshResp := requestKBase(handler, http.MethodPost, "/api/projects/health/collection/refresh?limit=10", "secret-token")
	if refreshResp.Code != http.StatusOK {
		t.Fatalf("collection refresh status = %d, body=%s", refreshResp.Code, refreshResp.Body.String())
	}
	refreshBody := refreshResp.Body.String()
	for _, want := range []string{
		`"project_id":"health"`,
		`"source":"verification_report"`,
		`"human_loop":"async_audit_only"`,
		`"collection_id"`,
		`"items"`,
		`"audit_queue"`,
		`"review_status":"pending_async_audit"`,
		`"sample_reason":"health_sensitive_claim"`,
		`"sample_reason":"missing_citation"`,
		`"source_hash"`,
	} {
		if !strings.Contains(refreshBody, want) {
			t.Fatalf("collection refresh response missing %q: %s", want, refreshBody)
		}
	}

	collectionResp := requestKBase(handler, http.MethodGet, "/api/projects/health/collection", "secret-token")
	if collectionResp.Code != http.StatusOK {
		t.Fatalf("collection status = %d, body=%s", collectionResp.Code, collectionResp.Body.String())
	}
	if !strings.Contains(collectionResp.Body.String(), `"project_id":"health"`) ||
		!strings.Contains(collectionResp.Body.String(), `"item_count":3`) {
		t.Fatalf("collection response missing persisted summary: %s", collectionResp.Body.String())
	}

	auditResp := requestKBase(handler, http.MethodGet, "/api/projects/health/audit-queue?limit=10", "secret-token")
	if auditResp.Code != http.StatusOK {
		t.Fatalf("audit queue status = %d, body=%s", auditResp.Code, auditResp.Body.String())
	}
	if !strings.Contains(auditResp.Body.String(), `"project_id":"health"`) ||
		!strings.Contains(auditResp.Body.String(), `"audit_items"`) ||
		!strings.Contains(auditResp.Body.String(), `"pending_async_audit"`) {
		t.Fatalf("audit queue response missing async audit contract: %s", auditResp.Body.String())
	}

	unknownResp := requestKBase(handler, http.MethodPost, "/api/projects/unknown/collection/refresh", "secret-token")
	if unknownResp.Code != http.StatusNotFound {
		t.Fatalf("unknown project collection status = %d, want 404", unknownResp.Code)
	}
}

func TestKBaseHTTPHandlerExportsProjectCollectionJSONL(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForVerification()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	missingResp := requestKBase(handler, http.MethodGet, "/api/projects/health/collection/export?format=jsonl", "secret-token")
	if missingResp.Code != http.StatusNotFound {
		t.Fatalf("missing collection export status = %d, want 404", missingResp.Code)
	}

	refreshResp := requestKBase(handler, http.MethodPost, "/api/projects/health/collection/refresh?limit=10", "secret-token")
	if refreshResp.Code != http.StatusOK {
		t.Fatalf("collection refresh status = %d, body=%s", refreshResp.Code, refreshResp.Body.String())
	}

	exportResp := requestKBase(handler, http.MethodGet, "/api/projects/health/collection/export?format=jsonl", "secret-token")
	if exportResp.Code != http.StatusOK {
		t.Fatalf("collection export status = %d, body=%s", exportResp.Code, exportResp.Body.String())
	}
	if contentType := exportResp.Header().Get("Content-Type"); !strings.Contains(contentType, "application/x-ndjson") {
		t.Fatalf("collection export content-type = %q, want ndjson", contentType)
	}
	lines := strings.Split(strings.TrimSpace(exportResp.Body.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("export line count = %d, want 3; body=%s", len(lines), exportResp.Body.String())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first export line is not JSON: %v; line=%s", err, lines[0])
	}
	for _, key := range []string{
		"consumer_contract",
		"collection_id",
		"project_id",
		"target_system",
		"claim_id",
		"source_hash",
		"risk_tier",
		"decision",
		"allowed_uses",
		"blocked_uses",
		"human_loop",
		"audit_status",
	} {
		if _, ok := first[key]; !ok {
			t.Fatalf("first export line missing %q: %#v", key, first)
		}
	}
	if first["consumer_contract"] != "dedao_project_collection_jsonl_v1" ||
		first["project_id"] != "health" ||
		first["target_system"] != "health-llm-driven" ||
		first["claim_id"] != "verify-claim-study" {
		t.Fatalf("first export line has wrong identity fields: %#v", first)
	}
	if first["audit_status"] != "not_required" {
		t.Fatalf("first export line audit_status = %#v, want not_required", first["audit_status"])
	}
	if !strings.Contains(exportResp.Body.String(), `"audit_status":"pending_async_audit"`) ||
		!strings.Contains(exportResp.Body.String(), `"audit_reason":"health_sensitive_claim"`) ||
		!strings.Contains(exportResp.Body.String(), `"audit_reason":"missing_citation"`) {
		t.Fatalf("export missing async audit fields: %s", exportResp.Body.String())
	}

	badFormatResp := requestKBase(handler, http.MethodGet, "/api/projects/health/collection/export?format=json", "secret-token")
	if badFormatResp.Code != http.StatusBadRequest {
		t.Fatalf("bad format status = %d, want 400", badFormatResp.Code)
	}
}

func TestKBaseHTTPHandlerServesPageAnalysis(t *testing.T) {
	var gotTokenPlanAuth string
	var gotTokenPlanBody string
	tokenPlanServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTokenPlanAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/compatible-mode/v1/chat/completions" {
			t.Fatalf("TokenPlan path = %q", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		gotTokenPlanBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"当前页面重点是主动回忆。"}}]}`))
	}))
	defer tokenPlanServer.Close()

	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte(strings.Join([]string{
		"TOKENPLAN_API_KEY=sk-page-web-test",
		"TOKENPLAN_BASE_URL=" + tokenPlanServer.URL + "/compatible-mode/v1",
		"TOKENPLAN_MODEL=MiniMax-M2.5",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "")
	t.Setenv("DEDAO_TOKENPLAN_MODEL", "")
	t.Setenv("TOKENPLAN_API_KEY", "")
	t.Setenv("TOKENPLAN_BASE_URL", "")
	t.Setenv("TOKENPLAN_MODEL", "")
	t.Setenv("DEDAO_TOKENPLAN_ENV_FILE", envFile)

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
	})

	resp := requestKBaseJSON(handler, http.MethodPost, "/api/analyze-page", "secret-token", `{
		"source":"course",
		"title":"有效学习",
		"url":"/course/course-enid",
		"mode":"study",
		"question":"分析当前页面",
		"model":"qwen3.7-max",
		"context_sections":[
			{"title":"课程信息","content":"讲师: 王老师"},
			{"title":"当前文章","content":"主动回忆比重复阅读更有效。"}
		]
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("page analysis status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"answer":"当前页面重点是主动回忆。"`) ||
		!strings.Contains(resp.Body.String(), `"model":"qwen3.7-max"`) ||
		!strings.Contains(resp.Body.String(), `"source":"course"`) ||
		!strings.Contains(resp.Body.String(), `"context_stats"`) {
		t.Fatalf("page analysis response missing metadata: %s", resp.Body.String())
	}
	if gotTokenPlanAuth != "Bearer sk-page-web-test" {
		t.Fatalf("TokenPlan auth = %q, want fake bearer", gotTokenPlanAuth)
	}
	if !strings.Contains(gotTokenPlanBody, "有效学习") ||
		!strings.Contains(gotTokenPlanBody, "主动回忆比重复阅读更有效") {
		t.Fatalf("TokenPlan request missing page context: %s", gotTokenPlanBody)
	}
}

func TestKBaseHTTPHandlerServesJobs(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	listResp := requestKBase(handler, http.MethodGet, "/api/jobs", "secret-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("jobs list status = %d, body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), `"jobs"`) {
		t.Fatalf("jobs list response missing jobs: %s", listResp.Body.String())
	}

	createResp := requestKBaseJSON(handler, http.MethodPost, "/api/jobs", "secret-token", `{"type":"notebooklm_export","book_id":"42"}`)
	if createResp.Code != http.StatusAccepted {
		t.Fatalf("job create status = %d, body=%s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		Job BookKnowledgeJob `json:"job"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal create response returned error: %v", err)
	}
	if created.Job.ID == "" || created.Job.Type != "notebooklm_export" || created.Job.BookID != "42" {
		t.Fatalf("created job = %#v", created.Job)
	}

	var loaded BookKnowledgeJob
	for i := 0; i < 50; i++ {
		getResp := requestKBase(handler, http.MethodGet, "/api/jobs/"+created.Job.ID, "secret-token")
		if getResp.Code != http.StatusOK {
			t.Fatalf("job get status = %d, body=%s", getResp.Code, getResp.Body.String())
		}
		var payload struct {
			Job BookKnowledgeJob `json:"job"`
		}
		if err := json.Unmarshal(getResp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("Unmarshal get response returned error: %v", err)
		}
		loaded = payload.Job
		if loaded.Status == BookKnowledgeJobStatusSucceeded || loaded.Status == BookKnowledgeJobStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if loaded.Status != BookKnowledgeJobStatusSucceeded {
		t.Fatalf("job status = %s, error=%s, logs=%v", loaded.Status, loaded.Error, loaded.Logs)
	}
	if !strings.Contains(fmt.Sprint(loaded.Result), "notebooklm-prompt.md") {
		t.Fatalf("job result missing NotebookLM files: %#v", loaded.Result)
	}

	listResp = requestKBase(handler, http.MethodGet, "/api/jobs?limit=10", "secret-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("jobs list status after create = %d, body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), created.Job.ID) ||
		!strings.Contains(listResp.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("jobs list response missing completed job: %s", listResp.Body.String())
	}

	unauthorizedResp := requestKBaseJSON(handler, http.MethodPost, "/api/jobs", "", `{"type":"notebooklm_export","book_id":"42"}`)
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("job create without bearer = %d, want 401", unauthorizedResp.Code)
	}
}

func TestKBaseHTTPHandlerServesDedaoSession(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/session", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("session without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/session", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("session status = %d, body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"logged_in"`) || !strings.Contains(body, `"user_count"`) {
		t.Fatalf("session response missing safe status fields: %s", body)
	}
	if strings.Contains(strings.ToLower(body), "cookie") {
		t.Fatalf("session response must not expose cookies: %s", body)
	}
}

func TestKBaseHTTPHandlerServesDedaoAuth(t *testing.T) {
	auth := &fakeDedaoAuthProvider{
		qr: DedaoLoginQRCode{
			Token:        "login-token",
			QRCode:       "data:image/png;base64,abc",
			QRCodeString: "qr-string",
		},
		check: DedaoLoginCheck{
			Status: 1,
			User: &DedaoSessionUser{
				UIDHazy: "uid-1",
				Name:    "学习者",
				Avatar:  "https://example.test/avatar.png",
			},
			Session: DedaoSession{
				LoggedIn: true,
				ActiveUser: &DedaoSessionUser{
					UIDHazy: "uid-1",
					Name:    "学习者",
				},
				UserCount: 1,
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
		DedaoAuth: auth,
	})

	unauthorizedResp := requestKBase(handler, http.MethodPost, "/api/dedao/auth/qrcode", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("auth qrcode without bearer = %d, want 401", unauthorizedResp.Code)
	}

	qrResp := requestKBase(handler, http.MethodPost, "/api/dedao/auth/qrcode", "secret-token")
	if qrResp.Code != http.StatusOK {
		t.Fatalf("auth qrcode status = %d, body=%s", qrResp.Code, qrResp.Body.String())
	}
	if cacheControl := qrResp.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("auth qrcode Cache-Control = %q, want no-store", cacheControl)
	}
	qrBody := qrResp.Body.String()
	if !strings.Contains(qrBody, `"token":"login-token"`) ||
		!strings.Contains(qrBody, `"qr_code":"data:image/png;base64,abc"`) ||
		!strings.Contains(qrBody, `"qr_code_string":"qr-string"`) {
		t.Fatalf("qrcode response missing safe QR payload: %s", qrBody)
	}
	if strings.Contains(strings.ToLower(qrBody), "cookie") {
		t.Fatalf("qrcode response must not expose cookies: %s", qrBody)
	}

	checkResp := requestKBaseJSON(handler, http.MethodPost, "/api/dedao/auth/check", "secret-token", `{"token":"login-token","qr_code_string":"qr-string"}`)
	if checkResp.Code != http.StatusOK {
		t.Fatalf("auth check status = %d, body=%s", checkResp.Code, checkResp.Body.String())
	}
	if cacheControl := checkResp.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("auth check Cache-Control = %q, want no-store", cacheControl)
	}
	checkBody := checkResp.Body.String()
	if auth.gotToken != "login-token" || auth.gotQRCodeString != "qr-string" {
		t.Fatalf("auth check arguments = token %q qr %q", auth.gotToken, auth.gotQRCodeString)
	}
	if !strings.Contains(checkBody, `"status":1`) ||
		!strings.Contains(checkBody, `"uid_hazy":"uid-1"`) ||
		!strings.Contains(checkBody, `"session"`) {
		t.Fatalf("auth check response missing safe status/user/session: %s", checkBody)
	}
	if strings.Contains(strings.ToLower(checkBody), "cookie") {
		t.Fatalf("auth check response must not expose cookies: %s", checkBody)
	}

	badResp := requestKBaseJSON(handler, http.MethodPost, "/api/dedao/auth/check", "secret-token", `{"token":"","qr_code_string":""}`)
	if badResp.Code != http.StatusBadRequest {
		t.Fatalf("auth check missing token status = %d, want 400", badResp.Code)
	}
}

func TestKBaseHTTPHandlerServesDedaoEbooks(t *testing.T) {
	content := &fakeDedaoContentProvider{
		ebooks: DedaoEbookPage{
			Ebooks: []DedaoEbook{
				{
					ID:         67929,
					Enid:       "ebook-enid",
					Title:      "强化学习教程",
					Author:     "王琦",
					Intro:      "适合系统学习强化学习的电子书。",
					Icon:       "https://example.test/cover.jpg",
					Price:      "99.00",
					Progress:   42,
					PublishNum: 18,
					LastRead:   "第 3 章",
				},
			},
			Page:       2,
			PageSize:   1,
			Total:      3,
			TotalPages: 3,
			IsMore:     1,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/ebooks", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("ebooks without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/ebooks?page=2&page_size=1&q=强化学习", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("ebooks status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotPage != 2 || content.gotPageSize != 1 || content.gotQuery != "强化学习" {
		t.Fatalf("ebooks request args = page %d pageSize %d query %q", content.gotPage, content.gotPageSize, content.gotQuery)
	}
	var payload DedaoEbookPage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Page != 2 || payload.PageSize != 1 || payload.Total != 3 || payload.TotalPages != 3 || payload.IsMore != 1 {
		t.Fatalf("ebooks pagination = %#v", payload)
	}
	if len(payload.Ebooks) != 1 || payload.Ebooks[0].Title != "强化学习教程" || payload.Ebooks[0].Enid != "ebook-enid" {
		t.Fatalf("ebooks payload = %#v", payload.Ebooks)
	}
	body := strings.ToLower(resp.Body.String())
	for _, forbidden := range []string{"cookie", "drm_token"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("ebooks response must not expose %s: %s", forbidden, resp.Body.String())
		}
	}
}

func TestKBaseHTTPHandlerServesDedaoSiteEbookSearch(t *testing.T) {
	content := &fakeDedaoContentProvider{
		searchEbooks: DedaoEbookPage{
			Ebooks: []DedaoEbook{
				{
					ID:       32355,
					Enid:     "site-ebook-enid",
					Title:    "陆蓉行为金融学讲义",
					Author:   "陆蓉",
					Intro:    "聪明的投资者都该懂的实战智慧。",
					Icon:     "https://example.test/site-cover.jpg",
					Price:    "41.30",
					Progress: 0,
				},
			},
			Page:       3,
			PageSize:   7,
			Total:      54455,
			TotalPages: 7780,
			IsMore:     1,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/search/ebooks?q=金融", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("site ebook search without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/search/ebooks?page=3&page_size=7&q=金融", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("site ebook search status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotSearchEbookPage != 3 || content.gotSearchEbookPageSize != 7 || content.gotSearchEbookQuery != "金融" {
		t.Fatalf(
			"site ebook search request args = page %d pageSize %d query %q",
			content.gotSearchEbookPage,
			content.gotSearchEbookPageSize,
			content.gotSearchEbookQuery,
		)
	}
	var payload DedaoEbookPage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Page != 3 || payload.PageSize != 7 || payload.Total != 54455 || payload.TotalPages != 7780 || payload.IsMore != 1 {
		t.Fatalf("site ebook search pagination = %#v", payload)
	}
	if len(payload.Ebooks) != 1 || payload.Ebooks[0].Title != "陆蓉行为金融学讲义" || payload.Ebooks[0].Enid != "site-ebook-enid" {
		t.Fatalf("site ebook search payload = %#v", payload.Ebooks)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerAddsDedaoEbookToBookshelf(t *testing.T) {
	content := &fakeDedaoContentProvider{
		addEbook: DedaoEbook{
			ID:       32355,
			Enid:     "site-ebook-enid",
			Title:    "陆蓉行为金融学讲义",
			Author:   "陆蓉",
			Icon:     "https://example.test/site-cover.jpg",
			Price:    "41.30",
			Progress: 0,
			IsBuy:    true,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodPost, "/api/dedao/ebooks/site-ebook-enid/bookshelf", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("add ebook shelf without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodPost, "/api/dedao/ebooks/site-ebook-enid/bookshelf", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("add ebook shelf status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotAddEbookEnid != "site-ebook-enid" {
		t.Fatalf("add ebook shelf enid = %q, want site-ebook-enid", content.gotAddEbookEnid)
	}
	var payload DedaoEbook
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Enid != "site-ebook-enid" || payload.ID != 32355 || !payload.IsBuy {
		t.Fatalf("add ebook shelf payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoCourses(t *testing.T) {
	content := &fakeDedaoContentProvider{
		courses: DedaoCoursePage{
			Courses: []DedaoCourse{
				{
					ID:         101,
					ClassID:    202,
					Enid:       "course-enid",
					Title:      "商业分析课",
					Intro:      "从案例学习商业分析。",
					Icon:       "https://example.test/course.jpg",
					Price:      "199.00",
					Progress:   67,
					PublishNum: 36,
					CourseNum:  48,
					LastRead:   "第 12 讲",
				},
			},
			Page:       3,
			PageSize:   1,
			Total:      9,
			TotalPages: 9,
			IsMore:     1,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/courses", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("courses without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/courses?page=3&page_size=1&q=商业", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("courses status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotCourseCategory != CateCourse || content.gotCoursePage != 3 || content.gotCoursePageSize != 1 || content.gotCourseQuery != "商业" {
		t.Fatalf("courses request args = category %q page %d pageSize %d query %q", content.gotCourseCategory, content.gotCoursePage, content.gotCoursePageSize, content.gotCourseQuery)
	}
	var payload DedaoCoursePage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Page != 3 || payload.PageSize != 1 || payload.Total != 9 || payload.TotalPages != 9 || payload.IsMore != 1 {
		t.Fatalf("courses pagination = %#v", payload)
	}
	if len(payload.Courses) != 1 || payload.Courses[0].Title != "商业分析课" || payload.Courses[0].ClassID != 202 {
		t.Fatalf("courses payload = %#v", payload.Courses)
	}
	body := strings.ToLower(resp.Body.String())
	for _, forbidden := range []string{"cookie", "drm_token", "dd_url"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("courses response must not expose %s: %s", forbidden, resp.Body.String())
		}
	}
}

func TestKBaseHTTPHandlerServesDedaoCompassCourses(t *testing.T) {
	content := &fakeDedaoContentProvider{
		courses: DedaoCoursePage{
			Courses: []DedaoCourse{
				{
					ID:       801,
					ClassID:  901,
					Enid:     "compass-enid",
					Title:    "沟通锦囊",
					Intro:    "可直接使用的问题解决方案。",
					Icon:     "https://example.test/compass.jpg",
					Progress: 12,
				},
			},
			Page:       1,
			PageSize:   12,
			Total:      22,
			TotalPages: 2,
			IsMore:     1,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/courses?category=compass&page=1&page_size=12&q=沟通", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("compass courses status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotCourseCategory != CateAce || content.gotCoursePage != 1 || content.gotCoursePageSize != 12 || content.gotCourseQuery != "沟通" {
		t.Fatalf("compass request args = category %q page %d pageSize %d query %q", content.gotCourseCategory, content.gotCoursePage, content.gotCoursePageSize, content.gotCourseQuery)
	}
	var payload DedaoCoursePage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if len(payload.Courses) != 1 || payload.Courses[0].Title != "沟通锦囊" || payload.Total != 22 {
		t.Fatalf("compass payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoTopics(t *testing.T) {
	content := &fakeDedaoContentProvider{
		topics: DedaoTopicPage{
			Topics: []DedaoTopic{
				{
					TopicIDHazy: "topic-1",
					Name:        "AI学习",
					Intro:       "围绕 AI 工具和学习方法的讨论。",
					Img:         "https://example.test/topic.jpg",
					Tag:         2,
					ViewCount:   1200,
					NotesCount:  89,
					HasNewNotes: true,
				},
			},
			Page:     1,
			PageSize: 20,
			HasMore:  true,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/topics", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("topics without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/topics?page=1&page_size=20", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("topics status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotTopicPage != 1 || content.gotTopicPageSize != 20 {
		t.Fatalf("topics request args = page %d pageSize %d", content.gotTopicPage, content.gotTopicPageSize)
	}
	var payload DedaoTopicPage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if len(payload.Topics) != 1 || payload.Topics[0].Name != "AI学习" || !payload.HasMore {
		t.Fatalf("topics payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoTopicNotes(t *testing.T) {
	content := &fakeDedaoContentProvider{
		topicNotes: DedaoTopicNotePage{
			TopicIDHazy: "topic-1",
			Notes: []DedaoTopicNote{
				{
					NoteIDHazy:   "note-1",
					AuthorName:   "学习者",
					Avatar:       "https://example.test/avatar.jpg",
					TimeDesc:     "今天",
					Note:         "这条讨论很适合沉淀成学习卡片。",
					TopicName:    "AI学习",
					LikeCount:    23,
					CommentCount: 4,
				},
			},
			Page:      2,
			PageSize:  10,
			HasMore:   false,
			IsElected: false,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/topics/topic-1/notes?page=2&page_size=10&elected=false", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("topic notes status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotTopicNotesID != "topic-1" || content.gotTopicNotesPage != 2 || content.gotTopicNotesPageSize != 10 || content.gotTopicNotesElected {
		t.Fatalf("topic notes args = id %q elected %v page %d pageSize %d", content.gotTopicNotesID, content.gotTopicNotesElected, content.gotTopicNotesPage, content.gotTopicNotesPageSize)
	}
	var payload DedaoTopicNotePage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.TopicIDHazy != "topic-1" || len(payload.Notes) != 1 || payload.Notes[0].AuthorName != "学习者" {
		t.Fatalf("topic notes payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoOdobs(t *testing.T) {
	content := &fakeDedaoContentProvider{
		odobs: DedaoOdobPage{
			Odobs: []DedaoOdob{
				{
					ID:            301,
					ClassID:       301,
					Enid:          "odob-enid",
					Title:         "每天听本书",
					Intro:         "一本书的精华解读。",
					Author:        "得到听书",
					Icon:          "https://example.test/odob.jpg",
					Price:         "29.90",
					Progress:      55,
					Duration:      1800,
					AudioAliasID:  "audio-alias",
					AudioTitle:    "听书音频",
					AudioPlayURL:  "https://example.test/audio.mp3",
					HasPlayAuth:   true,
					AudioDuration: 1800,
				},
			},
			Page:       2,
			PageSize:   1,
			Total:      4,
			TotalPages: 4,
			IsMore:     1,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/odobs", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("odobs without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/odobs?page=2&page_size=1&q=听书", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("odobs status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotOdobPage != 2 || content.gotOdobPageSize != 1 || content.gotOdobQuery != "听书" {
		t.Fatalf("odobs request args = page %d pageSize %d query %q", content.gotOdobPage, content.gotOdobPageSize, content.gotOdobQuery)
	}
	var payload DedaoOdobPage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Page != 2 || payload.PageSize != 1 || payload.Total != 4 || payload.TotalPages != 4 || payload.IsMore != 1 {
		t.Fatalf("odobs pagination = %#v", payload)
	}
	if len(payload.Odobs) != 1 || payload.Odobs[0].Title != "每天听本书" || payload.Odobs[0].AudioAliasID != "audio-alias" {
		t.Fatalf("odobs payload = %#v", payload.Odobs)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoOdobDetail(t *testing.T) {
	content := &fakeDedaoContentProvider{
		odobDetail: DedaoOdobDetail{
			Enid:           "odob-enid",
			ID:             301,
			Title:          "每天听本书",
			Icon:           "https://example.test/odob.jpg",
			Duration:       1800,
			AudioPrice:     "29.90",
			AudioSummary:   "一本书的精华解读。",
			IsBuy:          true,
			InBookrack:     true,
			Progress:       55,
			Tags:           []string{"商业", "认知"},
			LearnCountDesc: "1.2 万人学习",
			Agency: DedaoOdobAgency{
				Name:       "得到听书",
				MemberName: "讲书人",
			},
			TopicSummary: []DedaoOdobTopicSummary{
				{Title: "核心观点", SubTitle: "三条主线"},
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/odobs/odob-enid", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("odob detail status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotOdobDetailEnid != "odob-enid" {
		t.Fatalf("odob detail enid = %q", content.gotOdobDetailEnid)
	}
	var payload DedaoOdobDetail
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Title != "每天听本书" || payload.Agency.Name != "得到听书" || len(payload.TopicSummary) != 1 {
		t.Fatalf("odob detail payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoCourseDetail(t *testing.T) {
	content := &fakeDedaoContentProvider{
		courseDetail: DedaoCourseDetail{
			Course: DedaoCourseDetailMeta{
				Enid:          "course-enid",
				ID:            101,
				Title:         "商业分析课",
				Intro:         "从案例学习商业分析。",
				Highlight:     "建立结构化分析框架。",
				LecturerName:  "张三",
				LecturerTitle: "商业顾问",
				Logo:          "https://example.test/course.jpg",
				ArticleCount:  36,
			},
			Articles: []DedaoArticle{
				{
					Enid:        "article-enid",
					ID:          501,
					Title:       "第一讲：问题定义",
					Summary:     "先定义问题，再选择工具。",
					PublishTime: 1710000000,
					IsRead:      true,
					OrderNum:    1,
				},
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/courses/course-enid", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("course detail without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/courses/course-enid", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("course detail status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotCourseDetailEnid != "course-enid" {
		t.Fatalf("course detail enid = %q", content.gotCourseDetailEnid)
	}
	var payload DedaoCourseDetail
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Course.Title != "商业分析课" || len(payload.Articles) != 1 || payload.Articles[0].Title != "第一讲：问题定义" {
		t.Fatalf("course detail payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoCourseArticles(t *testing.T) {
	content := &fakeDedaoContentProvider{
		articles: DedaoArticlePage{
			Articles: []DedaoArticle{
				{
					Enid:        "article-enid",
					ID:          501,
					Title:       "第一讲：问题定义",
					Summary:     "先定义问题，再选择工具。",
					PublishTime: 1710000000,
					OrderNum:    1,
				},
			},
			Count: 2,
			MaxID: 10,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/courses/course-enid/articles?count=2&max_id=10", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("course articles status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotArticlesEnid != "course-enid" || content.gotArticlesCount != 2 || content.gotArticlesMaxID != 10 {
		t.Fatalf("course articles args = enid %q count %d maxID %d", content.gotArticlesEnid, content.gotArticlesCount, content.gotArticlesMaxID)
	}
	var payload DedaoArticlePage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Count != 2 || payload.MaxID != 10 || len(payload.Articles) != 1 || payload.Articles[0].Enid != "article-enid" {
		t.Fatalf("course articles payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoArticleMarkdown(t *testing.T) {
	content := &fakeDedaoContentProvider{
		articleMarkdown: DedaoArticleMarkdown{
			Enid:     "article-enid",
			Type:     "course",
			Title:    "第一讲：问题定义",
			Markdown: "## 问题定义\n\n先定义问题，再选择工具。",
		},
		odobMarkdown: DedaoArticleMarkdown{
			Enid:     "audio-alias",
			Type:     "odob",
			Title:    "每天听本书",
			Markdown: "## 听书文稿\n\n一本书的精华解读。",
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/articles/article-enid?type=course", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("article markdown status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotArticleMarkdownEnid != "article-enid" {
		t.Fatalf("article markdown enid = %q", content.gotArticleMarkdownEnid)
	}
	var payload DedaoArticleMarkdown
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Markdown == "" || !strings.Contains(payload.Markdown, "问题定义") {
		t.Fatalf("article markdown payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())

	odobResp := requestKBase(handler, http.MethodGet, "/api/dedao/articles/audio-alias?type=odob", "secret-token")
	if odobResp.Code != http.StatusOK {
		t.Fatalf("odob markdown status = %d, body=%s", odobResp.Code, odobResp.Body.String())
	}
	if content.gotOdobMarkdownEnid != "audio-alias" {
		t.Fatalf("odob markdown enid = %q", content.gotOdobMarkdownEnid)
	}
	var odobPayload DedaoArticleMarkdown
	if err := json.Unmarshal(odobResp.Body.Bytes(), &odobPayload); err != nil {
		t.Fatalf("Unmarshal odob returned error: %v", err)
	}
	if odobPayload.Type != "odob" || !strings.Contains(odobPayload.Markdown, "听书文稿") {
		t.Fatalf("odob markdown payload = %#v", odobPayload)
	}
	assertDedaoResponseOmitsSecrets(t, odobResp.Body.String())

	badTypeResp := requestKBase(handler, http.MethodGet, "/api/dedao/articles/article-enid?type=video", "secret-token")
	if badTypeResp.Code != http.StatusBadRequest {
		t.Fatalf("article unsupported type status = %d, want 400", badTypeResp.Code)
	}
}

func TestKBaseHTTPHandlerServesDedaoEbookDetail(t *testing.T) {
	content := &fakeDedaoContentProvider{
		ebookDetail: DedaoEbookDetail{
			Enid:           "ebook-enid",
			ID:             67929,
			Title:          "强化学习教程",
			OperatingTitle: "强化学习教程",
			Cover:          "https://example.test/cover.jpg",
			BookAuthor:     "王琦",
			AuthorInfo:     "长期研究强化学习。",
			BookIntro:      "一本系统学习强化学习的教材。",
			PressName:      "测试出版社",
			ClassifyName:   "计算机",
			ReadTime:       12,
			Catalog: []DedaoEbookCatalogItem{
				{
					Level:     1,
					Text:      "第一章 导论",
					Href:      "chapter-1#0",
					ChapterID: "chapter-1",
					PlayOrder: 1,
				},
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/ebooks/ebook-enid", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("ebook detail status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotEbookDetailEnid != "ebook-enid" {
		t.Fatalf("ebook detail enid = %q", content.gotEbookDetailEnid)
	}
	var payload DedaoEbookDetail
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Title != "强化学习教程" || len(payload.Catalog) != 1 || payload.Catalog[0].ChapterID != "chapter-1" {
		t.Fatalf("ebook detail payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoEbookChapterPages(t *testing.T) {
	content := &fakeDedaoContentProvider{
		ebookPages: DedaoEbookChapterPages{
			Enid:      "ebook-enid",
			ChapterID: "chapter-1",
			Index:     4,
			Count:     8,
			Offset:    2,
			IsEnd:     true,
			Pages: []DedaoEbookPageSVG{
				{
					PageNum:     5,
					BeginOffset: 2,
					EndOffset:   88,
					SVG:         `<svg xmlns="http://www.w3.org/2000/svg"><text>第一章</text></svg>`,
				},
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/ebooks/ebook-enid/chapters/chapter-1/pages?index=4&count=99&offset=2", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("ebook pages status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotEbookPagesEnid != "ebook-enid" || content.gotEbookPagesChapterID != "chapter-1" ||
		content.gotEbookPagesIndex != 4 || content.gotEbookPagesCount != 8 || content.gotEbookPagesOffset != 2 {
		t.Fatalf(
			"ebook pages args = enid %q chapter %q index %d count %d offset %d",
			content.gotEbookPagesEnid,
			content.gotEbookPagesChapterID,
			content.gotEbookPagesIndex,
			content.gotEbookPagesCount,
			content.gotEbookPagesOffset,
		)
	}
	var payload DedaoEbookChapterPages
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Count != 8 || len(payload.Pages) != 1 || !strings.Contains(payload.Pages[0].SVG, "<svg") {
		t.Fatalf("ebook pages payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesWebAssets(t *testing.T) {
	root := t.TempDir()
	webDir := filepath.Join(root, "web")
	assetDir := filepath.Join(webDir, "assets")
	if err := os.MkdirAll(assetDir, os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<main id=\"app\">kbase web</main>"), 0o644); err != nil {
		t.Fatalf("WriteFile index returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "app.js"), []byte("console.log('kbase')"), 0o644); err != nil {
		t.Fatalf("WriteFile asset returned error: %v", err)
	}

	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		StaticDir: webDir,
	})

	indexResp := requestKBase(handler, http.MethodGet, "/", "")
	if indexResp.Code != http.StatusOK {
		t.Fatalf("index status = %d, body=%s", indexResp.Code, indexResp.Body.String())
	}
	if !strings.Contains(indexResp.Body.String(), "kbase web") {
		t.Fatalf("index response missing app shell: %s", indexResp.Body.String())
	}

	assetResp := requestKBase(handler, http.MethodGet, "/assets/app.js", "")
	if assetResp.Code != http.StatusOK {
		t.Fatalf("asset status = %d, body=%s", assetResp.Code, assetResp.Body.String())
	}
	if !strings.Contains(assetResp.Body.String(), "console.log") {
		t.Fatalf("asset response missing js: %s", assetResp.Body.String())
	}

	fallbackResp := requestKBase(handler, http.MethodGet, "/books/42", "")
	if fallbackResp.Code != http.StatusOK {
		t.Fatalf("fallback status = %d, body=%s", fallbackResp.Code, fallbackResp.Body.String())
	}
	if !strings.Contains(fallbackResp.Body.String(), "kbase web") {
		t.Fatalf("fallback response missing index: %s", fallbackResp.Body.String())
	}

	apiResp := requestKBase(handler, http.MethodGet, "/api/books", "")
	if apiResp.Code != http.StatusUnauthorized {
		t.Fatalf("api status without token = %d, want 401", apiResp.Code)
	}
}

func TestKBaseHTTPHandlerServesBrowserSessionTokenOnlyFromTrustedProxy(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
	})

	directResp := requestKBase(handler, http.MethodGet, "/browser/session-token", "")
	if directResp.Code != http.StatusNotFound {
		t.Fatalf("direct browser token status = %d, want 404", directResp.Code)
	}

	proxyResp := requestKBaseWithHeaders(handler, http.MethodGet, "/browser/session-token", "", "", map[string]string{
		"X-KBase-Browser-Session": "1",
	})
	if proxyResp.Code != http.StatusOK {
		t.Fatalf("proxy browser token status = %d, body=%s", proxyResp.Code, proxyResp.Body.String())
	}
	if !strings.Contains(proxyResp.Body.String(), `"token":"secret-token"`) {
		t.Fatalf("browser token response missing token: %s", proxyResp.Body.String())
	}
	if cacheControl := proxyResp.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}

	postResp := requestKBaseWithHeaders(handler, http.MethodPost, "/browser/session-token", "", "", map[string]string{
		"X-KBase-Browser-Session": "1",
	})
	if postResp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("browser token POST status = %d, want 405", postResp.Code)
	}
}

func TestKBaseHTTPHandlerServesSkillDiscoveryWithoutBearer(t *testing.T) {
	root := t.TempDir()
	handler := newTestKBaseHandlerWithSystemKB(t, root)

	discoveryResp := requestKBase(handler, http.MethodGet, "/.well-known/dedao-kbase-skills.json", "")
	if discoveryResp.Code != http.StatusOK {
		t.Fatalf("discovery status = %d, body=%s", discoveryResp.Code, discoveryResp.Body.String())
	}
	if !strings.Contains(discoveryResp.Body.String(), `"dedao.book.search"`) ||
		!strings.Contains(discoveryResp.Body.String(), `"/api/skills/dedao.book.search/manifest.json"`) {
		t.Fatalf("discovery response missing skill metadata: %s", discoveryResp.Body.String())
	}

	listResp := requestKBase(handler, http.MethodGet, "/api/skills", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("skills list status = %d, body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), `"dedao.system_kb.export"`) {
		t.Fatalf("skills list missing system kb export skill: %s", listResp.Body.String())
	}

	manifestResp := requestKBase(handler, http.MethodGet, "/api/skills/dedao.book.search/manifest.json", "")
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, body=%s", manifestResp.Code, manifestResp.Body.String())
	}
	if !strings.Contains(manifestResp.Body.String(), `"invoke_url"`) ||
		!strings.Contains(manifestResp.Body.String(), `"bearer"`) {
		t.Fatalf("manifest response missing invocation contract: %s", manifestResp.Body.String())
	}

	openAPIResp := requestKBase(handler, http.MethodGet, "/api/skills/dedao.book.search/openapi.json", "")
	if openAPIResp.Code != http.StatusOK {
		t.Fatalf("openapi status = %d, body=%s", openAPIResp.Code, openAPIResp.Body.String())
	}
	if !strings.Contains(openAPIResp.Body.String(), `"/api/skills/dedao.book.search/invoke"`) {
		t.Fatalf("openapi response missing invoke path: %s", openAPIResp.Body.String())
	}

	skillDocResp := requestKBase(handler, http.MethodGet, "/api/skills/dedao.book.search/SKILL.md", "")
	if skillDocResp.Code != http.StatusOK {
		t.Fatalf("skill doc status = %d, body=%s", skillDocResp.Code, skillDocResp.Body.String())
	}
	if contentType := skillDocResp.Header().Get("Content-Type"); !strings.Contains(contentType, "text/markdown") {
		t.Fatalf("skill doc content-type = %q, want text/markdown", contentType)
	}
	if !strings.Contains(skillDocResp.Body.String(), "Authorization: Bearer") {
		t.Fatalf("skill doc should describe bearer token usage: %s", skillDocResp.Body.String())
	}

	invokeResp := requestKBaseJSON(handler, http.MethodPost, "/api/skills/dedao.book.search/invoke", "", `{"arguments":{"query":"MACD"}}`)
	if invokeResp.Code != http.StatusUnauthorized {
		t.Fatalf("invoke status without token = %d, want 401", invokeResp.Code)
	}
}

func TestKBaseHTTPHandlerInvokesSkillsWithBearer(t *testing.T) {
	root := t.TempDir()
	handler := newTestKBaseHandlerWithSystemKB(t, root)

	searchResp := requestKBaseJSON(handler, http.MethodPost, "/api/skills/dedao.book.search/invoke", "secret-token", `{"arguments":{"query":"MACD","limit":5}}`)
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search invoke status = %d, body=%s", searchResp.Code, searchResp.Body.String())
	}
	if !strings.Contains(searchResp.Body.String(), `"skill":"dedao.book.search"`) ||
		!strings.Contains(searchResp.Body.String(), `"book_id":"42"`) {
		t.Fatalf("search invoke response missing result: %s", searchResp.Body.String())
	}

	contextResp := requestKBaseJSON(handler, http.MethodPost, "/api/skills/dedao.book.get_context/invoke", "secret-token", `{"arguments":{"book_id":"42"}}`)
	if contextResp.Code != http.StatusOK {
		t.Fatalf("context invoke status = %d, body=%s", contextResp.Code, contextResp.Body.String())
	}
	if !strings.Contains(contextResp.Body.String(), `"chapters"`) ||
		!strings.Contains(contextResp.Body.String(), `"claims"`) {
		t.Fatalf("context invoke response missing package context: %s", contextResp.Body.String())
	}

	manifestResp := requestKBaseJSON(handler, http.MethodPost, "/api/skills/dedao.system_kb.manifest/invoke", "secret-token", `{}`)
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest invoke status = %d, body=%s", manifestResp.Code, manifestResp.Body.String())
	}
	if !strings.Contains(manifestResp.Body.String(), `"version":"test-version"`) {
		t.Fatalf("manifest invoke response missing version: %s", manifestResp.Body.String())
	}

	unknownResp := requestKBase(handler, http.MethodGet, "/api/skills/unknown.skill/manifest.json", "")
	if unknownResp.Code != http.StatusNotFound {
		t.Fatalf("unknown skill status = %d, want 404", unknownResp.Code)
	}
}

type fakeDedaoAuthProvider struct {
	qr              DedaoLoginQRCode
	check           DedaoLoginCheck
	gotToken        string
	gotQRCodeString string
}

func (f *fakeDedaoAuthProvider) NewQRCode() (DedaoLoginQRCode, error) {
	return f.qr, nil
}

func (f *fakeDedaoAuthProvider) CheckLogin(token string, qrCodeString string) (DedaoLoginCheck, error) {
	f.gotToken = token
	f.gotQRCodeString = qrCodeString
	return f.check, nil
}

type fakeDedaoContentProvider struct {
	ebooks                 DedaoEbookPage
	searchEbooks           DedaoEbookPage
	courses                DedaoCoursePage
	topics                 DedaoTopicPage
	topicNotes             DedaoTopicNotePage
	odobs                  DedaoOdobPage
	odobDetail             DedaoOdobDetail
	courseDetail           DedaoCourseDetail
	articles               DedaoArticlePage
	articleMarkdown        DedaoArticleMarkdown
	odobMarkdown           DedaoArticleMarkdown
	ebookDetail            DedaoEbookDetail
	addEbook               DedaoEbook
	ebookPages             DedaoEbookChapterPages
	gotQuery               string
	gotPage                int
	gotPageSize            int
	gotSearchEbookQuery    string
	gotSearchEbookPage     int
	gotSearchEbookPageSize int
	gotCourseQuery         string
	gotCourseCategory      string
	gotCoursePage          int
	gotCoursePageSize      int
	gotTopicPage           int
	gotTopicPageSize       int
	gotTopicNotesID        string
	gotTopicNotesElected   bool
	gotTopicNotesPage      int
	gotTopicNotesPageSize  int
	gotOdobQuery           string
	gotOdobPage            int
	gotOdobPageSize        int
	gotOdobDetailEnid      string
	gotCourseDetailEnid    string
	gotArticlesEnid        string
	gotArticlesCount       int
	gotArticlesMaxID       int
	gotArticleMarkdownEnid string
	gotOdobMarkdownEnid    string
	gotEbookDetailEnid     string
	gotAddEbookEnid        string
	gotEbookPagesEnid      string
	gotEbookPagesChapterID string
	gotEbookPagesIndex     int
	gotEbookPagesCount     int
	gotEbookPagesOffset    int
}

func (f *fakeDedaoContentProvider) ListEbooks(query string, page, pageSize int) (DedaoEbookPage, error) {
	f.gotQuery = query
	f.gotPage = page
	f.gotPageSize = pageSize
	return f.ebooks, nil
}

func (f *fakeDedaoContentProvider) SearchEbooks(query string, page, pageSize int) (DedaoEbookPage, error) {
	f.gotSearchEbookQuery = query
	f.gotSearchEbookPage = page
	f.gotSearchEbookPageSize = pageSize
	return f.searchEbooks, nil
}

func (f *fakeDedaoContentProvider) ListCourses(query string, page, pageSize int) (DedaoCoursePage, error) {
	return f.ListCoursesByCategory(CateCourse, query, page, pageSize)
}

func (f *fakeDedaoContentProvider) ListCoursesByCategory(category string, query string, page, pageSize int) (DedaoCoursePage, error) {
	f.gotCourseCategory = category
	f.gotCourseQuery = query
	f.gotCoursePage = page
	f.gotCoursePageSize = pageSize
	return f.courses, nil
}

func (f *fakeDedaoContentProvider) ListTopics(page, pageSize int) (DedaoTopicPage, error) {
	f.gotTopicPage = page
	f.gotTopicPageSize = pageSize
	return f.topics, nil
}

func (f *fakeDedaoContentProvider) ListTopicNotes(topicID string, isElected bool, page, pageSize int) (DedaoTopicNotePage, error) {
	f.gotTopicNotesID = topicID
	f.gotTopicNotesElected = isElected
	f.gotTopicNotesPage = page
	f.gotTopicNotesPageSize = pageSize
	return f.topicNotes, nil
}

func (f *fakeDedaoContentProvider) ListOdobs(query string, page, pageSize int) (DedaoOdobPage, error) {
	f.gotOdobQuery = query
	f.gotOdobPage = page
	f.gotOdobPageSize = pageSize
	return f.odobs, nil
}

func (f *fakeDedaoContentProvider) GetOdobDetail(enid string) (DedaoOdobDetail, error) {
	f.gotOdobDetailEnid = enid
	return f.odobDetail, nil
}

func (f *fakeDedaoContentProvider) GetCourseDetail(enid string) (DedaoCourseDetail, error) {
	f.gotCourseDetailEnid = enid
	return f.courseDetail, nil
}

func (f *fakeDedaoContentProvider) ListCourseArticles(enid string, count, maxID int) (DedaoArticlePage, error) {
	f.gotArticlesEnid = enid
	f.gotArticlesCount = count
	f.gotArticlesMaxID = maxID
	return f.articles, nil
}

func (f *fakeDedaoContentProvider) GetCourseArticleMarkdown(enid string) (DedaoArticleMarkdown, error) {
	f.gotArticleMarkdownEnid = enid
	return f.articleMarkdown, nil
}

func (f *fakeDedaoContentProvider) GetOdobArticleMarkdown(enid string) (DedaoArticleMarkdown, error) {
	f.gotOdobMarkdownEnid = enid
	return f.odobMarkdown, nil
}

func (f *fakeDedaoContentProvider) GetEbookDetail(enid string) (DedaoEbookDetail, error) {
	f.gotEbookDetailEnid = enid
	return f.ebookDetail, nil
}

func (f *fakeDedaoContentProvider) AddEbookToBookshelf(enid string) (DedaoEbook, error) {
	f.gotAddEbookEnid = enid
	return f.addEbook, nil
}

func (f *fakeDedaoContentProvider) GetEbookChapterPages(enid string, chapterID string, index, count, offset int) (DedaoEbookChapterPages, error) {
	f.gotEbookPagesEnid = enid
	f.gotEbookPagesChapterID = chapterID
	f.gotEbookPagesIndex = index
	f.gotEbookPagesCount = count
	f.gotEbookPagesOffset = offset
	return f.ebookPages, nil
}

func assertDedaoResponseOmitsSecrets(t *testing.T, body string) {
	t.Helper()
	lowerBody := strings.ToLower(body)
	for _, forbidden := range []string{"cookie", "token", "drm_token", "dd_url", "dd_article_token"} {
		if strings.Contains(lowerBody, forbidden) {
			t.Fatalf("dedao response must not expose %s: %s", forbidden, body)
		}
	}
}

func newTestKBaseHandlerWithSystemKB(t *testing.T, root string) http.Handler {
	t.Helper()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	exportPath := filepath.Join(root, "artifacts", "system_kb_export.json")
	if err := os.MkdirAll(filepath.Dir(exportPath), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	exportPayload := map[string]any{
		"type":        "system_kb_v2_export",
		"schema_id":   "llm-wiki-v2-system-kb-export",
		"version":     "test-version",
		"source":      "dedao-kbase",
		"compiled_at": "2026-06-27T10:00:00Z",
		"stats":       map[string]any{"claim_count": 1},
		"pages":       []any{},
		"entities":    []any{},
		"claims":      []any{},
		"relations":   []any{},
	}
	data, err := json.Marshal(exportPayload)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if err := os.WriteFile(exportPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:              store,
		AuthToken:          "secret-token",
		SystemKBExportPath: exportPath,
	})
}

func sampleBookKnowledgePackageWithID(bookID, title string) BookKnowledgePackage {
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = bookID
	pkg.Book.Title = title
	pkg.Book.UpdatedAt = fmt.Sprintf("2026-06-28T00:00:0%sZ", strings.TrimPrefix(bookID, "book-"))
	for i := range pkg.Chapters {
		pkg.Chapters[i].BookID = bookID
		pkg.Chapters[i].ChapterID = bookID + "-chapter-1"
	}
	for i := range pkg.Chunks {
		pkg.Chunks[i].BookID = bookID
		pkg.Chunks[i].ChapterID = bookID + "-chapter-1"
		pkg.Chunks[i].ChunkID = bookID + "-chunk-1"
	}
	for i := range pkg.Claims {
		pkg.Claims[i].BookID = bookID
		pkg.Claims[i].ChapterID = bookID + "-chapter-1"
		pkg.Claims[i].ClaimID = bookID + "-claim-1"
	}
	for i := range pkg.Citations {
		pkg.Citations[i].BookID = bookID
		pkg.Citations[i].ChapterID = bookID + "-chapter-1"
		pkg.Citations[i].ChunkID = bookID + "-chunk-1"
		pkg.Citations[i].CitationID = bookID + "-citation-1"
	}
	return pkg
}

func requestKBase(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	return requestKBaseJSON(handler, method, path, token, "")
}

func requestKBaseJSON(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	return requestKBaseWithHeaders(handler, method, path, token, body, nil)
}

func requestKBaseWithHeaders(handler http.Handler, method, path, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}
