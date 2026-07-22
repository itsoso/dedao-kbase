package app

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClinicalTrialAuditStoreCreatesAndReplaysIdempotently(t *testing.T) {
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	store, err := newClinicalTrialAuditStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	created, err := store.CreateRun("book-agent-clinical-trials-truth", "1.2.0", ClinicalTrialAuditRequest{
		InputType: ClinicalTrialInputNCTID,
		Input:     " nct 01234567 ",
	}, "request-1")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if created.State != ClinicalTrialAuditRunQueued || created.Request.NormalizedInput != "NCT01234567" || created.Request.InputHash == "" {
		t.Fatalf("created run = %#v", created)
	}
	if created.IdempotencyKey == "request-1" || !strings.HasPrefix(created.IdempotencyKey, "sha256:") {
		t.Fatalf("idempotency key was not redacted: %q", created.IdempotencyKey)
	}
	if created.CreatedAt != clock.Now().Format(time.RFC3339Nano) || created.UpdatedAt != created.CreatedAt {
		t.Fatalf("timestamps = %q / %q", created.CreatedAt, created.UpdatedAt)
	}
	if store.DBPath() != filepath.Join(store.Root(), clinicalTrialAuditDBName) {
		t.Fatalf("db path = %q", store.DBPath())
	}

	replayed, err := store.CreateRun("book-agent-clinical-trials-truth", "1.2.0", ClinicalTrialAuditRequest{
		InputType: ClinicalTrialInputNCTID,
		Input:     "NCT01234567",
	}, "request-1")
	if err != nil || replayed.RunID != created.RunID {
		t.Fatalf("replay = %#v, err=%v", replayed, err)
	}

	_, err = store.CreateRun("book-agent-clinical-trials-truth", "1.2.1", ClinicalTrialAuditRequest{
		InputType: ClinicalTrialInputNCTID,
		Input:     "NCT01234567",
	}, "request-1")
	if !errors.Is(err, ErrClinicalTrialAuditIdempotencyConflict) {
		t.Fatalf("version conflict = %v", err)
	}
	_, err = store.CreateRun("book-agent-clinical-trials-truth", "1.2.0", ClinicalTrialAuditRequest{
		InputType: ClinicalTrialInputNCTID,
		Input:     "NCT76543210",
	}, "request-1")
	if !errors.Is(err, ErrClinicalTrialAuditIdempotencyConflict) {
		t.Fatalf("input conflict = %v", err)
	}
}

func TestClinicalTrialAuditStoreCreatesSameIdempotencyKeyConcurrently(t *testing.T) {
	root := t.TempDir()
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 12, 30, 0, 0, time.UTC))
	first, err := newClinicalTrialAuditStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := newClinicalTrialAuditStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	type result struct {
		run ClinicalTrialAuditStoredRun
		err error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, store := range []*ClinicalTrialAuditStore{first, second} {
		wg.Add(1)
		go func(store *ClinicalTrialAuditStore) {
			defer wg.Done()
			<-start
			run, createErr := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000008"}, "concurrent-key")
			results <- result{run: run, err: createErr}
		}(store)
	}
	close(start)
	wg.Wait()
	close(results)
	var runID string
	for item := range results {
		if item.err != nil {
			t.Fatalf("concurrent create: %v", item.err)
		}
		if runID == "" {
			runID = item.run.RunID
		} else if item.run.RunID != runID {
			t.Fatalf("concurrent run IDs differ: %q / %q", runID, item.run.RunID)
		}
	}
	if _, err := second.CreateRun("package-a", "2.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000008"}, "concurrent-key"); !errors.Is(err, ErrClinicalTrialAuditIdempotencyConflict) {
		t.Fatalf("concurrent identity conflict = %v", err)
	}
}

func TestClinicalTrialAuditStoreMigratesLegacyDatabaseConcurrently(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, clinicalTrialAuditDBName)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE clinical_trial_audit_runs (
		run_id TEXT PRIMARY KEY, package_id TEXT NOT NULL, package_version TEXT NOT NULL,
		idempotency_key TEXT NOT NULL, request_json TEXT NOT NULL, input_hash TEXT NOT NULL,
		state TEXT NOT NULL, resume_state TEXT NOT NULL DEFAULT '', attempt INTEGER NOT NULL DEFAULT 1,
		retryable INTEGER NOT NULL DEFAULT 0, error_code TEXT NOT NULL DEFAULT '',
		lease_owner TEXT NOT NULL DEFAULT '', lease_expires_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	stores := make(chan *ClinicalTrialAuditStore, 2)
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			store, openErr := NewClinicalTrialAuditStore(root)
			if openErr == nil {
				stores <- store
			}
			errs <- openErr
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(stores)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migration: %v", err)
		}
	}
	for store := range stores {
		defer store.Close()
	}
	check, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	for _, column := range []string{"lease_token_hash", "lease_generation"} {
		var count int
		if err := check.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('clinical_trial_audit_runs') WHERE name = ?`, column).Scan(&count); err != nil || count != 1 {
			t.Fatalf("column %s count=%d err=%v", column, count, err)
		}
	}
	var version int
	if err := check.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != clinicalTrialAuditSchemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
}

func TestClinicalTrialAuditStoreGetsAndListsWithStableCursor(t *testing.T) {
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))
	store, err := newClinicalTrialAuditStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var ids []string
	for index, item := range []struct{ pkg, input string }{
		{"package-a", "NCT00000001"},
		{"package-b", "NCT00000002"},
		{"package-a", "NCT00000003"},
	} {
		run, createErr := store.CreateRun(item.pkg, "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: item.input}, "key-"+item.input)
		if createErr != nil {
			t.Fatal(createErr)
		}
		ids = append(ids, run.RunID)
		clock.Advance(time.Second + time.Duration(index))
	}

	got, err := store.GetRun(ids[1])
	if err != nil || got.PackageID != "package-b" {
		t.Fatalf("get = %#v, err=%v", got, err)
	}
	if _, err := store.GetRun("missing"); !errors.Is(err, ErrClinicalTrialAuditRunNotFound) {
		t.Fatalf("missing = %v", err)
	}

	first, err := store.ListRuns(ClinicalTrialAuditListOptions{PackageID: "package-a", Limit: 1})
	if err != nil || len(first.Runs) != 1 || first.Runs[0].RunID != ids[2] || first.NextCursor == "" {
		t.Fatalf("first page = %#v, err=%v", first, err)
	}
	second, err := store.ListRuns(ClinicalTrialAuditListOptions{PackageID: "package-a", Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Runs) != 1 || second.Runs[0].RunID != ids[0] || second.NextCursor != "" {
		t.Fatalf("second page = %#v, err=%v", second, err)
	}
}

func TestClinicalTrialAuditStoreGetUsesConsistentReadSnapshot(t *testing.T) {
	root := t.TempDir()
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 13, 30, 0, 0, time.UTC))
	reader, err := newClinicalTrialAuditStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := newClinicalTrialAuditStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	created, err := writer.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000009"}, "consistent-read")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := writer.LeaseNextRun("worker-a", time.Minute)
	if err != nil || lease == nil {
		t.Fatalf("lease=%#v err=%v", lease, err)
	}
	snapshot := clinicalTrialAuditStoreSnapshot(t, clock.Now())
	if _, err := writer.CheckpointRun(created.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}}); err != nil {
		t.Fatal(err)
	}

	rowRead := make(chan struct{})
	writerDone := make(chan struct{})
	reader.afterRunRowRead = func() {
		close(rowRead)
		<-writerDone
	}
	type readResult struct {
		run ClinicalTrialAuditStoredRun
		err error
	}
	result := make(chan readResult, 1)
	go func() {
		run, readErr := reader.GetRun(created.RunID)
		result <- readResult{run: run, err: readErr}
	}()
	<-rowRead
	finding := ClinicalTrialFinding{FindingID: "finding-consistent", Class: ClinicalTrialFindingRegisteredFact, Summary: "Enrollment is registered.", CitationIDs: []string{"citation-consistent"}}
	citation := ClinicalTrialAuditCitation{CitationID: "citation-consistent", SourceFingerprint: snapshot.Fingerprint, Locator: "protocolSection"}
	if _, err := writer.CheckpointRun(created.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{
		State: ClinicalTrialAuditRunReasoning, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Findings: []ClinicalTrialFinding{finding}, Citations: []ClinicalTrialAuditCitation{citation},
	}); err != nil {
		t.Fatal(err)
	}
	close(writerDone)
	read := <-result
	if read.err != nil {
		t.Fatal(read.err)
	}
	if read.run.State != ClinicalTrialAuditRunComparing || len(read.run.Findings) != 0 {
		t.Fatalf("mixed read snapshot = %#v", read.run)
	}
}

func TestClinicalTrialAuditStoreLeasesQueuedRunOnceAcrossInstances(t *testing.T) {
	root := t.TempDir()
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 14, 0, 0, 0, time.UTC))
	first, err := newClinicalTrialAuditStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := newClinicalTrialAuditStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := first.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000004"}, "lease-once"); err != nil {
		t.Fatal(err)
	}

	type result struct {
		run *ClinicalTrialAuditStoredRun
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for index, store := range []*ClinicalTrialAuditStore{first, second} {
		wg.Add(1)
		go func(index int, store *ClinicalTrialAuditStore) {
			defer wg.Done()
			run, leaseErr := store.LeaseNextRun("worker-"+string(rune('a'+index)), time.Minute)
			results <- result{run: run, err: leaseErr}
		}(index, store)
	}
	wg.Wait()
	close(results)
	leased := 0
	for item := range results {
		if item.err != nil {
			t.Fatalf("lease: %v", item.err)
		}
		if item.run != nil {
			leased++
			if item.run.State != ClinicalTrialAuditRunCollecting || item.run.LeaseOwner == "" || item.run.LeaseExpiresAt == "" {
				t.Fatalf("leased run = %#v", item.run)
			}
		}
	}
	if leased != 1 {
		t.Fatalf("leased count = %d", leased)
	}
}

func TestClinicalTrialAuditStoreCheckpointsOwnedStagesAndPersistsTerminalAudit(t *testing.T) {
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC))
	store, err := newClinicalTrialAuditStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000005"}, "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	leased, err := store.LeaseNextRun("worker-a", 10*time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("lease = %#v, err=%v", leased, err)
	}
	snapshot := clinicalTrialAuditStoreSnapshot(t, clock.Now())
	if _, err := store.CheckpointRun(created.RunID, "worker-b", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}}); !errors.Is(err, ErrClinicalTrialAuditLeaseOwner) {
		t.Fatalf("foreign checkpoint = %v", err)
	}
	comparing, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}})
	if err != nil || comparing.State != ClinicalTrialAuditRunComparing || len(comparing.Sources) != 1 {
		t.Fatalf("comparing = %#v, err=%v", comparing, err)
	}
	if _, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunCompleted}); !errors.Is(err, ErrClinicalTrialAuditInvalidTransition) {
		t.Fatalf("invalid transition = %v", err)
	}

	finding := ClinicalTrialFinding{FindingID: "finding-1", Class: ClinicalTrialFindingRegisteredFact, Summary: "Registered enrollment is 100.", CitationIDs: []string{"citation-1"}}
	citation := ClinicalTrialAuditCitation{CitationID: "citation-1", SourceFingerprint: snapshot.Fingerprint, Locator: "protocolSection.designModule.enrollmentInfo"}
	badFinding := finding
	badFinding.CitationIDs = []string{"missing-citation"}
	if _, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{
		State: ClinicalTrialAuditRunReasoning, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Findings: []ClinicalTrialFinding{badFinding}, Citations: []ClinicalTrialAuditCitation{citation},
	}); err == nil {
		t.Fatal("reasoning checkpoint accepted an unknown citation")
	}
	reasoning, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{
		State: ClinicalTrialAuditRunReasoning, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Findings: []ClinicalTrialFinding{finding}, Citations: []ClinicalTrialAuditCitation{citation},
	})
	if err != nil || reasoning.State != ClinicalTrialAuditRunReasoning || len(reasoning.Findings) != 1 {
		t.Fatalf("reasoning = %#v, err=%v", reasoning, err)
	}
	if _, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunAwaitingReview}); err != nil {
		t.Fatalf("awaiting review: %v", err)
	}
	audit := ClinicalTrialAudit{
		SchemaVersion: ClinicalTrialAuditSchemaVersion,
		AuditID:       "audit-1",
		Request:       created.Request,
		Sources:       []ClinicalTrialSourceSnapshot{snapshot},
		Findings:      []ClinicalTrialFinding{finding},
		Citations:     []ClinicalTrialAuditCitation{citation},
		Confidence:    0.92,
		Limitations:   []string{"Publication comparison is pending."},
		CompletedAt:   clock.Now().Format(time.RFC3339Nano),
	}
	completed, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunCompleted, Audit: &audit})
	if err != nil || completed.Audit == nil || completed.Audit.AuditID != "audit-1" || completed.LeaseOwner != "" {
		t.Fatalf("completed = %#v, err=%v", completed, err)
	}
	loaded, err := store.GetRun(created.RunID)
	if err != nil || len(loaded.Sources) != 1 || len(loaded.Findings) != 1 || len(loaded.Citations) != 1 || loaded.Audit == nil {
		t.Fatalf("loaded = %#v, err=%v", loaded, err)
	}
	if _, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunFailed, ErrorCode: ClinicalTrialAuditErrorInternal}); !errors.Is(err, ErrClinicalTrialAuditTerminal) {
		t.Fatalf("terminal mutation = %v", err)
	}
}

func TestClinicalTrialAuditStoreBoundsFailureAndRetriesOnlyEligibleRuns(t *testing.T) {
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 16, 0, 0, 0, time.UTC))
	store, err := newClinicalTrialAuditStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000006"}, "retry")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryRun(created.RunID); !errors.Is(err, ErrClinicalTrialAuditNotRetryable) {
		t.Fatalf("retry queued = %v", err)
	}
	leased, err := store.LeaseNextRun("worker-a", time.Minute)
	if err != nil || leased == nil {
		t.Fatal(err)
	}
	if _, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunFailed, ErrorCode: "upstream timeout: dial tcp", Retryable: true}); err == nil {
		t.Fatal("unsafe error code accepted")
	}
	if _, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunFailed, ErrorCode: "arbitrary_lowercase", Retryable: true}); err == nil {
		t.Fatal("unregistered lowercase error code accepted")
	}
	if _, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunFailed, ErrorCode: "token_exposed_value", Retryable: true}); err == nil {
		t.Fatal("credential-shaped error code accepted")
	}
	failed, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunFailed, ErrorCode: ClinicalTrialAuditErrorSourceTimeout, Retryable: true})
	if err != nil || failed.ErrorCode != ClinicalTrialAuditErrorSourceTimeout || !failed.Retryable {
		t.Fatalf("failed = %#v, err=%v", failed, err)
	}
	retried, err := store.RetryRun(created.RunID)
	if err != nil || retried.State != ClinicalTrialAuditRunQueued || retried.Attempt != 2 || retried.ErrorCode != "" || !retried.Retryable {
		t.Fatalf("retried = %#v, err=%v", retried, err)
	}
	retriedLease, err := store.LeaseNextRun("worker-b", time.Minute)
	if err != nil || retriedLease == nil {
		t.Fatal(err)
	}
	if _, err := store.CheckpointRun(created.RunID, "worker-b", retriedLease.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunFailed, ErrorCode: strings.Repeat("x", clinicalTrialAuditErrorCodeMaxBytes+1), Retryable: false}); err == nil {
		t.Fatal("oversized error code accepted")
	}
}

func TestClinicalTrialAuditStoreRecoversExpiredLeaseAtCheckpoint(t *testing.T) {
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 17, 0, 0, 0, time.UTC))
	store, err := newClinicalTrialAuditStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000007"}, "expired")
	if err != nil {
		t.Fatal(err)
	}
	firstLease, err := store.LeaseNextRun("worker-a", time.Minute)
	if err != nil || firstLease == nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	snapshot := clinicalTrialAuditStoreSnapshot(t, clock.Now())
	if _, err := store.CheckpointRun(created.RunID, "worker-a", firstLease.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}}); !errors.Is(err, ErrClinicalTrialAuditLeaseExpired) {
		t.Fatalf("expired checkpoint = %v", err)
	}
	recovered, err := store.RecoverExpiredLeases()
	if err != nil || recovered != 1 {
		t.Fatalf("recover = %d, err=%v", recovered, err)
	}
	leased, err := store.LeaseNextRun("worker-a", time.Minute)
	if err != nil || leased == nil || leased.RunID != created.RunID || leased.State != ClinicalTrialAuditRunCollecting || leased.Attempt != 1 {
		t.Fatalf("re-lease = %#v, err=%v", leased, err)
	}
	if leased.LeaseToken == "" || leased.LeaseToken == firstLease.LeaseToken || leased.LeaseGeneration <= firstLease.LeaseGeneration {
		t.Fatalf("lease was not fenced: first=%#v second=%#v", firstLease, leased)
	}
	if _, err := store.CheckpointRun(created.RunID, "worker-a", firstLease.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}}); !errors.Is(err, ErrClinicalTrialAuditLeaseFence) {
		t.Fatalf("stale same-owner checkpoint = %v", err)
	}
	if _, err := store.CheckpointRun(created.RunID, "worker-a", leased.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}}); err != nil {
		t.Fatalf("current fenced checkpoint: %v", err)
	}
}

func TestClinicalTrialAuditStoreRenewsOnlyCurrentUnexpiredLease(t *testing.T) {
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 18, 0, 0, 0, time.UTC))
	store, err := newClinicalTrialAuditStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000010"}, "renew")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNextRun("worker-a", time.Minute)
	if err != nil || lease == nil {
		t.Fatalf("lease=%#v err=%v", lease, err)
	}
	clock.Advance(30 * time.Second)
	renewed, err := store.RenewLease(created.RunID, "worker-a", lease.LeaseToken, 2*time.Minute)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	wantExpiry := clock.Now().Add(2 * time.Minute).Format(time.RFC3339Nano)
	if renewed.LeaseExpiresAt != wantExpiry || renewed.LeaseGeneration != lease.LeaseGeneration || renewed.LeaseToken != lease.LeaseToken {
		t.Fatalf("renewed lease = %#v", renewed)
	}
	if _, err := store.RenewLease(created.RunID, "worker-a", "stale-token", time.Minute); !errors.Is(err, ErrClinicalTrialAuditLeaseFence) {
		t.Fatalf("stale renew = %v", err)
	}
	clock.Advance(2 * time.Minute)
	if _, err := store.RenewLease(created.RunID, "worker-a", lease.LeaseToken, time.Minute); !errors.Is(err, ErrClinicalTrialAuditLeaseExpired) {
		t.Fatalf("expired renew = %v", err)
	}
	snapshot := clinicalTrialAuditStoreSnapshot(t, clock.Now())
	if _, err := store.CheckpointRun(created.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}}); !errors.Is(err, ErrClinicalTrialAuditLeaseExpired) {
		t.Fatalf("expired checkpoint = %v", err)
	}
	current, err := store.GetRun(created.RunID)
	if err != nil || current.State != ClinicalTrialAuditRunCollecting {
		t.Fatalf("expired mutation changed run = %#v err=%v", current, err)
	}
}

func TestClinicalTrialAuditStoreFailsClosedWhenRandomnessFails(t *testing.T) {
	store, err := newClinicalTrialAuditStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.random = clinicalTrialAuditFailingReader{}
	if _, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000011"}, "random-create"); !errors.Is(err, errClinicalTrialAuditRandomUnavailable) {
		t.Fatalf("create randomness failure = %v", err)
	}
	page, err := store.ListRuns(ClinicalTrialAuditListOptions{})
	if err != nil || len(page.Runs) != 0 {
		t.Fatalf("failed create persisted run = %#v err=%v", page, err)
	}
	store.random = strings.NewReader(strings.Repeat("r", 128))
	created, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000012"}, "random-lease")
	if err != nil {
		t.Fatal(err)
	}
	store.random = clinicalTrialAuditFailingReader{}
	if _, err := store.LeaseNextRun("worker-a", time.Minute); !errors.Is(err, errClinicalTrialAuditRandomUnavailable) {
		t.Fatalf("lease randomness failure = %v", err)
	}
	current, err := store.GetRun(created.RunID)
	if err != nil || current.State != ClinicalTrialAuditRunQueued || current.LeaseOwner != "" {
		t.Fatalf("failed lease mutated run = %#v err=%v", current, err)
	}
}

func TestClinicalTrialAuditStoreRejectsOversizedCursor(t *testing.T) {
	store, err := NewClinicalTrialAuditStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.ListRuns(ClinicalTrialAuditListOptions{Cursor: strings.Repeat("A", clinicalTrialAuditCursorEncodedMaxBytes+1)}); err == nil {
		t.Fatal("oversized encoded cursor accepted")
	}
	decodedOversized := make([]byte, clinicalTrialAuditCursorDecodedMaxBytes+1)
	for index := range decodedOversized {
		decodedOversized[index] = 'x'
	}
	if _, err := decodeClinicalTrialAuditCursor(base64.RawURLEncoding.EncodeToString(decodedOversized)); err == nil {
		t.Fatal("oversized decoded cursor accepted")
	}
}

func TestClinicalTrialAuditStoreDefinesTerminalErrorCodeAllowlist(t *testing.T) {
	want := []string{
		ClinicalTrialAuditErrorIdentifierInvalid,
		ClinicalTrialAuditErrorSourceNotFound,
		ClinicalTrialAuditErrorSourceRateLimited,
		ClinicalTrialAuditErrorSourceTimeout,
		ClinicalTrialAuditErrorSourceSchemaInvalid,
		ClinicalTrialAuditErrorModelTimeout,
		ClinicalTrialAuditErrorModelInvalidOutput,
		ClinicalTrialAuditErrorEvidenceInvalid,
		ClinicalTrialAuditErrorRetryExhausted,
		ClinicalTrialAuditErrorInternal,
	}
	for _, code := range want {
		if err := validateClinicalTrialAuditErrorCode(code); err != nil {
			t.Errorf("allowlisted code %q: %v", code, err)
		}
	}
	for _, code := range []string{"", "arbitrary_lowercase", "token_value", "source timeout", strings.Repeat("x", clinicalTrialAuditErrorCodeMaxBytes+1)} {
		if err := validateClinicalTrialAuditErrorCode(code); err == nil {
			t.Errorf("unexpectedly accepted code %q", code)
		}
	}
}

func clinicalTrialAuditStoreSnapshot(t *testing.T, now time.Time) ClinicalTrialSourceSnapshot {
	t.Helper()
	snapshot, err := FinalizeClinicalTrialSourceSnapshot(ClinicalTrialSourceSnapshot{
		SourceType:   "clinicaltrials_gov",
		CanonicalID:  "NCT00000005",
		RetrievedAt:  now.Format(time.RFC3339Nano),
		ContentHash:  "sha256:" + strings.Repeat("a", 64),
		LicenseScope: "public_registry",
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

type clinicalTrialAuditStoreTestClock struct {
	mu  sync.Mutex
	now time.Time
}

type clinicalTrialAuditFailingReader struct{}

func (clinicalTrialAuditFailingReader) Read([]byte) (int, error) {
	return 0, errClinicalTrialAuditRandomUnavailable
}

func newClinicalTrialAuditStoreTestClock(now time.Time) *clinicalTrialAuditStoreTestClock {
	return &clinicalTrialAuditStoreTestClock{now: now}
}

func (c *clinicalTrialAuditStoreTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clinicalTrialAuditStoreTestClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}
