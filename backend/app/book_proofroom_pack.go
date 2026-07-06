package app

import (
	"bytes"
	"encoding/json"
)

const ProofroomArgumentPackContractV1 = "proofroom_argument_pack_v1"

type ProofroomArgumentPack struct {
	ConsumerContract            string                        `json:"consumer_contract"`
	ProjectID                   string                        `json:"project_id"`
	TargetSystem                string                        `json:"target_system"`
	BasePackID                  string                        `json:"base_pack_id"`
	SourceFingerprint           string                        `json:"source_fingerprint"`
	GeneratedAt                 string                        `json:"generated_at"`
	ItemCount                   int                           `json:"item_count"`
	ReviewCount                 int                           `json:"review_count"`
	ContradictionCandidateCount int                           `json:"contradiction_candidate_count"`
	Items                       []ProofroomArgumentPackRecord `json:"items"`
}

type ProofroomArgumentPackRecord struct {
	EvidenceID             string                     `json:"evidence_id"`
	SourceRefs             VerifiedEvidenceSourceRefs `json:"source_refs"`
	Title                  string                     `json:"title"`
	Summary                string                     `json:"summary"`
	NormalizedClaim        string                     `json:"normalized_claim"`
	ArgumentRoles          []string                   `json:"argument_roles"`
	ContradictionCandidate bool                       `json:"contradiction_candidate"`
	ReviewStatus           string                     `json:"review_status"`
	RiskTier               string                     `json:"risk_tier"`
	Decision               string                     `json:"decision"`
	VerificationScore      float64                    `json:"verification_score"`
	AllowedUses            []string                   `json:"allowed_uses,omitempty"`
	BlockedUses            []string                   `json:"blocked_uses,omitempty"`
	RiskFlags              []string                   `json:"risk_flags,omitempty"`
	RiskReason             string                     `json:"risk_reason,omitempty"`
	FailureReasons         []string                   `json:"failure_reasons,omitempty"`
	Citations              []string                   `json:"citations,omitempty"`
	SourceHash             string                     `json:"source_hash"`
}

type ProofroomArgumentPackExportRecord struct {
	ConsumerContract  string `json:"consumer_contract"`
	BasePackID        string `json:"base_pack_id"`
	SourceFingerprint string `json:"source_fingerprint"`
	ProjectID         string `json:"project_id"`
	TargetSystem      string `json:"target_system"`
	GeneratedAt       string `json:"generated_at"`
	ProofroomArgumentPackRecord
}

func (s *BookKnowledgeStore) BuildProofroomArgumentPack(limit int) (*ProofroomArgumentPack, error) {
	basePack, err := s.BuildVerifiedEvidencePack(BookKnowledgeProjectProofroom, limit)
	if err != nil {
		return nil, err
	}
	items := make([]ProofroomArgumentPackRecord, 0, len(basePack.Records))
	reviewCount := 0
	contradictionCount := 0
	for _, record := range basePack.Records {
		item := proofroomArgumentPackRecordFromEvidence(record)
		items = append(items, item)
		if item.ReviewStatus != "ready_for_argument_draft" {
			reviewCount++
		}
		if item.ContradictionCandidate {
			contradictionCount++
		}
	}
	return &ProofroomArgumentPack{
		ConsumerContract:            ProofroomArgumentPackContractV1,
		ProjectID:                   basePack.ProjectID,
		TargetSystem:                basePack.TargetSystem,
		BasePackID:                  basePack.PackID,
		SourceFingerprint:           basePack.SourceFingerprint,
		GeneratedAt:                 basePack.GeneratedAt,
		ItemCount:                   len(items),
		ReviewCount:                 reviewCount,
		ContradictionCandidateCount: contradictionCount,
		Items:                       items,
	}, nil
}

func (s *BookKnowledgeStore) ExportProofroomArgumentPackJSONL(limit int) ([]byte, error) {
	pack, err := s.BuildProofroomArgumentPack(limit)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	for _, item := range pack.Items {
		record := ProofroomArgumentPackExportRecord{
			ConsumerContract:            pack.ConsumerContract,
			BasePackID:                  pack.BasePackID,
			SourceFingerprint:           pack.SourceFingerprint,
			ProjectID:                   pack.ProjectID,
			TargetSystem:                pack.TargetSystem,
			GeneratedAt:                 pack.GeneratedAt,
			ProofroomArgumentPackRecord: item,
		}
		if err := encoder.Encode(record); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func proofroomArgumentPackRecordFromEvidence(record VerifiedEvidencePackRecord) ProofroomArgumentPackRecord {
	contradictionCandidate := proofroomContradictionCandidate(record)
	return ProofroomArgumentPackRecord{
		EvidenceID:             record.EvidenceID,
		SourceRefs:             record.SourceRefs,
		Title:                  record.Title,
		Summary:                record.Summary,
		NormalizedClaim:        record.NormalizedClaim,
		ArgumentRoles:          proofroomArgumentRoles(record, contradictionCandidate),
		ContradictionCandidate: contradictionCandidate,
		ReviewStatus:           proofroomReviewStatus(record, contradictionCandidate),
		RiskTier:               record.RiskTier,
		Decision:               record.Decision,
		VerificationScore:      record.VerificationScore,
		AllowedUses:            append([]string(nil), record.AllowedUses...),
		BlockedUses:            append([]string(nil), record.BlockedUses...),
		RiskFlags:              append([]string(nil), record.RiskFlags...),
		RiskReason:             record.RiskReason,
		FailureReasons:         append([]string(nil), record.FailureReasons...),
		Citations:              append([]string(nil), record.SourceRefs.Citations...),
		SourceHash:             record.SourceRefs.SourceHash,
	}
}

func proofroomArgumentRoles(record VerifiedEvidencePackRecord, contradictionCandidate bool) []string {
	roles := []string{"claim"}
	if proofroomHasSourceRefs(record) {
		roles = append(roles, "support")
	}
	if contradictionCandidate {
		roles = append(roles, "counterpoint", "question")
	} else if !proofroomHasSourceRefs(record) {
		roles = append(roles, "question")
	}
	return roles
}

func proofroomReviewStatus(record VerifiedEvidencePackRecord, contradictionCandidate bool) string {
	if !proofroomHasSourceRefs(record) {
		return "needs_source_review"
	}
	if contradictionCandidate {
		return "needs_corroboration"
	}
	return "ready_for_argument_draft"
}

func proofroomContradictionCandidate(record VerifiedEvidencePackRecord) bool {
	return len(record.RiskFlags) > 0 ||
		record.VerificationScore < 0.55 ||
		record.RiskTier != bookKnowledgeRiskAutoUsable
}

func proofroomHasSourceRefs(record VerifiedEvidencePackRecord) bool {
	return record.SourceRefs.SourceHash != "" && len(record.SourceRefs.Citations) > 0
}
