package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvidenceAuditStoreCreatesContentAddressedInputAndIsIdempotent(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	input := validEvidenceAuditInput()
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)

	created, wasCreated, err := CreateEvidenceAudit(store, input, "request-1", now)
	if err != nil {
		t.Fatalf("CreateEvidenceAudit() error = %v", err)
	}
	if !wasCreated || created.Status != EvidenceAuditQueued || created.AuditID == "" || created.InputHash == "" {
		t.Fatalf("created audit = %#v, wasCreated=%v", created, wasCreated)
	}
	if _, err := os.Stat(store.EvidenceAuditInputPath(created.InputHash)); err != nil {
		t.Fatalf("input artifact missing: %v", err)
	}
	if _, err := os.Stat(store.EvidenceAuditManifestPath()); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}

	replayed, wasCreated, err := CreateEvidenceAudit(store, input, "request-2", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("package+input idempotent replay error = %v", err)
	}
	if wasCreated || replayed.AuditID != created.AuditID || replayed.CreatedAt != created.CreatedAt {
		t.Fatalf("replayed audit = %#v, wasCreated=%v", replayed, wasCreated)
	}

	changed := input
	changed.Scope = "A different bounded scope."
	if _, _, err := CreateEvidenceAudit(store, changed, "request-1", now); !errors.Is(err, ErrEvidenceAuditIdempotencyConflict) {
		t.Fatalf("reused idempotency key error = %v", err)
	}
}

func TestEvidenceAuditStoreConcurrentCreatesShareOneAudit(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	input := validEvidenceAuditInput()
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)

	const workers = 8
	auditIDs := make(chan string, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			audit, _, err := CreateEvidenceAudit(
				store,
				input,
				fmt.Sprintf("concurrent-request-%d", index),
				now,
			)
			if err != nil {
				errs <- err
				return
			}
			auditIDs <- audit.AuditID
		}(index)
	}
	wait.Wait()
	close(auditIDs)
	close(errs)

	for err := range errs {
		t.Fatalf("CreateEvidenceAudit() error = %v", err)
	}
	var sharedAuditID string
	for auditID := range auditIDs {
		if sharedAuditID == "" {
			sharedAuditID = auditID
			continue
		}
		if auditID != sharedAuditID {
			t.Fatalf("concurrent create returned audit %q, want %q", auditID, sharedAuditID)
		}
	}
	records, err := store.ListEvidenceAudits(input.Package.PackageID, input.Package.Version, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one content-addressed audit", records)
	}
}

func TestEvidenceAuditStoreConcurrentCreatesAcrossStoreInstances(t *testing.T) {
	root := t.TempDir()
	stores := []*BookKnowledgeStore{NewBookKnowledgeStore(root), NewBookKnowledgeStore(root)}
	input := validEvidenceAuditInput()
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)

	const workers = 12
	auditIDs := make(chan string, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			audit, _, err := CreateEvidenceAudit(
				stores[index%len(stores)],
				input,
				fmt.Sprintf("cross-store-%d", index),
				now,
			)
			if err != nil {
				errs <- err
				return
			}
			auditIDs <- audit.AuditID
		}(index)
	}
	wait.Wait()
	close(auditIDs)
	close(errs)
	for err := range errs {
		t.Fatalf("CreateEvidenceAudit() error = %v", err)
	}
	var shared string
	for auditID := range auditIDs {
		if shared == "" {
			shared = auditID
		} else if auditID != shared {
			t.Fatalf("auditID = %q, want %q", auditID, shared)
		}
	}
	records, err := stores[0].ListEvidenceAudits("", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
}

func TestEvidenceAuditStoreRootLockBlocksOtherStoreInstances(t *testing.T) {
	root := t.TempDir()
	first := NewBookKnowledgeStore(root)
	second := NewBookKnowledgeStore(root)

	unlockFirst, err := first.acquireEvidenceAuditRootLock()
	if err != nil {
		t.Fatal(err)
	}
	firstReleased := false
	defer func() {
		if !firstReleased {
			unlockFirst()
		}
	}()

	acquired := make(chan func(), 1)
	errs := make(chan error, 1)
	go func() {
		unlockSecond, lockErr := second.acquireEvidenceAuditRootLock()
		if lockErr != nil {
			errs <- lockErr
			return
		}
		acquired <- unlockSecond
	}()

	select {
	case unlockSecond := <-acquired:
		unlockSecond()
		t.Fatal("second store acquired root lock while first store still held it")
	case err := <-errs:
		t.Fatalf("second store lock failed while waiting: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	unlockFirst()
	firstReleased = true
	select {
	case unlockSecond := <-acquired:
		unlockSecond()
	case err := <-errs:
		t.Fatalf("second store lock failed after release: %v", err)
	case <-time.After(time.Second):
		t.Fatal("second store did not acquire root lock after release")
	}
}

func TestEvidenceAuditStoreConcurrentCreatesAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	const workers = 6
	commands := make([]*exec.Cmd, 0, workers)
	outputs := make([]*bytes.Buffer, 0, workers)
	for index := 0; index < workers; index++ {
		command := exec.Command(os.Args[0], "-test.run=^TestEvidenceAuditStoreCrossProcessHelper$")
		command.Env = append(
			os.Environ(),
			"EVIDENCE_AUDIT_HELPER=1",
			"EVIDENCE_AUDIT_ROOT="+root,
			fmt.Sprintf("EVIDENCE_AUDIT_KEY=process-%d", index),
		)
		output := &bytes.Buffer{}
		outputs = append(outputs, output)
		command.Stdout = output
		command.Stderr = output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, command)
	}
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("helper failed: %v\n%s", err, outputs[index].String())
		}
	}
	store := NewBookKnowledgeStore(root)
	records, err := store.ListEvidenceAudits("", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one cross-process audit", records)
	}
}

func TestEvidenceAuditStoreCrossProcessHelper(t *testing.T) {
	if os.Getenv("EVIDENCE_AUDIT_HELPER") != "1" {
		t.Skip("helper process only")
	}
	store := NewBookKnowledgeStore(os.Getenv("EVIDENCE_AUDIT_ROOT"))
	if _, _, err := CreateEvidenceAudit(
		store,
		validEvidenceAuditInput(),
		os.Getenv("EVIDENCE_AUDIT_KEY"),
		time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceAuditStorePersistsLifecycleAndCompletedReportIsImmutable(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	queued, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "request-lifecycle", now)
	if err != nil {
		t.Fatal(err)
	}
	running, err := StartEvidenceAudit(store, queued.AuditID, "trace-1", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("StartEvidenceAudit() error = %v", err)
	}
	if running.Status != EvidenceAuditRunning || running.StartedAt == "" || running.TraceID != "trace-1" {
		t.Fatalf("running audit = %#v", running)
	}

	report := validCompletedEvidenceAudit()
	report.AuditID = queued.AuditID
	report.IdempotencyKey = queued.IdempotencyKey
	report.InputHash = queued.InputHash
	report.CreatedAt = queued.CreatedAt
	report.StartedAt = running.StartedAt
	completed, err := completeEvidenceAuditForTest(t, store, report, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("CompleteEvidenceAudit() error = %v", err)
	}
	if completed.Status != EvidenceAuditCompleted || completed.OutputHash == "" || completed.CompletedAt == "" {
		t.Fatalf("completed audit = %#v", completed)
	}
	reportPath := store.EvidenceAuditReportPath(completed.OutputHash)
	before, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report.Summary.Conclusion = "mutated conclusion"
	if _, err := completeEvidenceAuditForTest(t, store, report, now.Add(3*time.Minute)); !errors.Is(err, ErrEvidenceAuditImmutable) {
		t.Fatalf("overwrite completed report error = %v", err)
	}
	after, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("completed report artifact was mutated")
	}

	loaded, err := store.LoadEvidenceAudit(queued.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.OutputHash != completed.OutputHash || loaded.Summary.Conclusion != completed.Summary.Conclusion {
		t.Fatalf("loaded audit = %#v", loaded)
	}
}

func TestEvidenceAuditPreparedReportIsNotObservableAsCompletedBeforeTrace(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	queued, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "request-two-phase", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartEvidenceAudit(store, queued.AuditID, "trace-two-phase", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	report := validCompletedEvidenceAudit()
	report.AuditID = queued.AuditID
	prepared, err := PrepareEvidenceAuditCompletion(store, report, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	visible, err := store.LoadEvidenceAudit(queued.AuditID)
	if err != nil || visible.Status != EvidenceAuditRunning || visible.OutputHash != "" {
		t.Fatalf("prepared audit leaked completed state: %#v err=%v", visible, err)
	}
	if _, err := PublishEvidenceAuditCompletion(store, queued.AuditID); err == nil ||
		!strings.Contains(err.Error(), "terminal") {
		t.Fatalf("publish without trace error = %v", err)
	}
	completed, err := completeEvidenceAuditForTest(t, store, *prepared, now.Add(3*time.Minute))
	if err != nil || completed.Status != EvidenceAuditCompleted {
		t.Fatalf("two-phase completion = %#v err=%v", completed, err)
	}
}

func TestEvidenceAuditStorePersistsFailedStateWithoutPartialReport(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	queued, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "request-failed", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartEvidenceAudit(store, queued.AuditID, "trace-failed", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	failed, err := FailEvidenceAudit(
		store,
		queued.AuditID,
		"model_invalid",
		"Model route rejected the request.",
		now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("FailEvidenceAudit() error = %v", err)
	}
	if failed.Status != EvidenceAuditFailed || failed.FailedAt == "" ||
		failed.FailureCode != "model_invalid" || failed.FailureSummary != "Model route rejected the request." ||
		failed.OutputHash != "" {
		t.Fatalf("failed audit = %#v", failed)
	}
	rawManifest, err := os.ReadFile(store.EvidenceAuditManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-token", "prompt=private", "patient data", "Bearer"} {
		if strings.Contains(string(rawManifest), forbidden) {
			t.Fatalf("manifest leaked failure detail %q: %s", forbidden, rawManifest)
		}
	}
}

func TestEvidenceAuditStoreRedactsSensitiveFailureSummary(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	queued, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "request-redaction", now)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := FailEvidenceAudit(
		store,
		queued.AuditID,
		"upstream_error",
		"Bearer secret-token prompt=private remote body: patient data",
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if failed.FailureSummary != "Audit failed; sensitive upstream detail was redacted." {
		t.Fatalf("failure summary = %q", failed.FailureSummary)
	}
	raw, err := os.ReadFile(store.EvidenceAuditManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-token", "prompt=private", "patient data", "Bearer"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("manifest leaked %q: %s", forbidden, raw)
		}
	}
}

func TestEvidenceAuditStoreRejectsLegacyAndUnknownManifestVersions(t *testing.T) {
	for _, version := range []string{"1", "999"} {
		t.Run(version, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			if err := os.MkdirAll(store.EvidenceAuditDir(), 0o755); err != nil {
				t.Fatal(err)
			}
			payload := []byte(`{"version":"` + version + `","audits":[],"idempotency":[]}`)
			if err := os.WriteFile(store.EvidenceAuditManifestPath(), payload, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ListEvidenceAudits("", "", 10); err == nil ||
				!strings.Contains(err.Error(), "manifest version") {
				t.Fatalf("ListEvidenceAudits() error = %v", err)
			}
		})
	}
}

func TestEvidenceAuditStoreEnforcesManifestCapacity(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	auditOverflow := EvidenceAuditManifest{
		Version:     evidenceAuditStoreVersion,
		Audits:      make([]EvidenceAuditRecord, evidenceAuditMaxManifestAudits+1),
		Idempotency: []EvidenceAuditIdempotencyRecord{},
	}
	if err := store.writeEvidenceAuditManifestUnlocked(&auditOverflow); err == nil ||
		!strings.Contains(err.Error(), "capacity") {
		t.Fatalf("writeEvidenceAuditManifestUnlocked() error = %v", err)
	}
	idempotencyOverflow := EvidenceAuditManifest{
		Version:     evidenceAuditStoreVersion,
		Audits:      []EvidenceAuditRecord{},
		Idempotency: make([]EvidenceAuditIdempotencyRecord, evidenceAuditMaxManifestIdempotency+1),
	}
	if err := store.writeEvidenceAuditManifestUnlocked(&idempotencyOverflow); err == nil ||
		!strings.Contains(err.Error(), "capacity") {
		t.Fatalf("writeEvidenceAuditManifestUnlocked() idempotency error = %v", err)
	}
}

func TestEvidenceAuditStoreRejectsAtCapacityBeforeWritingInput(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := os.MkdirAll(store.EvidenceAuditDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := EvidenceAuditManifest{
		Version:     evidenceAuditStoreVersion,
		Audits:      make([]EvidenceAuditRecord, evidenceAuditMaxManifestAudits),
		Idempotency: []EvidenceAuditIdempotencyRecord{},
	}
	for index := range manifest.Audits {
		manifest.Audits[index] = EvidenceAuditRecord{
			AuditID:   fmt.Sprintf("audit-capacity-%d", index),
			PackageID: "another-package",
			InputHash: fmt.Sprintf("sha256:%064x", index),
			CreatedAt: "2026-07-23T10:00:00Z",
			UpdatedAt: "2026-07-23T10:00:00Z",
		}
	}
	if err := store.writeEvidenceAuditManifestUnlocked(&manifest); err != nil {
		t.Fatal(err)
	}
	input := validEvidenceAuditInput()
	inputHash, err := EvidenceAuditInputHash(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateEvidenceAudit(
		store,
		input,
		"capacity-request",
		time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
	); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("CreateEvidenceAudit() error = %v", err)
	}
	if _, err := os.Stat(store.EvidenceAuditInputPath(inputHash)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capacity rejection wrote input artifact: %v", err)
	}
}

func TestEvidenceAuditStoreRejectsOutOfOrderTransitionsWithoutPersistingThem(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	queued, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "request-timeline", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartEvidenceAudit(store, queued.AuditID, "trace-too-early", now.Add(-time.Minute)); err == nil {
		t.Fatal("StartEvidenceAudit() accepted started_at before created_at")
	}
	reloaded, err := store.LoadEvidenceAudit(queued.AuditID)
	if err != nil {
		t.Fatalf("LoadEvidenceAudit() after rejected transition = %v", err)
	}
	if reloaded.Status != EvidenceAuditQueued || reloaded.StartedAt != "" {
		t.Fatalf("rejected transition persisted: %#v", reloaded)
	}
}

func TestEvidenceAuditStoreRecoversWhenManifestWriteFailsAfterReportWrite(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	queued, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "request-recovery", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartEvidenceAudit(store, queued.AuditID, "trace-recovery", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	report := validCompletedEvidenceAudit()
	report.AuditID = queued.AuditID

	originalWriter := writeEvidenceAuditManifestFile
	failedOnce := false
	writeEvidenceAuditManifestFile = func(path string, payload []byte) error {
		if !failedOnce {
			failedOnce = true
			return errors.New("injected manifest failure")
		}
		return originalWriter(path, payload)
	}
	t.Cleanup(func() { writeEvidenceAuditManifestFile = originalWriter })

	if _, err := completeEvidenceAuditForTest(t, store, report, now.Add(2*time.Minute)); err == nil ||
		!strings.Contains(err.Error(), "injected manifest failure") {
		t.Fatalf("first completion error = %v", err)
	}
	writeEvidenceAuditManifestFile = originalWriter
	if _, err := os.Stat(store.EvidenceAuditPreparedPath(queued.AuditID)); err != nil {
		t.Fatalf("prepared journal missing: %v", err)
	}
	if runtime.GOOS != "windows" {
		var prepared evidenceAuditPreparedRecord
		if err := readJSONFile(store.EvidenceAuditPreparedPath(queued.AuditID), &prepared); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{
			store.EvidenceAuditManifestPath(),
			store.EvidenceAuditInputPath(queued.InputHash),
			store.EvidenceAuditReportPath(prepared.OutputHash),
			store.EvidenceAuditPreparedPath(queued.AuditID),
		} {
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("private audit state file %s mode=%#o", path, info.Mode().Perm())
			}
		}
		for _, path := range []string{
			store.EvidenceAuditDir(),
			filepath.Dir(store.EvidenceAuditInputPath(queued.InputHash)),
			filepath.Dir(store.EvidenceAuditReportPath(prepared.OutputHash)),
			filepath.Dir(store.EvidenceAuditPreparedPath(queued.AuditID)),
		} {
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("private audit state directory %s mode=%#o", path, info.Mode().Perm())
			}
		}
	}
	if err := os.WriteFile(
		filepath.Join(store.EvidenceAuditDir(), "reports", "unrelated-corrupt.json"),
		[]byte("{broken"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	completed, err := completeEvidenceAuditForTest(t, store, report, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("recovered completion error = %v", err)
	}
	if completed.Status != EvidenceAuditCompleted {
		t.Fatalf("recovered audit = %#v", completed)
	}
	reports, err := filepath.Glob(filepath.Join(store.EvidenceAuditDir(), "reports", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 {
		t.Fatalf("report artifacts = %v, want report plus injected corrupt artifact", reports)
	}
	if _, err := os.Stat(store.EvidenceAuditPreparedPath(queued.AuditID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prepared journal still exists: %v", err)
	}
}

func TestEvidenceAuditStoreRejectsPreparedJournalWithMismatchedReportIdentity(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	queued, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "request-journal", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartEvidenceAudit(store, queued.AuditID, "trace-journal", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	report := validCompletedEvidenceAudit()
	report.AuditID = queued.AuditID

	originalWriter := writeEvidenceAuditManifestFile
	writeEvidenceAuditManifestFile = func(string, []byte) error {
		return errors.New("injected manifest failure")
	}
	t.Cleanup(func() { writeEvidenceAuditManifestFile = originalWriter })
	if _, err := completeEvidenceAuditForTest(t, store, report, now.Add(2*time.Minute)); err == nil {
		t.Fatal("completion unexpectedly succeeded")
	}
	writeEvidenceAuditManifestFile = originalWriter

	var prepared evidenceAuditPreparedRecord
	if err := readJSONFile(store.EvidenceAuditPreparedPath(queued.AuditID), &prepared); err != nil {
		t.Fatal(err)
	}
	prepared.OutputHash = testSecondSupportHash
	payload, err := encodeJSONFile(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.EvidenceAuditPreparedPath(queued.AuditID), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := completeEvidenceAuditForTest(t, store, report, now.Add(3*time.Minute)); err == nil ||
		(!strings.Contains(err.Error(), "prepared") && !errors.Is(err, os.ErrNotExist)) {
		t.Fatalf("CompleteEvidenceAudit() error = %v", err)
	}
}

func TestEvidenceAuditImmutableWriteNeverPublishesPartialFinal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports", "report.json")
	payload := []byte("{\"complete\":true}\n")
	originalFault := evidenceAuditStorageFault
	evidenceAuditStorageFault = func(stage, _ string) error {
		if stage == evidenceAuditFaultImmutableTempSynced {
			return errors.New("injected crash after immutable temp sync")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditStorageFault = originalFault })

	if err := writeEvidenceAuditImmutableFile(path, payload); err == nil ||
		!strings.Contains(err.Error(), "injected crash") {
		t.Fatalf("writeEvidenceAuditImmutableFile() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial final artifact was published: %v", err)
	}

	evidenceAuditStorageFault = originalFault
	if err := writeEvidenceAuditImmutableFile(path, payload); err != nil {
		t.Fatalf("retry immutable write: %v", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, payload) {
		t.Fatalf("stored payload = %q, want %q", stored, payload)
	}
}

func TestEvidenceAuditManifestRecoversLastKnownGoodAtEveryPublishStage(t *testing.T) {
	stages := []string{
		evidenceAuditFaultManifestTempSynced,
		evidenceAuditFaultManifestBackupPublished,
		evidenceAuditFaultManifestBeforePublish,
	}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
			firstInput := validEvidenceAuditInput()
			first, _, err := CreateEvidenceAudit(store, firstInput, "manifest-first", now)
			if err != nil {
				t.Fatal(err)
			}

			originalFault := evidenceAuditStorageFault
			evidenceAuditStorageFault = func(current, _ string) error {
				if current == stage {
					return errors.New("injected manifest publish crash")
				}
				return nil
			}
			t.Cleanup(func() { evidenceAuditStorageFault = originalFault })

			secondInput := validEvidenceAuditInput()
			secondInput.Subject = "A distinct second audit"
			if _, _, err := CreateEvidenceAudit(
				store,
				secondInput,
				"manifest-second",
				now.Add(time.Minute),
			); err == nil || !strings.Contains(err.Error(), "injected manifest publish crash") {
				t.Fatalf("CreateEvidenceAudit() error = %v", err)
			}
			evidenceAuditStorageFault = originalFault

			reopened := NewBookKnowledgeStore(store.root)
			records, err := reopened.ListEvidenceAudits("", "", 10)
			if err != nil {
				t.Fatalf("recover manifest: %v", err)
			}
			if len(records) != 1 || records[0].AuditID != first.AuditID {
				t.Fatalf("recovered records = %#v, want only last-known-good audit %q", records, first.AuditID)
			}
			assertEvidenceAuditManifestFileValid(t, reopened.EvidenceAuditManifestPath())
			if _, err := os.Stat(reopened.EvidenceAuditManifestPath() + ".bak"); err == nil {
				assertEvidenceAuditManifestFileValid(t, reopened.EvidenceAuditManifestPath()+".bak")
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		})
	}
}

func completeEvidenceAuditForTest(
	t *testing.T,
	store *BookKnowledgeStore,
	report EvidenceAudit,
	now time.Time,
) (*EvidenceAudit, error) {
	t.Helper()
	if existing, err := store.LoadEvidenceAudit(report.AuditID); err == nil &&
		existing.Status == EvidenceAuditCompleted {
		if !evidenceAuditReportContentEqual(*existing, report) {
			return nil, ErrEvidenceAuditImmutable
		}
		return existing, nil
	}
	prepared, err := PrepareEvidenceAuditCompletion(store, report, now)
	if err != nil {
		return nil, err
	}
	retrieved := make([]evidenceAuditRetrievedItem, 0)
	for _, claim := range prepared.ClaimAudits {
		for _, ref := range claim.Evidence {
			retrieved = append(retrieved, evidenceAuditRetrievedItem{
				Evidence: AgentPackageEvidence{
					ReleaseID: ref.ReleaseID, ClaimID: ref.ClaimID,
					Statement:   "bounded test evidence",
					CitationIDs: []string{ref.CitationID}, Score: 1,
				},
				Ref: ref,
			})
		}
	}
	fingerprint, err := evidenceAuditReportFingerprint(*prepared)
	if err != nil {
		return nil, err
	}
	trace, err := buildEvidenceAuditTrace(
		store, *prepared, AgentPackage{}, retrieved, prepared.ClaimAudits,
		prepared.TraceID, AgentTraceOutcomeCompleted, fingerprint,
		EvidenceAuditRunnerConfig{Now: func() time.Time { return now }},
	)
	if err != nil {
		return nil, err
	}
	terminal := evidenceAuditTraceTerminal{
		Version: evidenceAuditTraceTerminalVersion,
		AuditID: prepared.AuditID, InputHash: prepared.InputHash,
		TraceID: prepared.TraceID, ReportFingerprint: fingerprint,
		Report: prepared, Trace: trace,
	}
	if err := store.prepareEvidenceAuditTraceTerminal(terminal); err != nil {
		return nil, err
	}
	if err := store.finalizeEvidenceAuditTraceTerminal(terminal); err != nil {
		return nil, err
	}
	completed, err := PublishEvidenceAuditCompletion(store, prepared.AuditID)
	if err != nil {
		return nil, err
	}
	if err := store.removeEvidenceAuditTraceTerminal(prepared.AuditID); err != nil {
		return nil, err
	}
	return completed, nil
}

func TestEvidenceAuditManifestFallsBackFromCorruptPrimaryToValidBackup(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	first, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "backup-first", now)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := validEvidenceAuditInput()
	secondInput.Subject = "Second manifest generation"
	if _, _, err := CreateEvidenceAudit(store, secondInput, "backup-second", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.EvidenceAuditManifestPath(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	records, err := NewBookKnowledgeStore(store.root).ListEvidenceAudits("", "", 10)
	if err != nil {
		t.Fatalf("load backup manifest: %v", err)
	}
	if len(records) != 1 || records[0].AuditID != first.AuditID {
		t.Fatalf("backup records = %#v, want last-known-good audit %q", records, first.AuditID)
	}
	assertEvidenceAuditManifestFileValid(t, store.EvidenceAuditManifestPath())
	assertEvidenceAuditManifestFileValid(t, store.EvidenceAuditManifestPath()+".bak")
}

func assertEvidenceAuditManifestFileValid(t *testing.T, path string) {
	t.Helper()
	var manifest EvidenceAuditManifest
	if err := readJSONFile(path, &manifest); err != nil {
		t.Fatalf("read manifest %s: %v", path, err)
	}
	if manifest.Version != evidenceAuditStoreVersion {
		t.Fatalf("manifest %s version = %q", path, manifest.Version)
	}
	if err := validateEvidenceAuditManifestCapacity(&manifest); err != nil {
		t.Fatalf("validate manifest %s: %v", path, err)
	}
}

func TestEvidenceAuditStoreListOrderingAndManifestPrivacyAreStable(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	base := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	inputB := validEvidenceAuditInput()
	inputB.Subject = "PRIVATE SUBJECT B"
	rawKeyB := "request-b Bearer private-token prompt=secret"
	first, _, err := CreateEvidenceAudit(store, inputB, rawKeyB, base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	inputA := validEvidenceAuditInput()
	inputA.Subject = "PRIVATE SUBJECT A"
	rawKeyA := "request-a remote-response private"
	second, _, err := CreateEvidenceAudit(store, inputA, rawKeyA, base)
	if err != nil {
		t.Fatal(err)
	}

	records, err := store.ListEvidenceAudits("", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].AuditID != second.AuditID || records[1].AuditID != first.AuditID {
		t.Fatalf("stable records = %#v", records)
	}
	filtered, err := store.ListEvidenceAudits(inputA.Package.PackageID, inputA.Package.Version, 10)
	if err != nil || len(filtered) != 2 {
		t.Fatalf("filtered records = %#v, err=%v", filtered, err)
	}

	rawManifest, err := os.ReadFile(store.EvidenceAuditManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"PRIVATE SUBJECT", inputA.Scope, "credential", "source_body", "raw_prompt",
		rawKeyA, rawKeyB, "private-token", "prompt=secret", "remote-response",
	} {
		if strings.Contains(string(rawManifest), forbidden) {
			t.Fatalf("manifest persisted forbidden content %q: %s", forbidden, rawManifest)
		}
	}
	var manifest EvidenceAuditManifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Audits) != 2 || len(manifest.Idempotency) != 2 {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
	for _, record := range manifest.Idempotency {
		if !strings.HasPrefix(record.IdempotencyIdentity, "sha256:") || len(record.IdempotencyIdentity) != 71 {
			t.Fatalf("idempotency identity = %q", record.IdempotencyIdentity)
		}
	}
	temporary, err := filepath.Glob(filepath.Join(store.EvidenceAuditDir(), ".*.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("atomic writes left temporary files: %v", temporary)
	}
}

func TestManualRetryEvidenceAuditCreatesImmutableIdempotentAttempt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewBookKnowledgeStore(t.TempDir())
	input := validEvidenceAuditInput()
	original, _, err := CreateEvidenceAudit(store, input, "original-request", testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartEvidenceAudit(store, original.AuditID, "trace-original", testAgentPackageTime().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := FailEvidenceAudit(
		store,
		original.AuditID,
		"model_outcome_unknown",
		"requires manual retry",
		testAgentPackageTime().Add(2*time.Minute),
	); err != nil {
		t.Fatal(err)
	}

	retry, created, err := ManualRetryEvidenceAudit(
		store,
		original.AuditID,
		validEvidenceAuditRetryAuthorization(original.AuditID, testAgentPackageTime()),
		acceptEvidenceAuditRetryAuthorization,
		"manual-retry-request",
		testAgentPackageTime().Add(3*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created || retry.AuditID == original.AuditID || retry.RetryOf != original.AuditID ||
		retry.Attempt != 2 || retry.RequestIdentity == "" ||
		strings.Contains(retry.RequestIdentity, "human-authorization-token") {
		t.Fatalf("manual retry = %#v created=%v", retry, created)
	}
	repeated, created, err := ManualRetryEvidenceAudit(
		store,
		original.AuditID,
		validEvidenceAuditRetryAuthorization(original.AuditID, testAgentPackageTime()),
		acceptEvidenceAuditRetryAuthorization,
		"manual-retry-request",
		testAgentPackageTime().Add(4*time.Minute),
	)
	if err != nil || created || repeated.AuditID != retry.AuditID {
		t.Fatalf("repeated retry = %#v created=%v err=%v", repeated, created, err)
	}
	ordinary, created, err := CreateEvidenceAudit(
		store, input, "ordinary-create-cannot-bypass", testAgentPackageTime().Add(5*time.Minute),
	)
	if err != nil || created || ordinary.AuditID != original.AuditID {
		t.Fatalf("ordinary create bypassed failed audit: %#v created=%v err=%v", ordinary, created, err)
	}
	loadedOriginal, err := store.LoadEvidenceAudit(original.AuditID)
	if err != nil || loadedOriginal.Status != EvidenceAuditFailed || loadedOriginal.RetryOf != "" {
		t.Fatalf("original audit mutated: %#v err=%v", loadedOriginal, err)
	}
}

func TestManualRetryEvidenceAuditRejectsUnauthorizedOrIneligibleFailures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewBookKnowledgeStore(t.TempDir())
	input := validEvidenceAuditInput()
	audit, _, err := CreateEvidenceAudit(store, input, "not-retryable", testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FailEvidenceAudit(
		store, audit.AuditID, "invalid_model_output", "not manually retryable", testAgentPackageTime().Add(time.Minute),
	); err != nil {
		t.Fatal(err)
	}
	for _, authorization := range []EvidenceAuditRetryAuthorization{
		{},
		validEvidenceAuditRetryAuthorization(audit.AuditID, testAgentPackageTime()),
	} {
		if _, _, err := ManualRetryEvidenceAudit(
			store, audit.AuditID, authorization, acceptEvidenceAuditRetryAuthorization,
			"manual-retry-invalid", testAgentPackageTime().Add(2*time.Minute),
		); err == nil {
			t.Fatalf("ManualRetryEvidenceAudit() accepted authorization=%#v", authorization)
		}
	}
}

func TestManualRetryEvidenceAuditValidatesAuthorizationAndAllowsOnlyOneActiveAttempt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewBookKnowledgeStore(t.TempDir())
	input := validEvidenceAuditInput()
	original, _, err := CreateEvidenceAudit(store, input, "retry-auth-source", testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FailEvidenceAudit(
		store, original.AuditID, "model_outcome_unknown", "unknown",
		testAgentPackageTime().Add(time.Minute),
	); err != nil {
		t.Fatal(err)
	}
	now := testAgentPackageTime().Add(2 * time.Minute)
	tests := []struct {
		name   string
		mutate func(*EvidenceAuditRetryAuthorization)
	}{
		{"unverified", func(value *EvidenceAuditRetryAuthorization) { value.Verified = false }},
		{"expired", func(value *EvidenceAuditRetryAuthorization) { value.ExpiresAt = now.Add(-time.Second) }},
		{"wrong audit", func(value *EvidenceAuditRetryAuthorization) { value.AuditID = "other-audit" }},
		{"wrong scope", func(value *EvidenceAuditRetryAuthorization) { value.Scope = "evidence-audit:read" }},
		{"missing signature", func(value *EvidenceAuditRetryAuthorization) { value.Signature = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorization := validEvidenceAuditRetryAuthorization(original.AuditID, now)
			tt.mutate(&authorization)
			if _, _, err := ManualRetryEvidenceAudit(
				store, original.AuditID, authorization, acceptEvidenceAuditRetryAuthorization,
				"invalid-"+tt.name, now,
			); err == nil {
				t.Fatal("invalid retry authorization unexpectedly succeeded")
			}
		})
	}

	type result struct {
		audit   *EvidenceAudit
		created bool
		err     error
	}
	results := make(chan result, 2)
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			authorization := validEvidenceAuditRetryAuthorization(original.AuditID, now)
			authorization.Nonce = fmt.Sprintf("nonce-%d", index)
			authorization.Signature = fmt.Sprintf("signature-%d", index)
			audit, created, retryErr := ManualRetryEvidenceAudit(
				store, original.AuditID, authorization, acceptEvidenceAuditRetryAuthorization,
				fmt.Sprintf("parallel-%d", index), now,
			)
			results <- result{audit: audit, created: created, err: retryErr}
		}()
	}
	successes, conflicts := 0, 0
	for index := 0; index < 2; index++ {
		value := <-results
		if value.err == nil && value.created {
			successes++
			continue
		}
		if errors.Is(value.err, ErrEvidenceAuditStateConflict) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected parallel retry result: %#v", value)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("parallel retries successes=%d conflicts=%d", successes, conflicts)
	}
}

func validEvidenceAuditRetryAuthorization(
	auditID string,
	now time.Time,
) EvidenceAuditRetryAuthorization {
	return EvidenceAuditRetryAuthorization{
		AuditID: auditID, Actor: "reviewer-1", Issuer: "kbase-test",
		Scope: EvidenceAuditRetryScope, ExpiresAt: now.Add(time.Hour),
		Nonce: "nonce-1", Signature: "verified-test-signature", Verified: true,
	}
}

func acceptEvidenceAuditRetryAuthorization(
	authorization EvidenceAuditRetryAuthorization,
	now time.Time,
) error {
	if !authorization.Verified || !authorization.ExpiresAt.After(now) {
		return errors.New("authorization rejected")
	}
	return nil
}

func TestListEvidenceAuditsReconcilesPreparedTerminalState(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(
		t, store, pkg, "list-terminal-reconcile", "Evidence comparison only.",
	)
	originalWriter := writeEvidenceAuditManifestFile
	failedOnce := false
	writeEvidenceAuditManifestFile = func(path string, payload []byte) error {
		if !failedOnce && strings.Contains(string(payload), `"status":"failed"`) {
			failedOnce = true
			return errors.New("synthetic terminal manifest failure")
		}
		return originalWriter(path, payload)
	}
	t.Cleanup(func() { writeEvidenceAuditManifestFile = originalWriter })
	client := &evidenceAuditFakeClient{answers: []string{`not-json`}}
	if _, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig(),
	); err == nil {
		t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
	}
	writeEvidenceAuditManifestFile = originalWriter
	records, err := store.ListEvidenceAudits(pkg.PackageID, pkg.Version, 10)
	if err != nil {
		t.Fatal(err)
	}
	var reconciled *EvidenceAuditRecord
	for index := range records {
		if records[index].AuditID == audit.AuditID {
			reconciled = &records[index]
			break
		}
	}
	if reconciled == nil || reconciled.Status != EvidenceAuditFailed ||
		reconciled.FailureCode != "invalid_model_output" {
		t.Fatalf("reconciled list = %#v", records)
	}
	detail, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != reconciled.Status || detail.FailureCode != reconciled.FailureCode {
		t.Fatalf("list/detail terminal mismatch: record=%#v detail=%#v", reconciled, detail)
	}
}

func TestEvidenceAuditLeaseClaimRenewReleaseAndExpiryTakeover(t *testing.T) {
	storeA := NewBookKnowledgeStore(t.TempDir())
	storeB := NewBookKnowledgeStore(storeA.Root())
	now := testAgentPackageTime()
	audit, _, err := CreateEvidenceAudit(storeA, validEvidenceAuditInput(), "lease-lifecycle", now)
	if err != nil {
		t.Fatalf("CreateEvidenceAudit() error = %v", err)
	}

	claimed, err := storeA.ClaimEvidenceAuditLease(audit.AuditID, "owner-a", now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimEvidenceAuditLease(owner-a) error = %v", err)
	}
	if !claimed.Claimed || claimed.Record.LeaseOwner != "owner-a" || claimed.Record.LeaseAttempt != 1 {
		t.Fatalf("first lease = %+v", claimed)
	}

	blocked, err := storeB.ClaimEvidenceAuditLease(audit.AuditID, "owner-b", now.Add(30*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("ClaimEvidenceAuditLease(owner-b active) error = %v", err)
	}
	if blocked.Claimed || blocked.Record.LeaseOwner != "owner-a" {
		t.Fatalf("active lease takeover = %+v, want blocked by owner-a", blocked)
	}

	renewed, err := storeA.RenewEvidenceAuditLease(audit.AuditID, "owner-a", now.Add(40*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("RenewEvidenceAuditLease() error = %v", err)
	}
	if renewed.LeaseExpiresAt != now.Add(100*time.Second).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("renewed expiry = %q", renewed.LeaseExpiresAt)
	}

	taken, err := storeB.ClaimEvidenceAuditLease(audit.AuditID, "owner-b", now.Add(101*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("ClaimEvidenceAuditLease(owner-b expired) error = %v", err)
	}
	if !taken.Claimed || taken.Record.LeaseOwner != "owner-b" || taken.Record.LeaseAttempt != 2 {
		t.Fatalf("expired lease takeover = %+v", taken)
	}

	if err := storeA.ReleaseEvidenceAuditLease(audit.AuditID, "owner-a", now.Add(102*time.Second)); !errors.Is(err, ErrEvidenceAuditLeaseLost) {
		t.Fatalf("stale ReleaseEvidenceAuditLease() error = %v, want lease lost", err)
	}
	if err := storeB.ReleaseEvidenceAuditLease(audit.AuditID, "owner-b", now.Add(103*time.Second)); err != nil {
		t.Fatalf("ReleaseEvidenceAuditLease(owner-b) error = %v", err)
	}
}

func TestEvidenceAuditRecoveryPageIsBoundedAndCursorBased(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := testAgentPackageTime()
	for index := 0; index < 7; index++ {
		input := validEvidenceAuditInput()
		input.Subject = fmt.Sprintf("recovery-page-%d", index)
		if _, _, err := CreateEvidenceAudit(store, input, fmt.Sprintf("recovery-page-%d", index), now); err != nil {
			t.Fatalf("CreateEvidenceAudit(%d) error = %v", index, err)
		}
	}

	first, err := store.ListRecoverableEvidenceAuditsPage("", 3, now)
	if err != nil {
		t.Fatalf("first page error = %v", err)
	}
	if len(first.Records) != 3 || first.NextCursor == "" || first.Scanned != 3 {
		t.Fatalf("first page = %+v", first)
	}
	second, err := store.ListRecoverableEvidenceAuditsPage(first.NextCursor, 3, now)
	if err != nil {
		t.Fatalf("second page error = %v", err)
	}
	if len(second.Records) != 3 || second.NextCursor == "" || second.Scanned != 3 {
		t.Fatalf("second page = %+v", second)
	}
	third, err := store.ListRecoverableEvidenceAuditsPage(second.NextCursor, 3, now)
	if err != nil {
		t.Fatalf("third page error = %v", err)
	}
	if len(third.Records) != 1 || third.NextCursor != "" || third.Scanned != 1 {
		t.Fatalf("third page = %+v", third)
	}
}

func TestEvidenceAuditLeaseMetadataIsBoundedAndHiddenFromPublicList(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	now := testAgentPackageTime()
	audit, _, err := CreateEvidenceAudit(store, validEvidenceAuditInput(), "lease-public-boundary", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimEvidenceAuditLease(
		audit.AuditID, strings.Repeat("x", 129), now, time.Minute,
	); err == nil {
		t.Fatal("oversized lease owner was accepted")
	}
	if _, err := store.ClaimEvidenceAuditLease(audit.AuditID, "private-owner", now, time.Minute); err != nil {
		t.Fatal(err)
	}
	records, err := store.ListEvidenceAudits("", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].LeaseOwner != "" || records[0].LeaseExpiresAt != "" {
		t.Fatalf("public audit list exposed lease metadata: %+v", records)
	}
	internal, err := store.LoadEvidenceAuditRecord(audit.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if internal.LeaseOwner != "private-owner" || internal.LeaseExpiresAt == "" {
		t.Fatalf("persisted lease metadata missing: %+v", internal)
	}
}
