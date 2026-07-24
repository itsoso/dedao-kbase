package app

import (
	"context"
	"errors"
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
