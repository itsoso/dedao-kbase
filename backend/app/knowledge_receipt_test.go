package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestDeliveryReceiptIsIdempotentAndRejectsConflicts(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	saveFeedRelease(t, store, KnowledgeRelease{ReleaseID: "release-a", BookID: "book-a", ContentHash: "hash-a", UsagePolicy: BookUsageStandard, CreatedAt: "2026-07-14T00:00:00Z"})
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})
	path := "/api/knowledge/releases/release-a/receipts"
	payload := `{"schema_version":"delivery_receipt.v1","consumer":"health-consumer","release_id":"release-a","idempotency_key":"health-consumer:release-a:1","disposition":"imported","imported_fingerprint":"sha256:imported"}`

	created := requestJSONKBase(handler, http.MethodPost, path, "secret-token", payload)
	if created.Code != http.StatusOK || !strings.Contains(created.Body.String(), `"receipt_id":"receipt-`) {
		t.Fatalf("created receipt status=%d body=%s", created.Code, created.Body.String())
	}
	replayed := requestJSONKBase(handler, http.MethodPost, path, "secret-token", payload)
	if replayed.Code != http.StatusOK || replayed.Body.String() != created.Body.String() {
		t.Fatalf("replayed receipt status=%d body=%s want=%s", replayed.Code, replayed.Body.String(), created.Body.String())
	}
	conflict := requestJSONKBase(handler, http.MethodPost, path, "secret-token", strings.Replace(payload, `"imported"`, `"held"`, 1))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestDeliveryReceiptRequiresReleaseAndAuth(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "secret-token"})
	payload := `{"schema_version":"delivery_receipt.v1","consumer":"health-consumer","release_id":"missing","idempotency_key":"health-consumer:missing:1","disposition":"imported"}`
	unauthorized := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/releases/missing/receipts", "", payload)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	missing := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/releases/missing/receipts", "secret-token", payload)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing release status=%d body=%s", missing.Code, missing.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodGet, "/api/knowledge/releases/missing/receipts", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}
