package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	bookKnowledgeProjectCollectionSource        = "verification_report"
	bookKnowledgeProjectAuditStatusPending      = "pending_async_audit"
	bookKnowledgeProjectCollectionFileName      = "collection.json"
	bookKnowledgeProjectCollectionSchemaVersion = "1"
)

type BookKnowledgeProjectCollection struct {
	Version      string                               `json:"version"`
	CollectionID string                               `json:"collection_id"`
	ProjectID    string                               `json:"project_id"`
	Project      BookKnowledgeProject                 `json:"project"`
	Source       string                               `json:"source"`
	GeneratedAt  string                               `json:"generated_at"`
	AutonomyMode string                               `json:"autonomy_mode"`
	HumanLoop    string                               `json:"human_loop"`
	ItemCount    int                                  `json:"item_count"`
	AuditCount   int                                  `json:"audit_count"`
	Items        []BookKnowledgeProjectCollectionItem `json:"items"`
	AuditQueue   []BookKnowledgeProjectAuditItem      `json:"audit_queue"`
}

type BookKnowledgeProjectCollectionItem struct {
	ProjectID         string   `json:"project_id"`
	BookID            string   `json:"book_id"`
	BookTitle         string   `json:"book_title"`
	ChapterID         string   `json:"chapter_id,omitempty"`
	ChapterTitle      string   `json:"chapter_title,omitempty"`
	ClaimID           string   `json:"claim_id"`
	Title             string   `json:"title"`
	Summary           string   `json:"summary"`
	VerificationScore float64  `json:"verification_score"`
	RiskTier          string   `json:"risk_tier"`
	Decision          string   `json:"decision"`
	AllowedUses       []string `json:"allowed_uses,omitempty"`
	BlockedUses       []string `json:"blocked_uses,omitempty"`
	SourceHash        string   `json:"source_hash"`
}

type BookKnowledgeProjectAuditItem struct {
	AuditID      string   `json:"audit_id"`
	ProjectID    string   `json:"project_id"`
	BookID       string   `json:"book_id"`
	BookTitle    string   `json:"book_title"`
	ChapterID    string   `json:"chapter_id,omitempty"`
	ClaimID      string   `json:"claim_id"`
	Title        string   `json:"title"`
	RiskTier     string   `json:"risk_tier"`
	Decision     string   `json:"decision"`
	ReviewStatus string   `json:"review_status"`
	SampleReason string   `json:"sample_reason"`
	SourceHash   string   `json:"source_hash"`
	AllowedUses  []string `json:"allowed_uses,omitempty"`
	BlockedUses  []string `json:"blocked_uses,omitempty"`
	CreatedAt    string   `json:"created_at"`
}

type BookKnowledgeProjectAuditQueue struct {
	ProjectID    string                          `json:"project_id"`
	CollectionID string                          `json:"collection_id"`
	AuditItems   []BookKnowledgeProjectAuditItem `json:"audit_items"`
	Total        int                             `json:"total"`
	Limit        int                             `json:"limit"`
}

func (s *BookKnowledgeStore) ProjectDir(projectID string) string {
	return filepath.Join(s.root, "projects", sanitizeBookKnowledgeID(projectID))
}

func (s *BookKnowledgeStore) ProjectCollectionPath(projectID string) string {
	return filepath.Join(s.ProjectDir(projectID), bookKnowledgeProjectCollectionFileName)
}

func (s *BookKnowledgeStore) RefreshProjectCollection(projectID string, limit int) (*BookKnowledgeProjectCollection, error) {
	report, err := s.BuildProjectVerificationReport(projectID, limit)
	if err != nil {
		return nil, err
	}
	collection := buildProjectCollectionFromReport(report)
	if err := s.SaveProjectCollection(collection); err != nil {
		return nil, err
	}
	return collection, nil
}

func (s *BookKnowledgeStore) SaveProjectCollection(collection *BookKnowledgeProjectCollection) error {
	if collection == nil {
		return fmt.Errorf("project collection is required")
	}
	if strings.TrimSpace(collection.ProjectID) == "" {
		return fmt.Errorf("project_id is required")
	}
	if err := os.MkdirAll(s.ProjectDir(collection.ProjectID), os.ModePerm); err != nil {
		return err
	}
	return writeJSONFile(s.ProjectCollectionPath(collection.ProjectID), collection)
}

func (s *BookKnowledgeStore) LoadProjectCollection(projectID string) (*BookKnowledgeProjectCollection, error) {
	if _, ok := BookKnowledgeProjectByID(projectID); !ok {
		return nil, fmt.Errorf("unknown book knowledge project: %s", projectID)
	}
	var collection BookKnowledgeProjectCollection
	if err := readJSONFile(s.ProjectCollectionPath(projectID), &collection); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("project collection not found: %s", projectID)
		}
		return nil, err
	}
	if collection.Items == nil {
		collection.Items = []BookKnowledgeProjectCollectionItem{}
	}
	if collection.AuditQueue == nil {
		collection.AuditQueue = []BookKnowledgeProjectAuditItem{}
	}
	return &collection, nil
}

func (s *BookKnowledgeStore) LoadProjectAuditQueue(projectID string, limit int) (*BookKnowledgeProjectAuditQueue, error) {
	collection, err := s.LoadProjectCollection(projectID)
	if err != nil {
		return nil, err
	}
	limit = normalizeProjectLimit(limit)
	items := append([]BookKnowledgeProjectAuditItem(nil), collection.AuditQueue...)
	total := len(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return &BookKnowledgeProjectAuditQueue{
		ProjectID:    collection.ProjectID,
		CollectionID: collection.CollectionID,
		AuditItems:   items,
		Total:        total,
		Limit:        limit,
	}, nil
}

func buildProjectCollectionFromReport(report *BookKnowledgeProjectVerificationReport) *BookKnowledgeProjectCollection {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	items := make([]BookKnowledgeProjectCollectionItem, 0, len(report.Items))
	auditItems := []BookKnowledgeProjectAuditItem{}
	for _, verified := range report.Items {
		item := projectCollectionItemFromVerified(verified)
		items = append(items, item)
		if reason := auditSampleReason(verified); reason != "" {
			auditItems = append(auditItems, projectAuditItemFromVerified(verified, reason, now))
		}
	}
	collectionID := projectCollectionID(report.ProjectID, now, len(items))
	return &BookKnowledgeProjectCollection{
		Version:      bookKnowledgeProjectCollectionSchemaVersion,
		CollectionID: collectionID,
		ProjectID:    report.ProjectID,
		Project:      report.Project,
		Source:       bookKnowledgeProjectCollectionSource,
		GeneratedAt:  now,
		AutonomyMode: report.AutonomyMode,
		HumanLoop:    report.HumanLoop,
		ItemCount:    len(items),
		AuditCount:   len(auditItems),
		Items:        items,
		AuditQueue:   auditItems,
	}
}

func projectCollectionItemFromVerified(verified BookKnowledgeVerifiedItem) BookKnowledgeProjectCollectionItem {
	return BookKnowledgeProjectCollectionItem{
		ProjectID:         verified.ProjectID,
		BookID:            verified.BookID,
		BookTitle:         verified.BookTitle,
		ChapterID:         verified.ChapterID,
		ChapterTitle:      verified.ChapterTitle,
		ClaimID:           verified.ClaimID,
		Title:             verified.Title,
		Summary:           verified.Summary,
		VerificationScore: verified.VerificationScore,
		RiskTier:          verified.RiskTier,
		Decision:          verified.Decision,
		AllowedUses:       append([]string(nil), verified.AllowedUses...),
		BlockedUses:       append([]string(nil), verified.BlockedUses...),
		SourceHash:        verified.Provenance.SourceHash,
	}
}

func projectAuditItemFromVerified(verified BookKnowledgeVerifiedItem, reason, now string) BookKnowledgeProjectAuditItem {
	return BookKnowledgeProjectAuditItem{
		AuditID:      projectAuditID(verified.ProjectID, verified.ClaimID, verified.Provenance.SourceHash),
		ProjectID:    verified.ProjectID,
		BookID:       verified.BookID,
		BookTitle:    verified.BookTitle,
		ChapterID:    verified.ChapterID,
		ClaimID:      verified.ClaimID,
		Title:        verified.Title,
		RiskTier:     verified.RiskTier,
		Decision:     verified.Decision,
		ReviewStatus: bookKnowledgeProjectAuditStatusPending,
		SampleReason: reason,
		SourceHash:   verified.Provenance.SourceHash,
		AllowedUses:  append([]string(nil), verified.AllowedUses...),
		BlockedUses:  append([]string(nil), verified.BlockedUses...),
		CreatedAt:    now,
	}
}

func auditSampleReason(verified BookKnowledgeVerifiedItem) string {
	if verified.Decision == bookKnowledgeDecisionAllow && verified.RiskTier == bookKnowledgeRiskAutoUsable {
		return ""
	}
	if len(verified.FailureReasons) > 0 {
		return verified.FailureReasons[0]
	}
	if verified.RiskTier != "" {
		return verified.RiskTier
	}
	return "non_auto_decision"
}

func projectCollectionID(projectID, generatedAt string, itemCount int) string {
	return shortSHA256(fmt.Sprintf("%s|%s|%d", projectID, generatedAt, itemCount))
}

func projectAuditID(projectID, claimID, sourceHash string) string {
	return shortSHA256(projectID + "|" + claimID + "|" + sourceHash)
}

func shortSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}
