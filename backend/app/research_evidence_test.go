package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestResearchEvidenceNormalizationIsDeterministicAndDeduplicates(t *testing.T) {
	candidate := researchEvidenceTestCandidate("Selected evidence")
	result := ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog},
		Items:           []ResearchWorkerEvidenceCandidate{candidate, candidate},
	}

	first, err := NormalizeResearchWorkerResult(result)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizeResearchWorkerResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Evidence) != 1 || len(second.Evidence) != 1 {
		t.Fatalf("first=%#v second=%#v", first.Evidence, second.Evidence)
	}
	if first.Evidence[0].EvidenceID != second.Evidence[0].EvidenceID ||
		first.Evidence[0].LocatorHash != second.Evidence[0].LocatorHash ||
		first.Evidence[0].ContentHash != second.Evidence[0].ContentHash {
		t.Fatalf("evidence identifiers are not deterministic: first=%#v second=%#v", first.Evidence[0], second.Evidence[0])
	}
	if len(first.SearchedSources) != 1 || first.SearchedSources[0] != ResearchSourceChatlog ||
		len(first.CitedSources) != 1 || first.CitedSources[0] != ResearchSourceChatlog {
		t.Fatalf("scope=%#v", first)
	}
}

func TestResearchEvidenceRejectsInvalidRolesDerivedWorkerEvidenceAndSourceChanges(t *testing.T) {
	invalidRole := researchEvidenceTestCandidate("advice")
	invalidRole.SourceType = ResearchEvidenceSourceKnowledge
	if _, err := NormalizeResearchWorkerResult(ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{invalidRole}}); !errors.Is(err, ErrResearchEvidenceSourceRole) {
		t.Fatalf("invalid source-role error=%v", err)
	}

	derived := researchEvidenceTestCandidate("derived")
	derived.SourceType = ResearchEvidenceSourceDerived
	derived.SourceRole = ResearchEvidenceRoleDerivedAnalysis
	derived.Privacy = ResearchEvidencePrivacyPrivate
	if _, err := NormalizeResearchWorkerResult(ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{derived}}); !errors.Is(err, ErrResearchEvidenceDerivedForbidden) {
		t.Fatalf("derived worker evidence error=%v", err)
	}

	changed := researchEvidenceTestCandidate("first version")
	changedAgain := changed
	changedAgain.Content = "second version"
	if _, err := NormalizeResearchWorkerResult(ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{changed, changedAgain}}); !errors.Is(err, ErrResearchEvidenceSourceChanged) {
		t.Fatalf("source-change error=%v", err)
	}
}

func TestResearchEvidencePromotesOnlySelectedBoundedExcerptsAndDropsRawFields(t *testing.T) {
	const (
		rawSentinel      = "RAW_RESPONSE_SHOULD_NOT_ESCAPE"
		contactSentinel  = "FULL_CONTACT_SHOULD_NOT_ESCAPE"
		cookieSentinel   = "COOKIE_SHOULD_NOT_ESCAPE"
		bearerSentinel   = "Bearer TOKEN_SHOULD_NOT_ESCAPE"
		pathSentinel     = "/local/private/chat.db"
		unselectedSecret = "UNSELECTED_LONG_MESSAGE_SHOULD_NOT_ESCAPE"
	)
	selected := researchEvidenceTestCandidate(strings.Repeat("证", researchEvidenceExcerptMaxRunes+50))
	unselected := researchEvidenceTestCandidate(unselectedSecret + strings.Repeat("x", researchEvidenceExcerptMaxRunes+50))
	unselected.Selected = false
	unselected.Locator.MessageRef = "opaque-message-unselected"

	bundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog},
		Items:           []ResearchWorkerEvidenceCandidate{selected, unselected},
		RawResponseBody: rawSentinel,
		ContactObject:   map[string]any{"display_name": contactSentinel},
		Cookie:          cookieSentinel,
		Authorization:   bearerSentinel,
		LocalPath:       pathSentinel,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Evidence) != 1 || len([]rune(bundle.Evidence[0].ContentExcerpt)) != researchEvidenceExcerptMaxRunes {
		t.Fatalf("evidence=%#v", bundle.Evidence)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{rawSentinel, contactSentinel, cookieSentinel, bearerSentinel, pathSentinel, unselectedSecret} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public projection leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestResearchEvidenceStorePersistsOnlyNormalizedEvidenceAndScope(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "evidence-store")
	selected := researchEvidenceTestCandidate("Minimal retained excerpt")
	unselected := researchEvidenceTestCandidate("DATABASE_SECRET_SHOULD_NOT_ESCAPE")
	unselected.Selected = false
	unselected.Locator.MessageRef = "opaque-message-unselected"
	bundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog, ResearchSourceKnowledge},
		Items:           []ResearchWorkerEvidenceCandidate{selected, unselected},
		RawResponseBody: "RAW_DATABASE_SENTINEL",
		Cookie:          "COOKIE_DATABASE_SENTINEL",
		Authorization:   "Bearer DATABASE_SENTINEL",
		LocalPath:       "/private/database/path",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.StoreEvidenceBundle(run.RunID, run.Version, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != run.Version+1 || len(updated.ActualScope.SearchedSources) != 2 ||
		len(updated.ActualScope.CitedSources) != 1 || updated.ActualScope.CitedSources[0] != ResearchSourceChatlog {
		t.Fatalf("updated=%#v", updated)
	}
	stored, err := store.ListEvidence(run.RunID)
	if err != nil || len(stored) != 1 || stored[0].ContentExcerpt != selected.Content {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}

	rows, err := store.db.Query(`SELECT content_excerpt, locator_json FROM research_evidence WHERE run_id = ?`, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var persisted strings.Builder
	for rows.Next() {
		var excerpt, locator string
		if err := rows.Scan(&excerpt, &locator); err != nil {
			t.Fatal(err)
		}
		persisted.WriteString(excerpt)
		persisted.WriteString(locator)
	}
	for _, forbidden := range []string{
		"DATABASE_SECRET_SHOULD_NOT_ESCAPE", "RAW_DATABASE_SENTINEL", "COOKIE_DATABASE_SENTINEL",
		"Bearer DATABASE_SENTINEL", "/private/database/path",
	} {
		if strings.Contains(persisted.String(), forbidden) {
			t.Fatalf("database leaked %q: %s", forbidden, persisted.String())
		}
	}
}

func TestResearchEvidenceStoreEnforcesRunItemAndQuotedCharacterBudgetsAtomically(t *testing.T) {
	store := newResearchStoreForTest(t)
	input := researchStoreTestInput("evidence-budget")
	input.Budget.MaxEvidenceItems = 1
	input.Budget.MaxQuotedChars = 10
	run, _, err := store.CreateRun(input)
	if err != nil {
		t.Fatal(err)
	}
	first := researchEvidenceTestCandidate("123456")
	firstBundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog}, Items: []ResearchWorkerEvidenceCandidate{first},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.StoreEvidenceBundle(run.RunID, run.Version, firstBundle)
	if err != nil {
		t.Fatal(err)
	}
	second := researchEvidenceTestCandidate("abcdef")
	second.Locator.MessageRef = "sha256:6b01731f9d22d0e8243e4f3f5170b8710d35a48a49bf1090962a7a37efa94452"
	secondBundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog}, Items: []ResearchWorkerEvidenceCandidate{second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StoreEvidenceBundle(run.RunID, updated.Version, secondBundle); !errors.Is(err, ErrResearchBudgetExhausted) {
		t.Fatalf("item budget error=%v", err)
	}
	stored, err := store.ListEvidence(run.RunID)
	if err != nil || len(stored) != 1 || stored[0].ContentExcerpt != first.Content {
		t.Fatalf("stored after item rejection=%#v err=%v", stored, err)
	}

	input = researchStoreTestInput("quoted-budget")
	input.Budget.MaxEvidenceItems = 5
	input.Budget.MaxQuotedChars = 10
	quotedRun, _, err := store.CreateRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StoreEvidenceBundle(quotedRun.RunID, quotedRun.Version, ResearchEvidenceBundle{
		Evidence:        append(append([]ResearchEvidence{}, firstBundle.Evidence...), secondBundle.Evidence...),
		SearchedSources: []string{ResearchSourceChatlog}, CitedSources: []string{ResearchSourceChatlog},
	}); !errors.Is(err, ErrResearchBudgetExhausted) {
		t.Fatalf("quoted-character budget error=%v", err)
	}
	stored, err = store.ListEvidence(quotedRun.RunID)
	if err != nil || len(stored) != 0 {
		t.Fatalf("quoted budget partially persisted=%#v err=%v", stored, err)
	}
}

func TestResearchEvidenceStoreDetectsChangedSourceAcrossBundles(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "evidence-change")
	firstCandidate := researchEvidenceTestCandidate("original content")
	first, err := NormalizeResearchWorkerResult(ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{firstCandidate}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.StoreEvidenceBundle(run.RunID, run.Version, first)
	if err != nil {
		t.Fatal(err)
	}
	changedCandidate := firstCandidate
	changedCandidate.Content = "changed content"
	changed, err := NormalizeResearchWorkerResult(ResearchWorkerResult{Items: []ResearchWorkerEvidenceCandidate{changedCandidate}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StoreEvidenceBundle(run.RunID, updated.Version, changed); !errors.Is(err, ErrResearchEvidenceSourceChanged) {
		t.Fatalf("source-change error=%v", err)
	}
	loaded, err := store.LoadRun(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != updated.Version {
		t.Fatalf("failed evidence transaction changed run version: got=%d want=%d", loaded.Version, updated.Version)
	}
}

func TestResearchEvidenceStoreAllowsSameEvidenceInSeparateRuns(t *testing.T) {
	store := newResearchStoreForTest(t)
	firstRun := createResearchRunForTest(t, store, "shared-evidence-first")
	secondRun := createResearchRunForTest(t, store, "shared-evidence-second")
	bundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog},
		Items:           []ResearchWorkerEvidenceCandidate{researchEvidenceTestCandidate("shared source content")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StoreEvidenceBundle(firstRun.RunID, firstRun.Version, bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StoreEvidenceBundle(secondRun.RunID, secondRun.Version, bundle); err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{firstRun.RunID, secondRun.RunID} {
		items, err := store.ListEvidence(runID)
		if err != nil || len(items) != 1 {
			t.Fatalf("run=%s evidence=%#v err=%v", runID, items, err)
		}
	}
}

func TestResearchEvidenceStoreRejectsUnsupportedCitedScope(t *testing.T) {
	store := newResearchStoreForTest(t)
	run := createResearchRunForTest(t, store, "unsupported-citation")
	bundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
		Items: []ResearchWorkerEvidenceCandidate{researchEvidenceTestCandidate("chat evidence")},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle.CitedSources = append(bundle.CitedSources, ResearchSourceKnowledge)
	if _, err := store.StoreEvidenceBundle(run.RunID, run.Version, bundle); err == nil {
		t.Fatal("store accepted a cited source with no selected evidence")
	}
}

func researchEvidenceTestCandidate(content string) ResearchWorkerEvidenceCandidate {
	return ResearchWorkerEvidenceCandidate{
		SourceType:         ResearchEvidenceSourceChatlog,
		SourceRole:         ResearchEvidenceRoleDirectAdvice,
		AuthorIdentityID:   "identity-author",
		SubjectIdentityIDs: []string{"identity-subject"},
		OccurredAt:         "2026-08-13T07:43:06+08:00",
		Content:            content,
		Locator: ResearchEvidenceLocator{
			WorkerID:        "worker-fixture",
			ConversationRef: "sha256:5a01731f9d22d0e8243e4f3f5170b8710d35a48a49bf1090962a7a37efa94451",
			MessageRef:      "sha256:5a01731f9d22d0e8243e4f3f5170b8710d35a48a49bf1090962a7a37efa94451",
		},
		Privacy:  ResearchEvidencePrivacyPrivate,
		Selected: true,
	}
}
