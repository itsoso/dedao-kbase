package app

import (
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

func ParseHealthEvidenceSearchQuery(values url.Values) HealthEvidenceSearchQuery {
	limit, _ := strconv.Atoi(values.Get("limit"))
	return HealthEvidenceSearchQuery{
		Query: strings.TrimSpace(values.Get("q")),
		Tag:   strings.TrimSpace(values.Get("tag")),
		Limit: limit,
	}
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
