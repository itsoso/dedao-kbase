//go:build darwin

package app

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
)

const sourceAgentLaunchctlExecutable = "/bin/launchctl"

type sourceAgentUpdaterLaunchctlActivator struct {
	target string
	runner SourceAgentLaunchctlRunner
}

type sourceAgentOSLaunchctlRunner struct{}

func (sourceAgentOSLaunchctlRunner) Run(ctx context.Context, path string, args ...string) error {
	if path != sourceAgentLaunchctlExecutable {
		return errors.New("invalid launchctl executable")
	}
	if err := exec.CommandContext(ctx, path, args...).Run(); err != nil {
		return errors.New("updater launch agent operation failed")
	}
	return nil
}

func NewSourceAgentUpdaterActivator(
	workerType string,
	uid int,
	runner SourceAgentLaunchctlRunner,
) (SourceAgentUpdaterActivator, error) {
	label, ok := sourceAgentUpdaterLaunchAgentLabel(workerType)
	if !ok || uid < 0 {
		return nil, errors.New("invalid fixed updater launch agent identity")
	}
	if runner == nil {
		runner = sourceAgentOSLaunchctlRunner{}
	}
	return &sourceAgentUpdaterLaunchctlActivator{
		target: "gui/" + strconv.Itoa(uid) + "/" + label,
		runner: runner,
	}, nil
}

func (a *sourceAgentUpdaterLaunchctlActivator) StartUpdater(ctx context.Context) error {
	if err := a.runner.Run(ctx, sourceAgentLaunchctlExecutable, "kickstart", a.target); err != nil {
		return errors.New("updater launch agent start failed")
	}
	return nil
}

func sourceAgentUpdaterLaunchAgentLabel(workerType string) (string, bool) {
	switch workerType {
	case "wechat-worker":
		return "life.executor.kbase.source-agent.updater", true
	case "wcplus-worker":
		return "life.executor.kbase.wcplus-agent.updater", true
	case "chatlog-worker":
		return "life.executor.kbase.chatlog-agent.updater", true
	default:
		return "", false
	}
}
