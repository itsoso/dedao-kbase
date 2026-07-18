package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const HealthEvidenceSchemaVersion = "health_evidence.v1"
const HealthEvidenceSearchSchemaVersion = "health_evidence_search.v1"
const HealthEvidenceReadinessSchemaVersion = "health_evidence_readiness.v1"
const HealthEvidenceAnalysisBatchSchemaVersion = "health_evidence_analysis_batch.v1"

const (
	HealthEvidenceReadinessPublished      = "published"
	HealthEvidenceReadinessReadyToPublish = "ready_to_publish"
	HealthEvidenceReadinessNeedsAnalysis  = "needs_analysis"
	HealthEvidenceReadinessNeedsQuality   = "needs_quality"
	HealthEvidenceReadinessPolicyBlocked  = "policy_blocked"
	HealthEvidenceReadinessQualityBlocked = "quality_blocked"
)

type HealthEvidencePackage struct {
	SchemaVersion string                  `json:"schema_version"`
	ReleaseID     string                  `json:"release_id"`
	BookID        string                  `json:"book_id"`
	ContentHash   string                  `json:"content_hash"`
	UsagePolicy   string                  `json:"usage_policy"`
	Title         string                  `json:"title"`
	Summary       string                  `json:"summary,omitempty"`
	Source        HealthEvidenceSource    `json:"source"`
	Freshness     HealthEvidenceFreshness `json:"freshness"`
	Evidence      []HealthEvidenceClaim   `json:"evidence"`
	CreatedAt     string                  `json:"created_at"`
}

type HealthEvidenceSource struct {
	URI         string `json:"uri,omitempty"`
	Type        string `json:"type,omitempty"`
	Account     string `json:"account,omitempty"`
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type HealthEvidenceFreshness struct {
	PublishedAt string `json:"published_at,omitempty"`
	AgeDays     int    `json:"age_days,omitempty"`
	Stale       bool   `json:"stale"`
}

type HealthEvidenceClaim struct {
	EvidenceID    string                   `json:"evidence_id"`
	ReleaseID     string                   `json:"release_id"`
	ClaimID       string                   `json:"claim_id"`
	Statement     string                   `json:"statement"`
	Confidence    float64                  `json:"confidence"`
	RiskLevel     string                   `json:"risk_level"`
	EvidenceLevel string                   `json:"evidence_level"`
	Scope         []string                 `json:"scope,omitempty"`
	Tags          HealthEvidenceTags       `json:"tags"`
	CitationIDs   []string                 `json:"citation_ids"`
	Citations     []HealthEvidenceCitation `json:"citations,omitempty"`
	SafetyFlags   []string                 `json:"safety_flags,omitempty"`
	URL           string                   `json:"url"`
}

type HealthEvidenceTags struct {
	Conditions    []string `json:"conditions,omitempty"`
	Interventions []string `json:"interventions,omitempty"`
	Metrics       []string `json:"metrics,omitempty"`
	Populations   []string `json:"populations,omitempty"`
	Risks         []string `json:"risks,omitempty"`
	EvidenceLevel []string `json:"evidence_levels,omitempty"`
}

type HealthEvidenceCitation struct {
	CitationID    string `json:"citation_id"`
	ChunkID       string `json:"chunk_id,omitempty"`
	ChapterID     string `json:"chapter_id,omitempty"`
	SourceURI     string `json:"source_uri,omitempty"`
	SourceType    string `json:"source_type,omitempty"`
	SourceAccount string `json:"source_account,omitempty"`
	PublishedAt   string `json:"published_at,omitempty"`
}

type HealthEvidenceSearchQuery struct {
	Query string
	Tag   string
	Limit int
}

type HealthEvidenceSearchPage struct {
	SchemaVersion string                `json:"schema_version"`
	Items         []HealthEvidenceClaim `json:"items"`
}

type HealthEvidenceReadinessReport struct {
	SchemaVersion string                        `json:"schema_version"`
	Totals        HealthEvidenceReadinessTotals `json:"totals"`
	Items         []HealthEvidenceReadinessItem `json:"items"`
}

type HealthEvidenceReadinessTotals struct {
	Total          int `json:"total"`
	Published      int `json:"published"`
	ReadyToPublish int `json:"ready_to_publish"`
	NeedsAnalysis  int `json:"needs_analysis"`
	NeedsQuality   int `json:"needs_quality"`
	Blocked        int `json:"blocked"`
}

type HealthEvidenceReadinessItem struct {
	BookID            string   `json:"book_id"`
	Title             string   `json:"title"`
	ContentHash       string   `json:"content_hash,omitempty"`
	Status            string   `json:"status"`
	UsagePolicy       string   `json:"usage_policy,omitempty"`
	LatestReleaseID   string   `json:"latest_release_id,omitempty"`
	EvidenceReleaseID string   `json:"evidence_release_id,omitempty"`
	NextAction        string   `json:"next_action,omitempty"`
	Reasons           []string `json:"reasons,omitempty"`
	UpdatedAt         string   `json:"updated_at,omitempty"`
}

type HealthEvidenceAnalysisBatchRequest struct {
	Limit           int    `json:"limit,omitempty"`
	Model           string `json:"model,omitempty"`
	MaxContextChars int    `json:"max_context_chars,omitempty"`
	DryRun          bool   `json:"dry_run,omitempty"`
	SummaryOnly     bool   `json:"summary_only,omitempty"`
}

type HealthEvidenceAnalysisBatchResult struct {
	SchemaVersion           string                            `json:"schema_version"`
	DryRun                  bool                              `json:"dry_run"`
	SummaryOnly             bool                              `json:"summary_only,omitempty"`
	Eligible                int                               `json:"eligible"`
	Skipped                 int                               `json:"skipped"`
	SkippedByStatus         map[string]int                    `json:"skipped_by_status,omitempty"`
	Scanned                 int                               `json:"scanned"`
	HasWork                 bool                              `json:"has_work"`
	QueueState              string                            `json:"queue_state"`
	RecommendedAction       string                            `json:"recommended_action"`
	ReadyToPublish          int                               `json:"ready_to_publish"`
	Published               int                               `json:"published"`
	Blocked                 int                               `json:"blocked"`
	RequestedLimit          int                               `json:"requested_limit"`
	NextBatchSize           int                               `json:"next_batch_size"`
	RemainingAfterNextBatch int                               `json:"remaining_after_next_batch"`
	HasMoreAfterNextBatch   bool                              `json:"has_more_after_next_batch"`
	EstimatedBatches        int                               `json:"estimated_batches"`
	LimitReached            bool                              `json:"limit_reached"`
	Processed               int                               `json:"processed"`
	Succeeded               int                               `json:"succeeded"`
	Failed                  int                               `json:"failed"`
	Items                   []HealthEvidenceAnalysisBatchItem `json:"items"`
}

type HealthEvidenceAnalysisBatchItem struct {
	BookID      string `json:"book_id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	NextStatus  string `json:"next_status,omitempty"`
	NextAction  string `json:"next_action,omitempty"`
	Error       string `json:"error,omitempty"`
	AnalysisID  string `json:"analysis_id,omitempty"`
	Quality     string `json:"quality,omitempty"`
	UsagePolicy string `json:"usage_policy,omitempty"`
}

func BuildHealthEvidencePackage(store *BookKnowledgeStore, releaseID string) (HealthEvidencePackage, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return HealthEvidencePackage{}, fmt.Errorf("release_id is required")
	}
	release, err := store.LoadKnowledgeRelease(releaseID)
	if err != nil {
		return HealthEvidencePackage{}, err
	}
	if release.UsagePolicy != BookUsageEvidenceOnly {
		return HealthEvidencePackage{}, fmt.Errorf("release %s is not available for health evidence", release.ReleaseID)
	}
	citationsByID := make(map[string]BookKnowledgeCitation)
	for _, citation := range release.Citations {
		citationsByID[citation.CitationID] = citation
	}
	pkg := HealthEvidencePackage{
		SchemaVersion: HealthEvidenceSchemaVersion,
		ReleaseID:     release.ReleaseID,
		BookID:        release.BookID,
		ContentHash:   release.ContentHash,
		UsagePolicy:   release.UsagePolicy,
		Title:         release.Book.Title,
		Source: HealthEvidenceSource{
			URI:         release.Book.SourceHTML,
			Type:        release.Book.SourceType,
			Account:     release.Book.SourceAccount,
			Author:      release.Book.Author,
			PublishedAt: release.Book.PublishedAt,
		},
		Freshness: healthEvidenceFreshness(release.Book.PublishedAt, release.CreatedAt),
		CreatedAt: release.CreatedAt,
	}
	if release.Analysis != nil {
		pkg.Summary = release.Analysis.Summary
		for _, claim := range release.Analysis.Claims {
			evidence := healthEvidenceClaimFromAnalysis(release, claim, citationsByID)
			pkg.Evidence = append(pkg.Evidence, evidence)
		}
	}
	return pkg, nil
}

func SearchHealthEvidence(store *BookKnowledgeStore, query HealthEvidenceSearchQuery) (HealthEvidenceSearchPage, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if query.Limit <= 0 || query.Limit > 100 {
		query.Limit = 20
	}
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	tag := strings.ToLower(strings.TrimSpace(query.Tag))
	manifest, err := store.loadKnowledgeReleaseManifest()
	if err != nil {
		return HealthEvidenceSearchPage{}, err
	}
	page := HealthEvidenceSearchPage{
		SchemaVersion: HealthEvidenceSearchSchemaVersion,
		Items:         []HealthEvidenceClaim{},
	}
	for _, record := range manifest.Releases {
		if record.UsagePolicy != BookUsageEvidenceOnly {
			continue
		}
		pkg, err := BuildHealthEvidencePackage(store, record.ReleaseID)
		if err != nil {
			return HealthEvidenceSearchPage{}, err
		}
		for _, evidence := range pkg.Evidence {
			if !healthEvidenceMatchesQuery(evidence, needle) || !healthEvidenceMatchesTag(evidence, tag) {
				continue
			}
			page.Items = append(page.Items, evidence)
			if len(page.Items) >= query.Limit {
				return page, nil
			}
		}
	}
	return page, nil
}

func BuildHealthEvidenceReadiness(store *BookKnowledgeStore, limit int) (HealthEvidenceReadinessReport, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	books, err := store.ListBooks()
	if err != nil {
		return HealthEvidenceReadinessReport{}, err
	}
	releaseManifest, err := store.loadKnowledgeReleaseManifest()
	if err != nil {
		return HealthEvidenceReadinessReport{}, err
	}
	latestByBook := make(map[string]KnowledgeReleaseRecord)
	evidenceByBook := make(map[string]KnowledgeReleaseRecord)
	for _, record := range releaseManifest.Releases {
		latestByBook[record.BookID] = record
		if record.UsagePolicy == BookUsageEvidenceOnly {
			evidenceByBook[record.BookID] = record
		}
	}
	report := HealthEvidenceReadinessReport{
		SchemaVersion: HealthEvidenceReadinessSchemaVersion,
		Items:         []HealthEvidenceReadinessItem{},
	}
	for _, book := range books {
		if len(report.Items) >= limit {
			break
		}
		item := buildHealthEvidenceReadinessItem(store, book, latestByBook[book.BookID], evidenceByBook[book.BookID])
		report.Items = append(report.Items, item)
		report.Totals.Total++
		switch item.Status {
		case HealthEvidenceReadinessPublished:
			report.Totals.Published++
		case HealthEvidenceReadinessReadyToPublish:
			report.Totals.ReadyToPublish++
		case HealthEvidenceReadinessNeedsAnalysis:
			report.Totals.NeedsAnalysis++
		case HealthEvidenceReadinessNeedsQuality:
			report.Totals.NeedsQuality++
		case HealthEvidenceReadinessPolicyBlocked, HealthEvidenceReadinessQualityBlocked:
			report.Totals.Blocked++
		}
	}
	return report, nil
}

func RunHealthEvidenceAnalysisBatch(
	ctx context.Context,
	store *BookKnowledgeStore,
	generator BookAnalysisGenerator,
	request HealthEvidenceAnalysisBatchRequest,
) (HealthEvidenceAnalysisBatchResult, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if generator == nil {
		generator = GenerateBookAnalysisManifest
	}
	if request.Limit <= 0 || request.Limit > 20 {
		request.Limit = 5
	}
	if request.MaxContextChars < 0 {
		request.MaxContextChars = 0
	}
	readiness, err := BuildHealthEvidenceReadiness(store, 500)
	if err != nil {
		return HealthEvidenceAnalysisBatchResult{}, err
	}
	result := HealthEvidenceAnalysisBatchResult{
		SchemaVersion:   HealthEvidenceAnalysisBatchSchemaVersion,
		DryRun:          request.DryRun || request.SummaryOnly,
		SummaryOnly:     request.SummaryOnly,
		RequestedLimit:  request.Limit,
		SkippedByStatus: map[string]int{},
		Items:           []HealthEvidenceAnalysisBatchItem{},
	}
	result.Scanned = len(readiness.Items)
	for _, item := range readiness.Items {
		if item.Status == HealthEvidenceReadinessNeedsAnalysis {
			result.Eligible++
			continue
		}
		result.Skipped++
		result.SkippedByStatus[item.Status]++
		switch item.Status {
		case HealthEvidenceReadinessReadyToPublish:
			result.ReadyToPublish++
		case HealthEvidenceReadinessPublished:
			result.Published++
		case HealthEvidenceReadinessPolicyBlocked, HealthEvidenceReadinessQualityBlocked:
			result.Blocked++
		}
	}
	if len(result.SkippedByStatus) == 0 {
		result.SkippedByStatus = nil
	}
	result.NextBatchSize = result.Eligible
	if result.NextBatchSize > request.Limit {
		result.NextBatchSize = request.Limit
	}
	result.RemainingAfterNextBatch = result.Eligible - result.NextBatchSize
	result.HasMoreAfterNextBatch = result.RemainingAfterNextBatch > 0
	if result.Eligible > 0 {
		result.EstimatedBatches = (result.Eligible + request.Limit - 1) / request.Limit
	}
	result.HasWork = result.Eligible > 0
	result.QueueState, result.RecommendedAction = healthEvidenceAnalysisQueueDecision(result)
	result.LimitReached = result.Eligible > request.Limit
	if request.SummaryOnly {
		return result, nil
	}
	for _, item := range readiness.Items {
		if result.Processed >= request.Limit {
			break
		}
		if item.Status != HealthEvidenceReadinessNeedsAnalysis {
			continue
		}
		result.Processed++
		batchItem := HealthEvidenceAnalysisBatchItem{
			BookID:     item.BookID,
			Title:      item.Title,
			Status:     "processing",
			NextAction: item.NextAction,
		}
		if result.DryRun {
			batchItem.Status = "preview"
			batchItem.NextStatus = item.Status
			result.Items = append(result.Items, batchItem)
			continue
		}
		manifest, analysisErr := generator(ctx, store, BookAnalysisGenerateRequest{
			BookID:          item.BookID,
			Model:           request.Model,
			MaxContextChars: request.MaxContextChars,
		})
		if analysisErr == nil && manifest != nil {
			if saveErr := store.SaveAnalysisManifest(*manifest); saveErr != nil {
				analysisErr = saveErr
			}
		}
		if analysisErr != nil {
			batchItem.Status = "failed"
			batchItem.Error = trimRunes(analysisErr.Error(), 500)
			result.Failed++
			result.Items = append(result.Items, batchItem)
			continue
		}
		quality, qualityErr := EvaluateBookAnalysisQuality(store, item.BookID)
		if qualityErr != nil {
			batchItem.Status = "failed"
			batchItem.Error = trimRunes(qualityErr.Error(), 500)
			result.Failed++
			result.Items = append(result.Items, batchItem)
			continue
		}
		nextReadiness, nextErr := BuildHealthEvidenceReadiness(store, 500)
		nextStatus := ""
		nextAction := ""
		if nextErr == nil {
			for _, next := range nextReadiness.Items {
				if next.BookID == item.BookID {
					nextStatus = next.Status
					nextAction = next.NextAction
					break
				}
			}
		}
		batchItem.Status = "succeeded"
		batchItem.NextStatus = nextStatus
		batchItem.NextAction = nextAction
		batchItem.AnalysisID = item.BookID + ":" + item.ContentHash
		batchItem.Quality = quality.Decision
		batchItem.UsagePolicy = quality.UsagePolicy
		result.Succeeded++
		result.Items = append(result.Items, batchItem)
	}
	return result, nil
}

func healthEvidenceAnalysisQueueDecision(result HealthEvidenceAnalysisBatchResult) (string, string) {
	if result.Eligible > 0 {
		return "ready", "run_analysis"
	}
	if result.Scanned == 0 {
		return "empty", "idle"
	}
	for status := range result.SkippedByStatus {
		if status != HealthEvidenceReadinessPublished && status != HealthEvidenceReadinessReadyToPublish {
			return "blocked", "review_blocked"
		}
	}
	return "complete", "idle"
}

func ParseHealthEvidenceSearchQuery(values url.Values) HealthEvidenceSearchQuery {
	limit, _ := strconv.Atoi(values.Get("limit"))
	return HealthEvidenceSearchQuery{
		Query: strings.TrimSpace(values.Get("q")),
		Tag:   strings.TrimSpace(values.Get("tag")),
		Limit: limit,
	}
}

func ParseHealthEvidenceReadinessLimit(values url.Values) int {
	limit, _ := strconv.Atoi(values.Get("limit"))
	return limit
}

func ValidateHealthEvidenceContract(raw []byte) error {
	var pkg HealthEvidencePackage
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return err
	}
	if pkg.SchemaVersion != HealthEvidenceSchemaVersion {
		return fmt.Errorf("schema_version must be %q", HealthEvidenceSchemaVersion)
	}
	if err := requireContractFields(map[string]string{
		"release_id":   pkg.ReleaseID,
		"book_id":      pkg.BookID,
		"content_hash": pkg.ContentHash,
		"usage_policy": pkg.UsagePolicy,
		"title":        pkg.Title,
		"created_at":   pkg.CreatedAt,
	}); err != nil {
		return err
	}
	if pkg.UsagePolicy != BookUsageEvidenceOnly {
		return fmt.Errorf("usage_policy must be %q", BookUsageEvidenceOnly)
	}
	for index, evidence := range pkg.Evidence {
		if err := requireContractFields(map[string]string{
			"evidence_id": evidence.EvidenceID,
			"release_id":  evidence.ReleaseID,
			"claim_id":    evidence.ClaimID,
			"statement":   evidence.Statement,
			"risk_level":  evidence.RiskLevel,
			"url":         evidence.URL,
		}); err != nil {
			return fmt.Errorf("evidence[%d]: %w", index, err)
		}
	}
	return nil
}

func healthEvidenceClaimFromAnalysis(release *KnowledgeRelease, claim BookAnalysisClaim, citationsByID map[string]BookKnowledgeCitation) HealthEvidenceClaim {
	citationIDs := uniqueNonEmptyStrings(claim.CitationIDs)
	evidenceLevel := "source_claim"
	citations := make([]HealthEvidenceCitation, 0, len(citationIDs))
	for _, citationID := range citationIDs {
		citation, ok := citationsByID[citationID]
		if !ok {
			continue
		}
		citations = append(citations, HealthEvidenceCitation{
			CitationID:    citation.CitationID,
			ChunkID:       citation.ChunkID,
			ChapterID:     citation.ChapterID,
			SourceURI:     firstNonEmpty(citation.SourceHTML, release.Book.SourceHTML),
			SourceType:    firstNonEmpty(citation.SourceType, release.Book.SourceType),
			SourceAccount: firstNonEmpty(citation.SourceAccount, release.Book.SourceAccount),
			PublishedAt:   firstNonEmpty(citation.PublishedAt, release.Book.PublishedAt),
		})
	}
	tags := inferHealthEvidenceTags(claim)
	tags.EvidenceLevel = uniqueNonEmptyStrings(append(tags.EvidenceLevel, evidenceLevel))
	flags := inferHealthEvidenceSafetyFlags(claim, tags, citationIDs)
	return HealthEvidenceClaim{
		EvidenceID:    release.ReleaseID + ":" + claim.ID,
		ReleaseID:     release.ReleaseID,
		ClaimID:       claim.ID,
		Statement:     strings.TrimSpace(claim.Statement),
		Confidence:    claim.Confidence,
		RiskLevel:     strings.TrimSpace(claim.RiskLevel),
		EvidenceLevel: evidenceLevel,
		Scope:         uniqueNonEmptyStrings(claim.Scope),
		Tags:          tags,
		CitationIDs:   citationIDs,
		Citations:     citations,
		SafetyFlags:   flags,
		URL:           "/api/consumers/health/evidence/" + url.PathEscape(release.ReleaseID),
	}
}

func buildHealthEvidenceReadinessItem(store *BookKnowledgeStore, book BookKnowledgeBook, latestRelease, evidenceRelease KnowledgeReleaseRecord) HealthEvidenceReadinessItem {
	item := HealthEvidenceReadinessItem{
		BookID:      book.BookID,
		Title:       firstNonEmpty(book.Title, book.BookID),
		ContentHash: book.ContentHash,
		UpdatedAt:   book.UpdatedAt,
	}
	if latestRelease.ReleaseID != "" {
		item.LatestReleaseID = latestRelease.ReleaseID
	}
	if evidenceRelease.ReleaseID != "" {
		item.EvidenceReleaseID = evidenceRelease.ReleaseID
	}
	if evidenceRelease.ReleaseID != "" && evidenceRelease.ContentHash == book.ContentHash {
		item.Status = HealthEvidenceReadinessPublished
		item.UsagePolicy = BookUsageEvidenceOnly
		return item
	}
	analysis, err := store.LoadAnalysisManifest(book.BookID)
	if err != nil || analysis.Status != BookAnalysisReady || analysis.Payload == nil || analysis.ContentHash != book.ContentHash {
		item.Status = HealthEvidenceReadinessNeedsAnalysis
		item.NextAction = "analyze"
		item.Reasons = []string{"analysis_missing_or_stale"}
		return item
	}
	quality, err := store.LoadBookQualityReport(book.BookID)
	if err != nil || quality.ContentHash != book.ContentHash {
		item.Status = HealthEvidenceReadinessNeedsQuality
		item.NextAction = "evaluate_quality"
		item.Reasons = []string{"quality_missing_or_stale"}
		return item
	}
	item.UsagePolicy = quality.UsagePolicy
	if quality.Decision != BookQualityPass {
		item.Status = HealthEvidenceReadinessQualityBlocked
		item.NextAction = "fix_quality"
		item.Reasons = []string{"quality_not_passed"}
		return item
	}
	if quality.UsagePolicy != BookUsageEvidenceOnly {
		item.Status = HealthEvidenceReadinessPolicyBlocked
		item.NextAction = "review_policy"
		item.Reasons = []string{"usage_policy_not_evidence_only"}
		return item
	}
	item.Status = HealthEvidenceReadinessReadyToPublish
	item.NextAction = "publish"
	return item
}

func inferHealthEvidenceTags(claim BookAnalysisClaim) HealthEvidenceTags {
	text := strings.Join(append([]string{claim.Statement, claim.RiskLevel}, claim.Scope...), " ")
	return HealthEvidenceTags{
		Conditions:    inferKeywordTags(text, []string{"高血压", "血压", "糖尿病", "睡眠", "慢性疲劳", "HIV", "疫苗", "肥胖", "脂肪肝", "肾结石", "甲状腺", "鼻炎"}),
		Interventions: inferKeywordTags(text, []string{"运动", "饮食", "药物", "疫苗", "睡眠", "检查", "监测", "复查", "训练"}),
		Metrics:       inferKeywordTags(text, []string{"血压", "体重", "腰围", "BMI", "CRP", "eGFR", "血糖", "心率"}),
		Populations:   inferKeywordTags(text, []string{"成人", "儿童", "老人", "孕妇", "慢病"}),
		Risks:         inferKeywordTags(text, []string{"高风险", "禁忌", "副作用", "并发症", "医疗决策"}),
	}
}

func inferHealthEvidenceSafetyFlags(claim BookAnalysisClaim, tags HealthEvidenceTags, citationIDs []string) []string {
	flags := []string{}
	if strings.EqualFold(strings.TrimSpace(claim.RiskLevel), "high") {
		flags = append(flags, "high_risk")
	}
	if len(citationIDs) == 0 {
		flags = append(flags, "low_citation")
	}
	if len(tags.Conditions)+len(tags.Interventions)+len(tags.Metrics) > 0 {
		flags = append(flags, "medical_evidence")
	}
	return uniqueNonEmptyStrings(flags)
}

func healthEvidenceFreshness(publishedAt, releaseCreatedAt string) HealthEvidenceFreshness {
	freshness := HealthEvidenceFreshness{PublishedAt: strings.TrimSpace(publishedAt)}
	published, err := time.Parse(time.RFC3339, freshness.PublishedAt)
	if err != nil {
		return freshness
	}
	reference, err := time.Parse(time.RFC3339, strings.TrimSpace(releaseCreatedAt))
	if err != nil {
		reference = time.Now().UTC()
	}
	if reference.Before(published) {
		return freshness
	}
	freshness.AgeDays = int(reference.Sub(published).Hours() / 24)
	freshness.Stale = freshness.AgeDays > 730
	return freshness
}

func inferKeywordTags(text string, candidates []string) []string {
	result := []string{}
	lower := strings.ToLower(text)
	for _, candidate := range candidates {
		if strings.Contains(lower, strings.ToLower(candidate)) {
			result = append(result, candidate)
		}
	}
	return uniqueNonEmptyStrings(result)
}

func healthEvidenceMatchesQuery(evidence HealthEvidenceClaim, needle string) bool {
	if needle == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		evidence.Statement,
		evidence.ClaimID,
		strings.Join(evidence.Scope, " "),
		strings.Join(evidence.Tags.Conditions, " "),
		strings.Join(evidence.Tags.Interventions, " "),
		strings.Join(evidence.Tags.Metrics, " "),
	}, " "))
	return strings.Contains(haystack, needle)
}

func healthEvidenceMatchesTag(evidence HealthEvidenceClaim, tag string) bool {
	if tag == "" {
		return true
	}
	allTags := append([]string{}, evidence.Tags.Conditions...)
	allTags = append(allTags, evidence.Tags.Interventions...)
	allTags = append(allTags, evidence.Tags.Metrics...)
	allTags = append(allTags, evidence.Tags.Populations...)
	allTags = append(allTags, evidence.Tags.Risks...)
	allTags = append(allTags, evidence.Tags.EvidenceLevel...)
	for _, value := range allTags {
		if strings.EqualFold(strings.TrimSpace(value), tag) || strings.Contains(strings.ToLower(value), tag) {
			return true
		}
	}
	return false
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
