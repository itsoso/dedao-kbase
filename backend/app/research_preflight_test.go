package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateResearchPreflightRequestNormalizesBoundedScope(t *testing.T) {
	request, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
		Mode:              " auto ",
		Question:          "  compare evidence  ",
		RequestedSources:  []string{"knowledge", "chatlog"},
		PackageConstraint: "  collection-agent  ",
		ParentRunID:       "  research-run-parent  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Question != "compare evidence" || request.Mode != ResearchModeAuto {
		t.Fatalf("normalized request = %#v", request)
	}
	if request.PackageConstraint != "collection-agent" || request.ParentRunID != "research-run-parent" {
		t.Fatalf("normalized optional scope = %#v", request)
	}
}

func TestValidateResearchPreflightRequestUsesStableSourceOrdering(t *testing.T) {
	request, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
		Question:         "compare evidence",
		RequestedSources: []string{" prior_runs ", "chatlog", " knowledge "},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{ResearchSourceKnowledge, ResearchSourceChatlog, ResearchSourcePriorRuns}
	if len(request.RequestedSources) != len(want) {
		t.Fatalf("requested_sources = %#v", request.RequestedSources)
	}
	for index := range want {
		if request.RequestedSources[index] != want[index] {
			t.Fatalf("requested_sources = %#v, want %#v", request.RequestedSources, want)
		}
	}
}

func TestValidateResearchPreflightRequestRejectsHardBounds(t *testing.T) {
	tests := []struct {
		name    string
		request ResearchPreflightRequest
		want    string
	}{
		{
			name:    "question required",
			request: ResearchPreflightRequest{Question: "  "},
			want:    "question is required",
		},
		{
			name:    "question bounded",
			request: ResearchPreflightRequest{Question: strings.Repeat("q", researchQuestionMaxRunes+1)},
			want:    "question exceeds",
		},
		{
			name: "sources bounded",
			request: ResearchPreflightRequest{
				Question:         "compare evidence",
				RequestedSources: make([]string, researchRequestedSourcesMax+1),
			},
			want: "requested_sources exceeds",
		},
		{
			name: "duplicate source rejected",
			request: ResearchPreflightRequest{
				Question:         "compare evidence",
				RequestedSources: []string{"knowledge", " knowledge "},
			},
			want: "duplicate requested source",
		},
		{
			name: "unknown source rejected",
			request: ResearchPreflightRequest{
				Question:         "compare evidence",
				RequestedSources: []string{"global"},
			},
			want: "unsupported requested source",
		},
		{
			name:    "constraint bounded",
			request: ResearchPreflightRequest{Question: "compare evidence", PackageConstraint: strings.Repeat("p", researchPackageIDMaxRunes+1)},
			want:    "package_constraint exceeds",
		},
		{
			name:    "mode allowlisted",
			request: ResearchPreflightRequest{Mode: "wide", Question: "compare evidence"},
			want:    "mode must be auto, quick, or deep",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeResearchPreflightRequest(test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateResearchPreflightRequestEnforcesParentRunIDBoundary(t *testing.T) {
	t.Run("exact maximum allowed", func(t *testing.T) {
		parentRunID := strings.Repeat("r", researchPackageIDMaxRunes)
		request, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
			Question:    "compare evidence",
			ParentRunID: parentRunID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if request.ParentRunID != parentRunID {
			t.Fatalf("parent_run_id length = %d", len([]rune(request.ParentRunID)))
		}
	})

	t.Run("over maximum rejected", func(t *testing.T) {
		_, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
			Question:    "compare evidence",
			ParentRunID: strings.Repeat("r", researchPackageIDMaxRunes+1),
		})
		if err == nil || !strings.Contains(err.Error(), "parent_run_id exceeds") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestValidateResearchPreflightRejectsMoreThanThreeCandidates(t *testing.T) {
	preflight := testResearchPreflight()
	preflight.Candidates = append(preflight.Candidates,
		ResearchPreflightCandidate{PackageID: "candidate-b", PackageVersion: "1.0.0", MatchLevel: ResearchPreflightMatchMedium},
		ResearchPreflightCandidate{PackageID: "candidate-c", PackageVersion: "1.0.0", MatchLevel: ResearchPreflightMatchLow},
		ResearchPreflightCandidate{PackageID: "candidate-d", PackageVersion: "1.0.0", MatchLevel: ResearchPreflightMatchLow},
	)
	if err := ValidateResearchPreflight(preflight); err == nil || !strings.Contains(err.Error(), "candidates exceeds 3 items") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateResearchPreflightRejectsUnknownDecisionStates(t *testing.T) {
	preflight := testResearchPreflight()
	preflight.Candidates[0].MatchLevel = "exact"
	if err := ValidateResearchPreflight(preflight); err == nil || !strings.Contains(err.Error(), "unsupported match level") {
		t.Fatalf("match level error = %v", err)
	}

	preflight = testResearchPreflight()
	preflight.Checks[0].Status = "unknown"
	if err := ValidateResearchPreflight(preflight); err == nil || !strings.Contains(err.Error(), "unsupported check status") {
		t.Fatalf("check status error = %v", err)
	}
}

func TestValidateResearchPreflightEnforcesCandidateAndReadyConsistency(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ResearchPreflight)
		want   string
	}{
		{
			name: "candidate package id required",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates[0].PackageID = "  "
			},
			want: "candidate 0 package_id is required",
		},
		{
			name: "candidate package version required",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates[0].PackageVersion = ""
			},
			want: "candidate 0 package_version is required",
		},
		{
			name: "candidate content hash required",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates[0].ContentHash = ""
			},
			want: "candidate 0 content_hash is required",
		},
		{
			name: "ready requires candidate",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates = nil
			},
			want: "ready preflight requires a confirmable candidate",
		},
		{
			name: "ready rejects blocked global check",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Checks[0].Status = ResearchPreflightCheckBlocked
			},
			want: "ready preflight cannot contain blocked check",
		},
		{
			name: "ready rejects all candidates blocked",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates[0].Readiness = ResearchPreflightCheckBlocked
			},
			want: "ready preflight cannot have all candidates blocked",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preflight := testResearchPreflight()
			test.mutate(&preflight)
			if err := ValidateResearchPreflight(preflight); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateResearchPreflightAllowsApprovedBlockedAndWarningShapes(t *testing.T) {
	t.Run("ready warning candidate", func(t *testing.T) {
		preflight := testResearchPreflight()
		preflight.Candidates[0].Readiness = ResearchPreflightCheckWarning
		if err := ValidateResearchPreflight(preflight); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("blocked without candidates", func(t *testing.T) {
		preflight := testResearchPreflight()
		preflight.Status = ResearchPreflightStatusBlocked
		preflight.Candidates = nil
		if err := ValidateResearchPreflight(preflight); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("blocked with identified candidate", func(t *testing.T) {
		preflight := testResearchPreflight()
		preflight.Status = ResearchPreflightStatusBlocked
		preflight.Candidates[0].Readiness = ResearchPreflightCheckBlocked
		if err := ValidateResearchPreflight(preflight); err != nil {
			t.Fatal(err)
		}
	})
}

func TestResearchPreflightProjectionContainsNoPrivateBodies(t *testing.T) {
	encoded, err := json.Marshal(PublicResearchPreflight(testResearchPreflight()))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"content_excerpt", "message_ref", "local_path", "identity_id",
		"request_hash", "content_hash", "private-request-hash", "private-content-hash",
	} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("projection leaks %s", forbidden)
		}
	}
}

func TestResearchPreflightInternalProjectionPreservesContentHash(t *testing.T) {
	encoded, err := json.Marshal(testResearchPreflight())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"content_hash":"private-content-hash"`)) {
		t.Fatalf("internal projection lost content hash: %s", encoded)
	}
}

func testResearchPreflight() ResearchPreflight {
	return ResearchPreflight{
		PreflightID: "research-preflight-a",
		RequestHash: "private-request-hash",
		Status:      ResearchPreflightStatusReady,
		Candidates: []ResearchPreflightCandidate{{
			PackageID:        "candidate-a",
			PackageVersion:   "1.0.0",
			ContentHash:      "private-content-hash",
			DisplayName:      "Collection research Agent",
			MatchLevel:       ResearchPreflightMatchHigh,
			ReasonCodes:      []string{"topic_match"},
			KnowledgeScope:   []string{"authorized_collection"},
			UpdatedAt:        "2026-08-19T12:00:00Z",
			EvaluationStatus: "passed",
			SupportedSources: []string{ResearchSourceKnowledge},
			Readiness:        ResearchPreflightCheckPass,
		}},
		Checks: []ResearchPreflightCheck{{
			Code:       "package_ready",
			Status:     ResearchPreflightCheckPass,
			Message:    "Package is eligible",
			NextAction: "confirm_package",
		}},
		Gaps: []ResearchPreflightGap{{
			Code:       "optional_context",
			Message:    "More context may improve coverage",
			NextAction: "add_context",
		}},
		ParentRunID: "research-run-parent",
		CreatedAt:   "2026-08-19T12:00:00Z",
		ExpiresAt:   "2026-08-19T12:10:00Z",
	}
}
