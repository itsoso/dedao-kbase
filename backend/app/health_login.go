package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const healthLoginCookie = "__Host-kbase_login"

var healthLoginNonce = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type HealthLoginConfig struct{ Issuer, Origin, Secret, Subject string }
type healthLoginPending struct {
	CookieHash [32]byte
	ClientID   string
	Epoch      int64
	Expires    time.Time
}
type HealthLogin struct {
	config  HealthLoginConfig
	mu      sync.Mutex
	pending map[string]healthLoginPending
}

func healthLoginOrigin(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery && (u.Port() == "" || u.Port() == "443") && value == "https://"+u.Host
}
func NewHealthLogin(c HealthLoginConfig) (*HealthLogin, error) {
	if !healthLoginOrigin(c.Issuer) || !healthLoginOrigin(c.Origin) || c.Issuer == c.Origin || len(c.Secret) < 32 || !regexp.MustCompile(`^[1-9][0-9]{0,18}$`).MatchString(c.Subject) {
		return nil, errors.New("invalid health login configuration")
	}
	return &HealthLogin{config: c, pending: make(map[string]healthLoginPending)}, nil
}
func HealthLoginFromEnv(publicOrigin string) (*HealthLogin, error) {
	if os.Getenv("KBASE_HEALTH_LOGIN_ENABLED") != "true" {
		return nil, nil
	}
	return NewHealthLogin(HealthLoginConfig{Issuer: os.Getenv("KBASE_HEALTH_LOGIN_ISSUER"), Origin: publicOrigin, Secret: os.Getenv("KBASE_HEALTH_LOGIN_SECRET"), Subject: os.Getenv("KBASE_HEALTH_LOGIN_SUBJECT")})
}
func healthLoginRandom() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
func (h *kbaseHTTPHandler) handleHealthLogin(w http.ResponseWriter, r *http.Request) {
	setBrowserSessionNoStore(w)
	w.Header().Set("Referrer-Policy", "no-referrer")
	l := h.healthLogin
	if r.URL.Path == "/browser/health/config" {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		writeHTTPJSON(w, 200, map[string]bool{"enabled": l != nil})
		return
	}
	if l == nil || h.browserSessions.Store == nil || h.browserSessions.PublicOrigin != l.config.Origin {
		writeHTTPError(w, 503, "Health login is not configured")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	switch r.URL.Path {
	case "/browser/health/start":
		h.startHealthLogin(w, r)
	case "/browser/health/callback":
		h.finishHealthLogin(w, r)
	default:
		w.WriteHeader(404)
	}
}
func (h *kbaseHTTPHandler) startHealthLogin(w http.ResponseWriter, r *http.Request) {
	l := h.healthLogin
	origin, ok := singleBoundedHeader(r, "Origin", maxBrowserSessionOriginBytes)
	if !ok || origin != l.config.Origin {
		writeHTTPError(w, 403, "forbidden")
		return
	}
	clientID, ok := singleBoundedHeader(r, browserSessionClientIDHeaderName, maxBrowserSessionClientIDBytes)
	if !ok || validateBrowserSessionClientID(clientID) != nil {
		writeHTTPError(w, 400, "invalid browser client")
		return
	}
	state, err := healthLoginRandom()
	if err != nil {
		writeHTTPError(w, 503, "login unavailable")
		return
	}
	binding, err := healthLoginRandom()
	if err != nil {
		writeHTTPError(w, 503, "login unavailable")
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for key, p := range l.pending {
		if !p.Expires.After(now) {
			delete(l.pending, key)
		}
	}
	if len(l.pending) >= 128 {
		writeHTTPError(w, 429, "too many pending logins")
		return
	}
	family, err := h.browserSessions.Store.AcquireClientEpoch(clientID)
	if err != nil {
		writeBrowserSessionStoreError(w, err)
		return
	}
	l.pending[state] = healthLoginPending{CookieHash: sha256.Sum256([]byte(binding)), ClientID: clientID, Epoch: family.Epoch, Expires: now.Add(5 * time.Minute)}
	http.SetCookie(w, &http.Cookie{Name: healthLoginCookie, Value: binding, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode, MaxAge: 300})
	writeHTTPJSON(w, 200, map[string]string{"authorization_url": l.config.Issuer + "/connect/kbase?state=" + url.QueryEscape(state)})
}

type healthLoginClaims struct {
	Version  int    `json:"v"`
	Purpose  string `json:"purpose"`
	Issuer   string `json:"iss"`
	Audience string `json:"aud"`
	Subject  string `json:"sub"`
	State    string `json:"state"`
	Issued   int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

func (l *HealthLogin) verify(ticket string) (healthLoginClaims, bool) {
	var c healthLoginClaims
	parts := strings.Split(ticket, ".")
	if len(parts) != 2 || len(ticket) > 4096 {
		return c, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, false
	}
	mac := hmac.New(sha256.New, []byte(l.config.Secret))
	mac.Write([]byte("kbase-login-v1." + parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, false
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(body, &c) != nil {
		return c, false
	}
	now := time.Now().Unix()
	return c, c.Version == 1 && c.Purpose == "kbase-browser-login" && c.Issuer == l.config.Issuer && c.Audience == l.config.Origin && c.Subject == l.config.Subject && healthLoginNonce.MatchString(c.State) && c.Expires > now && c.Issued <= now+5 && c.Issued >= now-60 && c.Expires > c.Issued && c.Expires-c.Issued <= 60
}
func (h *kbaseHTTPHandler) finishHealthLogin(w http.ResponseWriter, r *http.Request) {
	l := h.healthLogin
	origin, ok := singleBoundedHeader(r, "Origin", maxBrowserSessionOriginBytes)
	if !ok || origin != l.config.Issuer {
		writeHTTPError(w, 403, "forbidden")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil || len(r.PostForm) != 1 || len(r.PostForm["ticket"]) != 1 || r.URL.RawQuery != "" {
		writeHTTPError(w, 400, "invalid login response")
		return
	}
	claims, valid := l.verify(r.PostForm.Get("ticket"))
	if !valid {
		writeHTTPError(w, 403, "invalid or expired login")
		return
	}
	cookies := r.Cookies()
	var binding string
	count := 0
	for _, cookie := range cookies {
		if cookie.Name == healthLoginCookie {
			binding = cookie.Value
			count++
		}
	}
	if count != 1 || !healthLoginNonce.MatchString(binding) {
		writeHTTPError(w, 403, "login browser mismatch")
		return
	}
	l.mu.Lock()
	pending, exists := l.pending[claims.State]
	hash := sha256.Sum256([]byte(binding))
	valid = exists && pending.Expires.After(time.Now()) && hmac.Equal(hash[:], pending.CookieHash[:])
	if valid {
		delete(l.pending, claims.State)
	}
	l.mu.Unlock()
	if !valid {
		writeHTTPError(w, 403, "invalid or expired login")
		return
	}
	credentials, err := h.browserSessions.Store.Create(BrowserSessionCreate{ClientID: pending.ClientID, ExpectedEpoch: pending.Epoch, DeviceLabel: "Health login", UserAgent: boundedUserAgent(r)})
	if err != nil {
		writeBrowserSessionStoreError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: healthLoginCookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode, MaxAge: -1})
	setBrowserSessionCookie(w, credentials.Token, credentials.Session.ExpiresAt, h.browserSessions.TTL)
	http.Redirect(w, r, "/sources/dedao/courses", http.StatusSeeOther)
}
