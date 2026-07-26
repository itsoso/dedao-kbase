package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const KnowledgeReadinessSchemaVersion = "knowledge_readiness.v1"

type KnowledgeReadiness struct {
	SchemaVersion string                    `json:"schema_version"`
	Summary       KnowledgeReadinessSummary `json:"summary"`
	Items         []KnowledgeReadinessItem  `json:"items"`
}

type KnowledgeReadinessSummary struct {
	Total                      int     `json:"total"`
	Ready                      int     `json:"ready"`
	NeedsAnalysis              int     `json:"needs_analysis"`
	NeedsQuality               int     `json:"needs_quality"`
	ReadyToPublish             int     `json:"ready_to_publish"`
	Published                  int     `json:"published"`
	Blocked                    int     `json:"blocked"`
	AnalysisClaims             int     `json:"analysis_claims"`
	ClaimsWithEvidence         int     `json:"claims_with_evidence"`
	ClaimsWithExplicitCitation int     `json:"claims_with_explicit_citation"`
	EvidenceReferences         int     `json:"evidence_references"`
	ResolvedReferences         int     `json:"resolved_references"`
	ClaimCoverage              float64 `json:"claim_coverage"`
	ResolutionRate             float64 `json:"resolution_rate"`
	ExplicitCitationCoverage   float64 `json:"explicit_citation_coverage"`
}

type KnowledgeReadinessItem struct {
	BookID                      string                       `json:"book_id"`
	Title                       string                       `json:"title"`
	SourceType                  string                       `json:"source_type,omitempty"`
	SourceAccount               string                       `json:"source_account,omitempty"`
	Publication                 KnowledgePublicationIdentity `json:"publication"`
	Stage                       string                       `json:"stage"`
	NextAction                  string                       `json:"next_action"`
	UpdatedAt                   string                       `json:"updated_at,omitempty"`
	LastPublishedReleaseID      string                       `json:"last_published_release_id,omitempty"`
	LastPublishedAt             string                       `json:"last_published_at,omitempty"`
	AnalysisClaims              int                          `json:"analysis_claims"`
	ClaimsWithEvidence          int                          `json:"claims_with_evidence"`
	ClaimsWithExplicitCitation  int                          `json:"claims_with_explicit_citation"`
	EvidenceReferences          int                          `json:"evidence_references"`
	ResolvedReferences          int                          `json:"resolved_references"`
	ExplicitCitationReferences  int                          `json:"explicit_citation_references"`
	LegacyDirectChunkReferences int                          `json:"legacy_direct_chunk_references"`
	ClaimCoverage               float64                      `json:"claim_coverage"`
	ResolutionRate              float64                      `json:"resolution_rate"`
	ExplicitCitationCoverage    float64                      `json:"explicit_citation_coverage"`
	BlockerCodes                []string                     `json:"blocker_codes"`
	WarningCodes                []string                     `json:"warning_codes"`
}

func BuildKnowledgeReadiness(store *BookKnowledgeStore, limit int, bookID string) (*KnowledgeReadiness, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	bookID = strings.TrimSpace(bookID)
	books, err := store.ListBooks()
	if err != nil {
		return nil, err
	}
	result := &KnowledgeReadiness{
		SchemaVersion: KnowledgeReadinessSchemaVersion,
		Items:         make([]KnowledgeReadinessItem, 0, minInt(limit, len(books))),
	}
	for _, book := range books {
		if bookID != "" && book.BookID != bookID {
			continue
		}
		if len(result.Items) >= limit {
			break
		}
		pkg, err := store.LoadPackage(book.BookID)
		if err != nil {
			return nil, err
		}
		projection, err := deriveKnowledgePipelineProjection(store, book, time.Now)
		if err != nil {
			return nil, err
		}
		var analysis *BookAnalysisManifest
		analysis, err = store.LoadAnalysisManifest(book.BookID)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			analysis = nil
		}
		evidence := EvaluateKnowledgeEvidence(*pkg, analysis)
		nextAction := knowledgePipelineNextAction(projection)
		if evidence.HasBlockers() {
			nextAction = "blocked"
		}
		item := KnowledgeReadinessItem{
			BookID:                      book.BookID,
			Title:                       book.Title,
			SourceType:                  book.SourceType,
			SourceAccount:               book.SourceAccount,
			Publication:                 evidence.Publication,
			Stage:                       projection.Stage,
			NextAction:                  nextAction,
			UpdatedAt:                   projection.UpdatedAt,
			LastPublishedReleaseID:      projection.LastPublishedReleaseID,
			LastPublishedAt:             projection.LastPublishedAt,
			AnalysisClaims:              evidence.AnalysisClaims,
			ClaimsWithEvidence:          evidence.ClaimsWithEvidence,
			ClaimsWithExplicitCitation:  evidence.ClaimsWithExplicitCitation,
			EvidenceReferences:          evidence.EvidenceReferences,
			ResolvedReferences:          evidence.ResolvedReferences,
			ExplicitCitationReferences:  evidence.ExplicitCitationReferences,
			LegacyDirectChunkReferences: evidence.LegacyDirectChunkReferences,
			ClaimCoverage:               evidence.ClaimCoverage,
			ResolutionRate:              evidence.ResolutionRate,
			ExplicitCitationCoverage:    evidence.ExplicitCitationCoverage,
			BlockerCodes:                boundedKnowledgeEvidenceCodes(evidence.Issues, KnowledgeEvidenceBlocker, 20),
			WarningCodes:                boundedKnowledgeEvidenceCodes(evidence.Issues, KnowledgeEvidenceWarning, 20),
		}
		result.Items = append(result.Items, item)
		accumulateKnowledgeReadinessSummary(&result.Summary, item)
	}
	finalizeKnowledgeReadinessSummary(&result.Summary)
	return result, nil
}

func ValidateKnowledgeReadinessContract(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, required := range []string{"schema_version", "summary", "items"} {
		if value, exists := fields[required]; !exists || len(value) == 0 || string(value) == "null" {
			return fmt.Errorf("%s is required", required)
		}
	}
	var readiness KnowledgeReadiness
	if err := json.Unmarshal(raw, &readiness); err != nil {
		return err
	}
	if readiness.SchemaVersion != KnowledgeReadinessSchemaVersion {
		return fmt.Errorf("schema_version must be %q", KnowledgeReadinessSchemaVersion)
	}
	if err := validateKnowledgeReadinessSummary(readiness.Summary); err != nil {
		return err
	}
	for index, item := range readiness.Items {
		if err := requireContractFields(map[string]string{
			"book_id":           item.BookID,
			"title":             item.Title,
			"publication.key":   item.Publication.Key,
			"publication.basis": item.Publication.Basis,
			"stage":             item.Stage,
			"next_action":       item.NextAction,
		}); err != nil {
			return fmt.Errorf("items[%d]: %w", index, err)
		}
		if !validKnowledgeReadinessAction(item.NextAction) {
			return fmt.Errorf("items[%d].next_action is invalid", index)
		}
		if err := validateKnowledgeReadinessMetrics(
			item.AnalysisClaims,
			item.ClaimsWithEvidence,
			item.ClaimsWithExplicitCitation,
			item.EvidenceReferences,
			item.ResolvedReferences,
			item.ClaimCoverage,
			item.ResolutionRate,
			item.ExplicitCitationCoverage,
		); err != nil {
			return fmt.Errorf("items[%d]: %w", index, err)
		}
	}
	return nil
}

func accumulateKnowledgeReadinessSummary(summary *KnowledgeReadinessSummary, item KnowledgeReadinessItem) {
	summary.Total++
	switch item.NextAction {
	case "needs_analysis":
		summary.NeedsAnalysis++
	case "needs_quality":
		summary.NeedsQuality++
	case "ready_to_publish":
		summary.ReadyToPublish++
		summary.Ready++
	case "published":
		summary.Published++
		summary.Ready++
	default:
		summary.Blocked++
	}
	summary.AnalysisClaims += item.AnalysisClaims
	summary.ClaimsWithEvidence += item.ClaimsWithEvidence
	summary.ClaimsWithExplicitCitation += item.ClaimsWithExplicitCitation
	summary.EvidenceReferences += item.EvidenceReferences
	summary.ResolvedReferences += item.ResolvedReferences
}

func finalizeKnowledgeReadinessSummary(summary *KnowledgeReadinessSummary) {
	if summary.AnalysisClaims > 0 {
		summary.ClaimCoverage = float64(summary.ClaimsWithEvidence) / float64(summary.AnalysisClaims)
		summary.ExplicitCitationCoverage = float64(summary.ClaimsWithExplicitCitation) / float64(summary.AnalysisClaims)
	}
	if summary.EvidenceReferences > 0 {
		summary.ResolutionRate = float64(summary.ResolvedReferences) / float64(summary.EvidenceReferences)
	}
}

func boundedKnowledgeEvidenceCodes(issues []KnowledgeEvidenceIssue, severity string, limit int) []string {
	if limit <= 0 {
		return []string{}
	}
	result := make([]string, 0, minInt(limit, len(issues)))
	seen := make(map[string]struct{})
	for _, issue := range issues {
		if issue.Severity != severity {
			continue
		}
		if _, exists := seen[issue.Code]; exists {
			continue
		}
		seen[issue.Code] = struct{}{}
		result = append(result, issue.Code)
		if len(result) == limit {
			break
		}
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func validateKnowledgeReadinessSummary(summary KnowledgeReadinessSummary) error {
	for name, value := range map[string]int{
		"summary.total":            summary.Total,
		"summary.ready":            summary.Ready,
		"summary.needs_analysis":   summary.NeedsAnalysis,
		"summary.needs_quality":    summary.NeedsQuality,
		"summary.ready_to_publish": summary.ReadyToPublish,
		"summary.published":        summary.Published,
		"summary.blocked":          summary.Blocked,
	} {
		if value < 0 {
			return fmt.Errorf("%s must be non-negative", name)
		}
	}
	return validateKnowledgeReadinessMetrics(
		summary.AnalysisClaims,
		summary.ClaimsWithEvidence,
		summary.ClaimsWithExplicitCitation,
		summary.EvidenceReferences,
		summary.ResolvedReferences,
		summary.ClaimCoverage,
		summary.ResolutionRate,
		summary.ExplicitCitationCoverage,
	)
}

func validateKnowledgeReadinessMetrics(
	analysisClaims,
	claimsWithEvidence,
	claimsWithExplicitCitation,
	evidenceReferences,
	resolvedReferences int,
	claimCoverage,
	resolutionRate,
	explicitCitationCoverage float64,
) error {
	for name, value := range map[string]int{
		"analysis_claims":               analysisClaims,
		"claims_with_evidence":          claimsWithEvidence,
		"claims_with_explicit_citation": claimsWithExplicitCitation,
		"evidence_references":           evidenceReferences,
		"resolved_references":           resolvedReferences,
	} {
		if value < 0 {
			return fmt.Errorf("%s must be non-negative", name)
		}
	}
	for name, value := range map[string]float64{
		"claim_coverage":             claimCoverage,
		"resolution_rate":            resolutionRate,
		"explicit_citation_coverage": explicitCitationCoverage,
	} {
		if value < 0 || value > 1 {
			return fmt.Errorf("%s must be between 0 and 1", name)
		}
	}
	return nil
}

func validKnowledgeReadinessAction(action string) bool {
	switch action {
	case "needs_analysis", "needs_quality", "ready_to_publish", "published", "blocked":
		return true
	default:
		return false
	}
}
