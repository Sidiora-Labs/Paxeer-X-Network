import { defineConfig } from "tsup";

export default defineConfig((options) => ({
  entry: ["src/index.ts"],
  format: ["cjs", "esm"],
  target: "es6",
  splitting: false,
  sourcemap: true,
  clean: !options.watch,
  dts: true,
  tsconfig: "tsconfig.json",
}));
