package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/yann0917/dedao-gui/backend/services"
)

func TestBookKnowledgeJobsMigrateLegacyJSONOnce(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	legacyJobs := []BookKnowledgeJob{
		{
			ID: "job-old-log", Type: BookKnowledgeJobTypeDedaoEbookDownload,
			Status: BookKnowledgeJobStatusFailed, EbookID: 42, EbookEnID: "owned-log", DownloadType: 1,
			Error: "job execution failed", Logs: []string{"queued", "running", "failed: interrupted"},
			CreatedAt: "2026-08-09T00:00:00Z", UpdatedAt: "2026-08-09T00:01:00Z",
		},
		{
			ID: "job-old-error", Type: BookKnowledgeJobTypeDedaoEbookDownload,
			Status: BookKnowledgeJobStatusFailed, EbookID: 43, EbookEnID: "owned-error", DownloadType: 2,
			Error: "interrupted by server restart", Logs: []string{"queued", "running", "failed"},
			CreatedAt: "2026-08-09T00:02:00Z", UpdatedAt: "2026-08-09T00:03:00Z",
		},
	}
	writeLegacyBookJobs(t, store.LegacyJobsPath(), legacyJobs)
	legacyBefore, err := os.ReadFile(store.LegacyJobsPath())
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.ListBookKnowledgeJobs(10)
	if err != nil {
		t.Fatalf("first list: %v", err)
	}
	db, err := store.openBookJobsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM book_job_meta WHERE key = ?`, bookKnowledgeLegacyJobsImportedV1); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	secondStore := NewBookKnowledgeStore(store.Root())
	second, err := secondStore.ListBookKnowledgeJobs(10)
	if err != nil {
		t.Fatalf("second list: %v", err)
	}
	if len(first) != len(legacyJobs) || len(second) != len(legacyJobs) {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	for _, jobs := range [][]BookKnowledgeJob{first, second} {
		for _, job := range jobs {
			if job.Status != BookKnowledgeJobStatusInterrupted || job.FailureCode != BookKnowledgeJobFailureWorkerInterrupted {
				t.Fatalf("migrated job = %#v", job)
			}
		}
	}

	legacyAfter, err := os.ReadFile(store.LegacyJobsPath())
	if err != nil {
		t.Fatalf("legacy file was not retained: %v", err)
	}
	if string(legacyAfter) != string(legacyBefore) {
		t.Fatalf("legacy file was modified\nbefore: %s\nafter: %s", legacyBefore, legacyAfter)
	}
}

func TestBookKnowledgeJobsReadModifyWriteUsesImmediateTransaction(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 44, EbookEnID: "immediate", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := store.openBookJobsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.Exec(`PRAGMA busy_timeout = 20`); err != nil {
		t.Fatal(err)
	}

	tx, err := first.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT status FROM book_jobs WHERE job_id = ?`, job.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Exec(`UPDATE book_jobs SET updated_at = 'competing' WHERE job_id = ?`, job.ID); err == nil {
		t.Fatal("competing writer succeeded while read-modify-write transaction was open")
	} else {
		var sqliteErr sqlite3.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.Code != sqlite3.ErrBusy {
			t.Fatalf("competing writer error = %v, want SQLITE_BUSY", err)
		}
	}
	if _, err := tx.Exec(`UPDATE book_jobs SET updated_at = 'owned' WHERE job_id = ?`, job.ID); err != nil {
		t.Fatalf("upgrade read-modify-write transaction: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestBookKnowledgeJobsSQLiteSchema(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	db, err := store.openBookJobsDB()
	if err != nil {
		t.Fatalf("open book jobs database: %v", err)
	}
	defer db.Close()

	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	var busyTimeout int
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}

	for _, table := range []string{"book_jobs", "book_job_events", "book_job_meta"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("table %q count = %d, want 1", table, count)
		}
	}
	var marker string
	if err := db.QueryRow(`SELECT value FROM book_job_meta WHERE key = ?`, bookKnowledgeLegacyJobsImportedV1).Scan(&marker); err != nil {
		t.Fatalf("read migration marker: %v", err)
	}
	if marker != "1" {
		t.Fatalf("migration marker = %q, want 1", marker)
	}
}

func writeLegacyBookJobs(t *testing.T, path string, jobs []BookKnowledgeJob) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	payload, err := json.MarshalIndent(bookKnowledgeJobsFile{Jobs: jobs}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

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
	if err != nil || loaded.Status != BookKnowledgeJobStatusSucceeded || loaded.Stage != "completed" || loaded.StartedAt == "" || loaded.FinishedAt == "" {
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
	if err != nil || loaded.Status != BookKnowledgeJobStatusFailed || loaded.Stage != "failed" || loaded.Error == "" {
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
	if err != nil || running.Status != BookKnowledgeJobStatusRunning || running.Stage != "running" || running.StartedAt == "" {
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
		if loadErr != nil || job.Status != BookKnowledgeJobStatusFailed || job.Stage != "failed" || job.FinishedAt == "" {
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
