import { defineConfig } from "tsup";

export default defineConfig((options) => ({
  entry: ["src/index.ts"],
  format: ["esm", "cjs"],
  target: "es2020",
  splitting: false,
  sourcemap: true,
  treeshake: true,
  clean: !options.watch,
  dts: true,
  external: ["react", "react-dom"],
  tsconfig: "tsconfig.build.json",
}));
