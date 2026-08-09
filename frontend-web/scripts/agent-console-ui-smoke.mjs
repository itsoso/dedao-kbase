import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");

for (const marker of [
  "renderAgentConsole",
  "agentConsoleDisplayName",
  "agentEvaluationMetricLabel",
  "受控知识 Agent",
  "Agent 控制台",
  "运行就绪",
  "评测通过",
  "包契约",
  "阅读应用",
  "技术身份与版本凭据",
  "证据与运行边界",
  "Package、Release 与可信评测已载入",
]) {
  assert.ok(js.includes(marker), `Agent console should include ${marker}`);
}
assert.ok(!js.includes('bookAgentState.loading = "Loading Agent Packages"'), "Agent loading status should not fall back to English");

const consoleSource = js.match(/function renderAgentConsole\([\s\S]*?\n\}/)?.[0] || "";
const displayNameSource = js.match(/function agentConsoleDisplayName\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(displayNameSource.includes("split(/[：:]/)"), "long Chinese subtitles should not dominate the console title");
assert.ok(displayNameSource.includes("知识研究助手"), "missing book titles should still receive a Chinese display name");
const displayName = Function(`${displayNameSource}; return agentConsoleDisplayName;`)();
assert.equal(
  displayName("128942_人工智能注意力机制：体系、模型与算法剖析"),
  "人工智能注意力机制研究助手",
  "internal numeric book prefixes should not leak into the product title",
);
assert.ok(consoleSource.includes("release.book?.title"), "console title should derive from the pinned Chinese book title");
assert.ok(consoleSource.includes("pkg.package_id"), "console should retain the technical Agent ID");
assert.ok(consoleSource.includes('id="book-agent-search-form"'), "console should preserve the search form contract");
assert.ok(consoleSource.includes('id="book-agent-chat-form"') || consoleSource.includes("renderGroundedConversation"), "console should preserve grounded conversation");
assert.ok(consoleSource.includes("<details"), "low-frequency technical identity should be collapsible");
assert.ok(!consoleSource.includes("<details open"), "technical identity should be collapsed by default");

const evidenceSource = js.match(/function renderBookAgentEvidence\([\s\S]*?\n\}/)?.[0] || "";
for (const label of ["证据账本", "条结论", "条引用", "无引用 ID"]) {
  assert.ok(evidenceSource.includes(label), `Agent evidence ledger should use Chinese-first copy for ${label}`);
}

const platformSource = js.match(/function renderBookAgentPlatform\([\s\S]*?\n\}\n\nfunction bindAgentCompilerEvents/)?.[0] || "";
assert.ok(platformSource.includes('route.view === "agent"'), "platform should select the Agent-specific layout explicitly");
assert.ok(platformSource.includes('pkg.schema_version !== "agent-package.v2"'), "v2 evidence audit must keep its dedicated layout");
assert.ok(platformSource.includes("renderAgentConsole"), "v1 Agent routes should render the new console");

for (const className of [
  ".agent-console",
  ".agent-console__masthead",
  ".agent-console__workspace",
  ".agent-console__status-rail",
  ".agent-console__metric-grid",
  ".agent-console__technical",
]) {
  assert.ok(css.includes(className), `styles should include ${className}`);
}

const railSource = css.match(/\.agent-console__status-rail\s*\{([\s\S]*?)\}/)?.[1] || "";
assert.ok(railSource.includes("position: sticky"), "desktop status rail should remain visible while using the workspace");
assert.ok(css.includes("grid-template-columns: minmax(0, 2fr) minmax(280px, 1fr)"), "desktop console should use a bounded two-column layout");
assert.ok(css.includes("overflow-wrap: anywhere"), "long technical values should never force page overflow");

const mobileStart = css.lastIndexOf("@media (max-width: 760px)");
const mobileEnd = css.indexOf("@media (prefers-reduced-motion: reduce)", mobileStart);
const mobileSource = mobileStart >= 0 ? css.slice(mobileStart, mobileEnd >= 0 ? mobileEnd : undefined) : "";
assert.ok(mobileSource.includes(".agent-console__workspace"), "console should define a mobile workspace layout");
assert.ok(mobileSource.includes("grid-template-columns: minmax(0, 1fr)"), "mobile console should collapse to one column");
assert.ok(css.includes("prefers-reduced-motion: reduce"), "console animation should respect reduced motion");
assert.ok(html.includes("20260808-agent-console-zh"), "production should publish a fresh Agent console asset version");

console.log("Agent console UI smoke passed");
