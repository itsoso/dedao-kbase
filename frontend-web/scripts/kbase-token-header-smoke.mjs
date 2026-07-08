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
    return new Response(JSON.stringify({ token: "fresh-token" }), {
      status: 200,
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

console.log("kbase token header smoke passed");
