package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
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
	assertEvolutionObservationCount(t, store, 1)
}

func TestEvolutionSignalIdempotencyFingerprintCoversEveryPayloadField(t *testing.T) {
	store := newEvolutionTestStore(t)
	base := validEvolutionSignalInput("all-fields-conflict")
	base.ReleaseID = testEvolutionReleaseID("d")
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
		{name: "release ID", mutate: func(input *EvolutionSignalInput) { input.ReleaseID = testEvolutionReleaseID("e") }},
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
	second.IdempotencyKey = testEvolutionRequestID("dedup-fingerprint-second")
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
	assertEvolutionObservationCount(t, store, 2)
	var severity string
	var observedValue float64
	var evidenceJSON, observedAt string
	if err := store.db.QueryRow(`
		SELECT severity, observed_value, evidence_refs_json, observed_at
		FROM evolution_signal_observations WHERE request_key_hash = ?
	`, evolutionSignalRequestKeyHash(second.IdempotencyKey)).Scan(&severity, &observedValue, &evidenceJSON, &observedAt); err != nil {
		t.Fatal(err)
	}
	if severity != second.Severity || observedValue != second.ObservedValue ||
		!strings.Contains(evidenceJSON, second.EvidenceRefs[0]) || observedAt != second.ObservedAt.Format(time.RFC3339Nano) {
		t.Fatalf("secondary observation lost audit fields: %q %v %q %q", severity, observedValue, evidenceJSON, observedAt)
	}
	events, err := store.ListEvents(run.RunID, "", 100)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %#v, %v", events, err)
	}
	for _, event := range events {
		if len(event.ArtifactRefs) != 0 {
			t.Fatalf("timeline leaked internal mapping refs: %#v", event.ArtifactRefs)
		}
	}
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
	second.IdempotencyKey = testEvolutionRequestID("cooldown-second")
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
	third.IdempotencyKey = testEvolutionRequestID("cooldown-third")
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
			related.IdempotencyKey = testEvolutionRequestID(fmt.Sprintf("cross-store-aggregate-%d", index))
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

func TestEvolutionSignalDeterministicWriteConflictAndReplay(t *testing.T) {
	root := t.TempDir()
	lockAcquired := make(chan struct{})
	releaseLock := make(chan struct{})
	var releaseOnce sync.Once
	storeA := openEvolutionTestStoreAtRoot(t, root, evolutionStoreHooks{
		afterBeginTx: func() error {
			close(lockAcquired)
			select {
			case <-releaseLock:
				return nil
			case <-time.After(5 * time.Second):
				return errors.New("timed out waiting to release signal lock")
			}
		},
	})
	storeB := openEvolutionTestStoreAtRootWithOptions(t, root, evolutionStoreOpenOptions{busyTimeout: 50 * time.Millisecond})
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseLock) }) })
	input := validEvolutionSignalInput("deterministic-signal-lock")
	type result struct {
		signal  *EvolutionSignal
		run     *EvolutionRun
		created bool
		err     error
	}
	resultA := make(chan result, 1)
	go func() {
		signal, run, created, err := storeA.IngestSignal(input)
		resultA <- result{signal: signal, run: run, created: created, err: err}
	}()
	waitEvolutionTestSignal(t, lockAcquired, "store A signal lock")
	if _, _, _, err := storeB.IngestSignal(input); !errors.Is(err, ErrEvolutionWriteConflict) {
		t.Fatalf("store B locked IngestSignal error = %v", err)
	}
	releaseOnce.Do(func() { close(releaseLock) })
	first := <-resultA
	if first.err != nil || !first.created {
		t.Fatalf("store A ingest = %#v", first)
	}
	replayedSignal, replayedRun, created, err := storeB.IngestSignal(input)
	if err != nil || created || replayedSignal.SignalID != first.signal.SignalID || replayedRun.RunID != first.run.RunID {
		t.Fatalf("post-lock replay = %#v, %#v, %v, %v", replayedSignal, replayedRun, created, err)
	}
	assertEvolutionSignalCounts(t, storeB, 1, 1, 1)
	assertEvolutionObservationCount(t, storeB, 1)
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
	related.IdempotencyKey = testEvolutionRequestID("durable-related")
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
	assertEvolutionObservationCount(t, store, 0)
	var mappings int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_signal_observations WHERE request_key_hash = ?`, evolutionSignalRequestKeyHash(input.IdempotencyKey)).Scan(&mappings); err != nil {
		t.Fatal(err)
	}
	if mappings != 0 {
		t.Fatalf("idempotency mappings = %d", mappings)
	}
}

func TestEvolutionSignalAggregationEventFailureRollsBackEveryMutation(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	seedStore, err := OpenEvolutionControlStore(root, clock)
	if err != nil {
		t.Fatal(err)
	}
	agent := validEvolutionSignalInput("aggregate-rollback-agent")
	agent.ObservedAt = now
	_, originalRun, _, err := seedStore.IngestSignal(agent)
	if err != nil {
		t.Fatal(err)
	}
	before := cloneEvolutionRun(originalRun)
	if err := seedStore.Close(); err != nil {
		t.Fatal(err)
	}

	injected := errors.New("aggregate event insert failed")
	seenEvent := make(chan EvolutionEvent, 1)
	failingStore, err := openEvolutionControlStoreWithOptions(root, clock, evolutionStoreOpenOptions{
		hooks: evolutionStoreHooks{
			beforeEventInsert: func(event EvolutionEvent) error {
				seenEvent <- event
				return injected
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	knowledge := validEvolutionKnowledgeSignalInput("aggregate-rollback-knowledge", testEvolutionReleaseID("6"), agent.PackageID)
	knowledge.ObservedAt = now
	if _, _, _, err := failingStore.IngestSignal(knowledge); !errors.Is(err, injected) {
		t.Fatalf("aggregate ingest error = %v", err)
	}
	event := <-seenEvent
	if event.EventType != "signal_aggregated" || event.RunID != originalRun.RunID {
		t.Fatalf("failure hook did not observe aggregate path: %#v", event)
	}
	if err := failingStore.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenEvolutionControlStore(root, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	after, err := reopened.LoadRun(originalRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("aggregate rollback changed run:\nafter:  %#v\nbefore: %#v", after, before)
	}
	assertEvolutionSignalCounts(t, reopened, 1, 1, 1)
	assertEvolutionObservationCount(t, reopened, 1)
	var signals, mappings int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM evolution_signals WHERE idempotency_key = ?`, knowledge.IdempotencyKey).Scan(&signals); err != nil {
		t.Fatal(err)
	}
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM evolution_signal_observations WHERE request_key_hash = ?`, evolutionSignalRequestKeyHash(knowledge.IdempotencyKey)).Scan(&mappings); err != nil {
		t.Fatal(err)
	}
	if signals != 0 || mappings != 0 {
		t.Fatalf("rolled-back signal/mapping counts = %d/%d", signals, mappings)
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
	input.IdempotencyKey = testEvolutionRequestID("terminal-second")
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
	releaseID := testEvolutionReleaseID("f")
	knowledge := EvolutionSignalInput{
		IdempotencyKey: testEvolutionRequestID("combined-knowledge"),
		SignalType:     EvolutionSignalReleaseStale,
		SourceType:     EvolutionSignalSourceKnowledgeStore,
		SourceID:       releaseID,
		PackageID:      agent.PackageID,
		ReleaseID:      releaseID,
		Severity:       EvolutionSignalSeverityHigh,
		ObservedValue:  31,
		BaselineValue:  30,
		EvidenceRefs:   []string{"release:" + releaseID},
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
	if got := fmt.Sprint(combined.BaselineReleaseIDs); got != fmt.Sprintf("[%s]", releaseID) {
		t.Fatalf("release identity = %s", got)
	}
}

func TestEvolutionSignalKnowledgeRunCombinesWhenRelatedAgentArrives(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store := newEvolutionSignalTestStore(t, func() time.Time { return now })
	knowledge := validEvolutionKnowledgeSignalInput("reverse-combined-knowledge", testEvolutionReleaseID("7"), "reverse-agent")
	knowledge.ObservedAt = now
	knowledgeSignal, knowledgeRun, _, err := store.IngestSignal(knowledge)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	agent := validEvolutionSignalInput("reverse-combined-agent")
	agent.PackageID = knowledge.PackageID
	agent.ObservedAt = now
	agentSignal, combined, _, err := store.IngestSignal(agent)
	if err != nil {
		t.Fatal(err)
	}
	if combined.RunID != knowledgeRun.RunID || combined.RunType != EvolutionRunCombined {
		t.Fatalf("reverse combined run = %#v, knowledge = %#v", combined, knowledgeRun)
	}
	if got := fmt.Sprint(combined.TriggerSignalIDs); got != fmt.Sprintf("[%s %s]", knowledgeSignal.SignalID, agentSignal.SignalID) {
		t.Fatalf("reverse combined trigger IDs = %s", got)
	}
}

func TestEvolutionSignalCooldownBoundaryIsInclusive(t *testing.T) {
	base := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name        string
		age         time.Duration
		wantSameRun bool
	}{
		{name: "exact boundary", age: EvolutionSignalCooldown, wantSameRun: true},
		{name: "one nanosecond after", age: EvolutionSignalCooldown + time.Nanosecond, wantSameRun: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := base
			store := newEvolutionSignalTestStore(t, func() time.Time { return now })
			first := validEvolutionSignalInput("boundary-first-" + test.name)
			first.ObservedAt = now
			_, firstRun, _, err := store.IngestSignal(first)
			if err != nil {
				t.Fatal(err)
			}
			now = base.Add(test.age)
			second := first
			second.IdempotencyKey = testEvolutionRequestID("boundary-second-" + test.name)
			second.ObservedAt = now
			_, secondRun, _, err := store.IngestSignal(second)
			if err != nil {
				t.Fatal(err)
			}
			if got := firstRun.RunID == secondRun.RunID; got != test.wantSameRun {
				t.Fatalf("same run = %v, want %v", got, test.wantSameRun)
			}
		})
	}
}

func TestEvolutionSignalScopeAndReplayQueriesUseIndexes(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store := newEvolutionSignalTestStore(t, func() time.Time { return now })
	target := validEvolutionSignalInput("indexed-target")
	target.PackageID = "indexed-target-agent"
	target.ObservedAt = now
	_, targetRun, _, err := store.IngestSignal(target)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 128; index++ {
		unrelated := validEvolutionSignalInput(fmt.Sprintf("indexed-unrelated-%03d", index))
		unrelated.PackageID = fmt.Sprintf("unrelated-agent-%03d", index)
		unrelated.ObservedAt = now
		if _, _, _, err := store.IngestSignal(unrelated); err != nil {
			t.Fatal(err)
		}
	}
	related := target
	related.IdempotencyKey = testEvolutionRequestID("indexed-target-related")
	related.ObservedValue = 0.41
	_, aggregated, _, err := store.IngestSignal(related)
	if err != nil || aggregated.RunID != targetRun.RunID {
		t.Fatalf("indexed aggregate = %#v, %v", aggregated, err)
	}

	assertEvolutionQueryPlanUses(t, store, "idx_evolution_run_scopes_lookup", `
		SELECT r.run_id FROM evolution_run_scopes AS scope
		JOIN evolution_runs AS r ON r.run_id = scope.run_id
		WHERE scope.scope_type = 'package' AND scope.scope_id = ?
	`, target.PackageID)
	assertEvolutionQueryPlanUses(t, store, "idx_evolution_signal_observations_request", `
		SELECT run_id FROM evolution_signal_observations WHERE request_key_hash = ?
	`, evolutionSignalRequestKeyHash(related.IdempotencyKey))
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
	secondAgent.IdempotencyKey = testEvolutionRequestID("identity-agent-b")
	secondAgent.PackageID = "different-agent"
	_, agentRunB, _, err := store.IngestSignal(secondAgent)
	if err != nil {
		t.Fatal(err)
	}
	if agentRunA.RunID == agentRunB.RunID || agentRunB.RunType != EvolutionRunAgentPolicy {
		t.Fatalf("different packages aggregated: %#v / %#v", agentRunA, agentRunB)
	}

	knowledgeA := validEvolutionKnowledgeSignalInput("identity-release-a", testEvolutionReleaseID("1"), "knowledge-agent")
	knowledgeA.ObservedAt = now
	_, knowledgeRunA, _, err := store.IngestSignal(knowledgeA)
	if err != nil {
		t.Fatal(err)
	}
	knowledgeB := validEvolutionKnowledgeSignalInput("identity-release-b", testEvolutionReleaseID("2"), "knowledge-agent")
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
		{name: "free form idempotency", mutate: func(input *EvolutionSignalInput) { input.IdempotencyKey = "request-one" }},
		{name: "api key source", mutate: func(input *EvolutionSignalInput) { input.SourceID = "api_key=abc123" }},
		{name: "private answer source", mutate: func(input *EvolutionSignalInput) { input.SourceID = "private_user_answer" }},
		{name: "user answer source", mutate: func(input *EvolutionSignalInput) { input.SourceID = "user-answer" }},
		{name: "feedback body evidence", mutate: func(input *EvolutionSignalInput) { input.EvidenceRefs = []string{"feedback:private_user_answer"} }},
		{name: "api key trace evidence", mutate: func(input *EvolutionSignalInput) { input.EvidenceRefs = []string{"trace:api_key=abc123"} }},
		{name: "arbitrary metric evidence", mutate: func(input *EvolutionSignalInput) { input.EvidenceRefs = []string{"metric:user_answer"} }},
		{name: "release evidence mismatch", mutate: func(input *EvolutionSignalInput) {
			input.ReleaseID = testEvolutionReleaseID("3")
			input.EvidenceRefs = []string{"release:" + testEvolutionReleaseID("4")}
		}},
		{name: "malformed package identity", mutate: func(input *EvolutionSignalInput) { input.PackageID = "api_key=abc123" }},
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

func TestEvolutionSignalKnowledgeReleaseIdentityUsesProductionFormat(t *testing.T) {
	validReleaseID := testEvolutionReleaseID("a")
	valid := validEvolutionKnowledgeSignalInput("release-format-valid", validReleaseID, "knowledge-agent")
	store := newEvolutionTestStore(t)
	if _, _, created, err := store.IngestSignal(valid); err != nil || !created {
		t.Fatalf("valid production release identity = %v, %v", created, err)
	}

	invalidReleaseIDs := []struct {
		name      string
		releaseID string
	}{
		{name: "matching private body", releaseID: "private_user_answer"},
		{name: "uppercase digest", releaseID: "release-" + strings.Repeat("A", 64)},
		{name: "short digest", releaseID: "release-abc123"},
		{name: "non hex digest", releaseID: "release-" + strings.Repeat("g", 64)},
	}
	for _, test := range invalidReleaseIDs {
		t.Run(test.name, func(t *testing.T) {
			input := validEvolutionKnowledgeSignalInput("release-format-"+test.name, test.releaseID, "knowledge-agent")
			if _, _, _, err := store.IngestSignal(input); err == nil {
				t.Fatalf("invalid release identity %q was accepted", test.releaseID)
			}
		})
	}
}

func TestEvolutionSignalAllowsHumanReadableDomainPackageIdentities(t *testing.T) {
	store := newEvolutionTestStore(t)
	for _, packageID := range []string{"token-economics-agent", "session-analysis-agent", "cookie-policy-research"} {
		t.Run(packageID, func(t *testing.T) {
			input := validEvolutionSignalInput("domain-package-" + packageID)
			input.PackageID = packageID
			if _, _, created, err := store.IngestSignal(input); err != nil || !created {
				t.Fatalf("domain package identity = %v, %v", created, err)
			}
		})
	}
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

	knowledge := validEvolutionKnowledgeSignalInput("structured-release", testEvolutionReleaseID("5"), "knowledge-agent")
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
	if got := agentPackageVersions(agentA.History); fmt.Sprint(got) != "[1.0.0]" {
		t.Fatalf("history = %v", got)
	}
	if got := evolutionRunIDs(agentA.OpenRuns); fmt.Sprint(got) != "[run-c run-b]" {
		t.Fatalf("agent-a runs = %v", got)
	}

	page, err := PaginateEvolutionRuns(overview.OpenRuns, "", 2)
	if err != nil || fmt.Sprint(evolutionRunIDs(page.Runs)) != "[run-a run-b]" || page.NextCursor == "" {
		t.Fatalf("page 1 = %#v, %v", page, err)
	}
	next, err := PaginateEvolutionRuns(overview.OpenRuns, page.NextCursor, 2)
	if err != nil || fmt.Sprint(evolutionRunIDs(next.Runs)) != "[run-c]" || next.NextCursor != "" {
		t.Fatalf("page 2 = %#v, %v", next, err)
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

func TestEvolutionOverviewSortsParsedTimesFiltersHistoryAndClonesRuntime(t *testing.T) {
	runtimeV2 := &AgentPackageRuntimeDescriptor{SchemaVersion: "agent-package.v2", PackageID: "time-agent", Version: "2.0.0", TimeoutMS: 2000}
	records := []AgentPackageRecord{
		{PackageID: "time-agent", Version: "1.0.0", LifecycleState: AgentPackagePublished, PublishedAt: "2026-08-11T12:00:00+02:00"},
		{PackageID: "time-agent", Version: "2.0.0", LifecycleState: AgentPackagePublished, PublishedAt: "2026-08-11T10:00:00.1Z", Runtime: runtimeV2},
		{PackageID: "time-agent", Version: "0.9.0", LifecycleState: AgentPackageSuperseded, PublishedAt: "2026-08-11T09:59:59.999999999Z"},
		{PackageID: "time-agent", Version: "3.0.0", LifecycleState: AgentPackageDraft},
	}
	runs := []EvolutionRun{
		validOverviewRun("run-offset", "time-agent", 50, "2026-08-11T12:00:00+02:00", EvolutionDetected),
		validOverviewRun("run-fraction", "time-agent", 50, "2026-08-11T10:00:00.1Z", EvolutionDetected),
	}
	overview, err := BuildEvolutionOverview(records, runs)
	if err != nil {
		t.Fatal(err)
	}
	fleet := overview.AgentFleet[0]
	if fleet.Current == nil || fleet.Current.Version != "2.0.0" {
		t.Fatalf("current by parsed time = %#v", fleet.Current)
	}
	if got := fmt.Sprint(agentPackageVersions(fleet.History)); got != "[0.9.0]" {
		t.Fatalf("history states = %s", got)
	}
	if got := fmt.Sprint(evolutionRunIDs(overview.OpenRuns)); got != "[run-offset run-fraction]" {
		t.Fatalf("run time order = %s", got)
	}
	runtimeV2.TimeoutMS = 9999
	if fleet.Current.Runtime == nil || fleet.Current.Runtime.TimeoutMS != 2000 {
		t.Fatalf("runtime pointer was not deep-cloned: %#v", fleet.Current.Runtime)
	}

	invalid := records[:1]
	invalid[0].PublishedAt = "not-a-time"
	if _, err := BuildEvolutionOverview(invalid, nil); err == nil {
		t.Fatal("invalid non-empty published_at was accepted")
	}
}

func TestEvolutionRunListUsesImmutableKeysetWhenPriorityChanges(t *testing.T) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	store := newEvolutionSignalTestStore(t, func() time.Time { return now })
	runs := make([]*EvolutionRun, 0, 3)
	for index := 0; index < 3; index++ {
		input := validEvolutionSignalInput(fmt.Sprintf("keyset-run-%d", index))
		input.PackageID = fmt.Sprintf("keyset-agent-%d", index)
		input.ObservedAt = now
		_, run, _, err := store.IngestSignal(input)
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run)
		now = now.Add(time.Hour)
	}
	page1, err := store.ListEvolutionRuns("", 1)
	if err != nil || len(page1.Runs) != 1 || page1.Runs[0].RunID != runs[2].RunID || page1.NextCursor == "" {
		t.Fatalf("page 1 = %#v, %v", page1, err)
	}
	if _, err := store.db.Exec(`UPDATE evolution_runs SET priority_score = 1 WHERE run_id = ?`, runs[2].RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE evolution_runs SET priority_score = 100 WHERE run_id = ?`, runs[1].RunID); err != nil {
		t.Fatal(err)
	}
	page2, err := store.ListEvolutionRuns(page1.NextCursor, 2)
	if err != nil || fmt.Sprint(evolutionRunIDs(page2.Runs)) != fmt.Sprintf("[%s %s]", runs[1].RunID, runs[0].RunID) {
		t.Fatalf("page 2 = %#v, %v", page2, err)
	}
	if _, err := store.ListEvolutionRuns("not-base64", 1); err == nil {
		t.Fatal("malformed keyset cursor was accepted")
	}
	missingCursor, err := encodeEvolutionRunCursor(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC).UnixNano(), "run-missing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListEvolutionRuns(missingCursor, 1); !errors.Is(err, ErrEvolutionRunCursorNotFound) {
		t.Fatalf("missing keyset cursor error = %v", err)
	}
	assertEvolutionQueryPlanUses(t, store, "idx_evolution_runs_created_ns", `
		SELECT run_id FROM evolution_runs
		ORDER BY created_at_unix_nano DESC, run_id ASC LIMIT 2
	`)
}

func validEvolutionSignalInput(key string) EvolutionSignalInput {
	return EvolutionSignalInput{
		IdempotencyKey: testEvolutionRequestID(key),
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
		IdempotencyKey: testEvolutionRequestID(key),
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

func testEvolutionRequestID(label string) string {
	digest := sha256.Sum256([]byte(label))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func testEvolutionReleaseID(character string) string {
	return "release-" + strings.Repeat(character, 64)
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

func assertEvolutionObservationCount(t *testing.T, store *EvolutionControlStore, want int) {
	t.Helper()
	var got int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_signal_observations`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("evolution_signal_observations count = %d, want %d", got, want)
	}
}

func assertEvolutionQueryPlanUses(t *testing.T, store *EvolutionControlStore, indexName, query string, args ...any) {
	t.Helper()
	rows, err := store.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(details, "\n")
	if !strings.Contains(joined, indexName) {
		t.Fatalf("query plan did not use %s:\n%s", indexName, joined)
	}
	if strings.Contains(joined, "SCAN r") {
		t.Fatalf("query plan scanned evolution_runs:\n%s", joined)
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
