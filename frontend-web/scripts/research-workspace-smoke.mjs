import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");

for (const marker of [
  'research: "/research"',
  "getResearchRoute",
  "renderResearchWorkspace",
  "loadResearchRun",
  "pollResearchEvents",
  "createResearchRun",
  "cancelResearchRun",
  "confirmResearchIdentity",
  "研究工作台",
  "快速检索",
  "自动判断",
  "深度研究",
  "知识库",
  "本地聊天记录",
  "历史研究",
  "开始研究",
  "取消运行",
  "检索范围",
  "引用范围",
  "证据",
  "时间线",
  "冲突",
  "研究报告",
  "确认身份",
  "模型输出格式无效",
]) {
  assert.ok(js.includes(marker), `Research workspace should include ${marker}`);
}

for (const controller of [
  "researchListRequestController",
  "researchDetailRequestController",
  "researchEventsRequestController",
]) {
  assert.ok(js.includes(controller), `Research workspace should keep an independent ${controller}`);
}
assert.ok(js.includes('query.set("after"'), "event polling should resume from an after cursor");
assert.ok(js.includes("researchTerminalStatuses"), "terminal runs should stop event polling");
assert.ok(js.includes("clearResearchRunDetail"), "route changes should clear stale run detail immediately");
assert.ok(js.includes('headers: { "Idempotency-Key"'), "run creation should use an idempotency key");
assert.ok(js.includes('capabilities.includes("deep_research")'), "Agent console should expose Research only for opted-in packages");

const renderer = js.match(/function renderResearchWorkspace\([\s\S]*?\n\}/)?.[0] || "";
for (const forbidden of ["思维链", "隐藏推理", "raw worker", "Worker 原始", "私有标识符", "完整聊天导出", "Token 输入"]) {
  assert.ok(!renderer.includes(forbidden), `Research renderer must not expose ${forbidden}`);
}
for (const semantic of ["role=\"tablist\"", "role=\"tab\"", "aria-live=\"polite\"", "aria-label=\"研究阶段\""]) {
  assert.ok(renderer.includes(semantic), `Research renderer should include ${semantic}`);
}

for (const className of [
  ".research-workspace",
  ".research-launchpad",
  ".research-stage-rail",
  ".research-dossier",
  ".research-scope-ledger",
  ".research-tablist",
  ".research-evidence-card",
  ".research-failure",
]) {
  assert.ok(css.includes(className), `Research styles should include ${className}`);
}
assert.ok(css.includes("grid-template-columns: minmax(180px, 0.55fr) minmax(0, 2fr) minmax(230px, 0.72fr)"), "desktop workspace should use a dense three-column dossier layout");
assert.ok(css.includes("overflow-wrap: anywhere"), "opaque identifiers must wrap instead of causing overflow");
const researchStyles = css.slice(css.indexOf("/* Research dossier"));
const mobile = researchStyles.slice(researchStyles.indexOf("@media (max-width: 760px)"));
assert.ok(mobile.includes(".research-dossier"), "mobile workspace should collapse the dossier grid");
assert.ok(mobile.includes("grid-template-columns: minmax(0, 1fr)"), "mobile research layout should use one bounded column");
assert.ok(css.includes("prefers-reduced-motion: reduce"), "research motion should respect reduced motion");
assert.ok(html.includes("20260814-research-workspace"), "Research workspace should publish fresh assets");
assert.ok(html.includes("20260815-research-model-output"), "Research failure guidance should publish with a fresh app asset");

console.log("Research workspace smoke passed");
