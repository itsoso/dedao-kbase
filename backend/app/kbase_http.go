package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
)

type KBaseHTTPConfig struct {
	Store                   *BookKnowledgeStore
	AuthToken               string
	SystemKBExportPath      string
	StaticDir               string
	WeChat                  *WeChatSourceService
	WCPlus                  *WCPlusSourceService
	SourceSync              *SourceSyncStore
	SourceIngest            *SourceIngestService
	SourceAgentToken        string
	SourceAgentMaxBodyBytes int64
	SourceAssets            *SourceAssetStore
	AnalysisGenerator       BookAnalysisGenerator
	DedaoLibrary            DedaoLibraryService
	ReverificationNow       func() time.Time
	ReverificationCooldown  time.Duration
}

type BookAnalysisGenerator func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error)

type DedaoLibraryService interface {
	CourseList(category, order string, page, limit int) (*services.CourseList, error)
	CourseInfo(enid string) (*services.CourseInfo, error)
	ArticleList(enid, chapterID string, count, maxID int) (*services.ArticleList, error)
}

type kbaseHTTPHandler struct {
	store                   *BookKnowledgeStore
	authToken               string
	systemKBExportPath      string
	staticDir               string
	wechat                  *WeChatSourceService
	wcplus                  *WCPlusSourceService
	sourceSync              *SourceSyncStore
	sourceIngest            *SourceIngestService
	sourceAgentToken        string
	sourceAgentMaxBodyBytes int64
	sourceAssets            *SourceAssetStore
	analysisGenerator       BookAnalysisGenerator
	dedaoLibrary            DedaoLibraryService
	reverificationNow       func() time.Time
	reverificationCooldown  time.Duration
}

const defaultSourceAgentMaxBodyBytes int64 = 8 << 20

func NewKBaseHTTPHandler(cfg KBaseHTTPConfig) http.Handler {
	store := cfg.Store
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	maxBodyBytes := cfg.SourceAgentMaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultSourceAgentMaxBodyBytes
	}
	sourceIngest := cfg.SourceIngest
	if sourceIngest == nil && cfg.SourceSync != nil {
		sourceIngest = NewSourceIngestService(store, cfg.SourceSync)
	}
	authToken := strings.TrimSpace(cfg.AuthToken)
	sourceAgentToken := strings.TrimSpace(cfg.SourceAgentToken)
	if authToken != "" && sourceAgentToken == authToken {
		sourceAgentToken = ""
	}
	assets := cfg.SourceAssets
	if assets == nil {
		assets, _ = NewSourceAssetStore(store.Root())
	}
	analysisGenerator := cfg.AnalysisGenerator
	if analysisGenerator == nil {
		analysisGenerator = GenerateBookAnalysisManifest
	}
	dedaoLibrary := cfg.DedaoLibrary
	if dedaoLibrary == nil {
		dedaoLibrary = defaultDedaoLibrary{}
	}
	reverificationNow := cfg.ReverificationNow
	if reverificationNow == nil {
		reverificationNow = time.Now
	}
	reverificationCooldown := cfg.ReverificationCooldown
	if reverificationCooldown < 0 {
		reverificationCooldown = 0
	}
	if reverificationCooldown == 0 {
		reverificationCooldown = 5 * time.Minute
	}
	return &kbaseHTTPHandler{
		store:                   store,
		authToken:               authToken,
		systemKBExportPath:      strings.TrimSpace(cfg.SystemKBExportPath),
		staticDir:               strings.TrimSpace(cfg.StaticDir),
		wechat:                  cfg.WeChat,
		wcplus:                  cfg.WCPlus,
		sourceSync:              cfg.SourceSync,
		sourceIngest:            sourceIngest,
		sourceAgentToken:        sourceAgentToken,
		sourceAgentMaxBodyBytes: maxBodyBytes,
		sourceAssets:            assets,
		analysisGenerator:       analysisGenerator,
		dedaoLibrary:            dedaoLibrary,
		reverificationNow:       reverificationNow,
		reverificationCooldown:  reverificationCooldown,
	}
}

type defaultDedaoLibrary struct{}

func (defaultDedaoLibrary) CourseList(category, order string, page, limit int) (*services.CourseList, error) {
	return CourseList(category, order, page, limit)
}

func (defaultDedaoLibrary) CourseInfo(enid string) (*services.CourseInfo, error) {
	return CourseInfoByEnid(enid)
}

func (defaultDedaoLibrary) ArticleList(enid, chapterID string, count, maxID int) (*services.ArticleList, error) {
	return ArticleList(enid, chapterID, count, maxID)
}

func (h *kbaseHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") && h.applyCORS(w, r) && r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
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
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		h.serveStatic(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/source-agent/") {
		if h.sourceSync == nil || h.sourceAgentToken == "" {
			writeHTTPError(w, http.StatusServiceUnavailable, "source agent API is not configured")
			return
		}
		if !authorizeBearerToken(w, r, h.sourceAgentToken) {
			return
		}
		h.handleSourceAgent(w, r)
		return
	}
	if !h.authorize(w, r) {
		return
	}
	if isSourceSyncAdminPath(r.URL.Path) {
		h.handleSourceSyncAdmin(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/source-assets/") {
		h.handleSourceAssetRead(w, r)
		return
	}
	if r.URL.Path == "/api/wechat/import" {
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleWeChatImport(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/wcplus/") && r.Method == http.MethodPost {
		h.handleWCPlusPost(w, r)
		return
	}
	if bookID, ok := bookAnalysisPathID(r.URL.Path); ok {
		h.handleBookAnalysis(w, r, bookID)
		return
	}
	if bookID, ok := bookNestedPathID(r.URL.Path, "quality"); ok {
		h.handleBookQuality(w, r, bookID)
		return
	}
	if bookID, ok := bookNestedPathID(r.URL.Path, "publish"); ok {
		h.handleBookPublish(w, r, bookID)
		return
	}
	if releaseID, ok := knowledgeReleaseFeedbackPathID(r.URL.Path); ok {
		h.handleKnowledgeFeedback(w, r, releaseID)
		return
	}
	if releaseID, ok := knowledgeReleaseReverificationPathID(r.URL.Path); ok {
		h.handleKnowledgeReverification(w, r, releaseID)
		return
	}
	if releaseID, ok := knowledgeReleaseReverificationRetryPathID(r.URL.Path); ok {
		h.handleKnowledgeReverificationRetry(w, r, releaseID)
		return
	}
	if releaseID, ok := knowledgeReleaseReceiptPathID(r.URL.Path); ok {
		h.handleDeliveryReceipt(w, r, releaseID)
		return
	}
	if r.URL.Path == "/api/consumers/health/releases" {
		h.handleHealthKnowledgeFeed(w, r)
		return
	}
	if r.URL.Path == "/api/consumers/health/readiness" {
		h.handleHealthEvidenceReadiness(w, r)
		return
	}
	if r.URL.Path == "/api/consumers/health/readiness/analyze" {
		h.handleHealthEvidenceReadinessAnalyze(w, r)
		return
	}
	if r.URL.Path == "/api/consumers/health/search" {
		h.handleHealthEvidenceSearch(w, r)
		return
	}
	if releaseID, ok := healthEvidencePathID(r.URL.Path); ok {
		h.handleHealthEvidence(w, r, releaseID)
		return
	}
	if r.URL.Path == "/api/knowledge/feed" {
		h.handleKnowledgeFeed(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/knowledge/lineage/") {
		h.handleKnowledgeLineage(w, r)
		return
	}
	if r.URL.Path == "/api/knowledge/impact" {
		h.handleKnowledgeImpact(w, r)
		return
	}
	if r.URL.Path == "/api/knowledge/gaps" {
		h.handleKnowledgeGaps(w, r)
		return
	}
	if r.URL.Path == "/api/knowledge/review" {
		h.handleKnowledgeReview(w, r)
		return
	}
	if r.URL.Path == "/api/knowledge/releases" || strings.HasPrefix(r.URL.Path, "/api/knowledge/releases/") {
		h.handleKnowledgeReleases(w, r)
		return
	}
	if r.URL.Path == "/api/book-chat" {
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleBookChat(w, r)
		return
	}
	if r.URL.Path == "/api/dedao/library" {
		h.handleDedaoLibrary(w, r)
		return
	}
	if r.URL.Path == "/api/dedao/home" {
		h.handleDedaoHome(w, r)
		return
	}
	if r.URL.Path == "/api/dedao/course" {
		h.handleDedaoCourse(w, r)
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
	case r.URL.Path == "/api/wechat/article":
		h.handleWeChatArticle(w, r)
	case r.URL.Path == "/api/wechat/search":
		h.handleWeChatSearch(w, r)
	case r.URL.Path == "/api/wechat/articles":
		h.handleWeChatArticles(w, r)
	case r.URL.Path == "/api/wcplus/gzh/list":
		h.handleWCPlusAccountList(w, r)
	case r.URL.Path == "/api/wcplus/gzh/articles":
		h.handleWCPlusArticleList(w, r)
	case r.URL.Path == "/api/wcplus/article/content":
		h.handleWCPlusArticleContent(w, r)
	case r.URL.Path == "/api/wcplus/task/all":
		h.handleWCPlusTaskList(w, r)
	case r.URL.Path == "/api/wcplus/status":
		h.handleWCPlusStatus(w, r)
	case r.URL.Path == "/api/wcplus/env/check":
		h.handleWCPlusEnvCheck(w, r)
	case r.URL.Path == "/api/wcplus/gzh/search":
		h.handleWCPlusGetJSON(w, r, "/api/gzh/search")
	case r.URL.Path == "/api/wcplus/search-gzh":
		h.handleWCPlusGetJSON(w, r, "/api/search_gzh/search")
	case r.URL.Path == "/api/wcplus/article/all":
		h.handleWCPlusGetJSON(w, r, "/api/article/all_articles")
	case r.URL.Path == "/api/wcplus/article/search-title":
		h.handleWCPlusGetJSON(w, r, "/api/article/search_title")
	case r.URL.Path == "/api/wcplus/search":
		h.handleWCPlusGetJSON(w, r, "/api/search/search")
	case r.URL.Path == "/api/wcplus/report/reading-data":
		h.handleWCPlusGetJSON(w, r, "/api/report/reading_data")
	case r.URL.Path == "/api/wcplus/report/statistic-data":
		h.handleWCPlusGetJSON(w, r, "/api/report/statistic_data")
	case r.URL.Path == "/api/wcplus/article/gzh":
		h.handleWCPlusGetJSON(w, r, "/api/article/gzh")
	case r.URL.Path == "/api/wcplus/like-articles":
		h.handleWCPlusGetJSON(w, r, "/api/like_article/get_all")
	case r.URL.Path == "/api/wcplus/request/gzh":
		h.handleWCPlusGetJSON(w, r, "/api/req_data/get_gzh")
	case r.URL.Path == "/api/wcplus/export/text":
		h.handleWCPlusGetJSON(w, r, "/api/article/export_text")
	case r.URL.Path == "/api/wcplus/export/gzh-csv":
		h.handleWCPlusGetJSON(w, r, "/api/gzh/export_csv")
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
	}
}

func (h *kbaseHTTPHandler) handleKnowledgeFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	page, err := BuildKnowledgeFeedPage(h.store, parseKnowledgeFeedQuery(r.URL.Query()))
	if err != nil {
		if strings.Contains(err.Error(), "invalid cursor") {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, page)
}

func (h *kbaseHTTPHandler) handleHealthKnowledgeFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	page, err := BuildHealthKnowledgeFeedPage(h.store, r.URL.Query())
	if err != nil {
		if strings.Contains(err.Error(), "invalid cursor") {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, page)
}

func (h *kbaseHTTPHandler) handleHealthEvidence(w http.ResponseWriter, r *http.Request, releaseID string) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pkg, err := BuildHealthEvidencePackage(h.store, releaseID)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not available for health") {
			writeHTTPError(w, http.StatusNotFound, "health evidence not found")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, pkg)
}

func (h *kbaseHTTPHandler) handleHealthEvidenceSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	page, err := SearchHealthEvidence(h.store, ParseHealthEvidenceSearchQuery(r.URL.Query()))
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, page)
}

func (h *kbaseHTTPHandler) handleHealthEvidenceReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	report, err := BuildHealthEvidenceReadiness(h.store, ParseHealthEvidenceReadinessLimit(r.URL.Query()))
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, report)
}

func (h *kbaseHTTPHandler) handleHealthEvidenceReadinessAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input HealthEvidenceAnalysisBatchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := RunHealthEvidenceAnalysisBatch(r.Context(), h.store, h.analysisGenerator, input)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func (h *kbaseHTTPHandler) handleDedaoLibrary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := r.URL.Query()
	category := strings.TrimSpace(query.Get("category"))
	if category == "" {
		category = CateCourse
	}
	if !isDedaoLibraryCategory(category) {
		writeHTTPError(w, http.StatusBadRequest, "invalid dedao category")
		return
	}
	order := strings.TrimSpace(query.Get("order"))
	if order == "" {
		order = "study"
	}
	page := parseBoundedInt(query.Get("page"), 1, 1, 10000)
	pageSize := parseBoundedInt(firstNonEmpty(query.Get("page_size"), query.Get("limit")), 15, 1, 100)
	list, err := h.dedaoLibrary.CourseList(category, order, page, pageSize)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	if list == nil {
		list = &services.CourseList{}
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"category":  category,
		"order":     order,
		"page":      page,
		"page_size": pageSize,
		"list":      list.List,
		"is_more":   list.ISMore,
	})
}

func (h *kbaseHTTPHandler) handleDedaoHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pageSize := parseBoundedInt(firstNonEmpty(r.URL.Query().Get("page_size"), r.URL.Query().Get("limit")), 6, 1, 30)
	payload := map[string]any{}
	for key, category := range map[string]string{
		"courses": CateCourse,
		"ebooks":  CateEbook,
		"odob":    CateAudioBook,
	} {
		list, err := h.dedaoLibrary.CourseList(category, "study", 1, pageSize)
		if err != nil {
			writeHTTPError(w, http.StatusBadGateway, err.Error())
			return
		}
		items := []services.Course{}
		isMore := 0
		if list != nil {
			items = list.List
			isMore = list.ISMore
		}
		payload[key] = map[string]any{
			"category":  category,
			"page":      1,
			"page_size": pageSize,
			"list":      items,
			"is_more":   isMore,
		}
	}
	writeHTTPJSON(w, http.StatusOK, payload)
}

func (h *kbaseHTTPHandler) handleDedaoCourse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	enid := strings.TrimSpace(r.URL.Query().Get("enid"))
	if enid == "" {
		writeHTTPError(w, http.StatusBadRequest, "missing enid")
		return
	}
	info, err := h.dedaoLibrary.CourseInfo(enid)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	if info == nil {
		writeHTTPError(w, http.StatusNotFound, "course not found")
		return
	}
	if len(info.FlatArticleList) == 0 {
		articles, err := h.dedaoLibrary.ArticleList(enid, "", 30, 30)
		if err != nil {
			info.ArticleListError = err.Error()
			writeHTTPJSON(w, http.StatusOK, info)
			return
		}
		for _, article := range articles.List {
			info.FlatArticleList = append(info.FlatArticleList, article.ArticleBase)
		}
	}
	writeHTTPJSON(w, http.StatusOK, info)
}

func isDedaoLibraryCategory(category string) bool {
	switch category {
	case CateCourse, CateEbook, CateAudioBook, CateAce:
		return true
	default:
		return false
	}
}

func parseBoundedInt(value string, fallback, min, max int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	if parsed < min {
		return min
	}
	if parsed > max {
		return max
	}
	return parsed
}

func (h *kbaseHTTPHandler) handleDeliveryReceipt(w http.ResponseWriter, r *http.Request, releaseID string) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, err := h.store.LoadKnowledgeRelease(releaseID); err != nil {
		if os.IsNotExist(err) {
			writeHTTPError(w, http.StatusNotFound, "release not found")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var input DeliveryReceipt
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if input.ReleaseID != releaseID {
		writeHTTPError(w, http.StatusBadRequest, "release_id must match path")
		return
	}
	raw, err := json.Marshal(input)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := ValidateDeliveryReceiptContract(raw); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	catalog, err := NewKnowledgeCatalogStore(h.store.Root(), time.Now)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer catalog.Close()
	receipt, err := catalog.SaveDeliveryReceipt(input, time.Now)
	if err != nil {
		if strings.Contains(err.Error(), "idempotency payload conflict") {
			writeHTTPError(w, http.StatusConflict, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, receipt)
}

func (h *kbaseHTTPHandler) handleKnowledgeLineage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	objectID, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/knowledge/lineage/"))
	if err != nil || strings.TrimSpace(objectID) == "" || strings.Contains(objectID, "/") {
		writeHTTPError(w, http.StatusBadRequest, "object_id is required")
		return
	}
	lineage, err := BuildKnowledgeLineage(h.store, objectID)
	if err != nil {
		if os.IsNotExist(err) {
			writeHTTPError(w, http.StatusNotFound, "object not found")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, lineage)
}

func (h *kbaseHTTPHandler) handleKnowledgeImpact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	catalog, err := NewKnowledgeCatalogStore(h.store.Root(), time.Now)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer catalog.Close()
	report, err := BuildKnowledgeImpactReport(h.store, catalog)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, report)
}

func (h *kbaseHTTPHandler) handleKnowledgeGaps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	catalog, err := NewKnowledgeCatalogStore(h.store.Root(), time.Now)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer catalog.Close()
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	report, err := ListKnowledgeGaps(catalog, limit)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, report)
}

func (h *kbaseHTTPHandler) handleKnowledgeReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 200 {
			writeHTTPError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = parsed
	}
	catalog, err := NewKnowledgeCatalogStore(h.store.Root(), time.Now)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer catalog.Close()
	report, err := BuildKnowledgeReviewCockpit(h.store, catalog, limit, time.Now)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, report)
}

func knowledgeReleaseFeedbackPathID(path string) (string, bool) {
	return knowledgeReleaseNestedPathID(path, "feedback")
}

func knowledgeReleaseReceiptPathID(path string) (string, bool) {
	return knowledgeReleaseNestedPathID(path, "receipts")
}

func knowledgeReleaseReverificationPathID(path string) (string, bool) {
	return knowledgeReleaseNestedPathID(path, "reverification")
}

func knowledgeReleaseReverificationRetryPathID(path string) (string, bool) {
	return knowledgeReleaseNestedPathID(path, "reverification/retry")
}

func knowledgeReleaseNestedPathID(path, resource string) (string, bool) {
	const prefix = "/api/knowledge/releases/"
	suffix := "/" + strings.Trim(resource, "/")
	if suffix == "/" {
		return "", false
	}
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	rawID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if rawID == "" || strings.Contains(rawID, "/") {
		return "", false
	}
	releaseID, err := url.PathUnescape(rawID)
	return releaseID, err == nil && strings.TrimSpace(releaseID) != ""
}

func healthEvidencePathID(path string) (string, bool) {
	const prefix = "/api/consumers/health/evidence/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rawID := strings.TrimPrefix(path, prefix)
	if rawID == "" || strings.Contains(rawID, "/") {
		return "", false
	}
	releaseID, err := url.PathUnescape(rawID)
	return releaseID, err == nil && strings.TrimSpace(releaseID) != ""
}

func (h *kbaseHTTPHandler) handleKnowledgeFeedback(w http.ResponseWriter, r *http.Request, releaseID string) {
	if r.Method == http.MethodGet {
		assessment, err := h.store.AssessKnowledgeFeedback(releaseID)
		if err != nil {
			if os.IsNotExist(err) {
				writeHTTPError(w, http.StatusNotFound, "release not found")
				return
			}
			writeHTTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeHTTPJSON(w, http.StatusOK, assessment)
		return
	}
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input KnowledgeFeedbackInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	feedback, counts, err := h.store.SaveKnowledgeFeedback(releaseID, input)
	if err != nil {
		if os.IsNotExist(err) {
			writeHTTPError(w, http.StatusNotFound, "release not found")
			return
		}
		if strings.Contains(err.Error(), "idempotency payload conflict") {
			writeHTTPError(w, http.StatusConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid feedback") || strings.Contains(err.Error(), "claim_id") || strings.Contains(err.Error(), "too long") {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	assessment, err := h.store.AssessKnowledgeFeedback(releaseID)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response := map[string]any{"feedback": feedback, "status_counts": counts, "assessment": assessment}
	if invalidatesKnowledgeRelease(input.Outcome) {
		task, enqueueErr := h.store.EnqueueKnowledgeReverification(releaseID, *assessment, h.reverificationNow(), h.reverificationCooldown)
		if enqueueErr != nil {
			writeHTTPError(w, http.StatusInternalServerError, enqueueErr.Error())
			return
		}
		response["reverification"] = task
	}
	writeHTTPJSON(w, http.StatusOK, response)
}

func (h *kbaseHTTPHandler) handleKnowledgeReverification(w http.ResponseWriter, r *http.Request, releaseID string) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, err := h.store.LoadKnowledgeRelease(releaseID); err != nil {
		if os.IsNotExist(err) {
			writeHTTPError(w, http.StatusNotFound, "release not found")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tasks, err := h.store.ListKnowledgeReverifications(releaseID)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"release_id": releaseID, "tasks": tasks})
}

func (h *kbaseHTTPHandler) handleKnowledgeReverificationRetry(w http.ResponseWriter, r *http.Request, releaseID string) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	task, err := h.store.RetryKnowledgeReverification(releaseID, h.reverificationNow())
	if err != nil {
		if os.IsNotExist(err) {
			writeHTTPError(w, http.StatusNotFound, "release not found")
			return
		}
		if strings.Contains(err.Error(), "requires") || strings.Contains(err.Error(), "superseded") || strings.Contains(err.Error(), "task not found") {
			writeHTTPError(w, http.StatusConflict, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, task)
}

func bookAnalysisPathID(path string) (string, bool) {
	return bookNestedPathID(path, "analysis")
}

func bookNestedPathID(path, resource string) (string, bool) {
	const prefix = "/api/books/"
	suffix := "/" + strings.Trim(resource, "/")
	if suffix == "/" || !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	bookID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if bookID == "" || strings.Contains(bookID, "/") {
		return "", false
	}
	decoded, err := url.PathUnescape(bookID)
	if err != nil || strings.TrimSpace(decoded) == "" {
		return "", false
	}
	return decoded, true
}

func (h *kbaseHTTPHandler) handleBookQuality(w http.ResponseWriter, r *http.Request, bookID string) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	report, err := h.store.LoadBookQualityReport(bookID)
	if err != nil {
		if os.IsNotExist(err) {
			writeHTTPError(w, http.StatusNotFound, "quality report not found")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, report)
}

func (h *kbaseHTTPHandler) handleBookPublish(w http.ResponseWriter, r *http.Request, bookID string) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	release, err := PublishKnowledgeRelease(h.store, bookID)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "book not found") {
			writeHTTPError(w, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "quality decision") || strings.Contains(err.Error(), "requires ready") || strings.Contains(err.Error(), "stale") || strings.Contains(err.Error(), "reverification") {
			writeHTTPError(w, http.StatusConflict, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, release)
}

func (h *kbaseHTTPHandler) handleKnowledgeReleases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	const prefix = "/api/knowledge/releases/"
	if strings.HasPrefix(r.URL.Path, prefix) {
		rawID := strings.TrimPrefix(r.URL.Path, prefix)
		if rawID == "" || strings.Contains(rawID, "/") {
			writeHTTPError(w, http.StatusNotFound, "release not found")
			return
		}
		releaseID, err := url.PathUnescape(rawID)
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest, "invalid release_id")
			return
		}
		release, err := h.store.LoadKnowledgeRelease(releaseID)
		if err != nil {
			if os.IsNotExist(err) {
				writeHTTPError(w, http.StatusNotFound, "release not found")
				return
			}
			writeHTTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeHTTPJSON(w, http.StatusOK, release)
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 200 {
			writeHTTPError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = parsed
	}
	releases, err := h.store.ListKnowledgeReleasesForBook(r.URL.Query().Get("after"), limit, r.URL.Query().Get("book_id"))
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	nextCursor := ""
	if len(releases) > 0 {
		nextCursor = releases[len(releases)-1].ReleaseID
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"releases": releases, "next_cursor": nextCursor})
}

func (h *kbaseHTTPHandler) handleBookAnalysis(w http.ResponseWriter, r *http.Request, bookID string) {
	switch r.Method {
	case http.MethodGet:
		manifest, err := h.store.LoadAnalysisManifest(bookID)
		if err != nil {
			if os.IsNotExist(err) {
				writeHTTPError(w, http.StatusNotFound, "analysis manifest not found")
				return
			}
			writeHTTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeHTTPJSON(w, http.StatusOK, manifest)
	case http.MethodPost:
		var request BookAnalysisGenerateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
			writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		request.BookID = bookID
		manifest, err := h.analysisGenerator(r.Context(), h.store, request)
		if err != nil {
			if os.IsNotExist(err) || strings.Contains(err.Error(), "book not found") {
				writeHTTPError(w, http.StatusNotFound, err.Error())
				return
			}
			writeHTTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeHTTPJSON(w, http.StatusOK, manifest)
	default:
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *kbaseHTTPHandler) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || !isAllowedKBaseCORSOrigin(origin) {
		return false
	}
	w.Header().Add("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
	w.Header().Set("Access-Control-Max-Age", "600")
	return true
}

func isAllowedKBaseCORSOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	switch parsed.Scheme {
	case "wails":
		return host == "wails.localhost"
	case "http", "https":
		return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "wails.localhost"
	default:
		return false
	}
}

func (h *kbaseHTTPHandler) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.staticDir == "" {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	info, err := os.Stat(h.staticDir)
	if err != nil || !info.IsDir() {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}

	staticDir, err := filepath.Abs(h.staticDir)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	requestPath := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), string(filepath.Separator))
	if requestPath == "." {
		requestPath = ""
	}
	filePath := filepath.Join(staticDir, requestPath)
	rel, err := filepath.Rel(staticDir, filePath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		writeHTTPError(w, http.StatusBadRequest, "invalid static path")
		return
	}

	if fileInfo, err := os.Stat(filePath); err == nil && !fileInfo.IsDir() {
		if strings.EqualFold(filepath.Base(filePath), "index.html") {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, filePath)
		return
	}
	if filepath.Ext(requestPath) != "" {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}

	indexPath := filepath.Join(staticDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, indexPath)
}

func (h *kbaseHTTPHandler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.authToken == "" {
		writeHTTPError(w, http.StatusUnauthorized, "kbase auth token is not configured")
		return false
	}
	return authorizeBearerToken(w, r, h.authToken)
}

func authorizeBearerToken(w http.ResponseWriter, r *http.Request, expected string) bool {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	token := strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	if token == value || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (h *kbaseHTTPHandler) handleBrowserSessionToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		writeHTTPError(w, http.StatusUnauthorized, "browser session does not accept bearer authorization")
		return
	}
	if h.authToken == "" {
		writeHTTPError(w, http.StatusUnauthorized, "kbase auth token is not configured")
		return
	}
	if strings.TrimSpace(r.Header.Get("X-KBase-Browser-Session")) != "1" {
		writeHTTPError(w, http.StatusUnauthorized, "browser session is not authorized")
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"token": h.authToken,
	})
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
	pkg, err := h.loadHTTPBookPackage(bookID)
	if err != nil {
		writeHTTPError(w, http.StatusNotFound, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, pkg)
}

func (h *kbaseHTTPHandler) loadHTTPBookPackage(bookID string) (*BookKnowledgePackage, error) {
	bookID = sanitizeBookKnowledgeID(bookID)
	if strings.TrimSpace(bookID) == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	if pkg, err := h.store.LoadPackage(bookID); err == nil {
		return pkg, nil
	}
	if fallback := stripReaderRouteSuffix(bookID); fallback != bookID {
		if pkg, err := h.store.LoadPackage(fallback); err == nil {
			return pkg, nil
		}
	}
	return nil, fmt.Errorf("book not found: %s", bookID)
}

var readerRouteSuffixes = []string{
	"overview",
	"chat",
	"prompts",
	"chapters",
	"claims",
	"chunks",
	"jobs",
	"system-kb",
	"skills",
	"ops",
}

func stripReaderRouteSuffix(bookID string) string {
	for _, suffix := range readerRouteSuffixes {
		marker := "-" + suffix
		if strings.HasSuffix(bookID, marker) {
			base := strings.TrimSuffix(bookID, marker)
			if isNumericBookID(base) {
				return base
			}
		}
	}
	return bookID
}

func isNumericBookID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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

func (h *kbaseHTTPHandler) handleBookChat(w http.ResponseWriter, r *http.Request) {
	var request BookKnowledgeChatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	response, err := BookKnowledgeChat(r.Context(), h.store, request)
	if err != nil {
		if strings.Contains(err.Error(), "book_id is required") || strings.Contains(err.Error(), "question is required") {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.Contains(err.Error(), "book not found") {
			writeHTTPError(w, http.StatusNotFound, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, response)
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

func (h *kbaseHTTPHandler) wechatService() *WeChatSourceService {
	if h.wechat != nil {
		return h.wechat
	}
	h.wechat = NewWeChatSourceService(WeChatSourceConfigFromEnv())
	return h.wechat
}

func (h *kbaseHTTPHandler) wcplusService() *WCPlusSourceService {
	if h.wcplus != nil {
		return h.wcplus
	}
	h.wcplus = NewWCPlusSourceService(WCPlusSourceConfigFromEnv())
	return h.wcplus
}

func (h *kbaseHTTPHandler) handleWeChatArticle(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	article, err := h.wechatService().DownloadArticle(r.Context(), rawURL)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"article": article})
}

func (h *kbaseHTTPHandler) handleWeChatSearch(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.wechatService().SearchOfficialAccounts(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		h.writeWeChatError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}

func (h *kbaseHTTPHandler) handleWeChatArticles(w http.ResponseWriter, r *http.Request) {
	begin := parseNonNegativeQueryInt(r, "begin", 0)
	count := parseNonNegativeQueryInt(r, "count", 5)
	articles, err := h.wechatService().ListOfficialAccountArticles(r.Context(), r.URL.Query().Get("fakeid"), begin, count)
	if err != nil {
		h.writeWeChatError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"articles": articles})
}

func (h *kbaseHTTPHandler) handleWeChatImport(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var payload struct {
		URL    string `json:"url"`
		BookID string `json:"book_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	pkg, err := h.wechatService().ImportArticle(r.Context(), h.store, payload.URL, payload.BookID)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"book": pkg.Book})
}

func (h *kbaseHTTPHandler) writeWeChatError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrWeChatCredentialsNotConfigured) {
		writeHTTPError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeHTTPError(w, http.StatusBadRequest, err.Error())
}

func (h *kbaseHTTPHandler) handleWCPlusAccountList(w http.ResponseWriter, r *http.Request) {
	list, err := h.wcplusService().ListAccounts(r.Context(), WCPlusListOptions{
		Offset:    parseNonNegativeQueryInt(r, "offset", 0),
		Num:       parseNonNegativeQueryInt(r, "num", 20),
		Sort:      r.URL.Query().Get("sort"),
		Direction: r.URL.Query().Get("direction"),
		Query:     r.URL.Query().Get("q"),
	})
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, list)
}

func (h *kbaseHTTPHandler) handleWCPlusArticleList(w http.ResponseWriter, r *http.Request) {
	list, err := h.wcplusService().ListAccountArticles(r.Context(), WCPlusArticleListOptions{
		Biz:       r.URL.Query().Get("biz"),
		Nickname:  r.URL.Query().Get("nickname"),
		Offset:    parseNonNegativeQueryInt(r, "offset", 0),
		Num:       parseNonNegativeQueryInt(r, "num", 20),
		Sort:      r.URL.Query().Get("sort"),
		Direction: r.URL.Query().Get("direction"),
	})
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, list)
}

func (h *kbaseHTTPHandler) handleWCPlusArticleContent(w http.ResponseWriter, r *http.Request) {
	var content *WCPlusArticleContent
	var err error
	if rawURL := strings.TrimSpace(r.URL.Query().Get("url")); rawURL != "" && strings.TrimSpace(r.URL.Query().Get("id")) == "" {
		content, err = h.wcplusService().GetArticleContentByURL(r.Context(), rawURL)
	} else {
		content, err = h.wcplusService().GetArticleContent(r.Context(), r.URL.Query().Get("nickname"), r.URL.Query().Get("id"))
	}
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, content)
}

func (h *kbaseHTTPHandler) handleWCPlusTaskList(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.wcplusService().ListTasks(r.Context())
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (h *kbaseHTTPHandler) handleWCPlusPost(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/wcplus/import/article":
		h.handleWCPlusImportArticle(w, r)
	case "/api/wcplus/import/raw":
		h.handleWCPlusImportRawArticle(w, r)
	case "/api/wcplus/import/account":
		h.handleWCPlusImportAccount(w, r)
	case "/api/wcplus/task/new":
		h.handleWCPlusTaskCreate(w, r)
	case "/api/wcplus/task/control":
		h.handleWCPlusTaskControl(w, r)
	case "/api/wcplus/batch-task/create":
		h.handleWCPlusPostJSON(w, r, "/api/batch_task/create_task")
	case "/api/wcplus/batch-task/delete":
		h.handleWCPlusPostJSON(w, r, "/api/batch_task/delete_task")
	case "/api/wcplus/export/all-articles-xlsx":
		h.handleWCPlusExportAllArticlesXLSX(w, r)
	case "/api/wcplus/batch-import/gzh":
		h.handleWCPlusBatchImportGZH(w, r)
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
	}
}

func (h *kbaseHTTPHandler) handleWCPlusStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.wcplusService().Status(r.Context())
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, status)
}

func (h *kbaseHTTPHandler) handleWCPlusEnvCheck(w http.ResponseWriter, r *http.Request) {
	writeHTTPJSON(w, http.StatusOK, h.wcplusService().CheckEnvironment(r.Context()))
}

func (h *kbaseHTTPHandler) handleWCPlusGetJSON(w http.ResponseWriter, r *http.Request, upstreamPath string) {
	payload, err := h.wcplusService().GetJSON(r.Context(), upstreamPath, r.URL.Query())
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, payload)
}

func (h *kbaseHTTPHandler) handleWCPlusBatchImportGZH(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var payload WCPlusBatchImportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&payload); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.wcplusService().BatchImportNicknamesToKnowledge(r.Context(), h.store, payload)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func (h *kbaseHTTPHandler) handleWCPlusPostJSON(w http.ResponseWriter, r *http.Request, upstreamPath string) {
	payload, ok := decodeHTTPJSONBody(w, r)
	if !ok {
		return
	}
	result, err := h.wcplusService().PostJSON(r.Context(), upstreamPath, payload)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func (h *kbaseHTTPHandler) handleWCPlusExportAllArticlesXLSX(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeHTTPJSONBody(w, r)
	if !ok {
		return
	}
	body, contentType, err := h.wcplusService().PostRaw(r.Context(), "/api/article/all_articles/export_xlsx", payload)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="wcplus-all-articles.xlsx"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *kbaseHTTPHandler) handleWCPlusImportArticle(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var payload WCPlusImportArticleRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	pkg, err := h.wcplusService().ImportArticle(r.Context(), h.store, payload)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"book": pkg.Book})
}

func (h *kbaseHTTPHandler) handleWCPlusImportRawArticle(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var payload WCPlusRawImportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&payload); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	pkg, err := h.wcplusService().ImportRawArticle(r.Context(), h.store, payload)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"book": pkg.Book})
}

func (h *kbaseHTTPHandler) handleWCPlusImportAccount(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var payload WCPlusImportAccountRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.wcplusService().ImportAccountArticles(r.Context(), h.store, payload)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func (h *kbaseHTTPHandler) handleWCPlusTaskCreate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var payload WCPlusTaskRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	task, err := h.wcplusService().CreateTask(r.Context(), payload)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, task)
}

func (h *kbaseHTTPHandler) handleWCPlusTaskControl(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeHTTPJSONBody(w, r)
	if !ok {
		return
	}
	result, err := h.wcplusService().PostJSON(r.Context(), "/api/task/control", payload)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func isSourceSyncAdminPath(path string) bool {
	return path == "/api/source-agents" ||
		path == "/api/source-subscriptions" ||
		strings.HasPrefix(path, "/api/source-subscriptions/") ||
		path == "/api/source-sync/runs" ||
		strings.HasPrefix(path, "/api/source-sync/runs/")
}

func (h *kbaseHTTPHandler) handleSourceAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	switch r.URL.Path {
	case "/api/source-agent/heartbeat":
		var payload SourceAgentHeartbeat
		if !h.decodeSourceAgentJSON(w, r, &payload) {
			return
		}
		agent, err := h.sourceSync.HeartbeatAgent(payload)
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"agent": agent})
	case "/api/source-agent/lease":
		var payload struct {
			AgentID      string   `json:"agent_id"`
			Capabilities []string `json:"capabilities"`
			LeaseSeconds int      `json:"lease_seconds"`
		}
		if !h.decodeSourceAgentJSON(w, r, &payload) {
			return
		}
		leaseDuration := time.Duration(payload.LeaseSeconds) * time.Second
		run, err := h.sourceSync.LeaseNextRun(payload.AgentID, payload.Capabilities, leaseDuration)
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		if run != nil {
			started, err := h.sourceSync.StartRun(run.ID, payload.AgentID)
			if err != nil {
				h.writeSourceSyncError(w, err)
				return
			}
			subscription, err := h.sourceSync.GetSubscription(started.SubscriptionID)
			if err != nil {
				h.writeSourceSyncError(w, err)
				return
			}
			started.Subscription = &subscription
			run = &started
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"run": run})
	default:
		h.handleSourceAgentRun(w, r)
	}
}

func (h *kbaseHTTPHandler) handleSourceAgentRun(w http.ResponseWriter, r *http.Request) {
	runID, action, ok := parseSourceSyncRunAction(r.URL.Path, "/api/source-agent/runs/")
	if !ok {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	switch action {
	case "items":
		var payload struct {
			AgentID string `json:"agent_id"`
			Error   string `json:"error,omitempty"`
			SourceArticleEnvelope
		}
		if !h.decodeSourceAgentJSON(w, r, &payload) {
			return
		}
		if strings.TrimSpace(payload.Error) != "" && strings.TrimSpace(payload.Content) == "" {
			item, err := h.sourceSync.RecordRunItem(runID, payload.AgentID, SourceSyncItemInput{
				SourceItemKey:  payload.SourceItemID,
				IdempotencyKey: payload.IdempotencyKey,
				Outcome:        SourceItemFailed,
				Error:          trimRunes(payload.Error, 1000),
			})
			if err != nil {
				h.writeSourceSyncError(w, err)
				return
			}
			writeHTTPJSON(w, http.StatusCreated, map[string]any{"item": item})
			return
		}
		if h.sourceIngest == nil {
			writeHTTPError(w, http.StatusServiceUnavailable, "source ingestion service is not configured")
			return
		}
		receipt, err := h.sourceIngest.IngestArticle(runID, payload.AgentID, payload.SourceArticleEnvelope)
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusCreated, map[string]any{"receipt": receipt})
	case "assets":
		run, err := h.sourceSync.GetRun(runID)
		agentID := strings.TrimSpace(r.Header.Get("X-Source-Agent-ID"))
		if err != nil || run.LeaseOwner != agentID || run.Status != SourceRunRunning {
			h.writeSourceSyncError(w, ErrSourceRunLeaseOwner)
			return
		}
		data, err := readBoundedAsset(r.Body)
		if err != nil {
			writeHTTPError(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		ref, err := h.sourceAssets.Save(r.Context(), SourceAssetEnvelope{SourceItemKey: r.Header.Get("X-Source-Item-Key"), SourceURL: r.Header.Get("X-Source-URL"), SHA256: r.Header.Get("X-Content-SHA256"), ContentType: r.Header.Get("Content-Type"), Data: data})
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeHTTPJSON(w, http.StatusCreated, map[string]any{"asset": ref})
	case "complete":
		var payload struct {
			AgentID string `json:"agent_id"`
			Cursor  string `json:"cursor,omitempty"`
		}
		if !h.decodeSourceAgentJSON(w, r, &payload) {
			return
		}
		run, err := h.sourceSync.CompleteRun(runID, payload.AgentID, trimRunes(payload.Cursor, 1000))
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"run": run})
	case "fail":
		var payload struct {
			AgentID string `json:"agent_id"`
			Error   string `json:"error"`
			Cursor  string `json:"cursor,omitempty"`
		}
		if !h.decodeSourceAgentJSON(w, r, &payload) {
			return
		}
		run, err := h.sourceSync.FailRun(runID, payload.AgentID, payload.Error, trimRunes(payload.Cursor, 1000))
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"run": run})
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
	}
}

func (h *kbaseHTTPHandler) handleSourceAssetRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	hash := strings.TrimPrefix(r.URL.Path, "/api/source-assets/")
	file, err := h.sourceAssets.Open(hash)
	if err != nil {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	defer file.Close()
	head := make([]byte, 512)
	n, _ := file.Read(head)
	_, _ = file.Seek(0, 0)
	w.Header().Set("Content-Type", http.DetectContentType(head[:n]))
	w.Header().Set("Cache-Control", "private, immutable")
	http.ServeContent(w, r, hash, time.Time{}, file)
}

func (h *kbaseHTTPHandler) handleSourceSyncAdmin(w http.ResponseWriter, r *http.Request) {
	if h.sourceSync == nil {
		writeHTTPError(w, http.StatusServiceUnavailable, "source sync store is not configured")
		return
	}
	switch {
	case r.URL.Path == "/api/source-agents":
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		agents, err := h.sourceSync.ListAgents()
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"agents": agents})
	case r.URL.Path == "/api/source-subscriptions":
		h.handleSourceSubscriptions(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/source-subscriptions/"):
		h.handleSourceSubscriptionAction(w, r)
	case r.URL.Path == "/api/source-sync/runs":
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		runs, err := h.sourceSync.ListRuns(parseNonNegativeQueryInt(r, "limit", 100))
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"runs": runs})
	case strings.HasPrefix(r.URL.Path, "/api/source-sync/runs/"):
		h.handleSourceSyncRunAdmin(w, r)
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
	}
}

func (h *kbaseHTTPHandler) handleSourceSubscriptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		subscriptions, err := h.sourceSync.ListSubscriptions()
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"subscriptions": subscriptions})
	case http.MethodPost:
		var payload struct {
			ID string `json:"id,omitempty"`
			SourceSubscriptionInput
		}
		if !decodeLimitedHTTPJSON(w, r, 1<<20, &payload) {
			return
		}
		var subscription SourceSubscription
		var err error
		status := http.StatusCreated
		if strings.TrimSpace(payload.ID) == "" {
			subscription, err = h.sourceSync.CreateSubscription(payload.SourceSubscriptionInput)
		} else {
			status = http.StatusOK
			subscription, err = h.sourceSync.UpdateSubscription(payload.ID, payload.SourceSubscriptionInput)
		}
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, status, map[string]any{"subscription": subscription})
	default:
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *kbaseHTTPHandler) handleSourceSubscriptionAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	subscriptionID, action, ok := parseSourceSyncRunAction(r.URL.Path, "/api/source-subscriptions/")
	if !ok {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	switch action {
	case "sync":
		var payload struct {
			Operation string `json:"operation,omitempty"`
		}
		if !decodeLimitedHTTPJSON(w, r, 1<<20, &payload) {
			return
		}
		run, err := h.sourceSync.CreateRun(subscriptionID, payload.Operation)
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusCreated, map[string]any{"run": run})
	case "enabled":
		var payload struct {
			Enabled *bool `json:"enabled"`
		}
		if !decodeLimitedHTTPJSON(w, r, 1<<20, &payload) {
			return
		}
		if payload.Enabled == nil {
			writeHTTPError(w, http.StatusBadRequest, "enabled is required")
			return
		}
		subscription, err := h.sourceSync.SetSubscriptionEnabled(subscriptionID, *payload.Enabled)
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"subscription": subscription})
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
}

func (h *kbaseHTTPHandler) handleSourceSyncRunAdmin(w http.ResponseWriter, r *http.Request) {
	remainder := strings.TrimPrefix(r.URL.Path, "/api/source-sync/runs/")
	parts := strings.Split(strings.Trim(remainder, "/"), "/")
	if len(parts) == 0 || len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	runID, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(runID) == "" {
		writeHTTPError(w, http.StatusBadRequest, "run_id is required")
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		run, err := h.sourceSync.GetRun(runID)
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		items, err := h.sourceSync.ListRunItems(runID)
		if err != nil {
			h.writeSourceSyncError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"run": run, "items": items})
		return
	}
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var run SourceSyncRun
	switch parts[1] {
	case "retry":
		run, err = h.sourceSync.RetryRun(runID)
	case "cancel":
		run, err = h.sourceSync.CancelRun(runID)
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.writeSourceSyncError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"run": run})
}

func parseSourceSyncRunAction(path, prefix string) (string, string, bool) {
	remainder := strings.TrimPrefix(path, prefix)
	parts := strings.Split(strings.Trim(remainder, "/"), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(id) == "" {
		return "", "", false
	}
	return id, parts[1], true
}

func (h *kbaseHTTPHandler) decodeSourceAgentJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	return decodeLimitedHTTPJSON(w, r, h.sourceAgentMaxBodyBytes, value)
}

func decodeLimitedHTTPJSON(w http.ResponseWriter, r *http.Request, limit int64, value any) bool {
	defer r.Body.Close()
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(value)
	if err == nil {
		return true
	}
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeHTTPError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return false
	}
	writeHTTPError(w, http.StatusBadRequest, err.Error())
	return false
}

func (h *kbaseHTTPHandler) writeSourceSyncError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSourceRunNotFound), errors.Is(err, ErrSourceSubscriptionAbsent):
		writeHTTPError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSourceRunLeaseOwner), errors.Is(err, ErrSourceRunLeaseExpired),
		errors.Is(err, ErrSourceRunTerminal), errors.Is(err, ErrSourceRunInvalidState),
		errors.Is(err, ErrSourceRunNotRetryable), errors.Is(err, ErrSourceRunActive):
		writeHTTPError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrSourceArticleContentTooShort), errors.Is(err, ErrSourceArticleInvalidURL):
		writeHTTPError(w, http.StatusBadRequest, err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "required") ||
		strings.Contains(strings.ToLower(err.Error()), "unsupported"):
		writeHTTPError(w, http.StatusBadRequest, err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "unique constraint"):
		writeHTTPError(w, http.StatusConflict, "source subscription already exists")
	default:
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
	}
}

func decodeHTTPJSONBody(w http.ResponseWriter, r *http.Request) (any, bool) {
	defer r.Body.Close()
	var payload any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&payload); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, true
}

func parseNonNegativeQueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
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
