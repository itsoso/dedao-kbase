# Dedao Ebook Shelf Actions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add explicit and automatic add-to-shelf support so full-site ebook search results can be added, downloaded, and synced to the book knowledge base.

**Architecture:** Extend the existing KBase HTTP server with one private shelf-add route backed by the existing Dedao `EbookShelfAdd` service. The web UI will expose an explicit `加入书架` action and reuse the same API as an automatic prerequisite for download and knowledge-base jobs.

**Tech Stack:** Go KBase HTTP handlers and provider interfaces, Dedao service wrappers, Vue 3 frontend, TypeScript API client, Node smoke tests.

---

### Task 1: Backend Route Test

**Files:**
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write the failing test**

Add a fake provider method for shelf add and a test that `POST /api/dedao/ebooks/{enid}/bookshelf` requires Bearer auth, invokes the provider with the path `enid`, and returns a Dedao ebook with no private credential fields.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run TestKBaseHTTPHandlerAddsDedaoEbookToBookshelf -count=1`

Expected: FAIL because the route and provider method do not exist.

### Task 2: Backend Implementation

**Files:**
- Modify: `backend/app/dedao_content.go`
- Modify: `backend/app/kbase_http.go`

**Step 1: Extend the provider interface**

Add `AddEbookToBookshelf(ctx context.Context, enid string) (DedaoEbook, error)`.

**Step 2: Implement the live provider**

Trim and validate `enid`, call `getService().EbookShelfAdd([]string{enid})`, then `getService().EbookDetail(enid)`, and return `dedaoEbookFromDetail(detail)` with `IsBuy=true`.

**Step 3: Add HTTP handler**

Add `POST /api/dedao/ebooks/{enid}/bookshelf` before the generic ebook subroute. Return the normalized ebook as JSON.

**Step 4: Run backend tests**

Run: `go test ./backend/app -run 'TestKBaseHTTPHandler(AddsDedaoEbookToBookshelf|ServesDedaoSiteEbookSearch)' -count=1`

Expected: PASS.

### Task 3: Frontend API And Search Actions

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/EbookLibrary.vue`

**Step 1: Add API client**

Add `addDedaoEbookToBookshelf(enid: string): Promise<DedaoEbook>` calling `POST /api/dedao/ebooks/${encodeURIComponent(enid)}/bookshelf`.

**Step 2: Add explicit search-list action**

Render `加入书架` for full-site search rows and selected detail. Disable only while that ebook action is loading.

**Step 3: Add automatic prerequisite**

Before creating download or knowledge-base jobs, call `ensureEbookOnShelf` when the selected ebook is from full-site search and lacks `is_buy` or `id`. Merge the hydrated response back into `ebooks` and `selectedEbook`.

### Task 4: Reader Toolbar Action

**Files:**
- Modify: `frontend-web/src/views/EbookDetailReader.vue`

**Step 1: Enable toolbar action**

Wire the existing `加入书架` toolbar button to `addDedaoEbookToBookshelf`.

**Step 2: Reflect success**

After success, mark the current detail as on-shelf when the local type supports it and show the existing connected/status message.

### Task 5: Smoke Tests And Verification

**Files:**
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Add static smoke assertions**

Assert that the API client includes the bookshelf POST route, `EbookLibrary.vue` includes `ensureEbookOnShelf`, and `EbookDetailReader.vue` calls the add API.

**Step 2: Run verification**

Run:
- `go test ./backend/app -count=1`
- `go test ./cmd/kbase-server -count=1`
- `node frontend-web/scripts/web-kbase-ui-smoke.mjs`
- `npm --prefix frontend-web run build`
- `git diff --check`

Expected: all pass.
