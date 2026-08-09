package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	bookQualityVersion = "1"

	BookQualityPass       = "pass"
	BookQualityQuarantine = "quarantine"
	BookQualityReject     = "reject"

	BookUsageStandard     = "standard"
	BookUsageEvidenceOnly = "evidence_only"
)

type BookQualityRule struct {
	ID      string `json:"id"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
	Hard    bool   `json:"hard,omitempty"`
}

type BookQualityReport struct {
	Version      string            `json:"version"`
	BookID       string            `json:"book_id"`
	ContentHash  string            `json:"content_hash"`
	AnalysisHash string            `json:"analysis_hash"`
	Decision     string            `json:"decision"`
	UsagePolicy  string            `json:"usage_policy"`
	Rules        []BookQualityRule `json:"rules"`
	EvaluatedAt  string            `json:"evaluated_at"`
}

func (s *BookKnowledgeStore) BookQualityReportPath(bookID string) string {
	return filepath.Join(s.BookDir(bookID), "quality_report.json")
}

func (s *BookKnowledgeStore) SaveBookQualityReport(report BookQualityReport) error {
	return s.SaveBookQualityReportContext(context.Background(), report)
}

func (s *BookKnowledgeStore) SaveBookQualityReportContext(ctx context.Context, report BookQualityReport) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	rootLock, err := s.acquireBookKnowledgeRootLock(ctx)
	if err != nil {
		return err
	}
	defer rootLock.Close()
	return s.saveBookQualityReportUnlocked(report)
}

func (s *BookKnowledgeStore) saveBookQualityReportUnlocked(report BookQualityReport) error {
	report.BookID = sanitizeBookKnowledgeID(report.BookID)
	if strings.TrimSpace(report.BookID) == "" {
		return fmt.Errorf("quality report missing book_id")
	}
	if strings.TrimSpace(report.Version) == "" {
		report.Version = bookQualityVersion
	}
	payload, err := encodeJSONFile(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.BookDir(report.BookID), os.ModePerm); err != nil {
		return err
	}
	return writeFileAtomically(s.BookQualityReportPath(report.BookID), payload)
}

func (s *BookKnowledgeStore) LoadBookQualityReport(bookID string) (*BookQualityReport, error) {
	return s.LoadBookQualityReportContext(context.Background(), bookID)
}

func (s *BookKnowledgeStore) LoadBookQualityReportContext(ctx context.Context, bookID string) (*BookQualityReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rootLock, err := s.acquireBookKnowledgeRootReadLock(ctx)
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	return s.loadBookQualityReportUnlocked(bookID)
}

func (s *BookKnowledgeStore) loadBookQualityReportUnlocked(bookID string) (*BookQualityReport, error) {
	bookID = sanitizeBookKnowledgeID(bookID)
	if strings.TrimSpace(bookID) == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	var report BookQualityReport
	if err := readJSONFile(s.BookQualityReportPath(bookID), &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func EvaluateBookAnalysisQuality(store *BookKnowledgeStore, bookID string) (*BookQualityReport, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	pkg, err := store.LoadPackage(bookID)
	if err != nil {
		return nil, err
	}
	manifest, err := store.LoadAnalysisManifest(bookID)
	if err != nil {
		return nil, err
	}
	analysisHash, err := bookAnalysisHash(*manifest)
	if err != nil {
		return nil, err
	}
	report := BookQualityReport{
		Version:      bookQualityVersion,
		BookID:       pkg.Book.BookID,
		ContentHash:  pkg.Book.ContentHash,
		AnalysisHash: analysisHash,
		Decision:     BookQualityPass,
		UsagePolicy:  BookUsageStandard,
		EvaluatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	addRule := func(id string, passed, hard bool, message string) {
		report.Rules = append(report.Rules, BookQualityRule{ID: id, Passed: passed, Hard: hard, Message: message})
		if passed {
			return
		}
		if hard {
			report.Decision = BookQualityReject
		} else if report.Decision != BookQualityReject {
			report.Decision = BookQualityQuarantine
		}
	}

	addRule("manifest_ready", manifest.Status == BookAnalysisReady, true, "analysis manifest must be ready")
	addRule("content_version", manifest.ContentHash != "" && manifest.ContentHash == pkg.Book.ContentHash, true, "analysis content hash must match the current package")
	addRule("structured_payload", manifest.Payload != nil && strings.TrimSpace(manifest.Payload.Summary) != "" && len(manifest.Payload.Claims) > 0, true, "structured summary and at least one claim are required")

	validCitationIDs := make(map[string]struct{})
	for _, chapter := range pkg.Chapters {
		validCitationIDs[chapter.ChapterID] = struct{}{}
	}
	for _, claim := range pkg.Claims {
		validCitationIDs[claim.ClaimID] = struct{}{}
	}
	for _, chunk := range pkg.Chunks {
		validCitationIDs[chunk.ChunkID] = struct{}{}
	}
	for _, citation := range pkg.Citations {
		validCitationIDs[citation.CitationID] = struct{}{}
	}
	for _, source := range manifest.Sources {
		validCitationIDs[source.ID] = struct{}{}
	}

	claimsHaveCitations := manifest.Payload != nil
	citationsValid := manifest.Payload != nil
	claimMetadataValid := manifest.Payload != nil
	if manifest.Payload != nil {
		for _, claim := range manifest.Payload.Claims {
			if len(claim.CitationIDs) == 0 {
				claimsHaveCitations = false
			}
			for _, id := range claim.CitationIDs {
				if _, ok := validCitationIDs[id]; !ok {
					citationsValid = false
				}
			}
			if strings.TrimSpace(claim.ID) == "" || strings.TrimSpace(claim.Statement) == "" || claim.Confidence < 0 || claim.Confidence > 1 || !validBookRiskLevel(claim.RiskLevel) {
				claimMetadataValid = false
			}
			if strings.EqualFold(strings.TrimSpace(claim.RiskLevel), "high") {
				report.UsagePolicy = BookUsageEvidenceOnly
			}
		}
	}
	addRule("claim_citations", claimsHaveCitations, false, "every claim must cite at least one source ID")
	addRule("citation_integrity", citationsValid, false, "every cited source ID must belong to the current package")
	addRule("claim_metadata", claimMetadataValid, false, "claim ids, statements, confidence, and risk levels must be valid")
	evidence := EvaluateKnowledgeEvidence(*pkg, manifest)
	addRule("evidence_structure", !evidence.HasStructuralBlockers(), true, "package evidence identities and edges must be internally consistent")
	addRule("evidence_reference_resolution", !evidence.HasBlockers(), false, "every analysis claim evidence reference must resolve in the current package")

	if err := store.SaveBookQualityReport(report); err != nil {
		return nil, err
	}
	return &report, nil
}

func bookAnalysisHash(manifest BookAnalysisManifest) (string, error) {
	seed := struct {
		ContentHash   string                    `json:"content_hash"`
		Model         string                    `json:"model"`
		PromptVersion string                    `json:"prompt_version"`
		Payload       *BookAnalysisPayload      `json:"payload"`
		Sources       []BookKnowledgeChatSource `json:"sources"`
	}{manifest.ContentHash, manifest.Model, manifest.PromptVersion, manifest.Payload, manifest.Sources}
	payload, err := json.Marshal(seed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func validBookRiskLevel(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}
