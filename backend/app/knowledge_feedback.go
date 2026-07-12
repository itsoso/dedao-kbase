package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	KnowledgeFeedbackUsed     = "used"
	KnowledgeFeedbackRejected = "rejected"
	KnowledgeFeedbackStale    = "stale"
	KnowledgeFeedbackConflict = "conflict"
	KnowledgeFeedbackZeroHit  = "zero_hit"

	KnowledgeFeedbackReasonUsedForAnswer       = "used_for_answer"
	KnowledgeFeedbackReasonOutOfScope          = "out_of_scope"
	KnowledgeFeedbackReasonStaleSource         = "stale_source"
	KnowledgeFeedbackReasonConflictingEvidence = "conflicting_evidence"
	KnowledgeFeedbackReasonNoRelevantClaim     = "no_relevant_claim"
	KnowledgeFeedbackReasonPolicyBlocked       = "policy_blocked"
)

type KnowledgeFeedbackInput struct {
	EventID    string   `json:"event_id"`
	Consumer   string   `json:"consumer"`
	Outcome    string   `json:"outcome"`
	ClaimIDs   []string `json:"claim_ids,omitempty"`
	ReasonCode string   `json:"reason_code,omitempty"`
}

type KnowledgeFeedback struct {
	FeedbackID string   `json:"feedback_id"`
	ReleaseID  string   `json:"release_id"`
	EventID    string   `json:"event_id"`
	Consumer   string   `json:"consumer"`
	Outcome    string   `json:"outcome"`
	ClaimIDs   []string `json:"claim_ids,omitempty"`
	ReasonCode string   `json:"reason_code,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

func (s *BookKnowledgeStore) KnowledgeFeedbackPath(releaseID string) string {
	return filepath.Join(s.KnowledgeReleaseDir(), "feedback", sanitizeBookKnowledgeID(releaseID)+".jsonl")
}

func (s *BookKnowledgeStore) SaveKnowledgeFeedback(releaseID string, input KnowledgeFeedbackInput) (*KnowledgeFeedback, map[string]int, error) {
	release, err := s.LoadKnowledgeRelease(releaseID)
	if err != nil {
		return nil, nil, err
	}
	input.EventID = strings.TrimSpace(input.EventID)
	input.Consumer = strings.TrimSpace(input.Consumer)
	input.Outcome = strings.ToLower(strings.TrimSpace(input.Outcome))
	input.ReasonCode = strings.ToLower(strings.TrimSpace(input.ReasonCode))
	if input.EventID == "" || input.Consumer == "" {
		return nil, nil, fmt.Errorf("event_id and consumer are required")
	}
	if len(input.EventID) > 200 || len(input.Consumer) > 100 {
		return nil, nil, fmt.Errorf("event_id or consumer is too long")
	}
	if !validKnowledgeFeedbackOutcome(input.Outcome) {
		return nil, nil, fmt.Errorf("invalid feedback outcome %q", input.Outcome)
	}
	if input.ReasonCode != "" && !validKnowledgeFeedbackReason(input.ReasonCode) {
		return nil, nil, fmt.Errorf("invalid feedback reason_code %q", input.ReasonCode)
	}
	validClaims := make(map[string]struct{})
	for _, claim := range release.Analysis.Claims {
		validClaims[claim.ID] = struct{}{}
	}
	for _, claimID := range input.ClaimIDs {
		if _, ok := validClaims[claimID]; !ok {
			return nil, nil, fmt.Errorf("unknown claim_id %q", claimID)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := readJSONLFile[KnowledgeFeedback](s.KnowledgeFeedbackPath(release.ReleaseID))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	feedbackID := knowledgeFeedbackID(release.ReleaseID, input.Consumer, input.EventID)
	for index := range items {
		if items[index].FeedbackID == feedbackID {
			if items[index].Outcome != input.Outcome || items[index].ReasonCode != input.ReasonCode || !equalFeedbackClaimIDs(items[index].ClaimIDs, input.ClaimIDs) {
				return nil, nil, fmt.Errorf("feedback idempotency payload conflict")
			}
			counts := knowledgeFeedbackCounts(items)
			return &items[index], counts, nil
		}
	}
	feedback := KnowledgeFeedback{
		FeedbackID: feedbackID,
		ReleaseID:  release.ReleaseID,
		EventID:    input.EventID,
		Consumer:   input.Consumer,
		Outcome:    input.Outcome,
		ClaimIDs:   append([]string(nil), input.ClaimIDs...),
		ReasonCode: input.ReasonCode,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	items = append(items, feedback)
	payload, err := encodeJSONLFile(items)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(s.KnowledgeFeedbackPath(release.ReleaseID)), os.ModePerm); err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomically(s.KnowledgeFeedbackPath(release.ReleaseID), payload); err != nil {
		return nil, nil, err
	}
	return &feedback, knowledgeFeedbackCounts(items), nil
}

func validKnowledgeFeedbackReason(reason string) bool {
	switch reason {
	case KnowledgeFeedbackReasonUsedForAnswer, KnowledgeFeedbackReasonOutOfScope, KnowledgeFeedbackReasonStaleSource,
		KnowledgeFeedbackReasonConflictingEvidence, KnowledgeFeedbackReasonNoRelevantClaim, KnowledgeFeedbackReasonPolicyBlocked:
		return true
	default:
		return false
	}
}

func equalFeedbackClaimIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *BookKnowledgeStore) ListKnowledgeFeedback(releaseID string) ([]KnowledgeFeedback, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := readJSONLFile[KnowledgeFeedback](s.KnowledgeFeedbackPath(releaseID))
	if os.IsNotExist(err) {
		return []KnowledgeFeedback{}, nil
	}
	return items, err
}

func knowledgeFeedbackID(releaseID, consumer, eventID string) string {
	sum := sha256.Sum256([]byte(releaseID + "\x00" + consumer + "\x00" + eventID))
	return "feedback-" + hex.EncodeToString(sum[:])
}

func validKnowledgeFeedbackOutcome(outcome string) bool {
	switch outcome {
	case KnowledgeFeedbackUsed, KnowledgeFeedbackRejected, KnowledgeFeedbackStale, KnowledgeFeedbackConflict, KnowledgeFeedbackZeroHit:
		return true
	default:
		return false
	}
}

func knowledgeFeedbackCounts(items []KnowledgeFeedback) map[string]int {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Outcome]++
	}
	return counts
}
