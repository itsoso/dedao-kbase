package app

import (
	"reflect"
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

func TestCompileAgentPackagesDualBuildsDeterministicCandidates(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"干预能改善结局",
		"Publisher Primary",
		"dedao_ebook",
	)
	support := agentCompilerTestRelease(
		"release-support",
		"book-support",
		"2026-07-26T11:00:00Z",
		"干预能改善结局",
		"Publisher Support",
		"wechat_mp_article",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)
	saveKnowledgeAssemblyRelease(t, store, support)
	request := AgentCompilationRequest{
		SchemaVersion:        AgentCompilationRequestSchemaVersion,
		Mode:                 AgentCompilationModeDual,
		PrimaryReleaseID:     primary.ReleaseID,
		SupportingReleaseIDs: []string{support.ReleaseID},
		Version:              "1.2.0",
	}
	requestBefore := request
	requestBefore.SupportingReleaseIDs = append([]string(nil), request.SupportingReleaseIDs...)
	primaryBefore, err := store.LoadKnowledgeRelease(primary.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	supportBefore, err := store.LoadKnowledgeRelease(support.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}

	first, err := CompileAgentPackages(store, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileAgentPackages(store, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != AgentCompilationStatusReady ||
		len(first.Candidates) != 2 ||
		first.Candidates[0].Kind != AgentCompilationCandidateStudy ||
		first.Candidates[1].Kind != AgentCompilationCandidateEvidence {
		t.Fatalf("dual compilation = %#v", first)
	}
	for index, candidate := range first.Candidates {
		if candidate.Status != AgentCompilationCandidateReady || candidate.Package == nil {
			t.Fatalf("candidate[%d] = %#v", index, candidate)
		}
		if err := ValidateAgentPackage(*candidate.Package, store, AgentReadOnlyToolIDs()); err != nil {
			t.Fatalf("candidate[%d] package invalid: %v", index, err)
		}
	}
	if first.CompilationID != second.CompilationID ||
		first.AssemblyID != second.AssemblyID ||
		first.Candidates[0].Package.ContentHash != second.Candidates[0].Package.ContentHash ||
		first.Candidates[1].Package.ContentHash != second.Candidates[1].Package.ContentHash {
		t.Fatalf("dual compilation is not deterministic: first=%#v second=%#v", first, second)
	}
	if !reflect.DeepEqual(request, requestBefore) {
		t.Fatalf("compiler mutated request: before=%#v after=%#v", requestBefore, request)
	}
	primaryAfter, err := store.LoadKnowledgeRelease(primary.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	supportAfter, err := store.LoadKnowledgeRelease(support.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(primaryBefore, primaryAfter) ||
		!reflect.DeepEqual(supportBefore, supportAfter) {
		t.Fatalf("compiler mutated releases")
	}
}

func TestCompileAgentPackagesDualKeepsStudyReadyWithoutSupport(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"单一来源结论",
		"Publisher Primary",
		"dedao_ebook",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeDual,
		PrimaryReleaseID: primary.ReleaseID,
		Version:          "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusPartial || len(result.Candidates) != 2 {
		t.Fatalf("dual compilation = %#v", result)
	}
	if result.Candidates[0].Kind != AgentCompilationCandidateStudy ||
		result.Candidates[0].Status != AgentCompilationCandidateReady ||
		result.Candidates[0].Package == nil {
		t.Fatalf("study candidate = %#v", result.Candidates[0])
	}
	evidence := result.Candidates[1]
	if evidence.Kind != AgentCompilationCandidateEvidence ||
		evidence.Status != AgentCompilationCandidateBlocked ||
		evidence.Package != nil ||
		len(evidence.Issues) != 1 ||
		evidence.Issues[0].Code != AgentCompilationIssueSupportingReleaseRequired {
		t.Fatalf("evidence candidate = %#v", evidence)
	}
}

func agentCompilerTestRelease(
	releaseID, bookID, createdAt, statement, publisher, sourceType string,
) KnowledgeRelease {
	release := knowledgeAssemblyTestRelease(
		releaseID,
		bookID,
		createdAt,
		statement,
		publisher,
		sourceType,
	)
	release.UsagePolicy = BookUsageStandard
	release.Quality.UsagePolicy = BookUsageStandard
	return release
}
