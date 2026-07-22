# Clinical Trial Truth Agent Dossier

## Intake

- Date: 2026-07-22
- User request: choose route B and turn
  `book-agent-clinical-trials-truth` into a clinical-trial evidence audit
  product rather than a generic Agent Package demonstration.
- User need: make a book-backed Agent into a useful vertical product combining
  model, knowledge, tools, evidence review, and downstream consumers.
- Current workaround: use generic package search and grounded chat, then inspect
  citations manually.

## Current State

Production package `1.1.0` is published and has passed the runtime pilot. It
pins one WeChat article release with five structured claims and four citations,
uses lexical retrieval, and exposes internal read-only search, claim, citation,
and package metadata tools. Its UI is a generic reader/search/chat/evidence
console. The deterministic evaluation proves infrastructure behavior but does
not measure clinical-trial domain quality.

Reusable foundations:

- immutable Knowledge Releases and citation identities;
- Agent Package policy, publication, evaluation, runtime, and trace contracts;
- source adapter, durable run, outbox, and reverification patterns;
- Proofroom import/feedback and health draft/hold boundaries;
- manifest-driven Web Agent surface and stable routes.

## Definition Artifacts

- Design: `docs/plans/2026-07-22-clinical-trial-truth-agent-design.md`
- Parent architecture: `docs/plans/2026-07-19-book-as-agent-platform-design.md`
- Implementation plan:
  `docs/plans/2026-07-22-clinical-trial-truth-agent.md`

## Gate Decisions

### G1 Admission — PASS

The feature directly advances the product goal of turning each authorized book
into a useful Agent-backed product. The smallest complete slice is one durable
NCT audit with primary-source snapshots, deterministic comparison, cited
explanation, Proofroom review, and health draft isolation.

### G2 Feasibility And Risk — PASS WITH HARD CONSTRAINTS

The existing runtime and source pipeline can support the workflow. Delivery is
allowed only with these constraints:

- read-only authoritative-source tools;
- `evidence_only` package policy and mandatory citations;
- deterministic comparisons before model interpretation;
- explicit abstention and human domain review;
- no diagnosis, treatment selection, prescription, or automatic health serving;
- no copyrighted full-text ingestion without authorization;
- durable execution so browser timeouts cannot orphan model work.

## Delivery Progress

The first implementation slice is complete:

- Task 1 defines and validates the public audit request, source, finding,
  result, and run contracts.
- Task 2 persists leased runs, checkpoints, immutable normalized evidence,
  and idempotency records in SQLite.
- Task 3 collects bounded ClinicalTrials.gov API v2 protocol, outcome,
  participant-flow, denominator, and analysis evidence from the fixed official
  HTTPS origin.
- Database v4 separates stable evidence identity from collection provenance
  and atomically migrates known v3 single- and multi-source terminal audits.

Implementation commits: `afd5c61` through `02c2f3e`. Independent task and
integration reviews found no remaining Critical or Important findings.

## G3 First-Slice Test Evidence — PASS

- focused and race tests for `ClinicalTrialAudit` and `ClinicalTrialsGov`;
- complete `backend/app` and `backend/...` tests;
- `go vet`, `go mod verify`, and JSON syntax validation;
- privacy smoke and `git diff --check`.

The macOS race build emits the existing non-fatal `LC_DYSYMTAB` linker warning;
tests still exit successfully.

## Current Stage

`S5 implementation in progress` — Tasks 1-3 are complete and approved. The
next delivery slice begins with Task 4: bounded PubMed collection and explicit
DOI/PMID/NCT identity resolution. No production route or package version has
changed yet.
