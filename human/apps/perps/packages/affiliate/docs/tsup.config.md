# tsup.config.ts

## Overview

Build configuration for the affiliate package using tsup. Entry is `src/index.ts`, output formats are ESM and CJS, with declaration files and source maps.

## Exports

- **default** – A function returning tsup config (entry, format, target, dts, external react/react-dom, esbuild options).

## Usage Example

```ts
// Consumed by build script; no direct import in app code.
// Entry: src/index.ts
// Output: ESM + CJS, target es2020, dts: true
```
