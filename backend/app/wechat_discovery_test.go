package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type staticWeChatSessionProvider struct {
	session WeChatMPSession
	err     error
}

func (p staticWeChatSessionProvider) Session(context.Context) (WeChatMPSession, error) {
	return p.session, p.err
}

func TestWeChatDiscoveryReportsPublicationProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("begin") != "0" || r.URL.Query().Get("count") != "10" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"base_resp":{"ret":0},"publish_page":"{\"publish_list\":[{\"publish_info\":\"{\\\"appmsgex\\\":[{\\\"title\\\":\\\"First\\\",\\\"link\\\":\\\"https://mp.weixin.qq.com/s/a\\\",\\\"aid\\\":\\\"aid-a\\\",\\\"appmsgid\\\":11,\\\"itemidx\\\":1,\\\"update_time\\\":100},{\\\"title\\\":\\\"Second\\\",\\\"link\\\":\\\"https://mp.weixin.qq.com/s/b\\\",\\\"appmsgid\\\":11,\\\"itemidx\\\":2,\\\"update_time\\\":99}]}\"}]}"}`)
	}))
	defer server.Close()
	discovery, err := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: server.URL, HTTPClient: server.Client(), SessionProvider: staticWeChatSessionProvider{session: WeChatMPSession{Token: "test-value", Cookies: []WeChatMPCookie{{Name: "session", Value: "test-value"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := discovery.Discover(context.Background(), "account-key", WeChatDiscoveryCursor{}, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Articles) != 2 || page.Articles[0].ArticleKey == "" || page.Articles[0].ArticleKey == page.Articles[1].ArticleKey {
		t.Fatalf("articles=%#v", page.Articles)
	}
	if page.UpstreamBegin != 0 || page.PublicationCount != 1 {
		t.Fatalf("page progress=%#v", page)
	}
}

func TestWeChatDiscoveryAdvancesFilteredEmptyPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("begin") != "7" {
			t.Fatalf("begin=%s", r.URL.Query().Get("begin"))
		}
		fmt.Fprint(w, `{"base_resp":{"ret":0},"publish_page":"{\"publish_list\":[{\"publish_info\":\"{\\\"appmsgex\\\":[{\\\"title\\\":\\\"Unmatched\\\",\\\"link\\\":\\\"https://mp.weixin.qq.com/s/a\\\",\\\"aid\\\":\\\"aid-a\\\"}]}\"}]}"}`)
	}))
	defer server.Close()
	discovery, err := NewWeChatDiscovery(WeChatDiscoveryConfig{
		BaseURL:         server.URL,
		HTTPClient:      server.Client(),
		SessionProvider: staticWeChatSessionProvider{session: WeChatMPSession{Token: "test-value"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := discovery.Discover(context.Background(), "account-key", WeChatDiscoveryCursor{Begin: 7}, 10, "wanted")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Articles) != 0 {
		t.Fatalf("filtered articles=%#v", page.Articles)
	}
	if page.UpstreamBegin != 7 || page.PublicationCount != 1 {
		t.Fatalf("page progress=%#v", page)
	}
}

func TestWeChatDiscoveryClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{{"login", `{"base_resp":{"ret":200003}}`, "login_required"}, {"throttle", `{"base_resp":{"ret":200013}}`, "throttled"}, {"verify", `{"base_resp":{"ret":-8}}`, "verification_required"}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			d, _ := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: server.URL, HTTPClient: server.Client(), SessionProvider: staticWeChatSessionProvider{session: WeChatMPSession{Token: "test-value"}}})
			_, err := d.Discover(context.Background(), "account", WeChatDiscoveryCursor{}, 5, "")
			if err == nil || WeChatDiscoveryErrorCode(err) != tc.want {
				t.Fatalf("err=%v code=%s", err, WeChatDiscoveryErrorCode(err))
			}
		})
	}
}

func TestWeChatDiscoveryRejectsInsecureRemoteBaseURL(t *testing.T) {
	_, err := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: "http://example.invalid", SessionProvider: staticWeChatSessionProvider{}})
	if err == nil {
		t.Fatal("accepted insecure remote discovery base URL")
	}
}
