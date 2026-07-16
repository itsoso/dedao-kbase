package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHealthKnowledgeFeedReturnsEvidenceOnlyReleases(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveFeedRelease(t, store, KnowledgeRelease{
		ReleaseID:   "release-standard",
		BookID:      "book-standard",
		ContentHash: "hash-standard",
		UsagePolicy: BookUsageStandard,
		CreatedAt:   "2026-07-16T10:00:00Z",
		Book:        BookKnowledgeBook{SourceType: "dedao_ebook"},
	})
	saveFeedRelease(t, store, KnowledgeRelease{
		ReleaseID:   "release-health",
		BookID:      "book-health",
		ContentHash: "hash-health",
		UsagePolicy: BookUsageEvidenceOnly,
		CreatedAt:   "2026-07-16T10:01:00Z",
		Book:        BookKnowledgeBook{SourceType: "wechat_mp_article"},
	})
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	resp := requestKBase(handler, http.MethodGet, "/api/consumers/health/releases?limit=10", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("health feed status=%d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "release-standard") {
		t.Fatalf("health feed leaked standard release: %s", resp.Body.String())
	}
	var page KnowledgeFeedPage
	if err := json.Unmarshal(resp.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode feed: %v", err)
	}
	if page.SchemaVersion != KnowledgeFeedSchemaVersion || len(page.Items) != 1 || page.Items[0].ReleaseID != "release-health" || page.Items[0].UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("page = %#v", page)
	}
}

func TestHealthKnowledgeFeedRequiresAuthAndRejectsInvalidCursor(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveFeedRelease(t, store, KnowledgeRelease{
		ReleaseID:   "release-health",
		BookID:      "book-health",
		ContentHash: "hash-health",
		UsagePolicy: BookUsageEvidenceOnly,
		CreatedAt:   "2026-07-16T10:01:00Z",
	})
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	unauthorized := requestKBase(handler, http.MethodGet, "/api/consumers/health/releases", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	invalid := requestKBase(handler, http.MethodGet, "/api/consumers/health/releases?after=missing", "secret-token")
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "invalid cursor") {
		t.Fatalf("invalid cursor status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodPost, "/api/consumers/health/releases", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}
