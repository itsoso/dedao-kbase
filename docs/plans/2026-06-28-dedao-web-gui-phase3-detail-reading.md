# Dedao Web GUI Phase 3 Detail And Reading Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add read-only course and ebook detail/reading flows to the Web GUI.

**Architecture:** Extend the existing `DedaoContentProvider` facade in `backend/app/dedao_content.go` and route it through `backend/app/kbase_http.go`. Add browser routes/views under `frontend-web/src/views/` that reuse `KBaseClient`, token hydration, and markdown rendering.

**Tech Stack:** Go HTTP handlers, existing Dedao app/service helpers, Vue 3, Vue Router, Vite, `marked` via `frontend-web/src/utils/markdownRender.ts`.

---

### Task 1: Backend Detail And Reading API

**Files:**
- Modify: `backend/app/dedao_content.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write failing tests**

Add tests for:
- `GET /api/dedao/courses/{enid}` returning safe course detail with articles.
- `GET /api/dedao/courses/{enid}/articles` returning safe article list.
- `GET /api/dedao/articles/{enid}?type=course` returning Markdown.
- `GET /api/dedao/ebooks/{enid}` returning safe ebook detail/catalog.
- `GET /api/dedao/ebooks/{enid}/chapters/{chapter_id}/pages` returning decrypted SVG page strings.
- Unauthorized requests return `401`.
- Responses do not contain `cookie`, `token`, `drm_token`, `dd_url`, or `dd_article_token`.

**Step 2: Run tests to verify RED**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerServesDedao(CourseDetail|CourseArticles|ArticleMarkdown|EbookDetail|EbookChapterPages)' -count=1
```

Expected: fail because types/routes/provider methods are missing.

**Step 3: Implement minimal backend**

Add safe DTOs and provider methods:

- `GetCourseDetail(enid string) (DedaoCourseDetail, error)`
- `ListCourseArticles(enid string, count, maxID int) (DedaoArticlePage, error)`
- `GetCourseArticleMarkdown(enid string) (DedaoArticleMarkdown, error)`
- `GetEbookDetail(enid string) (DedaoEbookDetail, error)`
- `GetEbookChapterPages(enid, chapterID string, index, count, offset int) (DedaoEbookChapterPages, error)`

Live provider reuses existing app/service functions. Handler parses path segments and clamps `count` to a small maximum.

**Step 4: Run tests to verify GREEN**

Run:

```bash
go test ./backend/app -count=1
```

Expected: pass.

### Task 2: Frontend Routes, Client, And Views

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/router.ts`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`
- Modify: `frontend-web/src/views/CourseLibrary.vue`
- Modify: `frontend-web/src/views/EbookLibrary.vue`
- Create: `frontend-web/src/views/CourseDetailReader.vue`
- Create: `frontend-web/src/views/EbookDetailReader.vue`

**Step 1: Write failing smoke assertions**

Assert:
- API has `getDedaoCourseDetail`, `listDedaoCourseArticles`, `getDedaoArticleMarkdown`, `getDedaoEbookDetail`, `getDedaoEbookChapterPages`.
- Router includes `/course/:enid` and `/ebook/:enid`.
- New views include `course-detail-reader`, `ebook-detail-reader`, `answer-markdown`, `ebook-page-frame`, and actionable empty/error states.
- List rows navigate to detail routes.

**Step 2: Run smoke to verify RED**

Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
```

Expected: fail on missing methods/views/hooks.

**Step 3: Implement minimal frontend**

Add typed client methods, route definitions, and detail reader views. Course detail loads course detail and articles, then opens article Markdown on click. Ebook detail loads metadata/catalog and opens chapter pages on click. Use existing storage key and browser session hydration.

**Step 4: Run smoke/build to verify GREEN**

Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
cd frontend-web && npm run build
```

Expected: pass.

### Task 3: Docs, Browser Verification, Deploy

**Files:**
- Modify: `README.md`
- Modify: `docs/system-map/product-map.md`
- Modify: `docs/dossiers/2026-06-28-dedao-web-gui-parity.md`
- Create: `docs/dossiers/2026-06-28-dedao-web-gui-phase3-detail-reading.md`

**Step 1: Update docs**

Document the new read-only detail/reading APIs and security boundary.

**Step 2: Run local verification**

Run:

```bash
go test ./backend/app -count=1
node frontend-web/scripts/web-kbase-ui-smoke.mjs
cd frontend-web && npm run build
git diff --check
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/kbase-server-detail-reading ./cmd/kbase-server
```

Expected: pass.

**Step 3: Browser DOM check**

Start a local server with `KBASE_WEB_DIR=frontend-web/dist`, use system Chrome through Playwright, and verify detail routes render without `ModuleLanding`.

**Step 4: Deploy and verify**

Deploy binary and static bundle to `executor.life`, restart `dedao-kbase.service`, then verify:

- `/health` returns `200`.
- Unauthenticated new API routes return `401`.
- Bearer new API routes return `200` with safe payloads or explicit upstream errors.
- `/course/:enid` and `/ebook/:enid` remain Basic Auth protected.

**Step 5: Commit**

Stage only task-related files and commit:

```bash
git commit -m "feat(web): add detail reading flows"
```
