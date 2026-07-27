import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = fs.readFileSync(path.join(root, "app.js"), "utf8");
const storage = new Map([["kbase.token", "错误 token"]]);
const app = { className: "", innerHTML: "" };
const fetchCalls = [];
let sessionToken = "fresh-token";

const context = {
  Blob,
  Headers,
  Response,
  URL,
  URLSearchParams,
  console,
  document: {
    body: {
      append() {},
    },
    createElement() {
      return {
        click() {},
        remove() {},
      };
    },
    querySelector(selector) {
      return selector === "#app" ? app : null;
    },
    querySelectorAll() {
      return [];
    },
  },
  window: {
    localStorage: {
      getItem(key) {
        return storage.get(key) || null;
      },
      removeItem(key) {
        storage.delete(key);
      },
      setItem(key, value) {
        storage.set(key, String(value));
      },
    },
    location: {
      pathname: "/unit-test",
    },
  },
};

context.fetch = async (url, options = {}) => {
  const headers = options.headers instanceof Headers ? options.headers : new Headers(options.headers || {});
  fetchCalls.push({
    url: String(url),
    authorization: headers.get("Authorization") || "",
  });
  if (url === "/browser/session-token") {
    return new Response(JSON.stringify({ token: sessionToken }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }
  if (url === "/api/rotated" && headers.get("Authorization") === "Bearer stale-token") {
    return new Response(JSON.stringify({ error: "unauthorized" }), {
      status: 401,
      headers: { "content-type": "application/json" },
    });
  }
  return new Response(JSON.stringify({ ok: true }), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
};

vm.runInNewContext(`${source}\nglobalThis.__apiFetch = apiFetch;`, context, {
  filename: "frontend-web/app.js",
});

await context.__apiFetch("/api/books");

assert.equal(storage.get("kbase.token"), "fresh-token");
assert.ok(fetchCalls.some((call) => call.url === "/browser/session-token"));
assert.equal(fetchCalls.at(-1).authorization, "Bearer fresh-token");
assert.ok(fetchCalls.every((call) => !call.authorization.includes("错误")));

const sessionTokenCalls = fetchCalls.filter(
  (call) => call.url === "/browser/session-token",
).length;
await context.__apiFetch("/api/books");
assert.equal(
  fetchCalls.filter((call) => call.url === "/browser/session-token").length,
  sessionTokenCalls,
);
assert.equal(fetchCalls.at(-1).authorization, "Bearer fresh-token");

storage.set("kbase.token", "stale-token");
sessionToken = "rotated-token";
const refreshStart = fetchCalls.length;
await context.__apiFetch("/api/rotated");
const refreshCalls = fetchCalls.slice(refreshStart);
assert.deepEqual(
  refreshCalls.map((call) => [call.url, call.authorization]),
  [
    ["/api/rotated", "Bearer stale-token"],
    ["/browser/session-token", ""],
    ["/api/rotated", "Bearer rotated-token"],
  ],
);
assert.equal(storage.get("kbase.token"), "rotated-token");

console.log("kbase token header smoke passed");
