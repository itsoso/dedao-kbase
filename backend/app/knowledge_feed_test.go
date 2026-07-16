package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestKnowledgeFeedReturnsCursorPageAndFilters(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveFeedRelease(t, store, KnowledgeRelease{ReleaseID: "release-a", BookID: "book-a", ContentHash: "hash-a", UsagePolicy: BookUsageStandard, CreatedAt: "2026-07-14T00:00:00Z", Book: BookKnowledgeBook{SourceType: "dedao_ebook"}})
	saveFeedRelease(t, store, KnowledgeRelease{ReleaseID: "release-b", BookID: "book-b", ContentHash: "hash-b", UsagePolicy: BookUsageEvidenceOnly, CreatedAt: "2026-07-14T00:01:00Z", Book: BookKnowledgeBook{SourceType: "wechat_mp_article"}})
	saveFeedRelease(t, store, KnowledgeRelease{ReleaseID: "release-c", BookID: "book-c", ContentHash: "hash-c", UsagePolicy: BookUsageEvidenceOnly, CreatedAt: "2026-07-14T00:02:00Z", Book: BookKnowledgeBook{SourceType: "wechat_mp_article"}})
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	page := requestKBase(handler, http.MethodGet, "/api/knowledge/feed?limit=1&policy=evidence_only&source=wechat_mp_article", "secret-token")
	if page.Code != http.StatusOK {
		t.Fatalf("feed status=%d body=%s", page.Code, page.Body.String())
	}
	var feed KnowledgeFeedPage
	if err := json.Unmarshal(page.Body.Bytes(), &feed); err != nil {
		t.Fatalf("decode feed: %v", err)
	}
	if feed.SchemaVersion != KnowledgeFeedSchemaVersion || len(feed.Items) != 1 || feed.Items[0].ReleaseID != "release-b" || !feed.HasMore || feed.NextCursor != "release-b" {
		t.Fatalf("feed page = %#v", feed)
	}
	if feed.Items[0].URL != "/api/knowledge/releases/release-b" {
		t.Fatalf("feed item URL = %#v", feed.Items[0])
	}
	next := requestKBase(handler, http.MethodGet, "/api/knowledge/feed?after=release-b&limit=2&policy=evidence_only&source=wechat_mp_article", "secret-token")
	if next.Code != http.StatusOK || strings.Contains(next.Body.String(), "release-b") || !strings.Contains(next.Body.String(), "release-c") {
		t.Fatalf("next feed status=%d body=%s", next.Code, next.Body.String())
	}
	byBook := requestKBase(handler, http.MethodGet, "/api/knowledge/feed?book_id=book-c", "secret-token")
	if byBook.Code != http.StatusOK || !strings.Contains(byBook.Body.String(), "release-c") || strings.Contains(byBook.Body.String(), "release-a") {
		t.Fatalf("book feed status=%d body=%s", byBook.Code, byBook.Body.String())
	}
}

func TestKnowledgeFeedRejectsInvalidCursorAndRequiresAuth(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveFeedRelease(t, store, KnowledgeRelease{ReleaseID: "release-a", BookID: "book-a", ContentHash: "hash-a", UsagePolicy: BookUsageStandard, CreatedAt: "2026-07-14T00:00:00Z"})
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	unauthorized := requestKBase(handler, http.MethodGet, "/api/knowledge/feed", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	invalidCursor := requestKBase(handler, http.MethodGet, "/api/knowledge/feed?after=missing", "secret-token")
	if invalidCursor.Code != http.StatusBadRequest || !strings.Contains(invalidCursor.Body.String(), "invalid cursor") {
		t.Fatalf("invalid cursor status=%d body=%s", invalidCursor.Code, invalidCursor.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodPost, "/api/knowledge/feed", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func saveFeedRelease(t *testing.T, store *BookKnowledgeStore, release KnowledgeRelease) {
	t.Helper()
	if release.SchemaVersion == "" {
		release.SchemaVersion = KnowledgeReleaseSchemaVersion
	}
	if release.Version == "" {
		release.Version = knowledgeReleaseVersion
	}
	if release.Book.BookID == "" {
		release.Book.BookID = release.BookID
	}
	if release.Book.Title == "" {
		release.Book.Title = release.BookID
	}
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
}
