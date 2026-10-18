# assets.d.ts

> Location: `packages/ui-transfer/src/types/assets.d.ts`

## Overview

Ambient module declarations so TypeScript treats image imports as strings. Used for `import img from "./x.png"` etc.

## Declarations

Each of the following modules is declared with a default export of type `string`:

- `*.png`
- `*.jpg`
- `*.jpeg`
- `*.svg`
- `*.gif`
- `*.webp`

## Usage example

```ts
import logo from "./logo.png";
// logo: string
```
