import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const helperPath = path.join(root, "evolution-console.js");
const appPath = path.join(root, "app.js");
const cssPath = path.join(root, "styles.css");
const htmlPath = path.join(root, "index.html");

assert.ok(fs.existsSync(helperPath), "evolution-console.js should exist");

const helperSource = fs.readFileSync(helperPath, "utf8");
const appSource = fs.readFileSync(appPath, "utf8");
const cssSource = fs.readFileSync(cssPath, "utf8");
const htmlSource = fs.readFileSync(htmlPath, "utf8");
const context = { globalThis: {}, URL, URLSearchParams };
vm.runInNewContext(helperSource, context, { filename: helperPath });
const helpers = context.globalThis.AgentEvolutionConsole;

assert.ok(helpers, "classic helper should expose globalThis.AgentEvolutionConsole");

const parsed = helpers.parseRoute("/agent-packages?view=inbox&risk=p0,p1&type=combined&run=run-123&tab=evidence&cursor=next-1&drawer=compiler");
assert.deepEqual(Array.from(parsed.risk), ["p0", "p1"]);
assert.equal(parsed.view, "inbox");
assert.equal(parsed.type, "combined");
assert.equal(parsed.run, "run-123");
assert.equal(parsed.tab, "evidence");
assert.equal(parsed.cursor, "next-1");
assert.equal(parsed.drawer, "compiler");
assert.equal(
  helpers.serializeRoute({ view: "inbox", risk: ["p0", "p1"], type: "combined", run: "run-123" }),
  "/agent-packages?view=inbox&risk=p0%2Cp1&type=combined&run=run-123",
);

const grouped = helpers.groupPackages([
  { package_id: "agent-b", version: "1.0.0", lifecycle_state: "published", published_at: "2026-08-09T00:00:00Z" },
  { package_id: "agent-a", version: "1.0.0", lifecycle_state: "superseded", published_at: "2026-08-08T00:00:00Z" },
  { package_id: "agent-a", version: "1.1.0", lifecycle_state: "published", published_at: "2026-08-10T00:00:00Z" },
]);
assert.deepEqual(Array.from(grouped, (agent) => agent.package_id), ["agent-a", "agent-b"]);
assert.equal(grouped[0].current.version, "1.1.0");
assert.deepEqual(Array.from(grouped[0].history, (pkg) => pkg.version), ["1.1.0", "1.0.0"]);
assert.equal(helpers.selectCurrentPublished(grouped[0].history).version, "1.1.0");

const sorted = helpers.sortRuns([
  { run_id: "p2-high", risk_level: "p2", priority_score: 99, updated_at: "2026-08-11T09:00:00Z" },
  { run_id: "p0-low", risk_level: "p0", priority_score: 1, updated_at: "2026-08-11T08:00:00Z" },
  { run_id: "p1-new", risk_level: "p1", priority_score: 20, updated_at: "2026-08-11T10:00:00Z" },
  { run_id: "p1-old", risk_level: "p1", priority_score: 20, updated_at: "2026-08-11T07:00:00Z" },
]);
assert.deepEqual(Array.from(sorted, (run) => run.run_id), ["p0-low", "p1-new", "p1-old", "p2-high"]);
assert.equal(helpers.scoreDelta(82.4, 86.1), 3.7);
assert.equal(helpers.scoreDelta(null, 86.1), null);

assert.equal(helpers.shouldHandleClick({ button: 0 }, { target: "", hasAttribute: () => false }), true);
assert.equal(helpers.shouldHandleClick({ button: 0, metaKey: true }, { target: "", hasAttribute: () => false }), false);
assert.equal(helpers.shouldHandleClick({ button: 1 }, { target: "", hasAttribute: () => false }), false);
assert.equal(helpers.shouldHandleClick({ button: 0 }, { target: "_blank", hasAttribute: () => false }), false);

for (const marker of [
  "Agent 演化中心",
  "待审批",
  "已阻断",
  "知识过期",
  "运行异常",
  "演化待办队列",
  "线上版本对比",
  "全部 Agent",
  "演化历史",
  "演化规则",
  "data-evolution-run-id",
  "data-agent-package-id",
  'aria-live="polite"',
]) {
  assert.ok(appSource.includes(marker), `Agent evolution UI should include ${marker}`);
}

for (const marker of [
  "evolutionConsoleState",
  "loadAgentEvolutionConsole",
  "renderAgentEvolutionConsole",
  "evolutionLoadSequence",
  "/api/evolution/overview",
  "/api/evolution/runs?",
  "Promise.allSettled",
  "history.pushState",
  "history.replaceState",
  "data-evolution-drawer",
  "data-evolution-detail-tab",
]) {
  assert.ok(appSource.includes(marker), `Agent evolution behavior should include ${marker}`);
}
assert.ok(appSource.includes("sequence !== evolutionLoadSequence"), "stale evolution responses must not replace newer route state");
const bookAgentLoader = appSource.match(/async function loadBookAgentPlatform\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(bookAgentLoader.includes("evolutionLoadSequence += 1"), "leaving the evolution index must invalidate in-flight responses");

assert.ok(htmlSource.indexOf("/evolution-console.js") < htmlSource.indexOf("/app.js"), "helper should load before app.js");
assert.ok(htmlSource.includes('<script src="/evolution-console.js?v='), "helper should be a classic versioned script");
assert.ok(!appSource.includes("<h1>${escapeHTML(viewLabel)}</h1>"), "old giant Agent Packages hero should be removed");

const indexRenderer = appSource.match(/function renderBookAgentPackageIndex\([\s\S]*?\n\}/)?.[0] || "";
assert.ok(indexRenderer.includes("renderAgentEvolutionConsole"), "package index should render the evolution console");
assert.ok(!indexRenderer.includes("${renderAgentCompiler()}"), "compiler must not remain permanently expanded");

for (const marker of [
  ".evolution-console",
  ".evolution-console__status-strip",
  ".evolution-console__workspace",
  ".evolution-console__queue",
  ".evolution-console__detail",
  ".evolution-console__fleet",
  ".evolution-console__drawer",
  ":focus-visible",
  "font-variant-numeric: tabular-nums",
  "prefers-reduced-motion: reduce",
]) {
  assert.ok(cssSource.includes(marker), `evolution console styles should include ${marker}`);
}

console.log("Agent evolution console smoke passed");
