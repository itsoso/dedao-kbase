package app

import (
	"context"
	"testing"
)

func TestEmitProgressSkipsNonWailsContext(t *testing.T) {
	emitted := emitProgress(context.Background(), "ebookDownload", Progress{Value: "server job"})
	if emitted {
		t.Fatal("emitProgress emitted for a non-Wails context")
	}
}
