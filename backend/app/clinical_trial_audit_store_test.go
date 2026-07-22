package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClinicalTrialAuditStoredRunProjectsStrictPublicContract(t *testing.T) {
	request, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"})
	if err != nil {
		t.Fatal(err)
	}
	stored := ClinicalTrialAuditStoredRun{
		SchemaVersion:  ClinicalTrialAuditStoredRunSchemaVersion,
		RunID:          "run-projection",
		PackageID:      "book-agent-clinical-trials-truth",
		PackageVersion: "1.2.0",
		State:          ClinicalTrialAuditRunFailed,
		Request:        request,
		ErrorCode:      ClinicalTrialAuditErrorSourceNotFound,
		CreatedAt:      "2026-07-22T12:00:00Z",
		UpdatedAt:      "2026-07-22T12:01:00Z",
	}
	publicRun, err := ProjectClinicalTrialAuditRun(stored)
	if err != nil {
		t.Fatalf("ProjectClinicalTrialAuditRun() error = %v", err)
	}
	if publicRun.SchemaVersion != ClinicalTrialAuditRunSchemaVersion || publicRun.Error == nil || *publicRun.Error != ClinicalTrialAuditErrorSourceNotFound {
		t.Fatalf("public run = %#v", publicRun)
	}
	encoded, err := json.Marshal(publicRun)
	if err != nil {
		t.Fatal(err)
	}
	var publicShape map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &publicShape); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"package_id", "package_version", "idempotency_key", "error_code", "retryable", "lease_owner", "lease_expires_at"} {
		if _, exists := publicShape[forbidden]; exists {
			t.Fatalf("public projection leaked %q: %s", forbidden, encoded)
		}
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	var roundTrip ClinicalTrialAuditRun
	if err := decoder.Decode(&roundTrip); err != nil {
		t.Fatalf("strict public round trip: %v", err)
	}
	if err := ValidateClinicalTrialAuditRun(roundTrip); err != nil {
		t.Fatalf("projected public run is invalid: %v", err)
	}
}

func TestClinicalTrialAuditStoredRunUsesInternalSchema(t *testing.T) {
	store, err := NewClinicalTrialAuditStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"}, "schema-key")
	if err != nil {
		t.Fatal(err)
	}
	if run.SchemaVersion != ClinicalTrialAuditStoredRunSchemaVersion || run.SchemaVersion == ClinicalTrialAuditRunSchemaVersion {
		t.Fatalf("stored schema version = %q", run.SchemaVersion)
	}
}

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

func TestClinicalTrialAuditStorePersistsImmutableEvidenceAndRecoversAfterReopen(t *testing.T) {
	root := t.TempDir()
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))
	store, err := newClinicalTrialAuditStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"}, "evidence-reopen")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNextRun("worker-a", time.Minute)
	if err != nil || lease == nil || lease.RunID != run.RunID {
		t.Fatalf("lease = %#v err=%v", lease, err)
	}
	study := clinicalTrialAuditStoreStudy()
	evidence, err := NewClinicalTrialsGovEvidencePayload(study)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := clinicalTrialAuditStoreEvidenceSnapshot(t, evidence.ContentHash, clock.Now())
	checkpointed, err := store.CheckpointRun(run.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{
		State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Evidence: []ClinicalTrialAuditEvidencePayload{evidence},
	})
	if err != nil {
		t.Fatalf("checkpoint evidence: %v", err)
	}
	if len(checkpointed.Evidence) != 1 || checkpointed.Evidence[0].ContentHash != snapshot.ContentHash {
		t.Fatalf("checkpointed evidence = %#v", checkpointed.Evidence)
	}
	citation := ClinicalTrialAuditCitation{CitationID: "flow-started", SourceFingerprint: snapshot.Fingerprint, Locator: "participant_flow.periods[0].milestones[0].achievements[0]"}
	finding := ClinicalTrialFinding{FindingID: "actual-enrollment", Class: ClinicalTrialFindingRegisteredFact, Summary: "120 participants started in the experimental group.", CitationIDs: []string{citation.CitationID}}
	checkpointed, err = store.CheckpointRun(run.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{
		State: ClinicalTrialAuditRunReasoning, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Citations: []ClinicalTrialAuditCitation{citation}, Findings: []ClinicalTrialFinding{finding},
	})
	if err != nil {
		t.Fatalf("reuse persisted evidence: %v", err)
	}
	if len(checkpointed.Evidence) != 1 {
		t.Fatalf("later checkpoint lost immutable evidence: %#v", checkpointed.Evidence)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := newClinicalTrialAuditStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.GetRun(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Evidence) != 1 {
		t.Fatalf("recovered evidence = %#v", recovered.Evidence)
	}
	if recovered.Sources[0].DataTimestamp != "2026-07-21T15:04:05Z" || recovered.Sources[0].ProvenanceDigest == "" {
		t.Fatalf("recovered provenance = %#v", recovered.Sources[0])
	}
	recoveredStudy, err := DecodeClinicalTrialsGovEvidencePayload(recovered.Evidence[0])
	if err != nil {
		t.Fatalf("decode recovered evidence: %v", err)
	}
	if recoveredStudy.NCTID != study.NCTID || recoveredStudy.ParticipantFlow.Periods[0].Milestones[0].Achievements[0].Subjects != "120" {
		t.Fatalf("recovered study = %#v", recoveredStudy)
	}
}

func TestClinicalTrialAuditStoreCanonicalizesEvidenceDataBeforePersistence(t *testing.T) {
	store, run, lease, snapshot, evidence := clinicalTrialAuditStoreEvidenceFixture(t)
	defer store.Close()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, evidence.Data, "", "  "); err != nil {
		t.Fatal(err)
	}
	evidence.Data = pretty.Bytes()
	if _, err := store.CheckpointRun(run.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{
		State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Evidence: []ClinicalTrialAuditEvidencePayload{evidence},
	}); err != nil {
		t.Fatalf("checkpoint pretty normalized evidence: %v", err)
	}
	loaded, err := store.GetRun(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Evidence) != 1 || bytes.Equal(loaded.Evidence[0].Data, pretty.Bytes()) || bytes.Contains(loaded.Evidence[0].Data, []byte("\n")) {
		t.Fatalf("stored evidence was not canonicalized: %s", loaded.Evidence[0].Data)
	}
}

func TestClinicalTrialAuditStoreFailsClosedOnCorruptSnapshotEvidenceChain(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(*testing.T, *ClinicalTrialAuditStore, ClinicalTrialAuditStoredRun, ClinicalTrialAuditEvidencePayload)
	}{
		{name: "snapshot json fingerprint", corrupt: func(t *testing.T, store *ClinicalTrialAuditStore, run ClinicalTrialAuditStoredRun, evidence ClinicalTrialAuditEvidencePayload) {
			var encoded string
			if err := store.db.QueryRow(`SELECT snapshot_json FROM clinical_trial_audit_snapshots WHERE run_id = ?`, run.RunID).Scan(&encoded); err != nil {
				t.Fatal(err)
			}
			var snapshot ClinicalTrialSourceSnapshot
			if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
				t.Fatal(err)
			}
			snapshot.Fingerprint = "sha256:" + strings.Repeat("b", 64)
			mutated, _ := json.Marshal(snapshot)
			if _, err := store.db.Exec(`UPDATE clinical_trial_audit_snapshots SET snapshot_json = ? WHERE run_id = ?`, string(mutated), run.RunID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "link content hash", corrupt: func(t *testing.T, store *ClinicalTrialAuditStore, run ClinicalTrialAuditStoredRun, evidence ClinicalTrialAuditEvidencePayload) {
			otherStudy := clinicalTrialAuditStoreStudy()
			otherStudy.BriefTitle = "Different normalized evidence"
			other, err := NewClinicalTrialsGovEvidencePayload(otherStudy)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(other)
			if _, err := store.db.Exec(`INSERT INTO clinical_trial_audit_evidence(content_hash, source_type, evidence_json) VALUES (?, ?, ?)`, other.ContentHash, other.SourceType, string(encoded)); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`UPDATE clinical_trial_audit_snapshot_evidence SET content_hash = ? WHERE run_id = ?`, other.ContentHash, run.RunID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "evidence source type", corrupt: func(t *testing.T, store *ClinicalTrialAuditStore, _ ClinicalTrialAuditStoredRun, evidence ClinicalTrialAuditEvidencePayload) {
			if _, err := store.db.Exec(`UPDATE clinical_trial_audit_evidence SET source_type = 'forged_source' WHERE content_hash = ?`, evidence.ContentHash); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "evidence payload", corrupt: func(t *testing.T, store *ClinicalTrialAuditStore, _ ClinicalTrialAuditStoredRun, evidence ClinicalTrialAuditEvidencePayload) {
			if _, err := store.db.Exec(`UPDATE clinical_trial_audit_evidence SET evidence_json = ? WHERE content_hash = ?`, "{", evidence.ContentHash); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "link retrieved at", corrupt: func(t *testing.T, store *ClinicalTrialAuditStore, run ClinicalTrialAuditStoredRun, _ ClinicalTrialAuditEvidencePayload) {
			if _, err := store.db.Exec(`UPDATE clinical_trial_audit_snapshot_evidence SET retrieved_at = '2026-07-22T14:00:00Z' WHERE run_id = ?`, run.RunID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "link data timestamp", corrupt: func(t *testing.T, store *ClinicalTrialAuditStore, run ClinicalTrialAuditStoredRun, _ ClinicalTrialAuditEvidencePayload) {
			if _, err := store.db.Exec(`UPDATE clinical_trial_audit_snapshot_evidence SET data_timestamp = '2026-07-21T16:04:05Z' WHERE run_id = ?`, run.RunID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "link provenance digest", corrupt: func(t *testing.T, store *ClinicalTrialAuditStore, run ClinicalTrialAuditStoredRun, _ ClinicalTrialAuditEvidencePayload) {
			if _, err := store.db.Exec(`UPDATE clinical_trial_audit_snapshot_evidence SET provenance_digest = ? WHERE run_id = ?`, "sha256:"+strings.Repeat("f", 64), run.RunID); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, run, lease, snapshot, evidence := clinicalTrialAuditStoreEvidenceFixture(t)
			defer store.Close()
			if _, err := store.CheckpointRun(run.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{
				State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Evidence: []ClinicalTrialAuditEvidencePayload{evidence},
			}); err != nil {
				t.Fatal(err)
			}
			test.corrupt(t, store, run, evidence)
			if _, err := store.GetRun(run.RunID); err == nil {
				t.Fatal("GetRun accepted corrupt snapshot/evidence chain")
			}
		})
	}
}

func TestClinicalTrialAuditStoreRejectsEvidenceHashMismatchTransactionally(t *testing.T) {
	store, err := NewClinicalTrialAuditStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"}, "evidence-mismatch")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNextRun("worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := NewClinicalTrialsGovEvidencePayload(clinicalTrialAuditStoreStudy())
	if err != nil {
		t.Fatal(err)
	}
	evidence.ContentHash = "sha256:" + strings.Repeat("f", 64)
	snapshot := clinicalTrialAuditStoreEvidenceSnapshot(t, evidence.ContentHash, time.Now())
	if _, err := store.CheckpointRun(run.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{
		State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Evidence: []ClinicalTrialAuditEvidencePayload{evidence},
	}); err == nil {
		t.Fatal("checkpoint accepted evidence hash mismatch")
	}
	var evidenceRows, snapshotRows int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM clinical_trial_audit_evidence`).Scan(&evidenceRows); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM clinical_trial_audit_snapshots WHERE run_id = ?`, run.RunID).Scan(&snapshotRows); err != nil {
		t.Fatal(err)
	}
	if evidenceRows != 0 || snapshotRows != 0 {
		t.Fatalf("partial checkpoint persisted evidence=%d snapshots=%d", evidenceRows, snapshotRows)
	}
}

func TestClinicalTrialAuditStoreDeduplicatesEvidenceAcrossConcurrentStores(t *testing.T) {
	root := t.TempDir()
	first, err := NewClinicalTrialAuditStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewClinicalTrialAuditStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	evidence, err := NewClinicalTrialsGovEvidencePayload(clinicalTrialAuditStoreStudy())
	if err != nil {
		t.Fatal(err)
	}
	type leasedRun struct {
		store *ClinicalTrialAuditStore
		run   ClinicalTrialAuditStoredRun
		lease *ClinicalTrialAuditStoredRun
	}
	items := make([]leasedRun, 0, 2)
	for index, store := range []*ClinicalTrialAuditStore{first, second} {
		run, createErr := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"}, fmt.Sprintf("evidence-concurrent-%d", index))
		if createErr != nil {
			t.Fatal(createErr)
		}
		lease, leaseErr := store.LeaseNextRun(fmt.Sprintf("worker-%d", index), time.Minute)
		if leaseErr != nil || lease == nil {
			t.Fatalf("lease %d = %#v err=%v", index, lease, leaseErr)
		}
		items = append(items, leasedRun{store: store, run: run, lease: lease})
	}
	start := make(chan struct{})
	errs := make(chan error, len(items))
	var wg sync.WaitGroup
	for index, item := range items {
		wg.Add(1)
		go func(index int, item leasedRun) {
			defer wg.Done()
			<-start
			snapshot := clinicalTrialAuditStoreEvidenceSnapshot(t, evidence.ContentHash, time.Now())
			_, checkpointErr := item.store.CheckpointRun(item.run.RunID, fmt.Sprintf("worker-%d", index), item.lease.LeaseToken, ClinicalTrialAuditCheckpoint{
				State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Evidence: []ClinicalTrialAuditEvidencePayload{evidence},
			})
			errs <- checkpointErr
		}(index, item)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent evidence checkpoint: %v", err)
		}
	}
	var evidenceRows, links int
	if err := first.db.QueryRow(`SELECT COUNT(*) FROM clinical_trial_audit_evidence`).Scan(&evidenceRows); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRow(`SELECT COUNT(*) FROM clinical_trial_audit_snapshot_evidence`).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if evidenceRows != 1 || links != 2 {
		t.Fatalf("dedup rows=%d links=%d", evidenceRows, links)
	}
}

func TestClinicalTrialAuditStoreDeduplicatesEvidenceAcrossDistinctProvenance(t *testing.T) {
	store, err := NewClinicalTrialAuditStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	evidence, err := NewClinicalTrialsGovEvidencePayload(clinicalTrialAuditStoreStudy())
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range []struct {
		retrievedAt, dataTimestamp string
	}{
		{"2026-07-22T13:00:00Z", "2026-07-21T15:04:05Z"},
		{"2026-07-22T14:00:00Z", "2026-07-21T16:04:05Z"},
	} {
		run, createErr := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"}, fmt.Sprintf("provenance-%d", index))
		if createErr != nil {
			t.Fatal(createErr)
		}
		lease, leaseErr := store.LeaseNextRun(fmt.Sprintf("worker-%d", index), time.Minute)
		if leaseErr != nil || lease == nil || lease.RunID != run.RunID {
			t.Fatalf("lease %d = %#v err=%v", index, lease, leaseErr)
		}
		snapshot := clinicalTrialAuditStoreEvidenceSnapshotAt(t, evidence.ContentHash, item.retrievedAt, item.dataTimestamp)
		if _, checkpointErr := store.CheckpointRun(run.RunID, fmt.Sprintf("worker-%d", index), lease.LeaseToken, ClinicalTrialAuditCheckpoint{
			State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Evidence: []ClinicalTrialAuditEvidencePayload{evidence},
		}); checkpointErr != nil {
			t.Fatal(checkpointErr)
		}
		loaded, loadErr := store.GetRun(run.RunID)
		if loadErr != nil || loaded.Sources[0].RetrievedAt != item.retrievedAt || loaded.Sources[0].DataTimestamp != item.dataTimestamp {
			t.Fatalf("loaded provenance %d = %#v err=%v", index, loaded.Sources, loadErr)
		}
	}
	var evidenceRows, links int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM clinical_trial_audit_evidence`).Scan(&evidenceRows); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM clinical_trial_audit_snapshot_evidence`).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if evidenceRows != 1 || links != 2 {
		t.Fatalf("stable evidence dedup rows=%d links=%d", evidenceRows, links)
	}
}

func TestClinicalTrialAuditStoreMigratesVersion3EvidenceProvenance(t *testing.T) {
	root, runID, dataTimestamp := prepareClinicalTrialAuditV3EvidenceFixture(t, nil)
	reopened, err := NewClinicalTrialAuditStore(root)
	if err != nil {
		t.Fatalf("migrate v3 fixture: %v", err)
	}
	defer reopened.Close()
	loaded, err := reopened.GetRun(runID)
	if err != nil {
		t.Fatalf("read migrated v3 run: %v", err)
	}
	if len(loaded.Evidence) != 1 || len(loaded.Sources) != 1 || loaded.Sources[0].DataTimestamp != dataTimestamp || loaded.Sources[0].ProvenanceDigest == "" {
		t.Fatalf("migrated run = %#v", loaded)
	}
	var migratedVersion int
	if err := reopened.db.QueryRow(`PRAGMA user_version`).Scan(&migratedVersion); err != nil || migratedVersion != clinicalTrialAuditSchemaVersion {
		t.Fatalf("migrated version=%d err=%v", migratedVersion, err)
	}
}

func TestClinicalTrialAuditStoreMarksInvalidVersion3EvidenceIncompatible(t *testing.T) {
	root, runID, _ := prepareClinicalTrialAuditV3EvidenceFixture(t, func(evidence map[string]any) {
		data := evidence["data"].(map[string]any)
		coverage := data["coverage"].(map[string]any)
		coverage["limitations"] = []any{"No limitations"}
	})
	store, err := NewClinicalTrialAuditStore(root)
	if err != nil {
		t.Fatalf("open database containing incompatible v3 evidence: %v", err)
	}
	defer store.Close()
	if _, err := store.GetRun(runID); !errors.Is(err, ErrClinicalTrialAuditEvidenceIncompatible) {
		t.Fatalf("incompatible v3 evidence error = %v", err)
	}
	var compatible int
	var code string
	if err := store.db.QueryRow(`SELECT compatible, incompatibility_code FROM clinical_trial_audit_evidence`).Scan(&compatible, &code); err != nil {
		t.Fatal(err)
	}
	if compatible != 0 || code != clinicalTrialAuditEvidenceV1Incompatible {
		t.Fatalf("compatibility state = %d %q", compatible, code)
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

func TestClinicalTrialAuditStoreRollsBackCheckpointWhenLeaseExpiresBeforeFinalFence(t *testing.T) {
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 17, 30, 0, 0, time.UTC))
	store, err := newClinicalTrialAuditStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000013"}, "checkpoint-expiry-race")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNextRun("worker-a", time.Minute)
	if err != nil || lease == nil {
		t.Fatalf("lease=%#v err=%v", lease, err)
	}
	clock.Advance(59 * time.Second)
	store.beforeLeaseFinalUpdate = func() { clock.Advance(2 * time.Second) }
	snapshot := clinicalTrialAuditStoreSnapshot(t, clock.Now())
	if _, err := store.CheckpointRun(created.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{
		State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot},
	}); !errors.Is(err, ErrClinicalTrialAuditLeaseExpired) {
		t.Fatalf("checkpoint expiry race = %v", err)
	}
	current, err := store.GetRun(created.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != ClinicalTrialAuditRunCollecting || len(current.Sources) != 0 {
		t.Fatalf("expired checkpoint leaked partial evidence = %#v", current)
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

func TestClinicalTrialAuditStoreRenewUsesFreshFenceAndNeverShortensLease(t *testing.T) {
	clock := newClinicalTrialAuditStoreTestClock(time.Date(2026, 7, 22, 18, 30, 0, 0, time.UTC))
	store, err := newClinicalTrialAuditStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT00000014"}, "renew-fresh-fence")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNextRun("worker-a", 5*time.Minute)
	if err != nil || lease == nil {
		t.Fatalf("lease=%#v err=%v", lease, err)
	}
	originalExpiry := lease.LeaseExpiresAt
	clock.Advance(30 * time.Second)
	renewed, err := store.RenewLease(created.RunID, "worker-a", lease.LeaseToken, time.Minute)
	if err != nil {
		t.Fatalf("short renewal: %v", err)
	}
	if renewed.LeaseExpiresAt != originalExpiry {
		t.Fatalf("renewal shortened lease: original=%q renewed=%q", originalExpiry, renewed.LeaseExpiresAt)
	}

	clock.Advance(4*time.Minute + 29*time.Second)
	store.beforeLeaseFinalUpdate = func() { clock.Advance(2 * time.Second) }
	if _, err := store.RenewLease(created.RunID, "worker-a", lease.LeaseToken, 2*time.Minute); !errors.Is(err, ErrClinicalTrialAuditLeaseExpired) {
		t.Fatalf("renew expiry race = %v", err)
	}
	current, err := store.GetRun(created.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.LeaseExpiresAt != originalExpiry {
		t.Fatalf("expired renewal changed lease: original=%q current=%q", originalExpiry, current.LeaseExpiresAt)
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
		ClinicalTrialAuditErrorSourceCanceled,
		ClinicalTrialAuditErrorSourceResponseTooLarge,
		ClinicalTrialAuditErrorSourceMalformedJSON,
		ClinicalTrialAuditErrorSourceIdentifierMismatch,
		ClinicalTrialAuditErrorSourceSchemaInvalid,
		ClinicalTrialAuditErrorSourceUpstreamPermanent,
		ClinicalTrialAuditErrorSourceUpstreamTransient,
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

func clinicalTrialAuditStoreEvidenceSnapshot(t *testing.T, contentHash string, now time.Time) ClinicalTrialSourceSnapshot {
	t.Helper()
	return clinicalTrialAuditStoreEvidenceSnapshotAt(t, contentHash, now.UTC().Format(time.RFC3339Nano), "2026-07-21T15:04:05Z")
}

func clinicalTrialAuditStoreEvidenceSnapshotAt(t *testing.T, contentHash, retrievedAt, dataTimestamp string) ClinicalTrialSourceSnapshot {
	t.Helper()
	snapshot, err := FinalizeClinicalTrialSourceSnapshot(ClinicalTrialSourceSnapshot{
		SourceType: ClinicalTrialsGovStudySourceType, CanonicalID: "NCT01234567", RetrievedAt: retrievedAt,
		DataTimestamp: dataTimestamp, ContentHash: contentHash, LicenseScope: "public_metadata",
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func clinicalTrialAuditStoreEvidenceFixtureAt(t *testing.T, root string) (*ClinicalTrialAuditStore, ClinicalTrialAuditStoredRun, *ClinicalTrialAuditStoredRun, ClinicalTrialSourceSnapshot, ClinicalTrialAuditEvidencePayload) {
	t.Helper()
	store, err := NewClinicalTrialAuditStore(root)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun("package-a", "1.0.0", ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: "NCT01234567"}, "evidence-fixture")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	lease, err := store.LeaseNextRun("worker-a", time.Minute)
	if err != nil || lease == nil {
		store.Close()
		t.Fatalf("lease = %#v err=%v", lease, err)
	}
	evidence, err := NewClinicalTrialsGovEvidencePayload(clinicalTrialAuditStoreStudy())
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	snapshot := clinicalTrialAuditStoreEvidenceSnapshot(t, evidence.ContentHash, time.Now())
	return store, run, lease, snapshot, evidence
}

func prepareClinicalTrialAuditV3EvidenceFixture(t *testing.T, mutateEvidence func(map[string]any)) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	store, run, lease, snapshot, evidence := clinicalTrialAuditStoreEvidenceFixtureAt(t, root)
	if _, err := store.CheckpointRun(run.RunID, "worker-a", lease.LeaseToken, ClinicalTrialAuditCheckpoint{
		State: ClinicalTrialAuditRunComparing, Sources: []ClinicalTrialSourceSnapshot{snapshot}, Evidence: []ClinicalTrialAuditEvidencePayload{evidence},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, clinicalTrialAuditDBName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var evidenceJSON, snapshotJSON string
	if err := db.QueryRow(`SELECT evidence_json FROM clinical_trial_audit_evidence`).Scan(&evidenceJSON); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT snapshot_json FROM clinical_trial_audit_snapshots WHERE run_id = ?`, run.RunID).Scan(&snapshotJSON); err != nil {
		t.Fatal(err)
	}
	var evidenceObject, snapshotObject map[string]any
	if err := json.Unmarshal([]byte(evidenceJSON), &evidenceObject); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshotObject); err != nil {
		t.Fatal(err)
	}
	evidenceObject["provenance"] = map[string]any{"data_timestamp": snapshot.DataTimestamp}
	delete(snapshotObject, "data_timestamp")
	delete(snapshotObject, "provenance_digest")
	if mutateEvidence != nil {
		mutateEvidence(evidenceObject)
	}
	legacyEvidence, err := json.Marshal(evidenceObject)
	if err != nil {
		t.Fatal(err)
	}
	legacySnapshot, err := json.Marshal(snapshotObject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE clinical_trial_audit_evidence SET evidence_json = ?`, string(legacyEvidence)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE clinical_trial_audit_snapshots SET snapshot_json = ? WHERE run_id = ?`, string(legacySnapshot), run.RunID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ALTER TABLE clinical_trial_audit_snapshot_evidence DROP COLUMN retrieved_at`,
		`ALTER TABLE clinical_trial_audit_snapshot_evidence DROP COLUMN data_timestamp`,
		`ALTER TABLE clinical_trial_audit_snapshot_evidence DROP COLUMN provenance_digest`,
		`PRAGMA user_version = 3`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("prepare v3 fixture %q: %v", statement, err)
		}
	}
	return root, run.RunID, snapshot.DataTimestamp
}

func clinicalTrialAuditStoreEvidenceFixture(t *testing.T) (*ClinicalTrialAuditStore, ClinicalTrialAuditStoredRun, *ClinicalTrialAuditStoredRun, ClinicalTrialSourceSnapshot, ClinicalTrialAuditEvidencePayload) {
	t.Helper()
	return clinicalTrialAuditStoreEvidenceFixtureAt(t, t.TempDir())
}

func clinicalTrialAuditStoreStudy() ClinicalTrialsGovStudy {
	return ClinicalTrialsGovStudy{
		SourceAPIVersion: "2.0.4", NCTID: "NCT01234567", BriefTitle: "Synthetic trial", OverallStatus: "COMPLETED", StudyType: "INTERVENTIONAL",
		Enrollment:       ClinicalTrialsGovEnrollment{Count: 240, Type: "ACTUAL"},
		LastUpdatePosted: ClinicalTrialsGovDate{Value: "2026-07-20T00:00:00Z", Type: "ACTUAL", Precision: "day"},
		Coverage: ClinicalTrialsGovEvidenceCoverage{
			IncludedModules: []string{"protocol", "participant_flow", "outcome_measures"},
			ExcludedModules: []string{"baseline_characteristics", "adverse_events", "more_info"},
			Limitations:     []string{ClinicalTrialsGovCoverageLimitationResultsModulesExcludedV1},
		},
		ParticipantFlow: ClinicalTrialsGovParticipantFlow{
			Groups:  []ClinicalTrialsGovResultGroup{{ID: "FG000", Title: "Experimental"}},
			Periods: []ClinicalTrialsGovFlowPeriod{{Title: "Overall", Milestones: []ClinicalTrialsGovFlowMilestone{{Type: "STARTED", Achievements: []ClinicalTrialsGovFlowCount{{GroupID: "FG000", Subjects: "120"}}}}}},
		},
	}
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
