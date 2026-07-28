package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
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
		Path:            filepath.Join(t.TempDir(), "sessions.sqlite3"),
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
	dbPath := filepath.Join(t.TempDir(), "browser_sessions.sqlite3")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
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
	`)
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("legacy-token"))
	csrfHash := sha256.Sum256([]byte("legacy-csrf"))
	userAgentHash := sha256.Sum256([]byte("legacy-agent"))
	const (
		sessionID      = "session_legacy"
		csrfExpiresAt  = "2026-07-28T12:15:00.000000000Z"
		deviceLabel    = "Legacy Browser"
		createdAt      = "2026-07-28T12:00:00.000000000Z"
		lastActiveAt   = "2026-07-28T12:01:00.000000000Z"
		expiresAt      = "2026-08-27T12:01:00.000000000Z"
		legacyRevoke   = ""
		legacyReason   = ""
		expectedSchema = 1
	)
	_, err = db.Exec(`
		INSERT INTO browser_sessions (
			id, token_hash, csrf_hash, csrf_expires_at, device_label,
			user_agent_hash, created_at, last_active_at, expires_at,
			revoked_at, revoke_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)
	`, sessionID, tokenHash[:], csrfHash[:], csrfExpiresAt, deviceLabel,
		userAgentHash[:], createdAt, lastActiveAt, expiresAt, legacyReason)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var schemaVersion int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaVersion != expectedSchema {
		t.Fatalf("migrated schema version = %d, want %d", schemaVersion, expectedSchema)
	}
	var (
		gotID, gotCSRFExpiresAt, gotDeviceLabel     string
		gotCreatedAt, gotLastActiveAt, gotExpiresAt string
		gotRevokedAt, gotRevokeReason               string
		gotTokenHash, gotCSRFHash, gotUserAgentHash []byte
	)
	err = store.db.QueryRow(`
		SELECT id, token_hash, csrf_hash, csrf_expires_at, device_label,
			user_agent_hash, created_at, last_active_at, expires_at,
			revoked_at, revoke_reason
		FROM browser_sessions
		WHERE id = ?
	`, sessionID).Scan(
		&gotID, &gotTokenHash, &gotCSRFHash, &gotCSRFExpiresAt, &gotDeviceLabel,
		&gotUserAgentHash, &gotCreatedAt, &gotLastActiveAt, &gotExpiresAt,
		&gotRevokedAt, &gotRevokeReason,
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotID != sessionID ||
		!bytes.Equal(gotTokenHash, tokenHash[:]) ||
		!bytes.Equal(gotCSRFHash, csrfHash[:]) ||
		gotCSRFExpiresAt != csrfExpiresAt ||
		gotDeviceLabel != deviceLabel ||
		!bytes.Equal(gotUserAgentHash, userAgentHash[:]) ||
		gotCreatedAt != createdAt ||
		gotLastActiveAt != lastActiveAt ||
		gotExpiresAt != expiresAt ||
		gotRevokedAt != legacyRevoke ||
		gotRevokeReason != legacyReason {
		t.Fatalf("legacy row changed during migration")
	}
	var legacyIndexCount, activeIndexCount int
	if err := store.db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_browser_sessions_active_token'
	`).Scan(&legacyIndexCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_browser_sessions_active'
	`).Scan(&activeIndexCount); err != nil {
		t.Fatal(err)
	}
	if legacyIndexCount != 0 || activeIndexCount != 1 {
		t.Fatalf("migration indexes: legacy=%d active=%d, want 0/1", legacyIndexCount, activeIndexCount)
	}
}

func TestBrowserSessionStoreRejectsIncompatibleVersions(t *testing.T) {
	t.Run("invalid version one schema", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "browser_sessions.sqlite3")
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
		dbPath := filepath.Join(t.TempDir(), "browser_sessions.sqlite3")
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
		dbPath := filepath.Join(t.TempDir(), "browser_sessions.sqlite3")
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
		want os.FileMode
	}{
		{name: "restrict permissive directory", mode: 0o777, want: 0o750},
		{name: "do not widen private directory", mode: 0o700, want: 0o700},
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
			if got := info.Mode().Perm(); got != testCase.want {
				t.Fatalf("directory mode = %o, want %o", got, testCase.want)
			}
		})
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
			Path:   filepath.Join(t.TempDir(), "browser_sessions.sqlite3"),
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
		Path: filepath.Join(t.TempDir(), "browser_sessions.sqlite3"),
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
				Path:   filepath.Join(t.TempDir(), "browser_sessions.sqlite3"),
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
			Path:   filepath.Join(t.TempDir(), "browser_sessions.sqlite3"),
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
