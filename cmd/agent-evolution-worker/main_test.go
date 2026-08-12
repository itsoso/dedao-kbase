package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentEvolutionWorkerBuildInfoAndCheckConfig(t *testing.T) {
	var output bytes.Buffer
	if err := runAgentEvolutionWorkerCLI(context.Background(), []string{"build-info"}, nil, &output); err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	if err := json.Unmarshal(output.Bytes(), &info); err != nil || info["component"] != "agent-evolution-worker" {
		t.Fatalf("build info=%v err=%v", info, err)
	}

	output.Reset()
	environment := map[string]string{
		"KBASE_REMOTE_URL": "https://kbase.example.test", "KBASE_SOURCE_AGENT_TOKEN": "shared-worker-token",
		"KBASE_EVOLUTION_WORKER_ID": "agent-evolution-worker-macos-a",
	}
	if err := runAgentEvolutionWorkerCLI(context.Background(), []string{"check-config"}, mapEnvironment(environment), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"schema_version\":1,\"status\":\"ok\"}\n" {
		t.Fatalf("check config=%q", output.String())
	}
}

func TestAgentEvolutionWorkerRejectsIncompleteConfig(t *testing.T) {
	if err := runAgentEvolutionWorkerCLI(context.Background(), []string{"check-config"}, mapEnvironment(map[string]string{
		"KBASE_REMOTE_URL": "https://kbase.example.test",
	}), &bytes.Buffer{}); err == nil {
		t.Fatal("incomplete configuration should fail")
	}
}

func TestAgentEvolutionWorkerCheckLiveRequiresSuccessfulHeartbeat(t *testing.T) {
	heartbeats := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/source-agent/heartbeat" || request.Header.Get("Authorization") != "Bearer shared-worker-token" {
			http.Error(w, "unexpected request", http.StatusUnauthorized)
			return
		}
		heartbeats++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent":{"agent_id":"agent-evolution-worker-macos-a","worker_type":"agent-evolution-worker","version":"development","protocol_version":"evolution-worker.v1","capabilities":["agent_evolution"],"capability_health":{"agent_evolution":{"healthy":true,"version":"development"}},"last_seen_at":"2026-08-11T00:00:00Z"}}`))
	}))
	defer server.Close()
	environment := map[string]string{
		"KBASE_REMOTE_URL": server.URL, "KBASE_SOURCE_AGENT_TOKEN": "shared-worker-token",
		"KBASE_EVOLUTION_WORKER_ID": "agent-evolution-worker-macos-a",
	}
	var output bytes.Buffer
	if err := runAgentEvolutionWorkerCLI(context.Background(), []string{"check-live"}, mapEnvironment(environment), &output); err != nil {
		t.Fatal(err)
	}
	if heartbeats != 1 || output.String() != "{\"schema_version\":1,\"status\":\"live\"}\n" {
		t.Fatalf("check live heartbeats=%d output=%q", heartbeats, output.String())
	}
}

func TestAgentEvolutionWorkerCheckLiveRejectsFailedHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	environment := map[string]string{
		"KBASE_REMOTE_URL": server.URL, "KBASE_SOURCE_AGENT_TOKEN": "wrong-shared-worker-token",
		"KBASE_EVOLUTION_WORKER_ID": "agent-evolution-worker-macos-a",
	}
	var output bytes.Buffer
	if err := runAgentEvolutionWorkerCLI(context.Background(), []string{"check-live"}, mapEnvironment(environment), &output); err == nil {
		t.Fatal("failed heartbeat should reject liveness check")
	}
	if output.Len() != 0 {
		t.Fatalf("failed liveness output=%q", output.String())
	}
}

func mapEnvironment(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
