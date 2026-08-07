//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestKBaseHTTPSourceAgentArtifactMetadataGatesDoNotReadFIFO(t *testing.T) {
	handler, sourceSync, clock, browserSessions := newKBaseSourceAgentCommandHTTPFixture(t)
	credentials, err := createBrowserSessionForTest(browserSessions, BrowserSessionCreate{DeviceLabel: "Artifact FIFO Browser"})
	if err != nil {
		t.Fatal(err)
	}
	artifactRoot := sourceAgentArtifactRootFromHandlerForTest(t, handler)
	artifactPath := filepath.Join(artifactRoot, "artifacts", "artifact-worker")
	if err := os.Remove(artifactPath); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(artifactPath, 0o600); err != nil {
		t.Fatal(err)
	}

	body := `{"type":"upgrade","idempotency_key":"fifo-metadata","payload":{"artifact_id":"artifact-worker","expected_current_version":"1.0.0"},"expires_at":"` + clock.Now().Add(time.Hour).Format(time.RFC3339Nano) + `"}`
	request := newKBaseBrowserCookieRequest(http.MethodPost, "/api/source-agents/agent-a/commands", credentials.Token, body)
	addKBaseBrowserSessionSecurityHeaders(request, credentials.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("metadata-only create status=%d body=%s", response.Code, response.Body.String())
	}
	commands, err := sourceSync.ListSourceAgentCommands("agent-a", 0)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands=%#v err=%v", commands, err)
	}
	claimed, err := sourceSync.ClaimSourceAgentCommand(commands[0].ID, "agent-a", "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	downloadPath := "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-a&command_id=" + url.QueryEscape(claimed.ID)
	download := requestKBase(handler, http.MethodGet, downloadPath, "agent-secret")
	if download.Code != http.StatusConflict {
		t.Fatalf("FIFO download status=%d body=%s", download.Code, download.Body.String())
	}

	commandPath := "/api/source-agent/commands/" + url.PathEscape(claimed.ID) + "/progress"
	for _, state := range []string{SourceAgentCommandDownloading, SourceAgentCommandVerified, SourceAgentCommandInstalling} {
		progress := requestJSONKBase(handler, http.MethodPost, commandPath, "agent-secret", `{"agent_id":"agent-a","state":"`+state+`"}`)
		if progress.Code != http.StatusOK {
			t.Fatalf("metadata-only progress %s status=%d body=%s", state, progress.Code, progress.Body.String())
		}
	}
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
		selection, err := catalog.selectForRollout("artifact-1", sourceAgentArtifactTargetForTest())
		if err != nil {
			t.Fatalf("selectForRollout() FIFO error = %v", err)
		}
		lease, err := catalog.acquireSnapshotLease(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Close()
		if _, err := lease.prepareSnapshot(context.Background(), selection); !errors.Is(err, ErrSourceAgentArtifactIntegrity) {
			t.Fatalf("prepareSnapshot() FIFO error = %v, want ErrSourceAgentArtifactIntegrity", err)
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
