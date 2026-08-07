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
	"time"

	"github.com/yann0917/dedao-gui/backend/app"
	"github.com/yann0917/dedao-gui/internal/sourceagentsecret"
)

const (
	sourceAgentKeychainService = sourceagentsecret.KeychainService
	keychainMasterKeyName      = "_storage-key-v1"
	keychainEnvelopePrefix     = "kbase:v1:"
	keychainMasterKeySize      = 32
	keychainCommandMaxOutput   = 64 << 10
)

type keychainCommandRunner func(context.Context, string, []string, []byte) ([]byte, error)
type keychainSecretStore struct {
	agentID string
	run     keychainCommandRunner
	prompt  keychainCommandRunner
	random  io.Reader
}

func newKeychainSecretStore(agentID string, runner keychainCommandRunner) app.SourceSecretStore {
	promptRunner := runner
	if runner == nil {
		runner = runKeychainCommand
		promptRunner = runKeychainPromptCommand
	}
	return &keychainSecretStore{
		agentID: strings.TrimSpace(agentID),
		run:     runner,
		prompt:  promptRunner,
		random:  rand.Reader,
	}
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
	_, addErr := s.prompt(ctx, "/usr/bin/security", []string{
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
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, keychainCommandMaxOutput+1))
	if readErr != nil || len(output) > keychainCommandMaxOutput {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf("keychain command output exceeded limit")
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	return output, nil
}

const keychainPromptExpectScript = `
log_user 0
set timeout 10
fconfigure stdin -translation lf -encoding utf-8

if {
    [gets stdin service] < 0 ||
    [gets stdin account] < 0 ||
    [gets stdin first] < 0 ||
    [gets stdin second] < 0 ||
    $service eq "" ||
    $account eq "" ||
    $first eq "" ||
    $first ne $second
} {
    exit 64
}

spawn -noecho /usr/bin/security add-generic-password -s $service -a $account -w
expect {
    -re {password data for new item:[[:space:]]*$} {
        send -- "$first\r"
    }
    timeout {
        exit 124
    }
    eof {
        set result [wait]
        exit [lindex $result 3]
    }
}
expect {
    -re {retype password for new item:[[:space:]]*$} {
        send -- "$second\r"
    }
    timeout {
        exit 124
    }
    eof {
        set result [wait]
        exit [lindex $result 3]
    }
}
expect {
    eof {
        set result [wait]
        exit [lindex $result 3]
    }
    timeout {
        exit 124
    }
}
`

func runKeychainPromptCommand(ctx context.Context, path string, args []string, input []byte) ([]byte, error) {
	if path != "/usr/bin/security" ||
		len(args) != 6 ||
		args[0] != "add-generic-password" ||
		args[1] != "-s" ||
		args[2] != sourceAgentKeychainService ||
		args[3] != "-a" ||
		args[4] == "" ||
		strings.ContainsAny(args[4], "\r\n") ||
		args[5] != "-w" {
		return nil, fmt.Errorf("unsupported keychain prompt command")
	}
	promptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	promptInput := make([]byte, 0, len(sourceAgentKeychainService)+len(args[4])+len(input)+2)
	promptInput = append(promptInput, sourceAgentKeychainService...)
	promptInput = append(promptInput, '\n')
	promptInput = append(promptInput, args[4]...)
	promptInput = append(promptInput, '\n')
	promptInput = append(promptInput, input...)

	cmd := exec.CommandContext(promptCtx, "/usr/bin/expect", "-c", keychainPromptExpectScript)
	cmd.Stdin = bytes.NewReader(promptInput)
	return cmd.Output()
}
