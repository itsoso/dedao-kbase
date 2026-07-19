package app

import (
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
		return marshalMCPResult(searchAgentReleaseClaims(*release, stringArgument(input, "query"), limit))
	case "agent.resolve_citation":
		citationID := stringArgument(input, "citation_id")
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
				return marshalMCPResult(claim)
			}
		}
		return nil, fmt.Errorf("claim not found in pinned release: %s", claimID)
	default:
		return nil, fmt.Errorf("tool is outside the read-only Agent tool catalog: %s", name)
	}
}

func searchAgentReleaseClaims(release KnowledgeRelease, query string, limit int) []AgentScopedSearchResult {
	if release.Analysis == nil {
		return []AgentScopedSearchResult{}
	}
	terms := splitSearchTerms(query)
	results := make([]AgentScopedSearchResult, 0)
	for _, claim := range release.Analysis.Claims {
		haystack := strings.ToLower(strings.Join(append([]string{claim.Statement}, claim.Scope...), " "))
		matched := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				matched++
			}
		}
		if matched == 0 {
			continue
		}
		results = append(results, AgentScopedSearchResult{
			ClaimID: claim.ID, Statement: claim.Statement,
			CitationIDs: append([]string(nil), claim.CitationIDs...),
			Score:       float64(matched) / float64(len(terms)),
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
	return results
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
