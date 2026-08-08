//go:build !darwin && !linux

// Package sourceagentupdate contains the platform-neutral updater protocols.
// Protected config storage is macOS-first and remains unavailable here.
package sourceagentupdate

import "errors"

const ConfigSchemaV1 = "source-agent-updater-config.v1"

var (
	ErrInvalidConfig       = errors.New("source agent updater config is invalid")
	ErrUnsafeConfigStorage = errors.New("source agent updater config storage is unsafe")
)

// Config is the cross-platform, non-secret protocol shape. Unsupported
// platforms expose the type but reject all local persistence operations.
type Config struct {
	Schema   string `json:"schema"`
	KBaseURL string `json:"kbase_url"`
	AgentID  string `json:"agent_id"`
}

func LoadConfig(string) (Config, error) {
	return Config{}, ErrUnsafeConfigStorage
}

func SaveConfig(string, Config) error {
	return ErrUnsafeConfigStorage
}
