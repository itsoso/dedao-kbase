# Web KBase Learning Layout Design

## Goal

Make the browser workbench feel like a book-learning tool: find a book or passage quickly, keep the selected book as the conversation target, and give most screen space to TokenPlan-assisted study.

## Scope

- Merge book filtering and knowledge search into the left rail.
- Make TokenPlan chat the central primary workspace.
- Replace the free-form model text input with a model selector that defaults to `Qwen-3.7-Max`.
- Render Markdown answers with the same escaped `marked` pipeline used by the desktop GUI.
- Shrink the right details rail by default.
- Add draggable column splitters and persist column widths in `localStorage`.

Out of scope: streaming chat, new backend APIs, multi-book notebooks, and write/import actions into health or proofroom.

## Layout

The page keeps the same independent `frontend-web` runtime served by `cmd/kbase-server`.

```text
Left rail                    Main study panel                   Right reference rail
Book/search controls   |     TokenPlan chat                |     compact book details
Book list + pagination |     prompt templates/history      |     tabs: overview/chapters/claims/chunks
Search results         |     grounded answer + sources     |     System KB
```

The left rail owns all retrieval controls. One keyword field drives both `/api/books?q=...` and `/api/search?q=...`; scope controls decide whether search targets the selected book or all books. The selected book remains explicit and continues to drive prompts, chat, history, and details.

## Interaction Details

- Default model: `qwen3.7-max` with label `Qwen-3.7-Max`.
- Model selector options: `Qwen-3.7-Max`, `MiniMax-M2.5`, and custom-compatible values already accepted by the backend.
- Chat answers render Markdown as headings, lists, tables, blockquotes, and code blocks; raw HTML is escaped before rendering.
- Drag handles sit between columns. Widths are clamped so the left rail remains usable, the chat panel stays dominant, and the right rail can become compact but not disappear.
- Right Overview uses compact summary chips instead of large metric cards.

## Verification

- Extend `frontend-web/scripts/web-kbase-ui-smoke.mjs` with layout hooks: merged search rail, model select, default Qwen model, drag handles, persisted column widths, compact details, and Markdown rendering.
- Run `node frontend-web/scripts/web-kbase-ui-smoke.mjs`.
- Run `cd frontend-web && npm run build`.
