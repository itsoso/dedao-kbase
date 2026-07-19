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
