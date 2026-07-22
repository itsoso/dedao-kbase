# Clinical Trial Truth Agent Design

## Product Decision

Build `book-agent-clinical-trials-truth` as a clinical-trial evidence audit
product on the shared Agent Package runtime. Do not extend it as a generic book
chatbot and do not fork a standalone application for this book.

The current `1.1.0` package proves publication, pinned releases, grounded
search, model execution, citations, traces, and consumer delivery. It does not
prove domain usefulness: its release contains one WeChat article, five claims,
four citations, lexical retrieval, and only internal read-only package tools.
Its deterministic evaluation validates runtime contracts, not clinical-trial
truthfulness.

## User And Job

The primary user is an evidence analyst reviewing a clinical-trial claim. The
product accepts one of three starting points:

- an NCT identifier;
- a DOI or PMID;
- a plain-language claim about a clinical trial.

The product answers: what was registered, what was reported, where they differ,
which evidence supports each conclusion, what remains unknown, and what needs
human review. It is not a diagnostic, prescribing, treatment-selection, or
patient-specific decision system.

## Alternatives Considered

1. Add more prompts and articles to the generic chat UI. This is quick but
   cannot produce repeatable audits or measurable domain quality.
2. Build a task-oriented audit product on the existing runtime. This is the
   selected approach because it reuses package, release, policy, trace, and
   consumer contracts while adding only domain-specific data and workflow.
3. Build a multi-agent clinical-research platform immediately. This may become
   useful later, but it introduces orchestration complexity before a single
   auditable workflow has passed a domain golden set.

## Product Surface

The Agent page opens with three commands: **Audit NCT**, **Compare publication**,
and **Verify claim**. Submitting an input creates a durable audit run and returns
a stable URL. The result page contains:

1. conclusion and confidence;
2. trial identity and version timestamps;
3. registration, protocol, publication, and results timeline;
4. planned versus reported enrollment, arms, endpoints, analysis, and results;
5. discrepancy and selective-reporting findings;
6. supporting and contradicting evidence with resolvable citations;
7. missing evidence, limitations, abstentions, and human-review items;
8. Proofroom verification and downstream delivery status.

Generic grounded chat remains available as a secondary interaction over the
completed audit, not as the primary product.

## Architecture

The existing seven-layer Book Agent architecture remains authoritative. This
feature adds a domain workflow above it:

`Audit input -> identifier resolution -> source snapshots -> normalized trial
record -> deterministic comparisons -> grounded synthesis -> evidence audit ->
Proofroom review -> optional health review draft`

### Domain contracts

`clinical-trial-audit.v1` stores the immutable audit request, source snapshot
fingerprints, normalized trial fields, comparison findings, citations,
confidence, limitations, and status. `clinical-trial-audit-run.v1` stores the
durable lifecycle: `queued`, `collecting`, `comparing`, `reasoning`,
`awaiting_review`, `completed`, `failed`, or `abstained`.

The deterministic comparison layer owns field-level differences. The model may
explain those differences but may not invent or overwrite them. Every generated
conclusion must resolve to an audit finding or pinned release citation.

### Source adapters

The first version adds read-only adapters for:

- ClinicalTrials.gov API v2 study records and data timestamps;
- PubMed metadata and abstracts through NCBI E-utilities;
- versioned ICH E6(R3) and CONSORT 2025 rule releases.

Source responses are normalized into immutable snapshots with canonical IDs,
retrieved timestamps, upstream version timestamps, hashes, and license scope.
Full copyrighted papers are not copied unless an authorized source already
exists. Failed or partial retrieval remains visible and forces limitation or
abstention output.

### Runtime and tools

The package tool allowlist gains read-only, typed tools:

- `clinical_trials.get_study`
- `clinical_trials.get_history`
- `pubmed.search`
- `pubmed.get_record`
- `trial_audit.compare_registration_and_report`
- `trial_audit.resolve_evidence`

Network tools have explicit timeouts, bounded results, retries for transient
failures, and immutable response fingerprints. Prompt text cannot authorize a
tool or widen its package/release scope.

### Durable execution

Audit submission writes a SQLite-backed task before returning. A leased worker
advances one stage at a time and persists checkpoints, following the existing
source-sync and reverification patterns. Browser requests never own the entire
model call. Duplicate submissions with the same package version and normalized
input reuse the same idempotency key unless the caller explicitly requests a
fresh upstream snapshot.

## Retrieval And Evidence

Version `1.2.0` moves from one-source lexical retrieval to hybrid retrieval over
the book/article release plus normalized registry, publication, and rule
snapshots. Metadata filters constrain evidence by trial identifier, source
class, publication date, and release fingerprint. Reranking occurs before
citation resolution.

The system distinguishes:

- registered fact;
- publication claim;
- deterministic discrepancy;
- model interpretation;
- unresolved or conflicting evidence.

These classes remain visible in the API and UI. Retrieval alone is never
treated as proof.

## Safety And Consumer Boundaries

The package uses `evidence_only`, requires citations, and escalates to
`human_domain_review`. It must abstain on missing identity, unresolved source
conflict, unavailable primary evidence, patient-specific requests, and requests
for diagnosis or treatment selection.

Proofroom owns claim/citation adjudication and conflict resolution. The health
consumer may import only a reviewed draft. It retains ownership of medical
safety, personal context, serving approval, and user-facing actions. No KBase
audit can automatically enter a health serving index.

## Evaluation

The existing deterministic runtime suite remains a prerequisite but is not a
domain release gate. Add a versioned golden set covering:

- NCT and DOI resolution;
- endpoint, enrollment, arm, masking, and timeline extraction;
- registration-publication discrepancy detection;
- citation correctness and evidence-class attribution;
- support, contradiction, unknown, and abstention behavior;
- stale snapshots and changed registry records;
- malformed tool arguments, upstream failures, and prompt injection;
- latency, cost, idempotency, trace redaction, and replay.

The first release gate requires zero unresolved citations, 100% abstention on
unsafe or missing-primary-evidence cases, and explicit human review for every
high-risk discrepancy. Quality thresholds are stored in the package and cannot
be lowered by runtime input.

## Observability And Failure Handling

Each audit trace records package/release versions, source fingerprints,
deterministic findings, retrieval IDs, model route, tool decisions, citations,
cost, latency, and final outcome. It excludes credentials, full source bodies,
private prompts, and consumer user data.

Upstream 404, rate limit, timeout, schema drift, and partial data produce typed
failures. The UI shows the failed stage, retained evidence, retry eligibility,
and whether a fresh snapshot is required. A failed source must never silently
fall back to model memory.

## MVP Acceptance

Given a valid NCT ID, DOI/PMID, or bounded trial claim, the product creates a
shareable audit URL and a structured report whose factual statements resolve
to pinned evidence. It correctly reports missing or conflicting evidence,
survives restart and replay, passes the clinical golden set, receives a
Proofroom verdict, and remains a non-serving draft in the health consumer until
explicit domain approval.

