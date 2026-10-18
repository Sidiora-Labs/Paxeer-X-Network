const fs = require("fs-extra");
const path = require("path");

async function copyLocales() {
  await fs.copy(
    path.resolve(__dirname, "../locales"),
    path.resolve(__dirname, "../dist/locales"),
  );
}

copyLocales();
