package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

var errSourceAgentUpdaterUnsupportedPlatform = errors.New("source agent updater is unsupported on this platform")
var errSourceAgentPendingExecutionUnavailable = errors.New("source agent pending update execution is unavailable")

const (
	sourceAgentUpdaterVersion         = "0.2.0"
	sourceAgentUpdaterProtocolVersion = "2026-08-01"
)

var sourceAgentUpdaterRevision = "0000000000000000000000000000000000000000"

type sourceAgentUpdaterInvocation struct {
	BuildInfo      bool
	Check          bool
	CheckConfig    bool
	CheckUninstall bool
	InstallConfig  bool
	RunPending     bool
	HoldLifecycle  bool
	WorkerType     string
}

type sourceAgentProcessControl interface {
	StartUpdater(context.Context) error
	RestartWorker(context.Context) error
}

type sourceAgentLaunchctlRunner interface {
	Run(context.Context, string, ...string) error
}

type sourceAgentPlatformProcessConfig struct {
	WorkerType string
	UID        int
	Runner     sourceAgentLaunchctlRunner
}

type sourceAgentLaunchAgentIdentity struct {
	Worker  string
	Updater string
}

type sourceAgentPendingRunner func(context.Context, string, sourceAgentProcessControl) error

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runSourceAgentUpdaterWithContext(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, sourceAgentUpdaterPublicError(err))
		os.Exit(1)
	}
}

func runSourceAgentUpdater(args []string, stdout, _ io.Writer) error {
	return runSourceAgentUpdaterWithContext(context.Background(), args, stdout, io.Discard)
}

func runSourceAgentUpdaterWithContext(ctx context.Context, args []string, stdout, _ io.Writer) error {
	return runSourceAgentUpdaterWithContextAndPendingRunner(
		ctx, args, stdout, io.Discard, runSourceAgentPendingFromProtectedState,
	)
}

func runSourceAgentUpdaterWithPendingRunner(args []string, stdout, _ io.Writer, runPending sourceAgentPendingRunner) error {
	return runSourceAgentUpdaterWithContextAndPendingRunner(
		context.Background(), args, stdout, io.Discard, runPending,
	)
}

func runSourceAgentUpdaterWithContextAndPendingRunner(
	ctx context.Context,
	args []string,
	stdout, _ io.Writer,
	runPending sourceAgentPendingRunner,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	invocation, err := parseSourceAgentUpdaterArgs(args)
	if err != nil {
		return err
	}
	if invocation.BuildInfo {
		return writeSourceAgentUpdaterBuildInfo(stdout, invocation.WorkerType)
	}
	if invocation.HoldLifecycle {
		executable, err := sourceAgentUpdaterExecutable()
		if err != nil {
			return errors.New("source agent updater executable is unavailable")
		}
		return runSourceAgentLifecycleHolder(ctx, filepath.Dir(executable), os.Stdin, stdout)
	}
	control, err := newSourceAgentPlatformProcessControl(sourceAgentPlatformProcessConfig{
		WorkerType: invocation.WorkerType,
		UID:        os.Getuid(),
	})
	if err != nil {
		return err
	}
	if invocation.Check {
		fmt.Fprintln(stdout, "source agent updater configuration is valid")
		return nil
	}
	if invocation.InstallConfig {
		return installSourceAgentProtectedConfig()
	}
	if invocation.CheckConfig {
		return checkSourceAgentProtectedConfig()
	}
	if invocation.CheckUninstall {
		return checkSourceAgentUninstallSafe()
	}
	if runPending == nil {
		return errSourceAgentPendingExecutionUnavailable
	}
	return runPending(ctx, invocation.WorkerType, control)
}

func parseSourceAgentUpdaterArgs(args []string) (sourceAgentUpdaterInvocation, error) {
	var invocation sourceAgentUpdaterInvocation
	workerTypeSeen := false
	modeSeen := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			if modeSeen {
				return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
			}
			modeSeen = true
			invocation.Check = true
		case "--build-info":
			if modeSeen {
				return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
			}
			modeSeen = true
			invocation.BuildInfo = true
		case "--check-config":
			if modeSeen {
				return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
			}
			modeSeen = true
			invocation.CheckConfig = true
		case "--check-uninstall":
			if modeSeen {
				return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
			}
			modeSeen = true
			invocation.CheckUninstall = true
		case "--install-config":
			if modeSeen {
				return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
			}
			modeSeen = true
			invocation.InstallConfig = true
		case "--run-pending":
			if modeSeen {
				return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
			}
			modeSeen = true
			invocation.RunPending = true
		case "--hold-lifecycle-lock":
			if modeSeen {
				return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
			}
			modeSeen = true
			invocation.HoldLifecycle = true
		case "--worker-type":
			if workerTypeSeen || i+1 >= len(args) {
				return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
			}
			i++
			invocation.WorkerType = args[i]
			workerTypeSeen = true
		default:
			return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
		}
	}
	if !modeSeen || !workerTypeSeen {
		return sourceAgentUpdaterInvocation{}, sourceAgentUpdaterUsageError()
	}
	if _, ok := sourceAgentLaunchAgentLabels(invocation.WorkerType); !ok {
		return sourceAgentUpdaterInvocation{}, errors.New("unsupported worker type")
	}
	return invocation, nil
}

func sourceAgentUpdaterUsageError() error {
	return errors.New("usage: source-agent-updater (--build-info|--check|--check-config|--check-uninstall|--install-config|--run-pending|--hold-lifecycle-lock) --worker-type <worker>")
}

func writeSourceAgentUpdaterBuildInfo(output io.Writer, workerType string) error {
	if output == nil || !validSourceAgentUpdaterCompiledRevision(sourceAgentUpdaterRevision) {
		return errors.New("source agent updater build identity is invalid")
	}
	return json.NewEncoder(output).Encode(map[string]string{
		"worker_type": workerType, "version": sourceAgentUpdaterVersion,
		"protocol_version": sourceAgentUpdaterProtocolVersion, "platform": runtime.GOOS,
		"architecture": runtime.GOARCH, "revision": sourceAgentUpdaterRevision,
	})
}

func validSourceAgentUpdaterCompiledRevision(value string) bool {
	if (len(value) != 40 && len(value) != 64) || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sourceAgentLaunchAgentLabels(workerType string) (sourceAgentLaunchAgentIdentity, bool) {
	switch workerType {
	case "wechat-worker":
		return sourceAgentLaunchAgentIdentity{
			Worker:  "life.executor.kbase.source-agent",
			Updater: "life.executor.kbase.source-agent.updater",
		}, true
	case "wcplus-worker":
		return sourceAgentLaunchAgentIdentity{
			Worker:  "life.executor.kbase.wcplus-agent",
			Updater: "life.executor.kbase.wcplus-agent.updater",
		}, true
	default:
		return sourceAgentLaunchAgentIdentity{}, false
	}
}

func sourceAgentUpdaterPublicError(err error) string {
	if errors.Is(err, errSourceAgentUpdaterUnsupportedPlatform) {
		return "source agent updater is unsupported on this platform"
	}
	return "source agent updater configuration is invalid"
}
