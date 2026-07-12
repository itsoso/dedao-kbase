package app

import "testing"

func TestEvaluateBookAnalysisQualityPassesGroundedPayload(t *testing.T) {
	store := qualityTestStore(t)
	report, err := EvaluateBookAnalysisQuality(store, "42")
	if err != nil {
		t.Fatalf("EvaluateBookAnalysisQuality returned error: %v", err)
	}
	if report.Decision != BookQualityPass || report.UsagePolicy != BookUsageStandard {
		t.Fatalf("report = %#v", report)
	}
	stored, err := store.LoadBookQualityReport("42")
	if err != nil || stored.Decision != BookQualityPass {
		t.Fatalf("stored report = %#v, err=%v", stored, err)
	}
}

func TestEvaluateBookAnalysisQualityQuarantinesMissingCitation(t *testing.T) {
	store := qualityTestStore(t)
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.Payload.Claims[0].CitationIDs = nil
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateBookAnalysisQuality(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision != BookQualityQuarantine || !qualityRuleFailed(report, "claim_citations") {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateBookAnalysisQualityQuarantinesUnknownCitation(t *testing.T) {
	store := qualityTestStore(t)
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.Payload.Claims[0].CitationIDs = []string{"unknown-chunk"}
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateBookAnalysisQuality(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision != BookQualityQuarantine || !qualityRuleFailed(report, "citation_integrity") {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateBookAnalysisQualityQuarantinesInvalidClaimMetadata(t *testing.T) {
	store := qualityTestStore(t)
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.Payload.Claims[0].Confidence = 1.5
	manifest.Payload.Claims[0].RiskLevel = "critical"
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateBookAnalysisQuality(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision != BookQualityQuarantine || !qualityRuleFailed(report, "claim_metadata") {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateBookAnalysisQualityRejectsContentHashMismatch(t *testing.T) {
	store := qualityTestStore(t)
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.ContentHash = "stale-hash"
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateBookAnalysisQuality(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision != BookQualityReject || !qualityRuleFailed(report, "content_version") {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateBookAnalysisQualityMarksHighRiskEvidenceOnly(t *testing.T) {
	store := qualityTestStore(t)
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.Payload.Claims[0].RiskLevel = "high"
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateBookAnalysisQuality(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision != BookQualityPass || report.UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("report = %#v", report)
	}
}

func qualityTestStore(t *testing.T) *BookKnowledgeStore {
	t.Helper()
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	manifest := BookAnalysisManifest{
		Version: "1", BookID: "42", ContentHash: pkg.Book.ContentHash, Status: BookAnalysisReady,
		Payload: &BookAnalysisPayload{
			Summary: "基于本地证据的摘要。",
			Claims: []BookAnalysisClaim{{
				ID: "claim-1", Statement: "趋势过滤是前置条件。", CitationIDs: []string{"42-chunk-1"},
				Confidence: 0.86, Scope: []string{"示例策略"}, RiskLevel: "medium",
			}},
			Risks:   []BookAnalysisRisk{{ID: "risk-1", Description: "需要外部验证。", CitationIDs: []string{"42-chunk-1"}, Severity: "medium"}},
			Actions: []BookAnalysisAction{{ID: "action-1", Description: "核对样本。", CitationIDs: []string{"42-chunk-1"}, Kind: "verify"}},
		},
		Sources:   []BookKnowledgeChatSource{{Kind: "chunk", ID: "42-chunk-1", ChapterID: "42-chapter-1"}},
		UpdatedAt: "2026-07-12T12:00:00Z",
	}
	if err := store.SaveAnalysisManifest(manifest); err != nil {
		t.Fatal(err)
	}
	return store
}

func qualityRuleFailed(report *BookQualityReport, id string) bool {
	for _, rule := range report.Rules {
		if rule.ID == id {
			return !rule.Passed
		}
	}
	return false
}
