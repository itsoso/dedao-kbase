package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	agentRuntimeDefaultMaxOutputTokens = 2200
	agentRuntimeUSDPerTokenCeiling     = 0.0001
)

type AgentPackageSearchRequest struct {
	PackageID      string `json:"package_id"`
	PackageVersion string `json:"package_version"`
	Query          string `json:"query"`
	Limit          int    `json:"limit,omitempty"`
}

type AgentPackageEvidence struct {
	ReleaseID   string   `json:"release_id"`
	ClaimID     string   `json:"claim_id"`
	Statement   string   `json:"statement"`
	CitationIDs []string `json:"citation_ids"`
	Score       float64  `json:"score"`
}

type AgentPackageSearchResponse struct {
	PackageID         string                 `json:"package_id"`
	PackageVersion    string                 `json:"package_version"`
	PackageHash       string                 `json:"package_hash"`
	RetrievalStrategy string                 `json:"retrieval_strategy"`
	Results           []AgentPackageEvidence `json:"results"`
}

type AgentPackageChatRequest struct {
	PackageID      string `json:"package_id"`
	PackageVersion string `json:"package_version"`
	Question       string `json:"question"`
}

type AgentPackageChatResponse struct {
	TraceID          string                 `json:"trace_id"`
	PackageID        string                 `json:"package_id"`
	PackageVersion   string                 `json:"package_version"`
	PackageHash      string                 `json:"package_hash"`
	Outcome          string                 `json:"outcome"`
	AbstentionReason string                 `json:"abstention_reason,omitempty"`
	Answer           string                 `json:"answer,omitempty"`
	Model            string                 `json:"model,omitempty"`
	ModelCapability  string                 `json:"model_capability,omitempty"`
	PromptProfile    string                 `json:"prompt_profile,omitempty"`
	Evidence         []AgentPackageEvidence `json:"evidence"`
	Citations        []AgentScopedCitation  `json:"citations"`
}

func SearchAgentPackage(store *BookKnowledgeStore, request AgentPackageSearchRequest) (*AgentPackageSearchResponse, error) {
	pkg, err := loadRunnableAgentPackage(store, request.PackageID, request.PackageVersion, "search")
	if err != nil {
		return nil, err
	}
	return searchAgentPackageEvidence(store, *pkg, request.Query, request.Limit)
}

func searchAgentPackageEvidence(
	store *BookKnowledgeStore,
	pkg AgentPackage,
	rawQuery string,
	requestedLimit int,
) (*AgentPackageSearchResponse, error) {
	query := strings.TrimSpace(rawQuery)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	limit := requestedLimit
	if limit <= 0 {
		limit = pkg.RetrievalPolicy.MaxContextChunks
	}
	if limit > pkg.RetrievalPolicy.MaxContextChunks {
		return nil, fmt.Errorf("limit exceeds retrieval_policy.max_context_chunks (%d)", pkg.RetrievalPolicy.MaxContextChunks)
	}
	results := make([]AgentPackageEvidence, 0)
	for _, ref := range pkg.Releases {
		release, loadErr := store.LoadKnowledgeRelease(ref.ReleaseID)
		if loadErr != nil {
			return nil, fmt.Errorf("load pinned release %q: %w", ref.ReleaseID, loadErr)
		}
		releaseResults, searchErr := searchAgentReleaseClaimsWithStrategy(
			store, *release, query, pkg.RetrievalPolicy.MaxContextChunks, pkg.RetrievalPolicy.Strategy,
		)
		if searchErr != nil {
			return nil, searchErr
		}
		for _, result := range releaseResults {
			results = append(results, AgentPackageEvidence{
				ReleaseID: ref.ReleaseID, ClaimID: result.ClaimID,
				Statement: result.Statement, CitationIDs: append([]string(nil), result.CitationIDs...),
				Score: result.Score,
			})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].ReleaseID != results[j].ReleaseID {
			return results[i].ReleaseID < results[j].ReleaseID
		}
		return results[i].ClaimID < results[j].ClaimID
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return &AgentPackageSearchResponse{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version, PackageHash: pkg.ContentHash,
		RetrievalStrategy: pkg.RetrievalPolicy.Strategy, Results: results,
	}, nil
}

func ChatAgentPackageWithClient(
	ctx context.Context,
	store *BookKnowledgeStore,
	request AgentPackageChatRequest,
	client BookKnowledgeLLMClient,
) (*AgentPackageChatResponse, error) {
	startedAt := time.Now().UTC()
	pkg, err := loadRunnableAgentPackage(store, request.PackageID, request.PackageVersion, "grounded_chat")
	if err != nil {
		return nil, err
	}
	return chatFinalizedAgentPackageWithClient(ctx, store, *pkg, request.Question, client, nil, startedAt, true)
}

func chatFinalizedAgentPackageWithClient(
	ctx context.Context,
	store *BookKnowledgeStore,
	pkg AgentPackage,
	rawQuestion string,
	client BookKnowledgeLLMClient,
	configOverride *BookTokenPlanConfig,
	startedAt time.Time,
	persistTrace bool,
) (*AgentPackageChatResponse, error) {
	question := strings.TrimSpace(rawQuestion)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	search, err := searchAgentPackageEvidence(store, pkg, question, pkg.RetrievalPolicy.MaxContextChunks)
	if err != nil {
		return nil, err
	}
	response := &AgentPackageChatResponse{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version, PackageHash: pkg.ContentHash,
		Outcome: AgentTraceOutcomeAbstained, Evidence: search.Results, Citations: []AgentScopedCitation{},
	}
	model := firstAgentPackageModel(pkg.ModelPolicy)
	if model == "" {
		return nil, fmt.Errorf("model_policy has no executable fallback model")
	}
	normalizedModel := normalizeBookTokenPlanModel(model)
	if len(search.Results) == 0 {
		response.AbstentionReason = preferredAgentAbstention(pkg.SafetyPolicy.AbstentionReasons)
		traceID, traceErr := maybeSaveAgentRuntimeTrace(persistTrace, store, pkg, search.Results, normalizedModel,
			AgentTraceOutcomeAbstained, response.AbstentionReason, nil, startedAt, time.Now().UTC())
		if traceErr != nil {
			return nil, traceErr
		}
		response.TraceID = traceID
		response.Model = normalizedModel
		response.ModelCapability = pkg.ModelPolicy.PreferredCapability
		return response, nil
	}
	citations, err := resolveAgentRuntimeCitations(store, search.Results)
	if err != nil {
		return nil, err
	}
	if pkg.RetrievalPolicy.RequireCitations && len(citations) == 0 {
		response.AbstentionReason = "citation_required"
		traceID, traceErr := maybeSaveAgentRuntimeTrace(persistTrace, store, pkg, search.Results, normalizedModel,
			AgentTraceOutcomeAbstained, response.AbstentionReason, nil, startedAt, time.Now().UTC())
		if traceErr != nil {
			return nil, traceErr
		}
		response.TraceID = traceID
		response.Model = normalizedModel
		response.ModelCapability = pkg.ModelPolicy.PreferredCapability
		return response, nil
	}
	if client == nil {
		client = NewTokenPlanChatClient(nil)
	}
	var cfg BookTokenPlanConfig
	if configOverride != nil {
		cfg = *configOverride
	} else {
		cfg, err = LoadBookTokenPlanConfig()
		if err != nil {
			return nil, err
		}
	}
	cfg.Model = normalizedModel
	promptProfile := pkg.PromptProfiles[0]
	messages := buildAgentPackageMessages(pkg, promptProfile, question, search.Results)
	if err := applyAgentRuntimeCostBudget(&cfg, messages, pkg.ModelPolicy.MaxCostUSD); err != nil {
		return nil, err
	}
	callCtx := ctx
	if pkg.ModelPolicy.TimeoutMS > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(pkg.ModelPolicy.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	answer, err := client.Chat(callCtx, cfg, messages)
	if err != nil {
		if _, traceErr := maybeSaveAgentRuntimeTrace(persistTrace, store, pkg, search.Results, cfg.Model,
			AgentTraceOutcomeFailed, err.Error(), nil, startedAt, time.Now().UTC()); traceErr != nil {
			return nil, fmt.Errorf("model call failed: %v; persist failed trace: %w", err, traceErr)
		}
		return nil, err
	}
	response.Model = cfg.Model
	response.ModelCapability = pkg.ModelPolicy.PreferredCapability
	response.PromptProfile = promptProfile.ProfileID
	usedCitations, citationErr := selectAgentRuntimeCitations(answer, citations)
	if citationErr != nil {
		response.AbstentionReason = "citation_required"
		traceID, traceErr := maybeSaveAgentRuntimeTrace(persistTrace, store, pkg, search.Results, cfg.Model,
			AgentTraceOutcomeAbstained, answer, nil, startedAt, time.Now().UTC())
		if traceErr != nil {
			return nil, traceErr
		}
		response.TraceID = traceID
		return response, nil
	}
	response.Outcome = AgentTraceOutcomeCompleted
	response.Answer = answer
	response.Citations = usedCitations
	traceID, err := maybeSaveAgentRuntimeTrace(persistTrace, store, pkg, search.Results, cfg.Model,
		AgentTraceOutcomeCompleted, answer, usedCitations, startedAt, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	response.TraceID = traceID
	return response, nil
}

func maybeSaveAgentRuntimeTrace(
	persist bool,
	store *BookKnowledgeStore,
	pkg AgentPackage,
	evidence []AgentPackageEvidence,
	model, outcome, responseText string,
	citations []AgentScopedCitation,
	startedAt, completedAt time.Time,
) (string, error) {
	if !persist {
		return "", nil
	}
	return saveAgentRuntimeTrace(store, pkg, evidence, model, outcome, responseText, citations, startedAt, completedAt)
}

func saveAgentRuntimeTrace(
	store *BookKnowledgeStore,
	pkg AgentPackage,
	evidence []AgentPackageEvidence,
	model, outcome, responseText string,
	citations []AgentScopedCitation,
	startedAt, completedAt time.Time,
) (string, error) {
	traceID, err := newAgentRuntimeTraceID()
	if err != nil {
		return "", err
	}
	releases := make([]AgentTraceReleaseRef, 0, len(pkg.Releases))
	for _, ref := range pkg.Releases {
		release, loadErr := store.LoadKnowledgeRelease(ref.ReleaseID)
		if loadErr != nil {
			return "", loadErr
		}
		releases = append(releases, AgentTraceReleaseRef{
			ReleaseID: ref.ReleaseID, Version: release.Version, ContentHash: ref.ContentHash,
		})
	}
	retrievals := make([]AgentTraceRetrieval, 0, len(evidence))
	for index, item := range evidence {
		retrievals = append(retrievals, AgentTraceRetrieval{
			EvidenceID: agentRuntimeEvidenceID(item), ReleaseID: item.ReleaseID,
			Score: item.Score, Rank: index + 1,
		})
	}
	traceCitations := agentRuntimeTraceCitations(evidence, citations)
	trace := AgentTrace{
		SchemaVersion: AgentTraceSchemaVersion,
		TraceID:       traceID,
		Package: AgentTracePackageRef{
			PackageID: pkg.PackageID, Version: pkg.Version, ContentHash: pkg.ContentHash,
		},
		Releases: releases, Retrievals: retrievals,
		ModelRoute: AgentTraceModelRoute{
			Provider: "tokenplan", Model: model, Capability: pkg.ModelPolicy.PreferredCapability,
		},
		ToolCalls: []AgentTraceToolCall{},
		Final: AgentTraceFinal{
			Outcome: outcome, ResponseFingerprint: sha256Fingerprint([]byte(responseText)),
			Citations: traceCitations,
		},
		StartedAt: startedAt.Format(time.RFC3339Nano), CompletedAt: completedAt.Format(time.RFC3339Nano),
	}
	if err := store.SaveAgentTrace(trace); err != nil {
		return "", err
	}
	return traceID, nil
}

func newAgentRuntimeTraceID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create agent trace id: %w", err)
	}
	return "agent-run-" + hex.EncodeToString(random), nil
}

func agentRuntimeEvidenceID(item AgentPackageEvidence) string {
	return item.ReleaseID + ":" + item.ClaimID
}

func agentRuntimeTraceCitations(evidence []AgentPackageEvidence, citations []AgentScopedCitation) []AgentTraceCitation {
	allowed := make(map[string]bool, len(citations))
	for _, citation := range citations {
		allowed[citation.CitationID] = true
	}
	seen := make(map[string]bool)
	result := make([]AgentTraceCitation, 0, len(citations))
	for _, item := range evidence {
		for _, citationID := range item.CitationIDs {
			key := item.ReleaseID + "\x00" + item.ClaimID + "\x00" + citationID
			if !allowed[citationID] || seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, AgentTraceCitation{
				CitationID: citationID, ReleaseID: item.ReleaseID, EvidenceID: agentRuntimeEvidenceID(item),
			})
		}
	}
	return result
}

func loadRunnableAgentPackage(store *BookKnowledgeStore, packageID, version, capability string) (*AgentPackage, error) {
	if store == nil {
		return nil, fmt.Errorf("agent package store is required")
	}
	packageID = strings.TrimSpace(packageID)
	version = strings.TrimSpace(version)
	if packageID == "" || version == "" {
		return nil, fmt.Errorf("package_id and package_version are required")
	}
	pkg, err := store.LoadAgentPackage(packageID, version)
	if err != nil {
		return nil, err
	}
	if pkg.LifecycleState != AgentPackagePublished {
		return nil, fmt.Errorf("agent package %s is not published", agentPackageReference(pkg.PackageID, pkg.Version))
	}
	if err := ValidateAgentPackageEvaluationGate(store, *pkg); err != nil {
		return nil, err
	}
	if !agentPackageHasCapability(*pkg, capability) {
		return nil, fmt.Errorf("capability %q is not declared by the package manifest", capability)
	}
	return pkg, nil
}

func agentPackageHasCapability(pkg AgentPackage, capability string) bool {
	for _, declared := range pkg.UIManifest.Capabilities {
		if strings.TrimSpace(declared) == capability {
			return true
		}
	}
	return false
}

func firstAgentPackageModel(policy AgentPackageModelPolicy) string {
	for _, model := range policy.Fallbacks {
		if strings.TrimSpace(model) != "" {
			return strings.TrimSpace(model)
		}
	}
	return ""
}

func applyAgentRuntimeCostBudget(cfg *BookTokenPlanConfig, messages []BookKnowledgeMessage, maxCostUSD float64) error {
	if cfg == nil {
		return fmt.Errorf("model configuration is required")
	}
	inputTokens := 0
	for _, message := range messages {
		inputTokens += len([]rune(message.Content))
	}
	remainingUSD := maxCostUSD - float64(inputTokens)*agentRuntimeUSDPerTokenCeiling
	maxOutputTokens := int(remainingUSD / agentRuntimeUSDPerTokenCeiling)
	if maxOutputTokens < 1 {
		return fmt.Errorf("model_policy.max_cost_usd cost budget is exhausted before the model call")
	}
	if maxOutputTokens > agentRuntimeDefaultMaxOutputTokens {
		maxOutputTokens = agentRuntimeDefaultMaxOutputTokens
	}
	cfg.MaxTokens = maxOutputTokens
	return nil
}

func agentRuntimeEstimatedMaxCostUSD(messages []BookKnowledgeMessage, maxOutputTokens int) float64 {
	inputTokens := 0
	for _, message := range messages {
		inputTokens += len([]rune(message.Content))
	}
	return float64(inputTokens+maxOutputTokens) * agentRuntimeUSDPerTokenCeiling
}

func preferredAgentAbstention(reasons []string) string {
	for _, reason := range reasons {
		if strings.TrimSpace(reason) == "insufficient_evidence" {
			return "insufficient_evidence"
		}
	}
	if len(reasons) > 0 {
		return strings.TrimSpace(reasons[0])
	}
	return "insufficient_evidence"
}

func buildAgentPackagePrompt(question string, evidence []AgentPackageEvidence) string {
	var builder strings.Builder
	builder.WriteString("Question: ")
	builder.WriteString(question)
	builder.WriteString("\n\nPinned package evidence:\n")
	for _, item := range evidence {
		fmt.Fprintf(&builder, "- release=%s claim=%s citations=%s\n  %s\n",
			item.ReleaseID, item.ClaimID, strings.Join(item.CitationIDs, ","), item.Statement)
	}
	return builder.String()
}

func buildAgentPackageMessages(
	pkg AgentPackage,
	promptProfile AgentPackagePromptProfile,
	question string,
	evidence []AgentPackageEvidence,
) []BookKnowledgeMessage {
	return []BookKnowledgeMessage{
		{
			Role: "system",
			Content: fmt.Sprintf("You are executing immutable Agent Package %s version %s. Use only the pinned evidence below. Capability: %s. Usage policy: %s. Output schema: %s. Cite every factual claim as [citation:<id>]. If evidence is insufficient, abstain.",
				pkg.PackageID, pkg.Version, pkg.ModelPolicy.PreferredCapability, pkg.SafetyPolicy.UsagePolicy, promptProfile.OutputSchema),
		},
		{Role: "user", Content: buildAgentPackagePrompt(question, evidence)},
	}
}

func resolveAgentRuntimeCitations(store *BookKnowledgeStore, evidence []AgentPackageEvidence) ([]AgentScopedCitation, error) {
	wanted := make(map[string]map[string]bool)
	for _, item := range evidence {
		if wanted[item.ReleaseID] == nil {
			wanted[item.ReleaseID] = make(map[string]bool)
		}
		for _, citationID := range item.CitationIDs {
			wanted[item.ReleaseID][citationID] = true
		}
	}
	result := make([]AgentScopedCitation, 0)
	for releaseID, citationIDs := range wanted {
		release, err := store.LoadKnowledgeRelease(releaseID)
		if err != nil {
			return nil, err
		}
		for _, citation := range release.Citations {
			if citationIDs[citation.CitationID] {
				result = append(result, AgentScopedCitation{
					CitationID: citation.CitationID, BookID: citation.BookID,
					ChapterID: citation.ChapterID, ChunkID: citation.ChunkID,
					Anchor: citation.Anchor, Note: citation.Note,
					SourceType: citation.SourceType, PublishedAt: citation.PublishedAt,
				})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].BookID != result[j].BookID {
			return result[i].BookID < result[j].BookID
		}
		return result[i].CitationID < result[j].CitationID
	})
	return result, nil
}

func selectAgentRuntimeCitations(answer string, available []AgentScopedCitation) ([]AgentScopedCitation, error) {
	byID := make(map[string]AgentScopedCitation, len(available))
	for _, citation := range available {
		byID[citation.CitationID] = citation
	}
	seen := make(map[string]bool)
	selected := make([]AgentScopedCitation, 0)
	remaining := answer
	for {
		start := strings.Index(remaining, "[citation:")
		if start < 0 {
			break
		}
		remaining = remaining[start+len("[citation:"):]
		end := strings.Index(remaining, "]")
		if end < 0 {
			break
		}
		citationID := strings.TrimSpace(remaining[:end])
		remaining = remaining[end+1:]
		citation, ok := byID[citationID]
		if !ok {
			return nil, fmt.Errorf("answer citation %q is outside retrieved evidence", citationID)
		}
		if !seen[citationID] {
			seen[citationID] = true
			selected = append(selected, citation)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("answer requires at least one retrieved citation")
	}
	return selected, nil
}
