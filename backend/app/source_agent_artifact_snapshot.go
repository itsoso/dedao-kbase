package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
)

const (
	sourceAgentArtifactSnapshotConcurrency = 2
	sourceAgentArtifactCopyBufferBytes     = 64 << 10
)

type sourceAgentArtifactSnapshot struct {
	file      *os.File
	path      string
	closeOnce sync.Once
	closeErr  error
}

type sourceAgentArtifactSnapshotLease struct {
	catalog   *SourceAgentArtifactCatalog
	closeOnce sync.Once
}

func (c *SourceAgentArtifactCatalog) acquireSnapshotLease(ctx context.Context) (*sourceAgentArtifactSnapshotLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.snapshotLeaseObserver != nil {
		c.snapshotLeaseObserver()
	}
	select {
	case c.snapshotSlots <- struct{}{}:
		return &sourceAgentArtifactSnapshotLease{catalog: c}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (lease *sourceAgentArtifactSnapshotLease) prepareSnapshot(ctx context.Context, selection sourceAgentArtifactSelection) (*sourceAgentArtifactSnapshot, error) {
	c := lease.catalog
	source, err := c.openArtifact(c.root, strings.Split(selection.artifact.StorageKey, "/"))
	if err != nil {
		return nil, ErrSourceAgentArtifactIntegrity
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != selection.artifact.Size {
		return nil, ErrSourceAgentArtifactIntegrity
	}

	temporary, err := os.CreateTemp(c.snapshotTempDir, ".source-agent-artifact-*")
	if err != nil {
		return nil, ErrSourceAgentArtifactIntegrity
	}
	snapshot := &sourceAgentArtifactSnapshot{file: temporary, path: temporary.Name()}
	keep := false
	defer func() {
		if !keep {
			_ = snapshot.Close()
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return nil, ErrSourceAgentArtifactIntegrity
	}
	if unlinkSourceAgentArtifactSnapshot(snapshot.path) {
		snapshot.path = ""
	}

	digest := sha256.New()
	written, err := copySourceAgentArtifactWithContext(
		ctx,
		io.MultiWriter(temporary, digest),
		io.LimitReader(source, selection.artifact.Size+1),
	)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, ErrSourceAgentArtifactIntegrity
	}
	if written != selection.artifact.Size || hex.EncodeToString(digest.Sum(nil)) != selection.artifact.SHA256 {
		return nil, ErrSourceAgentArtifactIntegrity
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return nil, ErrSourceAgentArtifactIntegrity
	}
	keep = true
	return snapshot, nil
}

func (snapshot *sourceAgentArtifactSnapshot) Close() error {
	if snapshot == nil {
		return nil
	}
	snapshot.closeOnce.Do(func() {
		var removeErr error
		if snapshot.file != nil {
			snapshot.closeErr = snapshot.file.Close()
		}
		if snapshot.path != "" {
			removeErr = os.Remove(snapshot.path)
			if errors.Is(removeErr, os.ErrNotExist) {
				removeErr = nil
			}
		}
		snapshot.closeErr = errors.Join(snapshot.closeErr, removeErr)
	})
	return snapshot.closeErr
}

func (lease *sourceAgentArtifactSnapshotLease) Close() error {
	if lease == nil {
		return nil
	}
	lease.closeOnce.Do(func() { <-lease.catalog.snapshotSlots })
	return nil
}

func copySourceAgentArtifactWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, sourceAgentArtifactCopyBufferBytes)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
		if read == 0 {
			return total, io.ErrNoProgress
		}
	}
}
