# Citation Migration v1 PRD

**Status:** Approved for implementation  
**Date:** 2026-07-26

## Goal

Make every newly generated structured analysis cite explicit package citation
IDs rather than chunk IDs. This turns the Phase 1 evidence graph from an
observability layer into the default knowledge-production path.

## Problem

KBase already stores citations, but the structured-analysis context labels
evidence as chunks. Models therefore return chunk IDs, which Phase 1 accepts as
a visible compatibility mode. The HTML extractor also creates one citation per
chapter, so a multi-chunk chapter does not have a complete explicit evidence
surface.

## Users

- Source operators need deterministic citation records for every normalized
  evidence chunk.
- Reviewers need new analysis to resolve through the complete
  claim -> citation -> chunk -> chapter -> book chain.
- Agent builders need releases that do not depend on legacy chunk-reference
  interpretation.

## Requirements

1. HTML extraction emits one deterministic citation per chunk.
2. Chapter-level draft claims cite every citation supporting that chapter.
3. Structured-analysis context labels evidence with citation IDs and retains
   the underlying chunk and chapter identity.
4. The structured-analysis prompt requires citation IDs and names the
   compatibility boundary explicitly.
5. New structured analysis fails closed if a claim, risk, or action references
   a chunk, chapter, claim, or unknown ID instead of a package citation ID.
6. The analysis prompt version advances so downstream hashes and releases can
   distinguish the citation-native behavior.
7. Existing packages and immutable releases are not rewritten automatically.

## Non-Goals

- No bulk rewrite of the current corpus.
- No change to `knowledge_release.v1`.
- No model call during migration.
- No automatic publication.
- No removal of Phase 1 legacy-read compatibility.

## Success Criteria

- New HTML packages have citation coverage for every chunk.
- A generated analysis prompt exposes explicit citation IDs.
- Citation-native model output reaches `ready` and passes quality evaluation.
- Chunk-ID model output fails before quality or publication.
- Phase 1 readiness reports new analysis with zero legacy direct references.

## Rollout

Ship the write path first. Existing packages remain readable. A later bounded
operator job may re-normalize or explicitly backfill selected packages after
preview and approval.
