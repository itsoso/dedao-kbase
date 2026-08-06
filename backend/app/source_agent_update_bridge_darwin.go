//go:build darwin

package app

import (
	"bytes"
	"context"
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

type darwinSourceAgentUpdateBridgeStorage struct {
	installRoot       string
	parentFD          int
	pathPins          []darwinSourceAgentUpdatePathPin
	stagingFD         int
	handoffFD         int
	workerName        string
	workerIdentity    darwinSourceAgentUpdateEntryIdentity
	updaterIdentity   darwinSourceAgentUpdateEntryIdentity
	stagingIdentity   darwinSourceAgentUpdateEntryIdentity
	handoffIdentity   darwinSourceAgentUpdateEntryIdentity
	workerDevice      uint64
	installIdentity   darwinSourceAgentUpdateEntryIdentity
	lifecycleIdentity darwinSourceAgentUpdateEntryIdentity
	syncFD            func(int) error
	linkat            func(int, string, int, string, int) error
	unlinkat          func(int, string, int) error
	readFixedFile     func(int, string, uint16, int64, uint64, func(int)) ([]byte, bool, error)
	afterFixedRead    func(string, int)
	verifyStagedFile  func(int, string, int64, string, uint64, func(int)) (bool, error)
	afterStagedRead   func(string, int)
}

type darwinSourceAgentUpdateEntryIdentity struct {
	device uint64
	inode  uint64
}

type darwinSourceAgentUpdatePathPin struct {
	directoryFD int
	name        string
	identity    darwinSourceAgentUpdateEntryIdentity
}

func newSourceAgentUpdateBridgeStorage(updaterExecutable, workerType string) (sourceAgentUpdateBridgeStorage, error) {
	workerName, ok := sourceAgentUpdateWorkerBasename(workerType)
	if !ok || filepath.Base(updaterExecutable) != sourceAgentUpdateUpdaterBasename {
		return nil, errSourceAgentUpdateBridgeUnavailable
	}
	installRoot := filepath.Dir(updaterExecutable)
	parentFD, pathPins, err := openDarwinSourceAgentAbsoluteDirectoryNoFollow(installRoot)
	if err != nil {
		return nil, fmt.Errorf("open pinned install directory: %w", err)
	}
	storage := &darwinSourceAgentUpdateBridgeStorage{
		installRoot: installRoot, parentFD: parentFD, pathPins: pathPins, stagingFD: -1, handoffFD: -1,
		workerName: workerName, syncFD: unix.Fsync, linkat: unix.Linkat, unlinkat: unix.Unlinkat,
		readFixedFile: readDarwinSourceAgentFixedFile, verifyStagedFile: verifyDarwinSourceAgentStagedFile,
	}
	ok = false
	defer func() {
		if !ok {
			_ = storage.Close()
		}
	}()
	workerStat, err := openDarwinSourceAgentInstalledExecutable(parentFD, workerName)
	if err != nil {
		return nil, fmt.Errorf("open fixed worker executable: %w", err)
	}
	updaterStat, err := openDarwinSourceAgentInstalledExecutable(parentFD, sourceAgentUpdateUpdaterBasename)
	if err != nil || workerStat.Dev != updaterStat.Dev {
		return nil, fmt.Errorf("open fixed updater executable: %w", errSourceAgentUpdateBridgeUnavailable)
	}
	storage.workerIdentity = darwinSourceAgentIdentity(workerStat)
	storage.updaterIdentity = darwinSourceAgentIdentity(updaterStat)
	storage.workerDevice = uint64(workerStat.Dev)
	var installStat unix.Stat_t
	if err := unix.Fstat(parentFD, &installStat); err != nil || installStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		!darwinSourceAgentModeIsExact(installStat.Mode, 0o700) || installStat.Uid != uint32(unix.Geteuid()) ||
		uint64(installStat.Dev) != storage.workerDevice {
		return nil, fmt.Errorf("validate private install directory: %w", errSourceAgentUpdateBridgeUnavailable)
	}
	storage.installIdentity = darwinSourceAgentIdentity(installStat)
	lifecycleStat, err := openDarwinSourceAgentLifecycleFile(parentFD, storage.workerDevice)
	if err != nil {
		return nil, fmt.Errorf("open fixed lifecycle lock: %w", err)
	}
	storage.lifecycleIdentity = darwinSourceAgentIdentity(lifecycleStat)
	if err := unix.Flock(parentFD, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrSourceAgentUpdateBusy
		}
		return nil, fmt.Errorf("lock private install directory: %w", errSourceAgentUpdateBridgeUnavailable)
	}
	locked := true
	defer func() {
		if locked {
			_ = unix.Flock(parentFD, unix.LOCK_UN)
		}
	}()
	storage.stagingFD, storage.stagingIdentity, err = openDarwinSourceAgentPrivateChildDirectory(
		parentFD, sourceAgentUpdateStagingDirectoryName, storage.workerDevice,
	)
	if err != nil {
		return nil, fmt.Errorf("open fixed staging directory: %w", err)
	}
	storage.handoffFD, storage.handoffIdentity, err = openDarwinSourceAgentPrivateChildDirectory(
		parentFD, sourceAgentUpdateHandoffDirectoryName, storage.workerDevice,
	)
	if err != nil {
		return nil, fmt.Errorf("open fixed handoff directory: %w", err)
	}
	if err := storage.removePending(
		storage.stagingFD, sourceAgentUpdateStagedPendingName, sourceAgentArtifactMaxBytes, 0o600, 0o755,
	); err != nil {
		return nil, fmt.Errorf("clean fixed staged pending file: %w", err)
	}
	if err := storage.removePending(
		storage.handoffFD, sourceAgentUpdateHandoffPendingName, sourceAgentUpdateReceiptMaxBytes, 0o600,
	); err != nil {
		return nil, fmt.Errorf("clean fixed handoff pending file: %w", err)
	}
	if err := storage.syncDirectory(parentFD); err != nil || storage.validatePinned() != nil {
		return nil, fmt.Errorf("validate pinned update directories: %w", errSourceAgentUpdateBridgeUnavailable)
	}
	if err := unix.Flock(parentFD, unix.LOCK_UN); err != nil {
		return nil, fmt.Errorf("unlock private install directory: %w", errSourceAgentUpdateBridgeUnavailable)
	}
	locked = false
	ok = true
	return storage, nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) AcquireLifecycleShared() (func() error, error) {
	if s.validatePinned() != nil {
		return nil, errSourceAgentUpdateHandoffConflict
	}
	fd, err := unix.Openat(
		s.parentFD,
		sourceAgentUpdateLifecycleFileName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return nil, errSourceAgentUpdateBridgeUnavailable
	}
	closeFD := true
	defer func() {
		if closeFD {
			_ = unix.Close(fd)
		}
	}()
	var lockStat unix.Stat_t
	if err := unix.Fstat(fd, &lockStat); err != nil ||
		darwinSourceAgentIdentity(lockStat) != s.lifecycleIdentity ||
		lockStat.Mode&unix.S_IFMT != unix.S_IFREG || !darwinSourceAgentModeIsExact(lockStat.Mode, 0o600) ||
		lockStat.Uid != uint32(unix.Geteuid()) || uint64(lockStat.Dev) != s.workerDevice {
		return nil, errSourceAgentUpdateHandoffConflict
	}
	if err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrSourceAgentUpdateBusy
		}
		return nil, errSourceAgentUpdateBridgeUnavailable
	}
	locked := true
	defer func() {
		if locked {
			_ = unix.Flock(fd, unix.LOCK_UN)
		}
	}()
	var marker unix.Stat_t
	if err := unix.Fstatat(s.parentFD, sourceAgentUpdateMaintenanceFileName, &marker, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return nil, ErrSourceAgentUpdateBusy
	} else if !errors.Is(err, unix.ENOENT) {
		return nil, errSourceAgentUpdateBridgeUnavailable
	}
	if s.validatePinned() != nil {
		return nil, errSourceAgentUpdateHandoffConflict
	}
	closeFD = false
	locked = false
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			if err := unix.Flock(fd, unix.LOCK_UN); err != nil {
				releaseErr = errSourceAgentUpdateBridgeUnavailable
			}
			if err := unix.Close(fd); err != nil && releaseErr == nil {
				releaseErr = errSourceAgentUpdateBridgeUnavailable
			}
		})
		return releaseErr
	}, nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) Lock() error {
	if s.validatePinned() != nil || unix.Flock(s.parentFD, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	if s.validatePinned() != nil {
		_ = unix.Flock(s.parentFD, unix.LOCK_UN)
		return errSourceAgentUpdateHandoffConflict
	}
	return nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) Unlock() error {
	if err := unix.Flock(s.parentFD, unix.LOCK_UN); err != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	return nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) StagedPath() string {
	return filepath.Join(s.installRoot, sourceAgentUpdateStagingDirectoryName, sourceAgentUpdateStagedBasename)
}

func (s *darwinSourceAgentUpdateBridgeStorage) RemoveStaged() error {
	if s.validatePinned() != nil {
		return errSourceAgentUpdateHandoffConflict
	}
	if err := s.unlinkat(s.stagingFD, sourceAgentUpdateStagedBasename, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return errSourceAgentUpdateBridgeUnavailable
	}
	if err := s.syncDirectory(s.stagingFD); err != nil || s.validatePinned() != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	return nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) LoadHandoff() ([]byte, bool, error) {
	if err := s.validatePinned(); err != nil {
		return nil, false, err
	}
	payload, found, err := s.readFixedFile(
		s.handoffFD, sourceAgentUpdateHandoffFileName, 0o600, sourceAgentUpdateReceiptMaxBytes, s.workerDevice,
		func(fd int) {
			if s.afterFixedRead != nil {
				s.afterFixedRead(sourceAgentUpdateHandoffFileName, fd)
			}
		},
	)
	if err != nil {
		return nil, found, errSourceAgentUpdateHandoffConflict
	}
	if found {
		if err := s.removePending(
			s.handoffFD, sourceAgentUpdateHandoffPendingName, sourceAgentUpdateReceiptMaxBytes, 0o600,
		); err != nil {
			return nil, true, err
		}
	}
	if s.validatePinned() != nil {
		return nil, found, errSourceAgentUpdateHandoffConflict
	}
	return payload, found, nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) Stage(
	ctx context.Context,
	reader io.Reader,
	expectedSize int64,
	expectedSHA256 string,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if expectedSize <= 0 || expectedSize > sourceAgentArtifactMaxBytes ||
		!isExactLowerHex(expectedSHA256, sha256.Size*2) || s.validatePinned() != nil {
		return errSourceAgentUpdateHandoffConflict
	}
	if found, err := s.verifyStaged(expectedSize, expectedSHA256); found {
		if err != nil {
			return err
		}
		if err := s.removePending(
			s.stagingFD, sourceAgentUpdateStagedPendingName, sourceAgentArtifactMaxBytes, 0o600, 0o755,
		); err != nil {
			return err
		}
		return nil
	} else if err != nil {
		return err
	}
	temporary := sourceAgentUpdateStagedPendingName
	fd, err := createDarwinSourceAgentBridgePending(s.stagingFD, temporary)
	if err != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	file := os.NewFile(uintptr(fd), "source-agent-staged-artifact")
	if file == nil {
		_ = unix.Close(fd)
		_ = s.unlinkat(s.stagingFD, temporary, 0)
		return errSourceAgentUpdateBridgeUnavailable
	}
	fd = -1
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		_ = s.unlinkat(s.stagingFD, temporary, 0)
	}()
	ownedFD := int(file.Fd())
	hasher := sha256.New()
	written, copyErr := io.Copy(
		io.MultiWriter(file, hasher),
		io.LimitReader(&sourceAgentUpdateContextReader{ctx: ctx, reader: reader}, expectedSize+1),
	)
	if copyErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errSourceAgentUpdateHandoffConflict
	}
	if written != expectedSize || fmt.Sprintf("%x", hasher.Sum(nil)) != expectedSHA256 {
		return errSourceAgentUpdateHandoffConflict
	}
	var stat unix.Stat_t
	if err := unix.Fstat(ownedFD, &stat); err != nil || uint64(stat.Dev) != s.workerDevice || stat.Size != expectedSize {
		return errSourceAgentUpdateBridgeUnavailable
	}
	if err := unix.Fchmod(ownedFD, 0o755); err != nil || file.Sync() != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	var readyStat unix.Stat_t
	if err := unix.Fstat(ownedFD, &readyStat); err != nil || readyStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		!darwinSourceAgentModeIsExact(readyStat.Mode, 0o755) || readyStat.Uid != uint32(unix.Geteuid()) ||
		uint64(readyStat.Dev) != s.workerDevice || readyStat.Size != expectedSize {
		return errSourceAgentUpdateBridgeUnavailable
	}
	closeErr := file.Close()
	file = nil
	if closeErr != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	if s.validatePinned() != nil {
		return errSourceAgentUpdateHandoffConflict
	}
	if !darwinSourceAgentPendingEntryMatches(s.stagingFD, temporary, readyStat) {
		return errSourceAgentUpdateHandoffConflict
	}
	linked := false
	if err := s.linkat(s.stagingFD, temporary, s.stagingFD, sourceAgentUpdateStagedBasename, 0); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return errSourceAgentUpdateBridgeUnavailable
		}
	} else {
		linked = true
	}
	found, verifyErr := s.verifyStaged(expectedSize, expectedSHA256)
	if !found || verifyErr != nil {
		if linked {
			_ = s.unlinkat(s.stagingFD, sourceAgentUpdateStagedBasename, 0)
			_ = s.syncDirectory(s.stagingFD)
		}
		return errSourceAgentUpdateHandoffConflict
	}
	unlinkErr := s.unlinkat(s.stagingFD, temporary, 0)
	unlinked := unlinkErr == nil || errors.Is(unlinkErr, unix.ENOENT)
	if err := s.syncDirectory(s.stagingFD); err != nil || s.validatePinned() != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	if !unlinked {
		return errSourceAgentUpdateBridgeUnavailable
	}
	return nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) VerifyStaged(expectedSize int64, expectedSHA256 string) error {
	found, err := s.verifyStaged(expectedSize, expectedSHA256)
	if !found || err != nil {
		return errSourceAgentUpdateHandoffConflict
	}
	if err := s.removePending(
		s.stagingFD, sourceAgentUpdateStagedPendingName, sourceAgentArtifactMaxBytes, 0o600, 0o755,
	); err != nil {
		return err
	}
	return nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) verifyStaged(expectedSize int64, expectedSHA256 string) (bool, error) {
	if s.validatePinned() != nil {
		return false, errSourceAgentUpdateHandoffConflict
	}
	found, err := s.verifyStagedFile(
		s.stagingFD, sourceAgentUpdateStagedBasename, expectedSize, expectedSHA256, s.workerDevice,
		func(fd int) {
			if s.afterStagedRead != nil {
				s.afterStagedRead(sourceAgentUpdateStagedBasename, fd)
			}
		},
	)
	if err != nil || !found {
		return found, err
	}
	if s.validatePinned() != nil {
		return true, errSourceAgentUpdateHandoffConflict
	}
	return true, nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) PublishHandoff(payload []byte) error {
	if len(payload) == 0 || len(payload) > sourceAgentUpdateReceiptMaxBytes || s.validatePinned() != nil {
		return errSourceAgentUpdateHandoffConflict
	}
	if existing, found, err := s.LoadHandoff(); err != nil {
		return err
	} else if found {
		if bytes.Equal(existing, payload) {
			return nil
		}
		return errSourceAgentUpdateHandoffConflict
	}
	temporary := sourceAgentUpdateHandoffPendingName
	fd, err := createDarwinSourceAgentBridgePending(s.handoffFD, temporary)
	if err != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	defer func() {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		_ = s.unlinkat(s.handoffFD, temporary, 0)
	}()
	if err := writeDarwinSourceAgentBridgeFile(fd, payload); err != nil || unix.Fsync(fd) != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	var readyStat unix.Stat_t
	if err := unix.Fstat(fd, &readyStat); err != nil || readyStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		!darwinSourceAgentModeIsExact(readyStat.Mode, 0o600) || readyStat.Uid != uint32(unix.Geteuid()) ||
		uint64(readyStat.Dev) != s.workerDevice || readyStat.Size != int64(len(payload)) {
		return errSourceAgentUpdateBridgeUnavailable
	}
	closeErr := unix.Close(fd)
	fd = -1
	if closeErr != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	if s.validatePinned() != nil {
		return errSourceAgentUpdateHandoffConflict
	}
	if !darwinSourceAgentPendingEntryMatches(s.handoffFD, temporary, readyStat) {
		return errSourceAgentUpdateHandoffConflict
	}
	linked := false
	if err := s.linkat(s.handoffFD, temporary, s.handoffFD, sourceAgentUpdateHandoffFileName, 0); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return errSourceAgentUpdateBridgeUnavailable
		}
	} else {
		linked = true
	}
	existing, found, loadErr := s.LoadHandoff()
	if loadErr != nil {
		if errors.Is(loadErr, errSourceAgentUpdateBridgeUnavailable) {
			return errSourceAgentUpdateBridgeUnavailable
		}
		if linked {
			_ = s.unlinkat(s.handoffFD, sourceAgentUpdateHandoffFileName, 0)
			_ = s.syncDirectory(s.handoffFD)
		}
		return errSourceAgentUpdateHandoffConflict
	}
	if !found || !bytes.Equal(existing, payload) {
		if linked {
			_ = s.unlinkat(s.handoffFD, sourceAgentUpdateHandoffFileName, 0)
			_ = s.syncDirectory(s.handoffFD)
		}
		return errSourceAgentUpdateHandoffConflict
	}
	unlinkErr := s.unlinkat(s.handoffFD, temporary, 0)
	unlinked := unlinkErr == nil || errors.Is(unlinkErr, unix.ENOENT)
	if err := s.syncDirectory(s.handoffFD); err != nil || s.validatePinned() != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	if !unlinked {
		return errSourceAgentUpdateBridgeUnavailable
	}
	return nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) validatePinned() error {
	if s.parentFD < 0 || s.stagingFD < 0 || s.handoffFD < 0 {
		return errSourceAgentUpdateBridgeUnavailable
	}
	var installStat unix.Stat_t
	if unix.Fstat(s.parentFD, &installStat) != nil || darwinSourceAgentIdentity(installStat) != s.installIdentity ||
		installStat.Mode&unix.S_IFMT != unix.S_IFDIR || !darwinSourceAgentModeIsExact(installStat.Mode, 0o700) ||
		installStat.Uid != uint32(unix.Geteuid()) || uint64(installStat.Dev) != s.workerDevice {
		return errSourceAgentUpdateHandoffConflict
	}
	for _, pin := range s.pathPins {
		if !darwinSourceAgentPathEntryMatches(pin) {
			return errSourceAgentUpdateHandoffConflict
		}
	}
	if !darwinSourceAgentEntryMatches(s.parentFD, s.workerName, s.workerIdentity, unix.S_IFREG, 0o755, s.workerDevice) ||
		!darwinSourceAgentEntryMatches(s.parentFD, sourceAgentUpdateUpdaterBasename, s.updaterIdentity, unix.S_IFREG, 0o755, s.workerDevice) ||
		!darwinSourceAgentEntryMatches(s.parentFD, sourceAgentUpdateLifecycleFileName, s.lifecycleIdentity, unix.S_IFREG, 0o600, s.workerDevice) ||
		!darwinSourceAgentEntryMatches(s.parentFD, sourceAgentUpdateStagingDirectoryName, s.stagingIdentity, unix.S_IFDIR, 0o700, s.workerDevice) ||
		!darwinSourceAgentEntryMatches(s.parentFD, sourceAgentUpdateHandoffDirectoryName, s.handoffIdentity, unix.S_IFDIR, 0o700, s.workerDevice) {
		return errSourceAgentUpdateHandoffConflict
	}
	return nil
}

func openDarwinSourceAgentLifecycleFile(parentFD int, device uint64) (unix.Stat_t, error) {
	fd, err := unix.Openat(
		parentFD,
		sourceAgentUpdateLifecycleFileName,
		unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_CREAT,
		0o600,
	)
	if err != nil {
		return unix.Stat_t{}, errSourceAgentUpdateBridgeUnavailable
	}
	var stat unix.Stat_t
	statErr := unix.Fstat(fd, &stat)
	syncErr := unix.Fsync(fd)
	closeErr := unix.Close(fd)
	if statErr != nil || syncErr != nil || closeErr != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		!darwinSourceAgentModeIsExact(stat.Mode, 0o600) || stat.Uid != uint32(unix.Geteuid()) ||
		uint64(stat.Dev) != device {
		return unix.Stat_t{}, errSourceAgentUpdateHandoffConflict
	}
	if err := unix.Fsync(parentFD); err != nil {
		return unix.Stat_t{}, errSourceAgentUpdateBridgeUnavailable
	}
	return stat, nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) syncDirectory(fd int) error {
	if err := s.syncFD(fd); err != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	return nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) removePending(
	directoryFD int,
	name string,
	maximum int64,
	allowedModes ...uint16,
) error {
	var entry unix.Stat_t
	if err := unix.Fstatat(directoryFD, name, &entry, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		if err := s.syncDirectory(directoryFD); err != nil || s.validatePinned() != nil {
			return errSourceAgentUpdateBridgeUnavailable
		}
		return nil
	} else if err != nil || entry.Mode&unix.S_IFMT != unix.S_IFREG || entry.Uid != uint32(unix.Geteuid()) ||
		uint64(entry.Dev) != s.workerDevice || entry.Size < 0 || entry.Size > maximum {
		return errSourceAgentUpdateHandoffConflict
	}
	modeAllowed := false
	for _, allowedMode := range allowedModes {
		if darwinSourceAgentModeIsExact(entry.Mode, allowedMode) {
			modeAllowed = true
			break
		}
	}
	if !modeAllowed {
		return errSourceAgentUpdateHandoffConflict
	}
	fd, err := unix.Openat(directoryFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return errSourceAgentUpdateHandoffConflict
	}
	var pinned unix.Stat_t
	statErr := unix.Fstat(fd, &pinned)
	closeErr := unix.Close(fd)
	if statErr != nil || closeErr != nil || pinned.Mode&unix.S_IFMT != unix.S_IFREG ||
		pinned.Mode != entry.Mode || pinned.Dev != entry.Dev || pinned.Ino != entry.Ino ||
		pinned.Uid != entry.Uid || pinned.Size != entry.Size {
		return errSourceAgentUpdateHandoffConflict
	}
	if err := s.unlinkat(directoryFD, name, 0); err != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	if err := s.syncDirectory(directoryFD); err != nil || s.validatePinned() != nil {
		return errSourceAgentUpdateBridgeUnavailable
	}
	return nil
}

func (s *darwinSourceAgentUpdateBridgeStorage) Close() error {
	var closeErr error
	for _, target := range []*int{&s.handoffFD, &s.stagingFD, &s.parentFD} {
		if *target >= 0 {
			if err := unix.Close(*target); err != nil && closeErr == nil {
				closeErr = err
			}
			*target = -1
		}
	}
	for index := len(s.pathPins) - 1; index >= 0; index-- {
		if s.pathPins[index].directoryFD >= 0 {
			if err := unix.Close(s.pathPins[index].directoryFD); err != nil && closeErr == nil {
				closeErr = err
			}
			s.pathPins[index].directoryFD = -1
		}
	}
	return closeErr
}

type sourceAgentUpdateContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *sourceAgentUpdateContextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func openDarwinSourceAgentAbsoluteDirectoryNoFollow(path string) (int, []darwinSourceAgentUpdatePathPin, error) {
	if !isCleanAbsoluteSourceAgentUpdatePath(path) || path == string(filepath.Separator) {
		return -1, nil, errSourceAgentUpdateBridgeUnavailable
	}
	current, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, nil, err
	}
	pins := make([]darwinSourceAgentUpdatePathPin, 0, 8)
	closePins := func() {
		_ = unix.Close(current)
		for index := len(pins) - 1; index >= 0; index-- {
			_ = unix.Close(pins[index].directoryFD)
		}
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if !validSourceAgentUpdateLocalName(part) {
			closePins()
			return -1, nil, errSourceAgentUpdateBridgeUnavailable
		}
		next, openErr := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			closePins()
			return -1, nil, errSourceAgentUpdateBridgeUnavailable
		}
		var stat unix.Stat_t
		if unix.Fstat(next, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			_ = unix.Close(next)
			closePins()
			return -1, nil, errSourceAgentUpdateBridgeUnavailable
		}
		pins = append(pins, darwinSourceAgentUpdatePathPin{
			directoryFD: current, name: part, identity: darwinSourceAgentIdentity(stat),
		})
		current = next
	}
	return current, pins, nil
}

func openDarwinSourceAgentInstalledExecutable(parentFD int, name string) (unix.Stat_t, error) {
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return unix.Stat_t{}, err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != uint32(unix.Geteuid()) || !darwinSourceAgentModeIsExact(stat.Mode, 0o755) || stat.Size <= 0 {
		return unix.Stat_t{}, errSourceAgentUpdateBridgeUnavailable
	}
	return stat, nil
}

func darwinSourceAgentPathEntryMatches(pin darwinSourceAgentUpdatePathPin) bool {
	var stat unix.Stat_t
	return pin.directoryFD >= 0 && unix.Fstatat(pin.directoryFD, pin.name, &stat, unix.AT_SYMLINK_NOFOLLOW) == nil &&
		stat.Mode&unix.S_IFMT == unix.S_IFDIR && darwinSourceAgentIdentity(stat) == pin.identity
}

func openDarwinSourceAgentPrivateChildDirectory(
	parentFD int,
	name string,
	device uint64,
) (int, darwinSourceAgentUpdateEntryIdentity, error) {
	if err := unix.Mkdirat(parentFD, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, darwinSourceAgentUpdateEntryIdentity{}, err
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, darwinSourceAgentUpdateEntryIdentity{}, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		!darwinSourceAgentModeIsExact(stat.Mode, 0o700) || stat.Uid != uint32(unix.Geteuid()) || uint64(stat.Dev) != device {
		_ = unix.Close(fd)
		return -1, darwinSourceAgentUpdateEntryIdentity{}, errSourceAgentUpdateBridgeUnavailable
	}
	return fd, darwinSourceAgentIdentity(stat), nil
}

func darwinSourceAgentIdentity(stat unix.Stat_t) darwinSourceAgentUpdateEntryIdentity {
	return darwinSourceAgentUpdateEntryIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}
}

func darwinSourceAgentEntryMatches(
	parentFD int,
	name string,
	identity darwinSourceAgentUpdateEntryIdentity,
	kind uint16,
	mode uint16,
	device uint64,
) bool {
	var stat unix.Stat_t
	if unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		stat.Mode&unix.S_IFMT != kind || uint64(stat.Dev) != identity.device || uint64(stat.Ino) != identity.inode ||
		uint64(stat.Dev) != device || stat.Uid != uint32(unix.Geteuid()) {
		return false
	}
	return darwinSourceAgentModeIsExact(stat.Mode, mode)
}

func darwinSourceAgentModeIsExact(mode uint16, expected uint16) bool {
	const permissionAndSpecialMask = uint16(0o777 | unix.S_ISUID | unix.S_ISGID | unix.S_ISVTX)
	return mode&permissionAndSpecialMask == expected
}

func darwinSourceAgentPendingEntryMatches(directoryFD int, name string, expected unix.Stat_t) bool {
	var entry unix.Stat_t
	return unix.Fstatat(directoryFD, name, &entry, unix.AT_SYMLINK_NOFOLLOW) == nil &&
		entry.Mode == expected.Mode && entry.Uid == expected.Uid && entry.Dev == expected.Dev &&
		entry.Ino == expected.Ino && entry.Size == expected.Size && entry.Mtim == expected.Mtim &&
		entry.Ctim == expected.Ctim
}

func createDarwinSourceAgentBridgePending(directoryFD int, name string) (int, error) {
	fd, err := unix.Openat(
		directoryFD, name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0o600,
	)
	if err != nil {
		return -1, err
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size != 0 ||
		stat.Uid != uint32(unix.Geteuid()) || !darwinSourceAgentModeIsExact(stat.Mode, 0o600) {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(directoryFD, name, 0)
		return -1, errSourceAgentUpdateBridgeUnavailable
	}
	return fd, nil
}

func readDarwinSourceAgentFixedFile(
	directoryFD int,
	name string,
	mode uint16,
	maximum int64,
	device uint64,
	afterRead func(int),
) ([]byte, bool, error) {
	var entry unix.Stat_t
	if err := unix.Fstatat(directoryFD, name, &entry, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	} else if err != nil || entry.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, true, errSourceAgentUpdateHandoffConflict
	}
	fd, err := unix.Openat(directoryFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, true, errSourceAgentUpdateHandoffConflict
	}
	file := os.NewFile(uintptr(fd), "source-agent-update-fixed-file")
	if file == nil {
		_ = unix.Close(fd)
		return nil, true, errSourceAgentUpdateBridgeUnavailable
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		!darwinSourceAgentModeIsExact(stat.Mode, mode) || stat.Uid != uint32(unix.Geteuid()) || uint64(stat.Dev) != device ||
		stat.Size <= 0 || stat.Size > maximum || uint64(stat.Dev) != uint64(entry.Dev) || uint64(stat.Ino) != uint64(entry.Ino) {
		return nil, true, errSourceAgentUpdateHandoffConflict
	}
	payload, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(payload)) != stat.Size || int64(len(payload)) > maximum {
		return nil, true, errSourceAgentUpdateHandoffConflict
	}
	if afterRead != nil {
		afterRead(fd)
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || after.Mode != stat.Mode || after.Uid != stat.Uid ||
		after.Dev != stat.Dev || after.Ino != stat.Ino || after.Size != stat.Size ||
		after.Mtim != stat.Mtim || after.Ctim != stat.Ctim {
		return nil, true, errSourceAgentUpdateHandoffConflict
	}
	if !darwinSourceAgentFixedEntryMatchesStat(directoryFD, name, after) {
		return nil, true, errSourceAgentUpdateHandoffConflict
	}
	return payload, true, nil
}

func darwinSourceAgentFixedEntryMatchesStat(directoryFD int, name string, expected unix.Stat_t) bool {
	var entry unix.Stat_t
	return unix.Fstatat(directoryFD, name, &entry, unix.AT_SYMLINK_NOFOLLOW) == nil &&
		entry.Mode == expected.Mode && entry.Uid == expected.Uid && entry.Dev == expected.Dev &&
		entry.Ino == expected.Ino && entry.Size == expected.Size && entry.Mtim == expected.Mtim &&
		entry.Ctim == expected.Ctim
}

func verifyDarwinSourceAgentStagedFile(
	directoryFD int,
	name string,
	expectedSize int64,
	expectedSHA256 string,
	device uint64,
	afterRead func(int),
) (bool, error) {
	if expectedSize <= 0 || expectedSize > sourceAgentArtifactMaxBytes ||
		!isExactLowerHex(expectedSHA256, sha256.Size*2) {
		return false, errSourceAgentUpdateHandoffConflict
	}
	var entry unix.Stat_t
	if err := unix.Fstatat(directoryFD, name, &entry, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return false, nil
	} else if err != nil || entry.Mode&unix.S_IFMT != unix.S_IFREG {
		return true, errSourceAgentUpdateHandoffConflict
	}
	fd, err := unix.Openat(directoryFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return true, errSourceAgentUpdateHandoffConflict
	}
	file := os.NewFile(uintptr(fd), "source-agent-update-staged-file")
	if file == nil {
		_ = unix.Close(fd)
		return true, errSourceAgentUpdateBridgeUnavailable
	}
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG ||
		!darwinSourceAgentModeIsExact(before.Mode, 0o755) || before.Uid != uint32(unix.Geteuid()) || uint64(before.Dev) != device ||
		before.Size != expectedSize || uint64(before.Dev) != uint64(entry.Dev) || uint64(before.Ino) != uint64(entry.Ino) {
		return true, errSourceAgentUpdateHandoffConflict
	}
	hasher := sha256.New()
	read, readErr := io.Copy(hasher, io.LimitReader(file, expectedSize+1))
	if readErr != nil || read != expectedSize || fmt.Sprintf("%x", hasher.Sum(nil)) != expectedSHA256 {
		return true, errSourceAgentUpdateHandoffConflict
	}
	if afterRead != nil {
		afterRead(fd)
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || after.Mode != before.Mode || after.Uid != before.Uid ||
		after.Dev != before.Dev || after.Ino != before.Ino || after.Size != before.Size ||
		after.Mtim != before.Mtim || after.Ctim != before.Ctim {
		return true, errSourceAgentUpdateHandoffConflict
	}
	if !darwinSourceAgentFixedEntryMatchesStat(directoryFD, name, after) {
		return true, errSourceAgentUpdateHandoffConflict
	}
	return true, nil
}

func writeDarwinSourceAgentBridgeFile(fd int, payload []byte) error {
	for len(payload) > 0 {
		written, err := unix.Write(fd, payload)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || written <= 0 {
			return errSourceAgentUpdateBridgeUnavailable
		}
		payload = payload[written:]
	}
	return nil
}
