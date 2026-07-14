# Feedback-Driven Reverification Dossier

**Status:** Shipped

## Requirement

Complete the existing `feedback_received -> re-evaluated` producer loop without
allowing downstream feedback to mutate or auto-publish knowledge.

## Gates

- **G1 Admission:** PASS. The source-to-knowledge design already requires
  feedback-driven re-evaluation.
- **G2 Feasibility and risk:** PASS. The existing append-only feedback log is
  sufficient; no migration or new infrastructure is required. Automatic
  analysis and publication are explicitly out of scope.
- **G3 Test:** PASS. Focused red-green tests, `go test ./...`, `go vet ./...`,
  race tests for `backend/app` and `cmd/kbase-server`, the Vue production
  build, four frontend smoke checks, privacy smoke, and diff checks passed.
- **G4 Review:** PASS. The review confirmed the existing authentication
  boundary is preserved, GET exposes aggregate assessment only, releases stay
  immutable, and feedback cannot trigger analysis or publication. No Critical,
  High, or Medium findings remain.
- **G5 Deployment health:** PASS. PR #1 merged as `4a0654c`. The first static
  Linux build failed because `go-sqlite3` requires CGO; the deployment script
  restored the previous binary and public health returned 200. A CGO-enabled
  Linux build then passed an isolated service-user preflight and was deployed
  with running SHA-256
  `b7e8a0b51c7d2102f01ceae75e928c4fbd15c9550b76731226508a4e47374843`.
  Backup: `/opt/dedao-kbase/bin/kbase-server.before-20260714083248`.
- **G6 Online verification:** PASS. Public `/health` returned 200 and an
  unauthenticated assessment request returned 401. Authenticated GET for real
  release `release-43a7dbb5062e51e383597c1452dfe5b187a2ce8b78690915f18cb1bc8819bcbb`
  returned only the approved aggregate fields with `disposition=healthy`,
  `used=1`, and `reverify_required=false`.

## Scope

Add deterministic assessment and authenticated GET/POST visibility for one
release. Do not expose raw event IDs, consumer IDs, or free text. Do not alter
release availability, source content, analysis manifests, or quality reports.

## Implementation

- `20dae99`: approved design, plan, and Gate dossier.
- `4c679bc`: deterministic assessment plus authenticated GET/POST contract.

The frontend dependency audit reported 16 existing vulnerabilities (1 low,
7 moderate, 8 high). This backend-only slice does not modify dependencies;
dependency remediation remains a separate change to avoid unreviewed upgrades.

## Deployment Notes

The rollback rehearsal was exercised by the failed non-CGO artifact: systemd
rejected it with the explicit SQLite stub error, the previous binary was
restored, and health recovered before the corrected build was installed. The
final verification compared the running `/proc/<pid>/exe` hash, not only the
file on disk.
