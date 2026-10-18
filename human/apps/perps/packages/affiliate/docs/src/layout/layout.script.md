# layout.script.tsx

## Overview

Hook `useLayoutBuilder` returns `SideBarProps`: sidebar items (trading, affiliate links with icons) and current path state. Uses `useTranslation` for labels.

## Exports

| Export | Description |
|--------|-------------|
| `useLayoutBuilder` | Returns `SideBarProps` (items, current route state) for the left sidebar |

## Usage Example

```tsx
import { useLayoutBuilder } from "./layout.script";
const sideBarProps = useLayoutBuilder();
// Pass to AffiliateLayout or SideBar
```
