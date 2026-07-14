package app

import (
	"testing"
	"time"
)

func TestKnowledgeReverificationEnqueueCoalescesActiveTask(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)

	first, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 5*time.Minute)
	if err != nil {
		t.Fatalf("EnqueueKnowledgeReverification returned error: %v", err)
	}
	replayed, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now.Add(time.Second), 5*time.Minute)
	if err != nil {
		t.Fatalf("replayed enqueue returned error: %v", err)
	}
	if first.TaskID == "" || replayed.TaskID != first.TaskID || first.Status != KnowledgeReverificationQueued {
		t.Fatalf("tasks = first %#v replayed %#v", first, replayed)
	}

	newAssessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-conflict", KnowledgeFeedbackConflict)
	coalesced, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *newAssessment, now.Add(2*time.Second), 5*time.Minute)
	if err != nil {
		t.Fatalf("coalesced enqueue returned error: %v", err)
	}
	if coalesced.TaskID != first.TaskID || coalesced.AssessmentAt != newAssessment.LatestFeedbackAt {
		t.Fatalf("coalesced task = %#v", coalesced)
	}
	if len(coalesced.TriggerOutcomes) != 2 {
		t.Fatalf("trigger outcomes = %#v", coalesced.TriggerOutcomes)
	}
	tasks, err := store.ListKnowledgeReverifications(release.ReleaseID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %#v, err=%v", tasks, err)
	}
}

func TestKnowledgeReverificationCreatesCooledTaskAfterTerminalTask(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	task, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextKnowledgeReverification(now, 15*time.Minute)
	if err != nil || !ok || claimed.TaskID != task.TaskID {
		t.Fatalf("claimed = %#v, ok=%v, err=%v", claimed, ok, err)
	}
	if _, err := store.CompleteKnowledgeReverification(task.TaskID, claimed.AssessmentAt, KnowledgeReverificationCandidate{
		AnalysisHash: "analysis-1", QualityDecision: BookQualityPass,
	}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	newAssessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-rejected", KnowledgeFeedbackRejected)
	second, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *newAssessment, now.Add(2*time.Minute), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.TaskID == task.TaskID || second.AvailableAt != now.Add(6*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("second task = %#v", second)
	}
	if _, ok, err := store.ClaimNextKnowledgeReverification(now.Add(5*time.Minute), 15*time.Minute); err != nil || ok {
		t.Fatalf("early claim ok=%v err=%v", ok, err)
	}
	claimedSecond, ok, err := store.ClaimNextKnowledgeReverification(now.Add(6*time.Minute), 15*time.Minute)
	if err != nil || !ok || claimedSecond.TaskID != second.TaskID {
		t.Fatalf("cooled claim = %#v, ok=%v, err=%v", claimedSecond, ok, err)
	}
}

func TestKnowledgeReverificationRecoversStaleRunningTask(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	task, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextKnowledgeReverification(now, 15*time.Minute)
	if err != nil || !ok {
		t.Fatalf("initial claim = %#v, ok=%v, err=%v", claimed, ok, err)
	}
	if _, ok, err := store.ClaimNextKnowledgeReverification(now.Add(14*time.Minute), 15*time.Minute); err != nil || ok {
		t.Fatalf("premature recovery ok=%v err=%v", ok, err)
	}
	recovered, ok, err := store.ClaimNextKnowledgeReverification(now.Add(16*time.Minute), 15*time.Minute)
	if err != nil || !ok || recovered.TaskID != task.TaskID || recovered.Attempts != 2 {
		t.Fatalf("recovered = %#v, ok=%v, err=%v", recovered, ok, err)
	}
}

func saveReverificationFeedback(t *testing.T, store *BookKnowledgeStore, releaseID, eventID, outcome string) *KnowledgeFeedbackAssessment {
	t.Helper()
	if _, _, err := store.SaveKnowledgeFeedback(releaseID, KnowledgeFeedbackInput{
		EventID: eventID, Consumer: "consumer-a", Outcome: outcome,
	}); err != nil {
		t.Fatal(err)
	}
	assessment, err := store.AssessKnowledgeFeedback(releaseID)
	if err != nil {
		t.Fatal(err)
	}
	return assessment
}
