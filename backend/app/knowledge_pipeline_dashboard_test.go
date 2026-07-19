package app

import (
	"context"
	"testing"
)

func TestKnowledgePipelineDashboardSummarizesStagesAndNextActions(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	savePipelinePackage(t, store, "needs-analysis", "hash-analysis")
	savePipelinePackage(t, store, "needs-quality", "hash-quality")
	savePipelineAnalysis(t, store, "needs-quality", "hash-quality")
	savePipelinePackage(t, store, "ready-release", "hash-release")
	savePipelineAnalysis(t, store, "ready-release", "hash-release")
	savePipelineQuality(t, store, "ready-release", "hash-release", BookQualityPass)

	dashboard, err := BuildKnowledgePipelineDashboard(store, 20)
	if err != nil {
		t.Fatalf("BuildKnowledgePipelineDashboard returned error: %v", err)
	}
	if dashboard.Summary.Total != 3 || dashboard.Summary.NeedsAnalysis != 1 || dashboard.Summary.NeedsQuality != 1 || dashboard.Summary.ReadyToPublish != 1 {
		t.Fatalf("summary = %#v", dashboard.Summary)
	}
	assertPipelineDashboardItem(t, dashboard.Items, "needs-analysis", "needs_analysis")
	assertPipelineDashboardItem(t, dashboard.Items, "needs-quality", "needs_quality")
	assertPipelineDashboardItem(t, dashboard.Items, "ready-release", "ready_to_publish")
}

func TestRunKnowledgePipelineAutomationProcessesEligibleItems(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	savePipelinePackage(t, store, "needs-analysis", "hash-analysis")
	savePipelinePackage(t, store, "needs-quality", "hash-quality")
	savePipelineAnalysis(t, store, "needs-quality", "hash-quality")
	calls := 0
	generator := func(_ context.Context, current *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		calls++
		savePipelineAnalysis(t, current, request.BookID, "hash-analysis")
		return current.LoadAnalysisManifest(request.BookID)
	}

	result, err := RunKnowledgePipelineAutomation(context.Background(), store, generator, KnowledgePipelineAutomationRequest{Limit: 10})
	if err != nil {
		t.Fatalf("RunKnowledgePipelineAutomation returned error: %v", err)
	}
	if calls != 1 || result.Analyzed != 1 || result.Qualified != 2 || result.Processed != 2 {
		t.Fatalf("result=%#v calls=%d", result, calls)
	}
	after, err := BuildKnowledgePipelineDashboard(store, 20)
	if err != nil {
		t.Fatalf("BuildKnowledgePipelineDashboard after run returned error: %v", err)
	}
	if after.Summary.ReadyToPublish != 2 || after.Summary.NeedsAnalysis != 0 || after.Summary.NeedsQuality != 0 {
		t.Fatalf("after summary = %#v", after.Summary)
	}
}

func TestRunKnowledgePipelineAutomationDryRunDoesNotWrite(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	savePipelinePackage(t, store, "needs-analysis", "hash-analysis")
	calls := 0
	generator := func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
		calls++
		return nil, nil
	}

	result, err := RunKnowledgePipelineAutomation(context.Background(), store, generator, KnowledgePipelineAutomationRequest{Limit: 10, DryRun: true})
	if err != nil {
		t.Fatalf("RunKnowledgePipelineAutomation dry run returned error: %v", err)
	}
	if calls != 0 || result.DryRun != true || result.Eligible != 1 || result.Processed != 0 {
		t.Fatalf("dry run result=%#v calls=%d", result, calls)
	}
	if _, err := store.LoadAnalysisManifest("needs-analysis"); err == nil {
		t.Fatal("dry run wrote an analysis manifest")
	}
}

func assertPipelineDashboardItem(t *testing.T, items []KnowledgePipelineDashboardItem, bookID, nextAction string) {
	t.Helper()
	for _, item := range items {
		if item.BookID == bookID {
			if item.NextAction != nextAction {
				t.Fatalf("item %s next_action=%s want %s: %#v", bookID, item.NextAction, nextAction, item)
			}
			return
		}
	}
	t.Fatalf("missing dashboard item %s in %#v", bookID, items)
}
