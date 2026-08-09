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
  sourceAgentManagementStatus,
  sourceAgentSafeError,
  sourceAgentManagementState,
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

const restartable = { ...bookWorker, capabilities: [...bookWorker.capabilities, "controlled_restart"] };
assert.equal(ui.canRestartBookJobWorker(restartable), true);
rendered = ui.renderSourceAgentManagementCard(restartable);
assert.match(rendered, /data-source-agent-restart/);
assert.doesNotMatch(rendered, /data-source-agent-(?:pause|resume|upgrade|artifact)/);

const wcplus = { ...bookWorker, agent_id: "wc-1", worker_type: "wcplus-worker", capabilities: ["diagnose"], current_run_id: "" };
rendered = ui.renderSourceAgentManagementCard(wcplus);
assert.match(rendered, /data-source-agent-upgrade/);
assert.match(rendered, /data-source-agent-pause/);
assert.doesNotMatch(rendered, /data-source-agent-restart/);

assert.equal(ui.sourceAgentManagementStatus(restartable, [{ type: "restart", state: "claimed" }]), "commanding");
assert.equal(ui.sourceAgentManagementStatus(wcplus, [{ type: "upgrade", state: "claimed" }]), "upgrading");
assert.equal(ui.sourceAgentSafeError("Bearer token exposed in worker diagnostics"), "Worker 报告异常，敏感技术细节已隐藏；请先运行诊断。");

console.log("source agent book worker behavior smoke passed");
