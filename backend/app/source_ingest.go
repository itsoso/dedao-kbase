package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	sourceArticleMaxChunkRunes = 1800
	sourceArticleMinRunes      = 32
)

var (
	ErrSourceArticleContentTooShort = errors.New("source article content is too short")
	ErrSourceArticleInvalidURL      = errors.New("source article URL is invalid")
)

type SourceArticleEnvelope struct {
	IdempotencyKey  string            `json:"idempotency_key"`
	SourceType      string            `json:"source_type"`
	SourceAccountID string            `json:"source_account_key"`
	SourceAccount   string            `json:"source_account"`
	SourceItemID    string            `json:"source_item_key"`
	Title           string            `json:"title"`
	Author          string            `json:"author,omitempty"`
	SourceURL       string            `json:"source_url"`
	PublishedAt     string            `json:"published_at,omitempty"`
	Content         string            `json:"content"`
	ContentFormat   string            `json:"content_format"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type SourceIngestReceipt struct {
	IdempotencyKey string `json:"idempotency_key"`
	RunID          string `json:"run_id"`
	ItemID         string `json:"item_id"`
	SourceItemKey  string `json:"source_item_key"`
	Outcome        string `json:"outcome"`
	TargetBookID   string `json:"target_book_id"`
	ContentHash    string `json:"content_hash"`
	AcceptedAt     string `json:"accepted_at"`
}

type SourceDocument struct {
	SourceType      string `json:"source_type"`
	SourceItemKey   string `json:"source_item_key"`
	ContentHash     string `json:"content_hash"`
	TargetBookID    string `json:"target_book_id"`
	SourceTimestamp string `json:"source_timestamp,omitempty"`
	LastSeenAt      string `json:"last_seen_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type SourceIngestService struct {
	books *BookKnowledgeStore
	sync  *SourceSyncStore
	now   func() time.Time
}

func NewSourceIngestService(books *BookKnowledgeStore, syncStore *SourceSyncStore) *SourceIngestService {
	return newSourceIngestService(books, syncStore, time.Now)
}

func newSourceIngestService(books *BookKnowledgeStore, syncStore *SourceSyncStore, now func() time.Time) *SourceIngestService {
	if books == nil {
		books = DefaultBookKnowledgeStore()
	}
	if now == nil {
		now = time.Now
	}
	return &SourceIngestService{books: books, sync: syncStore, now: now}
}

func (s *SourceIngestService) IngestArticle(runID, agentID string, envelope SourceArticleEnvelope) (SourceIngestReceipt, error) {
	if s == nil || s.sync == nil {
		return SourceIngestReceipt{}, fmt.Errorf("source sync store is required")
	}
	runID = strings.TrimSpace(runID)
	agentID = strings.TrimSpace(agentID)
	envelope.IdempotencyKey = strings.TrimSpace(envelope.IdempotencyKey)
	if runID == "" || agentID == "" || envelope.IdempotencyKey == "" {
		return SourceIngestReceipt{}, fmt.Errorf("run_id, agent_id, and idempotency_key are required")
	}
	if receipt, found, err := s.sync.getSourceIngestReceipt(envelope.IdempotencyKey); err != nil {
		return SourceIngestReceipt{}, err
	} else if found {
		return receipt, nil
	}

	normalized, contentHash, err := normalizeSourceArticleEnvelope(envelope)
	if err != nil {
		return SourceIngestReceipt{}, s.recordFailure(runID, agentID, envelope, err)
	}
	if err := s.sync.validateSourceIngestRun(runID, agentID); err != nil {
		return SourceIngestReceipt{}, err
	}

	document, found, err := s.sync.getSourceDocument(normalized.SourceType, normalized.SourceItemID)
	if err != nil {
		return SourceIngestReceipt{}, err
	}
	outcome := SourceItemNew
	targetBookID := deterministicSourceBookID(normalized.SourceType, normalized.SourceItemID)
	createdAt := ""
	if found {
		targetBookID = document.TargetBookID
		if document.ContentHash == contentHash {
			outcome = SourceItemSkipped
		} else {
			outcome = SourceItemUpdated
			existing, loadErr := s.books.LoadPackage(targetBookID)
			if loadErr != nil {
				return SourceIngestReceipt{}, s.recordFailure(runID, agentID, normalized,
					fmt.Errorf("load existing source package: %w", loadErr))
			}
			createdAt = existing.Book.CreatedAt
		}
	}

	acceptedAt := s.now().UTC().Format(time.RFC3339Nano)
	if outcome != SourceItemSkipped {
		pkg := buildSourceArticlePackage(normalized, contentHash, targetBookID, createdAt, acceptedAt)
		if err := s.books.SavePackage(pkg); err != nil {
			return SourceIngestReceipt{}, s.recordFailure(runID, agentID, normalized,
				fmt.Errorf("save source article package: %w", err))
		}
	}

	receipt, err := s.sync.commitSourceIngest(runID, agentID, normalized, contentHash, targetBookID, outcome, acceptedAt)
	if err != nil {
		if replay, found, replayErr := s.sync.getSourceIngestReceipt(normalized.IdempotencyKey); replayErr == nil && found {
			return replay, nil
		}
		return SourceIngestReceipt{}, err
	}
	return receipt, nil
}

func (s *SourceIngestService) recordFailure(runID, agentID string, envelope SourceArticleEnvelope, cause error) error {
	if strings.TrimSpace(envelope.SourceItemID) == "" || strings.TrimSpace(envelope.IdempotencyKey) == "" {
		return cause
	}
	_, err := s.sync.RecordRunItem(runID, agentID, SourceSyncItemInput{
		SourceItemKey:  envelope.SourceItemID,
		IdempotencyKey: envelope.IdempotencyKey,
		Outcome:        SourceItemFailed,
		Error:          cause.Error(),
	})
	if err != nil {
		return fmt.Errorf("%w (record source item failure: %v)", cause, err)
	}
	return cause
}

func normalizeSourceArticleEnvelope(envelope SourceArticleEnvelope) (SourceArticleEnvelope, string, error) {
	envelope.IdempotencyKey = strings.TrimSpace(envelope.IdempotencyKey)
	envelope.SourceType = strings.TrimSpace(envelope.SourceType)
	envelope.SourceAccountID = strings.TrimSpace(envelope.SourceAccountID)
	envelope.SourceAccount = strings.TrimSpace(envelope.SourceAccount)
	envelope.SourceItemID = strings.TrimSpace(envelope.SourceItemID)
	envelope.Title = strings.TrimSpace(envelope.Title)
	envelope.Author = strings.TrimSpace(envelope.Author)
	envelope.PublishedAt = strings.TrimSpace(envelope.PublishedAt)
	envelope.ContentFormat = strings.ToLower(strings.TrimSpace(envelope.ContentFormat))
	if envelope.SourceType == "" || envelope.SourceAccountID == "" || envelope.SourceItemID == "" || envelope.Title == "" {
		return envelope, "", fmt.Errorf("source_type, source_account_key, source_item_key, and title are required")
	}
	if envelope.SourceAccount == "" {
		envelope.SourceAccount = envelope.SourceAccountID
	}
	if envelope.ContentFormat == "" {
		envelope.ContentFormat = "markdown"
	}
	if envelope.ContentFormat != "markdown" {
		return envelope, "", fmt.Errorf("unsupported content_format %q", envelope.ContentFormat)
	}
	canonicalURL, err := canonicalSourceArticleURL(envelope.SourceURL)
	if err != nil {
		return envelope, "", err
	}
	envelope.SourceURL = canonicalURL
	envelope.Content = normalizeSourceArticleContent(envelope.Content)
	if len([]rune(envelope.Content)) < sourceArticleMinRunes {
		return envelope, "", ErrSourceArticleContentTooShort
	}
	sum := sha256.Sum256([]byte(envelope.Content))
	return envelope, hex.EncodeToString(sum[:]), nil
}

func canonicalSourceArticleURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ErrSourceArticleInvalidURL
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		host += ":" + port
	}
	parsed.Host = host
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

func normalizeSourceArticleContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	normalized := make([]string, 0, len(lines))
	blankLines := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blankLines++
			if blankLines > 2 {
				continue
			}
		} else {
			blankLines = 0
		}
		normalized = append(normalized, line)
	}
	return strings.TrimSpace(strings.Join(normalized, "\n"))
}

func deterministicSourceBookID(sourceType, sourceItemKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceType) + "\x00" + strings.TrimSpace(sourceItemKey)))
	prefix := "source"
	if sourceType == "wcplus_wechat_article" {
		prefix = "wcplus"
	}
	return prefix + "-" + hex.EncodeToString(sum[:])[:16]
}

type sourceMarkdownSection struct {
	title string
	text  string
}

func buildSourceArticlePackage(envelope SourceArticleEnvelope, contentHash, bookID, createdAt, updatedAt string) BookKnowledgePackage {
	if strings.TrimSpace(createdAt) == "" {
		createdAt = updatedAt
	}
	sections := splitSourceMarkdownSections(envelope.Title, envelope.Content)
	chapters := make([]BookKnowledgeChapter, 0, len(sections))
	chunks := make([]BookKnowledgeChunk, 0)
	citations := make([]BookKnowledgeCitation, 0)
	chunkOrder := 0
	for sectionIndex, section := range sections {
		chapterID := bookID + "-chapter-" + strconv.Itoa(sectionIndex+1)
		chapter := BookKnowledgeChapter{
			ChapterID: chapterID,
			BookID:    bookID,
			Order:     sectionIndex + 1,
			Title:     section.title,
			Summary:   trimRunes(section.text, 240),
		}
		for _, text := range splitBookKnowledgeText(section.text, sourceArticleMaxChunkRunes) {
			chunkOrder++
			chunkID := bookID + "-chunk-" + strconv.Itoa(chunkOrder)
			citationID := bookID + "-citation-" + strconv.Itoa(chunkOrder)
			chapter.ChunkIDs = append(chapter.ChunkIDs, chunkID)
			chunks = append(chunks, BookKnowledgeChunk{
				ChunkID:   chunkID,
				BookID:    bookID,
				ChapterID: chapterID,
				Order:     chunkOrder,
				Text:      text,
				Tokens:    estimateBookTokens(text),
			})
			citations = append(citations, BookKnowledgeCitation{
				CitationID:    citationID,
				BookID:        bookID,
				ChapterID:     chapterID,
				ChunkID:       chunkID,
				SourceHTML:    envelope.SourceURL,
				Anchor:        section.title,
				Note:          "normalized source article",
				SourceType:    envelope.SourceType,
				SourceAccount: envelope.SourceAccount,
				SourceItemKey: envelope.SourceItemID,
				PublishedAt:   envelope.PublishedAt,
			})
		}
		chapters = append(chapters, chapter)
	}
	return BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:        bookID,
			Title:         envelope.Title,
			Author:        envelope.Author,
			SourceHTML:    envelope.SourceURL,
			SourceType:    envelope.SourceType,
			SourceKey:     envelope.SourceItemID,
			SourceAccount: envelope.SourceAccount,
			PublishedAt:   envelope.PublishedAt,
			ContentHash:   contentHash,
			CreatedAt:     createdAt,
			UpdatedAt:     updatedAt,
			Status:        "ready",
			Extractor:     "source-ingest-v1",
		},
		Chapters:  chapters,
		Chunks:    chunks,
		Claims:    []BookKnowledgeClaim{},
		Citations: citations,
	}
}

func splitSourceMarkdownSections(title, content string) []sourceMarkdownSection {
	lines := strings.Split(content, "\n")
	sections := make([]sourceMarkdownSection, 0)
	currentTitle := strings.TrimSpace(title)
	currentLines := make([]string, 0)
	flush := func() {
		text := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if text == "" {
			return
		}
		sections = append(sections, sourceMarkdownSection{title: currentTitle, text: text})
		currentLines = nil
	}
	for _, line := range lines {
		if heading, ok := sourceMarkdownHeading(line); ok {
			flush()
			currentTitle = heading
			currentLines = append(currentLines, line)
			continue
		}
		currentLines = append(currentLines, line)
	}
	flush()
	if len(sections) == 0 {
		sections = append(sections, sourceMarkdownSection{title: title, text: content})
	}
	return sections
}

func sourceMarkdownHeading(line string) (string, bool) {
	line = strings.TrimSpace(line)
	count := 0
	for count < len(line) && count < 6 && line[count] == '#' {
		count++
	}
	if count == 0 || count >= len(line) || line[count] != ' ' {
		return "", false
	}
	title := strings.TrimSpace(line[count:])
	return title, title != ""
}

func (s *SourceSyncStore) validateSourceIngestRun(runID, agentID string) error {
	run, err := s.GetRun(runID)
	if err != nil {
		return err
	}
	if run.Status != SourceRunRunning {
		if isTerminalSourceRunStatus(run.Status) {
			return ErrSourceRunTerminal
		}
		return ErrSourceRunInvalidState
	}
	if run.LeaseOwner != strings.TrimSpace(agentID) {
		return ErrSourceRunLeaseOwner
	}
	if sourceLeaseExpired(run.LeaseExpiresAt, s.now()) {
		return ErrSourceRunLeaseExpired
	}
	return nil
}

func (s *SourceSyncStore) getSourceDocument(sourceType, sourceItemKey string) (SourceDocument, bool, error) {
	var document SourceDocument
	err := s.db.QueryRow(`
		SELECT source_type, source_item_key, content_hash, target_book_id, source_timestamp,
			last_seen_at, created_at, updated_at
		FROM source_documents WHERE source_type = ? AND source_item_key = ?
	`, strings.TrimSpace(sourceType), strings.TrimSpace(sourceItemKey)).Scan(
		&document.SourceType, &document.SourceItemKey, &document.ContentHash, &document.TargetBookID,
		&document.SourceTimestamp, &document.LastSeenAt, &document.CreatedAt, &document.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceDocument{}, false, nil
	}
	return document, err == nil, err
}

func (s *SourceSyncStore) getSourceIngestReceipt(idempotencyKey string) (SourceIngestReceipt, bool, error) {
	var receipt SourceIngestReceipt
	err := s.db.QueryRow(`
		SELECT receipt.idempotency_key, receipt.run_id, receipt.item_id,
			item.source_item_key, receipt.outcome, receipt.target_book_id,
			item.content_hash, receipt.accepted_at
		FROM source_outbox_receipts AS receipt
		JOIN source_sync_items AS item ON item.id = receipt.item_id
		WHERE receipt.idempotency_key = ?
	`, strings.TrimSpace(idempotencyKey)).Scan(
		&receipt.IdempotencyKey, &receipt.RunID, &receipt.ItemID, &receipt.SourceItemKey,
		&receipt.Outcome, &receipt.TargetBookID, &receipt.ContentHash, &receipt.AcceptedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceIngestReceipt{}, false, nil
	}
	return receipt, err == nil, err
}

func (s *SourceSyncStore) commitSourceIngest(
	runID, agentID string,
	envelope SourceArticleEnvelope,
	contentHash, targetBookID, outcome, acceptedAt string,
) (SourceIngestReceipt, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return SourceIngestReceipt{}, err
	}
	defer tx.Rollback()
	var status, leaseOwner, leaseExpiresAt string
	if err := tx.QueryRow(`SELECT status, lease_owner, lease_expires_at FROM source_sync_runs WHERE id = ?`, runID).
		Scan(&status, &leaseOwner, &leaseExpiresAt); errors.Is(err, sql.ErrNoRows) {
		return SourceIngestReceipt{}, ErrSourceRunNotFound
	} else if err != nil {
		return SourceIngestReceipt{}, err
	}
	if status != SourceRunRunning {
		if isTerminalSourceRunStatus(status) {
			return SourceIngestReceipt{}, ErrSourceRunTerminal
		}
		return SourceIngestReceipt{}, ErrSourceRunInvalidState
	}
	if leaseOwner != strings.TrimSpace(agentID) {
		return SourceIngestReceipt{}, ErrSourceRunLeaseOwner
	}
	if sourceLeaseExpired(leaseExpiresAt, s.now()) {
		return SourceIngestReceipt{}, ErrSourceRunLeaseExpired
	}

	itemID := newSourceSyncID("item", s.now())
	_, err = tx.Exec(`
		INSERT INTO source_sync_items (
			id, run_id, source_item_key, idempotency_key, content_hash, outcome,
			target_book_id, error_text, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?)
		ON CONFLICT(run_id, source_item_key) DO UPDATE SET
			idempotency_key = excluded.idempotency_key,
			content_hash = excluded.content_hash,
			outcome = excluded.outcome,
			target_book_id = excluded.target_book_id,
			error_text = '',
			updated_at = excluded.updated_at
	`, itemID, runID, envelope.SourceItemID, envelope.IdempotencyKey, contentHash,
		outcome, targetBookID, acceptedAt, acceptedAt)
	if err != nil {
		return SourceIngestReceipt{}, err
	}
	if err := tx.QueryRow(`SELECT id FROM source_sync_items WHERE run_id = ? AND source_item_key = ?`, runID, envelope.SourceItemID).Scan(&itemID); err != nil {
		return SourceIngestReceipt{}, err
	}
	_, err = tx.Exec(`
		INSERT INTO source_documents (
			source_type, source_item_key, content_hash, target_book_id, source_timestamp,
			last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_type, source_item_key) DO UPDATE SET
			content_hash = excluded.content_hash,
			target_book_id = excluded.target_book_id,
			source_timestamp = excluded.source_timestamp,
			last_seen_at = excluded.last_seen_at,
			updated_at = CASE WHEN ? = ? THEN source_documents.updated_at ELSE excluded.updated_at END
	`, envelope.SourceType, envelope.SourceItemID, contentHash, targetBookID, envelope.PublishedAt,
		acceptedAt, acceptedAt, acceptedAt, outcome, SourceItemSkipped)
	if err != nil {
		return SourceIngestReceipt{}, err
	}
	_, err = tx.Exec(`
		INSERT INTO source_outbox_receipts (
			idempotency_key, run_id, item_id, outcome, target_book_id, accepted_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, envelope.IdempotencyKey, runID, itemID, outcome, targetBookID, acceptedAt)
	if err != nil {
		return SourceIngestReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return SourceIngestReceipt{}, err
	}
	return SourceIngestReceipt{
		IdempotencyKey: envelope.IdempotencyKey,
		RunID:          runID,
		ItemID:         itemID,
		SourceItemKey:  envelope.SourceItemID,
		Outcome:        outcome,
		TargetBookID:   targetBookID,
		ContentHash:    contentHash,
		AcceptedAt:     acceptedAt,
	}, nil
}
