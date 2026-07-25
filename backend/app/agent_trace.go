package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const (
	AgentTraceSchemaVersion           = "agent-trace.v1"
	evidenceAuditTraceTerminalVersion = "evidence-audit-trace-terminal.v1"
	evidenceAuditExecutionVersion     = "evidence-audit-execution.v2"
	evidenceAuditInvocationVersion    = "evidence-audit-model-invocation.v1"
	evidenceAuditTraceReceiptVersion  = "evidence-audit-trace-receipt.v1"

	evidenceAuditInvocationInFlight  = "in_flight"
	evidenceAuditInvocationCompleted = "completed"

	evidenceAuditTraceFaultBeforePrepare  = "before_prepare"
	evidenceAuditTraceFaultBeforeSave     = "before_save"
	evidenceAuditTraceFaultBeforeFinalize = "before_finalize"

	AgentToolOutcomeNotExecuted          = "not_executed"
	AgentToolOutcomeSucceeded            = "succeeded"
	AgentToolOutcomeFailed               = "failed"
	AgentToolOutcomeBlocked              = "blocked"
	AgentToolOutcomeConfirmationRequired = "confirmation_required"

	AgentTraceOutcomeCompleted = "completed"
	AgentTraceOutcomeAbstained = "abstained"
	AgentTraceOutcomeFailed    = "failed"
)

var evidenceAuditTraceStorageFault = func(string, string) error { return nil }

type AgentTrace struct {
	SchemaVersion  string                      `json:"schema_version"`
	TraceID        string                      `json:"trace_id"`
	Package        AgentTracePackageRef        `json:"package"`
	Releases       []AgentTraceReleaseRef      `json:"releases"`
	RetrievalRoute AgentTraceRetrievalRoute    `json:"retrieval_route"`
	Retrievals     []AgentTraceRetrieval       `json:"retrievals"`
	ModelRoute     AgentTraceModelRoute        `json:"model_route"`
	ToolCalls      []AgentTraceToolCall        `json:"tool_calls"`
	Final          AgentTraceFinal             `json:"final"`
	StartedAt      string                      `json:"started_at"`
	CompletedAt    string                      `json:"completed_at"`
	EvidenceAudit  *AgentTraceEvidenceAuditRef `json:"evidence_audit,omitempty"`
	Observability  *AgentTraceObservability    `json:"observability,omitempty"`

	Credentials    string   `json:"-"`
	SourceBodies   []string `json:"-"`
	PrivatePrompt  string   `json:"-"`
	ConsumerUserID string   `json:"-"`
}

type AgentTraceObservability struct {
	Stages                            []AgentTraceStage             `json:"stages"`
	CitationResolutionRate            float64                       `json:"citation_resolution_rate"`
	IndependentPublicationSourceCount int                           `json:"independent_publication_source_count"`
	FreshnessDecisions                []AgentTraceFreshnessDecision `json:"freshness_decisions,omitempty"`
	ReservedCostUSD                   float64                       `json:"reserved_cost_usd,omitempty"`
	AbstentionReason                  string                        `json:"abstention_reason,omitempty"`
	Usage                             AgentTraceUsage               `json:"usage"`
	TerminalProtocol                  string                        `json:"terminal_protocol,omitempty"`
}

type AgentTraceFreshnessDecision struct {
	ReleaseID   string `json:"release_id"`
	CitationID  string `json:"citation_id"`
	PublishedAt string `json:"published_at,omitempty"`
	Decision    string `json:"decision"`
}

type AgentTraceStage struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Definition string `json:"definition,omitempty"`
}

type AgentTraceUsage struct {
	Status           string  `json:"status"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	TotalTokens      int     `json:"total_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	CostStatus       string  `json:"cost_status,omitempty"`
}

type AgentTraceEvidenceAuditRef struct {
	AuditID   string `json:"audit_id"`
	InputHash string `json:"input_hash"`
}

type evidenceAuditTraceTerminal struct {
	Version           string         `json:"version"`
	AuditID           string         `json:"audit_id"`
	InputHash         string         `json:"input_hash"`
	TraceID           string         `json:"trace_id"`
	ReportFingerprint string         `json:"report_fingerprint"`
	Report            *EvidenceAudit `json:"report,omitempty"`
	FailureCode       string         `json:"failure_code,omitempty"`
	FailureSummary    string         `json:"failure_summary,omitempty"`
	Trace             AgentTrace     `json:"trace"`
}

type evidenceAuditTraceReceipt struct {
	Version    string `json:"version"`
	TraceID    string `json:"trace_id"`
	TraceHash  string `json:"trace_hash"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	SavedAt    string `json:"saved_at"`
}

type evidenceAuditExecutionPlan struct {
	Version     string                     `json:"version"`
	AuditID     string                     `json:"audit_id"`
	InputHash   string                     `json:"input_hash"`
	Package     EvidenceAuditPackageRef    `json:"package"`
	Model       EvidenceAuditModelIdentity `json:"model"`
	ClaimHashes []string                   `json:"claim_hashes"`
}

type evidenceAuditClaimCandidate struct {
	Version              string                     `json:"version"`
	AuditID              string                     `json:"audit_id"`
	InputHash            string                     `json:"input_hash"`
	Package              EvidenceAuditPackageRef    `json:"package"`
	Model                EvidenceAuditModelIdentity `json:"model"`
	ClaimIndex           int                        `json:"claim_index"`
	ClaimHash            string                     `json:"claim_hash"`
	RequestIdentity      string                     `json:"request_identity"`
	RetrievalFingerprint string                     `json:"retrieval_fingerprint"`
	Decision             evidenceAuditModelDecision `json:"decision"`
	Usage                AgentTraceUsage            `json:"usage"`
	ReservationUSD       float64                    `json:"reservation_usd"`
	CandidateHash        string                     `json:"candidate_hash"`
}

type evidenceAuditModelInvocation struct {
	Version         string `json:"version"`
	AuditID         string `json:"audit_id"`
	InputHash       string `json:"input_hash"`
	ClaimIndex      int    `json:"claim_index"`
	ClaimHash       string `json:"claim_hash"`
	RequestIdentity string `json:"request_identity"`
	Status          string `json:"status"`
	CandidateHash   string `json:"candidate_hash,omitempty"`
}

type AgentTraceRetrievalRoute struct {
	Strategy          string `json:"strategy"`
	EmbeddingIdentity string `json:"embedding_identity,omitempty"`
	RerankerVersion   string `json:"reranker_version,omitempty"`
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
	CitationID        string `json:"citation_id"`
	ReleaseID         string `json:"release_id"`
	EvidenceID        string `json:"evidence_id"`
	PublishedAt       string `json:"published_at,omitempty"`
	FreshnessDecision string `json:"freshness_decision,omitempty"`
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
		"retrieval_route.strategy":   trace.RetrievalRoute.Strategy,
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
	if len(trace.TraceID) > 128 || !agentPackageIDPattern.MatchString(trace.TraceID) {
		return fmt.Errorf("trace_id must be a URL-safe identifier of at most 128 characters")
	}
	if err := validateAgentSHA256("package.content_hash", trace.Package.ContentHash); err != nil {
		return err
	}
	if err := validateAgentSHA256("final.response_fingerprint", trace.Final.ResponseFingerprint); err != nil {
		return err
	}
	if trace.EvidenceAudit != nil {
		if strings.TrimSpace(trace.EvidenceAudit.AuditID) == "" ||
			!agentPackageIDPattern.MatchString(trace.EvidenceAudit.AuditID) {
			return fmt.Errorf("evidence_audit.audit_id must be a URL-safe identifier")
		}
		if err := validateAgentSHA256("evidence_audit.input_hash", trace.EvidenceAudit.InputHash); err != nil {
			return err
		}
		if err := validateAgentTraceObservability(trace.Observability); err != nil {
			return err
		}
	}
	switch trace.RetrievalRoute.Strategy {
	case "lexical", "graph":
	case "vector", "hybrid":
		if strings.TrimSpace(trace.RetrievalRoute.EmbeddingIdentity) == "" ||
			strings.TrimSpace(trace.RetrievalRoute.RerankerVersion) == "" {
			return fmt.Errorf("semantic retrieval trace requires embedding identity and reranker version")
		}
	default:
		return fmt.Errorf("unsupported retrieval route strategy %q", trace.RetrievalRoute.Strategy)
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
		if err := validateAgentSHA256(fmt.Sprintf("releases[%d].content_hash", index), release.ContentHash); err != nil {
			return err
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
	if trace.Final.Outcome == AgentTraceOutcomeCompleted && (len(trace.Retrievals) == 0 || len(trace.Final.Citations) == 0) {
		return fmt.Errorf("completed trace requires grounded evidence and citations")
	}
	return validateAgentTraceCitations(trace.Final.Citations, releases, evidence)
}

func validateAgentTraceObservability(value *AgentTraceObservability) error {
	if value == nil {
		return fmt.Errorf("evidence audit trace requires observability")
	}
	if len(value.Stages) == 0 || len(value.Stages) > 7 {
		return fmt.Errorf("observability.stages must contain at most seven stages")
	}
	allowedStages := map[string]bool{
		"package_validation": true, "claim_selection": true, "retrieval": true,
		"citation_resolution": true, "model": true, "report_persistence": true,
		"trace_persistence": true,
	}
	seen := map[string]bool{}
	for _, stage := range value.Stages {
		if !allowedStages[stage.Name] || seen[stage.Name] || stage.DurationMS < 0 {
			return fmt.Errorf("observability.stages contains an invalid stage")
		}
		if stage.DurationMS > int64((24*time.Hour)/time.Millisecond) {
			return fmt.Errorf("observability.stages duration exceeds 24 hours")
		}
		seen[stage.Name] = true
		switch stage.Status {
		case "pending", "completed", "failed", "skipped":
		default:
			return fmt.Errorf("observability stage %q has invalid status", stage.Name)
		}
		switch stage.Definition {
		case "", "immutable_report_preparation", "durable_trace_terminal_preparation":
		default:
			return fmt.Errorf("observability stage %q has invalid definition", stage.Name)
		}
		if stage.Name == "report_persistence" && stage.Definition != "" &&
			stage.Definition != "immutable_report_preparation" {
			return fmt.Errorf("report_persistence has invalid definition")
		}
		if stage.Name == "trace_persistence" && stage.Definition != "" &&
			stage.Definition != "durable_trace_terminal_preparation" {
			return fmt.Errorf("trace_persistence has invalid definition")
		}
	}
	if value.CitationResolutionRate < 0 || value.CitationResolutionRate > 1 {
		return fmt.Errorf("observability.citation_resolution_rate must be between zero and one")
	}
	if value.IndependentPublicationSourceCount < 0 || value.IndependentPublicationSourceCount > evidenceAuditMaxReleases {
		return fmt.Errorf("observability independent source count is invalid")
	}
	if value.ReservedCostUSD < 0 || value.ReservedCostUSD > 1_000_000 ||
		math.IsNaN(value.ReservedCostUSD) || math.IsInf(value.ReservedCostUSD, 0) {
		return fmt.Errorf("observability reserved cost is invalid")
	}
	if len(value.FreshnessDecisions) > evidenceAuditMaxListItems {
		return fmt.Errorf("observability freshness decisions exceed limit")
	}
	for index, decision := range value.FreshnessDecisions {
		if strings.TrimSpace(decision.ReleaseID) == "" ||
			strings.TrimSpace(decision.CitationID) == "" {
			return fmt.Errorf("observability freshness_decisions[%d] identity is required", index)
		}
		switch decision.Decision {
		case EvidenceAuditFreshnessFresh, EvidenceAuditFreshnessStale:
			if _, err := parseEvidenceAuditPublicationDate(decision.PublishedAt); err != nil {
				return fmt.Errorf("observability freshness_decisions[%d].published_at: %w", index, err)
			}
		case EvidenceAuditFreshnessMissing:
			if strings.TrimSpace(decision.PublishedAt) != "" {
				return fmt.Errorf("observability freshness_decisions[%d] missing date must be empty", index)
			}
		default:
			return fmt.Errorf("observability freshness_decisions[%d] decision is invalid", index)
		}
	}
	if value.AbstentionReason != "" &&
		(len(value.AbstentionReason) > 128 || !agentPackageIDPattern.MatchString(value.AbstentionReason)) {
		return fmt.Errorf("observability.abstention_reason must be a bounded reason code")
	}
	if value.TerminalProtocol != "" &&
		value.TerminalProtocol != "prepared-report-trace-then-audit-publish.v1" &&
		value.TerminalProtocol != "prepared-report-trace-receipt-audit-publish.v2" {
		return fmt.Errorf("observability terminal_protocol is invalid")
	}
	return validateAgentTraceUsage(value.Usage)
}

func validateAgentTraceUsage(value AgentTraceUsage) error {
	switch value.Status {
	case "unknown":
		if value.PromptTokens != 0 || value.CompletionTokens != 0 ||
			value.TotalTokens != 0 || value.CostUSD != 0 || value.CostStatus != "" {
			return fmt.Errorf("observability usage marked unknown must not contain values")
		}
	case "reported":
		if value.PromptTokens < 0 || value.CompletionTokens < 0 ||
			value.TotalTokens < 0 ||
			value.TotalTokens != value.PromptTokens+value.CompletionTokens ||
			value.TotalTokens > 5_000_000 ||
			value.CostUSD < 0 || value.CostUSD > 1_000_000 {
			return fmt.Errorf("observability usage is invalid")
		}
		switch value.CostStatus {
		case "reported":
		case "unknown":
			if value.CostUSD != 0 {
				return fmt.Errorf("observability usage cost marked unknown must be zero")
			}
		default:
			return fmt.Errorf("observability usage cost_status is invalid")
		}
	default:
		return fmt.Errorf("observability usage status is invalid")
	}
	return nil
}

func validateAgentTraceToolCall(call AgentTraceToolCall) error {
	if err := validateAgentSHA256("argument_fingerprint", call.ArgumentFingerprint); err != nil {
		return fmt.Errorf("tool %q: %w", call.CallID, err)
	}
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
	if strings.TrimSpace(call.ResultFingerprint) != "" {
		if err := validateAgentSHA256("result_fingerprint", call.ResultFingerprint); err != nil {
			return fmt.Errorf("tool %q: %w", call.CallID, err)
		}
	}
	return nil
}

func validateAgentSHA256(field, value string) error {
	digest := strings.TrimPrefix(strings.TrimSpace(value), "sha256:")
	if !strings.HasPrefix(strings.TrimSpace(value), "sha256:") || len(digest) != 64 || !isLowerHex(digest) {
		return fmt.Errorf("%s must be a lowercase sha256 fingerprint", field)
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
		if citation.PublishedAt != "" || citation.FreshnessDecision != "" {
			switch citation.FreshnessDecision {
			case EvidenceAuditFreshnessFresh, EvidenceAuditFreshnessStale:
				if _, err := parseEvidenceAuditPublicationDate(citation.PublishedAt); err != nil {
					return fmt.Errorf("citations[%d].published_at: %w", index, err)
				}
			case EvidenceAuditFreshnessMissing:
				if strings.TrimSpace(citation.PublishedAt) != "" {
					return fmt.Errorf("citations[%d] missing publication date must be empty", index)
				}
			default:
				return fmt.Errorf("citations[%d].freshness_decision is invalid", index)
			}
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

func (s *BookKnowledgeStore) EvidenceAuditTraceReceiptPath(traceID string) string {
	return filepath.Join(
		s.AgentTraceDir(), "receipts", sanitizeBookKnowledgeID(traceID)+".json",
	)
}

func (s *BookKnowledgeStore) EvidenceAuditTraceTerminalPath(auditID string) string {
	return filepath.Join(s.AgentTraceDir(), "prepared", sanitizeBookKnowledgeID(auditID)+".json")
}

func (s *BookKnowledgeStore) evidenceAuditExecutionDir(auditID string) string {
	return filepath.Join(s.AgentTraceDir(), "execution", sanitizeBookKnowledgeID(auditID))
}

func (s *BookKnowledgeStore) acquireEvidenceAuditExecutionLock(
	ctx context.Context,
	auditID string,
) (func(), error) {
	dir := s.evidenceAuditExecutionDir(auditID)
	if err := ensureEvidenceAuditPrivateDir(dir); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fileLock := flock.New(filepath.Join(dir, ".run.lock"))
	locked, err := fileLock.TryLock()
	if err != nil || !locked {
		_ = fileLock.Close()
		if err == nil {
			err = ErrEvidenceAuditExecutionBusy
		}
		return nil, fmt.Errorf("%w: %v", ErrEvidenceAuditExecutionBusy, err)
	}
	return func() { _ = fileLock.Close() }, nil
}

func (s *BookKnowledgeStore) prepareEvidenceAuditExecutionPlan(
	audit EvidenceAudit,
) (evidenceAuditExecutionPlan, error) {
	plan := evidenceAuditExecutionPlan{
		Version: evidenceAuditExecutionVersion,
		AuditID: audit.AuditID, InputHash: audit.InputHash,
		Package: audit.Package, Model: audit.Model,
		ClaimHashes: make([]string, 0, len(audit.SelectedClaims)),
	}
	for _, claim := range audit.SelectedClaims {
		plan.ClaimHashes = append(plan.ClaimHashes, sha256Fingerprint([]byte(strings.TrimSpace(claim))))
	}
	if err := validateEvidenceAuditExecutionPlan(plan); err != nil {
		return evidenceAuditExecutionPlan{}, err
	}
	payload, err := encodeJSONFile(plan)
	if err != nil {
		return evidenceAuditExecutionPlan{}, err
	}
	path := filepath.Join(s.evidenceAuditExecutionDir(audit.AuditID), "plan.json")
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ensureEvidenceAuditPrivateDir(filepath.Dir(path)); err != nil {
		return evidenceAuditExecutionPlan{}, err
	}
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if !bytes.Equal(existing, payload) {
			return evidenceAuditExecutionPlan{}, fmt.Errorf("evidence audit execution plan identity conflict")
		}
		return plan, nil
	} else if !os.IsNotExist(readErr) {
		return evidenceAuditExecutionPlan{}, readErr
	}
	if err := writeEvidenceAuditImmutableFile(path, payload); err != nil {
		return evidenceAuditExecutionPlan{}, err
	}
	return plan, nil
}

func validateEvidenceAuditExecutionPlan(plan evidenceAuditExecutionPlan) error {
	if plan.Version != evidenceAuditExecutionVersion {
		return fmt.Errorf("unsupported evidence audit execution version %q", plan.Version)
	}
	if strings.TrimSpace(plan.AuditID) == "" {
		return fmt.Errorf("evidence audit execution plan requires audit_id")
	}
	if err := validateAgentSHA256("execution input_hash", plan.InputHash); err != nil {
		return err
	}
	if err := validateAgentSHA256("execution package content_hash", plan.Package.ContentHash); err != nil {
		return err
	}
	if strings.TrimSpace(plan.Package.PackageID) == "" || strings.TrimSpace(plan.Package.Version) == "" ||
		strings.TrimSpace(plan.Model.Provider) == "" || strings.TrimSpace(plan.Model.Model) == "" ||
		len(plan.ClaimHashes) == 0 || len(plan.ClaimHashes) > agentEvidenceMaxClaims {
		return fmt.Errorf("evidence audit execution plan identity is incomplete")
	}
	for _, claimHash := range plan.ClaimHashes {
		if err := validateAgentSHA256("execution claim_hash", claimHash); err != nil {
			return err
		}
	}
	return nil
}

func (s *BookKnowledgeStore) saveEvidenceAuditClaimCandidate(
	plan evidenceAuditExecutionPlan,
	claimIndex int,
	requestIdentity, retrievalFingerprint string,
	decision evidenceAuditModelDecision,
	usage AgentTraceUsage,
	reservationUSD float64,
) (evidenceAuditClaimCandidate, error) {
	if err := validateEvidenceAuditExecutionPlan(plan); err != nil {
		return evidenceAuditClaimCandidate{}, err
	}
	if claimIndex < 0 || claimIndex >= len(plan.ClaimHashes) {
		return evidenceAuditClaimCandidate{}, fmt.Errorf("claim checkpoint index is outside execution plan")
	}
	candidate := evidenceAuditClaimCandidate{
		Version: evidenceAuditExecutionVersion,
		AuditID: plan.AuditID, InputHash: plan.InputHash,
		Package: plan.Package, Model: plan.Model,
		ClaimIndex: claimIndex, ClaimHash: plan.ClaimHashes[claimIndex],
		RequestIdentity: requestIdentity, RetrievalFingerprint: retrievalFingerprint,
		Decision: decision, Usage: usage, ReservationUSD: reservationUSD,
	}
	hash, err := evidenceAuditClaimCandidateHash(candidate)
	if err != nil {
		return evidenceAuditClaimCandidate{}, err
	}
	candidate.CandidateHash = hash
	if err := validateEvidenceAuditClaimCandidate(plan, candidate); err != nil {
		return evidenceAuditClaimCandidate{}, err
	}
	payload, err := encodeJSONFile(candidate)
	if err != nil {
		return evidenceAuditClaimCandidate{}, err
	}
	path := filepath.Join(
		s.evidenceAuditExecutionDir(plan.AuditID), "results",
		evidenceAuditHashName(candidate.CandidateHash)+".json",
	)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ensureEvidenceAuditPrivateDir(filepath.Dir(path)); err != nil {
		return evidenceAuditClaimCandidate{}, err
	}
	if err := writeEvidenceAuditImmutableFile(path, payload); err != nil {
		return evidenceAuditClaimCandidate{}, err
	}
	return candidate, nil
}

func (s *BookKnowledgeStore) beginEvidenceAuditModelInvocation(
	plan evidenceAuditExecutionPlan,
	claimIndex int,
	requestIdentity string,
) (evidenceAuditModelInvocation, bool, error) {
	invocation := evidenceAuditModelInvocation{
		Version: evidenceAuditInvocationVersion,
		AuditID: plan.AuditID, InputHash: plan.InputHash,
		ClaimIndex: claimIndex, RequestIdentity: requestIdentity,
		Status: evidenceAuditInvocationInFlight,
	}
	if claimIndex >= 0 && claimIndex < len(plan.ClaimHashes) {
		invocation.ClaimHash = plan.ClaimHashes[claimIndex]
	}
	if err := validateEvidenceAuditModelInvocation(plan, invocation); err != nil {
		return evidenceAuditModelInvocation{}, false, err
	}
	path := filepath.Join(
		s.evidenceAuditExecutionDir(plan.AuditID), "requests",
		fmt.Sprintf("%02d.json", claimIndex),
	)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existingPayload, err := os.ReadFile(path); err == nil {
		var existing evidenceAuditModelInvocation
		if err := json.Unmarshal(existingPayload, &existing); err != nil {
			return evidenceAuditModelInvocation{}, false, err
		}
		if err := validateEvidenceAuditModelInvocation(plan, existing); err != nil {
			return evidenceAuditModelInvocation{}, false, err
		}
		if existing.RequestIdentity != requestIdentity {
			return evidenceAuditModelInvocation{}, false, fmt.Errorf("model invocation request identity conflict")
		}
		return existing, false, nil
	} else if !os.IsNotExist(err) {
		return evidenceAuditModelInvocation{}, false, err
	}
	payload, err := encodeJSONFile(invocation)
	if err != nil {
		return evidenceAuditModelInvocation{}, false, err
	}
	if err := ensureEvidenceAuditPrivateDir(filepath.Dir(path)); err != nil {
		return evidenceAuditModelInvocation{}, false, err
	}
	if err := writeEvidenceAuditPrivateFile(path, payload); err != nil {
		return evidenceAuditModelInvocation{}, false, err
	}
	return invocation, true, nil
}

func (s *BookKnowledgeStore) completeEvidenceAuditModelInvocation(
	plan evidenceAuditExecutionPlan,
	invocation evidenceAuditModelInvocation,
	candidateHash string,
) error {
	if invocation.Status != evidenceAuditInvocationInFlight {
		return fmt.Errorf("only an in-flight model invocation can be completed")
	}
	invocation.Status = evidenceAuditInvocationCompleted
	invocation.CandidateHash = candidateHash
	if err := validateEvidenceAuditModelInvocation(plan, invocation); err != nil {
		return err
	}
	path := filepath.Join(
		s.evidenceAuditExecutionDir(plan.AuditID), "requests",
		fmt.Sprintf("%02d.json", invocation.ClaimIndex),
	)
	payload, err := encodeJSONFile(invocation)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeEvidenceAuditPrivateFile(path, payload)
}

func validateEvidenceAuditModelInvocation(
	plan evidenceAuditExecutionPlan,
	invocation evidenceAuditModelInvocation,
) error {
	if err := validateEvidenceAuditExecutionPlan(plan); err != nil {
		return err
	}
	if invocation.Version != evidenceAuditInvocationVersion ||
		invocation.AuditID != plan.AuditID ||
		invocation.InputHash != plan.InputHash ||
		invocation.ClaimIndex < 0 ||
		invocation.ClaimIndex >= len(plan.ClaimHashes) ||
		invocation.ClaimHash != plan.ClaimHashes[invocation.ClaimIndex] {
		return fmt.Errorf("model invocation identity does not match execution plan")
	}
	if err := validateAgentSHA256("model request_identity", invocation.RequestIdentity); err != nil {
		return err
	}
	switch invocation.Status {
	case evidenceAuditInvocationInFlight:
		if invocation.CandidateHash != "" {
			return fmt.Errorf("in-flight model invocation cannot reference a candidate")
		}
	case evidenceAuditInvocationCompleted:
		if err := validateAgentSHA256("model candidate_hash", invocation.CandidateHash); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported model invocation status %q", invocation.Status)
	}
	return nil
}

func (s *BookKnowledgeStore) loadEvidenceAuditClaimCandidates(
	plan evidenceAuditExecutionPlan,
) (map[int]evidenceAuditClaimCandidate, error) {
	if err := validateEvidenceAuditExecutionPlan(plan); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.evidenceAuditExecutionDir(plan.AuditID), "results")
	s.mu.RLock()
	entries, err := os.ReadDir(dir)
	s.mu.RUnlock()
	if os.IsNotExist(err) {
		return map[int]evidenceAuditClaimCandidate{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(entries) > agentEvidenceMaxClaims {
		return nil, fmt.Errorf("evidence audit execution has too many candidate files")
	}
	result := make(map[int]evidenceAuditClaimCandidate, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return nil, fmt.Errorf("unexpected evidence audit checkpoint entry %q", entry.Name())
		}
		var candidate evidenceAuditClaimCandidate
		s.mu.RLock()
		readErr := readJSONFile(filepath.Join(dir, entry.Name()), &candidate)
		s.mu.RUnlock()
		if readErr != nil {
			return nil, readErr
		}
		if err := validateEvidenceAuditClaimCandidate(plan, candidate); err != nil {
			return nil, err
		}
		if strings.TrimSuffix(entry.Name(), ".json") != evidenceAuditHashName(candidate.CandidateHash) {
			return nil, fmt.Errorf("evidence audit candidate filename does not match content hash")
		}
		if prior, ok := result[candidate.ClaimIndex]; ok && prior.CandidateHash != candidate.CandidateHash {
			return nil, fmt.Errorf("conflicting evidence audit candidates for claim %d", candidate.ClaimIndex)
		}
		result[candidate.ClaimIndex] = candidate
	}
	return result, nil
}

func evidenceAuditClaimCandidateHash(candidate evidenceAuditClaimCandidate) (string, error) {
	candidate.CandidateHash = ""
	payload, err := json.Marshal(candidate)
	if err != nil {
		return "", err
	}
	return sha256Fingerprint(payload), nil
}

func validateEvidenceAuditClaimCandidate(
	plan evidenceAuditExecutionPlan,
	candidate evidenceAuditClaimCandidate,
) error {
	// These hashes bind recovery to the canonical request and retrieval snapshot.
	// The local state directory remains a trusted boundary; this is drift
	// detection, not a claim of cryptographic authenticity against a local writer.
	if candidate.Version != evidenceAuditExecutionVersion ||
		candidate.AuditID != plan.AuditID || candidate.InputHash != plan.InputHash ||
		!reflect.DeepEqual(candidate.Package, plan.Package) ||
		!reflect.DeepEqual(candidate.Model, plan.Model) ||
		candidate.ClaimIndex < 0 || candidate.ClaimIndex >= len(plan.ClaimHashes) ||
		candidate.ClaimHash != plan.ClaimHashes[candidate.ClaimIndex] {
		return fmt.Errorf("evidence audit candidate identity does not match execution plan")
	}
	if err := validateAgentSHA256("candidate request_identity", candidate.RequestIdentity); err != nil {
		return err
	}
	if err := validateAgentSHA256("candidate retrieval_fingerprint", candidate.RetrievalFingerprint); err != nil {
		return err
	}
	if _, err := parseEvidenceAuditModelDecision(mustEncodeEvidenceAuditDecision(candidate.Decision)); err != nil {
		return fmt.Errorf("validate evidence audit candidate decision: %w", err)
	}
	if err := validateAgentTraceUsage(candidate.Usage); err != nil {
		return fmt.Errorf("validate evidence audit candidate usage: %w", err)
	}
	if candidate.ReservationUSD <= 0 || candidate.ReservationUSD > 1_000_000 ||
		math.IsNaN(candidate.ReservationUSD) || math.IsInf(candidate.ReservationUSD, 0) {
		return fmt.Errorf("candidate reservation_usd is invalid")
	}
	expected, err := evidenceAuditClaimCandidateHash(candidate)
	if err != nil {
		return err
	}
	if candidate.CandidateHash != expected {
		return fmt.Errorf("evidence audit candidate content hash mismatch")
	}
	return nil
}

func mustEncodeEvidenceAuditDecision(decision evidenceAuditModelDecision) string {
	payload, _ := json.Marshal(decision)
	return string(payload)
}

func (s *BookKnowledgeStore) prepareEvidenceAuditTraceTerminal(terminal evidenceAuditTraceTerminal) error {
	if err := validateEvidenceAuditTraceTerminal(terminal); err != nil {
		return err
	}
	payload, err := encodeJSONFile(terminal)
	if err != nil {
		return err
	}
	path := s.EvidenceAuditTraceTerminalPath(terminal.AuditID)
	if err := evidenceAuditTraceStorageFault(evidenceAuditTraceFaultBeforePrepare, path); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ensureEvidenceAuditPrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err == nil {
		if bytes.Equal(existing, payload) {
			return nil
		}
		return fmt.Errorf("prepared evidence audit terminal already exists with different content")
	}
	if !os.IsNotExist(err) {
		return err
	}
	return writeEvidenceAuditPrivateFile(path, payload)
}

func (s *BookKnowledgeStore) finalizeEvidenceAuditTraceTerminalMetadata(
	terminal evidenceAuditTraceTerminal,
) error {
	if err := validateEvidenceAuditTraceTerminal(terminal); err != nil {
		return err
	}
	payload, err := encodeJSONFile(terminal)
	if err != nil {
		return err
	}
	path := s.EvidenceAuditTraceTerminalPath(terminal.AuditID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return writeEvidenceAuditPrivateFile(path, payload)
}

func (s *BookKnowledgeStore) loadEvidenceAuditTraceTerminal(auditID string) (*evidenceAuditTraceTerminal, error) {
	auditID = strings.TrimSpace(auditID)
	if auditID == "" {
		return nil, fmt.Errorf("audit_id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var terminal evidenceAuditTraceTerminal
	if err := readJSONFile(s.EvidenceAuditTraceTerminalPath(auditID), &terminal); err != nil {
		return nil, err
	}
	if terminal.AuditID != auditID {
		return nil, fmt.Errorf("prepared evidence audit terminal identity does not match audit_id")
	}
	if err := validateEvidenceAuditTraceTerminal(terminal); err != nil {
		return nil, err
	}
	return &terminal, nil
}

func (s *BookKnowledgeStore) finalizeEvidenceAuditTraceTerminal(terminal evidenceAuditTraceTerminal) error {
	if err := validateEvidenceAuditTraceTerminal(terminal); err != nil {
		return err
	}
	path := s.EvidenceAuditTraceTerminalPath(terminal.AuditID)
	if err := evidenceAuditTraceStorageFault(evidenceAuditTraceFaultBeforeSave, path); err != nil {
		return err
	}
	startedAt := time.Now()
	if err := s.SaveAgentTrace(terminal.Trace); err != nil {
		return err
	}
	duration := time.Since(startedAt).Milliseconds()
	if duration < 1 {
		duration = 1
	}
	if err := s.saveEvidenceAuditTraceReceipt(terminal.Trace, duration); err != nil {
		s.mu.Lock()
		_ = os.Remove(s.AgentTracePath(terminal.Trace.TraceID))
		s.mu.Unlock()
		return err
	}
	if err := evidenceAuditTraceStorageFault(evidenceAuditTraceFaultBeforeFinalize, path); err != nil {
		return err
	}
	return nil
}

func (s *BookKnowledgeStore) saveEvidenceAuditTraceReceipt(
	trace AgentTrace,
	durationMS int64,
) error {
	payload, err := encodeJSONFile(trace)
	if err != nil {
		return err
	}
	receipt := evidenceAuditTraceReceipt{
		Version: evidenceAuditTraceReceiptVersion, TraceID: trace.TraceID,
		TraceHash: sha256Fingerprint(payload), Status: evidenceAuditTraceReceiptStatus(trace),
		DurationMS: durationMS, SavedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := validateEvidenceAuditTraceReceipt(receipt, payload); err != nil {
		return err
	}
	receiptPayload, err := encodeJSONFile(receipt)
	if err != nil {
		return err
	}
	return writeEvidenceAuditPrivateFile(
		s.EvidenceAuditTraceReceiptPath(trace.TraceID), receiptPayload,
	)
}

func evidenceAuditTraceReceiptStatus(trace AgentTrace) string {
	if trace.Observability != nil {
		for _, stage := range trace.Observability.Stages {
			if stage.Name == "trace_persistence" && stage.Status == "failed" {
				return "failed"
			}
		}
	}
	return "completed"
}

func validateEvidenceAuditTraceReceipt(
	receipt evidenceAuditTraceReceipt,
	tracePayload []byte,
) error {
	if receipt.Version != evidenceAuditTraceReceiptVersion ||
		strings.TrimSpace(receipt.TraceID) == "" ||
		(receipt.Status != "completed" && receipt.Status != "failed") ||
		receipt.DurationMS < 0 || receipt.DurationMS > int64((24*time.Hour)/time.Millisecond) {
		return fmt.Errorf("invalid evidence audit trace persistence receipt")
	}
	if err := validateAgentSHA256("trace receipt hash", receipt.TraceHash); err != nil {
		return err
	}
	if receipt.TraceHash != sha256Fingerprint(tracePayload) {
		return fmt.Errorf("trace persistence receipt does not match stored trace")
	}
	if _, err := time.Parse(time.RFC3339Nano, receipt.SavedAt); err != nil {
		return fmt.Errorf("trace receipt saved_at must be RFC3339: %w", err)
	}
	return nil
}

func mergeEvidenceAuditTraceReceipt(
	trace *AgentTrace,
	receipt evidenceAuditTraceReceipt,
	tracePayload []byte,
) error {
	if trace == nil || trace.TraceID != receipt.TraceID {
		return fmt.Errorf("trace persistence receipt identity mismatch")
	}
	if err := validateEvidenceAuditTraceReceipt(receipt, tracePayload); err != nil {
		return err
	}
	if trace.Observability == nil {
		return fmt.Errorf("evidence audit trace receipt requires observability")
	}
	found := false
	for index := range trace.Observability.Stages {
		stage := &trace.Observability.Stages[index]
		if stage.Name != "trace_persistence" {
			continue
		}
		stage.Status = receipt.Status
		stage.DurationMS = receipt.DurationMS
		found = true
	}
	if !found {
		return fmt.Errorf("trace persistence receipt has no target stage")
	}
	return nil
}

func (s *BookKnowledgeStore) removeEvidenceAuditTraceTerminal(auditID string) error {
	path := s.EvidenceAuditTraceTerminalPath(auditID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func validateEvidenceAuditTraceTerminal(terminal evidenceAuditTraceTerminal) error {
	if terminal.Version != evidenceAuditTraceTerminalVersion {
		return fmt.Errorf("unsupported evidence audit trace terminal version %q", terminal.Version)
	}
	if strings.TrimSpace(terminal.AuditID) == "" || strings.TrimSpace(terminal.TraceID) == "" {
		return fmt.Errorf("prepared evidence audit terminal requires audit_id and trace_id")
	}
	if err := validateAgentSHA256("prepared input_hash", terminal.InputHash); err != nil {
		return err
	}
	if err := validateAgentSHA256("prepared report_fingerprint", terminal.ReportFingerprint); err != nil {
		return err
	}
	if err := ValidateAgentTrace(terminal.Trace); err != nil {
		return fmt.Errorf("validate prepared evidence audit trace: %w", err)
	}
	if terminal.Trace.TraceID != terminal.TraceID ||
		terminal.Trace.EvidenceAudit == nil ||
		terminal.Trace.EvidenceAudit.AuditID != terminal.AuditID ||
		terminal.Trace.EvidenceAudit.InputHash != terminal.InputHash ||
		terminal.Trace.Final.ResponseFingerprint != terminal.ReportFingerprint {
		return fmt.Errorf("prepared evidence audit terminal trace identity is inconsistent")
	}
	switch terminal.Trace.Final.Outcome {
	case AgentTraceOutcomeCompleted, AgentTraceOutcomeAbstained:
		if terminal.Report == nil || terminal.FailureCode != "" || terminal.FailureSummary != "" {
			return fmt.Errorf("successful evidence audit terminal requires only a report")
		}
		if terminal.Report.AuditID != terminal.AuditID || terminal.Report.TraceID != terminal.TraceID {
			return fmt.Errorf("prepared evidence audit terminal report identity is inconsistent")
		}
		if terminal.Report.InputHash != terminal.InputHash ||
			terminal.Report.Package.PackageID != terminal.Trace.Package.PackageID ||
			terminal.Report.Package.Version != terminal.Trace.Package.Version ||
			terminal.Report.Package.ContentHash != terminal.Trace.Package.ContentHash {
			return fmt.Errorf("prepared evidence audit terminal report scope is inconsistent")
		}
		fingerprint, err := evidenceAuditReportFingerprint(*terminal.Report)
		if err != nil {
			return err
		}
		if fingerprint != terminal.ReportFingerprint {
			return fmt.Errorf("prepared evidence audit terminal report fingerprint is inconsistent")
		}
		expectedCitations := make(map[string]struct{})
		for _, claim := range terminal.Report.ClaimAudits {
			for _, ref := range claim.Evidence {
				key := ref.CitationID + "\x00" + ref.ReleaseID + "\x00" +
					ref.ReleaseID + ":" + ref.ClaimID + ":" + ref.CitationID
				expectedCitations[key] = struct{}{}
			}
		}
		actualCitations := make(map[string]struct{}, len(terminal.Trace.Final.Citations))
		for _, citation := range terminal.Trace.Final.Citations {
			key := citation.CitationID + "\x00" + citation.ReleaseID + "\x00" + citation.EvidenceID
			actualCitations[key] = struct{}{}
		}
		if !reflect.DeepEqual(expectedCitations, actualCitations) {
			return fmt.Errorf("prepared evidence audit terminal final citations do not match report evidence")
		}
	case AgentTraceOutcomeFailed:
		if terminal.Report != nil || strings.TrimSpace(terminal.FailureCode) == "" ||
			strings.TrimSpace(terminal.FailureSummary) == "" {
			return fmt.Errorf("failed evidence audit terminal requires failure metadata only")
		}
	default:
		return fmt.Errorf("unsupported evidence audit terminal outcome %q", terminal.Trace.Final.Outcome)
	}
	return nil
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
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil, fmt.Errorf("trace_id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	tracePayload, err := os.ReadFile(s.AgentTracePath(traceID))
	if err != nil {
		return nil, err
	}
	var trace AgentTrace
	if err := json.Unmarshal(tracePayload, &trace); err != nil {
		return nil, err
	}
	if trace.TraceID != traceID {
		return nil, fmt.Errorf("stored trace identity does not match requested trace_id")
	}
	if trace.EvidenceAudit != nil && trace.Observability != nil &&
		trace.Observability.TerminalProtocol == "prepared-report-trace-receipt-audit-publish.v2" {
		var receipt evidenceAuditTraceReceipt
		if err := readJSONFile(s.EvidenceAuditTraceReceiptPath(traceID), &receipt); err != nil {
			return nil, fmt.Errorf("load evidence audit trace persistence receipt: %w", err)
		}
		if err := mergeEvidenceAuditTraceReceipt(&trace, receipt, tracePayload); err != nil {
			return nil, err
		}
	}
	if err := ValidateAgentTrace(trace); err != nil {
		return nil, err
	}
	return &trace, nil
}

func ensureEvidenceAuditPrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func writeEvidenceAuditPrivateFile(path string, payload []byte) error {
	if err := ensureEvidenceAuditPrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	tempPath, err := writeEvidenceAuditSyncedTemp(
		filepath.Dir(path), "."+filepath.Base(path)+".tmp-", payload,
	)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	if err := os.Rename(tempPath, path); err != nil {
		if runtime.GOOS != "windows" {
			return err
		}
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return removeErr
		}
		if err := os.Rename(tempPath, path); err != nil {
			return err
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return syncEvidenceAuditDir(filepath.Dir(path))
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
		if err := validateAgentSHA256(fmt.Sprintf("stored evidence %q content_hash", item.EvidenceID), item.ContentHash); err != nil {
			return AgentReplayResult{}, err
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
	if err := validateAgentSHA256("mock model output_hash", fixture.Model.OutputHash); err != nil {
		return AgentReplayResult{}, err
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
		if result.Outcome == AgentToolOutcomeSucceeded || result.Outcome == AgentToolOutcomeFailed || strings.TrimSpace(result.ResultHash) != "" {
			if err := validateAgentSHA256(fmt.Sprintf("mock tool result %q result_hash", result.CallID), result.ResultHash); err != nil {
				return AgentReplayResult{}, err
			}
		}
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
