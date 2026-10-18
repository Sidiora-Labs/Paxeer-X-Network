import { defineConfig } from "tsup";

export default defineConfig((options) => ({
  entry: ["src/index.ts"],
  splitting: false,
  // minify: !options.watch,
  format: ["cjs", "esm"],
  target: "es6",
  sourcemap: true,
  clean: !options.watch,
  dts: true,
  tsconfig: "tsconfig.build.json",
}));
