package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestKnowledgeCollectionDefinitionPersistsAndEnforcesSourceIdentity(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	definition := KnowledgeCollectionDefinition{
		SchemaVersion:    KnowledgeCollectionDefinitionSchemaVersion,
		CollectionID:     "wechat-account-fixture",
		Title:            "Fixture account knowledge",
		SourceType:       "wechat_mp_article",
		SourceAccountKey: "account-fixture",
		SourceAccount:    "Fixture account",
		Enabled:          true,
	}

	saved, err := store.SaveKnowledgeCollection(definition)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CollectionID != definition.CollectionID || saved.CreatedAt == "" || saved.UpdatedAt == "" {
		t.Fatalf("saved definition = %#v", saved)
	}
	loaded, err := store.LoadKnowledgeCollection(definition.CollectionID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*saved, *loaded) {
		t.Fatalf("loaded definition = %#v, want %#v", loaded, saved)
	}
	listed, err := store.ListKnowledgeCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].CollectionID != definition.CollectionID {
		t.Fatalf("listed definitions = %#v", listed)
	}

	conflict := definition
	conflict.CollectionID = "wechat-account-conflict"
	if _, err := store.SaveKnowledgeCollection(conflict); !errors.Is(err, ErrKnowledgeCollectionSourceConflict) {
		t.Fatalf("source identity conflict error = %v", err)
	}
}

func TestBuildKnowledgeCollectionCandidateUsesCatalogAccountIdentity(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	saveCollectionArticleFixture(t, store, "book-a", "article-a", "Display name changed")
	saveCollectionArticleFixture(t, store, "book-foreign", "article-foreign", "Same display name")
	recordCollectionArticleFixture(t, store, "account-a", "Display name changed", "book-a", "article-a")
	recordCollectionArticleFixture(t, store, "account-b", "Same display name", "book-foreign", "article-foreign")

	candidate, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != KnowledgeCollectionCandidateReady || candidate.MemberCount != 1 || candidate.Members[0].BookID != "book-a" {
		t.Fatalf("candidate = %#v", candidate)
	}
	quality, err := store.LoadKnowledgeCollectionQuality("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if quality.Decision != BookQualityPass || quality.CandidateHash != candidate.CandidateHash || quality.UsagePolicy != BookUsageEvidenceOnly {
		t.Fatalf("quality = %#v", quality)
	}
	reloaded, err := store.LoadKnowledgeCollectionCandidate("wechat-account-fixture")
	if err != nil || !reflect.DeepEqual(candidate, reloaded) {
		t.Fatalf("reloaded candidate = %#v err=%v", reloaded, err)
	}
}

func TestBuildKnowledgeCollectionCandidateAcceptsLegacySourceContentHashAndPinsArtifactHash(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	saveCollectionArticleFixture(t, store, "book-legacy", "article-legacy", "Fixture account")

	pkg, err := store.LoadPackage("book-legacy")
	if err != nil {
		t.Fatal(err)
	}
	legacySourceHash := strings.Repeat("a", 64)
	pkg.Book.ContentHash = legacySourceHash
	if err := store.SavePackage(*pkg); err != nil {
		t.Fatal(err)
	}
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-legacy", "article-legacy")

	candidate, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	wantArtifactHash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != KnowledgeCollectionCandidateReady || candidate.MemberCount != 1 {
		t.Fatalf("candidate = %#v", candidate)
	}
	if candidate.Members[0].ContentHash != wantArtifactHash || candidate.Members[0].ContentHash == legacySourceHash {
		t.Fatalf("member content hash = %q, want artifact hash %q", candidate.Members[0].ContentHash, wantArtifactHash)
	}
}

func TestBuildKnowledgeCollectionCandidateQuarantinesInvalidMember(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	saveCollectionArticleFixture(t, store, "book-valid", "article-valid", "Fixture account")
	saveCollectionArticleFixture(t, store, "book-invalid", "article-invalid", "Fixture account")
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-valid", "article-valid")
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-invalid", "article-invalid")

	invalid, err := store.LoadPackage("book-invalid")
	if err != nil {
		t.Fatal(err)
	}
	invalid.Citations = nil
	invalid.Book.ContentHash = ""
	if err := store.SavePackage(*invalid); err != nil {
		t.Fatal(err)
	}

	candidate, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != KnowledgeCollectionCandidatePartial || candidate.MemberCount != 1 || len(candidate.Exclusions) != 1 {
		t.Fatalf("candidate = %#v", candidate)
	}
	quality, err := store.LoadKnowledgeCollectionQuality("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if quality.Decision != BookQualityQuarantine {
		t.Fatalf("quality = %#v", quality)
	}
}

func TestBuildKnowledgeCollectionCandidateRejectsWhenNoMemberIsUsable(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	saveCollectionArticleFixture(t, store, "book-invalid", "article-invalid", "Fixture account")
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-invalid", "article-invalid")

	invalid, err := store.LoadPackage("book-invalid")
	if err != nil {
		t.Fatal(err)
	}
	invalid.Citations[0].ChunkID = "missing-chunk"
	invalid.Book.ContentHash = ""
	if err := store.SavePackage(*invalid); err != nil {
		t.Fatal(err)
	}

	candidate, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != KnowledgeCollectionCandidateBlocked || candidate.MemberCount != 0 || len(candidate.Exclusions) != 1 {
		t.Fatalf("candidate = %#v", candidate)
	}
	quality, err := store.LoadKnowledgeCollectionQuality("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if quality.Decision != BookQualityReject {
		t.Fatalf("quality = %#v", quality)
	}
}

func TestKnowledgeCollectionReleaseRequiresPassingFreshCandidate(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	if _, err := store.PublishKnowledgeCollection("wechat-account-fixture"); err == nil {
		t.Fatal("publication without a candidate unexpectedly succeeded")
	}

	saveCollectionArticleFixture(t, store, "book-valid", "article-valid", "Fixture account")
	saveCollectionArticleFixture(t, store, "book-invalid", "article-invalid", "Fixture account")
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-valid", "article-valid")
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-invalid", "article-invalid")
	invalid, err := store.LoadPackage("book-invalid")
	if err != nil {
		t.Fatal(err)
	}
	invalid.Citations = nil
	invalid.Book.ContentHash = ""
	if err := store.SavePackage(*invalid); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishKnowledgeCollection("wechat-account-fixture"); err == nil {
		t.Fatal("publication with quarantined quality unexpectedly succeeded")
	}
}

func TestKnowledgeCollectionReleaseIsDeterministicImmutableAndPersistent(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	saveCollectionArticleFixture(t, store, "book-a", "article-a", "Fixture account")
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-a", "article-a")
	if _, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture"); err != nil {
		t.Fatal(err)
	}

	first, err := store.PublishKnowledgeCollection("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := store.PublishKnowledgeCollection("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if first.ReleaseID == "" || !reflect.DeepEqual(first, replayed) {
		t.Fatalf("first=%#v replayed=%#v", first, replayed)
	}
	restarted := NewBookKnowledgeStore(root)
	loaded, err := restarted.LoadKnowledgeCollectionRelease(first.ReleaseID)
	if err != nil || !reflect.DeepEqual(first, loaded) {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	listed, err := restarted.ListKnowledgeCollectionReleases("wechat-account-fixture")
	if err != nil || len(listed) != 1 || listed[0].ReleaseID != first.ReleaseID {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
}

func TestKnowledgeCollectionReleaseRejectsStaleMemberAndSupersedesWithoutMutation(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveCollectionFixture(t, store, "wechat-account-fixture", "account-a")
	saveCollectionArticleFixture(t, store, "book-a", "article-a", "Fixture account")
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-a", "article-a")
	if _, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture"); err != nil {
		t.Fatal(err)
	}
	first, err := store.PublishKnowledgeCollection("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	firstSnapshot := *first

	pkg, err := store.LoadPackage("book-a")
	if err != nil {
		t.Fatal(err)
	}
	pkg.Chunks[0].Text += " updated"
	pkg.Book.ContentHash = ""
	if err := store.SavePackage(*pkg); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishKnowledgeCollection("wechat-account-fixture"); err == nil {
		t.Fatal("stale candidate unexpectedly published")
	}
	recordCollectionArticleFixture(t, store, "account-a", "Fixture account", "book-a", "article-a")
	if _, err := store.BuildKnowledgeCollectionCandidate("wechat-account-fixture"); err != nil {
		t.Fatal(err)
	}
	second, err := store.PublishKnowledgeCollection("wechat-account-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if second.ReleaseID == first.ReleaseID {
		t.Fatal("updated collection reused the old release id")
	}
	loadedFirst, err := store.LoadKnowledgeCollectionRelease(first.ReleaseID)
	if err != nil || !reflect.DeepEqual(firstSnapshot, *loadedFirst) {
		t.Fatalf("old release mutated: %#v err=%v", loadedFirst, err)
	}
}

func saveCollectionFixture(t *testing.T, store *BookKnowledgeStore, collectionID, accountKey string) {
	t.Helper()
	_, err := store.SaveKnowledgeCollection(KnowledgeCollectionDefinition{
		SchemaVersion: KnowledgeCollectionDefinitionSchemaVersion,
		CollectionID:  collectionID, Title: "Fixture collection", SourceType: "wechat_mp_article",
		SourceAccountKey: accountKey, SourceAccount: "Fixture account", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func saveCollectionArticleFixture(t *testing.T, store *BookKnowledgeStore, bookID, sourceItemKey, sourceAccount string) {
	t.Helper()
	pkg := BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID: bookID, Title: "Article " + sourceItemKey, SourceType: "wechat_mp_article",
			SourceKey: sourceItemKey, SourceAccount: sourceAccount, PublishedAt: "2026-08-12T00:00:00Z", Status: "ready",
		},
		Chapters: []BookKnowledgeChapter{{ChapterID: bookID + "-chapter", BookID: bookID, Order: 1, Title: "Article", ChunkIDs: []string{bookID + "-chunk"}}},
		Chunks:   []BookKnowledgeChunk{{ChunkID: bookID + "-chunk", BookID: bookID, ChapterID: bookID + "-chapter", Order: 1, Text: "Evidence for " + sourceItemKey}},
		Citations: []BookKnowledgeCitation{{
			CitationID: bookID + "-citation", BookID: bookID, ChapterID: bookID + "-chapter", ChunkID: bookID + "-chunk",
			SourceType: "wechat_mp_article", SourceAccount: sourceAccount, SourceItemKey: sourceItemKey,
		}},
	}
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
}

func recordCollectionArticleFixture(t *testing.T, store *BookKnowledgeStore, accountKey, accountName, bookID, sourceItemKey string) {
	t.Helper()
	pkg, err := store.LoadPackage(bookID)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewKnowledgeCatalogStore(store.Root(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	_, err = catalog.RecordContentVersion(SourceArticleEnvelope{
		SourceType: "wechat_mp_article", SourceAccountID: accountKey, SourceAccount: accountName,
		SourceItemID: sourceItemKey, SourceURL: "https://mp.weixin.qq.com/s/" + sourceItemKey,
	}, pkg.Book.ContentHash, bookID, "books/"+bookID+"/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeCollectionCandidateHashIsDeterministic(t *testing.T) {
	first, err := FinalizeKnowledgeCollectionCandidate(KnowledgeCollectionCandidate{
		SchemaVersion: KnowledgeCollectionCandidateSchemaVersion,
		CollectionID:  "wechat-account-fixture",
		Status:        KnowledgeCollectionCandidateReady,
		Members: []KnowledgeCollectionMember{
			{BookID: "book-b", ContentHash: "hash-b", SourceID: "source-b", SourceItemKey: "item-b", CitationIDs: []string{"citation-b"}},
			{BookID: "book-a", ContentHash: "hash-a", SourceID: "source-a", SourceItemKey: "item-a", CitationIDs: []string{"citation-a"}},
		},
		BuiltAt: "2026-08-12T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := FinalizeKnowledgeCollectionCandidate(KnowledgeCollectionCandidate{
		SchemaVersion: KnowledgeCollectionCandidateSchemaVersion,
		CollectionID:  "wechat-account-fixture",
		Status:        KnowledgeCollectionCandidateReady,
		Members: []KnowledgeCollectionMember{
			{BookID: "book-a", ContentHash: "hash-a", SourceID: "source-a", SourceItemKey: "item-a", CitationIDs: []string{"citation-a"}},
			{BookID: "book-b", ContentHash: "hash-b", SourceID: "source-b", SourceItemKey: "item-b", CitationIDs: []string{"citation-b"}},
		},
		BuiltAt: "2026-08-12T13:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.CandidateHash == "" || first.CandidateHash != second.CandidateHash {
		t.Fatalf("candidate hashes = %q, %q", first.CandidateHash, second.CandidateHash)
	}
	if first.Members[0].BookID != "book-a" || second.Members[0].BookID != "book-a" {
		t.Fatalf("members were not canonicalized: %#v %#v", first.Members, second.Members)
	}
}

func TestKnowledgeCollectionCandidateRejectsDuplicateAndOversizedMembers(t *testing.T) {
	duplicate := KnowledgeCollectionCandidate{
		SchemaVersion: KnowledgeCollectionCandidateSchemaVersion,
		CollectionID:  "wechat-account-fixture",
		Status:        KnowledgeCollectionCandidateReady,
		Members: []KnowledgeCollectionMember{
			{BookID: "book-a", ContentHash: "hash-a", SourceID: "source-a", SourceItemKey: "item-a", CitationIDs: []string{"citation-a"}},
			{BookID: "book-a", ContentHash: "hash-b", SourceID: "source-b", SourceItemKey: "item-b", CitationIDs: []string{"citation-b"}},
		},
	}
	if _, err := FinalizeKnowledgeCollectionCandidate(duplicate); err == nil {
		t.Fatal("duplicate member unexpectedly accepted")
	}

	oversized := duplicate
	oversized.Members = make([]KnowledgeCollectionMember, KnowledgeCollectionMaxMembers+1)
	for index := range oversized.Members {
		oversized.Members[index] = KnowledgeCollectionMember{
			BookID:        "book-" + time.Unix(int64(index), 0).UTC().Format("150405.000000000"),
			ContentHash:   "hash",
			SourceID:      "source-" + time.Unix(int64(index), 0).UTC().Format("150405.000000000"),
			SourceItemKey: "item-" + time.Unix(int64(index), 0).UTC().Format("150405.000000000"),
			CitationIDs:   []string{"citation"},
		}
	}
	if _, err := FinalizeKnowledgeCollectionCandidate(oversized); err == nil {
		t.Fatal("oversized member set unexpectedly accepted")
	}
}
