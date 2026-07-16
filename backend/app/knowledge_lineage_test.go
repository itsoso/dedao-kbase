package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestKnowledgeLineageReturnsReleaseAndBookProvenance(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	if err := store.SavePackage(BookKnowledgePackage{
		Book:      BookKnowledgeBook{BookID: "book-a", Title: "Book A", SourceType: "wechat_mp_article", SourceKey: "article-a", ContentHash: "hash-a"},
		Citations: []BookKnowledgeCitation{{CitationID: "citation-a", BookID: "book-a", ChunkID: "chunk-a", SourceType: "wechat_mp_article", SourceItemKey: "article-a"}},
	}); err != nil {
		t.Fatal(err)
	}
	saveFeedRelease(t, store, KnowledgeRelease{ReleaseID: "release-a", BookID: "book-a", ContentHash: "hash-a", UsagePolicy: BookUsageEvidenceOnly, CreatedAt: "2026-07-14T00:00:00Z"})
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	release := requestKBase(handler, http.MethodGet, "/api/knowledge/lineage/release-a", "secret-token")
	if release.Code != http.StatusOK || !strings.Contains(release.Body.String(), `"object_kind":"release"`) || !strings.Contains(release.Body.String(), `"release_id":"release-a"`) || !strings.Contains(release.Body.String(), `"citations":["citation-a"]`) {
		t.Fatalf("release lineage status=%d body=%s", release.Code, release.Body.String())
	}
	book := requestKBase(handler, http.MethodGet, "/api/knowledge/lineage/book-a", "secret-token")
	if book.Code != http.StatusOK || !strings.Contains(book.Body.String(), `"object_kind":"book"`) || !strings.Contains(book.Body.String(), `"source_item_key":"article-a"`) {
		t.Fatalf("book lineage status=%d body=%s", book.Code, book.Body.String())
	}
}

func TestKnowledgeLineageRequiresAuthAndHandlesMissing(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "secret-token"})
	unauthorized := requestKBase(handler, http.MethodGet, "/api/knowledge/lineage/missing", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	missing := requestKBase(handler, http.MethodGet, "/api/knowledge/lineage/missing", "secret-token")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodPost, "/api/knowledge/lineage/missing", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}
