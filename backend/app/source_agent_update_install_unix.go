//go:build darwin || linux

package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

type unixSourceAgentUpdateFileSystem struct {
	mu             sync.Mutex
	fd             int
	locked         bool
	root           string
	executableName string
}

func NewOSSourceAgentUpdateFileSystem(executable string) (SourceAgentUpdateFileSystem, error) {
	if !isCleanAbsoluteSourceAgentUpdatePath(executable) || !validSourceAgentUpdateLocalName(filepath.Base(executable)) {
		return nil, errors.New("source agent executable path is invalid")
	}
	root := filepath.Dir(executable)
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errSourceAgentUpdateStorageUnavailable
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return nil, errSourceAgentUpdateStorageUnavailable
	}
	return &unixSourceAgentUpdateFileSystem{fd: fd, root: root, executableName: filepath.Base(executable)}, nil
}

func (f *unixSourceAgentUpdateFileSystem) duplicateFD() (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fd < 0 {
		return -1, errSourceAgentUpdateStorageUnavailable
	}
	fd, err := unix.FcntlInt(uintptr(f.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, errSourceAgentUpdateStorageUnavailable
	}
	return fd, nil
}

func (f *unixSourceAgentUpdateFileSystem) localName(path string) (string, error) {
	if !isCleanAbsoluteSourceAgentUpdatePath(path) || filepath.Dir(path) != f.root {
		return "", errors.New("source agent install path is outside pinned directory")
	}
	name := filepath.Base(path)
	if !validSourceAgentUpdateLocalName(name) {
		return "", errors.New("source agent install name is invalid")
	}
	return name, nil
}

func (f *unixSourceAgentUpdateFileSystem) requireExecutable(path string) error {
	name, err := f.localName(path)
	if err != nil || name != f.executableName {
		return errors.New("source agent executable identity is invalid")
	}
	return nil
}

func (f *unixSourceAgentUpdateFileSystem) Acquire(ctx context.Context) (func(), error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	f.mu.Lock()
	if f.fd < 0 || f.locked {
		f.mu.Unlock()
		return nil, ErrSourceAgentUpdateBusy
	}
	fd, err := unix.FcntlInt(uintptr(f.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err == nil {
		f.locked = true
	}
	f.mu.Unlock()
	if err != nil {
		return nil, ErrSourceAgentUpdateBusy
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(fd)
		f.mu.Lock()
		f.locked = false
		f.mu.Unlock()
		return nil, ErrSourceAgentUpdateBusy
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = unix.Flock(fd, unix.LOCK_UN)
			_ = unix.Close(fd)
			f.mu.Lock()
			f.locked = false
			f.mu.Unlock()
		})
	}, nil
}

func (f *unixSourceAgentUpdateFileSystem) OpenStaged(root, path string) (SourceAgentUpdateStagedFile, error) {
	return openSourceAgentUpdateStagedFile(root, path)
}

func (f *unixSourceAgentUpdateFileSystem) CreatePrepared(executable, nonce string) (SourceAgentUpdatePreparedFile, error) {
	if err := f.requireExecutable(executable); err != nil || !isExactLowerHex(nonce, sha256.Size*2) {
		return nil, errors.New("invalid local update attempt")
	}
	directoryFD, err := f.duplicateFD()
	if err != nil {
		return nil, err
	}
	defer unix.Close(directoryFD)
	name := filepath.Base(sourceAgentUpdatePreparedPath(executable, nonce))
	fd, err := unix.Openat(directoryFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, errSourceAgentUpdateStorageUnavailable
	}
	if err := validateNewSourceAgentUpdateFileFD(fd); err != nil {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(directoryFD, name, 0)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(f.root, name))
	if file == nil {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(directoryFD, name, 0)
		return nil, errSourceAgentUpdateStorageUnavailable
	}
	return file, nil
}

func (f *unixSourceAgentUpdateFileSystem) BackupExecutable(executable, backup string) (SourceAgentBinaryIdentity, error) {
	if err := f.requireExecutable(executable); err != nil {
		return SourceAgentBinaryIdentity{}, err
	}
	backupName, err := f.localName(backup)
	if err != nil || backupName != sourceAgentUpdateBackupName() {
		return SourceAgentBinaryIdentity{}, errors.New("source agent backup path is invalid")
	}
	directoryFD, err := f.duplicateFD()
	if err != nil {
		return SourceAgentBinaryIdentity{}, err
	}
	defer unix.Close(directoryFD)
	source, stat, err := openRegularSourceAgentUpdateFileAt(directoryFD, f.executableName)
	if err != nil {
		return SourceAgentBinaryIdentity{}, err
	}
	defer source.Close()
	pending := sourceAgentUpdateBackupPendingName()
	targetFD, err := unix.Openat(directoryFD, pending, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return SourceAgentBinaryIdentity{}, errSourceAgentUpdateStorageUnavailable
	}
	if err := validateNewSourceAgentUpdateFileFD(targetFD); err != nil {
		_ = unix.Close(targetFD)
		_ = unix.Unlinkat(directoryFD, pending, 0)
		return SourceAgentBinaryIdentity{}, err
	}
	target := os.NewFile(uintptr(targetFD), filepath.Join(f.root, pending))
	if target == nil {
		_ = unix.Close(targetFD)
		_ = unix.Unlinkat(directoryFD, pending, 0)
		return SourceAgentBinaryIdentity{}, errSourceAgentUpdateStorageUnavailable
	}
	ok := false
	defer func() {
		_ = target.Close()
		if !ok {
			_ = unix.Unlinkat(directoryFD, pending, 0)
		}
	}()
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(target, hasher), io.LimitReader(source, sourceAgentArtifactMaxBytes+1))
	if copyErr != nil || written <= 0 || written > sourceAgentArtifactMaxBytes || written != stat.Size {
		return SourceAgentBinaryIdentity{}, errors.New("backup copy failed")
	}
	if err := unix.Fchmod(targetFD, 0o755); err != nil || target.Sync() != nil || target.Close() != nil {
		return SourceAgentBinaryIdentity{}, errSourceAgentUpdateStorageUnavailable
	}
	if err := unix.Linkat(directoryFD, pending, directoryFD, backupName, 0); err != nil {
		return SourceAgentBinaryIdentity{}, errSourceAgentUpdateStorageUnavailable
	}
	if err := unix.Unlinkat(directoryFD, pending, 0); err != nil {
		_ = unix.Unlinkat(directoryFD, backupName, 0)
		return SourceAgentBinaryIdentity{}, errSourceAgentUpdateStorageUnavailable
	}
	ok = true
	return sourceAgentIdentityFromStat(stat, written, fmt.Sprintf("%x", hasher.Sum(nil))), nil
}

func (f *unixSourceAgentUpdateFileSystem) ReplaceExecutable(prepared, executable string) error {
	if err := f.requireExecutable(executable); err != nil {
		return err
	}
	preparedName, err := f.localName(prepared)
	if err != nil || !strings.HasPrefix(preparedName, ".source-agent-prepared-") {
		return errors.New("prepared executable identity is invalid")
	}
	directoryFD, err := f.duplicateFD()
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	preparedFile, _, err := openRegularSourceAgentUpdateFileAt(directoryFD, preparedName)
	if err != nil {
		return err
	}
	defer preparedFile.Close()
	if err := unix.Renameat(directoryFD, preparedName, directoryFD, f.executableName); err != nil {
		return errSourceAgentUpdateStorageUnavailable
	}
	return nil
}

func (f *unixSourceAgentUpdateFileSystem) RestoreExecutable(backup, executable, nonce string, expected SourceAgentBinaryIdentity) error {
	if err := f.requireExecutable(executable); err != nil {
		return err
	}
	backupName, err := f.localName(backup)
	if err != nil || backupName != sourceAgentUpdateBackupName() {
		return errors.New("source agent backup identity is invalid")
	}
	directoryFD, err := f.duplicateFD()
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	source, _, err := openRegularSourceAgentUpdateFileAt(directoryFD, backupName)
	if err != nil {
		return err
	}
	defer source.Close()
	restoreSuffix := nonce
	if !isExactLowerHex(restoreSuffix, sha256.Size*2) {
		restoreSuffix = "recovery"
	}
	temporary, targetFD, err := createRandomSourceAgentUpdateFileAt(directoryFD, ".source-agent-restore-"+restoreSuffix+"-")
	if err != nil {
		return err
	}
	target := os.NewFile(uintptr(targetFD), filepath.Join(f.root, temporary))
	if target == nil {
		_ = unix.Close(targetFD)
		_ = unix.Unlinkat(directoryFD, temporary, 0)
		return errSourceAgentUpdateStorageUnavailable
	}
	ok := false
	defer func() {
		_ = target.Close()
		if !ok {
			_ = unix.Unlinkat(directoryFD, temporary, 0)
		}
	}()
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(target, hasher), io.LimitReader(source, sourceAgentArtifactMaxBytes+1))
	actualHash := fmt.Sprintf("%x", hasher.Sum(nil))
	if copyErr != nil || written <= 0 || written > sourceAgentArtifactMaxBytes ||
		(validSourceAgentBinaryIdentity(expected) && (written != expected.Size || actualHash != expected.SHA256)) {
		return errors.New("backup integrity check failed")
	}
	if err := unix.Fchmod(targetFD, 0o755); err != nil || target.Sync() != nil || target.Close() != nil {
		return errSourceAgentUpdateStorageUnavailable
	}
	if err := unix.Renameat(directoryFD, temporary, directoryFD, f.executableName); err != nil {
		return errSourceAgentUpdateStorageUnavailable
	}
	ok = true
	return nil
}

func (f *unixSourceAgentUpdateFileSystem) RegularFileExists(path string) (bool, error) {
	name, err := f.localName(path)
	if err != nil {
		return false, err
	}
	directoryFD, err := f.duplicateFD()
	if err != nil {
		return false, err
	}
	defer unix.Close(directoryFD)
	var stat unix.Stat_t
	if err := unix.Fstatat(directoryFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return false, nil
	} else if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return false, errors.New("update recovery file is not regular")
	}
	return true, nil
}

func (f *unixSourceAgentUpdateFileSystem) RegularFileIdentity(path string) (SourceAgentBinaryIdentity, error) {
	name, err := f.localName(path)
	if err != nil {
		return SourceAgentBinaryIdentity{}, err
	}
	directoryFD, err := f.duplicateFD()
	if err != nil {
		return SourceAgentBinaryIdentity{}, err
	}
	defer unix.Close(directoryFD)
	file, stat, err := openRegularSourceAgentUpdateFileAt(directoryFD, name)
	if err != nil {
		return SourceAgentBinaryIdentity{}, err
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, io.LimitReader(file, sourceAgentArtifactMaxBytes+1))
	if err != nil || size <= 0 || size > sourceAgentArtifactMaxBytes || size != stat.Size {
		return SourceAgentBinaryIdentity{}, errors.New("source agent update file identity is invalid")
	}
	return sourceAgentIdentityFromStat(stat, size, fmt.Sprintf("%x", hasher.Sum(nil))), nil
}

func (f *unixSourceAgentUpdateFileSystem) SyncDirectory(path string) error {
	if filepath.Clean(path) != f.root {
		return errors.New("source agent sync directory is outside pinned install root")
	}
	directoryFD, err := f.duplicateFD()
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	if err := unix.Fsync(directoryFD); err != nil && !errors.Is(err, unix.EINVAL) {
		return errSourceAgentUpdateStorageUnavailable
	}
	return nil
}

func (f *unixSourceAgentUpdateFileSystem) Remove(path string) error {
	name, err := f.localName(path)
	if err != nil {
		return err
	}
	directoryFD, err := f.duplicateFD()
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	if err := unix.Unlinkat(directoryFD, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return errSourceAgentUpdateStorageUnavailable
	}
	return nil
}

func (f *unixSourceAgentUpdateFileSystem) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fd < 0 {
		return nil
	}
	err := unix.Close(f.fd)
	f.fd = -1
	return err
}

func openRegularSourceAgentUpdateFileAt(directoryFD int, name string) (*os.File, unix.Stat_t, error) {
	if !validSourceAgentUpdateLocalName(name) {
		return nil, unix.Stat_t{}, errors.New("source agent install name is invalid")
	}
	fd, err := unix.Openat(directoryFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, unix.Stat_t{}, errSourceAgentUpdateStorageUnavailable
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size <= 0 || stat.Size > sourceAgentArtifactMaxBytes {
		_ = unix.Close(fd)
		return nil, unix.Stat_t{}, errors.New("source agent install file is not regular")
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, unix.Stat_t{}, errSourceAgentUpdateStorageUnavailable
	}
	return file, stat, nil
}

func sourceAgentIdentityFromStat(stat unix.Stat_t, size int64, hash string) SourceAgentBinaryIdentity {
	return SourceAgentBinaryIdentity{Size: size, SHA256: hash, Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}
}

func createRandomSourceAgentUpdateFileAt(directoryFD int, prefix string) (string, int, error) {
	for attempt := 0; attempt < 8; attempt++ {
		var random [16]byte
		if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
			return "", -1, errSourceAgentUpdateStorageUnavailable
		}
		name := fmt.Sprintf("%s%x", prefix, random[:])
		fd, err := unix.Openat(directoryFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
		if err == nil {
			if validateErr := validateNewSourceAgentUpdateFileFD(fd); validateErr != nil {
				_ = unix.Close(fd)
				_ = unix.Unlinkat(directoryFD, name, 0)
				return "", -1, validateErr
			}
			return name, fd, nil
		}
		if !errors.Is(err, unix.EEXIST) {
			return "", -1, errSourceAgentUpdateStorageUnavailable
		}
	}
	return "", -1, errSourceAgentUpdateStorageUnavailable
}

func validateNewSourceAgentUpdateFileFD(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size != 0 {
		return errSourceAgentUpdateStorageUnavailable
	}
	return nil
}
