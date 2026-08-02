//go:build darwin

package sourceagentsecret

import (
	"bytes"
	"context"
	"os/exec"
)

func loadTransportToken(ctx context.Context, runner keychainCommandRunner) (string, error) {
	if runner == nil {
		runner = runKeychainCommand
	}
	output, err := runner(ctx, "/usr/bin/security", []string{
		"find-generic-password",
		"-s", KeychainService,
		"-a", TransportTokenAccount,
		"-w",
	}, nil)
	if err != nil {
		return "", ErrTransportTokenUnavailable
	}
	output = bytes.TrimSuffix(output, []byte("\n"))
	output = bytes.TrimSuffix(output, []byte("\r"))
	value := string(output)
	if !validTransportToken(value) {
		return "", ErrTransportTokenUnavailable
	}
	return value, nil
}

func runKeychainCommand(ctx context.Context, path string, args []string, input []byte) ([]byte, error) {
	command := exec.CommandContext(ctx, path, args...)
	command.Stdin = bytes.NewReader(input)
	return command.Output()
}
