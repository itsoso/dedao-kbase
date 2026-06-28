# Web KBase Learning Layout Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rework the browser KBase workbench into a learning-first layout with merged search/book navigation, central model chat, Markdown-rendered answers, compact details, and draggable columns.

**Architecture:** Keep the existing Vue 3 single-page `frontend-web` app and `KBaseClient` API surface. This is a frontend-only layout and interaction change over the already shipped Bearer-protected APIs.

**Tech Stack:** Vue 3 Composition API, TypeScript, Vite, `marked`, plain CSS, Node smoke script, browser `localStorage`.

---

### Task 1: Add Failing UI Contract

**Files:**
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Steps:**
1. Add assertions for `library-search-panel`, `combinedSearchQuery`, `runLibrarySearch`, `model-select`, `qwen3.7-max`, `column-resizer`, `layoutColumns`, `compact-detail-summary`, `renderMarkdown`, and `answer-markdown`.
2. Run `node frontend-web/scripts/web-kbase-ui-smoke.mjs`.
3. Expected: FAIL because these hooks do not exist yet.

### Task 2: Restructure App State And Template

**Files:**
- Modify: `frontend-web/src/App.vue`

**Steps:**
1. Replace the separate middle `search-panel` with a left-rail `library-search-panel`.
2. Introduce `combinedSearchQuery`; use it for `bookQuery` when loading books and for `/api/search` when running retrieval.
3. Move `chat-panel` to the center column as the dominant panel.
4. Change `chatModel` from free-form input to `selectedChatModel` bound to a `<select class="model-select">`.
5. Default selected model to `qwen3.7-max`.
6. Keep selected book, prompts, chat history, and details behavior intact.

### Task 3: Add Draggable Columns

**Files:**
- Modify: `frontend-web/src/App.vue`
- Modify: `frontend-web/src/style.css`

**Steps:**
1. Add `layoutColumns` state with left/main/right widths and `localStorage` persistence.
2. Add two `column-resizer` handles and pointer handlers.
3. Clamp widths to usable ranges.
4. Bind `workbench-grid` columns through CSS variables.

### Task 4: Compact The Detail Rail

**Files:**
- Modify: `frontend-web/src/App.vue`
- Modify: `frontend-web/src/style.css`
- Create: `frontend-web/src/utils/markdownRender.ts`
- Modify: `frontend-web/package.json`
- Modify: `frontend-web/package-lock.json`

**Steps:**
1. Replace large overview metric cards with `compact-detail-summary`.
2. Make the right rail narrower by default.
3. Add the same escaped `marked` Markdown renderer used by the desktop GUI.
4. Render `chatResponse.answer` through `answer-markdown`.
5. Keep tabs and System KB actions available.

### Task 5: Verify And Deploy

**Commands:**

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
cd frontend-web && npm run build
git diff --check
rsync -az --delete frontend-web/dist/ executor.life:/var/www/kbase.executor.life/
ssh executor.life 'nginx -t && systemctl reload nginx && curl -fsS http://127.0.0.1:8719/health'
```

**Expected:** smoke passes, production build passes, no whitespace errors, and deployed health remains OK.
