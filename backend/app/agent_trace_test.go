package app

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAgentTracePersistsVersionedRuntimeEvidenceWithoutPrivateInputs(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	trace := agentTraceTestTrace()
	trace.Credentials = "transient-credential-marker"
	trace.SourceBodies = []string{"licensed-source-body-marker"}
	trace.PrivatePrompt = "private-prompt-marker"
	trace.ConsumerUserID = "consumer-user-marker"

	if err := store.SaveAgentTrace(trace); err != nil {
		t.Fatalf("SaveAgentTrace returned error: %v", err)
	}
	payload, err := os.ReadFile(store.AgentTracePath(trace.TraceID))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"transient-credential-marker",
		"licensed-source-body-marker",
		"private-prompt-marker",
		"consumer-user-marker",
	} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("persisted trace leaked %q: %s", forbidden, payload)
		}
	}
	loaded, err := store.LoadAgentTrace(trace.TraceID)
	if err != nil {
		t.Fatalf("LoadAgentTrace returned error: %v", err)
	}
	if loaded.Package.ContentHash != trace.Package.ContentHash || loaded.Releases[0].Version != "1" {
		t.Fatalf("loaded provenance = %#v", loaded)
	}
	if loaded.Retrievals[0].EvidenceID != "chunk-1" || loaded.ModelRoute.Model != "grounded-model" {
		t.Fatalf("loaded runtime trace = %#v", loaded)
	}
	if loaded.RetrievalRoute.EmbeddingIdentity != agentPackageSemanticEmbedderIdentity(validAgentPackage().RetrievalPolicy) ||
		loaded.RetrievalRoute.RerankerVersion != AgentSemanticRerankerVersion {
		t.Fatalf("loaded retrieval provenance = %#v", loaded.RetrievalRoute)
	}
	if loaded.ToolCalls[0].PolicyDecision != AgentToolAllow || loaded.Final.Citations[0].CitationID != "citation-1" {
		t.Fatalf("loaded policy/final trace = %#v", loaded)
	}
	if err := store.SaveAgentTrace(trace); err != nil {
		t.Fatalf("idempotent SaveAgentTrace returned error: %v", err)
	}
	changed := trace
	changed.Final.ResponseFingerprint = "sha256:" + strings.Repeat("9", 64)
	if err := store.SaveAgentTrace(changed); err == nil || !strings.Contains(err.Error(), "trace_id already exists") {
		t.Fatalf("changed trace SaveAgentTrace error = %v", err)
	}
}

func TestAgentTraceCollectionRuntimeRequiresPinnedMemberProvenance(t *testing.T) {
	trace := agentTraceTestTrace()
	trace.Releases[0].CollectionID = "wechat-account-fixture"
	trace.Retrievals[0].MemberBookID = "book-a"
	trace.Retrievals[0].MemberContentHash = "sha256:" + strings.Repeat("a", 64)
	trace.Retrievals[0].ChunkID = "book-a-chunk"
	if err := ValidateAgentTrace(trace); err != nil {
		t.Fatalf("valid collection trace: %v", err)
	}
	trace.Retrievals[0].MemberContentHash = ""
	if err := ValidateAgentTrace(trace); err == nil || !strings.Contains(err.Error(), "member provenance") {
		t.Fatalf("missing member hash error=%v", err)
	}
}

func TestAgentTraceRejectsIncompleteOrUnsafeRuntimeRecords(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*AgentTrace)
		message string
	}{
		{
			name: "release version",
			mutate: func(trace *AgentTrace) {
				trace.Releases[0].Version = ""
			},
			message: "release version",
		},
		{
			name: "retrieval scope",
			mutate: func(trace *AgentTrace) {
				trace.Retrievals[0].ReleaseID = "release-other"
			},
			message: "retrieval release",
		},
		{
			name: "blocked execution",
			mutate: func(trace *AgentTrace) {
				trace.ToolCalls[0].PolicyDecision = AgentToolBlock
				trace.ToolCalls[0].Outcome = AgentToolOutcomeSucceeded
			},
			message: "blocked tool",
		},
		{
			name: "citation evidence",
			mutate: func(trace *AgentTrace) {
				trace.Final.Citations[0].EvidenceID = "chunk-missing"
			},
			message: "citation evidence",
		},
		{
			name: "raw fingerprint value",
			mutate: func(trace *AgentTrace) {
				trace.ToolCalls[0].ArgumentFingerprint = "private prompt copied here"
			},
			message: "sha256",
		},
		{
			name: "completed without grounding",
			mutate: func(trace *AgentTrace) {
				trace.Retrievals = nil
				trace.Final.Citations = nil
			},
			message: "completed trace requires grounded evidence",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			trace := agentTraceTestTrace()
			test.mutate(&trace)
			if err := ValidateAgentTrace(trace); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("ValidateAgentTrace error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestAgentTraceEvidenceAuditPersistsOnlyBoundedIdentity(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	trace := agentTraceTestTrace()
	trace.EvidenceAudit = &AgentTraceEvidenceAuditRef{
		AuditID:   "audit-test",
		InputHash: "sha256:" + strings.Repeat("7", 64),
	}
	trace.Observability = validEvidenceAuditTraceObservability()
	trace.PrivatePrompt = "private-evidence-audit-prompt"
	trace.SourceBodies = []string{"private-evidence-body"}
	if err := store.SaveAgentTrace(trace); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(store.AgentTracePath(trace.TraceID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"audit_id": "audit-test"`) ||
		strings.Contains(string(payload), "private-evidence-audit-prompt") ||
		strings.Contains(string(payload), "private-evidence-body") {
		t.Fatalf("evidence audit trace payload = %s", payload)
	}
	loaded, err := store.LoadAgentTrace(trace.TraceID)
	if err != nil || loaded.EvidenceAudit == nil || loaded.EvidenceAudit.AuditID != "audit-test" {
		t.Fatalf("loaded evidence audit trace = %#v err=%v", loaded, err)
	}
}

func TestEvidenceAuditTraceRejectsUnboundedOrPrivateObservability(t *testing.T) {
	trace := agentTraceTestTrace()
	trace.EvidenceAudit = &AgentTraceEvidenceAuditRef{
		AuditID: "audit-observability", InputHash: "sha256:" + strings.Repeat("7", 64),
	}
	trace.Observability = &AgentTraceObservability{
		Stages: []AgentTraceStage{{
			Name: "package_validation", Status: "completed", DurationMS: 1,
		}},
		CitationResolutionRate:            1,
		IndependentPublicationSourceCount: 1,
		Usage:                             AgentTraceUsage{Status: "unknown"},
	}
	trace.Observability.AbstentionReason = strings.Repeat("private-prompt-marker", 100)
	if err := ValidateAgentTrace(trace); err == nil || !strings.Contains(err.Error(), "abstention_reason") {
		t.Fatalf("ValidateAgentTrace error = %v", err)
	}
	trace.Observability.AbstentionReason = ""
	trace.Observability.Stages = make([]AgentTraceStage, 20)
	if err := ValidateAgentTrace(trace); err == nil || !strings.Contains(err.Error(), "stages") {
		t.Fatalf("ValidateAgentTrace error = %v", err)
	}
	trace.Observability.Stages = []AgentTraceStage{{
		Name: "package_validation", Status: "completed", DurationMS: 1,
	}}
	trace.Observability.Usage = AgentTraceUsage{
		Status: "reported", PromptTokens: -1, CompletionTokens: 1, TotalTokens: 0,
	}
	if err := ValidateAgentTrace(trace); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("ValidateAgentTrace error = %v", err)
	}
	trace.Observability.Usage = AgentTraceUsage{
		Status: "reported", PromptTokens: 5_000_001, CompletionTokens: 1,
		TotalTokens: 5_000_002, CostStatus: "unknown",
	}
	if err := ValidateAgentTrace(trace); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("ValidateAgentTrace error = %v", err)
	}
	trace.Observability.Usage = AgentTraceUsage{Status: "unknown"}
	trace.Observability.AbstentionReason = "private prompt marker"
	if err := ValidateAgentTrace(trace); err == nil || !strings.Contains(err.Error(), "abstention_reason") {
		t.Fatalf("ValidateAgentTrace error = %v", err)
	}
}

func TestEvidenceAuditTraceTerminalStrictlyBindsAuditReportAndTraceIdentity(t *testing.T) {
	trace := agentTraceTestTrace()
	inputHash := "sha256:" + strings.Repeat("9", 64)
	trace.EvidenceAudit = &AgentTraceEvidenceAuditRef{AuditID: "audit-strict-binding", InputHash: inputHash}
	trace.Observability = validEvidenceAuditTraceObservability()
	trace.Retrievals[0].EvidenceID = "release-1:claim-1:citation-1"
	trace.Final.Citations[0].EvidenceID = trace.Retrievals[0].EvidenceID
	report := EvidenceAudit{
		AuditID:   "audit-strict-binding",
		InputHash: inputHash,
		Package: EvidenceAuditPackageRef{
			PackageID: trace.Package.PackageID, Version: trace.Package.Version,
			ContentHash: trace.Package.ContentHash,
		},
		TraceID: trace.TraceID,
		ClaimAudits: []EvidenceAuditClaim{{
			SourceClaim: "claim", NormalizedStatement: "claim",
			Verdict: EvidenceAuditVerdictSupported,
			Evidence: []EvidenceAuditEvidenceRef{{
				ReleaseID: "release-1", ClaimID: "claim-1", CitationID: "citation-1",
			}},
		}},
		Summary: EvidenceAuditSummary{Conclusion: "bounded"},
		Proofroom: EvidenceAuditProofroomProjection{
			SchemaVersion: "proofroom-evidence-task.v1", Title: "review",
		},
	}
	fingerprint, err := evidenceAuditReportFingerprint(report)
	if err != nil {
		t.Fatal(err)
	}
	trace.Final.ResponseFingerprint = fingerprint
	terminal := evidenceAuditTraceTerminal{
		Version: evidenceAuditTraceTerminalVersion, AuditID: report.AuditID,
		InputHash: inputHash, TraceID: trace.TraceID, ReportFingerprint: fingerprint,
		Report: &report, Trace: trace,
	}
	if err := validateEvidenceAuditTraceTerminal(terminal); err != nil {
		t.Fatalf("valid terminal rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*evidenceAuditTraceTerminal)
	}{
		{
			name: "report input hash",
			mutate: func(value *evidenceAuditTraceTerminal) {
				value.Report.InputHash = "sha256:" + strings.Repeat("7", 64)
			},
		},
		{
			name: "report package",
			mutate: func(value *evidenceAuditTraceTerminal) {
				value.Report.Package.PackageID = "other-package"
			},
		},
		{
			name: "trace audit",
			mutate: func(value *evidenceAuditTraceTerminal) {
				value.Trace.EvidenceAudit.AuditID = "other-audit"
			},
		},
		{
			name: "trace final citations",
			mutate: func(value *evidenceAuditTraceTerminal) {
				value.Trace.Final.Citations[0].EvidenceID = "release-1:other-claim:citation-1"
				value.Trace.Retrievals[0].EvidenceID = value.Trace.Final.Citations[0].EvidenceID
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := terminal
			reportCopy := *terminal.Report
			changed.Report = &reportCopy
			traceCopy := terminal.Trace
			auditRefCopy := *terminal.Trace.EvidenceAudit
			traceCopy.EvidenceAudit = &auditRefCopy
			changed.Trace = traceCopy
			tt.mutate(&changed)
			if err := validateEvidenceAuditTraceTerminal(changed); err == nil {
				t.Fatal("mutated terminal unexpectedly validated")
			}
		})
	}
}

func validEvidenceAuditTraceObservability() *AgentTraceObservability {
	return &AgentTraceObservability{
		Stages: []AgentTraceStage{{
			Name: "package_validation", Status: "completed", DurationMS: 1,
		}},
		CitationResolutionRate: 1,
		Usage:                  AgentTraceUsage{Status: "unknown"},
	}
}

func TestReplayAgentTraceIsDeterministicOverStoredEvidenceAndMockResults(t *testing.T) {
	trace := agentTraceTestTrace()
	fixture := AgentReplayFixture{
		Evidence: []AgentReplayEvidence{{EvidenceID: "chunk-1", ContentHash: "sha256:" + strings.Repeat("5", 64)}},
		Model: AgentReplayModelResult{
			OutputHash: "sha256:" + strings.Repeat("8", 64),
			Citations:  trace.Final.Citations,
		},
		Tools: []AgentReplayToolResult{{
			CallID:     "tool-call-1",
			Outcome:    AgentToolOutcomeSucceeded,
			ResultHash: "sha256:" + strings.Repeat("4", 64),
		}},
	}

	first, err := ReplayAgentTrace(trace, fixture)
	if err != nil {
		t.Fatalf("ReplayAgentTrace returned error: %v", err)
	}
	second, err := ReplayAgentTrace(trace, fixture)
	if err != nil {
		t.Fatalf("second ReplayAgentTrace returned error: %v", err)
	}
	if !reflect.DeepEqual(first, second) || !first.MatchesOriginal {
		t.Fatalf("replay is not deterministic/matching: first=%#v second=%#v", first, second)
	}
	if first.InputHash == "" || first.EvidenceIDs[0] != "chunk-1" || first.Citations[0].CitationID != "citation-1" {
		t.Fatalf("replay result = %#v", first)
	}

	fixture.Evidence = nil
	if _, err := ReplayAgentTrace(trace, fixture); err == nil || !strings.Contains(err.Error(), "stored evidence") {
		t.Fatalf("missing evidence replay error = %v", err)
	}
	fixture = AgentReplayFixture{
		Evidence: []AgentReplayEvidence{{EvidenceID: "chunk-1", ContentHash: "sha256:" + strings.Repeat("5", 64)}},
		Model:    AgentReplayModelResult{OutputHash: trace.Final.ResponseFingerprint, Citations: trace.Final.Citations},
		Tools:    []AgentReplayToolResult{{CallID: "tool-call-1", Outcome: AgentToolOutcomeSucceeded, ResultHash: "raw tool output"}},
	}
	if _, err := ReplayAgentTrace(trace, fixture); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("raw tool result hash replay error = %v", err)
	}
}

func TestExportAgentTraceOTLPUsesAllowlistedPhoenixCompatibleSpans(t *testing.T) {
	trace := agentTraceTestTrace()
	trace.Credentials = "transient-credential-marker"
	trace.SourceBodies = []string{"licensed-source-body-marker"}
	trace.PrivatePrompt = "private-prompt-marker"
	trace.ConsumerUserID = "consumer-user-marker"

	envelope, err := ExportAgentTraceOTLP(trace)
	if err != nil {
		t.Fatalf("ExportAgentTraceOTLP returned error: %v", err)
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"resourceSpans"`) ||
		!strings.Contains(string(payload), `"kbase.agent.run"`) ||
		!strings.Contains(string(payload), `"kbase.agent.tool"`) {
		t.Fatalf("OTLP envelope missing spans: %s", payload)
	}
	for _, forbidden := range []string{
		"transient-credential-marker",
		"licensed-source-body-marker",
		"private-prompt-marker",
		"consumer-user-marker",
	} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("OTLP export leaked %q: %s", forbidden, payload)
		}
	}
}

func TestAgentTraceJSONSchemaDeclaresBoundedObservableContract(t *testing.T) {
	payload, err := os.ReadFile("../../contracts/agent-trace-v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, required := range []string{
		`"agent-trace.v1"`,
		`"package"`,
		`"releases"`,
		`"retrieval_route"`,
		`"retrievals"`,
		`"model_route"`,
		`"tool_calls"`,
		`"final"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("trace schema missing %s", required)
		}
	}
	for _, forbidden := range []string{"source_body", "private_prompt", "consumer_user_id", "credentials"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("trace schema exposes forbidden field %q", forbidden)
		}
	}
}

func agentTraceTestTrace() AgentTrace {
	return AgentTrace{
		SchemaVersion: AgentTraceSchemaVersion,
		TraceID:       "trace-example-1",
		Package: AgentTracePackageRef{
			PackageID:   "agent-package-example",
			Version:     "1.0.0",
			ContentHash: "sha256:" + strings.Repeat("1", 64),
		},
		Releases: []AgentTraceReleaseRef{{
			ReleaseID:   "release-1",
			Version:     "1",
			ContentHash: "sha256:" + strings.Repeat("2", 64),
		}},
		Retrievals: []AgentTraceRetrieval{{
			EvidenceID: "chunk-1",
			ReleaseID:  "release-1",
			Score:      0.91,
			Rank:       1,
		}},
		RetrievalRoute: AgentTraceRetrievalRoute{
			Strategy: "hybrid", EmbeddingIdentity: agentPackageSemanticEmbedderIdentity(validAgentPackage().RetrievalPolicy),
			RerankerVersion: AgentSemanticRerankerVersion,
		},
		ModelRoute: AgentTraceModelRoute{
			Provider:   "tokenplan-compatible",
			Model:      "grounded-model",
			Capability: "grounded_reasoning",
		},
		ToolCalls: []AgentTraceToolCall{{
			CallID:              "tool-call-1",
			MCPServer:           "book-kbase",
			ToolName:            "search_package",
			ArgumentFingerprint: "sha256:" + strings.Repeat("3", 64),
			PolicyDecision:      AgentToolAllow,
			Outcome:             AgentToolOutcomeSucceeded,
			ResultFingerprint:   "sha256:" + strings.Repeat("4", 64),
		}},
		Final: AgentTraceFinal{
			Outcome:             AgentTraceOutcomeCompleted,
			ResponseFingerprint: "sha256:" + strings.Repeat("8", 64),
			Citations: []AgentTraceCitation{{
				CitationID: "citation-1",
				ReleaseID:  "release-1",
				EvidenceID: "chunk-1",
			}},
		},
		StartedAt:   "2026-07-19T12:00:00Z",
		CompletedAt: "2026-07-19T12:00:01Z",
	}
}
