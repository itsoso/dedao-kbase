package app

import (
	"errors"
	"fmt"
	"strings"
)

const (
	ResearchRunSchemaVersion = "research_run.v1"

	ResearchModeAuto  = "auto"
	ResearchModeQuick = "quick"
	ResearchModeDeep  = "deep"

	ResearchSourceKnowledge = "knowledge"
	ResearchSourceChatlog   = "chatlog"
	ResearchSourcePriorRuns = "prior_runs"

	ResearchRouteExplicitQuick    = "explicit_quick"
	ResearchRouteExplicitDeep     = "explicit_deep"
	ResearchRoutePrivateHistory   = "private_history"
	ResearchRoutePriorResearch    = "prior_research"
	ResearchRouteCrossSource      = "cross_source"
	ResearchRouteIdentity         = "identity_resolution"
	ResearchRouteTimeline         = "timeline_reconstruction"
	ResearchRouteCaseComparison   = "case_comparison"
	ResearchRouteConflict         = "conflict_analysis"
	ResearchRouteBoundedKnowledge = "bounded_knowledge"

	researchQuestionMaxRunes       = 8000
	researchPackageIDMaxRunes      = 128
	researchPackageVersionMaxRunes = 128
	researchRequestedSourcesMax    = 8
	researchSubjectIDsMax          = 16
)

var ErrResearchDeepRequired = errors.New("deep_research_required")

type ResearchRunStatus string

const (
	ResearchPlanning           ResearchRunStatus = "planning"
	ResearchRetrieving         ResearchRunStatus = "retrieving"
	ResearchResolvingIdentity  ResearchRunStatus = "resolving_identity"
	ResearchBuildingTimeline   ResearchRunStatus = "building_timeline"
	ResearchExtractingFacts    ResearchRunStatus = "extracting_facts"
	ResearchDetectingConflicts ResearchRunStatus = "detecting_conflicts"
	ResearchComparingCases     ResearchRunStatus = "comparing_cases"
	ResearchSynthesizing       ResearchRunStatus = "synthesizing"
	ResearchVerifying          ResearchRunStatus = "verifying"
	ResearchCompleted          ResearchRunStatus = "completed"
	ResearchInsufficient       ResearchRunStatus = "insufficient"
	ResearchFailed             ResearchRunStatus = "failed"
	ResearchCanceled           ResearchRunStatus = "canceled"
)

type ResearchRunRequest struct {
	PreflightID      string   `json:"preflight_id"`
	Mode             string   `json:"mode"`
	Question         string   `json:"question"`
	PackageID        string   `json:"package_id,omitempty"`
	PackageVersion   string   `json:"package_version,omitempty"`
	RequestedSources []string `json:"requested_sources,omitempty"`
	SubjectIDs       []string `json:"subject_ids,omitempty"`
}

type ResearchRun struct {
	SchemaVersion    string            `json:"schema_version"`
	RunID            string            `json:"run_id"`
	ParentRunID      string            `json:"parent_run_id,omitempty"`
	PreflightID      string            `json:"preflight_id,omitempty"`
	Mode             string            `json:"mode"`
	Question         string            `json:"question"`
	Status           ResearchRunStatus `json:"status"`
	PackageID        string            `json:"package_id,omitempty"`
	PackageVersion   string            `json:"package_version,omitempty"`
	SubjectIDs       []string          `json:"subject_ids,omitempty"`
	RequestedSources []string          `json:"requested_sources"`
	RouteReasons     []string          `json:"route_reasons,omitempty"`
	ActualScope      ResearchScope     `json:"actual_scope"`
	Budget           ResearchBudget    `json:"budget"`
	WaitReason       string            `json:"wait_reason,omitempty"`
	Failure          *ResearchFailure  `json:"failure,omitempty"`
	Version          int64             `json:"version"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	LeaseOwner       string            `json:"-"`
	LeaseEpoch       string            `json:"-"`
	LeaseExpiresAt   string            `json:"-"`
}

type ResearchScope struct {
	TimeFrom            string   `json:"time_from,omitempty"`
	TimeTo              string   `json:"time_to,omitempty"`
	KnowledgeReleaseIDs []string `json:"knowledge_release_ids,omitempty"`
	ChatScopeIDs        []string `json:"chat_scope_ids,omitempty"`
	SearchedSources     []string `json:"searched_sources,omitempty"`
	CitedSources        []string `json:"cited_sources,omitempty"`
}

type ResearchStageSummary struct {
	Stage           ResearchRunStatus `json:"stage"`
	Status          string            `json:"status"`
	DecisionSummary string            `json:"decision_summary,omitempty"`
	StartedAt       string            `json:"started_at,omitempty"`
	CompletedAt     string            `json:"completed_at,omitempty"`
}

type ResearchBudget struct {
	MaxIterations    int     `json:"max_iterations"`
	MaxEvidenceItems int     `json:"max_evidence_items"`
	MaxQuotedChars   int     `json:"max_quoted_chars"`
	MaxModelCalls    int     `json:"max_model_calls"`
	MaxCostUSD       float64 `json:"max_cost_usd"`
}

type ResearchFailure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type ResearchTransition struct {
	Code    string `json:"code"`
	Actor   string `json:"actor"`
	Summary string `json:"summary,omitempty"`
}

func ValidateResearchRunRequest(request ResearchRunRequest) error {
	preflightID := strings.TrimSpace(request.PreflightID)
	if preflightID == "" {
		return ErrResearchPreflightRequired
	}
	if len([]rune(preflightID)) > researchPreflightIDMaxRunes ||
		!validResearchPreflightResourceID(preflightID) || preflightID != request.PreflightID {
		return fmt.Errorf("preflight_id must be a canonical bounded Research resource ID")
	}
	mode := strings.TrimSpace(request.Mode)
	if mode == "" {
		mode = ResearchModeAuto
	}
	switch mode {
	case ResearchModeAuto, ResearchModeQuick, ResearchModeDeep:
	default:
		return fmt.Errorf("mode must be auto, quick, or deep")
	}
	question := strings.TrimSpace(request.Question)
	if question == "" {
		return fmt.Errorf("question is required")
	}
	if len([]rune(question)) > researchQuestionMaxRunes {
		return fmt.Errorf("question exceeds %d characters", researchQuestionMaxRunes)
	}
	if len([]rune(strings.TrimSpace(request.PackageID))) > researchPackageIDMaxRunes {
		return fmt.Errorf("package_id exceeds %d characters", researchPackageIDMaxRunes)
	}
	if len([]rune(strings.TrimSpace(request.PackageVersion))) > researchPackageVersionMaxRunes {
		return fmt.Errorf("package_version exceeds %d characters", researchPackageVersionMaxRunes)
	}
	if strings.TrimSpace(request.PackageID) == "" || strings.TrimSpace(request.PackageVersion) == "" {
		return fmt.Errorf("package_id and package_version are required")
	}
	if len(request.RequestedSources) > researchRequestedSourcesMax {
		return fmt.Errorf("requested_sources exceeds %d items", researchRequestedSourcesMax)
	}
	seenSources := make(map[string]bool, len(request.RequestedSources))
	for _, rawSource := range request.RequestedSources {
		source := strings.TrimSpace(rawSource)
		switch source {
		case ResearchSourceKnowledge, ResearchSourceChatlog, ResearchSourcePriorRuns:
		default:
			return fmt.Errorf("unsupported requested source %q", source)
		}
		if seenSources[source] {
			return fmt.Errorf("duplicate requested source %q", source)
		}
		seenSources[source] = true
	}
	if len(request.SubjectIDs) > researchSubjectIDsMax {
		return fmt.Errorf("subject_ids exceeds %d items", researchSubjectIDsMax)
	}
	if _, err := normalizeResearchSubjectIDs(request.SubjectIDs); err != nil {
		return err
	}
	return nil
}

func RouteResearchMode(request ResearchRunRequest) (string, []string, error) {
	if err := ValidateResearchRunRequest(request); err != nil {
		return "", nil, err
	}
	mode, reasons := routeValidatedResearchMode(request)
	return mode, reasons, nil
}

func routeValidatedResearchMode(request ResearchRunRequest) (string, []string) {
	mode := strings.TrimSpace(request.Mode)
	if mode == "" {
		mode = ResearchModeAuto
	}
	switch mode {
	case ResearchModeQuick:
		return ResearchModeQuick, []string{ResearchRouteExplicitQuick}
	case ResearchModeDeep:
		return ResearchModeDeep, []string{ResearchRouteExplicitDeep}
	}
	reasons := researchDeepRouteReasons(request)
	if len(reasons) > 0 {
		return ResearchModeDeep, reasons
	}
	return ResearchModeQuick, []string{ResearchRouteBoundedKnowledge}
}

func ValidateResearchModeScope(request ResearchRunRequest, resolvedMode string) error {
	if err := ValidateResearchRunRequest(request); err != nil {
		return err
	}
	if resolvedMode == ResearchModeQuick && len(researchDeepRouteReasons(request)) > 0 {
		return ErrResearchDeepRequired
	}
	return nil
}

func researchDeepRouteReasons(request ResearchRunRequest) []string {
	reasons := make([]string, 0, 6)
	seen := map[string]bool{}
	appendReason := func(reason string) {
		if !seen[reason] {
			seen[reason] = true
			reasons = append(reasons, reason)
		}
	}
	for _, rawSource := range request.RequestedSources {
		switch strings.TrimSpace(rawSource) {
		case ResearchSourceChatlog:
			appendReason(ResearchRoutePrivateHistory)
		case ResearchSourcePriorRuns:
			appendReason(ResearchRoutePriorResearch)
		}
	}
	if len(request.RequestedSources) > 1 {
		appendReason(ResearchRouteCrossSource)
	}
	question := strings.ToLower(strings.TrimSpace(request.Question))
	for _, match := range []struct {
		reason string
		terms  []string
	}{
		{ResearchRouteIdentity, []string{"身份", "同一个人", "identity", "alias"}},
		{ResearchRouteTimeline, []string{"时间线", "过程", "timeline", "history", "earlier", "去年", "之前"}},
		{ResearchRouteCaseComparison, []string{"比较", "对比", "compare", "case"}},
		{ResearchRouteConflict, []string{"冲突", "矛盾", "conflict", "contradict"}},
	} {
		for _, term := range match.terms {
			if strings.Contains(question, term) {
				appendReason(match.reason)
				break
			}
		}
	}
	return reasons
}

func ValidateResearchTransition(from, to ResearchRunStatus) error {
	if isTerminalResearchStatus(from) {
		return fmt.Errorf("terminal research run cannot transition from %q", from)
	}
	if to == ResearchFailed || to == ResearchCanceled || to == ResearchInsufficient {
		return nil
	}
	allowed := map[ResearchRunStatus]map[ResearchRunStatus]bool{
		ResearchPlanning: {
			ResearchRetrieving: true,
		},
		ResearchRetrieving: {
			ResearchPlanning:          true,
			ResearchResolvingIdentity: true,
			ResearchExtractingFacts:   true,
			ResearchSynthesizing:      true,
		},
		ResearchResolvingIdentity: {
			ResearchExtractingFacts: true,
		},
		ResearchBuildingTimeline: {
			ResearchDetectingConflicts: true,
			ResearchComparingCases:     true,
			ResearchSynthesizing:       true,
		},
		ResearchExtractingFacts: {
			ResearchBuildingTimeline:   true,
			ResearchDetectingConflicts: true,
			ResearchComparingCases:     true,
			ResearchSynthesizing:       true,
		},
		ResearchDetectingConflicts: {
			ResearchComparingCases: true,
			ResearchSynthesizing:   true,
		},
		ResearchComparingCases: {
			ResearchSynthesizing: true,
		},
		ResearchSynthesizing: {
			ResearchVerifying: true,
		},
		ResearchVerifying: {
			ResearchPlanning:     true,
			ResearchCompleted:    true,
			ResearchInsufficient: true,
		},
	}
	if !allowed[from][to] {
		return fmt.Errorf("research transition %q -> %q is not allowed", from, to)
	}
	return nil
}

func isTerminalResearchStatus(status ResearchRunStatus) bool {
	switch status {
	case ResearchCompleted, ResearchInsufficient, ResearchFailed, ResearchCanceled:
		return true
	default:
		return false
	}
}
