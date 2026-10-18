#!/usr/bin/env node
/**
 * Split locales/en.json into smaller JSON files so each part can be pasted into Cursor / another LLM
 * for Persian (fa) or Hebrew (he) translation without calling external translation APIs.
 *
 * Usage (from packages/i18n):
 *   node scripts/splitEnLocaleChunks.js [chunkSize] [outDir]
 *
 * Defaults: chunkSize=80, outDir=locales/_ai_translate_chunks/en
 */
const fs = require("fs-extra");
const path = require("path");

async function main() {
  const chunkSize = Number(process.argv[2]) || 80;
  const outDir = path.resolve(
    process.argv[3] ||
      path.join(__dirname, "../locales/_ai_translate_chunks/en"),
  );
  const enPath = path.resolve(__dirname, "../locales/en.json");
  const en = await fs.readJSON(enPath);
  const keys = Object.keys(en).sort();

  await fs.ensureDir(outDir);
  let idx = 0;
  for (let i = 0; i < keys.length; i += chunkSize, idx++) {
    const slice = keys.slice(i, i + chunkSize);
    const chunk = {};
    for (const k of slice) {
      chunk[k] = en[k];
    }
    const name = `en-${String(idx).padStart(2, "0")}.json`;
    await fs.outputJSON(path.join(outDir, name), chunk, { spaces: 2 });
    console.log("wrote", name, Object.keys(chunk).length, "keys");
  }
  console.log("done =>", outDir);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
