package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const knowledgeCatalogDBName = "knowledge_catalog.sqlite3"

type KnowledgeCatalogStore struct {
	root string
	db   *sql.DB
	now  func() time.Time
}

type KnowledgeCatalogSource struct {
	SourceID         string `json:"source_id"`
	SourceType       string `json:"source_type"`
	SourceAccountKey string `json:"source_account_key,omitempty"`
	SourceAccount    string `json:"source_account,omitempty"`
	SourceItemKey    string `json:"source_item_key"`
	CanonicalURI     string `json:"canonical_uri,omitempty"`
	LicenseScope     string `json:"license_scope,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type KnowledgeContentVersion struct {
	ContentVersionID     string `json:"content_version_id"`
	SourceID             string `json:"source_id"`
	ContentHash          string `json:"content_hash"`
	TargetBookID         string `json:"target_book_id"`
	ArtifactRef          string `json:"artifact_ref"`
	PredecessorVersionID string `json:"predecessor_version_id,omitempty"`
	IsCurrent            bool   `json:"is_current"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

type KnowledgeCatalogRecord struct {
	Source           KnowledgeCatalogSource  `json:"source"`
	Version          KnowledgeContentVersion `json:"version"`
	DuplicateGroupID string                  `json:"duplicate_group_id,omitempty"`
}

func NewKnowledgeCatalogStore(root string, now func() time.Time) (*KnowledgeCatalogStore, error) {
	if strings.TrimSpace(root) == "" {
		root = DefaultBookKnowledgeRoot()
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(root, os.ModePerm); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, knowledgeCatalogDBName))
	if err != nil {
		return nil, err
	}
	store := &KnowledgeCatalogStore{root: root, db: db, now: now}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *KnowledgeCatalogStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *KnowledgeCatalogStore) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS knowledge_sources (
			source_id TEXT PRIMARY KEY,
			source_type TEXT NOT NULL,
			source_account_key TEXT NOT NULL DEFAULT '',
			source_account TEXT NOT NULL DEFAULT '',
			source_item_key TEXT NOT NULL,
			canonical_uri TEXT NOT NULL DEFAULT '',
			license_scope TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(source_type, source_item_key)
		)`,
		`CREATE TABLE IF NOT EXISTS knowledge_content_versions (
			content_version_id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			target_book_id TEXT NOT NULL,
			artifact_ref TEXT NOT NULL,
			predecessor_version_id TEXT NOT NULL DEFAULT '',
			is_current INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(source_id, content_hash)
		)`,
		`CREATE TABLE IF NOT EXISTS knowledge_duplicate_groups (
			duplicate_group_id TEXT PRIMARY KEY,
			content_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS knowledge_pipeline_projections (
			book_id TEXT PRIMARY KEY,
			content_hash TEXT NOT NULL,
			stage TEXT NOT NULL,
			input_fingerprint TEXT NOT NULL,
			output_ref TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			public_error_code TEXT NOT NULL DEFAULT '',
			last_published_release_id TEXT NOT NULL DEFAULT '',
			last_published_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS knowledge_delivery_receipts (
			receipt_id TEXT PRIMARY KEY,
			schema_version TEXT NOT NULL,
			consumer TEXT NOT NULL,
			release_id TEXT NOT NULL,
			idempotency_key TEXT NOT NULL UNIQUE,
			disposition TEXT NOT NULL,
			imported_fingerprint TEXT NOT NULL DEFAULT '',
			received_at TEXT NOT NULL,
			payload_hash TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *KnowledgeCatalogStore) RecordContentVersion(envelope SourceArticleEnvelope, contentHash, targetBookID, artifactRef string) (KnowledgeCatalogRecord, error) {
	if s == nil || s.db == nil {
		return KnowledgeCatalogRecord{}, fmt.Errorf("knowledge catalog store is required")
	}
	envelope.SourceType = strings.TrimSpace(envelope.SourceType)
	envelope.SourceAccountID = strings.TrimSpace(envelope.SourceAccountID)
	envelope.SourceAccount = strings.TrimSpace(envelope.SourceAccount)
	envelope.SourceItemID = strings.TrimSpace(envelope.SourceItemID)
	envelope.SourceURL = strings.TrimSpace(envelope.SourceURL)
	contentHash = strings.TrimSpace(contentHash)
	targetBookID = strings.TrimSpace(targetBookID)
	artifactRef = filepath.ToSlash(strings.TrimSpace(artifactRef))
	if envelope.SourceType == "" || envelope.SourceItemID == "" || contentHash == "" || targetBookID == "" || artifactRef == "" {
		return KnowledgeCatalogRecord{}, fmt.Errorf("source_type, source_item_key, content_hash, target_book_id, and artifact_ref are required")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	sourceID := stableKnowledgeID("source", envelope.SourceType, envelope.SourceItemID)
	versionID := stableKnowledgeID("version", sourceID, contentHash)
	duplicateGroupID := stableKnowledgeID("duplicate", contentHash)

	tx, err := s.db.Begin()
	if err != nil {
		return KnowledgeCatalogRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`INSERT INTO knowledge_sources (
		source_id, source_type, source_account_key, source_account, source_item_key, canonical_uri, license_scope, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(source_id) DO UPDATE SET
		source_account_key=excluded.source_account_key,
		source_account=excluded.source_account,
		canonical_uri=excluded.canonical_uri,
		updated_at=excluded.updated_at`,
		sourceID, envelope.SourceType, envelope.SourceAccountID, envelope.SourceAccount, envelope.SourceItemID, envelope.SourceURL, "personal_use", now, now,
	); err != nil {
		return KnowledgeCatalogRecord{}, err
	}
	if _, err := tx.Exec(`INSERT INTO knowledge_duplicate_groups (duplicate_group_id, content_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(content_hash) DO UPDATE SET updated_at=excluded.updated_at`, duplicateGroupID, contentHash, now, now); err != nil {
		return KnowledgeCatalogRecord{}, err
	}
	var existingVersionID string
	if err := tx.QueryRow(`SELECT content_version_id FROM knowledge_content_versions WHERE source_id = ? AND content_hash = ?`, sourceID, contentHash).Scan(&existingVersionID); err != nil && err != sql.ErrNoRows {
		return KnowledgeCatalogRecord{}, err
	} else if err == nil {
		if err := tx.Commit(); err != nil {
			return KnowledgeCatalogRecord{}, err
		}
		return s.FindSourceVersion(envelope.SourceType, envelope.SourceItemID)
	}
	var predecessor string
	if err := tx.QueryRow(`SELECT content_version_id FROM knowledge_content_versions WHERE source_id = ? AND is_current = 1`, sourceID).Scan(&predecessor); err != nil && err != sql.ErrNoRows {
		return KnowledgeCatalogRecord{}, err
	}
	if _, err := tx.Exec(`UPDATE knowledge_content_versions SET is_current = 0, updated_at = ? WHERE source_id = ?`, now, sourceID); err != nil {
		return KnowledgeCatalogRecord{}, err
	}
	if _, err := tx.Exec(`INSERT INTO knowledge_content_versions (
		content_version_id, source_id, content_hash, target_book_id, artifact_ref, predecessor_version_id, is_current, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		versionID, sourceID, contentHash, targetBookID, artifactRef, predecessor, now, now,
	); err != nil {
		return KnowledgeCatalogRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return KnowledgeCatalogRecord{}, err
	}
	return s.FindSourceVersion(envelope.SourceType, envelope.SourceItemID)
}

func (s *KnowledgeCatalogStore) FindSourceVersion(sourceType, sourceItemKey string) (KnowledgeCatalogRecord, error) {
	sourceID := stableKnowledgeID("source", strings.TrimSpace(sourceType), strings.TrimSpace(sourceItemKey))
	source, err := s.loadSource(sourceID)
	if err != nil {
		return KnowledgeCatalogRecord{}, err
	}
	version, err := s.CurrentContentVersion(sourceID)
	if err != nil {
		return KnowledgeCatalogRecord{}, err
	}
	return KnowledgeCatalogRecord{Source: source, Version: version, DuplicateGroupID: stableKnowledgeID("duplicate", version.ContentHash)}, nil
}

func (s *KnowledgeCatalogStore) CurrentContentVersion(sourceID string) (KnowledgeContentVersion, error) {
	row := s.db.QueryRow(`SELECT content_version_id, source_id, content_hash, target_book_id, artifact_ref, predecessor_version_id, is_current, created_at, updated_at
		FROM knowledge_content_versions WHERE source_id = ? AND is_current = 1`, strings.TrimSpace(sourceID))
	return scanKnowledgeContentVersion(row)
}

func (s *KnowledgeCatalogStore) ListContentVersions(sourceID string) ([]KnowledgeContentVersion, error) {
	rows, err := s.db.Query(`SELECT content_version_id, source_id, content_hash, target_book_id, artifact_ref, predecessor_version_id, is_current, created_at, updated_at
		FROM knowledge_content_versions WHERE source_id = ? ORDER BY created_at, content_version_id`, strings.TrimSpace(sourceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []KnowledgeContentVersion
	for rows.Next() {
		version, err := scanKnowledgeContentVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func RebuildKnowledgeCatalog(root string, bookStore *BookKnowledgeStore, now func() time.Time) (*KnowledgeCatalogStore, error) {
	if bookStore == nil {
		bookStore = NewBookKnowledgeStore(root)
	}
	if err := os.Remove(filepath.Join(root, knowledgeCatalogDBName)); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	catalog, err := NewKnowledgeCatalogStore(root, now)
	if err != nil {
		return nil, err
	}
	books, err := bookStore.ListBooks()
	if err != nil {
		return nil, err
	}
	for _, book := range books {
		if strings.TrimSpace(book.SourceType) == "" || strings.TrimSpace(book.SourceKey) == "" || strings.TrimSpace(book.ContentHash) == "" {
			continue
		}
		envelope := SourceArticleEnvelope{
			SourceType:    book.SourceType,
			SourceAccount: book.SourceAccount,
			SourceItemID:  book.SourceKey,
			Title:         book.Title,
			SourceURL:     book.SourceHTML,
			PublishedAt:   book.PublishedAt,
			ContentFormat: "markdown",
		}
		if _, err := catalog.RecordContentVersion(envelope, book.ContentHash, book.BookID, filepath.ToSlash(filepath.Join("books", sanitizeBookKnowledgeID(book.BookID), "manifest.json"))); err != nil {
			return nil, err
		}
	}
	return catalog, nil
}

func (s *KnowledgeCatalogStore) loadSource(sourceID string) (KnowledgeCatalogSource, error) {
	row := s.db.QueryRow(`SELECT source_id, source_type, source_account_key, source_account, source_item_key, canonical_uri, license_scope, created_at, updated_at
		FROM knowledge_sources WHERE source_id = ?`, strings.TrimSpace(sourceID))
	var source KnowledgeCatalogSource
	err := row.Scan(&source.SourceID, &source.SourceType, &source.SourceAccountKey, &source.SourceAccount, &source.SourceItemKey, &source.CanonicalURI, &source.LicenseScope, &source.CreatedAt, &source.UpdatedAt)
	return source, err
}

type knowledgeContentVersionScanner interface {
	Scan(dest ...any) error
}

func scanKnowledgeContentVersion(scanner knowledgeContentVersionScanner) (KnowledgeContentVersion, error) {
	var version KnowledgeContentVersion
	var current int
	err := scanner.Scan(&version.ContentVersionID, &version.SourceID, &version.ContentHash, &version.TargetBookID, &version.ArtifactRef, &version.PredecessorVersionID, &current, &version.CreatedAt, &version.UpdatedAt)
	version.IsCurrent = current == 1
	return version, err
}

func stableKnowledgeID(prefix string, parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, strings.TrimSpace(part))
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x00")))
	return prefix + "-" + hex.EncodeToString(sum[:])[:24]
}
