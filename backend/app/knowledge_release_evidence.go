package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ReleaseEvidence contains only release-pinned, bounded excerpts; no local paths.
type ReleaseEvidence struct {
	SchemaVersion string `json:"schema_version"`
	ReleaseID     string `json:"release_id"`
	ContentHash   string `json:"content_hash"`
	CitationID    string `json:"citation_id"`
	BookID        string `json:"book_id"`
	ChapterID     string `json:"chapter_id"`
	ChunkID       string `json:"chunk_id"`
	UsagePolicy   string `json:"usage_policy"`
	Status        string `json:"status"`
	Excerpt       string `json:"excerpt,omitempty"`
	Truncated     bool   `json:"truncated"`
}

type releaseEvidenceSnapshot struct {
	ReleaseID   string            `json:"release_id"`
	ContentHash string            `json:"content_hash"`
	Items       []ReleaseEvidence `json:"items"`
}

func buildReleaseEvidence(release *KnowledgeRelease, pkg *BookKnowledgePackage) (*releaseEvidenceSnapshot, error) {
	hash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		return nil, err
	}
	verified := hash == release.ContentHash
	chunks := make(map[string]string)
	for _, chunk := range pkg.Chunks {
		chunks[chunk.ChunkID] = chunk.Text
	}
	snapshot := &releaseEvidenceSnapshot{ReleaseID: release.ReleaseID, ContentHash: release.ContentHash}
	for _, citation := range release.Citations {
		item := ReleaseEvidence{SchemaVersion: "release_evidence.v1", ReleaseID: release.ReleaseID,
			ContentHash: release.ContentHash, CitationID: citation.CitationID, BookID: release.BookID,
			ChapterID: citation.ChapterID, ChunkID: citation.ChunkID, UsagePolicy: release.UsagePolicy, Status: "unavailable"}
		if release.UsagePolicy == "evidence_only" {
			item.Status = "metadata_only"
		} else if release.UsagePolicy == "standard" && verified {
			if content, ok := chunks[citation.ChunkID]; ok {
				runes := []rune(content)
				if len(runes) > 4000 {
					runes = runes[:4000]
					item.Truncated = true
				}
				item.Status, item.Excerpt = "available", string(runes)
			}
		}
		snapshot.Items = append(snapshot.Items, item)
	}
	return snapshot, nil
}

func (s *BookKnowledgeStore) saveReleaseEvidence(release *KnowledgeRelease, pkg *BookKnowledgePackage) error {
	path := s.KnowledgeReleasePath(release.ReleaseID) + ".evidence"
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	snapshot, err := buildReleaseEvidence(release, pkg)
	if err != nil {
		return err
	}
	raw, err := encodeJSONFile(snapshot)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".release-evidence-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	// CreateTemp keeps raw excerpts private (0600), unlike general package JSON.
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func (s *BookKnowledgeStore) ResolveReleaseEvidence(releaseID, contentHash, citationID string) (*ReleaseEvidence, error) {
	if !strings.HasPrefix(releaseID, "release-") || sanitizeBookKnowledgeID(releaseID) != releaseID {
		return nil, fmt.Errorf("invalid release identity")
	}
	release, err := s.LoadKnowledgeRelease(releaseID)
	if err != nil {
		return nil, err
	}
	if release.Analysis == nil || release.ReleaseID != releaseID || release.ContentHash != contentHash {
		return nil, fmt.Errorf("release identity mismatch")
	}
	actualID, err := knowledgeReleaseID(release.Book, *release.Analysis, release.Quality, release.Sources, release.Citations)
	if err != nil || actualID != releaseID {
		return nil, fmt.Errorf("release integrity mismatch")
	}
	found := false
	for _, c := range release.Citations {
		if c.CitationID == citationID {
			found = true
		}
	}
	if !found {
		return nil, os.ErrNotExist
	}
	raw, err := os.ReadFile(s.KnowledgeReleasePath(releaseID) + ".evidence")
	var snapshot *releaseEvidenceSnapshot
	if err == nil {
		if err = json.Unmarshal(raw, &snapshot); err != nil {
			return nil, err
		}
	} else if os.IsNotExist(err) {
		// Legacy releases have no frozen excerpt. Never substitute a newer package.
		pkg, loadErr := s.LoadPackage(release.BookID)
		if loadErr == nil {
			snapshot, err = buildReleaseEvidence(release, pkg)
		}
		if loadErr != nil || err != nil {
			return &ReleaseEvidence{SchemaVersion: "release_evidence.v1", ReleaseID: releaseID,
				ContentHash: contentHash, CitationID: citationID, BookID: release.BookID,
				UsagePolicy: release.UsagePolicy, Status: "unavailable"}, nil
		}
	} else {
		return nil, err
	}
	if snapshot == nil || snapshot.ReleaseID != releaseID || snapshot.ContentHash != contentHash {
		return nil, fmt.Errorf("evidence snapshot identity mismatch")
	}
	for _, item := range snapshot.Items {
		if item.CitationID == citationID {
			if item.ReleaseID != releaseID || item.ContentHash != contentHash || item.UsagePolicy != release.UsagePolicy {
				return nil, fmt.Errorf("evidence identity mismatch")
			}
			if release.UsagePolicy != "standard" {
				item.Excerpt = ""
				item.Status = "metadata_only"
			}
			return &item, nil
		}
	}
	return nil, os.ErrNotExist
}

func (h *kbaseHTTPHandler) handleReleaseEvidence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	releaseID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/knowledge/releases/"), "/evidence")
	result, err := h.store.ResolveReleaseEvidence(releaseID, r.URL.Query().Get("content_hash"), r.URL.Query().Get("citation_id"))
	if err != nil {
		status := http.StatusConflict
		if os.IsNotExist(err) {
			status = http.StatusNotFound
		}
		writeHTTPError(w, status, "release evidence unavailable or identity mismatch")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeHTTPJSON(w, http.StatusOK, result)
}
