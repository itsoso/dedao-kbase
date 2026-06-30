# Book Knowledge Quality Governance Design

## Goal

Add a deterministic quality and governance layer for every ingested book so the kbase can distinguish usable material from drafts that need review before health, proofroom, or other downstream systems consume it.

## Scope

This design covers book-level quality reports generated during `BookKnowledgeStore.SavePackage`, surfaced through existing `/api/books` and `/api/books/{book_id}` responses, and displayed in the Web KBase workbench. It does not run LLM verification and does not write into health or proofroom.

## Data Model

Each book directory gets `quality_report.json`:

- `version`, `book_id`, `generated_at`
- `status`: `usable`, `needs_review`, or `rejected`
- `score`: deterministic 0-1 score
- `metrics`: chapter, chunk, claim, citation, empty chunk ratio, duplicate claim ratio, average chunk chars, source presence
- `issues`: `{code, severity, message}`
- `allowed_uses` and `blocked_uses`

`BookKnowledgeBook` also carries `quality_status`, `quality_score`, and `quality_updated_at` so list APIs can filter and display quality without loading every package.

## Scoring Rules

The first version is structural and deterministic:

- Reject if there are no chunks or no chapters.
- Mark `needs_review` if claims are absent, citations are absent, empty chunk ratio is high, or duplicate claim ratio is high.
- Mark `usable` only when score is at least 0.75 and no high-severity issue exists.
- Health-sensitive downstream uses stay blocked at the book-quality layer. Project-specific verification can later narrow or expand allowed uses per claim.

## API And UI

`GET /api/books` includes quality fields on each book. `GET /api/books/{book_id}` includes `quality_report`. The Web KBase left rail shows quality status beside extractor, and Overview shows score, metrics, issues, allowed uses, and blocked uses.

## Downstream Contract

Project verification and collection export should treat rejected books as unavailable, and mark `needs_review` books as assistive-only. This keeps the existing project-level gate as the final authority while giving it a stable book-level signal.

## Verification

Add focused Go tests for report generation, manifest quality fields, rejected empty packages, and HTTP serialization. Add Web smoke assertions for quality fields and Overview rendering. Build and deploy with the existing kbase-server flow.
