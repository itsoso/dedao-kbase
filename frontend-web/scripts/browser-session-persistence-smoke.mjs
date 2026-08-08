import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const configPath = path.join(root, "deploy/nginx/kbase.executor.life.conf");
const config = fs.readFileSync(configPath, "utf8");
const locationsPath = path.join(
  root,
  "deploy/nginx/kbase.locations.conf.template",
);
const locations = fs.readFileSync(locationsPath, "utf8");
const rendererPath = path.join(root, "deploy/nginx/render-kbase-config.sh");
const renderer = fs.readFileSync(rendererPath, "utf8");

assert.match(
  config,
  /include \/etc\/dedao-kbase\/kbase\.locations\.conf;/,
);
assert.doesNotMatch(config, /auth_basic/);
assert.doesNotMatch(config, /persisted Bearer token/);
assert.doesNotMatch(
  renderer,
  /sed[\s\S]*KBASE_BROWSER_SESSION_SECRET|sed[\s\S]*browser_secret/,
);

function renderedLocationBlock(source, pattern) {
  const match = source.match(pattern);
  assert.ok(match, `missing nginx location matching ${pattern}`);
  return match[0];
}

const browserSession = renderedLocationBlock(
  locations,
  /location = \/browser\/session \{[^}]+\}/,
);
assert.match(browserSession, /auth_basic "dedao-kbase";/);
assert.match(
  browserSession,
  /auth_basic_user_file __KBASE_BASIC_AUTH_FILE__;/,
);
assert.match(browserSession, /proxy_set_header Authorization "";/);
assert.match(
  browserSession,
  /proxy_set_header X-KBase-Browser-Session "__KBASE_BROWSER_SESSION_SECRET__";/,
);

const migration = renderedLocationBlock(
  locations,
  /location = \/browser\/session\/migrate \{[^}]+\}/,
);
assert.match(migration, /auth_basic off;/);
assert.doesNotMatch(migration, /auth_basic_user_file/);
assert.doesNotMatch(migration, /proxy_set_header Authorization "";/);
assert.match(migration, /proxy_set_header X-KBase-Browser-Session "";/);

const retired = renderedLocationBlock(
  locations,
  /location = \/browser\/session-token \{[^}]+\}/,
);
assert.match(retired, /auth_basic off;/);
assert.doesNotMatch(retired, /auth_basic_user_file/);
assert.match(retired, /proxy_set_header Authorization "";/);
assert.match(retired, /proxy_set_header X-KBase-Browser-Session "";/);
assert.doesNotMatch(retired, /__KBASE_BROWSER_SESSION_SECRET__/);

const api = renderedLocationBlock(locations, /location \/api\/ \{[^}]+\}/);
assert.doesNotMatch(api, /auth_basic/);
assert.match(api, /proxy_pass http:\/\/__KBASE_BACKEND_ADDR__;/);
assert.doesNotMatch(api, /proxy_set_header Authorization "";/);
assert.match(api, /proxy_set_header X-KBase-Browser-Session "";/);

const rootLocations = [
  ...locations.matchAll(/\nlocation \/ \{[^}]+\}/g),
].map((match) => match[0]);
assert.equal(rootLocations.length, 1);
const staticShell = rootLocations[0];
assert.doesNotMatch(staticShell, /auth_basic/);
assert.match(staticShell, /proxy_pass http:\/\/__KBASE_BACKEND_ADDR__;/);
assert.match(staticShell, /proxy_set_header Authorization "";/);
assert.match(staticShell, /proxy_set_header X-KBase-Browser-Session "";/);

const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "kbase-nginx-render-"));
try {
  const outputPath = path.join(tempDir, "locations.conf");
  const browserProxyValue = "browser_proxy_value_0123456789abcdef";
  const result = spawnSync(
    "bash",
    [rendererPath, locationsPath, outputPath],
    {
      cwd: root,
      encoding: "utf8",
      env: {
        ...process.env,
        KBASE_BROWSER_SESSION_SECRET: browserProxyValue,
        KBASE_BACKEND_ADDR: "127.0.0.1:18719",
        KBASE_BASIC_AUTH_FILE: "/tmp/kbase-browser-auth.htpasswd",
      },
    },
  );
  assert.equal(result.status, 0, result.stderr);
  const rendered = fs.readFileSync(outputPath, "utf8");
  assert.doesNotMatch(rendered, /__[A-Z0-9_]+__/);
  assert.match(
    rendered,
    new RegExp(`X-KBase-Browser-Session "${browserProxyValue}";`),
  );
  assert.equal(
    rendered.split(`X-KBase-Browser-Session "${browserProxyValue}";`).length - 1,
    1,
  );
  assert.match(rendered, /proxy_pass http:\/\/127\.0\.0\.1:18719;/);
  assert.match(
    rendered,
    /auth_basic_user_file \/tmp\/kbase-browser-auth\.htpasswd;/,
  );
  assert.equal(fs.statSync(outputPath).mode & 0o777, 0o600);

  const invalidOutput = path.join(tempDir, "invalid.conf");
  const invalidValue = `${"x".repeat(31)}$`;
  const invalid = spawnSync(
    "bash",
    [rendererPath, locationsPath, invalidOutput],
    {
      cwd: root,
      encoding: "utf8",
      env: {
        ...process.env,
        KBASE_BROWSER_SESSION_SECRET: invalidValue,
      },
    },
  );
  assert.notEqual(invalid.status, 0);
  assert.equal(fs.existsSync(invalidOutput), false);
  assert.doesNotMatch(invalid.stderr, new RegExp(invalidValue.replace("$", "\\$")));

  const invalidPortOutput = path.join(tempDir, "invalid-port.conf");
  const invalidPort = spawnSync(
    "bash",
    [rendererPath, locationsPath, invalidPortOutput],
    {
      cwd: root,
      encoding: "utf8",
      env: {
        ...process.env,
        KBASE_BROWSER_SESSION_SECRET: browserProxyValue,
        KBASE_BACKEND_ADDR: "127.0.0.1:65536",
      },
    },
  );
  assert.notEqual(invalidPort.status, 0);
  assert.equal(fs.existsSync(invalidPortOutput), false);
} finally {
  fs.rmSync(tempDir, { recursive: true, force: true });
}

console.log("browser session persistence smoke passed");
