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
  -> Wails desktop workbench / MCP server / private kbase HTTP server
  -> exports for health KB, quant rule cards, and NotebookLM bridge packages
```

The desktop workbench is Wails-native and calls generated `frontend/wailsjs/go/backend/App.*` bindings. The private kbase server is HTTP-native and exposes read-only book and System KB endpoints under `/api/*`.

## Web KBase Expansion Point

The web UI should not reuse the Wails runtime. It should be an independent browser app served by `cmd/kbase-server`, using the existing Bearer-protected HTTP API. This keeps desktop and browser runtimes separate while sharing the same `BookKnowledgeStore` and System KB export files.

## Agent Skills Expansion Point

Agent-facing discovery is exposed by `cmd/kbase-server` as public descriptors under `/.well-known/dedao-kbase-skills.json` and `/api/skills/*`. Descriptor routes are safe to publish; `invoke` routes stay Bearer-protected and read from the same book knowledge and System KB sources. Downstream systems such as health, Reva, and proofroom should treat returned book claims as draft source material unless their own review gates promote them.
