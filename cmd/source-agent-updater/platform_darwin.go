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
	workerTarget  string
	updaterTarget string
	runner        sourceAgentLaunchctlRunner
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
	labels, ok := sourceAgentLaunchAgentLabels(config.WorkerType)
	if !ok || config.UID < 0 {
		return nil, errors.New("invalid fixed launch agent identity")
	}
	runner := config.Runner
	if runner == nil {
		runner = sourceAgentOSLaunchctlRunner{}
	}
	return &sourceAgentDarwinProcessControl{
		workerTarget:  "gui/" + strconv.Itoa(config.UID) + "/" + labels.Worker,
		updaterTarget: "gui/" + strconv.Itoa(config.UID) + "/" + labels.Updater,
		runner:        runner,
	}, nil
}

func (p *sourceAgentDarwinProcessControl) StartUpdater(ctx context.Context) error {
	if err := p.runner.Run(ctx, sourceAgentLaunchctlPath, "kickstart", p.updaterTarget); err != nil {
		return errors.New("launch agent start failed")
	}
	return nil
}

func (p *sourceAgentDarwinProcessControl) RestartWorker(ctx context.Context) error {
	if err := p.runner.Run(ctx, sourceAgentLaunchctlPath, "kickstart", "-k", p.workerTarget); err != nil {
		return errors.New("launch agent restart failed")
	}
	return nil
}
