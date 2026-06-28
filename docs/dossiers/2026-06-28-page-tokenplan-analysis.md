# Page TokenPlan Analysis Dossier

## Status

In progress.

## Requirement

Add large-model entry points to the Web course and ebook pages. The entry point must use Token Plan to analyze the current page information directly.

## Gate Notes

- G1 Scope: accepted as a one-shot analysis panel for course and ebook detail pages.
- G2 Feasibility: reuse existing TokenPlan config and OpenAI-compatible client; add a generic protected page-analysis endpoint.
- G3 Tests: pending.
- G4 Review: pending.
- G5 Deploy Health: pending.
- G6 Online Verification: pending.

## Implementation Notes

Keep book knowledge chat separate from page analysis. Page analysis uses transient UI context and does not write chat history in this slice.
