#!/usr/bin/env node

import fs from "node:fs";
import { spawn } from "node:child_process";

const OPTION_NAMES = new Set([
  "--archive",
  "--gzip-bin",
  "--tar-bin",
  "--timeout-ms",
  "--stdout-limit-bytes",
  "--stderr-limit-bytes",
]);

function usage() {
  return [
    "Usage: archive-list.mjs",
    "  --archive PATH",
    "  --gzip-bin PATH",
    "  --tar-bin PATH",
    "  --timeout-ms N",
    "  --stdout-limit-bytes N",
    "  --stderr-limit-bytes N",
  ].join("\n");
}

function parsePositiveInteger(name, value) {
  if (!/^[1-9][0-9]*$/.test(value)) {
    throw new Error(`${name} must be a positive integer`);
  }
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed)) {
    throw new Error(`${name} exceeds the supported integer range`);
  }
  return parsed;
}

function parseOptions(argv) {
  const values = new Map();
  for (let index = 0; index < argv.length; index += 2) {
    const name = argv[index];
    const value = argv[index + 1];
    if (!OPTION_NAMES.has(name) || value === undefined || value.startsWith("--")) {
      throw new Error(usage());
    }
    if (values.has(name)) {
      throw new Error(`duplicate option: ${name}`);
    }
    values.set(name, value);
  }
  for (const name of OPTION_NAMES) {
    if (!values.has(name)) {
      throw new Error(`missing required option: ${name}\n${usage()}`);
    }
  }

  const archive = values.get("--archive");
  let archiveStat;
  try {
    archiveStat = fs.statSync(archive);
  } catch (error) {
    throw new Error(`cannot stat archive ${archive}: ${error.message}`);
  }
  if (!archiveStat.isFile()) {
    throw new Error(`archive is not a regular file: ${archive}`);
  }

  return {
    archive,
    gzipBin: values.get("--gzip-bin"),
    tarBin: values.get("--tar-bin"),
    timeoutMs: parsePositiveInteger("--timeout-ms", values.get("--timeout-ms")),
    stdoutLimit: parsePositiveInteger(
      "--stdout-limit-bytes",
      values.get("--stdout-limit-bytes"),
    ),
    stderrLimit: parsePositiveInteger(
      "--stderr-limit-bytes",
      values.get("--stderr-limit-bytes"),
    ),
  };
}

function killProcessGroup(child, signal) {
  if (!child.pid) {
    return;
  }
  try {
    process.kill(-child.pid, signal);
  } catch {
    try {
      child.kill(signal);
    } catch {
      // The process may have exited between the close check and the signal.
    }
  }
}

function describeExit(tool, code, signal, stderr) {
  const status =
    code === null ? `signal ${signal || "unknown"}` : `status ${String(code)}`;
  const detail = stderr.trim();
  return `${tool} exited with ${status}${detail ? `: ${detail}` : ""}`;
}

function listArchive(options) {
  return new Promise((resolve, reject) => {
    const gzip = spawn(options.gzipBin, ["-dc", options.archive], {
      detached: true,
      stdio: ["ignore", "pipe", "pipe"],
    });
    const tar = spawn(
      options.tarBin,
      [
        "--numeric-owner",
        "--full-time",
        "--quoting-style=escape",
        "-tvf",
        "-",
      ],
      {
        detached: true,
        stdio: ["pipe", "pipe", "pipe"],
      },
    );

    const children = [
      { child: gzip, name: "gzip" },
      { child: tar, name: "tar" },
    ];
    const stdoutChunks = [];
    const stderrChunks = [];
    let stdoutBytes = 0;
    let stderrBytes = 0;
    let firstFailure = null;
    let closedChildren = 0;
    let settled = false;

    const forceKillTimer = setTimeout(() => {
      if (!settled && firstFailure) {
        for (const { child } of children) {
          killProcessGroup(child, "SIGKILL");
        }
      }
    }, options.timeoutMs + 250);
    forceKillTimer.unref();

    const abort = (error) => {
      if (firstFailure) {
        return;
      }
      firstFailure = error;
      gzip.stdout?.unpipe(tar.stdin);
      tar.stdin?.destroy();
      for (const { child } of children) {
        killProcessGroup(child, "SIGTERM");
      }
    };

    const finishIfClosed = () => {
      if (closedChildren !== children.length || settled) {
        return;
      }
      settled = true;
      clearTimeout(timeout);
      clearTimeout(forceKillTimer);
      if (firstFailure) {
        reject(firstFailure);
        return;
      }
      resolve(Buffer.concat(stdoutChunks, stdoutBytes).toString("utf8"));
    };

    const timeout = setTimeout(() => {
      abort(new Error(`archive listing timed out after ${options.timeoutMs} ms`));
    }, options.timeoutMs);

    gzip.stdout.pipe(tar.stdin);
    tar.stdin.on("error", (error) => {
      if (error.code !== "EPIPE") {
        abort(new Error(`tar stdin failed: ${error.message}`));
      }
    });

    tar.stdout.on("data", (chunk) => {
      stdoutBytes += chunk.length;
      if (stdoutBytes > options.stdoutLimit) {
        abort(
          new Error(
            `tar stdout byte limit exceeded: ${stdoutBytes} > ${options.stdoutLimit}`,
          ),
        );
        return;
      }
      stdoutChunks.push(chunk);
    });

    for (const { child, name } of children) {
      child.stderr.on("data", (chunk) => {
        stderrBytes += chunk.length;
        if (stderrBytes > options.stderrLimit) {
          abort(
            new Error(
              `child stderr byte limit exceeded: ${stderrBytes} > ${options.stderrLimit}`,
            ),
          );
          return;
        }
        stderrChunks.push(chunk);
      });
      child.on("error", (error) => {
        abort(new Error(`failed to start ${name}: ${error.message}`));
      });
      child.on("close", (code, signal) => {
        closedChildren += 1;
        if (!firstFailure && code !== 0) {
          abort(
            new Error(
              describeExit(
                name,
                code,
                signal,
                Buffer.concat(stderrChunks, stderrBytes).toString("utf8"),
              ),
            ),
          );
        }
        finishIfClosed();
      });
    }
  });
}

const TYPE_NAMES = new Map([
  ["-", "file"],
  ["d", "directory"],
  ["l", "symlink"],
  ["h", "hardlink"],
  ["c", "character-device"],
  ["b", "block-device"],
  ["p", "fifo"],
  ["s", "socket"],
]);

function rejectControlCharacters(value, context) {
  for (const character of value) {
    const codePoint = character.codePointAt(0);
    if (
      codePoint <= 0x1f ||
      (codePoint >= 0x7f && codePoint <= 0x9f)
    ) {
      throw new Error(`${context} contains a control character`);
    }
  }
}

function decodeSafeGNUPath(value) {
  rejectControlCharacters(value, "archive member path");
  let decoded = "";
  for (let index = 0; index < value.length; index += 1) {
    const character = value[index];
    if (character !== "\\") {
      decoded += character;
      continue;
    }
    const escaped = value[index + 1];
    if (escaped === "\\") {
      decoded += "\\";
      index += 1;
      continue;
    }
    if (escaped && /[abfnrtv0-7x]/.test(escaped)) {
      throw new Error("archive member path contains a control character escape");
    }
    throw new Error("archive member path contains an unsupported GNU tar escape");
  }
  if (decoded.length === 0) {
    throw new Error("archive member path is empty");
  }
  return decoded;
}

function memberPath(type, listingPath) {
  if (type === "symlink") {
    const separator = " -> ";
    const separatorIndex = listingPath.indexOf(separator);
    if (separatorIndex <= 0) {
      throw new Error("cannot parse GNU tar symlink target");
    }
    return listingPath.slice(0, separatorIndex);
  }
  if (type === "hardlink") {
    const separator = " link to ";
    const separatorIndex = listingPath.indexOf(separator);
    if (separatorIndex <= 0) {
      throw new Error("cannot parse GNU tar hardlink target");
    }
    return listingPath.slice(0, separatorIndex);
  }
  return listingPath;
}

function parseSize(type, sizeToken) {
  if (/^[0-9]+$/.test(sizeToken)) {
    const size = Number(sizeToken);
    if (!Number.isSafeInteger(size)) {
      throw new Error(`archive member size is too large: ${sizeToken}`);
    }
    return size;
  }
  if (
    (type === "character-device" || type === "block-device") &&
    /^[0-9]+,[0-9]+$/.test(sizeToken)
  ) {
    return 0;
  }
  throw new Error(`cannot parse GNU tar member size: ${sizeToken}`);
}

function parseListing(listing) {
  if (listing.includes("\uFFFD")) {
    throw new Error("tar output is not valid UTF-8");
  }
  for (const character of listing) {
    const codePoint = character.codePointAt(0);
    if (
      character !== "\n" &&
      (codePoint <= 0x1f || (codePoint >= 0x7f && codePoint <= 0x9f))
    ) {
      throw new Error("tar output contains a control character");
    }
  }

  const lines = listing.endsWith("\n")
    ? listing.slice(0, -1).split("\n")
    : listing.split("\n");
  if (lines.length === 1 && lines[0] === "") {
    return { members: [] };
  }

  const members = lines.map((line, index) => {
    const gnuMatch =
      /^([\-dlhcbps])[rwxStTs-]{9}[.+@]?\s+[0-9]+\/[0-9]+\s+([0-9]+(?:,[0-9]+)?)\s+[0-9]{4}-[0-9]{2}-[0-9]{2}\s+[0-9]{2}:[0-9]{2}(?::[0-9]{2}(?:\.[0-9]+)?)?\s+(?:[+-][0-9]{4}\s+)?(.+)$/.exec(
        line,
      );
    const bsdMatch =
      /^([\-dlhcbps])[rwxStTs-]{9}[.+@]?\s+\d+\s+\S+\s+\S+\s+([0-9]+(?:,[0-9]+)?)\s+[A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}\s+(.+)$/.exec(
        line,
      );
    const match = gnuMatch || bsdMatch;
    if (!match) {
      throw new Error(
        `cannot parse tar verbose output at line ${index + 1}`,
      );
    }
    const type = TYPE_NAMES.get(match[1]);
    const path = decodeSafeGNUPath(memberPath(type, match[3]));
    return {
      path,
      type,
      size: parseSize(type, match[2]),
    };
  });

  return { members };
}

async function main() {
  const options = parseOptions(process.argv.slice(2));
  const listing = await listArchive(options);
  process.stdout.write(`${JSON.stringify(parseListing(listing))}\n`);
}

main().catch((error) => {
  process.stderr.write(`archive-list: ${error.message}\n`);
  process.exitCode = 1;
});
