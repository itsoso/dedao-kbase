package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	bookKnowledgeVersion          = "1"
	defaultBookKnowledgeExtractor = "dedao-gui-fallback"
)

type BookKnowledgeBook struct {
	BookID     string `json:"book_id"`
	DedaoID    int    `json:"dedao_id,omitempty"`
	EnID       string `json:"enid,omitempty"`
	Title      string `json:"title"`
	Author     string `json:"author,omitempty"`
	SourceHTML string `json:"source_html,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	Status     string `json:"status,omitempty"`
	Extractor  string `json:"extractor,omitempty"`
}

type BookKnowledgeChapter struct {
	ChapterID string   `json:"chapter_id"`
	BookID    string   `json:"book_id"`
	Order     int      `json:"order"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary,omitempty"`
	ChunkIDs  []string `json:"chunk_ids,omitempty"`
}

type BookKnowledgeChunk struct {
	ChunkID   string `json:"chunk_id"`
	BookID    string `json:"book_id"`
	ChapterID string `json:"chapter_id"`
	Order     int    `json:"order"`
	Text      string `json:"text"`
	Tokens    int    `json:"tokens,omitempty"`
}

type BookKnowledgeClaim struct {
	ClaimID       string   `json:"claim_id"`
	BookID        string   `json:"book_id"`
	ChapterID     string   `json:"chapter_id,omitempty"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Body          string   `json:"body,omitempty"`
	EvidenceLevel string   `json:"evidence_level,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	ReviewStatus  string   `json:"review_status,omitempty"`
	Citations     []string `json:"citations,omitempty"`
}

type BookKnowledgeCitation struct {
	CitationID string `json:"citation_id"`
	BookID     string `json:"book_id"`
	ChapterID  string `json:"chapter_id,omitempty"`
	ChunkID    string `json:"chunk_id,omitempty"`
	SourceHTML string `json:"source_html,omitempty"`
	Anchor     string `json:"anchor,omitempty"`
	Note       string `json:"note,omitempty"`
}

type BookKnowledgePackage struct {
	Book      BookKnowledgeBook       `json:"book"`
	Chapters  []BookKnowledgeChapter  `json:"chapters"`
	Chunks    []BookKnowledgeChunk    `json:"chunks"`
	Claims    []BookKnowledgeClaim    `json:"claims"`
	Citations []BookKnowledgeCitation `json:"citations"`
}

type BookKnowledgeManifest struct {
	Version   string              `json:"version"`
	UpdatedAt string              `json:"updated_at"`
	Books     []BookKnowledgeBook `json:"books"`
}

type BookKnowledgeSearchQuery struct {
	Query  string `json:"query"`
	BookID string `json:"book_id,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type BookKnowledgeSearchResult struct {
	Kind      string  `json:"kind"`
	BookID    string  `json:"book_id"`
	BookTitle string  `json:"book_title,omitempty"`
	ChapterID string  `json:"chapter_id,omitempty"`
	ChunkID   string  `json:"chunk_id,omitempty"`
	ClaimID   string  `json:"claim_id,omitempty"`
	Title     string  `json:"title,omitempty"`
	Snippet   string  `json:"snippet"`
	Score     float64 `json:"score"`
}

type BookKnowledgeStore struct {
	root string
}

func DefaultBookKnowledgeRoot() string {
	if value := strings.TrimSpace(os.Getenv("DEDAO_BOOK_KNOWLEDGE_ROOT")); value != "" {
		return value
	}
	return filepath.Join(defaultEbookWikiRepoDir, "book_knowledge")
}

func DefaultBookKnowledgeStore() *BookKnowledgeStore {
	return NewBookKnowledgeStore(DefaultBookKnowledgeRoot())
}

func NewBookKnowledgeStore(root string) *BookKnowledgeStore {
	if strings.TrimSpace(root) == "" {
		root = DefaultBookKnowledgeRoot()
	}
	return &BookKnowledgeStore{root: root}
}

func (s *BookKnowledgeStore) Root() string {
	return s.root
}

func (s *BookKnowledgeStore) ManifestPath() string {
	return filepath.Join(s.root, "manifest.json")
}

func (s *BookKnowledgeStore) BookDir(bookID string) string {
	return filepath.Join(s.root, "books", sanitizeBookKnowledgeID(bookID))
}

func (s *BookKnowledgeStore) BookManifestPath(bookID string) string {
	return filepath.Join(s.BookDir(bookID), "manifest.json")
}

func (s *BookKnowledgeStore) BookJSONLPath(bookID, name string) string {
	return filepath.Join(s.BookDir(bookID), sanitizeBookKnowledgeID(name)+".jsonl")
}

func (s *BookKnowledgeStore) SavePackage(pkg BookKnowledgePackage) error {
	if strings.TrimSpace(pkg.Book.BookID) == "" {
		return fmt.Errorf("book knowledge package missing book_id")
	}
	if strings.TrimSpace(pkg.Book.Title) == "" {
		pkg.Book.Title = pkg.Book.BookID
	}
	if strings.TrimSpace(pkg.Book.Status) == "" {
		pkg.Book.Status = "draft"
	}
	if strings.TrimSpace(pkg.Book.Extractor) == "" {
		pkg.Book.Extractor = defaultBookKnowledgeExtractor
	}

	bookDir := s.BookDir(pkg.Book.BookID)
	if err := os.MkdirAll(bookDir, os.ModePerm); err != nil {
		return err
	}
	if err := writeJSONFile(s.BookManifestPath(pkg.Book.BookID), pkg.Book); err != nil {
		return err
	}
	if err := writeJSONLFile(s.BookJSONLPath(pkg.Book.BookID, "chapters"), pkg.Chapters); err != nil {
		return err
	}
	if err := writeJSONLFile(s.BookJSONLPath(pkg.Book.BookID, "chunks"), pkg.Chunks); err != nil {
		return err
	}
	if err := writeJSONLFile(s.BookJSONLPath(pkg.Book.BookID, "claims"), pkg.Claims); err != nil {
		return err
	}
	if err := writeJSONLFile(s.BookJSONLPath(pkg.Book.BookID, "citations"), pkg.Citations); err != nil {
		return err
	}
	return s.upsertManifestBook(pkg.Book)
}

func (s *BookKnowledgeStore) LoadPackage(bookID string) (*BookKnowledgePackage, error) {
	bookID = sanitizeBookKnowledgeID(bookID)
	if strings.TrimSpace(bookID) == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	var book BookKnowledgeBook
	if err := readJSONFile(s.BookManifestPath(bookID), &book); err != nil {
		return nil, err
	}

	chapters, err := readJSONLFile[BookKnowledgeChapter](s.BookJSONLPath(bookID, "chapters"))
	if err != nil {
		return nil, err
	}
	chunks, err := readJSONLFile[BookKnowledgeChunk](s.BookJSONLPath(bookID, "chunks"))
	if err != nil {
		return nil, err
	}
	claims, err := readJSONLFile[BookKnowledgeClaim](s.BookJSONLPath(bookID, "claims"))
	if err != nil {
		return nil, err
	}
	citations, err := readJSONLFile[BookKnowledgeCitation](s.BookJSONLPath(bookID, "citations"))
	if err != nil {
		return nil, err
	}
	return &BookKnowledgePackage{
		Book:      book,
		Chapters:  chapters,
		Chunks:    chunks,
		Claims:    claims,
		Citations: citations,
	}, nil
}

func (s *BookKnowledgeStore) ListBooks() ([]BookKnowledgeBook, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return nil, err
	}
	books := append([]BookKnowledgeBook(nil), manifest.Books...)
	sort.SliceStable(books, func(i, j int) bool {
		if books[i].UpdatedAt != books[j].UpdatedAt {
			return books[i].UpdatedAt > books[j].UpdatedAt
		}
		return books[i].BookID < books[j].BookID
	})
	return books, nil
}

func (s *BookKnowledgeStore) Search(query BookKnowledgeSearchQuery) ([]BookKnowledgeSearchResult, error) {
	terms := splitSearchTerms(query.Query)
	if len(terms) == 0 {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	books, err := s.ListBooks()
	if err != nil {
		return nil, err
	}
	var results []BookKnowledgeSearchResult
	for _, book := range books {
		if query.BookID != "" && book.BookID != query.BookID {
			continue
		}
		pkg, err := s.LoadPackage(book.BookID)
		if err != nil {
			return nil, err
		}
		chapterTitles := make(map[string]string, len(pkg.Chapters))
		for _, chapter := range pkg.Chapters {
			chapterTitles[chapter.ChapterID] = chapter.Title
		}
		for _, chunk := range pkg.Chunks {
			score := searchScore(chunk.Text, terms)
			if score <= 0 {
				continue
			}
			results = append(results, BookKnowledgeSearchResult{
				Kind:      "chunk",
				BookID:    book.BookID,
				BookTitle: book.Title,
				ChapterID: chunk.ChapterID,
				ChunkID:   chunk.ChunkID,
				Title:     chapterTitles[chunk.ChapterID],
				Snippet:   makeSnippet(chunk.Text, terms),
				Score:     score,
			})
		}
		for _, claim := range pkg.Claims {
			text := strings.TrimSpace(claim.Title + " " + claim.Summary + " " + claim.Body)
			score := searchScore(text, terms)
			if score <= 0 {
				continue
			}
			results = append(results, BookKnowledgeSearchResult{
				Kind:      "claim",
				BookID:    book.BookID,
				BookTitle: book.Title,
				ChapterID: claim.ChapterID,
				ClaimID:   claim.ClaimID,
				Title:     claim.Title,
				Snippet:   makeSnippet(text, terms),
				Score:     score,
			})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].BookID != results[j].BookID {
			return results[i].BookID < results[j].BookID
		}
		return results[i].Kind < results[j].Kind
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *BookKnowledgeStore) upsertManifestBook(book BookKnowledgeBook) error {
	manifest, err := s.loadManifest()
	if err != nil {
		return err
	}
	replaced := false
	for i, existing := range manifest.Books {
		if existing.BookID == book.BookID {
			manifest.Books[i] = book
			replaced = true
			break
		}
	}
	if !replaced {
		manifest.Books = append(manifest.Books, book)
	}
	manifest.Version = bookKnowledgeVersion
	manifest.UpdatedAt = time.Now().Format(time.RFC3339)
	sort.SliceStable(manifest.Books, func(i, j int) bool {
		return manifest.Books[i].BookID < manifest.Books[j].BookID
	})
	if err := os.MkdirAll(s.root, os.ModePerm); err != nil {
		return err
	}
	return writeJSONFile(s.ManifestPath(), manifest)
}

func (s *BookKnowledgeStore) loadManifest() (BookKnowledgeManifest, error) {
	manifest := BookKnowledgeManifest{
		Version: bookKnowledgeVersion,
		Books:   []BookKnowledgeBook{},
	}
	if _, err := os.Stat(s.ManifestPath()); err != nil {
		if os.IsNotExist(err) {
			return manifest, nil
		}
		return manifest, err
	}
	if err := readJSONFile(s.ManifestPath(), &manifest); err != nil {
		return manifest, err
	}
	if manifest.Version == "" {
		manifest.Version = bookKnowledgeVersion
	}
	if manifest.Books == nil {
		manifest.Books = []BookKnowledgeBook{}
	}
	return manifest, nil
}

func writeJSONFile(path string, value any) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func readJSONFile(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewDecoder(file).Decode(target)
}

func writeJSONLFile[T any](path string, values []T) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			return err
		}
	}
	return nil
}

func readJSONLFile[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []T{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var values []T
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var value T
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		values = append(values, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if values == nil {
		values = []T{}
	}
	return values, nil
}

func splitSearchTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	terms := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		field = strings.TrimFunc(field, func(r rune) bool {
			return unicode.IsPunct(r) || unicode.IsSymbol(r)
		})
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		terms = append(terms, field)
	}
	return terms
}

func searchScore(text string, terms []string) float64 {
	lower := strings.ToLower(text)
	score := 0.0
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score++
		}
	}
	if score == 0 {
		return 0
	}
	return score / float64(len(terms))
}

func makeSnippet(text string, terms []string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= 180 {
		return string(runes)
	}
	lower := strings.ToLower(string(runes))
	start := 0
	for _, term := range terms {
		if idx := strings.Index(lower, term); idx >= 0 {
			start = len([]rune(lower[:idx])) - 50
			break
		}
	}
	if start < 0 {
		start = 0
	}
	end := start + 180
	if end > len(runes) {
		end = len(runes)
	}
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if end < len(runes) {
		suffix = "..."
	}
	return prefix + string(runes[start:end]) + suffix
}

func sanitizeBookKnowledgeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
		case r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}
