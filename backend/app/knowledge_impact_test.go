package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestKnowledgeImpactAggregatesReleasesReceiptsAndPipeline(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	saveFeedRelease(t, store, KnowledgeRelease{ReleaseID: "release-a", BookID: "book-a", ContentHash: "hash-a", UsagePolicy: BookUsageEvidenceOnly, CreatedAt: "2026-07-14T00:00:00Z"})
	catalog, err := NewKnowledgeCatalogStore(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.SaveDeliveryReceipt(DeliveryReceipt{SchemaVersion: DeliveryReceiptSchemaVersion, Consumer: "health-consumer", ReleaseID: "release-a", IdempotencyKey: "health:release-a:1", Disposition: "imported"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReplaceKnowledgePipelineProjections([]KnowledgePipelineProjection{{BookID: "book-a", ContentHash: "hash-a", Stage: KnowledgePipelineStagePublished, InputFingerprint: "hash-a", UpdatedAt: "2026-07-14T00:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	resp := requestKBase(handler, http.MethodGet, "/api/knowledge/impact", "secret-token")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"published_releases":1`) || !strings.Contains(resp.Body.String(), `"imported":1`) || !strings.Contains(resp.Body.String(), `"published":1`) {
		t.Fatalf("impact status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestKnowledgeGapsReturnsAggregatesWithoutRawQueries(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	catalog, err := NewKnowledgeCatalogStore(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.RecordKnowledgeGap(KnowledgeGapInput{Consumer: "health-consumer", Domain: "health", Fingerprint: "gap-hash-1", Kind: "zero_hit"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.RecordKnowledgeGap(KnowledgeGapInput{Consumer: "health-consumer", Domain: "health", Fingerprint: "gap-hash-1", Kind: "zero_hit"}); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	resp := requestKBase(handler, http.MethodGet, "/api/knowledge/gaps", "secret-token")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"fingerprint":"gap-hash-1"`) || !strings.Contains(resp.Body.String(), `"count":2`) {
		t.Fatalf("gaps status=%d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "raw_query") {
		t.Fatalf("gaps leaked raw query field: %s", resp.Body.String())
	}
}
