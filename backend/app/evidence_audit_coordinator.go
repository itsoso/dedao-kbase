package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type EvidenceAuditRunFunc func(
	context.Context,
	*BookKnowledgeStore,
	string,
	BookKnowledgeLLMClient,
	EvidenceAuditRunnerConfig,
) (*EvidenceAudit, error)

type EvidenceAuditCoordinatorConfig struct {
	Store        *BookKnowledgeStore
	Client       BookKnowledgeLLMClient
	RunnerConfig EvidenceAuditRunnerConfig
	Workers      int
	QueueSize    int
	PollInterval time.Duration
	Run          EvidenceAuditRunFunc
}

type EvidenceAuditCoordinator struct {
	store        *BookKnowledgeStore
	client       BookKnowledgeLLMClient
	runnerConfig EvidenceAuditRunnerConfig
	workers      int
	pollInterval time.Duration
	run          EvidenceAuditRunFunc
	queue        chan string

	mu      sync.Mutex
	started bool
	stopped bool
	pending map[string]struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewEvidenceAuditCoordinator(config EvidenceAuditCoordinatorConfig) (*EvidenceAuditCoordinator, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("evidence audit coordinator store is required")
	}
	if config.Workers <= 0 {
		config.Workers = 2
	}
	if config.Workers > 32 {
		return nil, fmt.Errorf("evidence audit coordinator workers must be between 1 and 32")
	}
	if config.QueueSize <= 0 {
		config.QueueSize = config.Workers * 4
	}
	if config.QueueSize > 4096 {
		return nil, fmt.Errorf("evidence audit coordinator queue size must be between 1 and 4096")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	run := config.Run
	if run == nil {
		run = RunEvidenceAudit
	}
	return &EvidenceAuditCoordinator{
		store: config.Store, client: config.Client, runnerConfig: config.RunnerConfig,
		workers: config.Workers, pollInterval: config.PollInterval, run: run,
		queue: make(chan string, config.QueueSize), pending: map[string]struct{}{},
	}, nil
}

func (c *EvidenceAuditCoordinator) Start(parent context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return nil
	}
	if c.stopped {
		return fmt.Errorf("evidence audit coordinator is stopped")
	}
	if parent == nil {
		parent = context.Background()
	}
	c.ctx, c.cancel = context.WithCancel(parent)
	c.started = true
	for index := 0; index < c.workers; index++ {
		c.wg.Add(1)
		go c.worker()
	}
	c.wg.Add(1)
	go c.poll()
	return nil
}

func (c *EvidenceAuditCoordinator) Enqueue(auditID string) error {
	c.mu.Lock()
	if !c.started || c.stopped {
		c.mu.Unlock()
		return fmt.Errorf("evidence audit coordinator is not running")
	}
	if _, exists := c.pending[auditID]; exists {
		c.mu.Unlock()
		return nil
	}
	c.pending[auditID] = struct{}{}
	queue := c.queue
	ctx := c.ctx
	c.mu.Unlock()
	select {
	case queue <- auditID:
		return nil
	case <-ctx.Done():
		c.removePending(auditID)
		return ctx.Err()
	default:
		// The durable store remains authoritative. The poller will discover this
		// queued audit after in-memory pressure subsides.
		c.removePending(auditID)
		return nil
	}
}

func (c *EvidenceAuditCoordinator) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return nil
	}
	c.stopped = true
	cancel := c.cancel
	started := c.started
	c.mu.Unlock()
	if !started {
		return nil
	}
	cancel()
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *EvidenceAuditCoordinator) poll() {
	defer c.wg.Done()
	c.scan()
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.scan()
		}
	}
}

func (c *EvidenceAuditCoordinator) scan() {
	auditIDs, err := c.recoverableAuditIDs()
	if err != nil {
		return
	}
	for _, auditID := range auditIDs {
		if err := c.Enqueue(auditID); err != nil && !errors.Is(err, context.Canceled) {
			return
		}
	}
}

func (c *EvidenceAuditCoordinator) recoverableAuditIDs() ([]string, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	unlockRoot, err := c.store.acquireEvidenceAuditRootLock()
	if err != nil {
		return nil, err
	}
	defer unlockRoot()
	manifest, err := c.store.loadEvidenceAuditManifestUnlocked()
	if err != nil {
		return nil, err
	}
	auditIDs := make([]string, 0)
	for index := range manifest.Audits {
		record := &manifest.Audits[index]
		if record.Status != EvidenceAuditQueued && record.Status != EvidenceAuditRunning {
			continue
		}
		if err := c.store.reconcileEvidenceAuditTerminalUnlocked(manifest, record); err != nil {
			return nil, err
		}
		if record.Status == EvidenceAuditQueued || record.Status == EvidenceAuditRunning {
			auditIDs = append(auditIDs, record.AuditID)
		}
	}
	return auditIDs, nil
}

func (c *EvidenceAuditCoordinator) worker() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case auditID := <-c.queue:
			_, _ = c.run(c.ctx, c.store, auditID, c.client, c.runnerConfig)
			c.removePending(auditID)
		}
	}
}

func (c *EvidenceAuditCoordinator) removePending(auditID string) {
	c.mu.Lock()
	delete(c.pending, auditID)
	c.mu.Unlock()
}
