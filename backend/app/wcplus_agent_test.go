package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWCPlusAgentExecutesLeasedRunEndToEnd(t *testing.T) {
	var callsMu sync.Mutex
	var calls []string
	record := func(call string) {
		callsMu.Lock()
		defer callsMu.Unlock()
		calls = append(calls, call)
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			record("local-status")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<html><head><title>wcplusPro 9.483</title></head></html>`)
		case "/api/report/gzh_articles":
			w.Header().Set("Content-Type", "application/json")
			record("local-list")
			if r.URL.Query().Get("biz") != "biz-med" {
				t.Fatalf("list biz = %q", r.URL.Query().Get("biz"))
			}
			fmt.Fprint(w, `{
				"gzh":{"Biz":"biz-med","Nickname":"医学参考"},
				"articles":[{"ID":"article-1","Title":"可验证知识","URL":"https://mp.weixin.qq.com/s/article-1","PDateText":"2026-07-10"}],
				"total":1
			}`)
		case "/api/article/content":
			w.Header().Set("Content-Type", "application/json")
			record("local-content")
			fmt.Fprint(w, `{
				"ID":"article-1",
				"Title":"可验证知识",
				"Nickname":"医学参考",
				"URL":"https://mp.weixin.qq.com/s/article-1",
				"Content":"# 可验证知识\\n\\n每个结论都应保留来源、上下文和更新时间，以便其他系统进行可靠的交叉验证。",
				"PublishTime":"2026-07-10"
			}`)
		default:
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
	}))
	defer local.Close()

	harness := newWCPlusAgentServerHarness(t, "existing_articles", map[string]any{"limit": float64(10)}, record)
	defer harness.Close()
	agent := newTestWCPlusAgent(t, harness.RemoteURL, local.URL, t.TempDir(), nil)
	defer agent.Close()

	result, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if result.RunID != harness.Run.ID || result.Status != SourceRunSucceeded || result.Uploaded != 1 {
		t.Fatalf("unexpected cycle result: %#v", result)
	}
	persisted, err := harness.Sync.GetRun(harness.Run.ID)
	if err != nil || persisted.Status != SourceRunSucceeded || persisted.NewCount != 1 {
		t.Fatalf("persisted run = %#v, err=%v", persisted, err)
	}
	subscription, err := harness.Sync.GetSubscription(harness.Run.SubscriptionID)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	wantCursor := `{"published_at":"2026-07-10","source_item_key":"article-1"}`
	if subscription.Cursor != wantCursor {
		t.Fatalf("subscription cursor = %q, want %q", subscription.Cursor, wantCursor)
	}
	agents, err := harness.Sync.ListAgents()
	if err != nil || len(agents) != 1 || agents[0].WCPlusVersion != "9.483" {
		t.Fatalf("source agents = %#v, err=%v", agents, err)
	}
	books, err := harness.Books.ListBooks()
	if err != nil || len(books) != 1 || books[0].SourceKey != "article-1" {
		t.Fatalf("imported books = %#v, err=%v", books, err)
	}

	callsMu.Lock()
	gotCalls := strings.Join(calls, ",")
	callsMu.Unlock()
	wantCalls := "local-status,remote-heartbeat,remote-lease,local-list,local-content,remote-item,remote-complete"
	if gotCalls != wantCalls {
		t.Fatalf("calls = %s, want %s", gotCalls, wantCalls)
	}
}

func TestWCPlusAgentCreatesLinkTaskAndVerifiesDisappearance(t *testing.T) {
	var listCalls, taskPolls int
	var progress []WCPlusTask
	taskFinished := false
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/report/gzh_articles":
			listCalls++
			if !taskFinished {
				fmt.Fprint(w, `{"gzh":{"Biz":"biz-med","Nickname":"医学参考","Img":"https://example.test/account.png"},"articles":[],"total":0}`)
				return
			}
			fmt.Fprint(w, `{"gzh":{"Biz":"biz-med","Nickname":"医学参考","Img":"https://example.test/account.png"},"articles":[{"ID":"article-1","Title":"任务后文章","URL":"https://mp.weixin.qq.com/s/article-1"}],"total":1}`)
		case "/api/task/new":
			var payload WCPlusTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode task: %v", err)
			}
			if payload.CrawlerType != "gzh_article_link" || payload.Biz != "biz-med" || payload.ImageURL != "https://example.test/account.png" {
				t.Fatalf("task payload = %#v", payload)
			}
			fmt.Fprint(w, `{"task_id":"task-link-1","status":"ready"}`)
		case "/api/task/control":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode control: %v", err)
			}
			if payload["command"] != "run" {
				t.Fatalf("control payload = %#v", payload)
			}
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/api/task/all":
			taskPolls++
			if taskPolls == 1 {
				fmt.Fprint(w, `{"status":"ok","tasks":[{"ID":"task-link-1","CrawlerType":"gzh_article_link","Status":"running","StatusArticleTotalAmount":1,"StatusArticleFinishedAmount":1}]}`)
				return
			}
			taskFinished = true
			fmt.Fprint(w, `{"status":"ok","tasks":[]}`)
		case "/api/article/content":
			fmt.Fprint(w, `{"ID":"article-1","Title":"任务后文章","Nickname":"医学参考","URL":"https://mp.weixin.qq.com/s/article-1","Content":"任务完成后得到了一篇正文完整且可复核的公众号文章，用于测试消失任务的状态验证。"}`)
		default:
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
	}))
	defer local.Close()

	harness := newWCPlusAgentServerHarness(t, "sync_links", map[string]any{"limit": float64(10)}, nil)
	defer harness.Close()
	agent := newTestWCPlusAgent(t, harness.RemoteURL, local.URL, t.TempDir(), func(task WCPlusTask) {
		progress = append(progress, task)
	})
	defer agent.Close()
	result, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if result.Status != SourceRunSucceeded || listCalls < 2 || taskPolls != 2 {
		t.Fatalf("result=%#v listCalls=%d taskPolls=%d", result, listCalls, taskPolls)
	}
	if len(progress) != 1 || progress[0].ArticleFinished != 1 {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestWCPlusAgentIncludesAccountImageInReadingDataTask(t *testing.T) {
	taskCreated := false
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"gzh":{"Biz":"biz-med","Nickname":"医学参考","Img":"https://example.test/account.png"},"articles":[],"total":0}`)
		case "/api/task/new":
			var payload WCPlusTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode task: %v", err)
			}
			if payload.CrawlerType != "reading_data" || payload.ImageURL != "https://example.test/account.png" {
				t.Fatalf("task payload = %#v", payload)
			}
			taskCreated = true
			fmt.Fprint(w, `{"task_id":"task-reading-1","status":"ready"}`)
		case "/api/task/control":
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/api/task/all":
			fmt.Fprint(w, `{"tasks":[{"ID":"task-reading-1","Status":"succeeded"}]}`)
		default:
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
	}))
	defer local.Close()

	harness := newWCPlusAgentServerHarness(t, "sync_reading_data", nil, nil)
	defer harness.Close()
	agent := newTestWCPlusAgent(t, harness.RemoteURL, local.URL, t.TempDir(), nil)
	defer agent.Close()
	result, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if result.Status != SourceRunSucceeded || !taskCreated {
		t.Fatalf("result=%#v taskCreated=%v", result, taskCreated)
	}
}

func TestWCPlusAgentSyncLinksRefreshesExistingList(t *testing.T) {
	taskCreated := false
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"gzh":{"Biz":"biz-med","Nickname":"医学参考"},"articles":[{"ID":"article-1","Title":"已有链接","URL":"https://mp.weixin.qq.com/s/article-1"}],"total":1}`)
		case "/api/task/new":
			taskCreated = true
			fmt.Fprint(w, `{"task_id":"task-refresh-1","status":"ready"}`)
		case "/api/task/control":
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/api/task/all":
			fmt.Fprint(w, `{"tasks":[{"ID":"task-refresh-1","Status":"succeeded"}]}`)
		case "/api/article/content":
			fmt.Fprint(w, `{"ID":"article-1","Title":"已有链接","Nickname":"医学参考","URL":"https://mp.weixin.qq.com/s/article-1","Content":"已有列表不代表链接已经刷新，sync_links 仍需执行一次明确的 WC Plus 链接同步任务。"}`)
		default:
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
	}))
	defer local.Close()

	harness := newWCPlusAgentServerHarness(t, "sync_links", map[string]any{"limit": float64(10)}, nil)
	defer harness.Close()
	agent := newTestWCPlusAgent(t, harness.RemoteURL, local.URL, t.TempDir(), nil)
	defer agent.Close()
	result, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if result.Status != SourceRunSucceeded || !taskCreated {
		t.Fatalf("result=%#v taskCreated=%v", result, taskCreated)
	}
}

func TestWCPlusAgentRejectsDisappearedLinkTaskWhenArticleListIsUnchanged(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"gzh":{"Biz":"biz-med","Nickname":"医学参考","Img":"https://example.test/account.png"},"articles":[{"ID":"article-1","Title":"已有链接","URL":"https://mp.weixin.qq.com/s/article-1","PDateText":"2026-07-10"}],"total":1}`)
		case "/api/task/new":
			fmt.Fprint(w, `{"task_id":"task-disappeared-1","status":"ready"}`)
		case "/api/task/control":
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/api/task/all":
			fmt.Fprint(w, `{"tasks":[]}`)
		case "/api/article/content":
			fmt.Fprint(w, `{"ID":"article-1","Title":"已有链接","Nickname":"医学参考","URL":"https://mp.weixin.qq.com/s/article-1","Content":"已有文章不能作为新同步任务成功的证据；列表没有变化时必须报告结果无法验证。"}`)
		default:
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
	}))
	defer local.Close()

	harness := newWCPlusAgentServerHarness(t, "sync_links", map[string]any{"limit": float64(10)}, nil)
	defer harness.Close()
	agent := newTestWCPlusAgent(t, harness.RemoteURL, local.URL, t.TempDir(), nil)
	defer agent.Close()
	_, err := agent.RunOnce(context.Background())
	if !errors.Is(err, ErrWCPlusTaskOutcomeUnverified) {
		t.Fatalf("RunOnce error = %v, want ErrWCPlusTaskOutcomeUnverified", err)
	}
	persisted, getErr := harness.Sync.GetRun(harness.Run.ID)
	if getErr != nil || persisted.Status != SourceRunFailed || !strings.Contains(persisted.Error, ErrWCPlusTaskOutcomeUnverified.Error()) {
		t.Fatalf("persisted run = %#v, err=%v", persisted, getErr)
	}
}

func TestWCPlusAgentDoesNotRegressSubscriptionCursor(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"gzh":{"Biz":"biz-med","Nickname":"医学参考"},"articles":[{"ID":"article-old","Title":"较早文章","URL":"https://mp.weixin.qq.com/s/article-old","PDateText":"2026-07-09"}],"total":1}`)
		case "/api/article/content":
			fmt.Fprint(w, `{"ID":"article-old","Title":"较早文章","Nickname":"医学参考","URL":"https://mp.weixin.qq.com/s/article-old","Content":"重新同步较早文章时可以验证或更新正文，但不能让订阅游标倒退到更早的发布时间。","PublishTime":"2026-07-09"}`)
		default:
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
	}))
	defer local.Close()

	harness := newWCPlusAgentServerHarness(t, "existing_articles", map[string]any{"limit": float64(10)}, nil)
	defer harness.Close()
	subscription, err := harness.Sync.GetSubscription(harness.Run.SubscriptionID)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	wantCursor := `{"published_at":"2026-07-10","source_item_key":"article-new"}`
	subscription.Cursor = wantCursor
	if _, err := harness.Sync.UpdateSubscription(subscription.ID, SourceSubscriptionInput{
		SourceType:       subscription.SourceType,
		SourceAccountKey: subscription.SourceAccountKey,
		SourceAccount:    subscription.SourceAccount,
		AgentID:          subscription.AgentID,
		Schedule:         subscription.Schedule,
		Cursor:           subscription.Cursor,
		Operation:        subscription.Operation,
		Options:          subscription.Options,
		Enabled:          subscription.Enabled,
	}); err != nil {
		t.Fatalf("seed subscription cursor: %v", err)
	}
	agent := newTestWCPlusAgent(t, harness.RemoteURL, local.URL, t.TempDir(), nil)
	defer agent.Close()
	if _, err := agent.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	updated, err := harness.Sync.GetSubscription(subscription.ID)
	if err != nil || updated.Cursor != wantCursor {
		t.Fatalf("updated subscription=%#v err=%v", updated, err)
	}
}

func TestWCPlusAgentMarksRunPartialWhenOneArticleFails(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"gzh":{"Biz":"biz-med","Nickname":"医学参考"},"articles":[
				{"ID":"article-good","Title":"有效文章","URL":"https://mp.weixin.qq.com/s/article-good"},
				{"ID":"article-bad","Title":"失败文章","URL":"https://mp.weixin.qq.com/s/article-bad"}
			],"total":2}`)
		case "/api/article/content":
			if r.URL.Query().Get("id") == "article-bad" {
				writeHTTPError(w, http.StatusBadGateway, "local content unavailable")
				return
			}
			fmt.Fprint(w, `{"ID":"article-good","Title":"有效文章","Nickname":"医学参考","URL":"https://mp.weixin.qq.com/s/article-good","Content":"这是一篇正文完整的文章，其他文章失败时仍然应被可靠导入，并让整次运行呈现 partial。"}`)
		default:
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
	}))
	defer local.Close()

	harness := newWCPlusAgentServerHarness(t, "existing_articles", map[string]any{"limit": float64(10)}, nil)
	defer harness.Close()
	agent := newTestWCPlusAgent(t, harness.RemoteURL, local.URL, t.TempDir(), nil)
	defer agent.Close()
	result, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if result.Status != SourceRunPartial || result.Uploaded != 1 || result.Failed != 1 {
		t.Fatalf("result = %#v", result)
	}
	run, err := harness.Sync.GetRun(harness.Run.ID)
	if err != nil || run.Status != SourceRunPartial || run.NewCount != 1 || run.FailedCount != 1 {
		t.Fatalf("persisted run = %#v, err=%v", run, err)
	}
}

func TestWCPlusAgentRejectsUnverifiedAndBlockedTasksWithoutRetry(t *testing.T) {
	blockedReasons := []string{"not_max_version", "unactivated", "request throttled", "parameter expired"}
	for _, reason := range blockedReasons {
		t.Run(reason, func(t *testing.T) {
			polls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				polls++
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"tasks":[{"ID":"task-1","Status":"running","StatusError":%q}]}`, reason)
			}))
			defer server.Close()
			agent := &WCPlusAgent{
				wcplus:           NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: server.URL}),
				taskPollAttempts: 3,
				taskPollInterval: time.Millisecond,
			}
			err := agent.waitForTask(context.Background(), "run-1", "task-1", func(context.Context) (bool, error) {
				return false, nil
			})
			var blocked *WCPlusAgentBlockedError
			if !errors.As(err, &blocked) || polls != 1 {
				t.Fatalf("error=%#v polls=%d", err, polls)
			}
		})
	}

	statusOnly := wcplusTaskBlockedReason(WCPlusTask{Status: "not_max_version"})
	if statusOnly != "not_max_version" {
		t.Fatalf("status-only blocked reason = %q", statusOnly)
	}

	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tasks":[]}`)
	}))
	defer server.Close()
	agent := &WCPlusAgent{
		wcplus:           NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: server.URL}),
		taskPollAttempts: 1,
		taskPollInterval: time.Millisecond,
	}
	err := agent.waitForTask(context.Background(), "run-1", "task-1", func(context.Context) (bool, error) {
		return false, nil
	})
	if !errors.Is(err, ErrWCPlusTaskOutcomeUnverified) || polls != 1 {
		t.Fatalf("unverified error=%v polls=%d", err, polls)
	}
}

type wcplusAgentServerHarness struct {
	Books     *BookKnowledgeStore
	Sync      *SourceSyncStore
	Run       SourceSyncRun
	RemoteURL string
	server    *httptest.Server
}

func newWCPlusAgentServerHarness(t *testing.T, operation string, options map[string]any, record func(string)) *wcplusAgentServerHarness {
	t.Helper()
	root := t.TempDir()
	books := NewBookKnowledgeStore(root)
	syncStore, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	subscription, err := syncStore.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-med",
		SourceAccount:    "医学参考",
		AgentID:          "agent-a",
		Schedule:         "manual",
		Operation:        operation,
		Options:          options,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	run, err := syncStore.CreateRun(subscription.ID, operation)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            books,
		AuthToken:        "admin-secret",
		SourceSync:       syncStore,
		SourceAgentToken: "agent-secret",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read remote body: %v", err)
			}
			for _, forbidden := range []string{"127.0.0.1", "localhost", "request_params", "cookie"} {
				if strings.Contains(strings.ToLower(string(body)), forbidden) {
					t.Fatalf("remote body contains local acquisition state %q", forbidden)
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		if record != nil {
			switch {
			case r.URL.Path == "/api/source-agent/heartbeat":
				record("remote-heartbeat")
			case r.URL.Path == "/api/source-agent/lease":
				record("remote-lease")
			case strings.HasSuffix(r.URL.Path, "/items"):
				record("remote-item")
			case strings.HasSuffix(r.URL.Path, "/complete"):
				record("remote-complete")
			}
		}
		handler.ServeHTTP(w, r)
	}))
	return &wcplusAgentServerHarness{
		Books:     books,
		Sync:      syncStore,
		Run:       run,
		RemoteURL: server.URL,
		server:    server,
	}
}

func (h *wcplusAgentServerHarness) Close() {
	h.server.Close()
	_ = h.Sync.Close()
}

func newTestWCPlusAgent(t *testing.T, remoteURL, localURL, stateDir string, progress func(WCPlusTask)) *WCPlusAgent {
	t.Helper()
	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL:  remoteURL,
		AgentToken: "agent-secret",
		AgentID:    "agent-a",
		StateDir:   stateDir,
	})
	if err != nil {
		t.Fatalf("new source agent client: %v", err)
	}
	outbox, err := NewSourceAgentOutbox(stateDir)
	if err != nil {
		t.Fatalf("new source agent outbox: %v", err)
	}
	agent, err := NewWCPlusAgent(WCPlusAgentConfig{
		Client:           client,
		WCPlus:           NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: localURL}),
		Outbox:           outbox,
		Version:          "0.1.0",
		Capabilities:     []string{"existing_articles", "sync_links", "sync_content", "sync_reading_data"},
		LeaseDuration:    2 * time.Minute,
		TaskPollAttempts: 3,
		TaskPollInterval: time.Millisecond,
		OnTaskProgress:   progress,
	})
	if err != nil {
		t.Fatalf("new WC Plus agent: %v", err)
	}
	return agent
}
