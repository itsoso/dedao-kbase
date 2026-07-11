package main

import (
	"testing"
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
