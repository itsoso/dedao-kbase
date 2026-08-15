package app

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
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
	LeaseID           string          `json:"lease_id,omitempty"`
	LeaseExpiresAt    string          `json:"lease_expires_at,omitempty"`
	RequestHash       string          `json:"request_hash"`
	ResultFingerprint string          `json:"result_fingerprint,omitempty"`
	FailureCode       string          `json:"failure_code,omitempty"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
	CompletedAt       string          `json:"completed_at,omitempty"`
}

type ResearchWorkerSearchChatlogArgs struct {
	TimeFrom  string `json:"time_from,omitempty"`
	TimeTo    string `json:"time_to,omitempty"`
	TalkerRef string `json:"talker_ref,omitempty"`
	SenderRef string `json:"sender_ref,omitempty"`
	Keyword   string `json:"keyword,omitempty"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset,omitempty"`
}

type ResearchWorkerExpandChatContextArgs struct {
	MessageRef      string `json:"message_ref"`
	ConversationRef string `json:"conversation_ref"`
	Time            string `json:"time"`
	Before          int    `json:"before"`
	After           int    `json:"after"`
}

type ResearchWorkerResolveChatIdentityArgs struct {
	IdentityRef     string `json:"identity_ref"`
	ConversationRef string `json:"conversation_ref,omitempty"`
}

type ResearchWorkerListIdentityConversationsArgs struct {
	IdentityRef string `json:"identity_ref"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset,omitempty"`
}

type ResearchWorkerFetchChatMessageArgs struct {
	MessageRef      string `json:"message_ref"`
	ConversationRef string `json:"conversation_ref"`
	Time            string `json:"time"`
}

type ResearchWorkerCandidate struct {
	RunID        string `json:"run_id"`
	JobID        string `json:"job_id"`
	CandidateRef string `json:"candidate_ref"`
	SourceType   string `json:"source_type"`
	SourceRole   string `json:"source_role"`
	OccurredAt   string `json:"occurred_at,omitempty"`
	CreatedAt    string `json:"created_at"`
}

func (s *ResearchStore) CreateWorkerJob(input ResearchWorkerJobInput) (*ResearchWorkerJob, bool, error) {
	return s.createWorkerJob(input, "", "", false)
}

func (s *ResearchStore) CreateWorkerJobWithLease(input ResearchWorkerJobInput, owner, epoch string) (*ResearchWorkerJob, bool, error) {
	return s.createWorkerJob(input, owner, epoch, true)
}

func (s *ResearchStore) createWorkerJob(input ResearchWorkerJobInput, owner, epoch string, guarded bool) (*ResearchWorkerJob, bool, error) {
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		return nil, false, fmt.Errorf("%w: run_id is required", ErrResearchInvalidToolRequest)
	}
	targetAgentID, err := normalizeSourceAgentName("target_agent_id", input.TargetAgentID, sourceAgentIDMaxRunes, false)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrResearchInvalidToolRequest, err)
	}
	tool := strings.TrimSpace(input.Tool)
	arguments, err := normalizeResearchWorkerArguments(tool, input.Arguments)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrResearchInvalidToolRequest, err)
	}
	if input.MaxAttempts <= 0 || input.MaxAttempts > researchWorkerAttemptsMax {
		return nil, false, fmt.Errorf("%w: max_attempts must be between 1 and %d", ErrResearchInvalidToolRequest, researchWorkerAttemptsMax)
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
	if guarded {
		if err := assertResearchRunLeaseTx(tx, run.RunID, owner, epoch, s.now()); err != nil {
			return nil, false, err
		}
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
		lease_owner, lease_id, lease_expires_at, request_hash, result_fingerprint, failure_code,
		created_at, updated_at, completed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', '', '', ?, '', '', ?, ?, '')`,
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
	leaseID := newResearchWorkerLeaseID()
	result, err := tx.Exec(`UPDATE research_worker_jobs SET state = ?, attempt = attempt + 1,
		lease_owner = ?, lease_id = ?, lease_expires_at = ?, updated_at = ?, failure_code = ''
		WHERE job_id = ? AND state = ?`, ResearchWorkerJobLeased, agentID, leaseID, expires, now, job.JobID, ResearchWorkerJobQueued)
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
	job.LeaseID = leaseID
	job.LeaseExpiresAt = expires
	job.UpdatedAt = now
	return job, nil
}

func (s *ResearchStore) RenewWorkerJobLease(jobID, agentID, leaseID string, lease time.Duration) error {
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
	if strings.TrimSpace(leaseID) == "" || job.LeaseID != strings.TrimSpace(leaseID) {
		return ErrResearchWorkerStaleResult
	}
	if job.State != ResearchWorkerJobLeased {
		return ErrResearchWorkerTerminal
	}
	nowTime := s.now().UTC()
	if researchWorkerLeaseExpired(job.LeaseExpiresAt, nowTime) {
		return ErrResearchWorkerTerminal
	}
	now := nowTime.Format(time.RFC3339Nano)
	expires := nowTime.Add(lease).Format(time.RFC3339Nano)
	updated, err := tx.Exec(`UPDATE research_worker_jobs SET lease_expires_at = ?, updated_at = ?
		WHERE job_id = ? AND state = ? AND lease_owner = ? AND lease_id = ?`,
		expires, now, job.JobID, ResearchWorkerJobLeased, agentID, leaseID)
	if err != nil {
		return err
	}
	if changed, err := updated.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return ErrResearchWorkerStaleResult
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

func (s *ResearchStore) CompleteWorkerJob(jobID, agentID, leaseID, requestHash string, result ResearchWorkerResult) (*ResearchWorkerJob, error) {
	bundle, err := NormalizeResearchWorkerResult(result)
	if err != nil {
		return nil, err
	}
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
	if strings.TrimSpace(leaseID) == "" || job.LeaseID != strings.TrimSpace(leaseID) {
		return nil, ErrResearchWorkerStaleResult
	}
	if requestHash == "" || requestHash != job.RequestHash {
		return nil, ErrResearchWorkerStaleResult
	}
	if job.State != ResearchWorkerJobLeased && job.State != ResearchWorkerJobCompleted {
		return nil, ErrResearchWorkerTerminal
	}
	if job.State == ResearchWorkerJobLeased && researchWorkerLeaseExpired(job.LeaseExpiresAt, s.now()) {
		return nil, ErrResearchWorkerTerminal
	}
	candidates, err := normalizeResearchWorkerCandidates(job, result)
	if err != nil {
		return nil, err
	}
	identityCandidates, err := normalizeResearchWorkerIdentityCandidates(job, result.IdentityCandidates)
	if err != nil {
		return nil, err
	}
	if err := validateResearchWorkerSelectedBoundaryTx(tx, job, result); err != nil {
		return nil, err
	}
	encodedProjection, err := json.Marshal(struct {
		Bundle             ResearchEvidenceBundle      `json:"bundle"`
		Candidates         []ResearchWorkerCandidate   `json:"candidates"`
		IdentityCandidates []ResearchIdentityCandidate `json:"identity_candidates"`
	}{Bundle: bundle, Candidates: candidates, IdentityCandidates: identityCandidates})
	if err != nil {
		return nil, err
	}
	resultFingerprint := researchEvidenceHash(encodedProjection)
	if job.TargetAgentID != agentID || job.LeaseOwner != agentID {
		return nil, ErrResearchWorkerLeaseOwner
	}
	if strings.TrimSpace(leaseID) == "" || job.LeaseID != strings.TrimSpace(leaseID) {
		return nil, ErrResearchWorkerStaleResult
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
	for _, candidate := range candidates {
		if _, err := tx.Exec(`INSERT INTO research_worker_candidates
			(run_id, job_id, candidate_ref, source_type, source_role, occurred_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(run_id, candidate_ref) DO UPDATE SET job_id = excluded.job_id,
				source_type = excluded.source_type, source_role = excluded.source_role,
				occurred_at = excluded.occurred_at`, run.RunID, job.JobID, candidate.CandidateRef,
			candidate.SourceType, candidate.SourceRole, candidate.OccurredAt, now); err != nil {
			return nil, err
		}
	}
	for _, candidate := range identityCandidates {
		bindingID := "research-binding-" + researchAnalysisID(run.RunID, candidate.IdentityID)
		confidence := researchIdentityCandidateConfidence(candidate)
		if _, err := tx.Exec(`INSERT INTO research_identity_bindings
			(binding_id, run_id, identity_id, source_type, source_identity_hash, confidence, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(binding_id) DO UPDATE SET confidence = excluded.confidence`, bindingID, run.RunID,
			candidate.IdentityID, ResearchSourceChatlog, researchEvidenceHash([]byte(candidate.IdentityID)), confidence, now); err != nil {
			return nil, err
		}
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

func validateResearchWorkerSelectedBoundaryTx(tx *sql.Tx, job *ResearchWorkerJob, result ResearchWorkerResult) error {
	if job == nil {
		return ErrResearchWorkerJobNotFound
	}
	selected := make([]ResearchWorkerEvidenceCandidate, 0, len(result.Items))
	for _, item := range result.Items {
		if item.Selected {
			selected = append(selected, item)
		}
	}
	if job.Tool != ResearchWorkerToolFetchChatMessage && job.Tool != ResearchWorkerToolExpandChatContext {
		if len(selected) != 0 || strings.TrimSpace(result.AnchorCandidateRef) != "" {
			return fmt.Errorf("selected evidence is outside a candidate-bound fetch")
		}
		return nil
	}
	var anchorRef string
	maxSelected := 1
	switch job.Tool {
	case ResearchWorkerToolFetchChatMessage:
		var args ResearchWorkerFetchChatMessageArgs
		if err := json.Unmarshal(job.Arguments, &args); err != nil {
			return err
		}
		anchorRef = strings.TrimSpace(args.MessageRef)
		if strings.TrimSpace(args.ConversationRef) != anchorRef || len(selected) != 1 {
			return fmt.Errorf("fetch result does not match the requested candidate")
		}
	case ResearchWorkerToolExpandChatContext:
		var args ResearchWorkerExpandChatContextArgs
		if err := json.Unmarshal(job.Arguments, &args); err != nil {
			return err
		}
		anchorRef = strings.TrimSpace(args.MessageRef)
		maxSelected = args.Before + args.After + 1
		if strings.TrimSpace(args.ConversationRef) != anchorRef || strings.TrimSpace(result.AnchorCandidateRef) != anchorRef || len(selected) == 0 || len(selected) > maxSelected {
			return fmt.Errorf("expanded context is outside the server-issued candidate window")
		}
	}
	if !validResearchOpaqueCandidateRef(anchorRef) {
		return fmt.Errorf("candidate reference is not opaque")
	}
	var candidateCount int
	err := tx.QueryRow(`SELECT COUNT(*) FROM research_worker_candidates c
		JOIN research_worker_jobs source_job ON source_job.job_id = c.job_id
		WHERE c.run_id = ? AND c.candidate_ref = ? AND c.source_type = ?
			AND source_job.target_agent_id = ? AND source_job.tool = ?`,
		job.RunID, anchorRef, ResearchEvidenceSourceChatlog, job.TargetAgentID, ResearchWorkerToolSearchChatlog).Scan(&candidateCount)
	if err != nil {
		return err
	}
	if candidateCount != 1 {
		return fmt.Errorf("candidate does not belong to this run and worker")
	}
	anchorSeen := false
	for _, item := range selected {
		messageRef := strings.TrimSpace(item.Locator.MessageRef)
		if item.SourceType != ResearchEvidenceSourceChatlog || item.Privacy != ResearchEvidencePrivacyPrivate ||
			item.Locator.WorkerID != job.TargetAgentID || !validResearchOpaqueCandidateRef(messageRef) ||
			strings.TrimSpace(item.Locator.ConversationRef) != messageRef {
			return fmt.Errorf("selected evidence crosses the candidate boundary")
		}
		if messageRef == anchorRef {
			anchorSeen = true
		}
		if job.Tool == ResearchWorkerToolFetchChatMessage && messageRef != anchorRef {
			return fmt.Errorf("fetch result does not match the requested candidate")
		}
	}
	if !anchorSeen {
		return fmt.Errorf("selected evidence omits the requested candidate")
	}
	return nil
}

func normalizeResearchWorkerIdentityCandidates(job *ResearchWorkerJob, candidates []ResearchIdentityCandidate) ([]ResearchIdentityCandidate, error) {
	if len(candidates) == 0 {
		return []ResearchIdentityCandidate{}, nil
	}
	if job == nil || job.Tool != ResearchWorkerToolResolveChatIdentity || len(candidates) > researchModelArrayMax {
		return nil, fmt.Errorf("identity candidates are outside the resolve-identity boundary")
	}
	result := make([]ResearchIdentityCandidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		identityID := strings.TrimSpace(candidate.IdentityID)
		if !strings.HasPrefix(identityID, "chat-identity-") || len(identityID) != len("chat-identity-")+32 ||
			strings.TrimSpace(candidate.DisplayName) != "" || len(candidate.Aliases) != 0 ||
			(candidate.AccountID != "" && candidate.AccountID != identityID) ||
			(candidate.TargetAccountID != "" && candidate.TargetAccountID != identityID) {
			return nil, fmt.Errorf("identity candidate crosses the private identity boundary")
		}
		if _, err := hex.DecodeString(strings.TrimPrefix(identityID, "chat-identity-")); err != nil {
			return nil, fmt.Errorf("identity candidate is not opaque")
		}
		if seen[identityID] {
			continue
		}
		seen[identityID] = true
		candidate.IdentityID = identityID
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].IdentityID < result[j].IdentityID })
	return result, nil
}

func researchIdentityCandidateConfidence(candidate ResearchIdentityCandidate) float64 {
	if candidate.ConfirmedBinding || candidate.SelfIdentification ||
		(candidate.AccountID != "" && candidate.AccountID == candidate.TargetAccountID) {
		return 1
	}
	confidence := 0.4
	for _, matched := range []bool{candidate.DisplayNameMatch, candidate.ContactMetadataMatch, candidate.GroupMembershipMatch, candidate.ConversationContinuity} {
		if matched {
			confidence += 0.1
		}
	}
	if confidence > 0.9 {
		confidence = 0.9
	}
	return confidence
}

func normalizeResearchWorkerCandidates(job *ResearchWorkerJob, result ResearchWorkerResult) ([]ResearchWorkerCandidate, error) {
	if job == nil {
		return nil, ErrResearchWorkerJobNotFound
	}
	candidates := []ResearchWorkerCandidate{}
	for _, item := range result.Items {
		if item.Selected {
			if job.Tool == ResearchWorkerToolSearchChatlog {
				return nil, fmt.Errorf("chatlog search cannot promote evidence directly")
			}
			continue
		}
		if job.Tool != ResearchWorkerToolSearchChatlog {
			return nil, fmt.Errorf("only chatlog search can return discovery candidates")
		}
		candidateRef := strings.TrimSpace(item.Locator.MessageRef)
		if item.SourceType != ResearchEvidenceSourceChatlog || item.Privacy != ResearchEvidencePrivacyPrivate ||
			strings.TrimSpace(item.SourceRole) == "" || strings.TrimSpace(item.Content) != "" ||
			strings.TrimSpace(item.AuthorIdentityID) != "" || len(item.SubjectIdentityIDs) != 0 ||
			!validResearchOpaqueCandidateRef(candidateRef) || strings.TrimSpace(item.Locator.ConversationRef) != candidateRef ||
			strings.TrimSpace(item.Locator.WorkerID) != job.TargetAgentID || item.Locator.ReleaseID != "" || item.Locator.PriorRunID != "" {
			return nil, fmt.Errorf("chatlog search candidate crosses the private locator boundary")
		}
		occurredAt := strings.TrimSpace(item.OccurredAt)
		if occurredAt != "" {
			if _, err := time.Parse(time.RFC3339, occurredAt); err != nil {
				return nil, fmt.Errorf("chatlog search candidate occurred_at is invalid")
			}
		}
		candidates = append(candidates, ResearchWorkerCandidate{
			RunID: job.RunID, JobID: job.JobID, CandidateRef: candidateRef,
			SourceType: item.SourceType, SourceRole: item.SourceRole, OccurredAt: occurredAt,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].OccurredAt == candidates[j].OccurredAt {
			return candidates[i].CandidateRef < candidates[j].CandidateRef
		}
		return candidates[i].OccurredAt < candidates[j].OccurredAt
	})
	return candidates, nil
}

func validResearchOpaqueCandidateRef(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func (s *ResearchStore) ListResearchWorkerCandidates(runID string) ([]ResearchWorkerCandidate, error) {
	rows, err := s.db.Query(`SELECT run_id, job_id, candidate_ref, source_type, source_role, occurred_at, created_at
		FROM research_worker_candidates WHERE run_id = ? ORDER BY occurred_at ASC, candidate_ref ASC`, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ResearchWorkerCandidate{}
	for rows.Next() {
		var item ResearchWorkerCandidate
		if err := rows.Scan(&item.RunID, &item.JobID, &item.CandidateRef, &item.SourceType, &item.SourceRole, &item.OccurredAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *ResearchStore) FailWorkerJob(jobID, agentID, leaseID, requestHash, code string, retryable bool) (*ResearchWorkerJob, error) {
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
	if job.TargetAgentID != agentID {
		return nil, ErrResearchWorkerLeaseOwner
	}
	if strings.TrimSpace(leaseID) == "" || job.LeaseID != strings.TrimSpace(leaseID) {
		return nil, ErrResearchWorkerStaleResult
	}
	if requestHash == "" || requestHash != job.RequestHash {
		return nil, ErrResearchWorkerStaleResult
	}
	if (job.State == ResearchWorkerJobQueued || job.State == ResearchWorkerJobFailed) && job.FailureCode == code {
		return job, nil
	}
	if job.LeaseOwner != agentID {
		return nil, ErrResearchWorkerLeaseOwner
	}
	if job.State != ResearchWorkerJobLeased || researchWorkerLeaseExpired(job.LeaseExpiresAt, s.now()) {
		return nil, ErrResearchWorkerTerminal
	}
	state := ResearchWorkerJobFailed
	completedAt := s.now().UTC().Format(time.RFC3339Nano)
	if retryable && job.Attempt < job.MaxAttempts {
		state = ResearchWorkerJobQueued
		completedAt = ""
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	update, err := tx.Exec(`UPDATE research_worker_jobs SET state = ?, lease_owner = '', lease_expires_at = '',
		failure_code = ?, updated_at = ?, completed_at = ? WHERE job_id = ? AND state = ? AND lease_owner = ? AND lease_id = ?`,
		state, code, now, completedAt, job.JobID, ResearchWorkerJobLeased, agentID, leaseID)
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
		target = &ResearchWorkerSearchChatlogArgs{}
	case ResearchWorkerToolExpandChatContext:
		target = &ResearchWorkerExpandChatContextArgs{}
	case ResearchWorkerToolResolveChatIdentity:
		target = &ResearchWorkerResolveChatIdentityArgs{}
	case ResearchWorkerToolListIdentityConversations:
		target = &ResearchWorkerListIdentityConversationsArgs{}
	case ResearchWorkerToolFetchChatMessage:
		target = &ResearchWorkerFetchChatMessageArgs{}
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
		var args ResearchWorkerSearchChatlogArgs
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if args.Limit <= 0 || args.Limit > researchWorkerQueryLimitMax || args.Offset < 0 {
			return fmt.Errorf("search limit must be between 1 and %d and offset must be non-negative", researchWorkerQueryLimitMax)
		}
		if strings.TrimSpace(args.TalkerRef) == "" {
			return fmt.Errorf("talker_ref is required")
		}
		if strings.TrimSpace(args.TalkerRef) == "*" &&
			(strings.TrimSpace(args.Keyword) == "" || args.Offset > 500) {
			return fmt.Errorf("global Chatlog search requires a keyword and offset at most 500")
		}
		if strings.TrimSpace(args.TimeFrom) == "" || strings.TrimSpace(args.TimeTo) == "" {
			return fmt.Errorf("time_from and time_to are required")
		}
		if err := validateResearchWorkerTimeRange(args.TimeFrom, args.TimeTo); err != nil {
			return err
		}
	case ResearchWorkerToolExpandChatContext:
		var args ResearchWorkerExpandChatContextArgs
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if strings.TrimSpace(args.MessageRef) == "" || strings.TrimSpace(args.ConversationRef) == "" ||
			!validResearchWorkerChatlogDate(args.Time) || args.Before < 0 || args.After < 0 ||
			args.Before > researchWorkerContextWindowMax || args.After > researchWorkerContextWindowMax {
			return fmt.Errorf("context arguments are outside supported bounds")
		}
	case ResearchWorkerToolResolveChatIdentity:
		var args ResearchWorkerResolveChatIdentityArgs
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if strings.TrimSpace(args.IdentityRef) == "" {
			return fmt.Errorf("identity_ref is required")
		}
	case ResearchWorkerToolListIdentityConversations:
		var args ResearchWorkerListIdentityConversationsArgs
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if strings.TrimSpace(args.IdentityRef) == "" || args.Limit <= 0 || args.Limit > researchWorkerQueryLimitMax || args.Offset < 0 {
			return fmt.Errorf("identity conversation query is outside supported bounds")
		}
	case ResearchWorkerToolFetchChatMessage:
		var args ResearchWorkerFetchChatMessageArgs
		if err := json.Unmarshal(encoded, &args); err != nil {
			return err
		}
		if strings.TrimSpace(args.MessageRef) == "" || strings.TrimSpace(args.ConversationRef) == "" || !validResearchWorkerChatlogDate(args.Time) {
			return fmt.Errorf("message_ref, conversation_ref, and time are required")
		}
	}
	return nil
}

func validResearchWorkerChatlogDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	return err == nil && parsed.Format("2006-01-02") == strings.TrimSpace(value)
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
		attempt, max_attempts, lease_owner, lease_id, lease_expires_at, request_hash, result_fingerprint,
		failure_code, created_at, updated_at, completed_at FROM research_worker_jobs WHERE job_id = ?`, jobID).Scan(
		&job.JobID, &job.RunID, &job.TargetAgentID, &job.Tool, &arguments, &job.State,
		&job.Attempt, &job.MaxAttempts, &job.LeaseOwner, &job.LeaseID, &job.LeaseExpiresAt, &job.RequestHash,
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

func newResearchWorkerLeaseID() string {
	return strings.Replace(newResearchRunID(), "research-run-", "research-lease-", 1)
}
