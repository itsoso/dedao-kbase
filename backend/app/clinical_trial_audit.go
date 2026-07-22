package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

const (
	ClinicalTrialAuditSchemaVersion    = "clinical-trial-audit.v1"
	ClinicalTrialAuditRunSchemaVersion = "clinical-trial-audit-run.v1"

	ClinicalTrialInputNCTID = "nct_id"
	ClinicalTrialInputDOI   = "doi"
	ClinicalTrialInputPMID  = "pmid"
	ClinicalTrialInputClaim = "claim"

	ClinicalTrialFindingRegisteredFact           = "registered_fact"
	ClinicalTrialFindingPublicationClaim         = "publication_claim"
	ClinicalTrialFindingDeterministicDiscrepancy = "deterministic_discrepancy"
	ClinicalTrialFindingModelInterpretation      = "model_interpretation"
	ClinicalTrialFindingUnresolvedConflict       = "unresolved_conflict"

	ClinicalTrialAuditRunQueued         = "queued"
	ClinicalTrialAuditRunCollecting     = "collecting"
	ClinicalTrialAuditRunComparing      = "comparing"
	ClinicalTrialAuditRunReasoning      = "reasoning"
	ClinicalTrialAuditRunAwaitingReview = "awaiting_review"
	ClinicalTrialAuditRunCompleted      = "completed"
	ClinicalTrialAuditRunFailed         = "failed"
	ClinicalTrialAuditRunAbstained      = "abstained"
)

var (
	clinicalTrialNCTPattern  = regexp.MustCompile(`^NCT[0-9]{8}$`)
	clinicalTrialDOIPattern  = regexp.MustCompile(`^10\.[0-9]{4,9}/\S+$`)
	clinicalTrialPMIDPattern = regexp.MustCompile(`^[0-9]+$`)
)

type ClinicalTrialAuditRequest struct {
	InputType       string `json:"input_type"`
	Input           string `json:"input"`
	NormalizedInput string `json:"normalized_input"`
	InputHash       string `json:"input_hash"`
}

type ClinicalTrialSourceSnapshot struct {
	SourceType        string `json:"source_type"`
	CanonicalID       string `json:"canonical_id"`
	RetrievedAt       string `json:"retrieved_at"`
	UpstreamUpdatedAt string `json:"upstream_updated_at,omitempty"`
	ContentHash       string `json:"content_hash"`
	Fingerprint       string `json:"fingerprint"`
	LicenseScope      string `json:"license_scope"`
}

type ClinicalTrialAuditCitation struct {
	CitationID        string `json:"citation_id"`
	SourceFingerprint string `json:"source_fingerprint"`
	Locator           string `json:"locator"`
}

type ClinicalTrialFinding struct {
	FindingID   string   `json:"finding_id"`
	Class       string   `json:"class"`
	Summary     string   `json:"summary"`
	CitationIDs []string `json:"citation_ids"`
}

type ClinicalTrialAudit struct {
	SchemaVersion string                        `json:"schema_version"`
	AuditID       string                        `json:"audit_id"`
	Request       ClinicalTrialAuditRequest     `json:"request"`
	Sources       []ClinicalTrialSourceSnapshot `json:"sources"`
	Findings      []ClinicalTrialFinding        `json:"findings"`
	Citations     []ClinicalTrialAuditCitation  `json:"citations"`
	Confidence    float64                       `json:"confidence"`
	Limitations   []string                      `json:"limitations"`
	CompletedAt   string                        `json:"completed_at"`
}

type ClinicalTrialAuditRun struct {
	SchemaVersion string                    `json:"schema_version"`
	RunID         string                    `json:"run_id"`
	State         string                    `json:"state"`
	Request       ClinicalTrialAuditRequest `json:"request"`
	Audit         *ClinicalTrialAudit       `json:"audit,omitempty"`
	Error         *string                   `json:"error,omitempty"`
	CreatedAt     string                    `json:"created_at,omitempty"`
	UpdatedAt     string                    `json:"updated_at,omitempty"`
}

func FinalizeClinicalTrialAuditRequest(request ClinicalTrialAuditRequest) (ClinicalTrialAuditRequest, error) {
	normalized, err := normalizeClinicalTrialAuditInput(request.InputType, request.Input)
	if err != nil {
		return ClinicalTrialAuditRequest{}, err
	}
	request.InputType = strings.TrimSpace(request.InputType)
	request.Input = strings.TrimSpace(request.Input)
	request.NormalizedInput = normalized
	request.InputHash = hashClinicalTrialValue(request.InputType + "\x00" + normalized)
	return request, nil
}

func FinalizeClinicalTrialSourceSnapshot(snapshot ClinicalTrialSourceSnapshot) (ClinicalTrialSourceSnapshot, error) {
	if err := validateClinicalTrialSourceFields(snapshot); err != nil {
		return ClinicalTrialSourceSnapshot{}, err
	}
	retrievedAt, err := canonicalClinicalTrialTimestamp("retrieved_at", snapshot.RetrievedAt, true)
	if err != nil {
		return ClinicalTrialSourceSnapshot{}, err
	}
	upstreamUpdatedAt, err := canonicalClinicalTrialTimestamp("upstream_updated_at", snapshot.UpstreamUpdatedAt, false)
	if err != nil {
		return ClinicalTrialSourceSnapshot{}, err
	}
	payload := struct {
		SourceType        string `json:"source_type"`
		CanonicalID       string `json:"canonical_id"`
		UpstreamUpdatedAt string `json:"upstream_updated_at,omitempty"`
		ContentHash       string `json:"content_hash"`
		LicenseScope      string `json:"license_scope"`
	}{
		SourceType:        strings.TrimSpace(snapshot.SourceType),
		CanonicalID:       strings.TrimSpace(snapshot.CanonicalID),
		UpstreamUpdatedAt: upstreamUpdatedAt,
		ContentHash:       strings.TrimSpace(snapshot.ContentHash),
		LicenseScope:      strings.TrimSpace(snapshot.LicenseScope),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ClinicalTrialSourceSnapshot{}, err
	}
	snapshot.SourceType = payload.SourceType
	snapshot.CanonicalID = payload.CanonicalID
	snapshot.UpstreamUpdatedAt = payload.UpstreamUpdatedAt
	snapshot.ContentHash = payload.ContentHash
	snapshot.LicenseScope = payload.LicenseScope
	snapshot.RetrievedAt = retrievedAt
	snapshot.Fingerprint = hashClinicalTrialValue(string(encoded))
	return snapshot, nil
}

func FinalizeClinicalTrialAudit(audit ClinicalTrialAudit) (ClinicalTrialAudit, error) {
	completedAt, err := canonicalClinicalTrialTimestamp("completed_at", audit.CompletedAt, true)
	if err != nil {
		return ClinicalTrialAudit{}, err
	}
	audit.CompletedAt = completedAt
	if err := ValidateClinicalTrialAudit(audit); err != nil {
		return ClinicalTrialAudit{}, err
	}
	return audit, nil
}

func ValidateClinicalTrialAudit(audit ClinicalTrialAudit) error {
	return validateClinicalTrialAudit(audit, true)
}

func ValidateClinicalTrialAuditRun(run ClinicalTrialAuditRun) error {
	if run.SchemaVersion != ClinicalTrialAuditRunSchemaVersion {
		return fmt.Errorf("schema_version must be %q", ClinicalTrialAuditRunSchemaVersion)
	}
	if strings.TrimSpace(run.RunID) == "" {
		return fmt.Errorf("run_id is required")
	}
	if err := validateClinicalTrialAuditRequest(run.Request); err != nil {
		return fmt.Errorf("request: %w", err)
	}
	switch run.State {
	case ClinicalTrialAuditRunQueued, ClinicalTrialAuditRunCollecting, ClinicalTrialAuditRunComparing,
		ClinicalTrialAuditRunReasoning, ClinicalTrialAuditRunAwaitingReview:
		if run.Audit != nil || run.Error != nil {
			return fmt.Errorf("nonterminal run cannot contain audit or error")
		}
		return nil
	case ClinicalTrialAuditRunCompleted:
		if run.Audit == nil {
			return fmt.Errorf("completed run requires an audit")
		}
		if run.Error != nil {
			return fmt.Errorf("completed run cannot contain an error")
		}
		if err := validateClinicalTrialRunAuditRequest(run.Request, run.Audit.Request); err != nil {
			return err
		}
		return validateClinicalTrialAudit(*run.Audit, true)
	case ClinicalTrialAuditRunAbstained:
		if run.Audit == nil {
			return fmt.Errorf("abstained run requires an audit")
		}
		if run.Error != nil {
			return fmt.Errorf("abstained run cannot contain an error")
		}
		if err := validateClinicalTrialRunAuditRequest(run.Request, run.Audit.Request); err != nil {
			return err
		}
		if len(run.Audit.Findings) != 0 {
			return fmt.Errorf("abstained run audit findings must be empty")
		}
		return validateClinicalTrialAudit(*run.Audit, false)
	case ClinicalTrialAuditRunFailed:
		if run.Error == nil || strings.TrimSpace(*run.Error) == "" {
			return fmt.Errorf("failed run requires an error")
		}
		if run.Audit != nil {
			return fmt.Errorf("failed run cannot contain an audit")
		}
		return nil
	default:
		return fmt.Errorf("unsupported clinical trial audit run state %q", run.State)
	}
}

func validateClinicalTrialAudit(audit ClinicalTrialAudit, requireFindings bool) error {
	if audit.SchemaVersion != ClinicalTrialAuditSchemaVersion {
		return fmt.Errorf("schema_version must be %q", ClinicalTrialAuditSchemaVersion)
	}
	if strings.TrimSpace(audit.AuditID) == "" {
		return fmt.Errorf("audit_id is required")
	}
	if err := validateClinicalTrialAuditRequest(audit.Request); err != nil {
		return fmt.Errorf("request: %w", err)
	}
	if len(audit.Sources) == 0 {
		return fmt.Errorf("sources are required")
	}
	sourceFingerprints := make(map[string]struct{}, len(audit.Sources))
	for index, source := range audit.Sources {
		finalized, err := FinalizeClinicalTrialSourceSnapshot(source)
		if err != nil {
			return fmt.Errorf("sources[%d]: %w", index, err)
		}
		if source.Fingerprint != finalized.Fingerprint {
			return fmt.Errorf("sources[%d].fingerprint does not match source content", index)
		}
		if _, exists := sourceFingerprints[source.Fingerprint]; exists {
			return fmt.Errorf("duplicate source fingerprint %q", source.Fingerprint)
		}
		sourceFingerprints[source.Fingerprint] = struct{}{}
	}
	if len(audit.Citations) == 0 {
		return fmt.Errorf("citations are required")
	}
	citationIDs := make(map[string]struct{}, len(audit.Citations))
	for index, citation := range audit.Citations {
		if strings.TrimSpace(citation.CitationID) == "" || strings.TrimSpace(citation.SourceFingerprint) == "" || strings.TrimSpace(citation.Locator) == "" {
			return fmt.Errorf("citations[%d] requires citation_id, source_fingerprint, and locator", index)
		}
		if _, exists := citationIDs[citation.CitationID]; exists {
			return fmt.Errorf("duplicate citation ID %q", citation.CitationID)
		}
		if _, exists := sourceFingerprints[citation.SourceFingerprint]; !exists {
			return fmt.Errorf("citation %q references unknown source fingerprint", citation.CitationID)
		}
		citationIDs[citation.CitationID] = struct{}{}
	}
	if requireFindings && len(audit.Findings) == 0 {
		return fmt.Errorf("findings are required")
	}
	findingIDs := make(map[string]struct{}, len(audit.Findings))
	for index, finding := range audit.Findings {
		if strings.TrimSpace(finding.FindingID) == "" || strings.TrimSpace(finding.Summary) == "" {
			return fmt.Errorf("findings[%d] requires finding_id and summary", index)
		}
		if !isClinicalTrialFindingClass(finding.Class) {
			return fmt.Errorf("unsupported finding class %q", finding.Class)
		}
		if _, exists := findingIDs[finding.FindingID]; exists {
			return fmt.Errorf("duplicate finding ID %q", finding.FindingID)
		}
		findingIDs[finding.FindingID] = struct{}{}
		if len(finding.CitationIDs) == 0 {
			return fmt.Errorf("finding %q requires citations", finding.FindingID)
		}
		seen := make(map[string]struct{}, len(finding.CitationIDs))
		for _, citationID := range finding.CitationIDs {
			if _, exists := citationIDs[citationID]; !exists {
				return fmt.Errorf("finding %q references unknown citation %q", finding.FindingID, citationID)
			}
			if _, exists := seen[citationID]; exists {
				return fmt.Errorf("finding %q has duplicate citation %q", finding.FindingID, citationID)
			}
			seen[citationID] = struct{}{}
		}
	}
	if math.IsNaN(audit.Confidence) || math.IsInf(audit.Confidence, 0) || audit.Confidence < 0 || audit.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if err := validateClinicalTrialLimitations(audit.Limitations); err != nil {
		return err
	}
	if _, err := canonicalClinicalTrialTimestamp("completed_at", audit.CompletedAt, true); err != nil {
		return err
	}
	return nil
}

func validateClinicalTrialAuditRequest(request ClinicalTrialAuditRequest) error {
	finalized, err := FinalizeClinicalTrialAuditRequest(request)
	if err != nil {
		return err
	}
	if request.NormalizedInput != finalized.NormalizedInput {
		return fmt.Errorf("normalized_input does not match normalized request input")
	}
	if request.InputHash != finalized.InputHash {
		return fmt.Errorf("input_hash does not match normalized request input")
	}
	return nil
}

func normalizeClinicalTrialAuditInput(inputType, input string) (string, error) {
	inputType = strings.TrimSpace(inputType)
	value := strings.TrimSpace(input)
	switch inputType {
	case ClinicalTrialInputNCTID:
		value = strings.ToUpper(strings.Join(strings.Fields(value), ""))
		if !clinicalTrialNCTPattern.MatchString(value) {
			return "", fmt.Errorf("nct_id must match NCT followed by eight digits")
		}
	case ClinicalTrialInputDOI:
		value = strings.ToLower(value)
		for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "doi:"} {
			value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
		if !clinicalTrialDOIPattern.MatchString(value) {
			return "", fmt.Errorf("doi is invalid")
		}
	case ClinicalTrialInputPMID:
		value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "pmid:"))
		value = strings.Join(strings.Fields(value), "")
		if !clinicalTrialPMIDPattern.MatchString(value) {
			return "", fmt.Errorf("pmid must contain only digits")
		}
		value = strings.TrimLeft(value, "0")
		if value == "" {
			return "", fmt.Errorf("pmid must be positive")
		}
	case ClinicalTrialInputClaim:
		value = norm.NFC.String(value)
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			return "", fmt.Errorf("claim is required")
		}
	default:
		return "", fmt.Errorf("unsupported clinical trial audit input type %q", inputType)
	}
	return value, nil
}

func validateClinicalTrialSourceFields(snapshot ClinicalTrialSourceSnapshot) error {
	if strings.TrimSpace(snapshot.SourceType) == "" || strings.TrimSpace(snapshot.CanonicalID) == "" ||
		strings.TrimSpace(snapshot.RetrievedAt) == "" || strings.TrimSpace(snapshot.ContentHash) == "" ||
		strings.TrimSpace(snapshot.LicenseScope) == "" {
		return fmt.Errorf("source_type, canonical_id, retrieved_at, content_hash, and license_scope are required")
	}
	if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(strings.TrimSpace(snapshot.ContentHash)) {
		return fmt.Errorf("content_hash must be a sha256 fingerprint")
	}
	if _, err := canonicalClinicalTrialTimestamp("retrieved_at", snapshot.RetrievedAt, true); err != nil {
		return err
	}
	if _, err := canonicalClinicalTrialTimestamp("upstream_updated_at", snapshot.UpstreamUpdatedAt, false); err != nil {
		return err
	}
	return nil
}

func validateClinicalTrialRunAuditRequest(runRequest, auditRequest ClinicalTrialAuditRequest) error {
	if runRequest.InputType != auditRequest.InputType ||
		runRequest.NormalizedInput != auditRequest.NormalizedInput ||
		runRequest.InputHash != auditRequest.InputHash {
		return fmt.Errorf("run request does not match audit request")
	}
	return nil
}

func canonicalClinicalTrialTimestamp(field, value string, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return "", fmt.Errorf("%s is required", field)
		}
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", fmt.Errorf("%s must be RFC3339: %w", field, err)
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}

func validateClinicalTrialLimitations(limitations []string) error {
	if len(limitations) == 0 {
		return fmt.Errorf("limitations are required")
	}
	seen := make(map[string]struct{}, len(limitations))
	for index, limitation := range limitations {
		trimmed := strings.TrimSpace(limitation)
		if trimmed == "" {
			return fmt.Errorf("limitations[%d] must not be empty", index)
		}
		if limitation != trimmed {
			return fmt.Errorf("limitations[%d] must not contain surrounding whitespace", index)
		}
		if _, exists := seen[limitation]; exists {
			return fmt.Errorf("limitations must not contain duplicates")
		}
		seen[limitation] = struct{}{}
	}
	return nil
}

func isClinicalTrialFindingClass(class string) bool {
	switch class {
	case ClinicalTrialFindingRegisteredFact, ClinicalTrialFindingPublicationClaim,
		ClinicalTrialFindingDeterministicDiscrepancy, ClinicalTrialFindingModelInterpretation,
		ClinicalTrialFindingUnresolvedConflict:
		return true
	default:
		return false
	}
}

func hashClinicalTrialValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
