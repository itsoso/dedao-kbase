package app

import (
	"errors"
	"os"
	"strings"
)

type KnowledgeLineage struct {
	ObjectID      string   `json:"object_id"`
	ObjectKind    string   `json:"object_kind"`
	BookID        string   `json:"book_id,omitempty"`
	ReleaseID     string   `json:"release_id,omitempty"`
	ContentHash   string   `json:"content_hash,omitempty"`
	SourceType    string   `json:"source_type,omitempty"`
	SourceItemKey string   `json:"source_item_key,omitempty"`
	UsagePolicy   string   `json:"usage_policy,omitempty"`
	ArtifactRefs  []string `json:"artifact_refs,omitempty"`
	Citations     []string `json:"citations,omitempty"`
}

func BuildKnowledgeLineage(store *BookKnowledgeStore, objectID string) (KnowledgeLineage, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	objectID = strings.TrimSpace(objectID)
	if objectID == "" {
		return KnowledgeLineage{}, os.ErrNotExist
	}
	if release, err := store.LoadKnowledgeRelease(objectID); err == nil {
		return releaseLineage(store, *release)
	} else if !errors.Is(err, os.ErrNotExist) {
		return KnowledgeLineage{}, err
	}
	if pkg, err := store.LoadPackage(objectID); err == nil {
		return packageLineage(objectID, pkg.Book, pkg.Citations), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return KnowledgeLineage{}, err
	}
	return KnowledgeLineage{}, os.ErrNotExist
}

func releaseLineage(store *BookKnowledgeStore, release KnowledgeRelease) (KnowledgeLineage, error) {
	lineage := KnowledgeLineage{
		ObjectID:     release.ReleaseID,
		ObjectKind:   "release",
		BookID:       release.BookID,
		ReleaseID:    release.ReleaseID,
		ContentHash:  release.ContentHash,
		UsagePolicy:  release.UsagePolicy,
		ArtifactRefs: []string{"releases/" + sanitizeBookKnowledgeID(release.ReleaseID) + ".json"},
	}
	if pkg, err := store.LoadPackage(release.BookID); err == nil {
		lineage.SourceType = pkg.Book.SourceType
		lineage.SourceItemKey = pkg.Book.SourceKey
		lineage.ArtifactRefs = append(lineage.ArtifactRefs, "books/"+sanitizeBookKnowledgeID(release.BookID)+"/manifest.json")
		lineage.Citations = citationIDs(pkg.Citations)
	} else if !errors.Is(err, os.ErrNotExist) {
		return KnowledgeLineage{}, err
	}
	if len(lineage.Citations) == 0 {
		lineage.Citations = citationIDs(release.Citations)
	}
	return lineage, nil
}

func packageLineage(objectID string, book BookKnowledgeBook, citations []BookKnowledgeCitation) KnowledgeLineage {
	return KnowledgeLineage{
		ObjectID:      objectID,
		ObjectKind:    "book",
		BookID:        book.BookID,
		ContentHash:   book.ContentHash,
		SourceType:    book.SourceType,
		SourceItemKey: book.SourceKey,
		ArtifactRefs:  []string{"books/" + sanitizeBookKnowledgeID(book.BookID) + "/manifest.json"},
		Citations:     citationIDs(citations),
	}
}

func citationIDs(citations []BookKnowledgeCitation) []string {
	ids := make([]string, 0, len(citations))
	for _, citation := range citations {
		if id := strings.TrimSpace(citation.CitationID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
