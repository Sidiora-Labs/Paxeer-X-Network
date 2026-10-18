# announcementCenter.page

## Overview

Page component that renders the announcement center widget and wires link clicks to the app router via `RouterAdapter`, opening URLs (e.g. in a new tab).

## Exports

### `AnnouncementCenterPage`

React component that wraps `AnnouncementCenterWidget` and forwards `onRouteChange` to `routerAdapter.onRouteChange` with `href`, `name`, and `target: "_blank"`.

#### Props

| Prop           | Type             | Required | Description |
|----------------|------------------|----------|-------------|
| `routerAdapter`| `RouterAdapter?`  | No       | Adapter with `onRouteChange({ href, name, target })` for navigation |

## Usage example

```tsx
import { AnnouncementCenterPage } from "@paxeer-network/ui-notification";

<AnnouncementCenterPage
  routerAdapter={{
    onRouteChange: ({ href, target }) => {
      if (target === "_blank") window.open(href);
      else history.push(href);
    },
  }}
/>
```
