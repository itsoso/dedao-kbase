//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

type unixSourceAgentUpdateDirectory struct {
	fd int
}

func openSourceAgentUpdateDirectory(root string) (sourceAgentUpdateDirectory, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errSourceAgentUpdateStorageUnavailable
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o077 != 0 {
		unix.Close(fd)
		return nil, errSourceAgentUpdateStorageUnavailable
	}
	return &unixSourceAgentUpdateDirectory{fd: fd}, nil
}

func (d *unixSourceAgentUpdateDirectory) acquire(ctx context.Context) (func(), error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	fd, err := unix.Openat(d.fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrSourceAgentUpdateBusy
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, ErrSourceAgentUpdateBusy
	}
	return func() {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = unix.Close(fd)
	}, nil
}

func (d *unixSourceAgentUpdateDirectory) read(name string, maximum int64) ([]byte, error) {
	if !validSourceAgentUpdateLocalName(name) || maximum <= 0 {
		return nil, ErrSourceAgentReadyInvalid
	}
	fd, err := unix.Openat(d.fd, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, ErrSourceAgentReadyInvalid
	}
	file := os.NewFile(uintptr(fd), "source-agent-update-state")
	if file == nil {
		unix.Close(fd)
		return nil, ErrSourceAgentReadyInvalid
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0o777 != 0o600 || stat.Size <= 0 || stat.Size > maximum {
		return nil, ErrSourceAgentReadyInvalid
	}
	payload, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(payload)) > maximum {
		return nil, ErrSourceAgentReadyInvalid
	}
	return payload, nil
}

func (d *unixSourceAgentUpdateDirectory) writeImmutable(name string, payload []byte) error {
	if !validSourceAgentUpdateLocalName(name) || len(payload) == 0 || len(payload) > sourceAgentUpdateReceiptMaxBytes {
		return ErrSourceAgentReadyInvalid
	}
	temporary, err := d.writeTemporary(payload)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(d.fd, temporary, 0)
	if err := unix.Linkat(d.fd, temporary, d.fd, name, 0); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return errSourceAgentUpdateStorageUnavailable
		}
		existing, readErr := d.read(name, sourceAgentUpdateReceiptMaxBytes)
		if readErr != nil || !bytes.Equal(existing, payload) {
			return errors.New("source agent update state conflicts")
		}
		return d.sync()
	}
	if err := d.sync(); err != nil {
		_ = unix.Unlinkat(d.fd, name, 0)
		_ = d.sync()
		return err
	}
	return nil
}

func (d *unixSourceAgentUpdateDirectory) writeAtomic(name string, payload []byte) error {
	if !validSourceAgentUpdateLocalName(name) || len(payload) == 0 || len(payload) > sourceAgentUpdateReceiptMaxBytes {
		return ErrSourceAgentReadyInvalid
	}
	temporary, err := d.writeTemporary(payload)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(d.fd, temporary, 0)
	if err := unix.Renameat(d.fd, temporary, d.fd, name); err != nil {
		return errSourceAgentUpdateStorageUnavailable
	}
	return d.sync()
}

func (d *unixSourceAgentUpdateDirectory) writeTemporary(payload []byte) (string, error) {
	var random [16]byte
	if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
		return "", errSourceAgentUpdateStorageUnavailable
	}
	name := fmt.Sprintf(".update-state-%x", random[:])
	fd, err := unix.Openat(d.fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", errSourceAgentUpdateStorageUnavailable
	}
	ok := false
	defer func() {
		unix.Close(fd)
		if !ok {
			unix.Unlinkat(d.fd, name, 0)
		}
	}()
	remaining := payload
	for len(remaining) > 0 {
		written, writeErr := unix.Write(fd, remaining)
		if writeErr == unix.EINTR {
			continue
		}
		if writeErr != nil || written <= 0 {
			return "", errSourceAgentUpdateStorageUnavailable
		}
		remaining = remaining[written:]
	}
	if err := unix.Fsync(fd); err != nil {
		return "", errSourceAgentUpdateStorageUnavailable
	}
	if err := unix.Close(fd); err != nil {
		return "", errSourceAgentUpdateStorageUnavailable
	}
	fd = -1
	ok = true
	return name, nil
}

func (d *unixSourceAgentUpdateDirectory) remove(name string) error {
	if !validSourceAgentUpdateLocalName(name) {
		return ErrSourceAgentReadyInvalid
	}
	if err := unix.Unlinkat(d.fd, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return errSourceAgentUpdateStorageUnavailable
	}
	return d.sync()
}

func (d *unixSourceAgentUpdateDirectory) sync() error {
	if err := unix.Fsync(d.fd); err != nil && !errors.Is(err, unix.EINVAL) {
		return errSourceAgentUpdateStorageUnavailable
	}
	return nil
}

func (d *unixSourceAgentUpdateDirectory) close() error {
	if d.fd < 0 {
		return nil
	}
	err := unix.Close(d.fd)
	d.fd = -1
	return err
}
