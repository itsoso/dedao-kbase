package app

import (
	"fmt"
	"sort"
	"strings"
)

const (
	KnowledgeRebuildPlanSchemaVersion = "knowledge_rebuild_plan.v1"

	KnowledgeRebuildActionNoop            = "noop"
	KnowledgeRebuildActionRebuild         = "rebuild"
	KnowledgeRebuildActionReevaluate      = "reevaluate"
	KnowledgeRebuildActionRepublish       = "republish"
	KnowledgeRebuildActionNotifyConsumers = "notify_consumers"
)

type KnowledgeRebuildPlanQuery struct {
	BookID string `json:"book_id,omitempty"`
}

type KnowledgeRebuildPlan struct {
	SchemaVersion string                     `json:"schema_version"`
	Items         []KnowledgeRebuildPlanItem `json:"items"`
}

type KnowledgeRebuildPlanItem struct {
	BookID               string   `json:"book_id"`
	Title                string   `json:"title,omitempty"`
	ReleaseID            string   `json:"release_id"`
	ReleaseContentHash   string   `json:"release_content_hash"`
	CurrentContentHash   string   `json:"current_content_hash"`
	ContentChanged       bool     `json:"content_changed"`
	QualityDecision      string   `json:"quality_decision,omitempty"`
	ConsumerReceiptCount int      `json:"consumer_receipt_count"`
	Actions              []string `json:"actions"`
	Reasons              []string `json:"reasons"`
}

func BuildKnowledgeRebuildPlan(store *BookKnowledgeStore, catalog *KnowledgeCatalogStore, query KnowledgeRebuildPlanQuery) (KnowledgeRebuildPlan, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if catalog == nil {
		var err error
		catalog, err = NewKnowledgeCatalogStore(store.Root(), nil)
		if err != nil {
			return KnowledgeRebuildPlan{}, err
		}
		defer catalog.Close()
	}
	records, err := store.ListKnowledgeReleases("", 500)
	if err != nil {
		return KnowledgeRebuildPlan{}, err
	}
	items := make([]KnowledgeRebuildPlanItem, 0, len(records))
	bookID := strings.TrimSpace(query.BookID)
	for _, record := range records {
		if bookID != "" && record.BookID != bookID {
			continue
		}
		release, err := store.LoadKnowledgeRelease(record.ReleaseID)
		if err != nil {
			return KnowledgeRebuildPlan{}, err
		}
		item, err := buildKnowledgeRebuildPlanItem(store, catalog, *release)
		if err != nil {
			return KnowledgeRebuildPlan{}, err
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ContentChanged != items[j].ContentChanged {
			return items[i].ContentChanged
		}
		return items[i].ReleaseID > items[j].ReleaseID
	})
	return KnowledgeRebuildPlan{SchemaVersion: KnowledgeRebuildPlanSchemaVersion, Items: items}, nil
}

func buildKnowledgeRebuildPlanItem(store *BookKnowledgeStore, catalog *KnowledgeCatalogStore, release KnowledgeRelease) (KnowledgeRebuildPlanItem, error) {
	pkg, err := store.LoadPackage(release.BookID)
	item := KnowledgeRebuildPlanItem{
		BookID:             release.BookID,
		Title:              release.Book.Title,
		ReleaseID:          release.ReleaseID,
		ReleaseContentHash: release.ContentHash,
		QualityDecision:    release.Quality.Decision,
	}
	if err != nil {
		item.ContentChanged = true
		item.Actions = append(item.Actions, KnowledgeRebuildActionRebuild)
		item.Reasons = append(item.Reasons, "package_missing")
	} else {
		item.Title = firstNonEmpty(pkg.Book.Title, release.Book.Title)
		item.CurrentContentHash = pkg.Book.ContentHash
		item.ContentChanged = pkg.Book.ContentHash != "" && pkg.Book.ContentHash != release.ContentHash
	}
	item.ConsumerReceiptCount, err = countDeliveryReceiptsForRelease(catalog, release.ReleaseID)
	if err != nil {
		return KnowledgeRebuildPlanItem{}, err
	}
	if item.ContentChanged {
		item.Actions = appendMissingStrings(item.Actions, KnowledgeRebuildActionRebuild, KnowledgeRebuildActionReevaluate, KnowledgeRebuildActionRepublish)
		if len(item.Reasons) == 0 {
			item.Reasons = append(item.Reasons, "source_content_changed")
		}
		if item.ConsumerReceiptCount > 0 {
			item.Actions = appendMissingStrings(item.Actions, KnowledgeRebuildActionNotifyConsumers)
			item.Reasons = append(item.Reasons, "consumer_imported_previous_release")
		}
	} else {
		item.Actions = append(item.Actions, KnowledgeRebuildActionNoop)
		item.Reasons = append(item.Reasons, "release_matches_current_content")
	}
	return item, nil
}

func appendMissingStrings(values []string, candidates ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, candidate := range candidates {
		if !seen[candidate] {
			values = append(values, candidate)
			seen[candidate] = true
		}
	}
	return values
}

func countDeliveryReceiptsForRelease(catalog *KnowledgeCatalogStore, releaseID string) (int, error) {
	if catalog == nil || catalog.db == nil {
		return 0, fmt.Errorf("knowledge catalog store is required")
	}
	var count int
	if err := catalog.db.QueryRow(`SELECT COUNT(*) FROM knowledge_delivery_receipts WHERE release_id = ?`, strings.TrimSpace(releaseID)).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
