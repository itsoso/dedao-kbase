# Book Job Worker Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move ebook download and knowledge-ingest jobs from `kbase-server` goroutines into a durable, observable, independently deployed Worker with safe manual retry.

**Architecture:** Keep KBase as the authenticated control plane and use a SQLite queue under the existing book-knowledge root as the single job source of truth. A new `book-job-worker` claims jobs transactionally, maintains a lease, reports through the existing Source Agent protocol, and classifies terminal failures without exposing private paths or credentials.

**Tech Stack:** Go 1.23, `database/sql`, `github.com/mattn/go-sqlite3`, existing KBase HTTP and Source Agent protocols, vanilla frontend Web JavaScript/CSS, systemd, shell deployment gates.

---

### Task 1: Replace JSON Job Persistence with an Idempotent SQLite Store

**Files:**
- Modify: `backend/app/book_jobs.go`
- Modify: `backend/app/book_jobs_test.go`

**Step 1: Write the failing migration tests**

Create a legacy `jobs.json`, open the new store twice, and verify every job is imported once. Include an old `failed: interrupted` record and require `interrupted` plus `worker_interrupted`.

```go
func TestBookKnowledgeJobsMigrateLegacyJSONOnce(t *testing.T) {
    store := NewBookKnowledgeStore(t.TempDir())
    writeLegacyBookJobs(t, store.LegacyJobsPath(), []BookKnowledgeJob{{
        ID: "job-old", Type: BookKnowledgeJobTypeDedaoEbookDownload,
        Status: BookKnowledgeJobStatusFailed, EbookID: 42, EbookEnID: "owned",
        Error: "job execution failed", Logs: []string{"queued", "running", "failed: interrupted"},
        CreatedAt: "2026-08-09T00:00:00Z", UpdatedAt: "2026-08-09T00:01:00Z",
    }})
    first := mustListBookJobs(t, store)
    second := mustListBookJobs(t, store)
    if len(first) != 1 || len(second) != 1 || first[0].Status != BookKnowledgeJobStatusInterrupted {
        t.Fatalf("first=%#v second=%#v", first, second)
    }
    if first[0].FailureCode != BookKnowledgeJobFailureWorkerInterrupted {
        t.Fatalf("job=%#v", first[0])
    }
}
```

Also verify a fresh database enables foreign keys and WAL without modifying or deleting the legacy file.

**Step 2: Run RED**

```bash
go test ./backend/app -run 'TestBookKnowledgeJobs(MigrateLegacyJSONOnce|SQLiteSchema)' -count=1
```

Expected: FAIL because `LegacyJobsPath`, `interrupted`, and SQLite migration do not exist.

**Step 3: Implement the minimal schema and migration**

Keep `BookKnowledgeStore` as the root owner to minimize call-site churn. Add `BookJobsDBPath()` and `LegacyJobsPath()`. Open SQLite with busy timeout, foreign keys, and WAL.

```sql
CREATE TABLE IF NOT EXISTS book_jobs (
  job_id TEXT PRIMARY KEY, job_type TEXT NOT NULL, status TEXT NOT NULL,
  ebook_id INTEGER NOT NULL, ebook_enid TEXT NOT NULL, download_type INTEGER NOT NULL,
  result_json TEXT NOT NULL DEFAULT '{}', retry_of TEXT NOT NULL DEFAULT '',
  stage TEXT NOT NULL DEFAULT 'queued', worker_id TEXT NOT NULL DEFAULT '',
  lease_expires_at TEXT NOT NULL DEFAULT '', failure_code TEXT NOT NULL DEFAULT '',
  failure_message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL, started_at TEXT NOT NULL DEFAULT '',
  finished_at TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(retry_of) REFERENCES book_jobs(job_id)
);
CREATE TABLE IF NOT EXISTS book_job_events (
  event_id INTEGER PRIMARY KEY AUTOINCREMENT, job_id TEXT NOT NULL,
  status TEXT NOT NULL, stage TEXT NOT NULL, code TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
  FOREIGN KEY(job_id) REFERENCES book_jobs(job_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS book_job_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
```

Import within one transaction, store `legacy_jobs_imported_v1`, and use `ON CONFLICT DO NOTHING`. Do not delete or rename `jobs.json`.

**Step 4: Run GREEN**

```bash
go test ./backend/app -run 'Test(BookKnowledgeJob|BookKnowledgeJobs|DefaultDedaoDownloadRoot)' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/book_jobs.go backend/app/book_jobs_test.go
git commit -m "feat(kbase): persist book jobs in sqlite"
```

### Task 2: Add Claims, Leases, Structured Failures, Retry, and Legacy Export

**Files:**
- Modify: `backend/app/book_jobs.go`
- Modify: `backend/app/book_jobs_test.go`

**Step 1: Write failing state-machine tests**

Cover two independent connections claiming one job, lease ownership/renewal, lease expiry to `interrupted`, queued recovery, new-job retry with `retry_of`, duplicate active retry conflict, and legacy export without lease/private fields.

**Step 2: Run RED**

```bash
go test ./backend/app -run 'TestBookKnowledgeJob(ConcurrentClaim|LeaseOwner|LeaseExpiry|Retry|ExportLegacy)' -count=1
```

Expected: FAIL because the APIs do not exist.

**Step 3: Implement the state machine**

Add:

```go
func (s *BookKnowledgeStore) ClaimNextBookKnowledgeJob(workerID string, lease time.Duration) (*BookKnowledgeJob, error)
func (s *BookKnowledgeStore) RenewBookKnowledgeJobLease(jobID, workerID string, lease time.Duration) (BookKnowledgeJob, error)
func (s *BookKnowledgeStore) UpdateBookKnowledgeJobStage(jobID, workerID, stage string) (BookKnowledgeJob, error)
func (s *BookKnowledgeStore) CompleteBookKnowledgeJob(jobID, workerID string, result map[string]any) (BookKnowledgeJob, error)
func (s *BookKnowledgeStore) FailBookKnowledgeJob(jobID, workerID, code string) (BookKnowledgeJob, error)
func (s *BookKnowledgeStore) InterruptBookKnowledgeJob(jobID, workerID string) (BookKnowledgeJob, error)
func (s *BookKnowledgeStore) ReconcileExpiredBookKnowledgeJobs() (int, error)
func (s *BookKnowledgeStore) RetryBookKnowledgeJob(jobID string) (BookKnowledgeJob, error)
func (s *BookKnowledgeStore) ExportLegacyBookKnowledgeJobs(path string) error
```

Claim the oldest queued ID and update with `WHERE status='queued'`; require one affected row. Restrict stored failures to predefined codes and derive Chinese messages server-side.

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_book_jobs_one_active_retry
ON book_jobs(retry_of)
WHERE retry_of <> '' AND status IN ('queued', 'running');
```

Export via temporary file and atomic rename. Map `interrupted` to legacy `failed` with a safe interruption message.

**Step 4: Run GREEN repeatedly**

```bash
go test ./backend/app -run 'TestBookKnowledgeJob(ConcurrentClaim|LeaseOwner|LeaseExpiry|Retry|ExportLegacy)' -count=20
```

Expected: PASS on every repetition.

**Step 5: Commit**

```bash
git add backend/app/book_jobs.go backend/app/book_jobs_test.go
git commit -m "feat(kbase): add durable book job leases and retry"
```

### Task 3: Turn KBase into a Control Plane Only

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/dedao_ebook_jobs_http_test.go`
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`

**Step 1: Write failing HTTP/startup tests**

Prove `POST /api/jobs` persists `queued` without executing. Require `POST /api/jobs/<id>/retry` to authenticate, accept only failed/interrupted work, revalidate authoritative identity and ownership, return a linked new job, reject an active retry with `409`, and omit private details. KBase startup must initialize migration without failing or executing queued/running jobs.

**Step 2: Run RED**

```bash
go test ./backend/app -run 'TestKBaseHTTP.*BookJob.*(QueuedOnly|Retry)' -count=1
go test ./cmd/kbase-server -run 'Test.*BookKnowledgeJobs.*ControlPlane' -count=1
```

Expected: FAIL because creation starts a goroutine and retry is absent.

**Step 3: Implement exact retry routing and remove execution**

```go
if jobID, ok := parseBookKnowledgeJobAction(r.URL.Path, "retry"); ok {
    h.handleRetryBookKnowledgeJob(w, r, jobID)
    return
}
```

Remove `go h.store.RunBookKnowledgeJobWithService(...)`. The retry handler loads the original, fetches ebook detail, verifies ID/EnID/ownership, then calls `RetryBookKnowledgeJob`. Remove KBase startup's interrupted-job failure conversion; initialize SQLite early and fail startup on migration errors.

**Step 4: Run GREEN**

```bash
go test ./backend/app -run 'TestKBaseHTTP.*(BookJob|DedaoEbookJob)' -count=1
go test ./cmd/kbase-server -run 'Test.*BookKnowledgeJobs' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/dedao_ebook_jobs_http_test.go cmd/kbase-server/main.go cmd/kbase-server/main_test.go
git commit -m "feat(kbase): expose safe book job retry control"
```

### Task 4: Build the Independent Worker

**Files:**
- Create: `backend/app/book_job_worker.go`
- Create: `backend/app/book_job_worker_test.go`
- Create: `cmd/book-job-worker/main.go`
- Create: `cmd/book-job-worker/main_test.go`
- Modify: `backend/app/book_jobs.go`

**Step 1: Write failing Worker tests**

Use real SQLite and injected executors for oldest claim, stage transitions, structured failure classification, periodic renewal, graceful interruption, expired-job reconciliation, and CLI commands `build-info`, `check-config`, `once`, `run`, and `export-legacy`.

**Step 2: Run RED**

```bash
go test ./backend/app -run 'TestBookJobWorker' -count=1
go test ./cmd/book-job-worker -count=1
```

Expected: FAIL because the Worker does not exist.

**Step 3: Implement the minimal runtime**

```go
type BookJobWorkerConfig struct {
    Store *BookKnowledgeStore
    WorkerID string
    LeaseDuration time.Duration
    RenewInterval time.Duration
    PollInterval time.Duration
    Execute func(context.Context, BookKnowledgeJob, func(string) error) (map[string]any, error)
}

func (w *BookJobWorker) RunOnce(ctx context.Context) (bool, error)
func (w *BookJobWorker) Run(ctx context.Context) error
```

`RunOnce` reconciles expired leases, claims once, renews in a bounded child loop, calls existing download/sync functions, and commits exactly one terminal state. Context cancellation maps to `worker_interrupted`; typed stage wrappers map other failures without persisting raw errors.

CLI:

```text
book-job-worker build-info
book-job-worker check-config
book-job-worker once
book-job-worker run
book-job-worker export-legacy --out <path>
```

Use existing environment roots; never print tokens or add machine-specific defaults.

**Step 4: Run GREEN**

```bash
go test ./backend/app -run 'TestBookJobWorker' -count=1
go test ./cmd/book-job-worker -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/book_job_worker.go backend/app/book_job_worker_test.go backend/app/book_jobs.go cmd/book-job-worker/main.go cmd/book-job-worker/main_test.go
git commit -m "feat(kbase): add independent book job worker"
```

### Task 5: Register the Worker and Add Restricted Restart

**Files:**
- Modify: `backend/app/source_agent_command.go`
- Modify: `backend/app/source_agent_command_test.go`
- Modify: `backend/app/source_agent_client.go`
- Modify: `backend/app/source_agent_client_test.go`
- Modify: `backend/app/book_job_worker.go`
- Modify: `backend/app/book_job_worker_test.go`
- Modify: `cmd/book-job-worker/main.go`
- Modify: `cmd/book-job-worker/main_test.go`

**Step 1: Write failing protocol tests**

Add `restart` command tests for only `queued -> claimed -> succeeded/failed/expired`. Reject payload, upgrade specs, arbitrary commands, and agents without `controlled_restart`. Build a production-schema fixture whose CHECK allows only diagnose/upgrade; reopening must preserve rows and then accept restart.

Require Worker heartbeat:

```go
app.SourceAgentHeartbeat{
    WorkerType: "book-job-worker", Version: buildVersion,
    ProtocolVersion: protocolVersion,
    Capabilities: []string{"book_jobs", "diagnose", "controlled_restart"},
    CurrentRunID: currentJobID,
}
```

**Step 2: Run RED**

```bash
go test ./backend/app -run 'TestSourceAgentCommand(Restart|MigrationAddsRestart)|TestBookJobWorker.*(Heartbeat|Restart)' -count=1
```

Expected: FAIL because restart is unsupported.

**Step 3: Extend the protocol without weakening existing commands**

```go
const SourceAgentCommandRestart = "restart"
const SourceAgentCommandCodeRestartComplete = "restart_complete"
```

Rebuild the command table transactionally when its CHECK lacks restart, copy all rows, and recreate indexes/foreign keys. The Worker claims via existing `SourceAgentClient`; restart interrupts its claimed book job, reports success, then exits zero for systemd. It never executes a path, shell, or payload.

**Step 4: Run GREEN**

```bash
go test ./backend/app -run 'TestSourceAgent(Command|Client)|TestBookJobWorker' -count=1
go test ./cmd/book-job-worker -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/source_agent_command.go backend/app/source_agent_command_test.go backend/app/source_agent_client.go backend/app/source_agent_client_test.go backend/app/book_job_worker.go backend/app/book_job_worker_test.go cmd/book-job-worker/main.go cmd/book-job-worker/main_test.go
git commit -m "feat(agent): add restricted book worker restart"
```

### Task 6: Add Actionable Job and Worker UI

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs`
- Modify: `frontend-web/scripts/source-agent-management-smoke.mjs`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write failing smoke assertions**

Require interrupted/stage labels, retry only for KBase failed/interrupted jobs, retry API/reload, retry history, a “书籍任务 Worker” card, current job/heartbeat/safe error, diagnosis/restart controls, and no raw `job execution failed` product copy.

**Step 2: Run RED**

```bash
node frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs
node frontend-web/scripts/source-agent-management-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: FAIL on missing behavior.

**Step 3: Implement minimal UI behavior**

```js
function bookJobFailureMessage(job) {
  return String(job?.failure_message || job?.error || "").trim();
}

async function retryBookJob(jobID) {
  await apiFetch(`/api/jobs/${encodeURIComponent(jobID)}/retry`, {
    method: "POST", body: "{}",
  });
  await loadJobCenter();
}
```

Show stage and retry history in ebook cards and job center. Disable while submitting and map `409` to “已有重试任务正在排队或运行”. Render restart only for `worker_type=book-job-worker` with `controlled_restart`.

**Step 4: Run GREEN**

```bash
node --check frontend-web/app.js
node frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs
node frontend-web/scripts/source-agent-management-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs frontend-web/scripts/source-agent-management-smoke.mjs frontend-web/scripts/book-knowledge-web-smoke.mjs
git commit -m "feat(web): add book job recovery controls"
```

### Task 7: Add Service, Build, Migration, and Rollback Contracts

**Files:**
- Create: `deploy/systemd/dedao-book-job-worker.service`
- Modify: `README.md`
- Modify: `scripts/kbase-direct-deployment-smoke.sh`
- Modify: `.github/workflows/kbase-build-gates.yml`
- Modify: `docs/_generated/system-map.json`
- Modify: `docs/dossiers/2026-08-09-book-job-worker.md`

**Step 1: Write failing deployment checks**

Require build/hash/backup/install/start/verify/rollback for both binaries, Worker `build-info` and `check-config`, SQLite backup, legacy export before old-binary rollback, and both systemd health checks.

**Step 2: Run RED**

```bash
bash scripts/kbase-direct-deployment-smoke.sh
```

Expected: FAIL because Worker deployment markers are absent.

**Step 3: Add systemd and direct-release contracts**

```ini
[Unit]
Description=KBase book job worker
After=network-online.target dedao-kbase.service
Requires=dedao-kbase.service

[Service]
Type=simple
User=dedao-kbase
Group=dedao-kbase
EnvironmentFile=/etc/dedao-kbase/kbase.env
ExecStart=/opt/dedao-kbase/bin/book-job-worker run
Restart=always
RestartSec=3
KillSignal=SIGTERM
TimeoutStopSec=45

[Install]
WantedBy=multi-user.target
```

Build both commands from one revision and verify separate SHA-256 values. Rollback must stop Worker, export SQLite atomically to legacy JSON, restore both binaries/Web, restart KBase, and leave SQLite/downloads intact.

Regenerate structural metadata:

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
```

**Step 4: Run GREEN**

```bash
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/system-map-smoke.sh
```

Expected: PASS.

**Step 5: Commit**

```bash
git add deploy/systemd/dedao-book-job-worker.service README.md scripts/kbase-direct-deployment-smoke.sh .github/workflows/kbase-build-gates.yml docs/_generated/system-map.json docs/dossiers/2026-08-09-book-job-worker.md
git commit -m "docs(deploy): add book worker rollout contract"
```

### Task 8: Complete Gates, Review, Merge, Deploy, and Verify

**Files:**
- Modify: `docs/dossiers/2026-08-09-book-job-worker.md`

**Step 1: Run G3 gates**

```bash
go test ./backend/app -run 'Test(BookKnowledgeJob|BookJobWorker|KBaseHTTP.*BookJob|SourceAgentCommandRestart)' -count=1
go test ./cmd/book-job-worker ./cmd/kbase-server -count=1
go test ./... -timeout=300s -count=1
go vet ./...
node --check frontend-web/app.js
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
(cd frontend && npm run build)
bash scripts/privacy-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/kbase-direct-deployment-smoke.sh
git diff --check
```

Expected: all exit zero; never pipe tests through plain `tail`.

**Step 2: Perform G4 review**

Review double execution, stale leases/cancellation, retry authorization, duplicate retry, restart command scope/schema migration, legacy import/export preservation, privacy leakage, and rollback ordering. Any BLOCK returns to implementation and reruns G3/G4.

**Step 3: Record and commit gate evidence**

```bash
git add docs/dossiers/2026-08-09-book-job-worker.md
git commit -m "docs(kbase): record book worker release gates"
```

**Step 4: Merge and deploy from clean main**

After review, merge only a clean branch, push deployment `main`, archive the exact revision, verify SHA-256 remotely, and rebuild/test as the service user. One backup batch must include KBase binary, Worker binary, Web, SQLite database backup, legacy JSON, and changed systemd unit. Install rollback traps before replacement. Do not add signing or manifest mechanisms.

**Step 5: Run G5/G6 production verification**

Verify exact build identities, KBase health/routes/401 boundaries, fresh Worker heartbeat, exactly-once claim, KBase restart isolation, controlled Worker interruption, linked retry, legacy export compatibility, stable service restart counts, and clean logs. Any G5 failure triggers immediate rollback. G6 requires the user to confirm the real browser path.

**Step 6: Mark shipped after user confirmation**

Record revision, backup/rollback location, live evidence, and lessons learned; set the dossier state to `shipped`.
