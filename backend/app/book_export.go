package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	BookKnowledgeExportHealthSystemKBV2 = "health_system_kb_v2"
	BookKnowledgeExportQuantRuleCards   = "quant_rule_cards"
	BookKnowledgeExportNotebookLMBridge = "notebooklm_bridge"
)

type BookKnowledgeExportResult struct {
	BookID    string   `json:"book_id"`
	Target    string   `json:"target"`
	OutputDir string   `json:"output_dir"`
	Files     []string `json:"files"`
}

func ExportBookKnowledgePackage(store *BookKnowledgeStore, bookID, target string) (*BookKnowledgeExportResult, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	pkg, err := store.LoadPackage(bookID)
	if err != nil {
		return nil, err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("export target is required")
	}
	outputDir := filepath.Join(store.BookDir(pkg.Book.BookID), "exports", sanitizeBookKnowledgeID(target))
	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		return nil, err
	}

	var files []string
	switch target {
	case BookKnowledgeExportHealthSystemKBV2:
		files, err = exportHealthSystemKBV2(pkg, outputDir)
	case BookKnowledgeExportQuantRuleCards:
		files, err = exportQuantRuleCards(pkg, outputDir)
	case BookKnowledgeExportNotebookLMBridge:
		files, err = exportNotebookLMBridge(pkg, outputDir)
	default:
		return nil, fmt.Errorf("unknown book knowledge export target: %s", target)
	}
	if err != nil {
		return nil, err
	}
	return &BookKnowledgeExportResult{
		BookID:    pkg.Book.BookID,
		Target:    target,
		OutputDir: outputDir,
		Files:     files,
	}, nil
}

func exportHealthSystemKBV2(pkg *BookKnowledgePackage, outputDir string) ([]string, error) {
	now := time.Now().Format(time.RFC3339)
	entityID := "book:" + pkg.Book.BookID
	entities := []map[string]any{
		{
			"entity_id":      entityID,
			"entity_type":    "book",
			"canonical_name": pkg.Book.Title,
			"aliases":        []string{pkg.Book.BookID},
			"source":         "dedao-gui",
			"metadata": map[string]any{
				"book_id":       pkg.Book.BookID,
				"source_html":   pkg.Book.SourceHTML,
				"review_status": "draft",
				"created_at":    now,
			},
		},
	}

	pages := make([]map[string]any, 0, len(pkg.Chapters))
	for _, chapter := range pkg.Chapters {
		pages = append(pages, map[string]any{
			"page_id":       chapter.ChapterID,
			"title":         chapter.Title,
			"body":          chapter.Summary,
			"source":        "dedao-gui",
			"source_entity": entityID,
			"metadata": map[string]any{
				"book_id":       pkg.Book.BookID,
				"chapter_id":    chapter.ChapterID,
				"review_status": "draft",
			},
		})
	}

	claims := make([]map[string]any, 0, len(pkg.Claims))
	for _, claim := range pkg.Claims {
		claims = append(claims, map[string]any{
			"claim_id":   claim.ClaimID,
			"subject_id": entityID,
			"predicate":  "book_claim",
			"object":     claim.Summary,
			"confidence": claim.Confidence,
			"source":     "dedao-gui",
			"evidence":   claim.Citations,
			"metadata": map[string]any{
				"book_id":        pkg.Book.BookID,
				"chapter_id":     claim.ChapterID,
				"title":          claim.Title,
				"review_status":  "draft",
				"evidence_level": claim.EvidenceLevel,
			},
		})
	}

	files := []string{
		filepath.Join(outputDir, "entities.jsonl"),
		filepath.Join(outputDir, "pages.jsonl"),
		filepath.Join(outputDir, "claims.jsonl"),
	}
	if err := writeJSONLFile(files[0], entities); err != nil {
		return nil, err
	}
	if err := writeJSONLFile(files[1], pages); err != nil {
		return nil, err
	}
	if err := writeJSONLFile(files[2], claims); err != nil {
		return nil, err
	}
	return files, nil
}

func exportQuantRuleCards(pkg *BookKnowledgePackage, outputDir string) ([]string, error) {
	ruleCards := make([]map[string]any, 0, len(pkg.Claims))
	for _, claim := range pkg.Claims {
		ruleCards = append(ruleCards, map[string]any{
			"rule_id":          "rule:" + claim.ClaimID,
			"book_id":          pkg.Book.BookID,
			"source_claim_id":  claim.ClaimID,
			"title":            claim.Title,
			"rule_text":        claim.Summary,
			"review_status":    "draft",
			"execution_mode":   "paper_only",
			"source":           "dedao-gui",
			"source_html":      pkg.Book.SourceHTML,
			"candidate_scope":  []string{"research", "backtest", "paper"},
			"required_reviews": []string{"domain_review", "backtest_review", "risk_review"},
			"guardrails": map[string]any{
				"allow_live_orders": false,
				"requires_backtest": true,
			},
		})
	}
	files := []string{filepath.Join(outputDir, "rule_cards.jsonl")}
	if err := writeJSONLFile(files[0], ruleCards); err != nil {
		return nil, err
	}
	return files, nil
}
