package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gofrs/flock"
)

const (
	bookKnowledgeVersion          = "1"
	defaultBookKnowledgeExtractor = "dedao-gui-fallback"
	bookKnowledgeRootLockFileName = ".package.lock"
	bookKnowledgeRootLockRetry    = 10 * time.Millisecond
)

type BookKnowledgeBook struct {
	BookID        string `json:"book_id"`
	DedaoID       int    `json:"dedao_id,omitempty"`
	EnID          string `json:"enid,omitempty"`
	Title         string `json:"title"`
	Author        string `json:"author,omitempty"`
	SourceHTML    string `json:"source_html,omitempty"`
	SourceType    string `json:"source_type,omitempty"`
	SourceKey     string `json:"source_key,omitempty"`
	SourceAccount string `json:"source_account,omitempty"`
	PublishedAt   string `json:"published_at,omitempty"`
	ContentHash   string `json:"content_hash,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	Status        string `json:"status,omitempty"`
	Extractor     string `json:"extractor,omitempty"`
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
	CitationID    string `json:"citation_id"`
	BookID        string `json:"book_id"`
	ChapterID     string `json:"chapter_id,omitempty"`
	ChunkID       string `json:"chunk_id,omitempty"`
	SourceHTML    string `json:"source_html,omitempty"`
	Anchor        string `json:"anchor,omitempty"`
	Note          string `json:"note,omitempty"`
	SourceType    string `json:"source_type,omitempty"`
	SourceAccount string `json:"source_account,omitempty"`
	SourceItemKey string `json:"source_item_key,omitempty"`
	PublishedAt   string `json:"published_at,omitempty"`
}

type BookKnowledgePackage struct {
	Book      BookKnowledgeBook       `json:"book"`
	Chapters  []BookKnowledgeChapter  `json:"chapters"`
	Chunks    []BookKnowledgeChunk    `json:"chunks"`
	Claims    []BookKnowledgeClaim    `json:"claims"`
	Citations []BookKnowledgeCitation `json:"citations"`
}

func BookKnowledgeContentHash(pkg BookKnowledgePackage) (string, error) {
	book := pkg.Book
	book.ContentHash = ""
	book.CreatedAt = ""
	book.UpdatedAt = ""
	book.Status = ""

	chapters := append([]BookKnowledgeChapter(nil), pkg.Chapters...)
	for index := range chapters {
		chapters[index].ChunkIDs = append([]string(nil), chapters[index].ChunkIDs...)
		sort.Strings(chapters[index].ChunkIDs)
	}
	sort.Slice(chapters, func(i, j int) bool { return chapters[i].ChapterID < chapters[j].ChapterID })

	chunks := append([]BookKnowledgeChunk(nil), pkg.Chunks...)
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ChunkID < chunks[j].ChunkID })

	claims := append([]BookKnowledgeClaim(nil), pkg.Claims...)
	for index := range claims {
		claims[index].Citations = append([]string(nil), claims[index].Citations...)
		sort.Strings(claims[index].Citations)
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].ClaimID < claims[j].ClaimID })

	citations := append([]BookKnowledgeCitation(nil), pkg.Citations...)
	sort.Slice(citations, func(i, j int) bool { return citations[i].CitationID < citations[j].CitationID })

	canonical := struct {
		Book      BookKnowledgeBook       `json:"book"`
		Chapters  []BookKnowledgeChapter  `json:"chapters"`
		Chunks    []BookKnowledgeChunk    `json:"chunks"`
		Claims    []BookKnowledgeClaim    `json:"claims"`
		Citations []BookKnowledgeCitation `json:"citations"`
	}{book, chapters, chunks, claims, citations}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode canonical book knowledge: %w", err)
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", digest), nil
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
	root                      string
	mu                        sync.RWMutex
	agentSemanticEmbedder     AgentSemanticEmbedder
	beforePackageRootLock     func()
	beforePackageRootReadLock func()
	afterPackageManifestRead  func()
	beforePackagePublish      func()
	afterPackagePublishGate   func()
	afterPackageBookInstall   func() error
}

type bookKnowledgePackageCommitFence struct {
	prepare func(context.Context, BookKnowledgePackage) error
	discard func(BookKnowledgePackage) error
}

type bookKnowledgePackageCommitFenceContextKey struct{}

func contextWithBookKnowledgePackageCommitFence(
	ctx context.Context,
	fence bookKnowledgePackageCommitFence,
) context.Context {
	return context.WithValue(ctx, bookKnowledgePackageCommitFenceContextKey{}, fence)
}

func bookKnowledgePackageCommitFenceFromContext(ctx context.Context) (bookKnowledgePackageCommitFence, bool) {
	if ctx == nil {
		return bookKnowledgePackageCommitFence{}, false
	}
	fence, ok := ctx.Value(bookKnowledgePackageCommitFenceContextKey{}).(bookKnowledgePackageCommitFence)
	return fence, ok && fence.prepare != nil
}

func (s *BookKnowledgeStore) SetAgentSemanticEmbedder(embedder AgentSemanticEmbedder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentSemanticEmbedder = embedder
}

func DefaultBookKnowledgeRoot() string {
	if value := strings.TrimSpace(os.Getenv("DEDAO_BOOK_KNOWLEDGE_ROOT")); value != "" {
		return value
	}
	if repoDir := defaultWikiRepoDirFromEnv(); repoDir != "" {
		return filepath.Join(repoDir, "book_knowledge")
	}
	return "book_knowledge"
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
	return s.SavePackageContext(context.Background(), pkg)
}

func (s *BookKnowledgeStore) SavePackageContext(ctx context.Context, pkg BookKnowledgePackage) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	rootLock, err := s.acquireBookKnowledgeRootLock(ctx)
	if err != nil {
		return err
	}
	defer rootLock.Close()

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
	if strings.TrimSpace(pkg.Book.ContentHash) == "" {
		contentHash, err := BookKnowledgeContentHash(pkg)
		if err != nil {
			return err
		}
		pkg.Book.ContentHash = contentHash
	}
	bookJSON, err := encodeJSONFile(pkg.Book)
	if err != nil {
		return err
	}
	chaptersJSONL, err := encodeJSONLFile(pkg.Chapters)
	if err != nil {
		return err
	}
	chunksJSONL, err := encodeJSONLFile(pkg.Chunks)
	if err != nil {
		return err
	}
	claimsJSONL, err := encodeJSONLFile(pkg.Claims)
	if err != nil {
		return err
	}
	citationsJSONL, err := encodeJSONLFile(pkg.Citations)
	if err != nil {
		return err
	}
	manifest, err := s.loadManifest()
	if err != nil {
		return err
	}
	if s.afterPackageManifestRead != nil {
		s.afterPackageManifestRead()
	}
	manifest = upsertBookKnowledgeManifest(manifest, pkg.Book)
	manifestJSON, err := encodeJSONFile(manifest)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stagingRoot, err := os.MkdirTemp(s.root, ".book-package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stagingRoot)
	stagedBookDir := filepath.Join(stagingRoot, "book")
	if err := os.MkdirAll(stagedBookDir, os.ModePerm); err != nil {
		return err
	}
	if err := copyBookKnowledgeDirectoryContext(ctx, s.BookDir(pkg.Book.BookID), stagedBookDir); err != nil {
		return err
	}
	stagedFiles := []struct {
		name string
		data []byte
	}{
		{name: "chapters.jsonl", data: chaptersJSONL},
		{name: "chunks.jsonl", data: chunksJSONL},
		{name: "claims.jsonl", data: claimsJSONL},
		{name: "citations.jsonl", data: citationsJSONL},
		{name: "manifest.json", data: bookJSON},
	}
	for _, file := range stagedFiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeFileAtomically(filepath.Join(stagedBookDir, file.name), file.data); err != nil {
			return err
		}
	}
	stagedManifest := filepath.Join(stagingRoot, "manifest.json")
	if err := writeFileAtomically(stagedManifest, manifestJSON); err != nil {
		return err
	}
	if s.beforePackagePublish != nil {
		s.beforePackagePublish()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fence, hasFence := bookKnowledgePackageCommitFenceFromContext(ctx)
	if hasFence {
		if err := fence.prepare(ctx, pkg); err != nil {
			return err
		}
	}
	if s.afterPackagePublishGate != nil {
		s.afterPackagePublishGate()
	}
	publishErr := publishBookKnowledgePackage(
		stagedBookDir, s.BookDir(pkg.Book.BookID), stagedManifest, s.ManifestPath(), s.afterPackageBookInstall,
	)
	if publishErr != nil && hasFence && fence.discard != nil {
		if discardErr := fence.discard(pkg); discardErr != nil {
			return errors.Join(publishErr, fmt.Errorf("discard package commit receipt: %w", discardErr))
		}
	}
	return publishErr
}

func (s *BookKnowledgeStore) acquireBookKnowledgeRootLock(ctx context.Context) (*flock.Flock, error) {
	return s.acquireBookKnowledgeRootFileLock(ctx, false)
}

func (s *BookKnowledgeStore) acquireBookKnowledgeRootReadLock(ctx context.Context) (*flock.Flock, error) {
	return s.acquireBookKnowledgeRootFileLock(ctx, true)
}

func (s *BookKnowledgeStore) acquireBookKnowledgeRootFileLock(ctx context.Context, shared bool) (*flock.Flock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensureBookKnowledgePrivateRoot(s.root); err != nil {
		return nil, err
	}
	if shared && s.beforePackageRootReadLock != nil {
		s.beforePackageRootReadLock()
	} else if !shared && s.beforePackageRootLock != nil {
		s.beforePackageRootLock()
	}
	fileLock := flock.New(filepath.Join(s.root, bookKnowledgeRootLockFileName))
	var locked bool
	var err error
	if shared {
		locked, err = fileLock.TryRLockContext(ctx, bookKnowledgeRootLockRetry)
	} else {
		locked, err = fileLock.TryLockContext(ctx, bookKnowledgeRootLockRetry)
	}
	if err != nil || !locked {
		_ = fileLock.Close()
		if err != nil {
			return nil, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.New("book knowledge root lock was not acquired")
	}
	if err := os.Chmod(filepath.Join(s.root, bookKnowledgeRootLockFileName), 0o600); err != nil {
		_ = fileLock.Close()
		return nil, err
	}
	return fileLock, nil
}

func ensureBookKnowledgePrivateRoot(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("book knowledge root is not a directory: %s", root)
	}
	return os.Chmod(root, 0o700)
}

func copyBookKnowledgeDirectoryContext(ctx context.Context, source, target string) error {
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, info.Mode().Perm())
		}
		if entry.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, destination)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported book artifact type: %s", relative)
		}
		if err := os.Link(path, destination); err == nil {
			return nil
		}
		return copyBookKnowledgeFileContext(ctx, path, destination, info.Mode().Perm())
	})
}

func copyBookKnowledgeFileContext(ctx context.Context, source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
	}()
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readErr := input.Read(buffer)
		if read > 0 {
			if _, err := output.Write(buffer[:read]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

func publishBookKnowledgePackage(
	stagedBookDir, bookDir, stagedManifest, manifestPath string,
	afterBookInstall func() error,
) error {
	if err := os.MkdirAll(filepath.Dir(bookDir), os.ModePerm); err != nil {
		return err
	}
	backupRoot, err := os.MkdirTemp(filepath.Dir(manifestPath), ".book-package-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(backupRoot)
	backupBookDir := filepath.Join(backupRoot, "book")
	bookExisted, err := movePathToBackup(bookDir, backupBookDir)
	if err != nil {
		return err
	}
	rollback := func(publishErr error) error {
		var rollbackErrors []error
		if err := os.RemoveAll(bookDir); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove partial book package: %w", err))
		}
		if err := restorePathFromBackup(backupBookDir, bookDir, bookExisted); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore book package: %w", err))
		}
		return errors.Join(append([]error{publishErr}, rollbackErrors...)...)
	}
	if err := os.Rename(stagedBookDir, bookDir); err != nil {
		return rollback(err)
	}
	if afterBookInstall != nil {
		if err := afterBookInstall(); err != nil {
			return rollback(err)
		}
	}
	if err := replaceFileAtomically(stagedManifest, manifestPath); err != nil {
		return rollback(err)
	}
	return nil
}

func movePathToBackup(path, backupPath string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.Rename(path, backupPath); err != nil {
		return false, err
	}
	return true, nil
}

func restorePathFromBackup(backupPath, path string, existed bool) error {
	if !existed {
		return nil
	}
	return os.Rename(backupPath, path)
}

func (s *BookKnowledgeStore) RepairMissingBookContentHash(bookID string) (*BookKnowledgePackage, error) {
	pkg, err := s.LoadPackage(bookID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(pkg.Book.ContentHash) != "" {
		return nil, fmt.Errorf("book already has content hash")
	}
	contentHash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{s.BookAnalysisManifestPath(bookID), s.BookQualityReportPath(bookID)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("invalidate derived book artifact: %w", err)
		}
	}
	pkg.Book.ContentHash = contentHash
	if err := s.SavePackage(*pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

func (s *BookKnowledgeStore) LoadPackage(bookID string) (*BookKnowledgePackage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rootLock, err := s.acquireBookKnowledgeRootReadLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	return s.loadPackageUnlocked(bookID)
}

func (s *BookKnowledgeStore) loadPackageUnlocked(bookID string) (*BookKnowledgePackage, error) {
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	rootLock, err := s.acquireBookKnowledgeRootReadLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	return s.listBooksUnlocked()
}

func (s *BookKnowledgeStore) listBooksUnlocked() ([]BookKnowledgeBook, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return nil, err
	}
	books := append([]BookKnowledgeBook(nil), manifest.Books...)
	sort.SliceStable(books, func(i, j int) bool {
		left := bookKnowledgeListTimestamp(books[i])
		right := bookKnowledgeListTimestamp(books[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return books[i].BookID < books[j].BookID
	})
	return books, nil
}

func bookKnowledgeListTimestamp(book BookKnowledgeBook) time.Time {
	candidates := []string{book.UpdatedAt, book.CreatedAt}
	if book.SourceType == "wechat_mp_article" {
		candidates = append([]string{book.PublishedAt}, candidates...)
	}
	for _, candidate := range candidates {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(layout, strings.TrimSpace(candidate)); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
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

	s.mu.RLock()
	defer s.mu.RUnlock()
	rootLock, err := s.acquireBookKnowledgeRootReadLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	books, err := s.listBooksUnlocked()
	if err != nil {
		return nil, err
	}
	var results []BookKnowledgeSearchResult
	for _, book := range books {
		if query.BookID != "" && book.BookID != query.BookID {
			continue
		}
		pkg, err := s.loadPackageUnlocked(book.BookID)
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

func upsertBookKnowledgeManifest(manifest BookKnowledgeManifest, book BookKnowledgeBook) BookKnowledgeManifest {
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
	return manifest
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
	data, err := encodeJSONFile(value)
	if err != nil {
		return err
	}
	return writeFileAtomically(path, data)
}

func encodeJSONFile(value any) ([]byte, error) {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
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
	data, err := encodeJSONLFile(values)
	if err != nil {
		return err
	}
	return writeFileAtomically(path, data)
}

func encodeJSONLFile[T any](values []T) ([]byte, error) {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			return nil, err
		}
	}
	return data.Bytes(), nil
}

func writeFileAtomically(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporaryPath, path)
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
