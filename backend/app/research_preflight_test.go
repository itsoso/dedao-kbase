package app

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func TestValidateResearchPreflightRequestNormalizesBoundedScope(t *testing.T) {
	request, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
		Mode:              " auto ",
		Question:          "  compare evidence  ",
		RequestedSources:  []string{"knowledge", "chatlog"},
		PackageConstraint: "  collection-agent  ",
		ParentRunID:       "  research-run-parent  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Question != "compare evidence" || request.Mode != ResearchModeAuto {
		t.Fatalf("normalized request = %#v", request)
	}
	if request.PackageConstraint != "collection-agent" || request.ParentRunID != "research-run-parent" {
		t.Fatalf("normalized optional scope = %#v", request)
	}
}

func TestValidateResearchPreflightRequestUsesStableSourceOrdering(t *testing.T) {
	request, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
		Question:         "compare evidence",
		RequestedSources: []string{" prior_runs ", "chatlog", " knowledge "},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{ResearchSourceKnowledge, ResearchSourceChatlog, ResearchSourcePriorRuns}
	if len(request.RequestedSources) != len(want) {
		t.Fatalf("requested_sources = %#v", request.RequestedSources)
	}
	for index := range want {
		if request.RequestedSources[index] != want[index] {
			t.Fatalf("requested_sources = %#v, want %#v", request.RequestedSources, want)
		}
	}
}

func TestValidateResearchPreflightRequestRejectsHardBounds(t *testing.T) {
	tests := []struct {
		name    string
		request ResearchPreflightRequest
		want    string
	}{
		{
			name:    "question required",
			request: ResearchPreflightRequest{Question: "  "},
			want:    "question is required",
		},
		{
			name:    "question bounded",
			request: ResearchPreflightRequest{Question: strings.Repeat("q", researchQuestionMaxRunes+1)},
			want:    "question exceeds",
		},
		{
			name: "sources bounded",
			request: ResearchPreflightRequest{
				Question:         "compare evidence",
				RequestedSources: make([]string, researchRequestedSourcesMax+1),
			},
			want: "requested_sources exceeds",
		},
		{
			name: "duplicate source rejected",
			request: ResearchPreflightRequest{
				Question:         "compare evidence",
				RequestedSources: []string{"knowledge", " knowledge "},
			},
			want: "duplicate requested source",
		},
		{
			name: "unknown source rejected",
			request: ResearchPreflightRequest{
				Question:         "compare evidence",
				RequestedSources: []string{"global"},
			},
			want: "unsupported requested source",
		},
		{
			name:    "constraint bounded",
			request: ResearchPreflightRequest{Question: "compare evidence", PackageConstraint: strings.Repeat("p", researchPackageIDMaxRunes+1)},
			want:    "package_constraint exceeds",
		},
		{
			name:    "mode allowlisted",
			request: ResearchPreflightRequest{Mode: "wide", Question: "compare evidence"},
			want:    "mode must be auto, quick, or deep",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeResearchPreflightRequest(test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateResearchPreflightRequestEnforcesParentRunIDBoundary(t *testing.T) {
	t.Run("exact maximum allowed", func(t *testing.T) {
		parentRunID := strings.Repeat("r", researchPackageIDMaxRunes)
		request, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
			Question:    "compare evidence",
			ParentRunID: parentRunID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if request.ParentRunID != parentRunID {
			t.Fatalf("parent_run_id length = %d", len([]rune(request.ParentRunID)))
		}
	})

	t.Run("over maximum rejected", func(t *testing.T) {
		_, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
			Question:    "compare evidence",
			ParentRunID: strings.Repeat("r", researchPackageIDMaxRunes+1),
		})
		if err == nil || !strings.Contains(err.Error(), "parent_run_id exceeds") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestValidateResearchPreflightRejectsMoreThanThreeCandidates(t *testing.T) {
	preflight := testResearchPreflight()
	preflight.Candidates = append(preflight.Candidates,
		ResearchPreflightCandidate{PackageID: "candidate-b", PackageVersion: "1.0.0", MatchLevel: ResearchPreflightMatchMedium},
		ResearchPreflightCandidate{PackageID: "candidate-c", PackageVersion: "1.0.0", MatchLevel: ResearchPreflightMatchLow},
		ResearchPreflightCandidate{PackageID: "candidate-d", PackageVersion: "1.0.0", MatchLevel: ResearchPreflightMatchLow},
	)
	if err := ValidateResearchPreflight(preflight); err == nil || !strings.Contains(err.Error(), "candidates exceeds 3 items") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateResearchPreflightRejectsUnknownDecisionStates(t *testing.T) {
	preflight := testResearchPreflight()
	preflight.Candidates[0].MatchLevel = "exact"
	if err := ValidateResearchPreflight(preflight); err == nil || !strings.Contains(err.Error(), "unsupported match level") {
		t.Fatalf("match level error = %v", err)
	}

	preflight = testResearchPreflight()
	preflight.Checks[0].Status = "unknown"
	if err := ValidateResearchPreflight(preflight); err == nil || !strings.Contains(err.Error(), "unsupported check status") {
		t.Fatalf("check status error = %v", err)
	}
}

func TestValidateResearchPreflightEnforcesCandidateAndReadyConsistency(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ResearchPreflight)
		want   string
	}{
		{
			name: "candidate package id required",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates[0].PackageID = "  "
			},
			want: "candidate 0 package_id is required",
		},
		{
			name: "candidate package version required",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates[0].PackageVersion = ""
			},
			want: "candidate 0 package_version is required",
		},
		{
			name: "candidate content hash required",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates[0].ContentHash = ""
			},
			want: "candidate 0 content_hash is required",
		},
		{
			name: "ready requires candidate",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates = nil
			},
			want: "ready preflight requires a confirmable candidate",
		},
		{
			name: "ready rejects blocked global check",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Checks[0].Status = ResearchPreflightCheckBlocked
			},
			want: "ready preflight cannot contain blocked check",
		},
		{
			name: "ready rejects all candidates blocked",
			mutate: func(preflight *ResearchPreflight) {
				preflight.Candidates[0].Readiness = ResearchPreflightCheckBlocked
			},
			want: "ready preflight cannot have all candidates blocked",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preflight := testResearchPreflight()
			test.mutate(&preflight)
			if err := ValidateResearchPreflight(preflight); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateResearchPreflightAllowsApprovedBlockedAndWarningShapes(t *testing.T) {
	t.Run("ready warning candidate", func(t *testing.T) {
		preflight := testResearchPreflight()
		preflight.Candidates[0].Readiness = ResearchPreflightCheckWarning
		if err := ValidateResearchPreflight(preflight); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("blocked without candidates", func(t *testing.T) {
		preflight := testResearchPreflight()
		preflight.Status = ResearchPreflightStatusBlocked
		preflight.Candidates = nil
		if err := ValidateResearchPreflight(preflight); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("blocked with identified candidate", func(t *testing.T) {
		preflight := testResearchPreflight()
		preflight.Status = ResearchPreflightStatusBlocked
		preflight.Candidates[0].Readiness = ResearchPreflightCheckBlocked
		if err := ValidateResearchPreflight(preflight); err != nil {
			t.Fatal(err)
		}
	})
}

func TestResearchPreflightProjectionContainsNoPrivateBodies(t *testing.T) {
	encoded, err := json.Marshal(PublicResearchPreflight(testResearchPreflight()))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"content_excerpt", "message_ref", "local_path", "identity_id",
		"request_hash", "content_hash", "private-request-hash", "private-content-hash",
	} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("projection leaks %s", forbidden)
		}
	}
}

func TestResearchPreflightInternalProjectionPreservesContentHash(t *testing.T) {
	encoded, err := json.Marshal(testResearchPreflight())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"content_hash":"private-content-hash"`)) {
		t.Fatalf("internal projection lost content hash: %s", encoded)
	}
}

func TestResearchPreflightEligibility(t *testing.T) {
	request := ResearchPreflightRequest{
		Mode:             ResearchModeDeep,
		Question:         "compare evidence",
		RequestedSources: []string{ResearchSourceKnowledge, ResearchSourceChatlog},
	}

	tests := []struct {
		name   string
		mutate func(*ResearchPreflightPackageFacts)
		want   int
	}{
		{name: "published v4 research package with trusted evaluation", want: 1},
		{name: "draft package rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.Package.LifecycleState = AgentPackageDraft
		}},
		{name: "legacy schema rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.Package.SchemaVersion = AgentPackageSchemaVersionV3
			facts.Package.ResearchPolicy = nil
		}},
		{name: "missing research policy rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.Package.ResearchPolicy = nil
		}},
		{name: "untrusted evaluation rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.EvaluationPassed = false
		}},
		{name: "package without complete runtime validation rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.RunnablePackageValidated = false
		}},
		{name: "mode outside policy rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.Package.ResearchPolicy.Modes = []string{ResearchModeQuick}
		}},
		{name: "source outside policy rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.Package.ResearchPolicy.AllowedSources = []string{ResearchSourceKnowledge}
		}},
		{name: "missing search capability rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.Package.UIManifest.Capabilities = []string{"reader", "deep_research"}
		}},
		{name: "missing deep research capability rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.Package.UIManifest.Capabilities = []string{"reader", "search"}
		}},
		{name: "invalid research tool policy rejected", mutate: func(facts *ResearchPreflightPackageFacts) {
			for index := range facts.Package.ToolPolicy.Tools {
				if facts.Package.ToolPolicy.Tools[index].MCPServer == "research" {
					facts.Package.ToolPolicy.Tools[index].Decision = AgentToolBlock
					return
				}
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := testResearchPreflightPackageFacts(t, "eligible-agent")
			if test.mutate != nil {
				test.mutate(&facts)
			}
			candidates, _ := RankResearchPreflightCandidates(request, []ResearchPreflightPackageFacts{facts})
			if len(candidates) != test.want {
				t.Fatalf("candidate count = %d, want %d: %#v", len(candidates), test.want, candidates)
			}
		})
	}
}

func TestResearchPreflightEligibilityExplicitConstraintNeverWidens(t *testing.T) {
	eligible := testResearchPreflightPackageFacts(t, "eligible-agent")
	ineligible := testResearchPreflightPackageFacts(t, "constrained-agent")
	ineligible.Package.LifecycleState = AgentPackageDraft

	candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeAuto, Question: "compare evidence", PackageConstraint: "constrained-agent",
	}, []ResearchPreflightPackageFacts{eligible, ineligible})
	if len(candidates) != 0 {
		t.Fatalf("explicit constraint widened eligibility: %#v", candidates)
	}

	candidates, _ = RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeAuto, Question: "compare evidence", PackageConstraint: "eligible-agent",
	}, []ResearchPreflightPackageFacts{eligible, testResearchPreflightPackageFacts(t, "other-agent")})
	if len(candidates) != 1 || candidates[0].PackageID != "eligible-agent" {
		t.Fatalf("explicit constraint did not narrow candidates: %#v", candidates)
	}
}

func TestResearchPreflightEligibilityRejectsPackageChangedAfterValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AgentPackage)
	}{
		{name: "version changed", mutate: func(pkg *AgentPackage) { pkg.Version = "4.0.0" }},
		{name: "content changed", mutate: func(pkg *AgentPackage) { pkg.ModelPolicy.TimeoutMS++ }},
		{name: "hash changed", mutate: func(pkg *AgentPackage) { pkg.ContentHash = "sha256:changed" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := testResearchPreflightPackageFacts(t, "changed-agent")
			test.mutate(&facts.Package)
			candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
				Mode: ResearchModeAuto, Question: "compare evidence",
			}, []ResearchPreflightPackageFacts{facts})
			if len(candidates) != 0 {
				t.Fatalf("package changed after validation remained eligible: %#v", candidates)
			}
		})
	}
}

func TestRankResearchPreflightCandidatesEvidenceCoverageOutranksMetadata(t *testing.T) {
	metadata := testResearchPreflightPackageFacts(t, "metadata-agent")
	metadata.TopicHits = 20
	metadata.EvidenceHits = 0
	evidence := testResearchPreflightPackageFacts(t, "evidence-agent")
	evidence.TopicHits = 0
	evidence.EvidenceHits = 1

	candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeAuto, Question: "compare evidence",
	}, []ResearchPreflightPackageFacts{metadata, evidence})
	if len(candidates) != 2 || candidates[0].PackageID != "evidence-agent" {
		t.Fatalf("ranked candidates = %#v", candidates)
	}
	if candidates[0].MatchLevel != ResearchPreflightMatchHigh || candidates[1].MatchLevel != ResearchPreflightMatchMedium {
		t.Fatalf("match levels = %#v", candidates)
	}
}

func TestRankResearchPreflightCandidatesReadinessDoesNotChangeEligibility(t *testing.T) {
	online := testResearchPreflightPackageFacts(t, "online-agent")
	online.WorkerState = SourceAgentObservedOnline
	degraded := testResearchPreflightPackageFacts(t, "degraded-agent")
	degraded.WorkerState = SourceAgentObservedDegraded
	offline := testResearchPreflightPackageFacts(t, "offline-agent")
	offline.WorkerState = SourceAgentObservedOffline
	budgetBlocked := testResearchPreflightPackageFacts(t, "budget-agent")
	budgetBlocked.BudgetFits = false

	candidates, gaps := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeDeep, Question: "compare evidence", RequestedSources: []string{ResearchSourceChatlog},
	}, []ResearchPreflightPackageFacts{offline, online, budgetBlocked, degraded})
	if len(candidates) != 3 {
		t.Fatalf("readiness filtered policy-eligible candidates: %#v", candidates)
	}
	readiness := map[string]string{}
	reasons := map[string][]string{}
	for _, candidate := range candidates {
		readiness[candidate.PackageID] = candidate.Readiness
		reasons[candidate.PackageID] = candidate.ReasonCodes
	}
	if readiness["online-agent"] != ResearchPreflightCheckPass ||
		readiness["degraded-agent"] != ResearchPreflightCheckWarning ||
		readiness["budget-agent"] != ResearchPreflightCheckBlocked {
		t.Fatalf("candidate readiness = %#v", readiness)
	}
	if !containsResearchString(reasons["online-agent"], "worker_ready") || containsResearchString(reasons["degraded-agent"], "worker_ready") {
		t.Fatalf("worker reasons = %#v", reasons)
	}
	if !researchPreflightGapCodes(gaps)["worker_offline"] || !researchPreflightGapCodes(gaps)["budget_insufficient"] {
		t.Fatalf("readiness gaps = %#v", gaps)
	}

	knowledgeOnly := offline
	knowledgeOnly.Package.PackageID = "knowledge-agent"
	knowledgeOnly = finalizeResearchPreflightPackageFacts(t, knowledgeOnly)
	candidates, _ = RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeAuto, Question: "compare evidence", RequestedSources: []string{ResearchSourceKnowledge},
	}, []ResearchPreflightPackageFacts{knowledgeOnly})
	if len(candidates) != 1 || candidates[0].Readiness != ResearchPreflightCheckPass {
		t.Fatalf("knowledge-only readiness depended on Worker: %#v", candidates)
	}
}

func TestRankResearchPreflightCandidatesUsesStableTieBreakAndLimit(t *testing.T) {
	alphaV2 := testResearchPreflightPackageFacts(t, "alpha-agent")
	alphaV2.Package.Version = "2.0.0"
	alphaV2 = finalizeResearchPreflightPackageFacts(t, alphaV2)
	alphaV1HashB := testResearchPreflightPackageFacts(t, "Alpha-Agent")
	alphaV1HashB.Package.Version = "1.0.0"
	alphaV1HashB.Package.ModelPolicy.TimeoutMS++
	alphaV1HashB = finalizeResearchPreflightPackageFacts(t, alphaV1HashB)
	alphaV1HashA := testResearchPreflightPackageFacts(t, "Alpha-Agent")
	alphaV1HashA.Package.Version = "1.0.0"
	alphaV1HashA.Package.ModelPolicy.TimeoutMS += 2
	alphaV1HashA = finalizeResearchPreflightPackageFacts(t, alphaV1HashA)
	beta := testResearchPreflightPackageFacts(t, "beta-agent")
	gamma := testResearchPreflightPackageFacts(t, "gamma-agent")

	facts := []ResearchPreflightPackageFacts{gamma, alphaV2, beta, alphaV1HashB, alphaV1HashA}
	request := ResearchPreflightRequest{Mode: ResearchModeAuto, Question: "compare evidence"}
	first, _ := RankResearchPreflightCandidates(request, facts)
	second, _ := RankResearchPreflightCandidates(request, []ResearchPreflightPackageFacts{beta, alphaV1HashA, gamma, alphaV1HashB, alphaV2})
	if len(first) != researchPreflightCandidateMax || len(second) != researchPreflightCandidateMax {
		t.Fatalf("candidate limit = %d and %d", len(first), len(second))
	}
	for index := range first {
		if first[index].PackageID != second[index].PackageID || first[index].PackageVersion != second[index].PackageVersion || first[index].ContentHash != second[index].ContentHash {
			t.Fatalf("unstable ordering: %#v versus %#v", first, second)
		}
	}
	alphaV1Hashes := []string{alphaV1HashA.Package.ContentHash, alphaV1HashB.Package.ContentHash}
	sort.Strings(alphaV1Hashes)
	want := []struct{ id, version, hash string }{
		{"Alpha-Agent", "1.0.0", alphaV1Hashes[0]},
		{"Alpha-Agent", "1.0.0", alphaV1Hashes[1]},
		{"alpha-agent", "2.0.0", alphaV2.Package.ContentHash},
	}
	for index, expected := range want {
		if first[index].PackageID != expected.id || first[index].PackageVersion != expected.version || first[index].ContentHash != expected.hash {
			t.Fatalf("candidate %d = %#v, want %#v", index, first[index], expected)
		}
	}
}

func TestRankResearchPreflightCandidatesUsesStableReasonCodesOnly(t *testing.T) {
	facts := testResearchPreflightPackageFacts(t, "reason-agent")
	facts.TopicHits = 2
	facts.EvidenceHits = 3
	facts.LatestPublishedAt = "2026-08-18T12:00:00Z"
	facts.WorkerState = SourceAgentObservedOnline

	candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeAuto, Question: "compare evidence", RequestedSources: []string{ResearchSourceChatlog},
	}, []ResearchPreflightPackageFacts{facts})
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	want := []string{"evidence_coverage", "topic_match", "fresh_release", "trusted_evaluation", "worker_ready"}
	if strings.Join(candidates[0].ReasonCodes, ",") != strings.Join(want, ",") {
		t.Fatalf("reason codes = %#v, want %#v", candidates[0].ReasonCodes, want)
	}
	if candidates[0].DisplayName != "reason-agent" || candidates[0].EvaluationStatus != "passed" {
		t.Fatalf("candidate safe summary = %#v", candidates[0])
	}
}

func testResearchPreflightPackageFacts(t *testing.T, packageID string) ResearchPreflightPackageFacts {
	t.Helper()
	pkg := validAgentPackageV4()
	pkg.PackageID = packageID
	pkg.LifecycleState = AgentPackagePublished
	pkg.PublishedAt = "2026-08-17T12:00:00Z"
	finalized, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return ResearchPreflightPackageFacts{
		Package: finalized, RunnablePackageValidated: true, EvaluationPassed: true,
		WorkerState: SourceAgentObservedOnline, BudgetFits: true,
	}
}

func finalizeResearchPreflightPackageFacts(t *testing.T, facts ResearchPreflightPackageFacts) ResearchPreflightPackageFacts {
	t.Helper()
	finalized, err := FinalizeAgentPackage(facts.Package)
	if err != nil {
		t.Fatal(err)
	}
	facts.Package = finalized
	return facts
}

func researchPreflightGapCodes(gaps []ResearchPreflightGap) map[string]bool {
	codes := make(map[string]bool, len(gaps))
	for _, gap := range gaps {
		codes[gap.Code] = true
	}
	return codes
}

func testResearchPreflight() ResearchPreflight {
	return ResearchPreflight{
		PreflightID: "research-preflight-a",
		RequestHash: "private-request-hash",
		Status:      ResearchPreflightStatusReady,
		Candidates: []ResearchPreflightCandidate{{
			PackageID:        "candidate-a",
			PackageVersion:   "1.0.0",
			ContentHash:      "private-content-hash",
			DisplayName:      "Collection research Agent",
			MatchLevel:       ResearchPreflightMatchHigh,
			ReasonCodes:      []string{"topic_match"},
			KnowledgeScope:   []string{"authorized_collection"},
			UpdatedAt:        "2026-08-19T12:00:00Z",
			EvaluationStatus: "passed",
			SupportedSources: []string{ResearchSourceKnowledge},
			Readiness:        ResearchPreflightCheckPass,
		}},
		Checks: []ResearchPreflightCheck{{
			Code:       "package_ready",
			Status:     ResearchPreflightCheckPass,
			Message:    "Package is eligible",
			NextAction: "confirm_package",
		}},
		Gaps: []ResearchPreflightGap{{
			Code:       "optional_context",
			Message:    "More context may improve coverage",
			NextAction: "add_context",
		}},
		ParentRunID: "research-run-parent",
		CreatedAt:   "2026-08-19T12:00:00Z",
		ExpiresAt:   "2026-08-19T12:10:00Z",
	}
}
