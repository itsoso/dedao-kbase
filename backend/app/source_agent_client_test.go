package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestSourceAgentClientCommands(t *testing.T) {
	t.Run("claim and report use scoped command contracts", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if got := r.Header.Get("Authorization"); got != "Bearer agent-secret" {
				t.Fatalf("Authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			switch calls {
			case 1:
				if r.Method != http.MethodPost || r.URL.EscapedPath() != "/api/source-agent/commands/claim" {
					t.Fatalf("claim request = %s %s", r.Method, r.URL.EscapedPath())
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(payload, map[string]any{"agent_id": "agent-a"}) {
					t.Fatalf("claim payload = %#v", payload)
				}
				fmt.Fprint(w, `{"command":{"id":"cmd-1","target_agent_id":"agent-a","type":"upgrade","state":"claimed"}}`)
			case 2:
				if r.URL.EscapedPath() != "/api/source-agent/commands/cmd-1/progress" {
					t.Fatalf("progress path = %q", r.URL.EscapedPath())
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{"agent_id": "agent-a", "state": SourceAgentCommandDownloading, "message": "downloading"}
				if !reflect.DeepEqual(payload, want) {
					t.Fatalf("progress payload = %#v, want %#v", payload, want)
				}
				fmt.Fprint(w, `{"command":{"id":"cmd-1","target_agent_id":"agent-a","type":"upgrade","state":"downloading"}}`)
			case 3:
				if r.URL.EscapedPath() != "/api/source-agent/commands/cmd%2Fwith%20space/complete" {
					t.Fatalf("complete path = %q", r.URL.EscapedPath())
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{
					"agent_id":       "agent-a",
					"state":          SourceAgentCommandSucceeded,
					"code":           SourceAgentCommandCodeUpgradeComplete,
					"message":        "installed",
					"actual_version": "2.0.0",
				}
				if !reflect.DeepEqual(payload, want) {
					t.Fatalf("complete payload = %#v, want %#v", payload, want)
				}
				fmt.Fprint(w, `{"command":{"id":"cmd/with space","target_agent_id":"agent-a","type":"upgrade","state":"succeeded","actual_version":"2.0.0"}}`)
			case 4:
				fmt.Fprint(w, `{"command":null}`)
			default:
				t.Fatalf("unexpected request %d: %s %s", calls, r.Method, r.URL.EscapedPath())
			}
		}))
		defer server.Close()

		client, err := NewSourceAgentClient(SourceAgentConfig{
			RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
			StateDir: t.TempDir(), HTTPClient: server.Client(),
		})
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := client.ClaimCommand(context.Background())
		if err != nil || claimed == nil || claimed.ID != "cmd-1" || claimed.State != SourceAgentCommandClaimed {
			t.Fatalf("ClaimCommand() = %#v, %v", claimed, err)
		}
		progress, err := client.ReportCommand(context.Background(), "cmd-1", SourceAgentCommandDownloading, "", "downloading", "")
		if err != nil || progress.State != SourceAgentCommandDownloading {
			t.Fatalf("progress = %#v, %v", progress, err)
		}
		completed, err := client.ReportCommand(
			context.Background(), "cmd/with space", SourceAgentCommandSucceeded,
			SourceAgentCommandCodeUpgradeComplete, "installed", "2.0.0",
		)
		if err != nil || completed.State != SourceAgentCommandSucceeded || completed.ActualVersion != "2.0.0" {
			t.Fatalf("complete = %#v, %v", completed, err)
		}
		empty, err := client.ClaimCommand(context.Background())
		if err != nil || empty != nil {
			t.Fatalf("empty ClaimCommand() = %#v, %v", empty, err)
		}
	})

	t.Run("http errors never expose response bodies", func(t *testing.T) {
		privatePath := "/" + "Users/example/private-agent"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprintf(w, `{"error":"secret-token %s spec_json"}`, privatePath)
		}))
		defer server.Close()
		client, err := NewSourceAgentClient(SourceAgentConfig{
			RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
			StateDir: t.TempDir(), HTTPClient: server.Client(),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ClaimCommand(context.Background())
		var httpErr *SourceAgentHTTPError
		if !errors.As(err, &httpErr) || httpErr.Method != http.MethodPost ||
			httpErr.Path != "/api/source-agent/commands/claim" || httpErr.StatusCode != http.StatusConflict {
			t.Fatalf("ClaimCommand() error = %#v", err)
		}
		for _, secret := range []string{"secret-token", privatePath, "spec_json"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("HTTP error leaked %q: %v", secret, err)
			}
		}
	})

	t.Run("rejects command ID path segments without sending", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			fmt.Fprint(w, `{"command":{"id":"unexpected"}}`)
		}))
		defer server.Close()
		client, err := NewSourceAgentClient(SourceAgentConfig{
			RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
			StateDir: t.TempDir(), HTTPClient: server.Client(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.ReportCommand(
			context.Background(), "..", SourceAgentCommandFailed,
			SourceAgentCommandCodeUpgradeFailed, "failed", "",
		); err == nil {
			t.Fatal("ReportCommand accepted a dot-segment command ID")
		}
		if calls != 0 {
			t.Fatalf("ReportCommand sent %d requests for a dot-segment command ID", calls)
		}
	})

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing command key", body: `{}`},
		{name: "null envelope", body: `null`},
		{name: "empty command ID", body: `{"command":{"id":"","target_agent_id":"agent-a","type":"diagnose","state":"claimed"}}`},
		{name: "wrong target", body: `{"command":{"id":"cmd-claim","target_agent_id":"agent-other-secret","type":"diagnose","state":"claimed"}}`},
		{name: "wrong state", body: `{"command":{"id":"cmd-claim","target_agent_id":"agent-a","type":"diagnose","state":"queued"}}`},
		{name: "unknown type", body: `{"command":{"id":"cmd-claim","target_agent_id":"agent-a","type":"private-shell","state":"claimed"}}`},
	} {
		t.Run("rejects claim response with "+test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client, err := NewSourceAgentClient(SourceAgentConfig{
				RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
				StateDir: t.TempDir(), HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ClaimCommand(context.Background()); err == nil {
				t.Fatalf("accepted invalid claim response: %s", test.body)
			} else {
				for _, private := range []string{"agent-other-secret", "private-shell", "cmd-claim"} {
					if strings.Contains(err.Error(), private) {
						t.Fatalf("claim response error leaked %q: %v", private, err)
					}
				}
			}
		})
	}

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing command key", body: `{}`},
		{name: "null envelope", body: `null`},
		{name: "null command", body: `{"command":null}`},
		{name: "wrong target", body: `{"command":{"id":"cmd-expected","target_agent_id":"agent-other-secret","type":"upgrade","state":"downloading"}}`},
		{name: "wrong ID", body: `{"command":{"id":"cmd-other-secret","target_agent_id":"agent-a","type":"upgrade","state":"downloading"}}`},
		{name: "wrong state", body: `{"command":{"id":"cmd-expected","target_agent_id":"agent-a","type":"upgrade","state":"verified"}}`},
		{name: "unknown type", body: `{"command":{"id":"cmd-expected","target_agent_id":"agent-a","type":"private-shell","state":"downloading"}}`},
	} {
		t.Run("rejects report response with "+test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client, err := NewSourceAgentClient(SourceAgentConfig{
				RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
				StateDir: t.TempDir(), HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ReportCommand(
				context.Background(), "cmd-expected", SourceAgentCommandDownloading, "", "", "",
			); err == nil {
				t.Fatalf("accepted invalid report response: %s", test.body)
			} else {
				for _, private := range []string{"agent-other-secret", "cmd-other-secret", "private-shell"} {
					if strings.Contains(err.Error(), private) {
						t.Fatalf("report response error leaked %q: %v", private, err)
					}
				}
			}
		})
	}

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"command":`},
		{name: "trailing", body: `{"command":null}{"secret":"trailing"}`},
		{name: "oversized", body: `{"command":null,"padding":"` + strings.Repeat("x", (2<<20)+1) + `"}`},
	} {
		t.Run("rejects "+test.name+" responses", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client, err := NewSourceAgentClient(SourceAgentConfig{
				RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
				StateDir: t.TempDir(), HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ClaimCommand(context.Background()); err == nil {
				t.Fatalf("accepted %s response", test.name)
			}
		})
	}
}
