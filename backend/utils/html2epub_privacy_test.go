package utils

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/bmaupin/go-epub"
)

func captureEpubDiagnostics(t *testing.T, run func()) string {
	t.Helper()
	previousLogWriter := log.Writer()
	previousStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	log.SetOutput(&logs)
	os.Stdout = writer
	defer func() {
		log.SetOutput(previousLogWriter)
		os.Stdout = previousStdout
	}()

	run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return logs.String() + string(stdout)
}

func writePrivacyTestPNG(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	pixel := image.NewRGBA(image.Rect(0, 0, 1, 1))
	pixel.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	if err := png.Encode(file, pixel); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func imageSelectionForPrivacyTest(t *testing.T, src string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body><img src="` + src + `"></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	return doc.Find("img").First()
}

func assertEpubDiagnosticsPrivateValuesAbsent(t *testing.T, output string, privateValues ...string) {
	t.Helper()
	for _, privateValue := range privateValues {
		if privateValue != "" && strings.Contains(output, privateValue) {
			t.Fatalf("diagnostics expose private value %q: %q", privateValue, output)
		}
	}
}

func TestHtmlToEpubImageDiagnosticsDoNotExposeSignedURLsPathsOrErrors(t *testing.T) {
	imagesDir := t.TempDir()
	signedURL := "http://127.0.0.1:1/private-cover.png?X-Amz-Signature=secret-signed-query"
	malformedURL := "http://[::1/private.png?token=secret-parse-token"
	missingPath := filepath.Join(t.TempDir(), "private", "missing-image.png")
	privateErrorFragment := "connection refused"

	h := &HtmlToEpub{EpubOptions: EpubOptions{ImagesDir: imagesDir}}
	output := captureEpubDiagnostics(t, func() {
		for _, src := range []string{signedURL, malformedURL} {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body><img src="` + src + `"></body></html>`))
			if err != nil {
				t.Fatal(err)
			}
			h.saveImagesContext(context.Background(), doc)
		}
		h.changeRef("private-book.html", imageSelectionForPrivacyTest(t, missingPath), map[string]string{}, map[string]string{})
	})

	assertEpubDiagnosticsPrivateValuesAbsent(t, output, signedURL, "secret-signed-query", malformedURL, "secret-parse-token", missingPath, privateErrorFragment)
}

func TestHtmlToEpubImageDiagnosticsDoNotExposeMimeAddImageOrVerboseInputs(t *testing.T) {
	root := t.TempDir()
	nonImagePath := filepath.Join(root, "private-not-an-image.txt")
	if err := os.WriteFile(nonImagePath, []byte("private payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	privateImagePath := filepath.Join(root, "private-valid-image.png")
	writePrivacyTestPNG(t, privateImagePath)

	h := &HtmlToEpub{EpubOptions: EpubOptions{Verbose: true}, book: epub.NewEpub("privacy test")}
	if _, err := h.book.AddImage(privateImagePath, "image_000.png"); err != nil {
		t.Fatal(err)
	}
	output := captureEpubDiagnostics(t, func() {
		h.changeRef("private-book.html", imageSelectionForPrivacyTest(t, nonImagePath), map[string]string{}, map[string]string{})
		h.changeRef("private-book.html", imageSelectionForPrivacyTest(t, privateImagePath), map[string]string{}, map[string]string{})
		h.imgIdx = 1
		h.changeRef("private-book.html", imageSelectionForPrivacyTest(t, privateImagePath), map[string]string{}, map[string]string{})
	})

	assertEpubDiagnosticsPrivateValuesAbsent(t, output, nonImagePath, privateImagePath, "private payload")
}

func TestHtmlToEpubReturnedErrorsDoNotExposeContentIDsOrPaths(t *testing.T) {
	root := t.TempDir()
	coverPath := filepath.Join(root, "private-cover.png")
	writePrivacyTestPNG(t, coverPath)
	chapterID := "private-chapter-id.xhtml"
	privateContent := "private chapter body token"
	outputPath := filepath.Join(root, "private-output", "book.epub")
	h := &HtmlToEpub{EpubOptions: EpubOptions{
		Cover:  coverPath,
		Output: outputPath,
		HTML: []HtmlContent{
			{Content: `<html><body>` + privateContent + `</body></html>`, ChapterID: chapterID},
			{Content: `<html><body>duplicate</body></html>`, ChapterID: chapterID},
		},
	}}

	err := h.RunContext(context.Background())
	if err == nil {
		t.Fatal("RunContext unexpectedly succeeded")
	}
	assertEpubDiagnosticsPrivateValuesAbsent(t, err.Error(), chapterID, privateContent, coverPath, outputPath, root)
}
