package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEvolutionWorkerClientUsesSharedTokenForLifecycle(t *testing.T) {
	var paths []string
	var heartbeatHealth SourceCapabilityHealth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer shared-worker-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			var heartbeat SourceAgentHeartbeat
			if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
				http.Error(w, "invalid heartbeat", http.StatusBadRequest)
				return
			}
			health, err := normalizeSourceCapabilityHealth(heartbeat.CapabilityHealth)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			heartbeatHealth = health[string(EvolutionCapabilityAgent)]
			writeHTTPJSON(w, http.StatusOK, map[string]any{"agent": SourceAgent{AgentID: "worker-a"}})
		case "/api/evolution/workers/lease":
			writeHTTPJSON(w, http.StatusOK, map[string]any{"work": EvolutionWork{WorkID: "work-a", Status: EvolutionWorkLeased, Attempt: 1, WorkerID: "worker-a", LeaseID: "lease-a"}})
		case "/api/evolution/workers/renew":
			writeHTTPJSON(w, http.StatusOK, map[string]any{"work": EvolutionWork{WorkID: "work-a", Status: EvolutionWorkLeased, Attempt: 1, WorkerID: "worker-a", LeaseID: "lease-a"}})
		case "/api/evolution/workers/generate":
			writeHTTPJSON(w, http.StatusOK, EvolutionGenerationResult{Candidate: &EvolutionCandidate{CandidateID: "candidate-a", ArtifactRef: "candidate:sha256:" + workerHex('a')}})
		case "/api/evolution/workers/evaluate":
			writeHTTPJSON(w, http.StatusOK, EvolutionEvaluationResult{Scorecard: &EvolutionScorecard{ScorecardID: "sha256:" + workerHex('b')}, RunStatus: EvolutionAwaitingApproval})
		case "/api/evolution/workers/complete":
			writeHTTPJSON(w, http.StatusOK, map[string]any{"work": EvolutionWork{WorkID: "work-a", Status: EvolutionWorkCompleted}})
		case "/api/evolution/workers/fail":
			writeHTTPJSON(w, http.StatusOK, map[string]any{"work": EvolutionWork{WorkID: "work-a", Status: EvolutionWorkPending}})
		case "/api/evolution/workers/defer":
			writeHTTPJSON(w, http.StatusOK, map[string]any{"work": EvolutionWork{WorkID: "work-a", Status: EvolutionWorkPending}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewEvolutionWorkerClient(EvolutionWorkerClientConfig{
		RemoteURL: server.URL, Token: "shared-worker-token", WorkerID: "worker-a", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.Heartbeat(ctx, EvolutionCapabilityAgent, "1.0.0", "revision-a"); err != nil {
		t.Fatal(err)
	}
	if heartbeatHealth.Version != "1.0.0" || heartbeatHealth.Revision != "revision-a" || heartbeatHealth.Code != "" {
		t.Fatalf("production heartbeat health=%#v", heartbeatHealth)
	}
	work, err := client.Lease(ctx, []EvolutionWorkerCapability{EvolutionCapabilityAgent}, time.Minute)
	if err != nil || work == nil {
		t.Fatalf("lease = %#v, %v", work, err)
	}
	if _, err := client.Renew(ctx, *work, time.Minute); err != nil {
		t.Fatal(err)
	}
	generated, err := client.Generate(ctx, *work)
	if err != nil || generated.Candidate == nil {
		t.Fatalf("generate = %#v, %v", generated, err)
	}
	evaluated, err := client.Evaluate(ctx, *work)
	if err != nil || evaluated.Scorecard == nil {
		t.Fatalf("evaluate = %#v, %v", evaluated, err)
	}
	if _, err := client.Complete(ctx, *work, generated.Candidate.ArtifactRef); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Fail(ctx, *work, "generation_failed", "candidate generation failed", time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Defer(ctx, *work, "knowledge_candidate_waiting", "waiting", time.Minute); err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/source-agent/heartbeat", "/api/evolution/workers/lease", "/api/evolution/workers/renew", "/api/evolution/workers/generate", "/api/evolution/workers/evaluate", "/api/evolution/workers/complete", "/api/evolution/workers/fail", "/api/evolution/workers/defer"}
	if encoded, _ := json.Marshal(paths); string(encoded) != mustEvolutionWorkerJSON(t, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestEvolutionWorkerClientRejectsUnsafeConfigurationAndOversizedResponse(t *testing.T) {
	for _, config := range []EvolutionWorkerClientConfig{
		{RemoteURL: "http://example.com", Token: "token", WorkerID: "worker-a"},
		{RemoteURL: "https://user@example.com", Token: "token", WorkerID: "worker-a"},
		{RemoteURL: "https://example.com", Token: "bad token", WorkerID: "worker-a"},
		{RemoteURL: "https://example.com", Token: "token", WorkerID: "../worker"},
	} {
		if _, err := NewEvolutionWorkerClient(config); err == nil {
			t.Fatalf("configuration should fail: %#v", config)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(make([]byte, evolutionWorkerClientResponseMaxBytes+1))
	}))
	defer server.Close()
	client, err := NewEvolutionWorkerClient(EvolutionWorkerClientConfig{RemoteURL: server.URL, Token: "token", WorkerID: "worker-a", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Lease(context.Background(), []EvolutionWorkerCapability{EvolutionCapabilityAgent}, time.Minute); err == nil {
		t.Fatal("oversized response should fail")
	}
}

func mustEvolutionWorkerJSON(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
