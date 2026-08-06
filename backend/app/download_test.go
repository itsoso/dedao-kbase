package app

import (
	"context"
	"strings"
	"testing"
)

func TestEmitEbookDownloadProgressIgnoresNonWailsContext(t *testing.T) {
	emitEbookDownloadProgress(context.Background(), Progress{Pct: 100, Value: "done"})
}

func TestRunEbookPageFetchRecoversPanic(t *testing.T) {
	_, err := runEbookPageFetch(func() ([]string, error) {
		panic("malformed upstream page")
	})
	if err == nil || !strings.Contains(err.Error(), "ebook page fetch failed") {
		t.Fatalf("panic error = %v", err)
	}
}

func TestDecryptEbookPageRejectsMalformedPayload(t *testing.T) {
	for _, payload := range []string{"", "not-base64", "YQ=="} {
		if _, err := decryptEbookPage(payload); err == nil {
			t.Fatalf("decryptEbookPage(%q) returned nil error", payload)
		}
	}
}
