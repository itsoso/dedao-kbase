# Book Prompt Studio Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use test-driven-development for backend and frontend behavior checks.

**Goal:** Add a Prompt Studio to book knowledge so each book exposes reusable and book-specific prompts that can be copied, inserted into chat, or run immediately.

**Architecture:** Backend generates static prompt templates plus dynamic prompts from a loaded `BookKnowledgePackage`. Wails exposes `BookKnowledgePrompts(bookID)`. The existing `BookKnowledge.vue` adds a `Prompts` tab and reuses the current chat runner.

**Tech Stack:** Go, Wails, Vue 3, Element Plus, local JSONL book knowledge store.

---

### Task 1: Backend prompt generation

**Files:**
- Create: `backend/app/book_prompts.go`
- Create: `backend/app/book_prompts_test.go`
- Modify: `backend/book_knowledge.go`

**Steps:**
1. Write failing tests for static prompt count, dynamic prompt generation, citation requirements, and quant/health project prompts.
2. Implement prompt types and generation helpers.
3. Expose prompts through `BookKnowledgePrompts`.

### Task 2: Frontend Prompt Studio

**Files:**
- Modify: `frontend/src/views/BookKnowledge.vue`
- Modify: `frontend/scripts/book-knowledge-ui-smoke.mjs`
- Regenerate: `frontend/wailsjs/go/backend/App.*`, `frontend/wailsjs/go/models.ts`

**Steps:**
1. Add smoke assertions for `prompt-studio`, `BookKnowledgePrompts`, `insertPrompt`, and `runPrompt`.
2. Add a `Prompts` tab with category filters and prompt cards.
3. Hook cards to copy, fill the chat box, and run immediately.

### Task 3: Verify and publish

**Commands:**
- `go test . ./backend ./backend/app ./backend/services ./cmd/book-mcp -count=1`
- `node frontend/scripts/book-knowledge-ui-smoke.mjs && node frontend/scripts/markdown-render-smoke.mjs`
- `cd frontend && npm run build`
- `git diff --check`

Commit and push only changed source, docs, tests, and generated bindings to `dedao-kbase/main`.
