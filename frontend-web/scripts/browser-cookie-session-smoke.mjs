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

function deferredBodyResponse(body, contentType) {
  let markBodyStarted;
  let resolveBody;
  const bodyStarted = new Promise((resolve) => {
    markBodyStarted = resolve;
  });
  const response = new Response(null, {
    status: 200,
    headers: { "content-type": contentType },
  });
  function waitForBody(value) {
    markBodyStarted();
    return new Promise((resolve) => {
      resolveBody = () => resolve(value);
    });
  }
  Object.defineProperty(response, "blob", {
    configurable: true,
    value: () => waitForBody(new Blob([body], { type: contentType })),
  });
  Object.defineProperty(response, "text", {
    configurable: true,
    value: () => waitForBody(body),
  });
  return {
    bodyStarted,
    releaseBody(afterResolve = null) {
      resolveBody();
      if (afterResolve) {
        // VM promise adoption uses two jobs; the third lands after guard return, before commit.
        queueMicrotask(() => queueMicrotask(() => queueMicrotask(afterResolve)));
      }
    },
    response,
  };
}

function createHarness({
  localEntries = [],
  sessionEntries = [],
  responder,
  now = Date.parse("2026-07-28T22:00:00Z"),
  randomByte = 0x5a,
  familyEpoch = 1,
  storageAccessError = false,
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

  const testWindow = {
    BroadcastChannel: TestBroadcastChannel,
    location: {
      pathname: "/unit-test",
      origin: "https://kbase.example",
    },
    addEventListener(type, listener) {
      windowListeners.set(type, listener);
    },
  };
  if (storageAccessError) {
    for (const property of ["localStorage", "sessionStorage"]) {
      Object.defineProperty(testWindow, property, {
        configurable: true,
        get() {
          throw new DOMException("storage access denied", "SecurityError");
        },
      });
    }
  } else {
    testWindow.localStorage = storageFor(local);
    testWindow.sessionStorage = storageFor(session);
  }

  const context = {
    AbortController,
    Blob,
    Date: TestDate,
    Headers,
    Request,
    Response,
    URL: TestURL,
    URLSearchParams,
    structuredClone,
    crypto: {
      getRandomValues(values) {
        values.fill(randomByte);
        return values;
      },
    },
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
    window: testWindow,
  };
  context.globalThis = context;
  context.fetch = async (input, options = {}) => {
    const headers = options.headers instanceof Headers
      ? options.headers
      : new Headers(options.headers || {});
    const target = String(input);
    let normalizedURL = target;
    try {
      const parsed = new URL(target, "https://kbase.example");
      if (parsed.origin === "https://kbase.example") {
        normalizedURL = `${parsed.pathname}${parsed.search}${parsed.hash}`;
      }
    } catch {
      normalizedURL = target;
    }
    const call = {
      url: normalizedURL,
      target,
      method: String(options.method || "GET").toUpperCase(),
      credentials: options.credentials || "",
      authorization: headers.get("Authorization") || "",
      csrf: headers.get("X-KBase-CSRF") || "",
      clientID: headers.get("X-KBase-Browser-Client-ID") || "",
      epoch: headers.get("X-KBase-Browser-Epoch") || "",
      signal: options.signal || null,
    };
    fetchCalls.push(call);
    if (call.url === "/browser/session" && call.method === "GET") {
      const currentEpoch = typeof familyEpoch === "function" ? familyEpoch() : familyEpoch;
      return responseJSON(200, {
        client_id: call.clientID,
        epoch: currentEpoch,
      });
    }
    if (responder) {
      const response = await responder(call, fetchCalls);
      if (call.url === "/api/browser/session" && response.ok) {
        const payload = await response.clone().json();
        return responseJSON(response.status, {
          client_id: call.clientID || "client_cookie_default",
          epoch: 1,
          ...payload,
        });
      }
      return response;
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
  browserSessionFetch,
  assertBrowserSessionResponseCurrent,
  releaseBrowserSessionResponse,
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
    setOrigin(origin) {
      context.window.location.origin = origin;
    },
  };
}

const persistedClientID = "client_existing_0123456789";
const cookieClientID = "client_cookie_family_9876543210";
const cookieFirst = createHarness({
  localEntries: [
    ["kbase.browser-client-id", persistedClientID],
    ["kbase.token", "legacy-token-must-not-run"],
  ],
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "existing-cookie-session" },
        client_id: cookieClientID,
        epoch: 7,
        csrf_token: "existing-cookie-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    throw new Error(`cookie status must win before migration: ${call.method} ${call.url}`);
  },
});
await cookieFirst.ensureBrowserSession();
assert.equal(
  cookieFirst.fetchCalls[0]?.url,
  "/api/browser/session",
  "an existing HttpOnly Cookie must be checked before legacy migration",
);
assert.equal(
  cookieFirst.fetchCalls.some((call) => call.url === "/browser/session/migrate"),
  false,
  "an existing Cookie must not be migrated into a different client family",
);
assert.equal(cookieFirst.local.get("kbase.browser-client-id"), cookieClientID);
assert.equal(cookieFirst.browserSessionState.clientID, cookieClientID);
assert.equal(cookieFirst.browserSessionState.epoch, 7);
assert.equal(
  cookieFirst.local.has("kbase.browser-epoch"),
  false,
  "epoch must never be persisted",
);

let generatedStatusCount = 0;
const generatedClient = createHarness({
  localEntries: [["kbase.browser-client-id", "错误 client id"]],
  randomByte: 0x2a,
  responder(call) {
    if (call.url === "/api/browser/session") {
      generatedStatusCount += 1;
      if (generatedStatusCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, {
        session: { id: "generated-client-session" },
        client_id: call.clientID || generatedClient.local.get("kbase.browser-client-id"),
        epoch: 1,
        csrf_token: "generated-client-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/browser/session" && call.method === "POST") {
      assert.match(call.clientID, /^[A-Za-z0-9_-]{16,128}$/);
      assert.equal(call.epoch, "1");
      return responseJSON(200, {
        session: { id: "generated-client-session" },
        client_id: call.clientID,
        epoch: 1,
      });
    }
    throw new Error(`unexpected generated client fetch ${call.method} ${call.url}`);
  },
});
await generatedClient.ensureBrowserSession();
const generatedClientID = generatedClient.local.get("kbase.browser-client-id");
assert.match(generatedClientID, /^[A-Za-z0-9_-]{16,128}$/);
assert.notEqual(generatedClientID, "错误 client id");
assert.equal(generatedClient.browserSessionState.clientID, generatedClientID);
assert.equal(generatedClient.browserSessionState.epoch, 1);
assert.equal(
  generatedClient.fetchCalls.filter(
    (call) => call.url === "/browser/session" && call.method === "GET",
  ).length,
  1,
  "a no-Cookie login must acquire the current family epoch once",
);
assert.equal(
  generatedClient.fetchCalls.filter(
    (call) => call.url === "/browser/session" && call.method === "POST",
  ).length,
  1,
);

const restartedClient = createHarness({
  localEntries: [...generatedClient.local.entries()],
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "restarted-client-session" },
        client_id: generatedClientID,
        epoch: 1,
        csrf_token: "restarted-client-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    throw new Error(`unexpected restarted client fetch ${call.method} ${call.url}`);
  },
});
await restartedClient.ensureBrowserSession();
assert.equal(
  restartedClient.local.get("kbase.browser-client-id"),
  generatedClientID,
  "the non-sensitive client identity must remain stable across page restarts",
);

let overlongStatusCount = 0;
const overlongClient = createHarness({
  localEntries: [["kbase.browser-client-id", "a".repeat(129)]],
  randomByte: 0x4b,
  responder(call) {
    if (call.url === "/api/browser/session") {
      overlongStatusCount += 1;
      if (overlongStatusCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, {
        session: { id: "overlong-replacement-session" },
        client_id: overlongClient.local.get("kbase.browser-client-id"),
        epoch: 1,
        csrf_token: "overlong-replacement-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/browser/session" && call.method === "POST") {
      return responseJSON(200, {
        session: { id: "overlong-replacement-session" },
        client_id: call.clientID,
        epoch: 1,
      });
    }
    throw new Error(`unexpected overlong client fetch ${call.method} ${call.url}`);
  },
});
await overlongClient.ensureBrowserSession();
assert.match(overlongClient.local.get("kbase.browser-client-id"), /^[A-Za-z0-9_-]{16,128}$/);
assert.notEqual(overlongClient.local.get("kbase.browser-client-id"), "a".repeat(129));

const legacySecret = "legacy-secret-value";
let migrationStatusCount = 0;
const migration = createHarness({
  localEntries: [
    ["kbase.token", legacySecret],
    ["kbaseToken", legacySecret],
  ],
  sessionEntries: [["KBASE_AUTH_TOKEN", legacySecret]],
  responder(call) {
    if (call.url === "/api/browser/session") {
      migrationStatusCount += 1;
      if (migrationStatusCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, {
        session: { id: "session-migrated" },
        client_id: call.clientID || migration.local.get("kbase.browser-client-id"),
        epoch: 1,
        csrf_token: "csrf-migrated",
        csrf_expires_at: "2026-07-28T22:15:00Z",
      });
    }
    if (call.url === "/browser/session/migrate") {
      assert.equal(call.authorization, `Bearer ${legacySecret}`);
      assert.match(call.clientID, /^[A-Za-z0-9_-]{16,128}$/);
      assert.equal(call.epoch, "1");
      return responseJSON(200, {
        session: { id: "session-migrated" },
        client_id: call.clientID,
        epoch: 1,
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
  2,
  "concurrent requests should share both Cookie checks",
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

for (const confirmationStatus of [401, 503]) {
  const unconfirmedSecret = `unconfirmed-migration-${confirmationStatus}`;
  let unconfirmedStatusCount = 0;
  const unconfirmedMigration = createHarness({
    localEntries: [["kbase.token", unconfirmedSecret]],
    sessionEntries: [["KBASE_AUTH_TOKEN", unconfirmedSecret]],
    responder(call) {
      if (call.url === "/api/browser/session") {
        unconfirmedStatusCount += 1;
        return responseJSON(
          unconfirmedStatusCount === 1 ? 401 : confirmationStatus,
          { error: confirmationStatus === 401 ? "unauthorized" : "service unavailable" },
        );
      }
      if (call.url === "/browser/session/migrate") {
        return responseJSON(200, {
          session: { id: `unconfirmed-${confirmationStatus}` },
          client_id: call.clientID,
          epoch: Number(call.epoch),
        });
      }
      throw new Error(`unexpected unconfirmed migration fetch ${call.method} ${call.url}`);
    },
  });
  await assert.rejects(
    unconfirmedMigration.ensureBrowserSession(),
    (error) => error.status === confirmationStatus,
  );
  assert.equal(
    unconfirmedMigration.local.get("kbase.token"),
    unconfirmedSecret,
    `migration ${confirmationStatus} confirmation failure must preserve localStorage`,
  );
  assert.equal(
    unconfirmedMigration.session.get("KBASE_AUTH_TOKEN"),
    unconfirmedSecret,
    `migration ${confirmationStatus} confirmation failure must preserve sessionStorage`,
  );
}

const mismatchedMigrationSecret = "mismatched-migration-secret";
let mismatchedMigrationStatusCount = 0;
const mismatchedMigration = createHarness({
  localEntries: [["kbase.token", mismatchedMigrationSecret]],
  responder(call) {
    if (call.url === "/api/browser/session") {
      mismatchedMigrationStatusCount += 1;
      if (mismatchedMigrationStatusCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, {
        session: { id: "different-cookie-session" },
        client_id: "client_different_cookie_family_123",
        epoch: 2,
        csrf_token: "different-cookie-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/browser/session/migrate") {
      return responseJSON(200, {
        session: { id: "migrated-cookie-session" },
        client_id: call.clientID,
        epoch: Number(call.epoch),
      });
    }
    throw new Error(`unexpected mismatched migration fetch ${call.method} ${call.url}`);
  },
});
await mismatchedMigration.ensureBrowserSession();
assert.equal(
  mismatchedMigration.local.get("kbase.token"),
  mismatchedMigrationSecret,
  "a different confirmed client family must not delete the migrated legacy token",
);

const transientSecret = "transient-migration-secret";
const malformedLegacyValue = "错误 token";
const transientMigration = createHarness({
  localEntries: [
    ["kbase.token", transientSecret],
    ["kbaseToken", malformedLegacyValue],
  ],
  sessionEntries: [["KBASE_AUTH_TOKEN", transientSecret]],
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(401, { error: "unauthorized" });
    }
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

let privateStorageStatusCount = 0;
let privateStorageClientID = "";
const privateStorage = createHarness({
  storageAccessError: true,
  responder(call) {
    if (call.url === "/api/browser/session") {
      privateStorageStatusCount += 1;
      if (privateStorageStatusCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, {
        session: { id: "private-storage-session" },
        client_id: privateStorageClientID,
        epoch: 1,
        csrf_token: "private-storage-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/browser/session" && call.method === "POST") {
      privateStorageClientID = call.clientID;
      return responseJSON(200, {
        session: { id: "private-storage-session" },
        client_id: call.clientID,
        epoch: Number(call.epoch),
      });
    }
    throw new Error(`unexpected private storage fetch ${call.method} ${call.url}`);
  },
});
await privateStorage.ensureBrowserSession();
assert.match(privateStorageClientID, /^[A-Za-z0-9_-]{16,128}$/);
assert.equal(privateStorage.browserSessionState.ready, true);
assert.equal(
  privateStorage.fetchCalls.filter(
    (call) => call.url === "/browser/session" && call.method === "POST",
  ).length,
  1,
  "blocked storage getters must not prevent interactive Cookie login",
);

const rejectedSecret = "rejected-migration-secret";
let rejectedMigrationStatusCount = 0;
const rejectedMigration = createHarness({
  localEntries: [["kbase.token", rejectedSecret]],
  sessionEntries: [["KBASE_AUTH_TOKEN", rejectedSecret]],
  responder(call) {
    if (call.url === "/api/browser/session") {
      rejectedMigrationStatusCount += 1;
      if (rejectedMigrationStatusCount === 1) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, {
        session: { id: "replacement-session" },
        client_id: call.clientID || rejectedMigration.local.get("kbase.browser-client-id"),
        epoch: 1,
        csrf_token: "replacement-csrf",
        csrf_expires_at: "2026-07-28T22:15:00Z",
      });
    }
    if (call.url === "/browser/session/migrate") {
      return responseJSON(401, { error: "unauthorized" });
    }
    if (call.url === "/browser/session") {
      return responseJSON(200, { id: "replacement-session" });
    }
    throw new Error(`unexpected rejected migration fetch ${call.method} ${call.url}`);
  },
});
await rejectedMigration.ensureBrowserSession();
assert.equal(rejectedMigration.local.has("kbase.token"), false);
assert.equal(rejectedMigration.session.has("KBASE_AUTH_TOKEN"), false);

const fencedMigrationSecret = "fenced-migration-secret";
let fencedMigrationCount = 0;
const fencedMigration = createHarness({
  localEntries: [["kbase.token", fencedMigrationSecret]],
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(401, { error: "unauthorized" });
    }
    if (call.url === "/browser/session/migrate") {
      fencedMigrationCount += 1;
      return responseJSON(409, {
        error: "stale browser session epoch",
        client_id: call.clientID,
        epoch: 2,
      });
    }
    throw new Error(`unexpected fenced migration fetch ${call.method} ${call.url}`);
  },
});
await assert.rejects(
  fencedMigration.ensureBrowserSession(),
  (error) => error.status === 409,
);
assert.equal(fencedMigrationCount, 1, "stale migration must not retry");
assert.equal(
  fencedMigration.local.get("kbase.token"),
  fencedMigrationSecret,
  "stale migration must preserve the legacy token for a later page-load attempt",
);
assert.ok(fencedMigration.broadcasts.some((message) => message?.type === "logout"));

let staleEpoch = 1;
let staleEpochStatusCount = 0;
let staleEpochLoginCount = 0;
const staleEpochLogin = createHarness({
  familyEpoch: () => staleEpoch,
  responder(call) {
    if (call.url === "/api/browser/session") {
      staleEpochStatusCount += 1;
      if (staleEpochStatusCount < 3) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, {
        session: { id: "post-fence-session" },
        client_id: call.clientID || staleEpochLogin.local.get("kbase.browser-client-id"),
        epoch: 2,
        csrf_token: "post-fence-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/browser/session" && call.method === "POST") {
      staleEpochLoginCount += 1;
      if (staleEpochLoginCount === 1) {
        assert.equal(call.epoch, "1");
        staleEpoch = 2;
        return responseJSON(409, {
          error: "stale browser session epoch",
          client_id: call.clientID,
          epoch: 2,
        });
      }
      assert.equal(call.epoch, "2");
      return responseJSON(200, {
        session: { id: "post-fence-session" },
        client_id: call.clientID,
        epoch: 2,
      });
    }
    throw new Error(`unexpected stale epoch fetch ${call.method} ${call.url}`);
  },
});
await assert.rejects(
  staleEpochLogin.ensureBrowserSession(),
  (error) => error.status === 409,
  "a stale family epoch must surface without automatic login retry",
);
assert.equal(staleEpochLoginCount, 1);
assert.equal(staleEpochStatusCount, 1);
assert.ok(
  staleEpochLogin.broadcasts.some((message) => message?.type === "logout"),
  "stale epoch must notify other tabs of the family fence",
);
await staleEpochLogin.ensureBrowserSession();
assert.equal(staleEpochLoginCount, 2, "the next explicit action may acquire the new epoch and login");
assert.equal(staleEpochLogin.browserSessionState.epoch, 2);

let duplicateSignalStatusCount = 0;
let markDuplicateSignalRequestStarted;
let releaseDuplicateSignalRequest;
const duplicateSignalRequestStarted = new Promise((resolve) => {
  markDuplicateSignalRequestStarted = resolve;
});
const duplicateSignalRequestGate = new Promise((resolve) => {
  releaseDuplicateSignalRequest = resolve;
});
const duplicateSignal = createHarness({
  responder: async (call) => {
    if (call.url === "/api/browser/session") {
      duplicateSignalStatusCount += 1;
      return responseJSON(200, {
        session: { id: `duplicate-signal-session-${duplicateSignalStatusCount}` },
        client_id: "client_duplicate_signal_123",
        epoch: 1,
        csrf_token: `duplicate-signal-csrf-${duplicateSignalStatusCount}`,
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/duplicate-signal") {
      markDuplicateSignalRequestStarted();
      await duplicateSignalRequestGate;
      return responseJSON(200, { ok: true });
    }
    throw new Error(`unexpected duplicate signal fetch ${call.method} ${call.url}`);
  },
});
await duplicateSignal.ensureBrowserSession();
const duplicateHint = {
  type: "login",
  at: 1,
  nonce: "same-signal-over-two-transports",
};
duplicateSignal.emitBroadcast(duplicateHint);
const duplicateSignalRequest = duplicateSignal.apiFetch("/api/duplicate-signal");
await duplicateSignalRequestStarted;
const generationAfterFirstSignal = duplicateSignal.browserSessionState.generation;
duplicateSignal.emitStorage(duplicateHint);
assert.equal(
  duplicateSignal.browserSessionState.generation,
  generationAfterFirstSignal,
  "the same nonce delivered over BroadcastChannel and storage must reset once",
);
releaseDuplicateSignalRequest();
assert.equal(
  (await duplicateSignalRequest).ok,
  true,
  "a duplicate transport delivery must not abort work started after the first signal",
);

for (const signalCase of [
  { type: "logout-start", transport: "broadcast" },
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
staleLogin.emitStorage({ type: "logout-start", at: 1, nonce: "logout-during-login" });
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

let staleRecoveryStatusCount = 0;
let staleRecoveryLoginCount = 0;
let staleRecoveryRequestCount = 0;
let markStaleRecoveryLoginStarted;
let releaseStaleRecoveryLogin;
const staleRecoveryLoginStarted = new Promise((resolve) => {
  markStaleRecoveryLoginStarted = resolve;
});
const delayedStaleRecoveryLogin = new Promise((resolve) => {
  releaseStaleRecoveryLogin = resolve;
});
const staleRecovery = createHarness({
  responder: async (call) => {
    if (call.url === "/api/browser/session") {
      staleRecoveryStatusCount += 1;
      return responseJSON(200, {
        session: { id: `stale-recovery-${staleRecoveryStatusCount}` },
        client_id: "client_stale_recovery_123",
        epoch: 1,
        csrf_token: `stale-recovery-csrf-${staleRecoveryStatusCount}`,
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/browser/session" && call.method === "POST") {
      staleRecoveryLoginCount += 1;
      markStaleRecoveryLoginStarted();
      await delayedStaleRecoveryLogin;
      return responseJSON(200, {
        session: { id: "stale-recovery-login" },
        client_id: call.clientID,
        epoch: Number(call.epoch),
      });
    }
    if (call.url === "/api/stale-recovery") {
      staleRecoveryRequestCount += 1;
      return staleRecoveryRequestCount === 1
        ? responseJSON(401, { error: "unauthorized" })
        : responseJSON(200, { ok: true });
    }
    throw new Error(`unexpected stale recovery fetch ${call.method} ${call.url}`);
  },
});
const staleRecoveryRequest = staleRecovery.apiFetch("/api/stale-recovery");
await staleRecoveryLoginStarted;
staleRecovery.emitBroadcast({
  type: "logout-start",
  at: 1,
  nonce: "logout-during-recovery",
});
releaseStaleRecoveryLogin();
await assert.rejects(
  staleRecoveryRequest,
  (error) => error.code === "browser_session_stale" || error.name === "AbortError",
  "cross-tab logout-start must invalidate an in-flight recovery",
);
assert.equal(staleRecoveryLoginCount, 1);
assert.equal(staleRecoveryRequestCount, 1, "stale recovery must not retry old API work");
assert.equal(staleRecovery.browserSessionState.ready, false);

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
  "//kbase.example/api/write",
  "https://user:password@kbase.example/api/write",
  new Request("https://evil.example/api/write", { method: "POST" }),
  new URL("https://kbase.example/api/write"),
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

const mutableURLHarness = createHarness({
  responder(call) {
    throw new Error(`mutable URL reached fetch: ${call.method} ${call.url}`);
  },
});
const mutableURL = new URL("https://kbase.example/api/write");
const mutableURLRequest = mutableURLHarness.apiFetch(mutableURL, { method: "POST" });
mutableURL.hostname = "evil.example";
await assert.rejects(
  mutableURLRequest,
  (error) => error.code === "cross_origin_request",
  "URL objects must be rejected before later mutation can redirect credentials",
);
assert.equal(mutableURLHarness.fetchCalls.length, 0);

let sameOriginStatusCount = 0;
const sameOrigin = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      sameOriginStatusCount += 1;
      return responseJSON(200, {
        session: { id: "same-origin-session" },
        client_id: "client_same_origin_123456",
        epoch: 1,
        csrf_token: "same-origin-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/same-origin") {
      assert.equal(call.target, "https://kbase.example/api/same-origin");
      assert.equal(call.csrf, "same-origin-csrf");
      return responseJSON(200, { ok: true });
    }
    throw new Error(`unexpected same-origin fetch ${call.method} ${call.url}`);
  },
});
assert.equal(
  (await sameOrigin.apiFetch("https://kbase.example/api/same-origin", {
    method: "POST",
  })).ok,
  true,
);
assert.equal(sameOriginStatusCount, 1);

const stableBase = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "stable-base-session" },
        client_id: "client_stable_base_123456",
        epoch: 1,
        csrf_token: "stable-base-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/stable-base") {
      assert.equal(
        call.target,
        "https://kbase.example/api/stable-base",
        "the validated target must remain an immutable absolute URL",
      );
      assert.equal(call.csrf, "stable-base-csrf");
      return responseJSON(200, { stable: true });
    }
    throw new Error(`unexpected stable-base fetch ${call.method} ${call.target}`);
  },
});
const stableBaseRequest = stableBase.apiFetch("/api/stable-base", { method: "POST" });
stableBase.setOrigin("https://evil.example");
assert.equal((await stableBaseRequest).stable, true);
assert.equal(
  stableBase.fetchCalls.filter((call) => call.url === "/api/stable-base").length,
  1,
);

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

let joinedRecoveryStatusCount = 0;
let joinedRecoveryLoginCount = 0;
let joinedRecoveryOwnerCount = 0;
let joinedRecoveryLoginResolved = false;
let markJoinedRecoveryLoginStarted;
let releaseJoinedRecoveryLogin;
const joinedRecoveryLoginStarted = new Promise((resolve) => {
  markJoinedRecoveryLoginStarted = resolve;
});
const joinedRecoveryLogin = new Promise((resolve) => {
  releaseJoinedRecoveryLogin = resolve;
});
const recoveryWithNewRequest = createHarness({
  responder: async (call) => {
    if (call.url === "/api/browser/session") {
      joinedRecoveryStatusCount += 1;
      if (joinedRecoveryStatusCount === 1 || joinedRecoveryLoginResolved) {
        return responseJSON(200, {
          session: { id: `joined-recovery-${joinedRecoveryStatusCount}` },
          client_id: call.clientID || "client_joined_recovery_123",
          epoch: 1,
          csrf_token: `joined-recovery-csrf-${joinedRecoveryStatusCount}`,
          csrf_expires_at: "2999-01-01T00:00:00Z",
        });
      }
      return responseJSON(401, { error: "unauthorized" });
    }
    if (call.url === "/browser/session" && call.method === "POST") {
      joinedRecoveryLoginCount += 1;
      if (joinedRecoveryLoginCount === 1) {
        markJoinedRecoveryLoginStarted();
        await joinedRecoveryLogin;
        joinedRecoveryLoginResolved = true;
      }
      return responseJSON(200, {
        session: { id: "joined-recovery-session" },
        client_id: call.clientID,
        epoch: Number(call.epoch),
      });
    }
    if (call.url === "/api/recovery-owner") {
      joinedRecoveryOwnerCount += 1;
      return joinedRecoveryOwnerCount === 1
        ? responseJSON(401, { error: "unauthorized" })
        : responseJSON(200, { owner: true });
    }
    if (call.url === "/api/recovery-joiner") {
      return responseJSON(200, { joiner: true });
    }
    throw new Error(`unexpected joined recovery fetch ${call.method} ${call.url}`);
  },
});
const recoveryOwner = recoveryWithNewRequest.apiFetch("/api/recovery-owner");
await joinedRecoveryLoginStarted;
const recoveryJoiner = recoveryWithNewRequest.apiFetch("/api/recovery-joiner");
await new Promise((resolve) => setImmediate(resolve));
assert.equal(
  joinedRecoveryLoginCount,
  1,
  "a new request must join an active recovery instead of starting another login",
);
releaseJoinedRecoveryLogin();
const [ownerPayload, joinerPayload] = await Promise.all([recoveryOwner, recoveryJoiner]);
assert.equal(ownerPayload.owner, true);
assert.equal(joinerPayload.joiner, true);
assert.equal(joinedRecoveryLoginCount, 1);

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
assert.equal(
  csrfThenUnauthorized.browserSessionState.operationControllers.size,
  0,
  "a 401 retry must release the old request operation",
);

for (const status of [403, 409, 503]) {
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

let markLateJSONHeadersStarted;
let releaseLateJSONHeaders;
const lateJSONHeadersStarted = new Promise((resolve) => {
  markLateJSONHeadersStarted = resolve;
});
const lateJSONHeaders = new Promise((resolve) => {
  releaseLateJSONHeaders = resolve;
});
let lateJSONCall = null;
const lateJSON = createHarness({
  responder: async (call) => {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "late-json-session" },
        client_id: "client_late_json_123456",
        epoch: 1,
        csrf_token: "late-json-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/late-json") {
      lateJSONCall = call;
      markLateJSONHeadersStarted();
      await lateJSONHeaders;
      return responseJSON(200, { stale: "must-not-render" });
    }
    throw new Error(`unexpected late JSON fetch ${call.method} ${call.target}`);
  },
});
const lateJSONRequest = lateJSON.apiFetch("/api/late-json");
await lateJSONHeadersStarted;
lateJSON.emitBroadcast({ type: "logout", at: 1, nonce: "late-json-logout" });
releaseLateJSONHeaders();
await assert.rejects(
  lateJSONRequest,
  (error) => error.code === "browser_session_stale",
  "a delayed successful API response must not reach rendering after logout",
);
assert.equal(lateJSONCall?.signal?.aborted, true, "logout must abort the in-flight API request");
assert.equal(lateJSON.browserSessionState.operationControllers.size, 0);

const lateDownloadBody = deferredBodyResponse(
  "downloaded-after-logout",
  "application/octet-stream",
);
let lateDownloadCall = null;
const lateDownload = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "late-download-session" },
        client_id: "client_late_download_123",
        epoch: 1,
        csrf_token: "late-download-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/late-download") {
      lateDownloadCall = call;
      return lateDownloadBody.response;
    }
    throw new Error(`unexpected late download fetch ${call.method} ${call.target}`);
  },
});
const lateDownloadRequest = lateDownload.apiDownload(
  "/api/late-download",
  {},
  "stale.bin",
);
await lateDownloadBody.bodyStarted;
lateDownload.emitStorage({ type: "logout", at: 1, nonce: "late-download-logout" });
lateDownloadBody.releaseBody();
await assert.rejects(
  lateDownloadRequest,
  (error) => error.code === "browser_session_stale",
  "a body completing after logout must not be saved",
);
assert.equal(lateDownloadCall?.signal?.aborted, true);
assert.equal(lateDownload.links.length, 0);
assert.equal(lateDownload.objectURLs.length, 0);
assert.equal(lateDownload.browserSessionState.operationControllers.size, 0);

const finalAPICommitBody = deferredBodyResponse(
  JSON.stringify({ stale: "must-not-return" }),
  "application/json",
);
const finalAPICommit = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "final-api-commit-session" },
        client_id: "client_final_api_commit_123",
        epoch: 1,
        csrf_token: "final-api-commit-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/final-api-commit") {
      return finalAPICommitBody.response;
    }
    throw new Error(`unexpected final API commit fetch ${call.method} ${call.target}`);
  },
});
const finalAPICommitRequest = finalAPICommit.apiFetch("/api/final-api-commit");
await finalAPICommitBody.bodyStarted;
finalAPICommitBody.releaseBody(() => {
  finalAPICommit.emitBroadcast({
    type: "logout",
    at: 4,
    nonce: "final-api-commit-logout",
  });
});
await assert.rejects(
  finalAPICommitRequest,
  (error) => error.code === "browser_session_stale",
  "API payload must be fenced when logout lands after body completion but before return",
);
assert.equal(finalAPICommit.browserSessionState.operationControllers.size, 0);

const finalDownloadCommitBody = deferredBodyResponse(
  "download-must-not-commit",
  "application/octet-stream",
);
const finalDownloadCommit = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "final-download-commit-session" },
        client_id: "client_final_download_commit_123",
        epoch: 1,
        csrf_token: "final-download-commit-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/final-download-commit") {
      return finalDownloadCommitBody.response;
    }
    throw new Error(`unexpected final download commit fetch ${call.method} ${call.target}`);
  },
});
const finalDownloadCommitRequest = finalDownloadCommit.apiDownload(
  "/api/final-download-commit",
  {},
  "stale-final.bin",
);
await finalDownloadCommitBody.bodyStarted;
finalDownloadCommitBody.releaseBody(() => {
  finalDownloadCommit.emitStorage({
    type: "logout",
    at: 5,
    nonce: "final-download-commit-logout",
  });
});
await assert.rejects(
  finalDownloadCommitRequest,
  (error) => error.code === "browser_session_stale",
  "download side effects must be fenced after body completion",
);
assert.equal(finalDownloadCommit.objectURLs.length, 0);
assert.equal(finalDownloadCommit.links.length, 0);
assert.equal(finalDownloadCommit.browserSessionState.operationControllers.size, 0);

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

for (const logoutStatus of [204, 503]) {
  let markDelayedLogoutStarted;
  let releaseDelayedLogout;
  const delayedLogoutStarted = new Promise((resolve) => {
    markDelayedLogoutStarted = resolve;
  });
  const delayedLogoutGate = new Promise((resolve) => {
    releaseDelayedLogout = resolve;
  });
  const supersededLogout = createHarness({
    responder: async (call) => {
      if (call.url === "/api/browser/session") {
        return responseJSON(200, {
          session: { id: `superseded-logout-${logoutStatus}` },
          client_id: `client_superseded_logout_${logoutStatus}`,
          epoch: 1,
          csrf_token: `csrf-superseded-${logoutStatus}`,
          csrf_expires_at: "2999-01-01T00:00:00Z",
        });
      }
      if (call.url === "/api/browser/session/logout") {
        markDelayedLogoutStarted();
        await delayedLogoutGate;
        return logoutStatus === 204
          ? new Response(null, { status: 204 })
          : responseJSON(503, { error: "logout unavailable" });
      }
      throw new Error(`unexpected superseded logout fetch ${call.method} ${call.target}`);
    },
  });
  await supersededLogout.ensureBrowserSession();
  const delayedLogoutRequest = supersededLogout.logoutBrowserSession();
  await delayedLogoutStarted;
  supersededLogout.emitBroadcast({
    type: "login",
    at: 2,
    nonce: `newer-login-${logoutStatus}`,
  });
  releaseDelayedLogout();
  await assert.rejects(
    delayedLogoutRequest,
    (error) => error.code === "browser_session_stale",
    `a ${logoutStatus} logout completion must not overwrite a newer cross-tab login`,
  );
  assert.equal(supersededLogout.browserSessionState.invalidationReason, "login");
  assert.equal(supersededLogout.browserSessionState.logoutPending, false);
  assert.deepEqual(
    supersededLogout.broadcasts.map((message) => message.type),
    ["logout-start"],
    "a superseded logout completion must not broadcast stale logout or login",
  );
}

let logoutBarrierAPIRequests = 0;
let logoutBarrierLoginRequests = 0;
let logoutBarrierEpoch = 1;
let logoutBarrierLoggedOut = false;
let logoutStartSeenBeforeHTTP = false;
let markLogoutBarrierAPIStarted;
let releaseLogoutBarrierAPI;
let markLogoutBarrierHTTPStarted;
let releaseLogoutBarrierHTTP;
const logoutBarrierAPIStarted = new Promise((resolve) => {
  markLogoutBarrierAPIStarted = resolve;
});
const delayedLogoutBarrierAPI = new Promise((resolve) => {
  releaseLogoutBarrierAPI = resolve;
});
const logoutBarrierHTTPStarted = new Promise((resolve) => {
  markLogoutBarrierHTTPStarted = resolve;
});
const delayedLogoutBarrierHTTP = new Promise((resolve) => {
  releaseLogoutBarrierHTTP = resolve;
});
let logoutBarrier;
logoutBarrier = createHarness({
  familyEpoch: () => logoutBarrierEpoch,
  responder: async (call) => {
    if (call.url === "/api/browser/session") {
      if (logoutBarrierLoggedOut && logoutBarrierLoginRequests === 0) {
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, {
        session: {
          id: logoutBarrierLoggedOut
            ? "logout-barrier-new-session"
            : "logout-barrier-session",
        },
        client_id: "client_logout_barrier_123",
        epoch: logoutBarrierEpoch,
        csrf_token: logoutBarrierLoggedOut
          ? "logout-barrier-new-csrf"
          : "logout-barrier-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/logout-barrier") {
      logoutBarrierAPIRequests += 1;
      if (logoutBarrierAPIRequests === 1) {
        markLogoutBarrierAPIStarted();
        await delayedLogoutBarrierAPI;
        return responseJSON(401, { error: "unauthorized" });
      }
      return responseJSON(200, { ok: true });
    }
    if (call.url === "/browser/session" && call.method === "POST") {
      logoutBarrierLoginRequests += 1;
      assert.equal(call.epoch, "2");
      return responseJSON(200, {
        session: { id: "logout-barrier-recovered" },
        client_id: call.clientID,
        epoch: Number(call.epoch),
      });
    }
    if (call.url === "/api/browser/session/logout") {
      logoutStartSeenBeforeHTTP = logoutBarrier.broadcasts.some(
        (message) => message?.type === "logout-start",
      );
      markLogoutBarrierHTTPStarted();
      await delayedLogoutBarrierHTTP;
      logoutBarrierEpoch = 2;
      logoutBarrierLoggedOut = true;
      return new Response(null, { status: 204 });
    }
    if (call.url === "/api/after-logout") {
      return responseJSON(200, { fresh: true });
    }
    throw new Error(`unexpected logout barrier fetch ${call.method} ${call.url}`);
  },
});
const oldAPIRequest = logoutBarrier.apiFetch("/api/logout-barrier");
await logoutBarrierAPIStarted;
const logoutRequest = logoutBarrier.logoutBrowserSession();
await logoutBarrierHTTPStarted;
releaseLogoutBarrierAPI();
const oldAPIOutcome = await oldAPIRequest.then(
  (value) => ({ value }),
  (error) => ({ error }),
);
releaseLogoutBarrierHTTP();
await logoutRequest;
assert.equal(logoutStartSeenBeforeHTTP, true, "logout-start must be broadcast before HTTP logout");
assert.equal(oldAPIOutcome.error?.status, 401, "a pre-logout API 401 must surface unchanged");
assert.equal(logoutBarrierLoginRequests, 0, "logout must fence late 401 recovery");
assert.equal(logoutBarrierAPIRequests, 1, "logout must prevent retry of pre-barrier work");
assert.deepEqual(
  logoutBarrier.broadcasts
    .filter((message) => message?.type?.startsWith("logout"))
    .map((message) => message.type),
  ["logout-start", "logout"],
);
assert.equal(
  (await logoutBarrier.apiFetch("/api/after-logout")).fresh,
  true,
  "the next user action after logout may acquire the fenced epoch and login",
);
assert.equal(logoutBarrierLoginRequests, 1);
assert.equal(logoutBarrier.browserSessionState.epoch, 2);

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

function privateAssetFixture(sourcePath) {
  const classes = new Set(["is-loading"]);
  const status = {
    textContent: "图片加载中",
    removed: false,
    remove() {
      this.removed = true;
    },
  };
  const figure = {
    classList: {
      add(value) {
        classes.add(value);
      },
      remove(value) {
        classes.delete(value);
      },
    },
    querySelector(selector) {
      return selector === ".reader-page__image-status" ? status : null;
    },
  };
  const image = {
    dataset: { privateSrc: sourcePath },
    src: "",
    removedAttribute: "",
    closest(selector) {
      return selector === "figure" ? figure : null;
    },
    removeAttribute(name) {
      this.removedAttribute = name;
      if (name === "data-private-src") {
        delete this.dataset.privateSrc;
      }
    },
  };
  return {
    classes,
    status,
    image,
    container: {
      querySelectorAll(selector) {
        assert.equal(selector, "img[data-private-src]");
        return [image];
      },
    },
  };
}

async function exercisePrivateAsset(
  response,
  fetchPrivateAsset = null,
  responseLifecycle = null,
) {
  const fixture = privateAssetFixture("/api/source-assets/private-image");
  const calls = [];
  const created = [];
  class PrivateAssetURL extends URL {
    static createObjectURL(blob) {
      created.push(blob);
      return "blob:private-asset";
    }
  }
  const context = {
    Headers,
    URL: PrivateAssetURL,
    app: null,
    readerAssetObjectURLs: [],
    browserSessionFetch: async (path, options) => {
      const headers = options.headers instanceof Headers
        ? options.headers
        : new Headers(options.headers || {});
      calls.push({
        path,
        credentials: options.credentials,
        accept: headers.get("Accept"),
        authorization: headers.get("Authorization"),
      });
      return fetchPrivateAsset ? fetchPrivateAsset(path, options) : response;
    },
    assertBrowserSessionResponseCurrent: responseLifecycle
      ? responseLifecycle.assertBrowserSessionResponseCurrent
      : () => {},
    releaseBrowserSessionResponse: responseLifecycle
      ? responseLifecycle.releaseBrowserSessionResponse
      : () => {},
  };
  context.globalThis = context;
  vm.runInNewContext(
    `${privateAssets}\nglobalThis.__loadPrivateSourceAssets = loadPrivateSourceAssets;`,
    context,
    { filename: "frontend-web/private-assets.js" },
  );
  await context.__loadPrivateSourceAssets(fixture.container);
  return { ...fixture, calls, created, objectURLs: context.readerAssetObjectURLs };
}

const privateAssetSuccess = await exercisePrivateAsset(new Response(
  new Blob(["image"], { type: "image/png" }),
  { status: 200, headers: { "content-type": "image/png" } },
));
assert.deepEqual(privateAssetSuccess.calls, [{
  path: "/api/source-assets/private-image",
  credentials: "same-origin",
  accept: "image/*",
  authorization: null,
}]);
assert.equal(privateAssetSuccess.created[0]?.type, "image/png");
assert.equal(privateAssetSuccess.image.src, "blob:private-asset");
assert.equal(privateAssetSuccess.image.removedAttribute, "data-private-src");
assert.equal(privateAssetSuccess.classes.has("is-loading"), false);
assert.equal(privateAssetSuccess.status.removed, true);
assert.deepEqual(privateAssetSuccess.objectURLs, ["blob:private-asset"]);

const privateAssetFailure = await exercisePrivateAsset(new Response(
  new Blob(["not an image"], { type: "text/plain" }),
  { status: 200, headers: { "content-type": "text/plain" } },
));
assert.equal(privateAssetFailure.image.src, "");
assert.equal(privateAssetFailure.classes.has("is-loading"), false);
assert.equal(privateAssetFailure.classes.has("is-error"), true);
assert.match(privateAssetFailure.status.textContent, /invalid image response/);
assert.equal(privateAssetFailure.status.removed, false);

const latePrivateAssetBody = deferredBodyResponse("private-after-logout", "image/png");
let latePrivateAssetCall = null;
const latePrivateAsset = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "late-private-asset-session" },
        client_id: "client_late_private_asset_123",
        epoch: 1,
        csrf_token: "late-private-asset-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/source-assets/private-image") {
      latePrivateAssetCall = call;
      return latePrivateAssetBody.response;
    }
    throw new Error(`unexpected late private asset fetch ${call.method} ${call.target}`);
  },
});
const latePrivateAssetRequest = exercisePrivateAsset(
  null,
  latePrivateAsset.browserSessionFetch,
  latePrivateAsset,
);
await latePrivateAssetBody.bodyStarted;
latePrivateAsset.emitBroadcast({
  type: "logout",
  at: 3,
  nonce: "late-private-asset-logout",
});
latePrivateAssetBody.releaseBody();
const latePrivateAssetResult = await latePrivateAssetRequest;
assert.equal(latePrivateAssetCall?.signal?.aborted, true);
assert.equal(latePrivateAssetResult.created.length, 0);
assert.equal(latePrivateAssetResult.image.src, "");
assert.equal(latePrivateAssetResult.image.dataset.privateSrc, "/api/source-assets/private-image");
assert.equal(latePrivateAssetResult.classes.has("is-error"), true);
assert.match(latePrivateAssetResult.status.textContent, /browser session state changed/);
assert.equal(latePrivateAsset.browserSessionState.operationControllers.size, 0);

const finalPrivateAssetCommitBody = deferredBodyResponse(
  "private-image-must-not-commit",
  "image/png",
);
const finalPrivateAssetCommit = createHarness({
  responder(call) {
    if (call.url === "/api/browser/session") {
      return responseJSON(200, {
        session: { id: "final-private-asset-commit-session" },
        client_id: "client_final_private_asset_commit_123",
        epoch: 1,
        csrf_token: "final-private-asset-commit-csrf",
        csrf_expires_at: "2999-01-01T00:00:00Z",
      });
    }
    if (call.url === "/api/source-assets/private-image") {
      return finalPrivateAssetCommitBody.response;
    }
    throw new Error(`unexpected final private asset fetch ${call.method} ${call.target}`);
  },
});
const finalPrivateAssetCommitRequest = exercisePrivateAsset(
  null,
  finalPrivateAssetCommit.browserSessionFetch,
  finalPrivateAssetCommit,
);
await finalPrivateAssetCommitBody.bodyStarted;
finalPrivateAssetCommitBody.releaseBody(() => {
  finalPrivateAssetCommit.emitBroadcast({
    type: "logout",
    at: 6,
    nonce: "final-private-asset-commit-logout",
  });
});
const finalPrivateAssetCommitResult = await finalPrivateAssetCommitRequest;
assert.equal(finalPrivateAssetCommitResult.created.length, 0);
assert.equal(finalPrivateAssetCommitResult.image.src, "");
assert.equal(
  finalPrivateAssetCommitResult.image.dataset.privateSrc,
  "/api/source-assets/private-image",
);
assert.equal(finalPrivateAssetCommitResult.classes.has("is-error"), true);
assert.match(
  finalPrivateAssetCommitResult.status.textContent,
  /browser session state changed/,
);
assert.equal(finalPrivateAssetCommit.browserSessionState.operationControllers.size, 0);

console.log("browser cookie session smoke passed");
