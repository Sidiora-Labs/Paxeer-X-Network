const fs = require("fs-extra");
const path = require("path");
const { checkFileExists, findJsonFiles } = require("./utils");
const { LocaleEnum } = require("../dist");

/**
 * Merge default and extend JSON files back into one file
 * @param {string} inputDir - The input directory containing both default and extend JSON files
 * @param {string} outputDir - The output directory for merged JSON files
 */
async function mergeJson(inputDir, outputDir) {
  const jsonFiles = await findJsonFiles(inputDir);

  // Sort input files by locale
  jsonFiles.sort((a, b) => (b.startsWith(LocaleEnum.en) ? 1 : -1));
  let baseJson = {};

  const extendDir = path.resolve(inputDir, "extend");

  for (const [index, file] of jsonFiles.entries()) {
    const defaultJsonPath = path.resolve(inputDir, file);
    const extendJsonPath = path.resolve(extendDir, file);

    // Read default JSON file
    const defaultJson = await fs.readJSON(defaultJsonPath, {
      encoding: "utf8",
    });

    // Read extend JSON file if it exists
    let extendJson = {};
    if (await fs.pathExists(extendJsonPath)) {
      extendJson = await fs.readJSON(extendJsonPath, {
        encoding: "utf8",
      });
    }

    // Merge the JSON objects
    const mergedJson = { ...defaultJson, ...extendJson };

    let sortedJson = {};

    // base json
    if (index === 0) {
      baseJson = mergedJson;
      sortedJson = mergedJson;
    } else {
      for (const key of Object.keys(baseJson)) {
        sortedJson[key] = mergedJson[key];
      }
    }

    const outputPath = path.resolve(outputDir, file);

    await fs.outputFile(outputPath, JSON.stringify(sortedJson, null, 2), {
      encoding: "utf8",
    });

    console.log("mergeJson success =>", outputPath);
  }

  await fs.remove(extendDir);
  console.log("remove extendDir =>", extendDir);
}

module.exports = {
  mergeJson,
};
