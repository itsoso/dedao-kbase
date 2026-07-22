package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const KnowledgeOperationsSchemaVersion = "knowledge_operations.v1"

type KnowledgeOperationsConsole struct {
	SchemaVersion     string                                `json:"schema_version"`
	Summary           KnowledgeOperationsSummary            `json:"summary"`
	HealthReviewQueue []KnowledgeOperationsHealthReviewItem `json:"health_review_queue"`
	Items             []KnowledgeOperationsItem             `json:"items"`
}

type KnowledgeOperationsSummary struct {
	Total                int `json:"total"`
	NeedsAnalysis        int `json:"needs_analysis"`
	NeedsQuality         int `json:"needs_quality"`
	ReadyToPublish       int `json:"ready_to_publish"`
	Published            int `json:"published"`
	Blocked              int `json:"blocked"`
	HealthReadyToPublish int `json:"health_ready_to_publish"`
	HealthPublished      int `json:"health_published"`
	HealthBlocked        int `json:"health_blocked"`
}

type KnowledgeOperationsItem struct {
	BookID          string                            `json:"book_id"`
	Title           string                            `json:"title"`
	SourceType      string                            `json:"source_type,omitempty"`
	SourceAccount   string                            `json:"source_account,omitempty"`
	ContentHash     string                            `json:"content_hash,omitempty"`
	PipelineStage   string                            `json:"pipeline_stage"`
	NextAction      string                            `json:"next_action"`
	ReleaseID       string                            `json:"release_id,omitempty"`
	QualityDecision string                            `json:"quality_decision,omitempty"`
	UsagePolicy     string                            `json:"usage_policy,omitempty"`
	Health          KnowledgeOperationsHealthSummary  `json:"health"`
	Failure         KnowledgeOperationsFailureSummary `json:"failure"`
}

type KnowledgeOperationsHealthSummary struct {
	Status         string         `json:"status,omitempty"`
	NextAction     string         `json:"next_action,omitempty"`
	ServingAllowed bool           `json:"serving_allowed"`
	Reasons        []string       `json:"reasons,omitempty"`
	ClaimCount     int            `json:"claim_count,omitempty"`
	CitationCount  int            `json:"citation_count,omitempty"`
	RiskCounts     map[string]int `json:"risk_counts,omitempty"`
}

type KnowledgeOperationsHealthReviewItem struct {
	BookID                 string         `json:"book_id"`
	Title                  string         `json:"title"`
	ReleaseID              string         `json:"release_id,omitempty"`
	Status                 string         `json:"status"`
	Priority               int            `json:"priority"`
	PriorityLabel          string         `json:"priority_label"`
	NextOperatorAction     string         `json:"next_operator_action"`
	ConsumerReviewRequired bool           `json:"consumer_review_required"`
	ServingAllowed         bool           `json:"serving_allowed"`
	ClaimCount             int            `json:"claim_count,omitempty"`
	CitationCount          int            `json:"citation_count,omitempty"`
	RiskCounts             map[string]int `json:"risk_counts,omitempty"`
	Reasons                []string       `json:"reasons,omitempty"`
}

type KnowledgeOperationsFailureSummary struct {
	Code                    string   `json:"code,omitempty"`
	Explanation             string   `json:"explanation,omitempty"`
	SafeReplayAction        string   `json:"safe_replay_action,omitempty"`
	DangerousActionsBlocked []string `json:"dangerous_actions_blocked"`
}

type KnowledgeOperationsReplayRequest struct {
	BookID          string `json:"book_id"`
	Action          string `json:"action"`
	Confirm         bool   `json:"confirm,omitempty"`
	DryRun          bool   `json:"dry_run,omitempty"`
	Model           string `json:"model,omitempty"`
	MaxContextChars int    `json:"max_context_chars,omitempty"`
}

type KnowledgeOperationsReplayResult struct {
	BookID     string `json:"book_id"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	Mutated    bool   `json:"mutated"`
	NextAction string `json:"next_action,omitempty"`
	Error      string `json:"error,omitempty"`
}

func BuildKnowledgeOperationsConsole(store *BookKnowledgeStore, limit int) (*KnowledgeOperationsConsole, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	pipeline, err := BuildKnowledgePipelineDashboard(store, limit)
	if err != nil {
		return nil, err
	}
	healthReadiness, err := BuildHealthEvidenceReadiness(store, limit)
	if err != nil {
		return nil, err
	}
	healthByBook := make(map[string]HealthEvidenceReadinessItem, len(healthReadiness.Items))
	for _, item := range healthReadiness.Items {
		healthByBook[item.BookID] = item
	}
	console := &KnowledgeOperationsConsole{
		SchemaVersion: KnowledgeOperationsSchemaVersion,
		Items:         make([]KnowledgeOperationsItem, 0, len(pipeline.Items)),
	}
	for _, pipelineItem := range pipeline.Items {
		item := KnowledgeOperationsItem{
			BookID:        pipelineItem.BookID,
			Title:         pipelineItem.Title,
			SourceType:    pipelineItem.SourceType,
			SourceAccount: pipelineItem.SourceAccount,
			ContentHash:   pipelineItem.ContentHash,
			PipelineStage: pipelineItem.Stage,
			NextAction:    pipelineItem.NextAction,
			ReleaseID:     pipelineItem.LastPublishedReleaseID,
			Failure: KnowledgeOperationsFailureSummary{
				Code:                    pipelineItem.PublicErrorCode,
				DangerousActionsBlocked: knowledgeOperationsDangerousActions(),
			},
		}
		item.Failure = explainKnowledgeOperationsFailure(item.Failure, pipelineItem.NextAction)
		if quality, qualityErr := store.LoadBookQualityReport(pipelineItem.BookID); qualityErr == nil {
			item.QualityDecision = quality.Decision
			item.UsagePolicy = quality.UsagePolicy
		}
		if releaseID := strings.TrimSpace(item.ReleaseID); releaseID != "" {
			if release, releaseErr := store.LoadKnowledgeRelease(releaseID); releaseErr == nil {
				item.Title = firstNonEmpty(release.Book.Title, item.Title)
				item.UsagePolicy = firstNonEmpty(item.UsagePolicy, release.UsagePolicy)
			}
		}
		if healthItem, ok := healthByBook[pipelineItem.BookID]; ok {
			item.Health = KnowledgeOperationsHealthSummary{
				Status:         healthItem.Status,
				NextAction:     healthItem.NextAction,
				ServingAllowed: false,
				Reasons:        append([]string(nil), healthItem.Reasons...),
			}
			releaseID := firstNonEmpty(healthItem.EvidenceReleaseID, item.ReleaseID)
			if releaseID != "" {
				item.Health = summarizeKnowledgeOperationsHealthEvidence(store, releaseID, item.Health)
			}
		}
		console.Items = append(console.Items, item)
	}
	console.Summary.Total = len(console.Items)
	console.Summary.NeedsAnalysis = pipeline.Summary.NeedsAnalysis
	console.Summary.NeedsQuality = pipeline.Summary.NeedsQuality
	console.Summary.ReadyToPublish = pipeline.Summary.ReadyToPublish
	console.Summary.Published = pipeline.Summary.Published
	console.Summary.Blocked = pipeline.Summary.Blocked
	console.Summary.HealthReadyToPublish = healthReadiness.Totals.ReadyToPublish
	console.Summary.HealthPublished = healthReadiness.Totals.Published
	console.Summary.HealthBlocked = healthReadiness.Totals.Blocked
	console.HealthReviewQueue = buildKnowledgeOperationsHealthReviewQueue(console.Items)
	return console, nil
}

func knowledgeOperationsDangerousActions() []string {
	return []string{"publish", "health_serving_promote"}
}

func explainKnowledgeOperationsFailure(summary KnowledgeOperationsFailureSummary, nextAction string) KnowledgeOperationsFailureSummary {
	switch strings.TrimSpace(summary.Code) {
	case "quality_stale":
		summary.Explanation = "quality report is stale for the current package content"
		summary.SafeReplayAction = "evaluate_quality"
	case "quality_missing_decision":
		summary.Explanation = "quality report is missing a deterministic decision"
		summary.SafeReplayAction = "evaluate_quality"
	case "":
		switch nextAction {
		case "needs_analysis":
			summary.Explanation = "analysis is missing or stale"
			summary.SafeReplayAction = "analyze"
		case "needs_quality":
			summary.Explanation = "analysis exists and needs deterministic quality evaluation"
			summary.SafeReplayAction = "evaluate_quality"
		}
	default:
		summary.Explanation = "pipeline is blocked by " + summary.Code
	}
	return summary
}

func RunKnowledgeOperationsReplay(
	ctx context.Context,
	store *BookKnowledgeStore,
	generator BookAnalysisGenerator,
	request KnowledgeOperationsReplayRequest,
) (*KnowledgeOperationsReplayResult, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if generator == nil {
		generator = GenerateBookAnalysisManifest
	}
	bookID := strings.TrimSpace(request.BookID)
	action := strings.TrimSpace(request.Action)
	result := &KnowledgeOperationsReplayResult{BookID: bookID, Action: action}
	if bookID == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	switch action {
	case "analyze", "evaluate_quality":
	default:
		return nil, fmt.Errorf("replay action %q is not allowed", action)
	}
	if _, err := store.LoadPackage(bookID); err != nil {
		return nil, err
	}
	if request.DryRun || !request.Confirm {
		result.Status = "planned"
		result.NextAction = action
		return result, nil
	}
	switch action {
	case "analyze":
		manifest, err := generator(ctx, store, BookAnalysisGenerateRequest{
			BookID:          bookID,
			Model:           request.Model,
			MaxContextChars: request.MaxContextChars,
		})
		if err != nil {
			result.Status = "failed"
			result.Error = trimRunes(err.Error(), 500)
			return result, nil
		}
		if manifest != nil {
			if err := store.SaveAnalysisManifest(*manifest); err != nil {
				result.Status = "failed"
				result.Error = trimRunes(err.Error(), 500)
				return result, nil
			}
		}
		if _, err := EvaluateBookAnalysisQuality(store, bookID); err != nil {
			result.Status = "failed"
			result.Error = trimRunes(err.Error(), 500)
			return result, nil
		}
	case "evaluate_quality":
		if _, err := EvaluateBookAnalysisQuality(store, bookID); err != nil {
			result.Status = "failed"
			result.Error = trimRunes(err.Error(), 500)
			return result, nil
		}
	}
	result.Status = "succeeded"
	result.Mutated = true
	if after, err := BuildKnowledgePipelineDashboard(store, 500); err == nil {
		result.NextAction = findKnowledgePipelineDashboardAction(after.Items, bookID)
	}
	return result, nil
}

func summarizeKnowledgeOperationsHealthEvidence(store *BookKnowledgeStore, releaseID string, summary KnowledgeOperationsHealthSummary) KnowledgeOperationsHealthSummary {
	pkg, err := BuildHealthEvidencePackage(store, releaseID)
	if err != nil {
		return summary
	}
	summary.ClaimCount = len(pkg.Evidence)
	seenCitations := map[string]struct{}{}
	riskCounts := map[string]int{}
	for _, evidence := range pkg.Evidence {
		if evidence.RiskLevel != "" {
			riskCounts[evidence.RiskLevel]++
		}
		for _, citationID := range evidence.CitationIDs {
			if citationID != "" {
				seenCitations[citationID] = struct{}{}
			}
		}
	}
	summary.CitationCount = len(seenCitations)
	if len(riskCounts) > 0 {
		summary.RiskCounts = riskCounts
	}
	return summary
}

func buildKnowledgeOperationsHealthReviewQueue(items []KnowledgeOperationsItem) []KnowledgeOperationsHealthReviewItem {
	queue := make([]KnowledgeOperationsHealthReviewItem, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Health.Status) == "" {
			continue
		}
		reviewItem := knowledgeOperationsHealthReviewItemFromOperationsItem(item)
		if reviewItem.Priority <= 0 {
			continue
		}
		queue = append(queue, reviewItem)
	}
	sort.SliceStable(queue, func(i, j int) bool {
		if queue[i].Priority != queue[j].Priority {
			return queue[i].Priority > queue[j].Priority
		}
		if queue[i].Title != queue[j].Title {
			return queue[i].Title < queue[j].Title
		}
		return queue[i].BookID < queue[j].BookID
	})
	return queue
}

func knowledgeOperationsHealthReviewItemFromOperationsItem(item KnowledgeOperationsItem) KnowledgeOperationsHealthReviewItem {
	priority, priorityLabel, nextOperatorAction, consumerReviewRequired := knowledgeOperationsHealthReviewPriority(item.Health.Status)
	reviewItem := KnowledgeOperationsHealthReviewItem{
		BookID:                 item.BookID,
		Title:                  item.Title,
		ReleaseID:              item.ReleaseID,
		Status:                 item.Health.Status,
		Priority:               priority,
		PriorityLabel:          priorityLabel,
		NextOperatorAction:     nextOperatorAction,
		ConsumerReviewRequired: consumerReviewRequired,
		ServingAllowed:         false,
		ClaimCount:             item.Health.ClaimCount,
		CitationCount:          item.Health.CitationCount,
		Reasons:                append([]string(nil), item.Health.Reasons...),
	}
	if len(item.Health.RiskCounts) > 0 {
		reviewItem.RiskCounts = make(map[string]int, len(item.Health.RiskCounts))
		for riskLevel, count := range item.Health.RiskCounts {
			reviewItem.RiskCounts[riskLevel] = count
		}
	}
	if reviewItem.ConsumerReviewRequired && !knowledgeOperationsContainsString(reviewItem.Reasons, "consumer_review_required") {
		reviewItem.Reasons = append(reviewItem.Reasons, "consumer_review_required")
	}
	return reviewItem
}

func knowledgeOperationsHealthReviewPriority(status string) (int, string, string, bool) {
	switch status {
	case HealthEvidenceReadinessPublished:
		return 90, "review_next", "send_to_health_review", true
	case HealthEvidenceReadinessReadyToPublish:
		return 80, "prepare_review", "prepare_health_release", false
	case HealthEvidenceReadinessPolicyBlocked:
		return 70, "blocked", "inspect_policy_block", false
	case HealthEvidenceReadinessQualityBlocked:
		return 65, "blocked", "inspect_quality_block", false
	case HealthEvidenceReadinessNeedsQuality:
		return 50, "needs_work", "evaluate_quality", false
	case HealthEvidenceReadinessNeedsAnalysis:
		return 40, "needs_work", "run_analysis", false
	default:
		return 10, "monitor", "inspect_status", false
	}
}

func knowledgeOperationsContainsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
