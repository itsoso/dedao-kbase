package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ResearchWaitWorkerPending = "worker_pending"

	ResearchOutcomeCompleted         = "completed"
	ResearchOutcomeWorkerOffline     = "worker_offline"
	ResearchOutcomeIdentityAmbiguous = "identity_ambiguous"
	ResearchOutcomeZeroHit           = "zero_hit"
	ResearchOutcomePartialEvidence   = "partial_evidence"
	ResearchOutcomeBudgetExhausted   = "budget_exhausted"
	ResearchOutcomeCitationMismatch  = "citation_mismatch"
	ResearchOutcomeSourceChanged     = "source_changed"
	ResearchOutcomeModelTimeout      = "model_timeout"
	ResearchOutcomeCanceled          = "canceled"
)

var (
	ErrResearchIdentityAmbiguous = errors.New(ResearchOutcomeIdentityAmbiguous)
	ErrResearchZeroHit           = errors.New(ResearchOutcomeZeroHit)
	ErrResearchPartialEvidence   = errors.New(ResearchOutcomePartialEvidence)
	ErrResearchBudgetExhausted   = errors.New(ResearchOutcomeBudgetExhausted)
	ErrResearchCitationMismatch  = errors.New(ResearchOutcomeCitationMismatch)
)

type ResearchOrchestratorConfig struct {
	KnowledgeStore *BookKnowledgeStore
	ResearchStore  *ResearchStore
	Tools          *ResearchToolRegistry
	Model          ResearchStageModel
	ModelConfig    BookTokenPlanConfig
	WorkerAgentID  string
}

type ResearchAdvanceResult struct {
	Run        ResearchRun `json:"run"`
	Outcome    string      `json:"outcome,omitempty"`
	WaitReason string      `json:"wait_reason,omitempty"`
}

type ResearchVerifiedConclusion struct {
	ConclusionID string   `json:"conclusion_id"`
	RunID        string   `json:"run_id"`
	Text         string   `json:"text"`
	EvidenceIDs  []string `json:"evidence_ids"`
	Confidence   float64  `json:"confidence"`
	CreatedAt    string   `json:"created_at"`
}

type researchOrchestratorState struct {
	Iteration  int
	ModelCalls int
	Outcome    string
}

type ResearchOrchestrator struct {
	config ResearchOrchestratorConfig
}

func NewResearchOrchestrator(config ResearchOrchestratorConfig) (*ResearchOrchestrator, error) {
	if config.KnowledgeStore == nil || config.ResearchStore == nil || config.Tools == nil || config.Model == nil {
		return nil, fmt.Errorf("research orchestrator stores, tools, and model are required")
	}
	if strings.TrimSpace(config.WorkerAgentID) == "" {
		config.WorkerAgentID = "chatlog-agent"
	}
	if err := migrateResearchOrchestrator(config.ResearchStore.db); err != nil {
		return nil, err
	}
	return &ResearchOrchestrator{config: config}, nil
}

func (o *ResearchOrchestrator) Advance(ctx context.Context, runID string) (ResearchAdvanceResult, error) {
	run, err := o.config.ResearchStore.LoadRun(strings.TrimSpace(runID))
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	if isTerminalResearchStatus(run.Status) {
		return o.terminalResult(*run), nil
	}
	if err := o.ensureState(run.RunID); err != nil {
		return ResearchAdvanceResult{}, err
	}
	var result ResearchAdvanceResult
	switch run.Status {
	case ResearchPlanning:
		result, err = o.advancePlanning(ctx, *run)
	case ResearchRetrieving:
		result, err = o.advanceRetrieving(ctx, *run)
	case ResearchResolvingIdentity:
		result, err = o.advanceIdentity(*run)
	case ResearchBuildingTimeline:
		result, err = o.transition(*run, ResearchExtractingFacts, "timeline_built")
	case ResearchExtractingFacts:
		result, err = o.advanceExtracting(ctx, *run)
	case ResearchDetectingConflicts:
		result, err = o.advanceConflicts(*run)
	case ResearchComparingCases:
		result, err = o.transition(*run, ResearchSynthesizing, "cases_compared")
	case ResearchSynthesizing:
		result, err = o.advanceSynthesizing(ctx, *run)
	case ResearchVerifying:
		result, err = o.advanceVerifying(ctx, *run)
	default:
		err = fmt.Errorf("unsupported research stage %q", run.Status)
	}
	if err == nil {
		return result, nil
	}
	outcome := ClassifyResearchOrchestratorOutcome(err)
	if outcome == "" || errors.Is(err, context.Canceled) {
		return ResearchAdvanceResult{}, err
	}
	status := ResearchInsufficient
	if outcome == ResearchOutcomeModelTimeout {
		status = ResearchFailed
	}
	finished, finishErr := o.finish(*run, status, outcome)
	if finishErr != nil {
		return ResearchAdvanceResult{}, fmt.Errorf("%v; persist typed research outcome: %w", err, finishErr)
	}
	return finished, nil
}

func (o *ResearchOrchestrator) advancePlanning(ctx context.Context, run ResearchRun) (ResearchAdvanceResult, error) {
	if run.Mode == ResearchModeQuick {
		return o.transition(run, ResearchRetrieving, "quick_retrieval_planned")
	}
	state, err := o.loadState(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	messages, err := o.stageMessages(run, "Plan bounded retrieval. Use only supported tools.")
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	var output ResearchPlannerOutput
	if _, err := o.invokeModel(ctx, run, ResearchRolePlanner, fmt.Sprintf("planner:%d", state.Iteration), messages, &output); err != nil {
		return ResearchAdvanceResult{}, err
	}
	for _, call := range output.ToolCalls {
		if isResearchWorkerTool(call.Tool) {
			arguments, err := json.Marshal(call.Arguments)
			if err != nil {
				return ResearchAdvanceResult{}, err
			}
			if _, _, err := o.config.ResearchStore.CreateWorkerJob(ResearchWorkerJobInput{
				RunID: run.RunID, TargetAgentID: o.config.WorkerAgentID, Tool: call.Tool,
				Arguments: arguments, MaxAttempts: 3,
			}); err != nil {
				return ResearchAdvanceResult{}, err
			}
			continue
		}
		toolResult, err := o.config.Tools.Execute(ctx, call.Tool, ResearchToolRequest{
			RunID: run.RunID, PackageID: run.PackageID, PackageVersion: run.PackageVersion, Arguments: call.Arguments,
		})
		if err != nil {
			return ResearchAdvanceResult{}, err
		}
		if err := o.storePromotedEvidence(run.RunID, toolResult.PromotedEvidence); err != nil {
			return ResearchAdvanceResult{}, err
		}
	}
	loaded, err := o.config.ResearchStore.LoadRun(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	return o.transition(*loaded, ResearchRetrieving, "deep_retrieval_planned")
}

func (o *ResearchOrchestrator) advanceRetrieving(ctx context.Context, run ResearchRun) (ResearchAdvanceResult, error) {
	evidence, err := o.config.ResearchStore.ListEvidence(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	if run.Mode == ResearchModeQuick {
		if len(evidence) == 0 {
			result, err := o.config.Tools.Execute(ctx, ResearchToolSearchKnowledge, ResearchToolRequest{
				RunID: run.RunID, PackageID: run.PackageID, PackageVersion: run.PackageVersion,
				Arguments: map[string]any{"query": run.Question},
			})
			if err != nil {
				return ResearchAdvanceResult{}, err
			}
			if len(result.Knowledge) == 0 {
				return ResearchAdvanceResult{}, ErrResearchZeroHit
			}
			promoted := []ResearchEvidence{}
			for _, item := range result.Knowledge {
				if len(item.CitationIDs) == 0 {
					continue
				}
				fetched, err := o.config.Tools.Execute(ctx, ResearchToolFetchKnowledgeEvidence, ResearchToolRequest{
					RunID: run.RunID, PackageID: run.PackageID, PackageVersion: run.PackageVersion,
					Arguments: map[string]any{"release_id": item.ReleaseID, "claim_id": item.ClaimID, "citation_id": item.CitationIDs[0]},
				})
				if err != nil {
					return ResearchAdvanceResult{}, err
				}
				promoted = append(promoted, fetched.PromotedEvidence...)
				if len(promoted) >= run.Budget.MaxEvidenceItems {
					break
				}
			}
			if len(promoted) == 0 {
				return ResearchAdvanceResult{}, ErrResearchZeroHit
			}
			if err := o.storePromotedEvidence(run.RunID, promoted); err != nil {
				return ResearchAdvanceResult{}, err
			}
		}
		loaded, err := o.config.ResearchStore.LoadRun(run.RunID)
		if err != nil {
			return ResearchAdvanceResult{}, err
		}
		return o.transition(*loaded, ResearchSynthesizing, "quick_evidence_retrieved")
	}

	jobs, err := o.listWorkerJobs(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	if len(jobs) > 0 {
		for _, job := range jobs {
			switch job.State {
			case ResearchWorkerJobQueued, ResearchWorkerJobLeased:
				return o.wait(run, ResearchWaitWorkerPending)
			case ResearchWorkerJobFailed, ResearchWorkerJobExpired:
				return ResearchAdvanceResult{}, ErrResearchWorkerTerminal
			}
		}
	}
	evidence, err = o.config.ResearchStore.ListEvidence(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	if len(evidence) == 0 {
		return ResearchAdvanceResult{}, ErrResearchZeroHit
	}
	next := ResearchExtractingFacts
	if containsResearchString(run.RouteReasons, ResearchRouteIdentity) {
		next = ResearchResolvingIdentity
	}
	return o.transition(run, next, "deep_evidence_retrieved")
}

func (o *ResearchOrchestrator) advanceIdentity(run ResearchRun) (ResearchAdvanceResult, error) {
	if len(run.SubjectIDs) == 0 {
		return ResearchAdvanceResult{}, ErrResearchIdentityAmbiguous
	}
	next := ResearchExtractingFacts
	if containsResearchString(run.RouteReasons, ResearchRouteTimeline) {
		next = ResearchBuildingTimeline
	}
	return o.transition(run, next, "identity_resolved")
}

func (o *ResearchOrchestrator) advanceExtracting(ctx context.Context, run ResearchRun) (ResearchAdvanceResult, error) {
	state, err := o.loadState(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	messages, err := o.stageMessages(run, "Extract only grounded facts and claims.")
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	var output ResearchExtractorOutput
	if _, err := o.invokeModel(ctx, run, ResearchRoleExtractor, fmt.Sprintf("extractor:%d", state.Iteration), messages, &output); err != nil {
		return ResearchAdvanceResult{}, err
	}
	records := make([]ResearchAnalysisRecord, 0, len(output.Facts)+len(output.Claims))
	for _, fact := range output.Facts {
		records = append(records, ResearchAnalysisRecord{
			RecordID: fact.FactID, Kind: ResearchAnalysisFact, Summary: fact.Summary,
			Attributes:         map[string]any{"fact_kind": fact.Kind, "status": fact.Status, "occurred_at": fact.OccurredAt},
			SupportEvidenceIDs: fact.EvidenceIDs, Confidence: fact.Confidence, ReviewState: defaultResearchReviewState(fact.ReviewState),
		})
	}
	for _, claim := range output.Claims {
		records = append(records, ResearchAnalysisRecord{
			RecordID: claim.ClaimID, Kind: ResearchAnalysisFact, Summary: claim.Value,
			Attributes: map[string]any{"claim_kind": claim.Kind, "topic": claim.Topic, "value": claim.Value,
				"timing": claim.Timing, "amount": claim.Amount, "applies_to": claim.AppliesTo},
			SupportEvidenceIDs: claim.EvidenceIDs, Confidence: defaultResearchConfidence(claim.Confidence),
			ReviewState: defaultResearchReviewState(claim.ReviewState),
		})
	}
	if len(records) > 0 {
		if err := o.config.ResearchStore.StoreResearchAnalysisRecords(run.RunID, records); err != nil {
			return ResearchAdvanceResult{}, err
		}
	}
	next := ResearchSynthesizing
	if containsResearchString(run.RouteReasons, ResearchRouteConflict) {
		next = ResearchDetectingConflicts
	} else if containsResearchString(run.RouteReasons, ResearchRouteCaseComparison) {
		next = ResearchComparingCases
	}
	return o.transition(run, next, "facts_extracted")
}

func (o *ResearchOrchestrator) advanceConflicts(run ResearchRun) (ResearchAdvanceResult, error) {
	records, err := o.config.ResearchStore.ListResearchAnalysisRecords(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	claims := []ResearchClaim{}
	for _, record := range records {
		kind, _ := record.Attributes["claim_kind"].(string)
		if kind == "" {
			continue
		}
		claims = append(claims, ResearchClaim{
			ClaimID: record.RecordID, Kind: kind, Topic: researchStringAttribute(record.Attributes, "topic"),
			Value: researchStringAttribute(record.Attributes, "value"), Timing: researchStringAttribute(record.Attributes, "timing"),
			Amount: researchStringAttribute(record.Attributes, "amount"), AppliesTo: researchStringAttribute(record.Attributes, "applies_to"),
			EvidenceIDs: record.SupportEvidenceIDs, Confidence: record.Confidence, ReviewState: record.ReviewState,
		})
	}
	conflicts := DetectResearchConflicts(claims)
	analysis := make([]ResearchAnalysisRecord, 0, len(conflicts))
	for _, conflict := range conflicts {
		analysis = append(analysis, ResearchAnalysisRecord{
			RecordID: conflict.ConflictID, Kind: ResearchAnalysisConflict,
			Summary:            "Conflicting recommendation dimensions: " + strings.Join(conflict.Dimensions, ", "),
			Attributes:         map[string]any{"claim_ids": conflict.ClaimIDs, "dimensions": conflict.Dimensions},
			SupportEvidenceIDs: conflict.EvidenceIDs, Confidence: defaultResearchConfidence(conflict.Confidence), ReviewState: ResearchReviewPending,
		})
	}
	if len(analysis) > 0 {
		if err := o.config.ResearchStore.StoreResearchAnalysisRecords(run.RunID, analysis); err != nil {
			return ResearchAdvanceResult{}, err
		}
	}
	next := ResearchSynthesizing
	if containsResearchString(run.RouteReasons, ResearchRouteCaseComparison) {
		next = ResearchComparingCases
	}
	return o.transition(run, next, "conflicts_detected")
}

func (o *ResearchOrchestrator) advanceSynthesizing(ctx context.Context, run ResearchRun) (ResearchAdvanceResult, error) {
	state, err := o.loadState(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	messages, err := o.stageMessages(run, "Synthesize conclusions. Every conclusion must cite accessible support.")
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	var output ResearchSynthesizerOutput
	if _, err := o.invokeModel(ctx, run, ResearchRoleSynthesizer, fmt.Sprintf("synthesizer:%d", state.Iteration), messages, &output); err != nil {
		return ResearchAdvanceResult{}, err
	}
	if len(output.Conclusions) == 0 {
		return ResearchAdvanceResult{}, ErrResearchPartialEvidence
	}
	if err := o.saveDrafts(run.RunID, output.Conclusions); err != nil {
		return ResearchAdvanceResult{}, err
	}
	return o.transition(run, ResearchVerifying, "conclusions_synthesized")
}

func (o *ResearchOrchestrator) advanceVerifying(ctx context.Context, run ResearchRun) (ResearchAdvanceResult, error) {
	state, err := o.loadState(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	drafts, err := o.loadDrafts(run.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	if err := o.validateDraftSupports(run, drafts); err != nil {
		return ResearchAdvanceResult{}, err
	}
	messages, err := o.stageMessages(run, "Verify every conclusion and remove unsupported claims.")
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	for _, draft := range drafts {
		messages = append(messages, BookKnowledgeMessage{Role: "user", Content: "[conclusion:" + draft.ConclusionID + "] " + draft.Text})
	}
	var output ResearchVerifierOutput
	if _, err := o.invokeModel(ctx, run, ResearchRoleVerifier, fmt.Sprintf("verifier:%d", state.Iteration), messages, &output); err != nil {
		return ResearchAdvanceResult{}, err
	}
	caseWarningPresent := !containsResearchString(run.RouteReasons, ResearchRouteCaseComparison) ||
		containsResearchString(output.Warnings, "case_transfer_limited")
	if output.Verdict == ResearchVerifierVerified && caseWarningPresent &&
		sameResearchStringSet(output.VerifiedConclusionIDs, researchDraftIDs(drafts)) {
		if err := o.promoteVerifiedConclusions(run.RunID, drafts); err != nil {
			return ResearchAdvanceResult{}, err
		}
		return o.finish(run, ResearchCompleted, ResearchOutcomeCompleted)
	}
	if output.Verdict == ResearchVerifierInsufficient {
		return o.finish(run, ResearchInsufficient, ResearchOutcomePartialEvidence)
	}
	state.Iteration++
	if err := o.updateIteration(run.RunID, state.Iteration); err != nil {
		return ResearchAdvanceResult{}, err
	}
	if state.Iteration >= run.Budget.MaxIterations {
		return o.finish(run, ResearchInsufficient, ResearchOutcomePartialEvidence)
	}
	return o.transition(run, ResearchPlanning, "verifier_requested_replan")
}

func (o *ResearchOrchestrator) invokeModel(
	ctx context.Context,
	run ResearchRun,
	role ResearchModelRole,
	requestKey string,
	messages []BookKnowledgeMessage,
	output any,
) (ResearchModelUsage, error) {
	requestIdentity := researchToolFingerprint(map[string]any{
		"run_id": run.RunID, "role": role, "request_key": requestKey, "messages": messages,
	})
	var responseJSON, usageJSON string
	err := o.config.ResearchStore.db.QueryRow(`SELECT response_json, usage_json FROM research_orchestrator_models
		WHERE request_identity = ?`, requestIdentity).Scan(&responseJSON, &usageJSON)
	if err == nil {
		if err := json.Unmarshal([]byte(responseJSON), output); err != nil {
			return ResearchModelUsage{}, err
		}
		var usage ResearchModelUsage
		if err := json.Unmarshal([]byte(usageJSON), &usage); err != nil {
			return ResearchModelUsage{}, err
		}
		return usage, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ResearchModelUsage{}, err
	}
	state, err := o.loadState(run.RunID)
	if err != nil {
		return ResearchModelUsage{}, err
	}
	if state.ModelCalls >= run.Budget.MaxModelCalls {
		return ResearchModelUsage{}, ErrResearchBudgetExhausted
	}
	now := o.config.ResearchStore.now().UTC().Format(time.RFC3339Nano)
	invocationID := "research-model-" + researchAnalysisID(run.RunID, requestIdentity)
	startTx, err := o.config.ResearchStore.db.Begin()
	if err != nil {
		return ResearchModelUsage{}, err
	}
	if _, err := startTx.Exec(`INSERT INTO research_model_invocations
		(invocation_id, run_id, request_identity, model, purpose, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'running', ?, ?)
		ON CONFLICT(request_identity) DO UPDATE SET status = 'running', updated_at = excluded.updated_at`,
		invocationID, run.RunID, requestIdentity, normalizeBookTokenPlanModel(o.config.ModelConfig.Model), role, now, now); err != nil {
		_ = startTx.Rollback()
		return ResearchModelUsage{}, err
	}
	if _, err := startTx.Exec(`UPDATE research_orchestrator_state SET model_calls = model_calls + 1, updated_at = ? WHERE run_id = ?`, now, run.RunID); err != nil {
		_ = startTx.Rollback()
		return ResearchModelUsage{}, err
	}
	if err := startTx.Commit(); err != nil {
		return ResearchModelUsage{}, err
	}
	usage, err := o.config.Model.Run(ctx, role, o.config.ModelConfig, messages, output)
	if err != nil {
		_, _ = o.config.ResearchStore.db.Exec(`UPDATE research_model_invocations SET status = 'failed', updated_at = ?
			WHERE request_identity = ?`, o.config.ResearchStore.now().UTC().Format(time.RFC3339Nano), requestIdentity)
		return ResearchModelUsage{}, err
	}
	response, err := json.Marshal(output)
	if err != nil {
		return ResearchModelUsage{}, err
	}
	encodedUsage, err := json.Marshal(usage)
	if err != nil {
		return ResearchModelUsage{}, err
	}
	now = o.config.ResearchStore.now().UTC().Format(time.RFC3339Nano)
	tx, err := o.config.ResearchStore.db.Begin()
	if err != nil {
		return ResearchModelUsage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`INSERT OR IGNORE INTO research_orchestrator_models
		(request_identity, run_id, role, response_json, usage_json, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		requestIdentity, run.RunID, role, string(response), string(encodedUsage), now)
	if err != nil {
		return ResearchModelUsage{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return ResearchModelUsage{}, err
	}
	if inserted == 1 {
		if _, err := tx.Exec(`UPDATE research_model_invocations SET status = 'completed', input_tokens = ?,
			output_tokens = ?, estimated_cost_usd = ?, updated_at = ? WHERE request_identity = ?`,
			usage.InputTokens, usage.OutputTokens, usage.CostUSD, now, requestIdentity); err != nil {
			return ResearchModelUsage{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ResearchModelUsage{}, err
	}
	return usage, nil
}

func (o *ResearchOrchestrator) stageMessages(run ResearchRun, instruction string) ([]BookKnowledgeMessage, error) {
	evidence, err := o.config.ResearchStore.ListEvidence(run.RunID)
	if err != nil {
		return nil, err
	}
	messages := []BookKnowledgeMessage{
		{Role: "system", Content: "Return strict role-specific JSON. Provide only decision_summary, never hidden reasoning."},
		{Role: "user", Content: instruction + "\nQuestion: " + run.Question},
	}
	for _, item := range evidence {
		content := "[evidence:" + item.EvidenceID + "] " + item.ContentExcerpt
		if item.SourceType == ResearchEvidenceSourceKnowledge && strings.TrimSpace(item.Locator.ConversationRef) != "" {
			content += " [citation:" + item.Locator.ConversationRef + "]"
		}
		messages = append(messages, BookKnowledgeMessage{Role: "user", Content: content})
	}
	return messages, nil
}

func (o *ResearchOrchestrator) storePromotedEvidence(runID string, evidence []ResearchEvidence) error {
	if len(evidence) == 0 {
		return nil
	}
	run, err := o.config.ResearchStore.LoadRun(runID)
	if err != nil {
		return err
	}
	searched := []string{}
	cited := []string{}
	for _, item := range evidence {
		source, err := researchScopeSourceForEvidence(item.SourceType)
		if err != nil {
			return err
		}
		searched = append(searched, source)
		cited = append(cited, source)
	}
	_, err = o.config.ResearchStore.StoreEvidenceBundle(runID, run.Version, ResearchEvidenceBundle{
		Evidence: evidence, SearchedSources: uniqueSortedResearchStrings(searched), CitedSources: uniqueSortedResearchStrings(cited),
	})
	return err
}

func (o *ResearchOrchestrator) transition(run ResearchRun, to ResearchRunStatus, code string) (ResearchAdvanceResult, error) {
	updated, err := o.config.ResearchStore.TransitionRun(run.RunID, run.Version, to,
		ResearchTransition{Code: code, Actor: "orchestrator"})
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	if _, err := o.config.ResearchStore.db.Exec(`UPDATE research_runs SET wait_reason = '' WHERE run_id = ?`, run.RunID); err != nil {
		return ResearchAdvanceResult{}, err
	}
	return ResearchAdvanceResult{Run: *updated}, nil
}

func (o *ResearchOrchestrator) wait(run ResearchRun, reason string) (ResearchAdvanceResult, error) {
	if _, err := o.config.ResearchStore.db.Exec(`UPDATE research_runs SET wait_reason = ? WHERE run_id = ?`, reason, run.RunID); err != nil {
		return ResearchAdvanceResult{}, err
	}
	run.WaitReason = reason
	return ResearchAdvanceResult{Run: run, WaitReason: reason}, nil
}

func (o *ResearchOrchestrator) finish(run ResearchRun, status ResearchRunStatus, outcome string) (ResearchAdvanceResult, error) {
	updated, err := o.config.ResearchStore.TransitionRun(run.RunID, run.Version, status,
		ResearchTransition{Code: outcome, Actor: "orchestrator"})
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	failureJSON := ""
	if status != ResearchCompleted {
		failure, err := json.Marshal(ResearchFailure{Code: outcome, Message: outcome, Retryable: false})
		if err != nil {
			return ResearchAdvanceResult{}, err
		}
		failureJSON = string(failure)
	}
	now := o.config.ResearchStore.now().UTC().Format(time.RFC3339Nano)
	if _, err := o.config.ResearchStore.db.Exec(`UPDATE research_runs SET wait_reason = '', failure_json = ? WHERE run_id = ?`, failureJSON, run.RunID); err != nil {
		return ResearchAdvanceResult{}, err
	}
	if _, err := o.config.ResearchStore.db.Exec(`UPDATE research_orchestrator_state SET last_outcome = ?, updated_at = ? WHERE run_id = ?`, outcome, now, run.RunID); err != nil {
		return ResearchAdvanceResult{}, err
	}
	loaded, err := o.config.ResearchStore.LoadRun(updated.RunID)
	if err != nil {
		return ResearchAdvanceResult{}, err
	}
	return ResearchAdvanceResult{Run: *loaded, Outcome: outcome}, nil
}

func (o *ResearchOrchestrator) terminalResult(run ResearchRun) ResearchAdvanceResult {
	outcome := ""
	if run.Failure != nil {
		outcome = run.Failure.Code
	}
	if run.Status == ResearchCompleted {
		outcome = ResearchOutcomeCompleted
	} else if run.Status == ResearchCanceled {
		outcome = ResearchOutcomeCanceled
	}
	return ResearchAdvanceResult{Run: run, Outcome: outcome, WaitReason: run.WaitReason}
}

func (o *ResearchOrchestrator) ensureState(runID string) error {
	now := o.config.ResearchStore.now().UTC().Format(time.RFC3339Nano)
	_, err := o.config.ResearchStore.db.Exec(`INSERT OR IGNORE INTO research_orchestrator_state
		(run_id, iteration, model_calls, last_outcome, updated_at) VALUES (?, 0, 0, '', ?)`, runID, now)
	return err
}

func (o *ResearchOrchestrator) loadState(runID string) (researchOrchestratorState, error) {
	if err := o.ensureState(runID); err != nil {
		return researchOrchestratorState{}, err
	}
	var state researchOrchestratorState
	err := o.config.ResearchStore.db.QueryRow(`SELECT iteration, model_calls, last_outcome
		FROM research_orchestrator_state WHERE run_id = ?`, runID).Scan(&state.Iteration, &state.ModelCalls, &state.Outcome)
	return state, err
}

func (o *ResearchOrchestrator) updateIteration(runID string, iteration int) error {
	now := o.config.ResearchStore.now().UTC().Format(time.RFC3339Nano)
	_, err := o.config.ResearchStore.db.Exec(`UPDATE research_orchestrator_state SET iteration = ?, updated_at = ? WHERE run_id = ?`, iteration, now, runID)
	return err
}

func (o *ResearchOrchestrator) listWorkerJobs(runID string) ([]ResearchWorkerJob, error) {
	rows, err := o.config.ResearchStore.db.Query(`SELECT job_id FROM research_worker_jobs WHERE run_id = ? ORDER BY created_at, job_id`, runID)
	if err != nil {
		return nil, err
	}
	jobIDs := []string{}
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		jobIDs = append(jobIDs, jobID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := make([]ResearchWorkerJob, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		job, err := o.config.ResearchStore.LoadWorkerJob(jobID)
		if err != nil {
			return nil, err
		}
		result = append(result, *job)
	}
	return result, nil
}

func (o *ResearchOrchestrator) saveDrafts(runID string, drafts []ResearchConclusionDraft) error {
	tx, err := o.config.ResearchStore.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := o.config.ResearchStore.now().UTC().Format(time.RFC3339Nano)
	for _, draft := range drafts {
		evidenceJSON, err := json.Marshal(uniqueSortedResearchStrings(draft.SupportEvidenceIDs))
		if err != nil {
			return err
		}
		citationJSON, err := json.Marshal(uniqueSortedResearchStrings(draft.CitationIDs))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO research_conclusion_drafts
			(run_id, conclusion_id, conclusion_text, evidence_ids_json, citation_ids_json, confidence, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(run_id, conclusion_id) DO UPDATE SET conclusion_text = excluded.conclusion_text,
				evidence_ids_json = excluded.evidence_ids_json, citation_ids_json = excluded.citation_ids_json,
				confidence = excluded.confidence`, runID, draft.ConclusionID, draft.Text,
			string(evidenceJSON), string(citationJSON), draft.Confidence, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (o *ResearchOrchestrator) loadDrafts(runID string) ([]ResearchConclusionDraft, error) {
	rows, err := o.config.ResearchStore.db.Query(`SELECT conclusion_id, conclusion_text, evidence_ids_json,
		citation_ids_json, confidence FROM research_conclusion_drafts WHERE run_id = ? ORDER BY conclusion_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ResearchConclusionDraft{}
	for rows.Next() {
		var draft ResearchConclusionDraft
		var evidenceJSON, citationJSON string
		if err := rows.Scan(&draft.ConclusionID, &draft.Text, &evidenceJSON, &citationJSON, &draft.Confidence); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &draft.SupportEvidenceIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(citationJSON), &draft.CitationIDs); err != nil {
			return nil, err
		}
		result = append(result, draft)
	}
	return result, rows.Err()
}

func (o *ResearchOrchestrator) validateDraftSupports(run ResearchRun, drafts []ResearchConclusionDraft) error {
	evidence, err := o.config.ResearchStore.ListEvidence(run.RunID)
	if err != nil {
		return err
	}
	availableEvidence := map[string]bool{}
	availableCitations := map[string]bool{}
	for _, item := range evidence {
		availableEvidence[item.EvidenceID] = true
		if item.SourceType == ResearchEvidenceSourceKnowledge && strings.TrimSpace(item.Locator.ConversationRef) != "" {
			availableCitations[item.Locator.ConversationRef] = true
		}
	}
	for _, draft := range drafts {
		if len(draft.SupportEvidenceIDs) == 0 {
			return ErrResearchPartialEvidence
		}
		for _, evidenceID := range draft.SupportEvidenceIDs {
			if !availableEvidence[evidenceID] {
				return ErrResearchPartialEvidence
			}
		}
		for _, citationID := range draft.CitationIDs {
			if !availableCitations[citationID] {
				return ErrResearchCitationMismatch
			}
		}
	}
	return nil
}

func (o *ResearchOrchestrator) promoteVerifiedConclusions(runID string, drafts []ResearchConclusionDraft) error {
	tx, err := o.config.ResearchStore.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := o.config.ResearchStore.now().UTC().Format(time.RFC3339Nano)
	for _, draft := range drafts {
		storedConclusionID := "research-conclusion-" + researchAnalysisID(runID, draft.ConclusionID)
		evidenceJSON, err := json.Marshal(uniqueSortedResearchStrings(draft.SupportEvidenceIDs))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO research_conclusions
			(conclusion_id, run_id, conclusion_text, evidence_ids_json, confidence, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(conclusion_id) DO UPDATE SET conclusion_text = excluded.conclusion_text,
				evidence_ids_json = excluded.evidence_ids_json, confidence = excluded.confidence`,
			storedConclusionID, runID, draft.Text, string(evidenceJSON), draft.Confidence, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *ResearchStore) ListVerifiedResearchConclusions(runID string) ([]ResearchVerifiedConclusion, error) {
	rows, err := s.db.Query(`SELECT conclusion_id, run_id, conclusion_text, evidence_ids_json, confidence, created_at
		FROM research_conclusions WHERE run_id = ? ORDER BY conclusion_id`, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ResearchVerifiedConclusion{}
	for rows.Next() {
		var item ResearchVerifiedConclusion
		var evidenceJSON string
		if err := rows.Scan(&item.ConclusionID, &item.RunID, &item.Text, &evidenceJSON, &item.Confidence, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &item.EvidenceIDs); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func migrateResearchOrchestrator(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS research_orchestrator_state (
			run_id TEXT PRIMARY KEY REFERENCES research_runs(run_id) ON DELETE CASCADE,
			iteration INTEGER NOT NULL DEFAULT 0, model_calls INTEGER NOT NULL DEFAULT 0,
			last_outcome TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS research_orchestrator_models (
			request_identity TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			role TEXT NOT NULL, response_json TEXT NOT NULL, usage_json TEXT NOT NULL, created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS research_conclusion_drafts (
			run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
			conclusion_id TEXT NOT NULL, conclusion_text TEXT NOT NULL, evidence_ids_json TEXT NOT NULL DEFAULT '[]',
			citation_ids_json TEXT NOT NULL DEFAULT '[]', confidence REAL NOT NULL, created_at TEXT NOT NULL,
			PRIMARY KEY(run_id, conclusion_id)
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func ClassifyResearchOrchestratorOutcome(err error) string {
	switch {
	case errors.Is(err, ErrResearchWorkerTerminal):
		return ResearchOutcomeWorkerOffline
	case errors.Is(err, ErrResearchIdentityAmbiguous):
		return ResearchOutcomeIdentityAmbiguous
	case errors.Is(err, ErrResearchZeroHit):
		return ResearchOutcomeZeroHit
	case errors.Is(err, ErrResearchPartialEvidence):
		return ResearchOutcomePartialEvidence
	case errors.Is(err, ErrResearchBudgetExhausted):
		return ResearchOutcomeBudgetExhausted
	case errors.Is(err, ErrResearchCitationMismatch):
		return ResearchOutcomeCitationMismatch
	case errors.Is(err, ErrResearchEvidenceSourceChanged):
		return ResearchOutcomeSourceChanged
	case errors.Is(err, context.DeadlineExceeded):
		return ResearchOutcomeModelTimeout
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "content hash changed") {
		return ResearchOutcomeSourceChanged
	}
	if strings.Contains(message, "unreferenced citation") || strings.Contains(message, "citation") && strings.Contains(message, "outside") {
		return ResearchOutcomeCitationMismatch
	}
	return ""
}

func isResearchWorkerTool(name string) bool {
	return stringBoolSet(
		ResearchWorkerToolSearchChatlog, ResearchWorkerToolExpandChatContext,
		ResearchWorkerToolResolveChatIdentity, ResearchWorkerToolListIdentityConversations,
		ResearchWorkerToolFetchChatMessage,
	)[strings.TrimSpace(name)]
}

func containsResearchString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func defaultResearchReviewState(value string) string {
	if value == ResearchReviewVerified || value == ResearchReviewRejected || value == ResearchReviewPending {
		return value
	}
	return ResearchReviewPending
}

func defaultResearchConfidence(value float64) float64 {
	if value <= 0 || value > 1 {
		return 0.5
	}
	return value
}

func researchStringAttribute(attributes map[string]any, key string) string {
	value, _ := attributes[key].(string)
	return value
}

func sameResearchStringSet(left, right []string) bool {
	left = uniqueSortedResearchStrings(left)
	right = uniqueSortedResearchStrings(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func researchDraftIDs(drafts []ResearchConclusionDraft) []string {
	result := make([]string, 0, len(drafts))
	for _, draft := range drafts {
		result = append(result, draft.ConclusionID)
	}
	sort.Strings(result)
	return result
}

type ResearchCoordinatorConfig struct {
	Store         *ResearchStore
	Orchestrator  *ResearchOrchestrator
	Workers       int
	QueueSize     int
	PollInterval  time.Duration
	LeaseDuration time.Duration
	OwnerID       string
}

type ResearchCoordinator struct {
	config  ResearchCoordinatorConfig
	queue   chan string
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	started bool
	stopped bool
	pending map[string]bool
	wg      sync.WaitGroup
}

func NewResearchCoordinator(config ResearchCoordinatorConfig) (*ResearchCoordinator, error) {
	if config.Store == nil || config.Orchestrator == nil {
		return nil, fmt.Errorf("research coordinator store and orchestrator are required")
	}
	if config.Workers <= 0 {
		config.Workers = 2
	}
	if config.Workers > 32 {
		return nil, fmt.Errorf("research coordinator workers must be between 1 and 32")
	}
	if config.QueueSize <= 0 {
		config.QueueSize = config.Workers * 4
	}
	if config.QueueSize > 4096 {
		return nil, fmt.Errorf("research coordinator queue size must be between 1 and 4096")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 30 * time.Second
	}
	if strings.TrimSpace(config.OwnerID) == "" {
		config.OwnerID = strings.Replace(newResearchRunID(), "research-run-", "research-coordinator-", 1)
	}
	return &ResearchCoordinator{
		config: config, queue: make(chan string, config.QueueSize), pending: map[string]bool{},
	}, nil
}

func (c *ResearchCoordinator) Start(parent context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return nil
	}
	if c.stopped {
		return fmt.Errorf("research coordinator is stopped")
	}
	if parent == nil {
		parent = context.Background()
	}
	if _, err := c.config.Store.RecoverExpiredWorkerJobs(); err != nil {
		return err
	}
	c.ctx, c.cancel = context.WithCancel(parent)
	c.started = true
	for index := 0; index < c.config.Workers; index++ {
		c.wg.Add(1)
		go c.worker()
	}
	c.wg.Add(1)
	go c.poll()
	return nil
}

func (c *ResearchCoordinator) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return nil
	}
	c.stopped = true
	cancel := c.cancel
	started := c.started
	c.mu.Unlock()
	if !started {
		return nil
	}
	cancel()
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *ResearchCoordinator) poll() {
	defer c.wg.Done()
	c.scan()
	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.scan()
		}
	}
}

func (c *ResearchCoordinator) scan() {
	for index := 0; index < c.config.QueueSize; index++ {
		run, err := c.config.Store.ClaimRunnableRun(c.config.OwnerID, c.config.LeaseDuration)
		if err != nil || run == nil {
			return
		}
		c.mu.Lock()
		if c.pending[run.RunID] {
			c.mu.Unlock()
			_ = c.config.Store.ReleaseRunLease(run.RunID, c.config.OwnerID)
			return
		}
		c.pending[run.RunID] = true
		c.mu.Unlock()
		select {
		case c.queue <- run.RunID:
		case <-c.ctx.Done():
			c.removePending(run.RunID)
			_ = c.config.Store.ReleaseRunLease(run.RunID, c.config.OwnerID)
			return
		default:
			c.removePending(run.RunID)
			_ = c.config.Store.ReleaseRunLease(run.RunID, c.config.OwnerID)
			return
		}
	}
}

func (c *ResearchCoordinator) worker() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case runID := <-c.queue:
			_, _ = c.config.Orchestrator.Advance(c.ctx, runID)
			_ = c.config.Store.ReleaseRunLease(runID, c.config.OwnerID)
			c.removePending(runID)
		}
	}
}

func (c *ResearchCoordinator) removePending(runID string) {
	c.mu.Lock()
	delete(c.pending, runID)
	c.mu.Unlock()
}

func (s *ResearchStore) ReleaseRunLease(runID, owner string) error {
	result, err := s.db.Exec(`UPDATE research_runs SET lease_owner = '', lease_expires_at = ''
		WHERE run_id = ? AND lease_owner = ?`, strings.TrimSpace(runID), strings.TrimSpace(owner))
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrResearchRunLeaseOwner
	}
	return nil
}
