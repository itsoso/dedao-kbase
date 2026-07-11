package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeEnrollmentLogin struct{ started, loggedOut bool }

func (f *fakeEnrollmentLogin) StartLogin(_ context.Context) error { f.started = true; return nil }
func (f *fakeEnrollmentLogin) QRImage(_ context.Context) ([]byte, string, error) {
	return []byte("image"), "image/png", nil
}
func (f *fakeEnrollmentLogin) LoginStatus(_ context.Context) (any, error) {
	return map[string]any{"state": "pending"}, nil
}
func (f *fakeEnrollmentLogin) Logout(_ context.Context) error { f.loggedOut = true; return nil }

func TestEnrollmentRequiresLoopbackOriginAndCSRF(t *testing.T) {
	login := &fakeEnrollmentLogin{}
	handler, err := newEnrollmentHandler(login, "csrf-value")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/local/wechat/login/start", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden || login.started {
		t.Fatalf("missing csrf status=%d started=%v", resp.Code, login.started)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/local/wechat/login/start", nil)
	req.Header.Set("Origin", "https://example.invalid")
	req.Header.Set("X-CSRF-Token", "csrf-value")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("remote origin status=%d", resp.Code)
	}
}
