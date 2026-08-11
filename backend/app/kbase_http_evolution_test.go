package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestKBaseHTTPHandlerEvolutionReadAPIsRequireAuthentication(t *testing.T) {
	books, evolution := newEvolutionHTTPTestStores(t)
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            books,
		AuthToken:        "consumer-token",
		EvolutionStore:   evolution,
		EvolutionEnabled: true,
	})

	for _, path := range []string{
		"/api/evolution/overview",
		"/api/evolution/runs",
		"/api/evolution/runs/run-missing",
		"/api/evolution/runs/run-missing/events",
	} {
		response := requestKBase(handler, http.MethodGet, path, "")
		if response.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without token status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestKBaseHTTPHandlerEvolutionOverviewIsDenseAndPrivate(t *testing.T) {
	books, evolution := newEvolutionHTTPTestStores(t)
	seedEvolutionHTTPPackages(t, books)
	run := createEvolutionTestRun(t, evolution, "overview-run")
	if _, err := evolution.TransitionRun(run.RunID, EvolutionFailed, EvolutionTransitionInput{
		Actor: "worker", Code: "private_failure", Message: "private sqlite path and stack trace",
		ArtifactRefs: []string{"artifact:sha256:private-internal-hash"},
	}); err != nil {
		t.Fatal(err)
	}
	handler := newEvolutionHTTPTestHandler(books, evolution, true)

	response := requestKBase(handler, http.MethodGet, "/api/evolution/overview", "consumer-token")
	if response.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		OpenRuns   []evolutionRunHTTPView   `json:"open_runs"`
		AgentFleet []evolutionAgentHTTPView `json:"agent_fleet"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.OpenRuns == nil || payload.AgentFleet == nil {
		t.Fatalf("overview arrays must be non-nil: %#v", payload)
	}
	if len(payload.AgentFleet) != 2 {
		t.Fatalf("agent fleet len=%d, want one row for each of 2 Agents", len(payload.AgentFleet))
	}
	body := response.Body.String()
	for _, secret := range []string{
		"private-content-hash", "private-descriptor-hash", "private sqlite path",
		"private-internal-hash", evolution.dbPath,
	} {
		if strings.Contains(body, secret) {
			t.Fatalf("overview leaked %q: %s", secret, body)
		}
	}
}

func TestKBaseHTTPHandlerEvolutionRunsFilterAndPaginate(t *testing.T) {
	books, evolution := newEvolutionHTTPTestStores(t)
	clock := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	evolution.now = func() time.Time { return clock }
	runA := createEvolutionHTTPRun(t, evolution, "filter-a", EvolutionRunCombined, "agent-a", "p1")
	for _, status := range []EvolutionRunStatus{EvolutionTriaged, EvolutionGenerating, EvolutionEvaluating, EvolutionAwaitingApproval} {
		if _, err := evolution.TransitionRun(runA.RunID, status, EvolutionTransitionInput{Actor: "worker", Code: "advance", Message: "safe"}); err != nil {
			t.Fatal(err)
		}
	}
	clock = clock.Add(time.Minute)
	runB := createEvolutionHTTPRun(t, evolution, "filter-b", EvolutionRunAgentPolicy, "agent-b", "p0")
	clock = clock.Add(time.Minute)
	createEvolutionHTTPRun(t, evolution, "filter-c", EvolutionRunKnowledgeRelease, "agent-a", "p2")

	handler := newEvolutionHTTPTestHandler(books, evolution, true)
	for name, query := range map[string]string{
		"status":   "status=awaiting_approval",
		"risk":     "risk=p1",
		"type":     "type=combined",
		"package":  "package=agent-a&status=awaiting_approval",
		"combined": "status=awaiting_approval&risk=p1&type=combined&package=agent-a",
	} {
		t.Run(name, func(t *testing.T) {
			response := requestKBase(handler, http.MethodGet, "/api/evolution/runs?"+query, "consumer-token")
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			page := decodeEvolutionHTTPRunPage(t, response)
			if len(page.Runs) == 0 {
				t.Fatalf("filter %s returned no runs", query)
			}
			for _, got := range page.Runs {
				switch name {
				case "status", "package", "combined":
					if got.RunID != runA.RunID {
						t.Fatalf("run=%s, want %s", got.RunID, runA.RunID)
					}
				case "risk":
					if got.RiskLevel != "p1" {
						t.Fatalf("risk=%s", got.RiskLevel)
					}
				case "type":
					if got.RunType != EvolutionRunCombined {
						t.Fatalf("type=%s", got.RunType)
					}
				}
			}
		})
	}

	first := requestKBase(handler, http.MethodGet, "/api/evolution/runs?limit=1", "consumer-token")
	page1 := decodeEvolutionHTTPRunPage(t, first)
	if len(page1.Runs) != 1 || page1.NextCursor == "" || strings.Contains(page1.NextCursor, page1.Runs[0].RunID) {
		t.Fatalf("first page=%#v", page1)
	}
	second := requestKBase(handler, http.MethodGet, "/api/evolution/runs?limit=1&cursor="+page1.NextCursor, "consumer-token")
	page2 := decodeEvolutionHTTPRunPage(t, second)
	if len(page2.Runs) != 1 || page2.Runs[0].RunID == page1.Runs[0].RunID || page2.Runs[0].RunID != runB.RunID {
		t.Fatalf("second page=%#v after %#v", page2, page1)
	}
}

func TestKBaseHTTPHandlerEvolutionEmptyCollectionsStayArrays(t *testing.T) {
	books, evolution := newEvolutionHTTPTestStores(t)
	handler := newEvolutionHTTPTestHandler(books, evolution, true)

	overview := requestKBase(handler, http.MethodGet, "/api/evolution/overview", "consumer-token")
	if overview.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", overview.Code, overview.Body.String())
	}
	var summary evolutionOverviewHTTPView
	if err := json.Unmarshal(overview.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.OpenRuns == nil || summary.AgentFleet == nil {
		t.Fatalf("overview arrays are nil: %#v", summary)
	}

	runs := requestKBase(handler, http.MethodGet, "/api/evolution/runs", "consumer-token")
	page := decodeEvolutionHTTPRunPage(t, runs)
	if page.Runs == nil {
		t.Fatal("runs array is nil")
	}
}

func TestKBaseHTTPHandlerEvolutionRunDetailAndEventsArePrivate(t *testing.T) {
	books, evolution := newEvolutionHTTPTestStores(t)
	run := createEvolutionTestRun(t, evolution, "detail-run")
	if _, err := evolution.TransitionRun(run.RunID, EvolutionBlocked, EvolutionTransitionInput{
		Actor: "worker", Code: "retry_exhausted", Message: "private database failure detail",
		ArtifactRefs: []string{"artifact:sha256:private-artifact-hash"},
	}); err != nil {
		t.Fatal(err)
	}
	handler := newEvolutionHTTPTestHandler(books, evolution, true)

	detail := requestKBase(handler, http.MethodGet, "/api/evolution/runs/"+run.RunID, "consumer-token")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	events := requestKBase(handler, http.MethodGet, "/api/evolution/runs/"+run.RunID+"/events?limit=1", "consumer-token")
	if events.Code != http.StatusOK {
		t.Fatalf("events status=%d body=%s", events.Code, events.Body.String())
	}
	var page evolutionEventHTTPPage
	if err := json.Unmarshal(events.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.NextCursor == "" {
		t.Fatalf("events page=%#v", page)
	}
	last := requestKBase(handler, http.MethodGet, "/api/evolution/runs/"+run.RunID+"/events?limit=1&cursor="+page.NextCursor, "consumer-token")
	if last.Code != http.StatusOK {
		t.Fatalf("last events status=%d body=%s", last.Code, last.Body.String())
	}
	var lastPage evolutionEventHTTPPage
	if err := json.Unmarshal(last.Body.Bytes(), &lastPage); err != nil {
		t.Fatal(err)
	}
	if lastPage.Events == nil {
		t.Fatal("events array is nil")
	}
	for _, secret := range []string{"private database failure detail", "private-artifact-hash", evolution.dbPath} {
		if strings.Contains(detail.Body.String(), secret) || strings.Contains(events.Body.String(), secret) || strings.Contains(last.Body.String(), secret) {
			t.Fatalf("detail/events leaked %q", secret)
		}
	}
}

func TestKBaseHTTPHandlerEvolutionRejectsInvalidQueriesAndMissingRuns(t *testing.T) {
	books, evolution := newEvolutionHTTPTestStores(t)
	run := createEvolutionTestRun(t, evolution, "validation-run")
	handler := newEvolutionHTTPTestHandler(books, evolution, true)

	tests := []struct {
		path string
		want int
	}{
		{"/api/evolution/runs?limit=0", http.StatusBadRequest},
		{"/api/evolution/runs?limit=201", http.StatusBadRequest},
		{"/api/evolution/runs?limit=1&limit=2", http.StatusBadRequest},
		{"/api/evolution/runs?cursor=not-opaque", http.StatusBadRequest},
		{"/api/evolution/runs?status=%zz", http.StatusBadRequest},
		{"/api/evolution/runs?status=unknown", http.StatusBadRequest},
		{"/api/evolution/runs?risk=unknown", http.StatusBadRequest},
		{"/api/evolution/runs?type=unknown", http.StatusBadRequest},
		{"/api/evolution/runs?package=../private", http.StatusBadRequest},
		{"/api/evolution/runs?unknown=value", http.StatusBadRequest},
		{"/api/evolution/runs/bad%2Fid", http.StatusBadRequest},
		{"/api/evolution/runs/run-missing", http.StatusNotFound},
		{"/api/evolution/runs/run-missing/events", http.StatusNotFound},
		{"/api/evolution/runs/" + run.RunID + "/events?limit=501", http.StatusBadRequest},
		{"/api/evolution/runs/" + run.RunID + "/events?cursor=event-missing", http.StatusBadRequest},
	}
	for _, test := range tests {
		response := requestKBase(handler, http.MethodGet, test.path, "consumer-token")
		if response.Code != test.want {
			t.Errorf("GET %s status=%d want=%d body=%s", test.path, response.Code, test.want, response.Body.String())
		}
	}
}

func TestKBaseHTTPHandlerEvolutionDisabledIsUnavailableWithoutAffectingPackages(t *testing.T) {
	books, _ := newEvolutionHTTPTestStores(t)
	handler := newEvolutionHTTPTestHandler(books, nil, false)

	disabled := requestKBase(handler, http.MethodGet, "/api/evolution/overview", "consumer-token")
	if disabled.Code != http.StatusServiceUnavailable || !strings.Contains(disabled.Body.String(), "evolution control plane is disabled") {
		t.Fatalf("disabled status=%d body=%s", disabled.Code, disabled.Body.String())
	}
	packages := requestKBase(handler, http.MethodGet, "/api/agent-packages", "consumer-token")
	if packages.Code != http.StatusOK {
		t.Fatalf("packages status=%d body=%s", packages.Code, packages.Body.String())
	}
}

func newEvolutionHTTPTestStores(t *testing.T) (*BookKnowledgeStore, *EvolutionControlStore) {
	t.Helper()
	root := t.TempDir()
	books := NewBookKnowledgeStore(root)
	evolution, err := OpenEvolutionControlStore(root, fixedEvolutionStoreClock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = evolution.Close() })
	return books, evolution
}

func newEvolutionHTTPTestHandler(books *BookKnowledgeStore, evolution *EvolutionControlStore, enabled bool) http.Handler {
	return NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: books, AuthToken: "consumer-token", EvolutionStore: evolution, EvolutionEnabled: enabled,
	})
}

func createEvolutionHTTPRun(t *testing.T, store *EvolutionControlStore, key string, runType EvolutionRunType, packageID, risk string) *EvolutionRun {
	t.Helper()
	input := validEvolutionRunInput(key)
	input.RunType = runType
	input.PackageID = packageID
	input.RiskLevel = risk
	if runType == EvolutionRunAgentPolicy {
		input.BaselineReleaseIDs = []string{}
	}
	run, created, err := store.CreateRun(input)
	if err != nil || !created {
		t.Fatalf("CreateRun = %#v, %v, %v", run, created, err)
	}
	return run
}

func seedEvolutionHTTPPackages(t *testing.T, store *BookKnowledgeStore) {
	t.Helper()
	if err := os.MkdirAll(store.AgentPackageDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	manifest := &AgentPackageManifest{Version: agentPackageStoreVersion, Packages: []AgentPackageRecord{
		{PackageID: "agent-a", Version: "1.0.0", ContentHash: "private-content-hash-old", LifecycleState: AgentPackageSuperseded, PublishedAt: "2026-08-10T10:00:00Z", URL: "/agents/agent-a"},
		{PackageID: "agent-a", Version: "1.1.0", ContentHash: "private-content-hash", LifecycleState: AgentPackagePublished, PublishedAt: "2026-08-11T10:00:00Z", URL: "/agents/agent-a", Runtime: &AgentPackageRuntimeDescriptor{DescriptorHash: "private-descriptor-hash"}},
		{PackageID: "agent-b", Version: "2.0.0", ContentHash: "private-content-hash-b", LifecycleState: AgentPackagePublished, PublishedAt: "2026-08-11T11:00:00Z", URL: "/agents/agent-b"},
	}, Idempotency: []AgentPackageIdempotencyRecord{}}
	store.mu.Lock()
	err := store.writeAgentPackageManifestUnlocked(manifest)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}

func decodeEvolutionHTTPRunPage(t *testing.T, response *httptest.ResponseRecorder) evolutionRunHTTPPage {
	t.Helper()
	var page evolutionRunHTTPPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}
