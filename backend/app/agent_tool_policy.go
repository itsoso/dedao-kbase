package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type AgentToolAudit struct {
	PackageID         string   `json:"package_id,omitempty"`
	PackageVersion    string   `json:"package_version,omitempty"`
	ReleaseID         string   `json:"release_id,omitempty"`
	ToolID            string   `json:"tool_id"`
	Decision          string   `json:"decision"`
	ArgumentNames     []string `json:"argument_names"`
	ArgumentHash      string   `json:"argument_hash"`
	RejectedArguments []string `json:"rejected_arguments,omitempty"`
}

type AgentToolPolicyDecision struct {
	Decision string         `json:"decision"`
	Reason   string         `json:"reason"`
	Audit    AgentToolAudit `json:"audit"`
}

type agentReadOnlyToolDefinition struct {
	Allowed  map[string]bool
	Required map[string]bool
}

var agentReadOnlyTools = map[string]agentReadOnlyToolDefinition{
	"agent.package_metadata": {
		Allowed:  stringBoolSet("package_id", "package_version", "release_id"),
		Required: stringBoolSet("package_id", "package_version", "release_id"),
	},
	"agent.search": {
		Allowed:  stringBoolSet("package_id", "package_version", "release_id", "query", "limit"),
		Required: stringBoolSet("package_id", "package_version", "release_id", "query"),
	},
	"agent.resolve_citation": {
		Allowed:  stringBoolSet("package_id", "package_version", "release_id", "citation_id"),
		Required: stringBoolSet("package_id", "package_version", "release_id", "citation_id"),
	},
	"agent.get_claim": {
		Allowed:  stringBoolSet("package_id", "package_version", "release_id", "claim_id"),
		Required: stringBoolSet("package_id", "package_version", "release_id", "claim_id"),
	},
}

func AgentReadOnlyToolIDs() []string {
	names := make([]string, 0, len(agentReadOnlyTools))
	for name := range agentReadOnlyTools {
		names = append(names, "book-mcp/"+name)
	}
	sort.Strings(names)
	return names
}

func EvaluateAgentToolCall(pkg AgentPackage, mcpServer, toolName string, arguments map[string]any) AgentToolPolicyDecision {
	toolID := strings.TrimSpace(mcpServer) + "/" + strings.TrimSpace(toolName)
	audit := AgentToolAudit{
		PackageID:      stringArgument(arguments, "package_id"),
		PackageVersion: stringArgument(arguments, "package_version"),
		ReleaseID:      stringArgument(arguments, "release_id"),
		ToolID:         toolID,
		ArgumentNames:  sortedArgumentNames(arguments),
		ArgumentHash:   agentToolArgumentHash(arguments),
	}
	block := func(reason string) AgentToolPolicyDecision {
		audit.Decision = AgentToolBlock
		return AgentToolPolicyDecision{Decision: AgentToolBlock, Reason: reason, Audit: audit}
	}
	definition, readOnly := agentReadOnlyTools[toolName]
	if mcpServer != "book-mcp" || !readOnly {
		return block(fmt.Sprintf("tool %q is outside the read-only Agent tool catalog", toolID))
	}
	if audit.PackageID == "" {
		return block("package_id scope is required")
	}
	if audit.PackageVersion == "" {
		return block("package_version scope is required")
	}
	if audit.ReleaseID == "" {
		return block("release_id scope is required")
	}
	if audit.PackageID != pkg.PackageID {
		return block(fmt.Sprintf("package scope %q does not match package %q", audit.PackageID, pkg.PackageID))
	}
	if audit.PackageVersion != pkg.Version {
		return block(fmt.Sprintf("package version scope %q does not match package version %q", audit.PackageVersion, pkg.Version))
	}
	pinned := false
	for _, release := range pkg.Releases {
		if release.ReleaseID == audit.ReleaseID {
			pinned = true
			break
		}
	}
	if !pinned {
		return block(fmt.Sprintf("release scope %q is not pinned by package", audit.ReleaseID))
	}
	for _, name := range audit.ArgumentNames {
		if !definition.Allowed[name] {
			audit.RejectedArguments = append(audit.RejectedArguments, name)
		}
	}
	if len(audit.RejectedArguments) > 0 {
		return block(fmt.Sprintf("unsupported argument(s): %s", strings.Join(audit.RejectedArguments, ", ")))
	}
	requiredNames := make([]string, 0, len(definition.Required))
	for name := range definition.Required {
		requiredNames = append(requiredNames, name)
	}
	sort.Strings(requiredNames)
	for _, name := range requiredNames {
		if !nonEmptyAgentArgument(arguments[name]) {
			return block(fmt.Sprintf("%s is required", name))
		}
	}
	for _, rule := range pkg.ToolPolicy.Tools {
		if rule.MCPServer != mcpServer || rule.ToolName != toolName {
			continue
		}
		switch rule.Decision {
		case AgentToolAllow:
			audit.Decision = AgentToolAllow
			return AgentToolPolicyDecision{Decision: AgentToolAllow, Reason: "read-only tool allowed by package policy", Audit: audit}
		case AgentToolRequireConfirmation:
			audit.Decision = AgentToolRequireConfirmation
			return AgentToolPolicyDecision{Decision: AgentToolRequireConfirmation, Reason: "package policy requires confirmation", Audit: audit}
		default:
			return block("package policy blocks tool")
		}
	}
	return block(fmt.Sprintf("tool %q is not in the package allowlist", toolID))
}

func agentToolArgumentHash(arguments map[string]any) string {
	payload, err := json.Marshal(arguments)
	if err != nil {
		payload = []byte("{}")
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedArgumentNames(arguments map[string]any) []string {
	names := make([]string, 0, len(arguments))
	for name := range arguments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func stringArgument(arguments map[string]any, name string) string {
	value, _ := arguments[name].(string)
	return strings.TrimSpace(value)
}

func nonEmptyAgentArgument(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case nil:
		return false
	default:
		return true
	}
}

func stringBoolSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
