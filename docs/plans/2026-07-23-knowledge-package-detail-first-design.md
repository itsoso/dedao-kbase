# Knowledge Package Detail-First Design

## Problem

The knowledge package route currently renders the global review cockpit and pipeline before the selected package. Clicking a global item changes selection, but the useful detail remains below several long operational sections and outside the viewport.

## Decision

Separate the global index and package detail into route-driven views:

- `/knowledge/packages` is the operations index. It owns the global review cockpit, supply status, and knowledge pipeline.
- `/knowledge/packages/:bookID` is a package workspace. It places the selected package at the top of the page and does not render the global dashboards ahead of it.

The detail workspace uses a compact two-column layout: a searchable, scrollable package list on the left and the selected package on the right. The right column starts with title, reading action, package statistics, review status, chapters, baseline analysis, and TokenPlan analysis. A compact toolbar provides “Back to global”, previous package, and next package navigation.

## Interaction

Clicking a cockpit, pipeline, or package-list row navigates to the package URL and renders its detail in the current viewport. Direct links load the same detail-first workspace. Browser back returns to the global index. On narrow screens, the selected package renders first and the searchable package list moves below it with a limited height.

## Validation

Smoke tests assert that global dashboards are route-gated, detail navigation is present, and package detail markup precedes operational dashboards. Playwright screenshots verify desktop and mobile layout, click navigation, and absence of incoherent overflow.
