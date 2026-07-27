package app

import (
	"strings"
	"testing"
)

func TestValidateAgentCompilationRequest(t *testing.T) {
	valid := AgentCompilationRequest{
		SchemaVersion:        AgentCompilationRequestSchemaVersion,
		Mode:                 AgentCompilationModeDual,
		PrimaryReleaseID:     "release-primary",
		SupportingReleaseIDs: []string{"release-support"},
		Version:              "1.0.0",
	}
	for _, mode := range []string{
		AgentCompilationModeDual,
		AgentCompilationModeEvidence,
		AgentCompilationModeStudy,
	} {
		request := valid
		request.Mode = mode
		if err := ValidateAgentCompilationRequest(request); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}

	tests := []struct {
		name   string
		mutate func(*AgentCompilationRequest)
		want   string
	}{
		{
			name: "schema",
			mutate: func(request *AgentCompilationRequest) {
				request.SchemaVersion = "agent-compilation-request.v0"
			},
			want: "schema_version",
		},
		{
			name: "mode",
			mutate: func(request *AgentCompilationRequest) {
				request.Mode = "automatic"
			},
			want: "mode",
		},
		{
			name: "primary release",
			mutate: func(request *AgentCompilationRequest) {
				request.PrimaryReleaseID = " "
			},
			want: "primary_release_id",
		},
		{
			name: "version",
			mutate: func(request *AgentCompilationRequest) {
				request.Version = "latest"
			},
			want: "version",
		},
		{
			name: "duplicate support",
			mutate: func(request *AgentCompilationRequest) {
				request.SupportingReleaseIDs = []string{"release-support", "release-support"}
			},
			want: "duplicate",
		},
		{
			name: "primary repeated as support",
			mutate: func(request *AgentCompilationRequest) {
				request.SupportingReleaseIDs = []string{"release-primary"}
			},
			want: "primary",
		},
		{
			name: "support bound",
			mutate: func(request *AgentCompilationRequest) {
				request.SupportingReleaseIDs = make([]string, agentCompilationMaxSupportingReleases+1)
				for index := range request.SupportingReleaseIDs {
					request.SupportingReleaseIDs[index] = "release-support-" + string(rune('a'+index))
				}
			},
			want: "supporting_release_ids",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := valid
			request.SupportingReleaseIDs = append([]string(nil), valid.SupportingReleaseIDs...)
			testCase.mutate(&request)
			err := ValidateAgentCompilationRequest(request)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validation error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestValidateAgentCompilationContract(t *testing.T) {
	readyPackage := validAgentPackage()
	compilation := AgentCompilation{
		SchemaVersion:   AgentCompilationSchemaVersion,
		CompilerVersion: AgentCompilerVersion,
		CompilationID:   "compilation-fixture",
		Mode:            AgentCompilationModeDual,
		AssemblyID:      "assembly-fixture",
		ReleaseIDs:      []string{"release-1"},
		Status:          AgentCompilationStatusPartial,
		Candidates: []AgentCompilationCandidate{
			{
				Kind:        AgentCompilationCandidateStudy,
				Status:      AgentCompilationCandidateReady,
				Package:     &readyPackage,
				NextActions: []string{AgentCompilationNextActionEvaluate},
			},
			{
				Kind:   AgentCompilationCandidateEvidence,
				Status: AgentCompilationCandidateBlocked,
				Issues: []AgentCompilationIssue{{
					Code:    AgentCompilationIssueSupportingReleaseRequired,
					Message: "An independently eligible supporting release is required.",
				}},
				NextActions: []string{AgentCompilationNextActionSelectSupport},
			},
		},
	}
	if err := ValidateAgentCompilation(compilation); err != nil {
		t.Fatalf("valid compilation rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AgentCompilation)
		want   string
	}{
		{
			name: "candidate bound",
			mutate: func(value *AgentCompilation) {
				value.Candidates = append(value.Candidates, AgentCompilationCandidate{
					Kind:   AgentCompilationCandidateStudy,
					Status: AgentCompilationCandidateBlocked,
					Issues: []AgentCompilationIssue{{Code: "blocked", Message: "blocked"}},
				})
			},
			want: "candidates",
		},
		{
			name: "duplicate kind",
			mutate: func(value *AgentCompilation) {
				value.Candidates[1].Kind = AgentCompilationCandidateStudy
			},
			want: "duplicate",
		},
		{
			name: "ready requires package",
			mutate: func(value *AgentCompilation) {
				value.Candidates[0].Package = nil
			},
			want: "package",
		},
		{
			name: "blocked excludes package",
			mutate: func(value *AgentCompilation) {
				pkg := readyPackage
				value.Candidates[1].Package = &pkg
			},
			want: "blocked",
		},
		{
			name: "blocked requires issue",
			mutate: func(value *AgentCompilation) {
				value.Candidates[1].Issues = nil
			},
			want: "issues",
		},
		{
			name: "bounded issue message",
			mutate: func(value *AgentCompilation) {
				value.Candidates[1].Issues[0].Message = strings.Repeat("界", agentCompilationMaxIssueMessageRunes+1)
			},
			want: "message",
		},
		{
			name: "status agreement",
			mutate: func(value *AgentCompilation) {
				value.Status = AgentCompilationStatusReady
			},
			want: "status",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			value := cloneAgentCompilationForTest(t, compilation)
			testCase.mutate(&value)
			err := ValidateAgentCompilation(value)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validation error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func cloneAgentCompilationForTest(t *testing.T, value AgentCompilation) AgentCompilation {
	t.Helper()
	cloned := value
	cloned.ReleaseIDs = append([]string(nil), value.ReleaseIDs...)
	cloned.Candidates = make([]AgentCompilationCandidate, len(value.Candidates))
	for index, candidate := range value.Candidates {
		cloned.Candidates[index] = candidate
		cloned.Candidates[index].Issues = append([]AgentCompilationIssue(nil), candidate.Issues...)
		cloned.Candidates[index].NextActions = append([]string(nil), candidate.NextActions...)
		if candidate.Package != nil {
			pkg := *candidate.Package
			cloned.Candidates[index].Package = &pkg
		}
	}
	return cloned
}
