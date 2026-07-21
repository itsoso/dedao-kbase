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
