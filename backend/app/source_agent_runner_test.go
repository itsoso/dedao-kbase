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
	var leaseSeconds int
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a"}}`)
		case "/api/source-agent/lease":
			var payload struct {
				LeaseSeconds int `json:"lease_seconds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode lease payload: %v", err)
			}
			leaseSeconds = payload.LeaseSeconds
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
	if leaseSeconds != 5400 {
		t.Fatalf("lease seconds=%d, want 5400", leaseSeconds)
	}
	if strings.Join(calls, ",") != "upload,fail" {
		t.Fatalf("calls=%v, want upload before fail", calls)
	}
}

func TestSourceAgentRunnerReportsAdapterItemFailuresBeforeCompletion(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a"}}`)
		case "/api/source-agent/lease":
			fmt.Fprint(w, `{"run":{"id":"run-partial","status":"running","requested_operation":"sync_fake","subscription":{"id":"sub-1","source_account_key":"account-key","source_account":"Account"}}}`)
		case "/api/source-agent/runs/run-partial/items":
			var payload struct {
				SourceItemKey string `json:"source_item_key"`
				Error         string `json:"error"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode item failure: %v", err)
			}
			if payload.SourceItemKey != "article-media" || payload.Error != "media archival failed" {
				t.Errorf("failure payload=%#v", payload)
			}
			calls = append(calls, "failure")
			fmt.Fprint(w, `{"item":{"source_item_key":"article-media","outcome":"failed"}}`)
		case "/api/source-agent/runs/run-partial/complete":
			calls = append(calls, "complete")
			fmt.Fprint(w, `{"run":{"id":"run-partial","status":"partial"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewSourceAgentClient(SourceAgentConfig{RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a", StateDir: t.TempDir(), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	adapter := &fakeSourceAdapter{status: SourceCapabilityHealth{Healthy: true}, result: SourceAdapterResult{
		Cursor: "cursor-partial",
		Failures: []SourceAdapterItemFailure{{
			SourceItemKey:  "article-media",
			IdempotencyKey: "failure-idem",
			Error:          "media archival failed",
		}},
	}}
	runner, err := NewSourceAgentRunner(SourceAgentRunnerConfig{Client: client, Outbox: outbox, Adapter: adapter})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SourceRunPartial || strings.Join(calls, ",") != "failure,complete" {
		t.Fatalf("result=%#v calls=%v", result, calls)
	}
}
