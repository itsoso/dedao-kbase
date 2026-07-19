package app

import (
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
	ExpectedIDs       []string          `json:"expected_ids,omitempty"`
	ObservedIDs       []string          `json:"-"`
	ExpectedValue     string            `json:"expected_value,omitempty"`
	ObservedValue     string            `json:"-"`
	ExpectedArguments map[string]string `json:"expected_arguments,omitempty"`
	ObservedArguments map[string]string `json:"-"`
}

type AgentEvaluationReport struct {
	SchemaVersion      string             `json:"schema_version"`
	PackageID          string             `json:"package_id"`
	PackageContentHash string             `json:"package_content_hash"`
	SuiteVersion       string             `json:"suite_version"`
	InputHash          string             `json:"input_hash"`
	EvaluatorVersion   string             `json:"evaluator_version"`
	Metrics            map[string]float64 `json:"metrics"`
	Passed             bool               `json:"passed"`
	Failures           []string           `json:"failures,omitempty"`
	EvaluatedAt        string             `json:"evaluated_at"`
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
	facts, err := loadAgentEvaluationFacts(store, pkg)
	if err != nil {
		return AgentEvaluationReport{}, err
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
		if deterministicAgentEvaluationCasePasses(pkg, facts, evalCase) {
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
	return AgentEvaluationReport{
		SchemaVersion:      AgentEvaluationReportSchemaVersion,
		PackageID:          pkg.PackageID,
		PackageContentHash: pkg.ContentHash,
		SuiteVersion:       suite.SuiteVersion,
		InputHash:          inputHash,
		EvaluatorVersion:   AgentDeterministicEvaluatorVersion,
		Metrics:            metrics,
		Passed:             len(failures) == 0,
		Failures:           failures,
		EvaluatedAt:        now.UTC().Format(time.RFC3339Nano),
	}, nil
}

type agentEvaluationFacts struct {
	chunkIDs    map[string]bool
	citationIDs map[string]bool
	claimIDs    map[string]bool
	releaseIDs  map[string]bool
	tools       map[string]bool
	abstentions map[string]bool
}

func loadAgentEvaluationFacts(store *BookKnowledgeStore, pkg AgentPackage) (agentEvaluationFacts, error) {
	facts := agentEvaluationFacts{
		chunkIDs: make(map[string]bool), citationIDs: make(map[string]bool),
		claimIDs: make(map[string]bool), releaseIDs: make(map[string]bool),
		tools: make(map[string]bool), abstentions: make(map[string]bool),
	}
	for _, reason := range uniqueTrimmedStrings(pkg.SafetyPolicy.AbstentionReasons) {
		facts.abstentions[reason] = true
	}
	for _, rule := range pkg.ToolPolicy.Tools {
		if rule.Decision == AgentToolAllow {
			facts.tools[strings.TrimSpace(rule.MCPServer)+"/"+strings.TrimSpace(rule.ToolName)] = true
		}
	}
	for _, ref := range pkg.Releases {
		release, err := store.LoadKnowledgeRelease(ref.ReleaseID)
		if err != nil {
			return agentEvaluationFacts{}, fmt.Errorf("load evaluation release %q: %w", ref.ReleaseID, err)
		}
		if release.ContentHash != ref.ContentHash {
			return agentEvaluationFacts{}, fmt.Errorf("evaluation release %q content hash mismatch", ref.ReleaseID)
		}
		facts.releaseIDs[ref.ReleaseID] = true
		for _, citation := range release.Citations {
			facts.citationIDs[citation.CitationID] = true
			if strings.TrimSpace(citation.ChunkID) != "" {
				facts.chunkIDs[citation.ChunkID] = true
			}
		}
		if release.Analysis != nil {
			for _, claim := range release.Analysis.Claims {
				facts.claimIDs[claim.ID] = true
			}
		}
	}
	return facts, nil
}

func deterministicAgentEvaluationCasePasses(pkg AgentPackage, facts agentEvaluationFacts, evalCase AgentEvaluationCase) bool {
	switch evalCase.Metric {
	case "retrieval", "citations", "faithfulness":
		if len(evalCase.ExpectedIDs) == 0 {
			return false
		}
		observed := facts.chunkIDs
		if evalCase.Metric == "citations" {
			observed = facts.citationIDs
		} else if evalCase.Metric == "faithfulness" {
			observed = facts.claimIDs
		}
		for _, expected := range uniqueTrimmedStrings(evalCase.ExpectedIDs) {
			if !observed[expected] {
				return false
			}
		}
		return true
	case "abstention":
		return facts.abstentions[strings.TrimSpace(evalCase.ExpectedValue)]
	case "tool_choice":
		return facts.tools[strings.TrimSpace(evalCase.ExpectedValue)]
	case "tool_arguments":
		if len(evalCase.ExpectedArguments) == 0 || evalCase.ExpectedArguments["package_id"] != pkg.PackageID ||
			evalCase.ExpectedArguments["package_version"] != pkg.Version ||
			!facts.releaseIDs[evalCase.ExpectedArguments["release_id"]] {
			return false
		}
		fingerprint := strings.TrimPrefix(evalCase.ExpectedArguments["query_fingerprint"], "sha256:")
		return len(fingerprint) == 64 && isLowerHex(fingerprint)
	default:
		return false
	}
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
