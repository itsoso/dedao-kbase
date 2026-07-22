package app

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

func TestClinicalTrialAuditRequestNormalizesSupportedInputsAndHashes(t *testing.T) {
	tests := []struct {
		name       string
		inputType  string
		first      string
		second     string
		normalized string
	}{
		{name: "nct_id", inputType: ClinicalTrialInputNCTID, first: " nct01234567 ", second: "NCT01234567", normalized: "NCT01234567"},
		{name: "doi", inputType: ClinicalTrialInputDOI, first: " https://doi.org/10.1000/Trial.1 ", second: "doi:10.1000/trial.1", normalized: "10.1000/trial.1"},
		{name: "pmid", inputType: ClinicalTrialInputPMID, first: " PMID: 12345678 ", second: "12345678", normalized: "12345678"},
		{name: "claim", inputType: ClinicalTrialInputClaim, first: "  Primary endpoint\n was met. ", second: "Primary endpoint was met.", normalized: "Primary endpoint was met."},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: test.inputType, Input: test.first})
			if err != nil {
				t.Fatalf("FinalizeClinicalTrialAuditRequest(first) error = %v", err)
			}
			second, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: test.inputType, Input: test.second})
			if err != nil {
				t.Fatalf("FinalizeClinicalTrialAuditRequest(second) error = %v", err)
			}
			if first.NormalizedInput != test.normalized {
				t.Fatalf("normalized input = %q, want %q", first.NormalizedInput, test.normalized)
			}
			if first.InputHash == "" || !strings.HasPrefix(first.InputHash, "sha256:") || first.InputHash != second.InputHash {
				t.Fatalf("input hashes = %q and %q", first.InputHash, second.InputHash)
			}
		})
	}
}

func TestClinicalTrialAuditRequestNormalizesUnicodeClaimsAndPMIDLeadingZeros(t *testing.T) {
	composed, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputClaim, Input: "Caf\u00e9 endpoint"})
	if err != nil {
		t.Fatal(err)
	}
	decomposed, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputClaim, Input: "  Cafe\u0301\nendpoint  "})
	if err != nil {
		t.Fatal(err)
	}
	if decomposed.NormalizedInput != "Caf\u00e9 endpoint" || decomposed.InputHash != composed.InputHash {
		t.Fatalf("decomposed request = %#v, composed = %#v", decomposed, composed)
	}

	canonical, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputPMID, Input: "PMID: 000123"})
	if err != nil {
		t.Fatal(err)
	}
	if canonical.NormalizedInput != "123" {
		t.Fatalf("normalized PMID = %q", canonical.NormalizedInput)
	}
	if _, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputPMID, Input: "0000"}); err == nil {
		t.Fatal("all-zero PMID error = nil")
	}
}

func TestClinicalTrialSourceSnapshotFingerprintIsStable(t *testing.T) {
	base := ClinicalTrialSourceSnapshot{
		SourceType:        "clinicaltrials.gov",
		CanonicalID:       "NCT01234567",
		UpstreamUpdatedAt: "2026-07-20T12:00:00Z",
		ContentHash:       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		LicenseScope:      "public_metadata",
		RetrievedAt:       "2026-07-21T12:00:00Z",
	}
	first, err := FinalizeClinicalTrialSourceSnapshot(base)
	if err != nil {
		t.Fatalf("FinalizeClinicalTrialSourceSnapshot() error = %v", err)
	}
	base.RetrievedAt = "2026-07-22T12:00:00Z"
	second, err := FinalizeClinicalTrialSourceSnapshot(base)
	if err != nil {
		t.Fatalf("FinalizeClinicalTrialSourceSnapshot(second) error = %v", err)
	}
	if first.Fingerprint == "" || !strings.HasPrefix(first.Fingerprint, "sha256:") || first.Fingerprint != second.Fingerprint {
		t.Fatalf("source fingerprints = %q and %q", first.Fingerprint, second.Fingerprint)
	}
}

func TestClinicalTrialSourceSnapshotProtectsRetrievalProvenanceSeparately(t *testing.T) {
	base := ClinicalTrialSourceSnapshot{
		SourceType:        ClinicalTrialsGovStudySourceType,
		CanonicalID:       "NCT01234567",
		UpstreamUpdatedAt: "2026-07-20T12:00:00Z",
		ContentHash:       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		LicenseScope:      "public_metadata",
		RetrievedAt:       "2026-07-21T12:00:00Z",
		DataTimestamp:     "2026-07-21T15:04:05Z",
	}
	first, err := FinalizeClinicalTrialSourceSnapshot(base)
	if err != nil {
		t.Fatal(err)
	}
	base.RetrievedAt = "2026-07-22T12:00:00Z"
	second, err := FinalizeClinicalTrialSourceSnapshot(base)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("retrieval provenance changed stable fingerprint: %q / %q", first.Fingerprint, second.Fingerprint)
	}
	if first.ProvenanceDigest == "" || first.ProvenanceDigest == second.ProvenanceDigest {
		t.Fatalf("provenance digests = %q / %q", first.ProvenanceDigest, second.ProvenanceDigest)
	}
	first.DataTimestamp = "2026-07-22T15:04:05Z"
	if _, err := FinalizeClinicalTrialSourceSnapshot(first); err == nil {
		t.Fatal("accepted tampered data timestamp with stale provenance digest")
	}
}

func TestClinicalTrialTimestampsAreParsedAndCanonicalized(t *testing.T) {
	snapshot, err := FinalizeClinicalTrialSourceSnapshot(ClinicalTrialSourceSnapshot{
		SourceType:        "clinicaltrials.gov",
		CanonicalID:       "NCT01234567",
		RetrievedAt:       "2026-07-21T08:00:00-04:00",
		UpstreamUpdatedAt: "2026-07-20T08:00:00-04:00",
		ContentHash:       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		LicenseScope:      "public_metadata",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RetrievedAt != "2026-07-21T12:00:00Z" || snapshot.UpstreamUpdatedAt != "2026-07-20T12:00:00Z" {
		t.Fatalf("canonical snapshot timestamps = %q, %q", snapshot.RetrievedAt, snapshot.UpstreamUpdatedAt)
	}

	audit := validClinicalTrialAudit(t)
	audit.CompletedAt = "2026-07-21T09:00:00-04:00"
	finalized, err := FinalizeClinicalTrialAudit(audit)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.CompletedAt != "2026-07-21T13:00:00Z" {
		t.Fatalf("canonical completed_at = %q", finalized.CompletedAt)
	}

	invalidSnapshot := snapshot
	invalidSnapshot.RetrievedAt = "yesterday"
	if _, err := FinalizeClinicalTrialSourceSnapshot(invalidSnapshot); err == nil {
		t.Fatal("invalid retrieved_at error = nil")
	}
	invalidAudit := validClinicalTrialAudit(t)
	invalidAudit.CompletedAt = "later"
	if err := ValidateClinicalTrialAudit(invalidAudit); err == nil {
		t.Fatal("invalid completed_at error = nil")
	}
}

func TestClinicalTrialAuditValidatesFindingsCitationsConfidenceAndLimitations(t *testing.T) {
	audit := validClinicalTrialAudit(t)
	audit.Findings = []ClinicalTrialFinding{
		{FindingID: "registered", Class: ClinicalTrialFindingRegisteredFact, Summary: "The registry names one primary endpoint.", CitationIDs: []string{"registry-primary"}},
		{FindingID: "reported", Class: ClinicalTrialFindingPublicationClaim, Summary: "The publication reports that endpoint.", CitationIDs: []string{"publication-primary"}},
		{FindingID: "difference", Class: ClinicalTrialFindingDeterministicDiscrepancy, Summary: "The reported time point differs.", CitationIDs: []string{"registry-primary", "publication-primary"}},
		{FindingID: "interpretation", Class: ClinicalTrialFindingModelInterpretation, Summary: "The difference needs domain review.", CitationIDs: []string{"registry-primary", "publication-primary"}},
		{FindingID: "conflict", Class: ClinicalTrialFindingUnresolvedConflict, Summary: "The available records conflict.", CitationIDs: []string{"registry-primary", "publication-primary"}},
	}
	audit.Confidence = 0.72
	audit.Limitations = []string{"The protocol was unavailable."}

	if err := ValidateClinicalTrialAudit(audit); err != nil {
		t.Fatalf("ValidateClinicalTrialAudit() error = %v", err)
	}
}

func TestClinicalTrialAuditRejectsUncitedFactualFinding(t *testing.T) {
	audit := validClinicalTrialAudit(t)
	audit.Findings[0].CitationIDs = nil

	err := ValidateClinicalTrialAudit(audit)
	if err == nil || !strings.Contains(err.Error(), "citations") {
		t.Fatalf("ValidateClinicalTrialAudit() error = %v", err)
	}
}

func TestClinicalTrialAuditRejectsUnknownFindingClassAndDuplicateCitationIDs(t *testing.T) {
	t.Run("unknown finding class", func(t *testing.T) {
		audit := validClinicalTrialAudit(t)
		audit.Findings[0].Class = "model_guess"
		if err := ValidateClinicalTrialAudit(audit); err == nil || !strings.Contains(err.Error(), "finding class") {
			t.Fatalf("ValidateClinicalTrialAudit() error = %v", err)
		}
	})

	t.Run("duplicate citation ID", func(t *testing.T) {
		audit := validClinicalTrialAudit(t)
		audit.Citations = append(audit.Citations, audit.Citations[0])
		if err := ValidateClinicalTrialAudit(audit); err == nil || !strings.Contains(err.Error(), "duplicate citation") {
			t.Fatalf("ValidateClinicalTrialAudit() error = %v", err)
		}
	})
}

func TestClinicalTrialAuditRejectsNonFiniteConfidenceAndInvalidLimitations(t *testing.T) {
	for _, confidence := range []float64{0, 1} {
		audit := validClinicalTrialAudit(t)
		audit.Confidence = confidence
		if err := ValidateClinicalTrialAudit(audit); err != nil {
			t.Fatalf("boundary confidence %v error = %v", confidence, err)
		}
	}

	for name, confidence := range map[string]float64{
		"nan":      math.NaN(),
		"positive": math.Inf(1),
		"negative": math.Inf(-1),
	} {
		t.Run(name, func(t *testing.T) {
			audit := validClinicalTrialAudit(t)
			audit.Confidence = confidence
			if err := ValidateClinicalTrialAudit(audit); err == nil || !strings.Contains(err.Error(), "confidence") {
				t.Fatalf("ValidateClinicalTrialAudit() error = %v", err)
			}
		})
	}

	for name, limitations := range map[string][]string{
		"empty":      {""},
		"duplicate":  {"Protocol unavailable", "Protocol unavailable"},
		"whitespace": {" Protocol unavailable "},
	} {
		t.Run(name, func(t *testing.T) {
			audit := validClinicalTrialAudit(t)
			audit.Limitations = limitations
			if err := ValidateClinicalTrialAudit(audit); err == nil || !strings.Contains(err.Error(), "limitations") {
				t.Fatalf("ValidateClinicalTrialAudit() error = %v", err)
			}
		})
	}
}

func TestClinicalTrialAuditRunAllowsDeclaredStatesAndRequiresCompleteTerminalState(t *testing.T) {
	request, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{
		ClinicalTrialAuditRunQueued,
		ClinicalTrialAuditRunCollecting,
		ClinicalTrialAuditRunComparing,
		ClinicalTrialAuditRunReasoning,
		ClinicalTrialAuditRunAwaitingReview,
	} {
		run := ClinicalTrialAuditRun{SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: state, Request: request}
		if err := ValidateClinicalTrialAuditRun(run); err != nil {
			t.Fatalf("state %q error = %v", state, err)
		}
	}

	audit := validClinicalTrialAudit(t)
	for _, state := range []string{ClinicalTrialAuditRunCompleted, ClinicalTrialAuditRunAbstained} {
		run := ClinicalTrialAuditRun{SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: state, Request: request, Audit: &audit}
		if state == ClinicalTrialAuditRunAbstained {
			run.Audit.Findings = nil
			run.Audit.Limitations = []string{"Primary evidence was unavailable."}
		}
		if err := ValidateClinicalTrialAuditRun(run); err != nil {
			t.Fatalf("terminal state %q error = %v", state, err)
		}
	}

	failed := ClinicalTrialAuditRun{SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunFailed, Request: request, Error: clinicalTrialStringPointer("source_timeout")}
	if err := ValidateClinicalTrialAuditRun(failed); err != nil {
		t.Fatalf("failed state error = %v", err)
	}

	for name, run := range map[string]ClinicalTrialAuditRun{
		"unknown":            {SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: "running", Request: request},
		"completed no audit": {SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunCompleted, Request: request},
		"abstained no audit": {SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunAbstained, Request: request},
		"failed no error":    {SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunFailed, Request: request},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateClinicalTrialAuditRun(run); err == nil {
				t.Fatal("ValidateClinicalTrialAuditRun() error = nil")
			}
		})
	}
}

func TestClinicalTrialAuditRunCanonicalizesAndValidatesLifecycleTimestamps(t *testing.T) {
	request, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"})
	if err != nil {
		t.Fatal(err)
	}
	run := ClinicalTrialAuditRun{
		SchemaVersion: ClinicalTrialAuditRunSchemaVersion,
		RunID:         "run-1",
		State:         ClinicalTrialAuditRunQueued,
		Request:       request,
		CreatedAt:     "2026-07-21T08:00:00-04:00",
		UpdatedAt:     "2026-07-21T09:30:00-04:00",
	}
	finalized, err := FinalizeClinicalTrialAuditRun(run)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.CreatedAt != "2026-07-21T12:00:00Z" || finalized.UpdatedAt != "2026-07-21T13:30:00Z" {
		t.Fatalf("canonical run timestamps = %q, %q", finalized.CreatedAt, finalized.UpdatedAt)
	}
	if err := ValidateClinicalTrialAuditRun(finalized); err != nil {
		t.Fatalf("ValidateClinicalTrialAuditRun(finalized) error = %v", err)
	}

	for name, timestamps := range map[string][2]string{
		"invalid created_at":     {"not-a-time", ""},
		"invalid updated_at":     {"2026-07-21T12:00:00Z", "not-a-time"},
		"noncanonical created":   {"2026-07-21T08:00:00-04:00", ""},
		"noncanonical updated":   {"", "2026-07-21T08:00:00-04:00"},
		"updated before created": {"2026-07-21T13:00:00Z", "2026-07-21T12:59:59Z"},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := ClinicalTrialAuditRun{
				SchemaVersion: ClinicalTrialAuditRunSchemaVersion,
				RunID:         "run-1",
				State:         ClinicalTrialAuditRunQueued,
				Request:       request,
				CreatedAt:     timestamps[0],
				UpdatedAt:     timestamps[1],
			}
			if err := ValidateClinicalTrialAuditRun(candidate); err == nil {
				t.Fatal("ValidateClinicalTrialAuditRun() error = nil")
			}
		})
	}
}

func TestClinicalTrialAuditRunRejectsMismatchedRequestsAndExclusivePayloadViolations(t *testing.T) {
	audit := validClinicalTrialAudit(t)
	request := audit.Request
	otherRequest, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputPMID, Input: "12345678"})
	if err != nil {
		t.Fatal(err)
	}

	mismatched := ClinicalTrialAuditRun{
		SchemaVersion: ClinicalTrialAuditRunSchemaVersion,
		RunID:         "run-1",
		State:         ClinicalTrialAuditRunCompleted,
		Request:       otherRequest,
		Audit:         &audit,
	}
	if err := ValidateClinicalTrialAuditRun(mismatched); err == nil || !strings.Contains(err.Error(), "request") {
		t.Fatalf("mismatched request error = %v", err)
	}

	for name, run := range map[string]ClinicalTrialAuditRun{
		"nonterminal audit": {SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunCollecting, Request: request, Audit: &audit},
		"nonterminal error": {SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunReasoning, Request: request, Error: clinicalTrialStringPointer("unexpected")},
		"failed audit":      {SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunFailed, Request: request, Audit: &audit, Error: clinicalTrialStringPointer("source_timeout")},
		"completed error":   {SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunCompleted, Request: request, Audit: &audit, Error: clinicalTrialStringPointer("stale")},
		"abstained error":   {SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunAbstained, Request: request, Audit: &audit, Error: clinicalTrialStringPointer("stale")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateClinicalTrialAuditRun(run); err == nil {
				t.Fatal("ValidateClinicalTrialAuditRun() error = nil")
			}
		})
	}

	completedWithoutFindings := audit
	completedWithoutFindings.Findings = nil
	completed := ClinicalTrialAuditRun{SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunCompleted, Request: request, Audit: &completedWithoutFindings}
	if err := ValidateClinicalTrialAuditRun(completed); err == nil || !strings.Contains(err.Error(), "findings") {
		t.Fatalf("completed without findings error = %v", err)
	}

	abstainedWithoutFindings := audit
	abstainedWithoutFindings.Findings = nil
	abstained := ClinicalTrialAuditRun{SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunAbstained, Request: request, Audit: &abstainedWithoutFindings}
	if err := ValidateClinicalTrialAuditRun(abstained); err != nil {
		t.Fatalf("abstained without findings error = %v", err)
	}
	abstainedWithFindings := ClinicalTrialAuditRun{SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunAbstained, Request: request, Audit: &audit}
	if err := ValidateClinicalTrialAuditRun(abstainedWithFindings); err == nil || !strings.Contains(err.Error(), "findings") {
		t.Fatalf("abstained with findings error = %v", err)
	}
}

func clinicalTrialStringPointer(value string) *string {
	return &value
}

func TestClinicalTrialAuditJSONSchemaMirrorsRunAndCollectionBoundaries(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/clinical-trial-audit-v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	snapshot := defs["source_snapshot"].(map[string]any)
	snapshotProperties := snapshot["properties"].(map[string]any)
	for _, field := range []string{"data_timestamp", "provenance_digest"} {
		if snapshotProperties[field] == nil {
			t.Fatalf("source snapshot schema is missing %s", field)
		}
	}
	audit := defs["audit"].(map[string]any)
	auditProperties := audit["properties"].(map[string]any)
	if auditProperties["findings"].(map[string]any)["minItems"] != float64(1) {
		t.Fatal("completed audit schema must require findings")
	}
	abstained := defs["abstained_audit"].(map[string]any)
	abstainedProperties := abstained["properties"].(map[string]any)
	if abstainedProperties["findings"].(map[string]any)["maxItems"] != float64(0) {
		t.Fatal("abstained audit schema must explicitly require empty findings")
	}
	abstainedLimitations := abstainedProperties["limitations"].(map[string]any)
	if abstainedLimitations["uniqueItems"] != true || abstainedLimitations["items"].(map[string]any)["pattern"] == nil {
		t.Fatal("abstained audit schema must enforce canonical unique limitations")
	}
	limitations := auditProperties["limitations"].(map[string]any)
	if limitations["minItems"] != float64(1) || limitations["uniqueItems"] != true {
		t.Fatal("audit schema must require unique non-empty limitations")
	}
	if limitations["items"].(map[string]any)["pattern"] == nil {
		t.Fatal("audit schema limitations must reject surrounding whitespace")
	}
	run := defs["run"].(map[string]any)
	runProperties := run["properties"].(map[string]any)
	for _, field := range []string{"created_at", "updated_at"} {
		if runProperties[field].(map[string]any)["format"] != "date-time" {
			t.Fatalf("run schema %s must use date-time format", field)
		}
	}
	rules := run["allOf"].([]any)
	if len(rules) != 4 {
		t.Fatal("run schema must define one exclusive payload rule per state class")
	}
	assertClinicalTrialSchemaStateRule(t, rules, "failed", "error", "audit", "")
	assertClinicalTrialSchemaStateRule(t, rules, "completed", "audit", "error", "#/$defs/audit")
	assertClinicalTrialSchemaStateRule(t, rules, "abstained", "audit", "error", "#/$defs/abstained_audit")

	nonterminal := rules[0].(map[string]any)
	states := nonterminal["if"].(map[string]any)["properties"].(map[string]any)["state"].(map[string]any)["enum"].([]any)
	if len(states) != 5 {
		t.Fatal("nonterminal schema rule must cover all five states")
	}
	forbidden := nonterminal["then"].(map[string]any)["not"].(map[string]any)["anyOf"].([]any)
	if len(forbidden) != 2 || firstRequiredClinicalTrialSchemaField(forbidden[0]) != "audit" || firstRequiredClinicalTrialSchemaField(forbidden[1]) != "error" {
		t.Fatal("nonterminal schema rule must reject contradictory audit and error payloads")
	}
}

func assertClinicalTrialSchemaStateRule(t *testing.T, rules []any, state, required, forbidden, auditRef string) {
	t.Helper()
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		stateSchema := rule["if"].(map[string]any)["properties"].(map[string]any)["state"].(map[string]any)
		if stateSchema["const"] != state {
			continue
		}
		then := rule["then"].(map[string]any)
		if firstRequiredClinicalTrialSchemaField(then) != required || firstRequiredClinicalTrialSchemaField(then["not"]) != forbidden {
			t.Fatalf("schema rule %s does not enforce required=%s forbidden=%s", state, required, forbidden)
		}
		if auditRef != "" {
			got := then["properties"].(map[string]any)["audit"].(map[string]any)["$ref"]
			if got != auditRef {
				t.Fatalf("schema rule %s audit ref = %v, want %s", state, got, auditRef)
			}
		}
		return
	}
	t.Fatalf("schema rule for state %s is missing", state)
}

func firstRequiredClinicalTrialSchemaField(value any) string {
	object := value.(map[string]any)
	required := object["required"].([]any)
	return required[0].(string)
}

func validClinicalTrialAudit(t *testing.T) ClinicalTrialAudit {
	t.Helper()
	request, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := FinalizeClinicalTrialSourceSnapshot(ClinicalTrialSourceSnapshot{
		SourceType: "clinicaltrials.gov", CanonicalID: "NCT01234567", UpstreamUpdatedAt: "2026-07-20T12:00:00Z",
		ContentHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LicenseScope: "public_metadata", RetrievedAt: "2026-07-21T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	publication, err := FinalizeClinicalTrialSourceSnapshot(ClinicalTrialSourceSnapshot{
		SourceType: "pubmed", CanonicalID: "PMID:12345678", UpstreamUpdatedAt: "2026-07-19T12:00:00Z",
		ContentHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", LicenseScope: "abstract_metadata", RetrievedAt: "2026-07-21T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	return ClinicalTrialAudit{
		SchemaVersion: ClinicalTrialAuditSchemaVersion,
		AuditID:       "audit-1",
		Request:       request,
		Sources:       []ClinicalTrialSourceSnapshot{registry, publication},
		Citations: []ClinicalTrialAuditCitation{
			{CitationID: "registry-primary", SourceFingerprint: registry.Fingerprint, Locator: "protocolSection.outcomesModule.primaryOutcomes[0]"},
			{CitationID: "publication-primary", SourceFingerprint: publication.Fingerprint, Locator: "abstract.results"},
		},
		Findings:    []ClinicalTrialFinding{{FindingID: "registered", Class: ClinicalTrialFindingRegisteredFact, Summary: "The registry names one primary endpoint.", CitationIDs: []string{"registry-primary"}}},
		Confidence:  0.8,
		Limitations: []string{"The protocol was unavailable."},
		CompletedAt: "2026-07-21T13:00:00Z",
	}
}
