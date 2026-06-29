package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

const (
	bookKnowledgeAutonomyMode = "machine_verified_async_audit"
	bookKnowledgeHumanLoop    = "async_audit_only"
	bookKnowledgeReviewSample = "async_sample"

	bookKnowledgeRiskAutoUsable = "auto_usable"
	bookKnowledgeRiskAssistive  = "assistive_only"
	bookKnowledgeRiskNeedsHuman = "needs_human"
	bookKnowledgeRiskBlocked    = "blocked"
	bookKnowledgeDecisionAllow  = "allow"
	bookKnowledgeDecisionAssist = "assist"
	bookKnowledgeDecisionQueue  = "queue"
	bookKnowledgeDecisionBlock  = "block"
	bookKnowledgeCheckPass      = "pass"
	bookKnowledgeCheckWarn      = "warn"
	bookKnowledgeCheckFail      = "fail"
)

type BookKnowledgeProjectVerificationReport struct {
	ProjectID      string                                 `json:"project_id"`
	Project        BookKnowledgeProject                   `json:"project"`
	AutonomyMode   string                                 `json:"autonomy_mode"`
	HumanLoop      string                                 `json:"human_loop"`
	ReviewSampling string                                 `json:"review_sampling"`
	Items          []BookKnowledgeVerifiedItem            `json:"items"`
	Total          int                                    `json:"total"`
	Limit          int                                    `json:"limit"`
	TierCounts     map[string]int                         `json:"tier_counts"`
	DecisionCounts map[string]int                         `json:"decision_counts"`
	Policy         BookKnowledgeProjectVerificationPolicy `json:"policy"`
}

type BookKnowledgeProjectVerificationPolicy struct {
	DefaultRiskTier string   `json:"default_risk_tier"`
	AutoUseTiers    []string `json:"auto_use_tiers"`
	AssistiveTiers  []string `json:"assistive_tiers"`
	BlockedUses     []string `json:"blocked_uses"`
}

type BookKnowledgeVerifiedItem struct {
	ProjectID         string                              `json:"project_id"`
	BookID            string                              `json:"book_id"`
	BookTitle         string                              `json:"book_title"`
	ChapterID         string                              `json:"chapter_id,omitempty"`
	ChapterTitle      string                              `json:"chapter_title,omitempty"`
	ClaimID           string                              `json:"claim_id"`
	Title             string                              `json:"title"`
	Summary           string                              `json:"summary"`
	VerificationScore float64                             `json:"verification_score"`
	RiskTier          string                              `json:"risk_tier"`
	Decision          string                              `json:"decision"`
	Checks            []BookKnowledgeVerificationCheck    `json:"checks"`
	FailureReasons    []string                            `json:"failure_reasons,omitempty"`
	AllowedUses       []string                            `json:"allowed_uses,omitempty"`
	BlockedUses       []string                            `json:"blocked_uses,omitempty"`
	RiskFlags         []string                            `json:"risk_flags,omitempty"`
	Provenance        BookKnowledgeVerificationProvenance `json:"provenance"`
}

type BookKnowledgeVerificationCheck struct {
	CheckID string  `json:"check_id"`
	Status  string  `json:"status"`
	Score   float64 `json:"score"`
	Message string  `json:"message"`
}

type BookKnowledgeVerificationProvenance struct {
	BookID     string   `json:"book_id"`
	ChapterID  string   `json:"chapter_id,omitempty"`
	ClaimID    string   `json:"claim_id"`
	Citations  []string `json:"citations,omitempty"`
	SourceHash string   `json:"source_hash"`
}

func (s *BookKnowledgeStore) BuildProjectVerificationReport(projectID string, limit int) (*BookKnowledgeProjectVerificationReport, error) {
	project, ok := BookKnowledgeProjectByID(projectID)
	if !ok {
		return nil, fmt.Errorf("unknown book knowledge project: %s", projectID)
	}
	limit = normalizeProjectLimit(limit)
	reviewItems, total, err := s.projectReviewItems(project, 0)
	if err != nil {
		return nil, err
	}

	verifiedItems := make([]BookKnowledgeVerifiedItem, 0, len(reviewItems))
	tierCounts := map[string]int{}
	decisionCounts := map[string]int{}
	for _, item := range reviewItems {
		verified := verifyProjectReviewItem(project, item)
		verifiedItems = append(verifiedItems, verified)
		tierCounts[verified.RiskTier]++
		decisionCounts[verified.Decision]++
	}
	if limit > 0 && len(verifiedItems) > limit {
		verifiedItems = verifiedItems[:limit]
	}
	return &BookKnowledgeProjectVerificationReport{
		ProjectID:      project.ProjectID,
		Project:        project,
		AutonomyMode:   bookKnowledgeAutonomyMode,
		HumanLoop:      bookKnowledgeHumanLoop,
		ReviewSampling: bookKnowledgeReviewSample,
		Items:          verifiedItems,
		Total:          total,
		Limit:          limit,
		TierCounts:     tierCounts,
		DecisionCounts: decisionCounts,
		Policy:         projectVerificationPolicy(project.ProjectID),
	}, nil
}

func verifyProjectReviewItem(project BookKnowledgeProject, item BookKnowledgeReviewQueueItem) BookKnowledgeVerifiedItem {
	hasSummary := strings.TrimSpace(item.Summary) != ""
	hasCitation := len(item.Citations) > 0
	healthSensitive := project.ProjectID == BookKnowledgeProjectHealth && containsHealthSensitiveTerm(item.Title+" "+item.Summary)
	score := calculateVerificationScore(item, hasSummary, hasCitation)

	checks := []BookKnowledgeVerificationCheck{
		verificationCheck("provenance", boolCheckStatus(hasSummary), boolCheckScore(hasSummary), "claim has stable source identity"),
		verificationCheck("citation_presence", boolCheckStatus(hasCitation), boolCheckScore(hasCitation), "claim cites source material"),
		evidenceStrengthCheck(item.EvidenceLevel),
		verificationCheck("confidence", confidenceCheckStatus(item.Confidence), clamp01(item.Confidence), "extractor confidence signal"),
	}
	if project.ProjectID == BookKnowledgeProjectHealth {
		status := bookKnowledgeCheckPass
		message := "claim is suitable for health education"
		if healthSensitive {
			status = bookKnowledgeCheckWarn
			message = "health-sensitive claim cannot become direct medical guidance"
		}
		checks = append(checks, verificationCheck("health_safety", status, safetyScore(status), message))
	}

	riskTier, decision, reasons := decideVerificationOutcome(project.ProjectID, score, hasSummary, hasCitation, healthSensitive)
	return BookKnowledgeVerifiedItem{
		ProjectID:         project.ProjectID,
		BookID:            item.BookID,
		BookTitle:         item.BookTitle,
		ChapterID:         item.ChapterID,
		ChapterTitle:      item.ChapterTitle,
		ClaimID:           item.ClaimID,
		Title:             item.Title,
		Summary:           item.Summary,
		VerificationScore: score,
		RiskTier:          riskTier,
		Decision:          decision,
		Checks:            checks,
		FailureReasons:    reasons,
		AllowedUses:       projectAllowedUses(project.ProjectID, riskTier),
		BlockedUses:       projectBlockedUses(project.ProjectID, riskTier),
		RiskFlags:         projectRiskFlags(project.ProjectID),
		Provenance: BookKnowledgeVerificationProvenance{
			BookID:     item.BookID,
			ChapterID:  item.ChapterID,
			ClaimID:    item.ClaimID,
			Citations:  append([]string(nil), item.Citations...),
			SourceHash: sourceHashForReviewItem(item),
		},
	}
}

func calculateVerificationScore(item BookKnowledgeReviewQueueItem, hasSummary, hasCitation bool) float64 {
	score := 0.0
	if hasSummary {
		score += 0.20
	}
	if hasCitation {
		score += 0.30
	}
	score += evidenceScore(item.EvidenceLevel) * 0.25
	score += clamp01(item.Confidence) * 0.25
	return math.Round(score*100) / 100
}

func decideVerificationOutcome(projectID string, score float64, hasSummary, hasCitation, healthSensitive bool) (string, string, []string) {
	reasons := []string{}
	if !hasSummary {
		reasons = append(reasons, "missing_summary")
	}
	if !hasCitation {
		reasons = append(reasons, "missing_citation")
	}
	if len(reasons) > 0 {
		return bookKnowledgeRiskNeedsHuman, bookKnowledgeDecisionQueue, reasons
	}
	if projectID == BookKnowledgeProjectHealth && healthSensitive {
		return bookKnowledgeRiskAssistive, bookKnowledgeDecisionAssist, []string{"health_sensitive_claim"}
	}
	if score >= 0.75 {
		return bookKnowledgeRiskAutoUsable, bookKnowledgeDecisionAllow, nil
	}
	if score >= 0.55 {
		return bookKnowledgeRiskAssistive, bookKnowledgeDecisionAssist, []string{"moderate_verification_score"}
	}
	return bookKnowledgeRiskNeedsHuman, bookKnowledgeDecisionQueue, []string{"low_verification_score"}
}

func projectVerificationPolicy(projectID string) BookKnowledgeProjectVerificationPolicy {
	switch projectID {
	case BookKnowledgeProjectHealth:
		return BookKnowledgeProjectVerificationPolicy{
			DefaultRiskTier: bookKnowledgeRiskNeedsHuman,
			AutoUseTiers:    []string{bookKnowledgeRiskAutoUsable},
			AssistiveTiers:  []string{bookKnowledgeRiskAssistive},
			BlockedUses:     []string{"diagnosis", "treatment_plan", "medication_instruction", "emergency_guidance"},
		}
	case BookKnowledgeProjectProofroom:
		return BookKnowledgeProjectVerificationPolicy{
			DefaultRiskTier: bookKnowledgeRiskAssistive,
			AutoUseTiers:    []string{bookKnowledgeRiskAutoUsable},
			AssistiveTiers:  []string{bookKnowledgeRiskAssistive},
			BlockedUses:     []string{"final_publication_without_review"},
		}
	default:
		return BookKnowledgeProjectVerificationPolicy{
			DefaultRiskTier: bookKnowledgeRiskNeedsHuman,
			AssistiveTiers:  []string{bookKnowledgeRiskAssistive},
			BlockedUses:     []string{"external_action"},
		}
	}
}

func projectAllowedUses(projectID, riskTier string) []string {
	switch riskTier {
	case bookKnowledgeRiskAutoUsable:
		if projectID == BookKnowledgeProjectHealth {
			return []string{"health_education", "question_preparation", "context_retrieval"}
		}
		return []string{"argument_draft", "source_pack", "counterclaim_scan"}
	case bookKnowledgeRiskAssistive:
		if projectID == BookKnowledgeProjectHealth {
			return []string{"context_retrieval", "draft_summary"}
		}
		return []string{"source_pack", "argument_draft"}
	case bookKnowledgeRiskNeedsHuman:
		return []string{"review_queue"}
	default:
		return nil
	}
}

func projectBlockedUses(projectID, riskTier string) []string {
	if riskTier == bookKnowledgeRiskBlocked {
		return []string{"all_downstream_use"}
	}
	switch projectID {
	case BookKnowledgeProjectHealth:
		return []string{"diagnosis", "treatment_plan", "medication_instruction", "emergency_guidance"}
	case BookKnowledgeProjectProofroom:
		return []string{"final_publication_without_review"}
	default:
		return []string{"external_action"}
	}
}

func containsHealthSensitiveTerm(text string) bool {
	normalized := strings.ToLower(text)
	for _, term := range []string{
		"诊断", "治疗", "用药", "剂量", "药物", "处方", "急症", "急救", "手术", "检查结果",
		"血压", "血糖", "医生", "医院", "diagnosis", "treatment", "medicine", "medication", "dose", "emergency",
	} {
		if strings.Contains(normalized, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func evidenceStrengthCheck(level string) BookKnowledgeVerificationCheck {
	score := evidenceScore(level)
	status := bookKnowledgeCheckWarn
	if score >= 0.8 {
		status = bookKnowledgeCheckPass
	} else if score < 0.4 {
		status = bookKnowledgeCheckFail
	}
	return verificationCheck("evidence_strength", status, score, "evidence level "+firstNonEmpty(strings.TrimSpace(level), "unknown"))
}

func evidenceScore(level string) float64 {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "A":
		return 1.0
	case "B":
		return 0.82
	case "C":
		return 0.55
	case "D":
		return 0.25
	default:
		return 0.35
	}
}

func verificationCheck(checkID, status string, score float64, message string) BookKnowledgeVerificationCheck {
	return BookKnowledgeVerificationCheck{
		CheckID: checkID,
		Status:  status,
		Score:   math.Round(clamp01(score)*100) / 100,
		Message: message,
	}
}

func boolCheckStatus(ok bool) string {
	if ok {
		return bookKnowledgeCheckPass
	}
	return bookKnowledgeCheckFail
}

func boolCheckScore(ok bool) float64 {
	if ok {
		return 1
	}
	return 0
}

func confidenceCheckStatus(confidence float64) string {
	if confidence >= 0.7 {
		return bookKnowledgeCheckPass
	}
	if confidence >= 0.4 {
		return bookKnowledgeCheckWarn
	}
	return bookKnowledgeCheckFail
}

func safetyScore(status string) float64 {
	if status == bookKnowledgeCheckPass {
		return 1
	}
	if status == bookKnowledgeCheckWarn {
		return 0.55
	}
	return 0
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func sourceHashForReviewItem(item BookKnowledgeReviewQueueItem) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(item.BookID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(item.ChapterID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(item.ClaimID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(item.Summary))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(strings.Join(item.Citations, "|")))
	return hex.EncodeToString(hasher.Sum(nil))
}
