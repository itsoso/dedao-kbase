package app

import (
	"strings"
	"testing"
)

func TestAgentPackageCollectionScopePinsOneImmutableRelease(t *testing.T) {
	store, release := saveAgentCollectionReleaseFixture(t)
	pkg := validCollectionAgentPackage(release)
	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(finalized, store, AgentReadOnlyToolIDs()); err != nil {
		t.Fatalf("valid collection package: %v", err)
	}
	if len(finalized.Releases) != 0 || len(finalized.CollectionReleases) != 1 || finalized.CollectionReleases[0].ContentHash != release.ContentHash {
		t.Fatalf("collection pins = %#v regular=%#v", finalized.CollectionReleases, finalized.Releases)
	}

	v1 := validAgentPackage()
	v1.CollectionReleases = []AgentPackageCollectionRef{{ReleaseID: release.ReleaseID, ContentHash: release.ContentHash}}
	v1, err = FinalizeAgentPackage(v1)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(v1, store, AgentReadOnlyToolIDs()); err == nil || !strings.Contains(err.Error(), "collection_releases") {
		t.Fatalf("v1 collection scope error=%v", err)
	}
}

func TestAgentPackageCollectionScopeRejectsMutableMissingAndForeignReferences(t *testing.T) {
	store, release := saveAgentCollectionReleaseFixture(t)
	tests := []struct {
		name string
		edit func(*AgentPackage)
		want string
	}{
		{name: "missing collection", edit: func(pkg *AgentPackage) { pkg.CollectionReleases = nil }, want: "exactly one"},
		{name: "multiple collections", edit: func(pkg *AgentPackage) {
			pkg.CollectionReleases = append(pkg.CollectionReleases, pkg.CollectionReleases[0])
		}, want: "exactly one"},
		{name: "regular release", edit: func(pkg *AgentPackage) { pkg.Releases = []AgentPackageReleaseRef{{ReleaseID: "release-1"}} }, want: "releases must be empty"},
		{name: "missing release", edit: func(pkg *AgentPackage) {
			pkg.CollectionReleases[0].ReleaseID = "collection-release-000000000000000000000000"
		}, want: "not found"},
		{name: "mutable hash", edit: func(pkg *AgentPackage) { pkg.CollectionReleases[0].ContentHash = "" }, want: "content_hash"},
		{name: "hash mismatch", edit: func(pkg *AgentPackage) { pkg.CollectionReleases[0].ContentHash = "sha256:changed" }, want: "content hash"},
		{name: "foreign source", edit: func(pkg *AgentPackage) { pkg.RetrievalPolicy.AllowedSourceTypes = []string{"dedao_ebook"} }, want: "source type"},
		{name: "unsafe policy", edit: func(pkg *AgentPackage) { pkg.SafetyPolicy.UsagePolicy = BookUsageStandard }, want: "evidence_only"},
		{name: "write tool", edit: func(pkg *AgentPackage) {
			pkg.ToolPolicy.Tools = append(pkg.ToolPolicy.Tools, AgentPackageToolRule{MCPServer: "book-mcp", ToolName: "delete", Decision: AgentToolAllow})
		}, want: "read-only"},
	}
	knownTools := append(AgentReadOnlyToolIDs(), "book-mcp/delete")
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			pkg := validCollectionAgentPackage(release)
			testCase.edit(&pkg)
			finalized, err := FinalizeAgentPackage(pkg)
			if err == nil {
				err = ValidateAgentPackage(finalized, store, knownTools)
			}
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error=%v want=%q", err, testCase.want)
			}
		})
	}
}

func TestBuildControlledCollectionAgentDraftIsDeterministicAndReadOnly(t *testing.T) {
	store, release := saveAgentCollectionReleaseFixture(t)
	first, err := BuildControlledCollectionAgentDraft(store, ControlledCollectionAgentDraftRequest{
		CollectionReleaseID: release.ReleaseID,
	}, AgentReadOnlyToolIDs())
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildControlledCollectionAgentDraft(store, ControlledCollectionAgentDraftRequest{
		CollectionReleaseID: release.ReleaseID,
	}, AgentReadOnlyToolIDs())
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash == "" || first.ContentHash != second.ContentHash || first.PackageID != "wechat-account-fixture-agent" || first.Version != "1.0.0" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if first.SchemaVersion != AgentPackageSchemaVersionV3 || first.SafetyPolicy.UsagePolicy != BookUsageEvidenceOnly || len(first.CollectionReleases) != 1 {
		t.Fatalf("draft=%#v", first)
	}
	if first.ModelPolicy.MaxCostUSD != 3.0 {
		t.Fatalf("default collection evaluation budget = %v, want 3.0", first.ModelPolicy.MaxCostUSD)
	}
	for _, rule := range first.ToolPolicy.Tools {
		if rule.Decision != AgentToolAllow || !stringSet(AgentReadOnlyToolIDs())[rule.MCPServer+"/"+rule.ToolName] {
			t.Fatalf("non-read-only tool=%#v", rule)
		}
	}
	if err := ValidateAgentPackage(*first, store, AgentReadOnlyToolIDs()); err != nil {
		t.Fatalf("draft validation: %v", err)
	}
}

func TestAgentPackageCollectionRuntimeDescriptorUsesV3Schema(t *testing.T) {
	_, release := saveAgentCollectionReleaseFixture(t)
	pkg, err := FinalizeAgentPackage(validCollectionAgentPackage(release))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := newAgentPackageRuntimeDescriptor(pkg)
	if err != nil {
		t.Fatal(err)
	}
	record := AgentPackageRecord{
		PackageID: pkg.PackageID, Version: pkg.Version, ContentHash: pkg.ContentHash,
		Runtime: &descriptor,
	}
	if descriptor.SchemaVersion != AgentPackageSchemaVersionV3 {
		t.Fatalf("descriptor=%#v", descriptor)
	}
	if err := validateAgentPackageRuntimeDescriptor(record, &pkg); err != nil {
		t.Fatalf("validate descriptor: %v", err)
	}
}

func saveAgentCollectionReleaseFixture(t *testing.T) (*BookKnowledgeStore, *KnowledgeCollectionRelease) {
	t.Helper()
	store := NewBookKnowledgeStore(t.TempDir())
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	saveCollectionArticleFixture(t, store, "book-a", "article-a", "Fixture account")
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-a", "article-a")
	if _, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture"); err != nil {
		t.Fatal(err)
	}
	release, err := store.PublishKnowledgeCollection("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	return store, release
}

func validCollectionAgentPackage(release *KnowledgeCollectionRelease) AgentPackage {
	return AgentPackage{
		SchemaVersion: AgentPackageSchemaVersionV3, PackageID: "wechat-account-fixture-agent", Version: "1.0.0", LifecycleState: AgentPackageDraft,
		CollectionReleases: []AgentPackageCollectionRef{{ReleaseID: release.ReleaseID, ContentHash: release.ContentHash}},
		RetrievalPolicy:    AgentPackageRetrievalPolicy{Strategy: "lexical", AllowedSourceTypes: []string{"wechat_mp_article"}, RequireCitations: true, MaxContextChunks: 12},
		ModelPolicy:        AgentPackageModelPolicy{PreferredCapability: "reasoning", Fallbacks: []string{"qwen3.7-max"}, MaxCostUSD: 0.25, TimeoutMS: 30000},
		PromptProfiles:     []AgentPackagePromptProfile{{ProfileID: "grounded-answer.v1", OutputSchema: "grounded-answer.v1"}},
		ToolPolicy:         AgentPackageToolPolicy{Tools: controlledAgentReadOnlyTools(AgentReadOnlyToolIDs())},
		SafetyPolicy:       AgentPackageSafetyPolicy{UsagePolicy: BookUsageEvidenceOnly, AbstentionReasons: []string{"insufficient_evidence", "outside_scope"}, EscalationTarget: "human_review"},
		EvaluationPolicy: AgentPackageEvaluationPolicy{SuiteVersion: "controlled-collection-agent-v1", MinimumScores: map[string]float64{
			"retrieval": 1, "retrieval_precision": 1, "citations": 1, "faithfulness": 1, "abstention": 1,
			"tool_choice": 1, "tool_arguments": 1, "task_completion": 1, "latency": 1, "cost": 1,
		}},
		UIManifest: AgentPackageUIManifest{Capabilities: []string{"reader", "search", "grounded_chat", "evidence"}},
	}
}
