const fs = require("fs-extra");
const path = require("path");
const { en } = require("../dist");

async function generateEnJson() {
  const outPath = path.resolve(__dirname, "../locales/en.json");
  const jsonData = JSON.stringify(en, null, 2);
  await fs.outputFile(outPath, jsonData, { encoding: "utf8" });
}

generateEnJson();
