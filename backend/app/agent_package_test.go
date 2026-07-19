package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentPackageValidatesPinnedReleasePoliciesAndCapabilities(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg := validAgentPackage()

	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatalf("FinalizeAgentPackage() error = %v", err)
	}
	if !strings.HasPrefix(finalized.ContentHash, "sha256:") {
		t.Fatalf("content hash = %q", finalized.ContentHash)
	}
	if err := ValidateAgentPackage(finalized, store, AgentReadOnlyToolIDs()); err != nil {
		t.Fatalf("ValidateAgentPackage() error = %v", err)
	}

	reordered := pkg
	reordered.RetrievalPolicy.AllowedSourceTypes = []string{"wechat_mp_article", "dedao_ebook"}
	reordered.ModelPolicy.Fallbacks = []string{"qwen-max", "qwen-plus"}
	reordered.SafetyPolicy.AbstentionReasons = []string{"outside_scope", "insufficient_evidence"}
	reordered.UIManifest.Capabilities = []string{"evidence", "reader", "grounded_chat", "search"}
	reordered.ToolPolicy.Tools = []AgentPackageToolRule{
		{MCPServer: "book-mcp", ToolName: "agent.resolve_citation", Decision: AgentToolAllow},
		{MCPServer: "book-mcp", ToolName: "agent.search", Decision: AgentToolAllow},
	}
	reorderedFinalized, err := FinalizeAgentPackage(reordered)
	if err != nil {
		t.Fatalf("FinalizeAgentPackage(reordered) error = %v", err)
	}
	if reorderedFinalized.ContentHash != finalized.ContentHash {
		t.Fatalf("hash changed after reordering set-like policies: %q != %q", reorderedFinalized.ContentHash, finalized.ContentHash)
	}
}

func TestAgentPackageRejectsInvalidOrMutableReferences(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	knownTools := AgentReadOnlyToolIDs()

	tests := []struct {
		name string
		edit func(*AgentPackage)
		want string
	}{
		{name: "missing package identity", edit: func(pkg *AgentPackage) { pkg.PackageID = "" }, want: "package_id"},
		{name: "missing pinned release", edit: func(pkg *AgentPackage) { pkg.Releases = nil }, want: "releases"},
		{name: "mutable release reference", edit: func(pkg *AgentPackage) { pkg.Releases[0].ContentHash = "" }, want: "content_hash"},
		{name: "unpublished release", edit: func(pkg *AgentPackage) { pkg.Releases[0].ReleaseID = "release-missing" }, want: "published release"},
		{name: "release hash mismatch", edit: func(pkg *AgentPackage) { pkg.Releases[0].ContentHash = "sha256:changed" }, want: "content hash"},
		{name: "missing citation", edit: func(pkg *AgentPackage) { pkg.Releases[0].CitationIDs = []string{"citation-missing"} }, want: "citation"},
		{name: "missing retrieval policy", edit: func(pkg *AgentPackage) { pkg.RetrievalPolicy.Strategy = "" }, want: "retrieval_policy.strategy"},
		{name: "missing model policy", edit: func(pkg *AgentPackage) { pkg.ModelPolicy.PreferredCapability = "" }, want: "model_policy.preferred_capability"},
		{name: "unknown tool", edit: func(pkg *AgentPackage) { pkg.ToolPolicy.Tools[0].ToolName = "delete_source" }, want: "unknown tool"},
		{name: "missing safety policy", edit: func(pkg *AgentPackage) { pkg.SafetyPolicy.AbstentionReasons = nil }, want: "abstention"},
		{name: "missing evaluation threshold", edit: func(pkg *AgentPackage) { pkg.EvaluationPolicy.MinimumScores = nil }, want: "minimum_scores"},
		{name: "invalid evaluation threshold", edit: func(pkg *AgentPackage) { pkg.EvaluationPolicy.MinimumScores["faithfulness"] = 1.1 }, want: "between 0 and 1"},
		{name: "unknown ui capability", edit: func(pkg *AgentPackage) {
			pkg.UIManifest.Capabilities = append(pkg.UIManifest.Capabilities, "private_fork")
		}, want: "ui capability"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := validAgentPackage()
			tt.edit(&pkg)
			finalized, err := FinalizeAgentPackage(pkg)
			if err != nil {
				if !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("FinalizeAgentPackage() error = %v, want %q", err, tt.want)
				}
				return
			}
			err = ValidateAgentPackage(finalized, store, knownTools)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateAgentPackage() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAgentPackageRejectsUsagePolicyDowngrade(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentPackageTestRelease()
	release.UsagePolicy = BookUsageEvidenceOnly
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(pkg, store, AgentReadOnlyToolIDs()); err == nil || !strings.Contains(err.Error(), "usage policy") {
		t.Fatalf("usage policy downgrade error = %v", err)
	}
}

func TestAgentPackageRejectsMissingSourceIdentity(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentPackageTestRelease()
	release.Book.SourceType = ""
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(pkg, store, AgentReadOnlyToolIDs()); err == nil || !strings.Contains(err.Error(), "source type is required") {
		t.Fatalf("missing source identity error = %v", err)
	}
}

func TestAgentPackageRejectsNonURLSafePackageID(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg := validAgentPackage()
	pkg.PackageID = "agent/package"
	pkg, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(pkg, store, AgentReadOnlyToolIDs()); err == nil || !strings.Contains(err.Error(), "package_id") {
		t.Fatalf("unsafe package_id error = %v", err)
	}
}

func TestAgentPackageHashDetectsPolicyChangesAndIgnoresLifecycleTimestamps(t *testing.T) {
	pkg := validAgentPackage()
	first, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	pkg.CreatedAt = "2026-07-20T00:00:00Z"
	pkg.PublishedAt = "2026-07-21T00:00:00Z"
	second, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash != second.ContentHash {
		t.Fatalf("operational timestamps changed content hash: %q != %q", first.ContentHash, second.ContentHash)
	}
	pkg.EvaluationPolicy.MinimumScores["faithfulness"] = 0.95
	third, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if third.ContentHash == first.ContentHash {
		t.Fatal("evaluation policy change did not change content hash")
	}
}

func TestAgentPackageSchemaIsValidJSONAndRequiresContractSections(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "agent-package-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("schema required is missing")
	}
	joined := strings.Join(anyStrings(required), ",")
	for _, field := range []string{"schema_version", "package_id", "version", "content_hash", "lifecycle_state", "releases", "retrieval_policy", "model_policy", "tool_policy", "safety_policy", "evaluation_policy", "ui_manifest"} {
		if !strings.Contains(joined, field) {
			t.Fatalf("schema does not require %q: %s", field, joined)
		}
	}
}

func validAgentPackage() AgentPackage {
	return AgentPackage{
		SchemaVersion:  AgentPackageSchemaVersion,
		PackageID:      "agent-package-example",
		Version:        "1.0.0",
		LifecycleState: AgentPackageDraft,
		Releases: []AgentPackageReleaseRef{{
			ReleaseID:   "release-1",
			ContentHash: "sha256:release-content",
			CitationIDs: []string{"citation-1"},
		}},
		RetrievalPolicy: AgentPackageRetrievalPolicy{
			Strategy:           "hybrid",
			AllowedSourceTypes: []string{"dedao_ebook", "wechat_mp_article"},
			RequireCitations:   true,
			MaxContextChunks:   8,
		},
		ModelPolicy: AgentPackageModelPolicy{
			PreferredCapability: "reasoning",
			Fallbacks:           []string{"qwen-plus", "qwen-max"},
			MaxCostUSD:          0.25,
			TimeoutMS:           30000,
		},
		PromptProfiles: []AgentPackagePromptProfile{{
			ProfileID:    "grounded-answer.v1",
			OutputSchema: "grounded-answer.v1",
		}},
		ToolPolicy: AgentPackageToolPolicy{Tools: []AgentPackageToolRule{
			{MCPServer: "book-mcp", ToolName: "agent.search", Decision: AgentToolAllow},
			{MCPServer: "book-mcp", ToolName: "agent.resolve_citation", Decision: AgentToolAllow},
		}},
		SafetyPolicy: AgentPackageSafetyPolicy{
			UsagePolicy:       BookUsageStandard,
			AbstentionReasons: []string{"insufficient_evidence", "outside_scope"},
			EscalationTarget:  "human_review",
		},
		EvaluationPolicy: AgentPackageEvaluationPolicy{
			SuiteVersion: "book-agent-v1",
			MinimumScores: map[string]float64{
				"retrieval":      0.8,
				"citations":      1,
				"faithfulness":   0.9,
				"abstention":     1,
				"tool_choice":    1,
				"tool_arguments": 1,
			},
		},
		UIManifest: AgentPackageUIManifest{Capabilities: []string{"reader", "search", "grounded_chat", "evidence"}},
	}
}

func saveAgentPackageTestRelease(t *testing.T, store *BookKnowledgeStore) {
	t.Helper()
	if err := store.saveKnowledgeRelease(agentPackageTestRelease()); err != nil {
		t.Fatal(err)
	}
}

func agentPackageTestRelease() KnowledgeRelease {
	return KnowledgeRelease{
		SchemaVersion: KnowledgeReleaseSchemaVersion,
		Version:       "1",
		ReleaseID:     "release-1",
		BookID:        "book-1",
		ContentHash:   "sha256:release-content",
		UsagePolicy:   BookUsageStandard,
		Book:          BookKnowledgeBook{BookID: "book-1", Title: "Synthetic Book", SourceType: "dedao_ebook"},
		Analysis: &BookAnalysisPayload{
			Summary: "Synthetic summary",
			Claims: []BookAnalysisClaim{{
				ID: "claim-1", Statement: "Synthetic grounded statement", CitationIDs: []string{"citation-1"}, Confidence: 1, RiskLevel: "low",
			}},
		},
		Quality:   BookQualityReport{Decision: BookQualityPass},
		Citations: []BookKnowledgeCitation{{CitationID: "citation-1", BookID: "book-1", ChunkID: "chunk-1"}},
		CreatedAt: "2026-07-19T00:00:00Z",
	}
}

func anyStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
