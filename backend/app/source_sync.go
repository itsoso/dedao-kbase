package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const sourceSyncDBName = "source_sync.sqlite3"

const (
	SourceRunQueued    = "queued"
	SourceRunLeased    = "leased"
	SourceRunRunning   = "running"
	SourceRunSucceeded = "succeeded"
	SourceRunPartial   = "partial"
	SourceRunFailed    = "failed"
	SourceRunCanceled  = "canceled"
)

const (
	SourceItemNew     = "new"
	SourceItemUpdated = "updated"
	SourceItemSkipped = "skipped"
	SourceItemFailed  = "failed"
)

var (
	ErrSourceRunNotFound        = errors.New("source sync run not found")
	ErrSourceRunLeaseOwner      = errors.New("source sync run belongs to another agent")
	ErrSourceRunLeaseExpired    = errors.New("source sync run lease expired")
	ErrSourceRunTerminal        = errors.New("source sync run is terminal")
	ErrSourceRunInvalidState    = errors.New("source sync run state transition is invalid")
	ErrSourceRunNotRetryable    = errors.New("source sync run is not retryable")
	ErrSourceRunActive          = errors.New("source subscription already has an active run")
	ErrSourceSubscriptionAbsent = errors.New("source subscription not found")
)

type SourceAgentHeartbeat struct {
	AgentID       string   `json:"agent_id"`
	Version       string   `json:"version,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
	WCPlusHealthy bool     `json:"wcplus_healthy"`
	WCPlusVersion string   `json:"wcplus_version,omitempty"`
	LastError     string   `json:"last_error,omitempty"`
}

type SourceAgent struct {
	AgentID         string   `json:"agent_id"`
	Version         string   `json:"version,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	WCPlusHealthy   bool     `json:"wcplus_healthy"`
	WCPlusVersion   string   `json:"wcplus_version,omitempty"`
	LastError       string   `json:"last_error,omitempty"`
	LastHeartbeatAt string   `json:"last_heartbeat_at"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

type SourceSubscriptionInput struct {
	SourceType       string         `json:"source_type"`
	SourceAccountKey string         `json:"source_account_key"`
	SourceAccount    string         `json:"source_account"`
	AgentID          string         `json:"agent_id,omitempty"`
	Schedule         string         `json:"schedule,omitempty"`
	Cursor           string         `json:"cursor,omitempty"`
	Operation        string         `json:"operation,omitempty"`
	Options          map[string]any `json:"options,omitempty"`
	Enabled          bool           `json:"enabled"`
}

type SourceSubscription struct {
	ID               string         `json:"id"`
	SourceType       string         `json:"source_type"`
	SourceAccountKey string         `json:"source_account_key"`
	SourceAccount    string         `json:"source_account"`
	AgentID          string         `json:"agent_id,omitempty"`
	Schedule         string         `json:"schedule"`
	Cursor           string         `json:"cursor,omitempty"`
	Operation        string         `json:"operation"`
	Options          map[string]any `json:"options,omitempty"`
	Enabled          bool           `json:"enabled"`
	LastSuccessAt    string         `json:"last_success_at,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

type SourceSyncRun struct {
	ID                 string `json:"id"`
	SubscriptionID     string `json:"subscription_id"`
	AgentID            string `json:"agent_id,omitempty"`
	RequestedOperation string `json:"requested_operation"`
	Status             string `json:"status"`
	Attempt            int    `json:"attempt"`
	RetryOf            string `json:"retry_of,omitempty"`
	LeaseOwner         string `json:"lease_owner,omitempty"`
	LeaseExpiresAt     string `json:"lease_expires_at,omitempty"`
	NewCount           int    `json:"new_count"`
	UpdatedCount       int    `json:"updated_count"`
	SkippedCount       int    `json:"skipped_count"`
	FailedCount        int    `json:"failed_count"`
	Error              string `json:"error,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	StartedAt          string `json:"started_at,omitempty"`
	FinishedAt         string `json:"finished_at,omitempty"`
}

type SourceSyncItemInput struct {
	SourceItemKey  string `json:"source_item_key"`
	IdempotencyKey string `json:"idempotency_key"`
	ContentHash    string `json:"content_hash,omitempty"`
	Outcome        string `json:"outcome"`
	TargetBookID   string `json:"target_book_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

type SourceSyncItem struct {
	ID             string `json:"id"`
	RunID          string `json:"run_id"`
	SourceItemKey  string `json:"source_item_key"`
	IdempotencyKey string `json:"idempotency_key"`
	ContentHash    string `json:"content_hash,omitempty"`
	Outcome        string `json:"outcome"`
	TargetBookID   string `json:"target_book_id,omitempty"`
	Error          string `json:"error,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type SourceSyncStore struct {
	dbPath string
	now    func() time.Time
	db     *sql.DB
}

func NewSourceSyncStore(root string) (*SourceSyncStore, error) {
	return newSourceSyncStore(root, time.Now)
}

func newSourceSyncStore(root string, now func() time.Time) (*SourceSyncStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("source sync root is required")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(root, sourceSyncDBName)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateSourceSyncDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return &SourceSyncStore{dbPath: dbPath, now: now, db: db}, nil
}

func (s *SourceSyncStore) DBPath() string {
	if s == nil {
		return ""
	}
	return s.dbPath
}

func (s *SourceSyncStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func migrateSourceSyncDB(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS source_agents (
			agent_id TEXT PRIMARY KEY,
			version TEXT NOT NULL DEFAULT '',
			capabilities_json TEXT NOT NULL DEFAULT '[]',
			wcplus_healthy INTEGER NOT NULL DEFAULT 0,
			wcplus_version TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			last_heartbeat_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS source_subscriptions (
			id TEXT PRIMARY KEY,
			source_type TEXT NOT NULL,
			source_account_key TEXT NOT NULL,
			source_account TEXT NOT NULL,
			agent_id TEXT NOT NULL DEFAULT '',
			schedule TEXT NOT NULL DEFAULT 'manual',
			cursor_value TEXT NOT NULL DEFAULT '',
			operation TEXT NOT NULL,
			options_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			last_success_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(source_type, source_account_key)
		);

		CREATE TABLE IF NOT EXISTS source_sync_runs (
			id TEXT PRIMARY KEY,
			subscription_id TEXT NOT NULL,
			agent_id TEXT NOT NULL DEFAULT '',
			requested_operation TEXT NOT NULL,
			status TEXT NOT NULL,
			attempt INTEGER NOT NULL DEFAULT 1,
			retry_of TEXT NOT NULL DEFAULT '',
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_expires_at TEXT NOT NULL DEFAULT '',
			new_count INTEGER NOT NULL DEFAULT 0,
			updated_count INTEGER NOT NULL DEFAULT 0,
			skipped_count INTEGER NOT NULL DEFAULT 0,
			failed_count INTEGER NOT NULL DEFAULT 0,
			error_text TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(subscription_id) REFERENCES source_subscriptions(id)
		);
		CREATE INDEX IF NOT EXISTS idx_source_sync_runs_status_created
			ON source_sync_runs(status, created_at);
		CREATE INDEX IF NOT EXISTS idx_source_sync_runs_subscription_created
			ON source_sync_runs(subscription_id, created_at DESC);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_source_sync_runs_one_active
			ON source_sync_runs(subscription_id)
			WHERE status IN ('queued', 'leased', 'running');

		CREATE TABLE IF NOT EXISTS source_sync_items (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			source_item_key TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			content_hash TEXT NOT NULL DEFAULT '',
			outcome TEXT NOT NULL,
			target_book_id TEXT NOT NULL DEFAULT '',
			error_text TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(run_id, source_item_key),
			FOREIGN KEY(run_id) REFERENCES source_sync_runs(id)
		);
		CREATE INDEX IF NOT EXISTS idx_source_sync_items_run_outcome
			ON source_sync_items(run_id, outcome);

		CREATE TABLE IF NOT EXISTS source_documents (
			source_type TEXT NOT NULL,
			source_item_key TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			target_book_id TEXT NOT NULL,
			source_timestamp TEXT NOT NULL DEFAULT '',
			last_seen_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(source_type, source_item_key)
		);

		CREATE TABLE IF NOT EXISTS source_outbox_receipts (
			idempotency_key TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			outcome TEXT NOT NULL,
			target_book_id TEXT NOT NULL DEFAULT '',
			accepted_at TEXT NOT NULL
		);
	`)
	return err
}

func (s *SourceSyncStore) HeartbeatAgent(heartbeat SourceAgentHeartbeat) (SourceAgent, error) {
	heartbeat.AgentID = strings.TrimSpace(heartbeat.AgentID)
	if heartbeat.AgentID == "" {
		return SourceAgent{}, fmt.Errorf("agent_id is required")
	}
	heartbeat.Capabilities = normalizeSourceCapabilities(heartbeat.Capabilities)
	capabilitiesJSON, err := json.Marshal(heartbeat.Capabilities)
	if err != nil {
		return SourceAgent{}, err
	}
	now := s.timestamp()
	healthy := 0
	if heartbeat.WCPlusHealthy {
		healthy = 1
	}
	_, err = s.db.Exec(`
		INSERT INTO source_agents (
			agent_id, version, capabilities_json, wcplus_healthy, wcplus_version,
			last_error, last_heartbeat_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			version = excluded.version,
			capabilities_json = excluded.capabilities_json,
			wcplus_healthy = excluded.wcplus_healthy,
			wcplus_version = excluded.wcplus_version,
			last_error = excluded.last_error,
			last_heartbeat_at = excluded.last_heartbeat_at,
			updated_at = excluded.updated_at
	`, heartbeat.AgentID, strings.TrimSpace(heartbeat.Version), string(capabilitiesJSON), healthy,
		strings.TrimSpace(heartbeat.WCPlusVersion), strings.TrimSpace(heartbeat.LastError), now, now, now)
	if err != nil {
		return SourceAgent{}, err
	}
	return s.getAgent(heartbeat.AgentID)
}

func (s *SourceSyncStore) ListAgents() ([]SourceAgent, error) {
	rows, err := s.db.Query(`
		SELECT agent_id, version, capabilities_json, wcplus_healthy, wcplus_version,
			last_error, last_heartbeat_at, created_at, updated_at
		FROM source_agents
		ORDER BY last_heartbeat_at DESC, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	agents := make([]SourceAgent, 0)
	for rows.Next() {
		agent, err := scanSourceAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func (s *SourceSyncStore) getAgent(agentID string) (SourceAgent, error) {
	return scanSourceAgent(s.db.QueryRow(`
		SELECT agent_id, version, capabilities_json, wcplus_healthy, wcplus_version,
			last_error, last_heartbeat_at, created_at, updated_at
		FROM source_agents WHERE agent_id = ?
	`, agentID))
}

type sourceSyncScanner interface {
	Scan(dest ...any) error
}

func scanSourceAgent(row sourceSyncScanner) (SourceAgent, error) {
	var agent SourceAgent
	var capabilitiesJSON string
	var healthy int
	err := row.Scan(&agent.AgentID, &agent.Version, &capabilitiesJSON, &healthy, &agent.WCPlusVersion,
		&agent.LastError, &agent.LastHeartbeatAt, &agent.CreatedAt, &agent.UpdatedAt)
	if err != nil {
		return SourceAgent{}, err
	}
	agent.WCPlusHealthy = healthy != 0
	if err := json.Unmarshal([]byte(capabilitiesJSON), &agent.Capabilities); err != nil {
		return SourceAgent{}, err
	}
	return agent, nil
}

func (s *SourceSyncStore) CreateSubscription(input SourceSubscriptionInput) (SourceSubscription, error) {
	input, optionsJSON, err := normalizeSourceSubscriptionInput(input)
	if err != nil {
		return SourceSubscription{}, err
	}
	now := s.timestamp()
	id := newSourceSyncID("sub", s.now())
	enabled := 0
	if input.Enabled {
		enabled = 1
	}
	_, err = s.db.Exec(`
		INSERT INTO source_subscriptions (
			id, source_type, source_account_key, source_account, agent_id, schedule,
			cursor_value, operation, options_json, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, input.SourceType, input.SourceAccountKey, input.SourceAccount, input.AgentID,
		input.Schedule, input.Cursor, input.Operation, optionsJSON, enabled, now, now)
	if err != nil {
		return SourceSubscription{}, err
	}
	return s.GetSubscription(id)
}

func (s *SourceSyncStore) UpdateSubscription(id string, input SourceSubscriptionInput) (SourceSubscription, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SourceSubscription{}, fmt.Errorf("subscription id is required")
	}
	input, optionsJSON, err := normalizeSourceSubscriptionInput(input)
	if err != nil {
		return SourceSubscription{}, err
	}
	enabled := 0
	if input.Enabled {
		enabled = 1
	}
	result, err := s.db.Exec(`
		UPDATE source_subscriptions SET
			source_type = ?, source_account_key = ?, source_account = ?, agent_id = ?,
			schedule = ?, cursor_value = ?, operation = ?, options_json = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, input.SourceType, input.SourceAccountKey, input.SourceAccount, input.AgentID,
		input.Schedule, input.Cursor, input.Operation, optionsJSON, enabled, s.timestamp(), id)
	if err != nil {
		return SourceSubscription{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return SourceSubscription{}, ErrSourceSubscriptionAbsent
	}
	return s.GetSubscription(id)
}

func (s *SourceSyncStore) GetSubscription(id string) (SourceSubscription, error) {
	subscription, err := scanSourceSubscription(s.db.QueryRow(`
		SELECT id, source_type, source_account_key, source_account, agent_id, schedule,
			cursor_value, operation, options_json, enabled, last_success_at, created_at, updated_at
		FROM source_subscriptions WHERE id = ?
	`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceSubscription{}, ErrSourceSubscriptionAbsent
	}
	return subscription, err
}

func (s *SourceSyncStore) ListSubscriptions() ([]SourceSubscription, error) {
	rows, err := s.db.Query(`
		SELECT id, source_type, source_account_key, source_account, agent_id, schedule,
			cursor_value, operation, options_json, enabled, last_success_at, created_at, updated_at
		FROM source_subscriptions
		ORDER BY updated_at DESC, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	subscriptions := make([]SourceSubscription, 0)
	for rows.Next() {
		subscription, err := scanSourceSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, rows.Err()
}

func normalizeSourceSubscriptionInput(input SourceSubscriptionInput) (SourceSubscriptionInput, string, error) {
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.SourceAccountKey = strings.TrimSpace(input.SourceAccountKey)
	input.SourceAccount = strings.TrimSpace(input.SourceAccount)
	input.AgentID = strings.TrimSpace(input.AgentID)
	input.Schedule = strings.TrimSpace(input.Schedule)
	input.Cursor = strings.TrimSpace(input.Cursor)
	input.Operation = strings.TrimSpace(input.Operation)
	if input.SourceType == "" {
		return input, "", fmt.Errorf("source_type is required")
	}
	if input.SourceAccountKey == "" {
		return input, "", fmt.Errorf("source_account_key is required")
	}
	if input.SourceAccount == "" {
		input.SourceAccount = input.SourceAccountKey
	}
	if input.Schedule == "" {
		input.Schedule = "manual"
	}
	if input.Operation == "" {
		input.Operation = "existing_articles"
	}
	if input.Options == nil {
		input.Options = map[string]any{}
	}
	optionsJSON, err := json.Marshal(input.Options)
	return input, string(optionsJSON), err
}

func scanSourceSubscription(row sourceSyncScanner) (SourceSubscription, error) {
	var subscription SourceSubscription
	var optionsJSON string
	var enabled int
	err := row.Scan(&subscription.ID, &subscription.SourceType, &subscription.SourceAccountKey,
		&subscription.SourceAccount, &subscription.AgentID, &subscription.Schedule, &subscription.Cursor,
		&subscription.Operation, &optionsJSON, &enabled, &subscription.LastSuccessAt,
		&subscription.CreatedAt, &subscription.UpdatedAt)
	if err != nil {
		return SourceSubscription{}, err
	}
	subscription.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(optionsJSON), &subscription.Options); err != nil {
		return SourceSubscription{}, err
	}
	return subscription, nil
}

func (s *SourceSyncStore) CreateRun(subscriptionID, operation string) (SourceSyncRun, error) {
	subscription, err := s.GetSubscription(subscriptionID)
	if err != nil {
		return SourceSyncRun{}, err
	}
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = subscription.Operation
	}
	return s.insertRun(subscription.ID, subscription.AgentID, operation, 1, "")
}

func (s *SourceSyncStore) insertRun(subscriptionID, agentID, operation string, attempt int, retryOf string) (SourceSyncRun, error) {
	now := s.timestamp()
	id := newSourceSyncID("run", s.now())
	_, err := s.db.Exec(`
		INSERT INTO source_sync_runs (
			id, subscription_id, agent_id, requested_operation, status, attempt,
			retry_of, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, subscriptionID, agentID, operation, SourceRunQueued, attempt, retryOf, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "idx_source_sync_runs_one_active") || strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			return SourceSyncRun{}, ErrSourceRunActive
		}
		return SourceSyncRun{}, err
	}
	return s.GetRun(id)
}

func (s *SourceSyncStore) LeaseNextRun(agentID string, capabilities []string, leaseDuration time.Duration) (*SourceSyncRun, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	capabilities = normalizeSourceCapabilities(capabilities)
	if len(capabilities) == 0 {
		return nil, nil
	}
	if leaseDuration <= 0 {
		leaseDuration = 2 * time.Minute
	}
	if _, err := s.RequeueExpiredRuns(); err != nil {
		return nil, err
	}

	placeholders := make([]string, len(capabilities))
	args := make([]any, 0, len(capabilities)+1)
	args = append(args, agentID)
	for index, capability := range capabilities {
		placeholders[index] = "?"
		args = append(args, capability)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	query := `
		SELECT id FROM source_sync_runs
		WHERE status = ? AND (agent_id = '' OR agent_id = ?)
			AND requested_operation IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY created_at, id
		LIMIT 1
	`
	queryArgs := append([]any{SourceRunQueued}, args...)
	var runID string
	if err := tx.QueryRow(query, queryArgs...).Scan(&runID); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	result, err := tx.Exec(`
		UPDATE source_sync_runs SET status = ?, lease_owner = ?, lease_expires_at = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, SourceRunLeased, agentID, now.Add(leaseDuration).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), runID, SourceRunQueued)
	if err != nil {
		return nil, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	run, err := s.GetRun(runID)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *SourceSyncStore) StartRun(runID, agentID string) (SourceSyncRun, error) {
	run, err := s.GetRun(runID)
	if err != nil {
		return SourceSyncRun{}, err
	}
	if isTerminalSourceRunStatus(run.Status) {
		return SourceSyncRun{}, ErrSourceRunTerminal
	}
	if run.LeaseOwner != strings.TrimSpace(agentID) {
		return SourceSyncRun{}, ErrSourceRunLeaseOwner
	}
	if sourceLeaseExpired(run.LeaseExpiresAt, s.now()) {
		return SourceSyncRun{}, ErrSourceRunLeaseExpired
	}
	if run.Status != SourceRunLeased {
		return SourceSyncRun{}, ErrSourceRunInvalidState
	}
	now := s.timestamp()
	result, err := s.db.Exec(`
		UPDATE source_sync_runs SET status = ?, started_at = ?, updated_at = ?
		WHERE id = ? AND status = ? AND lease_owner = ?
	`, SourceRunRunning, now, now, run.ID, SourceRunLeased, run.LeaseOwner)
	if err != nil {
		return SourceSyncRun{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return SourceSyncRun{}, ErrSourceRunInvalidState
	}
	return s.GetRun(run.ID)
}

func (s *SourceSyncStore) RecordRunItem(runID, agentID string, input SourceSyncItemInput) (SourceSyncItem, error) {
	run, err := s.GetRun(runID)
	if err != nil {
		return SourceSyncItem{}, err
	}
	if run.Status != SourceRunRunning {
		if isTerminalSourceRunStatus(run.Status) {
			return SourceSyncItem{}, ErrSourceRunTerminal
		}
		return SourceSyncItem{}, ErrSourceRunInvalidState
	}
	if run.LeaseOwner != strings.TrimSpace(agentID) {
		return SourceSyncItem{}, ErrSourceRunLeaseOwner
	}
	if sourceLeaseExpired(run.LeaseExpiresAt, s.now()) {
		return SourceSyncItem{}, ErrSourceRunLeaseExpired
	}
	input.SourceItemKey = strings.TrimSpace(input.SourceItemKey)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Outcome = strings.TrimSpace(input.Outcome)
	if input.SourceItemKey == "" || input.IdempotencyKey == "" {
		return SourceSyncItem{}, fmt.Errorf("source_item_key and idempotency_key are required")
	}
	if !isSourceItemOutcome(input.Outcome) {
		return SourceSyncItem{}, fmt.Errorf("unsupported source item outcome %q", input.Outcome)
	}
	now := s.timestamp()
	id := newSourceSyncID("item", s.now())
	_, err = s.db.Exec(`
		INSERT INTO source_sync_items (
			id, run_id, source_item_key, idempotency_key, content_hash, outcome,
			target_book_id, error_text, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, source_item_key) DO UPDATE SET
			idempotency_key = excluded.idempotency_key,
			content_hash = excluded.content_hash,
			outcome = excluded.outcome,
			target_book_id = excluded.target_book_id,
			error_text = excluded.error_text,
			updated_at = excluded.updated_at
	`, id, run.ID, input.SourceItemKey, input.IdempotencyKey, strings.TrimSpace(input.ContentHash),
		input.Outcome, strings.TrimSpace(input.TargetBookID), strings.TrimSpace(input.Error), now, now)
	if err != nil {
		return SourceSyncItem{}, err
	}
	return s.getRunItem(run.ID, input.SourceItemKey)
}

func (s *SourceSyncStore) CompleteRun(runID, agentID string) (SourceSyncRun, error) {
	tx, run, err := s.beginOwnedRunTransition(runID, agentID, SourceRunRunning)
	if err != nil {
		return SourceSyncRun{}, err
	}
	defer tx.Rollback()
	var newCount, updatedCount, skippedCount, failedCount int
	err = tx.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN outcome = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN outcome = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN outcome = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN outcome = ? THEN 1 ELSE 0 END), 0)
		FROM source_sync_items WHERE run_id = ?
	`, SourceItemNew, SourceItemUpdated, SourceItemSkipped, SourceItemFailed, run.ID).
		Scan(&newCount, &updatedCount, &skippedCount, &failedCount)
	if err != nil {
		return SourceSyncRun{}, err
	}
	status := SourceRunSucceeded
	if failedCount > 0 {
		status = SourceRunPartial
		if newCount+updatedCount+skippedCount == 0 {
			status = SourceRunFailed
		}
	}
	now := s.timestamp()
	result, err := tx.Exec(`
		UPDATE source_sync_runs SET
			status = ?, new_count = ?, updated_count = ?, skipped_count = ?, failed_count = ?,
			lease_owner = '', lease_expires_at = '', finished_at = ?, updated_at = ?
		WHERE id = ? AND status = ? AND lease_owner = ?
	`, status, newCount, updatedCount, skippedCount, failedCount, now, now,
		run.ID, SourceRunRunning, strings.TrimSpace(agentID))
	if err != nil {
		return SourceSyncRun{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return SourceSyncRun{}, ErrSourceRunInvalidState
	}
	if status == SourceRunSucceeded || status == SourceRunPartial {
		if _, err := tx.Exec(`UPDATE source_subscriptions SET last_success_at = ?, updated_at = ? WHERE id = ?`, now, now, run.SubscriptionID); err != nil {
			return SourceSyncRun{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SourceSyncRun{}, err
	}
	return s.GetRun(run.ID)
}

func (s *SourceSyncStore) FailRun(runID, agentID, message string) (SourceSyncRun, error) {
	run, err := s.GetRun(runID)
	if err != nil {
		return SourceSyncRun{}, err
	}
	if isTerminalSourceRunStatus(run.Status) {
		return SourceSyncRun{}, ErrSourceRunTerminal
	}
	if run.LeaseOwner != strings.TrimSpace(agentID) {
		return SourceSyncRun{}, ErrSourceRunLeaseOwner
	}
	if sourceLeaseExpired(run.LeaseExpiresAt, s.now()) {
		return SourceSyncRun{}, ErrSourceRunLeaseExpired
	}
	if run.Status != SourceRunLeased && run.Status != SourceRunRunning {
		return SourceSyncRun{}, ErrSourceRunInvalidState
	}
	now := s.timestamp()
	result, err := s.db.Exec(`
		UPDATE source_sync_runs SET status = ?, error_text = ?, lease_owner = '', lease_expires_at = '',
			finished_at = ?, updated_at = ?
		WHERE id = ? AND status = ? AND lease_owner = ?
	`, SourceRunFailed, strings.TrimSpace(message), now, now, run.ID, run.Status, strings.TrimSpace(agentID))
	if err != nil {
		return SourceSyncRun{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return SourceSyncRun{}, ErrSourceRunInvalidState
	}
	return s.GetRun(run.ID)
}

func (s *SourceSyncStore) beginOwnedRunTransition(runID, agentID, requiredStatus string) (*sql.Tx, SourceSyncRun, error) {
	run, err := s.GetRun(runID)
	if err != nil {
		return nil, SourceSyncRun{}, err
	}
	if isTerminalSourceRunStatus(run.Status) {
		return nil, SourceSyncRun{}, ErrSourceRunTerminal
	}
	if run.LeaseOwner != strings.TrimSpace(agentID) {
		return nil, SourceSyncRun{}, ErrSourceRunLeaseOwner
	}
	if sourceLeaseExpired(run.LeaseExpiresAt, s.now()) {
		return nil, SourceSyncRun{}, ErrSourceRunLeaseExpired
	}
	if run.Status != requiredStatus {
		return nil, SourceSyncRun{}, ErrSourceRunInvalidState
	}
	tx, err := s.db.Begin()
	return tx, run, err
}

func (s *SourceSyncStore) RequeueExpiredRuns() (int64, error) {
	now := s.timestamp()
	result, err := s.db.Exec(`
		UPDATE source_sync_runs SET
			status = ?, lease_owner = '', lease_expires_at = '', started_at = '', updated_at = ?
		WHERE status IN (?, ?) AND lease_expires_at != '' AND lease_expires_at <= ?
	`, SourceRunQueued, now, SourceRunLeased, SourceRunRunning, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SourceSyncStore) RetryRun(runID string) (SourceSyncRun, error) {
	run, err := s.GetRun(runID)
	if err != nil {
		return SourceSyncRun{}, err
	}
	if run.Status != SourceRunFailed && run.Status != SourceRunPartial {
		return SourceSyncRun{}, ErrSourceRunNotRetryable
	}
	return s.insertRun(run.SubscriptionID, run.AgentID, run.RequestedOperation, run.Attempt+1, run.ID)
}

func (s *SourceSyncStore) CancelRun(runID string) (SourceSyncRun, error) {
	run, err := s.GetRun(runID)
	if err != nil {
		return SourceSyncRun{}, err
	}
	if isTerminalSourceRunStatus(run.Status) {
		return run, nil
	}
	now := s.timestamp()
	result, err := s.db.Exec(`
		UPDATE source_sync_runs SET status = ?, lease_owner = '', lease_expires_at = '',
			finished_at = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, SourceRunCanceled, now, now, run.ID, run.Status)
	if err != nil {
		return SourceSyncRun{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return SourceSyncRun{}, ErrSourceRunInvalidState
	}
	return s.GetRun(run.ID)
}

func (s *SourceSyncStore) GetRun(runID string) (SourceSyncRun, error) {
	run, err := scanSourceSyncRun(s.db.QueryRow(sourceSyncRunSelect+` WHERE id = ?`, strings.TrimSpace(runID)))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceSyncRun{}, ErrSourceRunNotFound
	}
	return run, err
}

func (s *SourceSyncStore) ListRuns(limit int) ([]SourceSyncRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(sourceSyncRunSelect+` ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]SourceSyncRun, 0)
	for rows.Next() {
		run, err := scanSourceSyncRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

const sourceSyncRunSelect = `
	SELECT id, subscription_id, agent_id, requested_operation, status, attempt, retry_of,
		lease_owner, lease_expires_at, new_count, updated_count, skipped_count, failed_count,
		error_text, created_at, updated_at, started_at, finished_at
	FROM source_sync_runs`

func scanSourceSyncRun(row sourceSyncScanner) (SourceSyncRun, error) {
	var run SourceSyncRun
	err := row.Scan(&run.ID, &run.SubscriptionID, &run.AgentID, &run.RequestedOperation,
		&run.Status, &run.Attempt, &run.RetryOf, &run.LeaseOwner, &run.LeaseExpiresAt,
		&run.NewCount, &run.UpdatedCount, &run.SkippedCount, &run.FailedCount, &run.Error,
		&run.CreatedAt, &run.UpdatedAt, &run.StartedAt, &run.FinishedAt)
	return run, err
}

func (s *SourceSyncStore) ListRunItems(runID string) ([]SourceSyncItem, error) {
	rows, err := s.db.Query(`
		SELECT id, run_id, source_item_key, idempotency_key, content_hash, outcome,
			target_book_id, error_text, created_at, updated_at
		FROM source_sync_items WHERE run_id = ? ORDER BY created_at, id
	`, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SourceSyncItem, 0)
	for rows.Next() {
		item, err := scanSourceSyncItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SourceSyncStore) getRunItem(runID, sourceItemKey string) (SourceSyncItem, error) {
	return scanSourceSyncItem(s.db.QueryRow(`
		SELECT id, run_id, source_item_key, idempotency_key, content_hash, outcome,
			target_book_id, error_text, created_at, updated_at
		FROM source_sync_items WHERE run_id = ? AND source_item_key = ?
	`, runID, sourceItemKey))
}

func scanSourceSyncItem(row sourceSyncScanner) (SourceSyncItem, error) {
	var item SourceSyncItem
	err := row.Scan(&item.ID, &item.RunID, &item.SourceItemKey, &item.IdempotencyKey,
		&item.ContentHash, &item.Outcome, &item.TargetBookID, &item.Error,
		&item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *SourceSyncStore) timestamp() string {
	return s.now().UTC().Format(time.RFC3339Nano)
}

func normalizeSourceCapabilities(capabilities []string) []string {
	seen := make(map[string]struct{}, len(capabilities))
	normalized := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			continue
		}
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		normalized = append(normalized, capability)
	}
	sort.Strings(normalized)
	return normalized
}

func isSourceItemOutcome(outcome string) bool {
	switch outcome {
	case SourceItemNew, SourceItemUpdated, SourceItemSkipped, SourceItemFailed:
		return true
	default:
		return false
	}
}

func isTerminalSourceRunStatus(status string) bool {
	switch status {
	case SourceRunSucceeded, SourceRunPartial, SourceRunFailed, SourceRunCanceled:
		return true
	default:
		return false
	}
}

func sourceLeaseExpired(value string, now time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return true
	}
	return !expiresAt.After(now.UTC())
}

func newSourceSyncID(prefix string, now time.Time) string {
	var randomBytes [8]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return prefix + "_" + now.UTC().Format("20060102T150405.000000000Z")
	}
	return prefix + "_" + hex.EncodeToString(randomBytes[:])
}
