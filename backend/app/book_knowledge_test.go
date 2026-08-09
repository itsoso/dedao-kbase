package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBookKnowledgeContentHashIsStableAndTracksDurableContent(t *testing.T) {
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = ""
	pkg.Book.CreatedAt = "2026-01-01T00:00:00Z"
	pkg.Book.UpdatedAt = "2026-01-01T00:00:00Z"

	first, err := BookKnowledgeContentHash(pkg)
	if err != nil {
		t.Fatalf("BookKnowledgeContentHash returned error: %v", err)
	}
	if !strings.HasPrefix(first, "sha256:") || len(first) != len("sha256:")+64 {
		t.Fatalf("content hash = %q", first)
	}

	reordered := pkg
	reordered.Book.CreatedAt = "2027-02-02T00:00:00Z"
	reordered.Book.UpdatedAt = "2027-02-02T00:00:00Z"
	slices.Reverse(reordered.Chapters)
	slices.Reverse(reordered.Chunks)
	slices.Reverse(reordered.Claims)
	slices.Reverse(reordered.Citations)
	second, err := BookKnowledgeContentHash(reordered)
	if err != nil {
		t.Fatalf("BookKnowledgeContentHash reordered returned error: %v", err)
	}
	if second != first {
		t.Fatalf("stable hash changed: first=%s second=%s", first, second)
	}

	changed := pkg
	changed.Chunks = append([]BookKnowledgeChunk(nil), pkg.Chunks...)
	changed.Chunks[0].Text += " changed"
	third, err := BookKnowledgeContentHash(changed)
	if err != nil {
		t.Fatalf("BookKnowledgeContentHash changed returned error: %v", err)
	}
	if third == first {
		t.Fatal("durable content change did not change hash")
	}
}

func TestSavePackageAssignsMissingContentHash(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = ""
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	loaded, err := store.LoadPackage(pkg.Book.BookID)
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if !strings.HasPrefix(loaded.Book.ContentHash, "sha256:") {
		t.Fatalf("content hash = %q", loaded.Book.ContentHash)
	}
}

func TestSavePackageContextCancellationAtPublishGateLeavesNoPartialPackage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	ctx, cancel := context.WithCancel(context.Background())
	reachedGate := make(chan struct{})
	releaseGate := make(chan struct{})
	store.beforePackagePublish = func() {
		close(reachedGate)
		<-releaseGate
	}
	done := make(chan error, 1)
	go func() { done <- store.SavePackageContext(ctx, pkg) }()
	select {
	case <-reachedGate:
	case <-time.After(2 * time.Second):
		close(releaseGate)
		t.Fatal("save did not reach package publish gate")
	}
	cancel()
	close(releaseGate)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("SavePackageContext error=%v, want context canceled", err)
	}
	if _, err := os.Stat(store.ManifestPath()); !os.IsNotExist(err) {
		t.Fatalf("root manifest was partially published: %v", err)
	}
	if _, err := os.Stat(store.BookDir(pkg.Book.BookID)); !os.IsNotExist(err) {
		t.Fatalf("book directory was partially published: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(store.Root(), ".book-package-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("staging package leaked: %v, err=%v", matches, err)
	}
}

func TestSavePackageContextCancellationAfterPublishGateCommitsWholePackage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	ctx, cancel := context.WithCancel(context.Background())
	store.afterPackagePublishGate = cancel
	if err := store.SavePackageContext(ctx, pkg); err != nil {
		t.Fatalf("SavePackageContext after commit decision: %v", err)
	}
	loaded, err := store.LoadPackage(pkg.Book.BookID)
	if err != nil {
		t.Fatalf("committed package cannot be loaded: %v", err)
	}
	if loaded.Book.BookID != pkg.Book.BookID || len(loaded.Chunks) != len(pkg.Chunks) {
		t.Fatalf("partial committed package: %#v", loaded)
	}
	books, err := store.ListBooks()
	if err != nil || len(books) != 1 || books[0].BookID != pkg.Book.BookID {
		t.Fatalf("root manifest did not commit with package: books=%#v err=%v", books, err)
	}
}

func TestSavePackageContextCancellationPreservesExistingWholePackage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	original := sampleBookKnowledgePackageForExport()
	if err := store.SavePackage(original); err != nil {
		t.Fatal(err)
	}
	updated := original
	updated.Book.Title = "replacement title"
	updated.Book.ContentHash = ""
	updated.Chunks = append([]BookKnowledgeChunk(nil), original.Chunks...)
	updated.Chunks[0].Text = "replacement chunk"
	ctx, cancel := context.WithCancel(context.Background())
	store.beforePackagePublish = cancel
	if err := store.SavePackageContext(ctx, updated); !errors.Is(err, context.Canceled) {
		t.Fatalf("SavePackageContext error=%v, want context canceled", err)
	}
	loaded, err := store.LoadPackage(original.Book.BookID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Book.Title != original.Book.Title || loaded.Chunks[0].Text != original.Chunks[0].Text {
		t.Fatalf("existing package was partially replaced: %#v", loaded)
	}
}

func TestSavePackageContextPreservesDerivedBookArtifacts(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	derivedPath := filepath.Join(store.BookDir(pkg.Book.BookID), "quality_report.json")
	if err := os.WriteFile(derivedPath, []byte("derived"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg.Book.Title = "updated"
	pkg.Book.ContentHash = ""
	if err := store.SavePackageContext(context.Background(), pkg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(derivedPath)
	if err != nil || string(data) != "derived" {
		t.Fatalf("derived artifact=%q err=%v", data, err)
	}
}

func TestBookKnowledgeStorePaths(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)

	assertPath(t, store.ManifestPath(), filepath.Join(root, "manifest.json"))
	assertPath(t, store.BookDir("book-1"), filepath.Join(root, "books", "book-1"))
	assertPath(t, store.BookManifestPath("book-1"), filepath.Join(root, "books", "book-1", "manifest.json"))
	assertPath(t, store.BookJSONLPath("book-1", "chapters"), filepath.Join(root, "books", "book-1", "chapters.jsonl"))
}

func TestBookKnowledgePackageRoundTrip(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:        "42",
			DedaoID:       42,
			EnID:          "enid-42",
			Title:         "42_测试书_作者",
			Author:        "作者",
			SourceHTML:    "/tmp/book.html",
			Status:        "draft",
			Extractor:     "dedao-gui-fallback",
			SourceType:    "wcplus_wechat_article",
			SourceKey:     "article-42",
			SourceAccount: "测试账号",
			PublishedAt:   "2026-07-09T12:00:00Z",
			ContentHash:   "hash-42",
		},
		Chapters: []BookKnowledgeChapter{
			{
				ChapterID: "42-chapter-1",
				BookID:    "42",
				Order:     1,
				Title:     "第一章",
				Summary:   "第一章摘要",
				ChunkIDs:  []string{"42-chunk-1"},
			},
		},
		Chunks: []BookKnowledgeChunk{
			{
				ChunkID:   "42-chunk-1",
				BookID:    "42",
				ChapterID: "42-chapter-1",
				Order:     1,
				Text:      "这是一段用于测试的内容。",
				Tokens:    12,
			},
		},
		Claims: []BookKnowledgeClaim{
			{
				ClaimID:       "42-claim-1",
				BookID:        "42",
				ChapterID:     "42-chapter-1",
				Title:         "第一章",
				Summary:       "这是一条草稿 claim。",
				Body:          "这是一条草稿 claim。",
				EvidenceLevel: "D",
				Confidence:    0.4,
				ReviewStatus:  "draft",
				Citations:     []string{"42-citation-1"},
			},
		},
		Citations: []BookKnowledgeCitation{
			{
				CitationID:    "42-citation-1",
				BookID:        "42",
				ChapterID:     "42-chapter-1",
				ChunkID:       "42-chunk-1",
				SourceHTML:    "/tmp/book.html",
				Anchor:        "第一章",
				Note:          "自动提取",
				SourceType:    "wcplus_wechat_article",
				SourceAccount: "测试账号",
				SourceItemKey: "article-42",
				PublishedAt:   "2026-07-09T12:00:00Z",
			},
		},
	}

	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	got, err := store.LoadPackage("42")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if !reflect.DeepEqual(got.Book, pkg.Book) {
		t.Fatalf("book = %#v, want %#v", got.Book, pkg.Book)
	}
	if !reflect.DeepEqual(got.Chapters, pkg.Chapters) {
		t.Fatalf("chapters = %#v, want %#v", got.Chapters, pkg.Chapters)
	}
	if !reflect.DeepEqual(got.Chunks, pkg.Chunks) {
		t.Fatalf("chunks = %#v, want %#v", got.Chunks, pkg.Chunks)
	}
	if !reflect.DeepEqual(got.Claims, pkg.Claims) {
		t.Fatalf("claims = %#v, want %#v", got.Claims, pkg.Claims)
	}
	if !reflect.DeepEqual(got.Citations, pkg.Citations) {
		t.Fatalf("citations = %#v, want %#v", got.Citations, pkg.Citations)
	}

	books, err := store.ListBooks()
	if err != nil {
		t.Fatalf("ListBooks returned error: %v", err)
	}
	if len(books) != 1 || books[0].BookID != "42" {
		t.Fatalf("books = %#v, want one saved book", books)
	}
}

func TestBookKnowledgeWritesPreserveExistingFilesOnEncodeFailure(t *testing.T) {
	root := t.TempDir()
	jsonPath := filepath.Join(root, "manifest.json")
	if err := writeJSONFile(jsonPath, map[string]any{"version": "stable"}); err != nil {
		t.Fatalf("write initial JSON: %v", err)
	}
	jsonBefore, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read initial JSON: %v", err)
	}
	if err := writeJSONFile(jsonPath, map[string]any{"invalid": math.Inf(1)}); err == nil {
		t.Fatal("invalid JSON write unexpectedly succeeded")
	}
	jsonAfter, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read JSON after failure: %v", err)
	}
	if !bytes.Equal(jsonAfter, jsonBefore) {
		t.Fatalf("failed JSON write replaced existing data: before=%q after=%q", jsonBefore, jsonAfter)
	}

	jsonlPath := filepath.Join(root, "claims.jsonl")
	if err := writeJSONLFile(jsonlPath, []any{"stable"}); err != nil {
		t.Fatalf("write initial JSONL: %v", err)
	}
	jsonlBefore, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read initial JSONL: %v", err)
	}
	if err := writeJSONLFile(jsonlPath, []any{"new", math.Inf(1)}); err == nil {
		t.Fatal("invalid JSONL write unexpectedly succeeded")
	}
	jsonlAfter, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read JSONL after failure: %v", err)
	}
	if !bytes.Equal(jsonlAfter, jsonlBefore) {
		t.Fatalf("failed JSONL write replaced existing data: before=%q after=%q", jsonlBefore, jsonlAfter)
	}
}

func TestBookKnowledgeListOrdersPublishedArticlesNewestFirst(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	for _, book := range []BookKnowledgeBook{
		{BookID: "newer", Title: "Newer", SourceType: "wechat_mp_article", PublishedAt: "2026-07-20T08:00:00Z", UpdatedAt: "2026-07-20T09:00:00Z"},
		{BookID: "older-imported-later", Title: "Older", SourceType: "wechat_mp_article", PublishedAt: "2026-07-01T08:00:00Z", UpdatedAt: "2026-07-21T09:00:00Z"},
	} {
		if err := store.SavePackage(BookKnowledgePackage{Book: book}); err != nil {
			t.Fatal(err)
		}
	}
	books, err := store.ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 || books[0].BookID != "newer" || books[1].BookID != "older-imported-later" {
		t.Fatalf("books=%#v", books)
	}
}

func TestBookKnowledgeStoreSerializesConcurrentManifestUpdates(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	const count = 40
	start := make(chan struct{})
	errorsByBook := make(chan error, count)
	var wait sync.WaitGroup
	for index := 0; index < count; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			bookID := fmt.Sprintf("concurrent-%02d", index)
			errorsByBook <- store.SavePackage(BookKnowledgePackage{
				Book: BookKnowledgeBook{BookID: bookID, Title: bookID, Status: "ready"},
			})
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByBook)
	for err := range errorsByBook {
		if err != nil {
			t.Fatalf("concurrent SavePackage: %v", err)
		}
	}
	books, err := store.ListBooks()
	if err != nil {
		t.Fatalf("ListBooks after concurrent saves: %v", err)
	}
	if len(books) != count {
		t.Fatalf("manifest contains %d books, want %d", len(books), count)
	}
}

func TestBookKnowledgeSearch(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:    "42",
			Title:     "42_量化分析_作者",
			Status:    "draft",
			Extractor: "dedao-gui-fallback",
		},
		Chapters: []BookKnowledgeChapter{
			{ChapterID: "42-chapter-1", BookID: "42", Order: 1, Title: "趋势"},
		},
		Chunks: []BookKnowledgeChunk{
			{ChunkID: "42-chunk-1", BookID: "42", ChapterID: "42-chapter-1", Order: 1, Text: "MACD 背离需要先定义趋势过滤。"},
			{ChunkID: "42-chunk-2", BookID: "42", ChapterID: "42-chapter-1", Order: 2, Text: "仓位管理不能依赖单一信号。"},
		},
		Claims: []BookKnowledgeClaim{
			{ClaimID: "42-claim-1", BookID: "42", ChapterID: "42-chapter-1", Title: "趋势过滤", Summary: "MACD 规则需要趋势过滤。", ReviewStatus: "draft"},
		},
	}
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	results, err := store.Search(BookKnowledgeSearchQuery{Query: "MACD 趋势", Limit: 10})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %#v, want chunk and claim matches", results)
	}
	if results[0].BookID != "42" || results[0].Kind == "" || results[0].Snippet == "" {
		t.Fatalf("first result missing fields: %#v", results[0])
	}
}

func assertPath(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
