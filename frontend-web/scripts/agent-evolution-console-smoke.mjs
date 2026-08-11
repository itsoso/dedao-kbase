import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";
import { fixtureBrowserClientID, isValidBrowserClientID } from "./agent-evolution-console-fixture.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const helperPath = path.join(root, "evolution-console.js");
const appPath = path.join(root, "app.js");
const cssPath = path.join(root, "styles.css");
const htmlPath = path.join(root, "index.html");

assert.ok(fs.existsSync(helperPath), "evolution-console.js should exist");

const helperSource = fs.readFileSync(helperPath, "utf8");
const appSource = fs.readFileSync(appPath, "utf8");
const cssSource = fs.readFileSync(cssPath, "utf8");
const htmlSource = fs.readFileSync(htmlPath, "utf8");
const context = { globalThis: {}, URL, URLSearchParams };
vm.runInNewContext(helperSource, context, { filename: helperPath });
const helpers = context.globalThis.AgentEvolutionConsole;

assert.ok(helpers, "classic helper should expose globalThis.AgentEvolutionConsole");
assert.equal(isValidBrowserClientID(fixtureBrowserClientID), true, "fixture fallback must satisfy production browser client ID constraints");
assert.equal(fixtureBrowserClientID.length >= 16, true);

const parsed = helpers.parseRoute("/agent-packages?view=inbox&risk=p0,p1&type=combined&run=run-123&tab=evidence&cursor=next-1&drawer=compiler");
assert.deepEqual(Array.from(parsed.risk), ["p0", "p1"]);
assert.equal(parsed.view, "inbox");
assert.equal(parsed.type, "combined");
assert.equal(parsed.run, "run-123");
assert.equal(parsed.tab, "evidence");
assert.equal(parsed.cursor, "next-1");
assert.equal(parsed.drawer, "compiler");
assert.equal(
  helpers.serializeRoute({ view: "inbox", risk: ["p0", "p1"], type: "combined", run: "run-123" }),
  "/agent-packages?view=inbox&risk=p0%2Cp1&type=combined&run=run-123",
);
assert.equal(helpers.normalizeRisk("critical"), "p0");
assert.equal(helpers.normalizeRisk("high"), "p1");
assert.equal(helpers.normalizeRisk("medium"), "p2");
assert.equal(helpers.normalizeRisk("low"), "p3");
assert.equal(helpers.normalizeRisk("P0"), "p0");
assert.equal(helpers.normalizeRisk("unknown"), "");
assert.equal(helpers.riskLabel("critical"), "P0 紧急");
assert.deepEqual(
  Array.from(helpers.expandRiskQuery(["p0", "p1", "p0"])),
  ["p0", "critical", "p1", "high"],
);
assert.equal(
  helpers.expandRiskQuery(parsed.risk).join(","),
  "p0,critical,p1,high",
  "URL p0,p1 should query both canonical and signal severity values in a fixed order",
);

const inboxQuery = helpers.buildRunsQuery({ ...parsed, view: "inbox" });
assert.equal(
  inboxQuery.get("status"),
  "detected,triaged,generating,evaluating,awaiting_approval,approved,publishing,observing,blocked,failed",
  "inbox must request only actionable and open statuses",
);
assert.equal(inboxQuery.get("risk"), "p0,critical,p1,high");
assert.equal(inboxQuery.get("type"), "combined");
assert.equal(inboxQuery.get("cursor"), "next-1");
const historyQuery = helpers.buildRunsQuery({ ...parsed, view: "history" });
assert.equal(historyQuery.get("status"), "completed,rejected,superseded,rolled_back");
assert.equal(helpers.buildRunsQuery({ ...parsed, view: "fleet" }), null, "fleet should not consume run-list pagination");
assert.equal(helpers.buildRunsQuery({ ...parsed, view: "rules" }), null, "rules should not request runs");
assert.equal(helpers.detailTabForView("history"), "audit");
assert.equal(helpers.detailTabForView("inbox"), "comparison");
const historyRoutePatch = helpers.routePatchForView("history");
assert.equal(historyRoutePatch.run, "", "switching views must not retain a run from a different status scope");
assert.equal(historyRoutePatch.tab, "audit");
assert.equal(historyRoutePatch.cursor, "");
const historyRunPatch = helpers.navigationPatchForDataset(
  { evolutionRunId: "run-completed" },
  { route: { ...helpers.routeDefaults, view: "history" } },
);
assert.equal(historyRunPatch.run, "run-completed");
assert.equal(historyRunPatch.tab, "audit", "history row interception must use evolution state, not the outer package route");
const viewNavigationPatch = helpers.navigationPatchForDataset(
  { evolutionView: "history" },
  { route: { ...helpers.routeDefaults, view: "inbox", run: "run-open-a" } },
);
assert.equal(viewNavigationPatch.view, "history");
assert.equal(viewNavigationPatch.run, "");
assert.equal(viewNavigationPatch.tab, "audit");
assert.equal(helpers.navigationPatchForDataset({ evolutionDetailTab: "evidence" }, { route: parsed }).tab, "evidence");
const cursorPatch = helpers.navigationPatchForDataset({ evolutionCursor: "page-2" }, { route: parsed });
assert.equal(cursorPatch.cursor, "page-2");
assert.equal(cursorPatch.run, "");

const grouped = helpers.groupPackages([
  { package_id: "agent-b", version: "1.0.0", lifecycle_state: "published", published_at: "2026-08-09T00:00:00Z" },
  { package_id: "agent-a", version: "1.0.0", lifecycle_state: "superseded", published_at: "2026-08-08T00:00:00Z" },
  { package_id: "agent-a", version: "1.1.0", lifecycle_state: "published", published_at: "2026-08-10T00:00:00Z" },
]);
assert.deepEqual(Array.from(grouped, (agent) => agent.package_id), ["agent-a", "agent-b"]);
assert.equal(grouped[0].current.version, "1.1.0");
assert.deepEqual(Array.from(grouped[0].history, (pkg) => pkg.version), ["1.1.0", "1.0.0"]);
assert.equal(helpers.selectCurrentPublished(grouped[0].history).version, "1.1.0");

const sorted = helpers.sortRuns([
  { run_id: "critical-signal", risk_level: "critical", priority_score: 0, updated_at: "2026-08-11T06:00:00Z" },
  { run_id: "p2-high", risk_level: "p2", priority_score: 99, updated_at: "2026-08-11T09:00:00Z" },
  { run_id: "p0-low", risk_level: "p0", priority_score: 1, updated_at: "2026-08-11T08:00:00Z" },
  { run_id: "p1-new", risk_level: "p1", priority_score: 20, updated_at: "2026-08-11T10:00:00Z" },
  { run_id: "p1-old", risk_level: "p1", priority_score: 20, updated_at: "2026-08-11T07:00:00Z" },
  { run_id: "p3-run", risk_level: "p3", priority_score: 100, updated_at: "2026-08-11T11:00:00Z" },
]);
assert.deepEqual(Array.from(sorted, (run) => run.run_id), ["p0-low", "critical-signal", "p1-new", "p1-old", "p2-high", "p3-run"]);
assert.ok(sorted.findIndex((run) => run.run_id === "critical-signal") < sorted.findIndex((run) => run.risk_level === "p3"));
assert.equal(helpers.scoreDelta(82.4, 86.1), 3.7);
assert.equal(helpers.scoreDelta(null, 86.1), null);

assert.equal(helpers.shouldHandleClick({ button: 0 }, { target: "", hasAttribute: () => false }), true);
assert.equal(helpers.shouldHandleClick({ button: 0, metaKey: true }, { target: "", hasAttribute: () => false }), false);
assert.equal(helpers.shouldHandleClick({ button: 1 }, { target: "", hasAttribute: () => false }), false);
assert.equal(helpers.shouldHandleClick({ button: 0 }, { target: "_blank", hasAttribute: () => false }), false);

let showModalCalls = 0;
let closeFocused = false;
const modalDialog = {
  open: false,
  showModal() { showModalCalls += 1; this.open = true; },
  setAttribute() {},
  querySelector() { return { focus() { closeFocused = true; } }; },
};
assert.equal(helpers.activateDialog(modalDialog, []), "native");
assert.equal(showModalCalls, 1);
assert.equal(closeFocused, true);

const fallbackAttributes = new Map();
const fallbackBackground = { inert: false };
const fallbackDialog = {
  open: false,
  setAttribute(name, value) { fallbackAttributes.set(name, value); if (name === "open") this.open = true; },
  querySelector() { return { focus() {} }; },
};
assert.equal(helpers.activateDialog(fallbackDialog, [fallbackBackground]), "fallback");
assert.equal(fallbackDialog.open, true);
assert.equal(fallbackAttributes.get("aria-modal"), "true");
assert.equal(fallbackAttributes.get("data-evolution-modal-mode"), "fallback");
assert.equal(fallbackBackground.inert, true);

const dismissListeners = new Map();
const dismissDialog = {
  addEventListener(type, listener) { dismissListeners.set(type, listener); },
  removeEventListener(type) { dismissListeners.delete(type); },
};
let dismissCalls = 0;
let escapePrevented = false;
const unbindDismiss = helpers.bindDialogDismiss(dismissDialog, () => { dismissCalls += 1; });
dismissListeners.get("keydown")({ key: "Enter", preventDefault() {} });
assert.equal(dismissCalls, 0, "non-Escape keys should not dismiss the dialog");
dismissListeners.get("keydown")({ key: "Escape", preventDefault() { escapePrevented = true; } });
assert.equal(escapePrevented, true);
assert.equal(dismissCalls, 1, "Escape from any focused dialog control should dismiss once");
dismissListeners.get("cancel")({ preventDefault() {} });
assert.equal(dismissCalls, 1, "native cancel after keydown should not dismiss twice");
unbindDismiss();
assert.equal(dismissListeners.size, 0);

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

const compilerController = helpers.createLatestRequestController();
const firstCompilerRequest = deferred();
const secondCompilerRequest = deferred();
const compilerState = { loading: false, data: [], error: "" };
const compilerHandlers = {
  onStart() { compilerState.loading = true; compilerState.error = ""; },
  onSuccess(value) { compilerState.data = value; },
  onError(error) { compilerState.error = error.message; },
  onFinish() { compilerState.loading = false; },
};
const firstCompilerLoad = compilerController.run(() => firstCompilerRequest.promise, compilerHandlers);
assert.equal(compilerState.loading, true);
compilerController.cancel();
assert.equal(compilerState.loading, false, "closing the compiler must clear its own loading immediately");
const secondCompilerLoad = compilerController.run(() => secondCompilerRequest.promise, compilerHandlers);
secondCompilerRequest.resolve(["release-new"]);
await secondCompilerLoad;
firstCompilerRequest.reject(new Error("stale failure"));
await firstCompilerLoad;
assert.deepEqual(compilerState.data, ["release-new"]);
assert.equal(compilerState.error, "", "stale compiler requests cannot overwrite the latest result");
assert.equal(compilerState.loading, false);
await assert.rejects(
  helpers.createLatestRequestController().run(() => Promise.reject(new Error("current failure"))),
  /current failure/,
  "an active request without an error handler must remain observable",
);

const detailController = helpers.createLatestRequestController();
const detailA = deferred();
const detailB = deferred();
const detailState = {
  route: { run: "" },
  selectedDetail: null,
  events: [],
  loading: { overview: false, runs: false, detail: false },
  errors: { overview: "", runs: "", detail: "", events: "" },
};
const loadDetail = (route, request) => detailController.run(() => request.promise, {
  onStart() { helpers.beginEvolutionRouteState(detailState, route, { loadsRuns: true }); },
  onSuccess(value) { detailState.selectedDetail = value; },
  onFinish() { detailState.loading.detail = false; },
});
const loadA = loadDetail({ ...helpers.routeDefaults, run: "run-a" }, detailA);
detailState.selectedDetail = { run: { run_id: "run-a", package_id: "agent-a" } };
detailState.events = [{ event_id: "event-a" }];
const loadB = loadDetail({ ...helpers.routeDefaults, run: "run-b" }, detailB);
assert.equal(detailState.selectedDetail, null, "B loading state must not retain A detail");
assert.equal(detailState.events.length, 0, "B loading state must not retain A events");
assert.equal(detailState.loading.detail, true);
detailB.resolve({ run: { run_id: "run-b", package_id: "agent-b" } });
await loadB;
detailA.resolve({ run: { run_id: "run-a", package_id: "agent-a-stale" } });
await loadA;
assert.equal(detailState.selectedDetail.run.run_id, "run-b", "late A response must not replace B");

detailState.selectedDetail = { run: { run_id: "run-b" } };
helpers.beginEvolutionRouteState(
  detailState,
  { ...helpers.routeDefaults, run: "run-b", tab: "audit" },
  { loadsRuns: true },
);
assert.equal(detailState.selectedDetail.run.run_id, "run-b", "same-run tab changes should preserve detail");

const hangingPageRequest = deferred();
const pageController = helpers.createLatestRequestController();
const hangingLoad = pageController.run(() => hangingPageRequest.promise);
let triggerFocused = false;
let restoreFlag = true;
const restored = helpers.restoreDismissedDialogFocus({
  shouldRestore: restoreFlag,
  dialog: null,
  trigger: { focus() { triggerFocused = true; } },
  schedule(callback) { callback(); },
});
if (restored) restoreFlag = false;
assert.equal(triggerFocused, true, "drawer dismissal must restore focus while page APIs are still pending");
assert.equal(restoreFlag, false);
pageController.cancel();
hangingPageRequest.resolve(null);
await hangingLoad;

for (const marker of [
  "Agent 演化中心",
  "待审批",
  "已阻断",
  "知识过期",
  "运行异常",
  "演化待办队列",
  "线上版本对比",
  "全部 Agent",
  "演化历史",
  "演化规则",
  "data-evolution-run-id",
  "data-agent-package-id",
  'aria-live="polite"',
]) {
  assert.ok(appSource.includes(marker), `Agent evolution UI should include ${marker}`);
}

for (const marker of [
  "evolutionConsoleState",
  "loadAgentEvolutionConsole",
  "renderAgentEvolutionConsole",
  "/api/evolution/overview",
  "/api/evolution/runs?",
  "Promise.allSettled",
  "history.pushState",
  "history.replaceState",
  "data-evolution-drawer",
  "data-evolution-detail-tab",
]) {
  assert.ok(appSource.includes(marker), `Agent evolution behavior should include ${marker}`);
}
assert.ok(!appSource.includes('is-${escapeAttribute(run.risk_level'), "raw signal severities must not leak into risk CSS classes");

assert.ok(htmlSource.indexOf("/evolution-console.js") < htmlSource.indexOf("/app.js"), "helper should load before app.js");
assert.ok(htmlSource.includes('<script src="/evolution-console.js?v='), "helper should be a classic versioned script");
assert.ok(!appSource.includes("<h1>${escapeHTML(viewLabel)}</h1>"), "old giant Agent Packages hero should be removed");

const indexRenderer = appSource.match(/function renderBookAgentPackageIndex\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(indexRenderer.includes("renderAgentEvolutionConsole"), "package index should render the evolution console");
assert.ok(!indexRenderer.includes("${renderAgentCompiler()}"), "compiler must not remain permanently expanded");
const evolutionRenderer = appSource.match(/function renderAgentEvolutionConsole\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(!evolutionRenderer.includes("aria-labelledby=\"agent-compiler-title\" open"), "compiler dialog must not be statically open");

for (const marker of [
  ".evolution-console",
  ".evolution-console__status-strip",
  ".evolution-console__workspace",
  ".evolution-console__queue",
  ".evolution-console__detail",
  ".evolution-console__fleet",
  ".evolution-console__drawer",
  ":focus-visible",
  "font-variant-numeric: tabular-nums",
  "prefers-reduced-motion: reduce",
]) {
  assert.ok(cssSource.includes(marker), `evolution console styles should include ${marker}`);
}

console.log("Agent evolution console smoke passed");
