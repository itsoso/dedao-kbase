package app

import (
	"encoding/json"
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
