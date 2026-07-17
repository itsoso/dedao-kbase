# Health Evidence Package Design

**Status:** Approved for implementation

## Decision

KBase should expose a health-specific evidence package instead of asking
the Health consumer to consume generic knowledge releases. The package remains
evidence-only: it provides claims, citations, provenance, risk metadata,
freshness signals, and import receipts, while the health system keeps diagnosis,
personalization, safety policy, and user-facing medical decisions.

## Scope

This iteration adds a stable Health consumer contract and minimal automation
around existing KBase releases. It does not add new source crawlers, automatic
publication, or direct writes into the Health consumer.

## Contract

`health_evidence.v1` is derived from immutable `KnowledgeRelease` records with
`usage_policy=evidence_only`. Each item includes:

- release identity, content hash, source URI, source type, source account, and
  publication freshness;
- evidence candidates generated from structured analysis claims;
- bounded tags for condition, intervention, metric, population, risk, and
  evidence level;
- citation IDs and cited source/chunk pointers;
- safety flags that mark medical, high-risk, stale, or low-citation claims.

## Data Flow

```text
Published evidence-only release
  -> health evidence mapper
  -> health feed/detail/search endpoints
  -> Health importer
  -> delivery receipt or feedback
  -> rebuild and quality impact cockpit
```

## Endpoints

- `GET /api/consumers/health/releases`: existing cursor feed, unchanged.
- `GET /api/consumers/health/evidence/{release_id}`: full health evidence
  package for one release.
- `GET /api/consumers/health/search?q=...&tag=...&limit=...`: evidence-only
  claim search over published Health packages.

## Error Handling

Non-evidence releases return `404` on Health evidence detail. Invalid methods
return `405`; invalid cursors or query parameters return `400`. Missing optional
analysis fields degrade to empty tags but do not hide citation or release
identity.

## Testing

Tests cover evidence-only filtering, claim-to-citation mapping, safety flags,
search by text and tag, authentication, method rejection, and contract fixture
validation. Existing knowledge release and frontend smoke checks remain release
gates.
