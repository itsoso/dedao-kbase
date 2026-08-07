package sourceagentsecret

import (
	"context"
	"errors"
)

const (
	KeychainService        = "life.executor.kbase.source-agent"
	TransportTokenAccount  = "transport-token"
	MaxTransportTokenBytes = 1024
)

var (
	ErrTransportTokenUnavailable = errors.New("source agent transport token is unavailable")
	ErrTransportTokenNotFound    = errors.New("source agent transport token is not found")
	ErrUnsupportedPlatform       = errors.New("source agent secret store is unsupported on this platform")
)

type Loader func(context.Context) (string, error)
type AgentLoader func(context.Context, string) (string, error)

type keychainCommandRunner func(context.Context, string, []string, []byte) ([]byte, error)

func LoadTransportToken(ctx context.Context) (string, error) {
	return loadTransportToken(ctx, nil)
}

func ResolveTransportToken(ctx context.Context, value string, provided bool, loader Loader) (string, error) {
	if provided {
		if !validTransportToken(value) {
			return "", ErrTransportTokenUnavailable
		}
		return value, nil
	}
	if loader == nil {
		loader = LoadTransportToken
	}
	value, err := loader(ctx)
	if err != nil || !validTransportToken(value) {
		return "", ErrTransportTokenUnavailable
	}
	return value, nil
}

// ResolveSourceTransportToken preserves the source-agent's original per-agent
// Keychain account as a migration fallback. The fallback is intentionally
// available only when the fixed shared account is absent: a corrupt value or a
// read failure in the fixed account must fail closed.
func ResolveSourceTransportToken(ctx context.Context, value string, provided bool, agentID string, loader Loader, legacy AgentLoader) (string, error) {
	if provided {
		return ResolveTransportToken(ctx, value, true, loader)
	}
	if loader == nil {
		loader = LoadTransportToken
	}
	value, err := loader(ctx)
	if err == nil {
		if !validTransportToken(value) {
			return "", ErrTransportTokenUnavailable
		}
		return value, nil
	}
	if !errors.Is(err, ErrTransportTokenNotFound) || legacy == nil || !validLegacyAgentID(agentID) {
		return "", ErrTransportTokenUnavailable
	}
	value, err = legacy(ctx, agentID)
	if err != nil || !validTransportToken(value) {
		return "", ErrTransportTokenUnavailable
	}
	return value, nil
}

func validTransportToken(value string) bool {
	if value == "" || len(value) > MaxTransportTokenBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

func validLegacyAgentID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e || value[index] == '/' || value[index] == '\\' {
			return false
		}
	}
	return true
}
