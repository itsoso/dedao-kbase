package app

import (
	"testing"
	"time"
)

func TestKnowledgeRebuildPlanDetectsChangedSourceAndConsumerImpact(t *testing.T) {
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatalf("quality: %v", err)
	}
	release, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	catalog, err := NewKnowledgeCatalogStore(store.Root(), func() time.Time {
		return time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	defer catalog.Close()
	if _, err := catalog.SaveDeliveryReceipt(DeliveryReceipt{
		Consumer:            "health-consumer",
		ReleaseID:           release.ReleaseID,
		IdempotencyKey:      "health-consumer:" + release.ReleaseID + ":1",
		Disposition:         "imported",
		ImportedFingerprint: "sha256:imported",
	}, nil); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	pkg, err := store.LoadPackage("42")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	pkg.Book.ContentHash = "content-hash-42-v2"
	pkg.Book.UpdatedAt = "2026-07-16T11:00:00Z"
	if err := store.SavePackage(*pkg); err != nil {
		t.Fatalf("save updated package: %v", err)
	}

	plan, err := BuildKnowledgeRebuildPlan(store, catalog, KnowledgeRebuildPlanQuery{BookID: "42"})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.SchemaVersion != KnowledgeRebuildPlanSchemaVersion || len(plan.Items) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	item := plan.Items[0]
	if item.BookID != "42" || item.ReleaseID != release.ReleaseID || !item.ContentChanged || item.ConsumerReceiptCount != 1 {
		t.Fatalf("item = %#v", item)
	}
	wantActions := []string{KnowledgeRebuildActionRebuild, KnowledgeRebuildActionReevaluate, KnowledgeRebuildActionRepublish, KnowledgeRebuildActionNotifyConsumers}
	for _, action := range wantActions {
		if !containsString(item.Actions, action) {
			t.Fatalf("actions %v missing %s", item.Actions, action)
		}
	}
}

func TestKnowledgeRebuildPlanReturnsNoopForCurrentRelease(t *testing.T) {
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatalf("quality: %v", err)
	}
	release, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	catalog, err := NewKnowledgeCatalogStore(store.Root(), nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	defer catalog.Close()

	plan, err := BuildKnowledgeRebuildPlan(store, catalog, KnowledgeRebuildPlanQuery{BookID: "42"})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].ReleaseID != release.ReleaseID || !containsString(plan.Items[0].Actions, KnowledgeRebuildActionNoop) {
		t.Fatalf("noop plan = %#v", plan)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
