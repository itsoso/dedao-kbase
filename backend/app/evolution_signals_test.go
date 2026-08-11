package app

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvolutionSignalIdempotencyReplayAndConflict(t *testing.T) {
	store := newEvolutionTestStore(t)
	input := validEvolutionSignalInput("signal-replay")

	signal, run, created, err := store.IngestSignal(input)
	if err != nil || !created || signal == nil || run == nil {
		t.Fatalf("first ingest = %#v, %#v, %v, %v", signal, run, created, err)
	}
	replayedSignal, replayedRun, created, err := store.IngestSignal(input)
	if err != nil || created {
		t.Fatalf("replay = %#v, %#v, %v, %v", replayedSignal, replayedRun, created, err)
	}
	if replayedSignal.SignalID != signal.SignalID || replayedRun.RunID != run.RunID {
		t.Fatalf("replay identities = %q/%q, want %q/%q", replayedSignal.SignalID, replayedRun.RunID, signal.SignalID, run.RunID)
	}

	changed := input
	changed.Severity = EvolutionSignalSeverityCritical
	if _, _, _, err := store.IngestSignal(changed); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	assertEvolutionSignalCounts(t, store, 1, 1, 1)
}

func TestEvolutionSignalIdempotencyFingerprintCoversEveryPayloadField(t *testing.T) {
	store := newEvolutionTestStore(t)
	base := validEvolutionSignalInput("all-fields-conflict")
	base.ReleaseID = "release-base"
	base.EvidenceRefs = []string{"evaluation:" + testEvolutionOpaqueID("a"), "metric:regression_pass_rate"}
	if _, _, _, err := store.IngestSignal(base); err != nil {
		t.Fatal(err)
	}

	mutations := []struct {
		name   string
		mutate func(*EvolutionSignalInput)
	}{
		{name: "signal type", mutate: func(input *EvolutionSignalInput) { input.SignalType = EvolutionSignalToolFailure }},
		{name: "source type", mutate: func(input *EvolutionSignalInput) { input.SourceType = EvolutionSignalSourceObservation }},
		{name: "source ID", mutate: func(input *EvolutionSignalInput) { input.SourceID = testEvolutionOpaqueID("b") }},
		{name: "package ID", mutate: func(input *EvolutionSignalInput) { input.PackageID = "research-assistant-v2" }},
		{name: "release ID", mutate: func(input *EvolutionSignalInput) { input.ReleaseID = "release-changed" }},
		{name: "severity", mutate: func(input *EvolutionSignalInput) { input.Severity = EvolutionSignalSeverityCritical }},
		{name: "observed value", mutate: func(input *EvolutionSignalInput) { input.ObservedValue = 0.41 }},
		{name: "baseline value", mutate: func(input *EvolutionSignalInput) { input.BaselineValue = 0.81 }},
		{name: "evidence refs", mutate: func(input *EvolutionSignalInput) {
			input.EvidenceRefs = []string{"evaluation:" + testEvolutionOpaqueID("c")}
		}},
		{name: "observed at", mutate: func(input *EvolutionSignalInput) { input.ObservedAt = input.ObservedAt.Add(time.Second) }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			changed := base
			changed.EvidenceRefs = append([]string{}, base.EvidenceRefs...)
			mutation.mutate(&changed)
			if _, _, _, err := store.IngestSignal(changed); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
				t.Fatalf("changed payload error = %v", err)
			}
		})
	}

	equivalent := base
	equivalent.EvidenceRefs = []string{base.EvidenceRefs[1], base.EvidenceRefs[0]}
	if _, _, created, err := store.IngestSignal(equivalent); err != nil || created {
		t.Fatalf("canonical evidence replay = %v, %v", created, err)
	}
	assertEvolutionSignalCounts(t, store, 1, 1, 1)
}

func TestEvolutionSignalDeduplicatedRequestKeepsItsOwnIdempotencyFingerprint(t *testing.T) {
	store := newEvolutionTestStore(t)
	first := validEvolutionSignalInput("dedup-fingerprint-first")
	_, run, _, err := store.IngestSignal(first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.IdempotencyKey = "dedup-fingerprint-second"
	second.ObservedValue = 0.41
	signal, aggregatedRun, created, err := store.IngestSignal(second)
	if err != nil || created || aggregatedRun.RunID != run.RunID {
		t.Fatalf("deduplicated request = %#v, %#v, %v, %v", signal, aggregatedRun, created, err)
	}
	replayedSignal, replayedRun, created, err := store.IngestSignal(second)
	if err != nil || created || replayedSignal.SignalID != signal.SignalID || replayedRun.RunID != run.RunID {
		t.Fatalf("deduplicated replay = %#v, %#v, %v, %v", replayedSignal, replayedRun, created, err)
	}
	changed := second
	changed.ObservedValue = 0.5
	if _, _, _, err := store.IngestSignal(changed); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("deduplicated changed replay error = %v", err)
	}
	assertEvolutionSignalCounts(t, store, 1, 1, 2)
}

func TestEvolutionSignalCooldownAggregatesAndThenCreatesNewRun(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store := newEvolutionSignalTestStore(t, func() time.Time { return now })
	first := validEvolutionSignalInput("cooldown-first")
	first.ObservedAt = now
	signal, run, created, err := store.IngestSignal(first)
	if err != nil || !created {
		t.Fatalf("first ingest = %#v, %#v, %v, %v", signal, run, created, err)
	}

	now = now.Add(time.Hour)
	second := first
	second.IdempotencyKey = "cooldown-second"
	second.ObservedAt = now
	second.ObservedValue = 0.42
	aggregatedSignal, aggregatedRun, created, err := store.IngestSignal(second)
	if err != nil || created {
		t.Fatalf("cooldown aggregate = %#v, %#v, %v, %v", aggregatedSignal, aggregatedRun, created, err)
	}
	if aggregatedSignal.SignalID != signal.SignalID || aggregatedRun.RunID != run.RunID {
		t.Fatalf("cooldown created duplicate identities: signal=%q run=%q", aggregatedSignal.SignalID, aggregatedRun.RunID)
	}
	if aggregatedRun.UpdatedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("aggregated updated_at = %q", aggregatedRun.UpdatedAt)
	}
	if got := fmt.Sprint(aggregatedRun.TriggerSignalIDs); got != fmt.Sprintf("[%s]", signal.SignalID) {
		t.Fatalf("deduplicated trigger IDs = %s", got)
	}
	assertEvolutionSignalCounts(t, store, 1, 1, 2)

	now = now.Add(EvolutionSignalCooldown + time.Nanosecond)
	third := second
	third.IdempotencyKey = "cooldown-third"
	third.ObservedAt = now
	_, nextRun, created, err := store.IngestSignal(third)
	if err != nil || created {
		t.Fatalf("post-cooldown ingest = %#v, %v, %v", nextRun, created, err)
	}
	if nextRun.RunID == run.RunID {
		t.Fatal("post-cooldown signal reused stale run")
	}
	assertEvolutionSignalCounts(t, store, 1, 2, 3)
}

func TestEvolutionSignalCrossStoreReplayAndAggregationDoNotDuplicateRuns(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	stores := []*EvolutionControlStore{
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
		openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{}),
	}
	input := validEvolutionSignalInput("cross-store-replay")
	input.ObservedAt = now
	type result struct {
		signal  *EvolutionSignal
		run     *EvolutionRun
		created bool
		err     error
	}
	results := make(chan result, len(stores))
	var start sync.WaitGroup
	start.Add(1)
	for _, store := range stores {
		go func(store *EvolutionControlStore) {
			start.Wait()
			signal, run, created, err := store.IngestSignal(input)
			results <- result{signal: signal, run: run, created: created, err: err}
		}(store)
	}
	start.Done()
	first := <-results
	second := <-results
	for _, got := range []result{first, second} {
		if got.err != nil {
			t.Fatalf("cross-store replay: %v", got.err)
		}
	}
	if first.created == second.created || first.signal.SignalID != second.signal.SignalID || first.run.RunID != second.run.RunID {
		t.Fatalf("cross-store replay results = %#v / %#v", first, second)
	}
	assertEvolutionSignalCounts(t, stores[0], 1, 1, 1)

	aggregationResults := make(chan result, len(stores))
	start.Add(1)
	for index, store := range stores {
		go func(index int, store *EvolutionControlStore) {
			start.Wait()
			related := input
			related.IdempotencyKey = fmt.Sprintf("cross-store-aggregate-%d", index)
			related.ObservedValue += float64(index+1) / 100
			signal, run, created, err := store.IngestSignal(related)
			aggregationResults <- result{signal: signal, run: run, created: created, err: err}
		}(index, store)
	}
	start.Done()
	for range stores {
		got := <-aggregationResults
		if got.err != nil || got.created || got.run.RunID != first.run.RunID {
			t.Fatalf("cross-store aggregation = %#v", got)
		}
	}
	assertEvolutionSignalCounts(t, stores[0], 1, 1, 3)
}

func TestEvolutionSignalReplayAndAggregationSurviveStoreReopen(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store, err := OpenEvolutionControlStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	input := validEvolutionSignalInput("durable-first")
	input.ObservedAt = now
	signal, run, created, err := store.IngestSignal(input)
	if err != nil || !created {
		t.Fatalf("first ingest = %#v, %#v, %v, %v", signal, run, created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenEvolutionControlStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	replayedSignal, replayedRun, created, err := store.IngestSignal(input)
	if err != nil || created || replayedSignal.SignalID != signal.SignalID || replayedRun.RunID != run.RunID {
		t.Fatalf("reopen replay = %#v, %#v, %v, %v", replayedSignal, replayedRun, created, err)
	}
	assertEvolutionSignalCounts(t, store, 1, 1, 1)

	now = now.Add(time.Hour)
	related := input
	related.IdempotencyKey = "durable-related"
	related.ObservedAt = now
	related.ObservedValue = 0.41
	_, aggregatedRun, created, err := store.IngestSignal(related)
	if err != nil || created || aggregatedRun.RunID != run.RunID {
		t.Fatalf("reopen aggregate = %#v, %v, %v", aggregatedRun, created, err)
	}
	assertEvolutionSignalCounts(t, store, 1, 1, 2)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenEvolutionControlStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, durableRun, created, err := store.IngestSignal(related)
	if err != nil || created || durableRun.RunID != run.RunID {
		t.Fatalf("event replay after reopen = %#v, %v, %v", durableRun, created, err)
	}
	assertEvolutionSignalCounts(t, store, 1, 1, 2)
}

func TestEvolutionSignalEventFailureRollsBackSignalRunAndMapping(t *testing.T) {
	injected := errors.New("event insert failed")
	store := newEvolutionTestStoreWithHooks(t, evolutionStoreHooks{
		beforeEventInsert: func(EvolutionEvent) error { return injected },
	})
	input := validEvolutionSignalInput("atomic-signal")
	if _, _, _, err := store.IngestSignal(input); !errors.Is(err, injected) {
		t.Fatalf("ingest error = %v", err)
	}
	assertEvolutionSignalCounts(t, store, 0, 0, 0)
	var mappings int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_events WHERE event_id = ?`, evolutionSignalRequestEventID(input.IdempotencyKey)).Scan(&mappings); err != nil {
		t.Fatal(err)
	}
	if mappings != 0 {
		t.Fatalf("idempotency mappings = %d", mappings)
	}
}

func TestEvolutionSignalTerminalRunIsNotReopened(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store := newEvolutionSignalTestStore(t, func() time.Time { return now })
	input := validEvolutionSignalInput("terminal-first")
	input.ObservedAt = now
	_, run, _, err := store.IngestSignal(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionRun(run.RunID, EvolutionBlocked, EvolutionTransitionInput{Actor: "operator", Code: "blocked"}); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Minute)
	input.IdempotencyKey = "terminal-second"
	input.ObservedAt = now
	_, nextRun, _, err := store.IngestSignal(input)
	if err != nil {
		t.Fatal(err)
	}
	if nextRun.RunID == run.RunID || nextRun.Status != EvolutionDetected {
		t.Fatalf("terminal run was resumed: %#v", nextRun)
	}
}

func TestEvolutionSignalRelatedAgentAndKnowledgeBecomeCombined(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store := newEvolutionSignalTestStore(t, func() time.Time { return now })
	agent := validEvolutionSignalInput("combined-agent")
	agent.ObservedAt = now
	agentSignal, agentRun, _, err := store.IngestSignal(agent)
	if err != nil {
		t.Fatal(err)
	}
	if agentRun.RunType != EvolutionRunAgentPolicy {
		t.Fatalf("agent run type = %q", agentRun.RunType)
	}

	now = now.Add(time.Minute)
	knowledge := EvolutionSignalInput{
		IdempotencyKey: "combined-knowledge",
		SignalType:     EvolutionSignalReleaseStale,
		SourceType:     EvolutionSignalSourceKnowledgeStore,
		SourceID:       "release-2026-08",
		PackageID:      agent.PackageID,
		ReleaseID:      "release-2026-08",
		Severity:       EvolutionSignalSeverityHigh,
		ObservedValue:  31,
		BaselineValue:  30,
		EvidenceRefs:   []string{"release:release-2026-08"},
		ObservedAt:     now,
	}
	knowledgeSignal, combined, created, err := store.IngestSignal(knowledge)
	if err != nil || !created {
		t.Fatalf("knowledge ingest = %#v, %#v, %v, %v", knowledgeSignal, combined, created, err)
	}
	if combined.RunID != agentRun.RunID || combined.RunType != EvolutionRunCombined {
		t.Fatalf("combined run = %#v, original = %#v", combined, agentRun)
	}
	if got, want := fmt.Sprint(combined.TriggerSignalIDs), fmt.Sprint([]string{agentSignal.SignalID, knowledgeSignal.SignalID}); got != want {
		t.Fatalf("trigger IDs = %s, want %s", got, want)
	}
	if got := fmt.Sprint(combined.BaselineReleaseIDs); got != "[release-2026-08]" {
		t.Fatalf("release identity = %s", got)
	}
}

func TestEvolutionSignalUnrelatedIdentitiesDoNotAggregateOrCombine(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store := newEvolutionSignalTestStore(t, func() time.Time { return now })
	firstAgent := validEvolutionSignalInput("identity-agent-a")
	firstAgent.ObservedAt = now
	_, agentRunA, _, err := store.IngestSignal(firstAgent)
	if err != nil {
		t.Fatal(err)
	}
	secondAgent := firstAgent
	secondAgent.IdempotencyKey = "identity-agent-b"
	secondAgent.PackageID = "different-agent"
	_, agentRunB, _, err := store.IngestSignal(secondAgent)
	if err != nil {
		t.Fatal(err)
	}
	if agentRunA.RunID == agentRunB.RunID || agentRunB.RunType != EvolutionRunAgentPolicy {
		t.Fatalf("different packages aggregated: %#v / %#v", agentRunA, agentRunB)
	}

	knowledgeA := validEvolutionKnowledgeSignalInput("identity-release-a", "release-a", "knowledge-agent")
	knowledgeA.ObservedAt = now
	_, knowledgeRunA, _, err := store.IngestSignal(knowledgeA)
	if err != nil {
		t.Fatal(err)
	}
	knowledgeB := validEvolutionKnowledgeSignalInput("identity-release-b", "release-b", "knowledge-agent")
	knowledgeB.ObservedAt = now
	_, knowledgeRunB, _, err := store.IngestSignal(knowledgeB)
	if err != nil {
		t.Fatal(err)
	}
	if knowledgeRunA.RunID == knowledgeRunB.RunID || knowledgeRunB.RunType != EvolutionRunKnowledgeRelease {
		t.Fatalf("different releases aggregated: %#v / %#v", knowledgeRunA, knowledgeRunB)
	}
}

func TestEvolutionSignalPriorityIsStableAndExplainable(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	low, err := CalculateEvolutionPriority(EvolutionPriorityInput{
		Risk: 0.2, Impact: 0.3, ExpectedBenefit: 0.4, WaitingSince: now.Add(-24 * time.Hour), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	high, err := CalculateEvolutionPriority(EvolutionPriorityInput{
		Risk: 0.8, Impact: 0.7, ExpectedBenefit: 0.9, WaitingSince: now.Add(-72 * time.Hour), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if high <= low {
		t.Fatalf("priority high=%v, low=%v", high, low)
	}
	if again, err := CalculateEvolutionPriority(EvolutionPriorityInput{
		Risk: 0.8, Impact: 0.7, ExpectedBenefit: 0.9, WaitingSince: now.Add(-72 * time.Hour), Now: now,
	}); err != nil || again != high {
		t.Fatalf("priority is not deterministic: %v, %v", again, err)
	}
	if _, err := CalculateEvolutionPriority(EvolutionPriorityInput{Risk: 1.1, Now: now, WaitingSince: now}); err == nil {
		t.Fatal("priority accepted risk outside [0,1]")
	}
}

func TestEvolutionSignalPriorityRejectsInvalidBoundariesAndAcceptsEdges(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	invalid := []struct {
		name  string
		input EvolutionPriorityInput
	}{
		{name: "nan", input: EvolutionPriorityInput{Risk: math.NaN(), WaitingSince: now, Now: now}},
		{name: "positive infinity", input: EvolutionPriorityInput{Impact: math.Inf(1), WaitingSince: now, Now: now}},
		{name: "negative infinity", input: EvolutionPriorityInput{ExpectedBenefit: math.Inf(-1), WaitingSince: now, Now: now}},
		{name: "negative risk", input: EvolutionPriorityInput{Risk: -0.01, WaitingSince: now, Now: now}},
		{name: "risk over one", input: EvolutionPriorityInput{Risk: 1.01, WaitingSince: now, Now: now}},
		{name: "negative impact", input: EvolutionPriorityInput{Impact: -0.01, WaitingSince: now, Now: now}},
		{name: "impact over one", input: EvolutionPriorityInput{Impact: 1.01, WaitingSince: now, Now: now}},
		{name: "negative benefit", input: EvolutionPriorityInput{ExpectedBenefit: -0.01, WaitingSince: now, Now: now}},
		{name: "benefit over one", input: EvolutionPriorityInput{ExpectedBenefit: 1.01, WaitingSince: now, Now: now}},
		{name: "future waiting", input: EvolutionPriorityInput{WaitingSince: now.Add(time.Second), Now: now}},
		{name: "zero waiting since", input: EvolutionPriorityInput{Now: now}},
		{name: "zero now", input: EvolutionPriorityInput{WaitingSince: now}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CalculateEvolutionPriority(test.input); err == nil {
				t.Fatal("invalid priority was accepted")
			}
		})
	}
	if score, err := CalculateEvolutionPriority(EvolutionPriorityInput{
		Risk: 1, Impact: 1, ExpectedBenefit: 1, WaitingSince: now.Add(-7 * 24 * time.Hour), Now: now,
	}); err != nil || score != 100 {
		t.Fatalf("upper boundary = %v, %v", score, err)
	}
	if score, err := CalculateEvolutionPriority(EvolutionPriorityInput{WaitingSince: now, Now: now}); err != nil || score != 0 {
		t.Fatalf("lower boundary = %v, %v", score, err)
	}
}

func TestEvolutionSignalRejectsUnboundedOrSensitiveInputs(t *testing.T) {
	store := newEvolutionTestStore(t)
	tests := []struct {
		name   string
		mutate func(*EvolutionSignalInput)
	}{
		{name: "user body", mutate: func(input *EvolutionSignalInput) { input.SourceID = "the user said private words" }},
		{name: "cookie", mutate: func(input *EvolutionSignalInput) { input.SourceID = "cookie=session-secret" }},
		{name: "token query", mutate: func(input *EvolutionSignalInput) { input.EvidenceRefs = []string{"trace:case?token=secret"} }},
		{name: "absolute path", mutate: func(input *EvolutionSignalInput) { input.EvidenceRefs = []string{"/private/data.json"} }},
		{name: "windows absolute path", mutate: func(input *EvolutionSignalInput) { input.SourceID = "C:/private/data.json" }},
		{name: "shell source", mutate: func(input *EvolutionSignalInput) { input.SourceType = "shell" }},
		{name: "model output source", mutate: func(input *EvolutionSignalInput) { input.SourceType = "model_output" }},
		{name: "unknown signal", mutate: func(input *EvolutionSignalInput) { input.SignalType = "arbitrary_prompt" }},
		{name: "sensitive idempotency", mutate: func(input *EvolutionSignalInput) { input.IdempotencyKey = "token=private-value" }},
		{name: "api key source", mutate: func(input *EvolutionSignalInput) { input.SourceID = "api_key=abc123" }},
		{name: "private answer source", mutate: func(input *EvolutionSignalInput) { input.SourceID = "private_user_answer" }},
		{name: "user answer source", mutate: func(input *EvolutionSignalInput) { input.SourceID = "user-answer" }},
		{name: "feedback body evidence", mutate: func(input *EvolutionSignalInput) { input.EvidenceRefs = []string{"feedback:private_user_answer"} }},
		{name: "api key trace evidence", mutate: func(input *EvolutionSignalInput) { input.EvidenceRefs = []string{"trace:api_key=abc123"} }},
		{name: "arbitrary metric evidence", mutate: func(input *EvolutionSignalInput) { input.EvidenceRefs = []string{"metric:user_answer"} }},
		{name: "release evidence mismatch", mutate: func(input *EvolutionSignalInput) {
			input.ReleaseID = "release-a"
			input.EvidenceRefs = []string{"release:release-b"}
		}},
		{name: "secret package identity", mutate: func(input *EvolutionSignalInput) { input.PackageID = "access-token-value" }},
		{name: "secret release identity", mutate: func(input *EvolutionSignalInput) { input.ReleaseID = "session_cookie_value" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validEvolutionSignalInput("private-" + strings.ReplaceAll(test.name, " ", "-"))
			test.mutate(&input)
			if _, _, _, err := store.IngestSignal(input); err == nil {
				t.Fatal("sensitive input was accepted")
			}
		})
	}
	assertEvolutionSignalCounts(t, store, 0, 0, 0)
}

func TestEvolutionSignalAcceptsStructuredSourceAndEvidenceReferences(t *testing.T) {
	store := newEvolutionTestStore(t)
	input := validEvolutionSignalInput("structured-evidence")
	input.EvidenceRefs = []string{
		"evaluation:" + testEvolutionOpaqueID("b"),
		"feedback:123e4567-e89b-12d3-a456-426614174000",
		"metric:regression_pass_rate",
	}
	if _, _, created, err := store.IngestSignal(input); err != nil || !created {
		t.Fatalf("structured evidence = %v, %v", created, err)
	}

	knowledge := validEvolutionKnowledgeSignalInput("structured-release", "release-2026-08", "knowledge-agent")
	if _, _, created, err := store.IngestSignal(knowledge); err != nil || !created {
		t.Fatalf("related release evidence = %v, %v", created, err)
	}

	metric := validEvolutionSignalInput("structured-metric")
	metric.SignalType = EvolutionSignalTaskCompletionDrop
	metric.SourceType = EvolutionSignalSourceRuntimeMetric
	metric.SourceID = "task_completion_rate"
	metric.PackageID = "metric-agent"
	metric.EvidenceRefs = []string{"metric:task_completion_rate"}
	if _, _, created, err := store.IngestSignal(metric); err != nil || !created {
		t.Fatalf("allowlisted metric = %v, %v", created, err)
	}
}

func TestEvolutionOverviewGroupsFleetAndSortsOpenRuns(t *testing.T) {
	records := []AgentPackageRecord{
		{PackageID: "agent-b", Version: "1.0.0", LifecycleState: AgentPackagePublished, PublishedAt: "2026-08-01T00:00:00Z"},
		{PackageID: "agent-a", Version: "1.0.0", LifecycleState: AgentPackageSuperseded, PublishedAt: "2026-07-01T00:00:00Z"},
		{PackageID: "agent-a", Version: "1.1.0", LifecycleState: AgentPackagePublished, PublishedAt: "2026-08-01T00:00:00Z"},
		{PackageID: "agent-a", Version: "1.2.0", LifecycleState: AgentPackageDraft, PublishedAt: "2026-08-02T00:00:00Z"},
	}
	runs := []EvolutionRun{
		validOverviewRun("run-b", "agent-a", 50, "2026-08-11T10:00:00Z", EvolutionDetected),
		validOverviewRun("run-c", "agent-a", 50, "2026-08-11T09:00:00Z", EvolutionTriaged),
		validOverviewRun("run-a", "agent-b", 80, "2026-08-11T11:00:00Z", EvolutionDetected),
		validOverviewRun("run-completed", "agent-a", 100, "2026-08-10T00:00:00Z", EvolutionCompleted),
	}

	overview, err := BuildEvolutionOverview(records, runs)
	if err != nil {
		t.Fatal(err)
	}
	if got := evolutionRunIDs(overview.OpenRuns); fmt.Sprint(got) != "[run-a run-c run-b]" {
		t.Fatalf("open run order = %v", got)
	}
	if len(overview.AgentFleet) != 2 || overview.AgentFleet[0].PackageID != "agent-a" {
		t.Fatalf("fleet = %#v", overview.AgentFleet)
	}
	agentA := overview.AgentFleet[0]
	if agentA.Current == nil || agentA.Current.Version != "1.1.0" {
		t.Fatalf("current agent-a = %#v", agentA.Current)
	}
	if got := agentPackageVersions(agentA.History); fmt.Sprint(got) != "[1.2.0 1.0.0]" {
		t.Fatalf("history = %v", got)
	}
	if got := evolutionRunIDs(agentA.OpenRuns); fmt.Sprint(got) != "[run-c run-b]" {
		t.Fatalf("agent-a runs = %v", got)
	}

	page, err := PaginateEvolutionRuns(overview.OpenRuns, "run-c", 1)
	if err != nil || len(page.Runs) != 1 || page.Runs[0].RunID != "run-b" || page.NextCursor != "" {
		t.Fatalf("page = %#v, %v", page, err)
	}
}

func TestEvolutionOverviewUsesRunIDAsFinalStableTieBreak(t *testing.T) {
	runs := []EvolutionRun{
		validOverviewRun("run-z", "agent-a", 50, "2026-08-11T10:00:00Z", EvolutionDetected),
		validOverviewRun("run-a", "agent-a", 50, "2026-08-11T10:00:00Z", EvolutionDetected),
	}
	overview, err := BuildEvolutionOverview(nil, runs)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(evolutionRunIDs(overview.OpenRuns)); got != "[run-a run-z]" {
		t.Fatalf("tie order = %s", got)
	}
}

func validEvolutionSignalInput(key string) EvolutionSignalInput {
	return EvolutionSignalInput{
		IdempotencyKey: key,
		SignalType:     EvolutionSignalRegressionFailure,
		SourceType:     EvolutionSignalSourceEvaluation,
		SourceID:       testEvolutionOpaqueID("a"),
		PackageID:      "research-assistant",
		Severity:       EvolutionSignalSeverityHigh,
		ObservedValue:  0.4,
		BaselineValue:  0.8,
		EvidenceRefs:   []string{"evaluation:" + testEvolutionOpaqueID("a")},
		ObservedAt:     time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC),
	}
}

func validEvolutionKnowledgeSignalInput(key, releaseID, packageID string) EvolutionSignalInput {
	return EvolutionSignalInput{
		IdempotencyKey: key,
		SignalType:     EvolutionSignalReleaseStale,
		SourceType:     EvolutionSignalSourceKnowledgeStore,
		SourceID:       releaseID,
		PackageID:      packageID,
		ReleaseID:      releaseID,
		Severity:       EvolutionSignalSeverityHigh,
		ObservedValue:  31,
		BaselineValue:  30,
		EvidenceRefs:   []string{"release:" + releaseID},
		ObservedAt:     time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC),
	}
}

func testEvolutionOpaqueID(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func newEvolutionSignalTestStore(t *testing.T, now func() time.Time) *EvolutionControlStore {
	t.Helper()
	store, err := OpenEvolutionControlStore(t.TempDir(), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func assertEvolutionSignalCounts(t *testing.T, store *EvolutionControlStore, wantSignals, wantRuns, wantEvents int) {
	t.Helper()
	for table, want := range map[string]int{
		"evolution_signals": wantSignals,
		"evolution_runs":    wantRuns,
		"evolution_events":  wantEvents,
	} {
		var got int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
}

func validOverviewRun(id, packageID string, priority float64, createdAt string, status EvolutionRunStatus) EvolutionRun {
	return EvolutionRun{
		RunID: id, Attempt: 1, RunType: EvolutionRunAgentPolicy, PackageID: packageID,
		BaselinePackageVersion: "current", RiskLevel: "high", PriorityScore: priority,
		Status: status, TriggerSignalIDs: []string{"signal-" + id}, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}

func evolutionRunIDs(runs []EvolutionRun) []string {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, run.RunID)
	}
	return ids
}

func agentPackageVersions(records []AgentPackageRecord) []string {
	versions := make([]string, 0, len(records))
	for _, record := range records {
		versions = append(versions, record.Version)
	}
	return versions
}
