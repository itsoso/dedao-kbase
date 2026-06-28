---
last-reviewed: 2026-06-28
---

# dedao-gui System Map

## Read Order

1. Read this file for the project map and current expansion points.
2. Read `docs/system-map/product-map.md` for user-facing flows and module boundaries.
3. Read `README.md` for usage and operational notes.
4. Read `AGENTS.md` or the repository root guidance supplied by the active workspace for build, test, style, commit, and security rules.
5. Read `docs/dossiers/` for in-flight feature state before continuing another agent's work.

## Current Shape

`dedao-gui` is a Wails desktop client. Go owns the desktop shell, backend services, filesystem/export work, and auxiliary CLIs. Vue 3, Vite, TypeScript, Pinia, and Element Plus own the desktop UI. The book knowledge fork adds local `book_knowledge` packages plus private kbase and MCP access surfaces.

## Key Entry Points

- Desktop shell: `main.go`
- Wails-facing backend methods: `backend/`
- Domain logic: `backend/app/`
- Dedao API wrappers: `backend/services/`
- Request/download helpers: `backend/request/`, `backend/downloader/`
- Desktop UI: `frontend/src/`
- KBase HTTP server: `cmd/kbase-server/`
- Book MCP server: `cmd/book-mcp/`
- In-flight feature dossiers: `docs/dossiers/`
- Existing implementation plans: `docs/plans/`

## Expansion Rules

- Prefer extending existing Go app/service helpers and Vue views before adding new surfaces.
- Do not hand-edit generated Wails bindings under `frontend/wailsjs/`.
- Do not commit generated runtime outputs such as `frontend/dist`, `build/bin`, local `config.json`, or `.DS_Store`.
- Keep private kbase APIs protected by Bearer token; `/health` is the only intentional unauthenticated probe.
- Keep downloaded or extracted Dedao content personal-use only.

## Drift Policy

This first system-map version is narrative only and contains no live architecture counts. If a future update needs counts or rosters that can drift, add a generator under `scripts/`, write generated output to `docs/_generated/system-map.json`, and reference that generated file instead of hand-writing the numbers here.
