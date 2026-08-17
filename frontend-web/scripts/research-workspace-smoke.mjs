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

const launchpad = js.match(/<form class="research-launchpad"[\s\S]*?<\/form>/)?.[0] || "";
assert.match(launchpad, /name="package_id"[^>]*required/, "Agent Package should be required before creating a run");
assert.match(launchpad, /name="package_version"[^>]*required/, "Agent Package version should be required before creating a run");
assert.ok(launchpad.includes("ROUTES.agentPackages"), "Research workspace should link to Agent Package management for selection");
assert.ok(js.includes("validateResearchCreateDraft"), "Research creation should validate the package contract before calling the API");
assert.ok(js.includes("Agent Package 和版本为必填"), "Missing package guidance should be actionable Chinese text");
assert.ok(js.includes("研究问题为必填"), "Custom validation should preserve the required research question contract");
assert.ok(js.includes('"invalid_research_request"'), "Research creation should map invalid request errors");
assert.ok(js.includes('"research_package_not_eligible"'), "Research creation should map ineligible package errors");
const createRun = js.match(/async function createResearchRun[\s\S]*?\n\}/)?.[0] || "";
assert.ok(createRun.indexOf("validateResearchCreateDraft") < createRun.indexOf('apiFetch("/api/research/runs"'), "Research validation should run before the create request");

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
assert.ok(html.includes("20260817-research-package-required"), "Required package validation should publish with a fresh app asset");

console.log("Research workspace smoke passed");
