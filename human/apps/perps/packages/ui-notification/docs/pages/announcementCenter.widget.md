# announcementCenter.widget

## Overview

Widget that connects announcement center data (from `useAnnouncementCenterScript`) to `AnnouncementCenterUI` and forwards item URL clicks to a route-change callback.

## Exports

### `AnnouncementCenterWidget`

React component that uses `useAnnouncementCenterScript()` for `dataSource`, `current`, and `setCurrent`, and passes `onItemClick` that calls `onRouteChange(url)` when a link is clicked.

#### Props

| Prop             | Type                    | Required | Description |
|------------------|-------------------------|----------|-------------|
| `onRouteChange`  | `(url: string) => void` | Yes      | Called when user clicks a link (e.g. to open or navigate to URL) |

## Usage example

```tsx
import { AnnouncementCenterWidget } from "./announcementCenter.widget";

<AnnouncementCenterWidget
  onRouteChange={(url) => window.open(url, "_blank")}
/>
```
