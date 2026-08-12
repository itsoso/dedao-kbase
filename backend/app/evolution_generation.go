package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const evolutionEvaluationMaxAttempts = 3

type EvolutionGenerationConfig struct {
	ControlStore     *EvolutionControlStore
	KnowledgeStore   *BookKnowledgeStore
	GeneratorVersion string
	CompileAgent     func(*BookKnowledgeStore, AgentCompilationRequest) (*AgentCompilation, error)
}

type EvolutionGenerationService struct {
	control          *EvolutionControlStore
	knowledge        *BookKnowledgeStore
	generatorVersion string
	compileAgent     func(*BookKnowledgeStore, AgentCompilationRequest) (*AgentCompilation, error)
}

type EvolutionGenerationResult struct {
	Candidate      *EvolutionCandidate `json:"candidate"`
	EvaluationWork *EvolutionWork      `json:"evaluation_work"`
}

// EvolutionGenerationFailure exposes only a bounded, stable code and message to
// workers. Err is retained for server-side diagnosis and must not be serialized.
type EvolutionGenerationFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (failure *EvolutionGenerationFailure) Error() string {
	if failure == nil {
		return ""
	}
	return failure.Message
}

func (failure *EvolutionGenerationFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

func NewEvolutionGenerationService(config EvolutionGenerationConfig) (*EvolutionGenerationService, error) {
	if config.ControlStore == nil {
		return nil, fmt.Errorf("evolution control store is required")
	}
	if config.KnowledgeStore == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	config.GeneratorVersion = strings.TrimSpace(config.GeneratorVersion)
	if err := validateEvolutionIdentity("generator_version", config.GeneratorVersion); err != nil {
		return nil, err
	}
	if config.CompileAgent == nil {
		config.CompileAgent = CompileAgentPackages
	}
	return &EvolutionGenerationService{
		control: config.ControlStore, knowledge: config.KnowledgeStore,
		generatorVersion: config.GeneratorVersion, compileAgent: config.CompileAgent,
	}, nil
}

func (service *EvolutionGenerationService) Generate(ctx context.Context, work EvolutionWork) (*EvolutionGenerationResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("generation context is required")
	}
	if work.Status != EvolutionWorkLeased || work.Attempt < 1 {
		return nil, fmt.Errorf("generation work must hold an active lease")
	}
	if work.Capability != EvolutionCapabilityAgent && work.Capability != EvolutionCapabilityKnowledge {
		return nil, fmt.Errorf("generation work capability is invalid")
	}
	run, err := service.control.LoadRunContext(ctx, work.RunID)
	if err != nil {
		return nil, generationFailure("run_unavailable", "candidate generation could not load its run", err)
	}
	if run.Status == EvolutionEvaluating && run.CurrentCandidateID != "" {
		candidate, _, loadErr := service.control.LoadEvolutionCandidate(run.CurrentCandidateID)
		if loadErr != nil {
			return nil, generationFailure("candidate_unavailable", "candidate generation could not restore its result", loadErr)
		}
		evaluationWork, enqueueErr := service.enqueueEvaluation(*candidate)
		if enqueueErr != nil {
			return nil, generationFailure("evaluation_enqueue_failed", "candidate generation could not schedule evaluation", enqueueErr)
		}
		return &EvolutionGenerationResult{Candidate: candidate, EvaluationWork: evaluationWork}, nil
	}
	if run.Status != EvolutionGenerating {
		return nil, generationFailure("run_state_conflict", "candidate generation is not allowed in the current run state", ErrEvolutionTransitionConflict)
	}

	var input EvolutionCandidateInput
	switch work.Capability {
	case EvolutionCapabilityAgent:
		input, err = service.buildAgentCandidate(*run, work)
	case EvolutionCapabilityKnowledge:
		input, err = service.buildKnowledgeCandidate(*run, work)
	}
	if err != nil {
		return nil, err
	}
	candidate, _, err := service.control.SaveEvolutionCandidate(input)
	if err != nil {
		return nil, generationFailure("candidate_store_failed", "candidate generation could not store its immutable result", err)
	}
	if _, err := service.control.TransitionRun(run.RunID, EvolutionEvaluating, EvolutionTransitionInput{
		Actor: service.generatorVersion, Code: "candidate_generated",
		Message: "immutable candidate is ready for deterministic evaluation", ArtifactRefs: []string{candidate.ArtifactRef},
	}); err != nil {
		return nil, generationFailure("evaluation_transition_failed", "candidate generation could not enter evaluation", err)
	}
	evaluationWork, err := service.enqueueEvaluation(*candidate)
	if err != nil {
		return nil, generationFailure("evaluation_enqueue_failed", "candidate generation could not schedule evaluation", err)
	}
	return &EvolutionGenerationResult{Candidate: candidate, EvaluationWork: evaluationWork}, nil
}

func (service *EvolutionGenerationService) buildAgentCandidate(run EvolutionRun, work EvolutionWork) (EvolutionCandidateInput, error) {
	if len(run.BaselineReleaseIDs) == 0 {
		return EvolutionCandidateInput{}, generationFailure("baseline_release_required", "candidate generation requires a baseline release", nil)
	}
	mode := AgentCompilationModeStudy
	if run.RunType == EvolutionRunCombined {
		mode = AgentCompilationModeDual
	}
	request := AgentCompilationRequest{
		SchemaVersion: AgentCompilationRequestSchemaVersion,
		Mode:          mode, PrimaryReleaseID: run.BaselineReleaseIDs[0],
		SupportingReleaseIDs: append([]string(nil), run.BaselineReleaseIDs[1:]...),
		Version:              evolutionCandidateVersion(run.BaselinePackageVersion, run.Attempt),
	}
	compilation, err := service.compileAgent(service.knowledge, request)
	if err != nil {
		return EvolutionCandidateInput{}, generationFailure("agent_compilation_failed", "candidate generation could not compile the agent", err)
	}
	if compilation == nil {
		return EvolutionCandidateInput{}, generationFailure("agent_compilation_failed", "candidate generation could not compile the agent", nil)
	}
	if err := ValidateAgentCompilation(*compilation); err != nil {
		return EvolutionCandidateInput{}, generationFailure("agent_compilation_invalid", "candidate generation produced an invalid agent compilation", err)
	}
	if compilation.Status == AgentCompilationStatusBlocked {
		code := "agent_compilation_blocked"
		for _, candidate := range compilation.Candidates {
			if len(candidate.Issues) > 0 && validateEvolutionCode("failure_code", candidate.Issues[0].Code) == nil {
				code = candidate.Issues[0].Code
				break
			}
		}
		return EvolutionCandidateInput{}, generationFailure(code, "candidate generation is blocked", nil)
	}
	return EvolutionCandidateInput{
		IdempotencyKey: generationCandidateKey(work, compilation.CompilationID), RunID: run.RunID,
		CandidateType: EvolutionCandidateAgentCompilation, BaselineIdentity: evolutionBaselineIdentity(run),
		ChangeSummary: "已生成不可变 Agent 编译候选，等待确定性评估。", GeneratorVersion: service.generatorVersion,
		Artifact: compilation,
	}, nil
}

func (service *EvolutionGenerationService) buildKnowledgeCandidate(run EvolutionRun, work EvolutionWork) (EvolutionCandidateInput, error) {
	if len(run.BaselineReleaseIDs) == 0 {
		return EvolutionCandidateInput{}, generationFailure("baseline_release_required", "candidate generation requires a baseline release", nil)
	}
	releaseID := run.BaselineReleaseIDs[0]
	tasks, err := service.knowledge.ListKnowledgeReverifications(releaseID)
	if err != nil {
		return EvolutionCandidateInput{}, generationFailure("reverification_unavailable", "candidate generation could not inspect reverification", err)
	}
	var ready *KnowledgeReverificationTask
	for index := range tasks {
		if tasks[index].Status != KnowledgeReverificationCandidateReady {
			continue
		}
		if ready == nil || tasks[index].CompletedAt > ready.CompletedAt {
			copy := tasks[index]
			ready = &copy
		}
	}
	if ready == nil {
		return EvolutionCandidateInput{}, generationFailure("knowledge_candidate_not_ready", "candidate generation is waiting for reverification", nil)
	}
	return EvolutionCandidateInput{
		IdempotencyKey: generationCandidateKey(work, ready.TaskID), RunID: run.RunID,
		CandidateType: EvolutionCandidateKnowledgeRelease, BaselineIdentity: evolutionBaselineIdentity(run),
		ChangeSummary: "已记录知识重验证候选，尚未发布新的知识版本。", GeneratorVersion: service.generatorVersion,
		Artifact: ready,
	}, nil
}

func (service *EvolutionGenerationService) enqueueEvaluation(candidate EvolutionCandidate) (*EvolutionWork, error) {
	work, _, err := service.control.EnqueueEvolutionWork(EvolutionWorkInput{
		IdempotencyKey: "sha256:" + evolutionWorkerPayloadHash("evaluation:"+candidate.ContentHash),
		RunID:          candidate.RunID, Capability: EvolutionCapabilityEvaluation,
		ArtifactRef: candidate.ArtifactRef, MaxAttempts: evolutionEvaluationMaxAttempts,
	})
	return work, err
}

func evolutionCandidateVersion(baseline string, attempt int) string {
	baseline = strings.TrimSpace(baseline)
	match := agentCompilationVersionPattern.FindStringSubmatch(baseline)
	if len(match) < 4 {
		return "0.0.1-evolution." + strconv.Itoa(max(attempt, 1))
	}
	patch, err := strconv.Atoi(match[3])
	if err != nil {
		patch = 0
	}
	return strings.Join([]string{match[1], match[2], strconv.Itoa(patch + 1)}, ".") + "-evolution." + strconv.Itoa(max(attempt, 1))
}

func evolutionBaselineIdentity(run EvolutionRun) string {
	payload, _ := json.Marshal(struct {
		PackageID      string   `json:"package_id"`
		PackageVersion string   `json:"package_version"`
		ReleaseIDs     []string `json:"release_ids"`
	}{run.PackageID, run.BaselinePackageVersion, sortedUniqueStrings(run.BaselineReleaseIDs)})
	return "sha256:" + evolutionWorkerPayloadHash(string(payload))
}

func generationCandidateKey(work EvolutionWork, resultID string) string {
	return "sha256:" + evolutionWorkerPayloadHash(strings.Join([]string{
		"generation", work.WorkID, strconv.Itoa(work.Attempt), resultID,
	}, ":"))
}

func generationFailure(code, message string, err error) error {
	if validateEvolutionCode("failure_code", code) != nil {
		code = "generation_failed"
	}
	if len([]rune(message)) > EvolutionFailureMessageMaxRunes {
		message = string([]rune(message)[:EvolutionFailureMessageMaxRunes])
	}
	return &EvolutionGenerationFailure{Code: code, Message: message, Err: err}
}
