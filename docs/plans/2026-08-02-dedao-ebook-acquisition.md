# Dedao Ebook Acquisition Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Restore the canonical Web flow for Dedao QR login, site-wide ebook search, server-local download, and automatic ingestion into the existing book knowledge store.

**Architecture:** Port the tested authentication, safe ebook DTO mapping, and persisted job patterns from the historical Web implementation into the current frontend-web/app.js application. Keep credentials server-side, protect every API with the existing KBase Bearer token, run download/ingest work asynchronously, and write knowledge packages through the handler's injected BookKnowledgeStore.

**Tech Stack:** Go HTTP handlers and services, JSON-backed background jobs, existing Dedao downloader and knowledge extractor, static HTML/CSS/JavaScript frontend, Node smoke scripts.

---

### Task 1: Restore Safe Dedao Session and QR Login APIs

**Files:**
- Create: backend/app/dedao_session.go
- Create: backend/app/dedao_auth.go
- Create: backend/app/dedao_auth_test.go
- Modify: backend/app/kbase_http.go
- Modify: backend/app/kbase_http_test.go

**Step 1: Write the failing tests**

Add TestKBaseHTTPHandlerServesDedaoSessionAndQRCode. It must prove that GET /api/dedao/session, POST /api/dedao/auth/qrcode, and POST /api/dedao/auth/check are Bearer-protected, validate input, set no-store headers, and never return Cookie, CookieStr, access_token, or config paths.

Add TestLiveDedaoAuthProviderUsesDedicatedLoginService with a fake service so QR creation cannot reuse a stale active-user client.

**Step 2: Run RED**

~~~bash
go test ./backend/app -run 'TestKBaseHTTPHandlerServesDedaoSessionAndQRCode|TestLiveDedaoAuthProviderUsesDedicatedLoginService' -count=1
~~~

Expected: FAIL because the provider, DTOs, and routes do not exist.

**Step 3: Implement the minimal contract**

Create safe DedaoSession, DedaoSessionUser, DedaoLoginQRCode, DedaoLoginCheckRequest, and DedaoLoginCheck DTOs. Add DedaoAuthProvider to KBaseHTTPConfig and default it to a live provider backed by a fresh login service.

Add these routes before the current read-only Dedao routes:

~~~text
GET  /api/dedao/session
POST /api/dedao/auth/qrcode
POST /api/dedao/auth/check
~~~

QR and check responses must set Cache-Control: no-store and Pragma: no-cache. The browser response contains only short-lived QR fields and a safe user/session summary.

**Step 4: Run GREEN**

Run the narrow command again and confirm PASS.

**Step 5: Commit**

~~~bash
git add backend/app/dedao_session.go backend/app/dedao_auth.go backend/app/dedao_auth_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(dedao): restore web qr login api"
~~~

### Task 2: Restore Site-Wide Ebook Search and Bookshelf Actions

**Files:**
- Create: backend/app/dedao_ebook_acquisition.go
- Create: backend/app/dedao_ebook_acquisition_test.go
- Modify: backend/services/requester.go
- Modify: backend/services/sunflower.go
- Modify: backend/app/kbase_http.go
- Modify: backend/app/kbase_http_test.go

**Step 1: Write the failing tests**

Add TestDedaoSiteEbookSearchMapping. The safe result contains only id, enid, title, author, intro, icon, price, progress, is_buy, and can_trial_read.

Add TestKBaseHTTPHandlerServesDedaoSiteEbookSearch and TestKBaseHTTPHandlerAddsDedaoEbookToBookshelf. Verify auth, q/page/page_size forwarding, encoded enid handling, stable JSON, and secret omission.

Add a backend/services httptest case that asserts the Dedao search request path and pagination body.

**Step 2: Run RED**

~~~bash
go test ./backend/services ./backend/app -run 'Test.*(SearchEbooks|SiteEbookSearch|AddsDedaoEbookToBookshelf)' -count=1
~~~

Expected: FAIL because the request helper, provider, and routes are missing.

**Step 3: Implement the safe provider**

Add a narrow DedaoEbookAcquisitionService instead of expanding the existing read-only library interface:

~~~go
type DedaoEbookAcquisitionService interface {
    SearchEbooks(query string, page, pageSize int) (DedaoEbookPage, error)
    AddEbookToBookshelf(enid string) (DedaoEbook, error)
    EbookDetail(enid string) (*services.EbookDetail, error)
}
~~~

Implement the live adapter with existing service methods and add:

~~~text
GET  /api/dedao/search/ebooks
POST /api/dedao/ebooks/<enid>/bookshelf
~~~

A blank full-site query returns an empty page. Upstream failures return 502 and never fall back to shelf data.

**Step 4: Run GREEN**

Run the narrow command again and confirm PASS.

**Step 5: Commit**

~~~bash
git add backend/app/dedao_ebook_acquisition.go backend/app/dedao_ebook_acquisition_test.go backend/services/requester.go backend/services/sunflower.go backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(dedao): restore site ebook search"
~~~

### Task 3: Restore Persisted Download and Knowledge Ingest Jobs

**Files:**
- Create: backend/app/book_jobs.go
- Create: backend/app/book_jobs_test.go
- Modify: backend/app/ebook_wiki.go
- Modify: backend/app/ebook_wiki_test.go
- Modify: backend/app/kbase_http.go
- Modify: backend/app/kbase_http_test.go
- Modify: cmd/kbase-server/main.go
- Modify: cmd/kbase-server/main_test.go

**Step 1: Write failing job-store tests**

Cover the supported types dedao_ebook_download and dedao_ebook_sync_kbase. Verify invalid type, missing ebook_id, missing ebook_enid, and invalid format rejection; queued persistence; queued to running to succeeded transitions; failed executor state; interrupted queued/running recovery; and explicit/fallback download-root selection.

**Step 2: Run RED**

~~~bash
go test ./backend/app -run 'Test(BookKnowledgeJob|DefaultDedaoDownloadRoot|SyncEbookToBookKnowledgeStore)' -count=1
~~~

Expected: FAIL because the job store and injected sync helper do not exist.

**Step 3: Implement minimal persistence and execution**

Persist jobs.json under the injected BookKnowledgeStore root using a mutex and atomic replacement. Store only IDs, type, status, timestamps, sanitized errors, and safe result identifiers.

Refactor the ebook pipeline to expose:

~~~go
func SyncEbookToBookKnowledgeStore(
    ctx context.Context,
    id int,
    enid string,
    store *BookKnowledgeStore,
    downloadRoot string,
) (*EbookWikiSyncResult, error)
~~~

This function downloads HTML and runs BuildBookKnowledgeFromHTMLFile. It must not require the external Wiki compiler.

**Step 4: Write failing HTTP authorization and ownership tests**

Test GET /api/jobs, POST /api/jobs, and GET /api/jobs/<job_id>. Before queuing a Dedao job, the handler must call EbookDetail and accept only is_buy or is_on_bookshelf. Unauthorized and unowned requests must not create records.

**Step 5: Implement HTTP jobs and recovery**

Create the job, start RunBookKnowledgeJob asynchronously, and return 202. In cmd/kbase-server, mark interrupted queued/running jobs failed before starting HTTP.

API results must not return full output_dir, html_path, repo_dir, or book_knowledge_root values.

**Step 6: Run GREEN**

~~~bash
go test ./backend/app -run 'Test.*(BookKnowledgeJob|Jobs|DedaoEbookJob|DefaultDedaoDownloadRoot|SyncEbookToBookKnowledgeStore)' -count=1
go test ./cmd/kbase-server -run 'Test.*Interrupted.*Jobs' -count=1
~~~

Expected: PASS.

**Step 7: Commit**

~~~bash
git add backend/app/book_jobs.go backend/app/book_jobs_test.go backend/app/ebook_wiki.go backend/app/ebook_wiki_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go cmd/kbase-server/main.go cmd/kbase-server/main_test.go
git commit -m "feat(dedao): restore ebook download jobs"
~~~

### Task 4: Add the Canonical Web Login Entry

**Files:**
- Create: frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs
- Modify: frontend-web/app.js
- Modify: frontend-web/styles.css
- Modify: frontend-web/index.html

**Step 1: Write failing smoke assertions**

Require /sources/dedao/login, /api/dedao/session, /api/dedao/auth/qrcode, /api/dedao/auth/check, 扫码登录得到, renderDedaoLogin, startDedaoLoginPolling, and stopDedaoLoginPolling. Require login-panel, QR-frame, status, responsive, and reduced-motion styles.

**Step 2: Run RED**

~~~bash
node frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs
~~~

Expected: FAIL on the first missing marker.

**Step 3: Implement login route and state**

Add ROUTES.dedaoLogin, a top-nav account entry, a home CTA, and a dedicated login page matching the current paper/orange visual language.

The page loads a safe session summary, creates a QR on user action, polls every two seconds, stops on success/expiry/navigation/error, and refreshes home/library state after success. Do not persist Dedao login token or QR string in localStorage.

**Step 4: Run GREEN**

~~~bash
node frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/kbase-token-header-smoke.mjs
~~~

Expected: PASS.

**Step 5: Commit**

~~~bash
git add frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs frontend-web/app.js frontend-web/styles.css frontend-web/index.html
git commit -m "feat(web): restore dedao login entry"
~~~

### Task 5: Add Search, Download, Ingest, and Job Status UI

**Files:**
- Modify: frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs
- Modify: frontend-web/app.js
- Modify: frontend-web/styles.css
- Modify: frontend-web/index.html

**Step 1: Extend smoke assertions and verify RED**

Require /api/dedao/search/ebooks, /bookshelf, /api/jobs, dedao_ebook_download, dedao_ebook_sync_kbase, 我的书架, 全站搜索, 仅下载, and 下载并入知识库.

**Step 2: Implement ebook acquisition state and rendering**

Extend the current ebook page rather than adding another app. Add shelf/site source state, query, page, page size, normalized cards, retained results on failure, disabled reasons, bookshelf action, separate download action, one-click sync action, and duplicate-submit prevention.

The sync action posts:

~~~json
{
  "type": "dedao_ebook_sync_kbase",
  "ebook_id": 123,
  "ebook_enid": "stable-enid",
  "download_type": 1
}
~~~

Poll the job until a terminal status. On success, link knowledge_book_id through buildKnowledgePackageURL.

**Step 3: Extend the existing job center**

Fetch /api/jobs alongside WC Plus tasks. Normalize both into the existing job-card structure. A WC Plus failure must not hide successful KBase jobs, and a KBase failure must not hide WC Plus jobs.

**Step 4: Run GREEN**

~~~bash
node frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/ebook-reader-loading-smoke.mjs
node frontend-web/scripts/kbase-token-header-smoke.mjs
~~~

Expected: PASS.

**Step 5: Commit**

~~~bash
git add frontend-web/scripts/dedao-ebook-acquisition-smoke.mjs frontend-web/app.js frontend-web/styles.css frontend-web/index.html
git commit -m "feat(web): restore ebook acquisition workflow"
~~~

### Task 6: Regenerate Architecture Inventory and Document Configuration

**Files:**
- Modify: README.md
- Modify: docs/_generated/system-map.json

**Step 1: Document configuration**

Document DEDAO_DOWNLOAD_ROOT as an optional explicit server download location and the fallback relative to KBase roots. Include no real usernames, host paths, cookies, tokens, or downloaded titles.

**Step 2: Regenerate the code-owned system map**

~~~bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
~~~

Expected: PASS and generated route inventory includes auth, search, bookshelf, and jobs.

**Step 3: Run privacy checks**

~~~bash
bash scripts/privacy-smoke.sh
git diff --check
~~~

Expected: PASS.

**Step 4: Commit**

~~~bash
git add README.md docs/_generated/system-map.json
git commit -m "docs(dedao): document ebook acquisition storage"
~~~

### Task 7: Full Verification

**Step 1: Run all Web smoke scripts**

~~~bash
for test_file in frontend-web/scripts/*.mjs; do node "$test_file" || exit 1; done
~~~

Expected: every script exits 0.

**Step 2: Run all Go tests**

~~~bash
go test ./...
~~~

Expected: PASS with no failing packages.

**Step 3: Run the desktop frontend build**

~~~bash
cd frontend && npm run build
~~~

Expected: vue-tsc and Vite succeed. Do not commit frontend/dist.

**Step 4: Run architecture and privacy gates**

~~~bash
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
git status --short
~~~

Expected: all checks pass and status contains no binaries, downloaded books, config files, tokens, cookies, or macOS metadata.

**Step 5: Manual local-server verification**

Use only test fixtures. Verify the home login CTA, login terminal states, shelf/site switch, eligible job creation, terminal task status, and successful knowledge-package link. Do not commit account details, QR data, personal downloads, or screenshots containing private information.
