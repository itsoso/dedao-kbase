//go:build darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestKeychainSecretRoundTripsLargeValue(t *testing.T) {
	agentID := fmt.Sprintf("source-agent-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	store := newKeychainSecretStore(agentID, nil)
	defer func() { _ = store.Delete(context.Background(), "large-secret") }()
	defer func() { _ = store.Delete(context.Background(), keychainMasterKeyName) }()

	secret := bytes.Repeat([]byte("0123456789abcdef"), 256)
	if err := store.Save(context.Background(), "large-secret", secret); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "large-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, secret) {
		t.Fatalf("loaded %d bytes, want %d", len(loaded), len(secret))
	}
}

type fakeSecurityRunner struct {
	items            map[string][]byte
	arguments        [][]string
	failEnvelopeSave bool
}

func (f *fakeSecurityRunner) run(_ context.Context, _ string, args []string, input []byte) ([]byte, error) {
	if f.items == nil {
		f.items = map[string][]byte{}
	}
	f.arguments = append(f.arguments, append([]string(nil), args...))
	account := securityArgument(args, "-a")
	switch args[0] {
	case "find-generic-password":
		value, ok := f.items[account]
		if !ok {
			return nil, errors.New("not found")
		}
		return append(append([]byte(nil), value...), '\n'), nil
	case "add-generic-password":
		if f.failEnvelopeSave && hasSecurityArgument(args, "-U") {
			return nil, errors.New("command failed: never-log-this")
		}
		value := securityArgument(args, "-w")
		if value == "" {
			value = strings.SplitN(string(input), "\n", 2)[0]
		}
		if _, exists := f.items[account]; exists && !hasSecurityArgument(args, "-U") {
			return nil, errors.New("duplicate")
		}
		f.items[account] = []byte(value)
		return nil, nil
	case "delete-generic-password":
		if _, ok := f.items[account]; !ok {
			return nil, errors.New("not found")
		}
		delete(f.items, account)
		return nil, nil
	default:
		return nil, errors.New("unexpected command")
	}
}

func securityArgument(args []string, name string) string {
	for index, arg := range args {
		if arg == name {
			if index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
				return args[index+1]
			}
			return ""
		}
	}
	return ""
}

func hasSecurityArgument(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func TestKeychainSecretEncryptsLargeValueOutsideCommandArguments(t *testing.T) {
	runner := &fakeSecurityRunner{}
	store := newKeychainSecretStore("agent-a", runner.run)
	secret := bytes.Repeat([]byte("never-log-this"), 128)
	if err := store.Save(context.Background(), "wechat-session", secret); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "wechat-session")
	if err != nil || !bytes.Equal(loaded, secret) {
		t.Fatalf("loaded=%d error=%v", len(loaded), err)
	}
	for _, args := range runner.arguments {
		if strings.Contains(strings.Join(args, " "), string(secret)) || strings.Contains(strings.Join(args, " "), "never-log-this") {
			t.Fatal("plaintext secret leaked in command arguments")
		}
	}
}

func TestKeychainSecretSaveRedactsCommandErrors(t *testing.T) {
	runner := &fakeSecurityRunner{failEnvelopeSave: true}
	store := newKeychainSecretStore("agent-a", runner.run)
	err := store.Save(context.Background(), "wechat-session", []byte("never-log-this"))
	if err == nil || strings.Contains(err.Error(), "never-log-this") {
		t.Fatalf("error=%v", err)
	}
}

func TestKeychainSecretLoadsLegacyPlaintext(t *testing.T) {
	runner := &fakeSecurityRunner{items: map[string][]byte{"agent-a:transport-token": []byte("legacy-token")}}
	store := newKeychainSecretStore("agent-a", runner.run)
	value, err := store.Load(context.Background(), "transport-token")
	if err != nil || string(value) != "legacy-token" {
		t.Fatalf("value=%q error=%v", value, err)
	}
}

func TestKeychainSecretRejectsLineBreaks(t *testing.T) {
	runner := &fakeSecurityRunner{}
	store := newKeychainSecretStore("agent-a", runner.run)
	if err := store.Save(context.Background(), "wechat-session", []byte("line-1\nline-2")); err == nil {
		t.Fatal("Save accepted multiline secret")
	}
	if len(runner.arguments) != 0 {
		t.Fatal("security command received invalid secret")
	}
}
