package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	completed, err := CompleteEvidenceAudit(store, report, now.Add(2*time.Minute))
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
	if _, err := CompleteEvidenceAudit(store, report, now.Add(3*time.Minute)); !errors.Is(err, ErrEvidenceAuditImmutable) {
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

	if _, err := CompleteEvidenceAudit(store, report, now.Add(2*time.Minute)); err == nil ||
		!strings.Contains(err.Error(), "injected manifest failure") {
		t.Fatalf("first completion error = %v", err)
	}
	writeEvidenceAuditManifestFile = originalWriter
	if _, err := os.Stat(store.EvidenceAuditPreparedPath(queued.AuditID)); err != nil {
		t.Fatalf("prepared journal missing: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(store.EvidenceAuditDir(), "reports", "unrelated-corrupt.json"),
		[]byte("{broken"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	completed, err := CompleteEvidenceAudit(store, report, now.Add(5*time.Minute))
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
	if _, err := CompleteEvidenceAudit(store, report, now.Add(2*time.Minute)); err == nil {
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
	if _, err := CompleteEvidenceAudit(store, report, now.Add(3*time.Minute)); err == nil ||
		(!strings.Contains(err.Error(), "prepared") && !errors.Is(err, os.ErrNotExist)) {
		t.Fatalf("CompleteEvidenceAudit() error = %v", err)
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
