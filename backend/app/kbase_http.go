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
	"sort"
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
	if r.URL.Path == "/browser/session-token" {
		h.handleBrowserSessionToken(w, r)
		return
	}
	if r.URL.Path == "/.well-known/dedao-kbase-skills.json" {
		h.handleSkillsDiscovery(w, r)
		return
	}
	if r.URL.Path == "/api/skills" || strings.HasPrefix(r.URL.Path, "/api/skills/") {
		h.handleSkillRoute(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		h.serveStatic(w, r)
		return
	}
	if !h.authorize(w, r) {
		return
	}

	switch {
	case r.URL.Path == "/api/books":
		if !requireHTTPMethod(w, r, http.MethodGet) {
			return
		}
		h.handleListBooks(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/books/") && strings.HasSuffix(r.URL.Path, "/prompts"):
		if !requireHTTPMethod(w, r, http.MethodGet) {
			return
		}
		h.handleBookPrompts(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/books/") && strings.HasSuffix(r.URL.Path, "/chat-history"):
		if !requireHTTPMethod(w, r, http.MethodGet) {
			return
		}
		h.handleBookChatHistory(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/books/") && strings.HasSuffix(r.URL.Path, "/chat"):
		if !requireHTTPMethod(w, r, http.MethodPost) {
			return
		}
		h.handleBookChat(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/books/"):
		if !requireHTTPMethod(w, r, http.MethodGet) {
			return
		}
		h.handleGetBook(w, r)
	case r.URL.Path == "/api/search":
		if !requireHTTPMethod(w, r, http.MethodGet) {
			return
		}
		h.handleSearch(w, r)
	case r.URL.Path == "/api/system-kb/export":
		if !requireHTTPMethod(w, r, http.MethodGet) {
			return
		}
		h.handleSystemKBExport(w)
	case r.URL.Path == "/api/system-kb/manifest":
		if !requireHTTPMethod(w, r, http.MethodGet) {
			return
		}
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

func (h *kbaseHTTPHandler) handleBrowserSessionToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.TrimSpace(r.Header.Get("X-KBase-Browser-Session")) != "1" || h.authToken == "" {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"token": h.authToken,
	})
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

func (h *kbaseHTTPHandler) handleListBooks(w http.ResponseWriter, r *http.Request) {
	books, err := h.store.ListBooks()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	books = filterKBaseBooks(books, r.URL.Query().Get("q"))
	sortKBaseBooks(books, r.URL.Query().Get("sort"))
	page, pageSize := parseKBasePagination(r)
	total := len(books)
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"books":       books[start:end],
		"page":        page,
		"page_size":   pageSize,
		"total":       total,
		"total_pages": totalPages,
	})
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

func (h *kbaseHTTPHandler) handleBookPrompts(w http.ResponseWriter, r *http.Request) {
	bookID, err := bookIDFromAPIBookSubroute(r.URL.Path, "/prompts")
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	prompts, err := GenerateBookKnowledgePrompts(h.store, bookID)
	if err != nil {
		writeHTTPError(w, http.StatusNotFound, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"prompts": prompts})
}

func (h *kbaseHTTPHandler) handleBookChat(w http.ResponseWriter, r *http.Request) {
	bookID, err := bookIDFromAPIBookSubroute(r.URL.Path, "/chat")
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	var request BookKnowledgeChatRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.BookID = bookID
	response, err := BookKnowledgeChat(r.Context(), h.store, request)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, response)
}

func (h *kbaseHTTPHandler) handleBookChatHistory(w http.ResponseWriter, r *http.Request) {
	bookID, err := bookIDFromAPIBookSubroute(r.URL.Path, "/chat-history")
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit := 50
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
	history, err := h.store.ListChatHistory(bookID, limit)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"history": history})
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

func parseKBasePagination(r *http.Request) (int, int) {
	page := 1
	pageSize := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func filterKBaseBooks(books []BookKnowledgeBook, query string) []BookKnowledgeBook {
	term := strings.ToLower(strings.TrimSpace(query))
	if term == "" {
		return books
	}
	filtered := make([]BookKnowledgeBook, 0, len(books))
	for _, book := range books {
		haystack := strings.ToLower(strings.Join([]string{
			book.BookID,
			book.Title,
			book.Author,
			book.Status,
			book.Extractor,
		}, " "))
		if strings.Contains(haystack, term) {
			filtered = append(filtered, book)
		}
	}
	return filtered
}

func sortKBaseBooks(books []BookKnowledgeBook, sortMode string) {
	switch strings.TrimSpace(sortMode) {
	case "title_asc":
		sort.SliceStable(books, func(i, j int) bool {
			if books[i].Title != books[j].Title {
				return books[i].Title < books[j].Title
			}
			return books[i].BookID < books[j].BookID
		})
	default:
		sort.SliceStable(books, func(i, j int) bool {
			if books[i].UpdatedAt != books[j].UpdatedAt {
				return books[i].UpdatedAt > books[j].UpdatedAt
			}
			return books[i].BookID < books[j].BookID
		})
	}
}

func bookIDFromAPIBookSubroute(urlPath string, suffix string) (string, error) {
	trimmed := strings.TrimPrefix(urlPath, "/api/books/")
	if !strings.HasSuffix(trimmed, suffix) {
		return "", fmt.Errorf("invalid book route")
	}
	bookID, err := url.PathUnescape(strings.TrimSuffix(trimmed, suffix))
	if err != nil || strings.TrimSpace(bookID) == "" {
		return "", fmt.Errorf("book_id is required")
	}
	return bookID, nil
}

func requireHTTPMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
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
	manifest, status, err := h.systemKBManifest()
	if err != nil {
		writeHTTPError(w, status, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, manifest)
}

func (h *kbaseHTTPHandler) systemKBManifest() (map[string]any, int, error) {
	payload, err := h.readSystemKBExport()
	if err != nil {
		return nil, http.StatusNotFound, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("invalid system kb export: %v", err)
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
	return manifest, http.StatusOK, nil
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
