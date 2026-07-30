import fs from "node:fs";
import path from "node:path";

function fail(message) {
  process.stderr.write(`stage-files: ${message}\n`);
  process.exitCode = 1;
}

function parseArguments(argv) {
  const files = [];
  for (let index = 0; index < argv.length; ) {
    if (argv[index] !== "--file" || index + 4 >= argv.length) {
      throw new Error(
        "usage: stage-files.mjs --file SOURCE DESTINATION MAX_BYTES MODE [--file ...]",
      );
    }
    const source = path.resolve(argv[index + 1]);
    const destination = path.resolve(argv[index + 2]);
    const maximum = Number(argv[index + 3]);
    const modeText = argv[index + 4];
    if (!Number.isSafeInteger(maximum) || maximum <= 0) {
      throw new Error("MAX_BYTES must be a positive safe integer");
    }
    if (!/^0[0-7]{3}$/.test(modeText)) {
      throw new Error("MODE must be a four-digit octal file mode");
    }
    files.push({
      source,
      destination,
      maximum,
      mode: Number.parseInt(modeText, 8),
    });
    index += 5;
  }
  if (files.length === 0) {
    throw new Error("at least one --file entry is required");
  }
  return files;
}

function validateEntries(entries) {
  const destinations = new Set();
  for (const entry of entries) {
    if (entry.source === entry.destination) {
      throw new Error("source and destination must differ");
    }
    if (destinations.has(entry.destination)) {
      throw new Error(`duplicate destination: ${entry.destination}`);
    }
    destinations.add(entry.destination);

    const sourceStat = fs.lstatSync(entry.source);
    if (!sourceStat.isFile() || sourceStat.isSymbolicLink()) {
      throw new Error(`source must be a regular file: ${entry.source}`);
    }
    if (sourceStat.size > entry.maximum) {
      throw new Error(
        `source exceeds byte limit: ${entry.source} (${sourceStat.size} > ${entry.maximum})`,
      );
    }

    const parent = path.dirname(entry.destination);
    const parentStat = fs.lstatSync(parent);
    if (!parentStat.isDirectory() || parentStat.isSymbolicLink()) {
      throw new Error(`destination parent must be a real directory: ${parent}`);
    }
    fs.realpathSync(parent);
    try {
      fs.lstatSync(entry.destination);
      throw new Error(`destination already exists: ${entry.destination}`);
    } catch (error) {
      if (error.code !== "ENOENT") {
        throw error;
      }
    }
  }
}

function copyBounded(entry) {
  const noFollow = fs.constants.O_NOFOLLOW;
  if (typeof noFollow !== "number") {
    throw new Error("O_NOFOLLOW is unavailable on this platform");
  }

  let sourceDescriptor;
  let destinationDescriptor;
  try {
    try {
      sourceDescriptor = fs.openSync(
        entry.source,
        fs.constants.O_RDONLY | noFollow,
      );
      const sourceStat = fs.fstatSync(sourceDescriptor);
      if (!sourceStat.isFile() || sourceStat.size > entry.maximum) {
        throw new Error(`source changed or exceeds byte limit: ${entry.source}`);
      }
      destinationDescriptor = fs.openSync(
        entry.destination,
        fs.constants.O_WRONLY |
          fs.constants.O_CREAT |
          fs.constants.O_EXCL |
          noFollow,
        entry.mode,
      );

      const buffer = Buffer.allocUnsafe(64 * 1024);
      let total = 0;
      while (true) {
        const remaining = entry.maximum - total;
        const readLength = Math.min(buffer.length, remaining + 1);
        const bytesRead = fs.readSync(
          sourceDescriptor,
          buffer,
          0,
          readLength,
          null,
        );
        if (bytesRead === 0) {
          break;
        }
        total += bytesRead;
        if (total > entry.maximum) {
          throw new Error(`source grew beyond byte limit: ${entry.source}`);
        }
        let written = 0;
        while (written < bytesRead) {
          written += fs.writeSync(
            destinationDescriptor,
            buffer,
            written,
            bytesRead - written,
          );
        }
      }
      fs.fchmodSync(destinationDescriptor, entry.mode);
      fs.fsyncSync(destinationDescriptor);
    } finally {
      if (destinationDescriptor !== undefined) {
        fs.closeSync(destinationDescriptor);
      }
      if (sourceDescriptor !== undefined) {
        fs.closeSync(sourceDescriptor);
      }
    }
  } catch (error) {
    try {
      fs.unlinkSync(entry.destination);
    } catch (cleanupError) {
      if (cleanupError.code !== "ENOENT") {
        throw new Error(
          `${error.message}; cannot remove partial destination: ${cleanupError.message}`,
        );
      }
    }
    throw error;
  }
}

let entries;
const created = [];
try {
  entries = parseArguments(process.argv.slice(2));
  validateEntries(entries);
  for (const entry of entries) {
    copyBounded(entry);
    created.push(entry.destination);
  }
  for (const parent of new Set(entries.map((entry) => path.dirname(entry.destination)))) {
    const descriptor = fs.openSync(parent, fs.constants.O_RDONLY);
    try {
      fs.fsyncSync(descriptor);
    } finally {
      fs.closeSync(descriptor);
    }
  }
} catch (error) {
  for (const pathname of created.reverse()) {
    try {
      fs.unlinkSync(pathname);
    } catch (cleanupError) {
      if (cleanupError.code !== "ENOENT") {
        process.stderr.write(
          `stage-files: cannot remove partial destination ${pathname}: ${cleanupError.message}\n`,
        );
      }
    }
  }
  fail(error.message);
}
