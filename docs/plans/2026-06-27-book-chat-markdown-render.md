# Book Chat Markdown Render Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render TokenPlan Markdown answers in the book knowledge chat while preserving a raw Markdown view.

**Architecture:** Add a tiny frontend Markdown rendering helper that wraps the existing `marked` dependency with raw HTML escaping. The chat answer panel gets a segmented view switch: rendered Markdown by default and raw Markdown as a fallback.

**Tech Stack:** Vue 3, Element Plus, marked, Node smoke test, Vite build.

---

### Task 1: Markdown Render Helper

**Files:**
- Create: `frontend/src/utils/markdownRender.js`
- Create: `frontend/scripts/markdown-render-smoke.mjs`

**Step 1: Write failing test**

Test that Markdown headings and bold text render, and raw HTML input is escaped.

Run:

```bash
node frontend/scripts/markdown-render-smoke.mjs
```

Expected: FAIL because the helper does not exist.

**Step 2: Implement helper**

Use `marked` with `mangle: false`, `headerIds: false`, and escape raw HTML before parsing.

**Step 3: Verify**

Run the smoke test. Expected: PASS.

### Task 2: Chat Answer Toggle

**Files:**
- Modify: `frontend/src/views/BookKnowledge.vue`

**Step 1: Add UI state**

Add `answerView = ref('rendered')` and computed `renderedChatAnswer`.

**Step 2: Add segmented control**

Show `渲染 / Markdown` when a chat response exists. Default to rendered.

**Step 3: Render**

Use `v-html="renderedChatAnswer"` only for escaped Markdown output. Keep the raw view as text interpolation.

### Task 3: Verification

Run:

```bash
node frontend/scripts/markdown-render-smoke.mjs
cd frontend && npm run build
wails build
```

Expected: all pass, with only existing Vite bundle warnings.
