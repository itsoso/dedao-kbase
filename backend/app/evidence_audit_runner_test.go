package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
		context.Background(), store, loaded, releases, "Synthetic grounded statement",
		testAgentPackageTime(), newEvidenceAuditObserver(evidenceAuditRunnerConfig()),
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
	for _, scope := range []string{
		"Diagnose me and recommend an individual treatment.",
		"Evidence audit: Should I undergo surgery?",
		"I need surgery.",
		"Evidence audit: Should this patient undergo surgery?",
		"Clinical trial evidence: Should patient John undergo surgery?",
		"Review the evidence: should this 52-year-old person start treatment?",
		"Clinical trial evidence: Can I have this procedure?",
		"Would this test be appropriate for me?",
		"Is this therapy right for me?",
		"Should I stop aspirin?",
		"What dose should I take?",
		"Could another medicine work better for me?",
		"Could this rash be melanoma?",
		"Is chemotherapy right for my case?",
		"我是否应该停药？",
		"能不能给我换药？",
		"这个药要不要加量？",
		"阿司匹林怎么吃",
		"我的头痛是什么病？",
		"证据审计：我是否应该做手术？",
		"证据审计：患者张三是否应该做手术？",
		"文献综述：这个 62 岁病例要不要接受治疗？",
		"临床试验证据：这个检查适合我吗？",
		"我能不能接受这个治疗？",
	} {
		t.Run(scope, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(
				t, store, pkg, "runner-medical-advice-"+sha256Fingerprint([]byte(scope)), scope,
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
		})
	}
}

func TestEvidenceAuditRunnerAllowsPopulationEvidenceQuestions(t *testing.T) {
	for _, scope := range []string{
		"In adults with resectable lung cancer, does surgery improve five-year survival compared with radiotherapy?",
		"Chemotherapy improves survival in adults.",
		"在可切除肺癌患者中，手术相比放疗是否改善五年生存率？",
	} {
		t.Run(scope, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(
				t, store, pkg, "runner-population-evidence-"+sha256Fingerprint([]byte(scope)), scope,
			)
			client := &evidenceAuditFakeClient{answers: []string{
				`{"candidate_verdict":"supported","rationale":"population evidence","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			}}
			completed, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
			if err != nil {
				t.Fatal(err)
			}
			if len(client.calls) != 1 || completed.ClaimAudits[0].Verdict == EvidenceAuditVerdictInsufficient {
				t.Fatalf("population evidence path = %#v calls=%d", completed, len(client.calls))
			}
		})
	}
}

func TestEvidenceAuditRunnerEnforcesAllowedVerdictSubset(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	loaded, releases, err := loadEvidenceAuditPackageSnapshot(store, pkg.PackageID, pkg.Version)
	if err != nil {
		t.Fatal(err)
	}
	retrieved, err := retrieveEvidenceAuditSupportingEvidence(
		context.Background(), store, loaded, releases, "Synthetic grounded statement",
		testAgentPackageTime(), newEvidenceAuditObserver(evidenceAuditRunnerConfig()),
	)
	if err != nil {
		t.Fatal(err)
	}
	decision := evidenceAuditModelDecision{
		CandidateVerdict: EvidenceAuditVerdictSupported,
		Rationale:        "supported",
		Evidence: []evidenceAuditModelEvidence{{
			ReleaseID: "support-a", CitationID: "support-a-c1", Stance: "supports",
		}},
	}

	loaded.EvidencePolicy.AllowedVerdicts = []string{EvidenceAuditVerdictInsufficient}
	claim, err := decideEvidenceAuditClaim(loaded, "Synthetic grounded statement", retrieved, decision)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Verdict != EvidenceAuditVerdictInsufficient || len(claim.Evidence) != 0 {
		t.Fatalf("disallowed verdict was not downgraded: %#v", claim)
	}

	loaded.EvidencePolicy.AllowedVerdicts = []string{EvidenceAuditVerdictMixed}
	if _, err := decideEvidenceAuditClaim(loaded, "Synthetic grounded statement", retrieved, decision); err == nil ||
		!strings.Contains(err.Error(), "allowed_verdicts") {
		t.Fatalf("missing insufficient policy violation = %v", err)
	}
}

func TestEvidenceAuditRunnerFiltersMissingAndStaleSupportingEvidence(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	loaded, releases, err := loadEvidenceAuditPackageSnapshot(store, pkg.PackageID, pkg.Version)
	if err != nil {
		t.Fatal(err)
	}
	loaded.EvidencePolicy.FreshnessPolicy.MaxAgeDays = 30
	loaded.EvidencePolicy.FreshnessPolicy.RequirePublicationDate = true
	now := testAgentPackageTime()

	stale := releases["support-a"]
	stale.Book.PublishedAt = now.AddDate(0, 0, -31).Format(time.RFC3339)
	stale.Citations[0].PublishedAt = stale.Book.PublishedAt
	releases["support-a"] = stale
	stalePayload, err := encodeJSONFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.KnowledgeReleasePath(stale.ReleaseID), stalePayload, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := releases["support-b"]
	missing.Book.PublishedAt = ""
	missing.Citations[0].PublishedAt = ""
	releases["support-b"] = missing
	missingPayload, err := encodeJSONFile(missing)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.KnowledgeReleasePath(missing.ReleaseID), missingPayload, 0o600); err != nil {
		t.Fatal(err)
	}

	observer := newEvidenceAuditObserver(evidenceAuditRunnerConfig())
	retrieved, err := retrieveEvidenceAuditSupportingEvidence(
		context.Background(), store, loaded, releases, "Synthetic grounded statement",
		now, observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(retrieved) != 0 {
		t.Fatalf("stale or undated evidence remained eligible: %#v", retrieved)
	}
	snapshot := observer.snapshot(AgentTraceOutcomeCompleted)
	if len(snapshot.FreshnessDecisions) != 2 {
		t.Fatalf("trace freshness decisions = %#v", snapshot.FreshnessDecisions)
	}

	fresh := releases["support-a"]
	fresh.Citations[0].PublishedAt = now.AddDate(0, 0, -2).Format(time.RFC3339)
	releases["support-a"] = fresh
	freshPayload, err := encodeJSONFile(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.KnowledgeReleasePath(fresh.ReleaseID), freshPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	retrieved, err = retrieveEvidenceAuditSupportingEvidence(
		context.Background(), store, loaded, releases, "Synthetic grounded statement",
		now, newEvidenceAuditObserver(evidenceAuditRunnerConfig()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(retrieved) != 1 ||
		retrieved[0].Ref.PublishedAt != fresh.Citations[0].PublishedAt ||
		retrieved[0].Ref.FreshnessDecision != EvidenceAuditFreshnessFresh {
		t.Fatalf("freshness metadata = %#v", retrieved)
	}

	loaded.EvidencePolicy.FreshnessPolicy.RequirePublicationDate = false
	retrieved, err = retrieveEvidenceAuditSupportingEvidence(
		context.Background(), store, loaded, releases, "Synthetic grounded statement",
		now, newEvidenceAuditObserver(evidenceAuditRunnerConfig()),
	)
	if err != nil {
		t.Fatal(err)
	}
	foundMissing := false
	for _, item := range retrieved {
		if item.Ref.ReleaseID == "support-b" &&
			item.Ref.FreshnessDecision == EvidenceAuditFreshnessMissing &&
			item.Ref.PublishedAt == "" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatalf("optional missing publication date was not retained: %#v", retrieved)
	}
}

func TestEvidenceAuditCostLedgerReservesWholeAuditBudgetAndRestoresCheckpoints(t *testing.T) {
	messages := []BookKnowledgeMessage{{Role: "user", Content: "short evidence request"}}
	ledger := newEvidenceAuditCostLedger(0.25)
	cfg := BookTokenPlanConfig{}
	first, err := ledger.Reserve(messages, &cfg, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first <= 0 || ledger.Remaining() >= 0.25 {
		t.Fatalf("first reservation=%f remaining=%f", first, ledger.Remaining())
	}
	restored := newEvidenceAuditCostLedger(0.25)
	if err := restored.Restore(first); err != nil {
		t.Fatal(err)
	}
	if restored.Remaining() != ledger.Remaining() {
		t.Fatalf("restored remaining=%f want=%f", restored.Remaining(), ledger.Remaining())
	}
	if _, err := restored.Reserve(messages, &cfg, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Reserve(messages, &cfg, 1); err == nil {
		t.Fatal("audit budget allowed an extra model reservation")
	}
}

func TestEvidenceAuditRunnerDoesNotCallModelWhenWholeAuditBudgetIsInsufficient(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 2, 1)
	pkg.Version = "2.0.1-low-budget"
	pkg.LifecycleState = AgentPackageDraft
	pkg.CreatedAt = ""
	pkg.PublishedAt = ""
	pkg.ContentHash = ""
	pkg.ModelPolicy.MaxCostUSD = 0.1
	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, store, finalized)
	published, _, err := PublishAgentPackage(
		store, finalized, "publish-low-budget", AgentReadOnlyToolIDs(), testAgentPackageTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	audit := createEvidenceAuditRunnerTask(
		t, store, *published, "runner-low-whole-audit-budget", "Evidence comparison only.",
	)
	client := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
	if _, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig(),
	); err == nil || !strings.Contains(err.Error(), "max_cost_usd") {
		t.Fatalf("budget error = %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("model calls = %d, want zero", len(client.calls))
	}
	failed, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil || failed.Status != EvidenceAuditFailed {
		t.Fatalf("failed audit = %#v err=%v", failed, err)
	}
}

func TestEvidenceAuditRunnerRejectsIndividualDecisionsDespitePopulationPrefixes(t *testing.T) {
	for _, scope := range []string{
		"Population-level PICO: should this patient undergo surgery?",
		"Clinical trial evidence for adults: should patient John stop aspirin?",
		"Population-level PICO: should John, age 62, undergo surgery?",
		"Population-level PICO: should Maria undergo surgery?",
		"群体级PICO：患者张三是否应该做手术？",
		"群体级 PICO：年龄 62 的张三是否应该做手术？",
		"群体级 PICO：李雷是否应该做手术？",
		"人群证据比较：这个 62 岁病例要不要停药？",
		"Among adults, is surgery appropriate for John?",
		"成年患者中，张三做手术合适吗？",
	} {
		if !evidenceAuditRequestsMedicalAdvice(scope) {
			t.Fatalf("individual decision was not rejected: %q", scope)
		}
	}
	for _, scope := range []string{
		"In adults with resectable lung cancer, does surgery improve five-year survival compared with radiotherapy?",
		"Chemotherapy improves survival in adults.",
		"在可切除肺癌患者中，手术相比放疗是否改善五年生存率？",
	} {
		if evidenceAuditRequestsMedicalAdvice(scope) {
			t.Fatalf("population evidence question was rejected: %q", scope)
		}
	}
}

func TestEvidenceAuditRunnerTimeoutCoversPackageAndReleaseLoading(t *testing.T) {
	t.Run("explicit timeout interrupts package load", func(t *testing.T) {
		store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
		audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-package-load-timeout", "Evidence comparison only.")
		previous := evidenceAuditRuntimeStageHook
		evidenceAuditRuntimeStageHook = func(ctx context.Context, stage string) error {
			if stage == "package_load" {
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}
		t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
		cfg := evidenceAuditRunnerConfig()
		cfg.Timeout = 10 * time.Millisecond
		client := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
		if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, cfg); err == nil {
			t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
		}
		if len(client.calls) != 0 {
			t.Fatal("model called after package load timeout")
		}
	})

	t.Run("package deadline counts release load from original start", func(t *testing.T) {
		store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
		pkg.Version = "2.0.1-short-timeout"
		pkg.ModelPolicy.TimeoutMS = 5
		pkg.ContentHash = ""
		pkg, err := FinalizeAgentPackage(pkg)
		if err != nil {
			t.Fatal(err)
		}
		savePassingAgentPackageTestEvaluation(t, store, pkg)
		published, _, err := PublishAgentPackage(
			store, pkg, "publish-short-timeout", AgentReadOnlyToolIDs(), testAgentPackageTime(),
		)
		if err != nil {
			t.Fatal(err)
		}
		input, err := PrepareEvidenceAuditInput(
			store, published.PackageID, published.Version, "Clinical trial evidence", "Evidence comparison only.",
		)
		if err != nil {
			t.Fatal(err)
		}
		audit, _, err := CreateEvidenceAudit(store, input, "runner-release-load-timeout", testAgentPackageTime())
		if err != nil {
			t.Fatal(err)
		}
		previous := evidenceAuditRuntimeStageHook
		evidenceAuditRuntimeStageHook = func(ctx context.Context, stage string) error {
			if stage == "release_load" {
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}
		t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
		client := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
		cfg := evidenceAuditRunnerConfig()
		cfg.Timeout = 0
		if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, cfg); err == nil {
			t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
		}
		if len(client.calls) != 0 {
			t.Fatal("model called after package deadline expired")
		}
	})

	t.Run("v2 package deadline interrupts artifact load", func(t *testing.T) {
		store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
		pkg.Version = "2.0.2-artifact-timeout"
		pkg.ModelPolicy.TimeoutMS = 5
		pkg.ContentHash = ""
		pkg, err := FinalizeAgentPackage(pkg)
		if err != nil {
			t.Fatal(err)
		}
		savePassingAgentPackageTestEvaluation(t, store, pkg)
		published, _, err := PublishAgentPackage(
			store, pkg, "publish-artifact-timeout", AgentReadOnlyToolIDs(), testAgentPackageTime(),
		)
		if err != nil {
			t.Fatal(err)
		}
		input, err := PrepareEvidenceAuditInput(
			store, published.PackageID, published.Version, "Clinical trial evidence", "Evidence comparison only.",
		)
		if err != nil {
			t.Fatal(err)
		}
		audit, _, err := CreateEvidenceAudit(store, input, "runner-artifact-load-timeout", testAgentPackageTime())
		if err != nil {
			t.Fatal(err)
		}
		previous := agentPackageArtifactLoadHook
		agentPackageArtifactLoadHook = func(ctx context.Context, _ string) error {
			<-ctx.Done()
			return ctx.Err()
		}
		t.Cleanup(func() { agentPackageArtifactLoadHook = previous })
		client := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
		cfg := evidenceAuditRunnerConfig()
		cfg.Timeout = 0
		if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, cfg); err == nil {
			t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
		}
		if len(client.calls) != 0 {
			t.Fatal("model called after v2 package artifact deadline expired")
		}
	})
}

func TestEvidenceAuditRunnerEarlyLoadFailureReachesTerminalAndBusyLockDoesNotFail(t *testing.T) {
	t.Run("bootstrap load failure", func(t *testing.T) {
		store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
		audit := createEvidenceAuditRunnerTask(
			t, store, pkg, "runner-bootstrap-failure", "Evidence comparison only.",
		)
		previous := evidenceAuditRuntimeStageHook
		evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
			if stage == "load" {
				return context.DeadlineExceeded
			}
			return nil
		}
		t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
		if _, err := RunEvidenceAudit(
			context.Background(), store, audit.AuditID, &evidenceAuditFakeClient{},
			evidenceAuditRunnerConfig(),
		); err == nil {
			t.Fatal("bootstrap failure unexpectedly succeeded")
		}
		assertEvidenceAuditFailedTerminal(t, store, audit.AuditID)
	})

	t.Run("execution lock deadline", func(t *testing.T) {
		store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
		audit := createEvidenceAuditRunnerTask(
			t, store, pkg, "runner-lock-failure", "Evidence comparison only.",
		)
		unlock, err := store.acquireEvidenceAuditExecutionLock(context.Background(), audit.AuditID)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		cfg := evidenceAuditRunnerConfig()
		cfg.BootstrapTimeout = 20 * time.Millisecond
		if _, err := RunEvidenceAudit(
			context.Background(), store, audit.AuditID, &evidenceAuditFakeClient{}, cfg,
		); !errors.Is(err, ErrEvidenceAuditExecutionBusy) {
			t.Fatalf("lock failure error = %v, want execution busy", err)
		}
		loaded, err := store.LoadEvidenceAudit(audit.AuditID)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Status == EvidenceAuditFailed {
			t.Fatalf("busy lock incorrectly failed audit: %+v", loaded)
		}
	})
}

func assertEvidenceAuditFailedTerminal(
	t *testing.T,
	store *BookKnowledgeStore,
	auditID string,
) {
	t.Helper()
	audit, err := store.LoadEvidenceAudit(auditID)
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != EvidenceAuditFailed {
		t.Fatalf("audit status=%q, want failed", audit.Status)
	}
	trace, err := store.LoadAgentTrace(audit.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	if trace.Final.Outcome != AgentTraceOutcomeFailed {
		t.Fatalf("trace outcome=%q, want failed", trace.Final.Outcome)
	}
}

func TestEvidenceAuditRunnerFailsClosedWhenLaterClaimModelOutcomeIsUnknown(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 2, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-claim-checkpoint", "Evidence comparison only.")
	first := &evidenceAuditSequenceClient{
		answers: []string{
			`{"candidate_verdict":"supported","rationale":"first","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
		},
		blockAfterAnswers: true,
	}
	cfg := evidenceAuditRunnerConfig()
	cfg.Timeout = 2 * time.Second
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, first, cfg); err == nil {
		t.Fatal("first run unexpectedly succeeded")
	}
	running, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != EvidenceAuditFailed ||
		running.FailureCode != "model_outcome_unknown" {
		t.Fatalf("audit after uncertain model timeout = %#v", running)
	}
	trace, err := store.LoadAgentTrace(running.TraceID)
	if err != nil || trace.Final.Outcome != AgentTraceOutcomeFailed {
		t.Fatalf("failed trace = %#v err=%v", trace, err)
	}
	if len(first.calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(first.calls))
	}
}

func TestEvidenceAuditRunnerRecoversCandidateWhenCheckpointFollowupFails(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-checkpoint-followup", "Evidence comparison only.")
	previous := evidenceAuditRuntimeStageHook
	failed := false
	evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
		if stage == "checkpoint" && !failed {
			failed = true
			return errors.New("synthetic checkpoint followup failure")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
	first := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"saved candidate","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, first, evidenceAuditRunnerConfig()); err == nil {
		t.Fatal("first run unexpectedly succeeded")
	}
	running, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil || running.Status != EvidenceAuditRunning {
		t.Fatalf("running audit = %#v err=%v", running, err)
	}
	second := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
	completed, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, second, evidenceAuditRunnerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != EvidenceAuditCompleted || len(first.calls) != 1 || len(second.calls) != 0 {
		t.Fatalf("checkpoint recovery completed=%#v first_calls=%d second_calls=%d", completed, len(first.calls), len(second.calls))
	}
}

func TestEvidenceAuditRunnerRejectsCheckpointWithMismatchedRequestOrRetrievalIdentity(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(
		t, store, pkg, "runner-checkpoint-identity", "Evidence comparison only.",
	)
	previous := evidenceAuditRuntimeStageHook
	failed := false
	evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
		if stage == "checkpoint" && !failed {
			failed = true
			return errors.New("synthetic checkpoint followup failure")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
	first := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"saved candidate","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	if _, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, first, evidenceAuditRunnerConfig(),
	); err == nil {
		t.Fatal("first run unexpectedly succeeded")
	}
	resultDir := filepath.Join(store.evidenceAuditExecutionDir(audit.AuditID), "results")
	entries, err := os.ReadDir(resultDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("checkpoint entries=%v err=%v", entries, err)
	}
	oldPath := filepath.Join(resultDir, entries[0].Name())
	var candidate evidenceAuditClaimCandidate
	if err := readJSONFile(oldPath, &candidate); err != nil {
		t.Fatal(err)
	}
	candidate.RequestIdentity = "sha256:" + strings.Repeat("f", 64)
	hash, err := evidenceAuditClaimCandidateHash(candidate)
	if err != nil {
		t.Fatal(err)
	}
	candidate.CandidateHash = hash
	payload, err := encodeJSONFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	if err := writeEvidenceAuditPrivateFile(
		filepath.Join(resultDir, evidenceAuditHashName(hash)+".json"), payload,
	); err != nil {
		t.Fatal(err)
	}
	second := &evidenceAuditFakeClient{}
	if _, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, second, evidenceAuditRunnerConfig(),
	); err == nil || !strings.Contains(err.Error(), "checkpoint identity") {
		t.Fatalf("mismatched checkpoint error=%v", err)
	}
	if len(second.calls) != 0 {
		t.Fatalf("model was called after checkpoint identity mismatch: %d", len(second.calls))
	}
}

func TestEvidenceAuditRunnerRestoresCheckpointUsageWithoutRepeatingModelCall(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-checkpoint-usage", "Evidence comparison only.")
	previous := evidenceAuditRuntimeStageHook
	failed := false
	evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
		if stage == "checkpoint" && !failed {
			failed = true
			return errors.New("synthetic checkpoint followup failure")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
	cost := 0.0125
	first := &evidenceAuditResultClient{results: []BookKnowledgeLLMResult{{
		Content: `{"candidate_verdict":"supported","rationale":"saved candidate","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
		Usage: &BookKnowledgeLLMUsage{
			PromptTokens: 40, CompletionTokens: 10, TotalTokens: 50, CostUSD: &cost,
		},
	}}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, first, evidenceAuditRunnerConfig()); err == nil {
		t.Fatal("first run unexpectedly succeeded")
	}
	second := &evidenceAuditResultClient{}
	completed, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, second, evidenceAuditRunnerConfig())
	if err != nil {
		t.Fatal(err)
	}
	trace, err := store.LoadAgentTrace(completed.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.calls) != 1 || len(second.calls) != 0 {
		t.Fatalf("model calls first=%d second=%d", len(first.calls), len(second.calls))
	}
	want := AgentTraceUsage{
		Status: "reported", PromptTokens: 40, CompletionTokens: 10, TotalTokens: 50,
		CostUSD: 0.0125, CostStatus: "reported",
	}
	if trace.Observability == nil || trace.Observability.Usage != want {
		t.Fatalf("recovered usage = %#v, want %#v", trace.Observability, want)
	}
}

func TestEvidenceAuditRunnerCheckpointUnknownUsageRemainsUnknownAfterRecovery(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-checkpoint-unknown-usage", "Evidence comparison only.")
	previous := evidenceAuditRuntimeStageHook
	failed := false
	evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
		if stage == "checkpoint" && !failed {
			failed = true
			return errors.New("synthetic checkpoint followup failure")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
	first := &evidenceAuditResultClient{results: []BookKnowledgeLLMResult{{
		Content: `{"candidate_verdict":"supported","rationale":"saved candidate","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
		Usage:   nil,
	}}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, first, evidenceAuditRunnerConfig()); err == nil {
		t.Fatal("first run unexpectedly succeeded")
	}
	completed, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, &evidenceAuditResultClient{}, evidenceAuditRunnerConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := store.LoadAgentTrace(completed.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	if trace.Observability == nil || trace.Observability.Usage != (AgentTraceUsage{Status: "unknown"}) {
		t.Fatalf("recovered unknown usage = %#v", trace.Observability)
	}
}

func TestEvidenceAuditRunnerCheckpointDoesNotPersistPromptOrSourceBody(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-checkpoint-privacy", "Evidence comparison only.")
	privateMarker := "private-source-body-must-not-persist"
	client := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"` + privateMarker + `","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":["` + privateMarker + `"],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig()); err != nil {
		t.Fatal(err)
	}
	checkpointRoot := store.evidenceAuditExecutionDir(audit.AuditID)
	err := filepath.WalkDir(checkpointRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(payload), "Pinned supporting evidence") {
			t.Fatalf("checkpoint leaked private model or prompt content: %s", payload)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceAuditRunnerPrivateStatePermissionsAndRecoveredFields(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(
		t, store, pkg, "runner-private-state", "Evidence comparison only.",
	)
	client := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"bounded","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":["limited applicability"],"knowledge_gaps":["long-term outcome unknown"],"review_actions":["independent review"]}`,
	}}
	completed, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	claim := completed.ClaimAudits[0]
	if !reflect.DeepEqual(claim.Limitations, []string{"limited applicability"}) ||
		!reflect.DeepEqual(claim.KnowledgeGaps, []string{"long-term outcome unknown"}) ||
		!reflect.DeepEqual(claim.ReviewActions, []string{"independent review"}) {
		t.Fatalf("checkpoint fields were lost: %#v", claim)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for _, path := range []string{
		store.evidenceAuditExecutionDir(audit.AuditID),
		filepath.Dir(store.EvidenceAuditTraceReceiptPath(completed.TraceID)),
	} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("private directory %s mode=%#o", path, info.Mode().Perm())
		}
	}
	err = filepath.WalkDir(store.evidenceAuditExecutionDir(audit.AuditID), func(
		path string, entry os.DirEntry, walkErr error,
	) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("private state file %s mode=%#o", path, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	receiptInfo, err := os.Stat(store.EvidenceAuditTraceReceiptPath(completed.TraceID))
	if err != nil {
		t.Fatal(err)
	}
	if receiptInfo.Mode().Perm() != 0o600 {
		t.Fatalf("trace receipt mode=%#o", receiptInfo.Mode().Perm())
	}
	receiptPath := store.EvidenceAuditTraceReceiptPath(completed.TraceID)
	var receipt evidenceAuditTraceReceipt
	if err := readJSONFile(receiptPath, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.TraceHash = "sha256:" + strings.Repeat("0", 64)
	payload, err := encodeJSONFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeEvidenceAuditPrivateFile(receiptPath, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadAgentTrace(completed.TraceID); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered receipt error=%v", err)
	}
}

func TestEvidenceAuditRunnerDoesNotRetryUnknownInFlightModelOutcome(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-model-outcome-unknown", "Evidence comparison only.")
	first := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"returned but not persisted","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	previous := evidenceAuditRuntimeStageHook
	crashed := false
	evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
		if stage == "model_completed_before_checkpoint" && !crashed {
			crashed = true
			return errors.New("synthetic process crash after model response")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })

	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, first, evidenceAuditRunnerConfig()); err == nil {
		t.Fatal("first run unexpectedly succeeded")
	}
	running, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil || running.Status != EvidenceAuditRunning {
		t.Fatalf("audit after simulated crash = %#v err=%v", running, err)
	}

	evidenceAuditRuntimeStageHook = previous
	second := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, second, evidenceAuditRunnerConfig()); err == nil ||
		!strings.Contains(err.Error(), "model_outcome_unknown") {
		t.Fatalf("resume error = %v, want model_outcome_unknown", err)
	}
	failed, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil || failed.Status != EvidenceAuditFailed ||
		failed.FailureCode != "model_outcome_unknown" {
		t.Fatalf("failed audit = %#v err=%v", failed, err)
	}
	if len(first.calls) != 1 || len(second.calls) != 0 {
		t.Fatalf("model calls first=%d second=%d", len(first.calls), len(second.calls))
	}
}

func TestEvidenceAuditRunnerSerializesConcurrentExecutionWithoutDuplicateModelCalls(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-concurrent-checkpoint", "Evidence comparison only.")
	client := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"one call","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	var wait sync.WaitGroup
	wait.Add(2)
	errs := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			defer wait.Done()
			_, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	successes := 0
	busy := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrEvidenceAuditExecutionBusy):
			busy++
		default:
			t.Errorf("concurrent run failed: %v", err)
		}
	}
	if successes != 1 || busy != 1 {
		t.Fatalf("concurrent outcomes successes=%d busy=%d", successes, busy)
	}
	if len(client.calls) != 1 {
		t.Fatalf("concurrent execution called model %d times", len(client.calls))
	}
}

func TestEvidenceAuditRunnerTraceFinalCitationsMatchOnlyReportEvidence(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-selected-citations", "Evidence comparison only.")
	answer := `{"candidate_verdict":"supported","rationale":"one selected item","evidence":[` +
		`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],` +
		`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`
	completed, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID,
		&evidenceAuditFakeClient{answers: []string{answer}}, evidenceAuditRunnerConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := store.LoadAgentTrace(completed.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Retrievals) < 2 {
		t.Fatalf("retrieval trace lost unselected hits: %#v", trace.Retrievals)
	}
	if len(trace.Final.Citations) != 1 ||
		trace.Final.Citations[0].CitationID != completed.ClaimAudits[0].Evidence[0].CitationID {
		t.Fatalf("final citations = %#v, report evidence = %#v", trace.Final.Citations, completed.ClaimAudits[0].Evidence)
	}
}

func TestEvidenceAuditRunnerRecoversTerminalProtocolFailures(t *testing.T) {
	tests := []struct {
		name        string
		installFail func(t *testing.T)
		firstStatus string
		traceExists bool
		finalFailed bool
	}{
		{
			name:        "report store failure",
			firstStatus: EvidenceAuditCompleted,
			traceExists: true,
			installFail: func(t *testing.T) {
				previous := evidenceAuditStorageFault
				manifestWrites := 0
				evidenceAuditStorageFault = func(stage, _ string) error {
					if stage == evidenceAuditFaultManifestBeforePublish {
						manifestWrites++
						if manifestWrites == 2 {
							return errors.New("synthetic report manifest failure")
						}
					}
					return nil
				}
				t.Cleanup(func() { evidenceAuditStorageFault = previous })
			},
		},
		{
			name:        "trace save failure",
			firstStatus: EvidenceAuditFailed,
			traceExists: true,
			finalFailed: true,
			installFail: func(t *testing.T) {
				previous := evidenceAuditTraceStorageFault
				failed := false
				evidenceAuditTraceStorageFault = func(stage, _ string) error {
					if stage == evidenceAuditTraceFaultBeforeSave && !failed {
						failed = true
						return errors.New("synthetic trace save failure")
					}
					return nil
				}
				t.Cleanup(func() { evidenceAuditTraceStorageFault = previous })
			},
		},
		{
			name:        "trace finalize failure",
			firstStatus: EvidenceAuditCompleted,
			traceExists: true,
			installFail: func(t *testing.T) {
				previous := evidenceAuditTraceStorageFault
				failed := false
				evidenceAuditTraceStorageFault = func(stage, _ string) error {
					if stage == evidenceAuditTraceFaultBeforeFinalize && !failed {
						failed = true
						return errors.New("synthetic trace finalize failure")
					}
					return nil
				}
				t.Cleanup(func() { evidenceAuditTraceStorageFault = previous })
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-recovery-"+tt.name, "Evidence comparison only.")
			client := &evidenceAuditFakeClient{answers: []string{
				`{"candidate_verdict":"supported","rationale":"support","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			}}
			tt.installFail(t)
			if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig()); err == nil {
				t.Fatal("first run unexpectedly succeeded")
			}
			first, err := store.LoadEvidenceAudit(audit.AuditID)
			if err != nil {
				t.Fatal(err)
			}
			if first.Status != tt.firstStatus {
				t.Fatalf("audit status after injected failure = %q, want %q", first.Status, tt.firstStatus)
			}
			_, traceErr := store.LoadAgentTrace(first.TraceID)
			if (traceErr == nil) != tt.traceExists {
				t.Fatalf("trace existence after injected failure = %v, want %v", traceErr == nil, tt.traceExists)
			}
			if tt.finalFailed {
				trace, traceErr := store.LoadAgentTrace(first.TraceID)
				if traceErr != nil ||
					findEvidenceAuditTraceStage(t, trace, "trace_persistence").Status != "failed" {
					t.Fatalf("failed trace persistence = %#v err=%v", trace, traceErr)
				}
				return
			}
			recovered, err := store.LoadEvidenceAudit(audit.AuditID)
			if err == nil && recovered.Status != EvidenceAuditCompleted {
				recovered, err = RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
			}
			if err != nil {
				t.Fatalf("recovery run failed: %v", err)
			}
			if recovered.Status != EvidenceAuditCompleted {
				t.Fatalf("recovered audit = %#v", recovered)
			}
			trace, err := store.LoadAgentTrace(recovered.TraceID)
			if err != nil || trace.Final.Outcome != AgentTraceOutcomeCompleted {
				t.Fatalf("recovered trace = %#v err=%v", trace, err)
			}
			if len(client.calls) != 1 {
				t.Fatalf("recovery reran model %d times", len(client.calls))
			}
		})
	}
}

func TestEvidenceAuditRunnerFailsClosedWhenTracePreparationCannotPersist(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-trace-prepare-failure", "Evidence comparison only.")
	previous := evidenceAuditTraceStorageFault
	evidenceAuditTraceStorageFault = func(stage, _ string) error {
		if stage == evidenceAuditTraceFaultBeforePrepare {
			return errors.New("synthetic trace prepare failure")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditTraceStorageFault = previous })
	client := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"support","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig()); err == nil {
		t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
	}
	stored, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status == EvidenceAuditCompleted {
		t.Fatalf("audit completed without a recoverable trace: %#v", stored)
	}
}

func TestEvidenceAuditRunnerReadSideReconcilesPreparedFailure(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-failed-read-reconcile", "Evidence comparison only.")
	if _, err := StartEvidenceAudit(
		store, audit.AuditID, "trace-failed-read-reconcile",
		time.Date(2026, 7, 19, 17, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatal(err)
	}
	originalWriter := writeEvidenceAuditManifestFile
	failedOnce := false
	writeEvidenceAuditManifestFile = func(path string, payload []byte) error {
		if !failedOnce {
			failedOnce = true
			return errors.New("synthetic failed terminal publish interruption")
		}
		return originalWriter(path, payload)
	}
	t.Cleanup(func() { writeEvidenceAuditManifestFile = originalWriter })
	client := &evidenceAuditFakeClient{answers: []string{`not-json`}}
	if _, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig(),
	); err == nil {
		t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
	}
	writeEvidenceAuditManifestFile = originalWriter
	reconciled, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Status != EvidenceAuditFailed ||
		reconciled.FailureCode != "invalid_model_output" ||
		len(client.calls) != 1 {
		t.Fatalf("reconciled failed audit = %#v calls=%d", reconciled, len(client.calls))
	}
	if _, err := os.Stat(store.EvidenceAuditTraceTerminalPath(audit.AuditID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed terminal was not cleaned after read reconciliation: %v", err)
	}
}

func TestEvidenceAuditRunnerContextCoversRetrievalAndCitationResolution(t *testing.T) {
	for _, stage := range []string{"retrieval", "citation"} {
		t.Run(stage, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-context-"+stage, "Evidence comparison only.")
			previous := evidenceAuditRuntimeStageHook
			evidenceAuditRuntimeStageHook = func(ctx context.Context, current string) error {
				if current != stage {
					return nil
				}
				<-ctx.Done()
				return ctx.Err()
			}
			t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
			cfg := evidenceAuditRunnerConfig()
			cfg.Timeout = 10 * time.Millisecond
			client := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
			if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, cfg); err == nil {
				t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
			}
			if len(client.calls) != 0 {
				t.Fatalf("model called after %s timeout", stage)
			}
			failed, err := store.LoadEvidenceAudit(audit.AuditID)
			if err != nil || failed.Status != EvidenceAuditFailed {
				t.Fatalf("failed audit = %#v err=%v", failed, err)
			}
		})
	}
}

func TestEvidenceAuditRunnerFailsImmediatelyWhenLaterClaimRetrievalOrCitationTimesOut(t *testing.T) {
	for _, target := range []string{"retrieval", "citation"} {
		t.Run(target, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 2, 1)
			audit := createEvidenceAuditRunnerTask(
				t, store, pkg, "runner-later-timeout-"+target, "Evidence comparison only.",
			)
			previous := evidenceAuditRuntimeStageHook
			seen := 0
			evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
				if stage != target {
					return nil
				}
				seen++
				if seen > 2 {
					return context.DeadlineExceeded
				}
				return nil
			}
			t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
			client := &evidenceAuditFakeClient{answers: []string{
				`{"candidate_verdict":"supported","rationale":"first","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
				`must not be called`,
			}}
			if _, err := RunEvidenceAudit(
				context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig(),
			); err == nil {
				t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
			}
			failed, err := store.LoadEvidenceAudit(audit.AuditID)
			if err != nil || failed.Status != EvidenceAuditFailed ||
				failed.FailureCode != "runner_timeout" {
				t.Fatalf("failed audit = %#v err=%v", failed, err)
			}
			trace, err := store.LoadAgentTrace(failed.TraceID)
			if err != nil || trace.Final.Outcome != AgentTraceOutcomeFailed {
				t.Fatalf("failed trace = %#v err=%v", trace, err)
			}
			if len(client.calls) != 1 {
				t.Fatalf("model calls = %d, want only the completed first claim", len(client.calls))
			}
		})
	}
}

func TestEvidenceAuditRunnerPersistenceStagesMeasureRealFailures(t *testing.T) {
	tests := []struct {
		name      string
		stageName string
		install   func(t *testing.T)
	}{
		{
			name:      "report persistence",
			stageName: "report_persistence",
			install: func(t *testing.T) {
				previous := evidenceAuditStorageFault
				failed := false
				evidenceAuditStorageFault = func(stage, path string) error {
					if stage == evidenceAuditFaultImmutableTempSynced &&
						strings.Contains(path, string(filepath.Separator)+"reports"+string(filepath.Separator)) &&
						!failed {
						failed = true
						return errors.New("synthetic report persistence failure")
					}
					return nil
				}
				t.Cleanup(func() { evidenceAuditStorageFault = previous })
			},
		},
		{
			name:      "trace persistence",
			stageName: "trace_persistence",
			install: func(t *testing.T) {
				previous := evidenceAuditTraceStorageFault
				failed := false
				evidenceAuditTraceStorageFault = func(stage, _ string) error {
					if stage == evidenceAuditTraceFaultBeforePrepare && !failed {
						failed = true
						return errors.New("synthetic trace persistence failure")
					}
					return nil
				}
				t.Cleanup(func() { evidenceAuditTraceStorageFault = previous })
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-stage-failure-"+tt.name, "Evidence comparison only.")
			tt.install(t)
			base := testAgentPackageTime().Add(3 * time.Hour)
			tick := 0
			cfg := evidenceAuditRunnerConfig()
			cfg.Now = func() time.Time {
				current := base.Add(time.Duration(tick) * 7 * time.Millisecond)
				tick++
				return current
			}
			client := &evidenceAuditFakeClient{answers: []string{
				`{"candidate_verdict":"supported","rationale":"support","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			}}
			if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, cfg); err == nil {
				t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
			}
			stored, err := store.LoadEvidenceAudit(audit.AuditID)
			if err != nil {
				t.Fatal(err)
			}
			trace, err := store.LoadAgentTrace(stored.TraceID)
			if err != nil {
				t.Fatal(err)
			}
			stage := findEvidenceAuditTraceStage(t, trace, tt.stageName)
			if stage.Status != "failed" || stage.DurationMS <= 0 {
				t.Fatalf("%s stage = %#v, want failed with measured duration", tt.stageName, stage)
			}
		})
	}
}

func TestEvidenceAuditRunnerTraceIncludesBoundedDeterministicObservability(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-observability", "Evidence comparison only.")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"choices":[{"message":{"content":"{\"candidate_verdict\":\"supported\",\"rationale\":\"support\",\"evidence\":[{\"release_id\":\"support-a\",\"citation_id\":\"support-a-c1\",\"stance\":\"supports\"}],\"limitations\":[],\"knowledge_gaps\":[],\"review_actions\":[]}"}}],
			"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150,"cost":0.0042}
		}`)
	}))
	defer server.Close()
	base := testAgentPackageTime().Add(2 * time.Hour)
	tick := 0
	cfg := evidenceAuditRunnerConfig()
	cfg.ModelConfig.BaseURL = server.URL
	cfg.Now = func() time.Time {
		current := base.Add(time.Duration(tick) * 5 * time.Millisecond)
		tick++
		return current
	}
	completed, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, NewTokenPlanChatClient(server.Client()), cfg,
	)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := store.LoadAgentTrace(completed.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	observability, ok := document["observability"].(map[string]any)
	if !ok {
		t.Fatalf("trace has no structured observability: %s", payload)
	}
	stages, ok := observability["stages"].([]any)
	if !ok || len(stages) != 7 {
		t.Fatalf("stages = %#v, want seven bounded stages", observability["stages"])
	}
	for index, want := range []string{
		"package_validation", "claim_selection", "retrieval", "citation_resolution",
		"model", "report_persistence", "trace_persistence",
	} {
		stage := stages[index].(map[string]any)
		wantDurations := []float64{20, 5, 25, 10, 5, 10, 1}
		duration, _ := stage["duration_ms"].(float64)
		durationMatches := duration == wantDurations[index]
		if want == "trace_persistence" {
			durationMatches = duration > 0
		}
		if stage["name"] != want || stage["status"] != "completed" || !durationMatches {
			t.Fatalf("stage[%d] = %#v", index, stage)
		}
		if want == "report_persistence" && stage["definition"] != "immutable_report_preparation" {
			t.Fatalf("report persistence definition = %#v", stage)
		}
		if want == "trace_persistence" && stage["definition"] != "durable_trace_terminal_preparation" {
			t.Fatalf("trace persistence definition = %#v", stage)
		}
	}
	if observability["terminal_protocol"] != "prepared-report-trace-receipt-audit-publish.v2" {
		t.Fatalf("terminal protocol = %#v", observability["terminal_protocol"])
	}
	if observability["citation_resolution_rate"] != float64(1) ||
		observability["independent_publication_source_count"] != float64(1) {
		t.Fatalf("evidence metrics = %#v", observability)
	}
	if observability["reserved_cost_usd"].(float64) <= 0 {
		t.Fatalf("audit budget reservation is missing: %#v", observability)
	}
	freshness, ok := observability["freshness_decisions"].([]any)
	if !ok || len(freshness) == 0 ||
		freshness[0].(map[string]any)["decision"] != EvidenceAuditFreshnessFresh {
		t.Fatalf("freshness decisions = %#v", observability["freshness_decisions"])
	}
	usage := observability["usage"].(map[string]any)
	if usage["status"] != "reported" ||
		usage["prompt_tokens"] != float64(120) ||
		usage["completion_tokens"] != float64(30) ||
		usage["total_tokens"] != float64(150) ||
		usage["cost_usd"] != 0.0042 {
		t.Fatalf("provider usage = %#v", usage)
	}
}

func TestEvidenceAuditRunnerTraceObservabilityCoversFailedAndAbstainedRuns(t *testing.T) {
	t.Run("failed retrieval", func(t *testing.T) {
		store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
		audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-observability-failed", "Evidence comparison only.")
		previous := evidenceAuditRuntimeStageHook
		evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
			if stage == "retrieval" {
				return context.DeadlineExceeded
			}
			return nil
		}
		t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
		if _, err := RunEvidenceAudit(
			context.Background(), store, audit.AuditID,
			&evidenceAuditFakeClient{answers: []string{`must not be called`}}, evidenceAuditRunnerConfig(),
		); err == nil {
			t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
		}
		failed, err := store.LoadEvidenceAudit(audit.AuditID)
		if err != nil || failed.Status != EvidenceAuditFailed {
			t.Fatalf("failed audit = %#v err=%v", failed, err)
		}
		trace, err := store.LoadAgentTrace(failed.TraceID)
		if err != nil || trace.Observability.Stages[2].Status != "failed" ||
			trace.Observability.Usage.Status != "unknown" {
			t.Fatalf("failed observability = %#v err=%v", trace, err)
		}
	})
	t.Run("abstained", func(t *testing.T) {
		store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
		audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-observability-abstain", "Should this patient stop aspirin?")
		completed, err := RunEvidenceAudit(
			context.Background(), store, audit.AuditID,
			&evidenceAuditFakeClient{answers: []string{`must not be called`}}, evidenceAuditRunnerConfig(),
		)
		if err != nil {
			t.Fatal(err)
		}
		trace, err := store.LoadAgentTrace(completed.TraceID)
		if err != nil || trace.Final.Outcome != AgentTraceOutcomeAbstained ||
			trace.Observability.AbstentionReason == "" ||
			trace.Observability.Usage.Status != "unknown" {
			t.Fatalf("abstained observability = %#v err=%v", trace, err)
		}
	})
}

func TestEvidenceAuditFailureCodeDistinguishesCancellationFromTimeout(t *testing.T) {
	if got := evidenceAuditFailureCode(context.DeadlineExceeded); got != "runner_timeout" {
		t.Fatalf("deadline failure code = %q", got)
	}
	if got := evidenceAuditFailureCode(context.Canceled); got != "runner_cancelled" {
		t.Fatalf("cancellation failure code = %q", got)
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

type evidenceAuditResultClient struct {
	mu      sync.Mutex
	results []BookKnowledgeLLMResult
	err     error
	calls   [][]BookKnowledgeMessage
}

func (c *evidenceAuditResultClient) Chat(
	ctx context.Context,
	config BookTokenPlanConfig,
	messages []BookKnowledgeMessage,
) (string, error) {
	result, err := c.ChatWithResult(ctx, config, messages)
	return result.Content, err
}

func (c *evidenceAuditResultClient) ChatWithResult(
	_ context.Context,
	_ BookTokenPlanConfig,
	messages []BookKnowledgeMessage,
) (BookKnowledgeLLMResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, append([]BookKnowledgeMessage(nil), messages...))
	if c.err != nil {
		return BookKnowledgeLLMResult{}, c.err
	}
	index := len(c.calls) - 1
	if index >= len(c.results) {
		return BookKnowledgeLLMResult{}, errors.New("missing fake model result")
	}
	return c.results[index], nil
}

func findEvidenceAuditTraceStage(t *testing.T, trace *AgentTrace, name string) AgentTraceStage {
	t.Helper()
	if trace == nil || trace.Observability == nil {
		t.Fatalf("trace observability is missing: %#v", trace)
	}
	for _, stage := range trace.Observability.Stages {
		if stage.Name == name {
			return stage
		}
	}
	t.Fatalf("trace stage %q is missing", name)
	return AgentTraceStage{}
}

type evidenceAuditBlockingClient struct{}

func (evidenceAuditBlockingClient) Chat(ctx context.Context, _ BookTokenPlanConfig, _ []BookKnowledgeMessage) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

type evidenceAuditSequenceClient struct {
	mu                sync.Mutex
	answers           []string
	blockAfterAnswers bool
	calls             [][]BookKnowledgeMessage
}

func (c *evidenceAuditSequenceClient) Chat(ctx context.Context, _ BookTokenPlanConfig, messages []BookKnowledgeMessage) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, append([]BookKnowledgeMessage(nil), messages...))
	index := len(c.calls) - 1
	if index < len(c.answers) {
		answer := c.answers[index]
		c.mu.Unlock()
		return answer, nil
	}
	c.mu.Unlock()
	if c.blockAfterAnswers {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "", errors.New("missing fake model answer")
}

func evidenceAuditRunnerConfig() EvidenceAuditRunnerConfig {
	return EvidenceAuditRunnerConfig{
		ModelConfig: BookTokenPlanConfig{APIKey: "synthetic-test-key", BaseURL: "https://invalid.test/v1"},
		Timeout:     5 * time.Second,
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
	primary.Book.PublishedAt = testAgentPackageTime().AddDate(0, 0, -10).Format(time.RFC3339)
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
		release.Book.PublishedAt = testAgentPackageTime().AddDate(0, 0, -5-index).Format(time.RFC3339)
		release.ContentHash = "sha256:" + strings.Repeat(string(rune('a'+index)), 64)
		release.Analysis.Claims = []BookAnalysisClaim{
			{ID: releaseID + "-claim-1", Statement: "Synthetic grounded statement supporting evidence", CitationIDs: []string{releaseID + "-c1"}},
			{ID: releaseID + "-claim-2", Statement: "Primary claim two supporting evidence", CitationIDs: []string{releaseID + "-c2"}},
		}
		release.Citations = []BookKnowledgeCitation{
			{CitationID: releaseID + "-c1", BookID: release.BookID, ChunkID: releaseID + "-chunk-1", PublishedAt: release.Book.PublishedAt},
			{CitationID: releaseID + "-c2", BookID: release.BookID, ChunkID: releaseID + "-chunk-2", PublishedAt: release.Book.PublishedAt},
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

func TestRunEvidenceAuditExecutionLockBusyDoesNotFailAnotherOwnersAudit(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(
		t, store, pkg, "execution-lock-busy", "Population evidence comparison.",
	)
	now := testAgentPackageTime()
	claim, err := store.ClaimEvidenceAuditLease(audit.AuditID, "lease-owner", now, time.Minute)
	if err != nil || !claim.Claimed {
		t.Fatalf("ClaimEvidenceAuditLease() claim=%+v err=%v", claim, err)
	}
	lockCtx, lockCancel := context.WithTimeout(context.Background(), time.Second)
	defer lockCancel()
	unlock, err := store.acquireEvidenceAuditExecutionLock(lockCtx, audit.AuditID)
	if err != nil {
		t.Fatalf("acquireEvidenceAuditExecutionLock() error = %v", err)
	}
	defer unlock()

	config := evidenceAuditRunnerConfig()
	config.LeaseOwner = "lease-owner"
	config.Now = func() time.Time { return now }
	config.Timeout = 5 * time.Second
	if _, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, &evidenceAuditFakeClient{}, config,
	); !errors.Is(err, ErrEvidenceAuditExecutionBusy) {
		t.Fatalf("RunEvidenceAudit() error = %v, want execution busy", err)
	}
	loaded, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		t.Fatalf("LoadEvidenceAudit() error = %v", err)
	}
	if loaded.Status == EvidenceAuditFailed {
		t.Fatalf("busy execution incorrectly failed audit: %+v", loaded)
	}
}
