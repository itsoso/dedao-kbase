# Book as Agent Platform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make every subscribed book open at a stable source page and establish the route contract for promoting it into a knowledge package, agent, and app.

**Architecture:** Separate source identities from local package identities in the frontend router. Resolve a source-book detail from the existing Dedao library API, render metadata independently of download state, and keep package/agent/app links explicit lifecycle transitions.

**Tech Stack:** Vanilla JavaScript frontend, CSS, Node smoke tests, Go KBase HTTP service.

---

### Task 1: Lock the route contract with a failing smoke test

**Files:**
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

1. Assert that `getDedaoEbookRoute`, `loadDedaoEbookDetail`, and
   `renderDedaoEbookDetail` exist.
2. Assert that `getBookID` no longer consumes `/sources/dedao/ebooks/`.
3. Run `node frontend-web/scripts/book-knowledge-web-smoke.mjs` and confirm it
   fails because the source detail implementation is missing.

### Task 2: Implement the source-book detail route

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`

1. Parse the canonical source route into a source `enid`.
2. Load pages from `/api/dedao/library?category=ebook` until the source book is
   found or the bounded search is exhausted.
3. Render cover, metadata, introduction, progress, source identifiers, and clear
   loading/error/empty states.
4. Handle this route before the local reader fallback in `boot()`.
5. Run the smoke test and `node --check frontend-web/app.js`.

### Task 3: Expose the book lifecycle

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`

1. Link the source book to filtered knowledge-package search.
2. Display package, agent, and app states as explicit lifecycle stages.
3. Keep unavailable stages disabled with explanatory text rather than broken
   links.
4. Re-run the frontend smoke checks.

### Task 4: Verify and publish

**Files:**
- Modify only generated architecture files if their source inventory changes.

1. Run relevant frontend smoke tests and syntax checks.
2. Run `bash scripts/privacy-smoke.sh` and `git diff --check`.
3. If architecture surfaces changed, regenerate and verify the system map.
4. Commit only the files from this feature, push the branch, deploy using the
   repository release path, and verify the canonical production URL.
