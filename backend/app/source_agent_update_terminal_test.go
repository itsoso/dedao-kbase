package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSourceAgentUpgradeTerminalResolutionAcknowledgesMatchingEvidence(t *testing.T) {
	tests := []struct {
		name          string
		serverState   string
		serverCode    string
		serverVersion string
		journalStage  string
		localOutcome  string
		localCode     string
		localVersion  string
		restored      bool
	}{
		{
			name: "succeeded", serverState: SourceAgentCommandSucceeded,
			serverCode: SourceAgentCommandCodeUpgradeComplete, serverVersion: "2.0.0",
			journalStage: "ready", localOutcome: SourceAgentUpdateOutcomeSucceeded,
			localCode: SourceAgentCommandCodeUpgradeComplete, localVersion: "2.0.0",
		},
		{
			name: "rolled back", serverState: SourceAgentCommandRolledBack,
			serverCode:   SourceAgentCommandCodeRollbackComplete,
			journalStage: "rollback_restored", localOutcome: SourceAgentUpdateOutcomeRolledBack,
			localCode: SourceAgentCommandCodeRollbackComplete, localVersion: "1.0.0", restored: true,
		},
		{
			name: "failed before replacement", serverState: SourceAgentCommandFailed,
			serverCode:   SourceAgentCommandCodeInstallFailed,
			journalStage: "backup_durable", localOutcome: SourceAgentUpdateOutcomeFailed,
			localCode: SourceAgentCommandCodeInstallFailed, localVersion: "1.0.0",
		},
		{
			name: "canceled before replacement", serverState: SourceAgentCommandCanceled,
			serverCode:   SourceAgentCommandCodeCanceled,
			journalStage: "started", localOutcome: SourceAgentUpdateOutcomeFailed,
			localCode: SourceAgentUpdateCodeCanceled, localVersion: "1.0.0",
		},
		{
			name: "expired before replacement", serverState: SourceAgentCommandExpired,
			serverCode:   SourceAgentCommandCodeExpired,
			journalStage: "started", localOutcome: SourceAgentUpdateOutcomeFailed,
			localCode: SourceAgentUpdateCodeCanceled, localVersion: "1.0.0",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSourceAgentTerminalResolutionFixture(t)
			fixture.seedLocalEvidence(t, test.journalStage, test.localOutcome, test.localCode, test.localVersion, test.restored)
			fixture.recordServerTerminal(t, test.serverState, test.serverCode, test.serverVersion)

			resolution, err := fixture.store.ResolveUpgradeTerminalObservation(context.Background())
			if err != nil {
				t.Fatalf("ResolveUpgradeTerminalObservation() error=%v", err)
			}
			if resolution.Action != SourceAgentUpgradeTerminalAcknowledge ||
				resolution.CommandID != fixture.command.ID ||
				resolution.CommandFingerprint != sourceAgentUpgradeCommandFingerprint(fixture.command) ||
				resolution.RequestFingerprint != sourceAgentUpdateRequestFingerprint(fixture.request) {
				t.Fatalf("resolution=%#v", resolution)
			}
			loaded, found, err := fixture.store.LoadUpgradeTerminalResolution()
			if err != nil || !found || loaded != resolution {
				t.Fatalf("loaded=%#v found=%t error=%v", loaded, found, err)
			}
			replayed, err := fixture.store.ResolveUpgradeTerminalObservation(context.Background())
			if err != nil || replayed != resolution {
				t.Fatalf("replayed=%#v error=%v", replayed, err)
			}
			fixture.assertRecoveryEvidenceRetained(t)
		})
	}
}

func TestSourceAgentUpgradeTerminalResolutionRequestsRollbackOnPostReplacementConflict(t *testing.T) {
	fixture := newSourceAgentTerminalResolutionFixture(t)
	fixture.seedLocalEvidence(
		t, "ready", SourceAgentUpdateOutcomeSucceeded,
		SourceAgentCommandCodeUpgradeComplete, "2.0.0", false,
	)
	fixture.recordServerTerminal(t, SourceAgentCommandCanceled, SourceAgentCommandCodeCanceled, "")

	resolution, err := fixture.store.ResolveUpgradeTerminalObservation(context.Background())
	if err != nil {
		t.Fatalf("ResolveUpgradeTerminalObservation() error=%v", err)
	}
	if resolution.Action != SourceAgentUpgradeTerminalRollback || resolution.ServerState != SourceAgentCommandCanceled ||
		resolution.LocalOutcome != SourceAgentUpdateOutcomeSucceeded {
		t.Fatalf("resolution=%#v", resolution)
	}
	fixture.assertRecoveryEvidenceRetained(t)
	payload, err := fixture.store.directory.read(sourceAgentUpgradeTerminalResolutionFileName, sourceAgentUpdateReceiptMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	absoluteHomePrefix := string(os.PathSeparator) + "Users" + string(os.PathSeparator)
	for _, forbidden := range []string{"token", "cookie", "url", "path", "label", "script", "environment", absoluteHomePrefix} {
		if strings.Contains(strings.ToLower(string(payload)), strings.ToLower(forbidden)) {
			t.Fatalf("resolution leaked forbidden content %q: %s", forbidden, payload)
		}
	}
}

func TestSourceAgentUpgradeTerminalResolutionRequestsRollbackOnSuccessVersionMismatch(t *testing.T) {
	fixture := newSourceAgentTerminalResolutionFixture(t)
	fixture.seedLocalEvidence(
		t, "ready", SourceAgentUpdateOutcomeSucceeded,
		SourceAgentCommandCodeUpgradeComplete, "2.0.0", false,
	)
	fixture.recordServerTerminal(t, SourceAgentCommandSucceeded, SourceAgentCommandCodeUpgradeComplete, "3.0.0")
	resolution, err := fixture.store.ResolveUpgradeTerminalObservation(context.Background())
	if err != nil {
		t.Fatalf("ResolveUpgradeTerminalObservation() error=%v", err)
	}
	if resolution.Action != SourceAgentUpgradeTerminalRollback ||
		resolution.ServerActualVersion != "3.0.0" || resolution.LocalRuntimeVersion != "2.0.0" {
		t.Fatalf("resolution=%#v", resolution)
	}
}

func TestSourceAgentUpgradeConflictRequestsRollbackOnlyFromLocalReplacementEvidence(t *testing.T) {
	t.Run("replacement", func(t *testing.T) {
		fixture := newSourceAgentTerminalResolutionFixture(t)
		fixture.seedLocalEvidence(
			t, "ready", SourceAgentUpdateOutcomeSucceeded,
			SourceAgentCommandCodeUpgradeComplete, "2.0.0", false,
		)
		fingerprint := sourceAgentUpgradeCommandFingerprint(fixture.command)
		if err := fixture.store.RecordAuthenticatedUpgradeConflict(
			context.Background(), fixture.command.ID, fingerprint,
		); err != nil {
			t.Fatal(err)
		}
		resolution, err := fixture.store.ResolveUpgradeConflict(context.Background())
		if err != nil {
			t.Fatalf("ResolveUpgradeConflict() error=%v", err)
		}
		if resolution.Reason != SourceAgentUpgradeResolutionFingerprintConflict ||
			resolution.Action != SourceAgentUpgradeTerminalRollback ||
			resolution.CommandID != fixture.command.ID || resolution.CommandFingerprint != fingerprint ||
			resolution.ServerState != "" || resolution.ServerResultCode != "" || resolution.ServerActualVersion != "" {
			t.Fatalf("resolution=%#v", resolution)
		}
		payload, err := fixture.store.directory.read(sourceAgentUpgradeConflictFileName, sourceAgentUpdateReceiptMaxBytes)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"remote_", "2.0.0", "3.0.0", "message", "path", "token", "url"} {
			if strings.Contains(strings.ToLower(string(payload)), forbidden) {
				t.Fatalf("conflict marker leaked %q: %s", forbidden, payload)
			}
		}
	})

	t.Run("before replacement remains blocked", func(t *testing.T) {
		fixture := newSourceAgentTerminalResolutionFixture(t)
		fingerprint := sourceAgentUpgradeCommandFingerprint(fixture.command)
		if err := fixture.store.RecordAuthenticatedUpgradeConflict(
			context.Background(), fixture.command.ID, fingerprint,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.ResolveUpgradeConflict(context.Background()); !errors.Is(err, ErrSourceAgentUpgradeTerminalResolutionInvalid) {
			t.Fatalf("ResolveUpgradeConflict() error=%v", err)
		}
		if _, found, err := fixture.store.LoadUpgradeConflict(); err != nil || !found {
			t.Fatalf("conflict marker found=%t error=%v", found, err)
		}
		if _, found, err := fixture.store.LoadUpgradeCommandCheckpoint(); err != nil || !found {
			t.Fatalf("checkpoint found=%t error=%v", found, err)
		}
	})
}

func TestSourceAgentUpgradeTerminalResolutionFailsClosedWithoutConclusiveEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*sourceAgentTerminalResolutionFixture, *testing.T)
	}{
		{
			name: "missing outcome",
			mutate: func(f *sourceAgentTerminalResolutionFixture, t *testing.T) {
				f.seedJournal(t, "ready")
				f.recordServerTerminal(t, SourceAgentCommandSucceeded, SourceAgentCommandCodeUpgradeComplete, "2.0.0")
			},
		},
		{
			name: "request fingerprint mismatch",
			mutate: func(f *sourceAgentTerminalResolutionFixture, t *testing.T) {
				f.seedLocalEvidence(t, "ready", SourceAgentUpdateOutcomeSucceeded, SourceAgentCommandCodeUpgradeComplete, "2.0.0", false)
				outcome, found, err := f.store.LoadOutcome(f.command.ID)
				if err != nil || !found {
					t.Fatalf("LoadOutcome() found=%t error=%v", found, err)
				}
				if err := f.store.directory.remove(sourceAgentUpdateReceiptName("outcome", f.command.ID, "")); err != nil {
					t.Fatal(err)
				}
				outcome.RequestFingerprint = strings.Repeat("f", 64)
				if err := f.store.SaveOutcome(outcome); err != nil {
					t.Fatal(err)
				}
				f.recordServerTerminal(t, SourceAgentCommandSucceeded, SourceAgentCommandCodeUpgradeComplete, "2.0.0")
			},
		},
		{
			name: "rollback failed",
			mutate: func(f *sourceAgentTerminalResolutionFixture, t *testing.T) {
				f.seedLocalEvidence(t, "replaced", SourceAgentUpdateOutcomeFailed, SourceAgentCommandCodeRollbackFailed, "1.0.0", false)
				f.recordServerTerminal(t, SourceAgentCommandFailed, SourceAgentCommandCodeRollbackFailed, "")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSourceAgentTerminalResolutionFixture(t)
			test.mutate(&fixture, t)
			if _, err := fixture.store.ResolveUpgradeTerminalObservation(context.Background()); !errors.Is(err, ErrSourceAgentUpgradeTerminalResolutionInvalid) {
				t.Fatalf("ResolveUpgradeTerminalObservation() error=%v", err)
			}
			if _, found, err := fixture.store.LoadUpgradeTerminalResolution(); err != nil || found {
				t.Fatalf("resolution found=%t error=%v", found, err)
			}
			fixture.assertRecoveryEvidenceRetained(t)
		})
	}
}

func TestSourceAgentUpgradeTerminalResolutionRejectsCorruptOrConflictingResolution(t *testing.T) {
	t.Run("corrupt", func(t *testing.T) {
		fixture := newSourceAgentTerminalResolutionFixture(t)
		if err := fixture.store.directory.writeAtomic(sourceAgentUpgradeTerminalResolutionFileName, []byte(`{"schema_version":"wrong"}`)); err != nil {
			t.Fatal(err)
		}
		if _, found, err := fixture.store.LoadUpgradeTerminalResolution(); !found || !errors.Is(err, ErrSourceAgentUpgradeTerminalResolutionInvalid) {
			t.Fatalf("LoadUpgradeTerminalResolution() found=%t error=%v", found, err)
		}
	})
	t.Run("immutable conflict", func(t *testing.T) {
		fixture := newSourceAgentTerminalResolutionFixture(t)
		fixture.seedLocalEvidence(t, "ready", SourceAgentUpdateOutcomeSucceeded, SourceAgentCommandCodeUpgradeComplete, "2.0.0", false)
		fixture.recordServerTerminal(t, SourceAgentCommandSucceeded, SourceAgentCommandCodeUpgradeComplete, "2.0.0")
		conflict := SourceAgentUpgradeTerminalResolution{
			SchemaVersion: sourceAgentUpgradeTerminalResolutionSchema,
			Reason:        SourceAgentUpgradeResolutionServerTerminal,
			CommandID:     fixture.command.ID, CommandFingerprint: sourceAgentUpgradeCommandFingerprint(fixture.command),
			RequestFingerprint: sourceAgentUpdateRequestFingerprint(fixture.request), Action: SourceAgentUpgradeTerminalRollback,
			ServerState: SourceAgentCommandCanceled, ServerResultCode: SourceAgentCommandCodeCanceled,
			LocalOutcome: SourceAgentUpdateOutcomeSucceeded, LocalCode: SourceAgentCommandCodeUpgradeComplete,
			LocalRuntimeVersion: "2.0.0",
			ReplacementOccurred: true,
		}
		if !validSourceAgentUpgradeTerminalResolution(conflict) {
			t.Fatal("test conflict resolution is invalid")
		}
		payload, err := marshalSourceAgentUpdateJSON(conflict)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.store.directory.writeImmutable(sourceAgentUpgradeTerminalResolutionFileName, payload); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.ResolveUpgradeTerminalObservation(context.Background()); !errors.Is(err, ErrSourceAgentUpgradeTerminalResolutionConflict) {
			t.Fatalf("ResolveUpgradeTerminalObservation() error=%v", err)
		}
		if _, found, err := fixture.store.LoadUpgradeTerminalResolution(); !found || !errors.Is(err, ErrSourceAgentUpgradeTerminalResolutionInvalid) {
			t.Fatalf("LoadUpgradeTerminalResolution() found=%t error=%v", found, err)
		}
	})
}

type sourceAgentTerminalResolutionFixture struct {
	store   *FileSourceAgentUpdateReceiptStore
	command SourceAgentCommand
	request SourceAgentUpdateRequest
}

func newSourceAgentTerminalResolutionFixture(t *testing.T) sourceAgentTerminalResolutionFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSourceAgentUpdateReceiptStore(root, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	command := sourceAgentTerminalResolutionCommand()
	if err := store.SaveUpgradeCommandCheckpoint(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	request := SourceAgentUpdateRequest{
		CommandID: command.ID, ArtifactID: command.UpgradeSpec.ArtifactID,
		WorkerType: "wechat-worker", CurrentVersion: "1.0.0", TargetVersion: "2.0.0",
		ExpectedSHA256: strings.Repeat("a", 64), ExpectedSize: 128, StagedBinary: "worker.staged",
		Platform: "darwin", Architecture: "arm64", ProtocolVersion: "2026-08-01",
		Revision: sourceAgentUpdateTestRevision, Channel: "staging",
	}
	return sourceAgentTerminalResolutionFixture{store: store, command: command, request: request}
}

func sourceAgentTerminalResolutionCommand() SourceAgentCommand {
	return SourceAgentCommand{
		ID: "command-terminal", TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade,
		UpgradeSpec: &SourceAgentUpgradeSpec{ArtifactID: "artifact-2", ExpectedCurrentVersion: "1.0.0"},
		State:       SourceAgentCommandClaimed, IdempotencyKey: "upgrade-terminal",
		ExpectedCurrentVersion: "1.0.0", ClaimOwner: "agent-a",
		CreatedAt: "2026-08-01T12:00:00.000000000Z", UpdatedAt: "2026-08-01T12:00:01.000000000Z",
		ClaimedAt: "2026-08-01T12:00:01.000000000Z", ExpiresAt: "2026-08-01T13:00:00.000000000Z",
	}
}

func (f *sourceAgentTerminalResolutionFixture) seedJournal(t *testing.T, stage string) sourceAgentUpdateJournal {
	t.Helper()
	now := "2026-08-01T12:00:01.000000000Z"
	journal := sourceAgentUpdateJournal{
		SchemaVersion: sourceAgentUpdateJournalSchema, CommandID: f.request.CommandID,
		AttemptNonce: strings.Repeat("b", 64), RequestFingerprint: sourceAgentUpdateRequestFingerprint(f.request),
		WorkerType: f.request.WorkerType, CurrentVersion: f.request.CurrentVersion, TargetVersion: f.request.TargetVersion,
		Platform: f.request.Platform, Architecture: f.request.Architecture, ProtocolVersion: f.request.ProtocolVersion,
		Revision: f.request.Revision, Channel: f.request.Channel, Stage: stage, StartedAt: now, UpdatedAt: now,
	}
	if stage != "started" {
		journal.Backup = SourceAgentBinaryIdentity{Size: 64, SHA256: strings.Repeat("c", 64), Device: 1, Inode: 1}
	}
	if err := f.store.saveJournal(journal); err != nil {
		t.Fatal(err)
	}
	return journal
}

func (f *sourceAgentTerminalResolutionFixture) seedLocalEvidence(
	t *testing.T, stage, outcome, code, runtimeVersion string, restored bool,
) {
	t.Helper()
	journal := f.seedJournal(t, stage)
	result := SourceAgentUpdateResult{
		WorkerType: f.request.WorkerType, Platform: f.request.Platform, Architecture: f.request.Architecture,
		Channel: f.request.Channel, ProtocolVersion: f.request.ProtocolVersion, RuntimeVersion: runtimeVersion,
		CommandID: f.request.CommandID, Revision: f.request.Revision,
		RequestFingerprint: journal.RequestFingerprint, Outcome: outcome, Code: code,
		Message: sourceAgentUpdatePublicMessage(code), DurationMillis: 1, BinaryRestored: restored,
	}
	if err := f.store.SaveOutcome(result); err != nil {
		t.Fatal(err)
	}
}

func (f *sourceAgentTerminalResolutionFixture) recordServerTerminal(
	t *testing.T, state, code, actualVersion string,
) {
	t.Helper()
	terminal := f.command
	terminal.State, terminal.ResultCode, terminal.ActualVersion = state, code, actualVersion
	terminal.UpdatedAt = "2026-08-01T12:00:03.000000000Z"
	terminal.CompletedAt = terminal.UpdatedAt
	if err := f.store.RecordServerTerminalObservation(context.Background(), terminal); err != nil {
		t.Fatal(err)
	}
}

func (f *sourceAgentTerminalResolutionFixture) assertRecoveryEvidenceRetained(t *testing.T) {
	t.Helper()
	if _, found, err := f.store.LoadUpgradeCommandCheckpoint(); err != nil || !found {
		t.Fatalf("checkpoint found=%t error=%v", found, err)
	}
	if _, found, err := f.store.loadJournal(); err != nil || !found {
		t.Fatalf("journal found=%t error=%v", found, err)
	}
}
