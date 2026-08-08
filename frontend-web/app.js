const app = document.querySelector("#app");
let readerAssetObjectURLs = [];

const tokenKeys = [
  "kbase.token",
  "kbaseToken",
  "KBASE_AUTH_TOKEN",
];

const browserClientIDKey = "kbase.browser-client-id";
const browserClientIDHeader = "X-KBase-Browser-Client-ID";
const browserEpochHeader = "X-KBase-Browser-Epoch";

const browserSessionState = {
  ready: false,
  session: null,
  csrfToken: "",
  csrfExpiresAt: "",
  clientID: "",
  epoch: null,
  loginPromise: null,
  statusPromise: null,
  familyPromise: null,
  recoveryPromise: null,
  migrationAttempted: false,
  generation: 0,
  invalidationGeneration: 0,
  invalidationReason: "",
  logoutGeneration: 0,
  logoutPending: false,
  operationControllers: new Set(),
};

const sessionSettingsState = {
  status: "loading",
  session: null,
  message: "正在读取当前会话…",
  sequence: 0,
};

const browserSessionSignalKey = "kbase.browser-session.signal";
let browserSessionChannel = null;
const browserSessionSeenSignalNonces = new Set();
const browserSessionSeenSignalNonceLimit = 128;
const browserSessionSignalListeners = new Set();
const guardedBrowserResponses = new WeakMap();

const ROUTES = Object.freeze({
  dedaoHome: "/sources/dedao/home",
  dedaoLogin: "/sources/dedao/login",
  dedaoCourses: "/sources/dedao/courses",
  dedaoEbooks: "/sources/dedao/ebooks",
  dedaoAudio: "/sources/dedao/audio",
  sourceAgents: "/sources/agents",
  bookReader: "/read/books",
  knowledgePackages: "/knowledge/packages",
  agentPackages: "/agent-packages",
  agents: "/agents",
  bookApps: "/book-apps",
  healthReleases: "/delivery/health/releases",
  operations: "/operations",
  jobs: "/jobs",
  sessionSettings: "/settings/session",
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

const dedaoLoginState = {
  session: null,
  qrCode: null,
  phase: "idle",
  message: "",
  pollingTimer: null,
  expiresAt: 0,
};

const dedaoEbookAcquisitionState = {
  source: "shelf",
  query: "",
  page: 1,
  pageSize: 15,
  siteItems: [],
  siteTotal: 0,
  siteIsMore: 0,
  hasSearched: false,
  loading: "",
  message: "",
  submitting: new Set(),
  jobs: {},
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
  audioDetail: null,
  audioDetailLoading: "",
  audioDetailMessage: "",
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

const sourceAgentManagementState = {
  agents: [],
  artifacts: [],
  commandsByAgent: {},
  loading: "",
  message: "",
  pendingAgentID: "",
};

const sourceAgentDetailState = {
  agentID: "",
  agent: null,
  subscriptions: [],
  runs: [],
  runDetails: {},
  commands: [],
  loading: "",
  message: "",
  notFound: false,
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
  agentPackages: [],
  agentPackagesLoading: "",
  agentPackagesError: "",
  directoryCollapsed: false,
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

const agentCompilerState = {
  mode: "dual",
  releases: [],
  primaryReleaseID: "",
  supportingReleaseIDs: [],
  version: "1.0.0",
  result: null,
  loading: "",
  error: "",
};

const evidenceAuditState = {
  audits: [],
  audit: null,
  routeAuditID: "",
  subject: "",
  scope: "",
  selectedClaims: [],
  loading: "",
  error: "",
  proofroomPreview: null,
  proofroomStatus: "",
  proofroomError: "",
  deliveryReceipt: null,
  proofroomDeliveryKey: "",
  createIdempotencyKey: "",
  createRequestFingerprint: "",
  retryIdempotencyKey: "",
};

const knowledgeOperationsState = {
  console: null,
  loading: "",
  message: "",
  replaying: "",
  replayResult: null,
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
let sourceAgentManagementPollTimer = null;
let sourceAgentManagementSequence = 0;
let sourceAgentDetailSequence = 0;
let knowledgeReviewPollTimer = null;
let knowledgeReviewLoadSequence = 0;
let knowledgeAgentLoadSequence = 0;
let evidenceAuditPollTimer = null;
let evidenceAuditLoadSequence = 0;
let evidenceAuditWorkspaceSequence = 0;
let bookAgentLoadSequence = 0;
let agentCompilerRequestSequence = 0;
let proofroomOperationSequence = 0;
let bookKnowledgeLoadSequence = 0;
let bookKnowledgeDetailSequence = 0;
let proofroomPreviousFocus = null;
let proofroomKeydownHandler = null;
let proofroomReturnFocusSelector = "";

function isSafeBearerToken(token) {
  const clean = String(token || "").trim();
  if (!clean || /\s/.test(clean)) {
    return false;
  }
  return /^[\x21-\x7e]+$/.test(clean);
}

function browserTokenStores() {
  const stores = [];
  for (const property of ["localStorage", "sessionStorage"]) {
    try {
      const storage = window[property];
      if (storage) {
        stores.push(storage);
      }
    } catch {
      // Storage property access itself may be blocked by browser privacy policy.
    }
  }
  return stores;
}

function clearLegacyBrowserTokens() {
  for (const storage of browserTokenStores()) {
    for (const key of tokenKeys) {
      try {
        storage.removeItem(key);
      } catch {
        // Storage may be unavailable in privacy-restricted browser contexts.
      }
    }
  }
}

function findLegacyBrowserToken() {
  let legacyToken = "";
  for (const storage of browserTokenStores()) {
    for (const key of tokenKeys) {
      let value = "";
      try {
        value = String(storage.getItem(key) || "").trim();
      } catch {
        value = "";
      }
      if (!legacyToken && isSafeBearerToken(value)) {
        legacyToken = value;
      }
    }
  }
  return legacyToken;
}

function isValidBrowserClientID(value) {
  return (
    typeof value === "string" &&
    value.length >= 16 &&
    value.length <= 128 &&
    /^[A-Za-z0-9_-]+$/.test(value)
  );
}

function generateBrowserClientID() {
  if (!globalThis.crypto || typeof globalThis.crypto.getRandomValues !== "function") {
    const error = new Error("secure browser identity is unavailable");
    error.status = 503;
    throw error;
  }
  const bytes = new Uint8Array(24);
  globalThis.crypto.getRandomValues(bytes);
  return `client_${Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("")}`;
}

function persistBrowserClientID(clientID) {
  if (!isValidBrowserClientID(clientID)) {
    const error = new Error("invalid browser client id");
    error.status = 503;
    throw error;
  }
  try {
    window.localStorage?.setItem(browserClientIDKey, clientID);
  } catch {
    // The in-memory identity still works when persistent storage is unavailable.
  }
  browserSessionState.clientID = clientID;
  return clientID;
}

function getBrowserClientID() {
  if (isValidBrowserClientID(browserSessionState.clientID)) {
    return browserSessionState.clientID;
  }
  let stored = "";
  try {
    stored = String(window.localStorage?.getItem(browserClientIDKey) || "");
  } catch {
    stored = "";
  }
  return persistBrowserClientID(
    isValidBrowserClientID(stored) ? stored : generateBrowserClientID(),
  );
}

function parseBrowserEpoch(value) {
  const epoch = typeof value === "number" ? value : Number(value);
  if (!Number.isSafeInteger(epoch) || epoch <= 0 || String(epoch) !== String(value)) {
    const error = new Error("invalid browser session epoch");
    error.status = 503;
    throw error;
  }
  return epoch;
}

function calibrateBrowserClientMetadata(payload) {
  const clientID = String(payload?.client_id || "");
  if (!isValidBrowserClientID(clientID)) {
    const error = new Error("invalid browser client metadata");
    error.status = 503;
    throw error;
  }
  const epoch = parseBrowserEpoch(payload?.epoch);
  persistBrowserClientID(clientID);
  browserSessionState.epoch = epoch;
  return { clientID, epoch };
}

function abortBrowserSessionOperations() {
  for (const controller of browserSessionState.operationControllers) {
    try {
      controller.abort();
    } catch {
      // Generation checks still fence runtimes without abortable fetch.
    }
  }
  browserSessionState.operationControllers.clear();
}

function resetBrowserSessionMemory(reason = "reset", logoutPending = false) {
  abortBrowserSessionOperations();
  browserSessionState.ready = false;
  browserSessionState.session = null;
  browserSessionState.csrfToken = "";
  browserSessionState.csrfExpiresAt = "";
  browserSessionState.epoch = null;
  browserSessionState.loginPromise = null;
  browserSessionState.statusPromise = null;
  browserSessionState.familyPromise = null;
  browserSessionState.recoveryPromise = null;
  browserSessionState.generation += 1;
  browserSessionState.invalidationGeneration += 1;
  browserSessionState.invalidationReason = reason;
  browserSessionState.logoutPending = logoutPending;
  if (reason === "logout-start" || reason === "logout") {
    browserSessionState.logoutGeneration = browserSessionState.invalidationGeneration;
  }
}

function browserSessionStaleError() {
  const error = new Error("browser session state changed");
  error.code = "browser_session_stale";
  return error;
}

function assertBrowserSessionGeneration(expectedGeneration) {
  if (browserSessionState.generation === expectedGeneration) {
    return;
  }
  throw browserSessionStaleError();
}

function browserSessionOperationController(externalSignal = null) {
  if (typeof AbortController !== "function") {
    return { signal: undefined, abort() {}, release() {} };
  }
  const controller = new AbortController();
  let released = false;
  const abortFromExternal = () => controller.abort();
  if (externalSignal?.aborted) {
    controller.abort();
  } else {
    externalSignal?.addEventListener?.("abort", abortFromExternal, { once: true });
  }
  const operation = {
    signal: controller.signal,
    abort() {
      controller.abort();
      operation.release();
    },
    release() {
      if (released) {
        return;
      }
      released = true;
      externalSignal?.removeEventListener?.("abort", abortFromExternal);
      browserSessionState.operationControllers.delete(operation);
    },
  };
  browserSessionState.operationControllers.add(operation);
  return operation;
}

function browserSessionSignal(type) {
  const message = {
    type,
    at: Date.now(),
    nonce: Math.random().toString(36).slice(2),
  };
  try {
    browserSessionChannel?.postMessage(message);
  } catch {
    // Other tabs will still receive the storage hint when available.
  }
  try {
    window.localStorage?.setItem(browserSessionSignalKey, JSON.stringify(message));
    window.localStorage?.removeItem(browserSessionSignalKey);
  } catch {
    // Cross-tab synchronization is best effort and carries no credentials.
  }
}

function applyBrowserSessionSignal(message) {
  if (
    !message ||
    !["login", "logout-start", "logout"].includes(message.type)
  ) {
    return;
  }
  const nonce = typeof message.nonce === "string" ? message.nonce : "";
  if (nonce && nonce.length <= 256) {
    if (browserSessionSeenSignalNonces.has(nonce)) {
      return;
    }
    browserSessionSeenSignalNonces.add(nonce);
    if (browserSessionSeenSignalNonces.size > browserSessionSeenSignalNonceLimit) {
      const oldestNonce = browserSessionSeenSignalNonces.values().next().value;
      browserSessionSeenSignalNonces.delete(oldestNonce);
    }
  }
  resetBrowserSessionMemory(message.type, message.type === "logout-start");
  for (const listener of browserSessionSignalListeners) {
    listener(message.type);
  }
}

if (typeof window.BroadcastChannel === "function") {
  try {
    browserSessionChannel = new window.BroadcastChannel("kbase-browser-session");
    browserSessionChannel.onmessage = (event) => applyBrowserSessionSignal(event?.data);
  } catch {
    browserSessionChannel = null;
  }
}

window.addEventListener?.("storage", (event) => {
  if (event?.key !== browserSessionSignalKey || !event.newValue) {
    return;
  }
  try {
    applyBrowserSessionSignal(JSON.parse(event.newValue));
  } catch {
    // Ignore malformed cross-tab hints.
  }
});

async function readBrowserResponse(response) {
  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = text;
    }
  }
  return { text, payload };
}

function browserResponseError(response, result) {
  const message = typeof result.payload === "object" && result.payload
    ? (result.payload.error || result.payload.message || JSON.stringify(result.payload))
    : (result.payload || result.text || `HTTP ${response.status}`);
  const error = new Error(message);
  error.status = response.status;
  error.payload = result.payload;
  return error;
}

function handleBrowserEpochConflict(response, result) {
  const error = browserResponseError(response, result);
  resetBrowserSessionMemory("logout", false);
  browserSessionSignal("logout");
  throw error;
}

async function loadBrowserClientEpoch(expectedGeneration) {
  if (browserSessionState.familyPromise) {
    return browserSessionState.familyPromise;
  }
  const clientID = getBrowserClientID();
  const operation = browserSessionOperationController();
  const familyPromise = (async () => {
    try {
      const headers = new Headers({ Accept: "application/json" });
      headers.set(browserClientIDHeader, clientID);
      const response = await fetch("/browser/session", {
        method: "GET",
        headers,
        credentials: "same-origin",
        cache: "no-store",
        signal: operation.signal,
      });
      const result = await readBrowserResponse(response);
      assertBrowserSessionGeneration(expectedGeneration);
      if (!response.ok) {
        throw browserResponseError(response, result);
      }
      return calibrateBrowserClientMetadata(result.payload);
    } finally {
      operation.release();
    }
  })();
  browserSessionState.familyPromise = familyPromise;
  try {
    return await familyPromise;
  } finally {
    if (browserSessionState.familyPromise === familyPromise) {
      browserSessionState.familyPromise = null;
    }
  }
}

function browserClientRequestHeaders(snapshot) {
  const headers = new Headers({ Accept: "application/json" });
  headers.set(browserClientIDHeader, snapshot.clientID);
  headers.set(browserEpochHeader, String(snapshot.epoch));
  return headers;
}

async function migrateLegacyBrowserSession(snapshot, expectedGeneration) {
  if (browserSessionState.migrationAttempted) {
    return "absent";
  }
  browserSessionState.migrationAttempted = true;
  const legacyToken = findLegacyBrowserToken();
  if (!legacyToken) {
    return "absent";
  }
  const headers = browserClientRequestHeaders(snapshot);
  headers.set("Authorization", `Bearer ${legacyToken}`);
  const operation = browserSessionOperationController();
  try {
    const response = await fetch("/browser/session/migrate", {
      method: "POST",
      headers,
      credentials: "same-origin",
      cache: "no-store",
      signal: operation.signal,
    });
    const result = await readBrowserResponse(response);
    assertBrowserSessionGeneration(expectedGeneration);
    if (response.ok) {
      return "migrated";
    }
    if (response.status === 401) {
      clearLegacyBrowserTokens();
      return "unauthorized";
    }
    if (response.status === 409) {
      handleBrowserEpochConflict(response, result);
    }
    throw browserResponseError(response, result);
  } finally {
    operation.release();
  }
}

async function requestBrowserSessionLogin(snapshot, expectedGeneration) {
  const operation = browserSessionOperationController();
  try {
    const response = await fetch("/browser/session", {
      method: "POST",
      headers: browserClientRequestHeaders(snapshot),
      credentials: "same-origin",
      cache: "no-store",
      signal: operation.signal,
    });
    const result = await readBrowserResponse(response);
    assertBrowserSessionGeneration(expectedGeneration);
    if (response.status === 409) {
      handleBrowserEpochConflict(response, result);
    }
    if (!response.ok) {
      throw browserResponseError(response, result);
    }
    return result.payload;
  } finally {
    operation.release();
  }
}

async function loadBrowserSession(expectedGeneration = browserSessionState.generation) {
  if (browserSessionState.statusPromise) {
    return browserSessionState.statusPromise;
  }
  const operation = browserSessionOperationController();
  const statusPromise = (async () => {
    try {
      const response = await fetch("/api/browser/session", {
        headers: { Accept: "application/json" },
        credentials: "same-origin",
        cache: "no-store",
        signal: operation.signal,
      });
      const result = await readBrowserResponse(response);
      assertBrowserSessionGeneration(expectedGeneration);
      if (!response.ok) {
        throw browserResponseError(response, result);
      }
      const session = result.payload?.session || null;
      const csrfToken = String(result.payload?.csrf_token || "");
      if (!session || !csrfToken) {
        const error = new Error("invalid browser session response");
        error.status = 503;
        throw error;
      }
      calibrateBrowserClientMetadata(result.payload);
      browserSessionState.ready = true;
      browserSessionState.session = session;
      browserSessionState.csrfToken = csrfToken;
      browserSessionState.csrfExpiresAt = String(result.payload?.csrf_expires_at || "");
      return session;
    } finally {
      operation.release();
    }
  })();
  browserSessionState.statusPromise = statusPromise;
  try {
    return await statusPromise;
  } finally {
    if (browserSessionState.statusPromise === statusPromise) {
      browserSessionState.statusPromise = null;
    }
  }
}

async function establishBrowserSession(expectedGeneration, skipStatus = false) {
  if (!skipStatus) {
    try {
      return await loadBrowserSession(expectedGeneration);
    } catch (error) {
      if (error?.status !== 401) {
        throw error;
      }
    }
  }
  assertBrowserSessionGeneration(expectedGeneration);
  const snapshot = await loadBrowserClientEpoch(expectedGeneration);
  assertBrowserSessionGeneration(expectedGeneration);
  const migration = await migrateLegacyBrowserSession(snapshot, expectedGeneration);
  assertBrowserSessionGeneration(expectedGeneration);
  if (migration !== "migrated") {
    await requestBrowserSessionLogin(snapshot, expectedGeneration);
    assertBrowserSessionGeneration(expectedGeneration);
  }
  const session = await loadBrowserSession(expectedGeneration);
  if (
    migration === "migrated" &&
    browserSessionState.clientID === snapshot.clientID &&
    browserSessionState.epoch === snapshot.epoch
  ) {
    clearLegacyBrowserTokens();
  }
  browserSessionSignal("login");
  return session;
}

async function ensureBrowserSession() {
  if (browserSessionState.logoutPending) {
    const error = new Error("browser logout is in progress");
    error.code = "browser_session_logout_pending";
    throw error;
  }
  if (browserSessionState.ready && browserSessionState.session && browserSessionState.csrfToken) {
    return browserSessionState.session;
  }
  if (browserSessionState.recoveryPromise) {
    return browserSessionState.recoveryPromise;
  }
  if (browserSessionState.loginPromise) {
    return browserSessionState.loginPromise;
  }
  const requestGeneration = browserSessionState.generation;
  const loginPromise = establishBrowserSession(requestGeneration, false);
  browserSessionState.loginPromise = loginPromise;
  try {
    return await loginPromise;
  } finally {
    if (browserSessionState.loginPromise === loginPromise) {
      browserSessionState.loginPromise = null;
    }
  }
}

async function recoverBrowserSession() {
  if (browserSessionState.recoveryPromise) {
    return browserSessionState.recoveryPromise;
  }
  resetBrowserSessionMemory("recovery", false);
  const recoveryGeneration = browserSessionState.generation;
  const recoveryPromise = establishBrowserSession(recoveryGeneration, true);
  browserSessionState.recoveryPromise = recoveryPromise;
  try {
    return await recoveryPromise;
  } finally {
    if (browserSessionState.recoveryPromise === recoveryPromise) {
      browserSessionState.recoveryPromise = null;
    }
  }
}

function isUnsafeBrowserMethod(method) {
  return !["GET", "HEAD", "OPTIONS"].includes(String(method || "GET").toUpperCase());
}

function browserSessionCSRFNeedsRefresh() {
  if (!browserSessionState.csrfToken) {
    return true;
  }
  const expiresAt = Date.parse(browserSessionState.csrfExpiresAt);
  return !Number.isFinite(expiresAt) || expiresAt <= Date.now() + 30_000;
}

function requireSameOriginBrowserRequest(path) {
  if (
    typeof path !== "string" ||
    !path ||
    path.startsWith("//")
  ) {
    const error = new Error("cross-origin browser request rejected");
    error.code = "cross_origin_request";
    throw error;
  }
  let requestURL = null;
  try {
    requestURL = new URL(path, window.location.origin);
  } catch {
    requestURL = null;
  }
  if (
    !requestURL ||
    requestURL.origin !== window.location.origin ||
    requestURL.username ||
    requestURL.password
  ) {
    const error = new Error("cross-origin browser request rejected");
    error.code = "cross_origin_request";
    throw error;
  }
  return requestURL.href;
}

function isBrowserResponseLifecycleCurrent(expectedGeneration, expectedLogoutGeneration) {
  return (
    browserSessionState.generation === expectedGeneration &&
    browserSessionState.logoutGeneration === expectedLogoutGeneration &&
    !browserSessionState.logoutPending
  );
}

function assertBrowserResponseLifecycle(expectedGeneration, expectedLogoutGeneration) {
  if (isBrowserResponseLifecycleCurrent(expectedGeneration, expectedLogoutGeneration)) {
    return;
  }
  throw browserSessionStaleError();
}

function releaseBrowserSessionResponse(response, abort = false) {
  const lifecycle = guardedBrowserResponses.get(response);
  if (!lifecycle) {
    return;
  }
  guardedBrowserResponses.delete(response);
  if (abort) {
    lifecycle.operation.abort();
    return;
  }
  lifecycle.operation.release();
}

function assertBrowserSessionResponseCurrent(response) {
  const lifecycle = guardedBrowserResponses.get(response);
  if (!lifecycle) {
    return;
  }
  assertBrowserResponseLifecycle(
    lifecycle.expectedGeneration,
    lifecycle.expectedLogoutGeneration,
  );
}

function guardBrowserSessionResponse(
  response,
  operation,
  expectedGeneration,
  expectedLogoutGeneration,
) {
  const guardedBodyMethods = new Set([
    "arrayBuffer",
    "blob",
    "bytes",
    "formData",
    "json",
    "text",
  ]);
  let guardedResponse = null;
  guardedResponse = new Proxy(response, {
    get(target, property) {
      const value = Reflect.get(target, property, target);
      if (!guardedBodyMethods.has(property) || typeof value !== "function") {
        return typeof value === "function" ? value.bind(target) : value;
      }
      return async (...args) => {
        try {
          assertBrowserSessionResponseCurrent(guardedResponse);
          const result = await value.apply(target, args);
          assertBrowserSessionResponseCurrent(guardedResponse);
          return result;
        } catch (error) {
          if (!isBrowserResponseLifecycleCurrent(expectedGeneration, expectedLogoutGeneration)) {
            throw browserSessionStaleError();
          }
          throw error;
        }
      };
    },
  });
  guardedBrowserResponses.set(guardedResponse, {
    operation,
    expectedGeneration,
    expectedLogoutGeneration,
  });
  return guardedResponse;
}

function browserSessionFetch(path, options = {}, didRecover = false) {
  const target = requireSameOriginBrowserRequest(path);
  return performBrowserSessionFetch(target, options, didRecover);
}

async function performBrowserSessionFetch(target, options, didRecover) {
  await ensureBrowserSession();
  const headers = new Headers(options.headers || {});
  headers.delete("Authorization");
  const method = String(options.method || "GET").toUpperCase();
  if (isUnsafeBrowserMethod(method)) {
    if (browserSessionCSRFNeedsRefresh()) {
      try {
        await loadBrowserSession();
      } catch (error) {
        if (error?.status !== 401 || didRecover) {
          throw error;
        }
        await recoverBrowserSession();
        return browserSessionFetch(target, options, true);
      }
    }
    headers.set("X-KBase-CSRF", browserSessionState.csrfToken);
  }
  const requestGeneration = browserSessionState.generation;
  const requestLogoutGeneration = browserSessionState.logoutGeneration;
  const operation = browserSessionOperationController(options.signal);
  let response = null;
  try {
    response = await fetch(target, {
      ...options,
      method,
      headers,
      credentials: "same-origin",
      signal: operation.signal,
    });
  } catch (error) {
    operation.release();
    if (!isBrowserResponseLifecycleCurrent(requestGeneration, requestLogoutGeneration)) {
      throw browserSessionStaleError();
    }
    throw error;
  }
  if (response.status === 401 && !didRecover) {
    operation.abort();
    if (
      browserSessionState.logoutPending ||
      browserSessionState.logoutGeneration > requestLogoutGeneration
    ) {
      return response;
    }
    const activeRecovery = browserSessionState.recoveryPromise;
    if (activeRecovery) {
      await activeRecovery;
    } else if (browserSessionState.generation === requestGeneration) {
      await recoverBrowserSession();
    } else {
      await ensureBrowserSession();
    }
    return browserSessionFetch(target, options, true);
  }
  try {
    assertBrowserResponseLifecycle(requestGeneration, requestLogoutGeneration);
  } catch (error) {
    operation.abort();
    throw error;
  }
  return guardBrowserSessionResponse(
    response,
    operation,
    requestGeneration,
    requestLogoutGeneration,
  );
}

async function logoutBrowserSession() {
  await ensureBrowserSession();
  if (browserSessionCSRFNeedsRefresh()) {
    await loadBrowserSession();
  }
  const csrfToken = browserSessionState.csrfToken;
  resetBrowserSessionMemory("logout-start", true);
  const logoutBarrierGeneration = browserSessionState.generation;
  browserSessionSignal("logout-start");
  const headers = new Headers({
    Accept: "application/json",
    "X-KBase-CSRF": csrfToken,
  });
  try {
    const response = await fetch("/api/browser/session/logout", {
      method: "POST",
      headers,
      credentials: "same-origin",
      cache: "no-store",
    });
    const result = await readBrowserResponse(response);
    assertBrowserSessionGeneration(logoutBarrierGeneration);
    if (!response.ok) {
      throw browserResponseError(response, result);
    }
  } catch (error) {
    if (browserSessionState.generation !== logoutBarrierGeneration) {
      throw browserSessionStaleError();
    }
    browserSessionState.logoutPending = false;
    browserSessionState.invalidationReason = "logout-failed";
    browserSessionSignal("login");
    throw error;
  }
  assertBrowserSessionGeneration(logoutBarrierGeneration);
  browserSessionState.logoutPending = false;
  browserSessionState.invalidationReason = "logout";
  browserSessionSignal("logout");
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

async function apiFetch(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Accept", "application/json");
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await browserSessionFetch(path, {
    ...options,
    headers,
  });
  try {
    const result = await readBrowserResponse(response);
    if (!response.ok) {
      throw browserResponseError(response, result);
    }
    assertBrowserSessionResponseCurrent(response);
    return result.payload;
  } finally {
    releaseBrowserSessionResponse(response);
  }
}

async function apiDownload(path, options = {}, filename = "download.bin") {
  const headers = new Headers(options.headers || {});
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await browserSessionFetch(path, {
    ...options,
    headers,
  });
  let objectURL = "";
  let link = null;
  try {
    if (!response.ok) {
      const text = await response.text();
      let payload = text;
      try {
        payload = text ? JSON.parse(text) : null;
      } catch {
        // Keep the raw error body.
      }
      throw browserResponseError(response, { text, payload });
    }
    const blob = await response.blob();
    assertBrowserSessionResponseCurrent(response);
    objectURL = URL.createObjectURL(blob);
    link = document.createElement("a");
    link.href = objectURL;
    link.download = filename;
    assertBrowserSessionResponseCurrent(response);
    document.body.append(link);
    assertBrowserSessionResponseCurrent(response);
    link.click();
    return blob.size;
  } finally {
    link?.remove();
    if (objectURL) {
      setTimeout(() => URL.revokeObjectURL(objectURL), 0);
    }
    releaseBrowserSessionResponse(response);
  }
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

function getDedaoAudioRoute() {
  const raw = getPathSegmentAfter(`${ROUTES.dedaoAudio}/`);
  if (!raw) {
    return null;
  }
  const params = new URLSearchParams(window.location.search);
  try {
    return { enid: decodeURIComponent(raw), aliasID: params.get("alias_id") || "" };
  } catch {
    return { enid: raw, aliasID: params.get("alias_id") || "" };
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

function isKnowledgePackageDetailRoute() {
  return Boolean(getKnowledgeBookID());
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

function buildDedaoAudioURL(enid, aliasID = "") {
  if (!enid) {
    return "";
  }
  const query = aliasID ? `?alias_id=${encodeURIComponent(aliasID)}` : "";
  return `${ROUTES.dedaoAudio}/${encodeURIComponent(enid)}${query}`;
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

function buildEvidenceAuditURL(packageID, auditID, version = "") {
  if (!packageID || !auditID) {
    return buildAgentURL(packageID, version);
  }
  const params = new URLSearchParams();
  if (version) {
    params.set("version", version);
  }
  const query = params.toString();
  return `${ROUTES.agents}/${encodeURIComponent(packageID)}/audits/${encodeURIComponent(auditID)}${query ? `?${query}` : ""}`;
}

function getEvidenceAuditRoute() {
  const pathname = getRoutePathname();
  const prefix = `${ROUTES.agents}/`;
  if (!pathname.startsWith(prefix)) {
    return null;
  }
  const parts = pathname.slice(prefix.length).split("/").filter(Boolean);
  if (parts.length !== 3 || parts[1] !== "audits") {
    return null;
  }
  const params = new URLSearchParams(window.location.search);
  try {
    return {
      view: "agent",
      packageID: decodeURIComponent(parts[0]),
      auditID: decodeURIComponent(parts[2]),
      version: params.get("version") || "",
    };
  } catch {
    return {
      view: "agent",
      packageID: parts[0],
      auditID: parts[2],
      version: params.get("version") || "",
    };
  }
}

function getBookAgentRoute() {
  const auditRoute = getEvidenceAuditRoute();
  if (auditRoute) {
    return auditRoute;
  }
  const pathname = getRoutePathname();
  const routes = [
    [ROUTES.agentPackages, "package"],
    [ROUTES.agents, "agent"],
    [ROUTES.bookApps, "app"],
  ];
  for (const [base, view] of routes) {
    if (pathname === base) {
      return { view, packageID: "", auditID: "", version: "" };
    }
    if (pathname.startsWith(`${base}/`)) {
      const raw = pathname.slice(base.length + 1).split("/")[0];
      const params = new URLSearchParams(window.location.search);
      try {
        return { view, packageID: decodeURIComponent(raw), auditID: "", version: params.get("version") || "" };
      } catch {
        return { view, packageID: raw, auditID: "", version: params.get("version") || "" };
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
        <a class="${current === "source-agents" ? "active" : ""}" href="/sources/agents">Agent 管理</a>
        <a class="${current === "import" ? "active" : ""}" href="/wechat-import">单篇导入</a>
        <a class="${current === "knowledge" ? "active" : ""}" href="${escapeAttribute(ROUTES.knowledgePackages)}">书籍知识库</a>
        <a class="${current === "agents" ? "active" : ""}" href="${escapeAttribute(ROUTES.agentPackages)}">Book Agents</a>
        <a class="${current === "operations" ? "active" : ""}" href="${escapeAttribute(ROUTES.operations)}">Operations</a>
        <a class="${current === "jobs" ? "active" : ""}" href="${escapeAttribute(ROUTES.jobs)}">任务</a>
        <a class="web-nav__session ${current === "session" ? "active" : ""}" ${current === "session" ? 'aria-current="page"' : ""} href="${escapeAttribute(ROUTES.sessionSettings)}">会话</a>
        <a class="web-nav__account ${current === "login" ? "active" : ""}" href="${escapeAttribute(ROUTES.dedaoLogin)}">${escapeHTML(dedaoLoginState.session?.active_user?.name || "登录得到")}</a>
      </nav>
    </header>
    ${content}
  `;
}

function formatSessionSettingsTime(value) {
  if (!value) {
    return "—";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "—";
  }
  return parsed.toLocaleString("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
    hour12: false,
  });
}

function syncSessionSettingsAnnouncement() {
  const announcer = document.querySelector("#session-settings-announcer");
  if (!announcer) {
    return;
  }
  const { status, session, message } = sessionSettingsState;
  const stateMessages = {
    loading: message || "正在读取当前会话",
    revoked: "当前会话已退出",
    unauthorized: "尚未登录",
    forbidden: "无权访问当前会话",
    unavailable: "会话服务暂不可用",
  };
  const isLoading = status === "loading";
  if (isLoading) {
    announcer.setAttribute("aria-busy", "true");
  }
  announcer.textContent = status === "active" && session
    ? `当前会话已登录。当前设备：${session.device_label || "当前浏览器"}。`
    : (stateMessages[status] || stateMessages.unavailable);
  if (!isLoading) {
    announcer.setAttribute("aria-busy", "false");
  }
}

function isSessionSettingsRoute() {
  return getRoutePathname() === ROUTES.sessionSettings;
}

function sessionSettingsErrorStatus(error) {
  if (error?.status === 401) {
    return "unauthorized";
  }
  if (error?.status === 403) {
    return "forbidden";
  }
  return "unavailable";
}

function handleSessionSettingsSignal(type) {
  if (!isSessionSettingsRoute()) {
    return;
  }
  if (type === "logout-start" || type === "logout") {
    sessionSettingsState.sequence += 1;
    sessionSettingsState.status = "revoked";
    sessionSettingsState.session = null;
    sessionSettingsState.message = "";
    renderSessionSettings();
    return;
  }
  if (type === "login") {
    loadSessionSettings();
  }
}

browserSessionSignalListeners.add(handleSessionSettingsSignal);

function renderSessionSettings() {
  const { status, session, message } = sessionSettingsState;
  let panelBody = "";
  if (status === "active" && session) {
    panelBody = `
      <div class="session-settings__panel-head">
        <div>
          <p class="web-kicker">Current session</p>
          <h2>当前会话</h2>
        </div>
        <span class="session-settings__signal is-active">已登录</span>
      </div>
      <dl class="session-settings__details">
        <div>
          <dt>当前设备</dt>
          <dd>${escapeHTML(session.device_label || "当前浏览器")}</dd>
        </div>
        <div>
          <dt>最近活跃</dt>
          <dd><time datetime="${escapeAttribute(session.last_active_at || "")}">${escapeHTML(formatSessionSettingsTime(session.last_active_at))}</time></dd>
        </div>
        <div>
          <dt>到期时间</dt>
          <dd><time datetime="${escapeAttribute(session.expires_at || "")}">${escapeHTML(formatSessionSettingsTime(session.expires_at))}</time></dd>
        </div>
      </dl>
      <div class="session-settings__actions">
        <p>退出后，此浏览器需要重新登录才能访问私有内容。</p>
        <button class="button button-primary" type="button" data-session-logout>退出登录</button>
      </div>
    `;
  } else {
    const states = {
      loading: ["正在读取当前会话", message || "正在读取当前会话…", ""],
      revoked: ["当前会话已退出", "此浏览器的会话已经失效。", "login"],
      unauthorized: ["尚未登录", "登录后可查看并管理当前浏览器会话。", "login"],
      forbidden: ["无权访问当前会话", "当前凭据不能读取此会话。", "retry"],
      unavailable: ["会话服务暂不可用", "服务暂时无法响应，请稍后重试。", "retry"],
    };
    const [title, description, action] = states[status] || states.unavailable;
    panelBody = `
      <div class="session-settings__state is-${escapeAttribute(status)}">
        <span class="session-settings__state-mark" aria-hidden="true"></span>
        <div>
          <p class="web-kicker">Current session</p>
          <h2>${escapeHTML(title)}</h2>
          <p>${escapeHTML(description)}</p>
        </div>
        ${action === "login" ? '<button class="button button-primary" type="button" data-session-login>登录</button>' : ""}
        ${action === "retry" ? '<button class="button button-ghost" type="button" data-session-retry>重新检查</button>' : ""}
      </div>
    `;
  }

  renderShell(`
    <main class="session-settings">
      <section class="session-settings__band" aria-labelledby="session-settings-title">
        <div>
          <p class="web-kicker">Browser access</p>
          <h1 id="session-settings-title">会话</h1>
          <p>查看当前浏览器的登录状态和有效期，或从这台设备安全退出。</p>
        </div>
      </section>
      <section class="session-settings__panel" aria-busy="${status === "loading" ? "true" : "false"}">
        ${panelBody}
      </section>
    </main>
  `, "session");
  syncSessionSettingsAnnouncement();
  bindSessionSettingsEvents();
}

async function loadSessionSettings() {
  const sequence = sessionSettingsState.sequence + 1;
  sessionSettingsState.sequence = sequence;
  sessionSettingsState.status = "loading";
  sessionSettingsState.session = null;
  sessionSettingsState.message = "正在读取当前会话…";
  renderSessionSettings();
  try {
    const session = await loadBrowserSession();
    if (
      sequence !== sessionSettingsState.sequence ||
      !isSessionSettingsRoute()
    ) {
      return;
    }
    if (session?.revoked_at) {
      sessionSettingsState.status = "revoked";
      sessionSettingsState.session = null;
    } else {
      sessionSettingsState.status = "active";
      sessionSettingsState.session = session;
    }
  } catch (error) {
    if (
      sequence !== sessionSettingsState.sequence ||
      !isSessionSettingsRoute()
    ) {
      return;
    }
    sessionSettingsState.session = null;
    sessionSettingsState.status = sessionSettingsErrorStatus(error);
  }
  if (
    sequence === sessionSettingsState.sequence &&
    isSessionSettingsRoute()
  ) {
    renderSessionSettings();
  }
}

async function logoutCurrentSession() {
  const sequence = sessionSettingsState.sequence + 1;
  sessionSettingsState.sequence = sequence;
  sessionSettingsState.status = "loading";
  sessionSettingsState.message = "正在退出当前会话…";
  renderSessionSettings();
  try {
    await logoutBrowserSession();
    if (
      sequence !== sessionSettingsState.sequence ||
      !isSessionSettingsRoute()
    ) {
      return;
    }
    sessionSettingsState.status = "revoked";
    sessionSettingsState.session = null;
  } catch (error) {
    if (
      sequence !== sessionSettingsState.sequence ||
      !isSessionSettingsRoute()
    ) {
      return;
    }
    sessionSettingsState.session = null;
    sessionSettingsState.status = sessionSettingsErrorStatus(error);
  }
  if (
    sequence === sessionSettingsState.sequence &&
    isSessionSettingsRoute()
  ) {
    renderSessionSettings();
  }
}

async function loginCurrentSession() {
  const sequence = sessionSettingsState.sequence + 1;
  sessionSettingsState.sequence = sequence;
  sessionSettingsState.status = "loading";
  sessionSettingsState.session = null;
  sessionSettingsState.message = "正在建立当前会话…";
  renderSessionSettings();
  try {
    await ensureBrowserSession();
    if (
      sequence !== sessionSettingsState.sequence ||
      !isSessionSettingsRoute()
    ) {
      return;
    }
    await loadSessionSettings();
  } catch (error) {
    if (
      sequence !== sessionSettingsState.sequence ||
      !isSessionSettingsRoute()
    ) {
      return;
    }
    sessionSettingsState.session = null;
    sessionSettingsState.status = sessionSettingsErrorStatus(error);
    renderSessionSettings();
  }
}

function bindSessionSettingsEvents() {
  app.querySelector("[data-session-logout]")?.addEventListener("click", () => {
    return logoutCurrentSession();
  });
  app.querySelector("[data-session-retry]")?.addEventListener("click", () => {
    return loadSessionSettings();
  });
  app.querySelector("[data-session-login]")?.addEventListener("click", () => {
    return loginCurrentSession();
  });
}

function renderDedaoHome() {
  const activeDedaoUser = dedaoLoginState.session?.active_user;
  const accountAction = dedaoLoginState.session?.logged_in
    ? `<a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoEbooks)}">${escapeHTML(activeDedaoUser?.name || "得到用户")} · 浏览电子书</a>`
    : `<a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoLogin)}">扫码登录得到</a>`;
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
            ${accountAction}
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

function dedaoLoginStatusCopy() {
  if (dedaoLoginState.phase === "success") {
    return "登录成功，书架和订阅内容已刷新。";
  }
  const phaseCopy = ({
    loading: "正在确认当前登录状态…",
    creating: "正在生成一次性二维码…",
    scanning: "请使用得到 App 扫码，扫码完成后保持本页打开。",
    expired: "二维码已过期，请重新生成。",
    error: dedaoLoginState.message || "登录检查失败，请重新生成二维码。",
  })[dedaoLoginState.phase];
  if (phaseCopy) {
    return phaseCopy;
  }
  if (dedaoLoginState.session?.logged_in) {
    return `已登录：${dedaoLoginState.session.active_user?.name || "得到用户"}`;
  }
  return "登录凭证只保存在服务端，本页不会保存得到 Cookie。";
}

function renderDedaoLogin() {
  const session = dedaoLoginState.session || {};
  const user = session.active_user || {};
  const qr = dedaoLoginState.qrCode;
  const busy = dedaoLoginState.phase === "loading" || dedaoLoginState.phase === "creating";
  const scanning = dedaoLoginState.phase === "scanning";
  const authenticating = busy || scanning || Boolean(qr);
  renderShell(`
    <main class="dedao-login">
      <section class="dedao-login__intro">
        <p class="web-kicker">Dedao account</p>
        <h1>把书架重新接回你的知识工作台</h1>
        <p>扫码后即可搜索得到全站电子书、选择书籍并在服务器本地下载，或直接沉淀为可检索的知识包。</p>
        <ol>
          <li><span>01</span><div><strong>扫码</strong><small>得到 App 确认登录</small></div></li>
          <li><span>02</span><div><strong>选书</strong><small>书架与全站搜索并列使用</small></div></li>
          <li><span>03</span><div><strong>入库</strong><small>下载后直接生成知识包</small></div></li>
        </ol>
      </section>
      <section class="dedao-login__panel" aria-labelledby="dedao-login-title">
        <header>
          <div>
            <p class="web-kicker">Secure session</p>
            <h2 id="dedao-login-title">扫码登录得到</h2>
          </div>
          <span class="dedao-login__seal" aria-hidden="true">得</span>
        </header>
        ${session.logged_in && !authenticating ? `
          <div class="dedao-login__account">
            ${user.avatar ? `<img src="${escapeAttribute(user.avatar)}" alt="">` : '<span aria-hidden="true">✓</span>'}
            <div><strong>${escapeHTML(user.name || "得到用户")}</strong><small>当前服务端已保存此账号</small></div>
          </div>
        ` : `
          <div class="dedao-login__qr-frame ${scanning ? "is-scanning" : ""}">
            ${qr?.qr_code ? `<img src="${escapeAttribute(qr.qr_code)}" alt="得到登录二维码">` : '<div class="dedao-login__qr-placeholder" aria-hidden="true"><span></span><span></span><span></span></div>'}
          </div>
        `}
        <p class="dedao-login__status is-${escapeAttribute(dedaoLoginState.phase)}" role="status" aria-live="polite">${escapeHTML(dedaoLoginStatusCopy())}</p>
        ${dedaoLoginState.message && dedaoLoginState.phase !== "error" ? `<p class="web-status">${escapeHTML(dedaoLoginState.message)}</p>` : ""}
        <div class="dedao-login__actions">
          ${session.logged_in && !authenticating
            ? `<a class="button button-primary" href="${escapeAttribute(ROUTES.dedaoEbooks)}">进入电子书</a><button class="button button-ghost" type="button" data-action="create-dedao-qrcode">重新扫码登录</button>`
            : `<button class="button button-primary" type="button" data-action="create-dedao-qrcode" ${busy ? "disabled" : ""}>${qr ? "重新生成二维码" : "生成登录二维码"}</button>`}
          <a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoHome)}">返回首页</a>
        </div>
        <small class="dedao-login__privacy">二维码字段仅存在当前页面内存中；Cookie、Token 和本地路径不会返回浏览器。</small>
      </section>
    </main>
  `, "login");
  app.querySelector("[data-action='create-dedao-qrcode']")?.addEventListener("click", createDedaoLoginQRCode);
}

function stopDedaoLoginPolling() {
  if (dedaoLoginState.pollingTimer !== null) {
    window.clearTimeout(dedaoLoginState.pollingTimer);
    dedaoLoginState.pollingTimer = null;
  }
}

async function loadDedaoSession() {
  dedaoLoginState.phase = "loading";
  dedaoLoginState.message = "";
  renderDedaoLogin();
  try {
    dedaoLoginState.session = await apiFetch("/api/dedao/session");
    dedaoLoginState.phase = "idle";
  } catch (error) {
    dedaoLoginState.phase = "error";
    dedaoLoginState.message = error instanceof Error ? error.message : String(error);
  }
  renderDedaoLogin();
}

async function refreshDedaoContentAfterLogin() {
  const requests = [
    apiFetch("/api/dedao/home?page_size=4"),
    ...["bauhinia", "ebook", "odob"].map((category) => apiFetch(`/api/dedao/library?${new URLSearchParams({ category, order: "study", page: "1", page_size: "15" }).toString()}`)),
  ];
  const [home, ...libraries] = await Promise.allSettled(requests);
  if (home.status === "fulfilled") {
    dedaoLibraryState.home = home.value;
  }
  ["bauhinia", "ebook", "odob"].forEach((category, index) => {
    const result = libraries[index];
    if (result?.status !== "fulfilled") return;
    const state = dedaoLibraryState.pages[category];
    state.items = Array.isArray(result.value?.list) ? result.value.list : [];
    state.isMore = Number(result.value?.is_more || 0);
  });
}

async function createDedaoLoginQRCode() {
  stopDedaoLoginPolling();
  dedaoLoginState.phase = "creating";
  dedaoLoginState.message = "";
  dedaoLoginState.qrCode = null;
  renderDedaoLogin();
  try {
    dedaoLoginState.qrCode = await apiFetch("/api/dedao/auth/qrcode", { method: "POST", body: "{}" });
    dedaoLoginState.expiresAt = Date.now() + 5 * 60 * 1000;
    dedaoLoginState.phase = "scanning";
    renderDedaoLogin();
    startDedaoLoginPolling();
  } catch (error) {
    dedaoLoginState.phase = "error";
    dedaoLoginState.message = error instanceof Error ? error.message : String(error);
    renderDedaoLogin();
  }
}

function startDedaoLoginPolling() {
  stopDedaoLoginPolling();
  const poll = async () => {
    if (getRoutePathname() !== ROUTES.dedaoLogin || !dedaoLoginState.qrCode) {
      stopDedaoLoginPolling();
      return;
    }
    if (Date.now() >= dedaoLoginState.expiresAt) {
      dedaoLoginState.phase = "expired";
      stopDedaoLoginPolling();
      renderDedaoLogin();
      return;
    }
    try {
      const result = await apiFetch("/api/dedao/auth/check", {
        method: "POST",
        body: JSON.stringify({
          token: dedaoLoginState.qrCode.token,
          qr_code_string: dedaoLoginState.qrCode.qr_code_string,
        }),
      });
      if (result?.status === 1 || result?.session?.logged_in) {
        dedaoLoginState.session = result.session || { logged_in: true, active_user: result.user || null };
        dedaoLoginState.phase = "success";
        dedaoLoginState.qrCode = null;
        stopDedaoLoginPolling();
        await refreshDedaoContentAfterLogin();
        renderDedaoLogin();
        return;
      }
      if (result?.expired || result?.status === 2) {
        dedaoLoginState.phase = "expired";
        stopDedaoLoginPolling();
        renderDedaoLogin();
        return;
      }
      dedaoLoginState.pollingTimer = window.setTimeout(poll, 2000);
    } catch (error) {
      dedaoLoginState.phase = "error";
      dedaoLoginState.message = error instanceof Error ? error.message : String(error);
      stopDedaoLoginPolling();
      renderDedaoLogin();
    }
  };
  dedaoLoginState.pollingTimer = window.setTimeout(poll, 2000);
}

if (typeof window.addEventListener === "function") {
  window.addEventListener("beforeunload", stopDedaoLoginPolling);
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
  renderDedaoEbookAcquisition();
}

function renderDedaoOdob() {
  renderDedaoLibrary("odob");
}

function normalizeDedaoEbook(item, source = "shelf") {
  const value = item || {};
  const id = Number(value.id || value.class_id || 0);
  return {
    id: Number.isInteger(id) && id > 0 ? id : 0,
    enid: dedaoProductEnID(value),
    title: String(value.title || value.name || "未命名电子书").trim(),
    author: String(value.author || value.book_author || "").trim(),
    intro: String(value.intro || value.book_intro || "").trim(),
    icon: String(value.icon || value.cover || "").trim(),
    price: String(value.price || value.current_price || "").trim(),
    progress: Number(value.progress || value.read_progress || 0),
    isBuy: Boolean(value.is_buy),
    isOnBookshelf: Boolean(value.is_on_bookshelf) || source === "shelf",
    canTrial: Boolean(value.can_trial_read),
    source,
  };
}

function dedaoEbookJobKey(type, book) {
  return `${type}:${book.enid}`;
}

function dedaoEbookEligibility(book) {
  if (!book.id || !book.enid) return "书籍标识不完整，暂不能下载。";
  if (!book.isBuy && !book.isOnBookshelf) return "请先加入书架，再创建下载任务。";
  return "";
}

function renderDedaoEbookJobStatus(book) {
  const job = dedaoEbookAcquisitionState.jobs[book.enid];
  if (!job) return "";
  const knowledgeBookID = job.result?.knowledge_book_id || "";
  return `
    <div class="dedao-ebook-card__job ${escapeAttribute(jobStatusClass(job.status))}" role="status">
      <span>${escapeHTML(jobStatusLabel(job.status))}</span>
      ${job.error ? `<small>${escapeHTML(job.error)}</small>` : ""}
      ${knowledgeBookID ? `<a href="${escapeAttribute(buildKnowledgePackageURL(knowledgeBookID))}">打开知识包</a>` : ""}
    </div>
  `;
}

function renderDedaoEbookCard(book) {
  const eligibility = dedaoEbookEligibility(book);
  const isSite = book.source === "site";
  const currentJob = dedaoEbookAcquisitionState.jobs[book.enid];
  const jobActive = ["queued", "running"].includes(String(currentJob?.status || "").toLowerCase());
  const adding = dedaoEbookAcquisitionState.submitting.has(dedaoEbookJobKey("bookshelf", book));
  const downloading = dedaoEbookAcquisitionState.submitting.has(dedaoEbookJobKey("dedao_ebook_download", book));
  const syncing = dedaoEbookAcquisitionState.submitting.has(dedaoEbookJobKey("dedao_ebook_sync_kbase", book));
  const owned = book.isBuy || book.isOnBookshelf;
  return `
    <article class="dedao-course-card dedao-ebook-card">
      <div class="dedao-course-card__top">
        ${book.icon ? `<img src="${escapeAttribute(book.icon)}" alt="">` : '<div class="dedao-ebook-card__cover-placeholder">BOOK</div>'}
        <div>
          <p class="web-kicker">${isSite ? "全站电子书" : "我的书架"}</p>
          <h2>${escapeHTML(book.title)}</h2>
          <p>${escapeHTML(book.author || book.intro || "得到电子书")}</p>
        </div>
      </div>
      <div class="dedao-ebook-card__meta">
        <span>${owned ? "可处理" : (book.canTrial ? "可试读" : "未在书架")}</span>
        ${book.price ? `<span>¥ ${escapeHTML(book.price)}</span>` : ""}
        ${book.progress > 0 ? `<span>已读 ${escapeHTML(book.progress)}%</span>` : ""}
      </div>
      <div class="dedao-ebook-card__actions">
        ${book.enid ? `<a class="button button-ghost" href="${escapeAttribute(buildDedaoEbookURL(book.enid))}">详情</a>` : ""}
        ${isSite && !owned ? `<button class="button button-ghost" type="button" data-action="add-dedao-ebook-bookshelf" data-enid="${escapeAttribute(book.enid)}" ${!book.enid || adding ? "disabled" : ""}>${adding ? "加入中" : "加入书架"}</button>` : ""}
        <label class="dedao-ebook-card__format"><span>下载格式</span><select data-dedao-download-type data-enid="${escapeAttribute(book.enid)}" ${eligibility || downloading || syncing || jobActive ? "disabled" : ""}><option value="1">HTML</option><option value="2">PDF</option><option value="3">EPUB</option></select></label>
        <button class="button button-ghost" type="button" data-action="create-dedao-ebook-job" data-job-type="dedao_ebook_download" data-enid="${escapeAttribute(book.enid)}" data-id="${escapeAttribute(book.id)}" ${eligibility || downloading || syncing || jobActive ? "disabled" : ""}>${downloading ? "提交中" : (jobActive ? "任务进行中" : "仅下载")}</button>
        <button class="button button-primary" type="button" data-action="create-dedao-ebook-job" data-job-type="dedao_ebook_sync_kbase" data-enid="${escapeAttribute(book.enid)}" data-id="${escapeAttribute(book.id)}" ${eligibility || downloading || syncing || jobActive ? "disabled" : ""}>${syncing ? "提交中" : (jobActive ? "任务进行中" : "下载并入知识库")}</button>
      </div>
      ${eligibility ? `<small class="dedao-ebook-card__disabled-reason">${escapeHTML(eligibility)}</small>` : ""}
      ${renderDedaoEbookJobStatus(book)}
    </article>
  `;
}

function renderDedaoEbookAcquisition() {
  const state = dedaoEbookAcquisitionState;
  const shelfState = dedaoLibraryState.pages.ebook;
  const items = state.source === "site"
    ? state.siteItems.map((item) => normalizeDedaoEbook(item, "site"))
    : shelfState.items.map((item) => normalizeDedaoEbook(item, "shelf"));
  const message = state.source === "site" ? state.message : shelfState.message;
  const loading = state.source === "site" ? state.loading : shelfState.loading;
  const empty = state.source === "site"
    ? (state.hasSearched ? "没有找到匹配的电子书，换一个关键词试试。" : "输入书名或作者，从得到全站查找电子书。")
    : dedaoLibraryConfig.ebook.empty;
  const cards = items.map(renderDedaoEbookCard).join("");
  renderShell(`
    <main class="dedao-courses dedao-ebook-acquisition">
      <section class="dedao-courses__header dedao-ebook-acquisition__header">
        <div>
          <p class="web-kicker">得到电子书</p>
          <h1>从找到一本书，到拥有一份可检索知识</h1>
          <p>书架内容可以直接处理；全站结果需先确认已购买或加入书架。下载文件保存在服务器本地，浏览器只显示任务状态。</p>
        </div>
        <div class="dedao-courses__actions">
          <a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoLogin)}">登录状态</a>
          <a class="button button-ghost" href="${escapeAttribute(ROUTES.jobs)}">任务中心</a>
        </div>
      </section>
      <section class="dedao-ebook-acquisition__controls">
        <div class="dedao-ebook-acquisition__tabs" role="tablist" aria-label="电子书来源">
          <button type="button" role="tab" aria-selected="${state.source === "shelf"}" class="${state.source === "shelf" ? "is-active" : ""}" data-ebook-source="shelf">我的书架</button>
          <button type="button" role="tab" aria-selected="${state.source === "site"}" class="${state.source === "site" ? "is-active" : ""}" data-ebook-source="site">全站搜索</button>
        </div>
        ${state.source === "site" ? `
          <form class="dedao-ebook-acquisition__search" data-dedao-ebook-search>
            <label><span>书名或作者</span><input name="query" value="${escapeAttribute(state.query)}" placeholder="例如：行为金融学" autocomplete="off"></label>
            <button class="button button-primary" type="submit" ${loading ? "disabled" : ""}>${loading ? "搜索中" : "搜索"}</button>
          </form>
        ` : `
          <button class="button button-ghost" type="button" data-action="reload-dedao-ebooks" ${loading ? "disabled" : ""}>${loading ? "刷新中" : "刷新书架"}</button>
        `}
      </section>
      ${message ? `<p class="web-status">${escapeHTML(message)}</p>` : ""}
      <section class="dedao-courses__grid dedao-ebook-acquisition__grid">
        ${cards || `<div class="dedao-courses__empty"><h2>${escapeHTML(empty)}</h2><p>登录失效时可先回到登录页重新扫码；已入库内容不受影响。</p></div>`}
      </section>
      ${state.source === "site" && state.hasSearched ? `
        <nav class="dedao-ebook-acquisition__pager" aria-label="搜索分页">
          <button class="button button-ghost" type="button" data-search-page="${state.page - 1}" ${state.page <= 1 || loading ? "disabled" : ""}>上一页</button>
          <span>第 ${state.page} 页${state.siteTotal ? ` · 共 ${state.siteTotal} 本` : ""}</span>
          <button class="button button-ghost" type="button" data-search-page="${state.page + 1}" ${!state.siteIsMore || loading ? "disabled" : ""}>下一页</button>
        </nav>
      ` : ""}
    </main>
  `, "ebook");

  app.querySelectorAll("[data-ebook-source]").forEach((button) => button.addEventListener("click", () => setDedaoEbookSource(button.dataset.ebookSource)));
  app.querySelector("[data-dedao-ebook-search]")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    state.query = String(data.get("query") || "").trim();
    state.page = 1;
    searchDedaoEbooks();
  });
  app.querySelector("[data-action='reload-dedao-ebooks']")?.addEventListener("click", () => loadDedaoLibrary("ebook"));
  app.querySelectorAll("[data-search-page]").forEach((button) => button.addEventListener("click", () => {
    state.page = Number(button.dataset.searchPage || 1);
    searchDedaoEbooks();
  }));
  app.querySelectorAll("[data-action='add-dedao-ebook-bookshelf']").forEach((button) => button.addEventListener("click", () => addDedaoEbookToBookshelf(button.dataset.enid)));
  app.querySelectorAll("[data-action='create-dedao-ebook-job']").forEach((button) => button.addEventListener("click", () => {
    const type = button.dataset.jobType || "";
    const format = button.closest(".dedao-ebook-card")?.querySelector("[data-dedao-download-type]");
    createDedaoEbookJob({
      id: Number(button.dataset.id || 0),
      enid: button.dataset.enid || "",
      type,
      downloadType: type === "dedao_ebook_download" ? Number(format?.value || 1) : 1,
    });
  }));
}

function setDedaoEbookSource(source) {
  dedaoEbookAcquisitionState.source = source === "site" ? "site" : "shelf";
  dedaoEbookAcquisitionState.message = "";
  renderDedaoEbookAcquisition();
  if (dedaoEbookAcquisitionState.source === "shelf" && dedaoLibraryState.pages.ebook.items.length === 0) {
    loadDedaoLibrary("ebook");
  }
}

async function searchDedaoEbooks() {
  const state = dedaoEbookAcquisitionState;
  if (!state.query) {
    state.message = "请输入书名或作者。";
    renderDedaoEbookAcquisition();
    return;
  }
  state.loading = "searching";
  state.message = "";
  renderDedaoEbookAcquisition();
  try {
    const query = new URLSearchParams({ q: state.query, page: String(state.page), page_size: String(state.pageSize) });
    const payload = await apiFetch(`/api/dedao/search/ebooks?${query.toString()}`);
    state.siteItems = Array.isArray(payload?.ebooks) ? payload.ebooks : [];
    state.siteTotal = Number(payload?.total || 0);
    state.siteIsMore = Number(payload?.is_more || 0);
    state.hasSearched = true;
    state.message = state.siteItems.length ? `找到 ${state.siteTotal || state.siteItems.length} 本相关电子书。` : "没有找到匹配结果。";
  } catch (error) {
    state.message = `搜索失败，已保留当前结果：${error instanceof Error ? error.message : String(error)}`;
  } finally {
    state.loading = "";
    renderDedaoEbookAcquisition();
  }
}

async function addDedaoEbookToBookshelf(enid) {
  const book = normalizeDedaoEbook(dedaoEbookAcquisitionState.siteItems.find((item) => dedaoProductEnID(item) === enid), "site");
  const key = dedaoEbookJobKey("bookshelf", book);
  if (!enid || dedaoEbookAcquisitionState.submitting.has(key)) return;
  dedaoEbookAcquisitionState.submitting.add(key);
  dedaoEbookAcquisitionState.message = "正在加入书架…";
  renderDedaoEbookAcquisition();
  try {
    const updated = await apiFetch(`/api/dedao/ebooks/${encodeURIComponent(enid)}/bookshelf`, { method: "POST", body: "{}" });
    dedaoEbookAcquisitionState.siteItems = dedaoEbookAcquisitionState.siteItems.map((item) => dedaoProductEnID(item) === enid
      ? { ...item, ...updated, is_on_bookshelf: true }
      : item);
    dedaoEbookAcquisitionState.message = "已加入书架，现在可以下载或入库。";
  } catch (error) {
    dedaoEbookAcquisitionState.message = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoEbookAcquisitionState.submitting.delete(key);
    renderDedaoEbookAcquisition();
  }
}

async function createDedaoEbookJob({ id, enid, type, downloadType = 1 }) {
  const key = `${type}:${enid}`;
  const normalizedDownloadType = type === "dedao_ebook_download" ? Number(downloadType) : 1;
  if (!id || !enid || !["dedao_ebook_download", "dedao_ebook_sync_kbase"].includes(type) || ![1, 2, 3].includes(normalizedDownloadType) || dedaoEbookAcquisitionState.submitting.has(key)) return;
  dedaoEbookAcquisitionState.submitting.add(key);
  dedaoEbookAcquisitionState.message = type === "dedao_ebook_sync_kbase" ? "正在创建下载并入库任务…" : "正在创建下载任务…";
  renderDedaoEbookAcquisition();
  try {
    const payload = await apiFetch("/api/jobs", {
      method: "POST",
      body: JSON.stringify({ type, ebook_id: id, ebook_enid: enid, download_type: normalizedDownloadType }),
    });
    if (!payload?.job?.id) throw new Error("任务响应缺少 job id");
    dedaoEbookAcquisitionState.jobs[enid] = payload.job;
    dedaoEbookAcquisitionState.message = "任务已提交，可留在本页查看进度。";
    pollBookKnowledgeJob(payload.job.id, enid);
  } catch (error) {
    dedaoEbookAcquisitionState.message = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoEbookAcquisitionState.submitting.delete(key);
    renderDedaoEbookAcquisition();
  }
}

async function pollBookKnowledgeJob(jobID, enid, attempt = 0) {
  if (!jobID || !enid || !getRoutePathname().startsWith(ROUTES.dedaoEbooks)) return;
  try {
    const payload = await apiFetch(`/api/jobs/${encodeURIComponent(jobID)}`);
    const job = payload?.job || null;
    if (!job) throw new Error("任务不存在");
    dedaoEbookAcquisitionState.jobs[enid] = job;
    renderDedaoEbookAcquisition();
    if (["succeeded", "failed"].includes(job.status)) return;
    if (attempt >= 240) {
      dedaoEbookAcquisitionState.message = "任务仍在后台运行，请到任务中心继续查看。";
      renderDedaoEbookAcquisition();
      return;
    }
    window.setTimeout(() => pollBookKnowledgeJob(jobID, enid, attempt + 1), 1500);
  } catch (error) {
    dedaoEbookAcquisitionState.message = error instanceof Error ? error.message : String(error);
    renderDedaoEbookAcquisition();
  }
}

async function loadDedaoEbookJobs() {
  const restoredActiveJobs = [];
  try {
    const payload = await apiFetch("/api/jobs?limit=50");
    for (const job of Array.isArray(payload?.jobs) ? payload.jobs : []) {
      if (!["dedao_ebook_download", "dedao_ebook_sync_kbase"].includes(job?.type) || !job?.ebook_enid) continue;
      if (!dedaoEbookAcquisitionState.jobs[job.ebook_enid]) {
        dedaoEbookAcquisitionState.jobs[job.ebook_enid] = job;
        if (job.id && ["queued", "running"].includes(String(job.status || "").toLowerCase())) {
          restoredActiveJobs.push({ id: job.id, enid: job.ebook_enid });
        }
      }
    }
    await Promise.allSettled(restoredActiveJobs.map((job) => pollBookKnowledgeJob(job.id, job.enid)));
  } catch (error) {
    if (!dedaoEbookAcquisitionState.message) {
      dedaoEbookAcquisitionState.message = `任务状态暂不可用：${error instanceof Error ? error.message : String(error)}`;
    }
  }
  if (getRoutePathname() === ROUTES.dedaoEbooks) {
    renderDedaoEbookAcquisition();
  }
}

function renderDedaoAudioDetail(route = getDedaoAudioRoute()) {
  const payload = dedaoLibraryState.audioDetail || {};
  const detail = payload.detail || {};
  const topics = Array.isArray(detail.topic_summary) ? detail.topic_summary : [];
  const markdown = String(payload.markdown || "").trim();
  const durationMinutes = Number(detail.duration || 0) > 0 ? `${Math.ceil(Number(detail.duration) / 60)} 分钟` : "-";
  const topicRows = topics.map((topic) => `
    <section class="dedao-audio-detail__topic">
      <h3>${escapeHTML(topic.title || "主题内容")}</h3>
      <div>${renderSimpleMarkdown(topic.sub_title || "")}</div>
    </section>
  `).join("");
  renderShell(`
    <main class="dedao-audio-detail">
      <section class="dedao-audio-detail__header">
        <a class="button button-ghost" href="${escapeAttribute(ROUTES.dedaoAudio)}">返回听书书架</a>
        <div class="dedao-audio-detail__identity">
          ${detail.icon ? `<img src="${escapeAttribute(detail.icon)}" alt="${escapeAttribute(detail.title || "听书封面")}">` : ""}
          <div>
            <p class="web-kicker">听书详情</p>
            <h1>${escapeHTML(detail.title || route?.enid || "得到听书")}</h1>
            <p>${escapeHTML(detail.audio_summary || "查看听书介绍与文稿。")}</p>
          </div>
        </div>
        <dl>
          <div><dt>时长</dt><dd>${escapeHTML(durationMinutes)}</dd></div>
          <div><dt>学习</dt><dd>${escapeHTML(detail.learn_count_desc || "-")}</dd></div>
          <div><dt>解读者</dt><dd>${escapeHTML(detail.agency_detail?.qcg_member_name || detail.agency_detail?.name || "-")}</dd></div>
        </dl>
      </section>
      ${dedaoLibraryState.audioDetailLoading ? '<p class="web-status">正在加载听书内容...</p>' : ""}
      ${dedaoLibraryState.audioDetailMessage ? `<p class="web-status">${escapeHTML(dedaoLibraryState.audioDetailMessage)}</p>` : ""}
      ${detail.title ? `
        <section class="dedao-audio-detail__content">
          <aside>
            <h2>内容提要</h2>
            ${topicRows || renderSimpleMarkdown(detail.audio_summary || "暂无主题摘要。")}
          </aside>
          <article class="knowledge-web__answer dedao-course-article__body">
            <p class="web-kicker">听书文稿</p>
            ${markdown ? renderCourseMarkdown(markdown) : `<p>${escapeHTML(payload.transcript_error || "该听书暂未提供可读取文稿，仍可查看内容提要。")}</p>`}
          </article>
        </section>
      ` : ""}
    </main>
  `, "odob");
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
      : (category === "odob" && enid
        ? buildDedaoAudioURL(enid, item.audio_detail?.alias_id || "")
        : (enid ? `${cfg.path}/${encodeURIComponent(enid)}` : ""));
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
  const [home, session] = await Promise.allSettled([
    apiFetch("/api/dedao/home?page_size=4"),
    loadDedaoHomeSession(),
  ]);
  if (home.status === "fulfilled") {
    dedaoLibraryState.home = home.value;
  } else {
    dedaoLibraryState.homeMessage = home.reason instanceof Error ? home.reason.message : String(home.reason);
  }
  if (session.status === "rejected" && !dedaoLibraryState.homeMessage) {
    dedaoLibraryState.homeMessage = "得到登录状态暂不可用，内容仍可继续浏览。";
  }
  dedaoLibraryState.homeLoading = "";
  renderDedaoHome();
}

async function loadDedaoHomeSession() {
  dedaoLoginState.session = await apiFetch("/api/dedao/session");
  if (dedaoLoginState.phase !== "success") {
    dedaoLoginState.phase = "idle";
  }
  return dedaoLoginState.session;
}

async function loadDedaoCourses() {
  return loadDedaoLibrary("bauhinia");
}

async function loadDedaoLibrary(category) {
  const cfg = dedaoLibraryConfig[category] || dedaoLibraryConfig.bauhinia;
  const state = dedaoLibraryState.pages[category] || dedaoLibraryState.pages.bauhinia;
  state.loading = "loading";
  state.message = "";
  if (category === "ebook") renderDedaoEbookAcquisition(); else renderDedaoLibrary(category);
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
    if (category === "ebook") renderDedaoEbookAcquisition(); else renderDedaoLibrary(category);
  }
}

async function loadDedaoAudioDetail(route = getDedaoAudioRoute()) {
  if (!route?.enid) {
    dedaoLibraryState.audioDetailMessage = "听书链接缺少 enid。";
    renderDedaoAudioDetail(route);
    return;
  }
  dedaoLibraryState.audioDetail = null;
  dedaoLibraryState.audioDetailLoading = "loading";
  dedaoLibraryState.audioDetailMessage = "";
  renderDedaoAudioDetail(route);
  try {
    const query = new URLSearchParams({ enid: route.enid });
    if (route.aliasID) {
      query.set("alias_id", route.aliasID);
    }
    dedaoLibraryState.audioDetail = await apiFetch(`/api/dedao/audio?${query.toString()}`);
  } catch (error) {
    dedaoLibraryState.audioDetailMessage = error instanceof Error ? error.message : String(error);
  } finally {
    dedaoLibraryState.audioDetailLoading = "";
    renderDedaoAudioDetail(route);
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
  if (source === "kbase") {
    const knowledgeBookID = task?.result?.knowledge_book_id || "";
    return {
      id: String(task?.id || ""),
      source: "KBase",
      title: task?.result?.title || `电子书 ${task?.ebook_id || ""}`.trim(),
      operation: task?.type === "dedao_ebook_sync_kbase" ? "下载并入知识库" : "电子书下载",
      status: task?.status || "unknown",
      progress: task?.status === "running" ? "服务器本地处理中" : "",
      error: task?.error || "",
      updatedAt: task?.updated_at || task?.created_at || "",
      sourceURL: knowledgeBookID ? buildKnowledgePackageURL(knowledgeBookID) : ROUTES.dedaoEbooks,
      raw: task || {},
    };
  }
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
          <p>统一查看采集、下载、入库、分析和供给任务。得到电子书与 WC Plus 独立加载，单个来源故障不会遮蔽其他任务。</p>
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
    const [wcplusResult, kbaseResult] = await Promise.allSettled([
      apiFetch("/api/wcplus/task/all"),
      apiFetch("/api/jobs?limit=50"),
    ]);
    const wcplusTasks = wcplusResult.status === "fulfilled" && Array.isArray(wcplusResult.value?.tasks)
      ? wcplusResult.value.tasks.map((task) => normalizeJobTask(task, "wcplus"))
      : [];
    const kbaseTasks = kbaseResult.status === "fulfilled" && Array.isArray(kbaseResult.value?.jobs)
      ? kbaseResult.value.jobs.map((task) => normalizeJobTask(task, "kbase"))
      : [];
    jobCenterState.tasks = [...kbaseTasks, ...wcplusTasks];
    jobCenterState.lastUpdated = new Date().toLocaleString("zh-CN");
    const errors = [];
    if (wcplusResult.status === "rejected") errors.push(jobCenterErrorMessage(wcplusResult.reason));
    if (kbaseResult.status === "rejected") errors.push(`KBase 任务加载失败：${kbaseResult.reason instanceof Error ? kbaseResult.reason.message : String(kbaseResult.reason)}`);
    jobCenterState.message = errors.length
      ? `${jobCenterState.tasks.length ? `已加载 ${jobCenterState.tasks.length} 个任务。` : ""}${errors.join(" ")}`
      : `已加载 ${jobCenterState.tasks.length} 个任务。`;
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

function knowledgePackageAgentMatch() {
  const releaseID = knowledgeState.selectedRelease?.release_id
    || knowledgeState.releaseDetail?.release_id
    || "";
  if (!releaseID) {
    return null;
  }
  return (knowledgeState.agentPackages || []).find((pkg) => (
    pkg.lifecycle_state === "published"
    && Array.isArray(pkg.releases)
    && pkg.releases.some((release) => release.release_id === releaseID)
  )) || null;
}

function knowledgePackageLifecycle() {
  const pkg = knowledgeState.package || {};
  const book = pkg.book || knowledgeState.selectedBook || {};
  const task = knowledgeReviewLatestTask();
  const reviewStatus = knowledgeReviewStatus();
  const manifest = knowledgeState.analysisManifest || {};
  const agentPackage = knowledgePackageAgentMatch();
  const contentReady = Boolean(
    book.book_id
    && ((pkg.chapters || []).length || (pkg.claims || []).length || (pkg.chunks || []).length),
  );
  const analysisReady = manifest.status === "ready" && Boolean(manifest.answer);
  const releaseReady = Boolean(knowledgeState.selectedRelease?.release_id);
  const qualityFailed = task?.status === "failed"
    || knowledgeState.qualityReport?.decision === "fail";

  return [
    {
      key: "content",
      label: "内容",
      state: contentReady ? "ready" : "blocked",
      detail: contentReady ? "已形成可检索知识包" : "等待章节、claims 或 chunks",
      target: "#knowledge-overview",
    },
    {
      key: "analysis",
      label: "分析",
      state: analysisReady ? "ready" : (manifest.status === "failed" ? "failed" : "pending"),
      detail: analysisReady ? "基线分析已生成" : (manifest.error || "等待生成知识基线"),
      target: "#knowledge-analysis",
    },
    {
      key: "quality",
      label: "质量与发布",
      state: releaseReady ? "ready" : (qualityFailed ? "failed" : "pending"),
      detail: releaseReady
        ? `Release ${knowledgeHash(knowledgeState.selectedRelease.release_id)}`
        : knowledgeReviewStatusLabel(reviewStatus),
      target: "#knowledge-quality",
    },
    {
      key: "agent",
      label: "Agent",
      state: agentPackage ? "ready" : (releaseReady ? "pending" : "blocked"),
      detail: agentPackage
        ? `${agentPackage.package_id} · ${agentPackage.version}`
        : (releaseReady ? "等待 Agent Package 评测与发布" : "需先发布知识 Release"),
      target: "#knowledge-agent",
      href: agentPackage ? buildAgentPackageURL(agentPackage.package_id, agentPackage.version) : "",
    },
  ];
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

function scrollKnowledgeHashTarget() {
  const targetID = String(window.location.hash || "").replace(/^#/, "");
  if (!["knowledge-overview", "knowledge-quality", "knowledge-evidence", "knowledge-analysis", "knowledge-agent"].includes(targetID)) {
    return;
  }
  window.requestAnimationFrame?.(() => {
    document.getElementById(targetID)?.scrollIntoView({ block: "start" });
  });
}

function renderBookKnowledge() {
  const isPackageDetail = isKnowledgePackageDetailRoute();
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
  const cockpitHTML = isPackageDetail ? "" : renderKnowledgeReviewCockpit();
  const pipelineHTML = isPackageDetail ? "" : renderKnowledgePipelineDashboard();
  const currentIndex = knowledgeState.books.findIndex((book) => book.book_id === currentBook.book_id);
  const previousBook = currentIndex > 0 ? knowledgeState.books[currentIndex - 1] : null;
  const nextBook = currentIndex >= 0 && currentIndex < knowledgeState.books.length - 1
    ? knowledgeState.books[currentIndex + 1]
    : null;
  const lifecycle = isPackageDetail ? knowledgePackageLifecycle() : [];
  const agentPackage = isPackageDetail ? knowledgePackageAgentMatch() : null;
  const metadata = [
    currentBook.source_type,
    currentBook.extractor,
    currentBook.updated_at ? `更新 ${formatSourceControlTime(currentBook.updated_at)}` : "",
  ].filter(Boolean);
  const lifecycleHTML = lifecycle.map((stage, index) => `
    <a
      class="knowledge-workspace__stage is-${escapeAttribute(stage.state)}"
      href="${escapeAttribute(stage.href || stage.target)}"
      ${stage.key === "quality" ? 'data-knowledge-lifecycle="quality"' : ""}
    >
      <span>${String(index + 1).padStart(2, "0")}</span>
      <div>
        <strong>${escapeHTML(stage.label)}</strong>
        <small>${escapeHTML(stage.detail)}</small>
      </div>
    </a>
  `).join("");

  renderShell(`
    <main class="knowledge-web ${isPackageDetail ? "knowledge-web--detail" : "knowledge-web--index"} ${knowledgeState.directoryCollapsed ? "is-directory-collapsed" : ""}">
      <section class="knowledge-web__header">
        <div>
          <p class="web-kicker">${isPackageDetail ? "Knowledge Package" : "Book Knowledge"}</p>
          <h1>${isPackageDetail ? "知识包" : "书籍知识库"}</h1>
        </div>
        <div class="knowledge-web__header-actions">
          <button id="knowledge-refresh" class="button button-ghost" type="button">刷新</button>
        </div>
      </section>
      ${status}
      ${isPackageDetail ? `
        <nav class="knowledge-web__detail-toolbar" aria-label="知识包导航">
          <a class="knowledge-web__detail-back" href="${escapeAttribute(ROUTES.knowledgePackages)}">← 返回全部知识包</a>
          <span>${currentIndex >= 0 ? `${currentIndex + 1} / ${knowledgeState.books.length}` : "知识包详情"}</span>
          <div>
            <button id="knowledge-directory-toggle" class="button button-ghost" type="button" aria-expanded="${!knowledgeState.directoryCollapsed}">
              ${knowledgeState.directoryCollapsed ? "展开目录" : "收起目录"}
            </button>
            ${previousBook ? `<a class="button button-ghost" href="${escapeAttribute(sourceKnowledgeURL(previousBook.book_id))}" title="${escapeAttribute(previousBook.title || "上一条")}">← 上一条</a>` : `<span class="button button-ghost is-disabled">← 上一条</span>`}
            ${nextBook ? `<a class="button button-ghost" href="${escapeAttribute(sourceKnowledgeURL(nextBook.book_id))}" title="${escapeAttribute(nextBook.title || "下一条")}">下一条 →</a>` : `<span class="button button-ghost is-disabled">下一条 →</span>`}
          </div>
        </nav>
      ` : ""}
      ${cockpitHTML}
      ${pipelineHTML}

      ${isPackageDetail ? `<div class="knowledge-web__layout">
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
            <section id="knowledge-overview" class="knowledge-workspace__overview">
              <div class="knowledge-web__title-row">
                <div>
                  <p class="web-kicker">${escapeHTML(currentBook.book_id)}</p>
                  <h2>${escapeHTML(currentBook.title || currentBook.book_id)}</h2>
                  ${metadata.length ? `<p class="knowledge-workspace__metadata">${metadata.map((item) => `<span>${escapeHTML(item)}</span>`).join("")}</p>` : ""}
                </div>
                <a class="button button-primary" href="${escapeAttribute(buildBookReaderURL(currentBook.book_id))}">阅读</a>
              </div>
              <div class="knowledge-web__stats">
                <span>${(pkg.chapters || []).length} 章</span>
                <span>${(pkg.claims || []).length} claims</span>
                <span>${(pkg.chunks || []).length} chunks</span>
              </div>
              <div class="knowledge-workspace__lifecycle" aria-label="知识包生命周期">
                ${lifecycleHTML}
              </div>
            </section>
            <nav class="knowledge-workspace__nav" aria-label="知识包工作区">
              <a href="#knowledge-overview">概览</a>
              <a href="#knowledge-quality">质量</a>
              <a href="#knowledge-evidence">证据</a>
              <a href="#knowledge-analysis">分析</a>
              <a href="#knowledge-agent">Agent</a>
            </nav>
            <section id="knowledge-quality" class="knowledge-workspace__section">
              ${renderKnowledgeReview()}
            </section>
            <div id="knowledge-evidence" class="knowledge-web__content knowledge-workspace__section">
              <section>
                <p class="web-kicker">Chapters</p>
                <ul>${chapterRows || "<li><span>暂无章节</span></li>"}</ul>
              </section>
              <section>
                <p class="web-kicker">Search Results</p>
                ${resultRows || "<p class=\"web-muted\">输入关键词后查看检索结果。</p>"}
              </section>
            </div>
            <section id="knowledge-analysis" class="knowledge-workspace__section knowledge-workspace__analysis">
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
            </section>
            <section id="knowledge-agent" class="knowledge-workspace__section knowledge-workspace__agent">
              <div class="knowledge-workspace__agent-copy">
                <p class="web-kicker">Agent Supply</p>
                <h3>${agentPackage ? "Agent Package 已可用" : "供给到 Agent"}</h3>
                <p>
                  ${agentPackage
                    ? `当前知识 Release 已固定到 ${escapeHTML(agentPackage.package_id)} ${escapeHTML(agentPackage.version)}，可进入受控检索、证据问答和工具调用。`
                    : escapeHTML(knowledgeState.agentPackagesLoading || knowledgeState.agentPackagesError || (knowledgeState.selectedRelease
                      ? "知识 Release 已发布，等待 Agent Package 完成策略配置、评测和独立发布。"
                      : "先完成基线分析、质量校验和知识 Release 发布，才能生成可运行 Agent。"))}
                </p>
              </div>
              <div class="knowledge-workspace__agent-actions">
                ${agentPackage ? `
                  <a class="button button-primary" href="${escapeAttribute(buildAgentPackageURL(agentPackage.package_id, agentPackage.version))}">打开 Agent Package</a>
                  <a class="button button-ghost" href="${escapeAttribute(buildAgentURL(agentPackage.package_id, agentPackage.version))}">运行 Agent</a>
                ` : `<a class="button button-ghost" href="${escapeAttribute(ROUTES.agentPackages)}">查看 Book Agents</a>`}
              </div>
            </section>
          ` : "<p class=\"web-muted\">请选择书籍或导入新来源。</p>"}
        </section>
      </div>` : `
        <section class="knowledge-web__catalog" aria-label="知识包目录">
          <div class="knowledge-web__catalog-head">
            <div>
              <p class="web-kicker">Knowledge Catalog</p>
              <h2>全部知识包</h2>
              <p>选择一个知识包进入阅读、复核与大模型分析工作区。</p>
            </div>
            <form id="knowledge-search-form" class="knowledge-web__catalog-search">
              <input name="query" value="${escapeAttribute(knowledgeState.query)}" placeholder="搜索标题、claims 或 chunks" aria-label="搜索知识包">
              <button class="button button-primary" type="submit">搜索</button>
            </form>
          </div>
          <div class="knowledge-web__books knowledge-web__catalog-grid">
            ${bookRows || "<p class=\"web-muted\">暂无知识库条目，可先从微信来源导入。</p>"}
          </div>
        </section>
      `}
    </main>
  `, "knowledge");
  bindBookKnowledgeEvents();
  scrollKnowledgeHashTarget();
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
      ${renderAgentCompiler()}
      <section class="book-agent__package-grid" aria-label="Published Agent Packages">
        ${rows || `<div class="book-agent__empty"><strong>尚无已发布 Agent Package</strong><p>先完成知识发布与评测；这里不会用示例内容伪造可运行产品。</p></div>`}
      </section>
    </main>
  `;
}

function renderAgentCompiler() {
  const releases = Array.isArray(agentCompilerState.releases) ? agentCompilerState.releases : [];
  const selectedSupport = new Set(agentCompilerState.supportingReleaseIDs);
  const releaseLabel = (release) => (
    `${release.book_id || "book"} · ${release.release_id || "release"}`
  );
  const result = agentCompilerState.result;
  const candidateRows = (result?.candidates || []).map((candidate) => {
    const issues = Array.isArray(candidate.issues) ? candidate.issues : [];
    const nextActions = Array.isArray(candidate.next_actions) ? candidate.next_actions : [];
    return `
      <article class="agent-compiler__candidate is-${escapeAttribute(candidate.status || "blocked")}">
        <header>
          <span>${escapeHTML(candidate.kind || "candidate")}</span>
          <strong>${candidate.status === "ready" ? "已生成" : "被阻断"}</strong>
        </header>
        ${candidate.package ? `
          <dl>
            <div><dt>Package</dt><dd>${escapeHTML(candidate.package.package_id || "—")}</dd></div>
            <div><dt>Version</dt><dd>${escapeHTML(candidate.package.version || "—")}</dd></div>
            <div><dt>Hash</dt><dd><code>${escapeHTML(candidate.package.content_hash || "—")}</code></dd></div>
          </dl>
        ` : ""}
        ${issues.length ? `
          <ul class="agent-compiler__issues">
            ${issues.map((issue) => `<li><code>${escapeHTML(issue.code || "blocked")}</code><span>${escapeHTML(issue.message || "")}</span></li>`).join("")}
          </ul>
        ` : ""}
        ${nextActions.length ? `<p>下一步：${nextActions.map((action) => escapeHTML(action)).join(" · ")}</p>` : ""}
      </article>
    `;
  }).join("");
  return `
    <section class="agent-compiler" aria-labelledby="agent-compiler-title">
      <header>
        <div>
          <p class="web-kicker">Agent Compiler</p>
          <h2 id="agent-compiler-title">从 Release 生成候选包</h2>
        </div>
        <p>只读编译，不保存草稿。候选包必须经过可信评测，才能进入既有发布流程。</p>
      </header>
      <div class="agent-compiler__mode" role="group" aria-label="编译模式">
        ${[
          ["dual", "双模板"],
          ["evidence", "证据"],
          ["study", "学习"],
        ].map(([mode, label]) => `
          <button
            type="button"
            class="${agentCompilerState.mode === mode ? "is-active" : ""}"
            data-agent-compiler-mode="${mode}"
            aria-pressed="${agentCompilerState.mode === mode ? "true" : "false"}"
          >${label}</button>
        `).join("")}
      </div>
      <form id="agent-compiler-form" class="agent-compiler__controls">
        <label>
          <span>主 Release</span>
          <select name="primary_release_id" required ${releases.length ? "" : "disabled"}>
            ${releases.map((release) => `
              <option value="${escapeAttribute(release.release_id)}" ${release.release_id === agentCompilerState.primaryReleaseID ? "selected" : ""}>
                ${escapeHTML(releaseLabel(release))}
              </option>
            `).join("")}
          </select>
        </label>
        <label>
          <span>版本</span>
          <input name="version" value="${escapeAttribute(agentCompilerState.version)}" inputmode="text" pattern="[0-9]+\\.[0-9]+\\.[0-9]+(?:[+\\-][0-9A-Za-z.\\-]+)?" required>
        </label>
        ${agentCompilerState.mode === "study" ? `
          <p class="agent-compiler__study-note">学习模式只绑定主 Release，不选择支持源。</p>
        ` : `
          <fieldset class="agent-compiler__release-list">
            <legend>支持 Release <small>可留空，由 Assembly 自动选择相关独立来源</small></legend>
            <div>
              ${releases.filter((release) => release.release_id !== agentCompilerState.primaryReleaseID).map((release) => `
                <label>
                  <input
                    type="checkbox"
                    name="supporting_release_ids"
                    value="${escapeAttribute(release.release_id)}"
                    ${selectedSupport.has(release.release_id) ? "checked" : ""}
                  >
                  <span>${escapeHTML(releaseLabel(release))}</span>
                </label>
              `).join("") || `<p>当前没有其他可选 Release。</p>`}
            </div>
          </fieldset>
        `}
        <footer>
          <span role="status">${escapeHTML(agentCompilerState.error || agentCompilerState.loading || `${releases.length} 个最新 Release 可用于编译`)}</span>
          <button class="button button-primary" type="submit" ${!releases.length ? "disabled" : ""}>编译候选包</button>
        </footer>
      </form>
      ${result ? `
        <section class="agent-compiler__result" aria-live="polite">
          <header><span>Compilation</span><strong>${escapeHTML(result.status || "blocked")}</strong><code>${escapeHTML(result.compilation_id || "")}</code></header>
          <div>${candidateRows}</div>
          <p>候选内容未持久化。下一步使用 <code>run_trusted_evaluation</code> 完成可信评测，再由 publisher API 发布。</p>
        </section>
      ` : ""}
    </section>
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

function renderBookAgentAnswerCitations(answer) {
  const citations = Array.isArray(answer?.citations) ? answer.citations : [];
  if (!citations.length) {
    return "";
  }
  return `
    <div class="book-agent__answer-citations" aria-label="Answer citations">
      ${citations.map((citation) => `
        <article>
          <strong>${escapeHTML(citation.citation_id || "citation")}</strong>
          <span>${escapeHTML(citation.book_id || "book")} · ${escapeHTML(citation.chunk_id || citation.chapter_id || "anchored evidence")}</span>
          ${citation.note ? `<p>${escapeHTML(citation.note)}</p>` : ""}
        </article>
      `).join("")}
    </div>
  `;
}

function evidenceAuditPrimaryRelease() {
  const roles = bookAgentState.package?.evidence_policy?.release_roles;
  const primaryID = Array.isArray(roles)
    ? roles.find((item) => item.role === "primary")?.release_id
    : "";
  return bookAgentState.releases.find((release) => release.release_id === primaryID) || null;
}

function evidenceAuditPrimaryClaims() {
  const claims = evidenceAuditPrimaryRelease()?.analysis?.claims;
  return Array.isArray(claims) ? claims : [];
}

function evidenceAuditErrorDetails(error) {
  const nested = error?.payload?.error;
  if (nested && typeof nested === "object") {
    return {
      code: String(nested.code || "audit_request_failed"),
      message: String(nested.message || error.message || "证据审计请求失败"),
    };
  }
  return {
    code: String(error?.payload?.code || "audit_request_failed"),
    message: String(error?.message || "证据审计请求失败"),
  };
}

function evidenceAuditIdempotencyKey(prefix, identity = "") {
  const random = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `${prefix}:${identity || "new"}:${random}`;
}

function evidenceAuditStatusLabel(status) {
  return {
    queued: "等待执行",
    running: "审计中",
    failed: "执行失败",
    completed: "审计完成",
  }[status] || status || "尚未开始";
}

function evidenceAuditVerdictLabel(verdict) {
  return {
    supported: "支持",
    contradicted: "冲突",
    mixed: "证据不一",
    insufficient: "证据不足",
    abstained: "暂不裁定",
  }[verdict] || verdict || "未裁定";
}

function evidenceAuditFreshnessLabel(decision) {
  return {
    fresh: "时效合格",
    stale: "已过时",
    missing: "日期缺失",
  }[decision] || decision || "未判定";
}

function evidenceAuditCitationURL(evidence) {
  const release = bookAgentState.releases.find((item) => item.release_id === evidence.release_id);
  const bookID = release?.book_id || release?.book?.book_id || "";
  if (!bookID) {
    return ROUTES.knowledgePackages;
  }
  const params = new URLSearchParams();
  for (const [name, value] of [
    ["release_id", evidence.release_id],
    ["claim_id", evidence.claim_id],
    ["chunk_id", evidence.chunk_id],
    ["citation_id", evidence.citation_id],
  ]) {
    if (value) {
      params.set(name, value);
    }
  }
  const query = params.toString();
  return `${buildKnowledgePackageURL(bookID)}${query ? `?${query}` : ""}#knowledge-evidence`;
}

function canRetryEvidenceAudit(audit) {
  return audit?.status === "failed" &&
    ["model_outcome_unknown", "requires_manual_retry"].includes(audit?.failure_code);
}

function renderEvidenceAuditComposer(pkg) {
  if (pkg.schema_version !== "agent-package.v2") {
    return `
      <section class="book-agent__capability book-agent__unavailable">
        <span class="book-agent__capability-index">evidence audit</span>
        <div>
          <strong>当前版本不提供证据审计</strong>
          <p>此能力从 v2 package 开始，旧版本保持不可变，不显示可提交的审计表单。</p>
        </div>
      </section>
    `;
  }
  const policy = pkg.evidence_policy || {};
  const claims = evidenceAuditPrimaryClaims();
  const maxClaims = Math.max(1, Number(policy.max_claims || 1));
  const selected = new Set(evidenceAuditState.selectedClaims);
  const atLimit = selected.size >= maxClaims;
  return `
    <section class="evidence-audit__composer" aria-labelledby="evidence-audit-composer-title">
      <header class="evidence-audit__section-head">
        <div>
          <span>NEW AUDIT</span>
          <h3 id="evidence-audit-composer-title">新建证据审计</h3>
        </div>
        <p>从主书 claim 出发，在固定支持来源中核验，不替代临床裁决。</p>
      </header>
      <div class="evidence-audit__policy" aria-label="审计策略摘要">
        <div><span>来源独立性</span><strong>至少 ${Number(policy.minimum_independent_sources || 0)} 个独立来源</strong></div>
        <div><span>证据时效</span><strong>${Number(policy.freshness_policy?.max_age_days || 0)} 天内${policy.freshness_policy?.require_publication_date ? " · 必须有日期" : ""}</strong></div>
        <div><span>预算上限</span><strong>$${Number(pkg.model_policy?.max_cost_usd || 0).toFixed(2)} · ${Number(pkg.model_policy?.timeout_ms || 0) / 1000}s</strong></div>
      </div>
      <form id="evidence-audit-create-form" class="evidence-audit__form">
        <label>
          <span>审计主题</span>
          <input name="subject" value="${escapeAttribute(evidenceAuditState.subject)}" maxlength="240" required placeholder="例如：关键临床试验结论复核">
        </label>
        <label>
          <span>审计范围</span>
          <textarea name="scope" rows="3" maxlength="1200" required placeholder="说明人群、干预、对照、结局或需要排除的范围">${escapeHTML(evidenceAuditState.scope)}</textarea>
        </label>
        <fieldset>
          <legend>主书 claims <small>最多 ${maxClaims} 项 · 已选 ${selected.size}</small></legend>
          <div class="evidence-audit__claim-picker">
            ${claims.map((claim, index) => {
              const id = String(claim.id || "");
              const checked = selected.has(id);
              return `
                <label class="${checked ? "is-selected" : ""}">
                  <input type="checkbox" name="selected_claims" value="${escapeAttribute(id)}" ${checked ? "checked" : ""} ${!checked && atLimit ? "disabled" : ""}>
                  <span>${String(index + 1).padStart(2, "0")}</span>
                  <strong>${escapeHTML(claim.statement || id)}</strong>
                </label>
              `;
            }).join("") || `<p class="web-muted">主 release 暂无可选择的结构化 claims。</p>`}
          </div>
        </fieldset>
        <footer>
          <span aria-live="polite">${escapeHTML(evidenceAuditState.error || evidenceAuditState.loading || "审计创建后会生成稳定链接。")}</span>
          <button class="button button-primary" type="submit" ${evidenceAuditState.loading || !claims.length || !selected.size ? "disabled" : ""}>创建审计</button>
        </footer>
      </form>
    </section>
  `;
}

function renderEvidenceAuditStatus(audit) {
  if (!audit) {
    return "";
  }
  const terminal = audit.status === "failed" || audit.status === "completed";
  return `
    <section class="evidence-audit__status is-${escapeAttribute(audit.status)}" aria-live="polite" aria-busy="${terminal ? "false" : "true"}">
      <div>
        <span>状态</span>
        <strong>${escapeHTML(evidenceAuditStatusLabel(audit.status))}</strong>
      </div>
      <div>
        <span>审计 ID</span>
        <code>${escapeHTML(audit.audit_id || "")}</code>
      </div>
      <div>
        <span>尝试次数</span>
        <strong>${Number(audit.attempt || 1)}</strong>
      </div>
      ${audit.status === "failed" ? `
        <div class="evidence-audit__failure">
          <span>${escapeHTML(audit.failure_code || "audit_failed")}</span>
          <strong>${escapeHTML(audit.failure_summary || "审计未完成。")}</strong>
        </div>
        ${canRetryEvidenceAudit(audit) ? `<button class="button button-ghost" type="button" data-evidence-audit-retry>手动重试</button>` : ""}
      ` : ""}
    </section>
  `;
}

function renderEvidenceAuditEvidenceRow(evidence, index) {
  return `
    <details class="evidence-audit__evidence-row">
      <summary>
        <span>${String(index + 1).padStart(2, "0")}</span>
        <strong>${escapeHTML(evidence.publication_identity || evidence.source_type || "证据来源")}</strong>
        <small>${escapeHTML(evidenceAuditFreshnessLabel(evidence.freshness_decision))}${evidence.conflict ? " · 存在冲突" : ""}</small>
      </summary>
      <dl>
        <div><dt>Release</dt><dd>${escapeHTML(evidence.release_id || "—")}</dd></div>
        <div><dt>来源 / 角色</dt><dd>${escapeHTML(evidence.source_type || "—")} · ${escapeHTML(evidence.role || "—")}</dd></div>
        <div><dt>发布日期</dt><dd>${escapeHTML(evidence.published_at || "未提供")}</dd></div>
        <div><dt>Claim / Chunk</dt><dd>${escapeHTML(evidence.claim_id || "—")} · ${escapeHTML(evidence.chunk_id || "—")}</dd></div>
      </dl>
      <a href="${escapeAttribute(evidenceAuditCitationURL(evidence))}">查看 KBase 引用 ${escapeHTML(evidence.citation_id || "")} →</a>
    </details>
  `;
}

function renderEvidenceAuditClaim(claim, index) {
  const evidence = Array.isArray(claim.evidence_refs) ? claim.evidence_refs : [];
  const confidence = Math.round(Number(claim.computed_confidence || 0) * 100);
  const renderList = (items, empty) => (
    Array.isArray(items) && items.length
      ? `<ul>${items.map((item) => `<li>${renderSimpleMarkdown(item)}</li>`).join("")}</ul>`
      : `<p>${escapeHTML(empty)}</p>`
  );
  return `
    <article class="evidence-audit__claim">
      <header>
        <span>主张 CLAIM ${String(index + 1).padStart(2, "0")}</span>
        <div class="evidence-audit__verdict is-${escapeAttribute(claim.verdict)}">${escapeHTML(evidenceAuditVerdictLabel(claim.verdict))}</div>
        <strong>置信度 ${confidence}%</strong>
      </header>
      <div class="evidence-audit__claim-statement">${renderSimpleMarkdown(claim.normalized_statement || claim.source_claim || "")}</div>
      <section>
        <h4>证据卷宗 <span>${evidence.length}</span></h4>
        <div>${evidence.map(renderEvidenceAuditEvidenceRow).join("") || `<p class="web-muted">没有满足策略的可引用证据。</p>`}</div>
      </section>
      <div class="evidence-audit__review-grid">
        <section><h4>局限</h4>${renderList(claim.limitations, "未记录额外局限。")}</section>
        <section><h4>知识缺口</h4>${renderList(claim.knowledge_gaps, "未记录额外缺口。")}</section>
        <section><h4>复核行动</h4>${renderList(claim.review_actions, "无需额外行动。")}</section>
      </div>
    </article>
  `;
}

function renderEvidenceAuditTrace(audit) {
  const trace = audit.trace || {};
  const observability = audit.observability || trace.observability || {};
  const stages = Array.isArray(observability.stages) ? observability.stages : [];
  const usage = observability.usage || {};
  const started = Date.parse(audit.started_at || "");
  const ended = Date.parse(audit.completed_at || audit.failed_at || "");
  const duration = Number.isFinite(started) && Number.isFinite(ended) ? Math.max(0, ended - started) : 0;
  return `
    <section class="evidence-audit__trace" aria-labelledby="evidence-audit-trace-title">
      <header><span>TRACE</span><h3 id="evidence-audit-trace-title">Trace 与用量</h3></header>
      <dl>
        <div><dt>Trace ID</dt><dd>${escapeHTML(audit.trace_id || "未生成")}</dd></div>
        <div><dt>总耗时</dt><dd>${duration ? `${(duration / 1000).toFixed(1)}s` : "未提供"}</dd></div>
        <div><dt>独立来源</dt><dd>${Number(observability.independent_publication_source_count || 0) || "未提供"}</dd></div>
        <div><dt>引用解析率</dt><dd>${Number.isFinite(Number(observability.citation_resolution_rate)) ? `${Math.round(Number(observability.citation_resolution_rate) * 100)}%` : "未提供"}</dd></div>
        <div><dt>Tokens</dt><dd>${Number(usage.total_tokens || 0) || "未提供"}</dd></div>
        <div><dt>Cost</dt><dd>${Number.isFinite(Number(usage.cost_usd)) && usage.status !== "unknown" ? `$${Number(usage.cost_usd).toFixed(4)}` : "unknown"}</dd></div>
      </dl>
      ${stages.length ? `
        <div class="evidence-audit__stages">
          ${stages.map((stage) => `<div><span>${escapeHTML(stage.name || "stage")}</span><strong>${escapeHTML(stage.status || "—")}</strong><small>${Number(stage.duration_ms || 0)}ms</small></div>`).join("")}
        </div>
      ` : `<p class="web-muted">${escapeHTML(audit.trace_error || "当前 Trace 未包含阶段耗时明细。")}</p>`}
    </section>
  `;
}

function countProofroomRedactions(value) {
  if (!value || typeof value !== "object") {
    return 0;
  }
  if (Array.isArray(value)) {
    return value.reduce((total, item) => total + countProofroomRedactions(item), 0);
  }
  return (value.redacted === true ? 1 : 0) +
    Object.values(value).reduce((total, item) => total + countProofroomRedactions(item), 0);
}

function renderProofroomSafeText(value, fallback = "—") {
  const text = typeof value === "string" ? value : value?.text;
  return escapeHTML(text || fallback);
}

function renderProofroomPreviewClaim(claim, index) {
  const evidence = Array.isArray(claim?.evidence) ? claim.evidence : [];
  const limitations = Array.isArray(claim?.limitations) ? claim.limitations : [];
  const gaps = Array.isArray(claim?.knowledge_gaps) ? claim.knowledge_gaps : [];
  const actions = Array.isArray(claim?.review_actions) ? claim.review_actions : [];
  return `
    <article class="evidence-audit__proofroom-claim">
      <header>
        <span>CLAIM ${String(index + 1).padStart(2, "0")}</span>
        <strong>${escapeHTML(evidenceAuditVerdictLabel(claim?.verdict))}</strong>
        <small>置信度 ${Math.round(Number(claim?.computed_confidence || 0) * 100)}%</small>
      </header>
      <p>${renderProofroomSafeText(claim?.normalized_statement, "未提供规范化主张")}</p>
      <div>
        <strong>将投递的引用</strong>
        ${evidence.length ? `<ul>${evidence.map((item) => `
          <li>
            <code>${escapeHTML(item.citation_id || "citation")}</code>
            <span>${escapeHTML(item.release_id || "release")} · ${escapeHTML(item.claim_id || "claim")} · ${escapeHTML(item.chunk_id || "chunk")}</span>
            <small>${escapeHTML(item.role || "role")} · ${escapeHTML(item.source_type || "source")} · ${escapeHTML(evidenceAuditFreshnessLabel(item.freshness_decision))}${item.conflict ? " · 存在冲突" : ""}</small>
          </li>
        `).join("")}</ul>` : `<p>无引用。</p>`}
      </div>
      ${limitations.length ? `<div><strong>局限</strong><ul>${limitations.map((item) => `<li>${renderProofroomSafeText(item)}</li>`).join("")}</ul></div>` : ""}
      ${gaps.length ? `<div><strong>知识缺口</strong><ul>${gaps.map((item) => `<li>${renderProofroomSafeText(item)}</li>`).join("")}</ul></div>` : ""}
      ${actions.length ? `<div><strong>复核行动</strong><ul>${actions.map((item) => `<li>${renderProofroomSafeText(item)}</li>`).join("")}</ul></div>` : ""}
    </article>
  `;
}

function renderEvidenceAuditProofroom(audit) {
  const preview = evidenceAuditState.proofroomPreview;
  const delivery = evidenceAuditState.deliveryReceipt;
  const previewClaims = Array.isArray(preview?.payload?.claims) ? preview.payload.claims : [];
  const reviewItems = Array.isArray(preview?.payload?.proofroom?.review_items)
    ? preview.payload.proofroom.review_items
    : [];
  const summaryLimitations = Array.isArray(preview?.payload?.summary?.limitations)
    ? preview.payload.summary.limitations
    : [];
  return `
    <section class="evidence-audit__proofroom" aria-labelledby="evidence-audit-proofroom-title">
      <header>
        <div><span>DOWNSTREAM REVIEW</span><h3 id="evidence-audit-proofroom-title">Proofroom</h3></div>
        <button class="button button-ghost" type="button" data-proofroom-preview ${evidenceAuditState.proofroomStatus === "loading" ? "disabled" : ""}>Proofroom 预览</button>
      </header>
      ${preview ? `
        <div class="evidence-audit__proofroom-overlay" role="dialog" aria-modal="true" aria-labelledby="proofroom-preview-title">
        <div class="evidence-audit__proofroom-preview">
          <header>
            <div><span>投递前审阅</span><h4 id="proofroom-preview-title">Proofroom 实际投递内容</h4></div>
            <button class="button button-ghost" type="button" data-proofroom-close aria-label="关闭 Proofroom 预览">关闭</button>
          </header>
          <dl>
            <div><dt>Payload hash</dt><dd>${escapeHTML(preview.payload_hash || "—")}</dd></div>
            <div><dt>DLP redactions</dt><dd>${countProofroomRedactions(preview.payload)}</dd></div>
            <div><dt>Claims</dt><dd>${preview.payload?.claims?.length || 0}</dd></div>
            <div><dt>裁决归属</dt><dd>${escapeHTML(preview.payload?.adjudication_authority || "proofroom")}</dd></div>
          </dl>
          <p>${escapeHTML(preview.summary || "投递前最小化预览已生成。")}</p>
          <section class="evidence-audit__proofroom-payload" aria-label="Proofroom 实际投递内容">
            <h4>${renderProofroomSafeText(preview.payload?.proofroom?.title, "Proofroom 复核任务")}</h4>
            <section>
              <strong>审计摘要</strong>
              <p>${renderProofroomSafeText(preview.payload?.summary?.conclusion, "未提供摘要")}</p>
              ${summaryLimitations.length ? `<ul>${summaryLimitations.map((item) => `<li>${renderProofroomSafeText(item)}</li>`).join("")}</ul>` : ""}
            </section>
            <div>${previewClaims.map(renderProofroomPreviewClaim).join("") || `<p>当前投影没有 claim。</p>`}</div>
            ${reviewItems.length ? `
              <aside>
                <strong>Proofroom 复核清单</strong>
                <ul>${reviewItems.map((item) => `<li>${renderProofroomSafeText(item)}</li>`).join("")}</ul>
              </aside>
            ` : ""}
            <details>
              <summary>核对完整结构化 Payload</summary>
              <pre>${escapeHTML(JSON.stringify(preview.payload, null, 2))}</pre>
            </details>
          </section>
          <button class="button button-primary" type="button" data-proofroom-deliver ${evidenceAuditState.proofroomStatus === "delivering" || ["delivered", "outcome_unknown"].includes(evidenceAuditState.proofroomStatus) ? "disabled" : ""}>发送到 Proofroom</button>
        </div>
        </div>
      ` : `<p>仅在点击预览后读取最小化 payload；审计完成不会自动发送。</p>`}
      <div class="evidence-audit__delivery is-${escapeAttribute(evidenceAuditState.proofroomStatus || "idle")}" aria-live="polite">
        ${delivery ? `<strong>${escapeHTML(delivery.status || "delivered")}</strong><span>${escapeHTML(delivery.remote_receipt_id || delivery.receipt_hash || "")}</span>` : ""}
        ${evidenceAuditState.proofroomStatus === "outcome_unknown" ? `<strong>unknown</strong><span>远端结果未知，需人工核对后再处理，系统不会自动重发。</span>` : ""}
        ${evidenceAuditState.proofroomStatus === "rejected" ? `<strong>rejected</strong><span>${escapeHTML(evidenceAuditState.proofroomError)}</span>` : ""}
        ${evidenceAuditState.proofroomError && !["outcome_unknown", "rejected"].includes(evidenceAuditState.proofroomStatus) ? `<span>${escapeHTML(evidenceAuditState.proofroomError)}</span>` : ""}
      </div>
    </section>
  `;
}

function deactivateProofroomModal({ restoreFocus = false } = {}) {
  if (proofroomKeydownHandler) {
    document.removeEventListener("keydown", proofroomKeydownHandler);
    proofroomKeydownHandler = null;
  }
  document.body?.classList?.remove("has-proofroom-modal");
  app.inert = false;
  document.body?.querySelector?.(":scope > .evidence-audit__proofroom-overlay")?.remove();
  if (restoreFocus && proofroomPreviousFocus?.isConnected && typeof proofroomPreviousFocus.focus === "function") {
    proofroomPreviousFocus.focus();
  }
  if (restoreFocus) {
    proofroomPreviousFocus = null;
    proofroomReturnFocusSelector = "";
  }
}

function closeProofroomPreview(route) {
  const returnFocusSelector = proofroomReturnFocusSelector;
  deactivateProofroomModal();
  evidenceAuditState.proofroomPreview = null;
  evidenceAuditState.proofroomDeliveryKey = "";
  evidenceAuditState.proofroomStatus = "";
  evidenceAuditState.proofroomError = "";
  renderBookAgentPlatform(route);
  window.requestAnimationFrame?.(() => {
    const target = returnFocusSelector ? document.querySelector(returnFocusSelector) : null;
    if (target && typeof target.focus === "function") {
      target.focus();
    } else if (proofroomPreviousFocus?.isConnected && typeof proofroomPreviousFocus.focus === "function") {
      proofroomPreviousFocus.focus();
    }
    proofroomPreviousFocus = null;
    proofroomReturnFocusSelector = "";
  });
}

function activateProofroomModal(route) {
  const overlay = app.querySelector(".evidence-audit__proofroom-overlay");
  if (!overlay) {
    return;
  }
  deactivateProofroomModal();
  proofroomPreviousFocus = proofroomPreviousFocus || document.activeElement;
  document.body?.appendChild?.(overlay);
  document.body?.classList?.add("has-proofroom-modal");
  app.inert = true;
  const dialog = overlay.querySelector('[role="dialog"], .evidence-audit__proofroom-preview') || overlay;
  const focusableSelector = 'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
  const focusable = () => Array.from(dialog.querySelectorAll(focusableSelector));
  proofroomKeydownHandler = (event) => {
    if (event.key === "Escape") {
      event.preventDefault();
      closeProofroomPreview(route);
      return;
    }
    if (event.key !== "Tab") {
      return;
    }
    const items = focusable();
    if (!items.length) {
      event.preventDefault();
      dialog.focus();
      return;
    }
    const first = items[0];
    const last = items[items.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };
  document.addEventListener("keydown", proofroomKeydownHandler);
  const close = overlay.querySelector("[data-proofroom-close]");
  (close || focusable()[0] || dialog).focus();
}

function renderEvidenceAuditReport(audit) {
  const claims = Array.isArray(audit?.claim_audits) ? audit.claim_audits : [];
  const counts = audit?.summary?.verdict_counts || {};
  return `
    <section class="evidence-audit__report" aria-labelledby="evidence-audit-report-title">
      <header class="evidence-audit__report-head">
        <div>
          <span>证据审计 · ${escapeHTML(audit.schema_version || "evidence-audit.v1")}</span>
          <h2 id="evidence-audit-report-title">审计结论</h2>
          <div class="evidence-audit__conclusion">${renderSimpleMarkdown(audit.summary?.conclusion || "审计已完成。")}</div>
        </div>
        <div class="evidence-audit__verdict-totals">
          ${Object.entries(counts).map(([verdict, count]) => `<div><strong>${Number(count || 0)}</strong><span>${escapeHTML(evidenceAuditVerdictLabel(verdict))}</span></div>`).join("") || `<div><strong>${claims.length}</strong><span>claims</span></div>`}
        </div>
      </header>
      <div class="evidence-audit__report-body">
        <div class="evidence-audit__claims">${claims.map(renderEvidenceAuditClaim).join("") || `<p class="web-muted">报告未返回 claim 审计结果。</p>`}</div>
        <aside>
          <section>
            <h3>局限与缺口</h3>
            ${Array.isArray(audit.summary?.limitations) && audit.summary.limitations.length
              ? `<ul>${audit.summary.limitations.map((item) => `<li>${renderSimpleMarkdown(item)}</li>`).join("")}</ul>`
              : `<p>未记录全局局限。</p>`}
          </section>
          ${renderEvidenceAuditTrace(audit)}
          ${renderEvidenceAuditProofroom(audit)}
        </aside>
      </div>
    </section>
  `;
}

function renderEvidenceAuditHistory(pkg) {
  if (!evidenceAuditState.audits.length || pkg.schema_version !== "agent-package.v2") {
    return "";
  }
  return `
    <nav class="evidence-audit__history" aria-label="最近证据审计">
      <span>最近审计</span>
      ${evidenceAuditState.audits.slice(0, 6).map((audit) => `
        <a href="${escapeAttribute(buildEvidenceAuditURL(pkg.package_id, audit.audit_id, pkg.version))}">
          <strong>${escapeHTML(evidenceAuditStatusLabel(audit.status))}</strong>
          <small>${escapeHTML(audit.audit_id)}</small>
        </a>
      `).join("")}
    </nav>
  `;
}

function renderEvidenceAuditWorkspace(route, pkg) {
  if (route.view !== "agent") {
    return "";
  }
  if (pkg.schema_version !== "agent-package.v2") {
    return `
      <section class="evidence-audit evidence-audit--unavailable" aria-label="证据审计版本说明">
        ${renderEvidenceAuditComposer(pkg)}
      </section>
    `;
  }
  const audit = evidenceAuditState.audit;
  return `
    <section class="evidence-audit" aria-label="临床证据审计桌">
      ${evidenceAuditState.error && !audit ? `<div class="evidence-audit__error" role="alert"><strong>无法载入审计</strong><span>${escapeHTML(evidenceAuditState.error)}</span></div>` : ""}
      ${route.auditID ? `
        ${evidenceAuditState.loading && !audit ? `<div class="evidence-audit__waiting" role="status"><span aria-hidden="true"></span><strong>正在载入</strong><p>读取固定审计报告与状态。</p></div>` : ""}
        ${renderEvidenceAuditStatus(audit)}
        ${evidenceAuditState.error && audit ? `<div class="evidence-audit__error" role="alert"><strong>请求未完成</strong><span>${escapeHTML(evidenceAuditState.error)}</span></div>` : ""}
        ${audit?.status === "completed" ? renderEvidenceAuditReport(audit) : ""}
        ${audit && ["queued", "running"].includes(audit.status) ? `<div class="evidence-audit__waiting"><span aria-hidden="true"></span><strong>${escapeHTML(evidenceAuditStatusLabel(audit.status))}</strong><p>只轮询当前审计；离开此链接或进入终态后自动停止。</p></div>` : ""}
      ` : `
        ${renderEvidenceAuditComposer(pkg)}
        ${renderEvidenceAuditHistory(pkg)}
      `}
    </section>
  `;
}

function renderEvidenceAuditContext(route, pkg, evaluation) {
  const packageName = pkg.display_name || pkg.name || pkg.title || pkg.package_id;
  const model = pkg.model_policy?.preferred_capability || pkg.model_policy?.model || "未指定";
  const sourceCount = Array.isArray(pkg.releases) ? pkg.releases.length : 0;
  const agentURL = buildAgentURL(pkg.package_id, pkg.version);
  return `
    <header class="evidence-audit__context" aria-label="审计包上下文">
      <div class="evidence-audit__context-title">
        <a href="${escapeAttribute(agentURL)}">← 返回 Agent</a>
        <div><span>临床证据审计</span><h1>${escapeHTML(packageName)}</h1></div>
      </div>
      <dl>
        <div><dt>版本</dt><dd>${escapeHTML(pkg.version || "—")}</dd></div>
        <div><dt>模型</dt><dd>${escapeHTML(model)}</dd></div>
        <div><dt>来源</dt><dd>${sourceCount}</dd></div>
        <div class="${evaluation.passed ? "is-pass" : "is-hold"}"><dt>评测</dt><dd>${evaluation.passed ? "已通过" : "待通过"}</dd></div>
      </dl>
    </header>
  `;
}

function renderEvidenceAuditTools(pkg, release, bookID, searchRows) {
  return `
    <details class="evidence-audit__tools">
      <summary>
        <span>包内工具</span>
        <small>阅读器与固定范围检索</small>
      </summary>
      <div class="evidence-audit__tools-body">
        ${renderBookAgentCapability("reader", `
          <section class="book-agent__capability book-agent__reader" data-capability="reader">
            <div class="book-agent__section-head"><div><span>01</span><h2>阅读器 Reader</h2></div><p>回到固定 source version 的阅读面。</p></div>
            ${bookID ? `<a class="book-agent__reader-link" href="${escapeAttribute(buildBookReaderURL(bookID))}"><span>打开本书</span><strong>${escapeHTML(release.book?.title || bookID)}</strong><small>版本化阅读入口 →</small></a>` : `<div class="book-agent__unavailable"><strong>功能已声明，但运行时尚未接通</strong><p>Release 尚未提供可解析的 book_id。</p></div>`}
          </section>
        `)}
        ${renderBookAgentCapability("search", `
          <section class="book-agent__capability book-agent__search" data-capability="search">
            <div class="book-agent__section-head"><div><span>02</span><h2>包内检索 Grounded Search</h2></div><p>结果保持 Claim、Chunk 与 Release 身份。</p></div>
            <form id="book-agent-search-form"><input name="query" value="${escapeAttribute(bookAgentState.query)}" placeholder="检索当前知识包" aria-label="检索当前知识包"><button class="button button-primary" type="submit">检索</button></form>
            <div class="book-agent__search-results">${searchRows || `<p class="web-muted">输入关键词以检索此包固定的知识范围。</p>`}</div>
          </section>
        `, Boolean(pkg.package_id && pkg.version))}
      </div>
    </details>
  `;
}

function renderGroundedConversation(pkg) {
  return renderBookAgentCapability("grounded_chat", `
    <section class="book-agent__capability book-agent__chat" data-capability="grounded_chat">
      <div class="book-agent__section-head"><div><span>03</span><h2>循证对话 Grounded Conversation</h2></div><p>回答必须经过 Package 的引用与拒答边界。</p></div>
      <form id="book-agent-chat-form"><textarea name="question" rows="4" placeholder="基于当前知识包提问" aria-label="基于当前知识包提问">${escapeHTML(bookAgentState.question)}</textarea><button class="button button-primary" type="submit">基于证据提问</button></form>
      ${bookAgentState.answer?.answer ? `<article class="book-agent__answer">${renderSimpleMarkdown(bookAgentState.answer.answer)}${renderBookAgentAnswerCitations(bookAgentState.answer)}</article>` : ""}
      ${bookAgentState.answer?.outcome === "abstained" ? `<article class="book-agent__answer"><strong>已拒答</strong><p>${escapeHTML(bookAgentState.answer.abstention_reason || "证据不足")}</p></article>` : ""}
    </section>
  `, Boolean(pkg.package_id && pkg.version));
}

function renderBookAgentPlatform(route = bookAgentState.route || { view: "package", packageID: "" }) {
  deactivateProofroomModal();
  if (!route.packageID || !bookAgentState.package) {
    renderShell(renderBookAgentPackageIndex(route), "agents");
    bindAgentCompilerEvents(route);
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
      <span>${escapeHTML(result.release_id || "release")} · ${escapeHTML(result.claim_id || "claim")}</span>
      <strong>${escapeHTML((result.citation_ids || []).join(" · ") || "No citation IDs")}</strong>
      <p>${escapeHTML(result.statement || "")}</p>
    </article>
  `).join("");
  const evaluationMetrics = Object.entries(evaluation.metrics || {}).map(([metric, score]) => `
    <div><span>${escapeHTML(metric)}</span><strong>${Math.round(Number(score || 0) * 100)}%</strong></div>
  `).join("");
  const runtimeStatus = bookAgentState.loading || bookAgentState.message;
  const isEvidenceAuditRoute = route.view === "agent" && pkg.schema_version === "agent-package.v2";

  renderShell(`
    <main class="book-agent book-agent--detail ${isEvidenceAuditRoute ? "book-agent--audit" : ""}">
      ${isEvidenceAuditRoute ? renderEvidenceAuditContext(route, pkg, evaluation) : `
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
      `}

      ${runtimeStatus ? `<p class="web-status">${escapeHTML(runtimeStatus)}</p>` : ""}

      ${isEvidenceAuditRoute ? "" : `<section class="book-agent__manifest">
        <div><span>Package hash</span><code>${escapeHTML(pkg.content_hash)}</code></div>
        <div><span>Model route</span><strong>${escapeHTML(pkg.model_policy?.preferred_capability || "—")}</strong></div>
        <div><span>Retrieval</span><strong>${escapeHTML(pkg.retrieval_policy?.strategy || "—")}</strong></div>
        <div><span>Escalation</span><strong>${escapeHTML(pkg.safety_policy?.escalation_target || "—")}</strong></div>
        ${evaluationMetrics ? `<div class="book-agent__metric-strip">${evaluationMetrics}</div>` : ""}
      </section>`}

      <section class="book-agent__capabilities" aria-label="Manifest capabilities">
        ${isEvidenceAuditRoute ? `
          ${renderEvidenceAuditWorkspace(route, pkg)}
          ${renderEvidenceAuditTools(pkg, release, bookID, searchRows)}
          ${renderGroundedConversation(pkg)}
          ${renderBookAgentCapability("evidence", renderBookAgentEvidence())}
        ` : `
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
        `, Boolean(pkg.package_id && pkg.version))}
          ${renderEvidenceAuditWorkspace(route, pkg)}
          ${renderGroundedConversation(pkg)}
          ${renderBookAgentCapability("evidence", renderBookAgentEvidence())}
          ${renderBookAgentCapability("quiz", "", false)}
          ${renderBookAgentCapability("action_plan", "", false)}
        `}
      </section>
    </main>
  `, "agents");
  bindBookAgentPlatformEvents(route);
  if (evidenceAuditState.proofroomPreview) {
    window.requestAnimationFrame?.(() => activateProofroomModal(route));
  }
}

function bindAgentCompilerEvents(route) {
  document.querySelectorAll("[data-agent-compiler-mode]").forEach((button) => {
    button.addEventListener("click", () => {
      const mode = String(button.dataset.agentCompilerMode || "");
      if (!["dual", "evidence", "study"].includes(mode) || mode === agentCompilerState.mode) {
        return;
      }
      agentCompilerState.mode = mode;
      resetAgentCompilerResult();
      if (mode === "study") {
        agentCompilerState.supportingReleaseIDs = [];
      }
      renderBookAgentPlatform(route);
    });
  });
  const form = document.querySelector("#agent-compiler-form");
  form?.querySelector('[name="primary_release_id"]')?.addEventListener("change", (event) => {
    agentCompilerState.primaryReleaseID = String(event.currentTarget.value || "");
    agentCompilerState.supportingReleaseIDs = agentCompilerState.supportingReleaseIDs.filter(
      (releaseID) => releaseID !== agentCompilerState.primaryReleaseID,
    );
    resetAgentCompilerResult();
    renderBookAgentPlatform(route);
  });
  form?.querySelector('[name="version"]')?.addEventListener("input", (event) => {
    agentCompilerState.version = String(event.currentTarget.value || "");
    resetAgentCompilerResult();
  });
  form?.querySelectorAll('[name="supporting_release_ids"]').forEach((input) => {
    input.addEventListener("change", () => {
      const data = new FormData(form);
      agentCompilerState.supportingReleaseIDs = data.getAll("supporting_release_ids").map(String);
      resetAgentCompilerResult();
    });
  });
  form?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    agentCompilerState.primaryReleaseID = String(data.get("primary_release_id") || "").trim();
    agentCompilerState.version = String(data.get("version") || "").trim();
    agentCompilerState.supportingReleaseIDs = agentCompilerState.mode === "study"
      ? []
      : data.getAll("supporting_release_ids").map((value) => String(value).trim()).filter(Boolean);
    await compileAgentPackages(route);
  });
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
  document.querySelector("#evidence-audit-create-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    evidenceAuditState.subject = String(data.get("subject") || "").trim();
    evidenceAuditState.scope = String(data.get("scope") || "").trim();
    evidenceAuditState.selectedClaims = data.getAll("selected_claims").map((value) => String(value));
    await createEvidenceAudit(route);
  });
  document.querySelectorAll('#evidence-audit-create-form input[name="selected_claims"]').forEach((input) => {
    input.addEventListener("change", (event) => {
      const formData = new FormData(event.currentTarget.form);
      const values = formData.getAll("selected_claims").map((value) => String(value));
      const maxClaims = Math.max(1, Number(bookAgentState.package?.evidence_policy?.max_claims || 1));
      evidenceAuditState.subject = String(formData.get("subject") || "").trim();
      evidenceAuditState.scope = String(formData.get("scope") || "").trim();
      evidenceAuditState.selectedClaims = values.slice(0, maxClaims);
      renderBookAgentPlatform(route);
    });
  });
  document.querySelector("[data-evidence-audit-retry]")?.addEventListener("click", () => retryEvidenceAudit(route));
  document.querySelector("[data-proofroom-preview]")?.addEventListener("click", () => loadProofroomPreview(route));
  document.querySelector("[data-proofroom-close]")?.addEventListener("click", () => closeProofroomPreview(route));
  document.querySelector("[data-proofroom-deliver]")?.addEventListener("click", () => deliverEvidenceAuditToProofroom(route));
}

function resetAgentCompilerResult() {
  agentCompilerRequestSequence += 1;
  agentCompilerState.result = null;
  agentCompilerState.loading = "";
  agentCompilerState.error = "";
  document.querySelector(".agent-compiler__result")?.remove();
  const status = document.querySelector("#agent-compiler-form [role=status]");
  if (status) {
    status.textContent = `${agentCompilerState.releases.length} 个最新 Release 可用于编译`;
  }
}

async function loadAgentCompilerReleases() {
  const releases = [];
  let after = "";
  while (releases.length < 500) {
    const query = new URLSearchParams({ latest: "true", limit: "200" });
    if (after) {
      query.set("after", after);
    }
    const payload = await apiFetch(`/api/knowledge/releases?${query.toString()}`);
    const page = Array.isArray(payload.releases) ? payload.releases : [];
    releases.push(...page);
    const next = String(payload.next_cursor || "");
    if (!page.length || page.length < 200 || !next || next === after) {
      break;
    }
    after = next;
  }
  return releases.slice(0, 500);
}

async function compileAgentPackages(route) {
  if (!agentCompilerState.primaryReleaseID || !agentCompilerState.version) {
    return;
  }
  const sequence = ++agentCompilerRequestSequence;
  agentCompilerState.loading = "正在构建 Release Assembly 与候选包";
  agentCompilerState.error = "";
  agentCompilerState.result = null;
  renderBookAgentPlatform(route);
  try {
    const payload = {
      schema_version: "agent-compilation-request.v1",
      mode: agentCompilerState.mode,
      primary_release_id: agentCompilerState.primaryReleaseID,
      version: agentCompilerState.version,
    };
    if (agentCompilerState.mode !== "study" && agentCompilerState.supportingReleaseIDs.length) {
      payload.supporting_release_ids = [...agentCompilerState.supportingReleaseIDs].sort();
    }
    const result = await apiFetch("/api/agent-packages/compile", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    if (sequence !== agentCompilerRequestSequence) {
      return;
    }
    agentCompilerState.result = result;
  } catch (error) {
    if (sequence !== agentCompilerRequestSequence) {
      return;
    }
    agentCompilerState.error = error instanceof Error ? error.message : String(error);
  } finally {
    if (sequence === agentCompilerRequestSequence) {
      agentCompilerState.loading = "";
      renderBookAgentPlatform(route);
    }
  }
}

function resetEvidenceAuditState(auditID = "") {
  cancelEvidenceAuditPoll();
  evidenceAuditLoadSequence += 1;
  proofroomOperationSequence += 1;
  evidenceAuditState.audit = null;
  evidenceAuditState.routeAuditID = auditID;
  evidenceAuditState.loading = "";
  evidenceAuditState.error = "";
  evidenceAuditState.proofroomPreview = null;
  evidenceAuditState.proofroomStatus = "";
  evidenceAuditState.proofroomError = "";
  evidenceAuditState.deliveryReceipt = null;
  evidenceAuditState.proofroomDeliveryKey = "";
  evidenceAuditState.retryIdempotencyKey = "";
}

function cancelEvidenceAuditPoll() {
  if (evidenceAuditPollTimer) {
    clearTimeout(evidenceAuditPollTimer);
    evidenceAuditPollTimer = null;
  }
}

function scheduleEvidenceAuditPoll(route) {
  cancelEvidenceAuditPoll();
  const auditID = String(route?.auditID || evidenceAuditState.audit?.audit_id || "");
  const status = evidenceAuditState.audit?.status;
  if (!auditID || !["queued", "running"].includes(status)) {
    return;
  }
  evidenceAuditPollTimer = window.setTimeout(async () => {
    if (evidenceAuditState.routeAuditID !== auditID || bookAgentState.route?.auditID !== auditID) {
      cancelEvidenceAuditPoll();
      return;
    }
    await loadEvidenceAudit({ ...route, auditID }, { silent: true });
  }, 1800);
}

async function loadEvidenceAuditWorkspace(route) {
  const sequence = ++evidenceAuditWorkspaceSequence;
  const pkg = bookAgentState.package || {};
  const packageID = String(pkg.package_id || "");
  const version = String(pkg.version || "");
  if (pkg.schema_version !== "agent-package.v2") {
    resetEvidenceAuditState("");
    evidenceAuditState.audits = [];
    return;
  }
  if (route.auditID) {
    resetEvidenceAuditState(route.auditID);
    await loadEvidenceAudit(route);
    return;
  }
  resetEvidenceAuditState("");
  evidenceAuditState.audits = [];
  const claims = evidenceAuditPrimaryClaims();
  const maxClaims = Math.max(1, Number(pkg.evidence_policy?.max_claims || 1));
  evidenceAuditState.selectedClaims = claims.slice(0, maxClaims).map((claim) => String(claim.id || "")).filter(Boolean);
  evidenceAuditState.subject = evidenceAuditState.subject || `${evidenceAuditPrimaryRelease()?.book?.title || pkg.package_id} 临床证据复核`;
  evidenceAuditState.scope = evidenceAuditState.scope || "核验主书关键临床结论，检查独立来源、证据时效、冲突与需要人工复核的知识缺口。";
  try {
    const params = new URLSearchParams({ version: pkg.version, limit: "10" });
    const payload = await apiFetch(`/api/agent-packages/${encodeURIComponent(pkg.package_id)}/audits?${params.toString()}`);
    if (
      sequence !== evidenceAuditWorkspaceSequence ||
      String(bookAgentState.package?.package_id || "") !== packageID ||
      String(bookAgentState.package?.version || "") !== version
    ) {
      return;
    }
    evidenceAuditState.audits = Array.isArray(payload.audits) ? payload.audits : [];
  } catch (error) {
    if (sequence !== evidenceAuditWorkspaceSequence) {
      return;
    }
    evidenceAuditState.error = evidenceAuditErrorDetails(error).message;
  }
}

async function loadEvidenceAudit(route, { silent = false } = {}) {
  const auditID = String(route?.auditID || "");
  if (!auditID || evidenceAuditState.routeAuditID !== auditID) {
    return;
  }
  const sequence = ++evidenceAuditLoadSequence;
  if (!silent) {
    evidenceAuditState.loading = "正在载入证据审计";
    evidenceAuditState.error = "";
    renderBookAgentPlatform(route);
  }
  try {
    const audit = await apiFetch(`/api/agent-audits/${encodeURIComponent(route.auditID)}`);
    const expectedPackageID = String(route.packageID || bookAgentState.package?.package_id || "");
    const expectedVersion = String(route.version || bookAgentState.package?.version || "");
    if (
      String(audit?.audit_id || "") !== auditID ||
      String(audit?.package?.package_id || "") !== expectedPackageID ||
      (expectedVersion && String(audit?.package?.version || "") !== expectedVersion)
    ) {
      throw new Error("审计报告身份与当前 Package/version 不匹配，已拒绝展示。");
    }
    if (sequence !== evidenceAuditLoadSequence || evidenceAuditState.routeAuditID !== auditID) {
      return;
    }
    if (audit.trace_id) {
      try {
        const trace = await apiFetch(`/api/agent-traces/${encodeURIComponent(audit.trace_id)}`);
        if (
          String(trace?.trace_id || "") !== String(audit.trace_id) ||
          String(trace?.package?.package_id || "") !== expectedPackageID ||
          (trace?.evidence_audit?.audit_id && String(trace.evidence_audit.audit_id) !== auditID)
        ) {
          throw new Error("Trace 身份与当前审计不匹配。");
        }
        audit.trace = trace;
      } catch (traceError) {
        audit.trace_error = traceError instanceof Error ? traceError.message : String(traceError);
      }
    }
    if (sequence !== evidenceAuditLoadSequence || evidenceAuditState.routeAuditID !== auditID) {
      return;
    }
    evidenceAuditState.audit = audit;
    evidenceAuditState.error = "";
  } catch (error) {
    if (sequence !== evidenceAuditLoadSequence || evidenceAuditState.routeAuditID !== auditID) {
      return;
    }
    const details = evidenceAuditErrorDetails(error);
    evidenceAuditState.error = `${details.code}: ${details.message}`;
  } finally {
    if (sequence === evidenceAuditLoadSequence && evidenceAuditState.routeAuditID === auditID) {
      evidenceAuditState.loading = "";
      renderBookAgentPlatform(route);
      scheduleEvidenceAuditPoll(route);
    }
  }
}

async function createEvidenceAudit(route) {
  const pkg = bookAgentState.package || {};
  const maxClaims = Math.max(1, Number(pkg.evidence_policy?.max_claims || 1));
  if (
    pkg.schema_version !== "agent-package.v2" ||
    !evidenceAuditState.subject ||
    !evidenceAuditState.scope ||
    !evidenceAuditState.selectedClaims.length ||
    evidenceAuditState.selectedClaims.length > maxClaims
  ) {
    evidenceAuditState.error = "请填写主题、范围，并在策略上限内选择主书 claims。";
    renderBookAgentPlatform(route);
    return;
  }
  evidenceAuditState.loading = "正在创建审计";
  evidenceAuditState.error = "";
  renderBookAgentPlatform(route);
  try {
    const requestFingerprint = JSON.stringify({
      package_id: pkg.package_id,
      version: pkg.version,
      subject: evidenceAuditState.subject,
      scope: evidenceAuditState.scope,
      selected_claims: evidenceAuditState.selectedClaims,
    });
    if (
      evidenceAuditState.createRequestFingerprint !== requestFingerprint ||
      !evidenceAuditState.createIdempotencyKey
    ) {
      evidenceAuditState.createRequestFingerprint = requestFingerprint;
      evidenceAuditState.createIdempotencyKey = evidenceAuditIdempotencyKey("create", pkg.package_id);
    }
    const params = new URLSearchParams({ version: pkg.version });
    const payload = await apiFetch(`/api/agent-packages/${encodeURIComponent(pkg.package_id)}/audits?${params.toString()}`, {
      method: "POST",
      body: JSON.stringify({
        subject: evidenceAuditState.subject,
        scope: evidenceAuditState.scope,
        selected_claims: evidenceAuditState.selectedClaims,
        idempotency_key: evidenceAuditState.createIdempotencyKey,
      }),
    });
    const audit = payload.audit;
    const nextRoute = { ...route, view: "agent", auditID: audit.audit_id, version: pkg.version };
    const stableURL = buildEvidenceAuditURL(pkg.package_id, audit.audit_id, pkg.version);
    window.history.replaceState({}, "", stableURL);
    bookAgentState.route = nextRoute;
    resetEvidenceAuditState(audit.audit_id);
    evidenceAuditState.audit = audit;
    renderBookAgentPlatform(nextRoute);
    scheduleEvidenceAuditPoll(nextRoute);
  } catch (error) {
    const details = evidenceAuditErrorDetails(error);
    evidenceAuditState.error = `${details.code}: ${details.message}`;
    evidenceAuditState.loading = "";
    renderBookAgentPlatform(route);
  }
}

async function retryEvidenceAudit(route) {
  const audit = evidenceAuditState.audit;
  if (!canRetryEvidenceAudit(audit)) {
    return;
  }
  evidenceAuditState.loading = "正在提交重试";
  evidenceAuditState.error = "";
  renderBookAgentPlatform(route);
  try {
    evidenceAuditState.retryIdempotencyKey =
      evidenceAuditState.retryIdempotencyKey || `retry:${audit.audit_id}:manual-v1`;
    const payload = await apiFetch(`/api/agent-audits/${encodeURIComponent(audit.audit_id)}/retry`, {
      method: "POST",
      headers: {
        "Idempotency-Key": evidenceAuditState.retryIdempotencyKey,
      },
    });
    const retry = payload.audit;
    const nextRoute = { ...route, auditID: retry.audit_id };
    window.history.replaceState({}, "", buildEvidenceAuditURL(retry.package?.package_id || route.packageID, retry.audit_id, retry.package?.version || route.version));
    bookAgentState.route = nextRoute;
    resetEvidenceAuditState(retry.audit_id);
    evidenceAuditState.audit = retry;
    renderBookAgentPlatform(nextRoute);
    scheduleEvidenceAuditPoll(nextRoute);
  } catch (error) {
    const details = evidenceAuditErrorDetails(error);
    evidenceAuditState.error = `${details.code}: ${details.message}`;
    evidenceAuditState.loading = "";
    renderBookAgentPlatform(route);
  }
}

async function loadProofroomPreview(route) {
  const audit = evidenceAuditState.audit;
  if (audit?.status !== "completed") {
    return;
  }
  evidenceAuditState.proofroomStatus = "loading";
  evidenceAuditState.proofroomError = "";
  proofroomPreviousFocus = document.activeElement;
  proofroomReturnFocusSelector = "[data-proofroom-preview]";
  const operation = ++proofroomOperationSequence;
  const auditID = audit.audit_id;
  renderBookAgentPlatform(route);
  try {
    const preview = await apiFetch(`/api/agent-audits/${encodeURIComponent(auditID)}/proofroom`);
    if (
      operation !== proofroomOperationSequence ||
      evidenceAuditState.audit?.audit_id !== auditID ||
      bookAgentState.route?.auditID !== auditID
    ) {
      return;
    }
    evidenceAuditState.proofroomPreview = preview;
    evidenceAuditState.proofroomDeliveryKey = `proofroom:${auditID}:${preview?.payload_hash || audit.output_hash || "projection"}`;
    evidenceAuditState.proofroomStatus = "previewed";
  } catch (error) {
    if (operation !== proofroomOperationSequence || evidenceAuditState.audit?.audit_id !== auditID) {
      return;
    }
    const details = evidenceAuditErrorDetails(error);
    evidenceAuditState.proofroomStatus = "error";
    evidenceAuditState.proofroomError = `${details.code}: ${details.message}`;
  } finally {
    if (operation === proofroomOperationSequence && evidenceAuditState.audit?.audit_id === auditID) {
      renderBookAgentPlatform(route);
    }
  }
}

async function deliverEvidenceAuditToProofroom(route) {
  const audit = evidenceAuditState.audit;
  if (audit?.status !== "completed" || !evidenceAuditState.proofroomPreview) {
    return;
  }
  if (!window.confirm("确认发送到 Proofroom？该操作会创建可追溯投递回执，审计完成本身不会自动发送。")) {
    return;
  }
  evidenceAuditState.proofroomStatus = "delivering";
  evidenceAuditState.proofroomError = "";
  const operation = ++proofroomOperationSequence;
  const auditID = audit.audit_id;
  renderBookAgentPlatform(route);
  try {
    const payload = await apiFetch(`/api/agent-audits/${encodeURIComponent(auditID)}/proofroom`, {
      method: "POST",
      headers: {
        "Idempotency-Key": evidenceAuditState.proofroomDeliveryKey ||
          `proofroom:${auditID}:${evidenceAuditState.proofroomPreview.payload_hash || audit.output_hash || "projection"}`,
      },
    });
    if (
      operation !== proofroomOperationSequence ||
      evidenceAuditState.audit?.audit_id !== auditID ||
      bookAgentState.route?.auditID !== auditID
    ) {
      return;
    }
    evidenceAuditState.deliveryReceipt = payload.receipt || null;
    evidenceAuditState.proofroomStatus = payload.receipt?.status || "delivered";
  } catch (error) {
    if (operation !== proofroomOperationSequence || evidenceAuditState.audit?.audit_id !== auditID) {
      return;
    }
    const details = evidenceAuditErrorDetails(error);
    evidenceAuditState.proofroomError = `${details.code}: ${details.message}`;
    if (details.code === "proofroom_outcome_unknown") {
      evidenceAuditState.proofroomStatus = "outcome_unknown";
    } else if (details.code === "proofroom_remote_rejected") {
      evidenceAuditState.proofroomStatus = "rejected";
    } else {
      evidenceAuditState.proofroomStatus = "error";
    }
  } finally {
    if (operation === proofroomOperationSequence && evidenceAuditState.audit?.audit_id === auditID) {
      renderBookAgentPlatform(route);
    }
  }
}

async function loadBookAgentPlatform(route) {
  cancelEvidenceAuditPoll();
  const sequence = ++bookAgentLoadSequence;
  bookAgentState.route = route;
  bookAgentState.loading = "Loading Agent Packages";
  bookAgentState.message = "";
  renderBookAgentPlatform(route);
  try {
    if (!route.packageID) {
      resetAgentCompilerResult();
      agentCompilerState.loading = "正在加载最新 Release";
      renderBookAgentPlatform(route);
      const [packagesResult, releasesResult] = await Promise.allSettled([
        apiFetch("/api/agent-packages?limit=100"),
        loadAgentCompilerReleases(),
      ]);
      if (sequence !== bookAgentLoadSequence) {
        return;
      }
      if (releasesResult.status === "fulfilled") {
        const releases = releasesResult.value;
        agentCompilerState.releases = releases;
        agentCompilerState.loading = "";
        agentCompilerState.error = "";
        if (!releases.some((release) => release.release_id === agentCompilerState.primaryReleaseID)) {
          agentCompilerState.primaryReleaseID = releases[0]?.release_id || "";
        }
        const availableReleaseIDs = new Set(releases.map((release) => release.release_id));
        agentCompilerState.supportingReleaseIDs = agentCompilerState.supportingReleaseIDs.filter(
          (releaseID) => availableReleaseIDs.has(releaseID) &&
            releaseID !== agentCompilerState.primaryReleaseID,
        );
      } else {
        agentCompilerState.releases = [];
        agentCompilerState.primaryReleaseID = "";
        agentCompilerState.supportingReleaseIDs = [];
        agentCompilerState.loading = "";
        agentCompilerState.error = `Release 列表加载失败：${
          releasesResult.reason instanceof Error
            ? releasesResult.reason.message
            : String(releasesResult.reason)
        }`;
      }
      if (packagesResult.status === "rejected") {
        throw packagesResult.reason;
      }
      const payload = packagesResult.value;
      bookAgentState.packages = Array.isArray(payload.packages) ? payload.packages : [];
      bookAgentState.message = `${bookAgentState.packages.length} published packages`;
      return;
    }
    const query = route.version ? `?version=${encodeURIComponent(route.version)}` : "";
    const pkg = await apiFetch(`/api/agent-packages/${encodeURIComponent(route.packageID)}${query}`);
    if (sequence !== bookAgentLoadSequence) {
      return;
    }
    bookAgentState.package = pkg;
    const releases = await Promise.all((pkg.releases || []).map((reference) => (
      apiFetch(`/api/knowledge/releases/${encodeURIComponent(reference.release_id)}`)
    )));
    if (sequence !== bookAgentLoadSequence) {
      return;
    }
    bookAgentState.releases = releases;
    if (route.view === "agent") {
      await loadEvidenceAuditWorkspace(route);
      if (sequence !== bookAgentLoadSequence) {
        return;
      }
    }
    bookAgentState.message = "Package, releases, and evaluation loaded";
  } catch (error) {
    bookAgentState.message = error instanceof Error ? error.message : String(error);
  } finally {
    if (sequence === bookAgentLoadSequence) {
      bookAgentState.loading = "";
      renderBookAgentPlatform(route);
    }
  }
}

async function searchBookAgentPackage(route) {
  const pkg = bookAgentState.package || {};
  if (!bookAgentState.query || !pkg.package_id || !pkg.version) {
    bookAgentState.results = [];
    renderBookAgentPlatform(route);
    return;
  }
  bookAgentState.loading = "Searching pinned evidence";
  renderBookAgentPlatform(route);
  try {
    const maxContextChunks = Math.max(1, Number(pkg.retrieval_policy?.max_context_chunks || 20));
    const query = new URLSearchParams({ version: pkg.version, q: bookAgentState.query, limit: String(Math.min(20, maxContextChunks)) });
    const payload = await apiFetch(`/api/agent-packages/${encodeURIComponent(pkg.package_id)}/search?${query.toString()}`);
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
  const pkg = bookAgentState.package || {};
  if (!bookAgentState.question || !pkg.package_id || !pkg.version) {
    return;
  }
  bookAgentState.loading = "Reasoning over pinned evidence";
  renderBookAgentPlatform(route);
  try {
    bookAgentState.answer = await apiFetch(`/api/agent-packages/${encodeURIComponent(pkg.package_id)}/chat?version=${encodeURIComponent(pkg.version)}`, {
      method: "POST",
      body: JSON.stringify({
        question: bookAgentState.question,
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
    const imageMatch = block.match(/^!\[([^\]\n]*)\]\(([^\s)]+)\)$/i);
    if (imageMatch) {
      const alt = imageMatch[1] || "";
      const src = imageMatch[2] || "";
      const privateSourceAsset = /^\/api\/source-assets\/[a-f0-9]{64}$/.test(src);
      const publicImage = /^https?:\/\/[^\s]+$/i.test(src);
      if (!privateSourceAsset && !publicImage) {
        return `<p>${renderInlineMarkdown(block)}</p>`;
      }
      return `
        <figure class="dedao-course-article__image${privateSourceAsset ? " is-loading" : ""}">
          <img ${privateSourceAsset ? `data-private-src="${escapeAttribute(src)}"` : `src="${escapeAttribute(src)}"`} alt="${escapeAttribute(alt)}" loading="lazy">
          ${alt && alt !== src ? `<figcaption>${escapeHTML(alt)}</figcaption>` : ""}
          ${privateSourceAsset ? '<span class="reader-page__image-status">图片加载中</span>' : ""}
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

function releaseReaderAssetObjectURLs() {
  readerAssetObjectURLs.forEach((url) => URL.revokeObjectURL(url));
  readerAssetObjectURLs = [];
}

async function loadPrivateSourceAssets(container = app) {
  const images = Array.from(container.querySelectorAll("img[data-private-src]"));
  if (!images.length) {
    return;
  }
  await Promise.allSettled(images.map(async (image) => {
    const source = image.dataset.privateSrc || "";
    const figure = image.closest("figure");
    const status = figure?.querySelector(".reader-page__image-status");
    let response = null;
    let objectURL = "";
    let committed = false;
    try {
      const headers = new Headers();
      headers.set("Accept", "image/*");
      response = await browserSessionFetch(source, {
        headers,
        credentials: "same-origin",
      });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const blob = await response.blob();
      if (!String(blob.type || "").startsWith("image/")) {
        throw new Error("invalid image response");
      }
      assertBrowserSessionResponseCurrent(response);
      objectURL = URL.createObjectURL(blob);
      assertBrowserSessionResponseCurrent(response);
      readerAssetObjectURLs.push(objectURL);
      image.src = objectURL;
      image.removeAttribute("data-private-src");
      figure?.classList.remove("is-loading");
      status?.remove();
      committed = true;
    } catch (error) {
      figure?.classList.remove("is-loading");
      figure?.classList.add("is-error");
      if (status) {
        status.textContent = `图片加载失败：${error instanceof Error ? error.message : String(error)}`;
      }
    } finally {
      if (objectURL && !committed) {
        URL.revokeObjectURL(objectURL);
      }
      releaseBrowserSessionResponse(response);
    }
  }));
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
  knowledgeAgentLoadSequence++;
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
  knowledgeState.agentPackages = [];
  knowledgeState.agentPackagesLoading = "";
  knowledgeState.agentPackagesError = "";
}

async function loadKnowledgeAgentPackageRecords() {
  const packages = [];
  let after = "";
  for (let page = 0; page < 20; page++) {
    const path = after
      ? `/api/agent-packages?limit=200&after=${encodeURIComponent(after)}`
      : "/api/agent-packages?limit=200";
    const payload = await apiFetch(path);
    const pagePackages = Array.isArray(payload.packages) ? payload.packages : [];
    packages.push(...pagePackages);
    if (pagePackages.length < 200 || !payload.next_cursor || payload.next_cursor === after) {
      break;
    }
    after = payload.next_cursor;
  }
  return packages;
}

async function loadKnowledgeAgentPackageDetails(records) {
  const publishedRecords = (records || []).filter((record) => (
    record.lifecycle_state === "published"
    && record.package_id
    && record.version
  ));
  const packages = [];
  const errors = [];
  const concurrency = 8;
  for (let offset = 0; offset < publishedRecords.length; offset += concurrency) {
    const batch = publishedRecords.slice(offset, offset + concurrency);
    const results = await Promise.allSettled(batch.map((record) => (
      apiFetch(`/api/agent-packages/${encodeURIComponent(record.package_id)}?version=${encodeURIComponent(record.version)}`)
    )));
    results.forEach((result, index) => {
      if (result.status === "fulfilled") {
        packages.push(result.value);
        return;
      }
      const record = batch[index];
      const reason = result.reason instanceof Error ? result.reason.message : String(result.reason);
      errors.push(`${record.package_id} ${record.version}: ${reason}`);
    });
  }
  return { packages, errors };
}

async function loadKnowledgeAgentPackages(bookID, { silent = false, renderResult = true } = {}) {
  const sequence = ++knowledgeAgentLoadSequence;
  if (!silent) {
    knowledgeState.agentPackagesLoading = "加载 Agent 供给状态";
    knowledgeState.agentPackagesError = "";
    if (renderResult) {
      renderBookKnowledge();
    }
  }
  try {
    const records = await loadKnowledgeAgentPackageRecords();
    const { packages, errors } = await loadKnowledgeAgentPackageDetails(records);
    if (sequence !== knowledgeAgentLoadSequence || knowledgeState.selectedBook?.book_id !== bookID) {
      return;
    }
    knowledgeState.agentPackages = packages;
    knowledgeState.agentPackagesError = errors.length
      ? `${errors.length} 个 Agent Package 详情加载失败：${errors[0]}`
      : "";
  } catch (error) {
    if (sequence === knowledgeAgentLoadSequence) {
      knowledgeState.agentPackages = [];
      knowledgeState.agentPackagesError = error instanceof Error ? error.message : String(error);
    }
  } finally {
    if (sequence === knowledgeAgentLoadSequence) {
      knowledgeState.agentPackagesLoading = "";
      if (renderResult) {
        renderBookKnowledge();
      }
    }
  }
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
  if (!isKnowledgePackageDetailRoute() || !["queued", "running"].includes(task?.status)) {
    return;
  }
  knowledgeReviewPollTimer = setTimeout(() => {
    knowledgeReviewPollTimer = null;
    if (!isKnowledgePackageDetailRoute()) {
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
  window.history?.replaceState?.({}, "", `${window.location.pathname}${query ? `?${query}` : ""}${window.location.hash || ""}`);
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
  releaseReaderAssetObjectURLs();
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
  const chunkItems = chunks.map((chunk) => (
    `<section class="reader-page__chunk">${renderCourseMarkdown(chunk.text || chunk.content || "")}</section>`
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
  void loadPrivateSourceAssets(app);
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
        <div>${sourceAgentReturnLink()}${status}</div>
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

function sourceAgentManagementStatus(agent, commands = []) {
  const activeUpgrade = commands.some((command) => command.type === "upgrade" && !["succeeded", "failed", "canceled", "expired", "rolled_back"].includes(command.state));
  if (activeUpgrade || agent.current_command_id) {
    return "upgrading";
  }
  if (agent.desired_state === "paused") {
    return "paused";
  }
  if (!sourceAgentIsOnline(agent)) {
    return "offline";
  }
  const health = Object.values(agent.capability_health || {});
  if (health.some((item) => !item?.healthy || item?.code || item?.requires_action)) {
    return "attention";
  }
  return "online";
}

function sourceAgentManagementStatusLabel(status) {
  return ({
    online: "在线",
    attention: "需处理",
    offline: "离线",
    paused: "已暂停",
    upgrading: "升级中",
  })[status] || "未知";
}

function sourceAgentManagementGroups() {
  const groups = { online: [], attention: [], offline: [], paused: [], upgrading: [] };
  for (const agent of sourceAgentManagementState.agents) {
    const commands = sourceAgentManagementState.commandsByAgent[agent.agent_id] || [];
    groups[sourceAgentManagementStatus(agent, commands)].push(agent);
  }
  return groups;
}

function renderSourceAgentStatusSummary() {
  const groups = sourceAgentManagementGroups();
  return `
    <section class="source-agents__summary" aria-label="Agent 状态汇总">
      ${["online", "attention", "offline", "paused", "upgrading"].map((status) => `
        <div class="source-agents__summary-item is-${status}">
          <strong>${groups[status].length}</strong>
          <span>${sourceAgentManagementStatusLabel(status)}</span>
        </div>
      `).join("")}
    </section>
  `;
}

function sourceAgentCompatibleArtifacts(agent) {
  return sourceAgentManagementState.artifacts.filter((artifact) => (
    artifact.allowed_for_rollout &&
    artifact.worker_type === agent.worker_type &&
    artifact.platform === agent.platform &&
    artifact.architecture === agent.architecture &&
    artifact.version !== agent.version
  ));
}

function sourceAgentWorkspace(agent) {
  return agent.worker_type === "wcplus-worker"
    ? { href: "/wcplus-source", label: "WC Plus 工作台" }
    : { href: "/wechat-source", label: "微信工作台" };
}

function renderSourceAgentManagementCard(agent) {
  const commands = sourceAgentManagementState.commandsByAgent[agent.agent_id] || [];
  const latestCommand = commands[0] || null;
  const status = sourceAgentManagementStatus(agent, commands);
  const healthEntries = Object.entries(agent.capability_health || {});
  const artifacts = sourceAgentCompatibleArtifacts(agent);
  const workspace = sourceAgentWorkspace(agent);
  const pending = sourceAgentManagementState.pendingAgentID === agent.agent_id;
  return `
    <article class="source-agent-card is-${status}">
      <header class="source-agent-card__header">
        <div>
          <a class="source-agent-card__title" href="${escapeAttribute(`${ROUTES.sourceAgents}/${encodeURIComponent(agent.agent_id)}`)}">${escapeHTML(agent.agent_id)}</a>
          <span>${escapeHTML(agent.worker_type || "legacy")}</span>
        </div>
        <span class="source-agent-card__status">${sourceAgentManagementStatusLabel(status)}</span>
      </header>
      <dl class="source-agent-card__facts">
        <div><dt>平台 / 架构</dt><dd>${escapeHTML(agent.platform || "-")} / ${escapeHTML(agent.architecture || "-")}</dd></div>
        <div><dt>版本 / 协议</dt><dd>${escapeHTML(agent.version || "-")} / ${escapeHTML(agent.protocol_version || "-")}</dd></div>
        <div><dt>最后心跳</dt><dd>${escapeHTML(formatSourceControlTime(agent.last_heartbeat_at))}</dd></div>
        <div><dt>最后成功</dt><dd>${escapeHTML(formatSourceControlTime(agent.last_success_at))}</dd></div>
        <div><dt>当前运行</dt><dd>${escapeHTML(agent.current_run_id || "-")}</dd></div>
        <div><dt>当前命令</dt><dd>${escapeHTML(agent.current_command_id || latestCommand?.state || "-")}</dd></div>
        <div><dt>Outbox / Dead letter</dt><dd>${Number(agent.outbox_pending || 0)} / ${Number(agent.dead_letter_count || 0)}</dd></div>
      </dl>
      <section class="source-agent-card__health" aria-label="能力健康">
        <h3>能力健康</h3>
        ${healthEntries.length ? healthEntries.map(([name, health]) => `
          <div>
            <strong>${escapeHTML(name)}</strong>
            <span>${health?.healthy ? "可用" : "不可用"}${health?.code ? ` · ${escapeHTML(health.code)}` : ""}${health?.requires_action ? ` · ${escapeHTML(health.requires_action)}` : ""}</span>
          </div>
        `).join("") : '<p class="web-muted">无能力上报</p>'}
      </section>
      ${agent.last_error ? `<p class="source-agent-card__error">${escapeHTML(agent.last_error)}</p>` : ""}
      <div class="source-agent-card__upgrade">
        <label>
          <span>选择已批准版本</span>
          <select data-source-agent-artifact="${escapeAttribute(agent.agent_id)}" ${pending || artifacts.length === 0 ? "disabled" : ""}>
            <option value="">${artifacts.length ? "选择版本" : "暂无兼容版本"}</option>
            ${artifacts.map((artifact) => `<option value="${escapeAttribute(artifact.id)}">${escapeHTML(artifact.version)} · ${escapeHTML(artifact.channel)}</option>`).join("")}
          </select>
        </label>
        <button class="button button-ghost" type="button" data-source-agent-upgrade="${escapeAttribute(agent.agent_id)}" ${pending || artifacts.length === 0 ? "disabled" : ""}>升级</button>
      </div>
      <div class="source-agent-card__actions">
        ${agent.desired_state === "paused"
          ? `<button class="button button-ghost" type="button" data-source-agent-resume="${escapeAttribute(agent.agent_id)}" ${pending ? "disabled" : ""}>恢复</button>`
          : `<button class="button button-ghost" type="button" data-source-agent-pause="${escapeAttribute(agent.agent_id)}" ${pending ? "disabled" : ""}>暂停</button>`}
        <button class="button button-ghost" type="button" data-source-agent-diagnose="${escapeAttribute(agent.agent_id)}" ${pending ? "disabled" : ""}>诊断</button>
        <a class="button button-ghost" href="${workspace.href}?agent_id=${encodeURIComponent(agent.agent_id)}">${workspace.label}</a>
      </div>
    </article>
  `;
}

function renderSourceAgentOverview() {
  const groups = sourceAgentManagementGroups();
  const order = ["attention", "upgrading", "offline", "paused", "online"];
  const status = sourceAgentManagementState.loading
    ? `<div class="web-status">${escapeHTML(sourceAgentManagementState.loading)}</div>`
    : (sourceAgentManagementState.message ? `<div class="web-status">${escapeHTML(sourceAgentManagementState.message)}</div>` : "");
  renderShell(`
    <main class="source-agents">
      <header class="source-agents__header">
        <div><p class="web-kicker">Source Workers</p><h1>Agent 管理</h1><p class="web-muted">统一控制面，Worker 独立运行；升级只允许选择已批准产物。</p></div>
        <div>${status}<button id="source-agent-management-refresh" class="button button-ghost" type="button">刷新</button></div>
      </header>
      ${renderSourceAgentStatusSummary()}
      <div class="source-agents__groups">
        ${order.map((group) => groups[group].length ? `
          <section class="source-agents__group is-${group}">
            <h2>${sourceAgentManagementStatusLabel(group)} <span>${groups[group].length}</span></h2>
            <div class="source-agents__list">${groups[group].map(renderSourceAgentManagementCard).join("")}</div>
          </section>
        ` : "").join("") || '<p class="web-muted">尚未收到 Agent 心跳。</p>'}
      </div>
    </main>
  `, "source-agents");
  bindSourceAgentManagementEvents();
}

async function loadSourceAgentManagement({ silent = false } = {}) {
  const sequence = ++sourceAgentManagementSequence;
  if (!silent) {
    sourceAgentManagementState.loading = "正在加载 Agent 状态";
    sourceAgentManagementState.message = "";
    renderSourceAgentOverview();
  }
  try {
    const agentPayload = await apiFetch("/api/source-agents");
    if (sequence !== sourceAgentManagementSequence) return;
    const agents = Array.isArray(agentPayload.agents) ? agentPayload.agents : [];
    const [artifactResult, ...commandResults] = await Promise.allSettled([
      apiFetch("/api/source-agent-artifacts?limit=100"),
      ...agents.map((agent) => apiFetch(`/api/source-agents/${encodeURIComponent(agent.agent_id)}/commands?limit=10`)),
    ]);
    if (sequence !== sourceAgentManagementSequence) return;
    sourceAgentManagementState.agents = agents;
    sourceAgentManagementState.artifacts = artifactResult.status === "fulfilled" && Array.isArray(artifactResult.value.artifacts) ? artifactResult.value.artifacts : [];
    sourceAgentManagementState.commandsByAgent = Object.fromEntries(agents.map((agent, index) => {
      const result = commandResults[index];
      return [agent.agent_id, result?.status === "fulfilled" && Array.isArray(result.value.commands) ? result.value.commands : []];
    }));
    sourceAgentManagementState.message = `${agents.length} 个 Agent · ${sourceAgentManagementState.artifacts.length} 个可见产物`;
  } catch (error) {
    if (sequence === sourceAgentManagementSequence) {
      sourceAgentManagementState.message = error instanceof Error ? error.message : String(error);
    }
  } finally {
    if (sequence === sourceAgentManagementSequence) {
      sourceAgentManagementState.loading = "";
      renderSourceAgentOverview();
      scheduleSourceAgentManagementPoll();
    }
  }
}

function scheduleSourceAgentManagementPoll() {
  if (sourceAgentManagementPollTimer) {
    clearTimeout(sourceAgentManagementPollTimer);
    sourceAgentManagementPollTimer = null;
  }
  if (!getRoutePathname().startsWith(ROUTES.sourceAgents)) return;
  const hasActiveCommand = Object.values(sourceAgentManagementState.commandsByAgent).flat().some((command) => !["succeeded", "failed", "canceled", "expired", "rolled_back"].includes(command.state));
  if (!hasActiveCommand && !sourceAgentManagementState.pendingAgentID) return;
  sourceAgentManagementPollTimer = setTimeout(() => {
    sourceAgentManagementPollTimer = null;
    loadSourceAgentManagement({ silent: true });
  }, 5000);
}

function sourceAgentCommandEnvelope(type, payload) {
  const nonce = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return {
    type,
    idempotency_key: `web-${type}-${nonce}`,
    ...(payload ? { payload } : {}),
    expires_at: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
  };
}

async function runSourceAgentManagementAction(agentID, operation) {
  sourceAgentManagementState.pendingAgentID = agentID;
  sourceAgentManagementState.message = "正在提交操作";
  renderSourceAgentOverview();
  try {
    await operation();
    sourceAgentManagementState.message = "操作已提交，正在刷新权威状态";
  } catch (error) {
    sourceAgentManagementState.message = error instanceof Error ? error.message : String(error);
  } finally {
    sourceAgentManagementState.pendingAgentID = "";
    await loadSourceAgentManagement();
  }
}

function setSourceAgentDesiredState(agentID, desiredState) {
  return runSourceAgentManagementAction(agentID, () => apiFetch(`/api/source-agents/${encodeURIComponent(agentID)}/desired-state`, {
    method: "POST",
    body: JSON.stringify({ desired_state: desiredState }),
  }));
}

function createSourceAgentDiagnostic(agentID) {
  return runSourceAgentManagementAction(agentID, () => apiFetch(`/api/source-agents/${encodeURIComponent(agentID)}/commands`, {
    method: "POST",
    body: JSON.stringify(sourceAgentCommandEnvelope("diagnose")),
  }));
}

function confirmSourceAgentUpgrade(agent, artifact) {
  return window.confirm(`确认升级 ${agent.agent_id}\n当前版本：${agent.version || "-"}\n目标版本：${artifact.version}`);
}

function createSourceAgentUpgrade(agentID, artifactID) {
  const agent = sourceAgentManagementState.agents.find((item) => item.agent_id === agentID);
  const artifact = sourceAgentManagementState.artifacts.find((item) => item.id === artifactID);
  if (!agent || !artifact || !confirmSourceAgentUpgrade(agent, artifact)) return Promise.resolve();
  return runSourceAgentManagementAction(agentID, () => apiFetch(`/api/source-agents/${encodeURIComponent(agentID)}/commands`, {
    method: "POST",
    body: JSON.stringify(sourceAgentCommandEnvelope("upgrade", {
      artifact_id: artifact.id,
      expected_current_version: agent.version,
    })),
  }));
}

function bindSourceAgentManagementEvents() {
  document.querySelector("#source-agent-management-refresh")?.addEventListener("click", () => loadSourceAgentManagement());
  for (const button of document.querySelectorAll("[data-source-agent-pause]")) {
    button.addEventListener("click", () => setSourceAgentDesiredState(button.getAttribute("data-source-agent-pause"), "paused"));
  }
  for (const button of document.querySelectorAll("[data-source-agent-resume]")) {
    button.addEventListener("click", () => setSourceAgentDesiredState(button.getAttribute("data-source-agent-resume"), "active"));
  }
  for (const button of document.querySelectorAll("[data-source-agent-diagnose]")) {
    button.addEventListener("click", () => createSourceAgentDiagnostic(button.getAttribute("data-source-agent-diagnose")));
  }
  for (const button of document.querySelectorAll("[data-source-agent-upgrade]")) {
    button.addEventListener("click", () => {
      const agentID = button.getAttribute("data-source-agent-upgrade");
      const select = document.querySelector(`[data-source-agent-artifact="${CSS.escape(agentID)}"]`);
      if (select?.value) createSourceAgentUpgrade(agentID, select.value);
    });
  }
}

function getSourceAgentDetailID(pathname = getRoutePathname()) {
  if (!pathname.startsWith(`${ROUTES.sourceAgents}/`)) return null;
  const raw = pathname.slice(ROUTES.sourceAgents.length + 1);
  if (!raw || raw.includes("/")) return "";
  try {
    const agentID = decodeURIComponent(raw);
    return /^[A-Za-z0-9._-]{1,128}$/.test(agentID) ? agentID : "";
  } catch {
    return "";
  }
}

function sourceAgentReturnLink() {
  const agentID = String(new URLSearchParams(window.location.search).get("agent_id") || "").trim();
  if (!/^[A-Za-z0-9._-]{1,128}$/.test(agentID)) return "";
  return `<a class="button button-ghost" href="${escapeAttribute(`${ROUTES.sourceAgents}/${encodeURIComponent(agentID)}`)}">返回 Agent 详情</a>`;
}

function sourceAgentRedactedDiagnostics(agent) {
  if (!agent) return [];
  const diagnostics = Object.entries(agent.capability_health || {}).map(([capability, health]) => ({
    capability,
    healthy: Boolean(health?.healthy),
    code: String(health?.code || ""),
    requiresAction: String(health?.requires_action || ""),
  }));
  diagnostics.push({ capability: "outbox", healthy: Number(agent.dead_letter_count || 0) === 0, code: Number(agent.dead_letter_count || 0) ? "dead_letters_present" : "", requiresAction: "" });
  return diagnostics;
}

function renderSourceAgentDetail() {
  if (sourceAgentDetailState.notFound || !sourceAgentDetailState.agent) {
    renderShell(`
      <main class="source-agent-detail">
        <a class="button button-ghost" href="${ROUTES.sourceAgents}">返回 Agent 总览</a>
        <section class="source-agent-detail__empty">
          <p class="web-kicker">Agent 详情</p>
          <h1>${sourceAgentDetailState.loading ? "正在加载 Agent" : "未找到该 Agent"}</h1>
          <p class="web-muted">${escapeHTML(sourceAgentDetailState.message || "该标识无效，或 Worker 尚未注册。")}</p>
        </section>
      </main>
    `, "source-agents");
    return;
  }
  const agent = sourceAgentDetailState.agent;
  const workspace = sourceAgentWorkspace(agent);
  const diagnostics = sourceAgentRedactedDiagnostics(agent);
  const healthEntries = Object.entries(agent.capability_health || {});
  renderShell(`
    <main class="source-agent-detail">
      <nav class="source-agent-detail__nav" aria-label="Agent 详情导航">
        <a class="button button-ghost" href="${ROUTES.sourceAgents}">返回 Agent 总览</a>
        <a class="button button-primary" href="${workspace.href}?agent_id=${encodeURIComponent(sourceAgentDetailState.agentID)}">${workspace.label}</a>
      </nav>
      <header class="source-agent-detail__hero">
        <div><p class="web-kicker">Agent 详情</p><h1>${escapeHTML(agent.agent_id)}</h1><p>${escapeHTML(agent.worker_type)} · ${escapeHTML(agent.platform || "-")} / ${escapeHTML(agent.architecture || "-")}</p></div>
        <div><strong>${escapeHTML(agent.version || "-")}</strong><span>协议 ${escapeHTML(agent.protocol_version || "-")}</span></div>
      </header>
      <section class="source-agent-detail__grid">
        <article>
          <h2>能力详情</h2>
          ${healthEntries.length ? healthEntries.map(([name, health]) => `<dl><div><dt>${escapeHTML(name)}</dt><dd>${health?.healthy ? "可用" : "不可用"}</dd></div><div><dt>状态码</dt><dd>${escapeHTML(health?.code || "-")}</dd></div><div><dt>下一步</dt><dd>${escapeHTML(health?.requires_action || "-")}</dd></div></dl>`).join("") : '<p class="web-muted">无能力上报。</p>'}
        </article>
        <article>
          <h2>Outbox 统计</h2>
          <dl><div><dt>待上传</dt><dd>${Number(agent.outbox_pending || 0)}</dd></div><div><dt>Dead letter</dt><dd>${Number(agent.dead_letter_count || 0)}</dd></div><div><dt>最后成功</dt><dd>${escapeHTML(formatSourceControlTime(agent.last_success_at))}</dd></div></dl>
        </article>
        <article>
          <h2>绑定订阅</h2>
          ${sourceAgentDetailState.subscriptions.length ? sourceAgentDetailState.subscriptions.map((subscription) => `<div class="source-agent-detail__row"><strong>${escapeHTML(subscription.source_account || subscription.source_account_key)}</strong><span>${escapeHTML(subscription.operation || "-")} · ${escapeHTML(formatSourceSchedule(subscription.schedule))}</span></div>`).join("") : '<p class="web-muted">没有绑定订阅。</p>'}
        </article>
        <article>
          <h2>脱敏诊断</h2>
          ${diagnostics.map((item) => `<div class="source-agent-detail__row"><strong>${escapeHTML(item.capability)}</strong><span>${item.healthy ? "正常" : "需处理"}${item.code ? ` · ${escapeHTML(item.code)}` : ""}${item.requiresAction ? ` · ${escapeHTML(item.requiresAction)}` : ""}</span></div>`).join("")}
        </article>
      </section>
      <section class="source-agent-detail__history">
        <article>
          <h2>最近运行</h2>
          ${sourceAgentDetailState.runs.length ? sourceAgentDetailState.runs.map((run) => {
            const detail = sourceAgentDetailState.runDetails[run.id];
            const items = Array.isArray(detail?.items) ? detail.items : [];
            return `<div class="source-agent-detail__timeline"><time>${escapeHTML(formatSourceControlTime(run.created_at))}</time><div><strong>${escapeHTML(sourceRunStatusLabel(run.status))}</strong><span>${escapeHTML(run.requested_operation || "-")} · 新增 ${Number(run.new_count || 0)} / 更新 ${Number(run.updated_count || 0)} / 跳过 ${Number(run.skipped_count || 0)} / 失败 ${Number(run.failed_count || 0)} · 条目 ${items.length}</span></div></div>`;
          }).join("") : '<p class="web-muted">暂无运行。</p>'}
        </article>
        <article>
          <h2>命令时间线</h2>
          ${sourceAgentDetailState.commands.length ? sourceAgentDetailState.commands.map((command) => `<div class="source-agent-detail__timeline"><time>${escapeHTML(formatSourceControlTime(command.created_at))}</time><div><strong>${escapeHTML(command.type)} · ${escapeHTML(command.state)}</strong><span>${escapeHTML(command.result_code || "等待结果")}${command.actual_version ? ` · ${escapeHTML(command.actual_version)}` : ""}</span></div></div>`).join("") : '<p class="web-muted">暂无命令。</p>'}
        </article>
      </section>
    </main>
  `, "source-agents");
}

async function loadSourceAgentDetail(agentID) {
  const sequence = ++sourceAgentDetailSequence;
  sourceAgentDetailState.agentID = agentID;
  sourceAgentDetailState.agent = null;
  sourceAgentDetailState.notFound = !agentID;
  sourceAgentDetailState.loading = agentID ? "正在加载 Agent 详情" : "";
  sourceAgentDetailState.message = "";
  renderSourceAgentDetail();
  if (!agentID) return;
  try {
    const [agentPayload, subscriptionResult, runResult, commandResult] = await Promise.all([
      apiFetch(`/api/source-agents/${encodeURIComponent(sourceAgentDetailState.agentID)}`),
      Promise.resolve(apiFetch("/api/source-subscriptions")).then((value) => ({ status: "fulfilled", value }), (reason) => ({ status: "rejected", reason })),
      Promise.resolve(apiFetch("/api/source-sync/runs?limit=200")).then((value) => ({ status: "fulfilled", value }), (reason) => ({ status: "rejected", reason })),
      Promise.resolve(apiFetch(`/api/source-agents/${encodeURIComponent(sourceAgentDetailState.agentID)}/commands?limit=100`)).then((value) => ({ status: "fulfilled", value }), (reason) => ({ status: "rejected", reason })),
    ]);
    if (sequence !== sourceAgentDetailSequence) return;
    sourceAgentDetailState.agent = agentPayload.agent || null;
    const subscriptions = subscriptionResult.status === "fulfilled" && Array.isArray(subscriptionResult.value.subscriptions) ? subscriptionResult.value.subscriptions : [];
    const runs = runResult.status === "fulfilled" && Array.isArray(runResult.value.runs) ? runResult.value.runs : [];
    sourceAgentDetailState.subscriptions = subscriptions.filter((subscription) => subscription.agent_id === agentID);
    const subscriptionIDs = new Set(sourceAgentDetailState.subscriptions.map((subscription) => subscription.id));
    sourceAgentDetailState.runs = runs.filter((run) => run.agent_id === agentID || subscriptionIDs.has(run.subscription_id)).slice(0, 20);
    sourceAgentDetailState.commands = commandResult.status === "fulfilled" && Array.isArray(commandResult.value.commands) ? commandResult.value.commands : [];
    const detailResults = await Promise.allSettled(sourceAgentDetailState.runs.slice(0, 5).map((run) => apiFetch(`/api/source-sync/runs/${encodeURIComponent(run.id)}`)));
    if (sequence !== sourceAgentDetailSequence) return;
    sourceAgentDetailState.runDetails = Object.fromEntries(sourceAgentDetailState.runs.slice(0, 5).map((run, index) => [run.id, detailResults[index]?.status === "fulfilled" ? detailResults[index].value : null]));
  } catch (error) {
    if (sequence !== sourceAgentDetailSequence) return;
    sourceAgentDetailState.notFound = Number(error?.status || 0) === 404;
    sourceAgentDetailState.message = error instanceof Error ? error.message : String(error);
  } finally {
    if (sequence === sourceAgentDetailSequence) {
      sourceAgentDetailState.loading = "";
      renderSourceAgentDetail();
    }
  }
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
        ${sourceAgentReturnLink()}
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
        options: { page_size: 10, max_items: 500, include_media: true, title_query: "" },
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
  document.querySelector("#knowledge-directory-toggle")?.addEventListener("click", () => {
    knowledgeState.directoryCollapsed = !knowledgeState.directoryCollapsed;
    renderBookKnowledge();
  });
  document.querySelector("[data-knowledge-lifecycle='quality']")?.addEventListener("click", (event) => {
    event.preventDefault();
    setKnowledgeReviewOpen(true);
    window.requestAnimationFrame?.(() => {
      document.querySelector("#knowledge-quality")?.scrollIntoView({ block: "start", behavior: "smooth" });
      const params = new URLSearchParams(window.location.search);
      window.history?.replaceState?.({}, "", `${window.location.pathname}${params.toString() ? `?${params.toString()}` : ""}#knowledge-quality`);
    });
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
      await navigateKnowledgeBook(book);
    });
  }
  for (const button of document.querySelectorAll("[data-cockpit-book-id]")) {
    button.addEventListener("click", async () => {
      const bookID = button.getAttribute("data-cockpit-book-id") || "";
      const book = knowledgeState.books.find((item) => item.book_id === bookID);
      if (!book) {
        return;
      }
      await navigateKnowledgeBook(book, { review: true });
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
        await navigateKnowledgeBook(book);
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
  const sequence = ++bookKnowledgeLoadSequence;
  knowledgeState.loading = "加载书籍";
  knowledgeState.message = "";
  renderBookKnowledge();
  try {
    if (!isKnowledgePackageDetailRoute()) {
      await Promise.all([
        loadKnowledgeReviewCockpit({ silent: true, renderResult: false }),
        loadKnowledgePipelineDashboard({ silent: true, renderResult: false }),
      ]);
    }
    const payload = await apiFetch("/api/books");
    if (sequence !== bookKnowledgeLoadSequence) {
      return;
    }
    knowledgeState.books = Array.isArray(payload.books) ? payload.books : [];
    if (knowledgeState.books.length && isKnowledgePackageDetailRoute()) {
      const queryBookID = new URLSearchParams(window.location.search).get("book_id") || "";
      const preferredID = getKnowledgeBookID() || queryBookID || knowledgeState.selectedBook?.book_id || "";
      const preferred = preferredID
        ? knowledgeState.books.find((book) => book.book_id === preferredID)
        : null;
      await selectKnowledgeBook(preferred || knowledgeState.books[0], false);
      if (sequence !== bookKnowledgeLoadSequence) {
        return;
      }
      const evidenceLocator = new URLSearchParams(window.location.search);
      const citationID = evidenceLocator.get("citation_id") || "";
      const evidenceQuery = citationID || evidenceLocator.get("chunk_id") || evidenceLocator.get("claim_id") || "";
      if (citationID) {
        const resolved = await apiFetch(
          `/api/citations/${encodeURIComponent(citationID)}?book_id=${encodeURIComponent(knowledgeState.selectedBook.book_id)}`,
        );
        if (
          sequence !== bookKnowledgeLoadSequence ||
          String(knowledgeState.selectedBook?.book_id || "") !== String(preferred?.book_id || knowledgeState.books[0]?.book_id || "")
        ) {
          return;
        }
        const citation = resolved.citation || {};
        knowledgeState.query = citationID;
        knowledgeState.results = [{
          kind: "citation",
          id: citation.citation_id || citationID,
          title: `引用 ${citation.citation_id || citationID}`,
          snippet: [
            ...(Array.isArray(resolved.claim_ids) ? resolved.claim_ids : []),
            citation.chapter_id,
            citation.chunk_id,
          ].filter(Boolean).join(" · "),
        }];
        knowledgeState.message = "已精确定位审计引用。";
      } else if (evidenceQuery) {
        knowledgeState.query = evidenceQuery;
        await searchBookKnowledge();
      }
    } else if (!knowledgeState.books.length) {
      knowledgeState.selectedBook = null;
      knowledgeState.package = null;
      knowledgeState.results = [];
      resetKnowledgeReview();
    }
    knowledgeState.message = `已加载 ${knowledgeState.books.length} 本。`;
  } catch (error) {
    if (sequence !== bookKnowledgeLoadSequence) {
      return;
    }
    knowledgeState.message = error instanceof Error ? error.message : String(error);
  } finally {
    if (sequence === bookKnowledgeLoadSequence) {
      knowledgeState.loading = "";
      renderBookKnowledge();
    }
  }
}

async function navigateKnowledgeBook(book, { review = false } = {}) {
  if (!book?.book_id) {
    return;
  }
  const target = `${sourceKnowledgeURL(book.book_id)}${review ? "?review=1" : ""}`;
  window.history?.pushState?.({}, "", target);
  await selectKnowledgeBook(book);
  if (review) {
    setKnowledgeReviewOpen(true);
  }
  window.requestAnimationFrame?.(() => {
    document.querySelector(".knowledge-web__main")?.scrollIntoView({
      block: "start",
      behavior: "smooth",
    });
  });
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
        limit: 1,
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

function renderKnowledgeOperationsConsole() {
  const dashboard = knowledgeOperationsState.console || {};
  const summary = dashboard.summary || {};
  const items = Array.isArray(dashboard.items) ? dashboard.items : [];
  const healthReviewDiagnostics = dashboard.health_review_diagnostics || {};
  const healthReviewQueue = Array.isArray(dashboard.health_review_queue) ? dashboard.health_review_queue : [];
  const status = knowledgeOperationsState.loading || knowledgeOperationsState.message || `${Number(summary.total || 0)} packages`;
  const replay = knowledgeOperationsState.replayResult;
  renderShell(`
    <main class="knowledge-operations">
      <section class="knowledge-operations__hero">
        <p class="web-kicker">Knowledge Operations Console</p>
        <h1>Release Status Center</h1>
        <p>发布、Health 审核草稿和失败重放集中观察。这里不会展示源正文，也不会推进 Health serving。</p>
        <div class="knowledge-operations__actions">
          <button id="knowledge-operations-refresh" class="button button-primary" type="button" ${knowledgeOperationsState.loading ? "disabled" : ""}>刷新</button>
          <span>${escapeHTML(status)}</span>
        </div>
      </section>
      <section class="knowledge-operations__summary" aria-label="Release Status Center">
        ${renderKnowledgeOperationsMetric("待分析", summary.needs_analysis)}
        ${renderKnowledgeOperationsMetric("待质检", summary.needs_quality)}
        ${renderKnowledgeOperationsMetric("可发布", summary.ready_to_publish)}
        ${renderKnowledgeOperationsMetric("已发布", summary.published)}
        ${renderKnowledgeOperationsMetric("Health 待审核", summary.health_ready_to_publish)}
        ${renderKnowledgeOperationsMetric("Health 已拉取", summary.health_published)}
      </section>
      ${renderKnowledgeOperationsHealthReviewQueue(healthReviewQueue)}
      ${renderKnowledgeOperationsHealthReviewDiagnostics(healthReviewDiagnostics)}
      <section class="knowledge-operations__panel" aria-label="Health Evidence Review Workspace">
        <div class="knowledge-operations__panel-head">
          <div>
            <p class="web-kicker">Health Evidence Review Workspace</p>
            <h2>全部包状态和阻断原因</h2>
          </div>
          <small>serving_allowed 始终由 Health 审核系统决定；KBase 只显示证据元数据。</small>
        </div>
        <div class="knowledge-operations__table">
          ${items.map(renderKnowledgeOperationsItem).join("") || `<p class="knowledge-operations__empty">暂无 operations 数据。</p>`}
        </div>
      </section>
      <section class="knowledge-operations__panel" aria-label="Failure Explanation">
        <div class="knowledge-operations__panel-head">
          <div>
            <p class="web-kicker">Failure Explanation</p>
            <h2>安全重放结果</h2>
          </div>
          <small>只允许 analyze / evaluate_quality；publish 与 health_serving_promote 被后端拒绝。</small>
        </div>
        ${replay ? `<pre class="knowledge-operations__result">${escapeHTML(JSON.stringify(replay, null, 2))}</pre>` : `<p class="knowledge-operations__empty">选择一条安全 replay 操作后，结果会显示在这里。</p>`}
      </section>
    </main>
  `, "operations");
  bindKnowledgeOperationsEvents();
}

function renderKnowledgeOperationsMetric(label, value) {
  return `<div class="knowledge-operations__metric"><span>${escapeHTML(label)}</span><strong>${Number(value || 0)}</strong></div>`;
}

function renderKnowledgeOperationsHealthReviewQueue(queue) {
  return `
    <section class="knowledge-operations__panel" aria-label="Health Evidence Review Queue">
      <div class="knowledge-operations__panel-head">
        <div>
          <p class="web-kicker">Health Evidence Review Queue</p>
          <h2>待审核优先级队列</h2>
        </div>
        <small>只读队列：不写入 Health 审核状态，也不推进 serving。</small>
      </div>
      <div class="knowledge-operations__queue">
        ${queue.map(renderKnowledgeOperationsHealthReviewItem).join("") || `<p class="knowledge-operations__empty">暂无 Health 审核队列。</p>`}
      </div>
    </section>
  `;
}

function renderKnowledgeOperationsHealthReviewDiagnostics(diagnostics) {
  const statusCounts = diagnostics.status_counts || {};
  const blockers = Array.isArray(diagnostics.blockers) ? diagnostics.blockers : [];
  const actions = Array.isArray(diagnostics.next_safe_actions) ? diagnostics.next_safe_actions : [];
  const statusRows = Object.entries(statusCounts).map(([status, count]) => `
    <span class="knowledge-operations__pill">${escapeHTML(status)}: ${Number(count || 0)}</span>
  `).join("");
  return `
    <section class="knowledge-operations__panel" aria-label="Health Queue Diagnostics">
      <div class="knowledge-operations__panel-head">
        <div>
          <p class="web-kicker">Health Queue Diagnostics</p>
          <h2>为什么没有待审核项？</h2>
        </div>
        <small>只显示元数据诊断；这里不会执行发布、Health serving 或外部写入。</small>
      </div>
      <div class="knowledge-operations__diagnostics">
        <article>
          <strong>${escapeHTML(knowledgeOperationsDiagnosticReasonLabel(diagnostics.queue_empty_reason || "unknown"))}</strong>
          <small>${escapeHTML(diagnostics.queue_empty_reason || "unknown")}</small>
          <div class="knowledge-operations__pills">${statusRows || `<span class="knowledge-operations__pill">无 Health readiness 命中</span>`}</div>
        </article>
        <article>
          <strong>安全下一步</strong>
          ${actions.length ? actions.map((action) => `
            <div class="knowledge-operations__diagnostic-action" data-knowledge-health-diagnostic-action="${escapeAttribute(action.action || "")}">
              <span>${escapeHTML(action.label || action.action || "inspect_status")}</span>
              <small>${escapeHTML(action.action || "inspect_status")}${action.count ? ` · ${Number(action.count)} 项` : ""}</small>
            </div>
          `).join("") : `<p class="knowledge-operations__empty">暂无建议动作。</p>`}
        </article>
        <article>
          <strong>阻断分类</strong>
          ${blockers.length ? blockers.map((blocker) => `
            <div class="knowledge-operations__diagnostic-action">
              <span>${escapeHTML(blocker.label || blocker.status || "blocked")}</span>
              <small>${escapeHTML(blocker.safe_action || "inspect_status")} · ${Number(blocker.count || 0)} 项</small>
            </div>
          `).join("") : `<p class="knowledge-operations__empty">当前可见范围没有阻断分类。</p>`}
        </article>
      </div>
    </section>
  `;
}

function knowledgeOperationsDiagnosticReasonLabel(reason) {
  switch (reason) {
    case "queue_has_items":
      return "队列已有可处理项";
    case "no_operations_items":
      return "当前没有 operations 数据";
    case "no_health_readiness_items":
      return "当前可见包没有 Health readiness 状态";
    case "no_items_match_current_limit":
      return "当前可见范围没有命中 Health 队列";
    case "all_visible_items_need_upstream_work":
      return "可见包需要先完成上游分析或质检";
    case "all_visible_items_ready_or_imported":
      return "可见包已准备好，下一步归 Health 审核侧";
    default:
      return "等待更多诊断数据";
  }
}

function renderKnowledgeOperationsHealthReviewItem(item) {
  const riskCounts = item.risk_counts || {};
  const riskText = Object.entries(riskCounts).map(([risk, count]) => `${risk}:${count}`).join(" · ") || "无风险计数";
  const reasons = Array.isArray(item.reasons) ? item.reasons : [];
  return `
    <article class="knowledge-operations__queue-item">
      <div>
        <span class="knowledge-operations__badge">${escapeHTML(item.priority_label || "monitor")}</span>
        <strong>${escapeHTML(item.title || item.book_id || "未命名知识")}</strong>
        <small>${escapeHTML(item.book_id || "")}${item.release_id ? ` · ${escapeHTML(knowledgeHash(item.release_id))}` : ""}</small>
      </div>
      <div>
        <span>${escapeHTML(item.status || "unknown")}</span>
        <small>priority ${Number(item.priority || 0)} · ${item.consumer_review_required ? "需要 Health 人工审核" : "KBase 侧准备中"}</small>
      </div>
      <div>
        <span data-knowledge-health-review-action="${escapeAttribute(item.next_operator_action || "")}">${escapeHTML(item.next_operator_action || "inspect_status")}</span>
        <small>serving_allowed=${item.serving_allowed ? "true" : "false"}</small>
      </div>
      <div>
        <span>claims ${Number(item.claim_count || 0)} · citations ${Number(item.citation_count || 0)}</span>
        <small>${escapeHTML(riskText)}</small>
        ${reasons.length ? `<small>${escapeHTML(reasons.join(" / "))}</small>` : ""}
      </div>
    </article>
  `;
}

function renderKnowledgeOperationsItem(item) {
  const health = item.health || {};
  const failure = item.failure || {};
  const riskCounts = health.risk_counts || {};
  const riskText = Object.entries(riskCounts).map(([risk, count]) => `${risk}:${count}`).join(" · ") || "无风险计数";
  const safeAction = failure.safe_replay_action || "";
  const canReplay = safeAction === "analyze" || safeAction === "evaluate_quality";
  return `
    <article class="knowledge-operations__row">
      <div>
        <strong>${escapeHTML(item.title || item.book_id || "未命名知识")}</strong>
        <small>${escapeHTML(item.book_id || "")}</small>
      </div>
      <div>
        <span class="knowledge-operations__badge">${escapeHTML(item.pipeline_stage || "unknown")}</span>
        <small>${escapeHTML(item.next_action || "-")}</small>
      </div>
      <div>
        <span>${escapeHTML(item.release_id ? knowledgeHash(item.release_id) : "未发布")}</span>
        <small>${escapeHTML(item.quality_decision || "未质检")} · ${escapeHTML(item.usage_policy || "-")}</small>
      </div>
      <div>
        <span>${escapeHTML(health.status || "not_ready")}</span>
        <small>claims ${Number(health.claim_count || 0)} · citations ${Number(health.citation_count || 0)} · ${escapeHTML(riskText)}</small>
        ${(health.reasons || []).length ? `<small>${escapeHTML((health.reasons || []).join(" / "))}</small>` : ""}
      </div>
      <div>
        <span>${escapeHTML(failure.explanation || "无失败解释")}</span>
        ${canReplay ? `<button class="button button-ghost" type="button" data-knowledge-operations-replay="${escapeAttribute(safeAction)}" data-book-id="${escapeAttribute(item.book_id || "")}" ${knowledgeOperationsState.replaying ? "disabled" : ""}>安全重放：${escapeHTML(safeAction)}</button>` : `<small>无安全重放动作</small>`}
      </div>
    </article>
  `;
}

function bindKnowledgeOperationsEvents() {
  document.querySelector("#knowledge-operations-refresh")?.addEventListener("click", async () => {
    await loadKnowledgeOperationsConsole();
  });
  for (const button of document.querySelectorAll("[data-knowledge-operations-replay]")) {
    button.addEventListener("click", async () => {
      const action = button.getAttribute("data-knowledge-operations-replay") || "";
      const bookID = button.getAttribute("data-book-id") || "";
      await replayKnowledgeOperationsAction(bookID, action);
    });
  }
}

async function loadKnowledgeOperationsConsole() {
  knowledgeOperationsState.loading = "加载 Operations";
  knowledgeOperationsState.message = "";
  renderKnowledgeOperationsConsole();
  try {
    knowledgeOperationsState.console = await apiFetch("/api/knowledge/operations?limit=100");
    knowledgeOperationsState.message = "Operations 已刷新。";
  } catch (error) {
    knowledgeOperationsState.message = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeOperationsState.loading = "";
    renderKnowledgeOperationsConsole();
  }
}

async function replayKnowledgeOperationsAction(bookID, action) {
  if (!(action === "analyze" || action === "evaluate_quality")) {
    knowledgeOperationsState.message = "该动作不是安全 replay。";
    renderKnowledgeOperationsConsole();
    return;
  }
  const ok = window.confirm(`确认安全重放 ${action}？不会执行 publish，也不会推进 Health serving。`);
  if (!ok) {
    return;
  }
  knowledgeOperationsState.replaying = `${bookID}:${action}`;
  knowledgeOperationsState.message = "安全重放执行中";
  renderKnowledgeOperationsConsole();
  try {
    knowledgeOperationsState.replayResult = await apiFetch("/api/knowledge/operations/replay", {
      method: "POST",
      body: JSON.stringify({
        book_id: bookID,
        action,
        confirm: true,
        model: knowledgeState.analysisModel || "qwen3.7-max",
        max_context_chars: 16000,
      }),
    });
    knowledgeOperationsState.console = await apiFetch("/api/knowledge/operations?limit=100");
    knowledgeOperationsState.message = "安全重放完成。";
  } catch (error) {
    knowledgeOperationsState.message = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeOperationsState.replaying = "";
    renderKnowledgeOperationsConsole();
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
  const sequence = ++bookKnowledgeDetailSequence;
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
    const pkg = await apiFetch(`/api/books/${encodeURIComponent(book.book_id)}`);
    if (sequence !== bookKnowledgeDetailSequence || knowledgeState.selectedBook?.book_id !== book.book_id) {
      return;
    }
    knowledgeState.package = pkg;
    await Promise.all([
      loadKnowledgeAnalysisManifest(book.book_id, sequence),
      loadKnowledgeReview(book.book_id, { silent: true, renderResult: false }),
      loadKnowledgeAgentPackages(book.book_id, { silent: true, renderResult: false }),
    ]);
  } catch (error) {
    if (sequence === bookKnowledgeDetailSequence && knowledgeState.selectedBook?.book_id === book.book_id) {
      knowledgeState.message = error instanceof Error ? error.message : String(error);
    }
  } finally {
    if (sequence === bookKnowledgeDetailSequence && knowledgeState.selectedBook?.book_id === book.book_id) {
      knowledgeState.loading = "";
      if (renderBefore) {
        renderBookKnowledge();
      }
    }
  }
}

async function loadKnowledgeAnalysisManifest(bookID, sequence = bookKnowledgeDetailSequence) {
  knowledgeState.analysisManifestError = "";
  try {
    const manifest = await apiFetch(`/api/books/${encodeURIComponent(bookID)}/analysis`);
    if (sequence !== bookKnowledgeDetailSequence || knowledgeState.selectedBook?.book_id !== bookID) {
      return;
    }
    knowledgeState.analysisManifest = manifest;
  } catch (error) {
    if (sequence !== bookKnowledgeDetailSequence || knowledgeState.selectedBook?.book_id !== bookID) {
      return;
    }
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
  bookKnowledgeLoadSequence += 1;
  const routePathname = getRoutePathname();
  if (!routePathname.startsWith(ROUTES.sourceAgents)) {
    if (sourceAgentManagementPollTimer) {
      clearTimeout(sourceAgentManagementPollTimer);
      sourceAgentManagementPollTimer = null;
    }
    sourceAgentManagementSequence += 1;
    sourceAgentDetailSequence += 1;
  }
  const isBookAgentRoute = (
    routePathname === ROUTES.agentPackages || routePathname.startsWith(`${ROUTES.agentPackages}/`) ||
    routePathname === ROUTES.agents || routePathname.startsWith(`${ROUTES.agents}/`) ||
    routePathname === ROUTES.bookApps || routePathname.startsWith(`${ROUTES.bookApps}/`)
  );
  if (!isBookAgentRoute) {
    deactivateProofroomModal({ restoreFocus: true });
    cancelEvidenceAuditPoll();
    evidenceAuditLoadSequence += 1;
    evidenceAuditWorkspaceSequence += 1;
    bookAgentLoadSequence += 1;
    proofroomOperationSequence += 1;
    evidenceAuditState.routeAuditID = "";
  }
  if (routePathname === ROUTES.dedaoLogin) {
    renderDedaoLogin();
    await loadDedaoSession();
    return;
  }
  if (window.location.pathname === "/" || routePathname === ROUTES.dedaoHome) {
    renderDedaoHome();
    await loadDedaoHome();
    return;
  }
  if (routePathname === ROUTES.sessionSettings) {
    renderSessionSettings();
    await loadSessionSettings();
    return;
  }
  if (routePathname === ROUTES.jobs || routePathname.startsWith(`${ROUTES.jobs}/`)) {
    renderJobCenter();
    await loadJobCenter();
    return;
  }
  if (routePathname === ROUTES.operations) {
    renderKnowledgeOperationsConsole();
    await loadKnowledgeOperationsConsole();
    return;
  }
  const sourceAgentDetailID = getSourceAgentDetailID(routePathname);
  if (sourceAgentDetailID !== null) {
    renderSourceAgentDetail();
    await loadSourceAgentDetail(sourceAgentDetailID);
    return;
  }
  if (routePathname === ROUTES.sourceAgents) {
    renderSourceAgentOverview();
    await loadSourceAgentManagement();
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
    await Promise.allSettled([loadDedaoLibrary("ebook"), loadDedaoEbookJobs()]);
    return;
  }
  if (routePathname.startsWith(`${ROUTES.dedaoAudio}/`)) {
    const audioRoute = getDedaoAudioRoute();
    renderDedaoAudioDetail(audioRoute);
    await loadDedaoAudioDetail(audioRoute);
    return;
  }
  if (routePathname === ROUTES.dedaoAudio) {
    renderDedaoOdob();
    await loadDedaoLibrary("odob");
    return;
  }
  if (isBookAgentRoute) {
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

window.addEventListener?.("popstate", () => {
  boot();
});

boot();
