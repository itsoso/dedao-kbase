package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type WeChatMPSessionProvider interface {
	Session(context.Context) (WeChatMPSession, error)
}
type WeChatDiscoveryCursor struct {
	Begin          int    `json:"begin"`
	LastArticleKey string `json:"last_article_key,omitempty"`
	LastTimestamp  int64  `json:"last_timestamp,omitempty"`
}
type WeChatDiscoveredArticle struct {
	WeChatOfficialArticle
	ArticleKey string `json:"article_key"`
}
type WeChatDiscoveryPage struct {
	Articles         []WeChatDiscoveredArticle `json:"articles"`
	UpstreamBegin    int                       `json:"upstream_begin"`
	PublicationCount int                       `json:"publication_count"`
}
type WeChatDiscoveryConfig struct {
	BaseURL         string
	HTTPClient      *http.Client
	SessionProvider WeChatMPSessionProvider
}
type WeChatDiscovery struct {
	baseURL  string
	client   *http.Client
	sessions WeChatMPSessionProvider
}
type WeChatDiscoveryError struct{ Code string }

func (e *WeChatDiscoveryError) Error() string { return "wechat discovery failed: " + e.Code }
func WeChatDiscoveryErrorCode(err error) string {
	if typed, ok := err.(*WeChatDiscoveryError); ok {
		return typed.Code
	}
	return ""
}
func NewWeChatDiscovery(cfg WeChatDiscoveryConfig) (*WeChatDiscovery, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && !isLoopbackSourceAgentHost(parsed.Hostname())) {
		return nil, fmt.Errorf("wechat discovery base URL is invalid")
	}
	if cfg.SessionProvider == nil {
		return nil, fmt.Errorf("wechat MP session provider is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &WeChatDiscovery{baseURL: base, client: client, sessions: cfg.SessionProvider}, nil
}
func (d *WeChatDiscovery) Discover(ctx context.Context, account string, cursor WeChatDiscoveryCursor, pageSize int, titleQuery string) (WeChatDiscoveryPage, error) {
	result := WeChatDiscoveryPage{UpstreamBegin: cursor.Begin}
	session, err := d.sessions.Session(ctx)
	if err != nil || session.Token == "" {
		return result, &WeChatDiscoveryError{Code: "login_required"}
	}
	if pageSize <= 0 {
		pageSize = 5
	}
	if pageSize > 20 {
		pageSize = 20
	}
	values := url.Values{"sub": {"list"}, "begin": {strconv.Itoa(cursor.Begin)}, "count": {strconv.Itoa(pageSize)}, "fakeid": {strings.TrimSpace(account)}, "token": {session.Token}, "lang": {"zh_CN"}, "f": {"json"}, "ajax": {"1"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/cgi-bin/appmsgpublish?"+values.Encode(), nil)
	for _, cookie := range session.Cookies {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return result, fmt.Errorf("wechat discovery request failed")
	}
	defer resp.Body.Close()
	var outer struct {
		BaseResp    weChatBaseResp `json:"base_resp"`
		PublishPage string         `json:"publish_page"`
	}
	if json.NewDecoder(resp.Body).Decode(&outer) != nil {
		return result, &WeChatDiscoveryError{Code: "malformed_contract"}
	}
	if outer.BaseResp.Ret != 0 {
		return result, &WeChatDiscoveryError{Code: discoveryRetCode(outer.BaseResp.Ret)}
	}
	var page struct {
		PublishList []struct {
			PublishInfo string `json:"publish_info"`
		} `json:"publish_list"`
	}
	if json.Unmarshal([]byte(outer.PublishPage), &page) != nil {
		return result, &WeChatDiscoveryError{Code: "malformed_contract"}
	}
	items := []WeChatDiscoveredArticle{}
	result.PublicationCount = len(page.PublishList)
	for _, published := range page.PublishList {
		var info struct {
			AppMsgEx []WeChatOfficialArticle `json:"appmsgex"`
		}
		if json.Unmarshal([]byte(published.PublishInfo), &info) != nil {
			return result, &WeChatDiscoveryError{Code: "malformed_contract"}
		}
		for _, article := range info.AppMsgEx {
			if titleQuery != "" && !strings.Contains(strings.ToLower(article.Title), strings.ToLower(titleQuery)) {
				continue
			}
			key := stableWeChatArticleKey(article)
			items = append(items, WeChatDiscoveredArticle{WeChatOfficialArticle: article, ArticleKey: key})
		}
	}
	result.Articles = items
	return result, nil
}
func stableWeChatArticleKey(a WeChatOfficialArticle) string {
	if strings.TrimSpace(a.AID) != "" {
		return strings.TrimSpace(a.AID)
	}
	if a.AppMsgID != 0 {
		return fmt.Sprintf("%d:%d", a.AppMsgID, a.ItemIndex)
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(a.Link)))
	return hex.EncodeToString(sum[:])
}
func discoveryRetCode(ret int) string {
	switch ret {
	case 200003:
		return "login_required"
	case 200013:
		return "throttled"
	case -8:
		return "verification_required"
	default:
		return "upstream_rejected"
	}
}
