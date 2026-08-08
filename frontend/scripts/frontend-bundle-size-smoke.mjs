import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const entryBudgetBytes = 2_000_000;
const baselineBytes = 5_499_050;

const mainSource = fs.readFileSync(path.join(root, "src", "main.ts"), "utf8");
assert.ok(
  !/import\s+\*\s+as\s+\w+\s+from\s+['"]@element-plus\/icons-vue['"]/.test(mainSource),
  "main.ts must not import the entire Element Plus icon library",
);

const routeSource = fs
  .readFileSync(path.join(root, "src", "router", "index.ts"), "utf8")
  .replace(/\/\*[\s\S]*?\*\//g, "")
  .replace(/^\s*\/\/.*$/gm, "");
const registryMatch = mainSource.match(/const elementPlusIcons\s*=\s*\{([\s\S]*?)\n\}/);
assert.ok(registryMatch, "main.ts should expose a bounded Element Plus icon registry");

const routeIcons = [...routeSource.matchAll(/\bicon\s*:\s*['"]([A-Za-z0-9]+)['"]/g)].map(
  ([, icon]) => icon,
);
for (const icon of new Set(routeIcons)) {
  assert.match(
    registryMatch[1],
    new RegExp(`\\b${icon}\\b`),
    `route icon ${icon} should be present in the bounded registry`,
  );
}

const html = fs.readFileSync(path.join(root, "dist", "index.html"), "utf8");
const entryMatch = html.match(/<script[^>]+src="([^"]+\.js)"/);
assert.ok(entryMatch, "dist/index.html should reference a JavaScript entry");

const entryPath = path.join(root, "dist", entryMatch[1].replace(/^\//, ""));
const entryBytes = fs.statSync(entryPath).size;
const reduction = ((baselineBytes - entryBytes) / baselineBytes) * 100;

assert.ok(
  entryBytes <= entryBudgetBytes,
  `entry bundle is ${entryBytes.toLocaleString()} bytes (${reduction.toFixed(1)}% below baseline); budget is ${entryBudgetBytes.toLocaleString()} bytes`,
);

console.log(
  `frontend bundle smoke passed: ${entryBytes.toLocaleString()} bytes, ${reduction.toFixed(1)}% below baseline`,
);
