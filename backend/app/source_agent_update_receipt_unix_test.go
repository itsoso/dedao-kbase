//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSourceAgentUpdateReceiptStorePinsRootDirectory(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "receipts")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	receipt := sourceAgentUpdateTestReadyReceipt()
	seedSourceAgentReadyJournal(t, store, receipt)

	pinnedRoot := root + ".pinned"
	if err := os.Rename(root, pinnedRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, sourceAgentUpdateJournalFileName), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	journal, found, err := store.loadJournal()
	if err != nil || !found || journal.AttemptNonce != receipt.AttemptNonce {
		t.Fatalf("store followed replaced root: journal=%#v found=%v err=%v", journal, found, err)
	}
	if err := store.WriteReady(receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pinnedRoot, filepath.Base(store.readyPath(receipt.CommandID, receipt.AttemptNonce)))); err != nil {
		t.Fatalf("ready receipt was not published under pinned root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.Base(store.readyPath(receipt.CommandID, receipt.AttemptNonce)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement root was modified: %v", err)
	}
}

func TestSourceAgentUpdateReceiptStoreRejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	receipt := sourceAgentUpdateTestReadyReceipt()
	expected := SourceAgentReadyExpectation{
		CommandID: receipt.CommandID, AttemptNonce: receipt.AttemptNonce,
		WorkerType: receipt.WorkerType, Version: receipt.Version,
		Platform: receipt.Platform, Architecture: receipt.Architecture,
		ProtocolVersion: receipt.ProtocolVersion, Revision: receipt.Revision,
	}
	path := store.readyPath(receipt.CommandID, receipt.AttemptNonce)
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = store.WaitReady(context.Background(), expected, 25*time.Millisecond)
	if !errors.Is(err, ErrSourceAgentReadyInvalid) {
		t.Fatalf("FIFO error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("FIFO read blocked for %s", elapsed)
	}
}

func TestSourceAgentUpdateReceiptStoreJournalIsBoundedAndNoFollow(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(string) error
	}{
		{name: "symlink", setup: func(path string) error { return os.Symlink(filepath.Join(filepath.Dir(path), "missing"), path) }},
		{name: "oversized", setup: func(path string) error {
			return os.WriteFile(path, []byte(strings.Repeat("x", sourceAgentUpdateReceiptMaxBytes+1)), 0o600)
		}},
		{name: "duplicate", setup: func(path string) error {
			return os.WriteFile(path, []byte(`{"schema_version":"source-agent-update-journal.v1","schema_version":"source-agent-update-journal.v1"}`), 0o600)
		}},
		{name: "unknown", setup: func(path string) error {
			return os.WriteFile(path, []byte(`{"unknown":true}`), 0o600)
		}},
		{name: "trailing", setup: func(path string) error {
			return os.WriteFile(path, []byte(`{} {}`), 0o600)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if err := test.setup(filepath.Join(root, sourceAgentUpdateJournalFileName)); err != nil {
				t.Fatal(err)
			}
			if _, found, err := store.loadJournal(); err == nil || !found {
				t.Fatalf("loadJournal found=%v err=%v", found, err)
			}
		})
	}
}

func TestSourceAgentUpdateReceiptStoreSerializesAcrossInstances(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	release, err := first.Acquire(context.Background(), "command-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Acquire(context.Background(), "command-2"); !errors.Is(err, ErrSourceAgentUpdateBusy) {
		t.Fatalf("second acquire error=%v", err)
	}
	release()
	releaseSecond, err := second.Acquire(context.Background(), "command-2")
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond()
}

func TestSourceAgentUpdateReceiptStoreRequiresProtectedRealDirectory(t *testing.T) {
	parent := t.TempDir()
	unsafe := filepath.Join(parent, "unsafe")
	if err := os.Mkdir(unsafe, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileSourceAgentUpdateReceiptStore(unsafe, time.Millisecond); err == nil {
		t.Fatal("group/world accessible receipt root should fail")
	}
	realRoot := filepath.Join(parent, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(parent, "linked")
	if err := os.Symlink(realRoot, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileSourceAgentUpdateReceiptStore(linked, time.Millisecond); err == nil {
		t.Fatal("symlink receipt root should fail")
	}
}

func sourceAgentUpdateTestReadyReceipt() SourceAgentReadyReceipt {
	return SourceAgentReadyReceipt{
		CommandID: "command-1", AttemptNonce: strings.Repeat("a", 64),
		WorkerType: "wechat-worker", Version: "2.0.0",
		Platform: "darwin", Architecture: "arm64", ProtocolVersion: "2026-08-01",
		Revision: sourceAgentUpdateTestRevision, HeartbeatAuthenticated: true,
	}
}
