package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestChatlogAgentBuildInfoAndOfflineConfigValidation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	lookupCalled := false
	if err := runChatlogAgentCLI(context.Background(), []string{"build-info"}, func(string) (string, bool) {
		lookupCalled = true
		return "", false
	}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if lookupCalled {
		t.Fatal("build-info read environment")
	}
	var info struct {
		WorkerType      string `json:"worker_type"`
		Version         string `json:"version"`
		ProtocolVersion string `json:"protocol_version"`
		Platform        string `json:"platform"`
		Architecture    string `json:"architecture"`
		Revision        string `json:"revision"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.WorkerType != chatlogAgentWorkerType || info.Version != chatlogAgentVersion ||
		info.ProtocolVersion != chatlogAgentProtocolVersion || info.Platform != runtime.GOOS ||
		info.Architecture != runtime.GOARCH || info.Revision != chatlogAgentRevision {
		t.Fatalf("info=%#v", info)
	}

	stdout.Reset()
	tokenLookedUp := false
	env := chatlogAgentTestEnvironment("https://kbase.example.invalid", "http://127.0.0.1:5030", t.TempDir())
	if err := runChatlogAgentCLI(context.Background(), []string{"check-config"}, func(key string) (string, bool) {
		if key == "KBASE_SOURCE_AGENT_TOKEN" {
			tokenLookedUp = true
		}
		return env.lookup(key)
	}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if tokenLookedUp || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("tokenLookedUp=%v stdout=%q stderr=%q", tokenLookedUp, stdout.String(), stderr.String())
	}
}

func TestChatlogAgentDoctorChecksLocalReadAPIAndRemoteAuth(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/session" {
			t.Fatalf("local request=%s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer local.Close()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/source-agent/lease" || r.Header.Get("Authorization") != "Bearer shared-token" {
			t.Fatalf("remote request=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"run":null}`))
	}))
	defer remote.Close()
	env := chatlogAgentTestEnvironment(remote.URL, local.URL, t.TempDir())
	var stdout, stderr bytes.Buffer
	if err := runChatlogAgentCLI(context.Background(), []string{"doctor"}, env.lookup, &stdout, &stderr); err != nil {
		t.Fatalf("doctor error=%v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ok":true`) || strings.Contains(stdout.String(), "shared-token") || strings.Contains(stdout.String(), env["CHATLOG_AGENT_STATE_DIR"]) {
		t.Fatalf("doctor output=%s", stdout.String())
	}
}

func TestChatlogAgentOnceHeartbeatsAndCompletesSearchWithoutLoggingContent(t *testing.T) {
	const privateContent = "PRIVATE_MESSAGE_CONTENT"
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("local method=%s", r.Method)
		}
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/api/v1/chatlog":
			if r.URL.Query().Get("format") != "json" || r.URL.Query().Get("talker") != "conversation-1" {
				t.Fatalf("query=%s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"seq":3001,"time":"2026-08-13T08:01:00+08:00","talker":"conversation-1","sender":"identity-1","type":1,"content":"` + privateContent + `"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer local.Close()
	var mu sync.Mutex
	var calls []string
	var heartbeat map[string]any
	var completed map[string]any
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"agent":{"agent_id":"chatlog-agent-a","desired_state":"active"}}`))
		case "/api/source-agent/commands/claim":
			_, _ = w.Write([]byte(`{"command":null}`))
		case "/api/research-worker/jobs/claim":
			_, _ = w.Write([]byte(`{"job":{"job_id":"research-job-1","run_id":"research-run-1","target_agent_id":"chatlog-agent-a","tool":"search_chatlog","arguments":{"time_from":"2026-08-13T00:00:00Z","time_to":"2026-08-13T23:59:59Z","talker_ref":"conversation-1","keyword":"term","limit":10},"state":"leased","attempt":1,"max_attempts":2,"lease_owner":"chatlog-agent-a","lease_expires_at":"2026-08-13T13:00:00Z","request_hash":"sha256:request"}}`))
		case "/api/research-worker/jobs/research-job-1/complete":
			if err := json.NewDecoder(r.Body).Decode(&completed); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"job":{"job_id":"research-job-1","run_id":"research-run-1","target_agent_id":"chatlog-agent-a","tool":"search_chatlog","arguments":{"time_from":"2026-08-13T00:00:00Z","time_to":"2026-08-13T23:59:59Z","talker_ref":"conversation-1","keyword":"term","limit":10},"state":"completed","attempt":1,"max_attempts":2,"lease_owner":"chatlog-agent-a","request_hash":"sha256:request","result_fingerprint":"sha256:result"}}`))
		default:
			t.Fatalf("unexpected remote path=%s", r.URL.Path)
		}
	}))
	defer remote.Close()
	env := chatlogAgentTestEnvironment(remote.URL, local.URL, t.TempDir())
	var stdout, stderr bytes.Buffer
	if err := runChatlogAgentCLI(context.Background(), []string{"once"}, env.lookup, &stdout, &stderr); err != nil {
		t.Fatalf("once error=%v stderr=%s", err, stderr.String())
	}
	if strings.Join(calls, ",") != "/api/source-agent/heartbeat,/api/source-agent/commands/claim,/api/research-worker/jobs/claim,/api/research-worker/jobs/research-job-1/complete" {
		t.Fatalf("calls=%v", calls)
	}
	health, _ := heartbeat["capability_health"].(map[string]any)
	if heartbeat["worker_type"] != chatlogAgentWorkerType || health["chatlog_read"] == nil {
		t.Fatalf("heartbeat=%#v", heartbeat)
	}
	result, _ := completed["result"].(map[string]any)
	items, _ := result["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("completed=%#v", completed)
	}
	for _, output := range []string{stdout.String(), stderr.String()} {
		for _, forbidden := range []string{privateContent, "identity-1", "shared-token", env["CHATLOG_AGENT_STATE_DIR"]} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("output leaked %q: %s", forbidden, output)
			}
		}
	}
}

func TestChatlogAgentContextExpansionDropsKeywordAndSender(t *testing.T) {
	var chatlogQuery string
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/api/v1/chatlog":
			chatlogQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`[
				{"seq":4001,"time":"2026-08-13T08:01:00+08:00","talker":"conversation-1","sender":"identity-2","type":1,"content":"before"},
				{"seq":4002,"time":"2026-08-13T08:02:00+08:00","talker":"conversation-1","sender":"identity-1","type":1,"content":"match"},
				{"seq":4003,"time":"2026-08-13T08:03:00+08:00","talker":"conversation-1","sender":"identity-2","type":1,"content":"after"}
			]`))
		}
	}))
	defer local.Close()
	remote := chatlogAgentJobServer(t, `{"job":{"job_id":"research-job-2","run_id":"research-run-1","target_agent_id":"chatlog-agent-a","tool":"expand_chat_context","arguments":{"message_ref":"4002","conversation_ref":"conversation-1","time":"2026-08-13","before":1,"after":1},"state":"leased","attempt":1,"max_attempts":2,"lease_owner":"chatlog-agent-a","request_hash":"sha256:request"}}`)
	defer remote.Close()
	var stdout, stderr bytes.Buffer
	if err := runChatlogAgentCLI(context.Background(), []string{"once"}, chatlogAgentTestEnvironment(remote.URL, local.URL, t.TempDir()).lookup, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	values, _ := url.ParseQuery(chatlogQuery)
	if values.Get("keyword") != "" || values.Get("sender") != "" || values.Get("talker") != "conversation-1" {
		t.Fatalf("context query=%s", chatlogQuery)
	}
}

func TestChatlogAgentReportsDependencyUnavailableWithoutLeakingDetails(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "PRIVATE_LOCAL_FAILURE", http.StatusServiceUnavailable)
	}))
	localURL := local.URL
	local.Close()
	var heartbeat map[string]any
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/source-agent/heartbeat" {
			t.Fatalf("unexpected path=%s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"agent":{"agent_id":"chatlog-agent-a","desired_state":"active"}}`))
	}))
	defer remote.Close()
	var stdout, stderr bytes.Buffer
	err := runChatlogAgentCLI(context.Background(), []string{"once"}, chatlogAgentTestEnvironment(remote.URL, localURL, t.TempDir()).lookup, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	health := heartbeat["capability_health"].(map[string]any)["chatlog_read"].(map[string]any)
	if health["healthy"] != false || health["code"] != "dependency_unavailable" || !strings.Contains(stdout.String(), `"ok":false`) {
		t.Fatalf("heartbeat=%#v stdout=%s", heartbeat, stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_LOCAL_FAILURE") || strings.Contains(stdout.String()+stderr.String(), localURL) {
		t.Fatalf("output leaked dependency detail: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func chatlogAgentJobServer(t *testing.T, jobResponse string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			_, _ = w.Write([]byte(`{"agent":{"agent_id":"chatlog-agent-a","desired_state":"active"}}`))
		case "/api/source-agent/commands/claim":
			_, _ = w.Write([]byte(`{"command":null}`))
		case "/api/research-worker/jobs/claim":
			_, _ = w.Write([]byte(jobResponse))
		case "/api/research-worker/jobs/research-job-2/complete":
			_, _ = w.Write([]byte(strings.Replace(jobResponse, `"state":"leased"`, `"state":"completed"`, 1)))
		default:
			t.Fatalf("unexpected path=%s", r.URL.Path)
		}
	}))
}

type chatlogAgentTestEnv map[string]string

func (environment chatlogAgentTestEnv) lookup(key string) (string, bool) {
	value, ok := environment[key]
	return value, ok
}

func chatlogAgentTestEnvironment(remoteURL, localURL, stateDir string) chatlogAgentTestEnv {
	return chatlogAgentTestEnv{
		"KBASE_REMOTE_URL": remoteURL, "KBASE_SOURCE_AGENT_TOKEN": "shared-token",
		"KBASE_SOURCE_AGENT_ID": "chatlog-agent-a", "CHATLOG_AGENT_STATE_DIR": stateDir,
		"CHATLOG_BASE_URL": localURL, "CHATLOG_AGENT_POLL_SECONDS": "1",
	}
}
