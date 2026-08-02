//go:build windows

package app

func openSourceAgentUpdateDirectory(string) (sourceAgentUpdateDirectory, error) {
	return nil, errSourceAgentUpdateUnsupportedStorage
}
