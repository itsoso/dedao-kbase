package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3"
)

const (
	defaultBrowserSessionTTL             = 30 * 24 * time.Hour
	defaultBrowserSessionRenewalInterval = 5 * time.Minute
	defaultBrowserSessionMaxActive       = 10
	browserSessionCSRFTTL                = 15 * time.Minute
	browserSessionCredentialBytes        = 32
	browserSessionSchemaVersion          = 1
	browserSessionTimeLayout             = "2006-01-02T15:04:05.000000000Z07:00"
)

var (
	ErrBrowserSessionMissing         = errors.New("browser session missing")
	ErrBrowserSessionExpired         = errors.New("browser session expired")
	ErrBrowserSessionRevoked         = errors.New("browser session revoked")
	ErrBrowserSessionConflict        = errors.New("browser session conflict")
	ErrBrowserSessionUnavailable     = errors.New("browser session unavailable")
	ErrBrowserSessionInvalidArgument = errors.New("browser session invalid argument")
	ErrBrowserSessionCSRFInvalid     = errors.New("browser session CSRF invalid")
	ErrBrowserSessionCSRFExpired     = errors.New("browser session CSRF expired")
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

type BrowserSessionAuth struct {
	Session         BrowserSession
	Renewed         bool
	SetCookie       bool
	CookieExpiresAt time.Time
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
		return nil, fmt.Errorf(
			"browser session database path is required: %w",
			ErrBrowserSessionInvalidArgument,
		)
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
	if config.RenewalInterval <= 0 || config.RenewalInterval >= config.TTL {
		return nil, fmt.Errorf(
			"browser session renewal interval must be greater than zero and less than TTL: %w",
			ErrBrowserSessionInvalidArgument,
		)
	}
	if config.MaxActive <= 0 {
		config.MaxActive = defaultBrowserSessionMaxActive
	}
	if err := prepareBrowserSessionDirectory(filepath.Dir(dbPath)); err != nil {
		return nil, newBrowserSessionCategorizedError(
			"prepare browser session database directory",
			ErrBrowserSessionUnavailable,
			err,
		)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, newBrowserSessionCategorizedError(
			"open browser session database",
			ErrBrowserSessionUnavailable,
			err,
		)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, newBrowserSessionCategorizedError(
			"configure browser session database busy timeout",
			ErrBrowserSessionUnavailable,
			err,
		)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, newBrowserSessionCategorizedError(
			"enable browser session database foreign keys",
			ErrBrowserSessionUnavailable,
			err,
		)
	}
	if err := migrateBrowserSessionDB(db); err != nil {
		db.Close()
		return nil, newBrowserSessionCategorizedError(
			"migrate browser session database",
			ErrBrowserSessionUnavailable,
			err,
		)
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
		return BrowserSessionCredentials{}, fmt.Errorf(
			"create browser session: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return BrowserSessionCredentials{}, fmt.Errorf(
			"create browser session: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	token, err := s.randomCredential()
	if err != nil {
		return BrowserSessionCredentials{}, fmt.Errorf(
			"generate session credential: %v: %w",
			err,
			ErrBrowserSessionUnavailable,
		)
	}
	csrfToken, err := s.randomCredential()
	if err != nil {
		return BrowserSessionCredentials{}, fmt.Errorf(
			"generate CSRF credential: %v: %w",
			err,
			ErrBrowserSessionUnavailable,
		)
	}

	tokenHash := sha256.Sum256([]byte(token))
	csrfHash := sha256.Sum256([]byte(csrfToken))
	if subtle.ConstantTimeCompare(tokenHash[:], csrfHash[:]) == 1 {
		return BrowserSessionCredentials{}, fmt.Errorf(
			"create browser session credentials: %w",
			ErrBrowserSessionConflict,
		)
	}
	userAgentHash := sha256.Sum256([]byte(input.UserAgent))
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)
	csrfExpiresAt := now.Add(browserSessionCSRFTTL)
	session := BrowserSession{
		ID:           "session_" + base64.RawURLEncoding.EncodeToString(tokenHash[:18]),
		DeviceLabel:  strings.TrimSpace(input.DeviceLabel),
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    expiresAt,
	}

	err = s.withImmediateTransaction(context.Background(), func(conn *sql.Conn) error {
		var collisions int
		if err := conn.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*)
			 FROM browser_sessions
			 WHERE token_hash IN (?, ?) OR csrf_hash IN (?, ?)`,
			tokenHash[:],
			csrfHash[:],
			tokenHash[:],
			csrfHash[:],
		).Scan(&collisions); err != nil {
			return classifyBrowserSessionStoreError("check browser session credential collision", err)
		}
		if collisions != 0 {
			return fmt.Errorf("insert browser session: %w", ErrBrowserSessionConflict)
		}

		var activeCount int
		if err := conn.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM browser_sessions WHERE revoked_at = '' AND expires_at > ?`,
			formatBrowserSessionTime(now),
		).Scan(&activeCount); err != nil {
			return classifyBrowserSessionStoreError("count active browser sessions", err)
		}
		evictCount := activeCount - s.maxActive + 1
		if evictCount > 0 {
			result, err := conn.ExecContext(context.Background(), `
				UPDATE browser_sessions
				SET revoked_at = ?, revoke_reason = ?
				WHERE id IN (
					SELECT id
					FROM browser_sessions
					WHERE revoked_at = '' AND expires_at > ?
					ORDER BY last_active_at ASC, created_at ASC, id ASC
					LIMIT ?
				)
				AND revoked_at = ''
			`,
				formatBrowserSessionTime(now),
				"session_limit",
				formatBrowserSessionTime(now),
				evictCount,
			)
			if err != nil {
				return classifyBrowserSessionStoreError("evict browser sessions over active limit", err)
			}
			evicted, err := result.RowsAffected()
			if err != nil {
				return classifyBrowserSessionStoreError("read evicted browser session count", err)
			}
			if evicted != int64(evictCount) {
				return fmt.Errorf(
					"evict browser sessions over active limit: got %d want %d: %w",
					evicted,
					evictCount,
					ErrBrowserSessionConflict,
				)
			}
		}

		_, err := conn.ExecContext(context.Background(), `
			INSERT INTO browser_sessions (
				id, token_hash, csrf_hash, csrf_expires_at, device_label,
				user_agent_hash, created_at, last_active_at, expires_at,
				revoked_at, revoke_reason
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', '')
		`,
			session.ID,
			tokenHash[:],
			csrfHash[:],
			formatBrowserSessionTime(csrfExpiresAt),
			session.DeviceLabel,
			userAgentHash[:],
			formatBrowserSessionTime(now),
			formatBrowserSessionTime(now),
			formatBrowserSessionTime(expiresAt),
		)
		if err != nil {
			return classifyBrowserSessionStoreError("insert browser session", err)
		}
		return nil
	})
	if err != nil {
		return BrowserSessionCredentials{}, fmt.Errorf("create browser session: %w", err)
	}
	return BrowserSessionCredentials{
		Session:   session,
		Token:     token,
		CSRFToken: csrfToken,
	}, nil
}

func (s *BrowserSessionStore) AuthenticateAndRenew(token string) (BrowserSessionAuth, error) {
	if s == nil {
		return BrowserSessionAuth{}, fmt.Errorf(
			"authenticate browser session: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return BrowserSessionAuth{}, fmt.Errorf(
			"authenticate browser session: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	tokenHash := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	record, err := readBrowserSessionByToken(context.Background(), s.db, tokenHash[:])
	if err != nil {
		return BrowserSessionAuth{}, fmt.Errorf("authenticate browser session: %w", err)
	}
	if err := validateBrowserSessionActive(record.session, now); err != nil {
		return BrowserSessionAuth{}, fmt.Errorf("authenticate browser session: %w", err)
	}

	auth := BrowserSessionAuth{
		Session:         record.session,
		CookieExpiresAt: record.session.ExpiresAt,
	}
	if !now.Before(record.session.LastActiveAt.Add(s.renewalInterval)) {
		err := s.withImmediateTransaction(context.Background(), func(conn *sql.Conn) error {
			current, err := readBrowserSessionByToken(context.Background(), conn, tokenHash[:])
			if err != nil {
				return err
			}
			if err := validateBrowserSessionActive(current.session, now); err != nil {
				return err
			}
			auth.Session = current.session
			auth.CookieExpiresAt = current.session.ExpiresAt
			if now.Before(current.session.LastActiveAt.Add(s.renewalInterval)) {
				return nil
			}
			expiresAt := now.Add(s.ttl)
			result, err := conn.ExecContext(context.Background(), `
				UPDATE browser_sessions
				SET last_active_at = ?, expires_at = ?
				WHERE id = ? AND revoked_at = '' AND expires_at > ?
			`,
				formatBrowserSessionTime(now),
				formatBrowserSessionTime(expiresAt),
				current.session.ID,
				formatBrowserSessionTime(now),
			)
			if err != nil {
				return classifyBrowserSessionStoreError("renew browser session", err)
			}
			updated, err := result.RowsAffected()
			if err != nil {
				return classifyBrowserSessionStoreError("read browser session renewal count", err)
			}
			if updated != 1 {
				return fmt.Errorf("renew browser session: %w", ErrBrowserSessionConflict)
			}
			auth.Renewed = true
			auth.SetCookie = true
			auth.CookieExpiresAt = expiresAt
			return nil
		})
		if err != nil {
			return BrowserSessionAuth{}, fmt.Errorf("authenticate browser session: %w", err)
		}
	}

	auth.Session.LastActiveAt = now
	auth.Session.ExpiresAt = now.Add(s.ttl)
	return auth, nil
}

func (s *BrowserSessionStore) RotateCSRF(id string) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf(
			"rotate browser session CSRF: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return "", time.Time{}, fmt.Errorf(
			"rotate browser session CSRF: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	now := s.now().UTC()
	var csrfToken string
	expiresAt := now.Add(browserSessionCSRFTTL)
	err := s.withImmediateTransaction(context.Background(), func(conn *sql.Conn) error {
		record, err := readBrowserSessionByID(context.Background(), conn, id)
		if err != nil {
			return err
		}
		if err := validateBrowserSessionActive(record.session, now); err != nil {
			return err
		}

		csrfToken, err = s.randomCredential()
		if err != nil {
			return fmt.Errorf(
				"generate rotated CSRF credential: %v: %w",
				err,
				ErrBrowserSessionUnavailable,
			)
		}
		csrfHash := sha256.Sum256([]byte(csrfToken))
		var collisions int
		if err := conn.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*)
			 FROM browser_sessions
			 WHERE token_hash = ? OR csrf_hash = ?`,
			csrfHash[:],
			csrfHash[:],
		).Scan(&collisions); err != nil {
			return classifyBrowserSessionStoreError("check rotated CSRF collision", err)
		}
		if collisions != 0 {
			return fmt.Errorf("rotate browser session CSRF: %w", ErrBrowserSessionConflict)
		}

		result, err := conn.ExecContext(context.Background(), `
			UPDATE browser_sessions
			SET csrf_hash = ?, csrf_expires_at = ?
			WHERE id = ? AND revoked_at = '' AND expires_at > ?
		`,
			csrfHash[:],
			formatBrowserSessionTime(expiresAt),
			id,
			formatBrowserSessionTime(now),
		)
		if err != nil {
			return classifyBrowserSessionStoreError("persist rotated browser session CSRF", err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return classifyBrowserSessionStoreError("read rotated browser session CSRF count", err)
		}
		if updated != 1 {
			return fmt.Errorf("rotate browser session CSRF: %w", ErrBrowserSessionConflict)
		}
		return nil
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("rotate browser session CSRF: %w", err)
	}
	return csrfToken, expiresAt, nil
}

func (s *BrowserSessionStore) ValidateCSRF(id, token string) error {
	if s == nil {
		return fmt.Errorf(
			"validate browser session CSRF: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return fmt.Errorf(
			"validate browser session CSRF: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	record, err := readBrowserSessionByID(context.Background(), s.db, id)
	if err != nil {
		return fmt.Errorf("validate browser session CSRF: %w", err)
	}
	now := s.now().UTC()
	if err := validateBrowserSessionActive(record.session, now); err != nil {
		return fmt.Errorf("validate browser session CSRF: %w", err)
	}
	if !record.csrfExpiresAt.After(now) {
		return fmt.Errorf("validate browser session CSRF: %w", ErrBrowserSessionCSRFExpired)
	}
	tokenHash := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(tokenHash[:], record.csrfHash) != 1 {
		return fmt.Errorf("validate browser session CSRF: %w", ErrBrowserSessionCSRFInvalid)
	}
	return nil
}

func (s *BrowserSessionStore) Revoke(id, reason string) error {
	if s == nil {
		return fmt.Errorf(
			"revoke browser session: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return fmt.Errorf(
			"revoke browser session: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	_, err := s.db.Exec(`
		UPDATE browser_sessions
		SET revoked_at = ?, revoke_reason = ?
		WHERE id = ? AND revoked_at = ''
	`, formatBrowserSessionTime(s.now().UTC()), strings.TrimSpace(reason), id)
	if err != nil {
		return classifyBrowserSessionStoreError("revoke browser session", err)
	}
	return nil
}

func (s *BrowserSessionStore) RevokeByToken(token, reason string) error {
	if s == nil {
		return fmt.Errorf(
			"revoke browser session by token: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return fmt.Errorf(
			"revoke browser session by token: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	tokenHash := sha256.Sum256([]byte(token))
	_, err := s.db.Exec(`
		UPDATE browser_sessions
		SET revoked_at = ?, revoke_reason = ?
		WHERE token_hash = ? AND revoked_at = ''
	`, formatBrowserSessionTime(s.now().UTC()), strings.TrimSpace(reason), tokenHash[:])
	if err != nil {
		return classifyBrowserSessionStoreError("revoke browser session by token", err)
	}
	return nil
}

func (s *BrowserSessionStore) RevokeAll(reason string) (int64, error) {
	if s == nil {
		return 0, fmt.Errorf(
			"revoke all browser sessions: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return 0, fmt.Errorf(
			"revoke all browser sessions: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	result, err := s.db.Exec(`
		UPDATE browser_sessions
		SET revoked_at = ?, revoke_reason = ?
		WHERE revoked_at = ''
	`, formatBrowserSessionTime(s.now().UTC()), strings.TrimSpace(reason))
	if err != nil {
		return 0, classifyBrowserSessionStoreError("revoke all browser sessions", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, classifyBrowserSessionStoreError("read revoked browser session count", err)
	}
	return updated, nil
}

func (s *BrowserSessionStore) List() ([]BrowserSession, error) {
	if s == nil {
		return nil, fmt.Errorf(
			"list browser sessions: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return nil, fmt.Errorf(
			"list browser sessions: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	rows, err := s.db.Query(`
		SELECT id, device_label, created_at, last_active_at, expires_at,
			revoked_at, revoke_reason
		FROM browser_sessions
		ORDER BY created_at DESC, id ASC
	`)
	if err != nil {
		return nil, classifyBrowserSessionStoreError("list browser sessions", err)
	}
	defer rows.Close()

	sessions := make([]BrowserSession, 0)
	for rows.Next() {
		session, err := scanBrowserSessionPublic(rows)
		if err != nil {
			return nil, classifyBrowserSessionStoreError("scan listed browser session", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyBrowserSessionStoreError("iterate listed browser sessions", err)
	}
	return sessions, nil
}

func (s *BrowserSessionStore) Cleanup(retainAfter time.Duration) (int64, error) {
	if retainAfter < 0 {
		return 0, fmt.Errorf(
			"cleanup browser sessions: negative retention: %w",
			ErrBrowserSessionInvalidArgument,
		)
	}
	if s == nil {
		return 0, fmt.Errorf(
			"cleanup browser sessions: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return 0, fmt.Errorf(
			"cleanup browser sessions: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	boundary := formatBrowserSessionTime(s.now().UTC().Add(-retainAfter))
	result, err := s.db.Exec(`
		DELETE FROM browser_sessions
		WHERE (revoked_at <> '' AND revoked_at < ?) OR expires_at < ?
	`, boundary, boundary)
	if err != nil {
		return 0, classifyBrowserSessionStoreError("cleanup browser sessions", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, classifyBrowserSessionStoreError("read cleaned browser session count", err)
	}
	return deleted, nil
}

func (s *BrowserSessionStore) randomCredential() (string, error) {
	randomBytes := make([]byte, browserSessionCredentialBytes)
	_, err := io.ReadFull(s.random, randomBytes)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

type browserSessionRow struct {
	session       BrowserSession
	csrfHash      []byte
	csrfExpiresAt time.Time
}

type browserSessionScanner interface {
	Scan(dest ...any) error
}

type browserSessionQueryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readBrowserSessionByToken(
	ctx context.Context,
	db browserSessionQueryRower,
	tokenHash []byte,
) (browserSessionRow, error) {
	row, err := scanBrowserSessionRow(db.QueryRowContext(ctx, `
		SELECT id, device_label, created_at, last_active_at, expires_at,
			revoked_at, revoke_reason, csrf_hash, csrf_expires_at
		FROM browser_sessions
		WHERE token_hash = ?
	`, tokenHash))
	if errors.Is(err, sql.ErrNoRows) {
		return browserSessionRow{}, ErrBrowserSessionMissing
	}
	if err != nil {
		return browserSessionRow{}, classifyBrowserSessionStoreError("read browser session by token", err)
	}
	return row, nil
}

func readBrowserSessionByID(
	ctx context.Context,
	db browserSessionQueryRower,
	id string,
) (browserSessionRow, error) {
	row, err := scanBrowserSessionRow(db.QueryRowContext(ctx, `
		SELECT id, device_label, created_at, last_active_at, expires_at,
			revoked_at, revoke_reason, csrf_hash, csrf_expires_at
		FROM browser_sessions
		WHERE id = ?
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return browserSessionRow{}, ErrBrowserSessionMissing
	}
	if err != nil {
		return browserSessionRow{}, classifyBrowserSessionStoreError("read browser session by ID", err)
	}
	return row, nil
}

func scanBrowserSessionPublic(scanner browserSessionScanner) (BrowserSession, error) {
	var (
		session                            BrowserSession
		createdAt, lastActiveAt, expiresAt string
		revokedAt                          string
	)
	if err := scanner.Scan(
		&session.ID,
		&session.DeviceLabel,
		&createdAt,
		&lastActiveAt,
		&expiresAt,
		&revokedAt,
		&session.RevokeReason,
	); err != nil {
		return BrowserSession{}, err
	}
	if err := parseBrowserSessionPublicTimes(
		&session,
		createdAt,
		lastActiveAt,
		expiresAt,
		revokedAt,
	); err != nil {
		return BrowserSession{}, err
	}
	return session, nil
}

func scanBrowserSessionRow(scanner browserSessionScanner) (browserSessionRow, error) {
	var (
		row                                browserSessionRow
		createdAt, lastActiveAt, expiresAt string
		revokedAt, csrfExpiresAt           string
	)
	if err := scanner.Scan(
		&row.session.ID,
		&row.session.DeviceLabel,
		&createdAt,
		&lastActiveAt,
		&expiresAt,
		&revokedAt,
		&row.session.RevokeReason,
		&row.csrfHash,
		&csrfExpiresAt,
	); err != nil {
		return browserSessionRow{}, err
	}
	if err := parseBrowserSessionPublicTimes(
		&row.session,
		createdAt,
		lastActiveAt,
		expiresAt,
		revokedAt,
	); err != nil {
		return browserSessionRow{}, err
	}
	var err error
	row.csrfExpiresAt, err = parseBrowserSessionTime(csrfExpiresAt)
	if err != nil {
		return browserSessionRow{}, err
	}
	return row, nil
}

func parseBrowserSessionPublicTimes(
	session *BrowserSession,
	createdAt string,
	lastActiveAt string,
	expiresAt string,
	revokedAt string,
) error {
	var err error
	session.CreatedAt, err = parseBrowserSessionTime(createdAt)
	if err != nil {
		return err
	}
	session.LastActiveAt, err = parseBrowserSessionTime(lastActiveAt)
	if err != nil {
		return err
	}
	session.ExpiresAt, err = parseBrowserSessionTime(expiresAt)
	if err != nil {
		return err
	}
	if revokedAt != "" {
		parsed, err := parseBrowserSessionTime(revokedAt)
		if err != nil {
			return err
		}
		session.RevokedAt = &parsed
	}
	return nil
}

func validateBrowserSessionActive(session BrowserSession, now time.Time) error {
	if session.RevokedAt != nil {
		return ErrBrowserSessionRevoked
	}
	if !session.ExpiresAt.After(now) {
		return ErrBrowserSessionExpired
	}
	return nil
}

func (s *BrowserSessionStore) withImmediateTransaction(
	ctx context.Context,
	work func(*sql.Conn) error,
) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return classifyBrowserSessionStoreError("acquire browser session database connection", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return classifyBrowserSessionStoreError("begin immediate browser session transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	if err := work(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return classifyBrowserSessionStoreError("commit browser session transaction", err)
	}
	committed = true
	return nil
}

type browserSessionCategorizedError struct {
	operation string
	category  error
	cause     error
}

func newBrowserSessionCategorizedError(operation string, category, cause error) error {
	return &browserSessionCategorizedError{
		operation: operation,
		category:  category,
		cause:     cause,
	}
}

func (e *browserSessionCategorizedError) Error() string {
	return fmt.Sprintf("%s: %v", e.operation, e.cause)
}

func (e *browserSessionCategorizedError) Unwrap() []error {
	return []error{e.category, e.cause}
}

func classifyBrowserSessionStoreError(operation string, err error) error {
	if err == nil {
		return nil
	}
	for _, sentinel := range []error{
		ErrBrowserSessionMissing,
		ErrBrowserSessionExpired,
		ErrBrowserSessionRevoked,
		ErrBrowserSessionConflict,
		ErrBrowserSessionUnavailable,
		ErrBrowserSessionInvalidArgument,
		ErrBrowserSessionCSRFInvalid,
		ErrBrowserSessionCSRFExpired,
	} {
		if errors.Is(err, sentinel) {
			return fmt.Errorf("%s: %w", operation, err)
		}
	}
	var sqliteError sqlite3.Error
	if errors.As(err, &sqliteError) && sqliteError.Code == sqlite3.ErrConstraint {
		return newBrowserSessionCategorizedError(operation, ErrBrowserSessionConflict, err)
	}
	return newBrowserSessionCategorizedError(operation, ErrBrowserSessionUnavailable, err)
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
	info, err := os.Stat(directory)
	switch {
	case err == nil:
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", directory)
		}
		return validateBrowserSessionDirectoryMode(info.Mode().Perm())
	case !os.IsNotExist(err):
		return err
	}

	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	info, err = os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", directory)
	}
	return validateBrowserSessionDirectoryMode(info.Mode().Perm())
}

func validateBrowserSessionDirectoryMode(mode os.FileMode) error {
	return validateBrowserSessionDirectoryModeForOS(runtime.GOOS, mode)
}

func validateBrowserSessionDirectoryModeForOS(goos string, mode os.FileMode) error {
	if goos == "windows" {
		return nil
	}
	const (
		requiredOwnerPermissions = os.FileMode(0o700)
		allowedPermissions       = os.FileMode(0o750)
	)
	if mode&requiredOwnerPermissions != requiredOwnerPermissions ||
		mode&^allowedPermissions != 0 {
		return fmt.Errorf(
			"browser session database directory has unsafe permissions %04o; require permissions bounded by 0750 with owner access",
			mode,
		)
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
		if err := normalizeBrowserSessionTimesV1(tx); err != nil {
			return fmt.Errorf("normalize browser session schema version 1 times: %w", err)
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
	switch tableName {
	case "browser_sessions", "browser_sessions_v1":
	default:
		return fmt.Errorf("unsupported browser session migration table %q", tableName)
	}
	statement := fmt.Sprintf(`
		CREATE TABLE %s (
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
		)`, tableName)
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
	readLegacySessions := func() ([][]any, error) {
		rows, err := tx.Query(`
			SELECT id, token_hash, csrf_hash, csrf_expires_at, device_label,
				user_agent_hash, created_at, last_active_at, expires_at,
				revoked_at, revoke_reason
			FROM browser_sessions
		`)
		if err != nil {
			return nil, fmt.Errorf("read legacy browser sessions: %w", err)
		}
		defer rows.Close()

		var sessions [][]any
		for rows.Next() {
			var (
				id                                    string
				tokenHash, csrfHash, userAgentHash    []byte
				csrfExpiresAt, deviceLabel, createdAt string
				lastActiveAt, expiresAt, revokeReason string
				revokedAt                             sql.NullString
				canonicalRevokedAt                    string
			)
			if err := rows.Scan(
				&id,
				&tokenHash,
				&csrfHash,
				&csrfExpiresAt,
				&deviceLabel,
				&userAgentHash,
				&createdAt,
				&lastActiveAt,
				&expiresAt,
				&revokedAt,
				&revokeReason,
			); err != nil {
				return nil, fmt.Errorf("scan legacy browser session: %w", err)
			}
			if csrfExpiresAt, err = canonicalizePersistedBrowserSessionTime(
				"legacy",
				"csrf_expires_at",
				csrfExpiresAt,
				false,
			); err != nil {
				return nil, err
			}
			if createdAt, err = canonicalizePersistedBrowserSessionTime(
				"legacy",
				"created_at",
				createdAt,
				false,
			); err != nil {
				return nil, err
			}
			if lastActiveAt, err = canonicalizePersistedBrowserSessionTime(
				"legacy",
				"last_active_at",
				lastActiveAt,
				false,
			); err != nil {
				return nil, err
			}
			if expiresAt, err = canonicalizePersistedBrowserSessionTime(
				"legacy",
				"expires_at",
				expiresAt,
				false,
			); err != nil {
				return nil, err
			}
			if revokedAt.Valid {
				canonicalRevokedAt = revokedAt.String
			}
			if canonicalRevokedAt, err = canonicalizePersistedBrowserSessionTime(
				"legacy",
				"revoked_at",
				canonicalRevokedAt,
				true,
			); err != nil {
				return nil, err
			}
			sessions = append(sessions, []any{
				id,
				tokenHash,
				csrfHash,
				csrfExpiresAt,
				deviceLabel,
				userAgentHash,
				createdAt,
				lastActiveAt,
				expiresAt,
				canonicalRevokedAt,
				revokeReason,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate legacy browser sessions: %w", err)
		}
		return sessions, nil
	}

	sessions, err := readLegacySessions()
	if err != nil {
		return err
	}
	if err := createBrowserSessionTableV1(tx, "browser_sessions_v1"); err != nil {
		return err
	}
	for _, session := range sessions {
		if _, err := tx.Exec(`
		INSERT INTO browser_sessions_v1 (
			id, token_hash, csrf_hash, csrf_expires_at, device_label,
			user_agent_hash, created_at, last_active_at, expires_at,
			revoked_at, revoke_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, session...); err != nil {
			return fmt.Errorf("copy legacy browser session %q: %w", session[0], err)
		}
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

func normalizeBrowserSessionTimesV1(tx *sql.Tx) error {
	readUpdates := func() ([][]any, error) {
		rows, err := tx.Query(`
			SELECT id, csrf_expires_at, created_at, last_active_at, expires_at, revoked_at
			FROM browser_sessions
		`)
		if err != nil {
			return nil, fmt.Errorf("read version 1 browser session times: %w", err)
		}
		defer rows.Close()

		var updates [][]any
		for rows.Next() {
			var id, csrfExpiresAt, createdAt, lastActiveAt, expiresAt, revokedAt string
			if err := rows.Scan(
				&id,
				&csrfExpiresAt,
				&createdAt,
				&lastActiveAt,
				&expiresAt,
				&revokedAt,
			); err != nil {
				return nil, fmt.Errorf("scan version 1 browser session times: %w", err)
			}
			original := []string{csrfExpiresAt, createdAt, lastActiveAt, expiresAt, revokedAt}
			if csrfExpiresAt, err = canonicalizePersistedBrowserSessionTime(
				"version 1",
				"csrf_expires_at",
				csrfExpiresAt,
				false,
			); err != nil {
				return nil, err
			}
			if createdAt, err = canonicalizePersistedBrowserSessionTime(
				"version 1",
				"created_at",
				createdAt,
				false,
			); err != nil {
				return nil, err
			}
			if lastActiveAt, err = canonicalizePersistedBrowserSessionTime(
				"version 1",
				"last_active_at",
				lastActiveAt,
				false,
			); err != nil {
				return nil, err
			}
			if expiresAt, err = canonicalizePersistedBrowserSessionTime(
				"version 1",
				"expires_at",
				expiresAt,
				false,
			); err != nil {
				return nil, err
			}
			if revokedAt, err = canonicalizePersistedBrowserSessionTime(
				"version 1",
				"revoked_at",
				revokedAt,
				true,
			); err != nil {
				return nil, err
			}
			canonical := []string{csrfExpiresAt, createdAt, lastActiveAt, expiresAt, revokedAt}
			if equalBrowserSessionStrings(original, canonical) {
				continue
			}
			updates = append(updates, []any{
				csrfExpiresAt,
				createdAt,
				lastActiveAt,
				expiresAt,
				revokedAt,
				id,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate version 1 browser session times: %w", err)
		}
		return updates, nil
	}

	updates, err := readUpdates()
	if err != nil {
		return err
	}
	for _, update := range updates {
		if _, err := tx.Exec(`
			UPDATE browser_sessions
			SET csrf_expires_at = ?,
				created_at = ?,
				last_active_at = ?,
				expires_at = ?,
				revoked_at = ?
			WHERE id = ?
		`, update...); err != nil {
			return fmt.Errorf("update version 1 browser session %q times: %w", update[5], err)
		}
	}
	return nil
}

func canonicalizePersistedBrowserSessionTime(scope, field, value string, allowEmpty bool) (string, error) {
	if allowEmpty && value == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", fmt.Errorf("canonicalize %s browser session %s: %w", scope, field, err)
	}
	return formatBrowserSessionTime(parsed), nil
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
	legacyIndexCount := 0
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
			indexSQL, err := normalizedBrowserSessionIndexSQL(tx, index.name)
			if err != nil {
				return err
			}
			const activeIndexSQL = "create index idx_browser_sessions_active on browser_sessions(revoked_at, expires_at, last_active_at)"
			const activeIndexSQLIfAbsent = "create index if not exists idx_browser_sessions_active on browser_sessions(revoked_at, expires_at, last_active_at)"
			if indexSQL != activeIndexSQL &&
				(!allowLegacy || indexSQL != activeIndexSQLIfAbsent) {
				return fmt.Errorf("browser session active index definition is incompatible")
			}
			activeIndexCount++
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
			legacyIndexCount++
		default:
			return fmt.Errorf("unexpected browser session index %q", index.name)
		}
	}
	if !tokenUnique {
		return fmt.Errorf("browser session token_hash unique constraint is missing")
	}
	if allowLegacy {
		if activeIndexCount > 1 || legacyIndexCount > 1 ||
			activeIndexCount+legacyIndexCount == 0 {
			return fmt.Errorf(
				"legacy browser session index counts active=%d legacy=%d, want one known index of each kind at most",
				activeIndexCount,
				legacyIndexCount,
			)
		}
		return nil
	}
	if activeIndexCount != 1 {
		return fmt.Errorf("browser session active index count = %d, want 1", activeIndexCount)
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
