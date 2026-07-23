import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");

for (const marker of [
  "knowledgeOperationsState",
  "renderKnowledgeOperationsConsole",
  "loadKnowledgeOperationsConsole",
  "bindKnowledgeOperationsEvents",
  "ROUTES.operations",
  "/api/knowledge/operations",
  "/api/knowledge/operations/replay",
  "Release Status Center",
  "Health Evidence Review Workspace",
  "health_review_diagnostics",
  "health_review_queue",
  "renderKnowledgeOperationsHealthReviewDiagnostics",
  "renderKnowledgeOperationsHealthReviewQueue",
  "data-knowledge-health-diagnostic-action",
  "data-knowledge-health-review-action",
  "Failure Explanation",
  "data-knowledge-operations-replay",
]) {
  assert.ok(js.includes(marker), `app.js should include ${marker}`);
}

assert.ok(css.includes(".knowledge-operations"), "styles.css should include operations styles");
assert.ok(html.includes("20260723-operations-diagnostics"), "index.html should bust cache for operations diagnostics assets");
assert.ok(!js.includes("health_serving_promote</button>"), "UI must not expose Health serving promotion as a replay button");
assert.ok(!js.includes("publish</button>"), "UI must not expose publish as safe replay");
