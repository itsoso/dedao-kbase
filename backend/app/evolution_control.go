package app

import (
	"fmt"
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

func (run EvolutionRun) Validate() error {
	if !isKnownEvolutionRunType(run.RunType) {
		return fmt.Errorf("unknown evolution run type %q", run.RunType)
	}
	if !isKnownEvolutionRunStatus(run.Status) {
		return fmt.Errorf("unknown evolution run status %q", run.Status)
	}
	return validateEvolutionText("failure_message", run.FailureMessage, EvolutionFailureMessageMaxRunes)
}

func (candidate EvolutionCandidate) Validate() error {
	return validateEvolutionText("change_summary", candidate.ChangeSummary, EvolutionChangeSummaryMaxRunes)
}

func (approval EvolutionApproval) Validate() error {
	return validateEvolutionText("note", approval.Note, EvolutionApprovalNoteMaxRunes)
}

func (event EvolutionEvent) Validate() error {
	if event.FromStatus != "" && !isKnownEvolutionRunStatus(event.FromStatus) {
		return fmt.Errorf("unknown evolution run status %q", event.FromStatus)
	}
	if event.ToStatus != "" && !isKnownEvolutionRunStatus(event.ToStatus) {
		return fmt.Errorf("unknown evolution run status %q", event.ToStatus)
	}
	return validateEvolutionText("message", event.Message, EvolutionEventMessageMaxRunes)
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

func validateEvolutionText(field, value string, maxRunes int) error {
	if utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s exceeds %d characters", field, maxRunes)
	}
	return nil
}
