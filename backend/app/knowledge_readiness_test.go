package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildKnowledgeReadinessProjectsPipelineAndEvidenceStates(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	pkg.Book.SourceType = "wechat_mp"
	pkg.Book.SourceAccount = "publisher"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}

	initial, err := BuildKnowledgeReadiness(store, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if initial.SchemaVersion != KnowledgeReadinessSchemaVersion || initial.Summary.Total != 1 || initial.Summary.NeedsAnalysis != 1 {
		t.Fatalf("initial = %#v", initial)
	}

	readyStore := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(readyStore, "42"); err != nil {
		t.Fatal(err)
	}
	ready, err := BuildKnowledgeReadiness(readyStore, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ready.Items) != 1 || ready.Items[0].NextAction != "ready_to_publish" {
		t.Fatalf("ready = %#v", ready)
	}
	if ready.Summary.AnalysisClaims != 1 || ready.Summary.ClaimsWithEvidence != 1 ||
		ready.Summary.ClaimCoverage != 1 || ready.Summary.ResolutionRate != 1 {
		t.Fatalf("ready summary = %#v", ready.Summary)
	}
	if ready.Items[0].LegacyDirectChunkReferences != 1 ||
		!containsString(ready.Items[0].WarningCodes, "legacy_direct_chunk_reference") {
		t.Fatalf("ready item = %#v", ready.Items[0])
	}
}

func TestBuildKnowledgeReadinessMarksInvalidEvidenceBlocked(t *testing.T) {
	store := qualityTestStore(t)
	manifest, err := store.LoadAnalysisManifest("42")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Payload.Claims[0].CitationIDs = []string{"missing"}
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}

	readiness, err := BuildKnowledgeReadiness(store, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Summary.Blocked != 1 || readiness.Items[0].NextAction != "blocked" {
		t.Fatalf("readiness = %#v", readiness)
	}
	if !containsString(readiness.Items[0].BlockerCodes, "unresolved_evidence_reference") {
		t.Fatalf("item = %#v", readiness.Items[0])
	}
}

func TestBuildKnowledgeReadinessIncludesCurrentRelease(t *testing.T) {
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	release, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatal(err)
	}

	readiness, err := BuildKnowledgeReadiness(store, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Summary.Published != 1 || readiness.Summary.Ready != 1 {
		t.Fatalf("summary = %#v", readiness.Summary)
	}
	if readiness.Items[0].LastPublishedReleaseID != release.ReleaseID {
		t.Fatalf("item = %#v", readiness.Items[0])
	}
}

func TestBuildKnowledgeReadinessFiltersAndLimitsDeterministically(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	for _, id := range []string{"book-b", "book-a"} {
		pkg := evidenceTestPackage()
		pkg.Book.BookID = id
		pkg.Book.Title = id
		pkg.Book.UpdatedAt = "2026-07-25T10:00:00Z"
		for index := range pkg.Chapters {
			pkg.Chapters[index].BookID = id
		}
		for index := range pkg.Chunks {
			pkg.Chunks[index].BookID = id
		}
		for index := range pkg.Claims {
			pkg.Claims[index].BookID = id
		}
		for index := range pkg.Citations {
			pkg.Citations[index].BookID = id
		}
		if err := store.SavePackage(pkg); err != nil {
			t.Fatal(err)
		}
	}

	filtered, err := BuildKnowledgeReadiness(store, 100, "book-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Items) != 1 || filtered.Items[0].BookID != "book-b" {
		t.Fatalf("filtered = %#v", filtered)
	}
	limited, err := BuildKnowledgeReadiness(store, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Items) != 1 || limited.Items[0].BookID != "book-a" {
		t.Fatalf("limited = %#v", limited)
	}
	if limited.Summary.Total != 2 || limited.Summary.NeedsAnalysis != 2 {
		t.Fatalf("limited summary must cover all matching books: %#v", limited.Summary)
	}
}

func TestBuildKnowledgeReadinessResponseIsPrivacySafe(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := evidenceTestPackage()
	pkg.Book.SourceHTML = "sensitive-local-path/downloaded.html"
	pkg.Book.SourceAccount = "sensitive-local-path/account"
	pkg.Chunks[0].Text = "private body sentinel"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}

	readiness, err := BuildKnowledgeReadiness(store, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(readiness)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(payload)
	for _, forbidden := range []string{"sensitive-local-path", "private body sentinel", "downloaded.html", `"source_account":`, `"prompt"`, `"answer"`, `"token"`} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("readiness leaked %q: %s", forbidden, raw)
		}
	}
}
