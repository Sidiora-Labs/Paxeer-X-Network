const fs = require("fs-extra");
const path = require("path");
const { getMissingKeys } = require("./json-csv-converter");
const { checkFileExists, findJsonFiles } = require("./utils");
const { LocaleEnum } = require("../dist");

async function generateMissingKeys() {
  const inputDir = path.resolve(__dirname, "../locales");
  const jsonFiles = await findJsonFiles(inputDir);

  // Sort input files by locale
  jsonFiles.sort((a, b) => (b.startsWith(LocaleEnum.en) ? 1 : -1));

  const jsonList = [];
  const headers = [""];

  for (const file of jsonFiles) {
    const jsonPath = path.resolve(inputDir, file);

    const json = await fs.readJSON(jsonPath, {
      encoding: "utf8",
    });

    jsonList.push(json);

    const fileName = path.basename(file, path.extname(file));
    headers.push(fileName);
  }

  const errors = getMissingKeys(jsonList, headers);

  const missingJson = {};
  for (const locale of Object.values(errors)) {
    for (const [key, value] of Object.entries(locale)) {
      missingJson[key] = value;
    }
  }

  const outputPath = path.resolve(inputDir, "extend/en.json");

  // don't check file exists
  // await checkFileExists(outputPath);

  await fs.outputFile(outputPath, JSON.stringify(missingJson, null, 2), {
    encoding: "utf8",
  });
}

generateMissingKeys();
