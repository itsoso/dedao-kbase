package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
		"Bearer secret-token prompt=private remote body: patient data",
		now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("FailEvidenceAudit() error = %v", err)
	}
	if failed.Status != EvidenceAuditFailed || failed.FailedAt == "" ||
		failed.FailureCode != "model_invalid" || failed.FailureSummary == "" || failed.OutputHash != "" {
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
	if len(reports) != 1 {
		t.Fatalf("report artifacts = %v, want exactly one immutable report", reports)
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
