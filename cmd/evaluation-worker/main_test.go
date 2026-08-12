package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestEvaluationWorkerBuildInfoAndCheckConfig(t *testing.T) {
	var output bytes.Buffer
	if err := runEvaluationWorkerCLI(context.Background(), []string{"build-info"}, nil, &output); err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	if err := json.Unmarshal(output.Bytes(), &info); err != nil || info["component"] != "evaluation-worker" {
		t.Fatalf("build info=%v err=%v", info, err)
	}
	output.Reset()
	environment := map[string]string{
		"KBASE_REMOTE_URL": "https://kbase.example.test", "KBASE_SOURCE_AGENT_TOKEN": "shared-worker-token",
		"KBASE_EVOLUTION_WORKER_ID": "evaluation-worker-macos-a",
	}
	if err := runEvaluationWorkerCLI(context.Background(), []string{"check-config"}, evaluationEnvironment(environment), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"schema_version\":1,\"status\":\"ok\"}\n" {
		t.Fatalf("check config=%q", output.String())
	}
}

func TestEvaluationWorkerRejectsUnknownCommand(t *testing.T) {
	if err := runEvaluationWorkerCLI(context.Background(), []string{"publish"}, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("evaluation worker must not expose publishing commands")
	}
}

func evaluationEnvironment(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
