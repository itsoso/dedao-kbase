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

func TestRouteResearchModeKeepsExplicitQuickButRequiresDeepForChatlog(t *testing.T) {
	request := ResearchRunRequest{
		Mode:             ResearchModeQuick,
		Question:         "Summarize the earlier discussion.",
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
			name: "unknown source",
			request: ResearchRunRequest{
				Mode: ResearchModeAuto, Question: "question",
				RequestedSources: []string{"private_database"},
			},
			want: "unsupported requested source",
		},
		{
			name: "oversized question",
			request: ResearchRunRequest{
				Mode: ResearchModeAuto, Question: strings.Repeat("问", researchQuestionMaxRunes+1),
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
		ResearchBuildingTimeline,
		ResearchExtractingFacts,
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

func containsResearchReason(reasons []string, target string) bool {
	for _, reason := range reasons {
		if reason == target {
			return true
		}
	}
	return false
}
