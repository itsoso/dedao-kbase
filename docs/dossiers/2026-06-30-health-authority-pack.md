---
slug: 2026-06-30-health-authority-pack
status: implemented
owner: codex
---

# Health Authority Pack

## Request

Serve `health-llm-driven` with a governed Dedao book-knowledge review pack that can be dry-run before entering Health System KB review.

## Scope

- Add `health_authority_pack_v1` generation in dedao-kbase.
- Expose Bearer-protected health-only HTTP endpoints.
- Show the pack in the Web KBase project panel.
- Add a health-side dry-run importer that writes nothing.

## Gates

| Gate | Status | Evidence |
|---|---|---|
| G1 Intake | PASS | User selected execution option 1 for the Health Authority Pack plan. |
| G2 Feasibility | PASS | Reused existing health project verification collection and Web Project hub. |
| G3 Tests | PASS | `go test ./backend/app -run 'TestBuildHealthAuthorityPack|TestKBaseHTTPHandler' -count=1`; `node frontend-web/scripts/web-kbase-ui-smoke.mjs`; `npm --prefix frontend-web run build`; health dry-run pytest. |
| G4 Review | PASS | Dedao-only claims never return `action_support_candidate`; health importer blocks medical action claims. |
| G5 Deploy Health | PASS | Built Linux `kbase-server`, synced `frontend-web/dist`, `nginx -t` passed, `dedao-kbase.service` active. |
| G6 Online Verify | PASS | `https://kbase.executor.life/health` 200; unauth authority-pack 401; local Bearer refresh 200 with `health_authority_pack_v1`; JSONL export returned `application/x-ndjson`; deployed JS contains `health-authority-pack`. |

## Boundaries

Health Authority Pack is not runtime medical authority. It preserves source references and supports review workflows only. health-llm-driven remains responsible for final System KB admission and clinical safety gates.
