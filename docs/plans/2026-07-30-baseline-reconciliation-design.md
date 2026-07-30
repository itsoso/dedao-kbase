# KBase Baseline Reconciliation Design

**Status:** Approved

**Decision:** Reconcile the legacy working-tree patch against
`dedao-kbase/main`, retain only behavior that the current product baseline does
not already provide, and keep the original working tree untouched.

## Context

The legacy checkout is based on an older KBase snapshot and contains
uncommitted backend, Web UI, dependency, and packaging changes. The active
product history has continued on `dedao-kbase/main`, including the first-party
WeChat collector, hardened source-agent control plane, richer knowledge
workflows, and release evidence.

Applying the entire legacy patch would regress newer product behavior and
reintroduce obsolete dependency and packaging changes. The safe unit of
migration is therefore an independently verified behavior, not a file.

## Goals

- Preserve the legacy patch before reconciliation.
- Use the latest `dedao-kbase/main` commit as the sole product baseline.
- Compare legacy changes semantically and migrate only missing behavior.
- Preserve intentional Go 1.21 compatibility and tracked Wails build assets.
- Verify the reconciled branch with repository system-map, test, build, smoke,
  and privacy gates.
- Record remaining operational blockers without weakening security boundaries.

## Non-Goals

- Merging or resetting the legacy working tree.
- Replaying deleted build assets, broad dependency upgrades, or lockfile churn.
- Replacing the first-party WeChat collector with WC Plus.
- Bypassing WeChat authentication, WC Plus licensing, macOS host security, or
  other upstream controls.
- Deploying or enabling scheduled collection.

## Reconciliation Rules

1. A legacy change is retained only when a focused test demonstrates a behavior
   missing from the latest baseline.
2. When the latest baseline has an equivalent or stronger implementation, the
   legacy change is rejected.
3. Dependency and generated-file changes require an explicit product need;
   incidental churn is rejected.
4. Security fixes are ported as small, current-baseline-native changes with a
   failing regression test first.
5. Existing release dossiers remain the authority for deployment and online
   validation state.

## Findings

The latest baseline already supersedes the legacy Web navigation, book-chat
route, static cache handling, WC Plus UI, and first-party WeChat acquisition
work. It also contains newer browser-session, evidence, package, and release
workflows that the legacy patch does not understand.

One legacy behavior is still valuable: a missing book requested through
`POST /api/book-chat` must not expose a local `manifest.json` path. The current
baseline sanitizes missing-book errors for the ordinary book endpoint, but the
book-chat handler forwards unexpected storage errors as an HTTP response.

## Chosen Change

Add a focused HTTP regression test for missing-book chat and map a missing
package to a stable `404 book not found: <sanitized-id>` response. Keep the
existing request validation and internal-error behavior unchanged.

## Verification

- Observe the focused regression test fail before changing production code.
- Make the smallest handler change that passes the regression test.
- Run focused backend tests, then the full Go suite.
- Run the Vue type-check/build and repository Web smoke scripts.
- Run system-map drift, packaging, privacy, and diff checks.
- Do not claim G6 completion: first-party WeChat collection still requires
  explicit local enrollment and an operator-selected account.
