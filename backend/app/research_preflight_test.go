package app

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestValidateResearchPreflightRequestNormalizesBoundedScope(t *testing.T) {
	request, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
		Mode:              " auto ",
		Question:          "  compare evidence  ",
		RequestedSources:  []string{"knowledge", "chatlog"},
		SubjectIDs:        []string{" subject-b ", "subject-a", "subject-a"},
		PackageConstraint: "  collection-agent  ",
		ParentRunID:       "research-run-parent_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Question != "compare evidence" || request.Mode != ResearchModeAuto {
		t.Fatalf("normalized request = %#v", request)
	}
	if request.PackageConstraint != "collection-agent" || request.ParentRunID != "research-run-parent_1" {
		t.Fatalf("normalized optional scope = %#v", request)
	}
	if !reflect.DeepEqual(request.SubjectIDs, []string{"subject-a", "subject-b"}) {
		t.Fatalf("normalized subject_ids = %#v", request.SubjectIDs)
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
			name: "subjects bounded",
			request: ResearchPreflightRequest{
				Question:   "compare evidence",
				SubjectIDs: make([]string, researchSubjectIDsMax+1),
			},
			want: "subject_ids exceeds",
		},
		{
			name: "subject canonical",
			request: ResearchPreflightRequest{
				Question:   "compare evidence",
				SubjectIDs: []string{"../private-subject"},
			},
			want: "subject_ids",
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
	t.Run("valid opaque resource id allowed", func(t *testing.T) {
		request, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
			Question:    "compare evidence",
			ParentRunID: "research-run-parent_1",
		})
		if err != nil || request.ParentRunID != "research-run-parent_1" {
			t.Fatalf("request = %#v, %v", request, err)
		}
	})

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

	for _, parentRunID := range []string{
		"/tmp/private-run", "../parent", "research run", "research:run",
		"research\nrun", "研究-run", " research-run", "research-run ", " ",
	} {
		t.Run("invalid "+strings.ReplaceAll(parentRunID, "\n", "newline"), func(t *testing.T) {
			_, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
				Question:    "compare evidence",
				ParentRunID: parentRunID,
			})
			if err == nil || !strings.Contains(err.Error(), "parent_run_id") {
				t.Fatalf("parent_run_id %q error = %v", parentRunID, err)
			}
		})
	}
}

func TestValidateResearchPreflightRejectsInvalidParentRunID(t *testing.T) {
	preflight := testResearchPreflight()
	preflight.ParentRunID = "../private-parent"
	if err := ValidateResearchPreflight(preflight); err == nil || !strings.Contains(err.Error(), "parent_run_id") {
		t.Fatalf("error = %v", err)
	}
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
	preflight := testResearchPreflight()
	preflight.Candidates[0].Coverage = ResearchPreflightCoverage{
		EvidenceCount: 7, ReleaseCount: 1, CitationCount: 3,
		ReleaseIDs: []string{"release-private-locator"},
	}
	preflight.Checks[0].Message = "private free-form check message"
	preflight.Gaps[0].Message = "private free-form gap message"
	encoded, err := json.Marshal(PublicResearchPreflight(preflight))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"content_excerpt", "message_ref", "local_path", "identity_id",
		"request_hash", "content_hash", "private-request-hash", "private-content-hash",
		"release_ids", "release-private-locator", "private free-form check message",
		"private free-form gap message", `"message"`,
	} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("projection leaks %s", forbidden)
		}
	}
	for _, count := range []string{`"evidence_count":7`, `"release_count":1`, `"citation_count":3`} {
		if !bytes.Contains(encoded, []byte(count)) {
			t.Fatalf("projection lost public count %s: %s", count, encoded)
		}
	}
	if !bytes.Contains(encoded, []byte(`"parent_run_id":"research-run-parent"`)) {
		t.Fatalf("projection lost valid parent run id: %s", encoded)
	}
}

func TestResearchPreflightProjectionOmitsInvalidParentRunID(t *testing.T) {
	preflight := testResearchPreflight()
	preflight.ParentRunID = "/tmp/private-parent-run"
	encoded, err := json.Marshal(PublicResearchPreflight(preflight))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"parent_run_id", "/tmp/private-parent-run"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("projection leaks invalid parent %q: %s", forbidden, encoded)
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
				facts = finalizeResearchPreflightPackageFacts(t, facts)
			}
			candidates, _ := RankResearchPreflightCandidates(request, []ResearchPreflightPackageFacts{facts})
			if len(candidates) != test.want {
				t.Fatalf("candidate count = %d, want %d: %#v", len(candidates), test.want, candidates)
			}
		})
	}
}

func TestResearchPreflightEligibilityValidatesAutoResolvedMode(t *testing.T) {
	tests := []struct {
		name     string
		question string
		sources  []string
		modes    []string
	}{
		{
			name: "bounded knowledge resolves quick", question: "Summarize the selected collection.",
			sources: []string{ResearchSourceKnowledge}, modes: []string{ResearchModeAuto, ResearchModeDeep},
		},
		{
			name: "requested auto remains mandatory", question: "Summarize the selected collection.",
			sources: []string{ResearchSourceKnowledge}, modes: []string{ResearchModeQuick},
		},
		{
			name: "comparison question resolves deep", question: "Compare the two cases.",
			sources: []string{ResearchSourceKnowledge}, modes: []string{ResearchModeAuto, ResearchModeQuick},
		},
		{
			name: "chatlog source resolves deep", question: "Summarize the discussion.",
			sources: []string{ResearchSourceChatlog}, modes: []string{ResearchModeAuto, ResearchModeQuick},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := testResearchPreflightPackageFacts(t, "auto-agent")
			facts.Package.ResearchPolicy.Modes = test.modes
			facts = finalizeResearchPreflightPackageFacts(t, facts)
			candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
				Mode: ResearchModeAuto, Question: test.question, RequestedSources: test.sources,
			}, []ResearchPreflightPackageFacts{facts})
			if len(candidates) != 0 {
				t.Fatalf("resolved mode escaped package scope: %#v", candidates)
			}
		})
	}
}

func TestResearchPreflightEligibilityRequiresMinimumToolsForRequestedSources(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		missingTool string
	}{
		{name: "knowledge search", source: ResearchSourceKnowledge, missingTool: "research/" + ResearchToolSearchKnowledge},
		{name: "knowledge evidence fetch", source: ResearchSourceKnowledge, missingTool: "research/" + ResearchToolFetchKnowledgeEvidence},
		{name: "chatlog search", source: ResearchSourceChatlog, missingTool: "research/" + ResearchWorkerToolSearchChatlog},
		{name: "chatlog message fetch", source: ResearchSourceChatlog, missingTool: "research/" + ResearchWorkerToolFetchChatMessage},
		{name: "chatlog context expansion", source: ResearchSourceChatlog, missingTool: "research/" + ResearchWorkerToolExpandChatContext},
		{name: "prior run search", source: ResearchSourcePriorRuns, missingTool: "research/" + ResearchToolSearchPriorRuns},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := testResearchPreflightPackageFacts(t, "source-tool-agent")
			facts = removeResearchPreflightPackageTool(t, facts, test.missingTool)
			candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
				Mode: ResearchModeDeep, Question: "Analyze the selected source.", RequestedSources: []string{test.source},
			}, []ResearchPreflightPackageFacts{facts})
			if len(candidates) != 0 {
				t.Fatalf("source remained eligible without %q: %#v", test.missingTool, candidates)
			}
		})
	}
}

func TestResearchPreflightEligibilityDoesNotRequireChatlogIdentityTools(t *testing.T) {
	facts := testResearchPreflightPackageFacts(t, "chatlog-tool-agent")
	facts = removeResearchPreflightPackageTool(t, facts, "research/"+ResearchWorkerToolResolveChatIdentity)
	facts = removeResearchPreflightPackageTool(t, facts, "research/"+ResearchWorkerToolListIdentityConversations)
	candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeDeep, Question: "Summarize the selected discussion.", RequestedSources: []string{ResearchSourceChatlog},
	}, []ResearchPreflightPackageFacts{facts})
	if len(candidates) != 1 {
		t.Fatalf("optional identity tools became mandatory: %#v", candidates)
	}
}

func TestResearchPreflightRequiredToolBindingsUseCentralCatalog(t *testing.T) {
	tests := []struct {
		source string
		want   []string
	}{
		{source: ResearchSourceKnowledge, want: []string{
			"research/" + ResearchToolSearchKnowledge,
			"research/" + ResearchToolFetchKnowledgeEvidence,
		}},
		{source: ResearchSourceChatlog, want: []string{
			"research/" + ResearchWorkerToolSearchChatlog,
			"research/" + ResearchWorkerToolFetchChatMessage,
			"research/" + ResearchWorkerToolExpandChatContext,
		}},
		{source: ResearchSourcePriorRuns, want: []string{"research/" + ResearchToolSearchPriorRuns}},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			got, ok := researchPreflightRequiredToolBindings(test.source)
			if !ok || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("bindings for %q = %#v ok=%t, want %#v", test.source, got, ok, test.want)
			}
		})
	}
	if got, ok := researchPreflightRequiredToolBindings("unknown"); ok || got != nil {
		t.Fatalf("unknown source bindings = %#v ok=%t", got, ok)
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

func TestRankResearchPreflightCandidatesTreatsNegativeHitsAsZero(t *testing.T) {
	facts := testResearchPreflightPackageFacts(t, "negative-hits-agent")
	facts.TopicHits = -2
	facts.EvidenceHits = -3
	candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeQuick, Question: "Summarize the selected collection.",
	}, []ResearchPreflightPackageFacts{facts})
	if len(candidates) != 1 || candidates[0].MatchLevel != ResearchPreflightMatchLow ||
		containsResearchString(candidates[0].ReasonCodes, "topic_match") ||
		containsResearchString(candidates[0].ReasonCodes, "evidence_coverage") {
		t.Fatalf("negative hit signals = %#v", candidates)
	}
}

func TestRankResearchPreflightCandidatesEmptySourcesDoNotRequireWorker(t *testing.T) {
	facts := testResearchPreflightPackageFacts(t, "empty-source-agent")
	facts.WorkerState = SourceAgentObservedOffline
	candidates, gaps := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeAuto, Question: "Summarize the selected collection.",
	}, []ResearchPreflightPackageFacts{facts})
	if len(candidates) != 1 || candidates[0].Readiness != ResearchPreflightCheckPass || len(gaps) != 0 {
		t.Fatalf("empty-source readiness = candidates %#v gaps %#v", candidates, gaps)
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
	if researchPreflightGapCodes(gaps)["worker_offline"] || !researchPreflightGapCodes(gaps)["budget_insufficient"] {
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

func TestRankResearchPreflightCandidatesGapsOnlyReflectReturnedTopThree(t *testing.T) {
	passA := testResearchPreflightPackageFacts(t, "pass-a")
	passB := testResearchPreflightPackageFacts(t, "pass-b")
	passC := testResearchPreflightPackageFacts(t, "pass-c")
	blocked := testResearchPreflightPackageFacts(t, "blocked-z")
	blocked.WorkerState = SourceAgentObservedOffline
	blocked.BudgetFits = false

	candidates, gaps := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeDeep, Question: "Analyze the selected discussion.", RequestedSources: []string{ResearchSourceChatlog},
	}, []ResearchPreflightPackageFacts{blocked, passC, passA, passB})
	if len(candidates) != 3 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if len(gaps) != 0 {
		t.Fatalf("excluded candidate leaked gaps: %#v", gaps)
	}
}

func TestRankResearchPreflightCandidatesIncludesGapsForReturnedBlockedCandidate(t *testing.T) {
	blocked := testResearchPreflightPackageFacts(t, "blocked-agent")
	blocked.WorkerState = "unknown"
	blocked.BudgetFits = false
	candidates, gaps := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeDeep, Question: "Analyze the selected discussion.", RequestedSources: []string{ResearchSourceChatlog},
	}, []ResearchPreflightPackageFacts{blocked})
	if len(candidates) != 1 || candidates[0].Readiness != ResearchPreflightCheckBlocked {
		t.Fatalf("blocked candidate = %#v", candidates)
	}
	codes := researchPreflightGapCodes(gaps)
	if !codes["worker_offline"] || !codes["budget_insufficient"] {
		t.Fatalf("selected candidate gaps = %#v", gaps)
	}
}

func TestRankResearchPreflightCandidatesRejectsConflictingSameIdentityFactsDeterministically(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ResearchPreflightPackageFacts)
	}{
		{name: "freshness conflict", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.LatestPublishedAt = "2026-08-18T12:00:00Z"
			facts.FreshRelease = true
		}},
		{name: "worker conflict", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.WorkerState = SourceAgentObservedOffline
		}},
		{name: "budget conflict", mutate: func(facts *ResearchPreflightPackageFacts) {
			facts.BudgetFits = false
		}},
	}
	request := ResearchPreflightRequest{
		Mode: ResearchModeDeep, Question: "Analyze the selected discussion.", RequestedSources: []string{ResearchSourceChatlog},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := testResearchPreflightPackageFacts(t, "conflict-agent")
			conflict := original
			test.mutate(&conflict)
			stable := testResearchPreflightPackageFacts(t, "stable-agent")

			firstCandidates, firstGaps := RankResearchPreflightCandidates(request, []ResearchPreflightPackageFacts{original, conflict, stable})
			secondCandidates, secondGaps := RankResearchPreflightCandidates(request, []ResearchPreflightPackageFacts{stable, conflict, original})
			if !reflect.DeepEqual(firstCandidates, secondCandidates) || !reflect.DeepEqual(firstGaps, secondGaps) {
				t.Fatalf("same-identity conflict depended on input order: %#v/%#v versus %#v/%#v", firstCandidates, firstGaps, secondCandidates, secondGaps)
			}
			if len(firstCandidates) != 1 || firstCandidates[0].PackageID != "stable-agent" || len(firstGaps) != 0 {
				t.Fatalf("conflicting identity was not excluded: candidates %#v gaps %#v", firstCandidates, firstGaps)
			}
		})
	}
}

func TestRankResearchPreflightCandidatesDeduplicatesIdenticalSameIdentityFacts(t *testing.T) {
	facts := testResearchPreflightPackageFacts(t, "duplicate-agent")
	candidates, gaps := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeQuick, Question: "Summarize the selected collection.",
	}, []ResearchPreflightPackageFacts{facts, facts})
	if len(candidates) != 1 || candidates[0].PackageID != "duplicate-agent" || len(gaps) != 0 {
		t.Fatalf("identical identity was not deduplicated: candidates %#v gaps %#v", candidates, gaps)
	}
}

func TestRankResearchPreflightCandidatesReturnsNoEligibleGapForOnlyConflictingIdentity(t *testing.T) {
	original := testResearchPreflightPackageFacts(t, "conflict-agent")
	conflict := original
	conflict.BudgetFits = false
	candidates, gaps := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeQuick, Question: "Summarize the selected collection.",
	}, []ResearchPreflightPackageFacts{original, conflict})
	if len(candidates) != 0 || len(gaps) != 1 || gaps[0].Code != "no_eligible_package" {
		t.Fatalf("only conflicting identity = candidates %#v gaps %#v", candidates, gaps)
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

func TestRankResearchPreflightCandidatesNormalizesAndSortsValidatedFreshness(t *testing.T) {
	newer := testResearchPreflightPackageFacts(t, "newer-agent")
	newer.LatestPublishedAt = "2026-08-18T12:00:00-04:00"
	newer.FreshRelease = true
	older := testResearchPreflightPackageFacts(t, "older-agent")
	older.LatestPublishedAt = "2026-08-18T15:30:00Z"
	older.FreshRelease = true

	candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeQuick, Question: "Summarize the selected collection.",
	}, []ResearchPreflightPackageFacts{older, newer})
	if len(candidates) != 2 || candidates[0].PackageID != "newer-agent" {
		t.Fatalf("freshness order = %#v", candidates)
	}
	if candidates[0].UpdatedAt != "2026-08-18T16:00:00Z" || candidates[1].UpdatedAt != "2026-08-18T15:30:00Z" {
		t.Fatalf("normalized freshness = %#v", candidates)
	}
}

func TestRankResearchPreflightCandidatesRejectsUnvalidatedFreshnessText(t *testing.T) {
	t.Run("invalid latest falls back to valid package publication", func(t *testing.T) {
		facts := testResearchPreflightPackageFacts(t, "fallback-freshness-agent")
		facts.LatestPublishedAt = "not-a-time"
		facts.Package.PublishedAt = "2026-08-18T12:00:00-04:00"
		facts.FreshRelease = true
		candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
			Mode: ResearchModeQuick, Question: "Summarize the selected collection.",
		}, []ResearchPreflightPackageFacts{facts})
		if len(candidates) != 1 || candidates[0].UpdatedAt != "2026-08-18T16:00:00Z" ||
			containsResearchString(candidates[0].ReasonCodes, "fresh_release") {
			t.Fatalf("fallback freshness = %#v", candidates)
		}
	})

	t.Run("invalid times are neither exposed nor treated as fresh", func(t *testing.T) {
		facts := testResearchPreflightPackageFacts(t, "invalid-freshness-agent")
		facts.LatestPublishedAt = "not-a-time"
		facts.Package.PublishedAt = "also-not-a-time"
		facts.FreshRelease = true
		candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
			Mode: ResearchModeQuick, Question: "Summarize the selected collection.",
		}, []ResearchPreflightPackageFacts{facts})
		if len(candidates) != 1 || candidates[0].UpdatedAt != "" ||
			containsResearchString(candidates[0].ReasonCodes, "fresh_release") {
			t.Fatalf("invalid freshness leaked: %#v", candidates)
		}
	})

	t.Run("valid time without trusted freshness fact has no reason", func(t *testing.T) {
		facts := testResearchPreflightPackageFacts(t, "not-fresh-agent")
		facts.LatestPublishedAt = "2026-08-18T12:00:00Z"
		facts.FreshRelease = false
		candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
			Mode: ResearchModeQuick, Question: "Summarize the selected collection.",
		}, []ResearchPreflightPackageFacts{facts})
		if len(candidates) != 1 || containsResearchString(candidates[0].ReasonCodes, "fresh_release") {
			t.Fatalf("untrusted freshness reason = %#v", candidates)
		}
	})
}

func TestRankResearchPreflightCandidatesBindsFreshnessSignalToTrustedLatestTime(t *testing.T) {
	trustedLatest := testResearchPreflightPackageFacts(t, "z-trusted-latest")
	trustedLatest.LatestPublishedAt = "2026-08-17T12:00:00Z"
	trustedLatest.FreshRelease = true
	untrustedLatest := testResearchPreflightPackageFacts(t, "a-untrusted-latest")
	untrustedLatest.LatestPublishedAt = "2026-08-19T12:00:00Z"
	untrustedLatest.FreshRelease = false
	fallbackOnly := testResearchPreflightPackageFacts(t, "b-fallback-only")
	fallbackOnly.LatestPublishedAt = "not-a-time"
	fallbackOnly.Package.PublishedAt = "2026-08-20T12:00:00Z"
	fallbackOnly.FreshRelease = true

	candidates, _ := RankResearchPreflightCandidates(ResearchPreflightRequest{
		Mode: ResearchModeQuick, Question: "Summarize the selected collection.",
	}, []ResearchPreflightPackageFacts{fallbackOnly, untrustedLatest, trustedLatest})
	if len(candidates) != 3 || candidates[0].PackageID != "z-trusted-latest" ||
		candidates[1].PackageID != "a-untrusted-latest" || candidates[2].PackageID != "b-fallback-only" {
		t.Fatalf("freshness signal order = %#v", candidates)
	}
	if !containsResearchString(candidates[0].ReasonCodes, "fresh_release") ||
		containsResearchString(candidates[1].ReasonCodes, "fresh_release") ||
		containsResearchString(candidates[2].ReasonCodes, "fresh_release") {
		t.Fatalf("freshness signal reasons = %#v", candidates)
	}
}

func TestRankResearchPreflightCandidatesUsesStableReasonCodesOnly(t *testing.T) {
	facts := testResearchPreflightPackageFacts(t, "reason-agent")
	facts.TopicHits = 2
	facts.EvidenceHits = 3
	facts.LatestPublishedAt = "2026-08-18T12:00:00Z"
	facts.FreshRelease = true
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

func removeResearchPreflightPackageTool(t *testing.T, facts ResearchPreflightPackageFacts, toolID string) ResearchPreflightPackageFacts {
	t.Helper()
	allowed := facts.Package.ResearchPolicy.AllowedTools[:0]
	for _, candidate := range facts.Package.ResearchPolicy.AllowedTools {
		if candidate != toolID {
			allowed = append(allowed, candidate)
		}
	}
	facts.Package.ResearchPolicy.AllowedTools = allowed
	rules := facts.Package.ToolPolicy.Tools[:0]
	for _, rule := range facts.Package.ToolPolicy.Tools {
		if strings.TrimSpace(rule.MCPServer)+"/"+strings.TrimSpace(rule.ToolName) != toolID {
			rules = append(rules, rule)
		}
	}
	facts.Package.ToolPolicy.Tools = rules
	return finalizeResearchPreflightPackageFacts(t, facts)
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
