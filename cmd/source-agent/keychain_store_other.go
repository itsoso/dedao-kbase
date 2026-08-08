//go:build !darwin

package main

import (
	"context"

	"github.com/yann0917/dedao-gui/backend/app"
	"github.com/yann0917/dedao-gui/internal/sourceagentsecret"
)

type unsupportedSecretStore struct{}

func newKeychainSecretStore(string, any) app.SourceSecretStore { return unsupportedSecretStore{} }
func (unsupportedSecretStore) Load(context.Context, string) ([]byte, error) {
	return nil, sourceagentsecret.ErrUnsupportedPlatform
}
func (unsupportedSecretStore) Save(context.Context, string, []byte) error {
	return sourceagentsecret.ErrUnsupportedPlatform
}
func (unsupportedSecretStore) Delete(context.Context, string) error {
	return sourceagentsecret.ErrUnsupportedPlatform
}
