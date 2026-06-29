# Dedao Site Ebook Search Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a protected Web GUI and API path for dedao.cn site-wide ebook search without changing the existing purchased bookshelf endpoint.

**Architecture:** Add a `SearchEbooks` method to the existing `DedaoContentProvider`, route it through `/api/dedao/search/ebooks`, and expose it in `frontend-web/src/api.ts`. The `/ebook` view gets a compact source switch that decides whether to call bookshelf listing or full-site search.

**Tech Stack:** Go HTTP handler and service layer, Vue 3 `<script setup>`, TypeScript API client, existing smoke scripts, existing kbase-server deployment flow.

---

### Task 1: Backend API Contract

**Files:**
- Modify: `backend/app/dedao_content.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write the failing test**

Add `TestKBaseHTTPHandlerServesDedaoSiteEbookSearch` beside the current ebook list test. The fake provider should record `gotSearchEbookQuery`, `gotSearchEbookPage`, and `gotSearchEbookPageSize`, and return a `DedaoEbookPage` with one ebook.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./backend/app -run TestKBaseHTTPHandlerServesDedaoSiteEbookSearch -count=1
```

Expected: FAIL because the route and provider method do not exist.

**Step 3: Write minimal implementation**

Extend `DedaoContentProvider` and `fakeDedaoContentProvider`, add `SearchEbooks`, add the `/api/dedao/search/ebooks` route before the `/api/dedao/ebooks/` prefix route, and add `handleDedaoSearchEbooks`.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerServesDedao(SiteEbookSearch|Ebooks)' -count=1
```

Expected: PASS.

### Task 2: Service-Layer Dedao Search

**Files:**
- Modify: `backend/services/requester.go`
- Modify: `backend/services/sunflower.go` or create `backend/services/search.go`
- Modify: `backend/app/dedao_content.go`

**Step 1: Write the failing mapper test**

Add a small unit test for mapping upstream search product fields into `DedaoEbook`. Include `product_enid`, `product_id`, `title`, `intro`, `index_image`, `price`, and `author_list`.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./backend/app -run TestDedaoSiteEbookSearchMapping -count=1
```

Expected: FAIL because the mapper does not exist.

**Step 3: Write minimal implementation**

Implement a service helper for the dedao.cn search request and a mapper in `backend/app/dedao_content.go`. Keep the mapper tolerant of optional fields and do not expose raw upstream payloads.

**Step 4: Run tests**

Run:

```bash
go test ./backend/app -run 'TestDedaoSiteEbookSearchMapping|TestKBaseHTTPHandlerServesDedaoSiteEbookSearch' -count=1
```

Expected: PASS.

### Task 3: Frontend API and UI

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/EbookLibrary.vue`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Write the smoke assertions**

Add assertions for `searchDedaoEbooks`, `/api/dedao/search/ebooks`, `searchScope`, and the "全站搜索" label.

**Step 2: Run smoke to verify it fails**

Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
```

Expected: FAIL because the client method and UI switch do not exist.

**Step 3: Write minimal implementation**

Add `searchDedaoEbooks` to the API client. In `EbookLibrary.vue`, add `searchScope` with default `shelf`, render a small segmented source switch, call the right API in `loadEbooks`, and adjust copy from "已购电子书" to source-aware labels.

**Step 4: Run frontend checks**

Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
npm --prefix frontend-web run build
```

Expected: PASS.

### Task 4: Full Verification and Deployment

**Files:**
- Verify only unless test fixes are required.

**Step 1: Run backend checks**

Run:

```bash
go test ./backend/app -count=1
go test ./cmd/kbase-server -count=1
```

Expected: PASS.

**Step 2: Run privacy and diff checks**

Run:

```bash
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: PASS. If privacy script is missing, record that explicitly and run `git diff --check`.

**Step 3: Build and deploy**

Use the existing kbase-server deployment path: build `frontend-web`, build the Linux server binary, sync the dist assets and binary to the host, restart `dedao-kbase.service`.

**Step 4: Online verification**

Verify `/health`, unauthenticated search returns `401`, authenticated search returns JSON with `ebooks`, and `/ebook` serves the new bundle.
