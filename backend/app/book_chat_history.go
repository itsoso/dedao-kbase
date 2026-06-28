package app

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const bookChatHistoryDBName = "book_chat_history.sqlite3"
const bookChatHistoryJSONLName = "book_chat_history.jsonl"

type BookKnowledgeChatHistoryItem struct {
	ID           string                        `json:"id"`
	BookID       string                        `json:"book_id"`
	BookTitle    string                        `json:"book_title"`
	Mode         string                        `json:"mode"`
	Question     string                        `json:"question"`
	Model        string                        `json:"model"`
	Answer       string                        `json:"answer"`
	Sources      []BookKnowledgeChatSource     `json:"sources"`
	ContextStats BookKnowledgeChatContextStats `json:"context_stats"`
	CreatedAt    string                        `json:"created_at"`
}

func (s *BookKnowledgeStore) ChatHistoryDBPath() string {
	return filepath.Join(s.root, bookChatHistoryDBName)
}

func (s *BookKnowledgeStore) ChatHistoryJSONLPath() string {
	return filepath.Join(s.root, bookChatHistoryJSONLName)
}

func (s *BookKnowledgeStore) SaveChatHistory(item BookKnowledgeChatHistoryItem) (BookKnowledgeChatHistoryItem, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	item.BookID = strings.TrimSpace(item.BookID)
	if item.BookID == "" {
		return item, fmt.Errorf("book_id is required")
	}
	item.BookTitle = strings.TrimSpace(item.BookTitle)
	item.Mode = normalizeBookChatMode(item.Mode)
	item.Question = strings.TrimSpace(item.Question)
	item.Model = strings.TrimSpace(item.Model)
	item.Answer = strings.TrimSpace(item.Answer)
	if item.Answer == "" {
		return item, fmt.Errorf("answer is required")
	}
	if strings.TrimSpace(item.ID) == "" {
		item.ID = newBookChatHistoryID()
	}
	if strings.TrimSpace(item.CreatedAt) == "" {
		item.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	sourcesJSON, err := json.Marshal(item.Sources)
	if err != nil {
		return item, err
	}
	statsJSON, err := json.Marshal(item.ContextStats)
	if err != nil {
		return item, err
	}

	db, err := s.openChatHistoryDB()
	if err != nil {
		if isSQLiteCGOStubError(err) {
			return s.saveChatHistoryJSONL(item)
		}
		return item, err
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO book_chat_history (
			id, book_id, book_title, mode, question, model, answer,
			sources_json, context_stats_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.BookID, item.BookTitle, item.Mode, item.Question, item.Model, item.Answer, string(sourcesJSON), string(statsJSON), item.CreatedAt)
	if isSQLiteCGOStubError(err) {
		return s.saveChatHistoryJSONL(item)
	}
	return item, err
}

func (s *BookKnowledgeStore) ListChatHistory(bookID string, limit int) ([]BookKnowledgeChatHistoryItem, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	bookID = strings.TrimSpace(bookID)
	if bookID == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	db, err := s.openChatHistoryDB()
	if err != nil {
		if isSQLiteCGOStubError(err) {
			return s.listChatHistoryJSONL(bookID, limit)
		}
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, book_id, book_title, mode, question, model, answer,
			sources_json, context_stats_json, created_at
		FROM book_chat_history
		WHERE book_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, bookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []BookKnowledgeChatHistoryItem
	for rows.Next() {
		item, err := scanBookChatHistoryRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *BookKnowledgeStore) saveChatHistoryJSONL(item BookKnowledgeChatHistoryItem) (BookKnowledgeChatHistoryItem, error) {
	if err := os.MkdirAll(s.root, os.ModePerm); err != nil {
		return item, err
	}
	file, err := os.OpenFile(s.ChatHistoryJSONLPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return item, err
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(item); err != nil {
		return item, err
	}
	return item, nil
}

func (s *BookKnowledgeStore) listChatHistoryJSONL(bookID string, limit int) ([]BookKnowledgeChatHistoryItem, error) {
	file, err := os.Open(s.ChatHistoryJSONLPath())
	if os.IsNotExist(err) {
		return []BookKnowledgeChatHistoryItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	items := make([]BookKnowledgeChatHistoryItem, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item BookKnowledgeChatHistoryItem
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, err
		}
		if item.BookID == bookID {
			items = append(items, item)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt > items[j].CreatedAt
		}
		return items[i].ID > items[j].ID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

type bookChatHistoryScanner interface {
	Scan(dest ...any) error
}

func scanBookChatHistoryRow(row bookChatHistoryScanner) (BookKnowledgeChatHistoryItem, error) {
	var item BookKnowledgeChatHistoryItem
	var sourcesJSON, statsJSON string
	if err := row.Scan(
		&item.ID,
		&item.BookID,
		&item.BookTitle,
		&item.Mode,
		&item.Question,
		&item.Model,
		&item.Answer,
		&sourcesJSON,
		&statsJSON,
		&item.CreatedAt,
	); err != nil {
		return item, err
	}
	if strings.TrimSpace(sourcesJSON) != "" {
		if err := json.Unmarshal([]byte(sourcesJSON), &item.Sources); err != nil {
			return item, err
		}
	}
	if strings.TrimSpace(statsJSON) != "" {
		if err := json.Unmarshal([]byte(statsJSON), &item.ContextStats); err != nil {
			return item, err
		}
	}
	return item, nil
}

func (s *BookKnowledgeStore) openChatHistoryDB() (*sql.DB, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	if err := os.MkdirAll(s.root, os.ModePerm); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", s.ChatHistoryDBPath())
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateBookChatHistoryDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateBookChatHistoryDB(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS book_chat_history (
			id TEXT PRIMARY KEY,
			book_id TEXT NOT NULL,
			book_title TEXT NOT NULL,
			mode TEXT NOT NULL,
			question TEXT NOT NULL,
			model TEXT NOT NULL,
			answer TEXT NOT NULL,
			sources_json TEXT NOT NULL,
			context_stats_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_book_chat_history_book_created
			ON book_chat_history(book_id, created_at DESC);
	`)
	return err
}

func isSQLiteCGOStubError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "go-sqlite3 requires cgo")
}

func newBookChatHistoryID() string {
	var randomBytes [6]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "chat_" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return "chat_" + time.Now().UTC().Format("20060102T150405.000000000Z") + "_" + hex.EncodeToString(randomBytes[:])
}
