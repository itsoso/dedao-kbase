//go:build !darwin

package app

func newSourceAgentUpdateBridgeStorage(string, string) (sourceAgentUpdateBridgeStorage, error) {
	return nil, errSourceAgentUpdateUnsupportedStorage
}
