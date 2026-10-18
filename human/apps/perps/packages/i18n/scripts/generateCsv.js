const fs = require("fs-extra");
const path = require("path");
const { checkFileExists } = require("./utils");
const { multiJson2Csv } = require("./json-csv-converter");
const { defaultLanguages } = require("../dist");

/** Generate a locale CSV file */
async function generateCsv(inputDir, outputPath) {
  const headers = [""];
  const jsonList = [];

  for (const item of defaultLanguages) {
    headers.push(item.localCode);
    const json = await fs.readJSON(
      path.resolve(inputDir, `${item.localCode}.json`),
      {
        encoding: "utf8",
      },
    );
    jsonList.push(json);
  }

  const csv = multiJson2Csv(jsonList, headers);

  const csvPath = path.resolve(outputPath);

  await checkFileExists(outputPath);

  await fs.outputFile(outputPath, csv, { encoding: "utf8" });

  console.log("generateCsv success =>", csvPath);
}

module.exports = {
  generateCsv,
};
