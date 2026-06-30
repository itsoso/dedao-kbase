package app

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	BookKnowledgeQualityStatusUsable      = "usable"
	BookKnowledgeQualityStatusNeedsReview = "needs_review"
	BookKnowledgeQualityStatusRejected    = "rejected"

	bookKnowledgeQualityReportFileName = "quality_report.json"
	bookKnowledgeQualityVersion        = "1"
)

type BookKnowledgeQualityReport struct {
	Version     string                      `json:"version"`
	BookID      string                      `json:"book_id"`
	GeneratedAt string                      `json:"generated_at"`
	Status      string                      `json:"status"`
	Score       float64                     `json:"score"`
	Metrics     BookKnowledgeQualityMetrics `json:"metrics"`
	Issues      []BookKnowledgeQualityIssue `json:"issues,omitempty"`
	AllowedUses []string                    `json:"allowed_uses,omitempty"`
	BlockedUses []string                    `json:"blocked_uses,omitempty"`
}

type BookKnowledgeQualityMetrics struct {
	Chapters            int     `json:"chapters"`
	Chunks              int     `json:"chunks"`
	Claims              int     `json:"claims"`
	Citations           int     `json:"citations"`
	EmptyChunkRatio     float64 `json:"empty_chunk_ratio"`
	DuplicateClaimRatio float64 `json:"duplicate_claim_ratio"`
	AverageChunkChars   int     `json:"average_chunk_chars"`
	SourcePresent       bool    `json:"source_present"`
}

type BookKnowledgeQualityIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

func BuildBookKnowledgeQualityReport(pkg BookKnowledgePackage) BookKnowledgeQualityReport {
	metrics := bookKnowledgeQualityMetrics(pkg)
	issues := bookKnowledgeQualityIssues(metrics)
	score := bookKnowledgeQualityScore(issues)
	status := bookKnowledgeQualityStatus(score, issues)
	return BookKnowledgeQualityReport{
		Version:     bookKnowledgeQualityVersion,
		BookID:      pkg.Book.BookID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Status:      status,
		Score:       score,
		Metrics:     metrics,
		Issues:      issues,
		AllowedUses: bookKnowledgeQualityAllowedUses(status),
		BlockedUses: bookKnowledgeQualityBlockedUses(status),
	}
}

func (s *BookKnowledgeStore) QualityReportPath(bookID string) string {
	return filepath.Join(s.BookDir(bookID), bookKnowledgeQualityReportFileName)
}

func (s *BookKnowledgeStore) LoadBookQualityReport(bookID string) (*BookKnowledgeQualityReport, error) {
	bookID = sanitizeBookKnowledgeID(bookID)
	if strings.TrimSpace(bookID) == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	var report BookKnowledgeQualityReport
	if err := readJSONFile(s.QualityReportPath(bookID), &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func (s *BookKnowledgeStore) saveBookQualityReport(report BookKnowledgeQualityReport) error {
	if strings.TrimSpace(report.BookID) == "" {
		return fmt.Errorf("quality report missing book_id")
	}
	return writeJSONFile(s.QualityReportPath(report.BookID), report)
}

func bookKnowledgeQualityMetrics(pkg BookKnowledgePackage) BookKnowledgeQualityMetrics {
	emptyChunks := 0
	totalChunkChars := 0
	for _, chunk := range pkg.Chunks {
		text := strings.TrimSpace(chunk.Text)
		if text == "" {
			emptyChunks++
			continue
		}
		totalChunkChars += len([]rune(text))
	}
	duplicateClaims := 0
	seenClaims := map[string]bool{}
	for _, claim := range pkg.Claims {
		key := strings.ToLower(strings.TrimSpace(claim.Title + "\n" + claim.Summary))
		if key == "" {
			continue
		}
		if seenClaims[key] {
			duplicateClaims++
			continue
		}
		seenClaims[key] = true
	}
	return BookKnowledgeQualityMetrics{
		Chapters:            len(pkg.Chapters),
		Chunks:              len(pkg.Chunks),
		Claims:              len(pkg.Claims),
		Citations:           len(pkg.Citations),
		EmptyChunkRatio:     roundedRatio(emptyChunks, len(pkg.Chunks)),
		DuplicateClaimRatio: roundedRatio(duplicateClaims, len(pkg.Claims)),
		AverageChunkChars:   averageInt(totalChunkChars, len(pkg.Chunks)-emptyChunks),
		SourcePresent:       strings.TrimSpace(pkg.Book.SourceHTML) != "",
	}
}

func bookKnowledgeQualityIssues(metrics BookKnowledgeQualityMetrics) []BookKnowledgeQualityIssue {
	issues := []BookKnowledgeQualityIssue{}
	if metrics.Chapters == 0 {
		issues = append(issues, qualityIssue("missing_chapters", "high", "book has no extracted chapters"))
	}
	if metrics.Chunks == 0 {
		issues = append(issues, qualityIssue("missing_chunks", "high", "book has no searchable chunks"))
	}
	if metrics.Claims == 0 {
		issues = append(issues, qualityIssue("missing_claims", "medium", "book has no extracted claims"))
	}
	if metrics.Citations == 0 {
		issues = append(issues, qualityIssue("missing_citations", "medium", "book has no citations"))
	}
	if metrics.EmptyChunkRatio >= 0.5 && metrics.Chunks > 0 {
		issues = append(issues, qualityIssue("high_empty_chunk_ratio", "high", "too many chunks are empty"))
	} else if metrics.EmptyChunkRatio >= 0.25 {
		issues = append(issues, qualityIssue("elevated_empty_chunk_ratio", "medium", "some chunks are empty"))
	}
	if metrics.DuplicateClaimRatio >= 0.35 {
		issues = append(issues, qualityIssue("high_duplicate_claim_ratio", "medium", "too many claims appear duplicated"))
	}
	if metrics.AverageChunkChars > 0 && metrics.AverageChunkChars < 80 {
		issues = append(issues, qualityIssue("short_average_chunk", "low", "average chunk text is short"))
	}
	if !metrics.SourcePresent {
		issues = append(issues, qualityIssue("missing_source", "low", "book source is not recorded"))
	}
	return issues
}

func bookKnowledgeQualityScore(issues []BookKnowledgeQualityIssue) float64 {
	score := 1.0
	for _, issue := range issues {
		switch issue.Severity {
		case "high":
			score -= 0.4
		case "medium":
			score -= 0.18
		default:
			score -= 0.06
		}
	}
	if score < 0 {
		score = 0
	}
	return math.Round(score*100) / 100
}

func bookKnowledgeQualityStatus(score float64, issues []BookKnowledgeQualityIssue) string {
	for _, issue := range issues {
		if issue.Severity == "high" {
			return BookKnowledgeQualityStatusRejected
		}
	}
	if score < 0.5 {
		return BookKnowledgeQualityStatusRejected
	}
	if score < 0.75 || len(issues) > 0 {
		return BookKnowledgeQualityStatusNeedsReview
	}
	return BookKnowledgeQualityStatusUsable
}

func bookKnowledgeQualityAllowedUses(status string) []string {
	switch status {
	case BookKnowledgeQualityStatusUsable:
		return []string{"study", "context_retrieval", "draft_reference", "project_verification_input"}
	case BookKnowledgeQualityStatusNeedsReview:
		return []string{"study", "context_retrieval", "review_queue"}
	default:
		return nil
	}
}

func bookKnowledgeQualityBlockedUses(status string) []string {
	switch status {
	case BookKnowledgeQualityStatusRejected:
		return []string{"all_downstream_use"}
	case BookKnowledgeQualityStatusNeedsReview:
		return []string{"auto_publish", "medical_action", "final_answer_without_review"}
	default:
		return []string{"medical_action", "final_answer_without_review"}
	}
}

func qualityIssue(code, severity, message string) BookKnowledgeQualityIssue {
	return BookKnowledgeQualityIssue{Code: code, Severity: severity, Message: message}
}

func roundedRatio(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round((float64(part)/float64(total))*100) / 100
}

func averageInt(total, count int) int {
	if count <= 0 {
		return 0
	}
	return int(math.Round(float64(total) / float64(count)))
}

func isMissingQualityReport(err error) bool {
	return os.IsNotExist(err)
}
