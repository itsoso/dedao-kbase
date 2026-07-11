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

type fakeSourceAdapter struct {
	status  SourceCapabilityHealth
	run     SourceSyncRun
	called  bool
	result  SourceAdapterResult
	err     error
	enqueue *SourceArticleEnvelope
}

func (a *fakeSourceAdapter) Name() string                                  { return "fake" }
func (a *fakeSourceAdapter) Operations() []string                          { return []string{"sync_fake"} }
func (a *fakeSourceAdapter) Status(context.Context) SourceCapabilityHealth { return a.status }
func (a *fakeSourceAdapter) Execute(_ context.Context, run SourceSyncRun, sink SourceEnvelopeSink) (SourceAdapterResult, error) {
	a.called = true
	a.run = run
	if a.enqueue != nil {
		if _, err := sink.Enqueue(run.ID, *a.enqueue); err != nil {
			return SourceAdapterResult{}, err
		}
	}
	if a.err != nil || a.result.Cursor != "" {
		return a.result, a.err
	}
	return SourceAdapterResult{Cursor: "cursor-2"}, nil
}

func TestSourceAgentRunnerRejectsMissingDependencies(t *testing.T) {
	_, err := NewSourceAgentRunner(SourceAgentRunnerConfig{Adapter: &fakeSourceAdapter{}, LeaseDuration: time.Minute})
	if err == nil {
		t.Fatal("NewSourceAgentRunner succeeded without client and outbox")
	}
}

func TestSourceAgentRunnerUsesAdapterContract(t *testing.T) {
	var _ SourceAdapter = (*fakeSourceAdapter)(nil)
	var _ SourceEnvelopeSink = (*SourceAgentOutbox)(nil)
}

func TestSourceAgentRunnerPersistsAdapterFailureCheckpoint(t *testing.T) {
	var failedCursor string
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a"}}`)
		case "/api/source-agent/lease":
			fmt.Fprint(w, `{"run":{"id":"run-1","status":"running","requested_operation":"sync_fake","subscription":{"id":"sub-1","source_account_key":"account-key","source_account":"Account"}}}`)
		case "/api/source-agent/runs/run-1/items":
			calls = append(calls, "upload")
			fmt.Fprint(w, `{"receipt":{"run_id":"run-1","idempotency_key":"idem-1"}}`)
		case "/api/source-agent/runs/run-1/fail":
			calls = append(calls, "fail")
			var payload struct {
				Cursor string `json:"cursor"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode fail payload: %v", err)
			}
			failedCursor = payload.Cursor
			fmt.Fprint(w, `{"run":{"id":"run-1","status":"failed"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewSourceAgentClient(SourceAgentConfig{
		RemoteURL:  server.URL,
		AgentToken: "agent-secret",
		AgentID:    "agent-a",
		StateDir:   t.TempDir(),
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outbox.Close() })
	cause := errors.New("article download failed")
	adapter := &fakeSourceAdapter{
		status: SourceCapabilityHealth{Healthy: true},
		result: SourceAdapterResult{Cursor: "safe-cursor"},
		err:    &SourceAdapterExecutionError{Cursor: "safe-cursor", Err: cause},
		enqueue: &SourceArticleEnvelope{
			IdempotencyKey:  "idem-1",
			SourceType:      "wechat_mp_article",
			SourceAccountID: "account-key",
			SourceAccount:   "Account",
			SourceItemID:    "article-1",
			Title:           "Article 1",
			SourceURL:       "https://mp.weixin.qq.com/s/article-1",
			Content:         "# Article 1\n\nThis source article contains enough deterministic content for the runner checkpoint test.",
			ContentFormat:   "markdown",
		},
	}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{Client: client, Outbox: outbox, Adapter: adapter})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.RunOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("RunOnce() error=%v", err)
	}
	if failedCursor != "safe-cursor" {
		t.Fatalf("failed cursor=%q", failedCursor)
	}
	if strings.Join(calls, ",") != "upload,fail" {
		t.Fatalf("calls=%v, want upload before fail", calls)
	}
}
