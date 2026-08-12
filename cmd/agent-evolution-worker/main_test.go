package main

import (
	"bytes"
	"context"
	"encoding/json"
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

func mapEnvironment(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
