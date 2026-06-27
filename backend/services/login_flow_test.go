package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withTestLoginServer(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()

	oldBaseURL := baseURL
	oldCsrfToken := CsrfToken
	oldSetCookie := SetCookie

	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		baseURL = oldBaseURL
		CsrfToken = oldCsrfToken
		SetCookie = oldSetCookie
	})

	baseURL = server.URL
	CsrfToken = ""
	SetCookie = nil

	return NewService(&CookieOptions{})
}

func homeInitialStateBody() string {
	return `<script> window.__INITIAL_STATE__= {"isLogin":false,"homeData":{"moduleList":[],"categoryList":[],"banner":[]},"uid":""};</script>`
}

func TestLoginAccessTokenFetchesCSRFBeforeRequest(t *testing.T) {
	service := withTestLoginServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "csrfToken", Value: "fresh-csrf", Path: "/"})
			fmt.Fprint(w, homeInitialStateBody())
		case "/loginapi/getAccessToken":
			if got := r.Header.Get("Xi-Csrf-Token"); got != "fresh-csrf" {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"message":"missing csrf token"}`)
				return
			}
			fmt.Fprint(w, "access-token")
		default:
			http.NotFound(w, r)
		}
	})

	token, err := service.LoginAccessToken()
	if err != nil {
		t.Fatalf("LoginAccessToken returned error: %v", err)
	}
	if token != "access-token" {
		t.Fatalf("LoginAccessToken token = %q, want access-token", token)
	}
}

func TestLoginAccessTokenRefreshesInvalidCSRFAndRetries(t *testing.T) {
	service := withTestLoginServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "csrfToken", Value: "fresh-csrf", Path: "/"})
			fmt.Fprint(w, homeInitialStateBody())
		case "/loginapi/getAccessToken":
			if got := r.Header.Get("Xi-Csrf-Token"); got != "fresh-csrf" {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"message":"invalid csrf token"}`)
				return
			}
			fmt.Fprint(w, "access-token")
		default:
			http.NotFound(w, r)
		}
	})
	CsrfToken = "stale-csrf"

	token, err := service.LoginAccessToken()
	if err != nil {
		t.Fatalf("LoginAccessToken returned error: %v", err)
	}
	if token != "access-token" {
		t.Fatalf("LoginAccessToken token = %q, want access-token", token)
	}
}
