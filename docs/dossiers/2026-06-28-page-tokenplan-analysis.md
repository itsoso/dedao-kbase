# Page TokenPlan Analysis Dossier

## Status

Deployed and verified on production.

## Requirement

Add large-model entry points to the Web course and ebook pages. The entry point must use Token Plan to analyze the current page information directly.

## Gate Notes

- G1 Scope: accepted as a one-shot analysis panel for course and ebook detail pages.
- G2 Feasibility: reuse existing TokenPlan config and OpenAI-compatible client; add a generic protected page-analysis endpoint.
- G3 Tests: passed with `go test ./backend/app -count=1`, `go test ./... -count=1`, `npm --prefix frontend-web run build`, and `git diff --check`.
- G4 Review: implementation kept to generic page-analysis API plus shared Web panel; book knowledge chat history remains unchanged.
- G5 Deploy Health: passed after replacing `/opt/dedao-kbase/bin/kbase-server`, refreshing `/var/www/kbase.executor.life`, and confirming `dedao-kbase.service` is `active/running`.
- G6 Online Verification: passed with `/health` 200, unauthenticated `/api/analyze-page` 401, and authenticated local `/api/analyze-page` 200 using `qwen3.7-max`.

## Implementation Notes

Keep book knowledge chat separate from page analysis. Page analysis uses transient UI context and does not write chat history in this slice.
