const app = document.querySelector("#app");

const tokenKeys = [
  "kbase.token",
  "kbaseToken",
  "KBASE_AUTH_TOKEN",
];

const ROUTES = Object.freeze({
  dedaoHome: "/sources/dedao/home",
  dedaoCourses: "/sources/dedao/courses",
  dedaoEbooks: "/sources/dedao/ebooks",
  dedaoAudio: "/sources/dedao/audio",
  bookReader: "/read/books",
  knowledgePackages: "/knowledge/packages",
  agentPackages: "/agent-packages",
  agents: "/agents",
  bookApps: "/book-apps",
  healthReleases: "/delivery/health/releases",
  jobs: "/jobs",
});

const legacyRouteAliases = Object.freeze({
  "/home": ROUTES.dedaoHome,
  "/course": ROUTES.dedaoCourses,
  "/ebook": ROUTES.dedaoEbooks,
  "/odob": ROUTES.dedaoAudio,
  "/book-knowledge": ROUTES.knowledgePackages,
});

const wechatState = {
  articleURL: "",
  bookID: "",
  accountQuery: "",
  accounts: [],
  selectedAccount: null,
  accountArticles: [],
  articleBegin: 0,
  articleCount: 10,
  preview: null,
  imported: null,
  loading: "",
  message: "",
};

const wcplusState = {
  accounts: [],
  selectedAccount: null,
  articles: [],
  searchQuery: "",
  searchMode: "fulltext",
  searchResults: [],
  searchOffset: 0,
  searchNum: 30,
  tasks: [],
  preview: null,
  serviceStatus: null,
  envCheck: null,
  utilityResult: null,
  batchResult: null,
  accountOffset: 0,
  accountNum: 20,
  articleOffset: 0,
  articleNum: 20,
  importLimit: 10,
  exportRecentNum: 100,
  taskCrawlerType: "gzh_article_link",
  taskArticleListType: "all",
  taskArticleListDate: 0,
  taskArticleListAmount: 20,
  taskArticleListOffset: 0,
  taskArticleRefresh: false,
  taskArticleImageDownload: false,
  taskReadingDataType: "all",
  taskReadingDataStartDate: 0,
  taskReadingDataEndDate: 0,
  taskReadingDataAmount: 1000,
  taskReadingDataOnlyMain: true,
  taskReadingDataRefresh: false,
  batchNicknames: "",
  batchExactMatch: true,
  batchArticleListType: "all",
  batchArticleListAmount: 0,
  batchImportToKBase: false,
  batchWaitForCompletion: false,
  batchImportLimit: 10,
  rawTitle: "",
  rawNickname: "",
  rawURL: "",
  rawBookID: "",
  rawContent: "",
  rawImported: null,
  importedPackages: [],
  loading: "",
  message: "",
};

const jobCenterState = {
  tasks: [],
  loading: "",
  message: "",
  lastUpdated: "",
};

const dedaoLibraryState = {
  home: null,
  homeLoading: "",
  homeMessage: "",
  pages: {
    bauhinia: { items: [], page: 1, pageSize: 15, isMore: 0, loading: "", message: "" },
    ebook: { items: [], page: 1, pageSize: 15, isMore: 0, loading: "", message: "" },
    odob: { items: [], page: 1, pageSize: 15, isMore: 0, loading: "", message: "" },
  },
  courseDetail: null,
  courseDetailLoading: "",
  courseDetailMessage: "",
  courseArticlesLoadingMore: "",
  courseArticle: null,
  courseArticleLoading: "",
  courseArticleMessage: "",
  courseArticleAnalysisModel: "qwen3.7-max",
  courseArticleAnalysisPrompt: "",
  courseArticleAnalysisResponse: null,
  courseArticleAnalysisLoading: "",
  courseArticleAnalysisError: "",
  courseArticleAnalysisKey: "",
  ebookDetail: null,
  ebookPackage: null,
  ebookDetailLoading: "",
  ebookDetailMessage: "",
};

const sourceControlState = {
  agents: [],
  subscriptions: [],
  runs: [],
  selectedSubscriptionID: "",
  selectedRunID: "",
  runDetail: null,
  runFilter: "all",
  legacyDiagnosticsOpen: false,
  loading: "",
  message: "",
  draft: {
    sourceAccountKey: "",
    sourceAccount: "",
    sourceAgentID: "",
    sourceOperation: "sync_articles",
    sourceScheduleMode: "manual",
    sourceIntervalSeconds: 3600,
  },
};

const knowledgeState = {
  books: [],
  selectedBook: null,
  package: null,
  query: "",
  results: [],
  analysisModel: "qwen3.7-max",
  analysisPrompt: "",
  analysisResponse: null,
  analysisLoading: "",
  analysisError: "",
  analysisManifest: null,
  analysisManifestLoading: "",
  analysisManifestError: "",
  releases: [],
  selectedRelease: null,
  releaseDetail: null,
  feedbackAssessment: null,
  reverificationTasks: [],
  qualityReport: null,
  reviewCockpit: null,
  reviewCockpitOpen: true,
  reviewCockpitLoading: "",
  reviewCockpitError: "",
  reviewOpen: false,
  reviewLoading: "",
  reviewError: "",
  reviewOperation: "",
  pipelineDashboard: null,
  pipelineLoading: "",
  pipelineError: "",
  pipelineAutomation: null,
  pipelineAutomationLoading: "",
  pipelineAutomationError: "",
  loading: "",
  message: "",
};

const bookAgentState = {
  packages: [],
  package: null,
  releases: [],
  route: null,
  query: "",
  results: [],
  question: "",
  answer: null,
  loading: "",
  message: "",
};

const knowledgeAnalysisPrompts = [
  ["article", "分析当前文章", "请分析当前文章的核心论点、关键证据、适用边界和可执行启发。回答要引用 claim_id 或 chunk_id。"],
  ["summary", "结构化总结", "请用结构化方式总结当前内容：一句话结论、3-5 个关键点、证据来源、我下一步该怎么读。"],
  ["claims", "证据审计", "请审计当前内容的 claims：哪些证据强，哪些证据弱，哪些需要外部数据验证。每项引用 claim_id 或 chunk_id。"],
  ["actions", "行动建议", "请把当前内容转成可执行清单，区分立即行动、需要验证、长期跟踪，并说明依据。"],
];

const knowledgeAnalysisModels = [
  { id: "qwen3.7-max", label: "Qwen-3.7-Max" },
  { id: "qwen3.7-plus", label: "Qwen-3.7-Plus" },
  { id: "MiniMax-M2.5", label: "MiniMax-M2.5" },
];

function knowledgeModelLabel(modelID) {
  return knowledgeAnalysisModels.find((model) => model.id === modelID)?.label || modelID;
}

function knowledgeReviewReasonLabel(reason) {
  const labels = {
    reverification_queued: "等待复核",
    reverification_running: "复核中",
    reverification_failed: "复核失败",
    candidate_ready: "候选待发布",
    no_delivery_receipt: "未被下游接收",
  };
  return labels[reason] || reason;
}

let isWCPlusBootstrapped = false;
let sourceControlPollTimer = null;
let sourceControlLoadSequence = 0;
let knowledgeReviewPollTimer = null;
let knowledgeReviewLoadSequence = 0;

function getToken() {
  for (const key of tokenKeys) {
    const value = window.localStorage.getItem(key);
    const clean = String(value || "").trim();
    if (!clean) {
      continue;
    }
    if (isSafeBearerToken(clean)) {
      return clean;
    }
    console.warn("skip invalid kbase token", key);
    window.localStorage.removeItem(key);
  }
  return "";
}

function isSafeBearerToken(token) {
  const clean = String(token || "").trim();
  if (!clean || /\s/.test(clean)) {
    return false;
  }
  return /^[\x21-\x7e]+$/.test(clean);
}

function clearStoredToken() {
  for (const key of tokenKeys) {
    window.localStorage.removeItem(key);
  }
}

function storeToken(token) {
  const clean = String(token || "").trim();
  if (!isSafeBearerToken(clean)) {
    clearStoredToken();
    return "";
  }
  for (const key of tokenKeys) {
    window.localStorage.setItem(key, clean);
  }
  return clean;
}

function setAuthorizationHeader(headers, token) {
  const clean = String(token || "").trim();
  if (!isSafeBearerToken(clean)) {
    if (clean) {
      clearStoredToken();
      console.warn("skip invalid kbase token", "authorization");
    }
    return false;
  }
  headers.set("Authorization", `Bearer ${clean}`);
  return true;
}

async function refreshBrowserSessionToken() {
  const response = await fetch("/browser/session-token", {
    headers: {
      Accept: "application/json",
    },
    credentials: "same-origin",
    cache: "no-store",
  });
  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = null;
    }
  }
  if (!response.ok) {
    const message = payload && typeof payload === "object"
      ? (payload.error || payload.message || JSON.stringify(payload))
      : (text || `HTTP ${response.status}`);
    throw new Error(message);
  }
  return storeToken(payload?.token || "");
}

async function ensureBrowserSessionToken() {
  const existing = getToken();
  if (existing) {
    return existing;
  }
  try {
    return await refreshBrowserSessionToken();
  } catch (error) {
    console.warn("Unable to load kbase browser session token", error);
    return "";
  }
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function escapeAttribute(value) {
  return escapeHTML(value).replaceAll("\n", " ");
}

async function apiFetch(path, options = {}, didRefreshAuth = false) {
  const headers = new Headers(options.headers || {});
  headers.set("Accept", "application/json");
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const token = getToken() || await ensureBrowserSessionToken();
  if (token) {
    setAuthorizationHeader(headers, token);
  }

  const response = await fetch(path, {
    ...options,
    headers,
  });
  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = text;
    }
  }
  if (!response.ok) {
    if (response.status === 401 && !didRefreshAuth) {
      try {
        const refreshed = await refreshBrowserSessionToken();
        if (refreshed) {
          return apiFetch(path, options, true);
        }
      } catch (error) {
        console.warn("Unable to refresh kbase browser session token", error);
      }
    }
    const message = typeof payload === "object" && payload
      ? (payload.error || payload.message || JSON.stringify(payload))
      : (payload || `HTTP ${response.status}`);
    const error = new Error(message);
    error.status = response.status;
    error.payload = payload;
    throw error;
  }
  return payload;
}

async function apiDownload(path, options = {}, filename = "download.bin", didRefreshAuth = false) {
  const headers = new Headers(options.headers || {});
  const token = getToken() || await ensureBrowserSessionToken();
  if (token) {
    setAuthorizationHeader(headers, token);
  }
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(path, {
    ...options,
    headers,
  });
  if (!response.ok) {
    if (response.status === 401 && !didRefreshAuth) {
      try {
        const refreshed = await refreshBrowserSessionToken();
        if (refreshed) {
          return apiDownload(path, options, filename, true);
        }
      } catch (error) {
        console.warn("Unable to refresh kbase browser session token", error);
      }
    }
    const text = await response.text();
    throw new Error(text || `HTTP ${response.status}`);
  }
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
  return blob.size;
}

const readerRouteSuffixes = [
  "overview",
  "chat",
  "prompts",
  "chapters",
  "claims",
  "chunks",
  "jobs",
  "system-kb",
  "skills",
  "ops",
];

function normalizeReaderBookID(bookID) {
  const value = String(bookID || "").trim();
  for (const suffix of readerRouteSuffixes) {
    const marker = `-${suffix}`;
    if (value.endsWith(marker)) {
      const base = value.slice(0, -marker.length);
      if (/^\d+$/.test(base)) {
        return base;
      }
    }
  }
  return value;
}

function resolveCanonicalRoute(pathname = window.location.pathname) {
  for (const [legacy, canonical] of Object.entries(legacyRouteAliases)) {
    if (legacy === "/ebook" && pathname.startsWith(`${legacy}/`)) {
      return ROUTES.bookReader + pathname.slice(legacy.length);
    }
    if (pathname === legacy || pathname.startsWith(`${legacy}/`)) {
      return canonical + pathname.slice(legacy.length);
    }
  }
  return pathname;
}

function getRoutePathname() {
  return resolveCanonicalRoute(window.location.pathname);
}

function getPathSegmentAfter(prefix, pathname = getRoutePathname()) {
  if (!pathname.startsWith(prefix)) {
    return "";
  }
  return pathname.slice(prefix.length).split("/")[0];
}

function getBookID() {
  const raw = getPathSegmentAfter(`${ROUTES.bookReader}/`);
  if (!raw) {
    return "";
  }
  try {
    return normalizeReaderBookID(decodeURIComponent(raw));
  } catch {
    return normalizeReaderBookID(raw);
  }
}

function getDedaoEbookRoute() {
  const raw = getPathSegmentAfter(`${ROUTES.dedaoEbooks}/`);
  if (!raw) {
    return null;
  }
  try {
    return { enid: decodeURIComponent(raw) };
  } catch {
    return { enid: raw };
  }
}

function getKnowledgeBookID() {
  const raw = getPathSegmentAfter(`${ROUTES.knowledgePackages}/`) || getPathSegmentAfter("/book-knowledge/");
  if (!raw) {
    return "";
  }
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

function getDedaoCourseDetailEnID() {
  const raw = getPathSegmentAfter(`${ROUTES.dedaoCourses}/detail/`) || getPathSegmentAfter("/course/detail/");
  if (!raw) {
    return "";
  }
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

function getDedaoCourseRoute() {
  const pathname = getRoutePathname();
  const prefix = `${ROUTES.dedaoCourses}/`;
  if (!pathname.startsWith(prefix) || pathname.startsWith(`${ROUTES.dedaoCourses}/detail/`)) {
    return null;
  }
  const rawID = pathname.slice(prefix.length).split("/")[0];
  if (!rawID || !/^\d+$/.test(rawID)) {
    return null;
  }
  const params = new URLSearchParams(window.location.search);
  const enid = params.get("enid") || "";
  return {
    id: rawID,
    enid,
    title: params.get("title") || "",
    total: params.get("total") || "",
  };
}

function getDedaoCourseArticleRoute() {
  const pathname = getRoutePathname();
  const prefix = `${ROUTES.dedaoCourses}/`;
  if (!pathname.startsWith(prefix)) {
    return null;
  }
  const parts = pathname.slice(prefix.length).split("/").filter(Boolean);
  if (parts.length < 3 || parts[1] !== "articles" || !/^\d+$/.test(parts[0])) {
    return null;
  }
  const params = new URLSearchParams(window.location.search);
  try {
    return {
      courseID: decodeURIComponent(parts[0]),
      articleEnID: decodeURIComponent(parts[2]),
      classEnID: params.get("class_enid") || "",
      title: params.get("title") || "",
      courseTitle: params.get("course_title") || "",
    };
  } catch {
    return {
      courseID: parts[0],
      articleEnID: parts[2],
      classEnID: params.get("class_enid") || "",
      title: params.get("title") || "",
      courseTitle: params.get("course_title") || "",
    };
  }
}

function buildDedaoCourseURL(item) {
  const courseID = item?.id || item?.class_id || item?.product_id || "";
  const enid = dedaoProductEnID(item || {});
  const params = new URLSearchParams();
  if (enid) params.set("enid", enid);
  if (item?.publish_num) params.set("total", String(item.publish_num));
  if (item?.title || item?.name) params.set("title", item.title || item.name);
  return courseID ? `${ROUTES.dedaoCourses}/${encodeURIComponent(courseID)}${params.toString() ? `?${params.toString()}` : ""}` : "";
}

function buildDedaoCourseArticleURL(courseID, articleEnID, classEnID = "", title = "", courseTitle = "") {
  if (!courseID || !articleEnID) {
    return "";
  }
  const params = new URLSearchParams();
  if (classEnID) params.set("class_enid", classEnID);
  if (title) params.set("title", title);
  if (courseTitle) params.set("course_title", courseTitle);
  return `${ROUTES.dedaoCourses}/${encodeURIComponent(courseID)}/articles/${encodeURIComponent(articleEnID)}${params.toString() ? `?${params.toString()}` : ""}`;
}

function buildDedaoCourseDetailURL(enid) {
  return enid ? `${ROUTES.dedaoCourses}/detail/${encodeURIComponent(enid)}` : "";
}

function buildDedaoEbookURL(bookID) {
  return bookID ? `${ROUTES.dedaoEbooks}/${encodeURIComponent(bookID)}` : "";
}

function buildBookReaderURL(packageID) {
  return packageID ? `${ROUTES.bookReader}/${encodeURIComponent(packageID)}` : "";
}

function buildKnowledgePackageURL(packageID) {
  return packageID ? `${ROUTES.knowledgePackages}/${encodeURIComponent(packageID)}` : ROUTES.knowledgePackages;
}

function buildAgentPackageURL(packageID, version = "") {
  if (!packageID) {
    return ROUTES.agentPackages;
  }
  const query = version ? `?version=${encodeURIComponent(version)}` : "";
  return `${ROUTES.agentPackages}/${encodeURIComponent(packageID)}${query}`;
}

function buildAgentURL(packageID, version = "") {
  const query = version ? `?version=${encodeURIComponent(version)}` : "";
  return packageID ? `${ROUTES.agents}/${encodeURIComponent(packageID)}${query}` : ROUTES.agents;
}

function buildBookAppURL(packageID, version = "") {
  const query = version ? `?version=${encodeURIComponent(version)}` : "";
  return packageID ? `${ROUTES.bookApps}/${encodeURIComponent(packageID)}${query}` : ROUTES.bookApps;
}

function getBookAgentRoute() {
  const pathname = getRoutePathname();
  const routes = [
    [ROUTES.agentPackages, "package"],
    [ROUTES.agents, "agent"],
    [ROUTES.bookApps, "app"],
  ];
  for (const [base, view] of routes) {
    if (pathname === base) {
      return { view, packageID: "", version: "" };
    }
    if (pathname.startsWith(`${base}/`)) {
      const raw = pathname.slice(base.length + 1).split("/")[0];
      const params = new URLSearchParams(window.location.search);
      try {
        return { view, packageID: decodeURIComponent(raw), version: params.get("version") || "" };
      } catch {
        return { view, packageID: raw, version: params.get("version") || "" };
      }
    }
  }
  return null;
}

async function fetchBook(bookID) {
  return apiFetch(`/api/books/${encodeURIComponent(bookID)}`);
}

function renderShell(content, current = "") {
  app.className = "web-shell";
  app.innerHTML = `
    <header class="web-topbar">
      <a class="web-brand" href="${escapeAttribute(ROUTES.dedaoHome)}">得到 KBase</a>
      <nav class="web-nav" aria-label="主导航">
        <a class="${current === "home" ? "active" : ""}" href="${escapeAttribute(ROUTES.dedaoHome)}">首页</a>
        <a class="${current === "course" ? "active" : ""}" href="${escapeAttribute(ROUTES.dedaoCourses)}">课程</a>
        <a class="${current === "ebook" ? "active" : ""}" href="${escapeAttribute(ROUTES.dedaoEbooks)}">电子书</a>
        <a class="${current === "odob" ? "active" : ""}" href="${escapeAttribute(ROUTES.dedaoAudio)}">听书</a>
        <a class="${current === "wechat" ? "active" : ""}" href="/wechat-source">微信采集</a>
        <a class="${current === "import" ? "active" : ""}" href="/wechat-import">单篇导入</a>
        <a class="${current === "knowledge" ? "active" : ""}" href="${escapeAttribute(ROUTES.knowledgePackages)}">书籍知识库</a>
        <a class="${current === "agents" ? "active" : ""}" href="${escapeAttribute(ROUTES.agentPackages)}">Book Agents</a>
        <a class="${current === "jobs" ? "active" : ""}" href="${escapeAttribute(ROUTES.jobs)}">任务</a>
      </nav>
    </header>
    ${content}
  `;
}

function renderDedaoHome() {
  const sections = dedaoLibraryState.home ? `
    <section class="dedao-home__library" aria-label="得到订阅内容">
      ${renderDedaoHomeSection("订阅课程", dedaoLibraryState.home.courses?.list, ROUTES.dedaoCourses)}
      ${renderDedaoHomeSection("得到电子书", dedaoLibraryState.home.ebooks?.list, ROUTES.dedaoEbooks)}
      ${renderDedaoHomeSection("听书书架", dedaoLibraryState.home.odob?.list, ROUTES.dedaoAudio)}
    </section>
  ` : "";
  renderShell(`
    <main class="dedao-home">
      <section class="dedao-home__hero">
        <div>
          <p class="web-kicker">得到首页</p>
          <h1>把得到内容变成可学习、可验证、可供给的知识库</h1>
          <p>从课程、电子书、听书和公众号来源开始，完成搜索、下载、加工、分析和外部系统供给。</p>
          <div class="web-home__actions">
            <a class="button button-primary" href="${escapeAttribute(ROUTES.dedaoCourses)}">进入得到课程</a>
            <a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoEbooks)}">查看得到电子书</a>
            <a class="button button-ghost" href="${escapeAttribute(ROUTES.knowledgePackages)}">打开书籍知识库</a>
            <a class="button button-ghost" href="/wechat-source">微信采集</a>
          </div>
        </div>
        <div class="dedao-home__panel">
          <strong>今日工作台</strong>
          <span>搜索内容，导入知识库，再用 Token Plan 模型完成结构化分析。</span>
        </div>
      </section>
      <section class="dedao-home__shortcuts" aria-label="得到功能">
        <a class="dedao-card" href="${escapeAttribute(ROUTES.dedaoCourses)}">
          <span>得到课程</span>
          <strong>继续学习</strong>
          <small>查看已订阅课程和学习入口</small>
        </a>
        <a class="dedao-card" href="${escapeAttribute(ROUTES.dedaoEbooks)}">
          <span>得到电子书</span>
          <strong>书架阅读</strong>
          <small>查看已订阅电子书</small>
        </a>
        <a class="dedao-card" href="${escapeAttribute(ROUTES.knowledgePackages)}">
          <span>书籍知识库</span>
          <strong>知识加工</strong>
          <small>检索、分析、发布给外部系统</small>
        </a>
        <a class="dedao-card" href="/wcplus-source">
          <span>公众号</span>
          <strong>采集来源</strong>
          <small>同步文章并导入知识库</small>
        </a>
      </section>
      ${dedaoLibraryState.homeLoading ? `<p class="web-status">正在加载得到订阅内容...</p>` : ""}
      ${dedaoLibraryState.homeMessage ? `<p class="web-status">${escapeHTML(dedaoLibraryState.homeMessage)}</p>` : ""}
      ${sections}
    </main>
  `, "home");
}

function renderHome() {
  renderDedaoHome();
}

const dedaoLibraryConfig = {
  bauhinia: {
    nav: "course",
    path: "/course",
    kicker: "得到课程",
    title: "课程",
    description: "从得到账号读取已订阅课程，继续学习、下载或沉淀到书籍知识库。",
    empty: "暂无已订阅课程，或得到登录 cookie 已失效。",
    primaryAction: "继续学习",
  },
  ebook: {
    nav: "ebook",
    path: ROUTES.dedaoEbooks,
    kicker: "得到电子书",
    title: "电子书",
    description: "从得到账号读取已订阅电子书，进入阅读、下载或同步到书籍知识库。",
    empty: "暂无已订阅电子书，或得到登录 cookie 已失效。",
    primaryAction: "阅读",
  },
  odob: {
    nav: "odob",
    path: ROUTES.dedaoAudio,
    kicker: "听书书架",
    title: "听书",
    description: "从得到账号读取已订阅听书内容，查看文稿并沉淀成知识资料。",
    empty: "暂无已订阅听书内容，或得到登录 cookie 已失效。",
    primaryAction: "查看",
  },
};

function dedaoProductID(item, category) {
  if (category === "bauhinia") {
    return String(item.class_id || item.id || "").trim();
  }
  return String(item.id || item.class_id || "").trim();
}

function dedaoProductEnID(item) {
  return String(item.enid || item.en_id || "").trim();
}

function renderDedaoHomeSection(title, items, href) {
  const rows = (Array.isArray(items) ? items : []).slice(0, 4).map((item) => `
    <a class="dedao-mini-card" href="${escapeAttribute(href)}">
      ${item.icon ? `<img src="${escapeAttribute(item.icon)}" alt="">` : "<span></span>"}
      <strong>${escapeHTML(item.title || "未命名")}</strong>
      <small>${escapeHTML(item.author || item.intro || "得到订阅内容")}</small>
    </a>
  `).join("");
  return `
    <section>
      <div class="dedao-home__section-head">
        <h2>${escapeHTML(title)}</h2>
        <a href="${escapeAttribute(href)}">查看全部</a>
      </div>
      <div class="dedao-home__mini-grid">${rows || "<p class=\"web-muted\">暂无内容</p>"}</div>
    </section>
  `;
}

function renderDedaoCourses() {
  renderDedaoLibrary("bauhinia");
}

function renderDedaoEbooks() {
  renderDedaoLibrary("ebook");
}

function renderDedaoOdob() {
  renderDedaoLibrary("odob");
}

function renderDedaoLibrary(category) {
  const cfg = dedaoLibraryConfig[category] || dedaoLibraryConfig.bauhinia;
  const state = dedaoLibraryState.pages[category] || dedaoLibraryState.pages.bauhinia;
  const cards = state.items.map((item) => {
    const id = dedaoProductID(item, category);
    const enid = dedaoProductEnID(item);
    const progress = Number.isFinite(Number(item.progress)) ? Number(item.progress) : 0;
    const total = item.course_num || item.publish_num || item.duration || "-";
    const updated = item.publish_num ? `${item.publish_num}/${item.course_num || "?"}` : (item.last_read || "-");
    const primaryHref = category === "ebook" && enid
      ? buildDedaoEbookURL(enid)
      : (category === "bauhinia" ? buildDedaoCourseURL(item) : "");
    const detailHref = category === "bauhinia" && enid
      ? buildDedaoCourseDetailURL(enid)
      : (enid ? `${cfg.path}/${encodeURIComponent(enid)}` : "");
    return `
      <article class="dedao-course-card">
        <div class="dedao-course-card__top">
          ${item.icon ? `<img src="${escapeAttribute(item.icon)}" alt="">` : "<div></div>"}
          <div>
            <p class="web-kicker">${escapeHTML(cfg.kicker)}</p>
            <h2>${escapeHTML(item.title || id || "未命名")}</h2>
            <p>${escapeHTML(item.author || item.intro || "得到订阅内容")}</p>
          </div>
        </div>
        <div class="dedao-progress" aria-label="进度">
          <span style="width:${Math.max(0, Math.min(100, progress))}%"></span>
        </div>
        <dl>
          <div><dt>进度</dt><dd>${escapeHTML(progress ? `${progress}%` : "-")}</dd></div>
          <div><dt>更新</dt><dd>${escapeHTML(updated || total)}</dd></div>
        </dl>
        <div class="dedao-course-card__actions">
          ${primaryHref ? `<a class="button button-primary" href="${escapeAttribute(primaryHref)}">${escapeHTML(cfg.primaryAction)}</a>` : ""}
          ${detailHref ? `<a class="button button-ghost" href="${escapeAttribute(detailHref)}">详情</a>` : ""}
          ${id || enid ? `<a class="button button-ghost" href="${escapeAttribute(`${ROUTES.knowledgePackages}?query=${encodeURIComponent(item.title || id || enid)}`)}">查知识库</a>` : ""}
        </div>
      </article>
    `;
  }).join("");

  renderShell(`
    <main class="dedao-courses">
      <section class="dedao-courses__header">
        <div>
          <p class="web-kicker">${escapeHTML(cfg.kicker)}</p>
          <h1>${escapeHTML(cfg.title)}</h1>
          <p>${escapeHTML(cfg.description)}</p>
        </div>
        <div class="dedao-courses__actions">
          <button class="button button-primary" type="button" data-action="reload-dedao-library" ${state.loading ? "disabled" : ""}>
            ${state.loading ? "加载中" : "刷新"}
          </button>
          <a class="button button-ghost" href="${escapeAttribute(ROUTES.knowledgePackages)}">书籍知识库</a>
        </div>
      </section>
      ${state.message ? `<p class="web-status">${escapeHTML(state.message)}</p>` : ""}
      <section class="dedao-courses__grid">
        ${cards || `
          <div class="dedao-courses__empty">
            <h2>${escapeHTML(cfg.empty)}</h2>
            <p>确认得到扫码登录成功后，刷新本页；已下载加工过的内容仍可在书籍知识库查看。</p>
            <div class="web-home__actions">
              <a class="button button-primary" href="${escapeAttribute(ROUTES.knowledgePackages)}">打开书籍知识库</a>
              <a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoHome)}">返回首页</a>
            </div>
          </div>
        `}
      </section>
    </main>
  `, cfg.nav);

  app.querySelector("[data-action='reload-dedao-library']")?.addEventListener("click", () => loadDedaoLibrary(category));
}

async function loadDedaoHome() {
  dedaoLibraryState.homeLoading = "loading";
  dedaoLibraryState.homeMessage = "";
  renderDedaoHome();
  try {
    dedaoLibraryState.home = await apiFetch("/api/dedao/home?page_size=4");
  } catch (error) {
    dedaoLibraryState.homeMessage = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoLibraryState.homeLoading = "";
    renderDedaoHome();
  }
}

async function loadDedaoCourses() {
  return loadDedaoLibrary("bauhinia");
}

async function loadDedaoLibrary(category) {
  const cfg = dedaoLibraryConfig[category] || dedaoLibraryConfig.bauhinia;
  const state = dedaoLibraryState.pages[category] || dedaoLibraryState.pages.bauhinia;
  state.loading = "loading";
  state.message = "";
  renderDedaoLibrary(category);
  try {
    const query = new URLSearchParams({
      category,
      order: "study",
      page: String(state.page || 1),
      page_size: String(state.pageSize || 15),
    });
    const payload = await apiFetch(`/api/dedao/library?${query.toString()}`);
    state.items = Array.isArray(payload?.list) ? payload.list : [];
    state.isMore = Number(payload?.is_more || 0);
    state.message = state.items.length ? `已加载 ${state.items.length} 条${cfg.title}` : cfg.empty;
  } catch (error) {
    state.message = error instanceof Error ? error.message : String(error);
  } finally {
    state.loading = "";
    renderDedaoLibrary(category);
  }
}

function findKnowledgePackageForEbook(item, books) {
  const sourceID = dedaoProductID(item || {}, "ebook");
  const sourceEnID = dedaoProductEnID(item || {});
  const title = String(item?.title || "").trim();
  return (Array.isArray(books) ? books : []).find((book) => {
    const dedaoID = String(book?.dedao_id || "").trim();
    const bookEnID = String(book?.enid || "").trim();
    const sourceKey = String(book?.source_key || "").trim();
    const bookTitle = String(book?.title || "").trim();
    return Boolean(
      (sourceID && dedaoID === sourceID)
      || (sourceEnID && (bookEnID === sourceEnID || sourceKey === sourceEnID))
      || (title && bookTitle === title),
    );
  }) || null;
}

function renderDedaoEbookDetail(route) {
  const item = dedaoLibraryState.ebookDetail;
  const pkg = dedaoLibraryState.ebookPackage;
  const sourceID = dedaoProductID(item || {}, "ebook");
  const sourceEnID = dedaoProductEnID(item || {}) || route?.enid || "";
  const title = item?.title || "得到电子书";
  const progress = Number.isFinite(Number(item?.progress)) ? Number(item.progress) : 0;
  const packageID = String(pkg?.book_id || "").trim();
  const packageURL = packageID ? buildKnowledgePackageURL(packageID) : `${ROUTES.knowledgePackages}?query=${encodeURIComponent(title || sourceID || sourceEnID)}`;
  const readerURL = packageID ? buildBookReaderURL(packageID) : "";

  renderShell(`
    <main class="dedao-ebook-detail">
      <section class="dedao-ebook-detail__hero">
        <a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoEbooks)}">返回电子书</a>
        <div class="dedao-ebook-detail__cover">
          ${item?.icon ? `<img src="${escapeAttribute(item.icon)}" alt="${escapeAttribute(title)}封面">` : "<span>书</span>"}
        </div>
        <div class="dedao-ebook-detail__summary">
          <p class="web-kicker">得到电子书来源</p>
          <h1>${escapeHTML(title)}</h1>
          <p class="dedao-ebook-detail__author">${escapeHTML(item?.author || "作者信息暂缺")}</p>
          <p>${escapeHTML(item?.intro || dedaoLibraryState.ebookDetailMessage || "正在读取电子书信息。")}</p>
          <div class="dedao-progress" aria-label="阅读进度"><span style="width:${Math.max(0, Math.min(100, progress))}%"></span></div>
          <div class="dedao-ebook-detail__actions">
            ${readerURL ? `<a class="button button-primary" href="${escapeAttribute(readerURL)}">阅读知识包</a>` : ""}
            <a class="button ${packageID ? "button-ghost" : "button-primary"}" href="${escapeAttribute(packageURL)}">${packageID ? "打开知识包" : "检查并创建知识包"}</a>
          </div>
        </div>
      </section>

      ${dedaoLibraryState.ebookDetailLoading ? `<p class="web-status">正在加载电子书详情...</p>` : ""}
      ${dedaoLibraryState.ebookDetailMessage ? `<p class="web-status">${escapeHTML(dedaoLibraryState.ebookDetailMessage)}</p>` : ""}

      <section class="dedao-ebook-detail__body">
        <div class="dedao-ebook-detail__facts">
          <p class="web-kicker">来源信息</p>
          <dl>
            <div><dt>得到 ID</dt><dd>${escapeHTML(sourceID || "-")}</dd></div>
            <div><dt>来源 EnID</dt><dd>${escapeHTML(sourceEnID || "-")}</dd></div>
            <div><dt>阅读进度</dt><dd>${escapeHTML(progress ? `${progress}%` : "未开始")}</dd></div>
            <div><dt>知识包 ID</dt><dd>${escapeHTML(packageID || "尚未生成")}</dd></div>
          </dl>
        </div>
        <div class="dedao-ebook-detail__lifecycle">
          <div class="dedao-home__section-head">
            <div>
              <p class="web-kicker">Book Lifecycle</p>
              <h2>从一本书到一个 Agent</h2>
            </div>
          </div>
          <ol>
            <li class="is-ready"><span>1</span><div><strong>来源书</strong><small>元数据已连接，可稳定传播</small></div><b>已就绪</b></li>
            <li class="${packageID ? "is-ready" : "is-pending"}"><span>2</span><div><strong>知识包</strong><small>章节、chunks、claims 与引用</small></div><b>${packageID ? "已生成" : "待生成"}</b></li>
            <li class="is-pending"><span>3</span><div><strong>书籍 Agent</strong><small>绑定知识包、模型策略和评测</small></div><b>待接入</b></li>
            <li class="is-pending"><span>4</span><div><strong>独立应用</strong><small>基于同一 Agent 的专属学习软件</small></div><b>待发布</b></li>
          </ol>
        </div>
      </section>
    </main>
  `, "ebook");
}

async function loadDedaoEbookDetail(route) {
  dedaoLibraryState.ebookDetail = null;
  dedaoLibraryState.ebookPackage = null;
  dedaoLibraryState.ebookDetailLoading = "loading";
  dedaoLibraryState.ebookDetailMessage = "";
  renderDedaoEbookDetail(route);
  try {
    let matched = null;
    for (let page = 1; page <= 10 && !matched; page += 1) {
      const query = new URLSearchParams({
        category: "ebook",
        order: "study",
        page: String(page),
        page_size: "100",
      });
      const payload = await apiFetch(`/api/dedao/library?${query.toString()}`);
      const items = Array.isArray(payload?.list) ? payload.list : [];
      matched = items.find((item) => dedaoProductEnID(item) === route.enid || dedaoProductID(item, "ebook") === route.enid) || null;
      if (!Number(payload?.is_more)) {
        break;
      }
    }
    if (!matched) {
      throw new Error("未在当前得到书架中找到这本电子书，请刷新书架或重新登录得到账号。");
    }
    dedaoLibraryState.ebookDetail = matched;
    const booksPayload = await apiFetch("/api/books");
    const books = Array.isArray(booksPayload?.books) ? booksPayload.books : (Array.isArray(booksPayload) ? booksPayload : []);
    dedaoLibraryState.ebookPackage = findKnowledgePackageForEbook(matched, books);
  } catch (error) {
    dedaoLibraryState.ebookDetailMessage = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoLibraryState.ebookDetailLoading = "";
    renderDedaoEbookDetail(route);
  }
}

function renderDedaoCourseDetail() {
  const detail = dedaoLibraryState.courseDetail;
  const info = detail?.class_info || {};
  const articles = Array.isArray(detail?.flat_article_list) ? detail.flat_article_list : [];
  const articleListError = detail?.article_list_error || "";
  const hasMore = Boolean(detail?.has_more_flat_article_list) || Number(info.current_article_count || info.phase_num || 0) > articles.length;
  const articleRows = articles.map((article, index) => `
    <article class="dedao-article-row">
      <span>${index + 1}</span>
      <div>
        <strong>${escapeHTML(article.title || article.share_title || "未命名文章")}</strong>
        <small>${escapeHTML(formatArticleTime(article.publish_time || article.update_time || article.create_time))}</small>
      </div>
      ${article.enid ? `<a class="button button-ghost" href="${escapeAttribute(buildDedaoCourseArticleURL(info.id || article.class_id || "", article.enid, info.enid || getDedaoCourseDetailEnID(), article.title || article.share_title || "", info.name || ""))}">打开</a>` : ""}
    </article>
  `).join("");

  renderShell(`
    <main class="dedao-course-detail">
      <section class="dedao-course-detail__header">
        <a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoCourses)}">返回课程</a>
        <div>
          <p class="web-kicker">得到课程详情</p>
          <h1>${escapeHTML(info.name || "课程详情")}</h1>
          <p>${escapeHTML(info.intro || dedaoLibraryState.courseDetailMessage || "正在读取课程详情。")}</p>
        </div>
      </section>
      ${dedaoLibraryState.courseDetailLoading ? `<p class="web-status">正在加载课程详情...</p>` : ""}
      ${dedaoLibraryState.courseDetailMessage ? `<p class="web-status">${escapeHTML(dedaoLibraryState.courseDetailMessage)}</p>` : ""}
      <section class="dedao-course-detail__layout">
        <aside class="dedao-course-detail__aside">
          <dl>
            <div><dt>讲师</dt><dd>${escapeHTML(info.lecturer_name || "-")}</dd></div>
            <div><dt>更新</dt><dd>${escapeHTML(info.current_article_count || articles.length || "-")}/${escapeHTML(info.phase_num || "-")}</dd></div>
            <div><dt>学习人数</dt><dd>${escapeHTML(info.learn_user_count || "-")}</dd></div>
          </dl>
          <a class="button button-primary" href="${escapeAttribute(`${ROUTES.knowledgePackages}?query=${encodeURIComponent(info.name || "")}`)}">在知识库中检索</a>
        </aside>
        <section class="dedao-course-detail__articles">
          <div class="dedao-home__section-head">
            <h2>课程目录</h2>
            <span>${articles.length} 篇</span>
          </div>
          ${articleListError ? `<p class="web-status">课程目录暂时不可用：${escapeHTML(articleListError)}</p>` : ""}
          ${articleRows || "<p class=\"web-muted\">暂无课程文章。</p>"}
          ${hasMore ? `<button class="button button-ghost" type="button" data-action="load-more-course-articles" ${dedaoLibraryState.courseArticlesLoadingMore ? "disabled" : ""}>${dedaoLibraryState.courseArticlesLoadingMore ? "加载中" : "加载更多"}</button>` : ""}
        </section>
      </section>
    </main>
  `, "course");

  app.querySelector("[data-action='load-more-course-articles']")?.addEventListener("click", () => {
    const route = {
      id: String(info.id || ""),
      enid: info.enid || getDedaoCourseDetailEnID(),
      title: info.name || "",
      total: String(info.current_article_count || info.phase_num || ""),
    };
    loadMoreDedaoCourseArticles(route);
  });
}

function renderDedaoCourseArticles(route = getDedaoCourseRoute()) {
  const detail = dedaoLibraryState.courseDetail;
  const info = detail?.class_info || {};
  const articles = Array.isArray(detail?.flat_article_list) ? detail.flat_article_list : [];
  const articleListError = detail?.article_list_error || "";
  const title = info.name || route?.title || "课程目录";
  const hasMore = Boolean(detail?.has_more_flat_article_list) || Number(info.current_article_count || info.phase_num || route?.total || 0) > articles.length;
  const articleRows = articles.map((article, index) => `
    <article class="dedao-article-row">
      <span>${index + 1}</span>
      <div>
        <strong>${escapeHTML(article.title || article.share_title || "未命名文章")}</strong>
        <small>${escapeHTML(formatArticleTime(article.publish_time || article.update_time || article.create_time))}</small>
      </div>
      ${article.enid ? `<a class="button button-ghost" href="${escapeAttribute(buildDedaoCourseArticleURL(route?.id || info.id || article.class_id || "", article.enid, route?.enid || info.enid || "", article.title || article.share_title || "", title))}">打开</a>` : ""}
    </article>
  `).join("");

  renderShell(`
    <main class="dedao-course-detail">
      <section class="dedao-course-detail__header">
        <a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoCourses)}">返回课程</a>
        <div>
          <p class="web-kicker">得到课程目录</p>
          <h1>${escapeHTML(title)}</h1>
          <p>${escapeHTML(info.intro || "按桌面版课程入口打开课程目录。")}</p>
        </div>
      </section>
      ${dedaoLibraryState.courseDetailLoading ? `<p class="web-status">正在加载课程目录...</p>` : ""}
      ${dedaoLibraryState.courseDetailMessage ? `<p class="web-status">${escapeHTML(dedaoLibraryState.courseDetailMessage)}</p>` : ""}
      <section class="dedao-course-detail__layout">
        <aside class="dedao-course-detail__aside">
          <dl>
            <div><dt>课程 ID</dt><dd>${escapeHTML(route?.id || info.id || "-")}</dd></div>
            <div><dt>EnID</dt><dd>${escapeHTML(route?.enid || info.enid || "-")}</dd></div>
            <div><dt>目录</dt><dd>${escapeHTML(articles.length || route?.total || "-")} 篇</dd></div>
          </dl>
          ${route?.enid || info.enid ? `<a class="button button-ghost" href="${escapeAttribute(buildDedaoCourseDetailURL(route?.enid || info.enid))}">课程详情</a>` : ""}
        </aside>
        <section class="dedao-course-detail__articles">
          <div class="dedao-home__section-head">
            <h2>课程目录</h2>
            <span>${articles.length || route?.total || 0} 篇</span>
          </div>
          ${articleListError ? `<p class="web-status">课程目录暂时不可用：${escapeHTML(articleListError)}</p>` : ""}
          ${articleRows || "<p class=\"web-muted\">暂无课程文章。</p>"}
          ${hasMore ? `<button class="button button-ghost" type="button" data-action="load-more-course-articles" ${dedaoLibraryState.courseArticlesLoadingMore ? "disabled" : ""}>${dedaoLibraryState.courseArticlesLoadingMore ? "加载中" : "加载更多"}</button>` : ""}
        </section>
      </section>
    </main>
  `, "course");

  app.querySelector("[data-action='load-more-course-articles']")?.addEventListener("click", () => loadMoreDedaoCourseArticles(route));
}

async function loadDedaoCourseDetail(enid) {
  dedaoLibraryState.courseDetailLoading = "loading";
  dedaoLibraryState.courseDetailMessage = "";
  dedaoLibraryState.courseDetail = null;
  renderDedaoCourseDetail();
  try {
    dedaoLibraryState.courseDetail = await apiFetch(`/api/dedao/course?enid=${encodeURIComponent(enid)}`);
  } catch (error) {
    dedaoLibraryState.courseDetailMessage = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoLibraryState.courseDetailLoading = "";
    renderDedaoCourseDetail();
  }
}

async function loadDedaoCourseArticles(route) {
  dedaoLibraryState.courseDetailLoading = "loading";
  dedaoLibraryState.courseDetailMessage = "";
  dedaoLibraryState.courseDetail = null;
  renderDedaoCourseArticles(route);
  try {
    if (!route?.enid) {
      throw new Error("课程链接缺少 enid，无法加载目录。请从课程列表重新进入。");
    }
    dedaoLibraryState.courseDetail = await apiFetch(`/api/dedao/course?enid=${encodeURIComponent(route.enid)}`);
  } catch (error) {
    dedaoLibraryState.courseDetailMessage = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoLibraryState.courseDetailLoading = "";
    renderDedaoCourseArticles(route);
  }
}

async function loadMoreDedaoCourseArticles(route = getDedaoCourseRoute()) {
  const detail = dedaoLibraryState.courseDetail;
  const articles = Array.isArray(detail?.flat_article_list) ? detail.flat_article_list : [];
  const lastID = articles.length ? Number(articles[articles.length - 1]?.id || 0) : 0;
  const enid = route?.enid || detail?.class_info?.enid || "";
  if (!enid) {
    dedaoLibraryState.courseDetailMessage = "课程链接缺少 enid，无法继续加载目录。";
    renderDedaoCourseArticles(route);
    return;
  }
  dedaoLibraryState.courseArticlesLoadingMore = "loading";
  renderDedaoCourseArticles(route);
  try {
    const query = new URLSearchParams({
      enid,
      count: "30",
      max_id: String(lastID || 0),
    });
    const payload = await apiFetch(`/api/dedao/course/articles?${query.toString()}`);
    const nextArticles = Array.isArray(payload?.article_list) ? payload.article_list.map((article) => article.article_base || article) : [];
    const seen = new Set(articles.map((article) => String(article.id || article.enid || "")));
    const merged = articles.concat(nextArticles.filter((article) => {
      const key = String(article.id || article.enid || "");
      if (!key || seen.has(key)) {
        return false;
      }
      seen.add(key);
      return true;
    }));
    dedaoLibraryState.courseDetail = {
      ...(detail || {}),
      flat_article_list: merged,
      has_more_flat_article_list: nextArticles.length >= 30,
    };
  } catch (error) {
    dedaoLibraryState.courseDetailMessage = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoLibraryState.courseArticlesLoadingMore = "";
    renderDedaoCourseArticles(route);
  }
}

function renderDedaoCourseArticle(route = getDedaoCourseArticleRoute()) {
  const payload = dedaoLibraryState.courseArticle || {};
  const markdown = payload.markdown || "";
  const title = route?.title || payload.detail?.article?.Title || "课程正文";
  const analysisPrompt = dedaoLibraryState.courseArticleAnalysisPrompt || "请分析当前课程文章的核心论点、关键证据、适用边界和可执行启发。";
  const analysisResponse = dedaoLibraryState.courseArticleAnalysisResponse || {};
  const analysisStats = analysisResponse.context_stats
    ? `${analysisResponse.context_stats.chunks || 0} 段 · ${analysisResponse.context_stats.chars || 0} 字上下文`
    : "";
  renderShell(`
    <main class="dedao-course-article">
      <section class="dedao-course-detail__header">
        <a class="button button-ghost" href="${escapeAttribute(`${ROUTES.dedaoCourses}/${encodeURIComponent(route?.courseID || "")}${route?.classEnID ? `?enid=${encodeURIComponent(route.classEnID)}&title=${encodeURIComponent(route.courseTitle || "")}` : ""}`)}">返回目录</a>
        <div>
          <p class="web-kicker">课程正文</p>
          <h1>${escapeHTML(title)}</h1>
          <p>${escapeHTML(route?.courseTitle || "得到课程文章")}</p>
        </div>
      </section>
      ${dedaoLibraryState.courseArticleLoading ? `<p class="web-status">正在加载课程正文...</p>` : ""}
      ${dedaoLibraryState.courseArticleMessage ? `<p class="web-status">${escapeHTML(dedaoLibraryState.courseArticleMessage)}</p>` : ""}
      <article class="knowledge-web__answer dedao-course-article__body">
        ${markdown ? renderCourseMarkdown(markdown) : "<p>暂无正文。</p>"}
      </article>
      <section class="knowledge-web__analysis dedao-course-article__analysis" aria-label="TokenPlan 文章分析">
        <div class="knowledge-web__analysis-head">
          <div>
            <p class="web-kicker">TokenPlan</p>
            <h3>分析当前文章</h3>
          </div>
          <select id="course-article-analysis-model" aria-label="模型">
            ${knowledgeAnalysisModels.map((model) => `
              <option value="${escapeAttribute(model.id)}" ${dedaoLibraryState.courseArticleAnalysisModel === model.id ? "selected" : ""}>${escapeHTML(model.label)}</option>
            `).join("")}
          </select>
        </div>
        <div class="knowledge-web__prompt-grid">
          ${knowledgeAnalysisPrompts.map(([key, label, prompt]) => `
            <button class="button button-ghost" type="button" data-course-article-prompt="${escapeAttribute(key)}" data-prompt="${escapeAttribute(prompt)}">${escapeHTML(label)}</button>
          `).join("")}
        </div>
        <form id="course-article-analysis-form" class="knowledge-web__analysis-form">
          <textarea name="question" rows="5" placeholder="围绕当前课程文章提问，或点击上方模板">${escapeHTML(analysisPrompt)}</textarea>
          <div class="knowledge-web__analysis-actions">
            <span>${escapeHTML(analysisStats || dedaoLibraryState.courseArticleAnalysisError || dedaoLibraryState.courseArticleAnalysisLoading || "会基于当前课程文章正文回答。")}</span>
            <button class="button button-primary" type="submit">${dedaoLibraryState.courseArticleAnalysisLoading ? "分析中" : "发送给 TokenPlan"}</button>
          </div>
        </form>
        ${analysisResponse.answer ? `
          <article class="knowledge-web__answer">
            <div class="web-kicker">${escapeHTML(knowledgeModelLabel(analysisResponse.model || dedaoLibraryState.courseArticleAnalysisModel))}</div>
            ${renderSimpleMarkdown(analysisResponse.answer)}
          </article>
        ` : ""}
      </section>
    </main>
  `, "course");
  bindDedaoCourseArticleAnalysis(route);
}

async function loadDedaoCourseArticle(route) {
  const routeKey = route?.articleEnID || "";
  if (routeKey && routeKey !== dedaoLibraryState.courseArticleAnalysisKey) {
    resetDedaoCourseArticleAnalysis(routeKey);
  }
  dedaoLibraryState.courseArticleLoading = "loading";
  dedaoLibraryState.courseArticleMessage = "";
  dedaoLibraryState.courseArticle = null;
  renderDedaoCourseArticle(route);
  try {
    if (!route?.articleEnID) {
      throw new Error("课程文章链接缺少 enid。");
    }
    dedaoLibraryState.courseArticle = await apiFetch(`/api/dedao/article?enid=${encodeURIComponent(route.articleEnID)}`);
  } catch (error) {
    dedaoLibraryState.courseArticleMessage = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoLibraryState.courseArticleLoading = "";
    renderDedaoCourseArticle(route);
  }
}

function resetDedaoCourseArticleAnalysis(key = "") {
  dedaoLibraryState.courseArticleAnalysisPrompt = "";
  dedaoLibraryState.courseArticleAnalysisResponse = null;
  dedaoLibraryState.courseArticleAnalysisLoading = "";
  dedaoLibraryState.courseArticleAnalysisError = "";
  dedaoLibraryState.courseArticleAnalysisKey = key;
}

function jobStatusLabel(status) {
  return ({
    queued: "排队中",
    pending: "等待中",
    running: "运行中",
    processing: "处理中",
    ready: "已就绪",
    succeeded: "已完成",
    success: "已完成",
    completed: "已完成",
    failed: "失败",
    error: "失败",
    canceled: "已取消",
  })[String(status || "").toLowerCase()] || status || "未知";
}

function jobStatusClass(status) {
  const value = String(status || "unknown").toLowerCase().replace(/[^a-z0-9_-]/g, "");
  return `is-${value || "unknown"}`;
}

function normalizeJobTask(task, source = "wcplus") {
  const taskID = String(task?.task_id || task?.id || task?.biz || task?.nickname || "").trim();
  const progress = [];
  if (task?.article_total) {
    progress.push(`正文 ${task.article_finished || 0}/${task.article_total}`);
  }
  if (task?.reading_total) {
    progress.push(`阅读 ${task.reading_finished || 0}/${task.reading_total}`);
  }
  return {
    id: taskID || `${source}-${Math.random().toString(36).slice(2)}`,
    source,
    title: task?.nickname || task?.biz || taskID || "未命名任务",
    operation: task?.crawler_type || task?.operation || source,
    status: task?.status || "unknown",
    progress: progress.join(" · "),
    error: task?.status_error || task?.error || task?.message || "",
    updatedAt: task?.updated_at || task?.update_time || task?.created_at || "",
    sourceURL: "/wcplus-source",
    raw: task || {},
  };
}

function jobCenterErrorMessage(error) {
  const message = error instanceof Error ? error.message : String(error);
  if (/connect: connection refused|dial tcp|127\.0\.0\.1|localhost/i.test(message)) {
    return "WC Plus 服务暂不可用。请到来源控制页检查本地 Agent 或服务连接状态。";
  }
  return message;
}

function renderJobCenter() {
  const tasks = Array.isArray(jobCenterState.tasks) ? jobCenterState.tasks : [];
  const rows = tasks.map((task) => `
    <article class="job-card ${escapeAttribute(jobStatusClass(task.status))}">
      <div class="job-card__main">
        <span class="job-card__source">${escapeHTML(task.source)}</span>
        <h2>${escapeHTML(task.title)}</h2>
        <p>${escapeHTML([task.operation, task.progress].filter(Boolean).join(" · ") || "暂无进度")}</p>
        ${task.error ? `<small class="job-card__error">${escapeHTML(task.error)}</small>` : ""}
      </div>
      <div class="job-card__meta">
        <span class="job-card__status ${escapeAttribute(jobStatusClass(task.status))}">${escapeHTML(jobStatusLabel(task.status))}</span>
        ${task.updatedAt ? `<small>${escapeHTML(task.updatedAt)}</small>` : ""}
        <a class="button button-ghost" href="${escapeAttribute(task.sourceURL)}">打开来源</a>
      </div>
    </article>
  `).join("");

  renderShell(`
    <main class="job-center">
      <section class="job-center__toolbar">
        <div>
          <p class="web-kicker">Jobs</p>
          <h1>任务中心</h1>
          <p>统一查看采集、下载、入库、分析和供给任务。当前已接入 WC Plus 下载任务。</p>
        </div>
        <button class="button button-primary" type="button" data-action="reload-job-center" ${jobCenterState.loading ? "disabled" : ""}>
          ${jobCenterState.loading ? "加载中" : "刷新任务"}
        </button>
      </section>
      ${jobCenterState.message ? `<p class="web-status">${escapeHTML(jobCenterState.message)}</p>` : ""}
      ${jobCenterState.lastUpdated ? `<p class="web-muted">最后更新：${escapeHTML(jobCenterState.lastUpdated)}</p>` : ""}
      <section class="job-center__grid">
        ${rows || "<p class=\"web-muted\">暂无任务。先从来源控制或得到内容页创建下载、同步或入库任务。</p>"}
      </section>
    </main>
  `, "jobs");

  app.querySelector("[data-action='reload-job-center']")?.addEventListener("click", () => loadJobCenter());
}

async function loadJobCenter() {
  jobCenterState.loading = "loading";
  jobCenterState.message = "";
  renderJobCenter();
  try {
    const payload = await apiFetch("/api/wcplus/task/all");
    const wcplusTasks = Array.isArray(payload.tasks) ? payload.tasks.map((task) => normalizeJobTask(task, "wcplus")) : [];
    jobCenterState.tasks = wcplusTasks;
    jobCenterState.lastUpdated = new Date().toLocaleString("zh-CN");
    jobCenterState.message = wcplusTasks.length ? `已加载 ${wcplusTasks.length} 个任务。` : "暂无 WC Plus 任务。";
  } catch (error) {
    jobCenterState.message = jobCenterErrorMessage(error);
  } finally {
    jobCenterState.loading = "";
    renderJobCenter();
  }
}

function knowledgeReviewLatestTask() {
  const tasks = Array.isArray(knowledgeState.reverificationTasks) ? knowledgeState.reverificationTasks : [];
  return tasks[tasks.length - 1] || null;
}

function knowledgeReviewStatus() {
  const task = knowledgeReviewLatestTask();
  if (task?.status) {
    return task.status;
  }
  if (knowledgeState.selectedRelease) {
    return "healthy";
  }
  return "unpublished";
}

function knowledgeReviewStatusLabel(status) {
  return ({
    queued: "等待复核",
    running: "复核中",
    candidate_ready: "候选待发布",
    failed: "复核失败",
    published: "已发布",
    healthy: "已发布 · 无待复核",
    unpublished: "尚未发布",
  })[status] || status || "未知";
}

function knowledgeHash(value) {
  const clean = String(value || "").trim();
  return clean ? clean.slice(0, 12) : "-";
}

function renderKnowledgeReview() {
  const task = knowledgeReviewLatestTask();
  const status = knowledgeReviewStatus();
  const release = knowledgeState.releaseDetail || knowledgeState.selectedRelease || {};
  const assessment = knowledgeState.feedbackAssessment || {};
  const quality = knowledgeState.qualityReport || {};
  const rules = Array.isArray(quality.rules) ? quality.rules : [];
  const triggers = Array.isArray(task?.trigger_outcomes) ? task.trigger_outcomes : [];
  const canRetry = task?.status === "failed";
  const canPublish = task?.status === "candidate_ready"
    && task.quality_decision === "pass"
    && quality.decision === "pass";
  const busy = Boolean(knowledgeState.reviewOperation);
  const summary = knowledgeState.reviewLoading
    || knowledgeState.reviewError
    || (task ? `${triggers.join(" / ") || "反馈触发"} · 尝试 ${task.attempts || 0}` : (release.release_id ? `版本 ${knowledgeHash(release.release_id)}` : "完成基线分析与质量校验后可发布"));
  const ruleRows = rules.map((rule) => `
    <li class="${rule.passed ? "is-pass" : "is-fail"}">
      <span>${rule.passed ? "通过" : "未通过"}</span>
      <strong>${escapeHTML(rule.id || "quality_rule")}</strong>
      <small>${escapeHTML(rule.message || "-")}</small>
    </li>
  `).join("");

  return `
    <section class="knowledge-review is-${escapeAttribute(status)}" aria-label="复核与发布">
      <div class="knowledge-review__summary">
        <div>
          <p class="web-kicker">Reverification</p>
          <h3>复核与发布</h3>
          <p>${escapeHTML(summary)}</p>
        </div>
        <div class="knowledge-review__summary-actions">
          <span class="knowledge-review__status is-${escapeAttribute(status)}">${escapeHTML(knowledgeReviewStatusLabel(status))}</span>
          <button id="knowledge-review-toggle" class="button button-ghost" type="button" aria-expanded="${knowledgeState.reviewOpen}">${knowledgeState.reviewOpen ? "收起" : "详情"}</button>
        </div>
      </div>
      ${knowledgeState.reviewOpen ? `
        <div class="knowledge-review__body">
          ${knowledgeState.reviewError ? `<p class="knowledge-review__error">${escapeHTML(knowledgeState.reviewError)}</p>` : ""}
          <section class="knowledge-review__evidence" aria-label="候选差异">
            <div>
              <p class="web-kicker">Published</p>
              <h4>已发布版本</h4>
              <dl>
                <div><dt>Release</dt><dd>${escapeHTML(knowledgeHash(release.release_id))}</dd></div>
                <div><dt>内容</dt><dd>${escapeHTML(knowledgeHash(release.content_hash))}</dd></div>
                <div><dt>分析</dt><dd>${escapeHTML(knowledgeHash(release.quality?.analysis_hash))}</dd></div>
                <div><dt>时间</dt><dd>${escapeHTML(formatSourceControlTime(release.created_at))}</dd></div>
              </dl>
            </div>
            <div>
              <p class="web-kicker">Candidate</p>
              <h4>候选差异</h4>
              <dl>
                <div><dt>内容</dt><dd>${escapeHTML(knowledgeHash(task?.candidate_content_hash || quality.content_hash))}</dd></div>
                <div><dt>分析</dt><dd>${escapeHTML(knowledgeHash(task?.candidate_analysis_hash || quality.analysis_hash))}</dd></div>
                <div><dt>内容变化</dt><dd>${task?.content_changed ? "有变化" : "无变化"}</dd></div>
                <div><dt>策略</dt><dd>${escapeHTML(quality.usage_policy || release.usage_policy || "-")}</dd></div>
              </dl>
            </div>
          </section>
          <section class="knowledge-review__rules" aria-label="质量规则">
            <div class="knowledge-review__section-head">
              <div><p class="web-kicker">Quality Gate</p><h4>质量规则</h4></div>
              <span>${escapeHTML(quality.decision || "未评估")}</span>
            </div>
            <ul>${ruleRows || "<li><small>暂无质量报告。</small></li>"}</ul>
          </section>
          <div class="knowledge-review__actions">
            <span>${escapeHTML(knowledgeState.reviewOperation || task?.error_code || (assessment.reverify_required ? "等待人工处理" : "当前发布状态稳定"))}</span>
            ${canRetry ? `<button id="knowledge-review-retry" class="button button-ghost" type="button" ${busy ? "disabled" : ""}>重新入队</button>` : ""}
            ${canPublish ? `<button id="knowledge-review-publish" class="button button-primary" type="button" ${busy ? "disabled" : ""}>确认发布</button>` : ""}
          </div>
        </div>
      ` : ""}
    </section>
  `;
}

function renderKnowledgeReviewCockpit() {
  const cockpit = knowledgeState.reviewCockpit || {};
  const impact = cockpit.impact || {};
  const items = Array.isArray(cockpit.items) ? cockpit.items : [];
  const attentionItems = items.filter((item) => Array.isArray(item.attention_reasons) && item.attention_reasons.length);
  const stageEntries = Object.entries(impact.pipeline_stages || {});
  const receiptEntries = Object.entries(impact.receipts || {});
  const gapItems = Array.isArray(cockpit.gaps) ? cockpit.gaps : [];
  const receiptTotal = receiptEntries.reduce((sum, [, count]) => sum + Number(count || 0), 0);
  const status = knowledgeState.reviewCockpitLoading || knowledgeState.reviewCockpitError || `${attentionItems.length} 条需要处理`;
  const visibleItems = attentionItems.length ? attentionItems : items.slice(0, 5);
  const supplyStatus = renderKnowledgeSupplyStatus(cockpit);
  return `
    <section class="knowledge-cockpit ${knowledgeState.reviewCockpitOpen ? "is-open" : ""}" aria-label="全局复核">
      <div class="knowledge-cockpit__head">
        <div>
          <p class="web-kicker">Review Cockpit</p>
          <h2>全局复核</h2>
        </div>
        <div class="knowledge-cockpit__actions">
          <span>${escapeHTML(status)}</span>
          <button id="knowledge-cockpit-refresh" class="button button-ghost" type="button">更新</button>
          <button id="knowledge-cockpit-toggle" class="button button-ghost" type="button" aria-expanded="${knowledgeState.reviewCockpitOpen}">${knowledgeState.reviewCockpitOpen ? "收起" : "展开"}</button>
        </div>
      </div>
      ${knowledgeState.reviewCockpitOpen ? `
        <div class="knowledge-cockpit__body">
          <div class="knowledge-cockpit__metrics">
            <div><span>Published</span><strong>${Number(impact.published_releases || 0)}</strong></div>
            <div><span>Receipts</span><strong>${receiptTotal}</strong></div>
            <div><span>Gaps</span><strong>${gapItems.length}</strong></div>
          </div>
          ${supplyStatus}
          <div class="knowledge-cockpit__chips">
            ${stageEntries.map(([stage, count]) => `<span>${escapeHTML(stage)} ${Number(count || 0)}</span>`).join("") || "<span>pipeline 暂无数据</span>"}
            ${receiptEntries.map(([disposition, count]) => `<span>${escapeHTML(disposition)} ${Number(count || 0)}</span>`).join("")}
          </div>
          ${knowledgeState.reviewCockpitError ? `<p class="knowledge-cockpit__error">${escapeHTML(knowledgeState.reviewCockpitError)}</p>` : ""}
          <div class="knowledge-cockpit__items">
            ${visibleItems.map((item) => `
              <button class="knowledge-cockpit__item" type="button" data-cockpit-book-id="${escapeAttribute(item.book_id || "")}">
                <strong>${escapeHTML(item.title || item.book_id || item.release_id || "未命名知识")}</strong>
                <span>${escapeHTML(item.pipeline_stage || "unknown")} · ${escapeHTML(item.latest_reverification_status || item.quality_decision || "stable")}</span>
                <small>${(item.attention_reasons || []).map((reason) => escapeHTML(knowledgeReviewReasonLabel(reason))).join(" / ") || "暂无处理项"}</small>
              </button>
            `).join("") || "<p class=\"web-muted\">暂无发布知识，先完成分析、质量校验和发布。</p>"}
          </div>
        </div>
      ` : ""}
    </section>
  `;
}

function renderKnowledgeSupplyStatus(cockpit) {
  const impact = cockpit?.impact || {};
  const rebuildActions = impact.rebuild_actions || {};
  const rebuildPlan = cockpit?.rebuild_plan || {};
  const rebuildItems = Array.isArray(rebuildPlan.items) ? rebuildPlan.items : [];
  const needsRebuild = Number(rebuildActions.rebuild || 0) + Number(rebuildActions.reevaluate || 0) + Number(rebuildActions.republish || 0);
  const published = Number(impact.published_releases || 0);
  const cards = [
    ["Source Connector", "ready", "healthy", "统一来源契约已启用"],
    ["Search Index", "ready", published > 0 ? "healthy" : "quiet", published > 0 ? "可从知识包重建" : "等待发布知识"],
    ["Health Feed", published, published > 0 ? "healthy" : "quiet", "evidence_only release"],
    ["Evaluation", "smoke", "healthy", "检索与引用质量检查"],
    ["Rebuild Plan", needsRebuild || "clear", needsRebuild ? "attention" : "healthy", rebuildItems.length ? `${rebuildItems.length} 个 release 已评估` : "暂无发布版本"],
  ];
  return `
    <section class="knowledge-supply" aria-label="供应链状态">
      <div class="knowledge-supply__head">
        <p class="web-kicker">Knowledge Supply</p>
        <h3>供应链状态</h3>
      </div>
      <div class="knowledge-supply__grid">
        ${cards.map(([label, value, status, detail]) => `
          <div class="knowledge-supply__card">
            <span class="knowledge-supply__status is-${escapeAttribute(status)}">${escapeHTML(label)}</span>
            <strong>${escapeHTML(String(value))}</strong>
            <small>${escapeHTML(detail)}</small>
          </div>
        `).join("")}
      </div>
    </section>
  `;
}

function renderKnowledgePipelineDashboard() {
  const dashboard = knowledgeState.pipelineDashboard || {};
  const summary = dashboard.summary || {};
  const items = Array.isArray(dashboard.items) ? dashboard.items : [];
  const automation = knowledgeState.pipelineAutomation || {};
  const status = knowledgeState.pipelineLoading || knowledgeState.pipelineError || `${Number(summary.total || 0)} 条内容`;
  const rows = items.slice(0, 12).map((item) => `
    <button class="knowledge-pipeline__item" type="button" data-pipeline-book-id="${escapeAttribute(item.book_id || "")}">
      <div>
        <strong>${escapeHTML(item.title || item.book_id || "未命名内容")}</strong>
        <span>${escapeHTML([item.source_type || "source", item.source_account || ""].filter(Boolean).join(" · "))}</span>
      </div>
      <small class="knowledge-pipeline__stage is-${escapeAttribute(item.next_action || item.stage || "unknown")}">${escapeHTML(knowledgePipelineActionLabel(item.next_action || item.stage))}</small>
    </button>
  `).join("");
  const runRows = Array.isArray(automation.items) ? automation.items.slice(0, 5).map((item) => `
    <li>
      <span>${escapeHTML(item.title || item.book_id)}</span>
      <small>${escapeHTML(knowledgePipelineActionLabel(item.action))} · ${escapeHTML(item.status || "planned")}${item.next_action ? ` → ${escapeHTML(knowledgePipelineActionLabel(item.next_action))}` : ""}</small>
    </li>
  `).join("") : "";
  return `
    <section class="knowledge-pipeline" aria-label="知识流水线">
      <div class="knowledge-pipeline__head">
        <div>
          <p class="web-kicker">Knowledge Pipeline</p>
          <h2>知识流水线</h2>
        </div>
        <div class="knowledge-pipeline__actions">
          <span>${escapeHTML(status)}</span>
          <button id="knowledge-pipeline-refresh" class="button button-ghost" type="button">刷新</button>
          <button id="knowledge-pipeline-preview" class="button button-ghost" type="button" ${knowledgeState.pipelineAutomationLoading ? "disabled" : ""}>预览推进</button>
          <button id="knowledge-pipeline-run" class="button button-primary" type="button" ${knowledgeState.pipelineAutomationLoading ? "disabled" : ""}>${knowledgeState.pipelineAutomationLoading || "自动推进一次"}</button>
        </div>
      </div>
      <div class="knowledge-pipeline__metrics">
        <div><span>待分析</span><strong>${Number(summary.needs_analysis || 0)}</strong></div>
        <div><span>待质检</span><strong>${Number(summary.needs_quality || 0)}</strong></div>
        <div><span>可发布</span><strong>${Number(summary.ready_to_publish || 0)}</strong></div>
        <div><span>已发布</span><strong>${Number(summary.published || 0)}</strong></div>
        <div><span>阻塞</span><strong>${Number(summary.blocked || 0)}</strong></div>
      </div>
      ${knowledgeState.pipelineError ? `<p class="knowledge-cockpit__error">${escapeHTML(knowledgeState.pipelineError)}</p>` : ""}
      <div class="knowledge-pipeline__items">
        ${rows || "<p class=\"web-muted\">暂无流水线条目。</p>"}
      </div>
      ${knowledgeState.pipelineAutomationError ? `<p class="knowledge-cockpit__error">${escapeHTML(knowledgeState.pipelineAutomationError)}</p>` : ""}
      ${automation.items ? `
        <div class="knowledge-pipeline__run">
          <strong>${automation.dry_run ? "预览结果" : "推进结果"}：eligible ${Number(automation.eligible || 0)} · processed ${Number(automation.processed || 0)} · failed ${Number(automation.failed || 0)}</strong>
          <ul>${runRows || "<li><span>暂无可推进内容</span></li>"}</ul>
        </div>
      ` : ""}
    </section>
  `;
}

function knowledgePipelineActionLabel(action) {
  return ({
    needs_analysis: "待分析",
    needs_quality: "待质检",
    ready_to_publish: "可发布",
    published: "已发布",
    blocked: "阻塞",
    normalized: "已清洗",
    analyzed: "已分析",
    candidate: "候选",
  })[String(action || "")] || action || "未知";
}

function renderBookKnowledge() {
  const bookRows = knowledgeState.books.map((book, index) => {
    const active = book.book_id === knowledgeState.selectedBook?.book_id ? " active" : "";
    return `
      <button class="knowledge-web__book${active}" type="button" data-book-index="${index}">
        <strong>${escapeHTML(book.title || book.book_id)}</strong>
        <span>${escapeHTML([book.status || "draft", book.extractor || ""].filter(Boolean).join(" · "))}</span>
      </button>
    `;
  }).join("");
  const pkg = knowledgeState.package || {};
  const currentBook = pkg.book || knowledgeState.selectedBook || {};
  const resultRows = knowledgeState.results.map((result) => `
    <article class="knowledge-web__result">
      <div class="web-kicker">${escapeHTML(result.kind || "result")}</div>
      <h3>${escapeHTML(result.title || result.id || "片段")}</h3>
      <p>${escapeHTML(result.snippet || "")}</p>
    </article>
  `).join("");
  const chapterRows = (pkg.chapters || []).slice(0, 16).map((chapter) => `
    <li>
      <span>${escapeHTML(chapter.title || chapter.chapter_id)}</span>
      <small>${escapeHTML(chapter.summary || "")}</small>
    </li>
  `).join("");
  const status = knowledgeState.loading
    ? `<div class="web-status">处理中：${escapeHTML(knowledgeState.loading)}</div>`
    : (knowledgeState.message ? `<div class="web-status">${escapeHTML(knowledgeState.message)}</div>` : "");
  const analysisPrompt = knowledgeState.analysisPrompt || knowledgeAnalysisPrompts[0][2];
  const analysisResponse = knowledgeState.analysisResponse || {};
  const analysisStats = analysisResponse.context_stats
    ? `${analysisResponse.context_stats.chapters || 0} 章 · ${analysisResponse.context_stats.claims || 0} claims · ${analysisResponse.context_stats.chunks || 0} chunks`
    : "";
  const analysisManifest = knowledgeState.analysisManifest || {};
  const manifestStatus = analysisManifest.status || "pending";
  const manifestStatusLabels = {
    pending: "待分析",
    running: "分析中",
    ready: "已完成",
    failed: "需重试",
  };
  const manifestActionLabel = analysisManifest.answer ? "重新生成" : "生成基线分析";
  const cockpitHTML = renderKnowledgeReviewCockpit();
  const pipelineHTML = renderKnowledgePipelineDashboard();

  renderShell(`
    <main class="knowledge-web">
      <section class="knowledge-web__header">
        <div>
          <p class="web-kicker">Book Knowledge</p>
          <h1>书籍知识库</h1>
        </div>
        <button id="knowledge-refresh" class="button button-ghost" type="button">刷新</button>
      </section>
      ${status}
      ${cockpitHTML}
      ${pipelineHTML}

      <div class="knowledge-web__layout">
        <aside class="knowledge-web__sidebar">
          <form id="knowledge-search-form" class="source-form">
            <label>
              <span>搜索</span>
              <input name="query" value="${escapeAttribute(knowledgeState.query)}" placeholder="搜索标题、claims 或 chunks">
            </label>
            <button class="button button-primary" type="submit">Search</button>
          </form>
          <div class="knowledge-web__books">
            ${bookRows || "<p class=\"web-muted\">暂无知识库条目，可先从微信来源导入。</p>"}
          </div>
        </aside>

        <section class="knowledge-web__main">
          ${currentBook.book_id ? `
            <div class="knowledge-web__title-row">
              <div>
                <p class="web-kicker">${escapeHTML(currentBook.book_id)}</p>
                <h2>${escapeHTML(currentBook.title || currentBook.book_id)}</h2>
              </div>
              <a class="button button-primary" href="${escapeAttribute(buildBookReaderURL(currentBook.book_id))}">阅读</a>
            </div>
            <div class="knowledge-web__stats">
              <span>${(pkg.chapters || []).length} 章</span>
              <span>${(pkg.claims || []).length} claims</span>
              <span>${(pkg.chunks || []).length} chunks</span>
            </div>
            ${renderKnowledgeReview()}
            <div class="knowledge-web__content">
              <section>
                <p class="web-kicker">Chapters</p>
                <ul>${chapterRows || "<li><span>暂无章节</span></li>"}</ul>
              </section>
              <section>
                <p class="web-kicker">Search Results</p>
                ${resultRows || "<p class=\"web-muted\">输入关键词后查看检索结果。</p>"}
              </section>
            </div>
            <section class="knowledge-web__manifest" aria-label="知识基线分析">
              <div class="knowledge-web__manifest-head">
                <div>
                  <p class="web-kicker">Analysis Manifest</p>
                  <h3>知识基线分析</h3>
                </div>
                <span class="knowledge-web__manifest-status is-${escapeAttribute(manifestStatus)}">${escapeHTML(manifestStatusLabels[manifestStatus] || manifestStatus)}</span>
              </div>
              <div class="knowledge-web__manifest-meta">
                <span>${escapeHTML(knowledgeModelLabel(analysisManifest.model || knowledgeState.analysisModel))}</span>
                ${analysisManifest.updated_at ? `<span>更新于 ${escapeHTML(analysisManifest.updated_at)}</span>` : ""}
                ${analysisManifest.content_hash ? `<span>内容版本 ${escapeHTML(String(analysisManifest.content_hash).slice(0, 12))}</span>` : ""}
              </div>
              ${analysisManifest.error || knowledgeState.analysisManifestError ? `<p class="knowledge-web__manifest-error">${escapeHTML(analysisManifest.error || knowledgeState.analysisManifestError)}</p>` : ""}
              ${analysisManifest.answer ? `<article class="knowledge-web__answer">${renderSimpleMarkdown(analysisManifest.answer)}</article>` : `<p class="web-muted">生成后会形成可追溯、可供其他系统读取的文章基线分析。</p>`}
              <div class="knowledge-web__manifest-actions">
                <span>${escapeHTML(knowledgeState.analysisManifestLoading || "摘要、结论、风险与行动建议")}</span>
                <button id="knowledge-analysis-generate" class="button button-primary" type="button" ${knowledgeState.analysisManifestLoading ? "disabled" : ""}>${knowledgeState.analysisManifestLoading ? "生成中" : manifestActionLabel}</button>
              </div>
            </section>
            <section class="knowledge-web__analysis" aria-label="大模型分析">
              <div class="knowledge-web__analysis-head">
                <div>
                  <p class="web-kicker">TokenPlan Study</p>
                  <h3>大模型分析</h3>
                </div>
                <select id="knowledge-analysis-model" aria-label="模型">
                  ${knowledgeAnalysisModels.map((model) => `
                    <option value="${escapeAttribute(model.id)}" ${knowledgeState.analysisModel === model.id ? "selected" : ""}>${escapeHTML(model.label)}</option>
                  `).join("")}
                </select>
              </div>
              <div class="knowledge-web__prompt-grid">
                ${knowledgeAnalysisPrompts.map(([key, label]) => `
                  <button class="button button-ghost" type="button" data-knowledge-prompt="${escapeAttribute(key)}">${escapeHTML(label)}</button>
                `).join("")}
              </div>
              <form id="knowledge-analysis-form" class="knowledge-web__analysis-form">
                <textarea name="question" rows="5" placeholder="围绕当前文章提问，或点击上方模板">${escapeHTML(analysisPrompt)}</textarea>
                <div class="knowledge-web__analysis-actions">
                  <span>${escapeHTML(analysisStats || knowledgeState.analysisError || knowledgeState.analysisLoading || "会基于当前文章知识包回答。")}</span>
                  <button class="button button-primary" type="submit">${knowledgeState.analysisLoading ? "分析中" : "分析当前文章"}</button>
                </div>
              </form>
              ${analysisResponse.answer ? `
                <article class="knowledge-web__answer">
                  <div class="web-kicker">${escapeHTML(analysisResponse.model || knowledgeState.analysisModel)}</div>
                  ${renderSimpleMarkdown(analysisResponse.answer)}
                </article>
              ` : ""}
            </section>
          ` : "<p class=\"web-muted\">请选择书籍或导入新来源。</p>"}
        </section>
      </div>
    </main>
  `, "knowledge");
  bindBookKnowledgeEvents();
}

function hasBookAgentCapability(capability) {
  const capabilities = bookAgentState.package?.ui_manifest?.capabilities;
  return Array.isArray(capabilities) && capabilities.includes(capability);
}

function renderBookAgentCapability(capability, content, runtimeAvailable = true) {
  if (!hasBookAgentCapability(capability)) {
    return "";
  }
  if (!runtimeAvailable) {
    return `
      <section class="book-agent__capability book-agent__unavailable" data-capability="${escapeAttribute(capability)}">
        <span class="book-agent__capability-index">${escapeHTML(capability.replaceAll("_", " "))}</span>
        <div>
          <strong>功能已声明，但运行时尚未接通</strong>
          <p>当前包保留了这个入口；接入对应的受控运行时后才会开放，不会跳转到空页面。</p>
        </div>
      </section>
    `;
  }
  return content;
}

function renderBookAgentPackageIndex(route) {
  const rows = bookAgentState.packages.map((pkg, index) => {
    const version = pkg.version || "";
    return `
      <article class="book-agent__package-card" style="--card-index:${index}">
        <div class="book-agent__package-number">${String(index + 1).padStart(2, "0")}</div>
        <div>
          <p class="web-kicker">${escapeHTML(pkg.lifecycle_state || "published")}</p>
          <h2>${escapeHTML(pkg.package_id || "Untitled package")}</h2>
          <p>${escapeHTML(version ? `Version ${version}` : "Version unavailable")}</p>
        </div>
        <nav aria-label="Package destinations">
          <a href="${escapeAttribute(buildAgentPackageURL(pkg.package_id, version))}">Package</a>
          <a href="${escapeAttribute(buildAgentURL(pkg.package_id, version))}">Agent</a>
          <a href="${escapeAttribute(buildBookAppURL(pkg.package_id, version))}">Book App</a>
        </nav>
      </article>
    `;
  }).join("");
  const viewLabel = route.view === "app" ? "Book Apps" : (route.view === "agent" ? "Agents" : "Agent Packages");
  return `
    <main class="book-agent book-agent--index">
      <header class="book-agent__index-head">
        <p class="web-kicker">Shared Book Runtime</p>
        <h1>${escapeHTML(viewLabel)}</h1>
        <p>一个版本化知识包，三条稳定路径。Package 展示契约，Agent 展示运行边界，Book App 只呈现清单声明的能力。</p>
      </header>
      ${bookAgentState.message ? `<p class="web-status">${escapeHTML(bookAgentState.message)}</p>` : ""}
      <section class="book-agent__package-grid" aria-label="Published Agent Packages">
        ${rows || `<div class="book-agent__empty"><strong>尚无已发布 Agent Package</strong><p>先完成知识发布与评测；这里不会用示例内容伪造可运行产品。</p></div>`}
      </section>
    </main>
  `;
}

function renderBookAgentEvidence() {
  const releaseRows = bookAgentState.releases.map((release) => {
    const claims = Array.isArray(release.analysis?.claims) ? release.analysis.claims : [];
    const citations = Array.isArray(release.citations) ? release.citations : [];
    return `
      <article class="book-agent__release">
        <header>
          <div>
            <span>Release ${escapeHTML(release.version || "—")}</span>
            <strong>${escapeHTML(release.book?.title || release.book_id || release.release_id)}</strong>
          </div>
          <code>${escapeHTML(String(release.content_hash || "").slice(0, 18))}</code>
        </header>
        <div class="book-agent__evidence-grid">
          ${claims.slice(0, 6).map((claim) => `
            <div>
              <span>${escapeHTML(claim.id || "claim")}</span>
              <p>${escapeHTML(claim.statement || "")}</p>
              <small>${(claim.citation_ids || []).map((id) => escapeHTML(id)).join(" · ") || "No citation IDs"}</small>
            </div>
          `).join("") || `<p class="web-muted">此 release 暂无结构化 claims。</p>`}
        </div>
        <footer>${claims.length} claims · ${citations.length} citations · ${escapeHTML(release.usage_policy || "policy unknown")}</footer>
      </article>
    `;
  }).join("");
  return `
    <section class="book-agent__capability book-agent__evidence" data-capability="evidence">
      <div class="book-agent__section-head">
        <div><span>04</span><h2>Evidence ledger</h2></div>
        <p>固定 release、claim 与 citation 身份；不展示下载源正文。</p>
      </div>
      ${releaseRows || `<p class="web-muted">正在等待 release 证据。</p>`}
    </section>
  `;
}

function renderBookAgentPlatform(route = bookAgentState.route || { view: "package", packageID: "" }) {
  if (!route.packageID || !bookAgentState.package) {
    renderShell(renderBookAgentPackageIndex(route), "agents");
    return;
  }
  const pkg = bookAgentState.package;
  const evaluation = pkg.evaluation || {};
  const release = bookAgentState.releases[0] || {};
  const bookID = release.book_id || release.book?.book_id || "";
  const viewLabels = {
    package: ["Package contract", "版本、边界与评测证据"],
    agent: ["Agent console", "受策略约束的检索、模型与工具入口"],
    app: ["Shared Book App", "由 ui_manifest 生成的阅读与证据空间"],
  };
  const [viewLabel, viewDescription] = viewLabels[route.view] || viewLabels.app;
  const searchRows = bookAgentState.results.map((result) => `
    <article>
      <span>${escapeHTML(result.kind || "evidence")}</span>
      <strong>${escapeHTML(result.title || result.id || "Result")}</strong>
      <p>${escapeHTML(result.snippet || "")}</p>
    </article>
  `).join("");
  const evaluationMetrics = Object.entries(evaluation.metrics || {}).map(([metric, score]) => `
    <div><span>${escapeHTML(metric)}</span><strong>${Math.round(Number(score || 0) * 100)}%</strong></div>
  `).join("");
  const runtimeStatus = bookAgentState.loading || bookAgentState.message;

  renderShell(`
    <main class="book-agent book-agent--detail">
      <header class="book-agent__hero">
        <div class="book-agent__hero-copy">
          <p class="web-kicker">${escapeHTML(viewLabel)}</p>
          <h1>${escapeHTML(pkg.package_id)}</h1>
          <p>${escapeHTML(viewDescription)}</p>
          <div class="book-agent__route-switch" aria-label="Package routes">
            <a class="${route.view === "package" ? "active" : ""}" href="${escapeAttribute(buildAgentPackageURL(pkg.package_id, pkg.version))}">Package</a>
            <a class="${route.view === "agent" ? "active" : ""}" href="${escapeAttribute(buildAgentURL(pkg.package_id, pkg.version))}">Agent</a>
            <a class="${route.view === "app" ? "active" : ""}" href="${escapeAttribute(buildBookAppURL(pkg.package_id, pkg.version))}">Book App</a>
          </div>
        </div>
        <aside class="book-agent__hero-ledger">
          <div><span>VERSION</span><strong>${escapeHTML(pkg.version)}</strong></div>
          <div><span>RELEASES</span><strong>${pkg.releases?.length || 0}</strong></div>
          <div><span>POLICY</span><strong>${escapeHTML(pkg.safety_policy?.usage_policy || "unknown")}</strong></div>
          <div class="book-agent__evaluation ${evaluation.passed ? "is-pass" : "is-hold"}">
            <span>EVALUATION</span>
            <strong>${evaluation.passed ? "Evaluation passed" : "Evaluation hold"}</strong>
            <small>${escapeHTML(evaluation.suite_version || pkg.evaluation_policy?.suite_version || "suite unavailable")}</small>
          </div>
        </aside>
      </header>

      ${runtimeStatus ? `<p class="web-status">${escapeHTML(runtimeStatus)}</p>` : ""}

      <section class="book-agent__manifest">
        <div><span>Package hash</span><code>${escapeHTML(pkg.content_hash)}</code></div>
        <div><span>Model route</span><strong>${escapeHTML(pkg.model_policy?.preferred_capability || "—")}</strong></div>
        <div><span>Retrieval</span><strong>${escapeHTML(pkg.retrieval_policy?.strategy || "—")}</strong></div>
        <div><span>Escalation</span><strong>${escapeHTML(pkg.safety_policy?.escalation_target || "—")}</strong></div>
        ${evaluationMetrics ? `<div class="book-agent__metric-strip">${evaluationMetrics}</div>` : ""}
      </section>

      <section class="book-agent__capabilities" aria-label="Manifest capabilities">
        ${renderBookAgentCapability("reader", `
          <section class="book-agent__capability book-agent__reader" data-capability="reader">
            <div class="book-agent__section-head"><div><span>01</span><h2>Reader</h2></div><p>回到固定 source version 的阅读面。</p></div>
            ${bookID ? `<a class="book-agent__reader-link" href="${escapeAttribute(buildBookReaderURL(bookID))}"><span>Open the book</span><strong>${escapeHTML(release.book?.title || bookID)}</strong><small>版本化阅读入口 →</small></a>` : `<div class="book-agent__unavailable"><strong>功能已声明，但运行时尚未接通</strong><p>Release 尚未提供可解析的 book_id。</p></div>`}
          </section>
        `)}
        ${renderBookAgentCapability("search", `
          <section class="book-agent__capability book-agent__search" data-capability="search">
            <div class="book-agent__section-head"><div><span>02</span><h2>Grounded search</h2></div><p>结果保持 claim、chunk 与 release 身份。</p></div>
            <form id="book-agent-search-form"><input name="query" value="${escapeAttribute(bookAgentState.query)}" placeholder="Search this package"><button class="button button-primary" type="submit">Search</button></form>
            <div class="book-agent__search-results">${searchRows || `<p class="web-muted">输入关键词以检索此包固定的知识范围。</p>`}</div>
          </section>
        `, Boolean(bookID))}
        ${renderBookAgentCapability("grounded_chat", `
          <section class="book-agent__capability book-agent__chat" data-capability="grounded_chat">
            <div class="book-agent__section-head"><div><span>03</span><h2>Grounded conversation</h2></div><p>回答必须经过 package 的 citation 与 abstention 边界。</p></div>
            <form id="book-agent-chat-form"><textarea name="question" rows="4" placeholder="Ask a question grounded in this package">${escapeHTML(bookAgentState.question)}</textarea><button class="button button-primary" type="submit">Ask with evidence</button></form>
            ${bookAgentState.answer?.answer ? `<article class="book-agent__answer">${renderSimpleMarkdown(bookAgentState.answer.answer)}</article>` : ""}
          </section>
        `, Boolean(bookID))}
        ${renderBookAgentCapability("evidence", renderBookAgentEvidence())}
        ${renderBookAgentCapability("quiz", "", false)}
        ${renderBookAgentCapability("action_plan", "", false)}
      </section>
    </main>
  `, "agents");
  bindBookAgentPlatformEvents(route);
}

function bindBookAgentPlatformEvents(route) {
  document.querySelector("#book-agent-search-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    bookAgentState.query = String(data.get("query") || "").trim();
    await searchBookAgentPackage(route);
  });
  document.querySelector("#book-agent-chat-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    bookAgentState.question = String(data.get("question") || "").trim();
    await chatWithBookAgentPackage(route);
  });
}

async function loadBookAgentPlatform(route) {
  bookAgentState.route = route;
  bookAgentState.loading = "Loading Agent Packages";
  bookAgentState.message = "";
  renderBookAgentPlatform(route);
  try {
    if (!route.packageID) {
      const payload = await apiFetch("/api/agent-packages?limit=100");
      bookAgentState.packages = Array.isArray(payload.packages) ? payload.packages : [];
      bookAgentState.message = `${bookAgentState.packages.length} published packages`;
      return;
    }
    const query = route.version ? `?version=${encodeURIComponent(route.version)}` : "";
    bookAgentState.package = await apiFetch(`/api/agent-packages/${encodeURIComponent(route.packageID)}${query}`);
    bookAgentState.releases = await Promise.all((bookAgentState.package.releases || []).map((reference) => (
      apiFetch(`/api/knowledge/releases/${encodeURIComponent(reference.release_id)}`)
    )));
    bookAgentState.message = "Package, releases, and evaluation loaded";
  } catch (error) {
    bookAgentState.message = error instanceof Error ? error.message : String(error);
  } finally {
    bookAgentState.loading = "";
    renderBookAgentPlatform(route);
  }
}

async function searchBookAgentPackage(route) {
  const release = bookAgentState.releases[0] || {};
  const bookID = release.book_id || release.book?.book_id || "";
  if (!bookAgentState.query || !bookID) {
    bookAgentState.results = [];
    renderBookAgentPlatform(route);
    return;
  }
  bookAgentState.loading = "Searching pinned evidence";
  renderBookAgentPlatform(route);
  try {
    const query = new URLSearchParams({ q: bookAgentState.query, book_id: bookID, limit: "20" });
    const payload = await apiFetch(`/api/search?${query.toString()}`);
    bookAgentState.results = Array.isArray(payload.results) ? payload.results : [];
    bookAgentState.message = `${bookAgentState.results.length} evidence results`;
  } catch (error) {
    bookAgentState.message = error instanceof Error ? error.message : String(error);
  } finally {
    bookAgentState.loading = "";
    renderBookAgentPlatform(route);
  }
}

async function chatWithBookAgentPackage(route) {
  const release = bookAgentState.releases[0] || {};
  const bookID = release.book_id || release.book?.book_id || "";
  if (!bookAgentState.question || !bookID) {
    return;
  }
  bookAgentState.loading = "Reasoning over pinned evidence";
  renderBookAgentPlatform(route);
  try {
    bookAgentState.answer = await apiFetch("/api/book-chat", {
      method: "POST",
      body: JSON.stringify({
        book_id: bookID,
        mode: "analysis",
        question: bookAgentState.question,
        model: knowledgeState.analysisModel || "qwen3.7-max",
        max_context_chars: 12000,
      }),
    });
    bookAgentState.message = "Grounded response complete";
  } catch (error) {
    bookAgentState.message = error instanceof Error ? error.message : String(error);
  } finally {
    bookAgentState.loading = "";
    renderBookAgentPlatform(route);
  }
}

function renderInlineMarkdown(value) {
  const tokens = [];
  const stash = (html) => {
    const token = `\u0000markdown-token-${tokens.length}\u0000`;
    tokens.push(html);
    return token;
  };
  let source = String(value || "");
  source = source.replace(/`([^`\n]+)`/g, (_, code) => stash(`<code>${escapeHTML(code)}</code>`));
  source = source.replace(/\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/gi, (_, label, href) => (
    stash(`<a href="${escapeAttribute(href)}" target="_blank" rel="noopener noreferrer">${escapeHTML(label)}</a>`)
  ));
  let rendered = escapeHTML(source)
    .replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>")
    .replace(/__([^_\n]+)__/g, "<strong>$1</strong>")
    .replace(/(^|[\s(（])\*([^*\n]+)\*(?=$|[\s),.!?;:，。！？；：）])/g, "$1<em>$2</em>");
  tokens.forEach((html, index) => {
    rendered = rendered.replaceAll(`\u0000markdown-token-${index}\u0000`, html);
  });
  return rendered;
}

function renderSimpleMarkdown(markdown) {
  const blocks = String(markdown || "").split(/\n{2,}/).map((block) => block.trim()).filter(Boolean);
  if (!blocks.length) {
    return "";
  }
  return blocks.map((block) => {
    if (/^(?:-{3,}|\*{3,}|_{3,})$/.test(block)) {
      return "<hr>";
    }
    if (/^#{1,4}\s+/.test(block)) {
      const level = Math.min(4, block.match(/^#+/)?.[0]?.length || 3);
      return `<h${level}>${renderInlineMarkdown(block.replace(/^#{1,4}\s+/, ""))}</h${level}>`;
    }
    const lines = block.split(/\n/).filter(Boolean);
    if (lines.length && lines.every((line) => /^[-*]\s+/.test(line))) {
      const items = lines.map((line) => line.replace(/^[-*]\s+/, ""));
      return `<ul>${items.map((item) => `<li>${renderInlineMarkdown(item)}</li>`).join("")}</ul>`;
    }
    if (lines.length && lines.every((line) => /^\d+\.\s+/.test(line))) {
      const items = lines.map((line) => line.replace(/^\d+\.\s+/, ""));
      return `<ol>${items.map((item) => `<li>${renderInlineMarkdown(item)}</li>`).join("")}</ol>`;
    }
    if (lines.length && lines.every((line) => /^>\s?/.test(line))) {
      return `<blockquote>${lines.map((line) => renderInlineMarkdown(line.replace(/^>\s?/, ""))).join("<br>")}</blockquote>`;
    }
    return `<p>${lines.map((line) => renderInlineMarkdown(line)).join("<br>")}</p>`;
  }).join("");
}

function renderCourseMarkdown(markdown) {
  const source = String(markdown || "").replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const blocks = source.split(/\n{2,}/).map((block) => block.trim()).filter(Boolean);
  if (!blocks.length) {
    return "";
  }
  return blocks.map((block) => {
    if (/^(?:-{3,}|\*{3,}|_{3,}|✵)$/.test(block)) {
      return `<hr class="dedao-course-article__divider">`;
    }
    const imageMatch = block.match(/^!\[([^\]\n]*)\]\((https?:\/\/[^\s)]+)\)$/i);
    if (imageMatch) {
      const alt = imageMatch[1] || "";
      const src = imageMatch[2] || "";
      return `
        <figure class="dedao-course-article__image">
          <img src="${escapeAttribute(src)}" alt="${escapeAttribute(alt)}" loading="lazy">
          ${alt && alt !== src ? `<figcaption>${escapeHTML(alt)}</figcaption>` : ""}
        </figure>
      `;
    }
    if (/^#{1,6}\s+/.test(block)) {
      const level = Math.min(4, block.match(/^#+/)?.[0]?.length || 2);
      return `<h${level}>${renderInlineMarkdown(block.replace(/^#{1,6}\s+/, ""))}</h${level}>`;
    }
    const lines = block.split(/\n/).filter(Boolean);
    if (lines.length && lines.every((line) => /^[-*]\s+/.test(line))) {
      const items = lines.map((line) => line.replace(/^[-*]\s+/, ""));
      return `<ul>${items.map((item) => `<li>${renderInlineMarkdown(item)}</li>`).join("")}</ul>`;
    }
    if (lines.length && lines.every((line) => /^\d+\.\s+/.test(line))) {
      const items = lines.map((line) => line.replace(/^\d+\.\s+/, ""));
      return `<ol>${items.map((item) => `<li>${renderInlineMarkdown(item)}</li>`).join("")}</ol>`;
    }
    if (lines.length && lines.every((line) => /^>\s?/.test(line))) {
      return `<blockquote>${lines.map((line) => renderInlineMarkdown(line.replace(/^>\s?/, ""))).join("<br>")}</blockquote>`;
    }
    return `<p>${lines.map((line) => renderInlineMarkdown(line)).join("<br>")}</p>`;
  }).join("");
}

function resetKnowledgeAnalysis(prompt = "") {
  knowledgeState.analysisPrompt = prompt;
  knowledgeState.analysisResponse = null;
  knowledgeState.analysisLoading = "";
  knowledgeState.analysisError = "";
  knowledgeState.analysisManifest = null;
  knowledgeState.analysisManifestLoading = "";
  knowledgeState.analysisManifestError = "";
}

function resetKnowledgeReview() {
  knowledgeReviewLoadSequence++;
  if (knowledgeReviewPollTimer) {
    clearTimeout(knowledgeReviewPollTimer);
    knowledgeReviewPollTimer = null;
  }
  knowledgeState.releases = [];
  knowledgeState.selectedRelease = null;
  knowledgeState.releaseDetail = null;
  knowledgeState.feedbackAssessment = null;
  knowledgeState.reverificationTasks = [];
  knowledgeState.qualityReport = null;
  knowledgeState.reviewOpen = new URLSearchParams(window.location.search).get("review") === "1";
  knowledgeState.reviewLoading = "";
  knowledgeState.reviewError = "";
  knowledgeState.reviewOperation = "";
}

async function loadKnowledgeReleaseRecords(bookID) {
  const releases = [];
  let after = "";
  for (let page = 0; page < 20; page++) {
    const params = new URLSearchParams({ book_id: bookID, limit: "200" });
    if (after) {
      params.set("after", after);
    }
    const payload = await apiFetch(`/api/knowledge/releases?${params.toString()}`);
    const pageReleases = Array.isArray(payload.releases) ? payload.releases : [];
    releases.push(...pageReleases);
    if (pageReleases.length < 200 || !payload.next_cursor || payload.next_cursor === after) {
      break;
    }
    after = payload.next_cursor;
  }
  return releases;
}

async function loadOptionalKnowledgeResource(path) {
  try {
    return await apiFetch(path);
  } catch (error) {
    if (error?.status === 404) {
      return null;
    }
    throw error;
  }
}

async function loadKnowledgeReview(bookID, { silent = false, renderResult = true } = {}) {
  const sequence = ++knowledgeReviewLoadSequence;
  if (!silent) {
    knowledgeState.reviewLoading = "加载复核状态";
    knowledgeState.reviewError = "";
    if (renderResult) {
      renderBookKnowledge();
    }
  }
  try {
    const releases = await loadKnowledgeReleaseRecords(bookID);
    const selectedRelease = releases[releases.length - 1] || null;
    if (sequence !== knowledgeReviewLoadSequence || knowledgeState.selectedBook?.book_id !== bookID) {
      return;
    }
    knowledgeState.releases = releases;
    knowledgeState.selectedRelease = selectedRelease;
    if (!selectedRelease) {
      const qualityReport = await loadOptionalKnowledgeResource(`/api/books/${encodeURIComponent(bookID)}/quality`);
      if (sequence !== knowledgeReviewLoadSequence || knowledgeState.selectedBook?.book_id !== bookID) {
        return;
      }
      knowledgeState.releaseDetail = null;
      knowledgeState.feedbackAssessment = null;
      knowledgeState.reverificationTasks = [];
      knowledgeState.qualityReport = qualityReport;
      return;
    }
    const releaseID = encodeURIComponent(selectedRelease.release_id);
    const [releaseDetail, feedbackAssessment, taskPayload, qualityReport] = await Promise.all([
      apiFetch(`/api/knowledge/releases/${releaseID}`),
      apiFetch(`/api/knowledge/releases/${releaseID}/feedback`),
      apiFetch(`/api/knowledge/releases/${releaseID}/reverification`),
      loadOptionalKnowledgeResource(`/api/books/${encodeURIComponent(bookID)}/quality`),
    ]);
    if (sequence !== knowledgeReviewLoadSequence || knowledgeState.selectedBook?.book_id !== bookID) {
      return;
    }
    knowledgeState.releaseDetail = releaseDetail;
    knowledgeState.feedbackAssessment = feedbackAssessment;
    knowledgeState.reverificationTasks = Array.isArray(taskPayload.tasks) ? taskPayload.tasks : [];
    knowledgeState.qualityReport = qualityReport;
  } catch (error) {
    if (sequence === knowledgeReviewLoadSequence) {
      knowledgeState.reviewError = error instanceof Error ? error.message : String(error);
    }
  } finally {
    if (sequence === knowledgeReviewLoadSequence) {
      knowledgeState.reviewLoading = "";
      scheduleKnowledgeReviewPoll();
      if (renderResult) {
        renderBookKnowledge();
      }
    }
  }
}

function scheduleKnowledgeReviewPoll() {
  if (knowledgeReviewPollTimer) {
    clearTimeout(knowledgeReviewPollTimer);
    knowledgeReviewPollTimer = null;
  }
  const task = knowledgeReviewLatestTask();
  if (!window.location.pathname.startsWith("/book-knowledge") || !["queued", "running"].includes(task?.status)) {
    return;
  }
  knowledgeReviewPollTimer = setTimeout(() => {
    knowledgeReviewPollTimer = null;
    if (!window.location.pathname.startsWith("/book-knowledge")) {
      return;
    }
    const bookID = knowledgeState.selectedBook?.book_id || "";
    if (bookID) {
      loadKnowledgeReview(bookID, { silent: true });
    }
  }, 5000);
}

function setKnowledgeReviewOpen(open) {
  knowledgeState.reviewOpen = Boolean(open);
  const params = new URLSearchParams(window.location.search);
  if (knowledgeState.reviewOpen) {
    params.set("review", "1");
  } else {
    params.delete("review");
  }
  const query = params.toString();
  window.history?.replaceState?.({}, "", `${window.location.pathname}${query ? `?${query}` : ""}`);
  renderBookKnowledge();
}

async function retryKnowledgeReverification() {
  const releaseID = knowledgeState.selectedRelease?.release_id || "";
  const bookID = knowledgeState.selectedBook?.book_id || "";
  if (!releaseID || !bookID || knowledgeState.reviewOperation) {
    return;
  }
  knowledgeState.reviewOperation = "正在重新入队";
  knowledgeState.reviewError = "";
  renderBookKnowledge();
  try {
    await apiFetch(`/api/knowledge/releases/${encodeURIComponent(releaseID)}/reverification/retry`, {
      method: "POST",
      body: JSON.stringify({}),
    });
    await loadKnowledgeReview(bookID, { silent: true, renderResult: false });
  } catch (error) {
    if (knowledgeState.selectedBook?.book_id === bookID) {
      knowledgeState.reviewError = error instanceof Error ? error.message : String(error);
    }
  } finally {
    if (knowledgeState.selectedBook?.book_id === bookID) {
      knowledgeState.reviewOperation = "";
      renderBookKnowledge();
    }
  }
}

async function publishKnowledgeCandidate() {
  const bookID = knowledgeState.selectedBook?.book_id || "";
  if (!bookID || knowledgeState.reviewOperation) {
    return;
  }
  if (!window.confirm("确认发布当前通过质量校验的复核候选？发布后将生成新的不可变 release。")) {
    return;
  }
  knowledgeState.reviewOperation = "正在发布候选";
  knowledgeState.reviewError = "";
  renderBookKnowledge();
  try {
    await apiFetch(`/api/books/${encodeURIComponent(bookID)}/publish`, {
      method: "POST",
      body: JSON.stringify({}),
    });
    await loadKnowledgeReview(bookID, { silent: true, renderResult: false });
  } catch (error) {
    if (knowledgeState.selectedBook?.book_id === bookID) {
      knowledgeState.reviewError = error instanceof Error ? error.message : String(error);
    }
  } finally {
    if (knowledgeState.selectedBook?.book_id === bookID) {
      knowledgeState.reviewOperation = "";
      renderBookKnowledge();
    }
  }
}

function renderReader(payload) {
  const book = payload.book || {};
  const chapters = Array.isArray(payload.chapters) ? payload.chapters : [];
  const claims = Array.isArray(payload.claims) ? payload.claims : [];
  const chunks = Array.isArray(payload.chunks) ? payload.chunks : [];
  const title = book.title || book.book_id || "未命名书籍";
  const meta = [
    book.author,
    book.extractor,
    book.updated_at,
  ].filter(Boolean).join(" / ");
  const chapterItems = chapters.slice(0, 12).map((chapter) => (
    `<li>${escapeHTML(chapter.title || chapter.chapter_id || "章节")}</li>`
  )).join("");
  const claimItems = claims.slice(0, 8).map((claim) => (
    `<li>${escapeHTML(claim.text || claim.claim || claim.summary || "")}</li>`
  )).join("");
  const chunkItems = chunks.slice(0, 4).map((chunk) => (
    `<p>${escapeHTML(chunk.text || chunk.content || "")}</p>`
  )).join("");

  app.className = "reader-shell";
  app.innerHTML = `
    <main class="reader-page">
      <article class="reader-page__article">
        <p class="reader-page__eyebrow">KBase Reader</p>
        <h1>${escapeHTML(title)}</h1>
        <div class="reader-page__meta">${escapeHTML(meta || book.book_id || "")}</div>
        <section class="reader-page__section">
          <h2>目录</h2>
          ${chapterItems ? `<ul>${chapterItems}</ul>` : "<p>暂无目录数据。</p>"}
        </section>
        <section class="reader-page__section">
          <h2>重点</h2>
          ${claimItems ? `<ul>${claimItems}</ul>` : "<p>暂无重点摘录。</p>"}
        </section>
        <section class="reader-page__section">
          <h2>正文摘录</h2>
          ${chunkItems || "<p>暂无正文摘录。</p>"}
        </section>
      </article>
    </main>
  `;
}

function renderError(message) {
  app.className = "reader-shell";
  app.innerHTML = `
    <main class="reader-error">
      <section class="reader-error__card" role="alert">
        <h1>页面暂时无法打开</h1>
        <p>${escapeHTML(message)}</p>
      </section>
    </main>
  `;
}

function renderWeChatSource() {
  const accountRows = wechatState.accounts.map((account, index) => {
    const active = account.fakeid === wechatState.selectedAccount?.fakeid ? " active" : "";
    return `
      <button class="wechat-source__account${active}" type="button" data-account-index="${index}">
        <span>${escapeHTML(account.nickname || "未命名公众号")}</span>
        <small>${escapeHTML(account.alias || account.fakeid)}</small>
      </button>
    `;
  }).join("");
  const articleRows = wechatState.accountArticles.map((article, index) => `
    <article class="wechat-source__article">
      ${article.cover ? `<img src="${escapeAttribute(article.cover)}" alt="">` : "<div class=\"wechat-source__cover\"></div>"}
      <div>
        <h3>${escapeHTML(article.title || "未命名文章")}</h3>
        <p>${escapeHTML(article.digest || formatArticleTime(article.update_time) || "暂无摘要")}</p>
        <div class="wechat-source__row-actions">
          <button type="button" class="button button-ghost" data-preview-article="${index}">预览</button>
          <button type="button" class="button button-primary" data-import-article="${index}">导入知识库</button>
          ${article.link ? `<a class="button button-link" href="${escapeAttribute(article.link)}" target="_blank" rel="noreferrer">原文</a>` : ""}
        </div>
      </div>
    </article>
  `).join("");
  const status = wechatState.loading
    ? `<div class="web-status">处理中：${escapeHTML(wechatState.loading)}</div>`
    : (wechatState.message ? `<div class="web-status">${escapeHTML(wechatState.message)}</div>` : "");

  renderShell(`
    <main class="wechat-source">
      <section class="wechat-source__header">
        <div>
          <p class="web-kicker">WeChat Source</p>
          <h1>微信公众号来源</h1>
        </div>
        ${status}
      </section>

      <div class="wechat-source__layout">
        <section class="wechat-source__panel">
          <form id="wechat-preview-form" class="source-form">
            <label>
              <span>文章链接</span>
              <input name="articleURL" value="${escapeAttribute(wechatState.articleURL)}" placeholder="https://mp.weixin.qq.com/s/..." autocomplete="off">
            </label>
            <label>
              <span>知识库 ID（可选）</span>
              <input name="bookID" value="${escapeAttribute(wechatState.bookID)}" placeholder="留空自动生成 wechat-...">
            </label>
            <div class="source-form__actions">
              <button class="button button-ghost" type="submit">预览文章</button>
              <button id="wechat-import" class="button button-primary" type="button">导入知识库</button>
            </div>
          </form>

          <form id="wechat-account-form" class="source-form source-form--compact">
            <label>
              <span>搜索公众号</span>
              <input name="accountQuery" value="${escapeAttribute(wechatState.accountQuery)}" placeholder="输入公众号名称">
            </label>
            <button class="button button-primary" type="submit">搜索公众号</button>
          </form>

          <div class="wechat-source__accounts">
            ${accountRows || "<p class=\"web-muted\">搜索后可选择公众号并加载最近文章。</p>"}
          </div>
        </section>

        <section class="wechat-source__panel wechat-source__main">
          <div class="wechat-source__section-head">
            <div>
              <p class="web-kicker">Recent Articles</p>
              <h2>最近文章</h2>
            </div>
            <div class="wechat-source__pager">
              <button class="button button-ghost" type="button" id="wechat-prev" ${wechatState.articleBegin <= 0 ? "disabled" : ""}>上一页</button>
              <button class="button button-ghost" type="button" id="wechat-next" ${wechatState.selectedAccount ? "" : "disabled"}>下一页</button>
            </div>
          </div>
          <div class="wechat-source__articles">
            ${articleRows || "<p class=\"web-muted\">选择公众号后显示文章；也可以直接粘贴文章链接导入。</p>"}
          </div>
        </section>

        <aside class="wechat-source__panel wechat-source__preview">
          ${renderWeChatPreview()}
        </aside>
      </div>

      ${renderWCPlusSource()}
    </main>
  `, "import");
  bindWeChatSourceEvents();
  bindWCPlusEvents();
}

function renderWCPlusPage() {
  renderShell(`
    <main class="source-control">
      ${renderSourceControlPlane()}
      <details id="wcplus-legacy-diagnostics" class="source-control__legacy" ${sourceControlState.legacyDiagnosticsOpen ? "open" : ""}>
        <summary>本地 API 诊断</summary>
        ${renderWCPlusSource(false)}
      </details>
      ${renderSourceRunDrawer()}
    </main>
  `, "wechat");
  bindSourceControlEvents();
  bindWCPlusEvents();
}

function renderSourceControlPlane() {
  const status = sourceControlState.loading
    ? `<div class="web-status">处理中：${escapeHTML(sourceControlState.loading)}</div>`
    : (sourceControlState.message ? `<div class="web-status">${escapeHTML(sourceControlState.message)}</div>` : "");
  return `
    <section class="source-control__header">
      <div>
        <p class="web-kicker">WeChat Collector</p>
        <h1>微信公众号采集器</h1>
        <p class="web-muted">登录状态与公众号搜索由本地 Agent 处理，凭据不会发送到 KBase。</p>
      </div>
      <div class="source-control__header-actions">
        ${status}
        <a id="source-agent-enrollment-link" class="button button-primary" href="http://127.0.0.1:8765" target="_blank" rel="noreferrer">本地登录与公众号搜索</a>
        <button id="source-control-refresh" class="button button-ghost" type="button">刷新</button>
      </div>
    </section>
    <div class="source-control__layout">
      <aside class="source-control__sidebar">
        ${renderSourceAgentList()}
        ${renderSourceSubscriptionList()}
      </aside>
      <section class="source-control__workspace">
        ${renderSourceRunHistory()}
      </section>
    </div>
  `;
}

function renderSourceAgentList() {
  const rows = sourceControlState.agents.map((agent) => {
    const online = sourceAgentIsOnline(agent);
    const capabilities = Array.isArray(agent.capabilities) ? agent.capabilities : [];
    const capabilityHealth = agent.capability_health && typeof agent.capability_health === "object"
      ? agent.capability_health
      : { wcplus: { healthy: Boolean(agent.wcplus_healthy), version: agent.wcplus_version || "", last_error: agent.last_error || "" } };
    const healthRows = Object.entries(capabilityHealth).map(([name, health]) => `
      <div><dt>${escapeHTML(name)}</dt><dd class="${health?.healthy ? "is-ok" : "is-bad"}">${health?.healthy ? "可用" : "不可用"}${health?.version ? ` · ${escapeHTML(health.version)}` : ""}${health?.requires_action ? ` · ${escapeHTML(health.requires_action)}` : ""}</dd></div>
    `).join("");
    return `
      <article class="source-control__agent ${online ? "is-online" : "is-offline"}">
        <div class="source-control__item-head">
          <strong>${escapeHTML(agent.agent_id || "未命名 Agent")}</strong>
          <span class="source-control__status ${online ? "is-ok" : "is-muted"}">${online ? "在线" : "离线"}</span>
        </div>
        <dl class="source-control__facts">
          <div><dt>心跳</dt><dd>${escapeHTML(formatSourceControlTime(agent.last_heartbeat_at))}</dd></div>
          <div><dt>Agent</dt><dd>${escapeHTML(agent.version || "-")}</dd></div>
          ${healthRows}
        </dl>
        <div class="source-control__capabilities">
          ${capabilities.map((capability) => `<span>${escapeHTML(capability)}</span>`).join("") || "<span>无能力上报</span>"}
        </div>
        ${agent.last_error ? `<p class="source-control__error">${escapeHTML(agent.last_error)}</p>` : ""}
      </article>
    `;
  }).join("");
  return `
    <section class="source-control__section">
      <div class="source-control__section-head">
        <h2>本地 Agent</h2>
        <span>${sourceControlState.agents.length}</span>
      </div>
      <div class="source-control__agent-list">
        ${rows || '<p class="web-muted">尚未收到 Agent 心跳。</p>'}
      </div>
    </section>
  `;
}

function renderSourceSubscriptionList() {
  const draft = sourceControlState.draft;
  const agentOptions = sourceControlState.agents.map((agent) => `
    <option value="${escapeAttribute(agent.agent_id)}" ${draft.sourceAgentID === agent.agent_id ? "selected" : ""}>${escapeHTML(agent.agent_id)}</option>
  `).join("");
  const rows = sourceControlState.subscriptions.map((subscription, index) => {
    const active = subscription.id === sourceControlState.selectedSubscriptionID ? " active" : "";
    return `
      <article class="source-control__subscription${active}">
        <button class="source-control__subscription-select" type="button" data-source-subscription-index="${index}">
          <strong>${escapeHTML(subscription.source_account || subscription.source_account_key)}</strong>
          <span>${escapeHTML(subscription.operation || "existing_articles")} · ${escapeHTML(formatSourceSchedule(subscription.schedule))}</span>
        </button>
        <div class="source-control__subscription-actions">
          <label class="source-control__toggle">
            <input type="checkbox" data-source-subscription-enabled="${index}" ${subscription.enabled ? "checked" : ""}>
            <span>启用</span>
          </label>
          <button class="button button-ghost" type="button" data-source-subscription-sync="${index}">立即同步</button>
        </div>
      </article>
    `;
  }).join("");
  return `
    <section class="source-control__section source-control__subscriptions">
      <div class="source-control__section-head">
        <h2>订阅</h2>
        <span>${sourceControlState.subscriptions.length}</span>
      </div>
      <form id="source-subscription-form" class="source-control__form">
        <h3>新建订阅</h3>
        <label>
          <span>公众号标识</span>
          <input name="sourceAccountKey" value="${escapeAttribute(draft.sourceAccountKey)}" placeholder="biz 或稳定来源键" required>
        </label>
        <label>
          <span>公众号名称</span>
          <input name="sourceAccount" value="${escapeAttribute(draft.sourceAccount)}" placeholder="显示名称">
        </label>
        <label>
          <span>执行 Agent</span>
          <select name="sourceAgentID">
            <option value="">自动分配</option>
            ${agentOptions}
          </select>
        </label>
        <label>
          <span>同步范围</span>
          <select name="sourceOperation">
            <option value="discover_articles" ${draft.sourceOperation === "discover_articles" ? "selected" : ""}>发现文章</option>
            <option value="sync_articles" ${draft.sourceOperation === "sync_articles" ? "selected" : ""}>同步文章</option>
            <option value="sync_media" ${draft.sourceOperation === "sync_media" ? "selected" : ""}>同步媒体</option>
          </select>
        </label>
        <div class="source-control__schedule-fields">
          <label>
            <span>计划</span>
            <select name="sourceScheduleMode">
              <option value="manual" ${draft.sourceScheduleMode === "manual" ? "selected" : ""}>手动</option>
              <option value="interval" ${draft.sourceScheduleMode === "interval" ? "selected" : ""}>固定间隔</option>
            </select>
          </label>
          <label>
            <span>间隔秒数</span>
            <input name="sourceIntervalSeconds" type="number" min="60" max="31536000" value="${escapeAttribute(draft.sourceIntervalSeconds)}" ${draft.sourceScheduleMode === "interval" ? "" : "disabled"}>
          </label>
        </div>
        <button class="button button-primary" type="submit">创建订阅</button>
      </form>
      <div class="source-control__subscription-list">
        ${rows || '<p class="web-muted">暂无订阅。</p>'}
      </div>
    </section>
  `;
}

function renderSourceRunHistory() {
  const subscription = selectedSourceSubscription();
  if (!subscription) {
    return `
      <div class="source-control__empty">
        <h2>运行历史</h2>
        <p>选择或新建订阅后显示同步运行。</p>
      </div>
    `;
  }
  const filters = [
    ["all", "全部"],
    ["queued", "等待中"],
    ["running", "运行中"],
    ["partial", "部分完成"],
    ["failed", "失败"],
    ["succeeded", "已完成"],
  ];
  const subscriptionRuns = sourceControlState.runs.filter((run) => run.subscription_id === subscription.id);
  const visibleRuns = subscriptionRuns.filter((run) => sourceRunMatchesFilter(run, sourceControlState.runFilter));
  const rows = visibleRuns.map((run) => {
    const active = sourceRunIsActive(run);
    const canRetry = run.status === "failed" || run.status === "partial";
    return `
      <article class="source-control__run ${sourceRunStatusClass(run.status)}">
        <div class="source-control__run-main">
          <div class="source-control__item-head">
            <span class="source-control__status">${escapeHTML(sourceRunStatusLabel(run.status))}</span>
            <time>${escapeHTML(formatSourceControlTime(run.created_at))}</time>
          </div>
          <strong>${escapeHTML(run.requested_operation || subscription.operation)}</strong>
          <span class="source-control__run-id">${escapeHTML(run.id)}</span>
          <div class="source-control__counters">
            <span>新增 <b>${run.new_count || 0}</b></span>
            <span>更新 <b>${run.updated_count || 0}</b></span>
            <span>跳过 <b>${run.skipped_count || 0}</b></span>
            <span>失败 <b>${run.failed_count || 0}</b></span>
          </div>
          ${run.error ? `<p class="source-control__error">${escapeHTML(run.error)}</p>` : ""}
        </div>
        <div class="source-control__run-actions">
          <button class="button button-ghost" type="button" data-source-run-detail="${escapeAttribute(run.id)}">详情</button>
          ${canRetry ? `<button class="button button-ghost" type="button" data-source-run-retry="${escapeAttribute(run.id)}">重试</button>` : ""}
          ${active ? `<button class="button button-ghost" type="button" data-source-run-cancel="${escapeAttribute(run.id)}">取消</button>` : ""}
        </div>
      </article>
    `;
  }).join("");
  return `
    <div class="source-control__workspace-head">
      <div>
        <p class="web-kicker">${escapeHTML(subscription.source_account_key)}</p>
        <h2>${escapeHTML(subscription.source_account || subscription.source_account_key)}</h2>
        <p>${escapeHTML(subscription.operation)} · ${escapeHTML(formatSourceSchedule(subscription.schedule))}</p>
      </div>
      <button class="button button-primary" type="button" data-source-subscription-sync="${sourceControlState.subscriptions.indexOf(subscription)}">立即同步</button>
    </div>
    <div class="source-control__history-head">
      <h3>运行历史</h3>
      <span>${subscriptionRuns.length}</span>
    </div>
    <div class="source-control__filters" role="tablist" aria-label="运行状态">
      ${filters.map(([value, label]) => `
        <button class="${sourceControlState.runFilter === value ? "active" : ""}" type="button" role="tab" data-source-run-filter="${value}" aria-selected="${sourceControlState.runFilter === value}">${label}</button>
      `).join("")}
    </div>
    <div class="source-control__run-list">
      ${rows || '<p class="web-muted">当前筛选下没有运行记录。</p>'}
    </div>
  `;
}

function renderSourceRunDrawer() {
  const detail = sourceControlState.runDetail;
  if (!detail?.run) {
    return "";
  }
  const run = detail.run;
  const items = Array.isArray(detail.items) ? detail.items : [];
  const active = sourceRunIsActive(run);
  const canRetry = run.status === "failed" || run.status === "partial";
  const itemRows = items.map((item) => `
    <article class="source-control__drawer-item ${item.outcome === "failed" ? "is-failed" : ""}">
      <div class="source-control__item-head">
        <strong>${escapeHTML(item.source_item_key || item.id)}</strong>
        <span class="source-control__status">${escapeHTML(item.outcome || "unknown")}</span>
      </div>
      ${item.error ? `<p class="source-control__error">${escapeHTML(item.error)}</p>` : ""}
      ${item.target_book_id ? `<a class="button button-link" href="${sourceKnowledgeURL(item.target_book_id)}">导入知识</a>` : ""}
    </article>
  `).join("");
  return `
    <aside class="source-control__drawer" role="dialog" aria-label="运行详情">
      <div class="source-control__drawer-head">
        <div>
          <p class="web-kicker">${escapeHTML(sourceRunStatusLabel(run.status))}</p>
          <h2>运行详情</h2>
        </div>
        <div class="source-control__drawer-actions">
          ${canRetry ? `<button class="button button-ghost" type="button" data-source-run-retry="${escapeAttribute(run.id)}">重试</button>` : ""}
          ${active ? `<button class="button button-ghost" type="button" data-source-run-cancel="${escapeAttribute(run.id)}">取消</button>` : ""}
          <button class="button button-ghost" type="button" data-source-drawer-close>关闭</button>
        </div>
      </div>
      <dl class="source-control__facts source-control__drawer-facts">
        <div><dt>运行 ID</dt><dd>${escapeHTML(run.id)}</dd></div>
        <div><dt>操作</dt><dd>${escapeHTML(run.requested_operation || "-")}</dd></div>
        <div><dt>开始</dt><dd>${escapeHTML(formatSourceControlTime(run.started_at || run.created_at))}</dd></div>
        <div><dt>结束</dt><dd>${escapeHTML(formatSourceControlTime(run.finished_at))}</dd></div>
      </dl>
      <div class="source-control__counters is-drawer">
        <span>新增 <b>${run.new_count || 0}</b></span>
        <span>更新 <b>${run.updated_count || 0}</b></span>
        <span>跳过 <b>${run.skipped_count || 0}</b></span>
        <span>失败 <b>${run.failed_count || 0}</b></span>
      </div>
      ${run.error ? `<p class="source-control__error">${escapeHTML(run.error)}</p>` : ""}
      <section class="source-control__drawer-items">
        <div class="source-control__history-head"><h3>条目</h3><span>${items.length}</span></div>
        ${itemRows || '<p class="web-muted">暂无条目。</p>'}
      </section>
    </aside>
  `;
}

function selectedSourceSubscription() {
  return sourceControlState.subscriptions.find((subscription) => subscription.id === sourceControlState.selectedSubscriptionID) || null;
}

function sourceAgentIsOnline(agent, now = Date.now()) {
  const heartbeat = Date.parse(String(agent?.last_heartbeat_at || ""));
  return Number.isFinite(heartbeat) && heartbeat <= now + 5000 && now - heartbeat <= 90000;
}

function sourceRunIsActive(run) {
  return ["queued", "leased", "running"].includes(String(run?.status || ""));
}

function activeRunForSubscription(subscriptionID) {
  return sourceControlState.runs.find((run) => run.subscription_id === subscriptionID && sourceRunIsActive(run)) || null;
}

function sourceRunMatchesFilter(run, filter) {
  if (filter === "all") {
    return true;
  }
  if (filter === "queued") {
    return run.status === "queued" || run.status === "leased";
  }
  return run.status === filter;
}

function sourceRunStatusLabel(status) {
  return ({
    queued: "等待中",
    leased: "已分配",
    running: "运行中",
    partial: "部分完成",
    failed: "失败",
    succeeded: "已完成",
    canceled: "已取消",
  })[status] || status || "未知";
}

function sourceRunStatusClass(status) {
  return `is-${String(status || "unknown").replace(/[^a-z0-9_-]/gi, "")}`;
}

function sourceKnowledgeURL(bookID) {
  return buildKnowledgePackageURL(String(bookID || "").trim());
}

function formatSourceControlTime(value) {
  if (!value) {
    return "-";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return String(value);
  }
  return parsed.toLocaleString("zh-CN", { hour12: false });
}

function formatSourceSchedule(schedule) {
  const value = String(schedule || "manual").trim();
  if (value === "manual") {
    return "手动";
  }
  const seconds = Number.parseInt(value.replace(/^interval:/, ""), 10);
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return value;
  }
  if (seconds % 86400 === 0) {
    return `每 ${seconds / 86400} 天`;
  }
  if (seconds % 3600 === 0) {
    return `每 ${seconds / 3600} 小时`;
  }
  if (seconds % 60 === 0) {
    return `每 ${seconds / 60} 分钟`;
  }
  return `每 ${seconds} 秒`;
}

function refreshWCPlusView() {
  if (isSourceControlPath()) {
    renderWCPlusPage();
    return;
  }
  renderWeChatSource();
}

function isSourceControlPath() {
  return window.location.pathname.startsWith("/wechat-source") || window.location.pathname.startsWith("/wcplus-source");
}

function sourceControlPrefillFromLocation() {
  const params = new URLSearchParams(window.location.search);
  const sourceAccountKey = String(params.get("source_account_key") || "").trim();
  if (!sourceAccountKey) {
    return;
  }
  sourceControlState.draft.sourceAccountKey = sourceAccountKey;
  sourceControlState.draft.sourceAccount = String(params.get("source_account") || sourceAccountKey).trim();
  sourceControlState.draft.sourceAgentID = String(params.get("agent_id") || sourceControlState.draft.sourceAgentID || "").trim();
  sourceControlState.draft.sourceOperation = "sync_articles";
}

async function bootstrapSourceControlPlane() {
  sourceControlState.message = "";
  await loadSourceControlPlane();
}

async function loadSourceControlPlane({ silent = false, renderResult = true } = {}) {
  const sequence = ++sourceControlLoadSequence;
  const previousSignature = sourceControlDataSignature();
  let shouldRender = !silent;
  if (!silent) {
    sourceControlState.loading = "加载来源状态";
    sourceControlState.message = "";
    renderWCPlusPage();
  }
  try {
    const [agentPayload, subscriptionPayload, runPayload] = await Promise.all([
      apiFetch("/api/source-agents"),
      apiFetch("/api/source-subscriptions"),
      apiFetch("/api/source-sync/runs?limit=200"),
    ]);
    if (sequence !== sourceControlLoadSequence) {
      return;
    }
    sourceControlState.agents = Array.isArray(agentPayload.agents) ? agentPayload.agents : [];
    sourceControlState.subscriptions = Array.isArray(subscriptionPayload.subscriptions) ? subscriptionPayload.subscriptions : [];
    sourceControlState.runs = Array.isArray(runPayload.runs) ? runPayload.runs : [];
    if (!sourceControlState.draft.sourceAgentID && sourceControlState.agents.length === 1) {
      sourceControlState.draft.sourceAgentID = sourceControlState.agents[0].agent_id || "";
    }
    const selectedStillExists = sourceControlState.subscriptions.some((subscription) => subscription.id === sourceControlState.selectedSubscriptionID);
    if (!selectedStillExists) {
      sourceControlState.selectedSubscriptionID = sourceControlState.subscriptions[0]?.id || "";
      sourceControlState.selectedRunID = "";
      sourceControlState.runDetail = null;
    }
    if (sourceControlState.selectedRunID) {
      try {
        const detail = await apiFetch(`/api/source-sync/runs/${encodeURIComponent(sourceControlState.selectedRunID)}`);
        if (sequence === sourceControlLoadSequence) {
          sourceControlState.runDetail = detail;
        }
      } catch {
        sourceControlState.selectedRunID = "";
        sourceControlState.runDetail = null;
      }
    }
    shouldRender = shouldRender || previousSignature !== sourceControlDataSignature();
    sourceControlState.message = `${sourceControlState.agents.length} 个 Agent · ${sourceControlState.subscriptions.length} 个订阅 · ${sourceControlState.runs.length} 次运行`;
  } catch (error) {
    if (sequence === sourceControlLoadSequence) {
      sourceControlState.message = error instanceof Error ? error.message : String(error);
      shouldRender = true;
    }
  } finally {
    if (sequence === sourceControlLoadSequence) {
      sourceControlState.loading = "";
      if (renderResult && shouldRender) {
        renderWCPlusPage();
      }
      scheduleSourceControlPoll();
    }
  }
}

function sourceControlDataSignature() {
  return JSON.stringify({
    agents: sourceControlState.agents,
    subscriptions: sourceControlState.subscriptions,
    runs: sourceControlState.runs,
    runDetail: sourceControlState.runDetail,
  });
}

function scheduleSourceControlPoll() {
  if (sourceControlPollTimer) {
    clearTimeout(sourceControlPollTimer);
    sourceControlPollTimer = null;
  }
  if (!isSourceControlPath()) {
    return;
  }
  if (!sourceControlState.runs.some(sourceRunIsActive)) {
    return;
  }
  sourceControlPollTimer = setTimeout(() => {
    sourceControlPollTimer = null;
    loadSourceControlPlane({ silent: true });
  }, 5000);
}

function bindSourceControlEvents() {
  document.querySelector("#source-control-refresh")?.addEventListener("click", () => {
    loadSourceControlPlane();
  });
  const form = document.querySelector("#source-subscription-form");
  form?.addEventListener("input", () => {
    readSourceSubscriptionDraft();
  });
  form?.addEventListener("change", (event) => {
    readSourceSubscriptionDraft();
    if (event.target?.name === "sourceScheduleMode") {
      const intervalInput = form.querySelector('[name="sourceIntervalSeconds"]');
      if (intervalInput) {
        intervalInput.disabled = sourceControlState.draft.sourceScheduleMode !== "interval";
      }
    }
  });
  form?.addEventListener("submit", async (event) => {
    event.preventDefault();
    readSourceSubscriptionDraft();
    await createSourceSubscription();
  });
  for (const button of document.querySelectorAll("[data-source-subscription-index]")) {
    button.addEventListener("click", () => {
      const index = Number(button.getAttribute("data-source-subscription-index") || "0");
      const subscription = sourceControlState.subscriptions[index];
      if (!subscription) {
        return;
      }
      sourceControlState.selectedSubscriptionID = subscription.id;
      sourceControlState.selectedRunID = "";
      sourceControlState.runDetail = null;
      sourceControlState.runFilter = "all";
      renderWCPlusPage();
      scheduleSourceControlPoll();
    });
  }
  for (const input of document.querySelectorAll("[data-source-subscription-enabled]")) {
    input.addEventListener("change", () => {
      const index = Number(input.getAttribute("data-source-subscription-enabled") || "0");
      const subscription = sourceControlState.subscriptions[index];
      if (subscription) {
        const enabled = Boolean(input.checked);
        input.setAttribute("aria-busy", "true");
        setSourceSubscriptionEnabled(subscription, enabled, input);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-source-subscription-sync]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-source-subscription-sync") || "0");
      const subscription = sourceControlState.subscriptions[index];
      if (subscription) {
        await syncSourceSubscriptionNow(subscription.id);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-source-run-filter]")) {
    button.addEventListener("click", () => {
      sourceControlState.runFilter = String(button.getAttribute("data-source-run-filter") || "all");
      renderWCPlusPage();
      scheduleSourceControlPoll();
    });
  }
  for (const button of document.querySelectorAll("[data-source-run-detail]")) {
    button.addEventListener("click", async () => {
      await loadSourceRunDetail(String(button.getAttribute("data-source-run-detail") || ""));
    });
  }
  for (const button of document.querySelectorAll("[data-source-run-retry]")) {
    button.addEventListener("click", async () => {
      await retrySourceRun(String(button.getAttribute("data-source-run-retry") || ""));
    });
  }
  for (const button of document.querySelectorAll("[data-source-run-cancel]")) {
    button.addEventListener("click", async () => {
      await cancelSourceRun(String(button.getAttribute("data-source-run-cancel") || ""));
    });
  }
  document.querySelector("[data-source-drawer-close]")?.addEventListener("click", () => {
    sourceControlState.selectedRunID = "";
    sourceControlState.runDetail = null;
    renderWCPlusPage();
    scheduleSourceControlPoll();
  });
  document.querySelector("#wcplus-legacy-diagnostics")?.addEventListener("toggle", async (event) => {
    sourceControlState.legacyDiagnosticsOpen = Boolean(event.currentTarget.open);
    if (event.currentTarget.open && !isWCPlusBootstrapped) {
      await bootstrapWCPlusSource();
    }
  });
}

function readSourceSubscriptionDraft() {
  const form = document.querySelector("#source-subscription-form");
  if (!form) {
    return;
  }
  const data = new FormData(form);
  sourceControlState.draft.sourceAccountKey = String(data.get("sourceAccountKey") || "").trim();
  sourceControlState.draft.sourceAccount = String(data.get("sourceAccount") || "").trim();
  sourceControlState.draft.sourceAgentID = String(data.get("sourceAgentID") || "").trim();
  sourceControlState.draft.sourceOperation = String(data.get("sourceOperation") || "sync_articles");
  sourceControlState.draft.sourceScheduleMode = String(data.get("sourceScheduleMode") || "manual");
  sourceControlState.draft.sourceIntervalSeconds = boundedNumber(data.get("sourceIntervalSeconds"), 60, 31536000, sourceControlState.draft.sourceIntervalSeconds);
}

async function createSourceSubscription() {
  const draft = sourceControlState.draft;
  if (!draft.sourceAccountKey) {
    sourceControlState.message = "请填写公众号标识。";
    renderWCPlusPage();
    return;
  }
  sourceControlState.loading = "创建订阅";
  renderWCPlusPage();
  try {
    const schedule = draft.sourceScheduleMode === "interval"
      ? `interval:${draft.sourceIntervalSeconds}`
      : "manual";
    const payload = await apiFetch("/api/source-subscriptions", {
      method: "POST",
      body: JSON.stringify({
        source_type: "wechat_mp_article",
        source_account_key: draft.sourceAccountKey,
        source_account: draft.sourceAccount || draft.sourceAccountKey,
        agent_id: draft.sourceAgentID,
        schedule,
        operation: draft.sourceOperation,
        options: { page_size: 10, max_items: 100, include_media: true, title_query: "" },
        enabled: true,
      }),
    });
    sourceControlState.selectedSubscriptionID = payload.subscription?.id || "";
    sourceControlState.draft.sourceAccountKey = "";
    sourceControlState.draft.sourceAccount = "";
    await loadSourceControlPlane({ silent: true });
    sourceControlState.message = "订阅已创建。";
  } catch (error) {
    sourceControlState.message = error instanceof Error ? error.message : String(error);
  } finally {
    sourceControlState.loading = "";
    renderWCPlusPage();
    scheduleSourceControlPoll();
  }
}

async function setSourceSubscriptionEnabled(subscription, enabled, control = null) {
  sourceControlState.loading = enabled ? "启用订阅" : "停用订阅";
  try {
    await apiFetch(`/api/source-subscriptions/${encodeURIComponent(subscription.id)}/enabled`, {
      method: "POST",
      body: JSON.stringify({ enabled }),
    });
    await loadSourceControlPlane({ silent: true, renderResult: false });
    sourceControlState.message = enabled ? "订阅已启用。" : "订阅已停用。";
  } catch (error) {
    sourceControlState.message = error instanceof Error ? error.message : String(error);
  } finally {
    sourceControlState.loading = "";
    const authoritative = sourceControlState.subscriptions.find((item) => item.id === subscription.id);
    if (control?.isConnected) {
      control.checked = authoritative ? Boolean(authoritative.enabled) : Boolean(subscription.enabled);
      control.removeAttribute("aria-busy");
    }
    const status = document.querySelector(".source-control__header-actions .web-status");
    if (status) {
      status.textContent = sourceControlState.message;
    }
    scheduleSourceControlPoll();
  }
}

async function syncSourceSubscriptionNow(subscriptionID) {
  sourceControlState.loading = "创建同步运行";
  renderWCPlusPage();
  try {
    const payload = await apiFetch(`/api/source-subscriptions/${encodeURIComponent(subscriptionID)}/sync`, {
      method: "POST",
      body: JSON.stringify({}),
    });
    sourceControlState.selectedSubscriptionID = subscriptionID;
    sourceControlState.selectedRunID = payload.run?.id || "";
    await loadSourceControlPlane({ silent: true });
    sourceControlState.message = "同步运行已进入队列。";
  } catch (error) {
    if (error?.status === 409 && await handleSourceSyncConflict(subscriptionID)) {
      return;
    }
    sourceControlState.message = error instanceof Error ? error.message : String(error);
  } finally {
    sourceControlState.loading = "";
    renderWCPlusPage();
    scheduleSourceControlPoll();
  }
}

async function handleSourceSyncConflict(subscriptionID) {
  await loadSourceControlPlane({ silent: true, renderResult: false });
  const activeRun = activeRunForSubscription(subscriptionID);
  if (!activeRun) {
    sourceControlState.message = "已有同步任务在进行中，请稍后刷新运行历史。";
    return true;
  }
  sourceControlState.selectedSubscriptionID = subscriptionID;
  sourceControlState.selectedRunID = activeRun.id;
  try {
    await loadSourceRunDetail(activeRun.id);
  } catch {
    sourceControlState.runDetail = { run: activeRun, items: [] };
  }
  sourceControlState.message = `已有同步任务在进行中：${sourceRunStatusLabel(activeRun.status)}。`;
  return true;
}

async function loadSourceRunDetail(runID) {
  if (!runID) {
    return;
  }
  sourceControlState.loading = "加载运行详情";
  renderWCPlusPage();
  try {
    sourceControlState.runDetail = await apiFetch(`/api/source-sync/runs/${encodeURIComponent(runID)}`);
    sourceControlState.selectedRunID = runID;
  } catch (error) {
    sourceControlState.message = error instanceof Error ? error.message : String(error);
  } finally {
    sourceControlState.loading = "";
    renderWCPlusPage();
    scheduleSourceControlPoll();
  }
}

async function retrySourceRun(runID) {
  if (!runID) {
    return;
  }
  sourceControlState.loading = "重试运行";
  renderWCPlusPage();
  try {
    const payload = await apiFetch(`/api/source-sync/runs/${encodeURIComponent(runID)}/retry`, {
      method: "POST",
      body: JSON.stringify({}),
    });
    sourceControlState.selectedRunID = payload.run?.id || "";
    await loadSourceControlPlane({ silent: true });
    sourceControlState.message = "重试运行已进入队列。";
  } catch (error) {
    sourceControlState.message = error instanceof Error ? error.message : String(error);
  } finally {
    sourceControlState.loading = "";
    renderWCPlusPage();
    scheduleSourceControlPoll();
  }
}

async function cancelSourceRun(runID) {
  if (!runID) {
    return;
  }
  sourceControlState.loading = "取消运行";
  renderWCPlusPage();
  try {
    await apiFetch(`/api/source-sync/runs/${encodeURIComponent(runID)}/cancel`, {
      method: "POST",
      body: JSON.stringify({}),
    });
    sourceControlState.selectedRunID = runID;
    await loadSourceControlPlane({ silent: true });
    sourceControlState.message = "运行已取消。";
  } catch (error) {
    sourceControlState.message = error instanceof Error ? error.message : String(error);
  } finally {
    sourceControlState.loading = "";
    renderWCPlusPage();
    scheduleSourceControlPoll();
  }
}

function firstValue(value, keys) {
  for (const key of keys) {
    const found = value?.[key];
    if (found !== undefined && found !== null && String(found).trim() !== "") {
      return found;
    }
  }
  return "";
}

function firstArray(value, keys) {
  for (const key of keys) {
    if (Array.isArray(value?.[key])) {
      return value[key];
    }
  }
  return [];
}

function wcplusAccountBiz(account) {
  return String(firstValue(account, ["biz", "Biz", "fakeid", "FakeID"]) || "");
}

function wcplusAccountNickname(account) {
  return String(firstValue(account, ["nickname", "Nickname", "name", "Name"]) || "");
}

function wcplusAccountArticleCount(account) {
  return firstValue(account, ["article_count", "ArticleCount", "Articles", "articles"]);
}

function wcplusArticleID(article) {
  return String(firstValue(article, ["id", "ID", "article_id", "ArticleID", "ArticleId", "articleId", "appmsgid", "AppMsgID", "app_msg_id", "msgid", "MsgID", "aid", "Aid"]) || "");
}

function wcplusArticleTitle(article) {
  return String(firstValue(article, ["title", "Title"]) || "");
}

function wcplusArticleNickname(article) {
  return String(firstValue(article, ["nickname", "Nickname", "gzh_nickname", "GzhNickname"]) || "");
}

function wcplusArticleDigest(article) {
  return String(firstValue(article, ["digest", "Digest", "summary", "Summary"]) || "");
}

function wcplusArticleURL(article) {
  return String(firstValue(article, ["url", "URL", "link", "Link", "content_url", "ContentURL", "source_url", "SourceURL"]) || "");
}

function wcplusArticlePublishTime(article) {
  return String(firstValue(article, ["publish_time", "PublishTime", "p_date_text", "PDateText", "pDateText", "date", "Date"]) || "");
}

function renderWCPlusSource(showOwnStatus = true) {
  const accountRows = wcplusState.accounts.map((account, index) => {
    const active = wcplusAccountBiz(account) === wcplusAccountBiz(wcplusState.selectedAccount) ? " active" : "";
    const nickname = wcplusAccountNickname(account) || "未命名公众号";
    const articleCount = wcplusAccountArticleCount(account);
    return `
      <button class="wcplus-source__account${active}" type="button" data-wcplus-account-index="${index}">
        <span>${escapeHTML(nickname)}</span>
        <small>${escapeHTML([wcplusAccountBiz(account), articleCount ? `${articleCount} 篇` : ""].filter(Boolean).join(" · "))}</small>
      </button>
    `;
  }).join("");
  const articleRows = wcplusState.articles.map((article, index) => {
    const id = wcplusArticleID(article);
    const articleURL = wcplusArticleURL(article);
    return `
    <article class="wcplus-source__article">
      <div>
        <h3>${escapeHTML(wcplusArticleTitle(article) || id || "未命名文章")}</h3>
        <p>${escapeHTML([wcplusArticleDigest(article), wcplusArticlePublishTime(article)].filter(Boolean).join(" · ") || articleURL || "暂无摘要")}</p>
      </div>
      <div class="wcplus-source__row-actions">
        <button type="button" class="button button-ghost" data-wcplus-preview="${index}" ${id || articleURL ? "" : "disabled"}>预览</button>
        <button type="button" class="button button-primary" data-wcplus-import="${index}" ${id || articleURL ? "" : "disabled"}>导入知识库</button>
      </div>
    </article>
  `}).join("");
  const searchRows = wcplusState.searchResults.map((item, index) => {
    const articleID = wcplusArticleID(item);
    const articleURL = wcplusArticleURL(item);
    const accountBiz = wcplusAccountBiz(item);
    const title = wcplusArticleTitle(item) || wcplusAccountNickname(item) || articleID || accountBiz || "结果";
    const subline = [wcplusArticleDigest(item), wcplusArticlePublishTime(item), wcplusArticleURL(item), accountBiz].filter(Boolean).join(" · ");
    return `
      <article class="wcplus-source__search-result">
        <div>
          <h4>${escapeHTML(title)}</h4>
          <p>${escapeHTML(subline || "命中结果")}</p>
        </div>
        <div class="wcplus-source__row-actions">
          ${accountBiz ? `<button type="button" class="button button-ghost" data-wcplus-select-result-account="${index}">选择</button>` : ""}
          ${accountBiz ? `<button type="button" class="button button-ghost" data-wcplus-sync-result-account="${index}">同步</button>` : ""}
          ${articleID || articleURL ? `<button type="button" class="button button-ghost" data-wcplus-preview-result="${index}">预览</button>` : ""}
          ${articleID || articleURL ? `<button type="button" class="button button-primary" data-wcplus-import-result="${index}">导入</button>` : ""}
        </div>
      </article>
    `;
  }).join("");
  const taskRows = wcplusState.tasks.map((task) => {
    const progress = [];
    if (task.article_total) {
      progress.push(`正文 ${task.article_finished || 0}/${task.article_total}`);
    }
    if (task.reading_total) {
      progress.push(`阅读数据 ${task.reading_finished || 0}/${task.reading_total}`);
    }
    return `
      <div class="wcplus-source__task">
        <div>
          <strong>${escapeHTML(task.nickname || task.biz || task.task_id)}</strong>
          <span>${escapeHTML([task.crawler_type, task.status || "unknown", progress.join(" · ")].filter(Boolean).join(" · "))}</span>
          ${task.status_error ? `<small class="is-bad">${escapeHTML(task.status_error)}</small>` : ""}
        </div>
      </div>
    `;
  }).join("");
  const rawImportedBook = wcplusState.rawImported?.book;
  const rawImportedHTML = rawImportedBook
    ? `<div class="wcplus-source__imported">
        <strong>已导入：${escapeHTML(rawImportedBook.title || rawImportedBook.book_id || "WC Plus 文章")}</strong>
        <a href="/book-knowledge" data-link>打开书籍知识库</a>
      </div>`
    : "";
  const importedBooksHTML = renderWCPlusImportedBooks();
  const status = wcplusState.loading
    ? `<div class="web-status">处理中：${escapeHTML(wcplusState.loading)}</div>`
    : (wcplusState.message ? `<div class="web-status">${escapeHTML(wcplusState.message)}</div>` : "");
  const serviceStatus = wcplusState.serviceStatus
    ? `<span class="wcplus-source__badge ${wcplusState.serviceStatus.ok ? "is-ok" : "is-bad"}">${wcplusState.serviceStatus.ok ? "已连接" : "未连接"}</span>`
    : "";
  const envCheckHTML = wcplusState.envCheck ? `
    <section class="wcplus-source__env">
      <div class="wcplus-source__toolbar is-tight">
        <div>
          <p class="web-kicker">Environment</p>
          <h3>环境诊断</h3>
        </div>
        <span class="wcplus-source__badge ${wcplusState.envCheck.ok ? "is-ok" : "is-bad"}">${wcplusState.envCheck.ok ? "通过" : "需处理"}</span>
      </div>
      <div class="wcplus-source__env-list">
        <div class="wcplus-source__env-row wcplus-source__diagnostic-line">
          <strong>服务地址</strong>
          <code>${escapeHTML(wcplusState.envCheck.base_url || "-")}</code>
          <small>这是 kbase 服务端实际访问的 WC Plus API 地址。</small>
        </div>
        ${(Array.isArray(wcplusState.envCheck.checks) ? wcplusState.envCheck.checks : []).map((item) => `
          <div class="wcplus-source__env-row">
            <strong>${escapeHTML(item.name || "check")}</strong>
            <span class="${item.ok ? "is-ok" : "is-bad"}">${item.ok ? "OK" : "FAIL"}</span>
            <small>${escapeHTML(item.message || "-")}</small>
          </div>
        `).join("")}
      </div>
      ${Array.isArray(wcplusState.envCheck.advice) && wcplusState.envCheck.advice.length ? `
        <ul class="wcplus-source__env-advice">
          ${wcplusState.envCheck.advice.map((item) => `<li>${escapeHTML(item)}</li>`).join("")}
        </ul>
      ` : ""}
      <div class="wcplus-source__row-actions">
        <button class="button button-ghost" type="button" data-wcplus-copy-diagnostics>复制诊断</button>
      </div>
    </section>
  ` : "";
  const batchResultHTML = wcplusState.batchResult ? `
    <section class="wcplus-source__batch-result">
      <div class="wcplus-source__toolbar is-tight">
        <div>
          <p class="web-kicker">Batch Result</p>
          <h3>批量结果</h3>
        </div>
        <span class="wcplus-source__badge">成功 ${Array.isArray(wcplusState.batchResult.success) ? wcplusState.batchResult.success.length : 0} / 失败 ${Array.isArray(wcplusState.batchResult.failed) ? wcplusState.batchResult.failed.length : 0} / 入库 ${wcplusState.batchResult.imported_count || 0}</span>
      </div>
      <label>
        <span>成功清单</span>
        <textarea readonly>${escapeHTML(wcplusState.batchResult.success_text || "无成功项")}</textarea>
      </label>
      <label>
        <span>失败清单</span>
        <textarea readonly>${escapeHTML(wcplusState.batchResult.failed_text || "无失败项")}</textarea>
      </label>
      ${Array.isArray(wcplusState.batchResult.import_errors) && wcplusState.batchResult.import_errors.length ? `
        <label>
          <span>入库错误</span>
          <textarea readonly>${escapeHTML(wcplusState.batchResult.import_errors.join("\n"))}</textarea>
        </label>
      ` : ""}
      <div class="wcplus-source__row-actions">
        <button class="button button-ghost" type="button" data-wcplus-copy-batch="success">复制成功</button>
        <button class="button button-ghost" type="button" data-wcplus-copy-batch="failed">复制失败</button>
      </div>
    </section>
  ` : "";
  return `
    <section class="wcplus-source">
      <div class="wcplus-source__toolbar">
        <div>
          <p class="web-kicker">WC Plus Local API</p>
          <h2>WC Plus 本地服务</h2>
          <p class="web-muted">启动时自动检查环境；服务不可达时仍可用下方手动导入知识库。</p>
        </div>
        <div class="wcplus-source__actions">
          ${serviceStatus}
          <button id="wcplus-check-status" class="button button-ghost" type="button">检查状态</button>
          <button id="wcplus-check-env" class="button button-ghost" type="button">环境检查</button>
          <button id="wcplus-load-accounts" class="button button-primary" type="button">加载公众号</button>
          <button id="wcplus-load-tasks" class="button button-ghost" type="button">下载任务</button>
          <button id="wcplus-run-queue" class="button button-ghost" type="button">启动队列</button>
        </div>
      </div>
      <div class="wcplus-source__utility">
        <span>辅助查询</span>
        <button class="button button-ghost" type="button" data-wcplus-utility="reading" ${wcplusState.selectedAccount ? "" : "disabled"}>阅读数据</button>
        <button class="button button-ghost" type="button" data-wcplus-utility="statistics" ${wcplusState.selectedAccount ? "" : "disabled"}>统计数据</button>
        <button class="button button-ghost" type="button" data-wcplus-utility="owner" ${wcplusState.preview || wcplusState.articles.length ? "" : "disabled"}>公众号详情</button>
        <button class="button button-ghost" type="button" data-wcplus-utility="likes">收藏文章</button>
        <button class="button button-ghost" type="button" data-wcplus-utility="request" ${wcplusState.selectedAccount ? "" : "disabled"}>请求公众号</button>
      </div>
      ${showOwnStatus ? status : ""}
      ${envCheckHTML}
      <div class="wcplus-source__grid">
        <aside class="wcplus-source__panel">
          <form id="wcplus-search-form" class="source-form source-form--flat">
            <label>
              <span>搜索 WC Plus</span>
              <input name="query" value="${escapeAttribute(wcplusState.searchQuery)}" placeholder="标题、全文或公众号">
            </label>
            <label>
              <span>范围</span>
              <select name="mode">
                <option value="fulltext" ${wcplusState.searchMode === "fulltext" ? "selected" : ""}>全文</option>
                <option value="title" ${wcplusState.searchMode === "title" ? "selected" : ""}>标题</option>
                <option value="account" ${wcplusState.searchMode === "account" ? "selected" : ""}>已入库公众号</option>
                <option value="candidate" ${wcplusState.searchMode === "candidate" ? "selected" : ""}>可导入公众号</option>
                <option value="all" ${wcplusState.searchMode === "all" ? "selected" : ""}>全库文章</option>
              </select>
            </label>
            <label>
              <span>搜索每页</span>
              <input name="searchNum" type="number" min="1" max="100" value="${escapeAttribute(wcplusState.searchNum)}">
            </label>
            <button class="button button-primary" type="submit">搜索</button>
          </form>
          <form id="wcplus-account-options-form" class="wcplus-source__mini-form">
            <label>
              <span>每页</span>
              <input name="accountNum" type="number" min="1" max="100" value="${escapeAttribute(wcplusState.accountNum)}">
            </label>
            <div class="wcplus-source__pager">
              <button class="button button-ghost" type="button" data-wcplus-account-page="-1" ${wcplusState.accountOffset <= 0 ? "disabled" : ""}>上一页</button>
              <button class="button button-ghost" type="button" data-wcplus-account-page="1">下一页</button>
            </div>
          </form>
          <div class="wcplus-source__accounts">
            ${accountRows || "<p class=\"web-muted\">启动 WC Plus 本地服务后，可加载已同步公众号。</p>"}
          </div>
        </aside>
        <section class="wcplus-source__panel wcplus-source__articles">
          <div class="wcplus-source__toolbar is-tight">
            <div>
              <p class="web-kicker">Articles</p>
              <h3>${escapeHTML(wcplusAccountNickname(wcplusState.selectedAccount) || "公众号文章")}</h3>
            </div>
            <div class="wcplus-source__actions">
              <button id="wcplus-create-task" class="button button-ghost" type="button" ${wcplusState.selectedAccount ? "" : "disabled"}>同步公众号</button>
              <button id="wcplus-create-batch-task" class="button button-ghost" type="button">批量任务</button>
              <button id="wcplus-export-text" class="button button-ghost" type="button" ${wcplusState.selectedAccount ? "" : "disabled"}>导出 TXT</button>
              <button id="wcplus-export-csv" class="button button-ghost" type="button" ${wcplusState.selectedAccount ? "" : "disabled"}>导出 CSV</button>
              <button id="wcplus-import-account" class="button button-primary" type="button" ${wcplusState.selectedAccount ? "" : "disabled"}>批量导入</button>
            </div>
          </div>
          <form id="wcplus-work-options-form" class="wcplus-source__options">
            <label>
              <span>任务类型</span>
              <select name="taskCrawlerType">
                <option value="gzh_article_link" ${wcplusState.taskCrawlerType === "gzh_article_link" ? "selected" : ""}>公众号链接</option>
                <option value="article" ${wcplusState.taskCrawlerType === "article" ? "selected" : ""}>文章内容</option>
                <option value="reading_data" ${wcplusState.taskCrawlerType === "reading_data" ? "selected" : ""}>阅读数据任务</option>
              </select>
            </label>
            <label>
              <span>抓取范围</span>
              <select name="taskArticleListType">
                <option value="all" ${wcplusState.taskArticleListType === "all" ? "selected" : ""}>全部</option>
                <option value="amount" ${wcplusState.taskArticleListType === "amount" ? "selected" : ""}>指定篇数</option>
              </select>
            </label>
            <label>
              <span>抓取篇数</span>
              <input name="articleListAmount" type="number" min="0" max="1000" value="${escapeAttribute(wcplusState.taskArticleListAmount)}">
            </label>
            <label>
              <span>开始时间（Unix 秒）</span>
              <input name="articleListDate" type="number" min="0" value="${escapeAttribute(wcplusState.taskArticleListDate)}">
            </label>
            <label>
              <span>抓取偏移</span>
              <input name="articleListOffset" type="number" min="0" value="${escapeAttribute(wcplusState.taskArticleListOffset)}">
            </label>
            <label class="wcplus-source__inline-check">
              <input name="articleRefresh" type="checkbox" ${wcplusState.taskArticleRefresh ? "checked" : ""}>
              <span>文章刷新</span>
            </label>
            <label class="wcplus-source__inline-check">
              <input name="articleImageDownload" type="checkbox" ${wcplusState.taskArticleImageDownload ? "checked" : ""}>
              <span>下载正文图片</span>
            </label>
            <label>
              <span>阅读范围</span>
              <select name="readingDataType">
                <option value="all" ${wcplusState.taskReadingDataType === "all" ? "selected" : ""}>全部</option>
                <option value="date" ${wcplusState.taskReadingDataType === "date" ? "selected" : ""}>时间区间</option>
                <option value="amount" ${wcplusState.taskReadingDataType === "amount" ? "selected" : ""}>指定篇数</option>
              </select>
            </label>
            <label>
              <span>阅读开始（Unix 秒）</span>
              <input name="readingDataStartDate" type="number" min="0" value="${escapeAttribute(wcplusState.taskReadingDataStartDate)}">
            </label>
            <label>
              <span>阅读结束（Unix 秒）</span>
              <input name="readingDataEndDate" type="number" min="0" value="${escapeAttribute(wcplusState.taskReadingDataEndDate)}">
            </label>
            <label>
              <span>阅读数据篇数</span>
              <input name="readingDataAmount" type="number" min="0" max="1000" value="${escapeAttribute(wcplusState.taskReadingDataAmount)}">
            </label>
            <label class="wcplus-source__inline-check">
              <input name="readingDataOnlyMain" type="checkbox" ${wcplusState.taskReadingDataOnlyMain ? "checked" : ""}>
              <span>仅头条</span>
            </label>
            <label class="wcplus-source__inline-check">
              <input name="readingDataRefresh" type="checkbox" ${wcplusState.taskReadingDataRefresh ? "checked" : ""}>
              <span>刷新阅读数据</span>
            </label>
            <label>
              <span>导入篇数</span>
              <input name="importLimit" type="number" min="1" max="100" value="${escapeAttribute(wcplusState.importLimit)}">
            </label>
            <label>
              <span>最近导出</span>
              <input name="exportRecentNum" type="number" min="1" max="5000" value="${escapeAttribute(wcplusState.exportRecentNum)}">
            </label>
            <label>
              <span>文章每页</span>
              <input name="articleNum" type="number" min="1" max="100" value="${escapeAttribute(wcplusState.articleNum)}">
            </label>
          </form>
          <div class="wcplus-source__pager is-article">
            <button class="button button-ghost" type="button" data-wcplus-article-page="-1" ${wcplusState.articleOffset <= 0 ? "disabled" : ""}>上一页</button>
            <button class="button button-ghost" type="button" data-wcplus-article-page="1" ${wcplusState.selectedAccount ? "" : "disabled"}>下一页</button>
          </div>
          <div class="wcplus-source__pager is-search">
            <span>搜索结果 ${wcplusState.searchResults.length ? `${wcplusState.searchOffset + 1} - ${wcplusState.searchOffset + wcplusState.searchResults.length}` : "0"}</span>
            <button class="button button-ghost" type="button" data-wcplus-search-page="-1" ${wcplusState.searchOffset <= 0 ? "disabled" : ""}>上一页</button>
            <button class="button button-ghost" type="button" data-wcplus-search-page="1" ${wcplusState.searchResults.length ? "" : "disabled"}>下一页</button>
          </div>
          <div class="wcplus-source__search-results">
            ${searchRows || ""}
          </div>
          <div class="wcplus-source__article-list">
            ${articleRows || "<p class=\"web-muted\">选择公众号后显示已下载文章。</p>"}
          </div>
        </section>
        <aside class="wcplus-source__panel wcplus-source__preview">
          ${renderWCPlusPreview()}
        </aside>
      </div>
      <section class="wcplus-source__tasks">
        <div class="wcplus-source__toolbar is-tight">
          <div>
            <p class="web-kicker">Tasks</p>
            <h3>下载任务</h3>
          </div>
          <div class="wcplus-source__actions">
            <button id="wcplus-clean-batch-tasks" class="button button-ghost" type="button">清理 ready/error</button>
            <button id="wcplus-export-xlsx" class="button button-primary" type="button">导出全库 XLSX</button>
          </div>
        </div>
        <form id="wcplus-batch-nickname-form" class="wcplus-source__batch-form">
          <label>
            <span>批量导入公众号昵称</span>
            <textarea name="nicknames" placeholder="每行一个公众号昵称，严格精确匹配">${escapeHTML(wcplusState.batchNicknames)}</textarea>
          </label>
          <label>
            <span>抓取范围</span>
            <select name="batchArticleListType">
              <option value="all" ${wcplusState.batchArticleListType === "all" ? "selected" : ""}>全部</option>
              <option value="amount" ${wcplusState.batchArticleListType === "amount" ? "selected" : ""}>指定篇数</option>
            </select>
          </label>
          <label>
            <span>抓取篇数</span>
            <input name="batchArticleListAmount" type="number" min="0" max="1000" value="${escapeAttribute(wcplusState.batchArticleListAmount)}">
          </label>
          <label class="wcplus-source__inline-check">
            <input name="exactMatch" type="checkbox" ${wcplusState.batchExactMatch ? "checked" : ""}>
            <span>昵称精确匹配</span>
          </label>
          <label class="wcplus-source__inline-check">
            <input name="importToKBase" type="checkbox" ${wcplusState.batchImportToKBase ? "checked" : ""}>
            <span>同步后导入书籍知识库</span>
          </label>
          <label class="wcplus-source__inline-check">
            <input name="waitForCompletion" type="checkbox" ${wcplusState.batchWaitForCompletion ? "checked" : ""}>
            <span>等待任务完成后入库</span>
          </label>
          <label>
            <span>入库篇数</span>
            <input name="batchImportLimit" type="number" min="1" max="100" value="${escapeAttribute(wcplusState.batchImportLimit)}">
          </label>
          <button class="button button-primary" type="submit">创建链接任务并启动队列</button>
        </form>
        ${batchResultHTML}
        <form id="wcplus-raw-import-form" class="wcplus-source__manual-form">
          <label>
            <span>原文标题</span>
            <input name="rawTitle" value="${escapeAttribute(wcplusState.rawTitle)}" placeholder="文章标题">
          </label>
          <label>
            <span>公众号/作者</span>
            <input name="rawNickname" value="${escapeAttribute(wcplusState.rawNickname)}" placeholder="公众号或作者">
          </label>
          <label>
            <span>原文链接</span>
            <input name="rawURL" value="${escapeAttribute(wcplusState.rawURL)}" placeholder="https://mp.weixin.qq.com/s/...">
          </label>
          <label>
            <span>知识库 ID（可选）</span>
            <input name="rawBookID" value="${escapeAttribute(wcplusState.rawBookID)}" placeholder="留空自动生成">
          </label>
          <label class="is-wide">
            <span>导入 TXT / Markdown 文件</span>
            <input name="rawFile" type="file" accept=".txt,.md,.markdown,text/plain,text/markdown">
          </label>
          <label class="is-wide">
            <span>正文 Markdown / 纯文本</span>
            <textarea name="rawContent" placeholder="粘贴 WC Plus 导出的正文、Markdown 或清洗后的纯文本">${escapeHTML(wcplusState.rawContent)}</textarea>
          </label>
          <div class="wcplus-source__manual-actions">
            <button class="button button-primary" type="submit">手动导入知识库</button>
          </div>
        </form>
        ${rawImportedHTML}
        ${importedBooksHTML}
        ${taskRows || "<p class=\"web-muted\">点击“下载任务”查看 WC Plus 同步任务。</p>"}
      </section>
    </section>
  `;
}

function wcplusBooksFromPayload(payload) {
  const books = [];
  if (payload?.book) {
    books.push(payload.book);
  }
  if (Array.isArray(payload?.books)) {
    books.push(...payload.books);
  }
  if (Array.isArray(payload?.imported_books)) {
    books.push(...payload.imported_books);
  }
  return books.filter((book) => book && (book.book_id || book.title));
}

function rememberWCPlusImportedBooks(payload) {
  const incoming = wcplusBooksFromPayload(payload);
  if (!incoming.length) {
    return [];
  }
  const merged = [...incoming, ...wcplusState.importedPackages];
  const seen = new Set();
  wcplusState.importedPackages = merged.filter((book) => {
    const key = book.book_id || book.title;
    if (!key || seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  }).slice(0, 8);
  return incoming;
}

function renderWCPlusImportedBooks() {
  if (!wcplusState.importedPackages.length) {
    return "";
  }
  const rows = wcplusState.importedPackages.map((book) => {
    const id = book.book_id || "";
    const title = book.title || id || "WC Plus 文章";
    return `
      <li>
        <strong>${escapeHTML(title)}</strong>
        <span>${escapeHTML(id)}</span>
        <div class="wcplus-source__row-actions">
          ${id ? `<a class="button button-ghost" href="/book-knowledge?book_id=${encodeURIComponent(id)}" data-link>知识库</a>` : ""}
          ${id ? `<a class="button button-ghost" href="${escapeAttribute(buildBookReaderURL(id))}" data-link>阅读</a>` : ""}
        </div>
      </li>
    `;
  }).join("");
  return `
    <section class="wcplus-source__imported-books">
      <div class="wcplus-source__toolbar is-tight">
        <div>
          <p class="web-kicker">Imported</p>
          <h3>最近入库</h3>
        </div>
      </div>
      <ul>${rows}</ul>
    </section>
  `;
}

async function bootstrapWCPlusSource() {
  if (isWCPlusBootstrapped) {
    return;
  }
  isWCPlusBootstrapped = true;
  wcplusState.loading = "启动时自动检查环境";
  wcplusState.message = "启动时自动检查环境，加载诊断、任务和公众号列表。";
  refreshWCPlusView();

  const accountQuery = new URLSearchParams({
    offset: String(wcplusState.accountOffset),
    num: String(wcplusState.accountNum),
  });
  const [envResult, taskResult, accountResult] = await Promise.allSettled([
    apiFetch("/api/wcplus/env/check"),
    apiFetch("/api/wcplus/task/all"),
    apiFetch(`/api/wcplus/gzh/list?${accountQuery.toString()}`),
  ]);

  const failures = [];
  if (envResult.status === "fulfilled") {
    wcplusState.envCheck = envResult.value;
    wcplusState.serviceStatus = { ok: Boolean(envResult.value?.ok) };
    if (!envResult.value?.ok) {
      failures.push("环境检查");
    }
  } else {
    wcplusState.serviceStatus = { ok: false };
    failures.push("环境检查");
  }

  if (taskResult.status === "fulfilled") {
    wcplusState.tasks = Array.isArray(taskResult.value.tasks) ? taskResult.value.tasks : [];
  } else {
    failures.push("任务列表");
  }

  if (accountResult.status === "fulfilled") {
    wcplusState.accounts = Array.isArray(accountResult.value.accounts) ? accountResult.value.accounts : [];
    wcplusState.selectedAccount = wcplusState.accounts[0] || null;
    if (wcplusState.selectedAccount) {
      await loadWCPlusArticles(false);
    }
  } else {
    failures.push("公众号列表");
  }

  wcplusState.loading = "";
  if (failures.length) {
    wcplusState.message = `启动检查完成，但 ${failures.join("、")} 需要处理；可继续使用手动导入知识库。`;
  } else {
    wcplusState.message = `启动检查完成：${wcplusState.accounts.length} 个公众号，${wcplusState.tasks.length} 个任务。`;
  }
  refreshWCPlusView();
}

function renderWCPlusPreview() {
  const article = wcplusState.preview;
  const utility = wcplusState.utilityResult;
  const utilityHTML = utility ? `
    <div class="wcplus-source__utility-result">
      <p class="web-kicker">Auxiliary Result</p>
      <h4>${escapeHTML(utility.title || "辅助查询结果")}</h4>
      <pre>${escapeHTML(JSON.stringify(utility.payload || {}, null, 2).slice(0, 3200))}</pre>
    </div>
  ` : "";
  if (!article) {
    return `
      <p class="web-kicker">WC Plus Preview</p>
      <h3>等待文章预览</h3>
      <p class="web-muted">从 WC Plus 已下载文章中选择预览，确认内容后可导入书籍知识库。</p>
      ${utilityHTML}
    `;
  }
  return `
    <p class="web-kicker">WC Plus Preview</p>
    <h3>${escapeHTML(wcplusArticleTitle(article) || wcplusArticleID(article) || "未命名文章")}</h3>
    <div class="wechat-source__meta">
      <span>${escapeHTML(wcplusArticleNickname(article) || "未知公众号")}</span>
      <span>${escapeHTML(wcplusArticlePublishTime(article) || "")}</span>
    </div>
    <pre>${escapeHTML(String(firstValue(article, ["content", "Content"]) || "").slice(0, 2200))}</pre>
    ${utilityHTML}
  `;
}

function renderWeChatPreview() {
  const article = wechatState.preview;
  if (!article) {
    return `
      <p class="web-kicker">Preview</p>
      <h2>等待预览</h2>
      <p class="web-muted">文章预览会展示标题、公众号、摘要和正文片段。导入后会生成单篇文章知识包。</p>
      ${wechatState.imported ? renderImportedPackage() : ""}
    `;
  }
  return `
    <p class="web-kicker">Preview</p>
    <h2>${escapeHTML(article.title || "未命名文章")}</h2>
    <div class="wechat-source__meta">
      <span>${escapeHTML(article.account_name || "未知公众号")}</span>
      <span>${escapeHTML(article.published_at || "")}</span>
    </div>
    ${article.digest ? `<p class="wechat-source__digest">${escapeHTML(article.digest)}</p>` : ""}
    <pre>${escapeHTML((article.markdown || article.text || "").slice(0, 2600))}</pre>
    ${wechatState.imported ? renderImportedPackage() : ""}
  `;
}

function renderImportedPackage() {
  const book = wechatState.imported?.book || {};
  const id = book.book_id || "";
  return `
    <div class="wechat-source__imported">
      <p class="web-kicker">Imported</p>
      <strong>${escapeHTML(book.title || id || "已导入")}</strong>
      ${id ? `<a href="/ebook/${encodeURIComponent(id)}">打开阅读页</a>` : ""}
    </div>
  `;
}

function bindWeChatSourceEvents() {
  document.querySelector("#wechat-preview-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    wechatState.articleURL = String(data.get("articleURL") || "").trim();
    wechatState.bookID = String(data.get("bookID") || "").trim();
    await previewWeChatArticle(wechatState.articleURL);
  });

  document.querySelector("#wechat-import")?.addEventListener("click", async () => {
    await importWeChatArticle(wechatState.articleURL);
  });

  document.querySelector("#wechat-account-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    wechatState.accountQuery = String(data.get("accountQuery") || "").trim();
    await searchWeChatAccounts();
  });

  for (const button of document.querySelectorAll("[data-account-index]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-account-index") || "0");
      wechatState.selectedAccount = wechatState.accounts[index] || null;
      wechatState.articleBegin = 0;
      await loadWeChatAccountArticles();
    });
  }

  for (const button of document.querySelectorAll("[data-preview-article]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-preview-article") || "0");
      const article = wechatState.accountArticles[index];
      if (article?.link) {
        wechatState.articleURL = article.link;
        await previewWeChatArticle(article.link);
      }
    });
  }

  for (const button of document.querySelectorAll("[data-import-article]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-import-article") || "0");
      const article = wechatState.accountArticles[index];
      if (article?.link) {
        wechatState.articleURL = article.link;
        await importWeChatArticle(article.link);
      }
    });
  }

  document.querySelector("#wechat-prev")?.addEventListener("click", async () => {
    wechatState.articleBegin = Math.max(0, wechatState.articleBegin - wechatState.articleCount);
    await loadWeChatAccountArticles();
  });

  document.querySelector("#wechat-next")?.addEventListener("click", async () => {
    wechatState.articleBegin += wechatState.articleCount;
    await loadWeChatAccountArticles();
  });
}

function bindWCPlusEvents() {
  document.querySelector("#wcplus-account-options-form")?.addEventListener("change", () => {
    readWCPlusOptionsFromDOM();
  });
  document.querySelector("#wcplus-work-options-form")?.addEventListener("change", () => {
    readWCPlusOptionsFromDOM();
  });
  document.querySelector("#wcplus-check-status")?.addEventListener("click", () => {
    checkWCPlusStatus();
  });
  document.querySelector("#wcplus-check-env")?.addEventListener("click", () => {
    checkWCPlusEnvironment();
  });
  document.querySelector("#wcplus-load-accounts")?.addEventListener("click", () => {
    loadWCPlusAccounts();
  });
  document.querySelector("#wcplus-load-tasks")?.addEventListener("click", () => {
    loadWCPlusTasks();
  });
  document.querySelector("#wcplus-run-queue")?.addEventListener("click", () => {
    runWCPlusQueue();
  });
  document.querySelector("#wcplus-search-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    wcplusState.searchQuery = String(data.get("query") || "").trim();
    wcplusState.searchMode = String(data.get("mode") || "fulltext");
    wcplusState.searchNum = boundedNumber(data.get("searchNum"), 1, 100, wcplusState.searchNum);
    wcplusState.searchOffset = 0;
    await searchWCPlus();
  });
  document.querySelector("#wcplus-create-task")?.addEventListener("click", () => {
    createWCPlusTask();
  });
  document.querySelector("#wcplus-create-batch-task")?.addEventListener("click", () => {
    createWCPlusBatchTask();
  });
  document.querySelector("#wcplus-import-account")?.addEventListener("click", () => {
    importWCPlusAccount();
  });
  document.querySelector("#wcplus-export-text")?.addEventListener("click", () => {
    exportWCPlusText();
  });
  document.querySelector("#wcplus-export-csv")?.addEventListener("click", () => {
    exportWCPlusCSV();
  });
  document.querySelector("#wcplus-clean-batch-tasks")?.addEventListener("click", () => {
    cleanWCPlusBatchTasks();
  });
  document.querySelector("#wcplus-export-xlsx")?.addEventListener("click", () => {
    exportWCPlusAllArticlesXLSX();
  });
  document.querySelector("#wcplus-batch-nickname-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    wcplusState.batchNicknames = String(data.get("nicknames") || "");
    wcplusState.batchExactMatch = data.get("exactMatch") === "on";
    wcplusState.batchArticleListType = String(data.get("batchArticleListType") || "all");
    wcplusState.batchArticleListAmount = boundedNumber(data.get("batchArticleListAmount"), 0, 1000, 0);
    wcplusState.batchImportToKBase = data.get("importToKBase") === "on";
    wcplusState.batchWaitForCompletion = data.get("waitForCompletion") === "on";
    wcplusState.batchImportLimit = boundedNumber(data.get("batchImportLimit"), 1, 100, 10);
    await batchImportWCPlusNicknames();
  });
  for (const button of document.querySelectorAll("[data-wcplus-copy-batch]")) {
    button.addEventListener("click", async () => {
      await copyWCPlusBatchText(String(button.getAttribute("data-wcplus-copy-batch") || ""));
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-utility]")) {
    button.addEventListener("click", async () => {
      await runWCPlusUtility(String(button.getAttribute("data-wcplus-utility") || ""));
    });
  }
  document.querySelector("[data-wcplus-copy-diagnostics]")?.addEventListener("click", async () => {
    await copyWCPlusDiagnostics();
  });
  document.querySelector("#wcplus-raw-import-form input[name=\"rawFile\"]")?.addEventListener("change", async (event) => {
    const [file] = Array.from(event.currentTarget.files || []);
    if (file) {
      try {
        await loadWCPlusRawFile(file);
      } catch (error) {
        wcplusState.message = error instanceof Error ? error.message : String(error);
        refreshWCPlusView();
      }
    }
  });
  document.querySelector("#wcplus-raw-import-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    readWCPlusRawFormFromDOM();
    await importRawWCPlusArticle();
  });
  for (const button of document.querySelectorAll("[data-wcplus-account-page]")) {
    button.addEventListener("click", async () => {
      const delta = Number(button.getAttribute("data-wcplus-account-page") || "0");
      await pageWCPlusAccounts(delta);
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-article-page]")) {
    button.addEventListener("click", async () => {
      const delta = Number(button.getAttribute("data-wcplus-article-page") || "0");
      await pageWCPlusArticles(delta);
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-search-page]")) {
    button.addEventListener("click", async () => {
      const delta = Number(button.getAttribute("data-wcplus-search-page") || "0");
      await pageWCPlusSearch(delta);
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-account-index]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-account-index") || "0");
      wcplusState.selectedAccount = wcplusState.accounts[index] || null;
      wcplusState.articleOffset = 0;
      await loadWCPlusArticles();
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-preview]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-preview") || "0");
      const article = wcplusState.articles[index];
      if (article) {
        await previewWCPlusArticle(article);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-select-result-account]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-select-result-account") || "0");
      wcplusState.selectedAccount = wcplusState.searchResults[index] || null;
      wcplusState.articleOffset = 0;
      await loadWCPlusArticles();
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-sync-result-account]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-sync-result-account") || "0");
      const account = wcplusState.searchResults[index];
      if (account) {
        await createWCPlusTaskForAccount(account);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-preview-result]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-preview-result") || "0");
      const article = wcplusState.searchResults[index];
      if (article) {
        await previewWCPlusArticle(article);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-import-result]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-import-result") || "0");
      const article = wcplusState.searchResults[index];
      if (article) {
        await importWCPlusArticle(article);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-import]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-import") || "0");
      const article = wcplusState.articles[index];
      if (article) {
        await importWCPlusArticle(article);
      }
    });
  }
}

function boundedNumber(value, min, max, fallback) {
  const parsed = Number.parseInt(String(value ?? ""), 10);
  if (!Number.isFinite(parsed)) {
    return fallback;
  }
  return Math.min(max, Math.max(min, parsed));
}

function readWCPlusOptionsFromDOM() {
  const accountOptions = document.querySelector("#wcplus-account-options-form");
  if (accountOptions) {
    const data = new FormData(accountOptions);
    wcplusState.accountNum = boundedNumber(data.get("accountNum"), 1, 100, wcplusState.accountNum);
  }
  const workOptions = document.querySelector("#wcplus-work-options-form");
  if (workOptions) {
    const data = new FormData(workOptions);
    wcplusState.taskCrawlerType = String(data.get("taskCrawlerType") || "gzh_article_link");
    wcplusState.taskArticleListType = String(data.get("taskArticleListType") || "all");
    wcplusState.taskArticleListDate = boundedNumber(data.get("articleListDate"), 0, Number.MAX_SAFE_INTEGER, wcplusState.taskArticleListDate);
    wcplusState.taskArticleListAmount = boundedNumber(data.get("articleListAmount"), 0, 1000, wcplusState.taskArticleListAmount);
    wcplusState.taskArticleListOffset = boundedNumber(data.get("articleListOffset"), 0, Number.MAX_SAFE_INTEGER, wcplusState.taskArticleListOffset);
    wcplusState.taskArticleRefresh = data.has("articleRefresh");
    wcplusState.taskArticleImageDownload = data.has("articleImageDownload");
    wcplusState.taskReadingDataType = String(data.get("readingDataType") || "all");
    wcplusState.taskReadingDataStartDate = boundedNumber(data.get("readingDataStartDate"), 0, Number.MAX_SAFE_INTEGER, wcplusState.taskReadingDataStartDate);
    wcplusState.taskReadingDataEndDate = boundedNumber(data.get("readingDataEndDate"), 0, Number.MAX_SAFE_INTEGER, wcplusState.taskReadingDataEndDate);
    wcplusState.taskReadingDataAmount = boundedNumber(data.get("readingDataAmount"), 0, 1000, wcplusState.taskReadingDataAmount);
    wcplusState.taskReadingDataOnlyMain = data.has("readingDataOnlyMain");
    wcplusState.taskReadingDataRefresh = data.has("readingDataRefresh");
    wcplusState.importLimit = boundedNumber(data.get("importLimit"), 1, 100, wcplusState.importLimit);
    wcplusState.exportRecentNum = boundedNumber(data.get("exportRecentNum"), 1, 5000, wcplusState.exportRecentNum);
    wcplusState.articleNum = boundedNumber(data.get("articleNum"), 1, 100, wcplusState.articleNum);
  }
}

function readWCPlusRawFormFromDOM() {
  const rawForm = document.querySelector("#wcplus-raw-import-form");
  if (!rawForm) {
    return;
  }
  const data = new FormData(rawForm);
  wcplusState.rawTitle = String(data.get("rawTitle") || "").trim();
  wcplusState.rawNickname = String(data.get("rawNickname") || "").trim();
  wcplusState.rawURL = String(data.get("rawURL") || "").trim();
  wcplusState.rawBookID = String(data.get("rawBookID") || "").trim();
  wcplusState.rawContent = String(data.get("rawContent") || "");
}

async function pageWCPlusAccounts(delta) {
  readWCPlusOptionsFromDOM();
  wcplusState.accountOffset = Math.max(0, wcplusState.accountOffset + (delta * wcplusState.accountNum));
  await loadWCPlusAccounts();
}

async function pageWCPlusArticles(delta) {
  readWCPlusOptionsFromDOM();
  wcplusState.articleOffset = Math.max(0, wcplusState.articleOffset + (delta * wcplusState.articleNum));
  await loadWCPlusArticles();
}

async function pageWCPlusSearch(delta) {
  wcplusState.searchOffset = Math.max(0, wcplusState.searchOffset + (delta * wcplusState.searchNum));
  await searchWCPlus();
}

async function previewWeChatArticle(rawURL) {
  if (!rawURL) {
    wechatState.message = "请先输入文章链接。";
    renderWeChatSource();
    return;
  }
  wechatState.loading = "预览文章";
  wechatState.message = "";
  renderWeChatSource();
  try {
    const query = new URLSearchParams({ url: rawURL });
    const payload = await apiFetch(`/api/wechat/article?${query.toString()}`);
    wechatState.preview = payload.article || null;
    wechatState.message = "文章预览已更新。";
  } catch (error) {
    wechatState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wechatState.loading = "";
    renderWeChatSource();
  }
}

async function importWeChatArticle(rawURL) {
  if (!rawURL) {
    wechatState.message = "请先输入文章链接。";
    renderWeChatSource();
    return;
  }
  wechatState.loading = "导入知识库";
  wechatState.message = "";
  renderWeChatSource();
  try {
    wechatState.imported = await apiFetch("/api/wechat/import", {
      method: "POST",
      body: JSON.stringify({
        url: rawURL,
        book_id: wechatState.bookID,
      }),
    });
    wechatState.preview = wechatState.preview || {
      title: wechatState.imported?.book?.title || "",
      source_url: rawURL,
      markdown: "",
      text: "",
    };
    wechatState.message = "已导入书籍知识库。";
  } catch (error) {
    wechatState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wechatState.loading = "";
    renderWeChatSource();
  }
}

async function searchWeChatAccounts() {
  if (!wechatState.accountQuery) {
    wechatState.message = "请输入公众号名称。";
    renderWeChatSource();
    return;
  }
  wechatState.loading = "搜索公众号";
  wechatState.message = "";
  renderWeChatSource();
  try {
    const query = new URLSearchParams({ q: wechatState.accountQuery });
    const payload = await apiFetch(`/api/wechat/search?${query.toString()}`);
    wechatState.accounts = Array.isArray(payload.accounts) ? payload.accounts : [];
    wechatState.selectedAccount = wechatState.accounts[0] || null;
    wechatState.articleBegin = 0;
    if (wechatState.selectedAccount) {
      await loadWeChatAccountArticles(false);
    } else {
      wechatState.accountArticles = [];
    }
    wechatState.message = wechatState.accounts.length ? "请选择公众号或直接导入文章。" : "未找到公众号。";
  } catch (error) {
    wechatState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wechatState.loading = "";
    renderWeChatSource();
  }
}

async function loadWeChatAccountArticles(renderBefore = true) {
  const fakeid = wechatState.selectedAccount?.fakeid || "";
  if (!fakeid) {
    return;
  }
  wechatState.loading = "加载最近文章";
  wechatState.message = "";
  if (renderBefore) {
    renderWeChatSource();
  }
  try {
    const query = new URLSearchParams({
      fakeid,
      begin: String(wechatState.articleBegin),
      count: String(wechatState.articleCount),
    });
    const payload = await apiFetch(`/api/wechat/articles?${query.toString()}`);
    wechatState.accountArticles = Array.isArray(payload.articles) ? payload.articles : [];
    wechatState.message = `已加载 ${wechatState.accountArticles.length} 篇文章。`;
  } catch (error) {
    wechatState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wechatState.loading = "";
    renderWeChatSource();
  }
}

async function loadWCPlusAccounts() {
  readWCPlusOptionsFromDOM();
  wcplusState.loading = "加载 WC Plus 公众号";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = new URLSearchParams({
      offset: String(wcplusState.accountOffset),
      num: String(wcplusState.accountNum),
    });
    const payload = await apiFetch(`/api/wcplus/gzh/list?${query.toString()}`);
    wcplusState.accounts = Array.isArray(payload.accounts) ? payload.accounts : [];
    wcplusState.selectedAccount = wcplusState.accounts[0] || null;
    wcplusState.articles = [];
    if (wcplusState.selectedAccount) {
      await loadWCPlusArticles(false);
    }
    wcplusState.message = `已加载 ${wcplusState.accounts.length} 个公众号。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function loadWCPlusArticles(renderBefore = true) {
  readWCPlusOptionsFromDOM();
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    return;
  }
  wcplusState.loading = "加载 WC Plus 文章";
  wcplusState.message = "";
  if (renderBefore) {
    refreshWCPlusView();
  }
  try {
    const query = new URLSearchParams({
      biz,
      nickname: wcplusAccountNickname(account),
      offset: String(wcplusState.articleOffset),
      num: String(wcplusState.articleNum),
    });
    const payload = await apiFetch(`/api/wcplus/gzh/articles?${query.toString()}`);
    wcplusState.articles = Array.isArray(payload.articles) ? payload.articles : [];
    wcplusState.message = `已加载 ${wcplusState.articles.length} 篇 WC Plus 文章。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function previewWCPlusArticle(article) {
  const nickname = wcplusArticleNickname(article) || wcplusAccountNickname(wcplusState.selectedAccount);
  const id = wcplusArticleID(article);
  const articleURL = wcplusArticleURL(article);
  if ((!nickname || !id) && !articleURL) {
    wcplusState.message = "文章缺少 nickname/id 或 URL。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "预览 WC Plus 文章";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = id ? new URLSearchParams({ nickname, id }) : new URLSearchParams({ url: articleURL });
    wcplusState.preview = await apiFetch(`/api/wcplus/article/content?${query.toString()}`);
    wcplusState.message = "WC Plus 文章预览已更新。";
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function importWCPlusArticle(article) {
  const nickname = wcplusArticleNickname(article) || wcplusAccountNickname(wcplusState.selectedAccount);
  const id = wcplusArticleID(article);
  const articleURL = wcplusArticleURL(article);
  if ((!nickname || !id) && !articleURL) {
    wcplusState.message = "文章缺少 nickname/id 或 URL。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "导入 WC Plus 文章";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const payload = await apiFetch("/api/wcplus/import/article", {
      method: "POST",
      body: JSON.stringify(id ? { nickname, id } : { url: articleURL }),
    });
    rememberWCPlusImportedBooks(payload);
    wcplusState.message = `已导入：${payload.book?.title || wcplusArticleTitle(article) || id || articleURL}`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function importRawWCPlusArticle() {
  if (!wcplusState.rawTitle) {
    wcplusState.message = "请先输入原文标题。";
    refreshWCPlusView();
    return;
  }
  if (!wcplusState.rawContent.trim()) {
    wcplusState.message = "请先粘贴正文内容。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "手动导入 WC Plus 文章";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    wcplusState.rawImported = await apiFetch("/api/wcplus/import/raw", {
      method: "POST",
      body: JSON.stringify({
        title: wcplusState.rawTitle,
        nickname: wcplusState.rawNickname,
        url: wcplusState.rawURL,
        book_id: wcplusState.rawBookID,
        content: wcplusState.rawContent,
      }),
    });
    rememberWCPlusImportedBooks(wcplusState.rawImported);
    wcplusState.message = `已手动导入：${wcplusState.rawImported?.book?.title || wcplusState.rawTitle}`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function importWCPlusAccount() {
  readWCPlusOptionsFromDOM();
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "批量导入 WC Plus 文章";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const payload = await apiFetch("/api/wcplus/import/account", {
      method: "POST",
      body: JSON.stringify({
        biz,
        nickname: wcplusAccountNickname(account),
        limit: wcplusState.importLimit,
      }),
    });
    rememberWCPlusImportedBooks(payload);
    wcplusState.message = `批量导入完成：${payload.imported_count || 0} 篇。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function checkWCPlusStatus() {
  wcplusState.loading = "检查 WC Plus 状态";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    wcplusState.serviceStatus = await apiFetch("/api/wcplus/status");
    wcplusState.message = wcplusState.serviceStatus?.ok ? "WC Plus 本地服务已连接。" : "WC Plus 本地服务未连接。";
  } catch (error) {
    wcplusState.serviceStatus = { ok: false };
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function checkWCPlusEnvironment() {
  wcplusState.loading = "检查 WC Plus 环境";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const result = await apiFetch("/api/wcplus/env/check");
    wcplusState.envCheck = result;
    wcplusState.serviceStatus = { ok: Boolean(result.ok) };
    const failed = Array.isArray(result.checks)
      ? result.checks.filter((item) => !item.ok).map((item) => item.name).join(", ")
      : "";
    wcplusState.message = result.ok ? "环境检查通过。" : `环境检查未通过：${failed || "请检查服务状态"}`;
  } catch (error) {
    wcplusState.serviceStatus = { ok: false };
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function searchWCPlus() {
  if (!wcplusState.searchQuery && wcplusState.searchMode !== "all") {
    wcplusState.message = "请输入搜索关键词。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "搜索 WC Plus";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = new URLSearchParams({
      q: wcplusState.searchQuery,
      offset: String(wcplusState.searchOffset),
      num: String(wcplusState.searchNum),
      sort: "p_date",
      direction: "desc",
    });
    const endpointByMode = {
      fulltext: "/api/wcplus/search",
      title: "/api/wcplus/article/search-title",
      account: "/api/wcplus/gzh/search",
      candidate: "/api/wcplus/search-gzh",
      all: "/api/wcplus/article/all",
    };
    const payload = await apiFetch(`${endpointByMode[wcplusState.searchMode] || endpointByMode.fulltext}?${query.toString()}`);
    if (wcplusState.searchMode === "account" || wcplusState.searchMode === "candidate") {
      wcplusState.searchResults = firstArray(payload, ["accounts", "Accounts", "gzhs", "Gzhs", "candidates", "Candidates"]);
    } else {
      wcplusState.searchResults = firstArray(payload, ["results", "Results", "articles", "Articles", "items", "Items"]);
    }
    wcplusState.message = `搜索完成：${wcplusState.searchResults.length} 条结果。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function runWCPlusUtility(kind) {
  const biz = wcplusAccountBiz(wcplusState.selectedAccount);
  const articleID = wcplusArticleID(wcplusState.preview || wcplusState.articles[0] || {});
  const query = new URLSearchParams();
  const utilityByKind = {
    reading: {
      title: "阅读数据",
      endpoint: "/api/wcplus/report/reading-data",
      needsBiz: true,
    },
    statistics: {
      title: "统计数据",
      endpoint: "/api/wcplus/report/statistic-data",
      needsBiz: true,
    },
    owner: {
      title: "公众号详情",
      endpoint: "/api/wcplus/article/gzh",
      needsArticleID: true,
    },
    likes: {
      title: "收藏文章",
      endpoint: "/api/wcplus/like-articles",
      defaults: { offset: "0", num: String(wcplusState.articleNum || 20) },
    },
    request: {
      title: "请求公众号",
      endpoint: "/api/wcplus/request/gzh",
      needsBiz: true,
    },
  };
  const utility = utilityByKind[kind];
  if (!utility) {
    wcplusState.message = "未知辅助查询。";
    refreshWCPlusView();
    return;
  }
  if (utility.needsBiz && !biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  if (utility.needsArticleID && !articleID) {
    wcplusState.message = "请先预览或加载一篇带 id 的文章。";
    refreshWCPlusView();
    return;
  }
  if (utility.needsBiz) {
    query.set("biz", biz);
  }
  if (utility.needsArticleID) {
    query.set("id", articleID);
  }
  for (const [key, value] of Object.entries(utility.defaults || {})) {
    query.set(key, value);
  }

  wcplusState.loading = utility.title;
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const suffix = query.toString() ? `?${query.toString()}` : "";
    const payload = await apiFetch(`${utility.endpoint}${suffix}`);
    wcplusState.utilityResult = {
      title: utility.title,
      payload,
    };
    wcplusState.message = `${utility.title}已更新。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function batchImportWCPlusNicknames() {
  const nicknames = wcplusState.batchNicknames
    .split(/\r?\n/)
    .map((value) => value.trim())
    .filter(Boolean);
  if (!nicknames.length) {
    wcplusState.message = "请先输入公众号昵称。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "批量导入公众号昵称";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const articleListAmount = wcplusState.batchArticleListType === "amount" ? wcplusState.batchArticleListAmount : 0;
    const result = await apiFetch("/api/wcplus/batch-import/gzh", {
      method: "POST",
      body: JSON.stringify({
        nicknames,
        articleListType: wcplusState.batchArticleListType,
        articleListAmount,
        start_queue: true,
        exact_match: wcplusState.batchExactMatch,
        import_to_kbase: wcplusState.batchImportToKBase,
        wait_for_completion: wcplusState.batchWaitForCompletion,
        import_limit: wcplusState.batchImportLimit,
        poll_attempts: wcplusState.batchWaitForCompletion ? 30 : 0,
        poll_interval_millis: wcplusState.batchWaitForCompletion ? 2000 : 0,
      }),
    });
    wcplusState.batchResult = result;
    rememberWCPlusImportedBooks(result);
    const successCount = Array.isArray(result.success) ? result.success.length : 0;
    const failedCount = Array.isArray(result.failed) ? result.failed.length : 0;
    const importedCount = result.imported_count || 0;
    wcplusState.message = `批量任务完成：成功 ${successCount}，失败 ${failedCount}${result.started ? "，队列已启动" : ""}${wcplusState.batchImportToKBase ? `，入库 ${importedCount} 篇` : ""}。`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function copyWCPlusBatchText(kind) {
  const result = wcplusState.batchResult || {};
  const text = kind === "success" ? result.success_text : result.failed_text;
  if (!text) {
    wcplusState.message = kind === "success" ? "暂无成功清单。" : "暂无失败清单。";
    refreshWCPlusView();
    return;
  }
  try {
    await navigator.clipboard.writeText(text);
    wcplusState.message = "已复制到剪贴板。";
  } catch {
    wcplusState.message = "浏览器不允许写入剪贴板，请手动复制文本框内容。";
  }
  refreshWCPlusView();
}

function wcplusDiagnosticText() {
  const check = wcplusState.envCheck || {};
  const lines = [
    `WC Plus environment: ${check.ok ? "OK" : "NEEDS_ACTION"}`,
    `base_url: ${check.base_url || "-"}`,
  ];
  if (Array.isArray(check.checks) && check.checks.length) {
    lines.push("", "checks:");
    for (const item of check.checks) {
      lines.push(`- ${item.name || "check"}: ${item.ok ? "OK" : "FAIL"} ${item.message || ""}`.trim());
    }
  }
  if (Array.isArray(check.advice) && check.advice.length) {
    lines.push("", "advice:");
    for (const item of check.advice) {
      lines.push(`- ${item}`);
    }
  }
  const batch = wcplusState.batchResult;
  if (batch) {
    lines.push(
      "",
      `batch_success: ${Array.isArray(batch.success) ? batch.success.length : 0}`,
      `batch_failed: ${Array.isArray(batch.failed) ? batch.failed.length : 0}`,
    );
    if (batch.failed_text) {
      lines.push("", "failed_text:", batch.failed_text);
    }
  }
  return lines.join("\n");
}

async function copyWCPlusDiagnostics() {
  if (!wcplusState.envCheck) {
    wcplusState.message = "请先执行环境检查。";
    refreshWCPlusView();
    return;
  }
  try {
    await navigator.clipboard.writeText(wcplusDiagnosticText());
    wcplusState.message = "诊断信息已复制。";
  } catch {
    wcplusState.message = "浏览器不允许写入剪贴板，请手动复制环境诊断内容。";
  }
  refreshWCPlusView();
}

async function loadWCPlusRawFile(file) {
  readWCPlusRawFormFromDOM();
  const text = await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ""));
    reader.onerror = () => reject(reader.error || new Error("读取文件失败"));
    reader.readAsText(file);
  });
  wcplusState.rawContent = String(text);
  if (!wcplusState.rawTitle) {
    wcplusState.rawTitle = file.name.replace(/\.(txt|md|markdown)$/i, "");
  }
  wcplusState.message = `已读取文件：${file.name}`;
  refreshWCPlusView();
}

async function loadWCPlusTasks() {
  wcplusState.loading = "加载 WC Plus 下载任务";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const payload = await apiFetch("/api/wcplus/task/all");
    wcplusState.tasks = Array.isArray(payload.tasks) ? payload.tasks : [];
    wcplusState.message = `已加载 ${wcplusState.tasks.length} 个任务。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function createWCPlusTask() {
  await createWCPlusTaskForAccount(wcplusState.selectedAccount);
}

async function createWCPlusTaskForAccount(account) {
  readWCPlusOptionsFromDOM();
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  wcplusState.selectedAccount = account;
  wcplusState.loading = "创建 WC Plus 同步任务";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const task = await apiFetch("/api/wcplus/task/new", {
      method: "POST",
      body: JSON.stringify({
        biz,
        nickname: wcplusAccountNickname(account),
        crawlerType: wcplusState.taskCrawlerType,
        articleListType: wcplusState.taskArticleListType,
        articleListDate: wcplusState.taskArticleListDate,
        articleListAmount: wcplusState.taskArticleListAmount,
        articleListOffset: wcplusState.taskArticleListOffset,
        articleRefresh: wcplusState.taskArticleRefresh,
        articleImgDownload: wcplusState.taskArticleImageDownload,
        readingDataType: wcplusState.taskReadingDataType,
        readingDataStartDate: wcplusState.taskReadingDataStartDate,
        readingDataEndDate: wcplusState.taskReadingDataEndDate,
        readingDataAmount: wcplusState.taskReadingDataAmount,
        readingDataOnlyMain: wcplusState.taskReadingDataOnlyMain,
        readingDataRefresh: wcplusState.taskReadingDataRefresh,
      }),
    });
    await loadWCPlusTasks(false);
    wcplusState.message = `已创建同步任务：${task.task_id || wcplusAccountNickname(account) || biz}`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function createWCPlusBatchTask() {
  readWCPlusOptionsFromDOM();
  wcplusState.loading = "创建 WC Plus 批量任务";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const result = await apiFetch("/api/wcplus/batch-task/create", {
      method: "POST",
      body: JSON.stringify({
        articleListType: wcplusState.taskArticleListType,
        articleListDate: wcplusState.taskArticleListDate,
        articleListAmount: wcplusState.taskArticleListAmount,
        articleListOffset: wcplusState.taskArticleListOffset,
        articleRefresh: wcplusState.taskArticleRefresh,
        articleImgDownload: wcplusState.taskArticleImageDownload,
      }),
    });
    wcplusState.message = `批量任务已创建：${firstValue(result, ["task_id", "TaskID", "status", "Status"]) || "已提交"}`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function runWCPlusQueue() {
  wcplusState.loading = "启动 WC Plus 队列";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const result = await apiFetch("/api/wcplus/task/control", {
      method: "POST",
      body: JSON.stringify({ command: "run" }),
    });
    wcplusState.message = `队列已启动：${firstValue(result, ["status", "Status", "message", "Message"]) || "running"}`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function cleanWCPlusBatchTasks() {
  wcplusState.loading = "清理 WC Plus 批量任务";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const result = await apiFetch("/api/wcplus/batch-task/delete", {
      method: "POST",
      body: JSON.stringify({ status: ["ready", "error"] }),
    });
    wcplusState.message = `批量任务已清理：${firstValue(result, ["deleted", "Deleted", "count", "Count"]) || "完成"}`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function exportWCPlusText() {
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "导出 WC Plus TXT";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = new URLSearchParams({
      biz,
      nickname: wcplusAccountNickname(account),
      only_main: "true",
      need_img: "false",
      open_dir: "false",
    });
    const result = await apiFetch(`/api/wcplus/export/text?${query.toString()}`);
    wcplusState.message = `TXT 导出已触发：${JSON.stringify(result)}`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function exportWCPlusCSV() {
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "导出 WC Plus CSV";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = new URLSearchParams({
      biz,
      nickname: wcplusAccountNickname(account),
      open_dir: "false",
    });
    const result = await apiFetch(`/api/wcplus/export/gzh-csv?${query.toString()}`);
    wcplusState.message = `CSV 导出已触发：${JSON.stringify(result)}`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function exportWCPlusAllArticlesXLSX() {
  readWCPlusOptionsFromDOM();
  wcplusState.loading = "导出 WC Plus 全库 XLSX";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const size = await apiDownload("/api/wcplus/export/all-articles-xlsx", {
      method: "POST",
      body: JSON.stringify({
        sort: "p_date",
        direction: "desc",
        only_headline: false,
        range_mode: "recent",
        recent_num: wcplusState.exportRecentNum,
        fields: [
          "gzh_nickname",
          "title",
          "author",
          "p_date_text",
          "read_num",
          "like_num",
          "comment_num",
          "digest",
          "content_url",
          "source_url",
          "content",
        ],
      }),
    }, "wcplus-all-articles.xlsx");
    wcplusState.message = `XLSX 已下载：${size} bytes。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

function bindBookKnowledgeEvents() {
  document.querySelector("#knowledge-refresh")?.addEventListener("click", () => {
    loadBookKnowledge();
  });
  document.querySelector("#knowledge-cockpit-refresh")?.addEventListener("click", () => {
    loadKnowledgeReviewCockpit();
  });
  document.querySelector("#knowledge-cockpit-toggle")?.addEventListener("click", () => {
    knowledgeState.reviewCockpitOpen = !knowledgeState.reviewCockpitOpen;
    renderBookKnowledge();
  });
  document.querySelector("#knowledge-pipeline-refresh")?.addEventListener("click", () => {
    loadKnowledgePipelineDashboard();
  });
  document.querySelector("#knowledge-pipeline-preview")?.addEventListener("click", async () => {
    await runKnowledgePipelineAutomation({ dryRun: true });
  });
  document.querySelector("#knowledge-pipeline-run")?.addEventListener("click", async () => {
    await runKnowledgePipelineAutomation({ dryRun: false });
  });
  for (const button of document.querySelectorAll("[data-pipeline-book-id]")) {
    button.addEventListener("click", async () => {
      const bookID = button.getAttribute("data-pipeline-book-id") || "";
      const book = knowledgeState.books.find((item) => item.book_id === bookID);
      if (!book) {
        return;
      }
      window.history?.pushState?.({}, "", sourceKnowledgeURL(book.book_id));
      await selectKnowledgeBook(book);
    });
  }
  for (const button of document.querySelectorAll("[data-cockpit-book-id]")) {
    button.addEventListener("click", async () => {
      const bookID = button.getAttribute("data-cockpit-book-id") || "";
      const book = knowledgeState.books.find((item) => item.book_id === bookID);
      if (!book) {
        return;
      }
      window.history?.pushState?.({}, "", `${sourceKnowledgeURL(book.book_id)}?review=1`);
      await selectKnowledgeBook(book);
      setKnowledgeReviewOpen(true);
    });
  }
  document.querySelector("#knowledge-search-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    knowledgeState.query = String(data.get("query") || "").trim();
    await searchBookKnowledge();
  });
  document.querySelector("#knowledge-analysis-model")?.addEventListener("change", (event) => {
    knowledgeState.analysisModel = event.currentTarget.value || "qwen3.7-max";
  });
  for (const button of document.querySelectorAll("[data-knowledge-prompt]")) {
    button.addEventListener("click", () => {
      const key = button.getAttribute("data-knowledge-prompt") || "";
      const prompt = knowledgeAnalysisPrompts.find(([value]) => value === key)?.[2] || "";
      knowledgeState.analysisPrompt = prompt;
      const textarea = document.querySelector("#knowledge-analysis-form textarea[name='question']");
      if (textarea) {
        textarea.value = prompt;
        textarea.focus();
      }
    });
  }
  document.querySelector("#knowledge-analysis-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    knowledgeState.analysisPrompt = String(data.get("question") || "").trim();
    await runKnowledgeAnalysis();
  });
  document.querySelector("#knowledge-analysis-generate")?.addEventListener("click", async () => {
    await generateKnowledgeAnalysisManifest();
  });
  document.querySelector("#knowledge-review-toggle")?.addEventListener("click", () => {
    setKnowledgeReviewOpen(!knowledgeState.reviewOpen);
  });
  document.querySelector("#knowledge-review-retry")?.addEventListener("click", async () => {
    await retryKnowledgeReverification();
  });
  document.querySelector("#knowledge-review-publish")?.addEventListener("click", async () => {
    await publishKnowledgeCandidate();
  });
  for (const button of document.querySelectorAll("[data-book-index]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-book-index") || "0");
      const book = knowledgeState.books[index] || null;
      if (book) {
        window.history?.pushState?.({}, "", sourceKnowledgeURL(book.book_id));
        await selectKnowledgeBook(book);
      }
    });
  }
}

function bindDedaoCourseArticleAnalysis(route) {
  document.querySelector("#course-article-analysis-model")?.addEventListener("change", (event) => {
    dedaoLibraryState.courseArticleAnalysisModel = event.currentTarget.value || "qwen3.7-max";
  });
  for (const button of document.querySelectorAll("[data-course-article-prompt]")) {
    button.addEventListener("click", () => {
      const prompt = button.getAttribute("data-prompt") || "";
      dedaoLibraryState.courseArticleAnalysisPrompt = prompt;
      const textarea = document.querySelector("#course-article-analysis-form textarea[name='question']");
      if (textarea) {
        textarea.value = prompt;
        textarea.focus();
      }
    });
  }
  document.querySelector("#course-article-analysis-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    dedaoLibraryState.courseArticleAnalysisPrompt = String(data.get("question") || "").trim();
    await runDedaoCourseArticleAnalysis(route);
  });
}

async function loadBookKnowledge() {
  knowledgeState.loading = "加载书籍";
  knowledgeState.message = "";
  renderBookKnowledge();
  try {
    await Promise.all([
      loadKnowledgeReviewCockpit({ silent: true, renderResult: false }),
      loadKnowledgePipelineDashboard({ silent: true, renderResult: false }),
    ]);
    const payload = await apiFetch("/api/books");
    knowledgeState.books = Array.isArray(payload.books) ? payload.books : [];
    if (knowledgeState.books.length) {
      const queryBookID = new URLSearchParams(window.location.search).get("book_id") || "";
      const preferredID = getKnowledgeBookID() || queryBookID || knowledgeState.selectedBook?.book_id || "";
      const preferred = preferredID
        ? knowledgeState.books.find((book) => book.book_id === preferredID)
        : null;
      await selectKnowledgeBook(preferred || knowledgeState.books[0], false);
    } else {
      knowledgeState.selectedBook = null;
      knowledgeState.package = null;
      knowledgeState.results = [];
      resetKnowledgeReview();
    }
    knowledgeState.message = `已加载 ${knowledgeState.books.length} 本。`;
  } catch (error) {
    knowledgeState.message = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.loading = "";
    renderBookKnowledge();
  }
}

async function loadKnowledgePipelineDashboard({ silent = false, renderResult = true } = {}) {
  if (!silent) {
    knowledgeState.pipelineLoading = "加载流水线";
    knowledgeState.pipelineError = "";
    if (renderResult) {
      renderBookKnowledge();
    }
  }
  try {
    knowledgeState.pipelineDashboard = await apiFetch("/api/knowledge/pipeline?limit=100");
  } catch (error) {
    knowledgeState.pipelineError = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.pipelineLoading = "";
    if (renderResult) {
      renderBookKnowledge();
    }
  }
}

async function runKnowledgePipelineAutomation({ dryRun = false } = {}) {
  knowledgeState.pipelineAutomationLoading = dryRun ? "预览中" : "推进中";
  knowledgeState.pipelineAutomationError = "";
  renderBookKnowledge();
  try {
    knowledgeState.pipelineAutomation = await apiFetch("/api/knowledge/pipeline/run", {
      method: "POST",
      body: JSON.stringify({
        dry_run: dryRun,
        limit: 5,
        model: knowledgeState.analysisModel || "qwen3.7-max",
        max_context_chars: 16000,
      }),
    });
    await Promise.all([
      loadKnowledgePipelineDashboard({ silent: true, renderResult: false }),
      loadKnowledgeReviewCockpit({ silent: true, renderResult: false }),
    ]);
  } catch (error) {
    knowledgeState.pipelineAutomationError = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.pipelineAutomationLoading = "";
    renderBookKnowledge();
  }
}

async function loadKnowledgeReviewCockpit({ silent = false, renderResult = true } = {}) {
  if (!silent) {
    knowledgeState.reviewCockpitLoading = "加载全局复核";
    knowledgeState.reviewCockpitError = "";
    if (renderResult) {
      renderBookKnowledge();
    }
  }
  try {
    knowledgeState.reviewCockpit = await apiFetch("/api/knowledge/review?limit=50");
  } catch (error) {
    knowledgeState.reviewCockpitError = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.reviewCockpitLoading = "";
    if (renderResult) {
      renderBookKnowledge();
    }
  }
}

async function selectKnowledgeBook(book, renderBefore = true) {
  const previousID = knowledgeState.selectedBook?.book_id || "";
  knowledgeState.selectedBook = book;
  knowledgeState.package = null;
  knowledgeState.results = [];
  if (book?.book_id !== previousID) {
    resetKnowledgeAnalysis();
    resetKnowledgeReview();
  }
  knowledgeState.loading = "加载详情";
  if (renderBefore) {
    renderBookKnowledge();
  }
  try {
    knowledgeState.package = await apiFetch(`/api/books/${encodeURIComponent(book.book_id)}`);
    await Promise.all([
      loadKnowledgeAnalysisManifest(book.book_id),
      loadKnowledgeReview(book.book_id, { silent: true, renderResult: false }),
    ]);
  } catch (error) {
    knowledgeState.message = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.loading = "";
    if (renderBefore) {
      renderBookKnowledge();
    }
  }
}

async function loadKnowledgeAnalysisManifest(bookID) {
  knowledgeState.analysisManifestError = "";
  try {
    knowledgeState.analysisManifest = await apiFetch(`/api/books/${encodeURIComponent(bookID)}/analysis`);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (message.includes("HTTP 404")) {
      knowledgeState.analysisManifest = null;
      return;
    }
    knowledgeState.analysisManifestError = message;
  }
}

async function generateKnowledgeAnalysisManifest() {
  const bookID = knowledgeState.selectedBook?.book_id || knowledgeState.package?.book?.book_id || "";
  if (!bookID) {
    knowledgeState.analysisManifestError = "请先选择文章。";
    renderBookKnowledge();
    return;
  }
  knowledgeState.analysisManifestLoading = "正在生成可追溯分析";
  knowledgeState.analysisManifestError = "";
  renderBookKnowledge();
  try {
    knowledgeState.analysisManifest = await apiFetch(`/api/books/${encodeURIComponent(bookID)}/analysis`, {
      method: "POST",
      body: JSON.stringify({
        model: knowledgeState.analysisModel || "qwen3.7-max",
        max_context_chars: 16000,
      }),
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    await loadKnowledgeAnalysisManifest(bookID);
    if (!knowledgeState.analysisManifest?.error) {
      knowledgeState.analysisManifestError = message;
    }
  } finally {
    knowledgeState.analysisManifestLoading = "";
    renderBookKnowledge();
  }
}

async function runKnowledgeAnalysis() {
  const bookID = knowledgeState.selectedBook?.book_id || knowledgeState.package?.book?.book_id || "";
  const question = String(knowledgeState.analysisPrompt || "").trim();
  if (!bookID) {
    knowledgeState.analysisError = "请先选择文章。";
    renderBookKnowledge();
    return;
  }
  if (!question) {
    knowledgeState.analysisError = "请输入问题或选择模板。";
    renderBookKnowledge();
    return;
  }
  knowledgeState.analysisLoading = "TokenPlan 分析中";
  knowledgeState.analysisError = "";
  renderBookKnowledge();
  try {
    knowledgeState.analysisResponse = await apiFetch("/api/book-chat", {
      method: "POST",
      body: JSON.stringify({
        book_id: bookID,
        mode: "analysis",
        question,
        model: knowledgeState.analysisModel || "qwen3.7-max",
        max_context_chars: 12000,
      }),
    });
  } catch (error) {
    knowledgeState.analysisError = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.analysisLoading = "";
    renderBookKnowledge();
  }
}

async function runDedaoCourseArticleAnalysis(route) {
  const payload = dedaoLibraryState.courseArticle || {};
  const markdown = String(payload.markdown || "").trim();
  const question = String(dedaoLibraryState.courseArticleAnalysisPrompt || "").trim();
  const title = route?.title || payload.detail?.article?.Title || "课程正文";
  if (!markdown) {
    dedaoLibraryState.courseArticleAnalysisError = "当前文章正文还未加载完成。";
    renderDedaoCourseArticle(route);
    return;
  }
  if (!question) {
    dedaoLibraryState.courseArticleAnalysisError = "请输入问题或选择模板。";
    renderDedaoCourseArticle(route);
    return;
  }
  dedaoLibraryState.courseArticleAnalysisLoading = "TokenPlan 分析中";
  dedaoLibraryState.courseArticleAnalysisError = "";
  renderDedaoCourseArticle(route);
  try {
    dedaoLibraryState.courseArticleAnalysisResponse = await apiFetch("/api/context-chat", {
      method: "POST",
      body: JSON.stringify({
        title,
        source_type: "dedao_course_article",
        source_id: route?.articleEnID || "",
        question,
        content: markdown,
        model: dedaoLibraryState.courseArticleAnalysisModel || "qwen3.7-max",
        max_context_chars: 16000,
      }),
    });
  } catch (error) {
    dedaoLibraryState.courseArticleAnalysisError = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoLibraryState.courseArticleAnalysisLoading = "";
    renderDedaoCourseArticle(route);
  }
}

async function searchBookKnowledge() {
  if (!knowledgeState.query) {
    knowledgeState.results = [];
    renderBookKnowledge();
    return;
  }
  knowledgeState.loading = "检索";
  knowledgeState.message = "";
  renderBookKnowledge();
  try {
    const query = new URLSearchParams({
      q: knowledgeState.query,
      book_id: knowledgeState.selectedBook?.book_id || "",
      limit: "20",
    });
    const payload = await apiFetch(`/api/search?${query.toString()}`);
    knowledgeState.results = Array.isArray(payload.results) ? payload.results : [];
    knowledgeState.message = `找到 ${knowledgeState.results.length} 条结果。`;
  } catch (error) {
    knowledgeState.message = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.loading = "";
    renderBookKnowledge();
  }
}

function formatArticleTime(value) {
  if (!value) {
    return "";
  }
  const date = new Date(Number(value) * 1000);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toLocaleDateString("zh-CN");
}

async function boot() {
  const routePathname = getRoutePathname();
  if (window.location.pathname === "/" || routePathname === ROUTES.dedaoHome) {
    renderDedaoHome();
    await loadDedaoHome();
    return;
  }
  if (routePathname === ROUTES.jobs || routePathname.startsWith(`${ROUTES.jobs}/`)) {
    renderJobCenter();
    await loadJobCenter();
    return;
  }
  const dedaoCourseEnID = getDedaoCourseDetailEnID();
  if (dedaoCourseEnID) {
    renderDedaoCourseDetail();
    await loadDedaoCourseDetail(dedaoCourseEnID);
    return;
  }
  const dedaoCourseArticleRoute = getDedaoCourseArticleRoute();
  if (dedaoCourseArticleRoute) {
    renderDedaoCourseArticle(dedaoCourseArticleRoute);
    await loadDedaoCourseArticle(dedaoCourseArticleRoute);
    return;
  }
  const dedaoCourseRoute = getDedaoCourseRoute();
  if (dedaoCourseRoute) {
    renderDedaoCourseArticles(dedaoCourseRoute);
    await loadDedaoCourseArticles(dedaoCourseRoute);
    return;
  }
  if (routePathname === ROUTES.dedaoCourses) {
    renderDedaoCourses();
    await loadDedaoCourses();
    return;
  }
  const dedaoEbookRoute = getDedaoEbookRoute();
  if (dedaoEbookRoute) {
    renderDedaoEbookDetail(dedaoEbookRoute);
    await loadDedaoEbookDetail(dedaoEbookRoute);
    return;
  }
  if (routePathname === ROUTES.dedaoEbooks) {
    renderDedaoEbooks();
    await loadDedaoLibrary("ebook");
    return;
  }
  if (routePathname === ROUTES.dedaoAudio || routePathname.startsWith(`${ROUTES.dedaoAudio}/`)) {
    renderDedaoOdob();
    await loadDedaoLibrary("odob");
    return;
  }
  if (
    routePathname === ROUTES.agentPackages || routePathname.startsWith(`${ROUTES.agentPackages}/`) ||
    routePathname === ROUTES.agents || routePathname.startsWith(`${ROUTES.agents}/`) ||
    routePathname === ROUTES.bookApps || routePathname.startsWith(`${ROUTES.bookApps}/`)
  ) {
    const bookAgentRoute = getBookAgentRoute();
    renderBookAgentPlatform(bookAgentRoute);
    await loadBookAgentPlatform(bookAgentRoute);
    return;
  }
  if (window.location.pathname.startsWith("/wechat-import") || window.location.pathname.startsWith("/sources/wechat")) {
    renderWeChatSource();
    return;
  }
  if (isSourceControlPath()) {
    sourceControlPrefillFromLocation();
    renderWCPlusPage();
    await bootstrapSourceControlPlane();
    return;
  }
  if (routePathname === ROUTES.knowledgePackages || routePathname.startsWith(`${ROUTES.knowledgePackages}/`)) {
    renderBookKnowledge();
    await loadBookKnowledge();
    return;
  }

  const bookID = getBookID();
  if (!bookID) {
    renderDedaoHome();
    return;
  }
  try {
    const payload = await fetchBook(bookID);
    renderReader(payload);
  } catch (error) {
    renderError(error instanceof Error ? error.message : String(error));
  }
}

boot();
