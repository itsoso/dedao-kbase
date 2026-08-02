package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
)

func TestKBaseHTTPHandlerAuthorizesOwnedDedaoEbookJobs(t *testing.T) {
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
	done := make(chan struct{})
	runDedaoEbookDownloadJob = func(ctx context.Context, job BookKnowledgeJob) (map[string]any, error) {
		if provider.gotDetailSvc == nil || dedaoServiceFromContext(ctx) != provider.gotDetailSvc {
			t.Errorf("job service did not match ownership verification service")
		}
		close(done)
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
	var payload struct {
		Job BookKnowledgeJob `json:"job"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil || payload.Job.ID == "" {
		t.Fatalf("created payload=%#v err=%v", payload, err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("job runner did not start")
	}
	for i := 0; i < 100; i++ {
		job, _ := store.LoadBookKnowledgeJob(payload.Job.ID)
		if job.Status == BookKnowledgeJobStatusSucceeded {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	get := requestKBase(handler, http.MethodGet, "/api/jobs/"+payload.Job.ID, "secret-token")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"status":"succeeded"`) {
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
	if missingResp.Code != http.StatusNotFound || strings.Contains(missingResp.Body.String(), blockedRoot) {
		t.Fatalf("get error status=%d body=%s", missingResp.Code, missingResp.Body.String())
	}
}
