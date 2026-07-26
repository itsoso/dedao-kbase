# Knowledge Foundation v2 PRD

**Status:** Approved for Phase 1 implementation  
**Last reviewed:** 2026-07-25

## Product Goal

Turn KBase into a verifiable knowledge supply system rather than a collection of
downloaders, generated summaries, and search indexes. Every knowledge output
must answer five questions:

1. What authorized source version did it come from?
2. Which bounded evidence supports each claim?
3. Which transformations and model versions produced it?
4. Which quality gates did it pass?
5. Which immutable release and Agent Package consumed it?

The first phase strengthens the shared foundation used by Dedao, WeChat,
Health, Proofroom, and Book Agents. It does not replace the existing release
protocol or consumer-owned domain review.

## Users And Jobs

- **Collector operator:** determine whether acquired content has a stable source
  identity and whether normalization produced usable evidence.
- **Knowledge reviewer:** inspect claim coverage, unresolved references,
  provenance warnings, and release blockers without opening raw storage files.
- **Agent builder:** pin releases whose claims and evidence can be resolved
  deterministically.
- **Consumer integrator:** import immutable releases while preserving evidence
  identities and lineage.
- **Platform operator:** see where knowledge is blocked and measure evidence
  coverage across the corpus.

## First-Principles Model

KBase treats knowledge as a compiled, versioned artifact:

```text
Source -> Document Version -> Evidence -> Claim -> Knowledge Release
       -> Agent Package -> Consumer Outcome -> Feedback -> Revalidation
```

- A **Source** identifies an authorized publication or account.
- A **Document Version** is an immutable content hash plus provenance.
- **Evidence** is a bounded, addressable chunk with a citation.
- A **Claim** is useful only when its evidence references resolve.
- A **Knowledge Release** is an immutable, quality-gated output.
- An **Agent Package** binds releases to model, retrieval, tool, safety, and
  evaluation policy.

## Phase 1 Requirements

### Evidence integrity

- Validate unique chapter, chunk, claim, and citation identities.
- Validate that every object belongs to the current book.
- Resolve analysis claim references to citations, chunks, chapters, or declared
  analysis sources.
- For explicit citations, validate the complete
  `claim -> citation -> chunk -> chapter -> book` chain.
- Treat direct chunk references as a visible compatibility mode, not as
  equivalent to a complete citation chain.
- Return stable blocker and warning codes; never expose raw source bodies.

### Source identity

- Derive a conservative canonical publication identity from source account,
  source URL host, or stable source metadata.
- Mark fallback identities as ineligible for independent-source counting.
- Never infer independence from two item IDs on the same publication.

### Quality and publication gates

- Add evidence-integrity rules to deterministic quality evaluation.
- Quarantine incomplete candidate analysis; reserve rejection for corrupt,
  stale, cross-book, or structurally unsafe state.
- Re-run evidence validation during publication so a stale or forged quality
  report cannot bypass the gate.
- Keep existing immutable releases readable and unchanged.

### Readiness observability

Expose an authenticated read-only readiness API with:

- per-book pipeline state and bounded blocker/warning codes;
- analysis claim count and claim evidence coverage;
- evidence reference count and resolution rate;
- explicit-citation and legacy direct-chunk counts;
- canonical publication identity and independence eligibility;
- current release identity when published;
- aggregate coverage and funnel totals.

## API Contract

```text
GET /api/knowledge/readiness?limit={1..500}&book_id={optional}
```

The response is `knowledge_readiness.v1`. It contains metadata and identities
only. It must not contain chunk text, prompts, model answers, tokens, cookies,
local absolute paths, or downloaded source bodies.

## Compatibility And Migration

- `knowledge_release.v1` remains the release schema in Phase 1.
- Existing direct chunk references remain resolvable but produce
  `legacy_direct_chunk_reference` warnings.
- New analysis generation will migrate to explicit citation IDs in a later
  phase after source adapters consistently emit citation records.
- Existing releases are never rewritten. A source or analysis change creates a
  new release that supersedes the previous one.

## Success Metrics

- Analysis claims with at least one resolvable evidence reference.
- Explicit claim-to-citation coverage.
- Citation-to-chunk resolution rate.
- Packages with independently countable publication identity.
- Candidates blocked by stable reason code rather than opaque failure text.
- Releases rejected at publish time because stored quality became stale.

## Safety And Non-Goals

- No automatic medical approval, diagnosis, treatment, or Proofroom verdict.
- No automatic publication from analysis completion.
- No redistribution of paid source bodies.
- No source-independence claim when publication identity is unknown.
- No immediate database or microservice migration.
- No mutation of previously published release artifacts.

## Roadmap

1. **Foundation:** evidence validation, source identity, readiness API, and
   publication re-check.
2. **Citation migration:** adapters emit explicit citation records and analysis
   prompts cite citation IDs by default.
3. **Release assembly:** cross-release claim clustering, contradiction
   detection, and independent-publication scoring.
4. **Agent compiler:** generate Agent Packages from validated release sets with
   retrieval, tools, safety, and evaluation gates.
5. **Consumer loop:** Health and Proofroom receipts, gaps, conflicts, and stale
   signals automatically schedule bounded revalidation.
