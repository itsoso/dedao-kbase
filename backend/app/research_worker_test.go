package app

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResearchWorkerJobAcceptsOnlyBoundedTypedTools(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "worker-tools")
	tools := []struct {
		name string
		args string
	}{
		{ResearchWorkerToolSearchChatlog, `{"time_from":"2026-08-01T00:00:00Z","time_to":"2026-08-02T00:00:00Z","talker_ref":"conversation-1","keyword":"term","limit":20,"offset":0}`},
		{ResearchWorkerToolExpandChatContext, `{"message_ref":"message-1","conversation_ref":"conversation-1","time":"2026-08-13","before":5,"after":5}`},
		{ResearchWorkerToolResolveChatIdentity, `{"identity_ref":"identity-1","conversation_ref":"conversation-1"}`},
		{ResearchWorkerToolListIdentityConversations, `{"identity_ref":"identity-1","limit":20,"offset":0}`},
		{ResearchWorkerToolFetchChatMessage, `{"message_ref":"message-1","conversation_ref":"conversation-1","time":"2026-08-13"}`},
	}
	for _, fixture := range tools {
		job, created, err := store.CreateWorkerJob(ResearchWorkerJobInput{
			RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: fixture.name,
			Arguments: []byte(fixture.args), MaxAttempts: 2,
		})
		if err != nil || !created || !strings.HasPrefix(job.JobID, "research-job-") ||
			job.Tool != fixture.name || job.State != ResearchWorkerJobQueued {
			t.Fatalf("tool=%s job=%#v created=%v err=%v", fixture.name, job, created, err)
		}
		replayed, created, err := store.CreateWorkerJob(ResearchWorkerJobInput{
			RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: fixture.name,
			Arguments: []byte(fixture.args), MaxAttempts: 2,
		})
		if err != nil || created || replayed.JobID != job.JobID {
			t.Fatalf("replay tool=%s job=%#v created=%v err=%v", fixture.name, replayed, created, err)
		}
	}
	for _, invalid := range []ResearchWorkerJobInput{
		{RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: "shell", Arguments: []byte(`{}`), MaxAttempts: 2},
		{RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolSearchChatlog, Arguments: []byte(`{"time_from":"2026-08-02T00:00:00Z","time_to":"2026-08-01T00:00:00Z","limit":20}`), MaxAttempts: 2},
		{RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolSearchChatlog, Arguments: []byte(`{"limit":501}`), MaxAttempts: 2},
		{RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolSearchChatlog, Arguments: []byte(`{"talker_ref":"conversation-1","keyword":"term","limit":20}`), MaxAttempts: 2},
		{RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage, Arguments: []byte(`{"message_ref":"message-1","conversation_ref":"conversation-1","time":"2026-08-13","unknown":"private"}`), MaxAttempts: 2},
		{RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage, Arguments: []byte(`{"message_ref":"message-1"}`), MaxAttempts: 2},
		{RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolExpandChatContext, Arguments: []byte(`{"message_ref":"message-1","before":5,"after":5}`), MaxAttempts: 2},
	} {
		if _, _, err := store.CreateWorkerJob(invalid); err == nil {
			t.Fatalf("invalid worker job accepted: %#v", invalid)
		}
	}
}

func TestResearchWorkerJobLeaseCompletionAndPrivacyProjection(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	run := createResearchRunForTest(t, store, "worker-complete")
	const candidateRef = "sha256:5a01731f9d22d0e8243e4f3f5170b8710d35a48a49bf1090962a7a37efa94451"
	seedResearchWorkerCandidateForTest(t, store, run.RunID, "chatlog-agent-a", candidateRef)
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage,
		Arguments: []byte(`{"message_ref":"` + candidateRef + `","conversation_ref":"` + candidateRef + `","time":"2026-08-13"}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	if err != nil || claimed == nil || claimed.JobID != job.JobID || claimed.State != ResearchWorkerJobLeased || claimed.Attempt != 1 {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	if _, err := store.ClaimWorkerJob("chatlog-agent-b", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.RenewWorkerJobLease(job.JobID, "chatlog-agent-b", claimed.LeaseID, time.Minute); !errors.Is(err, ErrResearchWorkerLeaseOwner) {
		t.Fatalf("wrong owner renewal error=%v", err)
	}
	if err := store.RenewWorkerJobLease(job.JobID, "chatlog-agent-a", claimed.LeaseID, 2*time.Minute); err != nil {
		t.Fatal(err)
	}

	candidate := researchEvidenceTestCandidate("selected worker evidence")
	candidate.Locator.WorkerID = "chatlog-agent-a"
	completed, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, job.RequestHash, ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog}, Items: []ResearchWorkerEvidenceCandidate{candidate},
		RawResponseBody: "RAW_WORKER_RESPONSE", Cookie: "COOKIE_WORKER_SECRET",
		Authorization: "Bearer WORKER_SECRET", LocalPath: "/private/chat.db",
	})
	if err != nil || completed.State != ResearchWorkerJobCompleted || completed.ResultFingerprint == "" {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	replayed, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, job.RequestHash, ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog}, Items: []ResearchWorkerEvidenceCandidate{candidate},
	})
	if err != nil || replayed.JobID != completed.JobID {
		t.Fatalf("replayed=%#v err=%v", replayed, err)
	}
	changed := candidate
	changed.Content = "changed result"
	if _, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, job.RequestHash, ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{changed}}); !errors.Is(err, ErrResearchWorkerStaleResult) {
		t.Fatalf("changed completion error=%v", err)
	}
	if _, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, "sha256:stale", ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{candidate}}); !errors.Is(err, ErrResearchWorkerStaleResult) {
		t.Fatalf("stale request error=%v", err)
	}

	stored, err := store.ListEvidence(run.RunID)
	if err != nil || len(stored) != 1 || stored[0].ContentExcerpt != candidate.Content {
		t.Fatalf("evidence=%#v err=%v", stored, err)
	}
	var databaseText string
	if err := store.db.QueryRow(`SELECT COALESCE(GROUP_CONCAT(content_excerpt || locator_json), '') FROM research_evidence WHERE run_id = ?`, run.RunID).Scan(&databaseText); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"RAW_WORKER_RESPONSE", "COOKIE_WORKER_SECRET", "Bearer WORKER_SECRET", "/private/chat.db"} {
		if contains := strings.Contains(databaseText, forbidden); contains {
			t.Fatalf("database leaked %q: %s", forbidden, databaseText)
		}
	}
}

func TestResearchWorkerFetchRejectsWrongOrCrossRunCandidate(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "worker-fetch-boundary")
	otherRun := createResearchRunForTest(t, store, "worker-fetch-other-run")
	const requestedRef = "sha256:5a01731f9d22d0e8243e4f3f5170b8710d35a48a49bf1090962a7a37efa94451"
	const wrongRef = "sha256:6b12842f0e33e1f9354f50406281c9821e46b59b5acf21a1073b8b48fba05562"
	seedResearchWorkerCandidateForTest(t, store, run.RunID, "chatlog-agent-a", requestedRef)
	seedResearchWorkerCandidateForTest(t, store, otherRun.RunID, "chatlog-agent-a", wrongRef)
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage,
		Arguments: []byte(`{"message_ref":"` + requestedRef + `","conversation_ref":"` + requestedRef + `","time":"2026-08-15"}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	if err != nil || claimed.JobID != job.JobID {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	item := researchEvidenceTestCandidate("wrong message")
	item.Locator.WorkerID = "chatlog-agent-a"
	item.Locator.MessageRef = wrongRef
	item.Locator.ConversationRef = wrongRef
	if _, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, job.RequestHash, ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{item}}); err == nil {
		t.Fatal("accepted a fetch result for a different or cross-run candidate")
	}
	evidence, err := store.ListEvidence(run.RunID)
	if err != nil || len(evidence) != 0 {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
}

func seedResearchWorkerCandidateForTest(t *testing.T, store *ResearchStore, runID, agentID, candidateRef string) {
	t.Helper()
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: runID, TargetAgentID: agentID, Tool: ResearchWorkerToolSearchChatlog,
		Arguments: []byte(`{"time_from":"2026-08-15T00:00:00Z","time_to":"2026-08-15T23:59:59Z","talker_ref":"conversation","keyword":"term","limit":10}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimWorkerJob(agentID, time.Minute)
	if err != nil || claimed == nil || claimed.JobID != job.JobID {
		t.Fatalf("claim candidate job=%#v err=%v", claimed, err)
	}
	_, err = store.CompleteWorkerJob(job.JobID, agentID, claimed.LeaseID, job.RequestHash, ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog}, Items: []ResearchWorkerEvidenceCandidate{{
			SourceType: ResearchEvidenceSourceChatlog, SourceRole: ResearchEvidenceRoleUserHistory,
			OccurredAt: "2026-08-15T08:00:00Z", Privacy: ResearchEvidencePrivacyPrivate,
			Locator: ResearchEvidenceLocator{WorkerID: agentID, ConversationRef: candidateRef, MessageRef: candidateRef},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResearchWorkerSearchPersistsOnlyOpaqueCandidates(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "worker-search-candidates")
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolSearchChatlog,
		Arguments: []byte(`{"time_from":"2026-08-13T00:00:00Z","time_to":"2026-08-13T23:59:59Z","talker_ref":"private-conversation","keyword":"bounded","limit":10}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	const candidateRef = "sha256:5a01731f9d22d0e8243e4f3f5170b8710d35a48a49bf1090962a7a37efa94451"
	result := ResearchWorkerResult{SearchedSources: []string{ResearchSourceChatlog}, Items: []ResearchWorkerEvidenceCandidate{{
		SourceType: ResearchEvidenceSourceChatlog, SourceRole: ResearchEvidenceRoleUserHistory,
		OccurredAt: "2026-08-13T08:01:00+08:00", Privacy: ResearchEvidencePrivacyPrivate, Selected: false,
		Locator: ResearchEvidenceLocator{WorkerID: "chatlog-agent-a", ConversationRef: candidateRef, MessageRef: candidateRef},
	}}}
	if _, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, job.RequestHash, result); err != nil {
		t.Fatal(err)
	}
	evidence, err := store.ListEvidence(run.RunID)
	if err != nil || len(evidence) != 0 {
		t.Fatalf("search promoted evidence=%#v err=%v", evidence, err)
	}
	candidates, err := store.ListResearchWorkerCandidates(run.RunID)
	if err != nil || len(candidates) != 1 || candidates[0].CandidateRef != candidateRef {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
	var databaseText string
	if err := store.db.QueryRow(`SELECT COALESCE(GROUP_CONCAT(candidate_ref || occurred_at), '') FROM research_worker_candidates WHERE run_id = ?`, run.RunID).Scan(&databaseText); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-conversation", "PRIVATE_MESSAGE", "3001"} {
		if strings.Contains(databaseText, forbidden) {
			t.Fatalf("candidate store leaked %q: %s", forbidden, databaseText)
		}
	}
}

func TestResearchWorkerIdentityCandidatesPersistAsOpaqueBindings(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "worker-candidate-bindings")
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolResolveChatIdentity,
		Arguments: []byte(`{"identity_ref":"target-name"}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	const identityID = "chat-identity-5a01731f9d22d0e8243e4f3f5170b871"
	if _, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, job.RequestHash, ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog},
		IdentityCandidates: []ResearchIdentityCandidate{{
			IdentityID: identityID, AccountID: identityID, TargetAccountID: identityID,
			ContactMetadataMatch: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	var bindingID, storedIdentity, sourceHash string
	var confidence float64
	if err := store.db.QueryRow(`SELECT binding_id, identity_id, source_identity_hash, confidence
		FROM research_identity_bindings WHERE run_id = ?`, run.RunID).Scan(&bindingID, &storedIdentity, &sourceHash, &confidence); err != nil {
		t.Fatal(err)
	}
	if bindingID == "" || storedIdentity != identityID || !strings.HasPrefix(sourceHash, "sha256:") || confidence != 1 {
		t.Fatalf("binding=%q identity=%q hash=%q confidence=%v", bindingID, storedIdentity, sourceHash, confidence)
	}
	for _, forbidden := range []string{"target-name", "display-name", "raw-account"} {
		if strings.Contains(bindingID+storedIdentity+sourceHash, forbidden) {
			t.Fatalf("identity binding leaked %q", forbidden)
		}
	}
}

func TestResearchWorkerSearchRejectsDirectPromotionAndRawLocators(t *testing.T) {
	job := &ResearchWorkerJob{Tool: ResearchWorkerToolSearchChatlog, TargetAgentID: "chatlog-agent-a"}
	for _, result := range []ResearchWorkerResult{
		{Items: []ResearchWorkerEvidenceCandidate{{
			SourceType: ResearchEvidenceSourceChatlog, SourceRole: ResearchEvidenceRoleUserHistory,
			Content: "PRIVATE_MESSAGE", Privacy: ResearchEvidencePrivacyPrivate, Selected: true,
			Locator: ResearchEvidenceLocator{WorkerID: "chatlog-agent-a", ConversationRef: "raw-conversation", MessageRef: "3001"},
		}}},
		{Items: []ResearchWorkerEvidenceCandidate{{
			SourceType: ResearchEvidenceSourceChatlog, SourceRole: ResearchEvidenceRoleUserHistory,
			Privacy: ResearchEvidencePrivacyPrivate, Selected: false,
			Locator: ResearchEvidenceLocator{WorkerID: "chatlog-agent-a", ConversationRef: "raw-conversation", MessageRef: "3001"},
		}}},
	} {
		if _, err := normalizeResearchWorkerCandidates(job, result); err == nil {
			t.Fatalf("invalid search result accepted: %#v", result)
		}
	}
}

func TestResearchWorkerJobRecoversExpiredLeaseWithinRetryBudget(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	run := createResearchRunForTest(t, store, "worker-recovery")
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage,
		Arguments: []byte(`{"message_ref":"message-1","conversation_ref":"conversation-1","time":"2026-08-13"}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimWorkerJob("chatlog-agent-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if recovered, err := store.RecoverExpiredWorkerJobs(); err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	second, err := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	if err != nil || second == nil || second.JobID != job.JobID || second.Attempt != 2 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	now = now.Add(2 * time.Minute)
	if recovered, err := store.RecoverExpiredWorkerJobs(); err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	loaded, err := store.LoadWorkerJob(job.JobID)
	if err != nil || loaded.State != ResearchWorkerJobExpired {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestResearchWorkerJobFailureHonorsRetryBudget(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "worker-failure")
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage,
		Arguments: []byte(`{"message_ref":"message-1","conversation_ref":"conversation-1","time":"2026-08-13"}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, _ := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	requeued, err := store.FailWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, claimed.RequestHash, "dependency_unavailable", true)
	if err != nil || requeued.State != ResearchWorkerJobQueued {
		t.Fatalf("requeued=%#v err=%v", requeued, err)
	}
	replayed, err := store.FailWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, claimed.RequestHash, "dependency_unavailable", true)
	if err != nil || replayed.State != ResearchWorkerJobQueued || replayed.JobID != requeued.JobID {
		t.Fatalf("replayed requeue=%#v err=%v", replayed, err)
	}
	claimed, _ = store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	failed, err := store.FailWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, claimed.RequestHash, "dependency_unavailable", true)
	if err != nil || failed.State != ResearchWorkerJobFailed || failed.FailureCode != "dependency_unavailable" {
		t.Fatalf("failed=%#v err=%v", failed, err)
	}
	replayed, err = store.FailWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, claimed.RequestHash, "dependency_unavailable", true)
	if err != nil || replayed.State != ResearchWorkerJobFailed || replayed.JobID != failed.JobID {
		t.Fatalf("replayed failure=%#v err=%v", replayed, err)
	}
}

func TestResearchWorkerJobRejectsFailureAfterLeaseExpiry(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	run := createResearchRunForTest(t, store, "worker-expired-failure")
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage,
		Arguments: []byte(`{"message_ref":"message-1","conversation_ref":"conversation-1","time":"2026-08-14"}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := store.RenewWorkerJobLease(job.JobID, "chatlog-agent-a", claimed.LeaseID, time.Minute); !errors.Is(err, ErrResearchWorkerTerminal) {
		t.Fatalf("expired renewal error=%v", err)
	}
	if _, err := store.FailWorkerJob(job.JobID, "chatlog-agent-a", claimed.LeaseID, claimed.RequestHash, "dependency_unavailable", true); !errors.Is(err, ErrResearchWorkerTerminal) {
		t.Fatalf("expired failure error=%v", err)
	}
}

func TestResearchWorkerJobRejectsStaleAttemptAfterReclaim(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	run := createResearchRunForTest(t, store, "worker-attempt-fence")
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage,
		Arguments: []byte(`{"message_ref":"message-1","conversation_ref":"conversation-1","time":"2026-08-15"}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := store.RecoverExpiredWorkerJobs(); err != nil {
		t.Fatal(err)
	}
	second, err := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.LeaseID == "" || second.LeaseID == "" || first.LeaseID == second.LeaseID {
		t.Fatalf("lease ids must be unique: first=%q second=%q", first.LeaseID, second.LeaseID)
	}
	if err := store.RenewWorkerJobLease(job.JobID, "chatlog-agent-a", first.LeaseID, time.Minute); !errors.Is(err, ErrResearchWorkerStaleResult) {
		t.Fatalf("stale renewal error=%v", err)
	}
	if _, err := store.FailWorkerJob(job.JobID, "chatlog-agent-a", first.LeaseID, first.RequestHash, "dependency_unavailable", true); !errors.Is(err, ErrResearchWorkerStaleResult) {
		t.Fatalf("stale failure error=%v", err)
	}
	if _, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", first.LeaseID, first.RequestHash, ResearchWorkerResult{}); !errors.Is(err, ErrResearchWorkerStaleResult) {
		t.Fatalf("stale completion error=%v", err)
	}
}
