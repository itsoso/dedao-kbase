package sourceagentsecret

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestResolveTransportTokenPrefersExplicitEnvironment(t *testing.T) {
	called := false
	token, err := ResolveTransportToken(context.Background(), "env-token", true, func(context.Context) (string, error) {
		called = true
		return "keychain-token", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if token != "env-token" || called {
		t.Fatalf("token=%q loader_called=%t", token, called)
	}
}

func TestResolveTransportTokenLoadsFixedPlatformSecretWhenEnvironmentMissing(t *testing.T) {
	token, err := ResolveTransportToken(context.Background(), "", false, func(context.Context) (string, error) {
		return "keychain-token", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if token != "keychain-token" {
		t.Fatalf("token=%q", token)
	}
}

func TestResolveTransportTokenFailsClosedWithoutLeakingInputOrRawErrors(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		provided bool
		loader   Loader
	}{
		{name: "explicit empty", value: "", provided: true, loader: func(context.Context) (string, error) { return "fallback", nil }},
		{name: "explicit whitespace", value: "  ", provided: true, loader: func(context.Context) (string, error) { return "fallback", nil }},
		{name: "stored whitespace", loader: func(context.Context) (string, error) { return "\t", nil }},
		{name: "explicit oversize", value: strings.Repeat("x", MaxTransportTokenBytes+1), provided: true, loader: func(context.Context) (string, error) { return "fallback", nil }},
		{name: "stored oversize", loader: func(context.Context) (string, error) { return strings.Repeat("x", MaxTransportTokenBytes+1), nil }},
		{name: "stored missing", loader: func(context.Context) (string, error) { return "", ErrTransportTokenUnavailable }},
		{name: "unsupported", loader: func(context.Context) (string, error) { return "", ErrUnsupportedPlatform }},
		{name: "raw loader error", loader: func(context.Context) (string, error) { return "", errors.New("raw /Users/private token-sentinel") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveTransportToken(context.Background(), test.value, test.provided, test.loader)
			if !errors.Is(err, ErrTransportTokenUnavailable) {
				t.Fatalf("error=%v", err)
			}
			message := err.Error()
			for _, forbidden := range []string{"fallback", "/Users/", "token-sentinel", "unsupported"} {
				if strings.Contains(message, forbidden) {
					t.Fatalf("error leaked %q: %v", forbidden, err)
				}
			}
		})
	}
}

func TestKeychainTransportTokenUsesFixedServiceAndAccountWithoutSecretArguments(t *testing.T) {
	if runtime.GOOS != "darwin" {
		_, err := LoadTransportToken(context.Background())
		if !errors.Is(err, ErrUnsupportedPlatform) {
			t.Fatalf("error=%v", err)
		}
		return
	}
	var path string
	var args []string
	var input []byte
	token, err := loadTransportToken(context.Background(), func(_ context.Context, gotPath string, gotArgs []string, gotInput []byte) ([]byte, error) {
		path = gotPath
		args = append([]string(nil), gotArgs...)
		input = append([]byte(nil), gotInput...)
		return []byte("stored-token\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if token != "stored-token" {
		t.Fatalf("token=%q", token)
	}
	if path != "/usr/bin/security" {
		t.Fatalf("path=%q", path)
	}
	wantArgs := "find-generic-password -s life.executor.kbase.source-agent -a transport-token -w"
	if strings.Join(args, " ") != wantArgs {
		t.Fatalf("args=%q", strings.Join(args, " "))
	}
	if len(input) != 0 || strings.Contains(strings.Join(args, " "), token) {
		t.Fatalf("secret reached command input or arguments: args=%q input=%q", args, input)
	}
}

func TestKeychainTransportTokenRejectsOversizeRunnerOutput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin Keychain runner")
	}
	_, err := loadTransportToken(context.Background(), func(context.Context, string, []string, []byte) ([]byte, error) {
		return []byte(strings.Repeat("x", MaxTransportTokenBytes+1) + "\n"), nil
	})
	if !errors.Is(err, ErrTransportTokenUnavailable) {
		t.Fatalf("error=%v", err)
	}
}
