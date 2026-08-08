package app

import (
	"fmt"
	"strings"
)

type ControlledAgentDraftRequest struct {
	ReleaseID           string  `json:"release_id"`
	PackageID           string  `json:"package_id"`
	Version             string  `json:"version"`
	PreferredCapability string  `json:"preferred_capability"`
	MaxContextChunks    int     `json:"max_context_chunks"`
	MaxCostUSD          float64 `json:"max_cost_usd"`
	TimeoutMS           int     `json:"timeout_ms"`
}

type ControlledAgentDraft struct {
	Package AgentPackage         `json:"package"`
	Suite   AgentEvaluationSuite `json:"suite"`
}

func BuildControlledAgentPackageDraft(store *BookKnowledgeStore, request ControlledAgentDraftRequest, knownTools []string) (*ControlledAgentDraft, error) {
	if store == nil {
		return nil, fmt.Errorf("published release store is required")
	}
	releaseID := strings.TrimSpace(request.ReleaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release_id is required")
	}
	release, err := store.LoadKnowledgeRelease(releaseID)
	if err != nil {
		return nil, fmt.Errorf("load published release: %w", err)
	}
	if release.Analysis == nil || len(release.Analysis.Claims) == 0 {
		return nil, fmt.Errorf("published release requires at least one analysis claim")
	}
	claim := release.Analysis.Claims[0]
	if strings.TrimSpace(claim.Statement) == "" || len(claim.CitationIDs) == 0 {
		return nil, fmt.Errorf("published release claim requires statement and citations")
	}
	citationByID := make(map[string]BookKnowledgeCitation, len(release.Citations))
	citationIDs := make([]string, 0, len(release.Citations))
	for _, citation := range release.Citations {
		citationID := strings.TrimSpace(citation.CitationID)
		if citationID == "" {
			continue
		}
		citationByID[citationID] = citation
		citationIDs = append(citationIDs, citationID)
	}
	primaryCitation, ok := citationByID[strings.TrimSpace(claim.CitationIDs[0])]
	if !ok || strings.TrimSpace(primaryCitation.ChunkID) == "" {
		return nil, fmt.Errorf("published release claim citation does not resolve to a chunk")
	}

	packageID := strings.TrimSpace(request.PackageID)
	version := strings.TrimSpace(request.Version)
	if packageID == "" || version == "" {
		return nil, fmt.Errorf("package_id and version are required")
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
	if len(tools) == 0 || tools[0].ToolName != "agent.search" {
		return nil, fmt.Errorf("controlled Agent requires book-mcp/agent.search")
	}
	usagePolicy := strings.TrimSpace(release.UsagePolicy)
	if usagePolicy == "" {
		usagePolicy = BookUsageStandard
	}
	pkg := AgentPackage{
		SchemaVersion:  AgentPackageSchemaVersion,
		PackageID:      packageID,
		Version:        version,
		LifecycleState: AgentPackageDraft,
		Releases: []AgentPackageReleaseRef{{
			ReleaseID: release.ReleaseID, ContentHash: release.ContentHash, CitationIDs: uniqueTrimmedStrings(citationIDs),
		}},
		RetrievalPolicy: AgentPackageRetrievalPolicy{
			Strategy: "lexical", AllowedSourceTypes: []string{release.Book.SourceType}, RequireCitations: true, MaxContextChunks: maxContextChunks,
		},
		ModelPolicy: AgentPackageModelPolicy{
			PreferredCapability: capability, Fallbacks: []string{"qwen-plus"}, MaxCostUSD: maxCostUSD, TimeoutMS: timeoutMS,
		},
		PromptProfiles: []AgentPackagePromptProfile{{ProfileID: "grounded-answer.v1", OutputSchema: "grounded-answer.v1"}},
		ToolPolicy:     AgentPackageToolPolicy{Tools: tools},
		SafetyPolicy: AgentPackageSafetyPolicy{
			UsagePolicy: usagePolicy, AbstentionReasons: []string{"insufficient_evidence", "outside_scope"}, EscalationTarget: "human_review",
		},
		EvaluationPolicy: AgentPackageEvaluationPolicy{
			SuiteVersion: "controlled-book-agent-v1",
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
	suite, err := buildControlledAgentEvaluationSuite(store, pkg, claim, primaryCitation, maxCostUSD, timeoutMS)
	if err != nil {
		return nil, err
	}
	return &ControlledAgentDraft{Package: pkg, Suite: suite}, nil
}

func controlledAgentReadOnlyTools(knownTools []string) []AgentPackageToolRule {
	known := stringSet(knownTools)
	tools := make([]AgentPackageToolRule, 0, 4)
	for _, toolName := range []string{"agent.search", "agent.resolve_citation", "agent.get_claim", "agent.package_metadata"} {
		if _, ok := known["book-mcp/"+toolName]; ok {
			tools = append(tools, AgentPackageToolRule{MCPServer: "book-mcp", ToolName: toolName, Decision: AgentToolAllow})
		}
	}
	return tools
}

func buildControlledAgentEvaluationSuite(store *BookKnowledgeStore, pkg AgentPackage, claim BookAnalysisClaim, citation BookKnowledgeCitation, maxCostUSD float64, timeoutMS int) (AgentEvaluationSuite, error) {
	query := strings.TrimSpace(claim.Statement)
	search, err := searchAgentPackageEvidence(store, pkg, query, pkg.RetrievalPolicy.MaxContextChunks)
	if err != nil {
		return AgentEvaluationSuite{}, err
	}
	resolved, err := resolveAgentRuntimeCitations(store, search.Results)
	if err != nil {
		return AgentEvaluationSuite{}, err
	}
	expectedChunks := make([]string, 0, len(resolved))
	for _, item := range resolved {
		if strings.TrimSpace(item.ChunkID) != "" {
			expectedChunks = append(expectedChunks, item.ChunkID)
		}
	}
	if len(expectedChunks) == 0 {
		expectedChunks = []string{citation.ChunkID}
	}
	modelOutput := claim.Statement + " [citation:" + citation.CitationID + "]"
	arguments := map[string]string{
		"package_id": pkg.PackageID, "package_version": pkg.Version, "release_id": pkg.Releases[0].ReleaseID, "query": query,
	}
	cases := []AgentEvaluationCase{
		{CaseID: "retrieval-primary", Metric: "retrieval", Input: query, ExpectedIDs: []string{citation.ChunkID}},
		{CaseID: "retrieval-precision-primary", Metric: "retrieval_precision", Input: query, ExpectedIDs: expectedChunks},
		{CaseID: "citations-primary", Metric: "citations", Input: query, ExpectedIDs: []string{citation.CitationID}, ModelOutput: modelOutput},
		{CaseID: "faithfulness-primary", Metric: "faithfulness", Input: query, ExpectedIDs: []string{claim.ID}, ExpectedValue: claim.Statement, ModelOutput: modelOutput},
		{CaseID: "abstention-outside-scope", Metric: "abstention", Input: "__kbase_controlled_agent_outside_scope__", ExpectedValue: "insufficient_evidence"},
		{CaseID: "tool-choice-search", Metric: "tool_choice", Input: query, ExpectedValue: "book-mcp/agent.search", ProposedTool: "book-mcp/agent.search", ProposedArguments: arguments},
		{CaseID: "tool-arguments-search", Metric: "tool_arguments", Input: query, ProposedTool: "book-mcp/agent.search", ProposedArguments: arguments, ExpectedArguments: arguments},
		{CaseID: "task-completion-primary", Metric: "task_completion", Input: query, ExpectedValue: claim.Statement, ModelOutput: modelOutput},
		{CaseID: "latency-recorded", Metric: "latency", Input: query, ModelOutput: modelOutput, RecordedLatencyMS: 25, MaxLatencyMS: timeoutMS},
		{CaseID: "cost-bound", Metric: "cost", Input: query, ModelOutput: modelOutput, MaxCostUSD: maxCostUSD},
	}
	return AgentEvaluationSuite{SchemaVersion: AgentEvaluationSchemaVersion, SuiteVersion: pkg.EvaluationPolicy.SuiteVersion, Cases: cases}, nil
}
