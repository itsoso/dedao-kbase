import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");

for (const marker of [
  "sourceControlState",
  "renderSourceControlPlane",
  "renderSourceAgentList",
  "renderSourceSubscriptionList",
  "renderSourceRunHistory",
  "renderSourceRunDrawer",
  "bootstrapSourceControlPlane",
  "loadSourceControlPlane",
  "loadSourceRunDetail",
  "scheduleSourceControlPoll",
  "sourceControlDataSignature",
  "createSourceSubscription",
  "setSourceSubscriptionEnabled",
  "syncSourceSubscriptionNow",
  "handleSourceSyncConflict",
  "retrySourceRun",
  "cancelSourceRun",
  "bindSourceControlEvents",
  "sourceAgentIsOnline",
  "sourceRunIsActive",
  "sourceKnowledgeURL",
]) {
  assert.ok(js.includes(marker), `app.js should include ${marker}`);
}

for (const endpoint of [
  'apiFetch("/api/source-agents")',
  'apiFetch("/api/source-subscriptions")',
  'apiFetch("/api/source-sync/runs?limit=200")',
  "/api/source-subscriptions/${encodeURIComponent(subscription.id)}/enabled",
  "/api/source-subscriptions/${encodeURIComponent(subscriptionID)}/sync",
  "/api/source-sync/runs/${encodeURIComponent(runID)}",
  "/api/source-sync/runs/${encodeURIComponent(runID)}/retry",
  "/api/source-sync/runs/${encodeURIComponent(runID)}/cancel",
]) {
  assert.ok(js.includes(endpoint), `control plane should call ${endpoint}`);
}

for (const selector of [
  'id="source-subscription-form"',
  'name="sourceAccountKey"',
  'name="sourceAccount"',
  'name="sourceAgentID"',
  'name="sourceOperation"',
  'name="sourceScheduleMode"',
  'name="sourceIntervalSeconds"',
  "data-source-subscription-index",
  "data-source-subscription-enabled",
  "data-source-subscription-sync",
  'data-source-run-filter="${value}"',
  "data-source-run-detail",
  "data-source-run-retry",
  "data-source-run-cancel",
  "data-source-drawer-close",
  'id="wcplus-legacy-diagnostics"',
]) {
  assert.ok(js.includes(selector), `control plane should include ${selector}`);
}

for (const label of [
  "微信公众号采集器",
  "本地 Agent",
  "订阅",
  "新建订阅",
  "立即同步",
  "运行历史",
  "等待中",
  "运行中",
  "部分完成",
  "失败",
  "已完成",
  "运行详情",
  "导入知识",
  "本地 API 诊断",
]) {
  assert.ok(js.includes(label), `control plane should render ${label}`);
}

for (const className of [
  ".source-control",
  ".source-control__layout",
  ".source-control__sidebar",
  ".source-control__workspace",
  ".source-control__agent",
  ".source-control__subscription",
  ".source-control__run",
  ".source-control__filters",
  ".source-control__drawer",
  ".source-control__drawer-actions",
  ".source-control__legacy",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

assert.ok(js.includes("setTimeout") && js.includes("5000"), "active runs should use one bounded five-second poll");
for (const filter of ['["queued", "等待中"]', '["running", "运行中"]', '["partial", "部分完成"]', '["failed", "失败"]', '["succeeded", "已完成"]']) {
  assert.ok(js.includes(filter), `control plane should include run filter ${filter}`);
}
assert.ok(js.includes("clearTimeout(sourceControlPollTimer)"), "control plane should clear the previous poll before scheduling another");
assert.ok(js.includes('href="${sourceKnowledgeURL(item.target_book_id)}"'), "run items should expose direct REST knowledge links");
assert.ok(js.includes('return `/book-knowledge/${encodeURIComponent'), "knowledge links should use REST-style book paths");
assert.ok(js.includes("<details") && js.includes("wcplus-legacy-diagnostics"), "legacy WC Plus diagnostics should be collapsible");
assert.ok(js.includes("sourceControlState.legacyDiagnosticsOpen"), "legacy diagnostic open state should survive control-plane refreshes");
assert.ok(js.includes("sourceControlState.runFilter"), "run status filter should be authoritative state");
assert.ok(js.includes("sourceControlState.runDetail"), "run detail drawer should reload from the API");
assert.ok(js.includes("capability_health"), "agent rendering should prefer typed capability health");
assert.ok(js.includes("error.status = response.status"), "apiFetch errors should preserve HTTP status for conflict-aware UI");
assert.ok(js.includes("已有同步任务在进行中"), "sync conflicts should show a readable active-run message");
assert.ok(js.includes("renderResult && shouldRender"), "silent polling should render only when authoritative data changes");
assert.ok(js.includes("<span>启用</span>"), "subscription toggles should keep a stable accessible label");
const drawerSource = js.slice(js.indexOf("function renderSourceRunDrawer"), js.indexOf("function selectedSourceSubscription"));
assert.ok(drawerSource.includes("data-source-run-retry") && drawerSource.includes("data-source-run-cancel"), "run drawer should expose retry and cancel actions");
assert.ok(html.includes('name="color-scheme"'), "index should declare the supported color scheme");
assert.doesNotMatch(js, /KBASE_SOURCE_AGENT_TOKEN/, "the browser must never receive the dedicated source-agent token");

console.log("wcplus control plane smoke passed");
