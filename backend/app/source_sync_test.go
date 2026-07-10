package app

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestSourceSyncStoreMigratesEmptyRoot(t *testing.T) {
	root := t.TempDir()
	store, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}

	db, err := sql.Open("sqlite3", filepath.Join(root, sourceSyncDBName))
	if err != nil {
		t.Fatalf("open source sync db: %v", err)
	}
	defer db.Close()

	for _, table := range []string{
		"source_agents",
		"source_subscriptions",
		"source_sync_runs",
		"source_sync_items",
		"source_documents",
		"source_outbox_receipts",
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("query table %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %s count = %d", table, count)
		}
	}
	if store.DBPath() != filepath.Join(root, sourceSyncDBName) {
		t.Fatalf("db path = %q", store.DBPath())
	}
}

func TestSourceSyncStorePersistsLifecycleAndCounters(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	root := t.TempDir()
	store, err := newSourceSyncStore(root, clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}

	agent, err := store.HeartbeatAgent(SourceAgentHeartbeat{
		AgentID:       "agent-a",
		Version:       "1.0.0",
		Capabilities:  []string{"sync_content", "existing_articles"},
		WCPlusHealthy: true,
		WCPlusVersion: "4.2.0",
	})
	if err != nil {
		t.Fatalf("heartbeat agent: %v", err)
	}
	if agent.AgentID != "agent-a" || agent.LastHeartbeatAt != clock.Now().Format(time.RFC3339Nano) {
		t.Fatalf("unexpected agent: %#v", agent)
	}

	clock.Advance(time.Minute)
	agent, err = store.HeartbeatAgent(SourceAgentHeartbeat{
		AgentID:       "agent-a",
		Version:       "1.0.1",
		Capabilities:  []string{"sync_content"},
		WCPlusHealthy: false,
		LastError:     "wcplus unavailable",
	})
	if err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}
	if agent.Version != "1.0.1" || agent.WCPlusHealthy || agent.LastError == "" {
		t.Fatalf("heartbeat was not updated: %#v", agent)
	}
	agents, err := store.ListAgents()
	if err != nil || len(agents) != 1 {
		t.Fatalf("list agents = %#v, err=%v", agents, err)
	}

	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-med",
		SourceAccount:    "医学参考",
		AgentID:          "agent-a",
		Schedule:         "manual",
		Operation:        "sync_content",
		Options:          map[string]any{"limit": float64(20)},
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if subscription.ID == "" || !subscription.Enabled {
		t.Fatalf("unexpected subscription: %#v", subscription)
	}

	clock.Advance(time.Minute)
	subscription, err = store.UpdateSubscription(subscription.ID, SourceSubscriptionInput{
		SourceType:       subscription.SourceType,
		SourceAccountKey: subscription.SourceAccountKey,
		SourceAccount:    subscription.SourceAccount,
		AgentID:          "agent-a",
		Schedule:         "0 */6 * * *",
		Cursor:           "cursor-1",
		Operation:        "sync_content",
		Options:          map[string]any{"limit": float64(10)},
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("update subscription: %v", err)
	}
	if subscription.Schedule != "0 */6 * * *" || subscription.Cursor != "cursor-1" {
		t.Fatalf("subscription was not updated: %#v", subscription)
	}
	subscriptions, err := store.ListSubscriptions()
	if err != nil || len(subscriptions) != 1 {
		t.Fatalf("list subscriptions = %#v, err=%v", subscriptions, err)
	}

	run, err := store.CreateRun(subscription.ID, "")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.Status != SourceRunQueued || run.RequestedOperation != "sync_content" {
		t.Fatalf("unexpected queued run: %#v", run)
	}

	leased, err := store.LeaseNextRun("agent-a", []string{"reading_data"}, 5*time.Minute)
	if err != nil {
		t.Fatalf("lease with unsupported capability: %v", err)
	}
	if leased != nil {
		t.Fatalf("leased unsupported run: %#v", leased)
	}
	leased, err = store.LeaseNextRun("agent-a", []string{"sync_content"}, 5*time.Minute)
	if err != nil {
		t.Fatalf("lease run: %v", err)
	}
	if leased == nil || leased.ID != run.ID || leased.Status != SourceRunLeased {
		t.Fatalf("unexpected leased run: %#v", leased)
	}
	if _, err := store.StartRun(run.ID, "agent-b"); !errors.Is(err, ErrSourceRunLeaseOwner) {
		t.Fatalf("wrong-agent start error = %v", err)
	}
	running, err := store.StartRun(run.ID, "agent-a")
	if err != nil || running.Status != SourceRunRunning {
		t.Fatalf("start run = %#v, err=%v", running, err)
	}

	for index, outcome := range []string{SourceItemNew, SourceItemUpdated, SourceItemSkipped, SourceItemFailed} {
		_, err := store.RecordRunItem(run.ID, "agent-a", SourceSyncItemInput{
			SourceItemKey:  "article-" + string(rune('a'+index)),
			IdempotencyKey: "idem-" + string(rune('a'+index)),
			ContentHash:    "hash-" + string(rune('a'+index)),
			Outcome:        outcome,
			TargetBookID:   "book-" + string(rune('a'+index)),
			Error:          map[bool]string{true: "invalid article"}[outcome == SourceItemFailed],
		})
		if err != nil {
			t.Fatalf("record %s item: %v", outcome, err)
		}
	}
	if _, err := store.CompleteRun(run.ID, "agent-b"); !errors.Is(err, ErrSourceRunLeaseOwner) {
		t.Fatalf("wrong-agent completion error = %v", err)
	}
	completed, err := store.CompleteRun(run.ID, "agent-a")
	if err != nil {
		t.Fatalf("complete run: %v", err)
	}
	if completed.Status != SourceRunPartial || completed.NewCount != 1 || completed.UpdatedCount != 1 || completed.SkippedCount != 1 || completed.FailedCount != 1 {
		t.Fatalf("unexpected completed counters: %#v", completed)
	}
	if _, err := store.StartRun(run.ID, "agent-a"); !errors.Is(err, ErrSourceRunTerminal) {
		t.Fatalf("terminal start error = %v", err)
	}
	if canceled, err := store.CancelRun(run.ID); err != nil || canceled.Status != SourceRunPartial {
		t.Fatalf("cancel terminal run = %#v, err=%v", canceled, err)
	}

	reopened, err := newSourceSyncStore(root, clock.Now)
	if err != nil {
		t.Fatalf("reopen source sync store: %v", err)
	}
	persisted, err := reopened.GetRun(run.ID)
	if err != nil {
		t.Fatalf("get persisted run: %v", err)
	}
	if persisted.Status != SourceRunPartial || persisted.NewCount != 1 || persisted.FailedCount != 1 {
		t.Fatalf("persisted run lost state: %#v", persisted)
	}
}

func TestSourceSyncStoreRecoversExpiredLeaseAndRetries(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 9, 18, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-tech",
		SourceAccount:    "科技参考",
		Operation:        "existing_articles",
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	run, err := store.CreateRun(subscription.ID, "")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.LeaseNextRun("agent-a", []string{"existing_articles"}, time.Minute); err != nil {
		t.Fatalf("lease run: %v", err)
	}
	clock.Advance(2 * time.Minute)
	requeued, err := store.RequeueExpiredRuns()
	if err != nil || requeued != 1 {
		t.Fatalf("requeue expired = %d, err=%v", requeued, err)
	}
	current, err := store.GetRun(run.ID)
	if err != nil || current.Status != SourceRunQueued || current.LeaseOwner != "" {
		t.Fatalf("requeued run = %#v, err=%v", current, err)
	}
	if _, err := store.LeaseNextRun("agent-b", []string{"existing_articles"}, time.Minute); err != nil {
		t.Fatalf("re-lease run: %v", err)
	}
	if _, err := store.StartRun(run.ID, "agent-b"); err != nil {
		t.Fatalf("restart run: %v", err)
	}
	failed, err := store.FailRun(run.ID, "agent-b", "upstream unavailable")
	if err != nil || failed.Status != SourceRunFailed || failed.Error != "upstream unavailable" {
		t.Fatalf("fail run = %#v, err=%v", failed, err)
	}
	retry, err := store.RetryRun(run.ID)
	if err != nil {
		t.Fatalf("retry run: %v", err)
	}
	if retry.Status != SourceRunQueued || retry.RetryOf != run.ID || retry.Attempt != 2 {
		t.Fatalf("unexpected retry: %#v", retry)
	}
	canceled, err := store.CancelRun(retry.ID)
	if err != nil || canceled.Status != SourceRunCanceled {
		t.Fatalf("cancel retry = %#v, err=%v", canceled, err)
	}
	if _, err := store.RetryRun(retry.ID); !errors.Is(err, ErrSourceRunNotRetryable) {
		t.Fatalf("canceled retry error = %v", err)
	}
}

func TestSourceSyncStoreLeasesRunOnce(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-concurrent",
		SourceAccount:    "并发测试",
		Operation:        "sync_content",
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if _, err := store.CreateRun(subscription.ID, ""); err != nil {
		t.Fatalf("create run: %v", err)
	}

	type leaseResult struct {
		run *SourceSyncRun
		err error
	}
	results := make(chan leaseResult, 2)
	var wg sync.WaitGroup
	for _, agentID := range []string{"agent-a", "agent-b"} {
		wg.Add(1)
		go func(agentID string) {
			defer wg.Done()
			run, err := store.LeaseNextRun(agentID, []string{"sync_content"}, time.Minute)
			results <- leaseResult{run: run, err: err}
		}(agentID)
	}
	wg.Wait()
	close(results)

	leased := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("lease error: %v", result.err)
		}
		if result.run != nil {
			leased++
		}
	}
	if leased != 1 {
		t.Fatalf("leased runs = %d", leased)
	}
}

type sourceSyncTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newSourceSyncTestClock(now time.Time) *sourceSyncTestClock {
	return &sourceSyncTestClock{now: now}
}

func (c *sourceSyncTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *sourceSyncTestClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}
