//go:build !darwin

package main

import (
	"context"
	"io"
)

const (
	lifecycleLockName        = ".source-agent-lifecycle.lock"
	lifecycleMaintenanceName = ".managed-worker-maintenance"
)

func runSourceAgentLifecycleHolder(context.Context, string, io.Reader, io.Writer) error {
	return errSourceAgentUpdaterUnsupportedPlatform
}
