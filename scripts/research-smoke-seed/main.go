package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

func main() {
	if len(os.Args) != 3 {
		fail(fmt.Errorf("usage: research-smoke-seed STORE_ROOT FIXTURE_PATH"))
	}
	store := app.NewBookKnowledgeStore(os.Args[1])
	release, err := seedKnowledgeRelease(store)
	if err != nil {
		fail(err)
	}
	pkg, err := seedResearchPackage(store, *release, os.Args[2])
	if err != nil {
		fail(err)
	}
	fmt.Printf("%s\n%s\n", pkg.PackageID, pkg.Version)
}

func seedKnowledgeRelease(store *app.BookKnowledgeStore) (*app.KnowledgeRelease, error) {
	book := app.BookKnowledgeBook{
		BookID: "research-smoke-book", Title: "Synthetic Research Smoke Knowledge",
		Author: "KBase Smoke", SourceType: "dedao_ebook", SourceKey: "research-smoke-book",
	}
	pkg, err := app.ExtractBookKnowledgeFromHTML(book, `<h1>Synthetic evidence</h1><p>The synthetic timeline contains a bounded observation and a documented conflict.</p>`)
	if err != nil {
		return nil, err
	}
	if err := store.SavePackage(*pkg); err != nil {
		return nil, err
	}
	pkg, err = store.LoadPackage(book.BookID)
	if err != nil {
		return nil, err
	}
	if len(pkg.Citations) == 0 {
		return nil, fmt.Errorf("synthetic package produced no citations")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	manifest := app.BookAnalysisManifest{
		Version: "1", BookID: pkg.Book.BookID, ContentHash: pkg.Book.ContentHash,
		Status: app.BookAnalysisReady, Model: "deterministic-smoke", PromptVersion: "research-smoke.v1",
		Payload: &app.BookAnalysisPayload{
			Summary: "Synthetic bounded research evidence.",
			Claims: []app.BookAnalysisClaim{{
				ID: "research-smoke-claim", Statement: "The synthetic timeline contains a bounded observation and a documented conflict.",
				CitationIDs: []string{pkg.Citations[0].CitationID}, Confidence: 1, RiskLevel: "low",
			}},
		},
		CreatedAt: now, UpdatedAt: now, CompletedAt: now,
	}
	if err := store.SaveAnalysisManifest(manifest); err != nil {
		return nil, err
	}
	quality, err := app.EvaluateBookAnalysisQuality(store, pkg.Book.BookID)
	if err != nil {
		return nil, err
	}
	if quality.Decision != app.BookQualityPass {
		failedRules := make([]string, 0)
		for _, rule := range quality.Rules {
			if !rule.Passed {
				failedRules = append(failedRules, rule.ID)
			}
		}
		return nil, fmt.Errorf("synthetic knowledge quality decision is %q: %s", quality.Decision, strings.Join(failedRules, ","))
	}
	return app.PublishKnowledgeRelease(store, pkg.Book.BookID)
}

func seedResearchPackage(store *app.BookKnowledgeStore, release app.KnowledgeRelease, fixturePath string) (*app.AgentPackage, error) {
	citationIDs := make([]string, 0, len(release.Citations))
	for _, citation := range release.Citations {
		citationIDs = append(citationIDs, citation.CitationID)
	}
	tools := []app.AgentPackageToolRule{
		{MCPServer: "book-mcp", ToolName: "agent.search", Decision: app.AgentToolAllow},
		{MCPServer: "book-mcp", ToolName: "agent.resolve_citation", Decision: app.AgentToolAllow},
	}
	for _, toolID := range app.ResearchAgentToolIDs() {
		server, name, _ := strings.Cut(toolID, "/")
		tools = append(tools, app.AgentPackageToolRule{MCPServer: server, ToolName: name, Decision: app.AgentToolAllow})
	}
	minimumScores := make(map[string]float64, len(app.ResearchEvaluationMetricNames()))
	for _, metric := range app.ResearchEvaluationMetricNames() {
		minimumScores[metric] = 1
	}
	pkg, err := app.FinalizeAgentPackage(app.AgentPackage{
		SchemaVersion: app.AgentPackageSchemaVersionV4,
		PackageID:     "research-smoke-agent", Version: "1.0.0", LifecycleState: app.AgentPackageDraft,
		Releases: []app.AgentPackageReleaseRef{{
			ReleaseID: release.ReleaseID, ContentHash: release.ContentHash, CitationIDs: citationIDs,
		}},
		RetrievalPolicy: app.AgentPackageRetrievalPolicy{
			Strategy: "lexical", AllowedSourceTypes: []string{release.Book.SourceType}, RequireCitations: true, MaxContextChunks: 8,
		},
		ModelPolicy: app.AgentPackageModelPolicy{
			PreferredCapability: "reasoning", Fallbacks: []string{"smoke-synthesizer"}, MaxCostUSD: 10, TimeoutMS: 30000,
		},
		PromptProfiles: []app.AgentPackagePromptProfile{{ProfileID: "grounded-answer.v1", OutputSchema: "grounded-answer.v1"}},
		ToolPolicy:     app.AgentPackageToolPolicy{Tools: tools},
		SafetyPolicy: app.AgentPackageSafetyPolicy{
			UsagePolicy: app.BookUsageStandard, AbstentionReasons: []string{"insufficient_evidence", "outside_scope"}, EscalationTarget: "human_review",
		},
		EvaluationPolicy: app.AgentPackageEvaluationPolicy{SuiteVersion: app.ResearchEvaluationSuiteVersion, MinimumScores: minimumScores},
		ResearchPolicy: &app.AgentPackageResearchPolicy{
			Modes:          []string{app.ResearchModeAuto, app.ResearchModeQuick, app.ResearchModeDeep},
			AllowedSources: []string{app.ResearchSourceKnowledge, app.ResearchSourceChatlog, app.ResearchSourcePriorRuns},
			AllowedTools:   app.ResearchAgentToolIDs(), MaxIterations: 8, MaxEvidenceItems: 300,
			MaxQuotedChars: 80000, MaxCostUSD: 10, RequireVerification: true,
		},
		UIManifest: app.AgentPackageUIManifest{Capabilities: []string{"reader", "search", "grounded_chat", "evidence", "deep_research"}},
	})
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Clean(fixturePath))
	if err != nil {
		return nil, err
	}
	var submitted app.AgentEvaluationSuite
	if err := json.Unmarshal(raw, &submitted); err != nil {
		return nil, err
	}
	trusted := submitted
	trusted.ResearchCases = append([]app.ResearchEvaluationCase(nil), submitted.ResearchCases...)
	for index := range trusted.ResearchCases {
		trusted.ResearchCases[index].Observed = app.ResearchEvaluationObservation{}
	}
	if err := store.SaveTrustedAgentEvaluationSuite(pkg, trusted); err != nil {
		return nil, err
	}
	resolved, report, err := app.EvaluateAgentPackageAgainstTrustedSuite(store, pkg, submitted, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !report.Passed {
		return nil, fmt.Errorf("synthetic Research evaluation failed: %v", report.Failures)
	}
	if err := store.SaveAgentPackageEvaluation(pkg, resolved, report); err != nil {
		return nil, err
	}
	published, _, err := app.PublishAgentPackage(store, pkg, "research-smoke-publish", app.AgentPackageKnownToolIDs(), time.Now().UTC())
	return published, err
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
