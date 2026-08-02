package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yann0917/dedao-gui/backend/services"
)

func TestBookKnowledgeJobPersistsValidDedaoEbookRequests(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())

	download, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 67929, EbookEnID: "ebook-enid", DownloadType: 2,
	})
	if err != nil {
		t.Fatalf("create download job: %v", err)
	}
	if download.Status != BookKnowledgeJobStatusQueued || download.DownloadType != 2 || download.ID == "" {
		t.Fatalf("download job = %#v", download)
	}

	syncJob, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookSyncKBase, EbookID: 67929, EbookEnID: "ebook-enid", DownloadType: 3,
	})
	if err != nil {
		t.Fatalf("create sync job: %v", err)
	}
	if syncJob.DownloadType != 1 {
		t.Fatalf("sync download type = %d, want 1", syncJob.DownloadType)
	}

	loaded, err := store.LoadBookKnowledgeJob(download.ID)
	if err != nil || loaded.EbookEnID != "ebook-enid" {
		t.Fatalf("loaded job = %#v, err=%v", loaded, err)
	}
	jobs, err := store.ListBookKnowledgeJobs(10)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs = %#v, err=%v", jobs, err)
	}
}

func TestBookKnowledgeJobRejectsInvalidDedaoEbookRequests(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	for _, test := range []struct {
		name    string
		request BookKnowledgeJobRequest
		want    string
	}{
		{name: "unsupported type", request: BookKnowledgeJobRequest{Type: "other"}, want: "unsupported job type"},
		{name: "missing id", request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookEnID: "enid"}, want: "ebook_id is required"},
		{name: "missing enid", request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoEbookSyncKBase, EbookID: 42}, want: "ebook_enid is required"},
		{name: "invalid format", request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 42, EbookEnID: "enid", DownloadType: 9}, want: "download_type must be 1, 2, or 3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.CreateBookKnowledgeJob(test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBookKnowledgeJobTransitionsToSucceededAndFailed(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldRunner := runDedaoEbookDownloadJob
	defer func() { runDedaoEbookDownloadJob = oldRunner }()

	runDedaoEbookDownloadJob = func(_ context.Context, job BookKnowledgeJob) (map[string]any, error) {
		return map[string]any{"ebook_id": job.EbookID, "title": "测试书"}, nil
	}
	succeeded, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 42, EbookEnID: "enid", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.RunBookKnowledgeJob(succeeded.ID)
	loaded, err := store.LoadBookKnowledgeJob(succeeded.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusSucceeded || loaded.StartedAt == "" || loaded.FinishedAt == "" {
		t.Fatalf("succeeded job = %#v, err=%v", loaded, err)
	}

	runDedaoEbookDownloadJob = func(context.Context, BookKnowledgeJob) (map[string]any, error) {
		return nil, errors.New("private path /srv/private/download failed")
	}
	failed, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 43, EbookEnID: "enid-2", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.RunBookKnowledgeJob(failed.ID)
	loaded, err = store.LoadBookKnowledgeJob(failed.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusFailed || loaded.Error == "" {
		t.Fatalf("failed job = %#v, err=%v", loaded, err)
	}
	if strings.Contains(loaded.Error, "/srv/private") {
		t.Fatalf("failed job leaked path: %q", loaded.Error)
	}
}

func TestBookKnowledgeJobRecoversExecutorPanic(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldRunner := runDedaoEbookDownloadJob
	runDedaoEbookDownloadJob = func(context.Context, BookKnowledgeJob) (map[string]any, error) {
		panic("malformed upstream ebook payload")
	}
	defer func() { runDedaoEbookDownloadJob = oldRunner }()

	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 45, EbookEnID: "panic-enid", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.RunBookKnowledgeJob(job.ID)

	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusFailed || loaded.FinishedAt == "" {
		t.Fatalf("panic job = %#v, err=%v", loaded, err)
	}
	if loaded.Error != "job execution failed" {
		t.Fatalf("panic error = %q, want sanitized failure", loaded.Error)
	}
}

func TestBookKnowledgeJobUsesCapturedDedaoService(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	snapshot := &services.Service{}
	oldRunner := runDedaoEbookDownloadJob
	runDedaoEbookDownloadJob = func(ctx context.Context, job BookKnowledgeJob) (map[string]any, error) {
		if got := dedaoServiceFromContext(ctx); got != snapshot {
			t.Fatalf("job service = %p, want captured %p", got, snapshot)
		}
		return map[string]any{"ebook_id": job.EbookID}, nil
	}
	defer func() { runDedaoEbookDownloadJob = oldRunner }()

	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 47, EbookEnID: "snapshot-enid", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RunBookKnowledgeJobWithService(job.ID, snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestBookKnowledgeJobPersistsRunningBeforeExecutorCompletes(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldRunner := runDedaoEbookDownloadJob
	started := make(chan struct{})
	release := make(chan struct{})
	runDedaoEbookDownloadJob = func(_ context.Context, job BookKnowledgeJob) (map[string]any, error) {
		close(started)
		<-release
		return map[string]any{"ebook_id": job.EbookID}, nil
	}
	defer func() { runDedaoEbookDownloadJob = oldRunner }()

	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 44, EbookEnID: "running-enid", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		store.RunBookKnowledgeJob(job.ID)
	}()
	<-started
	running, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || running.Status != BookKnowledgeJobStatusRunning || running.StartedAt == "" {
		t.Fatalf("running job = %#v, err=%v", running, err)
	}
	close(release)
	<-done
	finished, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || finished.Status != BookKnowledgeJobStatusSucceeded {
		t.Fatalf("finished job = %#v, err=%v", finished, err)
	}
}

func TestBookKnowledgeJobSurfacesTerminalPersistenceFailure(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	oldRunner := runDedaoEbookDownloadJob
	runDedaoEbookDownloadJob = func(context.Context, BookKnowledgeJob) (map[string]any, error) {
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root, []byte("blocked"), 0o600); err != nil {
			t.Fatal(err)
		}
		return map[string]any{"ebook_id": 46}, nil
	}
	defer func() { runDedaoEbookDownloadJob = oldRunner }()

	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 46, EbookEnID: "persist-enid", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RunBookKnowledgeJob(job.ID); err == nil {
		t.Fatal("terminal persistence failure was not returned")
	}
}

func TestBookKnowledgeStoreFailsInterruptedQueuedAndRunningJobs(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	queued, _ := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 42, EbookEnID: "queued", DownloadType: 1,
	})
	running, _ := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 43, EbookEnID: "running", DownloadType: 1,
	})
	_, err := store.updateBookKnowledgeJob(running.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		job.Status = BookKnowledgeJobStatusRunning
		return job
	})
	if err != nil {
		t.Fatal(err)
	}

	count, err := store.FailInterruptedBookKnowledgeJobs("interrupted by server restart")
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	for _, id := range []string{queued.ID, running.ID} {
		job, loadErr := store.LoadBookKnowledgeJob(id)
		if loadErr != nil || job.Status != BookKnowledgeJobStatusFailed || job.FinishedAt == "" {
			t.Fatalf("recovered job = %#v, err=%v", job, loadErr)
		}
	}
}

func TestDefaultDedaoDownloadRootUsesExplicitAndKBaseRoots(t *testing.T) {
	t.Setenv("DEDAO_DOWNLOAD_ROOT", "/srv/dedao-downloads")
	if got := DefaultDedaoDownloadRoot(); got != "/srv/dedao-downloads" {
		t.Fatalf("explicit root = %q", got)
	}
	t.Setenv("DEDAO_DOWNLOAD_ROOT", "")
	t.Setenv("DEDAO_KBASE_DOWNLOAD_ROOT", "")
	t.Setenv("DEDAO_KBASE_ROOT", "/srv/dedao-kbase")
	if got, want := DefaultDedaoDownloadRoot(), filepath.Join("/srv/dedao-kbase", "downloads"); got != want {
		t.Fatalf("fallback root = %q, want %q", got, want)
	}
}
