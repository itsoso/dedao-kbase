package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

func TestMain(m *testing.M) {
	previous := chatlogAgentUpgradeFactory
	chatlogAgentUpgradeFactory = func(*app.SourceAgentClient) (chatlogWorkerUpgradeRuntime, error) {
		return chatlogWorkerUpgradeRuntime{updater: &chatlogAgentFailClosedUpdater{}}, nil
	}
	code := m.Run()
	chatlogAgentUpgradeFactory = previous
	os.Exit(code)
}

type chatlogAgentTestActivator struct{}

func (chatlogAgentTestActivator) StartUpdater(context.Context) error { return nil }

func TestChatlogAgentConstructsRestrictedUpgradeBridge(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin worker upgrade bridge")
	}
	temporaryRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(temporaryRoot, "installed")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(root, "chatlog-agent")
	updater := filepath.Join(root, "source-agent-updater")
	for _, path := range []string{worker, updater} {
		if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, directory := range []string{".source-agent-staging", ".source-agent-handoff"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	client, err := app.NewSourceAgentClient(app.SourceAgentConfig{
		RemoteURL: "https://kbase.example.invalid", AgentToken: "shared-token",
		AgentID: "chatlog-agent-a", StateDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := newChatlogWorkerUpgradeBridge(client, worker, chatlogAgentTestActivator{})
	if err != nil {
		t.Fatal(err)
	}
	if bridge == nil {
		t.Fatal("restricted upgrade bridge is nil")
	}
	if err := bridge.Close(); err != nil {
		t.Fatal(err)
	}
}

type chatlogControlRunnerFake struct {
	result app.SourceAgentCycleResult
	err    error
	active bool
}

func (f chatlogControlRunnerFake) RunOnce(context.Context) (app.SourceAgentCycleResult, error) {
	return f.result, f.err
}

func (f chatlogControlRunnerFake) ControlActive() bool { return f.active }

type chatlogResearchClientFake struct {
	mu            sync.Mutex
	job           *app.ResearchWorkerJob
	renewErr      error
	renewCalls    int
	completeCalls int
	failCalls     int
}

func (f *chatlogResearchClientFake) Claim(context.Context, time.Duration) (*app.ResearchWorkerJob, error) {
	return f.job, nil
}

func (f *chatlogResearchClientFake) Renew(context.Context, app.ResearchWorkerJob, time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewCalls++
	return f.renewErr
}

func (f *chatlogResearchClientFake) Complete(_ context.Context, job app.ResearchWorkerJob, _ app.ResearchWorkerResult) (app.ResearchWorkerJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completeCalls++
	job.State = app.ResearchWorkerJobCompleted
	return job, nil
}

func (f *chatlogResearchClientFake) Fail(_ context.Context, job app.ResearchWorkerJob, code string, _ bool) (app.ResearchWorkerJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failCalls++
	job.State = app.ResearchWorkerJobFailed
	job.FailureCode = code
	return job, nil
}

func (f *chatlogResearchClientFake) counts() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renewCalls, f.completeCalls, f.failCalls
}

func TestChatlogAgentRenewsLeaseDuringLongResearchJob(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chatlog" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(40 * time.Millisecond)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer local.Close()
	reader, err := app.NewChatlogHTTPReader(app.ChatlogHTTPConfig{BaseURL: local.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := &chatlogResearchClientFake{job: &app.ResearchWorkerJob{
		JobID: "research-job-heartbeat", RunID: "research-run-heartbeat", TargetAgentID: "chatlog-agent-a",
		Tool: app.ResearchWorkerToolSearchChatlog, Arguments: []byte(`{"time_from":"2026-08-15T00:00:00Z","time_to":"2026-08-15T23:59:59Z","talker_ref":"conversation","keyword":"term","limit":1}`),
		State: app.ResearchWorkerJobLeased, Attempt: 1, MaxAttempts: 2, LeaseOwner: "chatlog-agent-a",
		LeaseID: "research-lease-heartbeat", RequestHash: "sha256:request",
	}}
	runtime := &chatlogAgentRuntime{
		control: chatlogControlRunnerFake{result: app.SourceAgentCycleResult{OK: true}}, researchClient: client,
		reader: reader, agentID: "chatlog-agent-a", locators: &chatlogLocatorCache{entries: map[string]chatlogLocatorCacheEntry{}},
		jobLease: time.Second, renewInterval: 5 * time.Millisecond,
	}
	if _, err := runtime.once(context.Background()); err != nil {
		t.Fatal(err)
	}
	renews, completes, fails := client.counts()
	if renews == 0 || completes != 1 || fails != 0 {
		t.Fatalf("renews=%d completes=%d fails=%d", renews, completes, fails)
	}
}

func TestChatlogAgentCancelsJobAndDoesNotSubmitAfterRenewLoss(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chatlog" {
			http.NotFound(w, r)
			return
		}
		<-r.Context().Done()
	}))
	defer local.Close()
	reader, err := app.NewChatlogHTTPReader(app.ChatlogHTTPConfig{BaseURL: local.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := &chatlogResearchClientFake{renewErr: errors.New("stale lease"), job: &app.ResearchWorkerJob{
		JobID: "research-job-renew-loss", RunID: "research-run-renew-loss", TargetAgentID: "chatlog-agent-a",
		Tool: app.ResearchWorkerToolSearchChatlog, Arguments: []byte(`{"time_from":"2026-08-15T00:00:00Z","time_to":"2026-08-15T23:59:59Z","talker_ref":"conversation","keyword":"term","limit":1}`),
		State: app.ResearchWorkerJobLeased, Attempt: 1, MaxAttempts: 2, LeaseOwner: "chatlog-agent-a",
		LeaseID: "research-lease-renew-loss", RequestHash: "sha256:request",
	}}
	runtime := &chatlogAgentRuntime{
		control: chatlogControlRunnerFake{result: app.SourceAgentCycleResult{OK: true}}, researchClient: client,
		reader: reader, agentID: "chatlog-agent-a", locators: &chatlogLocatorCache{entries: map[string]chatlogLocatorCacheEntry{}},
		jobLease: time.Second, renewInterval: 5 * time.Millisecond,
	}
	if _, err := runtime.once(context.Background()); !errors.Is(err, errChatlogWorkerLeaseLost) {
		t.Fatalf("once error=%v", err)
	}
	renews, completes, fails := client.counts()
	if renews != 1 || completes != 0 || fails != 0 {
		t.Fatalf("renews=%d completes=%d fails=%d", renews, completes, fails)
	}
}

func TestChatlogAgentDoesNotClaimResearchWhileControlCommandIsActive(t *testing.T) {
	remoteCalls := 0
	remote := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		remoteCalls++
	}))
	defer remote.Close()
	research, err := app.NewResearchWorkerClient(app.ResearchWorkerClientConfig{
		RemoteURL: remote.URL, Token: "shared-token", AgentID: "chatlog-agent-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &chatlogAgentRuntime{
		control:        chatlogControlRunnerFake{result: app.SourceAgentCycleResult{OK: true}, active: true},
		researchClient: research,
	}
	result, err := runtime.once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.Heartbeat || remoteCalls != 0 {
		t.Fatalf("result=%#v remoteCalls=%d", result, remoteCalls)
	}
}

func TestChatlogAgentRuntimeCloseJoinsOwnedResources(t *testing.T) {
	want := errors.New("close failed")
	runtime := &chatlogAgentRuntime{upgrade: chatlogCloseError{err: want}}
	if err := runtime.close(); !errors.Is(err, want) {
		t.Fatalf("close() error=%v", err)
	}
}

type chatlogCloseError struct{ err error }

func (c chatlogCloseError) Close() error { return c.err }

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

func TestChatlogAgentDoctorDoesNotRequireInstalledUpdater(t *testing.T) {
	previous := chatlogAgentUpgradeFactory
	chatlogAgentUpgradeFactory = func(*app.SourceAgentClient) (chatlogWorkerUpgradeRuntime, error) {
		return chatlogWorkerUpgradeRuntime{}, errors.New("updater should not be loaded")
	}
	defer func() { chatlogAgentUpgradeFactory = previous }()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session" {
			t.Fatalf("local path=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer local.Close()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/source-agent/lease" {
			t.Fatalf("remote path=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"run":null}`))
	}))
	defer remote.Close()
	var stdout, stderr bytes.Buffer
	err := runChatlogAgentCLI(
		context.Background(), []string{"doctor"},
		chatlogAgentTestEnvironment(remote.URL, local.URL, t.TempDir()).lookup,
		&stdout, &stderr,
	)
	if err != nil {
		t.Fatal(err)
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
			_, _ = w.Write([]byte(`{"job":{"job_id":"research-job-1","run_id":"research-run-1","target_agent_id":"chatlog-agent-a","tool":"search_chatlog","arguments":{"time_from":"2026-08-13T00:00:00Z","time_to":"2026-08-13T23:59:59Z","talker_ref":"conversation-1","keyword":"term","limit":10},"state":"leased","attempt":1,"max_attempts":2,"lease_owner":"chatlog-agent-a","lease_id":"research-lease-1","lease_expires_at":"2026-08-13T13:00:00Z","request_hash":"sha256:request"}}`))
		case "/api/research-worker/jobs/research-job-1/complete":
			if err := json.NewDecoder(r.Body).Decode(&completed); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"job":{"job_id":"research-job-1","run_id":"research-run-1","target_agent_id":"chatlog-agent-a","tool":"search_chatlog","arguments":{"time_from":"2026-08-13T00:00:00Z","time_to":"2026-08-13T23:59:59Z","talker_ref":"conversation-1","keyword":"term","limit":10},"state":"completed","attempt":1,"max_attempts":2,"lease_owner":"chatlog-agent-a","lease_id":"research-lease-1","request_hash":"sha256:request","result_fingerprint":"sha256:result"}}`))
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
	var cycle chatlogAgentCycleResult
	if err := json.Unmarshal(stdout.Bytes(), &cycle); err != nil {
		t.Fatal(err)
	}
	if cycle.CandidateCount != 1 || cycle.EvidenceCount != 0 {
		t.Fatalf("cycle counts=%#v", cycle)
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
	candidate, _ := items[0].(map[string]any)
	locator, _ := candidate["locator"].(map[string]any)
	conversationRef, _ := locator["conversation_ref"].(string)
	messageRef, _ := locator["message_ref"].(string)
	if candidate["selected"] != false || candidate["content"] != "" || !strings.HasPrefix(conversationRef, "sha256:") || conversationRef != messageRef {
		t.Fatalf("search candidate crossed privacy boundary: %#v", candidate)
	}
	encodedCompletion, err := json.Marshal(completed)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{privateContent, "conversation-1", "3001", "identity-1"} {
		if strings.Contains(string(encodedCompletion), forbidden) {
			t.Fatalf("completion leaked %q: %s", forbidden, encodedCompletion)
		}
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
	stateDir := t.TempDir()
	cache, err := openChatlogLocatorCache(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	messageTime, err := time.Parse(time.RFC3339, "2026-08-13T08:02:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	candidateRef, err := cache.store(app.ChatlogMessage{Time: messageTime, Talker: "conversation-1", MessageRef: "4002"})
	if err != nil {
		t.Fatal(err)
	}
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
	remote := chatlogAgentJobServer(t, `{"job":{"job_id":"research-job-2","run_id":"research-run-1","target_agent_id":"chatlog-agent-a","tool":"expand_chat_context","arguments":{"message_ref":"`+candidateRef+`","conversation_ref":"`+candidateRef+`","time":"2026-08-13","before":1,"after":1},"state":"leased","attempt":1,"max_attempts":2,"lease_owner":"chatlog-agent-a","lease_id":"research-lease-2","request_hash":"sha256:request"}}`)
	defer remote.Close()
	var stdout, stderr bytes.Buffer
	if err := runChatlogAgentCLI(context.Background(), []string{"once"}, chatlogAgentTestEnvironment(remote.URL, local.URL, stateDir).lookup, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	values, _ := url.ParseQuery(chatlogQuery)
	if values.Get("keyword") != "" || values.Get("sender") != "" || values.Get("talker") != "conversation-1" {
		t.Fatalf("context query=%s", chatlogQuery)
	}
}

func TestChatlogAgentIdentityResolutionReturnsOnlyOpaqueCandidates(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/contact" || r.URL.Query().Get("keyword") != "target-name" {
			t.Fatalf("request=%s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"items":[
			{"userName":"target-name","alias":"private-alias","remark":"private-remark","nickName":"private-name","isFriend":true},
			{"userName":"other-account","alias":"target-name","remark":"","nickName":"","isFriend":true}
		]}`))
	}))
	defer local.Close()
	reader, err := app.NewChatlogHTTPReader(app.ChatlogHTTPConfig{BaseURL: local.URL})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &chatlogAgentRuntime{reader: reader, agentID: "chatlog-agent-a"}
	result, err := runtime.executeJob(context.Background(), app.ResearchWorkerJob{
		Tool: app.ResearchWorkerToolResolveChatIdentity, Arguments: []byte(`{"identity_ref":"target-name"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.IdentityCandidates) != 2 || result.IdentityCandidates[0].IdentityID == "" {
		t.Fatalf("identity candidates=%#v", result.IdentityCandidates)
	}
	exact := result.IdentityCandidates[0]
	if exact.AccountID != exact.IdentityID || exact.TargetAccountID != exact.IdentityID {
		t.Fatalf("exact identity candidate=%#v", exact)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"target-name", "private-alias", "private-remark", "private-name", "other-account"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("identity result leaked %q: %s", forbidden, encoded)
		}
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
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"agent":{"agent_id":"chatlog-agent-a","desired_state":"active"}}`))
		case "/api/source-agent/commands/claim":
			_, _ = w.Write([]byte(`{"command":null}`))
		default:
			t.Fatalf("unexpected path=%s", r.URL.Path)
		}
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
