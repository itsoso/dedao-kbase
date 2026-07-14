package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const knowledgeReleaseVersion = "1"

type KnowledgeRelease struct {
	Version     string                    `json:"version"`
	ReleaseID   string                    `json:"release_id"`
	BookID      string                    `json:"book_id"`
	ContentHash string                    `json:"content_hash"`
	Supersedes  string                    `json:"supersedes,omitempty"`
	UsagePolicy string                    `json:"usage_policy"`
	Book        BookKnowledgeBook         `json:"book"`
	Analysis    *BookAnalysisPayload      `json:"analysis"`
	Quality     BookQualityReport         `json:"quality"`
	Sources     []BookKnowledgeChatSource `json:"sources"`
	Citations   []BookKnowledgeCitation   `json:"citations"`
	CreatedAt   string                    `json:"created_at"`
}

type KnowledgeReleaseRecord struct {
	ReleaseID   string `json:"release_id"`
	BookID      string `json:"book_id"`
	ContentHash string `json:"content_hash"`
	Supersedes  string `json:"supersedes,omitempty"`
	UsagePolicy string `json:"usage_policy"`
	CreatedAt   string `json:"created_at"`
}

type KnowledgeReleaseManifest struct {
	Version   string                   `json:"version"`
	UpdatedAt string                   `json:"updated_at"`
	Releases  []KnowledgeReleaseRecord `json:"releases"`
}

func (s *BookKnowledgeStore) KnowledgeReleaseDir() string {
	return filepath.Join(s.root, "releases")
}

func (s *BookKnowledgeStore) KnowledgeReleaseManifestPath() string {
	return filepath.Join(s.KnowledgeReleaseDir(), "manifest.json")
}

func (s *BookKnowledgeStore) KnowledgeReleasePath(releaseID string) string {
	return filepath.Join(s.KnowledgeReleaseDir(), sanitizeBookKnowledgeID(releaseID)+".json")
}

func PublishKnowledgeRelease(store *BookKnowledgeStore, bookID string) (*KnowledgeRelease, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	releaseQueueLock, err := store.acquireKnowledgeReverificationFileLock()
	if err != nil {
		return nil, err
	}
	defer releaseQueueLock()
	pkg, err := store.LoadPackage(bookID)
	if err != nil {
		return nil, err
	}
	analysis, err := store.LoadAnalysisManifest(bookID)
	if err != nil {
		return nil, err
	}
	quality, err := store.LoadBookQualityReport(bookID)
	if err != nil {
		return nil, err
	}
	if quality.Decision != BookQualityPass {
		return nil, fmt.Errorf("knowledge release requires quality decision %q, got %q", BookQualityPass, quality.Decision)
	}
	if analysis.Status != BookAnalysisReady || analysis.Payload == nil {
		return nil, fmt.Errorf("knowledge release requires ready structured analysis")
	}
	if quality.ContentHash != pkg.Book.ContentHash || analysis.ContentHash != pkg.Book.ContentHash {
		return nil, fmt.Errorf("knowledge release content hash is stale")
	}
	analysisHash, err := bookAnalysisHash(*analysis)
	if err != nil {
		return nil, err
	}
	if quality.AnalysisHash == "" || quality.AnalysisHash != analysisHash {
		return nil, fmt.Errorf("knowledge release analysis hash is stale")
	}
	reverificationTask, err := store.ValidateKnowledgeReverificationPublication(bookID, analysisHash)
	if err != nil {
		return nil, err
	}
	releaseID, err := knowledgeReleaseID(pkg.Book, *analysis.Payload, *quality, analysis.Sources, pkg.Citations)
	if err != nil {
		return nil, err
	}
	if existing, loadErr := store.LoadKnowledgeRelease(releaseID); loadErr == nil {
		if err := store.saveKnowledgeRelease(*existing); err != nil {
			return nil, err
		}
		if reverificationTask != nil {
			if err := store.markKnowledgeReverificationPublished(reverificationTask.TaskID, existing.ReleaseID, time.Now()); err != nil {
				return nil, err
			}
		}
		return existing, nil
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return nil, loadErr
	}
	release := KnowledgeRelease{
		Version:     knowledgeReleaseVersion,
		ReleaseID:   releaseID,
		BookID:      pkg.Book.BookID,
		ContentHash: pkg.Book.ContentHash,
		UsagePolicy: quality.UsagePolicy,
		Book:        pkg.Book,
		Analysis:    analysis.Payload,
		Quality:     *quality,
		Sources:     append([]BookKnowledgeChatSource(nil), analysis.Sources...),
		Citations:   append([]BookKnowledgeCitation(nil), pkg.Citations...),
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	manifest, err := store.loadKnowledgeReleaseManifest()
	if err != nil {
		return nil, err
	}
	for index := len(manifest.Releases) - 1; index >= 0; index-- {
		record := manifest.Releases[index]
		if record.BookID == release.BookID && record.ReleaseID != release.ReleaseID {
			release.Supersedes = record.ReleaseID
			break
		}
	}
	if err := store.saveKnowledgeRelease(release); err != nil {
		return nil, err
	}
	if reverificationTask != nil {
		if err := store.markKnowledgeReverificationPublished(reverificationTask.TaskID, release.ReleaseID, time.Now()); err != nil {
			return nil, err
		}
	}
	return &release, nil
}

func knowledgeReleaseID(book BookKnowledgeBook, analysis BookAnalysisPayload, quality BookQualityReport, sources []BookKnowledgeChatSource, citations []BookKnowledgeCitation) (string, error) {
	seed := struct {
		Version      string                    `json:"version"`
		BookID       string                    `json:"book_id"`
		ContentHash  string                    `json:"content_hash"`
		Analysis     BookAnalysisPayload       `json:"analysis"`
		Decision     string                    `json:"decision"`
		UsagePolicy  string                    `json:"usage_policy"`
		AnalysisHash string                    `json:"analysis_hash"`
		Sources      []BookKnowledgeChatSource `json:"sources"`
		Citations    []BookKnowledgeCitation   `json:"citations"`
	}{knowledgeReleaseVersion, book.BookID, book.ContentHash, analysis, quality.Decision, quality.UsagePolicy, quality.AnalysisHash, sources, citations}
	payload, err := json.Marshal(seed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "release-" + hex.EncodeToString(sum[:]), nil
}

func (s *BookKnowledgeStore) saveKnowledgeRelease(release KnowledgeRelease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(release.ReleaseID) == "" || strings.TrimSpace(release.BookID) == "" {
		return fmt.Errorf("knowledge release requires release_id and book_id")
	}
	if err := os.MkdirAll(s.KnowledgeReleaseDir(), os.ModePerm); err != nil {
		return err
	}
	payload, err := encodeJSONFile(release)
	if err != nil {
		return err
	}
	path := s.KnowledgeReleasePath(release.ReleaseID)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := writeFileAtomically(path, payload); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	manifest, err := s.loadKnowledgeReleaseManifestUnlocked()
	if err != nil {
		return err
	}
	for _, record := range manifest.Releases {
		if record.ReleaseID == release.ReleaseID {
			return nil
		}
	}
	manifest.Version = knowledgeReleaseVersion
	manifest.UpdatedAt = release.CreatedAt
	manifest.Releases = append(manifest.Releases, KnowledgeReleaseRecord{
		ReleaseID: release.ReleaseID, BookID: release.BookID, ContentHash: release.ContentHash,
		Supersedes: release.Supersedes, UsagePolicy: release.UsagePolicy, CreatedAt: release.CreatedAt,
	})
	manifestPayload, err := encodeJSONFile(manifest)
	if err != nil {
		return err
	}
	return writeFileAtomically(s.KnowledgeReleaseManifestPath(), manifestPayload)
}

func (s *BookKnowledgeStore) LoadKnowledgeRelease(releaseID string) (*KnowledgeRelease, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	releaseID = sanitizeBookKnowledgeID(releaseID)
	if strings.TrimSpace(releaseID) == "" {
		return nil, fmt.Errorf("release_id is required")
	}
	var release KnowledgeRelease
	if err := readJSONFile(s.KnowledgeReleasePath(releaseID), &release); err != nil {
		return nil, err
	}
	return &release, nil
}

func (s *BookKnowledgeStore) ListKnowledgeReleases(after string, limit int) ([]KnowledgeReleaseRecord, error) {
	return s.ListKnowledgeReleasesForBook(after, limit, "")
}

func (s *BookKnowledgeStore) ListKnowledgeReleasesForBook(after string, limit int, bookID string) ([]KnowledgeReleaseRecord, error) {
	manifest, err := s.loadKnowledgeReleaseManifest()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	releases := manifest.Releases
	if bookID = strings.TrimSpace(bookID); bookID != "" {
		filtered := make([]KnowledgeReleaseRecord, 0)
		for _, record := range releases {
			if record.BookID == bookID {
				filtered = append(filtered, record)
			}
		}
		releases = filtered
	}
	start := 0
	if after = strings.TrimSpace(after); after != "" {
		start = len(releases)
		for index, record := range releases {
			if record.ReleaseID == after {
				start = index + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(releases) {
		end = len(releases)
	}
	return append([]KnowledgeReleaseRecord{}, releases[start:end]...), nil
}

func (s *BookKnowledgeStore) loadKnowledgeReleaseManifest() (*KnowledgeReleaseManifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadKnowledgeReleaseManifestUnlocked()
}

func (s *BookKnowledgeStore) loadKnowledgeReleaseManifestUnlocked() (*KnowledgeReleaseManifest, error) {
	var manifest KnowledgeReleaseManifest
	if err := readJSONFile(s.KnowledgeReleaseManifestPath(), &manifest); errors.Is(err, os.ErrNotExist) {
		return &KnowledgeReleaseManifest{Version: knowledgeReleaseVersion, Releases: []KnowledgeReleaseRecord{}}, nil
	} else if err != nil {
		return nil, err
	}
	sort.SliceStable(manifest.Releases, func(i, j int) bool {
		if manifest.Releases[i].CreatedAt != manifest.Releases[j].CreatedAt {
			return manifest.Releases[i].CreatedAt < manifest.Releases[j].CreatedAt
		}
		return manifest.Releases[i].ReleaseID < manifest.Releases[j].ReleaseID
	})
	return &manifest, nil
}
