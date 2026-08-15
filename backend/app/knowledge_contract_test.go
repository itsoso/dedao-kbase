package app

import (
	"encoding/json"
	"fmt"
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
	raw := []byte(fmt.Sprintf(`{
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
			"cluster_id":%q,
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
				"publication_identity":"account:sha256-0123456789abcdef",
				"publication_identity_basis":"source_account",
				"independent_publication_eligible":true
			}]
		}],
		"returned_clusters":1,
		"has_more":false
	}`, knowledgeAssemblyHashID("cluster", "assertion")))
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
		"agent-compilation-request-v1.schema.json",
		"agent-compilation-v1.schema.json",
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

func TestAgentCompilationSchemasCarryHardLimits(t *testing.T) {
	requestRaw, err := os.ReadFile(filepath.Join(
		"..",
		"..",
		"contracts",
		"agent-compilation-request-v1.schema.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var requestSchema map[string]any
	if err := json.Unmarshal(requestRaw, &requestSchema); err != nil {
		t.Fatal(err)
	}
	requestProperties := requestSchema["properties"].(map[string]any)
	if got := requestProperties["supporting_release_ids"].(map[string]any)["maxItems"]; got != float64(agentCompilationMaxSupportingReleases) {
		t.Fatalf("supporting_release_ids maxItems = %#v", got)
	}
	if got := requestProperties["primary_release_id"].(map[string]any)["maxLength"]; got != float64(agentCompilationMaxReleaseIDRunes) {
		t.Fatalf("primary_release_id maxLength = %#v", got)
	}
	requestSupportItems := requestProperties["supporting_release_ids"].(map[string]any)["items"].(map[string]any)
	if got := requestSupportItems["maxLength"]; got != float64(agentCompilationMaxReleaseIDRunes) {
		t.Fatalf("supporting release maxLength = %#v", got)
	}
	if got := requestProperties["research_enabled"].(map[string]any)["type"]; got != "boolean" {
		t.Fatalf("research_enabled type = %#v", got)
	}

	responseRaw, err := os.ReadFile(filepath.Join(
		"..",
		"..",
		"contracts",
		"agent-compilation-v1.schema.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var responseSchema map[string]any
	if err := json.Unmarshal(responseRaw, &responseSchema); err != nil {
		t.Fatal(err)
	}
	responseProperties := responseSchema["properties"].(map[string]any)
	if got := responseProperties["candidates"].(map[string]any)["maxItems"]; got != float64(agentCompilationMaxCandidates) {
		t.Fatalf("candidates maxItems = %#v", got)
	}
	if got := responseProperties["release_ids"].(map[string]any)["maxItems"]; got != float64(agentCompilationMaxSupportingReleases+1) {
		t.Fatalf("release_ids maxItems = %#v", got)
	}
	if got := len(responseSchema["allOf"].([]any)); got != 6 {
		t.Fatalf("response mode/status conditional contracts = %d", got)
	}
	defs := responseSchema["$defs"].(map[string]any)
	issueProperties := defs["issue"].(map[string]any)["properties"].(map[string]any)
	if got := issueProperties["message"].(map[string]any)["maxLength"]; got != float64(agentCompilationMaxIssueMessageRunes) {
		t.Fatalf("issue message maxLength = %#v", got)
	}
	candidate := defs["candidate"].(map[string]any)
	candidateProperties := candidate["properties"].(map[string]any)
	if got := len(candidateProperties["package"].(map[string]any)["oneOf"].([]any)); got != 3 {
		t.Fatalf("candidate package schema refs = %d", got)
	}
	if got := len(candidate["allOf"].([]any)); got != 4 {
		t.Fatalf("candidate conditional contracts = %d", got)
	}
	nextActionItems := candidateProperties["next_actions"].(map[string]any)["items"].(map[string]any)
	if got := nextActionItems["maxLength"]; got != float64(agentCompilationMaxNextActionRunes) {
		t.Fatalf("next action maxLength = %#v", got)
	}
}

func TestAgentCompilationSchemasAcceptResearchV4(t *testing.T) {
	request := AgentCompilationRequest{
		SchemaVersion: AgentCompilationRequestSchemaVersion,
		Mode:          AgentCompilationModeStudy, PrimaryReleaseID: "release-research",
		Version: "1.0.0", ResearchEnabled: true,
	}
	validateSchemaInstance(t, "agent-compilation-request-v1.schema.json", request, true)

	v4, err := FinalizeAgentPackage(validAgentPackageV4())
	if err != nil {
		t.Fatal(err)
	}
	compilation := AgentCompilation{
		SchemaVersion: AgentCompilationSchemaVersion, CompilerVersion: AgentCompilerVersion,
		CompilationID: "compilation-research-schema", Mode: AgentCompilationModeStudy,
		AssemblyID: "assembly-research-schema", ReleaseIDs: []string{"release-research"},
		Status: AgentCompilationStatusReady,
		Candidates: []AgentCompilationCandidate{{
			Kind: AgentCompilationCandidateStudy, Status: AgentCompilationCandidateReady,
			Package: &v4, NextActions: []string{AgentCompilationNextActionEvaluate},
		}},
	}
	validateSchemaInstance(t, "agent-compilation-v1.schema.json", compilation, true)
	compilation.Mode = AgentCompilationModeEvidence
	compilation.Candidates[0].Kind = AgentCompilationCandidateEvidence
	validateSchemaInstance(t, "agent-compilation-v1.schema.json", compilation, true)

	withoutPolicy := v4
	withoutPolicy.ResearchPolicy = nil
	compilation.Candidates[0].Package = &withoutPolicy
	validateSchemaInstance(t, "agent-compilation-v1.schema.json", compilation, false)
}

func TestAgentPackageV3AndV4PublicSchemasPreserveVersionBoundaries(t *testing.T) {
	v3 := validAgentPackage()
	v3.SchemaVersion = AgentPackageSchemaVersionV3
	v3.Releases = nil
	v3.CollectionReleases = []AgentPackageCollectionRef{{
		ReleaseID:   "collection-release-fixture",
		ContentHash: "sha256:" + strings.Repeat("a", 64),
	}}
	v3, err := FinalizeAgentPackage(v3)
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaInstance(t, "agent-package-v3.schema.json", v3, true)
	v3WithTwoCollections := v3
	v3WithTwoCollections.CollectionReleases = append(
		append([]AgentPackageCollectionRef(nil), v3.CollectionReleases...),
		AgentPackageCollectionRef{ReleaseID: "collection-release-second", ContentHash: "sha256:" + strings.Repeat("b", 64)},
	)
	validateSchemaInstance(t, "agent-package-v3.schema.json", v3WithTwoCollections, false)

	v4, err := FinalizeAgentPackage(validAgentPackageV4())
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaInstance(t, "agent-package-v4.schema.json", v4, true)

	v3WithResearch := v3
	v3WithResearch.ResearchPolicy = v4.ResearchPolicy
	validateSchemaInstance(t, "agent-package-v3.schema.json", v3WithResearch, false)
	v4WithCollection := v4
	v4WithCollection.CollectionReleases = v3.CollectionReleases
	validateSchemaInstance(t, "agent-package-v4.schema.json", v4WithCollection, false)
}

func TestAgentCompilationSchemaRequiresPackageVersionForCandidateKind(t *testing.T) {
	v1, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	v2, err := FinalizeAgentPackage(validAgentPackageV2())
	if err != nil {
		t.Fatal(err)
	}
	compilation := AgentCompilation{
		SchemaVersion:   AgentCompilationSchemaVersion,
		CompilerVersion: AgentCompilerVersion,
		CompilationID:   "compilation-schema-fixture",
		Mode:            AgentCompilationModeStudy,
		AssemblyID:      "assembly-schema-fixture",
		ReleaseIDs:      []string{"release-schema-fixture"},
		Status:          AgentCompilationStatusReady,
		Candidates: []AgentCompilationCandidate{{
			Kind:        AgentCompilationCandidateStudy,
			Status:      AgentCompilationCandidateReady,
			Package:     &v1,
			NextActions: []string{AgentCompilationNextActionEvaluate},
		}},
	}
	validateSchemaInstance(t, "agent-compilation-v1.schema.json", compilation, true)

	compilation.Candidates[0].Package = &v2
	validateSchemaInstance(t, "agent-compilation-v1.schema.json", compilation, false)

	compilation.Mode = AgentCompilationModeEvidence
	compilation.Candidates[0].Kind = AgentCompilationCandidateEvidence
	compilation.Candidates[0].Package = &v1
	validateSchemaInstance(t, "agent-compilation-v1.schema.json", compilation, false)

	compilation.Mode = AgentCompilationModeStudy
	compilation.Candidates[0].Kind = AgentCompilationCandidateStudy
	compilation.Candidates[0].Package = &v1
	compilation.Status = AgentCompilationStatusBlocked
	validateSchemaInstance(t, "agent-compilation-v1.schema.json", compilation, false)

	compilation.Status = AgentCompilationStatusReady
	compilation.Mode = AgentCompilationModeDual
	validateSchemaInstance(t, "agent-compilation-v1.schema.json", compilation, false)
}

func TestKnowledgeReleaseAssemblySchemaCarriesHardLimits(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(
		"..",
		"..",
		"contracts",
		"knowledge-release-assembly-v1.schema.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	cluster := defs["cluster"].(map[string]any)
	clusterProperties := cluster["properties"].(map[string]any)
	claim := defs["claim"].(map[string]any)
	claimProperties := claim["properties"].(map[string]any)
	tests := []struct {
		name string
		got  any
		want float64
	}{
		{
			name: "claims maxItems",
			got:  clusterProperties["claims"].(map[string]any)["maxItems"],
			want: knowledgeAssemblyMaxClaimsPerCluster,
		},
		{
			name: "normalized assertion maxLength",
			got:  clusterProperties["normalized_assertion"].(map[string]any)["maxLength"],
			want: knowledgeAssemblyMaxStatementRunes,
		},
		{
			name: "potential conflicts maxItems",
			got:  clusterProperties["potential_conflicts"].(map[string]any)["maxItems"],
			want: knowledgeAssemblyMaxConflictsPerCluster,
		},
		{
			name: "statement maxLength",
			got:  claimProperties["statement"].(map[string]any)["maxLength"],
			want: knowledgeAssemblyMaxStatementRunes,
		},
		{
			name: "citation ids maxItems",
			got:  claimProperties["citation_ids"].(map[string]any)["maxItems"],
			want: knowledgeAssemblyMaxCitationIDsPerClaim,
		},
	}
	for _, testCase := range tests {
		if testCase.got != testCase.want {
			t.Errorf("%s = %v, want %v", testCase.name, testCase.got, testCase.want)
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
