//go:build darwin || linux

package app

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func validateSourceAgentArtifactRoot(root string) error {
	fd, err := unix.Open(filepath.Clean(root), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return ErrSourceAgentArtifactCatalogInvalid
	}
	return unix.Close(fd)
}

// openSourceAgentArtifactRelative pins the configured root and walks every
// component through directory file descriptors. O_NOFOLLOW is applied at
// every step, so a concurrent path rename cannot redirect the final open.
func openSourceAgentArtifactRelative(root string, parts []string) (*os.File, error) {
	if len(parts) == 0 {
		return nil, ErrSourceAgentArtifactIntegrity
	}
	current, err := unix.Open(filepath.Clean(root), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrSourceAgentArtifactIntegrity
	}
	for index, part := range parts {
		if part == "" || part == "." || part == ".." || stringsContainsPathSeparator(part) {
			unix.Close(current)
			return nil, ErrSourceAgentArtifactIntegrity
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if index < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		} else {
			flags |= unix.O_NONBLOCK
		}
		next, openErr := unix.Openat(current, part, flags, 0)
		closeErr := unix.Close(current)
		if openErr != nil {
			return nil, ErrSourceAgentArtifactIntegrity
		}
		if closeErr != nil {
			unix.Close(next)
			return nil, ErrSourceAgentArtifactIntegrity
		}
		current = next
	}
	file := os.NewFile(uintptr(current), "source-agent-artifact")
	if file == nil {
		unix.Close(current)
		return nil, ErrSourceAgentArtifactIntegrity
	}
	return file, nil
}

func stringsContainsPathSeparator(value string) bool {
	for _, character := range value {
		if character == '/' || character == '\\' {
			return true
		}
	}
	return false
}
