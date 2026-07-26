package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

const KnowledgeEvidenceSchemaVersion = "knowledge_evidence.v1"

const (
	KnowledgeEvidenceBlocker = "blocker"
	KnowledgeEvidenceWarning = "warning"
	KnowledgeEvidenceInfo    = "info"
)

type KnowledgePublicationIdentity struct {
	Key                       string `json:"key"`
	Basis                     string `json:"basis"`
	IndependentSourceEligible bool   `json:"independent_source_eligible"`
}

type KnowledgeEvidenceIssue struct {
	Code       string `json:"code"`
	Severity   string `json:"severity"`
	ObjectKind string `json:"object_kind,omitempty"`
	ObjectID   string `json:"object_id,omitempty"`
	RelatedID  string `json:"related_id,omitempty"`
}

type KnowledgeEvidenceClaimResult struct {
	ClaimID                    string `json:"claim_id"`
	EvidenceReferences         int    `json:"evidence_references"`
	ResolvedReferences         int    `json:"resolved_references"`
	ExplicitCitationReferences int    `json:"explicit_citation_references"`
	LegacyDirectReferences     int    `json:"legacy_direct_references"`
}

type KnowledgeEvidenceReport struct {
	SchemaVersion               string                         `json:"schema_version"`
	BookID                      string                         `json:"book_id"`
	Publication                 KnowledgePublicationIdentity   `json:"publication"`
	AnalysisClaims              int                            `json:"analysis_claims"`
	ClaimsWithEvidence          int                            `json:"claims_with_evidence"`
	ClaimsWithExplicitCitation  int                            `json:"claims_with_explicit_citation"`
	EvidenceReferences          int                            `json:"evidence_references"`
	ResolvedReferences          int                            `json:"resolved_references"`
	ExplicitCitationReferences  int                            `json:"explicit_citation_references"`
	LegacyDirectChunkReferences int                            `json:"legacy_direct_chunk_references"`
	ClaimCoverage               float64                        `json:"claim_coverage"`
	ResolutionRate              float64                        `json:"resolution_rate"`
	ExplicitCitationCoverage    float64                        `json:"explicit_citation_coverage"`
	Issues                      []KnowledgeEvidenceIssue       `json:"issues"`
	Claims                      []KnowledgeEvidenceClaimResult `json:"claims,omitempty"`
}

func (r KnowledgeEvidenceReport) HasBlockers() bool {
	for _, issue := range r.Issues {
		if issue.Severity == KnowledgeEvidenceBlocker {
			return true
		}
	}
	return false
}

func (r KnowledgeEvidenceReport) HasStructuralBlockers() bool {
	for _, issue := range r.Issues {
		if issue.Severity == KnowledgeEvidenceBlocker && knowledgeEvidenceIssueIsStructural(issue.Code) {
			return true
		}
	}
	return false
}

func (r KnowledgeEvidenceReport) BlockerCodes() []string {
	seen := make(map[string]struct{})
	codes := make([]string, 0)
	for _, issue := range r.Issues {
		if issue.Severity != KnowledgeEvidenceBlocker {
			continue
		}
		if _, exists := seen[issue.Code]; exists {
			continue
		}
		seen[issue.Code] = struct{}{}
		codes = append(codes, issue.Code)
	}
	sort.Strings(codes)
	return codes
}

func EvaluateKnowledgeEvidence(pkg BookKnowledgePackage, analysis *BookAnalysisManifest) KnowledgeEvidenceReport {
	report := KnowledgeEvidenceReport{
		SchemaVersion: KnowledgeEvidenceSchemaVersion,
		BookID:        strings.TrimSpace(pkg.Book.BookID),
		Publication:   CanonicalKnowledgePublicationIdentity(pkg.Book),
	}
	addIssue := func(code, severity, kind, id, relatedID string) {
		report.Issues = append(report.Issues, KnowledgeEvidenceIssue{
			Code: code, Severity: severity, ObjectKind: kind,
			ObjectID: boundedEvidenceID(id), RelatedID: boundedEvidenceID(relatedID),
		})
	}
	if report.BookID == "" {
		addIssue("missing_book_id", KnowledgeEvidenceBlocker, "book", "", "")
	}
	if !report.Publication.IndependentSourceEligible {
		addIssue("publication_identity_not_independent", KnowledgeEvidenceWarning, "book", report.BookID, "")
	}

	chapters := make(map[string]BookKnowledgeChapter)
	chunks := make(map[string]BookKnowledgeChunk)
	claims := make(map[string]BookKnowledgeClaim)
	citations := make(map[string]BookKnowledgeCitation)
	validChapters := make(map[string]bool)
	validChunks := make(map[string]bool)
	validClaims := make(map[string]bool)
	validCitations := make(map[string]bool)
	idKinds := make(map[string]map[string]struct{})

	indexRecord := func(kind, id string, value any, target map[string]string) bool {
		id = strings.TrimSpace(id)
		if id == "" {
			addIssue("missing_object_id", KnowledgeEvidenceBlocker, kind, "", "")
			return false
		}
		if idKinds[id] == nil {
			idKinds[id] = make(map[string]struct{})
		}
		idKinds[id][kind] = struct{}{}
		payload, _ := json.Marshal(value)
		fingerprint := string(payload)
		if existing, ok := target[id]; ok {
			if existing != fingerprint {
				addIssue("conflicting_duplicate_id", KnowledgeEvidenceBlocker, kind, id, "")
				return false
			}
			return true
		}
		target[id] = fingerprint
		return true
	}
	chapterFingerprints := make(map[string]string)
	chunkFingerprints := make(map[string]string)
	claimFingerprints := make(map[string]string)
	citationFingerprints := make(map[string]string)

	for _, chapter := range pkg.Chapters {
		id := strings.TrimSpace(chapter.ChapterID)
		unique := indexRecord("chapter", id, chapter, chapterFingerprints)
		if _, exists := chapters[id]; !exists {
			chapters[id] = chapter
		}
		valid := unique && sameKnowledgeBook(report.BookID, chapter.BookID)
		if !sameKnowledgeBook(report.BookID, chapter.BookID) {
			addIssue("cross_book_reference", KnowledgeEvidenceBlocker, "chapter", id, chapter.BookID)
		}
		validChapters[id] = valid
	}
	for _, chunk := range pkg.Chunks {
		id := strings.TrimSpace(chunk.ChunkID)
		unique := indexRecord("chunk", id, chunk, chunkFingerprints)
		if _, exists := chunks[id]; !exists {
			chunks[id] = chunk
		}
		valid := unique && sameKnowledgeBook(report.BookID, chunk.BookID)
		if !sameKnowledgeBook(report.BookID, chunk.BookID) {
			addIssue("cross_book_reference", KnowledgeEvidenceBlocker, "chunk", id, chunk.BookID)
		}
		chapterID := strings.TrimSpace(chunk.ChapterID)
		if chapterID == "" || !validChapters[chapterID] || !sameKnowledgeBook(report.BookID, chapters[chapterID].BookID) {
			addIssue("chunk_chapter_unresolved", KnowledgeEvidenceBlocker, "chunk", id, chapterID)
			valid = false
		}
		validChunks[id] = valid
	}
	for _, claim := range pkg.Claims {
		id := strings.TrimSpace(claim.ClaimID)
		unique := indexRecord("package_claim", id, claim, claimFingerprints)
		if _, exists := claims[id]; !exists {
			claims[id] = claim
		}
		valid := unique && sameKnowledgeBook(report.BookID, claim.BookID)
		if !sameKnowledgeBook(report.BookID, claim.BookID) {
			addIssue("cross_book_reference", KnowledgeEvidenceBlocker, "package_claim", id, claim.BookID)
		}
		chapterID := strings.TrimSpace(claim.ChapterID)
		if chapterID != "" && !validChapters[chapterID] {
			addIssue("claim_chapter_unresolved", KnowledgeEvidenceBlocker, "package_claim", id, chapterID)
			valid = false
		}
		validClaims[id] = valid
	}
	for _, citation := range pkg.Citations {
		id := strings.TrimSpace(citation.CitationID)
		unique := indexRecord("citation", id, citation, citationFingerprints)
		if _, exists := citations[id]; !exists {
			citations[id] = citation
		}
		valid := unique && sameKnowledgeBook(report.BookID, citation.BookID)
		if !sameKnowledgeBook(report.BookID, citation.BookID) {
			addIssue("cross_book_reference", KnowledgeEvidenceBlocker, "citation", id, citation.BookID)
		}
		chunkID := strings.TrimSpace(citation.ChunkID)
		if chunkID == "" || !validChunks[chunkID] {
			addIssue("citation_chunk_unresolved", KnowledgeEvidenceBlocker, "citation", id, chunkID)
			valid = false
		}
		chapterID := strings.TrimSpace(citation.ChapterID)
		if chapterID != "" && !validChapters[chapterID] {
			addIssue("citation_chapter_unresolved", KnowledgeEvidenceBlocker, "citation", id, chapterID)
			valid = false
		}
		if chunkID != "" && validChunks[chunkID] && chapterID != "" {
			if chunkChapterID := strings.TrimSpace(chunks[chunkID].ChapterID); chunkChapterID != "" && chunkChapterID != chapterID {
				addIssue("citation_chunk_chapter_mismatch", KnowledgeEvidenceBlocker, "citation", id, chunkID)
				valid = false
			}
		}
		validCitations[id] = valid
	}
	for id, kinds := range idKinds {
		if len(kinds) > 1 {
			addIssue("ambiguous_object_id", KnowledgeEvidenceBlocker, "package", id, "")
		}
	}

	for _, claim := range pkg.Claims {
		for _, referenceID := range claim.Citations {
			referenceID = strings.TrimSpace(referenceID)
			if referenceID == "" {
				addIssue("package_claim_evidence_unresolved", KnowledgeEvidenceBlocker, "package_claim", claim.ClaimID, "")
				continue
			}
			if !validCitations[referenceID] && !validChunks[referenceID] {
				addIssue("package_claim_evidence_unresolved", KnowledgeEvidenceBlocker, "package_claim", claim.ClaimID, referenceID)
			}
		}
	}

	if analysis == nil || analysis.Payload == nil {
		finalizeKnowledgeEvidenceReport(&report)
		return report
	}
	if !sameKnowledgeBook(report.BookID, analysis.BookID) {
		addIssue("analysis_book_mismatch", KnowledgeEvidenceBlocker, "analysis", analysis.BookID, report.BookID)
	}
	for _, source := range analysis.Sources {
		id := strings.TrimSpace(source.ID)
		valid := false
		switch strings.ToLower(strings.TrimSpace(source.Kind)) {
		case "citation":
			valid = validCitations[id]
		case "chunk":
			valid = validChunks[id]
		case "chapter":
			valid = validChapters[id]
		case "claim":
			valid = validClaims[id]
		}
		if !valid {
			addIssue("declared_source_unresolved", KnowledgeEvidenceBlocker, "analysis_source", id, source.Kind)
			continue
		}
		chapterID := strings.TrimSpace(source.ChapterID)
		if chapterID != "" && !validChapters[chapterID] {
			addIssue("declared_source_chapter_unresolved", KnowledgeEvidenceBlocker, "analysis_source", id, chapterID)
			continue
		}
		if strings.EqualFold(strings.TrimSpace(source.Kind), "chunk") && chapterID != "" {
			if chunkChapterID := strings.TrimSpace(chunks[id].ChapterID); chunkChapterID != "" && chunkChapterID != chapterID {
				addIssue("declared_source_chapter_mismatch", KnowledgeEvidenceBlocker, "analysis_source", id, chapterID)
			}
		}
	}
	for _, claim := range analysis.Payload.Claims {
		result := KnowledgeEvidenceClaimResult{ClaimID: boundedEvidenceID(claim.ID)}
		report.AnalysisClaims++
		if len(claim.CitationIDs) == 0 {
			addIssue("missing_claim_evidence", KnowledgeEvidenceBlocker, "analysis_claim", claim.ID, "")
		}
		for _, rawReferenceID := range claim.CitationIDs {
			referenceID := strings.TrimSpace(rawReferenceID)
			report.EvidenceReferences++
			result.EvidenceReferences++
			switch {
			case referenceID == "":
				addIssue("unresolved_evidence_reference", KnowledgeEvidenceBlocker, "analysis_claim", claim.ID, "")
			case validCitations[referenceID]:
				report.ResolvedReferences++
				report.ExplicitCitationReferences++
				result.ResolvedReferences++
				result.ExplicitCitationReferences++
			case validChunks[referenceID]:
				report.ResolvedReferences++
				report.LegacyDirectChunkReferences++
				result.ResolvedReferences++
				result.LegacyDirectReferences++
				addIssue("legacy_direct_chunk_reference", KnowledgeEvidenceWarning, "analysis_claim", claim.ID, referenceID)
			case validChapters[referenceID] || validClaims[referenceID]:
				report.ResolvedReferences++
				result.ResolvedReferences++
				result.LegacyDirectReferences++
				addIssue("legacy_direct_object_reference", KnowledgeEvidenceWarning, "analysis_claim", claim.ID, referenceID)
			default:
				if len(idKinds[referenceID]) > 1 {
					addIssue("ambiguous_evidence_reference", KnowledgeEvidenceBlocker, "analysis_claim", claim.ID, referenceID)
				} else {
					addIssue("unresolved_evidence_reference", KnowledgeEvidenceBlocker, "analysis_claim", claim.ID, referenceID)
				}
			}
		}
		if result.EvidenceReferences > 0 && result.ResolvedReferences == result.EvidenceReferences {
			report.ClaimsWithEvidence++
		}
		if result.ExplicitCitationReferences > 0 {
			report.ClaimsWithExplicitCitation++
		}
		report.Claims = append(report.Claims, result)
	}
	finalizeKnowledgeEvidenceReport(&report)
	return report
}

func CanonicalKnowledgePublicationIdentity(book BookKnowledgeBook) KnowledgePublicationIdentity {
	sourceType := publicationIdentityComponent(firstNonEmpty(book.SourceType, "unknown"))
	if account := strings.TrimSpace(book.SourceAccount); account != "" {
		return KnowledgePublicationIdentity{
			Key:   "account:" + opaquePublicationIdentityComponent(account),
			Basis: "source_account", IndependentSourceEligible: true,
		}
	}
	if parsed, err := url.Parse(strings.TrimSpace(book.SourceHTML)); err == nil &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" {
		return KnowledgePublicationIdentity{
			Key:   "host:" + publicationIdentityComponent(parsed.Hostname()),
			Basis: "source_host", IndependentSourceEligible: true,
		}
	}
	if author := strings.TrimSpace(book.Author); author != "" && authoredKnowledgeSourceType(book.SourceType) {
		return KnowledgePublicationIdentity{
			Key:   "author:" + opaquePublicationIdentityComponent(author),
			Basis: "source_author", IndependentSourceEligible: true,
		}
	}
	if item := strings.TrimSpace(firstNonEmpty(book.SourceKey, book.EnID)); item != "" {
		return KnowledgePublicationIdentity{
			Key:   "item:" + sourceType + ":" + publicationIdentityComponent(item),
			Basis: "source_item", IndependentSourceEligible: false,
		}
	}
	return KnowledgePublicationIdentity{
		Key:   "book:" + publicationIdentityComponent(firstNonEmpty(book.BookID, "unknown")),
		Basis: "book_fallback", IndependentSourceEligible: false,
	}
}

func finalizeKnowledgeEvidenceReport(report *KnowledgeEvidenceReport) {
	if report.AnalysisClaims > 0 {
		report.ClaimCoverage = float64(report.ClaimsWithEvidence) / float64(report.AnalysisClaims)
		report.ExplicitCitationCoverage = float64(report.ClaimsWithExplicitCitation) / float64(report.AnalysisClaims)
	}
	if report.EvidenceReferences > 0 {
		report.ResolutionRate = float64(report.ResolvedReferences) / float64(report.EvidenceReferences)
	}
	sort.SliceStable(report.Issues, func(i, j int) bool {
		left, right := report.Issues[i], report.Issues[j]
		if evidenceSeverityRank(left.Severity) != evidenceSeverityRank(right.Severity) {
			return evidenceSeverityRank(left.Severity) < evidenceSeverityRank(right.Severity)
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.ObjectKind != right.ObjectKind {
			return left.ObjectKind < right.ObjectKind
		}
		if left.ObjectID != right.ObjectID {
			return left.ObjectID < right.ObjectID
		}
		return left.RelatedID < right.RelatedID
	})
	report.Issues = deduplicateKnowledgeEvidenceIssues(report.Issues)
}

func deduplicateKnowledgeEvidenceIssues(issues []KnowledgeEvidenceIssue) []KnowledgeEvidenceIssue {
	if len(issues) < 2 {
		return issues
	}
	result := make([]KnowledgeEvidenceIssue, 0, len(issues))
	var previous KnowledgeEvidenceIssue
	for index, issue := range issues {
		if index > 0 && issue == previous {
			continue
		}
		result = append(result, issue)
		previous = issue
	}
	return result
}

func evidenceSeverityRank(severity string) int {
	switch severity {
	case KnowledgeEvidenceBlocker:
		return 0
	case KnowledgeEvidenceWarning:
		return 1
	default:
		return 2
	}
}

func knowledgeEvidenceIssueIsStructural(code string) bool {
	switch code {
	case "missing_book_id",
		"missing_object_id",
		"conflicting_duplicate_id",
		"ambiguous_object_id",
		"analysis_book_mismatch",
		"cross_book_reference",
		"chunk_chapter_unresolved",
		"claim_chapter_unresolved",
		"citation_chunk_unresolved",
		"citation_chapter_unresolved",
		"citation_chunk_chapter_mismatch",
		"declared_source_unresolved",
		"declared_source_chapter_unresolved",
		"declared_source_chapter_mismatch",
		"package_claim_evidence_unresolved":
		return true
	default:
		return false
	}
}

func sameKnowledgeBook(bookID, objectBookID string) bool {
	return strings.TrimSpace(bookID) != "" && strings.TrimSpace(objectBookID) == strings.TrimSpace(bookID)
}

func authoredKnowledgeSourceType(sourceType string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "book", "ebook", "dedao_ebook", "audio_book", "audiobook", "dedao_audio":
		return true
	default:
		return false
	}
}

func publicationIdentityComponent(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), "-"))
	if value == "" {
		return "unknown"
	}
	safe := len(value) <= 80
	for _, r := range value {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_') {
			safe = false
			break
		}
	}
	if safe {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256-" + hex.EncodeToString(sum[:8])
}

func opaquePublicationIdentityComponent(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	sum := sha256.Sum256([]byte(value))
	return "sha256-" + hex.EncodeToString(sum[:8])
}

func boundedEvidenceID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 128 && !strings.Contains(value, "/") && !strings.Contains(value, "\\") {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256-" + hex.EncodeToString(sum[:8])
}
