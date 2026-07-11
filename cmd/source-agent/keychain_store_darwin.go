//go:build darwin

package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yann0917/dedao-gui/backend/app"
)

const sourceAgentKeychainService = "life.executor.kbase.source-agent"

type keychainCommandRunner func(context.Context, string, []string, []byte) ([]byte, error)
type keychainSecretStore struct {
	agentID string
	run     keychainCommandRunner
}

func newKeychainSecretStore(agentID string, runner keychainCommandRunner) app.SourceSecretStore {
	if runner == nil {
		runner = runKeychainCommand
	}
	return &keychainSecretStore{agentID: strings.TrimSpace(agentID), run: runner}
}
func (s *keychainSecretStore) account(key string) (string, error) {
	if s.agentID == "" || strings.ContainsAny(key, "/\\\n\r") || strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("invalid keychain account")
	}
	return s.agentID + ":" + key, nil
}
func (s *keychainSecretStore) Load(ctx context.Context, key string) ([]byte, error) {
	account, err := s.account(key)
	if err != nil {
		return nil, err
	}
	out, err := s.run(ctx, "/usr/bin/security", []string{"find-generic-password", "-s", sourceAgentKeychainService, "-a", account, "-w"}, nil)
	if err != nil {
		return nil, app.ErrSourceSecretNotFound
	}
	return bytes.TrimSuffix(out, []byte("\n")), nil
}
func (s *keychainSecretStore) Save(ctx context.Context, key string, value []byte) error {
	account, err := s.account(key)
	if err != nil {
		return err
	}
	_, err = s.run(ctx, "/usr/bin/security", []string{"add-generic-password", "-U", "-s", sourceAgentKeychainService, "-a", account, "-w"}, value)
	if err != nil {
		return fmt.Errorf("save source secret in keychain failed")
	}
	return nil
}
func (s *keychainSecretStore) Delete(ctx context.Context, key string) error {
	account, err := s.account(key)
	if err != nil {
		return err
	}
	_, err = s.run(ctx, "/usr/bin/security", []string{"delete-generic-password", "-s", sourceAgentKeychainService, "-a", account}, nil)
	if err != nil {
		return app.ErrSourceSecretNotFound
	}
	return nil
}
func runKeychainCommand(ctx context.Context, path string, args []string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = bytes.NewReader(input)
	return cmd.Output()
}
