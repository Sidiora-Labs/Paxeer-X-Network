#!/usr/bin/env node
/**
 * Merge translated chunk JSON files into locales/fa.json and locales/he.json.
 * Expects files named fa-00.json … fa-NN.json (and he-00.json …) under chunkDir.
 *
 * Usage (from packages/i18n):
 *   node scripts/mergeAiLocaleChunks.js ./locales/_ai_translate_chunks/fa locales/fa.json --fallback-en
 *   node scripts/mergeAiLocaleChunks.js ./locales/_ai_translate_chunks/he locales/he.json --fallback-en
 *
 * Without --fallback-en: throws if any key from en.json is missing in merged chunks.
 */
const fs = require("fs-extra");
const path = require("path");

async function merge(chunkDir, outFile, enPath, fallbackEn) {
  const files = (await fs.readdir(chunkDir))
    .filter((f) => f.endsWith(".json"))
    .sort();

  if (!files.length) {
    throw new Error(`No JSON chunks under ${chunkDir}`);
  }

  const merged = {};
  for (const f of files) {
    const part = await fs.readJSON(path.join(chunkDir, f));
    Object.assign(merged, part);
  }

  const en = await fs.readJSON(enPath);
  const missing = Object.keys(en).filter((k) => !(k in merged));

  if (missing.length) {
    if (!fallbackEn) {
      throw new Error(
        `Missing ${missing.length} keys (e.g. ${missing.slice(0, 5).join(", ")}) for ${path.basename(outFile)}`,
      );
    }
    for (const k of missing) {
      merged[k] = en[k];
    }
    console.warn(
      `[mergeAiLocaleChunks] ${missing.length} keys fell back to English in ${path.basename(outFile)}`,
    );
  }

  const sorted = {};
  for (const k of Object.keys(en)) {
    sorted[k] = merged[k];
  }

  await fs.outputJSON(outFile, sorted, { spaces: 2 });
  console.log("wrote", outFile);
}

async function main() {
  const [, , chunkDir, outFile, flag] = process.argv;
  if (!chunkDir || !outFile) {
    console.error(
      "Usage: node scripts/mergeAiLocaleChunks.js <chunkDir> <outFile> [--fallback-en]",
    );
    process.exit(1);
  }
  const fallbackEn = flag === "--fallback-en";
  const enPath = path.resolve(__dirname, "../locales/en.json");
  await merge(
    path.resolve(chunkDir),
    path.resolve(outFile),
    enPath,
    fallbackEn,
  );
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
