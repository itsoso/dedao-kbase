package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	WeChatMPLoginPending   = "pending"
	WeChatMPLoginScanned   = "scanned"
	WeChatMPLoginExpired   = "expired"
	WeChatMPLoginConfirmed = "confirmed"
	WeChatMPLoginVerify    = "verification_required"
	weChatMPUserAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36 KBaseSourceAgent/1.0"
)

var (
	ErrWeChatMPSessionExpired = errors.New("wechat MP session expired")
	ErrWeChatMPSessionInvalid = errors.New("wechat MP session is invalid")
)

type WeChatMPCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain,omitempty"`
	Path   string `json:"path,omitempty"`
}
type WeChatMPSession struct {
	Token          string           `json:"token"`
	Cookies        []WeChatMPCookie `json:"cookies"`
	AccountName    string           `json:"account_name,omitempty"`
	ObservedExpiry string           `json:"observed_expiry,omitempty"`
}

func (s WeChatMPSession) Validate(now time.Time) error {
	if strings.TrimSpace(s.Token) == "" {
		return ErrWeChatMPSessionInvalid
	}
	value := strings.TrimSpace(s.ObservedExpiry)
	if value == "" {
		return nil
	}
	expiry, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return ErrWeChatMPSessionInvalid
	}
	if !expiry.After(now.UTC()) {
		return ErrWeChatMPSessionExpired
	}
	return nil
}

type WeChatMPLoginStatus struct {
	State          string `json:"state"`
	RequiresAction string `json:"requires_action,omitempty"`
}
type WeChatMPSessionConfig struct {
	BaseURL     string
	HTTPClient  *http.Client
	SecretStore SourceSecretStore
	SecretKey   string
}
type WeChatMPSessionClient struct {
	base           *url.URL
	client         *http.Client
	jar            http.CookieJar
	store          SourceSecretStore
	key            string
	loginMu        sync.Mutex
	loginActive    bool
	loginConfirmed bool
}

func NewWeChatMPSessionClient(cfg WeChatMPSessionConfig) (*WeChatMPSessionClient, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"))
	if err != nil || base.Hostname() == "" || (base.Scheme != "https" && !isLoopbackSourceAgentHost(base.Hostname())) {
		return nil, fmt.Errorf("wechat MP base URL is invalid")
	}
	if cfg.SecretStore == nil {
		return nil, fmt.Errorf("source secret store is required")
	}
	if cfg.SecretKey == "" {
		cfg.SecretKey = "wechat-mp-session"
	}
	jar, _ := cookiejar.New(nil)
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	clone := *client
	clone.Jar = jar
	return &WeChatMPSessionClient{base: base, client: &clone, jar: jar, store: cfg.SecretStore, key: cfg.SecretKey}, nil
}
func (c *WeChatMPSessionClient) endpoint(path string) *url.URL {
	u := *c.base
	u.Path = path
	return &u
}
func (c *WeChatMPSessionClient) StartLogin(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	body, err := c.request(ctx, http.MethodPost, "/cgi-bin/bizlogin", url.Values{"action": {"startlogin"}}, url.Values{
		"userlang":     {"zh_CN"},
		"redirect_url": {""},
		"login_type":   {"3"},
		"sessionid":    {strconv.FormatInt(time.Now().UnixNano(), 10)},
		"token":        {""},
		"lang":         {"zh_CN"},
		"f":            {"json"},
		"ajax":         {"1"},
	})
	if err != nil {
		return err
	}
	var payload struct {
		BaseResp weChatBaseResp `json:"base_resp"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return fmt.Errorf("wechat MP login start response is malformed")
	}
	if payload.BaseResp.Ret != 0 {
		return fmt.Errorf("wechat MP login start was rejected (%d)", payload.BaseResp.Ret)
	}
	c.loginActive = true
	c.loginConfirmed = false
	return nil
}
func (c *WeChatMPSessionClient) QRImage(ctx context.Context) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/cgi-bin/scanloginqrcode", url.Values{
		"action": {"getqrcode"},
		"random": {strconv.FormatInt(time.Now().UnixMilli(), 10)},
	}, nil)
}
func (c *WeChatMPSessionClient) PollLogin(ctx context.Context) (WeChatMPLoginStatus, error) {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if c.loginConfirmed {
		return WeChatMPLoginStatus{State: WeChatMPLoginConfirmed}, nil
	}
	if !c.loginActive {
		if _, err := c.LoadSession(ctx); err == nil {
			c.loginConfirmed = true
			return WeChatMPLoginStatus{State: WeChatMPLoginConfirmed}, nil
		}
		return WeChatMPLoginStatus{State: WeChatMPLoginPending, RequiresAction: "login"}, nil
	}
	body, err := c.request(ctx, http.MethodGet, "/cgi-bin/scanloginqrcode", url.Values{
		"action": {"ask"},
		"token":  {""},
		"lang":   {"zh_CN"},
		"f":      {"json"},
		"ajax":   {"1"},
	}, nil)
	if err != nil {
		return WeChatMPLoginStatus{}, err
	}
	var payload struct {
		Status   json.RawMessage `json:"status"`
		AcctSize int             `json:"acct_size"`
		BaseResp weChatBaseResp  `json:"base_resp"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return WeChatMPLoginStatus{}, fmt.Errorf("wechat MP login response is malformed")
	}
	if payload.BaseResp.Ret != 0 {
		return WeChatMPLoginStatus{}, fmt.Errorf("wechat MP login was rejected (%d)", payload.BaseResp.Ret)
	}
	code, err := decodeWeChatMPScanStatus(payload.Status)
	if err != nil {
		return WeChatMPLoginStatus{}, err
	}
	switch code {
	case 0:
		return WeChatMPLoginStatus{State: WeChatMPLoginPending}, nil
	case 1:
		if err := c.completeLogin(ctx); err != nil {
			return WeChatMPLoginStatus{}, err
		}
		c.loginActive = false
		c.loginConfirmed = true
		return WeChatMPLoginStatus{State: WeChatMPLoginConfirmed}, nil
	case 2, 3:
		return WeChatMPLoginStatus{State: WeChatMPLoginExpired, RequiresAction: "login"}, nil
	case 4, 6:
		if payload.AcctSize == 0 {
			return WeChatMPLoginStatus{State: WeChatMPLoginScanned, RequiresAction: "account"}, nil
		}
		return WeChatMPLoginStatus{State: WeChatMPLoginScanned}, nil
	case 5:
		return WeChatMPLoginStatus{State: WeChatMPLoginVerify, RequiresAction: "account"}, nil
	default:
		return WeChatMPLoginStatus{}, fmt.Errorf("wechat MP login response is malformed")
	}
}

func (c *WeChatMPSessionClient) completeLogin(ctx context.Context) error {
	body, err := c.request(ctx, http.MethodPost, "/cgi-bin/bizlogin", url.Values{"action": {"login"}}, url.Values{
		"userlang":         {"zh_CN"},
		"redirect_url":     {""},
		"cookie_forbidden": {"0"},
		"cookie_cleaned":   {"0"},
		"plugin_used":      {"0"},
		"login_type":       {"3"},
		"token":            {""},
		"lang":             {"zh_CN"},
		"f":                {"json"},
		"ajax":             {"1"},
	})
	if err != nil {
		return err
	}
	var payload struct {
		RedirectURL string         `json:"redirect_url"`
		BaseResp    weChatBaseResp `json:"base_resp"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return fmt.Errorf("wechat MP login completion response is malformed")
	}
	if payload.BaseResp.Ret != 0 {
		return fmt.Errorf("wechat MP login completion was rejected (%d)", payload.BaseResp.Ret)
	}
	token, err := c.validateRedirect(payload.RedirectURL)
	if err != nil {
		return err
	}
	session := WeChatMPSession{Token: token, ObservedExpiry: time.Now().UTC().Add(4 * 24 * time.Hour).Format(time.RFC3339)}
	for _, cookie := range c.jar.Cookies(c.base) {
		session.Cookies = append(session.Cookies, WeChatMPCookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path})
	}
	if len(session.Cookies) == 0 {
		return fmt.Errorf("wechat MP login session is incomplete")
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode wechat MP session failed")
	}
	if err = c.store.Save(ctx, c.key, encoded); err != nil {
		return fmt.Errorf("save wechat MP session failed")
	}
	stored, err := c.LoadSession(ctx)
	if err != nil || !sameWeChatMPSession(stored, session) {
		return fmt.Errorf("verify saved wechat MP session failed")
	}
	return nil
}

func sameWeChatMPSession(left, right WeChatMPSession) bool {
	if left.Token != right.Token || left.ObservedExpiry != right.ObservedExpiry || len(left.Cookies) != len(right.Cookies) {
		return false
	}
	for index := range left.Cookies {
		if left.Cookies[index] != right.Cookies[index] {
			return false
		}
	}
	return true
}

func decodeWeChatMPScanStatus(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("wechat MP login response is malformed")
	}
	var code int
	if err := json.Unmarshal(raw, &code); err == nil {
		return code, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, fmt.Errorf("wechat MP login response is malformed")
	}
	code, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("wechat MP login response is malformed")
	}
	return code, nil
}
func (c *WeChatMPSessionClient) validateRedirect(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.HasPrefix(u.Path, "/cgi-bin/") {
		return "", fmt.Errorf("wechat MP login redirect is invalid")
	}
	if u.Host != "" && !strings.EqualFold(u.Host, c.base.Host) {
		return "", fmt.Errorf("wechat MP login redirect is invalid")
	}
	if u.IsAbs() && !strings.EqualFold(u.Scheme, c.base.Scheme) {
		return "", fmt.Errorf("wechat MP login redirect is invalid")
	}
	token := strings.TrimSpace(u.Query().Get("token"))
	if token == "" {
		return "", fmt.Errorf("wechat MP login redirect is invalid")
	}
	return token, nil
}
func (c *WeChatMPSessionClient) LoadSession(ctx context.Context) (WeChatMPSession, error) {
	raw, err := c.store.Load(ctx, c.key)
	if err != nil {
		return WeChatMPSession{}, err
	}
	var session WeChatMPSession
	if json.Unmarshal(raw, &session) != nil {
		return WeChatMPSession{}, fmt.Errorf("stored wechat MP session is invalid")
	}
	if err := session.Validate(time.Now()); err != nil {
		return WeChatMPSession{}, err
	}
	return session, nil
}
func (c *WeChatMPSessionClient) Logout(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	c.loginActive = false
	c.loginConfirmed = false
	return c.store.Delete(ctx, c.key)
}
func (c *WeChatMPSessionClient) request(ctx context.Context, method, path string, query, form url.Values) ([]byte, error) {
	endpoint := c.endpoint(path)
	endpoint.RawQuery = query.Encode()
	var requestBody io.Reader
	if form != nil {
		requestBody = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	origin := c.base.Scheme + "://" + c.base.Host
	req.Header.Set("Referer", origin+"/")
	req.Header.Set("Origin", origin)
	req.Header.Set("User-Agent", weChatMPUserAgent)
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wechat MP request failed (%s)", classifyWeChatMPRequestError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("wechat MP request failed with HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read wechat MP response failed")
	}
	return body, nil
}

func classifyWeChatMPRequestError(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "eof"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "timeout"
	}
	return "network"
}
