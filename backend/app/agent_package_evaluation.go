package app

import (
	"context"
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
	AgentEvaluationSchemaVersion       = "agent-evaluation.v1"
	AgentEvaluationReportSchemaVersion = "agent-evaluation-report.v1"
	AgentDeterministicEvaluatorVersion = "deterministic-agent-evaluator.v1"
)

type AgentEvaluationSuite struct {
	SchemaVersion string                `json:"schema_version"`
	SuiteVersion  string                `json:"suite_version"`
	Cases         []AgentEvaluationCase `json:"cases"`
}

type AgentEvaluationCase struct {
	CaseID            string            `json:"case_id"`
	Metric            string            `json:"metric"`
	Input             string            `json:"input,omitempty"`
	ExpectedIDs       []string          `json:"expected_ids,omitempty"`
	ObservedIDs       []string          `json:"-"`
	ExpectedValue     string            `json:"expected_value,omitempty"`
	ModelOutput       string            `json:"model_output,omitempty"`
	ProposedTool      string            `json:"proposed_tool,omitempty"`
	ProposedArguments map[string]string `json:"proposed_arguments,omitempty"`
	ObservedValue     string            `json:"-"`
	ExpectedArguments map[string]string `json:"expected_arguments,omitempty"`
	ObservedArguments map[string]string `json:"-"`
	MaxLatencyMS      int               `json:"max_latency_ms,omitempty"`
	RecordedLatencyMS int               `json:"recorded_latency_ms,omitempty"`
	MaxCostUSD        float64           `json:"max_cost_usd,omitempty"`
}

type AgentEvaluationReport struct {
	SchemaVersion      string                           `json:"schema_version"`
	PackageID          string                           `json:"package_id"`
	PackageContentHash string                           `json:"package_content_hash"`
	SuiteVersion       string                           `json:"suite_version"`
	InputHash          string                           `json:"input_hash"`
	EvaluatorVersion   string                           `json:"evaluator_version"`
	RetrievalIdentity  AgentEvaluationRetrievalIdentity `json:"retrieval_identity"`
	Metrics            map[string]float64               `json:"metrics"`
	Passed             bool                             `json:"passed"`
	Failures           []string                         `json:"failures,omitempty"`
	EvaluatedAt        string                           `json:"evaluated_at"`
}

type AgentEvaluationRetrievalIdentity struct {
	Strategy          string `json:"strategy"`
	EmbeddingIdentity string `json:"embedding_identity,omitempty"`
	RerankerVersion   string `json:"reranker_version,omitempty"`
}

func EvaluateAgentPackageDeterministically(store *BookKnowledgeStore, pkg AgentPackage, suite AgentEvaluationSuite, now time.Time) (AgentEvaluationReport, error) {
	if strings.TrimSpace(pkg.ContentHash) == "" {
		return AgentEvaluationReport{}, fmt.Errorf("package content_hash is required")
	}
	if store == nil {
		return AgentEvaluationReport{}, fmt.Errorf("published release store is required")
	}
	if suite.SchemaVersion != AgentEvaluationSchemaVersion {
		return AgentEvaluationReport{}, fmt.Errorf("schema_version must be %q", AgentEvaluationSchemaVersion)
	}
	if strings.TrimSpace(suite.SuiteVersion) == "" || suite.SuiteVersion != pkg.EvaluationPolicy.SuiteVersion {
		return AgentEvaluationReport{}, fmt.Errorf("evaluation suite version %q does not match package policy %q", suite.SuiteVersion, pkg.EvaluationPolicy.SuiteVersion)
	}
	if len(suite.Cases) == 0 {
		return AgentEvaluationReport{}, fmt.Errorf("evaluation cases are required")
	}
	inputHash, err := agentEvaluationInputHash(pkg.ContentHash, suite)
	if err != nil {
		return AgentEvaluationReport{}, err
	}
	metricPassed := make(map[string]int)
	metricTotal := make(map[string]int)
	for index, evalCase := range suite.Cases {
		if strings.TrimSpace(evalCase.CaseID) == "" || strings.TrimSpace(evalCase.Metric) == "" {
			return AgentEvaluationReport{}, fmt.Errorf("cases[%d] requires case_id and metric", index)
		}
		metricTotal[evalCase.Metric]++
		passed, caseErr := executeAgentEvaluationCase(store, pkg, evalCase)
		if caseErr != nil {
			return AgentEvaluationReport{}, fmt.Errorf("evaluate case %q: %w", evalCase.CaseID, caseErr)
		}
		if passed {
			metricPassed[evalCase.Metric]++
		}
	}
	metrics := make(map[string]float64, len(metricTotal))
	for metric, total := range metricTotal {
		metrics[metric] = float64(metricPassed[metric]) / float64(total)
	}
	failures := agentEvaluationThresholdFailures(pkg.EvaluationPolicy.MinimumScores, metrics)
	if now.IsZero() {
		now = time.Now()
	}
	retrievalIdentity := AgentEvaluationRetrievalIdentity{Strategy: pkg.RetrievalPolicy.Strategy}
	if pkg.RetrievalPolicy.Strategy == "vector" || pkg.RetrievalPolicy.Strategy == "hybrid" {
		retrievalIdentity.EmbeddingIdentity = agentPackageSemanticEmbedderIdentity(pkg.RetrievalPolicy)
		retrievalIdentity.RerankerVersion = pkg.RetrievalPolicy.RerankerVersion
	}
	return AgentEvaluationReport{
		SchemaVersion:      AgentEvaluationReportSchemaVersion,
		PackageID:          pkg.PackageID,
		PackageContentHash: pkg.ContentHash,
		SuiteVersion:       suite.SuiteVersion,
		InputHash:          inputHash,
		EvaluatorVersion:   AgentDeterministicEvaluatorVersion,
		RetrievalIdentity:  retrievalIdentity,
		Metrics:            metrics,
		Passed:             len(failures) == 0,
		Failures:           failures,
		EvaluatedAt:        now.UTC().Format(time.RFC3339Nano),
	}, nil
}

func executeAgentEvaluationCase(store *BookKnowledgeStore, pkg AgentPackage, evalCase AgentEvaluationCase) (bool, error) {
	input := strings.TrimSpace(evalCase.Input)
	if input == "" {
		return false, fmt.Errorf("input is required for behavioral metric %q", evalCase.Metric)
	}
	search, err := searchAgentPackageEvidence(store, pkg, input, pkg.RetrievalPolicy.MaxContextChunks)
	if err != nil {
		return false, err
	}
	citations, err := resolveAgentRuntimeCitations(store, search.Results)
	if err != nil {
		return false, err
	}

	switch evalCase.Metric {
	case "retrieval":
		observed := make([]string, 0, len(citations))
		for _, citation := range citations {
			if strings.TrimSpace(citation.ChunkID) != "" {
				observed = append(observed, citation.ChunkID)
			}
		}
		return agentEvaluationContainsExpected(observed, evalCase.ExpectedIDs), nil
	case "retrieval_precision":
		observed := make([]string, 0, len(citations))
		for _, citation := range citations {
			if strings.TrimSpace(citation.ChunkID) != "" {
				observed = append(observed, citation.ChunkID)
			}
		}
		return agentEvaluationExactIDs(observed, evalCase.ExpectedIDs), nil
	case "citations":
		response, _, chatErr := executeAgentEvaluationChat(store, pkg, input, evalCase.ModelOutput)
		if chatErr != nil {
			return false, chatErr
		}
		observed := make([]string, 0, len(response.Citations))
		for _, citation := range response.Citations {
			observed = append(observed, citation.CitationID)
		}
		return agentEvaluationContainsExpected(observed, evalCase.ExpectedIDs), nil
	case "faithfulness":
		observed := make([]string, 0, len(search.Results))
		byClaim := make(map[string]AgentPackageEvidence, len(search.Results))
		for _, evidence := range search.Results {
			observed = append(observed, evidence.ClaimID)
			byClaim[evidence.ClaimID] = evidence
		}
		if !agentEvaluationContainsExpected(observed, evalCase.ExpectedIDs) {
			return false, nil
		}
		for _, claimID := range uniqueTrimmedStrings(evalCase.ExpectedIDs) {
			evidence := byClaim[claimID]
			if strings.TrimSpace(evidence.Statement) == "" || len(evidence.CitationIDs) == 0 ||
				!strings.Contains(evidence.Statement, strings.TrimSpace(evalCase.ExpectedValue)) {
				return false, nil
			}
		}
		response, _, chatErr := executeAgentEvaluationChat(store, pkg, input, evalCase.ModelOutput)
		if chatErr != nil {
			return false, chatErr
		}
		if expected := strings.TrimSpace(evalCase.ExpectedValue); expected == "" ||
			response.Outcome != AgentTraceOutcomeCompleted || !strings.Contains(response.Answer, expected) {
			return false, nil
		}
		return len(response.Citations) > 0, nil
	case "abstention":
		response, _, chatErr := executeAgentEvaluationChat(store, pkg, input, evalCase.ModelOutput)
		return chatErr == nil && len(search.Results) == 0 && response.Outcome == AgentTraceOutcomeAbstained &&
			response.AbstentionReason == strings.TrimSpace(evalCase.ExpectedValue), chatErr
	case "tool_choice":
		actualTool := strings.TrimSpace(evalCase.ProposedTool)
		server, tool, ok := strings.Cut(actualTool, "/")
		if !ok || actualTool != strings.TrimSpace(evalCase.ExpectedValue) {
			return false, nil
		}
		arguments := make(map[string]any, len(evalCase.ProposedArguments))
		for key, value := range evalCase.ProposedArguments {
			arguments[key] = value
		}
		decision := EvaluateAgentToolCall(pkg, server, tool, arguments)
		return decision.Decision == AgentToolAllow, nil
	case "tool_arguments":
		actual := evalCase.ProposedArguments
		if !reflect.DeepEqual(actual, evalCase.ExpectedArguments) {
			return false, nil
		}
		server, tool, ok := strings.Cut(strings.TrimSpace(evalCase.ProposedTool), "/")
		if !ok || actual["package_id"] != pkg.PackageID || actual["package_version"] != pkg.Version ||
			actual["query"] != input || !agentPackagePinsRelease(pkg, actual["release_id"]) {
			return false, nil
		}
		arguments := make(map[string]any, len(actual))
		for key, value := range actual {
			arguments[key] = value
		}
		decision := EvaluateAgentToolCall(pkg, server, tool, arguments)
		return decision.Decision == AgentToolAllow, nil
	case "task_completion":
		response, _, chatErr := executeAgentEvaluationChat(store, pkg, input, evalCase.ModelOutput)
		return chatErr == nil && response.Outcome == AgentTraceOutcomeCompleted &&
			strings.Contains(response.Answer, strings.TrimSpace(evalCase.ExpectedValue)), chatErr
	case "latency":
		if evalCase.MaxLatencyMS <= 0 {
			return false, fmt.Errorf("max_latency_ms must be positive")
		}
		if evalCase.RecordedLatencyMS <= 0 {
			return false, fmt.Errorf("recorded_latency_ms must be positive")
		}
		_, _, chatErr := executeAgentEvaluationChat(store, pkg, input, evalCase.ModelOutput)
		return chatErr == nil && evalCase.RecordedLatencyMS <= evalCase.MaxLatencyMS, chatErr
	case "cost":
		if evalCase.MaxCostUSD <= 0 {
			return false, fmt.Errorf("max_cost_usd must be positive")
		}
		_, client, chatErr := executeAgentEvaluationChat(store, pkg, input, evalCase.ModelOutput)
		if chatErr != nil {
			return false, chatErr
		}
		return agentEvaluationObservedCostUSD(client.messages, client.output) <= evalCase.MaxCostUSD, nil
	default:
		return false, fmt.Errorf("unsupported behavioral metric %q", evalCase.Metric)
	}
}

type agentEvaluationModelClient struct {
	output   string
	messages []BookKnowledgeMessage
}

func (c *agentEvaluationModelClient) Chat(_ context.Context, _ BookTokenPlanConfig, messages []BookKnowledgeMessage) (string, error) {
	c.messages = append([]BookKnowledgeMessage(nil), messages...)
	return c.output, nil
}

func executeAgentEvaluationChat(store *BookKnowledgeStore, pkg AgentPackage, input, modelOutput string) (*AgentPackageChatResponse, *agentEvaluationModelClient, error) {
	if strings.TrimSpace(modelOutput) == "" {
		modelOutput = "evaluation model returned no grounded answer"
	}
	client := &agentEvaluationModelClient{output: modelOutput}
	cfg := BookTokenPlanConfig{Model: firstAgentPackageModel(pkg.ModelPolicy)}
	response, err := chatFinalizedAgentPackageWithClient(context.Background(), store, pkg, input, client, &cfg, time.Now().UTC(), false)
	return response, client, err
}

func agentEvaluationExactIDs(observed, expected []string) bool {
	left := sortedUniqueStrings(observed)
	right := sortedUniqueStrings(expected)
	return len(right) > 0 && reflect.DeepEqual(left, right)
}

func agentPackagePinsRelease(pkg AgentPackage, releaseID string) bool {
	for _, ref := range pkg.Releases {
		if ref.ReleaseID == releaseID {
			return true
		}
	}
	return false
}

func agentEvaluationObservedCostUSD(messages []BookKnowledgeMessage, output string) float64 {
	characters := len([]rune(output))
	for _, message := range messages {
		characters += len([]rune(message.Content))
	}
	return float64(characters) * agentRuntimeUSDPerTokenCeiling
}

func agentEvaluationContainsExpected(observed, expected []string) bool {
	wanted := uniqueTrimmedStrings(expected)
	if len(wanted) == 0 {
		return false
	}
	observedSet := stringBoolSet(observed...)
	for _, value := range wanted {
		if !observedSet[value] {
			return false
		}
	}
	return true
}

func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func agentEvaluationInputHash(packageHash string, suite AgentEvaluationSuite) (string, error) {
	seed := struct {
		PackageContentHash string               `json:"package_content_hash"`
		Suite              AgentEvaluationSuite `json:"suite"`
	}{PackageContentHash: packageHash, Suite: suite}
	payload, err := json.Marshal(seed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func agentEvaluationThresholdFailures(thresholds, metrics map[string]float64) []string {
	names := make([]string, 0, len(thresholds))
	for name := range thresholds {
		names = append(names, name)
	}
	sort.Strings(names)
	var failures []string
	for _, name := range names {
		score, ok := metrics[name]
		if !ok {
			failures = append(failures, fmt.Sprintf("missing required evaluation metric %q", name))
			continue
		}
		if score < thresholds[name] {
			failures = append(failures, fmt.Sprintf("evaluation metric %q scored %.4f below threshold %.4f", name, score, thresholds[name]))
		}
	}
	return failures
}

func (s *BookKnowledgeStore) AgentPackageEvaluationDir() string {
	return filepath.Join(s.AgentPackageDir(), "evaluations")
}

func (s *BookKnowledgeStore) AgentPackageEvaluationPath(packageContentHash string) string {
	name := strings.TrimPrefix(strings.TrimSpace(packageContentHash), "sha256:")
	return filepath.Join(s.AgentPackageEvaluationDir(), sanitizeBookKnowledgeID(name)+".json")
}

func (s *BookKnowledgeStore) AgentPackageEvaluationSuitePath(packageContentHash string) string {
	name := strings.TrimPrefix(strings.TrimSpace(packageContentHash), "sha256:")
	return filepath.Join(s.AgentPackageEvaluationDir(), sanitizeBookKnowledgeID(name)+".suite.json")
}

func (s *BookKnowledgeStore) SaveAgentPackageEvaluation(pkg AgentPackage, suite AgentEvaluationSuite, report AgentEvaluationReport) error {
	if report.SchemaVersion != AgentEvaluationReportSchemaVersion {
		return fmt.Errorf("schema_version must be %q", AgentEvaluationReportSchemaVersion)
	}
	if err := requireContractFields(map[string]string{
		"package_id":           report.PackageID,
		"package_content_hash": report.PackageContentHash,
		"suite_version":        report.SuiteVersion,
		"input_hash":           report.InputHash,
		"evaluator_version":    report.EvaluatorVersion,
		"evaluated_at":         report.EvaluatedAt,
	}); err != nil {
		return err
	}
	if len(report.Metrics) == 0 {
		return fmt.Errorf("metrics is required")
	}
	if report.PackageID != pkg.PackageID || report.PackageContentHash != pkg.ContentHash {
		return fmt.Errorf("evaluation report does not match package identity")
	}
	if report.SuiteVersion != suite.SuiteVersion || suite.SuiteVersion != pkg.EvaluationPolicy.SuiteVersion {
		return fmt.Errorf("evaluation suite does not match package policy")
	}
	if report.EvaluatorVersion != AgentDeterministicEvaluatorVersion {
		return fmt.Errorf("evaluation evaluator %q is not approved", report.EvaluatorVersion)
	}
	evaluatedAt, err := time.Parse(time.RFC3339Nano, report.EvaluatedAt)
	if err != nil {
		return fmt.Errorf("evaluation evaluated_at is invalid: %w", err)
	}
	expected, err := EvaluateAgentPackageDeterministically(s, pkg, suite, evaluatedAt)
	if err != nil {
		return err
	}
	if report.InputHash != expected.InputHash {
		return fmt.Errorf("evaluation input hash does not match trusted package and suite inputs")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.AgentPackageEvaluationDir(), os.ModePerm); err != nil {
		return err
	}
	var existing AgentEvaluationReport
	if err := readJSONFile(s.AgentPackageEvaluationPath(report.PackageContentHash), &existing); err == nil {
		if reflect.DeepEqual(existing, report) {
			return nil
		}
		return fmt.Errorf("agent package evaluation is immutable for content hash %q", report.PackageContentHash)
	} else if !os.IsNotExist(err) {
		return err
	}
	if !reflect.DeepEqual(expected, report) {
		return fmt.Errorf("evaluation report does not match trusted evaluator output")
	}
	var existingSuite AgentEvaluationSuite
	if err := readJSONFile(s.AgentPackageEvaluationSuitePath(report.PackageContentHash), &existingSuite); err == nil {
		if !reflect.DeepEqual(existingSuite, suite) {
			return fmt.Errorf("agent package evaluation suite is immutable for content hash %q", report.PackageContentHash)
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		suitePayload, encodeErr := encodeJSONFile(suite)
		if encodeErr != nil {
			return encodeErr
		}
		if writeErr := writeFileAtomically(s.AgentPackageEvaluationSuitePath(report.PackageContentHash), suitePayload); writeErr != nil {
			return writeErr
		}
	}
	payload, err := encodeJSONFile(report)
	if err != nil {
		return err
	}
	return writeFileAtomically(s.AgentPackageEvaluationPath(report.PackageContentHash), payload)
}

func (s *BookKnowledgeStore) LoadAgentPackageEvaluationSuite(packageContentHash string) (*AgentEvaluationSuite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if strings.TrimSpace(packageContentHash) == "" {
		return nil, fmt.Errorf("package_content_hash is required")
	}
	var suite AgentEvaluationSuite
	if err := readJSONFile(s.AgentPackageEvaluationSuitePath(packageContentHash), &suite); err != nil {
		return nil, err
	}
	return &suite, nil
}

func (s *BookKnowledgeStore) LoadAgentPackageEvaluation(packageContentHash string) (*AgentEvaluationReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if strings.TrimSpace(packageContentHash) == "" {
		return nil, fmt.Errorf("package_content_hash is required")
	}
	var report AgentEvaluationReport
	if err := readJSONFile(s.AgentPackageEvaluationPath(packageContentHash), &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func ValidateAgentPackageEvaluationGate(store *BookKnowledgeStore, pkg AgentPackage) error {
	if store == nil {
		return fmt.Errorf("evaluation store is required")
	}
	report, err := store.LoadAgentPackageEvaluation(pkg.ContentHash)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("evaluation report is required for package %q", pkg.PackageID)
		}
		return err
	}
	if report.PackageID != pkg.PackageID || report.PackageContentHash != pkg.ContentHash {
		return fmt.Errorf("evaluation report does not match package identity")
	}
	if report.SuiteVersion != pkg.EvaluationPolicy.SuiteVersion {
		return fmt.Errorf("evaluation suite %q does not match required suite %q", report.SuiteVersion, pkg.EvaluationPolicy.SuiteVersion)
	}
	if strings.TrimSpace(report.InputHash) == "" || strings.TrimSpace(report.EvaluatorVersion) == "" || strings.TrimSpace(report.EvaluatedAt) == "" {
		return fmt.Errorf("evaluation report provenance is incomplete")
	}
	if report.EvaluatorVersion != AgentDeterministicEvaluatorVersion {
		return fmt.Errorf("evaluation evaluator %q is not approved", report.EvaluatorVersion)
	}
	evaluatedAt, err := time.Parse(time.RFC3339Nano, report.EvaluatedAt)
	if err != nil {
		return fmt.Errorf("evaluation evaluated_at is invalid: %w", err)
	}
	suite, err := store.LoadAgentPackageEvaluationSuite(pkg.ContentHash)
	if err != nil {
		return fmt.Errorf("load trusted evaluation suite: %w", err)
	}
	expected, err := EvaluateAgentPackageDeterministically(store, pkg, *suite, evaluatedAt)
	if err != nil {
		return fmt.Errorf("recompute trusted evaluation: %w", err)
	}
	if report.InputHash != expected.InputHash {
		return fmt.Errorf("evaluation input hash does not match trusted package and suite inputs")
	}
	if !reflect.DeepEqual(*report, expected) {
		return fmt.Errorf("evaluation report does not match trusted evaluator output")
	}
	failures := agentEvaluationThresholdFailures(pkg.EvaluationPolicy.MinimumScores, report.Metrics)
	if len(failures) > 0 {
		return fmt.Errorf("agent package evaluation failed: %s", strings.Join(failures, "; "))
	}
	if !report.Passed {
		return fmt.Errorf("agent package evaluation failed")
	}
	return nil
}
