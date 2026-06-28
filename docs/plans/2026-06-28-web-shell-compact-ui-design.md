# Web Shell Compact UI Design

## Goal

Reduce wasted vertical space in the Web GUI shell, especially the large top header and placeholder module pages, while preserving the desktop-parity navigation model.

## Design

Use a compact operations-console layout:

- Header becomes a single dense row: product label, current route title, and security status pills.
- Navigation becomes a low-height tab strip instead of large cards.
- Placeholder `ModuleLanding` pages become compact status panels that keep status, desktop source, and Wails methods visible without pushing content down.
- Existing real workspaces (`/book-knowledge`, `/course`, `/ebook`, detail readers, login/profile) keep their current data flow and API behavior.

## Non-Goals

- No backend API changes.
- No new content features.
- No changes to Basic Auth, Bearer token, Dedao cookies, or TokenPlan secrets.
- No visual redesign of the KBase three-column workbench in this slice.

## Validation

- Add smoke assertions for compact shell hooks and run them red-green.
- Run `node frontend-web/scripts/web-kbase-ui-smoke.mjs`.
- Run `cd frontend-web && npm run build`.
- Browser-check `/home`, `/course`, and `/book-knowledge` for non-overlap and reduced header height.
