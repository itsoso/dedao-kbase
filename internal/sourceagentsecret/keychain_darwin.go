//go:build darwin

package sourceagentsecret

import (
	"bytes"
	"context"
	"io"
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
	if len(output) > MaxTransportTokenBytes+2 {
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
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, ErrTransportTokenUnavailable
	}
	if err := command.Start(); err != nil {
		return nil, ErrTransportTokenUnavailable
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, int64(MaxTransportTokenBytes+3)))
	if readErr != nil || len(output) > MaxTransportTokenBytes+2 {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, ErrTransportTokenUnavailable
	}
	if err := command.Wait(); err != nil {
		return nil, ErrTransportTokenUnavailable
	}
	return output, nil
}
