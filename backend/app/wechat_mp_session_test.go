package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWeChatMPLoginCompletesAndSavesSession(t *testing.T) {
	var pollCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/bizlogin":
			w.Header().Add("Set-Cookie", "uuid=fake-uuid; Path=/; HttpOnly")
			fmt.Fprint(w, `{"base_resp":{"ret":0},"redirect_url":"/cgi-bin/home?t=home/index&token=fake-token"}`)
		case "/cgi-bin/scanloginqrcode":
			pollCount++
			if pollCount == 1 {
				fmt.Fprint(w, `{"status":"scanned"}`)
				return
			}
			fmt.Fprint(w, `{"status":"confirmed","redirect_url":"/cgi-bin/home?t=home/index&token=fake-token"}`)
		case "/cgi-bin/loginqrcode":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("sanitized-qr-image"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	store := NewMemorySourceSecretStore()
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: server.URL, HTTPClient: server.Client(), SecretStore: store, SecretKey: "wechat-session"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.StartLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	qr, err := client.QRImage(context.Background())
	if err != nil || len(qr) == 0 {
		t.Fatalf("qr=%d err=%v", len(qr), err)
	}
	status, err := client.PollLogin(context.Background())
	if err != nil || status.State != WeChatMPLoginScanned {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	status, err = client.PollLogin(context.Background())
	if err != nil || status.State != WeChatMPLoginConfirmed {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	session, err := client.LoadSession(context.Background())
	if err != nil || session.Token == "" || len(session.Cookies) == 0 {
		t.Fatalf("session=%#v err=%v", session, err)
	}
	if strings.Contains(fmt.Sprint(status), session.Token) {
		t.Fatal("login status leaked token")
	}
}

func TestWeChatMPLoginRejectsMalformedRedirectWithoutSaving(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"confirmed","redirect_url":"https://example.invalid/escaped?token=secret"}`)
	}))
	defer server.Close()
	store := NewMemorySourceSecretStore()
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: server.URL, HTTPClient: server.Client(), SecretStore: store, SecretKey: "wechat-session"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.PollLogin(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err=%v", err)
	}
	if _, loadErr := store.Load(context.Background(), "wechat-session"); loadErr != ErrSourceSecretNotFound {
		t.Fatalf("saved malformed session: %v", loadErr)
	}
}

func TestWeChatMPLoginRejectsInsecureRemoteBaseURL(t *testing.T) {
	_, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: "http://example.invalid", SecretStore: NewMemorySourceSecretStore()})
	if err == nil {
		t.Fatal("accepted insecure remote MP base URL")
	}
}

func TestWeChatMPSessionLoadRejectsExpiredSession(t *testing.T) {
	store := NewMemorySourceSecretStore()
	raw, err := json.Marshal(WeChatMPSession{Token: "expired", ObservedExpiry: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "wechat-session", raw); err != nil {
		t.Fatal(err)
	}
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: "https://mp.weixin.qq.com", SecretStore: store, SecretKey: "wechat-session"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.LoadSession(context.Background()); !errors.Is(err, ErrWeChatMPSessionExpired) {
		t.Fatalf("LoadSession() error=%v", err)
	}
}
