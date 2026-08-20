package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
)

func TestResearchPreflightServiceRanksRealEvidenceAndRedactsBodies(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	knowledge := NewBookKnowledgeStore(t.TempDir())
	target := publishResearchPreflightServicePackage(
		t, knowledge, "target-agent", "release-target", "citation-target",
		"private-target-term evidence body", now.Add(-time.Hour), nil,
	)
	publishResearchPreflightServicePackage(
		t, knowledge, "other-agent", "release-other", "citation-other",
		"unrelated material", now, nil,
	)
	embedder := &rejectingResearchPreflightEmbedder{identity: agentPackageSemanticEmbedderIdentity(target.RetrievalPolicy)}
	knowledge.SetAgentSemanticEmbedder(embedder)
	research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
	service := testResearchPreflightService(knowledge, research, nil, now)

	result, err := service.Evaluate(context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
		Mode: ResearchModeQuick, Question: "private-target-term",
		RequestedSources: []string{ResearchSourceKnowledge},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResearchPreflightStatusReady || len(result.Candidates) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if err := ValidateResearchPreflight(*result); err != nil {
		t.Fatalf("ready Evaluate result is not persistable: %v", err)
	}
	if embedder.calls != 0 {
		t.Fatalf("preflight called semantic embedder %d times", embedder.calls)
	}
	first := result.Candidates[0]
	if first.PackageID != target.PackageID || first.Coverage.EvidenceCount != 1 ||
		first.Coverage.ReleaseCount != 1 || first.Coverage.CitationCount != 1 ||
		len(first.Coverage.ReleaseIDs) != 1 || first.Coverage.ReleaseIDs[0] != "release-target" {
		t.Fatalf("first candidate = %#v", first)
	}
	if first.Budget.ResolvedMode != ResearchModeQuick {
		t.Fatalf("resolved budget = %#v", first.Budget)
	}
	worker := researchPreflightServiceCheck(result.Checks, "worker")
	if worker.Status != ResearchPreflightCheckPass || worker.ResultCode != ResearchPreflightWorkerNotRequired {
		t.Fatalf("knowledge-only Worker check = %#v", worker)
	}
	publicJSON, err := json.Marshal(PublicResearchPreflight(*result))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"private-target-term", "evidence body", "citation-target", "claim-release-target",
		"content_hash", "message_ref", "local_path", "identity_id",
	} {
		if strings.Contains(string(publicJSON), forbidden) {
			t.Fatalf("public preflight leaked %q: %s", forbidden, publicJSON)
		}
	}
	persisted, err := research.LoadResearchPreflightForOwner(result.PreflightID, testResearchPreflightOwnerA)
	if err != nil || persisted.PreflightID != result.PreflightID {
		t.Fatalf("persisted = %#v, %v", persisted, err)
	}
}

func TestResearchPreflightServiceWorkerReadinessUsesHeartbeatOnly(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		setup      func(*testing.T, time.Time) *SourceSyncStore
		wantStatus string
		wantState  string
	}{
		{
			name: "queued jobs do not imply online",
			setup: func(t *testing.T, now time.Time) *SourceSyncStore {
				store := openResearchPreflightSourceStore(t, now)
				subscription, err := store.CreateSubscription(SourceSubscriptionInput{
					SourceType: "wechat_mp_article", SourceAccountKey: "synthetic-account",
					AgentID: "chatlog-agent", Operation: "sync_articles", Enabled: true,
				})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.CreateRun(subscription.ID, ""); err != nil {
					t.Fatal(err)
				}
				return store
			},
			wantStatus: ResearchPreflightStatusBlocked,
			wantState:  SourceAgentObservedOffline,
		},
		{
			name: "stale heartbeat blocks",
			setup: func(t *testing.T, now time.Time) *SourceSyncStore {
				store := openResearchPreflightSourceStore(t, now.Add(-10*time.Minute))
				if _, err := store.HeartbeatAgent(healthyResearchPreflightHeartbeat()); err != nil {
					t.Fatal(err)
				}
				return store
			},
			wantStatus: ResearchPreflightStatusBlocked,
			wantState:  SourceAgentObservedOffline,
		},
		{
			name: "fresh unhealthy heartbeat blocks",
			setup: func(t *testing.T, now time.Time) *SourceSyncStore {
				store := openResearchPreflightSourceStore(t, now)
				heartbeat := healthyResearchPreflightHeartbeat()
				heartbeat.CapabilityHealth["chatlog_read"] = SourceCapabilityHealth{
					Healthy: false, Code: "dependency_unavailable",
				}
				if _, err := store.HeartbeatAgent(heartbeat); err != nil {
					t.Fatal(err)
				}
				return store
			},
			wantStatus: ResearchPreflightStatusBlocked,
			wantState:  SourceAgentObservedDegraded,
		},
		{
			name: "fresh heartbeat without chatlog capability blocks",
			setup: func(t *testing.T, now time.Time) *SourceSyncStore {
				store := openResearchPreflightSourceStore(t, now)
				if _, err := store.HeartbeatAgent(SourceAgentHeartbeat{
					AgentID: "chatlog-agent", WorkerType: "chatlog-worker",
				}); err != nil {
					t.Fatal(err)
				}
				return store
			},
			wantStatus: ResearchPreflightStatusBlocked,
			wantState:  SourceAgentObservedDegraded,
		},
		{
			name: "unrelated healthy capability does not authorize chatlog",
			setup: func(t *testing.T, now time.Time) *SourceSyncStore {
				store := openResearchPreflightSourceStore(t, now)
				if _, err := store.HeartbeatAgent(SourceAgentHeartbeat{
					AgentID: "chatlog-agent", WorkerType: "chatlog-worker",
					Capabilities: []string{"sync_content"},
					CapabilityHealth: map[string]SourceCapabilityHealth{
						"sync_content": {Healthy: true},
					},
				}); err != nil {
					t.Fatal(err)
				}
				return store
			},
			wantStatus: ResearchPreflightStatusBlocked,
			wantState:  SourceAgentObservedDegraded,
		},
		{
			name: "fresh authenticated heartbeat passes",
			setup: func(t *testing.T, now time.Time) *SourceSyncStore {
				store := openResearchPreflightSourceStore(t, now)
				if _, err := store.HeartbeatAgent(healthyResearchPreflightHeartbeat()); err != nil {
					t.Fatal(err)
				}
				return store
			},
			wantStatus: ResearchPreflightStatusReady,
			wantState:  SourceAgentObservedOnline,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			knowledge := NewBookKnowledgeStore(t.TempDir())
			publishResearchPreflightServicePackage(
				t, knowledge, "worker-agent", "release-worker", "citation-worker",
				"worker evidence", now, nil,
			)
			research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
			sourceSync := test.setup(t, now)
			service := testResearchPreflightService(knowledge, research, sourceSync, now)
			result, err := service.Evaluate(context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
				Mode: ResearchModeDeep, Question: "worker evidence",
				RequestedSources: []string{ResearchSourceKnowledge, ResearchSourceChatlog},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q: %#v", result.Status, test.wantStatus, result)
			}
			worker := researchPreflightServiceCheck(result.Checks, "worker")
			if worker.ResultCode != test.wantState {
				t.Fatalf("Worker check = %#v, want state %q", worker, test.wantState)
			}
			if test.wantStatus == ResearchPreflightStatusBlocked && worker.Status != ResearchPreflightCheckBlocked {
				t.Fatalf("blocked Worker check = %#v", worker)
			}
			if test.wantStatus == ResearchPreflightStatusReady && worker.Status != ResearchPreflightCheckPass {
				t.Fatalf("ready Worker check = %#v", worker)
			}
		})
	}
}

func TestResearchPreflightServiceBoundsBudgetAfterResolvedRoute(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	knowledge := NewBookKnowledgeStore(t.TempDir())
	publishResearchPreflightServicePackage(
		t, knowledge, "budget-agent", "release-budget", "citation-budget",
		"budget evidence", now, func(policy *AgentPackageResearchPolicy) {
			policy.MaxIterations = 3
			policy.MaxEvidenceItems = 7
			policy.MaxQuotedChars = 500
			policy.MaxCostUSD = 0.5
		},
	)
	research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
	service := testResearchPreflightService(knowledge, research, nil, now)
	service.QuickBudget = ResearchBudget{
		MaxIterations: 5, MaxEvidenceItems: 10, MaxQuotedChars: 1000,
		MaxModelCalls: 2, MaxCostUSD: 1,
	}
	service.DeepBudget = ResearchBudget{
		MaxIterations: 2, MaxEvidenceItems: 5, MaxQuotedChars: 300,
		MaxModelCalls: 4, MaxCostUSD: 0.25,
	}

	quick, err := service.Evaluate(context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
		Mode: ResearchModeAuto, Question: "budget evidence",
		RequestedSources: []string{ResearchSourceKnowledge},
	})
	if err != nil {
		t.Fatal(err)
	}
	quickBudget := quick.Candidates[0].Budget
	if quickBudget.ResolvedMode != ResearchModeQuick || quickBudget.Limits != (ResearchBudget{
		MaxIterations: 3, MaxEvidenceItems: 7, MaxQuotedChars: 500,
		MaxModelCalls: 2, MaxCostUSD: 0.5,
	}) {
		t.Fatalf("quick budget = %#v", quickBudget)
	}

	deep, err := service.Evaluate(context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
		Mode: ResearchModeAuto, Question: "compare prior evidence",
		RequestedSources: []string{ResearchSourceKnowledge, ResearchSourcePriorRuns},
	})
	if err != nil {
		t.Fatal(err)
	}
	deepBudget := deep.Candidates[0].Budget
	if deepBudget.ResolvedMode != ResearchModeDeep || deepBudget.Limits != service.DeepBudget {
		t.Fatalf("deep budget = %#v, server = %#v", deepBudget, service.DeepBudget)
	}

	service.QuickBudget = ResearchBudget{}
	blocked, err := service.Evaluate(context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
		Mode: ResearchModeQuick, Question: "budget evidence",
		RequestedSources: []string{ResearchSourceKnowledge},
	})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != ResearchPreflightStatusBlocked ||
		!researchPreflightGapCodes(blocked.Gaps)["budget_insufficient"] ||
		researchPreflightServiceCheck(blocked.Checks, "budget").Status != ResearchPreflightCheckBlocked {
		t.Fatalf("insufficient budget result = %#v", blocked)
	}
}

func TestResearchPreflightServiceChecksMixedBudgetsRemainValid(t *testing.T) {
	fitBudget := ResearchPreflightBudget{
		ResolvedMode: ResearchModeQuick,
		Limits: ResearchBudget{
			MaxIterations: 2, MaxEvidenceItems: 20, MaxQuotedChars: 2000,
			MaxModelCalls: 2, MaxCostUSD: 0.5,
		},
	}
	blockedBudget := ResearchPreflightBudget{ResolvedMode: ResearchModeQuick}
	blocked := ResearchPreflightCandidate{
		PackageID: "high-evidence-blocked", PackageVersion: "1.0.0", ContentHash: "sha256:blocked",
		MatchLevel: ResearchPreflightMatchHigh, Readiness: ResearchPreflightCheckBlocked,
		Coverage: ResearchPreflightCoverage{EvidenceCount: 8}, Budget: blockedBudget,
	}
	fit := ResearchPreflightCandidate{
		PackageID: "lower-evidence-fit", PackageVersion: "1.0.0", ContentHash: "sha256:fit",
		MatchLevel: ResearchPreflightMatchMedium, Readiness: ResearchPreflightCheckPass,
		Coverage: ResearchPreflightCoverage{EvidenceCount: 1}, Budget: fitBudget,
	}
	details := map[researchPreflightPackageIdentity]researchPreflightCandidateDetail{}
	for _, entry := range []struct {
		candidate ResearchPreflightCandidate
		fits      bool
	}{
		{candidate: blocked, fits: false},
		{candidate: fit, fits: true},
	} {
		identity, ok := researchPreflightIdentity(AgentPackage{
			PackageID: entry.candidate.PackageID, Version: entry.candidate.PackageVersion,
			ContentHash: entry.candidate.ContentHash,
		})
		if !ok {
			t.Fatalf("invalid test identity: %#v", entry.candidate)
		}
		details[identity] = researchPreflightCandidateDetail{
			budget: entry.candidate.Budget, budgetFits: entry.fits,
		}
	}

	for _, candidates := range [][]ResearchPreflightCandidate{{blocked, fit}, {fit, blocked}} {
		checks := researchPreflightServiceChecks(
			ResearchPreflightStatusReady, candidates, details, researchPreflightServiceSignals{},
			[]string{ResearchSourceKnowledge}, ResearchPreflightWorkerNotRequired, ResearchModeQuick,
		)
		budget := researchPreflightServiceCheck(checks, "budget")
		if budget.Status != ResearchPreflightCheckWarning || budget.ResultCode != "candidate_specific" ||
			budget.NextAction != "review_research_budget" {
			t.Fatalf("mixed budget check = %#v for candidates %#v", budget, candidates)
		}
		for _, check := range checks {
			if check.Status == ResearchPreflightCheckBlocked {
				t.Fatalf("ready mixed result has blocked global check: %#v", checks)
			}
		}
		result := ResearchPreflight{Status: ResearchPreflightStatusReady, Candidates: candidates, Checks: checks}
		if err := ValidateResearchPreflight(result); err != nil {
			t.Fatalf("mixed result is not persistable: %v; %#v", err, result)
		}
	}
}

func TestResearchPreflightServiceCorruptionBlocksButStorageErrorsRetry(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

	t.Run("package artifact corruption is a typed block", func(t *testing.T) {
		knowledge := NewBookKnowledgeStore(t.TempDir())
		pkg := publishResearchPreflightServicePackage(
			t, knowledge, "corrupt-package", "release-corrupt-package", "citation-corrupt-package",
			"package evidence", now, nil,
		)
		if err := writeFileAtomically(knowledge.AgentPackagePath(pkg.ContentHash), []byte(`{"broken":true}`)); err != nil {
			t.Fatal(err)
		}
		research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
		result, err := testResearchPreflightService(knowledge, research, nil, now).Evaluate(
			context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
				Mode: ResearchModeQuick, Question: "package evidence",
				RequestedSources: []string{ResearchSourceKnowledge},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != ResearchPreflightStatusBlocked ||
			researchPreflightServiceCheck(result.Checks, "package_integrity").Status != ResearchPreflightCheckBlocked {
			t.Fatalf("package corruption result = %#v", result)
		}
	})

	t.Run("evaluation corruption is a typed block", func(t *testing.T) {
		knowledge := NewBookKnowledgeStore(t.TempDir())
		pkg := publishResearchPreflightServicePackage(
			t, knowledge, "corrupt-evaluation", "release-corrupt-evaluation", "citation-corrupt-evaluation",
			"evaluation evidence", now, nil,
		)
		if err := writeFileAtomically(knowledge.AgentPackageEvaluationPath(pkg.ContentHash), []byte(`{"broken":true}`)); err != nil {
			t.Fatal(err)
		}
		research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
		result, err := testResearchPreflightService(knowledge, research, nil, now).Evaluate(
			context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
				Mode: ResearchModeQuick, Question: "evaluation evidence",
				RequestedSources: []string{ResearchSourceKnowledge},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != ResearchPreflightStatusBlocked ||
			researchPreflightServiceCheck(result.Checks, "trusted_evaluation").Status != ResearchPreflightCheckBlocked {
			t.Fatalf("evaluation corruption result = %#v", result)
		}
	})

	t.Run("Research Store unavailability remains retryable", func(t *testing.T) {
		knowledge := NewBookKnowledgeStore(t.TempDir())
		publishResearchPreflightServicePackage(
			t, knowledge, "retryable-store", "release-retryable", "citation-retryable",
			"retryable evidence", now, nil,
		)
		research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
		if err := research.Close(); err != nil {
			t.Fatal(err)
		}
		result, err := testResearchPreflightService(knowledge, research, nil, now).Evaluate(
			context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
				Mode: ResearchModeQuick, Question: "retryable evidence",
				RequestedSources: []string{ResearchSourceKnowledge},
			},
		)
		if result != nil || !errors.Is(err, ErrResearchPreflightUnavailable) {
			t.Fatalf("retryable result = %#v, %v", result, err)
		}
	})

	t.Run("transient Package artifact load remains retryable", func(t *testing.T) {
		knowledge := NewBookKnowledgeStore(t.TempDir())
		publishResearchPreflightServicePackage(
			t, knowledge, "retryable-package", "release-retryable-package", "citation-retryable-package",
			"retryable package evidence", now, nil,
		)
		research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
		previous := agentPackageArtifactLoadHook
		loads := 0
		agentPackageArtifactLoadHook = func(context.Context, string) error {
			loads++
			if loads == 1 {
				return &os.PathError{Op: "read", Path: "synthetic-package", Err: os.ErrPermission}
			}
			return nil
		}
		t.Cleanup(func() { agentPackageArtifactLoadHook = previous })
		result, err := testResearchPreflightService(knowledge, research, nil, now).Evaluate(
			context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
				Mode: ResearchModeQuick, Question: "retryable package evidence",
				RequestedSources: []string{ResearchSourceKnowledge},
			},
		)
		if result != nil || !errors.Is(err, ErrResearchPreflightUnavailable) {
			t.Fatalf("transient Package load result = %#v, %v (loads=%d)", result, err, loads)
		}
	})
}

func TestResearchPreflightServiceNoEligiblePackageGuidesAgentCompletion(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	knowledge := NewBookKnowledgeStore(t.TempDir())
	research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
	result, err := testResearchPreflightService(knowledge, research, nil, now).Evaluate(
		context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
			Mode: ResearchModeQuick, Question: "synthetic question",
			RequestedSources: []string{ResearchSourceKnowledge},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResearchPreflightStatusBlocked || len(result.Candidates) != 0 {
		t.Fatalf("result = %#v", result)
	}
	gap := researchPreflightServiceGap(result.Gaps, "no_eligible_package")
	if gap.NextAction != "complete_agent_package" {
		t.Fatalf("no eligible gap = %#v", gap)
	}
	if result.PreflightID == "" || result.ExpiresAt == "" {
		t.Fatalf("blocked preflight was not persisted: %#v", result)
	}
}

func TestResearchPreflightWorkerReadinessPreservesUnavailableCause(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	source := openResearchPreflightSourceStore(t, now)
	if _, err := source.db.Exec(`DROP TABLE source_agents`); err != nil {
		t.Fatal(err)
	}
	_, cause := source.GetSourceAgent("chatlog-agent")
	var sqliteCause sqlite3.Error
	if !errors.As(cause, &sqliteCause) {
		t.Fatalf("SourceSync cause=%T %v", cause, cause)
	}
	service := &ResearchPreflightService{SourceSync: source}
	_, _, err := service.workerReadiness(ResearchPreflightRequest{
		RequestedSources: []string{ResearchSourceChatlog},
	}, now)
	if !errors.Is(err, ErrResearchPreflightUnavailable) || !errors.Is(err, cause) {
		t.Fatalf("worker readiness error=%v", err)
	}
}

func TestResearchPreflightServiceProbeIsLocalBoundedAndContextAware(t *testing.T) {
	if researchPreflightProbeMaxReleases != 8 || researchPreflightProbeMaxUnits != 64 ||
		researchPreflightProbeMaxResults != 8 {
		t.Fatalf("probe limits changed: releases=%d units=%d results=%d",
			researchPreflightProbeMaxReleases, researchPreflightProbeMaxUnits, researchPreflightProbeMaxResults)
	}

	t.Run("release scan is bounded and never embeds", func(t *testing.T) {
		store := NewBookKnowledgeStore(t.TempDir())
		embedder := &rejectingResearchPreflightEmbedder{identity: agentPackageSemanticEmbedderIdentity(validAgentPackage().RetrievalPolicy)}
		store.SetAgentSemanticEmbedder(embedder)
		pkg := AgentPackage{RetrievalPolicy: validAgentPackage().RetrievalPolicy}
		for index := 0; index < researchPreflightProbeMaxReleases+1; index++ {
			statement := "unrelated evidence"
			if index == researchPreflightProbeMaxReleases {
				statement = "beyond-release-boundary"
			}
			ref := saveResearchPreflightProbeRelease(t, store, index, []string{statement})
			pkg.Releases = append(pkg.Releases, ref)
		}
		results, err := researchPreflightProbeEvidence(context.Background(), store, pkg, "beyond-release-boundary")
		if err != nil || len(results) != 0 || embedder.calls != 0 {
			t.Fatalf("bounded Release probe = %#v, %v, embed calls=%d", results, err, embedder.calls)
		}
	})

	t.Run("claim scan and retained results are bounded", func(t *testing.T) {
		store := NewBookKnowledgeStore(t.TempDir())
		statements := make([]string, researchPreflightProbeMaxUnits+1)
		for index := range statements {
			statements[index] = "matching evidence"
		}
		statements[researchPreflightProbeMaxUnits] = "beyond-unit-boundary"
		ref := saveResearchPreflightProbeRelease(t, store, 0, statements)
		pkg := AgentPackage{
			RetrievalPolicy: validAgentPackage().RetrievalPolicy,
			Releases:        []AgentPackageReleaseRef{ref},
		}
		beyond, err := researchPreflightProbeEvidence(context.Background(), store, pkg, "beyond-unit-boundary")
		if err != nil || len(beyond) != 0 {
			t.Fatalf("bounded unit probe = %#v, %v", beyond, err)
		}
		matched, err := researchPreflightProbeEvidence(context.Background(), store, pkg, "matching evidence")
		if err != nil || len(matched) != researchPreflightProbeMaxResults {
			t.Fatalf("bounded results = %d, %v", len(matched), err)
		}
	})

	t.Run("canceled context stops before Store work", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		results, err := researchPreflightProbeEvidence(ctx, NewBookKnowledgeStore(t.TempDir()), AgentPackage{}, "query")
		if results != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled probe = %#v, %v", results, err)
		}
	})
}

func TestResearchPreflightServiceBoundsPackageCatalogBeforeEvaluation(t *testing.T) {
	if researchPreflightPackagePageSize != 100 || researchPreflightPackageMaxPages != 4 ||
		researchPreflightPackageMaxRecords != 400 {
		t.Fatalf("catalog limits changed: page_size=%d pages=%d records=%d",
			researchPreflightPackagePageSize, researchPreflightPackageMaxPages, researchPreflightPackageMaxRecords)
	}
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		prefixCount int
		wantError   bool
	}{
		{name: "candidate is discovered on second page", prefixCount: researchPreflightPackagePageSize},
		{name: "exact catalog limit completes", prefixCount: researchPreflightPackageMaxRecords - 1},
		{name: "catalog limit plus one fails before artifact load", prefixCount: researchPreflightPackageMaxRecords, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			knowledge := NewBookKnowledgeStore(t.TempDir())
			publishResearchPreflightServicePackage(
				t, knowledge, "catalog-target", "release-catalog", "citation-catalog",
				"catalog boundary evidence", now, nil,
			)
			prependResearchPreflightCatalogRecords(t, knowledge, test.prefixCount)
			research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
			previous := agentPackageArtifactLoadHook
			artifactLoads := 0
			agentPackageArtifactLoadHook = func(context.Context, string) error {
				artifactLoads++
				return nil
			}
			t.Cleanup(func() { agentPackageArtifactLoadHook = previous })

			result, err := testResearchPreflightService(knowledge, research, nil, now).Evaluate(
				context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
					Mode: ResearchModeQuick, Question: "catalog boundary evidence",
					RequestedSources: []string{ResearchSourceKnowledge},
				},
			)
			if test.wantError {
				if result != nil || !errors.Is(err, ErrResearchPreflightUnavailable) || artifactLoads != 0 {
					t.Fatalf("over-limit catalog = %#v, %v, artifact loads=%d", result, err, artifactLoads)
				}
				var saved int
				if countErr := research.db.QueryRow(`SELECT COUNT(*) FROM research_preflights`).Scan(&saved); countErr != nil || saved != 0 {
					t.Fatalf("saved preflights = %d, %v", saved, countErr)
				}
				return
			}
			if err != nil || result == nil || result.Status != ResearchPreflightStatusReady ||
				len(result.Candidates) != 1 || result.Candidates[0].PackageID != "catalog-target" || artifactLoads == 0 {
				t.Fatalf("bounded catalog result = %#v, %v, artifact loads=%d", result, err, artifactLoads)
			}
		})
	}

	t.Run("duplicate identities across pages fail closed", func(t *testing.T) {
		first := make([]AgentPackageRecord, researchPreflightPackagePageSize+1)
		for index := range first {
			first[index] = researchPreflightCatalogRecord(index)
		}
		calls := 0
		records, err := loadResearchPreflightPackageCatalog(context.Background(), func(after string, limit int) ([]AgentPackageRecord, error) {
			calls++
			if calls == 1 {
				return first, nil
			}
			return []AgentPackageRecord{researchPreflightCatalogRecord(50)}, nil
		})
		if records != nil || !errors.Is(err, ErrResearchPreflightUnavailable) {
			t.Fatalf("duplicate catalog = %#v, %v", records, err)
		}
	})

	t.Run("repeated cursor page fails closed", func(t *testing.T) {
		page := make([]AgentPackageRecord, researchPreflightPackagePageSize+1)
		for index := range page {
			page[index] = researchPreflightCatalogRecord(index)
		}
		records, err := loadResearchPreflightPackageCatalog(context.Background(), func(string, int) ([]AgentPackageRecord, error) {
			return append([]AgentPackageRecord(nil), page...), nil
		})
		if records != nil || !errors.Is(err, ErrResearchPreflightUnavailable) {
			t.Fatalf("repeated cursor catalog = %#v, %v", records, err)
		}
	})

	t.Run("canceled context stops before listing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		records, err := loadResearchPreflightPackageCatalog(ctx, func(string, int) ([]AgentPackageRecord, error) {
			calls++
			return nil, nil
		})
		if records != nil || !errors.Is(err, context.Canceled) || calls != 0 {
			t.Fatalf("canceled catalog = %#v, %v, calls=%d", records, err, calls)
		}
	})
}

func TestResearchPreflightServiceDeterministicCatalogWorkCeiling(t *testing.T) {
	result := exerciseResearchPreflightServiceAtCatalogLimit(t)
	if result.catalogUnits != researchPreflightPackageMaxRecords {
		t.Fatalf("service catalog units = %d, want %d", result.catalogUnits, researchPreflightPackageMaxRecords)
	}
	if result.publishedUnits != 4 || result.artifactLoads != result.publishedUnits {
		t.Fatalf("service published/artifact units = %d/%d, want 4/4", result.publishedUnits, result.artifactLoads)
	}
	if result.candidates != researchPreflightCandidateMax {
		t.Fatalf("service candidates = %d, want ceiling %d", result.candidates, researchPreflightCandidateMax)
	}
}

type researchPreflightServiceWorkResult struct {
	catalogUnits   int
	publishedUnits int
	artifactLoads  int
	candidates     int
}

func exerciseResearchPreflightServiceAtCatalogLimit(t *testing.T) researchPreflightServiceWorkResult {
	t.Helper()
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	knowledge := NewBookKnowledgeStore(t.TempDir())
	const publishedUnits = 4
	for index := 0; index < publishedUnits; index++ {
		publishResearchPreflightServicePackage(
			t, knowledge, fmt.Sprintf("bounded-service-agent-%02d", index),
			fmt.Sprintf("bounded-service-release-%02d", index),
			fmt.Sprintf("bounded-service-citation-%02d", index),
			"bounded service evidence", now, nil,
		)
	}
	prependResearchPreflightCatalogRecords(t, knowledge, researchPreflightPackageMaxRecords-publishedUnits)
	research := openResearchPreflightServiceStore(t, knowledge.Root(), now)
	previous := agentPackageArtifactLoadHook
	artifactLoads := 0
	agentPackageArtifactLoadHook = func(context.Context, string) error {
		artifactLoads++
		return nil
	}
	t.Cleanup(func() { agentPackageArtifactLoadHook = previous })
	result, err := testResearchPreflightService(knowledge, research, nil, now).Evaluate(
		context.Background(), testResearchPreflightOwnerA, ResearchPreflightRequest{
			Mode: ResearchModeQuick, Question: "bounded service evidence",
			RequestedSources: []string{ResearchSourceKnowledge},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResearchPreflightStatusReady || len(result.Candidates) != researchPreflightCandidateMax ||
		result.Candidates[0].PackageID != "bounded-service-agent-00" {
		t.Fatalf("bounded service result = %#v", result)
	}
	return researchPreflightServiceWorkResult{
		catalogUnits: researchPreflightPackageMaxRecords, publishedUnits: publishedUnits,
		artifactLoads: artifactLoads, candidates: len(result.Candidates),
	}
}

func testResearchPreflightService(
	knowledge *BookKnowledgeStore,
	research *ResearchStore,
	sourceSync *SourceSyncStore,
	now time.Time,
) *ResearchPreflightService {
	return &ResearchPreflightService{
		Knowledge: knowledge, Research: research, SourceSync: sourceSync,
		QuickBudget: ResearchBudget{
			MaxIterations: 4, MaxEvidenceItems: 50, MaxQuotedChars: 4000,
			MaxModelCalls: 2, MaxCostUSD: 0.5,
		},
		DeepBudget: ResearchBudget{
			MaxIterations: 8, MaxEvidenceItems: 200, MaxQuotedChars: 16000,
			MaxModelCalls: 8, MaxCostUSD: 2,
		},
		Now: func() time.Time { return now },
	}
}

func publishResearchPreflightServicePackage(
	t *testing.T,
	store *BookKnowledgeStore,
	packageID string,
	releaseID string,
	citationID string,
	statement string,
	publishedAt time.Time,
	editPolicy func(*AgentPackageResearchPolicy),
) AgentPackage {
	t.Helper()
	release := agentPackageTestRelease()
	release.ReleaseID = releaseID
	release.BookID = "book-" + releaseID
	release.Book.BookID = release.BookID
	release.ContentHash = sha256Fingerprint([]byte("content-" + releaseID))
	release.Analysis.Claims[0].ID = "claim-" + releaseID
	release.Analysis.Claims[0].Statement = statement
	release.Analysis.Claims[0].CitationIDs = []string{citationID}
	release.Citations = []BookKnowledgeCitation{{
		CitationID: citationID, BookID: release.BookID, ChunkID: "chunk-" + releaseID,
	}}
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}

	pkg := validAgentPackageV4()
	pkg.PackageID = packageID
	pkg.Version = "1.0.0"
	pkg.Releases = []AgentPackageReleaseRef{{
		ReleaseID: releaseID, ContentHash: release.ContentHash, CitationIDs: []string{citationID},
	}}
	pkg.ResearchPolicy = cloneAgentPackageResearchPolicy(pkg.ResearchPolicy)
	if editPolicy != nil {
		editPolicy(pkg.ResearchPolicy)
	}
	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	submitted := loadResearchEvaluationFixture(t)
	trusted := trustedResearchEvaluationFixture(submitted)
	if err := store.SaveTrustedAgentEvaluationSuite(finalized, trusted); err != nil {
		t.Fatal(err)
	}
	resolved, report, err := EvaluateAgentPackageAgainstTrustedSuite(store, finalized, submitted, publishedAt)
	if err != nil || !report.Passed {
		t.Fatalf("evaluation = %#v, %v", report, err)
	}
	if err := store.SaveAgentPackageEvaluation(finalized, resolved, report); err != nil {
		t.Fatal(err)
	}
	published, _, err := PublishAgentPackage(
		store, finalized, "publish-"+packageID, AgentPackageKnownToolIDs(), publishedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	return *published
}

func openResearchPreflightServiceStore(t *testing.T, root string, now time.Time) *ResearchStore {
	t.Helper()
	store, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func openResearchPreflightSourceStore(t *testing.T, now time.Time) *SourceSyncStore {
	t.Helper()
	store, err := newSourceSyncStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func healthyResearchPreflightHeartbeat() SourceAgentHeartbeat {
	return SourceAgentHeartbeat{
		AgentID: "chatlog-agent", WorkerType: "chatlog-worker",
		Capabilities: []string{"chatlog_read"},
		CapabilityHealth: map[string]SourceCapabilityHealth{
			"chatlog_read": {Healthy: true},
		},
	}
}

func researchPreflightServiceCheck(checks []ResearchPreflightCheck, code string) ResearchPreflightCheck {
	for _, check := range checks {
		if check.Code == code {
			return check
		}
	}
	return ResearchPreflightCheck{}
}

func researchPreflightServiceGap(gaps []ResearchPreflightGap, code string) ResearchPreflightGap {
	for _, gap := range gaps {
		if gap.Code == code {
			return gap
		}
	}
	return ResearchPreflightGap{}
}

func saveResearchPreflightProbeRelease(
	t *testing.T,
	store *BookKnowledgeStore,
	index int,
	statements []string,
) AgentPackageReleaseRef {
	t.Helper()
	release := agentPackageTestRelease()
	release.ReleaseID = fmt.Sprintf("probe-release-%02d", index)
	release.BookID = fmt.Sprintf("probe-book-%02d", index)
	release.Book.BookID = release.BookID
	release.ContentHash = sha256Fingerprint([]byte(release.ReleaseID))
	release.Analysis.Claims = make([]BookAnalysisClaim, 0, len(statements))
	release.Citations = make([]BookKnowledgeCitation, 0, len(statements))
	citationIDs := make([]string, 0, len(statements))
	for claimIndex, statement := range statements {
		claimID := fmt.Sprintf("probe-claim-%02d-%03d", index, claimIndex)
		citationID := fmt.Sprintf("probe-citation-%02d-%03d", index, claimIndex)
		release.Analysis.Claims = append(release.Analysis.Claims, BookAnalysisClaim{
			ID: claimID, Statement: statement, CitationIDs: []string{citationID},
		})
		release.Citations = append(release.Citations, BookKnowledgeCitation{
			CitationID: citationID, BookID: release.BookID, ChunkID: "chunk-" + claimID,
		})
		citationIDs = append(citationIDs, citationID)
	}
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	return AgentPackageReleaseRef{
		ReleaseID: release.ReleaseID, ContentHash: release.ContentHash, CitationIDs: citationIDs,
	}
}

func prependResearchPreflightCatalogRecords(t *testing.T, store *BookKnowledgeStore, count int) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	manifest, err := store.loadAgentPackageManifestUnlocked()
	if err != nil {
		t.Fatal(err)
	}
	prefix := make([]AgentPackageRecord, count)
	for index := range prefix {
		prefix[index] = researchPreflightCatalogRecord(index)
	}
	manifest.Packages = append(prefix, manifest.Packages...)
	if err := store.writeAgentPackageManifestUnlocked(manifest); err != nil {
		t.Fatal(err)
	}
}

func researchPreflightCatalogRecord(index int) AgentPackageRecord {
	return AgentPackageRecord{
		PackageID: fmt.Sprintf("catalog-draft-%03d", index), Version: "1.0.0",
		ContentHash:    sha256Fingerprint([]byte(fmt.Sprintf("catalog-draft-%03d", index))),
		LifecycleState: AgentPackageDraft,
	}
}

type rejectingResearchPreflightEmbedder struct {
	identity string
	calls    int
}

func (e *rejectingResearchPreflightEmbedder) Identity() string { return e.identity }

func (e *rejectingResearchPreflightEmbedder) Embed(context.Context, []string) ([][]float64, error) {
	e.calls++
	return nil, errors.New("preflight must not call semantic embedding")
}
