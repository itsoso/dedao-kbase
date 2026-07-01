package app

import (
	"bytes"
	"encoding/json"
	"strings"
)

const HealthAuthorityPackContractV1 = "health_authority_pack_v1"

const healthAuthorityPackCandidateEducationContext = "education_context_candidate"

type HealthAuthorityPack struct {
	ConsumerContract string                      `json:"consumer_contract"`
	ProjectID        string                      `json:"project_id"`
	TargetSystem     string                      `json:"target_system"`
	GeneratedAt      string                      `json:"generated_at"`
	ItemCount        int                         `json:"item_count"`
	Items            []HealthAuthorityPackRecord `json:"items"`
}

type HealthAuthorityPackRecord struct {
	ProjectID         string   `json:"project_id"`
	TargetSystem      string   `json:"target_system"`
	BookID            string   `json:"book_id"`
	BookTitle         string   `json:"book_title"`
	ChapterID         string   `json:"chapter_id,omitempty"`
	ChapterTitle      string   `json:"chapter_title,omitempty"`
	ClaimID           string   `json:"claim_id"`
	Title             string   `json:"title"`
	Summary           string   `json:"summary"`
	VerificationScore float64  `json:"verification_score"`
	RiskTier          string   `json:"risk_tier"`
	Decision          string   `json:"decision"`
	CandidateType     string   `json:"candidate_type"`
	AllowedUses       []string `json:"allowed_uses,omitempty"`
	BlockedUses       []string `json:"blocked_uses,omitempty"`
	RiskFlags         []string `json:"risk_flags,omitempty"`
	Citations         []string `json:"citations,omitempty"`
	SourceHash        string   `json:"source_hash"`
}

type HealthAuthorityPackExportRecord struct {
	ConsumerContract string `json:"consumer_contract"`
	GeneratedAt      string `json:"generated_at"`
	HealthAuthorityPackRecord
}

func (s *BookKnowledgeStore) BuildHealthAuthorityPack(limit int) (*HealthAuthorityPack, error) {
	collection, err := s.RefreshProjectCollection(BookKnowledgeProjectHealth, limit)
	if err != nil {
		return nil, err
	}
	items := make([]HealthAuthorityPackRecord, 0, len(collection.Items))
	for _, item := range collection.Items {
		items = append(items, healthAuthorityPackRecordFromCollection(collection, item))
	}
	return &HealthAuthorityPack{
		ConsumerContract: HealthAuthorityPackContractV1,
		ProjectID:        collection.ProjectID,
		TargetSystem:     collection.Project.TargetSystem,
		GeneratedAt:      collection.GeneratedAt,
		ItemCount:        len(items),
		Items:            items,
	}, nil
}

func (s *BookKnowledgeStore) ExportHealthAuthorityPackJSONL(limit int) ([]byte, error) {
	pack, err := s.BuildHealthAuthorityPack(limit)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	for _, item := range pack.Items {
		record := HealthAuthorityPackExportRecord{
			ConsumerContract:          pack.ConsumerContract,
			GeneratedAt:               pack.GeneratedAt,
			HealthAuthorityPackRecord: item,
		}
		if err := encoder.Encode(record); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func healthAuthorityPackRecordFromCollection(collection *BookKnowledgeProjectCollection, item BookKnowledgeProjectCollectionItem) HealthAuthorityPackRecord {
	riskTier := item.RiskTier
	decision := item.Decision
	if containsHealthSensitiveTerm(item.Title+" "+item.Summary) && riskTier == bookKnowledgeRiskAutoUsable {
		riskTier = bookKnowledgeRiskAssistive
		decision = bookKnowledgeDecisionAssist
	}
	return HealthAuthorityPackRecord{
		ProjectID:         item.ProjectID,
		TargetSystem:      collection.Project.TargetSystem,
		BookID:            item.BookID,
		BookTitle:         item.BookTitle,
		ChapterID:         item.ChapterID,
		ChapterTitle:      item.ChapterTitle,
		ClaimID:           healthAuthorityPackClaimID(item),
		Title:             item.Title,
		Summary:           item.Summary,
		VerificationScore: item.VerificationScore,
		RiskTier:          riskTier,
		Decision:          decision,
		CandidateType:     healthAuthorityPackCandidateEducationContext,
		AllowedUses:       healthAuthorityAllowedUses(item),
		BlockedUses:       healthAuthorityBlockedUses(),
		RiskFlags:         append([]string(nil), item.RiskFlags...),
		Citations:         append([]string(nil), item.Citations...),
		SourceHash:        item.SourceHash,
	}
}

func healthAuthorityPackClaimID(item BookKnowledgeProjectCollectionItem) string {
	if strings.HasPrefix(item.ClaimID, "dedao:") {
		return item.ClaimID
	}
	return "dedao:" + item.BookID + ":" + item.ClaimID
}

func healthAuthorityAllowedUses(item BookKnowledgeProjectCollectionItem) []string {
	allowedUses := []string{"health_education", "context_retrieval"}
	if !containsHealthSensitiveTerm(item.Title + " " + item.Summary) {
		allowedUses = append(allowedUses, "question_preparation")
	}
	return allowedUses
}

func healthAuthorityBlockedUses() []string {
	return []string{"diagnosis", "treatment", "dosage", "medication_change", "emergency_guidance"}
}
