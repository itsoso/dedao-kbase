package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type WeChatSourceAdapterConfig struct {
	Sessions  WeChatMPSessionProvider
	Discovery *WeChatDiscovery
	Source    *WeChatSourceService
	Media     *WeChatMediaDownloader
	Assets    WeChatAssetUploader
}

type WeChatAssetUploader interface {
	UploadAsset(context.Context, string, SourceAssetEnvelope) (SourceAssetReference, error)
}

type WeChatSourceAdapter struct {
	sessions  WeChatMPSessionProvider
	discovery *WeChatDiscovery
	source    *WeChatSourceService
	media     *WeChatMediaDownloader
	assets    WeChatAssetUploader
}

const weChatAgentCursorVersion = 2

type weChatAgentCursor struct {
	UpstreamBegin        int    `json:"upstream_begin"`
	PublicationItemIndex int    `json:"publication_item_index"`
	LastArticleKey       string `json:"last_article_key,omitempty"`
	LastTimestamp        int64  `json:"last_timestamp,omitempty"`
	FrontierArticleKey   string `json:"frontier_article_key,omitempty"`
	FrontierTimestamp    int64  `json:"frontier_timestamp,omitempty"`
	PendingFrontierKey   string `json:"pending_frontier_key,omitempty"`
	PendingFrontierTime  int64  `json:"pending_frontier_timestamp,omitempty"`
}

func NewWeChatSourceAdapter(cfg WeChatSourceAdapterConfig) (*WeChatSourceAdapter, error) {
	if cfg.Sessions == nil {
		return nil, fmt.Errorf("wechat MP session provider is required")
	}
	return &WeChatSourceAdapter{sessions: cfg.Sessions, discovery: cfg.Discovery, source: cfg.Source, media: cfg.Media, assets: cfg.Assets}, nil
}
func (a *WeChatSourceAdapter) Name() string { return "wechat_mp" }
func (a *WeChatSourceAdapter) Operations() []string {
	return []string{"discover_articles", "sync_articles", "sync_media"}
}
func (a *WeChatSourceAdapter) Status(ctx context.Context) SourceCapabilityHealth {
	session, err := a.sessions.Session(ctx)
	if err != nil || session.Validate(time.Now()) != nil {
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
	maxItems := sourceAgentOptionInt(run.Subscription.Options, "max_items", defaultWeChatSourceMaxItems, defaultWeChatSourceMaxItems)
	if run.RequestedOperation != "sync_articles" && run.RequestedOperation != "sync_media" {
		if run.RequestedOperation != "discover_articles" {
			return SourceAdapterResult{}, fmt.Errorf("unsupported source sync operation %q", run.RequestedOperation)
		}
	}
	if run.RequestedOperation != "discover_articles" && a.source == nil {
		return SourceAdapterResult{}, fmt.Errorf("wechat article source is not configured")
	}
	includeMedia := run.RequestedOperation == "sync_media" || sourceAgentOptionBool(run.Subscription.Options, "include_media", false)
	if includeMedia && (a.media == nil || a.assets == nil) {
		return SourceAdapterResult{}, fmt.Errorf("wechat media downloader and asset uploader are required")
	}

	next := cursor
	failures := make([]SourceAdapterItemFailure, 0)
	processed := 0
	titleQuery := sourceAgentOptionString(run.Subscription.Options, "title_query", "")
	for {
		if err := ctx.Err(); err != nil {
			return weChatAdapterFailure(next, err)
		}
		discoveryCursor := WeChatDiscoveryCursor{
			Begin:          next.UpstreamBegin,
			LastArticleKey: next.LastArticleKey,
			LastTimestamp:  next.LastTimestamp,
		}
		page, err := a.discovery.Discover(ctx, run.Subscription.SourceAccountKey, discoveryCursor, pageSize, titleQuery)
		if err != nil {
			return SourceAdapterResult{}, err
		}
		items := page.Articles
		if next.PublicationItemIndex == 0 && next.PendingFrontierKey == "" && len(items) > 0 {
			next.PendingFrontierKey = items[0].ArticleKey
			next.PendingFrontierTime = items[0].UpdateTime
		}
		processableEnd, reachedFrontier := weChatAgentPageBoundary(cursor.FrontierArticleKey, items)
		if next.PublicationItemIndex > processableEnd {
			return SourceAdapterResult{}, fmt.Errorf("wechat discovery cursor is beyond the current page")
		}
		if run.RequestedOperation == "discover_articles" {
			if processableEnd > next.PublicationItemIndex {
				last := items[processableEnd-1]
				next.LastArticleKey = last.ArticleKey
				next.LastTimestamp = last.UpdateTime
			}
			next.PublicationItemIndex = processableEnd
			advanceWeChatAgentPage(&next, page, processableEnd, reachedFrontier)
			return encodeWeChatAgentResult(next, failures)
		}

		pageItems := items[next.PublicationItemIndex:processableEnd]
		remaining := maxItems - processed
		if len(pageItems) > remaining {
			pageItems = pageItems[:remaining]
		}
		for _, item := range pageItems {
			article, err := a.source.DownloadArticle(ctx, item.Link)
			if err != nil {
				return weChatAdapterFailure(next, err)
			}
			content := article.Markdown
			var mediaErr error
			if includeMedia {
				content, mediaErr = a.archiveArticleMedia(ctx, run.ID, item.ArticleKey, article)
			}
			publishedAt := strings.TrimSpace(article.PublishedAt)
			if publishedAt == "" && item.UpdateTime > 0 {
				publishedAt = time.Unix(item.UpdateTime, 0).UTC().Format(time.RFC3339)
			}
			title := strings.TrimSpace(article.Title)
			if title == "" {
				title = strings.TrimSpace(item.Title)
			}
			envelope := SourceArticleEnvelope{SourceType: "wechat_mp_article", SourceAccountID: run.Subscription.SourceAccountKey, SourceAccount: run.Subscription.SourceAccount, SourceItemID: item.ArticleKey, IdempotencyKey: weChatArticleIdempotencyKey(run.Subscription.SourceAccountKey, item.ArticleKey, item.UpdateTime, content), Title: title, Author: article.AccountName, SourceURL: article.SourceURL, PublishedAt: publishedAt, Content: content, ContentFormat: "markdown"}
			itemErr := mediaErr
			if _, err := sink.Enqueue(run.ID, envelope); err != nil {
				if errors.Is(err, ErrSourceArticleContentTooShort) || errors.Is(err, ErrSourceArticleInvalidURL) {
					itemErr = err
				} else {
					return weChatAdapterFailure(next, err)
				}
			}
			if itemErr != nil {
				failures = append(failures, SourceAdapterItemFailure{
					SourceItemKey:  item.ArticleKey,
					IdempotencyKey: envelope.IdempotencyKey,
					Error:          itemErr.Error(),
				})
			}
			next.PublicationItemIndex++
			next.LastArticleKey = item.ArticleKey
			next.LastTimestamp = item.UpdateTime
			processed++
		}
		advanceWeChatAgentPage(&next, page, processableEnd, reachedFrontier)
		if reachedFrontier || page.PublicationCount == 0 || processed >= maxItems {
			return encodeWeChatAgentResult(next, failures)
		}
	}
}

func encodeWeChatAgentResult(cursor weChatAgentCursor, failures []SourceAdapterItemFailure) (SourceAdapterResult, error) {
	encoded, err := encodeWeChatAgentCursor(cursor)
	if err != nil {
		return SourceAdapterResult{}, err
	}
	return SourceAdapterResult{Cursor: encoded, Failures: failures}, nil
}

func weChatAgentPageBoundary(frontier string, items []WeChatDiscoveredArticle) (int, bool) {
	frontier = strings.TrimSpace(frontier)
	if frontier == "" {
		return len(items), false
	}
	for index, item := range items {
		if item.ArticleKey == frontier {
			return index, true
		}
	}
	return len(items), false
}

func advanceWeChatAgentPage(cursor *weChatAgentCursor, page WeChatDiscoveryPage, processableEnd int, reachedFrontier bool) {
	if cursor.PublicationItemIndex < processableEnd {
		return
	}
	if reachedFrontier || page.PublicationCount == 0 {
		if cursor.PendingFrontierKey != "" {
			cursor.FrontierArticleKey = cursor.PendingFrontierKey
			cursor.FrontierTimestamp = cursor.PendingFrontierTime
		} else if cursor.FrontierArticleKey == "" && cursor.LastArticleKey != "" {
			cursor.FrontierArticleKey = cursor.LastArticleKey
			cursor.FrontierTimestamp = cursor.LastTimestamp
		}
		cursor.PendingFrontierKey = ""
		cursor.PendingFrontierTime = 0
		cursor.UpstreamBegin = 0
		cursor.PublicationItemIndex = 0
		return
	}
	cursor.UpstreamBegin = page.UpstreamBegin + page.PublicationCount
	cursor.PublicationItemIndex = 0
}

func (a *WeChatSourceAdapter) archiveArticleMedia(ctx context.Context, runID, articleKey string, article *WeChatArticle) (string, error) {
	content := article.Markdown
	assets, failures := a.media.Download(ctx, article.Media)
	failureCount := len(failures)
	for _, asset := range assets {
		ref, err := a.assets.UploadAsset(ctx, runID, SourceAssetEnvelope{
			SourceItemKey: articleKey,
			SourceURL:     asset.SourceURL,
			SHA256:        asset.SHA256,
			ContentType:   asset.ContentType,
			Data:          asset.Data,
		})
		if err != nil || ref.SHA256 == "" {
			failureCount++
			continue
		}
		content = strings.ReplaceAll(content, asset.SourceURL, "/api/source-assets/"+ref.SHA256)
	}
	if failureCount > 0 {
		return content, fmt.Errorf("wechat media archival failed for %d assets", failureCount)
	}
	return content, nil
}

func weChatArticleIdempotencyKey(accountKey, articleKey string, updateTime int64, content string) string {
	sum := sha256.Sum256([]byte("wechat_mp_article:v2\x00" + strings.TrimSpace(accountKey) + "\x00" + strings.TrimSpace(articleKey) + "\x00" + fmt.Sprintf("%d", updateTime) + "\x00" + content))
	return hex.EncodeToString(sum[:])
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
	if payload.Version != 0 && payload.Version != 1 && payload.Version != weChatAgentCursorVersion {
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
