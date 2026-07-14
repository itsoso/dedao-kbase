# Knowledge Reverification Console Design

**Status:** Approved for implementation

## Problem

Automatic reverification now creates durable candidates, but operators cannot
inspect or act on them in the Web knowledge workbench. API-only visibility
leaves failed tasks, quality decisions, and publish readiness disconnected from
the book being reviewed.

## Decision

Add a compact reverification console inside the existing two-column
`/book-knowledge/{book_id}` page. The default book view remains focused on
reading and analysis. A status band summarizes the latest immutable release and
reverification state; an explicit details action expands the review surface in
the main column.

The expanded surface shows:

- latest release identity and creation time;
- invalidating outcomes and task attempts;
- release and candidate content/analysis hash prefixes;
- whether source content changed;
- current quality decision, usage policy, and each quality rule;
- retry for a failed current task;
- explicit publication for a passing `candidate_ready` task.

There is no automatic publication. Publishing retains the existing confirmation
and backend publication gate.

## API And State

The browser composes authenticated APIs for releases, release detail, feedback
assessment, task state, quality, and publication. The release collection accepts
`book_id` so a selected book cannot disappear behind the global 200-record
limit. One new action endpoint is required:

`POST /api/knowledge/releases/{release_id}/reverification/retry`

Retry is valid only for the latest failed task whose feedback fingerprint still
matches the release assessment. It resets the bounded attempt window, clears
candidate/error fields, and queues the task immediately under the existing OS
advisory lock. Active, ready, published, missing, or superseded tasks return
conflict and remain unchanged.

The Web state is reset when the selected book changes. Queued and running tasks
poll every five seconds; polling stops in terminal states or after navigation.
The expanded state is represented by `?review=1` for shareable operator links.

## Failure And Safety Boundaries

- Missing release or quality data renders a bounded empty/error state.
- Retry and publish buttons are disabled while an operation is running.
- Publication requires browser confirmation and backend validation.
- Raw model/filesystem errors are never exposed; task `error_code` remains the
  public diagnostic contract.
- The console never mutates immutable release files or consumer feedback.

## Verification

Add Go tests for retry state transitions, fingerprint protection, method and
conflict responses. Extend the structural Web smoke test for the composed APIs,
state reset, polling, confirmation, and control visibility. Run full Go, race,
frontend, privacy, and diff gates before deployment.
