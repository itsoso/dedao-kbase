# Book Knowledge TokenPlan Chat Design

## Goal

Add a NotebookLM-like conversation entry to the `dedao-gui` book knowledge workbench. The user can select one imported book, ask questions, and run common analysis prompts through Aliyun TokenPlan.

## Scope

First release:

- Add a `对话` tab to `书籍知识库`.
- Use the selected book knowledge package as the only grounding source.
- Support quick prompts: summarize, analyze, extract actions, extract executable rules.
- Call Aliyun TokenPlan through the OpenAI-compatible chat completions API.
- Reuse `health-llm-driven` TokenPlan configuration:
  - `TOKENPLAN_API_KEY`
  - `TOKENPLAN_BASE_URL`
  - `TOKENPLAN_MODEL`
- Keep the API key in Go backend memory only. Never expose it to Vue or generated frontend bindings.

Not in this release:

- Streaming output.
- Persisted chat history.
- Multi-book notebooks.
- Automatic import into external repos after a chat response.

## Architecture

The GUI remains a read-only frontend over the local `book_knowledge` package. A new backend chat layer builds a compact prompt from book metadata, chapter summaries, claims, and a bounded set of matching chunks. It then calls TokenPlan using the same OpenAI-compatible endpoint shape used by `health-llm-driven`.

Configuration resolves in this order:

1. `DEDAO_TOKENPLAN_*`
2. `TOKENPLAN_*`
3. `DEDAO_TOKENPLAN_ENV_FILE`
4. `/Users/liqiuhua/work/personal/health-llm-driven/backend/.env`
5. `/Users/liqiuhua/work/personal/health-llm-driven/.env`

The fallback is a runtime read, not a committed copy of secrets.

## Data Flow

1. User selects a book in `书籍知识库`.
2. User chooses a quick prompt or enters a custom question.
3. Vue calls `BookKnowledgeChat(bookID, mode, question, model)`.
4. Go loads the book package and builds a context bundle:
   - book title and id
   - chapter titles and summaries
   - draft/reviewed claims
   - query-matched chunks
5. Go sends system/user messages to TokenPlan.
6. The response is returned with model, context stats, and source ids.

## Error Handling

- Missing TokenPlan key returns a clear backend error.
- Missing book id or unknown book returns normal store errors.
- TokenPlan non-2xx responses include status and body snippet, with no API key.
- Empty question in custom mode is rejected.

## Testing

Backend tests cover:

- TokenPlan config fallback from a health env file.
- Prompt/context construction from a local book package.
- OpenAI-compatible HTTP request shape with `httptest`.

Frontend is verified by `npm run build` and `wails build`.
