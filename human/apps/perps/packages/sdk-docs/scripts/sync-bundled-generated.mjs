#!/usr/bin/env node
/**
 * Copies `apps/ai-docs/generated` → `bundled/` verbatim. Manifest and indexes already use paths
 * relative to the generated root, so no JSON rewriting is required.
 */
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(packageRoot, "..", "..");
const src = path.join(repoRoot, "apps", "ai-docs", "generated");
const dest = path.join(packageRoot, "bundled");

if (!fs.existsSync(path.join(src, "manifest.json"))) {
  console.error(
    "[sync:bundle] Missing apps/ai-docs/generated/manifest.json — run:\n  pnpm --filter @paxeer-network/ai-docs generate",
  );
  process.exit(1);
}

fs.rmSync(dest, { recursive: true, force: true });
fs.mkdirSync(dest, { recursive: true });
fs.cpSync(src, dest, { recursive: true });
console.log("[sync:bundle] OK", { from: src, to: dest });
