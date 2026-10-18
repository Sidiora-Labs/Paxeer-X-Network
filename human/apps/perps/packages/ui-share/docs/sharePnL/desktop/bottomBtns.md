# bottomBtns

## Overview

Two actions at the bottom of desktop Share PnL: Download (secondary style) and Copy (primary style), with icons and i18n labels.

## Component

### BottomButtons

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| onClickDownload | () => void | Yes | Called when Download is clicked. |
| onClickCopy | () => void | Yes | Called when Copy is clicked. |

Uses `Flex`, `Button` from `@paxeer-network/ui` and `t("common.download")`, `t("common.copy")`.

## Usage example

```tsx
<BottomButtons onClickCopy={onCopy} onClickDownload={onDownload} />
```
