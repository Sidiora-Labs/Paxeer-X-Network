const fs = require("fs-extra");
const path = require("path");
const { multiJson2Csv } = require("./json-csv-converter");
const { checkFileExists, findJsonFiles } = require("./utils");
const { LocaleEnum } = require("../dist");

/** Convert multiple locale JSON files to a locale CSV file */
async function json2csv(inputDir, outputPath) {
  const inputFiles = await findJsonFiles(inputDir);

  // Sort input files by locale
  inputFiles.sort((a, b) => (b.startsWith(LocaleEnum.en) ? 1 : -1));

  const jsonList = [];
  const headers = [""];
  for (const filePath of inputFiles) {
    const json = await fs.readJSON(path.resolve(inputDir, filePath), {
      encoding: "utf8",
    });
    jsonList.push(json);
    const fileName = path.basename(filePath, path.extname(filePath));
    headers.push(fileName);
  }

  const csv = multiJson2Csv(jsonList, headers);

  const csvPath = path.resolve(outputPath);

  await checkFileExists(csvPath);

  await fs.outputFile(csvPath, csv, { encoding: "utf8" });

  console.log("json2csv success =>", csvPath);
}

module.exports = {
  json2csv,
};
