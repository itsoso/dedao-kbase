package app

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	KnowledgePipelineStageCollected  = "collected"
	KnowledgePipelineStageNormalized = "normalized"
	KnowledgePipelineStageAnalyzed   = "analyzed"
	KnowledgePipelineStageVerified   = "verified"
	KnowledgePipelineStageCandidate  = "candidate"
	KnowledgePipelineStagePublished  = "published"
)

type KnowledgePipelineProjection struct {
	BookID                 string `json:"book_id"`
	ContentHash            string `json:"content_hash"`
	Stage                  string `json:"stage"`
	InputFingerprint       string `json:"input_fingerprint"`
	OutputRef              string `json:"output_ref,omitempty"`
	Attempts               int    `json:"attempts,omitempty"`
	UpdatedAt              string `json:"updated_at"`
	PublicErrorCode        string `json:"public_error_code,omitempty"`
	LastPublishedReleaseID string `json:"last_published_release_id,omitempty"`
	LastPublishedAt        string `json:"last_published_at,omitempty"`
}

func RebuildKnowledgePipelineProjection(store *BookKnowledgeStore, catalog *KnowledgeCatalogStore, now func() time.Time) ([]KnowledgePipelineProjection, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if now == nil {
		now = time.Now
	}
	if catalog == nil {
		var err error
		catalog, err = NewKnowledgeCatalogStore(store.Root(), now)
		if err != nil {
			return nil, err
		}
		defer catalog.Close()
	}
	books, err := store.ListBooks()
	if err != nil {
		return nil, err
	}
	projections := make([]KnowledgePipelineProjection, 0, len(books))
	for _, book := range books {
		projection, err := deriveKnowledgePipelineProjection(store, book, now)
		if err != nil {
			return nil, err
		}
		projections = append(projections, projection)
	}
	sort.Slice(projections, func(i, j int) bool {
		return projections[i].BookID < projections[j].BookID
	})
	if err := catalog.ReplaceKnowledgePipelineProjections(projections); err != nil {
		return nil, err
	}
	return projections, nil
}

func deriveKnowledgePipelineProjection(store *BookKnowledgeStore, book BookKnowledgeBook, now func() time.Time) (KnowledgePipelineProjection, error) {
	releaseID, publishedAt, err := latestPublishedReleaseForBook(store, book.BookID)
	if err != nil {
		return KnowledgePipelineProjection{}, err
	}
	updatedAt := firstNonEmpty(book.UpdatedAt, book.CreatedAt, now().UTC().Format(time.RFC3339Nano))
	projection := KnowledgePipelineProjection{
		BookID:                 book.BookID,
		ContentHash:            book.ContentHash,
		Stage:                  KnowledgePipelineStageNormalized,
		InputFingerprint:       book.ContentHash,
		OutputRef:              "books/" + sanitizeBookKnowledgeID(book.BookID) + "/manifest.json",
		UpdatedAt:              updatedAt,
		LastPublishedReleaseID: releaseID,
		LastPublishedAt:        publishedAt,
	}
	analysis, err := store.LoadAnalysisManifest(book.BookID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return projection, nil
		}
		return KnowledgePipelineProjection{}, err
	}
	projection.UpdatedAt = firstNonEmpty(analysis.UpdatedAt, projection.UpdatedAt)
	if analysis.ContentHash != book.ContentHash {
		projection.PublicErrorCode = "analysis_stale"
		return projection, nil
	}
	if analysis.Status == BookAnalysisFailed {
		projection.PublicErrorCode = "analysis_failed"
		return projection, nil
	}
	if analysis.Status != BookAnalysisReady || analysis.Payload == nil {
		return projection, nil
	}
	projection.Stage = KnowledgePipelineStageAnalyzed
	projection.OutputRef = "books/" + sanitizeBookKnowledgeID(book.BookID) + "/analysis_manifest.json"

	quality, err := store.LoadBookQualityReport(book.BookID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return projection, nil
		}
		return KnowledgePipelineProjection{}, err
	}
	projection.UpdatedAt = firstNonEmpty(quality.EvaluatedAt, projection.UpdatedAt)
	if quality.ContentHash != book.ContentHash {
		projection.PublicErrorCode = "quality_stale"
		return projection, nil
	}
	if quality.Decision != BookQualityPass {
		projection.Stage = KnowledgePipelineStageVerified
		projection.OutputRef = "books/" + sanitizeBookKnowledgeID(book.BookID) + "/quality_report.json"
		if strings.TrimSpace(quality.Decision) == "" {
			projection.PublicErrorCode = "quality_missing_decision"
		}
		return projection, nil
	}
	projection.Stage = KnowledgePipelineStageCandidate
	projection.OutputRef = "books/" + sanitizeBookKnowledgeID(book.BookID) + "/quality_report.json"
	if releaseID != "" {
		if release, loadErr := store.LoadKnowledgeRelease(releaseID); loadErr == nil && release.ContentHash == book.ContentHash {
			projection.Stage = KnowledgePipelineStagePublished
			projection.OutputRef = "releases/" + sanitizeBookKnowledgeID(releaseID) + ".json"
		}
	}
	return projection, nil
}

func latestPublishedReleaseForBook(store *BookKnowledgeStore, bookID string) (string, string, error) {
	releases, err := store.ListKnowledgeReleasesForBook("", 200, bookID)
	if err != nil {
		return "", "", err
	}
	if len(releases) == 0 {
		return "", "", nil
	}
	latest := releases[len(releases)-1]
	return latest.ReleaseID, latest.CreatedAt, nil
}

func (s *KnowledgeCatalogStore) ReplaceKnowledgePipelineProjections(projections []KnowledgePipelineProjection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge catalog store is required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM knowledge_pipeline_projections`); err != nil {
		return err
	}
	for _, projection := range projections {
		if _, err := tx.Exec(`INSERT INTO knowledge_pipeline_projections (
			book_id, content_hash, stage, input_fingerprint, output_ref, attempts, updated_at, public_error_code, last_published_release_id, last_published_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			projection.BookID, projection.ContentHash, projection.Stage, projection.InputFingerprint, projection.OutputRef, projection.Attempts,
			projection.UpdatedAt, projection.PublicErrorCode, projection.LastPublishedReleaseID, projection.LastPublishedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}
