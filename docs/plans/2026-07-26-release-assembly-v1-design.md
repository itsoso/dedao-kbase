# Release Assembly v1 Design

## Context

KBase currently validates each book package, publishes immutable Knowledge
Releases, and later binds selected Releases into Agent Packages. Evidence Audit
can adjudicate claims inside a package, but there is no deterministic projection
that helps an operator or compiler choose a coherent cross-Release set.

Release Assembly fills that gap:

```text
Knowledge Releases -> deterministic assembly -> Agent Package candidate
                                            -> Evidence Audit candidate
```

It is a read-only compiler stage, not another source store.

## Approaches Considered

### A. Deterministic lexical assembly

Normalize exact assertions, detect explicit polarity changes, and count
canonical publications. This is reproducible, cheap, privacy-safe, and easy to
audit, but deliberately misses paraphrases.

### B. Model-only semantic assembly

Ask an LLM to cluster and judge every claim. This increases recall but makes the
assembly expensive, nondeterministic, and vulnerable to unsupported merges.

### C. Hybrid candidate generation and model adjudication

Use deterministic retrieval to propose candidates and Evidence Audit to
adjudicate them. This is the long-term design. V1 implements the deterministic
candidate layer from A and leaves adjudication to the existing audit boundary.

**Decision:** implement A as the stable foundation for C.

## Data Model

`KnowledgeReleaseAssembly` contains:

- `schema_version=knowledge_release_assembly.v1`;
- `algorithm_version=deterministic-claim-assembly.v1`;
- content-addressed `assembly_id`;
- sorted selected Release IDs;
- aggregate summary;
- bounded `clusters`;
- `returned_clusters` and `has_more`.

Each cluster contains a stable cluster ID, normalized assertion identity,
status, independent-publication count, publication count, claim references,
and optional potential-conflict pairs.

Claim references contain only:

- Release, book, and claim IDs;
- the claim statement and polarity;
- bounded citation IDs;
- canonical publication identity and eligibility.

They never contain citation notes, source URLs, source account values, source
bodies, prompts, or model output.

## Snapshot Algorithm

1. Load the Release manifest.
2. Select the newest record for each book by parsed `created_at`, with
   Release ID as the deterministic tie-breaker.
3. Load and validate every selected immutable Release.
4. Sort selected Releases by Release ID.
5. Build all clusters before applying query or result limits.
6. Hash algorithm version, selected Release IDs, and canonical cluster content
   to derive the assembly ID.

An invalid selected Release fails the complete projection. Partial truth is
more dangerous than an explicit unavailable state.

## Claim Identity And Polarity

The normalizer lowercases text, replaces punctuation and whitespace with one
space, and preserves letters and numbers from all scripts.

Polarity detection removes exactly one explicit negative marker from a
normalized assertion. Supported markers are a small versioned set covering
common Chinese and English negations. Claims with the same remaining assertion
key join one cluster:

- identical polarity and statement: corroboration candidate;
- positive and negative polarity: potential conflict;
- merely similar wording: separate clusters.

The output label is `potential_conflict`; only Evidence Audit may produce a
`contradicted` verdict.

## Publication Identity

Canonical account and author identities are transport-independent. The same
publisher delivered through a course article and a public-account article is
one publication. Host identities remain host-based. Item/book fallbacks remain
ineligible for independent-source scoring.

Cluster status is deterministic:

- `potential_conflict`: both polarities exist;
- `corroborated`: at least two eligible publication identities;
- `single_publication`: one eligible identity;
- `insufficient_identity`: no eligible identity.

## API And Errors

`GET /api/knowledge/assembly` reuses bearer authentication and JSON error
helpers. `limit` is `1..500`; `query` is bounded and only filters the returned
cluster view. Assembly identity and aggregate summary always describe the full
latest snapshot.

Unsupported methods return `405`. Invalid bounds return `400`. Corrupt or
missing selected Releases return an explicit server error; there is no silent
fallback to older content.

## Testing

- Pure normalizer and polarity tests.
- Latest-per-book snapshot and deterministic ID tests.
- Cross-transport same-publication tests.
- Corroboration and potential-conflict tests.
- No broad paraphrase merge test.
- Contract and privacy serialization tests.
- Auth, method, limit, query, and bounded-output HTTP tests.
- Full Go, race, vet, system map, contract, privacy, and frontend smoke Gates.

