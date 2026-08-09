import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");

for (const marker of [
  'sourceAgents: "/sources/agents"',
  "sourceAgentManagementState",
  "renderSourceAgentOverview",
  "renderSourceAgentStatusSummary",
  "renderSourceAgentManagementCard",
  "loadSourceAgentManagement",
  "scheduleSourceAgentManagementPoll",
  "setSourceAgentDesiredState",
  "createSourceAgentDiagnostic",
  "createSourceAgentUpgrade",
  "isBookJobWorker",
  "canRestartBookJobWorker",
  "createSourceAgentRestart",
  "confirmSourceAgentRestart",
  "confirmSourceAgentUpgrade",
  "bindSourceAgentManagementEvents",
  "getSourceAgentDetailID",
  "sourceAgentDetailState",
  "renderSourceAgentDetail",
  "loadSourceAgentDetail",
  "sourceAgentDetailSequence",
  "sourceAgentRedactedDiagnostics",
  "sourceAgentManagementBusy",
  "sourceAgentHealthCodeLabel",
  "sourceAgentRequiresActionLabel",
]) {
  assert.ok(js.includes(marker), `app.js should include ${marker}`);
}

for (const marker of [
  'startsWith(`${ROUTES.sourceAgents}/`)',
  "decodeURIComponent",
  "encodeURIComponent(sourceAgentDetailState.agentID)",
  'apiFetch("/api/source-subscriptions")',
  'apiFetch("/api/source-sync/runs?limit=200")',
  "/api/source-sync/runs/${encodeURIComponent(run.id)}",
]) {
  assert.ok(js.includes(marker), `detail route should include ${marker}`);
}

for (const label of [
  "Agent 详情",
  "返回 Agent 总览",
  "绑定订阅",
  "最近运行",
  "命令时间线",
  "Outbox 统计",
  "脱敏诊断",
  "未找到该 Agent",
]) {
  assert.ok(js.includes(label), `detail route should render ${label}`);
}

for (const className of [
  ".source-agent-detail",
  ".source-agent-detail__hero",
  ".source-agent-detail__grid",
  ".source-agent-detail__timeline",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

assert.ok(js.includes("agent_id=${encodeURIComponent"), "source workspaces should preserve the stable Agent deep link");
assert.ok(js.includes("返回 Agent 详情"), "source-specific workspaces should link back to the Agent detail");
assert.doesNotMatch(js, /transport[_ -]?token|authorization.*diagnostic/i, "diagnostics must not render credentials");

for (const endpoint of [
  'apiFetch("/api/source-agents")',
  'apiFetch("/api/source-agent-artifacts?limit=100")',
  "/api/source-agents/${encodeURIComponent(agentID)}/desired-state",
  "/api/source-agents/${encodeURIComponent(agentID)}/commands",
]) {
  assert.ok(js.includes(endpoint), `management surface should call ${endpoint}`);
}

for (const label of [
  "Agent 管理",
  "在线",
  "需处理",
  "离线",
  "已暂停",
  "升级中",
  "操作中",
  "平台 / 架构",
  "版本 / 协议",
  "能力健康",
  "最后心跳",
  "当前运行",
  "当前命令",
  "Outbox / Dead letter",
  "暂停",
  "恢复",
  "诊断",
  "选择已批准版本",
  "微信工作台",
  "WC Plus 工作台",
  "书籍任务 Worker",
  "人工控制 · 独立运行",
  "当前任务",
  "受限重启",
  "任务中心",
]) {
  assert.ok(js.includes(label), `management surface should render ${label}`);
}

for (const selector of [
  "data-source-agent-pause",
  "data-source-agent-resume",
  "data-source-agent-diagnose",
  "data-source-agent-upgrade",
  "data-source-agent-artifact",
  "data-source-agent-restart",
]) {
  assert.ok(js.includes(selector), `management surface should include ${selector}`);
}

for (const className of [
  ".source-agents",
  ".source-agents__summary",
  ".source-agents__group",
  ".source-agent-card",
  ".source-agent-card__facts",
  ".source-agent-card__actions",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

assert.ok(js.includes('href="/sources/agents"'), "main navigation should link to the Agent overview");
assert.ok(js.includes("window.confirm"), "upgrade should require explicit operator confirmation");
assert.ok(js.includes('sourceAgentCommandEnvelope("restart")'), "restart commands should use the payload-free command envelope");
assert.doesNotMatch(js, /sourceAgentCommandEnvelope\("restart",\s*\{/m, "restart commands must not carry a payload");
assert.ok(js.includes('agent?.worker_type === "book-job-worker"'), "book presentation should be restricted to the book worker type");
assert.ok(js.includes('capabilities.includes("controlled_restart")'), "book restart should require the controlled_restart capability");
assert.ok(css.includes(".source-agent-card--book-worker"), "book worker should have distinct operational styling");
assert.ok(js.includes('bookWorker ? "" :'), "book workers should not expose unsupported pause or resume controls");
assert.ok(js.includes("commanding"), "non-upgrade commands should use an operation status rather than upgrading");
assert.ok(js.includes("sourceAgentManagementSequence"), "stale management responses should be rejected");
assert.ok(js.includes("pendingAgentIDs: new Set()"), "management actions should keep per-agent pending state");
assert.ok(js.includes("pendingAgentIDs.size"), "management polling should observe every pending agent");
assert.ok(js.includes('role="status" aria-live="polite"'), "management action results should be announced accessibly");
assert.ok(js.includes("clearTimeout(sourceAgentManagementPollTimer)"), "management polling should be bounded and replaceable");
assert.ok(html.includes('name="color-scheme"'), "index should retain color scheme metadata");

assert.doesNotMatch(js, /source-agent-(?:custom-url|shell|script|environment|force-all)/i, "management UI must not expose arbitrary execution controls");
assert.doesNotMatch(js, /KBASE_SOURCE_AGENT_TOKEN/, "browser code must not receive the shared Worker token");

console.log("source agent control plane smoke passed");
