package app

import (
	"testing"
	"time"
)

func TestSourceSchedulerTickQueuesOnlyDueSubscriptions(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	defer store.Close()

	due := createSchedulerTestSubscription(t, store, "due", "interval:60", true)
	disabled := createSchedulerTestSubscription(t, store, "disabled", "60", false)
	future := createSchedulerTestSubscription(t, store, "future", "interval:300", true)
	active := createSchedulerTestSubscription(t, store, "active", "interval:60", true)
	activeRun, err := store.CreateRun(active.ID, "")
	if err != nil {
		t.Fatalf("create active run: %v", err)
	}
	manual := createSchedulerTestSubscription(t, store, "manual", "manual", true)

	clock.Advance(2 * time.Minute)
	scheduler, err := NewSourceScheduler(store, clock.Now)
	if err != nil {
		t.Fatalf("new source scheduler: %v", err)
	}
	result, err := scheduler.Tick()
	if err != nil {
		t.Fatalf("Tick returned error: %v", err)
	}
	if result.Evaluated != 5 || result.Queued != 1 || result.Retried != 0 ||
		result.SkippedDisabled != 1 || result.SkippedFuture != 1 ||
		result.SkippedActive != 1 || result.SkippedManual != 1 {
		t.Fatalf("unexpected tick result: %#v", result)
	}
	runs, err := store.ListRuns(20)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if schedulerRunCount(runs, due.ID) != 1 || schedulerRunCount(runs, active.ID) != 1 {
		t.Fatalf("runs = %#v", runs)
	}
	if schedulerRunCount(runs, disabled.ID) != 0 || schedulerRunCount(runs, future.ID) != 0 || schedulerRunCount(runs, manual.ID) != 0 {
		t.Fatalf("non-due subscriptions received runs: %#v", runs)
	}
	if got, err := store.GetRun(activeRun.ID); err != nil || got.Status != SourceRunQueued {
		t.Fatalf("active run changed: %#v, err=%v", got, err)
	}
}

func TestSourceSchedulerRetriesFailedRunAfterInterval(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	defer store.Close()
	subscription := createSchedulerTestSubscription(t, store, "retry", "interval:60", true)
	failedRun := createSchedulerFailedRun(t, store, "agent-scheduler", "temporary local WC Plus outage")
	if failedRun.SubscriptionID != subscription.ID {
		t.Fatalf("failed run belongs to %q, want %q", failedRun.SubscriptionID, subscription.ID)
	}

	clock.Advance(2 * time.Minute)
	scheduler, err := NewSourceScheduler(store, clock.Now)
	if err != nil {
		t.Fatalf("new source scheduler: %v", err)
	}
	result, err := scheduler.Tick()
	if err != nil {
		t.Fatalf("Tick returned error: %v", err)
	}
	if result.Queued != 1 || result.Retried != 1 || result.SkippedBlocked != 0 {
		t.Fatalf("unexpected retry result: %#v", result)
	}
	runs, err := store.ListRuns(10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	latest := latestSchedulerRun(t, runs, subscription.ID)
	if latest.Status != SourceRunQueued || latest.Attempt != 2 || latest.RetryOf != failedRun.ID {
		t.Fatalf("retry run = %#v", latest)
	}
}

func TestSourceSchedulerDoesNotRetryBlockedFailures(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	defer store.Close()
	reasons := []string{
		"wcplus operation blocked: unactivated",
		"wcplus operation blocked: not_max_version",
		"source agent request failed with HTTP 401 unauthorized",
		"wcplus license expired",
		"request throttled",
		"request parameter expired",
	}
	for index, reason := range reasons {
		createSchedulerTestSubscription(t, store, "blocked-"+time.Duration(index).String(), "interval:60", true)
		createSchedulerFailedRun(t, store, "agent-scheduler", reason)
	}

	clock.Advance(2 * time.Minute)
	scheduler, err := NewSourceScheduler(store, clock.Now)
	if err != nil {
		t.Fatalf("new source scheduler: %v", err)
	}
	result, err := scheduler.Tick()
	if err != nil {
		t.Fatalf("Tick returned error: %v", err)
	}
	if result.Queued != 0 || result.Retried != 0 || result.SkippedBlocked != len(reasons) {
		t.Fatalf("unexpected blocked result: %#v", result)
	}
	runs, err := store.ListRuns(50)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != len(reasons) {
		t.Fatalf("blocked failures were retried: %#v", runs)
	}
}

func TestSourceSchedulerRejectsInvalidIntervalWithoutBlockingOtherSubscriptions(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 10, 11, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	defer store.Close()
	createSchedulerTestSubscription(t, store, "invalid", "0 */6 * * *", true)
	createSchedulerTestSubscription(t, store, "valid", "30", true)
	clock.Advance(time.Minute)

	scheduler, err := NewSourceScheduler(store, clock.Now)
	if err != nil {
		t.Fatalf("new source scheduler: %v", err)
	}
	result, err := scheduler.Tick()
	if err != nil {
		t.Fatalf("Tick returned error: %v", err)
	}
	if result.InvalidSchedule != 1 || result.Queued != 1 {
		t.Fatalf("unexpected invalid-schedule result: %#v", result)
	}
}

func createSchedulerTestSubscription(t *testing.T, store *SourceSyncStore, key, schedule string, enabled bool) SourceSubscription {
	t.Helper()
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: key,
		SourceAccount:    key,
		AgentID:          "agent-scheduler",
		Schedule:         schedule,
		Operation:        "existing_articles",
		Enabled:          enabled,
	})
	if err != nil {
		t.Fatalf("create subscription %s: %v", key, err)
	}
	return subscription
}

func createSchedulerFailedRun(t *testing.T, store *SourceSyncStore, agentID, reason string) SourceSyncRun {
	t.Helper()
	runs, err := store.ListRuns(100)
	if err != nil {
		t.Fatalf("list runs before failure: %v", err)
	}
	subscriptions, err := store.ListSubscriptions()
	if err != nil {
		t.Fatalf("list subscriptions before failure: %v", err)
	}
	var subscription SourceSubscription
	for _, candidate := range subscriptions {
		if schedulerRunCount(runs, candidate.ID) == 0 {
			subscription = candidate
			break
		}
	}
	if subscription.ID == "" {
		t.Fatal("no subscription without a run")
	}
	if _, err := store.CreateRun(subscription.ID, ""); err != nil {
		t.Fatalf("create failed run: %v", err)
	}
	leased, err := store.LeaseNextRun(agentID, []string{"existing_articles"}, time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("lease failed run: %#v, err=%v", leased, err)
	}
	if _, err := store.StartRun(leased.ID, agentID); err != nil {
		t.Fatalf("start failed run: %v", err)
	}
	failed, err := store.FailRun(leased.ID, agentID, reason)
	if err != nil {
		t.Fatalf("fail run: %v", err)
	}
	return failed
}

func schedulerRunCount(runs []SourceSyncRun, subscriptionID string) int {
	count := 0
	for _, run := range runs {
		if run.SubscriptionID == subscriptionID {
			count++
		}
	}
	return count
}

func latestSchedulerRun(t *testing.T, runs []SourceSyncRun, subscriptionID string) SourceSyncRun {
	t.Helper()
	for _, run := range runs {
		if run.SubscriptionID == subscriptionID {
			return run
		}
	}
	t.Fatalf("no run for subscription %s", subscriptionID)
	return SourceSyncRun{}
}
