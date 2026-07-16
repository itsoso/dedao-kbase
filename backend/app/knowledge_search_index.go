package app

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const knowledgeSearchIndexDBName = "knowledge_search_index.sqlite3"

type KnowledgeSearchIndex struct {
	root string
	db   *sql.DB
}

type KnowledgeSearchIndexQuery struct {
	Query        string `json:"query"`
	BookID       string `json:"book_id,omitempty"`
	SourceType   string `json:"source_type,omitempty"`
	UpdatedAfter string `json:"updated_after,omitempty"`
	Policy       string `json:"policy,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type KnowledgeSearchIndexResult struct {
	Kind        string  `json:"kind"`
	BookID      string  `json:"book_id"`
	BookTitle   string  `json:"book_title"`
	SourceType  string  `json:"source_type,omitempty"`
	SourceKey   string  `json:"source_key,omitempty"`
	ContentHash string  `json:"content_hash,omitempty"`
	ChapterID   string  `json:"chapter_id,omitempty"`
	ChunkID     string  `json:"chunk_id,omitempty"`
	ClaimID     string  `json:"claim_id,omitempty"`
	Title       string  `json:"title,omitempty"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
	UpdatedAt   string  `json:"updated_at,omitempty"`
}

type knowledgeSearchIndexRecord struct {
	Kind        string
	BookID      string
	BookTitle   string
	SourceType  string
	SourceKey   string
	ContentHash string
	ChapterID   string
	ChunkID     string
	ClaimID     string
	Title       string
	Body        string
	UpdatedAt   string
}

func NewKnowledgeSearchIndex(root string) (*KnowledgeSearchIndex, error) {
	if strings.TrimSpace(root) == "" {
		root = DefaultBookKnowledgeRoot()
	}
	if err := os.MkdirAll(root, os.ModePerm); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, knowledgeSearchIndexDBName))
	if err != nil {
		return nil, err
	}
	index := &KnowledgeSearchIndex{root: root, db: db}
	if err := index.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return index, nil
}

func (s *KnowledgeSearchIndex) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *KnowledgeSearchIndex) migrate() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS knowledge_search_records (
		record_id TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		book_id TEXT NOT NULL,
		book_title TEXT NOT NULL DEFAULT '',
		source_type TEXT NOT NULL DEFAULT '',
		source_key TEXT NOT NULL DEFAULT '',
		content_hash TEXT NOT NULL DEFAULT '',
		chapter_id TEXT NOT NULL DEFAULT '',
		chunk_id TEXT NOT NULL DEFAULT '',
		claim_id TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	return err
}

func (s *KnowledgeSearchIndex) RebuildFromBookStore(store *BookKnowledgeStore) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("knowledge search index is required")
	}
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	books, err := store.ListBooks()
	if err != nil {
		return 0, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM knowledge_search_records`); err != nil {
		return 0, err
	}
	count := 0
	for _, book := range books {
		pkg, err := store.LoadPackage(book.BookID)
		if err != nil {
			return 0, err
		}
		chapterTitles := make(map[string]string, len(pkg.Chapters))
		for _, chapter := range pkg.Chapters {
			chapterTitles[chapter.ChapterID] = chapter.Title
		}
		for _, chunk := range pkg.Chunks {
			record := knowledgeSearchIndexRecord{
				Kind:        "chunk",
				BookID:      book.BookID,
				BookTitle:   book.Title,
				SourceType:  book.SourceType,
				SourceKey:   book.SourceKey,
				ContentHash: book.ContentHash,
				ChapterID:   chunk.ChapterID,
				ChunkID:     chunk.ChunkID,
				Title:       chapterTitles[chunk.ChapterID],
				Body:        chunk.Text,
				UpdatedAt:   book.UpdatedAt,
			}
			if err := insertKnowledgeSearchIndexRecord(tx, record); err != nil {
				return 0, err
			}
			count++
		}
		for _, claim := range pkg.Claims {
			record := knowledgeSearchIndexRecord{
				Kind:        "claim",
				BookID:      book.BookID,
				BookTitle:   book.Title,
				SourceType:  book.SourceType,
				SourceKey:   book.SourceKey,
				ContentHash: book.ContentHash,
				ChapterID:   claim.ChapterID,
				ClaimID:     claim.ClaimID,
				Title:       claim.Title,
				Body:        strings.TrimSpace(claim.Title + " " + claim.Summary + " " + claim.Body),
				UpdatedAt:   book.UpdatedAt,
			}
			if err := insertKnowledgeSearchIndexRecord(tx, record); err != nil {
				return 0, err
			}
			count++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func insertKnowledgeSearchIndexRecord(tx *sql.Tx, record knowledgeSearchIndexRecord) error {
	recordID := stableKnowledgeID("search", record.Kind, record.BookID, record.ChunkID, record.ClaimID)
	_, err := tx.Exec(`INSERT INTO knowledge_search_records (
		record_id, kind, book_id, book_title, source_type, source_key, content_hash,
		chapter_id, chunk_id, claim_id, title, body, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		recordID, record.Kind, record.BookID, record.BookTitle, record.SourceType, record.SourceKey, record.ContentHash,
		record.ChapterID, record.ChunkID, record.ClaimID, record.Title, record.Body, record.UpdatedAt,
	)
	return err
}

func (s *KnowledgeSearchIndex) Search(query KnowledgeSearchIndexQuery) ([]KnowledgeSearchIndexResult, error) {
	terms := splitSearchTerms(query.Query)
	if len(terms) == 0 {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	where := []string{"1 = 1"}
	args := make([]any, 0)
	if strings.TrimSpace(query.BookID) != "" {
		where = append(where, "book_id = ?")
		args = append(args, strings.TrimSpace(query.BookID))
	}
	if strings.TrimSpace(query.SourceType) != "" {
		where = append(where, "source_type = ?")
		args = append(args, strings.TrimSpace(query.SourceType))
	}
	if strings.TrimSpace(query.UpdatedAfter) != "" {
		where = append(where, "updated_at >= ?")
		args = append(args, strings.TrimSpace(query.UpdatedAfter))
	}
	rows, err := s.db.Query(`SELECT kind, book_id, book_title, source_type, source_key, content_hash,
		chapter_id, chunk_id, claim_id, title, body, updated_at
		FROM knowledge_search_records WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]KnowledgeSearchIndexResult, 0)
	for rows.Next() {
		var record knowledgeSearchIndexRecord
		if err := rows.Scan(&record.Kind, &record.BookID, &record.BookTitle, &record.SourceType, &record.SourceKey, &record.ContentHash,
			&record.ChapterID, &record.ChunkID, &record.ClaimID, &record.Title, &record.Body, &record.UpdatedAt); err != nil {
			return nil, err
		}
		score := searchScore(record.Title+" "+record.Body, terms)
		if score <= 0 {
			continue
		}
		results = append(results, KnowledgeSearchIndexResult{
			Kind:        record.Kind,
			BookID:      record.BookID,
			BookTitle:   record.BookTitle,
			SourceType:  record.SourceType,
			SourceKey:   record.SourceKey,
			ContentHash: record.ContentHash,
			ChapterID:   record.ChapterID,
			ChunkID:     record.ChunkID,
			ClaimID:     record.ClaimID,
			Title:       record.Title,
			Snippet:     makeSnippet(record.Body, terms),
			Score:       score,
			UpdatedAt:   record.UpdatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].BookID != results[j].BookID {
			return results[i].BookID < results[j].BookID
		}
		if results[i].Kind != results[j].Kind {
			return results[i].Kind < results[j].Kind
		}
		return results[i].ChunkID+results[i].ClaimID < results[j].ChunkID+results[j].ClaimID
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
