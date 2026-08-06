//go:build !darwin

package main

import (
	"context"
	"io"
)

func runSourceAgentLifecycleHolder(context.Context, string, io.Reader, io.Writer) error {
	return errSourceAgentUpdaterUnsupportedPlatform
}
