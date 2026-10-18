# parseMarkdownLinks

## Overview

Parses markdown-style links `[text](url)` in a string and returns an array of React nodes: plain text and `<a>` elements (target="_blank", rel="noopener noreferrer") for use in descriptions.

## Exports

### parseMarkdownLinks(text: string): ReactNode[]

- **Parameters**: `text` — String that may contain `[label](url)`.
- **Returns**: Array of strings and `<a>` elements. Returns `[""]` if `text` is falsy.

## Usage example

```tsx
import { parseMarkdownLinks } from "../../utils/parseMarkdownLinks";
const parts = parseMarkdownLinks("See [docs](https://example.com) for more.");
// Render parts in a fragment or inline.
```
