---
last-reviewed: 2026-06-28
---

# Product Map

## User-Facing Areas

- Home and discovery surfaces are routed through `frontend/src/router/index.ts` and Vue views under `frontend/src/views/`.
- Course, listening-book, ebook, topic, and knowledge-city experiences call Wails-bound Go methods in `backend/`, which delegate to service wrappers under `backend/services/`.
- Download and export workflows use backend helpers for filesystem, media, PDF, EPUB, Markdown, and audio handling.
- Book knowledge workflows use `backend/app` domain modules and the `BookKnowledge.vue` workbench.

## Book Knowledge Flow

```text
Downloaded ebook HTML
  -> backend/app extraction
  -> local book_knowledge package
  -> Wails desktop workbench / MCP server / private kbase HTTP server / browser workbench
  -> exports for health KB, quant rule cards, and NotebookLM bridge packages
```

The desktop workbench is Wails-native and calls generated `frontend/wailsjs/go/backend/App.*` bindings. The private kbase server is HTTP-native and exposes Bearer-protected book, search, System KB, prompt, chat, chat-history, Dedao session/login, read-only course browser/detail/article reading, read-only ebook bookshelf/detail/chapter-page reading, listening-book bookshelf/detail/transcript reading, and job endpoints under `/api/*`.

## Web KBase Expansion Point

The web UI does not reuse the Wails runtime. It is an independent browser app served by `cmd/kbase-server`, using the existing Bearer-protected HTTP API. This keeps desktop and browser runtimes separate while sharing the same `BookKnowledgeStore`, prompt generator, TokenPlan chat layer, chat history store, job store, System KB export files, and server-side Dedao login config. TokenPlan secrets, Dedao cookies, and ebook read tokens remain server-side in environment/configuration. Chat history uses SQLite when cgo is available and a JSONL file fallback for cross-compiled `CGO_ENABLED=0` server builds. Online jobs are recorded in `jobs.json` so Linux cross-compiled deployments can create NotebookLM, health KB, quant rule-card, ebook download, ebook-to-kbase sync, listening-book download, and listening-book-to-kbase sync tasks without SQLite. Browser navigation exposes the current online surfaces: library search, book study, QR login, personal center status, read-only course browser/detail/article reading, ebook bookshelf/detail/chapter reading with download and kbase sync actions, listening-book bookshelf/detail/transcript reading with audio/PDF/Markdown downloads, kbase sync actions, and TokenPlan analysis, jobs, System KB, Skills/API discovery, and basic ops status.

## Agent Skills Expansion Point

Agent-facing discovery is exposed by `cmd/kbase-server` as public descriptors under `/.well-known/dedao-kbase-skills.json` and `/api/skills/*`. Descriptor routes are safe to publish; `invoke` routes stay Bearer-protected and read from the same book knowledge and System KB sources. Downstream systems such as health, Reva, and proofroom should treat returned book claims as draft source material unless their own review gates promote them.
