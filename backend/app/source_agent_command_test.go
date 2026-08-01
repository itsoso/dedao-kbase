package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSourceAgentCommandUpgradeLifecycle(t *testing.T) {
	store, clock := newSourceAgentCommandTestStore(t)
	registerSourceAgentCommandTestAgent(t, store, "agent-upgrade", "1.0.0")

	command, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
		TargetAgentID:  " agent-upgrade ",
		Type:           " upgrade ",
		IdempotencyKey: " upgrade-once ",
		Payload: json.RawMessage(`{
			"artifact_id":"artifact-2-0-0",
			"expected_current_version":"1.0.0"
		}`),
		ExpiresAt: clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.TargetAgentID != "agent-upgrade" || command.Type != SourceAgentCommandUpgrade || command.State != SourceAgentCommandQueued {
		t.Fatalf("created command = %#v", command)
	}
	if command.UpgradeSpec == nil || command.UpgradeSpec.ArtifactID != "artifact-2-0-0" || command.ExpectedCurrentVersion != "1.0.0" {
		t.Fatalf("typed upgrade spec = %#v", command)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, command.ExpiresAt)
	if err != nil || !expiresAt.Equal(clock.Now().Add(time.Hour)) {
		t.Fatalf("expires_at = %q, parse error = %v", command.ExpiresAt, err)
	}

	persisted, err := store.GetSourceAgentCommand(command.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted, command) {
		t.Fatalf("persisted command differs:\n got %#v\nwant %#v", persisted, command)
	}
	events := mustListSourceAgentCommandEvents(t, store, command.ID)
	assertSourceAgentCommandEventStates(t, events, SourceAgentCommandQueued)

	if _, err := store.ClaimSourceAgentCommand(command.ID, "agent-other", "process-a"); !errors.Is(err, ErrSourceAgentCommandTarget) {
		t.Fatalf("wrong target claim error = %v", err)
	}
	claimed, err := store.ClaimSourceAgentCommand(command.ID, "agent-upgrade", "process-a")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.State != SourceAgentCommandClaimed || claimed.ClaimOwner != "process-a" || claimed.ClaimedAt == "" {
		t.Fatalf("claimed command = %#v", claimed)
	}
	duplicateClaim, err := store.ClaimSourceAgentCommand(command.ID, "agent-upgrade", "process-a")
	if err != nil || duplicateClaim.State != claimed.State || duplicateClaim.ClaimOwner != claimed.ClaimOwner {
		t.Fatalf("duplicate claim = %#v, %v", duplicateClaim, err)
	}
	if _, err := store.ClaimSourceAgentCommand(command.ID, "agent-upgrade", "process-b"); !errors.Is(err, ErrSourceAgentCommandClaimOwner) {
		t.Fatalf("different owner claim error = %v", err)
	}

	for _, state := range []string{
		SourceAgentCommandDownloading,
		SourceAgentCommandVerified,
		SourceAgentCommandInstalling,
		SourceAgentCommandRestarting,
		SourceAgentCommandVerifying,
	} {
		clock.Advance(time.Second)
		command, err = store.TransitionSourceAgentCommand(command.ID, "agent-upgrade", "process-a", SourceAgentCommandTransition{State: state})
		if err != nil {
			t.Fatalf("transition to %s: %v", state, err)
		}
		if command.State != state {
			t.Fatalf("transition state = %q, want %q", command.State, state)
		}
	}
	clock.Advance(time.Second)
	completion := SourceAgentCommandTransition{
		State:         SourceAgentCommandSucceeded,
		ResultCode:    SourceAgentCommandCodeUpgradeComplete,
		Message:       " upgrade installed ",
		ActualVersion: " 2.0.0 ",
	}
	command, err = store.TransitionSourceAgentCommand(command.ID, "agent-upgrade", "process-a", completion)
	if err != nil {
		t.Fatal(err)
	}
	if command.State != SourceAgentCommandSucceeded || command.ResultCode != SourceAgentCommandCodeUpgradeComplete || command.Message != "upgrade installed" || command.ActualVersion != "2.0.0" || command.CompletedAt == "" {
		t.Fatalf("completed command = %#v", command)
	}
	beforeDuplicate := mustListSourceAgentCommandEvents(t, store, command.ID)
	duplicateCompletion, err := store.TransitionSourceAgentCommand(command.ID, "agent-upgrade", "process-a", completion)
	if err != nil || duplicateCompletion.State != SourceAgentCommandSucceeded {
		t.Fatalf("duplicate completion = %#v, %v", duplicateCompletion, err)
	}
	afterDuplicate := mustListSourceAgentCommandEvents(t, store, command.ID)
	if len(afterDuplicate) != len(beforeDuplicate) {
		t.Fatalf("duplicate completion added event: before=%d after=%d", len(beforeDuplicate), len(afterDuplicate))
	}
	conflictingCompletion := completion
	conflictingCompletion.Message = "different durable result"
	if _, err := store.TransitionSourceAgentCommand(command.ID, "agent-upgrade", "process-a", conflictingCompletion); !errors.Is(err, ErrSourceAgentCommandResultConflict) {
		t.Fatalf("conflicting completion error = %v", err)
	}

	events = mustListSourceAgentCommandEvents(t, store, command.ID)
	assertSourceAgentCommandEventStates(t, events,
		SourceAgentCommandQueued,
		SourceAgentCommandClaimed,
		SourceAgentCommandDownloading,
		SourceAgentCommandVerified,
		SourceAgentCommandInstalling,
		SourceAgentCommandRestarting,
		SourceAgentCommandVerifying,
		SourceAgentCommandSucceeded,
	)
	encodedEvents, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"artifact-2-0-0", "artifact_id", "expected_current_version", "/Users/", `\\Users\\`} {
		if strings.Contains(string(encodedEvents), forbidden) {
			t.Fatalf("event history leaked %q: %s", forbidden, encodedEvents)
		}
	}
}

func TestSourceAgentCommandDiagnoseLifecycleAndClaimNext(t *testing.T) {
	store, clock := newSourceAgentCommandTestStore(t)
	registerSourceAgentCommandTestAgent(t, store, "agent-diagnose", "1.0.0")
	registerSourceAgentCommandTestAgent(t, store, "agent-other", "1.0.0")

	command, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
		TargetAgentID:  "agent-diagnose",
		Type:           SourceAgentCommandDiagnose,
		IdempotencyKey: "diagnose-once",
		ExpiresAt:      clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.UpgradeSpec != nil || command.ExpectedCurrentVersion != "" {
		t.Fatalf("diagnose retained an upgrade payload: %#v", command)
	}
	if next, err := store.ClaimNextSourceAgentCommand("agent-other", "other-process"); err != nil || next != nil {
		t.Fatalf("other target claim next = %#v, %v", next, err)
	}
	claimed, err := store.ClaimNextSourceAgentCommand("agent-diagnose", "diagnostic-process")
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != command.ID || claimed.State != SourceAgentCommandClaimed {
		t.Fatalf("claim next = %#v", claimed)
	}
	completed, err := store.TransitionSourceAgentCommand(command.ID, "agent-diagnose", "diagnostic-process", SourceAgentCommandTransition{
		State:      SourceAgentCommandSucceeded,
		ResultCode: SourceAgentCommandCodeDiagnosticComplete,
		Message:    " no action required ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Message != "no action required" || completed.ResultCode != SourceAgentCommandCodeDiagnosticComplete {
		t.Fatalf("diagnose completion = %#v", completed)
	}
	assertSourceAgentCommandEventStates(t, mustListSourceAgentCommandEvents(t, store, command.ID),
		SourceAgentCommandQueued, SourceAgentCommandClaimed, SourceAgentCommandSucceeded)
}

func TestSourceAgentCommandRejectsInvalidCreation(t *testing.T) {
	store, clock := newSourceAgentCommandTestStore(t)
	registerSourceAgentCommandTestAgent(t, store, "agent-valid", "1.0.0")
	validUpgrade := SourceAgentCommandCreate{
		TargetAgentID:  "agent-valid",
		Type:           SourceAgentCommandUpgrade,
		IdempotencyKey: "valid-key",
		Payload:        json.RawMessage(`{"artifact_id":"artifact-2","expected_current_version":"1.0.0"}`),
		ExpiresAt:      clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
	}

	tests := []struct {
		name  string
		input SourceAgentCommandCreate
		want  error
	}{
		{name: "unknown target", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) { input.TargetAgentID = "missing" }), want: ErrSourceAgentNotFound},
		{name: "invalid target", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) { input.TargetAgentID = "../agent" })},
		{name: "unknown type", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) { input.Type = "shell" }), want: ErrSourceAgentCommandType},
		{name: "diagnose object payload", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) {
			input.Type = SourceAgentCommandDiagnose
			input.Payload = json.RawMessage(`{}`)
		})},
		{name: "upgrade missing payload", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) { input.Payload = nil })},
		{name: "upgrade unknown field", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) {
			input.Payload = json.RawMessage(`{"artifact_id":"artifact-2","expected_current_version":"1.0.0","url":"https://example.invalid/payload"}`)
		})},
		{name: "upgrade trailing object", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) {
			input.Payload = json.RawMessage(`{"artifact_id":"artifact-2","expected_current_version":"1.0.0"}{"script":"unsafe"}`)
		})},
		{name: "upgrade trailing token", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) {
			input.Payload = json.RawMessage(`{"artifact_id":"artifact-2","expected_current_version":"1.0.0"} true`)
		})},
		{name: "upgrade payload too large", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) {
			input.Payload = json.RawMessage(strings.Repeat(" ", sourceAgentCommandPayloadMaxBytes+1))
		})},
		{name: "artifact path", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) {
			input.Payload = json.RawMessage(`{"artifact_id":"../artifact","expected_current_version":"1.0.0"}`)
		})},
		{name: "missing expected version", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) {
			input.Payload = json.RawMessage(`{"artifact_id":"artifact-2","expected_current_version":" "}`)
		})},
		{name: "idempotency key too long", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) {
			input.IdempotencyKey = strings.Repeat("k", sourceAgentCommandIDMaxRunes+1)
		})},
		{name: "invalid idempotency key", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) { input.IdempotencyKey = "bad key" })},
		{name: "missing expiry", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) { input.ExpiresAt = "" })},
		{name: "invalid expiry", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) { input.ExpiresAt = "tomorrow" })},
		{name: "past expiry", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) { input.ExpiresAt = clock.Now().Format(time.RFC3339Nano) })},
		{name: "expiry too far", input: withSourceAgentCommandCreate(validUpgrade, func(input *SourceAgentCommandCreate) {
			input.ExpiresAt = clock.Now().Add(sourceAgentCommandMaxTTL + time.Nanosecond).Format(time.RFC3339Nano)
		})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.CreateSourceAgentCommand(test.input)
			if err == nil {
				t.Fatal("accepted invalid command")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSourceAgentCommandVersionAndIdempotencyConflicts(t *testing.T) {
	store, clock := newSourceAgentCommandTestStore(t)
	registerSourceAgentCommandTestAgent(t, store, "agent-idempotent", "1.0.0")

	input := SourceAgentCommandCreate{
		TargetAgentID:  "agent-idempotent",
		Type:           SourceAgentCommandUpgrade,
		IdempotencyKey: "upgrade-key",
		Payload:        json.RawMessage(`{"artifact_id":"artifact-2","expected_current_version":"1.0.0"}`),
		ExpiresAt:      clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
	}
	wrongVersion := input
	wrongVersion.IdempotencyKey = "wrong-version"
	wrongVersion.Payload = json.RawMessage(`{"artifact_id":"artifact-2","expected_current_version":"0.9.0"}`)
	if _, err := store.CreateSourceAgentCommand(wrongVersion); !errors.Is(err, ErrSourceAgentCommandVersionConflict) {
		t.Fatalf("version conflict error = %v", err)
	}

	first, err := store.CreateSourceAgentCommand(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateSourceAgentCommand(input)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("idempotent command ids = %q and %q", first.ID, second.ID)
	}
	if events := mustListSourceAgentCommandEvents(t, store, first.ID); len(events) != 1 {
		t.Fatalf("idempotent create events = %d", len(events))
	}

	conflict := input
	conflict.Payload = json.RawMessage(`{"artifact_id":"artifact-different","expected_current_version":"1.0.0"}`)
	if _, err := store.CreateSourceAgentCommand(conflict); !errors.Is(err, ErrSourceAgentCommandIdempotencyConflict) {
		t.Fatalf("idempotency conflict error = %v", err)
	}
}

func TestSourceAgentCommandTransitionsExpiryAndBounds(t *testing.T) {
	store, clock := newSourceAgentCommandTestStore(t)
	registerSourceAgentCommandTestAgent(t, store, "agent-transitions", "1.0.0")

	diagnose := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-transitions", "diagnose-invalid", time.Hour)
	if _, err := store.TransitionSourceAgentCommand(diagnose.ID, "agent-transitions", "process-a", SourceAgentCommandTransition{State: SourceAgentCommandSucceeded, ResultCode: SourceAgentCommandCodeDiagnosticComplete}); !errors.Is(err, ErrSourceAgentCommandInvalidState) {
		t.Fatalf("unclaimed completion error = %v", err)
	}
	if _, err := store.ClaimSourceAgentCommand(diagnose.ID, "agent-transitions", strings.Repeat("o", sourceAgentCommandIDMaxRunes+1)); err == nil {
		t.Fatal("accepted overlong claim owner")
	}
	claimed, err := store.ClaimSourceAgentCommand(diagnose.ID, "agent-transitions", "process-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionSourceAgentCommand(claimed.ID, "agent-transitions", "process-a", SourceAgentCommandTransition{
		State:      SourceAgentCommandFailed,
		ResultCode: "unknown_result",
	}); err == nil {
		t.Fatal("accepted unknown result code")
	}
	if _, err := store.TransitionSourceAgentCommand(claimed.ID, "agent-transitions", "process-a", SourceAgentCommandTransition{
		State:      SourceAgentCommandFailed,
		ResultCode: SourceAgentCommandCodeUpgradeComplete,
	}); err == nil {
		t.Fatal("accepted result code for another command protocol")
	}
	if _, err := store.TransitionSourceAgentCommand(claimed.ID, "agent-transitions", "process-a", SourceAgentCommandTransition{
		State:      SourceAgentCommandFailed,
		ResultCode: SourceAgentCommandCodeDiagnosticFailed,
		Message:    strings.Repeat("m", sourceAgentCommandMessageMaxRunes+1),
	}); err == nil {
		t.Fatal("accepted overlong result message")
	}
	if _, err := store.TransitionSourceAgentCommand(claimed.ID, "agent-transitions", "process-a", SourceAgentCommandTransition{
		State:      SourceAgentCommandFailed,
		ResultCode: SourceAgentCommandCodeDiagnosticFailed,
		Message:    "/users/example/private.log",
	}); err == nil {
		t.Fatal("accepted local path in result message")
	}
	if _, err := store.TransitionSourceAgentCommand(claimed.ID, "agent-transitions", "process-a", SourceAgentCommandTransition{
		State:      SourceAgentCommandFailed,
		ResultCode: SourceAgentCommandCodeDiagnosticFailed,
		Message:    `C:\Users\example\private.log`,
	}); err == nil {
		t.Fatal("accepted Windows local path in result message")
	}
	failed, err := store.TransitionSourceAgentCommand(claimed.ID, "agent-transitions", "process-a", SourceAgentCommandTransition{
		State:      SourceAgentCommandFailed,
		ResultCode: SourceAgentCommandCodeDiagnosticFailed,
		Message:    "dependency unavailable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionSourceAgentCommand(failed.ID, "agent-transitions", "process-a", SourceAgentCommandTransition{
		State:      SourceAgentCommandSucceeded,
		ResultCode: SourceAgentCommandCodeDiagnosticComplete,
	}); !errors.Is(err, ErrSourceAgentCommandResultConflict) {
		t.Fatalf("terminal transition error = %v", err)
	}

	expiring := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-transitions", "expires-queued", time.Minute)
	clock.Advance(time.Minute)
	if _, err := store.ClaimSourceAgentCommand(expiring.ID, "agent-transitions", "process-expired"); !errors.Is(err, ErrSourceAgentCommandExpired) {
		t.Fatalf("expired claim error = %v", err)
	}
	persisted, err := store.GetSourceAgentCommand(expiring.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != SourceAgentCommandExpired || persisted.CompletedAt == "" {
		t.Fatalf("expired persisted command = %#v", persisted)
	}
	assertSourceAgentCommandEventStates(t, mustListSourceAgentCommandEvents(t, store, expiring.ID), SourceAgentCommandQueued, SourceAgentCommandExpired)
	if _, err := store.ClaimSourceAgentCommand(expiring.ID, "agent-transitions", "process-expired"); !errors.Is(err, ErrSourceAgentCommandExpired) {
		t.Fatalf("repeat expired claim error = %v", err)
	}

	claimedExpiring := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-transitions", "expires-claimed", time.Minute)
	if _, err := store.ClaimSourceAgentCommand(claimedExpiring.ID, "agent-transitions", "process-expiring"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	if _, err := store.TransitionSourceAgentCommand(claimedExpiring.ID, "agent-transitions", "process-expiring", SourceAgentCommandTransition{
		State:      SourceAgentCommandSucceeded,
		ResultCode: SourceAgentCommandCodeDiagnosticComplete,
	}); !errors.Is(err, ErrSourceAgentCommandExpired) {
		t.Fatalf("expired transition error = %v", err)
	}
	assertSourceAgentCommandEventStates(t, mustListSourceAgentCommandEvents(t, store, claimedExpiring.ID),
		SourceAgentCommandQueued, SourceAgentCommandClaimed, SourceAgentCommandExpired)
	if _, err := store.ClaimSourceAgentCommand(claimedExpiring.ID, "agent-transitions", "process-expiring"); !errors.Is(err, ErrSourceAgentCommandExpired) {
		t.Fatalf("claimed expired command repeat claim error = %v", err)
	}
	if _, err := store.TransitionSourceAgentCommand(claimedExpiring.ID, "agent-transitions", "process-expiring", SourceAgentCommandTransition{
		State:      SourceAgentCommandSucceeded,
		ResultCode: SourceAgentCommandCodeDiagnosticComplete,
	}); !errors.Is(err, ErrSourceAgentCommandExpired) {
		t.Fatalf("claimed expired command repeat transition error = %v", err)
	}

	canceled := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-transitions", "cancel-queued", time.Hour)
	canceled, err = store.CancelSourceAgentCommand(canceled.ID, " operator canceled ")
	if err != nil {
		t.Fatal(err)
	}
	if canceled.State != SourceAgentCommandCanceled || canceled.ResultCode != SourceAgentCommandCodeCanceled || canceled.Message != "operator canceled" {
		t.Fatalf("canceled command = %#v", canceled)
	}
}

func TestSourceAgentCommandExpiryUsesChronologicalOrder(t *testing.T) {
	t.Run("fractional future remains claimable", func(t *testing.T) {
		store, clock := newSourceAgentCommandTestStore(t)
		registerSourceAgentCommandTestAgent(t, store, "agent-fractional-future", "1.0.0")
		command := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-fractional-future", "future-half-second", 500*time.Millisecond)

		claimed, err := store.ClaimNextSourceAgentCommand("agent-fractional-future", "process-future")
		if err != nil {
			t.Fatal(err)
		}
		if claimed == nil || claimed.ID != command.ID || claimed.State != SourceAgentCommandClaimed {
			t.Fatalf("future command claim = %#v", claimed)
		}
		assertSourceAgentCommandEventStates(t, mustListSourceAgentCommandEvents(t, store, command.ID),
			SourceAgentCommandQueued, SourceAgentCommandClaimed)
	})

	t.Run("whole-second expiry precedes fractional now", func(t *testing.T) {
		store, clock := newSourceAgentCommandTestStore(t)
		registerSourceAgentCommandTestAgent(t, store, "agent-fractional-past", "1.0.0")
		command := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-fractional-past", "past-tenth-second", time.Second)
		clock.Advance(time.Second + 100*time.Millisecond)

		claimed, err := store.ClaimNextSourceAgentCommand("agent-fractional-past", "process-past")
		if err != nil {
			t.Fatal(err)
		}
		if claimed != nil {
			t.Fatalf("expired command was claimed: %#v", claimed)
		}
		persisted, err := store.GetSourceAgentCommand(command.ID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.State != SourceAgentCommandExpired {
			t.Fatalf("past command state = %q, want %q", persisted.State, SourceAgentCommandExpired)
		}
		assertSourceAgentCommandEventStates(t, mustListSourceAgentCommandEvents(t, store, command.ID),
			SourceAgentCommandQueued, SourceAgentCommandExpired)
	})

	t.Run("exact expiry is expired", func(t *testing.T) {
		store, clock := newSourceAgentCommandTestStore(t)
		registerSourceAgentCommandTestAgent(t, store, "agent-exact-expiry", "1.0.0")
		command := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-exact-expiry", "exact-expiry", time.Second)
		clock.Advance(time.Second)

		claimed, err := store.ClaimNextSourceAgentCommand("agent-exact-expiry", "process-exact")
		if err != nil {
			t.Fatal(err)
		}
		if claimed != nil {
			t.Fatalf("exactly expired command was claimed: %#v", claimed)
		}
		persisted, err := store.GetSourceAgentCommand(command.ID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.State != SourceAgentCommandExpired {
			t.Fatalf("exact command state = %q, want %q", persisted.State, SourceAgentCommandExpired)
		}
	})
}

func TestSourceAgentCommandRejectsAbsoluteDiagnosticPaths(t *testing.T) {
	for _, message := range []string{
		"/tmp/source-agent.log",
		"/Volumes/Data/file",
		`D:\Temp\file`,
		`\\server\share\file`,
		"~/file",
		"D:/Temp/file",
		"failure (/tmp/source-agent.log)",
		`failure "/Volumes/Data/file"`,
		"path=/tmp/source-agent.log",
		`path='D:\Temp\file'`,
		`share=\\server\share\file`,
		"`/tmp/source-agent.log`",
		"`D:\\Temp\\file`",
		"`~/file`",
		"failure|/Volumes/Data/file",
		"日志位于/" + "Users/alice/secret.log",
		"日志位于/用户/爱丽丝/日志",
		"/用户/爱丽丝",
		"错误 /用户/爱丽丝",
		`日志位于D:\Temp\secret.log`,
		`日志位于\\server\share\secret.log`,
		"日志位于~/secret.log",
		"file://localhost/" + "Users/alice/secret.log",
		"FILE:///tmp/secret.log",
		"访问file:///tmp/secret",
		"path=file://localhost/" + "Users/alice/secret.log",
		"(FILE:///tmp/secret)",
		"inspect (https://example.test/api/v1)/tmp/secret",
		"prefix/https://example.test/api/v1",
		`prefix\https://example.test/api/v1`,
		"检查（https://example.test/api）/tmp/secret",
		"检查【custom://域名/路径】/" + "Users/alice/secret",
		"检查《https://example.test/api》/Volumes/Data/secret",
		"检查「custom://域名/路径」/home/alice/secret",
		"检查［https://example.test/api］/private/tmp/secret",
		"GETTING /api/source-agents guidance",
		"GET /" + "Users/alice/secret.log",
		"POST /tmp/secret.log",
		"DELETE /Volumes/Data/secret.log",
		"HEAD /home/alice/secret.log",
		"OPTIONS /private/tmp/secret.log",
		"GET /api/source-agents-evil failed",
		"GET /api/other failed",
		"GET /healthcheck failed",
		"GET /health/details failed",
	} {
		if normalized, err := normalizeSourceAgentCommandMessage(message); err == nil {
			t.Errorf("accepted absolute path message %q as %q", message, normalized)
		}
	}
	for _, boundary := range []string{":", ";", ",", "[", "{", "<"} {
		message := "failure" + boundary + "/tmp/source-agent.log"
		if normalized, err := normalizeSourceAgentCommandMessage(message); err == nil {
			t.Errorf("accepted absolute path after boundary %q as %q", boundary, normalized)
		}
	}

	for _, message := range []string{
		"input/output healthy",
		"输入/output healthy",
		"输入/输出 healthy",
		"version 1/2 complete",
		"http://example.invalid/status",
		"https://example.invalid/status",
		"https://example.test/api/v1",
		"访问https://example.test/api/v1",
		"endpoint=https://example.test/api/v1",
		"inspect (https://example.test/api/v1)",
		`inspect "https://example.test/api/v1"`,
		"custom+agent://node/status",
		"see custom://域名/路径/更多",
		"ordinary diagnostic text",
	} {
		normalized, err := normalizeSourceAgentCommandMessage(message)
		if err != nil {
			t.Errorf("rejected ordinary message %q: %v", message, err)
		}
		if normalized != message {
			t.Errorf("normalized ordinary message = %q, want %q", normalized, message)
		}
	}

	for _, test := range []struct {
		name    string
		message string
	}{
		{name: "get health", message: "GET /health failed"},
		{name: "get", message: "GET /api/source-agents guidance"},
		{name: "post agent command", message: "POST /api/source-agents/agent-1/commands failed"},
		{name: "post claim", message: "POST /api/source-agent/commands/claim failed"},
		{name: "health query", message: "GET /health?verbose=1 failed"},
		{name: "agents query", message: "GET /api/source-agents?limit=1 failed"},
		{name: "post lowercase", message: "post /api/source-agents guidance"},
		{name: "put", message: "PUT /api/source-agents guidance"},
		{name: "patch mixed case", message: "PaTcH /api/source-agents guidance"},
		{name: "delete", message: "DELETE /api/source-agents guidance"},
		{name: "head", message: "HEAD /api/source-agents guidance"},
		{name: "options", message: "OPTIONS /api/source-agents guidance"},
	} {
		t.Run("allows HTTP route guidance "+test.name, func(t *testing.T) {
			normalized, err := normalizeSourceAgentCommandMessage(test.message)
			if err != nil {
				t.Fatalf("rejected HTTP route guidance %q: %v", test.message, err)
			}
			if normalized != test.message {
				t.Fatalf("normalized HTTP route guidance = %q, want %q", normalized, test.message)
			}
		})
	}
}

func TestSourceAgentCommandOperationsUseSingleNow(t *testing.T) {
	t.Run("claim by id", func(t *testing.T) {
		store, clock := newSourceAgentCommandTestStore(t)
		registerSourceAgentCommandTestAgent(t, store, "agent-linear-claim", "1.0.0")
		command := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-linear-claim", "linear-claim", time.Hour)
		expiresAt, err := time.Parse(time.RFC3339Nano, command.ExpiresAt)
		if err != nil {
			t.Fatal(err)
		}
		entryNow := expiresAt.Add(-time.Millisecond)
		sequence := &sourceAgentCommandSequenceClock{values: []time.Time{entryNow, expiresAt.Add(time.Millisecond)}}
		store.now = sequence.Now

		claimed, err := store.ClaimSourceAgentCommand(command.ID, "agent-linear-claim", "process-linear-claim")
		if err != nil {
			t.Fatal(err)
		}
		if sequence.calls != 1 {
			t.Fatalf("claim read clock %d times, want 1", sequence.calls)
		}
		want := formatSourceAgentCommandTime(entryNow)
		if claimed.State != SourceAgentCommandClaimed || claimed.ClaimedAt != want || claimed.UpdatedAt != want {
			t.Fatalf("claimed command timestamps = %#v, want %q", claimed, want)
		}
		events := mustListSourceAgentCommandEvents(t, store, command.ID)
		if got := events[len(events)-1].CreatedAt; got != want {
			t.Fatalf("claimed event time = %q, want %q", got, want)
		}
	})

	t.Run("transition", func(t *testing.T) {
		store, clock := newSourceAgentCommandTestStore(t)
		registerSourceAgentCommandTestAgent(t, store, "agent-linear-transition", "1.0.0")
		command := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-linear-transition", "linear-transition", time.Hour)
		if _, err := store.ClaimSourceAgentCommand(command.ID, "agent-linear-transition", "process-linear-transition"); err != nil {
			t.Fatal(err)
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, command.ExpiresAt)
		if err != nil {
			t.Fatal(err)
		}
		entryNow := expiresAt.Add(-time.Millisecond)
		sequence := &sourceAgentCommandSequenceClock{values: []time.Time{entryNow, expiresAt.Add(time.Millisecond)}}
		store.now = sequence.Now

		completed, err := store.TransitionSourceAgentCommand(command.ID, "agent-linear-transition", "process-linear-transition", SourceAgentCommandTransition{
			State: SourceAgentCommandSucceeded, ResultCode: SourceAgentCommandCodeDiagnosticComplete,
		})
		if err != nil {
			t.Fatal(err)
		}
		if sequence.calls != 1 {
			t.Fatalf("transition read clock %d times, want 1", sequence.calls)
		}
		want := formatSourceAgentCommandTime(entryNow)
		if completed.State != SourceAgentCommandSucceeded || completed.UpdatedAt != want || completed.CompletedAt != want {
			t.Fatalf("completed command timestamps = %#v, want %q", completed, want)
		}
		events := mustListSourceAgentCommandEvents(t, store, command.ID)
		if got := events[len(events)-1].CreatedAt; got != want {
			t.Fatalf("completed event time = %q, want %q", got, want)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		store, clock := newSourceAgentCommandTestStore(t)
		registerSourceAgentCommandTestAgent(t, store, "agent-linear-cancel", "1.0.0")
		command := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-linear-cancel", "linear-cancel", time.Hour)
		expiresAt, err := time.Parse(time.RFC3339Nano, command.ExpiresAt)
		if err != nil {
			t.Fatal(err)
		}
		entryNow := expiresAt.Add(-time.Millisecond)
		sequence := &sourceAgentCommandSequenceClock{values: []time.Time{entryNow, expiresAt.Add(time.Millisecond)}}
		store.now = sequence.Now

		canceled, err := store.CancelSourceAgentCommand(command.ID, "operator canceled")
		if err != nil {
			t.Fatal(err)
		}
		if sequence.calls != 1 {
			t.Fatalf("cancel read clock %d times, want 1", sequence.calls)
		}
		want := formatSourceAgentCommandTime(entryNow)
		if canceled.State != SourceAgentCommandCanceled || canceled.UpdatedAt != want || canceled.CompletedAt != want {
			t.Fatalf("canceled command timestamps = %#v, want %q", canceled, want)
		}
		events := mustListSourceAgentCommandEvents(t, store, command.ID)
		if got := events[len(events)-1].CreatedAt; got != want {
			t.Fatalf("canceled event time = %q, want %q", got, want)
		}
	})
}

func TestSourceAgentCommandActiveUpgradeConstraint(t *testing.T) {
	store, clock := newSourceAgentCommandTestStore(t)
	registerSourceAgentCommandTestAgent(t, store, "agent-one-upgrade", "1.0.0")

	first := mustCreateSourceAgentUpgradeCommand(t, store, clock, "agent-one-upgrade", "artifact-2", "first-upgrade")
	if _, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
		TargetAgentID:  "agent-one-upgrade",
		Type:           SourceAgentCommandUpgrade,
		IdempotencyKey: "second-upgrade",
		Payload:        json.RawMessage(`{"artifact_id":"artifact-3","expected_current_version":"1.0.0"}`),
		ExpiresAt:      clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
	}); !errors.Is(err, ErrSourceAgentCommandActiveUpgrade) {
		t.Fatalf("second active upgrade error = %v", err)
	}
	completeSourceAgentUpgradeCommand(t, store, first.ID, "agent-one-upgrade", "process-upgrade", "2.0.0")
	registerSourceAgentCommandTestAgent(t, store, "agent-one-upgrade", "2.0.0")
	if _, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
		TargetAgentID:  "agent-one-upgrade",
		Type:           SourceAgentCommandUpgrade,
		IdempotencyKey: "second-upgrade",
		Payload:        json.RawMessage(`{"artifact_id":"artifact-3","expected_current_version":"2.0.0"}`),
		ExpiresAt:      clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("new upgrade after terminal state: %v", err)
	}
}

func TestSourceAgentCommandCreateReleasesExpiredUpgrade(t *testing.T) {
	t.Run("queued upgrade releases before new create", func(t *testing.T) {
		store, clock := newSourceAgentCommandTestStore(t)
		registerSourceAgentCommandTestAgent(t, store, "agent-stale-queued", "1.0.0")
		oldInput := SourceAgentCommandCreate{
			TargetAgentID:  "agent-stale-queued",
			Type:           SourceAgentCommandUpgrade,
			IdempotencyKey: "stale-queued",
			Payload:        json.RawMessage(`{"artifact_id":"artifact-old","expected_current_version":"1.0.0"}`),
			ExpiresAt:      clock.Now().Add(time.Minute).Format(time.RFC3339Nano),
		}
		oldCommand, err := store.CreateSourceAgentCommand(oldInput)
		if err != nil {
			t.Fatal(err)
		}
		clock.Advance(2 * time.Minute)

		newCommand, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
			TargetAgentID:  "agent-stale-queued",
			Type:           SourceAgentCommandUpgrade,
			IdempotencyKey: "replacement-queued",
			Payload:        json.RawMessage(`{"artifact_id":"artifact-new","expected_current_version":"1.0.0"}`),
			ExpiresAt:      clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatalf("create replacement upgrade: %v", err)
		}
		if newCommand.ID == oldCommand.ID || newCommand.State != SourceAgentCommandQueued {
			t.Fatalf("replacement command = %#v", newCommand)
		}
		expired, err := store.GetSourceAgentCommand(oldCommand.ID)
		if err != nil {
			t.Fatal(err)
		}
		if expired.State != SourceAgentCommandExpired {
			t.Fatalf("old command state = %q, want %q", expired.State, SourceAgentCommandExpired)
		}
		assertSourceAgentCommandEventStates(t, mustListSourceAgentCommandEvents(t, store, oldCommand.ID),
			SourceAgentCommandQueued, SourceAgentCommandExpired)
	})

	t.Run("claimed upgrade expires before idempotent replay", func(t *testing.T) {
		store, clock := newSourceAgentCommandTestStore(t)
		registerSourceAgentCommandTestAgent(t, store, "agent-stale-claimed", "1.0.0")
		oldInput := SourceAgentCommandCreate{
			TargetAgentID:  "agent-stale-claimed",
			Type:           SourceAgentCommandUpgrade,
			IdempotencyKey: "stale-claimed",
			Payload:        json.RawMessage(`{"artifact_id":"artifact-old","expected_current_version":"1.0.0"}`),
			ExpiresAt:      clock.Now().Add(time.Minute).Format(time.RFC3339Nano),
		}
		oldCommand, err := store.CreateSourceAgentCommand(oldInput)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ClaimSourceAgentCommand(oldCommand.ID, "agent-stale-claimed", "process-stale"); err != nil {
			t.Fatal(err)
		}
		clock.Advance(2 * time.Minute)

		replayed, err := store.CreateSourceAgentCommand(oldInput)
		if err != nil {
			t.Fatalf("replay expired command: %v", err)
		}
		if replayed.ID != oldCommand.ID || replayed.State != SourceAgentCommandExpired {
			t.Fatalf("replayed command = %#v, want expired %q", replayed, oldCommand.ID)
		}
		replayedAgain, err := store.CreateSourceAgentCommand(oldInput)
		if err != nil {
			t.Fatal(err)
		}
		if replayedAgain.ID != oldCommand.ID || replayedAgain.State != SourceAgentCommandExpired {
			t.Fatalf("second replay = %#v", replayedAgain)
		}
		assertSourceAgentCommandEventStates(t, mustListSourceAgentCommandEvents(t, store, oldCommand.ID),
			SourceAgentCommandQueued, SourceAgentCommandClaimed, SourceAgentCommandExpired)

		if _, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
			TargetAgentID:  "agent-stale-claimed",
			Type:           SourceAgentCommandUpgrade,
			IdempotencyKey: "replacement-claimed",
			Payload:        json.RawMessage(`{"artifact_id":"artifact-new","expected_current_version":"1.0.0"}`),
			ExpiresAt:      clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatalf("create after claimed expiry: %v", err)
		}
	})

	t.Run("failed create rolls back expiry materialization", func(t *testing.T) {
		store, clock := newSourceAgentCommandTestStore(t)
		registerSourceAgentCommandTestAgent(t, store, "agent-stale-rollback", "1.0.0")
		oldCommand := mustCreateSourceAgentUpgradeCommand(t, store, clock, "agent-stale-rollback", "artifact-old", "stale-rollback")
		clock.Advance(2 * time.Hour)

		_, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
			TargetAgentID:  "agent-stale-rollback",
			Type:           SourceAgentCommandUpgrade,
			IdempotencyKey: "replacement-version-conflict",
			Payload:        json.RawMessage(`{"artifact_id":"artifact-new","expected_current_version":"0.9.0"}`),
			ExpiresAt:      clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
		})
		if !errors.Is(err, ErrSourceAgentCommandVersionConflict) {
			t.Fatalf("replacement error = %v, want version conflict", err)
		}
		persisted, err := store.GetSourceAgentCommand(oldCommand.ID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.State != SourceAgentCommandQueued {
			t.Fatalf("rolled back command state = %q, want %q", persisted.State, SourceAgentCommandQueued)
		}
		assertSourceAgentCommandEventStates(t, mustListSourceAgentCommandEvents(t, store, oldCommand.ID), SourceAgentCommandQueued)
	})
}

func TestSourceAgentCommandConcurrentClaims(t *testing.T) {
	store, clock := newSourceAgentCommandTestStore(t)
	registerSourceAgentCommandTestAgent(t, store, "agent-race-claim", "1.0.0")
	command := mustCreateSourceAgentDiagnoseCommand(t, store, clock, "agent-race-claim", "race-claim", time.Hour)

	type claimResult struct {
		command SourceAgentCommand
		err     error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var wait sync.WaitGroup
	for _, owner := range []string{"process-a", "process-b"} {
		owner := owner
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			claimed, err := store.ClaimSourceAgentCommand(command.ID, "agent-race-claim", owner)
			results <- claimResult{command: claimed, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	succeeded := 0
	for result := range results {
		if result.err == nil {
			succeeded++
			continue
		}
		if !errors.Is(result.err, ErrSourceAgentCommandClaimOwner) {
			t.Fatalf("concurrent claim error = %v", result.err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful concurrent claims = %d", succeeded)
	}
	events := mustListSourceAgentCommandEvents(t, store, command.ID)
	assertSourceAgentCommandEventStates(t, events, SourceAgentCommandQueued, SourceAgentCommandClaimed)
}

func TestSourceAgentCommandConcurrentActiveUpgrades(t *testing.T) {
	store, clock := newSourceAgentCommandTestStore(t)
	registerSourceAgentCommandTestAgent(t, store, "agent-race-upgrade", "1.0.0")

	start := make(chan struct{})
	errorsByCreate := make(chan error, 2)
	var wait sync.WaitGroup
	for _, suffix := range []string{"a", "b"} {
		suffix := suffix
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
				TargetAgentID:  "agent-race-upgrade",
				Type:           SourceAgentCommandUpgrade,
				IdempotencyKey: "race-upgrade-" + suffix,
				Payload:        json.RawMessage(`{"artifact_id":"artifact-` + suffix + `","expected_current_version":"1.0.0"}`),
				ExpiresAt:      clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
			})
			errorsByCreate <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByCreate)

	succeeded := 0
	activeErrors := 0
	for err := range errorsByCreate {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrSourceAgentCommandActiveUpgrade):
			activeErrors++
		default:
			t.Fatalf("concurrent create error = %v", err)
		}
	}
	if succeeded != 1 || activeErrors != 1 {
		t.Fatalf("concurrent creates succeeded=%d active_errors=%d", succeeded, activeErrors)
	}
}

func TestSourceAgentCommandMigrationIsDurableAndIdempotent(t *testing.T) {
	root := t.TempDir()
	store, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	defer store.Close()

	db, err := sql.Open("sqlite3", filepath.Join(root, sourceSyncDBName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, object := range []struct {
		kind string
		name string
	}{
		{kind: "table", name: "source_agent_commands"},
		{kind: "table", name: "source_agent_command_events"},
		{kind: "index", name: "idx_source_agent_commands_idempotency"},
		{kind: "index", name: "idx_source_agent_commands_one_active_upgrade"},
		{kind: "index", name: "idx_source_agent_command_events_command"},
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, object.kind, object.name).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s %s count = %d", object.kind, object.name, count)
		}
	}
}

func newSourceAgentCommandTestStore(t testing.TB) (*SourceSyncStore, *sourceSyncTestClock) {
	t.Helper()
	clock := newSourceSyncTestClock(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	store, err := newSourceSyncStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, clock
}

func registerSourceAgentCommandTestAgent(t testing.TB, store *SourceSyncStore, agentID, version string) {
	t.Helper()
	if _, err := store.HeartbeatAgent(SourceAgentHeartbeat{AgentID: agentID, Version: version}); err != nil {
		t.Fatalf("register agent %q: %v", agentID, err)
	}
}

func mustCreateSourceAgentDiagnoseCommand(t testing.TB, store *SourceSyncStore, clock *sourceSyncTestClock, agentID, key string, ttl time.Duration) SourceAgentCommand {
	t.Helper()
	command, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
		TargetAgentID: agentID, Type: SourceAgentCommandDiagnose, IdempotencyKey: key,
		ExpiresAt: clock.Now().Add(ttl).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func mustCreateSourceAgentUpgradeCommand(t testing.TB, store *SourceSyncStore, clock *sourceSyncTestClock, agentID, artifactID, key string) SourceAgentCommand {
	t.Helper()
	command, err := store.CreateSourceAgentCommand(SourceAgentCommandCreate{
		TargetAgentID: agentID, Type: SourceAgentCommandUpgrade, IdempotencyKey: key,
		Payload:   json.RawMessage(`{"artifact_id":"` + artifactID + `","expected_current_version":"1.0.0"}`),
		ExpiresAt: clock.Now().Add(time.Hour).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func completeSourceAgentUpgradeCommand(t testing.TB, store *SourceSyncStore, commandID, agentID, owner, actualVersion string) SourceAgentCommand {
	t.Helper()
	command, err := store.ClaimSourceAgentCommand(commandID, agentID, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{
		SourceAgentCommandDownloading,
		SourceAgentCommandVerified,
		SourceAgentCommandInstalling,
		SourceAgentCommandRestarting,
		SourceAgentCommandVerifying,
	} {
		command, err = store.TransitionSourceAgentCommand(command.ID, agentID, owner, SourceAgentCommandTransition{State: state})
		if err != nil {
			t.Fatalf("transition to %s: %v", state, err)
		}
	}
	command, err = store.TransitionSourceAgentCommand(command.ID, agentID, owner, SourceAgentCommandTransition{
		State: SourceAgentCommandSucceeded, ResultCode: SourceAgentCommandCodeUpgradeComplete, ActualVersion: actualVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func mustListSourceAgentCommandEvents(t testing.TB, store *SourceSyncStore, commandID string) []SourceAgentCommandEvent {
	t.Helper()
	events, err := store.ListSourceAgentCommandEvents(commandID)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func assertSourceAgentCommandEventStates(t testing.TB, events []SourceAgentCommandEvent, want ...string) {
	t.Helper()
	states := make([]string, 0, len(events))
	lastSequence := int64(0)
	for _, event := range events {
		states = append(states, event.State)
		if event.Sequence <= lastSequence {
			t.Fatalf("event sequence %d is not after %d", event.Sequence, lastSequence)
		}
		lastSequence = event.Sequence
		if len([]rune(event.Code)) > sourceAgentCommandCodeMaxRunes || len([]rune(event.Message)) > sourceAgentCommandMessageMaxRunes {
			t.Fatalf("unbounded event = %#v", event)
		}
	}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("event states = %#v, want %#v", states, want)
	}
}

func withSourceAgentCommandCreate(input SourceAgentCommandCreate, mutate func(*SourceAgentCommandCreate)) SourceAgentCommandCreate {
	mutate(&input)
	return input
}

type sourceAgentCommandSequenceClock struct {
	values []time.Time
	calls  int
}

func (c *sourceAgentCommandSequenceClock) Now() time.Time {
	index := c.calls
	c.calls++
	if index >= len(c.values) {
		return c.values[len(c.values)-1]
	}
	return c.values[index]
}
