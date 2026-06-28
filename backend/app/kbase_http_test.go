package app

import (
	"bytes"
	"encoding/json"
	"fmt"
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
