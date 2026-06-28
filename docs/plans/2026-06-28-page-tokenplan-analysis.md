# Page TokenPlan Analysis Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add TokenPlan one-shot analysis to Web course and ebook detail pages.

**Architecture:** The backend exposes a generic protected page-analysis endpoint that reuses the existing TokenPlan client. The frontend packages current course or ebook page context into bounded sections and renders the Markdown answer in a reusable panel.

**Tech Stack:** Go `net/http`, existing `backend/app` TokenPlan client, Vue 3 script setup, TypeScript, Vite, existing Markdown renderer.

---

### Task 1: Backend Analysis Domain

**Files:**
- Create: `backend/app/page_analysis.go`
- Test: `backend/app/page_analysis_test.go`

**Step 1: Write failing tests**

Cover:
- Empty context is rejected.
- Request with model `qwen3.7-max` builds a prompt that includes page title, selected section, and question.
- Response includes answer, model, mode, source, and context stats.

**Step 2: Run failing test**

Run: `go test ./backend/app -run 'TestPageAnalysis' -count=1`

Expected: FAIL because the page-analysis types and functions do not exist.

**Step 3: Implement minimal domain code**

Add request/response structs, context trimming, prompt construction, and `AnalyzePageWithClient`.

**Step 4: Run green test**

Run: `go test ./backend/app -run 'TestPageAnalysis' -count=1`

Expected: PASS.

### Task 2: HTTP Endpoint

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing HTTP test**

Add a `POST /api/analyze-page` test using an httptest TokenPlan server and bearer auth. Assert status 200 and response model `qwen3.7-max`.

**Step 2: Run failing test**

Run: `go test ./backend/app -run 'TestKBaseHTTPHandlerServesPageAnalysis' -count=1`

Expected: FAIL with 404.

**Step 3: Add handler route**

Require `POST`, decode JSON, call `AnalyzePage`, and return JSON.

**Step 4: Run green test**

Run: `go test ./backend/app -run 'TestKBaseHTTPHandlerServesPageAnalysis' -count=1`

Expected: PASS.

### Task 3: Frontend API and Shared Panel

**Files:**
- Modify: `frontend-web/src/api.ts`
- Create: `frontend-web/src/components/PageAnalysisPanel.vue`

**Step 1: Add types and client method**

Add `PageAnalysisSection`, `PageAnalysisRequest`, `PageAnalysisResponse`, and `analyzePage`.

**Step 2: Add component**

Create a compact panel with model select, quick prompts, textarea, submit button, error state, context stats, and Markdown-rendered answer.

### Task 4: Wire Course and Ebook Pages

**Files:**
- Modify: `frontend-web/src/views/CourseDetailReader.vue`
- Modify: `frontend-web/src/views/EbookDetailReader.vue`

**Step 1: Course context**

Build sections from course metadata, article list, and selected article Markdown.

**Step 2: Ebook context**

Build sections from ebook metadata, catalog, selected chapter, and loaded page text extracted from SVG tags.

**Step 3: Layout**

Place the panel in the right context column without adding top-level navigation.

### Task 5: Verification and Deploy

**Commands:**
- `gofmt -w backend/app/page_analysis.go backend/app/page_analysis_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go`
- `go test ./backend/app -count=1`
- `go test ./... -count=1`
- `npm --prefix frontend-web run build`
- `git diff --check`

Build and deploy the updated frontend and `kbase-server` only after all checks pass.
