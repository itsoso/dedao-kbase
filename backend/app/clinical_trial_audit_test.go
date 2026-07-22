package app

import (
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

	failed := ClinicalTrialAuditRun{SchemaVersion: ClinicalTrialAuditRunSchemaVersion, RunID: "run-1", State: ClinicalTrialAuditRunFailed, Request: request, Error: "source_timeout"}
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
