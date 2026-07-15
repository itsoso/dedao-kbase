package app

import (
	"fmt"
	"strings"
	"time"
)

type KnowledgeImpactReport struct {
	PublishedReleases int            `json:"published_releases"`
	Receipts          map[string]int `json:"receipts"`
	PipelineStages    map[string]int `json:"pipeline_stages"`
}

type KnowledgeGapInput struct {
	Consumer    string `json:"consumer"`
	Domain      string `json:"domain"`
	Fingerprint string `json:"fingerprint"`
	Kind        string `json:"kind"`
}

type KnowledgeGapAggregate struct {
	Fingerprint string `json:"fingerprint"`
	Consumer    string `json:"consumer,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Kind        string `json:"kind"`
	Count       int    `json:"count"`
	UpdatedAt   string `json:"updated_at"`
}

type KnowledgeGapReport struct {
	Gaps []KnowledgeGapAggregate `json:"gaps"`
}

func BuildKnowledgeImpactReport(store *BookKnowledgeStore, catalog *KnowledgeCatalogStore) (KnowledgeImpactReport, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if catalog == nil {
		var err error
		catalog, err = NewKnowledgeCatalogStore(store.Root(), time.Now)
		if err != nil {
			return KnowledgeImpactReport{}, err
		}
		defer catalog.Close()
	}
	manifest, err := store.loadKnowledgeReleaseManifest()
	if err != nil {
		return KnowledgeImpactReport{}, err
	}
	report := KnowledgeImpactReport{
		PublishedReleases: len(manifest.Releases),
		Receipts:          map[string]int{},
		PipelineStages:    map[string]int{},
	}
	rows, err := catalog.db.Query(`SELECT disposition, COUNT(*) FROM knowledge_delivery_receipts GROUP BY disposition`)
	if err != nil {
		return KnowledgeImpactReport{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var disposition string
		var count int
		if err := rows.Scan(&disposition, &count); err != nil {
			return KnowledgeImpactReport{}, err
		}
		report.Receipts[disposition] = count
	}
	if err := rows.Err(); err != nil {
		return KnowledgeImpactReport{}, err
	}
	stageRows, err := catalog.db.Query(`SELECT stage, COUNT(*) FROM knowledge_pipeline_projections GROUP BY stage`)
	if err != nil {
		return KnowledgeImpactReport{}, err
	}
	defer stageRows.Close()
	for stageRows.Next() {
		var stage string
		var count int
		if err := stageRows.Scan(&stage, &count); err != nil {
			return KnowledgeImpactReport{}, err
		}
		report.PipelineStages[stage] = count
	}
	return report, stageRows.Err()
}

func (s *KnowledgeCatalogStore) RecordKnowledgeGap(input KnowledgeGapInput) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge catalog store is required")
	}
	input.Consumer = strings.TrimSpace(input.Consumer)
	input.Domain = strings.TrimSpace(input.Domain)
	input.Fingerprint = strings.TrimSpace(input.Fingerprint)
	input.Kind = strings.TrimSpace(input.Kind)
	if input.Fingerprint == "" || input.Kind == "" {
		return fmt.Errorf("fingerprint and kind are required")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`INSERT INTO knowledge_gap_aggregates (fingerprint, consumer, domain, kind, count, updated_at)
		VALUES (?, ?, ?, ?, 1, ?)
		ON CONFLICT(fingerprint, consumer, domain, kind) DO UPDATE SET count=count+1, updated_at=excluded.updated_at`,
		input.Fingerprint, input.Consumer, input.Domain, input.Kind, now)
	return err
}

func ListKnowledgeGaps(catalog *KnowledgeCatalogStore, limit int) (KnowledgeGapReport, error) {
	if catalog == nil {
		return KnowledgeGapReport{}, fmt.Errorf("knowledge catalog store is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := catalog.db.Query(`SELECT fingerprint, consumer, domain, kind, count, updated_at
		FROM knowledge_gap_aggregates ORDER BY count DESC, updated_at DESC, fingerprint LIMIT ?`, limit)
	if err != nil {
		return KnowledgeGapReport{}, err
	}
	defer rows.Close()
	report := KnowledgeGapReport{Gaps: []KnowledgeGapAggregate{}}
	for rows.Next() {
		var gap KnowledgeGapAggregate
		if err := rows.Scan(&gap.Fingerprint, &gap.Consumer, &gap.Domain, &gap.Kind, &gap.Count, &gap.UpdatedAt); err != nil {
			return KnowledgeGapReport{}, err
		}
		report.Gaps = append(report.Gaps, gap)
	}
	return report, rows.Err()
}
