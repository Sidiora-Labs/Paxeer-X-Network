import { defineConfig } from "tsup";

export default defineConfig({
  entry: {
    index: "src/index.ts",
    "mcp-cli": "src/mcp-cli.ts",
  },
  format: ["esm"],
  sourcemap: true,
  clean: true,
  dts: true,
  external: ["@tobilu/qmd", "better-sqlite3"],
});
