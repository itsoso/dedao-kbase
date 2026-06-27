# Book Knowledge TokenPlan Chat Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a NotebookLM-like TokenPlan chat and analysis entry to the `dedao-gui` book knowledge workbench.

**Architecture:** Backend Go code loads TokenPlan configuration from local environment or the existing `health-llm-driven` env files, builds a bounded book-grounded prompt, and calls the OpenAI-compatible TokenPlan `/chat/completions` API. The Vue workbench adds a `对话` tab with quick prompts and custom questions.

**Tech Stack:** Go, Wails v2, Vue 3, Element Plus, Aliyun TokenPlan OpenAI-compatible chat completions API.

---

### Task 1: TokenPlan Config Loader

**Files:**
- Create: `backend/app/book_chat.go`
- Create: `backend/app/book_chat_test.go`

**Step 1: Write the failing test**

Test that config resolves from a temporary health-style `.env` file using `DEDAO_TOKENPLAN_ENV_FILE` and does not require process env.

Run:

```bash
go test ./backend/app -run 'TestLoadBookTokenPlanConfigFromEnvFile' -count=1
```

Expected: FAIL because the loader does not exist.

**Step 2: Implement config loader**

Add `BookTokenPlanConfig`, `.env` parsing, fallback paths, and defaults for base URL/model.

**Step 3: Verify**

Run the same test. Expected: PASS.

### Task 2: Prompt Builder and Chat Orchestrator

**Files:**
- Modify: `backend/app/book_chat.go`
- Modify: `backend/app/book_chat_test.go`

**Step 1: Write failing tests**

Test that a selected book package produces a bounded context and calls a fake LLM client with:

- system instruction
- book title
- chapter summary
- claims
- matching chunks for the question

Run:

```bash
go test ./backend/app -run 'TestBookKnowledgeChatBuildsGroundedPrompt' -count=1
```

Expected: FAIL because the orchestrator is missing.

**Step 2: Implement minimal orchestration**

Add:

- `BookKnowledgeChatRequest`
- `BookKnowledgeChatResponse`
- `BookKnowledgeChatWithClient`
- quick prompt mapping for `summary`, `analysis`, `actions`, `rules`, and `chat`

**Step 3: Verify**

Run backend app tests. Expected: PASS.

### Task 3: TokenPlan HTTP Client

**Files:**
- Modify: `backend/app/book_chat.go`
- Modify: `backend/app/book_chat_test.go`

**Step 1: Write failing test**

Use `httptest.Server` and verify:

- request path is `/chat/completions`
- `Authorization: Bearer ...`
- model is sent
- returned content is parsed from `choices[0].message.content`

Run:

```bash
go test ./backend/app -run 'TestTokenPlanChatClientUsesOpenAICompatibleRequest' -count=1
```

Expected: FAIL because the HTTP client is missing.

**Step 2: Implement client**

Use `net/http` with JSON request/response structs. Do not log secrets.

**Step 3: Verify**

Run backend app tests. Expected: PASS.

### Task 4: Wails Binding

**Files:**
- Modify: `backend/book_knowledge.go`
- Generated/updated by Wails: `frontend/wailsjs/go/backend/App.d.ts`
- Generated/updated by Wails: `frontend/wailsjs/go/backend/App.js`
- Generated/updated by Wails: `frontend/wailsjs/go/models.ts`

**Step 1: Add backend method**

Expose:

```go
BookKnowledgeChat(bookID, mode, question, model string) (*app.BookKnowledgeChatResponse, error)
```

**Step 2: Verify compile**

Run:

```bash
go test . ./backend ./backend/app ./backend/services ./cmd/book-mcp -count=1
```

Expected: PASS.

### Task 5: GUI Chat Tab

**Files:**
- Modify: `frontend/src/views/BookKnowledge.vue`
- Generated/updated: `frontend/components.d.ts`

**Step 1: Add UI**

Add a `对话` tab with:

- model selector
- quick prompt buttons
- textarea for custom question
- send button
- answer panel
- source/context stats

**Step 2: Hook Wails method**

Call `BookKnowledgeChat` with selected book id and selected mode.

**Step 3: Verify frontend**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS.

### Task 6: Final Verification

Run:

```bash
go test ./backend/app -count=1
go test . ./backend ./backend/app ./backend/services ./cmd/book-mcp -count=1
cd frontend && npm run build
wails build
```

Expected:

- focused Go tests pass
- frontend build passes
- Wails app builds

Known existing risk:

- `go test ./backend/... -count=1` may still fail in `backend/utils/TestPrintToPdf` because of the existing Chrome DevTools timeout issue.
