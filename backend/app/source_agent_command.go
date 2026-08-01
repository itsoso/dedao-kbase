package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	SourceAgentCommandDiagnose = "diagnose"
	SourceAgentCommandUpgrade  = "upgrade"
)

const (
	SourceAgentCommandQueued      = "queued"
	SourceAgentCommandClaimed     = "claimed"
	SourceAgentCommandDownloading = "downloading"
	SourceAgentCommandVerified    = "verified"
	SourceAgentCommandInstalling  = "installing"
	SourceAgentCommandRestarting  = "restarting"
	SourceAgentCommandVerifying   = "verifying"
	SourceAgentCommandRollback    = "rollback"
	SourceAgentCommandSucceeded   = "succeeded"
	SourceAgentCommandFailed      = "failed"
	SourceAgentCommandCanceled    = "canceled"
	SourceAgentCommandExpired     = "expired"
	SourceAgentCommandRolledBack  = "rolled_back"
)

const (
	SourceAgentCommandCodeDiagnosticComplete = "diagnostic_complete"
	SourceAgentCommandCodeDiagnosticFailed   = "diagnostic_failed"
	SourceAgentCommandCodeUpgradeComplete    = "upgrade_complete"
	SourceAgentCommandCodeUpgradeFailed      = "upgrade_failed"
	SourceAgentCommandCodeDownloadFailed     = "download_failed"
	SourceAgentCommandCodeVerificationFailed = "verification_failed"
	SourceAgentCommandCodeInstallFailed      = "install_failed"
	SourceAgentCommandCodeRestartFailed      = "restart_failed"
	SourceAgentCommandCodeRollbackComplete   = "rollback_complete"
	SourceAgentCommandCodeRollbackFailed     = "rollback_failed"
	SourceAgentCommandCodeCanceled           = "canceled"
	SourceAgentCommandCodeExpired            = "expired"
)

const (
	sourceAgentCommandIDMaxRunes      = 128
	sourceAgentCommandCodeMaxRunes    = 64
	sourceAgentCommandMessageMaxRunes = 1000
	sourceAgentCommandPayloadMaxBytes = 2048
	sourceAgentCommandMaxTTL          = 7 * 24 * time.Hour
	sourceAgentCommandTimestampLayout = "2006-01-02T15:04:05.000000000Z"
)

var (
	ErrSourceAgentCommandNotFound            = errors.New("source agent command not found")
	ErrSourceAgentCommandTarget              = errors.New("source agent command belongs to another target")
	ErrSourceAgentCommandType                = errors.New("unsupported source agent command type")
	ErrSourceAgentCommandIdempotencyConflict = errors.New("source agent command idempotency conflict")
	ErrSourceAgentCommandVersionConflict     = errors.New("source agent command version conflict")
	ErrSourceAgentCommandActiveUpgrade       = errors.New("source agent already has an active upgrade")
	ErrSourceAgentCommandClaimOwner          = errors.New("source agent command belongs to another claim owner")
	ErrSourceAgentCommandInvalidState        = errors.New("source agent command state transition is invalid")
	ErrSourceAgentCommandResultConflict      = errors.New("source agent command durable result conflicts")
	ErrSourceAgentCommandExpired             = errors.New("source agent command expired")
)

var sourceAgentCommandResultCodes = map[string]struct{}{
	"":                                       {},
	SourceAgentCommandCodeDiagnosticComplete: {},
	SourceAgentCommandCodeDiagnosticFailed:   {},
	SourceAgentCommandCodeUpgradeComplete:    {},
	SourceAgentCommandCodeUpgradeFailed:      {},
	SourceAgentCommandCodeDownloadFailed:     {},
	SourceAgentCommandCodeVerificationFailed: {},
	SourceAgentCommandCodeInstallFailed:      {},
	SourceAgentCommandCodeRestartFailed:      {},
	SourceAgentCommandCodeRollbackComplete:   {},
	SourceAgentCommandCodeRollbackFailed:     {},
	SourceAgentCommandCodeCanceled:           {},
	SourceAgentCommandCodeExpired:            {},
}

var sourceAgentUpgradeCommandTransitions = map[string]map[string]struct{}{
	SourceAgentCommandQueued: {
		SourceAgentCommandClaimed: {}, SourceAgentCommandCanceled: {}, SourceAgentCommandExpired: {},
	},
	SourceAgentCommandClaimed: {
		SourceAgentCommandDownloading: {}, SourceAgentCommandFailed: {}, SourceAgentCommandExpired: {},
	},
	SourceAgentCommandDownloading: {
		SourceAgentCommandVerified: {}, SourceAgentCommandFailed: {},
	},
	SourceAgentCommandVerified: {
		SourceAgentCommandInstalling: {}, SourceAgentCommandFailed: {},
	},
	SourceAgentCommandInstalling: {
		SourceAgentCommandRestarting: {}, SourceAgentCommandRollback: {}, SourceAgentCommandFailed: {},
	},
	SourceAgentCommandRestarting: {
		SourceAgentCommandVerifying: {}, SourceAgentCommandRollback: {},
	},
	SourceAgentCommandVerifying: {
		SourceAgentCommandSucceeded: {}, SourceAgentCommandRollback: {},
	},
	SourceAgentCommandRollback: {
		SourceAgentCommandRolledBack: {}, SourceAgentCommandFailed: {},
	},
}

var sourceAgentDiagnoseCommandTransitions = map[string]map[string]struct{}{
	SourceAgentCommandQueued: {
		SourceAgentCommandClaimed: {}, SourceAgentCommandCanceled: {}, SourceAgentCommandExpired: {},
	},
	SourceAgentCommandClaimed: {
		SourceAgentCommandSucceeded: {}, SourceAgentCommandFailed: {}, SourceAgentCommandExpired: {},
	},
}

type SourceAgentUpgradeSpec struct {
	ArtifactID             string `json:"artifact_id"`
	ExpectedCurrentVersion string `json:"expected_current_version"`
}

type SourceAgentCommandCreate struct {
	TargetAgentID  string          `json:"target_agent_id"`
	Type           string          `json:"type"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	ExpiresAt      string          `json:"expires_at"`
}

type SourceAgentCommandTransition struct {
	State         string `json:"state"`
	ResultCode    string `json:"result_code,omitempty"`
	Message       string `json:"message,omitempty"`
	ActualVersion string `json:"actual_version,omitempty"`
}

type SourceAgentCommand struct {
	ID                     string                  `json:"id"`
	TargetAgentID          string                  `json:"target_agent_id"`
	Type                   string                  `json:"type"`
	UpgradeSpec            *SourceAgentUpgradeSpec `json:"upgrade_spec,omitempty"`
	State                  string                  `json:"state"`
	IdempotencyKey         string                  `json:"idempotency_key"`
	ExpectedCurrentVersion string                  `json:"expected_current_version,omitempty"`
	ActualVersion          string                  `json:"actual_version,omitempty"`
	ResultCode             string                  `json:"result_code,omitempty"`
	Message                string                  `json:"message,omitempty"`
	ClaimOwner             string                  `json:"claim_owner,omitempty"`
	CreatedAt              string                  `json:"created_at"`
	UpdatedAt              string                  `json:"updated_at"`
	ClaimedAt              string                  `json:"claimed_at,omitempty"`
	CompletedAt            string                  `json:"completed_at,omitempty"`
	ExpiresAt              string                  `json:"expires_at"`
}

type SourceAgentCommandEvent struct {
	Sequence  int64  `json:"sequence"`
	CommandID string `json:"command_id"`
	State     string `json:"state"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	CreatedAt string `json:"created_at"`
}

func migrateSourceAgentCommandDB(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS source_agent_commands (
			command_id TEXT PRIMARY KEY,
			target_agent_id TEXT NOT NULL,
			command_type TEXT NOT NULL CHECK(command_type IN ('diagnose', 'upgrade')),
			spec_json TEXT NOT NULL,
			state TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			expected_current_version TEXT NOT NULL DEFAULT '',
			actual_version TEXT NOT NULL DEFAULT '',
			result_code TEXT NOT NULL DEFAULT '',
			message_text TEXT NOT NULL DEFAULT '',
			claim_owner TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			claimed_at TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT '',
			expires_at TEXT NOT NULL,
			FOREIGN KEY(target_agent_id) REFERENCES source_agents(agent_id)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_source_agent_commands_idempotency
			ON source_agent_commands(target_agent_id, idempotency_key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_source_agent_commands_one_active_upgrade
			ON source_agent_commands(target_agent_id)
			WHERE command_type = 'upgrade' AND state IN (
				'queued', 'claimed', 'downloading', 'verified', 'installing', 'restarting', 'verifying', 'rollback'
			)`,
		`CREATE INDEX IF NOT EXISTS idx_source_agent_commands_target_state_created
			ON source_agent_commands(target_agent_id, state, created_at, command_id)`,
		`CREATE TABLE IF NOT EXISTS source_agent_command_events (
			event_id INTEGER PRIMARY KEY AUTOINCREMENT,
			command_id TEXT NOT NULL,
			state TEXT NOT NULL,
			result_code TEXT NOT NULL DEFAULT '',
			message_text TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			FOREIGN KEY(command_id) REFERENCES source_agent_commands(command_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_source_agent_command_events_command
			ON source_agent_command_events(command_id, event_id)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SourceSyncStore) CreateSourceAgentCommand(input SourceAgentCommandCreate) (SourceAgentCommand, error) {
	operationNow := s.now().UTC()
	now := formatSourceAgentCommandTime(operationNow)
	normalized, specJSON, spec, err := normalizeSourceAgentCommandCreate(input, operationNow)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return SourceAgentCommand{}, err
	}
	defer tx.Rollback()
	if err := expireDueSourceAgentCommandsTx(tx, normalized.TargetAgentID, now); err != nil {
		return SourceAgentCommand{}, err
	}

	var existingID, existingType, existingSpec, existingExpiry string
	err = tx.QueryRow(`
		SELECT command_id, command_type, spec_json, expires_at
		FROM source_agent_commands
		WHERE target_agent_id = ? AND idempotency_key = ?
	`, normalized.TargetAgentID, normalized.IdempotencyKey).Scan(
		&existingID, &existingType, &existingSpec, &existingExpiry,
	)
	if err == nil {
		if existingType != normalized.Type || existingSpec != specJSON || existingExpiry != normalized.ExpiresAt {
			return SourceAgentCommand{}, ErrSourceAgentCommandIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return SourceAgentCommand{}, err
		}
		return s.GetSourceAgentCommand(existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SourceAgentCommand{}, err
	}
	if err := validateNewSourceAgentCommandExpiry(normalized.ExpiresAt, operationNow); err != nil {
		return SourceAgentCommand{}, err
	}

	var agentVersion string
	if err := tx.QueryRow(`SELECT version FROM source_agents WHERE agent_id = ?`, normalized.TargetAgentID).Scan(&agentVersion); errors.Is(err, sql.ErrNoRows) {
		return SourceAgentCommand{}, ErrSourceAgentNotFound
	} else if err != nil {
		return SourceAgentCommand{}, err
	}
	expectedVersion := ""
	if spec != nil {
		expectedVersion = spec.ExpectedCurrentVersion
		if agentVersion != expectedVersion {
			return SourceAgentCommand{}, fmt.Errorf("%w: agent has %q, command expects %q", ErrSourceAgentCommandVersionConflict, agentVersion, expectedVersion)
		}
	}

	id := newSourceSyncID("cmd", operationNow)
	_, err = tx.Exec(`
		INSERT INTO source_agent_commands (
			command_id, target_agent_id, command_type, spec_json, state, idempotency_key,
			expected_current_version, created_at, updated_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, normalized.TargetAgentID, normalized.Type, specJSON, SourceAgentCommandQueued,
		normalized.IdempotencyKey, expectedVersion, now, now, normalized.ExpiresAt)
	if err != nil {
		if isSourceAgentCommandActiveUpgradeConstraint(err) {
			return SourceAgentCommand{}, ErrSourceAgentCommandActiveUpgrade
		}
		if isSourceAgentCommandIdempotencyConstraint(err) {
			return SourceAgentCommand{}, ErrSourceAgentCommandIdempotencyConflict
		}
		return SourceAgentCommand{}, err
	}
	if err := insertSourceAgentCommandEventTx(tx, id, SourceAgentCommandQueued, "", "", now); err != nil {
		return SourceAgentCommand{}, err
	}
	if err := tx.Commit(); err != nil {
		return SourceAgentCommand{}, err
	}
	return s.GetSourceAgentCommand(id)
}

func (s *SourceSyncStore) GetSourceAgentCommand(commandID string) (SourceAgentCommand, error) {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", commandID, true)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	command, err := scanSourceAgentCommand(s.db.QueryRow(sourceAgentCommandSelect+` WHERE command_id = ?`, commandID))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceAgentCommand{}, ErrSourceAgentCommandNotFound
	}
	return command, err
}

func (s *SourceSyncStore) ListSourceAgentCommandEvents(commandID string) ([]SourceAgentCommandEvent, error) {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", commandID, true)
	if err != nil {
		return nil, err
	}
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM source_agent_commands WHERE command_id = ?`, commandID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSourceAgentCommandNotFound
	} else if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
		SELECT event_id, command_id, state, result_code, message_text, created_at
		FROM source_agent_command_events
		WHERE command_id = ?
		ORDER BY event_id
	`, commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]SourceAgentCommandEvent, 0)
	for rows.Next() {
		var event SourceAgentCommandEvent
		if err := rows.Scan(&event.Sequence, &event.CommandID, &event.State, &event.Code, &event.Message, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *SourceSyncStore) ClaimNextSourceAgentCommand(agentID, claimOwner string) (*SourceAgentCommand, error) {
	operationNow := s.now().UTC()
	agentID, err := normalizeSourceAgentCommandIdentifier("agent_id", agentID, true)
	if err != nil {
		return nil, err
	}
	claimOwner, err = normalizeSourceAgentCommandIdentifier("claim_owner", claimOwner, true)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM source_agents WHERE agent_id = ?`, agentID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSourceAgentNotFound
	} else if err != nil {
		return nil, err
	}
	now := formatSourceAgentCommandTime(operationNow)
	if err := expireDueSourceAgentCommandsTx(tx, agentID, now); err != nil {
		return nil, err
	}
	var commandID string
	err = tx.QueryRow(`
		SELECT command_id FROM source_agent_commands
		WHERE target_agent_id = ? AND state = ?
		ORDER BY created_at, command_id
		LIMIT 1
	`, agentID, SourceAgentCommandQueued).Scan(&commandID)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := claimSourceAgentCommandTx(tx, commandID, agentID, claimOwner, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	command, err := s.GetSourceAgentCommand(commandID)
	return &command, err
}

func (s *SourceSyncStore) ClaimSourceAgentCommand(commandID, agentID, claimOwner string) (SourceAgentCommand, error) {
	operationNow := s.now().UTC()
	now := formatSourceAgentCommandTime(operationNow)
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", commandID, true)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	agentID, err = normalizeSourceAgentCommandIdentifier("agent_id", agentID, true)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	claimOwner, err = normalizeSourceAgentCommandIdentifier("claim_owner", claimOwner, true)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return SourceAgentCommand{}, err
	}
	defer tx.Rollback()
	command, err := scanSourceAgentCommand(tx.QueryRow(sourceAgentCommandSelect+` WHERE command_id = ?`, commandID))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceAgentCommand{}, ErrSourceAgentCommandNotFound
	}
	if err != nil {
		return SourceAgentCommand{}, err
	}
	if command.TargetAgentID != agentID {
		return SourceAgentCommand{}, ErrSourceAgentCommandTarget
	}
	if command.State == SourceAgentCommandExpired {
		return SourceAgentCommand{}, ErrSourceAgentCommandExpired
	}
	if sourceAgentCommandIsExpired(command, operationNow) && !isTerminalSourceAgentCommandState(command.State) {
		if err := expireSourceAgentCommandTx(tx, command, now); err != nil {
			return SourceAgentCommand{}, err
		}
		if err := tx.Commit(); err != nil {
			return SourceAgentCommand{}, err
		}
		return SourceAgentCommand{}, ErrSourceAgentCommandExpired
	}
	if command.State != SourceAgentCommandQueued {
		if command.ClaimOwner == claimOwner && command.ClaimOwner != "" {
			if err := tx.Commit(); err != nil {
				return SourceAgentCommand{}, err
			}
			return command, nil
		}
		if command.ClaimOwner != "" {
			return SourceAgentCommand{}, ErrSourceAgentCommandClaimOwner
		}
		return SourceAgentCommand{}, ErrSourceAgentCommandInvalidState
	}
	if err := claimSourceAgentCommandTx(tx, command.ID, agentID, claimOwner, now); err != nil {
		return SourceAgentCommand{}, err
	}
	if err := tx.Commit(); err != nil {
		return SourceAgentCommand{}, err
	}
	return s.GetSourceAgentCommand(command.ID)
}

func (s *SourceSyncStore) TransitionSourceAgentCommand(commandID, agentID, claimOwner string, input SourceAgentCommandTransition) (SourceAgentCommand, error) {
	operationNow := s.now().UTC()
	now := formatSourceAgentCommandTime(operationNow)
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", commandID, true)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	agentID, err = normalizeSourceAgentCommandIdentifier("agent_id", agentID, true)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	claimOwner, err = normalizeSourceAgentCommandIdentifier("claim_owner", claimOwner, true)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	input, err = normalizeSourceAgentCommandTransition(input)
	if err != nil {
		return SourceAgentCommand{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return SourceAgentCommand{}, err
	}
	defer tx.Rollback()
	command, err := scanSourceAgentCommand(tx.QueryRow(sourceAgentCommandSelect+` WHERE command_id = ?`, commandID))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceAgentCommand{}, ErrSourceAgentCommandNotFound
	}
	if err != nil {
		return SourceAgentCommand{}, err
	}
	if command.TargetAgentID != agentID {
		return SourceAgentCommand{}, ErrSourceAgentCommandTarget
	}
	if command.State == SourceAgentCommandExpired {
		return SourceAgentCommand{}, ErrSourceAgentCommandExpired
	}
	if sourceAgentCommandIsExpired(command, operationNow) && !isTerminalSourceAgentCommandState(command.State) {
		if err := expireSourceAgentCommandTx(tx, command, now); err != nil {
			return SourceAgentCommand{}, err
		}
		if err := tx.Commit(); err != nil {
			return SourceAgentCommand{}, err
		}
		return SourceAgentCommand{}, ErrSourceAgentCommandExpired
	}
	if command.State == SourceAgentCommandQueued {
		return SourceAgentCommand{}, ErrSourceAgentCommandInvalidState
	}
	if command.ClaimOwner != claimOwner || command.ClaimOwner == "" {
		return SourceAgentCommand{}, ErrSourceAgentCommandClaimOwner
	}
	if command.State == input.State {
		if command.ResultCode != input.ResultCode || command.Message != input.Message || command.ActualVersion != input.ActualVersion {
			return SourceAgentCommand{}, ErrSourceAgentCommandResultConflict
		}
		if err := tx.Commit(); err != nil {
			return SourceAgentCommand{}, err
		}
		return command, nil
	}
	if isTerminalSourceAgentCommandState(command.State) {
		return SourceAgentCommand{}, ErrSourceAgentCommandResultConflict
	}
	if !sourceAgentCommandTransitionAllowed(command.Type, command.State, input.State) {
		return SourceAgentCommand{}, ErrSourceAgentCommandInvalidState
	}
	if err := validateSourceAgentCommandTerminalResult(command.Type, input); err != nil {
		return SourceAgentCommand{}, err
	}
	completedAt := ""
	if isTerminalSourceAgentCommandState(input.State) {
		completedAt = now
	}
	result, err := tx.Exec(`
		UPDATE source_agent_commands SET
			state = ?, actual_version = ?, result_code = ?, message_text = ?, updated_at = ?,
			completed_at = CASE WHEN ? = '' THEN completed_at ELSE ? END
		WHERE command_id = ? AND state = ? AND claim_owner = ?
	`, input.State, input.ActualVersion, input.ResultCode, input.Message, now,
		completedAt, completedAt, command.ID, command.State, claimOwner)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return SourceAgentCommand{}, err
	} else if rows != 1 {
		return SourceAgentCommand{}, ErrSourceAgentCommandInvalidState
	}
	if err := insertSourceAgentCommandEventTx(tx, command.ID, input.State, input.ResultCode, input.Message, now); err != nil {
		return SourceAgentCommand{}, err
	}
	if err := tx.Commit(); err != nil {
		return SourceAgentCommand{}, err
	}
	return s.GetSourceAgentCommand(command.ID)
}

func (s *SourceSyncStore) CancelSourceAgentCommand(commandID, message string) (SourceAgentCommand, error) {
	operationNow := s.now().UTC()
	now := formatSourceAgentCommandTime(operationNow)
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", commandID, true)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	message, err = normalizeSourceAgentCommandMessage(message)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return SourceAgentCommand{}, err
	}
	defer tx.Rollback()
	command, err := scanSourceAgentCommand(tx.QueryRow(sourceAgentCommandSelect+` WHERE command_id = ?`, commandID))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceAgentCommand{}, ErrSourceAgentCommandNotFound
	}
	if err != nil {
		return SourceAgentCommand{}, err
	}
	if sourceAgentCommandIsExpired(command, operationNow) && !isTerminalSourceAgentCommandState(command.State) {
		if err := expireSourceAgentCommandTx(tx, command, now); err != nil {
			return SourceAgentCommand{}, err
		}
		if err := tx.Commit(); err != nil {
			return SourceAgentCommand{}, err
		}
		return SourceAgentCommand{}, ErrSourceAgentCommandExpired
	}
	if command.State == SourceAgentCommandCanceled {
		if command.ResultCode != SourceAgentCommandCodeCanceled || command.Message != message {
			return SourceAgentCommand{}, ErrSourceAgentCommandResultConflict
		}
		if err := tx.Commit(); err != nil {
			return SourceAgentCommand{}, err
		}
		return command, nil
	}
	if isTerminalSourceAgentCommandState(command.State) {
		return SourceAgentCommand{}, ErrSourceAgentCommandResultConflict
	}
	if !sourceAgentCommandTransitionAllowed(command.Type, command.State, SourceAgentCommandCanceled) {
		return SourceAgentCommand{}, ErrSourceAgentCommandInvalidState
	}
	result, err := tx.Exec(`
		UPDATE source_agent_commands SET state = ?, result_code = ?, message_text = ?, updated_at = ?, completed_at = ?
		WHERE command_id = ? AND state = ?
	`, SourceAgentCommandCanceled, SourceAgentCommandCodeCanceled, message, now, now, command.ID, command.State)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return SourceAgentCommand{}, err
	} else if rows != 1 {
		return SourceAgentCommand{}, ErrSourceAgentCommandInvalidState
	}
	if err := insertSourceAgentCommandEventTx(tx, command.ID, SourceAgentCommandCanceled, SourceAgentCommandCodeCanceled, message, now); err != nil {
		return SourceAgentCommand{}, err
	}
	if err := tx.Commit(); err != nil {
		return SourceAgentCommand{}, err
	}
	return s.GetSourceAgentCommand(command.ID)
}

const sourceAgentCommandSelect = `
	SELECT command_id, target_agent_id, command_type, spec_json, state, idempotency_key,
		expected_current_version, actual_version, result_code, message_text, claim_owner,
		created_at, updated_at, claimed_at, completed_at, expires_at
	FROM source_agent_commands`

func scanSourceAgentCommand(row sourceSyncScanner) (SourceAgentCommand, error) {
	var command SourceAgentCommand
	var specJSON string
	err := row.Scan(
		&command.ID, &command.TargetAgentID, &command.Type, &specJSON, &command.State,
		&command.IdempotencyKey, &command.ExpectedCurrentVersion, &command.ActualVersion,
		&command.ResultCode, &command.Message, &command.ClaimOwner, &command.CreatedAt,
		&command.UpdatedAt, &command.ClaimedAt, &command.CompletedAt, &command.ExpiresAt,
	)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	if command.Type == SourceAgentCommandUpgrade {
		var spec SourceAgentUpgradeSpec
		if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
			return SourceAgentCommand{}, err
		}
		command.UpgradeSpec = &spec
	}
	return command, nil
}

func normalizeSourceAgentCommandCreate(input SourceAgentCommandCreate, now time.Time) (SourceAgentCommandCreate, string, *SourceAgentUpgradeSpec, error) {
	var err error
	input.TargetAgentID, err = normalizeSourceAgentCommandIdentifier("target_agent_id", input.TargetAgentID, true)
	if err != nil {
		return input, "", nil, err
	}
	input.Type = strings.ToLower(strings.TrimSpace(input.Type))
	if input.Type != SourceAgentCommandDiagnose && input.Type != SourceAgentCommandUpgrade {
		return input, "", nil, ErrSourceAgentCommandType
	}
	input.IdempotencyKey, err = normalizeSourceAgentCommandIdentifier("idempotency_key", input.IdempotencyKey, true)
	if err != nil {
		return input, "", nil, err
	}
	input.ExpiresAt, err = normalizeSourceAgentCommandExpiry(input.ExpiresAt, now)
	if err != nil {
		return input, "", nil, err
	}
	payload := bytes.TrimSpace(input.Payload)
	if len(payload) > sourceAgentCommandPayloadMaxBytes {
		return input, "", nil, fmt.Errorf("payload exceeds %d bytes", sourceAgentCommandPayloadMaxBytes)
	}
	if input.Type == SourceAgentCommandDiagnose {
		if len(payload) != 0 {
			return input, "", nil, fmt.Errorf("diagnose payload must be omitted")
		}
		return input, `{}`, nil, nil
	}
	if len(payload) == 0 {
		return input, "", nil, fmt.Errorf("upgrade payload is required")
	}
	var spec SourceAgentUpgradeSpec
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return input, "", nil, fmt.Errorf("decode upgrade payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return input, "", nil, fmt.Errorf("upgrade payload must contain exactly one JSON value")
		}
		return input, "", nil, fmt.Errorf("decode trailing upgrade payload: %w", err)
	}
	spec.ArtifactID, err = normalizeSourceAgentCommandIdentifier("artifact_id", spec.ArtifactID, true)
	if err != nil {
		return input, "", nil, err
	}
	spec.ExpectedCurrentVersion, err = normalizeSourceAgentVersion("expected_current_version", spec.ExpectedCurrentVersion)
	if err != nil {
		return input, "", nil, err
	}
	if spec.ExpectedCurrentVersion == "" {
		return input, "", nil, fmt.Errorf("expected_current_version is required")
	}
	canonical, err := json.Marshal(spec)
	if err != nil {
		return input, "", nil, err
	}
	return input, string(canonical), &spec, nil
}

func normalizeSourceAgentCommandTransition(input SourceAgentCommandTransition) (SourceAgentCommandTransition, error) {
	input.State = strings.ToLower(strings.TrimSpace(input.State))
	if !isSourceAgentCommandState(input.State) || input.State == SourceAgentCommandQueued || input.State == SourceAgentCommandClaimed || input.State == SourceAgentCommandExpired || input.State == SourceAgentCommandCanceled {
		return input, ErrSourceAgentCommandInvalidState
	}
	input.ResultCode = strings.ToLower(strings.TrimSpace(input.ResultCode))
	if len([]rune(input.ResultCode)) > sourceAgentCommandCodeMaxRunes {
		return input, fmt.Errorf("result_code exceeds %d characters", sourceAgentCommandCodeMaxRunes)
	}
	if _, allowed := sourceAgentCommandResultCodes[input.ResultCode]; !allowed {
		return input, fmt.Errorf("unsupported source agent command result code %q", input.ResultCode)
	}
	message, err := normalizeSourceAgentCommandMessage(input.Message)
	if err != nil {
		return input, err
	}
	input.Message = message
	input.ActualVersion, err = normalizeSourceAgentVersion("actual_version", input.ActualVersion)
	if err != nil {
		return input, err
	}
	return input, nil
}

func normalizeSourceAgentCommandIdentifier(field, value string, required bool) (string, error) {
	value, err := normalizeSourceAgentName(field, value, sourceAgentCommandIDMaxRunes, false)
	if err != nil {
		return "", err
	}
	if required && value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	return value, nil
}

func normalizeSourceAgentCommandExpiry(value string, now time.Time) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("expires_at is required")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", fmt.Errorf("expires_at must be RFC3339: %w", err)
	}
	expiresAt = expiresAt.UTC()
	now = now.UTC()
	if expiresAt.Sub(now) > sourceAgentCommandMaxTTL {
		return "", fmt.Errorf("expires_at exceeds maximum TTL of %s", sourceAgentCommandMaxTTL)
	}
	return formatSourceAgentCommandTime(expiresAt), nil
}

func validateNewSourceAgentCommandExpiry(value string, now time.Time) error {
	expiresAt, err := time.Parse(sourceAgentCommandTimestampLayout, value)
	if err != nil {
		return fmt.Errorf("expires_at must be RFC3339: %w", err)
	}
	if !expiresAt.After(now.UTC()) {
		return fmt.Errorf("expires_at must be in the future")
	}
	return nil
}

func formatSourceAgentCommandTime(value time.Time) string {
	return value.UTC().Format(sourceAgentCommandTimestampLayout)
}

func normalizeSourceAgentCommandMessage(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > sourceAgentCommandMessageMaxRunes {
		return "", fmt.Errorf("message exceeds %d characters", sourceAgentCommandMessageMaxRunes)
	}
	for _, character := range value {
		if character < 0x20 && character != '\n' && character != '\t' {
			return "", fmt.Errorf("message contains control characters")
		}
	}
	if containsSourceAgentCommandAbsolutePath(value) {
		return "", fmt.Errorf("message must not contain a local absolute path")
	}
	return value, nil
}

func containsSourceAgentCommandAbsolutePath(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		boundary := sourceAgentCommandPathTokenBoundary(value, index)
		nonASCIIBoundary := sourceAgentCommandNonASCIIProseBoundary(value, index)
		if character == '/' {
			if scheme, ok := sourceAgentCommandURISchemeForTokenSlash(value, index); ok {
				if strings.EqualFold(scheme, "file") {
					return true
				}
				continue
			}
			if sourceAgentCommandHTTPRouteSlash(value, index) {
				continue
			}
			if boundary || nonASCIIBoundary && sourceAgentCommandTokenHasLaterSlash(value, index) {
				return true
			}
		}
		if character == '~' && (boundary || nonASCIIBoundary) && index+1 < len(value) &&
			(value[index+1] == '/' || value[index+1] == '\\') {
			return true
		}
		if (boundary || nonASCIIBoundary) && isASCIILetter(character) && index+2 < len(value) && value[index+1] == ':' &&
			(value[index+2] == '/' || value[index+2] == '\\') {
			return true
		}
		if character == '\\' && (boundary || nonASCIIBoundary) && index+1 < len(value) && value[index+1] == '\\' {
			return true
		}
	}
	return false
}

func sourceAgentCommandPathTokenBoundary(value string, index int) bool {
	if index == 0 {
		return true
	}
	previous, _ := utf8.DecodeLastRuneInString(value[:index])
	return !unicode.IsLetter(previous) && !unicode.IsDigit(previous) && previous != '_'
}

func sourceAgentCommandNonASCIIProseBoundary(value string, index int) bool {
	if index == 0 {
		return false
	}
	previous, _ := utf8.DecodeLastRuneInString(value[:index])
	return previous >= utf8.RuneSelf
}

func sourceAgentCommandTokenHasLaterSlash(value string, index int) bool {
	_, end := sourceAgentCommandWhitespaceTokenBounds(value, index)
	return strings.Contains(value[index+1:end], "/")
}

func sourceAgentCommandURISchemeForTokenSlash(value string, index int) (string, bool) {
	tokenStart, tokenEnd := sourceAgentCommandWhitespaceTokenBounds(value, index)
	token := value[tokenStart:tokenEnd]
	slashOffset := index - tokenStart
	searchEnd := slashOffset + 1
	if searchEnd < len(token) && token[searchEnd] == '/' {
		searchEnd++
	}
	colon := strings.LastIndex(token[:searchEnd], "://")
	if colon <= 0 {
		return "", false
	}
	schemeStart := colon - 1
	for schemeStart >= 0 && isSourceAgentCommandURISchemeCharacter(token[schemeStart]) {
		schemeStart--
	}
	schemeStart++
	if schemeStart >= colon || !isASCIILetter(token[schemeStart]) {
		return "", false
	}
	if schemeStart > 0 {
		previous, _ := utf8.DecodeLastRuneInString(token[:schemeStart])
		if previous == '/' || previous == '\\' {
			return "", false
		}
	}
	wrapper := sourceAgentCommandActiveURIWrapper(token[:schemeStart])
	if slashOffset > colon+2 && sourceAgentCommandURIContextTerminated(token[colon+3:slashOffset], wrapper) {
		return "", false
	}
	return token[schemeStart:colon], true
}

func sourceAgentCommandURIContextTerminated(value string, wrapper rune) bool {
	depth := 0
	if wrapper != 0 {
		depth = 1
	}
	for _, character := range value {
		if unicode.IsSpace(character) {
			return true
		}
		if depth > 0 {
			if delta := sourceAgentCommandURIWrapperDepthDelta(wrapper, character); delta != 0 {
				depth += delta
				if depth == 0 {
					return true
				}
				continue
			}
		}
		if !isSourceAgentCommandRawURIIRIRune(character) {
			return true
		}
	}
	return false
}

func sourceAgentCommandActiveURIWrapper(value string) rune {
	stack := make([]rune, 0, 4)
	for _, character := range value {
		if len(stack) > 0 {
			top := stack[len(stack)-1]
			if sourceAgentCommandURIQuoteWrapper(top) {
				if character == top {
					stack = stack[:len(stack)-1]
				}
				continue
			}
			if sourceAgentCommandURIWrapperDepthDelta(top, character) < 0 {
				stack = stack[:len(stack)-1]
				continue
			}
		}
		if sourceAgentCommandURIWrapperOpener(character) {
			stack = append(stack, character)
		}
	}
	if len(stack) == 0 {
		return 0
	}
	return stack[len(stack)-1]
}

func sourceAgentCommandURIWrapperOpener(character rune) bool {
	switch character {
	case '(', '[', '{', '<', '"', '\'', '`':
		return true
	default:
		return unicode.Is(unicode.Ps, character) || unicode.Is(unicode.Pi, character)
	}
}

func sourceAgentCommandURIQuoteWrapper(character rune) bool {
	return character == '"' || character == '\'' || character == '`'
}

func sourceAgentCommandURIWrapperDepthDelta(wrapper, character rune) int {
	switch wrapper {
	case '(':
		return sourceAgentCommandURIExactWrapperDelta(character, '(', ')')
	case '[':
		return sourceAgentCommandURIExactWrapperDelta(character, '[', ']')
	case '{':
		return sourceAgentCommandURIExactWrapperDelta(character, '{', '}')
	case '<':
		return sourceAgentCommandURIExactWrapperDelta(character, '<', '>')
	case '"', '\'', '`':
		if character == wrapper {
			return -1
		}
	case 0:
		return 0
	default:
		if unicode.Is(unicode.Ps, wrapper) {
			if unicode.Is(unicode.Ps, character) {
				return 1
			}
			if unicode.Is(unicode.Pe, character) {
				return -1
			}
		}
		if unicode.Is(unicode.Pi, wrapper) {
			if unicode.Is(unicode.Pi, character) {
				return 1
			}
			if unicode.Is(unicode.Pf, character) {
				return -1
			}
		}
	}
	return 0
}

func sourceAgentCommandURIExactWrapperDelta(character, opener, closer rune) int {
	if character == opener {
		return 1
	}
	if character == closer {
		return -1
	}
	return 0
}

func isSourceAgentCommandRawURIIRIRune(character rune) bool {
	if character > unicode.MaxASCII {
		return unicode.IsLetter(character) || unicode.IsNumber(character) || unicode.IsMark(character)
	}
	if isASCIILetter(byte(character)) || character >= '0' && character <= '9' {
		return true
	}
	return strings.ContainsRune("-._~:/?#[]@!$&'()*+,;=%", character)
}

func sourceAgentCommandWhitespaceTokenBounds(value string, index int) (int, int) {
	start := index
	for start > 0 {
		previous, size := utf8.DecodeLastRuneInString(value[:start])
		if unicode.IsSpace(previous) {
			break
		}
		start -= size
	}
	end := index
	for end < len(value) {
		character, size := utf8.DecodeRuneInString(value[end:])
		if unicode.IsSpace(character) {
			break
		}
		end += size
	}
	return start, end
}

func sourceAgentCommandHTTPRouteSlash(value string, index int) bool {
	if index == 0 {
		return false
	}
	separator, _ := utf8.DecodeLastRuneInString(value[:index])
	if !unicode.IsSpace(separator) {
		return false
	}
	prefix := strings.TrimSpace(value[:index])
	if prefix == "" {
		return false
	}
	methodStart := len(prefix)
	for methodStart > 0 {
		previous, size := utf8.DecodeLastRuneInString(prefix[:methodStart])
		if unicode.IsSpace(previous) {
			break
		}
		methodStart -= size
	}
	switch strings.ToUpper(prefix[methodStart:]) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE":
	default:
		return false
	}
	routeEnd := len(value)
	for offset, character := range value[index:] {
		if unicode.IsSpace(character) {
			routeEnd = index + offset
			break
		}
	}
	routePath := value[index:routeEnd]
	if query := strings.IndexByte(routePath, '?'); query >= 0 {
		routePath = routePath[:query]
	}
	return sourceAgentCommandAllowedControlPlaneRoute(routePath)
}

func sourceAgentCommandAllowedControlPlaneRoute(routePath string) bool {
	segments := strings.Split(strings.TrimPrefix(routePath, "/"), "/")
	if len(segments) == 1 && segments[0] == "health" {
		return true
	}
	return len(segments) >= 2 && segments[0] == "api" &&
		(segments[1] == "source-agent" || segments[1] == "source-agents")
}

func isSourceAgentCommandURISchemeCharacter(value byte) bool {
	return isASCIILetter(value) || value >= '0' && value <= '9' || value == '+' || value == '-' || value == '.'
}

func isASCIILetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func validateSourceAgentCommandTerminalResult(commandType string, input SourceAgentCommandTransition) error {
	if isTerminalSourceAgentCommandState(input.State) && input.ResultCode == "" {
		return fmt.Errorf("result_code is required for terminal command state")
	}
	if !isTerminalSourceAgentCommandState(input.State) && input.ResultCode != "" {
		return fmt.Errorf("result_code is only accepted for a terminal command state")
	}
	if !sourceAgentCommandResultCodeMatches(commandType, input.State, input.ResultCode) {
		return fmt.Errorf("result_code %q is invalid for %s command state %s", input.ResultCode, commandType, input.State)
	}
	if commandType == SourceAgentCommandDiagnose && input.ActualVersion != "" {
		return fmt.Errorf("diagnose command does not accept actual_version")
	}
	if commandType == SourceAgentCommandUpgrade && input.State == SourceAgentCommandSucceeded && input.ActualVersion == "" {
		return fmt.Errorf("actual_version is required for succeeded upgrade")
	}
	if commandType == SourceAgentCommandUpgrade && input.State != SourceAgentCommandSucceeded && input.ActualVersion != "" {
		return fmt.Errorf("actual_version is only accepted for a succeeded upgrade")
	}
	return nil
}

func sourceAgentCommandResultCodeMatches(commandType, state, code string) bool {
	if !isTerminalSourceAgentCommandState(state) {
		return code == ""
	}
	if commandType == SourceAgentCommandDiagnose {
		switch state {
		case SourceAgentCommandSucceeded:
			return code == SourceAgentCommandCodeDiagnosticComplete
		case SourceAgentCommandFailed:
			return code == SourceAgentCommandCodeDiagnosticFailed
		default:
			return false
		}
	}
	switch state {
	case SourceAgentCommandSucceeded:
		return code == SourceAgentCommandCodeUpgradeComplete
	case SourceAgentCommandRolledBack:
		return code == SourceAgentCommandCodeRollbackComplete
	case SourceAgentCommandFailed:
		switch code {
		case SourceAgentCommandCodeUpgradeFailed, SourceAgentCommandCodeDownloadFailed,
			SourceAgentCommandCodeVerificationFailed, SourceAgentCommandCodeInstallFailed,
			SourceAgentCommandCodeRestartFailed, SourceAgentCommandCodeRollbackFailed:
			return true
		}
	}
	return false
}

func claimSourceAgentCommandTx(tx *sql.Tx, commandID, agentID, claimOwner, now string) error {
	var commandType, state string
	if err := tx.QueryRow(`SELECT command_type, state FROM source_agent_commands WHERE command_id = ? AND target_agent_id = ?`, commandID, agentID).Scan(&commandType, &state); errors.Is(err, sql.ErrNoRows) {
		return ErrSourceAgentCommandTarget
	} else if err != nil {
		return err
	}
	if !sourceAgentCommandTransitionAllowed(commandType, state, SourceAgentCommandClaimed) {
		return ErrSourceAgentCommandInvalidState
	}
	result, err := tx.Exec(`
		UPDATE source_agent_commands SET state = ?, claim_owner = ?, claimed_at = ?, updated_at = ?
		WHERE command_id = ? AND target_agent_id = ? AND state = ?
	`, SourceAgentCommandClaimed, claimOwner, now, now, commandID, agentID, SourceAgentCommandQueued)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows != 1 {
		return ErrSourceAgentCommandInvalidState
	}
	return insertSourceAgentCommandEventTx(tx, commandID, SourceAgentCommandClaimed, "", "", now)
}

func expireDueSourceAgentCommandsTx(tx *sql.Tx, agentID, now string) error {
	rows, err := tx.Query(sourceAgentCommandSelect+`
		WHERE target_agent_id = ? AND expires_at <= ?
			AND state NOT IN (?, ?, ?, ?, ?)
		ORDER BY created_at, command_id
	`, agentID, now, SourceAgentCommandSucceeded, SourceAgentCommandFailed,
		SourceAgentCommandCanceled, SourceAgentCommandExpired, SourceAgentCommandRolledBack)
	if err != nil {
		return err
	}
	commands := make([]SourceAgentCommand, 0)
	for rows.Next() {
		command, err := scanSourceAgentCommand(rows)
		if err != nil {
			rows.Close()
			return err
		}
		commands = append(commands, command)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, command := range commands {
		if err := expireSourceAgentCommandTx(tx, command, now); err != nil {
			return err
		}
	}
	return nil
}

func expireSourceAgentCommandTx(tx *sql.Tx, command SourceAgentCommand, now string) error {
	if isTerminalSourceAgentCommandState(command.State) {
		return nil
	}
	result, err := tx.Exec(`
		UPDATE source_agent_commands SET state = ?, result_code = ?, updated_at = ?, completed_at = ?
		WHERE command_id = ? AND state = ?
	`, SourceAgentCommandExpired, SourceAgentCommandCodeExpired, now, now, command.ID, command.State)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows != 1 {
		return ErrSourceAgentCommandInvalidState
	}
	return insertSourceAgentCommandEventTx(tx, command.ID, SourceAgentCommandExpired, SourceAgentCommandCodeExpired, "", now)
}

func insertSourceAgentCommandEventTx(tx *sql.Tx, commandID, state, code, message, now string) error {
	_, err := tx.Exec(`
		INSERT INTO source_agent_command_events (command_id, state, result_code, message_text, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, commandID, state, code, message, now)
	return err
}

func sourceAgentCommandTransitionAllowed(commandType, from, to string) bool {
	transitions := sourceAgentDiagnoseCommandTransitions
	if commandType == SourceAgentCommandUpgrade {
		transitions = sourceAgentUpgradeCommandTransitions
	}
	allowed, exists := transitions[from]
	if !exists {
		return false
	}
	_, exists = allowed[to]
	return exists
}

func isSourceAgentCommandState(state string) bool {
	switch state {
	case SourceAgentCommandQueued, SourceAgentCommandClaimed, SourceAgentCommandDownloading,
		SourceAgentCommandVerified, SourceAgentCommandInstalling, SourceAgentCommandRestarting,
		SourceAgentCommandVerifying, SourceAgentCommandRollback, SourceAgentCommandSucceeded,
		SourceAgentCommandFailed, SourceAgentCommandCanceled, SourceAgentCommandExpired,
		SourceAgentCommandRolledBack:
		return true
	default:
		return false
	}
}

func isTerminalSourceAgentCommandState(state string) bool {
	switch state {
	case SourceAgentCommandSucceeded, SourceAgentCommandFailed, SourceAgentCommandCanceled,
		SourceAgentCommandExpired, SourceAgentCommandRolledBack:
		return true
	default:
		return false
	}
}

func sourceAgentCommandIsExpired(command SourceAgentCommand, now time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339Nano, command.ExpiresAt)
	return err != nil || !expiresAt.After(now.UTC())
}

func isSourceAgentCommandActiveUpgradeConstraint(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "idx_source_agent_commands_one_active_upgrade") ||
		strings.Contains(message, "source_agent_commands.target_agent_id") && !strings.Contains(message, "idempotency_key")
}

func isSourceAgentCommandIdempotencyConstraint(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "idx_source_agent_commands_idempotency") ||
		strings.Contains(message, "source_agent_commands.target_agent_id, source_agent_commands.idempotency_key")
}
