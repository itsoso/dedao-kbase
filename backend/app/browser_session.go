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
	ID           string    `json:"id"`
	DeviceLabel  string    `json:"device_label"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	RevokedAt    time.Time `json:"revoked_at,omitempty"`
	RevokeReason string    `json:"revoke_reason,omitempty"`
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
	randomMu        sync.Mutex
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
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateBrowserSessionDB(db); err != nil {
		db.Close()
		return nil, err
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
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *BrowserSessionStore) Create(input BrowserSessionCreate) (BrowserSessionCredentials, error) {
	token, err := s.randomCredential()
	if err != nil {
		return BrowserSessionCredentials{}, err
	}
	csrfToken, err := s.randomCredential()
	if err != nil {
		return BrowserSessionCredentials{}, err
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
		expiresAt.Format(time.RFC3339Nano),
		session.DeviceLabel,
		userAgentHash[:],
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		expiresAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return BrowserSessionCredentials{}, err
	}
	return BrowserSessionCredentials{
		Session:   session,
		Token:     token,
		CSRFToken: csrfToken,
	}, nil
}

func (s *BrowserSessionStore) randomCredential() (string, error) {
	randomBytes := make([]byte, browserSessionCredentialBytes)
	s.randomMu.Lock()
	_, err := io.ReadFull(s.random, randomBytes)
	s.randomMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("generate browser session credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func migrateBrowserSessionDB(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS browser_sessions (
			id TEXT PRIMARY KEY,
			token_hash BLOB NOT NULL UNIQUE,
			csrf_hash BLOB NOT NULL,
			csrf_expires_at TEXT NOT NULL,
			device_label TEXT NOT NULL DEFAULT '',
			user_agent_hash BLOB NOT NULL,
			created_at TEXT NOT NULL,
			last_active_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT NOT NULL DEFAULT '',
			revoke_reason TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_browser_sessions_active
			ON browser_sessions(revoked_at, expires_at, last_active_at);
	`)
	return err
}
