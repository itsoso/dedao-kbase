package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
)

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
