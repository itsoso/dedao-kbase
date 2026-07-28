package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if got := parentInfo.Mode().Perm(); got != 0o750 {
		t.Fatalf("session database parent mode = %o, want 750", got)
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
	revokedAtColumn := columnDetails["revoked_at"]
	if revokedAtColumn.dataType != "TEXT" ||
		!revokedAtColumn.notNull ||
		revokedAtColumn.defaultValue != "''" {
		t.Fatalf("revoked_at schema = %#v, want TEXT NOT NULL DEFAULT ''", revokedAtColumn)
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
