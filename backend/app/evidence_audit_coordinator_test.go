package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEvidenceAuditCoordinatorBoundsConcurrencyAndRecoversPersistedQueue(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := testAgentPackageTime()
	var auditIDs []string
	for index := 0; index < 4; index++ {
		input := validEvidenceAuditInput()
		input.Subject = input.Subject + string(rune('a'+index))
		audit, _, err := CreateEvidenceAudit(store, input, "coordinator-recovery-"+string(rune('a'+index)), now)
		if err != nil {
			t.Fatalf("CreateEvidenceAudit() error = %v", err)
		}
		auditIDs = append(auditIDs, audit.AuditID)
	}

	var active atomic.Int32
	var maximum atomic.Int32
	release := make(chan struct{})
	finished := make(chan string, len(auditIDs))
	run := func(ctx context.Context, _ *BookKnowledgeStore, auditID string, _ BookKnowledgeLLMClient, _ EvidenceAuditRunnerConfig) (*EvidenceAudit, error) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
		finished <- auditID
		return &EvidenceAudit{AuditID: auditID, Status: EvidenceAuditCompleted}, nil
	}
	coordinator, err := NewEvidenceAuditCoordinator(EvidenceAuditCoordinatorConfig{
		Store: store, Workers: 2, QueueSize: 1, PollInterval: 10 * time.Millisecond,
		Run: run,
	})
	if err != nil {
		t.Fatalf("NewEvidenceAuditCoordinator() error = %v", err)
	}
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = coordinator.Shutdown(ctx)
	})

	deadline := time.Now().Add(time.Second)
	for active.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := active.Load(); got != 2 {
		t.Fatalf("active workers = %d, want 2", got)
	}
	if got := maximum.Load(); got > 2 {
		t.Fatalf("maximum concurrency = %d, want <= 2", got)
	}
	close(release)
	seen := map[string]bool{}
	for len(seen) < len(auditIDs) {
		select {
		case auditID := <-finished:
			seen[auditID] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("recovered audits = %v, want %v", seen, auditIDs)
		}
	}
}

func TestEvidenceAuditCoordinatorDoesNotAutomaticallyRetryFailedAudit(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	audit, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "coordinator-failed", testAgentPackageTime())
	if err != nil {
		t.Fatalf("CreateEvidenceAudit() error = %v", err)
	}
	if _, err := StartEvidenceAudit(store, audit.AuditID, "trace-failed", testAgentPackageTime().Add(time.Minute)); err != nil {
		t.Fatalf("StartEvidenceAudit() error = %v", err)
	}
	if _, err := FailEvidenceAudit(store, audit.AuditID, "invalid_model_output", "failed", testAgentPackageTime().Add(2*time.Minute)); err != nil {
		t.Fatalf("FailEvidenceAudit() error = %v", err)
	}

	var calls atomic.Int32
	coordinator, err := NewEvidenceAuditCoordinator(EvidenceAuditCoordinatorConfig{
		Store: store, Workers: 1, QueueSize: 1, PollInterval: 10 * time.Millisecond,
		Run: func(context.Context, *BookKnowledgeStore, string, BookKnowledgeLLMClient, EvidenceAuditRunnerConfig) (*EvidenceAudit, error) {
			calls.Add(1)
			return nil, errors.New("must not run")
		},
	})
	if err != nil {
		t.Fatalf("NewEvidenceAuditCoordinator() error = %v", err)
	}
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := coordinator.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("runner calls = %d, want 0", got)
	}
}

func TestEvidenceAuditCoordinatorShutdownCancelsAndWaitsWithinContext(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	audit, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "coordinator-shutdown", testAgentPackageTime())
	if err != nil {
		t.Fatalf("CreateEvidenceAudit() error = %v", err)
	}
	started := make(chan struct{})
	var once sync.Once
	coordinator, err := NewEvidenceAuditCoordinator(EvidenceAuditCoordinatorConfig{
		Store: store, Workers: 1, QueueSize: 1, PollInterval: time.Hour,
		Run: func(ctx context.Context, _ *BookKnowledgeStore, auditID string, _ BookKnowledgeLLMClient, _ EvidenceAuditRunnerConfig) (*EvidenceAudit, error) {
			if auditID != audit.AuditID {
				t.Fatalf("auditID = %q, want %q", auditID, audit.AuditID)
			}
			once.Do(func() { close(started) })
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("NewEvidenceAuditCoordinator() error = %v", err)
	}
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := coordinator.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestEvidenceAuditCoordinatorsUsePersistentLeaseAcrossInstances(t *testing.T) {
	root := t.TempDir()
	storeA := NewBookKnowledgeStore(root)
	storeB := NewBookKnowledgeStore(root)
	now := testAgentPackageTime()
	audit, _, err := CreateEvidenceAudit(storeA, validEvidenceAuditInput(), "multi-instance-lease", now)
	if err != nil {
		t.Fatalf("CreateEvidenceAudit() error = %v", err)
	}

	var calls atomic.Int32
	started := make(chan string, 2)
	release := make(chan struct{})
	run := func(ctx context.Context, _ *BookKnowledgeStore, auditID string, _ BookKnowledgeLLMClient, config EvidenceAuditRunnerConfig) (*EvidenceAudit, error) {
		calls.Add(1)
		started <- config.LeaseOwner
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return &EvidenceAudit{AuditID: auditID, Status: EvidenceAuditCompleted}, nil
		}
	}
	newCoordinator := func(store *BookKnowledgeStore, owner string) *EvidenceAuditCoordinator {
		coordinator, err := NewEvidenceAuditCoordinator(EvidenceAuditCoordinatorConfig{
			Store: store, OwnerID: owner, Workers: 1, QueueSize: 1,
			PollInterval: 5 * time.Millisecond, LeaseDuration: time.Second,
			HeartbeatInterval: 100 * time.Millisecond, RecoveryBatch: 10, Run: run,
		})
		if err != nil {
			t.Fatalf("NewEvidenceAuditCoordinator(%s) error = %v", owner, err)
		}
		if err := coordinator.Start(context.Background()); err != nil {
			t.Fatalf("Start(%s) error = %v", owner, err)
		}
		return coordinator
	}
	first := newCoordinator(storeA, "coordinator-a")
	second := newCoordinator(storeB, "coordinator-b")
	t.Cleanup(func() {
		close(release)
		for _, coordinator := range []*EvidenceAuditCoordinator{first, second} {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = coordinator.Shutdown(ctx)
			cancel()
		}
	})

	select {
	case owner := <-started:
		if owner != "coordinator-a" && owner != "coordinator-b" {
			t.Fatalf("runner lease owner = %q", owner)
		}
	case <-time.After(time.Second):
		t.Fatal("leased audit did not start")
	}
	time.Sleep(350 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("runner calls = %d, want exactly 1 while heartbeat renews lease", got)
	}
	loaded, err := storeA.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		t.Fatalf("LoadEvidenceAudit() error = %v", err)
	}
	if loaded.Status == EvidenceAuditFailed {
		t.Fatalf("audit was incorrectly failed by competing coordinator: %+v", loaded)
	}
}

func TestEvidenceAuditCoordinatorRecoversExpiredLease(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := testAgentPackageTime()
	audit, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "expired-lease", now)
	if err != nil {
		t.Fatalf("CreateEvidenceAudit() error = %v", err)
	}
	if _, err := store.ClaimEvidenceAuditLease(audit.AuditID, "dead-owner", now, 10*time.Millisecond); err != nil {
		t.Fatalf("ClaimEvidenceAuditLease(dead-owner) error = %v", err)
	}

	started := make(chan EvidenceAuditRunnerConfig, 1)
	coordinator, err := NewEvidenceAuditCoordinator(EvidenceAuditCoordinatorConfig{
		Store: store, OwnerID: "recovery-owner", Workers: 1, QueueSize: 1,
		PollInterval: 5 * time.Millisecond, LeaseDuration: time.Minute,
		HeartbeatInterval: 10 * time.Millisecond, RecoveryBatch: 10,
		Now: func() time.Time { return now.Add(time.Second) },
		Run: func(_ context.Context, _ *BookKnowledgeStore, _ string, _ BookKnowledgeLLMClient, config EvidenceAuditRunnerConfig) (*EvidenceAudit, error) {
			started <- config
			return audit, nil
		},
	})
	if err != nil {
		t.Fatalf("NewEvidenceAuditCoordinator() error = %v", err)
	}
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = coordinator.Shutdown(ctx)
		cancel()
	})
	select {
	case config := <-started:
		if config.LeaseOwner != "recovery-owner" {
			t.Fatalf("LeaseOwner = %q", config.LeaseOwner)
		}
	case <-time.After(time.Second):
		t.Fatal("expired lease was not recovered")
	}
	record, err := store.LoadEvidenceAuditRecord(audit.AuditID)
	if err != nil {
		t.Fatalf("LoadEvidenceAuditRecord() error = %v", err)
	}
	if record.LeaseAttempt != 2 {
		t.Fatalf("LeaseAttempt = %d, want 2", record.LeaseAttempt)
	}
}

func TestEvidenceAuditCoordinatorQueueFullReleasesLeaseAndEmitsMetric(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := testAgentPackageTime()
	audits := make([]*EvidenceAudit, 0, 3)
	for index := 0; index < 3; index++ {
		input := validEvidenceAuditInput()
		input.Subject = fmt.Sprintf("queue-pressure-%d", index)
		audit, _, err := CreateEvidenceAudit(store, input, fmt.Sprintf("queue-pressure-%d", index), now)
		if err != nil {
			t.Fatalf("CreateEvidenceAudit(%d) error = %v", index, err)
		}
		audits = append(audits, audit)
	}
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	events := make(chan EvidenceAuditCoordinatorEvent, 10)
	coordinator, err := NewEvidenceAuditCoordinator(EvidenceAuditCoordinatorConfig{
		Store: store, OwnerID: "queue-owner", Workers: 1, QueueSize: 1,
		PollInterval: time.Hour, LeaseDuration: time.Minute,
		HeartbeatInterval: 10 * time.Second, RecoveryBatch: 1,
		Metrics: func(event EvidenceAuditCoordinatorEvent) { events <- event },
		Run: func(ctx context.Context, _ *BookKnowledgeStore, _ string, _ BookKnowledgeLLMClient, _ EvidenceAuditRunnerConfig) (*EvidenceAudit, error) {
			started <- struct{}{}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-block:
				return audits[0], nil
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(block)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = coordinator.Shutdown(ctx)
		cancel()
	})
	if err := coordinator.Enqueue(audits[0].AuditID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first audit did not start")
	}
	if err := coordinator.Enqueue(audits[1].AuditID); err != nil {
		t.Fatalf("second enqueue error = %v", err)
	}
	if err := coordinator.Enqueue(audits[2].AuditID); !errors.Is(err, ErrEvidenceAuditQueueFull) {
		t.Fatalf("third enqueue error = %v, want queue full", err)
	}
	record, err := store.LoadEvidenceAuditRecord(audits[2].AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if record.LeaseOwner != "" {
		t.Fatalf("queue-full audit retained lease owner %q", record.LeaseOwner)
	}
	select {
	case event := <-events:
		for event.Type != EvidenceAuditCoordinatorQueueFull {
			select {
			case event = <-events:
			default:
				t.Fatalf("queue_full event not emitted; last=%+v", event)
			}
		}
		if event.AuditID != audits[2].AuditID || event.ErrorCode != "queue_full" {
			t.Fatalf("queue_full event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("queue_full metric was not emitted")
	}
}

func TestEvidenceAuditCoordinatorScanErrorsBackOffWithInjectedJitter(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	now := testAgentPackageTime()
	current := now
	events := make([]EvidenceAuditCoordinatorEvent, 0, 4)
	coordinator, err := NewEvidenceAuditCoordinator(EvidenceAuditCoordinatorConfig{
		Store: store, OwnerID: "backoff-owner", Workers: 1, QueueSize: 1,
		PollInterval: time.Millisecond, LeaseDuration: time.Minute,
		HeartbeatInterval: 10 * time.Second, RecoveryBatch: 10,
		Now:            func() time.Time { return current },
		BackoffInitial: 100 * time.Millisecond, BackoffMax: time.Second,
		Jitter:  func(delay time.Duration) time.Duration { return delay + 7*time.Millisecond },
		Metrics: func(event EvidenceAuditCoordinatorEvent) { events = append(events, event) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.EvidenceAuditDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.EvidenceAuditManifestPath(), []byte("{private/path/token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.EvidenceAuditManifestPath()+".bak", []byte("{also-broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	coordinator.scan()
	coordinator.scan()
	if len(events) != 1 {
		t.Fatalf("events before backoff expiry = %+v", events)
	}
	if events[0].Type != EvidenceAuditCoordinatorScanFailed ||
		events[0].ErrorCode != "store_unavailable" ||
		events[0].RetryAfter != 107*time.Millisecond {
		t.Fatalf("first scan event = %+v", events[0])
	}
	current = current.Add(108 * time.Millisecond)
	coordinator.scan()
	if len(events) != 2 || events[1].RetryAfter != 207*time.Millisecond {
		t.Fatalf("second scan events = %+v", events)
	}
	for _, event := range events {
		if strings.Contains(event.ErrorCode, root) || strings.Contains(event.ErrorCode, "token") {
			t.Fatalf("metric leaked private detail: %+v", event)
		}
	}
}
