package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWCPlusAgentDoctorChecksLocalAndRemoteWithoutLeasing(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
		fmt.Fprint(w, "wcplus")
	}))
	defer local.Close()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/source-agent/lease" {
			t.Fatalf("unexpected remote path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer agent-secret" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var payload struct {
			Capabilities []string `json:"capabilities"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(payload.Capabilities) != 0 {
			t.Fatalf("doctor leased capabilities: %#v", payload.Capabilities)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"run":null}`)
	}))
	defer remote.Close()

	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"doctor"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI doctor: %v, stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ok":true`) || strings.Contains(stdout.String(), "agent-secret") {
		t.Fatalf("doctor output = %s", stdout.String())
	}
}

func TestWCPlusAgentOnceHeartbeatsFlushesAndPolls(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "wcplus")
	}))
	defer local.Close()
	var calls []string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","wcplus_healthy":true}}`)
		case "/api/source-agent/lease":
			calls = append(calls, "lease")
			fmt.Fprint(w, `{"run":null}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer remote.Close()

	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"once"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI once: %v, stderr=%s", err, stderr.String())
	}
	if strings.Join(calls, ",") != "heartbeat,lease" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestWCPlusAgentOnceExecutesLeasedArticleRun(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"gzh":{"Biz":"biz-med","Nickname":"医学参考"},"articles":[{"ID":"article-1","Title":"CLI 同步","URL":"https://mp.weixin.qq.com/s/article-1"}],"total":1}`)
		case "/api/article/content":
			fmt.Fprint(w, `{"ID":"article-1","Title":"CLI 同步","Nickname":"医学参考","URL":"https://mp.weixin.qq.com/s/article-1","Content":"这是一篇由 CLI once 模式读取并发送到远端知识库的完整测试文章正文。"}`)
		default:
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
	}))
	defer local.Close()
	var calls []string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","wcplus_healthy":true}}`)
		case r.URL.Path == "/api/source-agent/lease":
			calls = append(calls, "lease")
			fmt.Fprint(w, `{"run":{"id":"run-1","status":"running","requested_operation":"existing_articles","subscription":{"id":"sub-1","source_type":"wcplus_wechat_article","source_account_key":"biz-med","source_account":"医学参考","operation":"existing_articles","enabled":true,"options":{"limit":10}}}}`)
		case strings.HasSuffix(r.URL.Path, "/items"):
			calls = append(calls, "item")
			fmt.Fprint(w, `{"receipt":{"idempotency_key":"idem","run_id":"run-1","item_id":"item-1","source_item_key":"article-1","outcome":"new","target_book_id":"book-1","content_hash":"hash","accepted_at":"2026-07-10T00:00:00Z"}}`)
		case strings.HasSuffix(r.URL.Path, "/complete"):
			calls = append(calls, "complete")
			fmt.Fprint(w, `{"run":{"id":"run-1","status":"succeeded","new_count":1}}`)
		default:
			t.Fatalf("unexpected remote path: %s", r.URL.Path)
		}
	}))
	defer remote.Close()

	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"once"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI once: %v, stderr=%s", err, stderr.String())
	}
	if strings.Join(calls, ",") != "heartbeat,lease,item,complete" {
		t.Fatalf("calls = %#v", calls)
	}
	if !strings.Contains(stdout.String(), `"status":"succeeded"`) {
		t.Fatalf("once output = %s", stdout.String())
	}
}

func TestWCPlusAgentRequiresKnownModeAndConfiguration(t *testing.T) {
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"unknown"}, mapLookup(nil), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "doctor, once, or run") {
		t.Fatalf("unknown mode error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI(context.Background(), []string{"doctor"}, mapLookup(nil), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "KBASE_REMOTE_URL") {
		t.Fatalf("missing config error = %v", err)
	}
}

type testEnv map[string]string

func (e testEnv) Lookup(key string) (string, bool) {
	value, ok := e[key]
	return value, ok
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return testEnv(values).Lookup
}

func wcplusAgentTestEnv(remoteURL, localURL, stateDir string) testEnv {
	return testEnv{
		"KBASE_REMOTE_URL":         remoteURL,
		"KBASE_SOURCE_AGENT_TOKEN": "agent-secret",
		"KBASE_SOURCE_AGENT_ID":    "agent-a",
		"WCPLUS_AGENT_STATE_DIR":   stateDir,
		"WCPLUSPRO_BASE_URL":       localURL,
	}
}
