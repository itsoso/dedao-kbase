package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeKnowledgeAssemblyClaimAndPolarity(t *testing.T) {
	if got := normalizeKnowledgeAssemblyClaim("  Evidence，Supports  TRIAL! "); got != "evidence supports trial" {
		t.Fatalf("normalized claim = %q", got)
	}
	positiveBase, positive := splitKnowledgeAssemblyClaimPolarity("治疗能降低风险")
	negativeBase, negative := splitKnowledgeAssemblyClaimPolarity("治疗不能降低风险")
	if positive != KnowledgeAssemblyPolarityPositive ||
		negative != KnowledgeAssemblyPolarityNegative ||
		positiveBase != negativeBase {
		t.Fatalf(
			"polarity = positive(%q,%q) negative(%q,%q)",
			positiveBase, positive, negativeBase, negative,
		)
	}
}

func TestBuildKnowledgeReleaseAssemblyUsesLatestReleasePerBookAndIsDeterministic(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a-old", "book-a", "2026-07-25T10:00:00Z",
		"旧结论", "Publisher A", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a-new", "book-a", "2026-07-26T10:00:00Z",
		"干预能改善结局", "Publisher A", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"干预能改善结局", "Publisher B", "dedao_course_article",
	))

	first, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if first.AssemblyID == "" || first.AssemblyID != second.AssemblyID ||
		!reflect.DeepEqual(first, second) {
		t.Fatalf("assembly is not deterministic: first=%#v second=%#v", first, second)
	}
	if !reflect.DeepEqual(first.ReleaseIDs, []string{"release-a-new", "release-b"}) {
		t.Fatalf("release snapshot = %#v", first.ReleaseIDs)
	}
	if first.Summary.ReleaseCount != 2 || first.Summary.ClaimCount != 2 ||
		first.Summary.ClusterCount != 1 || first.Summary.CorroboratedClusters != 1 {
		t.Fatalf("summary = %#v", first.Summary)
	}
	if len(first.Clusters) != 1 ||
		first.Clusters[0].Status != KnowledgeAssemblyStatusCorroborated ||
		first.Clusters[0].IndependentPublicationCount != 2 {
		t.Fatalf("clusters = %#v", first.Clusters)
	}
	for _, ref := range first.Clusters[0].Claims {
		if ref.ReleaseID == "release-a-old" {
			t.Fatalf("superseded release was assembled: %#v", first.Clusters[0])
		}
	}
}

func TestLatestKnowledgeAssemblyReleaseRecordsComparesAbsoluteTimes(t *testing.T) {
	records, err := latestKnowledgeAssemblyReleaseRecords([]KnowledgeReleaseRecord{
		{
			ReleaseID: "release-earlier",
			BookID:    "book-a",
			CreatedAt: "2026-07-26T12:00:00+08:00",
		},
		{
			ReleaseID: "release-later",
			BookID:    "book-a",
			CreatedAt: "2026-07-26T05:00:00Z",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ReleaseID != "release-later" {
		t.Fatalf("latest records = %#v", records)
	}
}

func TestBuildKnowledgeReleaseAssemblyFlagsExplicitPolarityConflict(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a", "book-a", "2026-07-26T10:00:00Z",
		"治疗能降低风险", "Publisher A", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"治疗不能降低风险", "Publisher B", "dedao_course_article",
	))

	assembly, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Clusters) != 1 ||
		assembly.Clusters[0].Status != KnowledgeAssemblyStatusPotentialConflict ||
		len(assembly.Clusters[0].PotentialConflicts) != 1 ||
		assembly.Summary.PotentialConflictClusters != 1 {
		t.Fatalf("assembly = %#v", assembly)
	}
	conflict := assembly.Clusters[0].PotentialConflicts[0]
	if conflict.PositiveClaimID == "" || conflict.NegativeClaimID == "" ||
		!conflict.ReviewRequired {
		t.Fatalf("conflict = %#v", conflict)
	}
}

func TestBuildKnowledgeReleaseAssemblyCountsPublisherAcrossTransportsOnce(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a", "book-a", "2026-07-26T10:00:00Z",
		"同一结论", "Same Publisher", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"同一结论", " same publisher ", "dedao_course_article",
	))

	assembly, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Clusters) != 1 ||
		assembly.Clusters[0].PublicationCount != 1 ||
		assembly.Clusters[0].IndependentPublicationCount != 1 ||
		assembly.Clusters[0].Status != KnowledgeAssemblyStatusSinglePublication {
		t.Fatalf("cluster = %#v", assembly.Clusters)
	}
}

func TestBuildKnowledgeReleaseAssemblyDoesNotMergeBroadParaphrases(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a", "book-a", "2026-07-26T10:00:00Z",
		"治疗降低风险", "Publisher A", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"风险因治疗而下降", "Publisher B", "dedao_course_article",
	))

	assembly, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if assembly.Summary.ClusterCount != 2 || len(assembly.Clusters) != 2 {
		t.Fatalf("broad paraphrases were merged: %#v", assembly)
	}
}

func TestBuildKnowledgeReleaseAssemblyIsBoundedQueryableAndPrivacySafe(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	first := knowledgeAssemblyTestRelease(
		"release-a", "book-a", "2026-07-26T10:00:00Z",
		"可检索结论", "private publisher value", "wechat_mp_article",
	)
	first.Book.SourceHTML = "local/private/source.html"
	first.Analysis.Summary = "private summary sentinel"
	first.Citations[0].SourceHTML = "local/private/citation.html"
	first.Citations[0].Note = "private citation note"
	saveKnowledgeAssemblyRelease(t, store, first)
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"另一个结论", "Publisher B", "dedao_course_article",
	))

	assembly, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{
		Limit: 1,
		Query: "可检索",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Clusters) != 1 || assembly.ReturnedClusters != 1 ||
		assembly.HasMore || !strings.Contains(assembly.Clusters[0].Claims[0].Statement, "可检索") {
		t.Fatalf("bounded query result = %#v", assembly)
	}
	payload, err := json.Marshal(assembly)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(payload)
	for _, forbidden := range []string{
		"private publisher value",
		"private summary sentinel",
		"local/private",
		"private citation note",
		`"source_account":`,
		`"prompt"`,
		`"answer"`,
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("assembly leaked %q: %s", forbidden, raw)
		}
	}
}

func knowledgeAssemblyTestRelease(
	releaseID, bookID, createdAt, statement, publisher, sourceType string,
) KnowledgeRelease {
	citationID := releaseID + "-citation"
	return KnowledgeRelease{
		SchemaVersion: KnowledgeReleaseSchemaVersion,
		Version:       knowledgeReleaseVersion,
		ReleaseID:     releaseID,
		BookID:        bookID,
		ContentHash:   "sha256:" + releaseID,
		UsagePolicy:   BookUsageEvidenceOnly,
		Book: BookKnowledgeBook{
			BookID:        bookID,
			Title:         "Book " + bookID,
			SourceType:    sourceType,
			SourceAccount: publisher,
		},
		Analysis: &BookAnalysisPayload{
			Summary: "summary",
			Claims: []BookAnalysisClaim{{
				ID:          releaseID + "-claim",
				Statement:   statement,
				CitationIDs: []string{citationID},
				Confidence:  0.8,
				RiskLevel:   "low",
			}},
		},
		Quality: BookQualityReport{Decision: BookQualityPass},
		Citations: []BookKnowledgeCitation{{
			CitationID: citationID,
			BookID:     bookID,
			ChapterID:  bookID + "-chapter",
			ChunkID:    bookID + "-chunk",
		}},
		CreatedAt: createdAt,
	}
}

func saveKnowledgeAssemblyRelease(t *testing.T, store *BookKnowledgeStore, release KnowledgeRelease) {
	t.Helper()
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
}
