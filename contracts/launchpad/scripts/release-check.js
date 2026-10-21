#!/usr/bin/env node
"use strict";
const fs = require("fs");
const path = require("path");
const { verifyRelease } = require("./lib/release-check");

function main(args = process.argv.slice(2)) {
  if (args.length && (args.length !== 2 || args[0] !== "--report")) throw new Error("Usage: node scripts/release-check.js [--report FILE]");
  const result = verifyRelease();
  if (args.length) {
    const target = path.resolve(args[1]);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, JSON.stringify(result, null, 2) + "\n");
  }
  console.log(JSON.stringify({ status: result.status, sources: Object.keys(result.source_sha256).length,
    artifacts: result.artifacts.length, layouts: result.storage_layouts.length, errors: result.errors }));
  if (result.status !== "passed") process.exitCode = 1;
  return result;
}
if (require.main === module) { try { main(); } catch (error) { console.error(error.message); process.exitCode = 1; } }
module.exports = { main };
