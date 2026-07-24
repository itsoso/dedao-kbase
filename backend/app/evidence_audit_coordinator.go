package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrEvidenceAuditQueueFull = errors.New("evidence audit coordinator queue is full")

type EvidenceAuditCoordinatorEventType string

const (
	EvidenceAuditCoordinatorScanFailed      EvidenceAuditCoordinatorEventType = "scan_failed"
	EvidenceAuditCoordinatorExecutionFailed EvidenceAuditCoordinatorEventType = "execution_failed"
	EvidenceAuditCoordinatorQueueFull       EvidenceAuditCoordinatorEventType = "queue_full"
	EvidenceAuditCoordinatorLeaseLost       EvidenceAuditCoordinatorEventType = "lease_lost"
)

type EvidenceAuditCoordinatorEvent struct {
	Type       EvidenceAuditCoordinatorEventType
	AuditID    string
	ErrorCode  string
	Attempt    int
	RetryAfter time.Duration
}

type EvidenceAuditRunFunc func(
	context.Context,
	*BookKnowledgeStore,
	string,
	BookKnowledgeLLMClient,
	EvidenceAuditRunnerConfig,
) (*EvidenceAudit, error)

type EvidenceAuditCoordinatorConfig struct {
	Store             *BookKnowledgeStore
	Client            BookKnowledgeLLMClient
	RunnerConfig      EvidenceAuditRunnerConfig
	Workers           int
	QueueSize         int
	PollInterval      time.Duration
	OwnerID           string
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RecoveryBatch     int
	BackoffInitial    time.Duration
	BackoffMax        time.Duration
	Jitter            func(time.Duration) time.Duration
	Metrics           func(EvidenceAuditCoordinatorEvent)
	Now               func() time.Time
	Run               EvidenceAuditRunFunc
}

type EvidenceAuditCoordinator struct {
	store             *BookKnowledgeStore
	client            BookKnowledgeLLMClient
	runnerConfig      EvidenceAuditRunnerConfig
	workers           int
	pollInterval      time.Duration
	ownerID           string
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	recoveryBatch     int
	backoffInitial    time.Duration
	backoffMax        time.Duration
	jitter            func(time.Duration) time.Duration
	metrics           func(EvidenceAuditCoordinatorEvent)
	now               func() time.Time
	run               EvidenceAuditRunFunc
	queue             chan string

	mu           sync.Mutex
	started      bool
	stopped      bool
	pending      map[string]struct{}
	cursor       string
	scanFailures int
	scanRetryAt  time.Time
	runFailures  map[string]int
	runRetryAt   map[string]time.Time
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
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
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 30 * time.Second
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = config.LeaseDuration / 3
	}
	if config.HeartbeatInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("evidence audit heartbeat must be positive and shorter than lease duration")
	}
	if config.RecoveryBatch <= 0 {
		config.RecoveryBatch = 100
	}
	if config.RecoveryBatch > 500 {
		return nil, fmt.Errorf("evidence audit recovery batch must be between 1 and 500")
	}
	if config.BackoffInitial <= 0 {
		config.BackoffInitial = 250 * time.Millisecond
	}
	if config.BackoffMax <= 0 {
		config.BackoffMax = 30 * time.Second
	}
	if config.BackoffMax < config.BackoffInitial {
		return nil, fmt.Errorf("evidence audit backoff max must not be shorter than initial")
	}
	if config.Jitter == nil {
		config.Jitter = func(delay time.Duration) time.Duration { return delay }
	}
	if config.Metrics == nil {
		config.Metrics = func(EvidenceAuditCoordinatorEvent) {}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.OwnerID == "" {
		config.OwnerID = newEvidenceAuditCoordinatorOwnerID()
	}
	run := config.Run
	if run == nil {
		run = RunEvidenceAudit
	}
	return &EvidenceAuditCoordinator{
		store: config.Store, client: config.Client, runnerConfig: config.RunnerConfig,
		workers: config.Workers, pollInterval: config.PollInterval,
		ownerID: config.OwnerID, leaseDuration: config.LeaseDuration,
		heartbeatInterval: config.HeartbeatInterval, recoveryBatch: config.RecoveryBatch,
		backoffInitial: config.BackoffInitial, backoffMax: config.BackoffMax,
		jitter: config.Jitter, metrics: config.Metrics, now: config.Now, run: run,
		queue: make(chan string, config.QueueSize), pending: map[string]struct{}{},
		runFailures: map[string]int{}, runRetryAt: map[string]time.Time{},
	}, nil
}

func newEvidenceAuditCoordinatorOwnerID() string {
	var payload [12]byte
	if _, err := rand.Read(payload[:]); err != nil {
		return fmt.Sprintf("coordinator-%d", time.Now().UnixNano())
	}
	return "coordinator-" + hex.EncodeToString(payload[:])
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
	ctx := c.ctx
	c.mu.Unlock()

	claim, err := c.store.ClaimEvidenceAuditLease(auditID, c.ownerID, c.now(), c.leaseDuration)
	if err != nil {
		return err
	}
	if !claim.Claimed {
		return nil
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		_ = c.store.ReleaseEvidenceAuditLease(auditID, c.ownerID, c.now())
		return fmt.Errorf("evidence audit coordinator is not running")
	}
	if _, exists := c.pending[auditID]; exists {
		c.mu.Unlock()
		return nil
	}
	c.pending[auditID] = struct{}{}
	queue := c.queue
	c.mu.Unlock()
	select {
	case queue <- auditID:
		return nil
	case <-ctx.Done():
		c.removePending(auditID)
		_ = c.store.ReleaseEvidenceAuditLease(auditID, c.ownerID, c.now())
		return ctx.Err()
	default:
		c.removePending(auditID)
		_ = c.store.ReleaseEvidenceAuditLease(auditID, c.ownerID, c.now())
		c.emit(EvidenceAuditCoordinatorEvent{
			Type: EvidenceAuditCoordinatorQueueFull, AuditID: auditID, ErrorCode: "queue_full",
		})
		return ErrEvidenceAuditQueueFull
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
	now := c.now()
	c.mu.Lock()
	if now.Before(c.scanRetryAt) {
		c.mu.Unlock()
		return
	}
	cursor := c.cursor
	c.mu.Unlock()
	page, err := c.store.ListRecoverableEvidenceAuditsPage(cursor, c.recoveryBatch, now)
	if err != nil {
		c.mu.Lock()
		c.scanFailures++
		attempt := c.scanFailures
		c.mu.Unlock()
		delay := c.backoffDelay(attempt)
		c.mu.Lock()
		c.scanRetryAt = now.Add(delay)
		c.mu.Unlock()
		c.emit(EvidenceAuditCoordinatorEvent{
			Type: EvidenceAuditCoordinatorScanFailed, ErrorCode: "store_unavailable",
			Attempt: attempt, RetryAfter: delay,
		})
		return
	}
	c.mu.Lock()
	c.scanFailures = 0
	c.scanRetryAt = time.Time{}
	c.cursor = page.NextCursor
	c.mu.Unlock()
	for _, record := range page.Records {
		c.mu.Lock()
		retryAt := c.runRetryAt[record.AuditID]
		c.mu.Unlock()
		if now.Before(retryAt) {
			continue
		}
		err := c.Enqueue(record.AuditID)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrEvidenceAuditQueueFull) {
			return
		}
	}
}

func (c *EvidenceAuditCoordinator) worker() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case auditID := <-c.queue:
			c.execute(auditID)
		}
	}
}

func (c *EvidenceAuditCoordinator) execute(auditID string) {
	ctx, cancel := context.WithCancel(c.ctx)
	heartbeatDone := make(chan struct{})
	heartbeatErr := make(chan error, 1)
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(c.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := c.store.RenewEvidenceAuditLease(
					auditID, c.ownerID, c.now(), c.leaseDuration,
				); err != nil {
					heartbeatErr <- err
					cancel()
					return
				}
			}
		}
	}()
	config := c.runnerConfig
	config.LeaseOwner = c.ownerID
	_, runErr := c.run(ctx, c.store, auditID, c.client, config)
	cancel()
	<-heartbeatDone
	select {
	case err := <-heartbeatErr:
		runErr = err
	default:
	}
	var releaseErr error
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		c.mu.Lock()
		c.runFailures[auditID]++
		attempt := c.runFailures[auditID]
		c.mu.Unlock()
		delay := c.backoffDelay(attempt)
		c.mu.Lock()
		c.runRetryAt[auditID] = c.now().Add(delay)
		c.mu.Unlock()
		if !errors.Is(runErr, ErrEvidenceAuditLeaseLost) {
			_, releaseErr = c.store.RenewEvidenceAuditLease(
				auditID, c.ownerID, c.now(), delay,
			)
		}
		eventType := EvidenceAuditCoordinatorExecutionFailed
		errorCode := "execution_failed"
		if errors.Is(runErr, ErrEvidenceAuditLeaseLost) || errors.Is(releaseErr, ErrEvidenceAuditLeaseLost) {
			eventType = EvidenceAuditCoordinatorLeaseLost
			errorCode = "lease_lost"
		}
		c.emit(EvidenceAuditCoordinatorEvent{
			Type: eventType, AuditID: auditID, ErrorCode: errorCode,
			Attempt: attempt, RetryAfter: delay,
		})
	} else {
		releaseErr = c.store.ReleaseEvidenceAuditLease(auditID, c.ownerID, c.now())
		c.mu.Lock()
		delete(c.runFailures, auditID)
		delete(c.runRetryAt, auditID)
		c.mu.Unlock()
	}
	c.removePending(auditID)
}

func (c *EvidenceAuditCoordinator) removePending(auditID string) {
	c.mu.Lock()
	delete(c.pending, auditID)
	c.mu.Unlock()
}

func (c *EvidenceAuditCoordinator) backoffDelay(attempt int) time.Duration {
	delay := c.backoffInitial
	for index := 1; index < attempt && delay < c.backoffMax; index++ {
		if delay > c.backoffMax/2 {
			delay = c.backoffMax
			break
		}
		delay *= 2
	}
	if delay > c.backoffMax {
		delay = c.backoffMax
	}
	delay = c.jitter(delay)
	if delay < 0 {
		return 0
	}
	return delay
}

func (c *EvidenceAuditCoordinator) emit(event EvidenceAuditCoordinatorEvent) {
	c.metrics(event)
}
