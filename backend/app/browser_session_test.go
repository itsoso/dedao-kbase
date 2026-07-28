package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
)

func TestBrowserSessionStoreSchema(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "sessions", "browser_sessions.sqlite3")
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            dbPath,
		Now:             func() time.Time { return time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC) },
		Random:          bytes.NewReader(make([]byte, 64)),
		TTL:             30 * 24 * time.Hour,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if got := store.DBPath(); got != dbPath {
		t.Fatalf("DBPath() = %q, want %q", got, dbPath)
	}
	parentInfo, err := os.Stat(filepath.Dir(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := parentInfo.Mode().Perm(); got&^os.FileMode(0o750) != 0 {
		t.Fatalf("session database parent mode = %o, want no permissions outside 750", got)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`PRAGMA table_info(browser_sessions)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type columnInfo struct {
		dataType     string
		notNull      bool
		defaultValue any
	}
	var columns []string
	columnDetails := make(map[string]columnInfo)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
		columnDetails[name] = columnInfo{
			dataType:     dataType,
			notNull:      notNull != 0,
			defaultValue: defaultValue,
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantColumns := []string{
		"id",
		"token_hash",
		"csrf_hash",
		"csrf_expires_at",
		"device_label",
		"user_agent_hash",
		"created_at",
		"last_active_at",
		"expires_at",
		"revoked_at",
		"revoke_reason",
	}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("browser_sessions columns = %#v, want %#v", columns, wantColumns)
	}
	var schemaVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaVersion != 1 {
		t.Fatalf("browser session schema version = %d, want 1", schemaVersion)
	}
	revokedAtColumn := columnDetails["revoked_at"]
	if revokedAtColumn.dataType != "TEXT" ||
		!revokedAtColumn.notNull ||
		revokedAtColumn.defaultValue != "''" {
		t.Fatalf("revoked_at schema = %#v, want TEXT NOT NULL DEFAULT ''", revokedAtColumn)
	}
	deviceLabelColumn := columnDetails["device_label"]
	if deviceLabelColumn.dataType != "TEXT" ||
		!deviceLabelColumn.notNull ||
		deviceLabelColumn.defaultValue != nil {
		t.Fatalf("device_label schema = %#v, want TEXT NOT NULL without a default", deviceLabelColumn)
	}

	var indexSQL string
	if err := db.QueryRow(`
		SELECT sql
		FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_browser_sessions_active'
	`).Scan(&indexSQL); err != nil {
		t.Fatal(err)
	}
	normalizedIndex := strings.Join(strings.Fields(strings.ToLower(indexSQL)), " ")
	const wantIndexSQL = "create index idx_browser_sessions_active on browser_sessions(revoked_at, expires_at, last_active_at)"
	if normalizedIndex != wantIndexSQL {
		t.Fatalf("active session index = %q, want %q", normalizedIndex, wantIndexSQL)
	}
	indexRows, err := db.Query(`PRAGMA index_info(idx_browser_sessions_active)`)
	if err != nil {
		t.Fatal(err)
	}
	defer indexRows.Close()
	var indexColumns []string
	for indexRows.Next() {
		var sequence, cid int
		var name string
		if err := indexRows.Scan(&sequence, &cid, &name); err != nil {
			t.Fatal(err)
		}
		indexColumns = append(indexColumns, name)
	}
	if err := indexRows.Err(); err != nil {
		t.Fatal(err)
	}
	wantIndexColumns := []string{"revoked_at", "expires_at", "last_active_at"}
	if !reflect.DeepEqual(indexColumns, wantIndexColumns) {
		t.Fatalf("active session index columns = %#v, want %#v", indexColumns, wantIndexColumns)
	}
	var legacyIndexCount int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_browser_sessions_active_token'
	`).Scan(&legacyIndexCount); err != nil {
		t.Fatal(err)
	}
	if legacyIndexCount != 0 {
		t.Fatal("legacy active token partial index must not exist")
	}
}

func TestBrowserSessionStoreCreateHashesPrivateCredentials(t *testing.T) {
	now := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	randomBytes := make([]byte, 64)
	for index := range randomBytes {
		randomBytes[index] = byte(index + 1)
	}
	randomReader := bytes.NewReader(randomBytes)
	dbPath := filepath.Join(t.TempDir(), "sessions", "browser_sessions.sqlite3")
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            dbPath,
		Now:             func() time.Time { return now },
		Random:          randomReader,
		TTL:             30 * 24 * time.Hour,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       10,
	})
	if err != nil {
		t.Fatal(err)
	}

	const (
		deviceLabelInput = " \tWork Mac  "
		deviceLabel      = "Work Mac"
		userAgent        = "KBaseBrowser/1.0 private-agent-value"
	)
	credentials, err := store.Create(BrowserSessionCreate{
		DeviceLabel: deviceLabelInput,
		UserAgent:   userAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Token == "" || credentials.CSRFToken == "" {
		t.Fatalf("Create() returned empty credentials: %#v", credentials)
	}
	if credentials.Token == credentials.CSRFToken {
		t.Fatal("session and CSRF credentials must be independent")
	}
	for name, raw := range map[string]string{
		"session": credentials.Token,
		"CSRF":    credentials.CSRFToken,
	} {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			t.Fatalf("%s credential is not raw URL-safe base64: %v", name, err)
		}
		if len(decoded) != 32 {
			t.Fatalf("%s credential decoded length = %d, want 32", name, len(decoded))
		}
	}
	if randomReader.Len() != 0 {
		t.Fatalf("Create() consumed %d random bytes, want exactly 64", len(randomBytes)-randomReader.Len())
	}
	if credentials.Session.ID == "" {
		t.Fatal("Create() returned an empty public session ID")
	}
	if credentials.Session.DeviceLabel != deviceLabel ||
		!credentials.Session.CreatedAt.Equal(now) ||
		!credentials.Session.LastActiveAt.Equal(now) ||
		!credentials.Session.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("Create() returned unexpected session metadata: %#v", credentials.Session)
	}

	var (
		tokenHash, csrfHash, userAgentHash []byte
		csrfExpiresAt, storedDeviceLabel   string
		createdAt, lastActiveAt, expiresAt string
		revokedAt, revokeReason            string
	)
	err = store.db.QueryRow(`
		SELECT token_hash, csrf_hash, csrf_expires_at, device_label, user_agent_hash,
			created_at, last_active_at, expires_at, revoked_at, revoke_reason
		FROM browser_sessions
		WHERE id = ?
	`, credentials.Session.ID).Scan(
		&tokenHash,
		&csrfHash,
		&csrfExpiresAt,
		&storedDeviceLabel,
		&userAgentHash,
		&createdAt,
		&lastActiveAt,
		&expiresAt,
		&revokedAt,
		&revokeReason,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantTokenHash := sha256.Sum256([]byte(credentials.Token))
	wantCSRFHash := sha256.Sum256([]byte(credentials.CSRFToken))
	wantUserAgentHash := sha256.Sum256([]byte(userAgent))
	if !bytes.Equal(tokenHash, wantTokenHash[:]) {
		t.Fatalf("stored token hash = %x, want %x", tokenHash, wantTokenHash)
	}
	if !bytes.Equal(csrfHash, wantCSRFHash[:]) {
		t.Fatalf("stored CSRF hash = %x, want %x", csrfHash, wantCSRFHash)
	}
	if !bytes.Equal(userAgentHash, wantUserAgentHash[:]) {
		t.Fatalf("stored User-Agent hash = %x, want %x", userAgentHash, wantUserAgentHash)
	}
	if storedDeviceLabel != deviceLabel {
		t.Fatalf("stored device label = %q, want normalized %q", storedDeviceLabel, deviceLabel)
	}
	if csrfExpiresAt == "" || createdAt == "" || lastActiveAt == "" || expiresAt == "" {
		t.Fatal("session timestamps must be stored")
	}
	if revokedAt != "" || revokeReason != "" {
		t.Fatalf("new session is unexpectedly revoked: revoked_at=%q reason=%q", revokedAt, revokeReason)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(dbPath + "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for name, privateValue := range map[string]string{
			"session credential": credentials.Token,
			"CSRF credential":    credentials.CSRFToken,
			"raw User-Agent":     userAgent,
		} {
			if bytes.Contains(contents, []byte(privateValue)) {
				t.Fatalf("%s appears in SQLite file %s", name, filepath.Base(path))
			}
		}
	}
}

func TestBrowserSessionStoreTimeTextOrdering(t *testing.T) {
	current := time.Date(2026, time.July, 28, 12, 0, 0, 100_000_000, time.UTC)
	randomBytes := make([]byte, 128)
	for index := range randomBytes {
		randomBytes[index] = byte(index + 1)
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            newBrowserSessionTestDBPath(t),
		Now:             func() time.Time { return current },
		Random:          bytes.NewReader(randomBytes),
		TTL:             time.Hour,
		RenewalInterval: time.Minute,
		MaxActive:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.Create(BrowserSessionCreate{DeviceLabel: "first"})
	if err != nil {
		t.Fatal(err)
	}
	current = time.Date(2026, time.July, 28, 12, 0, 0, 100_000_001, time.UTC)
	second, err := store.Create(BrowserSessionCreate{DeviceLabel: "second"})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := store.db.Query(`SELECT id, last_active_at FROM browser_sessions ORDER BY last_active_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var orderedIDs []string
	storedTimes := make(map[string]string)
	for rows.Next() {
		var id, storedTime string
		if err := rows.Scan(&id, &storedTime); err != nil {
			t.Fatal(err)
		}
		orderedIDs = append(orderedIDs, id)
		storedTimes[id] = storedTime
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{first.Session.ID, second.Session.ID}
	if !reflect.DeepEqual(orderedIDs, wantOrder) {
		t.Fatalf("TEXT time order = %#v, want chronological order %#v (stored=%#v)", orderedIDs, wantOrder, storedTimes)
	}

	for id, storedTime := range storedTimes {
		if len(storedTime) != len("2006-01-02T15:04:05.000000000Z") {
			t.Fatalf("stored time for %s has variable width: %q", id, storedTime)
		}
		if _, err := parseBrowserSessionTime(storedTime); err != nil {
			t.Fatalf("stored time for %s is not fixed-width UTC nanoseconds: %q: %v", id, storedTime, err)
		}
	}
	if _, err := parseBrowserSessionTime("2026-07-28T12:00:00.1Z"); err == nil {
		t.Fatal("parseBrowserSessionTime() accepted a variable-width timestamp")
	}
}

func TestBrowserSessionStoreMigratesLegacyV0(t *testing.T) {
	dbPath := newBrowserSessionTestDBPath(t)
	tokenHash := sha256.Sum256([]byte("legacy-token"))
	csrfHash := sha256.Sum256([]byte("legacy-csrf"))
	userAgentHash := sha256.Sum256([]byte("legacy-agent"))
	secondTokenHash := sha256.Sum256([]byte("legacy-token-second"))
	secondCSRFHash := sha256.Sum256([]byte("legacy-csrf-second"))
	secondUserAgentHash := sha256.Sum256([]byte("legacy-agent-second"))
	revokedAt := "2026-07-28T12:05:00.1Z"
	const (
		sessionID       = "session_legacy"
		secondSessionID = "session_legacy_second"
	)
	seeds := []legacyBrowserSessionSeed{
		{
			id:            sessionID,
			tokenHash:     tokenHash[:],
			csrfHash:      csrfHash[:],
			csrfExpiresAt: "2026-07-28T12:15:00Z",
			deviceLabel:   "Legacy Browser",
			userAgentHash: userAgentHash[:],
			createdAt:     "2026-07-28T12:00:00Z",
			lastActiveAt:  "2026-07-28T12:01:00.1Z",
			expiresAt:     "2026-08-27T12:01:00Z",
			revokedAt:     &revokedAt,
			revokeReason:  "signed out",
		},
		{
			id:            secondSessionID,
			tokenHash:     secondTokenHash[:],
			csrfHash:      secondCSRFHash[:],
			csrfExpiresAt: "2026-07-28T12:15:00.1Z",
			deviceLabel:   "Second Browser",
			userAgentHash: secondUserAgentHash[:],
			createdAt:     "2026-07-28T12:00:00.1Z",
			lastActiveAt:  "2026-07-28T12:01:00.100000001Z",
			expiresAt:     "2026-08-27T12:01:00.1Z",
		},
	}
	seedLegacyBrowserSessionDB(t, dbPath, false, seeds)

	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var schemaVersion int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaVersion != 1 {
		t.Fatalf("migrated schema version = %d, want 1", schemaVersion)
	}

	for _, seed := range seeds {
		got := readLegacyBrowserSessionSeed(t, store.db, seed.id)
		if got.id != seed.id ||
			!bytes.Equal(got.tokenHash, seed.tokenHash) ||
			!bytes.Equal(got.csrfHash, seed.csrfHash) ||
			got.deviceLabel != seed.deviceLabel ||
			!bytes.Equal(got.userAgentHash, seed.userAgentHash) ||
			got.revokeReason != seed.revokeReason {
			t.Fatalf("legacy non-time data changed during migration for %s", seed.id)
		}
		assertCanonicalMigratedTime(t, "csrf_expires_at", seed.csrfExpiresAt, got.csrfExpiresAt)
		assertCanonicalMigratedTime(t, "created_at", seed.createdAt, got.createdAt)
		assertCanonicalMigratedTime(t, "last_active_at", seed.lastActiveAt, got.lastActiveAt)
		assertCanonicalMigratedTime(t, "expires_at", seed.expiresAt, got.expiresAt)
		if seed.revokedAt == nil {
			if got.revokedAt != "" {
				t.Fatalf("migrated revoked_at for %s = %q, want empty", seed.id, got.revokedAt)
			}
		} else {
			assertCanonicalMigratedTime(t, "revoked_at", *seed.revokedAt, got.revokedAt)
		}
	}

	rows, err := store.db.Query(`SELECT id FROM browser_sessions ORDER BY last_active_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var orderedIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		orderedIDs = append(orderedIDs, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{sessionID, secondSessionID}; !reflect.DeepEqual(orderedIDs, want) {
		t.Fatalf("migrated TEXT order = %#v, want chronological order %#v", orderedIDs, want)
	}
	assertMigratedBrowserSessionIndexes(t, store.db)
}

func TestBrowserSessionStoreLegacyMigrationRollsBackInvalidTime(t *testing.T) {
	dbPath := newBrowserSessionTestDBPath(t)
	tokenHash := sha256.Sum256([]byte("legacy-invalid-token"))
	csrfHash := sha256.Sum256([]byte("legacy-invalid-csrf"))
	userAgentHash := sha256.Sum256([]byte("legacy-invalid-agent"))
	seed := legacyBrowserSessionSeed{
		id:            "session_invalid_time",
		tokenHash:     tokenHash[:],
		csrfHash:      csrfHash[:],
		csrfExpiresAt: "2026-07-28T12:15:00Z",
		deviceLabel:   "Legacy Browser",
		userAgentHash: userAgentHash[:],
		createdAt:     "not-a-time",
		lastActiveAt:  "2026-07-28T12:01:00.1Z",
		expiresAt:     "2026-08-27T12:01:00Z",
	}
	seedLegacyBrowserSessionDB(t, dbPath, false, []legacyBrowserSessionSeed{seed})

	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
	if store != nil {
		store.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "canonicalize legacy browser session created_at") {
		t.Fatalf("legacy invalid-time migration error = %v, want contextual parse error", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Fatalf("failed migration schema version = %d, want rollback to 0", version)
	}
	got := readLegacyBrowserSessionSeed(t, db, seed.id)
	if got.createdAt != seed.createdAt {
		t.Fatalf("failed migration changed created_at to %q, want %q", got.createdAt, seed.createdAt)
	}
	var legacyIndexCount, temporaryTableCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_browser_sessions_active_token'
	`).Scan(&legacyIndexCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'browser_sessions_v1'
	`).Scan(&temporaryTableCount); err != nil {
		t.Fatal(err)
	}
	if legacyIndexCount != 1 || temporaryTableCount != 0 {
		t.Fatalf("failed migration did not roll back: legacy_index=%d temporary_table=%d", legacyIndexCount, temporaryTableCount)
	}
}

func TestBrowserSessionStoreMigratesLegacyV0WithBothKnownIndexes(t *testing.T) {
	dbPath := newBrowserSessionTestDBPath(t)
	tokenHash := sha256.Sum256([]byte("legacy-dual-token"))
	csrfHash := sha256.Sum256([]byte("legacy-dual-csrf"))
	userAgentHash := sha256.Sum256([]byte("legacy-dual-agent"))
	seedLegacyBrowserSessionDB(t, dbPath, true, []legacyBrowserSessionSeed{{
		id:            "session_dual_indexes",
		tokenHash:     tokenHash[:],
		csrfHash:      csrfHash[:],
		csrfExpiresAt: "2026-07-28T12:15:00Z",
		deviceLabel:   "Legacy Browser",
		userAgentHash: userAgentHash[:],
		createdAt:     "2026-07-28T12:00:00Z",
		lastActiveAt:  "2026-07-28T12:01:00.1Z",
		expiresAt:     "2026-08-27T12:01:00Z",
	}})

	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assertMigratedBrowserSessionIndexes(t, store.db)
}

func TestBrowserSessionStoreNormalizesPersistedV1Times(t *testing.T) {
	dbPath := newBrowserSessionTestDBPath(t)
	tokenHash := sha256.Sum256([]byte("version-one-token"))
	csrfHash := sha256.Sum256([]byte("version-one-csrf"))
	userAgentHash := sha256.Sum256([]byte("version-one-agent"))
	revokedAt := "2026-07-28T12:05:00.1Z"
	seed := legacyBrowserSessionSeed{
		id:            "session_version_one",
		tokenHash:     tokenHash[:],
		csrfHash:      csrfHash[:],
		csrfExpiresAt: "2026-07-28T12:15:00Z",
		deviceLabel:   "Persisted Browser",
		userAgentHash: userAgentHash[:],
		createdAt:     "2026-07-28T12:00:00.1Z",
		lastActiveAt:  "2026-07-28T12:01:00.100000001Z",
		expiresAt:     "2026-08-27T20:01:00+08:00",
		revokedAt:     &revokedAt,
		revokeReason:  "signed out",
	}
	seedV1BrowserSessionDB(t, dbPath, []legacyBrowserSessionSeed{seed})

	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	got := readLegacyBrowserSessionSeed(t, store.db, seed.id)
	assertCanonicalMigratedTime(t, "csrf_expires_at", seed.csrfExpiresAt, got.csrfExpiresAt)
	assertCanonicalMigratedTime(t, "created_at", seed.createdAt, got.createdAt)
	assertCanonicalMigratedTime(t, "last_active_at", seed.lastActiveAt, got.lastActiveAt)
	assertCanonicalMigratedTime(t, "expires_at", seed.expiresAt, got.expiresAt)
	assertCanonicalMigratedTime(t, "revoked_at", *seed.revokedAt, got.revokedAt)
	assertMigratedBrowserSessionIndexes(t, store.db)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reopened := readLegacyBrowserSessionSeed(t, store.db, seed.id)
	if !reflect.DeepEqual(reopened, got) {
		t.Fatalf("second v1 normalization changed canonical row:\nfirst:  %#v\nsecond: %#v", got, reopened)
	}
}

func TestBrowserSessionStorePersistedV1InvalidTimeRollsBack(t *testing.T) {
	dbPath := newBrowserSessionTestDBPath(t)
	firstTokenHash := sha256.Sum256([]byte("version-one-first-token"))
	firstCSRFHash := sha256.Sum256([]byte("version-one-first-csrf"))
	firstUserAgentHash := sha256.Sum256([]byte("version-one-first-agent"))
	secondTokenHash := sha256.Sum256([]byte("version-one-invalid-token"))
	secondCSRFHash := sha256.Sum256([]byte("version-one-invalid-csrf"))
	secondUserAgentHash := sha256.Sum256([]byte("version-one-invalid-agent"))
	seeds := []legacyBrowserSessionSeed{
		{
			id:            "session_version_one_valid",
			tokenHash:     firstTokenHash[:],
			csrfHash:      firstCSRFHash[:],
			csrfExpiresAt: "2026-07-28T12:15:00Z",
			deviceLabel:   "First Browser",
			userAgentHash: firstUserAgentHash[:],
			createdAt:     "2026-07-28T12:00:00.1Z",
			lastActiveAt:  "2026-07-28T12:01:00.1Z",
			expiresAt:     "2026-08-27T12:01:00Z",
		},
		{
			id:            "session_version_one_invalid",
			tokenHash:     secondTokenHash[:],
			csrfHash:      secondCSRFHash[:],
			csrfExpiresAt: "2026-07-28T12:15:00Z",
			deviceLabel:   "Invalid Browser",
			userAgentHash: secondUserAgentHash[:],
			createdAt:     "2026-07-28T12:00:00Z",
			lastActiveAt:  "not-a-time",
			expiresAt:     "2026-08-27T12:01:00Z",
		},
	}
	seedV1BrowserSessionDB(t, dbPath, seeds)

	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
	if store != nil {
		store.Close()
	}
	if err == nil ||
		!strings.Contains(err.Error(), "normalize browser session schema version 1 times") ||
		!strings.Contains(err.Error(), "canonicalize version 1 browser session last_active_at") {
		t.Fatalf("persisted v1 invalid-time error = %v, want contextual normalization error", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first := readLegacyBrowserSessionSeed(t, db, seeds[0].id)
	second := readLegacyBrowserSessionSeed(t, db, seeds[1].id)
	if first.createdAt != seeds[0].createdAt || second.lastActiveAt != seeds[1].lastActiveAt {
		t.Fatalf(
			"failed v1 normalization did not roll back: first created_at=%q second last_active_at=%q",
			first.createdAt,
			second.lastActiveAt,
		)
	}
	var schemaVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaVersion != 1 {
		t.Fatalf("failed v1 normalization schema version = %d, want 1", schemaVersion)
	}
}

func TestBrowserSessionStoreRejectsIncompatibleVersions(t *testing.T) {
	t.Run("invalid version one schema", func(t *testing.T) {
		dbPath := existingBrowserSessionTestDBPath(t)
		db, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			CREATE TABLE browser_sessions (id TEXT PRIMARY KEY);
			PRAGMA user_version = 1;
		`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
		if store != nil {
			store.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "validate browser session schema version 1") {
			t.Fatalf("NewBrowserSessionStore() error = %v, want contextual v1 validation error", err)
		}
	})

	t.Run("future version", func(t *testing.T) {
		dbPath := existingBrowserSessionTestDBPath(t)
		db, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`PRAGMA user_version = 2`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
		if store != nil {
			store.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "unsupported browser session database version 2") {
			t.Fatalf("NewBrowserSessionStore() error = %v, want future-version rejection", err)
		}
	})

	t.Run("version one legacy index", func(t *testing.T) {
		dbPath := newBrowserSessionTestDBPath(t)
		store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			DROP INDEX idx_browser_sessions_active;
			CREATE INDEX idx_browser_sessions_active_token
				ON browser_sessions(token_hash)
				WHERE revoked_at = '';
		`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		store, err = NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
		if store != nil {
			store.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "validate browser session schema version 1") {
			t.Fatalf("NewBrowserSessionStore() error = %v, want v1 index validation error", err)
		}
	})
}

func TestBrowserSessionStoreDirectoryPermissionCeiling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are not available on Windows")
	}
	for _, testCase := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "private directory", mode: 0o700},
		{name: "group-readable directory", mode: 0o750},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "sessions")
			if err := os.Mkdir(parent, testCase.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, testCase.mode); err != nil {
				t.Fatal(err)
			}
			store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
				Path: filepath.Join(parent, "browser_sessions.sqlite3"),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			info, err := os.Stat(parent)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != testCase.mode {
				t.Fatalf("directory mode = %o, want unchanged %o", got, testCase.mode)
			}
		})
	}

	for _, mode := range []os.FileMode{0o777, 0o755} {
		t.Run("reject unsafe "+mode.String(), func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "sessions")
			if err := os.Mkdir(parent, mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, mode); err != nil {
				t.Fatal(err)
			}
			store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
				Path: filepath.Join(parent, "browser_sessions.sqlite3"),
			})
			if store != nil {
				store.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "unsafe permissions") {
				t.Fatalf("NewBrowserSessionStore() error = %v, want unsafe-permissions rejection", err)
			}
			info, statErr := os.Stat(parent)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if got := info.Mode().Perm(); got != mode {
				t.Fatalf("rejected directory mode = %o, want unchanged %o", got, mode)
			}
		})
	}
}

func TestBrowserSessionStoreWindowsSkipsPOSIXModeCeiling(t *testing.T) {
	if err := validateBrowserSessionDirectoryModeForOS("windows", 0o777); err != nil {
		t.Fatalf("Windows mode validation error = %v, want POSIX mode bits ignored", err)
	}
}

func TestBrowserSessionStoreJSONRevocationSemantics(t *testing.T) {
	activePayload, err := json.Marshal(BrowserSession{ID: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(activePayload, []byte(`"revoked_at"`)) {
		t.Errorf("active session JSON includes revoked_at: %s", activePayload)
	}

	revokedAt := time.Date(2026, time.July, 28, 12, 30, 0, 123, time.UTC)
	revoked := BrowserSession{ID: "revoked", RevokedAt: &revokedAt}
	revokedPayload, err := json.Marshal(revoked)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(revokedPayload, &document); err != nil {
		t.Fatal(err)
	}
	var encodedRevokedAt string
	if err := json.Unmarshal(document["revoked_at"], &encodedRevokedAt); err != nil {
		t.Fatalf("revoked_at is not a JSON string: %s", revokedPayload)
	}
	if _, err := time.Parse(time.RFC3339Nano, encodedRevokedAt); err != nil {
		t.Fatalf("revoked_at is not a valid timestamp: %q: %v", encodedRevokedAt, err)
	}
}

func TestBrowserSessionStoreClosedState(t *testing.T) {
	t.Run("nil store create", func(t *testing.T) {
		var store *BrowserSessionStore
		var (
			createErr  error
			panicValue any
		)
		func() {
			defer func() { panicValue = recover() }()
			_, createErr = store.Create(BrowserSessionCreate{DeviceLabel: "browser"})
		}()
		if panicValue != nil {
			t.Fatalf("nil store Create() panicked: %v", panicValue)
		}
		if createErr == nil || !strings.Contains(createErr.Error(), "browser session store is nil") {
			t.Fatalf("nil store Create() error = %v, want contextual error", createErr)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("nil store Close() error = %v, want nil", err)
		}
	})

	t.Run("close is idempotent and create fails before randomness", func(t *testing.T) {
		random := &countingReader{reader: bytes.NewReader(make([]byte, 64))}
		store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
			Path:   newBrowserSessionTestDBPath(t),
			Random: random,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("second Close() error = %v, want nil", err)
		}
		if _, err := store.Create(BrowserSessionCreate{DeviceLabel: "browser"}); err == nil ||
			!strings.Contains(err.Error(), "browser session store is closed") {
			t.Fatalf("Create() after Close() error = %v, want closed error", err)
		}
		if random.bytesRead != 0 {
			t.Fatalf("Create() after Close() consumed %d random bytes, want 0", random.bytesRead)
		}
	})
}

func TestBrowserSessionStoreConcurrentCloseAndCreate(t *testing.T) {
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path: newBrowserSessionTestDBPath(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	const creators = 16
	start := make(chan struct{})
	errs := make(chan error, creators+1)
	var wait sync.WaitGroup
	for index := 0; index < creators; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := store.Create(BrowserSessionCreate{DeviceLabel: "browser"})
			if err != nil && !strings.Contains(err.Error(), "closed") {
				errs <- err
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		errs <- store.Close()
	}()
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Close/Create error = %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("idempotent Close() after concurrency = %v", err)
	}
}

func TestBrowserSessionStoreRandomFailuresDoNotInsert(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		randomData  []byte
		wantContext string
	}{
		{name: "first credential", wantContext: "generate session credential"},
		{name: "second credential", randomData: make([]byte, 32), wantContext: "generate CSRF credential"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			random := &failAfterReader{
				data: append([]byte(nil), testCase.randomData...),
				err:  errors.New("entropy unavailable"),
			}
			store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
				Path:   newBrowserSessionTestDBPath(t),
				Random: random,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			_, err = store.Create(BrowserSessionCreate{DeviceLabel: "browser"})
			if err == nil || !strings.Contains(err.Error(), testCase.wantContext) {
				t.Fatalf("Create() error = %v, want context %q", err, testCase.wantContext)
			}
			if !errors.Is(err, ErrBrowserSessionUnavailable) {
				t.Fatalf("Create() entropy error = %v, want ErrBrowserSessionUnavailable", err)
			}
			var count int
			if err := store.db.QueryRow(`SELECT COUNT(*) FROM browser_sessions`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("failed credential generation inserted %d sessions, want 0", count)
			}
		})
	}
}

func TestBrowserSessionStoreWrapsErrorsWithoutCredentials(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		parentFile := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(parentFile, []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
			Path: filepath.Join(parentFile, "browser_sessions.sqlite3"),
		})
		if err == nil || !strings.Contains(err.Error(), "prepare browser session database directory") {
			t.Fatalf("directory error = %v, want operation context", err)
		}
	})

	t.Run("insert", func(t *testing.T) {
		randomPair := make([]byte, 64)
		for index := range randomPair {
			randomPair[index] = byte(index + 1)
		}
		randomData := append(append([]byte(nil), randomPair...), randomPair...)
		store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
			Path:   newBrowserSessionTestDBPath(t),
			Random: bytes.NewReader(randomData),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if _, err := store.Create(BrowserSessionCreate{DeviceLabel: "first"}); err != nil {
			t.Fatal(err)
		}
		_, err = store.Create(BrowserSessionCreate{DeviceLabel: "second"})
		if err == nil || !strings.Contains(err.Error(), "insert browser session") {
			t.Fatalf("duplicate insert error = %v, want operation context", err)
		}
		sessionToken := base64.RawURLEncoding.EncodeToString(randomPair[:32])
		csrfToken := base64.RawURLEncoding.EncodeToString(randomPair[32:])
		if strings.Contains(err.Error(), sessionToken) || strings.Contains(err.Error(), csrfToken) {
			t.Fatalf("insert error leaked a generated credential: %v", err)
		}
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM browser_sessions`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("duplicate insert left %d records, want 1", count)
		}
	})
}

func TestBrowserSessionLifecycleSlidingExpiryAndRenewal(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            newBrowserSessionTestDBPath(t),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(1, 8)),
		RenewalInterval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	credentials, err := store.Create(BrowserSessionCreate{DeviceLabel: "Browser"})
	if err != nil {
		t.Fatal(err)
	}
	if want := clock.Now().Add(30 * 24 * time.Hour); !credentials.Session.ExpiresAt.Equal(want) {
		t.Fatalf("Create() expiry = %s, want exactly %s", credentials.Session.ExpiresAt, want)
	}
	createdExpiresAt := readBrowserSessionStoredTime(t, store.db, credentials.Session.ID, "expires_at")

	clock.Advance(4*time.Minute + 59*time.Second)
	first, err := store.AuthenticateAndRenew(credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	if first.Renewed || first.SetCookie {
		t.Fatalf("activity inside coalescing window renewed storage/cookie: %#v", first)
	}
	if want := clock.Now().Add(30 * 24 * time.Hour); !first.Session.ExpiresAt.Equal(want) {
		t.Fatalf("logical expiry = %s, want %s", first.Session.ExpiresAt, want)
	}
	if !first.CookieExpiresAt.Equal(createdExpiresAt) {
		t.Fatalf("unchanged Cookie expiry = %s, want persisted %s", first.CookieExpiresAt, createdExpiresAt)
	}
	assertBrowserSessionStoredActivity(t, store.db, credentials.Session.ID, credentials.Session.LastActiveAt, createdExpiresAt)

	clock.Advance(time.Second)
	second, err := store.AuthenticateAndRenew(credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	wantRenewedExpiry := clock.Now().Add(30 * 24 * time.Hour)
	if !second.Renewed || !second.SetCookie {
		t.Fatalf("activity at renewal boundary did not renew storage/cookie: %#v", second)
	}
	if !second.Session.LastActiveAt.Equal(clock.Now()) ||
		!second.Session.ExpiresAt.Equal(wantRenewedExpiry) ||
		!second.CookieExpiresAt.Equal(wantRenewedExpiry) {
		t.Fatalf("renewed auth metadata = %#v, want activity=%s expiry=%s", second, clock.Now(), wantRenewedExpiry)
	}
	assertBrowserSessionStoredActivity(t, store.db, credentials.Session.ID, clock.Now(), wantRenewedExpiry)

	clock.Advance(time.Minute)
	third, err := store.AuthenticateAndRenew(credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	if third.Renewed || third.SetCookie {
		t.Fatalf("second activity inside new coalescing window renewed: %#v", third)
	}
	assertBrowserSessionStoredActivity(t, store.db, credentials.Session.ID, second.Session.LastActiveAt, wantRenewedExpiry)
}

func TestBrowserSessionLifecycleRejectsMissingExpiredAndRevoked(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Now:    clock.Now,
		Random: bytes.NewReader(deterministicBrowserSessionBytes(2, 8)),
		TTL:    time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.AuthenticateAndRenew("missing"); !errors.Is(err, ErrBrowserSessionMissing) {
		t.Fatalf("missing AuthenticateAndRenew() error = %v, want ErrBrowserSessionMissing", err)
	}
	if _, err := store.Authenticate("missing"); !errors.Is(err, ErrBrowserSessionMissing) {
		t.Fatalf("missing Authenticate() error = %v, want ErrBrowserSessionMissing", err)
	}

	expired, err := store.Create(BrowserSessionCreate{DeviceLabel: "Expired"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Hour)
	if _, err := store.AuthenticateAndRenew(expired.Token); !errors.Is(err, ErrBrowserSessionExpired) {
		t.Fatalf("expired AuthenticateAndRenew() error = %v, want ErrBrowserSessionExpired", err)
	}
	if _, err := store.Authenticate(expired.Token); !errors.Is(err, ErrBrowserSessionExpired) {
		t.Fatalf("expired Authenticate() error = %v, want ErrBrowserSessionExpired", err)
	}

	clock.Advance(-30 * time.Minute)
	revoked, err := store.Create(BrowserSessionCreate{DeviceLabel: "Revoked"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeByToken(revoked.Token, "self logout"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAndRenew(revoked.Token); !errors.Is(err, ErrBrowserSessionRevoked) {
		t.Fatalf("revoked AuthenticateAndRenew() error = %v, want ErrBrowserSessionRevoked", err)
	}
	if _, err := store.Authenticate(revoked.Token); !errors.Is(err, ErrBrowserSessionRevoked) {
		t.Fatalf("revoked Authenticate() error = %v, want ErrBrowserSessionRevoked", err)
	}
}

func TestBrowserSessionLimitEvictsLeastRecentlyActive(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:      newBrowserSessionTestDBPath(t),
		Now:       clock.Now,
		Random:    bytes.NewReader(deterministicBrowserSessionBytes(3, 24)),
		MaxActive: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var created []BrowserSessionCredentials
	for index := 0; index < 10; index++ {
		credentials, err := store.Create(BrowserSessionCreate{
			DeviceLabel: fmt.Sprintf("Browser %02d", index),
		})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, credentials)
		clock.Advance(time.Second)
	}
	eleventh, err := store.Create(BrowserSessionCreate{DeviceLabel: "Browser 10"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.AuthenticateAndRenew(created[0].Token); !errors.Is(err, ErrBrowserSessionRevoked) {
		t.Fatalf("least-recently-active token error = %v, want revoked", err)
	}
	if _, err := store.AuthenticateAndRenew(created[1].Token); err != nil {
		t.Fatalf("second-oldest token was unexpectedly evicted: %v", err)
	}
	if _, err := store.AuthenticateAndRenew(eleventh.Token); err != nil {
		t.Fatalf("new token did not authenticate: %v", err)
	}
	assertBrowserSessionActiveCount(t, store.db, clock.Now(), 10)
}

func TestBrowserSessionLimitConcurrentStoresNeverExceedMaximum(t *testing.T) {
	dbPath := newBrowserSessionTestDBPath(t)
	fixedNow := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	first, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:      dbPath,
		Now:       func() time.Time { return fixedNow },
		Random:    bytes.NewReader(deterministicBrowserSessionBytes(11, 24)),
		MaxActive: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:      dbPath,
		Now:       func() time.Time { return fixedNow },
		Random:    bytes.NewReader(deterministicBrowserSessionBytes(101, 24)),
		MaxActive: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	const createsPerStore = 12
	start := make(chan struct{})
	errs := make(chan error, createsPerStore*2)
	var wait sync.WaitGroup
	for _, store := range []*BrowserSessionStore{first, second} {
		for index := 0; index < createsPerStore; index++ {
			wait.Add(1)
			go func(store *BrowserSessionStore) {
				defer wait.Done()
				<-start
				_, err := store.Create(BrowserSessionCreate{DeviceLabel: "Concurrent"})
				errs <- err
			}(store)
		}
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Create() error = %v", err)
		}
	}
	assertBrowserSessionActiveCount(t, first.db, fixedNow, 10)
}

func TestBrowserSessionRevokeOperationsAreIdempotent(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Now:    clock.Now,
		Random: bytes.NewReader(deterministicBrowserSessionBytes(4, 16)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.Create(BrowserSessionCreate{DeviceLabel: "First"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(BrowserSessionCreate{DeviceLabel: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.Create(BrowserSessionCreate{DeviceLabel: "Third"})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.RevokeByToken(first.Token, "self logout"); err != nil {
		t.Fatal(err)
	}
	firstRevokedAt, firstReason := readBrowserSessionRevocation(t, store.db, first.Session.ID)
	clock.Advance(time.Hour)
	if err := store.RevokeByToken(first.Token, "changed reason"); err != nil {
		t.Fatal(err)
	}
	assertBrowserSessionRevocation(t, store.db, first.Session.ID, firstRevokedAt, firstReason)

	if err := store.Revoke(second.Session.ID, "admin revoke"); err != nil {
		t.Fatal(err)
	}
	secondRevokedAt, secondReason := readBrowserSessionRevocation(t, store.db, second.Session.ID)
	clock.Advance(time.Hour)
	if err := store.Revoke(second.Session.ID, "changed reason"); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke("missing", "no-op"); err != nil {
		t.Fatalf("missing Revoke() error = %v, want idempotent no-op", err)
	}
	if err := store.RevokeByToken("missing", "no-op"); err != nil {
		t.Fatalf("missing RevokeByToken() error = %v, want idempotent no-op", err)
	}
	assertBrowserSessionRevocation(t, store.db, second.Session.ID, secondRevokedAt, secondReason)

	count, err := store.RevokeAll("admin revoke all")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("first RevokeAll() count = %d, want 1", count)
	}
	count, err = store.RevokeAll("changed reason")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("second RevokeAll() count = %d, want 0", count)
	}
	thirdRevokedAt, thirdReason := readBrowserSessionRevocation(t, store.db, third.Session.ID)
	if thirdReason != "admin revoke all" || !thirdRevokedAt.Equal(clock.Now()) {
		t.Fatalf("third revocation = (%s, %q), want (%s, %q)", thirdRevokedAt, thirdReason, clock.Now(), "admin revoke all")
	}
}

func TestBrowserSessionLifecycleListReturnsStablePublicMetadata(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Now:    clock.Now,
		Random: bytes.NewReader(deterministicBrowserSessionBytes(5, 8)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.Create(BrowserSessionCreate{DeviceLabel: "First"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	second, err := store.Create(BrowserSessionCreate{DeviceLabel: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(first.Session.ID, "signed out"); err != nil {
		t.Fatal(err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != second.Session.ID || sessions[1].ID != first.Session.ID {
		t.Fatalf("List() order = %#v, want newest creation first with stable IDs", sessions)
	}
	payload, err := json.Marshal(sessions)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"token", "csrf", "user_agent", "hash"} {
		if bytes.Contains(bytes.ToLower(payload), []byte(forbidden)) {
			t.Fatalf("List() JSON contains private field %q: %s", forbidden, payload)
		}
	}
}

func TestBrowserSessionCleanupHonorsRetentionBoundary(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Now:    clock.Now,
		Random: bytes.NewReader(deterministicBrowserSessionBytes(6, 16)),
		TTL:    48 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	oldExpired, err := store.Create(BrowserSessionCreate{DeviceLabel: "Old expired"})
	if err != nil {
		t.Fatal(err)
	}
	oldRevoked, err := store.Create(BrowserSessionCreate{DeviceLabel: "Old revoked"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(oldRevoked.Session.ID, "old"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(72 * time.Hour)
	boundary, err := store.Create(BrowserSessionCreate{DeviceLabel: "Boundary"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(boundary.Session.ID, "boundary"); err != nil {
		t.Fatal(err)
	}
	recent, err := store.Create(BrowserSessionCreate{DeviceLabel: "Recent active"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Cleanup(-time.Second); !errors.Is(err, ErrBrowserSessionInvalidArgument) {
		t.Fatalf("Cleanup(-1s) error = %v, want ErrBrowserSessionInvalidArgument", err)
	}
	deleted, err := store.Cleanup(0)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("Cleanup(0) deleted %d records, want old expired and old revoked", deleted)
	}
	assertBrowserSessionIDs(t, store.db, []string{boundary.Session.ID, recent.Session.ID})
	if oldExpired.Session.ID == oldRevoked.Session.ID {
		t.Fatal("test credentials unexpectedly collided")
	}
}

func TestBrowserSessionLifecycleAuthenticateDoesNotRenewActivity(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            newBrowserSessionTestDBPath(t),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(420, 2)),
		TTL:             time.Hour,
		RenewalInterval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	credentials, err := store.Create(BrowserSessionCreate{DeviceLabel: "Read Only Auth"})
	if err != nil {
		t.Fatal(err)
	}
	createdLastActiveAt := credentials.Session.LastActiveAt
	createdExpiresAt := credentials.Session.ExpiresAt
	clock.Advance(5 * time.Minute)

	session, err := store.Authenticate(credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !session.LastActiveAt.Equal(createdLastActiveAt) ||
		!session.ExpiresAt.Equal(createdExpiresAt) {
		t.Fatalf(
			"Authenticate() session activity = (%s, %s), want stored (%s, %s)",
			session.LastActiveAt,
			session.ExpiresAt,
			createdLastActiveAt,
			createdExpiresAt,
		)
	}
	assertBrowserSessionStoredActivity(
		t,
		store.db,
		credentials.Session.ID,
		createdLastActiveAt,
		createdExpiresAt,
	)

	renewed, err := store.AuthenticateAndRenew(credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.Renewed || !renewed.SetCookie {
		t.Fatalf("AuthenticateAndRenew() at boundary = %#v, want renewal", renewed)
	}
	assertBrowserSessionStoredActivity(
		t,
		store.db,
		credentials.Session.ID,
		clock.Now(),
		clock.Now().Add(time.Hour),
	)
}

func TestBrowserSessionLifecycleIssueCSRFIsStableConcurrentAndRotatesAfterExpiry(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Now:    clock.Now,
		Random: bytes.NewReader(deterministicBrowserSessionBytes(421, 2)),
		TTL:    time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	credentials, err := store.Create(BrowserSessionCreate{DeviceLabel: "Concurrent CSRF"})
	if err != nil {
		t.Fatal(err)
	}
	type issueResult struct {
		token     string
		expiresAt time.Time
		err       error
	}
	const callers = 12
	start := make(chan struct{})
	results := make(chan issueResult, callers)
	var wait sync.WaitGroup
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			token, expiresAt, issueErr := store.IssueCSRF(credentials.Token)
			results <- issueResult{token: token, expiresAt: expiresAt, err: issueErr}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	var issuedToken string
	var issuedExpiresAt time.Time
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent IssueCSRF() error: %v", result.err)
		}
		if issuedToken == "" {
			issuedToken = result.token
			issuedExpiresAt = result.expiresAt
			continue
		}
		if result.token != issuedToken || !result.expiresAt.Equal(issuedExpiresAt) {
			t.Fatalf(
				"concurrent IssueCSRF() = (%q, %s), want (%q, %s)",
				result.token,
				result.expiresAt,
				issuedToken,
				issuedExpiresAt,
			)
		}
	}
	if issuedToken == "" || issuedToken == credentials.CSRFToken {
		t.Fatalf("issued CSRF token = %q, want new stable credential", issuedToken)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(issuedToken)
	if err != nil || len(decoded) != sha256.Size {
		t.Fatalf("issued CSRF token encoding = %q len=%d err=%v", issuedToken, len(decoded), err)
	}
	if want := clock.Now().Add(browserSessionCSRFTTL); !issuedExpiresAt.Equal(want) {
		t.Fatalf("issued CSRF expiry = %s, want %s", issuedExpiresAt, want)
	}
	if err := store.ValidateCSRF(credentials.Session.ID, issuedToken); err != nil {
		t.Fatalf("issued CSRF did not validate: %v", err)
	}
	if err := store.ValidateCSRF(credentials.Session.ID, credentials.CSRFToken); !errors.Is(err, ErrBrowserSessionCSRFInvalid) {
		t.Fatalf("creation CSRF after issuance error = %v, want invalid", err)
	}
	var storedHash []byte
	if err := store.db.QueryRow(
		`SELECT csrf_hash FROM browser_sessions WHERE id = ?`,
		credentials.Session.ID,
	).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(issuedToken))
	if !bytes.Equal(storedHash, wantHash[:]) ||
		bytes.Contains(storedHash, []byte(issuedToken)) {
		t.Fatalf("stored CSRF value = %x, want hash-only %x", storedHash, wantHash)
	}

	reissued, reissuedExpiresAt, err := store.IssueCSRF(credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	if reissued != issuedToken || !reissuedExpiresAt.Equal(issuedExpiresAt) {
		t.Fatalf(
			"same-window IssueCSRF() = (%q, %s), want (%q, %s)",
			reissued,
			reissuedExpiresAt,
			issuedToken,
			issuedExpiresAt,
		)
	}

	clock.Advance(browserSessionCSRFTTL)
	rotated, rotatedExpiresAt, err := store.IssueCSRF(credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	if rotated == issuedToken || rotated == "" {
		t.Fatalf("post-expiry IssueCSRF() token = %q, want rotation", rotated)
	}
	if want := clock.Now().Add(browserSessionCSRFTTL); !rotatedExpiresAt.Equal(want) {
		t.Fatalf("post-expiry CSRF expiry = %s, want %s", rotatedExpiresAt, want)
	}
	if err := store.ValidateCSRF(credentials.Session.ID, issuedToken); !errors.Is(err, ErrBrowserSessionCSRFInvalid) {
		t.Fatalf("superseded CSRF error = %v, want invalid", err)
	}
	if err := store.ValidateCSRF(credentials.Session.ID, rotated); err != nil {
		t.Fatalf("rotated issued CSRF did not validate: %v", err)
	}
}

func TestBrowserSessionLifecycleCSRFUsesRotationExpiryAndTypedConflicts(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Now:    clock.Now,
		Random: bytes.NewReader(deterministicBrowserSessionBytes(7, 12)),
		TTL:    time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.Create(BrowserSessionCreate{DeviceLabel: "First"})
	if err != nil {
		t.Fatal(err)
	}
	if createdCSRFExpiresAt := readBrowserSessionStoredTime(
		t,
		store.db,
		first.Session.ID,
		"csrf_expires_at",
	); !createdCSRFExpiresAt.Equal(clock.Now().Add(15 * time.Minute)) {
		t.Fatalf(
			"created CSRF expiry = %s, want %s",
			createdCSRFExpiresAt,
			clock.Now().Add(15*time.Minute),
		)
	}
	if err := store.ValidateCSRF(first.Session.ID, first.CSRFToken); err != nil {
		t.Fatalf("created CSRF did not validate: %v", err)
	}
	if err := store.ValidateCSRF(first.Session.ID, first.CSRFToken+"x"); !errors.Is(err, ErrBrowserSessionCSRFInvalid) {
		t.Fatalf("wrong CSRF error = %v, want ErrBrowserSessionCSRFInvalid", err)
	}

	oldToken := first.CSRFToken
	clock.Advance(time.Minute)
	rotated, expiresAt, err := store.RotateCSRF(first.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated == "" || rotated == oldToken {
		t.Fatalf("RotateCSRF() token = %q, want new non-empty token", rotated)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(rotated)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("rotated CSRF is not a 32-byte raw URL-safe token: len=%d err=%v", len(decoded), err)
	}
	if want := clock.Now().Add(15 * time.Minute); !expiresAt.Equal(want) {
		t.Fatalf("RotateCSRF() expiry = %s, want %s", expiresAt, want)
	}
	var storedRotatedHash []byte
	if err := store.db.QueryRow(`
		SELECT csrf_hash
		FROM browser_sessions
		WHERE id = ?
	`, first.Session.ID).Scan(&storedRotatedHash); err != nil {
		t.Fatal(err)
	}
	wantRotatedHash := sha256.Sum256([]byte(rotated))
	if !bytes.Equal(storedRotatedHash, wantRotatedHash[:]) {
		t.Fatalf("stored rotated CSRF hash = %x, want %x", storedRotatedHash, wantRotatedHash)
	}
	if err := store.ValidateCSRF(first.Session.ID, oldToken); !errors.Is(err, ErrBrowserSessionCSRFInvalid) {
		t.Fatalf("old CSRF error = %v, want invalid", err)
	}
	if err := store.ValidateCSRF(first.Session.ID, rotated); err != nil {
		t.Fatalf("rotated CSRF did not validate: %v", err)
	}
	clock.Advance(15 * time.Minute)
	if err := store.ValidateCSRF(first.Session.ID, rotated); !errors.Is(err, ErrBrowserSessionCSRFExpired) {
		t.Fatalf("expired CSRF error = %v, want ErrBrowserSessionCSRFExpired", err)
	}
	if _, _, err := store.RotateCSRF("missing"); !errors.Is(err, ErrBrowserSessionMissing) {
		t.Fatalf("missing RotateCSRF() error = %v, want missing", err)
	}

	second, err := store.Create(BrowserSessionCreate{DeviceLabel: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	firstCSRFBytes, err := base64.RawURLEncoding.DecodeString(rotated)
	if err != nil {
		t.Fatal(err)
	}
	store.random = bytes.NewReader(firstCSRFBytes)
	if _, _, err := store.RotateCSRF(second.Session.ID); !errors.Is(err, ErrBrowserSessionConflict) {
		t.Fatalf("colliding RotateCSRF() error = %v, want ErrBrowserSessionConflict", err)
	}

	collisionBytes := deterministicBrowserSessionBytes(200, 2)
	store.random = bytes.NewReader(collisionBytes)
	if _, err := store.Create(BrowserSessionCreate{DeviceLabel: "Third"}); err != nil {
		t.Fatal(err)
	}
	store.random = bytes.NewReader(collisionBytes)
	if _, err := store.Create(BrowserSessionCreate{DeviceLabel: "Collision"}); !errors.Is(err, ErrBrowserSessionConflict) {
		t.Fatalf("colliding Create() error = %v, want ErrBrowserSessionConflict", err)
	}
}

func TestBrowserSessionLifecycleCSRFRejectsMissingExpiredAndRevokedSessions(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Now:    clock.Now,
		Random: bytes.NewReader(deterministicBrowserSessionBytes(9, 8)),
		TTL:    time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.ValidateCSRF("missing", "credential"); !errors.Is(err, ErrBrowserSessionMissing) {
		t.Fatalf("missing ValidateCSRF() error = %v, want missing", err)
	}
	if _, _, err := store.IssueCSRF("missing"); !errors.Is(err, ErrBrowserSessionMissing) {
		t.Fatalf("missing IssueCSRF() error = %v, want missing", err)
	}

	revoked, err := store.Create(BrowserSessionCreate{DeviceLabel: "Revoked"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(revoked.Session.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateCSRF(revoked.Session.ID, revoked.CSRFToken); !errors.Is(err, ErrBrowserSessionRevoked) {
		t.Fatalf("revoked ValidateCSRF() error = %v, want revoked", err)
	}
	if _, _, err := store.RotateCSRF(revoked.Session.ID); !errors.Is(err, ErrBrowserSessionRevoked) {
		t.Fatalf("revoked RotateCSRF() error = %v, want revoked", err)
	}
	if _, _, err := store.IssueCSRF(revoked.Token); !errors.Is(err, ErrBrowserSessionRevoked) {
		t.Fatalf("revoked IssueCSRF() error = %v, want revoked", err)
	}

	expired, err := store.Create(BrowserSessionCreate{DeviceLabel: "Expired"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Hour)
	if err := store.ValidateCSRF(expired.Session.ID, expired.CSRFToken); !errors.Is(err, ErrBrowserSessionExpired) {
		t.Fatalf("expired ValidateCSRF() error = %v, want expired", err)
	}
	if _, _, err := store.RotateCSRF(expired.Session.ID); !errors.Is(err, ErrBrowserSessionExpired) {
		t.Fatalf("expired RotateCSRF() error = %v, want expired", err)
	}
	if _, _, err := store.IssueCSRF(expired.Token); !errors.Is(err, ErrBrowserSessionExpired) {
		t.Fatalf("expired IssueCSRF() error = %v, want expired", err)
	}
}

func TestBrowserSessionLifecycleMapsDatabaseFailureToUnavailable(t *testing.T) {
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Random: bytes.NewReader(deterministicBrowserSessionBytes(8, 4)),
	})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := store.Create(BrowserSessionCreate{DeviceLabel: "Browser"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := store.AuthenticateAndRenew(credentials.Token); !errors.Is(err, ErrBrowserSessionUnavailable) {
		t.Fatalf("AuthenticateAndRenew() closed DB error = %v, want unavailable", err)
	}
	if _, err := store.Authenticate(credentials.Token); !errors.Is(err, ErrBrowserSessionUnavailable) {
		t.Fatalf("Authenticate() closed DB error = %v, want unavailable", err)
	}
	if _, _, err := store.IssueCSRF(credentials.Token); !errors.Is(err, ErrBrowserSessionUnavailable) {
		t.Fatalf("IssueCSRF() closed DB error = %v, want unavailable", err)
	}
	if _, err := store.List(); !errors.Is(err, ErrBrowserSessionUnavailable) {
		t.Fatalf("List() closed DB error = %v, want unavailable", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserSessionStoreRejectsCompleteCredentialCollisionMatrix(t *testing.T) {
	t.Run("same token and CSRF in one Create", func(t *testing.T) {
		raw := sha256.Sum256([]byte("same-create-credential"))
		random := append(append([]byte(nil), raw[:]...), raw[:]...)
		store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
			Path:   newBrowserSessionTestDBPath(t),
			Random: bytes.NewReader(random),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()

		if _, err := store.Create(BrowserSessionCreate{DeviceLabel: "Collision"}); !errors.Is(err, ErrBrowserSessionConflict) {
			t.Fatalf("same-credential Create() error = %v, want ErrBrowserSessionConflict", err)
		}
		assertBrowserSessionRowCount(t, store.db, 0)
	})

	for _, testCase := range []struct {
		name                string
		collidingCredential string
		collisionAsNewToken bool
	}{
		{
			name:                "new token collides with existing CSRF",
			collidingCredential: "csrf",
			collisionAsNewToken: true,
		},
		{
			name:                "new CSRF collides with existing token",
			collidingCredential: "token",
			collisionAsNewToken: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
				Path:   newBrowserSessionTestDBPath(t),
				Random: bytes.NewReader(deterministicBrowserSessionBytes(301, 4)),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			existing, err := store.Create(BrowserSessionCreate{DeviceLabel: "Existing"})
			if err != nil {
				t.Fatal(err)
			}

			colliding := existing.Token
			if testCase.collidingCredential == "csrf" {
				colliding = existing.CSRFToken
			}
			collidingRaw, err := base64.RawURLEncoding.DecodeString(colliding)
			if err != nil {
				t.Fatal(err)
			}
			uniqueRaw := sha256.Sum256([]byte("unique-" + testCase.name))
			var random []byte
			if testCase.collisionAsNewToken {
				random = append(append([]byte(nil), collidingRaw...), uniqueRaw[:]...)
			} else {
				random = append(append([]byte(nil), uniqueRaw[:]...), collidingRaw...)
			}
			store.random = bytes.NewReader(random)

			if _, err := store.Create(BrowserSessionCreate{DeviceLabel: "Cross-column"}); !errors.Is(err, ErrBrowserSessionConflict) {
				t.Fatalf("cross-column Create() error = %v, want ErrBrowserSessionConflict", err)
			}
			assertBrowserSessionRowCount(t, store.db, 1)
		})
	}
}

func TestBrowserSessionLifecycleRotateCSRFRejectsSessionTokenCollision(t *testing.T) {
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Random: bytes.NewReader(deterministicBrowserSessionBytes(302, 6)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := store.Create(BrowserSessionCreate{DeviceLabel: "First"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(BrowserSessionCreate{DeviceLabel: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	tokenRaw, err := base64.RawURLEncoding.DecodeString(first.Token)
	if err != nil {
		t.Fatal(err)
	}
	store.random = bytes.NewReader(tokenRaw)

	if _, _, err := store.RotateCSRF(second.Session.ID); !errors.Is(err, ErrBrowserSessionConflict) {
		t.Fatalf("RotateCSRF() session-token collision error = %v, want ErrBrowserSessionConflict", err)
	}
	if err := store.ValidateCSRF(second.Session.ID, second.CSRFToken); err != nil {
		t.Fatalf("RotateCSRF() collision changed the previous CSRF: %v", err)
	}
}

func TestBrowserSessionStoreConstructorErrorsAreTypedAndPreserveCause(t *testing.T) {
	t.Run("empty path is invalid argument", func(t *testing.T) {
		_, err := NewBrowserSessionStore(BrowserSessionStoreConfig{})
		if !errors.Is(err, ErrBrowserSessionInvalidArgument) {
			t.Fatalf("empty-path error = %v, want ErrBrowserSessionInvalidArgument", err)
		}
		if errors.Is(err, ErrBrowserSessionUnavailable) {
			t.Fatalf("empty-path error = %v, must not be unavailable", err)
		}
	})

	t.Run("directory permission failure", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX directory permissions are not enforced on Windows")
		}
		parent := filepath.Join(t.TempDir(), "blocked")
		if err := os.Mkdir(parent, 0o000); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(parent, 0o700)
		_, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
			Path: filepath.Join(parent, "child", "browser_sessions.sqlite3"),
		})
		if !errors.Is(err, ErrBrowserSessionUnavailable) {
			t.Fatalf("directory error = %v, want ErrBrowserSessionUnavailable", err)
		}
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("directory error = %v, want underlying os.ErrPermission preserved", err)
		}
	})

	t.Run("database open or pragma IO failure", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatal(err)
		}
		directoryPath := filepath.Join(root, "database-directory")
		if err := os.Mkdir(directoryPath, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: directoryPath})
		if !errors.Is(err, ErrBrowserSessionUnavailable) {
			t.Fatalf("database directory error = %v, want ErrBrowserSessionUnavailable", err)
		}
		var sqliteError sqlite3.Error
		if !errors.As(err, &sqliteError) {
			t.Fatalf("database directory error = %v, want underlying sqlite3.Error preserved", err)
		}
	})

	t.Run("migration failure", func(t *testing.T) {
		dbPath := newBrowserSessionTestDBPath(t)
		store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`PRAGMA user_version = 999`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		_, err = NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
		if !errors.Is(err, ErrBrowserSessionUnavailable) {
			t.Fatalf("migration error = %v, want ErrBrowserSessionUnavailable", err)
		}
		if err == nil || !strings.Contains(err.Error(), "migrate browser session database") {
			t.Fatalf("migration error = %v, want migration context", err)
		}
	})

	t.Run("migration busy failure preserves sqlite cause", func(t *testing.T) {
		dbPath := newBrowserSessionTestDBPath(t)
		store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		lockDB, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer lockDB.Close()
		lockDB.SetMaxOpenConns(1)
		if _, err := lockDB.Exec(`BEGIN EXCLUSIVE`); err != nil {
			t.Fatal(err)
		}
		defer lockDB.Exec(`ROLLBACK`)

		_, err = NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
		if !errors.Is(err, ErrBrowserSessionUnavailable) {
			t.Fatalf("busy migration error = %v, want ErrBrowserSessionUnavailable", err)
		}
		var sqliteError sqlite3.Error
		if !errors.As(err, &sqliteError) || sqliteError.Code != sqlite3.ErrBusy {
			t.Fatalf("busy migration error = %v, want underlying SQLITE_BUSY", err)
		}
	})
}

func TestBrowserSessionStoreValidatesRenewalIntervalBeforeFilesystemChanges(t *testing.T) {
	t.Run("defaults remain valid", func(t *testing.T) {
		store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
			Path: newBrowserSessionTestDBPath(t),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if store.ttl != 30*24*time.Hour || store.renewalInterval != 5*time.Minute {
			t.Fatalf(
				"default lifecycle = (ttl=%s renewal=%s), want (720h, 5m)",
				store.ttl,
				store.renewalInterval,
			)
		}
	})

	for _, testCase := range []struct {
		name    string
		ttl     time.Duration
		renewal time.Duration
	}{
		{name: "renewal equals TTL", ttl: 5 * time.Minute, renewal: 5 * time.Minute},
		{name: "renewal exceeds TTL", ttl: 4 * time.Minute, renewal: 5 * time.Minute},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			dbPath := filepath.Join(root, "must-not-exist", "browser_sessions.sqlite3")
			_, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
				Path:            dbPath,
				TTL:             testCase.ttl,
				RenewalInterval: testCase.renewal,
			})
			if !errors.Is(err, ErrBrowserSessionInvalidArgument) {
				t.Fatalf("invalid lifecycle error = %v, want ErrBrowserSessionInvalidArgument", err)
			}
			if _, statErr := os.Stat(filepath.Dir(dbPath)); !os.IsNotExist(statErr) {
				t.Fatalf("invalid lifecycle created or modified database directory: stat error = %v", statErr)
			}
		})
	}
}

func TestBrowserSessionLifecycleListIgnoresPrivateCSRFStorage(t *testing.T) {
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Random: bytes.NewReader(deterministicBrowserSessionBytes(305, 2)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created, err := store.Create(BrowserSessionCreate{DeviceLabel: "Public browser"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.Exec(`
		UPDATE browser_sessions
		SET csrf_hash = X'', csrf_expires_at = 'not-a-time'
		WHERE id = ?
	`, created.Session.ID); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List() parsed private CSRF storage: %v", err)
	}
	if len(sessions) != 1 ||
		sessions[0].ID != created.Session.ID ||
		sessions[0].DeviceLabel != created.Session.DeviceLabel {
		t.Fatalf("List() public metadata = %#v, want created session", sessions)
	}
	payload, err := json.Marshal(sessions)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"token", "csrf", "user_agent", "hash"} {
		if bytes.Contains(bytes.ToLower(payload), []byte(forbidden)) {
			t.Fatalf("List() JSON contains private field %q: %s", forbidden, payload)
		}
	}

	if _, err := store.db.Exec(`
		UPDATE browser_sessions
		SET expires_at = 'not-a-public-time'
		WHERE id = ?
	`, created.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); !errors.Is(err, ErrBrowserSessionUnavailable) {
		t.Fatalf("List() corrupt public time error = %v, want ErrBrowserSessionUnavailable", err)
	}
}

func TestBrowserSessionCleanupNegativeRetentionIsInvalidAndDoesNotDelete(t *testing.T) {
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:   newBrowserSessionTestDBPath(t),
		Random: bytes.NewReader(deterministicBrowserSessionBytes(303, 2)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Create(BrowserSessionCreate{DeviceLabel: "Retained"}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Cleanup(-time.Second); !errors.Is(err, ErrBrowserSessionInvalidArgument) {
		t.Fatalf("Cleanup(-1s) error = %v, want ErrBrowserSessionInvalidArgument", err)
	}
	assertBrowserSessionRowCount(t, store.db, 1)
}

func TestBrowserSessionLimitUsesIDAsStableFinalTieBreak(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:      newBrowserSessionTestDBPath(t),
		Now:       func() time.Time { return now },
		Random:    bytes.NewReader(deterministicBrowserSessionBytes(304, 2)),
		MaxActive: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var seededIDs []string
	for index := 9; index >= 0; index-- {
		id := fmt.Sprintf("session_seed_%02d", index)
		seededIDs = append(seededIDs, id)
		tokenHash := sha256.Sum256([]byte("seed-token-" + id))
		csrfHash := sha256.Sum256([]byte("seed-csrf-" + id))
		userAgentHash := sha256.Sum256([]byte("seed-agent-" + id))
		if _, err := store.db.Exec(`
			INSERT INTO browser_sessions (
				id, token_hash, csrf_hash, csrf_expires_at, device_label,
				user_agent_hash, created_at, last_active_at, expires_at,
				revoked_at, revoke_reason
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', '')
		`,
			id,
			tokenHash[:],
			csrfHash[:],
			formatBrowserSessionTime(now.Add(15*time.Minute)),
			id,
			userAgentHash[:],
			formatBrowserSessionTime(now),
			formatBrowserSessionTime(now),
			formatBrowserSessionTime(now.Add(30*24*time.Hour)),
		); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := store.Create(BrowserSessionCreate{DeviceLabel: "Eleventh"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range seededIDs {
		revokedAt, reason := readBrowserSessionRevocationState(t, store.db, id)
		if id == "session_seed_00" {
			if revokedAt == "" || reason != "session_limit" {
				t.Fatalf("smallest ID revocation = (%q, %q), want session_limit", revokedAt, reason)
			}
			continue
		}
		if revokedAt != "" || reason != "" {
			t.Fatalf("seeded session %q was unexpectedly revoked: (%q, %q)", id, revokedAt, reason)
		}
	}
	assertBrowserSessionActiveCount(t, store.db, now, 10)
}

type browserSessionTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *browserSessionTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *browserSessionTestClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

func deterministicBrowserSessionBytes(seed, credentialPairs int) []byte {
	data := make([]byte, 0, credentialPairs*2*browserSessionCredentialBytes)
	for index := 0; index < credentialPairs*2; index++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("browser-session-test/%d/%d", seed, index)))
		data = append(data, sum[:]...)
	}
	return data
}

func readBrowserSessionStoredTime(t *testing.T, db *sql.DB, id, column string) time.Time {
	t.Helper()
	switch column {
	case "created_at", "last_active_at", "expires_at", "csrf_expires_at", "revoked_at":
	default:
		t.Fatalf("unsupported browser session time column %q", column)
	}
	var value string
	query := fmt.Sprintf(`SELECT %s FROM browser_sessions WHERE id = ?`, column)
	if err := db.QueryRow(query, id).Scan(&value); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseBrowserSessionTime(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func assertBrowserSessionStoredActivity(
	t *testing.T,
	db *sql.DB,
	id string,
	wantLastActiveAt time.Time,
	wantExpiresAt time.Time,
) {
	t.Helper()
	var lastActiveAt, expiresAt string
	if err := db.QueryRow(`
		SELECT last_active_at, expires_at
		FROM browser_sessions
		WHERE id = ?
	`, id).Scan(&lastActiveAt, &expiresAt); err != nil {
		t.Fatal(err)
	}
	parsedLastActiveAt, err := parseBrowserSessionTime(lastActiveAt)
	if err != nil {
		t.Fatal(err)
	}
	parsedExpiresAt, err := parseBrowserSessionTime(expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if !parsedLastActiveAt.Equal(wantLastActiveAt) || !parsedExpiresAt.Equal(wantExpiresAt) {
		t.Fatalf(
			"stored activity = (%s, %s), want (%s, %s)",
			parsedLastActiveAt,
			parsedExpiresAt,
			wantLastActiveAt,
			wantExpiresAt,
		)
	}
}

func assertBrowserSessionActiveCount(t *testing.T, db *sql.DB, now time.Time, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM browser_sessions
		WHERE revoked_at = '' AND expires_at > ?
	`, formatBrowserSessionTime(now)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("active browser session count = %d, want %d", count, want)
	}
}

func assertBrowserSessionRowCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM browser_sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("browser session row count = %d, want %d", count, want)
	}
}

func readBrowserSessionRevocationState(t *testing.T, db *sql.DB, id string) (string, string) {
	t.Helper()
	var revokedAt, reason string
	if err := db.QueryRow(`
		SELECT revoked_at, revoke_reason
		FROM browser_sessions
		WHERE id = ?
	`, id).Scan(&revokedAt, &reason); err != nil {
		t.Fatal(err)
	}
	return revokedAt, reason
}

func readBrowserSessionRevocation(t *testing.T, db *sql.DB, id string) (time.Time, string) {
	t.Helper()
	var revokedAt, reason string
	if err := db.QueryRow(`
		SELECT revoked_at, revoke_reason
		FROM browser_sessions
		WHERE id = ?
	`, id).Scan(&revokedAt, &reason); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseBrowserSessionTime(revokedAt)
	if err != nil {
		t.Fatal(err)
	}
	return parsed, reason
}

func assertBrowserSessionRevocation(
	t *testing.T,
	db *sql.DB,
	id string,
	wantRevokedAt time.Time,
	wantReason string,
) {
	t.Helper()
	revokedAt, reason := readBrowserSessionRevocation(t, db, id)
	if !revokedAt.Equal(wantRevokedAt) || reason != wantReason {
		t.Fatalf(
			"browser session %q revocation = (%s, %q), want (%s, %q)",
			id,
			revokedAt,
			reason,
			wantRevokedAt,
			wantReason,
		)
	}
}

func assertBrowserSessionIDs(t *testing.T, db *sql.DB, want []string) {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM browser_sessions ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices := func(values []string) []string {
		cloned := append([]string(nil), values...)
		sort.Strings(cloned)
		return cloned
	}
	if !reflect.DeepEqual(slices(got), slices(want)) {
		t.Fatalf("browser session IDs = %#v, want %#v", got, want)
	}
}

type legacyBrowserSessionSeed struct {
	id            string
	tokenHash     []byte
	csrfHash      []byte
	csrfExpiresAt string
	deviceLabel   string
	userAgentHash []byte
	createdAt     string
	lastActiveAt  string
	expiresAt     string
	revokedAt     *string
	revokeReason  string
}

type migratedBrowserSessionRow struct {
	id            string
	tokenHash     []byte
	csrfHash      []byte
	csrfExpiresAt string
	deviceLabel   string
	userAgentHash []byte
	createdAt     string
	lastActiveAt  string
	expiresAt     string
	revokedAt     string
	revokeReason  string
}

func seedLegacyBrowserSessionDB(
	t *testing.T,
	dbPath string,
	bothIndexes bool,
	seeds []legacyBrowserSessionSeed,
) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE browser_sessions (
			id TEXT PRIMARY KEY,
			token_hash BLOB NOT NULL UNIQUE,
			csrf_hash BLOB NOT NULL,
			csrf_expires_at TEXT NOT NULL,
			device_label TEXT NOT NULL DEFAULT '',
			user_agent_hash BLOB NOT NULL,
			created_at TEXT NOT NULL,
			last_active_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT,
			revoke_reason TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX idx_browser_sessions_active_token
			ON browser_sessions(token_hash)
			WHERE revoked_at IS NULL;
	`); err != nil {
		t.Fatal(err)
	}
	if bothIndexes {
		if _, err := db.Exec(`
			CREATE INDEX idx_browser_sessions_active
				ON browser_sessions(revoked_at, expires_at, last_active_at)
		`); err != nil {
			t.Fatal(err)
		}
	}
	for _, seed := range seeds {
		var revokedAt any
		if seed.revokedAt != nil {
			revokedAt = *seed.revokedAt
		}
		if _, err := db.Exec(`
			INSERT INTO browser_sessions (
				id, token_hash, csrf_hash, csrf_expires_at, device_label,
				user_agent_hash, created_at, last_active_at, expires_at,
				revoked_at, revoke_reason
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			seed.id,
			seed.tokenHash,
			seed.csrfHash,
			seed.csrfExpiresAt,
			seed.deviceLabel,
			seed.userAgentHash,
			seed.createdAt,
			seed.lastActiveAt,
			seed.expiresAt,
			revokedAt,
			seed.revokeReason,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func seedV1BrowserSessionDB(t *testing.T, dbPath string, seeds []legacyBrowserSessionSeed) {
	t.Helper()
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, seed := range seeds {
		revokedAt := ""
		if seed.revokedAt != nil {
			revokedAt = *seed.revokedAt
		}
		if _, err := db.Exec(`
			INSERT INTO browser_sessions (
				id, token_hash, csrf_hash, csrf_expires_at, device_label,
				user_agent_hash, created_at, last_active_at, expires_at,
				revoked_at, revoke_reason
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			seed.id,
			seed.tokenHash,
			seed.csrfHash,
			seed.csrfExpiresAt,
			seed.deviceLabel,
			seed.userAgentHash,
			seed.createdAt,
			seed.lastActiveAt,
			seed.expiresAt,
			revokedAt,
			seed.revokeReason,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func newBrowserSessionTestDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "sessions", "browser_sessions.sqlite3")
}

func existingBrowserSessionTestDBPath(t *testing.T) string {
	t.Helper()
	dbPath := newBrowserSessionTestDBPath(t)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func readLegacyBrowserSessionSeed(t *testing.T, db *sql.DB, id string) migratedBrowserSessionRow {
	t.Helper()
	var row migratedBrowserSessionRow
	if err := db.QueryRow(`
		SELECT id, token_hash, csrf_hash, csrf_expires_at, device_label,
			user_agent_hash, created_at, last_active_at, expires_at,
			COALESCE(revoked_at, ''), revoke_reason
		FROM browser_sessions
		WHERE id = ?
	`, id).Scan(
		&row.id,
		&row.tokenHash,
		&row.csrfHash,
		&row.csrfExpiresAt,
		&row.deviceLabel,
		&row.userAgentHash,
		&row.createdAt,
		&row.lastActiveAt,
		&row.expiresAt,
		&row.revokedAt,
		&row.revokeReason,
	); err != nil {
		t.Fatal(err)
	}
	return row
}

func assertCanonicalMigratedTime(t *testing.T, field, legacy, migrated string) {
	t.Helper()
	legacyTime, err := time.Parse(time.RFC3339Nano, legacy)
	if err != nil {
		t.Fatalf("invalid legacy %s test value %q: %v", field, legacy, err)
	}
	migratedTime, err := parseBrowserSessionTime(migrated)
	if err != nil {
		t.Fatalf("migrated %s is not canonical: %q: %v", field, migrated, err)
	}
	if !migratedTime.Equal(legacyTime) {
		t.Fatalf("migrated %s = %s, want instant %s", field, migratedTime, legacyTime)
	}
	if want := formatBrowserSessionTime(legacyTime); migrated != want {
		t.Fatalf("migrated %s = %q, want canonical %q", field, migrated, want)
	}
}

func assertMigratedBrowserSessionIndexes(t *testing.T, db *sql.DB) {
	t.Helper()
	var legacyIndexCount, activeIndexCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_browser_sessions_active_token'
	`).Scan(&legacyIndexCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_browser_sessions_active'
	`).Scan(&activeIndexCount); err != nil {
		t.Fatal(err)
	}
	if legacyIndexCount != 0 || activeIndexCount != 1 {
		t.Fatalf("migration indexes: legacy=%d active=%d, want 0/1", legacyIndexCount, activeIndexCount)
	}

	rows, err := db.Query(`PRAGMA index_info(idx_browser_sessions_active)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var sequence, columnID int
		var name string
		if err := rows.Scan(&sequence, &columnID, &name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"revoked_at", "expires_at", "last_active_at"}; !reflect.DeepEqual(columns, want) {
		t.Fatalf("active index columns = %#v, want %#v", columns, want)
	}
}

type countingReader struct {
	reader    io.Reader
	bytesRead int
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	r.bytesRead += count
	return count, err
}

type failAfterReader struct {
	data []byte
	err  error
}

func (r *failAfterReader) Read(buffer []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	count := copy(buffer, r.data)
	r.data = r.data[count:]
	return count, nil
}
