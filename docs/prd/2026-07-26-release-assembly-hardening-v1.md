# Release Assembly Hardening v1 PRD

**Status:** Approved

## Goal

Make `knowledge_release_assembly.v1` safe for Agent Compiler consumption by
enforcing bounded cluster payloads and deterministic cross-field invariants at
runtime.

## Problem

Release Assembly v1 bounds the number of returned clusters, but one cluster can
still contain unbounded claims, statements, citation references, or generated
conflicts. Its runtime validator checks required fields but does not prove that
summary counts, release references, publication counts, status, and conflict
edges agree with the claims in the payload.

## Requirements

- Reject a cluster with more than 128 claims.
- Reject statements or normalized assertions over 4,096 Unicode code points.
- Reject a claim with more than 128 citation IDs.
- Reject a cluster with more than 256 potential-conflict edges.
- Require every claim release ID to exist in `release_ids`.
- Require unique claim and citation identities in the visible projection.
- Recompute cluster ID, publication counts, status, and conflict edges.
- Enforce consistent summary, returned count, and `has_more` relationships.
- Keep the current schema version and read-only API.
- Preserve the legacy Release read adapter introduced during production rollout.

## Non-Goals

- No Agent Package generation in this slice.
- No semantic paraphrase clustering.
- No truncation or best-effort recovery.
- No mutation of Releases or stored analysis.
- No new database or external model dependency.

## Success

The builder rejects oversized source projections before generating an
unbounded response. The contract validator rejects a structurally valid JSON
payload when any derived relationship is forged or inconsistent. Existing
production Assembly output remains valid.

