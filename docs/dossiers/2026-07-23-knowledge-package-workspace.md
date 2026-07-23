# Knowledge Package Workspace Delivery Dossier

## Status

Ready for production deployment on 2026-07-23.

## Requirement

Turn each knowledge package detail route into a focused workspace that shows
how source content becomes analyzed, quality-controlled, published knowledge
and finally a runnable Agent product.

## Definition

- Keep the selected package and its lifecycle visible before the directory.
- Provide stable overview, quality, evidence, analysis, and Agent anchors.
- Allow the directory to collapse so the main workspace can use the full width.
- Report Agent readiness only when a published Agent Package pins the current
  immutable knowledge Release.
- Preserve detail-first ordering and usable actions on mobile.

## Gate Decisions

### G1 - Admission

PASS. This continues the approved detail-first package redesign and directly
supports the product goal of turning each book or source into an Agent-ready
knowledge product.

### G2 - Feasibility And Risk

PASS. Existing knowledge package, release, review, analysis, and Agent Package
contracts are reused. Agent list records are resolved to versioned details
before release bindings are evaluated, preventing inferred or stale status.

### G3 - Tests

PASS.

- All `frontend-web/scripts/*smoke*.mjs`
- `node --check frontend-web/app.js`
- `npm --prefix frontend run build`
- `go test ./... -count=1`
- `go vet ./...`
- `go mod verify`
- `bash scripts/privacy-smoke.sh`
- `bash scripts/system-map-smoke.sh`
- `git diff --check`
- Playwright desktop and 390 px mobile lifecycle, navigation, directory,
  Agent-binding, and overflow checks

### G4 - Review

PASS. Review corrected a contract mismatch where Agent Package collection
records were assumed to contain Release references. The workspace now loads
published version details with bounded concurrency and exposes partial failures
instead of silently reporting a false state.

### G5 - Deployment Health

Pending deployment.

### G6 - Online Verification

Pending deployment.

## Outcome

The package route now communicates the complete knowledge supply path and gives
operators direct, contextual access to review, evidence, analysis, and the
exact versioned Agent that consumes the current Release.
