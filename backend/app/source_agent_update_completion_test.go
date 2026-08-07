package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceAgentUpdatePreparesAcknowledgedTerminalCleanupWithoutRestoring(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Outcome != SourceAgentUpdateOutcomeSucceeded {
		t.Fatalf("Apply() result = %#v", result)
	}
	resolution := sourceAgentUpdateCompletionResolution(fixture.request, SourceAgentUpgradeTerminalAcknowledge)
	if err := fixture.transaction.PrepareTerminalResolution(context.Background(), fixture.request, resolution); err != nil {
		t.Fatalf("PrepareTerminalResolution() error = %v", err)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.newBinary)
	journal, found, err := fixture.receipts.loadJournal()
	if err != nil || !found || journal.Stage != "terminal_cleanup" {
		t.Fatalf("journal=%#v found=%t error=%v", journal, found, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.executable), sourceAgentUpdateBackupName())); !os.IsNotExist(err) {
		t.Fatalf("backup still exists: %v", err)
	}
}

func TestSourceAgentUpdatePreparesConflictingTerminalByRestoringWorker(t *testing.T) {
	fixture := newSourceAgentUpdateFixture(t)
	result := fixture.transaction.Apply(context.Background(), fixture.request)
	if result.Outcome != SourceAgentUpdateOutcomeSucceeded {
		t.Fatalf("Apply() result = %#v", result)
	}
	resolution := sourceAgentUpdateCompletionResolution(fixture.request, SourceAgentUpgradeTerminalRollback)
	if err := fixture.transaction.PrepareTerminalResolution(context.Background(), fixture.request, resolution); err != nil {
		t.Fatalf("PrepareTerminalResolution() error = %v", err)
	}
	assertSourceAgentExecutable(t, fixture.executable, fixture.oldBinary)
	journal, found, err := fixture.receipts.loadJournal()
	if err != nil || !found || journal.Stage != "terminal_cleanup" {
		t.Fatalf("journal=%#v found=%t error=%v", journal, found, err)
	}
	if fixture.process.calls != 2 {
		t.Fatalf("restart calls=%d, want apply plus rollback restart", fixture.process.calls)
	}
}

func sourceAgentUpdateCompletionResolution(
	request SourceAgentUpdateRequest,
	action string,
) SourceAgentUpgradeTerminalResolution {
	resolution := SourceAgentUpgradeTerminalResolution{
		SchemaVersion:       sourceAgentUpgradeTerminalResolutionSchema,
		Reason:              SourceAgentUpgradeResolutionServerTerminal,
		CommandID:           request.CommandID,
		CommandFingerprint:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		RequestFingerprint:  sourceAgentUpdateRequestFingerprint(request),
		Action:              action,
		LocalOutcome:        SourceAgentUpdateOutcomeSucceeded,
		LocalCode:           SourceAgentCommandCodeUpgradeComplete,
		LocalRuntimeVersion: request.TargetVersion,
		ReplacementOccurred: true,
	}
	if action == SourceAgentUpgradeTerminalAcknowledge {
		resolution.ServerState = SourceAgentCommandSucceeded
		resolution.ServerResultCode = SourceAgentCommandCodeUpgradeComplete
		resolution.ServerActualVersion = request.TargetVersion
	} else {
		resolution.ServerState = SourceAgentCommandCanceled
		resolution.ServerResultCode = SourceAgentCommandCodeCanceled
	}
	return resolution
}
