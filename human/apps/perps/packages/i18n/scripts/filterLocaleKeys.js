const fs = require("fs");
const path = require("path");

const USAGE =
  "Usage: node filterLocaleKeys.js (--keep|-k | --remove|-r) <prefix> [--locales-dir <dir>]";

const KEEP_MODES = ["--keep", "-k"];
const REMOVE_MODES = ["--remove", "-r"];
const VALID_MODES = [...KEEP_MODES, ...REMOVE_MODES];

const DEFAULT_LOCALES_DIR = path.join(__dirname, "..", "locales");

/**
 * Filter locale JSON files by key prefix: keep or remove keys matching the prefix.
 * @param {"keep"|"remove"} mode - "keep": only keep keys with prefix; "remove": remove keys with prefix
 * @param {string} prefix - Key prefix to match (e.g. "trading." or "trading")
 * @param {string} [localesDir] - Directory containing locale JSON files (default: packages/i18n/locales)
 */
function filterLocaleKeys(mode, prefix, localesDir = DEFAULT_LOCALES_DIR) {
  if (!fs.existsSync(localesDir)) {
    console.error(`Locales directory not found: ${localesDir}`);
    process.exit(1);
  }

  const files = fs
    .readdirSync(localesDir)
    .filter((name) => name.endsWith(".json"))
    .sort();

  if (files.length === 0) {
    console.warn("No locale JSON files found.");
    return;
  }

  for (const file of files) {
    const filePath = path.join(localesDir, file);
    let content;
    try {
      content = fs.readFileSync(filePath, "utf8");
    } catch (err) {
      console.error(`Failed to read file: ${filePath}`, err.message);
      continue;
    }

    let data;
    try {
      data = JSON.parse(content);
    } catch (err) {
      console.error(`Failed to parse JSON: ${filePath}`, err.message);
      continue;
    }

    // Match key if it starts with prefix, or equals prefix without trailing dot
    // (e.g. prefix "trading." should match key "trading" as well as "trading.xxx")
    const prefixBase = prefix.endsWith(".") ? prefix.slice(0, -1) : prefix;
    const matchesPrefix = (key) => key.startsWith(prefix) || key === prefixBase;

    const filtered = {};
    for (const [key, value] of Object.entries(data)) {
      if (mode === "keep" ? matchesPrefix(key) : !matchesPrefix(key)) {
        filtered[key] = value;
      }
    }

    const filteredCount = Object.keys(filtered).length;
    if (mode === "keep" && filteredCount === 0) {
      console.warn(
        `Skip ${file} (no keys starting with "${prefix}" were found).`,
      );
      continue;
    }
    if (mode === "remove" && filteredCount === Object.keys(data).length) {
      console.warn(
        `Skip ${file} (no keys starting with "${prefix}" were found to remove).`,
      );
      continue;
    }

    try {
      fs.writeFileSync(
        filePath,
        JSON.stringify(filtered, null, 2) + "\n",
        "utf8",
      );
      if (mode === "keep") {
        console.log(
          `Filtered ${file}: kept ${filteredCount} keys with prefix "${prefix}".`,
        );
      } else {
        const removedCount = Object.keys(data).length - filteredCount;
        console.log(
          `Filtered ${file}: removed ${removedCount} keys with prefix "${prefix}", kept ${filteredCount} keys.`,
        );
      }
    } catch (err) {
      console.error(`Failed to write file: ${filePath}`, err.message);
    }
  }
}

// Run as CLI when executed directly
if (require.main === module) {
  const modeArg = process.argv[2];
  const prefix = process.argv[3];
  const localesDirIdx = process.argv.indexOf("--locales-dir");
  const localesDir =
    localesDirIdx >= 0 ? process.argv[localesDirIdx + 1] : DEFAULT_LOCALES_DIR;

  if (!VALID_MODES.includes(modeArg)) {
    console.error(
      "Error: Please specify a mode: --keep|-k (keep keys with prefix) or --remove|-r (remove keys with prefix).",
    );
    console.error(USAGE);
    process.exit(1);
  }

  if (!prefix) {
    console.error("Error: <prefix> is required.");
    console.error(USAGE);
    process.exit(1);
  }

  const mode = REMOVE_MODES.includes(modeArg) ? "remove" : "keep";
  filterLocaleKeys(mode, prefix, localesDir);
}

module.exports = { filterLocaleKeys };
