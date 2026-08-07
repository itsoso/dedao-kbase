//go:build !darwin && !linux && !windows

package app

func openSourceAgentUpdateDirectory(string) (sourceAgentUpdateDirectory, error) {
	return nil, errSourceAgentUpdateUnsupportedStorage
}
