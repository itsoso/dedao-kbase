import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";
import { loadAppSource } from "./load-app-source.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = loadAppSource(root);
const app = { className: "", innerHTML: "", querySelector() { return null; }, querySelectorAll() { return []; } };
const context = {
  AbortController,
  Blob,
  FormData,
  Headers,
  Response,
  URL,
  URLSearchParams,
  clearTimeout,
  console,
  setTimeout,
  document: {
    body: { append() {}, classList: { add() {}, remove() {} }, querySelector() { return null; } },
    createElement() { return { click() {}, remove() {} }; },
    querySelector(selector) { return selector === "#app" ? app : null; },
    querySelectorAll() { return []; },
  },
  window: {
    addEventListener() {},
    clearTimeout,
    confirm() { return true; },
    setTimeout,
    localStorage: { getItem() { return null; }, removeItem() {}, setItem() {} },
    location: { pathname: "/unit-test", search: "", hash: "", origin: "https://kbase.example" },
  },
};

vm.runInNewContext(`${source}\nglobalThis.__originalManagementLoader = loadSourceAgentManagement;\nglobalThis.__bookWorker = {
  isBookJobWorker,
  canRestartBookJobWorker,
  renderSourceAgentManagementCard,
  renderSourceAgentOverview,
  sourceAgentManagementStatus,
  sourceAgentSafeError,
  sourceAgentManagementState,
  loadSourceAgentManagement,
  sourceAgentDetailState,
  sourceControlState,
  sourceAgentHealthCodeLabel,
  sourceAgentRequiresActionLabel,
  sourceAgentRedactedDiagnostics,
  renderSourceAgentDetail,
  renderSourceAgentList,
  runSourceAgentManagementAction,
  sourceAgentManagementBusy,
  createSourceAgentRestart,
  setApi(value) { apiFetch = value; },
  setManagementLoader(value) { loadSourceAgentManagement = value; },
  restoreManagementLoader() { loadSourceAgentManagement = globalThis.__originalManagementLoader; },
};`, context, { filename: "frontend-web/app.js" });

const ui = context.__bookWorker;
const bookWorker = {
  agent_id: "book-worker-1",
  worker_type: "book-job-worker",
  capabilities: ["book_jobs", "diagnose"],
  platform: "darwin",
  architecture: "arm64",
  current_run_id: "job-42",
  current_run_stage: "building_knowledge",
  capability_health: {},
};

assert.equal(ui.isBookJobWorker(bookWorker), true);
assert.equal(ui.canRestartBookJobWorker(bookWorker), false);
let rendered = ui.renderSourceAgentManagementCard(bookWorker);
assert.match(rendered, /书籍任务 Worker/);
assert.match(rendered, /人工控制 · 独立运行/);
assert.match(rendered, /当前任务/);
assert.match(rendered, /job-42/);
assert.match(rendered, /正在生成知识库/);
assert.match(rendered, /任务中心/);
assert.match(rendered, /data-source-agent-diagnose/);
assert.doesNotMatch(rendered, /data-source-agent-(?:pause|resume|upgrade|artifact|restart)/, "book workers must not expose unsupported controls");

const bookWorkerWithoutDiagnose = { ...bookWorker, capabilities: ["book_jobs"] };
rendered = ui.renderSourceAgentManagementCard(bookWorkerWithoutDiagnose);
assert.doesNotMatch(rendered, /data-source-agent-diagnose/, "book workers must not advertise diagnose without the capability");

const restartable = { ...bookWorker, capabilities: [...bookWorker.capabilities, "controlled_restart"] };
assert.equal(ui.canRestartBookJobWorker(restartable), true);
rendered = ui.renderSourceAgentManagementCard(restartable);
assert.match(rendered, /data-source-agent-restart/);
assert.doesNotMatch(rendered, /data-source-agent-(?:pause|resume|upgrade|artifact)/);

ui.sourceAgentManagementState.agents = [{ ...restartable, current_command_id: "restart-command" }];
ui.sourceAgentManagementState.commandsByAgent[restartable.agent_id] = [];
assert.equal(ui.sourceAgentManagementBusy(restartable.agent_id), true, "heartbeat current_command_id must keep controls locked when command history is unavailable");
rendered = ui.renderSourceAgentManagementCard(ui.sourceAgentManagementState.agents[0]);
assert.match(rendered, /data-source-agent-restart[^>]*disabled/, "current command snapshot should disable restart");
ui.sourceAgentManagementState.agents = [];

const wcplus = { ...bookWorker, agent_id: "wc-1", worker_type: "wcplus-worker", capabilities: ["diagnose"], current_run_id: "" };
rendered = ui.renderSourceAgentManagementCard(wcplus);
assert.match(rendered, /data-source-agent-upgrade/);
assert.match(rendered, /data-source-agent-pause/);
assert.doesNotMatch(rendered, /data-source-agent-restart/);

assert.equal(ui.sourceAgentManagementStatus(restartable, [{ type: "restart", state: "claimed" }]), "commanding");
assert.equal(ui.sourceAgentManagementStatus(wcplus, [{ type: "upgrade", state: "claimed" }]), "upgrading");
ui.sourceAgentManagementState.authorityPendingAgentIDs.add(restartable.agent_id);
assert.equal(ui.sourceAgentManagementStatus(restartable, []), "commanding", "awaiting-authority state should be visible as an active operation");
ui.sourceAgentManagementState.authorityPendingAgentIDs.delete(restartable.agent_id);
assert.equal(ui.sourceAgentSafeError("harmless-looking raw failure"), "Worker 报告异常，详细信息已隐藏；请运行诊断并人工处理。");
for (const [code, label] of Object.entries({
  login_required: "需要重新登录",
  vendor_blocked: "上游服务已阻止",
  dependency_unavailable: "依赖服务不可用",
  config_invalid: "配置需要修正",
  upgrade_required: "需要升级 Worker",
  throttled: "请求受到限流",
})) {
  assert.equal(ui.sourceAgentHealthCodeLabel(code), label);
}
assert.equal(ui.sourceAgentHealthCodeLabel("private_path=/secret"), "需要人工处理");
assert.equal(ui.sourceAgentHealthCodeLabel("constructor"), "需要人工处理", "prototype keys must not escape the health allowlist");
assert.equal(ui.sourceAgentRequiresActionLabel("open /secret/key"), "需要人工处理");

const unsafeAgent = {
  ...restartable,
  last_error: "private last error /secret/key",
  capability_health: { book_jobs: { healthy: false, code: "unknown_private_code", requires_action: "open /secret/key" } },
};
rendered = ui.renderSourceAgentManagementCard(unsafeAgent);
assert.doesNotMatch(rendered, /private|secret|unknown_private_code/i);
assert.match(rendered, /需要人工处理/);

ui.sourceAgentDetailState.notFound = false;
ui.sourceAgentDetailState.agentID = unsafeAgent.agent_id;
ui.sourceAgentDetailState.agent = unsafeAgent;
ui.sourceAgentDetailState.subscriptions = [];
ui.sourceAgentDetailState.runs = [];
ui.sourceAgentDetailState.commands = [];
ui.renderSourceAgentDetail();
assert.doesNotMatch(app.innerHTML, /private|secret|unknown_private_code/i, "agent detail must not expose raw health diagnostics");

ui.sourceControlState.agents = [unsafeAgent];
rendered = ui.renderSourceAgentList();
assert.doesNotMatch(rendered, /private|secret|unknown_private_code/i, "legacy source overview must use the same safe health copy");

ui.setManagementLoader(async () => {});
let releaseA;
let releaseB;
const waitA = new Promise((resolve) => { releaseA = resolve; });
const waitB = new Promise((resolve) => { releaseB = resolve; });
const actionA = ui.runSourceAgentManagementAction("agent-a", () => waitA);
const actionB = ui.runSourceAgentManagementAction("agent-b", () => waitB);
assert.equal(ui.sourceAgentManagementState.pendingAgentIDs.has("agent-a"), true);
assert.equal(ui.sourceAgentManagementState.pendingAgentIDs.has("agent-b"), true);
releaseA();
await actionA;
assert.equal(ui.sourceAgentManagementState.pendingAgentIDs.has("agent-a"), false);
assert.equal(ui.sourceAgentManagementState.pendingAgentIDs.has("agent-b"), true, "A completion must not unlock B");
releaseB();
await actionB;
ui.sourceAgentManagementState.authorityPendingAgentIDs.clear();

let sameAgentRequests = 0;
let releaseSame;
const waitSame = new Promise((resolve) => { releaseSame = resolve; });
const firstSame = ui.runSourceAgentManagementAction("agent-a", () => { sameAgentRequests += 1; return waitSame; });
await Promise.resolve();
await ui.runSourceAgentManagementAction("agent-a", async () => { sameAgentRequests += 1; });
assert.equal(sameAgentRequests, 1, "same-agent in-flight actions must be deduplicated");
releaseSame();
await firstSame;
ui.sourceAgentManagementState.authorityPendingAgentIDs.clear();

let releaseAuthority;
const authorityWait = new Promise((resolve) => { releaseAuthority = resolve; });
ui.setManagementLoader(() => authorityWait);
const authorityAction = ui.runSourceAgentManagementAction("agent-authority", async () => {});
await Promise.resolve();
assert.equal(ui.sourceAgentManagementState.pendingAgentIDs.has("agent-authority"), true, "pending lock must remain until authoritative refresh completes");
releaseAuthority();
await authorityAction;
ui.setManagementLoader(async () => {});
ui.sourceAgentManagementState.authorityPendingAgentIDs.clear();

ui.sourceAgentManagementState.commandsByAgent[restartable.agent_id] = [{ type: "restart", state: "queued" }];
let queuedRestartRequests = 0;
await ui.runSourceAgentManagementAction(restartable.agent_id, async () => { queuedRestartRequests += 1; });
assert.equal(queuedRestartRequests, 0, "an active authoritative command must keep the agent locked after POST completion");

ui.sourceAgentManagementState.agents = [restartable];
ui.sourceAgentManagementState.commandsByAgent[restartable.agent_id] = [];
let restartPostCount = 0;
ui.setApi(async (requestPath) => {
  assert.match(requestPath, /\/commands$/);
  restartPostCount += 1;
  ui.sourceAgentManagementState.commandsByAgent[restartable.agent_id] = [{ type: "restart", state: "queued" }];
  return { command: { type: "restart", state: "queued" } };
});
await ui.createSourceAgentRestart(restartable.agent_id);
await ui.createSourceAgentRestart(restartable.agent_id);
assert.equal(restartPostCount, 1, "a queued restart must block a second restart POST");

ui.restoreManagementLoader();
const authorityAgent = { ...restartable, agent_id: "authority-worker", current_command_id: "" };
ui.sourceAgentManagementState.agents = [authorityAgent];
ui.sourceAgentManagementState.commandsByAgent = { [authorityAgent.agent_id]: [] };
ui.sourceAgentManagementState.pendingAgentIDs.clear();
ui.sourceAgentManagementState.authorityPendingAgentIDs.clear();
context.window.location.pathname = "/sources/agents";
ui.setApi(async (requestPath, options = {}) => {
  if (requestPath.endsWith("/commands") && options.method === "POST") {
    return { command: { id: "restart-authority", type: "restart", state: "queued" } };
  }
  if (requestPath === "/api/source-agents") throw new Error("authority refresh unavailable /private/detail");
  throw new Error(`unexpected request ${requestPath}`);
});
await ui.createSourceAgentRestart(authorityAgent.agent_id);
assert.equal(ui.sourceAgentManagementState.pendingAgentIDs.has(authorityAgent.agent_id), false);
assert.equal(ui.sourceAgentManagementState.authorityPendingAgentIDs.has(authorityAgent.agent_id), true, "failed authority refresh must preserve the awaiting-authority lock");
assert.equal(ui.sourceAgentManagementBusy(authorityAgent.agent_id), true);
assert.equal(ui.sourceAgentManagementState.commandsByAgent[authorityAgent.agent_id][0].id, "restart-authority", "POST command response should immediately become a local authoritative lock");
assert.match(app.innerHTML, /data-source-agent-restart[^>]*disabled/);
assert.match(app.innerHTML, /操作已提交，但权威状态暂未确认；系统将自动重试。/, "failed authority refresh should announce safe retry guidance");

ui.setApi(async (requestPath) => {
  if (requestPath === "/api/source-agents") return { agents: [authorityAgent] };
  if (requestPath === "/api/source-agent-artifacts?limit=100") return { artifacts: [] };
  if (requestPath.includes("/commands?limit=10")) return { commands: [{ id: "restart-authority", type: "restart", state: "claimed" }] };
  throw new Error(`unexpected request ${requestPath}`);
});
assert.equal(await ui.loadSourceAgentManagement({ silent: true, preserveActionOutcome: true }), true);
assert.equal(ui.sourceAgentManagementState.authorityPendingAgentIDs.has(authorityAgent.agent_id), false, "one successful authority refresh should clear its awaiting-authority lock");
assert.equal(ui.sourceAgentManagementBusy(authorityAgent.agent_id), true, "the active authoritative command should keep controls locked");

const staleInjectedAgent = { ...restartable, agent_id: "stale-injected-worker", current_command_id: "" };
ui.sourceAgentManagementState.agents = [staleInjectedAgent];
ui.sourceAgentManagementState.commandsByAgent = {
  [staleInjectedAgent.agent_id]: [{ id: "local-stale-command", type: "restart", state: "queued" }],
};
ui.sourceAgentManagementState.authorityPendingAgentIDs.add(staleInjectedAgent.agent_id);
ui.setApi(async (requestPath) => {
  if (requestPath === "/api/source-agents") return { agents: [staleInjectedAgent] };
  if (requestPath === "/api/source-agent-artifacts?limit=100") return { artifacts: [] };
  if (requestPath.includes("/commands?limit=10")) return { commands: [] };
  throw new Error(`unexpected request ${requestPath}`);
});
assert.equal(await ui.loadSourceAgentManagement({ silent: true, preserveActionOutcome: true }), true);
assert.equal(ui.sourceAgentManagementState.authorityPendingAgentIDs.has(staleInjectedAgent.agent_id), false, "successful authority refresh should release the awaiting-authority lock");
assert.deepEqual(ui.sourceAgentManagementState.commandsByAgent[staleInjectedAgent.agent_id], [], "successful authority list should remove an injected command that is absent upstream");
assert.equal(ui.sourceAgentManagementBusy(staleInjectedAgent.agent_id), false, "an absent injected command must not leave the agent permanently busy");
ui.sourceAgentManagementState.agents = [{ ...staleInjectedAgent, current_command_id: "upstream-command" }];
assert.equal(ui.sourceAgentManagementBusy(staleInjectedAgent.agent_id), true, "current_command_id should remain an independent authoritative lock");

const navigationAgent = { ...restartable, agent_id: "navigation-worker", current_command_id: "" };
ui.sourceAgentManagementState.agents = [navigationAgent];
ui.sourceAgentManagementState.commandsByAgent = { [navigationAgent.agent_id]: [] };
let releaseNavigationPost;
const navigationPost = new Promise((resolve) => { releaseNavigationPost = resolve; });
let navigationGETs = 0;
ui.setApi(async (requestPath, options = {}) => {
  if (requestPath.endsWith("/commands") && options.method === "POST") return navigationPost;
  navigationGETs += 1;
  throw new Error(`unexpected management load ${requestPath}`);
});
context.window.location.pathname = "/sources/agents";
const navigatingAction = ui.createSourceAgentRestart(navigationAgent.agent_id);
await Promise.resolve();
context.window.location.pathname = "/jobs";
app.innerHTML = "jobs-page-sentinel";
releaseNavigationPost({ command: { id: "restart-navigation", type: "restart", state: "queued" } });
await navigatingAction;
assert.equal(app.innerHTML, "jobs-page-sentinel", "POST completion after navigation must not repaint the Agent page");
assert.equal(navigationGETs, 0, "POST completion after navigation must not start a management load");

const failingAgent = { ...restartable, agent_id: "failing-worker", current_command_id: "" };
ui.sourceAgentManagementState.agents = [failingAgent];
ui.sourceAgentManagementState.commandsByAgent = { [failingAgent.agent_id]: [] };
ui.sourceAgentManagementState.pendingAgentIDs.clear();
ui.sourceAgentManagementState.authorityPendingAgentIDs.clear();
context.window.location.pathname = "/sources/agents";
ui.setApi(async (requestPath, options = {}) => {
  if (requestPath.endsWith("/commands") && options.method === "POST") throw new Error("private upstream failure /secret/key");
  throw new Error(`unexpected request ${requestPath}`);
});
await ui.createSourceAgentRestart(failingAgent.agent_id);
assert.equal(ui.sourceAgentManagementState.pendingAgentIDs.has(failingAgent.agent_id), false);
assert.equal(ui.sourceAgentManagementState.authorityPendingAgentIDs.has(failingAgent.agent_id), false);
const liveStatus = app.innerHTML.match(/role="status" aria-live="polite">([^<]+)/)?.[1] || "";
assert.equal(liveStatus, "操作提交失败，请稍后重试或运行诊断。", "POST failure should remain visible as safe live status copy");
assert.doesNotMatch(liveStatus, /个 Agent|private|secret/, "POST failure must not be replaced by the Agent count summary or raw error");

ui.sourceAgentManagementState.loading = "正在加载 Agent 状态";
ui.renderSourceAgentOverview();
assert.match(app.innerHTML, /role="status" aria-live="polite"/, "management async state should be announced");

console.log("source agent book worker behavior smoke passed");
