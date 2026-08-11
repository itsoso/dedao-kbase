package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestEvolutionExplicitRetryCreatesNewAttempt(t *testing.T) {
	terminal := []EvolutionRunStatus{
		EvolutionBlocked,
		EvolutionRejected,
		EvolutionFailed,
		EvolutionCompleted,
		EvolutionSuperseded,
		EvolutionRolledBack,
	}
	now := time.Date(2026, time.August, 11, 12, 30, 0, 123, time.FixedZone("test", 2*60*60))

	for _, status := range terminal {
		t.Run(string(status), func(t *testing.T) {
			original := EvolutionRun{
				RunID:                  "run-original",
				RunType:                EvolutionRunCombined,
				PackageID:              "package-1",
				BaselinePackageVersion: "1.2.3",
				BaselineReleaseIDs:     []string{"release-1", "release-2"},
				RiskLevel:              "p1",
				PriorityScore:          82.5,
				Status:                 status,
				Attempt:                3,
				TriggerSignalIDs:       []string{"signal-1", "signal-2"},
				CurrentCandidateID:     "candidate-old",
				FailureCode:            "retry_exhausted",
				FailureMessage:         "previous attempt stopped",
				CreatedAt:              "2026-08-10T00:00:00Z",
				UpdatedAt:              "2026-08-10T01:00:00Z",
			}
			before := original
			before.BaselineReleaseIDs = append([]string(nil), original.BaselineReleaseIDs...)
			before.TriggerSignalIDs = append([]string(nil), original.TriggerSignalIDs...)

			retry, err := NewEvolutionRetry(original, "run-retry", now)
			if err != nil {
				t.Fatal(err)
			}
			if retry.RunID != "run-retry" || retry.RetryOfRunID != original.RunID {
				t.Fatalf("retry identity = %#v", retry)
			}
			if retry.Attempt != 4 || retry.Status != EvolutionDetected {
				t.Fatalf("retry attempt/status = %d/%s", retry.Attempt, retry.Status)
			}
			if retry.RunType != original.RunType || retry.PackageID != original.PackageID ||
				retry.BaselinePackageVersion != original.BaselinePackageVersion ||
				retry.RiskLevel != original.RiskLevel || retry.PriorityScore != original.PriorityScore ||
				!reflect.DeepEqual(retry.BaselineReleaseIDs, original.BaselineReleaseIDs) ||
				!reflect.DeepEqual(retry.TriggerSignalIDs, original.TriggerSignalIDs) {
				t.Fatalf("retry lost run scope: %#v", retry)
			}
			if retry.CurrentCandidateID != "" || retry.FailureCode != "" || retry.FailureMessage != "" {
				t.Fatalf("retry copied execution state: %#v", retry)
			}
			expectedTime := now.UTC().Format(time.RFC3339Nano)
			if retry.CreatedAt != expectedTime || retry.UpdatedAt != expectedTime {
				t.Fatalf("retry timestamps = %q/%q", retry.CreatedAt, retry.UpdatedAt)
			}
			if !reflect.DeepEqual(original, before) {
				t.Fatalf("original mutated: before=%#v after=%#v", before, original)
			}

			retry.BaselineReleaseIDs[0] = "changed"
			retry.TriggerSignalIDs[0] = "changed"
			if !reflect.DeepEqual(original, before) {
				t.Fatal("retry aliases original slices")
			}
		})
	}
}

func TestEvolutionExplicitRetryValidatesIdentityAndStatus(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 30, 0, 0, time.UTC)
	original := EvolutionRun{
		RunID:   "run-original",
		RunType: EvolutionRunAgentPolicy,
		Status:  EvolutionBlocked,
	}

	for _, test := range []struct {
		name     string
		run      EvolutionRun
		newRunID string
	}{
		{name: "active run", run: EvolutionRun{RunID: "run-active", RunType: EvolutionRunAgentPolicy, Status: EvolutionEvaluating}, newRunID: "run-retry"},
		{name: "empty identity", run: original, newRunID: ""},
		{name: "reused identity", run: original, newRunID: original.RunID},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewEvolutionRetry(test.run, test.newRunID, now); err == nil {
				t.Fatal("invalid explicit retry accepted")
			}
		})
	}
}

func TestEvolutionExplicitRetryNormalizesLegacyZeroAttempt(t *testing.T) {
	retry, err := NewEvolutionRetry(EvolutionRun{
		RunID:   "run-original",
		RunType: EvolutionRunKnowledgeRelease,
		Status:  EvolutionBlocked,
		Attempt: 0,
	}, "run-retry", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if retry.Attempt != 2 {
		t.Fatalf("legacy retry attempt = %d, want 2", retry.Attempt)
	}
}
