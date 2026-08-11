package app

import (
	"errors"
	"fmt"
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
	storeA, err := openEvolutionControlStore(root, clock.Now, evolutionStoreHooks{afterBeginTx: func() error {
		close(entered)
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storeA.Close() })
	storeB, err := OpenEvolutionControlStore(root, clock.Now)
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
	close(release)
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
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: firstLeaseID, ResultIdempotencyKey: "33333333-3333-4333-8333-333333333333", ResultArtifactRef: "artifact:sha256:" + workerHex('b')}); !errors.Is(err, ErrEvolutionLeaseLost) {
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
	clock := newEvolutionWorkerClock()
	store := openEvolutionWorkerTestStore(t, clock)
	run := createEvolutionWorkerTestRun(t, store, "11111111-1111-4111-8111-111111111111")
	work := enqueueEvolutionWorkerTestWork(t, store, run.RunID, 2)
	leased := leaseEvolutionWorkerTestWork(t, store, "worker-a")
	failed, blocked, err := store.FailEvolutionWork(EvolutionWorkFailure{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: leased.LeaseID, FailureCode: "generation_failed", FailureMessage: "bounded worker failure", RetryDelay: time.Minute})
	if err != nil || blocked || failed.Status != EvolutionWorkPending {
		t.Fatalf("first failure = %#v blocked=%v err=%v", failed, blocked, err)
	}
	if _, ok, err := store.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{WorkerID: "worker-b", Capabilities: []EvolutionWorkerCapability{EvolutionCapabilityAgent}, LeaseDuration: time.Minute}); err != nil || ok {
		t.Fatalf("early retry lease ok=%v err=%v", ok, err)
	}
	clock.Advance(time.Minute)
	leased = leaseEvolutionWorkerTestWork(t, store, "worker-b")
	failed, blocked, err = store.FailEvolutionWork(EvolutionWorkFailure{WorkID: work.WorkID, WorkerID: "worker-b", LeaseID: leased.LeaseID, FailureCode: "generation_failed", FailureMessage: "bounded worker failure", RetryDelay: time.Minute})
	if err != nil || !blocked || failed.Status != EvolutionWorkBlocked {
		t.Fatalf("exhausted failure = %#v blocked=%v err=%v", failed, blocked, err)
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil || loaded.Status != EvolutionBlocked {
		t.Fatalf("blocked run = %#v, %v", loaded, err)
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
		WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: leased.LeaseID,
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
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: secondWork.WorkID, WorkerID: "worker-b", LeaseID: secondLease.LeaseID, ResultIdempotencyKey: completion.ResultIdempotencyKey, ResultArtifactRef: completion.ResultArtifactRef}); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("cross-work result replay error = %v", err)
	}

	injected := errors.New("outbox insert failed")
	rollbackStore := openEvolutionWorkerTestStoreWithHooks(t, newEvolutionWorkerClock(), evolutionStoreHooks{beforeOutboxInsert: func() error { return injected }})
	rollbackRun := createEvolutionWorkerTestRun(t, rollbackStore, "55555555-5555-4555-8555-555555555555")
	rollbackWork := enqueueEvolutionWorkerTestWork(t, rollbackStore, rollbackRun.RunID, 3)
	rollbackLease := leaseEvolutionWorkerTestWork(t, rollbackStore, "worker-a")
	if _, _, err := rollbackStore.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: rollbackWork.WorkID, WorkerID: "worker-a", LeaseID: rollbackLease.LeaseID, ResultIdempotencyKey: "66666666-6666-4666-8666-666666666666", ResultArtifactRef: "artifact:sha256:" + workerHex('d')}); !errors.Is(err, injected) {
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
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: leased.LeaseID, ResultIdempotencyKey: "22222222-2222-4222-8222-222222222222", ResultArtifactRef: "artifact:sha256:" + workerHex('b')}); err != nil {
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
	delivered, replay, err := store.DeliverEvolutionOutbox(EvolutionOutboxDelivery{OutboxID: message.OutboxID, WorkerID: "dispatcher-b", LeaseID: message.LeaseID, ReceiptID: receipt})
	if err != nil || replay || delivered.Status != EvolutionOutboxDelivered {
		t.Fatalf("deliver = %#v, replay=%v err=%v", delivered, replay, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenEvolutionControlStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	delivered, replay, err = store.DeliverEvolutionOutbox(EvolutionOutboxDelivery{OutboxID: message.OutboxID, WorkerID: "dispatcher-b", LeaseID: message.LeaseID, ReceiptID: receipt})
	if err != nil || !replay || delivered.ReceiptID != receipt {
		t.Fatalf("persisted receipt replay = %#v, replay=%v err=%v", delivered, replay, err)
	}
	if _, _, err := store.DeliverEvolutionOutbox(EvolutionOutboxDelivery{OutboxID: message.OutboxID, WorkerID: "dispatcher-b", LeaseID: message.LeaseID, ReceiptID: "44444444-4444-4444-8444-444444444444"}); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("different receipt error = %v", err)
	}

	secondRun := createEvolutionWorkerTestRun(t, store, "55555555-5555-4555-8555-555555555555")
	secondWork := enqueueEvolutionWorkerTestWork(t, store, secondRun.RunID, 3)
	secondLease := leaseEvolutionWorkerTestWork(t, store, "worker-c")
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: secondWork.WorkID, WorkerID: "worker-c", LeaseID: secondLease.LeaseID, ResultIdempotencyKey: "66666666-6666-4666-8666-666666666666", ResultArtifactRef: "artifact:sha256:" + workerHex('e')}); err != nil {
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
		dead, deadLettered, err = store.FailEvolutionOutbox(EvolutionOutboxFailure{OutboxID: dead.OutboxID, WorkerID: "dispatcher-c", LeaseID: dead.LeaseID, FailureCode: "delivery_unavailable", FailureMessage: "bounded failure", RetryDelay: time.Second})
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
	blocked, err := store.LoadRun(secondRun.RunID)
	if err != nil || blocked.Status != EvolutionBlocked || blocked.FailureCode != "outbox_delivery_exhausted" || len([]rune(blocked.FailureMessage)) > EvolutionFailureMessageMaxRunes {
		t.Fatalf("deadletter run = %#v, %v", blocked, err)
	}
	if replayed, replayDead, err := store.FailEvolutionOutbox(EvolutionOutboxFailure{OutboxID: dead.OutboxID, WorkerID: "dispatcher-c", LeaseID: deadLetterLeaseID, FailureCode: "delivery_unavailable", FailureMessage: "bounded failure", RetryDelay: time.Second}); err != nil || !replayDead || replayed.Status != EvolutionOutboxDeadLetter {
		t.Fatalf("deadletter replay = %#v, %v, %v", replayed, replayDead, err)
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
	if _, _, err := store.CompleteEvolutionWork(EvolutionWorkCompletion{WorkID: work.WorkID, WorkerID: "worker-a", LeaseID: leased.LeaseID, ResultIdempotencyKey: "22222222-2222-4222-8222-222222222222", ResultArtifactRef: "artifact:sha256:" + workerHex('f')}); err != nil {
		t.Fatal(err)
	}
	message, ok, err := store.LeaseNextEvolutionOutbox(EvolutionOutboxLeaseInput{WorkerID: "dispatcher", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("lease = %#v, %v, %v", message, ok, err)
	}
	if _, err := store.db.Exec(`UPDATE evolution_outbox SET max_attempts = 1 WHERE outbox_id = ?`, message.OutboxID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.FailEvolutionOutbox(EvolutionOutboxFailure{OutboxID: message.OutboxID, WorkerID: "dispatcher", LeaseID: message.LeaseID, FailureCode: "delivery_unavailable", FailureMessage: "failure", RetryDelay: time.Second}); !errors.Is(err, injected) {
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
