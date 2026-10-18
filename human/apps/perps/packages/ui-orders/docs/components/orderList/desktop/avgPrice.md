# avgPrice

## Overview

Memoized cell component that displays average executed price for an order. Uses `useSymbolsInfo()` for `quote_dp` and renders `Text.numeral` with the value; shows `"--"` when value is empty.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `symbol` | `string` | Yes | Symbol for format config. |
| `value` | `string` | Yes | Avg price string to display. |
