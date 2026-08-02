package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const sourceAgentUpdateTestRevision = "0123456789abcdef0123456789abcdef01234567"

type fakeSourceAgentUpdateGuard struct {
	mu       sync.Mutex
	calls    int
	failCall int
	checks   []SourceAgentUpdateGuardCheck
}

func (g *fakeSourceAgentUpdateGuard) Check(_ context.Context, check SourceAgentUpdateGuardCheck) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	g.checks = append(g.checks, check)
	if g.calls == g.failCall {
		return errors.New("private guard detail")
	}
	return nil
}

func (g *fakeSourceAgentUpdateGuard) snapshotChecks() []SourceAgentUpdateGuardCheck {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]SourceAgentUpdateGuardCheck(nil), g.checks...)
}

type fakeSourceAgentUpdateProcess struct {
	mu             sync.Mutex
	calls          int
	failCall       int
	rejectCanceled bool
	blockUntilDone bool
}

func (p *fakeSourceAgentUpdateProcess) Restart(ctx context.Context) error {
	p.mu.Lock()
	p.calls++
	blockUntilDone := p.blockUntilDone
	p.mu.Unlock()
	if blockUntilDone {
		<-ctx.Done()
		return ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rejectCanceled && ctx.Err() != nil {
		return errors.New("private canceled context detail")
	}
	if p.calls == p.failCall {
		return errors.New("private launch detail")
	}
	return nil
}

type fakeSourceAgentUpdateReceipts struct {
	mu           sync.Mutex
	locked       bool
	load         SourceAgentUpdateResult
	loaded       bool
	saved        []SourceAgentUpdateResult
	waitErr      error
	saveErr      error
	waitStart    chan struct{}
	waitBlock    chan struct{}
	journal      sourceAgentUpdateJournal
	journalFound bool
	journalErr   error
	clearErr     error
	closeCalls   int
}

func (r *fakeSourceAgentUpdateReceipts) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCalls++
	return nil
}

func (r *fakeSourceAgentUpdateReceipts) loadJournal() (sourceAgentUpdateJournal, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.journal, r.journalFound, r.journalErr
}

func (r *fakeSourceAgentUpdateReceipts) saveJournal(journal sourceAgentUpdateJournal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.journalErr != nil {
		return r.journalErr
	}
	r.journal, r.journalFound = journal, true
	return nil
}

func (r *fakeSourceAgentUpdateReceipts) clearJournal(commandID, attemptNonce string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clearErr != nil {
		return r.clearErr
	}
	if r.journalErr != nil {
		return r.journalErr
	}
	if r.journalFound && (r.journal.CommandID != commandID || r.journal.AttemptNonce != attemptNonce) {
		return errors.New("journal changed")
	}
	r.journal, r.journalFound = sourceAgentUpdateJournal{}, false
	return nil
}

func (r *fakeSourceAgentUpdateReceipts) Acquire(_ context.Context, _ string) (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locked {
		return nil, ErrSourceAgentUpdateBusy
	}
	r.locked = true
	return func() {
		r.mu.Lock()
		r.locked = false
		r.mu.Unlock()
	}, nil
}

func (r *fakeSourceAgentUpdateReceipts) LoadOutcome(commandID string) (SourceAgentUpdateResult, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.load, r.loaded && r.load.CommandID == commandID, nil
}

func (r *fakeSourceAgentUpdateReceipts) SaveOutcome(result SourceAgentUpdateResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = append(r.saved, result)
	r.load, r.loaded = result, true
	return nil
}

func (r *fakeSourceAgentUpdateReceipts) WaitReady(ctx context.Context, _ SourceAgentReadyExpectation, _ time.Duration) error {
	r.mu.Lock()
	waitStart := r.waitStart
	waitBlock := r.waitBlock
	r.waitStart = nil
	r.mu.Unlock()
	if waitStart != nil {
		close(waitStart)
	}
	if waitBlock != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-waitBlock:
		}
	}
	return r.waitErr
}

type failingSourceAgentUpdateFS struct {
	SourceAgentUpdateFileSystem
	fail                 string
	dirSyncNth           int
	dirSync              int
	mutatePath           string
	mutateData           []byte
	afterReplace         func()
	afterBackup          func()
	dirSyncFrom          int
	backupRemoveFailures int
}

func (f *failingSourceAgentUpdateFS) OpenStaged(root, path string) (SourceAgentUpdateStagedFile, error) {
	file, err := f.SourceAgentUpdateFileSystem.OpenStaged(root, path)
	if err == nil && f.mutatePath != "" {
		replacement := f.mutatePath + ".replacement"
		if writeErr := os.WriteFile(replacement, f.mutateData, 0o600); writeErr != nil {
			return nil, writeErr
		}
		if renameErr := os.Rename(replacement, f.mutatePath); renameErr != nil {
			return nil, renameErr
		}
	}
	return file, err
}

func (f *failingSourceAgentUpdateFS) CreatePrepared(path, nonce string) (SourceAgentUpdatePreparedFile, error) {
	if f.fail == "create_prepared" {
		return nil, errors.New("private prepared detail")
	}
	file, err := f.SourceAgentUpdateFileSystem.CreatePrepared(path, nonce)
	if err != nil {
		return nil, err
	}
	if f.fail == "prepared_sync" {
		return &failingPreparedSourceAgentFile{SourceAgentUpdatePreparedFile: file, failSync: true}, nil
	}
	return file, nil
}

func (f *failingSourceAgentUpdateFS) BackupExecutable(executable, backup string) (SourceAgentBinaryIdentity, error) {
	if f.fail == "backup" {
		return SourceAgentBinaryIdentity{}, errors.New("private backup path")
	}
	identity, err := f.SourceAgentUpdateFileSystem.BackupExecutable(executable, backup)
	if err == nil && f.afterBackup != nil {
		f.afterBackup()
	}
	return identity, err
}

func (f *failingSourceAgentUpdateFS) ReplaceExecutable(prepared, executable string) error {
	if f.fail == "replace" {
		return errors.New("private replace path")
	}
	err := f.SourceAgentUpdateFileSystem.ReplaceExecutable(prepared, executable)
	if err == nil && f.afterReplace != nil {
		f.afterReplace()
	}
	return err
}

func (f *failingSourceAgentUpdateFS) RestoreExecutable(backup, executable, nonce string, expected SourceAgentBinaryIdentity) error {
	if f.fail == "restore" {
		return errors.New("private restore path")
	}
	return f.SourceAgentUpdateFileSystem.RestoreExecutable(backup, executable, nonce, expected)
}

func (f *failingSourceAgentUpdateFS) SyncDirectory(path string) error {
	f.dirSync++
	if f.dirSyncFrom > 0 && f.dirSync >= f.dirSyncFrom {
		return errors.New("private persistent directory path")
	}
	if f.fail == "dir_sync" && f.dirSync == f.dirSyncNth {
		return errors.New("private directory path")
	}
	return f.SourceAgentUpdateFileSystem.SyncDirectory(path)
}

func (f *failingSourceAgentUpdateFS) Remove(path string) error {
	if filepath.Base(path) == sourceAgentUpdateBackupName() && f.backupRemoveFailures > 0 {
		f.backupRemoveFailures--
		return errors.New("private backup cleanup path")
	}
	return f.SourceAgentUpdateFileSystem.Remove(path)
}

type failingPreparedSourceAgentFile struct {
	SourceAgentUpdatePreparedFile
	failSync bool
}

func (f *failingPreparedSourceAgentFile) Sync() error {
	if f.failSync {
		return errors.New("private prepared path")
	}
	return f.SourceAgentUpdatePreparedFile.Sync()
}

type sourceAgentUpdateFixture struct {
	transaction *SourceAgentUpdateTransaction
	request     SourceAgentUpdateRequest
	executable  string
	staged      string
	oldBinary   []byte
	newBinary   []byte
	guard       *fakeSourceAgentUpdateGuard
	process     *fakeSourceAgentUpdateProcess
	receipts    *fakeSourceAgentUpdateReceipts
	fs          *failingSourceAgentUpdateFS
}

func newSourceAgentUpdateFixture(t *testing.T) sourceAgentUpdateFixture {
	t.Helper()
	root := t.TempDir()
	binRoot := filepath.Join(root, "bin")
	stageRoot := filepath.Join(root, "stage")
	receiptRoot := filepath.Join(root, "receipts")
	for _, path := range []string{binRoot, stageRoot, receiptRoot} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	oldBinary := []byte("#!/bin/sh\necho old\n")
	newBinary := []byte("#!/bin/sh\necho new release\n")
	executable := filepath.Join(binRoot, "source-worker")
	staged := filepath.Join(stageRoot, "artifact")
	if err := os.WriteFile(executable, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, newBinary, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(newBinary)
	guard := &fakeSourceAgentUpdateGuard{}
	process := &fakeSourceAgentUpdateProcess{}
	receipts := &fakeSourceAgentUpdateReceipts{}
	baseFS, err := NewOSSourceAgentUpdateFileSystem(executable)
	if err != nil {
		t.Fatalf("NewOSSourceAgentUpdateFileSystem() error = %v", err)
	}
	t.Cleanup(func() { _ = baseFS.Close() })
	fs := &failingSourceAgentUpdateFS{SourceAgentUpdateFileSystem: baseFS}
	config := SourceAgentUpdateConfig{
		WorkerType: "wechat-worker", Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		CurrentVersion: "1.0.0", ProtocolVersion: "2026-08-01",
		CurrentExecutable: executable, StagingRoot: stageRoot, BackupRoot: binRoot,
		ReceiptRoot: receiptRoot, ReadyTimeout: 25 * time.Millisecond,
		FileSystem: fs, ProcessControl: process, Guard: guard, Receipts: receipts,
	}
	transaction, err := NewSourceAgentUpdateTransaction(config)
	if err != nil {
		t.Fatalf("NewSourceAgentUpdateTransaction() error = %v", err)
	}
	request := SourceAgentUpdateRequest{
		CommandID: "command-1", ArtifactID: "artifact-1", WorkerType: "wechat-worker", CurrentVersion: "1.0.0",
		TargetVersion: "2.0.0", ExpectedSHA256: fmt.Sprintf("%x", digest),
		ExpectedSize: int64(len(newBinary)), StagedBinary: staged,
		Platform: runtime.GOOS, Architecture: runtime.GOARCH, ProtocolVersion: "2026-08-01",
		Revision: sourceAgentUpdateTestRevision, Channel: "staging",
	}
	return sourceAgentUpdateFixture{
		transaction: transaction, request: request, executable: executable, staged: staged,
		oldBinary: oldBinary, newBinary: newBinary, guard: guard, process: process,
		receipts: receipts, fs: fs,
	}
}

func TestSourceAgentUpdateRejectsInvalidRequestWithoutChangingExecutable(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SourceAgentUpdateRequest)
	}{
		{name: "missing artifact", mutate: func(r *SourceAgentUpdateRequest) { r.ArtifactID = "" }},
		{name: "noncanonical artifact", mutate: func(r *SourceAgentUpdateRequest) { r.ArtifactID = " artifact-1 " }},
		{name: "artifact path segment", mutate: func(r *SourceAgentUpdateRequest) { r.ArtifactID = ".." }},
		{name: "wrong current version", mutate: func(r *SourceAgentUpdateRequest) { r.CurrentVersion = "0.9.0" }},
		{name: "non upgrade target", mutate: func(r *SourceAgentUpdateRequest) { r.TargetVersion = "1.0.0" }},
		{name: "wrong worker", mutate: func(r *SourceAgentUpdateRequest) { r.WorkerType = "wcplus-worker" }},
		{name: "wrong platform", mutate: func(r *SourceAgentUpdateRequest) { r.Platform = "other" }},
		{name: "wrong architecture", mutate: func(r *SourceAgentUpdateRequest) { r.Architecture = "other" }},
		{name: "wrong protocol", mutate: func(r *SourceAgentUpdateRequest) { r.ProtocolVersion = "2026-07-01" }},
		{name: "bad revision", mutate: func(r *SourceAgentUpdateRequest) { r.Revision = "main" }},
		{name: "bad channel", mutate: func(r *SourceAgentUpdateRequest) { r.Channel = "nightly" }},
		{name: "path escape", mutate: func(r *SourceAgentUpdateRequest) {
			r.StagedBinary = filepath.Dir(filepath.Dir(r.StagedBinary)) + string(os.PathSeparator) + "other"
		}},
		{name: "oversize", mutate: func(r *SourceAgentUpdateRequest) { r.ExpectedSize = sourceAgentArtifactMaxBytes + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSourceAgentUpdateFixture(t)
			test.mutate(&fixture.request)
			result := fixture.transaction.Apply(context.Background(), fixture.request)
			if result.Outcome != SourceAgentUpdateOutcomeFailed || result.Code == "" {
				t.Fatalf("result=%#v", result)
			}
			assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
			if fixture.guard.calls != 0 || fixture.process.calls != 0 {
				t.Fatalf("guard=%d process=%d", fixture.guard.calls, fixture.process.calls)
			}
			assertSourceAgentUpdateResultRedacted(t, result)
		})
	}
}

func TestSourceAgentUpdateRejectsStagedIntegrityAndFileType(t *testing.T) {
	t.Run("size mismatch", func(t *testing.T) {
		fixture := newSourceAgentUpdateFixture(t)
		fixture.request.ExpectedSize++
		result := fixture.transaction.Apply(context.Background(), fixture.request)
		if result.Code != SourceAgentCommandCodeVerificationFailed {
			t.Fatalf("result=%#v", result)
		}
		assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	})
	t.Run("hash mismatch", func(t *testing.T) {
		fixture := newSourceAgentUpdateFixture(t)
		fixture.request.ExpectedSHA256 = strings.Repeat("0", sha256.Size*2)
		result := fixture.transaction.Apply(context.Background(), fixture.request)
		if result.Code != SourceAgentCommandCodeVerificationFailed {
			t.Fatalf("result=%#v", result)
		}
		assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	})
	t.Run("symlink", func(t *testing.T) {
		fixture := newSourceAgentUpdateFixture(t)
		if err := os.Remove(fixture.staged); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(fixture.executable, fixture.staged); err != nil {
			t.Fatal(err)
		}
		result := fixture.transaction.Apply(context.Background(), fixture.request)
		if result.Code != SourceAgentCommandCodeVerificationFailed {
			t.Fatalf("result=%#v", result)
		}
		assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	})
	t.Run("intermediate symlink escape", func(t *testing.T) {
		fixture := newSourceAgentUpdateFixture(t)
		outside := t.TempDir()
		outsideArtifact := filepath.Join(outside, "artifact")
		if err := os.WriteFile(outsideArtifact, fixture.newBinary, 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(filepath.Dir(fixture.staged), "linked")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		fixture.request.StagedBinary = filepath.Join(link, "artifact")
		result := fixture.transaction.Apply(context.Background(), fixture.request)
		if result.Code != SourceAgentCommandCodeVerificationFailed {
			t.Fatalf("result=%#v", result)
		}
		assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	})
	t.Run("non regular", func(t *testing.T) {
		fixture := newSourceAgentUpdateFixture(t)
		if err := os.Remove(fixture.staged); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.staged, 0o700); err != nil {
			t.Fatal(err)
		}
		result := fixture.transaction.Apply(context.Background(), fixture.request)
		if result.Code != SourceAgentCommandCodeVerificationFailed {
			t.Fatalf("result=%#v", result)
		}
		assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	})
}

func TestSourceAgentUpdateUsesOpenedStagedSnapshotWhenPathIsReplaced(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.fs.mutatePath = fixture.staged
	fixture.fs.mutateData = []byte("malicious replacement")
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Outcome != SourceAgentUpdateOutcomeSucceeded {
		t.Fatalf("result=%#v", result)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.newBinary)
}

func TestSourceAgentUpdateFailureMatrixRestoresOldExecutable(t *testing.T) {
	tests := []struct {
		name         string
		configure    func(*sourceAgentUpdateFixture)
		wantOutcome  string
		wantCode     string
		wantRestored bool
		wantRestarts int
	}{
		{name: "prepared sync", configure: func(f *sourceAgentUpdateFixture) { f.fs.fail = "prepared_sync" }, wantOutcome: SourceAgentUpdateOutcomeFailed, wantCode: SourceAgentCommandCodeInstallFailed},
		{name: "prepared directory sync", configure: func(f *sourceAgentUpdateFixture) { f.fs.fail, f.fs.dirSyncNth = "dir_sync", 1 }, wantOutcome: SourceAgentUpdateOutcomeFailed, wantCode: SourceAgentCommandCodeInstallFailed},
		{name: "backup failure", configure: func(f *sourceAgentUpdateFixture) { f.fs.fail = "backup" }, wantOutcome: SourceAgentUpdateOutcomeFailed, wantCode: SourceAgentCommandCodeInstallFailed},
		{name: "backup directory sync", configure: func(f *sourceAgentUpdateFixture) { f.fs.fail, f.fs.dirSyncNth = "dir_sync", 2 }, wantOutcome: SourceAgentUpdateOutcomeFailed, wantCode: SourceAgentCommandCodeInstallFailed},
		{name: "atomic replace failure", configure: func(f *sourceAgentUpdateFixture) { f.fs.fail = "replace" }, wantOutcome: SourceAgentUpdateOutcomeFailed, wantCode: SourceAgentCommandCodeInstallFailed},
		{name: "restart failure rolls back", configure: func(f *sourceAgentUpdateFixture) { f.process.failCall = 1 }, wantOutcome: SourceAgentUpdateOutcomeRolledBack, wantCode: SourceAgentCommandCodeRollbackComplete, wantRestored: true, wantRestarts: 2},
		{name: "ready timeout rolls back", configure: func(f *sourceAgentUpdateFixture) { f.receipts.waitErr = ErrSourceAgentReadyTimeout }, wantOutcome: SourceAgentUpdateOutcomeRolledBack, wantCode: SourceAgentCommandCodeRollbackComplete, wantRestored: true, wantRestarts: 2},
		{name: "wrong ready receipt rolls back", configure: func(f *sourceAgentUpdateFixture) { f.receipts.waitErr = ErrSourceAgentReadyMismatch }, wantOutcome: SourceAgentUpdateOutcomeRolledBack, wantCode: SourceAgentCommandCodeRollbackComplete, wantRestored: true, wantRestarts: 2},
		{name: "rollback restart failure is terminal", configure: func(f *sourceAgentUpdateFixture) {
			f.receipts.waitErr = ErrSourceAgentReadyTimeout
			f.process.failCall = 2
		}, wantOutcome: SourceAgentUpdateOutcomeFailed, wantCode: SourceAgentCommandCodeRollbackFailed, wantRestored: true, wantRestarts: 2},
		{name: "rollback directory sync failure is terminal", configure: func(f *sourceAgentUpdateFixture) {
			f.receipts.waitErr = ErrSourceAgentReadyTimeout
			f.fs.fail, f.fs.dirSyncNth = "dir_sync", 4
		}, wantOutcome: SourceAgentUpdateOutcomeFailed, wantCode: SourceAgentCommandCodeRollbackFailed, wantRestored: true, wantRestarts: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSourceAgentUpdateFixture(t)
			test.configure(&fixture)
			result := fixture.transaction.Apply(context.Background(), fixture.request)
			if result.Outcome != test.wantOutcome || result.Code != test.wantCode || result.BinaryRestored != test.wantRestored {
				t.Fatalf("result=%#v", result)
			}
			assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
			if test.wantRestarts > 0 && fixture.process.calls != test.wantRestarts {
				t.Fatalf("restart calls=%d want=%d", fixture.process.calls, test.wantRestarts)
			}
			assertSourceAgentUpdateResultRedacted(t, result)
		})
	}
}

func TestSourceAgentUpdateRetainsBackupWhenRestoreCannotComplete(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	fixture.receipts.waitErr = ErrSourceAgentReadyTimeout
	fixture.fs.fail = "restore"
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Outcome != SourceAgentUpdateOutcomeFailed || result.Code != SourceAgentCommandCodeRollbackFailed || result.BinaryRestored {
		t.Fatalf("result=%#v", result)
	}
	backup := filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())
	assertSourceAgentExecutable(t, backup, fixture.oldBinary)
}

func TestSourceAgentUpdateGuardChecksBeforeMutationAndImmediatelyBeforeApply(t *testing.T) {
	for _, failCall := range []int{1, 2} {
		t.Run(fmt.Sprintf("guard-%d", failCall), func(t *testing.T) {
			fixture := newSourceAgentUpdateFixture(t)
			fixture.guard.failCall = failCall
			result := fixture.transaction.Apply(context.Background(), fixture.request)
			if result.Code != SourceAgentCommandCodeInstallFailed {
				t.Fatalf("result=%#v", result)
			}
			assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
			if fixture.process.calls != 0 {
				t.Fatalf("process calls=%d", fixture.process.calls)
			}
			checks := fixture.guard.snapshotChecks()
			if len(checks) != failCall {
				t.Fatalf("guard checks=%#v", checks)
			}
			want := SourceAgentUpdateGuardCheck{
				CommandID: fixture.request.CommandID, ArtifactID: fixture.request.ArtifactID,
				WorkerType: fixture.request.WorkerType, CurrentVersion: fixture.request.CurrentVersion,
				Version: fixture.request.TargetVersion, Revision: fixture.request.Revision, Channel: fixture.request.Channel,
				Size: fixture.request.ExpectedSize, SHA256: fixture.request.ExpectedSHA256,
				Platform: fixture.request.Platform, Architecture: fixture.request.Architecture,
				ProtocolVersion: fixture.request.ProtocolVersion,
			}
			for index, check := range checks {
				if check != want {
					t.Fatalf("guard check %d=%#v want=%#v", index+1, check, want)
				}
			}
		})
	}
}

func TestSourceAgentUpdateRequestFingerprintBindsArtifactID(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	original := sourceAgentUpdateRequestFingerprint(fixture.request)
	changed := fixture.request
	changed.ArtifactID = "artifact-2"
	if sourceAgentUpdateRequestFingerprint(changed) == original {
		t.Fatal("request fingerprint did not bind artifact_id")
	}
}

func TestSourceAgentUpdateSuccessIsIdempotentAndConcurrentRequestIsBusy(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Outcome != SourceAgentUpdateOutcomeSucceeded || result.RuntimeVersion != "2.0.0" || fixture.process.calls != 1 {
		t.Fatalf("result=%#v process=%d", result, fixture.process.calls)
	}
	replayed := fixture.transaction.Apply(context.Background(), fixture.request)
	if replayed != result || fixture.process.calls != 1 || fixture.guard.calls != 2 {
		t.Fatalf("replayed=%#v process=%d guard=%d", replayed, fixture.process.calls, fixture.guard.calls)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.newBinary)
	conflict := fixture.request
	conflict.ExpectedSHA256 = strings.Repeat("0", sha256.Size*2)
	conflicted := fixture.transaction.Apply(context.Background(), conflict)
	if conflicted.Outcome != SourceAgentUpdateOutcomeFailed || conflicted.Code != sourceAgentUpdateCodeInvalidRequest || fixture.process.calls != 1 {
		t.Fatalf("conflicted=%#v process=%d", conflicted, fixture.process.calls)
	}

	busy := newSourceAgentUpdateFixture(t)
	release, err := busy.fs.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	busyResult := busy.transaction.Apply(context.Background(), busy.request)
	if busyResult.Code != SourceAgentUpdateCodeBusy || busyResult.Outcome != SourceAgentUpdateOutcomeFailed {
		t.Fatalf("busy result=%#v", busyResult)
	}
	assertSourceAgentExecutable(t, busy.executable, busy.oldBinary)
}

func TestSourceAgentUpdateCancellationBoundaries(t *testing.T) {
	t.Run("before mutation", func(t *testing.T) {
		fixture := newSourceAgentUpdateFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := fixture.transaction.Apply(ctx, fixture.request)
		if result.Code != SourceAgentUpdateCodeCanceled || fixture.guard.calls != 0 || fixture.process.calls != 0 {
			t.Fatalf("result=%#v guard=%d process=%d", result, fixture.guard.calls, fixture.process.calls)
		}
		assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	})
	t.Run("immediately after replace", func(t *testing.T) {
		fixture := newSourceAgentUpdateFixture(t)
		fixture.process.rejectCanceled = true
		ctx, cancel := context.WithCancel(context.Background())
		fixture.fs.afterReplace = cancel
		result := fixture.transaction.Apply(ctx, fixture.request)
		if result.Outcome != SourceAgentUpdateOutcomeRolledBack || !result.BinaryRestored {
			t.Fatalf("result=%#v", result)
		}
		assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	})
	t.Run("after replace", func(t *testing.T) {
		fixture := newSourceAgentUpdateFixture(t)
		waitStart := make(chan struct{})
		fixture.receipts.waitStart = waitStart
		fixture.receipts.waitBlock = make(chan struct{})
		fixture.process.rejectCanceled = true
		ctx, cancel := context.WithCancel(context.Background())
		resultChannel := make(chan SourceAgentUpdateResult, 1)
		go func() { resultChannel <- fixture.transaction.Apply(ctx, fixture.request) }()
		<-waitStart
		cancel()
		result := <-resultChannel
		if result.Outcome != SourceAgentUpdateOutcomeRolledBack || !result.BinaryRestored {
			t.Fatalf("result=%#v", result)
		}
		assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	})
}

func TestSourceAgentUpdateReadyReceiptIsStrictBoundedAndIdentityBound(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	receipt := SourceAgentReadyReceipt{
		CommandID: "command-1", AttemptNonce: strings.Repeat("a", sha256.Size*2), WorkerType: "wechat-worker", Version: "2.0.0",
		Platform: runtime.GOOS, Architecture: runtime.GOARCH, ProtocolVersion: "2026-08-01",
		Revision: sourceAgentUpdateTestRevision, HeartbeatAuthenticated: true,
	}
	seedSourceAgentReadyJournal(t, store, receipt)
	if err := store.WriteReady(receipt); err != nil {
		t.Fatal(err)
	}
	expectation := SourceAgentReadyExpectation{
		CommandID: receipt.CommandID, AttemptNonce: receipt.AttemptNonce, WorkerType: receipt.WorkerType, Version: receipt.Version,
		Platform: receipt.Platform, Architecture: receipt.Architecture,
		ProtocolVersion: receipt.ProtocolVersion, Revision: receipt.Revision,
	}
	if err := store.WaitReady(context.Background(), expectation, 20*time.Millisecond); err != nil {
		t.Fatalf("WaitReady() error = %v", err)
	}
	wrong := expectation
	wrong.CommandID = "command-2"
	if err := store.WaitReady(context.Background(), wrong, 2*time.Millisecond); !errors.Is(err, ErrSourceAgentReadyTimeout) {
		t.Fatalf("wrong command error=%v", err)
	}
	wrong = expectation
	wrong.Version = "2.0.1"
	if err := store.WaitReady(context.Background(), wrong, 50*time.Millisecond); !errors.Is(err, ErrSourceAgentReadyMismatch) {
		t.Fatalf("wrong version error=%v", err)
	}

	path := store.readyPath(receipt.CommandID, receipt.AttemptNonce)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.WaitReady(context.Background(), expectation, 50*time.Millisecond); !errors.Is(err, ErrSourceAgentReadyInvalid) {
		t.Fatalf("mode error=%v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), path); err != nil {
		t.Fatal(err)
	}
	if err := store.WaitReady(context.Background(), expectation, 50*time.Millisecond); !errors.Is(err, ErrSourceAgentReadyInvalid) {
		t.Fatalf("symlink error=%v", err)
	}
}

func TestSourceAgentUpdateReadyReceiptRejectsUnknownTrailingOversizedAndConflict(t *testing.T) {
	newStore := func(t *testing.T) (*FileSourceAgentUpdateReceiptStore, SourceAgentReadyReceipt) {
		t.Helper()
		root := t.TempDir()
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatal(err)
		}
		store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		return store, SourceAgentReadyReceipt{
			CommandID: "command-1", AttemptNonce: strings.Repeat("a", sha256.Size*2), WorkerType: "wechat-worker", Version: "2.0.0",
			Platform: runtime.GOOS, Architecture: runtime.GOARCH, ProtocolVersion: "2026-08-01",
			Revision: sourceAgentUpdateTestRevision, HeartbeatAuthenticated: true,
		}
	}
	expectation := func(receipt SourceAgentReadyReceipt) SourceAgentReadyExpectation {
		return SourceAgentReadyExpectation{
			CommandID: receipt.CommandID, AttemptNonce: receipt.AttemptNonce, WorkerType: receipt.WorkerType, Version: receipt.Version,
			Platform: receipt.Platform, Architecture: receipt.Architecture,
			ProtocolVersion: receipt.ProtocolVersion, Revision: receipt.Revision,
		}
	}
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "unknown", raw: `{"command_id":"command-1","worker_type":"wechat-worker","version":"2.0.0","platform":"darwin","architecture":"arm64","protocol_version":"2026-08-01","revision":"0123456789abcdef0123456789abcdef01234567","heartbeat_authenticated":true,"extra":true}`},
		{name: "trailing", raw: `{"command_id":"command-1"} {}`},
		{name: "oversized", raw: strings.Repeat("x", sourceAgentUpdateReceiptMaxBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, receipt := newStore(t)
			seedSourceAgentReadyJournal(t, store, receipt)
			if err := os.WriteFile(store.readyPath(receipt.CommandID, receipt.AttemptNonce), []byte(test.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := store.WaitReady(context.Background(), expectation(receipt), 50*time.Millisecond); !errors.Is(err, ErrSourceAgentReadyInvalid) {
				t.Fatalf("error=%v", err)
			}
		})
	}

	store, receipt := newStore(t)
	seedSourceAgentReadyJournal(t, store, receipt)
	if err := store.WriteReady(receipt); err != nil {
		t.Fatal(err)
	}
	conflict := receipt
	conflict.Version = "2.0.1"
	if err := store.WriteReady(conflict); err == nil {
		t.Fatal("conflicting ready receipt should not overwrite the original")
	}
	if err := store.WaitReady(context.Background(), expectation(receipt), 50*time.Millisecond); err != nil {
		t.Fatalf("original receipt changed: %v", err)
	}
}

func seedSourceAgentReadyJournal(t *testing.T, store *FileSourceAgentUpdateReceiptStore, receipt SourceAgentReadyReceipt) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	journal := sourceAgentUpdateJournal{
		SchemaVersion: sourceAgentUpdateJournalSchema, CommandID: receipt.CommandID,
		AttemptNonce: receipt.AttemptNonce, RequestFingerprint: strings.Repeat("b", sha256.Size*2),
		WorkerType: receipt.WorkerType, CurrentVersion: "1.0.0", TargetVersion: receipt.Version,
		Platform: receipt.Platform, Architecture: receipt.Architecture, ProtocolVersion: receipt.ProtocolVersion,
		Revision: receipt.Revision, Channel: "staging", Stage: "restarted",
		Backup: SourceAgentBinaryIdentity{
			Size: 1, SHA256: strings.Repeat("c", sha256.Size*2), Device: 1, Inode: 1,
		},
		StartedAt: now, UpdatedAt: now,
	}
	if err := store.saveJournal(journal); err != nil {
		t.Fatal(err)
	}
}

func TestSourceAgentUpdateConstructorRejectsUnfixedLocalPaths(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	_, err := NewSourceAgentUpdateTransaction(SourceAgentUpdateConfig{
		WorkerType: "wechat-worker", Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		CurrentVersion: "1.0.0", ProtocolVersion: "2026-08-01",
		CurrentExecutable: fixture.executable, StagingRoot: filepath.Dir(fixture.staged),
		BackupRoot:  filepath.Join(filepath.Dir(fixture.executable), "other"),
		ReceiptRoot: t.TempDir(), Guard: fixture.guard, ProcessControl: fixture.process,
	})
	if err == nil {
		t.Fatal("expected backup root outside executable directory to fail")
	}
}

func assertSourceAgentExecutable(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("executable=%q want=%q", got, want)
	}
}

func assertSourceAgentUpdateResultRedacted(t *testing.T, result SourceAgentUpdateResult) {
	t.Helper()
	raw := fmt.Sprintf("%#v", result)
	for _, forbidden := range []string{"private", string(os.PathSeparator) + "tmp", "cookie", "token="} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(forbidden)) {
			t.Fatalf("result leaks %q: %s", forbidden, raw)
		}
	}
}
