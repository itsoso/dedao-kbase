# Knowledge Supply Contract v1

KBase is the authoring and release plane. Consumers import only published
knowledge releases, run their own domain review, and serve from their own
reviewed indexes.

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

## Local Contract Smoke

Run:

```bash
bash scripts/knowledge-contract-smoke.sh
```

This executes the contract, feed, Health evidence, receipt, lineage, impact,
system-map, privacy, and whitespace checks without contacting production
services.
