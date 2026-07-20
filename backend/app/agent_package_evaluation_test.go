package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAgentPackageEvaluationDeterministicAdapterCoversRequiredMetrics(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	suite := loadAgentEvaluationFixture(t)
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC)
	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, now)
	if err != nil {
		t.Fatalf("EvaluateAgentPackageDeterministically() error = %v", err)
	}
	if !report.Passed || !strings.HasPrefix(report.InputHash, "sha256:") ||
		report.EvaluatorVersion != AgentDeterministicEvaluatorVersion ||
		report.EvaluatedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("report identity = %#v", report)
	}
	for _, metric := range []string{"retrieval", "citations", "faithfulness", "abstention", "tool_choice", "tool_arguments", "latency", "cost"} {
		if report.Metrics[metric] != 1 {
			t.Fatalf("metric %q = %v, report=%#v", metric, report.Metrics[metric], report)
		}
	}
	replayed, err := EvaluateAgentPackageDeterministically(store, pkg, suite, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.InputHash != report.InputHash {
		t.Fatalf("deterministic input hash changed: %q != %q", replayed.InputHash, report.InputHash)
	}
}

func TestAgentPackageEvaluationExecutesGoldenRetrievalQuery(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentPackageTestRelease()
	release.Analysis.Claims[0].Statement = "Synthetic unrelated statement"
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateAgentPackageDeterministically(
		store, pkg, loadAgentEvaluationFixture(t), time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics["retrieval"] != 0 || report.Metrics["citations"] != 0 || report.Metrics["faithfulness"] != 0 {
		t.Fatalf("non-matching golden query passed behavioral metrics: %#v", report)
	}
}

func TestAgentPackageEvaluationJudgesDeterministicGroundedAnswer(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentPackageTestRelease()
	release.Analysis.Claims[0].Statement = "Grounded but incorrect statement"
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	pkg, _ := FinalizeAgentPackage(validAgentPackage())
	report, err := EvaluateAgentPackageDeterministically(
		store, pkg, loadAgentEvaluationFixture(t), time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics["retrieval"] != 1 || report.Metrics["citations"] != 1 || report.Metrics["faithfulness"] != 0 {
		t.Fatalf("incorrect grounded answer passed faithfulness: %#v", report)
	}
}

func TestAgentPackageEvaluationFailedAndMissingMetricsBlockPublication(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	knownTools := AgentReadOnlyToolIDs()
	now := time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC)

	if _, _, err := PublishAgentPackage(store, pkg, "missing-evaluation", knownTools, now); err == nil || !strings.Contains(err.Error(), "evaluation") {
		t.Fatalf("missing evaluation publication error = %v", err)
	}

	suite := loadAgentEvaluationFixture(t)
	suite.Cases[0].ExpectedIDs = []string{"chunk-other"}
	failed, err := EvaluateAgentPackageDeterministically(store, pkg, suite, now)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Passed || failed.Metrics["retrieval"] != 0 {
		t.Fatalf("failed report = %#v", failed)
	}
	if err := store.SaveAgentPackageEvaluation(pkg, suite, failed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PublishAgentPackage(store, pkg, "failed-evaluation", knownTools, now); err == nil || !strings.Contains(err.Error(), "retrieval") {
		t.Fatalf("failed evaluation publication error = %v", err)
	}

	passingStore := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, passingStore)
	passingSuite := loadAgentEvaluationFixture(t)
	passing, err := EvaluateAgentPackageDeterministically(passingStore, pkg, passingSuite, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := passingStore.SaveAgentPackageEvaluation(pkg, passingSuite, passing); err != nil {
		t.Fatal(err)
	}
	if _, created, err := PublishAgentPackage(passingStore, pkg, "passing-evaluation", knownTools, now); err != nil || !created {
		t.Fatalf("passing evaluation publication created=%v err=%v", created, err)
	}
}

func TestAgentPackageEvaluationPersistsInputAndEvaluatorIdentity(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg, _ := FinalizeAgentPackage(validAgentPackage())
	suite := loadAgentEvaluationFixture(t)
	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAgentPackageEvaluation(pkg, suite, report); err != nil {
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
	storedSuite, err := store.LoadAgentPackageEvaluationSuite(pkg.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	evaluatedAt, _ := time.Parse(time.RFC3339Nano, loaded.EvaluatedAt)
	recomputed, err := EvaluateAgentPackageDeterministically(store, pkg, *storedSuite, evaluatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*loaded, recomputed) {
		t.Fatalf("persisted evaluation changed trusted output:\nloaded=%#v\nrecomputed=%#v", *loaded, recomputed)
	}
	if err := ValidateAgentPackageEvaluationGate(store, pkg); err != nil {
		t.Fatalf("trusted persisted evaluation failed gate: %v", err)
	}
}

func TestAgentPackageEvaluationIgnoresCallerSuppliedObservations(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg, _ := FinalizeAgentPackage(validAgentPackage())
	suite := loadAgentEvaluationFixture(t)
	suite.Cases[0].ObservedIDs = []string{"caller-forged-result"}
	suite.Cases[1].ObservedIDs = []string{"caller-forged-citation"}
	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("trusted evaluator used caller observations: %#v", report)
	}
}

func TestAgentPackageEvaluationRequiresVersionedToolArguments(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg, _ := FinalizeAgentPackage(validAgentPackage())
	suite := loadAgentEvaluationFixture(t)
	for index := range suite.Cases {
		if suite.Cases[index].Metric == "tool_arguments" {
			suite.Cases[index].ExpectedArguments["package_version"] = "2.0.0"
		}
	}
	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics["tool_arguments"] != 0 || report.Passed {
		t.Fatalf("version-mismatched tool arguments passed: %#v", report)
	}
}

func TestAgentPackageEvaluationRejectsTamperingAndOverwrite(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg, _ := FinalizeAgentPackage(validAgentPackage())
	suite := loadAgentEvaluationFixture(t)
	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	forged := report
	forged.InputHash = "sha256:" + strings.Repeat("0", 64)
	if err := store.SaveAgentPackageEvaluation(pkg, suite, forged); err == nil || !strings.Contains(err.Error(), "input hash") {
		t.Fatalf("forged input hash error = %v", err)
	}
	forged = report
	forged.EvaluatorVersion = "unapproved-evaluator"
	if err := store.SaveAgentPackageEvaluation(pkg, suite, forged); err == nil || !strings.Contains(err.Error(), "evaluator") {
		t.Fatalf("forged evaluator error = %v", err)
	}
	if err := store.SaveAgentPackageEvaluation(pkg, suite, report); err != nil {
		t.Fatal(err)
	}
	overwrite := report
	overwrite.Metrics = map[string]float64{"retrieval": 0}
	if err := store.SaveAgentPackageEvaluation(pkg, suite, overwrite); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("evaluation overwrite error = %v", err)
	}
	storedPath := store.AgentPackageEvaluationPath(pkg.ContentHash)
	raw, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	var tampered AgentEvaluationReport
	if err := json.Unmarshal(raw, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.InputHash = "sha256:" + strings.Repeat("f", 64)
	payload, _ := json.Marshal(tampered)
	if err := os.WriteFile(storedPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentPackageEvaluationGate(store, pkg); err == nil || !strings.Contains(err.Error(), "input hash") {
		t.Fatalf("tampered persisted evaluation gate error = %v", err)
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
	suite := loadAgentEvaluationFixture(t)
	for index := range suite.Cases {
		if suite.Cases[index].Metric == "tool_arguments" {
			suite.Cases[index].ExpectedArguments["package_id"] = pkg.PackageID
			suite.Cases[index].ExpectedArguments["package_version"] = pkg.Version
			suite.Cases[index].ExpectedArguments["release_id"] = pkg.Releases[0].ReleaseID
		}
	}
	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, time.Date(2026, 7, 19, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("test evaluation did not pass: %#v", report)
	}
	if err := store.SaveAgentPackageEvaluation(pkg, suite, report); err != nil {
		t.Fatal(err)
	}
}
