package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type sourceAgentArtifactDownloadClientForTest interface {
	DownloadArtifact(
		context.Context,
		SourceAgentCommand,
		SourceAgentArtifactTarget,
		string,
	) (SourceAgentArtifactPublic, io.ReadCloser, error)
}

type sourceAgentClientRoundTripperForTest func(*http.Request) (*http.Response, error)

func (transport sourceAgentClientRoundTripperForTest) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

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
		{name: "agent ID invalid character", mutate: func(cfg *SourceAgentConfig) { cfg.AgentID = "agent/a" }, want: "invalid characters"},
		{name: "agent ID too long", mutate: func(cfg *SourceAgentConfig) { cfg.AgentID = strings.Repeat("a", 129) }, want: "exceeds 128"},
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

func TestSourceAgentConfigStrictURLsPreserveBasePaths(t *testing.T) {
	valid := SourceAgentConfig{
		RemoteURL:     "https://kbase.example.invalid/tenant/root/",
		AgentToken:    "agent-secret",
		AgentID:       "agent-a",
		StateDir:      t.TempDir(),
		WCPlusBaseURL: "http://127.0.0.1:5001/api/",
	}
	normalized, err := valid.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.RemoteURL != "https://kbase.example.invalid/tenant/root" || normalized.WCPlusBaseURL != "http://127.0.0.1:5001/api" {
		t.Fatalf("normalized URLs = %q, %q", normalized.RemoteURL, normalized.WCPlusBaseURL)
	}

	for _, test := range []struct {
		name   string
		remote string
		wcplus string
	}{
		{name: "remote query", remote: "https://kbase.example.invalid/base?admin=1"},
		{name: "remote fragment", remote: "https://kbase.example.invalid/base#fragment"},
		{name: "remote empty host", remote: "https:///base"},
		{name: "remote opaque", remote: "https:kbase.example.invalid/base"},
		{name: "wcplus userinfo", wcplus: "http://user:pass@127.0.0.1:5001"},
		{name: "wcplus query", wcplus: "http://127.0.0.1:5001/?debug=1"},
		{name: "wcplus fragment", wcplus: "http://127.0.0.1:5001/#fragment"},
		{name: "wcplus non-exact loopback", wcplus: "http://127.0.0.2:5001"},
		{name: "wcplus opaque", wcplus: "http:127.0.0.1:5001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			if test.remote != "" {
				config.RemoteURL = test.remote
			}
			if test.wcplus != "" {
				config.WCPlusBaseURL = test.wcplus
			}
			if err := config.Validate(); err == nil {
				t.Fatal("invalid URL was accepted")
			}
		})
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

func TestSourceAgentArtifactHandoffClientRequiresStrictSnapshotMetadata(t *testing.T) {
	artifactBytes := []byte("fixed worker artifact")
	command := SourceAgentCommand{
		ID: "command-1", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade,
		State: SourceAgentCommandDownloading, ExpectedCurrentVersion: "1.0.0",
		UpgradeSpec: &SourceAgentUpgradeSpec{ArtifactID: "artifact-1", ExpectedCurrentVersion: "1.0.0"},
	}
	target := SourceAgentArtifactTarget{
		WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64", CurrentVersion: "1.0.0",
	}
	baseHeaders := func() http.Header {
		headers := make(http.Header)
		headers.Set("Content-Type", "application/octet-stream")
		headers.Set("Content-Length", strconv.Itoa(len(artifactBytes)))
		headers.Set("X-Source-Agent-Command-ID", command.ID)
		headers.Set("X-Source-Agent-Artifact-ID", command.UpgradeSpec.ArtifactID)
		headers.Set("X-Source-Agent-Artifact-Version", "2.0.0")
		headers.Set("X-Source-Agent-Artifact-Worker-Type", target.WorkerType)
		headers.Set("X-Source-Agent-Artifact-Platform", target.Platform)
		headers.Set("X-Source-Agent-Artifact-Architecture", target.Architecture)
		headers.Set("X-Source-Agent-Artifact-Protocol-Version", "2026-08-01")
		headers.Set("X-Source-Agent-Artifact-Revision", sourceAgentArtifactTestRevision)
		headers.Set("X-Source-Agent-Artifact-Channel", "staging")
		headers.Set("X-Source-Agent-Artifact-Size", strconv.Itoa(len(artifactBytes)))
		headers.Set("X-Source-Agent-Artifact-SHA256", sha256HexForTest(artifactBytes))
		return headers
	}

	for _, test := range []struct {
		name      string
		mutate    func(http.Header)
		wantError bool
	}{
		{name: "accepts exact metadata and bytes"},
		{name: "rejects missing metadata", mutate: func(header http.Header) { header.Del("X-Source-Agent-Artifact-Revision") }, wantError: true},
		{name: "rejects duplicate metadata", mutate: func(header http.Header) { header.Add("X-Source-Agent-Artifact-Revision", strings.Repeat("b", 40)) }, wantError: true},
		{name: "rejects malformed metadata", mutate: func(header http.Header) { header.Set("X-Source-Agent-Artifact-SHA256", "not-a-digest") }, wantError: true},
		{name: "rejects runtime mismatch", mutate: func(header http.Header) { header.Set("X-Source-Agent-Artifact-Worker-Type", "wcplus-worker") }, wantError: true},
		{name: "rejects oversized metadata", mutate: func(header http.Header) {
			header.Set("X-Source-Agent-Artifact-Size", strconv.FormatInt(sourceAgentArtifactMaxBytes+1, 10))
		}, wantError: true},
		{name: "rejects stale command", mutate: func(header http.Header) { header.Set("X-Source-Agent-Command-ID", "command-other") }, wantError: true},
		{name: "rejects artifact mismatch", mutate: func(header http.Header) { header.Set("X-Source-Agent-Artifact-ID", "artifact-other") }, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/source-agent/artifacts/artifact-1/download" ||
					r.URL.Query().Get("agent_id") != "agent-a" || r.URL.Query().Get("command_id") != command.ID {
					t.Fatalf("unexpected artifact request: %s %s", r.Method, r.URL.String())
				}
				if got := r.Header.Get("Authorization"); got != "Bearer agent-secret" {
					t.Fatalf("Authorization=%q", got)
				}
				headers := baseHeaders()
				if test.mutate != nil {
					test.mutate(headers)
				}
				for name, values := range headers {
					for _, value := range values {
						w.Header().Add(name, value)
					}
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(artifactBytes)
			}))
			defer server.Close()
			client, err := NewSourceAgentClient(SourceAgentConfig{
				RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
				StateDir: t.TempDir(), HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			downloader, ok := any(client).(sourceAgentArtifactDownloadClientForTest)
			if !ok {
				t.Fatal("SourceAgentClient does not implement strict command-bound artifact download")
			}
			metadata, body, err := downloader.DownloadArtifact(context.Background(), command, target, "2026-08-01")
			if test.wantError {
				if err == nil || body != nil {
					t.Fatalf("metadata=%#v body=%T err=%v, want rejection before body handoff", metadata, body, err)
				}
				return
			}
			if err != nil || body == nil {
				t.Fatalf("metadata=%#v body=%T err=%v", metadata, body, err)
			}
			defer body.Close()
			got, err := io.ReadAll(body)
			if err != nil || !reflect.DeepEqual(got, artifactBytes) {
				t.Fatalf("bytes=%q err=%v", got, err)
			}
			if metadata.ID != "artifact-1" || metadata.WorkerType != target.WorkerType || metadata.Version != "2.0.0" ||
				metadata.Platform != target.Platform || metadata.Architecture != target.Architecture ||
				metadata.ProtocolVersion != "2026-08-01" || metadata.Revision != sourceAgentArtifactTestRevision ||
				metadata.Channel != "staging" || metadata.Size != int64(len(artifactBytes)) || metadata.SHA256 != sha256HexForTest(artifactBytes) {
				t.Fatalf("metadata=%#v", metadata)
			}
		})
	}
}

func TestSourceAgentUpdateGuardClientSendsExactFieldsAndClassifiesDenials(t *testing.T) {
	newClient := func(t *testing.T, handler http.Handler) *SourceAgentClient {
		t.Helper()
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client, err := NewSourceAgentClient(SourceAgentConfig{
			RemoteURL: server.URL, AgentToken: "agent-secret", AgentID: "agent-a",
			StateDir: t.TempDir(), HTTPClient: server.Client(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	exactCheck := func(t *testing.T) SourceAgentUpdateGuardCheck {
		t.Helper()
		check := SourceAgentUpdateGuardCheck{
			CommandID: "command-1", WorkerType: "wechat-worker", Version: "2.0.0",
			Revision: sourceAgentArtifactTestRevision, Channel: "staging",
		}
		value := reflect.ValueOf(&check).Elem()
		for name, fieldValue := range map[string]any{
			"ArtifactID": "artifact-1", "CurrentVersion": "1.0.0",
			"Size": int64(len("fixed worker artifact")), "SHA256": sha256HexForTest([]byte("fixed worker artifact")),
			"Platform": "darwin", "Architecture": "arm64", "ProtocolVersion": "2026-08-01",
		} {
			field := value.FieldByName(name)
			if !field.IsValid() || !field.CanSet() {
				t.Fatalf("SourceAgentUpdateGuardCheck is missing required field %s", name)
			}
			field.Set(reflect.ValueOf(fieldValue))
		}
		return check
	}

	t.Run("sends worker auth and exact bounded identity", func(t *testing.T) {
		client := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/source-agent/commands/command-1/guard" {
				t.Fatalf("unexpected guard request: %s %s", r.Method, r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer agent-secret" {
				t.Fatalf("Authorization=%q", got)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"agent_id": "agent-a", "artifact_id": "artifact-1",
				"current_version": "1.0.0", "target_version": "2.0.0",
				"revision": sourceAgentArtifactTestRevision, "channel": "staging",
				"size": float64(len("fixed worker artifact")), "sha256": sha256HexForTest([]byte("fixed worker artifact")),
				"worker_type": "wechat-worker", "platform": "darwin", "architecture": "arm64",
				"protocol_version": "2026-08-01",
			}
			if !reflect.DeepEqual(payload, want) {
				t.Fatalf("guard payload=%#v want=%#v", payload, want)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		guard, ok := any(client).(SourceAgentUpdateGuard)
		if !ok {
			t.Fatal("SourceAgentClient does not implement the production update guard")
		}
		if err := guard.Check(context.Background(), exactCheck(t)); err != nil {
			t.Fatal(err)
		}
	})

	for _, test := range []struct {
		name      string
		status    int
		retryable bool
	}{
		{name: "permanent conflict denial", status: http.StatusConflict},
		{name: "permanent auth denial", status: http.StatusUnauthorized},
		{name: "retryable timeout", status: http.StatusRequestTimeout, retryable: true},
		{name: "retryable throttle", status: http.StatusTooManyRequests, retryable: true},
		{name: "retryable server failure", status: http.StatusServiceUnavailable, retryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "private guard detail", test.status)
			}))
			guard, ok := any(client).(SourceAgentUpdateGuard)
			if !ok {
				t.Fatal("SourceAgentClient does not implement the production update guard")
			}
			err := guard.Check(context.Background(), exactCheck(t))
			if err == nil || sourceAgentRequestRetryable(err) != test.retryable {
				t.Fatalf("error=%v retryable=%t, want retryable=%t", err, sourceAgentRequestRetryable(err), test.retryable)
			}
			if strings.Contains(err.Error(), "private guard detail") {
				t.Fatalf("guard error leaked response body: %v", err)
			}
		})
	}

	for _, test := range []struct {
		name    string
		ctx     func() context.Context
		err     error
		want    error
		retry   bool
		private string
	}{
		{name: "transport failure is retryable and bounded", ctx: context.Background, err: errors.New("private dial detail"), retry: true, private: "private dial detail"},
		{name: "client timeout with healthy parent is retryable", ctx: context.Background, err: context.DeadlineExceeded, retry: true},
		{name: "parent cancellation is preserved", ctx: func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}, err: context.Canceled, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewSourceAgentClient(SourceAgentConfig{
				RemoteURL: "https://kbase.example.invalid", AgentToken: "agent-secret", AgentID: "agent-a",
				StateDir: t.TempDir(), HTTPClient: &http.Client{Transport: sourceAgentClientRoundTripperForTest(func(*http.Request) (*http.Response, error) {
					return nil, test.err
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			guard := any(client).(SourceAgentUpdateGuard)
			err = guard.Check(test.ctx(), exactCheck(t))
			if err == nil || sourceAgentRequestRetryable(err) != test.retry || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error=%v retryable=%t want_retryable=%t", err, sourceAgentRequestRetryable(err), test.retry)
			}
			if test.private != "" && strings.Contains(err.Error(), test.private) {
				t.Fatalf("guard transport error leaked private detail: %v", err)
			}
		})
	}
}
