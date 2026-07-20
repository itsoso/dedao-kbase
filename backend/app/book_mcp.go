package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type BookKnowledgeMCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type BookKnowledgeMCPResource struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URITemplate string `json:"uriTemplate"`
}

type AgentScopedSearchResult struct {
	ClaimID     string   `json:"claim_id"`
	Statement   string   `json:"statement"`
	CitationIDs []string `json:"citation_ids"`
	Score       float64  `json:"score"`
}

type AgentScopedCitation struct {
	CitationID  string `json:"citation_id"`
	BookID      string `json:"book_id"`
	ChapterID   string `json:"chapter_id,omitempty"`
	ChunkID     string `json:"chunk_id,omitempty"`
	Anchor      string `json:"anchor,omitempty"`
	Note        string `json:"note,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type BookKnowledgeMCPServer struct {
	store *BookKnowledgeStore
}

func NewBookKnowledgeMCPServer(store *BookKnowledgeStore) *BookKnowledgeMCPServer {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	return &BookKnowledgeMCPServer{store: store}
}

func (s *BookKnowledgeMCPServer) Tools() []BookKnowledgeMCPTool {
	scope := map[string]any{
		"package_id":      map[string]any{"type": "string", "minLength": 1},
		"package_version": map[string]any{"type": "string", "minLength": 1},
		"release_id":      map[string]any{"type": "string", "minLength": 1},
	}
	return []BookKnowledgeMCPTool{
		{
			Name:        "agent.package_metadata",
			Description: "Read bounded metadata for one published Agent Package and pinned release.",
			InputSchema: strictObjectSchema(scope, []string{"package_id", "package_version", "release_id"}),
		},
		{
			Name:        "agent.search",
			Description: "Search claims inside one explicitly pinned Agent Package release.",
			InputSchema: strictObjectSchema(mergeSchemaProperties(scope, map[string]any{
				"query": map[string]any{"type": "string", "minLength": 1},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
			}), []string{"package_id", "package_version", "release_id", "query"}),
		},
		{
			Name:        "agent.resolve_citation",
			Description: "Resolve one citation ID inside one explicitly pinned Agent Package release.",
			InputSchema: strictObjectSchema(mergeSchemaProperties(scope, map[string]any{
				"citation_id": map[string]any{"type": "string", "minLength": 1},
			}), []string{"package_id", "package_version", "release_id", "citation_id"}),
		},
		{
			Name:        "agent.get_claim",
			Description: "Read one claim inside one explicitly pinned Agent Package release.",
			InputSchema: strictObjectSchema(mergeSchemaProperties(scope, map[string]any{
				"claim_id": map[string]any{"type": "string", "minLength": 1},
			}), []string{"package_id", "package_version", "release_id", "claim_id"}),
		},
	}
}

func (s *BookKnowledgeMCPServer) Resources() []BookKnowledgeMCPResource {
	const base = "agent-package://{package_id}/versions/{package_version}/releases/{release_id}"
	return []BookKnowledgeMCPResource{
		{Name: "agent.package_metadata", Description: "Published package metadata.", URITemplate: base + "/metadata"},
		{Name: "agent.search", Description: "Package-scoped claim search.", URITemplate: base + "/search{?query,limit}"},
		{Name: "agent.resolve_citation", Description: "Package-scoped citation resolution.", URITemplate: base + "/citations/{citation_id}"},
		{Name: "agent.get_claim", Description: "Package-scoped claim lookup.", URITemplate: base + "/claims/{claim_id}"},
	}
}

func (s *BookKnowledgeMCPServer) Call(name string, arguments json.RawMessage) (json.RawMessage, error) {
	var input map[string]any
	if len(arguments) == 0 {
		input = map[string]any{}
	} else if err := json.Unmarshal(arguments, &input); err != nil {
		return nil, fmt.Errorf("invalid Agent tool arguments: %w", err)
	}
	packageID := stringArgument(input, "package_id")
	packageVersion := stringArgument(input, "package_version")
	pkg := AgentPackage{PackageID: packageID}
	if packageID != "" && packageVersion != "" {
		loaded, err := s.store.LoadAgentPackage(packageID, packageVersion)
		if err != nil {
			return nil, fmt.Errorf("load Agent Package: %w", err)
		}
		pkg = *loaded
		if pkg.LifecycleState != AgentPackagePublished {
			return nil, fmt.Errorf("agent package %s is not published", agentPackageReference(pkg.PackageID, pkg.Version))
		}
		if err := ValidateAgentPackageEvaluationGate(s.store, pkg); err != nil {
			return nil, fmt.Errorf("agent package evaluation gate: %w", err)
		}
	}
	decision := EvaluateAgentToolCall(pkg, "book-mcp", name, input)
	if decision.Decision != AgentToolAllow {
		return nil, fmt.Errorf("agent tool policy %s: %s (argument_hash=%s)", decision.Decision, decision.Reason, decision.Audit.ArgumentHash)
	}
	releaseID := stringArgument(input, "release_id")
	release, err := s.store.LoadKnowledgeRelease(releaseID)
	if err != nil {
		return nil, fmt.Errorf("load pinned release: %w", err)
	}
	allowedCitations := agentPackageReleaseCitationAllowlist(pkg, releaseID)

	switch name {
	case "agent.package_metadata":
		evaluation, err := s.store.LoadAgentPackageEvaluation(pkg.ContentHash)
		if err != nil {
			return nil, fmt.Errorf("load package evaluation: %w", err)
		}
		return marshalMCPResult(map[string]any{
			"package_id":        pkg.PackageID,
			"package_version":   pkg.Version,
			"package_hash":      pkg.ContentHash,
			"lifecycle_state":   pkg.LifecycleState,
			"release_id":        release.ReleaseID,
			"release_hash":      release.ContentHash,
			"retrieval_policy":  pkg.RetrievalPolicy,
			"safety_policy":     pkg.SafetyPolicy,
			"evaluation_status": map[string]any{"passed": evaluation.Passed, "suite_version": evaluation.SuiteVersion, "metrics": evaluation.Metrics},
			"ui_manifest":       pkg.UIManifest,
		})
	case "agent.search":
		limit, err := agentToolLimit(input["limit"], pkg.RetrievalPolicy.MaxContextChunks)
		if err != nil {
			return nil, err
		}
		results, searchErr := searchAgentReleaseClaimsWithStrategy(
			s.store, *release, stringArgument(input, "query"), limit, pkg.RetrievalPolicy,
		)
		if searchErr != nil {
			return nil, searchErr
		}
		return marshalMCPResult(filterAgentScopedSearchResults(results, allowedCitations, pkg.RetrievalPolicy.RequireCitations))
	case "agent.resolve_citation":
		citationID := stringArgument(input, "citation_id")
		if !allowedCitations[citationID] {
			return nil, fmt.Errorf("citation is outside the pinned package release allowlist: %s", citationID)
		}
		for _, citation := range release.Citations {
			if citation.CitationID == citationID {
				return marshalMCPResult(AgentScopedCitation{
					CitationID: citation.CitationID, BookID: citation.BookID,
					ChapterID: citation.ChapterID, ChunkID: citation.ChunkID,
					Anchor: citation.Anchor, Note: citation.Note,
					SourceType: citation.SourceType, PublishedAt: citation.PublishedAt,
				})
			}
		}
		return nil, fmt.Errorf("citation not found in pinned release: %s", citationID)
	case "agent.get_claim":
		if release.Analysis == nil {
			return nil, fmt.Errorf("pinned release has no claims")
		}
		claimID := stringArgument(input, "claim_id")
		for _, claim := range release.Analysis.Claims {
			if claim.ID == claimID {
				filtered := make([]string, 0, len(claim.CitationIDs))
				for _, citationID := range claim.CitationIDs {
					if allowedCitations[citationID] {
						filtered = append(filtered, citationID)
					}
				}
				if pkg.RetrievalPolicy.RequireCitations && len(filtered) == 0 {
					return nil, fmt.Errorf("claim citations are outside the pinned package release allowlist: %s", claimID)
				}
				claim.CitationIDs = filtered
				return marshalMCPResult(claim)
			}
		}
		return nil, fmt.Errorf("claim not found in pinned release: %s", claimID)
	default:
		return nil, fmt.Errorf("tool is outside the read-only Agent tool catalog: %s", name)
	}
}

func agentPackageReleaseCitationAllowlist(pkg AgentPackage, releaseID string) map[string]bool {
	for _, ref := range pkg.Releases {
		if ref.ReleaseID == releaseID {
			return stringBoolSet(ref.CitationIDs...)
		}
	}
	return map[string]bool{}
}

func filterAgentScopedSearchResults(results []AgentScopedSearchResult, allowed map[string]bool, requireCitations bool) []AgentScopedSearchResult {
	filteredResults := make([]AgentScopedSearchResult, 0, len(results))
	for _, result := range results {
		filteredCitations := make([]string, 0, len(result.CitationIDs))
		for _, citationID := range result.CitationIDs {
			if allowed[citationID] {
				filteredCitations = append(filteredCitations, citationID)
			}
		}
		if requireCitations && len(filteredCitations) == 0 {
			continue
		}
		result.CitationIDs = filteredCitations
		filteredResults = append(filteredResults, result)
	}
	return filteredResults
}

func searchAgentReleaseClaims(release KnowledgeRelease, query string, limit int) []AgentScopedSearchResult {
	results, _ := searchAgentReleaseClaimsWithStrategy(nil, release, query, limit, AgentPackageRetrievalPolicy{Strategy: "lexical"})
	return results
}

func searchAgentReleaseClaimsWithStrategy(
	store *BookKnowledgeStore,
	release KnowledgeRelease,
	query string,
	limit int,
	policy AgentPackageRetrievalPolicy,
) ([]AgentScopedSearchResult, error) {
	if release.Analysis == nil {
		return []AgentScopedSearchResult{}, nil
	}
	strategy := strings.TrimSpace(policy.Strategy)
	if strategy == "graph" {
		return nil, fmt.Errorf("graph retrieval is not connected for Agent Package execution")
	}
	if strategy != "lexical" && strategy != "vector" && strategy != "hybrid" {
		return nil, fmt.Errorf("unsupported Agent Package retrieval strategy %q", strategy)
	}
	terms := splitSearchTerms(query)
	semanticScores := make(map[string]float64)
	if strategy == "vector" || strategy == "hybrid" {
		if store == nil {
			return nil, fmt.Errorf("semantic embedder store is required for %s retrieval", strategy)
		}
		embedder, err := store.configuredAgentSemanticEmbedder(policy)
		if err != nil {
			return nil, err
		}
		index, err := store.loadOrBuildAgentSemanticVectorIndex(context.Background(), release, embedder)
		if err != nil {
			return nil, fmt.Errorf("build semantic vector index: %w", err)
		}
		queryVectors, err := embedder.Embed(context.Background(), []string{query})
		if err != nil {
			return nil, fmt.Errorf("embed semantic query: %w", err)
		}
		if len(queryVectors) != 1 {
			return nil, fmt.Errorf("semantic embedder returned %d query vectors", len(queryVectors))
		}
		if err := validateAgentSemanticVector(queryVectors[0]); err != nil {
			return nil, err
		}
		for _, record := range index.Vectors {
			semanticScores[record.ClaimID] = agentDenseVectorScore(queryVectors[0], record.Vector)
		}
	}
	results := make([]AgentScopedSearchResult, 0)
	for _, claim := range release.Analysis.Claims {
		haystack := strings.ToLower(strings.Join(append([]string{claim.Statement}, claim.Scope...), " "))
		matched := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				matched++
			}
		}
		lexicalScore := float64(matched) / float64(len(terms))
		vectorScore := semanticScores[claim.ID]
		score := lexicalScore
		if strategy == "vector" {
			score = vectorScore
		} else if strategy == "hybrid" {
			score = lexicalScore*0.4 + vectorScore*0.6
		}
		if score <= 0 {
			continue
		}
		if strategy == "vector" || strategy == "hybrid" {
			score = agentSemanticRerankScore(policy.RerankerVersion, query, haystack, score)
		}
		results = append(results, AgentScopedSearchResult{
			ClaimID: claim.ID, Statement: claim.Statement,
			CitationIDs: append([]string(nil), claim.CitationIDs...),
			Score:       score,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].ClaimID < results[j].ClaimID
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func agentSemanticRerankScore(version, query, document string, baseScore float64) float64 {
	if version != AgentSemanticRerankerVersion {
		return 0
	}
	queryTerms := splitSearchTerms(query)
	if len(queryTerms) == 0 {
		return baseScore
	}
	matched := 0
	for _, term := range queryTerms {
		if strings.Contains(document, term) {
			matched++
		}
	}
	coverage := float64(matched) / float64(len(queryTerms))
	phraseBonus := 0.0
	if strings.Contains(document, strings.ToLower(strings.TrimSpace(query))) {
		phraseBonus = 0.05
	}
	return baseScore*0.8 + coverage*0.15 + phraseBonus
}

func agentToolLimit(value any, packageLimit int) (int, error) {
	if packageLimit <= 0 {
		return 0, fmt.Errorf("retrieval_policy.max_context_chunks must be positive")
	}
	if value == nil {
		if packageLimit < 10 {
			return packageLimit, nil
		}
		return 10, nil
	}
	number, ok := value.(float64)
	if !ok || number < 1 || number > 50 || number != float64(int(number)) {
		return 0, fmt.Errorf("limit must be an integer between 1 and 50")
	}
	if int(number) > packageLimit {
		return 0, fmt.Errorf("limit exceeds retrieval_policy.max_context_chunks (%d)", packageLimit)
	}
	return int(number), nil
}

func marshalMCPResult(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func strictObjectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func mergeSchemaProperties(groups ...map[string]any) map[string]any {
	result := make(map[string]any)
	for _, group := range groups {
		for name, schema := range group {
			result[name] = schema
		}
	}
	return result
}
