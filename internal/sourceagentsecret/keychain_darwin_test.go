//go:build darwin

package sourceagentsecret

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestKeychainCommandKillsAndWaitsAfterBoundedOutput(t *testing.T) {
	started := time.Now()
	_, err := runKeychainCommand(context.Background(), "/bin/sh", []string{
		"-c", "head -c 8192 /dev/zero; sleep 2",
	}, nil)
	if !errors.Is(err, ErrTransportTokenUnavailable) {
		t.Fatalf("error=%v", err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("bounded command took %s", elapsed)
	}
}
