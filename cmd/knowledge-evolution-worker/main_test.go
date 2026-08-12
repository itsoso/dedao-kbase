package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestKnowledgeEvolutionWorkerBuildInfoAndCheckConfig(t *testing.T) {
	var output bytes.Buffer
	if err := runKnowledgeEvolutionWorkerCLI(context.Background(), []string{"build-info"}, nil, &output); err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	if err := json.Unmarshal(output.Bytes(), &info); err != nil || info["component"] != "knowledge-evolution-worker" {
		t.Fatalf("build info=%v err=%v", info, err)
	}

	output.Reset()
	environment := map[string]string{
		"KBASE_REMOTE_URL": "https://kbase.example.test", "KBASE_SOURCE_AGENT_TOKEN": "shared-worker-token",
		"KBASE_EVOLUTION_WORKER_ID": "knowledge-evolution-worker-macos-a",
	}
	if err := runKnowledgeEvolutionWorkerCLI(context.Background(), []string{"check-config"}, mapKnowledgeEnvironment(environment), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"schema_version\":1,\"status\":\"ok\"}\n" {
		t.Fatalf("check config=%q", output.String())
	}
}

func TestKnowledgeEvolutionWorkerRejectsInvalidPolling(t *testing.T) {
	if err := runKnowledgeEvolutionWorkerCLI(context.Background(), []string{"check-config"}, mapKnowledgeEnvironment(map[string]string{
		"KBASE_REMOTE_URL": "https://kbase.example.test", "KBASE_SOURCE_AGENT_TOKEN": "shared-worker-token",
		"KBASE_EVOLUTION_WORKER_ID": "knowledge-evolution-worker-macos-a", "KBASE_EVOLUTION_POLL_SECONDS": "0",
	}), &bytes.Buffer{}); err == nil {
		t.Fatal("invalid polling should fail")
	}
}

func mapKnowledgeEnvironment(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
