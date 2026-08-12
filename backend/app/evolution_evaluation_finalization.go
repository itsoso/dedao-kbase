package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// FinalizeEvolutionEvaluation atomically completes evaluation work and moves
// the run to its human-review or blocked state. It also reconciles the legacy
// crash window where work was completed before the run transition committed.
func (s *EvolutionControlStore) FinalizeEvolutionEvaluation(input EvolutionWorkCompletion, target EvolutionRunStatus, transition EvolutionTransitionInput) (*EvolutionWork, *EvolutionRun, bool, error) {
	if err := validateEvolutionWorkCompletion(input); err != nil {
		return nil, nil, false, err
	}
	if target != EvolutionAwaitingApproval && target != EvolutionBlocked {
		return nil, nil, false, fmt.Errorf("evaluation finalization target is invalid")
	}
	if err := validateEvolutionTransitionInput("run-placeholder", target, transition); err != nil {
		return nil, nil, false, err
	}
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	nowNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return nil, nil, false, err
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, nil, false, wrapEvolutionSQLiteWriteError("begin finalize evolution evaluation", err)
	}
	defer func() { _ = tx.Rollback() }()
	work, err := loadEvolutionWorkTx(tx, input.WorkID)
	if err != nil {
		return nil, nil, false, normalizeEvolutionWorkLoadError(err)
	}
	if work.Capability != EvolutionCapabilityEvaluation {
		return nil, nil, false, fmt.Errorf("evaluation finalization requires evaluation work")
	}
	replay := work.Status == EvolutionWorkCompleted
	if replay {
		if work.ResultIdempotencyKey != input.ResultIdempotencyKey || work.ResultArtifactRef != input.ResultArtifactRef ||
			work.resultWorkerID != input.WorkerID || work.resultLeaseID != input.LeaseID || work.resultAttempt != input.Attempt {
			return nil, nil, false, ErrEvolutionIdempotencyConflict
		}
	} else {
		if err := validateActiveEvolutionWorkLease(work, input.WorkerID, input.LeaseID, now); err != nil {
			return nil, nil, false, err
		}
		if work.Attempt != input.Attempt {
			return nil, nil, false, ErrEvolutionLeaseLost
		}
		var existingResultWorkID string
		if err := tx.QueryRow(`SELECT work_id FROM evolution_work_items WHERE result_idempotency_key = ?`, input.ResultIdempotencyKey).Scan(&existingResultWorkID); err == nil {
			return nil, nil, false, ErrEvolutionIdempotencyConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, false, err
		}
		resultHash := evolutionWorkerPayloadHash(input.ResultArtifactRef)
		result, err := tx.Exec(`UPDATE evolution_work_items SET status = 'completed', result_idempotency_key = ?,
			result_hash = ?, result_artifact_ref = ?, result_worker_id = ?, result_lease_id = ?, result_attempt = ?,
			lease_id = '', lease_owner = '', lease_expires_at = '', lease_expires_at_unix_nano = 0, updated_at = ?
			WHERE work_id = ? AND status = 'leased' AND lease_owner = ? AND lease_id = ? AND lease_expires_at_unix_nano > ?`,
			input.ResultIdempotencyKey, resultHash, input.ResultArtifactRef, input.WorkerID, input.LeaseID, input.Attempt,
			timestamp, input.WorkID, input.WorkerID, input.LeaseID, nowNS)
		if err != nil {
			return nil, nil, false, err
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			if err != nil {
				return nil, nil, false, err
			}
			return nil, nil, false, ErrEvolutionLeaseLost
		}
		resultEventID, err := newEvolutionStoreID("event")
		if err != nil {
			return nil, nil, false, err
		}
		resultEvent := EvolutionEvent{
			EventID: resultEventID, RunID: work.RunID, EventType: "worker_result", Actor: input.WorkerID,
			Code: "worker_result_ready", Message: "worker result is ready for control-plane processing",
			ArtifactRefs: []string{input.ResultArtifactRef}, CreatedAt: timestamp,
		}
		if err := s.insertEventTx(tx, resultEvent); err != nil {
			return nil, nil, false, err
		}
		outboxID, err := newEvolutionStoreID("outbox")
		if err != nil {
			return nil, nil, false, err
		}
		outboxKey := "sha256:" + evolutionWorkerPayloadHash(work.WorkID+":"+resultHash)
		_, outboxInputHash, err := normalizeEvolutionOutboxInput(EvolutionOutboxInput{
			IdempotencyKey: outboxKey, RunID: work.RunID, Topic: "evolution.work.completed",
			PayloadRef: input.ResultArtifactRef, AvailableAt: now, MaxAttempts: 3,
		}, now)
		if err != nil {
			return nil, nil, false, err
		}
		if hook := s.hooks.beforeOutboxInsert; hook != nil {
			if err := hook(); err != nil {
				return nil, nil, false, err
			}
		}
		if _, err := tx.Exec(`INSERT INTO evolution_outbox (
			outbox_id, idempotency_key, input_hash, run_id, topic, payload_ref, status, attempt,
			available_at, available_at_unix_nano, max_attempts, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'evolution.work.completed', ?, 'pending', 0, ?, ?, 3, ?, ?)`,
			outboxID, outboxKey, outboxInputHash, work.RunID, input.ResultArtifactRef, timestamp, nowNS, timestamp, timestamp); err != nil {
			return nil, nil, false, err
		}
		work.Status, work.ResultIdempotencyKey, work.ResultArtifactRef = EvolutionWorkCompleted, input.ResultIdempotencyKey, input.ResultArtifactRef
		work.resultWorkerID, work.resultLeaseID, work.resultAttempt = input.WorkerID, input.LeaseID, input.Attempt
	}

	run, err := scanEvolutionRun(tx.QueryRow(evolutionRunSelect+` WHERE run_id = ?`, work.RunID))
	if err != nil {
		return nil, nil, false, err
	}
	if run.Status == target {
		if err := tx.Commit(); err != nil {
			return nil, nil, false, err
		}
		return work, run, true, nil
	}
	if run.Status != EvolutionEvaluating {
		return nil, nil, false, ErrEvolutionTransitionConflict
	}
	transitionEventID, err := newEvolutionStoreID("event")
	if err != nil {
		return nil, nil, false, err
	}
	transitionEvent := EvolutionEvent{
		EventID: transitionEventID, RunID: run.RunID, EventType: "transition", Actor: transition.Actor,
		FromStatus: EvolutionEvaluating, ToStatus: target, Code: transition.Code, Message: transition.Message,
		ArtifactRefs: append([]string(nil), transition.ArtifactRefs...), CreatedAt: timestamp,
	}
	if err := transitionEvent.Validate(); err != nil {
		return nil, nil, false, err
	}
	result, err := tx.Exec(`UPDATE evolution_runs SET status = ?, updated_at = ?, updated_at_unix_nano = ? WHERE run_id = ? AND status = ?`, target, timestamp, nowNS, run.RunID, EvolutionEvaluating)
	if err != nil {
		return nil, nil, false, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, nil, false, err
		}
		return nil, nil, false, ErrEvolutionTransitionConflict
	}
	if err := s.insertEventTx(tx, transitionEvent); err != nil {
		return nil, nil, false, err
	}
	run.Status, run.UpdatedAt = target, timestamp
	if err := tx.Commit(); err != nil {
		return nil, nil, false, wrapEvolutionSQLiteWriteError("commit finalize evolution evaluation", err)
	}
	return work, run, replay, nil
}
