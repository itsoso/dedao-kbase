# Research Runtime Recovery Design

**Date:** 2026-08-15

**Status:** Approved

## Problem

Production deep-research acceptance exposed two operator-facing failure modes:

1. A structurally invalid planner, extractor, synthesizer, or verifier response
   terminates the run immediately even when one bounded regeneration could
   recover it safely.
2. Every terminal Worker job is projected as `worker_offline`, including jobs
   that were claimed and explicitly failed because the local dependency or
   returned source data was invalid.

The recovery path must not invent evidence, weaken reference validation, hide
cost, or create an unbounded retry loop.

## Decision

### One durable model-output repair

Each logical role invocation may make one primary call and at most one repair
call. Both calls use the existing model reservation path, consume a model-call
slot, reserve cost before the provider request, and retain their own durable
request identity.

The repair is permitted only after strict JSON decoding, schema validation, or
runtime-reference validation returns `invalid_model_output`. Provider errors,
timeouts, policy denials, lease loss, and exhausted budgets are not repairable.
The repair request reuses the original trusted messages and appends an
allowlisted validation category plus the role schema. It does not include raw
validation text, private locators, or a fallback answer. The model must
regenerate the complete JSON object from the original evidence context.

The invocation ledger records a bounded failure category. On coordinator
restart, an already failed primary invocation is not sent again; execution
continues directly to the single repair identity. A failed repair terminates
with a role-specific public code:

- `planner_invalid_output`
- `extractor_invalid_output`
- `synthesizer_invalid_output`
- `verifier_invalid_output`

The legacy `invalid_model_output` presentation remains readable for existing
runs.

### Accurate Worker outcomes

Worker job state and failure code determine the public result:

- an expired lease with no successful completion becomes `worker_offline`;
- a claimed job that reports a terminal failure becomes `worker_failed`;
- invalid persisted candidate or result boundaries also become
  `worker_failed` and remain fail-closed;
- queued or leased work remains `worker_pending` and is never presented as a
  terminal outcome.

The public UI explains that `worker_failed` means the Worker connected but its
local query or returned data failed. It directs the operator to inspect Agent
health and retry only after the local dependency is healthy.

### Bounded long-range Chatlog reads

The production failure was reproduced from privacy-safe timing and state
metadata: identity resolution and a short-range Chatlog search completed, while
a separate global search spanning one calendar year failed three times with
`dependency_unavailable`. Each attempt ended at the HTTP reader's ten-second
deadline even though the local Chatlog service later logged successful 200
responses slightly beyond that boundary.

The loopback Chatlog reader therefore uses the already approved thirty-second
maximum as its default request deadline. The Worker lease heartbeat continues
during the request, so a longer local query cannot silently extend a stale job.
The deadline remains fixed and bounded; it is not configurable from a research
question or Worker job. Redirect rejection, loopback-only networking, response
size limits, row limits, and result validation are unchanged.

## Data and compatibility

The model invocation table gains only bounded failure metadata needed for
durable routing. It does not persist raw invalid model output. Existing rows
without failure metadata retain current behavior. Existing terminal runs and
the `invalid_model_output` code remain readable.

No API accepts new arbitrary strings. Role-specific failure codes and Worker
failure categories are fixed allowlists. Auth, owner isolation, evidence
budgets, citation validation, Worker lease fencing, and package policy are
unchanged.

## Verification

Tests must prove:

1. invalid primary output triggers exactly one separately budgeted repair;
2. a valid repair resumes the stage and is cached durably;
3. invalid repair output terminates with the correct role-specific code;
4. coordinator restart after primary failure skips the primary provider call;
5. budget exhaustion prevents the repair call;
6. provider timeout and policy errors never trigger repair;
7. failed and expired Worker jobs map to different public outcomes;
8. the default Chatlog request deadline is thirty seconds while custom shorter
   test deadlines and the thirty-second maximum remain enforced;
9. the Chinese UI presents legacy and new failure codes correctly;
10. focused race, process smoke, privacy, generated-map, and diff checks pass;
11. a production deep run exercises knowledge plus bounded Chatlog retrieval
    and reaches either human identity confirmation or a verified report without
    reporting a connected Worker as offline.
