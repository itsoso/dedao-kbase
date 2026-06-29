# Dedao Ebook Shelf Actions Design

## Goal

Full-site ebook search results should support both explicit shelf add and one-click action flows: add to shelf, download, and add to the book knowledge base.

## Scope

- Add a private KBase HTTP endpoint for adding a Dedao ebook to the user's ebook shelf.
- Reuse the existing Dedao `EbookShelfAdd` service wrapper.
- Hydrate the added ebook after shelf add so the web UI has `id`, `enid`, title, cover, and purchase state for download and knowledge-base jobs.
- Update the web ebook search list so each result has `加入书架`, `下载`, and `加入知识库`.
- Keep explicit add-to-shelf (B) and automatic add-before-action (A).
- Enable the reader toolbar `加入书架` action when the current ebook is not already on the shelf.

## Architecture

The KBase HTTP server remains the only web-facing backend. A new Bearer-protected `POST /api/dedao/ebooks/{enid}/bookshelf` route validates the `enid`, calls the live provider, and returns a normalized `DedaoEbook`. The live provider calls `EbookShelfAdd`, then `EbookDetail` to hydrate the ebook for follow-up jobs.

The frontend treats shelf add as an idempotent preparation step. In full-site search, `加入书架` calls the route directly. `下载` and `加入知识库` call an `ensureEbookOnShelf` helper first, then create the existing background jobs. If Dedao rejects the shelf add, the UI shows the error and does not create the follow-up job.

## Error Handling

- Missing or malformed `enid` returns `400`.
- Missing Bearer token remains `401`.
- Dedao or hydration failures return a visible non-2xx error through the existing API error surface.
- The UI keeps per-book action loading states, so a failed add only affects the selected row.

## Testing

- Backend route tests cover authorization, provider invocation, and response shape.
- Frontend smoke checks assert the API client, search list actions, automatic add-before-job flow, and reader toolbar integration.
- Release checks: focused Go tests, frontend smoke, full frontend build, and `git diff --check`.
