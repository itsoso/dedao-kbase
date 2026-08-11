package app

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type EvolutionRunStatus string
type EvolutionRunType string

const (
	EvolutionDetected         EvolutionRunStatus = "detected"
	EvolutionTriaged          EvolutionRunStatus = "triaged"
	EvolutionGenerating       EvolutionRunStatus = "generating"
	EvolutionEvaluating       EvolutionRunStatus = "evaluating"
	EvolutionAwaitingApproval EvolutionRunStatus = "awaiting_approval"
	EvolutionApproved         EvolutionRunStatus = "approved"
	EvolutionPublishing       EvolutionRunStatus = "publishing"
	EvolutionObserving        EvolutionRunStatus = "observing"
	EvolutionCompleted        EvolutionRunStatus = "completed"
	EvolutionBlocked          EvolutionRunStatus = "blocked"
	EvolutionRejected         EvolutionRunStatus = "rejected"
	EvolutionFailed           EvolutionRunStatus = "failed"
	EvolutionSuperseded       EvolutionRunStatus = "superseded"
	EvolutionRolledBack       EvolutionRunStatus = "rolled_back"

	EvolutionRunAgentPolicy      EvolutionRunType = "agent_policy"
	EvolutionRunKnowledgeRelease EvolutionRunType = "knowledge_release"
	EvolutionRunCombined         EvolutionRunType = "combined"

	EvolutionFailureMessageMaxRunes = 512
	EvolutionChangeSummaryMaxRunes  = 1_000
	EvolutionApprovalNoteMaxRunes   = 1_000
	EvolutionEventMessageMaxRunes   = 512
	EvolutionIdentityMaxRunes       = 256
	EvolutionCodeMaxRunes           = 64
	EvolutionReferenceMaxRunes      = 512
	EvolutionCollectionMaxItems     = 64
	EvolutionMetricMaxItems         = 64
)

type EvolutionSignal struct {
	SignalID         string   `json:"signal_id"`
	SignalType       string   `json:"signal_type"`
	SourceType       string   `json:"source_type"`
	SourceID         string   `json:"source_id"`
	PackageID        string   `json:"package_id"`
	ReleaseID        string   `json:"release_id"`
	Severity         string   `json:"severity"`
	ObservedValue    float64  `json:"observed_value"`
	BaselineValue    float64  `json:"baseline_value"`
	DeduplicationKey string   `json:"deduplication_key"`
	EvidenceRefs     []string `json:"evidence_refs"`
	ObservedAt       string   `json:"observed_at"`
}

type EvolutionRun struct {
	RunID                  string             `json:"run_id"`
	Attempt                int                `json:"attempt"`
	RetryOfRunID           string             `json:"retry_of_run_id"`
	RunType                EvolutionRunType   `json:"run_type"`
	PackageID              string             `json:"package_id"`
	BaselinePackageVersion string             `json:"baseline_package_version"`
	BaselineReleaseIDs     []string           `json:"baseline_release_ids"`
	RiskLevel              string             `json:"risk_level"`
	PriorityScore          float64            `json:"priority_score"`
	Status                 EvolutionRunStatus `json:"status"`
	TriggerSignalIDs       []string           `json:"trigger_signal_ids"`
	CurrentCandidateID     string             `json:"current_candidate_id"`
	FailureCode            string             `json:"failure_code"`
	FailureMessage         string             `json:"failure_message"`
	CreatedAt              string             `json:"created_at"`
	UpdatedAt              string             `json:"updated_at"`
}

type EvolutionCandidate struct {
	CandidateID      string `json:"candidate_id"`
	RunID            string `json:"run_id"`
	CandidateType    string `json:"candidate_type"`
	ContentHash      string `json:"content_hash"`
	ArtifactRef      string `json:"artifact_ref"`
	BaselineIdentity string `json:"baseline_identity"`
	ChangeSummary    string `json:"change_summary"`
	GeneratorVersion string `json:"generator_version"`
	CreatedAt        string `json:"created_at"`
}

type EvolutionScorecard struct {
	ScorecardID      string             `json:"scorecard_id"`
	CandidateID      string             `json:"candidate_id"`
	BaselineIdentity string             `json:"baseline_identity"`
	SuiteVersion     string             `json:"suite_version"`
	ScorerVersion    string             `json:"scorer_version"`
	HardGates        map[string]bool    `json:"hard_gates"`
	Metrics          map[string]float64 `json:"metrics"`
	WeightedScore    float64            `json:"weighted_score"`
	BaselineScore    float64            `json:"baseline_score"`
	Delta            float64            `json:"delta"`
	Decision         string             `json:"decision"`
	FailureCaseRefs  []string           `json:"failure_case_refs"`
}

type EvolutionApproval struct {
	ApprovalID           string `json:"approval_id"`
	RunID                string `json:"run_id"`
	CandidateID          string `json:"candidate_id"`
	CandidateContentHash string `json:"candidate_content_hash"`
	BaselineIdentity     string `json:"baseline_identity"`
	ScorecardID          string `json:"scorecard_id"`
	Decision             string `json:"decision"`
	ReasonCode           string `json:"reason_code"`
	Note                 string `json:"note"`
	ApprovedBy           string `json:"approved_by"`
	CreatedAt            string `json:"created_at"`
	ExpiresAt            string `json:"expires_at"`
}

type EvolutionObservation struct {
	ObservationID     string             `json:"observation_id"`
	RunID             string             `json:"run_id"`
	PublishedIdentity string             `json:"published_identity"`
	WindowStart       string             `json:"window_start"`
	WindowEnd         string             `json:"window_end"`
	Metrics           map[string]float64 `json:"metrics"`
	HardGateIncidents []string           `json:"hard_gate_incidents"`
	Outcome           string             `json:"outcome"`
	RollbackIdentity  string             `json:"rollback_identity"`
}

type EvolutionEvent struct {
	EventID      string             `json:"event_id"`
	RunID        string             `json:"run_id"`
	EventType    string             `json:"event_type"`
	Actor        string             `json:"actor"`
	FromStatus   EvolutionRunStatus `json:"from_status"`
	ToStatus     EvolutionRunStatus `json:"to_status"`
	Code         string             `json:"code"`
	Message      string             `json:"message"`
	ArtifactRefs []string           `json:"artifact_refs"`
	CreatedAt    string             `json:"created_at"`
}

func ValidateEvolutionTransition(from, to EvolutionRunStatus) error {
	if !isKnownEvolutionRunStatus(from) {
		return fmt.Errorf("unknown evolution run status %q", from)
	}
	if !isKnownEvolutionRunStatus(to) {
		return fmt.Errorf("unknown evolution run status %q", to)
	}

	allowed := false
	switch from {
	case EvolutionDetected:
		allowed = to == EvolutionTriaged || isEvolutionAbortStatus(to)
	case EvolutionTriaged:
		allowed = to == EvolutionGenerating || isEvolutionAbortStatus(to)
	case EvolutionGenerating:
		allowed = to == EvolutionEvaluating || isEvolutionAbortStatus(to)
	case EvolutionEvaluating:
		allowed = to == EvolutionAwaitingApproval || isEvolutionAbortStatus(to)
	case EvolutionAwaitingApproval:
		allowed = to == EvolutionApproved || to == EvolutionRejected || to == EvolutionBlocked || to == EvolutionSuperseded
	case EvolutionApproved:
		allowed = to == EvolutionPublishing || isEvolutionAbortStatus(to)
	case EvolutionPublishing:
		allowed = to == EvolutionObserving || isEvolutionAbortStatus(to) || to == EvolutionRolledBack
	case EvolutionObserving:
		allowed = to == EvolutionCompleted || isEvolutionAbortStatus(to) || to == EvolutionRolledBack
	}
	if !allowed {
		return fmt.Errorf("invalid evolution run transition %q -> %q", from, to)
	}
	return nil
}

func NewEvolutionRetry(original EvolutionRun, newRunID string, now time.Time) (EvolutionRun, error) {
	originalRunID := strings.TrimSpace(original.RunID)
	retryRunID := strings.TrimSpace(newRunID)
	if originalRunID == "" {
		return EvolutionRun{}, fmt.Errorf("original run_id is required")
	}
	if retryRunID == "" {
		return EvolutionRun{}, fmt.Errorf("new run_id is required")
	}
	if retryRunID == originalRunID {
		return EvolutionRun{}, fmt.Errorf("retry run_id must differ from original run_id")
	}
	if !isEvolutionRetryableStatus(original.Status) {
		return EvolutionRun{}, fmt.Errorf("evolution run status %q cannot be retried", original.Status)
	}
	if original.Attempt < 0 {
		return EvolutionRun{}, fmt.Errorf("attempt cannot be negative")
	}
	if original.Attempt == int(^uint(0)>>1) {
		return EvolutionRun{}, fmt.Errorf("attempt cannot be incremented")
	}
	if now.IsZero() {
		return EvolutionRun{}, fmt.Errorf("retry time is required")
	}

	normalizedOriginal := original
	normalizedOriginal.RunID = originalRunID
	previousAttempt := original.Attempt
	if previousAttempt == 0 {
		// Records created before attempt tracking represent their first attempt.
		previousAttempt = 1
		normalizedOriginal.Attempt = 1
	}
	if err := normalizedOriginal.Validate(); err != nil {
		return EvolutionRun{}, fmt.Errorf("original evolution run: %w", err)
	}
	timestamp := now.UTC().Format(time.RFC3339Nano)
	retry := EvolutionRun{
		RunID:                  retryRunID,
		Attempt:                previousAttempt + 1,
		RetryOfRunID:           originalRunID,
		RunType:                original.RunType,
		PackageID:              original.PackageID,
		BaselinePackageVersion: original.BaselinePackageVersion,
		BaselineReleaseIDs:     append([]string(nil), original.BaselineReleaseIDs...),
		RiskLevel:              original.RiskLevel,
		PriorityScore:          original.PriorityScore,
		Status:                 EvolutionDetected,
		TriggerSignalIDs:       append([]string(nil), original.TriggerSignalIDs...),
		CreatedAt:              timestamp,
		UpdatedAt:              timestamp,
	}
	if err := retry.Validate(); err != nil {
		return EvolutionRun{}, fmt.Errorf("retry evolution run: %w", err)
	}
	return retry, nil
}

func (signal EvolutionSignal) Validate() error {
	for _, field := range []evolutionStringField{
		{name: "source_id", value: signal.SourceID},
		{name: "package_id", value: signal.PackageID},
		{name: "release_id", value: signal.ReleaseID},
	} {
		if field.value != "" {
			if err := validateEvolutionIdentity(field.name, field.value); err != nil {
				return err
			}
		}
	}
	if err := validateEvolutionIdentity("signal_id", signal.SignalID); err != nil {
		return err
	}
	if signal.SourceID == "" && signal.PackageID == "" && signal.ReleaseID == "" {
		return fmt.Errorf("signal requires an affected identity")
	}
	if err := validateEvolutionCodeFields(
		evolutionStringField{name: "signal_type", value: signal.SignalType},
		evolutionStringField{name: "source_type", value: signal.SourceType},
		evolutionStringField{name: "severity", value: signal.Severity},
	); err != nil {
		return err
	}
	if err := validateEvolutionReference("deduplication_key", signal.DeduplicationKey); err != nil {
		return err
	}
	if err := validateEvolutionReferences("evidence_refs", signal.EvidenceRefs, true); err != nil {
		return err
	}
	if err := validateEvolutionNumber("observed_value", signal.ObservedValue); err != nil {
		return err
	}
	if err := validateEvolutionNumber("baseline_value", signal.BaselineValue); err != nil {
		return err
	}
	return validateEvolutionTimestamp("observed_at", signal.ObservedAt)
}

func (run EvolutionRun) Validate() error {
	if err := validateEvolutionIdentity("run_id", run.RunID); err != nil {
		return err
	}
	if run.Attempt < 1 {
		return fmt.Errorf("attempt must be at least 1")
	}
	if run.Attempt > 1 {
		if err := validateEvolutionIdentity("retry_of_run_id", run.RetryOfRunID); err != nil {
			return err
		}
		if run.RetryOfRunID == run.RunID {
			return fmt.Errorf("retry_of_run_id must differ from run_id")
		}
	} else if run.RetryOfRunID != "" {
		return fmt.Errorf("first attempt must not have retry_of_run_id")
	}
	if !isKnownEvolutionRunType(run.RunType) {
		return fmt.Errorf("unknown evolution run type %q", run.RunType)
	}
	if err := validateEvolutionRunScope(run); err != nil {
		return err
	}
	if err := validateEvolutionCode("risk_level", run.RiskLevel); err != nil {
		return err
	}
	if err := validateEvolutionNumber("priority_score", run.PriorityScore); err != nil {
		return err
	}
	if !isKnownEvolutionRunStatus(run.Status) {
		return fmt.Errorf("unknown evolution run status %q", run.Status)
	}
	if err := validateEvolutionReferences("trigger_signal_ids", run.TriggerSignalIDs, true); err != nil {
		return err
	}
	if run.CurrentCandidateID != "" {
		if err := validateEvolutionIdentity("current_candidate_id", run.CurrentCandidateID); err != nil {
			return err
		}
	}
	if run.FailureCode != "" {
		if err := validateEvolutionCode("failure_code", run.FailureCode); err != nil {
			return err
		}
	}
	if err := validateEvolutionText("failure_message", run.FailureMessage, EvolutionFailureMessageMaxRunes); err != nil {
		return err
	}
	createdAt, err := parseEvolutionTimestamp("created_at", run.CreatedAt)
	if err != nil {
		return err
	}
	updatedAt, err := parseEvolutionTimestamp("updated_at", run.UpdatedAt)
	if err != nil {
		return err
	}
	if updatedAt.Before(createdAt) {
		return fmt.Errorf("updated_at must not be before created_at")
	}
	return nil
}

func (candidate EvolutionCandidate) Validate() error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "candidate_id", value: candidate.CandidateID},
		evolutionStringField{name: "run_id", value: candidate.RunID},
		evolutionStringField{name: "generator_version", value: candidate.GeneratorVersion},
	); err != nil {
		return err
	}
	if err := validateEvolutionCode("candidate_type", candidate.CandidateType); err != nil {
		return err
	}
	if err := validateEvolutionReferenceFields(
		evolutionStringField{name: "content_hash", value: candidate.ContentHash},
		evolutionStringField{name: "artifact_ref", value: candidate.ArtifactRef},
		evolutionStringField{name: "baseline_identity", value: candidate.BaselineIdentity},
	); err != nil {
		return err
	}
	if strings.TrimSpace(candidate.ChangeSummary) == "" {
		return fmt.Errorf("change_summary is required")
	}
	if err := validateEvolutionText("change_summary", candidate.ChangeSummary, EvolutionChangeSummaryMaxRunes); err != nil {
		return err
	}
	return validateEvolutionTimestamp("created_at", candidate.CreatedAt)
}

func (scorecard EvolutionScorecard) Validate() error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "scorecard_id", value: scorecard.ScorecardID},
		evolutionStringField{name: "candidate_id", value: scorecard.CandidateID},
		evolutionStringField{name: "suite_version", value: scorecard.SuiteVersion},
		evolutionStringField{name: "scorer_version", value: scorecard.ScorerVersion},
	); err != nil {
		return err
	}
	if err := validateEvolutionReference("baseline_identity", scorecard.BaselineIdentity); err != nil {
		return err
	}
	if err := validateEvolutionCode("decision", scorecard.Decision); err != nil {
		return err
	}
	if err := validateEvolutionHardGates(scorecard.HardGates); err != nil {
		return err
	}
	if err := validateEvolutionMetrics("metrics", scorecard.Metrics, true); err != nil {
		return err
	}
	for _, field := range []evolutionNumberField{
		{name: "weighted_score", value: scorecard.WeightedScore},
		{name: "baseline_score", value: scorecard.BaselineScore},
		{name: "delta", value: scorecard.Delta},
	} {
		if err := validateEvolutionNumber(field.name, field.value); err != nil {
			return err
		}
	}
	return validateEvolutionReferences("failure_case_refs", scorecard.FailureCaseRefs, false)
}

func (approval EvolutionApproval) Validate() error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "approval_id", value: approval.ApprovalID},
		evolutionStringField{name: "run_id", value: approval.RunID},
		evolutionStringField{name: "candidate_id", value: approval.CandidateID},
		evolutionStringField{name: "scorecard_id", value: approval.ScorecardID},
		evolutionStringField{name: "approved_by", value: approval.ApprovedBy},
	); err != nil {
		return err
	}
	if err := validateEvolutionReferenceFields(
		evolutionStringField{name: "candidate_content_hash", value: approval.CandidateContentHash},
		evolutionStringField{name: "baseline_identity", value: approval.BaselineIdentity},
	); err != nil {
		return err
	}
	if err := validateEvolutionCodeFields(
		evolutionStringField{name: "decision", value: approval.Decision},
		evolutionStringField{name: "reason_code", value: approval.ReasonCode},
	); err != nil {
		return err
	}
	if err := validateEvolutionText("note", approval.Note, EvolutionApprovalNoteMaxRunes); err != nil {
		return err
	}
	createdAt, err := parseEvolutionTimestamp("created_at", approval.CreatedAt)
	if err != nil {
		return err
	}
	expiresAt, err := parseEvolutionTimestamp("expires_at", approval.ExpiresAt)
	if err != nil {
		return err
	}
	if !expiresAt.After(createdAt) {
		return fmt.Errorf("expires_at must be after created_at")
	}
	return nil
}

func (observation EvolutionObservation) Validate() error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "observation_id", value: observation.ObservationID},
		evolutionStringField{name: "run_id", value: observation.RunID},
	); err != nil {
		return err
	}
	if err := validateEvolutionReference("published_identity", observation.PublishedIdentity); err != nil {
		return err
	}
	if observation.RollbackIdentity != "" {
		if err := validateEvolutionReference("rollback_identity", observation.RollbackIdentity); err != nil {
			return err
		}
	}
	windowStart, err := parseEvolutionTimestamp("window_start", observation.WindowStart)
	if err != nil {
		return err
	}
	windowEnd, err := parseEvolutionTimestamp("window_end", observation.WindowEnd)
	if err != nil {
		return err
	}
	if !windowEnd.After(windowStart) {
		return fmt.Errorf("window_end must be after window_start")
	}
	if err := validateEvolutionMetrics("metrics", observation.Metrics, true); err != nil {
		return err
	}
	if err := validateEvolutionReferences("hard_gate_incidents", observation.HardGateIncidents, false); err != nil {
		return err
	}
	return validateEvolutionCode("outcome", observation.Outcome)
}

func (event EvolutionEvent) Validate() error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "event_id", value: event.EventID},
		evolutionStringField{name: "run_id", value: event.RunID},
		evolutionStringField{name: "actor", value: event.Actor},
	); err != nil {
		return err
	}
	if err := validateEvolutionCode("event_type", event.EventType); err != nil {
		return err
	}
	switch {
	case event.FromStatus == "" && event.ToStatus == "":
		if event.EventType == "transition" || strings.TrimSpace(event.Message) == "" {
			return fmt.Errorf("non-transition event requires a message event type and message")
		}
	case event.FromStatus == "":
		if event.ToStatus != EvolutionDetected {
			return fmt.Errorf("initial event must transition to %q", EvolutionDetected)
		}
	case event.ToStatus == "":
		return fmt.Errorf("transition event requires to_status")
	default:
		if err := ValidateEvolutionTransition(event.FromStatus, event.ToStatus); err != nil {
			return err
		}
	}
	if err := validateEvolutionCode("code", event.Code); err != nil {
		return err
	}
	if err := validateEvolutionText("message", event.Message, EvolutionEventMessageMaxRunes); err != nil {
		return err
	}
	if err := validateEvolutionReferences("artifact_refs", event.ArtifactRefs, false); err != nil {
		return err
	}
	return validateEvolutionTimestamp("created_at", event.CreatedAt)
}

func isKnownEvolutionRunStatus(status EvolutionRunStatus) bool {
	switch status {
	case EvolutionDetected,
		EvolutionTriaged,
		EvolutionGenerating,
		EvolutionEvaluating,
		EvolutionAwaitingApproval,
		EvolutionApproved,
		EvolutionPublishing,
		EvolutionObserving,
		EvolutionCompleted,
		EvolutionBlocked,
		EvolutionRejected,
		EvolutionFailed,
		EvolutionSuperseded,
		EvolutionRolledBack:
		return true
	default:
		return false
	}
}

func isKnownEvolutionRunType(runType EvolutionRunType) bool {
	switch runType {
	case EvolutionRunAgentPolicy, EvolutionRunKnowledgeRelease, EvolutionRunCombined:
		return true
	default:
		return false
	}
}

func isEvolutionAbortStatus(status EvolutionRunStatus) bool {
	return status == EvolutionBlocked || status == EvolutionFailed || status == EvolutionSuperseded
}

func isEvolutionRetryableStatus(status EvolutionRunStatus) bool {
	switch status {
	case EvolutionBlocked,
		EvolutionRejected,
		EvolutionFailed,
		EvolutionCompleted,
		EvolutionSuperseded,
		EvolutionRolledBack:
		return true
	default:
		return false
	}
}

type evolutionStringField struct {
	name  string
	value string
}

type evolutionNumberField struct {
	name  string
	value float64
}

func validateEvolutionIdentityFields(fields ...evolutionStringField) error {
	for _, field := range fields {
		if err := validateEvolutionIdentity(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func validateEvolutionCodeFields(fields ...evolutionStringField) error {
	for _, field := range fields {
		if err := validateEvolutionCode(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func validateEvolutionReferenceFields(fields ...evolutionStringField) error {
	for _, field := range fields {
		if err := validateEvolutionReference(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func validateEvolutionRunScope(run EvolutionRun) error {
	validatePackage := func() error {
		if err := validateEvolutionIdentity("package_id", run.PackageID); err != nil {
			return err
		}
		return validateEvolutionIdentity("baseline_package_version", run.BaselinePackageVersion)
	}
	validateReleases := func(required bool) error {
		return validateEvolutionReferences("baseline_release_ids", run.BaselineReleaseIDs, required)
	}

	switch run.RunType {
	case EvolutionRunAgentPolicy:
		if err := validatePackage(); err != nil {
			return err
		}
		return validateReleases(false)
	case EvolutionRunKnowledgeRelease:
		if run.PackageID != "" {
			if err := validateEvolutionIdentity("package_id", run.PackageID); err != nil {
				return err
			}
		}
		if run.BaselinePackageVersion != "" {
			if err := validateEvolutionIdentity("baseline_package_version", run.BaselinePackageVersion); err != nil {
				return err
			}
		}
		return validateReleases(true)
	case EvolutionRunCombined:
		if err := validatePackage(); err != nil {
			return err
		}
		return validateReleases(true)
	default:
		return fmt.Errorf("unknown evolution run type %q", run.RunType)
	}
}

func validateEvolutionIdentity(field, value string) error {
	return validateEvolutionToken(field, value, EvolutionIdentityMaxRunes)
}

func validateEvolutionCode(field, value string) error {
	return validateEvolutionToken(field, value, EvolutionCodeMaxRunes)
}

func validateEvolutionReference(field, value string) error {
	return validateEvolutionToken(field, value, EvolutionReferenceMaxRunes)
}

func validateEvolutionToken(field, value string, maxRunes int) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain surrounding whitespace", field)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", field)
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s exceeds %d characters", field, maxRunes)
	}
	for _, character := range value {
		if !isEvolutionTokenCharacter(character) {
			return fmt.Errorf("%s contains unsupported characters", field)
		}
	}
	return nil
}

func isEvolutionTokenCharacter(character rune) bool {
	if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
		return true
	}
	switch character {
	case '_', '-', '.', ':', '/', '@', '+', '#', '?', '=', '&', '%':
		return true
	default:
		return false
	}
}

func validateEvolutionReferences(field string, values []string, required bool) error {
	if required && len(values) == 0 {
		return fmt.Errorf("%s is required", field)
	}
	if len(values) > EvolutionCollectionMaxItems {
		return fmt.Errorf("%s exceeds %d items", field, EvolutionCollectionMaxItems)
	}
	for index, value := range values {
		if err := validateEvolutionReference(fmt.Sprintf("%s[%d]", field, index), value); err != nil {
			return err
		}
	}
	return nil
}

func validateEvolutionHardGates(gates map[string]bool) error {
	if len(gates) == 0 {
		return fmt.Errorf("hard_gates is required")
	}
	if len(gates) > EvolutionMetricMaxItems {
		return fmt.Errorf("hard_gates exceeds %d items", EvolutionMetricMaxItems)
	}
	names := make([]string, 0, len(gates))
	for name := range gates {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := validateEvolutionCode("hard_gates key", name); err != nil {
			return err
		}
	}
	return nil
}

func validateEvolutionMetrics(field string, metrics map[string]float64, required bool) error {
	if required && len(metrics) == 0 {
		return fmt.Errorf("%s is required", field)
	}
	if len(metrics) > EvolutionMetricMaxItems {
		return fmt.Errorf("%s exceeds %d items", field, EvolutionMetricMaxItems)
	}
	names := make([]string, 0, len(metrics))
	for name := range metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := validateEvolutionCode(field+" key", name); err != nil {
			return err
		}
		if err := validateEvolutionNumber(field+" value", metrics[name]); err != nil {
			return err
		}
	}
	return nil
}

func validateEvolutionNumber(field string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s must be finite", field)
	}
	return nil
}

func validateEvolutionTimestamp(field, value string) error {
	_, err := parseEvolutionTimestamp(field, value)
	return err
}

func parseEvolutionTimestamp(field, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.IsZero() {
		return time.Time{}, fmt.Errorf("%s must be a non-zero RFC3339 timestamp", field)
	}
	return parsed, nil
}

func validateEvolutionText(field, value string, maxRunes int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", field)
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s exceeds %d characters", field, maxRunes)
	}
	return nil
}
