package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIngestSourceArticleIsIdempotentAndUpdatesKnowledge(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 9, 20, 0, 0, 0, time.UTC))
	root := t.TempDir()
	bookStore := NewBookKnowledgeStore(root)
	syncStore, err := newSourceSyncStore(root, clock.Now)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	subscription := createSourceIngestSubscription(t, syncStore)
	run := createRunningSourceIngestRun(t, syncStore, subscription.ID, "agent-a")
	ingestor := newSourceIngestService(bookStore, syncStore, clock.Now)

	content := "# 核心结论\n\n" + strings.Repeat("权威知识需要保留来源、上下文与更新时间，才能被下游系统可靠验证。", 90) +
		"\n\n## 验证方法\n\n" + strings.Repeat("每个结论都应指向可复核的原始文章，并记录稳定的来源标识。", 70)
	envelope := SourceArticleEnvelope{
		IdempotencyKey:  "idem-article-1-v1",
		SourceType:      "wcplus_wechat_article",
		SourceAccountID: "biz-med",
		SourceAccount:   "医学参考",
		SourceItemID:    "article-1",
		Title:           "如何构建可验证知识",
		Author:          "编辑部",
		SourceURL:       "https://mp.weixin.qq.com/s/article-1#fragment",
		PublishedAt:     "2026-07-09T19:30:00Z",
		Content:         content,
		ContentFormat:   "markdown",
		Metadata:        map[string]string{"category": "health"},
	}
	receipt, err := ingestor.IngestArticle(run.ID, "agent-a", envelope)
	if err != nil {
		t.Fatalf("ingest new article: %v", err)
	}
	if receipt.Outcome != SourceItemNew || receipt.TargetBookID == "" || receipt.ContentHash == "" {
		t.Fatalf("unexpected new receipt: %#v", receipt)
	}

	replayed, err := ingestor.IngestArticle(run.ID, "agent-a", envelope)
	if err != nil {
		t.Fatalf("replay article: %v", err)
	}
	if !reflect.DeepEqual(replayed, receipt) {
		t.Fatalf("replayed receipt = %#v, want %#v", replayed, receipt)
	}
	items, err := syncStore.ListRunItems(run.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("replay created duplicate items: %#v, err=%v", items, err)
	}

	pkg, err := bookStore.LoadPackage(receipt.TargetBookID)
	if err != nil {
		t.Fatalf("load ingested package: %v", err)
	}
	if pkg.Book.SourceType != envelope.SourceType || pkg.Book.SourceKey != envelope.SourceItemID || pkg.Book.SourceAccount != envelope.SourceAccount {
		t.Fatalf("missing book provenance: %#v", pkg.Book)
	}
	if pkg.Book.SourceHTML != "https://mp.weixin.qq.com/s/article-1" || pkg.Book.PublishedAt != envelope.PublishedAt || pkg.Book.ContentHash != receipt.ContentHash {
		t.Fatalf("unexpected book source metadata: %#v", pkg.Book)
	}
	if len(pkg.Chunks) < 3 || len(pkg.Citations) != len(pkg.Chunks) {
		t.Fatalf("chunks/citations = %d/%d, want bounded per-chunk citations", len(pkg.Chunks), len(pkg.Citations))
	}
	analysis, err := bookStore.LoadAnalysisManifest(receipt.TargetBookID)
	if err != nil {
		t.Fatalf("load pending analysis manifest: %v", err)
	}
	if analysis.Status != BookAnalysisPending || analysis.BookID != receipt.TargetBookID || analysis.ContentHash != receipt.ContentHash {
		t.Fatalf("pending analysis manifest = %#v", analysis)
	}
	for index, chunk := range pkg.Chunks {
		if len([]rune(chunk.Text)) > sourceArticleMaxChunkRunes {
			t.Fatalf("chunk %d has %d runes", index, len([]rune(chunk.Text)))
		}
		citation := pkg.Citations[index]
		if citation.ChunkID != chunk.ChunkID || citation.SourceHTML != pkg.Book.SourceHTML ||
			citation.SourceType != envelope.SourceType || citation.SourceAccount != envelope.SourceAccount ||
			citation.SourceItemKey != envelope.SourceItemID || citation.PublishedAt != envelope.PublishedAt {
			t.Fatalf("chunk %d citation mismatch: %#v", index, citation)
		}
	}
	createdAt := pkg.Book.CreatedAt
	updatedAt := pkg.Book.UpdatedAt
	results, err := bookStore.Search(BookKnowledgeSearchQuery{Query: "可复核 原始文章", Limit: 10})
	if err != nil || len(results) == 0 || results[0].BookID != receipt.TargetBookID {
		t.Fatalf("ingested article not searchable: %#v, err=%v", results, err)
	}

	if _, err := syncStore.CompleteRun(run.ID, "agent-a"); err != nil {
		t.Fatalf("complete first run: %v", err)
	}
	clock.Advance(time.Hour)
	unchangedRun := createRunningSourceIngestRun(t, syncStore, subscription.ID, "agent-a")
	unchanged := envelope
	unchanged.IdempotencyKey = "idem-article-1-v1-second-run"
	unchangedReceipt, err := ingestor.IngestArticle(unchangedRun.ID, "agent-a", unchanged)
	if err != nil {
		t.Fatalf("ingest unchanged article: %v", err)
	}
	if unchangedReceipt.Outcome != SourceItemSkipped || unchangedReceipt.TargetBookID != receipt.TargetBookID {
		t.Fatalf("unexpected unchanged receipt: %#v", unchangedReceipt)
	}
	unchangedPackage, err := bookStore.LoadPackage(receipt.TargetBookID)
	if err != nil {
		t.Fatalf("load unchanged package: %v", err)
	}
	if unchangedPackage.Book.UpdatedAt != updatedAt {
		t.Fatalf("skipped import changed updated_at: %q -> %q", updatedAt, unchangedPackage.Book.UpdatedAt)
	}
	unchangedAnalysis, err := bookStore.LoadAnalysisManifest(receipt.TargetBookID)
	if err != nil || unchangedAnalysis.UpdatedAt != analysis.UpdatedAt {
		t.Fatalf("skipped import changed analysis manifest: %#v, err=%v", unchangedAnalysis, err)
	}
	if _, err := syncStore.CompleteRun(unchangedRun.ID, "agent-a"); err != nil {
		t.Fatalf("complete unchanged run: %v", err)
	}

	clock.Advance(time.Hour)
	updatedRun := createRunningSourceIngestRun(t, syncStore, subscription.ID, "agent-a")
	updated := envelope
	updated.IdempotencyKey = "idem-article-1-v2"
	updated.Content += "\n\n## 新增证据\n\n本次更新补充了外部交叉验证结果。"
	updatedReceipt, err := ingestor.IngestArticle(updatedRun.ID, "agent-a", updated)
	if err != nil {
		t.Fatalf("ingest updated article: %v", err)
	}
	if updatedReceipt.Outcome != SourceItemUpdated || updatedReceipt.ContentHash == receipt.ContentHash {
		t.Fatalf("unexpected updated receipt: %#v", updatedReceipt)
	}
	updatedPackage, err := bookStore.LoadPackage(receipt.TargetBookID)
	if err != nil {
		t.Fatalf("load updated package: %v", err)
	}
	if updatedPackage.Book.CreatedAt != createdAt || updatedPackage.Book.UpdatedAt == updatedAt {
		t.Fatalf("update timestamps = created %q/%q updated %q/%q", createdAt, updatedPackage.Book.CreatedAt, updatedAt, updatedPackage.Book.UpdatedAt)
	}
	updatedAnalysis, err := bookStore.LoadAnalysisManifest(receipt.TargetBookID)
	if err != nil {
		t.Fatalf("load updated analysis manifest: %v", err)
	}
	if updatedAnalysis.Status != BookAnalysisPending || updatedAnalysis.ContentHash != updatedReceipt.ContentHash || updatedAnalysis.UpdatedAt == analysis.UpdatedAt {
		t.Fatalf("updated analysis manifest = %#v", updatedAnalysis)
	}
}

func TestIngestSourceArticleBackfillsMetadataWithoutRebuildingKnowledge(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	root := t.TempDir()
	bookStore := NewBookKnowledgeStore(root)
	syncStore, err := newSourceSyncStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer syncStore.Close()
	subscription := createSourceIngestSubscription(t, syncStore)
	ingestor := newSourceIngestService(bookStore, syncStore, clock.Now)
	envelope := SourceArticleEnvelope{
		IdempotencyKey:  "metadata-v1",
		SourceType:      "wechat_mp_article",
		SourceAccountID: "account-key",
		SourceAccount:   "Account",
		SourceItemID:    "article-metadata",
		Title:           "Metadata backfill",
		SourceURL:       "https://mp.weixin.qq.com/s/article-metadata",
		Content:         "# Metadata backfill\n\n" + strings.Repeat("This article has stable content for metadata-only ingestion. ", 20),
		ContentFormat:   "markdown",
	}
	firstRun := createRunningSourceIngestRun(t, syncStore, subscription.ID, "agent-a")
	first, err := ingestor.IngestArticle(firstRun.ID, "agent-a", envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := syncStore.CompleteRun(firstRun.ID, "agent-a"); err != nil {
		t.Fatal(err)
	}
	before, err := bookStore.LoadPackage(first.TargetBookID)
	if err != nil {
		t.Fatal(err)
	}
	analysisBefore, err := bookStore.LoadAnalysisManifest(first.TargetBookID)
	if err != nil {
		t.Fatal(err)
	}

	clock.Advance(time.Hour)
	secondRun := createRunningSourceIngestRun(t, syncStore, subscription.ID, "agent-a")
	backfill := envelope
	backfill.IdempotencyKey = "metadata-v2"
	backfill.PublishedAt = "2026-07-17T05:52:36Z"
	receipt, err := ingestor.IngestArticle(secondRun.ID, "agent-a", backfill)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome != SourceItemUpdated {
		t.Fatalf("outcome=%q", receipt.Outcome)
	}
	after, err := bookStore.LoadPackage(first.TargetBookID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Book.PublishedAt != backfill.PublishedAt || after.Book.UpdatedAt == before.Book.UpdatedAt {
		t.Fatalf("book before=%#v after=%#v", before.Book, after.Book)
	}
	if !reflect.DeepEqual(after.Chunks, before.Chunks) || !reflect.DeepEqual(after.Claims, before.Claims) {
		t.Fatal("metadata backfill rebuilt knowledge content")
	}
	for _, citation := range after.Citations {
		if citation.PublishedAt != backfill.PublishedAt {
			t.Fatalf("citation=%#v", citation)
		}
	}
	analysisAfter, err := bookStore.LoadAnalysisManifest(first.TargetBookID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(analysisAfter, analysisBefore) {
		t.Fatalf("analysis changed: before=%#v after=%#v", analysisBefore, analysisAfter)
	}
}

func TestIngestSourceArticleRejectsShortContentWithoutManifest(t *testing.T) {
	root := t.TempDir()
	bookStore := NewBookKnowledgeStore(root)
	syncStore, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	subscription := createSourceIngestSubscription(t, syncStore)
	run := createRunningSourceIngestRun(t, syncStore, subscription.ID, "agent-a")
	ingestor := NewSourceIngestService(bookStore, syncStore)

	_, err = ingestor.IngestArticle(run.ID, "agent-a", SourceArticleEnvelope{
		IdempotencyKey:  "idem-short",
		SourceType:      "wcplus_wechat_article",
		SourceAccountID: "biz-short",
		SourceAccount:   "短文测试",
		SourceItemID:    "article-short",
		Title:           "正文不完整",
		SourceURL:       "https://mp.weixin.qq.com/s/short",
		Content:         "只有几个字",
		ContentFormat:   "markdown",
	})
	if !errors.Is(err, ErrSourceArticleContentTooShort) {
		t.Fatalf("short content error = %v", err)
	}
	books, listErr := bookStore.ListBooks()
	if listErr != nil {
		t.Fatalf("list books: %v", listErr)
	}
	if len(books) != 0 {
		t.Fatalf("short content wrote books: %#v", books)
	}
	items, itemErr := syncStore.ListRunItems(run.ID)
	if itemErr != nil || len(items) != 1 || items[0].Outcome != SourceItemFailed {
		t.Fatalf("short content item = %#v, err=%v", items, itemErr)
	}
}

func TestIngestSourceArticleDoesNotAdvanceDocumentWhenPackageWriteFails(t *testing.T) {
	syncStore, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	subscription := createSourceIngestSubscription(t, syncStore)
	run := createRunningSourceIngestRun(t, syncStore, subscription.ID, "agent-a")
	blockedRoot := filepath.Join(t.TempDir(), "blocked-root")
	if err := os.WriteFile(blockedRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create blocked root: %v", err)
	}
	ingestor := NewSourceIngestService(NewBookKnowledgeStore(blockedRoot), syncStore)
	envelope := SourceArticleEnvelope{
		IdempotencyKey:  "idem-write-failure",
		SourceType:      "wcplus_wechat_article",
		SourceAccountID: "biz-failure",
		SourceAccount:   "写入失败测试",
		SourceItemID:    "article-write-failure",
		Title:           "无法写入的文章",
		SourceURL:       "https://mp.weixin.qq.com/s/write-failure",
		Content:         strings.Repeat("这是一段足够长、但目标目录无法写入的文章正文。", 10),
		ContentFormat:   "markdown",
	}
	if _, err := ingestor.IngestArticle(run.ID, "agent-a", envelope); err == nil {
		t.Fatalf("package write unexpectedly succeeded")
	}
	if _, found, err := syncStore.getSourceDocument(envelope.SourceType, envelope.SourceItemID); err != nil || found {
		t.Fatalf("source document advanced after write failure: found=%v err=%v", found, err)
	}
	items, err := syncStore.ListRunItems(run.ID)
	if err != nil || len(items) != 1 || items[0].Outcome != SourceItemFailed {
		t.Fatalf("write failure item = %#v, err=%v", items, err)
	}
}

func TestSourceDocumentEnvelopeConvertsToSourceArticleEnvelope(t *testing.T) {
	document := SourceDocumentEnvelope{
		IdempotencyKey:   " doc-idem ",
		SourceType:       " wechat_mp_article ",
		SourceAccountKey: " biz-evidence ",
		SourceAccount:    " 证据参考 ",
		SourceItemKey:    " article-99 ",
		Title:            " 证据加工流程 ",
		Author:           " 作者 ",
		SourceURL:        "https://mp.weixin.qq.com/s/article-99#rd",
		PublishedAt:      "2026-07-16T10:00:00Z",
		Content:          "# 标题\n\n" + strings.Repeat("知识加工需要保留来源、授权、版本与证据链。", 6),
		ContentFormat:    "markdown",
		LicenseScope:     SourceLicenseScopePersonalUse,
		Metadata:         map[string]string{" topic ": " evidence "},
	}

	envelope, contentHash, err := SourceArticleEnvelopeFromDocument(document)
	if err != nil {
		t.Fatalf("convert document: %v", err)
	}
	if envelope.IdempotencyKey != "doc-idem" ||
		envelope.SourceType != "wechat_mp_article" ||
		envelope.SourceAccountID != "biz-evidence" ||
		envelope.SourceAccount != "证据参考" ||
		envelope.SourceItemID != "article-99" ||
		envelope.Title != "证据加工流程" ||
		envelope.Author != "作者" ||
		envelope.SourceURL != "https://mp.weixin.qq.com/s/article-99" ||
		envelope.ContentFormat != "markdown" {
		t.Fatalf("unexpected article envelope: %#v", envelope)
	}
	if envelope.Metadata["topic"] != "evidence" || envelope.Metadata["license_scope"] != SourceLicenseScopePersonalUse {
		t.Fatalf("metadata not preserved: %#v", envelope.Metadata)
	}
	_, normalizedHash, err := normalizeSourceArticleEnvelope(envelope)
	if err != nil {
		t.Fatalf("article envelope should remain ingestable: %v", err)
	}
	if contentHash == "" || contentHash != normalizedHash {
		t.Fatalf("content hash mismatch: document=%q article=%q", contentHash, normalizedHash)
	}
}

func createSourceIngestSubscription(t *testing.T, store *SourceSyncStore) SourceSubscription {
	t.Helper()
	subscription, err := store.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-med",
		SourceAccount:    "医学参考",
		AgentID:          "agent-a",
		Schedule:         "manual",
		Operation:        "sync_content",
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return subscription
}

func createRunningSourceIngestRun(t *testing.T, store *SourceSyncStore, subscriptionID, agentID string) SourceSyncRun {
	t.Helper()
	run, err := store.CreateRun(subscriptionID, "sync_content")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	leased, err := store.LeaseNextRun(agentID, []string{"sync_content"}, 5*time.Minute)
	if err != nil {
		t.Fatalf("lease run: %v", err)
	}
	if leased == nil || leased.ID != run.ID {
		t.Fatalf("leased run = %#v, want %s", leased, run.ID)
	}
	running, err := store.StartRun(run.ID, agentID)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	return running
}
