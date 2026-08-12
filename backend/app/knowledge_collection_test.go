package app

import (
	"errors"
	"reflect"
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
