package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

var errSourceAgentUpdaterUnsupportedPlatform = errors.New("source agent updater is unsupported on this platform")

type sourceAgentUpdaterInvocation struct {
	Check      bool
	WorkerType string
}

type sourceAgentProcessControl interface {
	Restart(context.Context) error
}

type sourceAgentLaunchctlRunner interface {
	Run(context.Context, string, ...string) error
}

type sourceAgentPlatformProcessConfig struct {
	WorkerType string
	Label      string
	Domain     string
	UID        int
	Runner     sourceAgentLaunchctlRunner
}

func main() {
	if err := runSourceAgentUpdater(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, sourceAgentUpdaterPublicError(err))
		os.Exit(1)
	}
}

func runSourceAgentUpdater(args []string, stdout, _ io.Writer) error {
	invocation, err := parseSourceAgentUpdaterArgs(args)
	if err != nil {
		return err
	}
	label, ok := sourceAgentLaunchAgentLabel(invocation.WorkerType)
	if !ok {
		return errors.New("unsupported worker type")
	}
	if _, err := newSourceAgentPlatformProcessControl(sourceAgentPlatformProcessConfig{
		WorkerType: invocation.WorkerType,
		Label:      label,
		Domain:     "gui",
		UID:        os.Getuid(),
	}); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "source agent updater configuration is valid")
	return nil
}

func parseSourceAgentUpdaterArgs(args []string) (sourceAgentUpdaterInvocation, error) {
	flags := flag.NewFlagSet("source-agent-updater", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var invocation sourceAgentUpdaterInvocation
	flags.BoolVar(&invocation.Check, "check", false, "validate the locally installed updater")
	flags.StringVar(&invocation.WorkerType, "worker-type", "", "installed worker type")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !invocation.Check {
		return sourceAgentUpdaterInvocation{}, errors.New("usage: source-agent-updater --check --worker-type <worker>")
	}
	if _, ok := sourceAgentLaunchAgentLabel(invocation.WorkerType); !ok {
		return sourceAgentUpdaterInvocation{}, errors.New("unsupported worker type")
	}
	return invocation, nil
}

func sourceAgentLaunchAgentLabel(workerType string) (string, bool) {
	switch workerType {
	case "wechat-worker":
		return "life.executor.kbase.source-agent", true
	case "wcplus-worker":
		return "life.executor.kbase.wcplus-agent", true
	default:
		return "", false
	}
}

func sourceAgentUpdaterPublicError(err error) string {
	if errors.Is(err, errSourceAgentUpdaterUnsupportedPlatform) {
		return "source agent updater is unsupported on this platform"
	}
	return "source agent updater configuration is invalid"
}
