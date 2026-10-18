const fs = require("fs-extra");
const path = require("path");
const { csv2multiJson } = require("./json-csv-converter");
const packageJson = require("../package.json");

/**
 * https://www.jsdelivr.com/package/npm/@paxeer-network/i18n?tab=files&path=dist
 * Compare two locale CSV files
 */
async function diffCsv(oldFile, newFile) {
  const oldCsv = await fs.readFile(oldFile, { encoding: "utf8" });
  const newCsv = await fs.readFile(newFile, { encoding: "utf8" });

  const oldJson = csv2multiJson(oldCsv);
  const newJson = csv2multiJson(newCsv);
  const diffResult = compareJsonFiles(oldJson, newJson);
  console.log("CSV diff result:", JSON.stringify(diffResult, null, 2));

  const filepath = path.resolve("LOCALE_CHANGELOG.md");

  // generate .md file
  let markdownContent = generateMarkdown(diffResult);

  if (!(await fs.exists(filepath))) {
    const title = `# Locale Changelog`;
    markdownContent = `${title}\n\n${markdownContent}`;

    await fs.writeFile(filepath, markdownContent, {
      encoding: "utf8",
    });
    console.log("LOCALE_CHANGELOG.md created");
  } else {
    // Read existing content
    const existingContent = await fs.readFile(filepath, { encoding: "utf8" });

    // Find the position after "# Locale Changelog"
    const titleIndex = existingContent.indexOf("# Locale Changelog");
    if (titleIndex === -1) {
      console.error("Could not find '# Locale Changelog' title in the file");
      return;
    }

    const titleEndIndex = titleIndex + "# Locale Changelog".length;

    // Split content and insert new content after the title
    const beforeTitle = existingContent.slice(0, titleEndIndex);
    const afterTitle = existingContent.slice(titleEndIndex);

    // Combine all parts
    const newContent = `${beforeTitle}\n\n${markdownContent}${afterTitle}`;

    // Write back to file
    await fs.writeFile(filepath, newContent, {
      encoding: "utf8",
    });
    console.log("LOCALE_CHANGELOG.md updated");
  }
}

// Compare function
function compareJsonFiles(oldJson, newJson) {
  const result = {
    added: {},
    removed: {},
    updated: {},
  };

  Object.keys(newJson).forEach((lang) => {
    result.added[lang] = {};
    result.removed[lang] = {};
    result.updated[lang] = {};

    const oldKeys = oldJson[lang] || {};
    const newKeys = newJson[lang];

    // Find added keys
    Object.keys(newKeys).forEach((key) => {
      if (!(key in oldKeys)) {
        result.added[lang][key] = newKeys[key];
      }
    });

    // Find removed keys
    Object.keys(oldKeys).forEach((key) => {
      if (!(key in newKeys)) {
        result.removed[lang][key] = oldKeys[key];
      }
    });

    // Find updated keys (same key, different value)
    Object.keys(newKeys).forEach((key) => {
      if (key in oldKeys && oldKeys[key] !== newKeys[key]) {
        result.updated[lang][key] = {
          old: oldKeys[key],
          new: newKeys[key],
        };
      }
    });
  });

  return result;
}

// generate Markdown conent
function generateMarkdown(diff) {
  let mdContent = `## ${packageJson.version}\n\n`;

  const addedKeysEmpty = Object.keys(diff.added).every((lang) => {
    return Object.keys(diff.added[lang]).length === 0;
  });
  const removedKeysEmpty = Object.keys(diff.removed).every((lang) => {
    return Object.keys(diff.removed[lang]).length === 0;
  });
  const updatedKeysEmpty = Object.keys(diff.updated).every((lang) => {
    return Object.keys(diff.updated[lang]).length === 0;
  });

  if (addedKeysEmpty && removedKeysEmpty && updatedKeysEmpty) {
    return `${mdContent}### No locale changes`;
  }

  // handle added content
  if (!addedKeysEmpty) {
    mdContent += `### Added Keys\n`;
    Object.keys(diff.added).forEach((lang) => {
      if (Object.keys(diff.added[lang]).length === 0) {
        mdContent += `\n#### Language: **${lang}**\n> No added keys.\n`;
      } else {
        mdContent += `\n#### Language: **${lang}**\n`;
        mdContent += `| Key | Value |\n| --- | --- |\n`;
        Object.entries(diff.added[lang]).forEach(([key, value]) => {
          mdContent += `| ${key} | ${value} |\n`;
        });
      }
    });
  }

  // handle removed content
  if (!removedKeysEmpty) {
    mdContent += `\n### Removed Keys\n`;
    Object.keys(diff.removed).forEach((lang) => {
      if (Object.keys(diff.removed[lang]).length === 0) {
        mdContent += `\n#### Language: **${lang}**\n> No removed keys.\n`;
      } else {
        mdContent += `\n#### Language: **${lang}**\n`;
        mdContent += `| Key | Value |\n| --- | --- |\n`;
        Object.entries(diff.removed[lang]).forEach(([key, value]) => {
          mdContent += `| ${key} | ${value} |\n`;
        });
      }
    });
  }

  // handle updated content
  if (!updatedKeysEmpty) {
    mdContent += `\n### Updated Keys\n`;
    Object.keys(diff.updated).forEach((lang) => {
      if (Object.keys(diff.updated[lang]).length === 0) {
        mdContent += `\n#### Language: **${lang}**\n> No updates found.\n`;
      } else {
        mdContent += `\n#### Language: **${lang}**\n`;
        mdContent += `| Key | Old Value | New Value |\n| --- | --- | --- |\n`;
        Object.entries(diff.updated[lang]).forEach(([key, values]) => {
          mdContent += `| ${key} | ${values.old} | ${values.new} |\n`;
        });
      }
    });
  }

  return mdContent;
}

module.exports = {
  diffCsv,
};
