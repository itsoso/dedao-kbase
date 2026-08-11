package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	EvolutionSignalCooldown = 24 * time.Hour

	EvolutionSignalTaskCompletionDrop        = "task_completion_drop"
	EvolutionSignalAnswerFailureRate         = "answer_failure_rate"
	EvolutionSignalUserFeedbackIncorrect     = "user_feedback_incorrect"
	EvolutionSignalToolFailure               = "tool_failure"
	EvolutionSignalLatencyRegression         = "latency_regression"
	EvolutionSignalCostRegression            = "cost_regression"
	EvolutionSignalModelCapabilityChange     = "model_capability_change"
	EvolutionSignalManualPolicyImprovement   = "manual_policy_improvement"
	EvolutionSignalRegressionFailure         = "regression_failure"
	EvolutionSignalMissingEvidence           = "missing_evidence"
	EvolutionSignalSourceAdded               = "source_added"
	EvolutionSignalReleaseStale              = "release_stale"
	EvolutionSignalEvidenceConflict          = "evidence_conflict"
	EvolutionSignalCitationUnavailable       = "citation_unavailable"
	EvolutionSignalKnowledgeFeedbackError    = "knowledge_feedback_incorrect"
	EvolutionSignalKnowledgeCoverageGap      = "knowledge_coverage_gap"
	EvolutionSignalReverificationFailed      = "reverification_failed"
	EvolutionSignalCandidateReleaseAvailable = "candidate_release_available"

	EvolutionSignalSourceRuntimeMetric  = "runtime_metric"
	EvolutionSignalSourceEvaluation     = "evaluation"
	EvolutionSignalSourceUserFeedback   = "user_feedback"
	EvolutionSignalSourceOperator       = "operator"
	EvolutionSignalSourceKnowledgeStore = "knowledge_store"
	EvolutionSignalSourceObservation    = "observation"
	EvolutionSignalSourceReverification = "reverification"

	EvolutionSignalSeverityLow      = "low"
	EvolutionSignalSeverityMedium   = "medium"
	EvolutionSignalSeverityHigh     = "high"
	EvolutionSignalSeverityCritical = "critical"

	evolutionSignalReasonIngested   = "signal_ingested"
	evolutionSignalReasonAggregated = "signal_aggregated"
	evolutionUnresolvedBaseline     = "unresolved"
	evolutionRunPageDefaultLimit    = 50
	evolutionRunPageMaxLimit        = 200
	evolutionRunCursorMaxPayload    = 256
	evolutionRunCursorMaxInputBytes = (evolutionRunCursorMaxPayload*8 + 5) / 6

	EvolutionObservationPayloadComplete       = "complete"
	EvolutionObservationPayloadLegacyHashOnly = "legacy_hash_only"
)

var ErrEvolutionRunCursorNotFound = errors.New("evolution run cursor not found")

type EvolutionSignalInput struct {
	IdempotencyKey string    `json:"idempotency_key"`
	SignalType     string    `json:"signal_type"`
	SourceType     string    `json:"source_type"`
	SourceID       string    `json:"source_id"`
	PackageID      string    `json:"package_id"`
	ReleaseID      string    `json:"release_id"`
	Severity       string    `json:"severity"`
	ObservedValue  float64   `json:"observed_value"`
	BaselineValue  float64   `json:"baseline_value"`
	EvidenceRefs   []string  `json:"evidence_refs"`
	ObservedAt     time.Time `json:"observed_at"`
}

type EvolutionPriorityInput struct {
	Risk            float64
	Impact          float64
	ExpectedBenefit float64
	WaitingSince    time.Time
	Now             time.Time
}

type EvolutionOverview struct {
	OpenRuns         []EvolutionRun                  `json:"open_runs"`
	AgentFleet       []EvolutionAgentFleetProjection `json:"agent_fleet"`
	TotalOpenRuns    int                             `json:"total_open_runs"`
	AwaitingApproval int                             `json:"awaiting_approval"`
	Blocked          int                             `json:"blocked"`
}

type EvolutionRunPage struct {
	Runs       []EvolutionRun `json:"runs"`
	NextCursor string         `json:"next_cursor"`
}

type evolutionRunCursor struct {
	CreatedAtUnixNano int64  `json:"created_at_unix_nano"`
	RunID             string `json:"run_id"`
}

type EvolutionAgentFleetProjection struct {
	PackageID string               `json:"package_id"`
	Current   *AgentPackageRecord  `json:"current"`
	History   []AgentPackageRecord `json:"history"`
	OpenRuns  []EvolutionRun       `json:"open_runs"`
}

type normalizedEvolutionSignalInput struct {
	IdempotencyKey string   `json:"idempotency_key"`
	SignalType     string   `json:"signal_type"`
	SourceType     string   `json:"source_type"`
	SourceID       string   `json:"source_id"`
	PackageID      string   `json:"package_id"`
	ReleaseID      string   `json:"release_id"`
	Severity       string   `json:"severity"`
	ObservedValue  float64  `json:"observed_value"`
	BaselineValue  float64  `json:"baseline_value"`
	EvidenceRefs   []string `json:"evidence_refs"`
	ObservedAt     string   `json:"observed_at"`
}

type EvolutionSignalObservation struct {
	ObservationID   string   `json:"observation_id"`
	RequestKeyHash  string   `json:"request_key_hash"`
	InputHash       string   `json:"input_hash"`
	SignalID        string   `json:"signal_id"`
	RunID           string   `json:"run_id"`
	PayloadFidelity string   `json:"payload_fidelity"`
	SignalType      *string  `json:"signal_type,omitempty"`
	SourceType      *string  `json:"source_type,omitempty"`
	SourceID        *string  `json:"source_id,omitempty"`
	PackageID       *string  `json:"package_id,omitempty"`
	ReleaseID       *string  `json:"release_id,omitempty"`
	Severity        *string  `json:"severity,omitempty"`
	ObservedValue   *float64 `json:"observed_value,omitempty"`
	BaselineValue   *float64 `json:"baseline_value,omitempty"`
	EvidenceRefs    []string `json:"evidence_refs,omitempty"`
	ObservedAt      *string  `json:"observed_at,omitempty"`
	CreatedAt       string   `json:"created_at"`
}

// CalculateEvolutionPriority gives risk the strongest influence, followed by
// impact, expected benefit, and a bounded one-week aging term. Every component
// is explicit and constrained to [0,1], so the resulting [0,100] score remains
// explainable and stable.
func CalculateEvolutionPriority(input EvolutionPriorityInput) (float64, error) {
	components := []struct {
		name  string
		value float64
	}{
		{name: "risk", value: input.Risk},
		{name: "impact", value: input.Impact},
		{name: "expected_benefit", value: input.ExpectedBenefit},
	}
	for _, component := range components {
		if math.IsNaN(component.value) || math.IsInf(component.value, 0) || component.value < 0 || component.value > 1 {
			return 0, fmt.Errorf("%s must be between 0 and 1", component.name)
		}
	}
	if input.Now.IsZero() || input.WaitingSince.IsZero() {
		return 0, fmt.Errorf("priority timestamps are required")
	}
	if input.WaitingSince.After(input.Now) {
		return 0, fmt.Errorf("waiting_since must not be after now")
	}
	wait := input.Now.Sub(input.WaitingSince).Hours() / (7 * 24)
	wait = math.Max(0, math.Min(1, wait))
	return input.Risk*40 + input.Impact*30 + input.ExpectedBenefit*20 + wait*10, nil
}

// IngestSignal atomically records a bounded signal and either creates or
// aggregates its evolution run. created reports whether a new signal identity
// was inserted; a deduplicated observation may still create a new run after the
// cooldown or after a terminal run.
func (s *EvolutionControlStore) IngestSignal(input EvolutionSignalInput) (*EvolutionSignal, *EvolutionRun, bool, error) {
	normalized, fingerprint, deduplicationKey, runType, err := normalizeEvolutionSignalInput(input)
	if err != nil {
		return nil, nil, false, err
	}
	now := s.now().UTC()
	if now.IsZero() {
		return nil, nil, false, fmt.Errorf("evolution store clock returned zero time")
	}
	timestamp := now.Format(time.RFC3339Nano)

	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, nil, false, wrapEvolutionSQLiteWriteError("begin evolution signal ingestion", err)
	}
	defer func() { _ = tx.Rollback() }()

	requestKeyHash := evolutionSignalRequestKeyHash(normalized.IdempotencyKey)
	observation, err := loadEvolutionSignalObservationByRequestHashTx(tx, requestKeyHash)
	if err == nil {
		if observation.InputHash != fingerprint {
			return nil, nil, false, ErrEvolutionIdempotencyConflict
		}
		replayed, err := loadEvolutionSignalByID(tx, observation.SignalID)
		if err != nil {
			return nil, nil, false, err
		}
		replayedRun, err := scanEvolutionRun(tx.QueryRow(evolutionRunSelect+` WHERE run_id = ?`, observation.RunID))
		if err != nil {
			return nil, nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, false, wrapEvolutionSQLiteWriteError("commit evolution signal replay", err)
		}
		return cloneEvolutionSignal(replayed), cloneEvolutionRun(replayedRun), false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, fmt.Errorf("find evolution signal observation replay: %w", err)
	}

	signal, err := loadEvolutionSignalByDeduplicationKey(tx, deduplicationKey)
	created := false
	if errors.Is(err, sql.ErrNoRows) {
		signalID, err := newEvolutionStoreID("signal")
		if err != nil {
			return nil, nil, false, err
		}
		signal = &EvolutionSignal{
			SignalID: signalID, SignalType: normalized.SignalType, SourceType: normalized.SourceType,
			SourceID: normalized.SourceID, PackageID: normalized.PackageID, ReleaseID: normalized.ReleaseID,
			Severity: normalized.Severity, ObservedValue: normalized.ObservedValue, BaselineValue: normalized.BaselineValue,
			DeduplicationKey: deduplicationKey, EvidenceRefs: append([]string{}, normalized.EvidenceRefs...), ObservedAt: normalized.ObservedAt,
		}
		if err := insertEvolutionSignalTx(tx, requestKeyHash, signal, timestamp); err != nil {
			return nil, nil, false, err
		}
		created = true
	} else if err != nil {
		return nil, nil, false, fmt.Errorf("find deduplicated evolution signal: %w", err)
	}

	run, err := findEvolutionRunForAggregationTx(tx, normalized, runType, now)
	if err != nil {
		return nil, nil, false, err
	}
	if run == nil {
		run, err = s.createEvolutionRunForSignalTx(tx, normalized, runType, signal.SignalID, timestamp, now)
	} else {
		run, err = s.aggregateEvolutionSignalTx(tx, run, normalized, runType, signal.SignalID, timestamp, now)
	}
	if err != nil {
		return nil, nil, false, err
	}
	if err := insertEvolutionSignalObservationTx(tx, normalized, requestKeyHash, fingerprint, signal.SignalID, run.RunID, timestamp); err != nil {
		return nil, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, wrapEvolutionSQLiteWriteError("commit evolution signal ingestion", err)
	}
	return cloneEvolutionSignal(signal), cloneEvolutionRun(run), created, nil
}

func normalizeEvolutionSignalInput(input EvolutionSignalInput) (normalizedEvolutionSignalInput, string, string, EvolutionRunType, error) {
	normalized := normalizedEvolutionSignalInput{
		IdempotencyKey: input.IdempotencyKey,
		SignalType:     input.SignalType,
		SourceType:     input.SourceType,
		SourceID:       input.SourceID,
		PackageID:      input.PackageID,
		ReleaseID:      input.ReleaseID,
		Severity:       input.Severity,
		ObservedValue:  input.ObservedValue,
		BaselineValue:  input.BaselineValue,
		EvidenceRefs:   append([]string{}, input.EvidenceRefs...),
	}
	if input.ObservedAt.IsZero() {
		return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("observed_at is required")
	}
	normalized.ObservedAt = input.ObservedAt.UTC().Format(time.RFC3339Nano)
	sort.Strings(normalized.EvidenceRefs)
	for index := 1; index < len(normalized.EvidenceRefs); index++ {
		if normalized.EvidenceRefs[index] == normalized.EvidenceRefs[index-1] {
			return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("evidence_refs must not contain duplicates")
		}
	}
	if err := validateEvolutionReference("idempotency_key", normalized.IdempotencyKey); err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", err
	}
	if !isEvolutionOpaqueID(normalized.IdempotencyKey) {
		return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("idempotency_key must be a canonical UUID or sha256 identity")
	}
	if !isAllowedEvolutionSignalType(normalized.SignalType) {
		return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("unsupported evolution signal_type %q", normalized.SignalType)
	}
	if !isAllowedEvolutionSignalSourceType(normalized.SourceType) {
		return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("unsupported evolution source_type %q", normalized.SourceType)
	}
	if !isAllowedEvolutionSignalSeverity(normalized.Severity) {
		return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("unsupported evolution severity %q", normalized.Severity)
	}
	if err := validateEvolutionIdentity("source_id", normalized.SourceID); err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", err
	}
	if normalized.PackageID != "" {
		if err := validateEvolutionPackageID(normalized.PackageID); err != nil {
			return normalizedEvolutionSignalInput{}, "", "", "", err
		}
	}
	if normalized.ReleaseID != "" && !isEvolutionKnowledgeReleaseID(normalized.ReleaseID) {
		return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("release_id must use release-<64 lowercase hex> format")
	}
	if err := validateEvolutionSignalSourceID(normalized.SourceType, normalized.SourceID, normalized.ReleaseID); err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", err
	}
	if err := validateEvolutionNumber("observed_value", normalized.ObservedValue); err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", err
	}
	if err := validateEvolutionNumber("baseline_value", normalized.BaselineValue); err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", err
	}
	if err := validateEvolutionSignalEvidenceRefs(normalized); err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", err
	}

	runType := EvolutionRunAgentPolicy
	if isKnowledgeEvolutionSignalType(normalized.SignalType) {
		runType = EvolutionRunKnowledgeRelease
		if normalized.ReleaseID == "" {
			return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("release_id is required for knowledge signal")
		}
	} else if normalized.PackageID == "" {
		return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("package_id is required for agent signal")
	}

	dedupPayload, err := json.Marshal(struct {
		SignalType string `json:"signal_type"`
		PackageID  string `json:"package_id"`
		ReleaseID  string `json:"release_id"`
	}{normalized.SignalType, normalized.PackageID, normalized.ReleaseID})
	if err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("encode evolution signal deduplication input: %w", err)
	}
	dedupDigest := sha256.Sum256(dedupPayload)
	deduplicationKey := "signal:" + hex.EncodeToString(dedupDigest[:])
	probe := EvolutionSignal{
		SignalID: "validation-signal", SignalType: normalized.SignalType, SourceType: normalized.SourceType,
		SourceID: normalized.SourceID, PackageID: normalized.PackageID, ReleaseID: normalized.ReleaseID,
		Severity: normalized.Severity, ObservedValue: normalized.ObservedValue, BaselineValue: normalized.BaselineValue,
		DeduplicationKey: deduplicationKey, EvidenceRefs: normalized.EvidenceRefs, ObservedAt: normalized.ObservedAt,
	}
	if err := probe.Validate(); err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", fmt.Errorf("encode evolution signal input: %w", err)
	}
	fingerprintDigest := sha256.Sum256(payload)
	return normalized, hex.EncodeToString(fingerprintDigest[:]), deduplicationKey, runType, nil
}

func (s *EvolutionControlStore) createEvolutionRunForSignalTx(tx *sql.Tx, input normalizedEvolutionSignalInput, runType EvolutionRunType, signalID, timestamp string, now time.Time) (*EvolutionRun, error) {
	runID, err := newEvolutionStoreID("run")
	if err != nil {
		return nil, err
	}
	eventID, err := newEvolutionStoreID("event")
	if err != nil {
		return nil, err
	}
	priority, err := evolutionSignalPriority(input, now, now)
	if err != nil {
		return nil, err
	}
	run := &EvolutionRun{
		RunID: runID, Attempt: 1, RunType: runType, PackageID: input.PackageID,
		RiskLevel: input.Severity, PriorityScore: priority, Status: EvolutionDetected,
		TriggerSignalIDs: []string{signalID}, CreatedAt: timestamp, UpdatedAt: timestamp,
	}
	if input.PackageID != "" {
		run.BaselinePackageVersion = evolutionUnresolvedBaseline
	}
	if input.ReleaseID != "" {
		run.BaselineReleaseIDs = []string{input.ReleaseID}
	} else {
		run.BaselineReleaseIDs = []string{}
	}
	if err := run.Validate(); err != nil {
		return nil, fmt.Errorf("validate signal evolution run: %w", err)
	}
	runInput := EvolutionRunInput{
		IdempotencyKey: "signal-run:" + run.RunID, RunType: run.RunType, PackageID: run.PackageID,
		BaselinePackageVersion: run.BaselinePackageVersion, BaselineReleaseIDs: run.BaselineReleaseIDs,
		RiskLevel: run.RiskLevel, PriorityScore: run.PriorityScore, TriggerSignalIDs: run.TriggerSignalIDs,
		Actor: "control-plane", Code: evolutionSignalReasonIngested,
	}
	_, inputHash, err := normalizeEvolutionRunInput(runInput)
	if err != nil {
		return nil, err
	}
	event := EvolutionEvent{
		EventID: eventID, RunID: run.RunID, EventType: "created", Actor: "control-plane",
		ToStatus: EvolutionDetected, Code: evolutionSignalReasonIngested,
		ArtifactRefs: []string{}, CreatedAt: timestamp,
	}
	if err := s.insertEvolutionRunTx(tx, runInput.IdempotencyKey, inputHash, run, event); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *EvolutionControlStore) aggregateEvolutionSignalTx(tx *sql.Tx, run *EvolutionRun, input normalizedEvolutionSignalInput, incomingType EvolutionRunType, signalID, timestamp string, now time.Time) (*EvolutionRun, error) {
	createdAt, err := parseEvolutionTimestamp("created_at", run.CreatedAt)
	if err != nil {
		return nil, err
	}
	priority, err := evolutionSignalPriority(input, createdAt, now)
	if err != nil {
		return nil, err
	}
	if priority > run.PriorityScore {
		run.PriorityScore = priority
	}
	if evolutionSeverityRank(input.Severity) > evolutionSeverityRank(run.RiskLevel) {
		run.RiskLevel = input.Severity
	}
	if incomingType != run.RunType {
		run.RunType = EvolutionRunCombined
	}
	run.TriggerSignalIDs = appendUniqueEvolutionReference(run.TriggerSignalIDs, signalID)
	if input.ReleaseID != "" {
		run.BaselineReleaseIDs = appendUniqueEvolutionReference(run.BaselineReleaseIDs, input.ReleaseID)
	}
	run.UpdatedAt = timestamp
	if err := run.Validate(); err != nil {
		return nil, fmt.Errorf("validate aggregated evolution run: %w", err)
	}
	baselineReleasesJSON, err := json.Marshal(run.BaselineReleaseIDs)
	if err != nil {
		return nil, err
	}
	triggerSignalsJSON, err := json.Marshal(run.TriggerSignalIDs)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`
		UPDATE evolution_runs SET run_type = ?, baseline_release_ids_json = ?, risk_level = ?,
			priority_score = ?, trigger_signal_ids_json = ?, updated_at = ?, updated_at_unix_nano = ?
		WHERE run_id = ?
	`, run.RunType, string(baselineReleasesJSON), run.RiskLevel, run.PriorityScore,
		string(triggerSignalsJSON), run.UpdatedAt, now.UnixNano(), run.RunID); err != nil {
		return nil, fmt.Errorf("update aggregated evolution run: %w", err)
	}
	if err := insertEvolutionRunScopesTx(tx, run); err != nil {
		return nil, err
	}
	eventID, err := newEvolutionStoreID("event")
	if err != nil {
		return nil, err
	}
	event := EvolutionEvent{
		EventID: eventID, RunID: run.RunID, EventType: "signal_aggregated", Actor: "control-plane",
		Code: evolutionSignalReasonAggregated, Message: "signal aggregated",
		ArtifactRefs: []string{}, CreatedAt: timestamp,
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	if err := s.insertEventTx(tx, event); err != nil {
		return nil, err
	}
	return run, nil
}

func evolutionSignalPriority(input normalizedEvolutionSignalInput, waitingSince, now time.Time) (float64, error) {
	risk := float64(evolutionSeverityRank(input.Severity)) / 4
	impact := math.Min(1, math.Abs(input.ObservedValue-input.BaselineValue))
	benefit := math.Max(risk, impact)
	return CalculateEvolutionPriority(EvolutionPriorityInput{
		Risk: risk, Impact: impact, ExpectedBenefit: benefit, WaitingSince: waitingSince, Now: now,
	})
}

func findEvolutionRunForAggregationTx(tx *sql.Tx, input normalizedEvolutionSignalInput, runType EvolutionRunType, now time.Time) (*EvolutionRun, error) {
	cutoff := now.Add(-EvolutionSignalCooldown).UnixNano()
	switch runType {
	case EvolutionRunAgentPolicy:
		if input.PackageID == "" {
			return nil, nil
		}
		return loadEvolutionRunByScopeTx(tx, "package", input.PackageID, cutoff, now.UnixNano(), "agent", input.PackageID)
	case EvolutionRunKnowledgeRelease:
		if input.ReleaseID != "" {
			run, err := loadEvolutionRunByScopeTx(tx, "release", input.ReleaseID, cutoff, now.UnixNano(), "knowledge", input.PackageID)
			if err != nil || run != nil {
				return run, err
			}
		}
		if input.PackageID != "" {
			// A new Release can combine only with a pure Agent run carrying the same Package scope.
			return loadEvolutionRunByScopeTx(tx, "package", input.PackageID, cutoff, now.UnixNano(), "pure_agent", input.PackageID)
		}
	}
	return nil, nil
}

const evolutionScopedRunSelect = `
	SELECT r.run_id, r.attempt, r.retry_of_run_id, r.run_type, r.package_id,
		r.baseline_package_version, r.baseline_release_ids_json, r.risk_level,
		r.priority_score, r.status, r.trigger_signal_ids_json, r.current_candidate_id,
		r.failure_code, r.failure_message, r.created_at, r.updated_at
	FROM evolution_run_scopes AS scope
	JOIN evolution_runs AS r ON r.run_id = scope.run_id`

func buildEvolutionRunScopeQuery(scopeType, scopeID string, cutoffNS, nowNS int64, mode, packageID string) (string, []any, error) {
	base := evolutionScopedRunSelect + `
		WHERE scope.scope_type = ? AND scope.scope_id = ?
			AND r.updated_at_unix_nano BETWEEN ? AND ?
			AND r.status NOT IN (?, ?, ?, ?, ?, ?)`
	args := []any{scopeType, scopeID, cutoffNS, nowNS,
		EvolutionCompleted, EvolutionBlocked, EvolutionRejected,
		EvolutionFailed, EvolutionSuperseded, EvolutionRolledBack}
	switch mode {
	case "agent":
		base += ` AND r.run_type IN (?, ?, ?)
			ORDER BY CASE r.run_type WHEN ? THEN 0 WHEN ? THEN 1 ELSE 2 END,
				r.updated_at_unix_nano DESC, r.run_id ASC LIMIT 1`
		args = append(args, EvolutionRunAgentPolicy, EvolutionRunCombined, EvolutionRunKnowledgeRelease,
			EvolutionRunAgentPolicy, EvolutionRunCombined)
	case "knowledge":
		base += ` AND r.run_type IN (?, ?)
			AND (? = '' OR r.package_id = '' OR r.package_id = ?)
			ORDER BY r.updated_at_unix_nano DESC, r.run_id ASC LIMIT 1`
		args = append(args, EvolutionRunKnowledgeRelease, EvolutionRunCombined, packageID, packageID)
	case "pure_agent":
		base += ` AND r.run_type = ?
			ORDER BY r.updated_at_unix_nano DESC, r.run_id ASC LIMIT 1`
		args = append(args, EvolutionRunAgentPolicy)
	default:
		return "", nil, fmt.Errorf("unknown evolution scope query mode %q", mode)
	}
	return base, args, nil
}

func loadEvolutionRunByScopeTx(tx *sql.Tx, scopeType, scopeID string, cutoffNS, nowNS int64, mode, packageID string) (*EvolutionRun, error) {
	query, args, err := buildEvolutionRunScopeQuery(scopeType, scopeID, cutoffNS, nowNS, mode, packageID)
	if err != nil {
		return nil, err
	}
	run, err := scanEvolutionRun(tx.QueryRow(query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load evolution aggregation scope: %w", err)
	}
	return run, nil
}

func insertEvolutionSignalTx(tx *sql.Tx, idempotencyKey string, signal *EvolutionSignal, timestamp string) error {
	evidenceJSON, err := json.Marshal(signal.EvidenceRefs)
	if err != nil {
		return fmt.Errorf("encode evolution signal evidence refs: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_signals (
			signal_id, idempotency_key, signal_type, source_type, source_id, package_id,
			release_id, severity, observed_value, baseline_value, deduplication_key,
			evidence_refs_json, observed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, signal.SignalID, idempotencyKey, signal.SignalType, signal.SourceType, signal.SourceID,
		signal.PackageID, signal.ReleaseID, signal.Severity, signal.ObservedValue, signal.BaselineValue,
		signal.DeduplicationKey, string(evidenceJSON), signal.ObservedAt, timestamp, timestamp); err != nil {
		return fmt.Errorf("insert evolution signal: %w", err)
	}
	return nil
}

const evolutionSignalSelect = `
	SELECT signal_id, signal_type, source_type, source_id, package_id, release_id,
		severity, observed_value, baseline_value, deduplication_key, evidence_refs_json, observed_at
	FROM evolution_signals`

func loadEvolutionSignalByDeduplicationKey(tx *sql.Tx, key string) (*EvolutionSignal, error) {
	return scanEvolutionSignal(tx.QueryRow(evolutionSignalSelect+` WHERE deduplication_key = ?`, key))
}

func loadEvolutionSignalByID(tx *sql.Tx, signalID string) (*EvolutionSignal, error) {
	return scanEvolutionSignal(tx.QueryRow(evolutionSignalSelect+` WHERE signal_id = ?`, signalID))
}

func scanEvolutionSignal(scanner evolutionRowScanner) (*EvolutionSignal, error) {
	var signal EvolutionSignal
	var evidenceJSON string
	if err := scanner.Scan(
		&signal.SignalID, &signal.SignalType, &signal.SourceType, &signal.SourceID,
		&signal.PackageID, &signal.ReleaseID, &signal.Severity, &signal.ObservedValue,
		&signal.BaselineValue, &signal.DeduplicationKey, &evidenceJSON, &signal.ObservedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &signal.EvidenceRefs); err != nil {
		return nil, fmt.Errorf("decode evolution signal evidence refs: %w", err)
	}
	if signal.EvidenceRefs == nil {
		signal.EvidenceRefs = []string{}
	}
	if err := signal.Validate(); err != nil {
		return nil, fmt.Errorf("invalid stored evolution signal: %w", err)
	}
	return &signal, nil
}

func insertEvolutionSignalObservationTx(tx *sql.Tx, input normalizedEvolutionSignalInput, requestKeyHash, inputHash, signalID, runID, timestamp string) error {
	observationID, err := newEvolutionStoreID("observation")
	if err != nil {
		return err
	}
	evidenceJSON, err := json.Marshal(input.EvidenceRefs)
	if err != nil {
		return fmt.Errorf("encode evolution signal observation evidence: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_signal_observations (
			observation_id, request_key_hash, input_hash, signal_id, run_id,
			payload_fidelity, signal_type, source_type, source_id, package_id, release_id, severity,
			observed_value, baseline_value, evidence_refs_json, observed_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, observationID, requestKeyHash, inputHash, signalID, runID, EvolutionObservationPayloadComplete, input.SignalType,
		input.SourceType, input.SourceID, input.PackageID, input.ReleaseID, input.Severity,
		input.ObservedValue, input.BaselineValue, string(evidenceJSON), input.ObservedAt, timestamp); err != nil {
		return fmt.Errorf("insert evolution signal observation: %w", err)
	}
	return nil
}

func loadEvolutionSignalObservationByRequestHashTx(tx *sql.Tx, requestKeyHash string) (*EvolutionSignalObservation, error) {
	var observation EvolutionSignalObservation
	var signalType, sourceType, sourceID, packageID, releaseID, severity sql.NullString
	var observedValue, baselineValue sql.NullFloat64
	var evidenceJSON, observedAt sql.NullString
	if err := tx.QueryRow(`
		SELECT observation_id, request_key_hash, input_hash, signal_id, run_id,
			payload_fidelity, signal_type, source_type, source_id, package_id, release_id, severity,
			observed_value, baseline_value, evidence_refs_json, observed_at, created_at
		FROM evolution_signal_observations WHERE request_key_hash = ?
	`, requestKeyHash).Scan(
		&observation.ObservationID, &observation.RequestKeyHash, &observation.InputHash,
		&observation.SignalID, &observation.RunID, &observation.PayloadFidelity,
		&signalType, &sourceType, &sourceID, &packageID, &releaseID, &severity,
		&observedValue, &baselineValue, &evidenceJSON, &observedAt,
		&observation.CreatedAt,
	); err != nil {
		return nil, err
	}
	switch observation.PayloadFidelity {
	case EvolutionObservationPayloadComplete:
		if !signalType.Valid || !sourceType.Valid || !sourceID.Valid || !packageID.Valid || !releaseID.Valid || !severity.Valid ||
			!observedValue.Valid || !baselineValue.Valid || !evidenceJSON.Valid || !observedAt.Valid {
			return nil, fmt.Errorf("complete evolution signal observation %q has null payload fields", observation.ObservationID)
		}
		observation.SignalType = &signalType.String
		observation.SourceType = &sourceType.String
		observation.SourceID = &sourceID.String
		observation.PackageID = &packageID.String
		observation.ReleaseID = &releaseID.String
		observation.Severity = &severity.String
		observation.ObservedValue = &observedValue.Float64
		observation.BaselineValue = &baselineValue.Float64
		observation.ObservedAt = &observedAt.String
		if err := json.Unmarshal([]byte(evidenceJSON.String), &observation.EvidenceRefs); err != nil {
			return nil, fmt.Errorf("decode evolution signal observation evidence: %w", err)
		}
		if observation.EvidenceRefs == nil {
			observation.EvidenceRefs = []string{}
		}
	case EvolutionObservationPayloadLegacyHashOnly:
		if signalType.Valid || sourceType.Valid || sourceID.Valid || packageID.Valid || releaseID.Valid || severity.Valid ||
			observedValue.Valid || baselineValue.Valid || evidenceJSON.Valid || observedAt.Valid {
			return nil, fmt.Errorf("legacy hash-only evolution signal observation %q contains fabricated payload", observation.ObservationID)
		}
	default:
		return nil, fmt.Errorf("evolution signal observation %q has unknown payload fidelity %q", observation.ObservationID, observation.PayloadFidelity)
	}
	return &observation, nil
}

func BuildEvolutionOverview(records []AgentPackageRecord, runs []EvolutionRun) (*EvolutionOverview, error) {
	overview := &EvolutionOverview{OpenRuns: []EvolutionRun{}, AgentFleet: []EvolutionAgentFleetProjection{}}
	openRuns := make([]EvolutionRun, 0, len(runs))
	for index := range runs {
		run := cloneEvolutionRun(&runs[index])
		if err := run.Validate(); err != nil {
			return nil, fmt.Errorf("invalid evolution run %q: %w", run.RunID, err)
		}
		switch run.Status {
		case EvolutionAwaitingApproval:
			overview.AwaitingApproval++
		case EvolutionBlocked:
			overview.Blocked++
		}
		if !isTerminalEvolutionRunStatus(run.Status) {
			openRuns = append(openRuns, *run)
		}
	}
	if err := sortEvolutionRuns(openRuns); err != nil {
		return nil, err
	}
	overview.OpenRuns = openRuns
	overview.TotalOpenRuns = len(openRuns)

	grouped := make(map[string][]AgentPackageRecord)
	for _, source := range records {
		record := cloneAgentPackageRecord(source)
		if err := validateEvolutionIdentity("package_id", record.PackageID); err != nil {
			return nil, err
		}
		if record.PublishedAt == "" && (record.LifecycleState == AgentPackagePublished || record.LifecycleState == AgentPackageSuperseded) {
			return nil, fmt.Errorf("invalid agent package %q version %q: published_at is required for %s lifecycle", record.PackageID, record.Version, record.LifecycleState)
		}
		if record.PublishedAt != "" {
			if _, err := parseEvolutionTimestamp("published_at", record.PublishedAt); err != nil {
				return nil, fmt.Errorf("invalid agent package %q version %q: %w", record.PackageID, record.Version, err)
			}
		}
		grouped[record.PackageID] = append(grouped[record.PackageID], record)
	}
	packageIDs := make([]string, 0, len(grouped))
	for packageID := range grouped {
		packageIDs = append(packageIDs, packageID)
	}
	sort.Strings(packageIDs)
	for _, packageID := range packageIDs {
		fleet := EvolutionAgentFleetProjection{PackageID: packageID, History: []AgentPackageRecord{}, OpenRuns: []EvolutionRun{}}
		packageRecords := append([]AgentPackageRecord{}, grouped[packageID]...)
		currentIndex := -1
		for index := range packageRecords {
			if packageRecords[index].LifecycleState != AgentPackagePublished {
				continue
			}
			if currentIndex == -1 || newerAgentPackageRecord(packageRecords[index], packageRecords[currentIndex]) {
				currentIndex = index
			}
		}
		if currentIndex >= 0 {
			current := cloneAgentPackageRecord(packageRecords[currentIndex])
			fleet.Current = &current
		}
		for index, record := range packageRecords {
			if index != currentIndex && record.LifecycleState == AgentPackageSuperseded {
				fleet.History = append(fleet.History, cloneAgentPackageRecord(record))
			}
		}
		sort.SliceStable(fleet.History, func(i, j int) bool {
			left, _ := parseOptionalEvolutionTimestamp(fleet.History[i].PublishedAt)
			right, _ := parseOptionalEvolutionTimestamp(fleet.History[j].PublishedAt)
			if !left.Equal(right) {
				return left.After(right)
			}
			return fleet.History[i].Version > fleet.History[j].Version
		})
		for _, run := range openRuns {
			if run.PackageID == packageID {
				fleet.OpenRuns = append(fleet.OpenRuns, run)
			}
		}
		overview.AgentFleet = append(overview.AgentFleet, fleet)
	}
	return overview, nil
}

func (s *EvolutionControlStore) EvolutionOverview(records []AgentPackageRecord) (*EvolutionOverview, error) {
	runs, err := s.listAllEvolutionRuns()
	if err != nil {
		return nil, err
	}
	return BuildEvolutionOverview(records, runs)
}

func (s *EvolutionControlStore) ListEvolutionRuns(after string, limit int) (*EvolutionRunPage, error) {
	limit, err := normalizeEvolutionRunLimit(limit)
	if err != nil {
		return nil, err
	}
	var cursor evolutionRunCursor
	if after != "" {
		cursor, err = decodeEvolutionRunCursor(after)
		if err != nil {
			return nil, err
		}
		var exists int
		err = s.db.QueryRow(`SELECT 1 FROM evolution_runs WHERE run_id = ? AND created_at_unix_nano = ?`, cursor.RunID, cursor.CreatedAtUnixNano).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEvolutionRunCursorNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("validate evolution run cursor: %w", err)
		}
	}
	query := evolutionRunSelect
	args := make([]any, 0, 4)
	if after != "" {
		query += ` WHERE created_at_unix_nano < ? OR (created_at_unix_nano = ? AND run_id > ?)`
		args = append(args, cursor.CreatedAtUnixNano, cursor.CreatedAtUnixNano, cursor.RunID)
	}
	query += ` ORDER BY created_at_unix_nano DESC, run_id ASC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list evolution runs page: %w", err)
	}
	defer rows.Close()
	runs := make([]EvolutionRun, 0, limit+1)
	for rows.Next() {
		run, err := scanEvolutionRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate evolution runs page: %w", err)
	}
	page := &EvolutionRunPage{Runs: runs}
	if len(page.Runs) > limit {
		page.Runs = page.Runs[:limit]
		last := page.Runs[len(page.Runs)-1]
		createdAt, err := parseEvolutionTimestamp("created_at", last.CreatedAt)
		if err != nil {
			return nil, err
		}
		page.NextCursor, err = encodeEvolutionRunCursor(createdAt.UnixNano(), last.RunID)
		if err != nil {
			return nil, err
		}
	}
	return page, nil
}

func (s *EvolutionControlStore) listAllEvolutionRuns() ([]EvolutionRun, error) {
	rows, err := s.db.Query(evolutionRunSelect)
	if err != nil {
		return nil, fmt.Errorf("list evolution runs: %w", err)
	}
	defer rows.Close()
	runs := make([]EvolutionRun, 0)
	for rows.Next() {
		run, err := scanEvolutionRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate evolution runs: %w", err)
	}
	if err := sortEvolutionRuns(runs); err != nil {
		return nil, err
	}
	return runs, nil
}

func PaginateEvolutionRuns(runs []EvolutionRun, after string, limit int) (*EvolutionRunPage, error) {
	limit, err := normalizeEvolutionRunLimit(limit)
	if err != nil {
		return nil, err
	}
	ordered := make([]EvolutionRun, len(runs))
	for index := range runs {
		ordered[index] = *cloneEvolutionRun(&runs[index])
	}
	if err := sortEvolutionRunsByCreated(ordered); err != nil {
		return nil, err
	}
	start := 0
	if after != "" {
		cursor, err := decodeEvolutionRunCursor(after)
		if err != nil {
			return nil, err
		}
		start = -1
		for index := range ordered {
			createdAt, _ := parseEvolutionTimestamp("created_at", ordered[index].CreatedAt)
			if ordered[index].RunID == cursor.RunID && createdAt.UnixNano() == cursor.CreatedAtUnixNano {
				start = index + 1
				break
			}
		}
		if start == -1 {
			return nil, ErrEvolutionRunCursorNotFound
		}
	}
	end := start + limit
	if end > len(ordered) {
		end = len(ordered)
	}
	page := &EvolutionRunPage{Runs: append([]EvolutionRun{}, ordered[start:end]...)}
	if end < len(ordered) && end > start {
		last := ordered[end-1]
		createdAt, _ := parseEvolutionTimestamp("created_at", last.CreatedAt)
		page.NextCursor, err = encodeEvolutionRunCursor(createdAt.UnixNano(), last.RunID)
		if err != nil {
			return nil, err
		}
	}
	return page, nil
}

func normalizeEvolutionRunLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, fmt.Errorf("run limit must not be negative")
	}
	if limit == 0 {
		return evolutionRunPageDefaultLimit, nil
	}
	if limit > evolutionRunPageMaxLimit {
		return 0, fmt.Errorf("run limit exceeds %d", evolutionRunPageMaxLimit)
	}
	return limit, nil
}

func encodeEvolutionRunCursor(createdAtUnixNano int64, runID string) (string, error) {
	if err := validateEvolutionIdentity("run_id", runID); err != nil {
		return "", err
	}
	payload, err := json.Marshal(evolutionRunCursor{CreatedAtUnixNano: createdAtUnixNano, RunID: runID})
	if err != nil {
		return "", fmt.Errorf("encode evolution run cursor: %w", err)
	}
	if len(payload) > evolutionRunCursorMaxPayload {
		return "", fmt.Errorf("evolution run cursor payload exceeds %d bytes", evolutionRunCursorMaxPayload)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if len(encoded) > evolutionRunCursorMaxInputBytes {
		return "", fmt.Errorf("evolution run cursor input exceeds %d bytes", evolutionRunCursorMaxInputBytes)
	}
	return encoded, nil
}

func decodeEvolutionRunCursor(value string) (evolutionRunCursor, error) {
	if len(value) > evolutionRunCursorMaxInputBytes {
		return evolutionRunCursor{}, fmt.Errorf("evolution run cursor input exceeds %d bytes", evolutionRunCursorMaxInputBytes)
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return evolutionRunCursor{}, fmt.Errorf("decode evolution run cursor: %w", err)
	}
	if len(payload) > evolutionRunCursorMaxPayload {
		return evolutionRunCursor{}, fmt.Errorf("evolution run cursor payload exceeds %d bytes", evolutionRunCursorMaxPayload)
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var cursor evolutionRunCursor
	if err := decoder.Decode(&cursor); err != nil {
		return evolutionRunCursor{}, fmt.Errorf("decode evolution run cursor: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return evolutionRunCursor{}, fmt.Errorf("decode evolution run cursor: trailing data")
	}
	if err := validateEvolutionIdentity("run_id", cursor.RunID); err != nil {
		return evolutionRunCursor{}, err
	}
	return cursor, nil
}

func sortEvolutionRuns(runs []EvolutionRun) error {
	times, err := evolutionRunCreatedTimes(runs)
	if err != nil {
		return err
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].PriorityScore != runs[j].PriorityScore {
			return runs[i].PriorityScore > runs[j].PriorityScore
		}
		if !times[runs[i].RunID].Equal(times[runs[j].RunID]) {
			return times[runs[i].RunID].Before(times[runs[j].RunID])
		}
		return runs[i].RunID < runs[j].RunID
	})
	return nil
}

func sortEvolutionRunsByCreated(runs []EvolutionRun) error {
	times, err := evolutionRunCreatedTimes(runs)
	if err != nil {
		return err
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if !times[runs[i].RunID].Equal(times[runs[j].RunID]) {
			return times[runs[i].RunID].After(times[runs[j].RunID])
		}
		return runs[i].RunID < runs[j].RunID
	})
	return nil
}

func evolutionRunCreatedTimes(runs []EvolutionRun) (map[string]time.Time, error) {
	times := make(map[string]time.Time, len(runs))
	for index := range runs {
		createdAt, err := parseEvolutionTimestamp("created_at", runs[index].CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("invalid evolution run %q: %w", runs[index].RunID, err)
		}
		times[runs[index].RunID] = createdAt
	}
	return times, nil
}

func newerAgentPackageRecord(candidate, current AgentPackageRecord) bool {
	candidateTime, _ := parseOptionalEvolutionTimestamp(candidate.PublishedAt)
	currentTime, _ := parseOptionalEvolutionTimestamp(current.PublishedAt)
	if !candidateTime.Equal(currentTime) {
		return candidateTime.After(currentTime)
	}
	return candidate.Version > current.Version
}

func parseOptionalEvolutionTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseEvolutionTimestamp("published_at", value)
}

func cloneAgentPackageRecord(record AgentPackageRecord) AgentPackageRecord {
	clone := record
	if record.Runtime != nil {
		runtime := *record.Runtime
		clone.Runtime = &runtime
	}
	return clone
}

func isTerminalEvolutionRunStatus(status EvolutionRunStatus) bool {
	switch status {
	case EvolutionCompleted, EvolutionBlocked, EvolutionRejected, EvolutionFailed, EvolutionSuperseded, EvolutionRolledBack:
		return true
	default:
		return false
	}
}

func isAllowedEvolutionSignalType(signalType string) bool {
	switch signalType {
	case EvolutionSignalTaskCompletionDrop, EvolutionSignalAnswerFailureRate,
		EvolutionSignalUserFeedbackIncorrect, EvolutionSignalToolFailure,
		EvolutionSignalLatencyRegression, EvolutionSignalCostRegression,
		EvolutionSignalModelCapabilityChange, EvolutionSignalManualPolicyImprovement,
		EvolutionSignalRegressionFailure, EvolutionSignalMissingEvidence,
		EvolutionSignalSourceAdded, EvolutionSignalReleaseStale,
		EvolutionSignalEvidenceConflict, EvolutionSignalCitationUnavailable,
		EvolutionSignalKnowledgeFeedbackError, EvolutionSignalKnowledgeCoverageGap,
		EvolutionSignalReverificationFailed, EvolutionSignalCandidateReleaseAvailable:
		return true
	default:
		return false
	}
}

func isKnowledgeEvolutionSignalType(signalType string) bool {
	switch signalType {
	case EvolutionSignalSourceAdded, EvolutionSignalReleaseStale, EvolutionSignalEvidenceConflict,
		EvolutionSignalCitationUnavailable, EvolutionSignalKnowledgeFeedbackError,
		EvolutionSignalKnowledgeCoverageGap, EvolutionSignalReverificationFailed,
		EvolutionSignalCandidateReleaseAvailable:
		return true
	default:
		return false
	}
}

func isAllowedEvolutionSignalSourceType(sourceType string) bool {
	switch sourceType {
	case EvolutionSignalSourceRuntimeMetric, EvolutionSignalSourceEvaluation,
		EvolutionSignalSourceUserFeedback, EvolutionSignalSourceOperator,
		EvolutionSignalSourceKnowledgeStore, EvolutionSignalSourceObservation,
		EvolutionSignalSourceReverification:
		return true
	default:
		return false
	}
}

func isAllowedEvolutionSignalSeverity(severity string) bool {
	return evolutionSeverityRank(severity) > 0
}

func evolutionSeverityRank(severity string) int {
	switch severity {
	case EvolutionSignalSeverityLow:
		return 1
	case EvolutionSignalSeverityMedium:
		return 2
	case EvolutionSignalSeverityHigh:
		return 3
	case EvolutionSignalSeverityCritical:
		return 4
	default:
		return 0
	}
}

func validateEvolutionSignalSourceID(sourceType, sourceID, releaseID string) error {
	switch sourceType {
	case EvolutionSignalSourceRuntimeMetric:
		if !isAllowedEvolutionMetricCode(sourceID) {
			return fmt.Errorf("source_id is not an allowed metric code")
		}
	case EvolutionSignalSourceKnowledgeStore:
		if releaseID == "" || sourceID != releaseID {
			return fmt.Errorf("knowledge_store source_id must equal release_id")
		}
	case EvolutionSignalSourceEvaluation, EvolutionSignalSourceUserFeedback,
		EvolutionSignalSourceOperator, EvolutionSignalSourceObservation,
		EvolutionSignalSourceReverification:
		if !isEvolutionOpaqueID(sourceID) {
			return fmt.Errorf("source_id must be a canonical UUID or sha256 identity")
		}
	default:
		return fmt.Errorf("unsupported evolution source_type %q", sourceType)
	}
	return nil
}

func validateEvolutionSignalEvidenceRefs(input normalizedEvolutionSignalInput) error {
	refs := input.EvidenceRefs
	if err := validateEvolutionReferences("evidence_refs", refs, true); err != nil {
		return err
	}
	for index, ref := range refs {
		if strings.Contains(ref, "?") || strings.Contains(ref, "//") {
			return fmt.Errorf("evidence_refs[%d] must not contain paths or URL queries", index)
		}
		kind, identity, found := strings.Cut(ref, ":")
		if !found || identity == "" {
			return fmt.Errorf("evidence_refs[%d] has unsupported reference syntax", index)
		}
		switch kind {
		case "metric":
			if !isAllowedEvolutionMetricCode(identity) {
				return fmt.Errorf("evidence_refs[%d] is not an allowed metric", index)
			}
		case "evaluation", "feedback", "trace", "observation", "audit":
			if !isEvolutionOpaqueID(identity) {
				return fmt.Errorf("evidence_refs[%d] requires an opaque identity", index)
			}
		case "release":
			if input.ReleaseID == "" || !isEvolutionKnowledgeReleaseID(identity) || identity != input.ReleaseID {
				return fmt.Errorf("evidence_refs[%d] must match release_id", index)
			}
		default:
			return fmt.Errorf("evidence_refs[%d] has unsupported reference type", index)
		}
	}
	return nil
}

func isEvolutionOpaqueID(value string) bool {
	if strings.HasPrefix(value, "sha256:") {
		digest := strings.TrimPrefix(value, "sha256:")
		return len(digest) == sha256.Size*2 && isEvolutionLowerHex(digest)
	}
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	return isEvolutionLowerHex(strings.ReplaceAll(value, "-", ""))
}

func validateEvolutionPackageID(packageID string) error {
	if err := validateEvolutionIdentity("package_id", packageID); err != nil {
		return err
	}
	if !agentPackageIDPattern.MatchString(packageID) {
		return fmt.Errorf("package_id must contain only URL-safe letters, digits, dot, underscore, or hyphen")
	}
	return nil
}

func isEvolutionKnowledgeReleaseID(releaseID string) bool {
	const prefix = "release-"
	if !strings.HasPrefix(releaseID, prefix) {
		return false
	}
	digest := strings.TrimPrefix(releaseID, prefix)
	return len(digest) == sha256.Size*2 && isEvolutionLowerHex(digest)
}

func isEvolutionLowerHex(value string) bool {
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return value != ""
}

func isAllowedEvolutionMetricCode(code string) bool {
	switch code {
	case "task_completion_rate", "answer_failure_rate", "tool_failure_rate",
		"latency_p95_ms", "cost_per_task", "citation_integrity_rate",
		"release_age_days", "regression_pass_rate":
		return true
	default:
		return false
	}
}

func appendUniqueEvolutionReference(values []string, value string) []string {
	if containsEvolutionReference(values, value) {
		return values
	}
	return append(values, value)
}

func containsEvolutionReference(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func cloneEvolutionSignal(signal *EvolutionSignal) *EvolutionSignal {
	cloned := *signal
	cloned.EvidenceRefs = append([]string{}, signal.EvidenceRefs...)
	return &cloned
}

func evolutionSignalRequestKeyHash(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return "sha256:" + hex.EncodeToString(digest[:])
}
