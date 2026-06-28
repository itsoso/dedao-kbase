package app

import (
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

func requestKBase(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}
