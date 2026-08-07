//go:build !darwin

package app

import "errors"

func NewSourceAgentUpdaterActivator(string, int, SourceAgentLaunchctlRunner) (SourceAgentUpdaterActivator, error) {
	return nil, errors.New("source agent updater activation is unsupported on this platform")
}
