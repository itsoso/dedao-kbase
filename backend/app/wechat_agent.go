package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type WeChatSourceAdapterConfig struct {
	Sessions  WeChatMPSessionProvider
	Discovery *WeChatDiscovery
	Source    *WeChatSourceService
	Media     *WeChatMediaDownloader
}
type WeChatSourceAdapter struct {
	sessions  WeChatMPSessionProvider
	discovery *WeChatDiscovery
	source    *WeChatSourceService
	media     *WeChatMediaDownloader
}

const weChatAgentCursorVersion = 1

type weChatAgentCursor struct {
	UpstreamBegin        int    `json:"upstream_begin"`
	PublicationItemIndex int    `json:"publication_item_index"`
	LastArticleKey       string `json:"last_article_key,omitempty"`
	LastTimestamp        int64  `json:"last_timestamp,omitempty"`
}

func NewWeChatSourceAdapter(cfg WeChatSourceAdapterConfig) (*WeChatSourceAdapter, error) {
	if cfg.Sessions == nil {
		return nil, fmt.Errorf("wechat MP session provider is required")
	}
	return &WeChatSourceAdapter{sessions: cfg.Sessions, discovery: cfg.Discovery, source: cfg.Source, media: cfg.Media}, nil
}
func (a *WeChatSourceAdapter) Name() string { return "wechat_mp" }
func (a *WeChatSourceAdapter) Operations() []string {
	return []string{"discover_articles", "sync_articles", "sync_media"}
}
func (a *WeChatSourceAdapter) Status(ctx context.Context) SourceCapabilityHealth {
	session, err := a.sessions.Session(ctx)
	if err != nil || session.Token == "" {
		return SourceCapabilityHealth{Healthy: false, RequiresAction: "login", LastError: "wechat MP login is required"}
	}
	return SourceCapabilityHealth{Healthy: true, Version: "1"}
}
func (a *WeChatSourceAdapter) Execute(ctx context.Context, run SourceSyncRun, sink SourceEnvelopeSink) (SourceAdapterResult, error) {
	if run.Subscription == nil {
		return SourceAdapterResult{}, fmt.Errorf("subscription snapshot is required")
	}
	cursor, err := decodeWeChatAgentCursor(run.Subscription.Cursor)
	if err != nil {
		return SourceAdapterResult{}, fmt.Errorf("decode wechat discovery cursor: %w", err)
	}
	if a.discovery == nil {
		return SourceAdapterResult{}, fmt.Errorf("wechat discovery is not configured")
	}
	pageSize := sourceAgentOptionInt(run.Subscription.Options, "page_size", 10, 20)
	maxItems := sourceAgentOptionInt(run.Subscription.Options, "max_items", 100, 100)
	discoveryCursor := WeChatDiscoveryCursor{
		Begin:          cursor.UpstreamBegin,
		LastArticleKey: cursor.LastArticleKey,
		LastTimestamp:  cursor.LastTimestamp,
	}
	page, err := a.discovery.Discover(ctx, run.Subscription.SourceAccountKey, discoveryCursor, pageSize, sourceAgentOptionString(run.Subscription.Options, "title_query", ""))
	if err != nil {
		return SourceAdapterResult{}, err
	}
	items := page.Articles
	next := cursor
	if run.RequestedOperation == "discover_articles" {
		next.UpstreamBegin = page.UpstreamBegin + page.PublicationCount
		next.PublicationItemIndex = 0
		if len(items) > 0 {
			next.LastArticleKey = items[len(items)-1].ArticleKey
			next.LastTimestamp = items[len(items)-1].UpdateTime
		}
		encoded, err := encodeWeChatAgentCursor(next)
		if err != nil {
			return SourceAdapterResult{}, err
		}
		return SourceAdapterResult{Cursor: encoded}, nil
	}
	if run.RequestedOperation == "sync_media" {
		next.UpstreamBegin = page.UpstreamBegin + page.PublicationCount
		next.PublicationItemIndex = 0
		encoded, err := encodeWeChatAgentCursor(next)
		if err != nil {
			return SourceAdapterResult{}, err
		}
		return SourceAdapterResult{Cursor: encoded}, nil
	}
	if run.RequestedOperation != "sync_articles" {
		return SourceAdapterResult{}, fmt.Errorf("unsupported source sync operation %q", run.RequestedOperation)
	}
	if a.source == nil {
		return SourceAdapterResult{}, fmt.Errorf("wechat article source is not configured")
	}
	if next.PublicationItemIndex > len(items) {
		return SourceAdapterResult{}, fmt.Errorf("wechat discovery cursor is beyond the current page")
	}
	items = items[next.PublicationItemIndex:]
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	for _, item := range items {
		article, err := a.source.DownloadArticle(ctx, item.Link)
		if err != nil {
			return weChatAdapterFailure(next, err)
		}
		sum := sha256.Sum256([]byte(article.Markdown))
		envelope := SourceArticleEnvelope{SourceType: "wechat_mp_article", SourceAccountID: run.Subscription.SourceAccountKey, SourceAccount: run.Subscription.SourceAccount, SourceItemID: item.ArticleKey, IdempotencyKey: hex.EncodeToString(sum[:]), Title: article.Title, Author: article.AccountName, SourceURL: article.SourceURL, PublishedAt: article.PublishedAt, Content: article.Markdown, ContentFormat: "markdown"}
		if _, err := sink.Enqueue(run.ID, envelope); err != nil {
			return weChatAdapterFailure(next, err)
		}
		next.PublicationItemIndex++
		next.LastArticleKey = item.ArticleKey
		next.LastTimestamp = item.UpdateTime
	}
	if len(page.Articles) == 0 || next.PublicationItemIndex >= len(page.Articles) {
		next.UpstreamBegin = page.UpstreamBegin + page.PublicationCount
		next.PublicationItemIndex = 0
	}
	encoded, err := encodeWeChatAgentCursor(next)
	if err != nil {
		return SourceAdapterResult{}, err
	}
	return SourceAdapterResult{Cursor: encoded}, nil
}

func weChatAdapterFailure(cursor weChatAgentCursor, cause error) (SourceAdapterResult, error) {
	encoded, err := encodeWeChatAgentCursor(cursor)
	if err != nil {
		return SourceAdapterResult{}, fmt.Errorf("%w; encode safe cursor: %v", cause, err)
	}
	return SourceAdapterResult{Cursor: encoded}, newSourceAdapterExecutionError(encoded, cause)
}

func decodeWeChatAgentCursor(raw string) (weChatAgentCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return weChatAgentCursor{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return weChatAgentCursor{}, fmt.Errorf("invalid cursor JSON")
	}
	if _, legacy := fields["begin"]; legacy {
		var cursor WeChatDiscoveryCursor
		if err := json.Unmarshal([]byte(raw), &cursor); err != nil {
			return weChatAgentCursor{}, fmt.Errorf("invalid legacy cursor")
		}
		decoded := weChatAgentCursor{
			UpstreamBegin:  cursor.Begin,
			LastArticleKey: cursor.LastArticleKey,
			LastTimestamp:  cursor.LastTimestamp,
		}
		if err := validateWeChatAgentCursor(decoded); err != nil {
			return weChatAgentCursor{}, err
		}
		return decoded, nil
	}
	if _, current := fields["upstream_begin"]; !current {
		return weChatAgentCursor{}, fmt.Errorf("cursor format is not recognized")
	}
	var payload struct {
		Version int `json:"version"`
		weChatAgentCursor
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return weChatAgentCursor{}, fmt.Errorf("invalid cursor")
	}
	if payload.Version != 0 && payload.Version != weChatAgentCursorVersion {
		return weChatAgentCursor{}, fmt.Errorf("unsupported cursor version")
	}
	if err := validateWeChatAgentCursor(payload.weChatAgentCursor); err != nil {
		return weChatAgentCursor{}, err
	}
	return payload.weChatAgentCursor, nil
}

func encodeWeChatAgentCursor(cursor weChatAgentCursor) (string, error) {
	if err := validateWeChatAgentCursor(cursor); err != nil {
		return "", err
	}
	payload := struct {
		Version int `json:"version"`
		weChatAgentCursor
	}{Version: weChatAgentCursorVersion, weChatAgentCursor: cursor}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode wechat discovery cursor: %w", err)
	}
	return string(encoded), nil
}

func validateWeChatAgentCursor(cursor weChatAgentCursor) error {
	if cursor.UpstreamBegin < 0 || cursor.PublicationItemIndex < 0 {
		return fmt.Errorf("cursor offsets must be non-negative")
	}
	return nil
}
