import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");

for (const marker of [
  "evidenceAuditState",
  "getEvidenceAuditRoute",
  "buildEvidenceAuditURL",
  "renderEvidenceAuditWorkspace",
  "renderEvidenceAuditComposer",
  "renderEvidenceAuditReport",
  "renderEvidenceAuditClaim",
  "renderEvidenceAuditTrace",
  "renderEvidenceAuditContext",
  "renderEvidenceAuditTools",
  "loadEvidenceAudit",
  "createEvidenceAudit",
  "retryEvidenceAudit",
  "scheduleEvidenceAuditPoll",
  "cancelEvidenceAuditPoll",
  "loadProofroomPreview",
  "deliverEvidenceAuditToProofroom",
  "新建证据审计",
  "来源独立性",
  "证据时效",
  "预算上限",
  "审计结论",
  "证据卷宗",
  "局限与缺口",
  "复核行动",
  "Trace 与用量",
  "Proofroom 预览",
  "发送到 Proofroom",
]) {
  assert.ok(js.includes(marker), `evidence audit workspace should include ${marker}`);
}

const routeSource = js.match(/function getEvidenceAuditRoute\(\) \{([\s\S]*?)\n\}/)?.[1] || "";
assert.ok(routeSource.includes('"audits"'), "audit route should recognize the audits segment");
assert.ok(routeSource.includes('params.get("version")'), "audit route should retain the package version");
assert.ok(routeSource.includes("auditID"), "audit route should decode a stable audit ID");

const buildURLSource = js.match(/function buildEvidenceAuditURL\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(buildURLSource.includes("/audits/"), "audit URL should use the stable REST path");
assert.ok(buildURLSource.includes("URLSearchParams"), "audit URL should safely encode version query parameters");

const loadSource = js.match(/async function loadEvidenceAudit\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(loadSource.includes("/api/agent-audits/"), "audit detail should load from its dedicated endpoint");
assert.ok(loadSource.includes("/api/agent-traces/"), "audit detail should load authenticated Trace observability");
assert.ok(loadSource.includes("route.auditID"), "audit detail loading should be scoped to the current route");
assert.ok(loadSource.includes("audit?.package?.package_id"), "audit detail should reject cross-package route composition");
assert.ok(loadSource.includes("audit?.package?.version"), "audit detail should reject cross-version route composition");

const pollSource = js.match(/function scheduleEvidenceAuditPoll\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(pollSource.includes('"queued"'), "audit polling should include queued state");
assert.ok(pollSource.includes('"running"'), "audit polling should include running state");
assert.ok(pollSource.includes("auditID"), "audit polling should remain scoped to one audit");
assert.ok(js.includes("clearTimeout(evidenceAuditPollTimer)"), "audit polling should be cancellable");

const createSource = js.match(/async function createEvidenceAudit\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(createSource.includes("/api/agent-packages/"), "composer should use the package audit endpoint");
assert.ok(createSource.includes("/audits?"), "composer should pin the package version");
assert.ok(createSource.includes("idempotency_key"), "audit creation should carry structured idempotency");
assert.ok(createSource.includes("createRequestFingerprint"), "audit creation should reuse its idempotency key for the same request");
assert.ok(createSource.includes("selected_claims"), "audit creation should submit selected primary claims");
assert.ok(createSource.includes("history.replaceState"), "created audits should receive a stable browser URL");

const retrySource = js.match(/async function retryEvidenceAudit\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(retrySource.includes("/retry"), "manual retry should use the structured retry endpoint");
assert.ok(retrySource.includes("Idempotency-Key"), "manual retry should send an idempotency header");
assert.ok(js.includes("canRetryEvidenceAudit"), "retry visibility should be governed by an explicit state predicate");

const proofroomSource = js.match(/async function deliverEvidenceAuditToProofroom\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(proofroomSource.includes("window.confirm"), "Proofroom delivery should require explicit confirmation");
assert.ok(proofroomSource.includes("Idempotency-Key"), "Proofroom delivery should be idempotent");
assert.ok(proofroomSource.includes("proofroomDeliveryKey"), "Proofroom retries should reuse the preview-bound idempotency key");
assert.ok(proofroomSource.includes("/proofroom"), "Proofroom delivery should use the audit projection endpoint");
assert.ok(!proofroomSource.includes("loadProofroomPreview()"), "delivery should not silently trigger preview loading");
assert.ok(js.includes("renderProofroomPreviewClaim"), "Proofroom preview should expose the actual minimized claim payload");
assert.ok(js.includes("proofroomOperationSequence"), "Proofroom responses should be scoped to the active audit route");
assert.ok(js.includes("bookAgentLoadSequence"), "Agent package responses should be scoped to the active route");
assert.ok(js.includes('"model_outcome_unknown", "requires_manual_retry"'), "retry control should match backend retryable failure codes");
assert.ok(js.includes('["citation_id", evidence.citation_id]'), "citation links should preserve citation identity");
assert.ok(js.includes('evidenceLocator.get("citation_id")'), "knowledge pages should resolve direct citation links");

for (const state of ["queued", "running", "failed", "completed", "outcome_unknown", "rejected", "delivered"]) {
  assert.ok(js.includes(state), `audit UI should render ${state}`);
}

assert.ok(js.includes("renderSimpleMarkdown"), "audit model text should use the existing safe Markdown renderer");
assert.ok(js.includes("buildKnowledgePackageURL"), "audit citations should link to existing KBase entities");
assert.ok(js.includes('pkg.schema_version === "agent-package.v2"'), "only explicit v2 packages should expose audit composition");
assert.ok(js.includes("当前版本不提供证据审计"), "v1 packages should not receive a false audit composer");

const platformSource = js.match(/function renderBookAgentPlatform\([\s\S]*?\n\}\n\nfunction bindBookAgentPlatformEvents/)?.[0] || "";
assert.ok(platformSource.includes("isEvidenceAuditRoute"), "platform should explicitly separate evidence audit routes");
assert.ok(platformSource.includes("renderEvidenceAuditContext"), "audit routes should render compact package context");
assert.ok(platformSource.includes("renderEvidenceAuditTools"), "audit routes should group secondary package tools");
assert.ok(
  platformSource.indexOf("renderEvidenceAuditContext") < platformSource.indexOf("renderEvidenceAuditWorkspace"),
  "compact package context should precede the audit workspace",
);
assert.ok(
  platformSource.indexOf("renderEvidenceAuditWorkspace") < platformSource.indexOf("renderEvidenceAuditTools"),
  "Reader and search tools should follow the audit workspace",
);
assert.ok(
  platformSource.indexOf("renderEvidenceAuditTools") < platformSource.indexOf("renderGroundedConversation"),
  "grounded conversation should remain after the audit report and package tools",
);

const contextSource = js.match(/function renderEvidenceAuditContext\([\s\S]*?\n\}/)?.[0] || "";
for (const label of ["返回 Agent", "版本", "模型", "来源", "评测"]) {
  assert.ok(contextSource.includes(label), `compact context should expose ${label}`);
}
assert.ok(!contextSource.includes("book-agent__hero"), "compact audit context should not reuse the large package hero");
assert.ok(!contextSource.includes("book-agent__manifest"), "compact audit context should not render the long manifest metrics");

const toolsSource = js.match(/function renderEvidenceAuditTools\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(toolsSource.includes("<details"), "package tools should be collapsible");
assert.ok(!toolsSource.includes("<details open"), "package tools should be collapsed by default");
assert.ok(toolsSource.includes("包内工具"), "package tools should use the Chinese-first label");
assert.ok(toolsSource.includes("阅读器"), "Reader should use a Chinese-first label");
assert.ok(toolsSource.includes("包内检索"), "Grounded search should use a Chinese-first label");

for (const className of [
  ".evidence-audit",
  ".evidence-audit__context",
  ".evidence-audit__tools",
  ".evidence-audit__composer",
  ".evidence-audit__status",
  ".evidence-audit__report",
  ".evidence-audit__claim",
  ".evidence-audit__evidence-row",
  ".evidence-audit__proofroom",
  ".evidence-audit__proofroom-payload",
  ".evidence-audit__proofroom-claim",
  ".evidence-audit__trace",
]) {
  assert.ok(css.includes(className), `styles should include ${className}`);
}

assert.ok(css.includes("@media (max-width: 760px)"), "audit workspace should include a mobile layout");
assert.ok(css.includes("minmax(0, 1fr)"), "audit layout should allow columns to shrink without overflow");
assert.ok(css.includes("max-height: 140px"), "desktop audit context should enforce its compact height budget");
assert.ok(css.includes("max-height: 220px"), "mobile audit context should enforce its compact height budget");
assert.ok(css.includes(".evidence-audit__waiting > span"), "audit progress animation should honor reduced motion");
assert.ok(html.includes("20260724-evidence-audit-review"), "audit workspace should publish a fresh asset version");

console.log("evidence audit agent smoke passed");
