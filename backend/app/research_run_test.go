package app

import (
	"errors"
	"strings"
	"testing"
)

func TestRouteResearchModeChoosesDeepForPrivateHistory(t *testing.T) {
	request := ResearchRunRequest{
		Mode:             ResearchModeAuto,
		Question:         "Compare the current case with an earlier case.",
		PackageID:        "research-agent",
		PackageVersion:   "1.0.0",
		RequestedSources: []string{ResearchSourceKnowledge, ResearchSourceChatlog},
	}

	mode, reasons, err := RouteResearchMode(request)
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResearchModeDeep || !containsResearchReason(reasons, ResearchRoutePrivateHistory) {
		t.Fatalf("mode=%q reasons=%v", mode, reasons)
	}
}

func TestRouteResearchModeChoosesDeepForPriorRuns(t *testing.T) {
	request := ResearchRunRequest{
		Question:         "总结以前的研究结论",
		PackageID:        "research-agent",
		PackageVersion:   "1.0.0",
		RequestedSources: []string{ResearchSourcePriorRuns},
	}
	mode, reasons, err := RouteResearchMode(request)
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResearchModeDeep || !containsResearchReason(reasons, ResearchRoutePriorResearch) {
		t.Fatalf("expected prior runs to route deep, got mode=%q reasons=%v", mode, reasons)
	}
	if err := ValidateResearchModeScope(ResearchRunRequest{
		Mode:             ResearchModeQuick,
		Question:         request.Question,
		PackageID:        request.PackageID,
		PackageVersion:   request.PackageVersion,
		RequestedSources: request.RequestedSources,
	}, ResearchModeQuick); !errors.Is(err, ErrResearchDeepRequired) {
		t.Fatalf("expected explicit quick prior-runs request to require deep mode, got %v", err)
	}
}

func TestRouteResearchModeKeepsExplicitQuickButRequiresDeepForChatlog(t *testing.T) {
	request := ResearchRunRequest{
		Mode:             ResearchModeQuick,
		Question:         "Summarize the earlier discussion.",
		PackageID:        "research-agent",
		PackageVersion:   "1.0.0",
		RequestedSources: []string{ResearchSourceChatlog},
	}

	mode, reasons, err := RouteResearchMode(request)
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResearchModeQuick || !containsResearchReason(reasons, ResearchRouteExplicitQuick) {
		t.Fatalf("mode=%q reasons=%v", mode, reasons)
	}
	if err := ValidateResearchModeScope(request, mode); !errors.Is(err, ErrResearchDeepRequired) {
		t.Fatalf("scope error=%v", err)
	}
}

func TestRouteResearchModeChoosesQuickForBoundedKnowledgeQuestion(t *testing.T) {
	mode, reasons, err := RouteResearchMode(ResearchRunRequest{
		Mode:             ResearchModeAuto,
		Question:         "What does the selected collection say about sleep?",
		PackageID:        "research-agent",
		PackageVersion:   "1.0.0",
		RequestedSources: []string{ResearchSourceKnowledge},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResearchModeQuick || !containsResearchReason(reasons, ResearchRouteBoundedKnowledge) {
		t.Fatalf("mode=%q reasons=%v", mode, reasons)
	}
}

func TestValidateResearchRunRequestRejectsUnknownSourceAndOversizedQuestion(t *testing.T) {
	tests := []struct {
		name    string
		request ResearchRunRequest
		want    string
	}{
		{
			name: "non canonical preflight",
			request: ResearchRunRequest{
				Mode: ResearchModeAuto, Question: "question", PackageID: "research-agent", PackageVersion: "1.0.0",
				PreflightID: "/private/preflight", RequestedSources: []string{ResearchSourceKnowledge},
			},
			want: "preflight_id",
		},
		{
			name: "missing package scope",
			request: ResearchRunRequest{
				Mode: ResearchModeAuto, Question: "question", PreflightID: "research-preflight-valid",
				RequestedSources: []string{ResearchSourceKnowledge},
			},
			want: "package_id and package_version",
		},
		{
			name: "unknown source",
			request: ResearchRunRequest{
				Mode: ResearchModeAuto, Question: "question", PackageID: "research-agent", PackageVersion: "1.0.0",
				PreflightID:      "research-preflight-valid",
				RequestedSources: []string{"private_database"},
			},
			want: "unsupported requested source",
		},
		{
			name: "oversized question",
			request: ResearchRunRequest{
				Mode: ResearchModeAuto, Question: strings.Repeat("问", researchQuestionMaxRunes+1), PackageID: "research-agent", PackageVersion: "1.0.0",
				PreflightID:      "research-preflight-valid",
				RequestedSources: []string{ResearchSourceKnowledge},
			},
			want: "question exceeds",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateResearchRunRequest(test.request); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestValidateResearchTransitionSupportsDeepAndQuickPaths(t *testing.T) {
	deep := []ResearchRunStatus{
		ResearchPlanning,
		ResearchRetrieving,
		ResearchResolvingIdentity,
		ResearchExtractingFacts,
		ResearchBuildingTimeline,
		ResearchDetectingConflicts,
		ResearchComparingCases,
		ResearchSynthesizing,
		ResearchVerifying,
		ResearchCompleted,
	}
	for index := 0; index+1 < len(deep); index++ {
		if err := ValidateResearchTransition(deep[index], deep[index+1]); err != nil {
			t.Fatalf("deep transition %q -> %q: %v", deep[index], deep[index+1], err)
		}
	}
	for _, transition := range [][2]ResearchRunStatus{
		{ResearchRetrieving, ResearchSynthesizing},
		{ResearchSynthesizing, ResearchVerifying},
		{ResearchVerifying, ResearchPlanning},
		{ResearchVerifying, ResearchInsufficient},
	} {
		if err := ValidateResearchTransition(transition[0], transition[1]); err != nil {
			t.Fatalf("transition %q -> %q: %v", transition[0], transition[1], err)
		}
	}
}

func TestValidateResearchTransitionRejectsSkippedAndTerminalTransitions(t *testing.T) {
	if err := ValidateResearchTransition(ResearchPlanning, ResearchCompleted); err == nil {
		t.Fatal("planning run skipped verification")
	}
	for _, terminal := range []ResearchRunStatus{
		ResearchCompleted, ResearchInsufficient, ResearchFailed, ResearchCanceled,
	} {
		if err := ValidateResearchTransition(terminal, ResearchPlanning); err == nil {
			t.Fatalf("terminal status %q resumed", terminal)
		}
	}
}

func TestValidateResearchTransitionAllowsTypedInsufficiencyFromEveryActiveStage(t *testing.T) {
	for _, status := range []ResearchRunStatus{
		ResearchPlanning, ResearchRetrieving, ResearchResolvingIdentity, ResearchBuildingTimeline,
		ResearchExtractingFacts, ResearchDetectingConflicts, ResearchComparingCases,
		ResearchSynthesizing, ResearchVerifying,
	} {
		if err := ValidateResearchTransition(status, ResearchInsufficient); err != nil {
			t.Fatalf("status=%s error=%v", status, err)
		}
	}
}

func containsResearchReason(reasons []string, target string) bool {
	for _, reason := range reasons {
		if reason == target {
			return true
		}
	}
	return false
}
