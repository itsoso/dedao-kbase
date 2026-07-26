package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvaluateKnowledgeEvidenceResolvesExplicitCitationChain(t *testing.T) {
	pkg := evidenceTestPackage()
	analysis := evidenceTestAnalysis("citation-1")

	report := EvaluateKnowledgeEvidence(pkg, analysis)

	if report.HasBlockers() {
		t.Fatalf("report blockers = %#v", report.Issues)
	}
	if report.AnalysisClaims != 1 || report.ClaimsWithEvidence != 1 || report.ClaimsWithExplicitCitation != 1 {
		t.Fatalf("claim metrics = %#v", report)
	}
	if report.EvidenceReferences != 1 || report.ResolvedReferences != 1 || report.ExplicitCitationReferences != 1 {
		t.Fatalf("reference metrics = %#v", report)
	}
	if report.ClaimCoverage != 1 || report.ResolutionRate != 1 || report.ExplicitCitationCoverage != 1 {
		t.Fatalf("coverage metrics = %#v", report)
	}
}

func TestEvaluateKnowledgeEvidenceAcceptsResolvableLegacyChunkReference(t *testing.T) {
	pkg := evidenceTestPackage()
	report := EvaluateKnowledgeEvidence(pkg, evidenceTestAnalysis("chunk-1"))

	if report.HasBlockers() {
		t.Fatalf("report blockers = %#v", report.Issues)
	}
	if report.LegacyDirectChunkReferences != 1 || !evidenceIssueExists(report, "legacy_direct_chunk_reference", "warning") {
		t.Fatalf("report = %#v", report)
	}
	if report.ClaimsWithEvidence != 1 || report.ClaimsWithExplicitCitation != 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateKnowledgeEvidenceBlocksUnresolvedReference(t *testing.T) {
	report := EvaluateKnowledgeEvidence(evidenceTestPackage(), evidenceTestAnalysis("missing"))

	if !report.HasBlockers() || !evidenceIssueExists(report, "unresolved_evidence_reference", "blocker") {
		t.Fatalf("report = %#v", report)
	}
	if report.ClaimsWithEvidence != 0 || report.ResolutionRate != 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateKnowledgeEvidenceBlocksMissingClaimEvidence(t *testing.T) {
	report := EvaluateKnowledgeEvidence(evidenceTestPackage(), evidenceTestAnalysis())

	if !report.HasBlockers() || !evidenceIssueExists(report, "missing_claim_evidence", "blocker") {
		t.Fatalf("report = %#v", report)
	}
	if report.AnalysisClaims != 1 || report.ClaimCoverage != 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateKnowledgeEvidenceBlocksConflictingDuplicateID(t *testing.T) {
	pkg := evidenceTestPackage()
	pkg.Chunks = append(pkg.Chunks, BookKnowledgeChunk{
		ChunkID: "chunk-1", BookID: "book-1", ChapterID: "chapter-1", Text: "different",
	})

	report := EvaluateKnowledgeEvidence(pkg, evidenceTestAnalysis("citation-1"))

	if !report.HasBlockers() || !evidenceIssueExists(report, "conflicting_duplicate_id", "blocker") {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateKnowledgeEvidenceBlocksIDSharedAcrossObjectKinds(t *testing.T) {
	pkg := evidenceTestPackage()
	pkg.Citations[0].CitationID = "chunk-1"

	report := EvaluateKnowledgeEvidence(pkg, evidenceTestAnalysis("chunk-1"))

	if !report.HasBlockers() || !evidenceIssueExists(report, "ambiguous_object_id", "blocker") {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateKnowledgeEvidenceBlocksCrossBookAndBrokenCitationEdges(t *testing.T) {
	tests := []struct {
		name string
		edit func(*BookKnowledgePackage)
		code string
	}{
		{
			name: "chunk belongs to another book",
			edit: func(pkg *BookKnowledgePackage) {
				pkg.Chunks[0].BookID = "other-book"
			},
			code: "cross_book_reference",
		},
		{
			name: "citation points to missing chunk",
			edit: func(pkg *BookKnowledgePackage) {
				pkg.Citations[0].ChunkID = "missing"
			},
			code: "citation_chunk_unresolved",
		},
		{
			name: "citation chapter differs from chunk",
			edit: func(pkg *BookKnowledgePackage) {
				pkg.Chapters = append(pkg.Chapters, BookKnowledgeChapter{
					ChapterID: "chapter-2", BookID: "book-1", Order: 2, Title: "Second",
				})
				pkg.Citations[0].ChapterID = "chapter-2"
			},
			code: "citation_chunk_chapter_mismatch",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg := evidenceTestPackage()
			test.edit(&pkg)
			report := EvaluateKnowledgeEvidence(pkg, evidenceTestAnalysis("citation-1"))
			if !report.HasBlockers() || !evidenceIssueExists(report, test.code, "blocker") {
				t.Fatalf("report = %#v", report)
			}
		})
	}
}

func TestEvaluateKnowledgeEvidenceUsesZeroSafeCoverage(t *testing.T) {
	report := EvaluateKnowledgeEvidence(evidenceTestPackage(), nil)

	if report.AnalysisClaims != 0 || report.ClaimCoverage != 0 || report.ResolutionRate != 0 || report.ExplicitCitationCoverage != 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestCanonicalPublicationIdentityIsConservative(t *testing.T) {
	tests := []struct {
		name     string
		book     BookKnowledgeBook
		basis    string
		eligible bool
	}{
		{
			name: "account",
			book: BookKnowledgeBook{
				BookID: "1", SourceType: "wechat_mp", SourceAccount: "益家知研",
			},
			basis: "source_account", eligible: true,
		},
		{
			name: "host",
			book: BookKnowledgeBook{
				BookID: "2", SourceHTML: "https://www.dedao.cn/ebook/reader?id=2",
			},
			basis: "source_host", eligible: true,
		},
		{
			name: "item fallback is not independent",
			book: BookKnowledgeBook{
				BookID: "3", SourceType: "import", SourceKey: "item-3",
			},
			basis: "source_item", eligible: false,
		},
		{
			name:  "book fallback is not independent",
			book:  BookKnowledgeBook{BookID: "4"},
			basis: "book_fallback", eligible: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity := CanonicalKnowledgePublicationIdentity(test.book)
			if identity.Key == "" || identity.Basis != test.basis || identity.IndependentSourceEligible != test.eligible {
				t.Fatalf("identity = %#v", identity)
			}
			if strings.Contains(identity.Key, "/") || strings.Contains(identity.Key, "益家知研") {
				t.Fatalf("identity key is not bounded/safe: %q", identity.Key)
			}
		})
	}
}

func TestKnowledgeEvidenceReportDoesNotExposeSourceBodiesOrPaths(t *testing.T) {
	pkg := evidenceTestPackage()
	pkg.Book.SourceHTML = "/Users/private/downloaded-book.html"
	pkg.Chunks[0].Text = "private source body sentinel"
	report := EvaluateKnowledgeEvidence(pkg, evidenceTestAnalysis("citation-1"))

	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(payload)
	for _, forbidden := range []string{"/Users/private", "private source body sentinel", "downloaded-book.html"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("report leaked %q: %s", forbidden, raw)
		}
	}
}

func evidenceTestPackage() BookKnowledgePackage {
	return BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID: "book-1", Title: "Evidence Book", SourceType: "wechat_mp", SourceAccount: "publisher",
		},
		Chapters: []BookKnowledgeChapter{
			{ChapterID: "chapter-1", BookID: "book-1", Order: 1, Title: "Chapter"},
		},
		Chunks: []BookKnowledgeChunk{
			{ChunkID: "chunk-1", BookID: "book-1", ChapterID: "chapter-1", Order: 1, Text: "Evidence."},
		},
		Claims: []BookKnowledgeClaim{
			{ClaimID: "package-claim-1", BookID: "book-1", ChapterID: "chapter-1", Title: "Claim", Summary: "Summary"},
		},
		Citations: []BookKnowledgeCitation{
			{CitationID: "citation-1", BookID: "book-1", ChapterID: "chapter-1", ChunkID: "chunk-1"},
		},
	}
}

func evidenceTestAnalysis(citationIDs ...string) *BookAnalysisManifest {
	return &BookAnalysisManifest{
		BookID: "book-1",
		Status: BookAnalysisReady,
		Payload: &BookAnalysisPayload{
			Summary: "Summary",
			Claims: []BookAnalysisClaim{{
				ID: "analysis-claim-1", Statement: "Statement", CitationIDs: citationIDs,
				Confidence: 0.8, RiskLevel: "medium",
			}},
		},
	}
}

func evidenceIssueExists(report KnowledgeEvidenceReport, code, severity string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code && issue.Severity == severity {
			return true
		}
	}
	return false
}
