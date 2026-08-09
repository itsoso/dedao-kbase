package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const bookKnowledgeRootLockHelperEnv = "DEDAO_TEST_BOOK_PACKAGE_LOCK_HELPER"
const bookKnowledgeDerivedLockHelperEnv = "DEDAO_TEST_BOOK_DERIVED_LOCK_HELPER"
const bookKnowledgePublishCrashHelperEnv = "DEDAO_TEST_BOOK_PUBLISH_CRASH_HELPER"
const bookKnowledgePublishCrashJobEnv = "DEDAO_TEST_BOOK_PUBLISH_CRASH_JOB"
const bookKnowledgePublishCrashWorkerEnv = "DEDAO_TEST_BOOK_PUBLISH_CRASH_WORKER"
const bookKnowledgePublishCrashPhaseEnv = "DEDAO_TEST_BOOK_PUBLISH_CRASH_PHASE"

func TestBookKnowledgeContentHashIsStableAndTracksDurableContent(t *testing.T) {
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = ""
	pkg.Book.CreatedAt = "2026-01-01T00:00:00Z"
	pkg.Book.UpdatedAt = "2026-01-01T00:00:00Z"

	first, err := BookKnowledgeContentHash(pkg)
	if err != nil {
		t.Fatalf("BookKnowledgeContentHash returned error: %v", err)
	}
	if !strings.HasPrefix(first, "sha256:") || len(first) != len("sha256:")+64 {
		t.Fatalf("content hash = %q", first)
	}

	reordered := pkg
	reordered.Book.CreatedAt = "2027-02-02T00:00:00Z"
	reordered.Book.UpdatedAt = "2027-02-02T00:00:00Z"
	slices.Reverse(reordered.Chapters)
	slices.Reverse(reordered.Chunks)
	slices.Reverse(reordered.Claims)
	slices.Reverse(reordered.Citations)
	second, err := BookKnowledgeContentHash(reordered)
	if err != nil {
		t.Fatalf("BookKnowledgeContentHash reordered returned error: %v", err)
	}
	if second != first {
		t.Fatalf("stable hash changed: first=%s second=%s", first, second)
	}

	changed := pkg
	changed.Chunks = append([]BookKnowledgeChunk(nil), pkg.Chunks...)
	changed.Chunks[0].Text += " changed"
	third, err := BookKnowledgeContentHash(changed)
	if err != nil {
		t.Fatalf("BookKnowledgeContentHash changed returned error: %v", err)
	}
	if third == first {
		t.Fatal("durable content change did not change hash")
	}
}

func TestSavePackageAssignsMissingContentHash(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = ""
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	loaded, err := store.LoadPackage(pkg.Book.BookID)
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if !strings.HasPrefix(loaded.Book.ContentHash, "sha256:") {
		t.Fatalf("content hash = %q", loaded.Book.ContentHash)
	}
}

func TestSavePackageContextCancellationAtPublishGateLeavesNoPartialPackage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	ctx, cancel := context.WithCancel(context.Background())
	reachedGate := make(chan struct{})
	releaseGate := make(chan struct{})
	store.beforePackagePublish = func() {
		close(reachedGate)
		<-releaseGate
	}
	done := make(chan error, 1)
	go func() { done <- store.SavePackageContext(ctx, pkg) }()
	select {
	case <-reachedGate:
	case <-time.After(2 * time.Second):
		close(releaseGate)
		t.Fatal("save did not reach package publish gate")
	}
	cancel()
	close(releaseGate)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("SavePackageContext error=%v, want context canceled", err)
	}
	if _, err := os.Stat(store.ManifestPath()); !os.IsNotExist(err) {
		t.Fatalf("root manifest was partially published: %v", err)
	}
	if _, err := os.Stat(store.BookDir(pkg.Book.BookID)); !os.IsNotExist(err) {
		t.Fatalf("book directory was partially published: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(store.Root(), ".book-package-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("staging package leaked: %v, err=%v", matches, err)
	}
}

func TestSavePackageContextCancellationAfterPublishGateCommitsWholePackage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	ctx, cancel := context.WithCancel(context.Background())
	store.afterPackagePublishGate = cancel
	if err := store.SavePackageContext(ctx, pkg); err != nil {
		t.Fatalf("SavePackageContext after commit decision: %v", err)
	}
	loaded, err := store.LoadPackage(pkg.Book.BookID)
	if err != nil {
		t.Fatalf("committed package cannot be loaded: %v", err)
	}
	if loaded.Book.BookID != pkg.Book.BookID || len(loaded.Chunks) != len(pkg.Chunks) {
		t.Fatalf("partial committed package: %#v", loaded)
	}
	books, err := store.ListBooks()
	if err != nil || len(books) != 1 || books[0].BookID != pkg.Book.BookID {
		t.Fatalf("root manifest did not commit with package: books=%#v err=%v", books, err)
	}
}

func TestSavePackageContextCancellationPreservesExistingWholePackage(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	original := sampleBookKnowledgePackageForExport()
	if err := store.SavePackage(original); err != nil {
		t.Fatal(err)
	}
	updated := original
	updated.Book.Title = "replacement title"
	updated.Book.ContentHash = ""
	updated.Chunks = append([]BookKnowledgeChunk(nil), original.Chunks...)
	updated.Chunks[0].Text = "replacement chunk"
	ctx, cancel := context.WithCancel(context.Background())
	store.beforePackagePublish = cancel
	if err := store.SavePackageContext(ctx, updated); !errors.Is(err, context.Canceled) {
		t.Fatalf("SavePackageContext error=%v, want context canceled", err)
	}
	loaded, err := store.LoadPackage(original.Book.BookID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Book.Title != original.Book.Title || loaded.Chunks[0].Text != original.Chunks[0].Text {
		t.Fatalf("existing package was partially replaced: %#v", loaded)
	}
}

func TestSavePackageContextPreservesDerivedBookArtifacts(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	derivedPath := filepath.Join(store.BookDir(pkg.Book.BookID), "quality_report.json")
	if err := os.WriteFile(derivedPath, []byte("derived"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg.Book.Title = "updated"
	pkg.Book.ContentHash = ""
	if err := store.SavePackageContext(context.Background(), pkg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(derivedPath)
	if err != nil || string(data) != "derived" {
		t.Fatalf("derived artifact=%q err=%v", data, err)
	}
}

func TestRepairMissingBookContentHashCannotOverwriteConcurrentPackage(t *testing.T) {
	root := t.TempDir()
	legacyStore := NewBookKnowledgeStore(root)
	legacy := sampleBookKnowledgePackageForExport()
	legacy.Book.BookID = "repair-race"
	legacy.Book.Title = "Legacy"
	legacy.Book.ContentHash = ""
	if err := legacyStore.SavePackage(legacy); err != nil {
		t.Fatal(err)
	}
	legacyPublished, err := legacyStore.LoadPackage(legacy.Book.BookID)
	if err != nil {
		t.Fatal(err)
	}
	legacyPublished.Book.ContentHash = ""
	if err := writeJSONFile(legacyStore.BookManifestPath(legacy.Book.BookID), legacyPublished.Book); err != nil {
		t.Fatal(err)
	}
	rootManifest, err := legacyStore.loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	rootManifest.Books[0].ContentHash = ""
	if err := writeJSONFile(legacyStore.ManifestPath(), rootManifest); err != nil {
		t.Fatal(err)
	}

	repairStore := NewBookKnowledgeStore(root)
	workerStore := NewBookKnowledgeStore(root)
	repairWaitingForWriteLock := make(chan struct{})
	releaseRepair := make(chan struct{})
	repairStore.beforePackageRootLock = func() {
		close(repairWaitingForWriteLock)
		<-releaseRepair
	}
	repairDone := make(chan error, 1)
	go func() {
		_, repairErr := repairStore.RepairMissingBookContentHash(legacy.Book.BookID)
		repairDone <- repairErr
	}()
	<-repairWaitingForWriteLock

	updated := legacy
	updated.Book.Title = "Concurrent New Package"
	updated.Book.ContentHash = ""
	updated.Chunks = append([]BookKnowledgeChunk(nil), legacy.Chunks...)
	updated.Chunks[0].Text = "new worker content"
	if err := workerStore.SavePackage(updated); err != nil {
		t.Fatal(err)
	}
	published, err := workerStore.LoadPackage(updated.Book.BookID)
	if err != nil {
		t.Fatal(err)
	}
	if err := workerStore.SaveAnalysisManifest(BookAnalysisManifest{
		BookID: updated.Book.BookID, ContentHash: published.Book.ContentHash, Status: BookAnalysisReady,
	}); err != nil {
		t.Fatal(err)
	}
	if err := workerStore.SaveBookQualityReport(BookQualityReport{
		BookID: updated.Book.BookID, ContentHash: published.Book.ContentHash,
	}); err != nil {
		t.Fatal(err)
	}
	close(releaseRepair)
	if err := <-repairDone; err == nil || !strings.Contains(err.Error(), "already has content hash") {
		t.Fatalf("RepairMissingBookContentHash error=%v, want concurrent package rejection", err)
	}

	got, err := NewBookKnowledgeStore(root).LoadPackage(updated.Book.BookID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Book.Title != updated.Book.Title || got.Chunks[0].Text != updated.Chunks[0].Text ||
		got.Book.ContentHash != published.Book.ContentHash {
		t.Fatalf("repair overwrote concurrent package: %#v", got)
	}
	if _, err := NewBookKnowledgeStore(root).LoadAnalysisManifest(updated.Book.BookID); err != nil {
		t.Fatalf("repair removed concurrent analysis: %v", err)
	}
	if _, err := NewBookKnowledgeStore(root).LoadBookQualityReport(updated.Book.BookID); err != nil {
		t.Fatalf("repair removed concurrent quality report: %v", err)
	}
}

func TestReconcileCompletesFencedPublishKilledAfterBookInstall(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 293)
	original := publishCrashTestPackage(job, "Original Package")
	if err := store.SavePackage(original); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextBookKnowledgeJob("crash-worker", time.Minute)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestBookKnowledgePublishCrashSubprocessHelper$", "-test.v")
	cmd.Env = append(os.Environ(),
		bookKnowledgePublishCrashHelperEnv+"="+root,
		bookKnowledgePublishCrashJobEnv+"="+job.ID,
		bookKnowledgePublishCrashWorkerEnv+"=crash-worker",
		bookKnowledgePublishCrashPhaseEnv+"=book-installed",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForSubprocessLine(t, lines, "publish-book-installed")
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("crash helper exited successfully, want killed process")
	}

	installed, err := NewBookKnowledgeStore(root).LoadPackage("293")
	if err != nil || installed.Book.Title != "Crash Replacement" {
		t.Fatalf("installed package=%#v err=%v stderr=%s", installed, err, stderr.String())
	}
	books, err := NewBookKnowledgeStore(root).ListBooks()
	if err != nil || len(books) != 1 || books[0].Title != original.Book.Title {
		t.Fatalf("pre-recovery manifest books=%#v err=%v", books, err)
	}
	transactionMatches, err := filepath.Glob(filepath.Join(root, bookKnowledgePublishTransactionsDir, "*"))
	if err != nil || len(transactionMatches) != 1 {
		t.Fatalf("durable transaction matches=%v err=%v", transactionMatches, err)
	}
	transactionInfo, err := os.Stat(transactionMatches[0])
	if err != nil {
		t.Fatal(err)
	}
	if transactionInfo.Mode().Perm() != 0o700 {
		t.Fatalf("transaction mode=%v, want 0700", transactionInfo.Mode().Perm())
	}
	journalInfo, err := os.Stat(filepath.Join(transactionMatches[0], bookKnowledgePublishJournalFileName))
	if err != nil {
		t.Fatal(err)
	}
	if journalInfo.Mode().Perm() != 0o600 {
		t.Fatalf("journal mode=%v, want 0600", journalInfo.Mode().Perm())
	}
	setBookKnowledgeJobLeaseExpiry(t, store, job.ID, time.Now().UTC().Add(-time.Minute))
	count, err := NewBookKnowledgeStore(root).ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	recovered, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || recovered.Status != BookKnowledgeJobStatusSucceeded {
		t.Fatalf("recovered job=%#v err=%v", recovered, err)
	}
	books, err = NewBookKnowledgeStore(root).ListBooks()
	if err != nil || len(books) != 1 || books[0].Title != "Crash Replacement" {
		t.Fatalf("repaired manifest books=%#v err=%v", books, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
	assertNoBookKnowledgePublishResidue(t, root)
	count, err = NewBookKnowledgeStore(root).ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("idempotent reconcile count=%d err=%v", count, err)
	}
}

func TestReconcileRestoresOldPackageWhenKilledPublishCannotResume(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 294)
	original := publishCrashTestPackage(job, "Original Before Backup Crash")
	if err := store.SavePackage(original); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextBookKnowledgeJob("backup-crash-worker", time.Minute)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestBookKnowledgePublishCrashSubprocessHelper$", "-test.v")
	cmd.Env = append(os.Environ(),
		bookKnowledgePublishCrashHelperEnv+"="+root,
		bookKnowledgePublishCrashJobEnv+"="+job.ID,
		bookKnowledgePublishCrashWorkerEnv+"=backup-crash-worker",
		bookKnowledgePublishCrashPhaseEnv+"=book-backed-up",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForSubprocessLine(t, lines, "publish-book-backed-up")
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("backup crash helper exited successfully, want killed process")
	}
	if _, err := os.Stat(store.BookDir("294")); !os.IsNotExist(err) {
		t.Fatalf("book directory after backup crash: %v, stderr=%s", err, stderr.String())
	}
	removeCrashPublishStagedBook(t, root, "Crash Replacement")

	setBookKnowledgeJobLeaseExpiry(t, store, job.ID, time.Now().UTC().Add(-time.Minute))
	count, err := NewBookKnowledgeStore(root).ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	recovered, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || recovered.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("recovered job=%#v err=%v", recovered, err)
	}
	got, err := NewBookKnowledgeStore(root).LoadPackage("294")
	if err != nil || got.Book.Title != original.Book.Title {
		t.Fatalf("restored original package=%#v err=%v", got, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
	assertNoBookKnowledgePublishResidue(t, root)
}

func TestPendingCrashedPublishRejectsSameBookSaveAndPreservesDifferentBook(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 295)
	original := publishCrashTestPackage(job, "Original Before Superseding Save")
	if err := store.SavePackage(original); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextBookKnowledgeJob("superseded-crash-worker", time.Minute)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestBookKnowledgePublishCrashSubprocessHelper$", "-test.v")
	cmd.Env = append(os.Environ(),
		bookKnowledgePublishCrashHelperEnv+"="+root,
		bookKnowledgePublishCrashJobEnv+"="+job.ID,
		bookKnowledgePublishCrashWorkerEnv+"=superseded-crash-worker",
		bookKnowledgePublishCrashPhaseEnv+"=book-backed-up",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForSubprocessLine(t, lines, "publish-book-backed-up")
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("superseded crash helper exited successfully, want killed process")
	}

	superseding := publishCrashTestPackage(job, "Superseding Package")
	superseding.Chunks[0].Text = "superseding worker content"
	if err := NewBookKnowledgeStore(root).SavePackage(superseding); err == nil ||
		!strings.Contains(err.Error(), "pending book publish transaction") {
		t.Fatalf("superseding SavePackage error=%v, want pending transaction rejection; stderr=%s", err, stderr.String())
	}
	different := sampleBookKnowledgePackageForExport()
	different.Book.BookID = "different-during-crash"
	different.Book.Title = "Different Book Preserved"
	different.Book.ContentHash = ""
	if err := NewBookKnowledgeStore(root).SavePackage(different); err != nil {
		t.Fatalf("different-book SavePackage: %v", err)
	}
	differentPublished, err := NewBookKnowledgeStore(root).LoadPackage(different.Book.BookID)
	if err != nil {
		t.Fatal(err)
	}
	setBookKnowledgeJobLeaseExpiry(t, store, job.ID, time.Now().UTC().Add(-time.Minute))
	count, err := NewBookKnowledgeStore(root).ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	recovered, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || recovered.Status != BookKnowledgeJobStatusSucceeded {
		t.Fatalf("recovered job=%#v err=%v", recovered, err)
	}
	got, err := NewBookKnowledgeStore(root).LoadPackage("295")
	if err != nil || got.Book.Title != "Crash Replacement" {
		t.Fatalf("recovered crashed package=%#v err=%v", got, err)
	}
	books, err := NewBookKnowledgeStore(root).ListBooks()
	if err != nil || len(books) != 2 {
		t.Fatalf("root manifest books=%#v err=%v", books, err)
	}
	manifestHashes := map[string]string{}
	for _, book := range books {
		manifestHashes[book.BookID] = book.ContentHash
	}
	if manifestHashes["295"] != got.Book.ContentHash ||
		manifestHashes[different.Book.BookID] != differentPublished.Book.ContentHash {
		t.Fatalf("recovery replaced current root manifest: %#v", books)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
	assertNoBookKnowledgePublishResidue(t, root)
}

func TestCorruptPublishJournalBlocksPackageWritesWithoutDeletingEvidence(t *testing.T) {
	root := t.TempDir()
	transactionRoot := filepath.Join(root, bookKnowledgePublishTransactionsDir, "corrupt-transaction")
	if err := os.MkdirAll(transactionRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(transactionRoot, bookKnowledgePublishJournalFileName)
	if err := os.WriteFile(journalPath, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "blocked-by-corrupt-journal"
	pkg.Book.ContentHash = ""
	err := NewBookKnowledgeStore(root).SavePackage(pkg)
	if err == nil || !strings.Contains(err.Error(), "corrupt book publish transaction") {
		t.Fatalf("SavePackage error=%v, want corrupt transaction rejection", err)
	}
	data, readErr := os.ReadFile(journalPath)
	if readErr != nil || string(data) != "{corrupt" {
		t.Fatalf("corrupt journal evidence=%q err=%v", data, readErr)
	}
	if _, statErr := os.Stat(NewBookKnowledgeStore(root).BookDir(pkg.Book.BookID)); !os.IsNotExist(statErr) {
		t.Fatalf("blocked package path error=%v, want not exist", statErr)
	}
}

func TestReconcileCleansExactPreparingTransactionWithoutChangingOldPackage(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 296)
	original := publishCrashTestPackage(job, "Original Before Preparing Crash")
	if err := store.SavePackage(original); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextBookKnowledgeJob("preparing-crash-worker", time.Minute)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
	}
	replacement := publishCrashTestPackage(job, "Preparing Replacement")
	hash, err := BookKnowledgeContentHash(replacement)
	if err != nil {
		t.Fatal(err)
	}
	_, nonce, err := store.fenceBookKnowledgeJobPackageCommit(
		job.ID, "preparing-crash-worker", replacement.Book.BookID, hash, bookJobWorkerCommitLeaseWindow,
	)
	if err != nil {
		t.Fatal(err)
	}
	marker := bookKnowledgeJobCommitMarker{
		Version: bookKnowledgeJobCommitMarkerVersion, JobID: job.ID, PublishNonce: nonce,
		BookID: replacement.Book.BookID, ContentHash: hash,
	}
	preparingRoot := bookKnowledgePublishTransactionPath(root, marker) + ".preparing"
	if err := os.MkdirAll(preparingRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(preparingRoot, bookKnowledgePublishJournalFileName), []byte("{partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	setBookKnowledgeJobLeaseExpiry(t, store, job.ID, time.Now().UTC().Add(-time.Minute))
	count, err := NewBookKnowledgeStore(root).ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	recovered, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || recovered.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("preparing crash job=%#v err=%v", recovered, err)
	}
	got, err := NewBookKnowledgeStore(root).LoadPackage("296")
	if err != nil || got.Book.Title != original.Book.Title {
		t.Fatalf("original package=%#v err=%v", got, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
	assertNoBookKnowledgePublishResidue(t, root)
}

func TestReconcileRetainsAmbiguousPublishTransactionEvidence(t *testing.T) {
	for index, stagedValid := range []bool{true, false} {
		name := "staged_invalid"
		if stagedValid {
			name = "staged_valid"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store := NewBookKnowledgeStore(root)
			ebookID := 297 + index
			job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, ebookID)
			original := publishCrashTestPackage(job, "Original Before Ambiguous State")
			if err := store.SavePackage(original); err != nil {
				t.Fatal(err)
			}
			workerID := fmt.Sprintf("ambiguous-worker-%d", index)
			claimed, err := store.ClaimNextBookKnowledgeJob(workerID, time.Minute)
			if err != nil || claimed == nil || claimed.ID != job.ID {
				t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
			}
			killBookKnowledgePublishHelper(t, root, job.ID, workerID, "book-backed-up", "publish-book-backed-up")
			if !stagedValid {
				removeCrashPublishStagedBook(t, root, "Crash Replacement")
			}

			externalRoot := t.TempDir()
			externalStore := NewBookKnowledgeStore(externalRoot)
			external := publishCrashTestPackage(job, "Externally Installed Package")
			if err := externalStore.SavePackage(external); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(externalStore.BookDir(external.Book.BookID), store.BookDir(external.Book.BookID)); err != nil {
				t.Fatal(err)
			}
			setBookKnowledgeJobLeaseExpiry(t, store, job.ID, time.Now().UTC().Add(-time.Minute))
			count, err := NewBookKnowledgeStore(root).ReconcileExpiredBookKnowledgeJobsContext(context.Background())
			if err == nil || count != 0 || !strings.Contains(err.Error(), "ambiguous book publish transaction") {
				t.Fatalf("reconcile count=%d err=%v, want retained ambiguous state", count, err)
			}
			running, err := store.LoadBookKnowledgeJob(job.ID)
			if err != nil || running.Status != BookKnowledgeJobStatusRunning {
				t.Fatalf("ambiguous job=%#v err=%v", running, err)
			}
			assertBookJobCommitReceiptCount(t, store, job.ID, 1)
			matches, err := filepath.Glob(filepath.Join(root, bookKnowledgePublishTransactionsDir, "*", "backup-book"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("retained backup matches=%v err=%v", matches, err)
			}
			got, err := NewBookKnowledgeStore(root).LoadPackage(external.Book.BookID)
			if err != nil || got.Book.Title != external.Book.Title {
				t.Fatalf("external package=%#v err=%v", got, err)
			}
		})
	}
}

func TestReconcileRemovesOwnedDamagedFinalFromFirstPublish(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	job := createWorkerTestJob(t, store, BookKnowledgeJobTypeDedaoEbookSyncKBase, 301)
	claimed, err := store.ClaimNextBookKnowledgeJob("damaged-first-publish-worker", time.Minute)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("ClaimNextBookKnowledgeJob=%#v err=%v", claimed, err)
	}
	killBookKnowledgePublishHelper(
		t, root, job.ID, "damaged-first-publish-worker", "book-installed", "publish-book-installed",
	)
	if err := os.Remove(store.BookJSONLPath("301", "chunks")); err != nil {
		t.Fatal(err)
	}
	setBookKnowledgeJobLeaseExpiry(t, store, job.ID, time.Now().UTC().Add(-time.Minute))
	count, err := NewBookKnowledgeStore(root).ReconcileExpiredBookKnowledgeJobsContext(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	recovered, err := store.LoadBookKnowledgeJob(job.ID)
	if err != nil || recovered.Status != BookKnowledgeJobStatusInterrupted {
		t.Fatalf("damaged first publish job=%#v err=%v", recovered, err)
	}
	if _, err := os.Stat(store.BookDir("301")); !os.IsNotExist(err) {
		t.Fatalf("owned damaged final error=%v, want removed", err)
	}
	books, err := store.ListBooks()
	if err != nil || len(books) != 0 {
		t.Fatalf("root manifest books=%#v err=%v, want no damaged publish", books, err)
	}
	assertBookJobCommitReceiptCount(t, store, job.ID, 0)
	assertNoBookKnowledgePublishResidue(t, root)
}

func TestRecoverBookKnowledgePublishTransactionStateMatrix(t *testing.T) {
	type expectedState struct {
		recovered bool
		errorIs   error
		cleanup   bool
		final     string
	}
	tests := []struct {
		name     string
		staged   string
		final    string
		backup   bool
		expected expectedState
	}{
		{name: "staged valid final absent no backup", staged: "valid", expected: expectedState{recovered: true, cleanup: true, final: "new"}},
		{name: "staged valid final absent backup", staged: "valid", backup: true, expected: expectedState{recovered: true, cleanup: true, final: "new"}},
		{name: "staged valid final old no backup", staged: "valid", final: "unknown", expected: expectedState{recovered: true, cleanup: true, final: "new"}},
		{name: "staged valid final unknown backup", staged: "valid", final: "unknown", backup: true, expected: expectedState{errorIs: errBookKnowledgePublishAmbiguous, final: "unknown"}},
		{name: "staged invalid final valid no backup", staged: "invalid", final: "valid", expected: expectedState{recovered: true, cleanup: true, final: "new"}},
		{name: "staged invalid final valid backup", staged: "invalid", final: "valid", backup: true, expected: expectedState{recovered: true, cleanup: true, final: "new"}},
		{name: "staged invalid final owned invalid no backup", staged: "invalid", final: "owned_invalid", expected: expectedState{cleanup: true}},
		{name: "staged invalid final owned invalid backup", staged: "invalid", final: "owned_invalid", backup: true, expected: expectedState{cleanup: true, final: "old"}},
		{name: "staged invalid final unknown no backup", staged: "invalid", final: "unknown", expected: expectedState{errorIs: errBookKnowledgePublishAmbiguous, final: "unknown"}},
		{name: "staged invalid final unknown backup", staged: "invalid", final: "unknown", backup: true, expected: expectedState{errorIs: errBookKnowledgePublishAmbiguous, final: "unknown"}},
		{name: "staged invalid final absent no backup", staged: "invalid", expected: expectedState{errorIs: errBookKnowledgePublishRecoveryRequired}},
		{name: "staged invalid final absent backup", staged: "invalid", backup: true, expected: expectedState{cleanup: true, final: "old"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := NewBookKnowledgeStore(root)
			job := BookKnowledgeJob{ID: "matrix-job", Type: BookKnowledgeJobTypeDedaoEbookSyncKBase, EbookID: 302, EbookEnID: "worker-302"}
			newPackage := publishCrashTestPackage(job, "Matrix New Package")
			hash, err := BookKnowledgeContentHash(newPackage)
			if err != nil {
				t.Fatal(err)
			}
			newPackage.Book.ContentHash = hash
			marker := bookKnowledgeJobCommitMarker{
				Version: bookKnowledgeJobCommitMarkerVersion, JobID: job.ID, PublishNonce: strings.Repeat("a", 32),
				BookID: newPackage.Book.BookID, ContentHash: hash,
			}
			receipt := bookKnowledgeJobCommitReceipt{
				JobID: job.ID, WorkerID: "matrix-worker", BookID: marker.BookID,
				ContentHash: marker.ContentHash, PublishNonce: marker.PublishNonce, PreparedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			transactionRoot, err := createBookKnowledgePublishTransaction(root, marker)
			if err != nil {
				t.Fatal(err)
			}
			journal := bookKnowledgePublishJournal{
				Version: bookKnowledgePublishJournalVersion, Marker: marker, Phase: bookKnowledgePublishPhasePrepared,
				BookExisted: test.backup,
			}
			if err := writeBookKnowledgePublishJournal(transactionRoot, journal); err != nil {
				t.Fatal(err)
			}
			if test.staged != "" {
				writeBookKnowledgePublishStatePackage(t, filepath.Join(transactionRoot, "book"), newPackage, marker, test.staged == "invalid")
			}
			switch test.final {
			case "valid":
				writeBookKnowledgePublishStatePackage(t, store.BookDir(marker.BookID), newPackage, marker, false)
			case "owned_invalid":
				writeBookKnowledgePublishStatePackage(t, store.BookDir(marker.BookID), newPackage, marker, true)
			case "unknown":
				unknown := publishCrashTestPackage(job, "Matrix Unknown Package")
				unknownHash, hashErr := BookKnowledgeContentHash(unknown)
				if hashErr != nil {
					t.Fatal(hashErr)
				}
				unknown.Book.ContentHash = unknownHash
				writeBookKnowledgePublishStatePackage(t, store.BookDir(marker.BookID), unknown, bookKnowledgeJobCommitMarker{}, false)
			}
			if test.backup {
				oldPackage := publishCrashTestPackage(job, "Matrix Old Package")
				oldHash, hashErr := BookKnowledgeContentHash(oldPackage)
				if hashErr != nil {
					t.Fatal(hashErr)
				}
				oldPackage.Book.ContentHash = oldHash
				writeBookKnowledgePublishStatePackage(t, filepath.Join(transactionRoot, "backup-book"), oldPackage, bookKnowledgeJobCommitMarker{}, false)
			}

			recovered, err := store.recoverBookKnowledgePublishTransaction(job, receipt)
			if recovered != test.expected.recovered || !errors.Is(err, test.expected.errorIs) {
				t.Fatalf("recover=%t err=%v, want recover=%t error=%v", recovered, err, test.expected.recovered, test.expected.errorIs)
			}
			_, statErr := os.Stat(transactionRoot)
			if test.expected.cleanup && !os.IsNotExist(statErr) {
				t.Fatalf("transaction cleanup error=%v, want removed", statErr)
			}
			if !test.expected.cleanup && statErr != nil {
				t.Fatalf("retained transaction error=%v", statErr)
			}
			got, loadErr := store.LoadPackage(marker.BookID)
			switch test.expected.final {
			case "new":
				if loadErr != nil || got.Book.Title != "Matrix New Package" {
					t.Fatalf("new final=%#v err=%v", got, loadErr)
				}
			case "old":
				if loadErr != nil || got.Book.Title != "Matrix Old Package" {
					t.Fatalf("old final=%#v err=%v", got, loadErr)
				}
			case "unknown":
				if loadErr != nil || got.Book.Title != "Matrix Unknown Package" {
					t.Fatalf("unknown final=%#v err=%v", got, loadErr)
				}
			default:
				if !os.IsNotExist(loadErr) {
					t.Fatalf("absent final=%#v err=%v", got, loadErr)
				}
			}
		})
	}
}

func writeBookKnowledgePublishStatePackage(
	t *testing.T,
	bookDir string,
	pkg BookKnowledgePackage,
	marker bookKnowledgeJobCommitMarker,
	damaged bool,
) {
	t.Helper()
	if err := os.MkdirAll(bookDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(bookDir, "manifest.json"), pkg.Book); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLFile(filepath.Join(bookDir, "chapters.jsonl"), pkg.Chapters); err != nil {
		t.Fatal(err)
	}
	if !damaged {
		if err := writeJSONLFile(filepath.Join(bookDir, "chunks.jsonl"), pkg.Chunks); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeJSONLFile(filepath.Join(bookDir, "claims.jsonl"), pkg.Claims); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLFile(filepath.Join(bookDir, "citations.jsonl"), pkg.Citations); err != nil {
		t.Fatal(err)
	}
	if marker.Version != "" {
		if err := writeBookKnowledgeJobCommitMarker(bookDir, marker); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBookKnowledgePublishCrashSubprocessHelper(t *testing.T) {
	root := os.Getenv(bookKnowledgePublishCrashHelperEnv)
	if root == "" {
		return
	}
	jobID := os.Getenv(bookKnowledgePublishCrashJobEnv)
	workerID := os.Getenv(bookKnowledgePublishCrashWorkerEnv)
	store := NewBookKnowledgeStore(root)
	job, err := store.LoadBookKnowledgeJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	switch os.Getenv(bookKnowledgePublishCrashPhaseEnv) {
	case "book-backed-up":
		store.afterPackageBookBackup = func() error {
			fmt.Println("publish-book-backed-up")
			_ = os.Stdout.Sync()
			select {}
		}
	default:
		store.afterPackageBookInstall = func() error {
			fmt.Println("publish-book-installed")
			_ = os.Stdout.Sync()
			select {}
		}
	}
	pkg := publishCrashTestPackage(job, "Crash Replacement")
	ctx := contextWithBookKnowledgePackageCommitFence(context.Background(), bookKnowledgePackageCommitFence{
		prepare: func(_ context.Context, candidate BookKnowledgePackage) (bookKnowledgeJobCommitMarker, error) {
			_, nonce, fenceErr := store.fenceBookKnowledgeJobPackageCommit(
				job.ID, workerID, candidate.Book.BookID, candidate.Book.ContentHash, bookJobWorkerCommitLeaseWindow,
			)
			return bookKnowledgeJobCommitMarker{
				Version: bookKnowledgeJobCommitMarkerVersion, JobID: job.ID, PublishNonce: nonce,
				BookID: candidate.Book.BookID, ContentHash: candidate.Book.ContentHash,
			}, fenceErr
		},
		discard: func(candidate BookKnowledgePackage, marker bookKnowledgeJobCommitMarker) error {
			return store.discardBookKnowledgeJobCommitReceipt(
				job.ID, workerID, candidate.Book.BookID, candidate.Book.ContentHash, marker.PublishNonce,
			)
		},
	})
	if err := store.SavePackageContext(ctx, pkg); err != nil {
		t.Fatal(err)
	}
}

func publishCrashTestPackage(job BookKnowledgeJob, title string) BookKnowledgePackage {
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = fmt.Sprintf("%d", job.EbookID)
	pkg.Book.DedaoID = job.EbookID
	pkg.Book.EnID = job.EbookEnID
	pkg.Book.Title = title
	pkg.Book.ContentHash = ""
	pkg.Chunks = append([]BookKnowledgeChunk(nil), pkg.Chunks...)
	pkg.Chunks[0].BookID = pkg.Book.BookID
	pkg.Chunks[0].Text = title
	return pkg
}

func killBookKnowledgePublishHelper(t *testing.T, root, jobID, workerID, phase, signal string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBookKnowledgePublishCrashSubprocessHelper$", "-test.v")
	cmd.Env = append(os.Environ(),
		bookKnowledgePublishCrashHelperEnv+"="+root,
		bookKnowledgePublishCrashJobEnv+"="+jobID,
		bookKnowledgePublishCrashWorkerEnv+"="+workerID,
		bookKnowledgePublishCrashPhaseEnv+"="+phase,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForSubprocessLine(t, lines, signal)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatalf("publish crash helper exited successfully, want killed process; stderr=%s", stderr.String())
	}
}

func assertNoBookKnowledgePublishResidue(t *testing.T, root string) {
	t.Helper()
	for _, pattern := range []string{".book-package-*", ".book-publish-transactions/*"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil || len(matches) != 0 {
			t.Fatalf("publish residue for %q: %v, err=%v", pattern, matches, err)
		}
	}
}

func removeCrashPublishStagedBook(t *testing.T, root, title string) {
	t.Helper()
	patterns := []string{".book-package-*", ".book-publish-transactions/*"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range matches {
			candidate := filepath.Join(match, "book")
			var book BookKnowledgeBook
			if err := readJSONFile(filepath.Join(candidate, "manifest.json"), &book); err != nil {
				continue
			}
			if book.Title == title {
				if err := os.RemoveAll(candidate); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
	}
	t.Fatal("crashed publish staging book not found")
}

func TestSavePackageContextSerializesIndependentStoresWithoutLosingManifestBooks(t *testing.T) {
	root := t.TempDir()
	storeA := NewBookKnowledgeStore(root)
	storeB := NewBookKnowledgeStore(root)
	pkgA := sampleBookKnowledgePackageForExport()
	pkgA.Book.BookID = "concurrent-a"
	pkgA.Book.Title = "Concurrent A"
	pkgA.Book.ContentHash = ""
	pkgB := sampleBookKnowledgePackageForExport()
	pkgB.Book.BookID = "concurrent-b"
	pkgB.Book.Title = "Concurrent B"
	pkgB.Book.ContentHash = ""

	aReadManifest := make(chan struct{})
	releaseA := make(chan struct{})
	bAttemptedLock := make(chan struct{})
	storeA.afterPackageManifestRead = func() {
		close(aReadManifest)
		<-releaseA
	}
	storeB.beforePackageRootLock = func() { close(bAttemptedLock) }

	aDone := make(chan error, 1)
	bDone := make(chan error, 1)
	go func() { aDone <- storeA.SavePackageContext(context.Background(), pkgA) }()
	<-aReadManifest
	go func() { bDone <- storeB.SavePackageContext(context.Background(), pkgB) }()
	<-bAttemptedLock
	close(releaseA)
	if err := <-aDone; err != nil {
		t.Fatalf("store A SavePackageContext: %v", err)
	}
	if err := <-bDone; err != nil {
		t.Fatalf("store B SavePackageContext: %v", err)
	}

	books, err := NewBookKnowledgeStore(root).ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 || books[0].BookID != "concurrent-a" || books[1].BookID != "concurrent-b" {
		t.Fatalf("manifest books=%#v, want both concurrent packages", books)
	}
	for _, bookID := range []string{"concurrent-a", "concurrent-b"} {
		if _, err := NewBookKnowledgeStore(root).LoadPackage(bookID); err != nil {
			t.Fatalf("LoadPackage(%q): %v", bookID, err)
		}
	}
}

func TestSavePackageContextRollbackCannotDeleteIndependentStoreCommit(t *testing.T) {
	root := t.TempDir()
	initialStore := NewBookKnowledgeStore(root)
	initial := sampleBookKnowledgePackageForExport()
	initial.Book.BookID = "shared-book"
	initial.Book.Title = "Initial"
	initial.Book.ContentHash = ""
	if err := initialStore.SavePackage(initial); err != nil {
		t.Fatal(err)
	}

	storeA := NewBookKnowledgeStore(root)
	storeB := NewBookKnowledgeStore(root)
	pkgA := initial
	pkgA.Book.Title = "Failed A"
	pkgA.Book.ContentHash = ""
	pkgB := initial
	pkgB.Book.Title = "Committed B"
	pkgB.Book.ContentHash = ""

	aInstalled := make(chan struct{})
	releaseAFailure := make(chan struct{})
	bAttemptedLock := make(chan struct{})
	storeA.afterPackageBookInstall = func() error {
		close(aInstalled)
		<-releaseAFailure
		return errors.New("injected manifest publish failure")
	}
	storeB.beforePackageRootLock = func() { close(bAttemptedLock) }
	aDone := make(chan error, 1)
	bDone := make(chan error, 1)
	go func() { aDone <- storeA.SavePackageContext(context.Background(), pkgA) }()
	<-aInstalled
	go func() { bDone <- storeB.SavePackageContext(context.Background(), pkgB) }()
	<-bAttemptedLock
	close(releaseAFailure)
	if err := <-aDone; err == nil || !strings.Contains(err.Error(), "injected manifest publish failure") {
		t.Fatalf("store A error=%v", err)
	}
	if err := <-bDone; err != nil {
		t.Fatalf("store B SavePackageContext: %v", err)
	}
	loaded, err := NewBookKnowledgeStore(root).LoadPackage("shared-book")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Book.Title != "Committed B" {
		t.Fatalf("final title=%q, want independent store commit", loaded.Book.Title)
	}
}

func TestSavePackageContextRootLockWaitHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	storeA := NewBookKnowledgeStore(root)
	storeB := NewBookKnowledgeStore(root)
	pkgA := sampleBookKnowledgePackageForExport()
	pkgA.Book.BookID = "lock-holder"
	pkgA.Book.ContentHash = ""
	pkgB := sampleBookKnowledgePackageForExport()
	pkgB.Book.BookID = "lock-waiter"
	pkgB.Book.ContentHash = ""

	aAtGate := make(chan struct{})
	releaseA := make(chan struct{})
	bAttemptedLock := make(chan struct{})
	storeA.beforePackagePublish = func() {
		close(aAtGate)
		<-releaseA
	}
	storeB.beforePackageRootLock = func() { close(bAttemptedLock) }
	aDone := make(chan error, 1)
	go func() { aDone <- storeA.SavePackageContext(context.Background(), pkgA) }()
	<-aAtGate
	ctx, cancel := context.WithCancel(context.Background())
	bDone := make(chan error, 1)
	go func() { bDone <- storeB.SavePackageContext(ctx, pkgB) }()
	<-bAttemptedLock
	cancel()
	select {
	case err := <-bDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting SavePackageContext error=%v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiting SavePackageContext did not stop after cancellation")
	}
	close(releaseA)
	if err := <-aDone; err != nil {
		t.Fatal(err)
	}
	if _, err := NewBookKnowledgeStore(root).LoadPackage("lock-waiter"); !os.IsNotExist(err) {
		t.Fatalf("canceled waiter package error=%v, want not exist", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if rootInfo.Mode().Perm() != 0o700 {
		t.Fatalf("root mode=%v, want 0700", rootInfo.Mode().Perm())
	}
	lockInfo, err := os.Stat(filepath.Join(root, bookKnowledgeRootLockFileName))
	if err != nil {
		t.Fatal(err)
	}
	if lockInfo.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode=%v, want 0600", lockInfo.Mode().Perm())
	}
}

func TestBookKnowledgeRootLockCancellationAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	rootLock, err := store.acquireBookKnowledgeRootLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rootLock.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestBookKnowledgeRootLockSubprocessHelper$", "-test.v")
	cmd.Env = append(os.Environ(), bookKnowledgeRootLockHelperEnv+"="+root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForSubprocessLine(t, lines, "package-lock-attempted")
	if _, err := stdin.Write([]byte("cancel\n")); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	waitForSubprocessLine(t, lines, "package-lock-canceled")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("lock helper: %v", err)
	}
}

func TestBookKnowledgeReaderWaitsForWholePackagePublishAcrossStores(t *testing.T) {
	root := t.TempDir()
	writer := NewBookKnowledgeStore(root)
	reader := NewBookKnowledgeStore(root)
	initial := sampleBookKnowledgePackageForExport()
	initial.Book.BookID = "reader-snapshot"
	initial.Book.Title = "Initial Snapshot"
	initial.Book.ContentHash = ""
	initial.Chunks[0].Text = "old-snapshot-marker"
	if err := writer.SavePackage(initial); err != nil {
		t.Fatal(err)
	}
	updated := initial
	updated.Book.Title = "Updated Snapshot"
	updated.Book.ContentHash = ""
	updated.Chunks = append([]BookKnowledgeChunk(nil), initial.Chunks...)
	updated.Chunks[0].Text = "new-snapshot-marker"

	bookInstalled := make(chan struct{})
	releasePublish := make(chan struct{})
	readerAttempted := make(chan struct{})
	writer.afterPackageBookInstall = func() error {
		close(bookInstalled)
		<-releasePublish
		return nil
	}
	reader.beforePackageRootReadLock = func() { close(readerAttempted) }
	writeDone := make(chan error, 1)
	go func() { writeDone <- writer.SavePackageContext(context.Background(), updated) }()
	<-bookInstalled
	type searchResult struct {
		results []BookKnowledgeSearchResult
		err     error
	}
	readDone := make(chan searchResult, 1)
	go func() {
		results, err := reader.Search(BookKnowledgeSearchQuery{Query: "new-snapshot-marker"})
		readDone <- searchResult{results: results, err: err}
	}()
	<-readerAttempted
	close(releasePublish)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	read := <-readDone
	if read.err != nil {
		t.Fatal(read.err)
	}
	if len(read.results) != 1 || read.results[0].BookTitle != "Updated Snapshot" ||
		!strings.Contains(read.results[0].Snippet, "new-snapshot-marker") {
		t.Fatalf("reader observed mixed package/catalog snapshot: %#v", read.results)
	}
}

func TestDerivedArtifactWriteWaitsForPackageSwapAndIsNotLost(t *testing.T) {
	root := t.TempDir()
	packageStore := NewBookKnowledgeStore(root)
	derivedStore := NewBookKnowledgeStore(root)
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "derived-write"
	pkg.Book.ContentHash = ""
	if err := packageStore.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	updated := pkg
	updated.Book.Title = "Updated Package"
	updated.Book.ContentHash = ""
	packageAtGate := make(chan struct{})
	releasePackage := make(chan struct{})
	derivedAttempted := make(chan struct{})
	packageStore.beforePackagePublish = func() {
		close(packageAtGate)
		<-releasePackage
	}
	derivedStore.beforePackageRootLock = func() { close(derivedAttempted) }
	packageDone := make(chan error, 1)
	go func() { packageDone <- packageStore.SavePackageContext(context.Background(), updated) }()
	<-packageAtGate
	derivedDone := make(chan error, 1)
	go func() {
		derivedDone <- derivedStore.SaveAnalysisManifestContext(context.Background(), BookAnalysisManifest{
			BookID: pkg.Book.BookID, ContentHash: "derived-hash", Status: BookAnalysisReady,
		})
	}()
	<-derivedAttempted
	close(releasePackage)
	if err := <-packageDone; err != nil {
		t.Fatal(err)
	}
	if err := <-derivedDone; err != nil {
		t.Fatal(err)
	}
	manifest, err := NewBookKnowledgeStore(root).LoadAnalysisManifest(pkg.Book.BookID)
	if err != nil || manifest.ContentHash != "derived-hash" {
		t.Fatalf("derived manifest=%#v err=%v", manifest, err)
	}
}

func TestDerivedArtifactReadWaitsForWholePackageSnapshot(t *testing.T) {
	root := t.TempDir()
	packageStore := NewBookKnowledgeStore(root)
	reader := NewBookKnowledgeStore(root)
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "derived-read"
	pkg.Book.ContentHash = ""
	if err := packageStore.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	if err := packageStore.SaveBookQualityReport(BookQualityReport{
		BookID: pkg.Book.BookID, ContentHash: "before-swap", Decision: BookQualityPass,
	}); err != nil {
		t.Fatal(err)
	}
	updated := pkg
	updated.Book.Title = "Updated Package"
	updated.Book.ContentHash = ""
	bookInstalled := make(chan struct{})
	releasePublish := make(chan struct{})
	readerAttempted := make(chan struct{})
	packageStore.afterPackageBookInstall = func() error {
		close(bookInstalled)
		<-releasePublish
		return nil
	}
	reader.beforePackageRootReadLock = func() { close(readerAttempted) }
	packageDone := make(chan error, 1)
	go func() { packageDone <- packageStore.SavePackageContext(context.Background(), updated) }()
	<-bookInstalled
	type qualityResult struct {
		report *BookQualityReport
		err    error
	}
	readDone := make(chan qualityResult, 1)
	go func() {
		report, err := reader.LoadBookQualityReportContext(context.Background(), pkg.Book.BookID)
		readDone <- qualityResult{report: report, err: err}
	}()
	<-readerAttempted
	close(releasePublish)
	if err := <-packageDone; err != nil {
		t.Fatal(err)
	}
	read := <-readDone
	if read.err != nil || read.report.ContentHash != "before-swap" {
		t.Fatalf("derived read=%#v err=%v", read.report, read.err)
	}
}

func TestDerivedArtifactLockWaitHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	packageStore := NewBookKnowledgeStore(root)
	derivedStore := NewBookKnowledgeStore(root)
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "derived-cancel"
	pkg.Book.ContentHash = ""
	if err := packageStore.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	packageAtGate := make(chan struct{})
	releasePackage := make(chan struct{})
	derivedAttempted := make(chan struct{})
	packageStore.beforePackagePublish = func() {
		close(packageAtGate)
		<-releasePackage
	}
	derivedStore.beforePackageRootLock = func() { close(derivedAttempted) }
	packageDone := make(chan error, 1)
	go func() { packageDone <- packageStore.SavePackageContext(context.Background(), pkg) }()
	<-packageAtGate
	ctx, cancel := context.WithCancel(context.Background())
	derivedDone := make(chan error, 1)
	go func() {
		derivedDone <- derivedStore.SaveAnalysisManifestContext(ctx, BookAnalysisManifest{BookID: pkg.Book.BookID})
	}()
	<-derivedAttempted
	cancel()
	select {
	case err := <-derivedDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("derived write error=%v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("derived write did not stop after cancellation")
	}
	close(releasePackage)
	if err := <-packageDone; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(packageStore.BookAnalysisManifestPath(pkg.Book.BookID)); !os.IsNotExist(err) {
		t.Fatalf("canceled derived artifact error=%v, want not exist", err)
	}
}

func TestBookKnowledgeRootLockSubprocessHelper(t *testing.T) {
	root := os.Getenv(bookKnowledgeRootLockHelperEnv)
	if root == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		cancel()
	}()
	store := NewBookKnowledgeStore(root)
	store.beforePackageRootLock = func() { fmt.Println("package-lock-attempted") }
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "subprocess-waiter"
	pkg.Book.ContentHash = ""
	if err := store.SavePackageContext(ctx, pkg); !errors.Is(err, context.Canceled) {
		t.Fatalf("SavePackageContext error=%v, want context canceled", err)
	}
	fmt.Println("package-lock-canceled")
}

func TestDerivedArtifactLockCancellationAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	rootLock, err := store.acquireBookKnowledgeRootLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rootLock.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestDerivedArtifactLockSubprocessHelper$", "-test.v")
	cmd.Env = append(os.Environ(), bookKnowledgeDerivedLockHelperEnv+"="+root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForSubprocessLine(t, lines, "derived-lock-attempted")
	if _, err := stdin.Write([]byte("cancel\n")); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	waitForSubprocessLine(t, lines, "derived-lock-canceled")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("derived lock helper: %v", err)
	}
}

func TestDerivedArtifactLockSubprocessHelper(t *testing.T) {
	root := os.Getenv(bookKnowledgeDerivedLockHelperEnv)
	if root == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		cancel()
	}()
	store := NewBookKnowledgeStore(root)
	store.beforePackageRootLock = func() { fmt.Println("derived-lock-attempted") }
	err := store.SaveAnalysisManifestContext(ctx, BookAnalysisManifest{BookID: "subprocess-derived"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SaveAnalysisManifestContext error=%v, want context canceled", err)
	}
	fmt.Println("derived-lock-canceled")
}

func waitForSubprocessLine(t *testing.T, lines <-chan string, want string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("subprocess output ended before %q", want)
			}
			if strings.Contains(line, want) {
				return
			}
		case <-timer.C:
			t.Fatalf("subprocess did not report %q", want)
		}
	}
}

func TestBookKnowledgeStorePaths(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)

	assertPath(t, store.ManifestPath(), filepath.Join(root, "manifest.json"))
	assertPath(t, store.BookDir("book-1"), filepath.Join(root, "books", "book-1"))
	assertPath(t, store.BookManifestPath("book-1"), filepath.Join(root, "books", "book-1", "manifest.json"))
	assertPath(t, store.BookJSONLPath("book-1", "chapters"), filepath.Join(root, "books", "book-1", "chapters.jsonl"))
}

func TestBookKnowledgePackageRoundTrip(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:        "42",
			DedaoID:       42,
			EnID:          "enid-42",
			Title:         "42_测试书_作者",
			Author:        "作者",
			SourceHTML:    "/tmp/book.html",
			Status:        "draft",
			Extractor:     "dedao-gui-fallback",
			SourceType:    "wcplus_wechat_article",
			SourceKey:     "article-42",
			SourceAccount: "测试账号",
			PublishedAt:   "2026-07-09T12:00:00Z",
			ContentHash:   "hash-42",
		},
		Chapters: []BookKnowledgeChapter{
			{
				ChapterID: "42-chapter-1",
				BookID:    "42",
				Order:     1,
				Title:     "第一章",
				Summary:   "第一章摘要",
				ChunkIDs:  []string{"42-chunk-1"},
			},
		},
		Chunks: []BookKnowledgeChunk{
			{
				ChunkID:   "42-chunk-1",
				BookID:    "42",
				ChapterID: "42-chapter-1",
				Order:     1,
				Text:      "这是一段用于测试的内容。",
				Tokens:    12,
			},
		},
		Claims: []BookKnowledgeClaim{
			{
				ClaimID:       "42-claim-1",
				BookID:        "42",
				ChapterID:     "42-chapter-1",
				Title:         "第一章",
				Summary:       "这是一条草稿 claim。",
				Body:          "这是一条草稿 claim。",
				EvidenceLevel: "D",
				Confidence:    0.4,
				ReviewStatus:  "draft",
				Citations:     []string{"42-citation-1"},
			},
		},
		Citations: []BookKnowledgeCitation{
			{
				CitationID:    "42-citation-1",
				BookID:        "42",
				ChapterID:     "42-chapter-1",
				ChunkID:       "42-chunk-1",
				SourceHTML:    "/tmp/book.html",
				Anchor:        "第一章",
				Note:          "自动提取",
				SourceType:    "wcplus_wechat_article",
				SourceAccount: "测试账号",
				SourceItemKey: "article-42",
				PublishedAt:   "2026-07-09T12:00:00Z",
			},
		},
	}

	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	got, err := store.LoadPackage("42")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if !reflect.DeepEqual(got.Book, pkg.Book) {
		t.Fatalf("book = %#v, want %#v", got.Book, pkg.Book)
	}
	if !reflect.DeepEqual(got.Chapters, pkg.Chapters) {
		t.Fatalf("chapters = %#v, want %#v", got.Chapters, pkg.Chapters)
	}
	if !reflect.DeepEqual(got.Chunks, pkg.Chunks) {
		t.Fatalf("chunks = %#v, want %#v", got.Chunks, pkg.Chunks)
	}
	if !reflect.DeepEqual(got.Claims, pkg.Claims) {
		t.Fatalf("claims = %#v, want %#v", got.Claims, pkg.Claims)
	}
	if !reflect.DeepEqual(got.Citations, pkg.Citations) {
		t.Fatalf("citations = %#v, want %#v", got.Citations, pkg.Citations)
	}

	books, err := store.ListBooks()
	if err != nil {
		t.Fatalf("ListBooks returned error: %v", err)
	}
	if len(books) != 1 || books[0].BookID != "42" {
		t.Fatalf("books = %#v, want one saved book", books)
	}
}

func TestBookKnowledgeWritesPreserveExistingFilesOnEncodeFailure(t *testing.T) {
	root := t.TempDir()
	jsonPath := filepath.Join(root, "manifest.json")
	if err := writeJSONFile(jsonPath, map[string]any{"version": "stable"}); err != nil {
		t.Fatalf("write initial JSON: %v", err)
	}
	jsonBefore, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read initial JSON: %v", err)
	}
	if err := writeJSONFile(jsonPath, map[string]any{"invalid": math.Inf(1)}); err == nil {
		t.Fatal("invalid JSON write unexpectedly succeeded")
	}
	jsonAfter, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read JSON after failure: %v", err)
	}
	if !bytes.Equal(jsonAfter, jsonBefore) {
		t.Fatalf("failed JSON write replaced existing data: before=%q after=%q", jsonBefore, jsonAfter)
	}

	jsonlPath := filepath.Join(root, "claims.jsonl")
	if err := writeJSONLFile(jsonlPath, []any{"stable"}); err != nil {
		t.Fatalf("write initial JSONL: %v", err)
	}
	jsonlBefore, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read initial JSONL: %v", err)
	}
	if err := writeJSONLFile(jsonlPath, []any{"new", math.Inf(1)}); err == nil {
		t.Fatal("invalid JSONL write unexpectedly succeeded")
	}
	jsonlAfter, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read JSONL after failure: %v", err)
	}
	if !bytes.Equal(jsonlAfter, jsonlBefore) {
		t.Fatalf("failed JSONL write replaced existing data: before=%q after=%q", jsonlBefore, jsonlAfter)
	}
}

func TestBookKnowledgeListOrdersPublishedArticlesNewestFirst(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	for _, book := range []BookKnowledgeBook{
		{BookID: "newer", Title: "Newer", SourceType: "wechat_mp_article", PublishedAt: "2026-07-20T08:00:00Z", UpdatedAt: "2026-07-20T09:00:00Z"},
		{BookID: "older-imported-later", Title: "Older", SourceType: "wechat_mp_article", PublishedAt: "2026-07-01T08:00:00Z", UpdatedAt: "2026-07-21T09:00:00Z"},
	} {
		if err := store.SavePackage(BookKnowledgePackage{Book: book}); err != nil {
			t.Fatal(err)
		}
	}
	books, err := store.ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 || books[0].BookID != "newer" || books[1].BookID != "older-imported-later" {
		t.Fatalf("books=%#v", books)
	}
}

func TestBookKnowledgeStoreSerializesConcurrentManifestUpdates(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	const count = 40
	start := make(chan struct{})
	errorsByBook := make(chan error, count)
	var wait sync.WaitGroup
	for index := 0; index < count; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			bookID := fmt.Sprintf("concurrent-%02d", index)
			errorsByBook <- store.SavePackage(BookKnowledgePackage{
				Book: BookKnowledgeBook{BookID: bookID, Title: bookID, Status: "ready"},
			})
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByBook)
	for err := range errorsByBook {
		if err != nil {
			t.Fatalf("concurrent SavePackage: %v", err)
		}
	}
	books, err := store.ListBooks()
	if err != nil {
		t.Fatalf("ListBooks after concurrent saves: %v", err)
	}
	if len(books) != count {
		t.Fatalf("manifest contains %d books, want %d", len(books), count)
	}
}

func TestBookKnowledgeSearch(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:    "42",
			Title:     "42_量化分析_作者",
			Status:    "draft",
			Extractor: "dedao-gui-fallback",
		},
		Chapters: []BookKnowledgeChapter{
			{ChapterID: "42-chapter-1", BookID: "42", Order: 1, Title: "趋势"},
		},
		Chunks: []BookKnowledgeChunk{
			{ChunkID: "42-chunk-1", BookID: "42", ChapterID: "42-chapter-1", Order: 1, Text: "MACD 背离需要先定义趋势过滤。"},
			{ChunkID: "42-chunk-2", BookID: "42", ChapterID: "42-chapter-1", Order: 2, Text: "仓位管理不能依赖单一信号。"},
		},
		Claims: []BookKnowledgeClaim{
			{ClaimID: "42-claim-1", BookID: "42", ChapterID: "42-chapter-1", Title: "趋势过滤", Summary: "MACD 规则需要趋势过滤。", ReviewStatus: "draft"},
		},
	}
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	results, err := store.Search(BookKnowledgeSearchQuery{Query: "MACD 趋势", Limit: 10})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %#v, want chunk and claim matches", results)
	}
	if results[0].BookID != "42" || results[0].Kind == "" || results[0].Snippet == "" {
		t.Fatalf("first result missing fields: %#v", results[0])
	}
}

func assertPath(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
