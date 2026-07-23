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
	failed, err := FailEvidenceAudit(store, queued.AuditID, "model output failed validation", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("FailEvidenceAudit() error = %v", err)
	}
	if failed.Status != EvidenceAuditFailed || failed.FailedAt == "" || failed.FailureReason == "" || failed.OutputHash != "" {
		t.Fatalf("failed audit = %#v", failed)
	}
}

func TestEvidenceAuditStoreListOrderingAndManifestPrivacyAreStable(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	base := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	inputB := validEvidenceAuditInput()
	inputB.Subject = "PRIVATE SUBJECT B"
	first, _, err := CreateEvidenceAudit(store, inputB, "request-b", base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	inputA := validEvidenceAuditInput()
	inputA.Subject = "PRIVATE SUBJECT A"
	second, _, err := CreateEvidenceAudit(store, inputA, "request-a", base)
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
	for _, forbidden := range []string{"PRIVATE SUBJECT", inputA.Scope, "credential", "source_body", "raw_prompt"} {
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
	temporary, err := filepath.Glob(filepath.Join(store.EvidenceAuditDir(), ".*.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("atomic writes left temporary files: %v", temporary)
	}
}
