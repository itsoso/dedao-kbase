package app

import (
	"bytes"
	"encoding/json"
	"strings"
)

const HealthAuthorityPackContractV1 = "health_authority_pack_v1"

const healthAuthorityPackCandidateEducationContext = "education_context_candidate"

type HealthAuthorityPack struct {
	ConsumerContract  string                      `json:"consumer_contract"`
	ProjectID         string                      `json:"project_id"`
	TargetSystem      string                      `json:"target_system"`
	BasePackID        string                      `json:"base_pack_id,omitempty"`
	SourceFingerprint string                      `json:"source_fingerprint,omitempty"`
	GeneratedAt       string                      `json:"generated_at"`
	ItemCount         int                         `json:"item_count"`
	ReviewableCount   int                         `json:"reviewable_count"`
	BlockedCount      int                         `json:"blocked_count"`
	RiskReasonCounts  map[string]int              `json:"risk_reason_counts"`
	Items             []HealthAuthorityPackRecord `json:"items"`
}

type HealthAuthorityPackRecord struct {
	EvidenceID        string                    `json:"evidence_id,omitempty"`
	ProjectID         string                    `json:"project_id"`
	TargetSystem      string                    `json:"target_system"`
	BookID            string                    `json:"book_id"`
	BookTitle         string                    `json:"book_title"`
	ChapterID         string                    `json:"chapter_id,omitempty"`
	ChapterTitle      string                    `json:"chapter_title,omitempty"`
	ClaimID           string                    `json:"claim_id"`
	Title             string                    `json:"title"`
	Summary           string                    `json:"summary"`
	VerificationScore float64                   `json:"verification_score"`
	RiskTier          string                    `json:"risk_tier"`
	Decision          string                    `json:"decision"`
	CandidateType     string                    `json:"candidate_type"`
	ReviewStatus      string                    `json:"review_status"`
	RiskReason        string                    `json:"risk_reason"`
	EntityCandidates  []string                  `json:"entity_candidates,omitempty"`
	AllowedUses       []string                  `json:"allowed_uses,omitempty"`
	BlockedUses       []string                  `json:"blocked_uses,omitempty"`
	RiskFlags         []string                  `json:"risk_flags,omitempty"`
	Citations         []string                  `json:"citations,omitempty"`
	SourceHash        string                    `json:"source_hash"`
	SourceRefs        HealthAuthoritySourceRefs `json:"source_refs"`
}

type HealthAuthoritySourceRefs struct {
	SourceType   string   `json:"source_type,omitempty"`
	SourceID     string   `json:"source_id,omitempty"`
	BookID       string   `json:"book_id"`
	BookTitle    string   `json:"book_title"`
	ChapterID    string   `json:"chapter_id,omitempty"`
	ChapterTitle string   `json:"chapter_title,omitempty"`
	ClaimID      string   `json:"claim_id"`
	Citations    []string `json:"citations,omitempty"`
	SourceHash   string   `json:"source_hash"`
}

type HealthAuthorityPackExportRecord struct {
	ConsumerContract  string `json:"consumer_contract"`
	BasePackID        string `json:"base_pack_id,omitempty"`
	SourceFingerprint string `json:"source_fingerprint,omitempty"`
	GeneratedAt       string `json:"generated_at"`
	HealthAuthorityPackRecord
}

func (s *BookKnowledgeStore) BuildHealthAuthorityPack(limit int) (*HealthAuthorityPack, error) {
	basePack, err := s.BuildVerifiedEvidencePack(BookKnowledgeProjectHealth, limit)
	if err != nil {
		return nil, err
	}
	items := make([]HealthAuthorityPackRecord, 0, len(basePack.Records))
	riskReasonCounts := map[string]int{}
	blockedCount := 0
	for _, item := range basePack.Records {
		record := healthAuthorityPackRecordFromEvidence(basePack, item)
		items = append(items, record)
		riskReasonCounts[record.RiskReason]++
		if record.ReviewStatus == "blocked" {
			blockedCount++
		}
	}
	return &HealthAuthorityPack{
		ConsumerContract:  HealthAuthorityPackContractV1,
		ProjectID:         basePack.ProjectID,
		TargetSystem:      basePack.TargetSystem,
		BasePackID:        basePack.PackID,
		SourceFingerprint: basePack.SourceFingerprint,
		GeneratedAt:       basePack.GeneratedAt,
		ItemCount:         len(items),
		ReviewableCount:   len(items) - blockedCount,
		BlockedCount:      blockedCount,
		RiskReasonCounts:  riskReasonCounts,
		Items:             items,
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
			BasePackID:                pack.BasePackID,
			SourceFingerprint:         pack.SourceFingerprint,
			GeneratedAt:               pack.GeneratedAt,
			HealthAuthorityPackRecord: item,
		}
		if err := encoder.Encode(record); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func healthAuthorityPackRecordFromEvidence(pack *VerifiedEvidencePack, item VerifiedEvidencePackRecord) HealthAuthorityPackRecord {
	text := item.Title + " " + item.Summary
	riskTier := item.RiskTier
	decision := item.Decision
	if containsHealthSensitiveTerm(text) && riskTier == bookKnowledgeRiskAutoUsable {
		riskTier = bookKnowledgeRiskAssistive
		decision = bookKnowledgeDecisionAssist
	}
	claimID := item.EvidenceID
	reviewStatus, riskReason := healthAuthorityReviewMetadata(text)
	return HealthAuthorityPackRecord{
		EvidenceID:        item.EvidenceID,
		ProjectID:         pack.ProjectID,
		TargetSystem:      pack.TargetSystem,
		BookID:            item.SourceRefs.SourceID,
		BookTitle:         item.SourceRefs.SourceTitle,
		ChapterID:         item.SourceRefs.SectionID,
		ChapterTitle:      item.SourceRefs.SectionTitle,
		ClaimID:           claimID,
		Title:             item.Title,
		Summary:           item.Summary,
		VerificationScore: item.VerificationScore,
		RiskTier:          riskTier,
		Decision:          decision,
		CandidateType:     healthAuthorityPackCandidateEducationContext,
		ReviewStatus:      reviewStatus,
		RiskReason:        riskReason,
		EntityCandidates:  firstNonEmptySlice(item.Entities, healthAuthorityEntityCandidatesForText(text)),
		AllowedUses:       healthAuthorityAllowedUsesForText(text),
		BlockedUses:       healthAuthorityBlockedUses(),
		RiskFlags:         append([]string(nil), item.RiskFlags...),
		Citations:         append([]string(nil), item.SourceRefs.Citations...),
		SourceHash:        item.SourceRefs.SourceHash,
		SourceRefs: HealthAuthoritySourceRefs{
			SourceType:   item.SourceRefs.SourceType,
			SourceID:     item.SourceRefs.SourceID,
			BookID:       item.SourceRefs.SourceID,
			BookTitle:    item.SourceRefs.SourceTitle,
			ChapterID:    item.SourceRefs.SectionID,
			ChapterTitle: item.SourceRefs.SectionTitle,
			ClaimID:      claimID,
			Citations:    append([]string(nil), item.SourceRefs.Citations...),
			SourceHash:   item.SourceRefs.SourceHash,
		},
	}
}

func healthAuthorityAllowedUsesForText(text string) []string {
	allowedUses := []string{"health_education", "context_retrieval"}
	if !containsHealthSensitiveTerm(text) {
		allowedUses = append(allowedUses, "question_preparation")
	}
	return allowedUses
}

func healthAuthorityBlockedUses() []string {
	return []string{"diagnosis", "treatment", "dosage", "medication_change", "emergency_guidance"}
}

func healthAuthorityReviewMetadata(text string) (string, string) {
	if containsHealthActionBoundaryTerm(text) {
		return "blocked", "medical_action_boundary"
	}
	if containsHealthSensitiveTerm(text) {
		return "education_only", "health_sensitive_education_only"
	}
	return "needs_review", "dedao_educational_source"
}

func healthAuthorityEntityCandidates(item BookKnowledgeProjectCollectionItem) []string {
	return healthAuthorityEntityCandidatesForText(item.Title + " " + item.Summary)
}

func healthAuthorityEntityCandidatesForText(text string) []string {
	candidates := []string{}
	add := func(value string) {
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}
	if strings.Contains(text, "用药") || strings.Contains(text, "药物") || strings.Contains(text, "剂量") {
		add("用药安全")
	}
	if strings.Contains(text, "睡眠") {
		add("睡眠管理")
	}
	if strings.Contains(text, "血压") || strings.Contains(text, "血糖") {
		add("慢病指标")
	}
	if strings.Contains(text, "复盘") || strings.Contains(strings.ToLower(text), "review") {
		add("学习复盘")
	}
	if len(candidates) == 0 {
		add("健康教育")
	}
	return candidates
}

func firstNonEmptySlice(primary, fallback []string) []string {
	if len(primary) > 0 {
		return append([]string(nil), primary...)
	}
	return append([]string(nil), fallback...)
}

func containsHealthActionBoundaryTerm(text string) bool {
	normalized := strings.ToLower(text)
	for _, term := range []string{
		"诊断", "治疗", "用药", "剂量", "药物", "处方", "急症", "急救", "手术",
		"diagnosis", "treatment", "medicine", "medication", "dose", "dosage", "emergency",
	} {
		if strings.Contains(normalized, strings.ToLower(term)) {
			return true
		}
	}
	return false
}
