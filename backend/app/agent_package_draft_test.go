package app

import "testing"

func TestBuildControlledAgentPackageDraftPinsReleaseAndPassesContract(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	knownTools := []string{
		"book-mcp/agent.search",
		"book-mcp/agent.resolve_citation",
		"book-mcp/agent.get_claim",
		"book-mcp/agent.package_metadata",
	}
	draft, err := BuildControlledAgentPackageDraft(store, ControlledAgentDraftRequest{
		ReleaseID:           "release-1",
		PackageID:           "attention-research-assistant",
		Version:             "1.0.0",
		PreferredCapability: "reasoning",
		MaxContextChunks:    12,
		MaxCostUSD:          0.2,
		TimeoutMS:           30000,
	}, knownTools)
	if err != nil {
		t.Fatalf("BuildControlledAgentPackageDraft returned error: %v", err)
	}
	if draft.Package.PackageID != "attention-research-assistant" || draft.Package.LifecycleState != AgentPackageDraft {
		t.Fatalf("package identity = %#v", draft.Package)
	}
	if len(draft.Package.Releases) != 1 || draft.Package.Releases[0].ReleaseID != "release-1" || len(draft.Package.Releases[0].CitationIDs) == 0 {
		t.Fatalf("release pin = %#v", draft.Package.Releases)
	}
	if draft.Package.RetrievalPolicy.Strategy != "lexical" || !draft.Package.RetrievalPolicy.RequireCitations {
		t.Fatalf("retrieval policy = %#v", draft.Package.RetrievalPolicy)
	}
	wantCapabilities := []string{"reader", "search", "grounded_chat", "evidence"}
	if !equalStringSlices(draft.Package.UIManifest.Capabilities, wantCapabilities) {
		t.Fatalf("capabilities = %#v", draft.Package.UIManifest.Capabilities)
	}
	if err := ValidateAgentPackage(draft.Package, store, knownTools); err != nil {
		t.Fatalf("draft contract validation failed: %v", err)
	}
	if len(draft.Suite.Cases) != 10 || draft.Suite.SuiteVersion != draft.Package.EvaluationPolicy.SuiteVersion {
		t.Fatalf("evaluation suite = %#v", draft.Suite)
	}
	report, err := EvaluateAgentPackageDeterministically(store, draft.Package, draft.Suite, testAgentPackageTime())
	if err != nil {
		t.Fatalf("evaluate generated draft: %v", err)
	}
	if !report.Passed {
		t.Fatalf("generated draft evaluation failed: %#v", report)
	}
}

func TestBuildControlledAgentPackageDraftRejectsUnpublishedRelease(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	_, err := BuildControlledAgentPackageDraft(store, ControlledAgentDraftRequest{
		ReleaseID: "missing", PackageID: "missing-agent", Version: "1.0.0",
	}, []string{"book-mcp/agent.search"})
	if err == nil {
		t.Fatal("missing release was accepted")
	}
}

func TestBuildControlledAgentPackageDraftResolvesChapterCitation(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentPackageTestRelease()
	release.Analysis.Claims[0].CitationIDs = []string{"chapter-1"}
	release.Citations[0].ChapterID = "chapter-1"
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}

	draft, err := BuildControlledAgentPackageDraft(store, ControlledAgentDraftRequest{
		ReleaseID: "release-1", PackageID: "chapter-citation-agent", Version: "1.0.0",
	}, AgentReadOnlyToolIDs())
	if err != nil {
		t.Fatalf("BuildControlledAgentPackageDraft returned error: %v", err)
	}
	if got := draft.Suite.Cases[0].ExpectedIDs; len(got) != 1 || got[0] != "chunk-1" {
		t.Fatalf("resolved retrieval citation = %#v", got)
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
