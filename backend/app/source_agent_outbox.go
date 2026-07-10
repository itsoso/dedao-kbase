package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const sourceAgentOutboxDBName = "source_agent_outbox.sqlite3"

const (
	SourceOutboxPending = "pending"
	SourceOutboxDead    = "dead"
)

type SourceAgentOutboxItem struct {
	ID             string                `json:"id"`
	RunID          string                `json:"run_id"`
	IdempotencyKey string                `json:"idempotency_key"`
	Envelope       SourceArticleEnvelope `json:"envelope"`
	State          string                `json:"state"`
	AttemptCount   int                   `json:"attempt_count"`
	NextAttemptAt  string                `json:"next_attempt_at"`
	LastError      string                `json:"last_error,omitempty"`
	CreatedAt      string                `json:"created_at"`
	UpdatedAt      string                `json:"updated_at"`
}

type SourceAgentOutbox struct {
	dbPath      string
	db          *sql.DB
	now         func() time.Time
	jitter      func(time.Duration) time.Duration
	maxAttempts int
}

func NewSourceAgentOutbox(stateDir string) (*SourceAgentOutbox, error) {
	return newSourceAgentOutbox(stateDir, time.Now, sourceAgentOutboxJitter, 8)
}

func newSourceAgentOutbox(
	stateDir string,
	now func() time.Time,
	jitter func(time.Duration) time.Duration,
	maxAttempts int,
) (*SourceAgentOutbox, error) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return nil, fmt.Errorf("WCPLUS_AGENT_STATE_DIR is required")
	}
	if now == nil {
		now = time.Now
	}
	if jitter == nil {
		jitter = sourceAgentOutboxJitter
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(stateDir, sourceAgentOutboxDBName)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateSourceAgentOutbox(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(dbPath, 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return &SourceAgentOutbox{
		dbPath:      dbPath,
		db:          db,
		now:         now,
		jitter:      jitter,
		maxAttempts: maxAttempts,
	}, nil
}

func migrateSourceAgentOutbox(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS source_agent_outbox (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			idempotency_key TEXT NOT NULL UNIQUE,
			envelope_json TEXT NOT NULL,
			state TEXT NOT NULL,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TEXT NOT NULL,
			next_attempt_at_ns INTEGER NOT NULL,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			created_at_ns INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			updated_at_ns INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_source_agent_outbox_ready
			ON source_agent_outbox(state, next_attempt_at_ns, created_at_ns);
	`)
	return err
}

func (o *SourceAgentOutbox) DBPath() string {
	if o == nil {
		return ""
	}
	return o.dbPath
}

func (o *SourceAgentOutbox) Close() error {
	if o == nil || o.db == nil {
		return nil
	}
	return o.db.Close()
}

func (o *SourceAgentOutbox) Enqueue(runID string, envelope SourceArticleEnvelope) (SourceAgentOutboxItem, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return SourceAgentOutboxItem{}, fmt.Errorf("run_id is required")
	}
	normalized, _, err := normalizeSourceArticleEnvelope(envelope)
	if err != nil {
		return SourceAgentOutboxItem{}, err
	}
	envelopeJSON, err := json.Marshal(normalized)
	if err != nil {
		return SourceAgentOutboxItem{}, err
	}
	nowTime := o.now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	id := newSourceSyncID("outbox", o.now())
	_, err = o.db.Exec(`
		INSERT INTO source_agent_outbox (
			id, run_id, idempotency_key, envelope_json, state, attempt_count,
			next_attempt_at, next_attempt_at_ns, last_error,
			created_at, created_at_ns, updated_at, updated_at_ns
		) VALUES (?, ?, ?, ?, ?, 0, ?, ?, '', ?, ?, ?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING
	`, id, runID, normalized.IdempotencyKey, string(envelopeJSON), SourceOutboxPending,
		now, nowTime.UnixNano(), now, nowTime.UnixNano(), now, nowTime.UnixNano())
	if err != nil {
		return SourceAgentOutboxItem{}, err
	}
	return o.getByIdempotencyKey(normalized.IdempotencyKey)
}

func (o *SourceAgentOutbox) PeekReady(limit int) ([]SourceAgentOutboxItem, error) {
	return o.peekReady("", limit)
}

func (o *SourceAgentOutbox) PeekReadyForRun(runID string, limit int) ([]SourceAgentOutboxItem, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run_id is required")
	}
	return o.peekReady(runID, limit)
}

func (o *SourceAgentOutbox) peekReady(runID string, limit int) ([]SourceAgentOutboxItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	query := sourceAgentOutboxSelect + ` WHERE state = ? AND next_attempt_at_ns <= ?`
	args := []any{SourceOutboxPending, o.now().UTC().UnixNano()}
	if runID != "" {
		query += ` AND run_id = ?`
		args = append(args, runID)
	}
	query += ` ORDER BY created_at_ns, id LIMIT ?`
	args = append(args, limit)
	rows, err := o.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SourceAgentOutboxItem, 0)
	for rows.Next() {
		item, err := scanSourceAgentOutboxItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (o *SourceAgentOutbox) CountPendingForRun(runID string) (int, error) {
	var count int
	err := o.db.QueryRow(`SELECT COUNT(*) FROM source_agent_outbox WHERE run_id = ? AND state = ?`,
		strings.TrimSpace(runID), SourceOutboxPending).Scan(&count)
	return count, err
}

func (o *SourceAgentOutbox) Acknowledge(id string) error {
	result, err := o.db.Exec(`DELETE FROM source_agent_outbox WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("source outbox item not found")
	}
	return nil
}

func (o *SourceAgentOutbox) RecordFailure(id string, statusCode int, cause error) (SourceAgentOutboxItem, error) {
	item, err := o.get(strings.TrimSpace(id))
	if err != nil {
		return SourceAgentOutboxItem{}, err
	}
	if item.State == SourceOutboxDead {
		return item, nil
	}
	message := "delivery failed"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = strings.TrimSpace(cause.Error())
	}
	attemptCount := item.AttemptCount + 1
	retryable := sourceAgentDeliveryRetryable(statusCode)
	state := SourceOutboxPending
	nowTime := o.now().UTC()
	nextAttemptTime := nowTime
	nextAttemptAt := nowTime.Format(time.RFC3339Nano)
	if !retryable || attemptCount >= o.maxAttempts {
		state = SourceOutboxDead
	} else {
		delay := sourceAgentOutboxBackoff(attemptCount)
		delay += o.jitter(delay / 4)
		nextAttemptTime = nowTime.Add(delay)
		nextAttemptAt = nextAttemptTime.Format(time.RFC3339Nano)
	}
	updatedAt := o.now().UTC()
	_, err = o.db.Exec(`
		UPDATE source_agent_outbox SET state = ?, attempt_count = ?, next_attempt_at = ?, next_attempt_at_ns = ?,
			last_error = ?, updated_at = ?, updated_at_ns = ?
		WHERE id = ?
	`, state, attemptCount, nextAttemptAt, nextAttemptTime.UnixNano(), message,
		updatedAt.Format(time.RFC3339Nano), updatedAt.UnixNano(), item.ID)
	if err != nil {
		return SourceAgentOutboxItem{}, err
	}
	return o.get(item.ID)
}

func (o *SourceAgentOutbox) ListDeadLetters(limit int) ([]SourceAgentOutboxItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := o.db.Query(sourceAgentOutboxSelect+`
		WHERE state = ? ORDER BY updated_at_ns DESC, id DESC LIMIT ?
	`, SourceOutboxDead, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SourceAgentOutboxItem, 0)
	for rows.Next() {
		item, err := scanSourceAgentOutboxItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (o *SourceAgentOutbox) get(id string) (SourceAgentOutboxItem, error) {
	return scanSourceAgentOutboxItem(o.db.QueryRow(sourceAgentOutboxSelect+` WHERE id = ?`, id))
}

func (o *SourceAgentOutbox) getByIdempotencyKey(key string) (SourceAgentOutboxItem, error) {
	return scanSourceAgentOutboxItem(o.db.QueryRow(sourceAgentOutboxSelect+` WHERE idempotency_key = ?`, key))
}

const sourceAgentOutboxSelect = `
	SELECT id, run_id, idempotency_key, envelope_json, state, attempt_count,
		next_attempt_at, last_error, created_at, updated_at
	FROM source_agent_outbox`

type sourceAgentOutboxScanner interface {
	Scan(dest ...any) error
}

func scanSourceAgentOutboxItem(row sourceAgentOutboxScanner) (SourceAgentOutboxItem, error) {
	var item SourceAgentOutboxItem
	var envelopeJSON string
	if err := row.Scan(&item.ID, &item.RunID, &item.IdempotencyKey, &envelopeJSON,
		&item.State, &item.AttemptCount, &item.NextAttemptAt, &item.LastError,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return SourceAgentOutboxItem{}, err
	}
	if err := json.Unmarshal([]byte(envelopeJSON), &item.Envelope); err != nil {
		return SourceAgentOutboxItem{}, err
	}
	return item, nil
}

func (o *SourceAgentOutbox) timestamp() string {
	return o.now().UTC().Format(time.RFC3339Nano)
}

func sourceAgentDeliveryRetryable(statusCode int) bool {
	return statusCode == 0 || statusCode >= 500 || statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests
}

func sourceAgentOutboxBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second
	for current := 1; current < attempt && delay < 5*time.Minute; current++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func sourceAgentOutboxJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(max) + 1))
}
