# Post-Deployment Hardening Design

## Context

Production acceptance for the controlled Book Agent workflow passed, but the
follow-up audit found three repository-owned issues:

1. a Dedao ebook detail always renders the Agent lifecycle step as pending,
   even when the matched knowledge release already has a published Agent
   Package;
2. the desktop frontend remains on an old Vue/Vite/TypeScript toolchain;
3. the desktop entry JavaScript is 5.499 MB because the application globally
   registers the complete Element Plus icon library.

Nginx also reports duplicate `server_name` warnings, but those duplicates come
from unrelated host configuration. This delivery records the evidence and does
not mutate non-KBase server configuration.

## Decisions

- Deliver the repository changes in one isolated branch with independently
  revertible commits.
- Upgrade the coordinated frontend stack to its current stable major versions,
  rather than applying only patch-level security updates.
- Require Node.js `^22.18.0 || >=24.11.0`, the strictest engine range in the
  coordinated dependency graph. This is a build-only requirement; the shipped
  application remains Go plus static assets and does not need Node.js at runtime.
- Reuse the existing knowledge-release and Agent Package APIs. Do not add a new
  backend route solely for the ebook lifecycle display.
- Measure the upgraded build before introducing manual vendor chunks. Remove
  the proven all-icons import first, then add explicit chunking only if the
  measured entry still exceeds the budget.

## Ebook Agent State

The ebook detail loader already resolves a Dedao source to a durable knowledge
book. After that match, it will load the book's published knowledge releases and
the published Agent Package details through the same helpers used by the
knowledge workspace. A shared matcher will consider an Agent available only
when a published package pins one of the book's releases.

The lifecycle step has four explicit states:

- **available**: show the Package ID and version, plus links to the Agent Package
  and runnable Agent;
- **ready to create**: a knowledge release exists but no package pins it; link
  to the knowledge workspace Agent section;
- **blocked**: no published knowledge release exists; direct the user to finish
  quality and release first;
- **unavailable**: release or package state could not be read; show the error
  rather than misreporting a pending state.

No loading response may overwrite state for a different ebook route.

## Coordinated Major Upgrade

The target set, based on stable registry tags on 2026-08-08, is:

- Vue 3.5, Vue Router 5, Pinia 4, and pinia-plugin-persistedstate 4;
- Vite 8 and `@vitejs/plugin-vue` 6;
- TypeScript 5.9 and vue-tsc 3. TypeScript 7 is the registry latest, but is not
  yet compatible with vue-tsc 3 because it no longer exports `lib/tsc`;
- Element Plus 2.14 and `@element-plus/icons-vue` 2.3;
- current stable releases of the existing Vite plugins, Sass, Marked,
  Highlight.js, and Video.js.

The package manifest will declare the Node.js floor. The lockfile will be
regenerated from the manifest. Compatibility changes must be driven by actual
type-check, build, or smoke-test failures; this task will not introduce
speculative refactors or use forced audit rewrites.

The security gate is no high-severity production dependency findings. If a
development-only finding remains after all compatible upgrades, the exact
dependency path and reason must be documented instead of suppressed. Because
the local and Linux npm clients previously returned different audit results,
the final release source is audited in both environments.

## Bundle Budget

The application currently imports `* as ElementPlusIconsVue` and registers
every exported icon globally. Replace that with a bounded registry containing
only route icons and icons actually referenced by templates. Existing lazy
route imports remain the primary code-splitting boundary.

The generated entry JavaScript must:

- be no larger than 2 MB uncompressed; and
- be at least 60 percent smaller than the 5.499 MB baseline.

A deterministic Node smoke test will inspect the built manifest/assets and fail
when the budget regresses. Manual vendor groups are allowed only when the
post-upgrade, bounded-icon build still misses the budget.

## Error Handling And Rollback

Partial Agent Package detail failures are surfaced in the ebook lifecycle
instead of silently becoming "pending". Existing book metadata and reader links
remain usable when Agent status fails.

Each logical change is committed separately: lifecycle consistency, coordinated
dependency upgrade, and bundle governance. A failed test gate stops delivery at
that commit. Production deployment uses an exact clean commit, backs up the
binary and static Web tree, and restores both if service health or online
acceptance fails.

## Verification

Verification includes:

- a RED/GREEN Web smoke for all ebook Agent lifecycle states;
- frontend toolchain contract and dependency audits;
- the bundle-size RED/GREEN smoke;
- all existing frontend-web and desktop frontend smoke scripts;
- TypeScript checking and Vite production build;
- Wails desktop build;
- complete Go tests after the frontend distribution exists;
- generated system-map drift, privacy smoke, and `git diff --check`;
- production checks for the ebook lifecycle, Agent navigation, browser session,
  controlled draft, grounded search/chat, abstention, health, restart count, and
  post-deployment logs.

## Out Of Scope

- changing or reloading unrelated Nginx virtual hosts;
- adding a new Agent Package API;
- changing the controlled Agent publication or authentication contracts;
- publishing a new immutable Agent version merely to test the UI.
