package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentPackageEvaluationDeterministicAdapterCoversRequiredMetrics(t *testing.T) {
	suite := loadAgentEvaluationFixture(t)
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC)
	report, err := EvaluateAgentPackageDeterministically(pkg, suite, now)
	if err != nil {
		t.Fatalf("EvaluateAgentPackageDeterministically() error = %v", err)
	}
	if !report.Passed || !strings.HasPrefix(report.InputHash, "sha256:") ||
		report.EvaluatorVersion != AgentDeterministicEvaluatorVersion ||
		report.EvaluatedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("report identity = %#v", report)
	}
	for _, metric := range []string{"retrieval", "citations", "abstention", "tool_choice", "tool_arguments"} {
		if report.Metrics[metric] != 1 {
			t.Fatalf("metric %q = %v, report=%#v", metric, report.Metrics[metric], report)
		}
	}
	replayed, err := EvaluateAgentPackageDeterministically(pkg, suite, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.InputHash != report.InputHash {
		t.Fatalf("deterministic input hash changed: %q != %q", replayed.InputHash, report.InputHash)
	}
}

func TestAgentPackageEvaluationFailedAndMissingMetricsBlockPublication(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	knownTools := []string{"book-mcp/search", "book-mcp/resolve_citation"}
	now := time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC)

	if _, _, err := PublishAgentPackage(store, pkg, "missing-evaluation", knownTools, now); err == nil || !strings.Contains(err.Error(), "evaluation") {
		t.Fatalf("missing evaluation publication error = %v", err)
	}

	suite := loadAgentEvaluationFixture(t)
	suite.Cases[0].ObservedIDs = []string{"chunk-other"}
	failed, err := EvaluateAgentPackageDeterministically(pkg, suite, now)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Passed || failed.Metrics["retrieval"] != 0 {
		t.Fatalf("failed report = %#v", failed)
	}
	if err := store.SaveAgentPackageEvaluation(failed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PublishAgentPackage(store, pkg, "failed-evaluation", knownTools, now); err == nil || !strings.Contains(err.Error(), "retrieval") {
		t.Fatalf("failed evaluation publication error = %v", err)
	}

	passing, err := EvaluateAgentPackageDeterministically(pkg, loadAgentEvaluationFixture(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAgentPackageEvaluation(passing); err != nil {
		t.Fatal(err)
	}
	if _, created, err := PublishAgentPackage(store, pkg, "passing-evaluation", knownTools, now); err != nil || !created {
		t.Fatalf("passing evaluation publication created=%v err=%v", created, err)
	}
}

func TestAgentPackageEvaluationPersistsInputAndEvaluatorIdentity(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg, _ := FinalizeAgentPackage(validAgentPackage())
	report, err := EvaluateAgentPackageDeterministically(pkg, loadAgentEvaluationFixture(t), time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAgentPackageEvaluation(report); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadAgentPackageEvaluation(pkg.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PackageContentHash != pkg.ContentHash || loaded.InputHash != report.InputHash ||
		loaded.EvaluatorVersion != report.EvaluatorVersion || loaded.EvaluatedAt != report.EvaluatedAt {
		t.Fatalf("loaded report = %#v, want %#v", loaded, report)
	}
}

func TestAgentEvaluationSchemaAndFixtureContainNoSourceBodies(t *testing.T) {
	for _, name := range []string{
		filepath.Join("..", "..", "contracts", "agent-evaluation-v1.schema.json"),
		filepath.Join("..", "..", "testdata", "agent-evals", "book-agent-v1.json"),
	} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		lower := strings.ToLower(string(raw))
		for _, forbidden := range []string{"source_body", "raw_prompt", "cookie", "authorization"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s contains forbidden field %q", name, forbidden)
			}
		}
	}
}

func loadAgentEvaluationFixture(t *testing.T) AgentEvaluationSuite {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "agent-evals", "book-agent-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite AgentEvaluationSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	return suite
}

func savePassingAgentPackageTestEvaluation(t *testing.T, store *BookKnowledgeStore, pkg AgentPackage) {
	t.Helper()
	report, err := EvaluateAgentPackageDeterministically(pkg, loadAgentEvaluationFixture(t), time.Date(2026, 7, 19, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("test evaluation did not pass: %#v", report)
	}
	if err := store.SaveAgentPackageEvaluation(report); err != nil {
		t.Fatal(err)
	}
}
