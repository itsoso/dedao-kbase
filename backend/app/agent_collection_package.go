package app

import (
	"fmt"
	"strings"
)

type ControlledCollectionAgentDraftRequest struct {
	CollectionReleaseID string  `json:"collection_release_id"`
	PackageID           string  `json:"package_id,omitempty"`
	Version             string  `json:"version,omitempty"`
	PreferredCapability string  `json:"preferred_capability,omitempty"`
	MaxContextChunks    int     `json:"max_context_chunks,omitempty"`
	MaxCostUSD          float64 `json:"max_cost_usd,omitempty"`
	TimeoutMS           int     `json:"timeout_ms,omitempty"`
}

func BuildControlledCollectionAgentDraft(store *BookKnowledgeStore, request ControlledCollectionAgentDraftRequest, knownTools []string) (*AgentPackage, error) {
	if store == nil {
		return nil, fmt.Errorf("published collection release store is required")
	}
	releaseID := strings.TrimSpace(request.CollectionReleaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("collection_release_id is required")
	}
	release, err := store.LoadKnowledgeCollectionRelease(releaseID)
	if err != nil {
		return nil, fmt.Errorf("load published collection release: %w", err)
	}
	if release.Definition.SourceType != "wechat_mp_article" {
		return nil, fmt.Errorf("collection release source type %q is unsupported", release.Definition.SourceType)
	}
	if len(release.Members) == 0 {
		return nil, fmt.Errorf("collection release requires at least one member")
	}
	packageID := strings.TrimSpace(request.PackageID)
	if packageID == "" {
		packageID = release.CollectionID + "-agent"
	}
	version := strings.TrimSpace(request.Version)
	if version == "" {
		version = "1.0.0"
	}
	capability := strings.TrimSpace(request.PreferredCapability)
	if capability == "" {
		capability = "reasoning"
	}
	maxContextChunks := request.MaxContextChunks
	if maxContextChunks <= 0 {
		maxContextChunks = 12
	}
	maxCostUSD := request.MaxCostUSD
	if maxCostUSD <= 0 {
		maxCostUSD = 0.25
	}
	timeoutMS := request.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 30000
	}
	tools := controlledAgentReadOnlyTools(knownTools)
	configured := make(map[string]bool, len(tools))
	for _, rule := range tools {
		configured[rule.MCPServer+"/"+rule.ToolName] = true
	}
	for _, required := range []string{"book-mcp/agent.search", "book-mcp/agent.resolve_citation"} {
		if !configured[required] {
			return nil, fmt.Errorf("controlled collection Agent requires %s", required)
		}
	}
	pkg := AgentPackage{
		SchemaVersion: AgentPackageSchemaVersionV3,
		PackageID:     packageID, Version: version, LifecycleState: AgentPackageDraft,
		CollectionReleases: []AgentPackageCollectionRef{{ReleaseID: release.ReleaseID, ContentHash: release.ContentHash}},
		RetrievalPolicy: AgentPackageRetrievalPolicy{
			Strategy: "lexical", AllowedSourceTypes: []string{release.Definition.SourceType}, RequireCitations: true, MaxContextChunks: maxContextChunks,
		},
		ModelPolicy: AgentPackageModelPolicy{
			PreferredCapability: capability, Fallbacks: []string{"qwen3.7-max"}, MaxCostUSD: maxCostUSD, TimeoutMS: timeoutMS,
		},
		PromptProfiles: []AgentPackagePromptProfile{{ProfileID: "grounded-answer.v1", OutputSchema: "grounded-answer.v1"}},
		ToolPolicy:     AgentPackageToolPolicy{Tools: tools},
		SafetyPolicy: AgentPackageSafetyPolicy{
			UsagePolicy: BookUsageEvidenceOnly, AbstentionReasons: []string{"insufficient_evidence", "outside_scope"}, EscalationTarget: "human_review",
		},
		EvaluationPolicy: AgentPackageEvaluationPolicy{
			SuiteVersion: "controlled-collection-agent-v1",
			MinimumScores: map[string]float64{
				"retrieval": 1, "retrieval_precision": 1, "citations": 1, "faithfulness": 1, "abstention": 1,
				"tool_choice": 1, "tool_arguments": 1, "task_completion": 1, "latency": 1, "cost": 1,
			},
		},
		UIManifest: AgentPackageUIManifest{Capabilities: []string{"reader", "search", "grounded_chat", "evidence"}},
	}
	pkg, err = FinalizeAgentPackage(pkg)
	if err != nil {
		return nil, err
	}
	if err := ValidateAgentPackage(pkg, store, knownTools); err != nil {
		return nil, err
	}
	return &pkg, nil
}
