package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yann0917/dedao-gui/backend/app"
)

type fakeEnrollmentLogin struct{ started, loggedOut bool }

func (f *fakeEnrollmentLogin) StartLogin(_ context.Context) error { f.started = true; return nil }
func (f *fakeEnrollmentLogin) QRImage(_ context.Context) ([]byte, string, error) {
	return []byte("image"), "image/png", nil
}
func (f *fakeEnrollmentLogin) LoginStatus(_ context.Context) (any, error) {
	return map[string]any{"state": "pending"}, nil
}
func (f *fakeEnrollmentLogin) Logout(_ context.Context) error { f.loggedOut = true; return nil }

type fakeEnrollmentDiscovery struct{}

func (fakeEnrollmentDiscovery) SearchOfficialAccounts(context.Context, string) ([]app.WeChatOfficialAccount, error) {
	return []app.WeChatOfficialAccount{{Nickname: "Health Reference", FakeID: "fake-health"}}, nil
}

func (fakeEnrollmentDiscovery) ListOfficialAccountArticles(context.Context, string, int, int) ([]app.WeChatOfficialArticle, error) {
	return []app.WeChatOfficialArticle{{Title: "Evidence article", Link: "https://mp.weixin.qq.com/s/evidence", AID: "aid-evidence"}}, nil
}

func TestEnrollmentRequiresLoopbackOriginAndCSRF(t *testing.T) {
	login := &fakeEnrollmentLogin{}
	handler, err := newEnrollmentHandler(login, nil, enrollmentHandlerConfig{CSRFToken: "csrf-value"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/local/wechat/login/start", nil)
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden || login.started {
		t.Fatalf("missing csrf status=%d started=%v", resp.Code, login.started)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/local/wechat/login/start", nil)
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "https://example.invalid")
	req.Header.Set("X-CSRF-Token", "csrf-value")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("remote origin status=%d", resp.Code)
	}
}

func TestEnrollmentServesOperableLocalPage(t *testing.T) {
	handler, err := newEnrollmentHandler(&fakeEnrollmentLogin{}, fakeEnrollmentDiscovery{}, enrollmentHandlerConfig{
		CSRFToken: "csrf-value",
		RemoteURL: "https://kbase.example.invalid",
		AgentID:   "agent-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "127.0.0.1:8765"
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, body)
	}
	for _, marker := range []string{"source-agent-enrollment", "login-start", "login-qr", "account-search", "account-results", "article-results", "csrf-value", "https://kbase.example.invalid"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page missing %q", marker)
		}
	}
}

func TestEnrollmentSearchesAccountsAndListsArticles(t *testing.T) {
	handler, err := newEnrollmentHandler(&fakeEnrollmentLogin{}, fakeEnrollmentDiscovery{}, enrollmentHandlerConfig{CSRFToken: "csrf-value"})
	if err != nil {
		t.Fatal(err)
	}
	search := httptest.NewRequest(http.MethodGet, "/api/local/wechat/accounts?q=health", nil)
	search.Host = "127.0.0.1:8765"
	search.Header.Set("Origin", "http://127.0.0.1:8765")
	searchResponse := httptest.NewRecorder()
	handler.ServeHTTP(searchResponse, search)
	if searchResponse.Code != http.StatusOK || !strings.Contains(searchResponse.Body.String(), "fake-health") {
		t.Fatalf("search status=%d body=%s", searchResponse.Code, searchResponse.Body.String())
	}
	articles := httptest.NewRequest(http.MethodGet, "/api/local/wechat/articles?fakeid=fake-health&begin=0&count=10", nil)
	articles.Host = "127.0.0.1:8765"
	articles.Header.Set("Origin", "http://127.0.0.1:8765")
	articleResponse := httptest.NewRecorder()
	handler.ServeHTTP(articleResponse, articles)
	if articleResponse.Code != http.StatusOK || !strings.Contains(articleResponse.Body.String(), "aid-evidence") {
		t.Fatalf("articles status=%d body=%s", articleResponse.Code, articleResponse.Body.String())
	}
}
