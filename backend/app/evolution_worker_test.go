package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvolutionWorkerCapabilityAllowlistAndArtifactBoundary(t *testing.T) {
	want := []EvolutionWorkerCapability{
		EvolutionCapabilityKnowledge,
		EvolutionCapabilityAgent,
		EvolutionCapabilityEvaluation,
		EvolutionCapabilityRelease,
		EvolutionCapabilityObservation,
	}
	wantValues := []string{"knowledge_evolution", "agent_evolution", "evaluation", "release", "observation"}
	for index, capability := range want {
		if string(capability) != wantValues[index] || !isAllowedEvolutionWorkerCapability(capability) {
			t.Fatalf("capability[%d] = %q", index, capability)
		}
	}
	if isAllowedEvolutionWorkerCapability("shell") {
		t.Fatal("arbitrary capability accepted")
	}

	store := openEvolutionWorkerTestStore(t, newEvolutionWorkerClock())
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	invalid := []EvolutionWorkInput{
		{IdempotencyKey: "22222222-2222-4222-8222-222222222222", RunID: run.RunID, Capability: "shell", ArtifactRef: "artifact:sha256:" + workerHex('a'), MaxAttempts: 3},
		{IdempotencyKey: "33333333-3333-4333-8333-333333333333", RunID: run.RunID, Capability: EvolutionCapabilityAgent, ArtifactRef: "/private/work/prompt.md", MaxAttempts: 3},
		{IdempotencyKey: "44444444-4444-4444-8444-444444444444", RunID: run.RunID, Capability: EvolutionCapabilityAgent, ArtifactRef: "https://example.invalid/a?token=secret", MaxAttempts: 3},
		{IdempotencyKey: "55555555-5555-4555-8555-555555555555", RunID: run.RunID, Capability: EvolutionCapabilityAgent, ArtifactRef: "embedded model output body", MaxAttempts: 3},
	}
	for _, input := range invalid {
		if _, _, err := store.EnqueueEvolutionWork(input); err == nil {
			t.Fatalf("invalid input accepted: %#v", input)
		}
	}
}

func TestEvolutionWorkerClaimsAcrossStoresWithCapabilityFilteringAndFreshLeaseIdentity(t *testing.T) {
	root := t.TempDir()
	clock := newEvolutionWorkerClock()
	seed, err := OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	run := createEvolutionWorkerTestRun(t, seed, "11111111-1111-4111-8111-111111111111")
	for index, capability := range []EvolutionWorkerCapability{EvolutionCapabilityAgent, EvolutionCapabilityKnowledge} {
		_, created, err := seed.EnqueueEvolutionWork(EvolutionWorkInput{
			IdempotencyKey: fmt.Sprintf("22222222-2222-4222-8222-%012d", index),
			RunID:          run.RunID, Capability: capability,
			ArtifactRef: "artifact:sha256:" + workerHex(rune('a'+index)), MaxAttempts: 3,
		})
		if err != nil || !created {
			t.Fatalf("enqueue %d: created=%v err=%v", index, created, err)
		}
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	storeBStarted := make(chan struct{})
	storeA, err := openEvolutionControlStore(root, clock.Now, evolutionStoreHooks{afterBeginTx: func() error {
		close(entered)
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storeA.Close() })
	storeB, err := openEvolutionControlStore(root, clock.Now, evolutionStoreHooks{beforeBeginTx: func() error {
		close(storeBStarted)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storeB.Close() })

	var wg sync.WaitGroup
	claimed := make(chan *EvolutionWork, 2)
	errs := make(chan error, 2)
	claim := func(index int, store *EvolutionControlStore) {
		wg.Add(1)
		go func(index int, store *EvolutionControlStore) {
			defer wg.Done()
			work, ok, err := store.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{
				WorkerID:      fmt.Sprintf("worker-%d", index),
				Capabilities:  []EvolutionWorkerCapability{EvolutionCapabilityAgent},
				LeaseDuration: time.Minute,
			})
			if err != nil {
				errs <- err
				return
			}
			if ok {
				claimed <- work
			}
		}(index, store)
	}
	claim(0, storeA)
	<-entered
	claim(1, storeB)
	select {
	case <-storeBStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("second store did not attempt its transaction")
	}
	releaseOnce.Do(func() { close(release) })
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	close(claimed)
	var got []*EvolutionWork
	for work := range claimed {
		got = append(got, work)
	}
	if len(got) != 1 || got[0].Capability != EvolutionCapabilityAgent || got[0].Attempt != 1 || got[0].LeaseID == "" {
		t.Fatalf("claims = %#v", got)
	}
	storeB.hooks.beforeBeginTx = nil
	if other, ok, err := storeB.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{WorkerID: "knowledge-worker", Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityKnowledge}, LeaseDuration: time.Minute}); err != nil || !ok || other.Capability != EvolutionCapabilityKnowledge {
		t.Fatalf("knowledge claim = %#v, %v, %v", other, ok, err)
	}
}

func TestEvolutionWorkerRenewsRejectsStaleLeaseAndRecoversExpiredAttempt(t *testing.T) {
	clock := newEvolutionWorkerClock()
	store := openEvolutionWorkerTestStore(t, clock)
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	work := enqueueEvolutionWorkerTestWork(t, store, run.RunID, 2)
	leased, ok, err := store.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{WorkerID: "worker-a", Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityAgent}, LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("lease = %#v, %v, %v", leased, ok, err)
	}
	firstLeaseID := leased.LeaseID
	clock.Advance(30 * time.Second)
	renewed, err := store.RenewEvolutionLease(EvolutionWorkLeaseUpdate{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: firstLeaseID, LeaseDuration: 2 * time.Minute})
	if err != nil || renewed.LeaseExpiresAt != clock.Now().Add(2*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("renewed = %#v, %v", renewed, err)
	}
	if _, err := store.RenewEvolutionLease(EvolutionWorkLeaseUpdate{WorkID: work.WorkID, WorkerID: "worker-b", LeaseID: firstLeaseID, LeaseDuration: time.Minute}); !errors.Is(err, ErrEvolutionLeaseLost) {
		t.Fatalf("wrong worker error = %v", err)
	}
	clock.Advance(3 * time.Minute)
	if _, err := store.RenewEvolutionLease(EvolutionWorkLeaseUpdate{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: firstLeaseID, LeaseDuration: time.Minute}); !errors.Is(err, ErrEvolutionLeaseExpired) {
		t.Fatalf("expired renew error = %v", err)
	}
	if recovered, err := store.RecoverExpiredEvolutionLeases(); err != nil || recovered != 1 {
		t.Fatalf("recover = %d, %v", recovered, err)
	}
	second, ok, err := store.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{WorkerID: "worker-b", Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityAgent}, LeaseDuration: time.Minute})
	if err != nil || !ok || second.Attempt != 2 || second.LeaseID == firstLeaseID {
		t.Fatalf("second lease = %#v, %v, %v", second, ok, err)
	}
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: firstLeaseID, Attempt: 1, ResultIdempotencyKey: "33333333-3333-4333-8333-333333333333", ResultArtifactRef: "artifact:sha256:" + workerHex('b')}); !errors.Is(err, ErrEvolutionLeaseLost) {
		t.Fatalf("stale completion error = %v", err)
	}
	clock.Advance(2 * time.Minute)
	if recovered, err := store.RecoverExpiredEvolutionLeases(); !errors.Is(err, ErrEvolutionAttemptExhausted) || recovered != 1 {
		t.Fatalf("exhausted recovery = %d, %v", recovered, err)
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionBlocked || loaded.FailureCode != "worker_attempts_exhausted" {
		t.Fatalf("blocked run = %#v, %v", loaded, err)
	}
}

func TestEvolutionWorkerFailureBackoffAndExhaustion(t *testing.T) {
	root := t.TempDir()
	clock := newEvolutionWorkerClock()
	store, err := OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	work := enqueueEvolutionWorkerTestWork(t, store, run.RunID, 2)
	leased := leaseEvolutionWorkerTestWork(t, store, "worker-a")
	firstFailure := EvolutionWorkFailure{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: leased.LeaseID, Attempt: leased.Attempt, FailureIdempotencyKey: "11111111-2222-4222-8222-111111111111", FailureCode: "generation_failed", FailureMessage: "bounded worker failure", RetryDelay: time.Minute}
	failed, blocked, err := store.FailEvolutionWork(firstFailure)
	if err != nil || blocked || failed.Status != EvolutionWorkPending {
		t.Fatalf("first failure = %#v blocked=%v err=%v", failed, blocked, err)
	}
	var failureKey, failureHash, failureWorker, failureLease string
	var failureAttempt int
	if err := store.db.QueryRow(`SELECT failure_idempotency_key, failure_hash, failure_worker_id, failure_lease_id, failure_attempt FROM evolution_work_items WHERE work_id = ?`, work.WorkID).Scan(&failureKey, &failureHash, &failureWorker, &failureLease, &failureAttempt); err != nil {
		t.Fatal(err)
	}
	if failureKey != firstFailure.FailureIdempotencyKey || failureHash != evolutionWorkerFailureHash(firstFailure) || failureWorker != firstFailure.WorkerID || failureLease != firstFailure.LeaseID || failureAttempt != firstFailure.Attempt {
		t.Fatalf("stored work failure identity = %q/%q/%q/%q/%d", failureKey, failureHash, failureWorker, failureLease, failureAttempt)
	}
	firstJSON, err := json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}
	if replayed, replayBlocked, err := store.FailEvolutionWork(firstFailure); err != nil || replayBlocked || replayed.Status != EvolutionWorkPending {
		t.Fatalf("first failure replay = %#v blocked=%v err=%v", replayed, replayBlocked, err)
	} else if replayJSON, marshalErr := json.Marshal(replayed); marshalErr != nil || string(replayJSON) != string(firstJSON) {
		t.Fatalf("first failure replay JSON = %s, %v; want %s", replayJSON, marshalErr, firstJSON)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if replayed, replayBlocked, replayErr := store.FailEvolutionWork(firstFailure); replayErr != nil || replayBlocked {
		t.Fatalf("reopen first failure replay = %#v blocked=%v err=%v", replayed, replayBlocked, replayErr)
	} else if replayJSON, marshalErr := json.Marshal(replayed); marshalErr != nil || string(replayJSON) != string(firstJSON) {
		t.Fatalf("reopen first failure replay JSON = %s, %v; want %s", replayJSON, marshalErr, firstJSON)
	}
	changedFailure := firstFailure
	changedFailure.FailureCode = "different_failure"
	if _, _, err := store.FailEvolutionWork(changedFailure); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("changed failure replay error = %v", err)
	}
	if _, ok, err := store.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{WorkerID: "worker-b", Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityAgent}, LeaseDuration: time.Minute}); err != nil || ok {
		t.Fatalf("early retry lease ok=%v err=%v", ok, err)
	}
	clock.Advance(time.Minute)
	leased = leaseEvolutionWorkerTestWork(t, store, "worker-b")
	if _, _, err := store.FailEvolutionWork(firstFailure); !errors.Is(err, ErrEvolutionLeaseLost) {
		t.Fatalf("old failure after a new claim error = %v", err)
	}
	secondFailure := EvolutionWorkFailure{WorkID: work.WorkID, WorkerID: "worker-b", LeaseID: leased.LeaseID, Attempt: leased.Attempt, FailureIdempotencyKey: "11111111-3333-4333-8333-111111111111", FailureCode: "generation_failed", FailureMessage: "bounded worker failure", RetryDelay: time.Minute}
	failed, blocked, err = store.FailEvolutionWork(secondFailure)
	if err != nil || !blocked || failed.Status != EvolutionWorkBlocked {
		t.Fatalf("exhausted failure = %#v blocked=%v err=%v", failed, blocked, err)
	}
	if replayed, replayBlocked, err := store.FailEvolutionWork(secondFailure); err != nil || !replayBlocked || replayed.Status != EvolutionWorkBlocked {
		t.Fatalf("blocked failure replay = %#v blocked=%v err=%v", replayed, replayBlocked, err)
	}
	changedBlocked := secondFailure
	changedBlocked.LeaseID = "lease-different"
	if _, _, err := store.FailEvolutionWork(changedBlocked); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("changed blocked failure error = %v", err)
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionBlocked {
		t.Fatalf("blocked run = %#v, %v", loaded, err)
	}
}

func TestEvolutionWorkerNanosecondAvailabilityExpiryAndIndexes(t *testing.T) {
	clock := newEvolutionWorkerClock()
	store := openEvolutionWorkerTestStore(t, clock)
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	availableAt := clock.Now().Add(900 * time.Millisecond)
	_, created, err := store.EnqueueEvolutionWork(EvolutionWorkInput{
		IdempotencyKey: "22222222-2222-4222-8222-222222222222", RunID: run.RunID,
		Capability: EvolutionCapabilityAgent, ArtifactRef: "artifact:sha256:" + workerHex('a'),
		AvailableAt: availableAt, MaxAttempts: 3,
	})
	if err != nil || !created {
		t.Fatalf("enqueue nanosecond work: created=%v err=%v", created, err)
	}
	claimInput := EvolutionWorkLeaseInput{WorkerID: "worker-a", Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityAgent}, LeaseDuration: time.Second}
	if _, ok, err := store.LeaseNextEvolutionWork(claimInput); err != nil || ok {
		t.Fatalf("claimed before subsecond availability: ok=%v err=%v", ok, err)
	}
	clock.Advance(900*time.Millisecond - time.Nanosecond)
	if _, ok, err := store.LeaseNextEvolutionWork(claimInput); err != nil || ok {
		t.Fatalf("claimed one nanosecond early: ok=%v err=%v", ok, err)
	}
	clock.Advance(time.Nanosecond)
	leased, ok, err := store.LeaseNextEvolutionWork(claimInput)
	if err != nil || !ok {
		t.Fatalf("claim at availability = %#v ok=%v err=%v", leased, ok, err)
	}
	clock.Advance(time.Second - time.Nanosecond)
	if _, err := store.RenewEvolutionLease(EvolutionWorkLeaseUpdate{WorkID: leased.WorkID, WorkerID: leased.WorkerID, LeaseID: leased.LeaseID, LeaseDuration: time.Second}); err != nil {
		t.Fatalf("renew one nanosecond before expiry: %v", err)
	}
	clock.Advance(time.Second)
	if _, err := store.RenewEvolutionLease(EvolutionWorkLeaseUpdate{WorkID: leased.WorkID, WorkerID: leased.WorkerID, LeaseID: leased.LeaseID, LeaseDuration: time.Second}); !errors.Is(err, ErrEvolutionLeaseExpired) {
		t.Fatalf("renew at exact expiry error = %v", err)
	}

	outboxAvailableAt := clock.Now().Add(900 * time.Millisecond)
	if _, created, err := store.EnqueueEvolutionOutbox(EvolutionOutboxInput{
		IdempotencyKey: "33333333-3333-4333-8333-333333333333", RunID: run.RunID,
		Topic: "evolution.work.completed", PayloadRef: "artifact:sha256:" + workerHex('b'), AvailableAt: outboxAvailableAt, MaxAttempts: 3,
	}); err != nil || !created {
		t.Fatalf("enqueue nanosecond outbox: created=%v err=%v", created, err)
	}
	outboxClaim := EvolutionOutboxLeaseInput{WorkerID: "dispatcher-a", LeaseDuration: time.Second}
	if _, ok, err := store.LeaseNextEvolutionOutbox(outboxClaim); err != nil || ok {
		t.Fatalf("claimed outbox before availability: ok=%v err=%v", ok, err)
	}
	clock.Advance(900*time.Millisecond - time.Nanosecond)
	if _, ok, err := store.LeaseNextEvolutionOutbox(outboxClaim); err != nil || ok {
		t.Fatalf("claimed outbox one nanosecond early: ok=%v err=%v", ok, err)
	}
	clock.Advance(time.Nanosecond)
	message, ok, err := store.LeaseNextEvolutionOutbox(outboxClaim)
	if err != nil || !ok {
		t.Fatalf("claim outbox at availability = %#v ok=%v err=%v", message, ok, err)
	}
	clock.Advance(time.Second - time.Nanosecond)
	if _, replay, err := store.DeliverEvolutionOutbox(EvolutionOutboxDelivery{OutboxID: message.OutboxID, WorkerID: message.WorkerID, LeaseID: message.LeaseID, Attempt: message.Attempt, ReceiptID: "55555555-5555-4555-8555-555555555555"}); err != nil || replay {
		t.Fatalf("deliver one nanosecond before expiry: replay=%v err=%v", replay, err)
	}
	if _, created, err := store.EnqueueEvolutionOutbox(EvolutionOutboxInput{
		IdempotencyKey: "44444444-4444-4444-8444-444444444444", RunID: run.RunID,
		Topic: "evolution.work.completed", PayloadRef: "artifact:sha256:" + workerHex('c'), MaxAttempts: 3,
	}); err != nil || !created {
		t.Fatalf("enqueue exact-expiry outbox: created=%v err=%v", created, err)
	}
	message, ok, err = store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher-b", LeaseDuration: time.Second})
	if err != nil || !ok {
		t.Fatalf("lease exact-expiry outbox = %#v ok=%v err=%v", message, ok, err)
	}
	clock.Advance(time.Second)
	if _, _, err := store.DeliverEvolutionOutbox(EvolutionOutboxDelivery{OutboxID: message.OutboxID, WorkerID: message.WorkerID, LeaseID: message.LeaseID, Attempt: message.Attempt, ReceiptID: "66666666-6666-4666-8666-666666666666"}); !errors.Is(err, ErrEvolutionLeaseExpired) {
		t.Fatalf("deliver at exact expiry error = %v", err)
	}

	for _, query := range []struct {
		name  string
		sql   string
		index string
	}{
		{name: "work claim", sql: `EXPLAIN QUERY PLAN SELECT work_id FROM evolution_work_items WHERE status = 'pending' AND capability = 'agent_evolution' AND available_at_unix_nano <= 1 ORDER BY available_at_unix_nano, created_at, work_id LIMIT 1`, index: "idx_evolution_work_pending_capability"},
		{name: "work expiry", sql: `EXPLAIN QUERY PLAN SELECT work_id FROM evolution_work_items WHERE status = 'leased' AND lease_expires_at_unix_nano <= 1 ORDER BY lease_expires_at_unix_nano, work_id`, index: "idx_evolution_work_lease_expiry"},
		{name: "outbox claim", sql: `EXPLAIN QUERY PLAN SELECT outbox_id FROM evolution_outbox WHERE status = 'pending' AND available_at_unix_nano <= 1 ORDER BY available_at_unix_nano, created_at, outbox_id LIMIT 1`, index: "idx_evolution_outbox_pending_delivery"},
		{name: "outbox expiry", sql: `EXPLAIN QUERY PLAN SELECT outbox_id FROM evolution_outbox WHERE status = 'leased' AND lease_expires_at_unix_nano <= 1 ORDER BY lease_expires_at_unix_nano, outbox_id`, index: "idx_evolution_outbox_lease_expiry"},
		{name: "run unfinished work", sql: `EXPLAIN QUERY PLAN SELECT COUNT(*) FROM evolution_work_items WHERE run_id = 'run' AND status IN ('pending', 'leased')`, index: "idx_evolution_work_run_unfinished"},
		{name: "run open outbox", sql: `EXPLAIN QUERY PLAN SELECT COUNT(*) FROM evolution_outbox WHERE run_id = 'run' AND status IN ('pending', 'leased')`, index: "idx_evolution_outbox_run_open"},
	} {
		t.Run(query.name, func(t *testing.T) {
			rows, err := store.db.Query(query.sql)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var id, parent, unused int
			var detail string
			found := false
			for rows.Next() {
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				found = found || strings.Contains(detail, query.index)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if !found {
				t.Fatalf("query plan did not use %s", query.index)
			}
		})
	}
}

func TestEvolutionWorkerRejectsUnsafeNanosecondTimes(t *testing.T) {
	if _, err := evolutionWorkerUnixNano(time.Date(2500, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("unsafe year was accepted as a nanosecond timestamp")
	}
	nearLimit := time.Unix(0, math.MaxInt64-5).UTC()
	if _, _, err := evolutionWorkerAddDuration(nearLimit, 10*time.Nanosecond); err == nil {
		t.Fatal("overflowing nanosecond duration was accepted")
	}
}

func TestEvolutionWorkerLeasePersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()
	clock := newEvolutionWorkerClock()
	store, err := OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	enqueueEvolutionWorkerTestWork(t, store, run.RunID, 3)
	leased := leaseEvolutionWorkerTestWork(t, store, "worker-a")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	renewed, err := store.RenewEvolutionLease(EvolutionWorkLeaseUpdate{WorkID: leased.WorkID, WorkerID: "worker-a", LeaseID: leased.LeaseID, LeaseDuration: 2 * time.Minute})
	if err != nil || renewed.LeaseID != leased.LeaseID || renewed.Attempt != 1 {
		t.Fatalf("persisted lease = %#v, %v", renewed, err)
	}
}

func TestEvolutionWorkerCompletionIsIdempotentAndAtomic(t *testing.T) {
	clock := newEvolutionWorkerClock()
	store := openEvolutionWorkerTestStore(t, clock)
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	work := enqueueEvolutionWorkerTestWork(t, store, run.RunID, 3)
	leased := leaseEvolutionWorkerTestWork(t, store, "worker-a")
	completion := EvolutionWorkCompletion{
		WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: leased.LeaseID, Attempt: leased.Attempt,
		ResultIdempotencyKey: "33333333-3333-4333-8333-333333333333",
		ResultArtifactRef:    "artifact:sha256:" + workerHex('b'),
	}
	completed, replay, err := store.CompleteEvolutionWork(completion)
	if err != nil || replay || completed.Status != EvolutionWorkCompleted {
		t.Fatalf("complete = %#v, replay=%v err=%v", completed, replay, err)
	}
	beforeEvents, beforeOutbox := evolutionWorkerCounts(t, store, run.RunID)
	replayed, replay, err := store.CompleteEvolutionWork(completion)
	if err != nil || !replay || replayed.ResultArtifactRef != completion.ResultArtifactRef {
		t.Fatalf("replay = %#v, replay=%v err=%v", replayed, replay, err)
	}
	afterEvents, afterOutbox := evolutionWorkerCounts(t, store, run.RunID)
	if beforeEvents != afterEvents || beforeOutbox != afterOutbox || afterOutbox != 1 {
		t.Fatalf("replay counts events=%d->%d outbox=%d->%d", beforeEvents, afterEvents, beforeOutbox, afterOutbox)
	}
	conflict := completion
	conflict.ResultArtifactRef = "artifact:sha256:" + workerHex('c')
	if _, _, err := store.CompleteEvolutionWork(conflict); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("same result key changed payload error = %v", err)
	}
	conflict.ResultIdempotencyKey = "44444444-4444-4444-8444-444444444444"
	if _, _, err := store.CompleteEvolutionWork(conflict); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("different result re-execution error = %v", err)
	}
	loadedRun, err := store.LoadRun(run.RunID)
	if err != nil || loadedRun.Status != EvolutionDetected {
		t.Fatalf("worker bypassed control state = %#v, %v", loadedRun, err)
	}
	secondRun := createEvolutionWorkerTestRun(t, store, "77777777-7777-4777-8777-777777777777")
	secondWork := enqueueEvolutionWorkerTestWork(t, store, secondRun.RunID, 3)
	secondLease := leaseEvolutionWorkerTestWork(t, store, "worker-b")
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: secondWork.WorkID, WorkerID: "worker-b", LeaseID: secondLease.LeaseID, Attempt: secondLease.Attempt, ResultIdempotencyKey: completion.ResultIdempotencyKey, ResultArtifactRef: completion.ResultArtifactRef}); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("cross-work result replay error = %v", err)
	}

	injected := errors.New("outbox insert failed")
	rollbackStore := openEvolutionWorkerTestStoreWithHooks(t, newEvolutionWorkerClock(), evolutionStoreHooks{beforeOutboxInsert: func() error { return injected }})
	rollbackRun := createEvolutionWorkerTestRun(t, rollbackStore, "55555555-5555-4555-8555-555555555555")
	rollbackWork := enqueueEvolutionWorkerTestWork(t, rollbackStore, rollbackRun.RunID, 3)
	rollbackLease := leaseEvolutionWorkerTestWork(t, rollbackStore, "worker-a")
	if _, _, err := rollbackStore.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: rollbackWork.WorkID, WorkerID: "worker-a", LeaseID: rollbackLease.LeaseID, Attempt: rollbackLease.Attempt, ResultIdempotencyKey: "66666666-6666-4666-8666-666666666666", ResultArtifactRef: "artifact:sha256:" + workerHex('d')}); !errors.Is(err, injected) {
		t.Fatalf("atomic failure = %v", err)
	}
	var status string
	if err := rollbackStore.db.QueryRow(`SELECT status FROM evolution_work_items WHERE work_id = ?`, rollbackWork.WorkID).Scan(&status); err != nil || status != string(EvolutionWorkLeased) {
		t.Fatalf("rolled back work status = %q, %v", status, err)
	}
	if events, outbox := evolutionWorkerCounts(t, rollbackStore, rollbackRun.RunID); events != 1 || outbox != 0 {
		t.Fatalf("rollback counts events=%d outbox=%d", events, outbox)
	}
}

func TestEvolutionWorkerCompletionPersistsResultLeaseIdentityAcrossReopen(t *testing.T) {
	root := t.TempDir()
	clock := newEvolutionWorkerClock()
	store, err := OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	work := enqueueEvolutionWorkerTestWork(t, store, run.RunID, 3)
	leased := leaseEvolutionWorkerTestWork(t, store, "worker-a")
	completion := EvolutionWorkCompletion{
		WorkID: work.WorkID, WorkerID: leased.WorkerID, LeaseID: leased.LeaseID, Attempt: leased.Attempt,
		ResultIdempotencyKey: "22222222-2222-4222-8222-222222222222",
		ResultArtifactRef:    "artifact:sha256:" + workerHex('b'),
	}
	completed, replay, err := store.CompleteEvolutionWork(completion)
	if err != nil || replay {
		t.Fatalf("complete = %#v replay=%v err=%v", completed, replay, err)
	}
	if completed.WorkerID != completion.WorkerID || completed.LeaseID != completion.LeaseID || completed.Attempt != completion.Attempt {
		t.Fatalf("completion identity = worker=%q lease=%q attempt=%d", completed.WorkerID, completed.LeaseID, completed.Attempt)
	}
	encoded, err := json.Marshal(completed)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	if public["worker_id"] != completion.WorkerID || public["lease_id"] != completion.LeaseID || public["attempt"] != float64(completion.Attempt) {
		t.Fatalf("completion JSON = %s", encoded)
	}
	var storedWorker, storedLease, currentWorker, currentLease string
	var storedAttempt int
	if err := store.db.QueryRow(`SELECT result_worker_id, result_lease_id, result_attempt, lease_owner, lease_id FROM evolution_work_items WHERE work_id = ?`, work.WorkID).Scan(&storedWorker, &storedLease, &storedAttempt, &currentWorker, &currentLease); err != nil {
		t.Fatal(err)
	}
	if storedWorker != completion.WorkerID || storedLease != completion.LeaseID || storedAttempt != completion.Attempt || currentWorker != "" || currentLease != "" {
		t.Fatalf("stored result identity = %q/%q/%d current=%q/%q", storedWorker, storedLease, storedAttempt, currentWorker, currentLease)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	replayed, replay, err := store.CompleteEvolutionWork(completion)
	if err != nil || !replay || replayed.WorkerID != completion.WorkerID || replayed.LeaseID != completion.LeaseID || replayed.Attempt != completion.Attempt {
		t.Fatalf("reopen replay = %#v replay=%v err=%v", replayed, replay, err)
	}
	workerConflict := completion
	workerConflict.WorkerID = "worker-b"
	leaseConflict := completion
	leaseConflict.LeaseID = "lease-stale"
	attemptConflict := completion
	attemptConflict.Attempt++
	for _, test := range []struct {
		name       string
		completion EvolutionWorkCompletion
	}{
		{name: "worker", completion: workerConflict},
		{name: "lease", completion: leaseConflict},
		{name: "attempt", completion: attemptConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := store.CompleteEvolutionWork(test.completion); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
				t.Fatalf("identity conflict error = %v", err)
			}
		})
	}
}

func TestEvolutionWorkerEmptyAvailableAtHasStableIdempotencyFingerprint(t *testing.T) {
	clock := newEvolutionWorkerClock()
	store := openEvolutionWorkerTestStore(t, clock)
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	workInput := EvolutionWorkInput{
		IdempotencyKey: "22222222-2222-4222-8222-222222222222", RunID: run.RunID,
		Capability: EvolutionCapabilityAgent, ArtifactRef: "artifact:sha256:" + workerHex('a'), MaxAttempts: 3,
	}
	work, created, err := store.EnqueueEvolutionWork(workInput)
	if err != nil || !created {
		t.Fatalf("enqueue work = %#v created=%v err=%v", work, created, err)
	}
	clock.Advance(time.Hour)
	replayedWork, created, err := store.EnqueueEvolutionWork(workInput)
	if err != nil || created || replayedWork.WorkID != work.WorkID {
		t.Fatalf("empty available work replay = %#v created=%v err=%v", replayedWork, created, err)
	}
	var workCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_work_items WHERE idempotency_key = ?`, workInput.IdempotencyKey).Scan(&workCount); err != nil || workCount != 1 {
		t.Fatalf("empty available work replay count = %d, %v", workCount, err)
	}
	explicitWork := workInput
	explicitWork.AvailableAt, err = time.Parse(time.RFC3339Nano, work.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.EnqueueEvolutionWork(explicitWork); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("empty to explicit work error = %v", err)
	}

	outboxInput := EvolutionOutboxInput{
		IdempotencyKey: "33333333-3333-4333-8333-333333333333", RunID: run.RunID,
		Topic: "evolution.work.completed", PayloadRef: "artifact:sha256:" + workerHex('b'), MaxAttempts: 3,
	}
	outbox, created, err := store.EnqueueEvolutionOutbox(outboxInput)
	if err != nil || !created {
		t.Fatalf("enqueue outbox = %#v created=%v err=%v", outbox, created, err)
	}
	clock.Advance(time.Hour)
	replayedOutbox, created, err := store.EnqueueEvolutionOutbox(outboxInput)
	if err != nil || created || replayedOutbox.OutboxID != outbox.OutboxID {
		t.Fatalf("empty available outbox replay = %#v created=%v err=%v", replayedOutbox, created, err)
	}
	var outboxCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_outbox WHERE idempotency_key = ?`, outboxInput.IdempotencyKey).Scan(&outboxCount); err != nil || outboxCount != 1 {
		t.Fatalf("empty available outbox replay count = %d, %v", outboxCount, err)
	}
	explicitOutbox := outboxInput
	explicitOutbox.AvailableAt, err = time.Parse(time.RFC3339Nano, outbox.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.EnqueueEvolutionOutbox(explicitOutbox); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("empty to explicit outbox error = %v", err)
	}

	explicitWork.IdempotencyKey = "44444444-4444-4444-8444-444444444444"
	explicitWork.AvailableAt = clock.Now().Add(time.Hour)
	explicitCreated, created, err := store.EnqueueEvolutionWork(explicitWork)
	if err != nil || !created {
		t.Fatalf("explicit work = %#v created=%v err=%v", explicitCreated, created, err)
	}
	clock.Advance(time.Hour)
	if replayed, created, err := store.EnqueueEvolutionWork(explicitWork); err != nil || created || replayed.WorkID != explicitCreated.WorkID {
		t.Fatalf("explicit work replay = %#v created=%v err=%v", replayed, created, err)
	}
	changedExplicit := explicitWork
	changedExplicit.AvailableAt = changedExplicit.AvailableAt.Add(time.Second)
	if _, _, err := store.EnqueueEvolutionWork(changedExplicit); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("changed explicit work error = %v", err)
	}
	explicitOutbox.IdempotencyKey = "55555555-5555-4555-8555-555555555555"
	explicitOutbox.AvailableAt = clock.Now().Add(time.Hour)
	explicitOutboxCreated, created, err := store.EnqueueEvolutionOutbox(explicitOutbox)
	if err != nil || !created {
		t.Fatalf("explicit outbox = %#v created=%v err=%v", explicitOutboxCreated, created, err)
	}
	clock.Advance(time.Hour)
	if replayed, created, err := store.EnqueueEvolutionOutbox(explicitOutbox); err != nil || created || replayed.OutboxID != explicitOutboxCreated.OutboxID {
		t.Fatalf("explicit outbox replay = %#v created=%v err=%v", replayed, created, err)
	}
	changedExplicitOutbox := explicitOutbox
	changedExplicitOutbox.AvailableAt = changedExplicitOutbox.AvailableAt.Add(time.Second)
	if _, _, err := store.EnqueueEvolutionOutbox(changedExplicitOutbox); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("changed explicit outbox error = %v", err)
	}
}

func TestEvolutionWorkerOutboxReceiptPersistenceRecoveryAndDeadLetter(t *testing.T) {
	root := t.TempDir()
	clock := newEvolutionWorkerClock()
	store, err := OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	work := enqueueEvolutionWorkerTestWork(t, store, run.RunID, 3)
	leased := leaseEvolutionWorkerTestWork(t, store, "worker-a")
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: leased.LeaseID, Attempt: leased.Attempt, ResultIdempotencyKey: "22222222-2222-4222-8222-222222222222", ResultArtifactRef: "artifact:sha256:" + workerHex('b')}); err != nil {
		t.Fatal(err)
	}
	message, ok, err := store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher-a", LeaseDuration: time.Minute})
	if err != nil || !ok || message.Attempt != 1 || message.PayloadRef == "" || message.LeaseID == "" {
		t.Fatalf("outbox lease = %#v, %v, %v", message, ok, err)
	}
	oldLease := message.LeaseID
	clock.Advance(2 * time.Minute)
	if recovered, err := store.RecoverExpiredEvolutionOutboxLeases(); err != nil || recovered != 1 {
		t.Fatalf("outbox recovery = %d, %v", recovered, err)
	}
	message, ok, err = store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher-b", LeaseDuration: time.Minute})
	if err != nil || !ok || message.Attempt != 2 || message.LeaseID == oldLease {
		t.Fatalf("outbox re-lease = %#v, %v, %v", message, ok, err)
	}
	receipt := "33333333-3333-4333-8333-333333333333"
	delivery := EvolutionOutboxDelivery{OutboxID: message.OutboxID, WorkerID: "dispatcher-b", LeaseID: message.LeaseID, Attempt: message.Attempt, ReceiptID: receipt}
	delivered, replay, err := store.DeliverEvolutionOutbox(delivery)
	if err != nil || replay || delivered.Status != EvolutionOutboxDelivered {
		t.Fatalf("deliver = %#v, replay=%v err=%v", delivered, replay, err)
	}
	var deliveryWorker, deliveryLease string
	var deliveryAttempt int
	if err := store.db.QueryRow(`SELECT delivery_worker_id, delivery_lease_id, delivery_attempt FROM evolution_outbox WHERE outbox_id = ?`, message.OutboxID).Scan(&deliveryWorker, &deliveryLease, &deliveryAttempt); err != nil {
		t.Fatal(err)
	}
	if deliveryWorker != delivery.WorkerID || deliveryLease != delivery.LeaseID || deliveryAttempt != delivery.Attempt {
		t.Fatalf("stored delivery identity = %q/%q/%d", deliveryWorker, deliveryLease, deliveryAttempt)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	delivered, replay, err = store.DeliverEvolutionOutbox(delivery)
	if err != nil || !replay || delivered.ReceiptID != receipt {
		t.Fatalf("persisted receipt replay = %#v, replay=%v err=%v", delivered, replay, err)
	}
	changedReceipt := delivery
	changedReceipt.ReceiptID = "44444444-4444-4444-8444-444444444444"
	if _, _, err := store.DeliverEvolutionOutbox(changedReceipt); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("different receipt error = %v", err)
	}
	for name, changed := range map[string]EvolutionOutboxDelivery{
		"worker":  func() EvolutionOutboxDelivery { value := delivery; value.WorkerID = "dispatcher-x"; return value }(),
		"lease":   func() EvolutionOutboxDelivery { value := delivery; value.LeaseID = "lease-x"; return value }(),
		"attempt": func() EvolutionOutboxDelivery { value := delivery; value.Attempt++; return value }(),
	} {
		t.Run("delivery "+name, func(t *testing.T) {
			if _, _, err := store.DeliverEvolutionOutbox(changed); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
				t.Fatalf("delivery identity conflict error = %v", err)
			}
		})
	}
	cross, created, err := store.EnqueueEvolutionOutbox(EvolutionOutboxInput{IdempotencyKey: "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa", RunID: run.RunID, Topic: "evolution.work.completed", PayloadRef: "artifact:sha256:" + workerHex('d'), MaxAttempts: 3})
	if err != nil || !created {
		t.Fatalf("cross receipt enqueue = %#v created=%v err=%v", cross, created, err)
	}
	cross, ok, err = store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher-cross", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("cross receipt lease = %#v ok=%v err=%v", cross, ok, err)
	}
	if _, _, err := store.DeliverEvolutionOutbox(EvolutionOutboxDelivery{OutboxID: cross.OutboxID, WorkerID: cross.WorkerID, LeaseID: cross.LeaseID, Attempt: cross.Attempt, ReceiptID: receipt}); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("cross-message receipt reuse error = %v", err)
	}

	secondRun := createEvolutionWorkerTestRun(t, store, "55555555-5555-4555-8555-555555555555")
	secondWork := enqueueEvolutionWorkerTestWork(t, store, secondRun.RunID, 3)
	secondLease := leaseEvolutionWorkerTestWork(t, store, "worker-c")
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: secondWork.WorkID, WorkerID: "worker-c", LeaseID: secondLease.LeaseID, Attempt: secondLease.Attempt, ResultIdempotencyKey: "66666666-6666-4666-8666-666666666666", ResultArtifactRef: "artifact:sha256:" + workerHex('e')}); err != nil {
		t.Fatal(err)
	}
	dead, ok, err := store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher-c", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("deadletter lease = %#v, %v, %v", dead, ok, err)
	}
	var deadLettered bool
	var deadLetterLeaseID string
	for attempt := dead.Attempt; attempt <= dead.MaxAttempts; attempt++ {
		deadLetterLeaseID = dead.LeaseID
		failure := EvolutionOutboxFailure{OutboxID: dead.OutboxID, WorkerID: "dispatcher-c", LeaseID: dead.LeaseID, Attempt: dead.Attempt, FailureIdempotencyKey: fmt.Sprintf("sha256:%s", evolutionWorkerPayloadHash(fmt.Sprintf("dead-%d", dead.Attempt))), FailureCode: "delivery_unavailable", FailureMessage: "bounded failure", RetryDelay: time.Second}
		dead, deadLettered, err = store.FailEvolutionOutbox(failure)
		if err != nil {
			t.Fatal(err)
		}
		if deadLettered {
			break
		}
		clock.Advance(time.Second)
		dead, ok, err = store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher-c", LeaseDuration: time.Minute})
		if err != nil || !ok {
			t.Fatalf("retry lease = %#v, %v, %v", dead, ok, err)
		}
	}
	if !deadLettered || dead.Status != EvolutionOutboxDeadLetter {
		t.Fatalf("dead letter = %#v, %v", dead, deadLettered)
	}
	deadJSON, err := json.Marshal(dead)
	if err != nil {
		t.Fatal(err)
	}
	var storedFailureKey, storedFailureHash, storedFailureWorker, storedFailureLease string
	var storedFailureAttempt int
	if err := store.db.QueryRow(`SELECT failure_idempotency_key, failure_hash, failure_worker_id, failure_lease_id, failure_attempt FROM evolution_outbox WHERE outbox_id = ?`, dead.OutboxID).Scan(&storedFailureKey, &storedFailureHash, &storedFailureWorker, &storedFailureLease, &storedFailureAttempt); err != nil {
		t.Fatal(err)
	}
	if storedFailureKey == "" || storedFailureHash == "" || storedFailureWorker != "dispatcher-c" || storedFailureLease != deadLetterLeaseID || storedFailureAttempt != dead.Attempt {
		t.Fatalf("stored outbox failure identity = %q/%q/%q/%q/%d", storedFailureKey, storedFailureHash, storedFailureWorker, storedFailureLease, storedFailureAttempt)
	}
	blocked, err := store.LoadRun(secondRun.RunID)
	if err != nil || blocked.Status != EvolutionBlocked || blocked.FailureCode != "outbox_delivery_exhausted" || len([]rune(blocked.FailureMessage)) > EvolutionFailureMessageMaxRunes {
		t.Fatalf("deadletter run = %#v, %v", blocked, err)
	}
	deadReplay := EvolutionOutboxFailure{OutboxID: dead.OutboxID, WorkerID: "dispatcher-c", LeaseID: deadLetterLeaseID, Attempt: dead.Attempt, FailureIdempotencyKey: fmt.Sprintf("sha256:%s", evolutionWorkerPayloadHash(fmt.Sprintf("dead-%d", dead.Attempt))), FailureCode: "delivery_unavailable", FailureMessage: "bounded failure", RetryDelay: time.Second}
	if replayed, replayDead, err := store.FailEvolutionOutbox(deadReplay); err != nil || !replayDead || replayed.Status != EvolutionOutboxDeadLetter {
		t.Fatalf("deadletter replay = %#v, %v, %v", replayed, replayDead, err)
	} else if replayJSON, marshalErr := json.Marshal(replayed); marshalErr != nil || string(replayJSON) != string(deadJSON) {
		t.Fatalf("deadletter replay JSON = %s, %v; want %s", replayJSON, marshalErr, deadJSON)
	}
	changedDeadReplay := deadReplay
	changedDeadReplay.FailureCode = "different_failure"
	if _, _, err := store.FailEvolutionOutbox(changedDeadReplay); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("changed deadletter replay error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if replayed, replayDead, replayErr := store.FailEvolutionOutbox(deadReplay); replayErr != nil || !replayDead {
		t.Fatalf("reopen deadletter replay = %#v dead=%v err=%v", replayed, replayDead, replayErr)
	} else if replayJSON, marshalErr := json.Marshal(replayed); marshalErr != nil || string(replayJSON) != string(deadJSON) {
		t.Fatalf("reopen deadletter replay JSON = %s, %v; want %s", replayJSON, marshalErr, deadJSON)
	}
}

func TestEvolutionWorkerOutboxClaimsAcrossStoresDeterministically(t *testing.T) {
	root := t.TempDir()
	clock := newEvolutionWorkerClock()
	seed, err := OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	run := createEvolutionWorkerTestRun(t, seed, "11111111-1111-4111-8111-111111111111")
	if _, created, err := seed.EnqueueEvolutionOutbox(EvolutionOutboxInput{IdempotencyKey: "22222222-2222-4222-8222-222222222222", RunID: run.RunID, Topic: "evolution.work.completed", PayloadRef: "artifact:sha256:" + workerHex('a'), MaxAttempts: 3}); err != nil || !created {
		t.Fatalf("seed outbox: created=%v err=%v", created, err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	secondStarted := make(chan struct{})
	storeA, err := openEvolutionControlStore(root, clock.Now, evolutionStoreHooks{afterBeginTx: func() error {
		close(locked)
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storeA.Close() })
	storeB, err := openEvolutionControlStore(root, clock.Now, evolutionStoreHooks{beforeBeginTx: func() error {
		close(secondStarted)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storeB.Close() })
	type claimResult struct {
		message *EvolutionOutboxMessage
		ok      bool
		err     error
	}
	results := make(chan claimResult, 2)
	go func() {
		message, ok, err := storeA.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher-a", LeaseDuration: time.Minute})
		results <- claimResult{message: message, ok: ok, err: err}
	}()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("first outbox store did not acquire its transaction")
	}
	go func() {
		message, ok, err := storeB.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher-b", LeaseDuration: time.Minute})
		results <- claimResult{message: message, ok: ok, err: err}
	}()
	select {
	case <-secondStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("second outbox store did not attempt its transaction")
	}
	releaseOnce.Do(func() { close(release) })
	claimed := 0
	for index := 0; index < 2; index++ {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.ok {
				claimed++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("outbox claim goroutine did not finish")
		}
	}
	if claimed != 1 {
		t.Fatalf("outbox claims = %d, want 1", claimed)
	}
}

func TestEvolutionWorkerOutboxEnqueueIsIdempotentAndBounded(t *testing.T) {
	clock := newEvolutionWorkerClock()
	store := openEvolutionWorkerTestStore(t, clock)
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	input := EvolutionOutboxInput{
		IdempotencyKey: "22222222-2222-4222-8222-222222222222",
		RunID:          run.RunID, Topic: "evolution.work.completed",
		PayloadRef: "artifact:sha256:" + workerHex('a'), MaxAttempts: 3,
	}
	message, created, err := store.EnqueueEvolutionOutbox(input)
	if err != nil || !created || message.Status != EvolutionOutboxPending {
		t.Fatalf("enqueue = %#v, created=%v err=%v", message, created, err)
	}
	replayed, created, err := store.EnqueueEvolutionOutbox(input)
	if err != nil || created || replayed.OutboxID != message.OutboxID {
		t.Fatalf("replay = %#v, created=%v err=%v", replayed, created, err)
	}
	changed := input
	changed.PayloadRef = "artifact:sha256:" + workerHex('b')
	if _, _, err := store.EnqueueEvolutionOutbox(changed); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	for _, invalid := range []EvolutionOutboxInput{
		{IdempotencyKey: "33333333-3333-4333-8333-333333333333", RunID: run.RunID, Topic: "shell.execute", PayloadRef: input.PayloadRef, MaxAttempts: 3},
		{IdempotencyKey: "44444444-4444-4444-8444-444444444444", RunID: run.RunID, Topic: input.Topic, PayloadRef: "embedded body", MaxAttempts: 3},
	} {
		if _, _, err := store.EnqueueEvolutionOutbox(invalid); err == nil {
			t.Fatalf("invalid outbox accepted: %#v", invalid)
		}
	}
}

func TestEvolutionWorkerOutboxDeadLetterRollsBackRunEventAndMessage(t *testing.T) {
	clock := newEvolutionWorkerClock()
	injected := errors.New("deadletter event failed")
	store := openEvolutionWorkerTestStoreWithHooks(t, clock, evolutionStoreHooks{beforeEventInsert: func(event EvolutionEvent) error {
		if event.Code == "outbox_delivery_exhausted" {
			return injected
		}
		return nil
	}})
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	work := enqueueEvolutionWorkerTestWork(t, store, run.RunID, 3)
	leased := leaseEvolutionWorkerTestWork(t, store, "worker-a")
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: leased.LeaseID, Attempt: leased.Attempt, ResultIdempotencyKey: "22222222-2222-4222-8222-222222222222", ResultArtifactRef: "artifact:sha256:" + workerHex('f')}); err != nil {
		t.Fatal(err)
	}
	message, ok, err := store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("lease = %#v, %v, %v", message, ok, err)
	}
	if _, err := store.db.Exec(`UPDATE evolution_outbox SET max_attempts = 1 WHERE outbox_id = ?`, message.OutboxID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.FailEvolutionOutbox(EvolutionOutboxFailure{OutboxID: message.OutboxID, WorkerID: "dispatcher", LeaseID: message.LeaseID, Attempt: message.Attempt, FailureIdempotencyKey: "77777777-7777-4777-8777-777777777777", FailureCode: "delivery_unavailable", FailureMessage: "failure", RetryDelay: time.Second}); !errors.Is(err, injected) {
		t.Fatalf("deadletter atomic error = %v", err)
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionDetected {
		t.Fatalf("run after rollback = %#v, %v", loaded, err)
	}
	var status string
	if err := store.db.QueryRow(`SELECT status FROM evolution_outbox WHERE outbox_id = ?`, message.OutboxID).Scan(&status); err != nil || status != string(EvolutionOutboxLeased) {
		t.Fatalf("outbox after rollback = %q, %v", status, err)
	}
}

func TestEvolutionWorkerPendingOutboxPreventsTerminalRunTransition(t *testing.T) {
	clock := newEvolutionWorkerClock()
	store := openEvolutionWorkerTestStore(t, clock)
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	message, created, err := store.EnqueueEvolutionOutbox(EvolutionOutboxInput{IdempotencyKey: "22222222-2222-4222-8222-222222222222", RunID: run.RunID, Topic: "evolution.work.completed", PayloadRef: "artifact:sha256:" + workerHex('a'), MaxAttempts: 1})
	if err != nil || !created {
		t.Fatalf("enqueue = %#v created=%v err=%v", message, created, err)
	}
	beforeEvents, _ := evolutionWorkerCounts(t, store, run.RunID)
	if _, err := store.TransitionRun(run.RunID, EvolutionSuperseded, EvolutionTransitionInput{Actor: "operator", Code: "superseded"}); !errors.Is(err, ErrEvolutionPendingOutbox) {
		t.Fatalf("terminal transition error = %v", err)
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionDetected {
		t.Fatalf("run changed on protected transition = %#v, %v", loaded, err)
	}
	afterEvents, _ := evolutionWorkerCounts(t, store, run.RunID)
	if afterEvents != beforeEvents {
		t.Fatalf("protected transition wrote events %d -> %d", beforeEvents, afterEvents)
	}
	message, ok, err := store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("lease = %#v ok=%v err=%v", message, ok, err)
	}
	failed, deadLetter, err := store.FailEvolutionOutbox(EvolutionOutboxFailure{
		OutboxID: message.OutboxID, WorkerID: message.WorkerID, LeaseID: message.LeaseID,
		Attempt: message.Attempt, FailureIdempotencyKey: "33333333-3333-4333-8333-333333333333",
		FailureCode: "delivery_unavailable", FailureMessage: "bounded failure", RetryDelay: time.Second,
	})
	if err != nil || !deadLetter || failed.Status != EvolutionOutboxDeadLetter {
		t.Fatalf("deadletter = %#v dead=%v err=%v", failed, deadLetter, err)
	}
	loaded, err = store.LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionBlocked {
		t.Fatalf("deadletter did not block run = %#v, %v", loaded, err)
	}
	events, err := store.ListEvents(run.RunID, "", 100)
	if err != nil || len(events) != beforeEvents+1 || events[len(events)-1].ToStatus != EvolutionBlocked {
		t.Fatalf("deadletter events = %#v, %v", events, err)
	}
}

func TestEvolutionWorkerPendingWorkAndTerminalRunGuards(t *testing.T) {
	clock := newEvolutionWorkerClock()
	store := openEvolutionWorkerTestStore(t, clock)
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	work := enqueueEvolutionWorkerTestWork(t, store, run.RunID, 3)
	leased := leaseEvolutionWorkerTestWork(t, store, "worker-a")
	beforeEvents, beforeOutbox := evolutionWorkerCounts(t, store, run.RunID)
	if _, err := store.TransitionRun(run.RunID, EvolutionSuperseded, EvolutionTransitionInput{Actor: "operator", Code: "superseded"}); !errors.Is(err, ErrEvolutionPendingWork) {
		t.Fatalf("terminal transition with leased work error = %v", err)
	}
	if _, err := store.db.Exec(`UPDATE evolution_runs SET status = 'superseded' WHERE run_id = ?`, run.RunID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.EnqueueEvolutionWork(EvolutionWorkInput{IdempotencyKey: "22222222-2222-4222-8222-222222222222", RunID: run.RunID, Capability: EvolutionCapabilityAgent, ArtifactRef: "artifact:sha256:" + workerHex('b'), MaxAttempts: 3}); !errors.Is(err, ErrEvolutionTransitionConflict) {
		t.Fatalf("enqueue work on terminal run error = %v", err)
	}
	if _, _, err := store.EnqueueEvolutionOutbox(EvolutionOutboxInput{IdempotencyKey: "33333333-3333-4333-8333-333333333333", RunID: run.RunID, Topic: "evolution.work.completed", PayloadRef: "artifact:sha256:" + workerHex('c'), MaxAttempts: 3}); !errors.Is(err, ErrEvolutionTransitionConflict) {
		t.Fatalf("enqueue outbox on terminal run error = %v", err)
	}
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: work.WorkID, WorkerID: leased.WorkerID, LeaseID: leased.LeaseID, Attempt: leased.Attempt, ResultIdempotencyKey: "44444444-4444-4444-8444-444444444444", ResultArtifactRef: "artifact:sha256:" + workerHex('d')}); !errors.Is(err, ErrEvolutionTransitionConflict) {
		t.Fatalf("complete work on legacy terminal run error = %v", err)
	}
	var status string
	if err := store.db.QueryRow(`SELECT status FROM evolution_work_items WHERE work_id = ?`, work.WorkID).Scan(&status); err != nil || status != string(EvolutionWorkLeased) {
		t.Fatalf("legacy terminal completion work status = %q, %v", status, err)
	}
	afterEvents, afterOutbox := evolutionWorkerCounts(t, store, run.RunID)
	if afterEvents != beforeEvents || afterOutbox != beforeOutbox {
		t.Fatalf("terminal guards changed event/outbox counts %d/%d -> %d/%d", beforeEvents, beforeOutbox, afterEvents, afterOutbox)
	}
}

func TestEvolutionWorkerLegacyTerminalPendingOutboxDeadLettersAndReportsConflict(t *testing.T) {
	root := t.TempDir()
	clock := newEvolutionWorkerClock()
	store, err := OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	message, created, err := store.EnqueueEvolutionOutbox(EvolutionOutboxInput{
		IdempotencyKey: "22222222-2222-4222-8222-222222222222", RunID: run.RunID,
		Topic: "evolution.work.completed", PayloadRef: "artifact:sha256:" + workerHex('a'), MaxAttempts: 1,
	})
	if err != nil || !created {
		t.Fatalf("enqueue = %#v created=%v err=%v", message, created, err)
	}
	message, ok, err := store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("lease = %#v ok=%v err=%v", message, ok, err)
	}
	// Simulate a database created before terminal transitions were protected by pending outbox state.
	if _, err := store.db.Exec(`UPDATE evolution_runs SET status = 'superseded' WHERE run_id = ?`, run.RunID); err != nil {
		t.Fatal(err)
	}
	failure := EvolutionOutboxFailure{
		OutboxID: message.OutboxID, WorkerID: message.WorkerID, LeaseID: message.LeaseID, Attempt: message.Attempt,
		FailureIdempotencyKey: "33333333-3333-4333-8333-333333333333", FailureCode: "delivery_unavailable",
		FailureMessage: "bounded failure", RetryDelay: time.Second,
	}
	failed, deadLetter, err := store.FailEvolutionOutbox(failure)
	if !errors.Is(err, ErrEvolutionTransitionConflict) || !deadLetter || failed == nil || failed.Status != EvolutionOutboxDeadLetter {
		t.Fatalf("legacy deadletter = %#v dead=%v err=%v", failed, deadLetter, err)
	}
	firstJSON, err := json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}
	if replayed, replayDead, replayErr := store.FailEvolutionOutbox(failure); !errors.Is(replayErr, ErrEvolutionTransitionConflict) || !replayDead {
		t.Fatalf("same-process legacy replay = %#v dead=%v err=%v", replayed, replayDead, replayErr)
	} else if replayJSON, marshalErr := json.Marshal(replayed); marshalErr != nil || string(replayJSON) != string(firstJSON) {
		t.Fatalf("same-process legacy replay JSON = %s, %v; want %s", replayJSON, marshalErr, firstJSON)
	}
	var status string
	if err := store.db.QueryRow(`SELECT status FROM evolution_outbox WHERE outbox_id = ?`, message.OutboxID).Scan(&status); err != nil || status != string(EvolutionOutboxDeadLetter) {
		t.Fatalf("persisted legacy deadletter status = %q, %v", status, err)
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionSuperseded {
		t.Fatalf("legacy terminal run changed = %#v, %v", loaded, err)
	}
	events, err := store.ListEvents(run.RunID, "", 100)
	if err != nil || events[len(events)-1].EventType != "worker_failure" || events[len(events)-1].Code != "outbox_delivery_exhausted" {
		t.Fatalf("legacy conflict event = %#v, %v", events, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if replayed, replayDead, replayErr := store.FailEvolutionOutbox(failure); !errors.Is(replayErr, ErrEvolutionTransitionConflict) || !replayDead {
		t.Fatalf("reopen legacy replay = %#v dead=%v err=%v", replayed, replayDead, replayErr)
	} else if replayJSON, marshalErr := json.Marshal(replayed); marshalErr != nil || string(replayJSON) != string(firstJSON) {
		t.Fatalf("reopen legacy replay JSON = %s, %v; want %s", replayJSON, marshalErr, firstJSON)
	}
}

func TestEvolutionWorkerLegacyTerminalExhaustedWorkReplaysConflictAcrossReopen(t *testing.T) {
	root := t.TempDir()
	clock := newEvolutionWorkerClock()
	store, err := OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	work := enqueueEvolutionWorkerTestWork(t, store, run.RunID, 1)
	leased := leaseEvolutionWorkerTestWork(t, store, "worker-a")
	if _, err := store.db.Exec(`UPDATE evolution_runs SET status = 'superseded' WHERE run_id = ?`, run.RunID); err != nil {
		t.Fatal(err)
	}
	failure := EvolutionWorkFailure{
		WorkID: work.WorkID, WorkerID: leased.WorkerID, LeaseID: leased.LeaseID, Attempt: leased.Attempt,
		FailureIdempotencyKey: "22222222-2222-4222-8222-222222222222", FailureCode: "generation_failed",
		FailureMessage: "bounded worker failure", RetryDelay: time.Second,
	}
	failed, blocked, err := store.FailEvolutionWork(failure)
	if !errors.Is(err, ErrEvolutionTransitionConflict) || !blocked || failed == nil || failed.Status != EvolutionWorkBlocked {
		t.Fatalf("legacy work failure = %#v blocked=%v err=%v", failed, blocked, err)
	}
	firstJSON, err := json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}
	assertReplay := func(label string) {
		t.Helper()
		replayed, replayBlocked, replayErr := store.FailEvolutionWork(failure)
		if !errors.Is(replayErr, ErrEvolutionTransitionConflict) || !replayBlocked {
			t.Fatalf("%s replay = %#v blocked=%v err=%v", label, replayed, replayBlocked, replayErr)
		}
		replayJSON, marshalErr := json.Marshal(replayed)
		if marshalErr != nil || string(replayJSON) != string(firstJSON) {
			t.Fatalf("%s replay JSON = %s, %v; want %s", label, replayJSON, marshalErr, firstJSON)
		}
	}
	assertReplay("same-process")
	changed := failure
	changed.LeaseID = "lease-different"
	if _, _, err := store.FailEvolutionWork(changed); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("legacy work changed identity error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	assertReplay("reopen")
}

type evolutionWorkerTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newEvolutionWorkerClock() *evolutionWorkerTestClock {
	return &evolutionWorkerTestClock{now: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)}
}

func (c *evolutionWorkerTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *evolutionWorkerTestClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func openEvolutionWorkerTestStore(t *testing.T, clock *evolutionWorkerTestClock) *EvolutionControlStore {
	t.Helper()
	return openEvolutionWorkerTestStoreWithHooks(t, clock, evolutionStoreHooks{})
}

func openEvolutionWorkerTestStoreWithHooks(t *testing.T, clock *evolutionWorkerTestClock, hooks evolutionStoreHooks) *EvolutionControlStore {
	t.Helper()
	store, err := openEvolutionControlStore(t.TempDir(), clock.Now, hooks)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createEvolutionWorkerTestRun(t *testing.T, store *EvolutionControlStore, key string) *EvolutionRun {
	t.Helper()
	run, created, err := store.CreateRun(validEvolutionRunInput(key))
	if err != nil || !created {
		t.Fatalf("create run = %#v, %v, %v", run, created, err)
	}
	return run
}

func enqueueEvolutionWorkerTestWork(t *testing.T, store *EvolutionControlStore, runID string, maxAttempts int) *EvolutionWork {
	t.Helper()
	work, created, err := store.EnqueueEvolutionWork(EvolutionWorkInput{IdempotencyKey: "sha256:" + evolutionWorkerPayloadHash("work:"+runID), RunID: runID, Capability: EvolutionCapabilityAgent, ArtifactRef: "artifact:sha256:" + workerHex('a'), MaxAttempts: maxAttempts})
	if err != nil || !created {
		t.Fatalf("enqueue = %#v, %v, %v", work, created, err)
	}
	return work
}

func leaseEvolutionWorkerTestWork(t *testing.T, store *EvolutionControlStore, workerID string) *EvolutionWork {
	t.Helper()
	work, ok, err := store.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{WorkerID: workerID, Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityAgent}, LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("lease = %#v, %v, %v", work, ok, err)
	}
	return work
}

func evolutionWorkerCounts(t *testing.T, store *EvolutionControlStore, runID string) (int, int) {
	t.Helper()
	var events, outbox int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_events WHERE run_id = ?`, runID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_outbox WHERE run_id = ?`, runID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	return events, outbox
}

func workerHex(character rune) string { return strings.Repeat(string(character), 64) }
