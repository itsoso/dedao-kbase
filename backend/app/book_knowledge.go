package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	bookKnowledgeVersion                          = "1"
	defaultBookKnowledgeExtractor                 = "dedao-gui-fallback"
	bookKnowledgeRootLockFileName                 = ".package.lock"
	bookKnowledgeRootLockRetry                    = 10 * time.Millisecond
	bookKnowledgeJobCommitMarkerFileName          = ".book-job-commit.json"
	bookKnowledgeJobCommitMarkerVersion           = "1"
	bookKnowledgePublishTransactionsDir           = ".book-publish-transactions"
	bookKnowledgePublishJournalFileName           = "transaction.json"
	bookKnowledgePublishJournalVersion            = "1"
	bookKnowledgePublishPhasePreparing            = "preparing"
	bookKnowledgePublishPhasePrepared             = "prepared"
	bookKnowledgePublishPhaseBackedUp             = "book_backed_up"
	bookKnowledgePublishPhaseInstalled            = "book_installed"
	bookKnowledgePublishPhaseRestoringBackup      = "restoring_backup"
	bookKnowledgePublishPhaseDiscardingOwnedFinal = "discarding_owned_final"
)

var (
	errBookKnowledgeJobCommitMarkerInvalid  = errors.New("invalid book job commit marker")
	errBookKnowledgePublishPending          = errors.New("pending book publish transaction")
	errBookKnowledgePublishCorrupt          = errors.New("corrupt book publish transaction")
	errBookKnowledgePublishAmbiguous        = errors.New("ambiguous book publish transaction")
	errBookKnowledgePublishRecoveryRequired = errors.New("book publish transaction requires recovery")
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
	afterPackageBookBackup    func() error
	afterPackageBookInstall   func() error
	cleanupPackageTransaction func(string) error
}

type bookKnowledgePackageCommitFence struct {
	prepare func(context.Context, BookKnowledgePackage) (bookKnowledgeJobCommitMarker, error)
	discard func(BookKnowledgePackage, bookKnowledgeJobCommitMarker) error
}

type bookKnowledgeJobCommitMarker struct {
	Version      string `json:"version"`
	JobID        string `json:"job_id"`
	PublishNonce string `json:"publish_nonce"`
	BookID       string `json:"book_id"`
	ContentHash  string `json:"content_hash"`
}

type bookKnowledgePublishJournal struct {
	Version      string                       `json:"version"`
	Marker       bookKnowledgeJobCommitMarker `json:"marker"`
	Phase        string                       `json:"phase"`
	BookExisted  bool                         `json:"book_existed"`
	RollbackBook *BookKnowledgeBook           `json:"rollback_book,omitempty"`
}

type bookKnowledgePackageCommitFenceContextKey struct{}

type bookKnowledgeInvalidateDerivedContextKey struct{}

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

func contextWithBookKnowledgeDerivedInvalidation(ctx context.Context) context.Context {
	return context.WithValue(ctx, bookKnowledgeInvalidateDerivedContextKey{}, true)
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
	return s.savePackageContextUnlocked(ctx, pkg)
}

func (s *BookKnowledgeStore) savePackageContextUnlocked(ctx context.Context, pkg BookKnowledgePackage) (returnErr error) {
	if strings.TrimSpace(pkg.Book.BookID) == "" {
		return fmt.Errorf("book knowledge package missing book_id")
	}
	if err := rejectPendingBookKnowledgePublishTransaction(s.root, pkg.Book.BookID); err != nil {
		return err
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
	fence, hasFence := bookKnowledgePackageCommitFenceFromContext(ctx)
	var commitMarker bookKnowledgeJobCommitMarker
	receiptActive := false
	transactionCreated := false
	if hasFence {
		commitMarker, err = fence.prepare(ctx, pkg)
		if err != nil {
			return err
		}
		receiptActive = true
		defer func() {
			if !receiptActive {
				return
			}
			if errors.Is(returnErr, errBookKnowledgePublishRecoveryRequired) {
				return
			}
			if transactionCreated {
				if cleanupErr := cleanupBookKnowledgePublishTransactionArtifacts(s.root, commitMarker); cleanupErr != nil {
					returnErr = errors.Join(returnErr, fmt.Errorf("clean package publish transaction: %w", cleanupErr))
					return
				}
			}
			if fence.discard == nil {
				return
			}
			if discardErr := fence.discard(pkg, commitMarker); discardErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("discard package commit receipt: %w", discardErr))
			}
		}()
	}
	var stagingRoot string
	if hasFence {
		stagingRoot, err = createBookKnowledgePublishTransaction(s.root, commitMarker)
	} else {
		stagingRoot, err = os.MkdirTemp(s.root, ".book-package-")
	}
	if err != nil {
		return err
	}
	transactionCreated = hasFence
	if !hasFence {
		defer os.RemoveAll(stagingRoot)
	}
	stagedBookDir := filepath.Join(stagingRoot, "book")
	if err := os.MkdirAll(stagedBookDir, os.ModePerm); err != nil {
		return err
	}
	if err := copyBookKnowledgeDirectoryContext(ctx, s.BookDir(pkg.Book.BookID), stagedBookDir); err != nil {
		return err
	}
	if err := os.Remove(bookKnowledgeJobCommitMarkerPath(stagedBookDir)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if invalidate, _ := ctx.Value(bookKnowledgeInvalidateDerivedContextKey{}).(bool); invalidate {
		for _, name := range []string{"analysis_manifest.json", "quality_report.json"} {
			if err := os.Remove(filepath.Join(stagedBookDir, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
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
	if hasFence {
		if err := writeBookKnowledgeJobCommitMarker(stagedBookDir, commitMarker); err != nil {
			return err
		}
		journal := bookKnowledgePublishJournal{
			Version: bookKnowledgePublishJournalVersion, Marker: commitMarker, Phase: bookKnowledgePublishPhasePrepared,
		}
		if err := writeBookKnowledgePublishJournal(stagingRoot, journal); err != nil {
			return err
		}
	}
	if s.beforePackagePublish != nil {
		s.beforePackagePublish()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.afterPackagePublishGate != nil {
		s.afterPackagePublishGate()
	}
	var publishErr error
	if hasFence {
		publishErr = publishBookKnowledgePackageTransaction(
			stagingRoot, s.BookDir(pkg.Book.BookID), s.ManifestPath(),
			s.afterPackageBookBackup, s.afterPackageBookInstall,
		)
	} else {
		publishErr = publishBookKnowledgePackage(
			stagedBookDir, s.BookDir(pkg.Book.BookID), stagedManifest, s.ManifestPath(),
			s.afterPackageBookBackup, s.afterPackageBookInstall,
		)
	}
	if publishErr != nil {
		return publishErr
	}
	receiptActive = false
	if hasFence {
		cleanup := cleanupBookKnowledgePublishTransaction
		if s.cleanupPackageTransaction != nil {
			cleanup = s.cleanupPackageTransaction
		}
		if err := cleanup(stagingRoot); err != nil {
			// The package and root manifest are already committed. Keep the exact journal as the
			// recovery handle; a later publisher verifies the complete final state before cleaning it.
			return nil
		}
	}
	return nil
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

func bookKnowledgeJobCommitMarkerPath(bookDir string) string {
	return filepath.Join(bookDir, bookKnowledgeJobCommitMarkerFileName)
}

func writeBookKnowledgeJobCommitMarker(bookDir string, marker bookKnowledgeJobCommitMarker) error {
	if err := validateBookKnowledgeJobCommitMarker(marker); err != nil {
		return err
	}
	payload, err := encodeJSONFile(marker)
	if err != nil {
		return err
	}
	path := bookKnowledgeJobCommitMarkerPath(bookDir)
	if err := writeFileAtomically(path, payload); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func readBookKnowledgeJobCommitMarker(bookDir string) (bookKnowledgeJobCommitMarker, error) {
	var marker bookKnowledgeJobCommitMarker
	file, err := os.Open(bookKnowledgeJobCommitMarkerPath(bookDir))
	if err != nil {
		return marker, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return bookKnowledgeJobCommitMarker{}, fmt.Errorf("%w: %v", errBookKnowledgeJobCommitMarkerInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return bookKnowledgeJobCommitMarker{}, errBookKnowledgeJobCommitMarkerInvalid
	}
	return marker, nil
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

func bookKnowledgePublishTransactionPath(root string, marker bookKnowledgeJobCommitMarker) string {
	digest := sha256.Sum256([]byte(marker.JobID + "\x00" + marker.PublishNonce))
	return filepath.Join(root, bookKnowledgePublishTransactionsDir, hex.EncodeToString(digest[:]))
}

func bookKnowledgePublishTransactionPreparingPath(root string, marker bookKnowledgeJobCommitMarker) string {
	return bookKnowledgePublishTransactionPath(root, marker) + ".preparing"
}

func createBookKnowledgePublishTransaction(root string, marker bookKnowledgeJobCommitMarker) (string, error) {
	if err := validateBookKnowledgeJobCommitMarker(marker); err != nil {
		return "", err
	}
	transactionsRoot := filepath.Join(root, bookKnowledgePublishTransactionsDir)
	if err := os.MkdirAll(transactionsRoot, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(transactionsRoot, 0o700); err != nil {
		return "", err
	}
	transactionRoot := bookKnowledgePublishTransactionPath(root, marker)
	preparingRoot := bookKnowledgePublishTransactionPreparingPath(root, marker)
	if err := os.Mkdir(preparingRoot, 0o700); err != nil {
		return "", err
	}
	journal := bookKnowledgePublishJournal{
		Version: bookKnowledgePublishJournalVersion, Marker: marker, Phase: bookKnowledgePublishPhasePreparing,
	}
	if err := writeBookKnowledgePublishJournal(preparingRoot, journal); err != nil {
		_ = os.RemoveAll(preparingRoot)
		return "", err
	}
	if err := os.Rename(preparingRoot, transactionRoot); err != nil {
		_ = os.RemoveAll(preparingRoot)
		return "", err
	}
	return transactionRoot, nil
}

func validateBookKnowledgeJobCommitMarker(marker bookKnowledgeJobCommitMarker) error {
	if marker.Version != bookKnowledgeJobCommitMarkerVersion || strings.TrimSpace(marker.JobID) == "" ||
		strings.TrimSpace(marker.PublishNonce) == "" || strings.TrimSpace(marker.BookID) == "" ||
		strings.TrimSpace(marker.ContentHash) == "" {
		return errBookKnowledgeJobCommitMarkerInvalid
	}
	return nil
}

func writeBookKnowledgePublishJournal(transactionRoot string, journal bookKnowledgePublishJournal) error {
	if err := validateBookKnowledgePublishJournal(journal); err != nil {
		return err
	}
	payload, err := encodeJSONFile(journal)
	if err != nil {
		return err
	}
	path := filepath.Join(transactionRoot, bookKnowledgePublishJournalFileName)
	if err := writeFileAtomically(path, payload); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func readBookKnowledgePublishJournal(transactionRoot string) (bookKnowledgePublishJournal, error) {
	var journal bookKnowledgePublishJournal
	file, err := os.Open(filepath.Join(transactionRoot, bookKnowledgePublishJournalFileName))
	if err != nil {
		return journal, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return bookKnowledgePublishJournal{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return bookKnowledgePublishJournal{}, fmt.Errorf("invalid trailing book publish journal data")
	}
	if err := validateBookKnowledgePublishJournal(journal); err != nil {
		return bookKnowledgePublishJournal{}, err
	}
	return journal, nil
}

func validateBookKnowledgePublishJournal(journal bookKnowledgePublishJournal) error {
	if journal.Version != bookKnowledgePublishJournalVersion || validateBookKnowledgeJobCommitMarker(journal.Marker) != nil {
		return fmt.Errorf("invalid book publish transaction journal")
	}
	switch journal.Phase {
	case bookKnowledgePublishPhasePreparing, bookKnowledgePublishPhasePrepared,
		bookKnowledgePublishPhaseBackedUp, bookKnowledgePublishPhaseInstalled:
		if journal.RollbackBook != nil {
			return fmt.Errorf("unexpected rollback identity for book publish transaction phase %q", journal.Phase)
		}
		return nil
	case bookKnowledgePublishPhaseRestoringBackup:
		if journal.RollbackBook == nil || journal.RollbackBook.BookID != journal.Marker.BookID ||
			strings.TrimSpace(journal.RollbackBook.ContentHash) == "" {
			return fmt.Errorf("invalid rollback identity for book publish transaction")
		}
		return nil
	case bookKnowledgePublishPhaseDiscardingOwnedFinal:
		if journal.RollbackBook != nil {
			return fmt.Errorf("unexpected rollback identity while discarding owned final")
		}
		return nil
	default:
		return fmt.Errorf("invalid book publish transaction phase %q", journal.Phase)
	}
}

func rejectPendingBookKnowledgePublishTransaction(root, bookID string) error {
	bookID = sanitizeBookKnowledgeID(bookID)
	transactionsRoot := filepath.Join(root, bookKnowledgePublishTransactionsDir)
	entries, err := os.ReadDir(transactionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		journal, err := readBookKnowledgePublishJournal(filepath.Join(transactionsRoot, entry.Name()))
		if err != nil {
			return fmt.Errorf("%w: journal cannot be verified", errBookKnowledgePublishCorrupt)
		}
		transactionRoot := filepath.Join(transactionsRoot, entry.Name())
		committed, err := verifyCommittedBookKnowledgePublishTransaction(root, journal)
		if err != nil {
			return fmt.Errorf("%w: committed state cannot be verified", errBookKnowledgePublishCorrupt)
		}
		if committed {
			if err := cleanupBookKnowledgePublishTransaction(transactionRoot); err != nil {
				return fmt.Errorf("clean committed book publish transaction: %w", err)
			}
			continue
		}
		if sanitizeBookKnowledgeID(journal.Marker.BookID) == bookID {
			return fmt.Errorf("%w for book %q", errBookKnowledgePublishPending, bookID)
		}
	}
	return nil
}

func verifyCommittedBookKnowledgePublishTransaction(root string, journal bookKnowledgePublishJournal) (bool, error) {
	bookDir := filepath.Join(root, "books", sanitizeBookKnowledgeID(journal.Marker.BookID))
	marker, err := readBookKnowledgeJobCommitMarker(bookDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errBookKnowledgeJobCommitMarkerInvalid) {
			return false, nil
		}
		return false, err
	}
	if marker != journal.Marker {
		return false, nil
	}
	pkg, err := loadBookKnowledgePackageFromDirectory(bookDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if pkg.Book.BookID != marker.BookID || pkg.Book.ContentHash != marker.ContentHash {
		return false, nil
	}
	computedHash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		return false, err
	}
	if computedHash != marker.ContentHash {
		return false, nil
	}
	manifestStore := NewBookKnowledgeStore(root)
	manifest, err := manifestStore.loadManifest()
	if err != nil {
		return false, err
	}
	for _, book := range manifest.Books {
		if book == pkg.Book {
			return true, nil
		}
	}
	return false, nil
}

func publishBookKnowledgePackageTransaction(
	transactionRoot, bookDir, manifestPath string,
	afterBookBackup func() error,
	afterBookInstall func() error,
) error {
	journal, err := readBookKnowledgePublishJournal(transactionRoot)
	if err != nil {
		return err
	}
	stagedBookDir := filepath.Join(transactionRoot, "book")
	stagedManifest := filepath.Join(transactionRoot, "manifest.json")
	backupBookDir := filepath.Join(transactionRoot, "backup-book")
	if err := os.MkdirAll(filepath.Dir(bookDir), os.ModePerm); err != nil {
		return err
	}
	bookExisted, err := movePathToBackup(bookDir, backupBookDir)
	if err != nil {
		return err
	}
	journal.BookExisted = bookExisted
	journal.Phase = bookKnowledgePublishPhaseBackedUp
	rollback := func(publishErr error) error {
		var rollbackErrors []error
		if err := os.RemoveAll(bookDir); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove partial book package: %w", err))
		}
		if err := restorePathFromBackup(backupBookDir, bookDir, bookExisted); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore book package: %w", err))
		}
		if len(rollbackErrors) != 0 {
			return errors.Join(append([]error{errBookKnowledgePublishRecoveryRequired, publishErr}, rollbackErrors...)...)
		}
		return publishErr
	}
	if err := writeBookKnowledgePublishJournal(transactionRoot, journal); err != nil {
		return rollback(err)
	}
	if afterBookBackup != nil {
		if err := afterBookBackup(); err != nil {
			return rollback(err)
		}
	}
	if err := os.Rename(stagedBookDir, bookDir); err != nil {
		return rollback(err)
	}
	journal.Phase = bookKnowledgePublishPhaseInstalled
	if err := writeBookKnowledgePublishJournal(transactionRoot, journal); err != nil {
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

func (s *BookKnowledgeStore) recoverBookKnowledgePublishTransaction(
	job BookKnowledgeJob,
	receipt bookKnowledgeJobCommitReceipt,
) (bool, error) {
	expectedMarker := bookKnowledgeJobCommitMarker{
		Version: bookKnowledgeJobCommitMarkerVersion, JobID: receipt.JobID, PublishNonce: receipt.PublishNonce,
		BookID: receipt.BookID, ContentHash: receipt.ContentHash,
	}
	transactionRoot := bookKnowledgePublishTransactionPath(s.root, expectedMarker)
	journal, err := readBookKnowledgePublishJournal(transactionRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			preparingRoot := bookKnowledgePublishTransactionPreparingPath(s.root, expectedMarker)
			preparingExists, statErr := pathExists(preparingRoot)
			if statErr != nil {
				return false, statErr
			}
			if preparingExists {
				if err := cleanupBookKnowledgePublishTransaction(preparingRoot); err != nil {
					return false, err
				}
			}
			return false, nil
		}
		return false, err
	}
	if journal.Marker != expectedMarker {
		return false, nil
	}
	finalBookDir := s.BookDir(receipt.BookID)
	stagedBookDir := filepath.Join(transactionRoot, "book")
	backupBookDir := filepath.Join(transactionRoot, "backup-book")
	switch journal.Phase {
	case bookKnowledgePublishPhaseRestoringBackup:
		return s.resumeBookKnowledgeBackupRestore(transactionRoot, journal)
	case bookKnowledgePublishPhaseDiscardingOwnedFinal:
		return s.resumeBookKnowledgeOwnedFinalDiscard(transactionRoot, journal)
	}
	finalPackage, finalValid, err := verifyBookKnowledgePublishDirectory(finalBookDir, job, receipt)
	if err != nil {
		return false, err
	}
	if finalValid {
		if err := s.repairBookKnowledgeRootManifest(*finalPackage); err != nil {
			return false, err
		}
		if err := s.cleanupBookKnowledgePublishTransaction(transactionRoot); err != nil {
			return false, err
		}
		return true, nil
	}
	stagedPackage, stagedValid, err := verifyBookKnowledgePublishDirectory(stagedBookDir, job, receipt)
	if err != nil {
		return false, err
	}
	if stagedValid {
		backupExists, err := pathExists(backupBookDir)
		if err != nil {
			return false, err
		}
		finalExists, err := pathExists(finalBookDir)
		if err != nil {
			return false, err
		}
		if finalExists {
			if backupExists {
				return false, fmt.Errorf("%w: current and backup packages both exist", errBookKnowledgePublishAmbiguous)
			}
			if err := os.Rename(finalBookDir, backupBookDir); err != nil {
				return false, err
			}
			journal.BookExisted = true
			journal.Phase = bookKnowledgePublishPhaseBackedUp
			if err := writeBookKnowledgePublishJournal(transactionRoot, journal); err != nil {
				return false, err
			}
		} else if backupExists {
			journal.BookExisted = true
		}
		if err := os.MkdirAll(filepath.Dir(finalBookDir), os.ModePerm); err != nil {
			return false, err
		}
		if err := os.Rename(stagedBookDir, finalBookDir); err != nil {
			return false, err
		}
		journal.Phase = bookKnowledgePublishPhaseInstalled
		if err := writeBookKnowledgePublishJournal(transactionRoot, journal); err != nil {
			return false, err
		}
		if err := s.repairBookKnowledgeRootManifest(*stagedPackage); err != nil {
			return false, err
		}
		if err := s.cleanupBookKnowledgePublishTransaction(transactionRoot); err != nil {
			return false, err
		}
		return true, nil
	}

	backupExists, err := pathExists(backupBookDir)
	if err != nil {
		return false, err
	}
	var backupPackage *BookKnowledgePackage
	if backupExists {
		var backupValid bool
		backupPackage, backupValid, err = verifyBookKnowledgeBackupDirectory(backupBookDir, receipt.BookID)
		if err != nil {
			return false, err
		}
		if !backupValid {
			return false, errBookKnowledgePublishRecoveryRequired
		}
		rollbackBook := backupPackage.Book
		journal.Phase = bookKnowledgePublishPhaseRestoringBackup
		journal.RollbackBook = &rollbackBook
		if err := writeBookKnowledgePublishJournal(transactionRoot, journal); err != nil {
			return false, err
		}
		return s.resumeBookKnowledgeBackupRestore(transactionRoot, journal)
	}
	finalExists, err := pathExists(finalBookDir)
	if err != nil {
		return false, err
	}
	finalOwned := false
	if finalExists {
		marker, markerErr := readBookKnowledgeJobCommitMarker(finalBookDir)
		finalOwned = markerErr == nil && marker == expectedMarker
	}
	if !backupExists && finalExists {
		if !finalOwned {
			return false, fmt.Errorf("%w: damaged current package is not owned by the transaction", errBookKnowledgePublishAmbiguous)
		}
		journal.Phase = bookKnowledgePublishPhaseDiscardingOwnedFinal
		journal.RollbackBook = nil
		if err := writeBookKnowledgePublishJournal(transactionRoot, journal); err != nil {
			return false, err
		}
		return s.resumeBookKnowledgeOwnedFinalDiscard(transactionRoot, journal)
	} else {
		return false, errBookKnowledgePublishRecoveryRequired
	}
}

func (s *BookKnowledgeStore) resumeBookKnowledgeBackupRestore(
	transactionRoot string,
	journal bookKnowledgePublishJournal,
) (bool, error) {
	if journal.Phase != bookKnowledgePublishPhaseRestoringBackup || journal.RollbackBook == nil {
		return false, fmt.Errorf("invalid backup restore journal")
	}
	finalBookDir := s.BookDir(journal.Marker.BookID)
	backupBookDir := filepath.Join(transactionRoot, "backup-book")
	finalPackage, finalValid, err := verifyBookKnowledgeRollbackDirectory(finalBookDir, *journal.RollbackBook)
	if err != nil {
		return false, err
	}
	if finalValid {
		if err := s.repairBookKnowledgeRootManifest(*finalPackage); err != nil {
			return false, err
		}
		if err := s.cleanupBookKnowledgePublishTransaction(transactionRoot); err != nil {
			return false, err
		}
		return false, nil
	}
	backupPackage, backupValid, err := verifyBookKnowledgeRollbackDirectory(backupBookDir, *journal.RollbackBook)
	if err != nil {
		return false, err
	}
	if !backupValid {
		finalExists, statErr := pathExists(finalBookDir)
		if statErr != nil {
			return false, statErr
		}
		if finalExists {
			return false, fmt.Errorf("%w: rollback final does not match journal identity", errBookKnowledgePublishAmbiguous)
		}
		return false, errBookKnowledgePublishRecoveryRequired
	}
	finalExists, err := pathExists(finalBookDir)
	if err != nil {
		return false, err
	}
	if finalExists {
		marker, markerErr := readBookKnowledgeJobCommitMarker(finalBookDir)
		if markerErr != nil || marker != journal.Marker {
			return false, fmt.Errorf("%w: current package is not owned by the transaction", errBookKnowledgePublishAmbiguous)
		}
	}
	if err := s.repairBookKnowledgeRootManifest(*backupPackage); err != nil {
		return false, err
	}
	if finalExists {
		if err := os.RemoveAll(finalBookDir); err != nil {
			return false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(finalBookDir), os.ModePerm); err != nil {
		return false, err
	}
	if err := os.Rename(backupBookDir, finalBookDir); err != nil {
		return false, err
	}
	if err := s.cleanupBookKnowledgePublishTransaction(transactionRoot); err != nil {
		return false, err
	}
	return false, nil
}

func (s *BookKnowledgeStore) resumeBookKnowledgeOwnedFinalDiscard(
	transactionRoot string,
	journal bookKnowledgePublishJournal,
) (bool, error) {
	if journal.Phase != bookKnowledgePublishPhaseDiscardingOwnedFinal || journal.RollbackBook != nil {
		return false, fmt.Errorf("invalid owned final discard journal")
	}
	finalBookDir := s.BookDir(journal.Marker.BookID)
	finalExists, err := pathExists(finalBookDir)
	if err != nil {
		return false, err
	}
	if finalExists {
		marker, markerErr := readBookKnowledgeJobCommitMarker(finalBookDir)
		if markerErr != nil || marker != journal.Marker {
			return false, fmt.Errorf("%w: damaged current package is not owned by the transaction", errBookKnowledgePublishAmbiguous)
		}
		if err := os.RemoveAll(finalBookDir); err != nil {
			return false, err
		}
	}
	if err := s.removeBookKnowledgeRootManifestEntry(journal.Marker.BookID, journal.Marker.ContentHash); err != nil {
		return false, err
	}
	if err := s.cleanupBookKnowledgePublishTransaction(transactionRoot); err != nil {
		return false, err
	}
	return false, nil
}

func verifyBookKnowledgePublishDirectory(
	bookDir string,
	job BookKnowledgeJob,
	receipt bookKnowledgeJobCommitReceipt,
) (*BookKnowledgePackage, bool, error) {
	marker, err := readBookKnowledgeJobCommitMarker(bookDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errBookKnowledgeJobCommitMarkerInvalid) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if marker.Version != bookKnowledgeJobCommitMarkerVersion || marker.JobID != receipt.JobID ||
		marker.PublishNonce != receipt.PublishNonce || marker.BookID != receipt.BookID ||
		marker.ContentHash != receipt.ContentHash {
		return nil, false, nil
	}
	pkg, err := loadBookKnowledgePackageFromDirectory(bookDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if pkg.Book.BookID != receipt.BookID || pkg.Book.DedaoID != job.EbookID || pkg.Book.EnID != job.EbookEnID ||
		pkg.Book.ContentHash != receipt.ContentHash {
		return nil, false, nil
	}
	computedHash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		return nil, false, err
	}
	if computedHash != receipt.ContentHash {
		return nil, false, nil
	}
	return pkg, true, nil
}

func verifyBookKnowledgeBackupDirectory(bookDir, bookID string) (*BookKnowledgePackage, bool, error) {
	pkg, err := loadBookKnowledgePackageFromDirectory(bookDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if pkg.Book.BookID != bookID || strings.TrimSpace(pkg.Book.ContentHash) == "" {
		return nil, false, nil
	}
	computedHash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		return nil, false, err
	}
	if computedHash != pkg.Book.ContentHash {
		return nil, false, nil
	}
	return pkg, true, nil
}

func verifyBookKnowledgeRollbackDirectory(
	bookDir string,
	expected BookKnowledgeBook,
) (*BookKnowledgePackage, bool, error) {
	pkg, err := loadBookKnowledgePackageFromDirectory(bookDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if pkg.Book != expected || strings.TrimSpace(expected.ContentHash) == "" {
		return nil, false, nil
	}
	computedHash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		return nil, false, err
	}
	if computedHash != expected.ContentHash {
		return nil, false, nil
	}
	return pkg, true, nil
}

func (s *BookKnowledgeStore) repairBookKnowledgeRootManifest(pkg BookKnowledgePackage) error {
	manifest, err := s.loadManifest()
	if err != nil {
		return err
	}
	manifest = upsertBookKnowledgeManifest(manifest, pkg.Book)
	payload, err := encodeJSONFile(manifest)
	if err != nil {
		return err
	}
	return writeFileAtomically(s.ManifestPath(), payload)
}

func (s *BookKnowledgeStore) removeBookKnowledgeRootManifestEntry(bookID, contentHash string) error {
	manifest, err := s.loadManifest()
	if err != nil {
		return err
	}
	books := manifest.Books[:0]
	for _, book := range manifest.Books {
		if book.BookID == bookID && book.ContentHash == contentHash {
			continue
		}
		books = append(books, book)
	}
	manifest.Books = books
	payload, err := encodeJSONFile(manifest)
	if err != nil {
		return err
	}
	return writeFileAtomically(s.ManifestPath(), payload)
}

func cleanupBookKnowledgePublishTransaction(transactionRoot string) error {
	if err := os.RemoveAll(transactionRoot); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(transactionRoot))
	return nil
}

func (s *BookKnowledgeStore) cleanupBookKnowledgePublishTransaction(transactionRoot string) error {
	if s.cleanupPackageTransaction != nil {
		return s.cleanupPackageTransaction(transactionRoot)
	}
	return cleanupBookKnowledgePublishTransaction(transactionRoot)
}

func cleanupBookKnowledgePublishTransactionArtifacts(root string, marker bookKnowledgeJobCommitMarker) error {
	transactionRoot := bookKnowledgePublishTransactionPath(root, marker)
	preparingRoot := bookKnowledgePublishTransactionPreparingPath(root, marker)
	var cleanupErrors []error
	for _, path := range []string{transactionRoot, preparingRoot} {
		if err := os.RemoveAll(path); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	_ = os.Remove(filepath.Join(root, bookKnowledgePublishTransactionsDir))
	return errors.Join(cleanupErrors...)
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func publishBookKnowledgePackage(
	stagedBookDir, bookDir, stagedManifest, manifestPath string,
	afterBookBackup func() error,
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
	if afterBookBackup != nil {
		if err := afterBookBackup(); err != nil {
			return rollback(err)
		}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	rootLock, err := s.acquireBookKnowledgeRootLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	if err := rejectPendingBookKnowledgePublishTransaction(s.root, bookID); err != nil {
		return nil, err
	}
	pkg, err := s.loadPackageUnlocked(bookID)
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
	pkg.Book.ContentHash = contentHash
	ctx := contextWithBookKnowledgeDerivedInvalidation(context.Background())
	if err := s.savePackageContextUnlocked(ctx, *pkg); err != nil {
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
	return loadBookKnowledgePackageFromDirectory(s.BookDir(bookID))
}

func loadBookKnowledgePackageFromDirectory(bookDir string) (*BookKnowledgePackage, error) {
	var book BookKnowledgeBook
	if err := readJSONFile(filepath.Join(bookDir, "manifest.json"), &book); err != nil {
		return nil, err
	}

	chapters, err := readJSONLFile[BookKnowledgeChapter](filepath.Join(bookDir, "chapters.jsonl"))
	if err != nil {
		return nil, err
	}
	chunks, err := readJSONLFile[BookKnowledgeChunk](filepath.Join(bookDir, "chunks.jsonl"))
	if err != nil {
		return nil, err
	}
	claims, err := readJSONLFile[BookKnowledgeClaim](filepath.Join(bookDir, "claims.jsonl"))
	if err != nil {
		return nil, err
	}
	citations, err := readJSONLFile[BookKnowledgeCitation](filepath.Join(bookDir, "citations.jsonl"))
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
