# WeChat Source Adapter Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a first-party WeChat public-account article adapter for private kbase import.

**Architecture:** Implement a backend `WeChatSourceService` that fetches article pages, parses Markdown-ready content, and calls user-authorized WeChat public-platform list APIs only when token/cookie are explicitly configured. Expose the service through authenticated kbase HTTP routes and convert imported articles into the existing `BookKnowledgePackage` format.

**Tech Stack:** Go, `net/http`, `goquery`, existing `backend/app` kbase store, existing `cmd/kbase-server` HTTP server.

---

### Task 1: Article HTML Extraction

**Files:**
- Create: `backend/app/wechat_source_test.go`
- Create: `backend/app/wechat_source.go`

**Step 1: Write the failing test**

Add `TestWeChatSourceDownloadsArticleAsMarkdown` with an `httptest.Server` returning WeChat-like HTML containing `#activity-name`, `#js_name`, `#publish_time`, `#js_content`, text paragraphs, and an image with `data-src`.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run TestWeChatSourceDownloadsArticleAsMarkdown -count=1`

Expected: FAIL because `NewWeChatSourceService` is undefined.

**Step 3: Write minimal implementation**

Create `WeChatSourceService.DownloadArticle(ctx, url)` and parse HTML into:

- `Title`
- `AccountName`
- `PublishedAt`
- `SourceURL`
- `Markdown`
- `Text`

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run TestWeChatSourceDownloadsArticleAsMarkdown -count=1`

Expected: PASS.

### Task 2: Official Account API Client

**Files:**
- Modify: `backend/app/wechat_source_test.go`
- Modify: `backend/app/wechat_source.go`

**Step 1: Write the failing test**

Add `TestWeChatSourceSearchAndListArticlesUseOfficialAPIs`. Use a fake API server and assert:

- `/cgi-bin/searchbiz` receives `token`, `query`, and `cookie`
- `/cgi-bin/appmsg` receives `token`, `fakeid`, `begin`, and `count`
- responses are decoded into account and article structs

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run TestWeChatSourceSearchAndListArticlesUseOfficialAPIs -count=1`

Expected: FAIL because search/list methods are missing.

**Step 3: Write minimal implementation**

Implement:

- `SearchOfficialAccounts(ctx, query)`
- `ListOfficialAccountArticles(ctx, fakeID, begin, count)`
- explicit `ErrWeChatCredentialsNotConfigured`

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run TestWeChatSourceSearchAndListArticlesUseOfficialAPIs -count=1`

Expected: PASS.

### Task 3: KBase HTTP Import Route

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/kbase-server/main.go`

**Step 1: Write the failing test**

Add `TestKBaseHTTPHandlerImportsWeChatArticleIntoBookKnowledge`. POST `{"url":"..."}` to `/api/wechat/import` with a valid Bearer token and assert the saved package contains article title, content chunk, and source citation.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run TestKBaseHTTPHandlerImportsWeChatArticleIntoBookKnowledge -count=1`

Expected: FAIL with 404 or method error.

**Step 3: Write minimal implementation**

Add `WeChatSource *WeChatSourceService` to `KBaseHTTPConfig`, allow POST for `/api/wechat/import`, and add GET routes for `/api/wechat/search` and `/api/wechat/articles`.

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run 'Test(KBaseHTTPHandlerImportsWeChatArticleIntoBookKnowledge|WeChatSource)' -count=1`

Expected: PASS.

### Task 4: Verification

**Files:**
- No source changes unless tests expose issues.

**Step 1: Run focused tests**

Run: `go test ./backend/app -run 'Test(WeChatSource|KBaseHTTPHandler)' -count=1`

Expected: PASS.

**Step 2: Run privacy checks**

Run: `bash scripts/privacy-smoke.sh`

Expected: PASS.

Run: `git diff --check`

Expected: no output.

### Task 5: Online Web Workbench

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Create: `frontend-web/scripts/wechat-source-ui-smoke.mjs`
- Create: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write failing smoke checks**

Add static checks proving the web app has a `/wechat-source` route, calls `/api/wechat/article`, `/api/wechat/import`, `/api/wechat/search`, and `/api/wechat/articles`, and does not embed WeChat token/cookie values.

**Step 2: Implement source UI**

Add a two-column workbench for direct article preview/import, official-account search, recent article listing, and result preview.

**Step 3: Implement book-knowledge fallback UI**

Make `/book-knowledge` render a REST-backed list/search/detail page instead of falling back to the home screen.

**Step 4: Verify**

Run:

- `node frontend-web/scripts/wechat-source-ui-smoke.mjs`
- `node frontend-web/scripts/book-knowledge-web-smoke.mjs`
- `node frontend-web/scripts/ebook-reader-loading-smoke.mjs`
- `node --check frontend-web/app.js`

Expected: all PASS.

### Task 6: WC Plus Local API Compatibility

**Files:**
- Create: `backend/app/wcplus_source_test.go`
- Create: `backend/app/wcplus_source.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/kbase-server/main.go`

**Step 1: Write failing backend tests**

Cover:

- `GET /api/gzh/list` account listing
- `GET /api/report/gzh_articles` article listing
- `GET /api/article/content` Markdown content
- single article import into `BookKnowledgePackage`
- task list/create/control

**Step 2: Implement WC Plus client and kbase routes**

Expose authenticated routes:

- `GET /api/wcplus/gzh/list`
- `GET /api/wcplus/gzh/articles`
- `GET /api/wcplus/article/content`
- `POST /api/wcplus/import/article`
- `POST /api/wcplus/import/account`
- `GET /api/wcplus/task/all`
- `POST /api/wcplus/task/new`
- `POST /api/wcplus/task/control`

**Step 3: Add online UI**

Extend `/wechat-source` with a WC Plus local service panel: account list, article list, preview, single import, latest-N batch import, task list, create task, and task start/stop.

**Step 4: Verify**

Run:

- `go test ./backend/app -run TestWCPlusSource -count=1`
- `go test ./backend/app -run TestKBaseHTTPHandlerProxiesAndImportsWCPlusArticles -count=1`
- `node frontend-web/scripts/wcplus-source-ui-smoke.mjs`

Expected: all PASS.

### Task 7: WC Plus API Parity Completion

**Files:**
- Modify: `backend/app/wcplus_source.go`
- Modify: `backend/app/wcplus_source_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/wcplus-source-ui-smoke.mjs`

**Step 1: Write failing backend tests**

Cover the remaining documented WC Plus local APIs:

- status check for the local service
- account search and import-candidate search
- full-library article list, title search, and full-text search
- reading/statistic/article-owner auxiliary reports
- TXT/CSV export triggers and all-article XLSX file download
- batch task create/delete and queue start command

**Step 2: Implement authenticated kbase proxy routes**

Expose only whitelisted routes under `/api/wcplus/*`, including:

- `GET /api/wcplus/status`
- `GET /api/wcplus/gzh/search`
- `GET /api/wcplus/search-gzh`
- `GET /api/wcplus/article/all`
- `GET /api/wcplus/article/search-title`
- `GET /api/wcplus/search`
- `GET /api/wcplus/report/reading-data`
- `GET /api/wcplus/report/statistic-data`
- `GET /api/wcplus/article/gzh`
- `GET /api/wcplus/like-articles`
- `GET /api/wcplus/request/gzh`
- `GET /api/wcplus/export/text`
- `GET /api/wcplus/export/gzh-csv`
- `POST /api/wcplus/export/all-articles-xlsx`
- `POST /api/wcplus/batch-task/create`
- `POST /api/wcplus/batch-task/delete`

**Step 3: Complete the Web Workbench controls**

Add service status, WC Plus search mode selector, account/result selection, queue start, batch-task cleanup, TXT/CSV triggers, and browser XLSX download. Keep the browser calling only the authenticated kbase proxy, never the local WC Plus URL.

**Step 4: Verify**

Run:

- `go test ./backend/app -run 'TestWCPlusSource|TestKBaseHTTPHandlerProxiesAndImportsWCPlusArticles|TestKBaseHTTPHandlerProxiesAdvancedWCPlusAPIs' -count=1`
- `node frontend-web/scripts/wcplus-source-ui-smoke.mjs`
- `node --check frontend-web/app.js`

Expected: all PASS.

### Task 8: WC Plus Skill Workflow Completion

**Files:**
- Modify: `backend/app/wcplus_source.go`
- Modify: `backend/app/wcplus_source_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/wcplus-source-ui-smoke.mjs`
- Modify: `docs/plans/2026-07-06-wechat-source-adapter-design.md`

**Step 1: Write failing backend tests**

Cover the WC Plus skill workflow:

- `WCPLUS_BASE_URL` takes precedence over `WCPLUSPRO_BASE_URL`, and `WCPLUSPRO_BASE_URL` is accepted as the documented fallback.
- Environment check verifies the local service and `/api/gzh/list`.
- Batch nickname import searches `/api/search_gzh/search`, requires exact nickname matches when requested, creates `gzh_article_link` batch tasks, and optionally starts the task queue.

**Step 2: Implement backend APIs**

Expose authenticated routes:

- `GET /api/wcplus/env/check`
- `POST /api/wcplus/batch-import/gzh`

The batch import route accepts `nicknames`, `articleListType`, `articleListAmount`, `exact_match`, and `start_queue`, then returns `success`, `failed`, `success_text`, and `failed_text`.

**Step 3: Complete the Web Workbench controls**

Add an environment-check button and a textarea-based batch nickname import form. The browser still calls only the authenticated kbase proxy, never the local WC Plus URL.

**Step 4: Verify**

Run:

- `go test ./backend/app -run 'TestWCPlusSource(ConfigFromEnvSupportsWCPlusProBaseURL|BatchImportsNicknamesWithExactMatch|ChecksEnvironment)' -count=1`
- `go test ./backend/app -run TestKBaseHTTPHandlerChecksEnvAndBatchImportsWCPlusNicknames -count=1`
- `node frontend-web/scripts/wcplus-source-ui-smoke.mjs`
- `node --check frontend-web/app.js`

Expected: all PASS.
