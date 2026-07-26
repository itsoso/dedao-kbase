# Release Assembly v1 PRD

**Status:** Approved for implementation
**Date:** 2026-07-26

## Goal

Add a deterministic layer between immutable Knowledge Releases and Agent
Packages. The layer must organize claims across the current release snapshot,
measure independent-publication support, and surface conservative conflict
candidates without treating model or lexical inference as an authoritative
verdict.

## User Value

- Knowledge operators can see whether a claim is single-source, corroborated,
  or needs conflict review.
- Agent builders can select a release set from an explicit assembly snapshot
  instead of manually comparing books.
- Health and review consumers can request bounded claim metadata without
  receiving paid source bodies, prompts, or model answers.
- Evidence Audit can consume deterministic conflict candidates for later
  model-assisted adjudication.

## Requirements

### Snapshot selection

- Use only the latest immutable Release for each book.
- Keep the exact selected Release IDs in the response.
- Derive a content-addressed assembly ID from the complete selected snapshot
  and deterministic algorithm version.
- Reject invalid releases instead of silently skipping them.

### Claim clustering

- Normalize case, punctuation, and whitespace deterministically.
- Group exact normalized assertions across releases.
- Group an assertion with its explicit negation only as a potential conflict
  candidate.
- Do not use embeddings or an LLM in v1.
- Do not merge broad paraphrases whose equivalence cannot be proven.

### Independent-publication scoring

- Reuse canonical publication identity.
- Count one publication once even when it appears through multiple transport
  types.
- Exclude fallback item and book identities from independent-source counts.
- Expose counts and status rather than an opaque confidence percentage.

### Output and API

Expose an authenticated, read-only projection:

```text
GET /api/knowledge/assembly?limit={1..500}&query={optional}
```

The response uses `knowledge_release_assembly.v1` and includes:

- assembly and algorithm identity;
- selected Release IDs;
- aggregate claim, cluster, corroboration, and conflict-candidate counts;
- bounded clusters with claim, citation, release, and publication references;
- pagination metadata for bounded cluster output.

## Safety Boundary

- A conflict candidate is not a contradiction verdict.
- No automatic diagnosis, treatment, publication, or consumer action.
- No source body, local path, source account value, prompt, answer, token, or
  cookie in the response.
- No mutation of Releases, Agent Packages, or completed Evidence Audits.
- Existing APIs and immutable artifacts remain readable.

## Success Criteria

- Rebuilding the same snapshot produces the same assembly ID and cluster order.
- Superseded Releases never double-count claims.
- Same-publication claims never inflate independent-source counts.
- Explicit positive/negative variants create a review candidate.
- Output validation and privacy tests fail closed.
