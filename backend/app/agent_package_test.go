package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentPackageValidatesPinnedReleasePoliciesAndCapabilities(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg := validAgentPackage()

	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatalf("FinalizeAgentPackage() error = %v", err)
	}
	if !strings.HasPrefix(finalized.ContentHash, "sha256:") {
		t.Fatalf("content hash = %q", finalized.ContentHash)
	}
	if err := ValidateAgentPackage(finalized, store, AgentReadOnlyToolIDs()); err != nil {
		t.Fatalf("ValidateAgentPackage() error = %v", err)
	}

	reordered := pkg
	reordered.RetrievalPolicy.AllowedSourceTypes = []string{"wechat_mp_article", "dedao_ebook"}
	reordered.SafetyPolicy.AbstentionReasons = []string{"outside_scope", "insufficient_evidence"}
	reordered.UIManifest.Capabilities = []string{"evidence", "reader", "grounded_chat", "search"}
	reordered.ToolPolicy.Tools = []AgentPackageToolRule{
		{MCPServer: "book-mcp", ToolName: "agent.resolve_citation", Decision: AgentToolAllow},
		{MCPServer: "book-mcp", ToolName: "agent.search", Decision: AgentToolAllow},
	}
	reorderedFinalized, err := FinalizeAgentPackage(reordered)
	if err != nil {
		t.Fatalf("FinalizeAgentPackage(reordered) error = %v", err)
	}
	if reorderedFinalized.ContentHash != finalized.ContentHash {
		t.Fatalf("hash changed after reordering set-like policies: %q != %q", reorderedFinalized.ContentHash, finalized.ContentHash)
	}
}

func TestAgentPackageV2RequiresEvidencePolicyAndKeepsV1Compatible(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)

	v1, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(v1, store, AgentReadOnlyToolIDs()); err != nil {
		t.Fatalf("v1 package without evidence policy no longer validates: %v", err)
	}

	v2 := validAgentPackage()
	v2.SchemaVersion = AgentPackageSchemaVersionV2
	v2, err = FinalizeAgentPackage(v2)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(v2, store, AgentReadOnlyToolIDs()); err == nil ||
		!strings.Contains(err.Error(), "evidence_policy") {
		t.Fatalf("v2 package without evidence policy error = %v", err)
	}
}

func TestAgentPackageV2ValidatesEvidencePolicy(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	saveAgentPackageSupportingRelease(t, store)

	valid, err := FinalizeAgentPackage(validAgentPackageV2())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(valid, store, AgentReadOnlyToolIDs()); err != nil {
		t.Fatalf("valid v2 package error = %v", err)
	}

	tests := []struct {
		name string
		edit func(*AgentPackage)
		want string
	}{
		{
			name: "role references unpinned release",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.ReleaseRoles[1].ReleaseID = "release-unpinned"
			},
			want: "pinned",
		},
		{
			name: "role reference is duplicated",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.ReleaseRoles[1].ReleaseID = "release-1"
			},
			want: "duplicate",
		},
		{
			name: "pinned release has no role",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.ReleaseRoles = pkg.EvidencePolicy.ReleaseRoles[:1]
				pkg.EvidencePolicy.MinimumIndependentSources = 0
			},
			want: "role",
		},
		{
			name: "unsupported release role",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.ReleaseRoles[1].Role = "background"
			},
			want: "role",
		},
		{
			name: "primary release is not independent support",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.MinimumIndependentSources = 2
			},
			want: "independent supporting",
		},
		{
			name: "unsupported verdict",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.AllowedVerdicts = append(pkg.EvidencePolicy.AllowedVerdicts, "likely")
			},
			want: "verdict",
		},
		{
			name: "missing verdict",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.AllowedVerdicts = nil
			},
			want: "allowed_verdicts",
		},
		{
			name: "invalid max claims",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.MaxClaims = 0
			},
			want: "max_claims",
		},
		{
			name: "invalid max evidence per claim",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.MaxEvidencePerClaim = 0
			},
			want: "max_evidence_per_claim",
		},
		{
			name: "invalid independent source minimum",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.MinimumIndependentSources = -1
			},
			want: "minimum_independent_sources",
		},
		{
			name: "invalid freshness policy",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.FreshnessPolicy.MaxAgeDays = 0
			},
			want: "freshness_policy.max_age_days",
		},
		{
			name: "invalid report schema",
			edit: func(pkg *AgentPackage) {
				pkg.EvidencePolicy.ReportSchema = "freeform"
			},
			want: "report_schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := validAgentPackageV2()
			tt.edit(&pkg)
			finalized, err := FinalizeAgentPackage(pkg)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateAgentPackage(finalized, store, AgentReadOnlyToolIDs())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateAgentPackage() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAgentPackageHashBindsEvidencePolicy(t *testing.T) {
	base, err := FinalizeAgentPackage(validAgentPackageV2())
	if err != nil {
		t.Fatal(err)
	}

	mutations := []func(*AgentPackage){
		func(pkg *AgentPackage) { pkg.EvidencePolicy.MaxClaims++ },
		func(pkg *AgentPackage) { pkg.EvidencePolicy.MaxEvidencePerClaim++ },
		func(pkg *AgentPackage) { pkg.EvidencePolicy.MinimumIndependentSources = 0 },
		func(pkg *AgentPackage) { pkg.EvidencePolicy.FreshnessPolicy.MaxAgeDays++ },
		func(pkg *AgentPackage) { pkg.EvidencePolicy.FreshnessPolicy.RequirePublicationDate = false },
		func(pkg *AgentPackage) { pkg.EvidencePolicy.ReportSchema = "evidence-audit.v2" },
	}
	for index, mutate := range mutations {
		changed := validAgentPackageV2()
		mutate(&changed)
		changed, err = FinalizeAgentPackage(changed)
		if err != nil {
			t.Fatalf("mutation %d: %v", index, err)
		}
		if changed.ContentHash == base.ContentHash {
			t.Fatalf("evidence policy mutation %d did not change package hash", index)
		}
	}

	reordered := validAgentPackageV2()
	reordered.EvidencePolicy.ReleaseRoles[0], reordered.EvidencePolicy.ReleaseRoles[1] =
		reordered.EvidencePolicy.ReleaseRoles[1], reordered.EvidencePolicy.ReleaseRoles[0]
	reordered.EvidencePolicy.AllowedVerdicts = []string{
		AgentEvidenceVerdictInsufficient,
		AgentEvidenceVerdictMixed,
		AgentEvidenceVerdictContradicted,
		AgentEvidenceVerdictSupported,
	}
	reordered, err = FinalizeAgentPackage(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.ContentHash != base.ContentHash {
		t.Fatalf("set-like evidence policy ordering changed hash: %q != %q", reordered.ContentHash, base.ContentHash)
	}
}

func TestAgentPackageHashPreservesRuntimeSignificantOrder(t *testing.T) {
	pkg := validAgentPackage()
	pkg.ModelPolicy.Fallbacks = []string{"qwen-plus", "qwen-max"}
	pkg.PromptProfiles = []AgentPackagePromptProfile{
		{ProfileID: "primary", OutputSchema: "answer.v1"},
		{ProfileID: "fallback", OutputSchema: "answer.v1"},
	}
	ordered, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}

	reorderedInput := pkg
	reorderedInput.ModelPolicy.Fallbacks = []string{"qwen-max", "qwen-plus"}
	reorderedInput.PromptProfiles = []AgentPackagePromptProfile{pkg.PromptProfiles[1], pkg.PromptProfiles[0]}
	reordered, err := FinalizeAgentPackage(reorderedInput)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.ContentHash == ordered.ContentHash {
		t.Fatalf("runtime-significant order produced identical hash %q", ordered.ContentHash)
	}
}

func TestAgentPackageHashBindsSemanticRetrievalIdentity(t *testing.T) {
	base, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*AgentPackage){
		func(pkg *AgentPackage) { pkg.RetrievalPolicy.EmbeddingProvider = "other-provider" },
		func(pkg *AgentPackage) { pkg.RetrievalPolicy.EmbeddingModel = "other-model" },
		func(pkg *AgentPackage) { pkg.RetrievalPolicy.EmbeddingVersion = "v2" },
		func(pkg *AgentPackage) {
			pkg.RetrievalPolicy.EmbeddingEndpointHash = "sha256:" + strings.Repeat("9", 64)
		},
		func(pkg *AgentPackage) { pkg.RetrievalPolicy.RerankerVersion = "semantic-reranker.v2" },
	}
	for index, mutate := range mutations {
		changed := validAgentPackage()
		mutate(&changed)
		changed, err = FinalizeAgentPackage(changed)
		if err != nil {
			t.Fatalf("mutation %d: %v", index, err)
		}
		if changed.ContentHash == base.ContentHash {
			t.Fatalf("semantic retrieval identity mutation %d did not change package hash", index)
		}
	}
}

func TestAgentPackageRejectsCrossReleaseCitationIDCollisions(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	first := agentPackageTestRelease()
	if err := store.saveKnowledgeRelease(first); err != nil {
		t.Fatal(err)
	}
	second := agentPackageTestRelease()
	second.ReleaseID = "release-2"
	second.BookID = "book-2"
	second.ContentHash = "sha256:release-content-2"
	second.Book = BookKnowledgeBook{BookID: "book-2", Title: "Second Book", SourceType: "dedao_ebook"}
	second.Citations[0].BookID = "book-2"
	second.Citations[0].ChunkID = "chunk-2"
	if err := store.saveKnowledgeRelease(second); err != nil {
		t.Fatal(err)
	}
	pkg := validAgentPackage()
	pkg.Releases = append(pkg.Releases, AgentPackageReleaseRef{
		ReleaseID:   second.ReleaseID,
		ContentHash: second.ContentHash,
		CitationIDs: []string{"citation-1"},
	})
	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(finalized, store, AgentReadOnlyToolIDs()); err == nil ||
		!strings.Contains(err.Error(), "citation") || !strings.Contains(err.Error(), "multiple releases") {
		t.Fatalf("cross-release citation collision error = %v", err)
	}
}

func TestAgentPackageRejectsDuplicateCitationIDsInsidePinnedRelease(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentPackageTestRelease()
	release.Citations = append(release.Citations, release.Citations[0])
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateAgentPackage(pkg, store, AgentReadOnlyToolIDs())
	if err == nil || !strings.Contains(err.Error(), "duplicate citation") {
		t.Fatalf("duplicate release citation error = %v", err)
	}
}

func TestAgentPackageRejectsInvalidOrMutableReferences(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	knownTools := AgentReadOnlyToolIDs()

	tests := []struct {
		name string
		edit func(*AgentPackage)
		want string
	}{
		{name: "missing package identity", edit: func(pkg *AgentPackage) { pkg.PackageID = "" }, want: "package_id"},
		{name: "missing pinned release", edit: func(pkg *AgentPackage) { pkg.Releases = nil }, want: "releases"},
		{name: "mutable release reference", edit: func(pkg *AgentPackage) { pkg.Releases[0].ContentHash = "" }, want: "content_hash"},
		{name: "unpublished release", edit: func(pkg *AgentPackage) { pkg.Releases[0].ReleaseID = "release-missing" }, want: "published release"},
		{name: "release hash mismatch", edit: func(pkg *AgentPackage) { pkg.Releases[0].ContentHash = "sha256:changed" }, want: "content hash"},
		{name: "missing citation", edit: func(pkg *AgentPackage) { pkg.Releases[0].CitationIDs = []string{"citation-missing"} }, want: "citation"},
		{name: "missing retrieval policy", edit: func(pkg *AgentPackage) { pkg.RetrievalPolicy.Strategy = "" }, want: "retrieval_policy.strategy"},
		{name: "missing model policy", edit: func(pkg *AgentPackage) { pkg.ModelPolicy.PreferredCapability = "" }, want: "model_policy.preferred_capability"},
		{name: "unknown tool", edit: func(pkg *AgentPackage) { pkg.ToolPolicy.Tools[0].ToolName = "delete_source" }, want: "unknown tool"},
		{name: "missing safety policy", edit: func(pkg *AgentPackage) { pkg.SafetyPolicy.AbstentionReasons = nil }, want: "abstention"},
		{name: "missing evaluation threshold", edit: func(pkg *AgentPackage) { pkg.EvaluationPolicy.MinimumScores = nil }, want: "minimum_scores"},
		{name: "missing behavioral evaluation threshold", edit: func(pkg *AgentPackage) { delete(pkg.EvaluationPolicy.MinimumScores, "cost") }, want: "required evaluation metric"},
		{name: "invalid evaluation threshold", edit: func(pkg *AgentPackage) { pkg.EvaluationPolicy.MinimumScores["faithfulness"] = 1.1 }, want: "between 0 and 1"},
		{name: "zero required evaluation threshold", edit: func(pkg *AgentPackage) { pkg.EvaluationPolicy.MinimumScores["citations"] = 0 }, want: "greater than zero"},
		{name: "unknown ui capability", edit: func(pkg *AgentPackage) {
			pkg.UIManifest.Capabilities = append(pkg.UIManifest.Capabilities, "private_fork")
		}, want: "ui capability"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := validAgentPackage()
			tt.edit(&pkg)
			finalized, err := FinalizeAgentPackage(pkg)
			if err != nil {
				if !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("FinalizeAgentPackage() error = %v, want %q", err, tt.want)
				}
				return
			}
			err = ValidateAgentPackage(finalized, store, knownTools)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateAgentPackage() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAgentPackageRejectsUsagePolicyDowngrade(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentPackageTestRelease()
	release.UsagePolicy = BookUsageEvidenceOnly
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(pkg, store, AgentReadOnlyToolIDs()); err == nil || !strings.Contains(err.Error(), "usage policy") {
		t.Fatalf("usage policy downgrade error = %v", err)
	}
}

func TestAgentPackageRejectsMissingSourceIdentity(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentPackageTestRelease()
	release.Book.SourceType = ""
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(pkg, store, AgentReadOnlyToolIDs()); err == nil || !strings.Contains(err.Error(), "source type is required") {
		t.Fatalf("missing source identity error = %v", err)
	}
}

func TestAgentPackageRejectsNonURLSafePackageID(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg := validAgentPackage()
	pkg.PackageID = "agent/package"
	pkg, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(pkg, store, AgentReadOnlyToolIDs()); err == nil || !strings.Contains(err.Error(), "package_id") {
		t.Fatalf("unsafe package_id error = %v", err)
	}
}

func TestAgentPackageHashDetectsPolicyChangesAndIgnoresLifecycleTimestamps(t *testing.T) {
	pkg := validAgentPackage()
	first, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	pkg.CreatedAt = "2026-07-20T00:00:00Z"
	pkg.PublishedAt = "2026-07-21T00:00:00Z"
	second, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash != second.ContentHash {
		t.Fatalf("operational timestamps changed content hash: %q != %q", first.ContentHash, second.ContentHash)
	}
	pkg.EvaluationPolicy.MinimumScores["faithfulness"] = 0.95
	third, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if third.ContentHash == first.ContentHash {
		t.Fatal("evaluation policy change did not change content hash")
	}
}

func TestAgentPackageSchemaIsValidJSONAndRequiresContractSections(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "agent-package-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("schema required is missing")
	}
	joined := strings.Join(anyStrings(required), ",")
	for _, field := range []string{"schema_version", "package_id", "version", "content_hash", "lifecycle_state", "releases", "retrieval_policy", "model_policy", "tool_policy", "safety_policy", "evaluation_policy", "ui_manifest"} {
		if !strings.Contains(joined, field) {
			t.Fatalf("schema does not require %q: %s", field, joined)
		}
	}
}

func validAgentPackage() AgentPackage {
	return AgentPackage{
		SchemaVersion:  AgentPackageSchemaVersion,
		PackageID:      "agent-package-example",
		Version:        "1.0.0",
		LifecycleState: AgentPackageDraft,
		Releases: []AgentPackageReleaseRef{{
			ReleaseID:   "release-1",
			ContentHash: "sha256:release-content",
			CitationIDs: []string{"citation-1"},
		}},
		RetrievalPolicy: AgentPackageRetrievalPolicy{
			Strategy:              "hybrid",
			AllowedSourceTypes:    []string{"dedao_ebook", "wechat_mp_article"},
			RequireCitations:      true,
			MaxContextChunks:      8,
			EmbeddingProvider:     "fixture",
			EmbeddingModel:        "semantic",
			EmbeddingVersion:      "v1",
			EmbeddingEndpointHash: sha256Fingerprint([]byte("https://embedding.test.invalid/v1")),
			RerankerVersion:       AgentSemanticRerankerVersion,
		},
		ModelPolicy: AgentPackageModelPolicy{
			PreferredCapability: "reasoning",
			Fallbacks:           []string{"qwen-plus", "qwen-max"},
			MaxCostUSD:          0.25,
			TimeoutMS:           30000,
		},
		PromptProfiles: []AgentPackagePromptProfile{{
			ProfileID:    "grounded-answer.v1",
			OutputSchema: "grounded-answer.v1",
		}},
		ToolPolicy: AgentPackageToolPolicy{Tools: []AgentPackageToolRule{
			{MCPServer: "book-mcp", ToolName: "agent.search", Decision: AgentToolAllow},
			{MCPServer: "book-mcp", ToolName: "agent.resolve_citation", Decision: AgentToolAllow},
		}},
		SafetyPolicy: AgentPackageSafetyPolicy{
			UsagePolicy:       BookUsageStandard,
			AbstentionReasons: []string{"insufficient_evidence", "outside_scope"},
			EscalationTarget:  "human_review",
		},
		EvaluationPolicy: AgentPackageEvaluationPolicy{
			SuiteVersion: "book-agent-v1",
			MinimumScores: map[string]float64{
				"retrieval":           0.8,
				"retrieval_precision": 0.8,
				"citations":           1,
				"faithfulness":        0.9,
				"abstention":          1,
				"tool_choice":         1,
				"tool_arguments":      1,
				"task_completion":     1,
				"latency":             1,
				"cost":                1,
			},
		},
		UIManifest: AgentPackageUIManifest{Capabilities: []string{"reader", "search", "grounded_chat", "evidence"}},
	}
}

func validAgentPackageV2() AgentPackage {
	pkg := validAgentPackage()
	pkg.SchemaVersion = AgentPackageSchemaVersionV2
	pkg.Version = "2.0.0"
	pkg.Releases = append(pkg.Releases, AgentPackageReleaseRef{
		ReleaseID:   "release-2",
		ContentHash: "sha256:supporting-release-content",
		CitationIDs: []string{"citation-2"},
	})
	pkg.EvidencePolicy = &AgentPackageEvidencePolicy{
		ReleaseRoles: []AgentPackageEvidenceReleaseRole{
			{ReleaseID: "release-1", Role: AgentEvidenceReleasePrimary},
			{ReleaseID: "release-2", Role: AgentEvidenceReleaseSupporting},
		},
		MinimumIndependentSources: 1,
		MaxClaims:                 20,
		MaxEvidencePerClaim:       8,
		AllowedVerdicts: []string{
			AgentEvidenceVerdictSupported,
			AgentEvidenceVerdictContradicted,
			AgentEvidenceVerdictMixed,
			AgentEvidenceVerdictInsufficient,
		},
		FreshnessPolicy: AgentPackageEvidenceFreshnessPolicy{
			MaxAgeDays:             365,
			RequirePublicationDate: true,
		},
		ReportSchema: AgentEvidenceReportSchemaV1,
	}
	return pkg
}

func saveAgentPackageTestRelease(t *testing.T, store *BookKnowledgeStore) {
	t.Helper()
	store.SetAgentSemanticEmbedder(&fakeAgentSemanticEmbedder{})
	if err := store.saveKnowledgeRelease(agentPackageTestRelease()); err != nil {
		t.Fatal(err)
	}
}

func saveAgentPackageSupportingRelease(t *testing.T, store *BookKnowledgeStore) {
	t.Helper()
	release := agentPackageTestRelease()
	release.ReleaseID = "release-2"
	release.BookID = "book-2"
	release.ContentHash = "sha256:supporting-release-content"
	release.Book = BookKnowledgeBook{
		BookID:     "book-2",
		Title:      "Synthetic Supporting Source",
		SourceType: "wechat_mp_article",
	}
	release.Analysis.Claims[0].CitationIDs = []string{"citation-2"}
	release.Citations = []BookKnowledgeCitation{{
		CitationID: "citation-2",
		BookID:     "book-2",
		ChunkID:    "chunk-2",
	}}
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
}

func agentPackageTestRelease() KnowledgeRelease {
	return KnowledgeRelease{
		SchemaVersion: KnowledgeReleaseSchemaVersion,
		Version:       "1",
		ReleaseID:     "release-1",
		BookID:        "book-1",
		ContentHash:   "sha256:release-content",
		UsagePolicy:   BookUsageStandard,
		Book:          BookKnowledgeBook{BookID: "book-1", Title: "Synthetic Book", SourceType: "dedao_ebook"},
		Analysis: &BookAnalysisPayload{
			Summary: "Synthetic summary",
			Claims: []BookAnalysisClaim{{
				ID: "claim-1", Statement: "Synthetic grounded statement", CitationIDs: []string{"citation-1"}, Confidence: 1, RiskLevel: "low",
			}},
		},
		Quality:   BookQualityReport{Decision: BookQualityPass},
		Citations: []BookKnowledgeCitation{{CitationID: "citation-1", BookID: "book-1", ChunkID: "chunk-1"}},
		CreatedAt: "2026-07-19T00:00:00Z",
	}
}

func anyStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
