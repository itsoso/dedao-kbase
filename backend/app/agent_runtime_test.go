package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
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

func TestAgentPackageRuntimeEnforcesPackageSearchLimit(t *testing.T) {
	store, pkg := agentRuntimeTestStoreWithLimit(t, 1)
	_, err := SearchAgentPackage(store, AgentPackageSearchRequest{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version, Query: "grounded", Limit: 2,
	})
	if err == nil || !strings.Contains(err.Error(), "max_context_chunks") {
		t.Fatalf("search limit error = %v", err)
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
	pkg.RetrievalPolicy.MaxContextChunks = maxContextChunks
	pkg.Releases = append(pkg.Releases, AgentPackageReleaseRef{
		ReleaseID: "release-2", ContentHash: second.ContentHash, CitationIDs: []string{"citation-2"},
	})
	pkg, _ = FinalizeAgentPackage(pkg)
	savePassingAgentPackageTestEvaluation(t, store, pkg)
	if _, _, err := PublishAgentPackage(store, pkg, "runtime-package", AgentReadOnlyToolIDs(), testAgentPackageTime()); err != nil {
		t.Fatal(err)
	}
	return store, pkg
}
