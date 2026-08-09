package utils

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type cancelAfterErrChecksContext struct {
	context.Context
	checks   int
	cancelAt int
}

func (c *cancelAfterErrChecksContext) Err() error {
	c.checks++
	if c.checks >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestOneByOneHtmlContextChecksCancellationInsideContentLoop(t *testing.T) {
	ctx := &cancelAfterErrChecksContext{Context: context.Background(), cancelAt: 2}
	_, _, err := OneByOneHtmlContext(ctx, eBookTypeHtml, 0, &SvgContent{Contents: []string{"unused", "unused"}}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OneByOneHtmlContext error=%v, want context canceled", err)
	}
}

func TestPdfOptionGenPdfContextTerminatesExternalProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	script := filepath.Join(t.TempDir(), "wkhtmltopdf")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := WkToPdfDir
	WkToPdfDir = script
	defer func() { WkToPdfDir = oldPath }()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := (&PdfOption{FileName: filepath.Join(t.TempDir(), "book.pdf")}).GenPdfContext(ctx, bytes.NewBufferString("<html></html>"))
	if ctx.Err() != context.DeadlineExceeded || err == nil {
		t.Fatalf("GenPdfContext error=%v context=%v", err, ctx.Err())
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("external PDF process took %s to terminate", elapsed)
	}
}

func TestHtmlToEpubRunContextRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (&HtmlToEpub{EpubOptions: EpubOptions{HTML: []HtmlContent{{Content: "<html></html>"}}}}).RunContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunContext error=%v, want context canceled", err)
	}
}
