package app

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"
)

type fakeResearchStageModel struct {
	mu              sync.Mutex
	calls           map[ResearchModelRole]int
	verifierVerdict string
	err             error
}

func (m *fakeResearchStageModel) Run(_ context.Context, role ResearchModelRole, _ BookTokenPlanConfig, messages []BookKnowledgeMessage, output any) (ResearchModelUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls == nil {
		m.calls = map[ResearchModelRole]int{}
	}
	m.calls[role]++
	if m.err != nil {
		return ResearchModelUsage{}, m.err
	}
	evidenceIDs := sortedResearchReferenceIDs(messages, "evidence")
	citationIDs := sortedResearchReferenceIDs(messages, "citation")
	conclusionIDs := sortedResearchReferenceIDs(messages, "conclusion")
	switch value := output.(type) {
	case *ResearchPlannerOutput:
		*value = ResearchPlannerOutput{
			DecisionSummary: "Retrieve a bounded synthetic chat range",
			ToolCalls: []ResearchPlannedToolCall{{
				Tool: ResearchWorkerToolSearchChatlog,
				Arguments: map[string]any{
					"talker_ref": "room-a", "time_from": "2032-01-01T00:00:00Z",
					"time_to": "2032-01-02T00:00:00Z", "limit": 20,
				},
			}},
		}
	case *ResearchExtractorOutput:
		if len(evidenceIDs) == 0 {
			return ResearchModelUsage{}, errors.New("no evidence marker")
		}
		*value = ResearchExtractorOutput{
			DecisionSummary: "Extract one grounded fact",
			Facts: []ResearchFact{{
				FactID: "fact-a", Kind: "observation", Summary: "Synthetic fact",
				EvidenceIDs: []string{evidenceIDs[0]}, Confidence: 0.9, ReviewState: ResearchReviewPending,
			}},
		}
	case *ResearchSynthesizerOutput:
		if len(evidenceIDs) == 0 {
			return ResearchModelUsage{}, errors.New("no evidence marker")
		}
		conclusion := ResearchConclusionDraft{
			ConclusionID: "conclusion-a", Text: "Synthetic grounded conclusion",
			SupportEvidenceIDs: []string{evidenceIDs[0]}, Confidence: 0.9,
		}
		if len(citationIDs) > 0 {
			conclusion.CitationIDs = []string{citationIDs[0]}
		}
		*value = ResearchSynthesizerOutput{DecisionSummary: "Synthesize grounded evidence", Conclusions: []ResearchConclusionDraft{conclusion}}
	case *ResearchVerifierOutput:
		verdict := m.verifierVerdict
		if verdict == "" {
			verdict = ResearchVerifierVerified
		}
		*value = ResearchVerifierOutput{DecisionSummary: "Verify all accessible support", Verdict: verdict}
		if verdict == ResearchVerifierVerified {
			value.VerifiedConclusionIDs = conclusionIDs
		} else {
			value.Gaps = []string{"synthetic_gap"}
		}
	}
	return ResearchModelUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}, nil
}

func (m *fakeResearchStageModel) callCount(role ResearchModelRole) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[role]
}

func TestResearchOrchestratorQuickPathCompletesWithGroundedPackageRetrieval(t *testing.T) {
	orchestrator, research, pkg, model := newResearchOrchestratorTestHarness(t)
	run := createResearchOrchestratorRun(t, research, pkg, "quick-path", ResearchModeQuick, []string{ResearchSourceKnowledge}, "grounded")
	result := advanceResearchUntilTerminal(t, orchestrator, run.RunID, 12)
	if result.Run.Status != ResearchCompleted || result.Outcome != ResearchOutcomeCompleted {
		t.Fatalf("quick result = %#v", result)
	}
	evidence, err := research.ListEvidence(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	conclusions, err := research.ListVerifiedResearchConclusions(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) == 0 || len(conclusions) != 1 || model.callCount(ResearchRoleSynthesizer) != 1 || model.callCount(ResearchRoleVerifier) != 1 {
		t.Fatalf("evidence=%#v conclusions=%#v calls=%#v", evidence, conclusions, model.calls)
	}
}

func TestResearchOrchestratorDeepPathWaitsResumesAndSurvivesRestart(t *testing.T) {
	orchestrator, research, pkg, model := newResearchOrchestratorTestHarness(t)
	run := createResearchOrchestratorRun(t, research, pkg, "deep-path", ResearchModeDeep,
		[]string{ResearchSourceKnowledge, ResearchSourceChatlog}, "cross source history")
	first, err := orchestrator.Advance(context.Background(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Run.Status != ResearchRetrieving || model.callCount(ResearchRolePlanner) != 1 {
		t.Fatalf("planning result = %#v calls=%#v", first, model.calls)
	}
	waiting, err := orchestrator.Advance(context.Background(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.WaitReason != ResearchWaitWorkerPending || waiting.Run.Status != ResearchRetrieving || model.callCount(ResearchRoleExtractor) != 0 {
		t.Fatalf("waiting result = %#v calls=%#v", waiting, model.calls)
	}

	job, err := research.ClaimWorkerJob("chatlog-agent", time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim job=%#v error=%v", job, err)
	}
	if _, err := research.CompleteWorkerJob(job.JobID, "chatlog-agent", job.RequestHash, ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog},
		Items: []ResearchWorkerEvidenceCandidate{{
			SourceType: ResearchEvidenceSourceChatlog, SourceRole: ResearchEvidenceRoleUserHistory,
			Content: "Synthetic private history", Privacy: ResearchEvidencePrivacyPrivate, Selected: true,
			Locator: ResearchEvidenceLocator{WorkerID: "chatlog-agent", ConversationRef: "room-a", MessageRef: "message-a"},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewResearchOrchestrator(orchestrator.config)
	if err != nil {
		t.Fatal(err)
	}
	result := advanceResearchUntilTerminal(t, restarted, run.RunID, 16)
	if result.Run.Status != ResearchCompleted || model.callCount(ResearchRolePlanner) != 1 ||
		model.callCount(ResearchRoleExtractor) != 1 || model.callCount(ResearchRoleSynthesizer) != 1 ||
		model.callCount(ResearchRoleVerifier) != 1 {
		t.Fatalf("deep result=%#v calls=%#v", result, model.calls)
	}
}

func TestResearchOrchestratorVerifierGapsReturnToPlanningWithinBound(t *testing.T) {
	orchestrator, research, pkg, model := newResearchOrchestratorTestHarness(t)
	model.verifierVerdict = ResearchVerifierGaps
	run := createResearchOrchestratorRun(t, research, pkg, "bounded-gaps", ResearchModeQuick, []string{ResearchSourceKnowledge}, "grounded")
	if _, err := research.db.Exec(`UPDATE research_runs SET budget_json = json_set(budget_json, '$.max_iterations', 1) WHERE run_id = ?`, run.RunID); err != nil {
		t.Fatal(err)
	}
	result := advanceResearchUntilTerminal(t, orchestrator, run.RunID, 12)
	if result.Run.Status != ResearchInsufficient || result.Outcome != ResearchOutcomePartialEvidence || model.callCount(ResearchRoleVerifier) != 1 {
		t.Fatalf("bounded verifier result=%#v calls=%#v", result, model.calls)
	}
}

func TestResearchOrchestratorCaseComparisonRequiresTransferWarning(t *testing.T) {
	orchestrator, research, pkg, _ := newResearchOrchestratorTestHarness(t)
	run := createResearchOrchestratorRun(t, research, pkg, "case-warning", ResearchModeQuick, []string{ResearchSourceKnowledge}, "grounded")
	if _, err := research.db.Exec(`UPDATE research_runs SET route_reasons_json = ?, budget_json = json_set(budget_json, '$.max_iterations', 1) WHERE run_id = ?`,
		`["case_comparison"]`, run.RunID); err != nil {
		t.Fatal(err)
	}
	result := advanceResearchUntilTerminal(t, orchestrator, run.RunID, 12)
	if result.Run.Status != ResearchInsufficient || result.Outcome != ResearchOutcomePartialEvidence {
		t.Fatalf("case warning result = %#v", result)
	}
}

func TestResearchOrchestratorTypedOutcomesAndCancellation(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrResearchWorkerTerminal, ResearchOutcomeWorkerOffline},
		{ErrResearchIdentityAmbiguous, ResearchOutcomeIdentityAmbiguous},
		{ErrResearchZeroHit, ResearchOutcomeZeroHit},
		{ErrResearchPartialEvidence, ResearchOutcomePartialEvidence},
		{ErrResearchBudgetExhausted, ResearchOutcomeBudgetExhausted},
		{ErrResearchCitationMismatch, ResearchOutcomeCitationMismatch},
		{ErrResearchEvidenceSourceChanged, ResearchOutcomeSourceChanged},
		{context.DeadlineExceeded, ResearchOutcomeModelTimeout},
	}
	for _, testCase := range tests {
		if got := ClassifyResearchOrchestratorOutcome(testCase.err); got != testCase.want {
			t.Fatalf("ClassifyResearchOrchestratorOutcome(%v)=%q want %q", testCase.err, got, testCase.want)
		}
	}

	orchestrator, research, pkg, model := newResearchOrchestratorTestHarness(t)
	run := createResearchOrchestratorRun(t, research, pkg, "cancel", ResearchModeQuick, []string{ResearchSourceKnowledge}, "grounded")
	canceled, err := research.TransitionRun(run.RunID, run.Version, ResearchCanceled, ResearchTransition{Code: "user_cancel", Actor: "user"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := orchestrator.Advance(context.Background(), canceled.RunID)
	if err != nil || result.Run.Status != ResearchCanceled || model.callCount(ResearchRoleSynthesizer) != 0 {
		t.Fatalf("canceled result=%#v error=%v calls=%#v", result, err, model.calls)
	}
}

func TestResearchOrchestratorModelInvocationIsDurablyIdempotent(t *testing.T) {
	orchestrator, research, pkg, model := newResearchOrchestratorTestHarness(t)
	run := createResearchOrchestratorRun(t, research, pkg, "model-idempotency", ResearchModeDeep, []string{ResearchSourceChatlog}, "history")
	messages := []BookKnowledgeMessage{{Role: "user", Content: "Plan a bounded retrieval"}}
	var first ResearchPlannerOutput
	if _, err := orchestrator.invokeModel(context.Background(), run, ResearchRolePlanner, "planner:0", messages, &first); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewResearchOrchestrator(orchestrator.config)
	if err != nil {
		t.Fatal(err)
	}
	var second ResearchPlannerOutput
	if _, err := restarted.invokeModel(context.Background(), run, ResearchRolePlanner, "planner:0", messages, &second); err != nil {
		t.Fatal(err)
	}
	if model.callCount(ResearchRolePlanner) != 1 || len(first.ToolCalls) != 1 || len(second.ToolCalls) != 1 {
		t.Fatalf("calls=%#v first=%#v second=%#v", model.calls, first, second)
	}
	var invocations, inputTokens, outputTokens int
	if err := research.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0)
		FROM research_model_invocations WHERE run_id = ?`, run.RunID).Scan(&invocations, &inputTokens, &outputTokens); err != nil {
		t.Fatal(err)
	}
	if invocations != 1 || inputTokens != 10 || outputTokens != 5 {
		t.Fatalf("invocations=%d input=%d output=%d", invocations, inputTokens, outputTokens)
	}
}

func TestResearchOrchestratorKeepsRepeatedDraftIDsIsolatedByRun(t *testing.T) {
	orchestrator, research, pkg, _ := newResearchOrchestratorTestHarness(t)
	first := createResearchOrchestratorRun(t, research, pkg, "conclusion-scope-a", ResearchModeQuick, []string{ResearchSourceKnowledge}, "grounded")
	second := createResearchOrchestratorRun(t, research, pkg, "conclusion-scope-b", ResearchModeQuick, []string{ResearchSourceKnowledge}, "grounded")
	draft := []ResearchConclusionDraft{{ConclusionID: "model-local-id", Text: "Synthetic", SupportEvidenceIDs: []string{"evidence-a"}, Confidence: 0.9}}
	if err := orchestrator.promoteVerifiedConclusions(first.RunID, draft); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.promoteVerifiedConclusions(second.RunID, draft); err != nil {
		t.Fatal(err)
	}
	firstItems, err := research.ListVerifiedResearchConclusions(first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	secondItems, err := research.ListVerifiedResearchConclusions(second.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstItems) != 1 || len(secondItems) != 1 || firstItems[0].ConclusionID == secondItems[0].ConclusionID {
		t.Fatalf("first=%#v second=%#v", firstItems, secondItems)
	}
}

func TestResearchOrchestratorCoordinatorAdvancesDurableRunsAndShutsDown(t *testing.T) {
	orchestrator, research, pkg, _ := newResearchOrchestratorTestHarness(t)
	run := createResearchOrchestratorRun(t, research, pkg, "coordinator", ResearchModeQuick, []string{ResearchSourceKnowledge}, "grounded")
	coordinator, err := NewResearchCoordinator(ResearchCoordinatorConfig{
		Store: research, Orchestrator: orchestrator, Workers: 2, QueueSize: 4,
		PollInterval: 10 * time.Millisecond, LeaseDuration: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := coordinator.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		loaded, loadErr := research.LoadRun(run.RunID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if loaded.Status == ResearchCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	loaded, err := research.LoadRun(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != ResearchCompleted {
		t.Fatalf("coordinator run = %#v", loaded)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := coordinator.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}

func newResearchOrchestratorTestHarness(t *testing.T) (*ResearchOrchestrator, *ResearchStore, AgentPackage, *fakeResearchStageModel) {
	t.Helper()
	knowledge, pkg := agentRuntimeTestStore(t)
	research, err := OpenResearchStore(knowledge.Root(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = research.Close() })
	tools, err := NewResearchToolRegistry(knowledge, research)
	if err != nil {
		t.Fatal(err)
	}
	model := &fakeResearchStageModel{}
	config := ResearchOrchestratorConfig{
		KnowledgeStore: knowledge, ResearchStore: research, Tools: tools, Model: model,
		ModelConfig: BookTokenPlanConfig{APIKey: "synthetic", Model: "qwen-plus"}, WorkerAgentID: "chatlog-agent",
	}
	orchestrator, err := NewResearchOrchestrator(config)
	if err != nil {
		t.Fatal(err)
	}
	return orchestrator, research, pkg, model
}

func createResearchOrchestratorRun(t *testing.T, store *ResearchStore, pkg AgentPackage, key, mode string, sources []string, question string) ResearchRun {
	t.Helper()
	input := researchStoreTestInput(key)
	input.Mode = mode
	input.Request.Mode = mode
	input.Request.Question = question
	input.Request.PackageID = pkg.PackageID
	input.Request.PackageVersion = pkg.Version
	input.Request.RequestedSources = sources
	input.RouteReasons = []string{ResearchRouteExplicitQuick}
	if mode == ResearchModeDeep {
		input.RouteReasons = []string{ResearchRouteExplicitDeep}
	}
	run, _, err := store.CreateRun(input)
	if err != nil {
		t.Fatal(err)
	}
	return *run
}

func advanceResearchUntilTerminal(t *testing.T, orchestrator *ResearchOrchestrator, runID string, limit int) ResearchAdvanceResult {
	t.Helper()
	var result ResearchAdvanceResult
	for index := 0; index < limit; index++ {
		var err error
		result, err = orchestrator.Advance(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if isTerminalResearchStatus(result.Run.Status) {
			return result
		}
	}
	t.Fatalf("run did not reach terminal state: %#v", result)
	return ResearchAdvanceResult{}
}

func sortedResearchReferenceIDs(messages []BookKnowledgeMessage, kind string) []string {
	refs := researchModelReferences(messages, kind)
	result := make([]string, 0, len(refs))
	for value := range refs {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
