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
	defaultBrowserSessionTTL                   = 30 * 24 * time.Hour
	defaultBrowserSessionRenewalInterval       = 5 * time.Minute
	defaultBrowserSessionMaxActive             = 10
	browserSessionCSRFTTL                      = 15 * time.Minute
	browserSessionCredentialBytes              = 32
	browserSessionSchemaVersion                = 2
	browserSessionTimeLayout                   = "2006-01-02T15:04:05.000000000Z07:00"
	minBrowserSessionClientIDBytes             = 16
	maxBrowserSessionClientIDBytes             = 128
	maxBrowserSessionEpoch               int64 = 1<<63 - 1
)

var (
	ErrBrowserSessionMissing             = errors.New("browser session missing")
	ErrBrowserSessionExpired             = errors.New("browser session expired")
	ErrBrowserSessionRevoked             = errors.New("browser session revoked")
	ErrBrowserSessionConflict            = errors.New("browser session conflict")
	ErrBrowserSessionUnavailable         = errors.New("browser session unavailable")
	ErrBrowserSessionInvalidArgument     = errors.New("browser session invalid argument")
	ErrBrowserSessionCSRFInvalid         = errors.New("browser session CSRF invalid")
	ErrBrowserSessionCSRFExpired         = errors.New("browser session CSRF expired")
	ErrBrowserSessionStaleEpoch          = errors.New("browser session stale client epoch")
	ErrBrowserSessionClientUninitialized = errors.New("browser session client is not initialized")
	ErrBrowserSessionClientMismatch      = errors.New("browser session client mismatch")
	ErrBrowserSessionEpochExhausted      = errors.New("browser session client epoch exhausted")
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
	ClientID     string     `json:"-"`
	IssuedEpoch  int64      `json:"-"`
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
	ClientID      string
	ExpectedEpoch int64
	DeviceLabel   string
	UserAgent     string
}

type BrowserClientFamily struct {
	ClientID string `json:"client_id"`
	Epoch    int64  `json:"epoch"`
}

type BrowserSessionStaleEpochError struct {
	ClientID     string
	CurrentEpoch int64
}

func (e *BrowserSessionStaleEpochError) Error() string {
	return fmt.Sprintf(
		"browser client %q epoch is stale; current epoch is %d",
		e.ClientID,
		e.CurrentEpoch,
	)
}

func (e *BrowserSessionStaleEpochError) Unwrap() error {
	return ErrBrowserSessionStaleEpoch
}

type BrowserSessionClientUninitializedError struct {
	ClientID string
}

func (e *BrowserSessionClientUninitializedError) Error() string {
	return fmt.Sprintf("browser client %q is not initialized", e.ClientID)
}

func (e *BrowserSessionClientUninitializedError) Unwrap() error {
	return ErrBrowserSessionClientUninitialized
}

type BrowserSessionClientMismatchError struct {
	ClientID string
}

func (e *BrowserSessionClientMismatchError) Error() string {
	return fmt.Sprintf("browser session does not belong to client %q", e.ClientID)
}

func (e *BrowserSessionClientMismatchError) Unwrap() error {
	return ErrBrowserSessionClientMismatch
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

	// Tests use this hook to hold the expected-auth transaction before commit.
	expectedAuthBeforeCommit func()
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

func (s *BrowserSessionStore) AcquireClientEpoch(clientID string) (BrowserClientFamily, error) {
	if err := validateBrowserSessionClientID(clientID); err != nil {
		return BrowserClientFamily{}, err
	}
	if s == nil {
		return BrowserClientFamily{}, fmt.Errorf(
			"acquire browser client epoch: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return BrowserClientFamily{}, fmt.Errorf(
			"acquire browser client epoch: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	now := formatBrowserSessionTime(s.now().UTC())
	family := BrowserClientFamily{ClientID: clientID}
	err := s.withImmediateTransaction(context.Background(), func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(context.Background(), `
			INSERT INTO browser_client_families (client_id, epoch, created_at, updated_at)
			VALUES (?, 1, ?, ?)
			ON CONFLICT(client_id) DO NOTHING
		`, clientID, now, now); err != nil {
			return classifyBrowserSessionStoreError("initialize browser client family", err)
		}
		if err := conn.QueryRowContext(
			context.Background(),
			`SELECT epoch FROM browser_client_families WHERE client_id = ?`,
			clientID,
		).Scan(&family.Epoch); err != nil {
			return classifyBrowserSessionStoreError("read browser client epoch", err)
		}
		return nil
	})
	if err != nil {
		return BrowserClientFamily{}, fmt.Errorf("acquire browser client epoch: %w", err)
	}
	return family, nil
}

func (s *BrowserSessionStore) ReadClientEpoch(clientID string) (BrowserClientFamily, error) {
	if err := validateBrowserSessionClientID(clientID); err != nil {
		return BrowserClientFamily{}, err
	}
	if s == nil {
		return BrowserClientFamily{}, fmt.Errorf(
			"read browser client epoch: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return BrowserClientFamily{}, fmt.Errorf(
			"read browser client epoch: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	family := BrowserClientFamily{ClientID: clientID}
	if err := s.db.QueryRow(
		`SELECT epoch FROM browser_client_families WHERE client_id = ?`,
		clientID,
	).Scan(&family.Epoch); errors.Is(err, sql.ErrNoRows) {
		return BrowserClientFamily{}, fmt.Errorf("read browser client epoch: %w", &BrowserSessionClientUninitializedError{
			ClientID: clientID,
		})
	} else if err != nil {
		return BrowserClientFamily{}, classifyBrowserSessionStoreError(
			"read browser client epoch",
			err,
		)
	}
	return family, nil
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
	if err := validateBrowserSessionClientID(input.ClientID); err != nil {
		return BrowserSessionCredentials{}, fmt.Errorf("create browser session: %w", err)
	}
	if input.ExpectedEpoch <= 0 {
		return BrowserSessionCredentials{}, fmt.Errorf(
			"create browser session: expected client epoch must be positive: %w",
			ErrBrowserSessionInvalidArgument,
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
		ClientID:     input.ClientID,
		IssuedEpoch:  input.ExpectedEpoch,
		DeviceLabel:  strings.TrimSpace(input.DeviceLabel),
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    expiresAt,
	}

	err = s.withImmediateTransaction(context.Background(), func(conn *sql.Conn) error {
		var currentEpoch int64
		if err := conn.QueryRowContext(
			context.Background(),
			`SELECT epoch FROM browser_client_families WHERE client_id = ?`,
			input.ClientID,
		).Scan(&currentEpoch); errors.Is(err, sql.ErrNoRows) {
			return &BrowserSessionClientUninitializedError{ClientID: input.ClientID}
		} else if err != nil {
			return classifyBrowserSessionStoreError("read browser client epoch before create", err)
		}
		if currentEpoch != input.ExpectedEpoch {
			return &BrowserSessionStaleEpochError{
				ClientID:     input.ClientID,
				CurrentEpoch: currentEpoch,
			}
		}

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
				id, client_id, issued_epoch, token_hash, csrf_hash, csrf_expires_at, device_label,
				user_agent_hash, created_at, last_active_at, expires_at,
				revoked_at, revoke_reason
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '')
		`,
			session.ID,
			session.ClientID,
			session.IssuedEpoch,
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

func (s *BrowserSessionStore) Authenticate(token string) (BrowserSession, error) {
	if s == nil {
		return BrowserSession{}, fmt.Errorf(
			"authenticate browser session without renewal: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return BrowserSession{}, fmt.Errorf(
			"authenticate browser session without renewal: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	tokenHash := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	record, err := readBrowserSessionByToken(context.Background(), s.db, tokenHash[:])
	if err != nil {
		return BrowserSession{}, fmt.Errorf(
			"authenticate browser session without renewal: %w",
			err,
		)
	}
	if err := validateBrowserSessionActive(record.session, now); err != nil {
		return BrowserSession{}, fmt.Errorf(
			"authenticate browser session without renewal: %w",
			err,
		)
	}
	return record.session, nil
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
		var renewalCredentialErr error
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
			renewed, err := readBrowserSessionByToken(context.Background(), conn, tokenHash[:])
			if err != nil {
				return err
			}
			if err := validateBrowserSessionActive(renewed.session, now); err != nil {
				renewalCredentialErr = err
				return nil
			}
			auth.Renewed = true
			auth.SetCookie = true
			auth.Session = renewed.session
			auth.CookieExpiresAt = renewed.session.ExpiresAt
			return nil
		})
		if err != nil {
			return BrowserSessionAuth{}, fmt.Errorf("authenticate browser session: %w", err)
		}
		if renewalCredentialErr != nil {
			return BrowserSessionAuth{}, fmt.Errorf(
				"authenticate browser session after renewal: %w",
				renewalCredentialErr,
			)
		}
	}

	auth.Session.LastActiveAt = now
	auth.Session.ExpiresAt = now.Add(s.ttl)
	return auth, nil
}

func (s *BrowserSessionStore) AuthenticateAndRenewExpected(
	token string,
	clientID string,
	expectedEpoch int64,
) (BrowserSessionAuth, error) {
	if s == nil {
		return BrowserSessionAuth{}, fmt.Errorf(
			"authenticate expected browser session: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	if err := validateBrowserSessionClientID(clientID); err != nil {
		return BrowserSessionAuth{}, fmt.Errorf("authenticate expected browser session: %w", err)
	}
	if expectedEpoch <= 0 {
		return BrowserSessionAuth{}, fmt.Errorf(
			"authenticate expected browser session: expected client epoch must be positive: %w",
			ErrBrowserSessionInvalidArgument,
		)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return BrowserSessionAuth{}, fmt.Errorf(
			"authenticate expected browser session: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	tokenHash := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	auth := BrowserSessionAuth{}
	err := s.withImmediateTransaction(context.Background(), func(conn *sql.Conn) error {
		var currentEpoch int64
		if err := conn.QueryRowContext(
			context.Background(),
			`SELECT epoch FROM browser_client_families WHERE client_id = ?`,
			clientID,
		).Scan(&currentEpoch); errors.Is(err, sql.ErrNoRows) {
			return &BrowserSessionClientUninitializedError{ClientID: clientID}
		} else if err != nil {
			return classifyBrowserSessionStoreError(
				"read browser client epoch before expected authentication",
				err,
			)
		}
		if currentEpoch != expectedEpoch {
			return &BrowserSessionStaleEpochError{
				ClientID:     clientID,
				CurrentEpoch: currentEpoch,
			}
		}

		record, err := readBrowserSessionByToken(context.Background(), conn, tokenHash[:])
		if err != nil {
			return err
		}
		if err := validateBrowserSessionActive(record.session, now); err != nil {
			return err
		}
		if record.session.ClientID != clientID ||
			record.session.IssuedEpoch != expectedEpoch {
			return &BrowserSessionClientMismatchError{ClientID: clientID}
		}

		auth = BrowserSessionAuth{
			Session:         record.session,
			CookieExpiresAt: record.session.ExpiresAt,
		}
		if !now.Before(record.session.LastActiveAt.Add(s.renewalInterval)) {
			expiresAt := now.Add(s.ttl)
			result, err := conn.ExecContext(context.Background(), `
				UPDATE browser_sessions
				SET last_active_at = ?, expires_at = ?
				WHERE id = ? AND client_id = ? AND issued_epoch = ?
					AND revoked_at = '' AND expires_at > ?
			`,
				formatBrowserSessionTime(now),
				formatBrowserSessionTime(expiresAt),
				record.session.ID,
				clientID,
				expectedEpoch,
				formatBrowserSessionTime(now),
			)
			if err != nil {
				return classifyBrowserSessionStoreError(
					"renew expected browser session",
					err,
				)
			}
			updated, err := result.RowsAffected()
			if err != nil {
				return classifyBrowserSessionStoreError(
					"read renewed expected browser session count",
					err,
				)
			}
			if updated != 1 {
				return fmt.Errorf(
					"renew expected browser session: %w",
					ErrBrowserSessionConflict,
				)
			}
			renewed, err := readBrowserSessionByToken(
				context.Background(),
				conn,
				tokenHash[:],
			)
			if err != nil {
				return err
			}
			if err := validateBrowserSessionActive(renewed.session, now); err != nil {
				return err
			}
			auth.Renewed = true
			auth.SetCookie = true
			auth.Session = renewed.session
			auth.CookieExpiresAt = renewed.session.ExpiresAt
		}
		if s.expectedAuthBeforeCommit != nil {
			s.expectedAuthBeforeCommit()
		}
		return nil
	})
	if err != nil {
		return BrowserSessionAuth{}, fmt.Errorf(
			"authenticate expected browser session: %w",
			err,
		)
	}

	auth.Session.LastActiveAt = now
	auth.Session.ExpiresAt = now.Add(s.ttl)
	return auth, nil
}

func (s *BrowserSessionStore) IssueCSRF(token string) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf(
			"issue browser session CSRF: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return "", time.Time{}, fmt.Errorf(
			"issue browser session CSRF: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	tokenHash := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	var csrfToken string
	var expiresAt time.Time
	err := s.withImmediateTransaction(context.Background(), func(conn *sql.Conn) error {
		record, err := readBrowserSessionByToken(context.Background(), conn, tokenHash[:])
		if err != nil {
			return err
		}
		if err := validateBrowserSessionActive(record.session, now); err != nil {
			return err
		}

		candidate := deriveBrowserSessionCSRFToken(token, record.csrfExpiresAt)
		candidateHash := sha256.Sum256([]byte(candidate))
		if record.csrfExpiresAt.After(now) &&
			subtle.ConstantTimeCompare(candidateHash[:], record.csrfHash) == 1 {
			csrfToken = candidate
			expiresAt = record.csrfExpiresAt
			return nil
		}

		expiresAt = now.Add(browserSessionCSRFTTL)
		csrfToken = deriveBrowserSessionCSRFToken(token, expiresAt)
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
			return classifyBrowserSessionStoreError("check issued CSRF collision", err)
		}
		if collisions != 0 {
			return fmt.Errorf("issue browser session CSRF: %w", ErrBrowserSessionConflict)
		}

		result, err := conn.ExecContext(context.Background(), `
			UPDATE browser_sessions
			SET csrf_hash = ?, csrf_expires_at = ?
			WHERE id = ? AND revoked_at = '' AND expires_at > ?
		`,
			csrfHash[:],
			formatBrowserSessionTime(expiresAt),
			record.session.ID,
			formatBrowserSessionTime(now),
		)
		if err != nil {
			return classifyBrowserSessionStoreError("persist issued browser session CSRF", err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return classifyBrowserSessionStoreError("read issued browser session CSRF count", err)
		}
		if updated != 1 {
			return fmt.Errorf("issue browser session CSRF: %w", ErrBrowserSessionConflict)
		}
		return nil
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("issue browser session CSRF: %w", err)
	}
	return csrfToken, expiresAt, nil
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

func (s *BrowserSessionStore) FenceClientBySession(
	id string,
	reason string,
) (BrowserClientFamily, error) {
	if s == nil {
		return BrowserClientFamily{}, fmt.Errorf(
			"fence browser client family: browser session store is nil: %w",
			ErrBrowserSessionUnavailable,
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.db == nil {
		return BrowserClientFamily{}, fmt.Errorf(
			"fence browser client family: browser session store is closed: %w",
			ErrBrowserSessionUnavailable,
		)
	}

	now := formatBrowserSessionTime(s.now().UTC())
	family := BrowserClientFamily{}
	err := s.withImmediateTransaction(context.Background(), func(conn *sql.Conn) error {
		var issuedEpoch int64
		if err := conn.QueryRowContext(context.Background(), `
			SELECT client_id, issued_epoch
			FROM browser_sessions
			WHERE id = ?
		`, id).Scan(&family.ClientID, &issuedEpoch); errors.Is(err, sql.ErrNoRows) {
			return ErrBrowserSessionMissing
		} else if err != nil {
			return classifyBrowserSessionStoreError("read browser session family", err)
		}

		if err := conn.QueryRowContext(
			context.Background(),
			`SELECT epoch FROM browser_client_families WHERE client_id = ?`,
			family.ClientID,
		).Scan(&family.Epoch); err != nil {
			return classifyBrowserSessionStoreError("read browser client epoch before fence", err)
		}
		if family.Epoch == maxBrowserSessionEpoch ||
			issuedEpoch == maxBrowserSessionEpoch {
			return ErrBrowserSessionEpochExhausted
		}
		if family.Epoch <= issuedEpoch {
			family.Epoch = issuedEpoch + 1
			if _, err := conn.ExecContext(context.Background(), `
				UPDATE browser_client_families
				SET epoch = ?, updated_at = ?
				WHERE client_id = ? AND epoch <= ?
			`, family.Epoch, now, family.ClientID, issuedEpoch); err != nil {
				return classifyBrowserSessionStoreError("advance browser client epoch", err)
			}
		}
		if _, err := conn.ExecContext(context.Background(), `
			UPDATE browser_sessions
			SET revoked_at = ?, revoke_reason = ?
			WHERE client_id = ? AND issued_epoch < ? AND revoked_at = ''
		`, now, strings.TrimSpace(reason), family.ClientID, family.Epoch); err != nil {
			return classifyBrowserSessionStoreError("revoke browser client family sessions", err)
		}
		return nil
	})
	if err != nil {
		return BrowserClientFamily{}, fmt.Errorf("fence browser client family: %w", err)
	}
	return family, nil
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
	var updated int64
	now := formatBrowserSessionTime(s.now().UTC())
	err := s.withImmediateTransaction(context.Background(), func(conn *sql.Conn) error {
		var exhausted int
		if err := conn.QueryRowContext(context.Background(), `
			SELECT COUNT(*)
			FROM browser_client_families
			WHERE epoch >= ?
		`, maxBrowserSessionEpoch).Scan(&exhausted); err != nil {
			return classifyBrowserSessionStoreError(
				"check browser client epoch exhaustion",
				err,
			)
		}
		if exhausted != 0 {
			return ErrBrowserSessionEpochExhausted
		}
		if _, err := conn.ExecContext(context.Background(), `
			UPDATE browser_client_families
			SET epoch = epoch + 1, updated_at = ?
		`, now); err != nil {
			return classifyBrowserSessionStoreError("advance all browser client epochs", err)
		}
		result, err := conn.ExecContext(context.Background(), `
			UPDATE browser_sessions
			SET revoked_at = ?, revoke_reason = ?
			WHERE revoked_at = ''
		`, now, strings.TrimSpace(reason))
		if err != nil {
			return classifyBrowserSessionStoreError("revoke all browser sessions", err)
		}
		updated, err = result.RowsAffected()
		if err != nil {
			return classifyBrowserSessionStoreError("read revoked browser session count", err)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("revoke all browser sessions: %w", err)
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
		SELECT id, client_id, issued_epoch, device_label, created_at, last_active_at, expires_at,
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
		SELECT id, client_id, issued_epoch, device_label, created_at, last_active_at, expires_at,
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
		SELECT id, client_id, issued_epoch, device_label, created_at, last_active_at, expires_at,
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
		&session.ClientID,
		&session.IssuedEpoch,
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
		&row.session.ClientID,
		&row.session.IssuedEpoch,
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
		ErrBrowserSessionStaleEpoch,
		ErrBrowserSessionClientUninitialized,
		ErrBrowserSessionClientMismatch,
		ErrBrowserSessionEpochExhausted,
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

func deriveBrowserSessionCSRFToken(sessionToken string, expiresAt time.Time) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("kbase-browser-session-csrf-v1\x00"))
	_, _ = hash.Write([]byte(sessionToken))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(formatBrowserSessionTime(expiresAt)))
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
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

func validateBrowserSessionClientID(clientID string) error {
	if len(clientID) < minBrowserSessionClientIDBytes ||
		len(clientID) > maxBrowserSessionClientIDBytes {
		return fmt.Errorf(
			"browser client id length must be between %d and %d bytes: %w",
			minBrowserSessionClientIDBytes,
			maxBrowserSessionClientIDBytes,
			ErrBrowserSessionInvalidArgument,
		)
	}
	for _, value := range []byte(clientID) {
		if (value >= 'a' && value <= 'z') ||
			(value >= 'A' && value <= 'Z') ||
			(value >= '0' && value <= '9') ||
			value == '_' ||
			value == '-' {
			continue
		}
		return fmt.Errorf(
			"browser client id contains invalid characters: %w",
			ErrBrowserSessionInvalidArgument,
		)
	}
	return nil
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
			if err := createBrowserClientFamilyTableV2(tx); err != nil {
				return fmt.Errorf("create browser client family schema version 2: %w", err)
			}
			if err := createBrowserSessionTableV2(tx, "browser_sessions"); err != nil {
				return fmt.Errorf("create browser session schema version 2: %w", err)
			}
			if err := createBrowserSessionActiveIndex(tx); err != nil {
				return fmt.Errorf("create browser session active index: %w", err)
			}
		}
		if exists {
			if err := migrateBrowserSessionV1ToV2(tx); err != nil {
				return fmt.Errorf("migrate browser session schema version 1 to 2: %w", err)
			}
		}
		if _, err := tx.Exec(`PRAGMA user_version = 2`); err != nil {
			return fmt.Errorf("set browser session schema version 2: %w", err)
		}
		if err := validateBrowserSessionSchemaV2(tx); err != nil {
			return fmt.Errorf("validate browser session schema version 2: %w", err)
		}
	case 1:
		if err := validateBrowserSessionSchemaV1(tx); err != nil {
			return fmt.Errorf("validate browser session schema version 1: %w", err)
		}
		if err := normalizeBrowserSessionTimesV1(tx); err != nil {
			return fmt.Errorf("normalize browser session schema version 1 times: %w", err)
		}
		if err := migrateBrowserSessionV1ToV2(tx); err != nil {
			return fmt.Errorf("migrate browser session schema version 1 to 2: %w", err)
		}
		if _, err := tx.Exec(`PRAGMA user_version = 2`); err != nil {
			return fmt.Errorf("set browser session schema version 2: %w", err)
		}
		if err := validateBrowserSessionSchemaV2(tx); err != nil {
			return fmt.Errorf("validate browser session schema version 2: %w", err)
		}
	case browserSessionSchemaVersion:
		if err := validateBrowserSessionSchemaV2(tx); err != nil {
			return fmt.Errorf("validate browser session schema version 2: %w", err)
		}
		if err := normalizeBrowserSessionTimesV2(tx); err != nil {
			return fmt.Errorf("normalize browser session schema version 2 times: %w", err)
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

func createBrowserClientFamilyTableV2(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE browser_client_families (
			client_id TEXT PRIMARY KEY,
			epoch INTEGER NOT NULL CHECK (epoch >= 1),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create browser_client_families table: %w", err)
	}
	return nil
}

func createBrowserSessionTableV2(tx *sql.Tx, tableName string) error {
	switch tableName {
	case "browser_sessions", "browser_sessions_v2":
	default:
		return fmt.Errorf("unsupported browser session migration table %q", tableName)
	}
	statement := fmt.Sprintf(`
		CREATE TABLE %s (
			id TEXT PRIMARY KEY,
			client_id TEXT NOT NULL REFERENCES browser_client_families(client_id),
			issued_epoch INTEGER NOT NULL CHECK (issued_epoch >= 1),
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

func migrateBrowserSessionV1ToV2(tx *sql.Tx) error {
	rows, err := tx.Query(`
		SELECT id, token_hash, csrf_hash, csrf_expires_at, device_label,
			user_agent_hash, created_at, last_active_at, expires_at,
			revoked_at, revoke_reason
		FROM browser_sessions
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("read version 1 browser sessions: %w", err)
	}
	var sessions [][]any
	for rows.Next() {
		var (
			id, csrfExpiresAt, deviceLabel     string
			createdAt, lastActiveAt, expiresAt string
			revokedAt, revokeReason            string
			tokenHash, csrfHash, userAgentHash []byte
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
			rows.Close()
			return fmt.Errorf("scan version 1 browser session: %w", err)
		}
		sessions = append(sessions, []any{
			id,
			legacyBrowserSessionClientID(id),
			int64(1),
			tokenHash,
			csrfHash,
			csrfExpiresAt,
			deviceLabel,
			userAgentHash,
			createdAt,
			lastActiveAt,
			expiresAt,
			revokedAt,
			revokeReason,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate version 1 browser sessions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close version 1 browser session rows: %w", err)
	}

	if err := createBrowserClientFamilyTableV2(tx); err != nil {
		return err
	}
	if err := createBrowserSessionTableV2(tx, "browser_sessions_v2"); err != nil {
		return err
	}
	for _, session := range sessions {
		if _, err := tx.Exec(`
			INSERT INTO browser_client_families (client_id, epoch, created_at, updated_at)
			VALUES (?, 1, ?, ?)
		`, session[1], session[8], session[9]); err != nil {
			return fmt.Errorf("copy browser client family %q: %w", session[1], err)
		}
		if _, err := tx.Exec(`
			INSERT INTO browser_sessions_v2 (
				id, client_id, issued_epoch, token_hash, csrf_hash, csrf_expires_at,
				device_label, user_agent_hash, created_at, last_active_at, expires_at,
				revoked_at, revoke_reason
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, session...); err != nil {
			return fmt.Errorf("copy version 1 browser session %q: %w", session[0], err)
		}
	}
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_browser_sessions_active`); err != nil {
		return fmt.Errorf("drop version 1 browser session active index: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE browser_sessions`); err != nil {
		return fmt.Errorf("drop version 1 browser session table: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE browser_sessions_v2 RENAME TO browser_sessions`); err != nil {
		return fmt.Errorf("activate browser session schema version 2: %w", err)
	}
	if err := createBrowserSessionActiveIndex(tx); err != nil {
		return fmt.Errorf("create browser session active index: %w", err)
	}
	return nil
}

func legacyBrowserSessionClientID(sessionID string) string {
	hash := sha256.Sum256([]byte("kbase-browser-client-v2\x00" + sessionID))
	return "legacy_" + base64.RawURLEncoding.EncodeToString(hash[:18])
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

func normalizeBrowserSessionTimesV2(tx *sql.Tx) error {
	if err := normalizeBrowserSessionTimesV1(tx); err != nil {
		return err
	}
	rows, err := tx.Query(`
		SELECT client_id, created_at, updated_at
		FROM browser_client_families
	`)
	if err != nil {
		return fmt.Errorf("read version 2 browser client family times: %w", err)
	}
	var updates [][]any
	for rows.Next() {
		var clientID, createdAt, updatedAt string
		if err := rows.Scan(&clientID, &createdAt, &updatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan version 2 browser client family: %w", err)
		}
		originalCreatedAt, originalUpdatedAt := createdAt, updatedAt
		if createdAt, err = canonicalizePersistedBrowserSessionTime(
			"version 2",
			"client created_at",
			createdAt,
			false,
		); err != nil {
			rows.Close()
			return err
		}
		if updatedAt, err = canonicalizePersistedBrowserSessionTime(
			"version 2",
			"client updated_at",
			updatedAt,
			false,
		); err != nil {
			rows.Close()
			return err
		}
		if createdAt != originalCreatedAt || updatedAt != originalUpdatedAt {
			updates = append(updates, []any{createdAt, updatedAt, clientID})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate version 2 browser client families: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close version 2 browser client family rows: %w", err)
	}
	for _, update := range updates {
		if _, err := tx.Exec(`
			UPDATE browser_client_families
			SET created_at = ?, updated_at = ?
			WHERE client_id = ?
		`, update...); err != nil {
			return fmt.Errorf("update version 2 browser client family %q times: %w", update[2], err)
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

var browserSessionColumnsV2 = []browserSessionColumnInfo{
	{name: "id", dataType: "TEXT", primaryKey: 1},
	{name: "client_id", dataType: "TEXT", notNull: true},
	{name: "issued_epoch", dataType: "INTEGER", notNull: true},
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

var browserClientFamilyColumnsV2 = []browserSessionColumnInfo{
	{name: "client_id", dataType: "TEXT", primaryKey: 1},
	{name: "epoch", dataType: "INTEGER", notNull: true},
	{name: "created_at", dataType: "TEXT", notNull: true},
	{name: "updated_at", dataType: "TEXT", notNull: true},
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

func validateBrowserSessionSchemaV2(tx *sql.Tx) error {
	columns, err := readBrowserSessionColumns(tx)
	if err != nil {
		return err
	}
	if err := compareBrowserSessionColumns(columns, browserSessionColumnsV2); err != nil {
		return err
	}
	familyColumns, err := readBrowserSessionTableColumns(tx, "browser_client_families")
	if err != nil {
		return err
	}
	if err := compareBrowserSessionColumns(familyColumns, browserClientFamilyColumnsV2); err != nil {
		return fmt.Errorf("browser_client_families schema: %w", err)
	}
	if err := validateSQLiteTableCheckConstraint(
		tx,
		"browser_sessions",
		[]string{"issued_epoch", ">=", "1"},
	); err != nil {
		return fmt.Errorf("browser_sessions CHECK constraints: %w", err)
	}
	if err := validateSQLiteTableCheckConstraint(
		tx,
		"browser_client_families",
		[]string{"epoch", ">=", "1"},
	); err != nil {
		return fmt.Errorf("browser_client_families CHECK constraints: %w", err)
	}
	if err := validateBrowserSessionClientForeignKey(tx); err != nil {
		return err
	}
	if err := validateBrowserSessionIndexes(tx, false); err != nil {
		return err
	}
	if err := validateBrowserSessionForeignKeyData(tx); err != nil {
		return err
	}
	return validateBrowserSessionEpochData(tx)
}

func validateSQLiteTableCheckConstraint(
	tx *sql.Tx,
	tableName string,
	expected []string,
) error {
	switch tableName {
	case "browser_sessions", "browser_client_families":
	default:
		return fmt.Errorf("unsupported CHECK constraint table %q", tableName)
	}
	var statement string
	if err := tx.QueryRow(`
		SELECT sql
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, tableName).Scan(&statement); err != nil {
		return fmt.Errorf("read %s table SQL: %w", tableName, err)
	}
	tokens, err := tokenizeSQLiteSchema(statement)
	if err != nil {
		return fmt.Errorf("tokenize %s table SQL: %w", tableName, err)
	}
	checks, err := extractSQLiteCheckExpressions(tokens)
	if err != nil {
		return fmt.Errorf("parse %s CHECK constraints: %w", tableName, err)
	}
	for _, check := range checks {
		if equalSQLiteSchemaTokens(check, expected) {
			return nil
		}
	}
	return fmt.Errorf("required CHECK (%s) is missing", strings.Join(expected, " "))
}

func tokenizeSQLiteSchema(statement string) ([]string, error) {
	tokens := make([]string, 0, len(statement)/4)
	for index := 0; index < len(statement); {
		value := statement[index]
		switch {
		case value == ' ' || value == '\t' || value == '\r' || value == '\n':
			index++
		case value == '-' && index+1 < len(statement) && statement[index+1] == '-':
			index += 2
			for index < len(statement) && statement[index] != '\n' {
				index++
			}
		case value == '/' && index+1 < len(statement) && statement[index+1] == '*':
			end := strings.Index(statement[index+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("unterminated block comment")
			}
			index += end + 4
		case isSQLiteIdentifierStart(value):
			end := index + 1
			for end < len(statement) && isSQLiteIdentifierPart(statement[end]) {
				end++
			}
			tokens = append(tokens, strings.ToLower(statement[index:end]))
			index = end
		case value >= '0' && value <= '9':
			end := index + 1
			for end < len(statement) &&
				statement[end] >= '0' &&
				statement[end] <= '9' {
				end++
			}
			tokens = append(tokens, statement[index:end])
			index = end
		case value == '\'':
			end, err := consumeSQLiteQuotedToken(statement, index, '\'', '\'')
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, "<string>")
			index = end
		case value == '"' || value == '`':
			end, content, err := consumeSQLiteQuotedIdentifier(statement, index, value, value)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, strings.ToLower(content))
			index = end
		case value == '[':
			end, content, err := consumeSQLiteQuotedIdentifier(statement, index, '[', ']')
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, strings.ToLower(content))
			index = end
		case value == '>' || value == '<' || value == '!' || value == '=':
			operator := string(value)
			if index+1 < len(statement) && statement[index+1] == '=' {
				operator += "="
				index++
			} else if value == '<' &&
				index+1 < len(statement) &&
				statement[index+1] == '>' {
				operator += ">"
				index++
			}
			tokens = append(tokens, operator)
			index++
		case strings.ContainsRune("(),.;+-*/", rune(value)):
			tokens = append(tokens, string(value))
			index++
		default:
			return nil, fmt.Errorf("unsupported schema byte 0x%02x", value)
		}
	}
	return tokens, nil
}

func isSQLiteIdentifierStart(value byte) bool {
	return (value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z') ||
		value == '_'
}

func isSQLiteIdentifierPart(value byte) bool {
	return isSQLiteIdentifierStart(value) || (value >= '0' && value <= '9')
}

func consumeSQLiteQuotedToken(
	statement string,
	start int,
	open byte,
	close byte,
) (int, error) {
	index := start + 1
	for index < len(statement) {
		if statement[index] != close {
			index++
			continue
		}
		if close != ']' &&
			index+1 < len(statement) &&
			statement[index+1] == close {
			index += 2
			continue
		}
		return index + 1, nil
	}
	return 0, fmt.Errorf("unterminated quoted token %q", open)
}

func consumeSQLiteQuotedIdentifier(
	statement string,
	start int,
	open byte,
	close byte,
) (int, string, error) {
	end, err := consumeSQLiteQuotedToken(statement, start, open, close)
	if err != nil {
		return 0, "", err
	}
	content := statement[start+1 : end-1]
	if close != ']' {
		content = strings.ReplaceAll(content, string(close)+string(close), string(close))
	}
	return end, content, nil
}

func extractSQLiteCheckExpressions(tokens []string) ([][]string, error) {
	var checks [][]string
	for index := 0; index < len(tokens); index++ {
		if tokens[index] != "check" {
			continue
		}
		if index+1 >= len(tokens) || tokens[index+1] != "(" {
			return nil, fmt.Errorf("CHECK is not followed by an expression")
		}
		depth := 1
		start := index + 2
		end := start
		for ; end < len(tokens) && depth != 0; end++ {
			switch tokens[end] {
			case "(":
				depth++
			case ")":
				depth--
			}
		}
		if depth != 0 {
			return nil, fmt.Errorf("unterminated CHECK expression")
		}
		checks = append(checks, trimSQLiteExpressionParentheses(tokens[start:end-1]))
		index = end - 1
	}
	return checks, nil
}

func trimSQLiteExpressionParentheses(tokens []string) []string {
	for len(tokens) >= 2 && tokens[0] == "(" && tokens[len(tokens)-1] == ")" {
		depth := 0
		wrapsWholeExpression := true
		for index, token := range tokens {
			switch token {
			case "(":
				depth++
			case ")":
				depth--
				if depth == 0 && index != len(tokens)-1 {
					wrapsWholeExpression = false
				}
			}
			if depth < 0 {
				return tokens
			}
		}
		if !wrapsWholeExpression || depth != 0 {
			return tokens
		}
		tokens = tokens[1 : len(tokens)-1]
	}
	return tokens
}

func equalSQLiteSchemaTokens(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func validateBrowserSessionForeignKeyData(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("run browser session foreign key check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var (
			table  string
			rowID  sql.NullInt64
			parent string
			fkID   int
		)
		if err := rows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			return fmt.Errorf("scan browser session foreign key check: %w", err)
		}
		return fmt.Errorf(
			"browser session foreign key check failed for table %q parent %q constraint %d",
			table,
			parent,
			fkID,
		)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate browser session foreign key check: %w", err)
	}
	return nil
}

func validateBrowserSessionEpochData(tx *sql.Tx) error {
	var invalid int
	if err := tx.QueryRow(`
		SELECT COUNT(*)
		FROM browser_client_families
		WHERE typeof(epoch) != 'integer' OR epoch < 1
	`).Scan(&invalid); err != nil {
		return fmt.Errorf("validate browser client family epoch data: %w", err)
	}
	if invalid != 0 {
		return fmt.Errorf("invalid browser client family epoch data: %d rows", invalid)
	}
	if err := tx.QueryRow(`
		SELECT COUNT(*)
		FROM browser_sessions
		WHERE typeof(issued_epoch) != 'integer' OR issued_epoch < 1
	`).Scan(&invalid); err != nil {
		return fmt.Errorf("validate browser session issued epoch data: %w", err)
	}
	if invalid != 0 {
		return fmt.Errorf("invalid browser session issued epoch data: %d rows", invalid)
	}
	if err := tx.QueryRow(`
		SELECT COUNT(*)
		FROM browser_sessions AS sessions
		JOIN browser_client_families AS families
			ON families.client_id = sessions.client_id
		WHERE sessions.revoked_at = ''
			AND sessions.issued_epoch != families.epoch
	`).Scan(&invalid); err != nil {
		return fmt.Errorf("validate active browser session epoch data: %w", err)
	}
	if invalid != 0 {
		return fmt.Errorf("invalid active browser session epoch data: %d rows", invalid)
	}
	return nil
}

func readBrowserSessionTableColumns(
	tx *sql.Tx,
	tableName string,
) ([]browserSessionColumnInfo, error) {
	if tableName != "browser_client_families" {
		return nil, fmt.Errorf("unsupported browser session schema table %q", tableName)
	}
	rows, err := tx.Query(`PRAGMA table_info(browser_client_families)`)
	if err != nil {
		return nil, fmt.Errorf("inspect %s columns: %w", tableName, err)
	}
	defer rows.Close()
	columns := make([]browserSessionColumnInfo, 0, len(browserClientFamilyColumnsV2))
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
			return nil, fmt.Errorf("scan %s column: %w", tableName, err)
		}
		column.dataType = strings.ToUpper(column.dataType)
		column.notNull = notNull != 0
		column.primaryKey = primaryKey
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s columns: %w", tableName, err)
	}
	return columns, nil
}

func validateBrowserSessionClientForeignKey(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA foreign_key_list(browser_sessions)`)
	if err != nil {
		return fmt.Errorf("inspect browser session foreign keys: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var (
			id, sequence                        int
			table, from, to, onUpdate, onDelete string
			match                               string
		)
		if err := rows.Scan(
			&id,
			&sequence,
			&table,
			&from,
			&to,
			&onUpdate,
			&onDelete,
			&match,
		); err != nil {
			return fmt.Errorf("scan browser session foreign key: %w", err)
		}
		if table != "browser_client_families" ||
			from != "client_id" ||
			to != "client_id" ||
			onUpdate != "NO ACTION" ||
			onDelete != "NO ACTION" ||
			match != "NONE" {
			return fmt.Errorf("browser session client foreign key is incompatible")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate browser session foreign keys: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("browser session client foreign key count = %d, want 1", count)
	}
	return nil
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
