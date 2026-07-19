package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBookKnowledgeMCPAdvertisesOnlyScopedReadOnlyAgentTools(t *testing.T) {
	server := NewBookKnowledgeMCPServer(NewBookKnowledgeStore(t.TempDir()))
	tools := server.Tools()
	if len(tools) != 4 {
		t.Fatalf("tools = %#v", tools)
	}
	for _, tool := range tools {
		if !strings.HasPrefix(tool.Name, "agent.") {
			t.Fatalf("unscoped tool is still advertised: %#v", tool)
		}
		required, ok := tool.InputSchema["required"].([]string)
		if !ok || !containsMCPString(required, "package_id") || !containsMCPString(required, "package_version") || !containsMCPString(required, "release_id") {
			t.Fatalf("tool %q does not require package version and release scope: %#v", tool.Name, tool.InputSchema)
		}
		if tool.InputSchema["additionalProperties"] != false {
			t.Fatalf("tool %q accepts unknown arguments: %#v", tool.Name, tool.InputSchema)
		}
	}
	resources := server.Resources()
	if len(resources) != 4 {
		t.Fatalf("resources = %#v", resources)
	}
	for _, resource := range resources {
		if !strings.Contains(resource.URITemplate, "{package_id}") || !strings.Contains(resource.URITemplate, "{package_version}") || !strings.Contains(resource.URITemplate, "{release_id}") {
			t.Fatalf("resource lacks package version/release scope: %#v", resource)
		}
	}
}

func TestBookKnowledgeMCPReadsOnlyPinnedPackageRelease(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentPackageTestRelease()
	release.Citations[0].SourceHTML = "private-source-marker"
	release.Citations[0].SourceAccount = "private-account-marker"
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	pkg := agentToolPolicyTestPackage()
	savePassingAgentPackageTestEvaluation(t, store, pkg)
	knownTools := []string{
		"book-mcp/agent.search",
		"book-mcp/agent.resolve_citation",
		"book-mcp/agent.get_claim",
		"book-mcp/agent.package_metadata",
	}
	if _, _, err := PublishAgentPackage(store, pkg, "mcp-package", knownTools, testAgentPackageTime()); err != nil {
		t.Fatal(err)
	}
	server := NewBookKnowledgeMCPServer(store)

	metadataRaw, err := server.Call("agent.package_metadata", json.RawMessage(`{"package_id":"agent-package-example","package_version":"1.0.0","release_id":"release-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metadataRaw), `"package_id":"agent-package-example"`) ||
		!strings.Contains(string(metadataRaw), `"release_id":"release-1"`) {
		t.Fatalf("metadata = %s", metadataRaw)
	}

	searchRaw, err := server.Call("agent.search", json.RawMessage(`{"package_id":"agent-package-example","package_version":"1.0.0","release_id":"release-1","query":"grounded","limit":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(searchRaw), `"claim_id":"claim-1"`) || !strings.Contains(string(searchRaw), `"citation_ids":["citation-1"]`) {
		t.Fatalf("search = %s", searchRaw)
	}

	citationRaw, err := server.Call("agent.resolve_citation", json.RawMessage(`{"package_id":"agent-package-example","package_version":"1.0.0","release_id":"release-1","citation_id":"citation-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(citationRaw), `"chunk_id":"chunk-1"`) {
		t.Fatalf("citation = %s", citationRaw)
	}
	if strings.Contains(string(citationRaw), "private-source-marker") || strings.Contains(string(citationRaw), "private-account-marker") ||
		strings.Contains(string(citationRaw), "source_html") || strings.Contains(string(citationRaw), "source_account") {
		t.Fatalf("citation leaked private source fields: %s", citationRaw)
	}

	claimRaw, err := server.Call("agent.get_claim", json.RawMessage(`{"package_id":"agent-package-example","package_version":"1.0.0","release_id":"release-1","claim_id":"claim-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claimRaw), `"statement":"Synthetic grounded statement"`) {
		t.Fatalf("claim = %s", claimRaw)
	}
}

func TestBookKnowledgeMCPRejectsMissingScopeUnknownArgumentsAndWrites(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg := agentToolPolicyTestPackage()
	savePassingAgentPackageTestEvaluation(t, store, pkg)
	knownTools := []string{
		"book-mcp/agent.search",
		"book-mcp/agent.resolve_citation",
		"book-mcp/agent.get_claim",
		"book-mcp/agent.package_metadata",
	}
	if _, _, err := PublishAgentPackage(store, pkg, "mcp-package", knownTools, testAgentPackageTime()); err != nil {
		t.Fatal(err)
	}
	server := NewBookKnowledgeMCPServer(store)

	for _, tc := range []struct {
		name string
		tool string
		args string
		want string
	}{
		{name: "missing release scope", tool: "agent.search", args: `{"package_id":"agent-package-example","package_version":"1.0.0","query":"q"}`, want: "release_id"},
		{name: "missing package version", tool: "agent.search", args: `{"package_id":"agent-package-example","release_id":"release-1","query":"q"}`, want: "package_version"},
		{name: "unknown argument", tool: "agent.search", args: `{"package_id":"agent-package-example","package_version":"1.0.0","release_id":"release-1","query":"q","write":true}`, want: "unsupported argument"},
		{name: "write tool", tool: "agent.publish", args: `{"package_id":"agent-package-example","package_version":"1.0.0","release_id":"release-1"}`, want: "read-only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := server.Call(tc.tool, json.RawMessage(tc.args)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBookKnowledgeMCPPinsPackageVersionAndContextLimit(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	v1 := agentToolPolicyTestPackage()
	v1.RetrievalPolicy.MaxContextChunks = 1
	v1, _ = FinalizeAgentPackage(v1)
	savePassingAgentPackageTestEvaluation(t, store, v1)
	if _, _, err := PublishAgentPackage(store, v1, "mcp-v1", AgentReadOnlyToolIDs(), testAgentPackageTime()); err != nil {
		t.Fatal(err)
	}
	v2 := v1
	v2.Version = "2.0.0"
	v2.ModelPolicy.PreferredCapability = "different-capability"
	v2, _ = FinalizeAgentPackage(v2)
	savePassingAgentPackageTestEvaluation(t, store, v2)
	if _, _, err := PublishAgentPackage(store, v2, "mcp-v2", AgentReadOnlyToolIDs(), testAgentPackageTime().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	server := NewBookKnowledgeMCPServer(store)
	metadata, err := server.Call("agent.package_metadata", json.RawMessage(`{"package_id":"agent-package-example","package_version":"1.0.0","release_id":"release-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metadata), `"package_version":"1.0.0"`) || strings.Contains(string(metadata), "different-capability") {
		t.Fatalf("version-pinned metadata = %s", metadata)
	}
	if _, err := server.Call("agent.search", json.RawMessage(`{"package_id":"agent-package-example","package_version":"1.0.0","release_id":"release-1","query":"grounded","limit":2}`)); err == nil || !strings.Contains(err.Error(), "max_context_chunks") {
		t.Fatalf("context limit error = %v", err)
	}
}

func containsMCPString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func testAgentPackageTime() time.Time {
	return time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC)
}
