package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func loginFixture(t *testing.T) (*kbaseHTTPHandler, *HealthLogin) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: filepath.Join(dir, "sessions.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	login, err := NewHealthLogin(HealthLoginConfig{Issuer: "https://health.example.test", Origin: "https://library.example.test", Secret: "synthetic-test-secret-with-at-least-32-bytes", Subject: "7"})
	if err != nil {
		t.Fatal(err)
	}
	return &kbaseHTTPHandler{healthLogin: login, browserSessions: BrowserSessionHTTPConfig{Store: store, PublicOrigin: login.config.Origin, TTL: time.Hour}}, login
}
func signedTicket(t *testing.T, l *HealthLogin, state string, modify func(map[string]any)) string {
	t.Helper()
	now := time.Now().Unix()
	claims := map[string]any{"v": 1, "purpose": "kbase-browser-login", "iss": l.config.Issuer, "aud": l.config.Origin, "sub": "7", "state": state, "iat": now, "exp": now + 60}
	if modify != nil {
		modify(claims)
	}
	b, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(l.config.Secret))
	mac.Write([]byte("kbase-login-v1." + payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func beginLogin(t *testing.T, h *kbaseHTTPHandler) (string, *http.Cookie) {
	t.Helper()
	r := httptest.NewRequest("POST", h.healthLogin.config.Origin+"/browser/health/start", nil)
	r.Header.Set("Origin", h.healthLogin.config.Origin)
	r.Header.Set(browserSessionClientIDHeaderName, "synthetic-client-1234567890")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	u, _ := url.Parse(result["authorization_url"])
	return u.Query().Get("state"), w.Result().Cookies()[0]
}
func callback(h *kbaseHTTPHandler, ticket string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", h.healthLogin.config.Origin+"/browser/health/callback", strings.NewReader(url.Values{"ticket": {ticket}}.Encode()))
	r.Header.Set("Origin", h.healthLogin.config.Issuer)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestHealthLoginSuccessAndReplay(t *testing.T) {
	h, l := loginFixture(t)
	state, cookie := beginLogin(t, h)
	ticket := signedTicket(t, l, state, nil)
	w := callback(h, ticket, cookie)
	if w.Code != 303 {
		t.Fatalf("callback: %d %s", w.Code, w.Body.String())
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == browserSessionCookieName {
			session = c
		}
	}
	if session == nil || !session.Secure || !session.HttpOnly || session.Domain != "" {
		t.Fatal("missing secure host-only session")
	}
	if _, err := h.browserSessions.Store.Authenticate(session.Value); err != nil {
		t.Fatal(err)
	}
	if callback(h, ticket, cookie).Code != 403 {
		t.Fatal("replay accepted")
	}
}
func TestHealthLoginRejectsInvalidClaims(t *testing.T) {
	changes := map[string]func(map[string]any){"subject": func(m map[string]any) { m["sub"] = "8" }, "audience": func(m map[string]any) { m["aud"] = "https://evil.test" }, "issuer": func(m map[string]any) { m["iss"] = "https://evil.test" }, "expired": func(m map[string]any) { m["exp"] = time.Now().Unix() - 1 }, "future": func(m map[string]any) { m["iat"] = time.Now().Unix() + 30 }, "long lived": func(m map[string]any) { m["exp"] = time.Now().Unix() + 3600 }, "purpose": func(m map[string]any) { m["purpose"] = "api" }, "state": func(m map[string]any) { m["state"] = strings.Repeat("b", 43) }}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			h, l := loginFixture(t)
			state, c := beginLogin(t, h)
			w := callback(h, signedTicket(t, l, state, change), c)
			if w.Code != 403 {
				t.Fatalf("accepted: %d", w.Code)
			}
		})
	}
}
func TestHealthLoginRequiresBrowserBinding(t *testing.T) {
	h, l := loginFixture(t)
	state, c := beginLogin(t, h)
	ticket := signedTicket(t, l, state, nil)
	if callback(h, ticket, nil).Code != 403 {
		t.Fatal("missing cookie accepted")
	}
	c.Value = "attacker"
	if callback(h, ticket, c).Code != 403 {
		t.Fatal("wrong cookie accepted")
	}
}
func TestHealthLoginConcurrentReplay(t *testing.T) {
	h, l := loginFixture(t)
	state, c := beginLogin(t, h)
	ticket := signedTicket(t, l, state, nil)
	codes := make(chan int, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- callback(h, ticket, c).Code }()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 303 {
			success++
		} else if code != 403 {
			t.Fatalf("status %d", code)
		}
	}
	if success != 1 {
		t.Fatalf("successes %d", success)
	}
}
func TestHealthLoginPendingCannotSurviveLogout(t *testing.T) {
	h, l := loginFixture(t)
	first, cookie := beginLogin(t, h)
	logged := callback(h, signedTicket(t, l, first, nil), cookie)
	var sessionToken string
	for _, c := range logged.Result().Cookies() {
		if c.Name == browserSessionCookieName {
			sessionToken = c.Value
		}
	}
	session, err := h.browserSessions.Store.Authenticate(sessionToken)
	if err != nil {
		t.Fatal(err)
	}
	state, binding := beginLogin(t, h)
	if _, err = h.browserSessions.Store.FenceClientBySession(session.ID, "logout"); err != nil {
		t.Fatal(err)
	}
	if got := callback(h, signedTicket(t, l, state, nil), binding).Code; got != 409 {
		t.Fatalf("expected stale epoch 409, got %d", got)
	}
}

func TestHealthLoginOriginsAndTampering(t *testing.T) {
	h, l := loginFixture(t)
	r := httptest.NewRequest("POST", l.config.Origin+"/browser/health/start", nil)
	r.Header.Set("Origin", "https://attacker.test")
	r.Header.Set(browserSessionClientIDHeaderName, "synthetic-client-1234567890")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin start accepted")
	}
	state, c := beginLogin(t, h)
	ticket := signedTicket(t, l, state, nil)
	if callback(h, ticket+"x", c).Code != 403 {
		t.Fatal("bad signature accepted")
	}
	r = httptest.NewRequest("POST", l.config.Origin+"/browser/health/callback", strings.NewReader(url.Values{"ticket": {ticket}}.Encode()))
	r.Header.Set("Origin", "https://attacker.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin callback accepted")
	}
	if callback(h, ticket, c).Code != 303 {
		t.Fatal("rejected request consumed legitimate transaction")
	}
}

func TestHealthLoginDisabledAndBadConfig(t *testing.T) {
	h := &kbaseHTTPHandler{}
	w := httptest.NewRecorder()
	h.handleHealthLogin(w, httptest.NewRequest("POST", "https://library.example.test/browser/health/start", nil))
	if w.Code != 503 {
		t.Fatal("disabled login accepted")
	}
	for _, c := range []HealthLoginConfig{{}, {Issuer: "http://health.example.test", Origin: "https://library.example.test", Secret: strings.Repeat("s", 32), Subject: "7"}, {Issuer: "https://health.example.test", Origin: "https://library.example.test", Secret: "short", Subject: "7"}} {
		if _, err := NewHealthLogin(c); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
