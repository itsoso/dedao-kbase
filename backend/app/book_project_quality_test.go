package app

import "testing"

func TestProjectReviewItemsSkipRejectedQualityBooks(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	usable := sampleQualityBookKnowledgePackage("usable-project-book")
	if err := store.SavePackage(usable); err != nil {
		t.Fatalf("SavePackage usable returned error: %v", err)
	}
	rejected := sampleQualityBookKnowledgePackage("rejected-project-book")
	rejected.Chapters = nil
	rejected.Chunks = nil
	if err := store.SavePackage(rejected); err != nil {
		t.Fatalf("SavePackage rejected returned error: %v", err)
	}

	queue, err := store.BuildProjectReviewQueue(BookKnowledgeProjectHealth, 0)
	if err != nil {
		t.Fatalf("BuildProjectReviewQueue returned error: %v", err)
	}
	for _, item := range queue.Items {
		if item.BookID == "rejected-project-book" {
			t.Fatalf("rejected book entered review queue: %#v", item)
		}
	}
	if queue.Total != 1 || len(queue.Items) != 1 || queue.Items[0].BookID != "usable-project-book" {
		t.Fatalf("queue = %#v, want only usable book", queue)
	}
}
