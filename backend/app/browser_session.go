package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	defaultBrowserSessionTTL             = 30 * 24 * time.Hour
	defaultBrowserSessionRenewalInterval = 5 * time.Minute
	defaultBrowserSessionMaxActive       = 10
	browserSessionCredentialBytes        = 32
	browserSessionSchemaVersion          = 1
	browserSessionTimeLayout             = "2006-01-02T15:04:05.000000000Z07:00"
)

type BrowserSessionStoreConfig struct {
	Path            string
	Now             func() time.Time
	Random          io.Reader
	TTL             time.Duration
	RenewalInterval time.Duration
	MaxActive       int
}

type BrowserSession struct {
	ID           string     `json:"id"`
	DeviceLabel  string     `json:"device_label"`
	CreatedAt    time.Time  `json:"created_at"`
	LastActiveAt time.Time  `json:"last_active_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	RevokeReason string     `json:"revoke_reason,omitempty"`
}

type BrowserSessionCredentials struct {
	Session   BrowserSession
	Token     string
	CSRFToken string
}

type BrowserSessionCreate struct {
	DeviceLabel string
	UserAgent   string
}

type BrowserSessionStore struct {
	dbPath          string
	now             func() time.Time
	random          io.Reader
	ttl             time.Duration
	renewalInterval time.Duration
	maxActive       int
	db              *sql.DB
	mu              sync.Mutex
	closed          bool
}

func NewBrowserSessionStore(config BrowserSessionStoreConfig) (*BrowserSessionStore, error) {
	dbPath := strings.TrimSpace(config.Path)
	if dbPath == "" {
		return nil, fmt.Errorf("browser session database path is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.TTL <= 0 {
		config.TTL = defaultBrowserSessionTTL
	}
	if config.RenewalInterval <= 0 {
		config.RenewalInterval = defaultBrowserSessionRenewalInterval
	}
	if config.MaxActive <= 0 {
		config.MaxActive = defaultBrowserSessionMaxActive
	}
	if err := prepareBrowserSessionDirectory(filepath.Dir(dbPath)); err != nil {
		return nil, fmt.Errorf("prepare browser session database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open browser session database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure browser session database busy timeout: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable browser session database foreign keys: %w", err)
	}
	if err := migrateBrowserSessionDB(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate browser session database: %w", err)
	}

	return &BrowserSessionStore{
		dbPath:          dbPath,
		now:             config.Now,
		random:          config.Random,
		ttl:             config.TTL,
		renewalInterval: config.RenewalInterval,
		maxActive:       config.MaxActive,
		db:              db,
	}, nil
}

func (s *BrowserSessionStore) DBPath() string {
	if s == nil {
		return ""
	}
	return s.dbPath
}

func (s *BrowserSessionStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.db == nil {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close browser session store: %w", err)
	}
	return nil
}

func (s *BrowserSessionStore) Create(input BrowserSessionCreate) (BrowserSessionCredentials, error) {
	if s == nil {
		return BrowserSessionCredentials{}, fmt.Errorf("create browser session: browser session store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return BrowserSessionCredentials{}, fmt.Errorf("create browser session: browser session store is closed")
	}

	token, err := s.randomCredential()
	if err != nil {
		return BrowserSessionCredentials{}, fmt.Errorf("generate session credential: %w", err)
	}
	csrfToken, err := s.randomCredential()
	if err != nil {
		return BrowserSessionCredentials{}, fmt.Errorf("generate CSRF credential: %w", err)
	}

	tokenHash := sha256.Sum256([]byte(token))
	csrfHash := sha256.Sum256([]byte(csrfToken))
	userAgentHash := sha256.Sum256([]byte(input.UserAgent))
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)
	session := BrowserSession{
		ID:           "session_" + base64.RawURLEncoding.EncodeToString(tokenHash[:18]),
		DeviceLabel:  strings.TrimSpace(input.DeviceLabel),
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    expiresAt,
	}

	_, err = s.db.Exec(`
		INSERT INTO browser_sessions (
			id, token_hash, csrf_hash, csrf_expires_at, device_label,
			user_agent_hash, created_at, last_active_at, expires_at,
			revoked_at, revoke_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', '')
	`,
		session.ID,
		tokenHash[:],
		csrfHash[:],
		formatBrowserSessionTime(expiresAt),
		session.DeviceLabel,
		userAgentHash[:],
		formatBrowserSessionTime(now),
		formatBrowserSessionTime(now),
		formatBrowserSessionTime(expiresAt),
	)
	if err != nil {
		return BrowserSessionCredentials{}, fmt.Errorf("insert browser session: %w", err)
	}
	return BrowserSessionCredentials{
		Session:   session,
		Token:     token,
		CSRFToken: csrfToken,
	}, nil
}

func (s *BrowserSessionStore) randomCredential() (string, error) {
	randomBytes := make([]byte, browserSessionCredentialBytes)
	_, err := io.ReadFull(s.random, randomBytes)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func formatBrowserSessionTime(value time.Time) string {
	return value.UTC().Format(browserSessionTimeLayout)
}

func parseBrowserSessionTime(value string) (time.Time, error) {
	parsed, err := time.Parse(browserSessionTimeLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse browser session time: %w", err)
	}
	parsed = parsed.UTC()
	if formatBrowserSessionTime(parsed) != value {
		return time.Time{}, fmt.Errorf("parse browser session time: value is not canonical UTC nanoseconds")
	}
	return parsed, nil
}

func prepareBrowserSessionDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", directory)
	}
	currentMode := info.Mode().Perm()
	safeMode := currentMode & 0o750
	if safeMode != currentMode {
		if err := os.Chmod(directory, safeMode); err != nil {
			return err
		}
	}
	return nil
}

func migrateBrowserSessionDB(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin browser session schema migration: %w", err)
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read browser session schema version: %w", err)
	}
	if version > browserSessionSchemaVersion {
		return fmt.Errorf(
			"unsupported browser session database version %d (maximum supported is %d)",
			version,
			browserSessionSchemaVersion,
		)
	}

	switch version {
	case 0:
		exists, err := browserSessionTableExists(tx)
		if err != nil {
			return err
		}
		if exists {
			if err := validateLegacyBrowserSessionSchema(tx); err != nil {
				return fmt.Errorf("validate legacy browser session schema version 0: %w", err)
			}
			if err := rebuildBrowserSessionTableV1(tx); err != nil {
				return fmt.Errorf("rebuild browser session schema version 1: %w", err)
			}
		} else {
			if err := createBrowserSessionTableV1(tx, "browser_sessions"); err != nil {
				return fmt.Errorf("create browser session schema version 1: %w", err)
			}
			if err := createBrowserSessionActiveIndex(tx); err != nil {
				return fmt.Errorf("create browser session active index: %w", err)
			}
		}
		if _, err := tx.Exec(`PRAGMA user_version = 1`); err != nil {
			return fmt.Errorf("set browser session schema version 1: %w", err)
		}
		if err := validateBrowserSessionSchemaV1(tx); err != nil {
			return fmt.Errorf("validate browser session schema version 1: %w", err)
		}
	case browserSessionSchemaVersion:
		if err := validateBrowserSessionSchemaV1(tx); err != nil {
			return fmt.Errorf("validate browser session schema version 1: %w", err)
		}
	default:
		return fmt.Errorf("unsupported browser session database version %d", version)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit browser session schema migration: %w", err)
	}
	return nil
}

func browserSessionTableExists(tx *sql.Tx) (bool, error) {
	var count int
	if err := tx.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = 'browser_sessions'
	`).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect browser session table: %w", err)
	}
	return count == 1, nil
}

func createBrowserSessionTableV1(tx *sql.Tx, tableName string) error {
	var statement string
	switch tableName {
	case "browser_sessions":
		statement = `
		CREATE TABLE browser_sessions (
			id TEXT PRIMARY KEY,
			token_hash BLOB NOT NULL UNIQUE,
			csrf_hash BLOB NOT NULL,
			csrf_expires_at TEXT NOT NULL,
			device_label TEXT NOT NULL,
			user_agent_hash BLOB NOT NULL,
			created_at TEXT NOT NULL,
			last_active_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT NOT NULL DEFAULT '',
			revoke_reason TEXT NOT NULL DEFAULT ''
		)`
	case "browser_sessions_v1":
		statement = `
		CREATE TABLE browser_sessions_v1 (
			id TEXT PRIMARY KEY,
			token_hash BLOB NOT NULL UNIQUE,
			csrf_hash BLOB NOT NULL,
			csrf_expires_at TEXT NOT NULL,
			device_label TEXT NOT NULL,
			user_agent_hash BLOB NOT NULL,
			created_at TEXT NOT NULL,
			last_active_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT NOT NULL DEFAULT '',
			revoke_reason TEXT NOT NULL DEFAULT ''
		)`
	default:
		return fmt.Errorf("unsupported browser session migration table %q", tableName)
	}
	if _, err := tx.Exec(statement); err != nil {
		return fmt.Errorf("create %s table: %w", tableName, err)
	}
	return nil
}

func createBrowserSessionActiveIndex(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE INDEX idx_browser_sessions_active
			ON browser_sessions(revoked_at, expires_at, last_active_at)
	`); err != nil {
		return err
	}
	return nil
}

func rebuildBrowserSessionTableV1(tx *sql.Tx) error {
	if err := createBrowserSessionTableV1(tx, "browser_sessions_v1"); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO browser_sessions_v1 (
			id, token_hash, csrf_hash, csrf_expires_at, device_label,
			user_agent_hash, created_at, last_active_at, expires_at,
			revoked_at, revoke_reason
		)
		SELECT
			id, token_hash, csrf_hash, csrf_expires_at, device_label,
			user_agent_hash, created_at, last_active_at, expires_at,
			COALESCE(revoked_at, ''), revoke_reason
		FROM browser_sessions
	`); err != nil {
		return fmt.Errorf("copy legacy browser sessions: %w", err)
	}
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_browser_sessions_active_token`); err != nil {
		return fmt.Errorf("drop legacy browser session index: %w", err)
	}
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_browser_sessions_active`); err != nil {
		return fmt.Errorf("drop previous browser session active index: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE browser_sessions`); err != nil {
		return fmt.Errorf("drop legacy browser session table: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE browser_sessions_v1 RENAME TO browser_sessions`); err != nil {
		return fmt.Errorf("activate browser session schema version 1: %w", err)
	}
	if err := createBrowserSessionActiveIndex(tx); err != nil {
		return fmt.Errorf("create browser session active index: %w", err)
	}
	return nil
}

type browserSessionColumnInfo struct {
	name         string
	dataType     string
	notNull      bool
	defaultValue sql.NullString
	primaryKey   int
}

var browserSessionColumnsV1 = []browserSessionColumnInfo{
	{name: "id", dataType: "TEXT", primaryKey: 1},
	{name: "token_hash", dataType: "BLOB", notNull: true},
	{name: "csrf_hash", dataType: "BLOB", notNull: true},
	{name: "csrf_expires_at", dataType: "TEXT", notNull: true},
	{name: "device_label", dataType: "TEXT", notNull: true},
	{name: "user_agent_hash", dataType: "BLOB", notNull: true},
	{name: "created_at", dataType: "TEXT", notNull: true},
	{name: "last_active_at", dataType: "TEXT", notNull: true},
	{name: "expires_at", dataType: "TEXT", notNull: true},
	{name: "revoked_at", dataType: "TEXT", notNull: true, defaultValue: sql.NullString{String: "''", Valid: true}},
	{name: "revoke_reason", dataType: "TEXT", notNull: true, defaultValue: sql.NullString{String: "''", Valid: true}},
}

func readBrowserSessionColumns(tx *sql.Tx) ([]browserSessionColumnInfo, error) {
	rows, err := tx.Query(`PRAGMA table_info(browser_sessions)`)
	if err != nil {
		return nil, fmt.Errorf("inspect browser session columns: %w", err)
	}
	defer rows.Close()
	columns := make([]browserSessionColumnInfo, 0, len(browserSessionColumnsV1))
	for rows.Next() {
		var (
			cid, notNull, primaryKey int
			column                   browserSessionColumnInfo
		)
		if err := rows.Scan(
			&cid,
			&column.name,
			&column.dataType,
			&notNull,
			&column.defaultValue,
			&primaryKey,
		); err != nil {
			return nil, fmt.Errorf("scan browser session column: %w", err)
		}
		column.dataType = strings.ToUpper(column.dataType)
		column.notNull = notNull != 0
		column.primaryKey = primaryKey
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate browser session columns: %w", err)
	}
	return columns, nil
}

func validateBrowserSessionSchemaV1(tx *sql.Tx) error {
	columns, err := readBrowserSessionColumns(tx)
	if err != nil {
		return err
	}
	if err := compareBrowserSessionColumns(columns, browserSessionColumnsV1); err != nil {
		return err
	}
	return validateBrowserSessionIndexes(tx, false)
}

func validateLegacyBrowserSessionSchema(tx *sql.Tx) error {
	columns, err := readBrowserSessionColumns(tx)
	if err != nil {
		return err
	}
	if len(columns) != len(browserSessionColumnsV1) {
		return fmt.Errorf("legacy browser_sessions has %d columns, want %d", len(columns), len(browserSessionColumnsV1))
	}
	for index, expected := range browserSessionColumnsV1 {
		actual := columns[index]
		if actual.name != expected.name ||
			actual.dataType != expected.dataType ||
			actual.primaryKey != expected.primaryKey {
			return fmt.Errorf("legacy browser_sessions column %d is incompatible", index)
		}
		switch actual.name {
		case "device_label":
			if !actual.notNull ||
				(actual.defaultValue.Valid && actual.defaultValue.String != "''") {
				return fmt.Errorf("legacy device_label column is incompatible")
			}
		case "revoked_at":
			validNullable := !actual.notNull && !actual.defaultValue.Valid
			validRequired := actual.notNull && actual.defaultValue.Valid && actual.defaultValue.String == "''"
			if !validNullable && !validRequired {
				return fmt.Errorf("legacy revoked_at column is incompatible")
			}
		default:
			if actual.notNull != expected.notNull ||
				actual.defaultValue != expected.defaultValue {
				return fmt.Errorf("legacy browser_sessions column %q is incompatible", actual.name)
			}
		}
	}
	return validateBrowserSessionIndexes(tx, true)
}

func compareBrowserSessionColumns(actual, expected []browserSessionColumnInfo) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("browser_sessions has %d columns, want %d", len(actual), len(expected))
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf(
				"browser_sessions column %d is incompatible: got %+v want %+v",
				index,
				actual[index],
				expected[index],
			)
		}
	}
	return nil
}

func validateBrowserSessionIndexes(tx *sql.Tx, allowLegacy bool) error {
	rows, err := tx.Query(`PRAGMA index_list(browser_sessions)`)
	if err != nil {
		return fmt.Errorf("inspect browser session indexes: %w", err)
	}
	type indexInfo struct {
		name    string
		unique  bool
		origin  string
		partial bool
	}
	var indexes []indexInfo
	for rows.Next() {
		var sequence, unique, partial int
		var index indexInfo
		if err := rows.Scan(&sequence, &index.name, &unique, &index.origin, &partial); err != nil {
			rows.Close()
			return fmt.Errorf("scan browser session index: %w", err)
		}
		index.unique = unique != 0
		index.partial = partial != 0
		indexes = append(indexes, index)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate browser session indexes: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close browser session index rows: %w", err)
	}

	activeIndexCount := 0
	recognizedIndexCount := 0
	tokenUnique := false
	for _, index := range indexes {
		columns, err := browserSessionIndexColumns(tx, index.name)
		if err != nil {
			return err
		}
		if index.unique && len(columns) == 1 && columns[0] == "token_hash" {
			tokenUnique = true
		}
		if index.origin != "c" {
			continue
		}
		switch index.name {
		case "idx_browser_sessions_active":
			if index.unique || index.partial ||
				!equalBrowserSessionStrings(columns, []string{"revoked_at", "expires_at", "last_active_at"}) {
				return fmt.Errorf("browser session active index is incompatible")
			}
			activeIndexCount++
			recognizedIndexCount++
		case "idx_browser_sessions_active_token":
			if !allowLegacy ||
				index.unique ||
				!index.partial ||
				!equalBrowserSessionStrings(columns, []string{"token_hash"}) {
				return fmt.Errorf("legacy browser session active token index is incompatible")
			}
			indexSQL, err := normalizedBrowserSessionIndexSQL(tx, index.name)
			if err != nil {
				return err
			}
			const legacyIndexSQL = "create index idx_browser_sessions_active_token on browser_sessions(token_hash) where revoked_at is null"
			const legacyIndexSQLIfAbsent = "create index if not exists idx_browser_sessions_active_token on browser_sessions(token_hash) where revoked_at is null"
			if indexSQL != legacyIndexSQL && indexSQL != legacyIndexSQLIfAbsent {
				return fmt.Errorf("legacy browser session active token index definition is incompatible")
			}
			recognizedIndexCount++
		default:
			return fmt.Errorf("unexpected browser session index %q", index.name)
		}
	}
	if !tokenUnique {
		return fmt.Errorf("browser session token_hash unique constraint is missing")
	}
	if allowLegacy {
		if recognizedIndexCount != 1 {
			return fmt.Errorf("legacy browser session explicit index count = %d, want 1", recognizedIndexCount)
		}
		return nil
	}
	if activeIndexCount != 1 {
		return fmt.Errorf("browser session active index count = %d, want 1", activeIndexCount)
	}
	indexSQL, err := normalizedBrowserSessionIndexSQL(tx, "idx_browser_sessions_active")
	if err != nil {
		return err
	}
	const expected = "create index idx_browser_sessions_active on browser_sessions(revoked_at, expires_at, last_active_at)"
	if indexSQL != expected {
		return fmt.Errorf("browser session active index definition is incompatible")
	}
	return nil
}

func normalizedBrowserSessionIndexSQL(tx *sql.Tx, indexName string) (string, error) {
	var indexSQL string
	if err := tx.QueryRow(`
		SELECT sql FROM sqlite_master
		WHERE type = 'index' AND name = ?
	`, indexName).Scan(&indexSQL); err != nil {
		return "", fmt.Errorf("read browser session index %q definition: %w", indexName, err)
	}
	return strings.Join(strings.Fields(strings.ToLower(indexSQL)), " "), nil
}

func browserSessionIndexColumns(tx *sql.Tx, indexName string) ([]string, error) {
	quotedName := `"` + strings.ReplaceAll(indexName, `"`, `""`) + `"`
	rows, err := tx.Query(`PRAGMA index_info(` + quotedName + `)`)
	if err != nil {
		return nil, fmt.Errorf("inspect browser session index %q: %w", indexName, err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var sequence, cid int
		var column string
		if err := rows.Scan(&sequence, &cid, &column); err != nil {
			return nil, fmt.Errorf("scan browser session index %q: %w", indexName, err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate browser session index %q: %w", indexName, err)
	}
	return columns, nil
}

func equalBrowserSessionStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
