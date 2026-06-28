# Web Ebook Actions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Web ebook shelf buttons for downloading ebooks and adding ebooks to the book knowledge base.

**Architecture:** Extend the existing JSON-backed `BookKnowledgeJob` system with Dedao ebook job types. Reuse the existing desktop ebook download and wiki-sync functions server-side. Update `frontend-web/src/views/EbookLibrary.vue` to create jobs and show action status.

**Tech Stack:** Go HTTP/job runner, existing Dedao ebook downloader, Vue 3, Vite, existing Web smoke script.

---

### Task 1: Backend Job Contracts

**Files:**
- Modify: `backend/app/book_jobs.go`
- Modify: `backend/app/book_jobs_test.go`

**Step 1:** Add failing tests for:
- creating `dedao_ebook_download` with `ebook_id`, `ebook_enid`, `download_type`;
- creating `dedao_ebook_sync_kbase` with `ebook_id`, `ebook_enid`;
- rejecting missing `ebook_id` or `ebook_enid`;
- executing each job through stubbed job runners.

**Step 2:** Run:

```bash
go test ./backend/app -run 'TestBookKnowledgeJob.*DedaoEbook' -count=1
```

Expected: fail on missing types/fields/runners.

**Step 3:** Implement:
- new job constants;
- new `BookKnowledgeJob` and request fields;
- request normalization;
- execution branches for download and sync.

### Task 2: Frontend Ebook Buttons

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/EbookLibrary.vue`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1:** Add smoke assertions for `ebook-action-bar`, `dedao_ebook_download`, `dedao_ebook_sync_kbase`, and job status rendering.

**Step 2:** Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
```

Expected: fail on missing UI/action hooks.

**Step 3:** Implement:
- typed request fields in `BookKnowledgeJobRequest`;
- row buttons and download format selector;
- action handlers that call `createJob`;
- selected ebook job status panel.

### Task 3: Verification And Deploy

Run:

```bash
go test ./backend/app -count=1
node frontend-web/scripts/web-kbase-ui-smoke.mjs
cd frontend-web && npm run build
git diff --check
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/kbase-server-ebook-actions ./cmd/kbase-server
```

Deploy the Linux binary and `frontend-web/dist`, restart `dedao-kbase.service`, then verify:

- `/health` returns 200;
- unauthenticated `/api/jobs` returns 401;
- Bearer `POST /api/jobs` accepts the new job payload shape without exposing secrets;
- `/ebook` assets contain the new action hooks.
