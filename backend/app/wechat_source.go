package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

var ErrWeChatCredentialsNotConfigured = errors.New("wechat mp token/cookie are not configured")

type WeChatSourceConfig struct {
	HTTPClient      *http.Client
	MPBaseURL       string
	Token           string
	Cookie          string
	UserAgent       string
	ArticleHosts    []string
	ResolveHost     func(context.Context, string) ([]net.IP, error)
	MaxArticleBytes int64
	SessionProvider WeChatMPSessionProvider
}

type WeChatSourceService struct {
	client                       *http.Client
	mpBaseURL                    string
	token                        string
	cookie                       string
	userAgent                    string
	articleHosts                 map[string]struct{}
	resolveHost                  func(context.Context, string) ([]net.IP, error)
	maxArticleBytes              int64
	allowNonstandardArticlePorts bool
	sessionProvider              WeChatMPSessionProvider
}

type WeChatArticle struct {
	Title       string `json:"title"`
	AccountName string `json:"account_name,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	SourceURL   string `json:"source_url"`
	Digest      string `json:"digest,omitempty"`
	Markdown    string `json:"markdown"`
	Text        string `json:"text"`
}

type WeChatOfficialAccount struct {
	Nickname string `json:"nickname"`
	FakeID   string `json:"fakeid"`
	Alias    string `json:"alias,omitempty"`
}

type WeChatOfficialArticle struct {
	Title      string `json:"title"`
	Link       string `json:"link"`
	Digest     string `json:"digest,omitempty"`
	Cover      string `json:"cover,omitempty"`
	UpdateTime int64  `json:"update_time,omitempty"`
	AID        string `json:"aid,omitempty"`
	AppMsgID   int64  `json:"appmsgid,omitempty"`
	ItemIndex  int    `json:"itemidx,omitempty"`
}

func WeChatSourceConfigFromEnv() WeChatSourceConfig {
	return WeChatSourceConfig{
		MPBaseURL: strings.TrimSpace(os.Getenv("WECHAT_MP_BASE_URL")),
		Token:     strings.TrimSpace(os.Getenv("WECHAT_MP_TOKEN")),
		Cookie:    strings.TrimSpace(os.Getenv("WECHAT_MP_COOKIE")),
		UserAgent: strings.TrimSpace(os.Getenv("WECHAT_SOURCE_USER_AGENT")),
	}
}

func NewWeChatSourceService(cfg WeChatSourceConfig) *WeChatSourceService {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	mpBaseURL := strings.TrimRight(strings.TrimSpace(cfg.MPBaseURL), "/")
	if mpBaseURL == "" {
		mpBaseURL = "https://mp.weixin.qq.com"
	}
	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (compatible; dedao-kbase-wechat-source/1.0)"
	}
	articleHosts := make(map[string]struct{})
	configuredArticleHosts := len(cfg.ArticleHosts) > 0
	if !configuredArticleHosts {
		cfg.ArticleHosts = []string{"mp.weixin.qq.com"}
	}
	for _, host := range cfg.ArticleHosts {
		if normalized := strings.ToLower(strings.TrimSpace(host)); normalized != "" {
			articleHosts[normalized] = struct{}{}
		}
	}
	resolveHost := cfg.ResolveHost
	if resolveHost == nil {
		resolver := net.DefaultResolver
		resolveHost = func(ctx context.Context, host string) ([]net.IP, error) {
			return resolver.LookupIP(ctx, "ip", host)
		}
	}
	maxArticleBytes := cfg.MaxArticleBytes
	if maxArticleBytes <= 0 {
		maxArticleBytes = 4 << 20
	}
	return &WeChatSourceService{
		client:                       client,
		mpBaseURL:                    mpBaseURL,
		token:                        strings.TrimSpace(cfg.Token),
		cookie:                       strings.TrimSpace(cfg.Cookie),
		userAgent:                    userAgent,
		articleHosts:                 articleHosts,
		resolveHost:                  resolveHost,
		maxArticleBytes:              maxArticleBytes,
		allowNonstandardArticlePorts: configuredArticleHosts,
		sessionProvider:              cfg.SessionProvider,
	}
}

func (s *WeChatSourceService) DownloadArticle(ctx context.Context, rawURL string) (*WeChatArticle, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("wechat article url is required")
	}
	articleURL, err := s.validateWeChatArticleURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, articleURL.String(), nil)
	if err != nil {
		return nil, err
	}
	s.applyHeaders(req, false)
	client := *s.client
	previousCheckRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("wechat article redirect limit exceeded")
		}
		if _, err := s.validateWeChatArticleURL(req.Context(), req.URL.String()); err != nil {
			return err
		}
		if previousCheckRedirect != nil {
			return previousCheckRedirect(req, via)
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("wechat article request failed: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, s.maxArticleBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > s.maxArticleBytes {
		return nil, fmt.Errorf("wechat article response exceeds %d bytes", s.maxArticleBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	return parseWeChatArticleDocument(doc, rawURL), nil
}

func (s *WeChatSourceService) validateWeChatArticleURL(ctx context.Context, rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid wechat article url: %w", err)
	}
	if parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("wechat article url must be credential-free HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	if _, ok := s.articleHosts[host]; !ok {
		return nil, fmt.Errorf("wechat article host is not allowed")
	}
	if parsed.Port() != "" && parsed.Port() != "443" && !s.allowNonstandardArticlePorts {
		return nil, fmt.Errorf("wechat article port is not allowed")
	}
	addresses, err := s.resolveHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve wechat article host: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("wechat article host resolved to no addresses")
	}
	for _, address := range addresses {
		if address == nil || address.IsLoopback() || address.IsPrivate() || address.IsUnspecified() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
			return nil, fmt.Errorf("wechat article host resolved to a non-public address")
		}
	}
	return parsed, nil
}

func (s *WeChatSourceService) SearchOfficialAccounts(ctx context.Context, query string) ([]WeChatOfficialAccount, error) {
	token, cookie, err := s.credentials(ctx)
	if err != nil {
		return nil, err
	}
	values := url.Values{
		"action": {"search_biz"},
		"scene":  {"1"},
		"begin":  {"0"},
		"count":  {"10"},
		"query":  {query},
		"token":  {token},
		"lang":   {"zh_CN"},
		"f":      {"json"},
		"ajax":   {"1"},
	}
	endpoint := s.mpBaseURL + "/cgi-bin/searchbiz?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	s.applyHeadersWithCookie(req, cookie)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("wechat account search failed: %s", resp.Status)
	}

	var decoded struct {
		BaseResp weChatBaseResp `json:"base_resp"`
		List     []struct {
			Nickname string `json:"nickname"`
			FakeID   string `json:"fakeid"`
			Alias    string `json:"alias"`
		} `json:"list"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	if decoded.BaseResp.Ret != 0 {
		return nil, fmt.Errorf("wechat account search rejected: %s", decoded.BaseResp.message())
	}
	accounts := make([]WeChatOfficialAccount, 0, len(decoded.List))
	for _, item := range decoded.List {
		accounts = append(accounts, WeChatOfficialAccount{
			Nickname: strings.TrimSpace(item.Nickname),
			FakeID:   strings.TrimSpace(item.FakeID),
			Alias:    strings.TrimSpace(item.Alias),
		})
	}
	return accounts, nil
}

func (s *WeChatSourceService) ListOfficialAccountArticles(ctx context.Context, fakeID string, begin int, count int) ([]WeChatOfficialArticle, error) {
	token, cookie, err := s.credentials(ctx)
	if err != nil {
		return nil, err
	}
	fakeID = strings.TrimSpace(fakeID)
	if fakeID == "" {
		return nil, fmt.Errorf("fakeid is required")
	}
	if begin < 0 {
		begin = 0
	}
	if count <= 0 {
		count = 5
	}
	values := url.Values{
		"action": {"list_ex"},
		"begin":  {strconv.Itoa(begin)},
		"count":  {strconv.Itoa(count)},
		"fakeid": {fakeID},
		"type":   {"9"},
		"query":  {""},
		"token":  {token},
		"lang":   {"zh_CN"},
		"f":      {"json"},
		"ajax":   {"1"},
	}
	endpoint := s.mpBaseURL + "/cgi-bin/appmsg?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	s.applyHeadersWithCookie(req, cookie)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("wechat article list failed: %s", resp.Status)
	}

	var decoded struct {
		BaseResp   weChatBaseResp `json:"base_resp"`
		AppMsgList []struct {
			Title      string `json:"title"`
			Link       string `json:"link"`
			Digest     string `json:"digest"`
			Cover      string `json:"cover"`
			UpdateTime int64  `json:"update_time"`
			AID        string `json:"aid"`
			AppMsgID   int64  `json:"appmsgid"`
			ItemIndex  int    `json:"itemidx"`
		} `json:"app_msg_list"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	if decoded.BaseResp.Ret != 0 {
		return nil, fmt.Errorf("wechat article list rejected: %s", decoded.BaseResp.message())
	}
	articles := make([]WeChatOfficialArticle, 0, len(decoded.AppMsgList))
	for _, item := range decoded.AppMsgList {
		articles = append(articles, WeChatOfficialArticle{
			Title:      strings.TrimSpace(item.Title),
			Link:       htmlUnescapeTrim(item.Link),
			Digest:     strings.TrimSpace(item.Digest),
			Cover:      htmlUnescapeTrim(item.Cover),
			UpdateTime: item.UpdateTime,
			AID:        strings.TrimSpace(item.AID),
			AppMsgID:   item.AppMsgID,
			ItemIndex:  item.ItemIndex,
		})
	}
	return articles, nil
}

func (s *WeChatSourceService) ImportArticle(ctx context.Context, store *BookKnowledgeStore, rawURL string, bookID string) (*BookKnowledgePackage, error) {
	if store == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	article, err := s.DownloadArticle(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	pkg := WeChatArticleToPackage(*article, bookID)
	if err := store.SavePackage(pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func WeChatArticleToPackage(article WeChatArticle, bookID string) BookKnowledgePackage {
	now := time.Now().Format(time.RFC3339)
	if strings.TrimSpace(bookID) == "" {
		sum := sha1.Sum([]byte(article.SourceURL))
		bookID = "wechat-" + hex.EncodeToString(sum[:])[:12]
	}
	bookID = sanitizeBookKnowledgeID(bookID)
	title := strings.TrimSpace(article.Title)
	if title == "" {
		title = bookID
	}
	text := strings.TrimSpace(article.Text)
	if text == "" {
		text = strings.TrimSpace(article.Markdown)
	}
	chapterID := bookID + "-article"
	chunkID := bookID + "-chunk-1"
	citationID := bookID + "-citation-1"
	return BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:     bookID,
			Title:      title,
			Author:     article.AccountName,
			SourceHTML: article.SourceURL,
			CreatedAt:  now,
			UpdatedAt:  now,
			Status:     "draft",
			Extractor:  "wechat-source-adapter",
		},
		Chapters: []BookKnowledgeChapter{{
			ChapterID: chapterID,
			BookID:    bookID,
			Order:     1,
			Title:     title,
			Summary:   article.Digest,
			ChunkIDs:  []string{chunkID},
		}},
		Chunks: []BookKnowledgeChunk{{
			ChunkID:   chunkID,
			BookID:    bookID,
			ChapterID: chapterID,
			Order:     1,
			Text:      text,
		}},
		Claims: []BookKnowledgeClaim{},
		Citations: []BookKnowledgeCitation{{
			CitationID: citationID,
			BookID:     bookID,
			ChapterID:  chapterID,
			ChunkID:    chunkID,
			SourceHTML: article.SourceURL,
			Anchor:     article.PublishedAt,
			Note:       "wechat public account article",
		}},
	}
}

func (s *WeChatSourceService) requireCredentials() error {
	if strings.TrimSpace(s.token) == "" || strings.TrimSpace(s.cookie) == "" {
		return ErrWeChatCredentialsNotConfigured
	}
	return nil
}

func (s *WeChatSourceService) credentials(ctx context.Context) (string, string, error) {
	if s.sessionProvider == nil {
		if err := s.requireCredentials(); err != nil {
			return "", "", err
		}
		return s.token, s.cookie, nil
	}
	session, err := s.sessionProvider.Session(ctx)
	if err != nil || strings.TrimSpace(session.Token) == "" {
		return "", "", ErrWeChatCredentialsNotConfigured
	}
	cookies := make([]string, 0, len(session.Cookies))
	for _, cookie := range session.Cookies {
		if cookie.Name != "" {
			cookies = append(cookies, cookie.Name+"="+cookie.Value)
		}
	}
	return session.Token, strings.Join(cookies, "; "), nil
}

func (s *WeChatSourceService) applyHeaders(req *http.Request, includeCookie bool) {
	req.Header.Set("User-Agent", s.userAgent)
	if includeCookie && s.cookie != "" {
		req.Header.Set("Cookie", s.cookie)
	}
}

func (s *WeChatSourceService) applyHeadersWithCookie(req *http.Request, cookie string) {
	req.Header.Set("User-Agent", s.userAgent)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
}

type weChatBaseResp struct {
	Ret    int    `json:"ret"`
	ErrMsg string `json:"err_msg"`
}

func (r weChatBaseResp) message() string {
	if strings.TrimSpace(r.ErrMsg) != "" {
		return r.ErrMsg
	}
	return fmt.Sprintf("ret=%d", r.Ret)
}

func parseWeChatArticleDocument(doc *goquery.Document, sourceURL string) *WeChatArticle {
	title := firstSelectionText(doc, "#activity-name", "h1.rich_media_title", ".rich_media_title", "meta[property='og:title']", "title")
	account := firstSelectionText(doc, "#js_name", ".rich_media_meta_nickname", "meta[property='og:article:author']")
	publishedAt := firstSelectionText(doc, "#publish_time", "#js_publish_time", "em.rich_media_meta_text")
	digest := firstSelectionAttr(doc, "meta[name='description']", "content")

	content := firstSelection(doc, "#js_content", ".rich_media_content", "#js_article_content", "article")
	markdown := buildWeChatMarkdown(title, content)
	text := normalizeWhitespace(content.Text())
	if text == "" {
		text = normalizeWhitespace(doc.Find("body").Text())
	}
	return &WeChatArticle{
		Title:       title,
		AccountName: account,
		PublishedAt: publishedAt,
		SourceURL:   sourceURL,
		Digest:      digest,
		Markdown:    markdown,
		Text:        text,
	}
}

func buildWeChatMarkdown(title string, content *goquery.Selection) string {
	var parts []string
	if title != "" {
		parts = append(parts, "# "+title)
	}
	if content == nil || content.Length() == 0 {
		return strings.TrimSpace(strings.Join(parts, "\n\n"))
	}

	imageIndex := 0
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			selection := goquery.NewDocumentFromNode(node).Selection
			switch node.Data {
			case "img":
				imageIndex++
				src := firstAttr(selection, "data-src", "data-original", "data-lazy-src", "src")
				if src != "" && !strings.HasPrefix(src, "data:") {
					alt := normalizeWhitespace(firstAttr(selection, "alt", "title"))
					if alt == "" {
						alt = fmt.Sprintf("image-%d", imageIndex)
					}
					parts = append(parts, fmt.Sprintf("![%s](%s)", alt, src))
				}
				return
			case "h2", "h3", "p", "li", "blockquote":
				text := normalizeWhitespace(selection.Text())
				if text != "" {
					prefix := ""
					if node.Data == "h2" {
						prefix = "## "
					} else if node.Data == "h3" {
						prefix = "### "
					} else if node.Data == "li" {
						prefix = "- "
					} else if node.Data == "blockquote" {
						prefix = "> "
					}
					parts = append(parts, prefix+text)
				}
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for _, node := range content.Nodes {
		walk(node)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func firstSelection(doc *goquery.Document, selectors ...string) *goquery.Selection {
	for _, selector := range selectors {
		sel := doc.Find(selector).First()
		if sel.Length() > 0 {
			return sel
		}
	}
	return &goquery.Selection{}
}

func firstSelectionText(doc *goquery.Document, selectors ...string) string {
	for _, selector := range selectors {
		sel := doc.Find(selector).First()
		if sel.Length() == 0 {
			continue
		}
		if strings.HasPrefix(selector, "meta") {
			if value := normalizeWhitespace(firstAttr(sel, "content", "value")); value != "" {
				return value
			}
		}
		if value := normalizeWhitespace(sel.Text()); value != "" {
			return value
		}
	}
	return ""
}

func firstSelectionAttr(doc *goquery.Document, selector string, attrs ...string) string {
	sel := doc.Find(selector).First()
	if sel.Length() == 0 {
		return ""
	}
	return normalizeWhitespace(firstAttr(sel, attrs...))
}

func firstAttr(sel *goquery.Selection, attrs ...string) string {
	for _, attr := range attrs {
		if value, ok := sel.Attr(attr); ok && strings.TrimSpace(value) != "" {
			return htmlUnescapeTrim(value)
		}
	}
	return ""
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func htmlUnescapeTrim(value string) string {
	return strings.TrimSpace(strings.NewReplacer("&amp;", "&", "&#39;", "'", "&quot;", "\"", "&lt;", "<", "&gt;", ">").Replace(value))
}
