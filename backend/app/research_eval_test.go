package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResearchEvaluationSyntheticGoldPassesAllHardGates(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := finalizedResearchEvaluationPackage(t)
	suite := loadResearchEvaluationFixture(t)
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, now)
	if err != nil {
		t.Fatalf("EvaluateAgentPackageDeterministically() error = %v", err)
	}
	if !report.Passed || len(report.Failures) != 0 || len(report.HardGateFailures) != 0 {
		t.Fatalf("research evaluation did not pass: %#v", report)
	}
	for _, metric := range ResearchEvaluationMetricNames() {
		if report.Metrics[metric] != 1 {
			t.Fatalf("metric %q = %v, want 1", metric, report.Metrics[metric])
		}
	}
	if report.SuiteVersion != ResearchEvaluationSuiteVersion || report.InputHash == "" || report.EvaluatedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("research evaluation provenance = %#v", report)
	}
	if report.RetrievalIdentity.EmbeddingIdentity == "" || report.RetrievalIdentity.RerankerVersion == "" {
		t.Fatalf("research retrieval identity = %#v", report.RetrievalIdentity)
	}
}

func TestResearchEvaluationHardFailuresCannotBeOverriddenByAggregateScore(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*AgentEvaluationSuite)
		failure string
	}{
		{
			name: "fabricated recovery",
			mutate: func(suite *AgentEvaluationSuite) {
				suite.ResearchCases[1].Observed.RecoveryStatus = ResearchFactRecovery
			},
			failure: "fabricated_recovery",
		},
		{
			name: "non monotonic trend labelled monotonic",
			mutate: func(suite *AgentEvaluationSuite) {
				suite.ResearchCases[1].Observed.ReportedTrendDirection = ResearchTrendDown
			},
			failure: "numeric_trend_mismatch",
		},
		{
			name: "ambiguous identity selected",
			mutate: func(suite *AgentEvaluationSuite) {
				suite.ResearchCases[2].Observed.IdentityID = "identity-alpha"
			},
			failure: "ambiguous_identity_used",
		},
		{
			name: "amount transferred without case differences",
			mutate: func(suite *AgentEvaluationSuite) {
				suite.ResearchCases[1].Observed.TransferredAmount = "synthetic-dose"
				suite.ResearchCases[1].Observed.CaseDifferenceDimensions = nil
			},
			failure: "unsafe_case_transfer",
		},
		{
			name: "unsupported conclusion",
			mutate: func(suite *AgentEvaluationSuite) {
				suite.ResearchCases[1].Observed.Conclusions[0].EvidenceIDs = []string{"missing-evidence"}
			},
			failure: "unsupported_conclusion",
		},
		{
			name: "private projection leak",
			mutate: func(suite *AgentEvaluationSuite) {
				suite.ResearchCases[1].Observed.PrivateProjectionSafe = false
			},
			failure: "private_data_projection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			pkg := finalizedResearchEvaluationPackage(t)
			suite := loadResearchEvaluationFixture(t)
			tt.mutate(&suite)
			for metric := range pkg.EvaluationPolicy.MinimumScores {
				pkg.EvaluationPolicy.MinimumScores[metric] = 0
			}
			report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, testAgentPackageTime())
			if err != nil {
				t.Fatal(err)
			}
			if report.Passed || !containsResearchEvaluationFailure(report.HardGateFailures, tt.failure) {
				t.Fatalf("hard failures = %#v, want %q", report.HardGateFailures, tt.failure)
			}
		})
	}
}

func TestResearchEvaluationFixtureIsSyntheticAndContainsRequiredModes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "research-evaluation-v1.synthetic.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"cookie", "authorization", "source_body", "raw_response", "wechat", "wxid_"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Fatalf("synthetic fixture contains forbidden marker %q", forbidden)
		}
	}
	suite := loadResearchEvaluationFixture(t)
	modes := map[string]bool{}
	for _, evalCase := range suite.ResearchCases {
		modes[evalCase.Mode] = true
	}
	if !modes[ResearchModeQuick] || !modes[ResearchModeDeep] {
		t.Fatalf("fixture modes = %#v", modes)
	}
}

func TestResearchEvaluationSchemaDeclaresResearchCasesWithoutRequiringLegacyCases(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "agent-evaluation-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	required, _ := schema["required"].([]any)
	for _, field := range required {
		if field == "cases" {
			t.Fatal("research evaluation schema still requires legacy cases")
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	if properties["research_cases"] == nil {
		t.Fatal("research evaluation schema does not declare research_cases")
	}

	suite := loadResearchEvaluationFixture(t)
	payload, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(payload, &encoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := encoded["cases"]; ok {
		t.Fatalf("research suite serializes an empty legacy cases field: %s", payload)
	}
	validateSchemaInstance(t, "agent-evaluation-v1.schema.json", suite, true)
}

func TestResearchEvaluationTrustedSuiteGatesV4PublicationAndRecomputesAtPublish(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg := finalizedResearchEvaluationPackage(t)
	submitted := loadResearchEvaluationFixture(t)
	trusted := trustedResearchEvaluationFixture(submitted)

	if err := store.SaveTrustedAgentEvaluationSuite(pkg, trusted); err != nil {
		t.Fatalf("SaveTrustedAgentEvaluationSuite() error = %v", err)
	}
	resolved, report, err := EvaluateAgentPackageAgainstTrustedSuite(store, pkg, submitted, testAgentPackageTime())
	if err != nil {
		t.Fatalf("EvaluateAgentPackageAgainstTrustedSuite() error = %v", err)
	}
	if !report.Passed || report.TrustedSuiteHash == "" {
		t.Fatalf("trusted research report = %#v", report)
	}
	if err := store.SaveAgentPackageEvaluation(pkg, resolved, report); err != nil {
		t.Fatalf("SaveAgentPackageEvaluation() error = %v", err)
	}
	if err := ValidateAgentPackageEvaluationGate(store, pkg); err != nil {
		loaded, _ := store.LoadAgentPackageEvaluation(pkg.ContentHash)
		storedSuite, _ := store.LoadAgentPackageEvaluationSuite(pkg.ContentHash)
		evaluatedAt, _ := time.Parse(time.RFC3339Nano, loaded.EvaluatedAt)
		_, recomputed, _ := EvaluateAgentPackageAgainstTrustedSuite(store, pkg, *storedSuite, evaluatedAt)
		t.Fatalf("ValidateAgentPackageEvaluationGate() error = %v\nloaded=%#v\nrecomputed=%#v", err, loaded, recomputed)
	}
	published, created, err := PublishAgentPackage(
		store, pkg, "research-evaluation-publish", AgentPackageKnownToolIDs(), testAgentPackageTime(),
	)
	if err != nil || !created || published.LifecycleState != AgentPackagePublished {
		t.Fatalf("PublishAgentPackage() = %#v, %v, %v", published, created, err)
	}

	tampered := submitted
	tampered.ResearchCases = append([]ResearchEvaluationCase(nil), submitted.ResearchCases...)
	tampered.ResearchCases[0].Expected.Outcome = ResearchOutcomeZeroHit
	if _, _, err := EvaluateAgentPackageAgainstTrustedSuite(store, pkg, tampered, testAgentPackageTime()); err == nil || !strings.Contains(err.Error(), "trusted evaluation suite") {
		t.Fatalf("tampered trusted gold error = %v", err)
	}
}

func trustedResearchEvaluationFixture(suite AgentEvaluationSuite) AgentEvaluationSuite {
	trusted := suite
	trusted.ResearchCases = append([]ResearchEvaluationCase(nil), suite.ResearchCases...)
	for index := range trusted.ResearchCases {
		trusted.ResearchCases[index].Observed = ResearchEvaluationObservation{}
	}
	return trusted
}

func finalizedResearchEvaluationPackage(t *testing.T) AgentPackage {
	t.Helper()
	pkg := validAgentPackageV4()
	pkg.EvaluationPolicy.MinimumScores = researchEvaluationMinimumScores()
	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return finalized
}

func researchAgentRuntimeTestStore(t *testing.T) (*BookKnowledgeStore, AgentPackage) {
	t.Helper()
	store, pkg := agentRuntimeTestStore(t)
	pkg.Version = "4.0.0-research"
	pkg.ContentHash = ""
	pkg.LifecycleState = AgentPackageDraft
	pkg.CreatedAt = ""
	pkg.PublishedAt = ""
	pkg.Supersedes = ""
	applyAgentCompilationResearchPolicy(&pkg, true)
	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	submitted := loadResearchEvaluationFixture(t)
	trusted := trustedResearchEvaluationFixture(submitted)
	if err := store.SaveTrustedAgentEvaluationSuite(finalized, trusted); err != nil {
		t.Fatal(err)
	}
	resolved, report, err := EvaluateAgentPackageAgainstTrustedSuite(store, finalized, submitted, testAgentPackageTime())
	if err != nil || !report.Passed {
		t.Fatalf("research evaluation = %#v err=%v", report, err)
	}
	if err := store.SaveAgentPackageEvaluation(finalized, resolved, report); err != nil {
		t.Fatal(err)
	}
	published, _, err := PublishAgentPackage(
		store, finalized, "runtime-research-v4", AgentPackageKnownToolIDs(), testAgentPackageTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return store, *published
}

func loadResearchEvaluationFixture(t *testing.T) AgentEvaluationSuite {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "research-evaluation-v1.synthetic.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite AgentEvaluationSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	return suite
}

func containsResearchEvaluationFailure(failures []string, code string) bool {
	for _, failure := range failures {
		if strings.Contains(failure, code) {
			return true
		}
	}
	return false
}
