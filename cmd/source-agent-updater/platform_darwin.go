//go:build darwin

package main

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
)

const sourceAgentLaunchctlPath = "/bin/launchctl"

type sourceAgentDarwinProcessControl struct {
	target string
	runner sourceAgentLaunchctlRunner
}

type sourceAgentOSLaunchctlRunner struct{}

func (sourceAgentOSLaunchctlRunner) Run(ctx context.Context, path string, args ...string) error {
	if path != sourceAgentLaunchctlPath {
		return errors.New("invalid launchctl executable")
	}
	if err := exec.CommandContext(ctx, path, args...).Run(); err != nil {
		return errors.New("launch agent operation failed")
	}
	return nil
}

func newSourceAgentPlatformProcessControl(config sourceAgentPlatformProcessConfig) (sourceAgentProcessControl, error) {
	expectedLabel, ok := sourceAgentLaunchAgentLabel(config.WorkerType)
	if !ok || config.Label != expectedLabel || config.Domain != "gui" || config.UID < 0 {
		return nil, errors.New("invalid fixed launch agent identity")
	}
	runner := config.Runner
	if runner == nil {
		runner = sourceAgentOSLaunchctlRunner{}
	}
	return &sourceAgentDarwinProcessControl{
		target: "gui/" + strconv.Itoa(config.UID) + "/" + config.Label,
		runner: runner,
	}, nil
}

func (p *sourceAgentDarwinProcessControl) Restart(ctx context.Context) error {
	if err := p.runner.Run(ctx, sourceAgentLaunchctlPath, "kickstart", "-k", p.target); err != nil {
		return errors.New("launch agent restart failed")
	}
	return nil
}
