package app

import (
	"strings"
	"testing"
)

const (
	testPrimaryPublication = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testSupportPublication = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	testSecondPublication  = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	testPackageHash        = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testPrimaryHash        = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testSupportHash        = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	testSecondSupportHash  = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
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
		{name: "invalid package hash", edit: func(a *EvidenceAudit) {
			a.Package.ContentHash = "sha256:package"
		}, want: "package.content_hash"},
		{name: "invalid release hash", edit: func(a *EvidenceAudit) {
			a.Releases[0].ContentHash = "sha256:primary"
		}, want: "releases[0].content_hash"},
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

func TestEvidenceAuditRejectsVerdictWithoutPolicyMinimumIndependentSupport(t *testing.T) {
	audit := validCompletedEvidenceAudit()
	audit.EvidencePolicy.MinimumIndependentSources = 2
	audit.Releases = append(audit.Releases, EvidenceAuditReleaseRef{
		ReleaseID:           "release-support-2",
		ContentHash:         testSecondSupportHash,
		Role:                EvidenceAuditReleaseSupporting,
		SourceType:          "clinical_guideline",
		PublicationIdentity: testSecondPublication,
		Citations: []EvidenceAuditCitationRef{{
			CitationID: "citation-d",
			ClaimID:    "claim-d",
			ChunkID:    "chunk-d",
		}},
	})
	audit.InputHash, _ = EvidenceAuditInputHash(auditInputFromAudit(audit))
	audit.ClaimAudits[0].Evidence = audit.ClaimAudits[0].Evidence[:1]
	audit.ClaimAudits[0].ComputedConfidence = ComputeEvidenceAuditConfidence(audit.ClaimAudits[0].Evidence, 0)
	if _, err := FinalizeEvidenceAuditReport(audit); err == nil ||
		!strings.Contains(err.Error(), "independent supporting publications") {
		t.Fatalf("FinalizeEvidenceAuditReport() error = %v", err)
	}
}

func TestEvidenceAuditRejectsDuplicateEvidenceIdentity(t *testing.T) {
	audit := validCompletedEvidenceAudit()
	duplicate := audit.ClaimAudits[0].Evidence[1]
	duplicate.Conflict = true
	audit.ClaimAudits[0].Evidence = append(audit.ClaimAudits[0].Evidence, duplicate)
	audit.ClaimAudits[0].ComputedConfidence = ComputeEvidenceAuditConfidence(audit.ClaimAudits[0].Evidence, 1)
	if _, err := FinalizeEvidenceAuditReport(audit); err == nil ||
		!strings.Contains(err.Error(), "duplicate evidence") {
		t.Fatalf("FinalizeEvidenceAuditReport() error = %v", err)
	}
}

func TestEvidenceAuditRejectsUnboundedInputAndReportFields(t *testing.T) {
	input := validEvidenceAuditInput()
	input.Subject = strings.Repeat("x", evidenceAuditMaxTextBytes+1)
	if _, err := EvidenceAuditInputHash(input); err == nil || !strings.Contains(err.Error(), "subject") {
		t.Fatalf("EvidenceAuditInputHash() error = %v", err)
	}

	input = validEvidenceAuditInput()
	input.Releases = make([]EvidenceAuditReleaseRef, evidenceAuditMaxReleases+1)
	for index := range input.Releases {
		input.Releases[index] = validEvidenceAuditInput().Releases[1]
		input.Releases[index].ReleaseID = "release-" + strings.Repeat("x", index+1)
	}
	if _, err := EvidenceAuditInputHash(input); err == nil || !strings.Contains(err.Error(), "releases") {
		t.Fatalf("EvidenceAuditInputHash() error = %v", err)
	}

	input = validEvidenceAuditInput()
	input.Releases[0].Citations = make(
		[]EvidenceAuditCitationRef,
		evidenceAuditMaxCitationsPerRelease+1,
	)
	for index := range input.Releases[0].Citations {
		input.Releases[0].Citations[index] = EvidenceAuditCitationRef{
			CitationID: "citation-" + strings.Repeat("x", index+1),
			ClaimID:    "claim",
			ChunkID:    "chunk",
		}
	}
	if _, err := EvidenceAuditInputHash(input); err == nil || !strings.Contains(err.Error(), "citations") {
		t.Fatalf("EvidenceAuditInputHash() citation error = %v", err)
	}

	audit := validCompletedEvidenceAudit()
	audit.Summary.Limitations = make([]string, evidenceAuditMaxListItems+1)
	for index := range audit.Summary.Limitations {
		audit.Summary.Limitations[index] = "bounded"
	}
	if _, err := FinalizeEvidenceAuditReport(audit); err == nil ||
		!strings.Contains(err.Error(), "summary.limitations") {
		t.Fatalf("FinalizeEvidenceAuditReport() error = %v", err)
	}

	audit = validCompletedEvidenceAudit()
	audit.Proofroom.ReviewItems = make([]string, evidenceAuditMaxListItems+1)
	for index := range audit.Proofroom.ReviewItems {
		audit.Proofroom.ReviewItems[index] = "review"
	}
	if _, err := FinalizeEvidenceAuditReport(audit); err == nil ||
		!strings.Contains(err.Error(), "proofroom_projection.review_items") {
		t.Fatalf("FinalizeEvidenceAuditReport() proofroom error = %v", err)
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
		validEvidenceRef("release-primary", testPrimaryHash, EvidenceAuditReleasePrimary, "dedao_ebook", testPrimaryPublication, "citation-a", "claim-a", "chunk-a"),
		validEvidenceRef("release-support", testSupportHash, EvidenceAuditReleaseSupporting, "wechat_mp_article", testSupportPublication, "citation-c", "claim-c", "chunk-c"),
		validEvidenceRef("release-support-2", testSecondSupportHash, EvidenceAuditReleaseSupporting, "wechat_mp_article", testSecondPublication, "citation-d", "claim-d", "chunk-d"),
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
		Package:       EvidenceAuditPackageRef{PackageID: "book-agent-clinical-trials-truth", Version: "2.0.0", ContentHash: testPackageHash},
		EvidencePolicy: EvidenceAuditPolicySnapshot{
			MinimumIndependentSources: 1,
			MaxClaims:                 agentEvidenceMaxClaims,
			MaxEvidencePerClaim:       agentEvidenceMaxEvidencePerClaim,
		},
		Model:     EvidenceAuditModelIdentity{Provider: "tokenplan", Model: "qwen3.7-max", Route: "evidence-audit"},
		Retrieval: EvidenceAuditRetrievalIdentity{Strategy: "hybrid", IndexVersion: "index-v1", RerankerVersion: "reranker-v1"},
		Releases: []EvidenceAuditReleaseRef{
			{
				ReleaseID: "release-primary", ContentHash: testPrimaryHash,
				Role: EvidenceAuditReleasePrimary, SourceType: "dedao_ebook",
				PublicationIdentity: testPrimaryPublication,
				Citations: []EvidenceAuditCitationRef{
					{CitationID: "citation-a", ClaimID: "claim-a", ChunkID: "chunk-a"},
					{CitationID: "citation-b", ClaimID: "claim-b", ChunkID: "chunk-b"},
				},
			},
			{
				ReleaseID: "release-support", ContentHash: testSupportHash,
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
		validEvidenceRef("release-primary", testPrimaryHash, EvidenceAuditReleasePrimary, "dedao_ebook", testPrimaryPublication, "citation-a", "claim-a", "chunk-a"),
		validEvidenceRef("release-support", testSupportHash, EvidenceAuditReleaseSupporting, "wechat_mp_article", testSupportPublication, "citation-c", "claim-c", "chunk-c"),
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
		EvidencePolicy: input.EvidencePolicy,
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
