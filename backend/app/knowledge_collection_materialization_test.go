package app

import (
	"reflect"
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
