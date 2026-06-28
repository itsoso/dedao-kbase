# Web KBase TokenPlan Chat Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add paginated book navigation and TokenPlan-powered chat to the browser KBase workbench.

**Architecture:** Extend `cmd/kbase-server`'s existing Bearer-protected HTTP API to wrap the already implemented `backend/app` book chat, prompt, and history functions. Update `frontend-web` to consume those endpoints while preserving the independent browser runtime and server-side TokenPlan secret boundary.

**Tech Stack:** Go `net/http` + `httptest`, Vue 3, Vite, TypeScript, plain CSS, Node smoke scripts, Aliyun TokenPlan OpenAI-compatible chat.

---

### Task 1: Backend HTTP Tests For Pagination And Chat APIs

**Files:**
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing pagination test**

Add `TestKBaseHTTPHandlerListsBooksWithPagination`:
- save three sample book packages.
- request `/api/books?page=2&page_size=1&q=book&sort=updated_at_desc`.
- assert status 200, one book, `page`, `page_size`, `total`, and `total_pages`.

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerListsBooksWithPagination' -count=1
```

Expected: FAIL because `/api/books` does not return pagination metadata.

**Step 2: Write failing chat route test**

Add `TestKBaseHTTPHandlerServesBookPromptsChatAndHistory`:
- use a fake TokenPlan HTTP server through `DEDAO_TOKENPLAN_ENV_FILE`.
- assert `GET /api/books/42/prompts` returns prompts.
- assert `POST /api/books/42/chat` returns answer, model, sources, and context stats.
- assert `GET /api/books/42/chat-history?limit=10` returns the saved chat.

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerServesBookPromptsChatAndHistory' -count=1
```

Expected: FAIL because the routes do not exist.

### Task 2: Backend HTTP Implementation

**Files:**
- Modify: `backend/app/kbase_http.go`

**Step 1: Implement paginated books response**

Create a helper that normalizes:
- `page` default 1, minimum 1.
- `page_size` default 30, minimum 1, maximum 100.
- `q` filters by `book_id`, title, author, status, extractor.
- `sort` supports `updated_at_desc` and `title_asc`.

**Step 2: Implement prompts/chat/history routes**

Add routes before generic `/api/books/{book_id}`:
- `/api/books/{book_id}/prompts`
- `/api/books/{book_id}/chat`
- `/api/books/{book_id}/chat-history`

For chat, decode JSON into `BookKnowledgeChatRequest`, force `BookID` from the path, and call `BookKnowledgeChat`.

**Step 3: Verify**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1
```

Expected: PASS.

### Task 3: Frontend API Client And Smoke Test

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Update smoke test**

Assert `api.ts` exposes:
- `listBooksPage`
- `getBookPrompts`
- `chatWithBook`
- `getBookChatHistory`

Assert `App.vue` references:
- `bookPagination`
- `chat-panel`
- `prompt templates`
- `chatHistory`

Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
```

Expected: FAIL before implementation.

**Step 2: Implement API client methods**

Add TypeScript interfaces for paginated book response, prompts, chat response, source ids, context stats, and history items.

**Step 3: Verify smoke**

Run the smoke script. Expected: PASS.

### Task 4: Frontend Workbench UI

**Files:**
- Modify: `frontend-web/src/App.vue`
- Modify: `frontend-web/src/style.css`

**Step 1: Paginated book rail**

Replace one-shot `listBooks()` with `listBooksPage()` and add:
- search input.
- page size selector.
- previous/next buttons.
- total count and page display.

**Step 2: Chat panel**

Add Search/Chat segmented tabs in the center panel. Chat panel includes:
- model input.
- prompt template selector.
- quick mode buttons.
- question textarea.
- send button.
- Markdown answer display.
- source list and context stats.
- history rail/list for selected book.

**Step 3: State flow**

On book select:
- load book details.
- load prompts.
- load chat history.
- reset active answer unless restoring history.

**Step 4: Verify frontend build**

Run:

```bash
cd frontend-web && npm run build
```

Expected: PASS.

### Task 5: Docs, Dossier, And Deployment

**Files:**
- Modify: `README.md`
- Modify: `docs/system-map/product-map.md`
- Create: `docs/dossiers/2026-06-28-web-kbase-tokenplan-chat.md`

**Step 1: Document usage**

Add notes that Web Workbench supports paginated books and TokenPlan chat. State that TokenPlan secrets stay server-side.

**Step 2: Verify and build**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1
node frontend-web/scripts/web-kbase-ui-smoke.mjs
cd frontend-web && npm run build
GOOS=linux GOARCH=amd64 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server
git diff --check
```

Expected: PASS.

**Step 3: Deploy**

Upload new Linux binary to `/opt/dedao-kbase/bin/kbase-server`, sync `frontend-web/dist` to `/var/www/kbase.executor.life`, restart `dedao-kbase.service`, reload Nginx if needed, and verify:
- `/health` 200.
- `/api/books?page=1&page_size=5` returns pagination metadata.
- `/api/books/{book_id}/prompts` returns templates.
- Web UI loads current asset hash.
- Browser chat path can be exercised with the configured token.
