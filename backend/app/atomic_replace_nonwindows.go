//go:build !windows

package app

import "os"

func replaceFileAtomically(source, target string) error {
	return os.Rename(source, target)
}
