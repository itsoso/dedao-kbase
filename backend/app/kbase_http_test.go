package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKBaseHTTPHandlerRequiresBearerTokenForAPI(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books", "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status without token = %d, want 401", resp.Code)
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "wrong-token")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status with wrong token = %d, want 401", resp.Code)
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("status with correct token = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"42"`) {
		t.Fatalf("books response missing sample book: %s", resp.Body.String())
	}
}

func TestKBaseHTTPHandlerBookChatAllowsPost(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestJSONKBase(handler, http.MethodPost, "/api/book-chat", "secret-token", `{}`)
	if resp.Code == http.StatusMethodNotAllowed {
		t.Fatalf("book chat POST returned 405; HTTP API should expose TokenPlan analysis: %s", resp.Body.String())
	}
}

func TestKBaseHTTPHandlerBookAnalysisGet(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SaveAnalysisManifest(BookAnalysisManifest{
		Version: "1", BookID: "source-article-1", ContentHash: "hash-1",
		Status: BookAnalysisPending, UpdatedAt: "2026-07-12T12:00:00Z",
	}); err != nil {
		t.Fatalf("SaveAnalysisManifest returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	resp := requestKBase(handler, http.MethodGet, "/api/books/source-article-1/analysis", "secret-token")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"status":"pending"`) {
		t.Fatalf("analysis GET status=%d body=%s", resp.Code, resp.Body.String())
	}
	missing := requestKBase(handler, http.MethodGet, "/api/books/missing/analysis", "secret-token")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing analysis status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestKBaseHTTPHandlerBookAnalysisPost(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	var got BookAnalysisGenerateRequest
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		AnalysisGenerator: func(_ context.Context, _ *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
			got = request
			return &BookAnalysisManifest{Version: "1", BookID: request.BookID, Status: BookAnalysisReady, Model: request.Model, Answer: "analysis"}, nil
		},
	})

	resp := requestJSONKBase(handler, http.MethodPost, "/api/books/source-article-1/analysis", "secret-token", `{"model":"Qwen-3.7-Max","max_context_chars":8000}`)
	if resp.Code != http.StatusOK || got.BookID != "source-article-1" || got.Model != "Qwen-3.7-Max" || got.MaxContextChars != 8000 {
		t.Fatalf("analysis POST status=%d request=%#v body=%s", resp.Code, got, resp.Body.String())
	}
	invalid := requestJSONKBase(handler, http.MethodPost, "/api/books/source-article-1/analysis", "secret-token", `{`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid analysis POST status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeQualityAndRelease(t *testing.T) {
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	quality := requestKBase(handler, http.MethodGet, "/api/books/42/quality", "secret-token")
	if quality.Code != http.StatusOK || !strings.Contains(quality.Body.String(), `"decision":"pass"`) {
		t.Fatalf("quality status=%d body=%s", quality.Code, quality.Body.String())
	}
	published := requestJSONKBase(handler, http.MethodPost, "/api/books/42/publish", "secret-token", `{}`)
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"release_id":"release-`) {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	var release KnowledgeRelease
	if err := json.Unmarshal(published.Body.Bytes(), &release); err != nil {
		t.Fatalf("decode release: %v", err)
	}

	list := requestKBase(handler, http.MethodGet, "/api/knowledge/releases?limit=10", "secret-token")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), release.ReleaseID) || !strings.Contains(list.Body.String(), `"next_cursor":"`+release.ReleaseID+`"`) {
		t.Fatalf("release list status=%d body=%s", list.Code, list.Body.String())
	}
	after := requestKBase(handler, http.MethodGet, "/api/knowledge/releases?after="+url.QueryEscape(release.ReleaseID), "secret-token")
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), `"releases":[]`) {
		t.Fatalf("release cursor status=%d body=%s", after.Code, after.Body.String())
	}
	detail := requestKBase(handler, http.MethodGet, "/api/knowledge/releases/"+url.PathEscape(release.ReleaseID), "secret-token")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"usage_policy":"standard"`) {
		t.Fatalf("release detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodDelete, "/api/books/42/quality", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("quality DELETE status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeReleaseRejectsQuarantinedAnalysis(t *testing.T) {
	store := qualityTestStore(t)
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.Payload.Claims[0].CitationIDs = nil
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})
	resp := requestJSONKBase(handler, http.MethodPost, "/api/books/42/publish", "secret-token", `{}`)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "quality decision") {
		t.Fatalf("publish quarantined status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeFeedback(t *testing.T) {
	store, release := feedbackTestStore(t)
	analysisCalls := 0
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token",
		ReverificationNow:      func() time.Time { return time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC) },
		ReverificationCooldown: 5 * time.Minute,
		AnalysisGenerator: func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
			analysisCalls++
			return nil, fmt.Errorf("must not run synchronously")
		},
	})
	path := "/api/knowledge/releases/" + url.PathEscape(release.ReleaseID) + "/feedback"
	empty := requestKBase(handler, http.MethodGet, path, "secret-token")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"disposition":"healthy"`) || strings.Contains(empty.Body.String(), "consumer") {
		t.Fatalf("empty feedback assessment status=%d body=%s", empty.Code, empty.Body.String())
	}
	payload := `{"event_id":"event-1","consumer":"health-assistant","outcome":"used","claim_ids":["claim-1"],"reason_code":"used_for_answer"}`
	resp := requestJSONKBase(handler, http.MethodPost, path, "secret-token", payload)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"feedback_id":"feedback-`) || !strings.Contains(resp.Body.String(), `"used":1`) || !strings.Contains(resp.Body.String(), `"assessment":{"release_id":`) {
		t.Fatalf("feedback status=%d body=%s", resp.Code, resp.Body.String())
	}
	reverify := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-1b","consumer":"health-assistant","outcome":"stale","reason_code":"stale_source"}`)
	if reverify.Code != http.StatusOK || !strings.Contains(reverify.Body.String(), `"disposition":"reverify_required"`) || !strings.Contains(reverify.Body.String(), `"trigger_outcomes":["stale"]`) || !strings.Contains(reverify.Body.String(), `"reverification":{"version":"1"`) || !strings.Contains(reverify.Body.String(), `"status":"queued"`) {
		t.Fatalf("reverify feedback status=%d body=%s", reverify.Code, reverify.Body.String())
	}
	if analysisCalls != 0 {
		t.Fatalf("feedback invoked analysis generator %d times", analysisCalls)
	}
	replayed := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-1b","consumer":"health-assistant","outcome":"stale","reason_code":"stale_source"}`)
	if replayed.Code != http.StatusOK {
		t.Fatalf("replayed feedback status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	tasks, err := store.ListKnowledgeReverifications(release.ReleaseID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("replayed feedback tasks=%#v err=%v", tasks, err)
	}
	statusPath := "/api/knowledge/releases/" + url.PathEscape(release.ReleaseID) + "/reverification"
	status := requestKBase(handler, http.MethodGet, statusPath, "secret-token")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"tasks":[`) || !strings.Contains(status.Body.String(), `"status":"queued"`) || strings.Contains(status.Body.String(), "health-assistant") || strings.Contains(status.Body.String(), "event-1b") {
		t.Fatalf("reverification status=%d body=%s", status.Code, status.Body.String())
	}
	method := requestJSONKBase(handler, http.MethodPost, statusPath, "secret-token", `{}`)
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("reverification POST status=%d body=%s", method.Code, method.Body.String())
	}
	read := requestKBase(handler, http.MethodGet, path, "secret-token")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"reverify_required":true`) || strings.Contains(read.Body.String(), "event-1") || strings.Contains(read.Body.String(), "health-assistant") {
		t.Fatalf("feedback assessment status=%d body=%s", read.Code, read.Body.String())
	}
	sensitive := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-2","consumer":"health-assistant","outcome":"used","user_id":"private-user"}`)
	if sensitive.Code != http.StatusBadRequest || !strings.Contains(sensitive.Body.String(), "invalid JSON body") {
		t.Fatalf("sensitive feedback status=%d body=%s", sensitive.Code, sensitive.Body.String())
	}
	freeText := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-2b","consumer":"health-assistant","outcome":"used","reason":"private free text"}`)
	if freeText.Code != http.StatusBadRequest || !strings.Contains(freeText.Body.String(), "invalid JSON body") {
		t.Fatalf("free-text feedback status=%d body=%s", freeText.Code, freeText.Body.String())
	}
	mismatch := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-1","consumer":"health-assistant","outcome":"rejected","claim_ids":["claim-1"]}`)
	if mismatch.Code != http.StatusConflict || !strings.Contains(mismatch.Body.String(), "idempotency") {
		t.Fatalf("mismatched feedback status=%d body=%s", mismatch.Code, mismatch.Body.String())
	}
	invalidClaim := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-3","consumer":"health-assistant","outcome":"conflict","claim_ids":["missing"]}`)
	if invalidClaim.Code != http.StatusBadRequest || !strings.Contains(invalidClaim.Body.String(), "claim_id") {
		t.Fatalf("invalid claim status=%d body=%s", invalidClaim.Code, invalidClaim.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeReverificationRetry(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	task, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextKnowledgeReverification(now, 15*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claimed = %#v, ok=%v, err=%v", claimed, ok, err)
	}
	if _, err := store.FailKnowledgeReverification(task.TaskID, claimed.AssessmentAt, claimed.AssessmentFingerprint, KnowledgeReverificationErrorAnalysisFailed, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token", ReverificationNow: func() time.Time { return now.Add(2 * time.Minute) },
	})
	path := "/api/knowledge/releases/" + url.PathEscape(release.ReleaseID) + "/reverification/retry"
	unauthorized := requestJSONKBase(handler, http.MethodPost, path, "", `{}`)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized retry status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodGet, path, "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("retry GET status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
	retried := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{}`)
	if retried.Code != http.StatusOK || !strings.Contains(retried.Body.String(), `"status":"queued"`) || !strings.Contains(retried.Body.String(), `"attempts":0`) {
		t.Fatalf("retry status=%d body=%s", retried.Code, retried.Body.String())
	}
	conflict := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{}`)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "failed") {
		t.Fatalf("retry conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	missing := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/releases/missing/reverification/retry", "secret-token", `{}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing retry status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeReleasesFiltersBookBeforeLimit(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	for _, release := range []KnowledgeRelease{
		{Version: knowledgeReleaseVersion, ReleaseID: "release-other", BookID: "other", CreatedAt: "2026-07-14T12:00:00Z"},
		{Version: knowledgeReleaseVersion, ReleaseID: "release-target", BookID: "target", CreatedAt: "2026-07-14T12:01:00Z"},
	} {
		if err := store.saveKnowledgeRelease(release); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})
	response := requestKBase(handler, http.MethodGet, "/api/knowledge/releases?book_id=target&limit=1", "secret-token")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"release_id":"release-target"`) || strings.Contains(response.Body.String(), `"release-other"`) {
		t.Fatalf("book-filtered releases status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestKBaseHTTPHandlerSourceAgentAuthenticationIsolation(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            NewBookKnowledgeStore(root),
		AuthToken:        "admin-secret",
		SourceSync:       sourceSync,
		SourceAgentToken: "agent-secret",
	})
	heartbeat := `{"agent_id":"agent-a","version":"1.0.0","capabilities":["sync_content"],"wcplus_healthy":true}`

	resp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "admin-secret", heartbeat)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("admin token on agent route status = %d, body=%s", resp.Code, resp.Body.String())
	}
	resp = requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "invalid-agent-token", heartbeat)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("invalid agent token status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "invalid-agent-token") {
		t.Fatalf("agent auth response leaked token: %s", resp.Body.String())
	}
	resp = requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "agent-secret", heartbeat)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"agent_id":"agent-a"`) {
		t.Fatalf("agent heartbeat status = %d, body=%s", resp.Code, resp.Body.String())
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "agent-secret")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("agent token on admin route status = %d, body=%s", resp.Code, resp.Body.String())
	}
	resp = requestKBase(handler, http.MethodGet, "/api/books", "admin-secret")
	if resp.Code != http.StatusOK {
		t.Fatalf("admin token on admin route status = %d, body=%s", resp.Code, resp.Body.String())
	}
	browserReq := httptest.NewRequest(http.MethodGet, "/browser/session-token", nil)
	browserReq.Header.Set("Authorization", "Bearer agent-secret")
	browserReq.Header.Set("X-KBase-Browser-Session", "1")
	browserResp := httptest.NewRecorder()
	handler.ServeHTTP(browserResp, browserReq)
	if browserResp.Code != http.StatusUnauthorized || strings.Contains(browserResp.Body.String(), "admin-secret") {
		t.Fatalf("agent token exchanged for browser token: status=%d body=%s", browserResp.Code, browserResp.Body.String())
	}

	unconfigured := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:      NewBookKnowledgeStore(t.TempDir()),
		AuthToken:  "admin-secret",
		SourceSync: sourceSync,
	})
	resp = requestJSONKBase(unconfigured, http.MethodPost, "/api/source-agent/heartbeat", "agent-secret", heartbeat)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured agent auth status = %d, body=%s", resp.Code, resp.Body.String())
	}

	sharedToken := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            NewBookKnowledgeStore(t.TempDir()),
		AuthToken:        "shared-secret",
		SourceSync:       sourceSync,
		SourceAgentToken: "shared-secret",
	})
	resp = requestJSONKBase(sharedToken, http.MethodPost, "/api/source-agent/heartbeat", "shared-secret", heartbeat)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("shared admin/agent token status = %d, body=%s", resp.Code, resp.Body.String())
	}
	resp = requestKBase(sharedToken, http.MethodGet, "/api/books", "shared-secret")
	if resp.Code != http.StatusOK {
		t.Fatalf("shared-token defense disabled admin API: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPHandlerSerializesCapabilityHealth(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: NewBookKnowledgeStore(root), AuthToken: "admin-secret", SourceSync: sourceSync, SourceAgentToken: "agent-secret"})
	heartbeat := `{"agent_id":"agent-a","capability_health":{"wechat_mp":{"healthy":false,"requires_action":"login"},"wcplus":{"healthy":false}}}`
	if resp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "agent-secret", heartbeat); resp.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", resp.Code, resp.Body.String())
	}
	resp := requestKBase(handler, http.MethodGet, "/api/source-agents", "admin-secret")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"capability_health":{"wcplus":{"healthy":false},"wechat_mp":{"healthy":false,"requires_action":"login"}}`) {
		t.Fatalf("agents capability response status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPHandlerCreatesWeChatCollectorSubscription(t *testing.T) {
	root := t.TempDir()
	syncStore, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: NewBookKnowledgeStore(root), AuthToken: "admin-secret", SourceSync: syncStore})
	resp := requestJSONKBase(handler, http.MethodPost, "/api/source-subscriptions", "admin-secret", `{"source_type":"wechat_mp_article","source_account_key":"account-key","source_account":"Sanitized account","operation":"sync_articles","schedule":"manual","enabled":true}`)
	if resp.Code != http.StatusCreated || !strings.Contains(resp.Body.String(), `"source_type":"wechat_mp_article"`) || !strings.Contains(resp.Body.String(), `"operation":"sync_articles"`) {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPHandlerSourceAgentPayloadLimit(t *testing.T) {
	sourceSync, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:                   NewBookKnowledgeStore(t.TempDir()),
		AuthToken:               "admin-secret",
		SourceSync:              sourceSync,
		SourceAgentToken:        "agent-secret",
		SourceAgentMaxBodyBytes: 128,
	})
	payload := `{"agent_id":"agent-a","source_item_key":"` + strings.Repeat("x", 512) + `","idempotency_key":"idem","outcome":"new"}`
	resp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/runs/run-1/items", "agent-secret", payload)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized agent payload status = %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPHandlerSourceSyncHTTP(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            NewBookKnowledgeStore(root),
		AuthToken:        "admin-secret",
		SourceSync:       sourceSync,
		SourceAgentToken: "agent-secret",
	})

	createResp := requestJSONKBase(handler, http.MethodPost, "/api/source-subscriptions", "admin-secret", `{
		"source_type":"wcplus_wechat_article",
		"source_account_key":"biz-med",
		"source_account":"医学参考",
		"agent_id":"agent-a",
		"schedule":"manual",
		"operation":"sync_content",
		"enabled":true
	}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create subscription status = %d, body=%s", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		Subscription SourceSubscription `json:"subscription"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	if createPayload.Subscription.ID == "" {
		t.Fatalf("created subscription missing id: %s", createResp.Body.String())
	}

	syncPath := "/api/source-subscriptions/" + url.PathEscape(createPayload.Subscription.ID) + "/sync"
	syncResp := requestJSONKBase(handler, http.MethodPost, syncPath, "admin-secret", `{}`)
	if syncResp.Code != http.StatusCreated {
		t.Fatalf("create sync run status = %d, body=%s", syncResp.Code, syncResp.Body.String())
	}

	heartbeatResp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "agent-secret", `{
		"agent_id":"agent-a",
		"version":"1.0.0",
		"capabilities":["sync_content"],
		"wcplus_healthy":true
	}`)
	if heartbeatResp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, body=%s", heartbeatResp.Code, heartbeatResp.Body.String())
	}
	leaseResp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/lease", "agent-secret", `{
		"agent_id":"agent-a",
		"capabilities":["sync_content"],
		"lease_seconds":120
	}`)
	if leaseResp.Code != http.StatusOK {
		t.Fatalf("lease status = %d, body=%s", leaseResp.Code, leaseResp.Body.String())
	}
	var leasePayload struct {
		Run *SourceSyncRun `json:"run"`
	}
	if err := json.Unmarshal(leaseResp.Body.Bytes(), &leasePayload); err != nil {
		t.Fatalf("decode lease: %v", err)
	}
	if leasePayload.Run == nil || leasePayload.Run.Status != SourceRunRunning {
		t.Fatalf("leased run = %#v, body=%s", leasePayload.Run, leaseResp.Body.String())
	}

	runPath := "/api/source-agent/runs/" + url.PathEscape(leasePayload.Run.ID)
	itemResp := requestJSONKBase(handler, http.MethodPost, runPath+"/items", "agent-secret", `{
		"agent_id":"agent-a",
		"source_type":"wcplus_wechat_article",
		"source_account_key":"biz-med",
		"source_account":"医学参考",
		"source_item_key":"article-1",
		"idempotency_key":"idem-1",
		"title":"可验证知识",
		"author":"编辑部",
		"source_url":"https://mp.weixin.qq.com/s/article-1",
		"published_at":"2026-07-09T19:30:00Z",
		"content":"# 可验证知识\\n\\n每一个知识结论都需要保留可复核的来源、上下文和更新时间，供下游系统进行交叉验证。",
		"content_format":"markdown"
	}`)
	if itemResp.Code != http.StatusCreated {
		t.Fatalf("record item status = %d, body=%s", itemResp.Code, itemResp.Body.String())
	}
	completeResp := requestJSONKBase(handler, http.MethodPost, runPath+"/complete", "agent-secret", `{"agent_id":"agent-a"}`)
	if completeResp.Code != http.StatusOK || !strings.Contains(completeResp.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("complete run status = %d, body=%s", completeResp.Code, completeResp.Body.String())
	}

	detailResp := requestKBase(handler, http.MethodGet, "/api/source-sync/runs/"+url.PathEscape(leasePayload.Run.ID), "admin-secret")
	if detailResp.Code != http.StatusOK || !strings.Contains(detailResp.Body.String(), `"new_count":1`) || !strings.Contains(detailResp.Body.String(), `"source_item_key":"article-1"`) {
		t.Fatalf("run detail status = %d, body=%s", detailResp.Code, detailResp.Body.String())
	}
	agentsResp := requestKBase(handler, http.MethodGet, "/api/source-agents", "admin-secret")
	if agentsResp.Code != http.StatusOK || !strings.Contains(agentsResp.Body.String(), `"agent_id":"agent-a"`) {
		t.Fatalf("agents status = %d, body=%s", agentsResp.Code, agentsResp.Body.String())
	}
	runsResp := requestKBase(handler, http.MethodGet, "/api/source-sync/runs", "admin-secret")
	if runsResp.Code != http.StatusOK || !strings.Contains(runsResp.Body.String(), leasePayload.Run.ID) {
		t.Fatalf("runs status = %d, body=%s", runsResp.Code, runsResp.Body.String())
	}
}

func TestKBaseHTTPHandlerPersistsFailureCheckpointCursor(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceSync.Close()
	subscription, err := sourceSync.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wechat_mp_article",
		SourceAccountKey: "account-key",
		SourceAccount:    "Account",
		Operation:        "sync_articles",
		Cursor:           "old-cursor",
		Enabled:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := sourceSync.CreateRun(subscription.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceSync.LeaseNextRun("agent-a", []string{"sync_articles"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceSync.StartRun(run.ID, "agent-a"); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            NewBookKnowledgeStore(root),
		AuthToken:        "admin-secret",
		SourceSync:       sourceSync,
		SourceAgentToken: "agent-secret",
	})
	response := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/runs/"+url.PathEscape(run.ID)+"/fail", "agent-secret", `{
		"agent_id":"agent-a",
		"error":"download failed",
		"cursor":"safe-cursor"
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("fail status=%d body=%s", response.Code, response.Body.String())
	}
	updated, err := sourceSync.GetSubscription(subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Cursor != "safe-cursor" || updated.LastSuccessAt != "" {
		t.Fatalf("updated subscription=%#v", updated)
	}
}

func TestKBaseHTTPHandlerSetsSubscriptionEnabledWithoutReplacingCursor(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:      NewBookKnowledgeStore(root),
		AuthToken:  "admin-secret",
		SourceSync: sourceSync,
	})
	subscription, err := sourceSync.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-med",
		SourceAccount:    "医学参考",
		AgentID:          "agent-a",
		Schedule:         "interval:3600",
		Cursor:           "2026-07-10T11:55:00Z|article-42",
		Operation:        "sync_content",
		Options:          map[string]any{"limit": float64(50)},
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	path := "/api/source-subscriptions/" + url.PathEscape(subscription.ID) + "/enabled"
	resp := requestJSONKBase(handler, http.MethodPost, path, "admin-secret", `{"enabled":false}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("disable subscription status = %d, body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Subscription SourceSubscription `json:"subscription"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	if payload.Subscription.Enabled || payload.Subscription.Cursor != subscription.Cursor || payload.Subscription.Schedule != subscription.Schedule || payload.Subscription.Operation != subscription.Operation {
		t.Fatalf("enabled endpoint replaced subscription state: before=%#v after=%#v", subscription, payload.Subscription)
	}

	missingEnabled := requestJSONKBase(handler, http.MethodPost, path, "admin-secret", `{}`)
	if missingEnabled.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled status = %d, body=%s", missingEnabled.Code, missingEnabled.Body.String())
	}
}

func TestKBaseHTTPHandlerBrowserSessionTokenRequiresTrustedHeader(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
	})

	req := httptest.NewRequest(http.MethodGet, "/browser/session-token", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status without trusted header = %d, want 401", resp.Code)
	}
	if strings.Contains(resp.Body.String(), "secret-token") {
		t.Fatalf("untrusted response leaked token: %s", resp.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/browser/session-token", nil)
	req.Header.Set("X-KBase-Browser-Session", "1")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status with trusted header = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"token":"secret-token"`) {
		t.Fatalf("trusted response missing token: %s", resp.Body.String())
	}
	if got := resp.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestKBaseHTTPHandlerAllowsDesktopCORSPreflight(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/wcplus/status", nil)
	req.Header.Set("Origin", "wails://wails.localhost")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "wails://wails.localhost" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := resp.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") || !strings.Contains(got, "Content-Type") {
		t.Fatalf("Access-Control-Allow-Headers = %q", got)
	}
	if got := resp.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodGet) || !strings.Contains(got, http.MethodPost) {
		t.Fatalf("Access-Control-Allow-Methods = %q", got)
	}

	untrustedReq := httptest.NewRequest(http.MethodOptions, "/api/wcplus/status", nil)
	untrustedReq.Header.Set("Origin", "https://example.invalid")
	untrustedReq.Header.Set("Access-Control-Request-Method", http.MethodGet)
	untrustedResp := httptest.NewRecorder()
	handler.ServeHTTP(untrustedResp, untrustedReq)
	if untrustedResp.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("untrusted origin received CORS header: %q", untrustedResp.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestKBaseHTTPHandlerServesSearchAndSystemKBExport(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	exportPath := filepath.Join(root, "artifacts", "system_kb_export.json")
	if err := os.MkdirAll(filepath.Dir(exportPath), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	exportPayload := map[string]any{
		"type":        "system_kb_v2_export",
		"schema_id":   "llm-wiki-v2-system-kb-export",
		"version":     "test-version",
		"source":      "dedao-kbase",
		"compiled_at": "2026-06-27T10:00:00Z",
		"stats":       map[string]any{"claim_count": 1},
		"pages":       []any{},
		"entities":    []any{},
		"claims":      []any{},
		"relations":   []any{},
	}
	data, err := json.Marshal(exportPayload)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if err := os.WriteFile(exportPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:              store,
		AuthToken:          "secret-token",
		SystemKBExportPath: exportPath,
	})

	searchResp := requestKBase(handler, http.MethodGet, "/api/search?q=MACD&limit=5", "secret-token")
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", searchResp.Code, searchResp.Body.String())
	}
	if !strings.Contains(searchResp.Body.String(), `"results"`) || !strings.Contains(searchResp.Body.String(), `"42"`) {
		t.Fatalf("search response missing results: %s", searchResp.Body.String())
	}

	manifestResp := requestKBase(handler, http.MethodGet, "/api/system-kb/manifest", "secret-token")
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, body=%s", manifestResp.Code, manifestResp.Body.String())
	}
	if !strings.Contains(manifestResp.Body.String(), `"version":"test-version"`) {
		t.Fatalf("manifest response missing version: %s", manifestResp.Body.String())
	}

	exportResp := requestKBase(handler, http.MethodGet, "/api/system-kb/export", "secret-token")
	if exportResp.Code != http.StatusOK {
		t.Fatalf("export status = %d, body=%s", exportResp.Code, exportResp.Body.String())
	}
	if !strings.Contains(exportResp.Body.String(), `"type":"system_kb_v2_export"`) {
		t.Fatalf("export response missing payload: %s", exportResp.Body.String())
	}
}

func TestKBaseHTTPHandlerReadsBookWithLegacyReaderSuffix(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "83477"
	pkg.Book.Title = "83477_测试书"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books/83477-prompts", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("legacy suffix status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"83477"`) {
		t.Fatalf("legacy suffix response did not resolve base book id: %s", resp.Body.String())
	}
}

func TestKBaseHTTPHandlerMissingBookDoesNotExposeFilesystemPath(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books/missing-prompts", "secret-token")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("missing book status = %d, want 404, body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, leak := range []string{root, "manifest.json", "book_knowledge"} {
		if strings.Contains(body, leak) {
			t.Fatalf("missing book response leaked %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "book not found") {
		t.Fatalf("missing book response should be actionable: %s", body)
	}
}

func TestKBaseHTTPHandlerServesWebAssets(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	webDir := filepath.Join(root, "web")
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<main class="reader-loading">reader</main>`), 0o644); err != nil {
		t.Fatalf("WriteFile index returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte(`console.log("reader")`), 0o644); err != nil {
		t.Fatalf("WriteFile asset returned error: %v", err)
	}

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		StaticDir: webDir,
	})

	indexResp := requestKBase(handler, http.MethodGet, "/", "")
	if indexResp.Code != http.StatusOK {
		t.Fatalf("index status = %d, body=%s", indexResp.Code, indexResp.Body.String())
	}
	if !strings.Contains(indexResp.Body.String(), `reader-loading`) {
		t.Fatalf("index response missing reader shell: %s", indexResp.Body.String())
	}

	assetResp := requestKBase(handler, http.MethodGet, "/assets/app.js", "")
	if assetResp.Code != http.StatusOK {
		t.Fatalf("asset status = %d, body=%s", assetResp.Code, assetResp.Body.String())
	}
	if !strings.Contains(assetResp.Body.String(), `console.log`) {
		t.Fatalf("asset response missing script: %s", assetResp.Body.String())
	}

	readerRouteResp := requestKBase(handler, http.MethodGet, "/ebook/42", "")
	if readerRouteResp.Code != http.StatusOK {
		t.Fatalf("reader route status = %d, body=%s", readerRouteResp.Code, readerRouteResp.Body.String())
	}
	if !strings.Contains(readerRouteResp.Body.String(), `reader-loading`) {
		t.Fatalf("reader route did not fall back to index: %s", readerRouteResp.Body.String())
	}

	missingAssetResp := requestKBase(handler, http.MethodGet, "/assets/missing.js", "")
	if missingAssetResp.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", missingAssetResp.Code)
	}

	apiResp := requestKBase(handler, http.MethodGet, "/api/books", "")
	if apiResp.Code != http.StatusUnauthorized {
		t.Fatalf("api status without token = %d, want 401", apiResp.Code)
	}
}

func TestKBaseHTTPHandlerImportsWeChatArticleIntoBookKnowledge(t *testing.T) {
	articleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html>
<html>
  <body>
    <h1 id="activity-name">健康验证方法</h1>
    <a id="js_name">健康知识</a>
    <em id="publish_time">2026-07-06</em>
    <div id="js_content"><p>用指标和来源交叉验证结论。</p></div>
  </body>
</html>`)
	}))
	defer articleServer.Close()

	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WeChat:    newTestWeChatSourceService(t, articleServer),
	})

	body := bytes.NewBufferString(`{"url":"` + articleServer.URL + `/s/test","book_id":"wechat-health"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/wechat/import", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("import status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"wechat-health"`) {
		t.Fatalf("import response missing book id: %s", resp.Body.String())
	}

	pkg, err := store.LoadPackage("wechat-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if pkg.Book.Title != "健康验证方法" {
		t.Fatalf("book title = %q", pkg.Book.Title)
	}
	if len(pkg.Chunks) != 1 || !strings.Contains(pkg.Chunks[0].Text, "交叉验证结论") {
		t.Fatalf("unexpected chunks: %#v", pkg.Chunks)
	}
	if len(pkg.Citations) != 1 || pkg.Citations[0].SourceHTML != articleServer.URL+"/s/test" {
		t.Fatalf("unexpected citations: %#v", pkg.Citations)
	}
}

func TestKBaseHTTPHandlerProxiesAndImportsWCPlusArticles(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/gzh/list":
			fmt.Fprint(w, `{"success":true,"data":{"gzhs":[{"biz":"biz-1","nickname":"医学参考","article_count":2}],"total":1}}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"success":true,"data":{"gzh":{"biz":"biz-1","nickname":"医学参考"},"articles":[{"id":"wx-1","title":"验证文章","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/wx1","digest":"摘要","publish_time":"2026-07-06"}],"total":1}}`)
		case "/api/article/content":
			fmt.Fprintf(w, `{"success":true,"data":{"id":"%s","title":"验证文章 %s","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/%s","content":"# 验证文章\n\n指标交叉验证。","publish_time":"2026-07-06"}}`, r.URL.Query().Get("id"), r.URL.Query().Get("id"), r.URL.Query().Get("id"))
		case "/api/task/all":
			fmt.Fprint(w, `{"success":true,"data":{"tasks":[{"task_id":"task-1","biz":"biz-1","nickname":"医学参考","status":"running"}]}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	listResp := requestKBase(handler, http.MethodGet, "/api/wcplus/gzh/list?offset=0&num=10", "secret-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("gzh list status = %d, body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), `"biz":"biz-1"`) {
		t.Fatalf("gzh list response missing account: %s", listResp.Body.String())
	}

	contentResp := requestKBase(handler, http.MethodGet, "/api/wcplus/article/content?nickname="+url.QueryEscape("医学参考")+"&id=wx-1", "secret-token")
	if contentResp.Code != http.StatusOK {
		t.Fatalf("content status = %d, body=%s", contentResp.Code, contentResp.Body.String())
	}
	if !strings.Contains(contentResp.Body.String(), `"content"`) || !strings.Contains(contentResp.Body.String(), "指标交叉验证") {
		t.Fatalf("content response missing article content: %s", contentResp.Body.String())
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/article", bytes.NewBufferString(`{"nickname":"医学参考","id":"wx-1","book_id":"wcplus-health"}`))
	importReq.Header.Set("Authorization", "Bearer secret-token")
	importResp := httptest.NewRecorder()
	handler.ServeHTTP(importResp, importReq)
	if importResp.Code != http.StatusOK {
		t.Fatalf("import status = %d, body=%s", importResp.Code, importResp.Body.String())
	}
	pkg, err := store.LoadPackage("wcplus-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if pkg.Book.Extractor != "wcplus-source-adapter" || !strings.Contains(pkg.Chunks[0].Text, "指标交叉验证") {
		t.Fatalf("unexpected imported package: %#v", pkg)
	}

	batchReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/account", bytes.NewBufferString(`{"biz":"biz-1","nickname":"医学参考","limit":1}`))
	batchReq.Header.Set("Authorization", "Bearer secret-token")
	batchResp := httptest.NewRecorder()
	handler.ServeHTTP(batchResp, batchReq)
	if batchResp.Code != http.StatusOK {
		t.Fatalf("batch import status = %d, body=%s", batchResp.Code, batchResp.Body.String())
	}
	if !strings.Contains(batchResp.Body.String(), `"imported_count":1`) {
		t.Fatalf("batch import response missing count: %s", batchResp.Body.String())
	}

	taskResp := requestKBase(handler, http.MethodGet, "/api/wcplus/task/all", "secret-token")
	if taskResp.Code != http.StatusOK {
		t.Fatalf("task status = %d, body=%s", taskResp.Code, taskResp.Body.String())
	}
	if !strings.Contains(taskResp.Body.String(), `"task_id":"task-1"`) {
		t.Fatalf("task response missing task: %s", taskResp.Body.String())
	}
}

func TestKBaseHTTPHandlerPreviewsAndImportsWCPlusArticleByURL(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/article/content":
			if got := r.URL.Query().Get("url"); got != "https://mp.weixin.qq.com/s/url-only" {
				t.Fatalf("url = %q", got)
			}
			fmt.Fprint(w, `{"success":true,"data":{"id":"url-only","title":"URL 文章","nickname":"URL 公众号","url":"https://mp.weixin.qq.com/s/url-only","content":"# URL 文章\n\n只通过链接也能预览和导入。","publish_time":"2026-07-08"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	contentResp := requestKBase(handler, http.MethodGet, "/api/wcplus/article/content?url="+url.QueryEscape("https://mp.weixin.qq.com/s/url-only"), "secret-token")
	if contentResp.Code != http.StatusOK {
		t.Fatalf("content by URL status = %d, body=%s", contentResp.Code, contentResp.Body.String())
	}
	if !strings.Contains(contentResp.Body.String(), "只通过链接") {
		t.Fatalf("content by URL response missing body: %s", contentResp.Body.String())
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/article", bytes.NewBufferString(`{"url":"https://mp.weixin.qq.com/s/url-only","book_id":"wcplus-url-only"}`))
	importReq.Header.Set("Authorization", "Bearer secret-token")
	importResp := httptest.NewRecorder()
	handler.ServeHTTP(importResp, importReq)
	if importResp.Code != http.StatusOK {
		t.Fatalf("import by URL status = %d, body=%s", importResp.Code, importResp.Body.String())
	}
	pkg, err := store.LoadPackage("wcplus-url-only")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if !strings.Contains(pkg.Chunks[0].Text, "只通过链接") {
		t.Fatalf("unexpected imported URL package: %#v", pkg)
	}
}

func TestKBaseHTTPHandlerImportsRawWCPlusArticle(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{}),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/raw", bytes.NewBufferString(`{
		"title":"人工导入文章",
		"nickname":"医学参考",
		"url":"https://mp.weixin.qq.com/s/manual",
		"content":"# 人工导入文章\n\n用指标和来源交叉验证结论。",
		"book_id":"wcplus-manual-health"
	}`))
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("raw import status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"wcplus-manual-health"`) {
		t.Fatalf("raw import response missing book id: %s", resp.Body.String())
	}

	pkg, err := store.LoadPackage("wcplus-manual-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if pkg.Book.Extractor != "wcplus-source-adapter" || !strings.Contains(pkg.Chunks[0].Text, "交叉验证结论") {
		t.Fatalf("unexpected imported package: %#v", pkg)
	}
}

func TestKBaseHTTPHandlerProxiesAdvancedWCPlusAPIs(t *testing.T) {
	var sawQueueRun bool
	var sawBatchCreate bool
	var sawBatchDelete bool
	var sawXLSXExport bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html>wcplus</html>`)
		case "/api/search/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if got := r.URL.Query().Get("q"); got != "血压" {
				t.Fatalf("search q = %q", got)
			}
			fmt.Fprint(w, `{"Results":[{"ID":"wx-1","Title":"血压验证"}],"Total":1}`)
		case "/api/gzh/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Gzhs":[{"Biz":"biz-1","Nickname":"医学参考"}],"Total":1}`)
		case "/api/search_gzh/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-2","Nickname":"候选公众号"}],"Total":1}`)
		case "/api/article/search_title":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[{"ID":"wx-2","Title":"标题搜索"}],"Total":1}`)
		case "/api/article/all_articles":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[{"ID":"wx-3","Title":"全库文章"}],"Total":1}`)
		case "/api/report/reading_data":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Rows":[{"date":"2026-07-06","read_num":42}]}`)
		case "/api/report/statistic_data":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"total_read":42}`)
		case "/api/article/gzh":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Biz":"biz-1","Nickname":"医学参考"}`)
		case "/api/like_article/get_all":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[]}`)
		case "/api/req_data/get_gzh":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Gzh":{"Biz":"biz-1","Nickname":"医学参考"}}`)
		case "/api/article/export_text":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `2`)
		case "/api/gzh/export_csv":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `3`)
		case "/api/task/control":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode task control body: %v", err)
			}
			if payload["command"] != "run" {
				t.Fatalf("task control body = %#v", payload)
			}
			sawQueueRun = true
			fmt.Fprint(w, `{"success":true,"data":{"status":"running"}}`)
		case "/api/batch_task/create_task":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sawBatchCreate = true
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"batch-1","status":"ready"}}`)
		case "/api/batch_task/delete_task":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sawBatchDelete = true
			fmt.Fprint(w, `{"success":true,"data":{"deleted":1}}`)
		case "/api/article/all_articles/export_xlsx":
			sawXLSXExport = true
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			fmt.Fprint(w, "xlsx-bytes")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	statusResp := requestKBase(handler, http.MethodGet, "/api/wcplus/status", "secret-token")
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status status = %d, body=%s", statusResp.Code, statusResp.Body.String())
	}
	if !strings.Contains(statusResp.Body.String(), `"ok":true`) {
		t.Fatalf("status response missing ok: %s", statusResp.Body.String())
	}

	searchResp := requestKBase(handler, http.MethodGet, "/api/wcplus/search?q="+url.QueryEscape("血压"), "secret-token")
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", searchResp.Code, searchResp.Body.String())
	}
	if !strings.Contains(searchResp.Body.String(), "血压验证") {
		t.Fatalf("search response missing result: %s", searchResp.Body.String())
	}

	for _, path := range []string{
		"/api/wcplus/gzh/search?q=test",
		"/api/wcplus/search-gzh?q=test",
		"/api/wcplus/article/search-title?q=test",
		"/api/wcplus/article/all?offset=0&num=10",
		"/api/wcplus/report/reading-data?biz=biz-1",
		"/api/wcplus/report/statistic-data?biz=biz-1",
		"/api/wcplus/article/gzh?id=wx-1",
		"/api/wcplus/like-articles?offset=0&num=10",
		"/api/wcplus/request/gzh?biz=biz-1",
		"/api/wcplus/export/text?biz=biz-1&nickname=test",
		"/api/wcplus/export/gzh-csv?biz=biz-1&nickname=test",
	} {
		resp := requestKBase(handler, http.MethodGet, path, "secret-token")
		if resp.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", path, resp.Code, resp.Body.String())
		}
	}

	queueReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/task/control", bytes.NewBufferString(`{"command":"run"}`))
	queueReq.Header.Set("Authorization", "Bearer secret-token")
	queueResp := httptest.NewRecorder()
	handler.ServeHTTP(queueResp, queueReq)
	if queueResp.Code != http.StatusOK || !sawQueueRun {
		t.Fatalf("queue run status = %d, body=%s", queueResp.Code, queueResp.Body.String())
	}

	batchCreateReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/batch-task/create", bytes.NewBufferString(`{"nickname":"医学参考"}`))
	batchCreateReq.Header.Set("Authorization", "Bearer secret-token")
	batchCreateResp := httptest.NewRecorder()
	handler.ServeHTTP(batchCreateResp, batchCreateReq)
	if batchCreateResp.Code != http.StatusOK || !sawBatchCreate {
		t.Fatalf("batch create status = %d, body=%s", batchCreateResp.Code, batchCreateResp.Body.String())
	}

	batchDeleteReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/batch-task/delete", bytes.NewBufferString(`{"status":"ready"}`))
	batchDeleteReq.Header.Set("Authorization", "Bearer secret-token")
	batchDeleteResp := httptest.NewRecorder()
	handler.ServeHTTP(batchDeleteResp, batchDeleteReq)
	if batchDeleteResp.Code != http.StatusOK || !sawBatchDelete {
		t.Fatalf("batch delete status = %d, body=%s", batchDeleteResp.Code, batchDeleteResp.Body.String())
	}

	xlsxReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/export/all-articles-xlsx", bytes.NewBufferString(`{"range_mode":"recent","recent_num":10,"fields":["title"]}`))
	xlsxReq.Header.Set("Authorization", "Bearer secret-token")
	xlsxResp := httptest.NewRecorder()
	handler.ServeHTTP(xlsxResp, xlsxReq)
	if xlsxResp.Code != http.StatusOK || !sawXLSXExport || xlsxResp.Body.String() != "xlsx-bytes" {
		t.Fatalf("xlsx export status = %d, body=%q", xlsxResp.Code, xlsxResp.Body.String())
	}
	if got := xlsxResp.Header().Get("Content-Type"); !strings.Contains(got, "spreadsheetml") {
		t.Fatalf("xlsx content type = %q", got)
	}
}

func TestKBaseHTTPHandlerChecksEnvAndBatchImportsWCPlusNicknames(t *testing.T) {
	var created []map[string]any
	var queueStarted bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html>wcplus</html>`)
		case "/api/gzh/list":
			fmt.Fprint(w, `{"Gzhs":[],"Total":0}`)
		case "/api/search_gzh/search":
			keyword := r.URL.Query().Get("keyword")
			if keyword == "" {
				keyword = r.URL.Query().Get("q")
			}
			switch keyword {
			case "医学参考":
				fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-med","Nickname":"医学参考"}],"Total":1}`)
			default:
				fmt.Fprint(w, `{"Candidates":[],"Total":0}`)
			}
		case "/api/task/new":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode create task body: %v", err)
			}
			created = append(created, payload)
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"task-1","status":"ready"}}`)
		case "/api/task/control":
			queueStarted = true
			fmt.Fprint(w, `{"success":true,"data":{"status":"running"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	envResp := requestKBase(handler, http.MethodGet, "/api/wcplus/env/check", "secret-token")
	if envResp.Code != http.StatusOK {
		t.Fatalf("env check status = %d, body=%s", envResp.Code, envResp.Body.String())
	}
	if !strings.Contains(envResp.Body.String(), `"ok":true`) || !strings.Contains(envResp.Body.String(), `"gzh_list"`) {
		t.Fatalf("env check response missing details: %s", envResp.Body.String())
	}

	body := `{"nicknames":["医学参考","不存在"],"articleListType":"amount","articleListAmount":20,"start_queue":true,"exact_match":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/wcplus/batch-import/gzh", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch import status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if len(created) != 1 || created[0]["crawlerType"] != "gzh_article_link" {
		t.Fatalf("unexpected created tasks: %#v", created)
	}
	if !queueStarted {
		t.Fatalf("queue was not started")
	}
	if !strings.Contains(resp.Body.String(), `"success"`) || !strings.Contains(resp.Body.String(), `"failed"`) {
		t.Fatalf("batch import response missing lists: %s", resp.Body.String())
	}
}

func requestKBase(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func requestJSONKBase(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}
