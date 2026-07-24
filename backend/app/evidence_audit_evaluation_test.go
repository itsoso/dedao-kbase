package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var evidenceAuditEvaluationMetrics = []string{
	"adjudication_consistency",
	"source_independence",
	"conflict_detection",
	"report_citation_completeness",
	"safe_insufficiency",
	"proofroom_projection_completeness",
}

func TestEvidenceAuditEvaluationScoresAllRequiredMetrics(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := evidenceAuditEvaluationPackage(t)
	suite := evidenceAuditEvaluationSuite(t, pkg)

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

func TestEvidenceAuditEvaluationRejectsSpecificQualityRegressions(t *testing.T) {
	tests := []struct {
		metric         string
		edit           func(*EvidenceAudit)
		validAfterEdit bool
	}{
		{
			metric: "adjudication_consistency",
			edit: func(audit *EvidenceAudit) {
				audit.ClaimAudits[0].Verdict = EvidenceAuditVerdictContradicted
				audit.Summary.VerdictCounts = map[string]int{EvidenceAuditVerdictContradicted: 1}
			},
			validAfterEdit: true,
		},
		{
			metric: "source_independence",
			edit: func(audit *EvidenceAudit) {
				current := append([]EvidenceAuditEvidenceRef(nil), audit.ClaimAudits[0].Evidence...)
				audit.ClaimAudits[0].Evidence = nil
				for _, evidence := range current {
					if evidence.Role == EvidenceAuditReleasePrimary {
						audit.ClaimAudits[0].Evidence = append(audit.ClaimAudits[0].Evidence, evidence)
						break
					}
				}
			},
		},
		{
			metric: "conflict_detection",
			edit: func(audit *EvidenceAudit) {
				audit.ClaimAudits[0].Verdict = EvidenceAuditVerdictSupported
				audit.Summary.VerdictCounts = map[string]int{EvidenceAuditVerdictSupported: 1}
				audit.ClaimAudits[0].ComputedConfidence = ComputeEvidenceAuditConfidence(
					audit.ClaimAudits[0].Evidence,
					1,
				)
			},
			validAfterEdit: true,
		},
		{
			metric: "report_citation_completeness",
			edit: func(audit *EvidenceAudit) {
				audit.ClaimAudits[0].Evidence[0].CitationID = "citation-unpinned"
			},
		},
		{
			metric: "safe_insufficiency",
			edit: func(audit *EvidenceAudit) {
				audit.ClaimAudits[0].KnowledgeGaps = nil
			},
			validAfterEdit: true,
		},
		{
			metric: "proofroom_projection_completeness",
			edit: func(audit *EvidenceAudit) {
				audit.Proofroom.ReviewItems = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.metric, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			pkg := evidenceAuditEvaluationPackage(t)
			suite := evidenceAuditEvaluationSuite(t, pkg)
			for index := range suite.Cases {
				if suite.Cases[index].Metric != tt.metric {
					continue
				}
				tt.edit(suite.Cases[index].EvidenceAudit)
				if tt.validAfterEdit {
					finalized, err := FinalizeEvidenceAuditReport(*suite.Cases[index].EvidenceAudit)
					if err != nil {
						t.Fatalf("FinalizeEvidenceAuditReport() error = %v", err)
					}
					suite.Cases[index].EvidenceAudit = &finalized
				} else {
					suite.Cases[index].EvidenceAudit.OutputHash = ""
					hash, err := EvidenceAuditOutputHash(*suite.Cases[index].EvidenceAudit)
					if err != nil {
						t.Fatal(err)
					}
					suite.Cases[index].EvidenceAudit.OutputHash = hash
				}
			}

			report, err := EvaluateAgentPackageDeterministically(store, pkg, suite, testAgentPackageTime())
			if err != nil {
				t.Fatalf("EvaluateAgentPackageDeterministically() error = %v", err)
			}
			if report.Metrics[tt.metric] != 0 || report.Passed {
				t.Fatalf("regression metric %q passed: %#v", tt.metric, report)
			}
		})
	}
}

func TestEvidenceAuditEvaluationCasePointerPreservesV1SuiteJSONAndHash(t *testing.T) {
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	suite := loadAgentEvaluationFixture(t)
	beforeJSON, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash, err := agentEvaluationInputHash(pkg.ContentHash, suite)
	if err != nil {
		t.Fatal(err)
	}

	var decoded AgentEvaluationSuite
	if err := json.Unmarshal(beforeJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	afterJSON, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	afterHash, err := agentEvaluationInputHash(pkg.ContentHash, decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeJSON, afterJSON) || beforeHash != afterHash {
		t.Fatalf("v1 suite JSON/hash changed: json=%s hash=%q want=%q", afterJSON, afterHash, beforeHash)
	}
	for _, evalCase := range decoded.Cases {
		if evalCase.EvidenceAudit != nil {
			t.Fatalf("v1 suite unexpectedly contains EvidenceAudit: %#v", evalCase)
		}
	}
}

func TestEvidenceAuditContractSchemasCoverRequiredEnumsAndLimits(t *testing.T) {
	tests := []struct {
		name      string
		required  []string
		fragments []string
	}{
		{
			name: "agent-package-v2.schema.json",
			required: []string{
				"schema_version", "package_id", "version", "content_hash", "lifecycle_state",
				"releases", "retrieval_policy", "model_policy", "prompt_profiles", "tool_policy",
				"safety_policy", "evaluation_policy", "evidence_policy", "ui_manifest",
			},
			fragments: []string{
				`"const":"agent-package.v2"`,
				`"report_schema":{"const":"evidence-audit.v1"}`,
				`"max_claims":{"type":"integer","minimum":1,"maximum":8}`,
				`"max_evidence_per_claim":{"type":"integer","minimum":1,"maximum":5}`,
				`"proofroom_projection_completeness"`,
			},
		},
		{
			name: "evidence-audit-v1.schema.json",
			required: []string{
				"schema_version", "audit_id", "status", "created_at", "updated_at",
				"idempotency_key", "input_hash", "package", "evidence_policy", "model",
				"retrieval", "releases", "subject", "scope", "selected_claims",
			},
			fragments: []string{
				`"const":"evidence-audit.v1"`,
				`"enum":["queued","running","completed","failed"]`,
				`"enum":["supported","contradicted","mixed","insufficient"]`,
				`"selected_claims":{"type":"array","minItems":1,"maxItems":8`,
				`"evidence_refs":{"type":"array","maxItems":5`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", tt.name))
			if err != nil {
				t.Fatal(err)
			}
			var schema map[string]any
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("schema is not valid JSON: %v", err)
			}
			required, ok := schema["required"].([]any)
			if !ok {
				t.Fatal("schema required is missing")
			}
			joined := strings.Join(anyStrings(required), ",")
			for _, field := range tt.required {
				if !strings.Contains(joined, field) {
					t.Fatalf("schema does not require %q: %s", field, joined)
				}
			}
			compact := strings.Join(strings.Fields(string(raw)), "")
			for _, fragment := range tt.fragments {
				if !strings.Contains(compact, fragment) {
					t.Fatalf("schema does not contain %s", fragment)
				}
			}
		})
	}
}

func evidenceAuditEvaluationPackage(t *testing.T) AgentPackage {
	t.Helper()
	pkg := validAgentPackageV2()
	pkg.EvaluationPolicy.SuiteVersion = "clinical-evidence-audit-v1"
	pkg.EvaluationPolicy.MinimumScores = make(map[string]float64, len(evidenceAuditEvaluationMetrics))
	for _, metric := range evidenceAuditEvaluationMetrics {
		pkg.EvaluationPolicy.MinimumScores[metric] = 1
	}
	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return finalized
}

func evidenceAuditEvaluationSuite(t *testing.T, pkg AgentPackage) AgentEvaluationSuite {
	t.Helper()
	supported := evidenceAuditEvaluationReport(t, pkg, EvidenceAuditVerdictSupported, false)
	conflicted := evidenceAuditEvaluationReport(t, pkg, EvidenceAuditVerdictMixed, true)
	insufficient := evidenceAuditEvaluationReport(t, pkg, EvidenceAuditVerdictInsufficient, false)
	return AgentEvaluationSuite{
		SchemaVersion: AgentEvaluationSchemaVersion,
		SuiteVersion:  pkg.EvaluationPolicy.SuiteVersion,
		Cases: []AgentEvaluationCase{
			{CaseID: "audit-adjudication", Metric: "adjudication_consistency", ExpectedValue: EvidenceAuditVerdictSupported, EvidenceAudit: &supported},
			{CaseID: "audit-independence", Metric: "source_independence", EvidenceAudit: &supported},
			{CaseID: "audit-conflicts", Metric: "conflict_detection", ExpectedValue: "detected", EvidenceAudit: &conflicted},
			{CaseID: "audit-citations", Metric: "report_citation_completeness", EvidenceAudit: &supported},
			{CaseID: "audit-insufficient", Metric: "safe_insufficiency", ExpectedValue: EvidenceAuditVerdictInsufficient, EvidenceAudit: &insufficient},
			{CaseID: "audit-proofroom", Metric: "proofroom_projection_completeness", EvidenceAudit: &supported},
		},
	}
}

func evidenceAuditEvaluationReport(t *testing.T, pkg AgentPackage, verdict string, conflict bool) EvidenceAudit {
	t.Helper()
	roles := make(map[string]string, len(pkg.Releases))
	for _, releaseRole := range pkg.EvidencePolicy.ReleaseRoles {
		roles[releaseRole.ReleaseID] = releaseRole.Role
	}
	releases := make([]EvidenceAuditReleaseRef, 0, len(pkg.Releases))
	var primaryEvidence []EvidenceAuditEvidenceRef
	var supportingEvidence []EvidenceAuditEvidenceRef
	for _, release := range pkg.Releases {
		role := roles[release.ReleaseID]
		sourceType := "wechat_mp_article"
		if role == EvidenceAuditReleasePrimary {
			sourceType = "dedao_ebook"
		}
		publicationIdentity := sha256Fingerprint([]byte("synthetic-publication:" + release.ReleaseID))
		citations := make([]EvidenceAuditCitationRef, 0, len(release.CitationIDs))
		for citationIndex, citationID := range release.CitationIDs {
			citations = append(citations, EvidenceAuditCitationRef{
				CitationID: citationID,
				ClaimID:    "synthetic-claim-" + release.ReleaseID,
				ChunkID:    "synthetic-chunk-" + release.ReleaseID + "-" + string(rune('a'+citationIndex)),
			})
		}
		releases = append(releases, EvidenceAuditReleaseRef{
			ReleaseID:           release.ReleaseID,
			ContentHash:         release.ContentHash,
			Role:                role,
			SourceType:          sourceType,
			PublicationIdentity: publicationIdentity,
			Citations:           citations,
		})
		if len(citations) > 0 {
			citation := citations[0]
			ref := EvidenceAuditEvidenceRef{
				ReleaseID:           release.ReleaseID,
				ContentHash:         release.ContentHash,
				Role:                role,
				SourceType:          sourceType,
				PublicationIdentity: publicationIdentity,
				ClaimID:             citation.ClaimID,
				ChunkID:             citation.ChunkID,
				CitationID:          citation.CitationID,
				PublishedAt:         "2026-07-20T00:00:00Z",
				FreshnessDecision:   EvidenceAuditFreshnessFresh,
			}
			if role == EvidenceAuditReleaseSupporting {
				supportingEvidence = append(supportingEvidence, ref)
			} else {
				primaryEvidence = append(primaryEvidence, ref)
			}
		}
	}
	evidence := make([]EvidenceAuditEvidenceRef, 0, pkg.EvidencePolicy.MaxEvidencePerClaim)
	if verdict == EvidenceAuditVerdictInsufficient {
		if len(primaryEvidence) > 0 && pkg.EvidencePolicy.MaxEvidencePerClaim > 0 {
			evidence = append(evidence, primaryEvidence[0])
		}
	} else {
		for _, ref := range supportingEvidence {
			if len(evidence) >= pkg.EvidencePolicy.MinimumIndependentSources ||
				len(evidence) >= pkg.EvidencePolicy.MaxEvidencePerClaim {
				break
			}
			evidence = append(evidence, ref)
		}
		if len(evidence) < pkg.EvidencePolicy.MaxEvidencePerClaim && len(primaryEvidence) > 0 {
			evidence = append(evidence, primaryEvidence[0])
		}
	}
	if conflict && len(evidence) > 0 {
		evidence[0].Conflict = true
	}
	conflicts := 0
	if conflict {
		conflicts = 1
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
		Model:          EvidenceAuditModelIdentity{Provider: "tokenplan", Model: "synthetic-model", Route: "evidence-audit"},
		Retrieval:      EvidenceAuditRetrievalIdentity{Strategy: pkg.RetrievalPolicy.Strategy, IndexVersion: "synthetic-index-v1"},
		Releases:       releases,
		Subject:        "Synthetic clinical evidence audit",
		Scope:          "Deterministic contract evaluation only.",
		SelectedClaims: []string{"synthetic source claim"},
	}
	inputHash, err := EvidenceAuditInputHash(input)
	if err != nil {
		t.Fatal(err)
	}
	audit := EvidenceAudit{
		SchemaVersion:  EvidenceAuditSchemaVersion,
		AuditID:        "synthetic-audit-" + verdict,
		Status:         EvidenceAuditCompleted,
		CreatedAt:      "2026-07-23T10:00:00Z",
		UpdatedAt:      "2026-07-23T10:02:00Z",
		StartedAt:      "2026-07-23T10:01:00Z",
		CompletedAt:    "2026-07-23T10:02:00Z",
		IdempotencyKey: "synthetic-evaluation-" + verdict,
		InputHash:      inputHash,
		Package:        input.Package,
		EvidencePolicy: input.EvidencePolicy,
		Model:          input.Model,
		Retrieval:      input.Retrieval,
		Releases:       input.Releases,
		Subject:        input.Subject,
		Scope:          input.Scope,
		SelectedClaims: input.SelectedClaims,
		ClaimAudits: []EvidenceAuditClaim{{
			SourceClaim:         input.SelectedClaims[0],
			NormalizedStatement: "Synthetic normalized statement.",
			Verdict:             verdict,
			Evidence:            evidence,
			ComputedConfidence:  ComputeEvidenceAuditConfidence(evidence, conflicts),
			Limitations:         []string{"Synthetic evidence is bounded."},
			KnowledgeGaps:       []string{"External validation is not included."},
			ReviewActions:       []string{"Request independent review."},
		}},
		Summary: EvidenceAuditSummary{
			Conclusion:    "Synthetic evidence audit conclusion.",
			VerdictCounts: map[string]int{verdict: 1},
			Limitations:   []string{"This fixture contains no source body."},
		},
		Proofroom: EvidenceAuditProofroomProjection{
			SchemaVersion: "proofroom-evidence-task.v1",
			Title:         "Review synthetic evidence audit",
			ReviewItems:   []string{"Verify citation applicability."},
		},
		TraceID: "synthetic-trace-" + verdict,
	}
	if verdict == EvidenceAuditVerdictInsufficient {
		audit.Summary.Conclusion = "The bounded synthetic evidence is insufficient for adjudication."
	}
	finalized, err := FinalizeEvidenceAuditReport(audit)
	if err != nil {
		t.Fatal(err)
	}
	return finalized
}
