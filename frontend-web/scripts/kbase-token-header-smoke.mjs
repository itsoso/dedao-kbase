import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = fs.readFileSync(path.join(root, "app.js"), "utf8");
const authEnd = source.indexOf("\nconst readerRouteSuffixes");
const storage = new Map([["kbase.token", "错误 token"]]);
const fetchCalls = [];
const logs = [];

const localStorage = {
  getItem(key) {
    return storage.get(key) || null;
  },
  removeItem(key) {
    storage.delete(key);
  },
  setItem(key, value) {
    storage.set(key, String(value));
  },
};

const context = {
  Blob,
  Headers,
  Response,
  URL,
  URLSearchParams,
  console: {
    log(...values) {
      logs.push(values.map(String).join(" "));
    },
    warn(...values) {
      logs.push(values.map(String).join(" "));
    },
  },
  document: {
    body: { append() {} },
    createElement() {
      return { click() {}, remove() {} };
    },
    querySelector(selector) {
      return selector === "#app" ? { className: "", innerHTML: "" } : null;
    },
  },
  window: {
    localStorage,
    sessionStorage: {
      getItem() {
        return null;
      },
      removeItem() {},
    },
    location: {
      pathname: "/unit-test",
      origin: "https://kbase.example",
    },
    addEventListener() {},
  },
};

let statusCalls = 0;
context.fetch = async (url, options = {}) => {
  const headers = options.headers instanceof Headers
    ? options.headers
    : new Headers(options.headers || {});
  fetchCalls.push({
    url: String(url),
    credentials: options.credentials || "",
    authorization: headers.get("Authorization") || "",
  });
  if (url === "/api/browser/session") {
    statusCalls += 1;
    if (statusCalls === 1) {
      return new Response(JSON.stringify({ error: "unauthorized" }), {
        status: 401,
        headers: { "content-type": "application/json" },
      });
    }
    return new Response(JSON.stringify({
      session: { id: "cookie-session" },
      csrf_token: "csrf-cookie",
      csrf_expires_at: "2026-07-28T22:15:00Z",
    }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }
  if (url === "/browser/session") {
    return new Response(JSON.stringify({ id: "cookie-session" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }
  return new Response(JSON.stringify({ ok: true }), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
};

vm.runInNewContext(`${source.slice(0, authEnd)}
globalThis.__apiFetch = apiFetch;`, context, {
  filename: "frontend-web/app.js",
});

const payload = await context.__apiFetch("/api/books");
assert.equal(payload.ok, true);
assert.equal(
  storage.get("kbase.token"),
  "错误 token",
  "legacy discovery should not mutate malformed browser storage",
);
assert.ok(fetchCalls.every((call) => call.credentials === "same-origin"));
assert.ok(fetchCalls.every((call) => !call.authorization), "ordinary requests must not build a Bearer header");
assert.ok(!logs.join("\n").includes("错误 token"), "invalid token values must not reach logs");
assert.ok(!source.includes("/browser/session-token"), "the retired token exchange endpoint must not be called");

console.log("kbase cookie auth smoke passed");
