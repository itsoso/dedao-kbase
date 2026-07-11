//go:build darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestKeychainSecretUsesStdinAndRedactsErrors(t *testing.T) {
	secret := []byte("never-log-this")
	var stdin []byte
	store := newKeychainSecretStore("agent-a", func(_ context.Context, path string, args []string, input []byte) ([]byte, error) {
		if path != "/usr/bin/security" {
			t.Fatalf("path=%q", path)
		}
		stdin = append([]byte(nil), input...)
		for _, arg := range args {
			if strings.Contains(arg, string(secret)) {
				t.Fatalf("secret leaked in args")
			}
		}
		return nil, errors.New("command failed: never-log-this")
	})
	err := store.Save(context.Background(), "wechat-session", secret)
	if !bytes.Equal(stdin, secret) || err == nil || strings.Contains(err.Error(), string(secret)) {
		t.Fatalf("stdin=%q err=%v", stdin, err)
	}
}
