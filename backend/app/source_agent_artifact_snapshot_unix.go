//go:build darwin || linux

package app

import "os"

func unlinkSourceAgentArtifactSnapshot(path string) bool {
	return os.Remove(path) == nil
}
