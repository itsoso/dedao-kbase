package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvolutionRunTransitionContract(t *testing.T) {
	allowed := [][2]EvolutionRunStatus{
		{EvolutionDetected, EvolutionTriaged},
		{EvolutionTriaged, EvolutionGenerating},
		{EvolutionGenerating, EvolutionEvaluating},
		{EvolutionEvaluating, EvolutionAwaitingApproval},
		{EvolutionAwaitingApproval, EvolutionApproved},
		{EvolutionApproved, EvolutionPublishing},
		{EvolutionPublishing, EvolutionObserving},
		{EvolutionObserving, EvolutionCompleted},
	}
	for _, transition := range allowed {
		if err := ValidateEvolutionTransition(transition[0], transition[1]); err != nil {
			t.Fatalf("transition %s -> %s: %v", transition[0], transition[1], err)
		}
	}

	if err := ValidateEvolutionTransition(EvolutionDetected, EvolutionPublishing); err == nil {
		t.Fatal("detected run skipped approval")
	}
}

func TestEvolutionTerminalAndSideStatesCannotResumeInPlace(t *testing.T) {
	terminal := []EvolutionRunStatus{
		EvolutionBlocked,
		EvolutionRejected,
		EvolutionFailed,
		EvolutionCompleted,
		EvolutionSuperseded,
		EvolutionRolledBack,
	}
	for _, status := range terminal {
		if err := ValidateEvolutionTransition(status, EvolutionTriaged); err == nil {
			t.Fatalf("terminal run %s resumed in place", status)
		}
	}
}

func TestEvolutionControlRecordsBoundPublicText(t *testing.T) {
	tests := []struct {
		name     string
		validate func() error
	}{
		{
			name: "run failure message",
			validate: func() error {
				return (EvolutionRun{
					RunType:        EvolutionRunAgentPolicy,
					Status:         EvolutionFailed,
					FailureMessage: strings.Repeat("界", EvolutionFailureMessageMaxRunes+1),
				}).Validate()
			},
		},
		{
			name: "candidate change summary",
			validate: func() error {
				return (EvolutionCandidate{ChangeSummary: strings.Repeat("界", EvolutionChangeSummaryMaxRunes+1)}).Validate()
			},
		},
		{
			name: "approval note",
			validate: func() error {
				return (EvolutionApproval{Note: strings.Repeat("界", EvolutionApprovalNoteMaxRunes+1)}).Validate()
			},
		},
		{
			name: "event message",
			validate: func() error {
				return (EvolutionEvent{Message: strings.Repeat("界", EvolutionEventMessageMaxRunes+1)}).Validate()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.validate(); err == nil {
				t.Fatal("overlong public text accepted")
			}
		})
	}
}

func TestEvolutionCandidateJSONContainsReferenceNotArtifactBody(t *testing.T) {
	payload, err := json.Marshal(EvolutionCandidate{
		CandidateID:   "candidate-1",
		ArtifactRef:   "sha256:artifact",
		ChangeSummary: "更新检索策略",
	})
	if err != nil {
		t.Fatal(err)
	}

	var record map[string]any
	if err := json.Unmarshal(payload, &record); err != nil {
		t.Fatal(err)
	}
	if record["artifact_ref"] != "sha256:artifact" {
		t.Fatalf("artifact_ref = %#v", record["artifact_ref"])
	}
	for _, forbidden := range []string{"artifact_body", "body", "content", "payload"} {
		if _, ok := record[forbidden]; ok {
			t.Fatalf("candidate embeds forbidden %q field", forbidden)
		}
	}
}
