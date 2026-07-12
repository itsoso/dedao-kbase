package app

import (
	"os"
	"strings"
	"testing"
)

func TestKnowledgeReleasePublicationIsContentAddressedAndIdempotent(t *testing.T) {
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	first, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatalf("PublishKnowledgeRelease returned error: %v", err)
	}
	second, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatalf("second PublishKnowledgeRelease returned error: %v", err)
	}
	if first.ReleaseID == "" || first.ReleaseID != second.ReleaseID || first.CreatedAt != second.CreatedAt {
		t.Fatalf("releases = first %#v second %#v", first, second)
	}
	loaded, err := store.LoadKnowledgeRelease(first.ReleaseID)
	if err != nil || loaded.ReleaseID != first.ReleaseID || loaded.Analysis == nil || len(loaded.Citations) == 0 {
		t.Fatalf("loaded release = %#v, err=%v", loaded, err)
	}
	listed, err := store.ListKnowledgeReleases("", 20)
	if err != nil || len(listed) != 1 || listed[0].ReleaseID != first.ReleaseID {
		t.Fatalf("listed releases = %#v, err=%v", listed, err)
	}
}

func TestKnowledgeReleaseRejectsNonPassingQuality(t *testing.T) {
	store := qualityTestStore(t)
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.Payload.Claims[0].CitationIDs = nil
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	_, err := PublishKnowledgeRelease(store, "42")
	if err == nil || !strings.Contains(err.Error(), "quality decision") {
		t.Fatalf("publish error = %v", err)
	}
}

func TestKnowledgeReleaseRejectsAnalysisChangedAfterQualityPass(t *testing.T) {
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.Payload.Claims[0].CitationIDs = nil
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	_, err := PublishKnowledgeRelease(store, "42")
	if err == nil || !strings.Contains(err.Error(), "analysis hash") {
		t.Fatalf("publish changed analysis error = %v", err)
	}
}

func TestKnowledgeReleaseRepairsMissingManifestEntry(t *testing.T) {
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	release, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.KnowledgeReleaseManifestPath()); err != nil {
		t.Fatal(err)
	}
	replayed, err := PublishKnowledgeRelease(store, "42")
	if err != nil || replayed.ReleaseID != release.ReleaseID {
		t.Fatalf("replayed release = %#v, err=%v", replayed, err)
	}
	listed, err := store.ListKnowledgeReleases("", 20)
	if err != nil || len(listed) != 1 || listed[0].ReleaseID != release.ReleaseID {
		t.Fatalf("repaired manifest = %#v, err=%v", listed, err)
	}
}

func TestKnowledgeReleaseContentUpdateCreatesNewImmutableRelease(t *testing.T) {
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	first, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	firstSummary := first.Analysis.Summary

	pkg, _ := store.LoadPackage("42")
	pkg.Book.ContentHash = "content-hash-43"
	pkg.Book.UpdatedAt = "2026-07-12T13:00:00Z"
	if err := store.SavePackage(*pkg); err != nil {
		t.Fatal(err)
	}
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.ContentHash = pkg.Book.ContentHash
	manifest.Payload.Summary = "更新后的摘要。"
	manifest.UpdatedAt = "2026-07-12T13:01:00Z"
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	second, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	if second.ReleaseID == first.ReleaseID || second.Supersedes != first.ReleaseID {
		t.Fatalf("second release = %#v, first=%s", second, first.ReleaseID)
	}
	old, err := store.LoadKnowledgeRelease(first.ReleaseID)
	if err != nil || old.Analysis.Summary != firstSummary || old.ContentHash != "content-hash-42" {
		t.Fatalf("old release mutated: %#v, err=%v", old, err)
	}
}

func TestKnowledgeReleaseHighRiskIsEvidenceOnly(t *testing.T) {
	store := qualityTestStore(t)
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.Payload.Claims[0].RiskLevel = "high"
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	release, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	if release.UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("release usage policy = %q", release.UsagePolicy)
	}
}
