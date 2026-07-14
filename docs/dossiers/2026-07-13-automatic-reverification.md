# Automatic Knowledge Reverification Dossier

**Status:** Shipped

## Requirement

Continue the feedback-driven knowledge loop with automatic, durable
reverification work after invalidating consumer feedback.

## User And Value

Knowledge consumers need authoritative releases to improve from observed stale,
conflicting, or rejected evidence without relying on an operator to monitor every
feedback event. Existing releases must remain stable until a replacement passes
quality gates and is explicitly published.

## Scope

- Durable and idempotent reverification tasks.
- Asynchronous candidate analysis and quality evaluation.
- Authenticated aggregate task visibility.
- Restart recovery, coalescing, cooldown, and bounded failures.

Out of scope: automatic publication, release deletion, consumer identity or free
text storage, and synchronous model work in the feedback request.

## Artifacts

- Design: `docs/plans/2026-07-13-automatic-reverification-design.md`
- Plan: `docs/plans/2026-07-13-automatic-reverification.md`

## Gates

- **G1 Admission:** PASS. This closes the approved `feedback_received ->
  re-evaluated` loop with a bounded end-to-end slice.
- **G2 Feasibility and risk:** PASS. Existing analysis and quality components are
  reusable. Hard boundaries are durable enqueue, one active task per release,
  no synchronous model invocation, no automatic publication, and no raw consumer
  data in task responses.
- **G3 Test:** PASS. Verified with `go test ./...`, `go vet ./...`,
  `go test -race ./backend/app ./cmd/kbase-server`, `cd frontend && npm run
  build`, a Windows amd64 compile-only check with CGO disabled,
  `bash scripts/privacy-smoke.sh`, and `git diff --check`. The frontend retains
  its existing large-chunk and dependency `eval` warnings; neither failed the
  production build.
- **G4 Review:** PASS AFTER FIXES. Independent review found two High
  issues (superseded candidate publication and cross-process duplicate claims)
  plus three Medium issues (cancellation handling, inconsistent content
  snapshot, and raw internal error exposure). Fixes add an owner-checked
  filesystem lock, publication gate, cancellation/content requeue, and public
  error codes. First re-review found three remaining High race/lifecycle issues
  and two Medium snapshot/retry issues. The second fix replaces custom stale
  lock removal with an OS advisory lock, serializes feedback and publication,
  records successful resolution as `published`, defines candidate snapshot
  semantics, and adds exponential backoff plus a five-attempt ceiling. Final
  review then identified two Medium issues: downstream timeouts bypassing the
  retry ceiling and timestamp-only invalidating-feedback identity. The final
  fix applies bounded backoff to downstream timeouts and uses a deterministic
  digest of invalidating feedback IDs for enqueue, completion, and publication
  gates. Independent re-review reported no remaining Critical, High, or Medium
  findings.
- **G5 Deployment health:** PASS. PR #3 merged as `2d85190`. Linux CGO tests
  and an isolated service-user preflight passed before replacement. The first
  module download through `proxy.golang.org` timed out; the successful retry
  used an HTTPS Go module mirror while retaining `go.sum` verification. The
  deployed and running binary SHA-256 is
  `83507b5b6b496decc4dbeeef9794b6121b15a73444e8fdf949f148b223b97f24`.
  Backup: `/opt/dedao-kbase/bin/kbase-server.before-20260714155715`.
- **G6 Online verification:** PASS. Public `/health` returned 200 before and
  after deployment. The unauthenticated reverification endpoint returned 401;
  an authenticated request for a real release returned 200 with the expected
  release ID and an empty task list. After a complete worker scheduling window,
  systemd remained active and logs contained no reverification failure, panic,
  or fatal event. No synthetic invalidating feedback was written to production
  knowledge solely for verification.

## Current Stage

S8 shipped and verified in production.
