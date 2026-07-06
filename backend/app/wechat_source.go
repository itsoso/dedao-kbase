package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var ErrWeChatCredentialsNotConfigured = errors.New("wechat mp token/cookie are not configured")

type WeChatSourceConfig struct {
	HTTPClient *http.Client
	MPBaseURL  string
	Token      string
	Cookie     string
	UserAgent  string
}

type WeChatSourceService struct {
	client    *http.Client
	mpBaseURL string
	token     string
	cookie    string
	userAgent string
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
	return &WeChatSourceService{
		client:    client,
		mpBaseURL: mpBaseURL,
		token:     strings.TrimSpace(cfg.Token),
		cookie:    strings.TrimSpace(cfg.Cookie),
		userAgent: userAgent,
	}
}

func (s *WeChatSourceService) DownloadArticle(ctx context.Context, rawURL string) (*WeChatArticle, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("wechat article url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	s.applyHeaders(req, false)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("wechat article request failed: %s", resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseWeChatArticleDocument(doc, rawURL), nil
}

func (s *WeChatSourceService) SearchOfficialAccounts(ctx context.Context, query string) ([]WeChatOfficialAccount, error) {
	if err := s.requireCredentials(); err != nil {
		return nil, err
	}
	values := url.Values{
		"action": {"search_biz"},
		"scene":  {"1"},
		"begin":  {"0"},
		"count":  {"10"},
		"query":  {query},
		"token":  {s.token},
		"lang":   {"zh_CN"},
		"f":      {"json"},
		"ajax":   {"1"},
	}
	endpoint := s.mpBaseURL + "/cgi-bin/searchbiz?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	s.applyHeaders(req, true)
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
	if err := s.requireCredentials(); err != nil {
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
		"token":  {s.token},
		"lang":   {"zh_CN"},
		"f":      {"json"},
		"ajax":   {"1"},
	}
	endpoint := s.mpBaseURL + "/cgi-bin/appmsg?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	s.applyHeaders(req, true)
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

func (s *WeChatSourceService) applyHeaders(req *http.Request, includeCookie bool) {
	req.Header.Set("User-Agent", s.userAgent)
	if includeCookie && s.cookie != "" {
		req.Header.Set("Cookie", s.cookie)
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

	content.Find("h2,h3,p,li,blockquote").Each(func(_ int, sel *goquery.Selection) {
		text := normalizeWhitespace(sel.Text())
		if text == "" {
			return
		}
		switch goquery.NodeName(sel) {
		case "h2":
			parts = append(parts, "## "+text)
		case "h3":
			parts = append(parts, "### "+text)
		case "li":
			parts = append(parts, "- "+text)
		default:
			parts = append(parts, text)
		}
	})
	content.Find("img").Each(func(i int, sel *goquery.Selection) {
		src := firstAttr(sel, "data-src", "src")
		if src == "" || strings.HasPrefix(src, "data:") {
			return
		}
		alt := normalizeWhitespace(firstAttr(sel, "alt", "title"))
		if alt == "" {
			alt = fmt.Sprintf("image-%d", i+1)
		}
		parts = append(parts, fmt.Sprintf("![%s](%s)", alt, src))
	})
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
