# Knowledge Package Workspace Design

## Problem

The package detail route now appears before global operations, but its internal
workflow remains a long sequence of unrelated sections. Operators must infer
whether content, baseline analysis, quality review, immutable release, and Agent
Package supply are complete. The package list also permanently consumes desktop
width even when the operator is reading or analyzing one package.

## Decision

Turn `/knowledge/packages/:bookID` into a focused package workspace while
preserving the global index at `/knowledge/packages`.

The workspace adds:

- a compact lifecycle rail for content, analysis, quality/release, and Agent
  supply;
- sticky section navigation for overview, evidence, analysis, and Agent;
- a collapsible package directory that gives the main workspace the full width;
- a real Agent supply status derived from `/api/agent-packages`, matched by the
  current immutable release;
- contextual actions that scroll to the required section, open review details,
  or open the published Agent Package.

## Data Flow

Package detail loading continues to fetch the book payload, analysis manifest,
quality/release state, and review tasks. It additionally loads the Agent Package
collection and finds a package whose pinned release matches the selected
knowledge release. Changing books resets all package-specific state before the
next payload is rendered.

The UI never claims that an Agent exists based only on a published knowledge
release. Agent readiness requires a matching published Agent Package returned
by the server. Missing analysis, failed quality, unpublished releases, and
missing Agent Packages remain distinct lifecycle states.

## Interaction

The lifecycle rail is informative and actionable:

1. Content links to the package overview.
2. Analysis links to baseline and TokenPlan analysis.
3. Quality opens review details and links to the quality section.
4. Agent opens the published package when available, otherwise explains the
   blocking prerequisite.

Desktop users may collapse or restore the package directory from the sticky
toolbar. Mobile always prioritizes the selected package and keeps the directory
below it. Hash links update the URL without losing the package route.

## Validation

Static smoke tests cover lifecycle semantics, section anchors, directory
collapse controls, and real Agent matching. Browser tests verify desktop
expanded/collapsed layouts, section navigation, an Agent-ready package, a
blocked package, mobile ordering, and absence of horizontal overflow.
