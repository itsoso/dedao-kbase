# Knowledge Package Detail-First Delivery Dossier

## Status

Released to production on 2026-07-23.

## Requirement

The package index placed global review and pipeline sections before the selected
package. Opening an item required scrolling to the bottom before its chapters,
review state, baseline analysis, and model workspace became visible.

## Definition

- `/knowledge/packages` remains the global review and pipeline index.
- `/knowledge/packages/:bookID` becomes a detail-first package workspace.
- Desktop keeps a compact package list beside the selected package.
- Mobile renders the selected package before the searchable package list.
- Direct URLs, browser history, previous, and next package navigation remain
  route-driven.

## Gate Decisions

### G1 - Admission

PASS. The problem was reproducible from the production screenshot and mapped to
the shared web renderer without requiring a backend contract change.

### G2 - Feasibility And Risk

PASS. Existing package, review, baseline analysis, and TokenPlan state could be
reused. The change was isolated to route-aware rendering, responsive CSS, and
smoke coverage.

### G3 - Tests

PASS.

- All `frontend-web/scripts/*smoke*.mjs`
- `node --check frontend-web/app.js`
- `npm --prefix frontend run build`
- `go test ./... -count=1`
- `go vet ./...`
- `go mod verify`
- `bash scripts/privacy-smoke.sh`
- `git diff --check`
- Playwright desktop and 390 px mobile route, overflow, and click-navigation
  checks

### G4 - Review

PASS. Visual review removed a duplicate global-return action and changed mobile
ordering so the selected package appears before the package list.

### G5 - Deployment Health

PASS.

- Release commit: `84fedb5`
- Archive SHA-256:
  `f1fc154697f571ceda9ead846bd8f20a1c6b08c954ee07f42769140f5bed3d4b`
- Production frontend backup:
  `/opt/dedao-kbase/frontend-web.backup-84fedb5-20260723160545`
- Service health: `{"ok":true,"service":"dedao-kbase"}`

### G6 - Online Verification

PASS.

- The public health endpoint returned successfully.
- Unauthenticated package and asset requests returned the expected Nginx Basic
  Auth `401`.
- The deployed protected route served the
  `20260723-package-detail-first` cache marker through the local service.
- The installed application script contains `knowledge-web--detail`.

## Outcome

Global operations and individual package work are now separate surfaces. A
selected package is visible immediately, while global review and automation
remain available from the index.
