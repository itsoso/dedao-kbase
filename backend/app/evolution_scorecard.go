package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	DefaultEvolutionWeightVersion = "evolution-weights.v1"
	EvolutionScorecardMinimumGain = 3.0

	EvolutionScorecardAwaitingApproval = "awaiting_approval"
	EvolutionScorecardBlocked          = "blocked"
	EvolutionScorecardDiscarded        = "discarded"
)

var ErrEvolutionScorecardNotFound = errors.New("evolution scorecard not found")

var DefaultEvolutionMetricWeights = map[string]float64{
	"answer_quality":   0.30,
	"evidence_quality": 0.25,
	"task_completion":  0.20,
	"reliability":      0.10,
	"cost":             0.10,
	"latency":          0.05,
}

type EvolutionScorecardInput struct {
	CandidateID            string             `json:"candidate_id"`
	BaselineIdentity       string             `json:"baseline_identity"`
	SuiteVersion           string             `json:"suite_version"`
	ScorerVersion          string             `json:"scorer_version"`
	WeightVersion          string             `json:"weight_version"`
	SuiteIdentity          string             `json:"suite_identity"`
	HardGates              map[string]bool    `json:"hard_gates"`
	BaselineMetrics        map[string]float64 `json:"baseline_metrics"`
	CandidateMetrics       map[string]float64 `json:"candidate_metrics"`
	MetricWeights          map[string]float64 `json:"metric_weights,omitempty"`
	ComponentContributions map[string]float64 `json:"component_contributions,omitempty"`
	FailureCaseRefs        []string           `json:"failure_case_refs"`
}

type evolutionScorecardMetricsEnvelope struct {
	WeightVersion          string             `json:"weight_version"`
	BaselineMetrics        map[string]float64 `json:"baseline_metrics"`
	CandidateMetrics       map[string]float64 `json:"candidate_metrics"`
	NormalizedMetrics      map[string]float64 `json:"normalized_metrics"`
	MetricWeights          map[string]float64 `json:"metric_weights"`
	SuiteIdentity          string             `json:"suite_identity"`
	ComponentContributions map[string]float64 `json:"component_contributions,omitempty"`
}

func CalculateEvolutionScorecard(input EvolutionScorecardInput) (*EvolutionScorecard, error) {
	input.CandidateID = strings.TrimSpace(input.CandidateID)
	input.BaselineIdentity = strings.TrimSpace(input.BaselineIdentity)
	input.SuiteVersion = strings.TrimSpace(input.SuiteVersion)
	input.ScorerVersion = strings.TrimSpace(input.ScorerVersion)
	input.WeightVersion = strings.TrimSpace(input.WeightVersion)
	input.SuiteIdentity = strings.TrimSpace(input.SuiteIdentity)
	if input.WeightVersion == "" {
		input.WeightVersion = DefaultEvolutionWeightVersion
	}
	if input.SuiteIdentity == "" {
		input.SuiteIdentity = "sha256:" + evolutionWorkerPayloadHash(input.SuiteVersion)
	}
	if err := validateEvolutionReference("suite_identity", input.SuiteIdentity); err != nil {
		return nil, err
	}
	input.ScorerVersion = boundEvolutionScorerVersion(input.ScorerVersion, input.WeightVersion, input.SuiteIdentity)
	if len(input.MetricWeights) == 0 {
		input.MetricWeights = cloneEvolutionMetrics(DefaultEvolutionMetricWeights)
	}
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "candidate_id", value: input.CandidateID},
		evolutionStringField{name: "suite_version", value: input.SuiteVersion},
		evolutionStringField{name: "scorer_version", value: input.ScorerVersion},
		evolutionStringField{name: "weight_version", value: input.WeightVersion},
	); err != nil {
		return nil, err
	}
	if err := validateEvolutionReference("baseline_identity", input.BaselineIdentity); err != nil {
		return nil, err
	}
	if err := validateEvolutionHardGates(input.HardGates); err != nil {
		return nil, err
	}
	if err := validateEvolutionReferences("failure_case_refs", input.FailureCaseRefs, false); err != nil {
		return nil, err
	}
	if err := validateEvolutionScorecardMetrics(input.BaselineMetrics, input.CandidateMetrics, input.MetricWeights); err != nil {
		return nil, err
	}
	if len(input.ComponentContributions) > 0 {
		if err := validateEvolutionMetrics("component_contributions", input.ComponentContributions, true); err != nil {
			return nil, err
		}
	}
	baselineRaw := weightedEvolutionScoreRaw(input.BaselineMetrics, input.MetricWeights)
	candidateRaw := weightedEvolutionScoreRaw(input.CandidateMetrics, input.MetricWeights)
	baselineScore := roundEvolutionScore(baselineRaw)
	candidateScore := roundEvolutionScore(candidateRaw)
	delta := roundEvolutionScore(candidateRaw - baselineRaw)
	decision := EvolutionScorecardAwaitingApproval
	for _, passed := range input.HardGates {
		if !passed {
			decision = EvolutionScorecardBlocked
			break
		}
	}
	if decision != EvolutionScorecardBlocked && candidateRaw-baselineRaw < EvolutionScorecardMinimumGain {
		decision = EvolutionScorecardDiscarded
	}
	scorecard := &EvolutionScorecard{
		CandidateID: input.CandidateID, BaselineIdentity: input.BaselineIdentity,
		SuiteVersion: input.SuiteVersion, ScorerVersion: input.ScorerVersion, WeightVersion: input.WeightVersion,
		HardGates: cloneEvolutionHardGates(input.HardGates), Metrics: cloneEvolutionMetrics(input.CandidateMetrics),
		BaselineMetrics: cloneEvolutionMetrics(input.BaselineMetrics), CandidateMetrics: cloneEvolutionMetrics(input.CandidateMetrics),
		MetricWeights: cloneEvolutionMetrics(input.MetricWeights), WeightedScore: candidateScore,
		BaselineScore: baselineScore, Delta: delta, Decision: decision,
		FailureCaseRefs: append([]string(nil), input.FailureCaseRefs...),
	}
	scorecard.SuiteIdentity = input.SuiteIdentity
	scorecard.ComponentContributions = cloneEvolutionMetrics(input.ComponentContributions)
	identityPayload, err := json.Marshal(scorecard)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(identityPayload)
	scorecard.ScorecardID = "sha256:" + hex.EncodeToString(digest[:])
	if err := scorecard.Validate(); err != nil {
		return nil, err
	}
	return scorecard, nil
}

func boundEvolutionScorerVersion(base, weightVersion, suiteIdentity string) string {
	suffix := "-binding-" + evolutionWorkerPayloadHash(weightVersion + ":" + suiteIdentity)[:16]
	baseRunes := []rune(strings.TrimSpace(base))
	maxBase := EvolutionIdentityMaxRunes - len([]rune(suffix))
	if len(baseRunes) > maxBase {
		baseRunes = baseRunes[:maxBase]
	}
	return string(baseRunes) + suffix
}

func (store *EvolutionControlStore) SaveEvolutionScorecard(idempotencyKey string, input EvolutionScorecardInput) (*EvolutionScorecard, bool, error) {
	if !isEvolutionOpaqueID(idempotencyKey) {
		return nil, false, fmt.Errorf("idempotency_key must be a canonical UUID or sha256 identity")
	}
	scorecard, err := CalculateEvolutionScorecard(input)
	if err != nil {
		return nil, false, err
	}
	metricsPayload, err := json.Marshal(evolutionScorecardMetricsEnvelope{
		WeightVersion: scorecard.WeightVersion, BaselineMetrics: scorecard.BaselineMetrics,
		CandidateMetrics: scorecard.CandidateMetrics, NormalizedMetrics: scorecard.Metrics,
		MetricWeights: scorecard.MetricWeights,
		SuiteIdentity: scorecard.SuiteIdentity, ComponentContributions: scorecard.ComponentContributions,
	})
	if err != nil {
		return nil, false, err
	}
	hardGatesPayload, _ := json.Marshal(scorecard.HardGates)
	failureRefsPayload, _ := json.Marshal(scorecard.FailureCaseRefs)
	tx, err := store.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin save evolution scorecard", err)
	}
	defer func() { _ = tx.Rollback() }()
	if existing, loadErr := loadEvolutionScorecardByIdempotencyKeyTx(tx, idempotencyKey); loadErr == nil {
		if existing.ScorecardID != scorecard.ScorecardID {
			return nil, false, ErrEvolutionIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, wrapEvolutionSQLiteWriteError("commit evolution scorecard replay", err)
		}
		return existing, false, nil
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return nil, false, loadErr
	}
	candidate, err := loadEvolutionCandidate(tx.QueryRow(`
		SELECT candidate_id, run_id, candidate_type, content_hash, artifact_ref,
			baseline_identity, change_summary, generator_version, created_at
		FROM evolution_candidates WHERE candidate_id = ?
	`, scorecard.CandidateID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrEvolutionCandidateNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if candidate.BaselineIdentity != scorecard.BaselineIdentity {
		return nil, false, ErrEvolutionIdempotencyConflict
	}
	run, err := scanEvolutionRun(tx.QueryRow(evolutionRunSelect+` WHERE run_id = ?`, candidate.RunID))
	if err != nil {
		return nil, false, err
	}
	if run.Status != EvolutionEvaluating {
		return nil, false, fmt.Errorf("%w: scorecard requires evaluating run", ErrEvolutionTransitionConflict)
	}
	createdAt := store.now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`
		INSERT INTO evolution_scorecards (
			scorecard_id, idempotency_key, candidate_id, baseline_identity, suite_version,
			scorer_version, hard_gates_json, metrics_json, weighted_score, baseline_score,
			delta, decision, failure_case_refs_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, scorecard.ScorecardID, idempotencyKey, scorecard.CandidateID, scorecard.BaselineIdentity,
		scorecard.SuiteVersion, scorecard.ScorerVersion, string(hardGatesPayload), string(metricsPayload),
		scorecard.WeightedScore, scorecard.BaselineScore, scorecard.Delta, scorecard.Decision,
		string(failureRefsPayload), createdAt); err != nil {
		return nil, false, fmt.Errorf("insert evolution scorecard: %w", err)
	}
	eventID, err := newEvolutionStoreID("event")
	if err != nil {
		return nil, false, err
	}
	event := EvolutionEvent{
		EventID: eventID, RunID: candidate.RunID, EventType: "scorecard_ready", Actor: scorecard.ScorerVersion,
		FromStatus: EvolutionEvaluating, ToStatus: EvolutionEvaluating, Code: scorecard.Decision,
		Message: "deterministic evolution scorecard is ready", ArtifactRefs: []string{"scorecard:" + scorecard.ScorecardID}, CreatedAt: createdAt,
	}
	if err := store.insertEventTx(tx, event); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit evolution scorecard", err)
	}
	return scorecard, true, nil
}

func (store *EvolutionControlStore) LoadEvolutionScorecardForCandidate(candidateID string) (*EvolutionScorecard, error) {
	if err := validateEvolutionIdentity("candidate_id", candidateID); err != nil {
		return nil, err
	}
	scorecard, err := loadEvolutionScorecard(store.db.QueryRow(`
		SELECT scorecard_id, candidate_id, baseline_identity, suite_version, scorer_version,
			hard_gates_json, metrics_json, weighted_score, baseline_score, delta, decision,
			failure_case_refs_json
		FROM evolution_scorecards WHERE candidate_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1
	`, candidateID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEvolutionScorecardNotFound
	}
	return scorecard, err
}

type evolutionScorecardScanner interface{ Scan(...any) error }

func loadEvolutionScorecard(scanner evolutionScorecardScanner) (*EvolutionScorecard, error) {
	var scorecard EvolutionScorecard
	var hardGatesJSON, metricsJSON, failureRefsJSON string
	if err := scanner.Scan(&scorecard.ScorecardID, &scorecard.CandidateID, &scorecard.BaselineIdentity,
		&scorecard.SuiteVersion, &scorecard.ScorerVersion, &hardGatesJSON, &metricsJSON,
		&scorecard.WeightedScore, &scorecard.BaselineScore, &scorecard.Delta, &scorecard.Decision,
		&failureRefsJSON); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(hardGatesJSON), &scorecard.HardGates); err != nil {
		return nil, err
	}
	var metrics evolutionScorecardMetricsEnvelope
	if err := json.Unmarshal([]byte(metricsJSON), &metrics); err != nil {
		return nil, err
	}
	if len(metrics.NormalizedMetrics) == 0 {
		var legacy map[string]float64
		if err := json.Unmarshal([]byte(metricsJSON), &legacy); err != nil {
			return nil, err
		}
		scorecard.Metrics = legacy
	} else {
		scorecard.WeightVersion = metrics.WeightVersion
		scorecard.BaselineMetrics = metrics.BaselineMetrics
		scorecard.CandidateMetrics = metrics.CandidateMetrics
		scorecard.Metrics = metrics.NormalizedMetrics
		scorecard.MetricWeights = metrics.MetricWeights
		scorecard.SuiteIdentity = metrics.SuiteIdentity
		scorecard.ComponentContributions = metrics.ComponentContributions
	}
	if err := json.Unmarshal([]byte(failureRefsJSON), &scorecard.FailureCaseRefs); err != nil {
		return nil, err
	}
	return &scorecard, scorecard.Validate()
}

func loadEvolutionScorecardByIdempotencyKeyTx(tx *sql.Tx, key string) (*EvolutionScorecard, error) {
	return loadEvolutionScorecard(tx.QueryRow(`
		SELECT scorecard_id, candidate_id, baseline_identity, suite_version, scorer_version,
			hard_gates_json, metrics_json, weighted_score, baseline_score, delta, decision,
			failure_case_refs_json
		FROM evolution_scorecards WHERE idempotency_key = ?
	`, key))
}

func validateEvolutionScorecardMetrics(baseline, candidate, weights map[string]float64) error {
	if len(baseline) != len(weights) || len(candidate) != len(weights) || len(weights) == 0 {
		return fmt.Errorf("baseline, candidate, and weight metrics must have identical non-empty keys")
	}
	weightTotal := 0.0
	keys := make([]string, 0, len(weights))
	for metric := range weights {
		keys = append(keys, metric)
	}
	sort.Strings(keys)
	for _, metric := range keys {
		weight := weights[metric]
		baselineValue, baselineFound := baseline[metric]
		candidateValue, candidateFound := candidate[metric]
		if strings.TrimSpace(metric) == "" || !baselineFound || !candidateFound {
			return fmt.Errorf("metric %q is missing", metric)
		}
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 || weight > 1 {
			return fmt.Errorf("metric weight %q is invalid", metric)
		}
		for name, value := range map[string]float64{"baseline": baselineValue, "candidate": candidateValue} {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
				return fmt.Errorf("%s metric %q must be finite and between 0 and 100", name, metric)
			}
		}
		weightTotal += weight
	}
	if math.Abs(weightTotal-1) > 0.0000001 {
		return fmt.Errorf("metric weights must total 1")
	}
	return nil
}

func weightedEvolutionScore(metrics, weights map[string]float64) float64 {
	return roundEvolutionScore(weightedEvolutionScoreRaw(metrics, weights))
}

func weightedEvolutionScoreRaw(metrics, weights map[string]float64) float64 {
	total := 0.0
	for metric, weight := range weights {
		total += metrics[metric] * weight
	}
	return total
}

func roundEvolutionScore(value float64) float64 {
	result := math.Round(value*10_000) / 10_000
	if result == 0 {
		return 0
	}
	return result
}

func cloneEvolutionMetrics(input map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneEvolutionHardGates(input map[string]bool) map[string]bool {
	result := make(map[string]bool, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
