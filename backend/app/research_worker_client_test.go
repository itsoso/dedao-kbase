package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResearchWorkerClientUsesSharedBearerTokenAndIdempotencyHeader(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer worker-secret" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/research-worker/jobs/claim":
			writeHTTPJSON(w, http.StatusOK, map[string]any{"job": ResearchWorkerJob{
				JobID: "job-1", RunID: "run-1", TargetAgentID: "agent-a", Tool: ResearchWorkerToolFetchChatMessage,
				Arguments: []byte(`{"message_ref":"message-1"}`), State: ResearchWorkerJobLeased, Attempt: 1,
				LeaseOwner: "agent-a", RequestHash: "sha256:request",
			}})
		case "/api/research-worker/jobs/job-1/renew":
			if r.Header.Get("Idempotency-Key") != "job-1:sha256:request:renew" {
				t.Fatalf("renew idempotency=%q", r.Header.Get("Idempotency-Key"))
			}
			writeHTTPJSON(w, http.StatusOK, map[string]any{"job": ResearchWorkerJob{
				JobID: "job-1", RunID: "run-1", TargetAgentID: "agent-a", Tool: ResearchWorkerToolFetchChatMessage,
				Arguments: []byte(`{"message_ref":"message-1"}`), State: ResearchWorkerJobLeased,
				Attempt: 1, LeaseOwner: "agent-a", RequestHash: "sha256:request",
			}})
		case "/api/research-worker/jobs/job-1/complete":
			if r.Header.Get("Idempotency-Key") != "job-1:sha256:request:complete" {
				t.Fatalf("complete idempotency=%q", r.Header.Get("Idempotency-Key"))
			}
			writeHTTPJSON(w, http.StatusOK, map[string]any{"job": ResearchWorkerJob{
				JobID: "job-1", RunID: "run-1", TargetAgentID: "agent-a", Tool: ResearchWorkerToolFetchChatMessage,
				Arguments: []byte(`{"message_ref":"message-1"}`), State: ResearchWorkerJobCompleted,
				Attempt: 1, LeaseOwner: "agent-a", RequestHash: "sha256:request",
				ResultFingerprint: "sha256:result",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewResearchWorkerClient(ResearchWorkerClientConfig{
		RemoteURL: server.URL, Token: "worker-secret", AgentID: "agent-a", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := client.Claim(context.Background(), time.Minute)
	if err != nil || job == nil || job.JobID != "job-1" {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	if err := client.Renew(context.Background(), *job, time.Minute); err != nil {
		t.Fatal(err)
	}
	completed, err := client.Complete(context.Background(), *job, ResearchWorkerResult{})
	if err != nil || completed.State != ResearchWorkerJobCompleted {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	if len(calls) != 3 {
		t.Fatalf("calls=%v", calls)
	}
}

func TestResearchWorkerClientRejectsMalformedOrForeignResponses(t *testing.T) {
	for _, response := range []string{
		`{"job":{"job_id":"job-1","run_id":"run-1","target_agent_id":"agent-b","tool":"fetch_chat_message","arguments":{"message_ref":"message-1"},"state":"leased","attempt":1,"lease_owner":"agent-b","request_hash":"sha256:request"}}`,
		`{"job":null,"unknown":"private"}`,
		`{"job":`,
	} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			client, err := NewResearchWorkerClient(ResearchWorkerClientConfig{
				RemoteURL: server.URL, Token: "worker-secret", AgentID: "agent-a", HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Claim(context.Background(), time.Minute); err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestResearchWorkerClientBoundsResponsesAndMarksTransportErrorsRetryable(t *testing.T) {
	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"job":null,"padding":"` + strings.Repeat("x", int(researchWorkerClientResponseMaxBytes)) + `"}`))
	}))
	client, err := NewResearchWorkerClient(ResearchWorkerClientConfig{
		RemoteURL: oversized.URL, Token: "worker-secret", AgentID: "agent-a", HTTPClient: oversized.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Claim(context.Background(), time.Minute); err == nil {
		t.Fatal("oversized response accepted")
	}
	oversized.Close()

	client, err = NewResearchWorkerClient(ResearchWorkerClientConfig{
		RemoteURL: "http://127.0.0.1:1", Token: "worker-secret", AgentID: "agent-a",
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Claim(context.Background(), time.Minute)
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("transport error=%T %v", err, err)
	}
}
