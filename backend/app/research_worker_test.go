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
		{ResearchWorkerToolSearchChatlog, `{"time_from":"2026-08-01T00:00:00Z","time_to":"2026-08-02T00:00:00Z","keyword":"term","limit":20,"offset":0}`},
		{ResearchWorkerToolExpandChatContext, `{"message_ref":"message-1","before":5,"after":5}`},
		{ResearchWorkerToolResolveChatIdentity, `{"identity_ref":"identity-1","conversation_ref":"conversation-1"}`},
		{ResearchWorkerToolListIdentityConversations, `{"identity_ref":"identity-1","limit":20,"offset":0}`},
		{ResearchWorkerToolFetchChatMessage, `{"message_ref":"message-1"}`},
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
		{RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage, Arguments: []byte(`{"message_ref":"message-1","unknown":"private"}`), MaxAttempts: 2},
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
	job, _, err := store.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolSearchChatlog,
		Arguments: []byte(`{"keyword":"bounded","limit":10}`), MaxAttempts: 2,
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
	if err := store.RenewWorkerJobLease(job.JobID, "chatlog-agent-b", time.Minute); !errors.Is(err, ErrResearchWorkerLeaseOwner) {
		t.Fatalf("wrong owner renewal error=%v", err)
	}
	if err := store.RenewWorkerJobLease(job.JobID, "chatlog-agent-a", 2*time.Minute); err != nil {
		t.Fatal(err)
	}

	candidate := researchEvidenceTestCandidate("selected worker evidence")
	completed, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", job.RequestHash, ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog}, Items: []ResearchWorkerEvidenceCandidate{candidate},
		RawResponseBody: "RAW_WORKER_RESPONSE", Cookie: "COOKIE_WORKER_SECRET",
		Authorization: "Bearer WORKER_SECRET", LocalPath: "/private/chat.db",
	})
	if err != nil || completed.State != ResearchWorkerJobCompleted || completed.ResultFingerprint == "" {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	replayed, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", job.RequestHash, ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog}, Items: []ResearchWorkerEvidenceCandidate{candidate},
	})
	if err != nil || replayed.JobID != completed.JobID {
		t.Fatalf("replayed=%#v err=%v", replayed, err)
	}
	changed := candidate
	changed.Content = "changed result"
	if _, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", job.RequestHash, ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{changed}}); !errors.Is(err, ErrResearchWorkerStaleResult) {
		t.Fatalf("changed completion error=%v", err)
	}
	if _, err := store.CompleteWorkerJob(job.JobID, "chatlog-agent-a", "sha256:stale", ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{candidate}}); !errors.Is(err, ErrResearchWorkerStaleResult) {
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
		Arguments: []byte(`{"message_ref":"message-1"}`), MaxAttempts: 2,
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
		Arguments: []byte(`{"message_ref":"message-1"}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, _ := store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	requeued, err := store.FailWorkerJob(job.JobID, "chatlog-agent-a", claimed.RequestHash, "dependency_unavailable", true)
	if err != nil || requeued.State != ResearchWorkerJobQueued {
		t.Fatalf("requeued=%#v err=%v", requeued, err)
	}
	claimed, _ = store.ClaimWorkerJob("chatlog-agent-a", time.Minute)
	failed, err := store.FailWorkerJob(job.JobID, "chatlog-agent-a", claimed.RequestHash, "dependency_unavailable", true)
	if err != nil || failed.State != ResearchWorkerJobFailed || failed.FailureCode != "dependency_unavailable" {
		t.Fatalf("failed=%#v err=%v", failed, err)
	}
}
