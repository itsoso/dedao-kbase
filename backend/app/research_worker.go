package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	ResearchWorkerToolSearchChatlog             = "search_chatlog"
	ResearchWorkerToolExpandChatContext         = "expand_chat_context"
	ResearchWorkerToolResolveChatIdentity       = "resolve_chat_identity"
	ResearchWorkerToolListIdentityConversations = "list_identity_conversations"
	ResearchWorkerToolFetchChatMessage          = "fetch_chat_message"

	ResearchWorkerJobQueued    = "queued"
	ResearchWorkerJobLeased    = "leased"
	ResearchWorkerJobCompleted = "completed"
	ResearchWorkerJobFailed    = "failed"
	ResearchWorkerJobExpired   = "expired"

	researchWorkerArgumentsMaxBytes = 64 << 10
	researchWorkerQueryLimitMax     = 500
	researchWorkerContextWindowMax  = 100
	researchWorkerAttemptsMax       = 5
	researchWorkerFailureCodeMax    = 128
	researchWorkerLeaseMax          = time.Hour
)

var (
	ErrResearchWorkerJobNotFound = errors.New("research worker job not found")
	ErrResearchWorkerLeaseOwner  = errors.New("research worker job belongs to another agent")
	ErrResearchWorkerStaleResult = errors.New("research worker result is stale")
	ErrResearchWorkerTerminal    = errors.New("research worker job is terminal")
)

type ResearchWorkerJobInput struct {
	RunID         string          `json:"run_id"`
	TargetAgentID string          `json:"target_agent_id"`
	Tool          string          `json:"tool"`
	Arguments     json.RawMessage `json:"arguments"`
	MaxAttempts   int             `json:"max_attempts"`
}

type ResearchWorkerJob struct {
	JobID             string          `json:"job_id"`
	RunID             string          `json:"run_id"`
	TargetAgentID     string          `json:"target_agent_id"`
	Tool              string          `json:"tool"`
	Arguments         json.RawMessage `json:"arguments"`
	State             string          `json:"state"`
	Attempt           int             `json:"attempt"`
	MaxAttempts       int             `json:"max_attempts"`
	LeaseOwner        string          `json:"lease_owner,omitempty"`
	LeaseExpiresAt    string          `json:"lease_expires_at,omitempty"`
	RequestHash       string          `json:"request_hash"`
	ResultFingerprint string          `json:"result_fingerprint,omitempty"`
	FailureCode       string          `json:"failure_code,omitempty"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
	CompletedAt       string          `json:"completed_at,omitempty"`
}

func (s *ResearchStore) CreateWorkerJob(input ResearchWorkerJobInput) (*ResearchWorkerJob, bool, error) {
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		return nil, false, fmt.Errorf("run_id is required")
	}
	targetAgentID, err := normalizeSourceAgentName("target_agent_id", input.TargetAgentID, sourceAgentIDMaxRunes, false)
	if err != nil {
		return nil, false, err
	}
	tool := strings.TrimSpace(input.Tool)
	arguments, err := normalizeResearchWorkerArguments(tool, input.Arguments)
	if err != nil {
		return nil, false, err
	}
	if input.MaxAttempts <= 0 || input.MaxAttempts > researchWorkerAttemptsMax {
		return nil, false, fmt.Errorf("max_attempts must be between 1 and %d", researchWorkerAttemptsMax)
	}
	requestIdentity, err := json.Marshal(struct {
		RunID         string          `json:"run_id"`
		TargetAgentID string          `json:"target_agent_id"`
		Tool          string          `json:"tool"`
		Arguments     json.RawMessage `json:"arguments"`
	}{runID, targetAgentID, tool, arguments})
	if err != nil {
		return nil, false, err
	}
	requestHash := researchEvidenceHash(requestIdentity)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := loadResearchRun(tx, runID)
	if err != nil {
		return nil, false, err
	}
	if isTerminalResearchStatus(run.Status) {
		return nil, false, ErrResearchWorkerTerminal
	}
	var existingID string
	err = tx.QueryRow(`SELECT job_id FROM research_worker_jobs WHERE run_id = ? AND request_hash = ?`, runID, requestHash).Scan(&existingID)
	if err == nil {
		job, err := loadResearchWorkerJob(tx, existingID)
		return job, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	job := &ResearchWorkerJob{
		JobID: newResearchWorkerJobID(), RunID: runID, TargetAgentID: targetAgentID, Tool: tool,
		Arguments: arguments, State: ResearchWorkerJobQueued, MaxAttempts: input.MaxAttempts,
		RequestHash: requestHash, CreatedAt: now, UpdatedAt: now,
	}
	_, err = tx.Exec(`INSERT INTO research_worker_jobs (
		job_id, run_id, target_agent_id, tool, arguments_json, state, attempt, max_attempts,
		lease_owner, lease_expires_at, request_hash, result_fingerprint, failure_code,
		created_at, updated_at, completed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, '', '', ?, ?, '')`,
		job.JobID, job.RunID, job.TargetAgentID, job.Tool, string(job.Arguments), job.State, job.Attempt,
		job.MaxAttempts, job.RequestHash, job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return nil, false, err
	}
	if err := insertResearchEvent(tx, run.RunID, run.Status, run.Status,
		ResearchTransition{Code: "worker_job_queued", Actor: "orchestrator"}, now); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return job, true, nil
}

func (s *ResearchStore) LoadWorkerJob(jobID string) (*ResearchWorkerJob, error) {
	return loadResearchWorkerJob(s.db, strings.TrimSpace(jobID))
}

func (s *ResearchStore) ClaimWorkerJob(agentID string, lease time.Duration) (*ResearchWorkerJob, error) {
	agentID, err := normalizeSourceAgentName("agent_id", agentID, sourceAgentIDMaxRunes, false)
	if err != nil {
		return nil, err
	}
	if lease <= 0 || lease > researchWorkerLeaseMax {
		return nil, fmt.Errorf("lease duration must be between 1 second and %s", researchWorkerLeaseMax)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var jobID string
	err = tx.QueryRow(`SELECT job_id FROM research_worker_jobs
		WHERE target_agent_id = ? AND state = ? ORDER BY created_at ASC, job_id ASC LIMIT 1`,
		agentID, ResearchWorkerJobQueued).Scan(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job, err := loadResearchWorkerJob(tx, jobID)
	if err != nil {
		return nil, err
	}
	nowTime := s.now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	expires := nowTime.Add(lease).Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE research_worker_jobs SET state = ?, attempt = attempt + 1,
		lease_owner = ?, lease_expires_at = ?, updated_at = ?, failure_code = ''
		WHERE job_id = ? AND state = ?`, ResearchWorkerJobLeased, agentID, expires, now, job.JobID, ResearchWorkerJobQueued)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrResearchWorkerStaleResult
	}
	run, err := loadResearchRun(tx, job.RunID)
	if err != nil {
		return nil, err
	}
	if err := insertResearchEvent(tx, run.RunID, run.Status, run.Status,
		ResearchTransition{Code: "worker_job_leased", Actor: agentID}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	job.State = ResearchWorkerJobLeased
	job.Attempt++
	job.LeaseOwner = agentID
	job.LeaseExpiresAt = expires
	job.UpdatedAt = now
	return job, nil
}

func (s *ResearchStore) RenewWorkerJobLease(jobID, agentID string, lease time.Duration) error {
	agentID, err := normalizeSourceAgentName("agent_id", agentID, sourceAgentIDMaxRunes, false)
	if err != nil {
		return err
	}
	if lease <= 0 || lease > researchWorkerLeaseMax {
		return fmt.Errorf("lease duration must be between 1 second and %s", researchWorkerLeaseMax)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	job, err := loadResearchWorkerJob(tx, strings.TrimSpace(jobID))
	if err != nil {
		return err
	}
	if job.TargetAgentID != agentID || job.LeaseOwner != agentID {
		return ErrResearchWorkerLeaseOwner
	}
	if job.State != ResearchWorkerJobLeased {
		return ErrResearchWorkerTerminal
	}
	nowTime := s.now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	expires := nowTime.Add(lease).Format(time.RFC3339Nano)
	if _, err := tx.Exec(`UPDATE research_worker_jobs SET lease_expires_at = ?, updated_at = ? WHERE job_id = ?`, expires, now, job.JobID); err != nil {
		return err
	}
	run, err := loadResearchRun(tx, job.RunID)
	if err != nil {
		return err
	}
	if err := insertResearchEvent(tx, run.RunID, run.Status, run.Status,
		ResearchTransition{Code: "worker_job_lease_renewed", Actor: agentID}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ResearchStore) CompleteWorkerJob(jobID, agentID, requestHash string, result ResearchWorkerResult) (*ResearchWorkerJob, error) {
	bundle, err := NormalizeResearchWorkerResult(result)
	if err != nil {
		return nil, err
	}
	encodedBundle, err := json.Marshal(bundle)
	if err != nil {
		return nil, err
	}
	resultFingerprint := researchEvidenceHash(encodedBundle)
	agentID = strings.TrimSpace(agentID)
	requestHash = strings.TrimSpace(requestHash)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	job, err := loadResearchWorkerJob(tx, strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	if job.TargetAgentID != agentID || job.LeaseOwner != agentID {
		return nil, ErrResearchWorkerLeaseOwner
	}
	if requestHash == "" || requestHash != job.RequestHash {
		return nil, ErrResearchWorkerStaleResult
	}
	if job.State == ResearchWorkerJobCompleted {
		if job.ResultFingerprint == resultFingerprint {
			return job, nil
		}
		return nil, ErrResearchWorkerStaleResult
	}
	if job.State != ResearchWorkerJobLeased || researchWorkerLeaseExpired(job.LeaseExpiresAt, s.now()) {
		return nil, ErrResearchWorkerTerminal
	}
	run, err := loadResearchRun(tx, job.RunID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	if err := storeResearchEvidenceBundleTx(tx, run, bundle, now); err != nil {
		return nil, err
	}
	update, err := tx.Exec(`UPDATE research_worker_jobs SET state = ?, result_fingerprint = ?,
		failure_code = '', updated_at = ?, completed_at = ? WHERE job_id = ? AND state = ?`,
		ResearchWorkerJobCompleted, resultFingerprint, now, now, job.JobID, ResearchWorkerJobLeased)
	if err != nil {
		return nil, err
	}
	changed, err := update.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrResearchWorkerStaleResult
	}
	if err := insertResearchEvent(tx, run.RunID, run.Status, run.Status,
		ResearchTransition{Code: "worker_job_completed", Actor: agentID}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	job.State = ResearchWorkerJobCompleted
	job.ResultFingerprint = resultFingerprint
	job.FailureCode = ""
	job.UpdatedAt = now
	job.CompletedAt = now
	return job, nil
}

func (s *ResearchStore) FailWorkerJob(jobID, agentID, requestHash, code string, retryable bool) (*ResearchWorkerJob, error) {
	agentID = strings.TrimSpace(agentID)
	requestHash = strings.TrimSpace(requestHash)
	code = strings.TrimSpace(code)
	if code == "" || len([]rune(code)) > researchWorkerFailureCodeMax {
		return nil, fmt.Errorf("failure code is required and must not exceed %d characters", researchWorkerFailureCodeMax)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	job, err := loadResearchWorkerJob(tx, strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	if job.TargetAgentID != agentID || job.LeaseOwner != agentID {
		return nil, ErrResearchWorkerLeaseOwner
	}
	if requestHash == "" || requestHash != job.RequestHash {
		return nil, ErrResearchWorkerStaleResult
	}
	if job.State == ResearchWorkerJobFailed && job.FailureCode == code {
		return job, nil
	}
	if job.State != ResearchWorkerJobLeased {
		return nil, ErrResearchWorkerTerminal
	}
	state := ResearchWorkerJobFailed
	completedAt := s.now().UTC().Format(time.RFC3339Nano)
	if retryable && job.Attempt < job.MaxAttempts {
		state = ResearchWorkerJobQueued
		completedAt = ""
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	_, err = tx.Exec(`UPDATE research_worker_jobs SET state = ?, lease_owner = '', lease_expires_at = '',
		failure_code = ?, updated_at = ?, completed_at = ? WHERE job_id = ?`, state, code, now, completedAt, job.JobID)
	if err != nil {
		return nil, err
	}
	run, err := loadResearchRun(tx, job.RunID)
	if err != nil {
		return nil, err
	}
	if err := insertResearchEvent(tx, run.RunID, run.Status, run.Status,
		ResearchTransition{Code: "worker_job_" + state, Actor: agentID}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	job.State = state
	job.LeaseOwner = ""
	job.LeaseExpiresAt = ""
	job.FailureCode = code
	job.UpdatedAt = now
	job.CompletedAt = completedAt
	return job, nil
}

func (s *ResearchStore) RecoverExpiredWorkerJobs() (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(`SELECT job_id FROM research_worker_jobs WHERE state = ? ORDER BY created_at, job_id`, ResearchWorkerJobLeased)
	if err != nil {
		return 0, err
	}
	var jobIDs []string
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		jobIDs = append(jobIDs, jobID)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	nowTime := s.now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	recovered := 0
	for _, jobID := range jobIDs {
		job, err := loadResearchWorkerJob(tx, jobID)
		if err != nil {
			return 0, err
		}
		if !researchWorkerLeaseExpired(job.LeaseExpiresAt, nowTime) {
			continue
		}
		state, completedAt := ResearchWorkerJobQueued, ""
		if job.Attempt >= job.MaxAttempts {
			state, completedAt = ResearchWorkerJobExpired, now
		}
		if _, err := tx.Exec(`UPDATE research_worker_jobs SET state = ?, lease_owner = '', lease_expires_at = '',
			failure_code = ?, updated_at = ?, completed_at = ? WHERE job_id = ?`,
			state, "lease_expired", now, completedAt, job.JobID); err != nil {
			return 0, err
		}
		run, err := loadResearchRun(tx, job.RunID)
		if err != nil {
			return 0, err
		}
		if err := insertResearchEvent(tx, run.RunID, run.Status, run.Status,
			ResearchTransition{Code: "worker_job_" + state, Actor: "orchestrator"}, now); err != nil {
			return 0, err
		}
		recovered++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return recovered, nil
}

func normalizeResearchWorkerArguments(tool string, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > researchWorkerArgumentsMaxBytes {
		return nil, fmt.Errorf("worker arguments are required and must not exceed %d bytes", researchWorkerArgumentsMaxBytes)
	}
	var target any
	switch tool {
	case ResearchWorkerToolSearchChatlog:
		target = &struct {
			TimeFrom  string `json:"time_from,omitempty"`
			TimeTo    string `json:"time_to,omitempty"`
			TalkerRef string `json:"talker_ref,omitempty"`
			SenderRef string `json:"sender_ref,omitempty"`
			Keyword   string `json:"keyword,omitempty"`
			Limit     int    `json:"limit"`
			Offset    int    `json:"offset,omitempty"`
		}{}
	case ResearchWorkerToolExpandChatContext:
		target = &struct {
			MessageRef string `json:"message_ref"`
			Before     int    `json:"before"`
			After      int    `json:"after"`
		}{}
	case ResearchWorkerToolResolveChatIdentity:
		target = &struct {
			IdentityRef     string `json:"identity_ref"`
			ConversationRef string `json:"conversation_ref,omitempty"`
		}{}
	case ResearchWorkerToolListIdentityConversations:
		target = &struct {
			IdentityRef string `json:"identity_ref"`
			Limit       int    `json:"limit"`
			Offset      int    `json:"offset,omitempty"`
		}{}
	case ResearchWorkerToolFetchChatMessage:
		target = &struct {
			MessageRef string `json:"message_ref"`
		}{}
	default:
		return nil, fmt.Errorf("unsupported research worker tool")
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(raw), researchWorkerArgumentsMaxBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, fmt.Errorf("invalid research worker arguments")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid research worker arguments")
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}
	if err := validateResearchWorkerArguments(tool, encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func validateResearchWorkerArguments(tool string, encoded json.RawMessage) error {
	switch tool {
	case ResearchWorkerToolSearchChatlog:
		var args struct {
			TimeFrom  string `json:"time_from"`
			TimeTo    string `json:"time_to"`
			TalkerRef string `json:"talker_ref"`
			SenderRef string `json:"sender_ref"`
			Keyword   string `json:"keyword"`
			Limit     int    `json:"limit"`
			Offset    int    `json:"offset"`
		}
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if args.Limit <= 0 || args.Limit > researchWorkerQueryLimitMax || args.Offset < 0 {
			return fmt.Errorf("search limit must be between 1 and %d and offset must be non-negative", researchWorkerQueryLimitMax)
		}
		if err := validateResearchWorkerTimeRange(args.TimeFrom, args.TimeTo); err != nil {
			return err
		}
	case ResearchWorkerToolExpandChatContext:
		var args struct {
			MessageRef string `json:"message_ref"`
			Before     int    `json:"before"`
			After      int    `json:"after"`
		}
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if strings.TrimSpace(args.MessageRef) == "" || args.Before < 0 || args.After < 0 ||
			args.Before > researchWorkerContextWindowMax || args.After > researchWorkerContextWindowMax {
			return fmt.Errorf("context arguments are outside supported bounds")
		}
	case ResearchWorkerToolResolveChatIdentity:
		var args struct {
			IdentityRef string `json:"identity_ref"`
		}
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if strings.TrimSpace(args.IdentityRef) == "" {
			return fmt.Errorf("identity_ref is required")
		}
	case ResearchWorkerToolListIdentityConversations:
		var args struct {
			IdentityRef string `json:"identity_ref"`
			Limit       int    `json:"limit"`
			Offset      int    `json:"offset"`
		}
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if strings.TrimSpace(args.IdentityRef) == "" || args.Limit <= 0 || args.Limit > researchWorkerQueryLimitMax || args.Offset < 0 {
			return fmt.Errorf("identity conversation query is outside supported bounds")
		}
	case ResearchWorkerToolFetchChatMessage:
		var args struct {
			MessageRef string `json:"message_ref"`
		}
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if strings.TrimSpace(args.MessageRef) == "" {
			return fmt.Errorf("message_ref is required")
		}
	}
	return nil
}

func validateResearchWorkerTimeRange(fromValue, toValue string) error {
	var from, to time.Time
	var err error
	if strings.TrimSpace(fromValue) != "" {
		from, err = time.Parse(time.RFC3339, fromValue)
		if err != nil {
			return fmt.Errorf("time_from must be RFC3339")
		}
	}
	if strings.TrimSpace(toValue) != "" {
		to, err = time.Parse(time.RFC3339, toValue)
		if err != nil {
			return fmt.Errorf("time_to must be RFC3339")
		}
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return fmt.Errorf("time_to must not be before time_from")
	}
	return nil
}

type researchWorkerJobQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func loadResearchWorkerJob(queryer researchWorkerJobQuerier, jobID string) (*ResearchWorkerJob, error) {
	var job ResearchWorkerJob
	var arguments string
	err := queryer.QueryRow(`SELECT job_id, run_id, target_agent_id, tool, arguments_json, state,
		attempt, max_attempts, lease_owner, lease_expires_at, request_hash, result_fingerprint,
		failure_code, created_at, updated_at, completed_at FROM research_worker_jobs WHERE job_id = ?`, jobID).Scan(
		&job.JobID, &job.RunID, &job.TargetAgentID, &job.Tool, &arguments, &job.State,
		&job.Attempt, &job.MaxAttempts, &job.LeaseOwner, &job.LeaseExpiresAt, &job.RequestHash,
		&job.ResultFingerprint, &job.FailureCode, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrResearchWorkerJobNotFound
	}
	if err != nil {
		return nil, err
	}
	job.Arguments = json.RawMessage(arguments)
	return &job, nil
}

func researchWorkerLeaseExpired(value string, now time.Time) bool {
	expires, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return err != nil || !expires.After(now.UTC())
}

func newResearchWorkerJobID() string {
	return strings.Replace(newResearchRunID(), "research-run-", "research-job-", 1)
}
