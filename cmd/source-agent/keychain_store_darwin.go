//go:build darwin

package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/yann0917/dedao-gui/backend/app"
)

const (
	sourceAgentKeychainService = "life.executor.kbase.source-agent"
	keychainMasterKeyName      = "_storage-key-v1"
	keychainEnvelopePrefix     = "kbase:v1:"
	keychainMasterKeySize      = 32
)

type keychainCommandRunner func(context.Context, string, []string, []byte) ([]byte, error)
type keychainSecretStore struct {
	agentID string
	run     keychainCommandRunner
	random  io.Reader
}

func newKeychainSecretStore(agentID string, runner keychainCommandRunner) app.SourceSecretStore {
	if runner == nil {
		runner = runKeychainCommand
	}
	return &keychainSecretStore{agentID: strings.TrimSpace(agentID), run: runner, random: rand.Reader}
}

func (s *keychainSecretStore) account(key string) (string, error) {
	if s.agentID == "" || strings.ContainsAny(s.agentID, "\x00\n\r") || strings.ContainsAny(key, "/\\\x00\n\r") || strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("invalid keychain account")
	}
	return s.agentID + ":" + key, nil
}

func (s *keychainSecretStore) Load(ctx context.Context, key string) ([]byte, error) {
	account, err := s.account(key)
	if err != nil {
		return nil, err
	}
	value, err := s.loadRaw(ctx, account)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(value, []byte(keychainEnvelopePrefix)) {
		return value, nil
	}
	masterKey, err := s.masterKey(ctx)
	if err != nil {
		return nil, err
	}
	plaintext, err := openKeychainEnvelope(masterKey, account, value)
	if err != nil {
		return nil, fmt.Errorf("decrypt source secret from keychain failed")
	}
	return plaintext, nil
}

func (s *keychainSecretStore) Save(ctx context.Context, key string, value []byte) error {
	account, err := s.account(key)
	if err != nil {
		return err
	}
	if len(value) == 0 || bytes.ContainsAny(value, "\r\n") {
		return fmt.Errorf("source secret must be non-empty single-line data")
	}
	masterKey, err := s.masterKey(ctx)
	if err != nil {
		return err
	}
	envelope, err := sealKeychainEnvelope(masterKey, account, value, s.random)
	if err != nil {
		return fmt.Errorf("encrypt source secret for keychain failed")
	}
	_, err = s.run(ctx, "/usr/bin/security", []string{
		"add-generic-password", "-U",
		"-s", sourceAgentKeychainService,
		"-a", account,
		"-w", string(envelope),
	}, nil)
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
	_, err = s.run(ctx, "/usr/bin/security", []string{
		"delete-generic-password",
		"-s", sourceAgentKeychainService,
		"-a", account,
	}, nil)
	if err != nil {
		return app.ErrSourceSecretNotFound
	}
	return nil
}

func (s *keychainSecretStore) loadRaw(ctx context.Context, account string) ([]byte, error) {
	out, err := s.run(ctx, "/usr/bin/security", []string{
		"find-generic-password",
		"-s", sourceAgentKeychainService,
		"-a", account,
		"-w",
	}, nil)
	if err != nil {
		return nil, app.ErrSourceSecretNotFound
	}
	return bytes.TrimSuffix(out, []byte("\n")), nil
}

func (s *keychainSecretStore) masterKey(ctx context.Context) ([]byte, error) {
	account, err := s.account(keychainMasterKeyName)
	if err != nil {
		return nil, err
	}
	if encoded, loadErr := s.loadRaw(ctx, account); loadErr == nil {
		return decodeKeychainMasterKey(encoded)
	}
	key := make([]byte, keychainMasterKeySize)
	if _, err = io.ReadFull(s.random, key); err != nil {
		return nil, fmt.Errorf("generate keychain encryption key failed")
	}
	encoded := []byte(base64.RawStdEncoding.EncodeToString(key))
	promptInput := make([]byte, 0, len(encoded)*2+2)
	promptInput = append(promptInput, encoded...)
	promptInput = append(promptInput, '\n')
	promptInput = append(promptInput, encoded...)
	promptInput = append(promptInput, '\n')
	_, addErr := s.run(ctx, "/usr/bin/security", []string{
		"add-generic-password",
		"-s", sourceAgentKeychainService,
		"-a", account,
		"-w",
	}, promptInput)
	if addErr == nil {
		return key, nil
	}
	existing, loadErr := s.loadRaw(ctx, account)
	if loadErr != nil {
		return nil, fmt.Errorf("save keychain encryption key failed")
	}
	return decodeKeychainMasterKey(existing)
}

func decodeKeychainMasterKey(value []byte) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(value)))
	if err != nil || len(decoded) != keychainMasterKeySize {
		return nil, fmt.Errorf("stored keychain encryption key is invalid")
	}
	return decoded, nil
}

func sealKeychainEnvelope(masterKey []byte, account string, plaintext []byte, random io.Reader) ([]byte, error) {
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(random, nonce); err != nil {
		return nil, err
	}
	payload := append([]byte{1}, nonce...)
	payload = aead.Seal(payload, nonce, plaintext, []byte(account))
	encoded := base64.RawStdEncoding.EncodeToString(payload)
	return []byte(keychainEnvelopePrefix + encoded), nil
}

func openKeychainEnvelope(masterKey []byte, account string, envelope []byte) ([]byte, error) {
	encoded := strings.TrimPrefix(string(envelope), keychainEnvelopePrefix)
	payload, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(payload) < 2 || payload[0] != 1 {
		return nil, fmt.Errorf("invalid keychain envelope")
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(payload) <= 1+aead.NonceSize() {
		return nil, fmt.Errorf("invalid keychain envelope")
	}
	nonce := payload[1 : 1+aead.NonceSize()]
	ciphertext := payload[1+aead.NonceSize():]
	return aead.Open(nil, nonce, ciphertext, []byte(account))
}

func runKeychainCommand(ctx context.Context, path string, args []string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = bytes.NewReader(input)
	return cmd.Output()
}
