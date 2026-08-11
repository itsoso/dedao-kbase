import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";
import { loadAppSource } from "./load-app-source.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const js = loadAppSource(root);
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");

function responseJSON(status, payload) {
  return new Response(payload == null ? null : JSON.stringify(payload), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function activeSessionPayload(id = "session-current") {
  return {
    session: {
      id,
      device_label: "Safari · macOS",
      created_at: "2026-07-28T12:00:00Z",
      last_active_at: "2026-07-28T12:05:00Z",
      expires_at: "2026-08-27T12:05:00Z",
    },
    client_id: "client_settings_smoke_0123456789",
    epoch: 1,
    csrf_token: "csrf-settings-smoke",
    csrf_expires_at: "2999-01-01T00:00:00Z",
  };
}

function createStorage() {
  const values = new Map();
  return {
    getItem(key) {
      return values.get(key) ?? null;
    },
    setItem(key, value) {
      values.set(key, String(value));
    },
    removeItem(key) {
      values.delete(key);
    },
  };
}

class TestElement {
  constructor() {
    this.listeners = new Map();
    this.attributes = new Map();
    this.mutations = [];
    this._textContent = "";
  }

  addEventListener(type, listener) {
    this.listeners.set(type, listener);
  }

  click() {
    return this.listeners.get("click")?.({
      currentTarget: this,
      preventDefault() {},
    });
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value));
    this.mutations.push(["attribute", name, String(value)]);
  }

  getAttribute(name) {
    return this.attributes.get(name) ?? null;
  }

  set textContent(value) {
    this._textContent = String(value);
    this.mutations.push(["text", this._textContent]);
  }

  get textContent() {
    return this._textContent;
  }
}

class TestApp {
  constructor() {
    this.className = "";
    this.inert = false;
    this._html = "";
    this.elements = new Map();
  }

  get innerHTML() {
    return this._html;
  }

  set innerHTML(value) {
    this._html = String(value);
    this.elements = new Map();
  }

  querySelector(selector) {
    const markers = {
      "[data-session-login]": "data-session-login",
      "[data-session-logout]": "data-session-logout",
      "[data-session-retry]": "data-session-retry",
    };
    const marker = markers[selector];
    if (!marker || !this._html.includes(marker)) {
      return null;
    }
    if (!this.elements.has(selector)) {
      this.elements.set(selector, new TestElement());
    }
    return this.elements.get(selector);
  }
}

async function waitFor(predicate, message) {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    if (predicate()) {
      return;
    }
    await new Promise((resolve) => setImmediate(resolve));
  }
  assert.fail(message);
}

function createHarness({ responder }) {
  const app = new TestApp();
  const announcer = new TestElement();
  const fetchCalls = [];
  const windowListeners = new Map();
  let browserChannel = null;

  class TestBroadcastChannel {
    constructor(name) {
      assert.equal(name, "kbase-browser-session");
      this.onmessage = null;
      browserChannel = this;
    }

    postMessage() {}

    close() {}
  }

  const localStorage = createStorage();
  const sessionStorage = createStorage();
  const body = {
    classList: { add() {}, remove() {} },
    querySelector() {
      return null;
    },
    appendChild() {},
    append() {},
  };
  const document = {
    body,
    addEventListener() {},
    removeEventListener() {},
    querySelector(selector) {
      if (selector === "#app") {
        return app;
      }
      if (selector === "#session-settings-announcer") {
        return announcer;
      }
      return null;
    },
    querySelectorAll() {
      return [];
    },
    createElement() {
      return new TestElement();
    },
  };
  const window = {
    BroadcastChannel: TestBroadcastChannel,
    location: {
      pathname: "/settings/session",
      search: "",
      hash: "",
      origin: "https://kbase.example",
    },
    localStorage,
    sessionStorage,
    addEventListener(type, listener) {
      const listeners = windowListeners.get(type) || [];
      listeners.push(listener);
      windowListeners.set(type, listeners);
    },
    removeEventListener() {},
    requestAnimationFrame(callback) {
      callback();
      return 1;
    },
  };
  const context = {
    AbortController,
    Blob,
    crypto: webcrypto,
    document,
    fetch: async (input, options = {}) => {
      const headers = options.headers instanceof Headers
        ? options.headers
        : new Headers(options.headers || {});
      const call = {
        url: String(input),
        method: String(options.method || "GET").toUpperCase(),
        csrf: headers.get("X-KBase-CSRF") || "",
        clientID: headers.get("X-KBase-Browser-Client-ID") || "",
        epoch: headers.get("X-KBase-Browser-Epoch") || "",
        signal: options.signal || null,
      };
      fetchCalls.push(call);
      if (call.url === "/browser/session" && call.method === "GET") {
        return responseJSON(200, {
          client_id: call.clientID,
          epoch: 1,
        });
      }
      return responder(call, fetchCalls);
    },
    Headers,
    navigator: {},
    Request,
    Response,
    setInterval,
    clearInterval,
    setTimeout,
    clearTimeout,
    structuredClone,
    URL,
    URLSearchParams,
    window,
  };
  context.globalThis = context;

  const runnable = js.replace(
    /\nboot\(\);\s*$/,
    `
globalThis.__sessionSettings = {
  boot,
  sessionSettingsState,
  browserSessionState,
};`,
  );
  assert.notEqual(runnable, js, "test harness should replace the automatic boot call");
  vm.runInNewContext(runnable, context, { filename: "frontend-web/app.js" });

  return {
    app,
    announcer,
    fetchCalls,
    state: context.__sessionSettings.sessionSettingsState,
    async boot() {
      await context.__sessionSettings.boot();
    },
    element(selector) {
      return app.querySelector(selector);
    },
    emitBroadcast(type, nonce = `${type}-broadcast`) {
      browserChannel?.onmessage?.({ data: { type, nonce, at: Date.now() } });
    },
    emitStorage(type, nonce = `${type}-storage`) {
      for (const listener of windowListeners.get("storage") || []) {
        listener({
          key: "kbase.browser-session.signal",
          newValue: JSON.stringify({ type, nonce, at: Date.now() }),
        });
      }
    },
  };
}

assert.match(
  js,
  /sessionSettings:\s*"\/settings\/session"/,
  "ROUTES should expose the canonical current-session settings path",
);
assert.match(
  js,
  /href="\$\{escapeAttribute\(ROUTES\.sessionSettings\)\}"[^>]*>会话<\/a>/,
  "the primary navigation should include a compact 会话 entry",
);

for (const className of [
  ".session-settings",
  ".session-settings__band",
  ".session-settings__panel",
  ".session-settings__state",
  ".session-settings__details",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}
const stylesheetVersion = html.match(/styles\.css\?v=([^"]+)/)?.[1] || "";
const scriptVersion = html.match(/app\.js\?v=([^"]+)/)?.[1] || "";
assert.ok(stylesheetVersion.includes("20260728-session-settings"));
assert.ok(scriptVersion.includes("20260728-session-settings"));

const active = createHarness({
  responder(call) {
    assert.equal(call.url, "/api/browser/session");
    return responseJSON(200, activeSessionPayload());
  },
});
await active.boot();
assert.equal(active.state.status, "active");
assert.match(active.app.innerHTML, /aria-current="page"[^>]*>会话<\/a>/);
assert.equal(active.announcer.getAttribute("aria-busy"), "false");
assert.match(active.announcer.textContent, /当前会话已登录/);
assert.deepEqual(
  active.announcer.mutations.slice(-2).map((entry) => entry[0]),
  ["text", "attribute"],
  "terminal content must update before aria-busy is cleared",
);
for (const marker of [
  "当前会话",
  "当前设备",
  "Safari · macOS",
  "最近活跃",
  'datetime="2026-07-28T12:05:00Z"',
  "到期时间",
  'datetime="2026-08-27T12:05:00Z"',
  "退出登录",
]) {
  assert.ok(active.app.innerHTML.includes(marker), `active route should render ${marker}`);
}
assert.ok(active.element("[data-session-logout]")?.listeners.has("click"));

for (const [status, expectedState, marker] of [
  [401, "unauthorized", "尚未登录"],
  [403, "forbidden", "无权访问当前会话"],
  [503, "unavailable", "会话服务暂不可用"],
]) {
  const harness = createHarness({
    responder(call) {
      assert.equal(call.url, "/api/browser/session");
      return responseJSON(status, { error: marker });
    },
  });
  await harness.boot();
  assert.equal(harness.state.status, expectedState);
  assert.ok(harness.app.innerHTML.includes(marker));
  assert.doesNotMatch(harness.app.innerHTML, /role="status"/);
  assert.equal(harness.announcer.getAttribute("aria-busy"), "false");
  assert.match(harness.announcer.textContent, new RegExp(marker));
}

let logoutMode = "success";
const logout = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, activeSessionPayload("logout-session"));
    }
    if (call.url === "/api/browser/session/logout") {
      return logoutMode === "success"
        ? responseJSON(200, { status: "revoked" })
        : responseJSON(503, { error: "unavailable" });
    }
    throw new Error(`unexpected logout fetch ${call.method} ${call.url}`);
  },
});
await logout.boot();
logout.element("[data-session-logout]").click();
await waitFor(
  () => logout.state.status === "revoked",
  "bound logout button should render revoked after a successful logout",
);
assert.ok(logout.app.innerHTML.includes("当前会话已退出"));
assert.equal(logout.announcer.getAttribute("aria-busy"), "false");
assert.match(logout.announcer.textContent, /当前会话已退出/);

logoutMode = "failed";
const failedLogout = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, activeSessionPayload("failed-logout-session"));
    }
    if (call.url === "/api/browser/session/logout") {
      return responseJSON(503, { error: "unavailable" });
    }
    throw new Error(`unexpected failed logout fetch ${call.method} ${call.url}`);
  },
});
await failedLogout.boot();
failedLogout.element("[data-session-logout]").click();
await waitFor(
  () => failedLogout.state.status === "unavailable",
  "bound logout button should render unavailable when logout fails",
);

let loginStatusCalls = 0;
const login = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      loginStatusCalls += 1;
      return loginStatusCalls <= 2
        ? responseJSON(401, { error: "unauthorized" })
        : responseJSON(200, activeSessionPayload("login-recovered"));
    }
    if (call.url === "/browser/session" && call.method === "POST") {
      return responseJSON(200, {
        session: { id: "login-recovered" },
        client_id: call.clientID,
        epoch: Number(call.epoch),
      });
    }
    throw new Error(`unexpected login fetch ${call.method} ${call.url}`);
  },
});
await login.boot();
assert.equal(login.state.status, "unauthorized");
login.element("[data-session-login]").click();
await waitFor(
  () => login.state.status === "active",
  "bound login button should recover the active current session",
);
assert.ok(login.app.innerHTML.includes("Safari · macOS"));

let crossTabLoginActive = false;
const crossTabLogin = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return crossTabLoginActive
        ? responseJSON(200, activeSessionPayload("cross-tab-login"))
        : responseJSON(401, { error: "unauthorized" });
    }
    throw new Error(`unexpected cross-tab login fetch ${call.method} ${call.url}`);
  },
});
await crossTabLogin.boot();
crossTabLoginActive = true;
crossTabLogin.emitBroadcast("login");
await waitFor(
  () => crossTabLogin.state.status === "active",
  "a cross-tab login signal should recheck the current session",
);

const storageLogout = createHarness({
  responder(call) {
    assert.equal(call.url, "/api/browser/session");
    return responseJSON(200, activeSessionPayload("storage-logout"));
  },
});
await storageLogout.boot();
storageLogout.emitStorage("logout");
assert.equal(storageLogout.state.status, "revoked");
assert.ok(storageLogout.app.innerHTML.includes("当前会话已退出"));

let resolveStaleLoad;
const staleLoad = createHarness({
  responder(call) {
    assert.equal(call.url, "/api/browser/session");
    return new Promise((resolve) => {
      resolveStaleLoad = resolve;
    });
  },
});
const staleBoot = staleLoad.boot();
await waitFor(
  () => typeof resolveStaleLoad === "function",
  "settings load should reach the session status endpoint",
);
assert.equal(staleLoad.announcer.getAttribute("aria-busy"), "true");
assert.match(staleLoad.announcer.textContent, /正在读取当前会话/);
assert.deepEqual(
  staleLoad.announcer.mutations.slice(-2).map((entry) => entry[0]),
  ["attribute", "text"],
  "loading must set aria-busy before changing the announcement",
);
staleLoad.emitBroadcast("logout-start");
assert.equal(staleLoad.state.status, "revoked");
assert.ok(staleLoad.app.innerHTML.includes("当前会话已退出"));
assert.equal(staleLoad.announcer.getAttribute("aria-busy"), "false");
assert.match(staleLoad.announcer.textContent, /当前会话已退出/);
resolveStaleLoad(responseJSON(200, activeSessionPayload("stale-active")));
await staleBoot;
assert.equal(
  staleLoad.state.status,
  "revoked",
  "an old load result must not overwrite a newer cross-tab logout",
);

for (const forbidden of ["全部设备", "退出所有", "管理员", "撤销其他"]) {
  assert.doesNotMatch(active.app.innerHTML, new RegExp(forbidden));
}

const sessionStyleStart = css.indexOf(".session-settings");
const sessionStyleEnd = css.indexOf(".web-home", sessionStyleStart);
assert.ok(sessionStyleStart >= 0 && sessionStyleEnd > sessionStyleStart);
assert.doesNotMatch(
  css.slice(sessionStyleStart, sessionStyleEnd),
  /font-size:\s*clamp\([^;]*vw/,
  "session settings typography must not scale with viewport width",
);
assert.match(
  css,
  /@media \(max-width: 760px\)[\s\S]*\.session-settings__band h1[\s\S]*font-size:/,
  "session settings should use a stable mobile heading size",
);
assert.match(
  html,
  /id="session-settings-announcer"[^>]*aria-live="polite"[^>]*aria-atomic="true"/,
  "index.html should provide a stable live region outside the replaceable app root",
);
assert.match(css, /\.visually-hidden\s*\{/);

console.log("browser session settings smoke passed");
