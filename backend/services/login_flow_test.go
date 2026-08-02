package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func withTestLoginServer(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()

	oldBaseURL := baseURL

	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		baseURL = oldBaseURL
	})

	baseURL = server.URL

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
	service.csrfToken = "stale-csrf"

	token, err := service.LoginAccessToken()
	if err != nil {
		t.Fatalf("LoginAccessToken returned error: %v", err)
	}
	if token != "access-token" {
		t.Fatalf("LoginAccessToken token = %q, want access-token", token)
	}
}

func TestConcurrentLoginServicesKeepBootstrapStateIsolated(t *testing.T) {
	oldBaseURL := baseURL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flow := r.Header.Get("X-Test-Login-Flow")
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "csrfToken", Value: "csrf-" + flow, Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "bootstrap", Value: "cookie-" + flow, Path: "/"})
			fmt.Fprint(w, homeInitialStateBody())
		case "/loginapi/getAccessToken":
			if got := r.Header.Get("Xi-Csrf-Token"); got != "csrf-"+flow {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, "wrong csrf %s for %s", got, flow)
				return
			}
			fmt.Fprint(w, "token-"+flow)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		server.Close()
		baseURL = oldBaseURL
	})
	baseURL = server.URL

	services := []*Service{NewService(&CookieOptions{}), NewService(&CookieOptions{})}
	flows := []string{"one", "two"}
	var wg sync.WaitGroup
	for index, service := range services {
		index, service := index, service
		service.client.SetHeader("X-Test-Login-Flow", flows[index])
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := service.LoginAccessToken()
			if err != nil {
				t.Errorf("flow %s token error: %v", flows[index], err)
				return
			}
			if token != "token-"+flows[index] {
				t.Errorf("flow %s token = %q", flows[index], token)
			}
			cookies := strings.Join(service.BootstrapCookies(), "; ")
			if !strings.Contains(cookies, "csrf-"+flows[index]) || !strings.Contains(cookies, "cookie-"+flows[index]) {
				t.Errorf("flow %s bootstrap cookies = %q", flows[index], cookies)
			}
		}()
	}
	wg.Wait()
}
