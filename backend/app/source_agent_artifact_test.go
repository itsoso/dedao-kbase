package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const sourceAgentArtifactTestRevision = "0123456789abcdef0123456789abcdef01234567"

func TestSourceAgentArtifactCatalogValidatesAndReadsExactBytes(t *testing.T) {
	requireSourceAgentArtifactFilesystem(t)
	root := t.TempDir()
	data := []byte("fixed source worker artifact\n")
	artifact := validSourceAgentArtifactForTest("wechat-2-0-0", "artifacts/wechat-worker", data)
	writeSourceAgentArtifactFixture(t, root, []SourceAgentArtifact{artifact}, map[string][]byte{
		artifact.StorageKey: data,
	})

	catalog, err := NewSourceAgentArtifactCatalog(root)
	if err != nil {
		t.Fatalf("NewSourceAgentArtifactCatalog() error = %v", err)
	}
	listed, err := catalog.List(0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != artifact.ID || listed[0].AllowedForRollout != true {
		t.Fatalf("List() = %#v", listed)
	}
	encoded, err := json.Marshal(listed[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"storage_key", artifact.StorageKey, root} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public artifact metadata leaked %q: %s", forbidden, encoded)
		}
	}

	metadata, got, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, SourceAgentArtifactTarget{
		WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64", CurrentVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	if metadata.ID != artifact.ID || metadata.Revision != sourceAgentArtifactTestRevision || string(got) != string(data) {
		t.Fatalf("snapshot = %#v, %q", metadata, got)
	}
}

func TestSourceAgentArtifactCatalogMetadataValidationDoesNotOpenArtifact(t *testing.T) {
	requireSourceAgentArtifactFilesystem(t)
	root := t.TempDir()
	data := []byte("artifact")
	artifact := validSourceAgentArtifactForTest("artifact-1", "missing/artifact.bin", data)
	writeSourceAgentArtifactCatalog(t, root, []SourceAgentArtifact{artifact})
	catalog, err := NewSourceAgentArtifactCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := catalog.selectForRollout(artifact.ID, sourceAgentArtifactTargetForTest())
	if err != nil {
		t.Fatalf("selectForRollout() read artifact bytes: %v", err)
	}
	if selection.artifact.StorageKey != artifact.StorageKey {
		t.Fatalf("selection = %#v", selection)
	}
	if _, err := catalog.prepareSnapshot(context.Background(), selection); !errors.Is(err, ErrSourceAgentArtifactIntegrity) {
		t.Fatalf("prepareSnapshot() error = %v, want ErrSourceAgentArtifactIntegrity", err)
	}
}

func TestSourceAgentArtifactCatalogSnapshotsArtifactOnceAndCleansPrivateTemp(t *testing.T) {
	requireSourceAgentArtifactFilesystem(t)
	root := t.TempDir()
	data := []byte("artifact snapshot bytes")
	artifact := validSourceAgentArtifactForTest("artifact-1", "artifacts/artifact.bin", data)
	writeSourceAgentArtifactFixture(t, root, []SourceAgentArtifact{artifact}, map[string][]byte{artifact.StorageKey: data})
	catalog, err := NewSourceAgentArtifactCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	catalog.snapshotTempDir = tempDir
	originalOpen := catalog.openArtifact
	openCount := 0
	catalog.openArtifact = func(root string, parts []string) (*os.File, error) {
		openCount++
		return originalOpen(root, parts)
	}
	selection, err := catalog.selectForRollout(artifact.ID, sourceAgentArtifactTargetForTest())
	if err != nil {
		t.Fatal(err)
	}
	if openCount != 0 {
		t.Fatalf("metadata validation opened artifact %d times", openCount)
	}
	snapshot, err := catalog.prepareSnapshot(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if openCount != 1 {
		t.Fatalf("prepareSnapshot() opened artifact %d times, want 1", openCount)
	}
	info, err := snapshot.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("snapshot mode = %#o, want 0600", got)
	}
	if entries, err := os.ReadDir(tempDir); err != nil || len(entries) != 0 {
		t.Fatalf("snapshot temp entries before close = %#v, %v", entries, err)
	}
	got, err := io.ReadAll(snapshot.file)
	if err != nil || string(got) != string(data) {
		t.Fatalf("snapshot bytes = %q, %v", got, err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(tempDir); err != nil || len(entries) != 0 {
		t.Fatalf("snapshot temp entries after close = %#v, %v", entries, err)
	}
}

func TestSourceAgentArtifactCatalogReloadsRolloutKillSwitch(t *testing.T) {
	requireSourceAgentArtifactFilesystem(t)
	root := t.TempDir()
	data := []byte("artifact")
	artifact := validSourceAgentArtifactForTest("wechat-2-0-0", "worker", data)
	writeSourceAgentArtifactFixture(t, root, []SourceAgentArtifact{artifact}, map[string][]byte{artifact.StorageKey: data})
	catalog, err := NewSourceAgentArtifactCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, sourceAgentArtifactTargetForTest()); err != nil {
		t.Fatalf("initial snapshot error = %v", err)
	}

	artifact.AllowedForRollout = false
	writeSourceAgentArtifactCatalog(t, root, []SourceAgentArtifact{artifact})
	if _, _, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, sourceAgentArtifactTargetForTest()); !errors.Is(err, ErrSourceAgentArtifactNotAllowed) {
		t.Fatalf("disabled snapshot error = %v, want ErrSourceAgentArtifactNotAllowed", err)
	}
	listed, err := catalog.List(0)
	if err != nil || len(listed) != 1 || listed[0].AllowedForRollout {
		t.Fatalf("disabled List() = %#v, %v", listed, err)
	}
}

func TestSourceAgentArtifactCatalogRejectsInvalidMetadata(t *testing.T) {
	requireSourceAgentArtifactFilesystem(t)
	data := []byte("artifact")
	base := validSourceAgentArtifactForTest("artifact-1", "artifact.bin", data)
	tests := []struct {
		name   string
		mutate func(*SourceAgentArtifact)
	}{
		{name: "empty id", mutate: func(a *SourceAgentArtifact) { a.ID = "" }},
		{name: "unsafe id", mutate: func(a *SourceAgentArtifact) { a.ID = "../artifact" }},
		{name: "empty worker type", mutate: func(a *SourceAgentArtifact) { a.WorkerType = "" }},
		{name: "unsupported platform", mutate: func(a *SourceAgentArtifact) { a.Platform = "windows" }},
		{name: "unsupported architecture", mutate: func(a *SourceAgentArtifact) { a.Architecture = "386" }},
		{name: "malformed revision", mutate: func(a *SourceAgentArtifact) { a.Revision = "main" }},
		{name: "malformed version", mutate: func(a *SourceAgentArtifact) { a.Version = "latest" }},
		{name: "malformed protocol", mutate: func(a *SourceAgentArtifact) { a.ProtocolVersion = "v1" }},
		{name: "malformed minimum", mutate: func(a *SourceAgentArtifact) { a.MinimumVersion = "old" }},
		{name: "unsupported channel", mutate: func(a *SourceAgentArtifact) { a.Channel = "nightly" }},
		{name: "dangerous notes url", mutate: func(a *SourceAgentArtifact) { a.ReleaseNotes = "fetch https://example.invalid" }},
		{name: "dangerous notes path", mutate: func(a *SourceAgentArtifact) { a.ReleaseNotes = "/private/update" }},
		{name: "zero size", mutate: func(a *SourceAgentArtifact) { a.Size = 0 }},
		{name: "malformed sha", mutate: func(a *SourceAgentArtifact) { a.SHA256 = "deadbeef" }},
		{name: "traversal key", mutate: func(a *SourceAgentArtifact) { a.StorageKey = "../artifact.bin" }},
		{name: "backslash key", mutate: func(a *SourceAgentArtifact) { a.StorageKey = `artifacts\\artifact.bin` }},
		{name: "absolute key", mutate: func(a *SourceAgentArtifact) { a.StorageKey = "/artifact.bin" }},
		{name: "dot segment key", mutate: func(a *SourceAgentArtifact) { a.StorageKey = "artifacts/./artifact.bin" }},
		{name: "failed build gate", mutate: func(a *SourceAgentArtifact) { a.BuildGate = "failed" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			artifact := base
			test.mutate(&artifact)
			writeSourceAgentArtifactCatalog(t, root, []SourceAgentArtifact{artifact})
			catalog, err := NewSourceAgentArtifactCatalog(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := catalog.List(0); err == nil {
				t.Fatalf("List() accepted invalid artifact: %#v", artifact)
			}
		})
	}
}

func TestSourceAgentArtifactCatalogRejectsInvalidCatalogJSON(t *testing.T) {
	requireSourceAgentArtifactFilesystem(t)
	data := []byte("artifact")
	artifact := validSourceAgentArtifactForTest("artifact-1", "artifact.bin", data)
	valid, err := json.Marshal(struct {
		Artifacts []SourceAgentArtifact `json:"artifacts"`
	}{Artifacts: []SourceAgentArtifact{artifact}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "missing artifacts", body: `{}`},
		{name: "null artifacts", body: `{"artifacts":null}`},
		{name: "unknown top field", body: `{"artifacts":[],"root":"private"}`},
		{name: "unknown artifact field", body: strings.Replace(string(valid), `"id":"artifact-1"`, `"id":"artifact-1","url":"https://example.invalid"`, 1)},
		{name: "duplicate top key", body: `{"artifacts":[],"artifacts":[]}`},
		{name: "duplicate artifact key", body: strings.Replace(string(valid), `"id":"artifact-1"`, `"id":"artifact-1","id":"artifact-2"`, 1)},
		{name: "trailing json", body: string(valid) + ` {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			catalog, err := NewSourceAgentArtifactCatalog(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := catalog.List(0); err == nil {
				t.Fatal("List() accepted invalid catalog JSON")
			}
		})
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte(strings.Repeat(" ", sourceAgentArtifactCatalogMaxBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewSourceAgentArtifactCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.List(0); !errors.Is(err, ErrSourceAgentArtifactCatalogInvalid) {
		t.Fatalf("oversized catalog error = %v", err)
	}
}

func TestSourceAgentArtifactCatalogRejectsUnsafeFilesAndByteDrift(t *testing.T) {
	requireSourceAgentArtifactFilesystem(t)
	data := []byte("artifact")
	artifact := validSourceAgentArtifactForTest("artifact-1", "nested/artifact.bin", data)
	target := sourceAgentArtifactTargetForTest()

	t.Run("wrong size", func(t *testing.T) {
		root := t.TempDir()
		artifact.Size++
		writeSourceAgentArtifactFixture(t, root, []SourceAgentArtifact{artifact}, map[string][]byte{artifact.StorageKey: data})
		catalog, _ := NewSourceAgentArtifactCatalog(root)
		if _, _, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, target); !errors.Is(err, ErrSourceAgentArtifactIntegrity) {
			t.Fatalf("snapshot error = %v", err)
		}
	})

	t.Run("wrong hash", func(t *testing.T) {
		root := t.TempDir()
		artifact := validSourceAgentArtifactForTest("artifact-1", "artifact.bin", data)
		artifact.SHA256 = strings.Repeat("0", 64)
		writeSourceAgentArtifactFixture(t, root, []SourceAgentArtifact{artifact}, map[string][]byte{artifact.StorageKey: data})
		catalog, _ := NewSourceAgentArtifactCatalog(root)
		if _, _, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, target); !errors.Is(err, ErrSourceAgentArtifactIntegrity) {
			t.Fatalf("snapshot error = %v", err)
		}
	})

	t.Run("artifact symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.bin")
		if err := os.WriteFile(outside, data, 0o600); err != nil {
			t.Fatal(err)
		}
		writeSourceAgentArtifactCatalog(t, root, []SourceAgentArtifact{artifact})
		if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(artifact.StorageKey))); err != nil {
			t.Fatal(err)
		}
		catalog, _ := NewSourceAgentArtifactCatalog(root)
		if _, _, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, target); err == nil {
			t.Fatal("snapshot followed artifact symlink")
		}
	})

	t.Run("component symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "artifact.bin"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		writeSourceAgentArtifactCatalog(t, root, []SourceAgentArtifact{artifact})
		if err := os.Symlink(outside, filepath.Join(root, "nested")); err != nil {
			t.Fatal(err)
		}
		catalog, _ := NewSourceAgentArtifactCatalog(root)
		if _, _, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, target); err == nil {
			t.Fatal("snapshot followed component symlink")
		}
	})

	t.Run("catalog symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "catalog.json")
		outsideRoot := filepath.Dir(outside)
		writeSourceAgentArtifactCatalog(t, outsideRoot, []SourceAgentArtifact{artifact})
		if err := os.Symlink(outside, filepath.Join(root, "catalog.json")); err != nil {
			t.Fatal(err)
		}
		catalog, _ := NewSourceAgentArtifactCatalog(root)
		if _, err := catalog.List(0); err == nil {
			t.Fatal("List() followed catalog symlink")
		}
	})

	t.Run("root symlink", func(t *testing.T) {
		realRoot := t.TempDir()
		writeSourceAgentArtifactCatalog(t, realRoot, []SourceAgentArtifact{artifact})
		linkRoot := filepath.Join(t.TempDir(), "catalog-root")
		if err := os.Symlink(realRoot, linkRoot); err != nil {
			t.Fatal(err)
		}
		if _, err := NewSourceAgentArtifactCatalog(linkRoot); err == nil {
			t.Fatal("NewSourceAgentArtifactCatalog() accepted root symlink")
		}
	})

	t.Run("non regular", func(t *testing.T) {
		root := t.TempDir()
		writeSourceAgentArtifactCatalog(t, root, []SourceAgentArtifact{artifact})
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(artifact.StorageKey)), 0o700); err != nil {
			t.Fatal(err)
		}
		catalog, _ := NewSourceAgentArtifactCatalog(root)
		if _, _, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, target); err == nil {
			t.Fatal("snapshot accepted directory")
		}
	})
}

func TestSourceAgentArtifactCatalogRejectsIncompatibleTargetAndDisabledRollout(t *testing.T) {
	requireSourceAgentArtifactFilesystem(t)
	root := t.TempDir()
	data := []byte("artifact")
	artifact := validSourceAgentArtifactForTest("artifact-1", "artifact.bin", data)
	writeSourceAgentArtifactFixture(t, root, []SourceAgentArtifact{artifact}, map[string][]byte{artifact.StorageKey: data})
	catalog, _ := NewSourceAgentArtifactCatalog(root)
	for _, target := range []SourceAgentArtifactTarget{
		{WorkerType: "wcplus-worker", Platform: "darwin", Architecture: "arm64", CurrentVersion: "1.0.0"},
		{WorkerType: "wechat-worker", Platform: "linux", Architecture: "arm64", CurrentVersion: "1.0.0"},
		{WorkerType: "wechat-worker", Platform: "darwin", Architecture: "amd64", CurrentVersion: "1.0.0"},
		{WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64", CurrentVersion: "0.8.0"},
		{WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64", CurrentVersion: "2.0.0"},
		{WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64", CurrentVersion: "3.0.0"},
	} {
		if _, _, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, target); !errors.Is(err, ErrSourceAgentArtifactIncompatible) {
			t.Fatalf("snapshot(%#v) error = %v", target, err)
		}
	}

	artifact.AllowedForRollout = false
	writeSourceAgentArtifactCatalog(t, root, []SourceAgentArtifact{artifact})
	if _, _, err := readSourceAgentArtifactSnapshotForTest(catalog, artifact.ID, sourceAgentArtifactTargetForTest()); !errors.Is(err, ErrSourceAgentArtifactNotAllowed) {
		t.Fatalf("disabled snapshot error = %v", err)
	}
}

func TestSourceAgentArtifactPackagingSmokeFixture(t *testing.T) {
	root := os.Getenv("KBASE_ARTIFACT_SMOKE_ROOT")
	if root == "" {
		t.Skip("packaging smoke fixture is not configured")
	}
	catalog, err := NewSourceAgentArtifactCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	metadata, data, err := readSourceAgentArtifactSnapshotForTest(catalog, "smoke-artifact", SourceAgentArtifactTarget{
		WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64", CurrentVersion: "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Revision != os.Getenv("KBASE_ARTIFACT_SMOKE_REVISION") ||
		metadata.Size != int64(len(data)) || metadata.SHA256 != sha256HexForTest(data) ||
		metadata.Platform+"/"+metadata.Architecture != "darwin/arm64" {
		t.Fatalf("metadata mismatch: %#v", metadata)
	}
}

func validSourceAgentArtifactForTest(id, storageKey string, data []byte) SourceAgentArtifact {
	return SourceAgentArtifact{
		ID: id, WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
		Revision: sourceAgentArtifactTestRevision, Version: "2.0.0", ProtocolVersion: "2026-08-01",
		MinimumVersion: "1.0.0", Channel: "staging", ReleaseNotes: "Operator approved maintenance release",
		Size: int64(len(data)), SHA256: sha256HexForTest(data), StorageKey: storageKey,
		BuildGate: "passed", AllowedForRollout: true,
	}
}

func sourceAgentArtifactTargetForTest() SourceAgentArtifactTarget {
	return SourceAgentArtifactTarget{WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64", CurrentVersion: "1.0.0"}
}

func writeSourceAgentArtifactFixture(t testing.TB, root string, artifacts []SourceAgentArtifact, files map[string][]byte) {
	t.Helper()
	writeSourceAgentArtifactCatalog(t, root, artifacts)
	for key, data := range files {
		path := filepath.Join(root, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeSourceAgentArtifactCatalog(t testing.TB, root string, artifacts []SourceAgentArtifact) {
	t.Helper()
	body, err := json.Marshal(struct {
		Artifacts []SourceAgentArtifact `json:"artifacts"`
	}{Artifacts: artifacts})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sha256HexForTest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readSourceAgentArtifactSnapshotForTest(catalog *SourceAgentArtifactCatalog, id string, target SourceAgentArtifactTarget) (SourceAgentArtifactPublic, []byte, error) {
	selection, err := catalog.selectForRollout(id, target)
	if err != nil {
		return SourceAgentArtifactPublic{}, nil, err
	}
	snapshot, err := catalog.prepareSnapshot(context.Background(), selection)
	if err != nil {
		return SourceAgentArtifactPublic{}, nil, err
	}
	defer snapshot.Close()
	data, err := io.ReadAll(snapshot.file)
	if err != nil {
		return SourceAgentArtifactPublic{}, nil, err
	}
	return selection.artifact.public(), data, nil
}

func requireSourceAgentArtifactFilesystem(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("V1 artifact filesystem is macOS-first and fails closed on Windows")
	}
}
