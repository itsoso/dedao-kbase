package app

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestValidateAgentCompilationRequest(t *testing.T) {
	valid := AgentCompilationRequest{
		SchemaVersion:        AgentCompilationRequestSchemaVersion,
		Mode:                 AgentCompilationModeDual,
		PrimaryReleaseID:     "release-primary",
		SupportingReleaseIDs: []string{"release-support"},
		Version:              "1.0.0",
	}
	for _, mode := range []string{
		AgentCompilationModeDual,
		AgentCompilationModeEvidence,
		AgentCompilationModeStudy,
	} {
		request := valid
		request.Mode = mode
		if err := ValidateAgentCompilationRequest(request); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}

	tests := []struct {
		name   string
		mutate func(*AgentCompilationRequest)
		want   string
	}{
		{
			name: "schema",
			mutate: func(request *AgentCompilationRequest) {
				request.SchemaVersion = "agent-compilation-request.v0"
			},
			want: "schema_version",
		},
		{
			name: "mode",
			mutate: func(request *AgentCompilationRequest) {
				request.Mode = "automatic"
			},
			want: "mode",
		},
		{
			name: "primary release",
			mutate: func(request *AgentCompilationRequest) {
				request.PrimaryReleaseID = " "
			},
			want: "primary_release_id",
		},
		{
			name: "primary release length",
			mutate: func(request *AgentCompilationRequest) {
				request.PrimaryReleaseID = strings.Repeat("r", agentCompilationMaxReleaseIDRunes+1)
			},
			want: "primary_release_id",
		},
		{
			name: "version",
			mutate: func(request *AgentCompilationRequest) {
				request.Version = "latest"
			},
			want: "version",
		},
		{
			name: "duplicate support",
			mutate: func(request *AgentCompilationRequest) {
				request.SupportingReleaseIDs = []string{"release-support", "release-support"}
			},
			want: "duplicate",
		},
		{
			name: "primary repeated as support",
			mutate: func(request *AgentCompilationRequest) {
				request.SupportingReleaseIDs = []string{"release-primary"}
			},
			want: "primary",
		},
		{
			name: "support bound",
			mutate: func(request *AgentCompilationRequest) {
				request.SupportingReleaseIDs = make([]string, agentCompilationMaxSupportingReleases+1)
				for index := range request.SupportingReleaseIDs {
					request.SupportingReleaseIDs[index] = "release-support-" + string(rune('a'+index))
				}
			},
			want: "supporting_release_ids",
		},
		{
			name: "support release length",
			mutate: func(request *AgentCompilationRequest) {
				request.SupportingReleaseIDs = []string{
					strings.Repeat("r", agentCompilationMaxReleaseIDRunes+1),
				}
			},
			want: "supporting_release_ids",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := valid
			request.SupportingReleaseIDs = append([]string(nil), valid.SupportingReleaseIDs...)
			testCase.mutate(&request)
			err := ValidateAgentCompilationRequest(request)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validation error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestValidateAgentCompilationContract(t *testing.T) {
	readyPackage := validAgentPackage()
	compilation := AgentCompilation{
		SchemaVersion:   AgentCompilationSchemaVersion,
		CompilerVersion: AgentCompilerVersion,
		CompilationID:   "compilation-fixture",
		Mode:            AgentCompilationModeDual,
		AssemblyID:      "assembly-fixture",
		ReleaseIDs:      []string{"release-1"},
		Status:          AgentCompilationStatusPartial,
		Candidates: []AgentCompilationCandidate{
			{
				Kind:        AgentCompilationCandidateStudy,
				Status:      AgentCompilationCandidateReady,
				Package:     &readyPackage,
				NextActions: []string{AgentCompilationNextActionEvaluate},
			},
			{
				Kind:   AgentCompilationCandidateEvidence,
				Status: AgentCompilationCandidateBlocked,
				Issues: []AgentCompilationIssue{{
					Code:    AgentCompilationIssueSupportingReleaseRequired,
					Message: "An independently eligible supporting release is required.",
				}},
				NextActions: []string{AgentCompilationNextActionSelectSupport},
			},
		},
	}
	if err := ValidateAgentCompilation(compilation); err != nil {
		t.Fatalf("valid compilation rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AgentCompilation)
		want   string
	}{
		{
			name: "release bound",
			mutate: func(value *AgentCompilation) {
				value.ReleaseIDs = make([]string, agentCompilationMaxSupportingReleases+2)
				for index := range value.ReleaseIDs {
					value.ReleaseIDs[index] = fmt.Sprintf("release-%d", index)
				}
			},
			want: "release_ids",
		},
		{
			name: "release identifier length",
			mutate: func(value *AgentCompilation) {
				value.ReleaseIDs[0] = strings.Repeat("r", agentCompilationMaxReleaseIDRunes+1)
			},
			want: "release_ids",
		},
		{
			name: "candidate bound",
			mutate: func(value *AgentCompilation) {
				value.Candidates = append(value.Candidates, AgentCompilationCandidate{
					Kind:   AgentCompilationCandidateStudy,
					Status: AgentCompilationCandidateBlocked,
					Issues: []AgentCompilationIssue{{Code: "blocked", Message: "blocked"}},
				})
			},
			want: "candidates",
		},
		{
			name: "duplicate kind",
			mutate: func(value *AgentCompilation) {
				value.Candidates[1].Kind = AgentCompilationCandidateStudy
			},
			want: "duplicate",
		},
		{
			name: "mode requires matching candidates",
			mutate: func(value *AgentCompilation) {
				value.Mode = AgentCompilationModeStudy
			},
			want: "mode",
		},
		{
			name: "ready requires package",
			mutate: func(value *AgentCompilation) {
				value.Candidates[0].Package = nil
			},
			want: "package",
		},
		{
			name: "candidate kind requires matching package schema",
			mutate: func(value *AgentCompilation) {
				pkg := validAgentPackageV2()
				value.Candidates[0].Package = &pkg
			},
			want: "schema_version",
		},
		{
			name: "research v4 requires policy",
			mutate: func(value *AgentCompilation) {
				pkg := validAgentPackageV4()
				pkg.ResearchPolicy = nil
				value.Candidates[0].Package = &pkg
			},
			want: "v4 package requires research_policy",
		},
		{
			name: "blocked excludes package",
			mutate: func(value *AgentCompilation) {
				pkg := readyPackage
				value.Candidates[1].Package = &pkg
			},
			want: "blocked",
		},
		{
			name: "blocked requires issue",
			mutate: func(value *AgentCompilation) {
				value.Candidates[1].Issues = nil
			},
			want: "issues",
		},
		{
			name: "bounded issue message",
			mutate: func(value *AgentCompilation) {
				value.Candidates[1].Issues[0].Message = strings.Repeat("界", agentCompilationMaxIssueMessageRunes+1)
			},
			want: "message",
		},
		{
			name: "bounded next action",
			mutate: func(value *AgentCompilation) {
				value.Candidates[0].NextActions[0] = strings.Repeat(
					"x",
					agentCompilationMaxNextActionRunes+1,
				)
			},
			want: "next_actions",
		},
		{
			name: "status agreement",
			mutate: func(value *AgentCompilation) {
				value.Status = AgentCompilationStatusReady
			},
			want: "status",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			value := cloneAgentCompilationForTest(t, compilation)
			testCase.mutate(&value)
			err := ValidateAgentCompilation(value)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validation error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func cloneAgentCompilationForTest(t *testing.T, value AgentCompilation) AgentCompilation {
	t.Helper()
	cloned := value
	cloned.ReleaseIDs = append([]string(nil), value.ReleaseIDs...)
	cloned.Candidates = make([]AgentCompilationCandidate, len(value.Candidates))
	for index, candidate := range value.Candidates {
		cloned.Candidates[index] = candidate
		cloned.Candidates[index].Issues = append([]AgentCompilationIssue(nil), candidate.Issues...)
		cloned.Candidates[index].NextActions = append([]string(nil), candidate.NextActions...)
		if candidate.Package != nil {
			pkg := *candidate.Package
			cloned.Candidates[index].Package = &pkg
		}
	}
	return cloned
}

func TestCompileAgentPackagesDualBuildsDeterministicCandidates(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"干预能改善结局",
		"Publisher Primary",
		"dedao_ebook",
	)
	support := agentCompilerTestRelease(
		"release-support",
		"book-support",
		"2026-07-26T11:00:00Z",
		"干预能改善结局",
		"Publisher Support",
		"wechat_mp_article",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)
	saveKnowledgeAssemblyRelease(t, store, support)
	request := AgentCompilationRequest{
		SchemaVersion:        AgentCompilationRequestSchemaVersion,
		Mode:                 AgentCompilationModeDual,
		PrimaryReleaseID:     primary.ReleaseID,
		SupportingReleaseIDs: []string{support.ReleaseID},
		Version:              "1.2.0",
	}
	requestBefore := request
	requestBefore.SupportingReleaseIDs = append([]string(nil), request.SupportingReleaseIDs...)
	primaryBefore, err := store.LoadKnowledgeRelease(primary.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	supportBefore, err := store.LoadKnowledgeRelease(support.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}

	first, err := CompileAgentPackages(store, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileAgentPackages(store, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != AgentCompilationStatusReady ||
		len(first.Candidates) != 2 ||
		first.Candidates[0].Kind != AgentCompilationCandidateStudy ||
		first.Candidates[1].Kind != AgentCompilationCandidateEvidence {
		t.Fatalf("dual compilation = %#v", first)
	}
	for index, candidate := range first.Candidates {
		if candidate.Status != AgentCompilationCandidateReady || candidate.Package == nil {
			t.Fatalf("candidate[%d] = %#v", index, candidate)
		}
		if err := ValidateAgentPackage(*candidate.Package, store, AgentReadOnlyToolIDs()); err != nil {
			t.Fatalf("candidate[%d] package invalid: %v", index, err)
		}
	}
	if first.CompilationID != second.CompilationID ||
		first.AssemblyID != second.AssemblyID ||
		first.Candidates[0].Package.ContentHash != second.Candidates[0].Package.ContentHash ||
		first.Candidates[1].Package.ContentHash != second.Candidates[1].Package.ContentHash {
		t.Fatalf("dual compilation is not deterministic: first=%#v second=%#v", first, second)
	}
	if !reflect.DeepEqual(request, requestBefore) {
		t.Fatalf("compiler mutated request: before=%#v after=%#v", requestBefore, request)
	}
	primaryAfter, err := store.LoadKnowledgeRelease(primary.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	supportAfter, err := store.LoadKnowledgeRelease(support.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(primaryBefore, primaryAfter) ||
		!reflect.DeepEqual(supportBefore, supportAfter) {
		t.Fatalf("compiler mutated releases")
	}
}

func TestAgentCompilationResearchOptInEmitsV4WithoutChangingOrdinaryTools(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease("release-research", "book-research", "2026-08-14T10:00:00Z", "有证据的结论", "Publisher", "dedao_ebook")
	saveKnowledgeAssemblyRelease(t, store, primary)
	base := AgentCompilationRequest{
		SchemaVersion: AgentCompilationRequestSchemaVersion, Mode: AgentCompilationModeStudy,
		PrimaryReleaseID: primary.ReleaseID, Version: "1.0.0",
	}
	ordinary, err := CompileAgentPackages(store, base)
	if err != nil {
		t.Fatal(err)
	}
	researchRequest := base
	researchRequest.ResearchEnabled = true
	research, err := CompileAgentPackages(store, researchRequest)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryPackage := ordinary.Candidates[0].Package
	researchPackage := research.Candidates[0].Package
	if ordinaryPackage == nil || researchPackage == nil {
		t.Fatalf("ordinary=%#v research=%#v", ordinary, research)
	}
	if ordinaryPackage.SchemaVersion != AgentPackageSchemaVersionV1 || ordinaryPackage.ResearchPolicy != nil ||
		!reflect.DeepEqual(ordinaryPackage.ToolPolicy, allReadOnlyAgentCompilationTools()) {
		t.Fatalf("ordinary compilation gained research: %#v", ordinaryPackage)
	}
	if researchPackage.SchemaVersion != AgentPackageSchemaVersionV4 || researchPackage.ResearchPolicy == nil ||
		!agentTestContainsString(researchPackage.UIManifest.Capabilities, "deep_research") ||
		researchPackage.EvaluationPolicy.SuiteVersion != "research-agent-v1" {
		t.Fatalf("research package = %#v", researchPackage)
	}
	if err := ValidateAgentPackage(*researchPackage, store, AgentPackageKnownToolIDs()); err != nil {
		t.Fatalf("research package invalid: %v", err)
	}
	for _, tool := range ResearchAgentToolIDs() {
		server, name, _ := strings.Cut(tool, "/")
		found := false
		for _, rule := range researchPackage.ToolPolicy.Tools {
			found = found || (rule.MCPServer == server && rule.ToolName == name && rule.Decision == AgentToolAllow)
		}
		if !found {
			t.Fatalf("research package missing %q", tool)
		}
	}
	if ordinary.CompilationID == research.CompilationID || ordinaryPackage.ContentHash == researchPackage.ContentHash {
		t.Fatal("research opt-in was not bound into compilation/package identity")
	}
}

func TestAgentCompilerAcceptsMaterializedCollectionForResearch(t *testing.T) {
	store, collection := collectionMaterializationFixture(t)
	materialized, _, err := store.MaterializeKnowledgeCollectionRelease(collection.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion: AgentCompilationRequestSchemaVersion,
		Mode:          AgentCompilationModeStudy, PrimaryReleaseID: materialized.Release.ReleaseID,
		Version: "1.0.0", ResearchEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusReady || len(result.Candidates) != 1 || result.Candidates[0].Package == nil {
		t.Fatalf("compilation=%#v", result)
	}
	pkg := result.Candidates[0].Package
	if pkg.SchemaVersion != AgentPackageSchemaVersionV4 || pkg.ResearchPolicy == nil ||
		len(pkg.Releases) != 1 || pkg.Releases[0].ReleaseID != materialized.Release.ReleaseID ||
		pkg.Releases[0].ContentHash != materialized.Release.ContentHash || len(pkg.CollectionReleases) != 0 ||
		pkg.SafetyPolicy.UsagePolicy != BookUsageEvidenceOnly ||
		!agentTestContainsString(pkg.UIManifest.Capabilities, "deep_research") {
		t.Fatalf("materialized Research package=%#v", pkg)
	}
	if err := ValidateAgentPackage(*pkg, store, AgentPackageKnownToolIDs()); err != nil {
		t.Fatalf("materialized Research package invalid: %v", err)
	}
	for _, toolID := range ResearchAgentToolIDs() {
		server, tool, _ := strings.Cut(toolID, "/")
		allowed := false
		for _, rule := range pkg.ToolPolicy.Tools {
			allowed = allowed || rule.MCPServer == server && rule.ToolName == tool && rule.Decision == AgentToolAllow
		}
		if !allowed {
			t.Fatalf("materialized Research package missing allow rule %q", toolID)
		}
	}
	if collection.ReleaseID == materialized.Release.ReleaseID || materialized.Release.Book.SourceKey != collection.ReleaseID {
		t.Fatalf("source collection provenance was not retained: collection=%#v release=%#v", collection, materialized.Release)
	}
}

func TestAgentCompilerAcceptsMaterializedCollectionWithLongCitedChunk(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	saveCollectionArticleFixture(t, store, "book-a", "article-a", "Fixture account")
	article, err := store.LoadPackage("book-a")
	if err != nil {
		t.Fatal(err)
	}
	article.Chunks[0].Text = strings.Repeat("证", knowledgeAssemblyMaxStatementRunes*2+17)
	article.Book.ContentHash = ""
	if err := store.SavePackage(*article); err != nil {
		t.Fatal(err)
	}
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-a", "article-a")
	if _, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture"); err != nil {
		t.Fatal(err)
	}
	collection, err := store.PublishKnowledgeCollection("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	materialized, _, err := store.MaterializeKnowledgeCollectionRelease(collection.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if materialized.Release.Analysis == nil || len(materialized.Release.Analysis.Claims) != 3 {
		t.Fatalf("long chunk claims=%#v", materialized.Release.Analysis)
	}
	for _, claim := range materialized.Release.Analysis.Claims {
		if utf8.RuneCountInString(claim.Statement) > knowledgeAssemblyMaxStatementRunes {
			t.Fatalf("oversized materialized claim=%#v", claim)
		}
	}
	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion: AgentCompilationRequestSchemaVersion, Mode: AgentCompilationModeStudy,
		PrimaryReleaseID: materialized.Release.ReleaseID, Version: "1.0.0", ResearchEnabled: true,
	})
	if err != nil || result.Status != AgentCompilationStatusReady {
		t.Fatalf("long chunk compilation=%#v err=%v", result, err)
	}
}

func TestAgentCompilationResearchStudyPreservesEvidenceOnlyAndInfersLegacyDedaoSource(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-research-legacy", "book-research-legacy", "2026-08-14T10:00:00Z",
		"有证据的结论", "Publisher", "",
	)
	primary.Book.DedaoID = 128942
	primary.UsagePolicy = BookUsageEvidenceOnly
	primary.Quality.UsagePolicy = BookUsageEvidenceOnly
	saveKnowledgeAssemblyRelease(t, store, primary)

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion: AgentCompilationRequestSchemaVersion, Mode: AgentCompilationModeStudy,
		PrimaryReleaseID: primary.ReleaseID, Version: "1.0.0", ResearchEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusReady || len(result.Candidates) != 1 || result.Candidates[0].Package == nil {
		t.Fatalf("legacy Research compilation = %#v", result)
	}
	pkg := result.Candidates[0].Package
	if !reflect.DeepEqual(pkg.RetrievalPolicy.AllowedSourceTypes, []string{"dedao_ebook"}) {
		t.Fatalf("allowed source types = %#v", pkg.RetrievalPolicy.AllowedSourceTypes)
	}
	if pkg.SafetyPolicy.UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("usage policy = %q", pkg.SafetyPolicy.UsagePolicy)
	}
}

func TestCompileAgentPackagesDualKeepsStudyReadyWithoutSupport(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"单一来源结论",
		"Publisher Primary",
		"dedao_ebook",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeDual,
		PrimaryReleaseID: primary.ReleaseID,
		Version:          "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusPartial || len(result.Candidates) != 2 {
		t.Fatalf("dual compilation = %#v", result)
	}
	if result.Candidates[0].Kind != AgentCompilationCandidateStudy ||
		result.Candidates[0].Status != AgentCompilationCandidateReady ||
		result.Candidates[0].Package == nil {
		t.Fatalf("study candidate = %#v", result.Candidates[0])
	}
	evidence := result.Candidates[1]
	if evidence.Kind != AgentCompilationCandidateEvidence ||
		evidence.Status != AgentCompilationCandidateBlocked ||
		evidence.Package != nil ||
		len(evidence.Issues) != 1 ||
		evidence.Issues[0].Code != AgentCompilationIssueSupportingReleaseRequired {
		t.Fatalf("evidence candidate = %#v", evidence)
	}
}

func agentCompilerTestRelease(
	releaseID, bookID, createdAt, statement, publisher, sourceType string,
) KnowledgeRelease {
	release := knowledgeAssemblyTestRelease(
		releaseID,
		bookID,
		createdAt,
		statement,
		publisher,
		sourceType,
	)
	release.UsagePolicy = BookUsageStandard
	release.Quality.UsagePolicy = BookUsageStandard
	return release
}

func knowledgeReleaseRecordForTest(release KnowledgeRelease) KnowledgeReleaseRecord {
	return KnowledgeReleaseRecord{
		ReleaseID:   release.ReleaseID,
		BookID:      release.BookID,
		ContentHash: release.ContentHash,
		Supersedes:  release.Supersedes,
		UsagePolicy: release.UsagePolicy,
		CreatedAt:   release.CreatedAt,
	}
}

func TestCompileAgentPackagesEvidenceAcceptsExplicitIndependentSupport(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"主要结论",
		"Publisher Primary",
		"dedao_ebook",
	)
	primary.Analysis.Claims[0].CitationIDs = append(
		primary.Analysis.Claims[0].CitationIDs,
		primary.Analysis.Claims[0].CitationIDs[0],
	)
	support := agentCompilerTestRelease(
		"release-support",
		"book-support",
		"2026-07-26T11:00:00Z",
		"主要结论",
		"Publisher Support",
		"wechat_mp_article",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)
	saveKnowledgeAssemblyRelease(t, store, support)

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:        AgentCompilationRequestSchemaVersion,
		Mode:                 AgentCompilationModeEvidence,
		PrimaryReleaseID:     primary.ReleaseID,
		SupportingReleaseIDs: []string{support.ReleaseID},
		Version:              "2.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusReady || len(result.Candidates) != 1 {
		t.Fatalf("evidence compilation = %#v", result)
	}
	pkg := result.Candidates[0].Package
	if pkg == nil || pkg.SchemaVersion != AgentPackageSchemaVersionV2 ||
		pkg.EvidencePolicy == nil {
		t.Fatalf("evidence package = %#v", pkg)
	}
	if len(pkg.EvidencePolicy.ReleaseRoles) != 2 ||
		pkg.EvidencePolicy.ReleaseRoles[0] != (AgentPackageEvidenceReleaseRole{
			ReleaseID: primary.ReleaseID,
			Role:      AgentEvidenceReleasePrimary,
		}) ||
		pkg.EvidencePolicy.ReleaseRoles[1] != (AgentPackageEvidenceReleaseRole{
			ReleaseID: support.ReleaseID,
			Role:      AgentEvidenceReleaseSupporting,
		}) {
		t.Fatalf("release roles = %#v", pkg.EvidencePolicy.ReleaseRoles)
	}
	for _, ref := range pkg.Releases {
		if len(ref.CitationIDs) == 0 ||
			!reflect.DeepEqual(ref.CitationIDs, sortedUniqueStrings(ref.CitationIDs)) {
			t.Fatalf("release citation allowlist is not canonical: %#v", ref)
		}
		release, err := store.LoadKnowledgeRelease(ref.ReleaseID)
		if err != nil {
			t.Fatal(err)
		}
		available := stringSet(agentCompilationReleaseCitationIDs(*release))
		for _, citationID := range ref.CitationIDs {
			if !available[citationID] {
				t.Fatalf("citation %q does not resolve to release %q", citationID, ref.ReleaseID)
			}
		}
	}
}

func TestCompileAgentPackagesEvidenceRejectsInvalidExplicitSupport(t *testing.T) {
	tests := []struct {
		name          string
		primarySource string
		supportSource string
		supporter     string
		saveSupport   bool
		wantCode      string
	}{
		{
			name:          "outside assembly",
			primarySource: "Publisher Primary",
			supportSource: "Publisher Support",
			supporter:     "release-missing",
			saveSupport:   false,
			wantCode:      AgentCompilationIssueReleaseNotInAssembly,
		},
		{
			name:          "unrelated publication",
			primarySource: "Publisher Primary",
			supportSource: "Publisher Support",
			supporter:     "release-support",
			saveSupport:   true,
			wantCode:      AgentCompilationIssueReleaseNotRelated,
		},
		{
			name:          "same publication",
			primarySource: "Same Publisher",
			supportSource: " same publisher ",
			supporter:     "release-support",
			saveSupport:   true,
			wantCode:      AgentCompilationIssueReleaseNotIndependent,
		},
		{
			name:          "ineligible publication",
			primarySource: "Publisher Primary",
			supportSource: "",
			supporter:     "release-support",
			saveSupport:   true,
			wantCode:      AgentCompilationIssueReleaseNotIndependent,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			primary := agentCompilerTestRelease(
				"release-primary",
				"book-primary",
				"2026-07-26T10:00:00Z",
				"主要结论",
				testCase.primarySource,
				"dedao_ebook",
			)
			saveKnowledgeAssemblyRelease(t, store, primary)
			if testCase.saveSupport {
				supportStatement := "主要结论"
				if testCase.name == "unrelated publication" {
					supportStatement = "完全无关的结论"
				}
				support := agentCompilerTestRelease(
					"release-support",
					"book-support",
					"2026-07-26T11:00:00Z",
					supportStatement,
					testCase.supportSource,
					"wechat_mp_article",
				)
				saveKnowledgeAssemblyRelease(t, store, support)
			}
			result, err := CompileAgentPackages(store, AgentCompilationRequest{
				SchemaVersion:        AgentCompilationRequestSchemaVersion,
				Mode:                 AgentCompilationModeEvidence,
				PrimaryReleaseID:     primary.ReleaseID,
				SupportingReleaseIDs: []string{testCase.supporter},
				Version:              "2.0.0",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != AgentCompilationStatusBlocked ||
				len(result.Candidates) != 1 ||
				len(result.Candidates[0].Issues) != 1 ||
				result.Candidates[0].Issues[0].Code != testCase.wantCode {
				t.Fatalf("evidence compilation = %#v", result)
			}
		})
	}

	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"主要结论",
		"Publisher Primary",
		"dedao_ebook",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)
	if _, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:        AgentCompilationRequestSchemaVersion,
		Mode:                 AgentCompilationModeEvidence,
		PrimaryReleaseID:     primary.ReleaseID,
		SupportingReleaseIDs: []string{primary.ReleaseID},
		Version:              "2.0.0",
	}); err == nil || !strings.Contains(err.Error(), "primary") {
		t.Fatalf("primary repeated as support error = %v", err)
	}
}

func TestCompileAgentPackagesEvidenceUsesFullScopedAssemblyBeyondGlobalClusterPage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	unrelated := agentCompilerTestRelease(
		"release-unrelated",
		"book-unrelated",
		"2026-07-25T10:00:00Z",
		"a unrelated assertion 000",
		"Publisher Unrelated",
		"wechat_mp_article",
	)
	for index := 0; index < knowledgeAssemblyMaxLimit; index++ {
		claim := unrelated.Analysis.Claims[0]
		claim.ID = fmt.Sprintf("claim-unrelated-%03d", index)
		claim.Statement = fmt.Sprintf("a unrelated assertion %03d", index)
		unrelated.Analysis.Claims = append(unrelated.Analysis.Claims, claim)
	}
	unrelated.Analysis.Claims = unrelated.Analysis.Claims[1:]
	saveKnowledgeAssemblyRelease(t, store, unrelated)
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"zzzz 治疗能降低风险",
		"Publisher Primary",
		"dedao_ebook",
	)
	support := agentCompilerTestRelease(
		"release-support",
		"book-support",
		"2026-07-26T11:00:00Z",
		"zzzz 治疗能降低风险",
		"Publisher Support",
		"wechat_mp_article",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)
	saveKnowledgeAssemblyRelease(t, store, support)

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:        AgentCompilationRequestSchemaVersion,
		Mode:                 AgentCompilationModeEvidence,
		PrimaryReleaseID:     primary.ReleaseID,
		SupportingReleaseIDs: []string{support.ReleaseID},
		Version:              "2.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusReady ||
		result.Candidates[0].Package == nil ||
		!reflect.DeepEqual(result.ReleaseIDs, []string{primary.ReleaseID, support.ReleaseID}) {
		t.Fatalf("scoped evidence compilation = %#v", result)
	}
}

func TestCompileAgentPackagesIgnoresMalformedUnrelatedRelease(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"学习结论",
		"Publisher Primary",
		"dedao_ebook",
	)
	unrelated := agentCompilerTestRelease(
		"release-unrelated",
		"book-unrelated",
		"2026-07-26T11:00:00Z",
		"无关结论",
		"Publisher Unrelated",
		"wechat_mp_article",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)
	saveKnowledgeAssemblyRelease(t, store, unrelated)
	if err := os.WriteFile(
		store.KnowledgeReleasePath(unrelated.ReleaseID),
		[]byte(`{"malformed":`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeStudy,
		PrimaryReleaseID: primary.ReleaseID,
		Version:          "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusReady ||
		result.Candidates[0].Package == nil {
		t.Fatalf("study compilation = %#v", result)
	}
}

func TestFinalizeAgentCompilationCandidateDoesNotLeakStorePath(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	pkg := validAgentPackage()
	candidate := finalizeAgentCompilationCandidate(
		store,
		AgentCompilationCandidateStudy,
		pkg,
	)
	if candidate.Status != AgentCompilationCandidateBlocked ||
		len(candidate.Issues) != 1 {
		t.Fatalf("candidate = %#v", candidate)
	}
	message := candidate.Issues[0].Message
	if strings.Contains(message, root) ||
		strings.Contains(strings.ToLower(message), "open ") {
		t.Fatalf("candidate issue leaked storage internals: %q", message)
	}
}

func TestCompileAgentPackagesEvidenceAutomaticallySelectsRelatedSupport(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		primaryStatement string
		supportStatement string
	}{
		{
			name:             "shared assertion",
			primaryStatement: "治疗能降低风险",
			supportStatement: "治疗能降低风险",
		},
		{
			name:             "explicit conflict",
			primaryStatement: "治疗能降低风险",
			supportStatement: "治疗不能降低风险",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			primary := agentCompilerTestRelease(
				"release-primary",
				"book-primary",
				"2026-07-26T10:00:00Z",
				testCase.primaryStatement,
				"Publisher Primary",
				"dedao_ebook",
			)
			support := agentCompilerTestRelease(
				"release-support",
				"book-support",
				"2026-07-26T11:00:00Z",
				testCase.supportStatement,
				"Publisher Support",
				"wechat_mp_article",
			)
			unrelated := agentCompilerTestRelease(
				"release-unrelated",
				"book-unrelated",
				"2026-07-26T12:00:00Z",
				"完全无关的结论",
				"Publisher Unrelated",
				"dedao_course_article",
			)
			saveKnowledgeAssemblyRelease(t, store, primary)
			saveKnowledgeAssemblyRelease(t, store, support)
			saveKnowledgeAssemblyRelease(t, store, unrelated)

			result, err := CompileAgentPackages(store, AgentCompilationRequest{
				SchemaVersion:    AgentCompilationRequestSchemaVersion,
				Mode:             AgentCompilationModeEvidence,
				PrimaryReleaseID: primary.ReleaseID,
				Version:          "2.0.0",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != AgentCompilationStatusReady ||
				result.Candidates[0].Package == nil ||
				len(result.Candidates[0].Package.Releases) != 2 {
				t.Fatalf("automatic evidence compilation = %#v", result)
			}
			releaseIDs := []string{
				result.Candidates[0].Package.Releases[0].ReleaseID,
				result.Candidates[0].Package.Releases[1].ReleaseID,
			}
			if !reflect.DeepEqual(releaseIDs, []string{primary.ReleaseID, support.ReleaseID}) {
				t.Fatalf("automatic release selection = %#v", releaseIDs)
			}
			if !reflect.DeepEqual(
				result.ReleaseIDs,
				[]string{primary.ReleaseID, support.ReleaseID},
			) {
				t.Fatalf("compilation release IDs = %#v", result.ReleaseIDs)
			}
		})
	}
}

func TestCompileAgentPackagesEvidenceDoesNotAutomaticallySelectUnrelatedSupport(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"主要结论",
		"Publisher Primary",
		"dedao_ebook",
	)
	unrelated := agentCompilerTestRelease(
		"release-unrelated",
		"book-unrelated",
		"2026-07-26T11:00:00Z",
		"完全无关的结论",
		"Publisher Unrelated",
		"wechat_mp_article",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)
	saveKnowledgeAssemblyRelease(t, store, unrelated)

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeEvidence,
		PrimaryReleaseID: primary.ReleaseID,
		Version:          "2.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusBlocked ||
		result.Candidates[0].Issues[0].Code != AgentCompilationIssueSupportingReleaseRequired {
		t.Fatalf("unrelated evidence compilation = %#v", result)
	}
}

func TestCompileAgentPackagesAutomaticSupportUsesNewestBoundedWindowInLargeCatalog(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T11:59:00Z",
		"共同结论",
		"Publisher Primary",
		"dedao_ebook",
	)
	support := agentCompilerTestRelease(
		"release-support",
		"book-support",
		"2026-07-26T12:00:00Z",
		"共同结论",
		"Publisher Support",
		"wechat_mp_article",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)
	saveKnowledgeAssemblyRelease(t, store, support)

	records := []KnowledgeReleaseRecord{
		knowledgeReleaseRecordForTest(primary),
		knowledgeReleaseRecordForTest(support),
	}
	base := time.Date(2026, 7, 26, 11, 58, 0, 0, time.UTC)
	for index := 0; index < agentCompilationMaxDiscoveryReleases-1; index++ {
		records = append(records, KnowledgeReleaseRecord{
			ReleaseID:   fmt.Sprintf("release-unavailable-%03d", index),
			BookID:      fmt.Sprintf("book-unavailable-%03d", index),
			ContentHash: sha256Fingerprint([]byte(fmt.Sprintf("unavailable-%03d", index))),
			UsagePolicy: BookUsageStandard,
			CreatedAt: base.Add(
				-time.Duration(index) * time.Minute,
			).Format(time.RFC3339),
		})
	}
	payload, err := encodeJSONFile(KnowledgeReleaseManifest{
		Version:   knowledgeReleaseVersion,
		UpdatedAt: support.CreatedAt,
		Releases:  records,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomically(store.KnowledgeReleaseManifestPath(), payload); err != nil {
		t.Fatal(err)
	}

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeEvidence,
		PrimaryReleaseID: primary.ReleaseID,
		Version:          "2.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusReady ||
		result.Candidates[0].Package == nil ||
		len(result.Candidates[0].Package.Releases) != 2 ||
		result.Candidates[0].Package.Releases[1].ReleaseID != support.ReleaseID {
		t.Fatalf("large catalog automatic support = %#v", result)
	}
}

func TestCompileAgentPackagesStudyBuildsSingleReleasePackage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentCompilerTestRelease(
		"release-study",
		"private-book-identity",
		"2026-07-26T10:00:00Z",
		"学习结论",
		"Publisher Study",
		"dedao_ebook",
	)
	release.Citations = append(release.Citations, BookKnowledgeCitation{
		CitationID: "unused-citation",
		BookID:     release.BookID,
		ChapterID:  "unused-chapter",
		ChunkID:    "unused-chunk",
	})
	saveKnowledgeAssemblyRelease(t, store, release)

	first, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeStudy,
		PrimaryReleaseID: release.ReleaseID,
		Version:          "1.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeStudy,
		PrimaryReleaseID: release.ReleaseID,
		Version:          "1.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != AgentCompilationStatusReady ||
		len(first.Candidates) != 1 ||
		first.Candidates[0].Package == nil {
		t.Fatalf("study compilation = %#v", first)
	}
	pkg := first.Candidates[0].Package
	if pkg.SchemaVersion != AgentPackageSchemaVersionV1 ||
		len(pkg.Releases) != 1 ||
		pkg.Releases[0].ReleaseID != release.ReleaseID ||
		!reflect.DeepEqual(
			pkg.Releases[0].CitationIDs,
			release.Analysis.Claims[0].CitationIDs,
		) {
		t.Fatalf("study release pin = %#v", pkg.Releases)
	}
	if strings.Contains(pkg.PackageID, release.BookID) ||
		!strings.HasSuffix(pkg.PackageID, "-study") ||
		pkg.PackageID != second.Candidates[0].Package.PackageID ||
		pkg.ContentHash != second.Candidates[0].Package.ContentHash {
		t.Fatalf("study package identity = %q hash=%q", pkg.PackageID, pkg.ContentHash)
	}
	if !reflect.DeepEqual(pkg.RetrievalPolicy.AllowedSourceTypes, []string{"dedao_ebook"}) ||
		!reflect.DeepEqual(pkg.ModelPolicy.Fallbacks, []string{"qwen3.7-max"}) {
		t.Fatalf(
			"study retrieval/model policy = %#v %#v",
			pkg.RetrievalPolicy,
			pkg.ModelPolicy,
		)
	}
	toolIDs := make([]string, 0, len(pkg.ToolPolicy.Tools))
	for _, rule := range pkg.ToolPolicy.Tools {
		if rule.Decision != AgentToolAllow {
			t.Fatalf("study tool is not read-only allow: %#v", rule)
		}
		toolIDs = append(toolIDs, rule.MCPServer+"/"+rule.ToolName)
	}
	if !reflect.DeepEqual(toolIDs, AgentReadOnlyToolIDs()) {
		t.Fatalf("study tool IDs = %#v", toolIDs)
	}
	if pkg.CreatedAt != "" || pkg.PublishedAt != "" {
		t.Fatalf("study candidate contains operational timestamps: %#v", pkg)
	}
}

func TestCompileAgentPackagesStudyBlocksMissingCitations(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := agentCompilerTestRelease(
		"release-study",
		"book-study",
		"2026-07-26T10:00:00Z",
		"没有引用的结论",
		"Publisher Study",
		"dedao_ebook",
	)
	release.Analysis.Claims = nil
	saveKnowledgeAssemblyRelease(t, store, release)

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeStudy,
		PrimaryReleaseID: release.ReleaseID,
		Version:          "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusBlocked ||
		result.Candidates[0].Issues[0].Code != AgentCompilationIssueMissingCitations ||
		!reflect.DeepEqual(
			result.Candidates[0].NextActions,
			[]string{AgentCompilationNextActionRepairEvidence},
		) {
		t.Fatalf("missing citations compilation = %#v", result)
	}
}

func TestCompileAgentPackagesStudyBlocksSupersededRelease(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldRelease := agentCompilerTestRelease(
		"release-old",
		"book-study",
		"2026-07-25T10:00:00Z",
		"旧结论",
		"Publisher Study",
		"dedao_ebook",
	)
	newRelease := agentCompilerTestRelease(
		"release-new",
		"book-study",
		"2026-07-26T10:00:00Z",
		"新结论",
		"Publisher Study",
		"dedao_ebook",
	)
	newRelease.Supersedes = oldRelease.ReleaseID
	saveKnowledgeAssemblyRelease(t, store, oldRelease)
	saveKnowledgeAssemblyRelease(t, store, newRelease)

	result, err := CompileAgentPackages(store, AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeStudy,
		PrimaryReleaseID: oldRelease.ReleaseID,
		Version:          "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AgentCompilationStatusBlocked ||
		result.Candidates[0].Issues[0].Code != AgentCompilationIssueReleaseNotInAssembly ||
		!reflect.DeepEqual(
			result.Candidates[0].NextActions,
			[]string{AgentCompilationNextActionSelectLatestRelease},
		) {
		t.Fatalf("superseded study compilation = %#v", result)
	}
}
