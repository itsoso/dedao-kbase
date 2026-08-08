package app

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSourceAgentGetNormalizesAndMapsNotFound(t *testing.T) {
	store, _ := newSourceAgentCommandTestStore(t)
	registerSourceAgentCommandTestAgent(t, store, "agent-detail", "1.0.0")

	agent, err := store.GetSourceAgent(" agent-detail ")
	if err != nil || agent.AgentID != "agent-detail" {
		t.Fatalf("GetSourceAgent() = %#v, %v", agent, err)
	}
	if _, err := store.GetSourceAgent("missing-agent"); !errors.Is(err, ErrSourceAgentNotFound) {
		t.Fatalf("unknown GetSourceAgent() error = %v", err)
	}
	if _, err := store.GetSourceAgent("../agent"); err == nil {
		t.Fatal("GetSourceAgent accepted invalid agent ID")
	}
}

func TestSourceAgentObservedStateTruthTable(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	freshness := 5 * time.Minute
	freshHeartbeat := now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	staleHeartbeat := now.Add(-10 * time.Minute).Format(time.RFC3339Nano)

	tests := []struct {
		name          string
		agent         SourceAgent
		freshness     time.Duration
		upgradeActive bool
		want          string
	}{
		{
			name: "upgrade overrides stale heartbeat and capability action",
			agent: SourceAgent{
				LastHeartbeatAt: staleHeartbeat,
				CapabilityHealth: map[string]SourceCapabilityHealth{
					"wechat": {Healthy: false, Code: "login_required"},
				},
			},
			freshness:     freshness,
			upgradeActive: true,
			want:          SourceAgentObservedUpgrading,
		},
		{name: "missing heartbeat is offline", agent: SourceAgent{}, freshness: freshness, want: SourceAgentObservedOffline},
		{name: "invalid heartbeat is offline", agent: SourceAgent{LastHeartbeatAt: "not-a-timestamp"}, freshness: freshness, want: SourceAgentObservedOffline},
		{name: "future heartbeat is offline", agent: SourceAgent{LastHeartbeatAt: now.Add(time.Hour).Format(time.RFC3339Nano)}, freshness: freshness, want: SourceAgentObservedOffline},
		{name: "heartbeat at freshness boundary is online", agent: SourceAgent{LastHeartbeatAt: now.Add(-freshness).Format(time.RFC3339Nano)}, freshness: freshness, want: SourceAgentObservedOnline},
		{
			name: "stale heartbeat overrides capability action",
			agent: SourceAgent{
				LastHeartbeatAt: staleHeartbeat,
				CapabilityHealth: map[string]SourceCapabilityHealth{
					"wechat": {Healthy: false, RequiresAction: "log in again"},
				},
			},
			freshness: freshness,
			want:      SourceAgentObservedOffline,
		},
		{name: "zero freshness is offline", agent: SourceAgent{LastHeartbeatAt: freshHeartbeat}, freshness: 0, want: SourceAgentObservedOffline},
		{name: "negative freshness is offline", agent: SourceAgent{LastHeartbeatAt: freshHeartbeat}, freshness: -time.Minute, want: SourceAgentObservedOffline},
		{
			name: "requires action text",
			agent: SourceAgent{LastHeartbeatAt: freshHeartbeat, CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: true, RequiresAction: "refresh credentials"},
			}},
			freshness: freshness,
			want:      SourceAgentObservedRequiresAction,
		},
		{
			name: "requires action code overrides ordinary unhealthy capability",
			agent: SourceAgent{LastHeartbeatAt: freshHeartbeat, CapabilityHealth: map[string]SourceCapabilityHealth{
				"dependency": {Healthy: false, Code: "dependency_unavailable"},
				"wechat":     {Healthy: false, Code: "login_required"},
			}},
			freshness: freshness,
			want:      SourceAgentObservedRequiresAction,
		},
		{
			name: "vendor blocked requires action",
			agent: SourceAgent{LastHeartbeatAt: freshHeartbeat, CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: false, Code: "vendor_blocked"},
			}},
			freshness: freshness,
			want:      SourceAgentObservedRequiresAction,
		},
		{
			name: "invalid config requires action",
			agent: SourceAgent{LastHeartbeatAt: freshHeartbeat, CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: false, Code: "config_invalid"},
			}},
			freshness: freshness,
			want:      SourceAgentObservedRequiresAction,
		},
		{
			name: "upgrade required requires action",
			agent: SourceAgent{LastHeartbeatAt: freshHeartbeat, CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: false, Code: "upgrade_required"},
			}},
			freshness: freshness,
			want:      SourceAgentObservedRequiresAction,
		},
		{
			name: "dependency unavailable is degraded",
			agent: SourceAgent{LastHeartbeatAt: freshHeartbeat, CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: false, Code: "dependency_unavailable"},
			}},
			freshness: freshness,
			want:      SourceAgentObservedDegraded,
		},
		{
			name: "throttled is degraded",
			agent: SourceAgent{LastHeartbeatAt: freshHeartbeat, CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: false, Code: "throttled"},
			}},
			freshness: freshness,
			want:      SourceAgentObservedDegraded,
		},
		{
			name: "unhealthy without code is degraded",
			agent: SourceAgent{LastHeartbeatAt: freshHeartbeat, CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: false},
			}},
			freshness: freshness,
			want:      SourceAgentObservedDegraded,
		},
		{
			name: "healthy capabilities are online",
			agent: SourceAgent{LastHeartbeatAt: freshHeartbeat, CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: true},
			}},
			freshness: freshness,
			want:      SourceAgentObservedOnline,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DeriveSourceAgentObservedState(test.agent, now, test.freshness, test.upgradeActive); got != test.want {
				t.Fatalf("observed state=%q, want %q", got, test.want)
			}
		})
	}
}

func TestSourceAgentObservedStateIsIndependentFromDesiredState(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	store, err := newSourceSyncStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	heartbeat := SourceAgentHeartbeat{
		AgentID:    "agent-paused-online",
		WorkerType: "source-agent",
		CapabilityHealth: map[string]SourceCapabilityHealth{
			"wechat": {Healthy: true},
		},
	}
	if _, err := store.HeartbeatAgent(heartbeat); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetAgentDesiredState(heartbeat.AgentID, SourceAgentDesiredPaused); err != nil {
		t.Fatal(err)
	}
	agent, err := store.HeartbeatAgent(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if agent.DesiredState != SourceAgentDesiredPaused {
		t.Fatalf("heartbeat changed desired state to %q", agent.DesiredState)
	}

	if got := DeriveSourceAgentObservedState(agent, now, 5*time.Minute, false); got != SourceAgentObservedOnline {
		t.Fatalf("paused agent observed state=%q, want %q", got, SourceAgentObservedOnline)
	}
	if agent.DesiredState != SourceAgentDesiredPaused {
		t.Fatalf("derive changed desired state to %q", agent.DesiredState)
	}
}

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

func TestSourceAgentWCPlusAuthority(t *testing.T) {
	store, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	t.Run("modern typed heartbeat does not invent wcplus", func(t *testing.T) {
		agent, err := store.HeartbeatAgent(SourceAgentHeartbeat{
			AgentID:      "wechat-worker-typed",
			WorkerType:   "wechat-worker",
			Capabilities: []string{"wechat"},
			CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: true, Version: "1.0.0"},
			},
			WCPlusHealthy: true,
			WCPlusVersion: "legacy-conflict",
			LastError:     "legacy conflict",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := agent.CapabilityHealth["wcplus"]; exists {
			t.Fatalf("modern heartbeat invented wcplus: %#v", agent)
		}
		if agent.WCPlusHealthy || agent.WCPlusVersion != "" || agent.LastError != "" {
			t.Fatalf("legacy fields conflict with typed capabilities: %#v", agent)
		}
	})

	t.Run("modern empty health does not invent wcplus", func(t *testing.T) {
		agent, err := store.HeartbeatAgent(SourceAgentHeartbeat{
			AgentID:      "wechat-worker-empty",
			WorkerType:   "wechat-worker",
			Capabilities: []string{"wechat"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := agent.CapabilityHealth["wcplus"]; exists {
			t.Fatalf("modern empty heartbeat invented wcplus: %#v", agent)
		}
	})

	t.Run("typed wcplus mirrors into legacy fields", func(t *testing.T) {
		input := SourceAgentHeartbeat{
			AgentID:      "wcplus-worker",
			WorkerType:   "source-worker",
			Capabilities: []string{"wcplus"},
			CapabilityHealth: map[string]SourceCapabilityHealth{
				"wcplus": {
					Healthy:   false,
					Code:      "vendor_blocked",
					Version:   "typed-2.0.0",
					LastError: "typed failure",
				},
			},
			WCPlusHealthy: true,
			WCPlusVersion: "legacy-1.0.0",
			LastError:     "legacy failure",
		}
		agent, err := store.HeartbeatAgent(input)
		if err != nil {
			t.Fatal(err)
		}
		persisted, err := store.getAgent(input.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		for _, got := range []SourceAgent{agent, persisted} {
			health := got.CapabilityHealth["wcplus"]
			if health.Healthy || health.Code != "vendor_blocked" || health.Version != "typed-2.0.0" || health.LastError != "typed failure" {
				t.Fatalf("typed wcplus changed: %#v", got)
			}
			if got.WCPlusHealthy || got.WCPlusVersion != health.Version || got.LastError != health.LastError {
				t.Fatalf("legacy fields do not mirror typed wcplus: %#v", got)
			}
		}
	})

	t.Run("legacy wcplus only heartbeat still maps", func(t *testing.T) {
		agent, err := store.HeartbeatAgent(SourceAgentHeartbeat{
			AgentID:       "legacy-wcplus-worker",
			WCPlusHealthy: true,
			WCPlusVersion: "4.2.0",
			LastError:     "legacy diagnostic",
		})
		if err != nil {
			t.Fatal(err)
		}
		health, exists := agent.CapabilityHealth["wcplus"]
		if !exists || !health.Healthy || health.Version != "4.2.0" || health.LastError != "legacy diagnostic" {
			t.Fatalf("legacy wcplus did not map: %#v", agent)
		}
	})
}
