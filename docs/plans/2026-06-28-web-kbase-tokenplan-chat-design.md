# Web KBase TokenPlan Chat Design

## Goal

Upgrade the browser KBase workbench into a practical book-analysis workspace: paginated book navigation, TokenPlan-powered book chat, prompt templates, chat history, Markdown answers, and source-aware context stats.

## Scope

The first release brings the main desktop `书籍知识库` chat workflow to `frontend-web`:

- Paginate the left book rail with `page`, `page_size`, `q`, and sort metadata.
- Chat with TokenPlan against one selected book at a time.
- Support quick modes: summary, analysis, actions, rules, and custom chat.
- Load prompt templates generated from the selected book.
- Persist and restore chat history through the existing local SQLite history store.
- Render Markdown answers and show source ids plus context stats.

Out of scope for this release: streaming output, multi-book notebooks, write/import actions into health/proofroom, public unauthenticated chat, and exposing TokenPlan secrets to the browser.

## Architecture

Reuse the existing Go chat layer instead of duplicating TokenPlan logic in the web app. `backend/app/book_chat.go` already owns TokenPlan config lookup, grounded context construction, LLM calls, source ids, and chat history persistence. `cmd/kbase-server` will expose a small Bearer-protected HTTP facade over those functions.

```text
Browser Workbench
  -> /api/books?page=...&page_size=...&q=...
  -> /api/books/{book_id}/prompts
  -> /api/books/{book_id}/chat
  -> /api/books/{book_id}/chat-history
  -> backend/app.BookKnowledgeStore + BookKnowledgeChat
  -> TokenPlan OpenAI-compatible /chat/completions
```

TokenPlan API keys stay server-side in `DEDAO_TOKENPLAN_*`, `TOKENPLAN_*`, or the existing health env fallback. Browser auth remains the existing `KBASE_AUTH_TOKEN` Bearer flow.

## UI Structure

The page remains a dense workbench:

- Left rail: search box, paginated book list, current page, page size, previous/next controls.
- Center panel: tabs for Search and Chat. Chat includes model field, quick prompt buttons, prompt template selector, question composer, send button, answer panel, source list, and context stats.
- Right panel: selected book details with Overview, Chapters, Claims, Chunks, and System KB.

The UI should stay utilitarian and data-dense. Avoid decorative landing-page patterns; optimize for repeated use, scanning, and fast book switching.

## API Contract

`GET /api/books?page=1&page_size=30&q=keyword&sort=updated_at_desc`

Returns:

```json
{
  "books": [],
  "page": 1,
  "page_size": 30,
  "total": 120,
  "total_pages": 4
}
```

`GET /api/books/{book_id}/prompts`

Returns generated prompt templates from `GenerateBookKnowledgePrompts`.

`POST /api/books/{book_id}/chat`

```json
{
  "mode": "chat",
  "question": "这本书的核心方法是什么？",
  "model": "MiniMax-M2.5",
  "max_context_chars": 12000
}
```

Returns the existing `BookKnowledgeChatResponse`.

`GET /api/books/{book_id}/chat-history?limit=50`

Returns persisted chat history.

## Error Handling

- Missing or invalid Bearer token stays 401.
- Missing TokenPlan config returns a clear server error without leaking secrets.
- TokenPlan non-2xx responses include status and body snippet, never API keys.
- Empty custom chat question is rejected.
- Pagination normalizes invalid page/page_size values and caps page size.

## Governance

Web chat is for private book analysis. Answers and generated project-import text are draft material. health/Reva must not treat dedao chat output as runtime authority until their own review gates promote it. proofroom may use results as evidence leads with source ids.

## Acceptance Criteria

- Browser login still auto-fills `KBASE_AUTH_TOKEN`.
- Left book rail can search and paginate without loading every book into the visible list.
- Chat quick modes and custom questions call TokenPlan via the server.
- Prompt templates and history load for the selected book.
- Answers render as Markdown and expose source/context stats.
- Existing `/api/books`, `/api/search`, System KB, skills, and Web static routes keep working.
