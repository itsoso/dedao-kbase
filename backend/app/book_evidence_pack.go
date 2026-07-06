package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	VerifiedEvidencePackContractV1         = "verified_evidence_pack_v1"
	VerifiedEvidencePullManifestContractV1 = "verified_evidence_pull_manifest_v1"
	verifiedEvidencePackSchemaVersion      = "1"
	verifiedEvidenceSourceTypeDedaoBook    = "dedao_book_claim"
	verifiedEvidencePackDirName            = "evidence_packs"
)

type VerifiedEvidencePack struct {
	ConsumerContract  string                             `json:"consumer_contract"`
	SchemaVersion     string                             `json:"schema_version"`
	PackID            string                             `json:"pack_id"`
	ProjectID         string                             `json:"project_id"`
	TargetSystem      string                             `json:"target_system"`
	ExportType        string                             `json:"export_type"`
	GeneratedAt       string                             `json:"generated_at"`
	SourceFingerprint string                             `json:"source_fingerprint"`
	SourceUnchanged   bool                               `json:"source_unchanged"`
	QualitySummary    VerifiedEvidencePackQualitySummary `json:"quality_summary"`
	Policy            VerifiedEvidencePackPolicy         `json:"policy"`
	Records           []VerifiedEvidencePackRecord       `json:"records"`
}

type VerifiedEvidencePackQualitySummary struct {
	Total             int `json:"total"`
	Accepted          int `json:"accepted"`
	Assistive         int `json:"assistive"`
	Blocked           int `json:"blocked"`
	Invalid           int `json:"invalid"`
	MissingSourceRefs int `json:"missing_source_refs"`
}

type VerifiedEvidencePackPolicy struct {
	DefaultAllowedUses []string `json:"default_allowed_uses,omitempty"`
	DefaultBlockedUses []string `json:"default_blocked_uses,omitempty"`
	HumanLoop          string   `json:"human_loop"`
}

type VerifiedEvidencePackRecord struct {
	EvidenceID        string                     `json:"evidence_id"`
	SourceRefs        VerifiedEvidenceSourceRefs `json:"source_refs"`
	Title             string                     `json:"title"`
	Summary           string                     `json:"summary"`
	NormalizedClaim   string                     `json:"normalized_claim"`
	VerificationScore float64                    `json:"verification_score"`
	QualityStatus     string                     `json:"quality_status"`
	RiskTier          string                     `json:"risk_tier"`
	Decision          string                     `json:"decision"`
	AllowedUses       []string                   `json:"allowed_uses,omitempty"`
	BlockedUses       []string                   `json:"blocked_uses,omitempty"`
	RiskFlags         []string                   `json:"risk_flags,omitempty"`
	RiskReason        string                     `json:"risk_reason,omitempty"`
	FailureReasons    []string                   `json:"failure_reasons,omitempty"`
	Entities          []string                   `json:"entities,omitempty"`
	Audit             VerifiedEvidenceAudit      `json:"audit"`
}

type VerifiedEvidenceSourceRefs struct {
	SourceType   string   `json:"source_type"`
	SourceID     string   `json:"source_id"`
	SourceTitle  string   `json:"source_title"`
	SectionID    string   `json:"section_id,omitempty"`
	SectionTitle string   `json:"section_title,omitempty"`
	ClaimID      string   `json:"claim_id"`
	Citations    []string `json:"citations,omitempty"`
	SourceHash   string   `json:"source_hash"`
}

type VerifiedEvidenceAudit struct {
	ReviewStatus       string   `json:"review_status"`
	AuditStatus        string   `json:"audit_status"`
	SampleReason       string   `json:"sample_reason,omitempty"`
	RecommendedActions []string `json:"recommended_actions,omitempty"`
}

type VerifiedEvidencePackExportRecord struct {
	ConsumerContract  string `json:"consumer_contract"`
	SchemaVersion     string `json:"schema_version"`
	PackID            string `json:"pack_id"`
	ProjectID         string `json:"project_id"`
	TargetSystem      string `json:"target_system"`
	ExportType        string `json:"export_type"`
	GeneratedAt       string `json:"generated_at"`
	SourceFingerprint string `json:"source_fingerprint"`
	VerifiedEvidencePackRecord
}

type VerifiedEvidencePackDiff struct {
	ConsumerContract          string                           `json:"consumer_contract"`
	SchemaVersion             string                           `json:"schema_version"`
	ProjectID                 string                           `json:"project_id"`
	TargetSystem              string                           `json:"target_system"`
	CurrentPackID             string                           `json:"current_pack_id"`
	PreviousPackID            string                           `json:"previous_pack_id"`
	CurrentSourceFingerprint  string                           `json:"current_source_fingerprint"`
	PreviousSourceFingerprint string                           `json:"previous_source_fingerprint"`
	SourceUnchanged           bool                             `json:"source_unchanged"`
	Counts                    VerifiedEvidencePackDiffCounts   `json:"counts"`
	Added                     []VerifiedEvidencePackDiffRecord `json:"added"`
	Removed                   []VerifiedEvidencePackDiffRecord `json:"removed"`
	Changed                   []VerifiedEvidencePackDiffRecord `json:"changed"`
	Unchanged                 []VerifiedEvidencePackDiffRecord `json:"unchanged"`
}

type VerifiedEvidencePackDiffCounts struct {
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
	Blocked   int `json:"blocked"`
}

type VerifiedEvidencePackDiffRecord struct {
	EvidenceID              string                     `json:"evidence_id"`
	ChangeType              string                     `json:"change_type"`
	ChangedFields           []string                   `json:"changed_fields,omitempty"`
	SourceRefs              VerifiedEvidenceSourceRefs `json:"source_refs"`
	CurrentRiskTier         string                     `json:"current_risk_tier,omitempty"`
	PreviousRiskTier        string                     `json:"previous_risk_tier,omitempty"`
	CurrentDecision         string                     `json:"current_decision,omitempty"`
	PreviousDecision        string                     `json:"previous_decision,omitempty"`
	CurrentSourceHash       string                     `json:"current_source_hash,omitempty"`
	PreviousSourceHash      string                     `json:"previous_source_hash,omitempty"`
	CurrentNormalizedClaim  string                     `json:"current_normalized_claim,omitempty"`
	PreviousNormalizedClaim string                     `json:"previous_normalized_claim,omitempty"`
}

type VerifiedEvidencePullManifest struct {
	ConsumerContract string                               `json:"consumer_contract"`
	SchemaVersion    string                               `json:"schema_version"`
	GeneratedAt      string                               `json:"generated_at"`
	ProjectID        string                               `json:"project_id"`
	TargetSystem     string                               `json:"target_system"`
	ExportType       string                               `json:"export_type"`
	CurrentPack      VerifiedEvidencePullManifestPack     `json:"current_pack"`
	Endpoints        VerifiedEvidencePullManifestEndpoint `json:"endpoints"`
	ConsumerGate     VerifiedEvidencePullManifestGate     `json:"consumer_gate"`
	NextActions      []string                             `json:"next_actions"`
}

type VerifiedEvidencePullManifestPack struct {
	ConsumerContract  string                             `json:"consumer_contract"`
	PackID            string                             `json:"pack_id"`
	GeneratedAt       string                             `json:"generated_at"`
	SourceFingerprint string                             `json:"source_fingerprint"`
	SourceUnchanged   bool                               `json:"source_unchanged"`
	RecordCount       int                                `json:"record_count"`
	QualitySummary    VerifiedEvidencePackQualitySummary `json:"quality_summary"`
}

type VerifiedEvidencePullManifestEndpoint struct {
	EvidencePackURL      string `json:"evidence_pack_url"`
	EvidencePackJSONLURL string `json:"evidence_pack_jsonl_url"`
	DiffURLTemplate      string `json:"diff_url_template"`
	DomainPackURL        string `json:"domain_pack_url,omitempty"`
	DomainPackJSONLURL   string `json:"domain_pack_jsonl_url,omitempty"`
}

type VerifiedEvidencePullManifestGate struct {
	Mode                       string   `json:"mode"`
	MustCheckSourceFingerprint bool     `json:"must_check_source_fingerprint"`
	MustRejectBlocked          bool     `json:"must_reject_blocked"`
	AllowedUses                []string `json:"allowed_uses,omitempty"`
	BlockedUses                []string `json:"blocked_uses,omitempty"`
	HumanLoop                  string   `json:"human_loop"`
}

func (s *BookKnowledgeStore) BuildVerifiedEvidencePack(projectID string, limit int) (*VerifiedEvidencePack, error) {
	collection, err := s.RefreshProjectCollection(projectID, limit)
	if err != nil {
		return nil, err
	}
	pack := buildVerifiedEvidencePackFromCollection(collection)
	if err := s.SaveVerifiedEvidencePack(pack); err != nil {
		return nil, err
	}
	return pack, nil
}

func (s *BookKnowledgeStore) ExportVerifiedEvidencePackJSONL(projectID string, limit int) ([]byte, error) {
	pack, err := s.BuildVerifiedEvidencePack(projectID, limit)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	for _, record := range pack.Records {
		exportRecord := VerifiedEvidencePackExportRecord{
			ConsumerContract:           pack.ConsumerContract,
			SchemaVersion:              pack.SchemaVersion,
			PackID:                     pack.PackID,
			ProjectID:                  pack.ProjectID,
			TargetSystem:               pack.TargetSystem,
			ExportType:                 pack.ExportType,
			GeneratedAt:                pack.GeneratedAt,
			SourceFingerprint:          pack.SourceFingerprint,
			VerifiedEvidencePackRecord: record,
		}
		if err := encoder.Encode(exportRecord); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func (s *BookKnowledgeStore) BuildVerifiedEvidencePackDiff(projectID, previousPackID string, limit int) (*VerifiedEvidencePackDiff, error) {
	previousPackID = strings.TrimSpace(previousPackID)
	if previousPackID == "" {
		return nil, fmt.Errorf("previous_pack_id is required")
	}
	previous, err := s.LoadVerifiedEvidencePack(projectID, previousPackID)
	if err != nil {
		return nil, err
	}
	current, err := s.BuildVerifiedEvidencePack(projectID, limit)
	if err != nil {
		return nil, err
	}
	return buildVerifiedEvidencePackDiff(current, previous), nil
}

func (s *BookKnowledgeStore) BuildVerifiedEvidencePullManifest(projectID string, limit int) (*VerifiedEvidencePullManifest, error) {
	pack, err := s.BuildVerifiedEvidencePack(projectID, limit)
	if err != nil {
		return nil, err
	}
	endpoints := verifiedEvidencePullManifestEndpoints(projectID, limit)
	return &VerifiedEvidencePullManifest{
		ConsumerContract: VerifiedEvidencePullManifestContractV1,
		SchemaVersion:    verifiedEvidencePackSchemaVersion,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		ProjectID:        pack.ProjectID,
		TargetSystem:     pack.TargetSystem,
		ExportType:       pack.ExportType,
		CurrentPack: VerifiedEvidencePullManifestPack{
			ConsumerContract:  pack.ConsumerContract,
			PackID:            pack.PackID,
			GeneratedAt:       pack.GeneratedAt,
			SourceFingerprint: pack.SourceFingerprint,
			SourceUnchanged:   pack.SourceUnchanged,
			RecordCount:       len(pack.Records),
			QualitySummary:    pack.QualitySummary,
		},
		Endpoints: endpoints,
		ConsumerGate: VerifiedEvidencePullManifestGate{
			Mode:                       "pull_and_verify",
			MustCheckSourceFingerprint: true,
			MustRejectBlocked:          true,
			AllowedUses:                append([]string(nil), pack.Policy.DefaultAllowedUses...),
			BlockedUses:                append([]string(nil), pack.Policy.DefaultBlockedUses...),
			HumanLoop:                  pack.Policy.HumanLoop,
		},
		NextActions: verifiedEvidencePullManifestNextActions(projectID),
	}, nil
}

func (s *BookKnowledgeStore) ProjectEvidencePackDir(projectID string) string {
	return filepath.Join(s.ProjectDir(projectID), verifiedEvidencePackDirName)
}

func (s *BookKnowledgeStore) VerifiedEvidencePackPath(projectID, packID string) string {
	return filepath.Join(s.ProjectEvidencePackDir(projectID), sanitizeBookKnowledgeID(packID)+".json")
}

func (s *BookKnowledgeStore) SaveVerifiedEvidencePack(pack *VerifiedEvidencePack) error {
	if pack == nil {
		return fmt.Errorf("verified evidence pack is required")
	}
	if strings.TrimSpace(pack.ProjectID) == "" {
		return fmt.Errorf("project_id is required")
	}
	if strings.TrimSpace(pack.PackID) == "" {
		return fmt.Errorf("pack_id is required")
	}
	if err := os.MkdirAll(s.ProjectEvidencePackDir(pack.ProjectID), os.ModePerm); err != nil {
		return err
	}
	return writeJSONFile(s.VerifiedEvidencePackPath(pack.ProjectID, pack.PackID), pack)
}

func (s *BookKnowledgeStore) LoadVerifiedEvidencePack(projectID, packID string) (*VerifiedEvidencePack, error) {
	if _, ok := BookKnowledgeProjectByID(projectID); !ok {
		return nil, fmt.Errorf("unknown book knowledge project: %s", projectID)
	}
	var pack VerifiedEvidencePack
	if err := readJSONFile(s.VerifiedEvidencePackPath(projectID, packID), &pack); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("previous_pack_not_found: %s", packID)
		}
		return nil, err
	}
	if pack.ProjectID != projectID {
		return nil, fmt.Errorf("previous_pack_project_mismatch: %s", packID)
	}
	if pack.Records == nil {
		pack.Records = []VerifiedEvidencePackRecord{}
	}
	return &pack, nil
}

func buildVerifiedEvidencePackFromCollection(collection *BookKnowledgeProjectCollection) *VerifiedEvidencePack {
	auditByClaim := map[string]BookKnowledgeProjectAuditItem{}
	for _, item := range collection.AuditQueue {
		auditByClaim[projectAuditLookupKey(item.ClaimID, item.SourceHash)] = item
	}

	records := make([]VerifiedEvidencePackRecord, 0, len(collection.Items))
	for _, item := range collection.Items {
		audit, hasAudit := auditByClaim[projectAuditLookupKey(item.ClaimID, item.SourceHash)]
		records = append(records, verifiedEvidenceRecordFromCollectionItem(collection, item, audit, hasAudit))
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].EvidenceID < records[j].EvidenceID
	})

	sourceFingerprint := verifiedEvidenceSourceFingerprint(collection.ProjectID, records)
	return &VerifiedEvidencePack{
		ConsumerContract:  VerifiedEvidencePackContractV1,
		SchemaVersion:     verifiedEvidencePackSchemaVersion,
		PackID:            "vep_" + shortSHA256(collection.ProjectID+"|"+sourceFingerprint),
		ProjectID:         collection.ProjectID,
		TargetSystem:      collection.Project.TargetSystem,
		ExportType:        collection.Project.ExportType,
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		SourceFingerprint: sourceFingerprint,
		SourceUnchanged:   false,
		QualitySummary:    verifiedEvidenceQualitySummary(records),
		Policy: VerifiedEvidencePackPolicy{
			DefaultAllowedUses: projectAllowedUses(collection.ProjectID, bookKnowledgeRiskAutoUsable),
			DefaultBlockedUses: projectBlockedUses(collection.ProjectID, bookKnowledgeRiskAutoUsable),
			HumanLoop:          collection.HumanLoop,
		},
		Records: records,
	}
}

func buildVerifiedEvidencePackDiff(current, previous *VerifiedEvidencePack) *VerifiedEvidencePackDiff {
	currentByID := evidencePackRecordMap(current.Records)
	previousByID := evidencePackRecordMap(previous.Records)

	ids := map[string]bool{}
	for id := range currentByID {
		ids[id] = true
	}
	for id := range previousByID {
		ids[id] = true
	}
	sortedIDs := make([]string, 0, len(ids))
	for id := range ids {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)

	diff := &VerifiedEvidencePackDiff{
		ConsumerContract:          current.ConsumerContract,
		SchemaVersion:             current.SchemaVersion,
		ProjectID:                 current.ProjectID,
		TargetSystem:              current.TargetSystem,
		CurrentPackID:             current.PackID,
		PreviousPackID:            previous.PackID,
		CurrentSourceFingerprint:  current.SourceFingerprint,
		PreviousSourceFingerprint: previous.SourceFingerprint,
		SourceUnchanged:           current.SourceFingerprint == previous.SourceFingerprint,
		Added:                     []VerifiedEvidencePackDiffRecord{},
		Removed:                   []VerifiedEvidencePackDiffRecord{},
		Changed:                   []VerifiedEvidencePackDiffRecord{},
		Unchanged:                 []VerifiedEvidencePackDiffRecord{},
	}
	for _, id := range sortedIDs {
		currentRecord, hasCurrent := currentByID[id]
		previousRecord, hasPrevious := previousByID[id]
		switch {
		case hasCurrent && !hasPrevious:
			diff.Added = append(diff.Added, evidencePackDiffRecord("added", currentRecord, VerifiedEvidencePackRecord{}, nil))
		case !hasCurrent && hasPrevious:
			diff.Removed = append(diff.Removed, evidencePackDiffRecord("removed", VerifiedEvidencePackRecord{}, previousRecord, nil))
		default:
			changedFields := evidencePackChangedFields(currentRecord, previousRecord)
			if len(changedFields) > 0 {
				diff.Changed = append(diff.Changed, evidencePackDiffRecord("changed", currentRecord, previousRecord, changedFields))
			} else {
				diff.Unchanged = append(diff.Unchanged, evidencePackDiffRecord("unchanged", currentRecord, previousRecord, nil))
			}
		}
	}
	diff.Counts = VerifiedEvidencePackDiffCounts{
		Added:     len(diff.Added),
		Removed:   len(diff.Removed),
		Changed:   len(diff.Changed),
		Unchanged: len(diff.Unchanged),
		Blocked:   verifiedEvidenceBlockedCount(current.Records),
	}
	return diff
}

func verifiedEvidencePullManifestEndpoints(projectID string, limit int) VerifiedEvidencePullManifestEndpoint {
	projectPath := url.PathEscape(projectID)
	limitSuffix := ""
	if limit > 0 {
		limitSuffix = "&limit=" + url.QueryEscape(fmt.Sprintf("%d", limit))
	}
	packQuery := "?limit=" + url.QueryEscape(fmt.Sprintf("%d", limit))
	if limit <= 0 {
		packQuery = ""
	}
	endpoints := VerifiedEvidencePullManifestEndpoint{
		EvidencePackURL:      "/api/projects/" + projectPath + "/evidence-pack" + packQuery,
		EvidencePackJSONLURL: "/api/projects/" + projectPath + "/evidence-pack/export?format=jsonl" + limitSuffix,
		DiffURLTemplate:      "/api/projects/" + projectPath + "/evidence-pack/diff?previous_pack_id={pack_id}" + limitSuffix,
	}
	switch projectID {
	case BookKnowledgeProjectHealth:
		endpoints.DomainPackURL = "/api/projects/health/authority-pack" + packQuery
		endpoints.DomainPackJSONLURL = "/api/projects/health/authority-pack/export?format=jsonl" + limitSuffix
	case BookKnowledgeProjectProofroom:
		endpoints.DomainPackURL = "/api/projects/proofroom/proofroom-pack" + packQuery
		endpoints.DomainPackJSONLURL = "/api/projects/proofroom/proofroom-pack/export?format=jsonl" + limitSuffix
	}
	return endpoints
}

func verifiedEvidencePullManifestNextActions(projectID string) []string {
	actions := []string{
		"pull:evidence_pack_jsonl",
		"compare:source_fingerprint",
		"reject:blocklisted_or_blocked_records",
	}
	switch projectID {
	case BookKnowledgeProjectHealth:
		actions = append(actions, "run:health_import_dry_run", "gate:clinical_safety_review")
	case BookKnowledgeProjectProofroom:
		actions = append(actions, "run:argument_pack_ingest", "gate:source_citation_review")
	default:
		actions = append(actions, "gate:consumer_policy_review")
	}
	return actions
}

func evidencePackRecordMap(records []VerifiedEvidencePackRecord) map[string]VerifiedEvidencePackRecord {
	byID := make(map[string]VerifiedEvidencePackRecord, len(records))
	for _, record := range records {
		byID[record.EvidenceID] = record
	}
	return byID
}

func evidencePackDiffRecord(
	changeType string,
	current VerifiedEvidencePackRecord,
	previous VerifiedEvidencePackRecord,
	changedFields []string,
) VerifiedEvidencePackDiffRecord {
	evidenceID := firstNonEmpty(current.EvidenceID, previous.EvidenceID)
	sourceRefs := current.SourceRefs
	if sourceRefs.SourceID == "" {
		sourceRefs = previous.SourceRefs
	}
	return VerifiedEvidencePackDiffRecord{
		EvidenceID:              evidenceID,
		ChangeType:              changeType,
		ChangedFields:           changedFields,
		SourceRefs:              sourceRefs,
		CurrentRiskTier:         current.RiskTier,
		PreviousRiskTier:        previous.RiskTier,
		CurrentDecision:         current.Decision,
		PreviousDecision:        previous.Decision,
		CurrentSourceHash:       current.SourceRefs.SourceHash,
		PreviousSourceHash:      previous.SourceRefs.SourceHash,
		CurrentNormalizedClaim:  current.NormalizedClaim,
		PreviousNormalizedClaim: previous.NormalizedClaim,
	}
}

func evidencePackChangedFields(current, previous VerifiedEvidencePackRecord) []string {
	fields := []string{}
	if current.NormalizedClaim != previous.NormalizedClaim {
		fields = append(fields, "normalized_claim")
	}
	if current.SourceRefs.SourceHash != previous.SourceRefs.SourceHash {
		fields = append(fields, "source_hash")
	}
	if !slices.Equal(current.SourceRefs.Citations, previous.SourceRefs.Citations) {
		fields = append(fields, "citations")
	}
	if current.RiskTier != previous.RiskTier {
		fields = append(fields, "risk_tier")
	}
	if current.Decision != previous.Decision {
		fields = append(fields, "decision")
	}
	if current.QualityStatus != previous.QualityStatus {
		fields = append(fields, "quality_status")
	}
	if !slices.Equal(current.AllowedUses, previous.AllowedUses) {
		fields = append(fields, "allowed_uses")
	}
	if !slices.Equal(current.BlockedUses, previous.BlockedUses) {
		fields = append(fields, "blocked_uses")
	}
	if !slices.Equal(current.RiskFlags, previous.RiskFlags) {
		fields = append(fields, "risk_flags")
	}
	return fields
}

func verifiedEvidenceBlockedCount(records []VerifiedEvidencePackRecord) int {
	count := 0
	for _, record := range records {
		if record.RiskTier == bookKnowledgeRiskBlocked || record.QualityStatus == "rejected" {
			count++
		}
	}
	return count
}

func verifiedEvidenceRecordFromCollectionItem(
	collection *BookKnowledgeProjectCollection,
	item BookKnowledgeProjectCollectionItem,
	audit BookKnowledgeProjectAuditItem,
	hasAudit bool,
) VerifiedEvidencePackRecord {
	evidenceID := verifiedEvidenceID(item)
	reviewStatus := "not_required"
	auditStatus := "not_required"
	sampleReason := ""
	failureReasons := []string{}
	if hasAudit {
		reviewStatus = bookKnowledgeProjectAuditStatusPending
		auditStatus = firstNonEmpty(audit.ReviewStatus, bookKnowledgeProjectAuditStatusPending)
		sampleReason = audit.SampleReason
		failureReasons = append([]string(nil), audit.Failures...)
	}
	riskReason := firstNonEmpty(sampleReason, riskReasonFromTier(item.RiskTier))
	return VerifiedEvidencePackRecord{
		EvidenceID: evidenceID,
		SourceRefs: VerifiedEvidenceSourceRefs{
			SourceType:   verifiedEvidenceSourceTypeDedaoBook,
			SourceID:     item.BookID,
			SourceTitle:  item.BookTitle,
			SectionID:    item.ChapterID,
			SectionTitle: item.ChapterTitle,
			ClaimID:      item.ClaimID,
			Citations:    append([]string(nil), item.Citations...),
			SourceHash:   item.SourceHash,
		},
		Title:             item.Title,
		Summary:           item.Summary,
		NormalizedClaim:   firstNonEmpty(item.Summary, item.Title),
		VerificationScore: item.VerificationScore,
		QualityStatus:     qualityStatusFromRiskTier(item.RiskTier),
		RiskTier:          item.RiskTier,
		Decision:          item.Decision,
		AllowedUses:       append([]string(nil), item.AllowedUses...),
		BlockedUses:       append([]string(nil), item.BlockedUses...),
		RiskFlags:         append([]string(nil), item.RiskFlags...),
		RiskReason:        riskReason,
		FailureReasons:    failureReasons,
		Entities:          verifiedEvidenceEntities(collection.ProjectID, item),
		Audit: VerifiedEvidenceAudit{
			ReviewStatus:       reviewStatus,
			AuditStatus:        auditStatus,
			SampleReason:       sampleReason,
			RecommendedActions: verifiedEvidenceRecommendedActions(item, hasAudit, sampleReason),
		},
	}
}

func verifiedEvidenceID(item BookKnowledgeProjectCollectionItem) string {
	if strings.HasPrefix(item.ClaimID, "dedao:") {
		return item.ClaimID
	}
	return "dedao:" + item.BookID + ":" + item.ClaimID
}

func verifiedEvidenceQualitySummary(records []VerifiedEvidencePackRecord) VerifiedEvidencePackQualitySummary {
	summary := VerifiedEvidencePackQualitySummary{Total: len(records)}
	for _, record := range records {
		switch record.RiskTier {
		case bookKnowledgeRiskAutoUsable:
			summary.Accepted++
		case bookKnowledgeRiskAssistive:
			summary.Assistive++
		case bookKnowledgeRiskBlocked:
			summary.Blocked++
		}
		if record.SourceRefs.SourceHash == "" || len(record.SourceRefs.Citations) == 0 {
			summary.MissingSourceRefs++
		}
		if record.EvidenceID == "" || record.NormalizedClaim == "" {
			summary.Invalid++
		}
	}
	return summary
}

func verifiedEvidenceSourceFingerprint(projectID string, records []VerifiedEvidencePackRecord) string {
	type fingerprintRecord struct {
		EvidenceID      string   `json:"evidence_id"`
		NormalizedClaim string   `json:"normalized_claim"`
		SourceHash      string   `json:"source_hash"`
		Citations       []string `json:"citations,omitempty"`
		RiskTier        string   `json:"risk_tier"`
		Decision        string   `json:"decision"`
	}
	items := make([]fingerprintRecord, 0, len(records))
	for _, record := range records {
		items = append(items, fingerprintRecord{
			EvidenceID:      record.EvidenceID,
			NormalizedClaim: record.NormalizedClaim,
			SourceHash:      record.SourceRefs.SourceHash,
			Citations:       append([]string(nil), record.SourceRefs.Citations...),
			RiskTier:        record.RiskTier,
			Decision:        record.Decision,
		})
	}
	payload, _ := json.Marshal(struct {
		ProjectID string              `json:"project_id"`
		Records   []fingerprintRecord `json:"records"`
	}{ProjectID: projectID, Records: items})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func qualityStatusFromRiskTier(riskTier string) string {
	switch riskTier {
	case bookKnowledgeRiskAutoUsable:
		return "usable"
	case bookKnowledgeRiskAssistive, bookKnowledgeRiskNeedsHuman:
		return "needs_review"
	case bookKnowledgeRiskBlocked:
		return "rejected"
	default:
		return "needs_review"
	}
}

func riskReasonFromTier(riskTier string) string {
	switch riskTier {
	case bookKnowledgeRiskAutoUsable:
		return "machine_verified"
	case bookKnowledgeRiskAssistive:
		return "assistive_only"
	case bookKnowledgeRiskBlocked:
		return "blocked"
	default:
		return "needs_review"
	}
}

func verifiedEvidenceEntities(projectID string, item BookKnowledgeProjectCollectionItem) []string {
	if projectID == BookKnowledgeProjectHealth {
		return healthAuthorityEntityCandidates(item)
	}
	entities := []string{}
	if item.BookTitle != "" {
		entities = append(entities, item.BookTitle)
	}
	if item.ChapterTitle != "" && item.ChapterTitle != item.BookTitle {
		entities = append(entities, item.ChapterTitle)
	}
	return entities
}

func verifiedEvidenceRecommendedActions(item BookKnowledgeProjectCollectionItem, hasAudit bool, sampleReason string) []string {
	if hasAudit {
		return []string{"review_evidence:" + firstNonEmpty(sampleReason, "pending_async_audit")}
	}
	if item.Decision == bookKnowledgeDecisionAllow {
		return []string{"allow_for_project_pack"}
	}
	return []string{"use_as_assistive_context"}
}
