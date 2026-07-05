# Course Reader Stability P0 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Web course reading stable by standardizing article pagination, prefetching the next article, and restoring the last reading position.

**Architecture:** Extend the existing Bearer-protected Dedao course article endpoint instead of adding a new service. The backend returns explicit pagination metadata; the Vue course reader consumes that metadata, keeps the next page enabled, prefetches the next article Markdown, and stores per-course reading position in browser localStorage.

**Tech Stack:** Go `backend/app` HTTP DTO/tests, Vue 3 `<script setup>`, TypeScript API client, existing `frontend-web/scripts/web-kbase-ui-smoke.mjs`, Vite build, kbase static deployment.

---

### Task 1: Backend Course Article Page Contract

**Files:**
- Modify: `backend/app/dedao_content.go`
- Modify: `backend/app/kbase_http_test.go`

**Steps:**
1. Add a failing HTTP handler test that expects `/api/dedao/courses/{enid}/articles` to serialize `loaded_count`, `article_count`, and `next_cursor`.
2. Add fields to `DedaoArticlePage`.
3. Populate those fields from live course article list calls, using course `article_count` when available and falling back safely when it is not.
4. Run `go test ./backend/app -run 'TestKBaseHTTPHandlerServesDedaoCourseArticles' -count=1`.

### Task 2: Frontend Pagination Contract Consumption

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/CourseDetailReader.vue`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Steps:**
1. Add failing smoke assertions for `article_count`, `loaded_count`, `next_cursor`, next-article prefetch, and reading-position persistence.
2. Update the TypeScript `DedaoArticlePage` interface.
3. Use `next_cursor` instead of guessing from the last article where possible.
4. Use backend `article_count` to drive total pages and has-more state.
5. Add a small Markdown cache/prefetch path for the next article.
6. Persist `selectedArticleEnid`, scroll position, and loaded article cursor by course ID in localStorage.

### Task 3: Verification And Deployment

**Files:**
- Build output only in `frontend-web/dist/` for deployment, not committed.

**Steps:**
1. Run `node frontend-web/scripts/web-kbase-ui-smoke.mjs`.
2. Run `npm --prefix frontend-web run build`.
3. Run `go test ./backend/app -run 'TestKBaseHTTPHandlerServesDedaoCourse(Detail|Articles)$' -count=1`.
4. Run `git diff --check`.
5. Sync `frontend-web/dist/` to the configured online static web root.
6. Verify the online `/health` endpoint and static bundle references.
