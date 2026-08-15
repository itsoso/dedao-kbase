# Collection Research Materialization Design

**Date:** 2026-08-15

**Status:** Approved

## Problem

Published account collections are immutable, evaluated `agent-package.v3`
packages. Research Runs intentionally require an evaluated
`agent-package.v4` with an explicit `research_policy`. This prevents an
ordinary collection Agent from acquiring private Chatlog tools, but it also
means a collection cannot currently be combined with Chatlog or prior-run
evidence in one Research Run.

The system must add that capability without allowing v3 packages to execute
Research tools and without weakening the v4 package boundary.

## Decision

Materialize an immutable collection release as a canonical standard knowledge
release, then use the existing Research-enabled compiler to create the v4
package.

The materialization is a controlled projection, not a live collection view.
Its identity is derived from the source collection release content hash, and a
repeated request returns the same standard release. A later collection release
produces a different materialized release and must be compiled as a new Agent
package version.

The v3 collection package remains unchanged and cannot create or resume a
Research Run. The v4 package continues to pin only standard knowledge releases
and must pass the existing Research policy, trusted evaluation, publication,
tool-authorization, and runtime gates.

## Alternatives considered

### Extend v4 to pin collection releases directly

This would reuse the existing collection search implementation, but it would
broaden the public v4 schema and require a second fetch/citation path inside the
Research runtime. It also makes the v3/v4 collection boundary harder to audit.

### Introduce an `agent-package.v5`

This provides the cleanest type distinction, but adds a new public contract,
compiler branch, evaluator branch, UI branch, and compatibility matrix for one
source adapter. It is not justified while a standard knowledge-release
projection can preserve the existing v4 contract.

## Materialized release contract

The materializer accepts one published `knowledge_collection_release.v1` ID.
It fails closed unless:

- the release exists and its stored content hash matches its canonical value;
- its quality decision permits evidence-only use;
- every member still matches the pinned book content hash and source identity;
- every projected item has at least one citation inside the member allowlist;
- aggregate member, claim, citation, and quoted-character limits are satisfied.

The standard release uses a deterministic synthetic book ID and release
identity derived from the collection release content hash. Member-local IDs are
namespaced by a stable member fingerprint before projection so two articles
cannot collide on chapter, chunk, claim, or citation IDs.

Each cited member chunk becomes one grounded claim. The claim statement is the
bounded chunk text, and its citations are the namespaced citations that already
support that chunk. Source type, source account, source item key, publication
time, anchor, and note are preserved. Raw cookies, tokens, local paths, and
collection credentials are never copied.

A small materialization record stores only the source collection release ID,
source content hash, target standard release ID, target content hash, counts,
and creation time. It is used for idempotency, provenance, and operator
inspection.

## API and operator flow

Add an authenticated endpoint:

```text
POST /api/knowledge/collection-releases/{release_id}/materialize
```

The request body is an empty JSON object and the response contains:

```json
{
  "created": true,
  "materialization": {
    "source_collection_release_id": "collection-release-example",
    "target_release_id": "release-example",
    "member_count": 10,
    "claim_count": 100,
    "citation_count": 100
  },
  "release": {
    "release_id": "release-example",
    "content_hash": "sha256:example"
  }
}
```

The endpoint returns `created=false` for an identical replay. A conflicting or
changed source returns a typed conflict and does not overwrite either object.

After materialization, the existing compile request is used unchanged:

```json
{
  "schema_version": "agent-compilation-request.v1",
  "mode": "study",
  "primary_release_id": "release-example",
  "version": "1.0.0",
  "research_enabled": true
}
```

Evaluation and publication remain explicit existing steps. The materializer
does not publish an Agent package, start a Research Run, or grant tool access.

## Data flow

```text
published collection release
  -> validate immutable member pins and citation allowlists
  -> namespace and project cited chunks
  -> persist canonical standard knowledge release + provenance record
  -> existing compiler creates v4 draft
  -> existing trusted Research evaluation and publication gates
  -> Research Run combines knowledge, Chatlog, and/or prior runs
```

## Failure and recovery

Materialization is assembled in memory first. Files are written through the
store's atomic-write helpers, and the provenance manifest is updated only after
the target release is durable. A retry after interruption either completes the
same deterministic release or reports a typed conflict; it never creates a
second logical snapshot.

Missing members, changed hashes, unsupported source types, uncited chunks,
oversized projections, or failed quality checks are terminal validation errors.
Filesystem and lock failures remain retryable operational errors. Existing
collection and standard releases are immutable and are never deleted or
rewritten by this flow.

## Security and privacy

- v3 packages remain ineligible for Research Runs.
- v4 validation and Research tool policy are unchanged.
- The endpoint requires the existing authenticated KBase surface.
- Only already-published, allowlisted collection evidence is projected.
- No signing mechanism is introduced.
- No raw Chatlog data participates in materialization.
- Public API responses expose IDs, hashes, counts, and bounded metadata, not
  article bodies.

## Verification

Unit and contract tests must prove:

- deterministic replay yields the same target release;
- a changed member hash or source identity fails closed;
- duplicate member-local IDs remain distinct after namespacing;
- citations cannot escape the collection member allowlist;
- oversize and uncited projections are rejected without partial persistence;
- the public endpoint rejects missing auth, invalid paths, unknown JSON fields,
  and unsupported methods;
- the materialized release works with the unchanged Research compiler;
- the resulting published v4 package can search and fetch materialized
  collection evidence while a v3 package remains policy-denied.

Release verification must run the complete Go suite, Research process smoke,
system-map drift check, privacy smoke, and diff check. Online acceptance must
materialize a real private collection release, compile and publish a new v4
package version, then complete a cross-source Research Run whose actual scope
and cited scope both contain knowledge and Chatlog. Only IDs, hashes, counts,
timings, outcomes, and citation re-fetch status may be added to the dossier.
