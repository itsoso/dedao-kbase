# Knowledge Supply Platform Dossier

**Status:** Planned

## Requirement

Evolve KBase into a domain-neutral knowledge supply control plane that reliably
serves a reviewed health knowledge system first and supports additional
consumers through the same release protocol later.

## Artifacts

- PRD: `docs/prd/2026-07-14-knowledge-supply-platform.md`
- Design: `docs/plans/2026-07-14-knowledge-supply-platform-design.md`
- Implementation plan: `docs/plans/2026-07-14-knowledge-supply-platform.md`

## Scope

- Generated system map and architecture drift checks.
- Canonical source/content identities and rebuildable catalog.
- Unified pipeline projection and operator visibility.
- Incremental published-release feed, delivery receipts, and lineage.
- Health-first consumer contract fixtures and impact metrics.
- Privacy-safe gap and feedback automation.

Out of scope: consumer domain approval, reviewed serving indexes, personal data,
automatic publication, immediate microservice decomposition, and unrestricted
source-text redistribution.

## Gates

- **G1 Admission:** PASS. The vertical content loop works; reliability now
  depends on cross-source and cross-system contracts rather than more isolated
  UI features.
- **G2 Feasibility and risk:** PASS WITH BOUNDARIES. Existing source runs,
  immutable artifacts, release feedback, and reverification are reusable. The
  catalog must remain rebuildable and publication/domain review must remain
  explicit.
- **G3 Test:** PENDING implementation.
- **G4 Review:** PENDING implementation.
- **G5 Deployment health:** PENDING implementation.
- **G6 Online verification:** PENDING implementation and consumer coordination.

## Delivery Checkpoints

1. Foundation and system-map drift gate.
2. Catalog and pipeline projection.
3. Release feed, receipts, lineage, and contract tests.
4. Health pilot and impact dashboard.
5. Gap automation and second-consumer readiness review.

Each checkpoint is independently releasable and must complete G3-G6 before the
next checkpoint becomes production-critical.

