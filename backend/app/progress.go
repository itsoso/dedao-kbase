package app

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func emitProgress(ctx context.Context, event string, progress Progress) bool {
	if ctx == nil || ctx.Value("events") == nil {
		return false
	}
	runtime.EventsEmit(ctx, event, progress)
	return true
}
