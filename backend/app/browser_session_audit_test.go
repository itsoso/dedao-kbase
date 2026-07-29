package app

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestBrowserSessionAuditPersistsSecurityLifecycle(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            newBrowserSessionTestDBPath(t),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(901, 8)),
		TTL:             30 * 24 * time.Hour,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	first, err := createBrowserSessionForTest(store, BrowserSessionCreate{
		DeviceLabel: "Safari / macOS",
		AuditType:   BrowserSessionAuditLogin,
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	second, err := createBrowserSessionForTest(store, BrowserSessionCreate{
		DeviceLabel: "Chrome / Linux",
		AuditType:   BrowserSessionAuditMigration,
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(5 * time.Minute)
	if auth, err := store.AuthenticateAndRenew(second.Token); err != nil {
		t.Fatal(err)
	} else if !auth.Renewed {
		t.Fatal("AuthenticateAndRenew() did not renew at the audit boundary")
	}
	clock.Advance(time.Minute)
	if _, err := store.FenceClientBySession(second.Session.ID, "logout"); err != nil {
		t.Fatal(err)
	}

	events, err := store.ListAuditEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(events))
	for _, event := range events {
		got = append(got, event.Type+"|"+event.SessionID+"|"+event.DeviceLabel+"|"+event.ReasonCode)
	}
	want := []string{
		BrowserSessionAuditLogin + "|" + first.Session.ID + "|Safari / macOS|login",
		BrowserSessionAuditLimitEviction + "|" + first.Session.ID + "|Safari / macOS|session_limit",
		BrowserSessionAuditMigration + "|" + second.Session.ID + "|Chrome / Linux|migration",
		BrowserSessionAuditRenewal + "|" + second.Session.ID + "|Chrome / Linux|sliding_renewal",
		BrowserSessionAuditLogout + "|" + second.Session.ID + "|Chrome / Linux|logout",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("audit events = %#v, want %#v", got, want)
	}
}

func TestBrowserSessionAuditPersistsAdminRevocationAndRejectedAuthentication(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            newBrowserSessionTestDBPath(t),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(902, 8)),
		TTL:             30 * 24 * time.Hour,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	first, err := createBrowserSessionForTest(store, BrowserSessionCreate{
		DeviceLabel: "Firefox / Windows",
		AuditType:   BrowserSessionAuditLogin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(first.Session.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	second, err := createBrowserSessionForTest(store, BrowserSessionCreate{
		DeviceLabel: "Edge / Windows",
		AuditType:   BrowserSessionAuditLogin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if revoked, err := store.RevokeAll("admin"); err != nil {
		t.Fatal(err)
	} else if revoked != 1 {
		t.Fatalf("RevokeAll() = %d, want 1", revoked)
	}
	if err := store.RecordAuthenticationRejected(
		first.Session.ID,
		first.Session.DeviceLabel,
		"revoked",
	); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAuthenticationRejected("", "", "invalid_credential"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAuthenticationRejected("", "", "raw bearer token"); !errors.Is(err, ErrBrowserSessionInvalidArgument) {
		t.Fatalf("unsafe rejection reason error = %v, want invalid argument", err)
	}

	events, err := store.ListAuditEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(events))
	for _, event := range events {
		got = append(got, event.Type+"|"+event.SessionID+"|"+event.DeviceLabel+"|"+event.ReasonCode)
	}
	want := []string{
		BrowserSessionAuditLogin + "|" + first.Session.ID + "|Firefox / Windows|login",
		BrowserSessionAuditAdminRevocation + "|" + first.Session.ID + "|Firefox / Windows|admin",
		BrowserSessionAuditLogin + "|" + second.Session.ID + "|Edge / Windows|login",
		BrowserSessionAuditAdminRevocation + "|" + second.Session.ID + "|Edge / Windows|admin",
		BrowserSessionAuditAuthenticationRejected + "|" + first.Session.ID + "|Firefox / Windows|revoked",
		BrowserSessionAuditAuthenticationRejected + "|||invalid_credential",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("audit events = %#v, want %#v", got, want)
	}
}

func TestKBaseHTTPBrowserSessionAuditClassifiesMigrationAndRejectedCredentials(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 11, 0, 0, 0, time.UTC),
	}
	handler, store := newKBaseBrowserSessionHTTPTestHandler(t, clock, 903)
	family, err := store.AcquireClientEpoch("browser_client_audit_http_01")
	if err != nil {
		t.Fatal(err)
	}

	migrate := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
	migrate.Header.Set("Origin", testBrowserSessionOrigin)
	migrate.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
	migrate.Header.Set(browserSessionClientIDHeaderName, family.ClientID)
	migrate.Header.Set(browserSessionEpochHeaderName, strconv.FormatInt(family.Epoch, 10))
	migrateResponse := httptest.NewRecorder()
	handler.ServeHTTP(migrateResponse, migrate)
	if migrateResponse.Code != http.StatusOK {
		t.Fatalf("migration status = %d body=%s", migrateResponse.Code, migrateResponse.Body.String())
	}

	rejected := newKBaseBrowserCookieRequest(
		http.MethodGet,
		"/api/books",
		"invalid-browser-session-credential",
		"",
	)
	rejectedResponse := httptest.NewRecorder()
	handler.ServeHTTP(rejectedResponse, rejected)
	if rejectedResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"rejected authentication status = %d body=%s",
			rejectedResponse.Code,
			rejectedResponse.Body.String(),
		)
	}
	rejectedMigration := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
	rejectedMigration.Header.Set("Origin", testBrowserSessionOrigin)
	rejectedMigration.Header.Set("Authorization", "Bearer invalid-migration-credential")
	rejectedMigration.Header.Set(browserSessionClientIDHeaderName, family.ClientID)
	rejectedMigration.Header.Set(
		browserSessionEpochHeaderName,
		strconv.FormatInt(family.Epoch, 10),
	)
	rejectedMigrationResponse := httptest.NewRecorder()
	handler.ServeHTTP(rejectedMigrationResponse, rejectedMigration)
	if rejectedMigrationResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"rejected migration status = %d body=%s",
			rejectedMigrationResponse.Code,
			rejectedMigrationResponse.Body.String(),
		)
	}
	invalidCookie := newKBaseBrowserCookieRequest(
		http.MethodGet,
		"/api/books",
		string(bytes.Repeat([]byte("x"), maxBrowserSessionCookieBytes+1)),
		"",
	)
	invalidCookieResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidCookieResponse, invalidCookie)
	if invalidCookieResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"invalid Cookie status = %d body=%s",
			invalidCookieResponse.Code,
			invalidCookieResponse.Body.String(),
		)
	}
	rejectedProxyLogin := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
	rejectedProxyLogin.Header.Set(browserSessionProxyHeaderName, "invalid-proxy-secret")
	rejectedProxyLoginResponse := httptest.NewRecorder()
	handler.ServeHTTP(rejectedProxyLoginResponse, rejectedProxyLogin)
	if rejectedProxyLoginResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"invalid proxy login status = %d body=%s",
			rejectedProxyLoginResponse.Code,
			rejectedProxyLoginResponse.Body.String(),
		)
	}
	unexpectedAuthorization := httptest.NewRequest(http.MethodGet, "/api/browser/session", nil)
	unexpectedAuthorization.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
	unexpectedAuthorizationResponse := httptest.NewRecorder()
	handler.ServeHTTP(unexpectedAuthorizationResponse, unexpectedAuthorization)
	if unexpectedAuthorizationResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"unexpected browser Authorization status = %d body=%s",
			unexpectedAuthorizationResponse.Code,
			unexpectedAuthorizationResponse.Body.String(),
		)
	}

	events, err := store.ListAuditEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(events))
	for _, event := range events {
		got = append(got, event.Type+"|"+event.SessionID+"|"+event.ReasonCode)
	}
	want := []string{
		BrowserSessionAuditMigration + "|" + events[0].SessionID + "|migration",
		BrowserSessionAuditAuthenticationRejected + "||missing",
		BrowserSessionAuditAuthenticationRejected + "||migration_credential",
		BrowserSessionAuditAuthenticationRejected + "||invalid_cookie",
		BrowserSessionAuditAuthenticationRejected + "||proxy_credential",
		BrowserSessionAuditAuthenticationRejected + "||unexpected_authorization",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HTTP audit events = %#v, want %#v", got, want)
	}
}

func TestBrowserSessionCleanupAppliesRetentionToAuditEvents(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            newBrowserSessionTestDBPath(t),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(904, 4)),
		TTL:             30 * 24 * time.Hour,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	credentials, err := createBrowserSessionForTest(store, BrowserSessionCreate{
		DeviceLabel: "Safari / macOS",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(credentials.Session.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(31 * 24 * time.Hour)
	if _, err := store.Cleanup(30 * 24 * time.Hour); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListAuditEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("Cleanup() retained %d expired audit events, want 0", len(events))
	}
}

func TestBrowserSessionAuditCoalescesRejectedAuthenticationAndBoundsRows(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 13, 0, 0, 0, time.UTC),
	}
	store, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            newBrowserSessionTestDBPath(t),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(905, 8)),
		TTL:             30 * 24 * time.Hour,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       10,
		AuditMaxEvents:  3,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for index := 0; index < 2; index++ {
		if err := store.RecordAuthenticationRejected("", "", "invalid_cookie"); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.ListAuditEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("duplicate rejected-auth events = %d, want 1", len(events))
	}

	clock.Advance(time.Minute)
	if err := store.RecordAuthenticationRejected("", "", "invalid_cookie"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if _, err := createBrowserSessionForTest(store, BrowserSessionCreate{
			DeviceLabel: "Bounded browser " + strconv.Itoa(index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err = store.ListAuditEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("bounded audit events = %d, want 3", len(events))
	}
	if events[0].Type != BrowserSessionAuditLogin ||
		events[0].DeviceLabel != "Bounded browser 0" {
		t.Fatalf("oldest retained audit event = %#v, want first bounded login", events[0])
	}
}
