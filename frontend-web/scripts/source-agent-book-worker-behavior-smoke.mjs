import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = fs.readFileSync(path.join(root, "app.js"), "utf8");
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

vm.runInNewContext(`${source}\nglobalThis.__bookWorker = {
  isBookJobWorker,
  canRestartBookJobWorker,
  renderSourceAgentManagementCard,
  renderSourceAgentOverview,
  sourceAgentManagementStatus,
  sourceAgentSafeError,
  sourceAgentManagementState,
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
};`, context, { filename: "frontend-web/app.js" });

const ui = context.__bookWorker;
const bookWorker = {
  agent_id: "book-worker-1",
  worker_type: "book-job-worker",
  capabilities: ["book_jobs", "diagnose"],
  platform: "darwin",
  architecture: "arm64",
  current_run_id: "job-42",
  capability_health: {},
};

assert.equal(ui.isBookJobWorker(bookWorker), true);
assert.equal(ui.canRestartBookJobWorker(bookWorker), false);
let rendered = ui.renderSourceAgentManagementCard(bookWorker);
assert.match(rendered, /书籍任务 Worker/);
assert.match(rendered, /人工控制 · 独立运行/);
assert.match(rendered, /当前任务/);
assert.match(rendered, /job-42/);
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

let sameAgentRequests = 0;
let releaseSame;
const waitSame = new Promise((resolve) => { releaseSame = resolve; });
const firstSame = ui.runSourceAgentManagementAction("agent-a", () => { sameAgentRequests += 1; return waitSame; });
await Promise.resolve();
await ui.runSourceAgentManagementAction("agent-a", async () => { sameAgentRequests += 1; });
assert.equal(sameAgentRequests, 1, "same-agent in-flight actions must be deduplicated");
releaseSame();
await firstSame;

let releaseAuthority;
const authorityWait = new Promise((resolve) => { releaseAuthority = resolve; });
ui.setManagementLoader(() => authorityWait);
const authorityAction = ui.runSourceAgentManagementAction("agent-authority", async () => {});
await Promise.resolve();
assert.equal(ui.sourceAgentManagementState.pendingAgentIDs.has("agent-authority"), true, "pending lock must remain until authoritative refresh completes");
releaseAuthority();
await authorityAction;
ui.setManagementLoader(async () => {});

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

ui.sourceAgentManagementState.loading = "正在加载 Agent 状态";
ui.renderSourceAgentOverview();
assert.match(app.innerHTML, /role="status" aria-live="polite"/, "management async state should be announced");

console.log("source agent book worker behavior smoke passed");
