package app

import (
	"fmt"
	"math"
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
	for field, value := range map[string]string{
		"source_id":  signal.SourceID,
		"package_id": signal.PackageID,
		"release_id": signal.ReleaseID,
	} {
		if value != "" {
			if err := validateEvolutionIdentity(field, value); err != nil {
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
	for field, value := range map[string]string{
		"signal_type": signal.SignalType,
		"source_type": signal.SourceType,
		"severity":    signal.Severity,
	} {
		if err := validateEvolutionCode(field, value); err != nil {
			return err
		}
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
	} else if run.RetryOfRunID != "" {
		if err := validateEvolutionIdentity("retry_of_run_id", run.RetryOfRunID); err != nil {
			return err
		}
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
	if err := validateEvolutionTimestamp("created_at", run.CreatedAt); err != nil {
		return err
	}
	return validateEvolutionTimestamp("updated_at", run.UpdatedAt)
}

func (candidate EvolutionCandidate) Validate() error {
	for field, value := range map[string]string{
		"candidate_id":      candidate.CandidateID,
		"run_id":            candidate.RunID,
		"generator_version": candidate.GeneratorVersion,
	} {
		if err := validateEvolutionIdentity(field, value); err != nil {
			return err
		}
	}
	if err := validateEvolutionCode("candidate_type", candidate.CandidateType); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"content_hash":      candidate.ContentHash,
		"artifact_ref":      candidate.ArtifactRef,
		"baseline_identity": candidate.BaselineIdentity,
	} {
		if err := validateEvolutionReference(field, value); err != nil {
			return err
		}
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
	for field, value := range map[string]string{
		"scorecard_id":   scorecard.ScorecardID,
		"candidate_id":   scorecard.CandidateID,
		"suite_version":  scorecard.SuiteVersion,
		"scorer_version": scorecard.ScorerVersion,
	} {
		if err := validateEvolutionIdentity(field, value); err != nil {
			return err
		}
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
	for field, value := range map[string]float64{
		"weighted_score": scorecard.WeightedScore,
		"baseline_score": scorecard.BaselineScore,
		"delta":          scorecard.Delta,
	} {
		if err := validateEvolutionNumber(field, value); err != nil {
			return err
		}
	}
	return validateEvolutionReferences("failure_case_refs", scorecard.FailureCaseRefs, false)
}

func (approval EvolutionApproval) Validate() error {
	for field, value := range map[string]string{
		"approval_id":  approval.ApprovalID,
		"run_id":       approval.RunID,
		"candidate_id": approval.CandidateID,
		"scorecard_id": approval.ScorecardID,
		"approved_by":  approval.ApprovedBy,
	} {
		if err := validateEvolutionIdentity(field, value); err != nil {
			return err
		}
	}
	for field, value := range map[string]string{
		"candidate_content_hash": approval.CandidateContentHash,
		"baseline_identity":      approval.BaselineIdentity,
	} {
		if err := validateEvolutionReference(field, value); err != nil {
			return err
		}
	}
	for field, value := range map[string]string{
		"decision":    approval.Decision,
		"reason_code": approval.ReasonCode,
	} {
		if err := validateEvolutionCode(field, value); err != nil {
			return err
		}
	}
	if err := validateEvolutionText("note", approval.Note, EvolutionApprovalNoteMaxRunes); err != nil {
		return err
	}
	if err := validateEvolutionTimestamp("created_at", approval.CreatedAt); err != nil {
		return err
	}
	return validateEvolutionTimestamp("expires_at", approval.ExpiresAt)
}

func (observation EvolutionObservation) Validate() error {
	for field, value := range map[string]string{
		"observation_id": observation.ObservationID,
		"run_id":         observation.RunID,
	} {
		if err := validateEvolutionIdentity(field, value); err != nil {
			return err
		}
	}
	if err := validateEvolutionReference("published_identity", observation.PublishedIdentity); err != nil {
		return err
	}
	if observation.RollbackIdentity != "" {
		if err := validateEvolutionReference("rollback_identity", observation.RollbackIdentity); err != nil {
			return err
		}
	}
	if err := validateEvolutionTimestamp("window_start", observation.WindowStart); err != nil {
		return err
	}
	if err := validateEvolutionTimestamp("window_end", observation.WindowEnd); err != nil {
		return err
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
	for field, value := range map[string]string{
		"event_id": event.EventID,
		"run_id":   event.RunID,
		"actor":    event.Actor,
	} {
		if err := validateEvolutionIdentity(field, value); err != nil {
			return err
		}
	}
	if err := validateEvolutionCode("event_type", event.EventType); err != nil {
		return err
	}
	if event.FromStatus != "" && !isKnownEvolutionRunStatus(event.FromStatus) {
		return fmt.Errorf("unknown evolution run status %q", event.FromStatus)
	}
	if !isKnownEvolutionRunStatus(event.ToStatus) {
		return fmt.Errorf("unknown evolution run status %q", event.ToStatus)
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
	for name := range gates {
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
	for name, value := range metrics {
		if err := validateEvolutionCode(field+" key", name); err != nil {
			return err
		}
		if err := validateEvolutionNumber(field+" value", value); err != nil {
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
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.IsZero() {
		return fmt.Errorf("%s must be a non-zero RFC3339 timestamp", field)
	}
	return nil
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
