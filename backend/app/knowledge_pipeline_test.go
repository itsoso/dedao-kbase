package app

import (
	"testing"
	"time"
)

func TestKnowledgePipelineProjectsCurrentStageFromArtifacts(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	catalog, err := NewKnowledgeCatalogStore(root, nil)
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}
	savePipelinePackage(t, store, "book-normalized", "hash-normalized")
	savePipelinePackage(t, store, "book-analyzed", "hash-analyzed")
	savePipelineAnalysis(t, store, "book-analyzed", "hash-analyzed")
	savePipelinePackage(t, store, "book-candidate", "hash-candidate")
	savePipelineAnalysis(t, store, "book-candidate", "hash-candidate")
	savePipelineQuality(t, store, "book-candidate", "hash-candidate", BookQualityPass)
	savePipelinePackage(t, store, "book-published", "hash-published")
	savePipelineAnalysis(t, store, "book-published", "hash-published")
	savePipelineQuality(t, store, "book-published", "hash-published", BookQualityPass)
	published := KnowledgeRelease{
		SchemaVersion: KnowledgeReleaseSchemaVersion,
		Version:       knowledgeReleaseVersion,
		ReleaseID:     "release-published",
		BookID:        "book-published",
		ContentHash:   "hash-published",
		UsagePolicy:   BookUsageStandard,
		Book:          BookKnowledgeBook{BookID: "book-published", Title: "Published"},
		Analysis:      &BookAnalysisPayload{Summary: "ok", Claims: []BookAnalysisClaim{{ID: "claim-1", Statement: "ok", CitationIDs: []string{"citation-1"}, Confidence: 0.8, RiskLevel: "low"}}},
		Quality:       BookQualityReport{Decision: BookQualityPass, UsagePolicy: BookUsageStandard},
		Citations:     []BookKnowledgeCitation{{CitationID: "citation-1", BookID: "book-published"}},
		CreatedAt:     "2026-07-14T00:00:00Z",
	}
	if err := store.saveKnowledgeRelease(published); err != nil {
		t.Fatalf("save release: %v", err)
	}

	projections, err := RebuildKnowledgePipelineProjection(store, catalog, time.Now)
	if err != nil {
		t.Fatalf("rebuild projection: %v", err)
	}
	assertPipelineStage(t, projections, "book-normalized", KnowledgePipelineStageNormalized, "")
	assertPipelineStage(t, projections, "book-analyzed", KnowledgePipelineStageAnalyzed, "")
	assertPipelineStage(t, projections, "book-candidate", KnowledgePipelineStageCandidate, "")
	assertPipelineStage(t, projections, "book-published", KnowledgePipelineStagePublished, "release-published")
}

func TestKnowledgePipelineKeepsLastPublishedReleaseWhenCurrentArtifactIsStale(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	catalog, err := NewKnowledgeCatalogStore(root, nil)
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}
	savePipelinePackage(t, store, "book-1", "hash-old")
	savePipelineAnalysis(t, store, "book-1", "hash-old")
	savePipelineQuality(t, store, "book-1", "hash-old", BookQualityPass)
	if err := store.saveKnowledgeRelease(KnowledgeRelease{
		SchemaVersion: KnowledgeReleaseSchemaVersion,
		Version:       knowledgeReleaseVersion,
		ReleaseID:     "release-old",
		BookID:        "book-1",
		ContentHash:   "hash-old",
		UsagePolicy:   BookUsageStandard,
		Book:          BookKnowledgeBook{BookID: "book-1", Title: "Book"},
		Analysis:      &BookAnalysisPayload{Summary: "old", Claims: []BookAnalysisClaim{{ID: "claim-1", Statement: "old", CitationIDs: []string{"citation-1"}, Confidence: 0.8, RiskLevel: "low"}}},
		Quality:       BookQualityReport{Decision: BookQualityPass, UsagePolicy: BookUsageStandard},
		Citations:     []BookKnowledgeCitation{{CitationID: "citation-1", BookID: "book-1"}},
		CreatedAt:     "2026-07-14T00:00:00Z",
	}); err != nil {
		t.Fatalf("save release: %v", err)
	}
	savePipelinePackage(t, store, "book-1", "hash-new")
	if err := store.SaveAnalysisManifest(BookAnalysisManifest{
		Version:     bookAnalysisVersion,
		BookID:      "book-1",
		ContentHash: "hash-new",
		Status:      BookAnalysisFailed,
		Error:       "tokenplan_unavailable",
		UpdatedAt:   "2026-07-14T01:00:00Z",
	}); err != nil {
		t.Fatalf("save failed analysis: %v", err)
	}

	projections, err := RebuildKnowledgePipelineProjection(store, catalog, time.Now)
	if err != nil {
		t.Fatalf("rebuild projection: %v", err)
	}
	projection := findPipelineProjection(t, projections, "book-1")
	if projection.Stage != KnowledgePipelineStageNormalized {
		t.Fatalf("stage = %s, want normalized for failed current analysis", projection.Stage)
	}
	if projection.PublicErrorCode != "analysis_failed" {
		t.Fatalf("public error = %q", projection.PublicErrorCode)
	}
	if projection.LastPublishedReleaseID != "release-old" {
		t.Fatalf("last published release = %q", projection.LastPublishedReleaseID)
	}
}

func savePipelinePackage(t *testing.T, store *BookKnowledgeStore, bookID, hash string) {
	t.Helper()
	if err := store.SavePackage(BookKnowledgePackage{Book: BookKnowledgeBook{BookID: bookID, Title: bookID, SourceType: "synthetic", SourceKey: bookID, ContentHash: hash}}); err != nil {
		t.Fatalf("save package %s: %v", bookID, err)
	}
}

func savePipelineAnalysis(t *testing.T, store *BookKnowledgeStore, bookID, hash string) {
	t.Helper()
	if err := store.SaveAnalysisManifest(BookAnalysisManifest{
		Version:     bookAnalysisVersion,
		BookID:      bookID,
		ContentHash: hash,
		Status:      BookAnalysisReady,
		Payload:     &BookAnalysisPayload{Summary: "summary", Claims: []BookAnalysisClaim{{ID: "claim-1", Statement: "claim", CitationIDs: []string{"citation-1"}, Confidence: 0.8, RiskLevel: "low"}}},
		UpdatedAt:   "2026-07-14T00:00:00Z",
	}); err != nil {
		t.Fatalf("save analysis %s: %v", bookID, err)
	}
}

func savePipelineQuality(t *testing.T, store *BookKnowledgeStore, bookID, hash, decision string) {
	t.Helper()
	if err := store.SaveBookQualityReport(BookQualityReport{
		Version:      bookQualityVersion,
		BookID:       bookID,
		ContentHash:  hash,
		AnalysisHash: "analysis-" + hash,
		Decision:     decision,
		UsagePolicy:  BookUsageStandard,
		EvaluatedAt:  "2026-07-14T00:00:00Z",
	}); err != nil {
		t.Fatalf("save quality %s: %v", bookID, err)
	}
}

func assertPipelineStage(t *testing.T, projections []KnowledgePipelineProjection, bookID, stage, releaseID string) {
	t.Helper()
	projection := findPipelineProjection(t, projections, bookID)
	if projection.Stage != stage || projection.LastPublishedReleaseID != releaseID {
		t.Fatalf("projection %s = %#v, want stage=%s release=%s", bookID, projection, stage, releaseID)
	}
}

func findPipelineProjection(t *testing.T, projections []KnowledgePipelineProjection, bookID string) KnowledgePipelineProjection {
	t.Helper()
	for _, projection := range projections {
		if projection.BookID == bookID {
			return projection
		}
	}
	t.Fatalf("missing projection for %s in %#v", bookID, projections)
	return KnowledgePipelineProjection{}
}
