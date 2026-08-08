# Post-Deployment Hardening Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make ebook Agent lifecycle state truthful, upgrade the desktop frontend to the current coordinated major stack, and prevent the desktop entry bundle from regressing above its size budget.

**Architecture:** The static Web UI reuses existing release and Agent Package loaders to resolve ebook state without changing the HTTP contract. The desktop frontend upgrades its peer-dependent framework and build packages as one coordinated unit, then replaces global icon registration with a bounded registry and enforces the measured output through a build artifact smoke test.

**Tech Stack:** Vanilla JavaScript Web UI, Node smoke tests, Vue 3, Vue Router, Pinia, Element Plus, Vite, TypeScript, Wails, Go, systemd deployment.

---

### Task 1: Truthful Ebook Agent Lifecycle

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write the failing smoke assertions**

Extend the Web smoke so it requires dedicated ebook Agent state, a matcher that
accepts only published packages pinning a release for the matched book, visible
available/ready/blocked/error labels, and Package/Agent links when available.

**Step 2: Run the smoke and verify RED**

Run:

```bash
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: FAIL because the ebook lifecycle still contains the hard-coded
`待接入` Agent row and has no Agent Package state.

**Step 3: Implement the minimal state and matcher**

Add route-scoped state for releases, the matching package, loading, and error.
After `findKnowledgePackageForEbook` succeeds, call the existing
`loadKnowledgeReleaseRecords`, `loadKnowledgeAgentPackageRecords`, and
`loadKnowledgeAgentPackageDetails` helpers. Match only:

```js
pkg.lifecycle_state === "published"
  && pkg.releases.some((ref) => releaseIDs.has(ref.release_id))
```

Render available, ready-to-create, blocked, and unavailable states. Preserve the
reader and knowledge-package actions if Agent lookup fails.

**Step 4: Run the focused smoke and verify GREEN**

Run the Step 2 command again.

Expected: PASS.

**Step 5: Commit**

Stage only the two Task 1 files and commit:

```text
fix(web): synchronize ebook agent lifecycle
```

### Task 2: Coordinated Frontend Major Upgrade

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/package-lock.json`
- Create: `frontend/scripts/frontend-toolchain-smoke.mjs`
- Modify only when required by verified compiler/build failures:
  `frontend/src/**`, `frontend/vite.config.ts`

**Step 1: Write the failing toolchain contract smoke**

Create a Node smoke that reads `package.json` and requires Node `>=20.19.0`,
Vue 3.5, Vue Router 5, Pinia 4, Vite 8, TypeScript 7, vue-tsc 3, and their
coordinated current plugins. It must also reject the deprecated Volar packages
present in the old lockfile.

**Step 2: Run the smoke and verify RED**

Run:

```bash
node frontend/scripts/frontend-toolchain-smoke.mjs
```

Expected: FAIL against the current Vue 3.2/Vite 3/TypeScript 4 manifest.

**Step 3: Update the manifest and regenerate the lockfile**

Set exact compatible stable ranges for the approved major versions, add the
Node engine floor, remove unused direct dependencies only when repository search
proves they are unused, and run:

```bash
cd frontend
npm install
```

Do not use `npm audit fix --force`.

**Step 4: Verify the toolchain contract and clean install**

Run:

```bash
node frontend/scripts/frontend-toolchain-smoke.mjs
cd frontend && npm ci
```

Expected: PASS and a reproducible clean install.

**Step 5: Run type-check/build and make only evidence-driven compatibility fixes**

Run:

```bash
cd frontend && npm run build
```

For each failure, record the exact failing API, make the smallest compatible
change, and rerun until green. Do not combine unrelated cleanup.

**Step 6: Run dependency audits**

Run:

```bash
cd frontend
npm audit --omit=dev
npm audit
```

Expected: no high-severity production findings. Any remaining development-only
finding must be documented with its dependency path.

**Step 7: Commit**

Stage only Task 2 files and commit:

```text
build(frontend): upgrade vue and vite toolchain
```

### Task 3: Bounded Icon Registry And Bundle Gate

**Files:**
- Modify: `frontend/src/main.ts`
- Modify: `frontend/vite.config.ts` only if measurement requires explicit vendor grouping
- Create: `frontend/scripts/frontend-bundle-size-smoke.mjs`
- Modify: `frontend/package.json`

**Step 1: Write the failing bundle smoke**

Create a smoke that locates the built entry JavaScript, asserts it is no larger
than 2,000,000 bytes, and reports the percentage reduction from the 5,499,050
byte baseline. Add a source assertion rejecting the wildcard Element Plus icon
import.

**Step 2: Build the baseline and verify RED**

Run:

```bash
cd frontend
npm run build
node scripts/frontend-bundle-size-smoke.mjs
```

Expected: FAIL because the entry is above 5 MB and `main.ts` imports all icons.

**Step 3: Replace wildcard icon registration**

Import and register only icons named by route metadata or unresolved icon tags
in Vue templates. Keep explicit component-local icon imports local. Add a source
contract assertion ensuring every string-valued route icon appears in the
bounded registry.

**Step 4: Build and measure**

Run the Step 2 commands again.

Expected: PASS, entry at or below 2 MB and at least 60 percent below baseline.

**Step 5: Add vendor grouping only if the measured entry still fails**

If Step 4 remains red, use the Vite 8-supported output configuration to isolate
Vue/Pinia, Element Plus, and media/markdown dependencies. Do not add manual
groups when the icon fix already passes the budget.

**Step 6: Commit**

Stage only Task 3 files and commit:

```text
perf(frontend): bound icons and entry bundle
```

### Task 4: Full Local Quality Gates

**Files:**
- Modify only files required by a verified failure
- Modify: `docs/dossiers/2026-08-08-controlled-book-agent-wizard.md`

**Step 1: Run frontend tests in dependency order**

Run:

```bash
cd frontend
npm ci
npm run build
node scripts/frontend-toolchain-smoke.mjs
node scripts/frontend-bundle-size-smoke.mjs
for smoke in scripts/*smoke*.mjs; do node "$smoke"; done
```

Expected: all commands exit zero.

**Step 2: Run the production Web checks**

Run:

```bash
node --check frontend-web/app.js
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
```

Expected: all commands exit zero.

**Step 3: Run desktop and Go checks**

Run the frontend build before Go because `main.go` embeds `frontend/dist`:

```bash
wails build --clean
go vet ./...
go test ./... -timeout=300s
```

Expected: all commands exit zero.

**Step 4: Run architecture and privacy gates**

Run:

```bash
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
git status --short
```

Expected: no architecture drift, privacy failure, or whitespace error; status
contains only task-owned files.

**Step 5: Record evidence and commit**

Append the dependency versions, entry size, audit outcome, full test evidence,
and the out-of-scope Nginx duplicate file evidence to the existing dossier.
Commit only the dossier and any verified task-owned correction:

```text
docs(kbase): record post-deploy hardening gates
```

### Task 5: Merge, Deploy, And Online Acceptance

**Files:**
- Modify only the dossier for verified deployment evidence

**Step 1: Review the branch**

Confirm every commit is independently revertible, the branch is clean, and the
main worktree's pre-existing changes remain untouched.

**Step 2: Preserve user changes and merge**

Create a named safety stash containing only the pre-existing main-worktree
changes, merge the verified branch into local `main`, then restore the stash.
Resolve overlaps by preserving both the verified feature behavior and the
user's newer uncommitted work. Never stage unrelated files.

**Step 3: Build from the exact clean merge commit on Linux**

Create a source archive from the commit, verify its SHA-256 on the server, run
module verification, clean frontend install/build, all smoke tests, Go vet, and
complete Go tests before producing the CGO-enabled server binary.

**Step 4: Deploy with rollback**

Back up `/opt/dedao-kbase/bin/kbase-server` and
`/opt/dedao-kbase/frontend-web`, atomically replace them, start the service, and
restore both backups automatically if the revision health check fails. Do not
modify Nginx configuration.

**Step 5: Run online acceptance**

Verify:

- public health reports the exact commit;
- the pilot ebook displays the existing Agent Package and version instead of
  `待接入`;
- Package and Agent links open without 401 or console errors;
- Cookie/CSRF controlled draft succeeds while bearer-only access is rejected;
- grounded search/chat returns citations and an unrelated query abstains;
- service state is active/running with zero restarts and no new fatal/error
  logs.

Do not publish a new Agent version during acceptance.

**Step 6: Record G5/G6 evidence**

Update the dossier with the exact revision, binary hash, backup path, health,
runtime, and log evidence. Commit the dossier only if that evidence is intended
for the repository; otherwise report it in the delivery handoff.
