package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type WeChatMediaItem struct {
	SourceURL string `json:"source_url"`
	Alt       string `json:"alt,omitempty"`
}
type WeChatMediaAsset struct {
	WeChatMediaItem
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}
type WeChatMediaFailure struct {
	SourceURL string `json:"source_url"`
	Error     string `json:"error"`
}
type WeChatMediaConfig struct {
	HTTPClient  *http.Client
	Hosts       []string
	ResolveHost func(context.Context, string) ([]net.IP, error)
	MaxBytes    int64
}
type WeChatMediaDownloader struct {
	client  *http.Client
	hosts   map[string]bool
	resolve func(context.Context, string) ([]net.IP, error)
	max     int64
}

func NewWeChatMediaDownloader(cfg WeChatMediaConfig) *WeChatMediaDownloader {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	hosts := map[string]bool{"mmbiz.qpic.cn": true, "mmbiz.qlogo.cn": true}
	if len(cfg.Hosts) > 0 {
		hosts = map[string]bool{}
		for _, h := range cfg.Hosts {
			hosts[strings.ToLower(strings.TrimSpace(h))] = true
		}
	}
	resolve := cfg.ResolveHost
	if resolve == nil {
		resolve = func(ctx context.Context, h string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", h)
		}
	}
	max := cfg.MaxBytes
	if max <= 0 {
		max = 8 << 20
	}
	return &WeChatMediaDownloader{client: client, hosts: hosts, resolve: resolve, max: max}
}
func (d *WeChatMediaDownloader) Download(ctx context.Context, items []WeChatMediaItem) ([]WeChatMediaAsset, []WeChatMediaFailure) {
	assets := []WeChatMediaAsset{}
	failures := []WeChatMediaFailure{}
	seen := map[string]bool{}
	for _, item := range items {
		if seen[item.SourceURL] {
			continue
		}
		seen[item.SourceURL] = true
		asset, err := d.download(ctx, item)
		if err != nil {
			failures = append(failures, WeChatMediaFailure{SourceURL: item.SourceURL, Error: err.Error()})
			continue
		}
		assets = append(assets, asset)
	}
	return assets, failures
}
func (d *WeChatMediaDownloader) download(ctx context.Context, item WeChatMediaItem) (WeChatMediaAsset, error) {
	validate := func(ctx context.Context, u *url.URL) error {
		if u.Scheme != "https" || u.User != nil || !d.hosts[strings.ToLower(u.Hostname())] {
			return fmt.Errorf("media URL is not allowed")
		}
		ips, err := d.resolve(ctx, u.Hostname())
		if err != nil || len(ips) == 0 {
			return fmt.Errorf("media host resolution failed")
		}
		for _, ip := range ips {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
				return fmt.Errorf("media host resolved to non-public address")
			}
		}
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(item.SourceURL))
	if err != nil || validate(ctx, u) != nil {
		return WeChatMediaAsset{}, fmt.Errorf("media URL is not allowed")
	}
	client := *d.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("media redirect limit exceeded")
		}
		return validate(req.Context(), req.URL)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := client.Do(req)
	if err != nil {
		return WeChatMediaAsset{}, fmt.Errorf("media request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WeChatMediaAsset{}, fmt.Errorf("media request failed with HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, d.max+1))
	if err != nil || int64(len(data)) > d.max {
		return WeChatMediaAsset{}, fmt.Errorf("media exceeds byte limit")
	}
	kind := http.DetectContentType(data)
	if !allowedSourceAssetType(kind) {
		return WeChatMediaAsset{}, fmt.Errorf("unsupported media type")
	}
	sum := sha256.Sum256(data)
	return WeChatMediaAsset{WeChatMediaItem: item, SHA256: hex.EncodeToString(sum[:]), ContentType: kind, Data: data}, nil
}
