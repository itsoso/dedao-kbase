package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/yann0917/dedao-gui/backend/services"
)

func TestBookKnowledgeJobsRejectInvalidLegacyJSONAtomically(t *testing.T) {
	valid := BookKnowledgeJob{
		ID: "job-valid", Type: BookKnowledgeJobTypeDedaoEbookDownload,
		Status: BookKnowledgeJobStatusQueued, EbookID: 42, EbookEnID: "valid", DownloadType: 1,
		CreatedAt: "2026-08-09T00:00:00Z", UpdatedAt: "2026-08-09T00:00:00Z",
	}
	validPayload, err := json.Marshal(bookKnowledgeJobsFile{Jobs: []BookKnowledgeJob{valid}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		payload string
	}{
		{name: "syntax error", payload: `{"jobs":[`},
		{name: "trailing garbage", payload: `{"jobs":[]}` + "\nnot-json"},
		{name: "invalid job fields", payload: `{"jobs":[{}]}`},
		{name: "missing jobs", payload: `{}`},
		{name: "null jobs", payload: `{"jobs":null}`},
		{name: "unknown top-level field", payload: `{"jobz":[]}`},
		{name: "unknown job field", payload: `{"jobs":[{"id":"job-unknown","type":"dedao_ebook_download","status":"queued","ebook_id":42,"ebook_enid":"unknown","download_type":1,"created_at":"2026-08-09T00:00:00Z","updated_at":"2026-08-09T00:00:00Z","unexpected":true}]}`},
		{name: "invalid optional timestamp", payload: `{"jobs":[{"id":"job-time","type":"dedao_ebook_download","status":"queued","ebook_id":42,"ebook_enid":"time","download_type":1,"created_at":"2026-08-09T00:00:00Z","updated_at":"2026-08-09T00:00:00Z","started_at":"not-a-time"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			if err := os.WriteFile(store.LegacyJobsPath(), []byte(test.payload), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ListBookKnowledgeJobs(10); err == nil {
				t.Fatal("invalid legacy jobs were accepted")
			}
			assertBookKnowledgeMigrationMarkerAbsent(t, store.BookJobsDBPath())

			if err := os.WriteFile(store.LegacyJobsPath(), append(validPayload, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			jobs, err := store.ListBookKnowledgeJobs(10)
			if err != nil || len(jobs) != 1 || jobs[0].ID != valid.ID {
				t.Fatalf("jobs = %#v, err=%v", jobs, err)
			}
		})
	}
}

func TestBookKnowledgeJobsLegacyInterruptedClassificationIsExact(t *testing.T) {
	for _, test := range []struct {
		name  string
		error string
		logs  []string
		want  bool
	}{
		{name: "exact error", error: " interrupted ", want: true},
		{name: "server restart", error: "Interrupted by server restart", want: true},
		{name: "kbase restart", error: "interrupted by kbase-server restart", want: true},
		{name: "exact log", logs: []string{" FAILED: INTERRUPTED "}, want: true},
		{name: "negated", error: "not interrupted", want: false},
		{name: "substring", error: "uninterrupted connection", want: false},
		{name: "log suffix", logs: []string{"failed: interrupted while writing"}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			job := BookKnowledgeJob{Status: BookKnowledgeJobStatusFailed, Error: test.error, Logs: test.logs}
			if got := legacyBookKnowledgeJobWasInterrupted(job); got != test.want {
				t.Fatalf("classified = %t, want %t", got, test.want)
			}
		})
	}
}

func TestBookKnowledgeJobsMigrationSanitizesLegacyData(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	privatePath := "/srv/private/legacy-download"
	privateWindowsPath := `C:\Users\example\private\book`
	privateUNCPath := `\\private-server\books\legacy`
	privateToken := "token=legacy-placeholder"
	writeLegacyBookJobs(t, store.LegacyJobsPath(), []BookKnowledgeJob{
		{
			ID: "job-private-interrupted", Type: BookKnowledgeJobTypeDedaoEbookDownload,
			Status: BookKnowledgeJobStatusFailed, EbookID: 42, EbookEnID: "private-interrupted", DownloadType: 1,
			Error: privatePath, Logs: []string{"failed: interrupted", privateToken},
			Result: map[string]any{
				"ebook_id": 42, "title": privatePath + " " + privateToken, "path": privatePath,
			},
			CreatedAt: "2026-08-09T00:00:00Z", UpdatedAt: "2026-08-09T00:01:00Z",
		},
		{
			ID: "job-private-failed", Type: BookKnowledgeJobTypeDedaoEbookDownload,
			Status: BookKnowledgeJobStatusFailed, EbookID: 43, EbookEnID: "private-failed", DownloadType: 2,
			Error: privatePath + " " + privateToken,
			Logs:  []string{"queued", "running", "downloaded from " + privateWindowsPath, privateToken, "failed"},
			Result: map[string]any{
				"ebook_id": 43, "title": "downloaded from " + privateWindowsPath,
				"knowledge_book_id": "copied from " + privateUNCPath, "private_path": privatePath,
			},
			CreatedAt: "2026-08-09T00:02:00Z", UpdatedAt: "2026-08-09T00:03:00Z",
		},
	})

	jobs, err := store.ListBookKnowledgeJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs = %#v", jobs)
	}
	for _, job := range jobs {
		if _, ok := job.Result["ebook_id"]; !ok {
			t.Fatalf("safe result removed: %#v", job.Result)
		}
		if _, ok := job.Result["path"]; ok {
			t.Fatalf("private path result retained: %#v", job.Result)
		}
		if _, ok := job.Result["private_path"]; ok {
			t.Fatalf("private result retained: %#v", job.Result)
		}
		if title, ok := job.Result["title"].(string); ok && title != "" {
			t.Fatalf("sensitive title retained: %#v", job.Result)
		}
		if job.ID == "job-private-failed" {
			wantLogs := []string{"queued", "running", "failed"}
			if !reflect.DeepEqual(job.Logs, wantLogs) {
				t.Fatalf("failed logs = %#v, want %#v", job.Logs, wantLogs)
			}
		}
	}

	db, err := sql.Open("sqlite3", store.BookJobsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT failure_message || ' ' || logs_json || ' ' || result_json FROM book_jobs
		UNION ALL
		SELECT message FROM book_job_events`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var persisted string
		if err := rows.Scan(&persisted); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{privatePath, privateWindowsPath, privateUNCPath, privateToken} {
			if strings.Contains(persisted, forbidden) {
				t.Fatalf("persisted legacy secret %q in %q", forbidden, persisted)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestBookKnowledgeJobsMigrationPreservesSafeLegacyResultAndLogs(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	slashTitle := "输入/输出"
	backslashTitle := `输入\输出`
	longTitle := strings.Repeat("长标题", 240)
	privateLog := "downloaded from /Volumes/private/books/legacy"
	tokenLog := "token=legacy-placeholder"
	unknownLog := "download finished for a private book"
	writeLegacyBookJobs(t, store.LegacyJobsPath(), []BookKnowledgeJob{
		{
			ID: "job-safe-slash", Type: BookKnowledgeJobTypeDedaoEbookDownload,
			Status: BookKnowledgeJobStatusSucceeded, EbookID: 61, EbookEnID: "random-enid-61", DownloadType: 1,
			Result:    map[string]any{"ebook_id": 61, "ebook_enid": "random-enid-61", "title": slashTitle},
			Logs:      []string{"queued", "running", privateLog, "succeeded"},
			CreatedAt: "2026-08-09T00:00:00Z", UpdatedAt: "2026-08-09T00:01:00Z",
		},
		{
			ID: "job-safe-long", Type: BookKnowledgeJobTypeDedaoEbookDownload,
			Status: BookKnowledgeJobStatusSucceeded, EbookID: 62, EbookEnID: "random-enid-62", DownloadType: 2,
			Result: map[string]any{
				"ebook_id": 62, "title": longTitle, "knowledge_book_id": "knowledge-book-62",
			},
			Logs:      []string{"queued", tokenLog, unknownLog, "running", "succeeded"},
			CreatedAt: "2026-08-09T00:02:00Z", UpdatedAt: "2026-08-09T00:03:00Z",
		},
		{
			ID: "job-safe-backslash", Type: BookKnowledgeJobTypeDedaoEbookDownload,
			Status: BookKnowledgeJobStatusSucceeded, EbookID: 63, EbookEnID: "random-enid-63", DownloadType: 3,
			Result:    map[string]any{"ebook_id": 63, "title": backslashTitle},
			Logs:      []string{"queued", "running", "succeeded", privateLog},
			CreatedAt: "2026-08-09T00:04:00Z", UpdatedAt: "2026-08-09T00:05:00Z",
		},
	})

	jobs, err := store.ListBookKnowledgeJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]BookKnowledgeJob, len(jobs))
	for _, job := range jobs {
		byID[job.ID] = job
	}
	if got := byID["job-safe-slash"].Result["title"]; got != slashTitle {
		t.Fatalf("slash title = %#v, want %q", got, slashTitle)
	}
	if got := byID["job-safe-slash"].Result["ebook_enid"]; got != "random-enid-61" {
		t.Fatalf("ebook_enid = %#v", got)
	}
	if got := byID["job-safe-long"].Result["title"]; got != longTitle {
		t.Fatalf("long title length = %d, want %d", len(fmt.Sprint(got)), len(longTitle))
	}
	if got := byID["job-safe-long"].Result["knowledge_book_id"]; got != "knowledge-book-62" {
		t.Fatalf("knowledge_book_id = %#v", got)
	}
	if got := byID["job-safe-backslash"].Result["title"]; got != backslashTitle {
		t.Fatalf("backslash title = %#v, want %q", got, backslashTitle)
	}
	wantLogs := []string{"queued", "running", "succeeded"}
	for _, jobID := range []string{"job-safe-slash", "job-safe-long", "job-safe-backslash"} {
		if got := byID[jobID].Logs; !reflect.DeepEqual(got, wantLogs) {
			t.Fatalf("%s logs = %#v, want %#v", jobID, got, wantLogs)
		}
	}

	db, err := sql.Open("sqlite3", store.BookJobsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT logs_json || ' ' || result_json FROM book_jobs
		UNION ALL
		SELECT message FROM book_job_events`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var persisted string
		if err := rows.Scan(&persisted); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{privateLog, tokenLog, unknownLog} {
			if strings.Contains(persisted, forbidden) {
				t.Fatalf("persisted sensitive log %q in %q", forbidden, persisted)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestBookKnowledgeJobsMigrationPreservesSafeInterruptedHistory(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	writeLegacyBookJobs(t, store.LegacyJobsPath(), []BookKnowledgeJob{
		{
			ID: "job-interrupted-history", Type: BookKnowledgeJobTypeDedaoEbookDownload,
			Status: BookKnowledgeJobStatusFailed, EbookID: 64, EbookEnID: "random-enid-64", DownloadType: 1,
			Error:     "interrupted by server restart",
			Logs:      []string{"queued", "running", "failed"},
			CreatedAt: "2026-08-09T00:06:00Z", UpdatedAt: "2026-08-09T00:07:00Z",
		},
	})

	jobs, err := store.ListBookKnowledgeJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %#v", jobs)
	}
	wantLogs := []string{"queued", "running", "interrupted"}
	if !reflect.DeepEqual(jobs[0].Logs, wantLogs) {
		t.Fatalf("logs = %#v, want %#v", jobs[0].Logs, wantLogs)
	}

	db, err := sql.Open("sqlite3", store.BookJobsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var message string
	if err := db.QueryRow(`SELECT message FROM book_job_events WHERE job_id = ?`, jobs[0].ID).Scan(&message); err != nil {
		t.Fatal(err)
	}
	if message != bookKnowledgeJobInterruptedMessage {
		t.Fatalf("event message = %q, want %q", message, bookKnowledgeJobInterruptedMessage)
	}
}

func TestBookKnowledgeJobsLegacySensitiveTextRecognizesUnixPathsAtBoundaries(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "path at start", value: "/data/private/book", want: true},
		{name: "path after whitespace", value: "downloaded from /mnt/private/book", want: true},
		{name: "path after delimiter", value: "output=(/data/private/book)", want: true},
		{name: "chinese slash", value: "输入/输出", want: false},
		{name: "ascii slash", value: "A/B", want: false},
		{name: "isolated slash", value: "/", want: false},
		{name: "spaced isolated slash", value: "value / only", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := legacyBookKnowledgeJobTextContainsSensitiveData(test.value); got != test.want {
				t.Fatalf("sensitive = %t, want %t for %q", got, test.want, test.value)
			}
		})
	}
}

func assertBookKnowledgeMigrationMarkerAbsent(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'book_job_meta'`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount == 0 {
		return
	}
	var markerCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM book_job_meta WHERE key = ?`, bookKnowledgeLegacyJobsImportedV1).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if markerCount != 0 {
		t.Fatal("legacy migration marker was committed")
	}
}

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

func TestBookKnowledgeJobRunStartsQueuedOnceAndClearsStaleState(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	oldRunner := runDedaoEbookDownloadJob
	defer func() { runDedaoEbookDownloadJob = oldRunner }()

	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 48, EbookEnID: "run-once", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.updateBookKnowledgeJob(job.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		job.Error = "old failure"
		job.FailureCode = "old_failure"
		job.FinishedAt = "2026-08-09T00:00:00Z"
		job.Result = map[string]any{"title": "old result"}
		job.LeaseOwner = "old-worker"
		job.LeaseExpiresAt = "2026-08-09T00:05:00Z"
		return job
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var stale atomic.Bool
	runDedaoEbookDownloadJob = func(_ context.Context, running BookKnowledgeJob) (map[string]any, error) {
		calls.Add(1)
		if running.Error != "" || running.FailureCode != "" || running.FinishedAt != "" ||
			running.Result != nil || running.LeaseOwner != "" || running.LeaseExpiresAt != "" {
			stale.Store(true)
		}
		return map[string]any{"ebook_id": running.EbookID}, nil
	}

	if err := store.RunBookKnowledgeJob(job.ID); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := store.RunBookKnowledgeJob(job.ID); err == nil {
		t.Fatal("second run succeeded")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
	if stale.Load() {
		t.Fatal("executor received stale execution state")
	}

	db, err := sql.Open("sqlite3", store.BookJobsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var runningMessage string
	if err := db.QueryRow(`
		SELECT message FROM book_job_events
		WHERE job_id = ? AND status = ? ORDER BY event_id DESC LIMIT 1`,
		job.ID, BookKnowledgeJobStatusRunning,
	).Scan(&runningMessage); err != nil {
		t.Fatal(err)
	}
	if runningMessage != "running" {
		t.Fatalf("running event message = %q, want running", runningMessage)
	}
}

func TestBookKnowledgeJobRunRejectsNonQueued(t *testing.T) {
	oldRunner := runDedaoEbookDownloadJob
	defer func() { runDedaoEbookDownloadJob = oldRunner }()
	var calls atomic.Int32
	runDedaoEbookDownloadJob = func(context.Context, BookKnowledgeJob) (map[string]any, error) {
		calls.Add(1)
		return nil, nil
	}

	for _, status := range []BookKnowledgeJobStatus{
		BookKnowledgeJobStatusRunning,
		BookKnowledgeJobStatusSucceeded,
		BookKnowledgeJobStatusFailed,
	} {
		t.Run(string(status), func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
				Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 49, EbookEnID: string(status), DownloadType: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.updateBookKnowledgeJob(job.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
				job.Status = status
				return job
			}); err != nil {
				t.Fatal(err)
			}
			if err := store.RunBookKnowledgeJob(job.ID); err == nil {
				t.Fatalf("run from %s succeeded", status)
			}
		})
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("executor calls = %d, want 0", got)
	}
}

func TestBookKnowledgeJobConcurrentRunExecutesOnce(t *testing.T) {
	root := t.TempDir()
	firstStore := NewBookKnowledgeStore(root)
	secondStore := NewBookKnowledgeStore(root)
	oldRunner := runDedaoEbookDownloadJob
	defer func() { runDedaoEbookDownloadJob = oldRunner }()

	job, err := firstStore.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 50, EbookEnID: "concurrent", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	runDedaoEbookDownloadJob = func(_ context.Context, running BookKnowledgeJob) (map[string]any, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return map[string]any{"ebook_id": running.EbookID}, nil
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- firstStore.RunBookKnowledgeJob(job.ID) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first executor did not start")
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- secondStore.RunBookKnowledgeJob(job.ID) }()
	select {
	case err := <-secondDone:
		if err == nil {
			close(release)
			<-firstDone
			t.Fatal("second concurrent run succeeded")
		}
	case <-time.After(time.Second):
		close(release)
		<-firstDone
		<-secondDone
		t.Fatal("second concurrent run reached executor")
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
}

func TestBookKnowledgeJobConcurrentConnectionStartIsAtomic(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 51, EbookEnID: "connection-race", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	start := make(chan struct{})
	results := make(chan error, 2)
	attempt := func(db *sql.DB) {
		<-start
		tx, err := db.Begin()
		if err != nil {
			results <- err
			return
		}
		if _, err := startBookKnowledgeJobInTx(tx, job.ID); err != nil {
			tx.Rollback()
			results <- err
			return
		}
		results <- tx.Commit()
	}
	go attempt(first)
	go attempt(second)
	close(start)
	succeeded := 0
	rejected := 0
	for index := 0; index < 2; index++ {
		if err := <-results; err != nil {
			rejected++
		} else {
			succeeded++
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("succeeded=%d rejected=%d, want 1/1", succeeded, rejected)
	}
	loaded, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || loaded.Status != BookKnowledgeJobStatusRunning {
		t.Fatalf("job = %#v, err=%v", loaded, err)
	}
	var runningEvents int
	if err := first.QueryRow(`SELECT COUNT(*) FROM book_job_events WHERE job_id = ? AND status = ?`,
		job.ID, BookKnowledgeJobStatusRunning).Scan(&runningEvents); err != nil {
		t.Fatal(err)
	}
	if runningEvents != 1 {
		t.Fatalf("running events = %d, want 1", runningEvents)
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
