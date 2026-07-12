package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type weChatMPRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f weChatMPRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type corruptingSourceSecretStore struct{ SourceSecretStore }

func (s corruptingSourceSecretStore) Save(ctx context.Context, key string, value []byte) error {
	if len(value) > 128 {
		value = value[:128]
	}
	return s.SourceSecretStore.Save(ctx, key, value)
}

func TestWeChatMPLoginCompletesAndSavesSession(t *testing.T) {
	var pollCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/bizlogin":
			if r.Method != http.MethodPost {
				t.Fatalf("bizlogin method=%s", r.Method)
			}
			if r.Header.Get("Referer") == "" || r.Header.Get("Origin") == "" || r.Header.Get("User-Agent") == "" || r.Header.Get("Accept-Encoding") != "identity" {
				t.Fatalf("bizlogin headers=%v", r.Header)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			switch r.URL.Query().Get("action") {
			case "startlogin":
				if r.Form.Get("login_type") != "3" || r.Form.Get("sessionid") == "" {
					t.Fatalf("start form=%v", r.Form)
				}
				w.Header().Add("Set-Cookie", "uuid=fake-uuid; Path=/; HttpOnly")
				fmt.Fprint(w, `{"base_resp":{"ret":0}}`)
			case "login":
				if r.Form.Get("login_type") != "3" {
					t.Fatalf("login form=%v", r.Form)
				}
				w.Header().Add("Set-Cookie", "session=fake-session; Path=/; HttpOnly")
				fmt.Fprint(w, `{"base_resp":{"ret":0},"redirect_url":"/cgi-bin/home?t=home/index&token=fake-token"}`)
			default:
				t.Fatalf("unexpected bizlogin action=%s", r.URL.Query().Get("action"))
			}
		case "/cgi-bin/scanloginqrcode":
			switch r.URL.Query().Get("action") {
			case "getqrcode":
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write([]byte("sanitized-qr-image"))
			case "ask":
				pollCount++
				if pollCount == 1 {
					fmt.Fprint(w, `{"base_resp":{"ret":0},"status":4,"acct_size":1}`)
					return
				}
				fmt.Fprint(w, `{"base_resp":{"ret":0},"status":1}`)
			default:
				t.Fatalf("unexpected scan action=%s", r.URL.Query().Get("action"))
			}
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

func TestWeChatMPLoginStatusWithoutActiveLoginDoesNotCallUpstream(t *testing.T) {
	var called atomic.Bool
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{
		BaseURL: "https://mp.weixin.qq.com",
		HTTPClient: &http.Client{Transport: weChatMPRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			called.Store(true)
			return nil, errors.New("unexpected upstream call")
		})},
		SecretStore: NewMemorySourceSecretStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.PollLogin(context.Background())
	if err != nil || status.State != WeChatMPLoginPending || called.Load() {
		t.Fatalf("status=%#v error=%v called=%v", status, err, called.Load())
	}
}

func TestWeChatMPLoginRejectsMalformedRedirectWithoutSaving(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/scanloginqrcode" {
			fmt.Fprint(w, `{"base_resp":{"ret":0},"status":1}`)
			return
		}
		fmt.Fprint(w, `{"base_resp":{"ret":0},"redirect_url":"https://example.invalid/escaped?token=secret"}`)
	}))
	defer server.Close()
	store := NewMemorySourceSecretStore()
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: server.URL, HTTPClient: server.Client(), SecretStore: store, SecretKey: "wechat-session"})
	if err != nil {
		t.Fatal(err)
	}
	client.loginActive = true
	_, err = client.PollLogin(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err=%v", err)
	}
	if _, loadErr := store.Load(context.Background(), "wechat-session"); loadErr != ErrSourceSecretNotFound {
		t.Fatalf("saved malformed session: %v", loadErr)
	}
}

func TestWeChatMPLoginAcceptsSameOriginAbsoluteRedirect(t *testing.T) {
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: "https://mp.weixin.qq.com", SecretStore: NewMemorySourceSecretStore()})
	if err != nil {
		t.Fatal(err)
	}
	token, err := client.validateRedirect("https://mp.weixin.qq.com/cgi-bin/home?t=home/index&token=safe-token")
	if err != nil || token != "safe-token" {
		t.Fatalf("token=%q error=%v", token, err)
	}
}

func TestWeChatMPLoginCompletesOnceAcrossConcurrentPolls(t *testing.T) {
	var loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/scanloginqrcode":
			fmt.Fprint(w, `{"base_resp":{"ret":0},"status":1}`)
		case "/cgi-bin/bizlogin":
			call := loginCalls.Add(1)
			if call > 1 {
				fmt.Fprint(w, `{"base_resp":{"ret":1000}}`)
				return
			}
			time.Sleep(25 * time.Millisecond)
			w.Header().Add("Set-Cookie", "session=concurrent-session; Path=/; HttpOnly")
			fmt.Fprint(w, `{"base_resp":{"ret":0},"redirect_url":"/cgi-bin/home?t=home/index&token=concurrent-token"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: server.URL, HTTPClient: server.Client(), SecretStore: NewMemorySourceSecretStore()})
	if err != nil {
		t.Fatal(err)
	}
	client.loginActive = true
	results := make(chan error, 6)
	var group sync.WaitGroup
	for index := 0; index < cap(results); index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			status, pollErr := client.PollLogin(context.Background())
			if pollErr == nil && status.State != WeChatMPLoginConfirmed {
				pollErr = fmt.Errorf("state=%s", status.State)
			}
			results <- pollErr
		}()
	}
	group.Wait()
	close(results)
	for pollErr := range results {
		if pollErr != nil {
			t.Errorf("PollLogin() error=%v", pollErr)
		}
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("login calls=%d", loginCalls.Load())
	}
}

func TestWeChatMPLoginDoesNotConfirmCorruptedPersistedSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/scanloginqrcode":
			fmt.Fprint(w, `{"base_resp":{"ret":0},"status":1}`)
		case "/cgi-bin/bizlogin":
			w.Header().Add("Set-Cookie", "session="+strings.Repeat("x", 256)+"; Path=/; HttpOnly")
			fmt.Fprint(w, `{"base_resp":{"ret":0},"redirect_url":"/cgi-bin/home?t=home/index&token=safe-token"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	store := corruptingSourceSecretStore{SourceSecretStore: NewMemorySourceSecretStore()}
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: server.URL, HTTPClient: server.Client(), SecretStore: store})
	if err != nil {
		t.Fatal(err)
	}
	client.loginActive = true
	status, err := client.PollLogin(context.Background())
	if err == nil || status.State == WeChatMPLoginConfirmed {
		t.Fatalf("status=%#v error=%v", status, err)
	}
}

func TestWeChatMPLoginMapsExpiredQRStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"base_resp":{"ret":0},"status":2}`)
	}))
	defer server.Close()
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: server.URL, HTTPClient: server.Client(), SecretStore: NewMemorySourceSecretStore()})
	if err != nil {
		t.Fatal(err)
	}
	client.loginActive = true
	status, err := client.PollLogin(context.Background())
	if err != nil || status.State != WeChatMPLoginExpired || status.RequiresAction != "login" {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestWeChatMPLoginReportsRejectedStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"base_resp":{"ret":200003,"err_msg":"private upstream detail"}}`)
	}))
	defer server.Close()
	client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{BaseURL: server.URL, HTTPClient: server.Client(), SecretStore: NewMemorySourceSecretStore()})
	if err != nil {
		t.Fatal(err)
	}
	client.loginActive = true
	_, err = client.PollLogin(context.Background())
	if err == nil || err.Error() != "wechat MP login was rejected (200003)" || strings.Contains(err.Error(), "private upstream detail") {
		t.Fatalf("error=%v", err)
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

func TestWeChatMPRequestFailureClassifiesNetworkErrorsWithoutLeakingURL(t *testing.T) {
	for _, test := range []struct {
		cause error
		want  string
	}{
		{cause: context.Canceled, want: "wechat MP request failed (canceled)"},
		{cause: context.DeadlineExceeded, want: "wechat MP request failed (timeout)"},
		{cause: io.ErrUnexpectedEOF, want: "wechat MP request failed (eof)"},
		{cause: errors.New("private transport detail"), want: "wechat MP request failed (network)"},
	} {
		client, err := NewWeChatMPSessionClient(WeChatMPSessionConfig{
			BaseURL: "https://mp.weixin.qq.com",
			HTTPClient: &http.Client{Transport: weChatMPRoundTripperFunc(func(*http.Request) (*http.Response, error) {
				return nil, test.cause
			})},
			SecretStore: NewMemorySourceSecretStore(),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.QRImage(context.Background())
		if err == nil || err.Error() != test.want || strings.Contains(err.Error(), "scanloginqrcode") || strings.Contains(err.Error(), "private transport detail") {
			t.Fatalf("cause=%v error=%v", test.cause, err)
		}
	}
}
