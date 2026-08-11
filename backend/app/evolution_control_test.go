package app

import (
	"encoding/json"
	"math"
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

func TestEvolutionRunTransitionRejectionMatrix(t *testing.T) {
	known := []EvolutionRunStatus{
		EvolutionDetected,
		EvolutionTriaged,
		EvolutionGenerating,
		EvolutionEvaluating,
		EvolutionAwaitingApproval,
		EvolutionApproved,
		EvolutionPublishing,
		EvolutionObserving,
		EvolutionCompleted,
		EvolutionBlocked,
		EvolutionRejected,
		EvolutionFailed,
		EvolutionSuperseded,
		EvolutionRolledBack,
	}
	terminal := []EvolutionRunStatus{
		EvolutionBlocked,
		EvolutionRejected,
		EvolutionFailed,
		EvolutionCompleted,
		EvolutionSuperseded,
		EvolutionRolledBack,
	}

	for _, from := range terminal {
		for _, to := range known {
			if err := ValidateEvolutionTransition(from, to); err == nil {
				t.Fatalf("terminal transition %s -> %s accepted", from, to)
			}
		}
	}
	for _, status := range known {
		if err := ValidateEvolutionTransition(status, status); err == nil {
			t.Fatalf("self transition %s accepted", status)
		}
	}
	if err := ValidateEvolutionTransition("unknown", EvolutionDetected); err == nil {
		t.Fatal("unknown source status accepted")
	}
	if err := ValidateEvolutionTransition(EvolutionDetected, "unknown"); err == nil {
		t.Fatal("unknown target status accepted")
	}

	bypasses := [][2]EvolutionRunStatus{
		{EvolutionDetected, EvolutionApproved},
		{EvolutionDetected, EvolutionPublishing},
		{EvolutionGenerating, EvolutionAwaitingApproval},
		{EvolutionEvaluating, EvolutionApproved},
		{EvolutionAwaitingApproval, EvolutionPublishing},
		{EvolutionApproved, EvolutionObserving},
		{EvolutionPublishing, EvolutionCompleted},
	}
	for _, transition := range bypasses {
		if err := ValidateEvolutionTransition(transition[0], transition[1]); err == nil {
			t.Fatalf("approval bypass %s -> %s accepted", transition[0], transition[1])
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
				record := validEvolutionRun()
				record.FailureMessage = strings.Repeat("界", EvolutionFailureMessageMaxRunes+1)
				return record.Validate()
			},
		},
		{
			name: "candidate change summary",
			validate: func() error {
				record := validEvolutionCandidate()
				record.ChangeSummary = strings.Repeat("界", EvolutionChangeSummaryMaxRunes+1)
				return record.Validate()
			},
		},
		{
			name: "approval note",
			validate: func() error {
				record := validEvolutionApproval()
				record.Note = strings.Repeat("界", EvolutionApprovalNoteMaxRunes+1)
				return record.Validate()
			},
		},
		{
			name: "event message",
			validate: func() error {
				record := validEvolutionEvent()
				record.Message = strings.Repeat("界", EvolutionEventMessageMaxRunes+1)
				return record.Validate()
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

func TestEvolutionControlRecordsAcceptUnicodeTextAtLimit(t *testing.T) {
	run := validEvolutionRun()
	run.FailureMessage = strings.Repeat("🧠", EvolutionFailureMessageMaxRunes)
	candidate := validEvolutionCandidate()
	candidate.ChangeSummary = strings.Repeat("知", EvolutionChangeSummaryMaxRunes)
	approval := validEvolutionApproval()
	approval.Note = strings.Repeat("🧭", EvolutionApprovalNoteMaxRunes)
	event := validEvolutionEvent()
	event.Message = strings.Repeat("验", EvolutionEventMessageMaxRunes)

	for _, record := range []struct {
		name     string
		validate func() error
	}{
		{name: "run", validate: run.Validate},
		{name: "candidate", validate: candidate.Validate},
		{name: "approval", validate: approval.Validate},
		{name: "event", validate: event.Validate},
	} {
		t.Run(record.name, func(t *testing.T) {
			if err := record.validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEvolutionControlRecordValidationContract(t *testing.T) {
	for _, record := range []struct {
		name     string
		validate func() error
	}{
		{name: "signal", validate: validEvolutionSignal().Validate},
		{name: "run", validate: validEvolutionRun().Validate},
		{name: "candidate", validate: validEvolutionCandidate().Validate},
		{name: "scorecard", validate: validEvolutionScorecard().Validate},
		{name: "approval", validate: validEvolutionApproval().Validate},
		{name: "observation", validate: validEvolutionObservation().Validate},
		{name: "event", validate: validEvolutionEvent().Validate},
	} {
		t.Run(record.name, func(t *testing.T) {
			if err := record.validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEvolutionControlRecordValidationRejectsUnboundedValues(t *testing.T) {
	tests := []struct {
		name     string
		validate func() error
	}{
		{name: "signal evidence count", validate: func() error {
			record := validEvolutionSignal()
			record.EvidenceRefs = makeEvolutionReferences(EvolutionCollectionMaxItems + 1)
			return record.Validate()
		}},
		{name: "signal overlong identity", validate: func() error {
			record := validEvolutionSignal()
			record.SignalID = strings.Repeat("s", EvolutionIdentityMaxRunes+1)
			return record.Validate()
		}},
		{name: "signal prose evidence", validate: func() error {
			record := validEvolutionSignal()
			record.EvidenceRefs = []string{"unrestricted evidence body"}
			return record.Validate()
		}},
		{name: "run signal count", validate: func() error {
			record := validEvolutionRun()
			record.TriggerSignalIDs = makeEvolutionReferences(EvolutionCollectionMaxItems + 1)
			return record.Validate()
		}},
		{name: "scorecard hard gate count", validate: func() error {
			record := validEvolutionScorecard()
			record.HardGates = makeEvolutionHardGates(EvolutionMetricMaxItems + 1)
			return record.Validate()
		}},
		{name: "scorecard metric name", validate: func() error {
			record := validEvolutionScorecard()
			record.Metrics = map[string]float64{strings.Repeat("m", EvolutionCodeMaxRunes+1): 1}
			return record.Validate()
		}},
		{name: "scorecard metric count", validate: func() error {
			record := validEvolutionScorecard()
			record.Metrics = makeEvolutionMetrics(EvolutionMetricMaxItems + 1)
			return record.Validate()
		}},
		{name: "scorecard metric nan", validate: func() error {
			record := validEvolutionScorecard()
			record.Metrics["quality"] = math.NaN()
			return record.Validate()
		}},
		{name: "scorecard failure ref", validate: func() error {
			record := validEvolutionScorecard()
			record.FailureCaseRefs = []string{strings.Repeat("r", EvolutionReferenceMaxRunes+1)}
			return record.Validate()
		}},
		{name: "scorecard failure count", validate: func() error {
			record := validEvolutionScorecard()
			record.FailureCaseRefs = makeEvolutionReferences(EvolutionCollectionMaxItems + 1)
			return record.Validate()
		}},
		{name: "candidate artifact prose", validate: func() error {
			record := validEvolutionCandidate()
			record.ArtifactRef = "embedded artifact body"
			return record.Validate()
		}},
		{name: "approval reason prose", validate: func() error {
			record := validEvolutionApproval()
			record.ReasonCode = "not safe because it contains prose"
			return record.Validate()
		}},
		{name: "approval missing scorecard", validate: func() error {
			record := validEvolutionApproval()
			record.ScorecardID = ""
			return record.Validate()
		}},
		{name: "observation incident count", validate: func() error {
			record := validEvolutionObservation()
			record.HardGateIncidents = makeEvolutionReferences(EvolutionCollectionMaxItems + 1)
			return record.Validate()
		}},
		{name: "observation metric infinity", validate: func() error {
			record := validEvolutionObservation()
			record.Metrics["latency"] = math.Inf(1)
			return record.Validate()
		}},
		{name: "event artifact ref", validate: func() error {
			record := validEvolutionEvent()
			record.ArtifactRefs = []string{"reference with unrestricted prose"}
			return record.Validate()
		}},
		{name: "event artifact count", validate: func() error {
			record := validEvolutionEvent()
			record.ArtifactRefs = makeEvolutionReferences(EvolutionCollectionMaxItems + 1)
			return record.Validate()
		}},
		{name: "event missing actor", validate: func() error {
			record := validEvolutionEvent()
			record.Actor = ""
			return record.Validate()
		}},
		{name: "missing required time", validate: func() error {
			record := validEvolutionObservation()
			record.WindowStart = ""
			return record.Validate()
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.validate(); err == nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
}

func TestEvolutionRunValidationRequiresScopeAndLineage(t *testing.T) {
	tests := []struct {
		name string
		run  EvolutionRun
	}{
		{name: "agent package", run: func() EvolutionRun {
			run := validEvolutionRun()
			run.PackageID = ""
			return run
		}()},
		{name: "agent baseline", run: func() EvolutionRun {
			run := validEvolutionRun()
			run.BaselinePackageVersion = ""
			return run
		}()},
		{name: "trigger signal", run: func() EvolutionRun {
			run := validEvolutionRun()
			run.TriggerSignalIDs = nil
			return run
		}()},
		{name: "retry lineage", run: func() EvolutionRun {
			run := validEvolutionRun()
			run.Attempt = 2
			run.RetryOfRunID = ""
			return run
		}()},
		{name: "first attempt parent", run: func() EvolutionRun {
			run := validEvolutionRun()
			run.RetryOfRunID = "run-parent"
			return run
		}()},
		{name: "retry self reference", run: func() EvolutionRun {
			run := validEvolutionRun()
			run.Attempt = 2
			run.RetryOfRunID = run.RunID
			return run
		}()},
		{name: "zero attempt", run: func() EvolutionRun {
			run := validEvolutionRun()
			run.Attempt = 0
			return run
		}()},
		{name: "knowledge baseline", run: func() EvolutionRun {
			run := validEvolutionRun()
			run.RunType = EvolutionRunKnowledgeRelease
			run.PackageID = ""
			run.BaselinePackageVersion = ""
			run.BaselineReleaseIDs = nil
			return run
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run.Validate(); err == nil {
				t.Fatal("run without required scope or lineage accepted")
			}
		})
	}
}

func TestEvolutionEventTransitionContract(t *testing.T) {
	initial := validEvolutionEvent()
	initial.EventType = "created"
	initial.FromStatus = ""
	initial.ToStatus = EvolutionDetected
	if err := initial.Validate(); err != nil {
		t.Fatalf("initial event: %v", err)
	}

	transition := validEvolutionEvent()
	if err := transition.Validate(); err != nil {
		t.Fatalf("transition event: %v", err)
	}

	note := validEvolutionEvent()
	note.EventType = "note"
	note.FromStatus = ""
	note.ToStatus = ""
	note.Message = "bounded operator note"
	if err := note.Validate(); err != nil {
		t.Fatalf("non-transition event: %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*EvolutionEvent)
	}{
		{name: "approval bypass", mutate: func(event *EvolutionEvent) {
			event.FromStatus = EvolutionDetected
			event.ToStatus = EvolutionPublishing
		}},
		{name: "missing source", mutate: func(event *EvolutionEvent) {
			event.FromStatus = ""
			event.ToStatus = EvolutionTriaged
		}},
		{name: "missing target", mutate: func(event *EvolutionEvent) {
			event.FromStatus = EvolutionDetected
			event.ToStatus = ""
		}},
		{name: "self transition", mutate: func(event *EvolutionEvent) {
			event.FromStatus = EvolutionDetected
			event.ToStatus = EvolutionDetected
		}},
		{name: "empty non-transition message", mutate: func(event *EvolutionEvent) {
			event.EventType = "note"
			event.FromStatus = ""
			event.ToStatus = ""
			event.Message = ""
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := validEvolutionEvent()
			test.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("invalid event transition accepted")
			}
		})
	}
}

func TestEvolutionRecordTimeOrdering(t *testing.T) {
	runEqual := validEvolutionRun()
	runEqual.UpdatedAt = runEqual.CreatedAt
	if err := runEqual.Validate(); err != nil {
		t.Fatalf("equal run timestamps: %v", err)
	}

	for _, test := range []struct {
		name     string
		validate func() error
	}{
		{name: "run reversed", validate: func() error {
			record := validEvolutionRun()
			record.CreatedAt = "2026-08-11T13:00:00Z"
			record.UpdatedAt = "2026-08-11T12:00:00Z"
			return record.Validate()
		}},
		{name: "approval equal", validate: func() error {
			record := validEvolutionApproval()
			record.ExpiresAt = record.CreatedAt
			return record.Validate()
		}},
		{name: "approval reversed", validate: func() error {
			record := validEvolutionApproval()
			record.ExpiresAt = "2026-08-11T11:00:00Z"
			return record.Validate()
		}},
		{name: "observation equal", validate: func() error {
			record := validEvolutionObservation()
			record.WindowEnd = record.WindowStart
			return record.Validate()
		}},
		{name: "observation reversed", validate: func() error {
			record := validEvolutionObservation()
			record.WindowEnd = "2026-08-11T11:00:00Z"
			return record.Validate()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.validate(); err == nil {
				t.Fatal("invalid time order accepted")
			}
		})
	}
}

func TestEvolutionValidationErrorsAreStable(t *testing.T) {
	approval := validEvolutionApproval()
	approval.ApprovalID = ""
	approval.RunID = ""

	scorecard := validEvolutionScorecard()
	scorecard.Metrics = map[string]float64{
		"bad metric": math.NaN(),
		"also bad":   math.Inf(1),
	}

	for _, record := range []struct {
		name     string
		validate func() error
	}{
		{name: "ordered fields", validate: approval.Validate},
		{name: "ordered map keys", validate: scorecard.Validate},
	} {
		t.Run(record.name, func(t *testing.T) {
			first := record.validate()
			if first == nil {
				t.Fatal("invalid record accepted")
			}
			for iteration := 0; iteration < 100; iteration++ {
				next := record.validate()
				if next == nil || next.Error() != first.Error() {
					t.Fatalf("unstable error: first=%v next=%v", first, next)
				}
			}
		})
	}
}

func TestEvolutionApprovalAndEventJSONContract(t *testing.T) {
	approvalPayload, err := json.Marshal(validEvolutionApproval())
	if err != nil {
		t.Fatal(err)
	}
	eventPayload, err := json.Marshal(validEvolutionEvent())
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		payload []byte
		field   string
		want    string
	}{
		{payload: approvalPayload, field: "scorecard_id", want: "scorecard-1"},
		{payload: eventPayload, field: "actor", want: "operator"},
	} {
		var record map[string]any
		if err := json.Unmarshal(check.payload, &record); err != nil {
			t.Fatal(err)
		}
		if record[check.field] != check.want {
			t.Fatalf("%s = %#v", check.field, record[check.field])
		}
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
				RetryOfRunID:           "run-before-original",
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

func TestEvolutionExplicitRetryRejectsInvalidBoundaries(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 30, 0, 0, time.UTC)
	valid := validEvolutionRun()
	valid.Status = EvolutionBlocked
	valid.Attempt = 3
	valid.RetryOfRunID = "run-before-original"

	for _, test := range []struct {
		name     string
		mutate   func(*EvolutionRun)
		newRunID string
		now      time.Time
	}{
		{name: "blank original id", mutate: func(run *EvolutionRun) { run.RunID = "  " }, newRunID: "run-retry", now: now},
		{name: "blank new id", mutate: func(run *EvolutionRun) {}, newRunID: "  ", now: now},
		{name: "same trimmed id", mutate: func(run *EvolutionRun) {}, newRunID: "  run-1  ", now: now},
		{name: "unknown run type", mutate: func(run *EvolutionRun) { run.RunType = "unknown" }, newRunID: "run-retry", now: now},
		{name: "negative attempt", mutate: func(run *EvolutionRun) { run.Attempt = -1 }, newRunID: "run-retry", now: now},
		{name: "attempt overflow", mutate: func(run *EvolutionRun) { run.Attempt = int(^uint(0) >> 1) }, newRunID: "run-retry", now: now},
		{name: "zero time", mutate: func(run *EvolutionRun) {}, newRunID: "run-retry", now: time.Time{}},
		{name: "nan priority", mutate: func(run *EvolutionRun) { run.PriorityScore = math.NaN() }, newRunID: "run-retry", now: now},
		{name: "infinite priority", mutate: func(run *EvolutionRun) { run.PriorityScore = math.Inf(1) }, newRunID: "run-retry", now: now},
		{name: "missing lineage scope", mutate: func(run *EvolutionRun) { run.TriggerSignalIDs = nil }, newRunID: "run-retry", now: now},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := valid
			test.mutate(&run)
			if _, err := NewEvolutionRetry(run, test.newRunID, test.now); err == nil {
				t.Fatal("invalid explicit retry accepted")
			}
		})
	}
}

func TestEvolutionExplicitRetryTrimsIdentityAndReturnsValidRecord(t *testing.T) {
	original := validEvolutionRun()
	original.RunID = "  run-original  "
	original.Status = EvolutionCompleted
	original.Attempt = 1

	retry, err := NewEvolutionRetry(original, "  run-retry  ", time.Date(2026, time.August, 11, 12, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if retry.RunID != "run-retry" || retry.RetryOfRunID != "run-original" {
		t.Fatalf("trimmed retry identity = %q/%q", retry.RunID, retry.RetryOfRunID)
	}
	if err := retry.Validate(); err != nil {
		t.Fatalf("retry record is not persistable: %v", err)
	}
}

func TestEvolutionExplicitRetryNormalizesLegacyZeroAttempt(t *testing.T) {
	original := validEvolutionRun()
	original.RunType = EvolutionRunKnowledgeRelease
	original.PackageID = ""
	original.BaselinePackageVersion = ""
	original.BaselineReleaseIDs = []string{"release-1"}
	original.Status = EvolutionBlocked
	original.Attempt = 0
	retry, err := NewEvolutionRetry(original, "run-retry", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if retry.Attempt != 2 {
		t.Fatalf("legacy retry attempt = %d, want 2", retry.Attempt)
	}
}

func validEvolutionSignal() EvolutionSignal {
	return EvolutionSignal{
		SignalID:         "signal-1",
		SignalType:       "quality_regression",
		SourceType:       "runtime_audit",
		SourceID:         "source-1",
		PackageID:        "package-1",
		ReleaseID:        "release-1",
		Severity:         "p1",
		ObservedValue:    0.72,
		BaselineValue:    0.91,
		DeduplicationKey: "quality_regression:package-1",
		EvidenceRefs:     []string{"artifact:evidence-1"},
		ObservedAt:       "2026-08-11T12:00:00Z",
	}
}

func validEvolutionRun() EvolutionRun {
	return EvolutionRun{
		RunID:                  "run-1",
		Attempt:                1,
		RunType:                EvolutionRunCombined,
		PackageID:              "package-1",
		BaselinePackageVersion: "1.0.0",
		BaselineReleaseIDs:     []string{"release-1"},
		RiskLevel:              "p1",
		PriorityScore:          80,
		Status:                 EvolutionDetected,
		TriggerSignalIDs:       []string{"signal-1"},
		CreatedAt:              "2026-08-11T12:00:00Z",
		UpdatedAt:              "2026-08-11T12:00:00Z",
	}
}

func validEvolutionCandidate() EvolutionCandidate {
	return EvolutionCandidate{
		CandidateID:      "candidate-1",
		RunID:            "run-1",
		CandidateType:    "combined",
		ContentHash:      "sha256:abc123",
		ArtifactRef:      "artifact:candidate-1",
		BaselineIdentity: "package-1@1.0.0+release-1",
		ChangeSummary:    "bounded summary",
		GeneratorVersion: "generator-1",
		CreatedAt:        "2026-08-11T12:00:00Z",
	}
}

func validEvolutionScorecard() EvolutionScorecard {
	return EvolutionScorecard{
		ScorecardID:      "scorecard-1",
		CandidateID:      "candidate-1",
		BaselineIdentity: "package-1@1.0.0+release-1",
		SuiteVersion:     "suite-1",
		ScorerVersion:    "scorer-1",
		HardGates:        map[string]bool{"privacy": true},
		Metrics:          map[string]float64{"quality": 92},
		WeightedScore:    92,
		BaselineScore:    85,
		Delta:            7,
		Decision:         "awaiting_approval",
		FailureCaseRefs:  []string{"artifact:failure-1"},
	}
}

func validEvolutionApproval() EvolutionApproval {
	return EvolutionApproval{
		ApprovalID:           "approval-1",
		RunID:                "run-1",
		CandidateID:          "candidate-1",
		CandidateContentHash: "sha256:abc123",
		BaselineIdentity:     "package-1@1.0.0+release-1",
		ScorecardID:          "scorecard-1",
		Decision:             "approved",
		ReasonCode:           "verified_improvement",
		Note:                 "bounded note",
		ApprovedBy:           "operator-1",
		CreatedAt:            "2026-08-11T12:00:00Z",
		ExpiresAt:            "2026-08-12T12:00:00Z",
	}
}

func validEvolutionObservation() EvolutionObservation {
	return EvolutionObservation{
		ObservationID:     "observation-1",
		RunID:             "run-1",
		PublishedIdentity: "package-1@1.1.0",
		WindowStart:       "2026-08-11T12:00:00Z",
		WindowEnd:         "2026-08-12T12:00:00Z",
		Metrics:           map[string]float64{"quality": 94},
		HardGateIncidents: []string{"incident:privacy-clear"},
		Outcome:           "improved",
		RollbackIdentity:  "package-1@1.0.0",
	}
}

func validEvolutionEvent() EvolutionEvent {
	return EvolutionEvent{
		EventID:      "event-1",
		RunID:        "run-1",
		EventType:    "transition",
		Actor:        "operator",
		FromStatus:   EvolutionDetected,
		ToStatus:     EvolutionTriaged,
		Code:         "triaged",
		Message:      "bounded event message",
		ArtifactRefs: []string{"artifact:event-1"},
		CreatedAt:    "2026-08-11T12:00:00Z",
	}
}

func makeEvolutionReferences(count int) []string {
	refs := make([]string, count)
	for index := range refs {
		refs[index] = "ref:" + strings.Repeat("x", index%10+1)
	}
	return refs
}

func makeEvolutionHardGates(count int) map[string]bool {
	gates := make(map[string]bool, count)
	for index := 0; index < count; index++ {
		gates["gate-"+strings.Repeat("x", index+1)] = true
	}
	return gates
}

func makeEvolutionMetrics(count int) map[string]float64 {
	metrics := make(map[string]float64, count)
	for index := 0; index < count; index++ {
		metrics["metric-"+strings.Repeat("x", index+1)] = float64(index)
	}
	return metrics
}
