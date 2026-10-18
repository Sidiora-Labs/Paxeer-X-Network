# theme (tailwindcss)

## Overview

Tailwind plugin that adds component styles for Recharts axis ticks: first tick text uses text-anchor start, last tick text uses text-anchor end. Used with class `.xAxis` on a parent.

## Exports

### chartPlugin

A `PluginCreator` that calls `plugin()` with `addComponents`:

- `.xAxis .recharts-cartesian-axis-tick:first-child text` → `text-anchor: start`
- `.xAxis .recharts-cartesian-axis-tick:last-child text` → `text-anchor: end`

Options: `respectPrefix: false`.

## Usage example

```js
// tailwind.config.js
import { chartPlugin } from "@paxeer-network/chart";
export default {
  plugins: [chartPlugin()],
};
```

```tsx
<div className="xAxis">
  <RechartsCartesianAxis ... />
</div>
```
