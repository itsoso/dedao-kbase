package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxAdminResponseBytes = 64 << 10
	maxAdminTokenBytes    = 128
	adminRequestTimeout   = 15 * time.Second
	defaultAdminBaseURL   = "https://kbase.executor.life"
)

var adminTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)

type envLookup func(string) (string, bool)

func main() {
	if err := runCLI(os.Args[1:], os.Stdout, os.Stderr, os.LookupEnv, http.DefaultClient); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(
	args []string,
	stdout, stderr io.Writer,
	getenv envLookup,
	baseClient *http.Client,
) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if getenv == nil {
		getenv = os.LookupEnv
	}

	flags := flag.NewFlagSet("kbase-session-admin", flag.ContinueOnError)
	flags.SetOutput(stderr)
	defaultBaseURL, _ := getenv("KBASE_SESSION_ADMIN_URL")
	if strings.TrimSpace(defaultBaseURL) == "" {
		defaultBaseURL = defaultAdminBaseURL
	}
	baseURL := flags.String(
		"base-url",
		strings.TrimSpace(defaultBaseURL),
		"KBase origin (or KBASE_SESSION_ADMIN_URL); HTTPS required except loopback HTTP",
	)
	tokenFile := flags.String(
		"token-file",
		"",
		"protected token file (or KBASE_SESSION_ADMIN_TOKEN_FILE)",
	)
	flags.Usage = func() {
		fmt.Fprintln(stdout, "Usage: kbase-session-admin [--base-url URL] [--token-file FILE] <command>")
		fmt.Fprintln(stdout, "Commands: list | revoke <session_id> | revoke-all --confirm")
		fmt.Fprintf(stdout, "Base URL: --base-url or KBASE_SESSION_ADMIN_URL (default %s)\n", defaultAdminBaseURL)
		fmt.Fprintln(stdout, "Token: KBASE_SESSION_ADMIN_TOKEN or a protected file from --token-file / KBASE_SESSION_ADMIN_TOKEN_FILE")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.Usage()
			return nil
		}
		return errors.New("invalid arguments")
	}

	method, path, err := parseCommand(flags.Args())
	if err != nil {
		return err
	}
	origin, err := validateBaseURL(*baseURL)
	if err != nil {
		return err
	}
	token, err := loadAdminToken(getenv, *tokenFile)
	if err != nil {
		return err
	}

	request, err := http.NewRequest(method, origin+path, nil)
	if err != nil {
		return errors.New("build admin request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")

	client := boundedAdminClient(baseClient)
	response, err := client.Do(request)
	if err != nil {
		return errors.New("admin request failed")
	}
	defer response.Body.Close()
	body, err := readBoundedResponse(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("admin API returned HTTP %d", response.StatusCode)
	}
	return writeAdminSuccess(stdout, method, path, response.StatusCode, body)
}

func parseCommand(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", errors.New("command is required")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return "", "", errors.New("list accepts no arguments")
		}
		return http.MethodGet, "/api/admin/browser-sessions", nil
	case "revoke":
		if len(args) != 2 || !regexp.MustCompile(`^session_[A-Za-z0-9_-]{1,128}$`).MatchString(args[1]) {
			return "", "", errors.New("revoke requires one valid session id")
		}
		return http.MethodDelete, "/api/admin/browser-sessions/" + url.PathEscape(args[1]), nil
	case "revoke-all":
		if len(args) != 2 || args[1] != "--confirm" {
			return "", "", errors.New("revoke-all requires --confirm")
		}
		return http.MethodPost, "/api/admin/browser-sessions/revoke-all", nil
	default:
		return "", "", errors.New("unknown command")
	}
}

func validateBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("invalid base URL")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("base URL must be an origin")
	}
	host := parsed.Hostname()
	loopback := strings.EqualFold(host, "localhost")
	if ip := net.ParseIP(host); ip != nil {
		loopback = ip.IsLoopback()
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return "", errors.New("HTTPS is required except for loopback HTTP")
	}
	return strings.TrimSuffix(parsed.Scheme+"://"+parsed.Host, "/"), nil
}

func loadAdminToken(getenv envLookup, optionFile string) (string, error) {
	envToken, hasEnvToken := getenv("KBASE_SESSION_ADMIN_TOKEN")
	envFile, hasEnvFile := getenv("KBASE_SESSION_ADMIN_TOKEN_FILE")
	optionFile = strings.TrimSpace(optionFile)
	envFile = strings.TrimSpace(envFile)
	hasEnvToken = hasEnvToken && envToken != ""
	hasEnvFile = hasEnvFile && envFile != ""
	if hasEnvToken && (hasEnvFile || optionFile != "") {
		return "", errors.New("admin token sources are ambiguous")
	}
	if hasEnvFile && optionFile != "" {
		return "", errors.New("admin token file sources are ambiguous")
	}

	var token string
	switch {
	case hasEnvToken:
		token = envToken
	case optionFile != "":
		var err error
		token, err = readProtectedTokenFile(optionFile)
		if err != nil {
			return "", err
		}
	case hasEnvFile:
		var err error
		token, err = readProtectedTokenFile(envFile)
		if err != nil {
			return "", err
		}
	default:
		return "", errors.New("admin token is not configured")
	}
	if len(token) > maxAdminTokenBytes || !adminTokenPattern.MatchString(token) {
		return "", errors.New("admin token must contain 32-128 URL-safe ASCII characters")
	}
	return token, nil
}

func readProtectedTokenFile(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	info, err := os.Lstat(path)
	if err != nil {
		return "", errors.New("read admin token file")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("admin token file must be a protected regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("read admin token file")
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxAdminTokenBytes+2))
	if err != nil || len(body) > maxAdminTokenBytes+1 {
		return "", errors.New("read admin token file")
	}
	body = bytesTrimSingleLineEnding(body)
	return string(body), nil
}

func boundedAdminClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	timeout := base.Timeout
	if timeout <= 0 || timeout > adminRequestTimeout {
		timeout = adminRequestTimeout
	}
	return &http.Client{
		Transport: base.Transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func readBoundedResponse(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxAdminResponseBytes+1))
	if err != nil {
		return nil, errors.New("read admin response")
	}
	if len(body) > maxAdminResponseBytes {
		return nil, errors.New("admin response exceeds size limit")
	}
	return body, nil
}

type adminSessionView struct {
	ID           string  `json:"id"`
	DeviceLabel  string  `json:"device_label"`
	CreatedAt    string  `json:"created_at"`
	LastActiveAt string  `json:"last_active_at"`
	ExpiresAt    string  `json:"expires_at"`
	RevokedAt    *string `json:"revoked_at,omitempty"`
	RevokeReason string  `json:"revoke_reason,omitempty"`
}

func writeAdminSuccess(
	stdout io.Writer,
	method, path string,
	status int,
	body []byte,
) error {
	switch {
	case method == http.MethodGet && path == "/api/admin/browser-sessions":
		if status != http.StatusOK {
			return fmt.Errorf("admin API returned unexpected HTTP %d", status)
		}
		var payload struct {
			Sessions []adminSessionView `json:"sessions"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Sessions == nil {
			return errors.New("admin API returned an invalid session list")
		}
		return json.NewEncoder(stdout).Encode(payload)
	case method == http.MethodPost && path == "/api/admin/browser-sessions/revoke-all":
		if status != http.StatusOK {
			return fmt.Errorf("admin API returned unexpected HTTP %d", status)
		}
		var payload struct {
			RevokedCount *int64 `json:"revoked_count"`
		}
		if err := json.Unmarshal(body, &payload); err != nil ||
			payload.RevokedCount == nil ||
			*payload.RevokedCount < 0 {
			return errors.New("admin API returned an invalid revoke count")
		}
		return json.NewEncoder(stdout).Encode(payload)
	case method == http.MethodDelete:
		if status != http.StatusNoContent || len(strings.TrimSpace(string(body))) != 0 {
			return errors.New("admin API returned an invalid revoke response")
		}
		_, err := fmt.Fprintln(stdout, "ok")
		return err
	default:
		return errors.New("unsupported admin response")
	}
}

func bytesTrimSingleLineEnding(body []byte) []byte {
	if len(body) > 0 && body[len(body)-1] == '\n' {
		body = body[:len(body)-1]
		if len(body) > 0 && body[len(body)-1] == '\r' {
			body = body[:len(body)-1]
		}
	}
	return body
}
