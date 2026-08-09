package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
	"github.com/yann0917/dedao-gui/backend/utils"
)

type cancelingEbookDownloadService struct {
	wantContext context.Context
	cancel      context.CancelFunc
	cancelPages bool
	calls       []string
}

func (s *cancelingEbookDownloadService) record(ctx context.Context, call string) {
	if ctx != s.wantContext {
		panic("ebook request did not receive the worker context")
	}
	s.calls = append(s.calls, call)
}

func (s *cancelingEbookDownloadService) EbookDetailContext(ctx context.Context, _ string) (*services.EbookDetail, error) {
	s.record(ctx, "detail")
	return &services.EbookDetail{Enid: "resolved-enid", Title: "book"}, nil
}

func (s *cancelingEbookDownloadService) EbookReadTokenContext(ctx context.Context, _ string) (*services.Token, error) {
	s.record(ctx, "read-token")
	return &services.Token{Token: "read-token"}, nil
}

func (s *cancelingEbookDownloadService) EbookInfoContext(ctx context.Context, _ string) (*services.EbookInfo, error) {
	s.record(ctx, "info")
	info := new(services.EbookInfo)
	info.BookInfo.Orders = []services.EbookOrders{{ChapterID: "chapter-1"}}
	return info, nil
}

func (s *cancelingEbookDownloadService) EbookPagesContext(ctx context.Context, _, _ string, _, _, _ int) (*services.EbookPage, error) {
	s.record(ctx, "pages")
	if s.cancelPages {
		s.cancel()
	}
	return &services.EbookPage{IsEnd: true}, nil
}

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

func TestEbookDownloadPropagatesContextThroughRealChainAndSkipsWriteAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := &cancelingEbookDownloadService{wantContext: ctx, cancel: cancel, cancelPages: true}
	writeCalled := false
	oldWriteHTML := writeEbookHTML
	writeEbookHTML = func(context.Context, string, string, []*utils.SvgContent, []utils.EbookToc) error {
		writeCalled = true
		return nil
	}
	defer func() { writeEbookHTML = oldWriteHTML }()

	download := EBookDownload{
		Ctx:          ctx,
		ebookService: service,
		DownloadType: 1,
		ID:           7,
		EnID:         "requested-enid",
		OutputDir:    t.TempDir(),
	}
	_, err := download.DownloadWithResult()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadWithResult error=%v, want context canceled", err)
	}
	if got := strings.Join(service.calls, ","); got != "detail,read-token,info,pages" {
		t.Fatalf("request chain=%q", got)
	}
	if writeCalled {
		t.Fatal("HTML writer was called after cancellation")
	}
}

func TestGenerateEbookFileAtomicallyDoesNotPublishCanceledOutput(t *testing.T) {
	for _, extension := range []string{"html", "pdf", "epub"} {
		t.Run(extension, func(t *testing.T) {
			outputDir := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			_, err := generateEbookFileAtomically(ctx, outputDir, "book", extension, func(_ context.Context, stagingRoot string) error {
				stagedPath, err := ebookGeneratedFilePath(stagingRoot, "book", extension)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(stagedPath), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(stagedPath, []byte("partial output"), 0o600); err != nil {
					return err
				}
				cancel()
				return nil
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v, want context canceled", err)
			}
			finalPath, pathErr := ebookGeneratedFilePath(outputDir, "book", extension)
			if pathErr != nil {
				t.Fatal(pathErr)
			}
			if _, statErr := os.Stat(finalPath); !os.IsNotExist(statErr) {
				t.Fatalf("canceled output was published at %q: %v", finalPath, statErr)
			}
			matches, globErr := filepath.Glob(filepath.Join(outputDir, ".dedao-ebook-staging-*"))
			if globErr != nil || len(matches) != 0 {
				t.Fatalf("staging files left behind: %v, err=%v", matches, globErr)
			}
		})
	}
}

func TestGenerateEbookFileAtomicallyReplacesExistingTarget(t *testing.T) {
	outputDir := t.TempDir()
	finalPath, err := ebookGeneratedFilePath(outputDir, "book", "html")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(finalPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := generateEbookFileAtomically(context.Background(), outputDir, "book", "html", func(_ context.Context, stagingRoot string) error {
		stagedPath, err := ebookGeneratedFilePath(stagingRoot, "book", "html")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(stagedPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(stagedPath, []byte("new"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != finalPath {
		t.Fatalf("published path=%q want=%q", got, finalPath)
	}
	data, err := os.ReadFile(finalPath)
	if err != nil || string(data) != "new" {
		t.Fatalf("replacement content=%q err=%v", data, err)
	}
}

func TestGenerateEbookFileAtomicallyCancelsBlockingGenerator(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	outputDir := t.TempDir()
	go func() {
		_, err := generateEbookFileAtomically(ctx, outputDir, "book", "html", func(ctx context.Context, _ string) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("blocking generator error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocking generator did not stop after cancellation")
	}
}

func TestEbookDownloadDoesNotPublishWhenEachGeneratorCancels(t *testing.T) {
	oldHTML, oldPDF, oldEPUB := writeEbookHTML, writeEbookPDF, writeEbookEPUB
	defer func() {
		writeEbookHTML, writeEbookPDF, writeEbookEPUB = oldHTML, oldPDF, oldEPUB
	}()
	for _, test := range []struct {
		name          string
		downloadType  int
		extension     string
		installWriter func(context.CancelFunc)
	}{
		{name: "html", downloadType: 1, extension: "html", installWriter: func(cancel context.CancelFunc) {
			writeEbookHTML = func(_ context.Context, root, title string, _ []*utils.SvgContent, _ []utils.EbookToc) error {
				return writeCanceledEbookTestOutput(root, title, "html", cancel)
			}
		}},
		{name: "pdf", downloadType: 2, extension: "pdf", installWriter: func(cancel context.CancelFunc) {
			writeEbookPDF = func(_ context.Context, root, title string, _ []*utils.SvgContent, _ []utils.EbookToc) error {
				return writeCanceledEbookTestOutput(root, title, "pdf", cancel)
			}
		}},
		{name: "epub", downloadType: 3, extension: "epub", installWriter: func(cancel context.CancelFunc) {
			writeEbookEPUB = func(_ context.Context, root, title string, _ []*utils.SvgContent, _ utils.EpubOptions) error {
				return writeCanceledEbookTestOutput(root, title, "epub", cancel)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			test.installWriter(cancel)
			outputDir := t.TempDir()
			service := &cancelingEbookDownloadService{wantContext: ctx, cancel: cancel}
			download := EBookDownload{Ctx: ctx, ebookService: service, DownloadType: test.downloadType, ID: 8, EnID: "ebook", OutputDir: outputDir}
			if _, err := download.DownloadWithResult(); !errors.Is(err, context.Canceled) {
				t.Fatalf("DownloadWithResult error=%v, want context canceled", err)
			}
			finalPath, err := ebookGeneratedFilePath(outputDir, "8_book_", test.extension)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
				t.Fatalf("canceled %s was published: %v", test.extension, err)
			}
		})
	}
}

func writeCanceledEbookTestOutput(root, title, extension string, cancel context.CancelFunc) error {
	path, err := ebookGeneratedFilePath(root, title, extension)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte("staged"), 0o600); err != nil {
		return err
	}
	cancel()
	return nil
}

func TestNormalizeEbookOutputDirPreservesRelativeDesktopDefault(t *testing.T) {
	if got := normalizeEbookOutputDir(""); got != "." {
		t.Fatalf("empty output directory normalized to %q, want current directory", got)
	}
	if got := normalizeEbookOutputDir("downloads"); got != "downloads" {
		t.Fatalf("explicit output directory normalized to %q", got)
	}
}
