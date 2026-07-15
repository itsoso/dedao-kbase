# Knowledge Supply Platform PRD

**Status:** Approved direction

## Product Goal

Turn KBase from a collection of source and analysis tools into a reliable,
domain-neutral knowledge supply control plane. It must convert authorized source
content into versioned, reviewable evidence releases and deliver those releases
to downstream products without owning their user context, domain policy, or
final decisions.

The first product outcome is a dependable supply path for a health consumer.
Proof-oriented consumers should be able to adopt the same release protocol later
without inheriting health-specific rules.

## Users And Jobs

- **Source operator:** connect local authenticated sources, understand freshness,
  and recover failed collection runs.
- **Knowledge reviewer:** inspect provenance, candidate differences, quality
  rules, and publish or hold a release.
- **Consumer integrator:** import only immutable published releases, preserve
  lineage, and return privacy-safe delivery and usage feedback.
- **Platform operator:** see pipeline health, lag, failures, release adoption,
  zero-hit demand, and reverification age from one control plane.

## Ownership Boundary

KBase owns source identity, collection, immutable content versions,
normalization, source-grounded analysis, quality reports, release publication,
delivery records, and feedback-driven reverification.

Consumers own domain review, external evidence policy, user permissions,
retrieval, safety, final advice or actions, and serving indexes. Health must not
search raw KBase notes at runtime; it imports published releases into its
reviewed System KB.

## Product Requirements

### Unified source and content ledger

- Give each logical source and content version a stable identity independent of
  adapter-specific IDs.
- Preserve adapter provenance, acquisition time, content hash, supersession,
  license scope, and asset status.
- Detect exact duplicates automatically and expose probable duplicates for
  review without deleting source history.

### Observable knowledge compiler

- Project collection, normalization, analysis, quality, candidate, and release
  state into one inspectable pipeline model.
- Cache stages by input hash and version; repeated work must be idempotent.
- Expose bounded retries, terminal error codes, cancellation, and stale-run
  recovery without persisting secrets or raw model failures.

### Release and delivery registry

- Keep releases immutable and content-addressed.
- Provide a cursor-based incremental feed with source, domain, policy, and time
  filters.
- Record idempotent consumer receipts for imported, held, rejected, and failed
  deliveries.
- Preserve claim and citation identity through every consumer import.

### Closed-loop quality

- Aggregate `used`, `zero_hit`, `rejected`, `stale`, and `conflict` signals.
- Convert zero hits into a knowledge-gap queue and invalidating feedback into
  source refresh or reverification candidates.
- Never treat usage as proof of correctness and never auto-promote a candidate
  into a consumer's reviewed serving plane.

### Unified control plane

The Web product converges on five workspaces: Sources, Pipeline, Review,
Releases, and Impact. Existing source-specific diagnostics remain available but
do not define the primary navigation.

## Success Metrics

- Published claims with valid, resolvable citations.
- Release-to-consumer import latency and receipt success rate.
- Consumer retrieval acceptance and visible citation usage.
- Unsupported-answer and zero-hit rates.
- Time from stale/conflict feedback to a new reviewed release.
- Source freshness and pipeline terminal-failure age.
- Share of high-risk claims held for domain review rather than served directly.

## Safety And Non-Goals

- No personal health data, user prompts, answers, cookies, or reviewer identity
  enters KBase feedback payloads.
- No automatic health advice, diagnosis, prescription, or domain approval.
- No broad redistribution of paid source text; releases contain bounded,
  transformed evidence with provenance and license metadata.
- No immediate microservice split or replacement of immutable artifact storage.
- No corpus-growth target that can outrun review and evidence quality.

## Roadmap

1. **Foundation:** generated system map, canonical identities, schemas, and
   global pipeline visibility.
2. **Reliable delivery:** incremental release feed, idempotent receipts, lineage,
   and consumer contract tests.
3. **Health pilot:** measured import, claim review, serving adoption, and
   privacy-safe feedback closure.
4. **Automated prioritization:** knowledge-gap queue, freshness policy, and
   feedback-driven refresh/reverification.
5. **Additional consumers:** protocol adapters with independent domain policy.

