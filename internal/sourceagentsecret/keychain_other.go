//go:build !darwin

package sourceagentsecret

import "context"

func loadTransportToken(context.Context, keychainCommandRunner) (string, error) {
	return "", ErrUnsupportedPlatform
}
