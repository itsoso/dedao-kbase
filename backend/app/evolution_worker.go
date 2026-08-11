package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	EvolutionCapabilityKnowledge   EvolutionWorkerCapability = "knowledge_evolution"
	EvolutionCapabilityAgent       EvolutionWorkerCapability = "agent_evolution"
	EvolutionCapabilityEvaluation  EvolutionWorkerCapability = "evaluation"
	EvolutionCapabilityRelease     EvolutionWorkerCapability = "release"
	EvolutionCapabilityObservation EvolutionWorkerCapability = "observation"

	EvolutionWorkPending   EvolutionWorkStatus = "pending"
	EvolutionWorkLeased    EvolutionWorkStatus = "leased"
	EvolutionWorkCompleted EvolutionWorkStatus = "completed"
	EvolutionWorkBlocked   EvolutionWorkStatus = "blocked"

	EvolutionOutboxPending    EvolutionOutboxStatus = "pending"
	EvolutionOutboxLeased     EvolutionOutboxStatus = "leased"
	EvolutionOutboxDelivered  EvolutionOutboxStatus = "delivered"
	EvolutionOutboxDeadLetter EvolutionOutboxStatus = "dead_letter"

	evolutionMinLeaseDuration = time.Second
	evolutionMaxLeaseDuration = 15 * time.Minute
	evolutionMaxAttempts      = 10
	evolutionMaxRetryDelay    = 24 * time.Hour
	evolutionAvailableAtNow   = "server_now"
)

var (
	ErrEvolutionWorkNotFound      = errors.New("evolution work not found")
	ErrEvolutionOutboxNotFound    = errors.New("evolution outbox message not found")
	ErrEvolutionLeaseLost         = errors.New("evolution lease lost")
	ErrEvolutionLeaseExpired      = errors.New("evolution lease expired")
	ErrEvolutionCapabilityInvalid = errors.New("evolution worker capability invalid")
	ErrEvolutionAttemptExhausted  = errors.New("evolution attempt budget exhausted")
)

type EvolutionWorkerCapability string
type EvolutionWorkStatus string
type EvolutionOutboxStatus string

type EvolutionWorkInput struct {
	IdempotencyKey string                    `json:"idempotency_key"`
	RunID          string                    `json:"run_id"`
	Capability     EvolutionWorkerCapability `json:"capability"`
	ArtifactRef    string                    `json:"artifact_ref"`
	AvailableAt    time.Time                 `json:"available_at"`
	MaxAttempts    int                       `json:"max_attempts"`
}

type EvolutionWorkLeaseInput struct {
	WorkerID      string                      `json:"worker_id"`
	Capabilities  []EvolutionWorkerCapability `json:"capabilities"`
	LeaseDuration time.Duration               `json:"lease_duration"`
}

type EvolutionWorkLeaseUpdate struct {
	WorkID        string        `json:"work_id"`
	WorkerID      string        `json:"worker_id"`
	LeaseID       string        `json:"lease_id"`
	LeaseDuration time.Duration `json:"lease_duration"`
}

type EvolutionWorkCompletion struct {
	WorkID               string `json:"work_id"`
	WorkerID             string `json:"worker_id"`
	LeaseID              string `json:"lease_id"`
	Attempt              int    `json:"attempt"`
	ResultIdempotencyKey string `json:"result_idempotency_key"`
	ResultArtifactRef    string `json:"result_artifact_ref"`
}

type EvolutionWorkFailure struct {
	WorkID                string        `json:"work_id"`
	WorkerID              string        `json:"worker_id"`
	LeaseID               string        `json:"lease_id"`
	Attempt               int           `json:"attempt"`
	FailureIdempotencyKey string        `json:"failure_idempotency_key"`
	FailureCode           string        `json:"failure_code"`
	FailureMessage        string        `json:"failure_message"`
	RetryDelay            time.Duration `json:"retry_delay"`
}

type EvolutionWork struct {
	WorkID                string                    `json:"work_id"`
	RunID                 string                    `json:"run_id"`
	Capability            EvolutionWorkerCapability `json:"capability"`
	ArtifactRef           string                    `json:"artifact_ref"`
	Status                EvolutionWorkStatus       `json:"status"`
	Attempt               int                       `json:"attempt"`
	MaxAttempts           int                       `json:"max_attempts"`
	AvailableAt           string                    `json:"available_at"`
	LeaseID               string                    `json:"lease_id"`
	WorkerID              string                    `json:"worker_id"`
	LeaseExpiresAt        string                    `json:"lease_expires_at"`
	ResultIdempotencyKey  string                    `json:"result_idempotency_key"`
	ResultArtifactRef     string                    `json:"result_artifact_ref"`
	FailureIdempotencyKey string                    `json:"failure_idempotency_key"`
	FailureCode           string                    `json:"failure_code"`
	FailureMessage        string                    `json:"failure_message"`
	CreatedAt             string                    `json:"created_at"`
	UpdatedAt             string                    `json:"updated_at"`
	inputHash             string
	resultWorkerID        string
	resultLeaseID         string
	resultAttempt         int
	availableAtUnixNano   int64
	leaseExpiresUnixNano  int64
	failureHash           string
	failureWorkerID       string
	failureLeaseID        string
	failureAttempt        int
}

type EvolutionOutboxLeaseInput struct {
	WorkerID      string        `json:"worker_id"`
	LeaseDuration time.Duration `json:"lease_duration"`
}

type EvolutionOutboxInput struct {
	IdempotencyKey string    `json:"idempotency_key"`
	RunID          string    `json:"run_id"`
	Topic          string    `json:"topic"`
	PayloadRef     string    `json:"payload_ref"`
	AvailableAt    time.Time `json:"available_at"`
	MaxAttempts    int       `json:"max_attempts"`
}

type EvolutionOutboxDelivery struct {
	OutboxID  string `json:"outbox_id"`
	WorkerID  string `json:"worker_id"`
	LeaseID   string `json:"lease_id"`
	Attempt   int    `json:"attempt"`
	ReceiptID string `json:"receipt_id"`
}

type EvolutionOutboxFailure struct {
	OutboxID              string        `json:"outbox_id"`
	WorkerID              string        `json:"worker_id"`
	LeaseID               string        `json:"lease_id"`
	Attempt               int           `json:"attempt"`
	FailureIdempotencyKey string        `json:"failure_idempotency_key"`
	FailureCode           string        `json:"failure_code"`
	FailureMessage        string        `json:"failure_message"`
	RetryDelay            time.Duration `json:"retry_delay"`
}

type EvolutionOutboxMessage struct {
	OutboxID              string                `json:"outbox_id"`
	RunID                 string                `json:"run_id"`
	Topic                 string                `json:"topic"`
	PayloadRef            string                `json:"payload_ref"`
	Status                EvolutionOutboxStatus `json:"status"`
	Attempt               int                   `json:"attempt"`
	MaxAttempts           int                   `json:"max_attempts"`
	AvailableAt           string                `json:"available_at"`
	LeaseID               string                `json:"lease_id"`
	WorkerID              string                `json:"worker_id"`
	LeaseExpiresAt        string                `json:"lease_expires_at"`
	ReceiptID             string                `json:"receipt_id"`
	FailureIdempotencyKey string                `json:"failure_idempotency_key"`
	FailureCode           string                `json:"failure_code"`
	FailureMessage        string                `json:"failure_message"`
	DeliveredAt           string                `json:"delivered_at"`
	CreatedAt             string                `json:"created_at"`
	UpdatedAt             string                `json:"updated_at"`
	inputHash             string
	availableAtUnixNano   int64
	leaseExpiresUnixNano  int64
	failureHash           string
	failureWorkerID       string
	failureLeaseID        string
	failureAttempt        int
	deliveryWorkerID      string
	deliveryLeaseID       string
	deliveryAttempt       int
}

func isAllowedEvolutionWorkerCapability(capability EvolutionWorkerCapability) bool {
	switch capability {
	case EvolutionCapabilityKnowledge, EvolutionCapabilityAgent, EvolutionCapabilityEvaluation,
		EvolutionCapabilityRelease, EvolutionCapabilityObservation:
		return true
	default:
		return false
	}
}

func (s *EvolutionControlStore) EnqueueEvolutionWork(input EvolutionWorkInput) (*EvolutionWork, bool, error) {
	now := s.now().UTC()
	normalized, inputHash, err := normalizeEvolutionWorkInput(input, now)
	if err != nil {
		return nil, false, err
	}
	workID, err := newEvolutionStoreID("work")
	if err != nil {
		return nil, false, err
	}
	timestamp := now.Format(time.RFC3339Nano)
	availableAtNS, err := evolutionWorkerUnixNano(normalized.AvailableAt)
	if err != nil {
		return nil, false, fmt.Errorf("resolve evolution work available_at: %w", err)
	}
	work := &EvolutionWork{
		WorkID: workID, RunID: normalized.RunID, Capability: normalized.Capability,
		ArtifactRef: normalized.ArtifactRef, Status: EvolutionWorkPending,
		MaxAttempts: normalized.MaxAttempts, AvailableAt: normalized.AvailableAt.UTC().Format(time.RFC3339Nano),
		CreatedAt: timestamp, UpdatedAt: timestamp, inputHash: inputHash,
		availableAtUnixNano: availableAtNS,
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin enqueue evolution work", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := rejectEvolutionTerminalRunTx(tx, normalized.RunID); err != nil {
		return nil, false, err
	}
	existing, err := loadEvolutionWorkByIdempotencyKeyTx(tx, normalized.IdempotencyKey)
	if err == nil {
		if existing.inputHash != inputHash {
			return nil, false, ErrEvolutionIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, wrapEvolutionSQLiteWriteError("commit evolution work replay", err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("find evolution work replay: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_work_items (
			work_id, idempotency_key, input_hash, run_id, capability, artifact_ref, status,
			attempt, max_attempts, available_at, available_at_unix_nano, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)
	`, work.WorkID, normalized.IdempotencyKey, inputHash, work.RunID, work.Capability,
		work.ArtifactRef, work.Status, work.MaxAttempts, work.AvailableAt, availableAtNS, timestamp, timestamp); err != nil {
		return nil, false, fmt.Errorf("insert evolution work: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit enqueue evolution work", err)
	}
	return work, true, nil
}

func (s *EvolutionControlStore) LeaseNextEvolutionWork(input EvolutionWorkLeaseInput) (*EvolutionWork, bool, error) {
	if err := validateEvolutionWorkLeaseInput(input); err != nil {
		return nil, false, err
	}
	leaseID, err := newEvolutionStoreID("lease")
	if err != nil {
		return nil, false, err
	}
	now := s.now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return nil, false, err
	}
	expiresTime, expiresNS, err := evolutionWorkerAddDuration(now, input.LeaseDuration)
	if err != nil {
		return nil, false, err
	}
	expiresAt := expiresTime.Format(time.RFC3339Nano)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(input.Capabilities)), ",")
	arguments := make([]any, 0, len(input.Capabilities)+1)
	arguments = append(arguments, nowNS)
	for _, capability := range input.Capabilities {
		arguments = append(arguments, capability)
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin lease evolution work", err)
	}
	defer func() { _ = tx.Rollback() }()
	query := evolutionWorkSelect + ` WHERE status = 'pending' AND available_at_unix_nano <= ? AND attempt < max_attempts AND capability IN (` + placeholders + `) ORDER BY available_at_unix_nano ASC, created_at ASC, work_id ASC LIMIT 1`
	work, err := scanEvolutionWork(tx.QueryRow(query, arguments...))
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, false, wrapEvolutionSQLiteWriteError("commit empty evolution work lease", err)
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("select evolution work: %w", err)
	}
	result, err := tx.Exec(`
		UPDATE evolution_work_items SET status = 'leased', attempt = attempt + 1,
			lease_id = ?, lease_owner = ?, lease_expires_at = ?, lease_expires_at_unix_nano = ?, updated_at = ?
		WHERE work_id = ? AND status = 'pending' AND attempt = ? AND available_at_unix_nano <= ?
	`, leaseID, input.WorkerID, expiresAt, expiresNS, nowText, work.WorkID, work.Attempt, nowNS)
	if err != nil {
		return nil, false, fmt.Errorf("claim evolution work: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, false, fmt.Errorf("check evolution work claim: %w", err)
	} else if affected != 1 {
		return nil, false, ErrEvolutionLeaseLost
	}
	work.Status, work.Attempt = EvolutionWorkLeased, work.Attempt+1
	work.LeaseID, work.WorkerID, work.LeaseExpiresAt = leaseID, input.WorkerID, expiresAt
	work.leaseExpiresUnixNano = expiresNS
	work.UpdatedAt = nowText
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit evolution work lease", err)
	}
	return work, true, nil
}

func (s *EvolutionControlStore) RenewEvolutionLease(input EvolutionWorkLeaseUpdate) (*EvolutionWork, error) {
	if err := validateEvolutionLeaseIdentity(input.WorkID, input.WorkerID, input.LeaseID, input.LeaseDuration); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return nil, err
	}
	expiresTime, expiresNS, err := evolutionWorkerAddDuration(now, input.LeaseDuration)
	if err != nil {
		return nil, err
	}
	expiresAt := expiresTime.Format(time.RFC3339Nano)
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, wrapEvolutionSQLiteWriteError("begin renew evolution lease", err)
	}
	defer func() { _ = tx.Rollback() }()
	work, err := loadEvolutionWorkTx(tx, input.WorkID)
	if err != nil {
		return nil, normalizeEvolutionWorkLoadError(err)
	}
	if err := validateActiveEvolutionWorkLease(work, input.WorkerID, input.LeaseID, now); err != nil {
		return nil, err
	}
	result, err := tx.Exec(`UPDATE evolution_work_items SET lease_expires_at = ?, lease_expires_at_unix_nano = ?, updated_at = ? WHERE work_id = ? AND status = 'leased' AND lease_owner = ? AND lease_id = ? AND lease_expires_at_unix_nano > ?`, expiresAt, expiresNS, timestamp, input.WorkID, input.WorkerID, input.LeaseID, nowNS)
	if err != nil {
		return nil, fmt.Errorf("renew evolution work lease: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, fmt.Errorf("check evolution work lease renewal: %w", err)
		}
		return nil, ErrEvolutionLeaseLost
	}
	work.LeaseExpiresAt, work.UpdatedAt = expiresAt, timestamp
	work.leaseExpiresUnixNano = expiresNS
	if err := tx.Commit(); err != nil {
		return nil, wrapEvolutionSQLiteWriteError("commit evolution lease renewal", err)
	}
	return work, nil
}

func (s *EvolutionControlStore) CompleteEvolutionWork(input EvolutionWorkCompletion) (*EvolutionWork, bool, error) {
	if err := validateEvolutionWorkCompletion(input); err != nil {
		return nil, false, err
	}
	resultHash := evolutionWorkerPayloadHash(input.ResultArtifactRef)
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return nil, false, err
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin complete evolution work", err)
	}
	defer func() { _ = tx.Rollback() }()
	work, err := loadEvolutionWorkTx(tx, input.WorkID)
	if err != nil {
		return nil, false, normalizeEvolutionWorkLoadError(err)
	}
	if work.Status == EvolutionWorkCompleted {
		if work.ResultIdempotencyKey == input.ResultIdempotencyKey &&
			work.ResultArtifactRef == input.ResultArtifactRef &&
			work.resultWorkerID == input.WorkerID &&
			work.resultLeaseID == input.LeaseID &&
			work.resultAttempt == input.Attempt {
			if err := tx.Commit(); err != nil {
				return nil, false, wrapEvolutionSQLiteWriteError("commit evolution work result replay", err)
			}
			return work, true, nil
		}
		return nil, false, ErrEvolutionIdempotencyConflict
	}
	if err := rejectEvolutionTerminalRunTx(tx, work.RunID); err != nil {
		return nil, false, err
	}
	var existingResultWorkID string
	if err := tx.QueryRow(`SELECT work_id FROM evolution_work_items WHERE result_idempotency_key = ?`, input.ResultIdempotencyKey).Scan(&existingResultWorkID); err == nil {
		return nil, false, ErrEvolutionIdempotencyConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("find evolution work result replay: %w", err)
	}
	if err := validateActiveEvolutionWorkLease(work, input.WorkerID, input.LeaseID, now); err != nil {
		return nil, false, err
	}
	if work.Attempt != input.Attempt {
		return nil, false, ErrEvolutionLeaseLost
	}
	result, err := tx.Exec(`
		UPDATE evolution_work_items SET status = 'completed', result_idempotency_key = ?,
			result_hash = ?, result_artifact_ref = ?, result_worker_id = ?, result_lease_id = ?,
			result_attempt = ?, lease_id = '', lease_owner = '',
			lease_expires_at = '', lease_expires_at_unix_nano = 0, updated_at = ?
		WHERE work_id = ? AND status = 'leased' AND lease_owner = ? AND lease_id = ? AND lease_expires_at_unix_nano > ?
	`, input.ResultIdempotencyKey, resultHash, input.ResultArtifactRef, input.WorkerID, input.LeaseID,
		input.Attempt, timestamp,
		input.WorkID, input.WorkerID, input.LeaseID, nowNS)
	if err != nil {
		return nil, false, fmt.Errorf("complete evolution work: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, false, fmt.Errorf("check evolution work completion: %w", err)
		}
		return nil, false, ErrEvolutionLeaseLost
	}
	eventID, err := newEvolutionStoreID("event")
	if err != nil {
		return nil, false, err
	}
	event := EvolutionEvent{EventID: eventID, RunID: work.RunID, EventType: "worker_result", Actor: input.WorkerID, Code: "worker_result_ready", Message: "worker result is ready for control-plane processing", ArtifactRefs: []string{input.ResultArtifactRef}, CreatedAt: timestamp}
	if err := event.Validate(); err != nil {
		return nil, false, fmt.Errorf("validate evolution worker result event: %w", err)
	}
	if err := s.insertEventTx(tx, event); err != nil {
		return nil, false, err
	}
	outboxID, err := newEvolutionStoreID("outbox")
	if err != nil {
		return nil, false, err
	}
	outboxKey := "sha256:" + evolutionWorkerPayloadHash(work.WorkID+":"+resultHash)
	_, outboxInputHash, err := normalizeEvolutionOutboxInput(EvolutionOutboxInput{
		IdempotencyKey: outboxKey, RunID: work.RunID, Topic: "evolution.work.completed",
		PayloadRef: input.ResultArtifactRef, AvailableAt: now, MaxAttempts: 3,
	}, now)
	if err != nil {
		return nil, false, fmt.Errorf("normalize evolution worker result outbox: %w", err)
	}
	if hook := s.hooks.beforeOutboxInsert; hook != nil {
		if err := hook(); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_outbox (
			outbox_id, idempotency_key, input_hash, run_id, topic, payload_ref, status, attempt,
			available_at, available_at_unix_nano, max_attempts, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'evolution.work.completed', ?, 'pending', 0, ?, ?, 3, ?, ?)
	`, outboxID, outboxKey, outboxInputHash, work.RunID, input.ResultArtifactRef, timestamp, nowNS, timestamp, timestamp); err != nil {
		return nil, false, fmt.Errorf("insert evolution worker result outbox: %w", err)
	}
	work.Status, work.ResultIdempotencyKey, work.ResultArtifactRef = EvolutionWorkCompleted, input.ResultIdempotencyKey, input.ResultArtifactRef
	work.resultWorkerID, work.resultLeaseID, work.resultAttempt = input.WorkerID, input.LeaseID, input.Attempt
	work.WorkerID, work.LeaseID, work.LeaseExpiresAt, work.UpdatedAt = input.WorkerID, input.LeaseID, "", timestamp
	work.leaseExpiresUnixNano = 0
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit evolution work completion", err)
	}
	return work, false, nil
}

func (s *EvolutionControlStore) FailEvolutionWork(input EvolutionWorkFailure) (*EvolutionWork, bool, error) {
	if err := validateEvolutionWorkFailure(input); err != nil {
		return nil, false, err
	}
	failureHash := evolutionWorkerFailureHash(input)
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return nil, false, err
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin fail evolution work", err)
	}
	defer func() { _ = tx.Rollback() }()
	work, err := loadEvolutionWorkTx(tx, input.WorkID)
	if err != nil {
		return nil, false, normalizeEvolutionWorkLoadError(err)
	}
	if work.FailureIdempotencyKey == input.FailureIdempotencyKey {
		if work.failureHash != failureHash || work.failureWorkerID != input.WorkerID ||
			work.failureLeaseID != input.LeaseID || work.failureAttempt != input.Attempt {
			return nil, false, ErrEvolutionIdempotencyConflict
		}
		if work.Attempt != input.Attempt || (work.Status != EvolutionWorkPending && work.Status != EvolutionWorkBlocked) {
			return nil, false, ErrEvolutionLeaseLost
		}
		legacyTerminalConflict, err := evolutionRunIsTerminalTx(tx, work.RunID)
		if err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, wrapEvolutionSQLiteWriteError("commit evolution work failure replay", err)
		}
		if legacyTerminalConflict {
			return work, work.Status == EvolutionWorkBlocked, ErrEvolutionTransitionConflict
		}
		return work, work.Status == EvolutionWorkBlocked, nil
	}
	if work.Status == EvolutionWorkBlocked {
		return nil, false, ErrEvolutionIdempotencyConflict
	}
	if err := validateActiveEvolutionWorkLease(work, input.WorkerID, input.LeaseID, now); err != nil {
		return nil, false, err
	}
	if work.Attempt != input.Attempt {
		return nil, false, ErrEvolutionLeaseLost
	}
	blocked := work.Attempt >= work.MaxAttempts
	legacyTerminalConflict := false
	if blocked {
		if _, err := tx.Exec(`UPDATE evolution_work_items SET status = 'blocked', lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, failure_code = ?, failure_message = ?, failure_idempotency_key = ?, failure_hash = ?, failure_worker_id = ?, failure_lease_id = ?, failure_attempt = ?, updated_at = ? WHERE work_id = ? AND status = 'leased' AND lease_owner = ? AND lease_id = ? AND lease_expires_at_unix_nano > ?`, input.FailureCode, input.FailureMessage, input.FailureIdempotencyKey, failureHash, input.WorkerID, input.LeaseID, input.Attempt, timestamp, input.WorkID, input.WorkerID, input.LeaseID, nowNS); err != nil {
			return nil, false, fmt.Errorf("block exhausted evolution work: %w", err)
		}
		legacyTerminalConflict, err = s.blockEvolutionRunTx(tx, work.RunID, "worker_attempts_exhausted", input.FailureMessage, timestamp)
		if err != nil {
			return nil, false, err
		}
		work.Status = EvolutionWorkBlocked
	} else {
		availableTime, availableNS, err := evolutionWorkerAddDuration(now, input.RetryDelay)
		if err != nil {
			return nil, false, err
		}
		availableAt := availableTime.Format(time.RFC3339Nano)
		if _, err := tx.Exec(`UPDATE evolution_work_items SET status = 'pending', available_at = ?, available_at_unix_nano = ?, lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, failure_code = ?, failure_message = ?, failure_idempotency_key = ?, failure_hash = ?, failure_worker_id = ?, failure_lease_id = ?, failure_attempt = ?, updated_at = ? WHERE work_id = ? AND status = 'leased' AND lease_owner = ? AND lease_id = ? AND lease_expires_at_unix_nano > ?`, availableAt, availableNS, input.FailureCode, input.FailureMessage, input.FailureIdempotencyKey, failureHash, input.WorkerID, input.LeaseID, input.Attempt, timestamp, input.WorkID, input.WorkerID, input.LeaseID, nowNS); err != nil {
			return nil, false, fmt.Errorf("retry failed evolution work: %w", err)
		}
		work.Status, work.AvailableAt = EvolutionWorkPending, availableAt
		work.availableAtUnixNano = availableNS
	}
	work.LeaseID, work.WorkerID, work.LeaseExpiresAt = input.LeaseID, input.WorkerID, ""
	work.FailureCode, work.FailureMessage, work.UpdatedAt = input.FailureCode, input.FailureMessage, timestamp
	work.leaseExpiresUnixNano = 0
	work.FailureIdempotencyKey, work.failureHash = input.FailureIdempotencyKey, failureHash
	work.failureWorkerID, work.failureLeaseID, work.failureAttempt = input.WorkerID, input.LeaseID, input.Attempt
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit evolution work failure", err)
	}
	if legacyTerminalConflict {
		return work, blocked, ErrEvolutionTransitionConflict
	}
	return work, blocked, nil
}

func (s *EvolutionControlStore) RecoverExpiredEvolutionLeases() (int, error) {
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return 0, err
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return 0, wrapEvolutionSQLiteWriteError("begin recover evolution leases", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(evolutionWorkSelect+` WHERE status = 'leased' AND lease_expires_at_unix_nano <= ? ORDER BY lease_expires_at_unix_nano, work_id`, nowNS)
	if err != nil {
		return 0, fmt.Errorf("list expired evolution work leases: %w", err)
	}
	var works []*EvolutionWork
	for rows.Next() {
		work, err := scanEvolutionWork(rows)
		if err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan expired evolution work: %w", err)
		}
		works = append(works, work)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("iterate expired evolution work: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close expired evolution work rows: %w", err)
	}
	exhausted := false
	legacyTerminalConflict := false
	for _, work := range works {
		failureKey := "sha256:" + evolutionWorkerPayloadHash(fmt.Sprintf("work-lease-expired:%s:%s:%d", work.WorkID, work.LeaseID, work.Attempt))
		failureInput := EvolutionWorkFailure{WorkID: work.WorkID, WorkerID: work.WorkerID, LeaseID: work.LeaseID, Attempt: work.Attempt, FailureIdempotencyKey: failureKey, FailureCode: "lease_expired", FailureMessage: "worker lease expired before completion"}
		failureHash := evolutionWorkerFailureHash(failureInput)
		if work.Attempt >= work.MaxAttempts {
			exhausted = true
			failureInput.FailureCode = "worker_attempts_exhausted"
			failureInput.FailureMessage = "worker lease expired after the attempt budget was exhausted"
			failureHash = evolutionWorkerFailureHash(failureInput)
			if _, err := tx.Exec(`UPDATE evolution_work_items SET status = 'blocked', lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, failure_code = ?, failure_message = ?, failure_idempotency_key = ?, failure_hash = ?, failure_worker_id = ?, failure_lease_id = ?, failure_attempt = ?, updated_at = ? WHERE work_id = ? AND status = 'leased' AND lease_id = ?`, failureInput.FailureCode, failureInput.FailureMessage, failureKey, failureHash, work.WorkerID, work.LeaseID, work.Attempt, timestamp, work.WorkID, work.LeaseID); err != nil {
				return 0, fmt.Errorf("block expired evolution work: %w", err)
			}
			legacyConflict, err := s.blockEvolutionRunTx(tx, work.RunID, "worker_attempts_exhausted", "worker lease expired after the attempt budget was exhausted", timestamp)
			if err != nil {
				return 0, err
			}
			legacyTerminalConflict = legacyTerminalConflict || legacyConflict
		} else if _, err := tx.Exec(`UPDATE evolution_work_items SET status = 'pending', available_at = ?, available_at_unix_nano = ?, lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, failure_code = ?, failure_message = ?, failure_idempotency_key = ?, failure_hash = ?, failure_worker_id = ?, failure_lease_id = ?, failure_attempt = ?, updated_at = ? WHERE work_id = ? AND status = 'leased' AND lease_id = ?`, timestamp, nowNS, failureInput.FailureCode, failureInput.FailureMessage, failureKey, failureHash, work.WorkerID, work.LeaseID, work.Attempt, timestamp, work.WorkID, work.LeaseID); err != nil {
			return 0, fmt.Errorf("recover expired evolution work: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, wrapEvolutionSQLiteWriteError("commit evolution lease recovery", err)
	}
	if exhausted {
		if legacyTerminalConflict {
			return len(works), ErrEvolutionTransitionConflict
		}
		return len(works), ErrEvolutionAttemptExhausted
	}
	return len(works), nil
}

func (s *EvolutionControlStore) EnqueueEvolutionOutbox(input EvolutionOutboxInput) (*EvolutionOutboxMessage, bool, error) {
	now := s.now().UTC()
	normalized, inputHash, err := normalizeEvolutionOutboxInput(input, now)
	if err != nil {
		return nil, false, err
	}
	outboxID, err := newEvolutionStoreID("outbox")
	if err != nil {
		return nil, false, err
	}
	timestamp := now.Format(time.RFC3339Nano)
	availableAtNS, err := evolutionWorkerUnixNano(normalized.AvailableAt)
	if err != nil {
		return nil, false, fmt.Errorf("resolve evolution outbox available_at: %w", err)
	}
	message := &EvolutionOutboxMessage{
		OutboxID: outboxID, RunID: normalized.RunID, Topic: normalized.Topic,
		PayloadRef: normalized.PayloadRef, Status: EvolutionOutboxPending,
		MaxAttempts: normalized.MaxAttempts, AvailableAt: normalized.AvailableAt.UTC().Format(time.RFC3339Nano),
		CreatedAt: timestamp, UpdatedAt: timestamp, inputHash: inputHash, availableAtUnixNano: availableAtNS,
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin enqueue evolution outbox", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := rejectEvolutionTerminalRunTx(tx, normalized.RunID); err != nil {
		return nil, false, err
	}
	existing, err := loadEvolutionOutboxByIdempotencyKeyTx(tx, normalized.IdempotencyKey)
	if err == nil {
		if existing.inputHash != inputHash {
			return nil, false, ErrEvolutionIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, wrapEvolutionSQLiteWriteError("commit evolution outbox replay", err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("find evolution outbox replay: %w", err)
	}
	if hook := s.hooks.beforeOutboxInsert; hook != nil {
		if err := hook(); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_outbox (
			outbox_id, idempotency_key, input_hash, run_id, topic, payload_ref, status,
			attempt, available_at, available_at_unix_nano, max_attempts, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'pending', 0, ?, ?, ?, ?, ?)
	`, message.OutboxID, normalized.IdempotencyKey, inputHash, message.RunID, message.Topic,
		message.PayloadRef, message.AvailableAt, availableAtNS, message.MaxAttempts, timestamp, timestamp); err != nil {
		return nil, false, fmt.Errorf("insert evolution outbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit enqueue evolution outbox", err)
	}
	return message, true, nil
}

func (s *EvolutionControlStore) LeaseNextEvolutionOutbox(input EvolutionOutboxLeaseInput) (*EvolutionOutboxMessage, bool, error) {
	if err := validateEvolutionIdentity("worker_id", input.WorkerID); err != nil {
		return nil, false, err
	}
	if err := validateEvolutionLeaseDuration(input.LeaseDuration); err != nil {
		return nil, false, err
	}
	leaseID, err := newEvolutionStoreID("lease")
	if err != nil {
		return nil, false, err
	}
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return nil, false, err
	}
	expiresTime, expiresNS, err := evolutionWorkerAddDuration(now, input.LeaseDuration)
	if err != nil {
		return nil, false, err
	}
	expiresAt := expiresTime.Format(time.RFC3339Nano)
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin lease evolution outbox", err)
	}
	defer func() { _ = tx.Rollback() }()
	message, err := scanEvolutionOutbox(tx.QueryRow(evolutionOutboxSelect+` WHERE status = 'pending' AND available_at_unix_nano <= ? AND attempt < max_attempts ORDER BY available_at_unix_nano, created_at, outbox_id LIMIT 1`, nowNS))
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, false, wrapEvolutionSQLiteWriteError("commit empty evolution outbox lease", err)
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("select evolution outbox: %w", err)
	}
	result, err := tx.Exec(`UPDATE evolution_outbox SET status = 'leased', attempt = attempt + 1, lease_id = ?, lease_owner = ?, lease_expires_at = ?, lease_expires_at_unix_nano = ?, updated_at = ? WHERE outbox_id = ? AND status = 'pending' AND attempt = ? AND available_at_unix_nano <= ?`, leaseID, input.WorkerID, expiresAt, expiresNS, timestamp, message.OutboxID, message.Attempt, nowNS)
	if err != nil {
		return nil, false, fmt.Errorf("claim evolution outbox: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, false, fmt.Errorf("check evolution outbox claim: %w", err)
		}
		return nil, false, ErrEvolutionLeaseLost
	}
	message.Status, message.Attempt = EvolutionOutboxLeased, message.Attempt+1
	message.LeaseID, message.WorkerID, message.LeaseExpiresAt, message.UpdatedAt = leaseID, input.WorkerID, expiresAt, timestamp
	message.leaseExpiresUnixNano = expiresNS
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit evolution outbox lease", err)
	}
	return message, true, nil
}

func (s *EvolutionControlStore) DeliverEvolutionOutbox(input EvolutionOutboxDelivery) (*EvolutionOutboxMessage, bool, error) {
	if err := validateEvolutionOutboxDelivery(input); err != nil {
		return nil, false, err
	}
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return nil, false, err
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin deliver evolution outbox", err)
	}
	defer func() { _ = tx.Rollback() }()
	message, err := loadEvolutionOutboxTx(tx, input.OutboxID)
	if err != nil {
		return nil, false, normalizeEvolutionOutboxLoadError(err)
	}
	if message.Status == EvolutionOutboxDelivered {
		if message.ReceiptID == input.ReceiptID && message.deliveryWorkerID == input.WorkerID &&
			message.deliveryLeaseID == input.LeaseID && message.deliveryAttempt == input.Attempt {
			if err := tx.Commit(); err != nil {
				return nil, false, wrapEvolutionSQLiteWriteError("commit evolution outbox receipt replay", err)
			}
			return message, true, nil
		}
		return nil, false, ErrEvolutionIdempotencyConflict
	}
	var receiptOutboxID string
	if err := tx.QueryRow(`SELECT outbox_id FROM evolution_outbox WHERE receipt_id = ?`, input.ReceiptID).Scan(&receiptOutboxID); err == nil {
		return nil, false, ErrEvolutionIdempotencyConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("find evolution outbox receipt replay: %w", err)
	}
	if err := validateActiveEvolutionOutboxLease(message, input.WorkerID, input.LeaseID, now); err != nil {
		return nil, false, err
	}
	if message.Attempt != input.Attempt {
		return nil, false, ErrEvolutionLeaseLost
	}
	result, err := tx.Exec(`UPDATE evolution_outbox SET status = 'delivered', receipt_id = ?, delivery_worker_id = ?, delivery_lease_id = ?, delivery_attempt = ?, delivered_at = ?, lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, updated_at = ? WHERE outbox_id = ? AND status = 'leased' AND lease_owner = ? AND lease_id = ? AND lease_expires_at_unix_nano > ?`, input.ReceiptID, input.WorkerID, input.LeaseID, input.Attempt, timestamp, timestamp, input.OutboxID, input.WorkerID, input.LeaseID, nowNS)
	if err != nil {
		return nil, false, fmt.Errorf("deliver evolution outbox: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, false, fmt.Errorf("check evolution outbox delivery: %w", err)
		}
		return nil, false, ErrEvolutionLeaseLost
	}
	message.Status, message.ReceiptID, message.DeliveredAt = EvolutionOutboxDelivered, input.ReceiptID, timestamp
	message.deliveryWorkerID, message.deliveryLeaseID, message.deliveryAttempt = input.WorkerID, input.LeaseID, input.Attempt
	message.LeaseID, message.WorkerID, message.LeaseExpiresAt, message.UpdatedAt = input.LeaseID, input.WorkerID, "", timestamp
	message.leaseExpiresUnixNano = 0
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit evolution outbox delivery", err)
	}
	return message, false, nil
}

func (s *EvolutionControlStore) FailEvolutionOutbox(input EvolutionOutboxFailure) (*EvolutionOutboxMessage, bool, error) {
	if err := validateEvolutionOutboxFailure(input); err != nil {
		return nil, false, err
	}
	failureHash := evolutionOutboxFailureHash(input)
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return nil, false, err
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin fail evolution outbox", err)
	}
	defer func() { _ = tx.Rollback() }()
	message, err := loadEvolutionOutboxTx(tx, input.OutboxID)
	if err != nil {
		return nil, false, normalizeEvolutionOutboxLoadError(err)
	}
	if message.FailureIdempotencyKey == input.FailureIdempotencyKey {
		if message.failureHash != failureHash || message.failureWorkerID != input.WorkerID ||
			message.failureLeaseID != input.LeaseID || message.failureAttempt != input.Attempt {
			return nil, false, ErrEvolutionIdempotencyConflict
		}
		if message.Attempt != input.Attempt || (message.Status != EvolutionOutboxPending && message.Status != EvolutionOutboxDeadLetter) {
			return nil, false, ErrEvolutionLeaseLost
		}
		legacyTerminalConflict, err := evolutionRunIsTerminalTx(tx, message.RunID)
		if err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, wrapEvolutionSQLiteWriteError("commit evolution outbox dead-letter replay", err)
		}
		if legacyTerminalConflict {
			return message, message.Status == EvolutionOutboxDeadLetter, ErrEvolutionTransitionConflict
		}
		return message, message.Status == EvolutionOutboxDeadLetter, nil
	}
	if message.Status == EvolutionOutboxDeadLetter {
		return nil, false, ErrEvolutionIdempotencyConflict
	}
	if err := validateActiveEvolutionOutboxLease(message, input.WorkerID, input.LeaseID, now); err != nil {
		return nil, false, err
	}
	if message.Attempt != input.Attempt {
		return nil, false, ErrEvolutionLeaseLost
	}
	deadLetter := message.Attempt >= message.MaxAttempts
	legacyTerminalConflict := false
	if deadLetter {
		if _, err := tx.Exec(`UPDATE evolution_outbox SET status = 'dead_letter', lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, failure_code = ?, failure_message = ?, failure_idempotency_key = ?, failure_hash = ?, failure_worker_id = ?, failure_lease_id = ?, failure_attempt = ?, updated_at = ? WHERE outbox_id = ? AND status = 'leased' AND lease_owner = ? AND lease_id = ? AND lease_expires_at_unix_nano > ?`, input.FailureCode, input.FailureMessage, input.FailureIdempotencyKey, failureHash, input.WorkerID, input.LeaseID, input.Attempt, timestamp, input.OutboxID, input.WorkerID, input.LeaseID, nowNS); err != nil {
			return nil, false, fmt.Errorf("dead-letter evolution outbox: %w", err)
		}
		legacyTerminalConflict, err = s.blockEvolutionRunTx(tx, message.RunID, "outbox_delivery_exhausted", input.FailureMessage, timestamp)
		if err != nil {
			return nil, false, err
		}
		message.Status = EvolutionOutboxDeadLetter
	} else {
		availableTime, availableNS, err := evolutionWorkerAddDuration(now, input.RetryDelay)
		if err != nil {
			return nil, false, err
		}
		availableAt := availableTime.Format(time.RFC3339Nano)
		if _, err := tx.Exec(`UPDATE evolution_outbox SET status = 'pending', available_at = ?, available_at_unix_nano = ?, lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, failure_code = ?, failure_message = ?, failure_idempotency_key = ?, failure_hash = ?, failure_worker_id = ?, failure_lease_id = ?, failure_attempt = ?, updated_at = ? WHERE outbox_id = ? AND status = 'leased' AND lease_owner = ? AND lease_id = ? AND lease_expires_at_unix_nano > ?`, availableAt, availableNS, input.FailureCode, input.FailureMessage, input.FailureIdempotencyKey, failureHash, input.WorkerID, input.LeaseID, input.Attempt, timestamp, input.OutboxID, input.WorkerID, input.LeaseID, nowNS); err != nil {
			return nil, false, fmt.Errorf("retry evolution outbox: %w", err)
		}
		message.Status, message.AvailableAt = EvolutionOutboxPending, availableAt
		message.availableAtUnixNano = availableNS
	}
	message.LeaseID, message.WorkerID, message.LeaseExpiresAt = input.LeaseID, input.WorkerID, ""
	message.FailureCode, message.FailureMessage, message.UpdatedAt = input.FailureCode, input.FailureMessage, timestamp
	message.leaseExpiresUnixNano = 0
	message.FailureIdempotencyKey, message.failureHash = input.FailureIdempotencyKey, failureHash
	message.failureWorkerID, message.failureLeaseID, message.failureAttempt = input.WorkerID, input.LeaseID, input.Attempt
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit evolution outbox failure", err)
	}
	if legacyTerminalConflict {
		return message, deadLetter, ErrEvolutionTransitionConflict
	}
	return message, deadLetter, nil
}

func (s *EvolutionControlStore) RecoverExpiredEvolutionOutboxLeases() (int, error) {
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return 0, err
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return 0, wrapEvolutionSQLiteWriteError("begin recover evolution outbox leases", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(evolutionOutboxSelect+` WHERE status = 'leased' AND lease_expires_at_unix_nano <= ? ORDER BY lease_expires_at_unix_nano, outbox_id`, nowNS)
	if err != nil {
		return 0, fmt.Errorf("list expired evolution outbox leases: %w", err)
	}
	var messages []*EvolutionOutboxMessage
	for rows.Next() {
		message, err := scanEvolutionOutbox(rows)
		if err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan expired evolution outbox: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("iterate expired evolution outbox: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close expired evolution outbox rows: %w", err)
	}
	exhausted := false
	legacyTerminalConflict := false
	for _, message := range messages {
		failureKey := "sha256:" + evolutionWorkerPayloadHash(fmt.Sprintf("outbox-lease-expired:%s:%s:%d", message.OutboxID, message.LeaseID, message.Attempt))
		failureInput := EvolutionOutboxFailure{OutboxID: message.OutboxID, WorkerID: message.WorkerID, LeaseID: message.LeaseID, Attempt: message.Attempt, FailureIdempotencyKey: failureKey, FailureCode: "lease_expired", FailureMessage: "outbox lease expired before delivery"}
		failureHash := evolutionOutboxFailureHash(failureInput)
		if message.Attempt >= message.MaxAttempts {
			exhausted = true
			failureInput.FailureCode = "outbox_delivery_exhausted"
			failureInput.FailureMessage = "outbox lease expired after the attempt budget was exhausted"
			failureHash = evolutionOutboxFailureHash(failureInput)
			if _, err := tx.Exec(`UPDATE evolution_outbox SET status = 'dead_letter', lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, failure_code = ?, failure_message = ?, failure_idempotency_key = ?, failure_hash = ?, failure_worker_id = ?, failure_lease_id = ?, failure_attempt = ?, updated_at = ? WHERE outbox_id = ? AND status = 'leased' AND lease_id = ?`, failureInput.FailureCode, failureInput.FailureMessage, failureKey, failureHash, message.WorkerID, message.LeaseID, message.Attempt, timestamp, message.OutboxID, message.LeaseID); err != nil {
				return 0, fmt.Errorf("dead-letter expired evolution outbox: %w", err)
			}
			legacyConflict, err := s.blockEvolutionRunTx(tx, message.RunID, "outbox_delivery_exhausted", "outbox lease expired after the attempt budget was exhausted", timestamp)
			if err != nil {
				return 0, err
			}
			legacyTerminalConflict = legacyTerminalConflict || legacyConflict
		} else if _, err := tx.Exec(`UPDATE evolution_outbox SET status = 'pending', available_at = ?, available_at_unix_nano = ?, lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, failure_code = ?, failure_message = ?, failure_idempotency_key = ?, failure_hash = ?, failure_worker_id = ?, failure_lease_id = ?, failure_attempt = ?, updated_at = ? WHERE outbox_id = ? AND status = 'leased' AND lease_id = ?`, timestamp, nowNS, failureInput.FailureCode, failureInput.FailureMessage, failureKey, failureHash, message.WorkerID, message.LeaseID, message.Attempt, timestamp, message.OutboxID, message.LeaseID); err != nil {
			return 0, fmt.Errorf("recover expired evolution outbox: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, wrapEvolutionSQLiteWriteError("commit evolution outbox lease recovery", err)
	}
	if exhausted {
		if legacyTerminalConflict {
			return len(messages), ErrEvolutionTransitionConflict
		}
		return len(messages), ErrEvolutionAttemptExhausted
	}
	return len(messages), nil
}

func (s *EvolutionControlStore) blockEvolutionRunTx(tx *sql.Tx, runID, code, message, timestamp string) (bool, error) {
	run, err := scanEvolutionRun(tx.QueryRow(evolutionRunSelect+` WHERE run_id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrEvolutionRunNotFound
	}
	if err != nil {
		return false, fmt.Errorf("load evolution run for worker block: %w", err)
	}
	if run.Status == EvolutionBlocked {
		if strings.TrimSpace(message) == "" {
			message = "evolution delivery exhausted its retry budget"
		}
		eventID, err := newEvolutionStoreID("event")
		if err != nil {
			return false, err
		}
		event := EvolutionEvent{EventID: eventID, RunID: runID, EventType: "worker_failure", Actor: "control-plane", Code: code, Message: message, ArtifactRefs: []string{}, CreatedAt: timestamp}
		if err := event.Validate(); err != nil {
			return false, fmt.Errorf("validate evolution worker failure event: %w", err)
		}
		return false, s.insertEventTx(tx, event)
	}
	if err := ValidateEvolutionTransition(run.Status, EvolutionBlocked); err != nil {
		if isEvolutionTerminalStatus(run.Status) {
			eventID, idErr := newEvolutionStoreID("event")
			if idErr != nil {
				return false, idErr
			}
			if strings.TrimSpace(message) == "" {
				message = "evolution delivery exhausted its retry budget"
			}
			event := EvolutionEvent{EventID: eventID, RunID: runID, EventType: "worker_failure", Actor: "control-plane", Code: code, Message: message, ArtifactRefs: []string{}, CreatedAt: timestamp}
			if validateErr := event.Validate(); validateErr != nil {
				return false, fmt.Errorf("validate legacy terminal evolution failure event: %w", validateErr)
			}
			if insertErr := s.insertEventTx(tx, event); insertErr != nil {
				return false, insertErr
			}
			return true, nil
		}
		return false, fmt.Errorf("%w: %v", ErrEvolutionTransitionConflict, err)
	}
	if err := validateEvolutionCode("failure_code", code); err != nil {
		return false, err
	}
	if err := validateEvolutionText("failure_message", message, EvolutionFailureMessageMaxRunes); err != nil {
		return false, err
	}
	from := run.Status
	updatedAt, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return false, fmt.Errorf("parse evolution worker block timestamp: %w", err)
	}
	updatedAtNS, err := evolutionWorkerUnixNano(updatedAt)
	if err != nil {
		return false, err
	}
	result, err := tx.Exec(`UPDATE evolution_runs SET status = ?, failure_code = ?, failure_message = ?, updated_at = ?, updated_at_unix_nano = ? WHERE run_id = ? AND status = ?`, EvolutionBlocked, code, message, timestamp, updatedAtNS, runID, from)
	if err != nil {
		return false, fmt.Errorf("block evolution run: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return false, fmt.Errorf("check evolution run block: %w", err)
		}
		return false, ErrEvolutionTransitionConflict
	}
	eventID, err := newEvolutionStoreID("event")
	if err != nil {
		return false, err
	}
	event := EvolutionEvent{EventID: eventID, RunID: runID, EventType: "transition", Actor: "control-plane", FromStatus: from, ToStatus: EvolutionBlocked, Code: code, Message: message, ArtifactRefs: []string{}, CreatedAt: timestamp}
	if err := event.Validate(); err != nil {
		return false, fmt.Errorf("validate evolution worker block event: %w", err)
	}
	return false, s.insertEventTx(tx, event)
}

func evolutionRunIsTerminalTx(tx *sql.Tx, runID string) (bool, error) {
	var status EvolutionRunStatus
	if err := tx.QueryRow(`SELECT status FROM evolution_runs WHERE run_id = ?`, runID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return false, ErrEvolutionRunNotFound
	} else if err != nil {
		return false, fmt.Errorf("load evolution run status: %w", err)
	}
	return isEvolutionTerminalStatus(status), nil
}

func rejectEvolutionTerminalRunTx(tx *sql.Tx, runID string) error {
	terminal, err := evolutionRunIsTerminalTx(tx, runID)
	if err != nil {
		return err
	}
	if terminal {
		return ErrEvolutionTransitionConflict
	}
	return nil
}

func isEvolutionTerminalStatus(status EvolutionRunStatus) bool {
	switch status {
	case EvolutionCompleted, EvolutionRejected, EvolutionFailed, EvolutionSuperseded, EvolutionRolledBack:
		return true
	default:
		return false
	}
}

func normalizeEvolutionWorkInput(input EvolutionWorkInput, now time.Time) (EvolutionWorkInput, string, error) {
	if !isEvolutionOpaqueID(input.IdempotencyKey) {
		return EvolutionWorkInput{}, "", fmt.Errorf("idempotency_key must be a canonical UUID or sha256 identity")
	}
	if err := validateEvolutionIdentity("run_id", input.RunID); err != nil {
		return EvolutionWorkInput{}, "", err
	}
	if !isAllowedEvolutionWorkerCapability(input.Capability) {
		return EvolutionWorkInput{}, "", fmt.Errorf("%w: %q", ErrEvolutionCapabilityInvalid, input.Capability)
	}
	if err := validateEvolutionWorkerArtifactRef("artifact_ref", input.ArtifactRef); err != nil {
		return EvolutionWorkInput{}, "", err
	}
	if input.MaxAttempts < 1 || input.MaxAttempts > evolutionMaxAttempts {
		return EvolutionWorkInput{}, "", fmt.Errorf("max_attempts must be between 1 and %d", evolutionMaxAttempts)
	}
	semanticAvailableAt := evolutionAvailableAtNow
	if input.AvailableAt.IsZero() {
		input.AvailableAt = now
	} else {
		input.AvailableAt = input.AvailableAt.UTC()
		semanticAvailableAt = input.AvailableAt.Format(time.RFC3339Nano)
	}
	fingerprint := struct {
		IdempotencyKey string                    `json:"idempotency_key"`
		RunID          string                    `json:"run_id"`
		Capability     EvolutionWorkerCapability `json:"capability"`
		ArtifactRef    string                    `json:"artifact_ref"`
		AvailableAt    string                    `json:"available_at"`
		MaxAttempts    int                       `json:"max_attempts"`
	}{
		IdempotencyKey: input.IdempotencyKey,
		RunID:          input.RunID,
		Capability:     input.Capability,
		ArtifactRef:    input.ArtifactRef,
		AvailableAt:    semanticAvailableAt,
		MaxAttempts:    input.MaxAttempts,
	}
	payload, err := json.Marshal(fingerprint)
	if err != nil {
		return EvolutionWorkInput{}, "", fmt.Errorf("encode evolution work input: %w", err)
	}
	return input, evolutionWorkerPayloadHash(string(payload)), nil
}

func normalizeEvolutionOutboxInput(input EvolutionOutboxInput, now time.Time) (EvolutionOutboxInput, string, error) {
	if !isEvolutionOpaqueID(input.IdempotencyKey) {
		return EvolutionOutboxInput{}, "", fmt.Errorf("idempotency_key must be a canonical UUID or sha256 identity")
	}
	if err := validateEvolutionIdentity("run_id", input.RunID); err != nil {
		return EvolutionOutboxInput{}, "", err
	}
	if input.Topic != "evolution.work.completed" {
		return EvolutionOutboxInput{}, "", fmt.Errorf("unsupported evolution outbox topic %q", input.Topic)
	}
	if err := validateEvolutionWorkerArtifactRef("payload_ref", input.PayloadRef); err != nil {
		return EvolutionOutboxInput{}, "", err
	}
	if input.MaxAttempts < 1 || input.MaxAttempts > evolutionMaxAttempts {
		return EvolutionOutboxInput{}, "", fmt.Errorf("max_attempts must be between 1 and %d", evolutionMaxAttempts)
	}
	semanticAvailableAt := evolutionAvailableAtNow
	if input.AvailableAt.IsZero() {
		input.AvailableAt = now
	} else {
		input.AvailableAt = input.AvailableAt.UTC()
		semanticAvailableAt = input.AvailableAt.Format(time.RFC3339Nano)
	}
	fingerprint := struct {
		IdempotencyKey string `json:"idempotency_key"`
		RunID          string `json:"run_id"`
		Topic          string `json:"topic"`
		PayloadRef     string `json:"payload_ref"`
		AvailableAt    string `json:"available_at"`
		MaxAttempts    int    `json:"max_attempts"`
	}{
		IdempotencyKey: input.IdempotencyKey,
		RunID:          input.RunID,
		Topic:          input.Topic,
		PayloadRef:     input.PayloadRef,
		AvailableAt:    semanticAvailableAt,
		MaxAttempts:    input.MaxAttempts,
	}
	payload, err := json.Marshal(fingerprint)
	if err != nil {
		return EvolutionOutboxInput{}, "", fmt.Errorf("encode evolution outbox input: %w", err)
	}
	return input, evolutionWorkerPayloadHash(string(payload)), nil
}

func validateEvolutionWorkLeaseInput(input EvolutionWorkLeaseInput) error {
	if err := validateEvolutionIdentity("worker_id", input.WorkerID); err != nil {
		return err
	}
	if len(input.Capabilities) == 0 || len(input.Capabilities) > 5 {
		return fmt.Errorf("capabilities must contain between 1 and 5 values")
	}
	seen := make(map[EvolutionWorkerCapability]struct{}, len(input.Capabilities))
	for _, capability := range input.Capabilities {
		if !isAllowedEvolutionWorkerCapability(capability) {
			return fmt.Errorf("%w: %q", ErrEvolutionCapabilityInvalid, capability)
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("duplicate evolution worker capability %q", capability)
		}
		seen[capability] = struct{}{}
	}
	return validateEvolutionLeaseDuration(input.LeaseDuration)
}

func validateEvolutionLeaseIdentity(workID, workerID, leaseID string, duration time.Duration) error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "work_id", value: workID},
		evolutionStringField{name: "worker_id", value: workerID},
		evolutionStringField{name: "lease_id", value: leaseID},
	); err != nil {
		return err
	}
	return validateEvolutionLeaseDuration(duration)
}

func validateEvolutionLeaseDuration(duration time.Duration) error {
	if duration < evolutionMinLeaseDuration || duration > evolutionMaxLeaseDuration {
		return fmt.Errorf("lease_duration must be between %s and %s", evolutionMinLeaseDuration, evolutionMaxLeaseDuration)
	}
	return nil
}

func validateEvolutionWorkCompletion(input EvolutionWorkCompletion) error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "work_id", value: input.WorkID},
		evolutionStringField{name: "worker_id", value: input.WorkerID},
		evolutionStringField{name: "lease_id", value: input.LeaseID},
	); err != nil {
		return err
	}
	if !isEvolutionOpaqueID(input.ResultIdempotencyKey) {
		return fmt.Errorf("result_idempotency_key must be a canonical UUID or sha256 identity")
	}
	if input.Attempt < 1 || input.Attempt > evolutionMaxAttempts {
		return fmt.Errorf("attempt must be between 1 and %d", evolutionMaxAttempts)
	}
	return validateEvolutionWorkerArtifactRef("result_artifact_ref", input.ResultArtifactRef)
}

func validateEvolutionWorkFailure(input EvolutionWorkFailure) error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "work_id", value: input.WorkID},
		evolutionStringField{name: "worker_id", value: input.WorkerID},
		evolutionStringField{name: "lease_id", value: input.LeaseID},
	); err != nil {
		return err
	}
	if input.Attempt < 1 || input.Attempt > evolutionMaxAttempts {
		return fmt.Errorf("attempt must be between 1 and %d", evolutionMaxAttempts)
	}
	if !isEvolutionOpaqueID(input.FailureIdempotencyKey) {
		return fmt.Errorf("failure_idempotency_key must be a canonical UUID or sha256 identity")
	}
	if err := validateEvolutionCode("failure_code", input.FailureCode); err != nil {
		return err
	}
	if err := validateEvolutionText("failure_message", input.FailureMessage, EvolutionFailureMessageMaxRunes); err != nil {
		return err
	}
	return validateEvolutionRetryDelay(input.RetryDelay)
}

func validateEvolutionOutboxDelivery(input EvolutionOutboxDelivery) error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "outbox_id", value: input.OutboxID},
		evolutionStringField{name: "worker_id", value: input.WorkerID},
		evolutionStringField{name: "lease_id", value: input.LeaseID},
	); err != nil {
		return err
	}
	if !isEvolutionOpaqueID(input.ReceiptID) {
		return fmt.Errorf("receipt_id must be a canonical UUID or sha256 identity")
	}
	if input.Attempt < 1 || input.Attempt > evolutionMaxAttempts {
		return fmt.Errorf("attempt must be between 1 and %d", evolutionMaxAttempts)
	}
	return nil
}

func validateEvolutionOutboxFailure(input EvolutionOutboxFailure) error {
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "outbox_id", value: input.OutboxID},
		evolutionStringField{name: "worker_id", value: input.WorkerID},
		evolutionStringField{name: "lease_id", value: input.LeaseID},
	); err != nil {
		return err
	}
	if input.Attempt < 1 || input.Attempt > evolutionMaxAttempts {
		return fmt.Errorf("attempt must be between 1 and %d", evolutionMaxAttempts)
	}
	if !isEvolutionOpaqueID(input.FailureIdempotencyKey) {
		return fmt.Errorf("failure_idempotency_key must be a canonical UUID or sha256 identity")
	}
	if err := validateEvolutionCode("failure_code", input.FailureCode); err != nil {
		return err
	}
	if err := validateEvolutionText("failure_message", input.FailureMessage, EvolutionFailureMessageMaxRunes); err != nil {
		return err
	}
	return validateEvolutionRetryDelay(input.RetryDelay)
}

func validateEvolutionRetryDelay(delay time.Duration) error {
	if delay < 0 || delay > evolutionMaxRetryDelay {
		return fmt.Errorf("retry_delay must be between 0 and %s", evolutionMaxRetryDelay)
	}
	return nil
}

func validateEvolutionWorkerArtifactRef(field, reference string) error {
	if err := validateEvolutionReference(field, reference); err != nil {
		return err
	}
	if filepath.IsAbs(reference) || strings.Contains(reference, "?") || strings.Contains(reference, "//") || strings.Contains(strings.ToLower(reference), "token") {
		return fmt.Errorf("%s must not contain an absolute path, URL query, or token", field)
	}
	kind, identity, found := strings.Cut(reference, ":")
	if !found || identity == "" {
		return fmt.Errorf("%s must be a structured reference", field)
	}
	switch kind {
	case "artifact", "candidate", "scorecard", "observation", "result":
		if !isEvolutionOpaqueID(identity) {
			return fmt.Errorf("%s requires an opaque identity", field)
		}
	case "release":
		if !isEvolutionKnowledgeReleaseID(identity) {
			return fmt.Errorf("%s requires a release identity", field)
		}
	default:
		return fmt.Errorf("%s has unsupported reference type", field)
	}
	return nil
}

func validateActiveEvolutionWorkLease(work *EvolutionWork, workerID, leaseID string, now time.Time) error {
	if work.Status != EvolutionWorkLeased || work.WorkerID != workerID || work.LeaseID != leaseID {
		return ErrEvolutionLeaseLost
	}
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return err
	}
	if work.leaseExpiresUnixNano <= nowNS {
		return ErrEvolutionLeaseExpired
	}
	return nil
}

func validateActiveEvolutionOutboxLease(message *EvolutionOutboxMessage, workerID, leaseID string, now time.Time) error {
	if message.Status != EvolutionOutboxLeased || message.WorkerID != workerID || message.LeaseID != leaseID {
		return ErrEvolutionLeaseLost
	}
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return err
	}
	if message.leaseExpiresUnixNano <= nowNS {
		return ErrEvolutionLeaseExpired
	}
	return nil
}

const evolutionWorkSelect = `
	SELECT work_id, run_id, capability, artifact_ref, status, attempt, max_attempts,
		available_at, available_at_unix_nano, lease_id, lease_owner, lease_expires_at,
		lease_expires_at_unix_nano, result_idempotency_key, result_artifact_ref,
		failure_code, failure_message, failure_idempotency_key, failure_hash,
		failure_worker_id, failure_lease_id, failure_attempt, created_at, updated_at, input_hash,
		result_worker_id, result_lease_id, result_attempt
	FROM evolution_work_items`

func scanEvolutionWork(scanner evolutionRowScanner) (*EvolutionWork, error) {
	var work EvolutionWork
	if err := scanner.Scan(
		&work.WorkID, &work.RunID, &work.Capability, &work.ArtifactRef, &work.Status,
		&work.Attempt, &work.MaxAttempts, &work.AvailableAt, &work.availableAtUnixNano,
		&work.LeaseID, &work.WorkerID, &work.LeaseExpiresAt, &work.leaseExpiresUnixNano,
		&work.ResultIdempotencyKey, &work.ResultArtifactRef, &work.FailureCode, &work.FailureMessage,
		&work.FailureIdempotencyKey, &work.failureHash, &work.failureWorkerID,
		&work.failureLeaseID, &work.failureAttempt, &work.CreatedAt, &work.UpdatedAt, &work.inputHash,
		&work.resultWorkerID, &work.resultLeaseID, &work.resultAttempt,
	); err != nil {
		return nil, err
	}
	if work.Status == EvolutionWorkCompleted {
		work.WorkerID = work.resultWorkerID
		work.LeaseID = work.resultLeaseID
		work.Attempt = work.resultAttempt
	} else if work.FailureIdempotencyKey != "" && work.Status != EvolutionWorkLeased {
		work.WorkerID = work.failureWorkerID
		work.LeaseID = work.failureLeaseID
		work.Attempt = work.failureAttempt
	}
	return &work, nil
}

func loadEvolutionWorkTx(tx *sql.Tx, workID string) (*EvolutionWork, error) {
	return scanEvolutionWork(tx.QueryRow(evolutionWorkSelect+` WHERE work_id = ?`, workID))
}

func loadEvolutionWorkByIdempotencyKeyTx(tx *sql.Tx, key string) (*EvolutionWork, error) {
	return scanEvolutionWork(tx.QueryRow(evolutionWorkSelect+` WHERE idempotency_key = ?`, key))
}

func normalizeEvolutionWorkLoadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrEvolutionWorkNotFound
	}
	return fmt.Errorf("load evolution work: %w", err)
}

const evolutionOutboxSelect = `
	SELECT outbox_id, run_id, topic, payload_ref, status, attempt, max_attempts,
		available_at, available_at_unix_nano, lease_id, lease_owner, lease_expires_at,
		lease_expires_at_unix_nano, receipt_id, failure_code, failure_message,
		failure_idempotency_key, failure_hash, failure_worker_id, failure_lease_id, failure_attempt,
		delivery_worker_id, delivery_lease_id, delivery_attempt, delivered_at, created_at, updated_at, input_hash
	FROM evolution_outbox`

func scanEvolutionOutbox(scanner evolutionRowScanner) (*EvolutionOutboxMessage, error) {
	var message EvolutionOutboxMessage
	if err := scanner.Scan(
		&message.OutboxID, &message.RunID, &message.Topic, &message.PayloadRef, &message.Status,
		&message.Attempt, &message.MaxAttempts, &message.AvailableAt, &message.availableAtUnixNano,
		&message.LeaseID, &message.WorkerID, &message.LeaseExpiresAt, &message.leaseExpiresUnixNano,
		&message.ReceiptID, &message.FailureCode, &message.FailureMessage,
		&message.FailureIdempotencyKey, &message.failureHash, &message.failureWorkerID,
		&message.failureLeaseID, &message.failureAttempt, &message.deliveryWorkerID,
		&message.deliveryLeaseID, &message.deliveryAttempt, &message.DeliveredAt,
		&message.CreatedAt, &message.UpdatedAt,
		&message.inputHash,
	); err != nil {
		return nil, err
	}
	if message.Status == EvolutionOutboxDelivered {
		message.WorkerID, message.LeaseID, message.Attempt = message.deliveryWorkerID, message.deliveryLeaseID, message.deliveryAttempt
	} else if message.FailureIdempotencyKey != "" && message.Status != EvolutionOutboxLeased {
		message.WorkerID, message.LeaseID, message.Attempt = message.failureWorkerID, message.failureLeaseID, message.failureAttempt
	}
	return &message, nil
}

func loadEvolutionOutboxTx(tx *sql.Tx, outboxID string) (*EvolutionOutboxMessage, error) {
	return scanEvolutionOutbox(tx.QueryRow(evolutionOutboxSelect+` WHERE outbox_id = ?`, outboxID))
}

func loadEvolutionOutboxByIdempotencyKeyTx(tx *sql.Tx, key string) (*EvolutionOutboxMessage, error) {
	return scanEvolutionOutbox(tx.QueryRow(evolutionOutboxSelect+` WHERE idempotency_key = ?`, key))
}

func normalizeEvolutionOutboxLoadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrEvolutionOutboxNotFound
	}
	return fmt.Errorf("load evolution outbox: %w", err)
}

func evolutionWorkerPayloadHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func evolutionWorkerFailureHash(input EvolutionWorkFailure) string {
	return evolutionCanonicalPayloadHash(input.WorkID, input.WorkerID, input.LeaseID,
		strconv.Itoa(input.Attempt), input.FailureIdempotencyKey, input.FailureCode,
		input.FailureMessage, strconv.FormatInt(int64(input.RetryDelay), 10))
}

func evolutionOutboxFailureHash(input EvolutionOutboxFailure) string {
	return evolutionCanonicalPayloadHash(input.OutboxID, input.WorkerID, input.LeaseID,
		strconv.Itoa(input.Attempt), input.FailureIdempotencyKey, input.FailureCode,
		input.FailureMessage, strconv.FormatInt(int64(input.RetryDelay), 10))
}

func evolutionCanonicalPayloadHash(parts ...string) string {
	var payload strings.Builder
	for _, part := range parts {
		payload.WriteString(strconv.Itoa(len(part)))
		payload.WriteByte(':')
		payload.WriteString(part)
	}
	return evolutionWorkerPayloadHash(payload.String())
}

func evolutionWorkerUnixNano(value time.Time) (int64, error) {
	value = value.UTC()
	unixNano := value.UnixNano()
	if !time.Unix(0, unixNano).UTC().Equal(value) {
		return 0, fmt.Errorf("timestamp %q is outside the safe nanosecond range", value.Format(time.RFC3339Nano))
	}
	return unixNano, nil
}

func evolutionWorkerAddDuration(now time.Time, duration time.Duration) (time.Time, int64, error) {
	result := now.Add(duration).UTC()
	if duration > 0 && !result.After(now) {
		return time.Time{}, 0, fmt.Errorf("duration overflows timestamp")
	}
	unixNano, err := evolutionWorkerUnixNano(result)
	if err != nil {
		return time.Time{}, 0, err
	}
	return result, unixNano, nil
}
