package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWCPlusAgentDoctorChecksLocalAndRemoteWithoutLeasing(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected local path: %s", r.URL.Path)
		}
		fmt.Fprint(w, "wcplus")
	}))
	defer local.Close()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/source-agent/lease" {
			t.Fatalf("unexpected remote path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer agent-secret" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var payload struct {
			Capabilities []string `json:"capabilities"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(payload.Capabilities) != 0 {
			t.Fatalf("doctor leased capabilities: %#v", payload.Capabilities)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"run":null}`)
	}))
	defer remote.Close()

	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"doctor"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI doctor: %v, stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ok":true`) || strings.Contains(stdout.String(), "agent-secret") {
		t.Fatalf("doctor output = %s", stdout.String())
	}
}

func TestWCPlusAgentOnceHeartbeatsFlushesAndPolls(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "wcplus")
	}))
	defer local.Close()
	var calls []string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/source-agent/heartbeat":
			calls = append(calls, "heartbeat")
			fmt.Fprint(w, `{"agent":{"agent_id":"agent-a","wcplus_healthy":true}}`)
		case "/api/source-agent/lease":
			calls = append(calls, "lease")
			fmt.Fprint(w, `{"run":null}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer remote.Close()

	env := wcplusAgentTestEnv(remote.URL, local.URL, t.TempDir())
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"once"}, env.Lookup, &stdout, &stderr); err != nil {
		t.Fatalf("runCLI once: %v, stderr=%s", err, stderr.String())
	}
	if strings.Join(calls, ",") != "heartbeat,lease" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestWCPlusAgentRequiresKnownModeAndConfiguration(t *testing.T) {
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), []string{"unknown"}, mapLookup(nil), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "doctor, once, or run") {
		t.Fatalf("unknown mode error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI(context.Background(), []string{"doctor"}, mapLookup(nil), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "KBASE_REMOTE_URL") {
		t.Fatalf("missing config error = %v", err)
	}
}

type testEnv map[string]string

func (e testEnv) Lookup(key string) (string, bool) {
	value, ok := e[key]
	return value, ok
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return testEnv(values).Lookup
}

func wcplusAgentTestEnv(remoteURL, localURL, stateDir string) testEnv {
	return testEnv{
		"KBASE_REMOTE_URL":         remoteURL,
		"KBASE_SOURCE_AGENT_TOKEN": "agent-secret",
		"KBASE_SOURCE_AGENT_ID":    "agent-a",
		"WCPLUS_AGENT_STATE_DIR":   stateDir,
		"WCPLUSPRO_BASE_URL":       localURL,
	}
}
