const fs = require("fs-extra");
const path = require("path");
const { csv2multiJson } = require("./json-csv-converter");
const { checkFileExists } = require("./utils");

/** Convert locale CSV to multiple locale JSON files */
async function csv2json(inputPath, outputDir) {
  const csv = fs.readFileSync(inputPath, { encoding: "utf8" });

  const json = csv2multiJson(csv);

  const files = [];

  for (const key of Object.keys(json)) {
    const filePath = path.resolve(outputDir, `${key}.json`);
    await checkFileExists(filePath);
    await fs.outputFile(filePath, JSON.stringify(json[key], undefined, 4), {
      encoding: "utf8",
    });
    files.push(filePath);
  }

  console.log("csv2json success =>", files);
}

module.exports = {
  csv2json,
};
