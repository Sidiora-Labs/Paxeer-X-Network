#!/usr/bin/env node
import { constants } from "node:fs";
import { lstat, open, readdir } from "node:fs/promises";
import { basename, join, relative, resolve } from "node:path";
import {
  bundleScanReport,
  collectSecretNames,
  collectSecretValues,
  scanBundleArtifacts,
} from "../dist/scan.js";

const DEFAULT_ROOTS = [".next/static", "public", "out"];
const MAXIMUM_ARTIFACT_BYTES = 32 * 1024 * 1024;
const MAXIMUM_TOTAL_BYTES = 1024 * 1024 * 1024;
const MAXIMUM_ARTIFACTS = 100_000;
const explicit = process.argv.slice(2);
const roots = explicit.length > 0 ? explicit : DEFAULT_ROOTS;
let totalBytes = 0;

async function readArtifact(path) {
  const handle = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const info = await handle.stat();
    if (!info.isFile() || info.size > MAXIMUM_ARTIFACT_BYTES) throw new Error("artifact_unscannable");
    const chunks = [];
    let length = 0;
    for (;;) {
      const chunk = Buffer.alloc(Math.min(65_536, MAXIMUM_ARTIFACT_BYTES + 1 - length));
      const { bytesRead } = await handle.read(chunk, 0, chunk.length, null);
      if (bytesRead === 0) break;
      length += bytesRead;
      if (length > MAXIMUM_ARTIFACT_BYTES) throw new Error("artifact_too_large");
      chunks.push(chunk.subarray(0, bytesRead));
    }
    totalBytes += length;
    if (totalBytes > MAXIMUM_TOTAL_BYTES) throw new Error("artifact_total_too_large");
    return new Uint8Array(Buffer.concat(chunks, length));
  } finally {
    await handle.close();
  }
}

async function collect(root, base, artifacts, depth = 0) {
  if (depth > 32) throw new Error("artifact_tree_too_deep");
  let info;
  try {
    info = await lstat(root);
  } catch (error) {
    if (error.code === "ENOENT" && explicit.length === 0 && depth === 0) return;
    throw error;
  }
  if (info.isSymbolicLink()) throw new Error("artifact_symlink_refused");
  if (/^\.env(?:\.|$)/u.test(basename(root))) throw new Error("credential_artifact_refused");
  if (info.isFile()) {
    if (artifacts.length >= MAXIMUM_ARTIFACTS) throw new Error("artifact_count_exceeded");
    artifacts.push({ path: relative(base, root), bytes: await readArtifact(root) });
    return;
  }
  if (!info.isDirectory()) throw new Error("artifact_unscannable");
  for (const entry of await readdir(root, { withFileTypes: true })) {
    await collect(join(root, entry.name), base, artifacts, depth + 1);
  }
}

const base = resolve(".");
const artifacts = [];
for (const root of roots) await collect(resolve(root), base, artifacts);
if (artifacts.length === 0) throw new Error("no_browser_artifacts");
const findings = scanBundleArtifacts({
  artifacts,
  secretValues: collectSecretValues(process.env),
  secretNames: collectSecretNames(process.env),
});
process.stdout.write(`${bundleScanReport(findings)}\n`);
process.exit(findings.length === 0 ? 0 : 1);
