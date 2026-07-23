package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildKnowledgeOperationsConsoleCombinesPipelineReleaseAndHealthState(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "ready", "hash-ready")
	saveHealthAnalysis(t, store, "ready", "hash-ready")
	saveHealthQuality(t, store, "ready", "hash-ready", BookQualityPass, BookUsageEvidenceOnly)
	release := sampleHealthEvidenceRelease()
	release.ReleaseID = "release-ready"
	release.BookID = "ready"
	release.ContentHash = "hash-ready"
	release.Book.BookID = "ready"
	release.Book.Title = "Ready Evidence"
	saveFeedRelease(t, store, release)

	console, err := BuildKnowledgeOperationsConsole(store, 10)
	if err != nil {
		t.Fatalf("BuildKnowledgeOperationsConsole returned error: %v", err)
	}
	if console.SchemaVersion != KnowledgeOperationsSchemaVersion {
		t.Fatalf("schema version = %q", console.SchemaVersion)
	}
	if console.Summary.Total != 1 || console.Summary.Published != 1 || console.Summary.HealthPublished != 1 {
		t.Fatalf("summary = %#v", console.Summary)
	}
	if len(console.Items) != 1 {
		t.Fatalf("items = %#v", console.Items)
	}
	item := console.Items[0]
	if item.BookID != "ready" || item.Title != "Ready Evidence" || item.PipelineStage != KnowledgePipelineStagePublished {
		t.Fatalf("item identity = %#v", item)
	}
	if item.ReleaseID != "release-ready" || item.QualityDecision != BookQualityPass || item.UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("item release = %#v", item)
	}
	if item.Health.Status != HealthEvidenceReadinessPublished || item.Health.NextAction != "" || item.Health.ServingAllowed {
		t.Fatalf("health = %#v", item.Health)
	}
	if item.Failure.DangerousActionsBlocked[0] != "publish" {
		t.Fatalf("failure policy = %#v", item.Failure)
	}
}

func TestKnowledgeOperationsHealthSummaryDoesNotExposeSourceBody(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "book-health", "hash-health")
	saveHealthAnalysis(t, store, "book-health", "hash-health")
	saveHealthQuality(t, store, "book-health", "hash-health", BookQualityPass, BookUsageEvidenceOnly)
	saveFeedRelease(t, store, sampleHealthEvidenceRelease())

	console, err := BuildKnowledgeOperationsConsole(store, 10)
	if err != nil {
		t.Fatalf("BuildKnowledgeOperationsConsole returned error: %v", err)
	}
	item := console.Items[0]
	if item.Health.ServingAllowed {
		t.Fatalf("operations console must not allow Health serving: %#v", item.Health)
	}
	if item.Health.ClaimCount != 2 || item.Health.CitationCount != 2 {
		t.Fatalf("health counts = %#v", item.Health)
	}
	if item.Health.RiskCounts["medium"] != 1 || item.Health.RiskCounts["high"] != 1 {
		t.Fatalf("risk counts = %#v", item.Health.RiskCounts)
	}
	body, err := json.Marshal(console)
	if err != nil {
		t.Fatalf("marshal console: %v", err)
	}
	if strings.Contains(string(body), "规律运动可能帮助") || strings.Contains(string(body), "糖尿病药物调整") {
		t.Fatalf("operations console exposed claim statements: %s", string(body))
	}
}

func TestKnowledgeOperationsBuildsHealthReviewQueue(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "needs-analysis", "hash-analysis")
	saveHealthReadinessBook(t, store, "ready", "hash-ready")
	saveHealthAnalysis(t, store, "ready", "hash-ready")
	saveHealthQuality(t, store, "ready", "hash-ready", BookQualityPass, BookUsageEvidenceOnly)
	saveHealthReadinessBook(t, store, "book-health", "hash-health")
	saveHealthAnalysis(t, store, "book-health", "hash-health")
	saveHealthQuality(t, store, "book-health", "hash-health", BookQualityPass, BookUsageEvidenceOnly)
	saveFeedRelease(t, store, sampleHealthEvidenceRelease())

	console, err := BuildKnowledgeOperationsConsole(store, 10)
	if err != nil {
		t.Fatalf("BuildKnowledgeOperationsConsole returned error: %v", err)
	}
	if len(console.HealthReviewQueue) != 3 {
		t.Fatalf("health review queue = %#v", console.HealthReviewQueue)
	}
	published := findKnowledgeOperationsHealthReviewItem(t, console.HealthReviewQueue, "book-health")
	if published.Status != HealthEvidenceReadinessPublished || published.NextOperatorAction != "send_to_health_review" {
		t.Fatalf("published review item = %#v", published)
	}
	if published.ReleaseID != "release-health" || published.PriorityLabel != "review_next" || published.Priority <= 0 {
		t.Fatalf("published priority = %#v", published)
	}
	if !published.ConsumerReviewRequired || published.ServingAllowed {
		t.Fatalf("published safety = %#v", published)
	}
	if published.ClaimCount != 2 || published.CitationCount != 2 || published.RiskCounts["high"] != 1 {
		t.Fatalf("published evidence metadata = %#v", published)
	}
	ready := findKnowledgeOperationsHealthReviewItem(t, console.HealthReviewQueue, "ready")
	if ready.NextOperatorAction != "prepare_health_release" || ready.ConsumerReviewRequired {
		t.Fatalf("ready action = %#v", ready)
	}
	needsAnalysis := findKnowledgeOperationsHealthReviewItem(t, console.HealthReviewQueue, "needs-analysis")
	if needsAnalysis.NextOperatorAction != "run_analysis" || needsAnalysis.PriorityLabel != "needs_work" {
		t.Fatalf("needs-analysis action = %#v", needsAnalysis)
	}
	if console.HealthReviewQueue[0].BookID != "book-health" {
		t.Fatalf("queue not sorted by priority: %#v", console.HealthReviewQueue)
	}

	body, err := json.Marshal(console.HealthReviewQueue)
	if err != nil {
		t.Fatalf("marshal health review queue: %v", err)
	}
	if strings.Contains(string(body), "规律运动可能帮助") || strings.Contains(string(body), "糖尿病药物调整") {
		t.Fatalf("health review queue exposed claim statements: %s", string(body))
	}
	for _, item := range console.HealthReviewQueue {
		if item.NextOperatorAction == "publish" || item.NextOperatorAction == "health_serving_promote" {
			t.Fatalf("health review queue exposed unsafe action: %#v", item)
		}
	}
}

func TestKnowledgeOperationsExplainsEmptyHealthReviewQueue(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())

	console, err := BuildKnowledgeOperationsConsole(store, 10)
	if err != nil {
		t.Fatalf("BuildKnowledgeOperationsConsole returned error: %v", err)
	}
	if console.HealthReviewDiagnostics.QueueEmptyReason != "no_operations_items" {
		t.Fatalf("empty store diagnostics = %#v", console.HealthReviewDiagnostics)
	}
	if len(console.HealthReviewDiagnostics.NextSafeActions) != 1 || console.HealthReviewDiagnostics.NextSafeActions[0].Action != "import_or_sync_sources" {
		t.Fatalf("empty store next actions = %#v", console.HealthReviewDiagnostics.NextSafeActions)
	}

	diagnostics := buildKnowledgeOperationsHealthReviewDiagnostics(
		[]KnowledgeOperationsItem{{
			BookID:        "visible-without-health",
			Title:         "Visible Package",
			PipelineStage: KnowledgePipelineStagePublished,
		}},
		nil,
		KnowledgeOperationsSummary{
			Total:           1,
			HealthPublished: 3,
		},
	)
	if diagnostics.QueueEmptyReason != "no_items_match_current_limit" {
		t.Fatalf("limit mismatch diagnostics = %#v", diagnostics)
	}
	if diagnostics.NextSafeActions[0].Action != "increase_limit_or_filter" || diagnostics.NextSafeActions[0].Count != 1 {
		t.Fatalf("limit mismatch actions = %#v", diagnostics.NextSafeActions)
	}

	body, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("marshal diagnostics: %v", err)
	}
	if strings.Contains(string(body), "publish") || strings.Contains(string(body), "health_serving_promote") {
		t.Fatalf("diagnostics exposed unsafe action: %s", string(body))
	}
}

func TestKnowledgeOperationsExplainsFailuresWithSafeReplay(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	savePipelinePackage(t, store, "stale-quality", "hash-current")
	savePipelineAnalysis(t, store, "stale-quality", "hash-current")
	savePipelineQuality(t, store, "stale-quality", "hash-old", BookQualityPass)

	console, err := BuildKnowledgeOperationsConsole(store, 10)
	if err != nil {
		t.Fatalf("BuildKnowledgeOperationsConsole returned error: %v", err)
	}
	item := console.Items[0]
	if item.NextAction != "blocked" || item.Failure.Code != "quality_stale" {
		t.Fatalf("failure item = %#v", item)
	}
	if item.Failure.SafeReplayAction != "evaluate_quality" || !strings.Contains(item.Failure.Explanation, "quality") {
		t.Fatalf("failure explanation = %#v", item.Failure)
	}

	planned, err := RunKnowledgeOperationsReplay(context.Background(), store, nil, KnowledgeOperationsReplayRequest{
		BookID: "stale-quality",
		Action: "evaluate_quality",
	})
	if err != nil {
		t.Fatalf("planned replay returned error: %v", err)
	}
	if planned.Status != "planned" || planned.Mutated {
		t.Fatalf("planned replay = %#v", planned)
	}

	result, err := RunKnowledgeOperationsReplay(context.Background(), store, nil, KnowledgeOperationsReplayRequest{
		BookID:  "stale-quality",
		Action:  "evaluate_quality",
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("confirmed replay returned error: %v", err)
	}
	if result.Status != "succeeded" || !result.Mutated || result.NextAction != "ready_to_publish" {
		t.Fatalf("confirmed replay = %#v", result)
	}
}

func findKnowledgeOperationsHealthReviewItem(t *testing.T, items []KnowledgeOperationsHealthReviewItem, bookID string) KnowledgeOperationsHealthReviewItem {
	t.Helper()
	for _, item := range items {
		if item.BookID == bookID {
			return item
		}
	}
	t.Fatalf("health review item %s not found in %#v", bookID, items)
	return KnowledgeOperationsHealthReviewItem{}
}

func TestRunKnowledgeOperationsReplayRejectsDangerousActions(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	savePipelinePackage(t, store, "ready", "hash-ready")
	savePipelineAnalysis(t, store, "ready", "hash-ready")
	savePipelineQuality(t, store, "ready", "hash-ready", BookQualityPass)

	for _, action := range []string{"publish", "health_serving_promote", "feedback", "unknown"} {
		_, err := RunKnowledgeOperationsReplay(context.Background(), store, nil, KnowledgeOperationsReplayRequest{
			BookID:  "ready",
			Action:  action,
			Confirm: true,
		})
		if err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Fatalf("action %s error = %v", action, err)
		}
	}
}
