//go:build !darwin && !linux && !windows

package app

func NewOSSourceAgentUpdateFileSystem(string) (SourceAgentUpdateFileSystem, error) {
	return nil, errSourceAgentUpdateUnsupportedStorage
}
