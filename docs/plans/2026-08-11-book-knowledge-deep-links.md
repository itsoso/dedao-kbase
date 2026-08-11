# Book Knowledge Deep Links Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make book, chapter, and search-result locations in the book knowledge UI directly addressable, and make WC Plus imports link to the canonical knowledge-package route.

**Architecture:** Keep `/knowledge/packages` as the catalog and extend its existing detail route with resource suffixes: `/knowledge/packages/:bookID/chapters/:chapterID` and `/knowledge/packages/:bookID/results/:kind/:resultID`. Parse the URL into a small route-state object, render ordinary links for every selectable resource, and let `boot()` restore the selected book and resource after refresh or browser history navigation. Preserve legacy `/book-knowledge/books/...` links by canonicalizing them before route parsing.

**Tech Stack:** Vanilla JavaScript SPA, semantic HTML/CSS, Node assertion-based smoke tests.

---

### Task 1: Define and restore canonical knowledge resource routes

**Files:**
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Modify: `frontend-web/app.js`

**Step 1: Write the failing route-contract assertions**

Add assertions requiring:

```js
for (const marker of [
  "knowledgeBookPath",
  "knowledgeChapterPath",
  "knowledgeResultPath",
  "knowledgeResourceFromLocation",
  'parts[1] === "chapters"',
  'parts[1] === "results"',
  'legacy === "/book-knowledge" && pathname.startsWith(`${legacy}/books/`)',
]) {
  assert.ok(js.includes(marker), `knowledge deep-link contract should include ${marker}`);
}
```

**Step 2: Run the test and verify RED**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: FAIL because the deep-link helpers and legacy REST-style normalization do not exist.

**Step 3: Implement the minimal route helpers**

Add pure helpers that:

```js
knowledgeBookPath(bookID)
knowledgeChapterPath(bookID, chapterID)
knowledgeResultPath(bookID, kind, resultID)
knowledgeResourceFromLocation(pathname)
```

The parser must decode segments defensively, return `{ type, bookID, resourceID, kind }`, and recognize only the canonical `chapters` and `results` suffixes. Extend `resolveCanonicalRoute()` so `/book-knowledge/books/:bookID/...` maps to the corresponding `/knowledge/packages/:bookID/...` route without changing query parameters or hashes.

**Step 4: Run the test and verify GREEN**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: PASS.

**Step 5: Commit the route contract**

Stage only `frontend-web/app.js`, `frontend-web/scripts/book-knowledge-web-smoke.mjs`, and this plan, then commit with `feat(web): add knowledge resource routes`.

### Task 2: Render navigable books, chapters, and results

**Files:**
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`

**Step 1: Write failing navigation assertions**

Require book, chapter, and result rows to expose link markers, canonical path builders, and active-resource styling:

```js
for (const marker of [
  "data-book-index",
  "data-chapter-index",
  "data-result-index",
  "pushKnowledgeRoute",
  "knowledgeResourceIsActive",
]) {
  assert.ok(js.includes(marker), `knowledge navigation should include ${marker}`);
}
assert.ok(css.includes(".knowledge-web__resource-link"));
assert.ok(css.includes(".knowledge-web__resource-link.active"));
```

**Step 2: Run the test and verify RED**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: FAIL because chapters/results are not links and resource-active styling is absent.

**Step 3: Implement minimal navigation behavior**

- Render book cards as canonical anchors while retaining `data-book-index` for in-app navigation.
- Render chapter rows as `/knowledge/packages/:bookID/chapters/:chapterID` anchors.
- Render result rows as `/knowledge/packages/:bookID/results/:kind/:resultID` anchors.
- Add `pushKnowledgeRoute()` to update browser history without reloading.
- On detail load, derive the active resource from the URL. For chapter routes, highlight and scroll to the matching chapter. For result routes, use the result ID as a bounded search locator, then highlight and scroll to the matching result.
- Ensure `popstate` continues to call `boot()` so Back/Forward restores the URL-scoped state.

**Step 4: Add focused styles**

Make the link rows inherit the existing card layout, remove default underline/color, provide a visible active state, and retain keyboard focus visibility.

**Step 5: Run the test and verify GREEN**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: PASS.

**Step 6: Commit the UI behavior**

Stage only the three files from this task and commit with `feat(web): navigate knowledge resources by URL`.

### Task 3: Link WC Plus imports to canonical knowledge packages

**Files:**
- Modify: `frontend-web/scripts/wcplus-source-ui-smoke.mjs`
- Modify: `frontend-web/app.js`

**Step 1: Write the failing WC Plus assertions**

Require `renderWCPlusBookLinks` and `knowledgeBookPath(id)`, and reject hard-coded `/book-knowledge?book_id=` links.

**Step 2: Run the test and verify RED**

Run: `node frontend-web/scripts/wcplus-source-ui-smoke.mjs`

Expected: FAIL because WC Plus still emits the legacy catalog URL.

**Step 3: Implement canonical import links**

Add one helper that renders knowledge and reader links for an imported book. Reuse it for both the single raw-import confirmation and the recent imported-book list. If no `book_id` exists, render no broken destination.

**Step 4: Run the test and verify GREEN**

Run: `node frontend-web/scripts/wcplus-source-ui-smoke.mjs`

Expected: PASS.

**Step 5: Commit WC Plus navigation**

Stage only the WC Plus smoke test and `frontend-web/app.js`, then commit with `fix(web): link WC Plus imports to knowledge packages`.

### Task 4: Version assets and run release-level verification

**Files:**
- Modify: `frontend-web/index.html`

**Step 1: Write the failing cache-version assertion**

Add a marker assertion for `20260811-knowledge-deep-links` to the book knowledge smoke test.

**Step 2: Run the test and verify RED**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: FAIL because the new asset version is absent.

**Step 3: Update asset versions**

Append `20260811-knowledge-deep-links` to both `styles.css` and `app.js` query versions in `frontend-web/index.html`.

**Step 4: Run all relevant verification**

Run, without piping output:

```bash
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
cd frontend && npm run build
cd .. && go test ./...
bash scripts/privacy-smoke.sh
git diff --check
git status --short
```

Expected: every command exits 0; status contains only the intended plan and Web files.

**Step 5: Commit the release marker**

Stage only `frontend-web/index.html` and any still-uncommitted task files, then commit with `chore(web): version knowledge deep links`.

### Task 5: Resolve independent review findings

**Files:**
- Create: `frontend-web/knowledge-deep-links.mjs`
- Create: `frontend-web/scripts/knowledge-deep-links-smoke.mjs`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Reproduce the API-contract mismatch**

Add executable fixtures proving that search results identify resources with `chunk_id`, `claim_id`, or `citation_id`, not only `id`, and that a resource can be restored exactly from an already loaded knowledge package.

**Step 2: Verify RED**

Run: `node frontend-web/scripts/knowledge-deep-links-smoke.mjs`

Expected: FAIL because the testable route module does not exist.

**Step 3: Implement testable route utilities and race guards**

Extract pure URL, resource-ID, package lookup, modified-click, active-selector, exact-book selection, and search-currentness helpers. Use them from `app.js` so invalid book IDs remain explicit, result refreshes do not misuse full-text search, and stale requests cannot overwrite a newer route.

**Step 4: Verify browser behavior**

Using a temporary local KBase server with generated non-private content, verify:

- chapter click, refresh, and active state;
- real `chunk_id` search-result link generation;
- result refresh and exact package restoration;
- Back/Forward restoration;
- invalid book IDs do not select the first book;
- browser console has no warnings or errors.

**Step 5: Re-run release-level verification and request re-review**

Run all three Web smoke tests, the frontend production build, `go test ./...`, system-map drift, privacy smoke, and `git diff --check`, then request independent review against the updated HEAD.
