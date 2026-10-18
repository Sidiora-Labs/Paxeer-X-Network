const fs = require("fs-extra");
const path = require("path");
const { checkFileExists } = require("./utils");
const { en } = require("../dist");

/** Fill values from the input locale JSON file and generate a new locale JSON file */
async function fillJson(inputPath, outputPath) {
  const inputJson = await fs.readJSON(inputPath, { encoding: "utf8" });

  const newJson = {};
  const missingValues = {};
  Object.keys(en).forEach((key) => {
    const value = inputJson[key] || "";
    if (!value) {
      missingValues[key] = en[key];
    }
    newJson[key] = value;
  });
  console.log("missingValues", missingValues);

  const jsonPath = path.resolve(outputPath);
  await checkFileExists(jsonPath);

  await fs.outputFile(jsonPath, JSON.stringify(newJson, null, 4), {
    encoding: "utf8",
  });

  console.log("fillJson success =>", jsonPath);
}

module.exports = {
  fillJson,
};
