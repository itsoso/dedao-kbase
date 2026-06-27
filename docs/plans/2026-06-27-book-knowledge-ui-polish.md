# Book Knowledge UI Polish Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the `书籍知识库` page feel like a professional book research workbench instead of a raw admin table.

**Architecture:** Keep the existing Wails data flow and Vue component. Improve only layout, copy, state presentation, and CSS in `BookKnowledge.vue`, with a small static smoke script guarding the main UI structure.

**Tech Stack:** Vue 3, Element Plus, scoped CSS, Node smoke script, Vite/Wails build.

---

### Task 1: Static UI Smoke

**Files:**
- Create: `frontend/scripts/book-knowledge-ui-smoke.mjs`

**Step 1: Write failing smoke**

Assert that `BookKnowledge.vue` contains the new layout hooks:

- `knowledge-shell`
- `library-panel`
- `research-panel`
- `chat-composer`
- `answer-report`

Run:

```bash
node frontend/scripts/book-knowledge-ui-smoke.mjs
```

Expected: FAIL before the UI refactor.

### Task 2: Refactor Workbench Layout

**Files:**
- Modify: `frontend/src/views/BookKnowledge.vue`

**Step 1: Update template**

Change the page into a two-column workbench:

- left library header and compact table
- right research header with export actions
- tabs inside a clean research panel
- chat composer grouped as one tool surface
- answer report panel with rendered/raw switch

**Step 2: Update scoped CSS**

Use a restrained professional palette, compact spacing, clear selected states, and left-aligned Markdown report typography.

### Task 3: Verification

Run:

```bash
node frontend/scripts/book-knowledge-ui-smoke.mjs
node frontend/scripts/markdown-render-smoke.mjs
cd frontend && npm run build
wails build
```

Expected: all pass, with only existing Vite bundle warnings.
