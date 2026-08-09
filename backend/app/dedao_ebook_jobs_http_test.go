package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
)

func TestKBaseHTTPHandlerBookJobQueuedOnly(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	provider := &fakeDedaoEbookAcquisition{detail: &services.EbookDetail{ID: 42, Enid: "owned-enid", IsBuy: true}}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token", DedaoEbooks: provider,
	})

	unauthorized := requestJSONKBase(handler, http.MethodPost, "/api/jobs", "", `{"type":"dedao_ebook_download","ebook_id":42,"ebook_enid":"owned-enid","download_type":1}`)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized create = %d", unauthorized.Code)
	}
	if jobs, _ := store.ListBookKnowledgeJobs(10); len(jobs) != 0 {
		t.Fatalf("unauthorized request created jobs: %#v", jobs)
	}

	oldRunner := runDedaoEbookDownloadJob
	var runnerCalls atomic.Int32
	runDedaoEbookDownloadJob = func(ctx context.Context, job BookKnowledgeJob) (map[string]any, error) {
		runnerCalls.Add(1)
		return map[string]any{"ebook_id": job.EbookID, "title": "测试书"}, nil
	}
	defer func() { runDedaoEbookDownloadJob = oldRunner }()

	created := requestJSONKBase(handler, http.MethodPost, "/api/jobs", "secret-token", `{"type":"dedao_ebook_download","ebook_id":42,"ebook_enid":"owned-enid","download_type":1}`)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body=%s", created.Code, created.Body.String())
	}
	if provider.gotDetailEnid != "owned-enid" {
		t.Fatalf("ownership detail enid = %q", provider.gotDetailEnid)
	}
	if provider.gotDetailCtx == nil {
		t.Fatal("ownership detail did not receive request context")
	}
	if _, ok := provider.gotDetailCtx.Deadline(); !ok {
		t.Fatal("ownership detail context has no verification deadline")
	}
	var payload struct {
		Job BookKnowledgeJob `json:"job"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil || payload.Job.ID == "" {
		t.Fatalf("created payload=%#v err=%v", payload, err)
	}
	if payload.Job.Status != BookKnowledgeJobStatusQueued || payload.Job.Stage != "queued" {
		t.Fatalf("created job = %#v, want queued", payload.Job)
	}
	if strings.Contains(created.Body.String(), "lease_owner") || strings.Contains(created.Body.String(), "lease_expires_at") {
		t.Fatalf("create response exposed lease fields: %s", created.Body.String())
	}
	time.Sleep(50 * time.Millisecond)
	if got := runnerCalls.Load(); got != 0 {
		t.Fatalf("executor calls after response = %d, want 0", got)
	}

	get := requestKBase(handler, http.MethodGet, "/api/jobs/"+payload.Job.ID, "secret-token")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"status":"queued"`) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	list := requestKBase(handler, http.MethodGet, "/api/jobs?limit=10", "secret-token")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), payload.Job.ID) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	for _, forbidden := range []string{"/srv/", "html_path", "output_dir", "book_knowledge_root", "repo_dir"} {
		if strings.Contains(get.Body.String(), forbidden) || strings.Contains(list.Body.String(), forbidden) {
			t.Fatalf("job response leaked %q: get=%s list=%s", forbidden, get.Body.String(), list.Body.String())
		}
	}
}

func TestKBaseHTTPHandlerBookJobResponsesUseSafeViews(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	runningSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 41, EbookEnID: "running-enid", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.ClaimNextBookKnowledgeJob("private-worker-owner", time.Hour)
	if err != nil || running == nil || running.ID != runningSource.ID {
		t.Fatalf("running=%#v err=%v", running, err)
	}

	safeSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 42, EbookEnID: "safe-enid", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("safe-worker", time.Hour); err != nil {
		t.Fatal(err)
	}
	safeSucceeded, err := store.CompleteBookKnowledgeJob(safeSource.ID, "safe-worker", map[string]any{
		"ebook_id": safeSource.EbookID, "ebook_enid": safeSource.EbookEnID,
		"download_type": safeSource.DownloadType, "knowledge_book_id": "book-42", "title": "安全标题",
	})
	if err != nil {
		t.Fatal(err)
	}

	unsafeSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 43, EbookEnID: "unsafe-enid", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("unsafe-worker", time.Hour); err != nil {
		t.Fatal(err)
	}
	unsafeSucceeded, err := store.CompleteBookKnowledgeJob(unsafeSource.ID, "unsafe-worker", map[string]any{
		"ebook_id": unsafeSource.EbookID, "ebook_enid": unsafeSource.EbookEnID,
		"download_type": unsafeSource.DownloadType, "knowledge_book_id": "token=private-value", "title": "/srv/private/title",
	})
	if err != nil {
		t.Fatal(err)
	}

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})
	for _, response := range []*httptest.ResponseRecorder{
		requestKBase(handler, http.MethodGet, "/api/jobs/"+running.ID, "secret-token"),
		requestKBase(handler, http.MethodGet, "/api/jobs/"+safeSucceeded.ID, "secret-token"),
		requestKBase(handler, http.MethodGet, "/api/jobs/"+unsafeSucceeded.ID, "secret-token"),
		requestKBase(handler, http.MethodGet, "/api/jobs?limit=10", "secret-token"),
	} {
		if response.Code != http.StatusOK {
			t.Fatalf("job view status=%d body=%s", response.Code, response.Body.String())
		}
		for _, forbidden := range []string{"lease_owner", "lease_expires_at", "private-worker-owner", "token=private-value", "/srv/private/title"} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Fatalf("job view leaked %q: %s", forbidden, response.Body.String())
			}
		}
	}

	safeGet := requestKBase(handler, http.MethodGet, "/api/jobs/"+safeSucceeded.ID, "secret-token")
	for _, expected := range []string{`"ebook_id":42`, `"ebook_enid":"safe-enid"`, `"knowledge_book_id":"book-42"`, `"title":"安全标题"`} {
		if !strings.Contains(safeGet.Body.String(), expected) {
			t.Fatalf("safe job view missing %s: %s", expected, safeGet.Body.String())
		}
	}
	unsafeGet := requestKBase(handler, http.MethodGet, "/api/jobs/"+unsafeSucceeded.ID, "secret-token")
	if !strings.Contains(unsafeGet.Body.String(), `"ebook_id":43`) {
		t.Fatalf("unsafe result filtering removed safe ID: %s", unsafeGet.Body.String())
	}
}

func TestKBaseHTTPHandlerBookJobQueuedOnlyRejectsTrailingJSON(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	provider := &fakeDedaoEbookAcquisition{detail: &services.EbookDetail{ID: 42, Enid: "owned-enid", IsBuy: true}}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token", DedaoEbooks: provider,
	})
	response := requestJSONKBase(
		handler,
		http.MethodPost,
		"/api/jobs",
		"secret-token",
		`{"type":"dedao_ebook_download","ebook_id":42,"ebook_enid":"owned-enid","download_type":1}{"private":"value"}`,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status=%d body=%s", response.Code, response.Body.String())
	}
	if provider.gotDetailEnid != "" {
		t.Fatalf("trailing JSON called provider with %q", provider.gotDetailEnid)
	}
	if jobs, err := store.ListBookKnowledgeJobs(10); err != nil || len(jobs) != 0 {
		t.Fatalf("trailing JSON created jobs=%#v err=%v", jobs, err)
	}
}

func TestKBaseHTTPHandlerBookJobVerificationTimeout(t *testing.T) {
	for _, test := range []struct {
		name        string
		preparePath func(*testing.T, *BookKnowledgeStore) string
	}{
		{
			name: "create",
			preparePath: func(*testing.T, *BookKnowledgeStore) string {
				return "/api/jobs"
			},
		},
		{
			name: "retry",
			preparePath: func(t *testing.T, store *BookKnowledgeStore) string {
				original := createBookKnowledgeJobInStatus(t, store, BookKnowledgeJobStatusFailed)
				return "/api/jobs/" + original.ID + "/retry"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			provider := &blockingDedaoEbookAcquisition{}
			handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
				Store: store, AuthToken: "secret-token", DedaoEbooks: provider,
				DedaoEbookVerifyTimeout: 20 * time.Millisecond,
			})
			path := test.preparePath(t, store)
			beforeJobs, err := store.ListBookKnowledgeJobs(10)
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			var response *httptest.ResponseRecorder
			if test.name == "create" {
				response = requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"type":"dedao_ebook_download","ebook_id":42,"ebook_enid":"ebook-enid","download_type":1}`)
			} else {
				response = requestKBase(handler, http.MethodPost, path, "secret-token")
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("verification timeout took %s", elapsed)
			}
			if response.Code != http.StatusGatewayTimeout || !strings.Contains(response.Body.String(), "dedao ebook verification timed out") {
				t.Fatalf("verification timeout status=%d body=%s", response.Code, response.Body.String())
			}
			if got := provider.contextCalls.Load(); got != 1 {
				t.Fatalf("context-aware detail calls=%d, want 1", got)
			}
			afterJobs, err := store.ListBookKnowledgeJobs(10)
			if err != nil || len(afterJobs) != len(beforeJobs) {
				t.Fatalf("verification timeout changed jobs: before=%#v after=%#v err=%v", beforeJobs, afterJobs, err)
			}
			assertDedaoEbookResponseOmitsSecrets(t, response.Body.String())
		})
	}
}

func TestKBaseHTTPHandlerBookJobRetryAuthorizationAndEligibleStates(t *testing.T) {
	for _, status := range []BookKnowledgeJobStatus{BookKnowledgeJobStatusFailed, BookKnowledgeJobStatusInterrupted} {
		t.Run(string(status), func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			original := createBookKnowledgeJobInStatus(t, store, status)
			provider := &fakeDedaoEbookAcquisition{detail: &services.EbookDetail{
				ID: original.EbookID, Enid: original.EbookEnID, IsBuy: true,
			}}
			handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
				Store: store, AuthToken: "secret-token", DedaoEbooks: provider,
			})

			unauthorized := requestKBase(handler, http.MethodPost, "/api/jobs/"+original.ID+"/retry", "")
			if unauthorized.Code != http.StatusUnauthorized {
				t.Fatalf("unauthorized retry status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
			}
			if provider.gotDetailEnid != "" {
				t.Fatalf("unauthorized retry called provider with %q", provider.gotDetailEnid)
			}

			response := requestKBase(handler, http.MethodPost, "/api/jobs/"+original.ID+"/retry", "secret-token")
			if response.Code != http.StatusCreated {
				t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
			}
			var payload struct {
				Job BookKnowledgeJob `json:"job"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Job.ID == "" || payload.Job.ID == original.ID || payload.Job.RetryOf != original.ID ||
				payload.Job.Status != BookKnowledgeJobStatusQueued || payload.Job.LeaseOwner != "" || payload.Job.LeaseExpiresAt != "" {
				t.Fatalf("retry job=%#v original=%#v", payload.Job, original)
			}
			if strings.Contains(response.Body.String(), "lease_owner") || strings.Contains(response.Body.String(), "lease_expires_at") {
				t.Fatalf("retry response exposed lease fields: %s", response.Body.String())
			}
			if provider.gotDetailEnid != original.EbookEnID || provider.gotDetailSvc == nil || provider.gotDetailCtx == nil {
				t.Fatalf("authoritative detail call enid=%q service=%p context=%v", provider.gotDetailEnid, provider.gotDetailSvc, provider.gotDetailCtx)
			}
			unchanged, err := store.LoadBookKnowledgeJob(original.ID)
			if err != nil || !reflect.DeepEqual(unchanged, original) {
				t.Fatalf("original changed: %#v err=%v", unchanged, err)
			}
		})
	}
}

func TestKBaseHTTPHandlerBookJobRetryRejectsIneligibleBeforeProvider(t *testing.T) {
	for _, status := range []BookKnowledgeJobStatus{
		BookKnowledgeJobStatusQueued, BookKnowledgeJobStatusRunning, BookKnowledgeJobStatusSucceeded,
	} {
		t.Run(string(status), func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			original := createBookKnowledgeJobInStatus(t, store, status)
			provider := &fakeDedaoEbookAcquisition{detail: &services.EbookDetail{
				ID: original.EbookID, Enid: original.EbookEnID, IsBuy: true,
			}}
			handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
				Store: store, AuthToken: "secret-token", DedaoEbooks: provider,
			})
			response := requestKBase(handler, http.MethodPost, "/api/jobs/"+original.ID+"/retry", "secret-token")
			if response.Code != http.StatusConflict {
				t.Fatalf("retry %s status=%d body=%s", status, response.Code, response.Body.String())
			}
			if provider.gotDetailEnid != "" {
				t.Fatalf("retry %s called provider with %q", status, provider.gotDetailEnid)
			}
		})
	}
}

func TestKBaseHTTPHandlerBookJobRetryRevalidatesIdentityAndOwnershipSafely(t *testing.T) {
	privateError := errors.New("provider CookieStr=private-cookie sqlite=/srv/private/book_jobs.sqlite3")
	tests := []struct {
		name       string
		detail     *services.EbookDetail
		detailErr  error
		wantStatus int
		wantError  string
	}{
		{name: "provider error", detailErr: privateError, wantStatus: http.StatusBadGateway, wantError: "failed to verify dedao ebook ownership"},
		{name: "not found", wantStatus: http.StatusNotFound, wantError: "ebook not found"},
		{name: "invalid identity", detail: &services.EbookDetail{Enid: "ebook-enid", IsBuy: true}, wantStatus: http.StatusBadGateway, wantError: "unable to verify dedao ebook identity"},
		{name: "identity mismatch", detail: &services.EbookDetail{ID: 99, Enid: "other-enid", IsBuy: true}, wantStatus: http.StatusConflict, wantError: "ebook identity no longer matches original job"},
		{name: "unowned", detail: &services.EbookDetail{ID: 42, Enid: "ebook-enid"}, wantStatus: http.StatusForbidden, wantError: "ebook is not owned or on the active bookshelf"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			original := createBookKnowledgeJobInStatus(t, store, BookKnowledgeJobStatusFailed)
			provider := &fakeDedaoEbookAcquisition{detail: test.detail, detailError: test.detailErr}
			handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
				Store: store, AuthToken: "secret-token", DedaoEbooks: provider,
			})
			response := requestKBase(handler, http.MethodPost, "/api/jobs/"+original.ID+"/retry", "secret-token")
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantError) {
				t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
			}
			assertDedaoEbookResponseOmitsSecrets(t, response.Body.String())
			for _, secret := range []string{"sqlite", "/srv/", "private-cookie"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("retry response leaked %q: %s", secret, response.Body.String())
				}
			}
			jobs, err := store.ListBookKnowledgeJobs(10)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("validation failure created retry: jobs=%#v err=%v", jobs, err)
			}
			unchanged, err := store.LoadBookKnowledgeJob(original.ID)
			if err != nil || !reflect.DeepEqual(unchanged, original) {
				t.Fatalf("validation failure changed original: %#v err=%v", unchanged, err)
			}
		})
	}
}

func TestKBaseHTTPHandlerBookJobRetryRejectsActiveDuplicateWithoutLeak(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	original := createBookKnowledgeJobInStatus(t, store, BookKnowledgeJobStatusFailed)
	provider := &fakeDedaoEbookAcquisition{detail: &services.EbookDetail{
		ID: original.EbookID, Enid: original.EbookEnID, IsBuy: true,
	}}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token", DedaoEbooks: provider,
	})
	first := requestKBase(handler, http.MethodPost, "/api/jobs/"+original.ID+"/retry", "secret-token")
	if first.Code != http.StatusCreated {
		t.Fatalf("first retry status=%d body=%s", first.Code, first.Body.String())
	}
	var firstPayload struct {
		Job BookKnowledgeJob `json:"job"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstPayload); err != nil {
		t.Fatal(err)
	}
	duplicate := requestKBase(handler, http.MethodPost, "/api/jobs/"+original.ID+"/retry", "secret-token")
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "an active retry is already queued or running") {
		t.Fatalf("duplicate retry status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	if strings.Contains(duplicate.Body.String(), firstPayload.Job.ID) || strings.Contains(duplicate.Body.String(), "SQL") {
		t.Fatalf("duplicate retry leaked internal detail: %s", duplicate.Body.String())
	}
}

func TestKBaseHTTPHandlerBookJobRetryRoutesAreExact(t *testing.T) {
	for _, path := range []string{
		"/api/jobs/job%2Fpart", "/api/jobs/job%2Fpart/retry", "/api/jobs/job%zz", "/api/jobs//retry", "/api/jobs/job/",
	} {
		if jobID, action, ok := parseBookKnowledgeJobRoute(path); ok {
			t.Fatalf("parseBookKnowledgeJobRoute(%q)=(%q,%q,true), want false", path, jobID, action)
		}
	}
	if jobID, action, ok := parseBookKnowledgeJobRoute("/api/jobs/job%252Fpart/retry"); !ok || jobID != "job%2Fpart" || action != "retry" {
		t.Fatalf("double-encoded parser=(%q,%q,%v)", jobID, action, ok)
	}

	store := NewBookKnowledgeStore(t.TempDir())
	original := createBookKnowledgeJobInStatus(t, store, BookKnowledgeJobStatusFailed)
	provider := &fakeDedaoEbookAcquisition{detail: &services.EbookDetail{
		ID: original.EbookID, Enid: original.EbookEnID, IsBuy: true,
	}}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token", DedaoEbooks: provider,
	})
	escapedID := "%6A" + strings.TrimPrefix(original.ID, "j")
	getEscaped := requestKBase(handler, http.MethodGet, "/api/jobs/"+escapedID, "secret-token")
	if getEscaped.Code != http.StatusOK || !strings.Contains(getEscaped.Body.String(), original.ID) {
		t.Fatalf("GET single-encoded id status=%d body=%s", getEscaped.Code, getEscaped.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodGet, "/api/jobs/"+original.ID+"/retry", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET retry status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
	unknown := requestKBase(handler, http.MethodPost, "/api/jobs/"+original.ID+"/unknown", "secret-token")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown action status=%d body=%s", unknown.Code, unknown.Body.String())
	}
	extra := requestKBase(handler, http.MethodPost, "/api/jobs/"+original.ID+"/retry/extra", "secret-token")
	if extra.Code != http.StatusNotFound {
		t.Fatalf("extra action status=%d body=%s", extra.Code, extra.Body.String())
	}
	missing := requestKBase(handler, http.MethodPost, "/api/jobs/missing/retry", "secret-token")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "job not found") {
		t.Fatalf("missing retry status=%d body=%s", missing.Code, missing.Body.String())
	}
	if provider.gotDetailEnid != "" {
		t.Fatalf("invalid routes called provider with %q", provider.gotDetailEnid)
	}
	retryEscaped := requestKBase(handler, http.MethodPost, "/api/jobs/"+escapedID+"/retry", "secret-token")
	if retryEscaped.Code != http.StatusCreated {
		t.Fatalf("POST retry single-encoded id status=%d body=%s", retryEscaped.Code, retryEscaped.Body.String())
	}
	if provider.detailCalls != 1 {
		t.Fatalf("valid retry detail calls=%d, want 1", provider.detailCalls)
	}

	doubleEncodedID := "%256A" + strings.TrimPrefix(original.ID, "j")
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/jobs/" + doubleEncodedID},
		{method: http.MethodPost, path: "/api/jobs/" + doubleEncodedID + "/retry"},
		{method: http.MethodGet, path: "/api/jobs/" + original.ID + "%2Fextra"},
		{method: http.MethodPost, path: "/api/jobs/" + original.ID + "%2Fextra/retry"},
		{method: http.MethodGet, path: "/api/jobs/" + original.ID + "/"},
	} {
		response := requestKBase(handler, test.method, test.path, "secret-token")
		if response.Code != http.StatusNotFound {
			t.Fatalf("invalid encoded route %s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if provider.detailCalls != 1 {
		t.Fatalf("invalid encoded routes called provider: calls=%d", provider.detailCalls)
	}
}

func createBookKnowledgeJobInStatus(
	t *testing.T,
	store *BookKnowledgeStore,
	status BookKnowledgeJobStatus,
) BookKnowledgeJob {
	t.Helper()
	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 42, EbookEnID: "ebook-enid", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status == BookKnowledgeJobStatusQueued {
		return job
	}
	claimed, err := store.ClaimNextBookKnowledgeJob("test-worker", time.Hour)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("claim job=%#v err=%v", claimed, err)
	}
	switch status {
	case BookKnowledgeJobStatusRunning:
		return *claimed
	case BookKnowledgeJobStatusSucceeded:
		job, err = store.CompleteBookKnowledgeJob(job.ID, "test-worker", map[string]any{"ebook_id": job.EbookID})
	case BookKnowledgeJobStatusFailed:
		job, err = store.FailBookKnowledgeJob(job.ID, "test-worker", BookKnowledgeJobFailureUnknownFailure)
	case BookKnowledgeJobStatusInterrupted:
		job, err = store.InterruptBookKnowledgeJob(job.ID, "test-worker")
	default:
		t.Fatalf("unsupported test status %q", status)
	}
	if err != nil {
		t.Fatal(err)
	}
	return job
}

type blockingDedaoEbookAcquisition struct {
	contextCalls atomic.Int32
}

func (*blockingDedaoEbookAcquisition) SearchEbooks(string, int, int) (DedaoEbookPage, error) {
	return DedaoEbookPage{}, nil
}

func (*blockingDedaoEbookAcquisition) AddEbookToBookshelf(string) (DedaoEbook, error) {
	return DedaoEbook{}, nil
}

func (provider *blockingDedaoEbookAcquisition) EbookDetailContext(ctx context.Context, _ string) (*services.EbookDetail, error) {
	provider.contextCalls.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (provider *blockingDedaoEbookAcquisition) EbookDetailWithServiceContext(ctx context.Context, _ *services.Service, enid string) (*services.EbookDetail, error) {
	return provider.EbookDetailContext(ctx, enid)
}

func TestKBaseHTTPHandlerRejectsUnownedDedaoEbookJob(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	provider := &fakeDedaoEbookAcquisition{detail: &services.EbookDetail{ID: 42, Enid: "unowned-enid"}}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token", DedaoEbooks: provider})

	resp := requestJSONKBase(handler, http.MethodPost, "/api/jobs", "secret-token", `{"type":"dedao_ebook_sync_kbase","ebook_id":42,"ebook_enid":"unowned-enid"}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("unowned status=%d body=%s", resp.Code, resp.Body.String())
	}
	if jobs, _ := store.ListBookKnowledgeJobs(10); len(jobs) != 0 {
		t.Fatalf("unowned request created jobs: %#v", jobs)
	}
}

func TestKBaseHTTPHandlerRequiresExactDedaoEbookIdentity(t *testing.T) {
	for _, test := range []struct {
		name       string
		detail     *services.EbookDetail
		wantStatus int
	}{
		{name: "missing authoritative id", detail: &services.EbookDetail{Enid: "owned-enid", IsBuy: true}, wantStatus: http.StatusBadGateway},
		{name: "mismatched authoritative enid", detail: &services.EbookDetail{ID: 42, Enid: "other-enid", IsBuy: true}, wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
				Store: store, AuthToken: "secret-token", DedaoEbooks: &fakeDedaoEbookAcquisition{detail: test.detail},
			})
			resp := requestJSONKBase(handler, http.MethodPost, "/api/jobs", "secret-token", `{"type":"dedao_ebook_download","ebook_id":42,"ebook_enid":"owned-enid","download_type":1}`)
			if resp.Code != test.wantStatus {
				t.Fatalf("identity status=%d body=%s", resp.Code, resp.Body.String())
			}
			if jobs, _ := store.ListBookKnowledgeJobs(10); len(jobs) != 0 {
				t.Fatalf("identity mismatch created jobs: %#v", jobs)
			}
		})
	}
}

func TestKBaseHTTPHandlerSanitizesDedaoJobErrors(t *testing.T) {
	privateError := errors.New("private-cookie /srv/private/config.json")
	detailFailure := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "secret-token",
		DedaoEbooks: &fakeDedaoEbookAcquisition{detailError: privateError},
	})
	detailResp := requestJSONKBase(detailFailure, http.MethodPost, "/api/jobs", "secret-token", `{"type":"dedao_ebook_download","ebook_id":42,"ebook_enid":"owned-enid","download_type":1}`)
	if detailResp.Code != http.StatusBadGateway || !strings.Contains(detailResp.Body.String(), "failed to verify dedao ebook ownership") {
		t.Fatalf("detail error status=%d body=%s", detailResp.Code, detailResp.Body.String())
	}
	assertDedaoEbookResponseOmitsSecrets(t, detailResp.Body.String())

	blockedRoot := filepath.Join(t.TempDir(), "blocked-root")
	if err := os.WriteFile(blockedRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	blockedStore := NewBookKnowledgeStore(blockedRoot)
	blockedHandler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: blockedStore, AuthToken: "secret-token",
		DedaoEbooks: &fakeDedaoEbookAcquisition{detail: &services.EbookDetail{ID: 42, Enid: "owned-enid", IsBuy: true}},
	})
	listResp := requestKBase(blockedHandler, http.MethodGet, "/api/jobs", "secret-token")
	if listResp.Code != http.StatusInternalServerError || !strings.Contains(listResp.Body.String(), "failed to list jobs") || strings.Contains(listResp.Body.String(), blockedRoot) {
		t.Fatalf("list error status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	createResp := requestJSONKBase(blockedHandler, http.MethodPost, "/api/jobs", "secret-token", `{"type":"dedao_ebook_download","ebook_id":42,"ebook_enid":"owned-enid","download_type":1}`)
	if createResp.Code != http.StatusInternalServerError || !strings.Contains(createResp.Body.String(), "failed to create job") || strings.Contains(createResp.Body.String(), blockedRoot) {
		t.Fatalf("create error status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	missingResp := requestKBase(blockedHandler, http.MethodGet, "/api/jobs/missing", "secret-token")
	if missingResp.Code != http.StatusInternalServerError || !strings.Contains(missingResp.Body.String(), "failed to load job") || strings.Contains(missingResp.Body.String(), blockedRoot) {
		t.Fatalf("get error status=%d body=%s", missingResp.Code, missingResp.Body.String())
	}
	retryResp := requestKBase(blockedHandler, http.MethodPost, "/api/jobs/missing/retry", "secret-token")
	if retryResp.Code != http.StatusInternalServerError || !strings.Contains(retryResp.Body.String(), "failed to load job") || strings.Contains(retryResp.Body.String(), blockedRoot) {
		t.Fatalf("retry load error status=%d body=%s", retryResp.Code, retryResp.Body.String())
	}
}
