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
	"runtime"
	"strings"
	"sync"
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
		{name: "path after full width colon", value: "路径：/data/private/book", want: true},
		{name: "path after full width parenthesis", value: "文件（/mnt/private/book）", want: true},
		{name: "chinese slash", value: "输入/输出", want: false},
		{name: "ascii slash", value: "A/B", want: false},
		{name: "isolated slash", value: "/", want: false},
		{name: "spaced isolated slash", value: "value / only", want: false},
		{name: "https URL", value: "https://example.com/book", want: false},
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
	var activeRetryIndexSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_book_jobs_one_active_retry'`).Scan(&activeRetryIndexSQL); err != nil {
		t.Fatalf("read active retry index: %v", err)
	}
	for _, clause := range []string{"UNIQUE INDEX", "retry_of", "status IN ('queued', 'running')"} {
		if !strings.Contains(activeRetryIndexSQL, clause) {
			t.Fatalf("active retry index = %q, want clause %q", activeRetryIndexSQL, clause)
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

func TestBookKnowledgeJobConcurrentClaim(t *testing.T) {
	root := t.TempDir()
	firstStore := NewBookKnowledgeStore(root)
	secondStore := NewBookKnowledgeStore(root)
	created, err := firstStore.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 101, EbookEnID: "claim-once", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstStore.updateBookKnowledgeJob(created.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		job.Stage = "failed"
		job.FailureCode = BookKnowledgeJobFailureUnknownFailure
		job.Error = "old safe failure"
		job.Result = map[string]any{"ebook_id": created.EbookID}
		job.LeaseOwner = "old-worker"
		job.LeaseExpiresAt = "2026-08-09T00:00:00Z"
		job.FinishedAt = "2026-08-09T00:00:00Z"
		return job
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	claims := make(chan *BookKnowledgeJob, 2)
	errorsFound := make(chan error, 2)
	claim := func(store *BookKnowledgeStore, workerID string) {
		<-start
		job, claimErr := store.ClaimNextBookKnowledgeJob(workerID, time.Minute)
		claims <- job
		errorsFound <- claimErr
	}
	go claim(firstStore, "worker-one")
	go claim(secondStore, "worker-two")
	close(start)

	winners := make([]BookKnowledgeJob, 0, 1)
	for index := 0; index < 2; index++ {
		if claimErr := <-errorsFound; claimErr != nil {
			t.Fatalf("claim error: %v", claimErr)
		}
		if claimed := <-claims; claimed != nil {
			winners = append(winners, *claimed)
		}
	}
	if len(winners) != 1 || winners[0].ID != created.ID {
		t.Fatalf("winners = %#v, want exactly %q", winners, created.ID)
	}
	if winners[0].Status != BookKnowledgeJobStatusRunning || winners[0].LeaseOwner == "" || winners[0].LeaseExpiresAt == "" {
		t.Fatalf("claimed job = %#v", winners[0])
	}
	if winners[0].Stage != "running" || winners[0].FailureCode != "" || winners[0].Error != "" ||
		winners[0].Result != nil || winners[0].FinishedAt != "" || winners[0].LeaseOwner == "old-worker" {
		t.Fatalf("claim retained stale state: %#v", winners[0])
	}
	if next, nextErr := firstStore.ClaimNextBookKnowledgeJob("worker-three", time.Minute); nextErr != nil || next != nil {
		t.Fatalf("empty claim = %#v, err=%v", next, nextErr)
	}
	second, err := firstStore.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 116, EbookEnID: "second-claim", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if next, nextErr := secondStore.ClaimNextBookKnowledgeJob("worker-three", time.Minute); nextErr != nil || next == nil || next.ID != second.ID {
		t.Fatalf("second claim = %#v, err=%v", next, nextErr)
	}
}

func TestBookKnowledgeJobConcurrentClaimDoesNotMaskDatabaseFailure(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	created, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 117, EbookEnID: "claim-trigger", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore := countBookKnowledgeJobEvents(t, store, created.ID)
	db, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TRIGGER reject_book_job_claim
		BEFORE UPDATE OF status ON book_jobs
		WHEN OLD.status = 'queued' AND NEW.status = 'running'
		BEGIN
			SELECT RAISE(ABORT, 'claim update blocked');
		END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := store.ClaimNextBookKnowledgeJob("trigger-worker", time.Minute)
	if err == nil || claimed != nil {
		t.Fatalf("claim = %#v, err=%v", claimed, err)
	}
	if errors.Is(err, ErrBookKnowledgeJobConflict) {
		t.Fatalf("database failure was reported as conflict: %v", err)
	}
	loaded, loadErr := store.LoadBookKnowledgeJob(created.ID)
	if loadErr != nil || loaded.Status != BookKnowledgeJobStatusQueued {
		t.Fatalf("job = %#v, err=%v", loaded, loadErr)
	}
	if got := countBookKnowledgeJobEvents(t, store, created.ID); got != eventsBefore {
		t.Fatalf("events = %d, want %d", got, eventsBefore)
	}
}

func TestBookKnowledgeJobLeaseOwner(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	job, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 102, EbookEnID: "lease-owner", DownloadType: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	initialEvents := countBookKnowledgeJobEvents(t, store, job.ID)
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{name: "empty worker", run: func() error {
			_, claimErr := store.ClaimNextBookKnowledgeJob("", time.Minute)
			return claimErr
		}},
		{name: "zero lease", run: func() error {
			_, claimErr := store.ClaimNextBookKnowledgeJob("worker-owner", 0)
			return claimErr
		}},
		{name: "negative lease", run: func() error {
			_, claimErr := store.ClaimNextBookKnowledgeJob("worker-owner", -time.Second)
			return claimErr
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal("invalid claim succeeded")
			}
		})
	}
	if got := countBookKnowledgeJobEvents(t, store, job.ID); got != initialEvents {
		t.Fatalf("events after invalid claims = %d, want %d", got, initialEvents)
	}

	claimed, err := store.ClaimNextBookKnowledgeJob("worker-owner", 2*time.Minute)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("claimed = %#v, err=%v", claimed, err)
	}
	claimedExpiry, err := time.Parse(time.RFC3339Nano, claimed.LeaseExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	ownedEvents := countBookKnowledgeJobEvents(t, store, job.ID)

	assertRejectedWithoutBookJobMutation(t, store, job.ID, ownedEvents, func() error {
		_, operationErr := store.RenewBookKnowledgeJobLease(job.ID, "wrong-worker", time.Minute)
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)
	assertRejectedWithoutBookJobMutation(t, store, job.ID, ownedEvents, func() error {
		_, operationErr := store.UpdateBookKnowledgeJobStage(job.ID, "wrong-worker", "downloading")
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)
	assertRejectedWithoutBookJobMutation(t, store, job.ID, ownedEvents, func() error {
		_, operationErr := store.CompleteBookKnowledgeJob(job.ID, "wrong-worker", map[string]any{"ebook_id": job.EbookID})
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)
	assertRejectedWithoutBookJobMutation(t, store, job.ID, ownedEvents, func() error {
		_, operationErr := store.FailBookKnowledgeJob(job.ID, "wrong-worker", BookKnowledgeJobFailureDownloadFailed)
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)
	assertRejectedWithoutBookJobMutation(t, store, job.ID, ownedEvents, func() error {
		_, operationErr := store.InterruptBookKnowledgeJob(job.ID, "wrong-worker")
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)
	assertRejectedWithoutBookJobMutation(t, store, job.ID, ownedEvents, func() error {
		_, operationErr := store.CompleteBookKnowledgeJob(job.ID, "", map[string]any{"ebook_id": job.EbookID})
		return operationErr
	}, ErrBookKnowledgeJobInvalidState)
	assertRejectedWithoutBookJobMutation(t, store, job.ID, ownedEvents, func() error {
		_, operationErr := store.UpdateBookKnowledgeJobStage(job.ID, "worker-owner", "arbitrary")
		return operationErr
	}, ErrBookKnowledgeJobInvalidState)
	assertRejectedWithoutBookJobMutation(t, store, job.ID, ownedEvents, func() error {
		_, operationErr := store.FailBookKnowledgeJob(job.ID, "worker-owner", "private /path failure")
		return operationErr
	}, ErrBookKnowledgeJobInvalidState)
	assertRejectedWithoutBookJobMutation(t, store, job.ID, ownedEvents, func() error {
		_, operationErr := store.RenewBookKnowledgeJobLease(job.ID, "worker-owner", 0)
		return operationErr
	}, ErrBookKnowledgeJobInvalidState)

	renewed, err := store.RenewBookKnowledgeJobLease(job.ID, "worker-owner", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	renewedExpiry, err := time.Parse(time.RFC3339Nano, renewed.LeaseExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if !renewedExpiry.After(claimedExpiry) {
		t.Fatalf("renewed expiry %s did not extend %s", renewedExpiry, claimedExpiry)
	}
	renewedEvents := countBookKnowledgeJobEvents(t, store, job.ID)
	assertRejectedWithoutBookJobMutation(t, store, job.ID, renewedEvents, func() error {
		_, operationErr := store.RenewBookKnowledgeJobLease(job.ID, "worker-owner", time.Second)
		return operationErr
	}, ErrBookKnowledgeJobInvalidState)

	downloading, err := store.UpdateBookKnowledgeJobStage(job.ID, "worker-owner", "downloading")
	if err != nil || downloading.Stage != "downloading" || downloading.Status != BookKnowledgeJobStatusRunning {
		t.Fatalf("downloading = %#v, err=%v", downloading, err)
	}
	building, err := store.UpdateBookKnowledgeJobStage(job.ID, "worker-owner", "building_knowledge")
	if err != nil || building.Stage != "building_knowledge" {
		t.Fatalf("building = %#v, err=%v", building, err)
	}

	completed, err := store.CompleteBookKnowledgeJob(job.ID, "worker-owner", map[string]any{
		"ebook_id": job.EbookID, "title": "安全标题", "path": "/private/export", "token": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != BookKnowledgeJobStatusSucceeded || completed.Stage != "completed" || completed.FinishedAt == "" ||
		completed.LeaseOwner != "" || completed.LeaseExpiresAt != "" || completed.Error != "" || completed.FailureCode != "" {
		t.Fatalf("completed job = %#v", completed)
	}
	if _, ok := completed.Result["path"]; ok {
		t.Fatalf("completed result leaked path: %#v", completed.Result)
	}
	if _, ok := completed.Result["token"]; ok {
		t.Fatalf("completed result leaked token: %#v", completed.Result)
	}
	if _, err := store.FailBookKnowledgeJob(job.ID, "worker-owner", "download_failed"); !errors.Is(err, ErrBookKnowledgeJobInvalidState) {
		t.Fatalf("terminal fail error = %v", err)
	}

	failedSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 103, EbookEnID: "structured-failure", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("worker-owner", time.Minute); err != nil {
		t.Fatal(err)
	}
	failed, err := store.FailBookKnowledgeJob(failedSource.ID, "worker-owner", "download_failed")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != BookKnowledgeJobStatusFailed || failed.Stage != "failed" || failed.FailureCode != "download_failed" ||
		failed.Error != "电子书下载失败，可以重新执行" || failed.Result != nil || failed.LeaseOwner != "" {
		t.Fatalf("failed job = %#v", failed)
	}

	interruptedSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 104, EbookEnID: "explicit-interrupt", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("worker-owner", time.Minute); err != nil {
		t.Fatal(err)
	}
	interrupted, err := store.InterruptBookKnowledgeJob(interruptedSource.ID, "worker-owner")
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.Status != BookKnowledgeJobStatusInterrupted || interrupted.Stage != "interrupted" ||
		interrupted.FailureCode != BookKnowledgeJobFailureWorkerInterrupted || interrupted.Error != "Worker 升级或异常退出，任务已中断" {
		t.Fatalf("interrupted job = %#v", interrupted)
	}

	exactOwnerSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 114, EbookEnID: "exact-owner", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("worker-exact ", time.Minute); err != nil {
		t.Fatal(err)
	}
	exactOwnerEvents := countBookKnowledgeJobEvents(t, store, exactOwnerSource.ID)
	assertRejectedWithoutBookJobMutation(t, store, exactOwnerSource.ID, exactOwnerEvents, func() error {
		_, operationErr := store.UpdateBookKnowledgeJobStage(exactOwnerSource.ID, "worker-exact", "downloading")
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)
	if _, err := store.InterruptBookKnowledgeJob(exactOwnerSource.ID, "worker-exact "); err != nil {
		t.Fatalf("exact lease owner rejected: %v", err)
	}
}

func TestBookKnowledgeJobLeaseExpiry(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	expired, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 105, EbookEnID: "expired", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 106, EbookEnID: "still-queued", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("expired-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	setBookKnowledgeJobLeaseExpiry(t, store, expired.ID, time.Now().UTC().Add(-time.Minute))
	withoutLease, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 115, EbookEnID: "missing-lease", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.updateBookKnowledgeJob(withoutLease.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		job.Status = BookKnowledgeJobStatusRunning
		job.LeaseOwner = ""
		job.LeaseExpiresAt = ""
		return job
	}); err != nil {
		t.Fatal(err)
	}

	eventsBefore := countBookKnowledgeJobEvents(t, store, expired.ID)
	assertRejectedWithoutBookJobMutation(t, store, expired.ID, eventsBefore, func() error {
		_, operationErr := store.RenewBookKnowledgeJobLease(expired.ID, "expired-worker", time.Minute)
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)
	assertRejectedWithoutBookJobMutation(t, store, expired.ID, eventsBefore, func() error {
		_, operationErr := store.UpdateBookKnowledgeJobStage(expired.ID, "expired-worker", "downloading")
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)
	assertRejectedWithoutBookJobMutation(t, store, expired.ID, eventsBefore, func() error {
		_, operationErr := store.CompleteBookKnowledgeJob(expired.ID, "expired-worker", map[string]any{"ebook_id": expired.EbookID})
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)
	assertRejectedWithoutBookJobMutation(t, store, expired.ID, eventsBefore, func() error {
		_, operationErr := store.FailBookKnowledgeJob(expired.ID, "expired-worker", "unknown_failure")
		return operationErr
	}, ErrBookKnowledgeJobLeaseLost)

	count, err := store.ReconcileExpiredBookKnowledgeJobs()
	if err != nil || count != 2 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	loadedExpired, err := store.LoadBookKnowledgeJob(expired.ID)
	if err != nil || loadedExpired.Status != BookKnowledgeJobStatusInterrupted ||
		loadedExpired.FailureCode != BookKnowledgeJobFailureWorkerInterrupted || loadedExpired.LeaseOwner != "" || loadedExpired.Result != nil {
		t.Fatalf("expired job = %#v, err=%v", loadedExpired, err)
	}
	loadedQueued, err := store.LoadBookKnowledgeJob(queued.ID)
	if err != nil || loadedQueued.Status != BookKnowledgeJobStatusQueued {
		t.Fatalf("queued job = %#v, err=%v", loadedQueued, err)
	}
	loadedWithoutLease, err := store.LoadBookKnowledgeJob(withoutLease.ID)
	if err != nil || loadedWithoutLease.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("missing lease job = %#v, err=%v", loadedWithoutLease, err)
	}
	if repeated, err := store.ReconcileExpiredBookKnowledgeJobs(); err != nil || repeated != 0 {
		t.Fatalf("repeated reconcile count=%d err=%v", repeated, err)
	}
}

func TestBookKnowledgeJobStructuredFailureCodes(t *testing.T) {
	for code, wantMessage := range map[string]string{
		BookKnowledgeJobFailureAuthenticationRequired: "登录已失效，请重新登录",
		BookKnowledgeJobFailureDownloadFailed:         "电子书下载失败，可以重新执行",
		BookKnowledgeJobFailureKnowledgeBuildFailed:   "下载完成，但知识包生成失败",
		BookKnowledgeJobFailureWorkerInterrupted:      "Worker 升级或异常退出，任务已中断",
		BookKnowledgeJobFailureSourceChanged:          "任务参数或书籍权限已经变化",
		BookKnowledgeJobFailureUnknownFailure:         "任务执行失败，请查看诊断并重试",
	} {
		t.Run(code, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			created, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
				Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 120, EbookEnID: code, DownloadType: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.ClaimNextBookKnowledgeJob("failure-worker", time.Minute); err != nil {
				t.Fatal(err)
			}
			failed, err := store.FailBookKnowledgeJob(created.ID, "failure-worker", code)
			if err != nil {
				t.Fatal(err)
			}
			if failed.Status != BookKnowledgeJobStatusFailed || failed.FailureCode != code || failed.Error != wantMessage {
				t.Fatalf("failed = %#v", failed)
			}
			var eventMessage string
			db, err := store.openBookJobsDB()
			if err != nil {
				t.Fatal(err)
			}
			err = db.QueryRow(`SELECT message FROM book_job_events WHERE job_id = ? ORDER BY event_id DESC LIMIT 1`, created.ID).Scan(&eventMessage)
			db.Close()
			if err != nil || eventMessage != wantMessage {
				t.Fatalf("event message = %q, err=%v", eventMessage, err)
			}
		})
	}
}

func TestBookKnowledgeJobQueuedRecovery(t *testing.T) {
	root := t.TempDir()
	firstStore := NewBookKnowledgeStore(root)
	created, err := firstStore.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookSyncKBase, EbookID: 107, EbookEnID: "recover-queued", DownloadType: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	reopened := NewBookKnowledgeStore(root)
	claimed, err := reopened.ClaimNextBookKnowledgeJob("replacement-worker", time.Minute)
	if err != nil || claimed == nil || claimed.ID != created.ID || claimed.Status != BookKnowledgeJobStatusRunning {
		t.Fatalf("claimed = %#v, err=%v", claimed, err)
	}
}

func TestBookKnowledgeJobRetry(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	failedOriginal, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 108, EbookEnID: "retry-failed", DownloadType: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("retry-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	failedOriginal, err = store.FailBookKnowledgeJob(failedOriginal.ID, "retry-worker", "knowledge_build_failed")
	if err != nil {
		t.Fatal(err)
	}
	originalEvents := countBookKnowledgeJobEvents(t, store, failedOriginal.ID)

	retry, err := store.RetryBookKnowledgeJob(failedOriginal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID == failedOriginal.ID || retry.RetryOf != failedOriginal.ID || retry.Status != BookKnowledgeJobStatusQueued || retry.Stage != "queued" ||
		retry.Type != failedOriginal.Type || retry.EbookID != failedOriginal.EbookID || retry.EbookEnID != failedOriginal.EbookEnID || retry.DownloadType != failedOriginal.DownloadType {
		t.Fatalf("retry = %#v, original = %#v", retry, failedOriginal)
	}
	unchanged, err := store.LoadBookKnowledgeJob(failedOriginal.ID)
	if err != nil || !reflect.DeepEqual(unchanged, failedOriginal) || countBookKnowledgeJobEvents(t, store, failedOriginal.ID) != originalEvents {
		t.Fatalf("original changed: %#v, err=%v", unchanged, err)
	}

	if _, err := store.ClaimNextBookKnowledgeJob("retry-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FailBookKnowledgeJob(retry.ID, "retry-worker", "unknown_failure"); err != nil {
		t.Fatal(err)
	}
	secondRetry, err := store.RetryBookKnowledgeJob(failedOriginal.ID)
	if err != nil || secondRetry.ID == retry.ID || secondRetry.RetryOf != failedOriginal.ID {
		t.Fatalf("second retry = %#v, err=%v", secondRetry, err)
	}

	for _, status := range []BookKnowledgeJobStatus{BookKnowledgeJobStatusQueued, BookKnowledgeJobStatusRunning, BookKnowledgeJobStatusSucceeded} {
		t.Run("reject_"+string(status), func(t *testing.T) {
			candidate, createErr := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
				Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 200 + int(status[0]), EbookEnID: "reject-" + string(status), DownloadType: 1,
			})
			if createErr != nil {
				t.Fatal(createErr)
			}
			if _, updateErr := store.updateBookKnowledgeJob(candidate.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
				job.Status = status
				return job
			}); updateErr != nil {
				t.Fatal(updateErr)
			}
			if _, retryErr := store.RetryBookKnowledgeJob(candidate.ID); !errors.Is(retryErr, ErrBookKnowledgeJobInvalidState) {
				t.Fatalf("retry %s error = %v", status, retryErr)
			}
		})
	}
}

func TestBookKnowledgeJobRetryRejectsActiveDuplicate(t *testing.T) {
	root := t.TempDir()
	firstStore := NewBookKnowledgeStore(root)
	secondStore := NewBookKnowledgeStore(root)
	original, err := firstStore.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 109, EbookEnID: "duplicate-retry", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstStore.ClaimNextBookKnowledgeJob("retry-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := firstStore.FailBookKnowledgeJob(original.ID, "retry-worker", "download_failed"); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var successful atomic.Int32
	var wait sync.WaitGroup
	for _, store := range []*BookKnowledgeStore{firstStore, secondStore} {
		wait.Add(1)
		go func(store *BookKnowledgeStore) {
			defer wait.Done()
			<-start
			if _, retryErr := store.RetryBookKnowledgeJob(original.ID); retryErr != nil {
				results <- retryErr
				return
			}
			successful.Add(1)
			results <- nil
		}(store)
	}
	close(start)
	wait.Wait()
	close(results)
	conflicts := 0
	for retryErr := range results {
		if retryErr == nil {
			continue
		}
		if !errors.Is(retryErr, ErrBookKnowledgeJobConflict) {
			t.Fatalf("duplicate retry error = %v", retryErr)
		}
		conflicts++
	}
	if successful.Load() != 1 || conflicts != 1 {
		t.Fatalf("successful=%d conflicts=%d", successful.Load(), conflicts)
	}
	if _, err := firstStore.RetryBookKnowledgeJob(original.ID); !errors.Is(err, ErrBookKnowledgeJobConflict) {
		t.Fatalf("sequential duplicate retry error = %v", err)
	}
}

func TestBookKnowledgeJobRetryDoesNotMaskConstraintFailure(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	original, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 118, EbookEnID: "retry-trigger", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("retry-trigger-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	original, err = store.FailBookKnowledgeJob(original.ID, "retry-trigger-worker", BookKnowledgeJobFailureUnknownFailure)
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore := countBookKnowledgeJobEvents(t, store, original.ID)
	db, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TRIGGER reject_book_job_retry
		BEFORE INSERT ON book_jobs
		WHEN NEW.retry_of IS NOT NULL
		BEGIN
			SELECT RAISE(ABORT, 'retry insert blocked');
		END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.RetryBookKnowledgeJob(original.ID); err == nil {
		t.Fatal("retry succeeded despite trigger")
	} else if errors.Is(err, ErrBookKnowledgeJobConflict) {
		t.Fatalf("constraint failure was reported as conflict: %v", err)
	}
	unchanged, err := store.LoadBookKnowledgeJob(original.ID)
	if err != nil || !reflect.DeepEqual(unchanged, original) {
		t.Fatalf("original = %#v, err=%v", unchanged, err)
	}
	if got := countBookKnowledgeJobEvents(t, store, original.ID); got != eventsBefore {
		t.Fatalf("events = %d, want %d", got, eventsBefore)
	}
	jobs, err := store.ListBookKnowledgeJobs(10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %#v, err=%v", jobs, err)
	}
}

func TestBookKnowledgeJobExportLegacy(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	failedSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 110, EbookEnID: "legacy-failed", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("export-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FailBookKnowledgeJob(failedSource.ID, "export-worker", "authentication_required"); err != nil {
		t.Fatal(err)
	}
	interruptedSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 111, EbookEnID: "legacy-interrupted", DownloadType: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("export-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InterruptBookKnowledgeJob(interruptedSource.ID, "export-worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.updateBookKnowledgeJob(interruptedSource.ID, func(job BookKnowledgeJob) BookKnowledgeJob {
		job.RetryOf = failedSource.ID
		return job
	}); err != nil {
		t.Fatal(err)
	}
	succeededSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 112, EbookEnID: "legacy-succeeded", DownloadType: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("export-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteBookKnowledgeJob(succeededSource.ID, "export-worker", map[string]any{
		"ebook_id": succeededSource.EbookID,
		"title":    "/private/downloaded-title",
		"path":     "/private/downloaded-book",
		"token":    "token=private-placeholder",
	}); err != nil {
		t.Fatal(err)
	}
	runningSource, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 113, EbookEnID: "legacy-running", DownloadType: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNextBookKnowledgeJob("private-worker-token", time.Minute); err != nil {
		t.Fatal(err)
	}

	exportRoot := t.TempDir()
	exportPath := filepath.Join(exportRoot, "nested", "jobs.json")
	if err := store.ExportLegacyBookKnowledgeJobs(exportPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("export permissions = %#o", info.Mode().Perm())
	}
	payload, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"retry_of", "stage", "failure_code", "lease_owner", "lease_expires_at", "private-worker-token", "/private/", "token="} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("legacy export contains %q: %s", forbidden, payload)
		}
	}
	legacy, err := readLegacyBookKnowledgeJobs(exportPath)
	if err != nil || len(legacy.Jobs) != 4 {
		t.Fatalf("legacy jobs = %#v, err=%v", legacy.Jobs, err)
	}
	if legacy.Jobs[0].ID != failedSource.ID || legacy.Jobs[1].ID != interruptedSource.ID ||
		legacy.Jobs[2].ID != succeededSource.ID || legacy.Jobs[3].ID != runningSource.ID {
		t.Fatalf("legacy order = %#v", legacy.Jobs)
	}
	byID := make(map[string]BookKnowledgeJob, len(legacy.Jobs))
	for _, job := range legacy.Jobs {
		byID[job.ID] = job
	}
	interrupted := byID[interruptedSource.ID]
	if interrupted.Status != BookKnowledgeJobStatusFailed || interrupted.Error != bookKnowledgeJobInterruptedMessage ||
		len(interrupted.Logs) == 0 || interrupted.Logs[len(interrupted.Logs)-1] != "failed: interrupted" {
		t.Fatalf("legacy interrupted = %#v", interrupted)
	}
	running := byID[runningSource.ID]
	if running.Status != BookKnowledgeJobStatusRunning || running.LeaseOwner != "" || running.LeaseExpiresAt != "" {
		t.Fatalf("legacy running = %#v", running)
	}
	failed := byID[failedSource.ID]
	if failed.Error != "job execution failed" {
		t.Fatalf("legacy failure error = %q", failed.Error)
	}
	succeeded := byID[succeededSource.ID]
	if _, ok := succeeded.Result["title"]; ok {
		t.Fatalf("legacy result leaked sensitive title: %#v", succeeded.Result)
	}
	if _, ok := succeeded.Result["path"]; ok {
		t.Fatalf("legacy result leaked path: %#v", succeeded.Result)
	}
	if got := succeeded.Result["ebook_id"]; got == nil {
		t.Fatalf("legacy result removed safe id: %#v", succeeded.Result)
	}

	reimportStore := NewBookKnowledgeStore(filepath.Join(exportRoot, "reimport"))
	if err := os.MkdirAll(filepath.Dir(reimportStore.LegacyJobsPath()), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reimportStore.LegacyJobsPath(), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	reimported, err := reimportStore.ListBookKnowledgeJobs(10)
	if err != nil || len(reimported) != 4 {
		t.Fatalf("reimported = %#v, err=%v", reimported, err)
	}
	if got := findBookKnowledgeJobByID(t, reimported, interruptedSource.ID); got.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("reimported interrupted = %#v", got)
	}

	if err := store.ExportLegacyBookKnowledgeJobs(""); err == nil {
		t.Fatal("empty export path succeeded")
	}
	failingTarget := filepath.Join(exportRoot, "existing-directory")
	if err := os.Mkdir(failingTarget, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := store.ExportLegacyBookKnowledgeJobs(failingTarget); err == nil {
		t.Fatal("export over directory succeeded")
	}
	if info, err := os.Stat(failingTarget); err != nil || !info.IsDir() {
		t.Fatalf("failed export replaced target: info=%#v err=%v", info, err)
	}
	temps, err := filepath.Glob(filepath.Join(exportRoot, ".book-jobs-export-*"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("temporary exports = %#v, err=%v", temps, err)
	}
}

func TestBookKnowledgeJobExportLegacyDirectorySyncPlatformContract(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := syncBookKnowledgeJobExportDirectory(missing, "windows"); err != nil {
		t.Fatalf("windows directory sync = %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := syncBookKnowledgeJobExportDirectory(t.TempDir(), runtime.GOOS); err != nil {
			t.Fatalf("%s directory sync = %v", runtime.GOOS, err)
		}
		if err := syncBookKnowledgeJobExportDirectory(missing, runtime.GOOS); err == nil {
			t.Fatalf("%s missing directory sync succeeded", runtime.GOOS)
		}
	}
}

func TestBookKnowledgeJobExportLegacyRejectsDatabasePaths(t *testing.T) {
	for _, test := range []struct {
		name string
		path func(*testing.T, *BookKnowledgeStore) string
	}{
		{name: "database", path: func(_ *testing.T, store *BookKnowledgeStore) string {
			return store.BookJobsDBPath()
		}},
		{name: "relative equivalent", path: func(t *testing.T, store *BookKnowledgeStore) string {
			workingDirectory, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			relative, err := filepath.Rel(workingDirectory, store.BookJobsDBPath())
			if err != nil {
				t.Fatal(err)
			}
			return relative
		}},
		{name: "wal sidecar", path: func(_ *testing.T, store *BookKnowledgeStore) string {
			return store.BookJobsDBPath() + "-wal"
		}},
		{name: "shm sidecar", path: func(_ *testing.T, store *BookKnowledgeStore) string {
			return store.BookJobsDBPath() + "-shm"
		}},
		{name: "parent symlink", path: func(t *testing.T, store *BookKnowledgeStore) string {
			alias := filepath.Join(t.TempDir(), "store-alias")
			if err := os.Symlink(filepath.Dir(store.BookJobsDBPath()), alias); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			return filepath.Join(alias, filepath.Base(store.BookJobsDBPath()))
		}},
		{name: "hardlink", path: func(t *testing.T, store *BookKnowledgeStore) string {
			hardlink := filepath.Join(t.TempDir(), "book-jobs-hardlink.sqlite3")
			if err := os.Link(store.BookJobsDBPath(), hardlink); err != nil {
				t.Skipf("hardlink unsupported: %v", err)
			}
			return hardlink
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, created := newBookKnowledgeJobExportTestStore(t)
			assertBookKnowledgeJobExportPathRejected(t, store, created.ID, test.path(t, store))
		})
	}

	t.Run("legacy rollback path allowed", func(t *testing.T) {
		store, created := newBookKnowledgeJobExportTestStore(t)
		if err := store.ExportLegacyBookKnowledgeJobs(store.LegacyJobsPath()); err != nil {
			t.Fatalf("legacy rollback path rejected: %v", err)
		}
		legacy, err := readLegacyBookKnowledgeJobs(store.LegacyJobsPath())
		if err != nil || len(legacy.Jobs) != 1 || legacy.Jobs[0].ID != created.ID {
			t.Fatalf("legacy jobs = %#v, err=%v", legacy.Jobs, err)
		}
	})
}

func newBookKnowledgeJobExportTestStore(t *testing.T) (*BookKnowledgeStore, BookKnowledgeJob) {
	t.Helper()
	store := NewBookKnowledgeStore(t.TempDir())
	created, err := store.CreateBookKnowledgeJob(BookKnowledgeJobRequest{
		Type: BookKnowledgeJobTypeDedaoEbookDownload, EbookID: 119, EbookEnID: "protected-export", DownloadType: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, created
}

func assertBookKnowledgeJobExportPathRejected(t *testing.T, store *BookKnowledgeStore, jobID, path string) {
	t.Helper()
	if err := store.ExportLegacyBookKnowledgeJobs(path); !errors.Is(err, ErrBookKnowledgeJobInvalidState) {
		t.Fatalf("export %q error = %v", path, err)
	}
	loaded, err := store.LoadBookKnowledgeJob(jobID)
	if err != nil || loaded.ID != jobID {
		t.Fatalf("loaded = %#v, err=%v", loaded, err)
	}
	jobs, err := store.ListBookKnowledgeJobs(10)
	if err != nil || len(jobs) != 1 || jobs[0].ID != jobID {
		t.Fatalf("jobs = %#v, err=%v", jobs, err)
	}
}

func countBookKnowledgeJobEvents(t *testing.T, store *BookKnowledgeStore, jobID string) int {
	t.Helper()
	db, err := store.openBookJobsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM book_job_events WHERE job_id = ?`, jobID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertRejectedWithoutBookJobMutation(
	t *testing.T,
	store *BookKnowledgeStore,
	jobID string,
	wantEvents int,
	operation func() error,
	wantError error,
) {
	t.Helper()
	before, err := store.LoadBookKnowledgeJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if err := operation(); !errors.Is(err, wantError) {
		t.Fatalf("operation error = %v, want errors.Is(%v)", err, wantError)
	}
	after, err := store.LoadBookKnowledgeJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("job changed\nbefore: %#v\nafter:  %#v", before, after)
	}
	if got := countBookKnowledgeJobEvents(t, store, jobID); got != wantEvents {
		t.Fatalf("events = %d, want %d", got, wantEvents)
	}
}

func setBookKnowledgeJobLeaseExpiry(t *testing.T, store *BookKnowledgeStore, jobID string, expiry time.Time) {
	t.Helper()
	db, err := store.openBookJobsWriteDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE book_jobs SET lease_expires_at = ? WHERE job_id = ?`, expiry.UTC().Format(time.RFC3339Nano), jobID); err != nil {
		t.Fatal(err)
	}
}

func findBookKnowledgeJobByID(t *testing.T, jobs []BookKnowledgeJob, jobID string) BookKnowledgeJob {
	t.Helper()
	for _, job := range jobs {
		if job.ID == jobID {
			return job
		}
	}
	t.Fatalf("job %q not found in %#v", jobID, jobs)
	return BookKnowledgeJob{}
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
