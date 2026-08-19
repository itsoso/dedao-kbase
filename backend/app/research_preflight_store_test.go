package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
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

	created, err := store.SaveResearchPreflight("owner-hash-a", request, result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.PreflightID, "research-preflight-") || created.RequestHash == "" {
		t.Fatalf("created identity = %#v", created)
	}
	if created.CreatedAt != now.Format(time.RFC3339Nano) || created.ExpiresAt != now.Add(10*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("created timestamps = %q to %q", created.CreatedAt, created.ExpiresAt)
	}
	loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, "owner-hash-a")
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
	persisted, err := reopened.LoadResearchPreflightForOwner(created.PreflightID, "owner-hash-a")
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
		"checks_json", "gaps_json", "parent_run_id", "created_at", "expires_at",
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

func TestResearchPreflightStoreOwnerIsolationAndTypedNotFound(t *testing.T) {
	store, err := OpenResearchStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := testResearchPreflight()
	result.PreflightID = "research-preflight-owner"
	created, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, "owner-hash-b"); loaded != nil || !errors.Is(err, ErrResearchPreflightOwner) {
		t.Fatalf("cross-owner load = %#v, %v", loaded, err)
	}
	if loaded, err := store.LoadResearchPreflightForOwner("research-preflight-missing", "owner-hash-a"); loaded != nil || !errors.Is(err, ErrResearchPreflightNotFound) {
		t.Fatalf("missing load = %#v, %v", loaded, err)
	}
	if replay, err := store.SaveResearchPreflight("owner-hash-b", testResearchPreflightStoreRequest(), *created, 10*time.Minute); replay != nil || !errors.Is(err, ErrResearchPreflightOwner) {
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
	created, err := store.SaveResearchPreflight("owner-hash-a", request, result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	reordered := request
	reordered.Question = "  generic question  "
	reordered.RequestedSources = []string{ResearchSourceChatlog, ResearchSourceKnowledge}
	replayed, err := store.SaveResearchPreflight("owner-hash-a", reordered, *created, 10*time.Minute)
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
			if replay, err := store.SaveResearchPreflight("owner-hash-a", changed, *created, 10*time.Minute); replay != nil || !errors.Is(err, ErrResearchPreflightIdempotencyConflict) {
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
	created, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(5 * time.Minute)
	replayed, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.CreatedAt != created.CreatedAt || replayed.ExpiresAt != created.ExpiresAt {
		t.Fatalf("replay changed immutable timestamps: %#v versus %#v", replayed, created)
	}

	changed := result
	changed.Candidates = append([]ResearchPreflightCandidate(nil), result.Candidates...)
	changed.Candidates[0].DisplayName = "Changed display"
	if replay, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), changed, 10*time.Minute); replay != nil || !errors.Is(err, ErrResearchPreflightIdempotencyConflict) {
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
	created, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), result, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(10 * time.Minute)
	if loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, "owner-hash-a"); loaded != nil || !errors.Is(err, ErrResearchPreflightExpired) {
		t.Fatalf("expired load = %#v, %v", loaded, err)
	}
	if replay, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), result, 10*time.Minute); replay != nil || !errors.Is(err, ErrResearchPreflightExpired) {
		t.Fatalf("expired replay = %#v, %v", replay, err)
	}
	if _, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), testResearchPreflight(), 9*time.Minute); err == nil || !strings.Contains(err.Error(), "ttl") {
		t.Fatalf("non-contract ttl error = %v", err)
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
	if _, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), oversized, 10*time.Minute); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized JSON error = %v", err)
	}

	valid := testResearchPreflight()
	valid.PreflightID = "research-preflight-corrupt"
	created, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), valid, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE research_preflights SET candidates_json = ? WHERE preflight_id = ?`, `[{"unknown":true}]`, created.PreflightID); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.LoadResearchPreflightForOwner(created.PreflightID, "owner-hash-a"); loaded != nil || err == nil || !strings.Contains(err.Error(), "persisted research preflight") {
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
		if _, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), result, 10*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(11 * time.Minute)
	live := testResearchPreflight()
	live.PreflightID = "research-preflight-live"
	if _, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), live, 10*time.Minute); err != nil {
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
	if _, err := store.LoadResearchPreflightForOwner(live.PreflightID, "owner-hash-a"); err != nil {
		t.Fatalf("live snapshot removed: %v", err)
	}
	for _, limit := range []int{0, researchPreflightCleanupMax + 1} {
		if _, err := store.DeleteExpiredResearchPreflights(limit); err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("cleanup limit %d error = %v", limit, err)
		}
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
	if _, err := store.SaveResearchPreflight("owner-hash-a", testResearchPreflightStoreRequest(), result, 10*time.Minute); err == nil || !strings.Contains(err.Error(), "preflight_id") {
		t.Fatalf("preflight id bound error = %v", err)
	}
	result.PreflightID = "research-preflight-owner-bound"
	if _, err := store.SaveResearchPreflight(strings.Repeat("o", researchPreflightOwnerHashMaxRunes+1), testResearchPreflightStoreRequest(), result, 10*time.Minute); err == nil || !strings.Contains(err.Error(), "owner_hash") {
		t.Fatalf("owner hash bound error = %v", err)
	}
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
