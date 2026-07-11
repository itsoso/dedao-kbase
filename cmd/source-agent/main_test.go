package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

type fakeSourceAgentCycleRunner struct {
	calls  int
	errors []error
	cancel context.CancelFunc
}

type fakeSourceAgentAuthChecker struct{ err error }

func (c fakeSourceAgentAuthChecker) CheckAuth(context.Context) error { return c.err }

func (r *fakeSourceAgentCycleRunner) RunOnce(context.Context) (app.SourceAgentCycleResult, error) {
	r.calls++
	if r.calls >= 3 && r.cancel != nil {
		r.cancel()
	}
	if r.calls <= len(r.errors) {
		return app.SourceAgentCycleResult{}, r.errors[r.calls-1]
	}
	return app.SourceAgentCycleResult{OK: true}, nil
}

func TestSourceAgentCLIConfigPrefersGenericStateDirectory(t *testing.T) {
	values := map[string]string{"KBASE_REMOTE_URL": "https://kbase.example.invalid", "KBASE_SOURCE_AGENT_TOKEN": "agent-value", "KBASE_SOURCE_AGENT_ID": "agent-a", "SOURCE_AGENT_STATE_DIR": "state"}
	cfg, err := loadSourceAgentConfig(func(key string) (string, bool) { v, ok := values[key]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != "state" {
		t.Fatalf("state=%q", cfg.StateDir)
	}
}

func TestSourceAgentEnrollmentAddressIsLoopbackOnly(t *testing.T) {
	for _, value := range []string{"127.0.0.1:8765", "localhost:9000"} {
		if _, err := normalizeEnrollmentAddress(value); err != nil {
			t.Fatalf("%s: %v", value, err)
		}
	}
	if _, err := normalizeEnrollmentAddress("0.0.0.0:8765"); err == nil {
		t.Fatal("accepted wildcard enrollment address")
	}
}

func TestStoredSessionProviderRejectsExpiredSession(t *testing.T) {
	store := app.NewMemorySourceSecretStore()
	raw, err := json.Marshal(app.WeChatMPSession{Token: "expired", ObservedExpiry: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "wechat-mp-session", raw); err != nil {
		t.Fatal(err)
	}
	_, err = (storedSessionProvider{store: store}).Session(context.Background())
	if !errors.Is(err, app.ErrWeChatMPSessionExpired) {
		t.Fatalf("Session() error=%v", err)
	}
}

func TestSourceAgentRunLoopContinuesAfterCycleFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeSourceAgentCycleRunner{errors: []error{errors.New("temporary-1"), errors.New("temporary-2")}, cancel: cancel}
	var reported []string
	if err := runSourceAgentLoop(ctx, runner, time.Millisecond, func(err error) {
		reported = append(reported, err.Error())
	}); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 3 || len(reported) != 2 {
		t.Fatalf("calls=%d reported=%v", runner.calls, reported)
	}
}

func TestSourceAgentRuntimeStopsWhenEnrollmentFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &fakeSourceAgentCycleRunner{}
	err := runSourceAgentRuntime(ctx, runner, time.Hour, func(context.Context) error {
		return errors.New("enrollment bind failed")
	}, nil)
	if err == nil || err.Error() != "enrollment bind failed" {
		t.Fatalf("runtime error=%v", err)
	}
}

func TestSourceAgentDoctorReportsLoginRequiredWithoutFailingTransport(t *testing.T) {
	report, err := inspectSourceAgent(context.Background(), fakeSourceAgentAuthChecker{}, fakeSessionHealthProviderForCLI{err: app.ErrSourceSecretNotFound})
	if err != nil {
		t.Fatal(err)
	}
	if !report.RemoteAuth || report.WeChatSession != "login_required" {
		t.Fatalf("report=%#v", report)
	}
}

type fakeSessionHealthProviderForCLI struct {
	session app.WeChatMPSession
	err     error
}

func (p fakeSessionHealthProviderForCLI) Session(context.Context) (app.WeChatMPSession, error) {
	return p.session, p.err
}
