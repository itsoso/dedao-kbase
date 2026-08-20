import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
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
  "requestResearchPreflight", "synchronizeResearchRouteConstraint",
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

const behavior = loadFunctions([
  "researchSourceOrder", "normalizeResearchDraft", "researchDraftFingerprint", "researchCandidateKey",
  "selectResearchPreflightCandidate", "applyResearchPreflightResponse", "researchRunStartBlockReason",
  "researchSelectedCandidate", "researchPreflightNotice", "researchSourceLabel", "buildResearchRunRequest", "prepareResearchRunSubmission",
  "clearResearchRunSubmission",
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

function createResearchRunRuntime(initialState, outcomes = [{ run: { run_id: "run-created" } }]) {
  const requests = [];
  const runtimeState = JSON.parse(JSON.stringify(initialState));
  let requestIndex = 0;
  let idempotencyIndex = 0;
  const runtime = loadFunctions([
    "researchSourceOrder", "normalizeResearchDraft", "researchDraftFingerprint", "researchCandidateKey",
    "researchSelectedCandidate", "researchRunStartBlockReason", "buildResearchRunRequest",
    "prepareResearchRunSubmission", "researchRunSubmissionFingerprint", "researchRunIdempotencyKey",
    "clearResearchRunSubmission", "postResearchRunSubmission", "createResearchRun",
  ], {
    researchState: runtimeState,
    researchDraftFromForm: () => draft,
    apiFetch: async (url, options) => {
      requests.push({ url, method: options?.method, headers: { ...options?.headers }, body: JSON.parse(options?.body || "null") });
      const outcome = outcomes[Math.min(requestIndex, outcomes.length - 1)];
      requestIndex += 1;
      if (outcome instanceof Error) throw outcome;
      return outcome;
    },
    researchRequestID: () => { idempotencyIndex += 1; return `request-${idempotencyIndex}`; },
    renderResearchWorkspace: () => {},
    rememberResearchRun: () => {},
    window: { history: { pushState: () => {} } },
    buildResearchRunURL: (runID) => `/research/${runID}`,
    clearResearchRunDetail: () => {},
    boot: async () => {},
    researchCreateErrorMessage: () => "创建失败",
    scheduleResearchPreflight: () => {},
    getResearchRoute: () => null,
  });
  return {
    requests,
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
const boot = functionSource("boot");
assert.ok(boot.includes("cancelResearchPreflightLifecycle"), "route changes should clear the debounce timer and abort preflight requests");
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

for (const className of [
  ".research-workspace", ".research-launchpad", ".research-preflight", ".research-agent-card", ".research-checks",
  ".research-stage-rail", ".research-dossier", ".research-scope-ledger", ".research-tablist",
  ".research-evidence-card", ".research-failure",
]) {
  assert.ok(css.includes(className), `Research styles should include ${className}`);
}
assert.ok(css.includes("grid-template-columns: minmax(180px, 0.55fr) minmax(0, 2fr) minmax(230px, 0.72fr)"), "desktop workspace should use a dense three-column dossier layout");
assert.ok(css.includes("overflow-wrap: anywhere"), "opaque identifiers must wrap instead of causing overflow");
assert.ok(css.includes("fieldset input:focus-visible + span"), "hidden scope controls should expose a visible keyboard focus ring");
const researchStyles = css.slice(css.indexOf("/* Research dossier"));
const mobile = researchStyles.slice(researchStyles.indexOf("@media (max-width: 760px)"));
for (const mobileClass of [".research-launchpad", ".research-preflight", ".research-agent-list", ".research-checks", ".research-dossier"]) {
  assert.ok(mobile.includes(mobileClass), `mobile workspace should collapse ${mobileClass}`);
}
assert.ok(mobile.includes("grid-template-columns: minmax(0, 1fr)"), "mobile research layout should use one bounded column");
assert.ok(css.includes("prefers-reduced-motion: reduce"), "research motion should respect reduced motion");
assert.ok(html.includes("20260814-research-workspace"), "Research workspace should preserve its prior asset marker");
assert.ok(html.includes("20260815-research-model-output"), "Research failure guidance should preserve its prior asset marker");
assert.ok(html.includes("20260817-research-package-required"), "Required package migration should preserve the prior marker");
assert.ok(html.includes("20260820-research-agent-preflight"), "Research preflight should publish fresh app and stylesheet assets");

console.log("Research workspace smoke passed");
