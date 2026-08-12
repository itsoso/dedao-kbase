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

func BuildControlledCollectionAgentDraftBundle(store *BookKnowledgeStore, request ControlledCollectionAgentDraftRequest, knownTools []string) (*ControlledAgentDraft, error) {
	pkg, err := BuildControlledCollectionAgentDraft(store, request, knownTools)
	if err != nil {
		return nil, err
	}
	suite, err := buildControlledCollectionAgentEvaluationSuite(store, *pkg)
	if err != nil {
		return nil, err
	}
	return &ControlledAgentDraft{Package: *pkg, Suite: suite}, nil
}

func buildControlledCollectionAgentEvaluationSuite(store *BookKnowledgeStore, pkg AgentPackage) (AgentEvaluationSuite, error) {
	release, err := loadPinnedAgentCollectionRelease(store, pkg)
	if err != nil {
		return AgentEvaluationSuite{}, err
	}
	if len(release.Members) == 0 {
		return AgentEvaluationSuite{}, fmt.Errorf("collection release requires at least one member")
	}
	member := release.Members[0]
	article, err := loadPinnedAgentCollectionMember(store, *release, member)
	if err != nil {
		return AgentEvaluationSuite{}, err
	}
	if len(article.Chunks) == 0 {
		return AgentEvaluationSuite{}, fmt.Errorf("collection member requires at least one chunk")
	}
	chunk := article.Chunks[0]
	allowed := stringBoolSet(member.CitationIDs...)
	citationID := ""
	for _, citation := range article.Citations {
		if citation.ChunkID == chunk.ChunkID && allowed[citation.CitationID] {
			citationID = citation.CitationID
			break
		}
	}
	if citationID == "" {
		return AgentEvaluationSuite{}, fmt.Errorf("collection member chunk requires a pinned citation")
	}
	query := strings.TrimSpace(chunk.Text)
	if query == "" {
		return AgentEvaluationSuite{}, fmt.Errorf("collection member chunk text is required")
	}
	search, err := searchAgentPackageEvidence(store, pkg, query, pkg.RetrievalPolicy.MaxContextChunks)
	if err != nil {
		return AgentEvaluationSuite{}, err
	}
	citations, err := resolveAgentRuntimeCitations(store, search.Results)
	if err != nil {
		return AgentEvaluationSuite{}, err
	}
	expectedChunks := make([]string, 0, len(citations))
	for _, citation := range citations {
		if citation.ChunkID != "" {
			expectedChunks = append(expectedChunks, citation.ChunkID)
		}
	}
	expectedChunks = sortedUniqueStrings(expectedChunks)
	if len(expectedChunks) == 0 {
		return AgentEvaluationSuite{}, fmt.Errorf("collection evaluation probe resolved no chunks")
	}
	expectedValue := query
	if runes := []rune(expectedValue); len(runes) > 180 {
		expectedValue = string(runes[:180])
	}
	modelOutput := expectedValue + " [citation:" + citationID + "]"
	arguments := map[string]string{
		"package_id": pkg.PackageID, "package_version": pkg.Version,
		"release_id": release.ReleaseID, "query": query,
	}
	cases := []AgentEvaluationCase{
		{CaseID: "collection-retrieval", Metric: "retrieval", Input: query, ExpectedIDs: []string{chunk.ChunkID}},
		{CaseID: "collection-retrieval-precision", Metric: "retrieval_precision", Input: query, ExpectedIDs: expectedChunks},
		{CaseID: "collection-citations", Metric: "citations", Input: query, ExpectedIDs: []string{citationID}, ModelOutput: modelOutput},
		{CaseID: "collection-faithfulness", Metric: "faithfulness", Input: query, ExpectedIDs: []string{chunk.ChunkID}, ExpectedValue: expectedValue, ModelOutput: modelOutput},
		{CaseID: "collection-abstention", Metric: "abstention", Input: "__kbase_collection_outside_scope__", ExpectedValue: "insufficient_evidence"},
		{CaseID: "collection-tool-choice", Metric: "tool_choice", Input: query, ExpectedValue: "book-mcp/agent.search", ProposedTool: "book-mcp/agent.search", ProposedArguments: arguments},
		{CaseID: "collection-tool-arguments", Metric: "tool_arguments", Input: query, ProposedTool: "book-mcp/agent.search", ProposedArguments: arguments, ExpectedArguments: arguments},
		{CaseID: "collection-task-completion", Metric: "task_completion", Input: query, ExpectedValue: expectedValue, ModelOutput: modelOutput},
		{CaseID: "collection-latency", Metric: "latency", Input: query, ModelOutput: modelOutput, RecordedLatencyMS: 25, MaxLatencyMS: pkg.ModelPolicy.TimeoutMS},
		{CaseID: "collection-cost", Metric: "cost", Input: query, ModelOutput: modelOutput, MaxCostUSD: pkg.ModelPolicy.MaxCostUSD},
	}
	return AgentEvaluationSuite{SchemaVersion: AgentEvaluationSchemaVersion, SuiteVersion: pkg.EvaluationPolicy.SuiteVersion, Cases: cases}, nil
}
