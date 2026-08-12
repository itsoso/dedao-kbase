import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");

for (const marker of [
  "knowledgeCollections",
  "collectionWorkspaceState",
  "getKnowledgeCollectionID",
  "renderKnowledgeCollectionWorkspace",
  "loadKnowledgeCollectionWorkspace",
  "createCollectionForSubscription",
  "buildKnowledgeCollectionCandidate",
  "publishKnowledgeCollectionRelease",
  "createCollectionAgentDraft",
  "evaluateCollectionAgent",
  "publishCollectionAgent",
]) {
  assert.ok(js.includes(marker), `collection workspace should include ${marker}`);
}

for (const endpoint of [
  "/api/knowledge/collections",
  "/build`,",
  "/publish`,",
  "/api/controlled-collection-agent/draft",
  "/api/controlled-collection-agent/evaluate",
  "/api/controlled-collection-agent/publish",
]) {
  assert.ok(js.includes(endpoint), `collection workspace should call ${endpoint}`);
}

for (const label of [
  "公众号集合知识库",
  "采集健康",
  "成员差异",
  "排除项",
  "质量规则",
  "发布知识 Release",
  "生成 Agent 草稿",
  "运行可信评测",
  "发布 Agent",
  "回滚锚点",
  "查看成员知识包",
  "尚未发布",
]) {
  assert.ok(js.includes(label), `collection workspace should render ${label}`);
}

for (const selector of [
  "data-source-collection-open",
  "data-source-collection-create",
  "data-collection-build",
  "data-collection-release-publish",
  "data-collection-agent-draft",
  "data-collection-agent-evaluate",
  "data-collection-agent-publish",
]) {
  assert.ok(js.includes(selector), `collection workspace should include ${selector}`);
}

for (const className of [
  ".collection-workspace",
  ".collection-workspace__command-bar",
  ".collection-workspace__metrics",
  ".collection-workspace__pipeline",
  ".collection-workspace__member-table",
  ".collection-workspace__side-stack",
]) {
  assert.ok(css.includes(className), `collection workspace styles should include ${className}`);
}

const publishReleaseSource = js.match(/async function publishKnowledgeCollectionRelease\([\s\S]*?\n\}/)?.[0] || "";
const publishAgentSource = js.match(/async function publishCollectionAgent\([\s\S]*?\n\}/)?.[0] || "";
const loadSource = js.match(/async function loadKnowledgeCollectionWorkspace\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(publishReleaseSource.includes("/publish"), "Release publication must be an explicit action");
assert.ok(publishAgentSource.includes("/api/controlled-collection-agent/publish"), "Agent publication must be an explicit action");
assert.ok(publishAgentSource.includes("window.confirm"), "Agent publication must require browser confirmation");
assert.doesNotMatch(loadSource, /publishKnowledgeCollectionRelease|publishCollectionAgent/, "loading must never auto-publish");
assert.ok(js.includes("evaluation?.passed"), "Agent publish control should be gated by a passed evaluation");
assert.ok(js.includes("buildKnowledgePackageURL(member.book_id)"), "members should link to their knowledge packages");
assert.ok(js.includes("candidate-versus-published"), "workspace should expose candidate-versus-published state");

const workspaceStyle = css.match(/\.collection-workspace\s*\{([\s\S]*?)\}/)?.[1] || "";
assert.doesNotMatch(workspaceStyle, /min-height:\s*(?:[7-9]\d|\d{3,})vh/, "workspace must not use an oversized empty hero");
assert.ok(css.includes("grid-template-columns: minmax(0, 1fr) minmax(300px, .42fr)"), "desktop workspace should use a dense split layout");
assert.ok(css.includes("@media (max-width: 820px)"), "workspace should include a mobile layout");
assert.ok(html.includes("20260812-wechat-collection-agent"), "production assets should receive a fresh collection workspace version");

console.log("WeChat collection Agent workspace smoke passed");
