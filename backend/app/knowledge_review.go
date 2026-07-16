package app

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const KnowledgeReviewSchemaVersion = "knowledge_review.v1"

type KnowledgeReviewCockpit struct {
	SchemaVersion string                  `json:"schema_version"`
	Items         []KnowledgeReviewItem   `json:"items"`
	Impact        KnowledgeImpactReport   `json:"impact"`
	RebuildPlan   KnowledgeRebuildPlan    `json:"rebuild_plan"`
	Gaps          []KnowledgeGapAggregate `json:"gaps"`
	GeneratedAt   string                  `json:"generated_at"`
}

type KnowledgeReviewItem struct {
	BookID                     string         `json:"book_id"`
	Title                      string         `json:"title,omitempty"`
	ReleaseID                  string         `json:"release_id"`
	ContentHash                string         `json:"content_hash"`
	UsagePolicy                string         `json:"usage_policy"`
	CreatedAt                  string         `json:"created_at"`
	QualityDecision            string         `json:"quality_decision,omitempty"`
	PipelineStage              string         `json:"pipeline_stage,omitempty"`
	PipelineErrorCode          string         `json:"pipeline_error_code,omitempty"`
	LatestReverificationStatus string         `json:"latest_reverification_status,omitempty"`
	LatestReverificationTaskID string         `json:"latest_reverification_task_id,omitempty"`
	ReceiptCounts              map[string]int `json:"receipt_counts"`
	AttentionReasons           []string       `json:"attention_reasons"`
}

func BuildKnowledgeReviewCockpit(store *BookKnowledgeStore, catalog *KnowledgeCatalogStore, limit int, now func() time.Time) (KnowledgeReviewCockpit, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if now == nil {
		now = time.Now
	}
	openedCatalog := false
	if catalog == nil {
		var err error
		catalog, err = NewKnowledgeCatalogStore(store.Root(), now)
		if err != nil {
			return KnowledgeReviewCockpit{}, err
		}
		openedCatalog = true
	}
	if openedCatalog {
		defer catalog.Close()
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if _, err := RebuildKnowledgePipelineProjection(store, catalog, now); err != nil {
		return KnowledgeReviewCockpit{}, err
	}
	manifest, err := store.loadKnowledgeReleaseManifest()
	if err != nil {
		return KnowledgeReviewCockpit{}, err
	}
	pipelineByBook, err := listKnowledgePipelineProjectionsByBook(catalog)
	if err != nil {
		return KnowledgeReviewCockpit{}, err
	}
	receiptCounts, err := listDeliveryReceiptCountsByRelease(catalog)
	if err != nil {
		return KnowledgeReviewCockpit{}, err
	}
	impact, err := BuildKnowledgeImpactReport(store, catalog)
	if err != nil {
		return KnowledgeReviewCockpit{}, err
	}
	rebuildPlan, err := BuildKnowledgeRebuildPlan(store, catalog, KnowledgeRebuildPlanQuery{})
	if err != nil {
		return KnowledgeReviewCockpit{}, err
	}
	gapReport, err := ListKnowledgeGaps(catalog, 20)
	if err != nil {
		return KnowledgeReviewCockpit{}, err
	}

	items := make([]KnowledgeReviewItem, 0, len(manifest.Releases))
	for index := len(manifest.Releases) - 1; index >= 0 && len(items) < limit; index-- {
		record := manifest.Releases[index]
		release, err := store.LoadKnowledgeRelease(record.ReleaseID)
		if err != nil {
			return KnowledgeReviewCockpit{}, err
		}
		item := KnowledgeReviewItem{
			BookID:          release.BookID,
			Title:           release.Book.Title,
			ReleaseID:       release.ReleaseID,
			ContentHash:     release.ContentHash,
			UsagePolicy:     release.UsagePolicy,
			CreatedAt:       release.CreatedAt,
			QualityDecision: release.Quality.Decision,
			ReceiptCounts:   copyStringIntMap(receiptCounts[release.ReleaseID]),
		}
		if item.ReceiptCounts == nil {
			item.ReceiptCounts = map[string]int{}
		}
		if projection, ok := pipelineByBook[release.BookID]; ok {
			item.PipelineStage = projection.Stage
			item.PipelineErrorCode = projection.PublicErrorCode
		}
		latest := latestKnowledgeReverificationTask(store, release.ReleaseID)
		if latest != nil {
			item.LatestReverificationStatus = latest.Status
			item.LatestReverificationTaskID = latest.TaskID
		}
		item.AttentionReasons = knowledgeReviewAttentionReasons(item)
		items = append(items, item)
	}
	return KnowledgeReviewCockpit{
		SchemaVersion: KnowledgeReviewSchemaVersion,
		Items:         items,
		Impact:        impact,
		RebuildPlan:   rebuildPlan,
		Gaps:          gapReport.Gaps,
		GeneratedAt:   now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func listKnowledgePipelineProjectionsByBook(catalog *KnowledgeCatalogStore) (map[string]KnowledgePipelineProjection, error) {
	if catalog == nil || catalog.db == nil {
		return nil, fmt.Errorf("knowledge catalog store is required")
	}
	rows, err := catalog.db.Query(`SELECT book_id, content_hash, stage, input_fingerprint, output_ref, attempts, updated_at, public_error_code, last_published_release_id, last_published_at FROM knowledge_pipeline_projections`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]KnowledgePipelineProjection{}
	for rows.Next() {
		var projection KnowledgePipelineProjection
		if err := rows.Scan(
			&projection.BookID,
			&projection.ContentHash,
			&projection.Stage,
			&projection.InputFingerprint,
			&projection.OutputRef,
			&projection.Attempts,
			&projection.UpdatedAt,
			&projection.PublicErrorCode,
			&projection.LastPublishedReleaseID,
			&projection.LastPublishedAt,
		); err != nil {
			return nil, err
		}
		result[projection.BookID] = projection
	}
	return result, rows.Err()
}

func listDeliveryReceiptCountsByRelease(catalog *KnowledgeCatalogStore) (map[string]map[string]int, error) {
	if catalog == nil || catalog.db == nil {
		return nil, fmt.Errorf("knowledge catalog store is required")
	}
	rows, err := catalog.db.Query(`SELECT release_id, disposition, COUNT(*) FROM knowledge_delivery_receipts GROUP BY release_id, disposition`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]map[string]int{}
	for rows.Next() {
		var releaseID, disposition string
		var count int
		if err := rows.Scan(&releaseID, &disposition, &count); err != nil {
			return nil, err
		}
		if result[releaseID] == nil {
			result[releaseID] = map[string]int{}
		}
		result[releaseID][disposition] = count
	}
	return result, rows.Err()
}

func latestKnowledgeReverificationTask(store *BookKnowledgeStore, releaseID string) *KnowledgeReverificationTask {
	tasks, err := store.ListKnowledgeReverifications(releaseID)
	if err != nil || len(tasks) == 0 {
		return nil
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt > tasks[j].UpdatedAt
	})
	return &tasks[0]
}

func knowledgeReviewAttentionReasons(item KnowledgeReviewItem) []string {
	var reasons []string
	switch item.LatestReverificationStatus {
	case KnowledgeReverificationQueued:
		reasons = append(reasons, "reverification_queued")
	case KnowledgeReverificationRunning:
		reasons = append(reasons, "reverification_running")
	case KnowledgeReverificationFailed:
		reasons = append(reasons, "reverification_failed")
	case KnowledgeReverificationCandidateReady:
		reasons = append(reasons, "candidate_ready")
	}
	if item.QualityDecision != "" && item.QualityDecision != BookQualityPass {
		reasons = append(reasons, "quality_"+sanitizeKnowledgeReviewReason(item.QualityDecision))
	}
	if item.PipelineErrorCode != "" {
		reasons = append(reasons, "pipeline_"+sanitizeKnowledgeReviewReason(item.PipelineErrorCode))
	}
	if len(item.ReceiptCounts) == 0 {
		reasons = append(reasons, "no_delivery_receipt")
	}
	return reasons
}

func sanitizeKnowledgeReviewReason(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, value)
	return strings.Trim(value, "_")
}

func copyStringIntMap(input map[string]int) map[string]int {
	if input == nil {
		return nil
	}
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
