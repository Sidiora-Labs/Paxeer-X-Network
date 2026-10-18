# title.ui

## Overview

Renders the Trading Rewards title block: title text, divider, subtitle, and an optional “Learn more” link that opens doc URL. Uses i18n for default title and subtitle.

## Exports

### Title (component)

Accepts `TitleConfig`: title, subtitle, content, docOpenOptions, brokerName. Defaults use translation keys and doc link.

### MultiLineText (component, default export)

Internal: “Learn more” clickable text that opens `docOpenOptions.url` with optional target/features.

## Usage example

```tsx
<Title title="Trading Rewards" subtitle="..." docOpenOptions={{ url: "...", target: "_blank" }} />
```
