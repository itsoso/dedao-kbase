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
	var cursor WeChatDiscoveryCursor
	if strings.TrimSpace(run.Subscription.Cursor) != "" {
		_ = json.Unmarshal([]byte(run.Subscription.Cursor), &cursor)
	}
	if a.discovery == nil {
		return SourceAdapterResult{}, fmt.Errorf("wechat discovery is not configured")
	}
	pageSize := sourceAgentOptionInt(run.Subscription.Options, "page_size", 10, 20)
	maxItems := sourceAgentOptionInt(run.Subscription.Options, "max_items", 100, 100)
	page, err := a.discovery.Discover(ctx, run.Subscription.SourceAccountKey, cursor, pageSize, sourceAgentOptionString(run.Subscription.Options, "title_query", ""))
	if err != nil {
		return SourceAdapterResult{}, err
	}
	items := page.Articles
	next := cursor
	next.Begin = page.UpstreamBegin + page.PublicationCount
	if len(items) > 0 {
		next.LastArticleKey = items[len(items)-1].ArticleKey
		next.LastTimestamp = items[len(items)-1].UpdateTime
	}
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	encoded, _ := json.Marshal(next)
	if run.RequestedOperation == "discover_articles" {
		return SourceAdapterResult{Cursor: string(encoded)}, nil
	}
	if run.RequestedOperation == "sync_media" {
		return SourceAdapterResult{Cursor: string(encoded)}, nil
	}
	if run.RequestedOperation != "sync_articles" {
		return SourceAdapterResult{}, fmt.Errorf("unsupported source sync operation %q", run.RequestedOperation)
	}
	if a.source == nil {
		return SourceAdapterResult{}, fmt.Errorf("wechat article source is not configured")
	}
	for _, item := range items {
		article, err := a.source.DownloadArticle(ctx, item.Link)
		if err != nil {
			return SourceAdapterResult{}, err
		}
		sum := sha256.Sum256([]byte(article.Markdown))
		envelope := SourceArticleEnvelope{SourceType: "wechat_mp_article", SourceAccountID: run.Subscription.SourceAccountKey, SourceAccount: run.Subscription.SourceAccount, SourceItemID: item.ArticleKey, IdempotencyKey: hex.EncodeToString(sum[:]), Title: article.Title, Author: article.AccountName, SourceURL: article.SourceURL, PublishedAt: article.PublishedAt, Content: article.Markdown, ContentFormat: "markdown"}
		if _, err := sink.Enqueue(run.ID, envelope); err != nil {
			return SourceAdapterResult{}, err
		}
	}
	return SourceAdapterResult{Cursor: string(encoded)}, nil
}
