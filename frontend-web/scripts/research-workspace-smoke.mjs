import assert from "node:assert/strict";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");

function functionSource(name) {
  const functionStart = js.indexOf(`function ${name}(`);
  assert.notEqual(functionStart, -1, `Research workspace should define ${name}`);
  const start = js.slice(Math.max(0, functionStart - 6), functionStart) === "async " ? functionStart - 6 : functionStart;
  const bodyStart = js.indexOf("{", start);
  let depth = 0;
  let quote = "";
  let escaped = false;
  for (let index = bodyStart; index < js.length; index += 1) {
    const character = js[index];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (quote) {
      if (character === "\\") escaped = true;
      else if (character === quote) quote = "";
      continue;
    }
    if (character === '"' || character === "'" || character === "`") {
      quote = character;
      continue;
    }
    if (character === "{") depth += 1;
    if (character === "}") {
      depth -= 1;
      if (depth === 0) return js.slice(start, index + 1);
    }
  }
  assert.fail(`Could not extract ${name}`);
}

function loadFunctions(names, context = {}) {
  const sandbox = vm.createContext({ ...context });
  vm.runInContext(`${names.map(functionSource).join("\n")}\nthis.result = { ${names.join(", ")} };`, sandbox);
  return sandbox.result;
}

async function openCDPSocket(url) {
  const socket = new WebSocket(url);
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", reject, { once: true });
  });
  let nextID = 0;
  const pending = new Map();
  socket.addEventListener("message", (event) => {
    const message = JSON.parse(String(event.data));
    if (!message.id || !pending.has(message.id)) return;
    const { resolve, reject } = pending.get(message.id);
    pending.delete(message.id);
    if (message.error) reject(new Error(message.error.message));
    else resolve(message.result);
  });
  return {
    command(method, params = {}) {
      nextID += 1;
      const id = nextID;
      socket.send(JSON.stringify({ id, method, params }));
      return new Promise((resolve, reject) => pending.set(id, { resolve, reject }));
    },
    close() { socket.close(); },
  };
}

async function researchWorkspaceBrowserLayout(cssText) {
  const browserCandidates = [
    process.env.KBASE_CHROME_PATH,
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
    "/usr/bin/google-chrome",
    "/usr/bin/chromium",
    "/usr/bin/chromium-browser",
  ].filter(Boolean);
  const browserPath = browserCandidates.find((candidate) => fs.existsSync(candidate));
  assert.ok(browserPath, "760px Research layout smoke requires a local Chrome or Chromium executable");
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), "kbase-research-layout-"));
  const debuggerPort = await new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close(() => resolve(address.port));
    });
  });
  const browserArguments = [
    "--headless", "--disable-gpu", "--hide-scrollbars", `--remote-debugging-port=${debuggerPort}`,
    `--user-data-dir=${profile}`, "--window-size=760,1100", "about:blank",
  ];
  const forceNativeChrome = process.platform === "darwin" && browserPath.includes("Google Chrome.app") && fs.existsSync("/usr/bin/arch");
  const browser = spawn(forceNativeChrome ? "/usr/bin/arch" : browserPath, forceNativeChrome
    ? ["-arm64", browserPath, ...browserArguments]
    : browserArguments, { stdio: ["ignore", "ignore", "pipe"] });
  const browserExit = new Promise((resolve) => browser.once("exit", resolve));
  let pageSocket;
  try {
    const targets = await new Promise((resolve, reject) => {
      let stderr = "";
      let settled = false;
      const finish = (callback, value) => {
        if (settled) return;
        settled = true;
        clearTimeout(timeout);
        callback(value);
      };
      const timeout = setTimeout(() => finish(reject, new Error(`Chrome DevTools endpoint timeout: ${stderr}`)), 30000);
      browser.stderr.on("data", (chunk) => { stderr += String(chunk); });
      browser.once("exit", (code) => {
        finish(reject, new Error(`Chrome exited before layout verification: ${code} ${stderr}`));
      });
      const poll = async () => {
        if (settled) return;
        try {
          const response = await fetch(`http://127.0.0.1:${debuggerPort}/json/list`);
          if (response.ok) {
            finish(resolve, await response.json());
            return;
          }
        } catch {
          // Chrome has not bound its DevTools port yet.
        }
        setTimeout(poll, 100);
      };
      void poll();
    });
    const pageTarget = targets.find((target) => target.type === "page");
    assert.ok(pageTarget?.webSocketDebuggerUrl, "Chrome should expose the synthetic Research page target");
    pageSocket = await openCDPSocket(pageTarget.webSocketDebuggerUrl);
    await pageSocket.command("Runtime.enable");
    await pageSocket.command("Emulation.setDeviceMetricsOverride", { width: 760, height: 1100, deviceScaleFactor: 1, mobile: false });
    const markup = `<div id="app"><main class="research-workspace">
      <header class="research-workspace__heading"><div><p>RESEARCH DOSSIER</p><h1>研究工作台</h1></div><p>记录检索范围、证据选择、身份边界与引用核验。</p></header>
      <form class="research-launchpad"><section class="research-launchpad__draft"><header><span>01 / 定义研究</span><h2>问题与范围</h2></header>
        <div class="research-launchpad__draft-grid"><label class="research-launchpad__question"><span>研究问题</span><textarea name="question">比较两个来源中超长但不应造成横向滚动的公开证据</textarea></label>
        <fieldset><legend>模式</legend><label><input type="radio" name="mode"><span>深度研究</span></label></fieldset>
        <fieldset><legend>来源</legend><label><input type="checkbox" name="sources"><span>本地聊天记录</span></label></fieldset></div></section>
        <section class="research-preflight"><header><div><span>02 / 自动预检与人工确认</span><h2>推荐 Agent</h2></div></header>
          <div class="research-preflight__grid"><section><div class="research-agent-list"><article class="research-agent-card"><strong>益家知研公众号集合知识库研究助手</strong><small>research-agent-abcdef0123456789abcdef0123456789abcdef0123456789</small></article></div></section>
          <section><div class="research-checks"><article class="is-pass"><span>✓</span><div><strong>运行前检查</strong><small>来源允许</small></div><b>通过</b></article></div></section></div></section>
      </form>
      <section class="research-dossier"><aside class="research-stage-rail"><header><span>RUN</span><strong>证据不足</strong></header></aside>
        <section class="research-dossier__center"><section class="research-failure"><span>partial_evidence</span><h3>只取得部分证据</h3><p>报告保留缺口。</p>
          <div class="research-failure__retry"><strong>下一步建议</strong><p>扩大检索范围后，再运行一次独立研究。</p><button class="button button-primary" type="button">调整并重试</button></div></section></section>
        <aside class="research-scope-ledger"><section><span>检索范围</span><p>knowledge chatlog prior_runs</p></section></aside></section>
    </main></div>`;
    await pageSocket.command("Runtime.evaluate", {
      expression: `document.head.innerHTML = '<meta name="viewport" content="width=device-width,initial-scale=1"><style>' + ${JSON.stringify(cssText)} + '</style>'; document.body.innerHTML = ${JSON.stringify(markup)}; document.body.tabIndex = -1; document.body.focus();`,
      awaitPromise: true,
    });
    await pageSocket.command("Input.dispatchKeyEvent", { type: "rawKeyDown", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 });
    await pageSocket.command("Input.dispatchKeyEvent", { type: "keyUp", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 });
    const evaluation = await pageSocket.command("Runtime.evaluate", {
      expression: `(() => { const active = document.activeElement; const style = getComputedStyle(active); return { clientWidth: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth, activeName: active?.getAttribute('name') || '', outlineStyle: style.outlineStyle, outlineWidth: style.outlineWidth }; })()`,
      returnByValue: true,
    });
    return evaluation.result.value;
  } finally {
    pageSocket?.close();
    if (browser.exitCode === null) browser.kill("SIGTERM");
    await Promise.race([browserExit, new Promise((resolve) => setTimeout(resolve, 3000))]);
    if (browser.exitCode === null) {
      browser.kill("SIGKILL");
      await browserExit;
    }
    fs.rmSync(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  }
}

for (const marker of [
  'research: "/research"', "getResearchRoute", "renderResearchWorkspace", "loadResearchRun",
  "pollResearchEvents", "createResearchRun", "cancelResearchRun", "confirmResearchIdentity",
  "研究工作台", "快速检索", "自动判断", "深度研究", "知识库", "本地聊天记录", "历史研究",
  "开始研究", "取消运行", "检索范围", "引用范围", "证据", "时间线", "冲突", "研究报告",
  "确认身份", "模型输出格式无效",
]) {
  assert.ok(js.includes(marker), `Research workspace should include ${marker}`);
}

for (const controller of [
  "researchListRequestController", "researchDetailRequestController", "researchEventsRequestController",
  "researchPreflightRequestController",
]) {
  assert.ok(js.includes(controller), `Research workspace should keep an independent ${controller}`);
}
assert.ok(js.includes('query.set("after"'), "event polling should resume from an after cursor");
assert.ok(js.includes("researchTerminalStatuses"), "terminal runs should stop event polling");
assert.ok(js.includes("clearResearchRunDetail"), "route changes should clear stale run detail immediately");
assert.ok(js.includes('headers: { "Idempotency-Key"'), "run creation should use an idempotency key");
assert.ok(js.includes('capabilities.includes("deep_research")'), "Agent console should expose Research only for opted-in packages");

class FakeAbortController {
  constructor() { this.signal = { aborted: false }; }
  abort() { this.signal.aborted = true; }
}

const { createResearchPreflightRequestController } = loadFunctions(
  ["createResearchPreflightRequestController"],
  { AbortController: FakeAbortController },
);
const controller = createResearchPreflightRequestController();
const firstRequest = controller.begin("first-fingerprint");
const secondRequest = controller.begin("second-fingerprint");
assert.equal(firstRequest.signal.aborted, true, "a newer preflight request should abort the previous request");
assert.equal(controller.isCurrent(firstRequest.sequence, firstRequest.fingerprint), false, "an older preflight sequence must be stale");
assert.equal(controller.isCurrent(secondRequest.sequence, secondRequest.fingerprint), true, "the newest preflight sequence and fingerprint should remain current");
controller.cancel();
assert.equal(secondRequest.signal.aborted, true, "route changes should abort the current preflight request");

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

const routeDraft = { question: "比较同一问题", mode: "auto", sources: ["knowledge"], subjectIDs: [], packageConstraint: "agent-old" };
const routeState = {
  draft: routeDraft,
  preflight: { preflight_id: "old-preflight", candidates: [{ package_id: "agent-old", package_version: "1.0.0" }] },
  preflightFingerprint: "old-fingerprint",
  selectedCandidateKey: "agent-old\u00001.0.0",
  runSubmission: { fingerprint: "old-submission", idempotencyKey: "research-ui:old" },
  loading: { preflight: false },
  error: "",
  message: "",
};
const routeController = createResearchPreflightRequestController();
const routeFetches = [];
const routeTimers = new Map();
const oldPreflightResponse = deferred();
const newPreflightResponse = deferred();
const newPreflightApplied = deferred();
let nextRouteTimerID = 0;
const routeRuntime = loadFunctions([
  "researchSourceOrder", "normalizeResearchDraft", "researchDraftFingerprint", "researchCandidateKey",
  "selectResearchPreflightCandidate", "applyResearchPreflightResponse", "markResearchPreflightPending",
  "clearResearchRunSubmission", "clearResearchPreflightTimers", "cancelResearchPreflightLifecycle", "scheduleResearchPreflight",
  "buildResearchPreflightRequest", "requestResearchPreflight", "synchronizeResearchRouteConstraint",
], {
  researchState: routeState,
  researchPreflightRequestController: routeController,
  researchPreflightDebounceMS: 600,
  researchPreflightDebounceTimer: null,
  researchPreflightExpiryTimer: null,
  clearTimeout: (timerID) => routeTimers.delete(timerID),
  window: { setTimeout: (callback) => { nextRouteTimerID += 1; routeTimers.set(nextRouteTimerID, callback); return nextRouteTimerID; } },
  document: { querySelector: () => null },
  getResearchRoute: () => ({ runID: "" }),
  renderResearchWorkspacePreservingDraftFocus: () => {},
  researchPreflightErrorMessage: () => "预检失败",
  scheduleResearchPreflightExpiry: () => newPreflightApplied.resolve(),
  apiFetch: async (url, options) => {
    routeFetches.push({ url, options, body: JSON.parse(options.body) });
    return routeFetches.length === 1 ? oldPreflightResponse.promise : newPreflightResponse.promise;
  },
});
const oldRequestCompletion = routeRuntime.requestResearchPreflight(routeDraft);
assert.equal(routeState.loading.preflight, true, "the original constrained preflight should be loading");
assert.equal(routeRuntime.synchronizeResearchRouteConstraint("agent-new"), true, "a changed route constraint should schedule a replacement preflight");
assert.equal(routeFetches[0].options.signal.aborted, true, "changing the route constraint should abort the in-flight request immediately");
assert.equal(routeState.loading.preflight, false, "canceling the old request should release its loading state before the replacement starts");
assert.equal(routeState.preflight, null, "changing the route constraint should clear the old preflight snapshot");
assert.equal(routeState.selectedCandidateKey, "", "changing the route constraint should clear the old Agent selection");
assert.equal(routeState.runSubmission, null, "changing the route constraint should clear the old run submission identity");
assert.equal(routeTimers.size, 1, "the changed constraint should schedule one replacement preflight");
const replacementTimer = [...routeTimers.values()][0];
replacementTimer();
assert.equal(routeFetches.length, 2, "the scheduled replacement should request a new preflight");
assert.equal(routeFetches[1].body.package_constraint, "agent-new", "the replacement preflight should use the new route constraint");
const newRouteFingerprint = routeRuntime.researchDraftFingerprint({ ...routeDraft, packageConstraint: "agent-new" });
oldPreflightResponse.resolve({ preflight_id: "stale-preflight", status: "ready", expires_at: "2099-01-01T00:00:00Z", candidates: [{ package_id: "agent-old", package_version: "1.0.0" }], checks: [] });
await oldRequestCompletion;
assert.equal(routeState.preflight, null, "the aborted request response must not restore the old snapshot");
assert.equal(routeState.loading.preflight, true, "the aborted request finally must not clear the replacement request loading state");
newPreflightResponse.resolve({ preflight_id: "new-preflight", status: "ready", expires_at: "2099-01-01T00:00:00Z", candidates: [{ package_id: "agent-new", package_version: "2.0.0" }], checks: [] });
await newPreflightApplied.promise;
await Promise.resolve();
assert.equal(routeState.preflight?.preflight_id, "new-preflight", "the replacement response should become the active preflight");
assert.equal(routeState.preflightFingerprint, newRouteFingerprint, "the replacement preflight should bind to the new draft fingerprint");
assert.equal(routeState.loading.preflight, false, "the replacement request should release loading after it applies");
routeState.selectedCandidateKey = "agent-new\u00002.0.0";
routeRuntime.scheduleResearchPreflight({ ...routeState.draft, question: "比较同一问题的新范围" });
assert.equal(routeState.selectedCandidateKey, "agent-new\u00002.0.0", "ordinary draft edits should preserve a manual selection for reuse when the next candidate set still contains it");
assert.equal(routeRuntime.synchronizeResearchRouteConstraint(""), true, "removing the route constraint should invalidate the constrained snapshot");
assert.equal(routeState.draft.packageConstraint, undefined, "leaving a constrained Research URL should remove the hidden Package constraint from the draft");
assert.equal(routeState.selectedCandidateKey, "", "removing the route constraint should clear the constrained Agent selection");

function researchDetailRuntime({ routeRunID = "research-run-owner", response, error = null } = {}) {
  const state = {
    detail: { run: { run_id: routeRunID, status: "insufficient", failure: { code: "partial_evidence" } } },
    detailOwnerVisible: true,
    loading: { detail: false },
    message: "",
  };
  let currentRouteRunID = routeRunID;
  let sequence = 0;
  const controller = {
    begin() { sequence += 1; return { sequence, signal: { aborted: false } }; },
    isCurrent(candidate) { return candidate === sequence; },
    cancel() { sequence += 1; },
  };
  const renders = [];
  const requests = [];
  const pending = deferred();
  const recentRunStorage = new Map();
  const runtime = loadFunctions([
    "researchSourceOrder", "normalizeResearchDraft", "researchCandidateKey", "researchLinkedRetrySuggestion",
    "researchLinkedRetryPlan", "isCanonicalResearchRunID", "researchRecentRunIDs", "rememberResearchRun",
    "researchDetailRequestOptions", "loadResearchRun", "startResearchLinkedRetry",
  ], {
    researchState: state,
    researchDetailRequestController: controller,
    researchRecentRunsKey: "kbase.research.recent-runs.v1",
    apiFetch: async (url, options) => {
      requests.push({ url, options });
      const next = await pending.promise;
      if (next instanceof Error) throw next;
      return next;
    },
    getResearchRoute: () => ({ runID: currentRouteRunID }),
    renderResearchWorkspace: (route) => renders.push(route),
    cancelResearchPreflightLifecycle: () => { throw new Error("unauthorized retry mutated preflight"); },
    clearResearchRunDetail: () => { throw new Error("unauthorized retry cleared detail"); },
    window: {
      history: { pushState: () => { throw new Error("unauthorized retry navigated"); } },
      localStorage: {
        getItem: (key) => recentRunStorage.get(key) ?? null,
        setItem: (key, value) => recentRunStorage.set(key, value),
      },
    },
    ROUTES: { research: "/research" },
    scheduleResearchPreflight: () => { throw new Error("unauthorized retry scheduled preflight"); },
  });
  return {
    state,
    renders,
    requests,
    pending,
    runtime,
    setRoute(runID) { currentRouteRunID = runID; },
    resolve() { pending.resolve(error || response); },
    invalidate() { controller.cancel(); },
  };
}

const ownerVisibleDetail = {
  run: {
    run_id: "research-run-owner", status: "insufficient", failure: { code: "partial_evidence" },
    question: "公开问题", mode: "quick", requested_sources: ["knowledge"],
    package_id: "owner-agent", package_version: "1.0.0",
  },
};
const detailLoading = researchDetailRuntime({ response: ownerVisibleDetail });
const detailLoadingPromise = detailLoading.runtime.loadResearchRun("research-run-owner");
assert.equal(detailLoading.state.detailOwnerVisible, false, "starting a detail refresh must immediately revoke retry authority");
assert.equal(detailLoading.renders.length, 1, "starting a detail refresh must rerender and remove the retry control");
assert.equal(detailLoading.requests[0]?.options?.cache, "no-store", "owner-visible detail authorization must bypass browser caches");
detailLoading.resolve();
await detailLoadingPromise;
assert.equal(detailLoading.state.detailOwnerVisible, true, "an exact canonical owner-visible response may authorize retry");
assert.notEqual(detailLoading.runtime.researchLinkedRetryPlan(detailLoading.state.detail, detailLoading.state.detailOwnerVisible), null, "an exact owner-visible eligible detail may start retry");

const detailFailure = researchDetailRuntime({ error: new Error("research_run_not_found") });
const detailFailurePromise = detailFailure.runtime.loadResearchRun("research-run-owner");
detailFailure.resolve();
await detailFailurePromise;
assert.equal(detailFailure.state.detailOwnerVisible, false, "a failed detail load must not authorize retry");
assert.equal(detailFailure.runtime.researchLinkedRetryPlan(detailFailure.state.detail, detailFailure.state.detailOwnerVisible), null, "a failed detail load must not start retry");
assert.equal(detailFailure.runtime.startResearchLinkedRetry(), false, "the production retry action must reject a failed detail load");

const detailStale = researchDetailRuntime({ response: { run: { run_id: "research-run-owner" } } });
const detailStalePromise = detailStale.runtime.loadResearchRun("research-run-owner");
detailStale.invalidate();
detailStale.resolve();
await detailStalePromise;
assert.equal(detailStale.state.detailOwnerVisible, false, "a stale detail response must not authorize retry");
assert.equal(detailStale.runtime.researchLinkedRetryPlan(detailStale.state.detail, detailStale.state.detailOwnerVisible), null, "a stale detail response must not start retry");
assert.equal(detailStale.runtime.startResearchLinkedRetry(), false, "the production retry action must reject a stale detail response");

const detailRouteChanged = researchDetailRuntime({ response: { run: { run_id: "research-run-owner" } } });
const detailRouteChangedPromise = detailRouteChanged.runtime.loadResearchRun("research-run-owner");
detailRouteChanged.setRoute("research-run-other");
detailRouteChanged.resolve();
await detailRouteChangedPromise;
assert.equal(detailRouteChanged.state.detailOwnerVisible, false, "a response for a route that is no longer current must not authorize retry");
assert.equal(detailRouteChanged.runtime.researchLinkedRetryPlan(detailRouteChanged.state.detail, detailRouteChanged.state.detailOwnerVisible), null, "a route-changed response must not start retry");
assert.equal(detailRouteChanged.runtime.startResearchLinkedRetry(), false, "the production retry action must reject a route-changed response");

for (const responseRunID of ["research-run-other", "../research-run-owner", " research-run-owner "]) {
  const mismatch = researchDetailRuntime({ response: { run: { run_id: responseRunID } } });
  const mismatchPromise = mismatch.runtime.loadResearchRun("research-run-owner");
  mismatch.resolve();
  await mismatchPromise;
  assert.equal(mismatch.state.detailOwnerVisible, false, `response run_id ${JSON.stringify(responseRunID)} must not authorize retry`);
  assert.equal(mismatch.runtime.researchLinkedRetryPlan(mismatch.state.detail, mismatch.state.detailOwnerVisible), null, `response run_id ${JSON.stringify(responseRunID)} must not start retry`);
  assert.equal(mismatch.runtime.startResearchLinkedRetry(), false, `the production retry action must reject response run_id ${JSON.stringify(responseRunID)}`);
}

const invalidDetail = researchDetailRuntime({
  routeRunID: "../research-run-owner",
  response: { run: { run_id: "../research-run-owner" } },
});
const invalidDetailPromise = invalidDetail.runtime.loadResearchRun("../research-run-owner");
invalidDetail.resolve();
await invalidDetailPromise;
assert.equal(invalidDetail.requests.length, 0, "a non-canonical Run ID must be rejected before issuing a detail request");
assert.equal(invalidDetail.state.detailOwnerVisible, false, "a rejected Run ID must not retain owner-visible authority");

const canonicalRecentRunID = "research-run-abcdef0123456789abcdef0123456789";
const runIDStorageWrites = [];
const runIDStorage = new Map([[
  "kbase.research.recent-runs.v1",
  JSON.stringify([canonicalRecentRunID, 123, " ../escape ", "", "research/run", "x".repeat(129)]),
]]);
const runIDBoundary = loadFunctions([
  "isCanonicalResearchRunID", "researchRecentRunIDs", "rememberResearchRun", "buildResearchRunURL",
], {
  researchRecentRunsKey: "kbase.research.recent-runs.v1",
  ROUTES: { research: "/research" },
  window: { localStorage: {
    getItem: (key) => runIDStorage.get(key) ?? null,
    setItem: (key, value) => { runIDStorage.set(key, value); runIDStorageWrites.push([key, value]); },
  } },
});
assert.deepEqual(Array.from(runIDBoundary.researchRecentRunIDs()), [canonicalRecentRunID], "recent Run IDs must fail closed to canonical opaque identities");
for (const invalidRunID of [123, "", "   ", "../new-escape", "research/run", "x".repeat(129)]) {
  runIDBoundary.rememberResearchRun(invalidRunID);
  assert.equal(runIDBoundary.buildResearchRunURL(invalidRunID), "/research", `invalid Run ID ${JSON.stringify(invalidRunID)} must not become a navigation target`);
}
assert.deepEqual(runIDStorageWrites, [], "a non-canonical Run ID must never be written to localStorage");

const behavior = loadFunctions([
  "researchSourceOrder", "normalizeResearchDraft", "researchDraftFingerprint", "researchCandidateKey",
  "isCanonicalResearchRunID",
  "selectResearchPreflightCandidate", "applyResearchPreflightResponse", "researchRunStartBlockReason",
  "researchSelectedCandidate", "researchPreflightNotice", "researchSourceLabel", "buildResearchPreflightRequest", "buildResearchRunRequest", "prepareResearchRunSubmission",
  "clearResearchRunSubmission", "researchLinkedRetrySuggestion", "researchLinkedRetryPlan",
]);
const draft = behavior.normalizeResearchDraft({
  question: "  比较两类证据  ", mode: " auto ",
  sources: ["prior_runs", "knowledge", "knowledge"],
  subjectIDs: ["subject-b", "subject-a", "subject-a"],
});
assert.deepEqual(JSON.parse(JSON.stringify(draft)), {
  question: "比较两类证据", mode: "auto", sources: ["knowledge", "prior_runs"],
  subjectIDs: ["subject-a", "subject-b"],
}, "preflight and run creation should share one normalized draft");
const fingerprint = behavior.researchDraftFingerprint(draft);
const candidates = [
  { package_id: "agent-one", package_version: "1.2.0", readiness: "pass" },
  { package_id: "agent-two", package_version: "2.0.0", readiness: "warning" },
  { package_id: "agent-three", package_version: "3.0.0", readiness: "pass" },
  { package_id: "agent-four", package_version: "4.0.0", readiness: "pass" },
];
const state = { preflight: null, preflightFingerprint: "", selectedCandidateKey: "", loading: { preflight: true }, error: "" };
const activeController = { isCurrent: (sequence, requestFingerprint) => sequence === 2 && requestFingerprint === fingerprint };
assert.equal(
  behavior.applyResearchPreflightResponse(state, activeController, { sequence: 1, fingerprint }, { preflight_id: "stale", candidates }, draft),
  false, "a stale response should be ignored",
);
assert.equal(state.preflight, null, "a stale response must not replace the visible preflight");
assert.equal(
  behavior.applyResearchPreflightResponse(state, activeController, { sequence: 2, fingerprint }, { preflight_id: "fresh", status: "ready", expires_at: "2099-01-01T00:00:00Z", candidates, checks: [] }, draft),
  true, "the current response should be accepted",
);
assert.equal(state.preflight.candidates.length, 3, "the UI must cap recommendation cards at three");
assert.equal(state.selectedCandidateKey, "agent-one\u00001.2.0", "a new preflight should select the first candidate");
state.selectedCandidateKey = "agent-two\u00002.0.0";
state.runSubmission = { fingerprint: "stale-submission", idempotencyKey: "research-ui:stale" };
state.message = "旧的创建失败提示";
behavior.applyResearchPreflightResponse(state, activeController, { sequence: 2, fingerprint }, { preflight_id: "fresh-2", status: "ready", expires_at: "2099-01-01T00:00:00Z", candidates: candidates.slice(1), checks: [] }, draft);
assert.equal(state.selectedCandidateKey, "agent-two\u00002.0.0", "a manual selection should survive when the new candidate set still contains it");
assert.equal(state.runSubmission, null, "applying a new preflight should clear the prior submission identity");
assert.equal(state.message, "", "applying a fresh preflight should clear a stale run-creation message");

const mixedPreflight = {
  ...state.preflight,
  status: "ready",
  gaps: [{ code: "budget_insufficient" }],
  candidates: [
    { package_id: "agent-two", package_version: "2.0.0", readiness: "warning" },
    { package_id: "agent-blocked", package_version: "1.0.0", readiness: "blocked" },
  ],
};
assert.deepEqual(
  JSON.parse(JSON.stringify(behavior.researchPreflightNotice(mixedPreflight, "agent-two\u00002.0.0"))),
  { hardBlocked: false, gaps: [{ code: "budget_insufficient" }] },
  "mixed candidates and budget warnings should render as reminders when the selected Agent can run",
);
assert.equal(
  behavior.researchPreflightNotice(mixedPreflight, "agent-blocked\u00001.0.0").hardBlocked,
  true,
  "selecting a blocked Agent should render the hard-blocked treatment",
);
assert.equal(
  behavior.researchPreflightNotice({ ...mixedPreflight, status: "blocked" }, "agent-two\u00002.0.0").hardBlocked,
  true,
  "an overall blocked preflight should render the hard-blocked treatment",
);
assert.deepEqual(
  ["wechat_mp_article", "wcplus_wechat_article", "dedao_ebook", "dedao_course_article", "future_private_source"].map(behavior.researchSourceLabel),
  ["微信公众号文章", "微信公众号文章", "得到电子书", "得到课程文章", "其他受控来源"],
  "Retrieval Policy source types should use stable Chinese labels without exposing unknown internal values",
);

const selected = state.preflight.candidates.find((candidate) => behavior.researchCandidateKey(candidate) === state.selectedCandidateKey);
const beforePreflight = behavior.prepareResearchRunSubmission(draft, {
  preflight: null, preflightFingerprint: "", selectedCandidateKey: "", loading: { preflight: false },
}, Date.parse("2026-08-20T00:00:00Z"));
assert.equal(beforePreflight.payload, null, "run payload must not exist before a matching preflight");
assert.equal(behavior.researchRunStartBlockReason(draft, state.preflight, state.selectedCandidateKey, fingerprint, Date.parse("2026-08-20T00:00:00Z")), "", "a fresh ready selection should allow launch");
assert.notEqual(behavior.researchRunStartBlockReason(draft, state.preflight, state.selectedCandidateKey, fingerprint, Date.parse("2026-08-20T00:00:00Z"), true), "", "a loading preflight should disable launch");
assert.notEqual(behavior.researchRunStartBlockReason(draft, { ...state.preflight, expires_at: "2026-08-19T00:00:00Z" }, state.selectedCandidateKey, fingerprint, Date.parse("2026-08-20T00:00:00Z")), "", "an expired preflight should disable launch");
assert.notEqual(behavior.researchRunStartBlockReason(draft, { ...state.preflight, status: "blocked" }, state.selectedCandidateKey, fingerprint, Date.parse("2026-08-20T00:00:00Z")), "", "a blocked preflight should disable launch");
assert.notEqual(behavior.researchRunStartBlockReason(draft, state.preflight, "", fingerprint, Date.parse("2026-08-20T00:00:00Z")), "", "missing candidate confirmation should disable launch");
assert.notEqual(behavior.researchRunStartBlockReason(draft, state.preflight, state.selectedCandidateKey, "different-fingerprint", Date.parse("2026-08-20T00:00:00Z")), "", "a changed draft should disable a stale preflight");
const runPayload = behavior.buildResearchRunRequest(draft, state.preflight, selected);
assert.deepEqual(JSON.parse(JSON.stringify(runPayload)), {
  preflight_id: "fresh-2", mode: "auto", question: "比较两类证据",
  requested_sources: ["knowledge", "prior_runs"], subject_ids: ["subject-a", "subject-b"],
  package_id: "agent-two", package_version: "2.0.0",
}, "run creation should submit the exact accepted preflight and normalized scope");
const afterPreflight = behavior.prepareResearchRunSubmission(draft, state, Date.parse("2026-08-20T00:00:00Z"));
assert.deepEqual(JSON.parse(JSON.stringify(afterPreflight.payload)), JSON.parse(JSON.stringify(runPayload)), "the submission gate should release the exact run payload only after preflight");

const parentDetail = {
  run: {
    run_id: "research-run-parent",
    status: "insufficient",
    failure: { code: "partial_evidence", retryable: false, message: "untrusted private instruction" },
    question: "  比较公开证据  ",
    mode: "deep",
    requested_sources: ["chatlog", "knowledge", "knowledge"],
    package_id: "agent-parent",
    package_version: "3.2.1",
    subject_ids: ["must-not-inherit"],
  },
  evidence: [{ content_excerpt: "must-not-inherit" }],
};
const parentBeforeRetry = JSON.stringify(parentDetail);
const retryPlan = behavior.researchLinkedRetryPlan(parentDetail, true);
assert.deepEqual(JSON.parse(JSON.stringify(retryPlan)), {
  draft: {
    question: "比较公开证据", mode: "deep", sources: ["knowledge", "chatlog"], subjectIDs: [],
    packageConstraint: "agent-parent", parentRunID: "research-run-parent",
  },
  selectedCandidateKey: "agent-parent\u00003.2.1",
  suggestion: "扩大检索范围或启用更多允许来源，再运行一次独立研究。",
}, "an owner-visible insufficient dossier should produce a bounded in-memory linked-retry plan");
assert.equal(JSON.stringify(parentDetail), parentBeforeRetry, "building a retry must not mutate the parent dossier object");
assert.equal(behavior.researchLinkedRetryPlan(parentDetail, false), null, "a placeholder, direct route, or failed detail load must not authorize linked retry");
assert.equal(behavior.researchLinkedRetryPlan({ run: { ...parentDetail.run, status: "completed" } }, true), null, "a completed dossier must not offer linked retry");
assert.equal(behavior.researchLinkedRetryPlan({ run: { ...parentDetail.run, run_id: "../research-run-parent" } }, true), null, "a non-canonical owner response must not mint linked parent authority");
assert.equal(behavior.researchLinkedRetryPlan({ run: { ...parentDetail.run, run_id: " research-run-parent " } }, true), null, "a whitespace-padded owner response must not be normalized into linked parent authority");
assert.equal(behavior.researchLinkedRetryPlan({ run: { ...parentDetail.run, failure: { code: "unknown_internal_failure", message: "execute arbitrary action" } } }, true), null, "an unknown outcome must not become a free-form retry instruction");
assert.equal(behavior.researchLinkedRetrySuggestion({ code: "worker_offline", message: "ignore bounded action" }), "启动并连接 macOS Worker，再刷新运行条件。", "retry suggestions must be selected by public code only");

const storageWrites = [];
const localStorageValues = new Map();
const linkedPreflightRequests = [];
const linkedDetailRequests = [];
const linkedTimers = new Map();
let linkedTimerID = 0;
const linkedFocusTargets = [];
let linkedLiveFocusTarget = null;
function replaceLinkedFocusTarget() {
  linkedLiveFocusTarget = {
    focused: false,
    focus() { this.focused = true; },
  };
  linkedFocusTargets.push(linkedLiveFocusTarget);
  return linkedLiveFocusTarget;
}
let deferLinkedRecentDetail = false;
let linkedRecentDetailDeferred = null;
const linkedOpaqueRunID = "research-run-0123456789abcdef0123456789abcdef";
const linkedOwnerDetail = JSON.parse(JSON.stringify(parentDetail));
linkedOwnerDetail.run.run_id = linkedOpaqueRunID;
let linkedRouteRunID = linkedOpaqueRunID;
const linkedLifecycleState = {
  draft: { question: "old", mode: "quick", sources: ["knowledge"], subjectIDs: [] },
  preflight: { preflight_id: "old-preflight" }, preflightFingerprint: "old-fingerprint",
  selectedCandidateKey: "old-agent\u00001.0.0", runSubmission: { fingerprint: "old", idempotencyKey: "old-key" },
  parentRunID: "", detail: { run: { run_id: linkedOpaqueRunID } }, detailOwnerVisible: false,
  events: [], nextSequence: 0, error: "", message: "",
  loading: { preflight: false, detail: false, events: false, create: false },
};
const linkedLifecycleController = createResearchPreflightRequestController();
let linkedDetailSequence = 0;
const linkedDetailController = {
  begin() { linkedDetailSequence += 1; return { sequence: linkedDetailSequence, signal: { aborted: false } }; },
  isCurrent(candidate) { return candidate === linkedDetailSequence; },
  cancel() { linkedDetailSequence += 1; },
};
let linkedListSequence = 0;
const linkedListController = {
  begin() { linkedListSequence += 1; return { sequence: linkedListSequence, signal: { aborted: false } }; },
  isCurrent(candidate) { return candidate === linkedListSequence; },
  cancel() { linkedListSequence += 1; },
};
const linkedLifecycle = loadFunctions([
  "researchSourceOrder", "normalizeResearchDraft", "researchDraftFingerprint", "researchCandidateKey",
  "selectResearchPreflightCandidate", "applyResearchPreflightResponse", "researchLinkedRetrySuggestion",
  "researchLinkedRetryPlan", "clearResearchRunSubmission", "buildResearchPreflightRequest",
  "markResearchPreflightPending", "clearResearchPreflightTimers", "cancelResearchPreflightLifecycle",
  "scheduleResearchPreflight", "requestResearchPreflight", "isCanonicalResearchRunID", "researchDetailRequestOptions", "loadResearchRun",
  "researchRecentRunIDs", "rememberResearchRun", "loadRecentResearchRuns", "startResearchLinkedRetry",
  "focusResearchLinkedDraft",
], {
  researchState: linkedLifecycleState,
  researchPreflightRequestController: linkedLifecycleController,
  researchDetailRequestController: linkedDetailController,
  researchListRequestController: linkedListController,
  researchRecentRunsKey: "kbase.research.recent-runs.v1",
  researchPreflightDebounceMS: 600,
  researchPreflightDebounceTimer: null,
  researchPreflightExpiryTimer: null,
  clearTimeout: (timerID) => linkedTimers.delete(timerID),
  window: {
    history: { pushState: (_state, _title, url) => { linkedRouteRunID = url === "/research" ? "" : linkedRouteRunID; } },
    setTimeout: (callback) => { linkedTimerID += 1; linkedTimers.set(linkedTimerID, callback); return linkedTimerID; },
    localStorage: {
      getItem: (key) => localStorageValues.get(key) ?? null,
      setItem: (key, value) => { localStorageValues.set(key, value); storageWrites.push(["local", key, value]); },
    },
    sessionStorage: {
      getItem: () => null,
      setItem: (key, value) => storageWrites.push(["session", key, value]),
    },
  },
  document: {
    querySelector: (selector) => ["#research-question", "#research-create-form [name='question']"].includes(selector)
      ? linkedLiveFocusTarget
      : null,
  },
  ROUTES: { research: "/research" },
  getResearchRoute: () => ({ runID: linkedRouteRunID }),
  renderResearchWorkspace: replaceLinkedFocusTarget,
  renderResearchWorkspacePreservingDraftFocus: () => {},
  clearResearchRunDetail: () => { linkedLifecycleState.detail = null; linkedLifecycleState.detailOwnerVisible = false; },
  researchPreflightErrorMessage: () => "预检失败",
  scheduleResearchPreflightExpiry: () => {},
  apiFetch: async (url, options) => {
    if (url === `/api/research/runs/${linkedOpaqueRunID}`) {
      linkedDetailRequests.push({ url, options });
      if (deferLinkedRecentDetail && linkedRouteRunID === "") return linkedRecentDetailDeferred.promise;
      return JSON.parse(JSON.stringify(linkedOwnerDetail));
    }
    linkedPreflightRequests.push({ url, body: JSON.parse(options.body) });
    return {
      preflight_id: "linked-preflight-fresh", status: "ready", expires_at: "2099-01-01T00:00:00Z",
      parent_run_id: linkedOpaqueRunID,
      candidates: [{ package_id: "agent-parent", package_version: "3.2.1", readiness: "pass" }], checks: [],
    };
  },
});
await linkedLifecycle.loadResearchRun(linkedOpaqueRunID);
assert.equal(linkedLifecycleState.detailOwnerVisible, true, "the full lifecycle must first earn retry authority through production detail loading");
assert.equal(linkedLifecycle.startResearchLinkedRetry(), true, "the full linked-retry lifecycle should start from owner-visible detail");
assert.equal(linkedRouteRunID, "", "linked retry should navigate to the clean Research route without query state");
assert.equal(linkedLifecycleState.detail, null, "linked retry should leave the immutable parent detail view");
assert.equal(linkedLifecycleState.preflight, null, "linked retry should clear the parent preflight snapshot before scheduling a fresh one");
assert.equal(linkedLifecycleState.runSubmission, null, "linked retry should clear the parent submission identity");
assert.equal(linkedLifecycleState.parentRunID, linkedOpaqueRunID, "linked retry should retain the owner-verified opaque parent only in memory");
assert.equal(linkedLifecycleState.selectedCandidateKey, "agent-parent\u00003.2.1", "linked retry should prefer the parent's selected Package identity");
assert.deepEqual(JSON.parse(JSON.stringify(linkedLifecycleState.draft)), {
  question: "比较公开证据", mode: "deep", sources: ["knowledge", "chatlog"], subjectIDs: [],
  packageConstraint: "agent-parent", parentRunID: linkedOpaqueRunID,
}, "the fresh linked retry should inherit only the bounded public draft fields");
assert.equal(linkedTimers.size, 1, "the full linked-retry lifecycle should schedule a fresh preflight");
[...linkedTimers.values()][0]();
await Promise.resolve();
await Promise.resolve();
await Promise.resolve();
assert.equal(linkedLiveFocusTarget?.focused, true, "the final live question textarea must regain focus after recent loading and completion replace the DOM");
assert.deepEqual(linkedPreflightRequests, [{
  url: "/api/research/preflight",
  body: {
    mode: "deep", question: "比较公开证据", requested_sources: ["knowledge", "chatlog"], subject_ids: [],
    package_constraint: "agent-parent", parent_run_id: linkedOpaqueRunID,
  },
}], "the executed detail → retry path should issue one exact fresh parent-bound preflight");
assert.equal(linkedLifecycleState.preflight?.preflight_id, "linked-preflight-fresh", "the fresh linked preflight should replace the prior snapshot");
assert.equal(linkedDetailRequests.length, 2, "owner detail and recent-run refresh should each request the private Research detail once");
assert.ok(linkedDetailRequests.every((request) => request.options?.cache === "no-store"), "main and recent private Research detail requests must both bypass browser caches");
const localWrites = storageWrites.filter(([kind]) => kind === "local");
const sessionWrites = storageWrites.filter(([kind]) => kind === "session");
assert.equal(localWrites.length, 1, "production detail loading should only update the existing bounded recent-Run index once");
for (const [, key, rawValue] of localWrites) {
  assert.equal(key, "kbase.research.recent-runs.v1", "linked retry must not create a new localStorage namespace");
  const storedRunIDs = JSON.parse(rawValue);
  assert.deepEqual(storedRunIDs, [linkedOpaqueRunID], "the allowed localStorage value must contain only the canonical opaque recent Run ID");
  assert.ok(storedRunIDs.every(linkedLifecycle.isCanonicalResearchRunID), "every stored recent Run ID must remain canonical and opaque");
  for (const inherited of ["比较公开证据", "deep", "knowledge", "chatlog", "agent-parent", "question", "mode", "sources", "requested_sources", "package", "parent"]) {
    assert.equal(rawValue.includes(inherited), false, `recent Run storage must not serialize inherited ${inherited}`);
  }
}
assert.deepEqual(sessionWrites, [], "linked retry must not write sessionStorage");

linkedRouteRunID = linkedOpaqueRunID;
linkedLifecycleState.detail = JSON.parse(JSON.stringify(linkedOwnerDetail));
linkedLifecycleState.detailOwnerVisible = true;
deferLinkedRecentDetail = true;
linkedRecentDetailDeferred = deferred();
assert.equal(linkedLifecycle.startResearchLinkedRetry(), true, "a second owner-visible retry should exercise route-change focus protection");
linkedRouteRunID = "research-run-other";
const routeChangedFocusTarget = replaceLinkedFocusTarget();
linkedRecentDetailDeferred.resolve(JSON.parse(JSON.stringify(linkedOwnerDetail)));
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(routeChangedFocusTarget.focused, false, "recent completion must not steal focus after the user leaves the linked Research draft route");

linkedRouteRunID = linkedOpaqueRunID;
linkedLifecycleState.detail = JSON.parse(JSON.stringify(linkedOwnerDetail));
linkedLifecycleState.detailOwnerVisible = true;
linkedRecentDetailDeferred = deferred();
assert.equal(linkedLifecycle.startResearchLinkedRetry(), true, "a third owner-visible retry should exercise recent-detail failure focus recovery");
linkedRecentDetailDeferred.reject(new Error("recent detail unavailable"));
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(linkedLiveFocusTarget?.focused, true, "recent detail failure must still leave the final live linked-draft textarea focused");

const linkedPreflight = {
  preflight_id: "retry-preflight", status: "ready", expires_at: "2099-01-01T00:00:00Z",
  parent_run_id: "research-run-parent",
  candidates: [{ package_id: "agent-parent", package_version: "3.2.1", readiness: "pass" }], checks: [],
};
const linkedFingerprint = behavior.researchDraftFingerprint(linkedLifecycleState.draft);
const linkedPreflightPayload = behavior.buildResearchPreflightRequest(linkedLifecycleState.draft);
assert.deepEqual(JSON.parse(JSON.stringify(linkedPreflightPayload)), {
  mode: "deep", question: "比较公开证据", requested_sources: ["knowledge", "chatlog"], subject_ids: [],
  package_constraint: "agent-parent", parent_run_id: linkedOpaqueRunID,
}, "the fresh preflight must carry the owner-verified parent and preferred Package constraint");
const linkedRunPayload = behavior.buildResearchRunRequest(linkedLifecycleState.draft, linkedPreflight, linkedPreflight.candidates[0]);
assert.deepEqual(JSON.parse(JSON.stringify(linkedRunPayload)), {
  preflight_id: "retry-preflight", mode: "deep", question: "比较公开证据",
  requested_sources: ["knowledge", "chatlog"], subject_ids: [], package_id: "agent-parent", package_version: "3.2.1",
}, "the new Run must rely on the parent-bound preflight snapshot instead of accepting a second parent authority");
assert.equal(Object.hasOwn(linkedRunPayload, "parent_run_id"), false, "Run creation must not duplicate parent authority outside the preflight snapshot");

function createResearchRunRuntime(initialState, outcomes = [{ run: { run_id: "run-created" } }], submittedDraft = draft) {
  const requests = [];
  const navigations = [];
  const runtimeState = JSON.parse(JSON.stringify(initialState));
  const recentRunStorage = new Map();
  let requestIndex = 0;
  let idempotencyIndex = 0;
  const runtime = loadFunctions([
    "researchSourceOrder", "normalizeResearchDraft", "researchDraftFingerprint", "researchCandidateKey",
    "researchSelectedCandidate", "researchRunStartBlockReason", "buildResearchRunRequest",
    "prepareResearchRunSubmission", "researchRunSubmissionFingerprint", "researchRunIdempotencyKey",
    "clearResearchRunSubmission", "clearResearchLinkedRetryContext", "isCanonicalResearchRunID", "researchRecentRunIDs", "rememberResearchRun",
    "researchPreflightErrorMessage", "researchCreateErrorMessage",
    "postResearchRunSubmission", "createResearchRun",
  ], {
    researchState: runtimeState,
    researchRecentRunsKey: "kbase.research.recent-runs.v1",
    researchDraftFromForm: () => submittedDraft,
    apiFetch: async (url, options) => {
      requests.push({ url, method: options?.method, headers: { ...options?.headers }, body: JSON.parse(options?.body || "null") });
      const outcome = outcomes[Math.min(requestIndex, outcomes.length - 1)];
      requestIndex += 1;
      if (outcome instanceof Error) throw outcome;
      return outcome;
    },
    researchRequestID: () => { idempotencyIndex += 1; return `request-${idempotencyIndex}`; },
    renderResearchWorkspace: () => {},
    window: {
      history: { pushState: (_state, _title, url) => navigations.push(url) },
      localStorage: {
        getItem: (key) => recentRunStorage.get(key) ?? null,
        setItem: (key, value) => recentRunStorage.set(key, value),
      },
    },
    buildResearchRunURL: (runID) => `/research/${runID}`,
    clearResearchRunDetail: () => {},
    boot: async () => {},
    scheduleResearchPreflight: () => {},
    getResearchRoute: () => null,
  });
  return {
    requests,
    navigations,
    recentRunStorage,
    state: runtimeState,
    invoke: () => runtime.createResearchRun({ preventDefault() {}, currentTarget: {} }),
  };
}

async function executeCreateResearchRun(initialState) {
  const execution = createResearchRunRuntime(initialState);
  await execution.invoke();
  return execution;
}

const missingPreflightExecution = await executeCreateResearchRun({
  preflight: null, preflightFingerprint: "", selectedCandidateKey: "",
  loading: { preflight: false, create: false }, message: "", draft,
});
assert.equal(missingPreflightExecution.requests.filter(({ url }) => url === "/api/research/runs").length, 0, "submitting without a valid preflight must not POST a run");

const blockedExecution = await executeCreateResearchRun({
  preflight: { ...state.preflight, status: "blocked" }, preflightFingerprint: fingerprint,
  selectedCandidateKey: state.selectedCandidateKey, loading: { preflight: false, create: false }, message: "", draft,
});
assert.equal(blockedExecution.requests.filter(({ url }) => url === "/api/research/runs").length, 0, "submitting a blocked preflight must not POST a run");

const readyExecution = await executeCreateResearchRun({
  preflight: state.preflight, preflightFingerprint: fingerprint,
  selectedCandidateKey: state.selectedCandidateKey, loading: { preflight: false, create: false }, message: "", draft,
});
const runRequests = readyExecution.requests.filter(({ url }) => url === "/api/research/runs");
assert.equal(runRequests.length, 1, "a ready confirmed preflight should POST exactly one run");
assert.equal(runRequests[0].method, "POST", "the ready submission should use POST for run creation");
assert.deepEqual(JSON.parse(JSON.stringify(runRequests[0].body)), JSON.parse(JSON.stringify(runPayload)), "the executed run POST should preserve the exact preflight-bound normalized payload");

const linkedCreateExecution = createResearchRunRuntime({
  preflight: linkedPreflight, preflightFingerprint: linkedFingerprint,
  selectedCandidateKey: "agent-parent\u00003.2.1", parentRunID: linkedOpaqueRunID,
  loading: { preflight: false, create: false }, message: "", draft: linkedLifecycleState.draft,
}, [{ run: { run_id: "research-run-child", parent_run_id: linkedOpaqueRunID } }], linkedLifecycleState.draft);
await linkedCreateExecution.invoke();
assert.deepEqual(JSON.parse(JSON.stringify(linkedCreateExecution.requests[0].body)), JSON.parse(JSON.stringify(linkedRunPayload)), "owner detail → linked retry → fresh preflight should create one exact child Run request");
assert.equal(Object.hasOwn(linkedCreateExecution.requests[0].body, "parent_run_id"), false, "the child Run request should rely on the parent-bound preflight snapshot");
assert.equal(linkedCreateExecution.state.parentRunID, "", "a confirmed child Run should clear the inherited parent from future drafts");
assert.equal(linkedCreateExecution.state.draft.parentRunID, undefined, "a confirmed child Run should not leave a hidden parent in the next workspace draft");

const invalidCreateState = {
  preflight: linkedPreflight, preflightFingerprint: linkedFingerprint,
  selectedCandidateKey: "agent-parent\u00003.2.1", parentRunID: linkedOpaqueRunID,
  loading: { preflight: false, create: false }, message: "", draft: linkedLifecycleState.draft,
  runSubmission: { fingerprint: "preserve", idempotencyKey: "preserve-key" },
};
const invalidCreateExecution = createResearchRunRuntime(
  invalidCreateState,
  [{ run: { run_id: "../research-run-child" } }],
  linkedLifecycleState.draft,
);
await invalidCreateExecution.invoke();
assert.deepEqual(invalidCreateExecution.navigations, [], "a non-canonical creation response must not navigate");
assert.equal(invalidCreateExecution.recentRunStorage.size, 0, "a non-canonical creation response must not persist a Run ID");
assert.equal(invalidCreateExecution.state.parentRunID, linkedOpaqueRunID, "a rejected creation response must preserve the verified linked parent");
assert.equal(invalidCreateExecution.state.draft.parentRunID, linkedOpaqueRunID, "a rejected creation response must preserve the correct retry draft");
assert.notEqual(invalidCreateExecution.state.runSubmission, null, "a rejected creation response must preserve the submission identity for visible recovery");
assert.match(invalidCreateExecution.state.message, /运行 ID|响应/, "a non-canonical creation response must surface a perceptible response error");

const retryExecution = createResearchRunRuntime({
  preflight: state.preflight, preflightFingerprint: fingerprint, runSubmission: null,
  selectedCandidateKey: state.selectedCandidateKey, loading: { preflight: false, create: false }, message: "", draft,
}, [new Error("network response unknown"), { run: { run_id: "run-after-retry" } }, { run: { run_id: "run-new-preflight" } }]);
await retryExecution.invoke();
await retryExecution.invoke();
assert.equal(retryExecution.requests.length, 2, "an uncertain first result followed by retry should execute two run POST attempts");
assert.equal(retryExecution.requests[0].headers["Idempotency-Key"], retryExecution.requests[1].headers["Idempotency-Key"], "an uncertain retry of the same submission should reuse its idempotency key");
assert.deepEqual(retryExecution.requests[0].body, retryExecution.requests[1].body, "an uncertain retry should preserve the canonical run payload");
const confirmedKey = retryExecution.requests[1].headers["Idempotency-Key"];
assert.equal(retryExecution.state.runSubmission, null, "a confirmed successful run should clear the cached submission identity");
retryExecution.state.preflight = { ...state.preflight, preflight_id: "fresh-3" };
await retryExecution.invoke();
assert.notEqual(retryExecution.requests[2].headers["Idempotency-Key"], confirmedKey, "a new preflight payload should receive a new idempotency key");
assert.equal(retryExecution.requests[2].body.preflight_id, "fresh-3", "the new idempotency key should bind to the new preflight payload");

const conflictExecution = createResearchRunRuntime({
  preflight: state.preflight, preflightFingerprint: fingerprint, runSubmission: null,
  selectedCandidateKey: state.selectedCandidateKey, loading: { preflight: false, create: false }, message: "", draft,
}, [new Error("research_idempotency_conflict")]);
await conflictExecution.invoke();
assert.equal(conflictExecution.state.runSubmission, null, "an authoritative idempotency conflict should clear the cached submission identity");

const debounceMatch = js.match(/const researchPreflightDebounceMS = (\d+);/);
assert.equal(Number(debounceMatch?.[1]), 600, "preflight should wait 600ms after draft edits");
const preflightRequest = functionSource("requestResearchPreflight");
assert.ok(preflightRequest.includes('apiFetch("/api/research/preflight"'), "preflight should POST to the dedicated endpoint");
assert.ok(preflightRequest.includes("request.signal"), "preflight fetch should receive the abort signal");
assert.doesNotMatch(preflightRequest, /console\.|localStorage|URLSearchParams/, "preflight must not log or persist the research question outside the request body");
const createRun = functionSource("createResearchRun");
assert.ok(createRun.indexOf("prepareResearchRunSubmission") < createRun.indexOf("postResearchRunSubmission"), "run creation should enforce the tested preflight gate before calling the API");
assert.doesNotMatch(createRun, /apiFetch\(/, "run creation should cross the mockable run-submission boundary after its gate");
assert.ok(createRun.includes('["preflight_required",'), "a server-side missing-preflight response should invalidate and refresh the local snapshot");
assert.doesNotMatch(`${functionSource("startResearchLinkedRetry")}\n${functionSource("researchLinkedRetryPlan")}`, /localStorage|sessionStorage|URLSearchParams|console\./, "linked retry inheritance must remain memory-only and out of logs");
const boot = functionSource("boot");
assert.ok(boot.includes("cancelResearchPreflightLifecycle"), "route changes should clear the debounce timer and abort preflight requests");
assert.ok(boot.includes("clearResearchLinkedRetryContext"), "leaving Research should discard the memory-only parent authority");
assert.match(boot, /if \(researchRoute\.runID\) \{\s*clearResearchLinkedRetryContext/, "opening any Run detail should require a fresh owner-visible retry action before restoring parent authority");
assert.ok(boot.includes("synchronizeResearchRouteConstraint"), "Research route handling should use the tested constraint lifecycle");

const launchpad = js.match(/<form class="research-launchpad"[\s\S]*?<\/form>/)?.[0] || "";
assert.doesNotMatch(launchpad, /name="package_id"|name="package_version"/, "normal Research flow must not expose raw Package identity inputs");
assert.ok(launchpad.includes("问题与范围"), "the first launch stage should name question and scope");
const renderer = functionSource("renderResearchWorkspace");
for (const forbidden of ["思维链", "隐藏推理", "raw worker", "Worker 原始", "私有标识符", "完整聊天导出", "Token 输入"]) {
  assert.ok(!renderer.includes(forbidden), `Research renderer must not expose ${forbidden}`);
}
for (const semantic of ["role=\"tablist\"", "role=\"tab\"", "aria-live=\"polite\"", "aria-label=\"研究阶段\""]) {
  assert.ok(renderer.includes(semantic), `Research renderer should include ${semantic}`);
}
const preflightRenderer = `${functionSource("renderResearchPreflight")}\n${functionSource("renderResearchCandidateCards")}`;
assert.ok(preflightRenderer.includes("researchPreflightNotice"), "the visible preflight treatment should use the tested mixed-candidate notice classification");
for (const semantic of ["type=\"radio\"", "data-research-candidate", "aria-busy=", "aria-describedby=", "data-research-preflight-status"]) {
  assert.ok(preflightRenderer.includes(semantic), `Preflight renderer should include ${semantic}`);
}
for (const label of ["高匹配", "中匹配", "低匹配", "匹配理由", "知识范围", "更新时间", "评测", "来源", "运行准备", "运行前检查", "Worker", "预算", "通过", "提醒", "阻断", "创建 / 补全 Agent"]) {
  assert.ok(js.includes(label), `Research preflight should include the Chinese label ${label}`);
}
for (const code of [
  "no_eligible_package", "insufficient_coverage", "worker_offline", "source_not_allowed", "budget_insufficient",
  "invalid_research_preflight_request", "research_preflight_not_found", "research_preflight_expired",
  "research_preflight_conflict", "research_preflight_unavailable", "preflight_required", "preflight_expired",
  "package_changed", "readiness_changed", "preflight_blocked",
]) {
  assert.ok(js.includes(`\"${code}\"`), `Research preflight should map ${code} to bounded Chinese guidance`);
}
assert.ok(js.includes("researchPreflightErrorMessage"), "preflight errors should use a bounded presentation mapper");
assert.doesNotMatch(functionSource("researchPreflightErrorMessage"), /\|\| code|\? code|: code/, "unknown internal preflight errors must not be displayed raw");
assert.ok(js.includes("调整并重试"), "eligible failed or insufficient dossiers should offer a Chinese linked-retry action");
assert.ok(js.includes("data-research-linked-retry"), "the linked-retry action should be a bound in-app control, not a query-bearing link");

for (const className of [
  ".research-workspace", ".research-launchpad", ".research-preflight", ".research-agent-card", ".research-checks",
  ".research-stage-rail", ".research-dossier", ".research-scope-ledger", ".research-tablist",
  ".research-evidence-card", ".research-failure", ".research-failure__retry",
]) {
  assert.ok(css.includes(className), `Research styles should include ${className}`);
}
assert.ok(css.includes("grid-template-columns: minmax(180px, 0.55fr) minmax(0, 2fr) minmax(230px, 0.72fr)"), "desktop workspace should use a dense three-column dossier layout");
assert.ok(css.includes("overflow-wrap: anywhere"), "opaque identifiers must wrap instead of causing overflow");
assert.ok(css.includes("fieldset input:focus-visible + span"), "hidden scope controls should expose a visible keyboard focus ring");
assert.ok(css.includes(".research-failure__retry .button:focus-visible"), "linked retry should expose a visible keyboard focus ring");
const mobileLayout = await researchWorkspaceBrowserLayout(css);
assert.ok(mobileLayout.scrollWidth <= mobileLayout.clientWidth, `760px Research layout must not overflow horizontally (${mobileLayout.scrollWidth} > ${mobileLayout.clientWidth})`);
assert.equal(mobileLayout.activeName, "question", "the first keyboard Tab at 760px should reach the Research question");
assert.notEqual(mobileLayout.outlineStyle, "none", "the keyboard-focused Research question should keep a visible focus outline");
assert.notEqual(mobileLayout.outlineWidth, "0px", "the keyboard-focused Research question should keep a non-zero focus outline");
assert.ok(css.includes("prefers-reduced-motion: reduce"), "research motion should respect reduced motion");
assert.ok(html.includes("20260814-research-workspace"), "Research workspace should preserve its prior asset marker");
assert.ok(html.includes("20260815-research-model-output"), "Research failure guidance should preserve its prior asset marker");
assert.ok(html.includes("20260817-research-package-required"), "Required package migration should preserve the prior marker");
assert.ok(html.includes("20260820-research-agent-preflight"), "Research preflight should publish fresh app and stylesheet assets");

console.log("Research workspace smoke passed");
