# Book as Agent Platform Design

## Goal

Turn every authorized book or article collection into a stable source that can
progress into a versioned Agent Package, a callable agent, and an optional
standalone product. The product is generated from shared infrastructure; it is
not a fork of the application code.

The first validation sequence is a proof-oriented evidence consumer followed by
a read-only health evidence consumer. The first proves claim and citation
quality at lower risk. The second verifies that the same package contract can
operate behind stronger review and abstention gates.

## Architecture

The platform uses seven explicit layers:

1. **Source book**: `/sources/dedao/ebooks/{source_enid}` is the canonical page
   for title, author, introduction, progress, source identity, and ingestion
   actions. It never assumes a local package exists.
2. **Knowledge compiler** converts source versions into normalized chapters,
   chunks, claims, citations, entities, relationships, freshness data, and
   explicit conflicts. Every stage is cached by input and compiler version.
3. **Knowledge release** is immutable, content-addressed, reviewable, and
   delivered through the existing cursor feed and receipt protocol.
4. **Agent Package** binds one or more pinned releases to model, retrieval,
   prompt, tool, safety, evaluation, and UI policies.
5. **Agent runtime** executes a durable workflow against the pinned package. It
   owns sessions and traces but does not own source ingestion or publication.
6. **Tool gateway** exposes typed MCP tools. A deterministic policy layer returns
   `allow`, `require_confirmation`, or `block` before execution.
7. **Book app** is a manifest-driven product shell. It exposes reading, search,
   grounded conversation, study workflows, and domain-specific actions without
   copying package or runtime code.

The source identity and package identity are deliberately different. The
source page links forward only when a package or agent exists; otherwise it
offers download and package creation as jobs with visible status.

## Agent Package Contract

`agent-package.v1` contains:

- package identity, version, content hash, lifecycle state, and supersession;
- pinned release IDs and allowed source types;
- retrieval strategy, metadata filters, citation requirements, and context
  limits;
- model policy with preferred capability, fallbacks, cost limits, and timeout;
- prompt profiles and structured output schemas;
- MCP server references and per-tool authorization rules;
- safety policy, abstention reasons, and escalation target;
- evaluation suite version and minimum release scores;
- UI capabilities such as reader, chat, evidence table, quiz, and action plan.

Package publication fails when a referenced release is unpublished, a tool is
not present in the allowlist, a citation cannot resolve, or a required
evaluation is below threshold.

## Data Flow

`Authorized source -> immutable source version -> compiler -> quality review ->
knowledge release -> agent package -> runtime -> product/consumer -> privacy-safe
feedback -> gap or reverification queue`

Every transition is idempotent and preserves provenance. Agent responses cite
release claims and chunks rather than raw files. Consumer feedback contains
bounded fingerprints and outcomes, never raw user prompts or personal records.

## Ownership Boundaries

- **KBase owns:** collection, source identity, normalization, provenance,
  knowledge quality, immutable release, Agent Package publication, delivery,
  and feedback-driven reverification.
- **Agent runtime owns:** model routing, retrieval execution, session state,
  tool proposals, durable workflow state, traces, and response assembly.
- **Proof consumer owns:** multi-model comparison, claim verification, conflict
  resolution, user adjudication, and trust scoring.
- **Health consumer owns:** domain review, personal context, medical safety,
  serving indexes, final wording, escalation, and all user-facing actions.

KBase does not diagnose, prescribe, or automatically approve health evidence.
The runtime cannot authorize a tool through prompting alone.

## Retrieval Design

Begin with hybrid lexical and vector retrieval plus metadata filters and a
reranker. Resolve citations after reranking and before generation. Add graph
retrieval only for cases requiring cross-source entity relationships,
contradiction traversal, or whole-corpus synthesis. This keeps the first release
measurable and avoids unnecessary indexing cost.

## Evaluation And Observability

Each package carries a versioned golden set. Release gates measure retrieval
recall and precision, answer faithfulness, citation coverage and resolution,
abstention correctness, tool selection and argument correctness, task
completion, latency, and cost. Runtime traces record package/release versions,
retrieved evidence, model route, proposed and executed tools, policy decisions,
and evaluation results without recording secrets.

## Error Handling

Source metadata remains visible when downloading, conversion, or package lookup
fails. Actions report explicit job errors and can be retried. Missing packages
are normal product state, not reader errors.

## Initial Scope

The first package combines one general-interest book with a bounded set of
related articles. It supports search, grounded answers, evidence inspection,
and read-only tools. The proof consumer validates support, contradiction, and
unknown outcomes. Only after those gates pass does a separate evidence-only
package enter the health consumer's draft review path.

The first release explicitly excludes autonomous purchases, publishing,
diagnosis, prescriptions, medication changes, and personal-data writes.

## Verification

Contract tests validate manifests, content hashes, pinned releases, policy
decisions, cursor delivery, and idempotent receipts. Golden-set tests validate
retrieval and answer quality. Consumer tests validate import, hold, rejection,
feedback, and replay. Production verification must prove that one package can
be consumed by both pilots without sharing consumer state or bypassing either
consumer's review policy.
