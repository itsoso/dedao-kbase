//go:build !darwin

package main

func newSourceAgentPlatformProcessControl(sourceAgentPlatformProcessConfig) (sourceAgentProcessControl, error) {
	return nil, errSourceAgentUpdaterUnsupportedPlatform
}
