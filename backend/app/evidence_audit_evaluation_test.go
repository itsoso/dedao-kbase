package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

var evidenceAuditEvaluationMetrics = []string{
	"adjudication_consistency",
	"source_independence",
	"conflict_detection",
	"report_citation_completeness",
	"safe_insufficiency",
	"proofroom_projection_completeness",
}

func TestEvidenceAuditEvaluationReadsCompletedAuditsAndTracesFromStore(t *testing.T) {
	store, pkg := evidenceAuditEvaluationStore(t)
	supported := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictSupported, false, "supported")
	conflicted := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictMixed, true, "conflicted")
	insufficient := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictInsufficient, false, "insufficient")
	auditSuite := evidenceAuditEvaluationSuite(pkg, supported, conflicted, insufficient)
	suite := loadAgentEvaluationFixture(t)
	suite.SuiteVersion = pkg.EvaluationPolicy.SuiteVersion
	for index := range suite.Cases {
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
	suite.Cases = append(suite.Cases, auditSuite.Cases...)

	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, testAgentPackageTime())
	if err != nil {
		t.Fatalf("EvaluateAgentPackageDeterministically() error = %v", err)
	}
	for _, metric := range evidenceAuditEvaluationMetrics {
		if report.Metrics[metric] != 1 {
			t.Fatalf("metric %q = %v, report=%#v", metric, report.Metrics[metric], report)
		}
	}
	if !report.Passed {
		t.Fatalf("evidence audit evaluation did not pass: %#v", report)
	}
}

func TestTrustedEvidenceAuditEvaluationSuiteBindsOnlyAuditIDs(t *testing.T) {
	store, pkg := evidenceAuditEvaluationStore(t)
	supported := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictSupported, false, "trusted-supported")
	conflicted := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictMixed, true, "trusted-conflicted")
	insufficient := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictInsufficient, false, "trusted-insufficient")
	submitted := evidenceAuditEvaluationSuite(pkg, supported, conflicted, insufficient)
	trusted := submitted
	trusted.Cases = append([]AgentEvaluationCase(nil), submitted.Cases...)
	for index := range trusted.Cases {
		trusted.Cases[index].AuditID = ""
	}
	if err := store.SaveTrustedAgentEvaluationSuite(pkg, trusted); err != nil {
		t.Fatalf("SaveTrustedAgentEvaluationSuite() error = %v", err)
	}
	info, err := os.Stat(store.TrustedAgentEvaluationSuitePath(pkg))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("trusted evaluation suite mode = %o, want 600", info.Mode().Perm())
	}

	resolved, identity, err := store.ResolveTrustedAgentEvaluationSuite(pkg, submitted)
	if err != nil {
		t.Fatalf("ResolveTrustedAgentEvaluationSuite() error = %v", err)
	}
	if !strings.HasPrefix(identity, "sha256:") || !reflect.DeepEqual(resolved, submitted) {
		t.Fatalf("resolved trusted suite identity=%q suite=%#v", identity, resolved)
	}

	tampered := submitted
	tampered.Cases = append([]AgentEvaluationCase(nil), submitted.Cases...)
	tampered.Cases[0].ExpectedClaims = append(
		[]AgentEvaluationExpectedClaim(nil),
		submitted.Cases[0].ExpectedClaims...,
	)
	tampered.Cases[0].ExpectedClaims[0].Verdict = EvidenceAuditVerdictContradicted
	if _, _, err := store.ResolveTrustedAgentEvaluationSuite(pkg, tampered); err == nil ||
		!strings.Contains(err.Error(), "trusted evaluation suite") {
		t.Fatalf("tampered caller gold error = %v", err)
	}

	reusedAudit := submitted
	reusedAudit.Cases = append([]AgentEvaluationCase(nil), submitted.Cases...)
	reusedAudit.Cases[2].AuditID = reusedAudit.Cases[0].AuditID
	if _, _, err := store.ResolveTrustedAgentEvaluationSuite(pkg, reusedAudit); err == nil ||
		!strings.Contains(err.Error(), "distinct") {
		t.Fatalf("reused positive/conflict audit error = %v", err)
	}
}

func TestEvidenceAuditEvaluationRejectsAllInsufficientCoverage(t *testing.T) {
	store, pkg := evidenceAuditEvaluationStore(t)
	insufficient := persistEvidenceAuditEvaluationReport(
		t, store, pkg, EvidenceAuditVerdictInsufficient, false, "all-insufficient",
	)
	passed, err := executeEvidenceAuditEvaluationCase(store, pkg, AgentEvaluationCase{
		CaseID: "all-insufficient-independence", Metric: "source_independence", AuditID: insufficient.AuditID,
	})
	if err != nil {
		t.Fatalf("executeEvidenceAuditEvaluationCase() error = %v", err)
	}
	if passed {
		t.Fatal("an all-insufficient audit passed source independence")
	}

	allInsufficient := evidenceAuditEvaluationSuite(pkg, insufficient, insufficient, insufficient)
	for index := range allInsufficient.Cases {
		allInsufficient.Cases[index].AuditID = ""
		if len(allInsufficient.Cases[index].ExpectedClaims) > 0 {
			allInsufficient.Cases[index].ExpectedClaims[0].Verdict = EvidenceAuditVerdictInsufficient
			if allInsufficient.Cases[index].ExpectedClaims[0].Conflict != nil {
				conflict := false
				allInsufficient.Cases[index].ExpectedClaims[0].Conflict = &conflict
			}
		}
	}
	if err := ValidateTrustedAgentEvaluationSuite(pkg, allInsufficient); err == nil {
		t.Fatal("trusted evaluation suite accepted all-insufficient coverage")
	}
}

func TestEvidenceAuditEvaluationRejectsMissingAuditAndTamperedTraceIdentity(t *testing.T) {
	store, pkg := evidenceAuditEvaluationStore(t)
	supported := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictSupported, false, "trace")
	suite := evidenceAuditEvaluationSuite(pkg, supported, supported, supported)

	suite.Cases[0].AuditID = "audit-not-persisted"
	if _, err := EvaluateAgentPackageDeterministically(store, pkg, suite, testAgentPackageTime()); err == nil ||
		!strings.Contains(err.Error(), "load completed evidence audit") {
		t.Fatalf("missing persisted audit error = %v", err)
	}

	suite = evidenceAuditEvaluationSuite(pkg, supported, supported, supported)
	tracePath := store.AgentTracePath(supported.TraceID)
	var trace AgentTrace
	if err := readJSONFile(tracePath, &trace); err != nil {
		t.Fatal(err)
	}
	trace.Package.ContentHash = sha256Fingerprint([]byte("forged-package"))
	payload, err := encodeJSONFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracePath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateAgentPackageDeterministically(store, pkg, suite, testAgentPackageTime()); err == nil ||
		!strings.Contains(err.Error(), "trace") {
		t.Fatalf("tampered trace error = %v", err)
	}
}

func TestEvidenceAuditEvaluationRejectsStoredCitationIdentityForgery(t *testing.T) {
	store, pkg := evidenceAuditEvaluationStore(t)
	supported := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictSupported, false, "citation")
	suite := evidenceAuditEvaluationSuite(pkg, supported, supported, supported)

	release, err := store.LoadKnowledgeRelease("release-2")
	if err != nil {
		t.Fatal(err)
	}
	release.Book.SourceType = "forged_source"
	payload, err := json.MarshalIndent(release, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.KnowledgeReleasePath(release.ReleaseID), payload, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, testAgentPackageTime())
	if err != nil {
		t.Fatalf("EvaluateAgentPackageDeterministically() error = %v", err)
	}
	if report.Passed || report.Metrics["report_citation_completeness"] != 0 {
		t.Fatalf("forged release identity passed: %#v", report)
	}
}

func TestEvidenceAuditAdjudicationAndConflictGoldensMatchEverySelectedClaim(t *testing.T) {
	store, pkg := evidenceAuditEvaluationStore(t)
	audit := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictSupported, false, "golden")
	identity := evidenceAuditClaimIdentity(audit.ClaimAudits[0].SourceClaim)

	tests := []AgentEvaluationCase{
		{
			CaseID: "missing-gold", Metric: "adjudication_consistency", AuditID: audit.AuditID,
		},
		{
			CaseID: "wrong-verdict", Metric: "adjudication_consistency", AuditID: audit.AuditID,
			ExpectedClaims: []AgentEvaluationExpectedClaim{{
				ClaimIdentity: identity, Verdict: EvidenceAuditVerdictContradicted,
			}},
		},
		{
			CaseID: "unknown-claim", Metric: "adjudication_consistency", AuditID: audit.AuditID,
			ExpectedClaims: []AgentEvaluationExpectedClaim{{
				ClaimIdentity: sha256Fingerprint([]byte("unknown")), Verdict: EvidenceAuditVerdictSupported,
			}},
		},
		{
			CaseID: "conflict-without-gold", Metric: "conflict_detection", AuditID: audit.AuditID,
			ExpectedClaims: []AgentEvaluationExpectedClaim{{
				ClaimIdentity: identity, Verdict: EvidenceAuditVerdictSupported,
			}},
		},
	}
	for _, evalCase := range tests {
		t.Run(evalCase.CaseID, func(t *testing.T) {
			passed, err := executeEvidenceAuditEvaluationCase(store, pkg, evalCase)
			if err != nil {
				t.Fatalf("executeEvidenceAuditEvaluationCase() error = %v", err)
			}
			if passed {
				t.Fatalf("invalid gold fixture passed: %#v", evalCase)
			}
		})
	}
}

func TestEvidenceAuditProofroomCompletenessDeepComparesEveryField(t *testing.T) {
	store, pkg := evidenceAuditEvaluationStore(t)
	audit := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictSupported, false, "proofroom")
	preview, err := BuildProofroomEvidenceAuditProjection(audit)
	if err != nil {
		t.Fatal(err)
	}
	if !evidenceAuditProofroomProjectionComplete(audit, preview.Payload) {
		t.Fatal("valid projection failed deep comparison")
	}
	mutations := []func(*ProofroomEvidenceAuditProjection){
		func(value *ProofroomEvidenceAuditProjection) { value.Claims[0].Verdict = EvidenceAuditVerdictMixed },
		func(value *ProofroomEvidenceAuditProjection) { value.Claims[0].Evidence[0].ChunkID = "forged" },
		func(value *ProofroomEvidenceAuditProjection) { value.Claims[0].Limitations = nil },
		func(value *ProofroomEvidenceAuditProjection) { value.Summary.VerdictCounts = map[string]int{} },
		func(value *ProofroomEvidenceAuditProjection) { value.Proofroom.ReviewItems = nil },
		func(value *ProofroomEvidenceAuditProjection) { value.AdjudicationAuthority = "kbase" },
	}
	for index, mutate := range mutations {
		changed := preview.Payload
		changed.Claims = append([]ProofroomEvidenceAuditClaim(nil), preview.Payload.Claims...)
		changed.Claims[0].Evidence = append([]ProofroomEvidenceRef(nil), preview.Payload.Claims[0].Evidence...)
		changed.Claims[0].Limitations = append([]ProofroomSafeText(nil), preview.Payload.Claims[0].Limitations...)
		changed.Proofroom.ReviewItems = append([]ProofroomSafeText(nil), preview.Payload.Proofroom.ReviewItems...)
		mutate(&changed)
		if evidenceAuditProofroomProjectionComplete(audit, changed) {
			t.Fatalf("projection mutation %d passed deep comparison", index)
		}
	}
}

func TestAgentEvaluationV1RawFixtureHasHistoricalGoldenHash(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "agent-evaluation-v1.raw.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite AgentEvaluationSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	var original any
	var decoded any
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(roundTrip, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("historical v1 suite changed after round trip: %s", roundTrip)
	}
	hash, err := agentEvaluationInputHash(
		"sha256:7bbf6f0e33f76ce60bf2f9f3ce0df8dafd98d81e7285db8bc3277cc48457f8a1",
		suite,
	)
	if err != nil {
		t.Fatal(err)
	}
	const golden = "sha256:c7ac563069cf858f6f3d664b8cc14f461046182a9b9fbe187e380c7c8673b3dd"
	if hash != golden {
		t.Fatalf("historical v1 suite hash = %q, want %q", hash, golden)
	}
}

func TestContractSchemasValidateGoPositiveAndNegativeExamples(t *testing.T) {
	store, pkg := evidenceAuditEvaluationStore(t)
	audit := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictSupported, false, "schema")
	toObject := func(value any) map[string]any {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]any
		if err := json.Unmarshal(payload, &object); err != nil {
			t.Fatal(err)
		}
		return object
	}
	conflict := false
	suite := AgentEvaluationSuite{
		SchemaVersion: AgentEvaluationSchemaVersion,
		SuiteVersion:  pkg.EvaluationPolicy.SuiteVersion,
		Cases: []AgentEvaluationCase{{
			CaseID: "schema-case", Metric: "conflict_detection", AuditID: audit.AuditID,
			ExpectedClaims: []AgentEvaluationExpectedClaim{{
				ClaimIdentity: evidenceAuditClaimIdentity(audit.ClaimAudits[0].SourceClaim),
				Verdict:       audit.ClaimAudits[0].Verdict,
				Conflict:      &conflict,
			}},
		}},
	}
	validateSchemaInstance(t, "agent-package-v2.schema.json", pkg, true)
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", audit, true)
	validateSchemaInstance(t, "agent-evaluation-v1.schema.json", suite, true)

	subset := pkg
	subset.EvidencePolicy = &AgentPackageEvidencePolicy{}
	*subset.EvidencePolicy = *pkg.EvidencePolicy
	subset.EvidencePolicy.AllowedVerdicts = []string{EvidenceAuditVerdictSupported}
	finalized, err := FinalizeAgentPackage(subset)
	if err != nil {
		t.Fatalf("Go contract rejected legal verdict subset: %v", err)
	}
	validateSchemaInstance(t, "agent-package-v2.schema.json", finalized, true)

	emptyClaims := audit
	emptyClaims.ClaimAudits = nil
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", emptyClaims, false)

	missingEvidence := audit
	missingEvidence.ClaimAudits = append([]EvidenceAuditClaim(nil), audit.ClaimAudits...)
	missingEvidence.ClaimAudits[0].Evidence = nil
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", missingEvidence, false)

	retryDrift := audit
	retryDrift.Attempt = 2
	retryDrift.RequestIdentity = sha256Fingerprint([]byte("retry"))
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", retryDrift, false)

	queued := toObject(audit)
	queued["status"] = EvidenceAuditQueued
	for _, field := range []string{
		"started_at", "completed_at", "failed_at", "trace_id", "failure_code",
		"failure_summary", "claim_audits", "output_hash",
	} {
		delete(queued, field)
	}
	queued["summary"] = map[string]any{}
	queued["proofroom_projection"] = map[string]any{}
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", queued, true)
	queued["claim_audits"] = []any{}
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", queued, true)

	queuedPartial := toObject(queued)
	queuedPartial["summary"] = toObject(audit)["summary"]
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", queuedPartial, false)

	running := toObject(queued)
	running["status"] = EvidenceAuditRunning
	running["started_at"] = audit.StartedAt
	running["trace_id"] = audit.TraceID
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", running, true)
	running["claim_audits"] = []any{}
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", running, true)

	failed := toObject(running)
	failed["status"] = EvidenceAuditFailed
	failed["failed_at"] = audit.CompletedAt
	failed["failure_code"] = "invalid_model_output"
	failed["failure_summary"] = "model output did not match the contract"
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", failed, true)
	failed["claim_audits"] = []any{}
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", failed, true)

	failedPartial := toObject(failed)
	failedPartial["claim_audits"] = toObject(audit)["claim_audits"]
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", failedPartial, false)

	stale := toObject(audit)
	staleClaims := stale["claim_audits"].([]any)
	staleEvidence := staleClaims[0].(map[string]any)["evidence_refs"].([]any)
	staleEvidence[0].(map[string]any)["freshness_decision"] = "stale"
	validateSchemaInstance(t, "evidence-audit-v1.schema.json", stale, false)

	suiteObject := toObject(suite)
	suiteObject["cases"].([]any)[0].(map[string]any)["evidence_audit"] = map[string]any{"status": "completed"}
	validateSchemaInstance(t, "agent-evaluation-v1.schema.json", suiteObject, false)

	adjudicationWithoutVerdict := toObject(suite)
	adjudicationCase := adjudicationWithoutVerdict["cases"].([]any)[0].(map[string]any)
	adjudicationCase["metric"] = "adjudication_consistency"
	delete(adjudicationCase["expected_claims"].([]any)[0].(map[string]any), "verdict")
	validateSchemaInstance(t, "agent-evaluation-v1.schema.json", adjudicationWithoutVerdict, false)

	conflictWithoutGold := toObject(suite)
	conflictCase := conflictWithoutGold["cases"].([]any)[0].(map[string]any)
	delete(conflictCase["expected_claims"].([]any)[0].(map[string]any), "conflict")
	validateSchemaInstance(t, "agent-evaluation-v1.schema.json", conflictWithoutGold, false)
}

func validateSchemaInstance(t *testing.T, name string, value any, wantValid bool) {
	t.Helper()
	schema, err := jsonschema.Compile(filepath.Join("..", "..", "contracts", name))
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var instance any
	if err := json.Unmarshal(payload, &instance); err != nil {
		t.Fatal(err)
	}
	err = schema.Validate(instance)
	if wantValid && err != nil {
		t.Fatalf("%s rejected valid instance: %v\n%s", name, err, payload)
	}
	if !wantValid && err == nil {
		t.Fatalf("%s accepted invalid instance:\n%s", name, payload)
	}
}

func evidenceAuditEvaluationStore(t *testing.T) (*BookKnowledgeStore, AgentPackage) {
	t.Helper()
	store := NewBookKnowledgeStore(t.TempDir())
	store.SetAgentSemanticEmbedder(&fakeAgentSemanticEmbedder{})
	pkg := validAgentPackageV2()
	pkg.EvaluationPolicy.SuiteVersion = "clinical-evidence-audit-v2"
	pkg.RetrievalPolicy.Strategy = "lexical"
	for _, metric := range evidenceAuditEvaluationMetrics {
		pkg.EvaluationPolicy.MinimumScores[metric] = 1
	}

	primary := agentPackageTestRelease()
	primary.ReleaseID = "release-1"
	primary.ContentHash = pkg.Releases[0].ContentHash
	primary.Book.SourceType = "dedao_ebook"
	primary.Book.PublishedAt = "2026-07-20T00:00:00Z"
	primary.Analysis.Claims = []BookAnalysisClaim{{
		ID: "claim-1", Statement: "Synthetic grounded statement", CitationIDs: []string{"citation-1"},
	}}
	primary.Citations = []BookKnowledgeCitation{{
		CitationID: "citation-1", BookID: primary.BookID, ChunkID: "chunk-1",
		PublishedAt: primary.Book.PublishedAt,
	}}
	if err := store.saveKnowledgeRelease(primary); err != nil {
		t.Fatal(err)
	}

	support := agentPackageTestRelease()
	support.ReleaseID = "release-2"
	support.BookID = "book-2"
	support.Book.BookID = support.BookID
	support.ContentHash = pkg.Releases[1].ContentHash
	support.Book.SourceType = "wechat_mp_article"
	support.Book.PublishedAt = "2026-07-21T00:00:00Z"
	support.Analysis.Claims = []BookAnalysisClaim{{
		ID: "support-claim", Statement: "Independent supporting evidence", CitationIDs: []string{"citation-2"},
	}}
	support.Citations = []BookKnowledgeCitation{{
		CitationID: "citation-2", BookID: support.BookID, ChunkID: "support-chunk",
		PublishedAt: support.Book.PublishedAt,
	}}
	if err := store.saveKnowledgeRelease(support); err != nil {
		t.Fatal(err)
	}

	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return store, finalized
}

func persistEvidenceAuditEvaluationReport(
	t *testing.T,
	store *BookKnowledgeStore,
	pkg AgentPackage,
	verdict string,
	conflict bool,
	suffix string,
) EvidenceAudit {
	t.Helper()
	releases := make(map[string]KnowledgeRelease, len(pkg.Releases))
	for _, ref := range pkg.Releases {
		release, err := store.LoadKnowledgeRelease(ref.ReleaseID)
		if err != nil {
			t.Fatal(err)
		}
		releases[ref.ReleaseID] = *release
	}
	inputReleases, err := evidenceAuditInputReleaseRefs(pkg, releases)
	if err != nil {
		t.Fatal(err)
	}
	roles := make(map[string]string, len(pkg.EvidencePolicy.ReleaseRoles))
	for _, role := range pkg.EvidencePolicy.ReleaseRoles {
		roles[role.ReleaseID] = role.Role
	}
	sourceClaim := ""
	for releaseID, role := range roles {
		if role != EvidenceAuditReleasePrimary {
			continue
		}
		release := releases[releaseID]
		if release.Analysis != nil && len(release.Analysis.Claims) > 0 {
			sourceClaim = release.Analysis.Claims[0].Statement
		}
	}
	if strings.TrimSpace(sourceClaim) == "" {
		t.Fatal("evaluation fixture primary release has no source claim")
	}
	input := EvidenceAuditInput{
		SchemaVersion: EvidenceAuditSchemaVersion,
		Package: EvidenceAuditPackageRef{
			PackageID: pkg.PackageID, Version: pkg.Version, ContentHash: pkg.ContentHash,
		},
		EvidencePolicy: EvidenceAuditPolicySnapshot{
			MinimumIndependentSources: pkg.EvidencePolicy.MinimumIndependentSources,
			MaxClaims:                 pkg.EvidencePolicy.MaxClaims,
			MaxEvidencePerClaim:       pkg.EvidencePolicy.MaxEvidencePerClaim,
		},
		Model: EvidenceAuditModelIdentity{
			Provider: "tokenplan", Model: "fixture-model", Route: "evidence-audit",
		},
		Retrieval: EvidenceAuditRetrievalIdentity{
			Strategy:         pkg.RetrievalPolicy.Strategy,
			IndexVersion:     "fixture-index-v1",
			RerankerVersion:  pkg.RetrievalPolicy.RerankerVersion,
			EmbeddingVersion: pkg.RetrievalPolicy.EmbeddingVersion,
		},
		Releases: inputReleases, Subject: "Evaluation " + suffix,
		Scope: "Deterministic evaluation.", SelectedClaims: []string{sourceClaim},
	}
	now := testAgentPackageTime()
	queued, _, err := CreateEvidenceAudit(store, input, "evaluation-"+suffix, now)
	if err != nil {
		t.Fatal(err)
	}
	traceID := "trace-evaluation-" + suffix
	if _, err := StartEvidenceAudit(store, queued.AuditID, traceID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	var primaryRefs []EvidenceAuditEvidenceRef
	var supportingRefs []EvidenceAuditEvidenceRef
	for _, release := range inputReleases {
		citation := release.Citations[0]
		stored := releases[release.ReleaseID]
		publishedAt := stored.Book.PublishedAt
		if strings.TrimSpace(publishedAt) == "" {
			publishedAt = testAgentPackageTime().Format(time.RFC3339)
		}
		ref := EvidenceAuditEvidenceRef{
			ReleaseID: release.ReleaseID, ContentHash: release.ContentHash,
			Role: release.Role, SourceType: release.SourceType,
			PublicationIdentity: release.PublicationIdentity,
			ClaimID:             citation.ClaimID, ChunkID: citation.ChunkID, CitationID: citation.CitationID,
			PublishedAt: publishedAt, FreshnessDecision: EvidenceAuditFreshnessFresh,
		}
		if release.Role == EvidenceAuditReleaseSupporting {
			supportingRefs = append(supportingRefs, ref)
		} else {
			primaryRefs = append(primaryRefs, ref)
		}
	}
	if len(primaryRefs) == 0 {
		t.Fatal("evaluation fixture has no primary evidence")
	}
	var evidence []EvidenceAuditEvidenceRef
	if verdict == EvidenceAuditVerdictInsufficient {
		evidence = []EvidenceAuditEvidenceRef{primaryRefs[0]}
	} else {
		required := pkg.EvidencePolicy.MinimumIndependentSources
		if required > len(supportingRefs) {
			t.Fatalf("evaluation fixture has %d supporting sources, requires %d", len(supportingRefs), required)
		}
		evidence = append([]EvidenceAuditEvidenceRef(nil), supportingRefs[:required]...)
		if len(evidence) < pkg.EvidencePolicy.MaxEvidencePerClaim {
			evidence = append(evidence, primaryRefs[0])
		}
	}
	if conflict {
		evidence[0].Conflict = true
	}
	conflicts := 0
	if conflict {
		conflicts = 1
	}
	report := EvidenceAudit{
		ClaimAudits: []EvidenceAuditClaim{{
			SourceClaim: sourceClaim, NormalizedStatement: "Synthetic normalized statement.",
			Verdict: verdict, Evidence: evidence,
			ComputedConfidence: ComputeEvidenceAuditConfidence(evidence, conflicts),
			Limitations:        []string{"Bounded fixture."}, KnowledgeGaps: []string{"External validation missing."},
			ReviewActions: []string{"Review source applicability."},
		}},
		Summary: EvidenceAuditSummary{
			Conclusion:    "Deterministic evidence audit conclusion.",
			VerdictCounts: map[string]int{verdict: 1}, Limitations: []string{"Fixture only."},
		},
		Proofroom: EvidenceAuditProofroomProjection{
			SchemaVersion: "proofroom-evidence-task.v1",
			Title:         "Review evidence audit", ReviewItems: []string{"Verify evidence applicability."},
		},
		AuditID: queued.AuditID, TraceID: traceID,
	}
	if verdict == EvidenceAuditVerdictInsufficient {
		report.Summary.Conclusion = "The bounded evidence is insufficient for adjudication."
	}
	completed, err := completeEvidenceAuditEvaluationForTest(
		t, store, pkg, report, now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	return *completed
}

func completeEvidenceAuditEvaluationForTest(
	t *testing.T,
	store *BookKnowledgeStore,
	pkg AgentPackage,
	report EvidenceAudit,
	now time.Time,
) (*EvidenceAudit, error) {
	t.Helper()
	prepared, err := PrepareEvidenceAuditCompletion(store, report, now)
	if err != nil {
		return nil, err
	}
	retrieved := make([]evidenceAuditRetrievedItem, 0)
	for _, claim := range prepared.ClaimAudits {
		for _, ref := range claim.Evidence {
			retrieved = append(retrieved, evidenceAuditRetrievedItem{
				Evidence: AgentPackageEvidence{
					ReleaseID: ref.ReleaseID, ClaimID: ref.ClaimID,
					Statement: "bounded evaluation evidence", CitationIDs: []string{ref.CitationID}, Score: 1,
				},
				Ref: ref,
			})
		}
	}
	fingerprint, err := evidenceAuditReportFingerprint(*prepared)
	if err != nil {
		return nil, err
	}
	trace, err := buildEvidenceAuditTrace(
		store, *prepared, pkg, retrieved, prepared.ClaimAudits,
		prepared.TraceID, AgentTraceOutcomeCompleted, fingerprint,
		EvidenceAuditRunnerConfig{Now: func() time.Time { return now }},
	)
	if err != nil {
		return nil, err
	}
	terminal := evidenceAuditTraceTerminal{
		Version: evidenceAuditTraceTerminalVersion,
		AuditID: prepared.AuditID, InputHash: prepared.InputHash,
		TraceID: prepared.TraceID, ReportFingerprint: fingerprint,
		Report: prepared, Trace: trace,
	}
	if err := store.prepareEvidenceAuditTraceTerminal(terminal); err != nil {
		return nil, err
	}
	if err := store.finalizeEvidenceAuditTraceTerminal(terminal); err != nil {
		return nil, err
	}
	completed, err := PublishEvidenceAuditCompletion(store, prepared.AuditID)
	if err != nil {
		return nil, err
	}
	if err := store.removeEvidenceAuditTraceTerminal(prepared.AuditID); err != nil {
		return nil, err
	}
	return completed, nil
}

func evidenceAuditEvaluationPackage(t *testing.T) AgentPackage {
	t.Helper()
	_, pkg := evidenceAuditEvaluationStore(t)
	return pkg
}

func evidenceAuditEvaluationSuite(
	pkg AgentPackage,
	supported EvidenceAudit,
	conflicted EvidenceAudit,
	insufficient EvidenceAudit,
) AgentEvaluationSuite {
	supportedIdentity := evidenceAuditClaimIdentity(supported.ClaimAudits[0].SourceClaim)
	conflictedIdentity := evidenceAuditClaimIdentity(conflicted.ClaimAudits[0].SourceClaim)
	conflict := true
	return AgentEvaluationSuite{
		SchemaVersion: AgentEvaluationSchemaVersion,
		SuiteVersion:  pkg.EvaluationPolicy.SuiteVersion,
		Cases: []AgentEvaluationCase{
			{
				CaseID: "audit-adjudication", Metric: "adjudication_consistency", AuditID: supported.AuditID,
				ExpectedClaims: []AgentEvaluationExpectedClaim{{
					ClaimIdentity: supportedIdentity, Verdict: EvidenceAuditVerdictSupported,
				}},
			},
			{CaseID: "audit-independence", Metric: "source_independence", AuditID: supported.AuditID},
			{
				CaseID: "audit-conflicts", Metric: "conflict_detection", AuditID: conflicted.AuditID,
				ExpectedClaims: []AgentEvaluationExpectedClaim{{
					ClaimIdentity: conflictedIdentity, Verdict: EvidenceAuditVerdictMixed, Conflict: &conflict,
				}},
			},
			{CaseID: "audit-citations", Metric: "report_citation_completeness", AuditID: supported.AuditID},
			{CaseID: "audit-insufficient", Metric: "safe_insufficiency", AuditID: insufficient.AuditID},
			{CaseID: "audit-proofroom", Metric: "proofroom_projection_completeness", AuditID: supported.AuditID},
		},
	}
}
