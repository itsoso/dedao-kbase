import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const manifest = JSON.parse(fs.readFileSync(path.join(root, "package.json"), "utf8"));
const lock = fs.readFileSync(path.join(root, "package-lock.json"), "utf8");

assert.equal(
  manifest.engines?.node,
  "^22.18.0 || >=24.11.0",
  "frontend should declare the strictest Node.js floor in the coordinated toolchain",
);

const expected = {
  dependencies: {
    "@element-plus/icons-vue": "2.3.2",
    "element-plus": "2.14.4",
    "highlight.js": "11.11.1",
    "marked": "18.0.9",
    "pinia": "4.0.2",
    "pinia-plugin-persistedstate": "4.7.1",
    "sass": "1.102.0",
    "video.js": "8.23.9",
    "vue": "3.5.41",
    "vue-router": "5.2.0",
  },
  devDependencies: {
    "@vitejs/plugin-vue": "6.0.8",
    "typescript": "5.9.3",
    "unplugin-auto-import": "21.1.0",
    "unplugin-vue-components": "32.1.0",
    "vite": "8.2.1",
    "vue-tsc": "3.3.9",
  },
};

for (const [section, packages] of Object.entries(expected)) {
  for (const [name, version] of Object.entries(packages)) {
    const actual = String(manifest[section]?.[name] || "").replace(/^[~^]/, "");
    assert.equal(actual, version, `${section}.${name} should use ${version}`);
  }
}

assert.ok(!manifest.devDependencies?.["@babel/types"], "unused @babel/types should not remain a direct dependency");
assert.ok(!lock.includes("@volar/vue-typescript"), "lockfile should not retain deprecated @volar/vue-typescript");
assert.ok(!lock.includes("@volar/vue-code-gen"), "lockfile should not retain deprecated @volar/vue-code-gen");

console.log("frontend toolchain smoke passed");
