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

func TestEvidenceAuditPublicationIdentityUsesPublisherNotTransport(t *testing.T) {
	first := KnowledgeRelease{
		BookID: "release-a",
		Book: BookKnowledgeBook{
			SourceType:    "wechat_mp_article",
			SourceAccount: "Clinical Evidence Review",
		},
	}
	samePublisherDifferentTransport := KnowledgeRelease{
		BookID: "release-b",
		Book: BookKnowledgeBook{
			SourceType:    "dedao_course_article",
			SourceAccount: " clinical evidence review ",
		},
	}
	differentPublisherSameTransport := KnowledgeRelease{
		BookID: "release-c",
		Book: BookKnowledgeBook{
			SourceType:    "wechat_mp_article",
			SourceAccount: "Independent Trial Journal",
		},
	}

	if evidenceAuditPublicationIdentity(first) != evidenceAuditPublicationIdentity(samePublisherDifferentTransport) {
		t.Fatal("the same publisher must keep one publication identity across transport types")
	}
	if evidenceAuditPublicationIdentity(first) == evidenceAuditPublicationIdentity(differentPublisherSameTransport) {
		t.Fatal("different publishers must not collapse into one identity for the same transport type")
	}
}

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
	if report.RetrievalIdentity.EmbeddingIdentity != agentPackageSemanticEmbedderIdentity(pkg.RetrievalPolicy) ||
		report.RetrievalIdentity.RerankerVersion != AgentSemanticRerankerVersion {
		t.Fatalf("evaluation retrieval identity = %#v", report.RetrievalIdentity)
	}
	for _, metric := range []string{"retrieval", "retrieval_precision", "citations", "faithfulness", "abstention", "tool_choice", "tool_arguments", "task_completion", "latency", "cost"} {
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

func TestAgentPackageEvaluationUsesGoldenModelAndToolObservations(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg, _ := FinalizeAgentPackage(validAgentPackage())
	suite := loadAgentEvaluationFixture(t)
	for index := range suite.Cases {
		switch suite.Cases[index].Metric {
		case "faithfulness":
			suite.Cases[index].ModelOutput = "Unsupported synthetic answer [citation:citation-1]"
		case "tool_choice":
			suite.Cases[index].ProposedTool = "book-mcp/agent.delete"
		}
	}
	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics["faithfulness"] != 0 || report.Metrics["tool_choice"] != 0 || report.Passed {
		t.Fatalf("forged evaluator observations passed: %#v", report)
	}
}

func TestAgentPackageEvaluationMeasuresRetrievalPrecisionAndTaskCompletion(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	store.SetAgentSemanticEmbedder(&fakeAgentSemanticEmbedder{})
	release := agentPackageTestRelease()
	release.Analysis.Claims = append(release.Analysis.Claims, BookAnalysisClaim{
		ID: "claim-extra", Statement: "Another grounded synthetic statement", CitationIDs: []string{"citation-extra"},
	})
	release.Citations = append(release.Citations, BookKnowledgeCitation{
		CitationID: "citation-extra", BookID: "book-1", ChunkID: "chunk-extra",
	})
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
	pkg := validAgentPackage()
	pkg.Releases[0].CitationIDs = append(pkg.Releases[0].CitationIDs, "citation-extra")
	pkg, _ = FinalizeAgentPackage(pkg)
	report, err := EvaluateAgentPackageDeterministically(store, pkg, loadAgentEvaluationFixture(t), testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics["retrieval"] != 1 || report.Metrics["retrieval_precision"] != 0 {
		t.Fatalf("retrieval recall/precision = %#v", report.Metrics)
	}
	if report.Metrics["task_completion"] != 1 {
		t.Fatalf("task completion did not execute package chat: %#v", report)
	}
}

func TestAgentPackageEvaluationLatencyUsesStableRecordedObservation(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg, _ := FinalizeAgentPackage(validAgentPackage())
	suite := loadAgentEvaluationFixture(t)
	for index := range suite.Cases {
		if suite.Cases[index].Metric == "latency" {
			suite.Cases[index].RecordedLatencyMS = suite.Cases[index].MaxLatencyMS + 1
		}
	}
	first, err := EvaluateAgentPackageDeterministically(store, pkg, suite, testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	second, err := EvaluateAgentPackageDeterministically(store, pkg, suite, testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	if first.Metrics["latency"] != 0 || !reflect.DeepEqual(first, second) {
		t.Fatalf("recorded latency was not stable: first=%#v second=%#v", first, second)
	}
}

func TestAgentPackageEvaluationExecutesGoldenRetrievalQuery(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	store.SetAgentSemanticEmbedder(&fakeAgentSemanticEmbedder{})
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
	store.SetAgentSemanticEmbedder(&fakeAgentSemanticEmbedder{})
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

func TestAgentPackageCollectionEvaluationIsTrustedAndRequiredForPublication(t *testing.T) {
	store, _, release := agentCollectionRuntimeFixture(t)
	bundle, err := BuildControlledCollectionAgentDraftBundle(store, ControlledCollectionAgentDraftRequest{
		CollectionReleaseID: release.ReleaseID,
	}, AgentReadOnlyToolIDs())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTrustedAgentEvaluationSuite(bundle.Package, bundle.Suite); err != nil {
		t.Fatal(err)
	}
	resolved, trustedHash, err := store.ResolveTrustedAgentEvaluationSuite(bundle.Package, bundle.Suite)
	if err != nil || trustedHash == "" {
		t.Fatalf("resolved=%#v hash=%q err=%v", resolved, trustedHash, err)
	}
	report, err := EvaluateAgentPackageDeterministically(store, bundle.Package, resolved, testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	report.TrustedSuiteHash = trustedHash
	if !report.Passed {
		t.Fatalf("report=%#v", report)
	}
	if _, _, err := PublishAgentPackage(store, bundle.Package, "collection-before-evaluation", AgentReadOnlyToolIDs(), testAgentPackageTime()); err == nil || !strings.Contains(err.Error(), "evaluation") {
		t.Fatalf("publication without stored evaluation error=%v", err)
	}
	if err := store.SaveAgentPackageEvaluation(bundle.Package, resolved, report); err != nil {
		t.Fatal(err)
	}
	if _, created, err := PublishAgentPackage(store, bundle.Package, "collection-after-evaluation", AgentReadOnlyToolIDs(), testAgentPackageTime()); err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
}

func TestAgentPackageCollectionEvaluationRejectsTamperedGoldAndStaleMember(t *testing.T) {
	store, _, release := agentCollectionRuntimeFixture(t)
	bundle, err := BuildControlledCollectionAgentDraftBundle(store, ControlledCollectionAgentDraftRequest{
		CollectionReleaseID: release.ReleaseID,
	}, AgentReadOnlyToolIDs())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTrustedAgentEvaluationSuite(bundle.Package, bundle.Suite); err != nil {
		t.Fatal(err)
	}
	tampered := bundle.Suite
	tampered.Cases = append([]AgentEvaluationCase(nil), bundle.Suite.Cases...)
	tampered.Cases[0].ExpectedIDs = []string{"foreign-chunk"}
	if _, _, err := store.ResolveTrustedAgentEvaluationSuite(bundle.Package, tampered); err == nil || !strings.Contains(err.Error(), "modifies trusted") {
		t.Fatalf("tampered gold error=%v", err)
	}

	member, err := store.LoadPackage(release.Members[0].BookID)
	if err != nil {
		t.Fatal(err)
	}
	member.Chunks[0].Text += " stale"
	member.Book.ContentHash = ""
	if err := store.SavePackage(*member); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateAgentPackageDeterministically(store, bundle.Package, bundle.Suite, testAgentPackageTime()); err == nil || !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("stale member evaluation error=%v", err)
	}
}

func savePassingAgentPackageTestEvaluation(t *testing.T, store *BookKnowledgeStore, pkg AgentPackage) {
	t.Helper()
	store.SetAgentSemanticEmbedder(&fakeAgentSemanticEmbedder{})
	suite := loadAgentEvaluationFixture(t)
	if pkg.SchemaVersion == AgentPackageSchemaVersionV2 {
		identity := strings.TrimPrefix(pkg.ContentHash, "sha256:")
		if len(identity) > 12 {
			identity = identity[:12]
		}
		supported := persistEvidenceAuditEvaluationReport(
			t, store, pkg, EvidenceAuditVerdictSupported, false, "publish-"+identity+"-supported",
		)
		conflicted := persistEvidenceAuditEvaluationReport(
			t, store, pkg, EvidenceAuditVerdictMixed, true, "publish-"+identity+"-conflicted",
		)
		insufficient := persistEvidenceAuditEvaluationReport(
			t, store, pkg, EvidenceAuditVerdictInsufficient, false, "publish-"+identity+"-insufficient",
		)
		auditSuite := evidenceAuditEvaluationSuite(pkg, supported, conflicted, insufficient)
		suite.Cases = append(suite.Cases, auditSuite.Cases...)
	}
	for index := range suite.Cases {
		if len(suite.Cases[index].ProposedArguments) > 0 {
			for _, arguments := range []map[string]string{
				suite.Cases[index].ExpectedArguments,
				suite.Cases[index].ProposedArguments,
			} {
				if arguments == nil {
					continue
				}
				arguments["package_id"] = pkg.PackageID
				arguments["package_version"] = pkg.Version
				arguments["release_id"] = pkg.Releases[0].ReleaseID
			}
		}
		if suite.Cases[index].Metric == "retrieval_precision" {
			search, err := searchAgentPackageEvidence(store, pkg, suite.Cases[index].Input, pkg.RetrievalPolicy.MaxContextChunks)
			if err != nil {
				t.Fatal(err)
			}
			citations, err := resolveAgentRuntimeCitations(store, search.Results)
			if err != nil {
				t.Fatal(err)
			}
			suite.Cases[index].ExpectedIDs = suite.Cases[index].ExpectedIDs[:0]
			for _, citation := range citations {
				suite.Cases[index].ExpectedIDs = append(suite.Cases[index].ExpectedIDs, citation.ChunkID)
			}
		}
	}
	now := time.Date(2026, 7, 19, 13, 0, 0, 0, time.UTC)
	report := AgentEvaluationReport{}
	var err error
	if pkg.SchemaVersion == AgentPackageSchemaVersionV2 {
		trusted := suite
		trusted.Cases = append([]AgentEvaluationCase(nil), suite.Cases...)
		for index := range trusted.Cases {
			if isEvidenceAuditEvaluationMetric(trusted.Cases[index].Metric) {
				trusted.Cases[index].AuditID = ""
			}
		}
		if err := store.SaveTrustedAgentEvaluationSuite(pkg, trusted); err != nil {
			t.Fatal(err)
		}
		suite, report, err = EvaluateAgentPackageAgainstTrustedSuite(store, pkg, suite, now)
	} else {
		report, err = EvaluateAgentPackageDeterministically(store, pkg, suite, now)
	}
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
