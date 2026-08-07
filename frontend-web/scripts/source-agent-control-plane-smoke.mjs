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
  "confirmSourceAgentUpgrade",
  "bindSourceAgentManagementEvents",
]) {
  assert.ok(js.includes(marker), `app.js should include ${marker}`);
}

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
]) {
  assert.ok(js.includes(label), `management surface should render ${label}`);
}

for (const selector of [
  "data-source-agent-pause",
  "data-source-agent-resume",
  "data-source-agent-diagnose",
  "data-source-agent-upgrade",
  "data-source-agent-artifact",
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
assert.ok(js.includes("sourceAgentManagementSequence"), "stale management responses should be rejected");
assert.ok(js.includes("clearTimeout(sourceAgentManagementPollTimer)"), "management polling should be bounded and replaceable");
assert.ok(html.includes('name="color-scheme"'), "index should retain color scheme metadata");

assert.doesNotMatch(js, /source-agent-(?:custom-url|shell|script|environment|force-all)/i, "management UI must not expose arbitrary execution controls");
assert.doesNotMatch(js, /KBASE_SOURCE_AGENT_TOKEN/, "browser code must not receive the shared Worker token");

console.log("source agent control plane smoke passed");
