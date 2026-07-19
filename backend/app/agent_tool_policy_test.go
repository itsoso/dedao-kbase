package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentToolPolicyAllowsScopedReadOnlyCallWithBoundedAudit(t *testing.T) {
	pkg := agentToolPolicyTestPackage()
	decision := EvaluateAgentToolCall(pkg, "book-mcp", "agent.search", map[string]any{
		"package_id":      "agent-package-example",
		"package_version": "1.0.0",
		"release_id":      "release-1",
		"query":           "private query must not enter audit",
		"limit":           float64(5),
	})
	if decision.Decision != AgentToolAllow || decision.Audit.PackageID != pkg.PackageID ||
		decision.Audit.ReleaseID != "release-1" || !strings.HasPrefix(decision.Audit.ArgumentHash, "sha256:") {
		t.Fatalf("decision = %#v", decision)
	}
	raw, err := json.Marshal(decision.Audit)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private query") || !strings.Contains(string(raw), "query") {
		t.Fatalf("audit leaked values or omitted argument names: %s", raw)
	}
}

func TestAgentToolPolicyRequiresScopeAndRejectsArguments(t *testing.T) {
	pkg := agentToolPolicyTestPackage()
	tests := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{name: "missing package", tool: "agent.search", args: map[string]any{"package_version": pkg.Version, "release_id": "release-1", "query": "q"}, want: "package_id"},
		{name: "missing package version", tool: "agent.search", args: map[string]any{"package_id": pkg.PackageID, "release_id": "release-1", "query": "q"}, want: "package_version"},
		{name: "wrong package version", tool: "agent.search", args: map[string]any{"package_id": pkg.PackageID, "package_version": "2.0.0", "release_id": "release-1", "query": "q"}, want: "package version"},
		{name: "missing release", tool: "agent.search", args: map[string]any{"package_id": pkg.PackageID, "package_version": pkg.Version, "query": "q"}, want: "release_id"},
		{name: "wrong package", tool: "agent.search", args: map[string]any{"package_id": "other", "package_version": pkg.Version, "release_id": "release-1", "query": "q"}, want: "package scope"},
		{name: "unpinned release", tool: "agent.search", args: map[string]any{"package_id": pkg.PackageID, "package_version": pkg.Version, "release_id": "release-other", "query": "q"}, want: "release scope"},
		{name: "unknown argument", tool: "agent.search", args: map[string]any{"package_id": pkg.PackageID, "package_version": pkg.Version, "release_id": "release-1", "query": "q", "write": true}, want: "unsupported argument"},
		{name: "write tool", tool: "agent.publish", args: map[string]any{"package_id": pkg.PackageID, "package_version": pkg.Version, "release_id": "release-1"}, want: "read-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := EvaluateAgentToolCall(pkg, "book-mcp", tt.tool, tt.args)
			if decision.Decision != AgentToolBlock || !strings.Contains(decision.Reason, tt.want) {
				t.Fatalf("decision = %#v, want blocked reason %q", decision, tt.want)
			}
		})
	}
}

func TestAgentToolPolicyPreservesRequireConfirmation(t *testing.T) {
	pkg := agentToolPolicyTestPackage()
	pkg.ToolPolicy.Tools[0].Decision = AgentToolRequireConfirmation
	decision := EvaluateAgentToolCall(pkg, "book-mcp", "agent.search", map[string]any{
		"package_id":      pkg.PackageID,
		"package_version": pkg.Version,
		"release_id":      "release-1",
		"query":           "q",
	})
	if decision.Decision != AgentToolRequireConfirmation || !strings.Contains(decision.Reason, "confirmation") {
		t.Fatalf("decision = %#v", decision)
	}
}

func agentToolPolicyTestPackage() AgentPackage {
	pkg := validAgentPackage()
	pkg.ToolPolicy.Tools = []AgentPackageToolRule{
		{MCPServer: "book-mcp", ToolName: "agent.search", Decision: AgentToolAllow},
		{MCPServer: "book-mcp", ToolName: "agent.resolve_citation", Decision: AgentToolAllow},
		{MCPServer: "book-mcp", ToolName: "agent.get_claim", Decision: AgentToolAllow},
		{MCPServer: "book-mcp", ToolName: "agent.package_metadata", Decision: AgentToolAllow},
	}
	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		panic(err)
	}
	return finalized
}
