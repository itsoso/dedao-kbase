import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");

for (const marker of [
  "renderDedaoHome",
  "renderDedaoCourses",
  "renderDedaoEbooks",
  "renderDedaoAudioDetail",
  "renderDedaoLibrary",
  "renderDedaoCourseDetail",
  "renderDedaoCourseArticles",
  "renderDedaoCourseArticle",
  "renderDedaoEbookDetail",
  "renderCourseMarkdown",
  "loadDedaoCourseDetail",
  "loadDedaoCourseArticles",
  "loadMoreDedaoCourseArticles",
  "loadDedaoCourseArticle",
  "loadDedaoEbookDetail",
  "loadDedaoAudioDetail",
  "loadDedaoLibrary",
  "renderBookKnowledge",
  "knowledgeState",
  "browserSessionState",
  "ensureBrowserSession",
  "loadBrowserSession",
  "logoutBrowserSession",
  "loadBookKnowledge",
  "searchBookKnowledge",
  "routePathname === ROUTES.dedaoHome",
  "routePathname === ROUTES.dedaoCourses",
  'getDedaoCourseDetailEnID()',
  'getDedaoCourseRoute()',
  'getDedaoEbookRoute()',
  'getDedaoAudioRoute()',
  "routePathname === ROUTES.dedaoEbooks",
  "routePathname === ROUTES.knowledgePackages",
  "renderBookAgentPlatform",
  "loadBookAgentPlatform",
  "bindBookAgentPlatformEvents",
  "getBookAgentRoute",
  "buildAgentPackageURL",
  "buildAgentURL",
  "buildBookAppURL",
  "hasBookAgentCapability",
  "renderBookAgentCapability",
  "bookAgentState",
  "routePathname === ROUTES.agentPackages",
  "routePathname.startsWith(`${ROUTES.agents}/`)",
  "routePathname.startsWith(`${ROUTES.bookApps}/`)",
]) {
  assert.ok(js.includes(marker), `app.js should include ${marker}`);
}

const getBookIDSource = js.match(/function getBookID\(\) \{([\s\S]*?)\n\}/)?.[1] || "";
assert.ok(getBookIDSource, "app.js should define getBookID");
assert.ok(
  !getBookIDSource.includes("ROUTES.dedaoEbooks"),
  "local reader IDs must not be parsed from canonical Dedao source routes",
);

for (const endpoint of [
  "/browser/session/migrate",
  "/browser/session",
  "/api/browser/session",
  "/api/browser/session/logout",
  "/api/dedao/home",
  "/api/dedao/library?",
  "/api/dedao/course?",
  "/api/dedao/course/articles?",
  "/api/dedao/article?",
  "/api/dedao/audio?",
  "/api/books",
  "/api/search?",
  "/api/book-chat",
  "/api/context-chat",
  "/analysis",
  "/api/knowledge/releases?",
  "/api/agent-packages",
  "/api/knowledge/releases/",
  "/api/knowledge/review?limit=50",
  "/api/knowledge/pipeline",
  "/api/knowledge/pipeline/run",
  "/feedback",
  "/reverification",
  "/reverification/retry",
  "/quality",
  "/publish",
]) {
  assert.ok(js.includes(endpoint), `book knowledge web UI should call ${endpoint}`);
}

for (const marker of [
  "ROUTES",
  "legacyRouteAliases",
  "buildDedaoCourseURL",
  "buildDedaoCourseDetailURL",
  "buildDedaoEbookURL",
  "buildBookReaderURL",
  "buildKnowledgePackageURL",
  "resolveCanonicalRoute",
  "renderJobCenter",
  "loadJobCenter",
  "normalizeJobTask",
  "jobCenterState",
  "/sources/dedao/courses",
  "/sources/dedao/ebooks",
  "/read/books",
  "/knowledge/packages",
  "/delivery/health/releases",
  "/jobs",
  "/agent-packages",
  "/agents",
  "/book-apps",
]) {
  assert.ok(js.includes(marker), `route contract should include ${marker}`);
}

for (const authMarker of [
  'credentials: "same-origin"',
  "response.status === 401",
  "isSafeBearerToken",
  "clearLegacyBrowserTokens",
  "browserSessionFetch",
  "X-KBase-CSRF",
  'new window.BroadcastChannel("kbase-browser-session")',
]) {
  assert.ok(js.includes(authMarker), `book knowledge web UI should include auth marker ${authMarker}`);
}

for (const unwrap of [
  "payload.books",
  "payload.results",
]) {
  assert.ok(js.includes(unwrap), `book knowledge web UI should unwrap ${unwrap}`);
}

for (const className of [
  ".dedao-home",
  ".dedao-courses",
  ".dedao-card",
  ".knowledge-web",
  ".knowledge-web__layout",
  ".knowledge-web__sidebar",
  ".knowledge-web__main",
  ".job-center",
  ".job-card",
  ".job-card__status",
  ".book-agent",
  ".book-agent__hero",
  ".book-agent__capabilities",
  ".book-agent__evidence",
  ".book-agent__evaluation",
  ".book-agent__unavailable",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

assert.ok(js.includes("暂无知识库条目，可先从微信来源导入。"), "empty state should point users to source import");
assert.ok(html.includes('/app.js?v='), "index.html should version app.js to avoid stale browser caches");
assert.ok(html.includes('/styles.css?v='), "index.html should version styles.css to avoid stale browser caches");
assert.ok(html.includes('rel="icon"'), "index.html should declare an application favicon");
assert.ok(html.includes("data:image/svg+xml"), "the favicon should be inline and avoid a missing /favicon.ico request");
assert.ok(html.includes("20260724-evidence-audit-focus"), "evidence audit workspace release should use a fresh browser cache version");
assert.ok(html.includes("20260802-dedao-acquisition"), "Dedao acquisition release should use a fresh browser cache version");
assert.ok(js.includes('"/home": ROUTES.dedaoHome'), "legacy home alias should be preserved");
assert.ok(js.includes('"/course": ROUTES.dedaoCourses'), "legacy course alias should be preserved");
assert.ok(js.includes('"/ebook": ROUTES.dedaoEbooks'), "legacy ebook alias should be preserved");
assert.ok(js.includes('legacy === "/ebook" && pathname.startsWith(`${legacy}/`)'), "legacy nested ebook links should remain local reader links");
assert.ok(js.includes("得到首页"), "Dedao home should be restored");
assert.ok(js.includes("得到课程"), "Dedao course page should be restored");
assert.ok(js.includes("得到电子书"), "Dedao ebook page should be restored");
assert.ok(js.includes("继续学习"), "Dedao course page should expose study actions");
assert.ok(js.includes('`${ROUTES.dedaoCourses}/${encodeURIComponent(courseID)}'), "subscribed course cards should link to canonical numeric course pages");
assert.ok(js.includes('`${ROUTES.dedaoCourses}/${encodeURIComponent(courseID)}/articles/${encodeURIComponent(articleEnID)}'), "course article rows should link to canonical course article pages");
assert.ok(js.includes('`${ROUTES.dedaoCourses}/detail/${encodeURIComponent(enid)}`'), "subscribed course details should use the explicit canonical detail route");
assert.ok(js.includes("加载更多"), "course detail should load more than the first page of articles");
assert.ok(js.includes("课程正文"), "course article reader should render article body");
assert.ok(js.includes("听书详情"), "audio detail route should render product details");
assert.ok(js.includes("听书文稿"), "audio detail route should render the available transcript");
assert.ok(js.includes("dedao-course-article__image"), "course article reader should render markdown images as images");
assert.ok(js.includes("dedao-course-article__analysis"), "course article reader should expose TokenPlan analysis");
assert.ok(js.includes("course-article-analysis-form"), "course article reader should include an article analysis form");
assert.ok(js.includes('buildBookReaderURL(currentBook.book_id)'), "book details should link to the canonical reader");
assert.ok(js.includes("knowledge-web__analysis"), "single article knowledge pages should expose an LLM analysis workspace");
assert.ok(js.includes("分析当前文章"), "single article knowledge pages should include an article analysis action");
assert.ok(js.includes("Qwen-3.7-Max"), "book knowledge analysis should default to Qwen-3.7-Max");
assert.ok(js.includes('<option value="${escapeAttribute(model.id)}"'), "model options should send canonical API ids");
assert.ok(js.includes('id: "qwen3.7-max", label: "Qwen-3.7-Max"'), "Qwen display label should map to the canonical TokenPlan id");
for (const marker of [
  "analysisManifest",
  "loadKnowledgeAnalysisManifest",
  "bookKnowledgeDetailSequence",
  "generateKnowledgeAnalysisManifest",
  "知识基线分析",
  "生成基线分析",
  "重新生成",
  "pending",
  "ready",
  "failed",
]) {
  assert.ok(js.includes(marker), `book knowledge web UI should include durable analysis marker ${marker}`);
}
assert.ok(css.includes(".knowledge-web__manifest"), "styles.css should style the durable analysis manifest");
const selectKnowledgeBookSource = js.match(/async function selectKnowledgeBook\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(selectKnowledgeBookSource.includes("++bookKnowledgeDetailSequence"), "book detail loading should assign a route-scoped sequence");
assert.ok(selectKnowledgeBookSource.includes("sequence !== bookKnowledgeDetailSequence"), "stale book detail responses should be discarded");
assert.ok(selectKnowledgeBookSource.includes("knowledgeState.selectedBook?.book_id !== book.book_id"), "book detail writes should remain scoped to the selected book");

for (const marker of [
  "controlledAgentWizard",
  "Book Agent 向导",
  "选择 Release",
  "配置只读策略",
  "评测并发布",
  "/api/controlled-agent/draft",
  "/api/controlled-agent/evaluate",
  "/api/controlled-agent/publish",
  "data-controlled-agent-step",
  "controlled-agent-confirm-publish",
]) {
  assert.ok(js.includes(marker), `controlled Book Agent wizard should include ${marker}`);
}
assert.ok(css.includes(".controlled-agent-wizard"), "styles.css should style the controlled Book Agent wizard");

for (const capability of ["reader", "search", "grounded_chat", "evidence", "quiz", "action_plan"]) {
  assert.ok(
    js.includes(`renderBookAgentCapability("${capability}"`),
    `shared Book App should render ${capability} through the manifest gate`,
  );
}
assert.ok(js.includes("ui_manifest?.capabilities"), "Book App capabilities should come from ui_manifest");
assert.ok(js.includes("功能已声明，但运行时尚未接通"), "declared unavailable capabilities should explain runtime status");
for (const marker of [
  "renderAgentPackageContract",
  "renderAgentReadingApp",
  "包契约总览",
  "阅读研究台",
  "推理模型",
  "关键词检索",
  "人工复核",
]) {
  assert.ok(js.includes(marker), `Chinese Agent pages should include ${marker}`);
}
for (const staleLabel of [
  "PACKAGE CONTRACT",
  "SHARED BOOK APP",
  ">Reader<",
  ">Grounded search<",
  ">Open the book<",
  "Evaluation passed",
]) {
  assert.ok(!js.includes(staleLabel), `Chinese Agent pages should remove ${staleLabel}`);
}
assert.ok(css.includes("@media (max-width: 760px)"), "Book App should include a narrow mobile layout");
const metricStripSource = css.match(/\.book-agent__manifest \.book-agent__metric-strip \{([\s\S]*?)\n\}/)?.[1] || "";
assert.ok(metricStripSource.includes("display: grid"), "Agent metrics should use a wrapping grid instead of one rigid flex row");
assert.ok(metricStripSource.includes("auto-fit"), "Agent metrics should adapt their column count to the viewport");

const packageSearchSource = js.match(/async function searchBookAgentPackage\(route\) \{([\s\S]*?)\n\}/)?.[1] || "";
const packageChatSource = js.match(/async function chatWithBookAgentPackage\(route\) \{([\s\S]*?)\n\}/)?.[1] || "";
assert.ok(packageSearchSource.includes("/api/agent-packages/"), "Book App search should use the versioned package runtime");
assert.ok(packageSearchSource.includes('version: pkg.version'), "Book App search should pin the package version");
assert.ok(!packageSearchSource.includes("/api/search?"), "Book App search must not fall back to the generic single-book endpoint");
assert.ok(packageChatSource.includes("/api/agent-packages/"), "Book App chat should use the versioned package runtime");
assert.ok(packageChatSource.includes("version=${encodeURIComponent(pkg.version)}"), "Book App chat should pin the package version");
assert.ok(!packageChatSource.includes("/api/book-chat"), "Book App chat must not fall back to the generic single-book endpoint");
assert.ok(js.includes("renderBookAgentAnswerCitations"), "Book App should render citation identities returned by package chat");
assert.ok(js.includes("result.release_id"), "Book App search should render release identity across multi-release packages");
for (const marker of [
  "bookAgentActionSequence",
  "isBookAgentActionCurrent",
  "searchStatus",
  "chatStatus",
  "activeAction",
  'aria-busy="${bookAgentState.activeAction',
  'bookAgentState.activeAction ? "disabled"',
]) {
  assert.ok(js.includes(marker), `Book App runtime actions should include ${marker}`);
}
assert.ok(
  packageSearchSource.includes("isBookAgentActionCurrent"),
  "a late search response must not update a different Agent route",
);
assert.ok(
  packageChatSource.includes("isBookAgentActionCurrent"),
  "a late chat response must not update a different Agent route",
);
assert.ok(
  packageSearchSource.includes("bookAgentState.activeAction"),
  "search should reject duplicate runtime submissions",
);
assert.ok(
  packageChatSource.includes("bookAgentState.activeAction"),
  "chat should reject duplicate runtime submissions",
);
const actionGuardSource = js.match(/function isBookAgentActionCurrent\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(actionGuardSource, "app.js should define a route-aware Agent action guard");
let activeAgentRoute = { view: "agent", packageID: "agent-1", version: "1.0.1" };
const isBookAgentActionCurrent = new Function(
  "getBookAgentRoute",
  "bookAgentState",
  `${actionGuardSource}; let bookAgentActionSequence = 7; return isBookAgentActionCurrent;`,
)(
  () => activeAgentRoute,
  { package: { package_id: "agent-1", version: "1.0.1" } },
);
const actionRoute = { view: "agent", packageID: "agent-1", version: "1.0.1" };
assert.equal(isBookAgentActionCurrent(7, actionRoute), true, "the current Agent action should remain active");
assert.equal(isBookAgentActionCurrent(6, actionRoute), false, "a superseded Agent action should be stale");
activeAgentRoute = { view: "package", packageID: "agent-1", version: "1.0.1" };
assert.equal(isBookAgentActionCurrent(7, actionRoute), false, "switching Agent views should invalidate the action");
activeAgentRoute = { view: "agent", packageID: "agent-2", version: "1.0.1" };
assert.equal(isBookAgentActionCurrent(7, actionRoute), false, "switching Agent packages should invalidate the action");

for (const marker of [
  "ebookAgentReleases",
  "ebookAgentPackage",
  "ebookAgentStatusError",
  "findPublishedAgentPackageForReleases",
  "Agent Package 已可用",
  "待创建 Agent",
  "需先发布知识 Release",
  "Agent 状态读取失败",
  "buildAgentPackageURL(ebookAgentPackage.package_id, ebookAgentPackage.version)",
  "buildAgentURL(ebookAgentPackage.package_id, ebookAgentPackage.version)",
]) {
  assert.ok(js.includes(marker), `Dedao ebook Agent lifecycle should include ${marker}`);
}
assert.ok(
  !/<li class="is-pending"><span>3<\/span><div><strong>书籍 Agent<\/strong>[\s\S]{0,180}<b>待接入<\/b><\/li>/.test(js),
  "Dedao ebook detail must not hard-code every Agent lifecycle as pending",
);
const ebookAgentMatcherSource = js.match(/function findPublishedAgentPackageForReleases\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(ebookAgentMatcherSource, "app.js should define the shared ebook Agent Package matcher");
const findPublishedAgentPackageForReleases = new Function(
  `${ebookAgentMatcherSource}; return findPublishedAgentPackageForReleases;`,
)();
const matchingAgent = findPublishedAgentPackageForReleases(
  [{ release_id: "release-book" }],
  [
    { package_id: "agent", version: "1.0.0", lifecycle_state: "published", published_at: "2026-08-01T00:00:00Z", releases: [{ release_id: "release-book" }] },
    { package_id: "agent", version: "1.0.1", lifecycle_state: "published", published_at: "2026-08-02T00:00:00Z", releases: [{ release_id: "release-book" }] },
    { package_id: "draft", version: "9.0.0", lifecycle_state: "draft", published_at: "2026-08-03T00:00:00Z", releases: [{ release_id: "release-book" }] },
    { package_id: "other", version: "2.0.0", lifecycle_state: "published", published_at: "2026-08-04T00:00:00Z", releases: [{ release_id: "release-other" }] },
  ],
);
assert.equal(matchingAgent?.version, "1.0.1", "ebook Agent matcher should prefer the newest published package pinning this book");
assert.equal(
  findPublishedAgentPackageForReleases([{ release_id: "release-book" }], []),
  null,
  "ebook Agent matcher should return null when no package pins this book",
);

const ebookKnowledgeMatcherSource = js.match(/function findKnowledgePackageForEbook\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(ebookKnowledgeMatcherSource, "app.js should define the ebook knowledge-package matcher");
const findKnowledgePackageForEbook = new Function(
  "dedaoProductID",
  "dedaoProductEnID",
  `${ebookKnowledgeMatcherSource}; return findKnowledgePackageForEbook;`,
)(
  (item) => String(item?.sourceID || ""),
  (item) => String(item?.sourceEnID || ""),
);
const sameTitleWrongBook = { book_id: "wrong", title: "同名书" };
const strongIDBook = { book_id: "right", dedao_id: "source-1", title: "另一标题" };
assert.equal(
  findKnowledgePackageForEbook(
    { sourceID: "source-1", title: "同名书" },
    [sameTitleWrongBook, strongIDBook],
  )?.book_id,
  "right",
  "ebook knowledge matching should prefer a durable source ID over title order",
);
assert.equal(
  findKnowledgePackageForEbook({ sourceID: "missing", title: "同名书" }, [sameTitleWrongBook]),
  null,
  "ebook knowledge matching must not fall back to title when a strong ID is available",
);
assert.equal(
  findKnowledgePackageForEbook(
    { sourceID: "source-1", sourceEnID: "enid-2", title: "同名书" },
    [strongIDBook, { book_id: "conflict", enid: "enid-2", title: "同名书" }],
  ),
  null,
  "conflicting strong ebook identifiers should fail closed",
);
assert.equal(
  findKnowledgePackageForEbook(
    { sourceID: "source-1", sourceEnID: "enid-2", title: "同名书" },
    [{ book_id: "internally-conflicted", dedao_id: "source-1", enid: "enid-1", title: "同名书" }],
  ),
  null,
  "one candidate with a matching ID but conflicting enid should fail closed",
);
assert.equal(
  findKnowledgePackageForEbook({ title: "唯一书" }, [{ book_id: "unique", title: "唯一书" }])?.book_id,
  "unique",
  "title fallback should remain available when the source has no strong identifier",
);
assert.equal(
  findKnowledgePackageForEbook(
    { title: "重复书" },
    [{ book_id: "first", title: "重复书" }, { book_id: "second", title: "重复书" }],
  ),
  null,
  "ambiguous title fallback should fail closed",
);

const ebookLoadCurrentSource = js.match(/function isDedaoEbookDetailLoadCurrent\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(ebookLoadCurrentSource, "app.js should define a route-aware ebook load guard");
let activeEbookRoute = { enid: "ebook-1" };
const isDedaoEbookDetailLoadCurrent = new Function(
  "getDedaoEbookRoute",
  `${ebookLoadCurrentSource}; let dedaoEbookDetailLoadSequence = 7; return isDedaoEbookDetailLoadCurrent;`,
)(() => activeEbookRoute);
assert.equal(
  isDedaoEbookDetailLoadCurrent(7, { enid: "ebook-1" }),
  true,
  "the active ebook request should remain current",
);
activeEbookRoute = null;
assert.equal(
  isDedaoEbookDetailLoadCurrent(7, { enid: "ebook-1" }),
  false,
  "an ebook request should become stale after navigating to a non-ebook route",
);
activeEbookRoute = { enid: "ebook-2" };
assert.equal(
  isDedaoEbookDetailLoadCurrent(7, { enid: "ebook-1" }),
  false,
  "an ebook request should become stale after navigating to another ebook",
);
const bootSource = js.slice(js.indexOf("async function boot()"), js.indexOf('window.addEventListener?.("popstate"'));
assert.ok(
  bootSource.includes("dedaoEbookDetailLoadSequence += 1"),
  "route navigation should invalidate a pending ebook detail request",
);

for (const marker of [
  "resetKnowledgeReview",
  "loadKnowledgeReview",
  "scheduleKnowledgeReviewPoll",
  "retryKnowledgeReverification",
  "publishKnowledgeCandidate",
  "loadKnowledgeReviewCockpit",
  "renderKnowledgeReviewCockpit",
  "Review Cockpit",
  "全局复核",
  "data-cockpit-book-id",
  "knowledgeReviewLatestTask",
  "candidate_ready",
  "window.confirm",
  'params.set("review", "1")',
  "复核与发布",
  "候选差异",
  "质量规则",
  "重新入队",
  "确认发布",
  "修复内容版本",
  "首次发布知识 Release",
  "repairBookContentHash",
  "/repair-content-hash",
  "canInitialPublish",
  "renderKnowledgeSupplyStatus",
  "供应链状态",
  "renderKnowledgePipelineDashboard",
  "知识流水线",
  "自动推进一次",
  "pipelineAutomation",
  "Source Connector",
  "Search Index",
  "Health Feed",
  "Evaluation",
  "Rebuild Plan",
  "rebuild_actions",
  "rebuild_plan",
]) {
  assert.ok(js.includes(marker), `book knowledge review console should include ${marker}`);
}
assert.ok(js.includes("limit: 1"), "pipeline automation should advance one package per browser request");

for (const marker of [
  "isKnowledgePackageDetailRoute",
  "knowledge-web--detail",
  "knowledge-web__detail-toolbar",
  "返回全部知识包",
  "上一条",
  "下一条",
]) {
  assert.ok(js.includes(marker), `knowledge package detail-first layout should include ${marker}`);
}
assert.ok(
  js.includes('isPackageDetail ? "" : renderKnowledgeReviewCockpit()'),
  "global review cockpit should be hidden on package detail routes",
);
assert.ok(
  js.includes('isPackageDetail ? "" : renderKnowledgePipelineDashboard()'),
  "global pipeline should be hidden on package detail routes",
);

for (const marker of [
  "knowledgePackageAgentMatch",
  "knowledgePackageLifecycle",
  "loadKnowledgeAgentPackageRecords",
  "loadKnowledgeAgentPackageDetails",
  "/api/agent-packages?limit=200",
  "/api/agent-packages/${encodeURIComponent(record.package_id)}?version=${encodeURIComponent(record.version)}",
  "next_cursor",
  "knowledge-workspace__lifecycle",
  "knowledge-workspace__nav",
  "knowledge-directory-toggle",
  'id="knowledge-overview"',
  'id="knowledge-evidence"',
  'id="knowledge-analysis"',
  'id="knowledge-agent"',
]) {
  assert.ok(js.includes(marker), `knowledge package workspace should include ${marker}`);
}

for (const className of [
  ".knowledge-review",
  ".knowledge-review__summary",
  ".knowledge-review__evidence",
  ".knowledge-review__rules",
  ".knowledge-cockpit",
  ".knowledge-cockpit__metrics",
  ".knowledge-cockpit__items",
  ".knowledge-supply",
  ".knowledge-supply__card",
  ".knowledge-supply__status",
  ".knowledge-web--detail",
  ".knowledge-web__detail-toolbar",
  ".knowledge-workspace__lifecycle",
  ".knowledge-workspace__nav",
  ".knowledge-web.is-directory-collapsed",
  ".knowledge-workspace__agent",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

console.log("book knowledge web smoke passed");
