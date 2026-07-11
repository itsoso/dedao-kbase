package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const (
	WeChatMPLoginPending   = "pending"
	WeChatMPLoginScanned   = "scanned"
	WeChatMPLoginExpired   = "expired"
	WeChatMPLoginConfirmed = "confirmed"
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
	base   *url.URL
	client *http.Client
	jar    http.CookieJar
	store  SourceSecretStore
	key    string
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
	_, err := c.request(ctx, "/cgi-bin/bizlogin")
	return err
}
func (c *WeChatMPSessionClient) QRImage(ctx context.Context) ([]byte, error) {
	return c.request(ctx, "/cgi-bin/loginqrcode")
}
func (c *WeChatMPSessionClient) PollLogin(ctx context.Context) (WeChatMPLoginStatus, error) {
	body, err := c.request(ctx, "/cgi-bin/scanloginqrcode")
	if err != nil {
		return WeChatMPLoginStatus{}, err
	}
	var payload struct {
		Status      string         `json:"status"`
		RedirectURL string         `json:"redirect_url"`
		BaseResp    weChatBaseResp `json:"base_resp"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return WeChatMPLoginStatus{}, fmt.Errorf("wechat MP login response is malformed")
	}
	if payload.BaseResp.Ret != 0 {
		return WeChatMPLoginStatus{}, fmt.Errorf("wechat MP login was rejected")
	}
	state := strings.ToLower(strings.TrimSpace(payload.Status))
	if state == "" {
		state = WeChatMPLoginPending
	}
	status := WeChatMPLoginStatus{State: state}
	if state == WeChatMPLoginExpired {
		status.RequiresAction = "login"
	}
	if state != WeChatMPLoginConfirmed {
		return status, nil
	}
	token, err := c.validateRedirect(payload.RedirectURL)
	if err != nil {
		return WeChatMPLoginStatus{}, err
	}
	session := WeChatMPSession{Token: token, ObservedExpiry: time.Now().UTC().Add(12 * time.Hour).Format(time.RFC3339)}
	for _, cookie := range c.jar.Cookies(c.base) {
		session.Cookies = append(session.Cookies, WeChatMPCookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path})
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		return WeChatMPLoginStatus{}, fmt.Errorf("encode wechat MP session failed")
	}
	if err = c.store.Save(ctx, c.key, encoded); err != nil {
		return WeChatMPLoginStatus{}, fmt.Errorf("save wechat MP session failed")
	}
	return status, nil
}
func (c *WeChatMPSessionClient) validateRedirect(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.IsAbs() || !strings.HasPrefix(u.Path, "/cgi-bin/") {
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
	if json.Unmarshal(raw, &session) != nil || session.Token == "" {
		return WeChatMPSession{}, fmt.Errorf("stored wechat MP session is invalid")
	}
	return session, nil
}
func (c *WeChatMPSessionClient) Logout(ctx context.Context) error { return c.store.Delete(ctx, c.key) }
func (c *WeChatMPSessionClient) request(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(path).String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wechat MP request failed")
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
