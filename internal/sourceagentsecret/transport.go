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
	ErrUnsupportedPlatform       = errors.New("source agent secret store is unsupported on this platform")
)

type Loader func(context.Context) (string, error)

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
