package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SourceAgentDesiredActive = "active"
	SourceAgentDesiredPaused = "paused"

	SourceAgentObservedOnline         = "online"
	SourceAgentObservedDegraded       = "degraded"
	SourceAgentObservedRequiresAction = "requires_action"
	SourceAgentObservedOffline        = "offline"
	SourceAgentObservedUpgrading      = "upgrading"
)

const (
	sourceAgentRuntimeNameMaxRunes = 64
	sourceAgentVersionMaxRunes     = 128
	sourceAgentIDMaxRunes          = 128
	sourceAgentMaxCounter          = 1<<31 - 1
	sourceAgentMaxCapabilities     = 32
)

var allowedSourceCapabilityCodes = map[string]struct{}{
	"": {}, "login_required": {}, "vendor_blocked": {},
	"dependency_unavailable": {}, "config_invalid": {},
	"upgrade_required": {}, "throttled": {},
}

var allowedSourceAgentRunStages = map[string]struct{}{
	"": {}, "queued": {}, "running": {}, "downloading": {}, "building_knowledge": {},
	"recovery_required": {}, "completed": {}, "failed": {}, "interrupted": {},
}

func DeriveSourceAgentObservedState(agent SourceAgent, now time.Time, freshness time.Duration, upgradeActive bool) string {
	if upgradeActive {
		return SourceAgentObservedUpgrading
	}
	if freshness <= 0 {
		return SourceAgentObservedOffline
	}
	heartbeatAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(agent.LastHeartbeatAt))
	if err != nil || heartbeatAt.After(now) || now.Sub(heartbeatAt) > freshness {
		return SourceAgentObservedOffline
	}

	degraded := false
	for _, health := range agent.CapabilityHealth {
		if strings.TrimSpace(health.RequiresAction) != "" {
			return SourceAgentObservedRequiresAction
		}
		switch strings.ToLower(strings.TrimSpace(health.Code)) {
		case "login_required", "vendor_blocked", "config_invalid", "upgrade_required":
			return SourceAgentObservedRequiresAction
		}
		if !health.Healthy {
			degraded = true
		}
	}
	if degraded {
		return SourceAgentObservedDegraded
	}
	return SourceAgentObservedOnline
}

func (s *SourceSyncStore) SetAgentDesiredState(agentID, desired string) (SourceAgent, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return SourceAgent{}, fmt.Errorf("agent_id is required")
	}
	desired = strings.TrimSpace(desired)
	if desired != SourceAgentDesiredActive && desired != SourceAgentDesiredPaused {
		return SourceAgent{}, ErrSourceAgentDesiredState
	}
	result, err := s.db.Exec(`
		UPDATE source_agents SET desired_state = ?, updated_at = ?
		WHERE agent_id = ?
	`, desired, s.timestamp(), agentID)
	if err != nil {
		return SourceAgent{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return SourceAgent{}, err
	}
	if rows == 0 {
		return SourceAgent{}, ErrSourceAgentNotFound
	}
	return s.getAgent(agentID)
}

func (s *SourceSyncStore) GetSourceAgent(agentID string) (SourceAgent, error) {
	agentID, err := normalizeSourceAgentCommandIdentifier("agent_id", agentID, true)
	if err != nil {
		return SourceAgent{}, err
	}
	agent, err := s.getAgent(agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceAgent{}, ErrSourceAgentNotFound
	}
	return agent, err
}

func normalizeSourceAgentHeartbeat(heartbeat SourceAgentHeartbeat) (SourceAgentHeartbeat, error) {
	var err error
	if heartbeat.AgentID, err = normalizeSourceAgentName("agent_id", heartbeat.AgentID, sourceAgentIDMaxRunes, false); err != nil {
		return heartbeat, err
	}
	if heartbeat.AgentID == "" {
		return heartbeat, fmt.Errorf("agent_id is required")
	}
	if heartbeat.WorkerType, err = normalizeSourceAgentName("worker_type", heartbeat.WorkerType, sourceAgentRuntimeNameMaxRunes, true); err != nil {
		return heartbeat, err
	}
	if heartbeat.WorkerType == "" {
		heartbeat.WorkerType = "legacy"
	}
	if heartbeat.Platform, err = normalizeSourceAgentName("platform", heartbeat.Platform, sourceAgentRuntimeNameMaxRunes, true); err != nil {
		return heartbeat, err
	}
	if heartbeat.Architecture, err = normalizeSourceAgentName("architecture", heartbeat.Architecture, sourceAgentRuntimeNameMaxRunes, true); err != nil {
		return heartbeat, err
	}
	if heartbeat.Version, err = normalizeSourceAgentVersion("version", heartbeat.Version); err != nil {
		return heartbeat, err
	}
	if heartbeat.ProtocolVersion, err = normalizeSourceAgentVersion("protocol_version", heartbeat.ProtocolVersion); err != nil {
		return heartbeat, err
	}
	if heartbeat.WCPlusVersion, err = normalizeSourceAgentVersion("wcplus_version", heartbeat.WCPlusVersion); err != nil {
		return heartbeat, err
	}
	if heartbeat.CurrentRunID, err = normalizeSourceAgentName("current_run_id", heartbeat.CurrentRunID, sourceAgentIDMaxRunes, false); err != nil {
		return heartbeat, err
	}
	if heartbeat.CurrentRunStage, err = normalizeSourceAgentName("current_run_stage", heartbeat.CurrentRunStage, sourceAgentRuntimeNameMaxRunes, true); err != nil {
		return heartbeat, err
	}
	if _, allowed := allowedSourceAgentRunStages[heartbeat.CurrentRunStage]; !allowed {
		return heartbeat, fmt.Errorf("unsupported current_run_stage")
	}
	if heartbeat.CurrentCommandID, err = normalizeSourceAgentName("current_command_id", heartbeat.CurrentCommandID, sourceAgentIDMaxRunes, false); err != nil {
		return heartbeat, err
	}
	if heartbeat.OutboxPending < 0 || heartbeat.OutboxPending > sourceAgentMaxCounter {
		return heartbeat, fmt.Errorf("outbox_pending must be between 0 and %d", sourceAgentMaxCounter)
	}
	if heartbeat.DeadLetterCount < 0 || heartbeat.DeadLetterCount > sourceAgentMaxCounter {
		return heartbeat, fmt.Errorf("dead_letter_count must be between 0 and %d", sourceAgentMaxCounter)
	}
	if heartbeat.LastSuccessAt, err = normalizeSourceAgentTimestamp("last_success_at", heartbeat.LastSuccessAt); err != nil {
		return heartbeat, err
	}
	return heartbeat, nil
}

func normalizeSourceAgentName(field, value string, maxRunes int, lowercase bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len([]rune(value)) > maxRunes {
		return "", fmt.Errorf("%s exceeds %d characters", field, maxRunes)
	}
	if lowercase {
		value = strings.ToLower(value)
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return "", fmt.Errorf("%s contains invalid characters", field)
	}
	return value, nil
}

func normalizeSourceAgentVersion(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > sourceAgentVersionMaxRunes {
		return "", fmt.Errorf("%s exceeds %d characters", field, sourceAgentVersionMaxRunes)
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return "", fmt.Errorf("%s must contain printable ASCII characters only", field)
		}
	}
	return value, nil
}

func normalizeSourceAgentTimestamp(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", fmt.Errorf("%s must be RFC3339: %w", field, err)
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}

func normalizeSourceCapabilityHealth(input map[string]SourceCapabilityHealth) (map[string]SourceCapabilityHealth, error) {
	if len(input) > sourceAgentMaxCapabilities {
		return nil, fmt.Errorf("capability_health exceeds %d entries", sourceAgentMaxCapabilities)
	}
	result := make(map[string]SourceCapabilityHealth, len(input))
	for rawKey, health := range input {
		key, err := normalizeSourceAgentName("capability_health key", rawKey, sourceAgentRuntimeNameMaxRunes, true)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("capability_health key is required")
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate capability_health key %q", key)
		}
		health.Code = strings.ToLower(strings.TrimSpace(health.Code))
		if _, allowed := allowedSourceCapabilityCodes[health.Code]; !allowed {
			return nil, fmt.Errorf("unsupported capability diagnostic code %q", health.Code)
		}
		health.Version, err = normalizeSourceAgentVersion("capability version", health.Version)
		if err != nil {
			return nil, err
		}
		health.LastError = trimSourceDiagnostic(health.LastError)
		health.RequiresAction = trimSourceDiagnostic(health.RequiresAction)
		result[key] = health
	}
	return result, nil
}
