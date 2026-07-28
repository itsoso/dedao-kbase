package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testAdminToken = "session-admin-token-0123456789abcdef"

func TestSessionAdminCLIRequestContracts(t *testing.T) {
	for _, test := range []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		response   string
	}{
		{
			name:       "list",
			args:       []string{"list"},
			wantMethod: http.MethodGet,
			wantPath:   "/api/admin/browser-sessions",
			response:   `{"sessions":[]}`,
		},
		{
			name:       "revoke",
			args:       []string{"revoke", "session_123"},
			wantMethod: http.MethodDelete,
			wantPath:   "/api/admin/browser-sessions/session_123",
		},
		{
			name:       "revoke all",
			args:       []string{"revoke-all", "--confirm"},
			wantMethod: http.MethodPost,
			wantPath:   "/api/admin/browser-sessions/revoke-all",
			response:   `{"revoked_count":2}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var gotAuthorization []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method != test.wantMethod || request.URL.EscapedPath() != test.wantPath {
					t.Fatalf("request = %s %s, want %s %s",
						request.Method, request.URL.EscapedPath(), test.wantMethod, test.wantPath)
				}
				gotAuthorization = request.Header.Values("Authorization")
				w.Header().Set("Content-Type", "application/json")
				if test.response == "" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				fmt.Fprintln(w, test.response)
			}))
			defer server.Close()

			var stdout, stderr bytes.Buffer
			err := runCLI(
				append([]string{"--base-url", server.URL}, test.args...),
				&stdout,
				&stderr,
				envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN": testAdminToken}),
				server.Client(),
			)
			if err != nil {
				t.Fatalf("runCLI error = %v, stderr=%q", err, stderr.String())
			}
			if len(gotAuthorization) != 1 ||
				gotAuthorization[0] != "Bearer "+testAdminToken {
				t.Fatalf("Authorization = %#v", gotAuthorization)
			}
			if strings.Contains(stdout.String(), testAdminToken) ||
				strings.Contains(stderr.String(), testAdminToken) {
				t.Fatal("token leaked to CLI output")
			}
		})
	}
}

func TestSessionAdminCLIDecodesOnlyWhitelistedSuccessFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/admin/browser-sessions":
			fmt.Fprintln(w, `{"sessions":[{"id":"session_public","device_label":"Safari","created_at":"2026-07-28T12:00:00Z","last_active_at":"2026-07-28T12:01:00Z","expires_at":"2026-08-27T12:01:00Z","token_hash":"must-not-print","user_agent":"must-not-print"}],"unexpected":"must-not-print"}`)
		case "/api/admin/browser-sessions/revoke-all":
			fmt.Fprintln(w, `{"revoked_count":2,"unexpected":"must-not-print"}`)
		default:
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"--base-url", server.URL, "list"},
		{"--base-url", server.URL, "revoke-all", "--confirm"},
	} {
		var stdout bytes.Buffer
		err := runCLI(
			args,
			&stdout,
			io.Discard,
			envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN": testAdminToken}),
			server.Client(),
		)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		for _, forbidden := range []string{"must-not-print", "token_hash", "user_agent", "unexpected"} {
			if strings.Contains(stdout.String(), forbidden) {
				t.Fatalf("%v output exposed %q: %s", args, forbidden, stdout.String())
			}
		}
	}
}

func TestSessionAdminCLIRejectsMalformedSuccessfulPayloads(t *testing.T) {
	for _, response := range []string{
		`{"sessions":"invalid"}`,
		`{"revoked_count":"invalid"}`,
		`not-json`,
	} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintln(w, response)
			}))
			defer server.Close()
			command := "list"
			if strings.Contains(response, "revoked_count") {
				command = "revoke-all"
			}
			args := []string{"--base-url", server.URL, command}
			if command == "revoke-all" {
				args = append(args, "--confirm")
			}
			err := runCLI(
				args,
				io.Discard,
				io.Discard,
				envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN": testAdminToken}),
				server.Client(),
			)
			if err == nil {
				t.Fatal("malformed successful response was accepted")
			}
		})
	}
	if err := writeAdminSuccess(
		io.Discard,
		http.MethodPost,
		"/api/admin/browser-sessions/revoke-all",
		http.StatusOK,
		[]byte(`{}`),
	); err == nil {
		t.Fatal("revoke-all response without revoked_count was accepted")
	}
}

type adminRoundTripFunc func(*http.Request) (*http.Response, error)

func (f adminRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSessionAdminCLIUsesDocumentedDefaultBaseURL(t *testing.T) {
	var requestedURL string
	client := &http.Client{Transport: adminRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestedURL = request.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"sessions":[]}`)),
			Request:    request,
		}, nil
	})}
	err := runCLI(
		[]string{"list"},
		io.Discard,
		io.Discard,
		envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN": testAdminToken}),
		client,
	)
	if err != nil {
		t.Fatalf("default URL request failed: %v", err)
	}
	want := defaultAdminBaseURL + "/api/admin/browser-sessions"
	if requestedURL != want {
		t.Fatalf("requested URL = %q, want %q", requestedURL, want)
	}
}

func TestSessionAdminCLIRejectsUnsafeOrAmbiguousSecrets(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "admin-token")
	if err := os.WriteFile(tokenFile, []byte(testAdminToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
		env  map[string]string
	}{
		{
			name: "positional token",
			args: []string{"list", testAdminToken},
		},
		{
			name: "token flag",
			args: []string{"--token", testAdminToken, "list"},
		},
		{
			name: "ambiguous sources",
			args: []string{"--token-file", tokenFile, "list"},
			env:  map[string]string{"KBASE_SESSION_ADMIN_TOKEN": testAdminToken},
		},
		{
			name: "missing source",
			args: []string{"list"},
		},
		{
			name: "invalid token",
			args: []string{"list"},
			env:  map[string]string{"KBASE_SESSION_ADMIN_TOKEN": "not valid"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runCLI(test.args, &stdout, &stderr, envValues(test.env), http.DefaultClient)
			if err == nil {
				t.Fatal("runCLI unexpectedly succeeded")
			}
			if strings.Contains(stdout.String(), testAdminToken) ||
				strings.Contains(stderr.String(), testAdminToken) ||
				strings.Contains(err.Error(), testAdminToken) {
				t.Fatal("token leaked in CLI error or output")
			}
		})
	}
}

func TestSessionAdminCLITokenFilePermissionsAndEnvironmentSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+testAdminToken {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		fmt.Fprintln(w, `{"sessions":[]}`)
	}))
	defer server.Close()

	for _, mode := range []os.FileMode{0o640, 0o604, 0o666} {
		t.Run(mode.String(), func(t *testing.T) {
			tokenFile := filepath.Join(t.TempDir(), "admin-token")
			if err := os.WriteFile(tokenFile, []byte(testAdminToken), mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(tokenFile, mode); err != nil {
				t.Fatal(err)
			}
			err := runCLI(
				[]string{"--base-url", server.URL, "--token-file", tokenFile, "list"},
				io.Discard,
				io.Discard,
				envValues(nil),
				server.Client(),
			)
			if err == nil {
				t.Fatalf("mode %o accepted", mode)
			}
		})
	}

	tokenFile := filepath.Join(t.TempDir(), "admin-token")
	if err := os.WriteFile(tokenFile, []byte(testAdminToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runCLI(
		[]string{"--base-url", server.URL, "list"},
		io.Discard,
		io.Discard,
		envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN_FILE": tokenFile}),
		server.Client(),
	)
	if err != nil {
		t.Fatalf("protected token file failed: %v", err)
	}
}

func TestSessionAdminCLITokenSourcesRejectWhitespaceAndSymlinks(t *testing.T) {
	if _, err := loadAdminToken(
		envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN": " " + testAdminToken}),
		"",
	); err == nil {
		t.Fatal("environment token with leading whitespace was accepted")
	}

	tokenFile := filepath.Join(t.TempDir(), "admin-token")
	if err := os.WriteFile(tokenFile, []byte(" "+testAdminToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAdminToken(envValues(nil), tokenFile); err == nil {
		t.Fatal("token file with leading whitespace was accepted")
	}

	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte(testAdminToken), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAdminToken(envValues(nil), link); err == nil {
		t.Fatal("symlink token file was accepted")
	}
}

func TestReadProtectedTokenFileRejectsPathReplacementAfterOpen(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "admin-token")
	openedPath := filepath.Join(dir, "opened-token")
	if err := os.WriteFile(tokenFile, []byte(testAdminToken), 0o600); err != nil {
		t.Fatal(err)
	}

	openAndReplace := func(path string) (*os.File, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		if err := os.Rename(path, openedPath); err != nil {
			file.Close()
			return nil, err
		}
		if err := os.WriteFile(path, []byte(strings.Repeat("x", 32)), 0o600); err != nil {
			file.Close()
			return nil, err
		}
		return file, nil
	}

	_, err := readProtectedTokenFileWithOps(tokenFile, openAndReplace, os.Lstat)
	if err == nil {
		t.Fatal("replaced token path was accepted")
	}
	if strings.Contains(err.Error(), testAdminToken) {
		t.Fatal("token leaked in path replacement error")
	}
}

func TestReadProtectedTokenFileRejectsPermissiveOpenedFile(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "admin-token")
	permissiveFile := filepath.Join(dir, "permissive-token")
	if err := os.WriteFile(tokenFile, []byte(testAdminToken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(permissiveFile, []byte(testAdminToken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(permissiveFile, 0o644); err != nil {
		t.Fatal(err)
	}

	openPermissive := func(string) (*os.File, error) {
		return os.Open(permissiveFile)
	}
	lstatOpened := func(string) (os.FileInfo, error) {
		return os.Lstat(permissiveFile)
	}
	_, err := readProtectedTokenFileWithOps(tokenFile, openPermissive, lstatOpened)
	if err == nil {
		t.Fatal("permissive opened token file was accepted")
	}
	if strings.Contains(err.Error(), testAdminToken) {
		t.Fatal("token leaked in permissive file error")
	}
}

func TestSessionAdminCLIBaseURLRejectsEmptyQueryMarker(t *testing.T) {
	if _, err := validateBaseURL("https://example.com?"); err == nil {
		t.Fatal("base URL with empty query marker was accepted")
	}
}

func TestSessionAdminCLIClientUsesBoundedTimeout(t *testing.T) {
	if got := boundedAdminClient(&http.Client{}).Timeout; got != adminRequestTimeout {
		t.Fatalf("default timeout = %s, want %s", got, adminRequestTimeout)
	}
	short := 250 * time.Millisecond
	if got := boundedAdminClient(&http.Client{Timeout: short}).Timeout; got != short {
		t.Fatalf("short timeout = %s, want %s", got, short)
	}
}

func TestSessionAdminCLIRequiresConfirmationAndSecureBaseURL(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "revoke all confirmation", args: []string{"revoke-all"}},
		{name: "public HTTP", args: []string{"--base-url", "http://example.com", "list"}},
		{name: "URL credentials", args: []string{"--base-url", "https://user@example.com", "list"}},
		{name: "URL path", args: []string{"--base-url", "https://example.com/admin", "list"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := runCLI(
				test.args,
				io.Discard,
				io.Discard,
				envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN": testAdminToken}),
				http.DefaultClient,
			)
			if err == nil {
				t.Fatal("runCLI unexpectedly succeeded")
			}
		})
	}
}

func TestSessionAdminCLIDoesNotFollowRedirectsAndBoundsResponses(t *testing.T) {
	var redirected bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected = true
	}))
	defer destination.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	err := runCLI(
		[]string{"--base-url", redirector.URL, "list"},
		io.Discard,
		io.Discard,
		envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN": testAdminToken}),
		redirector.Client(),
	)
	if err == nil || redirected {
		t.Fatalf("redirect error=%v followed=%v", err, redirected)
	}

	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxAdminResponseBytes+1))
	}))
	defer large.Close()
	err = runCLI(
		[]string{"--base-url", large.URL, "list"},
		io.Discard,
		io.Discard,
		envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN": testAdminToken}),
		large.Client(),
	)
	if err == nil || len(err.Error()) > 1024 {
		t.Fatalf("bounded response error length=%d error=%v", len(err.Error()), err)
	}
}

func TestSessionAdminCLIHelpDocumentsConfigurationWithoutSecrets(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"--help"}, &stdout, &stderr, envValues(nil), http.DefaultClient)
	if err != nil {
		t.Fatalf("help error = %v", err)
	}
	help := stdout.String() + stderr.String()
	for _, marker := range []string{
		"KBASE_SESSION_ADMIN_URL",
		"KBASE_SESSION_ADMIN_TOKEN",
		"KBASE_SESSION_ADMIN_TOKEN_FILE",
		defaultAdminBaseURL,
		"--base-url",
		"--token-file",
	} {
		if !strings.Contains(help, marker) {
			t.Fatalf("help missing %q: %s", marker, help)
		}
	}
	if strings.Contains(help, testAdminToken) {
		t.Fatal("help leaked token")
	}
}

func TestSessionAdminCLINonSuccessIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer server.Close()
	err := runCLI(
		[]string{"--base-url", server.URL, "list"},
		io.Discard,
		io.Discard,
		envValues(map[string]string{"KBASE_SESSION_ADMIN_TOKEN": testAdminToken}),
		server.Client(),
	)
	if err == nil {
		t.Fatal("non-2xx response unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), testAdminToken) {
		t.Fatal("non-2xx error leaked token")
	}
}

func envValues(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
