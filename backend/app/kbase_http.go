package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
)

const (
	sourceAgentUpdateGuardReconciliationWindow = 30 * time.Second
	sourceAgentUpdateGuardSafetyMargin         = 30 * time.Second
)

type KBaseHTTPConfig struct {
	Store                   *BookKnowledgeStore
	AuthToken               string
	ReleaseRevision         string
	BrowserSessionSecret    string
	BrowserSessions         BrowserSessionHTTPConfig
	AgentPublisherToken     string
	SystemKBExportPath      string
	StaticDir               string
	WeChat                  *WeChatSourceService
	WCPlus                  *WCPlusSourceService
	SourceSync              *SourceSyncStore
	SourceIngest            *SourceIngestService
	SourceAgentToken        string
	SourceAgentMaxBodyBytes int64
	SourceArtifacts         *SourceAgentArtifactCatalog
	SourceAssets            *SourceAssetStore
	AnalysisGenerator       BookAnalysisGenerator
	ChatClient              BookKnowledgeLLMClient
	DedaoLibrary            DedaoLibraryService
	DedaoAuth               DedaoAuthProvider
	DedaoEbooks             DedaoEbookAcquisitionService
	DedaoEbookVerifyTimeout time.Duration
	ReverificationNow       func() time.Time
	ReverificationCooldown  time.Duration
	AgentTools              []string
	AuditCoordinator        EvidenceAuditEnqueuer
	AuditMaxBodyBytes       int64
	AuditUnavailableReason  string
	AuditRetrySigningKey    []byte
	AuditRetryTTL           time.Duration
	AuditNow                func() time.Time
	AuditLogger             func(EvidenceAuditHTTPLogEvent)
	ProofroomDelivery       *ProofroomDeliveryService
}
type EvidenceAuditHTTPLogEvent struct {
	Operation string
	Code      string
	Cause     string
}
type EvidenceAuditEnqueuer interface {
	Enqueue(string) error
}
type BookAnalysisGenerator func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error)

type bookKnowledgeJobHTTPView struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Status       BookKnowledgeJobStatus `json:"status"`
	EbookID      int                    `json:"ebook_id"`
	EbookEnID    string                 `json:"ebook_enid"`
	DownloadType int                    `json:"download_type"`
	Result       map[string]any         `json:"result,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Logs         []string               `json:"logs,omitempty"`
	RetryOf      string                 `json:"retry_of,omitempty"`
	Stage        string                 `json:"stage,omitempty"`
	FailureCode  string                 `json:"failure_code,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
	StartedAt    string                 `json:"started_at,omitempty"`
	FinishedAt   string                 `json:"finished_at,omitempty"`
}

type DedaoLibraryService interface {
	CourseList(category, order string, page, limit int) (*services.CourseList, error)
	CourseInfo(enid string) (*services.CourseInfo, error)
	ArticleList(enid, chapterID string, count, maxID int) (*services.ArticleList, error)
	ArticleDetail(enid string) (*services.ArticleDetail, error)
	AudioDetail(enid string) (*services.AudioInfoResp, error)
	OdobArticleDetail(aliasID string) (*services.ArticleDetail, error)
}

type kbaseHTTPHandler struct {
	store                   *BookKnowledgeStore
	authToken               string
	releaseRevision         string
	browserSessionSecret    string
	browserSessions         BrowserSessionHTTPConfig
	agentPublisherToken     string
	systemKBExportPath      string
	staticDir               string
	wechat                  *WeChatSourceService
	wcplus                  *WCPlusSourceService
	sourceSync              *SourceSyncStore
	sourceIngest            *SourceIngestService
	sourceAgentToken        string
	sourceAgentMaxBodyBytes int64
	sourceArtifacts         *SourceAgentArtifactCatalog
	sourceAssets            *SourceAssetStore
	analysisGenerator       BookAnalysisGenerator
	chatClient              BookKnowledgeLLMClient
	dedaoLibrary            DedaoLibraryService
	dedaoAuth               DedaoAuthProvider
	dedaoEbooks             DedaoEbookAcquisitionService
	dedaoEbookVerifyTimeout time.Duration
	reverificationNow       func() time.Time
	reverificationCooldown  time.Duration
	agentTools              []string
	auditCoordinator        EvidenceAuditEnqueuer
	auditMaxBodyBytes       int64
	auditUnavailableReason  string
	auditRetrySigningKey    []byte
	auditRetryTTL           time.Duration
	auditNow                func() time.Time
	auditLogger             func(EvidenceAuditHTTPLogEvent)
	proofroomDelivery       *ProofroomDeliveryService
}

const defaultSourceAgentMaxBodyBytes int64 = 8 << 20
const defaultSourceAgentCommandHTTPMaxBodyBytes int64 = 64 << 10
const defaultEvidenceAuditMaxBodyBytes int64 = 64 << 10
const defaultAgentCompilationMaxBodyBytes int64 = 64 << 10
const defaultDedaoEbookVerificationTimeout = 15 * time.Second

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
	releaseRevision := strings.TrimSpace(cfg.ReleaseRevision)
	if releaseRevision == "" {
		releaseRevision = "development"
	}
	browserSessionSecret := strings.TrimSpace(cfg.BrowserSessionSecret)
	browserSessions := normalizeBrowserSessionHTTPConfig(cfg.BrowserSessions)
	agentPublisherToken := strings.TrimSpace(cfg.AgentPublisherToken)
	sourceAgentToken := strings.TrimSpace(cfg.SourceAgentToken)
	if browserSessionSecret != "" &&
		(browserSessionSecret == authToken ||
			browserSessionSecret == agentPublisherToken ||
			browserSessionSecret == sourceAgentToken) {
		browserSessionSecret = ""
	}
	if agentPublisherToken != "" && agentPublisherToken == authToken {
		agentPublisherToken = ""
	}
	if agentPublisherToken != "" && agentPublisherToken == sourceAgentToken {
		agentPublisherToken = ""
	}
	if authToken != "" && sourceAgentToken == authToken {
		sourceAgentToken = ""
	}
	if browserSessions.AdminToken != "" &&
		(browserSessions.AdminToken == authToken ||
			browserSessions.AdminToken == sourceAgentToken ||
			browserSessions.AdminToken == agentPublisherToken ||
			browserSessions.AdminToken == browserSessionSecret) {
		browserSessions.AdminToken = ""
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
	agentTools := uniqueTrimmedStrings(cfg.AgentTools)
	if len(agentTools) == 0 {
		agentTools = AgentReadOnlyToolIDs()
	}
	auditMaxBodyBytes := cfg.AuditMaxBodyBytes
	if auditMaxBodyBytes <= 0 {
		auditMaxBodyBytes = defaultEvidenceAuditMaxBodyBytes
	}
	auditRetryTTL := cfg.AuditRetryTTL
	if auditRetryTTL <= 0 {
		auditRetryTTL = 5 * time.Minute
	}
	auditNow := cfg.AuditNow
	if auditNow == nil {
		auditNow = time.Now
	}
	auditLogger := cfg.AuditLogger
	if auditLogger == nil {
		auditLogger = func(EvidenceAuditHTTPLogEvent) {}
	}
	dedaoEbookVerifyTimeout := cfg.DedaoEbookVerifyTimeout
	if dedaoEbookVerifyTimeout <= 0 {
		dedaoEbookVerifyTimeout = defaultDedaoEbookVerificationTimeout
	}
	return &kbaseHTTPHandler{
		store:                   store,
		authToken:               authToken,
		releaseRevision:         releaseRevision,
		browserSessionSecret:    browserSessionSecret,
		browserSessions:         browserSessions,
		agentPublisherToken:     agentPublisherToken,
		systemKBExportPath:      strings.TrimSpace(cfg.SystemKBExportPath),
		staticDir:               strings.TrimSpace(cfg.StaticDir),
		wechat:                  cfg.WeChat,
		wcplus:                  cfg.WCPlus,
		sourceSync:              cfg.SourceSync,
		sourceIngest:            sourceIngest,
		sourceAgentToken:        sourceAgentToken,
		sourceAgentMaxBodyBytes: maxBodyBytes,
		sourceArtifacts:         cfg.SourceArtifacts,
		sourceAssets:            assets,
		analysisGenerator:       analysisGenerator,
		chatClient:              cfg.ChatClient,
		dedaoLibrary:            dedaoLibrary,
		dedaoAuth:               defaultDedaoAuthProvider(cfg.DedaoAuth),
		dedaoEbooks:             defaultDedaoEbookAcquisitionService(cfg.DedaoEbooks),
		dedaoEbookVerifyTimeout: dedaoEbookVerifyTimeout,
		reverificationNow:       reverificationNow,
		reverificationCooldown:  reverificationCooldown,
		agentTools:              agentTools,
		auditCoordinator:        cfg.AuditCoordinator,
		auditMaxBodyBytes:       auditMaxBodyBytes,
		auditUnavailableReason:  strings.TrimSpace(cfg.AuditUnavailableReason),
		auditRetrySigningKey:    append([]byte(nil), cfg.AuditRetrySigningKey...),
		auditRetryTTL:           auditRetryTTL,
		auditNow:                auditNow,
		auditLogger:             auditLogger,
		proofroomDelivery:       cfg.ProofroomDelivery,
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

func (defaultDedaoLibrary) ArticleDetail(enid string) (*services.ArticleDetail, error) {
	return ArticleDetail(enid)
}

func (defaultDedaoLibrary) AudioDetail(enid string) (*services.AudioInfoResp, error) {
	return AudioDetail(enid)
}

func (defaultDedaoLibrary) OdobArticleDetail(aliasID string) (*services.ArticleDetail, error) {
	return OdobArticleDetail(aliasID)
}

func (h *kbaseHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.handleBrowserSessionAdminRoute(w, r) {
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && h.applyCORS(w, r) && r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/health" {
		w.Header().Set("Cache-Control", "no-store")
		writeHTTPJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"service":  "dedao-kbase",
			"revision": h.releaseRevision,
		})
		return
	}
	if h.handleBrowserSessionRoute(w, r) {
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
	if r.URL.Path == "/api/agent-packages/publish" ||
		r.URL.Path == "/api/agent-packages/evaluate" {
		if h.agentPublisherToken == "" {
			writeHTTPError(w, http.StatusServiceUnavailable, "agent package publisher API is not configured")
			return
		}
		if !authorizeBearerToken(w, r, h.agentPublisherToken) {
			return
		}
		h.handleAgentPackages(w, r)
		return
	}
	if h.handleBrowserSessionAPIRoute(w, r) {
		return
	}
	auditAPI := isEvidenceAuditAPIPath(r.URL.Path)
	unsafe := isUnsafeKBaseRequestMethod(r.Method)
	auth, authorized := h.authorizeKBaseRequest(w, r, auditAPI, !unsafe)
	if !authorized {
		return
	}
	if auth.Method == kbaseAuthMethodCookie && unsafe {
		if !h.authorizeBrowserSessionCSRF(w, r, auth, auditAPI) {
			return
		}
		auth, authorized = h.renewBrowserSessionAfterCSRF(w, auth, auditAPI)
		if !authorized {
			return
		}
	}
	r = requestWithKBaseAuth(r, auth)
	if strings.HasPrefix(r.URL.Path, "/api/controlled-agent/") {
		if h.agentPublisherToken == "" {
			writeHTTPError(w, http.StatusServiceUnavailable, "controlled Agent publisher is not configured")
			return
		}
		if auth.Method != kbaseAuthMethodCookie {
			writeHTTPError(w, http.StatusUnauthorized, "controlled Agent requires an authorized browser session")
			return
		}
		h.handleControlledAgentWorkflow(w, r)
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
	if bookID, ok := bookNestedPathID(r.URL.Path, "repair-content-hash"); ok {
		h.handleBookContentHashRepair(w, r, bookID)
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
	if r.URL.Path == "/api/knowledge/operations" {
		h.handleKnowledgeOperations(w, r)
		return
	}
	if r.URL.Path == "/api/knowledge/operations/replay" {
		h.handleKnowledgeOperationsReplay(w, r)
		return
	}
	if r.URL.Path == "/api/knowledge/readiness" {
		h.handleKnowledgeReadiness(w, r)
		return
	}
	if r.URL.Path == "/api/knowledge/assembly" {
		h.handleKnowledgeAssembly(w, r)
		return
	}
	if r.URL.Path == "/api/knowledge/pipeline" {
		h.handleKnowledgePipeline(w, r)
		return
	}
	if r.URL.Path == "/api/knowledge/pipeline/run" {
		h.handleKnowledgePipelineRun(w, r)
		return
	}
	if packageID, ok := agentPackageAuditCollectionPathID(r.URL.Path); ok {
		h.handleAgentPackageAudits(w, r, packageID)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/agent-traces/") {
		h.handleAgentTrace(w, r)
		return
	}
	if r.URL.Path == "/api/agent-audits" || strings.HasPrefix(r.URL.Path, "/api/agent-audits/") {
		h.handleEvidenceAudits(w, r)
		return
	}
	if r.URL.Path == "/api/agent-packages" || strings.HasPrefix(r.URL.Path, "/api/agent-packages/") {
		h.handleAgentPackages(w, r)
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
	if r.URL.Path == "/api/context-chat" {
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleContextChat(w, r)
		return
	}
	if r.URL.Path == "/api/jobs" {
		h.handleBookKnowledgeJobs(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/jobs/") {
		jobID, action, ok := parseBookKnowledgeJobRoute(r.URL.EscapedPath())
		if !ok {
			writeHTTPError(w, http.StatusNotFound, "not found")
			return
		}
		switch action {
		case "":
			h.handleGetBookKnowledgeJob(w, r, jobID)
		case "retry":
			h.handleRetryBookKnowledgeJob(w, r, jobID)
		default:
			writeHTTPError(w, http.StatusNotFound, "not found")
		}
		return
	}
	if r.URL.Path == "/api/dedao/session" {
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleDedaoSession(w)
		return
	}
	if r.URL.Path == "/api/dedao/auth/qrcode" {
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleDedaoAuthQRCode(w)
		return
	}
	if r.URL.Path == "/api/dedao/auth/check" {
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleDedaoAuthCheck(w, r)
		return
	}
	if r.URL.Path == "/api/dedao/search/ebooks" {
		h.handleDedaoEbookSearch(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/dedao/ebooks/") && strings.HasSuffix(r.URL.Path, "/bookshelf") {
		h.handleDedaoEbookBookshelf(w, r)
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
	if r.URL.Path == "/api/dedao/course/articles" {
		h.handleDedaoCourseArticles(w, r)
		return
	}
	if r.URL.Path == "/api/dedao/article" {
		h.handleDedaoArticle(w, r)
		return
	}
	if r.URL.Path == "/api/dedao/audio" {
		h.handleDedaoAudio(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch {
	case r.URL.Path == "/api/books":
		h.handleListBooks(w)
	case strings.HasPrefix(r.URL.Path, "/api/citations/"):
		h.handleGetCitation(w, r)
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

func (h *kbaseHTTPHandler) handleDedaoSession(w http.ResponseWriter) {
	setHTTPNoStore(w)
	writeHTTPJSON(w, http.StatusOK, h.dedaoAuth.Session())
}

func (h *kbaseHTTPHandler) handleDedaoAuthQRCode(w http.ResponseWriter) {
	setHTTPNoStore(w)
	qr, err := h.dedaoAuth.NewQRCode()
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, "failed to create dedao login qrcode")
		return
	}
	writeHTTPJSON(w, http.StatusOK, qr)
}

func (h *kbaseHTTPHandler) handleDedaoAuthCheck(w http.ResponseWriter, r *http.Request) {
	setHTTPNoStore(w)
	defer r.Body.Close()
	var request DedaoLoginCheckRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	request.Token = strings.TrimSpace(request.Token)
	request.QRCodeString = strings.TrimSpace(request.QRCodeString)
	if request.Token == "" || request.QRCodeString == "" {
		writeHTTPError(w, http.StatusBadRequest, "token and qr_code_string are required")
		return
	}
	result, err := h.dedaoAuth.CheckLogin(request.Token, request.QRCodeString)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, "failed to verify dedao login")
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func setHTTPNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func (h *kbaseHTTPHandler) handleBookKnowledgeJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, 100)
		jobs, err := h.store.ListBookKnowledgeJobs(limit)
		if err != nil {
			writeHTTPError(w, http.StatusInternalServerError, "failed to list jobs")
			return
		}
		views := make([]bookKnowledgeJobHTTPView, 0, len(jobs))
		for _, job := range jobs {
			views = append(views, newBookKnowledgeJobHTTPView(job))
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"jobs": views})
	case http.MethodPost:
		var request BookKnowledgeJobRequest
		if !decodeStrictLimitedHTTPJSON(w, r, 64<<10, &request) {
			return
		}
		normalized, err := normalizeBookKnowledgeJobRequest(request)
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		detail, err := h.dedaoEbookDetailForJobVerification(r.Context(), normalized.EbookEnID)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				writeHTTPError(w, http.StatusGatewayTimeout, "dedao ebook verification timed out")
				return
			}
			writeHTTPError(w, http.StatusBadGateway, "failed to verify dedao ebook ownership")
			return
		}
		if detail == nil {
			writeHTTPError(w, http.StatusNotFound, "ebook not found")
			return
		}
		detailEnID := strings.TrimSpace(detail.Enid)
		if detail.ID <= 0 || detailEnID == "" {
			writeHTTPError(w, http.StatusBadGateway, "unable to verify dedao ebook identity")
			return
		}
		if detail.ID != normalized.EbookID || detailEnID != normalized.EbookEnID {
			writeHTTPError(w, http.StatusBadRequest, "ebook identity does not match request")
			return
		}
		if !detail.IsBuy && !detail.IsOnBookshelf {
			writeHTTPError(w, http.StatusForbidden, "ebook is not owned or on the active bookshelf")
			return
		}
		job, err := h.store.CreateBookKnowledgeJob(normalized)
		if err != nil {
			writeHTTPError(w, http.StatusInternalServerError, "failed to create job")
			return
		}
		writeHTTPJSON(w, http.StatusAccepted, map[string]any{"job": newBookKnowledgeJobHTTPView(job)})
	default:
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func parseBookKnowledgeJobRoute(path string) (string, string, bool) {
	const prefix = "/api/jobs/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		return "", "", false
	}
	jobID, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(jobID) == "" || strings.Contains(jobID, "/") {
		return "", "", false
	}
	action := ""
	if len(parts) == 2 {
		action = parts[1]
		if action == "" {
			return "", "", false
		}
	}
	return jobID, action, true
}

func (h *kbaseHTTPHandler) handleRetryBookKnowledgeJob(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	original, err := h.store.LoadBookKnowledgeJob(jobID)
	if err != nil {
		if errors.Is(err, ErrBookKnowledgeJobNotFound) {
			writeHTTPError(w, http.StatusNotFound, "job not found")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, "failed to load job")
		return
	}
	if original.Status != BookKnowledgeJobStatusFailed && original.Status != BookKnowledgeJobStatusInterrupted {
		writeHTTPError(w, http.StatusConflict, "job is not eligible for retry")
		return
	}
	detail, err := h.dedaoEbookDetailForJobVerification(r.Context(), original.EbookEnID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeHTTPError(w, http.StatusGatewayTimeout, "dedao ebook verification timed out")
			return
		}
		writeHTTPError(w, http.StatusBadGateway, "failed to verify dedao ebook ownership")
		return
	}
	if detail == nil {
		writeHTTPError(w, http.StatusNotFound, "ebook not found")
		return
	}
	detailEnID := strings.TrimSpace(detail.Enid)
	if detail.ID <= 0 || detailEnID == "" {
		writeHTTPError(w, http.StatusBadGateway, "unable to verify dedao ebook identity")
		return
	}
	if detail.ID != original.EbookID || detailEnID != original.EbookEnID {
		writeHTTPError(w, http.StatusConflict, "ebook identity no longer matches original job")
		return
	}
	if !detail.IsBuy && !detail.IsOnBookshelf {
		writeHTTPError(w, http.StatusForbidden, "ebook is not owned or on the active bookshelf")
		return
	}
	retry, err := h.store.RetryBookKnowledgeJob(original.ID)
	if err != nil {
		switch {
		case errors.Is(err, ErrBookKnowledgeJobConflict):
			writeHTTPError(w, http.StatusConflict, "an active retry is already queued or running")
		case errors.Is(err, ErrBookKnowledgeJobInvalidState):
			writeHTTPError(w, http.StatusConflict, "job is not eligible for retry")
		case errors.Is(err, ErrBookKnowledgeJobNotFound):
			writeHTTPError(w, http.StatusNotFound, "job not found")
		default:
			writeHTTPError(w, http.StatusInternalServerError, "failed to retry job")
		}
		return
	}
	writeHTTPJSON(w, http.StatusCreated, map[string]any{"job": newBookKnowledgeJobHTTPView(retry)})
}

func (h *kbaseHTTPHandler) dedaoEbookDetailForJobVerification(ctx context.Context, enid string) (*services.EbookDetail, error) {
	verificationCtx, cancel := context.WithTimeout(ctx, h.dedaoEbookVerifyTimeout)
	defer cancel()
	detail, err := dedaoEbookDetailWithServiceContext(verificationCtx, h.dedaoEbooks, getService(), enid)
	if errors.Is(verificationCtx.Err(), context.DeadlineExceeded) {
		return nil, context.DeadlineExceeded
	}
	return detail, err
}

func (h *kbaseHTTPHandler) handleGetBookKnowledgeJob(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	job, err := h.store.LoadBookKnowledgeJob(jobID)
	if err != nil {
		if errors.Is(err, ErrBookKnowledgeJobNotFound) {
			writeHTTPError(w, http.StatusNotFound, "job not found")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, "failed to load job")
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"job": newBookKnowledgeJobHTTPView(job)})
}

func newBookKnowledgeJobHTTPView(job BookKnowledgeJob) bookKnowledgeJobHTTPView {
	failureCode := ""
	errorMessage := ""
	switch job.Status {
	case BookKnowledgeJobStatusInterrupted:
		failureCode = BookKnowledgeJobFailureWorkerInterrupted
		errorMessage = bookKnowledgeJobFailureMessages[failureCode]
	case BookKnowledgeJobStatusFailed:
		if message, ok := bookKnowledgeJobFailureMessages[job.FailureCode]; ok {
			failureCode = job.FailureCode
			errorMessage = message
		} else {
			errorMessage = sanitizeBookKnowledgeJobError(job.Error)
		}
	}
	return bookKnowledgeJobHTTPView{
		ID: job.ID, Type: job.Type, Status: job.Status,
		EbookID: job.EbookID, EbookEnID: job.EbookEnID, DownloadType: job.DownloadType,
		Result: safeLegacyBookKnowledgeJobResult(job, job.Result), Error: errorMessage,
		Logs: safeLegacyBookKnowledgeJobLogs(job.Status, job.Logs), RetryOf: job.RetryOf,
		Stage: bookKnowledgeJobStage(job), FailureCode: failureCode,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
		StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
	}
}

func (h *kbaseHTTPHandler) handleDedaoEbookSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := parseBoundedInt(r.URL.Query().Get("page"), 1, 1, 10000)
	pageSize := parseBoundedInt(r.URL.Query().Get("page_size"), 30, 1, 100)
	result, err := h.dedaoEbooks.SearchEbooks(query, page, pageSize)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, "failed to search dedao ebooks")
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func (h *kbaseHTTPHandler) handleDedaoEbookBookshelf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	const prefix = "/api/dedao/ebooks/"
	middle := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), "/bookshelf")
	if middle == "" || strings.Contains(middle, "/") {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	enid, err := url.PathUnescape(middle)
	if err != nil || strings.TrimSpace(enid) == "" || strings.Contains(enid, "/") {
		writeHTTPError(w, http.StatusBadRequest, "invalid ebook enid")
		return
	}
	result, err := h.dedaoEbooks.AddEbookToBookshelf(enid)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, "failed to add dedao ebook to bookshelf")
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
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

func (h *kbaseHTTPHandler) handleDedaoCourseArticles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	enid := strings.TrimSpace(r.URL.Query().Get("enid"))
	if enid == "" {
		writeHTTPError(w, http.StatusBadRequest, "missing enid")
		return
	}
	count := parseBoundedInt(r.URL.Query().Get("count"), 30, 1, 100)
	maxID := parseBoundedInt(r.URL.Query().Get("max_id"), 0, 0, int(^uint(0)>>1))
	articles, err := h.dedaoLibrary.ArticleList(enid, "", count, maxID)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	if articles == nil {
		articles = &services.ArticleList{}
	}
	writeHTTPJSON(w, http.StatusOK, articles)
}

func (h *kbaseHTTPHandler) handleDedaoArticle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	enid := strings.TrimSpace(r.URL.Query().Get("enid"))
	if enid == "" {
		writeHTTPError(w, http.StatusBadRequest, "missing enid")
		return
	}
	detail, err := h.dedaoLibrary.ArticleDetail(enid)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	if detail == nil {
		writeHTTPError(w, http.StatusNotFound, "article not found")
		return
	}
	var contents []services.Content
	if err := json.Unmarshal([]byte(detail.Content), &contents); err != nil {
		writeHTTPError(w, http.StatusBadGateway, fmt.Sprintf("parse article content: %v", err))
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"detail":   detail,
		"markdown": ContentsToMarkdown(contents),
	})
}

func (h *kbaseHTTPHandler) handleDedaoAudio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	enid := strings.TrimSpace(r.URL.Query().Get("enid"))
	if enid == "" {
		writeHTTPError(w, http.StatusBadRequest, "missing enid")
		return
	}
	detail, err := h.dedaoLibrary.AudioDetail(enid)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	if detail == nil {
		writeHTTPError(w, http.StatusNotFound, "audio not found")
		return
	}
	aliasID := strings.TrimSpace(r.URL.Query().Get("alias_id"))
	if aliasID == "" {
		aliasID = strings.TrimSpace(detail.AudioInfo.AudioID)
	}
	markdown := ""
	transcriptError := ""
	if aliasID != "" {
		transcript, transcriptErr := h.dedaoLibrary.OdobArticleDetail(aliasID)
		if transcriptErr != nil {
			transcriptError = transcriptErr.Error()
		} else if transcript != nil {
			var contents []services.Content
			if parseErr := json.Unmarshal([]byte(transcript.Content), &contents); parseErr != nil {
				transcriptError = fmt.Sprintf("parse audio transcript: %v", parseErr)
			} else {
				markdown = ContentsToMarkdown(contents)
			}
		}
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"detail":           detail.AudioInfo,
		"quality":          detail.Quality,
		"alias_id":         aliasID,
		"markdown":         markdown,
		"transcript_error": transcriptError,
	})
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

func (h *kbaseHTTPHandler) handleBookContentHashRepair(w http.ResponseWriter, r *http.Request, bookID string) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&request); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !request.Confirm {
		writeHTTPError(w, http.StatusBadRequest, "explicit confirmation is required")
		return
	}
	pkg, err := h.store.RepairMissingBookContentHash(bookID)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "book not found") {
			writeHTTPError(w, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "already has content hash") {
			writeHTTPError(w, http.StatusConflict, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"book_id": pkg.Book.BookID, "content_hash": pkg.Book.ContentHash, "repaired": true,
	})
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
	var releases []KnowledgeReleaseRecord
	var err error
	if r.URL.Query().Get("latest") == "true" {
		releases, err = h.store.ListLatestKnowledgeReleasesForBook(
			r.URL.Query().Get("after"),
			limit,
			r.URL.Query().Get("book_id"),
		)
	} else {
		releases, err = h.store.ListKnowledgeReleasesForBook(
			r.URL.Query().Get("after"),
			limit,
			r.URL.Query().Get("book_id"),
		)
	}
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

type AgentPackagePublishRequest struct {
	IdempotencyKey string       `json:"idempotency_key"`
	Package        AgentPackage `json:"package"`
}

type AgentPackageEvaluationRequest struct {
	Package AgentPackage         `json:"package"`
	Suite   AgentEvaluationSuite `json:"suite"`
}

type ControlledAgentWorkflowRequest struct {
	Draft          ControlledAgentDraftRequest `json:"draft"`
	IdempotencyKey string                      `json:"idempotency_key,omitempty"`
	Confirm        bool                        `json:"confirm,omitempty"`
}

func (h *kbaseHTTPHandler) handleControlledAgentWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	defer r.Body.Close()
	var request ControlledAgentWorkflowRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	draft, err := BuildControlledAgentPackageDraft(h.store, request.Draft, h.agentTools)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch r.URL.Path {
	case "/api/controlled-agent/draft":
		writeHTTPJSON(w, http.StatusOK, draft)
	case "/api/controlled-agent/evaluate":
		report, created, err := h.evaluateControlledAgentDraft(*draft)
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeHTTPJSON(w, status, map[string]any{"created": created, "draft": draft, "evaluation": report})
	case "/api/controlled-agent/publish":
		if !request.Confirm {
			writeHTTPError(w, http.StatusBadRequest, "explicit confirmation is required")
			return
		}
		if strings.TrimSpace(request.IdempotencyKey) == "" {
			writeHTTPError(w, http.StatusBadRequest, "idempotency_key is required")
			return
		}
		report, _, err := h.evaluateControlledAgentDraft(*draft)
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !report.Passed {
			writeHTTPError(w, http.StatusConflict, "controlled Agent evaluation did not pass")
			return
		}
		published, created, err := PublishAgentPackage(h.store, draft.Package, request.IdempotencyKey, h.agentTools, time.Now())
		if err != nil {
			writeHTTPError(w, http.StatusConflict, err.Error())
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeHTTPJSON(w, status, map[string]any{"created": created, "evaluation": report, "package": published})
	default:
		writeHTTPError(w, http.StatusNotFound, "controlled Agent operation not found")
	}
}

func (h *kbaseHTTPHandler) evaluateControlledAgentDraft(draft ControlledAgentDraft) (*AgentEvaluationReport, bool, error) {
	if existing, err := h.store.LoadAgentPackageEvaluation(draft.Package.ContentHash); err == nil {
		storedSuite, suiteErr := h.store.LoadAgentPackageEvaluationSuite(draft.Package.ContentHash)
		if suiteErr != nil {
			return nil, false, suiteErr
		}
		if !reflect.DeepEqual(*storedSuite, draft.Suite) {
			return nil, false, fmt.Errorf("agent package evaluation suite is immutable for this content hash")
		}
		return existing, false, nil
	} else if !os.IsNotExist(err) {
		return nil, false, err
	}
	report, err := EvaluateAgentPackageDeterministically(h.store, draft.Package, draft.Suite, time.Now())
	if err != nil {
		return nil, false, err
	}
	if err := h.store.SaveAgentPackageEvaluation(draft.Package, draft.Suite, report); err != nil {
		return nil, false, err
	}
	return &report, true, nil
}

type AgentPackageTrustedEvaluationSuiteRequest struct {
	Package AgentPackage         `json:"package"`
	Suite   AgentEvaluationSuite `json:"suite"`
}

type evidenceAuditCreateRequest struct {
	Subject        string   `json:"subject"`
	Scope          string   `json:"scope"`
	SelectedClaims []string `json:"selected_claims,omitempty"`
	IdempotencyKey string   `json:"idempotency_key"`
}

func agentPackageAuditCollectionPathID(path string) (string, bool) {
	const prefix = "/api/agent-packages/"
	const suffix = "/audits"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	rawID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if rawID == "" || strings.Contains(rawID, "/") {
		return "", false
	}
	packageID, err := url.PathUnescape(rawID)
	if err != nil || strings.TrimSpace(packageID) == "" {
		return "", false
	}
	return packageID, true
}

func isEvidenceAuditAPIPath(path string) bool {
	if path == "/api/agent-audits" || strings.HasPrefix(path, "/api/agent-audits/") {
		return true
	}
	if strings.HasPrefix(path, "/api/agent-traces/") {
		return true
	}
	_, ok := agentPackageAuditCollectionPathID(path)
	return ok
}

func (h *kbaseHTTPHandler) handleAgentTrace(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/agent-traces/"
	if r.Method != http.MethodGet {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusMethodNotAllowed, "audit_method_not_allowed",
			"method not allowed", "load_trace", nil,
		)
		return
	}
	rawID := strings.TrimPrefix(r.URL.Path, prefix)
	if rawID == "" || strings.Contains(rawID, "/") {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusNotFound, "trace_not_found",
			"agent trace not found", "load_trace", nil,
		)
		return
	}
	traceID, err := url.PathUnescape(rawID)
	if err != nil || strings.TrimSpace(traceID) == "" {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"invalid trace_id", "load_trace", nil,
		)
		return
	}
	trace, err := h.store.LoadAgentTrace(traceID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusNotFound, "trace_not_found",
				"agent trace not found", "load_trace", nil,
			)
			return
		}
		h.writeEvidenceAuditHTTPError(
			w, http.StatusInternalServerError, "audit_store_unavailable",
			"agent trace storage is unavailable", "load_trace", err,
		)
		return
	}
	writeHTTPJSON(w, http.StatusOK, trace)
}

func (h *kbaseHTTPHandler) handleAgentPackageAudits(w http.ResponseWriter, r *http.Request, packageID string) {
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	if version == "" {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"version is required", "validate_audit_request", nil,
		)
		return
	}
	if !agentPackageIDPattern.MatchString(packageID) || !agentPackageIDPattern.MatchString(version) {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"invalid package_id or version", "validate_audit_request", nil,
		)
		return
	}
	if r.Method == http.MethodGet {
		limit := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, 200)
		records, err := h.store.ListEvidenceAudits(packageID, version, limit)
		if err != nil {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusInternalServerError, "audit_store_unavailable",
				"evidence audit storage is unavailable", "list_package_audits", err,
			)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"audits": records})
		return
	}
	if r.Method != http.MethodPost {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusMethodNotAllowed, "audit_method_not_allowed",
			"method not allowed", "create_audit", nil,
		)
		return
	}
	if h.auditCoordinator == nil {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusServiceUnavailable, "audit_service_unavailable",
			"evidence audit service is unavailable", "create_audit", nil,
		)
		return
	}
	defer r.Body.Close()
	var request evidenceAuditCreateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, h.auditMaxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusRequestEntityTooLarge, "audit_request_too_large",
				"request body is too large", "decode_audit_request", nil,
			)
			return
		}
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"invalid JSON body", "decode_audit_request", nil,
		)
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"invalid JSON body", "decode_audit_request", nil,
		)
		return
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"idempotency_key is required", "validate_audit_request", nil,
		)
		return
	}
	input, err := PrepareEvidenceAuditInput(
		h.store, packageID, version, request.Subject, request.Scope,
	)
	if err != nil {
		h.writeEvidenceAuditCreateError(w, err)
		return
	}
	if len(request.SelectedClaims) > 0 {
		if len(request.SelectedClaims) > input.EvidencePolicy.MaxClaims {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusUnprocessableEntity, "audit_policy_violation",
				"selected_claims exceeds package evidence policy", "validate_audit_request", nil,
			)
			return
		}
		allowed := make(map[string]struct{}, len(input.SelectedClaims))
		for _, claim := range input.SelectedClaims {
			allowed[claim] = struct{}{}
		}
		selected := make([]string, 0, len(request.SelectedClaims))
		seen := map[string]struct{}{}
		for _, claim := range request.SelectedClaims {
			claim = strings.TrimSpace(claim)
			if _, ok := allowed[claim]; !ok {
				h.writeEvidenceAuditHTTPError(
					w, http.StatusUnprocessableEntity, "audit_policy_violation",
					"selected_claims contains a claim outside the package primary release",
					"validate_audit_request", nil,
				)
				return
			}
			if _, duplicate := seen[claim]; duplicate {
				h.writeEvidenceAuditHTTPError(
					w, http.StatusUnprocessableEntity, "audit_policy_violation",
					"selected_claims contains duplicates", "validate_audit_request", nil,
				)
				return
			}
			seen[claim] = struct{}{}
			selected = append(selected, claim)
		}
		input.SelectedClaims = selected
	}
	audit, created, err := CreateEvidenceAudit(h.store, input, request.IdempotencyKey, h.auditNow())
	if err != nil {
		if errors.Is(err, ErrEvidenceAuditIdempotencyConflict) {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusConflict, "audit_idempotency_conflict",
				"idempotency key conflicts with a different audit request", "create_audit", err,
			)
			return
		}
		h.writeEvidenceAuditCreateError(w, err)
		return
	}
	if audit.Status == EvidenceAuditQueued || audit.Status == EvidenceAuditRunning {
		if err := h.auditCoordinator.Enqueue(audit.AuditID); err != nil {
			code := "audit_coordinator_unavailable"
			message := "evidence audit coordinator is unavailable"
			if errors.Is(err, ErrEvidenceAuditQueueFull) {
				code = "audit_queue_full"
				message = "evidence audit queue is full; retry later"
			}
			h.writeEvidenceAuditHTTPError(
				w, http.StatusServiceUnavailable, code, message, "enqueue_audit", err,
			)
			return
		}
	}
	writeHTTPJSON(w, http.StatusAccepted, map[string]any{"created": created, "audit": audit})
}

func (h *kbaseHTTPHandler) handleEvidenceAudits(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/agent-audits/"
	if r.URL.Path == "/api/agent-audits" {
		if r.Method != http.MethodGet {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusMethodNotAllowed, "audit_method_not_allowed",
				"method not allowed", "list_audits", nil,
			)
			return
		}
		limit := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, 200)
		records, err := h.store.ListEvidenceAudits(
			strings.TrimSpace(r.URL.Query().Get("package_id")),
			strings.TrimSpace(r.URL.Query().Get("version")),
			limit,
		)
		if err != nil {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusInternalServerError, "audit_store_unavailable",
				"evidence audit storage is unavailable", "list_audits", err,
			)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"audits": records})
		return
	}
	if !strings.HasPrefix(r.URL.Path, prefix) {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusNotFound, "audit_not_found",
			"evidence audit not found", "load_audit", nil,
		)
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, prefix)
	if strings.HasSuffix(remainder, "/proofroom") {
		rawID := strings.TrimSuffix(remainder, "/proofroom")
		if rawID == "" || strings.Contains(rawID, "/") {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusNotFound, "audit_not_found",
				"evidence audit not found", "proofroom_projection", nil,
			)
			return
		}
		h.handleEvidenceAuditProofroom(w, r, rawID)
		return
	}
	if strings.HasSuffix(remainder, "/retry") {
		rawID := strings.TrimSuffix(remainder, "/retry")
		if rawID == "" || strings.Contains(rawID, "/") {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusNotFound, "audit_not_found",
				"evidence audit not found", "retry_audit", nil,
			)
			return
		}
		h.handleEvidenceAuditRetry(w, r, rawID)
		return
	}
	if r.Method != http.MethodGet {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusMethodNotAllowed, "audit_method_not_allowed",
			"method not allowed", "load_audit", nil,
		)
		return
	}
	if remainder == "" || strings.Contains(remainder, "/") {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusNotFound, "audit_not_found",
			"evidence audit not found", "load_audit", nil,
		)
		return
	}
	auditID, err := url.PathUnescape(remainder)
	if err != nil {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"invalid audit_id", "load_audit", nil,
		)
		return
	}
	audit, err := h.store.LoadEvidenceAudit(auditID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusNotFound, "audit_not_found",
				"evidence audit not found", "load_audit", nil,
			)
			return
		}
		h.writeEvidenceAuditHTTPError(
			w, http.StatusInternalServerError, "audit_store_unavailable",
			"evidence audit storage is unavailable", "load_audit", err,
		)
		return
	}
	writeHTTPJSON(w, http.StatusOK, audit)
}

func (h *kbaseHTTPHandler) handleEvidenceAuditProofroom(
	w http.ResponseWriter,
	r *http.Request,
	rawAuditID string,
) {
	auditID, err := url.PathUnescape(rawAuditID)
	if err != nil || strings.TrimSpace(auditID) == "" {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"invalid audit_id", "proofroom_projection", nil,
		)
		return
	}
	switch r.Method {
	case http.MethodGet:
		preview, err := PreviewEvidenceAuditProofroom(h.store, auditID)
		if err != nil {
			h.writeProofroomHTTPError(w, "preview", err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, preview)
	case http.MethodPost:
		if h.proofroomDelivery == nil {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusServiceUnavailable, "proofroom_unconfigured",
				"Proofroom delivery is not configured", "proofroom_delivery", nil,
			)
			return
		}
		if r.ContentLength != 0 {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusRequestEntityTooLarge, "proofroom_request_body_not_allowed",
				"Proofroom delivery request body is not allowed", "proofroom_delivery", nil,
			)
			return
		}
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if idempotencyKey == "" {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusBadRequest, "proofroom_idempotency_key_required",
				"Idempotency-Key header is required", "proofroom_delivery", nil,
			)
			return
		}
		if resolution := strings.TrimSpace(r.Header.Get("Proofroom-Delivery-Resolution")); resolution != "" {
			if resolution != ProofroomCoordinationConfirmedNotDelivered {
				h.writeEvidenceAuditHTTPError(
					w, http.StatusBadRequest, "proofroom_coordination_invalid",
					"invalid Proofroom delivery resolution", "proofroom_coordination", nil,
				)
				return
			}
			if err := CoordinateProofroomDeliveryForEndpoint(
				h.store, auditID, idempotencyKey, h.proofroomDelivery.endpointIdentity,
				resolution, h.auditNow(),
			); err != nil {
				h.writeProofroomHTTPError(w, "coordination", err)
				return
			}
			writeHTTPJSON(w, http.StatusOK, map[string]any{
				"coordinated": true, "resolution": resolution,
			})
			return
		}
		receipt, created, err := h.proofroomDelivery.Deliver(
			r.Context(), h.store, auditID, idempotencyKey,
		)
		if err != nil {
			h.writeProofroomHTTPError(w, "delivery", err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeHTTPJSON(w, status, map[string]any{"created": created, "receipt": receipt})
	default:
		h.writeEvidenceAuditHTTPError(
			w, http.StatusMethodNotAllowed, "audit_method_not_allowed",
			"method not allowed", "proofroom_projection", nil,
		)
	}
}

func (h *kbaseHTTPHandler) writeProofroomHTTPError(w http.ResponseWriter, operation string, err error) {
	var remoteError *ProofroomRemoteError
	switch {
	case errors.Is(err, os.ErrNotExist):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusNotFound, "audit_not_found",
			"evidence audit not found", "proofroom_"+operation, nil,
		)
	case errors.Is(err, ErrProofroomAuditNotReady):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusConflict, "proofroom_audit_not_ready",
			"evidence audit is not ready for Proofroom", "proofroom_"+operation, err,
		)
	case errors.Is(err, ErrProofroomAuditInvalid):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusUnprocessableEntity, "proofroom_audit_invalid",
			"evidence audit failed Proofroom projection validation", "proofroom_"+operation, err,
		)
	case errors.Is(err, ErrProofroomPrivacyBlocked):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusUnprocessableEntity, "privacy_blocked",
			"Proofroom projection was blocked by privacy policy", "proofroom_"+operation, err,
		)
	case errors.Is(err, ErrProofroomDeliveryConflict):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusConflict, "proofroom_idempotency_conflict",
			"idempotency key conflicts with a different projection", "proofroom_"+operation, err,
		)
	case errors.Is(err, ErrProofroomDeliveryOutcomeUnknown):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadGateway, "proofroom_outcome_unknown",
			"Proofroom delivery outcome is unknown and requires explicit coordination",
			"proofroom_"+operation, err,
		)
	case errors.Is(err, ErrProofroomDeliveryRejected):
		if errors.As(err, &remoteError) {
			h.auditLogger(EvidenceAuditHTTPLogEvent{
				Operation: "proofroom_" + operation,
				Code:      "proofroom_remote_rejected",
				Cause:     sanitizeEvidenceAuditHTTPLogCause(err.Error()),
			})
			writeHTTPJSON(w, http.StatusBadGateway, map[string]any{
				"code":          "proofroom_remote_rejected",
				"error":         "Proofroom rejected the delivery",
				"remote_status": remoteError.StatusCode,
			})
			return
		}
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadGateway, "proofroom_remote_rejected",
			"Proofroom rejected the delivery", "proofroom_"+operation, err,
		)
	case errors.Is(err, ErrProofroomDeliveryUnconfigured):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusServiceUnavailable, "proofroom_unconfigured",
			"Proofroom delivery is not configured", "proofroom_"+operation, nil,
		)
	default:
		h.writeEvidenceAuditHTTPError(
			w, http.StatusInternalServerError, "proofroom_delivery_unavailable",
			"Proofroom delivery is unavailable", "proofroom_"+operation, err,
		)
	}
}

func (h *kbaseHTTPHandler) handleEvidenceAuditRetry(w http.ResponseWriter, r *http.Request, rawAuditID string) {
	if r.Method != http.MethodPost {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusMethodNotAllowed, "audit_method_not_allowed",
			"method not allowed", "retry_audit", nil,
		)
		return
	}
	if h.auditCoordinator == nil {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusServiceUnavailable, "audit_service_unavailable",
			"evidence audit service is unavailable", "retry_audit", nil,
		)
		return
	}
	if len(h.auditRetrySigningKey) < 32 {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusServiceUnavailable, "audit_retry_unavailable",
			"evidence audit retry is unavailable", "retry_audit", nil,
		)
		return
	}
	auditID, err := url.PathUnescape(rawAuditID)
	if err != nil || strings.TrimSpace(auditID) == "" {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"invalid audit_id", "retry_audit", nil,
		)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"Idempotency-Key header is required", "retry_audit", nil,
		)
		return
	}
	now := h.auditNow()
	authorization := h.issueEvidenceAuditRetryAuthorization(r, auditID, idempotencyKey, now)
	retry, created, err := ManualRetryEvidenceAudit(
		h.store, auditID, authorization, h.validateEvidenceAuditRetryAuthorization,
		idempotencyKey, now,
	)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			h.writeEvidenceAuditHTTPError(
				w, http.StatusNotFound, "audit_not_found",
				"evidence audit not found", "retry_audit", nil,
			)
		case errors.Is(err, ErrEvidenceAuditStateConflict):
			h.writeEvidenceAuditHTTPError(
				w, http.StatusConflict, "audit_retry_conflict",
				"evidence audit retry conflicts with current state", "retry_audit", err,
			)
		default:
			h.writeEvidenceAuditHTTPError(
				w, http.StatusUnprocessableEntity, "audit_retry_invalid",
				"evidence audit retry request is invalid", "retry_audit", err,
			)
		}
		return
	}
	if retry.Status == EvidenceAuditQueued || retry.Status == EvidenceAuditRunning {
		if err := h.auditCoordinator.Enqueue(retry.AuditID); err != nil {
			code := "audit_coordinator_unavailable"
			message := "evidence audit coordinator is unavailable"
			if errors.Is(err, ErrEvidenceAuditQueueFull) {
				code = "audit_queue_full"
				message = "evidence audit queue is full; retry later"
			}
			h.writeEvidenceAuditHTTPError(
				w, http.StatusServiceUnavailable, code, message, "enqueue_retry", err,
			)
			return
		}
	}
	writeHTTPJSON(w, http.StatusAccepted, map[string]any{"created": created, "audit": retry})
}

func (h *kbaseHTTPHandler) issueEvidenceAuditRetryAuthorization(
	r *http.Request,
	auditID, idempotencyKey string,
	now time.Time,
) EvidenceAuditRetryAuthorization {
	auth, _ := kbaseRequestAuthFromContext(r.Context())
	var actor string
	if auth.Method == kbaseAuthMethodCookie {
		actor = evidenceAuditOpaqueIdentity("session-actor\x00" + auth.SessionID)
	} else {
		token := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer "))
		actor = evidenceAuditOpaqueIdentity("bearer-actor\x00" + token)
	}
	expiresAt := now.UTC().Truncate(h.auditRetryTTL).Add(h.auditRetryTTL)
	if !expiresAt.After(now) {
		expiresAt = now.Add(h.auditRetryTTL)
	}
	nonce := h.evidenceAuditRetryMAC("nonce", auditID, actor, idempotencyKey)
	authorization := EvidenceAuditRetryAuthorization{
		AuditID: auditID, Actor: actor, Issuer: "kbase-http",
		Scope: EvidenceAuditRetryScope, ExpiresAt: expiresAt,
		Nonce: nonce, Verified: true,
	}
	authorization.Signature = h.evidenceAuditRetryMAC(
		"grant", authorization.AuditID, authorization.Actor, authorization.Issuer,
		authorization.Scope, authorization.ExpiresAt.Format(time.RFC3339Nano), authorization.Nonce,
	)
	return authorization
}

func (h *kbaseHTTPHandler) validateEvidenceAuditRetryAuthorization(
	authorization EvidenceAuditRetryAuthorization,
	now time.Time,
) error {
	if !authorization.Verified || authorization.Issuer != "kbase-http" ||
		authorization.Scope != EvidenceAuditRetryScope || !authorization.ExpiresAt.After(now) {
		return fmt.Errorf("retry authorization is invalid or expired")
	}
	want := h.evidenceAuditRetryMAC(
		"grant", authorization.AuditID, authorization.Actor, authorization.Issuer,
		authorization.Scope, authorization.ExpiresAt.Format(time.RFC3339Nano), authorization.Nonce,
	)
	if subtle.ConstantTimeCompare([]byte(want), []byte(authorization.Signature)) != 1 {
		return fmt.Errorf("retry authorization signature is invalid")
	}
	return nil
}

func (h *kbaseHTTPHandler) evidenceAuditRetryMAC(parts ...string) string {
	mac := hmac.New(sha256.New, h.auditRetrySigningKey)
	for _, part := range parts {
		_, _ = mac.Write([]byte(part))
		_, _ = mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *kbaseHTTPHandler) writeEvidenceAuditCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusNotFound, "audit_package_not_found",
			"agent package not found", "create_audit", nil,
		)
	case errors.Is(err, ErrEvidenceAuditIdempotencyConflict):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusConflict, "audit_idempotency_conflict",
			"idempotency key conflicts with a different audit request", "create_audit", err,
		)
	case strings.Contains(err.Error(), "schema_version"),
		strings.Contains(err.Error(), "v2"),
		strings.Contains(err.Error(), "evaluation"),
		strings.Contains(err.Error(), "published"),
		strings.Contains(err.Error(), "selected_claims"),
		strings.Contains(err.Error(), "evidence_policy"):
		h.writeEvidenceAuditHTTPError(
			w, http.StatusUnprocessableEntity, "audit_package_not_ready",
			"agent package is not eligible for evidence audit", "create_audit", err,
		)
	default:
		h.writeEvidenceAuditHTTPError(
			w, http.StatusBadRequest, "audit_request_invalid",
			"evidence audit request is invalid", "create_audit", err,
		)
	}
}

func (h *kbaseHTTPHandler) writeEvidenceAuditHTTPError(
	w http.ResponseWriter,
	status int,
	code, message, operation string,
	cause error,
) {
	if cause != nil {
		h.auditLogger(EvidenceAuditHTTPLogEvent{
			Operation: operation,
			Code:      code,
			Cause:     sanitizeEvidenceAuditHTTPLogCause(cause.Error()),
		})
	}
	writeHTTPJSON(w, status, map[string]string{"code": code, "error": message})
}

var (
	evidenceAuditAuthorizationPattern = regexp.MustCompile(
		`(?i)\b(bearer|basic)\s+[a-z0-9._~+/=-]+`,
	)
	evidenceAuditCredentialPattern = regexp.MustCompile(
		`(?i)((?:["']?)(?:api[_-]?key|apikey|client[_-]?secret|secret|password|passwd|session|csrf|access[_-]?token|refresh[_-]?token|token|cookie|authorization|proxy-authorization)(?:["']?)\s*(?::|=)\s*)(?:"[^"]*"|'[^']*'|[^\s&,;}]+)`,
	)
)

func sanitizeEvidenceAuditHTTPLogCause(value string) string {
	value = strings.TrimSpace(value)
	value = evidenceAuditAuthorizationPattern.ReplaceAllString(value, "$1 [redacted]")
	return evidenceAuditCredentialPattern.ReplaceAllString(value, "${1}[redacted]")
}

func (h *kbaseHTTPHandler) handleAgentPackages(w http.ResponseWriter, r *http.Request) {
	const (
		collectionPath = "/api/agent-packages"
		compilePath    = "/api/agent-packages/compile"
		evaluatePath   = "/api/agent-packages/evaluate"
		publishPath    = "/api/agent-packages/publish"
		trustSuitePath = "/api/agent-packages/evaluation-suites/trust"
		detailPrefix   = "/api/agent-packages/"
	)
	if r.URL.Path == compilePath {
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		defer r.Body.Close()
		var input AgentCompilationRequest
		decoder := json.NewDecoder(http.MaxBytesReader(
			w,
			r.Body,
			defaultAgentCompilationMaxBodyBytes,
		))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := ValidateAgentCompilationRequest(input); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		compilation, err := CompileAgentPackages(h.store, input)
		if err != nil {
			writeHTTPError(
				w,
				http.StatusInternalServerError,
				"agent compilation unavailable",
			)
			return
		}
		writeHTTPJSON(w, http.StatusOK, compilation)
		return
	}
	if r.URL.Path == trustSuitePath {
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		defer r.Body.Close()
		var input AgentPackageTrustedEvaluationSuiteRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := ValidateAgentPackage(input.Package, h.store, h.agentTools); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := h.store.SaveTrustedAgentEvaluationSuite(input.Package, input.Suite); err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "immutable") {
				status = http.StatusConflict
			}
			writeHTTPError(w, status, err.Error())
			return
		}
		writeHTTPJSON(w, http.StatusCreated, map[string]any{
			"trusted":       true,
			"package_id":    input.Package.PackageID,
			"version":       input.Package.Version,
			"suite_version": input.Suite.SuiteVersion,
		})
		return
	}
	if r.URL.Path == evaluatePath {
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		defer r.Body.Close()
		var input AgentPackageEvaluationRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := ValidateAgentPackage(input.Package, h.store, h.agentTools); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		evaluationSuite := input.Suite
		trustedSuiteHash := ""
		if input.Package.SchemaVersion == AgentPackageSchemaVersionV2 {
			var resolveErr error
			evaluationSuite, trustedSuiteHash, resolveErr = h.store.ResolveTrustedAgentEvaluationSuite(
				input.Package,
				input.Suite,
			)
			if resolveErr != nil {
				writeHTTPError(w, http.StatusBadRequest, resolveErr.Error())
				return
			}
		}
		if existing, err := h.store.LoadAgentPackageEvaluation(input.Package.ContentHash); err == nil {
			storedSuite, suiteErr := h.store.LoadAgentPackageEvaluationSuite(input.Package.ContentHash)
			if suiteErr != nil {
				writeHTTPError(w, http.StatusInternalServerError, suiteErr.Error())
				return
			}
			if !reflect.DeepEqual(*storedSuite, evaluationSuite) {
				writeHTTPError(w, http.StatusConflict, "agent package evaluation suite is immutable for this content hash")
				return
			}
			if input.Package.SchemaVersion == AgentPackageSchemaVersionV2 {
				if existing.TrustedSuiteHash == "" {
					evaluatedAt, parseErr := time.Parse(time.RFC3339Nano, existing.EvaluatedAt)
					if parseErr != nil {
						writeHTTPError(w, http.StatusConflict, "legacy agent package evaluation timestamp is invalid")
						return
					}
					migrated, migrateErr := h.store.MigrateLegacyTrustedAgentPackageEvaluation(
						input.Package,
						evaluationSuite,
						evaluatedAt,
					)
					if migrateErr != nil {
						writeHTTPError(w, http.StatusConflict, migrateErr.Error())
						return
					}
					writeHTTPJSON(w, http.StatusOK, map[string]any{
						"created": false, "migrated": true, "evaluation": migrated,
					})
					return
				}
				if existing.TrustedSuiteHash != trustedSuiteHash {
					writeHTTPError(w, http.StatusConflict, "agent package trusted evaluation identity is immutable")
					return
				}
			}
			writeHTTPJSON(w, http.StatusOK, map[string]any{"created": false, "evaluation": existing})
			return
		} else if !os.IsNotExist(err) {
			writeHTTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		report, err := EvaluateAgentPackageDeterministically(h.store, input.Package, evaluationSuite, time.Now())
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		report.TrustedSuiteHash = trustedSuiteHash
		if err := h.store.SaveAgentPackageEvaluation(input.Package, evaluationSuite, report); err != nil {
			writeHTTPError(w, http.StatusConflict, err.Error())
			return
		}
		writeHTTPJSON(w, http.StatusCreated, map[string]any{"created": true, "evaluation": report})
		return
	}
	if r.URL.Path == publishPath {
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		defer r.Body.Close()
		var input AgentPackagePublishRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&input); err != nil {
			writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if strings.TrimSpace(input.IdempotencyKey) == "" {
			writeHTTPError(w, http.StatusBadRequest, "idempotency_key is required")
			return
		}
		if err := ValidateAgentPackage(input.Package, h.store, h.agentTools); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		published, created, err := PublishAgentPackage(h.store, input.Package, input.IdempotencyKey, h.agentTools, time.Now())
		if err != nil {
			if errors.Is(err, ErrAgentPackageIdempotencyConflict) || errors.Is(err, ErrAgentPackageVersionConflict) {
				writeHTTPError(w, http.StatusConflict, err.Error())
				return
			}
			writeHTTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeHTTPJSON(w, status, map[string]any{"created": created, "package": published})
		return
	}
	if packageID, action, ok := agentPackageRuntimePath(r.URL.Path); ok {
		h.handleAgentPackageRuntime(w, r, packageID, action)
		return
	}
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.URL.Path == collectionPath {
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > 200 {
				writeHTTPError(w, http.StatusBadRequest, "limit must be between 1 and 200")
				return
			}
			limit = parsed
		}
		packages, err := h.store.ListAgentPackages(r.URL.Query().Get("after"), limit)
		if err != nil {
			writeHTTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		nextCursor := ""
		if len(packages) > 0 {
			last := packages[len(packages)-1]
			nextCursor = agentPackageReference(last.PackageID, last.Version)
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"packages": packages, "next_cursor": nextCursor})
		return
	}
	if !strings.HasPrefix(r.URL.Path, detailPrefix) {
		writeHTTPError(w, http.StatusNotFound, "agent package not found")
		return
	}
	rawID := strings.TrimPrefix(r.URL.Path, detailPrefix)
	if rawID == "" || strings.Contains(rawID, "/") {
		writeHTTPError(w, http.StatusNotFound, "agent package not found")
		return
	}
	packageID, err := url.PathUnescape(rawID)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid package_id")
		return
	}
	pkg, err := h.store.LoadAgentPackage(packageID, r.URL.Query().Get("version"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeHTTPError(w, http.StatusNotFound, "agent package not found")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := ValidateAgentPackageEvaluationGate(h.store, *pkg); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	evaluation, err := h.store.LoadAgentPackageEvaluation(pkg.ContentHash)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, struct {
		*AgentPackage
		Evaluation *AgentEvaluationReport `json:"evaluation"`
	}{
		AgentPackage: pkg,
		Evaluation:   evaluation,
	})
}

func agentPackageRuntimePath(path string) (string, string, bool) {
	const prefix = "/api/agent-packages/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	for _, action := range []string{"search", "chat"} {
		suffix := "/" + action
		if !strings.HasSuffix(path, suffix) {
			continue
		}
		rawID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
		if rawID == "" || strings.Contains(rawID, "/") {
			return "", "", false
		}
		packageID, err := url.PathUnescape(rawID)
		if err != nil || strings.TrimSpace(packageID) == "" {
			return "", "", false
		}
		return packageID, action, true
	}
	return "", "", false
}

func (h *kbaseHTTPHandler) handleAgentPackageRuntime(w http.ResponseWriter, r *http.Request, packageID, action string) {
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	if version == "" {
		writeHTTPError(w, http.StatusBadRequest, "version is required")
		return
	}
	switch action {
	case "search":
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		limit := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeHTTPError(w, http.StatusBadRequest, "limit must be positive")
				return
			}
			limit = parsed
		}
		response, err := SearchAgentPackage(h.store, AgentPackageSearchRequest{
			PackageID: packageID, PackageVersion: version,
			Query: r.URL.Query().Get("q"), Limit: limit,
		})
		if err != nil {
			h.writeAgentPackageRuntimeError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, response)
	case "chat":
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		defer r.Body.Close()
		var input struct {
			Question string `json:"question"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
			writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		response, err := ChatAgentPackageWithClient(r.Context(), h.store, AgentPackageChatRequest{
			PackageID: packageID, PackageVersion: version, Question: input.Question,
		}, h.chatClient)
		if err != nil {
			h.writeAgentPackageRuntimeError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, response)
	}
}

func (h *kbaseHTTPHandler) writeAgentPackageRuntimeError(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrNotExist) {
		writeHTTPError(w, http.StatusNotFound, "agent package not found")
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeHTTPError(w, http.StatusGatewayTimeout, "agent model timed out; please retry")
		return
	}
	message := err.Error()
	for _, inputError := range []string{
		"is required", "must be positive", "max_context_chunks", "is not declared",
	} {
		if strings.Contains(message, inputError) {
			writeHTTPError(w, http.StatusBadRequest, message)
			return
		}
	}
	writeHTTPError(w, http.StatusInternalServerError, message)
}

func (h *kbaseHTTPHandler) handleKnowledgePipeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 500 {
			writeHTTPError(w, http.StatusBadRequest, "limit must be between 1 and 500")
			return
		}
		limit = parsed
	}
	dashboard, err := BuildKnowledgePipelineDashboard(h.store, limit)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, dashboard)
}

func (h *kbaseHTTPHandler) handleKnowledgeReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 500 {
			writeHTTPError(w, http.StatusBadRequest, "limit must be between 1 and 500")
			return
		}
		limit = parsed
	}
	readiness, err := BuildKnowledgeReadiness(h.store, limit, r.URL.Query().Get("book_id"))
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, readiness)
}

func (h *kbaseHTTPHandler) handleKnowledgeAssembly(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := knowledgeAssemblyDefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > knowledgeAssemblyMaxLimit {
			writeHTTPError(
				w,
				http.StatusBadRequest,
				fmt.Sprintf("limit must be between 1 and %d", knowledgeAssemblyMaxLimit),
			)
			return
		}
		limit = parsed
	}
	assembly, err := BuildKnowledgeReleaseAssembly(h.store, KnowledgeReleaseAssemblyQuery{
		Limit: limit,
		Query: r.URL.Query().Get("query"),
	})
	if err != nil {
		if strings.Contains(err.Error(), "query must not exceed") ||
			strings.Contains(err.Error(), "limit must be between") {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, "knowledge assembly unavailable")
		return
	}
	writeHTTPJSON(w, http.StatusOK, assembly)
}

func (h *kbaseHTTPHandler) handleKnowledgeOperations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 500 {
			writeHTTPError(w, http.StatusBadRequest, "limit must be between 1 and 500")
			return
		}
		limit = parsed
	}
	console, err := BuildKnowledgeOperationsConsole(h.store, limit)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, console)
}

func (h *kbaseHTTPHandler) handleKnowledgeOperationsReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request KnowledgeOperationsReplayRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := RunKnowledgeOperationsReplay(r.Context(), h.store, h.analysisGenerator, request)
	if err != nil {
		if strings.Contains(err.Error(), "not allowed") {
			writeHTTPError(w, http.StatusConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "required") {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not found") {
			writeHTTPError(w, http.StatusNotFound, err.Error())
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func (h *kbaseHTTPHandler) handleKnowledgePipelineRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request KnowledgePipelineAutomationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := RunKnowledgePipelineAutomation(r.Context(), h.store, h.analysisGenerator, request)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
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

func (h *kbaseHTTPHandler) authorizeEvidenceAudit(w http.ResponseWriter, r *http.Request) bool {
	token, valid := singleBearerToken(r)
	if h.authToken == "" || !valid ||
		subtle.ConstantTimeCompare([]byte(token), []byte(h.authToken)) != 1 {
		h.writeEvidenceAuditHTTPError(
			w, http.StatusUnauthorized, "audit_unauthorized",
			"unauthorized", "authorize_audit", nil,
		)
		return false
	}
	return true
}

const (
	kbaseAuthMethodBearer = "bearer"
	kbaseAuthMethodCookie = "cookie"
)

type kbaseRequestAuth struct {
	Method       string
	SessionID    string
	Renewed      bool
	ExpiresAt    time.Time
	sessionToken string
	session      BrowserSession
}

type kbaseRequestAuthContextKey struct{}

func kbaseRequestAuthFromContext(ctx context.Context) (kbaseRequestAuth, bool) {
	auth, ok := ctx.Value(kbaseRequestAuthContextKey{}).(kbaseRequestAuth)
	return auth, ok
}

func requestWithKBaseAuth(r *http.Request, auth kbaseRequestAuth) *http.Request {
	auth.sessionToken = ""
	return r.WithContext(context.WithValue(r.Context(), kbaseRequestAuthContextKey{}, auth))
}

func (h *kbaseHTTPHandler) authorizeKBaseRequest(
	w http.ResponseWriter,
	r *http.Request,
	auditAPI bool,
	renew bool,
) (kbaseRequestAuth, bool) {
	if len(r.Header.Values("Authorization")) != 0 {
		if auditAPI {
			if !h.authorizeEvidenceAudit(w, r) {
				return kbaseRequestAuth{}, false
			}
		} else if !h.authorize(w, r) {
			return kbaseRequestAuth{}, false
		}
		return kbaseRequestAuth{Method: kbaseAuthMethodBearer}, true
	}

	token, present, valid := browserSessionCookieToken(r)
	if !present {
		clearBrowserSessionCookie(w)
		if auditAPI {
			h.writeEvidenceAuditHTTPError(
				w, http.StatusUnauthorized, "audit_unauthorized",
				"unauthorized", "authorize_audit", nil,
			)
		} else if h.authToken == "" {
			writeHTTPError(w, http.StatusUnauthorized, "kbase auth token is not configured")
		} else {
			writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		}
		return kbaseRequestAuth{}, false
	}
	if !valid {
		if !h.recordBrowserSessionAuthenticationRejected(
			w,
			auditAPI,
			"invalid_cookie",
		) {
			return kbaseRequestAuth{}, false
		}
		clearBrowserSessionCookie(w)
		h.writeKBaseRequestSecurityError(
			w, auditAPI, http.StatusUnauthorized, "audit_unauthorized", "unauthorized",
		)
		return kbaseRequestAuth{}, false
	}
	if h.browserSessions.Store == nil {
		h.writeKBaseRequestSecurityError(
			w, auditAPI, http.StatusServiceUnavailable,
			"audit_service_unavailable", "service unavailable",
		)
		return kbaseRequestAuth{}, false
	}

	if !renew {
		session, err := h.browserSessions.Store.Authenticate(token)
		if err != nil {
			h.writeBrowserSessionAuthenticationError(w, auditAPI, token, err)
			return kbaseRequestAuth{}, false
		}
		return kbaseRequestAuth{
			Method:       kbaseAuthMethodCookie,
			SessionID:    session.ID,
			ExpiresAt:    session.ExpiresAt,
			sessionToken: token,
			session:      session,
		}, true
	}

	sessionAuth, err := h.browserSessions.Store.AuthenticateAndRenew(token)
	if err != nil {
		h.writeBrowserSessionAuthenticationError(w, auditAPI, token, err)
		return kbaseRequestAuth{}, false
	}
	if sessionAuth.SetCookie {
		setBrowserSessionCookie(
			w,
			token,
			sessionAuth.CookieExpiresAt,
			h.browserSessions.TTL,
		)
	}
	return kbaseRequestAuth{
		Method:       kbaseAuthMethodCookie,
		SessionID:    sessionAuth.Session.ID,
		Renewed:      sessionAuth.Renewed,
		ExpiresAt:    sessionAuth.Session.ExpiresAt,
		sessionToken: token,
		session:      sessionAuth.Session,
	}, true
}

func (h *kbaseHTTPHandler) renewBrowserSessionAfterCSRF(
	w http.ResponseWriter,
	auth kbaseRequestAuth,
	auditAPI bool,
) (kbaseRequestAuth, bool) {
	sessionAuth, err := h.browserSessions.Store.AuthenticateAndRenew(auth.sessionToken)
	if err != nil {
		h.writeBrowserSessionAuthenticationError(w, auditAPI, auth.sessionToken, err)
		return kbaseRequestAuth{}, false
	}
	if sessionAuth.SetCookie {
		setBrowserSessionCookie(
			w,
			auth.sessionToken,
			sessionAuth.CookieExpiresAt,
			h.browserSessions.TTL,
		)
	}
	auth.SessionID = sessionAuth.Session.ID
	auth.Renewed = sessionAuth.Renewed
	auth.ExpiresAt = sessionAuth.Session.ExpiresAt
	auth.session = sessionAuth.Session
	return auth, true
}

func (h *kbaseHTTPHandler) writeBrowserSessionAuthenticationError(
	w http.ResponseWriter,
	auditAPI bool,
	token string,
	err error,
) {
	if isBrowserSessionCredentialError(err) {
		if auditErr := h.browserSessions.Store.RecordAuthenticationRejectedByToken(
			token,
			browserSessionAuthenticationAuditReason(err),
		); auditErr != nil {
			h.writeKBaseRequestSecurityError(
				w, auditAPI, http.StatusServiceUnavailable,
				"audit_service_unavailable", "service unavailable",
			)
			return
		}
		w.Header().Del("Set-Cookie")
		clearBrowserSessionCookie(w)
		h.writeKBaseRequestSecurityError(
			w, auditAPI, http.StatusUnauthorized, "audit_unauthorized", "unauthorized",
		)
		return
	}
	h.writeKBaseRequestSecurityError(
		w, auditAPI, http.StatusServiceUnavailable,
		"audit_service_unavailable", "service unavailable",
	)
}

func (h *kbaseHTTPHandler) recordBrowserSessionAuthenticationRejected(
	w http.ResponseWriter,
	auditAPI bool,
	reasonCode string,
) bool {
	if h.browserSessions.Store == nil {
		return true
	}
	if err := h.browserSessions.Store.RecordAuthenticationRejected(
		"",
		"",
		reasonCode,
	); err != nil {
		h.writeKBaseRequestSecurityError(
			w, auditAPI, http.StatusServiceUnavailable,
			"audit_service_unavailable", "service unavailable",
		)
		return false
	}
	return true
}

func browserSessionAuthenticationAuditReason(err error) string {
	switch {
	case errors.Is(err, ErrBrowserSessionExpired):
		return "expired"
	case errors.Is(err, ErrBrowserSessionRevoked):
		return "revoked"
	case errors.Is(err, ErrBrowserSessionClientMismatch):
		return "client_mismatch"
	default:
		return "missing"
	}
}

func (h *kbaseHTTPHandler) authorizeBrowserSessionCSRF(
	w http.ResponseWriter,
	r *http.Request,
	auth kbaseRequestAuth,
	auditAPI bool,
) bool {
	origin, originOK := singleBoundedHeader(r, "Origin", maxBrowserSessionOriginBytes)
	fetchSite, fetchSiteOK := singleBoundedHeader(
		r, "Sec-Fetch-Site", maxBrowserSessionFetchSiteBytes,
	)
	csrfToken, csrfOK := singleBoundedHeader(
		r, "X-KBase-CSRF", maxBrowserSessionCSRFBytes,
	)
	if !originOK || origin != h.browserSessions.PublicOrigin ||
		!fetchSiteOK || fetchSite != "same-origin" ||
		!csrfOK || csrfToken == "" {
		h.writeKBaseRequestSecurityError(
			w, auditAPI, http.StatusForbidden, "audit_forbidden", "forbidden",
		)
		return false
	}
	if err := h.browserSessions.Store.ValidateCSRF(auth.SessionID, csrfToken); err != nil {
		switch {
		case isBrowserSessionCredentialError(err):
			w.Header().Del("Set-Cookie")
			clearBrowserSessionCookie(w)
			h.writeKBaseRequestSecurityError(
				w, auditAPI, http.StatusUnauthorized, "audit_unauthorized", "unauthorized",
			)
		case errors.Is(err, ErrBrowserSessionCSRFInvalid),
			errors.Is(err, ErrBrowserSessionCSRFExpired):
			h.writeKBaseRequestSecurityError(
				w, auditAPI, http.StatusForbidden, "audit_forbidden", "forbidden",
			)
		default:
			h.writeKBaseRequestSecurityError(
				w, auditAPI, http.StatusServiceUnavailable,
				"audit_service_unavailable", "service unavailable",
			)
		}
		return false
	}
	return true
}

func (h *kbaseHTTPHandler) writeKBaseRequestSecurityError(
	w http.ResponseWriter,
	auditAPI bool,
	status int,
	auditCode, message string,
) {
	if auditAPI {
		h.writeEvidenceAuditHTTPError(
			w, status, auditCode, message, "authorize_audit", nil,
		)
		return
	}
	writeHTTPError(w, status, message)
}

func isUnsafeKBaseRequestMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func authorizeBearerToken(w http.ResponseWriter, r *http.Request, expected string) bool {
	token, valid := singleBearerToken(r)
	if !valid || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (h *kbaseHTTPHandler) handleLegacyBrowserToken(w http.ResponseWriter, r *http.Request) {
	setBrowserSessionNoStore(w)
	w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
	switch r.Method {
	case http.MethodGet:
		writeHTTPJSON(w, http.StatusGone, map[string]any{
			"error":     "browser token exchange retired",
			"migration": "use POST /browser/session or POST /browser/session/migrate",
		})
	case http.MethodHead:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusGone)
	default:
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func setBrowserSessionNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

// Responses are intentionally limited to public session metadata.
func writeBrowserSessionMetadata(w http.ResponseWriter, session BrowserSession) {
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"session":   session,
		"client_id": session.ClientID,
		"epoch":     session.IssuedEpoch,
	})
}

func writeBrowserClientMetadata(w http.ResponseWriter, family BrowserClientFamily) {
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"client_id": family.ClientID,
		"epoch":     family.Epoch,
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

func (h *kbaseHTTPHandler) handleGetCitation(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/citations/"
	rawID := strings.TrimPrefix(r.URL.Path, prefix)
	if rawID == "" || strings.Contains(rawID, "/") {
		writeHTTPError(w, http.StatusNotFound, "citation not found")
		return
	}
	citationID, err := url.PathUnescape(rawID)
	if err != nil || strings.TrimSpace(citationID) == "" {
		writeHTTPError(w, http.StatusBadRequest, "citation_id is required")
		return
	}
	bookID := strings.TrimSpace(r.URL.Query().Get("book_id"))
	if bookID == "" {
		writeHTTPError(w, http.StatusBadRequest, "book_id is required")
		return
	}
	pkg, err := h.loadHTTPBookPackage(bookID)
	if err != nil {
		writeHTTPError(w, http.StatusNotFound, "book not found")
		return
	}
	for _, citation := range pkg.Citations {
		if citation.CitationID != citationID {
			continue
		}
		claimIDs := make([]string, 0, 2)
		for _, claim := range pkg.Claims {
			for _, candidate := range claim.Citations {
				if candidate == citationID {
					claimIDs = append(claimIDs, claim.ClaimID)
					break
				}
			}
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{
			"citation": map[string]string{
				"citation_id":  citation.CitationID,
				"book_id":      citation.BookID,
				"chapter_id":   citation.ChapterID,
				"chunk_id":     citation.ChunkID,
				"source_type":  citation.SourceType,
				"published_at": citation.PublishedAt,
			},
			"claim_ids": claimIDs,
		})
		return
	}
	writeHTTPError(w, http.StatusNotFound, "citation not found")
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
		if os.IsNotExist(err) {
			bookID := sanitizeBookKnowledgeID(stripReaderRouteSuffix(request.BookID))
			writeHTTPError(w, http.StatusNotFound, fmt.Sprintf("book not found: %s", bookID))
			return
		}
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

func (h *kbaseHTTPHandler) handleContextChat(w http.ResponseWriter, r *http.Request) {
	var request ContextKnowledgeChatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&request); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	response, err := ContextKnowledgeChatWithClient(r.Context(), request, h.chatClient)
	if err != nil {
		if strings.Contains(err.Error(), "question is required") || strings.Contains(err.Error(), "content is required") {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
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
	return path == "/api/source-agent-artifacts" ||
		path == "/api/source-agents" ||
		strings.HasPrefix(path, "/api/source-agents/") ||
		path == "/api/source-subscriptions" ||
		strings.HasPrefix(path, "/api/source-subscriptions/") ||
		path == "/api/source-sync/runs" ||
		strings.HasPrefix(path, "/api/source-sync/runs/")
}

func (h *kbaseHTTPHandler) handleSourceAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/source-agent/artifacts/") {
		h.handleSourceAgentArtifactDownload(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	switch r.URL.Path {
	case "/api/source-agent/commands/claim":
		h.handleSourceAgentCommandClaim(w, r)
	case "/api/source-agent/commands/recover":
		h.handleSourceAgentCommandRecovery(w, r)
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
		if strings.HasPrefix(r.URL.Path, "/api/source-agent/commands/") {
			h.handleSourceAgentCommandReport(w, r)
			return
		}
		h.handleSourceAgentRun(w, r)
	}
}

func (h *kbaseHTTPHandler) handleSourceAgentCommandRecovery(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		AgentID   string `json:"agent_id"`
		CommandID string `json:"command_id,omitempty"`
	}
	if !decodeStrictLimitedHTTPJSON(w, r, h.sourceAgentMaxBodyBytes, &payload) {
		return
	}
	var command *SourceAgentCommand
	if strings.TrimSpace(payload.CommandID) == "" {
		recovered, err := h.sourceSync.RecoverOwnedSourceAgentUpgrade(payload.AgentID, payload.AgentID)
		if err != nil {
			h.writeSourceAgentCommandWorkerError(w, err)
			return
		}
		command = recovered
	} else {
		resumed, err := h.sourceSync.ResumeOwnedSourceAgentUpgrade(payload.CommandID, payload.AgentID, payload.AgentID)
		if err != nil {
			h.writeSourceAgentCommandWorkerError(w, err)
			return
		}
		command = &resumed
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"command": command})
}

func (h *kbaseHTTPHandler) handleSourceAgentCommandClaim(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		AgentID   string `json:"agent_id"`
		CommandID string `json:"command_id,omitempty"`
	}
	if !decodeStrictLimitedHTTPJSON(w, r, h.sourceAgentMaxBodyBytes, &payload) {
		return
	}
	var command *SourceAgentCommand
	if strings.TrimSpace(payload.CommandID) == "" {
		claimed, err := h.sourceSync.ClaimNextSourceAgentCommand(payload.AgentID, payload.AgentID)
		if err != nil {
			h.writeSourceAgentCommandWorkerError(w, err)
			return
		}
		command = claimed
	} else {
		claimed, err := h.sourceSync.ClaimSourceAgentCommand(payload.CommandID, payload.AgentID, payload.AgentID)
		if err != nil {
			h.writeSourceAgentCommandWorkerError(w, err)
			return
		}
		command = &claimed
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"command": command})
}

func (h *kbaseHTTPHandler) handleSourceAgentCommandReport(w http.ResponseWriter, r *http.Request) {
	commandID, action, ok, invalid := parseSourceAgentCommandWorkerPath(r)
	if invalid {
		writeHTTPError(w, http.StatusBadRequest, "invalid command id")
		return
	}
	if !ok {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	if action == "guard" {
		h.handleSourceAgentUpdateGuard(w, r, commandID)
		return
	}
	if action != "progress" && action != "complete" {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	var payload struct {
		AgentID       string `json:"agent_id"`
		State         string `json:"state"`
		Code          string `json:"code,omitempty"`
		Message       string `json:"message,omitempty"`
		ActualVersion string `json:"actual_version,omitempty"`
	}
	if !decodeStrictLimitedHTTPJSON(w, r, h.sourceAgentMaxBodyBytes, &payload) {
		return
	}
	payload.State = strings.ToLower(strings.TrimSpace(payload.State))
	expectedAction, validState := sourceAgentCommandWorkerReportAction(payload.State)
	if !validState || expectedAction != action {
		writeHTTPError(w, http.StatusBadRequest, "invalid command report state")
		return
	}
	if payload.State == SourceAgentCommandInstalling {
		if _, _, _, err := h.validateSourceAgentArtifactCommand(commandID, payload.AgentID, SourceAgentCommandVerified); err != nil {
			h.writeSourceAgentArtifactWorkerError(w, err)
			return
		}
	}
	command, err := h.sourceSync.TransitionSourceAgentCommand(
		commandID,
		payload.AgentID,
		payload.AgentID,
		SourceAgentCommandTransition{
			State:         payload.State,
			ResultCode:    payload.Code,
			Message:       payload.Message,
			ActualVersion: payload.ActualVersion,
		},
	)
	if err != nil {
		h.writeSourceAgentCommandWorkerError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"command": command})
}

func (h *kbaseHTTPHandler) handleSourceAgentUpdateGuard(w http.ResponseWriter, r *http.Request, commandID string) {
	var payload struct {
		AgentID         string `json:"agent_id"`
		ArtifactID      string `json:"artifact_id"`
		CurrentVersion  string `json:"current_version"`
		TargetVersion   string `json:"target_version"`
		Revision        string `json:"revision"`
		Channel         string `json:"channel"`
		Size            int64  `json:"size"`
		SHA256          string `json:"sha256"`
		WorkerType      string `json:"worker_type"`
		Platform        string `json:"platform"`
		Architecture    string `json:"architecture"`
		ProtocolVersion string `json:"protocol_version"`
	}
	if !decodeStrictLimitedHTTPJSON(w, r, h.sourceAgentMaxBodyBytes, &payload) {
		return
	}
	agentID, err := normalizeSourceAgentCommandIdentifier("agent_id", payload.AgentID, true)
	check := SourceAgentUpdateGuardCheck{
		CommandID: commandID, ArtifactID: payload.ArtifactID, WorkerType: payload.WorkerType,
		CurrentVersion: payload.CurrentVersion, Version: payload.TargetVersion,
		Revision: payload.Revision, Channel: payload.Channel, Size: payload.Size, SHA256: payload.SHA256,
		Platform: payload.Platform, Architecture: payload.Architecture, ProtocolVersion: payload.ProtocolVersion,
	}
	if err != nil || agentID != payload.AgentID || !validSourceAgentUpdateGuardCheck(check) {
		writeHTTPError(w, http.StatusBadRequest, "invalid source agent update guard request")
		return
	}
	command, agent, selection, err := h.validateSourceAgentArtifactCommand(
		commandID, agentID, SourceAgentCommandInstalling,
	)
	if err != nil {
		h.writeSourceAgentArtifactWorkerError(w, err)
		return
	}
	metadata := selection.artifact.public()
	if command.UpgradeSpec == nil || command.UpgradeSpec.ArtifactID != payload.ArtifactID ||
		command.UpgradeSpec.ExpectedCurrentVersion != payload.CurrentVersion ||
		agent.Version != payload.CurrentVersion || agent.WorkerType != payload.WorkerType ||
		agent.Platform != payload.Platform || agent.Architecture != payload.Architecture ||
		agent.ProtocolVersion != payload.ProtocolVersion ||
		metadata.ID != payload.ArtifactID || metadata.Version != payload.TargetVersion ||
		metadata.Revision != payload.Revision || metadata.Channel != payload.Channel ||
		metadata.Size != payload.Size || metadata.SHA256 != payload.SHA256 ||
		metadata.WorkerType != payload.WorkerType || metadata.Platform != payload.Platform ||
		metadata.Architecture != payload.Architecture || metadata.ProtocolVersion != payload.ProtocolVersion {
		writeHTTPError(w, http.StatusConflict, "source agent update guard denied")
		return
	}
	activeRun, err := h.sourceSync.sourceAgentHasActiveRun(agentID)
	if err != nil {
		writeHTTPError(w, http.StatusServiceUnavailable, "source agent update guard unavailable")
		return
	}
	if activeRun {
		writeHTTPError(w, http.StatusConflict, "source agent update guard denied")
		return
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, command.ExpiresAt)
	minimumRemaining := sourceAgentUpdateDefaultRestartTimeout + sourceAgentUpdateDefaultTimeout +
		sourceAgentUpdateGuardReconciliationWindow + sourceAgentUpdateGuardSafetyMargin
	if err != nil || expiresAt.Sub(h.sourceSync.now().UTC()) < minimumRemaining {
		writeHTTPError(w, http.StatusConflict, "source agent update guard denied")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *kbaseHTTPHandler) handleSourceAgentArtifactDownload(w http.ResponseWriter, r *http.Request) {
	artifactID, ok, invalid := parseSourceAgentArtifactDownloadPath(r)
	if invalid {
		writeHTTPError(w, http.StatusBadRequest, "invalid artifact download request")
		return
	}
	if !ok {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	if h.sourceArtifacts == nil {
		writeHTTPError(w, http.StatusServiceUnavailable, "source agent artifact download unavailable")
		return
	}
	if r.ContentLength != 0 {
		writeHTTPError(w, http.StatusBadRequest, "invalid artifact download request")
		return
	}
	query := r.URL.Query()
	if len(query) != 2 || len(query["agent_id"]) != 1 || len(query["command_id"]) != 1 {
		writeHTTPError(w, http.StatusBadRequest, "invalid artifact download request")
		return
	}
	rawAgentID := query.Get("agent_id")
	agentID, err := normalizeSourceAgentCommandIdentifier("agent_id", rawAgentID, true)
	if err != nil || agentID != rawAgentID {
		writeHTTPError(w, http.StatusBadRequest, "invalid artifact download request")
		return
	}
	rawCommandID := query.Get("command_id")
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", rawCommandID, true)
	if err != nil || commandID != rawCommandID {
		writeHTTPError(w, http.StatusBadRequest, "invalid artifact download request")
		return
	}
	lease, err := h.sourceArtifacts.acquireSnapshotLease(r.Context())
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		h.writeSourceAgentArtifactWorkerError(w, err)
		return
	}
	defer lease.Close()
	command, _, selection, err := h.validateSourceAgentArtifactCommand(commandID, agentID,
		SourceAgentCommandClaimed, SourceAgentCommandDownloading)
	if err != nil {
		h.writeSourceAgentArtifactWorkerError(w, err)
		return
	}
	if command.UpgradeSpec == nil || command.UpgradeSpec.ArtifactID != artifactID {
		h.writeSourceAgentArtifactWorkerError(w, ErrSourceAgentCommandTarget)
		return
	}
	snapshot, err := lease.prepareSnapshot(r.Context(), selection)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		h.writeSourceAgentArtifactWorkerError(w, err)
		return
	}
	defer snapshot.Close()
	metadata := selection.artifact.public()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.FormatInt(metadata.Size, 10))
	w.Header().Set(sourceAgentHeaderCommandID, command.ID)
	w.Header().Set(sourceAgentHeaderArtifactID, metadata.ID)
	w.Header().Set(sourceAgentHeaderArtifactVersion, metadata.Version)
	w.Header().Set(sourceAgentHeaderArtifactWorkerType, metadata.WorkerType)
	w.Header().Set(sourceAgentHeaderArtifactPlatform, metadata.Platform)
	w.Header().Set(sourceAgentHeaderArtifactArch, metadata.Architecture)
	w.Header().Set(sourceAgentHeaderArtifactProtocol, metadata.ProtocolVersion)
	w.Header().Set(sourceAgentHeaderArtifactRevision, metadata.Revision)
	w.Header().Set(sourceAgentHeaderArtifactChannel, metadata.Channel)
	w.Header().Set(sourceAgentHeaderArtifactSize, strconv.FormatInt(metadata.Size, 10))
	w.Header().Set(sourceAgentHeaderArtifactSHA256, metadata.SHA256)
	w.WriteHeader(http.StatusOK)
	written, err := copySourceAgentArtifactWithContext(r.Context(), w, snapshot.file)
	if err != nil || written != metadata.Size {
		return
	}
}

func parseSourceAgentArtifactDownloadPath(r *http.Request) (string, bool, bool) {
	const prefix = "/api/source-agent/artifacts/"
	rawPath := r.URL.EscapedPath()
	if !strings.HasPrefix(rawPath, prefix) {
		return "", false, false
	}
	parts := strings.Split(strings.TrimPrefix(rawPath, prefix), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "download" {
		return "", false, false
	}
	rawArtifactID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", false, true
	}
	artifactID, err := normalizeSourceAgentCommandIdentifier("artifact_id", rawArtifactID, true)
	if err != nil || artifactID != rawArtifactID {
		return "", false, true
	}
	return artifactID, true, false
}

func (h *kbaseHTTPHandler) validateSourceAgentArtifactCommand(commandID, agentID string, allowedStates ...string) (SourceAgentCommand, SourceAgent, sourceAgentArtifactSelection, error) {
	if h.sourceArtifacts == nil {
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, ErrSourceAgentArtifactCatalogInvalid
	}
	command, err := h.sourceSync.GetSourceAgentCommand(commandID)
	if err != nil {
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, err
	}
	if command.TargetAgentID != agentID {
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, ErrSourceAgentCommandTarget
	}
	if !isTerminalSourceAgentCommandState(command.State) && sourceAgentCommandIsExpired(command, h.sourceSync.now().UTC()) {
		_, expireErr := h.sourceSync.ClaimSourceAgentCommand(command.ID, agentID, agentID)
		if expireErr != nil {
			return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, expireErr
		}
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, ErrSourceAgentCommandExpired
	}
	if command.Type != SourceAgentCommandUpgrade || command.UpgradeSpec == nil {
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, ErrSourceAgentCommandType
	}
	stateAllowed := false
	for _, allowed := range allowedStates {
		if command.State == allowed {
			stateAllowed = true
			break
		}
	}
	if !stateAllowed {
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, ErrSourceAgentCommandInvalidState
	}
	if command.ClaimOwner == "" || command.ClaimOwner != agentID {
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, ErrSourceAgentCommandClaimOwner
	}
	agent, err := h.sourceSync.GetSourceAgent(agentID)
	if err != nil {
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, err
	}
	if command.ExpectedCurrentVersion == "" || command.ExpectedCurrentVersion != agent.Version {
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, ErrSourceAgentCommandVersionConflict
	}
	selection, err := h.sourceArtifacts.selectForRollout(command.UpgradeSpec.ArtifactID, sourceAgentArtifactTargetFromAgent(agent))
	if err != nil {
		return SourceAgentCommand{}, SourceAgent{}, sourceAgentArtifactSelection{}, err
	}
	return command, agent, selection, nil
}

func sourceAgentArtifactTargetFromAgent(agent SourceAgent) SourceAgentArtifactTarget {
	return SourceAgentArtifactTarget{
		WorkerType: agent.WorkerType, Platform: agent.Platform, Architecture: agent.Architecture,
		CurrentVersion: agent.Version,
	}
}

func (h *kbaseHTTPHandler) writeSourceAgentArtifactWorkerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSourceAgentCommandNotFound), errors.Is(err, ErrSourceAgentNotFound),
		errors.Is(err, ErrSourceAgentArtifactNotFound):
		writeHTTPError(w, http.StatusNotFound, "source agent artifact unavailable")
	case errors.Is(err, ErrSourceAgentCommandTarget), errors.Is(err, ErrSourceAgentCommandClaimOwner),
		errors.Is(err, ErrSourceAgentCommandType), errors.Is(err, ErrSourceAgentArtifactIncompatible):
		writeHTTPError(w, http.StatusForbidden, "source agent artifact is not available to this worker")
	case errors.Is(err, ErrSourceAgentCommandInvalidState), errors.Is(err, ErrSourceAgentCommandExpired),
		errors.Is(err, ErrSourceAgentCommandVersionConflict),
		errors.Is(err, ErrSourceAgentArtifactNotAllowed), errors.Is(err, ErrSourceAgentArtifactIntegrity):
		writeHTTPError(w, http.StatusConflict, "source agent artifact unavailable")
	default:
		writeHTTPError(w, http.StatusServiceUnavailable, "source agent artifact unavailable")
	}
}

func parseSourceAgentCommandWorkerPath(r *http.Request) (string, string, bool, bool) {
	const prefix = "/api/source-agent/commands/"
	rawPath := r.URL.EscapedPath()
	if !strings.HasPrefix(rawPath, prefix) {
		return "", "", false, false
	}
	parts := strings.Split(strings.TrimPrefix(rawPath, prefix), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false, false
	}
	commandID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", false, true
	}
	commandID, err = normalizeSourceAgentCommandIdentifier("command_id", commandID, true)
	if err != nil {
		return "", "", false, true
	}
	return commandID, parts[1], true, false
}

func (h *kbaseHTTPHandler) writeSourceAgentCommandWorkerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSourceAgentCommandNotFound), errors.Is(err, ErrSourceAgentNotFound):
		writeHTTPError(w, http.StatusNotFound, "source agent command not found")
	case errors.Is(err, ErrSourceAgentCommandTarget), errors.Is(err, ErrSourceAgentCommandClaimOwner):
		writeHTTPError(w, http.StatusForbidden, "source agent command is not available to this worker")
	case errors.Is(err, ErrSourceAgentCommandInvalidState),
		errors.Is(err, ErrSourceAgentCommandResultConflict),
		errors.Is(err, ErrSourceAgentCommandExpired),
		errors.Is(err, ErrSourceAgentCommandRecoveryAmbiguous):
		writeHTTPError(w, http.StatusConflict, "source agent command conflict")
	case errors.Is(err, ErrSourceAgentCommandType):
		writeHTTPError(w, http.StatusBadRequest, "invalid source agent command")
	case isSourceAgentCommandInputError(err):
		writeHTTPError(w, http.StatusBadRequest, "invalid source agent command request")
	default:
		writeHTTPError(w, http.StatusInternalServerError, "source agent command unavailable")
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
	case r.URL.Path == "/api/source-agent-artifacts":
		h.handleSourceAgentArtifactMetadata(w, r)
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
	case strings.HasPrefix(r.URL.Path, "/api/source-agents/"):
		h.handleSourceAgentManagement(w, r)
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

func (h *kbaseHTTPHandler) handleSourceAgentArtifactMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.sourceArtifacts == nil {
		writeHTTPError(w, http.StatusServiceUnavailable, "source agent artifact catalog unavailable")
		return
	}
	limit, ok := parseSourceAgentArtifactListLimit(w, r)
	if !ok {
		return
	}
	artifacts, err := h.sourceArtifacts.List(limit)
	if err != nil {
		writeHTTPError(w, http.StatusServiceUnavailable, "source agent artifact catalog unavailable")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeHTTPJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
}

func parseSourceAgentArtifactListLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	query := r.URL.Query()
	if len(query) == 0 {
		return 0, true
	}
	values, exists := query["limit"]
	if len(query) != 1 || !exists || len(values) != 1 {
		writeHTTPError(w, http.StatusBadRequest, "invalid artifact list request")
		return 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(values[0]))
	if err != nil || limit < 0 {
		writeHTTPError(w, http.StatusBadRequest, "invalid artifact list request")
		return 0, false
	}
	if limit > sourceAgentArtifactListMax {
		limit = sourceAgentArtifactListMax
	}
	return limit, true
}

func (h *kbaseHTTPHandler) handleSourceAgentManagement(w http.ResponseWriter, r *http.Request) {
	agentID, action, ok, invalid := parseSourceAgentManagementPath(r)
	if invalid {
		writeHTTPError(w, http.StatusBadRequest, "invalid agent id")
		return
	}
	if !ok {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	switch action {
	case "":
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		agent, err := h.sourceSync.GetSourceAgent(agentID)
		if err != nil {
			h.writeSourceAgentManagementError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"agent": agent})
	case "desired-state":
		h.handleSourceAgentDesiredState(w, r, agentID)
	case "commands":
		h.handleSourceAgentManagementCommands(w, r, agentID)
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
	}
}

func (h *kbaseHTTPHandler) handleSourceAgentDesiredState(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var payload struct {
		DesiredState string `json:"desired_state"`
	}
	if !decodeStrictLimitedHTTPJSON(w, r, defaultSourceAgentCommandHTTPMaxBodyBytes, &payload) {
		return
	}
	agent, err := h.sourceSync.SetAgentDesiredState(agentID, payload.DesiredState)
	if err != nil {
		h.writeSourceAgentManagementError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"agent": agent})
}

func (h *kbaseHTTPHandler) handleSourceAgentManagementCommands(w http.ResponseWriter, r *http.Request, agentID string) {
	switch r.Method {
	case http.MethodGet:
		limit, ok := parseSourceAgentCommandListLimit(w, r)
		if !ok {
			return
		}
		commands, err := h.sourceSync.ListSourceAgentCommands(agentID, limit)
		if err != nil {
			h.writeSourceAgentManagementError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"commands": commands})
	case http.MethodPost:
		var payload struct {
			Type           string          `json:"type"`
			IdempotencyKey string          `json:"idempotency_key"`
			Payload        json.RawMessage `json:"payload,omitempty"`
			ExpiresAt      string          `json:"expires_at"`
		}
		if !decodeStrictLimitedHTTPJSON(w, r, defaultSourceAgentCommandHTTPMaxBodyBytes, &payload) {
			return
		}
		payload.Type = strings.ToLower(strings.TrimSpace(payload.Type))
		if payload.Type != SourceAgentCommandDiagnose && payload.Type != SourceAgentCommandUpgrade && payload.Type != SourceAgentCommandRestart {
			writeHTTPError(w, http.StatusBadRequest, "invalid source agent command type")
			return
		}
		if payload.Type == SourceAgentCommandUpgrade || payload.Type == SourceAgentCommandRestart {
			auth, ok := kbaseRequestAuthFromContext(r.Context())
			if !ok || auth.Method != kbaseAuthMethodCookie {
				writeHTTPError(w, http.StatusForbidden, "browser management session required")
				return
			}
		}
		if payload.Type == SourceAgentCommandUpgrade {
			if h.sourceArtifacts == nil {
				writeHTTPError(w, http.StatusConflict, "source agent artifact unavailable")
				return
			}
			_, _, spec, err := normalizeSourceAgentCommandCreate(SourceAgentCommandCreate{
				TargetAgentID: agentID, Type: payload.Type, IdempotencyKey: payload.IdempotencyKey,
				Payload: payload.Payload, ExpiresAt: payload.ExpiresAt,
			}, h.sourceSync.now().UTC())
			if err != nil || spec == nil {
				h.writeSourceAgentManagementError(w, err)
				return
			}
			agent, err := h.sourceSync.GetSourceAgent(agentID)
			if err != nil {
				h.writeSourceAgentManagementError(w, err)
				return
			}
			if _, err := h.sourceArtifacts.selectForRollout(spec.ArtifactID, sourceAgentArtifactTargetFromAgent(agent)); err != nil {
				h.writeSourceAgentManagementError(w, err)
				return
			}
		}
		command, err := h.sourceSync.CreateSourceAgentCommand(SourceAgentCommandCreate{
			TargetAgentID:  agentID,
			Type:           payload.Type,
			IdempotencyKey: payload.IdempotencyKey,
			Payload:        payload.Payload,
			ExpiresAt:      payload.ExpiresAt,
		})
		if err != nil {
			h.writeSourceAgentManagementError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusCreated, map[string]any{"command": command})
	default:
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func parseSourceAgentManagementPath(r *http.Request) (string, string, bool, bool) {
	const prefix = "/api/source-agents/"
	rawPath := r.URL.EscapedPath()
	if !strings.HasPrefix(rawPath, prefix) {
		return "", "", false, false
	}
	parts := strings.Split(strings.TrimPrefix(rawPath, prefix), "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" || len(parts) == 2 && parts[1] == "" {
		return "", "", false, false
	}
	agentID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", false, true
	}
	agentID, err = normalizeSourceAgentCommandIdentifier("agent_id", agentID, true)
	if err != nil {
		return "", "", false, true
	}
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	return agentID, action, true, false
}

func parseSourceAgentCommandListLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	values, exists := r.URL.Query()["limit"]
	if !exists {
		return 0, true
	}
	if len(values) != 1 {
		writeHTTPError(w, http.StatusBadRequest, "invalid command list limit")
		return 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(values[0]))
	if err != nil || limit < 0 {
		writeHTTPError(w, http.StatusBadRequest, "invalid command list limit")
		return 0, false
	}
	if limit > sourceAgentCommandListMax {
		limit = sourceAgentCommandListMax
	}
	return limit, true
}

func (h *kbaseHTTPHandler) writeSourceAgentManagementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSourceAgentNotFound):
		writeHTTPError(w, http.StatusNotFound, "source agent not found")
	case errors.Is(err, ErrSourceAgentCommandNotFound):
		writeHTTPError(w, http.StatusNotFound, "source agent command not found")
	case errors.Is(err, ErrSourceAgentCommandVersionConflict),
		errors.Is(err, ErrSourceAgentCommandIdempotencyConflict),
		errors.Is(err, ErrSourceAgentCommandActiveUpgrade),
		errors.Is(err, ErrSourceAgentCommandCapability),
		errors.Is(err, ErrSourceAgentArtifactNotFound), errors.Is(err, ErrSourceAgentArtifactNotAllowed),
		errors.Is(err, ErrSourceAgentArtifactIncompatible), errors.Is(err, ErrSourceAgentArtifactIntegrity),
		errors.Is(err, ErrSourceAgentArtifactCatalogInvalid):
		writeHTTPError(w, http.StatusConflict, "source agent command conflict")
	case errors.Is(err, ErrSourceAgentDesiredState), errors.Is(err, ErrSourceAgentCommandType),
		isSourceAgentCommandInputError(err):
		writeHTTPError(w, http.StatusBadRequest, "invalid source agent request")
	default:
		writeHTTPError(w, http.StatusInternalServerError, "source agent management unavailable")
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

func decodeStrictLimitedHTTPJSON(w http.ResponseWriter, r *http.Request, limit int64, value any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeStrictHTTPJSONDecodeError(w, err)
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			writeStrictHTTPJSONDecodeError(w, err)
		} else {
			writeHTTPError(w, http.StatusBadRequest, "JSON body must contain one value")
		}
		return false
	}
	return true
}

func writeStrictHTTPJSONDecodeError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeHTTPError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeHTTPError(w, http.StatusBadRequest, "invalid JSON body")
}

func isSourceAgentCommandInputError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		" is required", " exceeds ", " must be ", " must contain ",
		" contains invalid ", "unsupported source agent", "invalid command",
		"payload", "result_code", "actual_version", "expires_at", "message",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
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

type BrowserSessionHTTPConfig struct {
	Store           *BrowserSessionStore
	AdminToken      string
	PublicOrigin    string
	TTL             time.Duration
	RenewalInterval time.Duration
	MaxActive       int
}

func normalizeBrowserSessionHTTPConfig(cfg BrowserSessionHTTPConfig) BrowserSessionHTTPConfig {
	cfg.AdminToken = strings.TrimSpace(cfg.AdminToken)
	cfg.PublicOrigin = strings.TrimSpace(cfg.PublicOrigin)
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * 24 * time.Hour
	}
	if cfg.RenewalInterval <= 0 {
		cfg.RenewalInterval = 5 * time.Minute
	}
	if cfg.MaxActive <= 0 {
		cfg.MaxActive = 10
	}
	return cfg
}

const (
	browserSessionCookieName            = "__Host-kbase_session"
	browserSessionProxyHeaderName       = "X-KBase-Browser-Session"
	browserSessionClientIDHeaderName    = "X-KBase-Browser-Client-ID"
	browserSessionEpochHeaderName       = "X-KBase-Browser-Epoch"
	maxBrowserSessionProxySecretBytes   = 256
	maxBrowserSessionAuthorizationBytes = 4096
	maxBrowserSessionOriginBytes        = 2048
	maxBrowserSessionCookieBytes        = 256
	maxBrowserSessionUserAgentBytes     = 512
	maxBrowserSessionFetchSiteBytes     = 64
	maxBrowserSessionCSRFBytes          = 256
	maxBrowserSessionEpochBytes         = 19
)

const browserSessionAdminPath = "/api/admin/browser-sessions"

var browserSessionIDPattern = regexp.MustCompile(`^session_[A-Za-z0-9_-]{1,128}$`)

func (h *kbaseHTTPHandler) handleBrowserSessionAdminRoute(
	w http.ResponseWriter,
	r *http.Request,
) bool {
	if r.URL.Path != browserSessionAdminPath &&
		!strings.HasPrefix(r.URL.Path, browserSessionAdminPath+"/") {
		return false
	}
	setBrowserSessionNoStore(w)
	if h.browserSessions.AdminToken == "" || h.browserSessions.Store == nil {
		writeHTTPError(w, http.StatusServiceUnavailable, "service unavailable")
		return true
	}
	token, valid := singleBearerToken(r)
	if !valid || !constantTimeStringEqual(token, h.browserSessions.AdminToken) {
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		return true
	}

	switch r.URL.Path {
	case browserSessionAdminPath:
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		h.handleBrowserSessionAdminList(w)
	case browserSessionAdminPath + "/revoke-all":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		h.handleBrowserSessionAdminRevokeAll(w)
	default:
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", http.MethodDelete)
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		h.handleBrowserSessionAdminRevoke(w, r)
	}
	return true
}

func (h *kbaseHTTPHandler) handleBrowserSessionAdminList(w http.ResponseWriter) {
	sessions, err := h.browserSessions.Store.List()
	if err != nil {
		writeBrowserSessionStoreError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h *kbaseHTTPHandler) handleBrowserSessionAdminRevoke(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeHTTPError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	escapedPath := r.URL.EscapedPath()
	escapedPrefix := browserSessionAdminPath + "/"
	if !strings.HasPrefix(escapedPath, escapedPrefix) {
		writeHTTPError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	escapedID := strings.TrimPrefix(escapedPath, escapedPrefix)
	if escapedID == "" || strings.Contains(escapedID, "/") {
		writeHTTPError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	sessionID, err := url.PathUnescape(escapedID)
	if err != nil || !browserSessionIDPattern.MatchString(sessionID) {
		writeHTTPError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	if err := h.browserSessions.Store.Revoke(sessionID, "admin"); err != nil {
		writeBrowserSessionStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *kbaseHTTPHandler) handleBrowserSessionAdminRevokeAll(w http.ResponseWriter) {
	revokedCount, err := h.browserSessions.Store.RevokeAll("admin")
	if err != nil {
		writeBrowserSessionStoreError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"revoked_count": revokedCount})
}

func (h *kbaseHTTPHandler) handleBrowserSessionRoute(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/browser/session":
		h.handleBrowserSessionLogin(w, r)
	case "/browser/session/migrate":
		h.handleBrowserSessionMigration(w, r)
	case "/browser/session-token":
		h.handleLegacyBrowserToken(w, r)
	default:
		return false
	}
	return true
}

func (h *kbaseHTTPHandler) handleBrowserSessionAPIRoute(
	w http.ResponseWriter,
	r *http.Request,
) bool {
	switch r.URL.Path {
	case "/api/browser/session":
		h.handleBrowserSessionStatus(w, r)
	case "/api/browser/session/logout":
		h.handleBrowserSessionLogout(w, r)
	default:
		return false
	}
	return true
}

func (h *kbaseHTTPHandler) handleBrowserSessionStatus(w http.ResponseWriter, r *http.Request) {
	setBrowserSessionNoStore(w)
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, authorized := h.authorizeBrowserSessionOnly(w, r, true)
	if !authorized {
		return
	}
	r = requestWithKBaseAuth(r, auth)
	csrfToken, csrfExpiresAt, err := h.browserSessions.Store.IssueCSRF(auth.sessionToken)
	if err != nil {
		if isBrowserSessionCredentialError(err) {
			w.Header().Del("Set-Cookie")
			clearBrowserSessionCookie(w)
			writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		} else {
			writeBrowserSessionStoreError(w, err)
		}
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"session":         auth.session,
		"client_id":       auth.session.ClientID,
		"epoch":           auth.session.IssuedEpoch,
		"csrf_token":      csrfToken,
		"csrf_expires_at": csrfExpiresAt,
	})
}

func (h *kbaseHTTPHandler) handleBrowserSessionLogout(w http.ResponseWriter, r *http.Request) {
	setBrowserSessionNoStore(w)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, authorized := h.authorizeBrowserSessionOnly(w, r, false)
	if !authorized {
		return
	}
	r = requestWithKBaseAuth(r, auth)
	if !h.authorizeBrowserSessionCSRF(w, r, auth, false) {
		return
	}
	if _, err := h.browserSessions.Store.FenceClientBySession(auth.SessionID, "logout"); err != nil {
		writeBrowserSessionStoreError(w, err)
		return
	}
	w.Header().Del("Set-Cookie")
	clearBrowserSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *kbaseHTTPHandler) authorizeBrowserSessionOnly(
	w http.ResponseWriter,
	r *http.Request,
	renew bool,
) (kbaseRequestAuth, bool) {
	if len(r.Header.Values("Authorization")) != 0 {
		if !h.recordBrowserSessionAuthenticationRejected(
			w,
			false,
			"unexpected_authorization",
		) {
			return kbaseRequestAuth{}, false
		}
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		return kbaseRequestAuth{}, false
	}
	return h.authorizeKBaseRequest(w, r, false, renew)
}

func (h *kbaseHTTPHandler) handleBrowserSessionLogin(w http.ResponseWriter, r *http.Request) {
	setBrowserSessionNoStore(w)
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if len(r.Header.Values("Authorization")) != 0 {
		if !h.recordBrowserSessionAuthenticationRejected(
			w,
			false,
			"unexpected_authorization",
		) {
			return
		}
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.browserSessionSecret == "" || h.browserSessions.Store == nil {
		writeHTTPError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	proxySecret, ok := singleBoundedHeader(
		r,
		browserSessionProxyHeaderName,
		maxBrowserSessionProxySecretBytes,
	)
	if !ok || !constantTimeStringEqual(proxySecret, h.browserSessionSecret) {
		if !h.recordBrowserSessionAuthenticationRejected(
			w,
			false,
			"proxy_credential",
		) {
			return
		}
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	clientID, expectedEpoch, valid := browserSessionClientPrecondition(w, r, r.Method == http.MethodPost)
	if !valid {
		return
	}
	if r.Method == http.MethodGet {
		family, err := h.browserSessions.Store.AcquireClientEpoch(clientID)
		if err != nil {
			writeBrowserSessionStoreError(w, err)
			return
		}
		writeBrowserClientMetadata(w, family)
		return
	}

	userAgent := boundedUserAgent(r)
	credentials, err := h.browserSessions.Store.Create(BrowserSessionCreate{
		ClientID:      clientID,
		ExpectedEpoch: expectedEpoch,
		DeviceLabel:   browserSessionDeviceLabel(userAgent),
		UserAgent:     userAgent,
	})
	if err != nil {
		writeBrowserSessionStoreError(w, err)
		return
	}
	setBrowserSessionCookie(
		w,
		credentials.Token,
		credentials.Session.ExpiresAt,
		h.browserSessions.TTL,
	)
	writeBrowserSessionMetadata(w, credentials.Session)
}

func (h *kbaseHTTPHandler) handleBrowserSessionMigration(w http.ResponseWriter, r *http.Request) {
	setBrowserSessionNoStore(w)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.browserSessions.Store == nil || h.browserSessions.PublicOrigin == "" {
		writeHTTPError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	origin, ok := singleBoundedHeader(r, "Origin", maxBrowserSessionOriginBytes)
	if !ok || origin != h.browserSessions.PublicOrigin {
		writeHTTPError(w, http.StatusForbidden, "forbidden")
		return
	}
	clientID, expectedEpoch, valid := browserSessionClientPrecondition(w, r, true)
	if !valid {
		return
	}

	sessionToken, cookiePresent, cookieValid := browserSessionCookieToken(r)
	var cookieCredentialErr error
	if cookieValid {
		auth, err := h.browserSessions.Store.AuthenticateAndRenewExpected(
			sessionToken,
			clientID,
			expectedEpoch,
		)
		if err == nil {
			if auth.SetCookie {
				setBrowserSessionCookie(
					w,
					sessionToken,
					auth.CookieExpiresAt,
					h.browserSessions.TTL,
				)
			}
			writeBrowserSessionMetadata(w, auth.Session)
			return
		}
		if errors.Is(err, ErrBrowserSessionStaleEpoch) {
			writeBrowserSessionStoreError(w, err)
			return
		}
		if !isBrowserSessionCredentialError(err) {
			writeBrowserSessionStoreError(w, err)
			return
		}
		cookieCredentialErr = err
	}

	if validBrowserSessionMigrationBearer(r, h.authToken) {
		userAgent := boundedUserAgent(r)
		credentials, err := h.browserSessions.Store.Create(BrowserSessionCreate{
			ClientID:      clientID,
			ExpectedEpoch: expectedEpoch,
			DeviceLabel:   browserSessionDeviceLabel(userAgent),
			UserAgent:     userAgent,
			AuditType:     BrowserSessionAuditMigration,
		})
		if err != nil {
			writeBrowserSessionStoreError(w, err)
			return
		}
		setBrowserSessionCookie(
			w,
			credentials.Token,
			credentials.Session.ExpiresAt,
			h.browserSessions.TTL,
		)
		writeBrowserSessionMetadata(w, credentials.Session)
		return
	}

	family, err := h.browserSessions.Store.ReadClientEpoch(clientID)
	if err != nil {
		writeBrowserSessionStoreError(w, err)
		return
	}
	if family.Epoch != expectedEpoch {
		writeBrowserSessionStaleEpoch(w, &BrowserSessionStaleEpochError{
			ClientID:     clientID,
			CurrentEpoch: family.Epoch,
		})
		return
	}
	var auditErr error
	switch {
	case cookieCredentialErr != nil:
		auditErr = h.browserSessions.Store.RecordAuthenticationRejectedByToken(
			sessionToken,
			browserSessionAuthenticationAuditReason(cookieCredentialErr),
		)
	case cookiePresent:
		auditErr = h.browserSessions.Store.RecordAuthenticationRejected(
			"",
			"",
			"invalid_cookie",
		)
	default:
		auditErr = h.browserSessions.Store.RecordAuthenticationRejected(
			"",
			"",
			"migration_credential",
		)
	}
	if auditErr != nil {
		writeBrowserSessionStoreError(w, auditErr)
		return
	}
	if cookiePresent {
		clearBrowserSessionCookie(w)
	}
	writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
}

func setBrowserSessionCookie(
	w http.ResponseWriter,
	token string,
	expires time.Time,
	ttl time.Duration,
) {
	http.SetCookie(w, &http.Cookie{
		Name:     browserSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires.UTC(),
		MaxAge:   int(ttl / time.Second),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearBrowserSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     browserSessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func writeBrowserSessionStoreError(w http.ResponseWriter, err error) {
	var stale *BrowserSessionStaleEpochError
	if errors.As(err, &stale) {
		writeBrowserSessionStaleEpoch(w, stale)
		return
	}
	if errors.Is(err, ErrBrowserSessionClientUninitialized) {
		w.Header().Del("Set-Cookie")
		writeHTTPError(
			w,
			http.StatusPreconditionRequired,
			"browser client is not initialized",
		)
		return
	}
	writeHTTPError(w, http.StatusServiceUnavailable, "service unavailable")
}

func writeBrowserSessionStaleEpoch(w http.ResponseWriter, stale *BrowserSessionStaleEpochError) {
	w.Header().Del("Set-Cookie")
	writeHTTPJSON(w, http.StatusConflict, map[string]any{
		"error":     "stale browser session epoch",
		"client_id": stale.ClientID,
		"epoch":     stale.CurrentEpoch,
	})
}

func browserSessionClientPrecondition(
	w http.ResponseWriter,
	r *http.Request,
	requireEpoch bool,
) (string, int64, bool) {
	clientValues := r.Header.Values(browserSessionClientIDHeaderName)
	if len(clientValues) == 0 {
		writeHTTPError(w, http.StatusPreconditionRequired, "browser client id is required")
		return "", 0, false
	}
	if len(clientValues) != 1 ||
		len(clientValues[0]) > maxBrowserSessionClientIDBytes ||
		validateBrowserSessionClientID(clientValues[0]) != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid browser client id")
		return "", 0, false
	}
	if !requireEpoch {
		if len(r.Header.Values(browserSessionEpochHeaderName)) != 0 {
			writeHTTPError(w, http.StatusBadRequest, "browser epoch is not allowed")
			return "", 0, false
		}
		return clientValues[0], 0, true
	}

	epochValues := r.Header.Values(browserSessionEpochHeaderName)
	if len(epochValues) == 0 {
		writeHTTPError(w, http.StatusPreconditionRequired, "browser epoch is required")
		return "", 0, false
	}
	if len(epochValues) != 1 ||
		len(epochValues[0]) == 0 ||
		len(epochValues[0]) > maxBrowserSessionEpochBytes {
		writeHTTPError(w, http.StatusBadRequest, "invalid browser epoch")
		return "", 0, false
	}
	epoch, err := strconv.ParseInt(epochValues[0], 10, 64)
	if err != nil || epoch <= 0 || strconv.FormatInt(epoch, 10) != epochValues[0] {
		writeHTTPError(w, http.StatusBadRequest, "invalid browser epoch")
		return "", 0, false
	}
	return clientValues[0], epoch, true
}

func isBrowserSessionCredentialError(err error) bool {
	return errors.Is(err, ErrBrowserSessionMissing) ||
		errors.Is(err, ErrBrowserSessionExpired) ||
		errors.Is(err, ErrBrowserSessionRevoked) ||
		errors.Is(err, ErrBrowserSessionClientMismatch)
}

func singleBoundedHeader(r *http.Request, name string, maxBytes int) (string, bool) {
	values := r.Header.Values(name)
	if len(values) != 1 || len(values[0]) > maxBytes {
		return "", false
	}
	return values[0], true
}

func singleBearerToken(r *http.Request) (string, bool) {
	value, ok := singleBoundedHeader(
		r,
		"Authorization",
		maxBrowserSessionAuthorizationBytes,
	)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	return token, token != ""
}

func boundedUserAgent(r *http.Request) string {
	values := r.Header.Values("User-Agent")
	if len(values) != 1 {
		return ""
	}
	userAgent := values[0]
	if len(userAgent) > maxBrowserSessionUserAgentBytes {
		userAgent = userAgent[:maxBrowserSessionUserAgentBytes]
	}
	return userAgent
}

func browserSessionCookieToken(r *http.Request) (token string, present bool, valid bool) {
	for _, cookie := range r.Cookies() {
		if cookie.Name != browserSessionCookieName {
			continue
		}
		if present {
			return "", true, false
		}
		present = true
		token = cookie.Value
	}
	if !present || token == "" || len(token) > maxBrowserSessionCookieBytes {
		return token, present, false
	}
	return token, true, true
}

func validBrowserSessionMigrationBearer(r *http.Request, expected string) bool {
	if expected == "" {
		return false
	}
	token, valid := singleBearerToken(r)
	return valid && constantTimeStringEqual(token, expected)
}

func constantTimeStringEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

func browserSessionDeviceLabel(userAgent string) string {
	lower := strings.ToLower(userAgent)
	browser := "Other Browser"
	switch {
	case strings.Contains(lower, "edg/") ||
		strings.Contains(lower, "edga/") ||
		strings.Contains(lower, "edgios/"):
		browser = "Edge"
	case strings.Contains(lower, "firefox/") || strings.Contains(lower, "fxios/"):
		browser = "Firefox"
	case strings.Contains(lower, "chrome/") || strings.Contains(lower, "crios/"):
		browser = "Chrome"
	case strings.Contains(lower, "safari/"):
		browser = "Safari"
	}

	operatingSystem := "Other OS"
	switch {
	case strings.Contains(lower, "iphone") ||
		strings.Contains(lower, "ipad") ||
		strings.Contains(lower, "ipod"):
		operatingSystem = "iOS"
	case strings.Contains(lower, "android"):
		operatingSystem = "Android"
	case strings.Contains(lower, "windows"):
		operatingSystem = "Windows"
	case strings.Contains(lower, "cros"):
		operatingSystem = "ChromeOS"
	case strings.Contains(lower, "mac os x") || strings.Contains(lower, "macintosh"):
		operatingSystem = "macOS"
	case strings.Contains(lower, "linux"):
		operatingSystem = "Linux"
	}
	return browser + " on " + operatingSystem
}
