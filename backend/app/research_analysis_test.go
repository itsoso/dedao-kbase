package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type researchAnalysisFixture struct {
	Identities     []ResearchIdentityCandidate `json:"identities"`
	Measurements   []float64                   `json:"measurements"`
	Claims         []ResearchClaim             `json:"claims"`
	HistoricalCase ResearchCase                `json:"historical_case"`
	CurrentCase    ResearchCase                `json:"current_case"`
}

func TestResearchIdentityExactBindingWinsAndNameSimilarityAloneRemainsAmbiguous(t *testing.T) {
	fixture := loadResearchAnalysisFixture(t)
	decision := ResolveResearchIdentity(fixture.Identities)
	if decision.Status != ResearchIdentityResolved || decision.IdentityID != "person-alpha" || decision.Confidence != 1 {
		t.Fatalf("exact binding decision = %#v", decision)
	}

	for index := range fixture.Identities {
		fixture.Identities[index].AccountID = ""
		fixture.Identities[index].TargetAccountID = ""
		fixture.Identities[index].ConfirmedBinding = false
		fixture.Identities[index].DisplayNameMatch = true
	}
	ambiguous := ResolveResearchIdentity(fixture.Identities)
	if ambiguous.Status != ResearchIdentityAmbiguous || len(ambiguous.CandidateIDs) != 2 || ambiguous.IdentityID != "" {
		t.Fatalf("name-only decision = %#v", ambiguous)
	}
}

func TestResearchTimelineReportsMissingRecoveryAndKeepsGroundedChronology(t *testing.T) {
	evidence := []ResearchEvidence{
		{EvidenceID: "evidence-later", OccurredAt: "2032-01-03T09:00:00Z", ContentExcerpt: "Later observation"},
		{EvidenceID: "evidence-earlier", OccurredAt: "2032-01-01T09:00:00Z", ContentExcerpt: "Earlier observation"},
	}
	facts := []ResearchFact{
		{FactID: "fact-later", Kind: "observation", Summary: "Later fact", OccurredAt: "2032-01-03T09:00:00Z", EvidenceIDs: []string{"evidence-later"}, Confidence: 0.9, ReviewState: ResearchReviewVerified},
		{FactID: "fact-recovery", Kind: ResearchFactRecovery, Summary: "Recovery date", Status: ResearchAnalysisNotFound, EvidenceIDs: []string{"evidence-earlier"}, Confidence: 1, ReviewState: ResearchReviewVerified},
		{FactID: "fact-unsupported", Kind: "observation", Summary: "Unsupported", EvidenceIDs: []string{"fact-unsupported"}, Confidence: 0.7, ReviewState: ResearchReviewPending},
	}
	events := BuildResearchTimeline(evidence, facts)
	if len(events) != 2 || events[0].Kind != ResearchFactRecovery || events[0].Status != ResearchAnalysisNotFound ||
		events[1].FactID != "fact-later" {
		t.Fatalf("timeline = %#v", events)
	}
	for _, event := range events {
		if event.FactID == "fact-unsupported" {
			t.Fatalf("derived record became its own evidence: %#v", event)
		}
	}
}

func TestResearchNumericTrendDoesNotCallMixedSeriesMonotonic(t *testing.T) {
	fixture := loadResearchAnalysisFixture(t)
	trend := ClassifyResearchNumericTrend(fixture.Measurements)
	if trend.Direction != ResearchTrendMixed || trend.NetDirection != ResearchTrendDown {
		t.Fatalf("trend=%#v", trend)
	}
	if trend.Delta != -6 || trend.Increases != 1 || trend.Decreases != 1 {
		t.Fatalf("trend details=%#v", trend)
	}
}

func TestResearchConflictSeparatesDirectAdviceFromGeneralDiscussion(t *testing.T) {
	fixture := loadResearchAnalysisFixture(t)
	conflicts := DetectResearchConflicts(fixture.Claims)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %#v", conflicts)
	}
	conflict := conflicts[0]
	if strings.Join(conflict.ClaimIDs, ",") != "advice-conflict,advice-direct" ||
		strings.Join(conflict.Dimensions, ",") != "amount,timing" {
		t.Fatalf("conflict = %#v", conflict)
	}
	for _, claimID := range conflict.ClaimIDs {
		if claimID == "discussion-general" {
			t.Fatalf("general discussion was inferred as direct advice: %#v", conflict)
		}
	}
}

func TestResearchCaseComparisonAlwaysListsMaterialDifferences(t *testing.T) {
	fixture := loadResearchAnalysisFixture(t)
	comparison := CompareResearchCases(fixture.HistoricalCase, fixture.CurrentCase)
	if comparison.Transferability != ResearchCaseTransferLimited {
		t.Fatalf("comparison = %#v", comparison)
	}
	dimensions := map[string]bool{}
	for _, difference := range comparison.MaterialDifferences {
		dimensions[difference.Dimension] = true
	}
	for _, dimension := range []string{"age", "stage_day", "symptoms", "recovery_status"} {
		if !dimensions[dimension] {
			t.Fatalf("missing %s difference: %#v", dimension, comparison.MaterialDifferences)
		}
	}
}

func TestResearchAnalysisStorePersistsSupportConfidenceAndReviewState(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "analysis-records")
	bundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog},
		Items: []ResearchWorkerEvidenceCandidate{{
			SourceType: ResearchEvidenceSourceChatlog, SourceRole: ResearchEvidenceRoleDirectObservation,
			Content: "Synthetic grounded observation", Privacy: ResearchEvidencePrivacyPrivate, Selected: true,
			Locator: ResearchEvidenceLocator{WorkerID: "worker-a", ConversationRef: "room-a", MessageRef: "message-a"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runPointer, err := store.StoreEvidenceBundle(run.RunID, run.Version, bundle)
	if err != nil {
		t.Fatal(err)
	}
	run = *runPointer
	records := []ResearchAnalysisRecord{
		{RecordID: "fact-a", Kind: ResearchAnalysisFact, Summary: "Grounded fact", SupportEvidenceIDs: []string{bundle.Evidence[0].EvidenceID}, Confidence: 0.9, ReviewState: ResearchReviewVerified},
		{RecordID: "measurement-a", Kind: ResearchAnalysisMeasurement, Summary: "24", SupportEvidenceIDs: []string{bundle.Evidence[0].EvidenceID}, Confidence: 0.8, ReviewState: ResearchReviewPending},
		{RecordID: "case-difference-a", Kind: ResearchAnalysisCaseDifference, Summary: "Different stage", SupportEvidenceIDs: []string{bundle.Evidence[0].EvidenceID}, Confidence: 1, ReviewState: ResearchReviewVerified},
	}
	if err := store.StoreResearchAnalysisRecords(run.RunID, records); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.ListResearchAnalysisRecords(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != len(records) || loaded[0].SupportEvidenceIDs[0] != bundle.Evidence[0].EvidenceID ||
		loaded[0].ReviewState == "" || loaded[0].Confidence == 0 {
		t.Fatalf("analysis records = %#v", loaded)
	}
	if err := store.StoreResearchAnalysisRecords(run.RunID, []ResearchAnalysisRecord{{
		RecordID: "derived-only", Kind: ResearchAnalysisFact, Summary: "Circular",
		SupportEvidenceIDs: []string{"derived-only"}, Confidence: 0.7, ReviewState: ResearchReviewPending,
	}}); err == nil || !strings.Contains(err.Error(), "accessible source evidence") {
		t.Fatalf("circular support error = %v", err)
	}
}

func loadResearchAnalysisFixture(t *testing.T) researchAnalysisFixture {
	t.Helper()
	payload, err := os.ReadFile("testdata/research-analysis-v1.synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture researchAnalysisFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}
