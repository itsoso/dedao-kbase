package app

import (
	"strings"
	"testing"
)

func TestSourceAgentHeartbeatRuntimeMetadata(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	agent, err := store.HeartbeatAgent(SourceAgentHeartbeat{
		AgentID:          " worker-01 ",
		WorkerType:       " SOURCE-AGENT ",
		Platform:         " Darwin ",
		Architecture:     " ARM64 ",
		Version:          " 2.4.1 ",
		ProtocolVersion:  " 2026-08-01 ",
		Capabilities:     []string{"sync_content"},
		CurrentRunID:     " run_123 ",
		CurrentCommandID: " cmd_456 ",
		OutboxPending:    3,
		DeadLetterCount:  1,
		LastSuccessAt:    "2026-08-01T14:15:16-04:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if agent.AgentID != "worker-01" || agent.WorkerType != "source-agent" || agent.Platform != "darwin" || agent.Architecture != "arm64" {
		t.Fatalf("runtime metadata not normalized: %#v", agent)
	}
	if agent.Version != "2.4.1" || agent.ProtocolVersion != "2026-08-01" {
		t.Fatalf("versions not normalized: %#v", agent)
	}
	if agent.CurrentRunID != "run_123" || agent.CurrentCommandID != "cmd_456" {
		t.Fatalf("runtime ids not normalized: %#v", agent)
	}
	if agent.OutboxPending != 3 || agent.DeadLetterCount != 1 || agent.LastSuccessAt != "2026-08-01T18:15:16Z" {
		t.Fatalf("runtime progress not preserved: %#v", agent)
	}
	if agent.DesiredState != SourceAgentDesiredActive {
		t.Fatalf("desired state=%q", agent.DesiredState)
	}
}

func TestSourceAgentHeartbeatBounds(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tests := []struct {
		name      string
		heartbeat SourceAgentHeartbeat
	}{
		{name: "negative outbox", heartbeat: SourceAgentHeartbeat{AgentID: "worker", OutboxPending: -1}},
		{name: "negative dead letters", heartbeat: SourceAgentHeartbeat{AgentID: "worker", DeadLetterCount: -1}},
		{name: "worker type too long", heartbeat: SourceAgentHeartbeat{AgentID: "worker", WorkerType: strings.Repeat("a", 65)}},
		{name: "invalid platform", heartbeat: SourceAgentHeartbeat{AgentID: "worker", Platform: "darwin arm"}},
		{name: "invalid architecture", heartbeat: SourceAgentHeartbeat{AgentID: "worker", Architecture: "arm64/unsafe"}},
		{name: "version too long", heartbeat: SourceAgentHeartbeat{AgentID: "worker", Version: strings.Repeat("v", 129)}},
		{name: "protocol version too long", heartbeat: SourceAgentHeartbeat{AgentID: "worker", ProtocolVersion: strings.Repeat("p", 129)}},
		{name: "agent id too long", heartbeat: SourceAgentHeartbeat{AgentID: strings.Repeat("a", 129)}},
		{name: "run id too long", heartbeat: SourceAgentHeartbeat{AgentID: "worker", CurrentRunID: strings.Repeat("r", 129)}},
		{name: "command id too long", heartbeat: SourceAgentHeartbeat{AgentID: "worker", CurrentCommandID: strings.Repeat("c", 129)}},
		{name: "invalid success timestamp", heartbeat: SourceAgentHeartbeat{AgentID: "worker", LastSuccessAt: "yesterday"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.HeartbeatAgent(test.heartbeat); err == nil {
				t.Fatalf("accepted heartbeat=%#v", test.heartbeat)
			}
		})
	}
}

func TestSourceAgentCapabilityCode(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, code := range []string{"", "login_required", "vendor_blocked", "dependency_unavailable", "config_invalid", "upgrade_required", "throttled"} {
		agent, err := store.HeartbeatAgent(SourceAgentHeartbeat{
			AgentID: "worker-" + strings.ReplaceAll(code, "_", "-"),
			CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: code == "", Code: code, Version: "1.0.0"},
			},
		})
		if err != nil {
			t.Fatalf("code %q: %v", code, err)
		}
		if got := agent.CapabilityHealth["wechat"].Code; got != code {
			t.Fatalf("code round trip=%q, want %q", got, code)
		}
	}
	if _, err := store.HeartbeatAgent(SourceAgentHeartbeat{
		AgentID: "worker-unknown",
		CapabilityHealth: map[string]SourceCapabilityHealth{
			"wechat": {Healthy: false, Code: "unexpected_failure"},
		},
	}); err == nil {
		t.Fatal("accepted unknown diagnostic code")
	}
}
