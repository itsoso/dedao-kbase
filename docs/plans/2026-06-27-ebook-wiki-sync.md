# Ebook Wiki Sync Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a single-book `dedao-gui` action that downloads a Dedao ebook to `down-dedao/Ebook` and triggers wiki extraction plus compiler refresh.

**Architecture:** Reuse the existing authenticated ebook downloader and add a small orchestration layer after HTML generation. The new layer treats `llms-wikis` as an external local command, reports command output on failure, and emits Wails progress events through the existing `ebookDownload` channel.

**Tech Stack:** Go, Wails v2, Vue 3, Element Plus, external `llms-wikis`, Python `pipeline/compiler.py`.

---

### Task 1: Backend Sync Planner Tests

**Files:**
- Create: `backend/app/ebook_wiki_test.go`
- Modify: `backend/app/download.go`

**Step 1: Write the failing tests**

Add tests that verify:

- generated ebook file names resolve under `<repo>/Ebook`;
- wiki ingest command is built as `llms-wikis ingest-ebook --repo <repo> --input <html> --book-id <id> --title <title>`;
- compiler command is built as `python3 pipeline/compiler.py --changed-only` with working directory set to the vault repo.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./backend/app -run 'Test(EbookHTMLPath|EbookWikiCommand|EbookCompilerCommand)' -count=1
```

Expected: FAIL because the new helpers do not exist.

**Step 3: Implement minimal helpers**

Add path and command builder helpers in `backend/app/download.go` or a new focused backend file.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./backend/app -run 'Test(EbookHTMLPath|EbookWikiCommand|EbookCompilerCommand)' -count=1
```

Expected: PASS.

### Task 2: Backend Orchestration Tests

**Files:**
- Modify: `backend/app/ebook_wiki_test.go`
- Modify: `backend/app/download.go`
- Modify: `backend/download.go`

**Step 1: Write the failing tests**

Add tests around a fake command runner:

- if `llms-wikis` fails, the returned error includes command output;
- if compiler fails, the returned error includes command output;
- on success, runner is called first for ingest, then for compile.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./backend/app -run 'TestSyncEbookToWiki' -count=1
```

Expected: FAIL because orchestration is missing.

**Step 3: Implement orchestration**

Add an `EbookWikiSync` service that:

- downloads HTML using the existing ebook flow;
- returns the generated path;
- runs wiki ingest;
- runs compiler;
- emits progress between stages.

Expose it through a Wails backend method, for example `EbookDownloadAndSyncWiki(id int, enid string)`.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./backend/app -run 'TestSyncEbookToWiki' -count=1
```

Expected: PASS.

### Task 3: Frontend Button and Dialog

**Files:**
- Modify: `frontend/src/views/Ebook.vue`
- Modify: `frontend/src/components/DownloadDialog.vue`
- Regenerate if needed: `frontend/wailsjs/go/backend/App.d.ts`, `frontend/wailsjs/go/backend/App.js`

**Step 1: Add UI behavior**

Add a `下载并入 Wiki` icon button next to the existing download button. Use the existing download dialog and progress event, but invoke the new Wails method with fixed HTML output and wiki sync.

**Step 2: Handle success/failure**

Show a success message only after compiler completes. Show warning/error messages on any backend error.

**Step 3: Verify TypeScript/Wails bindings**

Run the project binding generation or frontend type check command used by the repo. If generation tooling is unavailable, update generated bindings narrowly and document the limitation.

### Task 4: Verification

**Files:**
- No new files unless generated bindings change.

**Step 1: Focused backend tests**

Run:

```bash
go test ./backend/app -count=1
```

Expected: PASS.

**Step 2: Backend compile/test smoke**

Run:

```bash
go test ./backend/... -count=1
```

Expected: PASS or record unrelated pre-existing failures.

**Step 3: Frontend validation**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS.

**Step 4: Manual dry run**

If `llms-wikis` is installed and the user is logged in, trigger one ebook from the UI. Confirm:

- file is created under `/Users/liqiuhua/work/personal/down-dedao/Ebook`;
- wiki ingest runs;
- `pipeline/compiler.py --changed-only` runs;
- errors are visible when a command is missing or fails.
