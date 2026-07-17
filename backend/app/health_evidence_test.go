package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestBuildHealthEvidencePackageMapsClaimsCitationsAndSafetyFlags(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveFeedRelease(t, store, sampleHealthEvidenceRelease())

	pkg, err := BuildHealthEvidencePackage(store, "release-health")
	if err != nil {
		t.Fatalf("BuildHealthEvidencePackage returned error: %v", err)
	}
	if pkg.SchemaVersion != HealthEvidenceSchemaVersion || pkg.ReleaseID != "release-health" || pkg.UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("package identity = %#v", pkg)
	}
	if pkg.Source.URI != "https://mp.weixin.qq.com/s/health-1" || pkg.Source.Type != "wechat_mp_article" || pkg.Source.Account != "医学参考" {
		t.Fatalf("source = %#v", pkg.Source)
	}
	if len(pkg.Evidence) != 2 {
		t.Fatalf("evidence len=%d package=%#v", len(pkg.Evidence), pkg)
	}
	first := pkg.Evidence[0]
	if first.ClaimID != "claim-1" || first.Statement == "" || first.EvidenceLevel != "source_claim" {
		t.Fatalf("first evidence = %#v", first)
	}
	if !containsString(first.Tags.Conditions, "高血压") || !containsString(first.Tags.Interventions, "运动") || !containsString(first.Tags.Metrics, "血压") {
		t.Fatalf("first tags = %#v", first.Tags)
	}
	if len(first.Citations) != 1 || first.Citations[0].ChunkID != "chunk-1" || first.Citations[0].SourceURI != "https://mp.weixin.qq.com/s/health-1" {
		t.Fatalf("first citations = %#v", first.Citations)
	}
	if containsString(first.SafetyFlags, "high_risk") {
		t.Fatalf("medium-risk claim should not be high risk: %#v", first.SafetyFlags)
	}
	second := pkg.Evidence[1]
	if !containsString(second.Tags.Conditions, "糖尿病") || !containsString(second.Tags.Interventions, "药物") || !containsString(second.SafetyFlags, "high_risk") {
		t.Fatalf("second evidence = %#v", second)
	}
}

func TestBuildHealthEvidencePackageRejectsNonEvidenceRelease(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := sampleHealthEvidenceRelease()
	release.ReleaseID = "release-standard"
	release.UsagePolicy = BookUsageStandard
	saveFeedRelease(t, store, release)

	if _, err := BuildHealthEvidencePackage(store, "release-standard"); err == nil || !strings.Contains(err.Error(), "not available for health") {
		t.Fatalf("expected non-health release rejection, got %v", err)
	}
}

func TestSearchHealthEvidenceFiltersByTextAndTag(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveFeedRelease(t, store, sampleHealthEvidenceRelease())

	results, err := SearchHealthEvidence(store, HealthEvidenceSearchQuery{Query: "运动", Tag: "高血压", Limit: 10})
	if err != nil {
		t.Fatalf("SearchHealthEvidence returned error: %v", err)
	}
	if len(results.Items) != 1 || results.Items[0].ClaimID != "claim-1" || results.Items[0].ReleaseID != "release-health" {
		t.Fatalf("search results = %#v", results)
	}
	if results.Items[0].URL != "/api/consumers/health/evidence/release-health" {
		t.Fatalf("search URL = %#v", results.Items[0])
	}
	empty, err := SearchHealthEvidence(store, HealthEvidenceSearchQuery{Query: "不存在的健康问题", Limit: 10})
	if err != nil {
		t.Fatalf("empty SearchHealthEvidence returned error: %v", err)
	}
	if empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty search should return stable empty array, got %#v", empty.Items)
	}
}

func TestHealthEvidenceHTTPDetailAndSearch(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveFeedRelease(t, store, sampleHealthEvidenceRelease())
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	unauthorized := requestKBase(handler, http.MethodGet, "/api/consumers/health/evidence/release-health", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	detail := requestKBase(handler, http.MethodGet, "/api/consumers/health/evidence/release-health", "secret-token")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	var pkg HealthEvidencePackage
	if err := json.Unmarshal(detail.Body.Bytes(), &pkg); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	if pkg.ReleaseID != "release-health" || len(pkg.Evidence) != 2 {
		t.Fatalf("detail package = %#v", pkg)
	}
	search := requestKBase(handler, http.MethodGet, "/api/consumers/health/search?q="+url.QueryEscape("运动")+"&tag="+url.QueryEscape("高血压"), "secret-token")
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), `"claim_id":"claim-1"`) || strings.Contains(search.Body.String(), `"claim_id":"claim-2"`) {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodPost, "/api/consumers/health/search", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestBuildHealthEvidenceReadinessExplainsPublicationState(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "needs-analysis", "hash-analysis")
	saveHealthReadinessBook(t, store, "ready", "hash-ready")
	saveHealthAnalysis(t, store, "ready", "hash-ready")
	saveHealthQuality(t, store, "ready", "hash-ready", BookQualityPass, BookUsageEvidenceOnly)
	saveHealthReadinessBook(t, store, "published", "hash-published")
	saveHealthAnalysis(t, store, "published", "hash-published")
	saveHealthQuality(t, store, "published", "hash-published", BookQualityPass, BookUsageEvidenceOnly)
	release := sampleHealthEvidenceRelease()
	release.ReleaseID = "release-published"
	release.BookID = "published"
	release.ContentHash = "hash-published"
	release.Book.BookID = "published"
	release.Book.Title = "published"
	saveFeedRelease(t, store, release)
	saveHealthReadinessBook(t, store, "policy-blocked", "hash-policy")
	saveHealthAnalysis(t, store, "policy-blocked", "hash-policy")
	saveHealthQuality(t, store, "policy-blocked", "hash-policy", BookQualityPass, BookUsageStandard)

	report, err := BuildHealthEvidenceReadiness(store, 20)
	if err != nil {
		t.Fatalf("BuildHealthEvidenceReadiness returned error: %v", err)
	}
	if report.SchemaVersion != HealthEvidenceReadinessSchemaVersion || report.Totals.Total != 4 || report.Totals.ReadyToPublish != 1 || report.Totals.Published != 1 {
		t.Fatalf("report totals = %#v", report)
	}
	items := healthReadinessByBook(report.Items)
	if items["needs-analysis"].Status != HealthEvidenceReadinessNeedsAnalysis || items["needs-analysis"].NextAction != "analyze" {
		t.Fatalf("needs-analysis item = %#v", items["needs-analysis"])
	}
	if items["ready"].Status != HealthEvidenceReadinessReadyToPublish || items["ready"].NextAction != "publish" {
		t.Fatalf("ready item = %#v", items["ready"])
	}
	if items["published"].Status != HealthEvidenceReadinessPublished || items["published"].EvidenceReleaseID != "release-published" {
		t.Fatalf("published item = %#v", items["published"])
	}
	if items["policy-blocked"].Status != HealthEvidenceReadinessPolicyBlocked || items["policy-blocked"].NextAction != "review_policy" {
		t.Fatalf("policy-blocked item = %#v", items["policy-blocked"])
	}
}

func TestHealthEvidenceReadinessHTTP(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "ready", "hash-ready")
	saveHealthAnalysis(t, store, "ready", "hash-ready")
	saveHealthQuality(t, store, "ready", "hash-ready", BookQualityPass, BookUsageEvidenceOnly)
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	unauthorized := requestKBase(handler, http.MethodGet, "/api/consumers/health/readiness", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	resp := requestKBase(handler, http.MethodGet, "/api/consumers/health/readiness?limit=10", "secret-token")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"status":"ready_to_publish"`) {
		t.Fatalf("readiness status=%d body=%s", resp.Code, resp.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodPost, "/api/consumers/health/readiness", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestRunHealthEvidenceAnalysisBatchProcessesNeedsAnalysisAndEvaluatesQuality(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "needs-analysis", "hash-analysis")
	saveHealthReadinessBook(t, store, "also-needs-analysis", "hash-second")
	saveHealthReadinessBook(t, store, "already-ready", "hash-ready")
	saveHealthAnalysis(t, store, "already-ready", "hash-ready")
	saveHealthQuality(t, store, "already-ready", "hash-ready", BookQualityPass, BookUsageEvidenceOnly)
	called := []string{}
	generator := func(_ context.Context, current *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		called = append(called, request.BookID)
		if request.BookID == "needs-analysis" {
			manifest := healthBatchAnalysisManifest(request.BookID, "hash-analysis")
			return &manifest, nil
		}
		return nil, fmt.Errorf("unexpected book %s", request.BookID)
	}

	result, err := RunHealthEvidenceAnalysisBatch(context.Background(), store, generator, HealthEvidenceAnalysisBatchRequest{Limit: 1, Model: "Qwen-3.7-Max"})
	if err != nil {
		t.Fatalf("RunHealthEvidenceAnalysisBatch returned error: %v", err)
	}
	if result.SchemaVersion != HealthEvidenceAnalysisBatchSchemaVersion || result.Processed != 1 || result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("batch result = %#v", result)
	}
	if result.DryRun || result.Eligible != 2 || result.Skipped != 1 || !result.LimitReached {
		t.Fatalf("batch summary = %#v", result)
	}
	if result.RequestedLimit != 1 || result.NextBatchSize != 1 || result.EstimatedBatches != 2 {
		t.Fatalf("batch estimates = %#v", result)
	}
	if result.Scanned != 3 || !result.HasWork || result.QueueState != "ready" || result.RecommendedAction != "run_analysis" {
		t.Fatalf("batch queue state = %#v", result)
	}
	if result.SkippedByStatus[HealthEvidenceReadinessReadyToPublish] != 1 {
		t.Fatalf("skipped statuses = %#v", result.SkippedByStatus)
	}
	if len(called) != 1 || called[0] != "needs-analysis" {
		t.Fatalf("generator calls = %#v", called)
	}
	quality, err := store.LoadBookQualityReport("needs-analysis")
	if err != nil {
		t.Fatalf("quality report was not created: %v", err)
	}
	if quality.Decision != BookQualityPass || quality.UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("quality = %#v", quality)
	}
	readiness, err := BuildHealthEvidenceReadiness(store, 10)
	if err != nil {
		t.Fatal(err)
	}
	items := healthReadinessByBook(readiness.Items)
	if items["needs-analysis"].Status != HealthEvidenceReadinessReadyToPublish {
		t.Fatalf("processed readiness = %#v", items["needs-analysis"])
	}
	if items["also-needs-analysis"].Status != HealthEvidenceReadinessNeedsAnalysis {
		t.Fatalf("limit should leave second item untouched: %#v", items["also-needs-analysis"])
	}
}

func TestRunHealthEvidenceAnalysisBatchDryRunDoesNotMutateOrCallModel(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "needs-analysis", "hash-analysis")
	saveHealthReadinessBook(t, store, "also-needs-analysis", "hash-second")
	called := false
	generator := func(_ context.Context, _ *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		called = true
		return nil, fmt.Errorf("generator should not run for dry run: %s", request.BookID)
	}

	result, err := RunHealthEvidenceAnalysisBatch(context.Background(), store, generator, HealthEvidenceAnalysisBatchRequest{Limit: 2, DryRun: true})
	if err != nil {
		t.Fatalf("RunHealthEvidenceAnalysisBatch returned error: %v", err)
	}
	if called {
		t.Fatal("dry run called model generator")
	}
	if result.Processed != 2 || result.Succeeded != 0 || result.Failed != 0 || len(result.Items) != 2 {
		t.Fatalf("dry-run result = %#v", result)
	}
	if !result.DryRun || result.Eligible != 2 || result.Skipped != 0 || result.LimitReached {
		t.Fatalf("dry-run summary = %#v", result)
	}
	if result.RequestedLimit != 2 || result.NextBatchSize != 2 || result.EstimatedBatches != 1 {
		t.Fatalf("dry-run estimates = %#v", result)
	}
	if result.Scanned != 2 || !result.HasWork || result.QueueState != "ready" || result.RecommendedAction != "run_analysis" {
		t.Fatalf("dry-run queue state = %#v", result)
	}
	if len(result.SkippedByStatus) != 0 {
		t.Fatalf("dry-run skipped statuses = %#v", result.SkippedByStatus)
	}
	for _, item := range result.Items {
		if item.Status != "preview" || item.NextAction != "analyze" || item.NextStatus != HealthEvidenceReadinessNeedsAnalysis {
			t.Fatalf("dry-run item = %#v", item)
		}
	}
	if _, err := store.LoadAnalysisManifest("needs-analysis"); err == nil {
		t.Fatal("dry run wrote analysis manifest")
	}
	if _, err := store.LoadBookQualityReport("needs-analysis"); err == nil {
		t.Fatal("dry run wrote quality report")
	}
	readiness, err := BuildHealthEvidenceReadiness(store, 10)
	if err != nil {
		t.Fatal(err)
	}
	items := healthReadinessByBook(readiness.Items)
	if items["needs-analysis"].Status != HealthEvidenceReadinessNeedsAnalysis || items["also-needs-analysis"].Status != HealthEvidenceReadinessNeedsAnalysis {
		t.Fatalf("dry run changed readiness: %#v", items)
	}
}

func TestRunHealthEvidenceAnalysisBatchSummaryOnlyReturnsCountsWithoutItems(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "needs-analysis", "hash-analysis")
	saveHealthReadinessBook(t, store, "also-needs-analysis", "hash-second")
	saveHealthReadinessBook(t, store, "already-ready", "hash-ready")
	saveHealthAnalysis(t, store, "already-ready", "hash-ready")
	saveHealthQuality(t, store, "already-ready", "hash-ready", BookQualityPass, BookUsageEvidenceOnly)
	called := false
	generator := func(_ context.Context, _ *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		called = true
		return nil, fmt.Errorf("generator should not run for summary only: %s", request.BookID)
	}

	result, err := RunHealthEvidenceAnalysisBatch(context.Background(), store, generator, HealthEvidenceAnalysisBatchRequest{Limit: 1, SummaryOnly: true})
	if err != nil {
		t.Fatalf("RunHealthEvidenceAnalysisBatch returned error: %v", err)
	}
	if called {
		t.Fatal("summary-only request called model generator")
	}
	if !result.DryRun || !result.SummaryOnly || result.Eligible != 2 || result.Skipped != 1 || !result.LimitReached {
		t.Fatalf("summary-only summary = %#v", result)
	}
	if result.RequestedLimit != 1 || result.NextBatchSize != 1 || result.EstimatedBatches != 2 {
		t.Fatalf("summary-only estimates = %#v", result)
	}
	if result.Scanned != 3 || !result.HasWork || result.QueueState != "ready" || result.RecommendedAction != "run_analysis" {
		t.Fatalf("summary-only queue state = %#v", result)
	}
	if result.Processed != 0 || result.Succeeded != 0 || result.Failed != 0 || len(result.Items) != 0 {
		t.Fatalf("summary-only should not process items: %#v", result)
	}
	if result.SkippedByStatus[HealthEvidenceReadinessReadyToPublish] != 1 {
		t.Fatalf("summary-only skipped statuses = %#v", result.SkippedByStatus)
	}
}

func TestHealthEvidenceAnalysisBatchHTTP(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "needs-analysis", "hash-analysis")
	var got BookAnalysisGenerateRequest
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		AnalysisGenerator: func(_ context.Context, _ *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
			got = request
			manifest := healthBatchAnalysisManifest(request.BookID, "hash-analysis")
			return &manifest, nil
		},
	})

	unauthorized := requestJSONKBase(handler, http.MethodPost, "/api/consumers/health/readiness/analyze", "", `{"limit":1}`)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	resp := requestJSONKBase(handler, http.MethodPost, "/api/consumers/health/readiness/analyze", "secret-token", `{"limit":1,"model":"Qwen-3.7-Max"}`)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"succeeded":1`) {
		t.Fatalf("batch status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got.BookID != "needs-analysis" || got.Model != "Qwen-3.7-Max" {
		t.Fatalf("analysis request = %#v", got)
	}
	wrongMethod := requestKBase(handler, http.MethodGet, "/api/consumers/health/readiness/analyze", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestHealthEvidenceAnalysisBatchHTTPDryRunDoesNotCallGenerator(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "needs-analysis", "hash-analysis")
	called := false
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		AnalysisGenerator: func(_ context.Context, _ *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
			called = true
			return nil, fmt.Errorf("generator should not run for dry run: %s", request.BookID)
		},
	})

	resp := requestJSONKBase(handler, http.MethodPost, "/api/consumers/health/readiness/analyze", "secret-token", `{"limit":1,"dry_run":true}`)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"dry_run":true`) || !strings.Contains(resp.Body.String(), `"eligible":1`) || !strings.Contains(resp.Body.String(), `"has_work":true`) || !strings.Contains(resp.Body.String(), `"queue_state":"ready"`) || !strings.Contains(resp.Body.String(), `"recommended_action":"run_analysis"`) || !strings.Contains(resp.Body.String(), `"requested_limit":1`) || !strings.Contains(resp.Body.String(), `"next_batch_size":1`) || !strings.Contains(resp.Body.String(), `"estimated_batches":1`) || !strings.Contains(resp.Body.String(), `"status":"preview"`) {
		t.Fatalf("dry-run batch status=%d body=%s", resp.Code, resp.Body.String())
	}
	if called {
		t.Fatal("dry-run HTTP request called model generator")
	}
}

func TestHealthEvidenceAnalysisBatchHTTPSummaryOnlyDoesNotCallGenerator(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "needs-analysis", "hash-analysis")
	called := false
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		AnalysisGenerator: func(_ context.Context, _ *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
			called = true
			return nil, fmt.Errorf("generator should not run for summary only: %s", request.BookID)
		},
	})

	resp := requestJSONKBase(handler, http.MethodPost, "/api/consumers/health/readiness/analyze", "secret-token", `{"limit":1,"summary_only":true}`)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"dry_run":true`) || !strings.Contains(resp.Body.String(), `"summary_only":true`) || !strings.Contains(resp.Body.String(), `"eligible":1`) || !strings.Contains(resp.Body.String(), `"has_work":true`) || !strings.Contains(resp.Body.String(), `"queue_state":"ready"`) || !strings.Contains(resp.Body.String(), `"recommended_action":"run_analysis"`) || !strings.Contains(resp.Body.String(), `"requested_limit":1`) || !strings.Contains(resp.Body.String(), `"next_batch_size":1`) || !strings.Contains(resp.Body.String(), `"estimated_batches":1`) || !strings.Contains(resp.Body.String(), `"items":[]`) {
		t.Fatalf("summary-only batch status=%d body=%s", resp.Code, resp.Body.String())
	}
	if called {
		t.Fatal("summary-only HTTP request called model generator")
	}
}

func sampleHealthEvidenceRelease() KnowledgeRelease {
	return KnowledgeRelease{
		SchemaVersion: KnowledgeReleaseSchemaVersion,
		Version:       knowledgeReleaseVersion,
		ReleaseID:     "release-health",
		BookID:        "book-health",
		ContentHash:   "hash-health",
		UsagePolicy:   BookUsageEvidenceOnly,
		CreatedAt:     "2026-07-16T10:01:00Z",
		Book: BookKnowledgeBook{
			BookID:        "book-health",
			Title:         "高血压运动与糖尿病药物观察",
			Author:        "医学参考",
			SourceHTML:    "https://mp.weixin.qq.com/s/health-1",
			SourceType:    "wechat_mp_article",
			SourceAccount: "医学参考",
			PublishedAt:   "2026-07-10T00:00:00Z",
		},
		Analysis: &BookAnalysisPayload{
			Summary: "健康证据摘要",
			Claims: []BookAnalysisClaim{
				{
					ID:          "claim-1",
					Statement:   "规律运动可能帮助高血压人群改善血压管理。",
					CitationIDs: []string{"citation-1"},
					Confidence:  0.74,
					Scope:       []string{"成人", "慢病管理"},
					RiskLevel:   "medium",
				},
				{
					ID:          "claim-2",
					Statement:   "糖尿病药物调整属于高风险医疗决策，需要医生评估。",
					CitationIDs: []string{"citation-2"},
					Confidence:  0.82,
					Scope:       []string{"糖尿病"},
					RiskLevel:   "high",
				},
			},
		},
		Citations: []BookKnowledgeCitation{
			{
				CitationID:    "citation-1",
				BookID:        "book-health",
				ChunkID:       "chunk-1",
				SourceHTML:    "https://mp.weixin.qq.com/s/health-1",
				SourceType:    "wechat_mp_article",
				SourceAccount: "医学参考",
				PublishedAt:   "2026-07-10T00:00:00Z",
			},
			{
				CitationID:    "citation-2",
				BookID:        "book-health",
				ChunkID:       "chunk-2",
				SourceHTML:    "https://mp.weixin.qq.com/s/health-1",
				SourceType:    "wechat_mp_article",
				SourceAccount: "医学参考",
				PublishedAt:   "2026-07-10T00:00:00Z",
			},
		},
		Quality: BookQualityReport{
			Decision:    BookQualityPass,
			UsagePolicy: BookUsageEvidenceOnly,
		},
	}
}

func healthBatchAnalysisManifest(bookID, contentHash string) BookAnalysisManifest {
	return BookAnalysisManifest{
		Version:       bookAnalysisVersion,
		BookID:        bookID,
		ContentHash:   contentHash,
		Status:        BookAnalysisReady,
		Model:         "Qwen-3.7-Max",
		PromptVersion: bookAnalysisPromptVersion,
		Payload: &BookAnalysisPayload{
			Summary: "summary",
			Claims: []BookAnalysisClaim{{
				ID:          "claim-1",
				Statement:   "高血压运动证据",
				CitationIDs: []string{bookID + "-citation-1"},
				Confidence:  0.8,
				RiskLevel:   "high",
			}},
		},
		UpdatedAt:   "2026-07-16T10:00:00Z",
		CompletedAt: "2026-07-16T10:00:00Z",
	}
}

func saveHealthReadinessBook(t *testing.T, store *BookKnowledgeStore, bookID, contentHash string) {
	t.Helper()
	updatedAt := "2026-07-16T10:00:00Z"
	if bookID == "needs-analysis" {
		updatedAt = "2026-07-16T11:00:00Z"
	}
	if err := store.SavePackage(BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:      bookID,
			Title:       bookID,
			ContentHash: contentHash,
			UpdatedAt:   updatedAt,
			SourceType:  "wechat_mp_article",
		},
		Chapters: []BookKnowledgeChapter{{ChapterID: bookID + "-chapter-1", BookID: bookID, Order: 1, Title: "chapter"}},
		Chunks:   []BookKnowledgeChunk{{ChunkID: bookID + "-chunk-1", BookID: bookID, ChapterID: bookID + "-chapter-1", Order: 1, Text: "健康证据"}},
		Claims:   []BookKnowledgeClaim{{ClaimID: bookID + "-claim-1", BookID: bookID, Title: "claim", Summary: "健康证据"}},
		Citations: []BookKnowledgeCitation{{
			CitationID: bookID + "-citation-1",
			BookID:     bookID,
			ChunkID:    bookID + "-chunk-1",
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func saveHealthAnalysis(t *testing.T, store *BookKnowledgeStore, bookID, contentHash string) {
	t.Helper()
	if err := store.SaveAnalysisManifest(BookAnalysisManifest{
		Version:     bookAnalysisVersion,
		BookID:      bookID,
		ContentHash: contentHash,
		Status:      BookAnalysisReady,
		Payload: &BookAnalysisPayload{
			Summary: "summary",
			Claims: []BookAnalysisClaim{{
				ID:          "claim-1",
				Statement:   "高血压运动证据",
				CitationIDs: []string{bookID + "-citation-1"},
				Confidence:  0.8,
				RiskLevel:   "high",
			}},
		},
		UpdatedAt:   "2026-07-16T10:00:00Z",
		CompletedAt: "2026-07-16T10:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

func saveHealthQuality(t *testing.T, store *BookKnowledgeStore, bookID, contentHash, decision, usagePolicy string) {
	t.Helper()
	if err := store.SaveBookQualityReport(BookQualityReport{
		Version:     bookQualityVersion,
		BookID:      bookID,
		ContentHash: contentHash,
		Decision:    decision,
		UsagePolicy: usagePolicy,
		EvaluatedAt: "2026-07-16T10:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

func healthReadinessByBook(items []HealthEvidenceReadinessItem) map[string]HealthEvidenceReadinessItem {
	result := make(map[string]HealthEvidenceReadinessItem)
	for _, item := range items {
		result[item.BookID] = item
	}
	return result
}
