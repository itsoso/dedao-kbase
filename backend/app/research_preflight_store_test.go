package app

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
)

var (
	testResearchPreflightOwnerA = testResearchPreflightOwnerHash("a")
	testResearchPreflightOwnerB = testResearchPreflightOwnerHash("b")
)

func TestResearchPreflightStoreCreateLoadAndRestartPersistence(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	store, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := testResearchPreflightStoreRequest()
	result := testResearchPreflight()
	result.PreflightID = ""
	result.RequestHash = ""
	result.CreatedAt = ""
	result.ExpiresAt = ""

	created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, request, result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.PreflightID, "research-preflight-") || created.RequestHash == "" {
		t.Fatalf("created identity = %#v", created)
	}
	if created.CreatedAt != formatResearchPreflightTimestamp(now) || created.ExpiresAt != formatResearchPreflightTimestamp(now.Add(10*time.Minute)) {
		t.Fatalf("created timestamps = %q to %q", created.CreatedAt, created.ExpiresAt)
	}
	loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, testResearchPreflightOwnerA)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, created) {
		t.Fatalf("loaded = %#v, want %#v", loaded, created)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.LoadResearchPreflightForOwner(created.PreflightID, testResearchPreflightOwnerA)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted, created) {
		t.Fatalf("persisted = %#v, want %#v", persisted, created)
	}
}

func TestResearchPreflightStoreMigrationContainsNoRawQuestionColumns(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	rows, err := store.db.Query(`PRAGMA table_info(research_preflights)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"preflight_id", "owner_hash", "request_hash", "status", "candidates_json",
		"checks_json", "gaps_json", "mode", "requested_sources_json", "package_constraint",
		"parent_run_id", "bound_run_id", "created_at", "expires_at",
	} {
		if !columns[required] {
			t.Fatalf("missing migration column %q: %#v", required, columns)
		}
	}
	for _, forbidden := range []string{"question", "content", "message", "locator", "path"} {
		if columns[forbidden] {
			t.Fatalf("migration contains private column %q", forbidden)
		}
	}
}

func TestResearchRunRequiresPreflightMigrationAddsBackwardCompatibleLinkColumns(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(root, researchStoreDBName))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE research_runs (
		run_id TEXT PRIMARY KEY, idempotency_key TEXT NOT NULL UNIQUE, request_hash TEXT NOT NULL,
		schema_version TEXT NOT NULL, mode TEXT NOT NULL, question TEXT NOT NULL, status TEXT NOT NULL,
		package_id TEXT NOT NULL DEFAULT '', package_version TEXT NOT NULL DEFAULT '',
		subject_ids_json TEXT NOT NULL DEFAULT '[]', requested_sources_json TEXT NOT NULL DEFAULT '[]',
		route_reasons_json TEXT NOT NULL DEFAULT '[]', actual_scope_json TEXT NOT NULL DEFAULT '{}',
		budget_json TEXT NOT NULL DEFAULT '{}', wait_reason TEXT NOT NULL DEFAULT '', failure_json TEXT NOT NULL DEFAULT '',
		version INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		lease_owner TEXT NOT NULL DEFAULT '', lease_epoch TEXT NOT NULL DEFAULT '', lease_expires_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenResearchStore(root, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, target := range []struct {
		table  string
		column string
	}{
		{table: "research_runs", column: "parent_run_id"},
		{table: "research_runs", column: "preflight_id"},
		{table: "research_preflights", column: "bound_run_id"},
	} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, target.table, target.column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("missing %s.%s", target.table, target.column)
		}
	}
}

func TestResearchRunRequiresPreflightRejectsEveryMismatchWithoutRunRows(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *ResearchStore, *ResearchRunConfirmation)
		want   error
	}{
		{name: "missing preflight", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.Input.Request.PreflightID = ""
		}, want: ErrResearchPreflightRequired},
		{name: "not found", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.Input.Request.PreflightID = "research-preflight-missing"
		}, want: ErrResearchPreflightNotFound},
		{name: "wrong owner", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.OwnerHash = testResearchPreflightOwnerB
		}, want: ErrResearchPreflightOwner},
		{name: "question changed", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.Input.Request.Question = "changed generic question"
		}, want: ErrResearchPreflightRequestChanged},
		{name: "mode changed", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.Input.Request.Mode = ResearchModeAuto
		}, want: ErrResearchPreflightRequestChanged},
		{name: "sources changed", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.Input.Request.RequestedSources = nil
		}, want: ErrResearchPreflightRequestChanged},
		{name: "package constraint changed", mutate: func(t *testing.T, store *ResearchStore, value *ResearchRunConfirmation) {
			if _, err := store.db.Exec(`UPDATE research_preflights SET package_constraint = ? WHERE preflight_id = ?`, "candidate-other", value.Input.Request.PreflightID); err != nil {
				t.Fatal(err)
			}
		}, want: ErrResearchPreflightRequestChanged},
		{name: "parent changed", mutate: func(t *testing.T, store *ResearchStore, value *ResearchRunConfirmation) {
			if _, err := store.db.Exec(`UPDATE research_preflights SET parent_run_id = ? WHERE preflight_id = ?`, "research-run-other", value.Input.Request.PreflightID); err != nil {
				t.Fatal(err)
			}
		}, want: ErrResearchPreflightRequestChanged},
		{name: "selection outside snapshot", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.Input.Request.PackageID = "candidate-other"
			value.SelectedCandidate.PackageID = "candidate-other"
		}, want: ErrResearchPreflightCandidate},
		{name: "package version changed", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.Input.Request.PackageVersion = "2.0.0"
			value.SelectedCandidate.PackageVersion = "2.0.0"
		}, want: ErrResearchPreflightCandidate},
		{name: "package content changed", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.SelectedCandidate.ContentHash = "changed-content-hash"
		}, want: ErrResearchPreflightPackageChanged},
		{name: "readiness changed", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.SelectedCandidate.Readiness = ResearchPreflightCheckWarning
		}, want: ErrResearchPreflightReadinessChanged},
		{name: "budget changed", mutate: func(_ *testing.T, _ *ResearchStore, value *ResearchRunConfirmation) {
			value.SelectedCandidate.Budget.Limits.MaxIterations++
		}, want: ErrResearchPreflightReadinessChanged},
		{name: "blocked preflight", mutate: func(t *testing.T, store *ResearchStore, value *ResearchRunConfirmation) {
			blocked := testResearchPreflight()
			blocked.PreflightID = value.Input.Request.PreflightID
			blocked.Status = ResearchPreflightStatusBlocked
			blocked.Candidates[0].Readiness = ResearchPreflightCheckBlocked
			blocked.Checks[0].Status = ResearchPreflightCheckBlocked
			payload, err := encodeResearchPreflightPayload(blocked)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`UPDATE research_preflights SET status = ?, candidates_json = ?, checks_json = ? WHERE preflight_id = ?`, blocked.Status, payload.candidatesJSON, payload.checksJSON, value.Input.Request.PreflightID); err != nil {
				t.Fatal(err)
			}
		}, want: ErrResearchPreflightBlocked},
		{name: "blocked selected candidate", mutate: func(t *testing.T, store *ResearchStore, value *ResearchRunConfirmation) {
			preflight, err := store.LoadResearchPreflightForOwner(value.Input.Request.PreflightID, value.OwnerHash)
			if err != nil {
				t.Fatal(err)
			}
			preflight.Candidates[0].Readiness = ResearchPreflightCheckBlocked
			preflight.Status = ResearchPreflightStatusBlocked
			payload, err := encodeResearchPreflightPayload(*preflight)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`UPDATE research_preflights SET status = ?, candidates_json = ? WHERE preflight_id = ?`, preflight.Status, payload.candidatesJSON, preflight.PreflightID); err != nil {
				t.Fatal(err)
			}
		}, want: ErrResearchPreflightBlocked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := openResearchRunConfirmationStore(t)
			confirmation := saveResearchRunConfirmationPreflight(t, store, "")
			test.mutate(t, store, &confirmation)
			if run, created, err := store.ConfirmResearchRun(confirmation); run != nil || created || !errors.Is(err, test.want) {
				t.Fatalf("run=%#v created=%v error=%v want=%v", run, created, err, test.want)
			}
			assertResearchRunCount(t, store, 0)
		})
	}
}

func TestResearchRunRequiresPreflightRejectsExpiryAndParentOwnershipWithoutRunRows(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
		store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		confirmation := saveResearchRunConfirmationPreflight(t, store, "")
		now = now.Add(researchPreflightStoreTTL)
		if _, _, err := store.ConfirmResearchRun(confirmation); !errors.Is(err, ErrResearchPreflightExpired) {
			t.Fatalf("error=%v", err)
		}
		assertResearchRunCount(t, store, 0)
	})
	for _, test := range []struct {
		name, parentOwner string
		want              error
	}{
		{name: "parent missing", want: ErrResearchRunNotFound},
		{name: "parent wrong owner", parentOwner: testResearchPreflightOwnerB, want: ErrResearchRunNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := openResearchRunConfirmationStore(t)
			parent := ResearchRun{RunID: "research-run-missing"}
			wantRows := 0
			if test.parentOwner != "" {
				parent = createResearchRunForTest(t, store, "parent-"+test.name)
				wantRows = 1
			}
			if test.parentOwner != "" {
				if _, err := store.db.Exec(`INSERT INTO research_http_owners(run_id, owner_hash, created_at) VALUES (?, ?, ?)`, parent.RunID, test.parentOwner, parent.CreatedAt); err != nil {
					t.Fatal(err)
				}
			}
			confirmation := saveResearchRunConfirmationPreflight(t, store, parent.RunID)
			if _, _, err := store.ConfirmResearchRun(confirmation); !errors.Is(err, test.want) {
				t.Fatalf("error=%v", err)
			}
			assertResearchRunCount(t, store, wantRows)
		})
	}
}

func TestResearchRunRequiresPreflightBindsOnceReplaysAndInheritsLinks(t *testing.T) {
	store := openResearchRunConfirmationStore(t)
	parent := createResearchRunForTest(t, store, "owned-parent")
	if _, err := store.db.Exec(`INSERT INTO research_http_owners(run_id, owner_hash, created_at) VALUES (?, ?, ?)`, parent.RunID, testResearchPreflightOwnerA, parent.CreatedAt); err != nil {
		t.Fatal(err)
	}
	confirmation := saveResearchRunConfirmationPreflight(t, store, parent.RunID)
	snapshotBefore, err := store.LoadResearchPreflightForOwner(confirmation.Input.Request.PreflightID, testResearchPreflightOwnerA)
	if err != nil {
		t.Fatal(err)
	}

	created, wasCreated, err := store.ConfirmResearchRun(confirmation)
	if err != nil || !wasCreated {
		t.Fatalf("created=%#v wasCreated=%v error=%v", created, wasCreated, err)
	}
	if created.ParentRunID != parent.RunID || created.PreflightID != confirmation.Input.Request.PreflightID {
		t.Fatalf("links=%#v", created)
	}
	replayed, wasCreated, err := store.ConfirmResearchRun(confirmation)
	if err != nil || wasCreated || replayed.RunID != created.RunID {
		t.Fatalf("replay=%#v created=%v error=%v", replayed, wasCreated, err)
	}

	differentKey := confirmation
	differentKey.Input.IdempotencyKey = "different-key"
	if _, _, err := store.ConfirmResearchRun(differentKey); !errors.Is(err, ErrResearchPreflightConsumed) {
		t.Fatalf("different key error=%v", err)
	}
	differentSelection := confirmation
	differentSelection.Input.Request.PackageVersion = "2.0.0"
	differentSelection.SelectedCandidate.PackageVersion = "2.0.0"
	if _, _, err := store.ConfirmResearchRun(differentSelection); !errors.Is(err, ErrResearchRunIdempotencyConflict) {
		t.Fatalf("different selection error=%v", err)
	}
	snapshotAfter, err := store.LoadResearchPreflightForOwner(confirmation.Input.Request.PreflightID, testResearchPreflightOwnerA)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshotAfter, snapshotBefore) {
		t.Fatalf("confirmation mutated or renewed snapshot: before=%#v after=%#v", snapshotBefore, snapshotAfter)
	}
	assertResearchRunCount(t, store, 2)
}

func TestResearchRunRequiresPreflightConcurrentConfirmationReturnsOneRunWithoutRawSQLiteErrors(t *testing.T) {
	store := openResearchRunConfirmationStore(t)
	confirmation := saveResearchRunConfirmationPreflight(t, store, "")
	start := make(chan struct{})
	type result struct {
		run     *ResearchRun
		created bool
		err     error
	}
	results := make(chan result, 2)
	for index := 0; index < 2; index++ {
		go func() {
			<-start
			run, created, err := store.ConfirmResearchRun(confirmation)
			results <- result{run: run, created: created, err: err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	for _, outcome := range []result{first, second} {
		if outcome.err != nil || outcome.run == nil {
			t.Fatalf("raw concurrent outcome=%#v", outcome)
		}
	}
	if first.run.RunID != second.run.RunID || first.created == second.created {
		t.Fatalf("outcomes=%#v %#v", first, second)
	}
	assertResearchRunCount(t, store, 1)
}

func TestResearchRunRequiresPreflightBoundReplaySurvivesExpiryWithoutRenewingSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	confirmation := saveResearchRunConfirmationPreflight(t, store, "")
	created, wasCreated, err := store.ConfirmResearchRun(confirmation)
	if err != nil || !wasCreated {
		t.Fatalf("create=%#v created=%v error=%v", created, wasCreated, err)
	}
	now = now.Add(researchPreflightStoreTTL + time.Second)
	replayed, wasCreated, err := store.ConfirmResearchRun(confirmation)
	if err != nil || wasCreated || replayed.RunID != created.RunID {
		t.Fatalf("expired replay=%#v created=%v error=%v", replayed, wasCreated, err)
	}
}

func TestResearchRunRequiresPreflightConcurrentHandlesReplayWithoutBusyOrUnique(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	storeA, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Close()
	storeB, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()
	confirmation := saveResearchRunConfirmationPreflight(t, storeA, "")
	start := make(chan struct{})
	type result struct {
		run     *ResearchRun
		created bool
		err     error
	}
	results := make(chan result, 2)
	for _, store := range []*ResearchStore{storeA, storeB} {
		go func(current *ResearchStore) {
			<-start
			run, created, err := current.ConfirmResearchRun(confirmation)
			results <- result{run: run, created: created, err: err}
		}(store)
	}
	close(start)
	first, second := <-results, <-results
	for _, outcome := range []result{first, second} {
		if outcome.err != nil || outcome.run == nil {
			t.Fatalf("concurrent handles returned run=%#v created=%v error=%v", outcome.run, outcome.created, outcome.err)
		}
	}
	if first.run.RunID != second.run.RunID || first.created == second.created {
		t.Fatalf("concurrent handle outcomes=%#v %#v", first, second)
	}
	assertResearchRunCount(t, storeA, 1)
}

func openResearchRunConfirmationStore(t *testing.T) *ResearchStore {
	t.Helper()
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func saveResearchRunConfirmationPreflight(t *testing.T, store *ResearchStore, parentRunID string) ResearchRunConfirmation {
	t.Helper()
	request := ResearchPreflightRequest{
		Mode: ResearchModeQuick, Question: "generic confirmation question",
		RequestedSources: []string{ResearchSourceKnowledge}, PackageConstraint: "candidate-a", ParentRunID: parentRunID,
	}
	result := testResearchPreflight()
	result.PreflightID = "research-preflight-confirm-" + strings.ReplaceAll(parentRunID, "research-run-", "")
	result.ParentRunID = parentRunID
	result.Checks = []ResearchPreflightCheck{{Code: "package_ready", Status: ResearchPreflightCheckPass, ResultCode: "validated"}}
	budget := ResearchBudget{MaxIterations: 2, MaxEvidenceItems: 20, MaxQuotedChars: 4000, MaxModelCalls: 2, MaxCostUSD: 1}
	result.Candidates[0].Budget = ResearchPreflightBudget{ResolvedMode: ResearchModeQuick, Limits: budget}
	created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, request, result, researchPreflightStoreTTL)
	if err != nil {
		t.Fatal(err)
	}
	return ResearchRunConfirmation{
		OwnerHash: testResearchPreflightOwnerA,
		Input: ResearchRunInput{
			IdempotencyKey: "confirmation-key",
			Request:        ResearchRunRequest{PreflightID: created.PreflightID, Mode: ResearchModeQuick, Question: request.Question, PackageID: "candidate-a", PackageVersion: "1.0.0", RequestedSources: request.RequestedSources},
			Mode:           ResearchModeQuick, RouteReasons: []string{ResearchRouteExplicitQuick}, Budget: budget,
		},
		SelectedCandidate: result.Candidates[0],
	}
}

func assertResearchRunCount(t *testing.T, store *ResearchStore, want int) {
	t.Helper()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM research_runs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("research_runs rows=%d want=%d", count, want)
	}
}

func TestResearchPreflightStoreCleanupUsesExpiryCoveringIndex(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var indexCount int
	if err := store.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = ?`,
		"idx_research_preflights_expiry").Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatalf("expiry index count = %d, want 1", indexCount)
	}

	rows, err := store.db.Query(`EXPLAIN QUERY PLAN
		SELECT preflight_id FROM research_preflights
		WHERE expires_at <= ? ORDER BY expires_at, preflight_id LIMIT ?`,
		formatResearchPreflightTimestamp(time.Now()), 10)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(details, " | ")
	if !strings.Contains(plan, "USING COVERING INDEX idx_research_preflights_expiry") ||
		strings.Contains(plan, "USE TEMP B-TREE") || strings.Contains(plan, "SCAN research_preflights") {
		t.Fatalf("cleanup query plan = %q", plan)
	}
}

func TestResearchPreflightStoreMigrationRecreatesExpiryIndexWithoutDataLoss(t *testing.T) {
	root := t.TempDir()
	store, err := OpenResearchStore(root, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO research_meta(key, value) VALUES ('migration-probe', 'preserved')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DROP INDEX IF EXISTS idx_research_preflights_expiry`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenResearchStore(root, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var value string
	if err := reopened.db.QueryRow(`SELECT value FROM research_meta WHERE key = 'migration-probe'`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "preserved" {
		t.Fatalf("migration probe = %q", value)
	}
	var indexCount int
	if err := reopened.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = ?`,
		"idx_research_preflights_expiry").Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatalf("recreated expiry index count = %d, want 1", indexCount)
	}
}

func TestResearchPreflightStoreOwnerIsolationAndTypedNotFound(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := testResearchPreflight()
	result.PreflightID = "research-preflight-owner"
	created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, testResearchPreflightOwnerB); loaded != nil || !errors.Is(err, ErrResearchPreflightOwner) {
		t.Fatalf("cross-owner load = %#v, %v", loaded, err)
	}
	if loaded, err := store.LoadResearchPreflightForOwner("research-preflight-missing", testResearchPreflightOwnerA); loaded != nil || !errors.Is(err, ErrResearchPreflightNotFound) {
		t.Fatalf("missing load = %#v, %v", loaded, err)
	}
	if replay, err := store.SaveResearchPreflight(testResearchPreflightOwnerB, testResearchPreflightStoreRequest(), *created, 10*time.Minute); replay != nil || !errors.Is(err, ErrResearchPreflightOwner) {
		t.Fatalf("cross-owner replay = %#v, %v", replay, err)
	}
}

func TestResearchPreflightStoreRequestHashIsCanonicalAndConflictsFailClosed(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	request := testResearchPreflightStoreRequest()
	result := testResearchPreflight()
	result.PreflightID = "research-preflight-request-hash"
	created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, request, result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	reordered := request
	reordered.Question = "  generic question  "
	reordered.RequestedSources = []string{ResearchSourceChatlog, ResearchSourceKnowledge}
	replayed, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, reordered, *created, 10*time.Minute)
	if err != nil || !reflect.DeepEqual(replayed, created) {
		t.Fatalf("canonical replay = %#v, %v", replayed, err)
	}

	changes := []struct {
		name   string
		mutate func(*ResearchPreflightRequest)
	}{
		{name: "question", mutate: func(value *ResearchPreflightRequest) { value.Question = "different generic question" }},
		{name: "mode", mutate: func(value *ResearchPreflightRequest) { value.Mode = ResearchModeQuick }},
		{name: "sources", mutate: func(value *ResearchPreflightRequest) { value.RequestedSources = []string{ResearchSourceKnowledge} }},
		{name: "constraint", mutate: func(value *ResearchPreflightRequest) { value.PackageConstraint = "other-agent" }},
		{name: "parent run", mutate: func(value *ResearchPreflightRequest) { value.ParentRunID = "research-run-other" }},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			changed := request
			change.mutate(&changed)
			if replay, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, changed, *created, 10*time.Minute); replay != nil || !errors.Is(err, ErrResearchPreflightIdempotencyConflict) {
				t.Fatalf("changed replay = %#v, %v", replay, err)
			}
		})
	}
}

func TestResearchPreflightStoreReplayIsImmutable(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := testResearchPreflight()
	result.PreflightID = "research-preflight-replay"
	result.CreatedAt = ""
	result.ExpiresAt = ""
	created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(5 * time.Minute)
	replayed, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.CreatedAt != created.CreatedAt || replayed.ExpiresAt != created.ExpiresAt {
		t.Fatalf("replay changed immutable timestamps: %#v versus %#v", replayed, created)
	}

	changed := result
	changed.Candidates = append([]ResearchPreflightCandidate(nil), result.Candidates...)
	changed.Candidates[0].DisplayName = "Changed display"
	if replay, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), changed, 10*time.Minute); replay != nil || !errors.Is(err, ErrResearchPreflightIdempotencyConflict) {
		t.Fatalf("changed payload replay = %#v, %v", replay, err)
	}
}

func TestResearchPreflightStoreExpiresAtTenMinutesBeforeCleanup(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := testResearchPreflight()
	result.PreflightID = "research-preflight-expiry"
	created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(10 * time.Minute)
	if loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, testResearchPreflightOwnerA); loaded != nil || !errors.Is(err, ErrResearchPreflightExpired) {
		t.Fatalf("expired load = %#v, %v", loaded, err)
	}
	if replay, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute); replay != nil || !errors.Is(err, ErrResearchPreflightExpired) {
		t.Fatalf("expired replay = %#v, %v", replay, err)
	}
	if _, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), testResearchPreflight(), 9*time.Minute); err == nil || !strings.Contains(err.Error(), "ttl") {
		t.Fatalf("non-contract ttl error = %v", err)
	}
}

func TestResearchPreflightStoreRejectsPersistedNonContractTTL(t *testing.T) {
	tests := []struct {
		name string
		ttl  time.Duration
	}{
		{name: "shortened", ttl: 9 * time.Minute},
		{name: "extended by one nanosecond", ttl: 10*time.Minute + time.Nanosecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 8, 19, 12, 0, 0, 123456789, time.UTC)
			store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			result := testResearchPreflight()
			result.PreflightID = "research-preflight-tampered-ttl"
			created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`UPDATE research_preflights SET expires_at = ? WHERE preflight_id = ?`,
				formatResearchPreflightTimestamp(now.Add(test.ttl)), created.PreflightID); err != nil {
				t.Fatal(err)
			}
			if loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, testResearchPreflightOwnerA); loaded != nil || !errors.Is(err, ErrResearchPreflightCorrupt) {
				t.Fatalf("tampered ttl load = %#v, %v", loaded, err)
			}
		})
	}
}

func TestResearchPreflightStoreJSONIsBoundedAndCorruptionFailsClosed(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oversized := testResearchPreflight()
	oversized.PreflightID = "research-preflight-oversized"
	oversized.Candidates[0].DisplayName = strings.Repeat("x", researchPreflightJSONMaxBytes+1)
	if _, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), oversized, 10*time.Minute); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized JSON error = %v", err)
	}

	valid := testResearchPreflight()
	valid.PreflightID = "research-preflight-corrupt"
	created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), valid, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE research_preflights SET candidates_json = ? WHERE preflight_id = ?`, `[{"unknown":true}]`, created.PreflightID); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, testResearchPreflightOwnerA); loaded != nil || !errors.Is(err, ErrResearchPreflightCorrupt) {
		t.Fatalf("corrupt load = %#v, %v", loaded, err)
	}
}

func TestResearchPreflightStoreCleanupIsBounded(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"research-preflight-expired-a", "research-preflight-expired-b"} {
		result := testResearchPreflight()
		result.PreflightID = id
		if _, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(11 * time.Minute)
	live := testResearchPreflight()
	live.PreflightID = "research-preflight-live"
	if _, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), live, 10*time.Minute); err != nil {
		t.Fatal(err)
	}

	deleted, err := store.DeleteExpiredResearchPreflights(1)
	if err != nil || deleted != 1 {
		t.Fatalf("first cleanup = %d, %v", deleted, err)
	}
	deleted, err = store.DeleteExpiredResearchPreflights(researchPreflightCleanupMax)
	if err != nil || deleted != 1 {
		t.Fatalf("second cleanup = %d, %v", deleted, err)
	}
	if _, err := store.LoadResearchPreflightForOwner(live.PreflightID, testResearchPreflightOwnerA); err != nil {
		t.Fatalf("live snapshot removed: %v", err)
	}
	for _, limit := range []int{0, researchPreflightCleanupMax + 1} {
		if _, err := store.DeleteExpiredResearchPreflights(limit); err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("cleanup limit %d error = %v", limit, err)
		}
	}
}

func TestResearchPreflightStoreCleanupPreservesNanosecondExpiryBoundary(t *testing.T) {
	createdAt := time.Date(2026, 8, 19, 12, 0, 0, 123456789, time.UTC)
	expiresAt := createdAt.Add(10 * time.Minute)
	tests := []struct {
		name    string
		now     time.Time
		deleted int
	}{
		{name: "one hundred nanoseconds before expiry", now: expiresAt.Add(-100 * time.Nanosecond), deleted: 0},
		{name: "exactly at expiry", now: expiresAt, deleted: 1},
		{name: "one hundred nanoseconds after expiry", now: expiresAt.Add(100 * time.Nanosecond), deleted: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := createdAt
			store, err := OpenResearchStore(t.TempDir(), func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			result := testResearchPreflight()
			result.PreflightID = "research-preflight-nanosecond-boundary"
			if _, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute); err != nil {
				t.Fatal(err)
			}

			now = test.now
			deleted, err := store.DeleteExpiredResearchPreflights(1)
			if err != nil || deleted != test.deleted {
				t.Fatalf("deleted = %d, want %d: %v", deleted, test.deleted, err)
			}
		})
	}
}

func TestResearchPreflightStoreRejectsUnboundedIdentity(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := testResearchPreflight()
	result.PreflightID = strings.Repeat("p", researchPreflightIDMaxRunes+1)
	if _, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute); err == nil || !strings.Contains(err.Error(), "preflight_id") {
		t.Fatalf("preflight id bound error = %v", err)
	}
	result.PreflightID = "research-preflight-owner-bound"
	if _, err := store.SaveResearchPreflight(strings.Repeat("o", researchPreflightOwnerHashMaxRunes+1), testResearchPreflightStoreRequest(), result, 10*time.Minute); err == nil || !strings.Contains(err.Error(), "owner_hash") {
		t.Fatalf("owner hash bound error = %v", err)
	}
}

func TestResearchPreflightStoreRequiresCanonicalOwnerHash(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	invalid := []string{
		"owner-hash-a",
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("A", 64),
		strings.Repeat("a", 63) + "g",
	}
	for _, ownerHash := range invalid {
		result := testResearchPreflight()
		result.PreflightID = "research-preflight-invalid-owner"
		if _, err := store.SaveResearchPreflight(ownerHash, testResearchPreflightStoreRequest(), result, 10*time.Minute); err == nil || !strings.Contains(err.Error(), "owner_hash") {
			t.Fatalf("invalid owner hash accepted: %q, %v", ownerHash, err)
		}
	}

	result := testResearchPreflight()
	result.PreflightID = "research-preflight-tampered-owner"
	ownerHash := testResearchPreflightOwnerHash("a")
	created, err := store.SaveResearchPreflight(ownerHash, testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE research_preflights SET owner_hash = ? WHERE preflight_id = ?`, strings.Repeat("A", 64), created.PreflightID); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, ownerHash); loaded != nil || !errors.Is(err, ErrResearchPreflightCorrupt) || errors.Is(err, ErrResearchPreflightOwner) {
		t.Fatalf("tampered owner load = %#v, %v", loaded, err)
	}
}

func TestResearchPreflightStoreClassifiesLockedDatabaseAsUnavailable(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`PRAGMA busy_timeout = 20`); err != nil {
		t.Fatal(err)
	}
	locker, err := sql.Open("sqlite3", store.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	locker.SetMaxOpenConns(1)
	connection, err := locker.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(context.Background(), `PRAGMA busy_timeout = 20`); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_, _ = connection.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	result := testResearchPreflight()
	result.PreflightID = "research-preflight-locked"
	created, saveErr := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if created != nil || !errors.Is(saveErr, ErrResearchPreflightUnavailable) {
		t.Fatalf("locked save = %#v, %v", created, saveErr)
	}
	var sqliteErr sqlite3.Error
	if !errors.As(saveErr, &sqliteErr) || (sqliteErr.Code != sqlite3.ErrBusy && sqliteErr.Code != sqlite3.ErrLocked) {
		t.Fatalf("locked save SQLite cause = %#v, %v", sqliteErr, saveErr)
	}
	if _, err := connection.ExecContext(context.Background(), `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	locked = false
	if created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute); err != nil || created == nil {
		t.Fatalf("retry after unlock = %#v, %v", created, err)
	}
}

func TestResearchPreflightStoreRequestHashIsStableAndCanonical(t *testing.T) {
	requestHash, err := hashResearchPreflightRequest(testResearchPreflightStoreRequest())
	if err != nil {
		t.Fatal(err)
	}
	const expected = "sha256:ac59b2ab14219c87c2f7dc6f1545d04423862362e9e379ceeec33ee14f402687"
	if requestHash != expected {
		t.Fatalf("request hash = %q, want %q", requestHash, expected)
	}
	normalized := testResearchPreflightStoreRequest()
	normalized.Question = "  generic question  "
	normalized.RequestedSources = []string{ResearchSourceChatlog, ResearchSourceKnowledge}
	normalizedHash, err := hashResearchPreflightRequest(normalized)
	if err != nil || normalizedHash != expected {
		t.Fatalf("normalized request hash = %q, %v", normalizedHash, err)
	}
}

func TestResearchPreflightStoreRejectsNonCanonicalPersistedRequestHash(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := testResearchPreflight()
	result.PreflightID = "research-preflight-uppercase-hash"
	created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	uppercase := "sha256:" + strings.Repeat("A", 64)
	if _, err := store.db.Exec(`UPDATE research_preflights SET request_hash = ? WHERE preflight_id = ?`, uppercase, created.PreflightID); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, testResearchPreflightOwnerA); loaded != nil || !errors.Is(err, ErrResearchPreflightCorrupt) {
		t.Fatalf("uppercase request hash load = %#v, %v", loaded, err)
	}
}

func TestResearchPreflightStoreFailsClosedWhenRandomIDGenerationFails(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.random = failingResearchPreflightReader{}
	result := testResearchPreflight()
	result.PreflightID = ""
	if created, err := store.SaveResearchPreflight(testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), result, 10*time.Minute); created != nil || err == nil || !strings.Contains(err.Error(), "random") {
		t.Fatalf("random failure save = %#v, %v", created, err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(1) FROM research_preflights`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("random failure persisted %d rows", count)
	}
}

func TestResearchPreflightStoreConcurrentHandlesReturnImmutableReplay(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 123456789, time.UTC)
	root := t.TempDir()
	storeA, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Close()
	storeB, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()
	result := testResearchPreflight()
	result.PreflightID = "research-preflight-concurrent-replay"

	results := saveResearchPreflightConcurrently(t, storeA, storeB, result, result)
	if results[0].err != nil || results[1].err != nil {
		t.Fatalf("concurrent replay errors = %v, %v", results[0].err, results[1].err)
	}
	if results[0].preflight == nil || !reflect.DeepEqual(results[0].preflight, results[1].preflight) {
		t.Fatalf("concurrent replay results = %#v, %#v", results[0].preflight, results[1].preflight)
	}
}

func TestResearchPreflightStoreConcurrentHandlesReturnTypedConflict(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 123456789, time.UTC)
	root := t.TempDir()
	storeA, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Close()
	storeB, err := OpenResearchStore(root, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()
	original := testResearchPreflight()
	original.PreflightID = "research-preflight-concurrent-conflict"
	changed := original
	changed.Candidates = append([]ResearchPreflightCandidate(nil), original.Candidates...)
	changed.Candidates[0].DisplayName = "Changed display"

	results := saveResearchPreflightConcurrently(t, storeA, storeB, original, changed)
	succeeded := 0
	conflicted := 0
	for _, result := range results {
		switch {
		case result.err == nil && result.preflight != nil:
			succeeded++
		case result.preflight == nil && errors.Is(result.err, ErrResearchPreflightIdempotencyConflict):
			conflicted++
		default:
			t.Fatalf("concurrent conflict returned raw result/error: %#v, %v", result.preflight, result.err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent conflict outcomes: succeeded=%d conflicted=%d", succeeded, conflicted)
	}
}

type researchPreflightSaveResult struct {
	preflight *ResearchPreflight
	err       error
}

func saveResearchPreflightConcurrently(
	t *testing.T,
	storeA, storeB *ResearchStore,
	resultA, resultB ResearchPreflight,
) [2]researchPreflightSaveResult {
	t.Helper()
	start := make(chan struct{})
	results := [2]researchPreflightSaveResult{}
	stores := []*ResearchStore{storeA, storeB}
	preflights := []ResearchPreflight{resultA, resultB}
	var wait sync.WaitGroup
	for index := range stores {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index].preflight, results[index].err = stores[index].SaveResearchPreflight(
				testResearchPreflightOwnerA, testResearchPreflightStoreRequest(), preflights[index], 10*time.Minute,
			)
		}(index)
	}
	close(start)
	wait.Wait()
	return results
}

type failingResearchPreflightReader struct{}

func (failingResearchPreflightReader) Read([]byte) (int, error) {
	return 0, errors.New("random unavailable")
}

func testResearchPreflightStoreRequest() ResearchPreflightRequest {
	return ResearchPreflightRequest{
		Mode:              ResearchModeAuto,
		Question:          "generic question",
		RequestedSources:  []string{ResearchSourceKnowledge, ResearchSourceChatlog},
		PackageConstraint: "candidate-a",
		ParentRunID:       "research-run-parent",
	}
}

func testResearchPreflightOwnerHash(digit string) string {
	return strings.Repeat(digit, 64)
}
