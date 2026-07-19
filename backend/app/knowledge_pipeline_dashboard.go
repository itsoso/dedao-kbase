package app

import (
	"context"
	"sort"
	"strings"
	"time"
)

type KnowledgePipelineDashboard struct {
	Summary KnowledgePipelineDashboardSummary `json:"summary"`
	Items   []KnowledgePipelineDashboardItem  `json:"items"`
}

type KnowledgePipelineDashboardSummary struct {
	Total          int `json:"total"`
	NeedsAnalysis  int `json:"needs_analysis"`
	NeedsQuality   int `json:"needs_quality"`
	ReadyToPublish int `json:"ready_to_publish"`
	Published      int `json:"published"`
	Blocked        int `json:"blocked"`
}

type KnowledgePipelineDashboardItem struct {
	BookID                 string `json:"book_id"`
	Title                  string `json:"title"`
	SourceType             string `json:"source_type,omitempty"`
	SourceAccount          string `json:"source_account,omitempty"`
	ContentHash            string `json:"content_hash,omitempty"`
	Stage                  string `json:"stage"`
	NextAction             string `json:"next_action"`
	PublicErrorCode        string `json:"public_error_code,omitempty"`
	UpdatedAt              string `json:"updated_at,omitempty"`
	LastPublishedReleaseID string `json:"last_published_release_id,omitempty"`
	LastPublishedAt        string `json:"last_published_at,omitempty"`
}

type KnowledgePipelineAutomationRequest struct {
	Limit              int    `json:"limit,omitempty"`
	DryRun             bool   `json:"dry_run,omitempty"`
	Model              string `json:"model,omitempty"`
	MaxContextChars    int    `json:"max_context_chars,omitempty"`
	EvaluateCandidates bool   `json:"evaluate_candidates,omitempty"`
}

type KnowledgePipelineAutomationResult struct {
	DryRun    bool                             `json:"dry_run"`
	Limit     int                              `json:"limit"`
	Eligible  int                              `json:"eligible"`
	Processed int                              `json:"processed"`
	Analyzed  int                              `json:"analyzed"`
	Qualified int                              `json:"qualified"`
	Skipped   int                              `json:"skipped"`
	Failed    int                              `json:"failed"`
	Items     []KnowledgePipelineAutomationRun `json:"items"`
}

type KnowledgePipelineAutomationRun struct {
	BookID     string `json:"book_id"`
	Title      string `json:"title,omitempty"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

func BuildKnowledgePipelineDashboard(store *BookKnowledgeStore, limit int) (*KnowledgePipelineDashboard, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	books, err := store.ListBooks()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := make([]KnowledgePipelineDashboardItem, 0, len(books))
	for _, book := range books {
		projection, err := deriveKnowledgePipelineProjection(store, book, time.Now)
		if err != nil {
			return nil, err
		}
		item := KnowledgePipelineDashboardItem{
			BookID:                 book.BookID,
			Title:                  book.Title,
			SourceType:             book.SourceType,
			SourceAccount:          book.SourceAccount,
			ContentHash:            book.ContentHash,
			Stage:                  projection.Stage,
			NextAction:             knowledgePipelineNextAction(projection),
			PublicErrorCode:        projection.PublicErrorCode,
			UpdatedAt:              projection.UpdatedAt,
			LastPublishedReleaseID: projection.LastPublishedReleaseID,
			LastPublishedAt:        projection.LastPublishedAt,
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if knowledgePipelineActionRank(items[i].NextAction) != knowledgePipelineActionRank(items[j].NextAction) {
			return knowledgePipelineActionRank(items[i].NextAction) < knowledgePipelineActionRank(items[j].NextAction)
		}
		if items[i].UpdatedAt != items[j].UpdatedAt {
			return items[i].UpdatedAt > items[j].UpdatedAt
		}
		return items[i].BookID < items[j].BookID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	dashboard := &KnowledgePipelineDashboard{Items: items}
	for _, item := range items {
		dashboard.Summary.Total++
		switch item.NextAction {
		case "needs_analysis":
			dashboard.Summary.NeedsAnalysis++
		case "needs_quality":
			dashboard.Summary.NeedsQuality++
		case "ready_to_publish":
			dashboard.Summary.ReadyToPublish++
		case "published":
			dashboard.Summary.Published++
		case "blocked":
			dashboard.Summary.Blocked++
		}
	}
	return dashboard, nil
}

func RunKnowledgePipelineAutomation(
	ctx context.Context,
	store *BookKnowledgeStore,
	generator BookAnalysisGenerator,
	request KnowledgePipelineAutomationRequest,
) (*KnowledgePipelineAutomationResult, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if generator == nil {
		generator = GenerateBookAnalysisManifest
	}
	if request.Limit <= 0 || request.Limit > 50 {
		request.Limit = 10
	}
	if request.MaxContextChars <= 0 {
		request.MaxContextChars = 16000
	}
	dashboard, err := BuildKnowledgePipelineDashboard(store, 500)
	if err != nil {
		return nil, err
	}
	result := &KnowledgePipelineAutomationResult{DryRun: request.DryRun, Limit: request.Limit}
	for _, item := range dashboard.Items {
		if item.NextAction != "needs_analysis" && item.NextAction != "needs_quality" {
			continue
		}
		result.Eligible++
		if len(result.Items) >= request.Limit {
			result.Skipped++
			continue
		}
		run := KnowledgePipelineAutomationRun{BookID: item.BookID, Title: item.Title, Action: item.NextAction, Status: "planned"}
		result.Items = append(result.Items, run)
		if request.DryRun {
			continue
		}
		result.Processed++
		switch item.NextAction {
		case "needs_analysis":
			_, err = generator(ctx, store, BookAnalysisGenerateRequest{
				BookID:          item.BookID,
				Model:           request.Model,
				MaxContextChars: request.MaxContextChars,
			})
			if err == nil {
				result.Analyzed++
				_, err = EvaluateBookAnalysisQuality(store, item.BookID)
				if err == nil {
					result.Qualified++
				}
			}
		case "needs_quality":
			_, err = EvaluateBookAnalysisQuality(store, item.BookID)
			if err == nil {
				result.Qualified++
			}
		}
		if err != nil {
			result.Failed++
			result.Items[len(result.Items)-1].Status = "failed"
			result.Items[len(result.Items)-1].Error = trimRunes(err.Error(), 500)
			continue
		}
		result.Items[len(result.Items)-1].Status = "succeeded"
		if after, loadErr := BuildKnowledgePipelineDashboard(store, 500); loadErr == nil {
			result.Items[len(result.Items)-1].NextAction = findKnowledgePipelineDashboardAction(after.Items, item.BookID)
		}
	}
	return result, nil
}

func knowledgePipelineNextAction(projection KnowledgePipelineProjection) string {
	if strings.TrimSpace(projection.PublicErrorCode) != "" {
		return "blocked"
	}
	switch projection.Stage {
	case KnowledgePipelineStageNormalized:
		return "needs_analysis"
	case KnowledgePipelineStageAnalyzed:
		return "needs_quality"
	case KnowledgePipelineStageCandidate:
		return "ready_to_publish"
	case KnowledgePipelineStagePublished:
		return "published"
	case KnowledgePipelineStageVerified:
		return "blocked"
	default:
		return "needs_analysis"
	}
}

func knowledgePipelineActionRank(action string) int {
	switch action {
	case "needs_analysis":
		return 1
	case "needs_quality":
		return 2
	case "ready_to_publish":
		return 3
	case "blocked":
		return 4
	case "published":
		return 5
	default:
		return 9
	}
}

func findKnowledgePipelineDashboardAction(items []KnowledgePipelineDashboardItem, bookID string) string {
	for _, item := range items {
		if item.BookID == bookID {
			return item.NextAction
		}
	}
	return ""
}
