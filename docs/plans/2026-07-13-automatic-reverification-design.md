# Automatic Knowledge Reverification Design

**Status:** Approved for implementation

## Problem

Consumer feedback can mark an immutable knowledge release as requiring
reverification, but an operator must currently notice the assessment and run
analysis manually. This leaves stale, rejected, or conflicting evidence visible
without a durable follow-up task.

## Decision

Add a durable, idempotent reverification queue owned by KBase. Invalidating
feedback (`stale`, `conflict`, or `rejected`) creates or updates one active task
for the affected release. A background runner processes due tasks asynchronously
and produces a candidate analysis and quality decision. It never publishes,
deletes, or mutates an existing release.

## State And Storage

Tasks are stored below the configured book-knowledge root, not in source files
or downloaded content. Each task contains opaque identifiers, trigger outcomes,
assessment timestamp, status, attempt count, due time, content hashes, quality
decision, an enumerated error code, and timestamps. Feedback writes, queue
mutations, and publication use the same short-lived OS advisory lock so
overlapping server processes cannot claim the same task or publish across an
invalidating feedback write. The operating system releases the lock on process
exit.

The state machine is:

`queued -> running -> candidate_ready -> published`

`running` may also transition to `queued` with bounded exponential backoff, or
to `failed` after the attempt ceiling.

New invalidating feedback while a task is queued or running is coalesced into
that task. New feedback after a terminal task creates a new task. A configurable
cooldown can defer the new task without dropping it. The runner recovers stale
`running` tasks after process restart.

## Processing

For each due task, the runner:

1. Loads the immutable release and current knowledge package.
2. Records whether source content changed since publication.
3. Runs the existing structured analysis generator for the current package.
4. Loads the resulting quality report and records its decision as a candidate.
5. Re-checks the feedback assessment; feedback received during processing
   requeues the task instead of falsely marking it current.

The package content hash is checked both before and after analysis. The task
records the candidate snapshot hash rather than claiming it remains the current
package forever. A changed snapshot or graceful cancellation requeues the task.
Automatic requeues use exponential backoff and a five-attempt ceiling. Raw
filesystem or model errors are never persisted in the public task record.

The existing explicit publish endpoint remains the only publication path. It
also requires the task matching the latest invalidating assessment to be
`candidate_ready` with an analysis hash matching the current quality report;
queued, running, failed, or superseded candidates are rejected. Successful
publication marks that assessment task `published`, so later source updates and
manual analyses are not permanently blocked.

## API And Operations

Authenticated endpoints expose aggregate task state only:

- `GET /api/knowledge/releases/{release_id}/reverification`
- The existing feedback POST response includes `reverification` when a task is
  created or coalesced.

Environment controls bound work without disabling durable enqueueing:

- `KBASE_REVERIFICATION_TICK_SECONDS` (default `30`, maximum `300`)
- `KBASE_REVERIFICATION_COOLDOWN_SECONDS` (default `300`)
- `KBASE_REVERIFICATION_STALE_SECONDS` (default `900`)

Failures remain visible and retryable through subsequent invalidating feedback;
there is no silent fallback or automatic publication.

## Acceptance Criteria

- Duplicate feedback does not create duplicate active tasks.
- Invalidating feedback returns before model analysis begins.
- Tasks survive restart and stale running tasks recover.
- Only one task per release runs at a time.
- Candidate quality and failure details are observable through authenticated API.
- Existing releases and publication behavior remain unchanged.
