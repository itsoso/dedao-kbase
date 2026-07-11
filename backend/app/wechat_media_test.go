package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestWeChatMediaDownloadsAndDeduplicates(t *testing.T) {
	data := []byte("\x89PNG\r\n\x1a\nsanitized")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	downloader := NewWeChatMediaDownloader(WeChatMediaConfig{HTTPClient: server.Client(), Hosts: []string{u.Hostname()}, ResolveHost: publicTestResolver, MaxBytes: 1024})
	manifest := []WeChatMediaItem{{SourceURL: server.URL + "/a"}, {SourceURL: server.URL + "/a"}}
	assets, failures := downloader.Download(context.Background(), manifest)
	sum := sha256.Sum256(data)
	if len(assets) != 1 || len(failures) != 0 || assets[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("assets=%#v failures=%#v", assets, failures)
	}
}
func TestWeChatMediaReturnsPartialFailures(t *testing.T) {
	downloader := NewWeChatMediaDownloader(WeChatMediaConfig{})
	assets, failures := downloader.Download(context.Background(), []WeChatMediaItem{{SourceURL: "data:text/plain,no"}, {SourceURL: "http://example.invalid/no"}})
	if len(assets) != 0 || len(failures) != 2 {
		t.Fatalf("assets=%#v failures=%#v", assets, failures)
	}
	_ = fmt.Sprint(failures)
}
