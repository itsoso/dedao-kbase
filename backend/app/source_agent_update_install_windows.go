//go:build windows

package app

func NewOSSourceAgentUpdateFileSystem(string) (SourceAgentUpdateFileSystem, error) {
	return nil, errSourceAgentUpdateUnsupportedStorage
}
