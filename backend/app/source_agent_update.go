package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	SourceAgentUpdateOutcomeSucceeded  = "succeeded"
	SourceAgentUpdateOutcomeRolledBack = "rolled_back"
	SourceAgentUpdateOutcomeFailed     = "failed"

	SourceAgentUpdateCodeBusy                     = "upgrade_busy"
	SourceAgentUpdateCodeCanceled                 = "upgrade_canceled"
	SourceAgentUpdateCodeInterrupted              = "upgrade_interrupted"
	SourceAgentUpdateCodeRecoveryFailed           = "upgrade_recovery_failed"
	SourceAgentUpdateCodeOutcomePersistenceFailed = "outcome_persistence_failed"
	SourceAgentUpdateCodeClosed                   = "upgrade_closed"

	sourceAgentUpdateCodeInvalidRequest            = "upgrade_request_invalid"
	sourceAgentUpdateReceiptMaxBytes               = 8 << 10
	sourceAgentUpdateDefaultTimeout                = 2 * time.Minute
	sourceAgentUpdateDefaultPoll                   = 100 * time.Millisecond
	sourceAgentUpdateDefaultRestartTimeout         = 30 * time.Second
	sourceAgentUpdateMaximumRestartTimeout         = 2 * time.Minute
	sourceAgentUpdateJournalFileName               = ".source-agent-update-journal.json"
	sourceAgentUpdateJournalSchema                 = "source-agent-update-journal.v1"
	sourceAgentUpdateFaultAfterBackup              = "after_backup"
	sourceAgentUpdateFaultAfterReplace             = "after_replace"
	sourceAgentUpdateFaultAfterRestart             = "after_restart"
	sourceAgentUpdateFaultAfterReady               = "after_ready"
	sourceAgentUpdateFaultBeforeOutcome            = "before_outcome"
	sourceAgentUpdateFaultPreReplaceAfterOutcome   = "pre_replace_after_outcome"
	sourceAgentUpdateFaultAfterCleanupBackupRemove = "after_cleanup_backup_remove"
	sourceAgentUpdateFaultAfterCleanupSync         = "after_cleanup_sync"
)

var (
	ErrSourceAgentUpdateBusy               = errors.New("source agent update is busy")
	ErrSourceAgentReadyTimeout             = errors.New("source agent ready receipt timed out")
	ErrSourceAgentReadyMismatch            = errors.New("source agent ready receipt does not match")
	ErrSourceAgentReadyInvalid             = errors.New("source agent ready receipt is invalid")
	errSourceAgentUpdateStorageUnavailable = errors.New("source agent update storage unavailable")
	errSourceAgentUpdateUnsupportedStorage = errors.New("source agent update storage unsupported")
)

type sourceAgentUpdateOutcomePublication uint8

const (
	sourceAgentUpdateOutcomeNotPublished sourceAgentUpdateOutcomePublication = iota
	sourceAgentUpdateOutcomePublished
	sourceAgentUpdateOutcomeDurable
)

type sourceAgentUpdatePublishedError struct {
	cause error
}

func (e *sourceAgentUpdatePublishedError) Error() string { return e.cause.Error() }
func (e *sourceAgentUpdatePublishedError) Unwrap() error { return e.cause }

func newSourceAgentUpdatePublishedError(err error) error {
	return &sourceAgentUpdatePublishedError{cause: err}
}

func isSourceAgentUpdatePublishedError(err error) bool {
	var published *sourceAgentUpdatePublishedError
	return errors.As(err, &published)
}

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
	PersistenceCode    string `json:"persistence_code,omitempty"`
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
	Acquire(context.Context) (func(), error)
	OpenStaged(string, string) (SourceAgentUpdateStagedFile, error)
	CreatePrepared(string, string) (SourceAgentUpdatePreparedFile, error)
	BackupExecutable(string, string) (SourceAgentBinaryIdentity, error)
	ReplaceExecutable(string, string) error
	RestoreExecutable(string, string, string, SourceAgentBinaryIdentity) error
	RegularFileExists(string) (bool, error)
	RegularFileIdentity(string) (SourceAgentBinaryIdentity, error)
	SyncDirectory(string) error
	Remove(string) error
	Close() error
}

type SourceAgentBinaryIdentity struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type SourceAgentReadyExpectation struct {
	CommandID       string
	AttemptNonce    string
	WorkerType      string
	Version         string
	Platform        string
	Architecture    string
	ProtocolVersion string
	Revision        string
}

type SourceAgentReadyReceipt struct {
	CommandID              string `json:"command_id"`
	AttemptNonce           string `json:"attempt_nonce"`
	WorkerType             string `json:"worker_type"`
	Version                string `json:"version"`
	Platform               string `json:"platform"`
	Architecture           string `json:"architecture"`
	ProtocolVersion        string `json:"protocol_version"`
	Revision               string `json:"revision"`
	HeartbeatAuthenticated bool   `json:"heartbeat_authenticated"`
}

// SourceAgentReadyChallenge is the only attempt API needed by Task 10. A
// worker reads it locally after restart and may write the matching receipt
// only after its authenticated heartbeat succeeds.
type SourceAgentReadyChallenge struct {
	CommandID       string `json:"command_id"`
	AttemptNonce    string `json:"attempt_nonce"`
	WorkerType      string `json:"worker_type"`
	Version         string `json:"version"`
	Platform        string `json:"platform"`
	Architecture    string `json:"architecture"`
	ProtocolVersion string `json:"protocol_version"`
	Revision        string `json:"revision"`
}

type sourceAgentUpdateJournal struct {
	SchemaVersion      string                    `json:"schema_version"`
	CommandID          string                    `json:"command_id"`
	AttemptNonce       string                    `json:"attempt_nonce"`
	RequestFingerprint string                    `json:"request_fingerprint"`
	WorkerType         string                    `json:"worker_type"`
	CurrentVersion     string                    `json:"current_version"`
	TargetVersion      string                    `json:"target_version"`
	Platform           string                    `json:"platform"`
	Architecture       string                    `json:"architecture"`
	ProtocolVersion    string                    `json:"protocol_version"`
	Revision           string                    `json:"revision"`
	Channel            string                    `json:"channel"`
	Stage              string                    `json:"stage"`
	Backup             SourceAgentBinaryIdentity `json:"backup"`
	StartedAt          string                    `json:"started_at"`
	UpdatedAt          string                    `json:"updated_at"`
}

type SourceAgentUpdateReceiptStore interface {
	Acquire(context.Context, string) (func(), error)
	LoadOutcome(string) (SourceAgentUpdateResult, bool, error)
	SaveOutcome(SourceAgentUpdateResult) error
	WaitReady(context.Context, SourceAgentReadyExpectation, time.Duration) error
	loadJournal() (sourceAgentUpdateJournal, bool, error)
	saveJournal(sourceAgentUpdateJournal) error
	clearJournal(string, string) error
}

type sourceAgentUpdateDirectory interface {
	acquire(context.Context) (func(), error)
	read(string, int64) ([]byte, error)
	writeImmutable(string, []byte) error
	writeAtomic(string, []byte) error
	remove(string) error
	sync() error
	close() error
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
	RestartTimeout time.Duration
	FileSystem     SourceAgentUpdateFileSystem
	ProcessControl SourceAgentUpdateProcessControl
	Guard          SourceAgentUpdateGuard
	Receipts       SourceAgentUpdateReceiptStore
	Clock          SourceAgentUpdateClock
}

type SourceAgentUpdateTransaction struct {
	config     SourceAgentUpdateConfig
	fs         SourceAgentUpdateFileSystem
	process    SourceAgentUpdateProcessControl
	guard      SourceAgentUpdateGuard
	receipts   SourceAgentUpdateReceiptStore
	clock      SourceAgentUpdateClock
	faultStage string
	lifecycle  sync.RWMutex
	closed     bool
	ownedFS    bool
	ownedStore bool
}

type realSourceAgentUpdateClock struct{}

func (realSourceAgentUpdateClock) Now() time.Time { return time.Now() }

func NewSourceAgentUpdateTransaction(config SourceAgentUpdateConfig) (*SourceAgentUpdateTransaction, error) {
	if !isAllowedSourceAgentUpdateWorkerType(config.WorkerType) ||
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
	if config.RestartTimeout <= 0 {
		config.RestartTimeout = sourceAgentUpdateDefaultRestartTimeout
	}
	if config.RestartTimeout > sourceAgentUpdateMaximumRestartTimeout {
		return nil, errors.New("source agent update restart timeout is too large")
	}
	fs := config.FileSystem
	ownedFS := false
	if fs == nil {
		var err error
		fs, err = NewOSSourceAgentUpdateFileSystem(config.CurrentExecutable)
		if err != nil {
			return nil, err
		}
		ownedFS = true
	}
	receipts := config.Receipts
	ownedStore := false
	if receipts == nil {
		var err error
		receipts, err = NewFileSourceAgentUpdateReceiptStore(config.ReceiptRoot, sourceAgentUpdateDefaultPoll)
		if err != nil {
			if ownedFS {
				_ = fs.Close()
			}
			return nil, err
		}
		ownedStore = true
	}
	clock := config.Clock
	if clock == nil {
		clock = realSourceAgentUpdateClock{}
	}
	return &SourceAgentUpdateTransaction{
		config: config, fs: fs, process: config.ProcessControl, guard: config.Guard,
		receipts: receipts, clock: clock, ownedFS: ownedFS, ownedStore: ownedStore,
	}, nil
}

func (u *SourceAgentUpdateTransaction) Close() error {
	u.lifecycle.Lock()
	defer u.lifecycle.Unlock()
	if u.closed {
		return nil
	}
	u.closed = true
	var closeErrors []error
	if u.ownedStore {
		if closer, ok := u.receipts.(interface{ Close() error }); ok {
			closeErrors = append(closeErrors, closer.Close())
		}
	}
	if u.ownedFS {
		closeErrors = append(closeErrors, u.fs.Close())
	}
	return errors.Join(closeErrors...)
}

func (u *SourceAgentUpdateTransaction) restart(ctx context.Context, ignoreCancellation bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ignoreCancellation {
		ctx = context.WithoutCancel(ctx)
	}
	restartCtx, cancel := context.WithTimeout(ctx, u.config.RestartTimeout)
	defer cancel()
	return u.process.Restart(restartCtx)
}

func isAllowedSourceAgentUpdateWorkerType(workerType string) bool {
	return workerType == "wechat-worker" || workerType == "wcplus-worker"
}

func (u *SourceAgentUpdateTransaction) Apply(ctx context.Context, request SourceAgentUpdateRequest) SourceAgentUpdateResult {
	u.lifecycle.RLock()
	defer u.lifecycle.RUnlock()
	started := u.clock.Now()
	if u.closed {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeClosed, false)
	}
	if code := u.validateRequest(request); code != "" {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, code, false)
	}
	if ctx.Err() != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeCanceled, false)
	}
	release, err := u.fs.Acquire(ctx)
	if err != nil {
		code := SourceAgentUpdateCodeBusy
		if ctx.Err() != nil {
			code = SourceAgentUpdateCodeCanceled
		}
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, code, false)
	}
	defer release()

	journal, journalFound, journalErr := u.receipts.loadJournal()
	previous, outcomeFound, outcomeErr := u.receipts.LoadOutcome(request.CommandID)
	if outcomeErr != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
	}
	if outcomeFound {
		if !sourceAgentUpdateOutcomeMatches(previous, request) {
			return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, sourceAgentUpdateCodeInvalidRequest, false)
		}
	}
	if journalErr != nil {
		if outcomeFound {
			return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
		}
		return u.recoverUnboundBackup(ctx, started, request)
	}
	if journalFound {
		terminal, terminalFound, terminalErr := u.loadJournalOutcome(journal, previous, outcomeFound, request.CommandID)
		if terminalErr != nil {
			return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
		}
		if terminalFound {
			if !sourceAgentUpdateOutcomeMatchesJournal(terminal, journal) {
				return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
			}
			confirmed, publication := u.confirmOutcome(terminal)
			if publication != sourceAgentUpdateOutcomeDurable {
				return confirmed
			}
			var cleanupErr error
			if sourceAgentUpdateTerminalNeedsRecovery(terminal, journal) {
				cleanupErr = u.recoverDurableTerminal(ctx, journal)
			} else {
				cleanupErr = u.cleanupDurableAttempt(journal)
			}
			if sourceAgentUpdateJournalMatches(journal, request) {
				return terminal
			}
			if cleanupErr != nil {
				return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
			}
			journalFound = false
		}
	}
	if outcomeFound {
		confirmed, _ := u.confirmOutcome(previous)
		return confirmed
	}
	if journalFound {
		if recovered, done := u.recoverInterrupted(ctx, started, request, journal); done {
			return recovered
		}
	}
	backupPath := filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupName())
	if backupExists, existsErr := u.fs.RegularFileExists(backupPath); existsErr != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
	} else if backupExists {
		if err := u.fs.RestoreExecutable(backupPath, u.config.CurrentExecutable, "orphan", SourceAgentBinaryIdentity{}); err != nil {
			return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
		}
		if err := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable)); err != nil {
			return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
		}
		if err := u.restart(ctx, true); err != nil {
			return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
		}
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, true)
	}

	nonce, err := newSourceAgentUpdateAttemptNonce()
	if err != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false)
	}
	journal = u.newJournal(request, nonce, "started")
	if err := u.receipts.saveJournal(journal); err != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeInstallFailed, false)
	}
	guardCheck := SourceAgentUpdateGuardCheck{
		CommandID: request.CommandID, WorkerType: request.WorkerType,
		Version: request.TargetVersion, Revision: request.Revision, Channel: request.Channel,
	}
	if ctx.Err() != nil {
		return u.failBeforeReplace(started, request, journal, "", "", SourceAgentUpdateCodeCanceled)
	}
	if err := u.guard.Check(ctx, guardCheck); err != nil {
		return u.failBeforeReplace(started, request, journal, "", "", SourceAgentCommandCodeInstallFailed)
	}

	staged, err := u.fs.OpenStaged(u.config.StagingRoot, request.StagedBinary)
	if err != nil {
		return u.failBeforeReplace(started, request, journal, "", "", SourceAgentCommandCodeVerificationFailed)
	}
	info, err := staged.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != request.ExpectedSize {
		_ = staged.Close()
		return u.failBeforeReplace(started, request, journal, "", "", SourceAgentCommandCodeVerificationFailed)
	}

	prepared, err := u.fs.CreatePrepared(u.config.CurrentExecutable, nonce)
	if err != nil {
		_ = staged.Close()
		return u.failBeforeReplace(started, request, journal, "", "", SourceAgentCommandCodeInstallFailed)
	}
	preparedPath := prepared.Name()
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(prepared, hasher), io.LimitReader(staged, sourceAgentArtifactMaxBytes+1))
	closeStagedErr := staged.Close()
	if copyErr != nil || closeStagedErr != nil || written != request.ExpectedSize ||
		fmt.Sprintf("%x", hasher.Sum(nil)) != request.ExpectedSHA256 {
		_ = prepared.Close()
		return u.failBeforeReplace(started, request, journal, preparedPath, "", SourceAgentCommandCodeVerificationFailed)
	}
	if err := prepared.Chmod(0o755); err != nil {
		_ = prepared.Close()
		return u.failBeforeReplace(started, request, journal, preparedPath, "", SourceAgentCommandCodeInstallFailed)
	}
	if err := prepared.Sync(); err != nil {
		_ = prepared.Close()
		return u.failBeforeReplace(started, request, journal, preparedPath, "", SourceAgentCommandCodeInstallFailed)
	}
	if err := prepared.Close(); err != nil {
		return u.failBeforeReplace(started, request, journal, preparedPath, "", SourceAgentCommandCodeInstallFailed)
	}
	if err := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable)); err != nil {
		return u.failBeforeReplace(started, request, journal, preparedPath, "", SourceAgentCommandCodeInstallFailed)
	}
	if ctx.Err() != nil {
		return u.failBeforeReplace(started, request, journal, preparedPath, "", SourceAgentUpdateCodeCanceled)
	}
	backupIdentity, err := u.fs.BackupExecutable(u.config.CurrentExecutable, backupPath)
	if err != nil {
		return u.failBeforeReplace(started, request, journal, preparedPath, "", SourceAgentCommandCodeInstallFailed)
	}
	if err := u.fs.SyncDirectory(u.config.BackupRoot); err != nil {
		return u.failBeforeReplace(started, request, journal, preparedPath, backupPath, SourceAgentCommandCodeInstallFailed)
	}
	if u.faultStage == sourceAgentUpdateFaultAfterBackup {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeInterrupted, false)
	}
	journal.Backup = backupIdentity
	journal = u.advanceJournal(journal, "backup_durable")
	if err := u.receipts.saveJournal(journal); err != nil {
		return u.failBeforeReplace(started, request, journal, preparedPath, backupPath, SourceAgentCommandCodeInstallFailed)
	}
	// The final rollout/idle guard is deliberately after the durable backup and
	// immediately before the atomic rename.
	if err := u.guard.Check(ctx, guardCheck); err != nil {
		return u.failBeforeReplace(started, request, journal, preparedPath, backupPath, SourceAgentCommandCodeInstallFailed)
	}
	if ctx.Err() != nil {
		return u.failBeforeReplace(started, request, journal, preparedPath, backupPath, SourceAgentUpdateCodeCanceled)
	}
	currentIdentity, err := u.fs.RegularFileIdentity(u.config.CurrentExecutable)
	if err != nil || currentIdentity != journal.Backup {
		return u.failBeforeReplace(started, request, journal, preparedPath, backupPath, SourceAgentCommandCodeInstallFailed)
	}
	if err := u.fs.ReplaceExecutable(preparedPath, u.config.CurrentExecutable); err != nil {
		return u.failBeforeReplace(started, request, journal, preparedPath, backupPath, SourceAgentCommandCodeInstallFailed)
	}
	if err := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable)); err != nil {
		return u.rollback(context.WithoutCancel(ctx), started, backupPath, request, journal)
	}
	if u.faultStage == sourceAgentUpdateFaultAfterReplace {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeInterrupted, false)
	}
	journal = u.advanceJournal(journal, "replaced")
	if err := u.receipts.saveJournal(journal); err != nil {
		return u.rollback(context.WithoutCancel(ctx), started, backupPath, request, journal)
	}
	if ctx.Err() != nil {
		return u.rollback(context.WithoutCancel(ctx), started, backupPath, request, journal)
	}
	if err := u.restart(ctx, false); err != nil {
		return u.rollback(context.WithoutCancel(ctx), started, backupPath, request, journal)
	}
	if u.faultStage == sourceAgentUpdateFaultAfterRestart {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeInterrupted, false)
	}
	journal = u.advanceJournal(journal, "restarted")
	if err := u.receipts.saveJournal(journal); err != nil {
		return u.rollback(context.WithoutCancel(ctx), started, backupPath, request, journal)
	}
	expectation := SourceAgentReadyExpectation{
		CommandID: request.CommandID, AttemptNonce: nonce, WorkerType: request.WorkerType, Version: request.TargetVersion,
		Platform: request.Platform, Architecture: request.Architecture,
		ProtocolVersion: request.ProtocolVersion, Revision: request.Revision,
	}
	if err := u.receipts.WaitReady(ctx, expectation, u.config.ReadyTimeout); err != nil {
		return u.rollback(context.WithoutCancel(ctx), started, backupPath, request, journal)
	}
	if u.faultStage == sourceAgentUpdateFaultAfterReady {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeInterrupted, false)
	}
	journal = u.advanceJournal(journal, "ready")
	if err := u.receipts.saveJournal(journal); err != nil {
		return u.rollback(context.WithoutCancel(ctx), started, backupPath, request, journal)
	}
	if u.faultStage == sourceAgentUpdateFaultBeforeOutcome {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeInterrupted, false)
	}
	succeeded := u.finishResult(started, request, SourceAgentUpdateOutcomeSucceeded, SourceAgentCommandCodeUpgradeComplete, false)
	succeeded.RuntimeVersion = request.TargetVersion
	persisted, publication := u.persistOutcome(succeeded)
	if publication == sourceAgentUpdateOutcomeNotPublished {
		return u.rollback(context.WithoutCancel(ctx), started, backupPath, request, journal)
	}
	if publication == sourceAgentUpdateOutcomePublished {
		return persisted
	}
	if err := u.finalizeDurableOutcome(journal); err != nil {
		return persisted
	}
	return persisted
}

func (u *SourceAgentUpdateTransaction) loadJournalOutcome(
	journal sourceAgentUpdateJournal,
	current SourceAgentUpdateResult,
	currentFound bool,
	currentCommandID string,
) (SourceAgentUpdateResult, bool, error) {
	if currentFound && journal.CommandID == currentCommandID {
		return current, true, nil
	}
	return u.receipts.LoadOutcome(journal.CommandID)
}

func (u *SourceAgentUpdateTransaction) recoverDurableTerminal(ctx context.Context, journal sourceAgentUpdateJournal) error {
	backupPath := filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupName())
	backupExists, err := u.fs.RegularFileExists(backupPath)
	if err != nil || !backupExists {
		return errors.New("durable updater recovery backup is unavailable")
	}
	if !validSourceAgentBinaryIdentity(journal.Backup) {
		journal.Backup, err = u.fs.RegularFileIdentity(backupPath)
		if err != nil {
			return err
		}
	}
	if err := u.fs.RestoreExecutable(backupPath, u.config.CurrentExecutable, journal.AttemptNonce, journal.Backup); err != nil {
		return err
	}
	if err := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable)); err != nil {
		return err
	}
	if err := u.restart(ctx, true); err != nil {
		return err
	}
	journal = u.advanceJournal(journal, "terminal_cleanup")
	if err := u.receipts.saveJournal(journal); err != nil {
		return err
	}
	return u.cleanupDurableAttempt(journal)
}

func (u *SourceAgentUpdateTransaction) recoverUnboundBackup(ctx context.Context, started time.Time, request SourceAgentUpdateRequest) SourceAgentUpdateResult {
	backupPath := filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupName())
	backupExists, err := u.fs.RegularFileExists(backupPath)
	if err != nil || !backupExists {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
	}
	if err := u.fs.RestoreExecutable(backupPath, u.config.CurrentExecutable, "recovery", SourceAgentBinaryIdentity{}); err != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
	}
	if err := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable)); err != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
	}
	if err := u.restart(ctx, true); err != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false)
	}
	return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, true)
}

func (u *SourceAgentUpdateTransaction) rollback(
	ctx context.Context,
	started time.Time,
	backupPath string,
	request SourceAgentUpdateRequest,
	journal sourceAgentUpdateJournal,
) SourceAgentUpdateResult {
	if err := u.fs.RestoreExecutable(backupPath, u.config.CurrentExecutable, journal.AttemptNonce, journal.Backup); err != nil {
		result := u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeRollbackFailed, false)
		persisted, _ := u.persistOutcome(result)
		return persisted
	}
	syncErr := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable))
	journal = u.advanceJournal(journal, "rollback_restored")
	journalErr := u.receipts.saveJournal(journal)
	restartErr := u.restart(ctx, true)
	if syncErr != nil || journalErr != nil || restartErr != nil {
		result := u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeRollbackFailed, true)
		persisted, _ := u.persistOutcome(result)
		return persisted
	}
	result := u.finishResult(started, request, SourceAgentUpdateOutcomeRolledBack, SourceAgentCommandCodeRollbackComplete, true)
	persisted, publication := u.persistOutcome(result)
	if publication == sourceAgentUpdateOutcomeDurable {
		if err := u.finalizeDurableOutcome(journal); err != nil {
			return persisted
		}
	}
	return persisted
}

func (u *SourceAgentUpdateTransaction) persistOutcome(result SourceAgentUpdateResult) (SourceAgentUpdateResult, sourceAgentUpdateOutcomePublication) {
	if err := u.receipts.SaveOutcome(result); err != nil {
		if u.outcomeWasPublished(result, err) {
			result.PersistenceCode = SourceAgentUpdateCodeOutcomePersistenceFailed
			return result, sourceAgentUpdateOutcomePublished
		}
		if retryErr := u.receipts.SaveOutcome(result); retryErr != nil {
			publication := sourceAgentUpdateOutcomeNotPublished
			if u.outcomeWasPublished(result, retryErr) {
				publication = sourceAgentUpdateOutcomePublished
			}
			result.PersistenceCode = SourceAgentUpdateCodeOutcomePersistenceFailed
			return result, publication
		}
	}
	return result, sourceAgentUpdateOutcomeDurable
}

func (u *SourceAgentUpdateTransaction) confirmOutcome(result SourceAgentUpdateResult) (SourceAgentUpdateResult, sourceAgentUpdateOutcomePublication) {
	if err := u.receipts.SaveOutcome(result); err != nil {
		publication := sourceAgentUpdateOutcomeNotPublished
		if u.outcomeWasPublished(result, err) {
			publication = sourceAgentUpdateOutcomePublished
		}
		result.PersistenceCode = SourceAgentUpdateCodeOutcomePersistenceFailed
		return result, publication
	}
	return result, sourceAgentUpdateOutcomeDurable
}

func (u *SourceAgentUpdateTransaction) outcomeWasPublished(result SourceAgentUpdateResult, saveErr error) bool {
	stored, found, loadErr := u.receipts.LoadOutcome(result.CommandID)
	return (loadErr == nil && found && stored == result) || isSourceAgentUpdatePublishedError(saveErr)
}

func (u *SourceAgentUpdateTransaction) finishResult(started time.Time, request SourceAgentUpdateRequest, outcome, code string, restored bool) SourceAgentUpdateResult {
	result := u.baseResult(request)
	result.Outcome, result.Code, result.Message, result.BinaryRestored = outcome, code, sourceAgentUpdatePublicMessage(code), restored
	duration := u.clock.Now().Sub(started)
	if duration < 0 {
		duration = 0
	}
	result.DurationMillis = duration.Milliseconds()
	return result
}

func (u *SourceAgentUpdateTransaction) failBeforeReplace(started time.Time, request SourceAgentUpdateRequest, journal sourceAgentUpdateJournal, preparedPath, backupPath, code string) SourceAgentUpdateResult {
	if journal.Stage == "backup_durable" && validSourceAgentBinaryIdentity(journal.Backup) {
		result := u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, code, false)
		persisted, publication := u.persistOutcome(result)
		if publication != sourceAgentUpdateOutcomeDurable {
			return persisted
		}
		if u.faultStage == sourceAgentUpdateFaultPreReplaceAfterOutcome {
			return persisted
		}
		if err := u.finalizeDurableOutcome(journal); err != nil {
			return persisted
		}
		return persisted
	}
	cleanupErr := u.cleanupBeforeReplace(journal, preparedPath, backupPath)
	result := u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, code, false)
	persisted, _ := u.persistOutcome(result)
	if cleanupErr != nil {
		return persisted
	}
	return persisted
}

func (u *SourceAgentUpdateTransaction) cleanupBeforeReplace(journal sourceAgentUpdateJournal, preparedPath, backupPath string) error {
	if preparedPath != "" {
		if err := u.fs.Remove(preparedPath); err != nil {
			return err
		}
	}
	if backupPath != "" {
		if err := u.fs.Remove(backupPath); err != nil {
			return err
		}
	}
	if err := u.fs.Remove(filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupPendingName())); err != nil {
		return err
	}
	if err := u.fs.SyncDirectory(u.config.BackupRoot); err != nil {
		return err
	}
	return u.receipts.clearJournal(journal.CommandID, journal.AttemptNonce)
}

func (u *SourceAgentUpdateTransaction) recoverInterrupted(ctx context.Context, started time.Time, request SourceAgentUpdateRequest, journal sourceAgentUpdateJournal) (SourceAgentUpdateResult, bool) {
	if !sourceAgentUpdateJournalMatches(journal, request) {
		return u.recoverUnboundBackup(ctx, started, request), true
	}
	backupPath := filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupName())
	backupExists, err := u.fs.RegularFileExists(backupPath)
	if err != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false), true
	}
	if !backupExists {
		if journal.Stage == "started" {
			_ = u.fs.Remove(sourceAgentUpdatePreparedPath(u.config.CurrentExecutable, journal.AttemptNonce))
			_ = u.fs.Remove(filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupPendingName()))
			_ = u.fs.SyncDirectory(u.config.BackupRoot)
			if err := u.receipts.clearJournal(journal.CommandID, journal.AttemptNonce); err == nil {
				return SourceAgentUpdateResult{}, false
			}
		}
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false), true
	}
	if !validSourceAgentBinaryIdentity(journal.Backup) {
		identity, identityErr := u.fs.RegularFileIdentity(backupPath)
		if identityErr != nil {
			return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false), true
		}
		journal.Backup = identity
	}
	if err := u.fs.RestoreExecutable(backupPath, u.config.CurrentExecutable, journal.AttemptNonce, journal.Backup); err != nil {
		return u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentUpdateCodeRecoveryFailed, false), true
	}
	syncErr := u.fs.SyncDirectory(filepath.Dir(u.config.CurrentExecutable))
	restartErr := u.restart(ctx, true)
	journal = u.advanceJournal(journal, "rollback_restored")
	journalErr := u.receipts.saveJournal(journal)
	if syncErr != nil || restartErr != nil || journalErr != nil {
		result := u.finishResult(started, request, SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeRollbackFailed, true)
		persisted, _ := u.persistOutcome(result)
		return persisted, true
	}
	result := u.finishResult(started, request, SourceAgentUpdateOutcomeRolledBack, SourceAgentCommandCodeRollbackComplete, true)
	persisted, publication := u.persistOutcome(result)
	if publication == sourceAgentUpdateOutcomeDurable {
		if err := u.finalizeDurableOutcome(journal); err != nil {
			return persisted, true
		}
	}
	return persisted, true
}

func (u *SourceAgentUpdateTransaction) finalizeDurableOutcome(journal sourceAgentUpdateJournal) error {
	journal = u.advanceJournal(journal, "terminal_cleanup")
	if err := u.receipts.saveJournal(journal); err != nil {
		return err
	}
	return u.cleanupDurableAttempt(journal)
}

func (u *SourceAgentUpdateTransaction) cleanupDurableAttempt(journal sourceAgentUpdateJournal) error {
	if err := u.fs.Remove(filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupName())); err != nil {
		return err
	}
	if u.faultStage == sourceAgentUpdateFaultAfterCleanupBackupRemove {
		return errors.New("source agent update cleanup interrupted after backup removal")
	}
	if err := u.fs.Remove(filepath.Join(u.config.BackupRoot, sourceAgentUpdateBackupPendingName())); err != nil {
		return err
	}
	if err := u.fs.Remove(sourceAgentUpdatePreparedPath(u.config.CurrentExecutable, journal.AttemptNonce)); err != nil {
		return err
	}
	if err := u.fs.SyncDirectory(u.config.BackupRoot); err != nil {
		return err
	}
	if u.faultStage == sourceAgentUpdateFaultAfterCleanupSync {
		return errors.New("source agent update cleanup interrupted after directory sync")
	}
	return u.receipts.clearJournal(journal.CommandID, journal.AttemptNonce)
}

func (u *SourceAgentUpdateTransaction) newJournal(request SourceAgentUpdateRequest, nonce, stage string) sourceAgentUpdateJournal {
	now := u.clock.Now().UTC().Format(time.RFC3339Nano)
	return sourceAgentUpdateJournal{
		SchemaVersion: sourceAgentUpdateJournalSchema, CommandID: request.CommandID,
		AttemptNonce: nonce, RequestFingerprint: sourceAgentUpdateRequestFingerprint(request),
		WorkerType: request.WorkerType, CurrentVersion: request.CurrentVersion, TargetVersion: request.TargetVersion,
		Platform: request.Platform, Architecture: request.Architecture, ProtocolVersion: request.ProtocolVersion,
		Revision: request.Revision, Channel: request.Channel, Stage: stage, StartedAt: now, UpdatedAt: now,
	}
}

func (u *SourceAgentUpdateTransaction) advanceJournal(journal sourceAgentUpdateJournal, stage string) sourceAgentUpdateJournal {
	journal.Stage = stage
	journal.UpdatedAt = u.clock.Now().UTC().Format(time.RFC3339Nano)
	return journal
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
	if !validSourceAgentUpdateResult(result) || result.CommandID != request.CommandID || result.Platform != request.Platform ||
		result.WorkerType != request.WorkerType || result.Architecture != request.Architecture || result.Channel != request.Channel ||
		result.ProtocolVersion != request.ProtocolVersion || result.Revision != request.Revision ||
		result.RequestFingerprint != sourceAgentUpdateRequestFingerprint(request) {
		return false
	}
	if result.Outcome == SourceAgentUpdateOutcomeSucceeded {
		return result.RuntimeVersion == request.TargetVersion
	}
	return result.RuntimeVersion == request.CurrentVersion
}

func sourceAgentUpdateOutcomeMatchesJournal(result SourceAgentUpdateResult, journal sourceAgentUpdateJournal) bool {
	return validSourceAgentUpdateResult(result) && validSourceAgentUpdateJournal(journal) &&
		result.CommandID == journal.CommandID && result.RequestFingerprint == journal.RequestFingerprint &&
		result.WorkerType == journal.WorkerType && result.Platform == journal.Platform &&
		result.Architecture == journal.Architecture && result.Channel == journal.Channel &&
		result.ProtocolVersion == journal.ProtocolVersion && result.Revision == journal.Revision &&
		validSourceAgentUpdateTerminalCombination(result, journal)
}

func validSourceAgentUpdateTerminalCombination(result SourceAgentUpdateResult, journal sourceAgentUpdateJournal) bool {
	if result.PersistenceCode != "" {
		return false
	}
	switch result.Outcome {
	case SourceAgentUpdateOutcomeSucceeded:
		return result.Code == SourceAgentCommandCodeUpgradeComplete && !result.BinaryRestored &&
			result.RuntimeVersion == journal.TargetVersion && sourceAgentUpdateJournalStageIs(journal.Stage, "ready", "terminal_cleanup")
	case SourceAgentUpdateOutcomeRolledBack:
		return result.Code == SourceAgentCommandCodeRollbackComplete && result.BinaryRestored &&
			result.RuntimeVersion == journal.CurrentVersion && sourceAgentUpdateJournalStageIs(journal.Stage, "rollback_restored", "terminal_cleanup")
	case SourceAgentUpdateOutcomeFailed:
		if result.RuntimeVersion != journal.CurrentVersion {
			return false
		}
		switch result.Code {
		case SourceAgentCommandCodeVerificationFailed, SourceAgentCommandCodeInstallFailed, SourceAgentUpdateCodeCanceled:
			return !result.BinaryRestored && sourceAgentUpdateJournalStageIs(journal.Stage, "started", "backup_durable", "terminal_cleanup")
		case SourceAgentCommandCodeRollbackFailed:
			return sourceAgentUpdateJournalStageIs(journal.Stage,
				"backup_durable", "replaced", "restarted", "ready", "rollback_restored", "terminal_cleanup")
		}
	}
	return false
}

func sourceAgentUpdateJournalStageIs(stage string, allowed ...string) bool {
	for _, candidate := range allowed {
		if stage == candidate {
			return true
		}
	}
	return false
}

func sourceAgentUpdateTerminalNeedsRecovery(result SourceAgentUpdateResult, journal sourceAgentUpdateJournal) bool {
	return result.Outcome == SourceAgentUpdateOutcomeFailed && result.Code == SourceAgentCommandCodeRollbackFailed &&
		journal.Stage != "terminal_cleanup"
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

func newSourceAgentUpdateAttemptNonce() (string, error) {
	var nonce [32]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", nonce), nil
}

func sourceAgentUpdateJournalMatches(journal sourceAgentUpdateJournal, request SourceAgentUpdateRequest) bool {
	return validSourceAgentUpdateJournal(journal) && journal.CommandID == request.CommandID &&
		journal.RequestFingerprint == sourceAgentUpdateRequestFingerprint(request)
}

func validSourceAgentUpdateJournal(journal sourceAgentUpdateJournal) bool {
	if journal.SchemaVersion != sourceAgentUpdateJournalSchema ||
		!isExactLowerHex(journal.AttemptNonce, sha256.Size*2) ||
		!isExactLowerHex(journal.RequestFingerprint, sha256.Size*2) ||
		!isAllowedSourceAgentUpdateWorkerType(journal.WorkerType) ||
		!isSourceAgentArtifactVersion(journal.CurrentVersion) || !isSourceAgentArtifactVersion(journal.TargetVersion) ||
		compareSourceAgentArtifactVersions(journal.CurrentVersion, journal.TargetVersion) >= 0 ||
		!isExactSourceAgentArtifactName("platform", journal.Platform) ||
		!isExactSourceAgentArtifactName("architecture", journal.Architecture) ||
		!isExactSourceAgentProtocolVersion(journal.ProtocolVersion) ||
		(!isExactLowerHex(journal.Revision, 40) && !isExactLowerHex(journal.Revision, 64)) ||
		(journal.Channel != "staging" && journal.Channel != "production") {
		return false
	}
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", journal.CommandID, true)
	if err != nil || commandID != journal.CommandID {
		return false
	}
	started, startErr := time.Parse(time.RFC3339Nano, journal.StartedAt)
	updated, updateErr := time.Parse(time.RFC3339Nano, journal.UpdatedAt)
	if startErr != nil || updateErr != nil || updated.Before(started) {
		return false
	}
	allowedStages := map[string]struct{}{
		"started": {}, "backup_durable": {}, "replaced": {}, "restarted": {}, "ready": {},
		"rollback_restored": {}, "terminal_cleanup": {},
	}
	if _, ok := allowedStages[journal.Stage]; !ok {
		return false
	}
	if journal.Stage != "started" && (!validSourceAgentBinaryIdentity(journal.Backup)) {
		return false
	}
	return true
}

func validSourceAgentBinaryIdentity(identity SourceAgentBinaryIdentity) bool {
	return identity.Size > 0 && identity.Size <= sourceAgentArtifactMaxBytes &&
		isExactLowerHex(identity.SHA256, sha256.Size*2) && identity.Device > 0 && identity.Inode > 0
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
	case SourceAgentUpdateCodeInterrupted:
		return "Upgrade was interrupted and requires local recovery."
	case SourceAgentUpdateCodeRecoveryFailed:
		return "Upgrade recovery requires operator attention."
	case SourceAgentUpdateCodeClosed:
		return "Upgrade transaction is closed."
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

func sourceAgentUpdateBackupName() string {
	return ".source-agent-backup"
}

func sourceAgentUpdateBackupPendingName() string {
	return ".source-agent-backup.pending"
}

func sourceAgentUpdatePreparedPath(executable, nonce string) string {
	return filepath.Join(filepath.Dir(executable), ".source-agent-prepared-"+nonce)
}

func openSourceAgentUpdateStagedFile(root, path string) (SourceAgentUpdateStagedFile, error) {
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

type FileSourceAgentUpdateReceiptStore struct {
	root         string
	directory    sourceAgentUpdateDirectory
	pollInterval time.Duration
}

func NewFileSourceAgentUpdateReceiptStore(root string, pollInterval time.Duration) (*FileSourceAgentUpdateReceiptStore, error) {
	if !isCleanAbsoluteSourceAgentUpdatePath(root) {
		return nil, errors.New("source agent receipt root must be a fixed absolute path")
	}
	directory, err := openSourceAgentUpdateDirectory(root)
	if err != nil {
		return nil, errors.New("source agent receipt root is unavailable")
	}
	if pollInterval <= 0 {
		pollInterval = sourceAgentUpdateDefaultPoll
	}
	return &FileSourceAgentUpdateReceiptStore{root: root, directory: directory, pollInterval: pollInterval}, nil
}

func (s *FileSourceAgentUpdateReceiptStore) Acquire(ctx context.Context, _ string) (func(), error) {
	return s.directory.acquire(ctx)
}

func (s *FileSourceAgentUpdateReceiptStore) Close() error {
	return s.directory.close()
}

func (s *FileSourceAgentUpdateReceiptStore) readyPath(commandID, attemptNonce string) string {
	return filepath.Join(s.root, sourceAgentUpdateReceiptName("ready", commandID, attemptNonce))
}

func (s *FileSourceAgentUpdateReceiptStore) outcomePath(commandID string) string {
	return filepath.Join(s.root, sourceAgentUpdateReceiptName("outcome", commandID, ""))
}

func sourceAgentUpdateReceiptName(kind, commandID, attemptNonce string) string {
	digest := sha256.Sum256([]byte(commandID + "\x00" + attemptNonce))
	return fmt.Sprintf("%s-%x.json", kind, digest[:16])
}

func validSourceAgentUpdateLocalName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name &&
		!strings.ContainsAny(name, "/\\\x00")
}

func (s *FileSourceAgentUpdateReceiptStore) ReadyChallenge(commandID string) (SourceAgentReadyChallenge, error) {
	journal, found, err := s.loadJournal()
	if err != nil || !found || journal.CommandID != commandID ||
		(journal.Stage != "restarted" && journal.Stage != "ready") {
		return SourceAgentReadyChallenge{}, ErrSourceAgentReadyInvalid
	}
	return SourceAgentReadyChallenge{
		CommandID: journal.CommandID, AttemptNonce: journal.AttemptNonce,
		WorkerType: journal.WorkerType, Version: journal.TargetVersion,
		Platform: journal.Platform, Architecture: journal.Architecture,
		ProtocolVersion: journal.ProtocolVersion, Revision: journal.Revision,
	}, nil
}

func (s *FileSourceAgentUpdateReceiptStore) WriteReady(receipt SourceAgentReadyReceipt) error {
	if !validSourceAgentReadyReceipt(receipt) {
		return ErrSourceAgentReadyInvalid
	}
	challenge, err := s.ReadyChallenge(receipt.CommandID)
	if err != nil || challenge.AttemptNonce != receipt.AttemptNonce || challenge.WorkerType != receipt.WorkerType ||
		challenge.Version != receipt.Version || challenge.Platform != receipt.Platform ||
		challenge.Architecture != receipt.Architecture || challenge.ProtocolVersion != receipt.ProtocolVersion ||
		challenge.Revision != receipt.Revision {
		return ErrSourceAgentReadyMismatch
	}
	payload, err := marshalSourceAgentUpdateJSON(receipt)
	if err != nil {
		return ErrSourceAgentReadyInvalid
	}
	return s.directory.writeImmutable(sourceAgentUpdateReceiptName("ready", receipt.CommandID, receipt.AttemptNonce), payload)
}

func (s *FileSourceAgentUpdateReceiptStore) WaitReady(ctx context.Context, expected SourceAgentReadyExpectation, timeout time.Duration) error {
	if !validSourceAgentReadyExpectation(expected) || timeout <= 0 {
		return ErrSourceAgentReadyInvalid
	}
	deadline := time.Now().Add(timeout)
	name := sourceAgentUpdateReceiptName("ready", expected.CommandID, expected.AttemptNonce)
	for {
		if !time.Now().Before(deadline) {
			return ErrSourceAgentReadyTimeout
		}
		var receipt SourceAgentReadyReceipt
		payload, err := s.directory.read(name, sourceAgentUpdateReceiptMaxBytes)
		if err == nil {
			if !time.Now().Before(deadline) || decodeStrictSourceAgentUpdateJSON(payload, &receipt) != nil || !validSourceAgentReadyReceipt(receipt) {
				if !time.Now().Before(deadline) {
					return ErrSourceAgentReadyTimeout
				}
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
		if err := waitSourceAgentUpdatePoll(ctx, time.Until(deadline), s.pollInterval); err != nil {
			return err
		}
	}
}

func (s *FileSourceAgentUpdateReceiptStore) LoadOutcome(commandID string) (SourceAgentUpdateResult, bool, error) {
	if normalized, err := normalizeSourceAgentCommandIdentifier("command_id", commandID, true); err != nil || normalized != commandID {
		return SourceAgentUpdateResult{}, false, errors.New("invalid source agent update command")
	}
	payload, err := s.directory.read(sourceAgentUpdateReceiptName("outcome", commandID, ""), sourceAgentUpdateReceiptMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return SourceAgentUpdateResult{}, false, nil
	}
	var result SourceAgentUpdateResult
	if err != nil || decodeStrictSourceAgentUpdateJSON(payload, &result) != nil || !validSourceAgentUpdateResult(result) {
		return SourceAgentUpdateResult{}, false, errors.New("invalid source agent update outcome")
	}
	return result, true, nil
}

func (s *FileSourceAgentUpdateReceiptStore) SaveOutcome(result SourceAgentUpdateResult) error {
	if !validSourceAgentUpdateResult(result) {
		return errors.New("invalid source agent update outcome")
	}
	payload, err := marshalSourceAgentUpdateJSON(result)
	if err != nil {
		return err
	}
	return s.directory.writeImmutable(sourceAgentUpdateReceiptName("outcome", result.CommandID, ""), payload)
}

func (s *FileSourceAgentUpdateReceiptStore) loadJournal() (sourceAgentUpdateJournal, bool, error) {
	payload, err := s.directory.read(sourceAgentUpdateJournalFileName, sourceAgentUpdateReceiptMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return sourceAgentUpdateJournal{}, false, nil
	}
	var journal sourceAgentUpdateJournal
	if err != nil || decodeStrictSourceAgentUpdateJSON(payload, &journal) != nil || !validSourceAgentUpdateJournal(journal) {
		return sourceAgentUpdateJournal{}, true, errors.New("source agent update journal is invalid")
	}
	return journal, true, nil
}

func (s *FileSourceAgentUpdateReceiptStore) saveJournal(journal sourceAgentUpdateJournal) error {
	if !validSourceAgentUpdateJournal(journal) {
		return errors.New("source agent update journal is invalid")
	}
	payload, err := marshalSourceAgentUpdateJSON(journal)
	if err != nil {
		return err
	}
	return s.directory.writeAtomic(sourceAgentUpdateJournalFileName, payload)
}

func (s *FileSourceAgentUpdateReceiptStore) clearJournal(commandID, attemptNonce string) error {
	journal, found, err := s.loadJournal()
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if journal.CommandID != commandID || journal.AttemptNonce != attemptNonce {
		return errors.New("source agent update journal changed")
	}
	if err := s.directory.remove(sourceAgentUpdateReceiptName("ready", commandID, attemptNonce)); err != nil {
		return err
	}
	return s.directory.remove(sourceAgentUpdateJournalFileName)
}

func validSourceAgentReadyExpectation(value SourceAgentReadyExpectation) bool {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", value.CommandID, true)
	return err == nil && commandID == value.CommandID && isExactLowerHex(value.AttemptNonce, sha256.Size*2) &&
		isAllowedSourceAgentUpdateWorkerType(value.WorkerType) &&
		isSourceAgentArtifactVersion(value.Version) && isExactSourceAgentArtifactName("platform", value.Platform) &&
		isExactSourceAgentArtifactName("architecture", value.Architecture) && isExactSourceAgentProtocolVersion(value.ProtocolVersion) &&
		(isExactLowerHex(value.Revision, 40) || isExactLowerHex(value.Revision, 64))
}

func validSourceAgentReadyReceipt(value SourceAgentReadyReceipt) bool {
	return value.HeartbeatAuthenticated && validSourceAgentReadyExpectation(SourceAgentReadyExpectation{
		CommandID: value.CommandID, AttemptNonce: value.AttemptNonce, WorkerType: value.WorkerType, Version: value.Version,
		Platform: value.Platform, Architecture: value.Architecture,
		ProtocolVersion: value.ProtocolVersion, Revision: value.Revision,
	})
}

func sourceAgentReadyReceiptMatches(receipt SourceAgentReadyReceipt, expected SourceAgentReadyExpectation) bool {
	return receipt.CommandID == expected.CommandID && receipt.AttemptNonce == expected.AttemptNonce && receipt.WorkerType == expected.WorkerType &&
		receipt.Version == expected.Version && receipt.Platform == expected.Platform &&
		receipt.Architecture == expected.Architecture && receipt.ProtocolVersion == expected.ProtocolVersion &&
		receipt.Revision == expected.Revision && receipt.HeartbeatAuthenticated
}

func validSourceAgentUpdateResult(value SourceAgentUpdateResult) bool {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", value.CommandID, true)
	if err != nil || commandID != value.CommandID || !isExactSourceAgentArtifactName("platform", value.Platform) ||
		!isAllowedSourceAgentUpdateWorkerType(value.WorkerType) ||
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
		SourceAgentUpdateCodeCanceled: {}, SourceAgentUpdateCodeRecoveryFailed: {}, sourceAgentUpdateCodeInvalidRequest: {},
	}
	_, ok := allowedCodes[value.Code]
	if !ok || value.PersistenceCode != "" {
		return false
	}
	switch value.Outcome {
	case SourceAgentUpdateOutcomeSucceeded:
		return value.Code == SourceAgentCommandCodeUpgradeComplete && !value.BinaryRestored
	case SourceAgentUpdateOutcomeRolledBack:
		return value.Code == SourceAgentCommandCodeRollbackComplete && value.BinaryRestored
	case SourceAgentUpdateOutcomeFailed:
		switch value.Code {
		case SourceAgentCommandCodeVerificationFailed, SourceAgentCommandCodeInstallFailed, SourceAgentUpdateCodeCanceled:
			return !value.BinaryRestored
		case SourceAgentCommandCodeRollbackFailed:
			return true
		}
	}
	return false
}

func marshalSourceAgentUpdateJSON(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) > sourceAgentUpdateReceiptMaxBytes {
		return nil, errors.New("source agent update state is invalid")
	}
	return append(payload, '\n'), nil
}

func decodeStrictSourceAgentUpdateJSON(payload []byte, target any) error {
	if rejectDuplicateJSONFields(payload) != nil {
		return errors.New("source agent update state is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("source agent update state is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("source agent update state is invalid")
	}
	return nil
}

func waitSourceAgentUpdatePoll(ctx context.Context, remaining, poll time.Duration) error {
	if remaining <= 0 {
		return ErrSourceAgentReadyTimeout
	}
	if poll > remaining {
		poll = remaining
	}
	timer := time.NewTimer(poll)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
