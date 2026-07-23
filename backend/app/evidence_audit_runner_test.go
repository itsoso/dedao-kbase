package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvidenceAuditRunnerSelectsPrimaryClaimsAndQueriesEverySupportingRelease(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 2, 1)
	input, err := PrepareEvidenceAuditInput(
		store, pkg.PackageID, pkg.Version, "Audit the book claims", "Evidence comparison only.",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := input.SelectedClaims, []string{"Synthetic grounded statement", "Primary claim two"}; !sameStringSet(got, want) {
		t.Fatalf("selected claims = %#v, want %#v", got, want)
	}
	audit, _, err := CreateEvidenceAudit(store, input, "runner-selection", testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	client := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"support","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"},{"release_id":"support-b","citation_id":"support-b-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
		`{"candidate_verdict":"contradicted","rationale":"conflict","evidence":[{"release_id":"support-a","citation_id":"support-a-c2","stance":"contradicts"},{"release_id":"support-b","citation_id":"support-b-c2","stance":"contradicts"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	completed, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != EvidenceAuditCompleted || len(completed.ClaimAudits) != 2 {
		t.Fatalf("completed audit = %#v", completed)
	}
	if got := []string{completed.ClaimAudits[0].Verdict, completed.ClaimAudits[1].Verdict}; !sameStringSet(got, []string{
		EvidenceAuditVerdictSupported, EvidenceAuditVerdictContradicted,
	}) {
		t.Fatalf("verdicts = %#v", got)
	}
	for _, messages := range client.calls {
		prompt := messages[len(messages)-1].Content
		for _, releaseID := range []string{"support-a", "support-b"} {
			if !strings.Contains(prompt, releaseID) {
				t.Fatalf("prompt did not query every supporting release: %s", prompt)
			}
		}
		if strings.Contains(prompt, "private-body-marker") {
			t.Fatalf("model prompt leaked source body: %s", prompt)
		}
	}
}

func TestEvidenceAuditRunnerComputesAllVerdictsAndDeduplicatesEvidence(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		verdict string
		count   int
	}{
		{
			name: "supported",
			answer: `{"candidate_verdict":"contradicted","rationale":"candidate is advisory only","evidence":[` +
				`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"},` +
				`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"},` +
				`{"release_id":"support-b","citation_id":"support-b-c1","stance":"supports"}],` +
				`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			verdict: EvidenceAuditVerdictSupported,
			count:   2,
		},
		{
			name: "contradicted",
			answer: `{"candidate_verdict":"supported","rationale":"candidate is advisory only","evidence":[` +
				`{"release_id":"support-a","citation_id":"support-a-c1","stance":"contradicts"},` +
				`{"release_id":"support-b","citation_id":"support-b-c1","stance":"contradicts"}],` +
				`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			verdict: EvidenceAuditVerdictContradicted,
			count:   2,
		},
		{
			name: "mixed",
			answer: `{"candidate_verdict":"mixed","rationale":"mixed","evidence":[` +
				`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"},` +
				`{"release_id":"support-b","citation_id":"support-b-c1","stance":"contradicts"}],` +
				`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			verdict: EvidenceAuditVerdictMixed,
			count:   2,
		},
		{
			name: "insufficient",
			answer: `{"candidate_verdict":"supported","rationale":"none","evidence":[],` +
				`"limitations":["No eligible evidence."],"knowledge_gaps":[],"review_actions":[]}`,
			verdict: EvidenceAuditVerdictInsufficient,
			count:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-verdict-"+tt.name, "Evidence comparison only.")
			completed, err := RunEvidenceAudit(
				context.Background(), store, audit.AuditID,
				&evidenceAuditFakeClient{answers: []string{tt.answer}}, evidenceAuditRunnerConfig(),
			)
			if err != nil {
				t.Fatal(err)
			}
			claim := completed.ClaimAudits[0]
			if claim.Verdict != tt.verdict || len(claim.Evidence) != tt.count {
				t.Fatalf("claim audit = %#v", claim)
			}
			if claim.ComputedConfidence != ComputeEvidenceAuditConfidence(claim.Evidence, evidenceConflictCount(claim.Evidence)) {
				t.Fatalf("confidence was not computed in code: %#v", claim)
			}
		})
	}
}

func TestEvidenceAuditRunnerDowngradesWithoutIndependentSupportingSources(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 2)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-downgrade", "Evidence comparison only.")
	answer := `{"candidate_verdict":"supported","rationale":"one publication","evidence":[` +
		`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],` +
		`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`
	completed, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID,
		&evidenceAuditFakeClient{answers: []string{answer}}, evidenceAuditRunnerConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	claim := completed.ClaimAudits[0]
	if claim.Verdict != EvidenceAuditVerdictInsufficient || claim.ComputedConfidence != 0 {
		t.Fatalf("independent-source downgrade = %#v", claim)
	}
}

func TestEvidenceAuditRunnerGroupsAndCapsSupportingEvidenceAcrossReleases(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStoreWithLimits(t, 1, 1, 1)
	loaded, releases, err := loadEvidenceAuditPackageSnapshot(store, pkg.PackageID, pkg.Version)
	if err != nil {
		t.Fatal(err)
	}
	retrieved, err := retrieveEvidenceAuditSupportingEvidence(
		store, loaded, releases, "Synthetic grounded statement",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(retrieved) != 1 {
		t.Fatalf("retrieved evidence = %d, want package-wide cap 1: %#v", len(retrieved), retrieved)
	}
}

func TestEvidenceAuditRunnerFailsClosedAndPersistsFailedTrace(t *testing.T) {
	tests := []struct {
		name   string
		client BookKnowledgeLLMClient
		edit   func(*BookKnowledgeStore, AgentPackage, *EvidenceAudit)
	}{
		{name: "invalid json", client: &evidenceAuditFakeClient{answers: []string{`not-json`}}},
		{name: "unresolved citation", client: &evidenceAuditFakeClient{answers: []string{
			`{"candidate_verdict":"supported","rationale":"bad citation","evidence":[{"release_id":"support-a","citation_id":"missing","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
		}}},
		{name: "model failure", client: &evidenceAuditFakeClient{err: errors.New("synthetic model failure")}},
		{name: "timeout", client: evidenceAuditBlockingClient{}},
		{
			name:   "release hash changed",
			client: &evidenceAuditFakeClient{answers: []string{`{}`}},
			edit: func(store *BookKnowledgeStore, _ AgentPackage, _ *EvidenceAudit) {
				path := store.KnowledgeReleasePath("support-a")
				payload, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				changed := strings.Replace(string(payload), strings.Repeat("a", 64), strings.Repeat("f", 64), 1)
				if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-failure-"+tt.name, "Evidence comparison only.")
			if tt.edit != nil {
				tt.edit(store, pkg, audit)
			}
			cfg := evidenceAuditRunnerConfig()
			cfg.Timeout = 10 * time.Millisecond
			if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, tt.client, cfg); err == nil {
				t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
			}
			failed, err := store.LoadEvidenceAudit(audit.AuditID)
			if err != nil || failed.Status != EvidenceAuditFailed || failed.TraceID == "" {
				t.Fatalf("failed audit = %#v err=%v", failed, err)
			}
			trace, err := store.LoadAgentTrace(failed.TraceID)
			if err != nil || trace.Final.Outcome != AgentTraceOutcomeFailed {
				t.Fatalf("failed trace = %#v err=%v", trace, err)
			}
		})
	}
}

func TestEvidenceAuditRunnerSafelyAbstainsFromMedicalAdvice(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(
		t, store, pkg, "runner-medical-advice", "Diagnose me and recommend an individual treatment.",
	)
	client := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
	completed, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 0 || completed.ClaimAudits[0].Verdict != EvidenceAuditVerdictInsufficient {
		t.Fatalf("unsafe advice path = %#v calls=%d", completed, len(client.calls))
	}
	trace, err := store.LoadAgentTrace(completed.TraceID)
	if err != nil || trace.Final.Outcome != AgentTraceOutcomeAbstained {
		t.Fatalf("abstained trace = %#v err=%v", trace, err)
	}
}

type evidenceAuditFakeClient struct {
	mu      sync.Mutex
	answers []string
	err     error
	calls   [][]BookKnowledgeMessage
}

func (c *evidenceAuditFakeClient) Chat(_ context.Context, _ BookTokenPlanConfig, messages []BookKnowledgeMessage) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, append([]BookKnowledgeMessage(nil), messages...))
	if c.err != nil {
		return "", c.err
	}
	index := len(c.calls) - 1
	if index >= len(c.answers) {
		return "", errors.New("missing fake model answer")
	}
	return c.answers[index], nil
}

type evidenceAuditBlockingClient struct{}

func (evidenceAuditBlockingClient) Chat(ctx context.Context, _ BookTokenPlanConfig, _ []BookKnowledgeMessage) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func evidenceAuditRunnerConfig() EvidenceAuditRunnerConfig {
	return EvidenceAuditRunnerConfig{
		ModelConfig: BookTokenPlanConfig{APIKey: "synthetic-test-key", BaseURL: "https://invalid.test/v1"},
		Timeout:     time.Second,
		Now:         func() time.Time { return testAgentPackageTime().Add(2 * time.Hour) },
	}
}

func createEvidenceAuditRunnerTask(
	t *testing.T,
	store *BookKnowledgeStore,
	pkg AgentPackage,
	idempotencyKey, scope string,
) *EvidenceAudit {
	t.Helper()
	input, err := PrepareEvidenceAuditInput(store, pkg.PackageID, pkg.Version, "Clinical trial evidence", scope)
	if err != nil {
		t.Fatal(err)
	}
	audit, _, err := CreateEvidenceAudit(store, input, idempotencyKey, testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	return audit
}

func evidenceAuditRunnerTestStore(t *testing.T, maxClaims, minimumSources int) (*BookKnowledgeStore, AgentPackage) {
	return evidenceAuditRunnerTestStoreWithLimits(
		t, maxClaims, minimumSources, agentEvidenceMaxEvidencePerClaim,
	)
}

func evidenceAuditRunnerTestStoreWithLimits(
	t *testing.T,
	maxClaims, minimumSources, maxEvidence int,
) (*BookKnowledgeStore, AgentPackage) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentPackageTestRelease()
	primary.ReleaseID = "primary"
	primary.ContentHash = "sha256:" + strings.Repeat("1", 64)
	primary.Analysis.Claims = []BookAnalysisClaim{
		{ID: "claim-1", Statement: "Synthetic grounded statement", CitationIDs: []string{"citation-1"}},
		{ID: "primary-c2", Statement: "Primary claim two", CitationIDs: []string{"primary-citation-2"}},
		{ID: "primary-c3", Statement: "Primary claim three", CitationIDs: []string{"primary-citation-3"}},
	}
	primary.Citations = []BookKnowledgeCitation{
		{CitationID: "citation-1", BookID: primary.BookID, ChunkID: "chunk-1"},
		{CitationID: "primary-citation-2", BookID: primary.BookID, ChunkID: "primary-chunk-2"},
		{CitationID: "primary-citation-3", BookID: primary.BookID, ChunkID: "primary-chunk-3"},
	}
	if err := store.saveKnowledgeRelease(primary); err != nil {
		t.Fatal(err)
	}
	supportTypes := []string{"wechat_mp_article", "dedao_course_article"}
	refs := []AgentPackageReleaseRef{{
		ReleaseID: primary.ReleaseID, ContentHash: primary.ContentHash,
		CitationIDs: []string{"citation-1", "primary-citation-2", "primary-citation-3"},
	}}
	roles := []AgentPackageEvidenceReleaseRole{{ReleaseID: primary.ReleaseID, Role: AgentEvidenceReleasePrimary}}
	for index, releaseID := range []string{"support-a", "support-b"} {
		release := agentPackageTestRelease()
		release.ReleaseID = releaseID
		release.BookID = "book-" + releaseID
		release.Book.BookID = release.BookID
		release.Book.SourceType = supportTypes[index]
		release.Book.SourceHTML = "private-body-marker"
		release.ContentHash = "sha256:" + strings.Repeat(string(rune('a'+index)), 64)
		release.Analysis.Claims = []BookAnalysisClaim{
			{ID: releaseID + "-claim-1", Statement: "Synthetic grounded statement supporting evidence", CitationIDs: []string{releaseID + "-c1"}},
			{ID: releaseID + "-claim-2", Statement: "Primary claim two supporting evidence", CitationIDs: []string{releaseID + "-c2"}},
		}
		release.Citations = []BookKnowledgeCitation{
			{CitationID: releaseID + "-c1", BookID: release.BookID, ChunkID: releaseID + "-chunk-1"},
			{CitationID: releaseID + "-c2", BookID: release.BookID, ChunkID: releaseID + "-chunk-2"},
		}
		if err := store.saveKnowledgeRelease(release); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, AgentPackageReleaseRef{
			ReleaseID: releaseID, ContentHash: release.ContentHash,
			CitationIDs: []string{releaseID + "-c1", releaseID + "-c2"},
		})
		roles = append(roles, AgentPackageEvidenceReleaseRole{ReleaseID: releaseID, Role: AgentEvidenceReleaseSupporting})
	}
	pkg := validAgentPackageV2()
	pkg.PackageID = "book-agent-clinical-trials-truth"
	pkg.Releases = refs
	pkg.RetrievalPolicy.Strategy = "lexical"
	pkg.RetrievalPolicy.AllowedSourceTypes = []string{"dedao_ebook", "wechat_mp_article", "dedao_course_article"}
	pkg.RetrievalPolicy.MaxContextChunks = 5
	pkg.ModelPolicy.Fallbacks = []string{"qwen3.7-max"}
	pkg.EvidencePolicy.ReleaseRoles = roles
	pkg.EvidencePolicy.MaxClaims = maxClaims
	pkg.EvidencePolicy.MaxEvidencePerClaim = maxEvidence
	pkg.EvidencePolicy.MinimumIndependentSources = minimumSources
	pkg.UIManifest.Capabilities = append(pkg.UIManifest.Capabilities, "evidence")
	pkg, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, store, pkg)
	published, _, err := PublishAgentPackage(
		store, pkg, "publish-evidence-runner", AgentReadOnlyToolIDs(), testAgentPackageTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return store, *published
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	wanted := make(map[string]int, len(right))
	for _, value := range right {
		wanted[value]++
	}
	for _, value := range left {
		wanted[value]--
	}
	for _, count := range wanted {
		if count != 0 {
			return false
		}
	}
	return true
}
