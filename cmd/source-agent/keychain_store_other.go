//go:build !darwin

package main

import (
	"context"
	"fmt"
	"github.com/yann0917/dedao-gui/backend/app"
)

type unsupportedSecretStore struct{}

func newKeychainSecretStore(string, any) app.SourceSecretStore { return unsupportedSecretStore{} }
func (unsupportedSecretStore) Load(context.Context, string) ([]byte, error) {
	return nil, fmt.Errorf("keychain secret store is unavailable on this platform")
}
func (unsupportedSecretStore) Save(context.Context, string, []byte) error {
	return fmt.Errorf("keychain secret store is unavailable on this platform")
}
func (unsupportedSecretStore) Delete(context.Context, string) error {
	return fmt.Errorf("keychain secret store is unavailable on this platform")
}
