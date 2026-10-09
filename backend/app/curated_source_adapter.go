package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const curatedMaxBytes = 4 << 20

// CuratedSourceConfig is local operator configuration, never subscription input.
type CuratedSourceConfig struct {
	SourceType string `json:"source_type"`
	FeedURL    string `json:"feed_url,omitempty"`
	ExportFile string `json:"export_file,omitempty"`
}
type CuratedSourceAdapter struct {
	sources map[string]CuratedSourceConfig
	client  *http.Client
}
type CuratedSourceItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Content     string `json:"content"`
	ContentKind string `json:"content_kind"`
}
type CuratedSourceBundle struct {
	SchemaVersion string              `json:"schema_version"`
	SourceType    string              `json:"source_type"`
	AccountKey    string              `json:"account_key"`
	Items         []CuratedSourceItem `json:"items"`
}

func NewCuratedSourceAdapter(sources map[string]CuratedSourceConfig) (*CuratedSourceAdapter, error) {
	if len(sources) == 0 || len(sources) > 50 {
		return nil, fmt.Errorf("configure 1 to 50 curated sources")
	}
	copySources := make(map[string]CuratedSourceConfig, len(sources))
	for key, cfg := range sources {
		if strings.TrimSpace(key) == "" || len(key) > 200 {
			return nil, fmt.Errorf("invalid curated account key")
		}
		switch cfg.SourceType {
		case "rss_entry", "xiaoyuzhou_episode", "x_post", "zhihu_answer", "dedao_article":
		default:
			return nil, fmt.Errorf("unsupported curated source type")
		}
		if (cfg.FeedURL == "") == (cfg.ExportFile == "") {
			return nil, fmt.Errorf("configure exactly one feed_url or export_file")
		}
		if cfg.FeedURL != "" {
			if cfg.SourceType != "rss_entry" && cfg.SourceType != "xiaoyuzhou_episode" {
				return nil, fmt.Errorf("feed mode requires rss_entry or xiaoyuzhou_episode")
			}
			u, err := url.Parse(cfg.FeedURL)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return nil, fmt.Errorf("feed_url must be a public HTTPS URL without credentials or query")
			}
		}
		copySources[key] = cfg
	}
	return &CuratedSourceAdapter{sources: copySources, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("feed redirects are disabled") }}}, nil
}
func (a *CuratedSourceAdapter) Name() string         { return "curated" }
func (a *CuratedSourceAdapter) Operations() []string { return []string{"sync_curated"} }
func (a *CuratedSourceAdapter) Status(context.Context) SourceCapabilityHealth {
	return SourceCapabilityHealth{Healthy: true, Code: "configured"}
}

func (a *CuratedSourceAdapter) read(ctx context.Context, cfg CuratedSourceConfig) ([]byte, error) {
	var reader io.ReadCloser
	if cfg.FeedURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.FeedURL, nil)
		if err != nil {
			return nil, fmt.Errorf("invalid feed request")
		}
		req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml")
		res, err := a.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("feed request failed")
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, fmt.Errorf("feed returned HTTP %d", res.StatusCode)
		}
		reader = res.Body
	} else {
		info, err := os.Lstat(cfg.ExportFile)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("export file must be a readable regular private file (0600)")
		}
		f, err := os.Open(cfg.ExportFile)
		if err != nil {
			return nil, fmt.Errorf("export file unavailable")
		}
		reader = f
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, curatedMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("source read failed")
	}
	if len(raw) > curatedMaxBytes {
		return nil, fmt.Errorf("source exceeds 4 MiB limit")
	}
	return raw, nil
}

func curatedPlainText(value string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(value))
	var result strings.Builder
	skip := 0
	for {
		typ := tokenizer.Next()
		switch typ {
		case html.ErrorToken:
			return strings.TrimSpace(result.String())
		case html.StartTagToken:
			token := tokenizer.Token()
			if token.Data == "script" || token.Data == "style" {
				skip++
			}
			if skip == 0 {
				result.WriteString("\n")
			}
		case html.EndTagToken:
			token := tokenizer.Token()
			if (token.Data == "script" || token.Data == "style") && skip > 0 {
				skip--
			}
			if skip == 0 {
				result.WriteString("\n")
			}
		case html.TextToken:
			if skip == 0 {
				result.WriteString(string(tokenizer.Text()))
			}
		}
	}
}

func parseCuratedRSS(raw []byte) ([]CuratedSourceItem, error) {
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel *struct {
			Items []struct {
				GUID        string `xml:"guid"`
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Author      string `xml:"author"`
				Date        string `xml:"pubDate"`
				Description string `xml:"description"`
				Encoded     string `xml:"encoded"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil, fmt.Errorf("invalid RSS document")
	}
	if feed.Channel == nil || feed.Version != "2.0" {
		return nil, fmt.Errorf("RSS 2.0 channel required")
	}
	items := make([]CuratedSourceItem, 0, len(feed.Channel.Items))
	for _, entry := range feed.Channel.Items {
		id := strings.TrimSpace(entry.GUID)
		if id == "" {
			id = strings.TrimSpace(entry.Link)
		}
		content := entry.Encoded
		if strings.TrimSpace(content) == "" {
			content = entry.Description
		}
		published := ""
		if entry.Date != "" {
			for _, format := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339} {
				if stamp, err := time.Parse(format, entry.Date); err == nil {
					published = stamp.UTC().Format(time.RFC3339)
					break
				}
			}
			if published == "" {
				return nil, fmt.Errorf("invalid RSS publication date")
			}
		}
		items = append(items, CuratedSourceItem{ID: id, Title: curatedPlainText(entry.Title), URL: entry.Link, Author: curatedPlainText(entry.Author), PublishedAt: published, Content: curatedPlainText(content), ContentKind: "feed_excerpt"})
	}
	return items, nil
}

func (a *CuratedSourceAdapter) Execute(ctx context.Context, run SourceSyncRun, sink SourceEnvelopeSink) (SourceAdapterResult, error) {
	if run.Subscription == nil || sink == nil || run.RequestedOperation != "sync_curated" {
		return SourceAdapterResult{}, fmt.Errorf("curated subscription and operation required")
	}
	sub := run.Subscription
	cfg, ok := a.sources[sub.SourceAccountKey]
	if !ok || cfg.SourceType != sub.SourceType {
		return SourceAdapterResult{}, fmt.Errorf("curated source is not locally authorized")
	}
	raw, err := a.read(ctx, cfg)
	if err != nil {
		return SourceAdapterResult{}, err
	}
	var items []CuratedSourceItem
	if cfg.FeedURL != "" {
		items, err = parseCuratedRSS(raw)
	} else {
		var bundle CuratedSourceBundle
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&bundle); err == nil {
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				err = fmt.Errorf("trailing export data")
			}
		}
		if err == nil && (bundle.SchemaVersion != "curated_source.v1" || bundle.SourceType != cfg.SourceType || bundle.AccountKey != sub.SourceAccountKey) {
			err = fmt.Errorf("export scope mismatch")
		}
		items = bundle.Items
	}
	if err != nil {
		return SourceAdapterResult{}, fmt.Errorf("invalid curated source document")
	}
	if len(items) > 200 {
		return SourceAdapterResult{}, fmt.Errorf("source exceeds 200 items; split export or reduce feed window")
	}
	envelopes := make([]SourceArticleEnvelope, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		if item.ID == "" || len(item.ID) > 1000 || seen[item.ID] || len(item.Content) > 200000 {
			return SourceAdapterResult{}, fmt.Errorf("invalid or duplicate curated item identity/content")
		}
		seen[item.ID] = true
		switch item.ContentKind {
		case "original", "excerpt", "transcript", "manual_transcript", "feed_excerpt":
		default:
			return SourceAdapterResult{}, fmt.Errorf("content_kind required")
		}
		if item.PublishedAt != "" {
			if _, err := time.Parse(time.RFC3339, item.PublishedAt); err != nil {
				return SourceAdapterResult{}, fmt.Errorf("publication date must be RFC3339")
			}
		}
		idHash := sha256.Sum256([]byte(sub.SourceAccountKey + "\x00" + item.ID))
		envelope := SourceArticleEnvelope{SourceType: cfg.SourceType, SourceAccountID: sub.SourceAccountKey, SourceAccount: sub.SourceAccount,
			SourceItemID: hex.EncodeToString(idHash[:]), Title: item.Title, Author: item.Author, SourceURL: item.URL, PublishedAt: item.PublishedAt,
			Content: item.Content, ContentFormat: "markdown", Metadata: map[string]string{"content_kind": item.ContentKind, "coverage_scope": "provided_items_only", "collection_method": "authorized_export", "hash_strategy": "canonical_package"}}
		if cfg.FeedURL != "" {
			envelope.Metadata["collection_method"] = "rss"
		}
		normalized, _, err := normalizeSourceArticleEnvelope(envelope)
		if err != nil {
			return SourceAdapterResult{}, fmt.Errorf("invalid curated article")
		}
		encoded, _ := json.Marshal(normalized)
		hash := sha256.Sum256(encoded)
		normalized.IdempotencyKey = "curated-" + hex.EncodeToString(hash[:])
		envelopes = append(envelopes, normalized)
	}
	// Validate the entire bounded document before committing anything to the durable outbox.
	for _, envelope := range envelopes {
		if err := ctx.Err(); err != nil {
			return SourceAdapterResult{}, err
		}
		if _, err := sink.Enqueue(run.ID, envelope); err != nil {
			return SourceAdapterResult{}, fmt.Errorf("curated outbox enqueue failed")
		}
	}
	normalized, _ := json.Marshal(envelopes)
	sum := sha256.Sum256(normalized)
	return SourceAdapterResult{Cursor: hex.EncodeToString(sum[:])}, nil
}
