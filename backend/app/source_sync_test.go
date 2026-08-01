package app

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
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

func TestSourceAgentCapabilityHealthMigratesLegacyDatabase(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(root, sourceSyncDBName))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE source_agents (
		agent_id TEXT PRIMARY KEY, version TEXT NOT NULL DEFAULT '', capabilities_json TEXT NOT NULL DEFAULT '[]',
		wcplus_healthy INTEGER NOT NULL DEFAULT 0, wcplus_version TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '',
		last_heartbeat_at TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	store, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("migrate legacy store: %v", err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('source_agents') WHERE name = 'capability_health_json'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("capability_health_json count=%d err=%v", count, err)
	}
}

func TestSourceAgentRegistryMigration(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(root, sourceSyncDBName))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE source_agents (
		agent_id TEXT PRIMARY KEY, version TEXT NOT NULL DEFAULT '', capabilities_json TEXT NOT NULL DEFAULT '[]',
		wcplus_healthy INTEGER NOT NULL DEFAULT 0, wcplus_version TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '',
		last_heartbeat_at TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		capability_health_json TEXT NOT NULL DEFAULT '{}')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO source_agents (
		agent_id, version, capabilities_json, wcplus_healthy, wcplus_version, last_error,
		last_heartbeat_at, created_at, updated_at, capability_health_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-agent", "1.2.3", `["sync_content"]`, 1, "4.2.0", "",
		"2026-07-31T12:00:00Z", "2026-07-31T11:00:00Z", "2026-07-31T12:00:00Z",
		`{"wcplus":{"healthy":true,"version":"4.2.0"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("migrate legacy store: %v", err)
	}
	defer store.Close()
	agents, err := store.ListAgents()
	if err != nil {
		t.Fatalf("list migrated agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("migrated agents=%#v", agents)
	}
	agent := agents[0]
	if agent.AgentID != "legacy-agent" || agent.Version != "1.2.3" || !agent.WCPlusHealthy {
		t.Fatalf("legacy fields changed: %#v", agent)
	}
	if agent.WorkerType != "legacy" || agent.Platform != "" || agent.Architecture != "" || agent.ProtocolVersion != "" {
		t.Fatalf("unsafe runtime defaults: %#v", agent)
	}
	if agent.DesiredState != SourceAgentDesiredActive || agent.CurrentRunID != "" || agent.CurrentCommandID != "" {
		t.Fatalf("unsafe control defaults: %#v", agent)
	}
	if agent.OutboxPending != 0 || agent.DeadLetterCount != 0 || agent.LastSuccessAt != "" {
		t.Fatalf("unsafe delivery defaults: %#v", agent)
	}
}

func TestSourceAgentCapabilityHealthRoundTripAndBoundsDiagnostics(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	agent, err := store.HeartbeatAgent(SourceAgentHeartbeat{
		AgentID: "agent-capability",
		CapabilityHealth: map[string]SourceCapabilityHealth{
			" wechat_mp ": {Healthy: false, RequiresAction: "login", LastError: strings.Repeat("故", sourceDiagnosticMaxRunes+20)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	health, ok := agent.CapabilityHealth["wechat_mp"]
	if !ok || health.RequiresAction != "login" || len([]rune(health.LastError)) != sourceDiagnosticMaxRunes {
		t.Fatalf("unexpected capability health: %#v", agent.CapabilityHealth)
	}
}

func TestSourceAgentCapabilityHealthMapsLegacyWCPlusFields(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	agent, err := store.HeartbeatAgent(SourceAgentHeartbeat{AgentID: "legacy", WCPlusHealthy: true, WCPlusVersion: "4.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if health := agent.CapabilityHealth["wcplus"]; !health.Healthy || health.Version != "4.2.0" {
		t.Fatalf("legacy fields not mapped: %#v", agent)
	}
}

func TestSourceSyncNormalizesWeChatSubscriptionOptions(t *testing.T) {
	input, _, err := normalizeSourceSubscriptionInput(SourceSubscriptionInput{
		SourceType:       "wechat_mp_article",
		SourceAccountKey: "account-key",
		Operation:        "sync_articles",
		Schedule:         "interval:60",
		Options: map[string]any{
			"page_size":     float64(999),
			"max_items":     float64(999),
			"include_media": true,
			"title_query":   strings.Repeat("筛", 150),
			"ignored":       "value",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sourceAgentOptionInt(input.Options, "page_size", 0, 1000) != 20 || sourceAgentOptionInt(input.Options, "max_items", 0, 1000) != 500 {
		t.Fatalf("options=%#v", input.Options)
	}
	if !sourceAgentOptionBool(input.Options, "include_media", false) || len([]rune(sourceAgentOptionString(input.Options, "title_query", ""))) != 100 {
		t.Fatalf("options=%#v", input.Options)
	}
	if _, ok := input.Options["ignored"]; ok {
		t.Fatalf("unexpected option survived: %#v", input.Options)
	}
}

func TestSourceSyncClampsWeChatSubscriptionBatchSizesToOne(t *testing.T) {
	input, _, err := normalizeSourceSubscriptionInput(SourceSubscriptionInput{
		SourceType:       "wechat_mp_article",
		SourceAccountKey: "account-key",
		Operation:        "sync_articles",
		Schedule:         "manual",
		Options: map[string]any{
			"page_size": float64(0),
			"max_items": float64(0),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sourceAgentOptionInt(input.Options, "page_size", 0, 1000) != 1 || sourceAgentOptionInt(input.Options, "max_items", 0, 1000) != 1 {
		t.Fatalf("options=%#v", input.Options)
	}
}

func TestSourceSyncRejectsUnsafeWeChatSubscriptionContract(t *testing.T) {
	for _, input := range []SourceSubscriptionInput{
		{SourceType: "wechat_mp_article", SourceAccountKey: "account", Operation: "unknown", Schedule: "manual"},
		{SourceType: "wechat_mp_article", SourceAccountKey: "account", Operation: "sync_articles", Schedule: "interval:1"},
	} {
		if _, _, err := normalizeSourceSubscriptionInput(input); err == nil {
			t.Fatalf("accepted input=%#v", input)
		}
	}
}

func TestSourceSyncStoreSetSubscriptionEnabledPreservesAgentCursor(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-med",
		SourceAccount:    "医学参考",
		AgentID:          "agent-a",
		Schedule:         "interval:3600",
		Cursor:           "2026-07-10T11:55:00Z|article-42",
		Operation:        "sync_content",
		Options:          map[string]any{"limit": float64(50), "include_html": true},
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	clock.Advance(time.Minute)
	updated, err := store.SetSubscriptionEnabled(subscription.ID, false)
	if err != nil {
		t.Fatalf("disable subscription: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("subscription remained enabled: %#v", updated)
	}
	if updated.Cursor != subscription.Cursor || updated.Schedule != subscription.Schedule || updated.Operation != subscription.Operation || updated.AgentID != subscription.AgentID {
		t.Fatalf("enabled update overwrote agent-owned fields: before=%#v after=%#v", subscription, updated)
	}
	if !reflect.DeepEqual(updated.Options, subscription.Options) {
		t.Fatalf("enabled update overwrote options: before=%#v after=%#v", subscription.Options, updated.Options)
	}
	if updated.CreatedAt != subscription.CreatedAt || updated.UpdatedAt != clock.Now().Format(time.RFC3339Nano) {
		t.Fatalf("unexpected timestamps: before=%#v after=%#v", subscription, updated)
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

func TestSourceLeaseRejectsPausedAgent(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.HeartbeatAgent(SourceAgentHeartbeat{AgentID: "agent-pause"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "paused-agent",
		SourceAccount:    "Paused Agent",
		AgentID:          "agent-pause",
		Operation:        "sync_content",
		Enabled:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(subscription.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	paused, err := store.SetAgentDesiredState(" agent-pause ", " paused ")
	if err != nil {
		t.Fatalf("pause agent: %v", err)
	}
	if paused.DesiredState != SourceAgentDesiredPaused {
		t.Fatalf("desired state=%q", paused.DesiredState)
	}
	leased, err := store.LeaseNextRun("agent-pause", []string{"sync_content"}, time.Minute)
	if err != nil {
		t.Fatalf("lease paused agent: %v", err)
	}
	if leased != nil {
		t.Fatalf("paused agent leased run: %#v", leased)
	}
	queued, err := store.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != SourceRunQueued || queued.LeaseOwner != "" {
		t.Fatalf("paused lease changed queued run: %#v", queued)
	}

	active, err := store.SetAgentDesiredState("agent-pause", "active")
	if err != nil {
		t.Fatalf("resume agent: %v", err)
	}
	if active.DesiredState != SourceAgentDesiredActive {
		t.Fatalf("desired state=%q", active.DesiredState)
	}
	leased, err = store.LeaseNextRun("agent-pause", []string{"sync_content"}, time.Minute)
	if err != nil || leased == nil || leased.ID != run.ID {
		t.Fatalf("resumed lease=%#v, err=%v", leased, err)
	}
	running, err := store.StartRun(run.ID, "agent-pause")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	if _, err := store.SetAgentDesiredState("agent-pause", SourceAgentDesiredPaused); err != nil {
		t.Fatalf("pause running agent: %v", err)
	}
	current, err := store.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != SourceRunRunning || current.LeaseOwner != running.LeaseOwner {
		t.Fatalf("pause changed running run: before=%#v after=%#v", running, current)
	}

	if _, err := store.SetAgentDesiredState("agent-pause", "offline"); !errors.Is(err, ErrSourceAgentDesiredState) {
		t.Fatalf("invalid desired state error=%v", err)
	}
	if _, err := store.SetAgentDesiredState("", SourceAgentDesiredActive); err == nil || !strings.Contains(err.Error(), "agent_id is required") {
		t.Fatalf("empty agent error=%v", err)
	}
	if _, err := store.SetAgentDesiredState("missing-agent", SourceAgentDesiredActive); !errors.Is(err, ErrSourceAgentNotFound) {
		t.Fatalf("missing agent error=%v", err)
	}

	unboundSubscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "unregistered-agent",
		SourceAccount:    "Unregistered Agent",
		Operation:        "sync_content",
		Enabled:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	unboundRun, err := store.CreateRun(unboundSubscription.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	leased, err = store.LeaseNextRun("missing-agent", []string{"sync_content"}, time.Minute)
	if err != nil || leased != nil {
		t.Fatalf("unregistered lease=%#v, err=%v", leased, err)
	}
	current, err = store.GetRun(unboundRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != SourceRunQueued || current.LeaseOwner != "" {
		t.Fatalf("unregistered lease changed queued run: %#v", current)
	}
}

func TestSourceLeaseClaimRejectsPauseCommittedAfterPrecheck(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	registerSourceLeaseAgent(t, store, "agent-linearized-pause")
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "linearized-pause",
		SourceAccount:    "Linearized Pause",
		AgentID:          "agent-linearized-pause",
		Operation:        "sync_content",
		Enabled:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(subscription.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	prechecked, err := store.getAgent("agent-linearized-pause")
	if err != nil || prechecked.DesiredState != SourceAgentDesiredActive {
		t.Fatalf("active precheck=%#v, err=%v", prechecked, err)
	}
	if _, err := store.SetAgentDesiredState("agent-linearized-pause", SourceAgentDesiredPaused); err != nil {
		t.Fatalf("pause after precheck: %v", err)
	}

	leased, err := store.claimNextRun("agent-linearized-pause", []string{"sync_content"}, time.Minute)
	if err != nil {
		t.Fatalf("claim after committed pause: %v", err)
	}
	if leased != nil {
		t.Fatalf("claim used stale active precheck: %#v", leased)
	}
	current, err := store.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != SourceRunQueued || current.LeaseOwner != "" {
		t.Fatalf("claim after pause changed queued run: %#v", current)
	}
}

func TestSourceSyncStoreRecoversExpiredLeaseAndRetries(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 9, 18, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	registerSourceLeaseAgent(t, store, "agent-a")
	registerSourceLeaseAgent(t, store, "agent-b")
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

func TestSourceSyncStoreFailRunPersistsCheckpointCursor(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registerSourceLeaseAgent(t, store, "agent-a")
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wechat_mp_article",
		SourceAccountKey: "account-key",
		SourceAccount:    "Account",
		Operation:        "sync_articles",
		Cursor:           "old-cursor",
		Enabled:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(subscription.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LeaseNextRun("agent-a", []string{"sync_articles"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartRun(run.ID, "agent-a"); err != nil {
		t.Fatal(err)
	}
	failed, err := store.FailRun(run.ID, "agent-a", "download failed", "safe-cursor")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != SourceRunFailed {
		t.Fatalf("failed run=%#v", failed)
	}
	updated, err := store.GetSubscription(subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Cursor != "safe-cursor" || updated.LastSuccessAt != "" {
		t.Fatalf("updated subscription=%#v", updated)
	}
}

func TestSourceSyncStoreBoundsRunFailureText(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	agent, err := store.HeartbeatAgent(SourceAgentHeartbeat{
		AgentID:       "agent-a",
		WCPlusHealthy: false,
		LastError:     strings.Repeat("错", 10000),
	})
	if err != nil {
		t.Fatalf("heartbeat agent: %v", err)
	}
	if got := len([]rune(agent.LastError)); got != 4000 {
		t.Fatalf("stored heartbeat error length = %d, want 4000", got)
	}
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-errors",
		SourceAccount:    "错误边界",
		Operation:        "sync_content",
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	run, err := store.CreateRun(subscription.ID, "")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	leased, err := store.LeaseNextRun("agent-a", []string{"sync_content"}, time.Minute)
	if err != nil || leased == nil || leased.ID != run.ID {
		t.Fatalf("lease run = %#v, err=%v", leased, err)
	}
	if _, err := store.StartRun(run.ID, "agent-a"); err != nil {
		t.Fatalf("start run: %v", err)
	}
	failed, err := store.FailRun(run.ID, "agent-a", strings.Repeat("错", 10000))
	if err != nil {
		t.Fatalf("fail run: %v", err)
	}
	if got := len([]rune(failed.Error)); got != 4000 {
		t.Fatalf("stored failure length = %d, want 4000", got)
	}
}

func TestSourceSyncStoreRequeuesLeaseExpiredDuringFractionalSecond(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 9, 18, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	defer store.Close()
	registerSourceLeaseAgent(t, store, "agent-a")
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-fractional",
		SourceAccount:    "Fractional Clock",
		Operation:        "existing_articles",
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if _, err := store.CreateRun(subscription.ID, ""); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.LeaseNextRun("agent-a", []string{"existing_articles"}, time.Minute); err != nil {
		t.Fatalf("lease run: %v", err)
	}

	clock.Advance(time.Minute + 500*time.Millisecond)
	requeued, err := store.RequeueExpiredRuns()
	if err != nil || requeued != 1 {
		t.Fatalf("requeue fractional-second expiry = %d, err=%v", requeued, err)
	}
}

func TestSourceSyncStoreLeasesRunOnce(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	registerSourceLeaseAgent(t, store, "agent-a")
	registerSourceLeaseAgent(t, store, "agent-b")
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

func registerSourceLeaseAgent(t testing.TB, store *SourceSyncStore, agentID string) {
	t.Helper()
	if _, err := store.HeartbeatAgent(SourceAgentHeartbeat{AgentID: agentID}); err != nil {
		t.Fatalf("register lease agent %q: %v", agentID, err)
	}
}
