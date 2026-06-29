package app

import (
	"fmt"
	"sort"
	"strings"
)

const (
	BookKnowledgeProjectHealth    = "health"
	BookKnowledgeProjectProofroom = "proofroom"
)

type BookKnowledgeProject struct {
	ProjectID       string   `json:"project_id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	TargetSystem    string   `json:"target_system"`
	ExportType      string   `json:"export_type"`
	SourcePolicy    string   `json:"source_policy"`
	RequiresReview  bool     `json:"requires_review"`
	DefaultStatuses []string `json:"default_statuses"`
	DefaultTags     []string `json:"default_tags"`
}

type BookKnowledgeProjectReviewQueue struct {
	ProjectID string                         `json:"project_id"`
	Project   BookKnowledgeProject           `json:"project"`
	Items     []BookKnowledgeReviewQueueItem `json:"items"`
	Total     int                            `json:"total"`
	Limit     int                            `json:"limit"`
}

type BookKnowledgeReviewQueueItem struct {
	ProjectID          string   `json:"project_id"`
	BookID             string   `json:"book_id"`
	BookTitle          string   `json:"book_title"`
	ChapterID          string   `json:"chapter_id,omitempty"`
	ChapterTitle       string   `json:"chapter_title,omitempty"`
	ClaimID            string   `json:"claim_id"`
	Title              string   `json:"title"`
	Summary            string   `json:"summary"`
	ReviewStatus       string   `json:"review_status"`
	SourceReviewStatus string   `json:"source_review_status,omitempty"`
	EvidenceLevel      string   `json:"evidence_level,omitempty"`
	Confidence         float64  `json:"confidence,omitempty"`
	Citations          []string `json:"citations,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	RiskFlags          []string `json:"risk_flags,omitempty"`
}

type BookKnowledgeProjectExportPreview struct {
	ProjectID          string                         `json:"project_id"`
	Project            BookKnowledgeProject           `json:"project"`
	ExportType         string                         `json:"export_type"`
	SourcePolicy       string                         `json:"source_policy"`
	RequiresReview     bool                           `json:"requires_review"`
	BookCount          int                            `json:"book_count"`
	ClaimCount         int                            `json:"claim_count"`
	ReviewStatusCounts map[string]int                 `json:"review_status_counts"`
	Items              []BookKnowledgeReviewQueueItem `json:"items,omitempty"`
}

func SupportedBookKnowledgeProjects() []BookKnowledgeProject {
	return []BookKnowledgeProject{
		{
			ProjectID:      BookKnowledgeProjectHealth,
			Name:           "阿衡 Health KB",
			Description:    "把得到书籍转成健康知识库的待审核实体、claim 和风险边界。",
			TargetSystem:   "health-llm-driven",
			ExportType:     "health_kb_review_pack",
			SourcePolicy:   "draft_source_material",
			RequiresReview: true,
			DefaultStatuses: []string{
				"needs_review",
				"approved_for_health",
			},
			DefaultTags: []string{"health", "evidence", "risk-boundary"},
		},
		{
			ProjectID:      BookKnowledgeProjectProofroom,
			Name:           "Proofroom Source Pack",
			Description:    "把得到书籍转成可追溯的研究素材、论证卡片和证据线索。",
			TargetSystem:   "proofroom",
			ExportType:     "proofroom_source_pack",
			SourcePolicy:   "draft_source_material",
			RequiresReview: true,
			DefaultStatuses: []string{
				"needs_review",
				"approved_for_proofroom",
			},
			DefaultTags: []string{"proofroom", "source-pack", "argument"},
		},
	}
}

func BookKnowledgeProjectByID(projectID string) (BookKnowledgeProject, bool) {
	projectID = sanitizeBookKnowledgeID(strings.ToLower(strings.TrimSpace(projectID)))
	for _, project := range SupportedBookKnowledgeProjects() {
		if project.ProjectID == projectID {
			return project, true
		}
	}
	return BookKnowledgeProject{}, false
}

func (s *BookKnowledgeStore) BuildProjectReviewQueue(projectID string, limit int) (*BookKnowledgeProjectReviewQueue, error) {
	project, ok := BookKnowledgeProjectByID(projectID)
	if !ok {
		return nil, fmt.Errorf("unknown book knowledge project: %s", projectID)
	}
	limit = normalizeProjectLimit(limit)
	items, total, err := s.projectReviewItems(project, limit)
	if err != nil {
		return nil, err
	}
	return &BookKnowledgeProjectReviewQueue{
		ProjectID: project.ProjectID,
		Project:   project,
		Items:     items,
		Total:     total,
		Limit:     limit,
	}, nil
}

func (s *BookKnowledgeStore) BuildProjectExportPreview(projectID string, limit int) (*BookKnowledgeProjectExportPreview, error) {
	project, ok := BookKnowledgeProjectByID(projectID)
	if !ok {
		return nil, fmt.Errorf("unknown book knowledge project: %s", projectID)
	}
	limit = normalizeProjectLimit(limit)
	items, total, err := s.projectReviewItems(project, limit)
	if err != nil {
		return nil, err
	}
	bookIDs := map[string]bool{}
	statusCounts := map[string]int{}
	for _, item := range items {
		bookIDs[item.BookID] = true
		statusCounts[item.ReviewStatus]++
	}
	if total > len(items) {
		allItems, _, err := s.projectReviewItems(project, 0)
		if err != nil {
			return nil, err
		}
		bookIDs = map[string]bool{}
		statusCounts = map[string]int{}
		for _, item := range allItems {
			bookIDs[item.BookID] = true
			statusCounts[item.ReviewStatus]++
		}
	}
	return &BookKnowledgeProjectExportPreview{
		ProjectID:          project.ProjectID,
		Project:            project,
		ExportType:         project.ExportType,
		SourcePolicy:       project.SourcePolicy,
		RequiresReview:     project.RequiresReview,
		BookCount:          len(bookIDs),
		ClaimCount:         total,
		ReviewStatusCounts: statusCounts,
		Items:              items,
	}, nil
}

func (s *BookKnowledgeStore) projectReviewItems(project BookKnowledgeProject, limit int) ([]BookKnowledgeReviewQueueItem, int, error) {
	books, err := s.ListBooks()
	if err != nil {
		return nil, 0, err
	}
	items := []BookKnowledgeReviewQueueItem{}
	for _, book := range books {
		pkg, err := s.LoadPackage(book.BookID)
		if err != nil {
			return nil, 0, err
		}
		chapterTitles := make(map[string]string, len(pkg.Chapters))
		for _, chapter := range pkg.Chapters {
			chapterTitles[chapter.ChapterID] = chapter.Title
		}
		for _, claim := range pkg.Claims {
			items = append(items, projectReviewItem(project, book, chapterTitles, claim))
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].BookID != items[j].BookID {
			return items[i].BookID < items[j].BookID
		}
		if items[i].ChapterID != items[j].ChapterID {
			return items[i].ChapterID < items[j].ChapterID
		}
		return items[i].ClaimID < items[j].ClaimID
	})
	total := len(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, total, nil
}

func projectReviewItem(project BookKnowledgeProject, book BookKnowledgeBook, chapterTitles map[string]string, claim BookKnowledgeClaim) BookKnowledgeReviewQueueItem {
	sourceStatus := strings.TrimSpace(claim.ReviewStatus)
	if sourceStatus == "" {
		sourceStatus = "draft"
	}
	reviewStatus := "needs_review"
	if sourceStatus == "approved_for_"+project.ProjectID {
		reviewStatus = sourceStatus
	}
	return BookKnowledgeReviewQueueItem{
		ProjectID:          project.ProjectID,
		BookID:             claim.BookID,
		BookTitle:          book.Title,
		ChapterID:          claim.ChapterID,
		ChapterTitle:       chapterTitles[claim.ChapterID],
		ClaimID:            claim.ClaimID,
		Title:              claim.Title,
		Summary:            claim.Summary,
		ReviewStatus:       reviewStatus,
		SourceReviewStatus: sourceStatus,
		EvidenceLevel:      claim.EvidenceLevel,
		Confidence:         claim.Confidence,
		Citations:          append([]string(nil), claim.Citations...),
		Tags:               append([]string(nil), project.DefaultTags...),
		RiskFlags:          projectRiskFlags(project.ProjectID),
	}
}

func projectRiskFlags(projectID string) []string {
	switch projectID {
	case BookKnowledgeProjectHealth:
		return []string{"not_medical_advice", "requires_human_review"}
	case BookKnowledgeProjectProofroom:
		return []string{"source_lead", "requires_corroboration"}
	default:
		return []string{"requires_review"}
	}
}

func normalizeProjectLimit(limit int) int {
	if limit < 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}
