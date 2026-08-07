//go:build darwin

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	lifecycleLockName        = ".source-agent-lifecycle.lock"
	lifecycleMaintenanceName = ".managed-worker-maintenance"
)

func runSourceAgentLifecycleHolder(ctx context.Context, directory string, input io.Reader, output io.Writer) error {
	if ctx == nil || input == nil || output == nil {
		return errors.New("invalid lifecycle holder")
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return errors.New("open lifecycle directory")
	}
	defer directoryFile.Close()
	lockFD, err := unix.Openat(int(directoryFile.Fd()), lifecycleLockName, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return errors.New("open lifecycle lock")
	}
	lockFile := os.NewFile(uintptr(lockFD), lifecycleLockName)
	defer lockFile.Close()
	info, err := lockFile.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return errors.New("invalid lifecycle lock")
	}
	if err := unix.Flock(lockFD, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("lock lifecycle")
	}
	defer unix.Flock(lockFD, unix.LOCK_UN) //nolint:errcheck

	marker := filepath.Join(directory, lifecycleMaintenanceName)
	phase, err := loadOrCreateLifecycleMarker(marker, directoryFile)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(output, "locked\n"); err != nil {
		return errors.New("write lifecycle acknowledgement")
	}

	reader := bufio.NewReader(io.LimitReader(input, 128))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		message, err := reader.ReadString('\n')
		if err != nil {
			return errors.New("lifecycle protocol ended before completion")
		}
		switch message {
		case "abort-before-mutation\n":
			if phase != "initial" {
				return errors.New("invalid lifecycle abort phase")
			}
			if err := removeLifecycleMarker(marker, directoryFile); err != nil {
				return err
			}
			_, err = io.WriteString(output, "aborted\n")
			return err
		case "begin-mutation\n":
			if phase != "initial" {
				return errors.New("invalid lifecycle begin phase")
			}
			if err := writeLifecycleMarker(marker, "begin-mutation\n", directoryFile); err != nil {
				return err
			}
			phase = "begun"
			if _, err := io.WriteString(output, "begun\n"); err != nil {
				return errors.New("write lifecycle acknowledgement")
			}
		case "commit\n":
			if phase != "begun" {
				return errors.New("invalid lifecycle commit phase")
			}
			if err := removeLifecycleMarker(marker, directoryFile); err != nil {
				return err
			}
			_, err = io.WriteString(output, "committed\n")
			return err
		default:
			return fmt.Errorf("invalid lifecycle protocol message")
		}
	}
}

func loadOrCreateLifecycleMarker(path string, directory *os.File) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := writeLifecycleMarker(path, "initial\n", directory); err != nil {
			return "", err
		}
		return "initial", nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return "", errors.New("invalid maintenance marker")
	}
	marker, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", errors.New("invalid maintenance marker")
	}
	defer marker.Close()
	pinnedInfo, err := marker.Stat()
	if err != nil || !os.SameFile(info, pinnedInfo) {
		return "", errors.New("invalid maintenance marker")
	}
	data, err := io.ReadAll(io.LimitReader(marker, 33))
	if err != nil || len(data) > 32 {
		return "", errors.New("invalid maintenance marker")
	}
	switch string(data) {
	case "initial\n":
		return "initial", nil
	case "begin-mutation\n":
		return "begun", nil
	default:
		return "", errors.New("invalid maintenance marker")
	}
}

func writeLifecycleMarker(path, value string, directory *os.File) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return errors.New("open maintenance marker")
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		file.Close()
		return errors.New("invalid maintenance marker")
	}
	if _, err = io.WriteString(file, value); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = directory.Sync()
	}
	return err
}

func removeLifecycleMarker(path string, directory *os.File) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return errors.New("invalid maintenance marker")
	}
	if err := os.Remove(path); err != nil {
		return errors.New("remove maintenance marker")
	}
	return directory.Sync()
}
