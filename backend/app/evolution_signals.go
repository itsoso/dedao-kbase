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

	replayed, err := loadEvolutionSignalByIdempotencyKey(tx, normalized.IdempotencyKey)
	if err == nil {
		replayedInput, err := evolutionSignalInputFromStored(replayed, normalized.IdempotencyKey)
		if err != nil {
			return nil, nil, false, err
		}
		_, storedFingerprint, _, _, err := normalizeEvolutionSignalInput(replayedInput)
		if err != nil {
			return nil, nil, false, fmt.Errorf("normalize stored evolution signal: %w", err)
		}
		if storedFingerprint != fingerprint {
			return nil, nil, false, ErrEvolutionIdempotencyConflict
		}
		run, err := loadEarliestEvolutionRunForSignalTx(tx, replayed.SignalID)
		if err != nil {
			return nil, nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, false, wrapEvolutionSQLiteWriteError("commit evolution signal replay", err)
		}
		return cloneEvolutionSignal(replayed), cloneEvolutionRun(run), false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, fmt.Errorf("find evolution signal replay: %w", err)
	}
	replayed, replayedRun, storedFingerprint, err := loadEvolutionSignalReplayEventTx(tx, normalized.IdempotencyKey)
	if err == nil {
		if storedFingerprint != fingerprint {
			return nil, nil, false, ErrEvolutionIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, false, wrapEvolutionSQLiteWriteError("commit evolution signal replay", err)
		}
		return cloneEvolutionSignal(replayed), cloneEvolutionRun(replayedRun), false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, fmt.Errorf("find evolution signal event replay: %w", err)
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
		if err := insertEvolutionSignalTx(tx, normalized.IdempotencyKey, signal, timestamp); err != nil {
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
		run, err = s.createEvolutionRunForSignalTx(tx, normalized, runType, signal.SignalID, fingerprint, timestamp, now)
	} else {
		run, err = s.aggregateEvolutionSignalTx(tx, run, normalized, runType, signal.SignalID, fingerprint, timestamp, now)
	}
	if err != nil {
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
	if err := rejectSensitiveEvolutionSignalToken("idempotency_key", normalized.IdempotencyKey); err != nil {
		return normalizedEvolutionSignalInput{}, "", "", "", err
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
	for _, field := range []evolutionStringField{
		{name: "source_id", value: normalized.SourceID},
		{name: "package_id", value: normalized.PackageID},
		{name: "release_id", value: normalized.ReleaseID},
	} {
		if field.value != "" {
			if err := validateEvolutionIdentity(field.name, field.value); err != nil {
				return normalizedEvolutionSignalInput{}, "", "", "", err
			}
			if err := rejectSensitiveEvolutionSignalToken(field.name, field.value); err != nil {
				return normalizedEvolutionSignalInput{}, "", "", "", err
			}
		}
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

func (s *EvolutionControlStore) createEvolutionRunForSignalTx(tx *sql.Tx, input normalizedEvolutionSignalInput, runType EvolutionRunType, signalID, fingerprint, timestamp string, now time.Time) (*EvolutionRun, error) {
	runID, err := newEvolutionStoreID("run")
	if err != nil {
		return nil, err
	}
	eventID := evolutionSignalRequestEventID(input.IdempotencyKey)
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
		ArtifactRefs: evolutionSignalReplayRefs(fingerprint, signalID), CreatedAt: timestamp,
	}
	if err := s.insertEvolutionRunTx(tx, runInput.IdempotencyKey, inputHash, run, event); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *EvolutionControlStore) aggregateEvolutionSignalTx(tx *sql.Tx, run *EvolutionRun, input normalizedEvolutionSignalInput, incomingType EvolutionRunType, signalID, fingerprint, timestamp string, now time.Time) (*EvolutionRun, error) {
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
			priority_score = ?, trigger_signal_ids_json = ?, updated_at = ? WHERE run_id = ?
	`, run.RunType, string(baselineReleasesJSON), run.RiskLevel, run.PriorityScore, string(triggerSignalsJSON), run.UpdatedAt, run.RunID); err != nil {
		return nil, fmt.Errorf("update aggregated evolution run: %w", err)
	}
	eventID := evolutionSignalRequestEventID(input.IdempotencyKey)
	event := EvolutionEvent{
		EventID: eventID, RunID: run.RunID, EventType: "signal_aggregated", Actor: "control-plane",
		Code: evolutionSignalReasonAggregated, Message: "signal aggregated",
		ArtifactRefs: evolutionSignalReplayRefs(fingerprint, signalID), CreatedAt: timestamp,
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
	rows, err := tx.Query(evolutionRunSelect + ` ORDER BY updated_at DESC, run_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list evolution runs for signal aggregation: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanEvolutionRun(rows)
		if err != nil {
			return nil, err
		}
		if isTerminalEvolutionRunStatus(run.Status) || !evolutionSignalScopeMatchesRun(input, runType, run) {
			continue
		}
		updatedAt, err := parseEvolutionTimestamp("updated_at", run.UpdatedAt)
		if err != nil {
			return nil, err
		}
		age := now.Sub(updatedAt)
		if age >= 0 && age <= EvolutionSignalCooldown {
			return run, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate evolution runs for signal aggregation: %w", err)
	}
	return nil, nil
}

func evolutionSignalScopeMatchesRun(input normalizedEvolutionSignalInput, runType EvolutionRunType, run *EvolutionRun) bool {
	samePackage := input.PackageID != "" && run.PackageID != "" && input.PackageID == run.PackageID
	switch runType {
	case EvolutionRunAgentPolicy:
		switch run.RunType {
		case EvolutionRunAgentPolicy, EvolutionRunCombined:
			return samePackage
		case EvolutionRunKnowledgeRelease:
			// Cross-domain aggregation needs the explicit Package identity on both sides.
			return samePackage
		}
	case EvolutionRunKnowledgeRelease:
		if run.RunType == EvolutionRunAgentPolicy {
			// A knowledge signal can promote an Agent run only with an explicit Package relation.
			return samePackage
		}
		packageCompatible := input.PackageID == "" || run.PackageID == "" || samePackage
		return packageCompatible && containsEvolutionReference(run.BaselineReleaseIDs, input.ReleaseID)
	}
	return false
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

func loadEvolutionSignalByIdempotencyKey(tx *sql.Tx, key string) (*EvolutionSignal, error) {
	return scanEvolutionSignal(tx.QueryRow(evolutionSignalSelect+` WHERE idempotency_key = ?`, key))
}

func loadEvolutionSignalByDeduplicationKey(tx *sql.Tx, key string) (*EvolutionSignal, error) {
	return scanEvolutionSignal(tx.QueryRow(evolutionSignalSelect+` WHERE deduplication_key = ?`, key))
}

func loadEvolutionSignalByID(tx *sql.Tx, signalID string) (*EvolutionSignal, error) {
	return scanEvolutionSignal(tx.QueryRow(evolutionSignalSelect+` WHERE signal_id = ?`, signalID))
}

func loadEvolutionSignalReplayEventTx(tx *sql.Tx, idempotencyKey string) (*EvolutionSignal, *EvolutionRun, string, error) {
	event, err := scanEvolutionEvent(tx.QueryRow(`
		SELECT event_id, run_id, event_type, actor, from_status, to_status, code,
			message, artifact_refs_json, created_at
		FROM evolution_events
		WHERE event_id = ?
	`, evolutionSignalRequestEventID(idempotencyKey)))
	if err != nil {
		return nil, nil, "", err
	}
	fingerprint, signalID := "", ""
	for _, ref := range event.ArtifactRefs {
		switch {
		case strings.HasPrefix(ref, "input-sha256:"):
			fingerprint = strings.TrimPrefix(ref, "input-sha256:")
		case strings.HasPrefix(ref, "signal-id:"):
			signalID = strings.TrimPrefix(ref, "signal-id:")
		}
	}
	if fingerprint == "" || signalID == "" {
		return nil, nil, "", fmt.Errorf("evolution signal replay event %q is incomplete", event.EventID)
	}
	signal, err := loadEvolutionSignalByID(tx, signalID)
	if err != nil {
		return nil, nil, "", err
	}
	run, err := scanEvolutionRun(tx.QueryRow(evolutionRunSelect+` WHERE run_id = ?`, event.RunID))
	if err != nil {
		return nil, nil, "", err
	}
	if !containsEvolutionReference(run.TriggerSignalIDs, signalID) {
		return nil, nil, "", fmt.Errorf("evolution signal replay event %q does not match its run", event.EventID)
	}
	return signal, run, fingerprint, nil
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

func evolutionSignalInputFromStored(signal *EvolutionSignal, idempotencyKey string) (EvolutionSignalInput, error) {
	observedAt, err := parseEvolutionTimestamp("observed_at", signal.ObservedAt)
	if err != nil {
		return EvolutionSignalInput{}, err
	}
	return EvolutionSignalInput{
		IdempotencyKey: idempotencyKey, SignalType: signal.SignalType, SourceType: signal.SourceType,
		SourceID: signal.SourceID, PackageID: signal.PackageID, ReleaseID: signal.ReleaseID,
		Severity: signal.Severity, ObservedValue: signal.ObservedValue, BaselineValue: signal.BaselineValue,
		EvidenceRefs: append([]string{}, signal.EvidenceRefs...), ObservedAt: observedAt,
	}, nil
}

func loadEarliestEvolutionRunForSignalTx(tx *sql.Tx, signalID string) (*EvolutionRun, error) {
	rows, err := tx.Query(evolutionRunSelect + ` ORDER BY created_at ASC, run_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanEvolutionRun(rows)
		if err != nil {
			return nil, err
		}
		if containsEvolutionReference(run.TriggerSignalIDs, signalID) {
			return run, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("signal %q has no evolution run", signalID)
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
	sortEvolutionRuns(openRuns)
	overview.OpenRuns = openRuns
	overview.TotalOpenRuns = len(openRuns)

	grouped := make(map[string][]AgentPackageRecord)
	for _, record := range records {
		if err := validateEvolutionIdentity("package_id", record.PackageID); err != nil {
			return nil, err
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
			current := packageRecords[currentIndex]
			fleet.Current = &current
		}
		for index, record := range packageRecords {
			if index != currentIndex {
				fleet.History = append(fleet.History, record)
			}
		}
		sort.SliceStable(fleet.History, func(i, j int) bool {
			if fleet.History[i].PublishedAt != fleet.History[j].PublishedAt {
				return fleet.History[i].PublishedAt > fleet.History[j].PublishedAt
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
	runs, err := s.listAllEvolutionRuns()
	if err != nil {
		return nil, err
	}
	return PaginateEvolutionRuns(runs, after, limit)
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
	sortEvolutionRuns(runs)
	return runs, nil
}

func PaginateEvolutionRuns(runs []EvolutionRun, after string, limit int) (*EvolutionRunPage, error) {
	if after != "" {
		if err := validateEvolutionIdentity("after", after); err != nil {
			return nil, err
		}
	}
	if limit < 0 {
		return nil, fmt.Errorf("run limit must not be negative")
	}
	if limit == 0 {
		limit = evolutionRunPageDefaultLimit
	}
	if limit > evolutionRunPageMaxLimit {
		return nil, fmt.Errorf("run limit exceeds %d", evolutionRunPageMaxLimit)
	}
	ordered := make([]EvolutionRun, len(runs))
	for index := range runs {
		ordered[index] = *cloneEvolutionRun(&runs[index])
	}
	sortEvolutionRuns(ordered)
	start := 0
	if after != "" {
		start = -1
		for index := range ordered {
			if ordered[index].RunID == after {
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
		page.NextCursor = ordered[end-1].RunID
	}
	return page, nil
}

func sortEvolutionRuns(runs []EvolutionRun) {
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].PriorityScore != runs[j].PriorityScore {
			return runs[i].PriorityScore > runs[j].PriorityScore
		}
		if runs[i].CreatedAt != runs[j].CreatedAt {
			return runs[i].CreatedAt < runs[j].CreatedAt
		}
		return runs[i].RunID < runs[j].RunID
	})
}

func newerAgentPackageRecord(candidate, current AgentPackageRecord) bool {
	if candidate.PublishedAt != current.PublishedAt {
		return candidate.PublishedAt > current.PublishedAt
	}
	return candidate.Version > current.Version
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
		if err := rejectSensitiveEvolutionSignalToken(fmt.Sprintf("evidence_refs[%d]", index), ref); err != nil {
			return err
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
			if input.ReleaseID == "" || identity != input.ReleaseID {
				return fmt.Errorf("evidence_refs[%d] must match release_id", index)
			}
		default:
			return fmt.Errorf("evidence_refs[%d] has unsupported reference type", index)
		}
	}
	return nil
}

func rejectSensitiveEvolutionSignalToken(field, value string) error {
	lower := strings.ToLower(value)
	windowsAbsolute := len(value) >= 3 && value[1] == ':' && value[2] == '/' &&
		((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z'))
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~/") || windowsAbsolute ||
		strings.Contains(lower, "file:") || strings.Contains(lower, "://") {
		return fmt.Errorf("%s must not contain an absolute filesystem path", field)
	}
	var compactBuilder strings.Builder
	compactBuilder.Grow(len(lower))
	for _, character := range lower {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			compactBuilder.WriteRune(character)
		}
	}
	compact := compactBuilder.String()
	for _, marker := range []string{"apikey", "accesstoken", "token", "bearer", "authorization", "password", "passwd", "privatekey", "session", "cookie", "secret"} {
		if strings.Contains(compact, marker) {
			return fmt.Errorf("%s contains sensitive material", field)
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

func evolutionSignalReplayRefs(fingerprint, signalID string) []string {
	return []string{"input-sha256:" + fingerprint, "signal-id:" + signalID}
}

func evolutionSignalRequestEventID(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return "event-signal-" + hex.EncodeToString(digest[:])
}
