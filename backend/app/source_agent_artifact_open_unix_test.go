//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	sourceAgentArtifactFIFOHelperEnv  = "KBASE_TEST_SOURCE_ARTIFACT_FIFO_HELPER"
	sourceAgentArtifactFIFOHelperMode = "KBASE_TEST_SOURCE_ARTIFACT_FIFO_MODE"
	sourceAgentArtifactFIFOHelperRoot = "KBASE_TEST_SOURCE_ARTIFACT_FIFO_ROOT"
)

func TestSourceAgentArtifactCatalogRejectsRootSymlinkCanonicalForms(t *testing.T) {
	realRoot := t.TempDir()
	writeSourceAgentArtifactCatalog(t, realRoot, []SourceAgentArtifact{})
	linkParent := t.TempDir()
	linkRoot := filepath.Join(linkParent, "catalog-root")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatal(err)
	}

	for _, root := range []string{
		linkRoot,
		linkRoot + "/",
		linkRoot + "/.",
		linkParent + "//catalog-root//.",
	} {
		if _, err := NewSourceAgentArtifactCatalog(root); err == nil {
			t.Errorf("NewSourceAgentArtifactCatalog() accepted root symlink form %q", root)
		}
	}
}

func TestSourceAgentArtifactCatalogAcceptsCanonicalRootFormsAndAncestorSymlink(t *testing.T) {
	realRoot := t.TempDir()
	writeSourceAgentArtifactCatalog(t, realRoot, []SourceAgentArtifact{})
	for _, root := range []string{
		realRoot,
		realRoot + "/",
		realRoot + "/.",
		filepath.Dir(realRoot) + "//" + filepath.Base(realRoot) + "//.",
	} {
		catalog, err := NewSourceAgentArtifactCatalog(root)
		if err != nil {
			t.Fatalf("NewSourceAgentArtifactCatalog(%q) error = %v", root, err)
		}
		if _, err := catalog.List(0); err != nil {
			t.Fatalf("List() through root form %q error = %v", root, err)
		}
	}

	realParent := t.TempDir()
	rootBelowAncestor := filepath.Join(realParent, "catalog-root")
	if err := os.Mkdir(rootBelowAncestor, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSourceAgentArtifactCatalog(t, rootBelowAncestor, []SourceAgentArtifact{})
	ancestorLink := filepath.Join(t.TempDir(), "ancestor")
	if err := os.Symlink(realParent, ancestorLink); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewSourceAgentArtifactCatalog(filepath.Join(ancestorLink, "catalog-root"))
	if err != nil {
		t.Fatalf("NewSourceAgentArtifactCatalog() rejected normal ancestor symlink: %v", err)
	}
	if _, err := catalog.List(0); err != nil {
		t.Fatalf("List() through normal ancestor symlink error = %v", err)
	}
}

func TestSourceAgentArtifactCatalogRejectsFIFOWithoutBlocking(t *testing.T) {
	t.Run("catalog", func(t *testing.T) {
		root := t.TempDir()
		if err := unix.Mkfifo(filepath.Join(root, "catalog.json"), 0o600); err != nil {
			t.Fatal(err)
		}
		runSourceAgentArtifactFIFOHelper(t, root, "catalog")
	})

	t.Run("artifact", func(t *testing.T) {
		root := t.TempDir()
		data := []byte("artifact")
		artifact := validSourceAgentArtifactForTest("artifact-1", "nested/artifact.bin", data)
		writeSourceAgentArtifactCatalog(t, root, []SourceAgentArtifact{artifact})
		if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(filepath.Join(root, filepath.FromSlash(artifact.StorageKey)), 0o600); err != nil {
			t.Fatal(err)
		}
		runSourceAgentArtifactFIFOHelper(t, root, "artifact")
	})
}

func TestSourceAgentArtifactFIFOHelper(t *testing.T) {
	if os.Getenv(sourceAgentArtifactFIFOHelperEnv) != "1" {
		return
	}
	root := os.Getenv(sourceAgentArtifactFIFOHelperRoot)
	catalog, err := NewSourceAgentArtifactCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	switch os.Getenv(sourceAgentArtifactFIFOHelperMode) {
	case "catalog":
		if _, err := catalog.List(0); !errors.Is(err, ErrSourceAgentArtifactCatalogInvalid) {
			t.Fatalf("List() FIFO error = %v, want ErrSourceAgentArtifactCatalogInvalid", err)
		}
	case "artifact":
		if _, _, err := catalog.ReadForRollout("artifact-1", sourceAgentArtifactTargetForTest()); !errors.Is(err, ErrSourceAgentArtifactIntegrity) {
			t.Fatalf("ReadForRollout() FIFO error = %v, want ErrSourceAgentArtifactIntegrity", err)
		}
	default:
		t.Fatal("unknown FIFO helper mode")
	}
}

func runSourceAgentArtifactFIFOHelper(t *testing.T, root, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceAgentArtifactFIFOHelper$")
	command.Env = append(os.Environ(),
		sourceAgentArtifactFIFOHelperEnv+"=1",
		sourceAgentArtifactFIFOHelperMode+"="+mode,
		sourceAgentArtifactFIFOHelperRoot+"="+root,
	)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s FIFO open blocked past deadline: %v", mode, ctx.Err())
	}
	if err != nil {
		t.Fatalf("%s FIFO helper error = %v\n%s", mode, err, output)
	}
}
