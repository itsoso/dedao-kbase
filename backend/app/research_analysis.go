package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	ResearchIdentityResolved  = "resolved"
	ResearchIdentityAmbiguous = "ambiguous"
	ResearchIdentityNotFound  = "not_found"

	ResearchAnalysisNotFound = "not_found"
	ResearchFactRecovery     = "recovery"

	ResearchTrendUp    = "up"
	ResearchTrendDown  = "down"
	ResearchTrendFlat  = "flat"
	ResearchTrendMixed = "mixed"

	ResearchClaimDirectRecommendation = "direct_recommendation"
	ResearchClaimGeneralDiscussion    = "general_discussion"

	ResearchCaseTransferComparable = "comparable"
	ResearchCaseTransferLimited    = "limited"

	ResearchAnalysisFact           = "fact"
	ResearchAnalysisIntervention   = "intervention"
	ResearchAnalysisMeasurement    = "measurement"
	ResearchAnalysisTimelineEvent  = "timeline_event"
	ResearchAnalysisConflict       = "conflict"
	ResearchAnalysisCaseDifference = "case_difference"

	ResearchReviewPending  = "pending"
	ResearchReviewVerified = "verified"
	ResearchReviewRejected = "rejected"
)

type ResearchIdentityCandidate struct {
	IdentityID             string   `json:"identity_id"`
	DisplayName            string   `json:"display_name,omitempty"`
	Aliases                []string `json:"aliases,omitempty"`
	AccountID              string   `json:"account_id,omitempty"`
	TargetAccountID        string   `json:"target_account_id,omitempty"`
	DisplayNameMatch       bool     `json:"display_name_match,omitempty"`
	ContactMetadataMatch   bool     `json:"contact_metadata_match,omitempty"`
	GroupMembershipMatch   bool     `json:"group_membership_match,omitempty"`
	ConversationContinuity bool     `json:"conversation_continuity,omitempty"`
	SelfIdentification     bool     `json:"self_identification,omitempty"`
	ConfirmedBinding       bool     `json:"confirmed_binding,omitempty"`
}

type ResearchIdentityDecision struct {
	Status       string   `json:"status"`
	IdentityID   string   `json:"identity_id,omitempty"`
	CandidateIDs []string `json:"candidate_ids,omitempty"`
	Reasons      []string `json:"reasons,omitempty"`
	Confidence   float64  `json:"confidence"`
}

type ResearchFact struct {
	FactID      string   `json:"fact_id"`
	Kind        string   `json:"kind"`
	Summary     string   `json:"summary"`
	Status      string   `json:"status,omitempty"`
	OccurredAt  string   `json:"occurred_at,omitempty"`
	EvidenceIDs []string `json:"evidence_ids"`
	Confidence  float64  `json:"confidence"`
	ReviewState string   `json:"review_state"`
}

type ResearchTimelineEvent struct {
	TimelineEventID string   `json:"timeline_event_id"`
	FactID          string   `json:"fact_id"`
	Kind            string   `json:"kind"`
	Summary         string   `json:"summary"`
	Status          string   `json:"status,omitempty"`
	OccurredAt      string   `json:"occurred_at,omitempty"`
	EvidenceIDs     []string `json:"evidence_ids"`
	Confidence      float64  `json:"confidence"`
	ReviewState     string   `json:"review_state"`
}

type ResearchNumericTrend struct {
	Direction    string  `json:"direction"`
	NetDirection string  `json:"net_direction"`
	Delta        float64 `json:"delta"`
	Increases    int     `json:"increases"`
	Decreases    int     `json:"decreases"`
	Unchanged    int     `json:"unchanged"`
}

type ResearchClaim struct {
	ClaimID     string   `json:"claim_id"`
	Kind        string   `json:"kind"`
	Topic       string   `json:"topic"`
	Value       string   `json:"value,omitempty"`
	Timing      string   `json:"timing,omitempty"`
	Amount      string   `json:"amount,omitempty"`
	AppliesTo   string   `json:"applies_to,omitempty"`
	EvidenceIDs []string `json:"evidence_ids"`
	Confidence  float64  `json:"confidence,omitempty"`
	ReviewState string   `json:"review_state,omitempty"`
}

type ResearchConflict struct {
	ConflictID  string   `json:"conflict_id"`
	Topic       string   `json:"topic"`
	ClaimIDs    []string `json:"claim_ids"`
	Dimensions  []string `json:"dimensions"`
	EvidenceIDs []string `json:"evidence_ids"`
	Confidence  float64  `json:"confidence"`
	ReviewState string   `json:"review_state"`
}

type ResearchCase struct {
	CaseID         string             `json:"case_id"`
	Age            int                `json:"age,omitempty"`
	StageDay       int                `json:"stage_day,omitempty"`
	Symptoms       []string           `json:"symptoms,omitempty"`
	Measurements   map[string]float64 `json:"measurements,omitempty"`
	RecoveryStatus string             `json:"recovery_status,omitempty"`
	EvidenceIDs    []string           `json:"evidence_ids,omitempty"`
}

type ResearchCaseDifference struct {
	Dimension string `json:"dimension"`
	Left      string `json:"left"`
	Right     string `json:"right"`
}

type ResearchCaseComparison struct {
	LeftCaseID          string                   `json:"left_case_id"`
	RightCaseID         string                   `json:"right_case_id"`
	Transferability     string                   `json:"transferability"`
	MaterialDifferences []ResearchCaseDifference `json:"material_differences"`
}

type ResearchAnalysisRecord struct {
	RecordID           string         `json:"record_id"`
	Kind               string         `json:"kind"`
	Summary            string         `json:"summary"`
	Attributes         map[string]any `json:"attributes,omitempty"`
	SupportEvidenceIDs []string       `json:"support_evidence_ids"`
	Confidence         float64        `json:"confidence"`
	ReviewState        string         `json:"review_state"`
	CreatedAt          string         `json:"created_at,omitempty"`
}

func ResolveResearchIdentity(candidates []ResearchIdentityCandidate) ResearchIdentityDecision {
	if len(candidates) == 0 {
		return ResearchIdentityDecision{Status: ResearchIdentityNotFound}
	}
	strong := make([]ResearchIdentityCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		exactAccount := strings.TrimSpace(candidate.AccountID) != "" &&
			candidate.AccountID == candidate.TargetAccountID
		if candidate.ConfirmedBinding || exactAccount || candidate.SelfIdentification {
			strong = append(strong, candidate)
		}
	}
	if len(strong) == 1 {
		reasons := []string{}
		if strong[0].ConfirmedBinding {
			reasons = append(reasons, "confirmed_binding")
		}
		if strings.TrimSpace(strong[0].AccountID) != "" && strong[0].AccountID == strong[0].TargetAccountID {
			reasons = append(reasons, "exact_account_id")
		}
		if strong[0].SelfIdentification {
			reasons = append(reasons, "self_identification")
		}
		return ResearchIdentityDecision{
			Status: ResearchIdentityResolved, IdentityID: strong[0].IdentityID,
			CandidateIDs: []string{strong[0].IdentityID}, Reasons: reasons, Confidence: 1,
		}
	}
	plausible := candidates
	if len(strong) > 1 {
		plausible = strong
	}
	ids := make([]string, 0, len(plausible))
	for _, candidate := range plausible {
		if id := strings.TrimSpace(candidate.IdentityID); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ResearchIdentityDecision{
		Status: ResearchIdentityAmbiguous, CandidateIDs: ids,
		Reasons: []string{"multiple_plausible_candidates"}, Confidence: 0,
	}
}

func BuildResearchTimeline(evidence []ResearchEvidence, facts []ResearchFact) []ResearchTimelineEvent {
	accessible := make(map[string]bool, len(evidence))
	for _, item := range evidence {
		if strings.TrimSpace(item.EvidenceID) != "" && item.SourceType != ResearchEvidenceSourceDerived {
			accessible[item.EvidenceID] = true
		}
	}
	events := make([]ResearchTimelineEvent, 0, len(facts))
	for _, fact := range facts {
		support := uniqueSortedResearchStrings(fact.EvidenceIDs)
		grounded := false
		for _, evidenceID := range support {
			if accessible[evidenceID] && evidenceID != fact.FactID {
				grounded = true
				break
			}
		}
		if !grounded {
			continue
		}
		events = append(events, ResearchTimelineEvent{
			TimelineEventID: "timeline-" + researchAnalysisID(fact.FactID, fact.Kind, fact.OccurredAt),
			FactID:          fact.FactID, Kind: fact.Kind, Summary: fact.Summary, Status: fact.Status,
			OccurredAt: fact.OccurredAt, EvidenceIDs: support, Confidence: fact.Confidence, ReviewState: fact.ReviewState,
		})
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].OccurredAt != events[j].OccurredAt {
			return events[i].OccurredAt < events[j].OccurredAt
		}
		return events[i].TimelineEventID < events[j].TimelineEventID
	})
	return events
}

func ClassifyResearchNumericTrend(values []float64) ResearchNumericTrend {
	trend := ResearchNumericTrend{Direction: ResearchTrendFlat, NetDirection: ResearchTrendFlat}
	if len(values) == 0 {
		return trend
	}
	trend.Delta = values[len(values)-1] - values[0]
	for index := 1; index < len(values); index++ {
		switch {
		case values[index] > values[index-1]:
			trend.Increases++
		case values[index] < values[index-1]:
			trend.Decreases++
		default:
			trend.Unchanged++
		}
	}
	switch {
	case trend.Increases > 0 && trend.Decreases > 0:
		trend.Direction = ResearchTrendMixed
	case trend.Increases > 0:
		trend.Direction = ResearchTrendUp
	case trend.Decreases > 0:
		trend.Direction = ResearchTrendDown
	}
	switch {
	case trend.Delta > 0:
		trend.NetDirection = ResearchTrendUp
	case trend.Delta < 0:
		trend.NetDirection = ResearchTrendDown
	}
	return trend
}

func DetectResearchConflicts(claims []ResearchClaim) []ResearchConflict {
	direct := make([]ResearchClaim, 0, len(claims))
	for _, claim := range claims {
		if claim.Kind == ResearchClaimDirectRecommendation && strings.TrimSpace(claim.Topic) != "" {
			direct = append(direct, claim)
		}
	}
	sort.Slice(direct, func(i, j int) bool { return direct[i].ClaimID < direct[j].ClaimID })
	conflicts := []ResearchConflict{}
	for left := 0; left < len(direct); left++ {
		for right := left + 1; right < len(direct); right++ {
			if direct[left].Topic != direct[right].Topic || direct[left].AppliesTo != direct[right].AppliesTo {
				continue
			}
			dimensions := []string{}
			for _, field := range []struct{ name, left, right string }{
				{"amount", direct[left].Amount, direct[right].Amount},
				{"timing", direct[left].Timing, direct[right].Timing},
				{"value", direct[left].Value, direct[right].Value},
			} {
				if strings.TrimSpace(field.left) != "" && strings.TrimSpace(field.right) != "" && field.left != field.right {
					dimensions = append(dimensions, field.name)
				}
			}
			if len(dimensions) == 0 {
				continue
			}
			confidence := direct[left].Confidence
			if confidence == 0 || (direct[right].Confidence > 0 && direct[right].Confidence < confidence) {
				confidence = direct[right].Confidence
			}
			conflicts = append(conflicts, ResearchConflict{
				ConflictID: "conflict-" + researchAnalysisID(direct[left].ClaimID, direct[right].ClaimID),
				Topic:      direct[left].Topic, ClaimIDs: []string{direct[left].ClaimID, direct[right].ClaimID},
				Dimensions:  dimensions,
				EvidenceIDs: uniqueSortedResearchStrings(append(append([]string{}, direct[left].EvidenceIDs...), direct[right].EvidenceIDs...)),
				Confidence:  confidence, ReviewState: ResearchReviewPending,
			})
		}
	}
	return conflicts
}

func CompareResearchCases(left, right ResearchCase) ResearchCaseComparison {
	comparison := ResearchCaseComparison{
		LeftCaseID: left.CaseID, RightCaseID: right.CaseID,
		Transferability: ResearchCaseTransferComparable, MaterialDifferences: []ResearchCaseDifference{},
	}
	appendDifference := func(dimension, leftValue, rightValue string) {
		if leftValue != rightValue {
			comparison.MaterialDifferences = append(comparison.MaterialDifferences, ResearchCaseDifference{
				Dimension: dimension, Left: leftValue, Right: rightValue,
			})
		}
	}
	appendDifference("age", strconv.Itoa(left.Age), strconv.Itoa(right.Age))
	appendDifference("stage_day", strconv.Itoa(left.StageDay), strconv.Itoa(right.StageDay))
	appendDifference("symptoms", strings.Join(uniqueSortedResearchStrings(left.Symptoms), ","), strings.Join(uniqueSortedResearchStrings(right.Symptoms), ","))
	appendDifference("recovery_status", left.RecoveryStatus, right.RecoveryStatus)
	measurementKeys := map[string]bool{}
	for key := range left.Measurements {
		measurementKeys[key] = true
	}
	for key := range right.Measurements {
		measurementKeys[key] = true
	}
	keys := make([]string, 0, len(measurementKeys))
	for key := range measurementKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		appendDifference("measurement:"+key, strconv.FormatFloat(left.Measurements[key], 'g', -1, 64), strconv.FormatFloat(right.Measurements[key], 'g', -1, 64))
	}
	if len(comparison.MaterialDifferences) > 0 {
		comparison.Transferability = ResearchCaseTransferLimited
	}
	return comparison
}

func (s *ResearchStore) StoreResearchAnalysisRecords(runID string, records []ResearchAnalysisRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("research store is required")
	}
	if err := migrateResearchAnalysisRecords(s.db); err != nil {
		return err
	}
	if _, err := s.LoadRun(runID); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := s.now().UTC().Format(timeRFC3339Nano)
	for _, record := range records {
		if err := validateResearchAnalysisRecord(tx, runID, record); err != nil {
			return err
		}
		supportJSON, err := json.Marshal(uniqueSortedResearchStrings(record.SupportEvidenceIDs))
		if err != nil {
			return err
		}
		attributesJSON, err := json.Marshal(record.Attributes)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO research_analysis_records (
			run_id, record_id, kind, summary, attributes_json, support_evidence_ids_json,
			confidence, review_state, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, record_id) DO UPDATE SET kind = excluded.kind, summary = excluded.summary,
			attributes_json = excluded.attributes_json, support_evidence_ids_json = excluded.support_evidence_ids_json,
			confidence = excluded.confidence, review_state = excluded.review_state`, strings.TrimSpace(runID),
			record.RecordID, record.Kind, record.Summary, string(attributesJSON), string(supportJSON),
			record.Confidence, record.ReviewState, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *ResearchStore) ListResearchAnalysisRecords(runID string) ([]ResearchAnalysisRecord, error) {
	if err := migrateResearchAnalysisRecords(s.db); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT record_id, kind, summary, attributes_json,
		support_evidence_ids_json, confidence, review_state, created_at
		FROM research_analysis_records WHERE run_id = ? ORDER BY created_at ASC, record_id ASC`, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ResearchAnalysisRecord{}
	for rows.Next() {
		var record ResearchAnalysisRecord
		var attributesJSON, supportJSON string
		if err := rows.Scan(&record.RecordID, &record.Kind, &record.Summary, &attributesJSON,
			&supportJSON, &record.Confidence, &record.ReviewState, &record.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(attributesJSON), &record.Attributes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(supportJSON), &record.SupportEvidenceIDs); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func migrateResearchAnalysisRecords(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS research_analysis_records (
		run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
		record_id TEXT NOT NULL, kind TEXT NOT NULL, summary TEXT NOT NULL,
		attributes_json TEXT NOT NULL DEFAULT '{}', support_evidence_ids_json TEXT NOT NULL DEFAULT '[]',
		confidence REAL NOT NULL, review_state TEXT NOT NULL, created_at TEXT NOT NULL,
		PRIMARY KEY (run_id, record_id)
	)`)
	return err
}

func validateResearchAnalysisRecord(tx *sql.Tx, runID string, record ResearchAnalysisRecord) error {
	allowedKinds := map[string]bool{
		ResearchAnalysisFact: true, ResearchAnalysisIntervention: true, ResearchAnalysisMeasurement: true,
		ResearchAnalysisTimelineEvent: true, ResearchAnalysisConflict: true, ResearchAnalysisCaseDifference: true,
	}
	if strings.TrimSpace(record.RecordID) == "" || !allowedKinds[record.Kind] || strings.TrimSpace(record.Summary) == "" {
		return fmt.Errorf("analysis record id, supported kind, and summary are required")
	}
	if record.Confidence <= 0 || record.Confidence > 1 {
		return fmt.Errorf("analysis record confidence must be within (0,1]")
	}
	if record.ReviewState != ResearchReviewPending && record.ReviewState != ResearchReviewVerified && record.ReviewState != ResearchReviewRejected {
		return fmt.Errorf("analysis record review_state is invalid")
	}
	support := uniqueSortedResearchStrings(record.SupportEvidenceIDs)
	if len(support) == 0 {
		return fmt.Errorf("analysis record requires accessible source evidence")
	}
	for _, evidenceID := range support {
		var sourceType string
		err := tx.QueryRow(`SELECT source_type FROM research_evidence
			WHERE run_id = ? AND evidence_id = ? AND selected = 1`, strings.TrimSpace(runID), evidenceID).Scan(&sourceType)
		if errors.Is(err, sql.ErrNoRows) || sourceType == ResearchEvidenceSourceDerived {
			return fmt.Errorf("analysis record requires accessible source evidence")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func uniqueSortedResearchStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func researchAnalysisID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:8])
}

const timeRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
