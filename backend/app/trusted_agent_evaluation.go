package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

var trustedEvidenceAuditEvaluationMetrics = []string{
	"adjudication_consistency",
	"source_independence",
	"conflict_detection",
	"report_citation_completeness",
	"safe_insufficiency",
	"proofroom_projection_completeness",
}

func (s *BookKnowledgeStore) TrustedAgentEvaluationSuiteDir() string {
	return filepath.Join(s.AgentPackageEvaluationDir(), "trusted")
}

func (s *BookKnowledgeStore) TrustedAgentEvaluationSuitePath(pkg AgentPackage) string {
	name := strings.TrimPrefix(strings.TrimSpace(pkg.ContentHash), "sha256:")
	return filepath.Join(
		s.TrustedAgentEvaluationSuiteDir(),
		sanitizeBookKnowledgeID(name)+"-"+sanitizeBookKnowledgeID(pkg.EvaluationPolicy.SuiteVersion)+".json",
	)
}

func ValidateTrustedAgentEvaluationSuite(pkg AgentPackage, suite AgentEvaluationSuite) error {
	switch pkg.SchemaVersion {
	case AgentPackageSchemaVersionV2:
	case AgentPackageSchemaVersionV3:
		if err := validateTrustedResearchEvaluationSuite(pkg, suite); err != nil {
			return err
		}
		if pkg.EvidencePolicy == nil {
			return nil
		}
	default:
		return fmt.Errorf("trusted evidence audit suites require an agent-package.v2 package")
	}
	if strings.TrimSpace(pkg.ContentHash) == "" {
		return fmt.Errorf("trusted evaluation suite requires package content_hash")
	}
	if suite.SchemaVersion != AgentEvaluationSchemaVersion ||
		suite.SuiteVersion != pkg.EvaluationPolicy.SuiteVersion {
		return fmt.Errorf("trusted evaluation suite does not match package policy")
	}
	if len(suite.Cases) == 0 {
		return fmt.Errorf("trusted evaluation suite cases are required")
	}
	seenCases := make(map[string]bool, len(suite.Cases))
	seenMetrics := make(map[string]bool, len(trustedEvidenceAuditEvaluationMetrics))
	hasPositiveGold := false
	hasConflictGold := false
	for index, evalCase := range suite.Cases {
		caseID := strings.TrimSpace(evalCase.CaseID)
		if caseID == "" || seenCases[caseID] {
			return fmt.Errorf("trusted evaluation suite cases[%d] has an empty or duplicate case_id", index)
		}
		seenCases[caseID] = true
		if !isEvidenceAuditEvaluationMetric(evalCase.Metric) {
			continue
		}
		if strings.TrimSpace(evalCase.AuditID) != "" {
			return fmt.Errorf("trusted evaluation suite case %q must not pin a runtime audit_id", caseID)
		}
		seenMetrics[evalCase.Metric] = true
		switch evalCase.Metric {
		case "adjudication_consistency":
			for _, claim := range evalCase.ExpectedClaims {
				if claim.Verdict == EvidenceAuditVerdictSupported ||
					claim.Verdict == EvidenceAuditVerdictContradicted ||
					claim.Verdict == EvidenceAuditVerdictMixed {
					hasPositiveGold = true
				}
			}
		case "conflict_detection":
			for _, claim := range evalCase.ExpectedClaims {
				if claim.Conflict != nil && *claim.Conflict &&
					(claim.Verdict == EvidenceAuditVerdictMixed ||
						claim.Verdict == EvidenceAuditVerdictContradicted) {
					hasConflictGold = true
				}
			}
		}
	}
	for _, metric := range trustedEvidenceAuditEvaluationMetrics {
		if !seenMetrics[metric] {
			return fmt.Errorf("trusted evaluation suite is missing evidence metric %q", metric)
		}
	}
	if !hasPositiveGold {
		return fmt.Errorf("trusted evaluation suite requires a non-insufficient adjudication gold case")
	}
	if !hasConflictGold {
		return fmt.Errorf("trusted evaluation suite requires a positive conflict gold case")
	}
	return nil
}

func (s *BookKnowledgeStore) SaveTrustedAgentEvaluationSuite(pkg AgentPackage, suite AgentEvaluationSuite) error {
	if s == nil {
		return fmt.Errorf("trusted evaluation store is required")
	}
	if err := ValidateTrustedAgentEvaluationSuite(pkg, suite); err != nil {
		return err
	}
	payload, err := encodeJSONFile(suite)
	if err != nil {
		return err
	}
	path := s.TrustedAgentEvaluationSuitePath(pkg)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.TrustedAgentEvaluationSuiteDir(), 0o700); err != nil {
		return err
	}
	var existing AgentEvaluationSuite
	if err := readJSONFile(path, &existing); err == nil {
		if reflect.DeepEqual(existing, suite) {
			return os.Chmod(path, 0o600)
		}
		return fmt.Errorf("trusted evaluation suite is immutable for package content hash %q", pkg.ContentHash)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := writeFileAtomically(path, payload); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (s *BookKnowledgeStore) LoadTrustedAgentEvaluationSuite(pkg AgentPackage) (*AgentEvaluationSuite, error) {
	if s == nil {
		return nil, fmt.Errorf("trusted evaluation store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var suite AgentEvaluationSuite
	if err := readJSONFile(s.TrustedAgentEvaluationSuitePath(pkg), &suite); err != nil {
		return nil, err
	}
	if err := ValidateTrustedAgentEvaluationSuite(pkg, suite); err != nil {
		return nil, err
	}
	return &suite, nil
}

func (s *BookKnowledgeStore) ResolveTrustedAgentEvaluationSuite(
	pkg AgentPackage,
	submitted AgentEvaluationSuite,
) (AgentEvaluationSuite, string, error) {
	trusted, err := s.LoadTrustedAgentEvaluationSuite(pkg)
	if err != nil {
		return AgentEvaluationSuite{}, "", fmt.Errorf("load trusted evaluation suite: %w", err)
	}
	if submitted.SchemaVersion != trusted.SchemaVersion ||
		submitted.SuiteVersion != trusted.SuiteVersion ||
		len(submitted.Cases) != len(trusted.Cases) {
		return AgentEvaluationSuite{}, "", fmt.Errorf("submitted suite does not match trusted evaluation suite identity")
	}
	submittedByID := make(map[string]AgentEvaluationCase, len(submitted.Cases))
	for _, evalCase := range submitted.Cases {
		caseID := strings.TrimSpace(evalCase.CaseID)
		if caseID == "" || submittedByID[caseID].CaseID != "" {
			return AgentEvaluationSuite{}, "", fmt.Errorf("submitted suite has an empty or duplicate trusted evaluation suite case")
		}
		submittedByID[caseID] = evalCase
	}
	resolved := *trusted
	if pkg.SchemaVersion == AgentPackageSchemaVersionV3 {
		resolved, err = resolveTrustedResearchEvaluationSuite(*trusted, submitted)
		if err != nil {
			return AgentEvaluationSuite{}, "", err
		}
		if pkg.EvidencePolicy == nil {
			payload, encodeErr := encodeJSONFile(*trusted)
			if encodeErr != nil {
				return AgentEvaluationSuite{}, "", encodeErr
			}
			return resolved, sha256Fingerprint(payload), nil
		}
	}
	resolved.Cases = append([]AgentEvaluationCase(nil), trusted.Cases...)
	for index, trustedCase := range trusted.Cases {
		submittedCase, ok := submittedByID[trustedCase.CaseID]
		if !ok || submittedCase.Metric != trustedCase.Metric {
			return AgentEvaluationSuite{}, "", fmt.Errorf("submitted suite case %q does not match trusted evaluation suite", trustedCase.CaseID)
		}
		if isEvidenceAuditEvaluationMetric(trustedCase.Metric) {
			auditID := strings.TrimSpace(submittedCase.AuditID)
			submittedCase.AuditID = ""
			if auditID == "" || !reflect.DeepEqual(submittedCase, trustedCase) {
				return AgentEvaluationSuite{}, "", fmt.Errorf("submitted suite case %q modifies trusted evaluation suite gold", trustedCase.CaseID)
			}
			resolved.Cases[index].AuditID = auditID
			continue
		}
		if !reflect.DeepEqual(submittedCase, trustedCase) {
			return AgentEvaluationSuite{}, "", fmt.Errorf("submitted suite case %q modifies trusted evaluation suite behavior", trustedCase.CaseID)
		}
	}
	if err := validateResolvedTrustedEvidenceCoverage(resolved); err != nil {
		return AgentEvaluationSuite{}, "", err
	}
	payload, err := encodeJSONFile(*trusted)
	if err != nil {
		return AgentEvaluationSuite{}, "", err
	}
	return resolved, sha256Fingerprint(payload), nil
}

func validateResolvedTrustedEvidenceCoverage(suite AgentEvaluationSuite) error {
	auditIDs := make(map[string]string)
	for _, evalCase := range suite.Cases {
		switch evalCase.Metric {
		case "adjudication_consistency", "conflict_detection", "safe_insufficiency":
			auditIDs[evalCase.Metric] = strings.TrimSpace(evalCase.AuditID)
		}
	}
	seen := map[string]string{}
	for _, metric := range []string{"adjudication_consistency", "conflict_detection", "safe_insufficiency"} {
		auditID := auditIDs[metric]
		if auditID == "" {
			return fmt.Errorf("trusted evaluation suite metric %q requires an audit_id", metric)
		}
		if prior := seen[auditID]; prior != "" {
			return fmt.Errorf("trusted evaluation suite requires distinct %s and %s audit runs", prior, metric)
		}
		seen[auditID] = metric
	}
	return nil
}
