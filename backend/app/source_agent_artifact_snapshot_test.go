package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCopySourceAgentArtifactWithContextUsesFixedBuffer(t *testing.T) {
	reader := &sourceAgentArtifactRecordingReader{remaining: sourceAgentArtifactCopyBufferBytes*3 + 17}
	written, err := copySourceAgentArtifactWithContext(context.Background(), io.Discard, reader)
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(sourceAgentArtifactCopyBufferBytes*3+17) {
		t.Fatalf("written = %d", written)
	}
	if reader.maximumReadBuffer != sourceAgentArtifactCopyBufferBytes {
		t.Fatalf("maximum read buffer = %d, want %d", reader.maximumReadBuffer, sourceAgentArtifactCopyBufferBytes)
	}
}

func TestCopySourceAgentArtifactWithContextStopsOnCancellationAndWriteError(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		writes := 0
		writer := sourceAgentArtifactWriterFunc(func(data []byte) (int, error) {
			writes++
			cancel()
			return len(data), nil
		})
		written, err := copySourceAgentArtifactWithContext(ctx, writer, strings.NewReader(strings.Repeat("x", sourceAgentArtifactCopyBufferBytes*2)))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("copy error = %v, want context.Canceled", err)
		}
		if written != sourceAgentArtifactCopyBufferBytes || writes != 1 {
			t.Fatalf("written = %d, writes = %d", written, writes)
		}
	})

	t.Run("write error", func(t *testing.T) {
		writeErr := errors.New("response disconnected")
		writes := 0
		writer := sourceAgentArtifactWriterFunc(func([]byte) (int, error) {
			writes++
			return 0, writeErr
		})
		written, err := copySourceAgentArtifactWithContext(context.Background(), writer, strings.NewReader("artifact"))
		if !errors.Is(err, writeErr) {
			t.Fatalf("copy error = %v, want write error", err)
		}
		if written != 0 || writes != 1 {
			t.Fatalf("written = %d, writes = %d", written, writes)
		}
	})
}

type sourceAgentArtifactRecordingReader struct {
	remaining         int
	maximumReadBuffer int
}

func (reader *sourceAgentArtifactRecordingReader) Read(buffer []byte) (int, error) {
	if len(buffer) > reader.maximumReadBuffer {
		reader.maximumReadBuffer = len(buffer)
	}
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	read := len(buffer)
	if read > reader.remaining {
		read = reader.remaining
	}
	for index := 0; index < read; index++ {
		buffer[index] = 'x'
	}
	reader.remaining -= read
	return read, nil
}

type sourceAgentArtifactWriterFunc func([]byte) (int, error)

func (write sourceAgentArtifactWriterFunc) Write(data []byte) (int, error) {
	return write(data)
}
