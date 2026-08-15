package app

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMaterializeKnowledgeCollectionReleaseIsDeterministicAndNamespaced(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	for _, fixture := range []struct {
		bookID, sourceItemKey, anchor, note string
	}{
		{"book-a", "article-a", "alpha anchor", "alpha note"},
		{"book-b", "article-b", "beta anchor", "beta note"},
	} {
		saveCollectionArticleFixture(t, store, fixture.bookID, fixture.sourceItemKey, "Fixture account")
		article, err := store.LoadPackage(fixture.bookID)
		if err != nil {
			t.Fatal(err)
		}
		article.Chapters[0].ChapterID = "chapter"
		article.Chapters[0].ChunkIDs = []string{"chunk"}
		article.Chunks[0].ChunkID = "chunk"
		article.Chunks[0].ChapterID = "chapter"
		article.Chunks[0].Text = "Grounded evidence for " + fixture.sourceItemKey
		article.Citations[0].CitationID = "citation"
		article.Citations[0].ChapterID = "chapter"
		article.Citations[0].ChunkID = "chunk"
		article.Citations[0].Anchor = fixture.anchor
		article.Citations[0].Note = fixture.note
		article.Book.ContentHash = ""
		if err := store.SavePackage(*article); err != nil {
			t.Fatal(err)
		}
		recordCollectionArticleFixture(t, store, "account-a", "Fixture account", fixture.bookID, fixture.sourceItemKey)
	}
	if _, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture"); err != nil {
		t.Fatal(err)
	}
	collection, err := store.PublishKnowledgeCollection("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}

	first, created, err := store.MaterializeKnowledgeCollectionRelease(collection.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first materialization reported a replay")
	}
	if first.Materialization.SchemaVersion != KnowledgeCollectionMaterializationSchemaVersion ||
		first.Materialization.SourceCollectionReleaseID != collection.ReleaseID ||
		first.Materialization.SourceContentHash != collection.ContentHash ||
		first.Materialization.TargetReleaseID != first.Release.ReleaseID ||
		first.Materialization.TargetContentHash != first.Release.ContentHash ||
		first.Materialization.MemberCount != 2 || first.Materialization.ClaimCount != 2 ||
		first.Materialization.CitationCount != 2 || first.Materialization.CreatedAt == "" {
		t.Fatalf("materialization = %#v", first.Materialization)
	}
	if first.Release.SchemaVersion != KnowledgeReleaseSchemaVersion ||
		first.Release.UsagePolicy != BookUsageEvidenceOnly || first.Release.Analysis == nil ||
		first.Release.Book.SourceType != "wechat_mp_article" ||
		first.Release.Book.SourceAccount != "Fixture account" {
		t.Fatalf("release = %#v", first.Release)
	}
	if len(first.Release.Analysis.Claims) != 2 || len(first.Release.Citations) != 2 {
		t.Fatalf("release evidence = %#v %#v", first.Release.Analysis.Claims, first.Release.Citations)
	}
	if first.Release.Analysis.Claims[0].ID == first.Release.Analysis.Claims[1].ID ||
		first.Release.Citations[0].CitationID == first.Release.Citations[1].CitationID ||
		first.Release.Citations[0].ChunkID == first.Release.Citations[1].ChunkID {
		t.Fatalf("local identifiers were not namespaced: claims=%#v citations=%#v", first.Release.Analysis.Claims, first.Release.Citations)
	}
	for index, claim := range first.Release.Analysis.Claims {
		if len(claim.CitationIDs) != 1 || claim.CitationIDs[0] != first.Release.Citations[index].CitationID {
			t.Fatalf("claim[%d] = %#v citation=%#v", index, claim, first.Release.Citations[index])
		}
		citation := first.Release.Citations[index]
		if citation.SourceType != "wechat_mp_article" || citation.SourceAccount != "Fixture account" ||
			citation.SourceItemKey == "" || citation.PublishedAt != "2026-08-12T00:00:00Z" ||
			citation.Anchor == "" || citation.Note == "" {
			t.Fatalf("citation[%d] provenance = %#v", index, citation)
		}
	}
	loaded, err := store.LoadKnowledgeRelease(first.Release.ReleaseID)
	if err != nil || !reflect.DeepEqual(*loaded, first.Release) {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}

	restarted := NewBookKnowledgeStore(root)
	replayed, created, err := restarted.MaterializeKnowledgeCollectionRelease(collection.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if created || !reflect.DeepEqual(first, replayed) {
		t.Fatalf("first=%#v replayed=%#v created=%v", first, replayed, created)
	}
}

func TestMaterializeKnowledgeCollectionReleaseRejectsInvalidPinnedEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *BookKnowledgeStore, KnowledgeCollectionRelease) string
	}{
		{
			name: "collection hash mismatch",
			mutate: func(t *testing.T, store *BookKnowledgeStore, release KnowledgeCollectionRelease) string {
				release.ContentHash = "sha256:" + strings.Repeat("0", 64)
				if err := writeJSONFile(store.KnowledgeCollectionReleasePath(release.ReleaseID), release); err != nil {
					t.Fatal(err)
				}
				return release.ReleaseID
			},
		},
		{
			name: "member content mismatch",
			mutate: func(t *testing.T, store *BookKnowledgeStore, release KnowledgeCollectionRelease) string {
				article, err := store.LoadPackage(release.Members[0].BookID)
				if err != nil {
					t.Fatal(err)
				}
				article.Chunks[0].Text += " changed"
				article.Book.ContentHash = ""
				if err := store.SavePackage(*article); err != nil {
					t.Fatal(err)
				}
				return release.ReleaseID
			},
		},
		{
			name: "member source identity mismatch",
			mutate: func(t *testing.T, store *BookKnowledgeStore, release KnowledgeCollectionRelease) string {
				article, err := store.LoadPackage(release.Members[0].BookID)
				if err != nil {
					t.Fatal(err)
				}
				article.Book.SourceAccount = "Different account"
				article.Book.ContentHash = ""
				if err := store.SavePackage(*article); err != nil {
					t.Fatal(err)
				}
				return repinCollectionReleaseForMaterializationTest(t, store, release, *article, nil)
			},
		},
		{
			name: "missing cited chunk",
			mutate: func(t *testing.T, store *BookKnowledgeStore, release KnowledgeCollectionRelease) string {
				article, err := store.LoadPackage(release.Members[0].BookID)
				if err != nil {
					t.Fatal(err)
				}
				article.Citations[0].ChunkID = "missing-chunk"
				article.Book.ContentHash = ""
				if err := store.SavePackage(*article); err != nil {
					t.Fatal(err)
				}
				return repinCollectionReleaseForMaterializationTest(t, store, release, *article, nil)
			},
		},
		{
			name: "no projectable citations",
			mutate: func(t *testing.T, store *BookKnowledgeStore, release KnowledgeCollectionRelease) string {
				article, err := store.LoadPackage(release.Members[0].BookID)
				if err != nil {
					t.Fatal(err)
				}
				return repinCollectionReleaseForMaterializationTest(t, store, release, *article, []string{})
			},
		},
		{
			name: "citation count limit",
			mutate: func(t *testing.T, store *BookKnowledgeStore, release KnowledgeCollectionRelease) string {
				article, err := store.LoadPackage(release.Members[0].BookID)
				if err != nil {
					t.Fatal(err)
				}
				article.Chunks = make([]BookKnowledgeChunk, knowledgeCollectionMaterializationMaxCitations+1)
				article.Citations = make([]BookKnowledgeCitation, knowledgeCollectionMaterializationMaxCitations+1)
				citationIDs := make([]string, knowledgeCollectionMaterializationMaxCitations+1)
				for index := range citationIDs {
					suffix := strings.TrimPrefix(stableKnowledgeID("materialization-limit", string(rune(index))), "materialization-limit-")
					chunkID := "chunk-" + suffix
					citationID := "citation-" + suffix
					article.Chunks[index] = BookKnowledgeChunk{
						ChunkID: chunkID, BookID: article.Book.BookID,
						ChapterID: article.Chapters[0].ChapterID, Order: index + 1, Text: "x",
					}
					article.Citations[index] = BookKnowledgeCitation{
						CitationID: citationID, BookID: article.Book.BookID,
						ChapterID: article.Chapters[0].ChapterID, ChunkID: chunkID,
						SourceType: article.Book.SourceType, SourceAccount: article.Book.SourceAccount,
						SourceItemKey: article.Book.SourceKey,
					}
					citationIDs[index] = citationID
				}
				article.Book.ContentHash = ""
				if err := store.SavePackage(*article); err != nil {
					t.Fatal(err)
				}
				return repinCollectionReleaseForMaterializationTest(t, store, release, *article, citationIDs)
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store, release := collectionMaterializationFixture(t)
			releaseID := testCase.mutate(t, store, *release)
			if _, _, err := store.MaterializeKnowledgeCollectionRelease(releaseID); err == nil {
				t.Fatal("invalid collection evidence unexpectedly materialized")
			}
			if _, err := os.Stat(store.KnowledgeCollectionMaterializationPath(releaseID)); !os.IsNotExist(err) {
				t.Fatalf("materialization record exists after rejection: %v", err)
			}
			releases, err := store.ListKnowledgeReleases("", 50)
			if err != nil {
				t.Fatal(err)
			}
			if len(releases) != 0 {
				t.Fatalf("partial target releases = %#v", releases)
			}
		})
	}
}

func TestMaterializeKnowledgeCollectionReleaseRejectsAggregateLimitOverflow(t *testing.T) {
	for _, testCase := range []struct {
		name                      string
		claims, citations, quoted int
	}{
		{"claims", knowledgeCollectionMaterializationMaxClaims + 1, 0, 0},
		{"citations", 0, knowledgeCollectionMaterializationMaxCitations + 1, 0},
		{"quoted characters", 0, 0, knowledgeCollectionMaterializationMaxQuoted + 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateKnowledgeCollectionMaterializationLimits(testCase.claims, testCase.citations, testCase.quoted); err == nil {
				t.Fatal("aggregate limit overflow unexpectedly accepted")
			}
		})
	}
}

func TestMaterializeKnowledgeCollectionReleaseProjectsOnlyAllowlistedCitations(t *testing.T) {
	store, collection := collectionMaterializationFixture(t)
	article, err := store.LoadPackage(collection.Members[0].BookID)
	if err != nil {
		t.Fatal(err)
	}
	article.Chunks = append(article.Chunks, BookKnowledgeChunk{
		ChunkID: "outside-chunk", BookID: article.Book.BookID,
		ChapterID: article.Chapters[0].ChapterID, Order: 2, Text: "outside evidence",
	})
	article.Citations = append(article.Citations, BookKnowledgeCitation{
		CitationID: "outside-citation", BookID: article.Book.BookID,
		ChapterID: article.Chapters[0].ChapterID, ChunkID: "outside-chunk",
		SourceType: article.Book.SourceType, SourceAccount: article.Book.SourceAccount,
		SourceItemKey: article.Book.SourceKey,
	})
	article.Book.ContentHash = ""
	if err := store.SavePackage(*article); err != nil {
		t.Fatal(err)
	}
	releaseID := repinCollectionReleaseForMaterializationTest(t, store, *collection, *article, collection.Members[0].CitationIDs)
	result, _, err := store.MaterializeKnowledgeCollectionRelease(releaseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Release.Analysis.Claims) != 1 || len(result.Release.Citations) != 1 ||
		strings.Contains(result.Release.Analysis.Claims[0].Statement, "outside evidence") {
		t.Fatalf("outside citation was projected: %#v %#v", result.Release.Analysis.Claims, result.Release.Citations)
	}
}

func TestMaterializeKnowledgeCollectionReleaseRecoversMissingProvenanceAndRejectsConflict(t *testing.T) {
	store, collection := collectionMaterializationFixture(t)
	first, _, err := store.MaterializeKnowledgeCollectionRelease(collection.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.KnowledgeCollectionMaterializationPath(collection.ReleaseID)); err != nil {
		t.Fatal(err)
	}
	recovered, created, err := store.MaterializeKnowledgeCollectionRelease(collection.ReleaseID)
	if err != nil || !created || !reflect.DeepEqual(first, recovered) {
		t.Fatalf("recovered=%#v created=%v err=%v", recovered, created, err)
	}

	conflict := recovered.Materialization
	conflict.TargetContentHash = "sha256:" + strings.Repeat("f", 64)
	if err := writeJSONFile(store.KnowledgeCollectionMaterializationPath(collection.ReleaseID), conflict); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MaterializeKnowledgeCollectionRelease(collection.ReleaseID); !errors.Is(err, ErrKnowledgeCollectionMaterializationConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}

func collectionMaterializationFixture(t *testing.T) (*BookKnowledgeStore, *KnowledgeCollectionRelease) {
	t.Helper()
	store := NewBookKnowledgeStore(t.TempDir())
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	saveCollectionArticleFixture(t, store, "book-a", "article-a", "Fixture account")
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-a", "article-a")
	if _, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture"); err != nil {
		t.Fatal(err)
	}
	release, err := store.PublishKnowledgeCollection("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	return store, release
}

func repinCollectionReleaseForMaterializationTest(
	t *testing.T,
	store *BookKnowledgeStore,
	release KnowledgeCollectionRelease,
	article BookKnowledgePackage,
	citationIDs []string,
) string {
	t.Helper()
	hash, err := BookKnowledgeContentHash(article)
	if err != nil {
		t.Fatal(err)
	}
	release.Members[0].ContentHash = hash
	if citationIDs != nil {
		release.Members[0].CitationIDs = append([]string(nil), citationIDs...)
	}
	release.ReleaseID = ""
	release.ContentHash = ""
	contentHash, releaseID, err := knowledgeCollectionReleaseIdentity(release)
	if err != nil {
		t.Fatal(err)
	}
	release.ContentHash = contentHash
	release.ReleaseID = releaseID
	if err := writeJSONFile(store.KnowledgeCollectionReleasePath(releaseID), release); err != nil {
		t.Fatal(err)
	}
	return releaseID
}
