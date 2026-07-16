# Knowledge Supply Chain Redesign Dossier

**Status:** Complete

## Requirement

Redesign KBase from a collection of imported Dedao and WeChat content into a
knowledge supply chain: search and download sources, normalize and package
knowledge, evaluate quality, expose consumer-ready feeds, and show operator
status in the control plane.

## Artifacts

- Design: `docs/plans/2026-07-16-knowledge-supply-chain-redesign-design.md`
- Plan: `docs/plans/2026-07-16-knowledge-supply-chain-redesign.md`

## Gates

- **G1 Admission:** PASS. Health and Proofroom need a stable knowledge source
  contract rather than page-specific scraping or manual exports.
- **G2 Feasibility and risk:** PASS. The implementation keeps the existing
  single KBase service and local-agent model, adds typed source envelopes,
  rebuildable indexes, release impact planning, and read-only consumer feeds.
- **G3 Test:** PASS. `go test ./...`, `cd frontend && npm run build`,
  `node frontend-web/scripts/book-knowledge-web-smoke.mjs`,
  `node frontend-web/scripts/wcplus-source-ui-smoke.mjs`,
  `bash scripts/knowledge-eval-smoke.sh`, `bash scripts/system-map-smoke.sh`,
  `bash scripts/privacy-smoke.sh`, and `git diff --check` passed.
- **G4 Review:** PASS. Source normalization preserves provenance and license
  scope. Health consumer feeds force `evidence_only` profile data. Rebuild
  planning is advisory and does not mutate releases. Search indexes are
  rebuildable from source packages.
- **G5 Deployment health:** PASS. `dedao-kbase/main` was fast-forwarded to
  `8148eed8089d85d15510cbb7b61982112dd198aa`. The server-built Linux binary
  SHA-256 is
  `272572e9e0d5b87290ff36ed3cbb4eb42433c8477ce290003217cbd91a809391`.
  Production backups were created at
  `/opt/dedao-kbase/bin/kbase-server.before-20260716225547` and
  `/opt/dedao-kbase/frontend-web.before-20260716225547`. The systemd unit
  `dedao-kbase` returned to `active`.
- **G6 Online verification:** PASS. Public `/health` returned 200. Authenticated
  loopback checks returned 200 for `/api/knowledge/feed`,
  `/api/knowledge/impact`, and `/api/consumers/health/releases?limit=1`.
  The deployed `app.js` contains the new `供应链状态` control-plane section.
  Post-deployment logs since restart contain no panic, fatal, error, or failed
  entries.

## Current Stage

S6 deployed and verified online.
