package app

import (
	"strings"
	"testing"
)

const (
	testPrimaryPublication = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testSupportPublication = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	testSecondPublication  = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
)

func TestEvidenceAuditHashesAreDeterministicAndCompletedReportsValidate(t *testing.T) {
	input := validEvidenceAuditInput()
	firstHash, err := EvidenceAuditInputHash(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Releases[0].Citations = []EvidenceAuditCitationRef{
		{CitationID: "citation-b", ClaimID: "claim-b", ChunkID: "chunk-b"},
		{CitationID: "citation-a", ClaimID: "claim-a", ChunkID: "chunk-a"},
	}
	secondHash, err := EvidenceAuditInputHash(input)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("set-like citation ordering changed input hash: %q != %q", firstHash, secondHash)
	}

	audit := validCompletedEvidenceAudit()
	finalized, err := FinalizeEvidenceAuditReport(audit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(finalized.OutputHash, "sha256:") {
		t.Fatalf("output hash = %q", finalized.OutputHash)
	}
	if err := ValidateEvidenceAudit(finalized); err != nil {
		t.Fatalf("ValidateEvidenceAudit() error = %v", err)
	}
	repeated, err := FinalizeEvidenceAuditReport(audit)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.OutputHash != finalized.OutputHash {
		t.Fatalf("report hash is not deterministic: %q != %q", repeated.OutputHash, finalized.OutputHash)
	}
}

func TestEvidenceAuditValidationRejectsInvalidContractAndUngroundedVerdicts(t *testing.T) {
	valid, err := FinalizeEvidenceAuditReport(validCompletedEvidenceAudit())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*EvidenceAudit)
		want string
	}{
		{name: "schema", edit: func(a *EvidenceAudit) { a.SchemaVersion = "evidence-audit.v2" }, want: "schema_version"},
		{name: "status", edit: func(a *EvidenceAudit) { a.Status = "published" }, want: "status"},
		{name: "missing input hash", edit: func(a *EvidenceAudit) { a.InputHash = "" }, want: "input_hash"},
		{name: "completed without output hash", edit: func(a *EvidenceAudit) { a.OutputHash = "" }, want: "output_hash"},
		{name: "unsupported without evidence", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].Evidence = nil
		}, want: "evidence"},
		{name: "unparseable citation", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].Evidence[0].CitationID = ""
			a.ClaimAudits[0].Evidence[0].ClaimID = ""
			a.ClaimAudits[0].Evidence[0].ChunkID = ""
		}, want: "citation"},
		{name: "model supplied confidence", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].ComputedConfidence = 0.99
		}, want: "computed_confidence"},
		{name: "evidence release is not pinned", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].Evidence[0].ReleaseID = "release-unpinned"
		}, want: "pinned release"},
		{name: "evidence release hash mismatch", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].Evidence[0].ContentHash = "sha256:wrong"
		}, want: "content_hash"},
		{name: "citation is not pinned", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].Evidence[0].CitationID = "citation-unpinned"
		}, want: "pinned citation"},
		{name: "citation claim mismatch", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].Evidence[0].ClaimID = "claim-unpinned"
		}, want: "citation binding"},
		{name: "citation chunk mismatch", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].Evidence[0].ChunkID = "chunk-unpinned"
		}, want: "citation binding"},
		{name: "publication identity mismatch", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].Evidence[0].PublicationIdentity = testSecondPublication
		}, want: "publication_identity"},
		{name: "evidence source type mismatch", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].Evidence[0].SourceType = "wechat_mp_article"
		}, want: "source_type"},
		{name: "injected claim", edit: func(a *EvidenceAudit) {
			a.ClaimAudits[0].SourceClaim = "claim not selected by the input"
		}, want: "selected_claims"},
		{name: "duplicate claim audit", edit: func(a *EvidenceAudit) {
			a.ClaimAudits = append(a.ClaimAudits, a.ClaimAudits[0])
		}, want: "selected_claims"},
		{name: "missing claim audit", edit: func(a *EvidenceAudit) {
			a.ClaimAudits = nil
		}, want: "selected_claims"},
		{name: "running audit carries partial report", edit: func(a *EvidenceAudit) {
			a.Status = EvidenceAuditRunning
			a.CompletedAt = ""
			a.OutputHash = ""
		}, want: "partial report"},
		{name: "queued audit carries partial report", edit: func(a *EvidenceAudit) {
			a.Status = EvidenceAuditQueued
			a.StartedAt = ""
			a.CompletedAt = ""
			a.TraceID = ""
			a.OutputHash = ""
		}, want: "partial report"},
		{name: "failed audit carries partial report", edit: func(a *EvidenceAudit) {
			a.Status = EvidenceAuditFailed
			a.CompletedAt = ""
			a.FailedAt = "2026-07-23T10:02:00Z"
			a.FailureCode = "audit_failed"
			a.FailureSummary = "audit failed with code audit_failed"
			a.OutputHash = ""
		}, want: "partial report"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			changed := valid
			changed.Releases = append([]EvidenceAuditReleaseRef{}, valid.Releases...)
			changed.ClaimAudits = cloneEvidenceAuditClaims(valid.ClaimAudits)
			testCase.edit(&changed)
			if err := ValidateEvidenceAudit(changed); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("ValidateEvidenceAudit() error = %v, want containing %q", err, testCase.want)
			}
		})
	}
}

func TestEvidenceAuditInsufficientVerdictMayDeclareKnowledgeGapWithoutEvidence(t *testing.T) {
	audit := validCompletedEvidenceAudit()
	audit.ClaimAudits[0] = EvidenceAuditClaim{
		SourceClaim:         "source claim 1",
		NormalizedStatement: "The intervention benefit remains uncertain.",
		Verdict:             EvidenceAuditVerdictInsufficient,
		ComputedConfidence:  0,
		Limitations:         []string{"No independent replication."},
		KnowledgeGaps:       []string{"Independent trial evidence."},
		ReviewActions:       []string{"Request specialist review."},
	}
	audit.Summary.VerdictCounts = map[string]int{EvidenceAuditVerdictInsufficient: 1}
	finalized, err := FinalizeEvidenceAuditReport(audit)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEvidenceAudit(finalized); err != nil {
		t.Fatalf("insufficient audit should validate: %v", err)
	}
}

func TestEvidenceAuditValidationRejectsInvalidLifecycleOrdering(t *testing.T) {
	valid, err := FinalizeEvidenceAuditReport(validCompletedEvidenceAudit())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*EvidenceAudit)
	}{
		{name: "updated before created", edit: func(a *EvidenceAudit) { a.UpdatedAt = "2026-07-23T09:59:00Z" }},
		{name: "started before created", edit: func(a *EvidenceAudit) { a.StartedAt = "2026-07-23T09:59:00Z" }},
		{name: "completed before started", edit: func(a *EvidenceAudit) { a.CompletedAt = "2026-07-23T10:00:30Z" }},
		{name: "updated before completed", edit: func(a *EvidenceAudit) { a.UpdatedAt = "2026-07-23T10:01:30Z" }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			changed := valid
			changed.Releases = append([]EvidenceAuditReleaseRef{}, valid.Releases...)
			changed.ClaimAudits = cloneEvidenceAuditClaims(valid.ClaimAudits)
			testCase.edit(&changed)
			changed.OutputHash, _ = EvidenceAuditOutputHash(changed)
			if err := ValidateEvidenceAudit(changed); err == nil || !strings.Contains(err.Error(), "timestamp order") {
				t.Fatalf("ValidateEvidenceAudit() error = %v, want timestamp order", err)
			}
		})
	}
}

func TestEvidenceAuditConfidenceSeparatesIndependentPublicationsAndSourceDiversity(t *testing.T) {
	evidence := []EvidenceAuditEvidenceRef{
		validEvidenceRef("release-primary", "sha256:primary", EvidenceAuditReleasePrimary, "dedao_ebook", testPrimaryPublication, "citation-a", "claim-a", "chunk-a"),
		validEvidenceRef("release-support", "sha256:support", EvidenceAuditReleaseSupporting, "wechat_mp_article", testSupportPublication, "citation-c", "claim-c", "chunk-c"),
		validEvidenceRef("release-support-2", "sha256:support-2", EvidenceAuditReleaseSupporting, "wechat_mp_article", testSecondPublication, "citation-d", "claim-d", "chunk-d"),
	}
	got := ComputeEvidenceAuditConfidence(evidence, 0)
	if got != 0.8 {
		t.Fatalf("confidence = %.2f, want 0.80", got)
	}
	withConflict := ComputeEvidenceAuditConfidence(evidence, 2)
	if withConflict >= got || withConflict < 0 || withConflict > 1 {
		t.Fatalf("conflicted confidence = %.2f, base = %.2f", withConflict, got)
	}
	samePublication := append([]EvidenceAuditEvidenceRef{}, evidence...)
	samePublication[2].PublicationIdentity = testSupportPublication
	if score := ComputeEvidenceAuditConfidence(samePublication, 0); score >= got {
		t.Fatalf("same-publication confidence = %.2f, want below %.2f", score, got)
	}
	differentSourceType := append([]EvidenceAuditEvidenceRef{}, evidence...)
	differentSourceType[2].SourceType = "clinical_guideline"
	if score := ComputeEvidenceAuditConfidence(differentSourceType, 0); score <= got {
		t.Fatalf("diverse-source confidence = %.2f, want above %.2f", score, got)
	}
	if got := ComputeEvidenceAuditConfidence(nil, 100); got != 0 {
		t.Fatalf("empty confidence = %.2f, want 0", got)
	}
}

func validEvidenceAuditInput() EvidenceAuditInput {
	return EvidenceAuditInput{
		SchemaVersion: EvidenceAuditSchemaVersion,
		Package:       EvidenceAuditPackageRef{PackageID: "book-agent-clinical-trials-truth", Version: "2.0.0", ContentHash: "sha256:package"},
		Model:         EvidenceAuditModelIdentity{Provider: "tokenplan", Model: "qwen3.7-max", Route: "evidence-audit"},
		Retrieval:     EvidenceAuditRetrievalIdentity{Strategy: "hybrid", IndexVersion: "index-v1", RerankerVersion: "reranker-v1"},
		Releases: []EvidenceAuditReleaseRef{
			{
				ReleaseID: "release-primary", ContentHash: "sha256:primary",
				Role: EvidenceAuditReleasePrimary, SourceType: "dedao_ebook",
				PublicationIdentity: testPrimaryPublication,
				Citations: []EvidenceAuditCitationRef{
					{CitationID: "citation-a", ClaimID: "claim-a", ChunkID: "chunk-a"},
					{CitationID: "citation-b", ClaimID: "claim-b", ChunkID: "chunk-b"},
				},
			},
			{
				ReleaseID: "release-support", ContentHash: "sha256:support",
				Role: EvidenceAuditReleaseSupporting, SourceType: "wechat_mp_article",
				PublicationIdentity: testSupportPublication,
				Citations: []EvidenceAuditCitationRef{
					{CitationID: "citation-c", ClaimID: "claim-c", ChunkID: "chunk-c"},
				},
			},
		},
		Subject:        "Clinical trial truth claims",
		Scope:          "Cross-check selected claims against pinned KBase releases.",
		SelectedClaims: []string{"source claim 1"},
	}
}

func validCompletedEvidenceAudit() EvidenceAudit {
	input := validEvidenceAuditInput()
	inputHash, _ := EvidenceAuditInputHash(input)
	evidence := []EvidenceAuditEvidenceRef{
		validEvidenceRef("release-primary", "sha256:primary", EvidenceAuditReleasePrimary, "dedao_ebook", testPrimaryPublication, "citation-a", "claim-a", "chunk-a"),
		validEvidenceRef("release-support", "sha256:support", EvidenceAuditReleaseSupporting, "wechat_mp_article", testSupportPublication, "citation-c", "claim-c", "chunk-c"),
	}
	return EvidenceAudit{
		SchemaVersion:  EvidenceAuditSchemaVersion,
		AuditID:        "audit-1",
		Status:         EvidenceAuditCompleted,
		CreatedAt:      "2026-07-23T10:00:00Z",
		UpdatedAt:      "2026-07-23T10:02:00Z",
		StartedAt:      "2026-07-23T10:01:00Z",
		CompletedAt:    "2026-07-23T10:02:00Z",
		IdempotencyKey: "audit-request-1",
		InputHash:      inputHash,
		Package:        input.Package,
		Model:          input.Model,
		Retrieval:      input.Retrieval,
		Releases:       input.Releases,
		Subject:        input.Subject,
		Scope:          input.Scope,
		SelectedClaims: input.SelectedClaims,
		ClaimAudits: []EvidenceAuditClaim{{
			SourceClaim:         "source claim 1",
			NormalizedStatement: "The intervention is supported by two pinned sources.",
			Verdict:             EvidenceAuditVerdictSupported,
			Evidence:            evidence,
			ComputedConfidence:  ComputeEvidenceAuditConfidence(evidence, 0),
			Limitations:         []string{"The sources are not a systematic review."},
			KnowledgeGaps:       []string{"Long-term outcomes."},
			ReviewActions:       []string{"Verify applicability with a clinical reviewer."},
		}},
		Summary: EvidenceAuditSummary{
			Conclusion:    "The selected claim is supported within the pinned evidence scope.",
			VerdictCounts: map[string]int{EvidenceAuditVerdictSupported: 1},
			Limitations:   []string{"Evidence scope is intentionally bounded."},
		},
		Proofroom: EvidenceAuditProofroomProjection{
			SchemaVersion: "proofroom-evidence-task.v1",
			Title:         "Review clinical evidence audit",
			ReviewItems:   []string{"Verify citation applicability."},
		},
		TraceID: "trace-1",
	}
}

func validEvidenceRef(
	releaseID, contentHash, role, sourceType, publicationIdentity, citationID, claimID, chunkID string,
) EvidenceAuditEvidenceRef {
	return EvidenceAuditEvidenceRef{
		ReleaseID: releaseID, ContentHash: contentHash, Role: role, SourceType: sourceType,
		PublicationIdentity: publicationIdentity,
		CitationID:          citationID, ClaimID: claimID, ChunkID: chunkID,
	}
}

func cloneEvidenceAuditClaims(input []EvidenceAuditClaim) []EvidenceAuditClaim {
	cloned := append([]EvidenceAuditClaim{}, input...)
	for index := range cloned {
		cloned[index].Evidence = append([]EvidenceAuditEvidenceRef{}, input[index].Evidence...)
	}
	return cloned
}
