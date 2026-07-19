# Knowledge Supply Contract v1

KBase is the authoring and release plane. Consumers import only published
knowledge releases, run their own domain review, and serve from their own
reviewed indexes.

## Agent Packages

An Agent Package binds immutable published knowledge releases to retrieval,
model, prompt-profile, tool, safety, evaluation, and shared-UI policies. The
manifest follows `contracts/agent-package-v1.schema.json` and uses
`schema_version: agent-package.v1`.

Every release reference must include the published `release_id`, its pinned
`content_hash`, and resolvable `citation_ids`. Package validation rejects
unpublished or changed releases, missing citations, unknown MCP tools, missing
abstention rules, invalid evaluation thresholds, and undeclared UI
capabilities. Package content hashes include identity, release pins, and policy
content, but exclude mutable lifecycle timestamps so the same artifact can be
replayed after publication.

Package manifests contain prompt profile and output-schema identifiers, not
private prompt bodies. They do not transfer downloaded source bodies,
credentials, consumer user data, or consumer-owned review decisions.

## Pull Feed

Use:

```text
GET /api/knowledge/feed?after={cursor}&limit={n}&source={kind}&policy={policy}&book_id={id}
```

The response uses `contracts/knowledge-feed-v1.schema.json`. Advance the
consumer cursor only after the import transaction succeeds. Each feed item
contains a stable `url` for the immutable release detail:

```text
GET /api/knowledge/releases/{release_id}
```

## Delivery Receipt

After a successful or held import, post an idempotent receipt:

```text
POST /api/knowledge/releases/{release_id}/receipts
```

Payloads follow `contracts/delivery-receipt-v1.schema.json`. The
`idempotency_key` must be stable for one consumer import attempt. Replays return
the original receipt; a different payload for the same key returns conflict.
Receipts must not include user records, raw prompts, or private query text.

## Lineage

Use lineage to debug what a release came from:

```text
GET /api/knowledge/lineage/{release_id}
GET /api/knowledge/lineage/{book_id}
```

Lineage returns relative artifact references, source identifiers, content hash,
usage policy, and citation IDs. It does not return source bodies.

## Impact And Gaps

Use:

```text
GET /api/knowledge/impact
GET /api/knowledge/gaps
```

Impact aggregates release count, receipt dispositions, and pipeline stages.
Gaps are fingerprinted aggregates only. Consumers should submit or sync gap
fingerprints rather than raw user queries.

## Health Evidence Consumer

Health systems should use the evidence-only consumer surface instead of
importing generic releases directly:

```text
GET /api/consumers/health/releases?after={cursor}&limit={n}
GET /api/consumers/health/readiness?limit={n}
POST /api/consumers/health/readiness/analyze
GET /api/consumers/health/evidence/{release_id}
GET /api/consumers/health/search?q={query}&tag={tag}&limit={n}
```

Evidence packages follow `contracts/health-evidence-v1.schema.json`. They
include release identity, source provenance, freshness, claim-level tags,
citations, and safety flags. They are inputs for domain review only; diagnosis,
personalization, and user-facing medical actions remain owned by the health
consumer.

Use readiness when the Health feed is empty or stale. It returns each knowledge
package state as `published`, `ready_to_publish`, `needs_analysis`,
`needs_quality`, `policy_blocked`, or `quality_blocked`, with a bounded
`next_action` such as `analyze`, `evaluate_quality`, `review_policy`, or
`publish`.

Use `POST /api/consumers/health/readiness/analyze` as an explicit operator
action for bounded backfill. The request accepts `limit`, `model`,
`max_context_chars`, `dry_run`, and `summary_only`; the server analyzes only
`needs_analysis` packages, writes the analysis manifest, evaluates quality
immediately, and returns per-book status. It does not publish releases
automatically. When `dry_run` is `true`, the response previews the same
candidate set without calling the model, writing analysis manifests, or changing
quality state. When `summary_only` is `true`, the response returns queue
statistics only, forces dry-run behavior, and omits candidate items. The
batch response includes `dry_run`, `eligible`, `skipped`, `skipped_by_status`,
`scanned`, `has_work`, `queue_state`, `recommended_action`,
`ready_to_publish`, `published`, `blocked`,
`requested_limit`, `next_batch_size`, `remaining_after_next_batch`,
`has_more_after_next_batch`, `estimated_batches`, and `limit_reached`
so operators can distinguish an empty queue from a limited preview, estimate
how many batches remain, or identify a queue blocked in another readiness state.
`queue_state` is `ready`, `complete`, `blocked`, or `empty`; `complete`
means all scanned items are already published or ready to publish.
`recommended_action` is `run_analysis`, `review_blocked`, or `idle`.

## Local Contract Smoke

Run:

```bash
bash scripts/knowledge-contract-smoke.sh
```

This executes the contract, feed, Health evidence, receipt, lineage, impact,
system-map, privacy, and whitespace checks without contacting production
services.
