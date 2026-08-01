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
	release   func()
	closeOnce sync.Once
	closeErr  error
}

func (c *SourceAgentArtifactCatalog) prepareSnapshot(ctx context.Context, selection sourceAgentArtifactSelection) (*sourceAgentArtifactSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case c.snapshotSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-c.snapshotSlots }

	source, err := c.openArtifact(c.root, strings.Split(selection.artifact.StorageKey, "/"))
	if err != nil {
		release()
		return nil, ErrSourceAgentArtifactIntegrity
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != selection.artifact.Size {
		release()
		return nil, ErrSourceAgentArtifactIntegrity
	}

	temporary, err := os.CreateTemp(c.snapshotTempDir, ".source-agent-artifact-*")
	if err != nil {
		release()
		return nil, ErrSourceAgentArtifactIntegrity
	}
	snapshot := &sourceAgentArtifactSnapshot{file: temporary, path: temporary.Name(), release: release}
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
		if snapshot.release != nil {
			snapshot.release()
		}
	})
	return snapshot.closeErr
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
