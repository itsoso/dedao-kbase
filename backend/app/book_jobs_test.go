package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestBookKnowledgeJobAcceptsDedaoEbookDownload(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())

	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type:         BookKnowledgeJobTypeDedaoEbookDownload,
		EbookID:      67929,
		EbookEnID:    "ebook-enid",
		DownloadType: 2,
	})
	if err != nil {
		t.Fatalf("CreateBookKnowledgeJob returned error: %v", err)
	}

	if job.Type != BookKnowledgeJobTypeDedaoEbookDownload || job.EbookID != 67929 ||
		job.EbookEnID != "ebook-enid" || job.DownloadType != 2 {
		t.Fatalf("job = %#v", job)
	}

	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil {
		t.Fatalf("LoadBookKnowledgeJob returned error: %v", err)
	}
	if loaded.EbookID != 67929 || loaded.EbookEnID != "ebook-enid" || loaded.DownloadType != 2 {
		t.Fatalf("loaded job = %#v", loaded)
	}
}

func TestBookKnowledgeJobAcceptsDedaoEbookSyncKBase(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())

	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type:      BookKnowledgeJobTypeDedaoEbookSyncKBase,
		EbookID:   67929,
		EbookEnID: "ebook-enid",
	})
	if err != nil {
		t.Fatalf("CreateBookKnowledgeJob returned error: %v", err)
	}

	if job.Type != BookKnowledgeJobTypeDedaoEbookSyncKBase || job.EbookID != 67929 ||
		job.EbookEnID != "ebook-enid" || job.DownloadType != 1 {
		t.Fatalf("job = %#v", job)
	}
}

func TestBookKnowledgeJobAcceptsDedaoOdobDownload(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())

	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type:         BookKnowledgeJobTypeDedaoOdobDownload,
		OdobID:       301,
		OdobEnID:     "odob-enid",
		OdobTitle:    "每天听本书",
		OdobAliasID:  "audio-alias",
		OdobCanPlay:  true,
		DownloadType: 3,
	})
	if err != nil {
		t.Fatalf("CreateBookKnowledgeJob returned error: %v", err)
	}

	if job.Type != BookKnowledgeJobTypeDedaoOdobDownload || job.OdobID != 301 ||
		job.OdobEnID != "odob-enid" || job.OdobAliasID != "audio-alias" ||
		job.OdobTitle != "每天听本书" || !job.OdobCanPlay || job.DownloadType != 3 {
		t.Fatalf("job = %#v", job)
	}

	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil {
		t.Fatalf("LoadBookKnowledgeJob returned error: %v", err)
	}
	if loaded.OdobID != 301 || loaded.OdobEnID != "odob-enid" || loaded.OdobAliasID != "audio-alias" ||
		loaded.DownloadType != 3 {
		t.Fatalf("loaded job = %#v", loaded)
	}
}

func TestBookKnowledgeJobAcceptsDedaoOdobSyncKBase(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())

	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type:        BookKnowledgeJobTypeDedaoOdobSyncKBase,
		OdobID:      301,
		OdobEnID:    "odob-enid",
		OdobTitle:   "每天听本书",
		OdobAliasID: "audio-alias",
	})
	if err != nil {
		t.Fatalf("CreateBookKnowledgeJob returned error: %v", err)
	}

	if job.Type != BookKnowledgeJobTypeDedaoOdobSyncKBase || job.OdobID != 301 ||
		job.OdobEnID != "odob-enid" || job.OdobAliasID != "audio-alias" || job.DownloadType != 3 {
		t.Fatalf("job = %#v", job)
	}
}

func TestBookKnowledgeJobRejectsInvalidDedaoEbookJobs(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	tests := []struct {
		name    string
		request BookKnowledgeJobRequest
		want    string
	}{
		{
			name:    "download missing ebook id",
			request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookEnID: "ebook-enid", DownloadType: 1},
			want:    "ebook_id is required",
		},
		{
			name:    "sync missing enid",
			request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoEbookSyncKBase, EbookID: 67929},
			want:    "ebook_enid is required",
		},
		{
			name:    "download unsupported format",
			request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 67929, EbookEnID: "ebook-enid", DownloadType: 9},
			want:    "download_type must be 1, 2, or 3",
		},
		{
			name:    "odob missing id",
			request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoOdobDownload, OdobEnID: "odob-enid", OdobAliasID: "audio-alias"},
			want:    "odob_id is required",
		},
		{
			name:    "odob missing enid",
			request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoOdobDownload, OdobID: 301, OdobAliasID: "audio-alias"},
			want:    "odob_enid is required",
		},
		{
			name:    "odob missing alias id",
			request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoOdobDownload, OdobID: 301, OdobEnID: "odob-enid"},
			want:    "odob_alias_id is required",
		},
		{
			name:    "odob unsupported format",
			request: BookKnowledgeJobRequest{Type: BookKnowledgeJobTypeDedaoOdobDownload, OdobID: 301, OdobEnID: "odob-enid", OdobAliasID: "audio-alias", DownloadType: 9},
			want:    "download_type must be 1, 2, or 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.CreateBookKnowledgeJob(tt.request)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestBookKnowledgeJobExecutesDedaoEbookDownload(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldRunner := runDedaoEbookDownloadJob
	defer func() { runDedaoEbookDownloadJob = oldRunner }()

	var gotJob BookKnowledgeJob
	runDedaoEbookDownloadJob = func(_ context.Context, job BookKnowledgeJob) (map[string]any, error) {
		gotJob = job
		return map[string]any{
			"ebook_id":      job.EbookID,
			"download_type": job.DownloadType,
		}, nil
	}

	result, err := store.executeBookKnowledgeJob(BookKnowledgeJob{
		Type:         BookKnowledgeJobTypeDedaoEbookDownload,
		EbookID:      67929,
		EbookEnID:    "ebook-enid",
		DownloadType: 3,
	})
	if err != nil {
		t.Fatalf("executeBookKnowledgeJob returned error: %v", err)
	}
	if gotJob.EbookID != 67929 || gotJob.EbookEnID != "ebook-enid" || gotJob.DownloadType != 3 {
		t.Fatalf("gotJob = %#v", gotJob)
	}
	if result["download_type"] != 3 {
		t.Fatalf("result = %#v", result)
	}
}

func TestBookKnowledgeJobExecutesDedaoEbookSyncKBase(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldRunner := runDedaoEbookSyncKBaseJob
	defer func() { runDedaoEbookSyncKBaseJob = oldRunner }()

	var gotStore *BookKnowledgeStore
	var gotJob BookKnowledgeJob
	runDedaoEbookSyncKBaseJob = func(_ context.Context, store *BookKnowledgeStore, job BookKnowledgeJob) (map[string]any, error) {
		gotStore = store
		gotJob = job
		return map[string]any{"knowledge_book_id": "67929"}, nil
	}

	result, err := store.executeBookKnowledgeJob(BookKnowledgeJob{
		Type:      BookKnowledgeJobTypeDedaoEbookSyncKBase,
		EbookID:   67929,
		EbookEnID: "ebook-enid",
	})
	if err != nil {
		t.Fatalf("executeBookKnowledgeJob returned error: %v", err)
	}
	if gotStore != store {
		t.Fatalf("gotStore = %#v, want test store", gotStore)
	}
	if gotJob.EbookID != 67929 || gotJob.EbookEnID != "ebook-enid" {
		t.Fatalf("gotJob = %#v", gotJob)
	}
	if result["knowledge_book_id"] != "67929" {
		t.Fatalf("result = %#v", result)
	}
}

func TestBookKnowledgeJobExecutesDedaoOdobDownload(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldRunner := runDedaoOdobDownloadJob
	defer func() { runDedaoOdobDownloadJob = oldRunner }()

	var gotJob BookKnowledgeJob
	runDedaoOdobDownloadJob = func(_ context.Context, job BookKnowledgeJob) (map[string]any, error) {
		gotJob = job
		return map[string]any{
			"odob_id":       job.OdobID,
			"odob_alias_id": job.OdobAliasID,
			"download_type": job.DownloadType,
		}, nil
	}

	result, err := store.executeBookKnowledgeJob(BookKnowledgeJob{
		Type:         BookKnowledgeJobTypeDedaoOdobDownload,
		OdobID:       301,
		OdobEnID:     "odob-enid",
		OdobTitle:    "每天听本书",
		OdobAliasID:  "audio-alias",
		OdobCanPlay:  true,
		DownloadType: 2,
	})
	if err != nil {
		t.Fatalf("executeBookKnowledgeJob returned error: %v", err)
	}
	if gotJob.OdobID != 301 || gotJob.OdobEnID != "odob-enid" || gotJob.OdobAliasID != "audio-alias" ||
		!gotJob.OdobCanPlay || gotJob.DownloadType != 2 {
		t.Fatalf("gotJob = %#v", gotJob)
	}
	if result["download_type"] != 2 || result["odob_alias_id"] != "audio-alias" {
		t.Fatalf("result = %#v", result)
	}
}

func TestBookKnowledgeJobExecutesDedaoOdobSyncKBase(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldRunner := runDedaoOdobSyncKBaseJob
	defer func() { runDedaoOdobSyncKBaseJob = oldRunner }()

	var gotStore *BookKnowledgeStore
	var gotJob BookKnowledgeJob
	runDedaoOdobSyncKBaseJob = func(_ context.Context, store *BookKnowledgeStore, job BookKnowledgeJob) (map[string]any, error) {
		gotStore = store
		gotJob = job
		return map[string]any{"knowledge_book_id": "odob-301"}, nil
	}

	result, err := store.executeBookKnowledgeJob(BookKnowledgeJob{
		Type:        BookKnowledgeJobTypeDedaoOdobSyncKBase,
		OdobID:      301,
		OdobEnID:    "odob-enid",
		OdobTitle:   "每天听本书",
		OdobAliasID: "audio-alias",
	})
	if err != nil {
		t.Fatalf("executeBookKnowledgeJob returned error: %v", err)
	}
	if gotStore != store {
		t.Fatalf("gotStore = %#v, want test store", gotStore)
	}
	if gotJob.OdobID != 301 || gotJob.OdobEnID != "odob-enid" || gotJob.OdobAliasID != "audio-alias" {
		t.Fatalf("gotJob = %#v", gotJob)
	}
	if result["knowledge_book_id"] != "odob-301" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDefaultDedaoDownloadRootUsesKBaseRootBeforeWikiRepo(t *testing.T) {
	t.Setenv("DEDAO_DOWNLOAD_ROOT", "")
	t.Setenv("DEDAO_KBASE_DOWNLOAD_ROOT", "")
	t.Setenv("DEDAO_KBASE_ROOT", "/srv/dedao-kbase")
	t.Setenv("DEDAO_BOOK_KNOWLEDGE_ROOT", "")
	t.Setenv("KBASE_BOOK_KNOWLEDGE_ROOT", "")
	t.Setenv("DEDAO_WIKI_REPO", "/legacy/wiki-repo")

	got := DefaultDedaoDownloadRoot()
	want := filepath.Join("/srv/dedao-kbase", "downloads")
	if got != want {
		t.Fatalf("DefaultDedaoDownloadRoot() = %q, want %q", got, want)
	}
}

func TestDefaultDedaoDownloadRootUsesExplicitOverride(t *testing.T) {
	t.Setenv("DEDAO_DOWNLOAD_ROOT", "/srv/dedao-downloads")
	t.Setenv("DEDAO_KBASE_DOWNLOAD_ROOT", "/srv/ignored-downloads")
	t.Setenv("DEDAO_KBASE_ROOT", "/srv/dedao-kbase")

	got := DefaultDedaoDownloadRoot()
	if got != "/srv/dedao-downloads" {
		t.Fatalf("DefaultDedaoDownloadRoot() = %q, want explicit override", got)
	}
}

func TestBookKnowledgeStoreFailsInterruptedRunningJobs(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	running, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type:   BookKnowledgeJobTypeNotebookLMExport,
		BookID: "67929",
	})
	if err != nil {
		t.Fatalf("CreateBookKnowledgeJob running returned error: %v", err)
	}
	queued, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type:   BookKnowledgeJobTypeNotebookLMExport,
		BookID: "123",
	})
	if err != nil {
		t.Fatalf("CreateBookKnowledgeJob queued returned error: %v", err)
	}
	_, err = store.updateBookKnowledgeJob(running.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		job.Status = BookKnowledgeJobStatusRunning
		job.StartedAt = "2026-06-28T13:53:54Z"
		job.UpdatedAt = "2026-06-28T13:53:54Z"
		job.Logs = append(job.Logs, "running")
		return job
	})
	if err != nil {
		t.Fatalf("updateBookKnowledgeJob returned error: %v", err)
	}

	count, err := store.FailRunningBookKnowledgeJobs("interrupted by server restart")
	if err != nil {
		t.Fatalf("FailRunningBookKnowledgeJobs returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	loadedRunning, err := store.LoadBookKnowledgeJob(running.ID)
	if err != nil {
		t.Fatalf("LoadBookKnowledgeJob running returned error: %v", err)
	}
	if loadedRunning.Status != BookKnowledgeJobStatusFailed {
		t.Fatalf("running status = %s, want failed", loadedRunning.Status)
	}
	if !strings.Contains(loadedRunning.Error, "interrupted by server restart") {
		t.Fatalf("running error = %q", loadedRunning.Error)
	}
	if loadedRunning.FinishedAt == "" {
		t.Fatal("running FinishedAt is empty")
	}

	loadedQueued, err := store.LoadBookKnowledgeJob(queued.ID)
	if err != nil {
		t.Fatalf("LoadBookKnowledgeJob queued returned error: %v", err)
	}
	if loadedQueued.Status != BookKnowledgeJobStatusQueued {
		t.Fatalf("queued status = %s, want queued", loadedQueued.Status)
	}
}

func TestBookKnowledgeStoreFailsInterruptedQueuedAndRunningJobs(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	running, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type:   BookKnowledgeJobTypeNotebookLMExport,
		BookID: "67929",
	})
	if err != nil {
		t.Fatalf("CreateBookKnowledgeJob running returned error: %v", err)
	}
	queued, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type:   BookKnowledgeJobTypeNotebookLMExport,
		BookID: "123",
	})
	if err != nil {
		t.Fatalf("CreateBookKnowledgeJob queued returned error: %v", err)
	}
	succeeded, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type:   BookKnowledgeJobTypeNotebookLMExport,
		BookID: "456",
	})
	if err != nil {
		t.Fatalf("CreateBookKnowledgeJob succeeded returned error: %v", err)
	}
	_, err = store.updateBookKnowledgeJob(running.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		job.Status = BookKnowledgeJobStatusRunning
		job.StartedAt = "2026-06-28T13:53:54Z"
		job.UpdatedAt = "2026-06-28T13:53:54Z"
		job.Logs = append(job.Logs, "running")
		return job
	})
	if err != nil {
		t.Fatalf("update running returned error: %v", err)
	}
	_, err = store.updateBookKnowledgeJob(succeeded.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		job.Status = BookKnowledgeJobStatusSucceeded
		job.UpdatedAt = "2026-06-28T13:53:55Z"
		job.FinishedAt = "2026-06-28T13:53:55Z"
		job.Logs = append(job.Logs, "succeeded")
		return job
	})
	if err != nil {
		t.Fatalf("update succeeded returned error: %v", err)
	}

	count, err := store.FailInterruptedBookKnowledgeJobs("interrupted by server restart")
	if err != nil {
		t.Fatalf("FailInterruptedBookKnowledgeJobs returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	for _, jobID := range []string{running.ID, queued.ID} {
		loaded, err := store.LoadBookKnowledgeJob(jobID)
		if err != nil {
			t.Fatalf("LoadBookKnowledgeJob(%s) returned error: %v", jobID, err)
		}
		if loaded.Status != BookKnowledgeJobStatusFailed {
			t.Fatalf("status = %s, want failed", loaded.Status)
		}
		if !strings.Contains(loaded.Error, "interrupted by server restart") {
			t.Fatalf("error = %q", loaded.Error)
		}
		if loaded.FinishedAt == "" {
			t.Fatalf("FinishedAt is empty for %s", jobID)
		}
	}

	loadedSucceeded, err := store.LoadBookKnowledgeJob(succeeded.ID)
	if err != nil {
		t.Fatalf("LoadBookKnowledgeJob succeeded returned error: %v", err)
	}
	if loadedSucceeded.Status != BookKnowledgeJobStatusSucceeded {
		t.Fatalf("succeeded status = %s, want succeeded", loadedSucceeded.Status)
	}
}
