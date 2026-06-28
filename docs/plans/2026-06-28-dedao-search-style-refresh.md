# Dedao Search Style Refresh Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Update the Web UI styling to follow the visual language of dedao.cn search/result pages.

**Architecture:** Keep the existing Vue components and routes. Add global design tokens in `frontend-web/src/style.css`, then override local list/detail styles in the current course, ebook, and KBase views where scoped CSS prevents global rules from applying.

**Tech Stack:** Vue 3, scoped CSS, Vite, existing frontend-web package.

---

### Task 1: Global Shell and Tokens

**Files:**
- Modify: `frontend-web/src/style.css`
- Modify: `frontend-web/src/App.vue` only if required.

**Steps:**
1. Add CSS variables for Dedao orange, page background, muted text, dividers, and module background.
2. Change body and shell background to white/light gray.
3. Restyle buttons, inputs, selects, shell header, and nav to match compact Dedao search styling.
4. Run `npm --prefix frontend-web run build`.

### Task 2: Result-Like Lists

**Files:**
- Modify: `frontend-web/src/views/CourseLibrary.vue`
- Modify: `frontend-web/src/views/EbookLibrary.vue`
- Modify: `frontend-web/src/views/KBaseWorkbench.vue`

**Steps:**
1. Convert course and ebook rows from heavy cards to separated result rows.
2. Use orange for active/primary states and green only where it carries status semantics.
3. Preserve download and knowledge-base action buttons.
4. Run `npm --prefix frontend-web run build`.

### Task 3: Detail and Analysis Surfaces

**Files:**
- Modify: `frontend-web/src/views/CourseDetailReader.vue`
- Modify: `frontend-web/src/views/EbookDetailReader.vue`
- Modify: `frontend-web/src/components/PageAnalysisPanel.vue`

**Steps:**
1. Align side panels to shallow gray module styling.
2. Keep Markdown reader content high contrast and readable.
3. Ensure TokenPlan panel remains compact.
4. Run `npm --prefix frontend-web run build`.

### Task 4: Screenshot Verification and Deploy

**Commands:**
- `npm --prefix frontend-web run build`
- Use local Vite preview or dev server and capture `/ebook`, `/course`, `/book-knowledge`.
- `git diff --check`

Deploy only after build and visual checks pass.
