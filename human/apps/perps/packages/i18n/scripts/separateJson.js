const fs = require("fs-extra");
const path = require("path");
const { checkFileExists, findJsonFiles } = require("./utils");

/**
 * Separate json file default and extend key values based on the key
 * @param {string} inputDir - The input directory for locale JSON files
 * @param {string} outputDir - The output directory for locale JSON files
 * @param {string} separateKey - The key to separate the json files
 */
async function separateJson(inputDir, outputDir, separateKey) {
  const jsonFiles = await findJsonFiles(inputDir);

  const separateKeys = separateKey.split(",");
  for (const file of jsonFiles) {
    const json = await fs.readJSON(path.resolve(inputDir, file), {
      encoding: "utf8",
    });

    const defaultJson = {};
    const extendJson = {};
    Object.keys(json).forEach((key) => {
      if (separateKeys.some((k) => key.startsWith(k))) {
        extendJson[key] = json[key];
      } else {
        defaultJson[key] = json[key];
      }
    });

    const jsonPath = path.resolve(outputDir, file);
    await checkFileExists(jsonPath);

    await fs.outputFile(jsonPath, JSON.stringify(defaultJson, null, 2), {
      encoding: "utf8",
    });

    const extendPath = path.resolve(outputDir, "extend", file);
    await checkFileExists(extendPath);

    await fs.outputFile(extendPath, JSON.stringify(extendJson, null, 2), {
      encoding: "utf8",
    });

    console.log("separateJson success =>", jsonPath, extendPath);
  }
}

module.exports = {
  separateJson,
};
