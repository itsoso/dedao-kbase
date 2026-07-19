package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	AgentTraceSchemaVersion = "agent-trace.v1"

	AgentToolOutcomeNotExecuted          = "not_executed"
	AgentToolOutcomeSucceeded            = "succeeded"
	AgentToolOutcomeFailed               = "failed"
	AgentToolOutcomeBlocked              = "blocked"
	AgentToolOutcomeConfirmationRequired = "confirmation_required"

	AgentTraceOutcomeCompleted = "completed"
	AgentTraceOutcomeAbstained = "abstained"
	AgentTraceOutcomeFailed    = "failed"
)

type AgentTrace struct {
	SchemaVersion string                 `json:"schema_version"`
	TraceID       string                 `json:"trace_id"`
	Package       AgentTracePackageRef   `json:"package"`
	Releases      []AgentTraceReleaseRef `json:"releases"`
	Retrievals    []AgentTraceRetrieval  `json:"retrievals"`
	ModelRoute    AgentTraceModelRoute   `json:"model_route"`
	ToolCalls     []AgentTraceToolCall   `json:"tool_calls"`
	Final         AgentTraceFinal        `json:"final"`
	StartedAt     string                 `json:"started_at"`
	CompletedAt   string                 `json:"completed_at"`

	Credentials    string   `json:"-"`
	SourceBodies   []string `json:"-"`
	PrivatePrompt  string   `json:"-"`
	ConsumerUserID string   `json:"-"`
}

type AgentTracePackageRef struct {
	PackageID   string `json:"package_id"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
}

type AgentTraceReleaseRef struct {
	ReleaseID   string `json:"release_id"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
}

type AgentTraceRetrieval struct {
	EvidenceID string  `json:"evidence_id"`
	ReleaseID  string  `json:"release_id"`
	Score      float64 `json:"score"`
	Rank       int     `json:"rank"`
}

type AgentTraceModelRoute struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Capability string `json:"capability"`
}

type AgentTraceToolCall struct {
	CallID              string `json:"call_id"`
	MCPServer           string `json:"mcp_server"`
	ToolName            string `json:"tool_name"`
	ArgumentFingerprint string `json:"argument_fingerprint"`
	PolicyDecision      string `json:"policy_decision"`
	Outcome             string `json:"outcome"`
	ResultFingerprint   string `json:"result_fingerprint,omitempty"`
}

type AgentTraceFinal struct {
	Outcome             string               `json:"outcome"`
	ResponseFingerprint string               `json:"response_fingerprint"`
	Citations           []AgentTraceCitation `json:"citations"`
}

type AgentTraceCitation struct {
	CitationID string `json:"citation_id"`
	ReleaseID  string `json:"release_id"`
	EvidenceID string `json:"evidence_id"`
}

type AgentReplayFixture struct {
	Evidence []AgentReplayEvidence   `json:"evidence"`
	Model    AgentReplayModelResult  `json:"model"`
	Tools    []AgentReplayToolResult `json:"tools"`
}

type AgentReplayEvidence struct {
	EvidenceID  string `json:"evidence_id"`
	ContentHash string `json:"content_hash"`
}

type AgentReplayModelResult struct {
	OutputHash string               `json:"output_hash"`
	Citations  []AgentTraceCitation `json:"citations"`
}

type AgentReplayToolResult struct {
	CallID     string `json:"call_id"`
	Outcome    string `json:"outcome"`
	ResultHash string `json:"result_hash,omitempty"`
}

type AgentReplayResult struct {
	TraceID         string                  `json:"trace_id"`
	InputHash       string                  `json:"input_hash"`
	EvidenceIDs     []string                `json:"evidence_ids"`
	ToolOutcomes    []AgentReplayToolResult `json:"tool_outcomes"`
	Citations       []AgentTraceCitation    `json:"citations"`
	MatchesOriginal bool                    `json:"matches_original"`
}

func ValidateAgentTrace(trace AgentTrace) error {
	if trace.SchemaVersion != AgentTraceSchemaVersion {
		return fmt.Errorf("schema_version must be %q", AgentTraceSchemaVersion)
	}
	if err := requireContractFields(map[string]string{
		"trace_id":                   trace.TraceID,
		"package.package_id":         trace.Package.PackageID,
		"package.version":            trace.Package.Version,
		"package.content_hash":       trace.Package.ContentHash,
		"model_route.provider":       trace.ModelRoute.Provider,
		"model_route.model":          trace.ModelRoute.Model,
		"model_route.capability":     trace.ModelRoute.Capability,
		"final.outcome":              trace.Final.Outcome,
		"final.response_fingerprint": trace.Final.ResponseFingerprint,
		"started_at":                 trace.StartedAt,
		"completed_at":               trace.CompletedAt,
	}); err != nil {
		return err
	}
	startedAt, err := time.Parse(time.RFC3339Nano, trace.StartedAt)
	if err != nil {
		return fmt.Errorf("started_at must be RFC3339: %w", err)
	}
	completedAt, err := time.Parse(time.RFC3339Nano, trace.CompletedAt)
	if err != nil {
		return fmt.Errorf("completed_at must be RFC3339: %w", err)
	}
	if completedAt.Before(startedAt) {
		return fmt.Errorf("completed_at must not precede started_at")
	}
	if len(trace.Releases) == 0 {
		return fmt.Errorf("releases is required")
	}
	releases := make(map[string]AgentTraceReleaseRef, len(trace.Releases))
	for index, release := range trace.Releases {
		if strings.TrimSpace(release.ReleaseID) == "" || strings.TrimSpace(release.ContentHash) == "" {
			return fmt.Errorf("releases[%d] requires release_id and content_hash", index)
		}
		if strings.TrimSpace(release.Version) == "" {
			return fmt.Errorf("release version is required for %q", release.ReleaseID)
		}
		if _, exists := releases[release.ReleaseID]; exists {
			return fmt.Errorf("duplicate release %q", release.ReleaseID)
		}
		releases[release.ReleaseID] = release
	}
	evidence := make(map[string]AgentTraceRetrieval, len(trace.Retrievals))
	for index, retrieval := range trace.Retrievals {
		if strings.TrimSpace(retrieval.EvidenceID) == "" || retrieval.Rank <= 0 || retrieval.Score < 0 {
			return fmt.Errorf("retrievals[%d] has invalid evidence_id, rank, or score", index)
		}
		if _, ok := releases[retrieval.ReleaseID]; !ok {
			return fmt.Errorf("retrieval release %q is outside trace scope", retrieval.ReleaseID)
		}
		if _, exists := evidence[retrieval.EvidenceID]; exists {
			return fmt.Errorf("duplicate retrieval evidence %q", retrieval.EvidenceID)
		}
		evidence[retrieval.EvidenceID] = retrieval
	}
	toolCalls := make(map[string]AgentTraceToolCall, len(trace.ToolCalls))
	for index, call := range trace.ToolCalls {
		if err := requireContractFields(map[string]string{
			"call_id":              call.CallID,
			"mcp_server":           call.MCPServer,
			"tool_name":            call.ToolName,
			"argument_fingerprint": call.ArgumentFingerprint,
			"policy_decision":      call.PolicyDecision,
			"outcome":              call.Outcome,
		}); err != nil {
			return fmt.Errorf("tool_calls[%d]: %w", index, err)
		}
		if _, exists := toolCalls[call.CallID]; exists {
			return fmt.Errorf("duplicate tool call %q", call.CallID)
		}
		toolCalls[call.CallID] = call
		if err := validateAgentTraceToolCall(call); err != nil {
			return err
		}
	}
	switch trace.Final.Outcome {
	case AgentTraceOutcomeCompleted, AgentTraceOutcomeAbstained, AgentTraceOutcomeFailed:
	default:
		return fmt.Errorf("unsupported final outcome %q", trace.Final.Outcome)
	}
	return validateAgentTraceCitations(trace.Final.Citations, releases, evidence)
}

func validateAgentTraceToolCall(call AgentTraceToolCall) error {
	switch call.PolicyDecision {
	case AgentToolAllow:
		if call.Outcome != AgentToolOutcomeSucceeded && call.Outcome != AgentToolOutcomeFailed {
			return fmt.Errorf("allowed tool %q has invalid outcome %q", call.CallID, call.Outcome)
		}
	case AgentToolRequireConfirmation:
		if call.Outcome != AgentToolOutcomeConfirmationRequired && call.Outcome != AgentToolOutcomeNotExecuted &&
			call.Outcome != AgentToolOutcomeSucceeded && call.Outcome != AgentToolOutcomeFailed {
			return fmt.Errorf("confirmation-required tool %q has invalid outcome %q", call.CallID, call.Outcome)
		}
	case AgentToolBlock:
		if call.Outcome != AgentToolOutcomeBlocked && call.Outcome != AgentToolOutcomeNotExecuted {
			return fmt.Errorf("blocked tool %q must not execute", call.CallID)
		}
	default:
		return fmt.Errorf("tool %q has unsupported policy decision %q", call.CallID, call.PolicyDecision)
	}
	if (call.Outcome == AgentToolOutcomeSucceeded || call.Outcome == AgentToolOutcomeFailed) &&
		strings.TrimSpace(call.ResultFingerprint) == "" {
		return fmt.Errorf("executed tool %q requires result_fingerprint", call.CallID)
	}
	return nil
}

func validateAgentTraceCitations(
	citations []AgentTraceCitation,
	releases map[string]AgentTraceReleaseRef,
	evidence map[string]AgentTraceRetrieval,
) error {
	seen := make(map[string]struct{}, len(citations))
	for index, citation := range citations {
		if strings.TrimSpace(citation.CitationID) == "" {
			return fmt.Errorf("citations[%d].citation_id is required", index)
		}
		if _, ok := releases[citation.ReleaseID]; !ok {
			return fmt.Errorf("citation release %q is outside trace scope", citation.ReleaseID)
		}
		retrieval, ok := evidence[citation.EvidenceID]
		if !ok || retrieval.ReleaseID != citation.ReleaseID {
			return fmt.Errorf("citation evidence %q is not a retrieved item for release %q", citation.EvidenceID, citation.ReleaseID)
		}
		key := citation.CitationID + "\x00" + citation.ReleaseID + "\x00" + citation.EvidenceID
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate final citation %q", citation.CitationID)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (s *BookKnowledgeStore) AgentTraceDir() string {
	return filepath.Join(s.root, "agent-traces")
}

func (s *BookKnowledgeStore) AgentTracePath(traceID string) string {
	return filepath.Join(s.AgentTraceDir(), sanitizeBookKnowledgeID(traceID)+".json")
}

func (s *BookKnowledgeStore) SaveAgentTrace(trace AgentTrace) error {
	if err := ValidateAgentTrace(trace); err != nil {
		return err
	}
	payload, err := encodeJSONFile(trace)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.AgentTraceDir(), os.ModePerm); err != nil {
		return err
	}
	existing, err := os.ReadFile(s.AgentTracePath(trace.TraceID))
	if err == nil {
		if bytes.Equal(existing, payload) {
			return nil
		}
		return fmt.Errorf("trace_id already exists with different content")
	}
	if !os.IsNotExist(err) {
		return err
	}
	return writeFileAtomically(s.AgentTracePath(trace.TraceID), payload)
}

func (s *BookKnowledgeStore) LoadAgentTrace(traceID string) (*AgentTrace, error) {
	if strings.TrimSpace(traceID) == "" {
		return nil, fmt.Errorf("trace_id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var trace AgentTrace
	if err := readJSONFile(s.AgentTracePath(traceID), &trace); err != nil {
		return nil, err
	}
	if err := ValidateAgentTrace(trace); err != nil {
		return nil, err
	}
	return &trace, nil
}

func ReplayAgentTrace(trace AgentTrace, fixture AgentReplayFixture) (AgentReplayResult, error) {
	if err := ValidateAgentTrace(trace); err != nil {
		return AgentReplayResult{}, err
	}
	retrievals := make(map[string]AgentTraceRetrieval, len(trace.Retrievals))
	for _, retrieval := range trace.Retrievals {
		retrievals[retrieval.EvidenceID] = retrieval
	}
	if len(fixture.Evidence) != len(retrievals) {
		return AgentReplayResult{}, fmt.Errorf("stored evidence must match every retrieved evidence ID")
	}
	evidence := append([]AgentReplayEvidence(nil), fixture.Evidence...)
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].EvidenceID < evidence[j].EvidenceID })
	evidenceIDs := make([]string, 0, len(evidence))
	seenEvidence := make(map[string]struct{}, len(evidence))
	for _, item := range evidence {
		if strings.TrimSpace(item.ContentHash) == "" {
			return AgentReplayResult{}, fmt.Errorf("stored evidence %q requires content_hash", item.EvidenceID)
		}
		if _, ok := retrievals[item.EvidenceID]; !ok {
			return AgentReplayResult{}, fmt.Errorf("stored evidence %q was not retrieved", item.EvidenceID)
		}
		if _, duplicate := seenEvidence[item.EvidenceID]; duplicate {
			return AgentReplayResult{}, fmt.Errorf("duplicate stored evidence %q", item.EvidenceID)
		}
		seenEvidence[item.EvidenceID] = struct{}{}
		evidenceIDs = append(evidenceIDs, item.EvidenceID)
	}
	if strings.TrimSpace(fixture.Model.OutputHash) == "" {
		return AgentReplayResult{}, fmt.Errorf("mock model output_hash is required")
	}
	releases := make(map[string]AgentTraceReleaseRef, len(trace.Releases))
	for _, release := range trace.Releases {
		releases[release.ReleaseID] = release
	}
	if err := validateAgentTraceCitations(fixture.Model.Citations, releases, retrievals); err != nil {
		return AgentReplayResult{}, err
	}
	tools := append([]AgentReplayToolResult(nil), fixture.Tools...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].CallID < tools[j].CallID })
	if len(tools) != len(trace.ToolCalls) {
		return AgentReplayResult{}, fmt.Errorf("mock tool results must match every proposed tool call")
	}
	toolCalls := make(map[string]AgentTraceToolCall, len(trace.ToolCalls))
	for _, call := range trace.ToolCalls {
		toolCalls[call.CallID] = call
	}
	seenTools := make(map[string]struct{}, len(tools))
	toolsMatch := true
	for _, result := range tools {
		call, ok := toolCalls[result.CallID]
		if !ok {
			return AgentReplayResult{}, fmt.Errorf("mock tool result %q was not proposed", result.CallID)
		}
		if _, duplicate := seenTools[result.CallID]; duplicate {
			return AgentReplayResult{}, fmt.Errorf("duplicate mock tool result %q", result.CallID)
		}
		seenTools[result.CallID] = struct{}{}
		if call.PolicyDecision == AgentToolBlock && result.Outcome != AgentToolOutcomeBlocked && result.Outcome != AgentToolOutcomeNotExecuted {
			return AgentReplayResult{}, fmt.Errorf("blocked tool %q cannot execute during replay", result.CallID)
		}
		if result.Outcome != call.Outcome || result.ResultHash != call.ResultFingerprint {
			toolsMatch = false
		}
	}
	canonical := struct {
		PackageHash string                  `json:"package_hash"`
		Releases    []AgentTraceReleaseRef  `json:"releases"`
		Evidence    []AgentReplayEvidence   `json:"evidence"`
		ModelRoute  AgentTraceModelRoute    `json:"model_route"`
		Model       AgentReplayModelResult  `json:"model"`
		Tools       []AgentReplayToolResult `json:"tools"`
	}{trace.Package.ContentHash, trace.Releases, evidence, trace.ModelRoute, fixture.Model, tools}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return AgentReplayResult{}, err
	}
	return AgentReplayResult{
		TraceID:         trace.TraceID,
		InputHash:       sha256Fingerprint(payload),
		EvidenceIDs:     evidenceIDs,
		ToolOutcomes:    tools,
		Citations:       append([]AgentTraceCitation(nil), fixture.Model.Citations...),
		MatchesOriginal: toolsMatch && fixture.Model.OutputHash == trace.Final.ResponseFingerprint && reflect.DeepEqual(fixture.Model.Citations, trace.Final.Citations),
	}, nil
}

type AgentTraceOTLPEnvelope struct {
	ResourceSpans []AgentTraceOTLPResourceSpans `json:"resourceSpans"`
}

type AgentTraceOTLPResourceSpans struct {
	Resource   AgentTraceOTLPResource    `json:"resource"`
	ScopeSpans []AgentTraceOTLPScopeSpan `json:"scopeSpans"`
}

type AgentTraceOTLPResource struct {
	Attributes []AgentTraceOTLPAttribute `json:"attributes"`
}

type AgentTraceOTLPScopeSpan struct {
	Scope AgentTraceOTLPScope  `json:"scope"`
	Spans []AgentTraceOTLPSpan `json:"spans"`
}

type AgentTraceOTLPScope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type AgentTraceOTLPSpan struct {
	TraceID           string                    `json:"traceId"`
	SpanID            string                    `json:"spanId"`
	ParentSpanID      string                    `json:"parentSpanId,omitempty"`
	Name              string                    `json:"name"`
	Kind              int                       `json:"kind"`
	StartTimeUnixNano string                    `json:"startTimeUnixNano"`
	EndTimeUnixNano   string                    `json:"endTimeUnixNano"`
	Attributes        []AgentTraceOTLPAttribute `json:"attributes"`
	Status            AgentTraceOTLPStatus      `json:"status"`
}

type AgentTraceOTLPAttribute struct {
	Key   string                 `json:"key"`
	Value AgentTraceOTLPAnyValue `json:"value"`
}

type AgentTraceOTLPAnyValue struct {
	StringValue string  `json:"stringValue,omitempty"`
	DoubleValue float64 `json:"doubleValue,omitempty"`
	IntValue    int64   `json:"intValue,omitempty"`
}

type AgentTraceOTLPStatus struct {
	Code int `json:"code"`
}

func ExportAgentTraceOTLP(trace AgentTrace) (AgentTraceOTLPEnvelope, error) {
	if err := ValidateAgentTrace(trace); err != nil {
		return AgentTraceOTLPEnvelope{}, err
	}
	startedAt, _ := time.Parse(time.RFC3339Nano, trace.StartedAt)
	completedAt, _ := time.Parse(time.RFC3339Nano, trace.CompletedAt)
	traceID := deterministicHexID(trace.TraceID, 32)
	rootSpanID := deterministicHexID(trace.TraceID+"/root", 16)
	spans := []AgentTraceOTLPSpan{
		agentTraceOTLPSpan(traceID, rootSpanID, "", "kbase.agent.run", startedAt, completedAt, []AgentTraceOTLPAttribute{
			stringOTLPAttribute("openinference.span.kind", "CHAIN"),
			stringOTLPAttribute("kbase.agent.package.id", trace.Package.PackageID),
			stringOTLPAttribute("kbase.agent.package.version", trace.Package.Version),
			stringOTLPAttribute("kbase.agent.package.content_hash", trace.Package.ContentHash),
			stringOTLPAttribute("kbase.agent.final.outcome", trace.Final.Outcome),
			stringOTLPAttribute("kbase.agent.final.citation_ids", joinedAgentTraceCitationIDs(trace.Final.Citations)),
		}),
	}
	for index, retrieval := range trace.Retrievals {
		spans = append(spans, agentTraceOTLPSpan(
			traceID,
			deterministicHexID(fmt.Sprintf("%s/retrieval/%d", trace.TraceID, index), 16),
			rootSpanID,
			"kbase.agent.retrieval",
			startedAt,
			completedAt,
			[]AgentTraceOTLPAttribute{
				stringOTLPAttribute("openinference.span.kind", "RETRIEVER"),
				stringOTLPAttribute("kbase.agent.evidence.id", retrieval.EvidenceID),
				stringOTLPAttribute("kbase.agent.release.id", retrieval.ReleaseID),
				doubleOTLPAttribute("kbase.agent.retrieval.score", retrieval.Score),
				intOTLPAttribute("kbase.agent.retrieval.rank", int64(retrieval.Rank)),
			},
		))
	}
	spans = append(spans, agentTraceOTLPSpan(
		traceID,
		deterministicHexID(trace.TraceID+"/model", 16),
		rootSpanID,
		"kbase.agent.model",
		startedAt,
		completedAt,
		[]AgentTraceOTLPAttribute{
			stringOTLPAttribute("openinference.span.kind", "LLM"),
			stringOTLPAttribute("llm.provider", trace.ModelRoute.Provider),
			stringOTLPAttribute("llm.model_name", trace.ModelRoute.Model),
			stringOTLPAttribute("kbase.agent.model.capability", trace.ModelRoute.Capability),
		},
	))
	for index, call := range trace.ToolCalls {
		spans = append(spans, agentTraceOTLPSpan(
			traceID,
			deterministicHexID(fmt.Sprintf("%s/tool/%d", trace.TraceID, index), 16),
			rootSpanID,
			"kbase.agent.tool",
			startedAt,
			completedAt,
			[]AgentTraceOTLPAttribute{
				stringOTLPAttribute("openinference.span.kind", "TOOL"),
				stringOTLPAttribute("tool.name", call.MCPServer+"/"+call.ToolName),
				stringOTLPAttribute("kbase.agent.tool.call_id", call.CallID),
				stringOTLPAttribute("kbase.agent.tool.policy_decision", call.PolicyDecision),
				stringOTLPAttribute("kbase.agent.tool.outcome", call.Outcome),
				stringOTLPAttribute("kbase.agent.tool.argument_fingerprint", call.ArgumentFingerprint),
				stringOTLPAttribute("kbase.agent.tool.result_fingerprint", call.ResultFingerprint),
			},
		))
	}
	return AgentTraceOTLPEnvelope{ResourceSpans: []AgentTraceOTLPResourceSpans{{
		Resource: AgentTraceOTLPResource{Attributes: []AgentTraceOTLPAttribute{
			stringOTLPAttribute("service.name", "kbase-book-agent-runtime"),
		}},
		ScopeSpans: []AgentTraceOTLPScopeSpan{{
			Scope: AgentTraceOTLPScope{Name: "kbase.agent.trace", Version: AgentTraceSchemaVersion},
			Spans: spans,
		}},
	}}}, nil
}

func agentTraceOTLPSpan(
	traceID, spanID, parentSpanID, name string,
	startedAt, completedAt time.Time,
	attributes []AgentTraceOTLPAttribute,
) AgentTraceOTLPSpan {
	return AgentTraceOTLPSpan{
		TraceID:           traceID,
		SpanID:            spanID,
		ParentSpanID:      parentSpanID,
		Name:              name,
		Kind:              1,
		StartTimeUnixNano: fmt.Sprintf("%d", startedAt.UnixNano()),
		EndTimeUnixNano:   fmt.Sprintf("%d", completedAt.UnixNano()),
		Attributes:        attributes,
		Status:            AgentTraceOTLPStatus{Code: 1},
	}
}

func stringOTLPAttribute(key, value string) AgentTraceOTLPAttribute {
	return AgentTraceOTLPAttribute{Key: key, Value: AgentTraceOTLPAnyValue{StringValue: value}}
}

func doubleOTLPAttribute(key string, value float64) AgentTraceOTLPAttribute {
	return AgentTraceOTLPAttribute{Key: key, Value: AgentTraceOTLPAnyValue{DoubleValue: value}}
}

func intOTLPAttribute(key string, value int64) AgentTraceOTLPAttribute {
	return AgentTraceOTLPAttribute{Key: key, Value: AgentTraceOTLPAnyValue{IntValue: value}}
}

func joinedAgentTraceCitationIDs(citations []AgentTraceCitation) string {
	ids := make([]string, 0, len(citations))
	for _, citation := range citations {
		ids = append(ids, citation.CitationID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func deterministicHexID(seed string, length int) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])[:length]
}

func sha256Fingerprint(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
