package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gofrs/flock"
)

const (
	SourceAgentUpdateOutcomeSucceeded  = "succeeded"
	SourceAgentUpdateOutcomeRolledBack = "rolled_back"
	SourceAgentUpdateOutcomeFailed     = "failed"

	SourceAgentUpdateCodeBusy     = "upgrade_busy"
	SourceAgentUpdateCodeCanceled = "upgrade_canceled"

	sourceAgentUpdateCodeInvalidRequest = "upgrade_request_invalid"
	sourceAgentUpdateReceiptMaxBytes    = 8 << 10
	sourceAgentUpdateDefaultTimeout     = 2 * time.Minute
	sourceAgentUpdateDefaultPoll        = 100 * time.Millisecond
)

var (
	ErrSourceAgentUpdateBusy    = errors.New("source agent update is busy")
	ErrSourceAgentReadyTimeout  = errors.New("source agent ready receipt timed out")
	ErrSourceAgentReadyMismatch = errors.New("source agent ready receipt does not match")
	ErrSourceAgentReadyInvalid  = errors.New("source agent ready receipt is invalid")
)

// SourceAgentUpdateRequest contains only locally resolved artifact metadata.
// StagedBinary is resolved beneath the constructor's fixed staging root; it is
// never accepted directly from a SourceAgentCommand or another remote payload.
type SourceAgentUpdateRequest struct {
	CommandID      string
	WorkerType     string
	CurrentVersion string
	TargetVersion  string
	ExpectedSHA256 string
	ExpectedSize   int64
	StagedBinary   string

	Platform        string
	Architecture    string
	ProtocolVersion string
	Revision        string
	Channel         string
}

type SourceAgentUpdateResult struct {
	WorkerType         string `json:"worker_type"`
	Platform           string `json:"platform"`
	Architecture       string `json:"architecture"`
	Channel            string `json:"channel"`
	ProtocolVersion    string `json:"protocol_version"`
	RuntimeVersion     string `json:"runtime_version"`
	CommandID          string `json:"command_id"`
	Revision           string `json:"revision"`
	RequestFingerprint string `json:"request_fingerprint"`
	Outcome            string `json:"outcome"`
	Code               string `json:"code"`
	Message            string `json:"message"`
	DurationMillis     int64  `json:"duration_ms"`
	BinaryRestored     bool   `json:"binary_restored,omitempty"`
}

type SourceAgentUpdateGuardCheck struct {
	CommandID  string
	WorkerType string
	Version    string
	Revision   string
	Channel    string
}

type SourceAgentUpdateGuard interface {
	Check(context.Context, SourceAgentUpdateGuardCheck) error
}

type SourceAgentUpdateProcessControl interface {
	Restart(context.Context) error
}

type SourceAgentUpdateStagedFile interface {
	io.Reader
	Stat() (os.FileInfo, error)
	Close() error
}

type SourceAgentUpdatePreparedFile interface {
	io.Writer
	Name() string
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

type SourceAgentUpdateFileSystem interface {
	OpenStaged(string, string) (SourceAgentUpdateStagedFile, error)
	CreatePrepared(string) (SourceAgentUpdatePreparedFile, error)
	BackupExecutable(string, string) error
	ReplaceExecutable(string, string) error
	RestoreExecutable(string, string) error
	SyncDirectory(string) error
	Remove(string) error
}

type SourceAgentReadyExpectation struct {
	CommandID       string
	WorkerType      string
	Version         string
	Platform        string
	Architecture    string
	ProtocolVersion string
	Revision        string
}

type SourceAgentReadyReceipt struct {
	CommandID              string `json:"command_id"`
	WorkerType             string `json:"worker_type"`
	Version                string `json:"version"`
	Platform               string `json:"platform"`
	Architecture           string `json:"architecture"`
	ProtocolVersion        string `json:"protocol_version"`
	Revision               string `json:"revision"`
	HeartbeatAuthenticated bool   `json:"heartbeat_authenticated"`
}

type SourceAgentUpdateReceiptStore interface {
	Acquire(context.Context, string) (func(), error)
	LoadOutcome(string) (SourceAgentUpdateResult, bool, error)
	SaveOutcome(SourceAgentUpdateResult) error
	WaitReady(context.Context, SourceAgentReadyExpectation, time.Duration) error
}

type SourceAgentUpdateClock interface {
	Now() time.Time
}

type SourceAgentUpdateConfig struct {
	WorkerType      string
	Platform        string
	Architecture    string
	CurrentVersion  string
	ProtocolVersion string

	CurrentExecutable string
	StagingRoot       string
	BackupRoot        string
	ReceiptRoot       string

	ReadyTimeout   time.Duration
	FileSystem     SourceAgentUpdateFileSystem
	ProcessControl SourceAgentUpdateProcessControl
	Guard          SourceAgentUpdateGuard
	Receipts       SourceAgentUpdateReceiptStore
	Clock          SourceAgentUpdateClock
}

type SourceAgentUpdateTransaction struct {
	config   SourceAgentUpdateConfig
	fs       SourceAgentUpdateFileSystem
	process  SourceAgentUpdateProcessControl
	guard    SourceAgentUpdateGuard
	receipts SourceAgentUpdateReceiptStore
	clock    SourceAgentUpdateClock
}

type realSourceAgentUpdateClock struct{}

func (realSourceAgentUpdateClock) Now() time.Time { return time.Now() }

func NewSourceAgentUpdateTransaction(config SourceAgentUpdateConfig) (*SourceAgentUpdateTransaction, error) {
	if !isExactSourceAgentArtifactName("worker_type", config.WorkerType) ||
		!isExactSourceAgentArtifactName("platform", config.Platform) ||
		!isExactSourceAgentArtifactName("architecture", config.Architecture) ||
		!isSourceAgentArtifactVersion(config.CurrentVersion) ||
		!isExactSourceAgentProtocolVersion(config.ProtocolVersion) {
		return nil, errors.New("invalid fixed source agent update identity")
	}
	if config.Guard == nil || config.ProcessControl == nil {
		return nil, errors.New("source agent update guard and process control are required")
	}
	for _, path := range []string{config.CurrentExecutable, config.StagingRoot, config.BackupRoot, config.ReceiptRoot} {
		if !isCleanAbsoluteSourceAgentUpdatePath(path) {
			return nil, errors.New("source agent update local paths must be fixed absolute paths")
		}
	}
	if filepath.Clean(config.BackupRoot) != filepath.Dir(filepath.Clean(config.CurrentExecutable)) {
		return nil, errors.New("source agent update backup root must be the executable directory")
	}
	if config.ReadyTimeout <= 0 {
		config.ReadyTimeout = sourceAgentUpdateDefaultTimeout
	}
	if config.ReadyTimeout > 10*time.Minute {
		return nil, errors.New("source agent update ready timeout is too large")
	}
	fs := config.FileSystem
	if fs == nil {
		fs = NewOSSourceAgentUpdateFileSystem()
	}
	receipts := config.Receipts
	if receipts == nil {
		var err error
		receipts, err = NewFileSourceAgentUpdateReceiptStore(config.ReceiptRoot, sourceAgentUpdateDefaultPoll)
		if err != nil {
			return nil, err
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = realSourceAgentUpdateClock{}
	}
	return &SourceAgentUpdateTransaction{
		config: config, fs: fs, process: config.ProcessControl, guard: config.Guard,
		receipts: receipts, clock: clock,
	}, nil
}

func (u *SourceAgentUpdateTransaction) Apply(ctx context.Context, request SourceAgentUpdateRequest) SourceAgentUpdateResult {
	started := u.clock.Now()
	result := u.baseResult(request)
	finish := func(outcome, code string, restored bool) SourceAgentUpdateResult {
		result.Outcome = outcome
		result.Code = code
		result.Message = sourceAgentUpdatePublicMessage(code)
		result.BinaryRestored = restored
		duration := u.clock.Now().Sub(started)
		if duration < 0 {
			duration = 0
		}
		result.DurationMillis = duration.Milliseconds()
		return result
	}

	if code := u.validateRequest(request); code != "" {
		return finish(SourceAgentUpdateOutcomeFailed, code, false)
	}
	if ctx.Err() != nil {
		return finish(SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeCanceled, false)
	}
	release, err := u.receipts.Acquire(ctx, request.CommandID)
	if err != nil {
		code := SourceAgentUpdateCodeBusy
		if ctx.Err() != nil {
			code = SourceAgentUpdateCodeCanceled
		}
		return finish(SourceAgentUpdateOutcomeFailed, code, false)
	}
	defer release()

	if previous, ok, loadErr := u.receipts.LoadOutcome(request.CommandID); loadErr == nil && ok {
		if sourceAgentUpdateOutcomeMatches(previous, request) {
			return previous
		}
		return finish(SourceAgentUpdateOutcomeFailed, sourceAgentUpdateCodeInvalidRequest, false)
	} else if loadErr != nil {
		return finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false)
	}

	guardCheck := SourceAgentUpdateGuardCheck{
		CommandID: request.CommandID, WorkerType: request.WorkerType,
		Version: request.TargetVersion, Revision: request.Revision, Channel: request.Channel,
	}
	if ctx.Err() != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeCanceled, false))
	}
	// This guard is deliberately before the first prepared/backup mutation.
	if err := u.guard.Check(ctx, guardCheck); err != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}

	staged, err := u.fs.OpenStaged(u.config.StagingRoot, request.StagedBinary)
	if err != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeVerificationFailed, false))
	}
	defer staged.Close()
	info, err := staged.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != request.ExpectedSize {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeVerificationFailed, false))
	}

	prepared, err := u.fs.CreatePrepared(u.config.CurrentExecutable)
	if err != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}
	preparedPath := prepared.Name()
	preparedClosed := false
	defer func() {
		if !preparedClosed {
			_ = prepared.Close()
		}
		_ = u.fs.Remove(preparedPath)
	}()

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(prepared, hasher), io.LimitReader(staged, sourceAgentArtifactMaxBytes+1))
	closeStagedErr := staged.Close()
	if copyErr != nil || closeStagedErr != nil || written != request.ExpectedSize ||
		fmt.Sprintf("%x", hasher.Sum(nil)) != request.ExpectedSHA256 {
		_ = prepared.Close()
		preparedClosed = true
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeVerificationFailed, false))
	}
	if err := prepared.Chmod(0o755); err != nil {
		_ = prepared.Close()
		preparedClosed = true
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}
	if err := prepared.Sync(); err != nil {
		_ = prepared.Close()
		preparedClosed = true
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}
	if err := prepared.Close(); err != nil {
		preparedClosed = true
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}
	preparedClosed = true
	if err := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable)); err != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}

	if ctx.Err() != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeCanceled, false))
	}
	// The second guard is adjacent to backup/replace so idle and rollout state
	// cannot be checked only at download time.
	if err := u.guard.Check(ctx, guardCheck); err != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}
	if ctx.Err() != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeCanceled, false))
	}

	backupPath := filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupName(request.CommandID))
	if err := u.fs.BackupExecutable(u.config.CurrentExecutable, backupPath); err != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}
	if err := u.fs.SyncDirectory(u.config.BackupRoot); err != nil {
		_ = u.fs.Remove(backupPath)
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}
	if ctx.Err() != nil {
		_ = u.fs.Remove(backupPath)
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeCanceled, false))
	}
	if err := u.fs.ReplaceExecutable(preparedPath, u.config.CurrentExecutable); err != nil {
		_ = u.fs.Remove(backupPath)
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false))
	}
	if err := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable)); err != nil {
		return u.rollback(context.WithoutCancel(ctx), backupPath, request, finish)
	}

	if ctx.Err() != nil {
		return u.rollback(context.WithoutCancel(ctx), backupPath, request, finish)
	}
	if err := u.process.Restart(ctx); err != nil {
		return u.rollback(context.WithoutCancel(ctx), backupPath, request, finish)
	}
	expectation := SourceAgentReadyExpectation{
		CommandID: request.CommandID, WorkerType: request.WorkerType, Version: request.TargetVersion,
		Platform: request.Platform, Architecture: request.Architecture,
		ProtocolVersion: request.ProtocolVersion, Revision: request.Revision,
	}
	if err := u.receipts.WaitReady(ctx, expectation, u.config.ReadyTimeout); err != nil {
		return u.rollback(context.WithoutCancel(ctx), backupPath, request, finish)
	}

	result.RuntimeVersion = request.TargetVersion
	succeeded := finish(SourceAgentUpdateOutcomeSucceeded, SourceAgentCommandCodeUpgradeComplete, false)
	if err := u.receipts.SaveOutcome(succeeded); err != nil {
		return u.rollback(context.WithoutCancel(ctx), backupPath, request, finish)
	}
	_ = u.fs.Remove(backupPath)
	_ = u.fs.SyncDirectory(u.config.BackupRoot)
	return succeeded
}

func (u *SourceAgentUpdateTransaction) rollback(
	ctx context.Context,
	backupPath string,
	request SourceAgentUpdateRequest,
	finish func(string, string, bool) SourceAgentUpdateResult,
) SourceAgentUpdateResult {
	if err := u.fs.RestoreExecutable(backupPath, u.config.CurrentExecutable); err != nil {
		return u.saveOutcome(finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeRollbackFailed, false))
	}
	syncErr := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable))
	restartErr := u.process.Restart(ctx)
	if syncErr == nil {
		_ = u.fs.Remove(backupPath)
		_ = u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable))
	}
	if syncErr != nil || restartErr != nil {
		result := finish(SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeRollbackFailed, true)
		result.RuntimeVersion = request.CurrentVersion
		return u.saveOutcome(result)
	}
	result := finish(SourceAgentUpdateOutcomeRolledBack, SourceAgentCommandCodeRollbackComplete, true)
	result.RuntimeVersion = request.CurrentVersion
	return u.saveOutcome(result)
}

func (u *SourceAgentUpdateTransaction) saveOutcome(result SourceAgentUpdateResult) SourceAgentUpdateResult {
	if err := u.receipts.SaveOutcome(result); err != nil {
		result.Outcome = SourceAgentUpdateOutcomeFailed
		result.Code = SourceAgentCommandCodeInstallFailed
		result.Message = sourceAgentUpdatePublicMessage(result.Code)
	}
	return result
}

func (u *SourceAgentUpdateTransaction) validateRequest(request SourceAgentUpdateRequest) string {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", request.CommandID, true)
	if err != nil || commandID != request.CommandID || request.WorkerType != u.config.WorkerType ||
		request.Platform != u.config.Platform || request.Architecture != u.config.Architecture ||
		request.ProtocolVersion != u.config.ProtocolVersion || !isExactSourceAgentProtocolVersion(request.ProtocolVersion) ||
		request.CurrentVersion != u.config.CurrentVersion || !isSourceAgentArtifactVersion(request.CurrentVersion) ||
		!isSourceAgentArtifactVersion(request.TargetVersion) ||
		compareSourceAgentArtifactVersions(request.CurrentVersion, request.TargetVersion) >= 0 ||
		(!isExactLowerHex(request.Revision, 40) && !isExactLowerHex(request.Revision, 64)) ||
		(request.Channel != "staging" && request.Channel != "production") ||
		request.ExpectedSize <= 0 || request.ExpectedSize > sourceAgentArtifactMaxBytes ||
		!isExactLowerHex(request.ExpectedSHA256, sha256.Size*2) ||
		!isSourceAgentUpdateChildPath(u.config.StagingRoot, request.StagedBinary) {
		return sourceAgentUpdateCodeInvalidRequest
	}
	return ""
}

func (u *SourceAgentUpdateTransaction) baseResult(request SourceAgentUpdateRequest) SourceAgentUpdateResult {
	return SourceAgentUpdateResult{
		WorkerType: request.WorkerType, Platform: request.Platform, Architecture: request.Architecture, Channel: request.Channel,
		ProtocolVersion: request.ProtocolVersion, RuntimeVersion: request.CurrentVersion,
		CommandID: request.CommandID, Revision: request.Revision, RequestFingerprint: sourceAgentUpdateRequestFingerprint(request),
	}
}

func sourceAgentUpdateOutcomeMatches(result SourceAgentUpdateResult, request SourceAgentUpdateRequest) bool {
	return result.CommandID == request.CommandID && result.Platform == request.Platform &&
		result.WorkerType == request.WorkerType && result.Architecture == request.Architecture && result.Channel == request.Channel &&
		result.ProtocolVersion == request.ProtocolVersion && result.Revision == request.Revision &&
		result.RequestFingerprint == sourceAgentUpdateRequestFingerprint(request) &&
		(result.RuntimeVersion == request.TargetVersion || result.RuntimeVersion == request.CurrentVersion) &&
		(result.Outcome == SourceAgentUpdateOutcomeSucceeded || result.Outcome == SourceAgentUpdateOutcomeRolledBack ||
			result.Outcome == SourceAgentUpdateOutcomeFailed)
}

// The local staged path is intentionally excluded: the artifact identity is
// the verified revision, target, byte size, and SHA-256, not a mutable path.
func sourceAgentUpdateRequestFingerprint(request SourceAgentUpdateRequest) string {
	canonical := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s",
		request.CommandID, request.WorkerType, request.CurrentVersion, request.TargetVersion,
		request.ExpectedSHA256, request.ExpectedSize, request.Platform, request.Architecture,
		request.ProtocolVersion, request.Revision, request.Channel)
	digest := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", digest)
}

func sourceAgentUpdatePublicMessage(code string) string {
	switch code {
	case SourceAgentCommandCodeUpgradeComplete:
		return "Upgrade completed."
	case SourceAgentCommandCodeVerificationFailed:
		return "Upgrade verification failed."
	case SourceAgentCommandCodeRestartFailed:
		return "Upgrade restart failed."
	case SourceAgentCommandCodeRollbackComplete:
		return "Upgrade rolled back."
	case SourceAgentCommandCodeRollbackFailed:
		return "Upgrade rollback failed."
	case SourceAgentUpdateCodeBusy:
		return "Another upgrade is active."
	case SourceAgentUpdateCodeCanceled:
		return "Upgrade was canceled before installation."
	case sourceAgentUpdateCodeInvalidRequest:
		return "Upgrade request is invalid."
	default:
		return "Upgrade installation failed."
	}
}

func isExactSourceAgentProtocolVersion(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func isCleanAbsoluteSourceAgentUpdatePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func isSourceAgentUpdateChildPath(root, child string) bool {
	if !isCleanAbsoluteSourceAgentUpdatePath(root) || !isCleanAbsoluteSourceAgentUpdatePath(child) {
		return false
	}
	relative, err := filepath.Rel(root, child)
	return err == nil && relative != "." && relative != "" && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func sourceAgentUpdateBackupName(commandID string) string {
	digest := sha256.Sum256([]byte(commandID))
	return fmt.Sprintf(".source-agent-backup-%x", digest[:16])
}

type osSourceAgentUpdateFileSystem struct{}

func NewOSSourceAgentUpdateFileSystem() SourceAgentUpdateFileSystem {
	return osSourceAgentUpdateFileSystem{}
}

func (osSourceAgentUpdateFileSystem) OpenStaged(root, path string) (SourceAgentUpdateStagedFile, error) {
	if !isSourceAgentUpdateChildPath(root, path) {
		return nil, errors.New("staged binary is outside the fixed staging root")
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	file, err := openSourceAgentArtifactRelative(root, strings.Split(filepath.ToSlash(relative), "/"))
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("staged binary is not a regular no-follow file")
	}
	return file, nil
}

func (osSourceAgentUpdateFileSystem) CreatePrepared(executable string) (SourceAgentUpdatePreparedFile, error) {
	file, err := os.CreateTemp(filepath.Dir(executable), ".source-agent-prepared-*")
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}

func (osSourceAgentUpdateFileSystem) BackupExecutable(executable, backup string) error {
	if filepath.Dir(executable) != filepath.Dir(backup) {
		return errors.New("backup must share the executable directory")
	}
	source, err := openRegularSourceAgentUpdateFile(executable)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		target.Close()
		if !ok {
			os.Remove(backup)
		}
	}()
	written, err := io.Copy(target, io.LimitReader(source, sourceAgentArtifactMaxBytes+1))
	if err != nil || written <= 0 || written > sourceAgentArtifactMaxBytes {
		return errors.New("backup copy failed")
	}
	if err := target.Chmod(0o755); err != nil {
		return err
	}
	if err := target.Sync(); err != nil {
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func (osSourceAgentUpdateFileSystem) ReplaceExecutable(prepared, executable string) error {
	if filepath.Dir(prepared) != filepath.Dir(executable) {
		return errors.New("prepared executable must be on the same filesystem")
	}
	return os.Rename(prepared, executable)
}

func (osSourceAgentUpdateFileSystem) RestoreExecutable(backup, executable string) error {
	if filepath.Dir(backup) != filepath.Dir(executable) {
		return errors.New("backup must share the executable directory")
	}
	source, err := openRegularSourceAgentUpdateFile(backup)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.CreateTemp(filepath.Dir(executable), ".source-agent-restore-*")
	if err != nil {
		return err
	}
	targetPath := target.Name()
	ok := false
	defer func() {
		target.Close()
		if !ok {
			os.Remove(targetPath)
		}
	}()
	if err := target.Chmod(0o600); err != nil {
		return err
	}
	written, err := io.Copy(target, io.LimitReader(source, sourceAgentArtifactMaxBytes+1))
	if err != nil || written <= 0 || written > sourceAgentArtifactMaxBytes {
		return errors.New("restore copy failed")
	}
	if err := target.Chmod(0o755); err != nil {
		return err
	}
	if err := target.Sync(); err != nil {
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	if err := os.Rename(targetPath, executable); err != nil {
		return err
	}
	ok = true
	return nil
}

func (osSourceAgentUpdateFileSystem) SyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
}

func (osSourceAgentUpdateFileSystem) Remove(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func openRegularSourceAgentUpdateFile(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, errors.New("file is not a regular no-follow file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		file.Close()
		return nil, errors.New("file changed while opening")
	}
	return file, nil
}

type FileSourceAgentUpdateReceiptStore struct {
	root         string
	pollInterval time.Duration
}

func NewFileSourceAgentUpdateReceiptStore(root string, pollInterval time.Duration) (*FileSourceAgentUpdateReceiptStore, error) {
	if !isCleanAbsoluteSourceAgentUpdatePath(root) {
		return nil, errors.New("source agent receipt root must be a fixed absolute path")
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("source agent receipt root is unavailable")
	}
	if pollInterval <= 0 {
		pollInterval = sourceAgentUpdateDefaultPoll
	}
	return &FileSourceAgentUpdateReceiptStore{root: root, pollInterval: pollInterval}, nil
}

func (s *FileSourceAgentUpdateReceiptStore) Acquire(ctx context.Context, _ string) (func(), error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	lock := flock.New(filepath.Join(s.root, ".source-agent-update.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return nil, ErrSourceAgentUpdateBusy
	}
	if !locked {
		return nil, ErrSourceAgentUpdateBusy
	}
	return func() { _ = lock.Unlock() }, nil
}

func (s *FileSourceAgentUpdateReceiptStore) readyPath(commandID string) string {
	return filepath.Join(s.root, sourceAgentUpdateReceiptName("ready", commandID))
}

func (s *FileSourceAgentUpdateReceiptStore) outcomePath(commandID string) string {
	return filepath.Join(s.root, sourceAgentUpdateReceiptName("outcome", commandID))
}

func sourceAgentUpdateReceiptName(kind, commandID string) string {
	digest := sha256.Sum256([]byte(commandID))
	return fmt.Sprintf("%s-%x.json", kind, digest[:16])
}

func (s *FileSourceAgentUpdateReceiptStore) WriteReady(receipt SourceAgentReadyReceipt) error {
	if !validSourceAgentReadyReceipt(receipt) {
		return ErrSourceAgentReadyInvalid
	}
	return writeStrictSourceAgentUpdateJSON(s.root, s.readyPath(receipt.CommandID), receipt)
}

func (s *FileSourceAgentUpdateReceiptStore) WaitReady(ctx context.Context, expected SourceAgentReadyExpectation, timeout time.Duration) error {
	if !validSourceAgentReadyExpectation(expected) || timeout <= 0 {
		return ErrSourceAgentReadyInvalid
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		var receipt SourceAgentReadyReceipt
		err := readStrictSourceAgentUpdateJSON(s.readyPath(expected.CommandID), &receipt)
		if err == nil {
			if !validSourceAgentReadyReceipt(receipt) {
				return ErrSourceAgentReadyInvalid
			}
			if !sourceAgentReadyReceiptMatches(receipt, expected) {
				return ErrSourceAgentReadyMismatch
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return ErrSourceAgentReadyInvalid
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return ErrSourceAgentReadyTimeout
		case <-ticker.C:
		}
	}
}

func (s *FileSourceAgentUpdateReceiptStore) LoadOutcome(commandID string) (SourceAgentUpdateResult, bool, error) {
	if normalized, err := normalizeSourceAgentCommandIdentifier("command_id", commandID, true); err != nil || normalized != commandID {
		return SourceAgentUpdateResult{}, false, errors.New("invalid source agent update command")
	}
	var result SourceAgentUpdateResult
	if err := readStrictSourceAgentUpdateJSON(s.outcomePath(commandID), &result); errors.Is(err, os.ErrNotExist) {
		return SourceAgentUpdateResult{}, false, nil
	} else if err != nil || !validSourceAgentUpdateResult(result) {
		return SourceAgentUpdateResult{}, false, errors.New("invalid source agent update outcome")
	}
	return result, true, nil
}

func (s *FileSourceAgentUpdateReceiptStore) SaveOutcome(result SourceAgentUpdateResult) error {
	if !validSourceAgentUpdateResult(result) {
		return errors.New("invalid source agent update outcome")
	}
	return writeStrictSourceAgentUpdateJSON(s.root, s.outcomePath(result.CommandID), result)
}

func validSourceAgentReadyExpectation(value SourceAgentReadyExpectation) bool {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", value.CommandID, true)
	return err == nil && commandID == value.CommandID && isExactSourceAgentArtifactName("worker_type", value.WorkerType) &&
		isSourceAgentArtifactVersion(value.Version) && isExactSourceAgentArtifactName("platform", value.Platform) &&
		isExactSourceAgentArtifactName("architecture", value.Architecture) && isExactSourceAgentProtocolVersion(value.ProtocolVersion) &&
		(isExactLowerHex(value.Revision, 40) || isExactLowerHex(value.Revision, 64))
}

func validSourceAgentReadyReceipt(value SourceAgentReadyReceipt) bool {
	return value.HeartbeatAuthenticated && validSourceAgentReadyExpectation(SourceAgentReadyExpectation{
		CommandID: value.CommandID, WorkerType: value.WorkerType, Version: value.Version,
		Platform: value.Platform, Architecture: value.Architecture,
		ProtocolVersion: value.ProtocolVersion, Revision: value.Revision,
	})
}

func sourceAgentReadyReceiptMatches(receipt SourceAgentReadyReceipt, expected SourceAgentReadyExpectation) bool {
	return receipt.CommandID == expected.CommandID && receipt.WorkerType == expected.WorkerType &&
		receipt.Version == expected.Version && receipt.Platform == expected.Platform &&
		receipt.Architecture == expected.Architecture && receipt.ProtocolVersion == expected.ProtocolVersion &&
		receipt.Revision == expected.Revision && receipt.HeartbeatAuthenticated
}

func validSourceAgentUpdateResult(value SourceAgentUpdateResult) bool {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", value.CommandID, true)
	if err != nil || commandID != value.CommandID || !isExactSourceAgentArtifactName("platform", value.Platform) ||
		!isExactSourceAgentArtifactName("worker_type", value.WorkerType) ||
		!isExactSourceAgentArtifactName("architecture", value.Architecture) || !isExactSourceAgentProtocolVersion(value.ProtocolVersion) ||
		!isSourceAgentArtifactVersion(value.RuntimeVersion) ||
		!isExactLowerHex(value.RequestFingerprint, sha256.Size*2) ||
		(!isExactLowerHex(value.Revision, 40) && !isExactLowerHex(value.Revision, 64)) ||
		(value.Channel != "staging" && value.Channel != "production") || value.DurationMillis < 0 || value.DurationMillis > int64((24*time.Hour)/time.Millisecond) {
		return false
	}
	if value.Outcome != SourceAgentUpdateOutcomeSucceeded && value.Outcome != SourceAgentUpdateOutcomeRolledBack && value.Outcome != SourceAgentUpdateOutcomeFailed {
		return false
	}
	if value.Message != sourceAgentUpdatePublicMessage(value.Code) {
		return false
	}
	allowedCodes := map[string]struct{}{
		SourceAgentCommandCodeUpgradeComplete: {}, SourceAgentCommandCodeVerificationFailed: {},
		SourceAgentCommandCodeInstallFailed: {}, SourceAgentCommandCodeRestartFailed: {},
		SourceAgentCommandCodeRollbackComplete: {}, SourceAgentCommandCodeRollbackFailed: {},
		SourceAgentUpdateCodeCanceled: {}, sourceAgentUpdateCodeInvalidRequest: {},
	}
	_, ok := allowedCodes[value.Code]
	return ok
}

func writeStrictSourceAgentUpdateJSON(root, target string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) > sourceAgentUpdateReceiptMaxBytes {
		return errors.New("source agent update receipt is invalid")
	}
	payload = append(payload, '\n')
	temporary, err := os.CreateTemp(root, ".source-agent-receipt-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		temporary.Close()
		os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		existing, readErr := readStrictSourceAgentUpdatePayload(target)
		if readErr != nil || !bytes.Equal(existing, payload) {
			return errors.New("source agent update receipt conflicts with an existing receipt")
		}
		return nil
	}
	if err := (osSourceAgentUpdateFileSystem{}).SyncDirectory(root); err != nil {
		_ = os.Remove(target)
		_ = (osSourceAgentUpdateFileSystem{}).SyncDirectory(root)
		return err
	}
	return nil
}

func readStrictSourceAgentUpdatePayload(path string) ([]byte, error) {
	file, err := openRegularSourceAgentUpdateFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() <= 0 || info.Size() > sourceAgentUpdateReceiptMaxBytes {
		return nil, errors.New("source agent update receipt is invalid")
	}
	payload, err := io.ReadAll(io.LimitReader(file, sourceAgentUpdateReceiptMaxBytes+1))
	if err != nil || len(payload) > sourceAgentUpdateReceiptMaxBytes {
		return nil, errors.New("source agent update receipt is invalid")
	}
	return payload, nil
}

func readStrictSourceAgentUpdateJSON(path string, target any) error {
	payload, err := readStrictSourceAgentUpdatePayload(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return err
	}
	if rejectDuplicateJSONFields(payload) != nil {
		return errors.New("source agent update receipt is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("source agent update receipt is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("source agent update receipt is invalid")
	}
	return nil
}
