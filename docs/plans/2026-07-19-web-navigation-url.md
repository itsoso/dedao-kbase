# Web Navigation And URL Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace ad hoc Web routes with a canonical navigation and URL contract while preserving existing public aliases.

**Architecture:** Keep the current single-page `frontend-web/app.js` shell and private HTTP server. Add route helper functions and canonical link builders first, then migrate source, knowledge, analysis, job, and delivery surfaces incrementally behind compatibility aliases.

**Tech Stack:** Go private HTTP server, vanilla frontend Web app in `frontend-web/app.js`, CSS in `frontend-web/styles.css`, smoke tests in `frontend-web/scripts/book-knowledge-web-smoke.mjs`.

---

### Task 1: Add Route Contract Smoke Coverage

**Files:**
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write failing assertions**

Add assertions for canonical route markers:

```js
for (const marker of [
  "ROUTES",
  "legacyRouteAliases",
  "buildDedaoCourseURL",
  "buildDedaoCourseDetailURL",
  "buildDedaoEbookURL",
  "buildKnowledgePackageURL",
  "resolveCanonicalRoute",
  "/sources/dedao/courses",
  "/sources/dedao/ebooks",
  "/knowledge/packages",
  "/delivery/health/releases",
]) {
  assert.ok(js.includes(marker), `route contract should include ${marker}`);
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: FAIL with missing `ROUTES`.

**Step 3: Commit after implementation passes**

Commit with:

```bash
git add frontend-web/scripts/book-knowledge-web-smoke.mjs frontend-web/app.js
git commit -m "feat(web): add canonical route contract"
```

### Task 2: Add Canonical Route Helpers

**Files:**
- Modify: `frontend-web/app.js`

**Step 1: Implement constants and helpers**

Add near existing state declarations:

```js
const ROUTES = Object.freeze({
  dedaoHome: "/sources/dedao/home",
  dedaoCourses: "/sources/dedao/courses",
  dedaoEbooks: "/sources/dedao/ebooks",
  knowledgePackages: "/knowledge/packages",
  healthReleases: "/delivery/health/releases",
});

const legacyRouteAliases = Object.freeze({
  "/home": ROUTES.dedaoHome,
  "/course": ROUTES.dedaoCourses,
  "/ebook": ROUTES.dedaoEbooks,
  "/book-knowledge": ROUTES.knowledgePackages,
});
```

Add builders:

```js
function buildDedaoCourseURL(item) {
  const courseID = item?.id || item?.class_id || item?.product_id || "";
  const enid = item?.enid || "";
  const params = new URLSearchParams();
  if (enid) params.set("enid", enid);
  if (item?.publish_num) params.set("total", String(item.publish_num));
  if (item?.title || item?.name) params.set("title", item.title || item.name);
  return courseID ? `${ROUTES.dedaoCourses}/${encodeURIComponent(courseID)}${params.toString() ? `?${params.toString()}` : ""}` : "";
}

function buildDedaoCourseDetailURL(enid) {
  return enid ? `${ROUTES.dedaoCourses}/detail/${encodeURIComponent(enid)}` : "";
}
```

**Step 2: Run smoke**

Run:

```bash
node --check frontend-web/app.js
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: PASS.

### Task 3: Preserve Legacy Aliases

**Files:**
- Modify: `frontend-web/app.js`

**Step 1: Update route parsing**

Make boot accept both canonical and legacy routes:

```js
function resolveCanonicalRoute(pathname = window.location.pathname) {
  for (const [legacy, canonical] of Object.entries(legacyRouteAliases)) {
    if (pathname === legacy || pathname.startsWith(`${legacy}/`)) {
      return canonical + pathname.slice(legacy.length);
    }
  }
  return pathname;
}
```

Use the resolved path in Dedao home, courses, ebooks, and knowledge routing.

**Step 2: Validate aliases**

Run:

```bash
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: PASS.

### Task 4: Migrate Source Navigation Links

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/index.html`

**Step 1: Update navigation hrefs**

Change visible navigation so primary links point to canonical URLs:

```html
<a href="/sources/dedao/home">首页</a>
<a href="/sources/dedao/courses">课程</a>
<a href="/sources/dedao/ebooks">电子书</a>
<a href="/knowledge/packages">书籍知识库</a>
```

Keep legacy aliases working in `boot()`.

**Step 2: Bump static version**

Change `frontend-web/index.html` query strings to a new version such as:

```html
?v=20260719-navigation-url-contract
```

**Step 3: Run tests**

Run:

```bash
node --check frontend-web/app.js
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: PASS.

### Task 5: Verify With Browser Clicks

**Files:**
- No source edits unless verification fails.

**Step 1: Start local static server**

Run:

```bash
cd frontend-web
python3 -m http.server 8766 --bind 127.0.0.1
```

**Step 2: Use browser automation**

Use Chrome/Playwright to assert:

- `/sources/dedao/courses` renders course cards.
- First `继续学习` link starts with `/sources/dedao/courses/{numericId}`.
- First `详情` link starts with `/sources/dedao/courses/detail/{enid}`.
- Clicking `继续学习` renders article titles.
- Legacy `/course` renders the same course list.

**Step 3: Stop local server**

Stop the static server before final response.

### Task 6: Full Verification And Deploy

**Files:**
- Commit only changed source and docs files.

**Step 1: Run checks**

Run:

```bash
go test ./...
cd frontend && npm run build
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
bash scripts/knowledge-eval-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/knowledge-contract-smoke.sh
bash scripts/health-evidence-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all pass.

**Step 2: Commit**

Run:

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/index.html frontend-web/scripts/book-knowledge-web-smoke.mjs docs/plans/2026-07-19-web-navigation-url-design.md docs/plans/2026-07-19-web-navigation-url.md
git commit -m "feat(web): define canonical navigation routes"
```

**Step 3: Deploy**

Use the existing dedao-kbase release workflow: build `cmd/kbase-server`, replace
`/opt/dedao-kbase/bin/kbase-server`, replace `/opt/dedao-kbase/frontend-web`,
restart `dedao-kbase`, and verify `/health`, canonical routes, legacy aliases,
and browser click behavior.
