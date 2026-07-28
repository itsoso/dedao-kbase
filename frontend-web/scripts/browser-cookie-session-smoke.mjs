import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = fs.readFileSync(path.join(root, "app.js"), "utf8");
const authEnd = source.indexOf("\nconst readerRouteSuffixes");

assert.ok(authEnd > 0, "app.js should expose its browser request helpers before reader routes");

const authSource = source.slice(0, authEnd);
const bearerSets = [...source.matchAll(/headers\.set\("Authorization"/g)];
assert.equal(bearerSets.length, 1, "browser code should set Authorization only for legacy migration");
assert.match(source, /fetch\("\/browser\/session\/migrate"/);
assert.doesNotMatch(source, /\/browser\/session-token/);

function responseJSON(status, payload) {
  return new Response(payload == null ? null : JSON.stringify(payload), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function createHarness({
  localEntries = [],
  sessionEntries = [],
  responder,
  now = Date.parse("2026-07-28T22:00:00Z"),
} = {}) {
  const local = new Map(localEntries);
  const session = new Map(sessionEntries);
  const fetchCalls = [];
  const broadcasts = [];
  const storageSignals = [];
  const consoleMessages = [];
  const objectURLs = [];
  const revokedURLs = [];
  const links = [];
  const windowListeners = new Map();
  let broadcastChannel = null;
  let nowMilliseconds = now;

  class TestDate extends Date {
    static now() {
      return nowMilliseconds;
    }
  }

  function storageFor(values) {
    return {
      getItem(key) {
        return values.get(key) ?? null;
      },
      removeItem(key) {
        values.delete(key);
      },
      setItem(key, value) {
        values.set(key, String(value));
        if (key === "kbase.browser-session.signal") {
          storageSignals.push(String(value));
        }
      },
    };
  }

  class TestBroadcastChannel {
    constructor(name) {
      assert.equal(name, "kbase-browser-session");
      this.name = name;
      this.onmessage = null;
      broadcastChannel = this;
    }

    postMessage(message) {
      broadcasts.push(structuredClone(message));
    }

    close() {}
  }

  class TestURL extends URL {
    static createObjectURL(blob) {
      const value = `blob:test-${objectURLs.length + 1}`;
      objectURLs.push({ value, blob });
      return value;
    }

    static revokeObjectURL(value) {
      revokedURLs.push(value);
    }
  }

  const context = {
    Blob,
    Date: TestDate,
    Headers,
    Response,
    URL: TestURL,
    URLSearchParams,
    structuredClone,
    setTimeout(callback) {
      callback();
      return 1;
    },
    clearTimeout() {},
    console: {
      log(...values) {
        consoleMessages.push(values.map(String).join(" "));
      },
      warn(...values) {
        consoleMessages.push(values.map(String).join(" "));
      },
      error(...values) {
        consoleMessages.push(values.map(String).join(" "));
      },
    },
    document: {
      body: {
        append(link) {
          links.push(link);
        },
      },
      createElement(tag) {
        assert.equal(tag, "a");
        return {
          href: "",
          download: "",
          click() {
            this.clicked = true;
          },
          remove() {
            this.removed = true;
          },
        };
      },
      querySelector(selector) {
        if (selector === "#app") {
          return { className: "", innerHTML: "" };
        }
        return null;
      },
    },
    window: {
      BroadcastChannel: TestBroadcastChannel,
      localStorage: storageFor(local),
      sessionStorage: storageFor(session),
      location: {
        pathname: "/unit-test",
        origin: "https://kbase.example",
      },
      addEventListener(type, listener) {
        windowListeners.set(type, listener);
      },
    },
  };
  context.globalThis = context;
  context.fetch = async (input, options = {}) => {
    const headers = options.headers instanceof Headers
      ? options.headers
      : new Headers(options.headers || {});
    const call = {
      url: String(input),
      method: String(options.method || "GET").toUpperCase(),
      credentials: options.credentials || "",
      authorization: headers.get("Authorization") || "",
      csrf: headers.get("X-KBase-CSRF") || "",
    };
    fetchCalls.push(call);
    if (responder) {
      return responder(call, fetchCalls);
    }
    throw new Error(`unexpected fetch ${call.method} ${call.url}`);
  };

  vm.runInNewContext(`${authSource}
globalThis.__auth = {
  apiFetch,
  apiDownload,
  ensureBrowserSession,
  loadBrowserSession,
  logoutBrowserSession,
  browserSessionState,
};`, context, { filename: "frontend-web/app.js" });

  return {
    ...context.__auth,
    local,
    session,
    fetchCalls,
    broadcasts,
    storageSignals,
    consoleMessages,
    objectURLs,
    revokedURLs,
    links,
    windowListeners,
    advanceTime(milliseconds) {
      nowMilliseconds += milliseconds;
    },
    emitBroadcast(message) {
      broadcastChannel?.onmessage?.({ data: structuredClone(message) });
    },
    emitStorage(message) {
      windowListeners.get("storage")?.({
        key: "kbase.browser-session.signal",
        newValue: JSON.stringify(message),
      });
    },
  };
}

const legacySecret = "legacy-secret-value";
const migration = createHarness({
  localEntries: [
    ["kbase.token", legacySecret],
    ["kbaseToken", legacySecret],
  ],
  sessionEntries: [["KBASE_AUTH_TOKEN", legacySecret]],
  responder(call) {
    if (call.url === "/browser/session/migrate") {
      assert.equal(call.authorization, `Bearer ${legacySecret}`);
      return responseJSON(200, { id: "session-migrated" });
    }
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "session-migrated" },
        csrf_token: "csrf-migrated",
        csrf_expires_at: "2026-07-28T22:15:00Z",
      });
    }
    if (call.url === "/api/books") {
      return responseJSON(200, { books: [] });
    }
    if (call.url === "/api/header-injection") {
      assert.equal(call.authorization, "");
      return responseJSON(200, { ok: true });
    }
    throw new Error(`unexpected migration fetch ${call.method} ${call.url}`);
  },
});

const [migrationFirst, migrationSecond] = await Promise.all([
  migration.apiFetch("/api/books"),
  migration.apiFetch("/api/books"),
]);
assert.equal(Array.isArray(migrationFirst.books), true);
assert.equal(Array.isArray(migrationSecond.books), true);
assert.equal(
  migration.fetchCalls.filter((call) => call.url === "/browser/session/migrate").length,
  1,
  "legacy Bearer migration should happen once",
);
assert.equal(
  migration.fetchCalls.filter((call) => call.url === "/api/browser/session").length,
  1,
  "concurrent requests should share session status loading",
);
for (const key of ["kbase.token", "kbaseToken", "KBASE_AUTH_TOKEN"]) {
  assert.equal(migration.local.has(key), false, `localStorage ${key} should be removed`);
  assert.equal(migration.session.has(key), false, `sessionStorage ${key} should be removed`);
}
assert.ok(
  migration.fetchCalls
    .filter((call) => call.url !== "/browser/session/migrate")
    .every((call) => call.credentials === "same-origin" && !call.authorization),
  "ordinary requests should use Cookie credentials without Authorization",
);
assert.ok(!migration.consoleMessages.join("\n").includes(legacySecret), "logs must not contain the legacy token");
assert.ok(!JSON.stringify(migrationFirst).includes(legacySecret), "API results must not expose the legacy token");
assert.equal(
  (await migration.apiFetch("/api/header-injection", {
    headers: { Authorization: "Bearer caller-injected-secret" },
  })).ok,
  true,
  "ordinary Cookie requests should strip caller-provided authorization",
);
for (const hint of [...migration.broadcasts.map(JSON.stringify), ...migration.storageSignals]) {
  assert.ok(!hint.includes(legacySecret), "cross-tab hints must not contain the legacy token");
  assert.doesNotMatch(hint, /csrf|credential|authorization/i, "cross-tab hints must not carry credentials");
}

const transientSecret = "transient-migration-secret";
const malformedLegacyValue = "错误 token";
const transientMigration = createHarness({
  localEntries: [
    ["kbase.token", transientSecret],
    ["kbaseToken", malformedLegacyValue],
  ],
  sessionEntries: [["KBASE_AUTH_TOKEN", transientSecret]],
  responder(call) {
    if (call.url === "/browser/session/migrate") {
      return responseJSON(503, { error: "service unavailable" });
    }
    throw new Error(`unexpected transient migration fetch ${call.method} ${call.url}`);
  },
});
await assert.rejects(
  transientMigration.ensureBrowserSession(),
  (error) => error.status === 503,
);
assert.equal(
  transientMigration.local.get("kbase.token"),
  transientSecret,
  "transient migration failure should preserve localStorage for a reload retry",
);
assert.equal(
  transientMigration.session.get("KBASE_AUTH_TOKEN"),
  transientSecret,
  "transient migration failure should preserve sessionStorage for a reload retry",
);
assert.equal(
  transientMigration.local.get("kbaseToken"),
  malformedLegacyValue,
  "transient migration failure should preserve malformed legacy storage",
);

const malformedOnly = createHarness({
  localEntries: [["kbase.token", malformedLegacyValue]],
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "existing-cookie-session" },
        csrf_token: "existing-cookie-csrf",
        csrf_expires_at: "2026-07-28T22:15:00Z",
      });
    }
    throw new Error(`unexpected malformed discovery fetch ${call.method} ${call.url}`);
  },
});
await malformedOnly.ensureBrowserSession();
assert.equal(
  malformedOnly.local.get("kbase.token"),
  malformedLegacyValue,
  "read-only legacy discovery must preserve malformed values",
);
assert.equal(
  malformedOnly.fetchCalls.some((call) => call.url === "/browser/session/migrate"),
  false,
  "malformed legacy values must not be sent to migration",
);

const rejectedSecret = "rejected-migration-secret";
const rejectedMigration = createHarness({
  localEntries: [["kbase.token", rejectedSecret]],
  sessionEntries: [["KBASE_AUTH_TOKEN", rejectedSecret]],
  responder(call) {
    if (call.url === "/browser/session/migrate") {
      return responseJSON(401, { error: "unauthorized" });
    }
    if (call.url === "/browser/session") {
      return responseJSON(200, { id: "replacement-session" });
    }
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "replacement-session" },
        csrf_token: "replacement-csrf",
        csrf_expires_at: "2026-07-28T22:15:00Z",
      });
    }
    throw new Error(`unexpected rejected migration fetch ${call.method} ${call.url}`);
  },
});
await rejectedMigration.ensureBrowserSession();
assert.equal(rejectedMigration.local.has("kbase.token"), false);
assert.equal(rejectedMigration.session.has("KBASE_AUTH_TOKEN"), false);

for (const signalCase of [
  { type: "logout", transport: "broadcast" },
  { type: "login", transport: "storage" },
]) {
  let resolveStatus;
  let markStatusStarted;
  const statusStarted = new Promise((resolve) => {
    markStatusStarted = resolve;
  });
  const delayedStatus = new Promise((resolve) => {
    resolveStatus = resolve;
  });
  const staleStatus = createHarness({
    responder: async (call) => {
      if (call.url === "/api/browser/session") {
        markStatusStarted();
        await delayedStatus;
        return responseJSON(200, {
          session: { id: `stale-${signalCase.type}-session` },
          csrf_token: `stale-${signalCase.type}-csrf`,
          csrf_expires_at: "2026-07-28T22:15:00Z",
        });
      }
      throw new Error(`unexpected stale status fetch ${call.method} ${call.url}`);
    },
  });
  const pendingSession = staleStatus.ensureBrowserSession();
  await statusStarted;
  const hint = { type: signalCase.type, at: 1, nonce: "cross-tab-hint" };
  if (signalCase.transport === "broadcast") {
    staleStatus.emitBroadcast(hint);
  } else {
    staleStatus.emitStorage(hint);
  }
  resolveStatus();
  await assert.rejects(
    pendingSession,
    (error) => error.code === "browser_session_stale",
    `${signalCase.transport} ${signalCase.type} should invalidate an in-flight status response`,
  );
  assert.equal(staleStatus.browserSessionState.ready, false);
  assert.equal(staleStatus.browserSessionState.session, null);
  assert.equal(staleStatus.browserSessionState.csrfToken, "");
}

let staleLoginStatusCount = 0;
let markLoginStarted;
let resolveLogin;
const loginStarted = new Promise((resolve) => {
  markLoginStarted = resolve;
});
const delayedLogin = new Promise((resolve) => {
  resolveLogin = resolve;
});
const staleLogin = createHarness({
  responder: async (call) => {
    if (call.url === "/api/browser/session") {
      staleLoginStatusCount += 1;
      if (staleLoginStatusCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, {
        session: { id: "stale-login-session" },
        csrf_token: "stale-login-csrf",
        csrf_expires_at: "2026-07-28T22:15:00Z",
      });
    }
    if (call.url === "/browser/session") {
      markLoginStarted();
      await delayedLogin;
      return responseJSON(200, { id: "stale-login-session" });
    }
    throw new Error(`unexpected stale login fetch ${call.method} ${call.url}`);
  },
});
const pendingLogin = staleLogin.ensureBrowserSession();
await loginStarted;
staleLogin.emitStorage({ type: "logout", at: 1, nonce: "logout-during-login" });
resolveLogin();
await assert.rejects(
  pendingLogin,
  (error) => error.code === "browser_session_stale",
  "a logout hint should invalidate an in-flight browser login",
);
assert.equal(staleLoginStatusCount, 1, "stale login must not start a follow-up status request");
assert.equal(staleLogin.browserSessionState.ready, false);
assert.equal(staleLogin.browserSessionState.session, null);
assert.equal(staleLogin.browserSessionState.csrfToken, "");

for (const transport of ["broadcast", "storage"]) {
  let logoutRaceStatusCount = 0;
  let logoutRaceLoginCount = 0;
  let logoutRaceRequestCount = 0;
  let markLogoutRaceRequestStarted;
  let releaseLogoutRaceRequest;
  const logoutRaceRequestStarted = new Promise((resolve) => {
    markLogoutRaceRequestStarted = resolve;
  });
  const delayedLogoutRaceRequest = new Promise((resolve) => {
    releaseLogoutRaceRequest = resolve;
  });
  const logoutRace = createHarness({
    responder: async (call) => {
      if (call.url === "/api/browser/session") {
        logoutRaceStatusCount += 1;
        if (logoutRaceStatusCount === 1 || logoutRaceLoginCount > 0) {
          return responseJSON(200, {
            session: { id: `logout-race-session-${transport}` },
            csrf_token: `logout-race-csrf-${transport}`,
            csrf_expires_at: "2026-07-28T22:15:00Z",
          });
        }
        return responseJSON(401, { error: "unauthorized" });
      }
      if (call.url === "/browser/session") {
        logoutRaceLoginCount += 1;
        return responseJSON(200, { id: `logout-race-session-${transport}` });
      }
      if (call.url === "/api/logout-race") {
        logoutRaceRequestCount += 1;
        if (logoutRaceRequestCount === 1) {
          markLogoutRaceRequestStarted();
          await delayedLogoutRaceRequest;
          return responseJSON(401, { error: "unauthorized" });
        }
        return responseJSON(200, { ok: true });
      }
      throw new Error(`unexpected logout race fetch ${call.method} ${call.url}`);
    },
  });
  const pendingLogoutRace = logoutRace.apiFetch("/api/logout-race");
  await logoutRaceRequestStarted;
  const logoutHint = { type: "logout", at: 1, nonce: `logout-race-${transport}` };
  if (transport === "broadcast") {
    logoutRace.emitBroadcast(logoutHint);
  } else {
    logoutRace.emitStorage(logoutHint);
  }
  releaseLogoutRaceRequest();
  await assert.rejects(
    pendingLogoutRace,
    (error) => error.status === 401,
    `${transport} logout should surface a late 401`,
  );
  assert.equal(logoutRaceLoginCount, 0, `${transport} logout must not trigger browser login`);
  assert.equal(logoutRaceRequestCount, 1, `${transport} logout must not retry the old request`);
  assert.equal(logoutRace.browserSessionState.ready, false);
  assert.equal(logoutRace.browserSessionState.session, null);
  assert.equal(logoutRace.browserSessionState.csrfToken, "");
  assert.equal(logoutRace.browserSessionState.invalidationReason, "logout");
  assert.ok(logoutRace.browserSessionState.logoutGeneration > 0);
}

const crossOrigin = createHarness({
  responder(call) {
    throw new Error(`cross-origin request reached fetch: ${call.method} ${call.url}`);
  },
});
for (const target of [
  "https://evil.example/api/write",
  "//evil.example/api/write",
]) {
  await assert.rejects(
    crossOrigin.apiFetch(target, {
      method: "POST",
      headers: { "X-KBase-CSRF": "caller-csrf-must-not-leak" },
      body: JSON.stringify({ value: 1 }),
    }),
    (error) => error.code === "cross_origin_request",
    `${target} should be rejected before browser session loading`,
  );
}
assert.equal(crossOrigin.fetchCalls.length, 0, "cross-origin requests must not reach fetch");
assert.equal(crossOrigin.browserSessionState.csrfToken, "", "cross-origin rejection must not load CSRF");

let loginCount = 0;
let statusCount = 0;
let protectedCount = 0;
const recovery = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      statusCount += 1;
      if (statusCount === 1) {
        return responseJSON(200, {
          session: { id: "before-recovery" },
          csrf_token: "csrf-before",
          csrf_expires_at: "2026-07-28T22:15:00Z",
        });
      }
      return responseJSON(200, {
        session: { id: "after-recovery" },
        csrf_token: "csrf-after",
        csrf_expires_at: "2026-07-28T22:30:00Z",
      });
    }
    if (call.url === "/browser/session") {
      loginCount += 1;
      return responseJSON(200, { id: "after-recovery" });
    }
    if (call.url === "/api/protected") {
      protectedCount += 1;
      if (protectedCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, { ok: true });
    }
    if (call.url === "/api/write") {
      assert.equal(call.csrf, "csrf-after");
      return responseJSON(200, { written: true });
    }
    throw new Error(`unexpected recovery fetch ${call.method} ${call.url}`);
  },
});

assert.equal((await recovery.apiFetch("/api/protected")).ok, true);
assert.equal(loginCount, 1, "401 recovery should perform one browser login");
assert.equal(protectedCount, 2, "401 recovery should retry the original request once");
assert.equal(
  (await recovery.apiFetch("/api/write", {
    method: "POST",
    body: JSON.stringify({ value: 1 }),
  })).written,
  true,
);

let blockedRecoveryStatusCount = 0;
let blockedRecoveryLoginCount = 0;
let blockedRecoveryRequestCount = 0;
let blockedRecoveryLoginResolved = false;
let markBlockedLoginStarted;
let releaseBlockedLogin;
let markSecondUnauthorizedDelivered;
const blockedLoginStarted = new Promise((resolve) => {
  markBlockedLoginStarted = resolve;
});
const blockedLogin = new Promise((resolve) => {
  releaseBlockedLogin = resolve;
});
const secondUnauthorizedDelivered = new Promise((resolve) => {
  markSecondUnauthorizedDelivered = resolve;
});
const blockedConcurrentRecovery = createHarness({
  responder: async (call) => {
    if (call.url === "/api/browser/session") {
      blockedRecoveryStatusCount += 1;
      if (blockedRecoveryStatusCount === 1 || blockedRecoveryLoginResolved) {
        return responseJSON(200, {
          session: { id: `blocked-recovery-session-${blockedRecoveryStatusCount}` },
          csrf_token: `blocked-recovery-csrf-${blockedRecoveryStatusCount}`,
          csrf_expires_at: "2999-01-01T00:00:00Z",
        });
      }
      return responseJSON(401, { error: "unauthorized" });
    }
    if (call.url === "/browser/session") {
      blockedRecoveryLoginCount += 1;
      markBlockedLoginStarted();
      await blockedLogin;
      blockedRecoveryLoginResolved = true;
      return responseJSON(200, { id: "blocked-recovery-session" });
    }
    if (call.url === "/api/blocked-concurrent-recovery") {
      blockedRecoveryRequestCount += 1;
      if (blockedRecoveryRequestCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      if (blockedRecoveryRequestCount === 2) {
        await blockedLoginStarted;
        markSecondUnauthorizedDelivered();
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, { ok: true });
    }
    throw new Error(`unexpected blocked recovery fetch ${call.method} ${call.url}`);
  },
});
const blockedRecoveryResultsPromise = Promise.all([
  blockedConcurrentRecovery.apiFetch("/api/blocked-concurrent-recovery"),
  blockedConcurrentRecovery.apiFetch("/api/blocked-concurrent-recovery"),
]);
await blockedLoginStarted;
await secondUnauthorizedDelivered;
await new Promise((resolve) => setImmediate(resolve));
releaseBlockedLogin();
const blockedRecoveryResults = await blockedRecoveryResultsPromise;
assert.ok(blockedRecoveryResults.every((payload) => payload.ok === true));
assert.equal(
  blockedRecoveryLoginCount,
  1,
  "a late concurrent 401 must join the blocked recovery login",
);
assert.equal(blockedRecoveryStatusCount, 2, "blocked concurrent recovery should share status refresh");
assert.equal(blockedRecoveryRequestCount, 4, "each blocked concurrent request should retry once");

let delayedStatusCount = 0;
let delayedLoginCount = 0;
let delayedRequestCount = 0;
let releaseDelayedUnauthorized;
const delayedUnauthorized = new Promise((resolve) => {
  releaseDelayedUnauthorized = resolve;
});
const concurrentRecovery = createHarness({
  responder: async (call) => {
    if (call.url === "/api/browser/session") {
      delayedStatusCount += 1;
      if (delayedStatusCount === 2) {
        setImmediate(releaseDelayedUnauthorized);
      }
      return responseJSON(200, {
        session: { id: `concurrent-session-${delayedStatusCount}` },
        csrf_token: `concurrent-csrf-${delayedStatusCount}`,
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/browser/session") {
      delayedLoginCount += 1;
      return responseJSON(200, { id: `concurrent-session-${delayedLoginCount + 1}` });
    }
    if (call.url === "/api/concurrent-recovery") {
      delayedRequestCount += 1;
      if (delayedRequestCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      if (delayedRequestCount === 2) {
        await delayedUnauthorized;
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, { ok: true });
    }
    throw new Error(`unexpected concurrent recovery fetch ${call.method} ${call.url}`);
  },
});
const concurrentResults = await Promise.all([
  concurrentRecovery.apiFetch("/api/concurrent-recovery"),
  concurrentRecovery.apiFetch("/api/concurrent-recovery"),
]);
assert.ok(concurrentResults.every((payload) => payload.ok === true));
assert.equal(delayedLoginCount, 1, "late concurrent 401 responses should share one login recovery");
assert.equal(delayedStatusCount, 2, "late concurrent 401 responses should share one status refresh");
assert.equal(delayedRequestCount, 4, "each concurrent request should retry only once");

let csrfStatusCount = 0;
const csrfRefresh = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      csrfStatusCount += 1;
      return responseJSON(200, {
        session: { id: "csrf-refresh-session" },
        csrf_token: csrfStatusCount === 1 ? "csrf-expired" : "csrf-refreshed",
        csrf_expires_at: csrfStatusCount === 1
          ? "2026-07-28T22:15:00Z"
          : "2026-07-28T22:30:00Z",
      });
    }
    if (call.url === "/api/write-after-expiry") {
      assert.equal(call.csrf, "csrf-refreshed");
      return responseJSON(200, { written: true });
    }
    throw new Error(`unexpected csrf refresh fetch ${call.method} ${call.url}`);
  },
});
await csrfRefresh.ensureBrowserSession();
csrfRefresh.advanceTime((14 * 60 + 45) * 1000);
assert.equal(
  (await csrfRefresh.apiFetch("/api/write-after-expiry", {
    method: "POST",
    body: JSON.stringify({ value: 1 }),
  })).written,
  true,
);
assert.equal(csrfStatusCount, 2, "unsafe requests should refresh an expired CSRF token");

let csrfRecoveryStatusCount = 0;
let csrfRecoveryLoginCount = 0;
let csrfRecoveryWriteCount = 0;
const csrfThenUnauthorized = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      csrfRecoveryStatusCount += 1;
      return responseJSON(200, {
        session: { id: `csrf-recovery-session-${csrfRecoveryStatusCount}` },
        csrf_token: `csrf-recovery-${csrfRecoveryStatusCount}`,
        csrf_expires_at: csrfRecoveryStatusCount === 1
          ? "2026-07-28T22:15:00Z"
          : "2026-07-28T22:30:00Z",
      });
    }
    if (call.url === "/browser/session") {
      csrfRecoveryLoginCount += 1;
      return responseJSON(200, { id: "csrf-recovery-session-3" });
    }
    if (call.url === "/api/write-after-csrf-refresh") {
      csrfRecoveryWriteCount += 1;
      if (csrfRecoveryWriteCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      assert.equal(call.csrf, "csrf-recovery-3");
      return responseJSON(200, { written: true });
    }
    throw new Error(`unexpected csrf recovery fetch ${call.method} ${call.url}`);
  },
});
await csrfThenUnauthorized.ensureBrowserSession();
csrfThenUnauthorized.advanceTime((14 * 60 + 45) * 1000);
assert.equal(
  (await csrfThenUnauthorized.apiFetch("/api/write-after-csrf-refresh", {
    method: "POST",
    body: JSON.stringify({ value: 1 }),
  })).written,
  true,
);
assert.equal(csrfRecoveryLoginCount, 1, "401 after proactive CSRF refresh should still recover once");
assert.equal(csrfRecoveryStatusCount, 3);
assert.equal(csrfRecoveryWriteCount, 2);

for (const status of [403, 503]) {
  let requestCount = 0;
  let sessionRequests = 0;
  const failure = createHarness({
    responder(call) {
      if (call.url === "/api/browser/session") {
        sessionRequests += 1;
        return responseJSON(200, {
          session: { id: `status-${status}` },
          csrf_token: `csrf-${status}`,
          csrf_expires_at: "2026-07-28T22:15:00Z",
        });
      }
      if (call.url === "/api/failure") {
        requestCount += 1;
        return responseJSON(status, { error: `failure-${status}` });
      }
      throw new Error(`unexpected failure fetch ${call.method} ${call.url}`);
    },
  });
  await assert.rejects(
    failure.apiFetch("/api/failure"),
    (error) => error.status === status && error.message === `failure-${status}`,
  );
  assert.equal(requestCount, 1, `${status} should not retry`);
  assert.equal(sessionRequests, 1, `${status} should not reopen login/status`);
}

const logout = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "logout-session" },
        csrf_token: "csrf-logout",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/browser/session/logout") {
      assert.equal(call.csrf, "csrf-logout");
      return new Response(null, { status: 204 });
    }
    throw new Error(`unexpected logout fetch ${call.method} ${call.url}`);
  },
});
await logout.ensureBrowserSession();
await logout.logoutBrowserSession();
assert.equal(logout.browserSessionState.ready, false);
assert.equal(logout.browserSessionState.session, null);
assert.equal(logout.browserSessionState.csrfToken, "");
assert.ok(logout.broadcasts.some((message) => message?.type === "logout"));
assert.ok(logout.broadcasts.every((message) => !JSON.stringify(message).includes("csrf-logout")));
for (const hint of [...logout.broadcasts.map(JSON.stringify), ...logout.storageSignals]) {
  assert.ok(!hint.includes("csrf-logout"), "logout hints must not contain CSRF credentials");
  assert.doesNotMatch(hint, /csrf|credential|authorization/i, "logout hints must remain credential-free");
}

let logoutStatusCount = 0;
const expiringLogout = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      logoutStatusCount += 1;
      return responseJSON(200, {
        session: { id: "expiring-logout-session" },
        csrf_token: logoutStatusCount === 1 ? "csrf-expired-logout" : "csrf-fresh-logout",
        csrf_expires_at: logoutStatusCount === 1
          ? "2026-07-28T22:15:00Z"
          : "2026-07-28T22:30:00Z",
      });
    }
    if (call.url === "/api/browser/session/logout") {
      assert.equal(call.csrf, "csrf-fresh-logout");
      return new Response(null, { status: 204 });
    }
    throw new Error(`unexpected expiring logout fetch ${call.method} ${call.url}`);
  },
});
await expiringLogout.ensureBrowserSession();
expiringLogout.advanceTime((14 * 60 + 45) * 1000);
await expiringLogout.logoutBrowserSession();
assert.equal(logoutStatusCount, 2, "logout should refresh an expired CSRF token");

const download = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "download-session" },
        csrf_token: "csrf-download",
        csrf_expires_at: "2026-07-28T22:15:00Z",
      });
    }
    if (call.url === "/api/export") {
      return new Response(new Blob(["downloaded"], { type: "application/octet-stream" }), {
        status: 200,
      });
    }
    throw new Error(`unexpected download fetch ${call.method} ${call.url}`);
  },
});
assert.equal(await download.apiDownload("/api/export", {}, "export.bin"), 10);
const downloadCall = download.fetchCalls.find((call) => call.url === "/api/export");
assert.equal(downloadCall.credentials, "same-origin");
assert.equal(downloadCall.authorization, "");
assert.equal(download.links[0].download, "export.bin");
assert.deepEqual(download.revokedURLs, ["blob:test-1"]);

assert.match(source, /loadPrivateSourceAssets/);
const privateAssets = source.match(/async function loadPrivateSourceAssets[\s\S]*?\n\}/)?.[0] || "";
assert.match(privateAssets, /credentials:\s*"same-origin"/);
assert.doesNotMatch(privateAssets, /Authorization|setAuthorizationHeader|getToken/);
assert.match(privateAssets, /URL\.createObjectURL/);

console.log("browser cookie session smoke passed");
