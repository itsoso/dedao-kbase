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

func TestWeChatDiscoveryPaginatesMultiplePublishedItems(t *testing.T) {
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
	items, next, err := discovery.Discover(context.Background(), "account-key", WeChatDiscoveryCursor{}, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ArticleKey == "" || items[0].ArticleKey == items[1].ArticleKey || next.Begin != 2 {
		t.Fatalf("items=%#v next=%#v", items, next)
	}
}

func TestWeChatDiscoveryClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{{"login", `{"base_resp":{"ret":200003}}`, "login_required"}, {"throttle", `{"base_resp":{"ret":200013}}`, "throttled"}, {"verify", `{"base_resp":{"ret":-8}}`, "verification_required"}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			d, _ := NewWeChatDiscovery(WeChatDiscoveryConfig{BaseURL: server.URL, HTTPClient: server.Client(), SessionProvider: staticWeChatSessionProvider{session: WeChatMPSession{Token: "test-value"}}})
			_, _, err := d.Discover(context.Background(), "account", WeChatDiscoveryCursor{}, 5, "")
			if err == nil || WeChatDiscoveryErrorCode(err) != tc.want {
				t.Fatalf("err=%v code=%s", err, WeChatDiscoveryErrorCode(err))
			}
		})
	}
}
