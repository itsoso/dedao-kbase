package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingSourceEnvelopeSink struct {
	envelopes []SourceArticleEnvelope
	failItem  string
}

func (s *recordingSourceEnvelopeSink) Enqueue(_ string, envelope SourceArticleEnvelope) (SourceAgentOutboxItem, error) {
	if envelope.SourceItemID == s.failItem {
		return SourceAgentOutboxItem{}, fmt.Errorf("enqueue %s failed", envelope.SourceItemID)
	}
	s.envelopes = append(s.envelopes, envelope)
	return SourceAgentOutboxItem{IdempotencyKey: envelope.IdempotencyKey, Envelope: envelope}, nil
}

type fakeSessionHealthProvider struct {
	session WeChatMPSession
	err     error
}

func (p fakeSessionHealthProvider) Session(context.Context) (WeChatMPSession, error) {
	return p.session, p.err
}
func TestWeChatAgentReportsLoginRequiredWithoutSession(t *testing.T) {
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: fakeSessionHealthProvider{err: ErrSourceSecretNotFound}})
	if err != nil {
		t.Fatal(err)
	}
	health := adapter.Status(context.Background())
	if health.Healthy || health.RequiresAction != "login" {
		t.Fatalf("health=%#v", health)
	}
	if adapter.Name() != "wechat_mp" {
		t.Fatalf("name=%s", adapter.Name())
	}
}
func TestWeChatAgentDeclaresFirstPartyOperations(t *testing.T) {
	adapter, _ := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: fakeSessionHealthProvider{session: WeChatMPSession{Token: "test-value"}}})
	want := map[string]bool{"discover_articles": true, "sync_articles": true, "sync_media": true}
	for _, operation := range adapter.Operations() {
		delete(want, operation)
	}
	if len(want) != 0 {
		t.Fatalf("missing operations=%v", want)
	}
}

func TestWeChatAgentCursorDecodesLegacyBegin(t *testing.T) {
	cursor, err := decodeWeChatAgentCursor(`{"begin":10}`)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.UpstreamBegin != 10 || cursor.PublicationItemIndex != 0 {
		t.Fatalf("cursor=%#v", cursor)
	}
}

func TestWeChatAgentCursorRoundTrips(t *testing.T) {
	want := weChatAgentCursor{
		UpstreamBegin:        12,
		PublicationItemIndex: 2,
		LastArticleKey:       "article-key",
		LastTimestamp:        1234,
	}
	encoded, err := encodeWeChatAgentCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeWeChatAgentCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cursor=%#v want=%#v encoded=%s", got, want, encoded)
	}
}

func TestWeChatAgentCursorRejectsInvalidValue(t *testing.T) {
	for _, raw := range []string{`{"upstream_begin":`, `{"upstream_begin":-1}`, `{"upstream_begin":1,"publication_item_index":-1}`} {
		t.Run(raw, func(t *testing.T) {
			if _, err := decodeWeChatAgentCursor(raw); err == nil {
				t.Fatalf("accepted invalid cursor %q", raw)
			}
		})
	}
}

func TestWeChatAgentResumesMidPublication(t *testing.T) {
	articleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><body><h1 id="activity-name">Article %s</h1><a id="js_name">Account</a><div id="js_content"><p>Article %s contains enough content for a deterministic source adapter test.</p></div></body></html>`, r.URL.Path, r.URL.Path)
	}))
	defer articleServer.Close()

	discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		articles := []WeChatOfficialArticle{
			{Title: "Article 1", Link: articleServer.URL + "/1", AID: "article-1", UpdateTime: 101},
			{Title: "Article 2", Link: articleServer.URL + "/2", AID: "article-2", UpdateTime: 102},
			{Title: "Article 3", Link: articleServer.URL + "/3", AID: "article-3", UpdateTime: 103},
		}
		publishInfo, _ := json.Marshal(map[string]any{"appmsgex": articles})
		publishPage, _ := json.Marshal(map[string]any{"publish_list": []map[string]any{{"publish_info": string(publishInfo)}}})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base_resp":    map[string]any{"ret": 0},
			"publish_page": string(publishPage),
		})
	}))
	defer discoveryServer.Close()

	sessions := fakeSessionHealthProvider{session: WeChatMPSession{Token: "test-token"}}
	discovery, err := NewWeChatDiscovery(WeChatDiscoveryConfig{
		BaseURL:         discoveryServer.URL,
		HTTPClient:      discoveryServer.Client(),
		SessionProvider: sessions,
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{
		Sessions:  sessions,
		Discovery: discovery,
		Source:    newTestWeChatSourceService(t, articleServer),
	})
	if err != nil {
		t.Fatal(err)
	}

	run := SourceSyncRun{
		ID:                 "run-1",
		RequestedOperation: "sync_articles",
		Subscription: &SourceSubscription{
			SourceAccountKey: "account-key",
			SourceAccount:    "Account",
			Options:          map[string]any{"max_items": float64(1)},
		},
	}
	firstSink := &recordingSourceEnvelopeSink{}
	first, err := adapter.Execute(context.Background(), run, firstSink)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstSink.envelopes) != 1 || firstSink.envelopes[0].SourceItemID != "article-1" {
		t.Fatalf("first envelopes=%#v", firstSink.envelopes)
	}
	firstCursor, err := decodeWeChatAgentCursor(first.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if firstCursor.UpstreamBegin != 0 || firstCursor.PublicationItemIndex != 1 || firstCursor.LastArticleKey != "article-1" {
		t.Fatalf("first cursor=%#v", firstCursor)
	}

	run.ID = "run-2"
	run.Subscription.Cursor = first.Cursor
	secondSink := &recordingSourceEnvelopeSink{}
	second, err := adapter.Execute(context.Background(), run, secondSink)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondSink.envelopes) != 1 || secondSink.envelopes[0].SourceItemID != "article-2" {
		t.Fatalf("second envelopes=%#v", secondSink.envelopes)
	}
	secondCursor, err := decodeWeChatAgentCursor(second.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if secondCursor.UpstreamBegin != 0 || secondCursor.PublicationItemIndex != 2 || secondCursor.LastArticleKey != "article-2" {
		t.Fatalf("second cursor=%#v", secondCursor)
	}

	run.ID = "run-3"
	run.Subscription.Cursor = second.Cursor
	thirdSink := &recordingSourceEnvelopeSink{}
	third, err := adapter.Execute(context.Background(), run, thirdSink)
	if err != nil {
		t.Fatal(err)
	}
	if len(thirdSink.envelopes) != 1 || thirdSink.envelopes[0].SourceItemID != "article-3" {
		t.Fatalf("third envelopes=%#v", thirdSink.envelopes)
	}
	thirdCursor, err := decodeWeChatAgentCursor(third.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if thirdCursor.UpstreamBegin != 1 || thirdCursor.PublicationItemIndex != 0 || thirdCursor.LastArticleKey != "article-3" {
		t.Fatalf("third cursor=%#v", thirdCursor)
	}
}

func TestWeChatAgentDoesNotAdvancePastFailure(t *testing.T) {
	tests := []struct {
		name           string
		failedPath     string
		failedSinkItem string
		wantError      string
	}{
		{name: "article download", failedPath: "/2", wantError: "wechat article request failed"},
		{name: "outbox enqueue", failedSinkItem: "article-2", wantError: "enqueue article-2 failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := newFailureTestWeChatAgentAdapter(t, test.failedPath)
			run := SourceSyncRun{
				ID:                 "run-failure",
				RequestedOperation: "sync_articles",
				Subscription: &SourceSubscription{
					SourceAccountKey: "account-key",
					SourceAccount:    "Account",
					Options:          map[string]any{"max_items": float64(3)},
				},
			}
			sink := &recordingSourceEnvelopeSink{failItem: test.failedSinkItem}
			result, err := adapter.Execute(context.Background(), run, sink)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Execute() error=%v, want %q", err, test.wantError)
			}
			var executionErr *SourceAdapterExecutionError
			if !errors.As(err, &executionErr) {
				t.Fatalf("Execute() error=%T, want SourceAdapterExecutionError", err)
			}
			if result.Cursor == "" || result.Cursor != executionErr.Cursor {
				t.Fatalf("result cursor=%q execution cursor=%q", result.Cursor, executionErr.Cursor)
			}
			cursor, decodeErr := decodeWeChatAgentCursor(executionErr.Cursor)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if cursor.UpstreamBegin != 0 || cursor.PublicationItemIndex != 1 || cursor.LastArticleKey != "article-1" {
				t.Fatalf("safe cursor=%#v", cursor)
			}
			if len(sink.envelopes) != 1 || sink.envelopes[0].SourceItemID != "article-1" {
				t.Fatalf("accepted envelopes=%#v", sink.envelopes)
			}
		})
	}
}

func newFailureTestWeChatAgentAdapter(t *testing.T, failedPath string) *WeChatSourceAdapter {
	t.Helper()
	articleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == failedPath {
			http.Error(w, "failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><body><h1 id="activity-name">Article %s</h1><a id="js_name">Account</a><div id="js_content"><p>Article %s contains enough content for a deterministic source adapter test.</p></div></body></html>`, r.URL.Path, r.URL.Path)
	}))
	t.Cleanup(articleServer.Close)
	discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		articles := []WeChatOfficialArticle{
			{Title: "Article 1", Link: articleServer.URL + "/1", AID: "article-1", UpdateTime: 101},
			{Title: "Article 2", Link: articleServer.URL + "/2", AID: "article-2", UpdateTime: 102},
			{Title: "Article 3", Link: articleServer.URL + "/3", AID: "article-3", UpdateTime: 103},
		}
		publishInfo, _ := json.Marshal(map[string]any{"appmsgex": articles})
		publishPage, _ := json.Marshal(map[string]any{"publish_list": []map[string]any{{"publish_info": string(publishInfo)}}})
		_ = json.NewEncoder(w).Encode(map[string]any{"base_resp": map[string]any{"ret": 0}, "publish_page": string(publishPage)})
	}))
	t.Cleanup(discoveryServer.Close)
	sessions := fakeSessionHealthProvider{session: WeChatMPSession{Token: "test-token"}}
	discovery, err := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: discoveryServer.URL, HTTPClient: discoveryServer.Client(), SessionProvider: sessions})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: sessions, Discovery: discovery, Source: newTestWeChatSourceService(t, articleServer)})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}
