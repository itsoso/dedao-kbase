package app

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	AgentCompilationRequestSchemaVersion = "agent-compilation-request.v1"
	AgentCompilationSchemaVersion        = "agent-compilation.v1"
	AgentCompilerVersion                 = "deterministic-agent-compiler.v1"

	AgentCompilationModeDual     = "dual"
	AgentCompilationModeEvidence = "evidence"
	AgentCompilationModeStudy    = "study"

	AgentCompilationStatusReady   = "ready"
	AgentCompilationStatusPartial = "partial"
	AgentCompilationStatusBlocked = "blocked"

	AgentCompilationCandidateStudy    = "study"
	AgentCompilationCandidateEvidence = "evidence"

	AgentCompilationCandidateReady   = "ready"
	AgentCompilationCandidateBlocked = "blocked"

	AgentCompilationIssueSupportingReleaseRequired = "supporting_release_required"
	AgentCompilationIssueReleaseNotInAssembly      = "release_not_in_assembly"
	AgentCompilationIssueReleaseInvalid            = "release_invalid"
	AgentCompilationIssueReleaseNotIndependent     = "release_not_independent"
	AgentCompilationIssueMissingCitations          = "missing_citations"

	AgentCompilationNextActionEvaluate      = "run_trusted_evaluation"
	AgentCompilationNextActionSelectSupport = "select_supporting_release"

	agentCompilationMaxSupportingReleases = 16
	agentCompilationMaxCandidates         = 2
	agentCompilationMaxIssuesPerCandidate = 8
	agentCompilationMaxNextActions        = 4
	agentCompilationMaxIssueMessageRunes  = 256
)

var agentCompilationVersionPattern = regexp.MustCompile(
	`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`,
)

type AgentCompilationRequest struct {
	SchemaVersion        string   `json:"schema_version"`
	Mode                 string   `json:"mode"`
	PrimaryReleaseID     string   `json:"primary_release_id"`
	SupportingReleaseIDs []string `json:"supporting_release_ids,omitempty"`
	Version              string   `json:"version"`
}

type AgentCompilation struct {
	SchemaVersion   string                      `json:"schema_version"`
	CompilerVersion string                      `json:"compiler_version"`
	CompilationID   string                      `json:"compilation_id"`
	Mode            string                      `json:"mode"`
	AssemblyID      string                      `json:"assembly_id"`
	ReleaseIDs      []string                    `json:"release_ids"`
	Status          string                      `json:"status"`
	Candidates      []AgentCompilationCandidate `json:"candidates"`
}

type AgentCompilationCandidate struct {
	Kind        string                  `json:"kind"`
	Status      string                  `json:"status"`
	Package     *AgentPackage           `json:"package,omitempty"`
	Issues      []AgentCompilationIssue `json:"issues,omitempty"`
	NextActions []string                `json:"next_actions,omitempty"`
}

type AgentCompilationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ValidateAgentCompilationRequest(request AgentCompilationRequest) error {
	if request.SchemaVersion != AgentCompilationRequestSchemaVersion {
		return fmt.Errorf("schema_version must be %q", AgentCompilationRequestSchemaVersion)
	}
	switch request.Mode {
	case AgentCompilationModeDual, AgentCompilationModeEvidence, AgentCompilationModeStudy:
	default:
		return fmt.Errorf("mode must be dual, evidence, or study")
	}
	if strings.TrimSpace(request.PrimaryReleaseID) == "" {
		return fmt.Errorf("primary_release_id is required")
	}
	if request.PrimaryReleaseID != strings.TrimSpace(request.PrimaryReleaseID) {
		return fmt.Errorf("primary_release_id must use canonical form without surrounding whitespace")
	}
	if !agentCompilationVersionPattern.MatchString(request.Version) {
		return fmt.Errorf("version must be a semantic version")
	}
	if len(request.SupportingReleaseIDs) > agentCompilationMaxSupportingReleases {
		return fmt.Errorf(
			"supporting_release_ids must not exceed %d items",
			agentCompilationMaxSupportingReleases,
		)
	}
	seen := make(map[string]struct{}, len(request.SupportingReleaseIDs))
	for index, releaseID := range request.SupportingReleaseIDs {
		canonical := strings.TrimSpace(releaseID)
		if canonical == "" {
			return fmt.Errorf("supporting_release_ids[%d] is required", index)
		}
		if canonical != releaseID {
			return fmt.Errorf(
				"supporting_release_ids[%d] must use canonical form without surrounding whitespace",
				index,
			)
		}
		if releaseID == request.PrimaryReleaseID {
			return fmt.Errorf("primary release must not be repeated as a supporting release")
		}
		if _, duplicate := seen[releaseID]; duplicate {
			return fmt.Errorf("duplicate supporting release %q", boundedEvidenceID(releaseID))
		}
		seen[releaseID] = struct{}{}
	}
	return nil
}

func ValidateAgentCompilation(compilation AgentCompilation) error {
	if compilation.SchemaVersion != AgentCompilationSchemaVersion {
		return fmt.Errorf("schema_version must be %q", AgentCompilationSchemaVersion)
	}
	if compilation.CompilerVersion != AgentCompilerVersion {
		return fmt.Errorf("compiler_version must be %q", AgentCompilerVersion)
	}
	if err := requireContractFields(map[string]string{
		"compilation_id": compilation.CompilationID,
		"assembly_id":    compilation.AssemblyID,
	}); err != nil {
		return err
	}
	switch compilation.Mode {
	case AgentCompilationModeDual, AgentCompilationModeEvidence, AgentCompilationModeStudy:
	default:
		return fmt.Errorf("mode must be dual, evidence, or study")
	}
	if len(compilation.ReleaseIDs) == 0 {
		return fmt.Errorf("release_ids is required")
	}
	if len(compilation.Candidates) == 0 || len(compilation.Candidates) > agentCompilationMaxCandidates {
		return fmt.Errorf("candidates must contain between 1 and %d items", agentCompilationMaxCandidates)
	}

	seenReleases := make(map[string]struct{}, len(compilation.ReleaseIDs))
	for index, releaseID := range compilation.ReleaseIDs {
		if strings.TrimSpace(releaseID) == "" {
			return fmt.Errorf("release_ids[%d] is required", index)
		}
		if _, duplicate := seenReleases[releaseID]; duplicate {
			return fmt.Errorf("release_ids contains duplicate %q", boundedEvidenceID(releaseID))
		}
		seenReleases[releaseID] = struct{}{}
	}

	readyCount := 0
	blockedCount := 0
	seenKinds := make(map[string]struct{}, len(compilation.Candidates))
	for index, candidate := range compilation.Candidates {
		switch candidate.Kind {
		case AgentCompilationCandidateStudy, AgentCompilationCandidateEvidence:
		default:
			return fmt.Errorf("candidates[%d].kind is invalid", index)
		}
		if _, duplicate := seenKinds[candidate.Kind]; duplicate {
			return fmt.Errorf("candidates contains duplicate kind %q", candidate.Kind)
		}
		seenKinds[candidate.Kind] = struct{}{}
		if len(candidate.Issues) > agentCompilationMaxIssuesPerCandidate {
			return fmt.Errorf(
				"candidates[%d].issues must not exceed %d items",
				index,
				agentCompilationMaxIssuesPerCandidate,
			)
		}
		if len(candidate.NextActions) > agentCompilationMaxNextActions {
			return fmt.Errorf(
				"candidates[%d].next_actions must not exceed %d items",
				index,
				agentCompilationMaxNextActions,
			)
		}
		for issueIndex, issue := range candidate.Issues {
			if strings.TrimSpace(issue.Code) == "" {
				return fmt.Errorf("candidates[%d].issues[%d].code is required", index, issueIndex)
			}
			if strings.TrimSpace(issue.Message) == "" {
				return fmt.Errorf("candidates[%d].issues[%d].message is required", index, issueIndex)
			}
			if utf8.RuneCountInString(issue.Message) > agentCompilationMaxIssueMessageRunes {
				return fmt.Errorf(
					"candidates[%d].issues[%d].message exceeds %d characters",
					index,
					issueIndex,
					agentCompilationMaxIssueMessageRunes,
				)
			}
		}
		switch candidate.Status {
		case AgentCompilationCandidateReady:
			readyCount++
			if candidate.Package == nil {
				return fmt.Errorf("ready candidates[%d] requires a package", index)
			}
			if len(candidate.Issues) != 0 {
				return fmt.Errorf("ready candidates[%d] must not contain issues", index)
			}
		case AgentCompilationCandidateBlocked:
			blockedCount++
			if candidate.Package != nil {
				return fmt.Errorf("blocked candidates[%d] must not contain a package", index)
			}
			if len(candidate.Issues) == 0 {
				return fmt.Errorf("blocked candidates[%d].issues is required", index)
			}
		default:
			return fmt.Errorf("candidates[%d].status is invalid", index)
		}
	}

	expectedStatus := AgentCompilationStatusPartial
	switch {
	case readyCount == len(compilation.Candidates):
		expectedStatus = AgentCompilationStatusReady
	case blockedCount == len(compilation.Candidates):
		expectedStatus = AgentCompilationStatusBlocked
	}
	if compilation.Status != expectedStatus {
		return fmt.Errorf(
			"status %q does not agree with candidate status; expected %q",
			compilation.Status,
			expectedStatus,
		)
	}
	return nil
}
