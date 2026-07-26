package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeContractReleaseFixtureRoundTrip(t *testing.T) {
	raw := readContractFixture(t, "release-minimal.json")
	if err := ValidateKnowledgeReleaseContract(raw); err != nil {
		t.Fatalf("ValidateKnowledgeReleaseContract() error = %v", err)
	}
	var release KnowledgeRelease
	if err := json.Unmarshal(raw, &release); err != nil {
		t.Fatal(err)
	}
	if release.SchemaVersion != KnowledgeReleaseSchemaVersion || release.ReleaseID == "" || release.ContentHash == "" {
		t.Fatalf("release did not round-trip required identity fields: %#v", release)
	}
	if release.UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("usage policy = %q", release.UsagePolicy)
	}
	if len(release.Citations) != 1 || release.Citations[0].CitationID == "" {
		t.Fatalf("citations were not preserved: %#v", release.Citations)
	}

	var withUnknown map[string]any
	if err := json.Unmarshal(raw, &withUnknown); err != nil {
		t.Fatal(err)
	}
	withUnknown["new_optional_field"] = map[string]any{"ok": true}
	unknownRaw, err := json.Marshal(withUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateKnowledgeReleaseContract(unknownRaw); err != nil {
		t.Fatalf("unknown optional field should be accepted: %v", err)
	}

	delete(withUnknown, "release_id")
	missingRaw, err := json.Marshal(withUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateKnowledgeReleaseContract(missingRaw); err == nil || !strings.Contains(err.Error(), "release_id") {
		t.Fatalf("missing release_id error = %v", err)
	}
}

func TestKnowledgeContractFeedAndReceiptRoundTrip(t *testing.T) {
	feedRaw := readContractFixture(t, "feed-page.json")
	if err := ValidateKnowledgeFeedContract(feedRaw); err != nil {
		t.Fatalf("ValidateKnowledgeFeedContract() error = %v", err)
	}
	var feed KnowledgeFeedPage
	if err := json.Unmarshal(feedRaw, &feed); err != nil {
		t.Fatal(err)
	}
	if feed.SchemaVersion != KnowledgeFeedSchemaVersion || feed.NextCursor == "" || len(feed.Items) != 1 {
		t.Fatalf("feed did not round-trip: %#v", feed)
	}
	if feed.Items[0].URL != "/api/knowledge/releases/release-fixture-1" {
		t.Fatalf("feed release URL changed: %#v", feed.Items[0])
	}

	receiptRaw := readContractFixture(t, "delivery-receipt.json")
	if err := ValidateDeliveryReceiptContract(receiptRaw); err != nil {
		t.Fatalf("ValidateDeliveryReceiptContract() error = %v", err)
	}
	var receipt DeliveryReceipt
	if err := json.Unmarshal(receiptRaw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.SchemaVersion != DeliveryReceiptSchemaVersion || receipt.IdempotencyKey == "" || receipt.Consumer != "health-consumer" {
		t.Fatalf("receipt did not round-trip: %#v", receipt)
	}
}

func TestKnowledgeContractHealthEvidenceRoundTrip(t *testing.T) {
	raw := readContractFixture(t, "health-evidence-package.json")
	if err := ValidateHealthEvidenceContract(raw); err != nil {
		t.Fatalf("ValidateHealthEvidenceContract() error = %v", err)
	}
	var pkg HealthEvidencePackage
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.SchemaVersion != HealthEvidenceSchemaVersion || pkg.ReleaseID == "" || pkg.UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("health evidence did not round-trip identity fields: %#v", pkg)
	}
	if len(pkg.Evidence) != 1 || pkg.Evidence[0].ClaimID == "" || len(pkg.Evidence[0].Citations) != 1 {
		t.Fatalf("health evidence did not preserve claim citations: %#v", pkg.Evidence)
	}
}

func TestKnowledgeReadinessContractRoundTrip(t *testing.T) {
	raw := []byte(`{
		"schema_version":"knowledge_readiness.v1",
		"summary":{
			"total":1,
			"ready":0,
			"needs_analysis":1,
			"needs_quality":0,
			"ready_to_publish":0,
			"published":0,
			"blocked":0,
			"analysis_claims":0,
			"claims_with_evidence":0,
			"claims_with_explicit_citation":0,
			"evidence_references":0,
			"resolved_references":0,
			"claim_coverage":0,
			"resolution_rate":0,
			"explicit_citation_coverage":0
		},
		"items":[{
			"book_id":"book-1",
			"title":"Book",
			"publication":{"key":"book:book-1","basis":"book_fallback","independent_source_eligible":false},
			"stage":"normalized",
			"next_action":"needs_analysis",
			"analysis_claims":0,
			"claims_with_evidence":0,
			"claims_with_explicit_citation":0,
			"evidence_references":0,
			"resolved_references":0,
			"explicit_citation_references":0,
			"legacy_direct_chunk_references":0,
			"claim_coverage":0,
			"resolution_rate":0,
			"explicit_citation_coverage":0,
			"blocker_codes":[],
			"warning_codes":["publication_identity_not_independent"]
		}]
	}`)
	if err := ValidateKnowledgeReadinessContract(raw); err != nil {
		t.Fatalf("ValidateKnowledgeReadinessContract() error = %v", err)
	}
	var missing map[string]any
	if err := json.Unmarshal(raw, &missing); err != nil {
		t.Fatal(err)
	}
	delete(missing, "summary")
	invalid, err := json.Marshal(missing)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateKnowledgeReadinessContract(invalid); err == nil || !strings.Contains(err.Error(), "summary") {
		t.Fatalf("missing summary error = %v", err)
	}
}

func TestKnowledgeReleaseAssemblyContractRoundTrip(t *testing.T) {
	raw := []byte(`{
		"schema_version":"knowledge_release_assembly.v1",
		"algorithm_version":"deterministic-claim-assembly.v1",
		"assembly_id":"assembly-fixture",
		"release_ids":["release-fixture"],
		"summary":{
			"release_count":1,
			"claim_count":1,
			"cluster_count":1,
			"matched_cluster_count":1,
			"corroborated_clusters":0,
			"potential_conflict_clusters":0,
			"single_publication_clusters":1,
			"insufficient_identity_clusters":0
		},
		"clusters":[{
			"cluster_id":"cluster-fixture",
			"normalized_assertion":"assertion",
			"status":"single_publication",
			"publication_count":1,
			"independent_publication_count":1,
			"claims":[{
				"release_id":"release-fixture",
				"book_id":"book-fixture",
				"claim_id":"claim-fixture",
				"statement":"Assertion",
				"polarity":"positive",
				"citation_ids":["citation-fixture"],
				"publication_identity":"account:sha256-fixture",
				"publication_identity_basis":"source_account",
				"independent_publication_eligible":true
			}]
		}],
		"returned_clusters":1,
		"has_more":false
	}`)
	if err := ValidateKnowledgeReleaseAssemblyContract(raw); err != nil {
		t.Fatalf("ValidateKnowledgeReleaseAssemblyContract() error = %v", err)
	}
	var missing map[string]any
	if err := json.Unmarshal(raw, &missing); err != nil {
		t.Fatal(err)
	}
	delete(missing, "assembly_id")
	invalid, err := json.Marshal(missing)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateKnowledgeReleaseAssemblyContract(invalid); err == nil ||
		!strings.Contains(err.Error(), "assembly_id") {
		t.Fatalf("missing assembly_id error = %v", err)
	}
}

func TestKnowledgeContractSchemaFilesArePresent(t *testing.T) {
	for _, name := range []string{
		"knowledge-release-v1.schema.json",
		"knowledge-feed-v1.schema.json",
		"knowledge-readiness-v1.schema.json",
		"knowledge-release-assembly-v1.schema.json",
		"delivery-receipt-v1.schema.json",
		"health-evidence-v1.schema.json",
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", name))
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		required, ok := schema["required"].([]any)
		if !ok || len(required) == 0 {
			t.Fatalf("%s missing required fields", name)
		}
	}
}

func readContractFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
