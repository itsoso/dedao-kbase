# Job Center And Recovery Design

**Status:** Approved for implementation

## Goal

Make background work visible and recoverable from one place. Users should not
need to know whether a task came from Dedao, WC Plus, knowledge review, or a
downstream delivery job before they can inspect status, understand failure, and
retry or navigate to the owning workspace.

## Product Model

The Web product gets one top-level **Jobs** workspace at `/jobs`. It aggregates
task sources incrementally:

1. WC Plus download and sync tasks.
2. Dedao download, shelf, and knowledge-build jobs.
3. Knowledge reverification, quality, and publication jobs.
4. Health/proofroom delivery and receipt jobs.

Each task row should expose:

- stable task id;
- source type and owning workspace;
- status and progress;
- created/updated time when available;
- human-readable error;
- next action: refresh, retry, open source, open output, or view logs.

## First Implementation

The first version uses existing `/api/wcplus/task/all` and does not add new
backend persistence. It introduces the route, UI shell, normalized task rows,
status badges, and navigation entry. Later versions can add `/api/jobs` as a
server-side aggregator when multiple backends are ready.

## Error Handling

Task failures must be visible by default. A failed row should show the raw
source error, a normalized status label, and an action back to the owning
workspace. Loading errors on `/jobs` must not blank the page; the page should
show the last known rows when possible.

## URL Contract

- `/jobs`: global job center.
- `/jobs/{jobId}`: reserved for future task detail.
- Existing source pages may continue showing local task panels, but they should
link to `/jobs` for global inspection.

## Testing

Smoke tests should assert that `renderJobCenter`, `loadJobCenter`, `ROUTES.jobs`,
`/jobs`, and `/api/wcplus/task/all` are present. Browser verification should
mock WC Plus tasks and confirm failed and running tasks render with status,
error, and source links.
