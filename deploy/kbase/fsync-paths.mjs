#!/usr/bin/env node

import fs from "node:fs";

function fail(message) {
  process.stderr.write(`fsync-paths: ${message}\n`);
  process.exit(1);
}

function syncEntry(pathname) {
  const stat = fs.lstatSync(pathname);
  if (stat.isSymbolicLink()) {
    return;
  }
  if (!stat.isFile() && !stat.isDirectory()) {
    fail(`unsupported filesystem entry: ${pathname}`);
  }
  const flags = fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW || 0);
  const descriptor = fs.openSync(pathname, flags);
  try {
    fs.fsyncSync(descriptor);
  } finally {
    fs.closeSync(descriptor);
  }
}

function syncTree(root) {
  const stack = [{ pathname: root, visited: false }];
  while (stack.length > 0) {
    const current = stack.pop();
    const stat = fs.lstatSync(current.pathname);
    if (stat.isSymbolicLink() || stat.isFile()) {
      syncEntry(current.pathname);
      continue;
    }
    if (!stat.isDirectory()) {
      fail(`unsupported filesystem entry: ${current.pathname}`);
    }
    if (current.visited) {
      syncEntry(current.pathname);
      continue;
    }
    stack.push({ pathname: current.pathname, visited: true });
    const children = fs.readdirSync(current.pathname).sort().reverse();
    for (const child of children) {
      stack.push({
        pathname: `${current.pathname}/${child}`,
        visited: false,
      });
    }
  }
}

const operations = [];
for (let index = 2; index < process.argv.length; index += 2) {
  const operation = process.argv[index];
  const pathname = process.argv[index + 1];
  if ((operation !== "--path" && operation !== "--tree") || !pathname) {
    fail("usage: fsync-paths.mjs (--path PATH | --tree PATH)...");
  }
  operations.push({ operation, pathname });
}
if (operations.length === 0) {
  fail("at least one path is required");
}

for (const { operation, pathname } of operations) {
  if (operation === "--tree") {
    syncTree(pathname);
  } else {
    syncEntry(pathname);
  }
}
