package app

import (
	"testing"
	"time"
)

func TestKnowledgeCatalogRecordsStableSourceAndContentVersions(t *testing.T) {
	root := t.TempDir()
	clock := newSourceSyncTestClock(time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC))
	catalog, err := NewKnowledgeCatalogStore(root, clock.Now)
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}
	envelope := SourceArticleEnvelope{
		SourceType:      "wcplus_wechat_article",
		SourceAccountID: "biz-health",
		SourceAccount:   "Health Source",
		SourceItemID:    "article-1",
		Title:           "Versioned evidence",
		SourceURL:       "https://mp.weixin.qq.com/s/article-1",
		PublishedAt:     "2026-07-14T00:00:00Z",
	}
	first, err := catalog.RecordContentVersion(envelope, "hash-v1", "book-1", "books/book-1/manifest.json")
	if err != nil {
		t.Fatalf("record first version: %v", err)
	}
	replayed, err := catalog.RecordContentVersion(envelope, "hash-v1", "book-1", "books/book-1/manifest.json")
	if err != nil {
		t.Fatalf("record duplicate version: %v", err)
	}
	if replayed.Source.SourceID != first.Source.SourceID || replayed.Version.ContentVersionID != first.Version.ContentVersionID {
		t.Fatalf("duplicate changed ids: first=%#v replayed=%#v", first, replayed)
	}
	if replayed.Version.PredecessorVersionID != "" {
		t.Fatalf("duplicate received predecessor: %#v", replayed.Version)
	}

	clock.Advance(time.Hour)
	updated, err := catalog.RecordContentVersion(envelope, "hash-v2", "book-1", "books/book-1/manifest.json")
	if err != nil {
		t.Fatalf("record updated version: %v", err)
	}
	if updated.Source.SourceID != first.Source.SourceID {
		t.Fatalf("source identity changed: %s -> %s", first.Source.SourceID, updated.Source.SourceID)
	}
	if updated.Version.ContentVersionID == first.Version.ContentVersionID || updated.Version.PredecessorVersionID != first.Version.ContentVersionID {
		t.Fatalf("updated version linkage = %#v, first=%#v", updated.Version, first.Version)
	}
	current, err := catalog.CurrentContentVersion(first.Source.SourceID)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if current.ContentHash != "hash-v2" || !current.IsCurrent {
		t.Fatalf("current version = %#v", current)
	}
	versions, err := catalog.ListContentVersions(first.Source.SourceID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions = %#v err=%v", versions, err)
	}
}

func TestKnowledgeCatalogGroupsProbableDuplicatesAndRebuildsFromPackages(t *testing.T) {
	root := t.TempDir()
	catalog, err := NewKnowledgeCatalogStore(root, nil)
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}
	sharedHash := "hash-shared"
	firstEnvelope := SourceArticleEnvelope{SourceType: "wcplus_wechat_article", SourceAccountID: "biz-a", SourceItemID: "article-a", Title: "A", SourceURL: "https://mp.weixin.qq.com/s/a"}
	secondEnvelope := SourceArticleEnvelope{SourceType: "wechat_mp_article", SourceAccountID: "biz-b", SourceItemID: "article-b", Title: "B", SourceURL: "https://mp.weixin.qq.com/s/b"}
	first, err := catalog.RecordContentVersion(firstEnvelope, sharedHash, "book-a", "books/book-a/manifest.json")
	if err != nil {
		t.Fatalf("record first: %v", err)
	}
	second, err := catalog.RecordContentVersion(secondEnvelope, sharedHash, "book-b", "books/book-b/manifest.json")
	if err != nil {
		t.Fatalf("record duplicate: %v", err)
	}
	if first.DuplicateGroupID == "" || first.DuplicateGroupID != second.DuplicateGroupID {
		t.Fatalf("duplicate groups = %q / %q", first.DuplicateGroupID, second.DuplicateGroupID)
	}

	bookStore := NewBookKnowledgeStore(root)
	for _, book := range []BookKnowledgeBook{
		{BookID: "book-a", Title: "A", SourceType: firstEnvelope.SourceType, SourceKey: firstEnvelope.SourceItemID, SourceAccount: "A", SourceHTML: firstEnvelope.SourceURL, ContentHash: sharedHash},
		{BookID: "book-b", Title: "B", SourceType: secondEnvelope.SourceType, SourceKey: secondEnvelope.SourceItemID, SourceAccount: "B", SourceHTML: secondEnvelope.SourceURL, ContentHash: sharedHash},
	} {
		if err := bookStore.SavePackage(BookKnowledgePackage{Book: book}); err != nil {
			t.Fatalf("save package %s: %v", book.BookID, err)
		}
	}
	rebuilt, err := RebuildKnowledgeCatalog(root, bookStore, nil)
	if err != nil {
		t.Fatalf("rebuild catalog: %v", err)
	}
	rebuiltFirst, err := rebuilt.FindSourceVersion(firstEnvelope.SourceType, firstEnvelope.SourceItemID)
	if err != nil {
		t.Fatalf("find rebuilt first: %v", err)
	}
	if rebuiltFirst.Version.ContentHash != sharedHash || rebuiltFirst.DuplicateGroupID == "" {
		t.Fatalf("rebuilt first = %#v", rebuiltFirst)
	}
	rebuiltSecond, err := rebuilt.FindSourceVersion(secondEnvelope.SourceType, secondEnvelope.SourceItemID)
	if err != nil {
		t.Fatalf("find rebuilt second: %v", err)
	}
	if rebuiltSecond.DuplicateGroupID != rebuiltFirst.DuplicateGroupID {
		t.Fatalf("rebuilt duplicate groups = %q/%q", rebuiltFirst.DuplicateGroupID, rebuiltSecond.DuplicateGroupID)
	}
}

func TestKnowledgeCatalogListsOnlyCurrentVersionsForExactSourceAccount(t *testing.T) {
	root := t.TempDir()
	clock := newSourceSyncTestClock(time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC))
	catalog, err := NewKnowledgeCatalogStore(root, clock.Now)
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}
	t.Cleanup(func() { _ = catalog.Close() })

	record := func(accountKey, itemKey, hash string) {
		t.Helper()
		_, err := catalog.RecordContentVersion(SourceArticleEnvelope{
			SourceType:      "wechat_mp_article",
			SourceAccountID: accountKey,
			SourceAccount:   "Fixture account",
			SourceItemID:    itemKey,
			SourceURL:       "https://mp.weixin.qq.com/s/" + itemKey,
		}, hash, "book-"+itemKey, "books/book-"+itemKey+"/manifest.json")
		if err != nil {
			t.Fatalf("record %s: %v", itemKey, err)
		}
	}

	record("account-a", "article-a", "hash-a1")
	clock.Advance(time.Minute)
	record("account-a", "article-a", "hash-a2")
	record("account-a", "article-b", "hash-b")
	record("account-b", "article-c", "hash-c")
	record("account-a", "article-d", "hash-d")

	records, err := catalog.ListCurrentContentVersionsBySourceAccount("wechat_mp_article", "account-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %#v", records)
	}
	if records[0].Source.SourceItemKey != "article-a" || records[0].Version.ContentHash != "hash-a2" {
		t.Fatalf("current article-a = %#v", records[0])
	}
	for _, record := range records {
		if record.Source.SourceAccountKey != "account-a" || !record.Version.IsCurrent {
			t.Fatalf("out-of-scope record = %#v", record)
		}
	}
	limited, err := catalog.ListCurrentContentVersionsBySourceAccount("wechat_mp_article", "account-a", 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limited records = %#v err=%v", limited, err)
	}
	if _, err := catalog.ListCurrentContentVersionsBySourceAccount("wechat_mp_article", "account-a", 0); err == nil {
		t.Fatal("zero limit unexpectedly accepted")
	}
}
