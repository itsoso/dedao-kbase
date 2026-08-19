package app

import (
	"fmt"
	"sort"
	"strings"
)

const (
	ResearchPreflightStatusReady   = "ready"
	ResearchPreflightStatusBlocked = "blocked"

	ResearchPreflightMatchHigh   = "high"
	ResearchPreflightMatchMedium = "medium"
	ResearchPreflightMatchLow    = "low"

	ResearchPreflightCheckPass    = "pass"
	ResearchPreflightCheckWarning = "warning"
	ResearchPreflightCheckBlocked = "blocked"

	researchPreflightCandidateMax = 3
)

type ResearchPreflightRequest struct {
	Mode              string   `json:"mode"`
	Question          string   `json:"question"`
	RequestedSources  []string `json:"requested_sources,omitempty"`
	PackageConstraint string   `json:"package_constraint,omitempty"`
	ParentRunID       string   `json:"parent_run_id,omitempty"`
}

type ResearchPreflight struct {
	PreflightID string                       `json:"preflight_id"`
	RequestHash string                       `json:"-"`
	Status      string                       `json:"status"`
	Candidates  []ResearchPreflightCandidate `json:"candidates"`
	Checks      []ResearchPreflightCheck     `json:"checks"`
	Gaps        []ResearchPreflightGap       `json:"gaps,omitempty"`
	ParentRunID string                       `json:"parent_run_id,omitempty"`
	CreatedAt   string                       `json:"created_at"`
	ExpiresAt   string                       `json:"expires_at"`
}

type ResearchPreflightCandidate struct {
	PackageID        string   `json:"package_id"`
	PackageVersion   string   `json:"package_version"`
	ContentHash      string   `json:"content_hash"`
	DisplayName      string   `json:"display_name,omitempty"`
	MatchLevel       string   `json:"match_level"`
	ReasonCodes      []string `json:"reason_codes,omitempty"`
	KnowledgeScope   []string `json:"knowledge_scope,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
	EvaluationStatus string   `json:"evaluation_status,omitempty"`
	SupportedSources []string `json:"supported_sources,omitempty"`
	Readiness        string   `json:"readiness"`
}

type ResearchPreflightCheck struct {
	Code       string `json:"code"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

type ResearchPreflightGap struct {
	Code       string `json:"code"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

type PublicResearchPreflightResult struct {
	PreflightID string                             `json:"preflight_id"`
	Status      string                             `json:"status"`
	Candidates  []PublicResearchPreflightCandidate `json:"candidates"`
	Checks      []PublicResearchPreflightCheck     `json:"checks"`
	Gaps        []PublicResearchPreflightGap       `json:"gaps,omitempty"`
	ParentRunID string                             `json:"parent_run_id,omitempty"`
	CreatedAt   string                             `json:"created_at"`
	ExpiresAt   string                             `json:"expires_at"`
}

type PublicResearchPreflightCandidate struct {
	PackageID        string   `json:"package_id"`
	PackageVersion   string   `json:"package_version"`
	DisplayName      string   `json:"display_name,omitempty"`
	MatchLevel       string   `json:"match_level"`
	ReasonCodes      []string `json:"reason_codes,omitempty"`
	KnowledgeScope   []string `json:"knowledge_scope,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
	EvaluationStatus string   `json:"evaluation_status,omitempty"`
	SupportedSources []string `json:"supported_sources,omitempty"`
	Readiness        string   `json:"readiness"`
}

type PublicResearchPreflightCheck struct {
	Code       string `json:"code"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

type PublicResearchPreflightGap struct {
	Code       string `json:"code"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

func NormalizeResearchPreflightRequest(request ResearchPreflightRequest) (ResearchPreflightRequest, error) {
	request.Mode = strings.TrimSpace(request.Mode)
	if request.Mode == "" {
		request.Mode = ResearchModeAuto
	}
	switch request.Mode {
	case ResearchModeAuto, ResearchModeQuick, ResearchModeDeep:
	default:
		return ResearchPreflightRequest{}, fmt.Errorf("mode must be auto, quick, or deep")
	}

	request.Question = strings.TrimSpace(request.Question)
	if request.Question == "" {
		return ResearchPreflightRequest{}, fmt.Errorf("question is required")
	}
	if len([]rune(request.Question)) > researchQuestionMaxRunes {
		return ResearchPreflightRequest{}, fmt.Errorf("question exceeds %d characters", researchQuestionMaxRunes)
	}

	request.PackageConstraint = strings.TrimSpace(request.PackageConstraint)
	if len([]rune(request.PackageConstraint)) > researchPackageIDMaxRunes {
		return ResearchPreflightRequest{}, fmt.Errorf("package_constraint exceeds %d characters", researchPackageIDMaxRunes)
	}
	request.ParentRunID = strings.TrimSpace(request.ParentRunID)
	if len([]rune(request.ParentRunID)) > researchPackageIDMaxRunes {
		return ResearchPreflightRequest{}, fmt.Errorf("parent_run_id exceeds %d characters", researchPackageIDMaxRunes)
	}

	if len(request.RequestedSources) > researchRequestedSourcesMax {
		return ResearchPreflightRequest{}, fmt.Errorf("requested_sources exceeds %d items", researchRequestedSourcesMax)
	}
	seenSources := make(map[string]bool, len(request.RequestedSources))
	normalizedSources := make([]string, 0, len(request.RequestedSources))
	for _, rawSource := range request.RequestedSources {
		source := strings.TrimSpace(rawSource)
		switch source {
		case ResearchSourceKnowledge, ResearchSourceChatlog, ResearchSourcePriorRuns:
		default:
			return ResearchPreflightRequest{}, fmt.Errorf("unsupported requested source %q", source)
		}
		if seenSources[source] {
			return ResearchPreflightRequest{}, fmt.Errorf("duplicate requested source %q", source)
		}
		seenSources[source] = true
		normalizedSources = append(normalizedSources, source)
	}
	sort.Slice(normalizedSources, func(left, right int) bool {
		return researchPreflightSourceOrder(normalizedSources[left]) < researchPreflightSourceOrder(normalizedSources[right])
	})
	request.RequestedSources = normalizedSources

	return request, nil
}

func ValidateResearchPreflight(preflight ResearchPreflight) error {
	switch preflight.Status {
	case ResearchPreflightStatusReady, ResearchPreflightStatusBlocked:
	default:
		return fmt.Errorf("unsupported preflight status %q", preflight.Status)
	}
	if len(preflight.Candidates) > researchPreflightCandidateMax {
		return fmt.Errorf("candidates exceeds %d items", researchPreflightCandidateMax)
	}
	confirmableCandidate := false
	blockedCandidates := 0
	for index, candidate := range preflight.Candidates {
		if strings.TrimSpace(candidate.PackageID) == "" {
			return fmt.Errorf("candidate %d package_id is required", index)
		}
		if strings.TrimSpace(candidate.PackageVersion) == "" {
			return fmt.Errorf("candidate %d package_version is required", index)
		}
		if strings.TrimSpace(candidate.ContentHash) == "" {
			return fmt.Errorf("candidate %d content_hash is required", index)
		}
		if !isResearchPreflightMatchLevel(candidate.MatchLevel) {
			return fmt.Errorf("candidate %d has unsupported match level %q", index, candidate.MatchLevel)
		}
		if !isResearchPreflightCheckStatus(candidate.Readiness) {
			return fmt.Errorf("candidate %d has unsupported check status %q", index, candidate.Readiness)
		}
		switch candidate.Readiness {
		case ResearchPreflightCheckPass, ResearchPreflightCheckWarning:
			confirmableCandidate = true
		case ResearchPreflightCheckBlocked:
			blockedCandidates++
		}
	}
	if preflight.Status == ResearchPreflightStatusReady {
		if len(preflight.Candidates) > 0 && blockedCandidates == len(preflight.Candidates) {
			return fmt.Errorf("ready preflight cannot have all candidates blocked")
		}
		if !confirmableCandidate {
			return fmt.Errorf("ready preflight requires a confirmable candidate")
		}
	}
	for index, check := range preflight.Checks {
		if !isResearchPreflightCheckStatus(check.Status) {
			return fmt.Errorf("check %d has unsupported check status %q", index, check.Status)
		}
		if preflight.Status == ResearchPreflightStatusReady && check.Status == ResearchPreflightCheckBlocked {
			return fmt.Errorf("ready preflight cannot contain blocked check %d", index)
		}
	}
	return nil
}

func PublicResearchPreflight(preflight ResearchPreflight) PublicResearchPreflightResult {
	public := PublicResearchPreflightResult{
		PreflightID: preflight.PreflightID,
		Status:      preflight.Status,
		Candidates:  make([]PublicResearchPreflightCandidate, 0, len(preflight.Candidates)),
		Checks:      make([]PublicResearchPreflightCheck, 0, len(preflight.Checks)),
		Gaps:        make([]PublicResearchPreflightGap, 0, len(preflight.Gaps)),
		ParentRunID: preflight.ParentRunID,
		CreatedAt:   preflight.CreatedAt,
		ExpiresAt:   preflight.ExpiresAt,
	}
	for _, candidate := range preflight.Candidates {
		public.Candidates = append(public.Candidates, PublicResearchPreflightCandidate{
			PackageID:        candidate.PackageID,
			PackageVersion:   candidate.PackageVersion,
			DisplayName:      candidate.DisplayName,
			MatchLevel:       candidate.MatchLevel,
			ReasonCodes:      append([]string(nil), candidate.ReasonCodes...),
			KnowledgeScope:   append([]string(nil), candidate.KnowledgeScope...),
			UpdatedAt:        candidate.UpdatedAt,
			EvaluationStatus: candidate.EvaluationStatus,
			SupportedSources: append([]string(nil), candidate.SupportedSources...),
			Readiness:        candidate.Readiness,
		})
	}
	for _, check := range preflight.Checks {
		public.Checks = append(public.Checks, PublicResearchPreflightCheck{
			Code:       check.Code,
			Status:     check.Status,
			Message:    check.Message,
			NextAction: check.NextAction,
		})
	}
	for _, gap := range preflight.Gaps {
		public.Gaps = append(public.Gaps, PublicResearchPreflightGap{
			Code:       gap.Code,
			Message:    gap.Message,
			NextAction: gap.NextAction,
		})
	}
	return public
}

func researchPreflightSourceOrder(source string) int {
	switch source {
	case ResearchSourceKnowledge:
		return 0
	case ResearchSourceChatlog:
		return 1
	case ResearchSourcePriorRuns:
		return 2
	default:
		return 3
	}
}

func isResearchPreflightMatchLevel(value string) bool {
	switch value {
	case ResearchPreflightMatchHigh, ResearchPreflightMatchMedium, ResearchPreflightMatchLow:
		return true
	default:
		return false
	}
}

func isResearchPreflightCheckStatus(value string) bool {
	switch value {
	case ResearchPreflightCheckPass, ResearchPreflightCheckWarning, ResearchPreflightCheckBlocked:
		return true
	default:
		return false
	}
}
