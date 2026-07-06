# Repository Guidelines

## Project Structure & Module Organization

This is a Wails desktop app: Go owns the backend and desktop shell, while Vue 3/Vite owns the UI. `main.go` wires Wails, embeds `frontend/dist`, and binds `backend.App`. Backend code lives in `backend/`: app-facing methods in `app/`, API wrappers in `services/`, helpers in `utils/`, request/download code in `request/` and `downloader/`, and config in `config/`. Extra CLIs are under `cmd/kbase-server` and `cmd/book-mcp`. Frontend source is under `frontend/src/`: `views/`, `components/`, `stores/`, `router/`, `utils/`, and `assets/`. Generated Wails bindings live in `frontend/wailsjs/`; avoid hand-editing them.

## Build, Test, and Development Commands

- `go install github.com/wailsapp/wails/v2/cmd/wails@latest`: install the Wails CLI.
- `cd frontend && npm install`: install frontend dependencies.
- `wails dev`: run the desktop app with the Vite dev server.
- `wails build --clean`: build the app and embedded frontend.
- `./scripts/build-macos.sh`, `./scripts/build-macos-arm.sh`, `./scripts/build-windows.sh`: platform builds.
- `go test ./...`: run Go unit tests.
- `cd frontend && npm run build`: run `vue-tsc --noEmit` and Vite build.
- `node frontend/scripts/markdown-render-smoke.mjs` and `node frontend/scripts/book-knowledge-ui-smoke.mjs`: frontend smoke checks.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use exported names only for Wails/API surfaces. Keep Go tests named `TestXxx` in `*_test.go`. Vue components use PascalCase filenames, and Pinia stores stay in `frontend/src/stores/`. Match nearby TypeScript style; there is no repo-wide ESLint/Prettier config. Do not commit generated outputs such as `frontend/dist`, `build/bin`, local `config.json`, or `.DS_Store`.

## Testing Guidelines

Add or update Go tests beside changed backend packages. For frontend behavior without a formal test runner, extend the existing smoke scripts or add a small script under `frontend/scripts/`. Run narrow tests first, then `go test ./...` and `cd frontend && npm run build` before release-level changes. Do not pipe test output through plain `tail`; preserve the real exit code.

## Commit & Pull Request Guidelines

Recent history mixes imperative subjects (`Add book prompt studio`) with scoped conventional commits (`feat(kbase): add private HTTP server`) and older emoji commits. Prefer short imperative subjects; use `feat(scope):`, `fix(scope):`, or `docs(scope):` when clear. PRs should include the user-visible change, backend/frontend impact, exact tests run, linked issue if any, and screenshots for UI changes.

## Security & Configuration Tips

Keep private tokens and cookies out of git. `KBASE_AUTH_TOKEN` is required for `/api/*` routes in `cmd/kbase-server`; `/health` is intentionally unauthenticated for probes. Treat downloaded course/book content as personal-use data and avoid adding samples with copyrighted material.

## Privacy Guard

Use `.claude/skills/privacy-guard/SKILL.md` whenever changing configuration defaults, local paths, prompts, docs, export paths, or GitHub publishing surfaces. Do not commit machine-specific absolute paths, private project names, tokens/cookies, downloaded book samples, or macOS metadata. Before commit/push/PR, run `bash scripts/privacy-smoke.sh` and `git diff --check`; fix any red result instead of bypassing it.
