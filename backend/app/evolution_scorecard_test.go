package app

import (
	"errors"
	"math"
	"testing"
)

func TestEvolutionScorecardRequiresThreePointGain(t *testing.T) {
	input := validEvolutionScorecardInput()
	input.CandidateMetrics = map[string]float64{
		"answer_quality": 83, "evidence_quality": 83, "task_completion": 83,
		"reliability": 83, "cost": 83, "latency": 83,
	}
	scorecard, err := CalculateEvolutionScorecard(input)
	if err != nil {
		t.Fatal(err)
	}
	if scorecard.WeightedScore != 83 || scorecard.BaselineScore != 80 || scorecard.Delta != 3 || scorecard.Decision != EvolutionScorecardAwaitingApproval {
		t.Fatalf("scorecard = %#v", scorecard)
	}

	input.CandidateMetrics["latency"] = 82.999
	scorecard, err = CalculateEvolutionScorecard(input)
	if err != nil {
		t.Fatal(err)
	}
	if scorecard.Decision != EvolutionScorecardDiscarded {
		t.Fatalf("sub-threshold decision = %s delta=%v", scorecard.Decision, scorecard.Delta)
	}
}

func TestEvolutionScorecardHardGateBlocksHigherScore(t *testing.T) {
	input := validEvolutionScorecardInput()
	for metric := range input.CandidateMetrics {
		input.CandidateMetrics[metric] = 99
	}
	input.HardGates["privacy"] = false
	scorecard, err := CalculateEvolutionScorecard(input)
	if err != nil {
		t.Fatal(err)
	}
	if scorecard.Decision != EvolutionScorecardBlocked || scorecard.WeightedScore <= scorecard.BaselineScore {
		t.Fatalf("hard-gate scorecard = %#v", scorecard)
	}
}

func TestEvolutionScorecardRejectsMissingOrNonFiniteMetrics(t *testing.T) {
	for name, mutate := range map[string]func(*EvolutionScorecardInput){
		"missing": func(input *EvolutionScorecardInput) { delete(input.CandidateMetrics, "reliability") },
		"nan":     func(input *EvolutionScorecardInput) { input.BaselineMetrics["cost"] = math.NaN() },
		"infinity": func(input *EvolutionScorecardInput) {
			input.CandidateMetrics["latency"] = math.Inf(1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := validEvolutionScorecardInput()
			mutate(&input)
			if _, err := CalculateEvolutionScorecard(input); err == nil {
				t.Fatal("invalid metrics should fail")
			}
		})
	}
}

func TestEvolutionScorecardUsesVersionedWeightsAndStableRounding(t *testing.T) {
	input := validEvolutionScorecardInput()
	input.WeightVersion = "weights.v2"
	input.MetricWeights = map[string]float64{
		"answer_quality": 0.25, "evidence_quality": 0.25, "task_completion": 0.20,
		"reliability": 0.10, "cost": 0.10, "latency": 0.10,
	}
	input.CandidateMetrics["answer_quality"] = 83.333333
	scorecard, err := CalculateEvolutionScorecard(input)
	if err != nil {
		t.Fatal(err)
	}
	if scorecard.WeightVersion != "weights.v2" || scorecard.MetricWeights["latency"] != 0.10 || scorecard.WeightedScore != 80.8333 || scorecard.Delta != 0.8333 {
		t.Fatalf("versioned scorecard = %#v", scorecard)
	}
}

func TestEvolutionScorecardPersistenceIsIdempotentAndHistorical(t *testing.T) {
	store := newEvolutionTestStore(t)
	run := createGeneratingEvolutionRunForType(t, store, EvolutionRunAgentPolicy, "scorecard-run", "agent-a", "1.0.0", []string{"release-a"})
	candidate, _, err := store.SaveEvolutionCandidate(EvolutionCandidateInput{
		IdempotencyKey: "sha256:" + evolutionWorkerPayloadHash("scorecard-candidate"), RunID: run.RunID,
		CandidateType: EvolutionCandidateAgentCompilation, BaselineIdentity: "sha256:" + workerHex('b'),
		ChangeSummary: "候选", GeneratorVersion: "generator.v1", Artifact: map[string]any{"ready": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionRun(run.RunID, EvolutionEvaluating, EvolutionTransitionInput{Actor: "test", Code: "evaluate"}); err != nil {
		t.Fatal(err)
	}
	input := validEvolutionScorecardInput()
	input.CandidateID = candidate.CandidateID
	input.BaselineIdentity = candidate.BaselineIdentity
	input.SuiteIdentity = "sha256:" + workerHex('d')
	input.ComponentContributions = map[string]float64{"agent": 4, "knowledge": 3}
	scorecard, created, err := store.SaveEvolutionScorecard("sha256:"+evolutionWorkerPayloadHash("scorecard-save"), input)
	if err != nil || !created {
		t.Fatalf("save = %#v, %v, %v", scorecard, created, err)
	}
	replayed, created, err := store.SaveEvolutionScorecard("sha256:"+evolutionWorkerPayloadHash("scorecard-save"), input)
	if err != nil || created || replayed.ScorecardID != scorecard.ScorecardID {
		t.Fatalf("replay = %#v, %v, %v", replayed, created, err)
	}
	input.WeightVersion = "weights.v2"
	if _, _, err := store.SaveEvolutionScorecard("sha256:"+evolutionWorkerPayloadHash("scorecard-save"), input); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	loaded, err := store.LoadEvolutionScorecardForCandidate(candidate.CandidateID)
	if err != nil || loaded.WeightVersion != DefaultEvolutionWeightVersion || loaded.MetricWeights["answer_quality"] != 0.30 ||
		loaded.SuiteIdentity != input.SuiteIdentity || loaded.ComponentContributions["agent"] != 4 || loaded.ComponentContributions["knowledge"] != 3 {
		t.Fatalf("loaded historical scorecard = %#v, %v", loaded, err)
	}
	secondInput := validEvolutionScorecardInput()
	secondInput.CandidateID = candidate.CandidateID
	secondInput.BaselineIdentity = candidate.BaselineIdentity
	secondInput.WeightVersion = "weights.v2"
	secondInput.SuiteIdentity = "sha256:" + workerHex('e')
	second, created, err := store.SaveEvolutionScorecard("sha256:"+evolutionWorkerPayloadHash("scorecard-save-v2"), secondInput)
	if err != nil || !created || second.ScorerVersion == scorecard.ScorerVersion {
		t.Fatalf("versioned historical scorecard = %#v, %v, %v", second, created, err)
	}
	latest, err := store.LoadEvolutionScorecardForCandidate(candidate.CandidateID)
	if err != nil || latest.ScorecardID != second.ScorecardID {
		t.Fatalf("latest versioned scorecard = %#v, %v; want %s", latest, err, second.ScorecardID)
	}
}

func validEvolutionScorecardInput() EvolutionScorecardInput {
	baseline := map[string]float64{
		"answer_quality": 80, "evidence_quality": 80, "task_completion": 80,
		"reliability": 80, "cost": 80, "latency": 80,
	}
	candidate := map[string]float64{}
	for metric, value := range baseline {
		candidate[metric] = value
	}
	return EvolutionScorecardInput{
		CandidateID: "candidate-a", BaselineIdentity: "sha256:" + workerHex('a'),
		SuiteVersion: "suite.v1", ScorerVersion: "scorer.v1", WeightVersion: DefaultEvolutionWeightVersion,
		HardGates:       map[string]bool{"privacy": true, "citations": true},
		BaselineMetrics: baseline, CandidateMetrics: candidate,
		FailureCaseRefs: []string{},
	}
}
