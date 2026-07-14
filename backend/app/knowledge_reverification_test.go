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
	if coalesced.TaskID != first.TaskID || coalesced.AssessmentAt != newAssessment.ReverificationAt ||
		coalesced.AssessmentFingerprint != newAssessment.ReverificationFingerprint {
		t.Fatalf("coalesced task = %#v", coalesced)
	}
	if len(coalesced.TriggerOutcomes) != 2 {
		t.Fatalf("trigger outcomes = %#v", coalesced.TriggerOutcomes)
	}
	replayedCoalesced, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *newAssessment, now.Add(3*time.Second), 5*time.Minute)
	if err != nil || replayedCoalesced.TaskID != first.TaskID {
		t.Fatalf("replayed coalesced task = %#v, err=%v", replayedCoalesced, err)
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
	if _, err := store.CompleteKnowledgeReverification(task.TaskID, claimed.AssessmentAt, claimed.AssessmentFingerprint, KnowledgeReverificationCandidate{
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
	if recovered, ok, err := store.ClaimNextKnowledgeReverification(now.Add(16*time.Minute), 15*time.Minute); err != nil || ok {
		t.Fatalf("recovered before backoff = %#v, ok=%v, err=%v", recovered, ok, err)
	}
	tasks, err := store.ListKnowledgeReverifications(release.ReleaseID)
	if err != nil || len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationQueued {
		t.Fatalf("stale task = %#v, err=%v", tasks, err)
	}
	availableAt, err := time.Parse(time.RFC3339Nano, tasks[0].AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	recovered, ok, err := store.ClaimNextKnowledgeReverification(availableAt, 15*time.Minute)
	if err != nil || !ok || recovered.TaskID != task.TaskID || recovered.Attempts != 2 {
		t.Fatalf("recovered after backoff = %#v, ok=%v, err=%v", recovered, ok, err)
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
	if len(tasks) != 1 || !tasks[0].ContentChanged || tasks[0].ReleaseContentHash != release.ContentHash || tasks[0].CandidateContentHash != "content-hash-new" {
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
	ctx, cancel := context.WithCancel(context.Background())
	runner := NewKnowledgeReverificationRunner(store, func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		cancel()
		return nil, context.Canceled
	}, func() time.Time { return now }, 15*time.Minute)
	result, err := runner.Tick(ctx)
	if err != nil || result.Status != KnowledgeReverificationQueued {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
	tasks, _ := store.ListKnowledgeReverifications(release.ReleaseID)
	if len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationQueued || tasks[0].CompletedAt != "" {
		t.Fatalf("cancelled task = %#v", tasks)
	}
}

func TestKnowledgeReverificationRunnerBacksOffDownstreamTimeout(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	runner := NewKnowledgeReverificationRunner(store, func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		return nil, context.DeadlineExceeded
	}, func() time.Time { return now }, 15*time.Minute)
	result, err := runner.Tick(context.Background())
	if err != nil || result.Status != KnowledgeReverificationQueued {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
	tasks, _ := store.ListKnowledgeReverifications(release.ReleaseID)
	if len(tasks) != 1 || tasks[0].Attempts != 1 || tasks[0].AvailableAt == now.Format(time.RFC3339Nano) {
		t.Fatalf("timeout task = %#v", tasks)
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
	if _, _, err := store.SaveKnowledgeFeedback(release.ReleaseID, KnowledgeFeedbackInput{
		EventID: "event-used-after-candidate", Consumer: "consumer-a", Outcome: KnowledgeFeedbackUsed,
	}); err != nil {
		t.Fatal(err)
	}
	published, err := PublishKnowledgeRelease(store, release.BookID)
	if err != nil {
		t.Fatalf("publish matching candidate returned error: %v", err)
	}
	tasks, err := store.ListKnowledgeReverifications(release.ReleaseID)
	if err != nil || len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationPublished || tasks[0].PublishedReleaseID != published.ReleaseID {
		t.Fatalf("published task = %#v, release=%#v, err=%v", tasks, published, err)
	}

	pkg, err := store.LoadPackage(release.BookID)
	if err != nil {
		t.Fatal(err)
	}
	pkg.Book.ContentHash = "later-manual-content"
	if err := store.SavePackage(*pkg); err != nil {
		t.Fatal(err)
	}
	saveReadyReverificationAnalysis(t, store, release.BookID)
	if _, err := EvaluateBookAnalysisQuality(store, release.BookID); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishKnowledgeRelease(store, release.BookID); err != nil {
		t.Fatalf("later manual publication remained blocked: %v", err)
	}
}

func TestKnowledgeReverificationRequeueUsesBackoffAndAttemptCeiling(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= knowledgeReverificationMaxAttempts; attempt++ {
		claimed, ok, err := store.ClaimNextKnowledgeReverification(now, 15*time.Minute)
		if err != nil || !ok {
			t.Fatalf("claim attempt %d = %#v ok=%v err=%v", attempt, claimed, ok, err)
		}
		task, err := store.RequeueKnowledgeReverification(claimed.TaskID, now, knowledgeReverificationRetryDelay(claimed.Attempts))
		if err != nil {
			t.Fatal(err)
		}
		if attempt < knowledgeReverificationMaxAttempts {
			if task.Status != KnowledgeReverificationQueued || task.AvailableAt == now.Format(time.RFC3339Nano) {
				t.Fatalf("backoff task at attempt %d = %#v", attempt, task)
			}
			availableAt, err := time.Parse(time.RFC3339Nano, task.AvailableAt)
			if err != nil {
				t.Fatal(err)
			}
			now = availableAt
		} else if task.Status != KnowledgeReverificationFailed || task.ErrorCode != KnowledgeReverificationErrorRetryExhausted {
			t.Fatalf("exhausted task = %#v", task)
		}
	}
}

func TestKnowledgeReverificationRetryResetsCurrentFailedTask(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	task, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextKnowledgeReverification(now, 15*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claimed = %#v, ok=%v, err=%v", claimed, ok, err)
	}
	failed, err := store.FailKnowledgeReverification(task.TaskID, claimed.AssessmentAt, claimed.AssessmentFingerprint, KnowledgeReverificationErrorAnalysisFailed, now.Add(time.Minute))
	if err != nil || failed.Status != KnowledgeReverificationFailed {
		t.Fatalf("failed task = %#v, err=%v", failed, err)
	}

	retried, err := store.RetryKnowledgeReverification(release.ReleaseID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != KnowledgeReverificationQueued || retried.Attempts != 0 || retried.AvailableAt != now.Add(2*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("retried task = %#v", retried)
	}
	if retried.ErrorCode != "" || retried.StartedAt != "" || retried.CompletedAt != "" {
		t.Fatalf("retry retained terminal state = %#v", retried)
	}
}

func TestKnowledgeReverificationRetryRejectsNonFailedTask(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryKnowledgeReverification(release.ReleaseID, now.Add(time.Minute)); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("queued retry error = %v", err)
	}
	tasks, err := store.ListKnowledgeReverifications(release.ReleaseID)
	if err != nil || len(tasks) != 1 || tasks[0].Status != KnowledgeReverificationQueued || tasks[0].Attempts != 0 {
		t.Fatalf("queued task changed = %#v, err=%v", tasks, err)
	}
	claimed, ok, err := store.ClaimNextKnowledgeReverification(now, 15*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claimed = %#v, ok=%v, err=%v", claimed, ok, err)
	}
	ready, err := store.CompleteKnowledgeReverification(claimed.TaskID, claimed.AssessmentAt, claimed.AssessmentFingerprint, KnowledgeReverificationCandidate{
		AnalysisHash: "analysis-ready", QualityDecision: BookQualityPass,
	}, now.Add(time.Minute))
	if err != nil || ready.Status != KnowledgeReverificationCandidateReady {
		t.Fatalf("ready task = %#v, err=%v", ready, err)
	}
	if _, err := store.RetryKnowledgeReverification(release.ReleaseID, now.Add(2*time.Minute)); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("candidate-ready retry error = %v", err)
	}
}

func TestKnowledgeReverificationRetryRejectsSupersededFeedback(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	task, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextKnowledgeReverification(now, 15*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claimed = %#v, ok=%v, err=%v", claimed, ok, err)
	}
	if _, err := store.FailKnowledgeReverification(task.TaskID, claimed.AssessmentAt, claimed.AssessmentFingerprint, KnowledgeReverificationErrorAnalysisFailed, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveKnowledgeFeedback(release.ReleaseID, KnowledgeFeedbackInput{
		EventID: "event-conflict", Consumer: "consumer-a", Outcome: KnowledgeFeedbackConflict,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryKnowledgeReverification(release.ReleaseID, now.Add(2*time.Minute)); err == nil || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("superseded retry error = %v", err)
	}
}

func TestKnowledgeFeedbackAndPublicationUseSameCrossProcessLock(t *testing.T) {
	store, release := feedbackTestStore(t)
	unlock, err := store.acquireKnowledgeReverificationFileLock()
	if err != nil {
		t.Fatal(err)
	}
	feedbackDone := make(chan error, 1)
	go func() {
		_, _, err := NewBookKnowledgeStore(store.Root()).SaveKnowledgeFeedback(release.ReleaseID, KnowledgeFeedbackInput{
			EventID: "event-stale", Consumer: "consumer-a", Outcome: KnowledgeFeedbackStale,
		})
		feedbackDone <- err
	}()
	select {
	case err := <-feedbackDone:
		t.Fatalf("feedback bypassed cross-process lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	if err := <-feedbackDone; err != nil {
		t.Fatal(err)
	}
	if _, err := PublishKnowledgeRelease(store, release.BookID); err == nil || !strings.Contains(err.Error(), "reverification") {
		t.Fatalf("publication ignored feedback committed before gate: %v", err)
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
