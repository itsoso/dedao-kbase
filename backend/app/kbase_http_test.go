package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func requestKBase(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	return requestKBaseJSON(handler, method, path, token, "")
}

func requestKBaseJSON(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}
