package app

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type KBaseHTTPConfig struct {
	Store              *BookKnowledgeStore
	AuthToken          string
	SystemKBExportPath string
	StaticDir          string
}

type kbaseHTTPHandler struct {
	store              *BookKnowledgeStore
	authToken          string
	systemKBExportPath string
	staticDir          string
}

func NewKBaseHTTPHandler(cfg KBaseHTTPConfig) http.Handler {
	store := cfg.Store
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	return &kbaseHTTPHandler{
		store:              store,
		authToken:          strings.TrimSpace(cfg.AuthToken),
		systemKBExportPath: strings.TrimSpace(cfg.SystemKBExportPath),
		staticDir:          strings.TrimSpace(cfg.StaticDir),
	}
}

func (h *kbaseHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		writeHTTPJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"service": "dedao-kbase",
		})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		h.serveStatic(w, r)
		return
	}
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch {
	case r.URL.Path == "/api/books":
		h.handleListBooks(w)
	case strings.HasPrefix(r.URL.Path, "/api/books/"):
		h.handleGetBook(w, r)
	case r.URL.Path == "/api/search":
		h.handleSearch(w, r)
	case r.URL.Path == "/api/system-kb/export":
		h.handleSystemKBExport(w)
	case r.URL.Path == "/api/system-kb/manifest":
		h.handleSystemKBManifest(w)
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
	}
}

func (h *kbaseHTTPHandler) serveStatic(w http.ResponseWriter, r *http.Request) {
	if h.staticDir == "" {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cleanURLPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	relativePath := strings.TrimPrefix(cleanURLPath, "/")
	if relativePath == "" {
		relativePath = "index.html"
	}
	candidate := filepath.Join(h.staticDir, filepath.FromSlash(relativePath))
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		http.ServeFile(w, r, candidate)
		return
	}

	indexPath := filepath.Join(h.staticDir, "index.html")
	if info, err := os.Stat(indexPath); err != nil || info.IsDir() {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	http.ServeFile(w, r, indexPath)
}

func (h *kbaseHTTPHandler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.authToken == "" {
		writeHTTPError(w, http.StatusUnauthorized, "kbase auth token is not configured")
		return false
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	token := strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	if token == value || subtle.ConstantTimeCompare([]byte(token), []byte(h.authToken)) != 1 {
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (h *kbaseHTTPHandler) handleListBooks(w http.ResponseWriter) {
	books, err := h.store.ListBooks()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"books": books})
}

func (h *kbaseHTTPHandler) handleGetBook(w http.ResponseWriter, r *http.Request) {
	bookID, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/books/"))
	if err != nil || strings.TrimSpace(bookID) == "" {
		writeHTTPError(w, http.StatusBadRequest, "book_id is required")
		return
	}
	pkg, err := h.store.LoadPackage(bookID)
	if err != nil {
		writeHTTPError(w, http.StatusNotFound, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, pkg)
}

func (h *kbaseHTTPHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeHTTPError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		if parsed > 0 {
			limit = parsed
		}
	}
	results, err := h.store.Search(BookKnowledgeSearchQuery{
		Query:  r.URL.Query().Get("q"),
		BookID: r.URL.Query().Get("book_id"),
		Limit:  limit,
	})
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (h *kbaseHTTPHandler) handleSystemKBExport(w http.ResponseWriter) {
	payload, err := h.readSystemKBExport()
	if err != nil {
		writeHTTPError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (h *kbaseHTTPHandler) handleSystemKBManifest(w http.ResponseWriter) {
	payload, err := h.readSystemKBExport()
	if err != nil {
		writeHTTPError(w, http.StatusNotFound, err.Error())
		return
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("invalid system kb export: %v", err))
		return
	}
	manifest := map[string]any{}
	for _, key := range []string{
		"id", "type", "schema_id", "version", "source", "source_repo",
		"source_commit", "compiled_at", "license_scope", "stats",
	} {
		if value, ok := decoded[key]; ok {
			manifest[key] = value
		}
	}
	writeHTTPJSON(w, http.StatusOK, manifest)
}

func (h *kbaseHTTPHandler) readSystemKBExport() ([]byte, error) {
	if h.systemKBExportPath == "" {
		return nil, fmt.Errorf("system kb export path is not configured")
	}
	payload, err := os.ReadFile(h.systemKBExportPath)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func writeHTTPJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func writeHTTPError(w http.ResponseWriter, status int, message string) {
	writeHTTPJSON(w, status, map[string]any{"error": message})
}
