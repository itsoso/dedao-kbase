package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func TestKnowledgeReverificationClaimIsAtomicAcrossStoreInstances(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	stores := []*BookKnowledgeStore{store, NewBookKnowledgeStore(store.Root())}
	start := make(chan struct{})
	results := make(chan bool, len(stores))
	errors := make(chan error, len(stores))
	for _, candidate := range stores {
		go func(current *BookKnowledgeStore) {
			<-start
			_, found, err := current.ClaimNextKnowledgeReverification(now, 15*time.Minute)
			results <- found
			errors <- err
		}(candidate)
	}
	close(start)
	claimed := 0
	for range stores {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		if <-results {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("cross-process-equivalent claims = %d, want 1", claimed)
	}
}

func TestKnowledgeReverificationRunnerProducesCandidateWithoutPublishing(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	calls := 0
	runner := NewKnowledgeReverificationRunner(store, func(_ context.Context, current *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		calls++
		return saveReadyReverificationAnalysis(t, current, request.BookID), nil
	}, func() time.Time { return now }, 15*time.Minute)

	result, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick returned error: %v", err)
	}
	if !result.Processed || result.Status != KnowledgeReverificationCandidateReady || calls != 1 {
		t.Fatalf("result = %#v, calls=%d", result, calls)
	}
	tasks, err := store.ListKnowledgeReverifications(release.ReleaseID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %#v, err=%v", tasks, err)
	}
	task := tasks[0]
	if task.QualityDecision != BookQualityPass || task.CandidateAnalysisHash == "" || task.ContentChanged {
		t.Fatalf("candidate task = %#v", task)
	}
	releases, err := store.ListKnowledgeReleases("", 10)
	if err != nil || len(releases) != 1 {
		t.Fatalf("releases changed = %#v, err=%v", releases, err)
	}
}

func TestKnowledgeReverificationRunnerRecordsChangedContent(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	pkg, err := store.LoadPackage(release.BookID)
	if err != nil {
		t.Fatal(err)
	}
	pkg.Book.ContentHash = "content-hash-new"
	if err := store.SavePackage(*pkg); err != nil {
		t.Fatal(err)
	}
	runner := NewKnowledgeReverificationRunner(store, func(_ context.Context, current *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		return saveReadyReverificationAnalysis(t, current, request.BookID), nil
	}, func() time.Time { return now }, 15*time.Minute)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks, _ := store.ListKnowledgeReverifications(release.ReleaseID)
	if len(tasks) != 1 || !tasks[0].ContentChanged || tasks[0].ReleaseContentHash != release.ContentHash || tasks[0].CurrentContentHash != "content-hash-new" {
		t.Fatalf("changed-content task = %#v", tasks)
	}
}

func TestKnowledgeReverificationRunnerRecordsAnalysisFailure(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	runner := NewKnowledgeReverificationRunner(store, func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		return nil, errors.New("model unavailable")
	}, func() time.Time { return now }, 15*time.Minute)
	result, err := runner.Tick(context.Background())
	if err != nil || result.Status != KnowledgeReverificationFailed {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
	tasks, _ := store.ListKnowledgeReverifications(release.ReleaseID)
	if len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationFailed || tasks[0].ErrorCode != KnowledgeReverificationErrorAnalysisFailed {
		t.Fatalf("failed task = %#v", tasks)
	}
	payload, err := json.Marshal(tasks[0])
	if err != nil || strings.Contains(string(payload), "model unavailable") || strings.Contains(string(payload), `"error":`) {
		t.Fatalf("public task leaked raw error: %s, err=%v", payload, err)
	}
}

func TestKnowledgeReverificationRunnerRequeuesCancellation(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	runner := NewKnowledgeReverificationRunner(store, func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		return nil, context.Canceled
	}, func() time.Time { return now }, 15*time.Minute)
	result, err := runner.Tick(context.Background())
	if err != nil || result.Status != KnowledgeReverificationQueued {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
	tasks, _ := store.ListKnowledgeReverifications(release.ReleaseID)
	if len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationQueued || tasks[0].CompletedAt != "" {
		t.Fatalf("cancelled task = %#v", tasks)
	}
}

func TestKnowledgeReverificationRunDoesNotClaimCancelledContext(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	runner := NewKnowledgeReverificationRunner(store, func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		t.Fatal("analysis generator must not run after cancellation")
		return nil, nil
	}, func() time.Time { return now }, 15*time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner.Run(ctx, time.Millisecond, nil)
	tasks, _ := store.ListKnowledgeReverifications(release.ReleaseID)
	if len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationQueued || tasks[0].Attempts != 0 {
		t.Fatalf("cancelled run task = %#v", tasks)
	}
}

func TestKnowledgeReverificationRunnerRequeuesWhenPackageChangesDuringAnalysis(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	runner := NewKnowledgeReverificationRunner(store, func(_ context.Context, current *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		manifest := saveReadyReverificationAnalysis(t, current, request.BookID)
		pkg, err := current.LoadPackage(request.BookID)
		if err != nil {
			t.Fatal(err)
		}
		pkg.Book.ContentHash = "changed-during-analysis"
		if err := current.SavePackage(*pkg); err != nil {
			t.Fatal(err)
		}
		return manifest, nil
	}, func() time.Time { return now }, 15*time.Minute)
	result, err := runner.Tick(context.Background())
	if err != nil || result.Status != KnowledgeReverificationQueued {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
	tasks, _ := store.ListKnowledgeReverifications(release.ReleaseID)
	if len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationQueued || tasks[0].CandidateAnalysisHash != "" {
		t.Fatalf("changed snapshot task = %#v", tasks)
	}
}

func TestKnowledgeReverificationRunnerRequeuesWhenFeedbackAdvances(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	runner := NewKnowledgeReverificationRunner(store, func(_ context.Context, current *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		advanced := saveReverificationFeedback(t, current, release.ReleaseID, "event-conflict", KnowledgeFeedbackConflict)
		if _, err := current.EnqueueKnowledgeReverification(release.ReleaseID, *advanced, now.Add(time.Second), 0); err != nil {
			t.Fatal(err)
		}
		return saveReadyReverificationAnalysis(t, current, request.BookID), nil
	}, func() time.Time { return now }, 15*time.Minute)
	result, err := runner.Tick(context.Background())
	if err != nil || result.Status != KnowledgeReverificationQueued {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
	tasks, _ := store.ListKnowledgeReverifications(release.ReleaseID)
	if len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationQueued || len(tasks[0].TriggerOutcomes) != 2 {
		t.Fatalf("requeued task = %#v", tasks)
	}
	if _, err := PublishKnowledgeRelease(store, release.BookID); err == nil || !strings.Contains(err.Error(), "reverification") {
		t.Fatalf("publish superseded candidate error = %v", err)
	}
}

func TestKnowledgeReleaseBlocksUnresolvedReverification(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishKnowledgeRelease(store, release.BookID); err == nil || !strings.Contains(err.Error(), "reverification") {
		t.Fatalf("publish with queued reverification error = %v", err)
	}
}

func TestKnowledgeReleaseAllowsMatchingReadyCandidate(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	runner := NewKnowledgeReverificationRunner(store, func(_ context.Context, current *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		return saveReadyReverificationAnalysis(t, current, request.BookID), nil
	}, func() time.Time { return now }, 15*time.Minute)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishKnowledgeRelease(store, release.BookID); err != nil {
		t.Fatalf("publish matching candidate returned error: %v", err)
	}
}

func saveReadyReverificationAnalysis(t *testing.T, store *BookKnowledgeStore, bookID string) *BookAnalysisManifest {
	t.Helper()
	pkg, err := store.LoadPackage(bookID)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.LoadAnalysisManifest(bookID)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ContentHash = pkg.Book.ContentHash
	manifest.Status = BookAnalysisReady
	manifest.Error = ""
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
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
