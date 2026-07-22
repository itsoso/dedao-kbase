package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type recordingSourceEnvelopeSink struct {
	envelopes []SourceArticleEnvelope
	failItem  string
	failErr   error
}

type recordingWeChatAssetUploader struct {
	assets []SourceAssetEnvelope
	fail   bool
}

func (u *recordingWeChatAssetUploader) UploadAsset(_ context.Context, _ string, asset SourceAssetEnvelope) (SourceAssetReference, error) {
	if u.fail {
		return SourceAssetReference{}, fmt.Errorf("asset upload failed")
	}
	u.assets = append(u.assets, asset)
	return SourceAssetReference{SHA256: asset.SHA256, ContentType: asset.ContentType, Size: int64(len(asset.Data))}, nil
}

func (s *recordingSourceEnvelopeSink) Enqueue(_ string, envelope SourceArticleEnvelope) (SourceAgentOutboxItem, error) {
	if envelope.SourceItemID == s.failItem {
		if s.failErr != nil {
			return SourceAgentOutboxItem{}, s.failErr
		}
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

func TestWeChatAgentReportsLoginRequiredForExpiredSession(t *testing.T) {
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: fakeSessionHealthProvider{session: WeChatMPSession{
		Token:          "expired-token",
		ObservedExpiry: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	health := adapter.Status(context.Background())
	if health.Healthy || health.RequiresAction != "login" {
		t.Fatalf("health=%#v", health)
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
			{Title: "Article 1", Link: articleServer.URL + "/1", AID: "article-1", UpdateTime: 103},
			{Title: "Article 2", Link: articleServer.URL + "/2", AID: "article-2", UpdateTime: 102},
			{Title: "Article 3", Link: articleServer.URL + "/3", AID: "article-3", UpdateTime: 101},
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

func TestWeChatAgentRecordsPermanentArticleRejectionAndContinues(t *testing.T) {
	adapter := newFailureTestWeChatAgentAdapter(t, "")
	sink := &recordingSourceEnvelopeSink{failItem: "article-2", failErr: ErrSourceArticleContentTooShort}
	result, err := adapter.Execute(context.Background(), SourceSyncRun{
		ID:                 "run-rejection",
		RequestedOperation: "sync_articles",
		Subscription: &SourceSubscription{
			SourceAccountKey: "account-key",
			SourceAccount:    "Account",
			Options:          map[string]any{"max_items": float64(3)},
		},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) != 1 || result.Failures[0].SourceItemKey != "article-2" {
		t.Fatalf("failures=%#v", result.Failures)
	}
	if len(sink.envelopes) != 2 || sink.envelopes[0].SourceItemID != "article-1" || sink.envelopes[1].SourceItemID != "article-3" {
		t.Fatalf("envelopes=%#v", sink.envelopes)
	}
	cursor, err := decodeWeChatAgentCursor(result.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.UpstreamBegin != 1 || cursor.PublicationItemIndex != 0 {
		t.Fatalf("cursor=%#v", cursor)
	}
}

func TestWeChatAgentSyncMediaUploadsAssetsAndRewritesArticle(t *testing.T) {
	mediaData := []byte("\x89PNG\r\n\x1a\nsanitized-media")
	mediaServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(mediaData)
	}))
	defer mediaServer.Close()
	mediaURL, err := url.Parse(mediaServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	articleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><body><h1 id="activity-name">Article with media</h1><a id="js_name">Account</a><div id="js_content"><p>Article content is long enough for deterministic ingestion.</p><img data-src="%s" alt="chart"></div></body></html>`, mediaServer.URL+"/chart.png")
	}))
	defer articleServer.Close()
	discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		publishInfo, _ := json.Marshal(map[string]any{"appmsgex": []WeChatOfficialArticle{{Title: "Article with media", Link: articleServer.URL + "/article", AID: "article-media", UpdateTime: 101}}})
		publishPage, _ := json.Marshal(map[string]any{"publish_list": []map[string]any{{"publish_info": string(publishInfo)}}})
		_ = json.NewEncoder(w).Encode(map[string]any{"base_resp": map[string]any{"ret": 0}, "publish_page": string(publishPage)})
	}))
	defer discoveryServer.Close()
	sessions := fakeSessionHealthProvider{session: WeChatMPSession{Token: "test-token"}}
	discovery, err := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: discoveryServer.URL, HTTPClient: discoveryServer.Client(), SessionProvider: sessions})
	if err != nil {
		t.Fatal(err)
	}
	uploader := &recordingWeChatAssetUploader{}
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{
		Sessions:  sessions,
		Discovery: discovery,
		Source:    newTestWeChatSourceService(t, articleServer),
		Media: NewWeChatMediaDownloader(WeChatMediaConfig{
			HTTPClient:  mediaServer.Client(),
			Hosts:       []string{mediaURL.Hostname()},
			ResolveHost: publicTestResolver,
		}),
		Assets: uploader,
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSourceEnvelopeSink{}
	result, err := adapter.Execute(context.Background(), SourceSyncRun{
		ID:                 "run-media",
		RequestedOperation: "sync_media",
		Subscription: &SourceSubscription{
			SourceAccountKey: "account-key",
			SourceAccount:    "Account",
			Options:          map[string]any{"max_items": float64(1)},
		},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(uploader.assets) != 1 || len(sink.envelopes) != 1 {
		t.Fatalf("assets=%d envelopes=%d", len(uploader.assets), len(sink.envelopes))
	}
	privateURL := "/api/source-assets/" + uploader.assets[0].SHA256
	if !strings.Contains(sink.envelopes[0].Content, privateURL) || strings.Contains(sink.envelopes[0].Content, mediaServer.URL) {
		t.Fatalf("rewritten content=%s", sink.envelopes[0].Content)
	}
	cursor, err := decodeWeChatAgentCursor(result.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.UpstreamBegin != 1 || cursor.PublicationItemIndex != 0 {
		t.Fatalf("cursor=%#v", cursor)
	}

	uploader.fail = true
	partialSink := &recordingSourceEnvelopeSink{}
	partial, err := adapter.Execute(context.Background(), SourceSyncRun{
		ID:                 "run-media-partial",
		RequestedOperation: "sync_media",
		Subscription: &SourceSubscription{
			SourceAccountKey: "account-key",
			SourceAccount:    "Account",
			Options:          map[string]any{"max_items": float64(1)},
		},
	}, partialSink)
	if err != nil {
		t.Fatalf("partial Execute() error=%v", err)
	}
	if len(partialSink.envelopes) != 1 || len(partial.Failures) != 1 || partial.Failures[0].SourceItemKey != "article-media" {
		t.Fatalf("partial=%#v envelopes=%#v", partial, partialSink.envelopes)
	}
}

func TestWeChatAgentIdempotencyKeyIsScopedToSourceItem(t *testing.T) {
	first := weChatArticleIdempotencyKey("account", "article-1", 100, "same content")
	second := weChatArticleIdempotencyKey("account", "article-2", 100, "same content")
	republished := weChatArticleIdempotencyKey("account", "article-1", 200, "same content")
	if first == second || first == republished || first != weChatArticleIdempotencyKey("account", "article-1", 100, "same content") {
		t.Fatalf("first=%q second=%q republished=%q", first, second, republished)
	}
}

func TestWeChatAgentCommitsFrontierAndOnlyProcessesNewArticles(t *testing.T) {
	articleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><body><h1 id="activity-name">Article %s</h1><a id="js_name">Account</a><div id="js_content"><p>Article %s contains enough content for deterministic frontier testing.</p></div></body></html>`, r.URL.Path, r.URL.Path)
	}))
	defer articleServer.Close()
	articles := []WeChatOfficialArticle{
		{Title: "Article 1", Link: articleServer.URL + "/1", AID: "article-1", UpdateTime: 102},
		{Title: "Article 2", Link: articleServer.URL + "/2", AID: "article-2", UpdateTime: 101},
	}
	discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		begin, _ := strconv.Atoi(r.URL.Query().Get("begin"))
		publishList := []map[string]any{}
		if begin == 0 {
			publishInfo, _ := json.Marshal(map[string]any{"appmsgex": articles})
			publishList = append(publishList, map[string]any{"publish_info": string(publishInfo)})
		}
		publishPage, _ := json.Marshal(map[string]any{"publish_list": publishList})
		_ = json.NewEncoder(w).Encode(map[string]any{"base_resp": map[string]any{"ret": 0}, "publish_page": string(publishPage)})
	}))
	defer discoveryServer.Close()
	sessions := fakeSessionHealthProvider{session: WeChatMPSession{Token: "test-token"}}
	discovery, err := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: discoveryServer.URL, HTTPClient: discoveryServer.Client(), SessionProvider: sessions})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: sessions, Discovery: discovery, Source: newTestWeChatSourceService(t, articleServer)})
	if err != nil {
		t.Fatal(err)
	}
	run := SourceSyncRun{ID: "run-frontier", RequestedOperation: "sync_articles", Subscription: &SourceSubscription{SourceAccountKey: "account-key", SourceAccount: "Account", Options: map[string]any{"max_items": float64(10)}}}

	firstSink := &recordingSourceEnvelopeSink{}
	first, err := adapter.Execute(context.Background(), run, firstSink)
	if err != nil || len(firstSink.envelopes) != 2 {
		t.Fatalf("first result=%#v envelopes=%d err=%v", first, len(firstSink.envelopes), err)
	}
	run.Subscription.Cursor = first.Cursor
	secondSink := &recordingSourceEnvelopeSink{}
	second, err := adapter.Execute(context.Background(), run, secondSink)
	if err != nil || len(secondSink.envelopes) != 0 {
		t.Fatalf("second result=%#v envelopes=%d err=%v", second, len(secondSink.envelopes), err)
	}
	secondCursor, err := decodeWeChatAgentCursor(second.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if secondCursor.UpstreamBegin != 0 || secondCursor.FrontierArticleKey != "article-1" {
		t.Fatalf("committed cursor=%#v", secondCursor)
	}

	run.Subscription.Cursor = second.Cursor
	unchangedSink := &recordingSourceEnvelopeSink{}
	unchanged, err := adapter.Execute(context.Background(), run, unchangedSink)
	if err != nil || len(unchangedSink.envelopes) != 0 {
		t.Fatalf("unchanged result=%#v envelopes=%d err=%v", unchanged, len(unchangedSink.envelopes), err)
	}
	articles = append([]WeChatOfficialArticle{{Title: "Article 3", Link: articleServer.URL + "/3", AID: "article-3", UpdateTime: 103}}, articles...)
	run.Subscription.Cursor = unchanged.Cursor
	newSink := &recordingSourceEnvelopeSink{}
	newResult, err := adapter.Execute(context.Background(), run, newSink)
	if err != nil || len(newSink.envelopes) != 1 || newSink.envelopes[0].SourceItemID != "article-3" {
		t.Fatalf("new result=%#v envelopes=%#v err=%v", newResult, newSink.envelopes, err)
	}
	newCursor, err := decodeWeChatAgentCursor(newResult.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if newCursor.UpstreamBegin != 0 || newCursor.FrontierArticleKey != "article-3" {
		t.Fatalf("new cursor=%#v", newCursor)
	}
}

func TestWeChatAgentDrainsAllAvailablePagesInOneRunNewestFirst(t *testing.T) {
	articleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/oldest" {
			fmt.Fprint(w, `<html><body><a id="js_name">Account</a><div id="js_content"><p>This article contains enough content to verify a missing parsed title fallback.</p></div></body></html>`)
			return
		}
		fmt.Fprintf(w, `<html><body><h1 id="activity-name">%s</h1><a id="js_name">Account</a><div id="js_content"><p>This article contains enough content to verify a complete subscription sync.</p></div></body></html>`, r.URL.Path)
	}))
	defer articleServer.Close()

	var begins []int
	discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		begin, _ := strconv.Atoi(r.URL.Query().Get("begin"))
		begins = append(begins, begin)
		publications := []map[string]any{}
		switch begin {
		case 0:
			for _, article := range []WeChatOfficialArticle{
				{Title: "Second", Link: articleServer.URL + "/second", AID: "second", UpdateTime: 200},
				{Title: "Newest", Link: articleServer.URL + "/newest", AID: "newest", UpdateTime: 300},
			} {
				publishInfo, _ := json.Marshal(map[string]any{"appmsgex": []WeChatOfficialArticle{article}})
				publications = append(publications, map[string]any{"publish_info": string(publishInfo)})
			}
		case 2:
			publishInfo, _ := json.Marshal(map[string]any{"appmsgex": []WeChatOfficialArticle{{Title: "Oldest", Link: articleServer.URL + "/oldest", AID: "oldest", UpdateTime: 100}}})
			publications = append(publications, map[string]any{"publish_info": string(publishInfo)})
		}
		publishPage, _ := json.Marshal(map[string]any{"publish_list": publications})
		_ = json.NewEncoder(w).Encode(map[string]any{"base_resp": map[string]any{"ret": 0}, "publish_page": string(publishPage)})
	}))
	defer discoveryServer.Close()

	sessions := fakeSessionHealthProvider{session: WeChatMPSession{Token: "test-token"}}
	discovery, err := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: discoveryServer.URL, HTTPClient: discoveryServer.Client(), SessionProvider: sessions})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: sessions, Discovery: discovery, Source: newTestWeChatSourceService(t, articleServer)})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSourceEnvelopeSink{}
	result, err := adapter.Execute(context.Background(), SourceSyncRun{
		ID:                 "run-drain",
		RequestedOperation: "sync_articles",
		Subscription: &SourceSubscription{
			SourceAccountKey: "account-key",
			SourceAccount:    "Account",
			Options:          map[string]any{"page_size": float64(2), "max_items": float64(10)},
		},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.envelopes) != 3 {
		t.Fatalf("processed envelopes=%#v", sink.envelopes)
	}
	if got := []string{sink.envelopes[0].SourceItemID, sink.envelopes[1].SourceItemID, sink.envelopes[2].SourceItemID}; !reflect.DeepEqual(got, []string{"newest", "second", "oldest"}) {
		t.Fatalf("processed order=%v", got)
	}
	if sink.envelopes[2].Title != "Oldest" {
		t.Fatalf("fallback title=%q", sink.envelopes[2].Title)
	}
	if got := []string{sink.envelopes[0].PublishedAt, sink.envelopes[1].PublishedAt, sink.envelopes[2].PublishedAt}; !reflect.DeepEqual(got, []string{
		time.Unix(300, 0).UTC().Format(time.RFC3339),
		time.Unix(200, 0).UTC().Format(time.RFC3339),
		time.Unix(100, 0).UTC().Format(time.RFC3339),
	}) {
		t.Fatalf("published times=%v", got)
	}
	if !reflect.DeepEqual(begins, []int{0, 2, 3}) {
		t.Fatalf("discovery begins=%v", begins)
	}
	cursor, err := decodeWeChatAgentCursor(result.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.UpstreamBegin != 0 || cursor.PublicationItemIndex != 0 || cursor.FrontierArticleKey != "newest" {
		t.Fatalf("cursor=%#v", cursor)
	}
}

func TestWeChatAgentSkipsUntitledArticleAndContinuesRun(t *testing.T) {
	articleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/untitled" {
			fmt.Fprint(w, `<html><body><a id="js_name">Account</a><div id="js_content"><p>This untitled article still has enough content to parse.</p></div></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><body><h1 id="activity-name">Valid article</h1><a id="js_name">Account</a><div id="js_content"><p>This valid article must still be synchronized after a malformed item.</p></div></body></html>`)
	}))
	defer articleServer.Close()

	discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publications := []map[string]any{}
		if r.URL.Query().Get("begin") == "0" {
			for _, article := range []WeChatOfficialArticle{
				{Link: articleServer.URL + "/untitled", AID: "untitled", UpdateTime: 200},
				{Title: "Valid article", Link: articleServer.URL + "/valid", AID: "valid", UpdateTime: 100},
			} {
				publishInfo, _ := json.Marshal(map[string]any{"appmsgex": []WeChatOfficialArticle{article}})
				publications = append(publications, map[string]any{"publish_info": string(publishInfo)})
			}
		}
		publishPage, _ := json.Marshal(map[string]any{"publish_list": publications})
		_ = json.NewEncoder(w).Encode(map[string]any{"base_resp": map[string]any{"ret": 0}, "publish_page": string(publishPage)})
	}))
	defer discoveryServer.Close()

	sessions := fakeSessionHealthProvider{session: WeChatMPSession{Token: "test-token"}}
	discovery, err := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: discoveryServer.URL, HTTPClient: discoveryServer.Client(), SessionProvider: sessions})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: sessions, Discovery: discovery, Source: newTestWeChatSourceService(t, articleServer)})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSourceEnvelopeSink{}
	result, err := adapter.Execute(context.Background(), SourceSyncRun{
		ID:                 "run-untitled",
		RequestedOperation: "sync_articles",
		Subscription: &SourceSubscription{
			SourceAccountKey: "account-key",
			SourceAccount:    "Account",
			Options:          map[string]any{"page_size": float64(10), "max_items": float64(10)},
		},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.envelopes) != 1 || sink.envelopes[0].SourceItemID != "valid" {
		t.Fatalf("envelopes=%#v", sink.envelopes)
	}
	if len(result.Failures) != 1 || result.Failures[0].SourceItemKey != "untitled" {
		t.Fatalf("failures=%#v", result.Failures)
	}
}

func TestWeChatAgentSetsInitialFrontierToNewestFilteredMatch(t *testing.T) {
	articleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><h1 id="activity-name">%s</h1><a id="js_name">Account</a><div id="js_content"><p>This filtered article contains enough content for frontier testing.</p></div></body></html>`, r.URL.Path)
	}))
	defer articleServer.Close()
	discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		begin, _ := strconv.Atoi(r.URL.Query().Get("begin"))
		publishList := []map[string]any{}
		if begin < 3 {
			title := "Ignored article"
			aid := "ignored"
			if begin > 0 {
				title = "Selected article"
				aid = fmt.Sprintf("selected-%d", begin)
			}
			publishInfo, _ := json.Marshal(map[string]any{"appmsgex": []WeChatOfficialArticle{{Title: title, Link: articleServer.URL + "/" + aid, AID: aid, UpdateTime: int64(103 - begin)}}})
			publishList = append(publishList, map[string]any{"publish_info": string(publishInfo)})
		}
		publishPage, _ := json.Marshal(map[string]any{"publish_list": publishList})
		_ = json.NewEncoder(w).Encode(map[string]any{"base_resp": map[string]any{"ret": 0}, "publish_page": string(publishPage)})
	}))
	defer discoveryServer.Close()
	sessions := fakeSessionHealthProvider{session: WeChatMPSession{Token: "test-token"}}
	discovery, err := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: discoveryServer.URL, HTTPClient: discoveryServer.Client(), SessionProvider: sessions})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: sessions, Discovery: discovery, Source: newTestWeChatSourceService(t, articleServer)})
	if err != nil {
		t.Fatal(err)
	}
	run := SourceSyncRun{ID: "run-filtered-frontier", RequestedOperation: "sync_articles", Subscription: &SourceSubscription{SourceAccountKey: "account-key", SourceAccount: "Account", Options: map[string]any{"max_items": float64(10), "title_query": "Selected"}}}
	for cycle := 0; cycle < 4; cycle++ {
		result, executeErr := adapter.Execute(context.Background(), run, &recordingSourceEnvelopeSink{})
		if executeErr != nil {
			t.Fatal(executeErr)
		}
		run.Subscription.Cursor = result.Cursor
	}
	cursor, err := decodeWeChatAgentCursor(run.Subscription.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.FrontierArticleKey != "selected-1" || cursor.UpstreamBegin != 0 {
		t.Fatalf("cursor=%#v", cursor)
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
			{Title: "Article 1", Link: articleServer.URL + "/1", AID: "article-1", UpdateTime: 103},
			{Title: "Article 2", Link: articleServer.URL + "/2", AID: "article-2", UpdateTime: 102},
			{Title: "Article 3", Link: articleServer.URL + "/3", AID: "article-3", UpdateTime: 101},
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
