package app

import (
	"strings"
	"testing"
)

func TestKnowledgeFeedbackIsValidatedAndIdempotent(t *testing.T) {
	store, release := feedbackTestStore(t)
	input := KnowledgeFeedbackInput{
		EventID: "event-1", Consumer: "health-assistant", Outcome: KnowledgeFeedbackUsed,
		ClaimIDs: []string{"claim-1"}, ReasonCode: KnowledgeFeedbackReasonUsedForAnswer,
	}
	first, counts, err := store.SaveKnowledgeFeedback(release.ReleaseID, input)
	if err != nil {
		t.Fatalf("SaveKnowledgeFeedback returned error: %v", err)
	}
	second, secondCounts, err := store.SaveKnowledgeFeedback(release.ReleaseID, input)
	if err != nil {
		t.Fatalf("replayed feedback returned error: %v", err)
	}
	if first.FeedbackID == "" || first.FeedbackID != second.FeedbackID || first.CreatedAt != second.CreatedAt {
		t.Fatalf("feedback = first %#v second %#v", first, second)
	}
	if counts[KnowledgeFeedbackUsed] != 1 || secondCounts[KnowledgeFeedbackUsed] != 1 {
		t.Fatalf("counts = %#v / %#v", counts, secondCounts)
	}
	items, err := store.ListKnowledgeFeedback(release.ReleaseID)
	if err != nil || len(items) != 1 {
		t.Fatalf("feedback items = %#v, err=%v", items, err)
	}
}

func TestKnowledgeFeedbackRejectsIdempotencyPayloadMismatch(t *testing.T) {
	store, release := feedbackTestStore(t)
	input := KnowledgeFeedbackInput{EventID: "event-1", Consumer: "health-assistant", Outcome: KnowledgeFeedbackUsed, ClaimIDs: []string{"claim-1"}}
	if _, _, err := store.SaveKnowledgeFeedback(release.ReleaseID, input); err != nil {
		t.Fatal(err)
	}
	input.Outcome = KnowledgeFeedbackRejected
	_, _, err := store.SaveKnowledgeFeedback(release.ReleaseID, input)
	if err == nil || !strings.Contains(err.Error(), "idempotency") {
		t.Fatalf("mismatched replay error = %v", err)
	}
}

func TestKnowledgeFeedbackRejectsUnknownReasonCode(t *testing.T) {
	store, release := feedbackTestStore(t)
	_, _, err := store.SaveKnowledgeFeedback(release.ReleaseID, KnowledgeFeedbackInput{
		EventID: "event-reason", Consumer: "consumer", Outcome: KnowledgeFeedbackRejected, ReasonCode: "free-form private detail",
	})
	if err == nil || !strings.Contains(err.Error(), "reason_code") {
		t.Fatalf("reason code error = %v", err)
	}
}

func TestKnowledgeFeedbackRejectsInvalidOutcomeAndClaim(t *testing.T) {
	store, release := feedbackTestStore(t)
	_, _, err := store.SaveKnowledgeFeedback(release.ReleaseID, KnowledgeFeedbackInput{
		EventID: "event-bad-outcome", Consumer: "consumer", Outcome: "liked",
	})
	if err == nil || !strings.Contains(err.Error(), "outcome") {
		t.Fatalf("invalid outcome error = %v", err)
	}
	_, _, err = store.SaveKnowledgeFeedback(release.ReleaseID, KnowledgeFeedbackInput{
		EventID: "event-bad-claim", Consumer: "consumer", Outcome: KnowledgeFeedbackConflict,
		ClaimIDs: []string{"unknown-claim"},
	})
	if err == nil || !strings.Contains(err.Error(), "claim_id") {
		t.Fatalf("invalid claim error = %v", err)
	}
}

func feedbackTestStore(t *testing.T) (*BookKnowledgeStore, *KnowledgeRelease) {
	t.Helper()
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	release, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	return store, release
}
