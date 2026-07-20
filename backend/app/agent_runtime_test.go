package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAgentPackageRuntimeSearchesEveryPinnedRelease(t *testing.T) {
	store, pkg := agentRuntimeTestStore(t)
	response, err := SearchAgentPackage(store, AgentPackageSearchRequest{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version,
		Query: "grounded", Limit: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.PackageVersion != pkg.Version || len(response.Results) != 2 {
		t.Fatalf("search response = %#v", response)
	}
	if response.Results[0].ReleaseID == response.Results[1].ReleaseID {
		t.Fatalf("search did not cover both releases: %#v", response.Results)
	}
	for _, result := range response.Results {
		if result.ClaimID == "" || len(result.CitationIDs) == 0 || result.ReleaseID == "" {
			t.Fatalf("search result lost evidence identity: %#v", result)
		}
	}
}

func TestAgentPackageRuntimeRejectsClaimCitationsOutsideReleaseReferenceAllowlist(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := validAgentPackage()
	pkg.RetrievalPolicy.Strategy = "lexical"
	pkg.Releases = nil
	for index, releaseID := range []string{"release-1", "release-2"} {
		release := agentPackageTestRelease()
		release.ReleaseID = releaseID
		release.ContentHash = "sha256:" + strings.Repeat(string(rune('1'+index)), 64)
		uniqueCitationID := "unique-citation-" + releaseID
		release.Citations = []BookKnowledgeCitation{
			{CitationID: uniqueCitationID, BookID: release.BookID, ChunkID: "unique-chunk-" + releaseID},
			{CitationID: "shared-citation", BookID: release.BookID, ChunkID: "shared-chunk-" + releaseID},
		}
		release.Analysis.Claims[0].CitationIDs = []string{"shared-citation"}
		if err := store.saveKnowledgeRelease(release); err != nil {
			t.Fatal(err)
		}
		pkg.Releases = append(pkg.Releases, AgentPackageReleaseRef{
			ReleaseID: releaseID, ContentHash: release.ContentHash, CitationIDs: []string{uniqueCitationID},
		})
	}
	pkg, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackage(pkg, store, AgentReadOnlyToolIDs()); err != nil {
		t.Fatalf("unique release refs should validate: %v", err)
	}

	response, err := searchAgentPackageEvidence(store, pkg, "grounded", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 0 {
		t.Fatalf("runtime exposed citations outside release ref allowlists: %#v", response.Results)
	}
}

func TestAgentPackageRuntimeEnforcesPackageSearchLimit(t *testing.T) {
	store, pkg := agentRuntimeTestStoreWithLimit(t, 1)
	_, err := SearchAgentPackage(store, AgentPackageSearchRequest{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version, Query: "grounded", Limit: 2,
	})
	if err == nil || !strings.Contains(err.Error(), "max_context_chunks") {
		t.Fatalf("search limit error = %v", err)
	}
}

func TestAgentPackageRuntimeExecutesDeclaredRetrievalStrategy(t *testing.T) {
	strategies := []string{"lexical", "vector", "hybrid"}
	for _, strategy := range strategies {
		store, pkg := agentRuntimeTestStoreWithPackageEdit(t, func(pkg *AgentPackage) {
			pkg.RetrievalPolicy.Strategy = strategy
		})
		response, err := SearchAgentPackage(store, AgentPackageSearchRequest{
			PackageID: pkg.PackageID, PackageVersion: pkg.Version, Query: "second grounded", Limit: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		if response.RetrievalStrategy != strategy || len(response.Results) == 0 || response.Results[0].ClaimID != "claim-2" {
			t.Fatalf("%s response = %#v", strategy, response)
		}
		if response.Results[0].Score <= 0 || response.Results[0].Score > 1 {
			t.Fatalf("%s score = %v", strategy, response.Results[0].Score)
		}
	}
}

func TestAgentPackageVectorRetrievalUsesSemanticEmbeddingIndexAndReranker(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	embedder := &fakeAgentSemanticEmbedder{}
	store.SetAgentSemanticEmbedder(embedder)
	release := agentPackageTestRelease()
	release.ContentHash = "sha256:" + strings.Repeat("a", 64)
	release.Analysis.Claims = []BookAnalysisClaim{
		{ID: "claim-heart", Statement: "Heart health improves with steady movement.", CitationIDs: []string{"citation-1"}},
		{ID: "claim-finance", Statement: "Portfolio allocation affects investment risk.", CitationIDs: []string{"citation-2"}},
	}

	vectorPolicy := validAgentPackage().RetrievalPolicy
	vectorPolicy.Strategy = "vector"
	results, err := searchAgentReleaseClaimsWithStrategy(store, release, "cardiac wellness", 2, vectorPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ClaimID != "claim-heart" {
		t.Fatalf("semantic vector results = %#v", results)
	}
	if embedder.documentInputs != 2 || embedder.queryInputs != 1 {
		t.Fatalf("embedding calls after index build = %#v", embedder)
	}

	reloaded := NewBookKnowledgeStore(root)
	reloadedEmbedder := &fakeAgentSemanticEmbedder{}
	reloaded.SetAgentSemanticEmbedder(reloadedEmbedder)
	if _, err := searchAgentReleaseClaimsWithStrategy(reloaded, release, "cardiac wellness", 2, validAgentPackage().RetrievalPolicy); err != nil {
		t.Fatal(err)
	}
	if reloadedEmbedder.documentInputs != 0 || reloadedEmbedder.queryInputs != 1 {
		t.Fatalf("persisted vector index was not reused: %#v", reloadedEmbedder)
	}
}

func TestAgentPackageSemanticRetrievalFailsClosedWithoutConfiguredEmbedder(t *testing.T) {
	t.Setenv("KBASE_EMBEDDING_BASE_URL", "")
	t.Setenv("KBASE_EMBEDDING_MODEL", "")
	store := NewBookKnowledgeStore(t.TempDir())
	_, err := searchAgentReleaseClaimsWithStrategy(store, agentPackageTestRelease(), "grounded", 2, validAgentPackage().RetrievalPolicy)
	if err == nil || !strings.Contains(err.Error(), "semantic embedder") {
		t.Fatalf("missing semantic embedder error = %v", err)
	}
}

func TestAgentPackageSemanticRetrievalRejectsEmbedderIdentityMismatch(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	store.SetAgentSemanticEmbedder(&fakeAgentSemanticEmbedder{identity: "other:model:v9:endpoint"})
	_, err := searchAgentReleaseClaimsWithStrategy(
		store, agentPackageTestRelease(), "grounded", 2, validAgentPackage().RetrievalPolicy,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match package policy") {
		t.Fatalf("embedder identity mismatch error = %v", err)
	}
}

func TestConfiguredSemanticEmbedderBindsEndpointFingerprintToPackagePolicy(t *testing.T) {
	baseURL := "https://embedding.test.invalid/v1"
	t.Setenv("KBASE_EMBEDDING_BASE_URL", baseURL)
	t.Setenv("KBASE_EMBEDDING_PROVIDER", "fixture")
	t.Setenv("KBASE_EMBEDDING_MODEL", "semantic")
	t.Setenv("KBASE_EMBEDDING_VERSION", "v1")
	t.Setenv("KBASE_EMBEDDING_API_KEY", "synthetic-test-key")
	store := NewBookKnowledgeStore(t.TempDir())
	policy := validAgentPackage().RetrievalPolicy
	embedder, err := store.configuredAgentSemanticEmbedder(policy)
	if err != nil {
		t.Fatal(err)
	}
	if embedder.Identity() != agentPackageSemanticEmbedderIdentity(policy) {
		t.Fatalf("configured embedder identity = %q", embedder.Identity())
	}

	t.Setenv("KBASE_EMBEDDING_BASE_URL", "https://different.test.invalid/v1")
	if _, err := store.configuredAgentSemanticEmbedder(policy); err == nil || !strings.Contains(err.Error(), "does not match package policy") {
		t.Fatalf("changed endpoint identity error = %v", err)
	}
}

type fakeAgentSemanticEmbedder struct {
	documentInputs int
	queryInputs    int
	identity       string
}

func (e *fakeAgentSemanticEmbedder) Identity() string {
	if e.identity != "" {
		return e.identity
	}
	return agentPackageSemanticEmbedderIdentity(validAgentPackage().RetrievalPolicy)
}

func (e *fakeAgentSemanticEmbedder) Embed(_ context.Context, inputs []string) ([][]float64, error) {
	vectors := make([][]float64, 0, len(inputs))
	for _, input := range inputs {
		lower := strings.ToLower(input)
		if strings.Contains(lower, "heart") || strings.Contains(lower, "cardiac") || strings.Contains(lower, "grounded") {
			vectors = append(vectors, []float64{1, 0})
		} else {
			vectors = append(vectors, []float64{0, 1})
		}
	}
	if len(inputs) > 1 {
		e.documentInputs += len(inputs)
	} else {
		e.queryInputs += len(inputs)
	}
	return vectors, nil
}

func TestAgentPackageRuntimeFailsClosedForUnavailableGraphRetrieval(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg := validAgentPackage()
	pkg.RetrievalPolicy.Strategy = "graph"
	pkg, _ = FinalizeAgentPackage(pkg)
	_, err := EvaluateAgentPackageDeterministically(store, pkg, loadAgentEvaluationFixture(t), testAgentPackageTime())
	if err == nil || !strings.Contains(err.Error(), "graph") {
		t.Fatalf("graph evaluation error = %v", err)
	}
}

func TestAgentPackageRuntimeEnforcesCostBudgetBeforeModelCall(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "synthetic-test-key")
	store, pkg := agentRuntimeTestStore(t)
	client := &fakeBookKnowledgeLLMClient{answer: "must not be called [citation:citation-1]"}
	_, err := ChatAgentPackageWithClient(context.Background(), store, AgentPackageChatRequest{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version, Question: strings.Repeat("grounded ", 4000),
	}, client)
	if err == nil || !strings.Contains(err.Error(), "cost budget") || len(client.messages) != 0 {
		t.Fatalf("cost budget error=%v messages=%#v", err, client.messages)
	}
}

func TestAgentPackageRuntimeRejectsSupersededVersion(t *testing.T) {
	store, v1 := agentRuntimeTestStore(t)
	v2 := v1
	v2.Version = "2.0.0"
	v2, _ = FinalizeAgentPackage(v2)
	savePassingAgentPackageTestEvaluation(t, store, v2)
	if _, _, err := PublishAgentPackage(store, v2, "runtime-package-v2", AgentReadOnlyToolIDs(), testAgentPackageTime().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	_, err := SearchAgentPackage(store, AgentPackageSearchRequest{
		PackageID: v1.PackageID, PackageVersion: v1.Version, Query: "grounded",
	})
	if err == nil || !strings.Contains(err.Error(), "not published") {
		t.Fatalf("superseded package search error = %v", err)
	}
}

func TestAgentPackageRuntimeChatUsesPinnedEvidencePolicyAndCitations(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "synthetic-test-key")
	store, pkg := agentRuntimeTestStore(t)
	client := &fakeBookKnowledgeLLMClient{answer: "Grounded answer [citation:citation-1] [citation:citation-2]"}
	response, err := ChatAgentPackageWithClient(context.Background(), store, AgentPackageChatRequest{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version,
		Question: "What is grounded?",
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != AgentTraceOutcomeCompleted || response.PackageVersion != pkg.Version ||
		response.Model != pkg.ModelPolicy.Fallbacks[0] || len(response.Evidence) != 2 || len(response.Citations) != 2 ||
		response.TraceID == "" {
		t.Fatalf("chat response = %#v", response)
	}
	trace, err := store.LoadAgentTrace(response.TraceID)
	if err != nil {
		t.Fatalf("load runtime trace: %v", err)
	}
	if trace.Package.Version != pkg.Version || len(trace.Releases) != 2 || len(trace.Retrievals) != 2 ||
		trace.Final.Outcome != AgentTraceOutcomeCompleted || len(trace.Final.Citations) != 2 {
		t.Fatalf("runtime trace = %#v", trace)
	}
	prompt := client.messages[len(client.messages)-1].Content
	for _, marker := range []string{"release-1", "release-2", "claim-1", "claim-2", "citation-1", "citation-2"} {
		if !strings.Contains(prompt, marker) {
			t.Fatalf("package prompt missing %q: %s", marker, prompt)
		}
	}
	if strings.Contains(prompt, "private-source-marker") {
		t.Fatalf("package prompt leaked source body/path marker: %s", prompt)
	}
}

func TestAgentPackageRuntimeAbstainsForMissingOrUnknownAnswerCitations(t *testing.T) {
	for _, answer := range []string{
		"Ungrounded answer without a citation marker",
		"Invented evidence [citation:not-pinned]",
	} {
		t.Run(answer, func(t *testing.T) {
			t.Setenv("DEDAO_TOKENPLAN_API_KEY", "synthetic-test-key")
			store, pkg := agentRuntimeTestStore(t)
			client := &fakeBookKnowledgeLLMClient{answer: answer}
			response, err := ChatAgentPackageWithClient(context.Background(), store, AgentPackageChatRequest{
				PackageID: pkg.PackageID, PackageVersion: pkg.Version, Question: "What is grounded?",
			}, client)
			if err != nil {
				t.Fatal(err)
			}
			if response.Outcome != AgentTraceOutcomeAbstained || response.AbstentionReason != "citation_required" ||
				response.Answer != "" || len(response.Citations) != 0 || response.TraceID == "" {
				t.Fatalf("ungrounded response = %#v", response)
			}
			trace, loadErr := store.LoadAgentTrace(response.TraceID)
			if loadErr != nil || trace.Final.Outcome != AgentTraceOutcomeAbstained || len(trace.Final.Citations) != 0 {
				t.Fatalf("ungrounded trace = %#v err=%v", trace, loadErr)
			}
		})
	}
}

func TestAgentPackageRuntimeReturnsOnlyCitationsUsedInAnswer(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "synthetic-test-key")
	store, pkg := agentRuntimeTestStore(t)
	client := &fakeBookKnowledgeLLMClient{answer: "Grounded answer [citation:citation-2]"}
	response, err := ChatAgentPackageWithClient(context.Background(), store, AgentPackageChatRequest{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version, Question: "What is grounded?",
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != AgentTraceOutcomeCompleted || len(response.Citations) != 1 ||
		response.Citations[0].CitationID != "citation-2" {
		t.Fatalf("answer citations = %#v", response)
	}
	trace, loadErr := store.LoadAgentTrace(response.TraceID)
	if loadErr != nil || len(trace.Final.Citations) != 1 || trace.Final.Citations[0].CitationID != "citation-2" {
		t.Fatalf("answer trace = %#v err=%v", trace, loadErr)
	}
}

func TestAgentPackageRuntimeAbstainsWithoutGroundedEvidence(t *testing.T) {
	store, pkg := agentRuntimeTestStore(t)
	client := &fakeBookKnowledgeLLMClient{answer: "must not be called"}
	response, err := ChatAgentPackageWithClient(context.Background(), store, AgentPackageChatRequest{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version,
		Question: "unmatched-token",
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != AgentTraceOutcomeAbstained || response.AbstentionReason != "insufficient_evidence" || len(client.messages) != 0 {
		t.Fatalf("abstention response=%#v messages=%#v", response, client.messages)
	}
	trace, err := store.LoadAgentTrace(response.TraceID)
	if err != nil || trace.Final.Outcome != AgentTraceOutcomeAbstained || len(trace.Retrievals) != 0 {
		t.Fatalf("abstention trace=%#v err=%v", trace, err)
	}
}

func TestAgentPackageRuntimePersistsFailedModelCall(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "synthetic-test-key")
	store, pkg := agentRuntimeTestStore(t)
	client := &fakeBookKnowledgeLLMClient{err: errors.New("synthetic model failure")}
	_, err := ChatAgentPackageWithClient(context.Background(), store, AgentPackageChatRequest{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version, Question: "What is grounded?",
	}, client)
	if err == nil || !strings.Contains(err.Error(), "synthetic model failure") {
		t.Fatalf("chat error = %v", err)
	}
	entries, readErr := os.ReadDir(store.AgentTraceDir())
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("failed trace entries=%#v err=%v", entries, readErr)
	}
	traceID := strings.TrimSuffix(entries[0].Name(), ".json")
	trace, loadErr := store.LoadAgentTrace(traceID)
	if loadErr != nil || trace.Final.Outcome != AgentTraceOutcomeFailed || len(trace.Retrievals) != 2 {
		t.Fatalf("failed trace=%#v err=%v", trace, loadErr)
	}
}

func TestKBaseHTTPHandlerRunsVersionedAgentPackage(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "synthetic-test-key")
	store, pkg := agentRuntimeTestStore(t)
	client := &fakeBookKnowledgeLLMClient{answer: "Grounded answer [citation:citation-1] [citation:citation-2]"}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-token", ChatClient: client,
	})

	search := requestKBase(handler, http.MethodGet,
		"/api/agent-packages/"+pkg.PackageID+"/search?version="+pkg.Version+"&q=grounded&limit=2",
		"consumer-token")
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), `"release_id":"release-1"`) ||
		!strings.Contains(search.Body.String(), `"release_id":"release-2"`) {
		t.Fatalf("package search status=%d body=%s", search.Code, search.Body.String())
	}

	chat := requestJSONKBase(handler, http.MethodPost,
		"/api/agent-packages/"+pkg.PackageID+"/chat?version="+pkg.Version,
		"consumer-token", `{"question":"What is grounded?"}`)
	if chat.Code != http.StatusOK || !strings.Contains(chat.Body.String(), `"package_version":"1.0.0"`) ||
		!strings.Contains(chat.Body.String(), `"citation_id":"citation-1"`) ||
		!strings.Contains(chat.Body.String(), `"citation_id":"citation-2"`) {
		t.Fatalf("package chat status=%d body=%s", chat.Code, chat.Body.String())
	}

	unversioned := requestKBase(handler, http.MethodGet,
		"/api/agent-packages/"+pkg.PackageID+"/search?q=grounded", "consumer-token")
	if unversioned.Code != http.StatusBadRequest {
		t.Fatalf("unversioned package search status=%d body=%s", unversioned.Code, unversioned.Body.String())
	}
}

func agentRuntimeTestStore(t *testing.T) (*BookKnowledgeStore, AgentPackage) {
	return agentRuntimeTestStoreWithLimit(t, 8)
}

func agentRuntimeTestStoreWithLimit(t *testing.T, maxContextChunks int) (*BookKnowledgeStore, AgentPackage) {
	return agentRuntimeTestStoreWithPackageEdit(t, func(pkg *AgentPackage) {
		pkg.RetrievalPolicy.MaxContextChunks = maxContextChunks
	})
}

func agentRuntimeTestStoreWithPackageEdit(t *testing.T, edit func(*AgentPackage)) (*BookKnowledgeStore, AgentPackage) {
	t.Helper()
	store := NewBookKnowledgeStore(t.TempDir())
	first := agentPackageTestRelease()
	first.ContentHash = "sha256:" + strings.Repeat("1", 64)
	first.Citations[0].SourceHTML = "private-source-marker"
	if err := store.saveKnowledgeRelease(first); err != nil {
		t.Fatal(err)
	}
	second := agentPackageTestRelease()
	second.ReleaseID = "release-2"
	second.BookID = "book-2"
	second.ContentHash = "sha256:" + strings.Repeat("2", 64)
	second.Book.BookID = "book-2"
	second.Book.Title = "Synthetic Article"
	second.Book.SourceType = "wechat_mp_article"
	second.Analysis.Claims[0].ID = "claim-2"
	second.Analysis.Claims[0].Statement = "Second grounded statement"
	second.Analysis.Claims[0].CitationIDs = []string{"citation-2"}
	second.Citations[0].CitationID = "citation-2"
	second.Citations[0].BookID = "book-2"
	second.Citations[0].ChunkID = "chunk-2"
	if err := store.saveKnowledgeRelease(second); err != nil {
		t.Fatal(err)
	}
	pkg := agentToolPolicyTestPackage()
	pkg.Releases[0].ContentHash = first.ContentHash
	pkg.Releases = append(pkg.Releases, AgentPackageReleaseRef{
		ReleaseID: "release-2", ContentHash: second.ContentHash, CitationIDs: []string{"citation-2"},
	})
	if edit != nil {
		edit(&pkg)
	}
	pkg, _ = FinalizeAgentPackage(pkg)
	savePassingAgentPackageTestEvaluation(t, store, pkg)
	if _, _, err := PublishAgentPackage(store, pkg, "runtime-package", AgentReadOnlyToolIDs(), testAgentPackageTime()); err != nil {
		t.Fatal(err)
	}
	return store, pkg
}
