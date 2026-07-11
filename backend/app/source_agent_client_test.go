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

func TestSourceAgentConfigValidation(t *testing.T) {
	valid := SourceAgentConfig{
		RemoteURL:  "https://kbase.example.invalid",
		AgentToken: "agent-secret",
		AgentID:    "agent-a",
		StateDir:   t.TempDir(),
	}
	tests := []struct {
		name   string
		mutate func(*SourceAgentConfig)
		want   string
	}{
		{name: "remote URL", mutate: func(cfg *SourceAgentConfig) { cfg.RemoteURL = "" }, want: "KBASE_REMOTE_URL"},
		{name: "agent token", mutate: func(cfg *SourceAgentConfig) { cfg.AgentToken = "" }, want: "KBASE_SOURCE_AGENT_TOKEN"},
		{name: "agent ID", mutate: func(cfg *SourceAgentConfig) { cfg.AgentID = "" }, want: "KBASE_SOURCE_AGENT_ID"},
		{name: "state directory", mutate: func(cfg *SourceAgentConfig) { cfg.StateDir = "" }, want: "SOURCE_AGENT_STATE_DIR"},
		{name: "insecure remote", mutate: func(cfg *SourceAgentConfig) { cfg.RemoteURL = "http://kbase.example.invalid" }, want: "HTTPS"},
		{name: "remote credentials", mutate: func(cfg *SourceAgentConfig) { cfg.RemoteURL = "https://user:pass@kbase.example.invalid" }, want: "credentials"},
		{name: "unicode token", mutate: func(cfg *SourceAgentConfig) { cfg.AgentToken = "agent-密钥" }, want: "ASCII"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}

	loopback := valid
	loopback.RemoteURL = "http://127.0.0.1:8719"
	if err := loopback.Validate(); err != nil {
		t.Fatalf("loopback remote rejected: %v", err)
	}
	loopback.WCPlusBaseURL = ""
	normalized, err := loopback.Normalized()
	if err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	if normalized.WCPlusBaseURL != "http://127.0.0.1:5001" {
		t.Fatalf("WCPlusBaseURL = %q", normalized.WCPlusBaseURL)
	}
}

func TestSourceAgentClientSendsScopedHeartbeatAndLease(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer agent-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		if strings.Contains(r.Header.Get("Authorization"), "admin") {
			t.Fatalf("admin credential leaked in Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			var payload SourceAgentHeartbeat
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode heartbeat: %v", err)
			}
			if payload.AgentID != "agent-a" || payload.Version != "0.1.0" || !payload.WCPlusHealthy {
				t.Fatalf("heartbeat = %#v", payload)
			}
			if strings.Join(payload.Capabilities, ",") != "existing_articles,sync_content" {
				t.Fatalf("capabilities = %#v", payload.Capabilities)
			}
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","version":"0.1.0","wcplus_healthy":true}}`)
		case "/api/source-agent/lease":
			calls = append(calls, "lease")
			var payload struct {
				AgentID      string   `json:"agent_id"`
				Capabilities []string `json:"capabilities"`
				LeaseSeconds int      `json:"lease_seconds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode lease: %v", err)
			}
			if payload.AgentID != "agent-a" || payload.LeaseSeconds != 120 || len(payload.Capabilities) != 2 {
				t.Fatalf("lease = %#v", payload)
			}
			fmt.Fprint(w, `{"run":{"id":"run-1","status":"running","requested_operation":"sync_content"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
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
		t.Fatalf("new source agent client: %v", err)
	}
	_, err = client.Heartbeat(context.Background(), SourceAgentHeartbeat{
		Version:       "0.1.0",
		Capabilities:  []string{"sync_content", "existing_articles"},
		WCPlusHealthy: true,
		WCPlusVersion: "9.84",
	})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	run, err := client.Lease(context.Background(), []string{"existing_articles", "sync_content"}, 2*time.Minute)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if run == nil || run.ID != "run-1" || run.Status != SourceRunRunning {
		t.Fatalf("run = %#v", run)
	}
	if strings.Join(calls, ",") != "heartbeat,lease" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestSourceAgentClientPreservesHTTPStatusWithoutResponseBody(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusConflict, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, "private upstream response body")
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
				t.Fatalf("new client: %v", err)
			}
			_, err = client.Lease(context.Background(), []string{"sync_content"}, time.Minute)
			var httpErr *SourceAgentHTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != status {
				t.Fatalf("Lease() error = %#v, want status %d", err, status)
			}
			if strings.Contains(err.Error(), "private upstream") {
				t.Fatalf("error leaked response body: %v", err)
			}
		})
	}
}

func TestSourceAgentClientAuthCheckDoesNotLeaseWork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Capabilities []string `json:"capabilities"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode auth check: %v", err)
		}
		if len(payload.Capabilities) != 0 {
			t.Fatalf("auth check requested capabilities: %#v", payload.Capabilities)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"run":null}`)
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
		t.Fatalf("new client: %v", err)
	}
	if err := client.CheckAuth(context.Background()); err != nil {
		t.Fatalf("CheckAuth() error = %v", err)
	}
}
