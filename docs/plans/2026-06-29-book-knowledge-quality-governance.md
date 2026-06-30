# Book Knowledge Quality Governance Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Generate and expose deterministic book-level quality reports for ingested knowledge packages.

**Architecture:** `BookKnowledgeStore.SavePackage` computes a `BookKnowledgeQualityReport`, saves it beside the book package, and copies summary fields into the book manifest. HTTP APIs serialize the report for detail views. The Web KBase workbench displays status and report details without adding a new page.

**Tech Stack:** Go backend, JSON files under `book_knowledge`, Vue 3 frontend, existing kbase HTTP API.

---

### Task 1: Backend Quality Report Model

**Files:**
- Create: `backend/app/book_quality.go`
- Modify: `backend/app/book_knowledge.go`
- Test: `backend/app/book_quality_test.go`

**Step 1: Write failing tests**

Add tests that save a healthy package and expect:
- `quality_report.json` exists
- `LoadBookQualityReport("42")` returns `status="usable"` or `needs_review` based on deterministic rules
- `ListBooks()` includes `quality_status`, `quality_score`, and `quality_updated_at`

Add a second test for an empty package expecting `status="rejected"` and a `missing_chunks` issue.

**Step 2: Run failing tests**

Run:

```bash
go test ./backend/app -run 'TestBookKnowledgeQuality'
```

Expected: FAIL because quality types and helpers do not exist.

**Step 3: Implement minimal backend model**

Create:
- `BookKnowledgeQualityReport`
- `BookKnowledgeQualityMetrics`
- `BookKnowledgeQualityIssue`
- `BuildBookKnowledgeQualityReport(pkg)`
- `QualityReportPath(bookID)`
- `LoadBookQualityReport(bookID)`

Update `BookKnowledgeBook` with summary fields and call the report builder from `SavePackage`.

**Step 4: Run backend tests**

Run:

```bash
go test ./backend/app -run 'TestBookKnowledgeQuality|TestBookKnowledgePackageRoundTrip'
```

Expected: PASS.

### Task 2: HTTP API Serialization

**Files:**
- Modify: `backend/app/kbase_http.go`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write failing tests**

Assert `GET /api/books` includes `quality_status` and `quality_score`, and `GET /api/books/{book_id}` includes `quality_report`.

**Step 2: Implement response shape**

Add `QualityReport *BookKnowledgeQualityReport` to `BookKnowledgePackage` JSON responses by extending the package struct or wrapping the HTTP detail response. Keep list response on `BookKnowledgeBook` summary fields only.

**Step 3: Run tests**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler.*Book'
```

Expected: PASS.

### Task 3: Project Verification Uses Book Quality

**Files:**
- Modify: `backend/app/book_project.go`
- Modify: `backend/app/book_project_verification.go`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write failing test**

Save one rejected package and one usable package, then assert project verification does not return claims from the rejected book.

**Step 2: Implement gating**

When building review items, skip `quality_status="rejected"` packages. For `needs_review`, keep items but add a book-quality warning check so downstream sees the assistive-only signal.

**Step 3: Run tests**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerServesProject'
```

Expected: PASS.

### Task 4: Web KBase Quality UI

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/KBaseWorkbench.vue`
- Test: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Add TypeScript types**

Extend `BookKnowledgeBook` and `BookKnowledgePackage` with quality summary/report fields.

**Step 2: Render UI**

Show quality status in the book rail. In Overview, show score, status, key metrics, issues, allowed uses, and blocked uses.

**Step 3: Add smoke checks**

Assert source includes `quality_status`, `quality_report`, and a quality Overview rendering marker.

**Step 4: Run frontend checks**

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
cd frontend-web && npm run build
```

Expected: PASS.

### Task 5: Final Verification And Deploy

**Files:**
- Modify: `README.md`

**Step 1: Document quality governance**

Add a short README note under kbase Web/API explaining `quality_report.json` and governance statuses.

**Step 2: Run release checks**

```bash
go test ./...
node frontend-web/scripts/web-kbase-ui-smoke.mjs
cd frontend-web && npm run build
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server
git diff --check
```

Expected: PASS. If `scripts/privacy-smoke.sh` exists, run it; otherwise record that it is absent.

**Step 3: Deploy**

Use the existing path: install `/tmp/dedao-kbase-web/kbase-server-linux-amd64` to `/opt/dedao-kbase/bin/kbase-server`, sync `frontend-web/dist/` to `/var/www/kbase.executor.life/`, restart `dedao-kbase.service`, and verify `/health`.
