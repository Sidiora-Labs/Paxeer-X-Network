# announcementCenter.ui

## Overview

Presentational UI for the announcement center: a styled container with rounded background that renders the shared `AnnouncementContent` list with expand/collapse and item click handling.

## Exports

### `AnnouncementCenterUI`

React component that displays the announcement list inside a card-style container (`oui-m-1 oui-rounded-xl oui-bg-base-9 oui-p-2`). Delegates to `AnnouncementContent` with `showDivider`.

#### Props

| Prop         | Type                              | Required | Description |
|--------------|-----------------------------------|----------|-------------|
| `dataSource` | `API.AnnouncementRow[]`           | Yes      | List of announcements |
| `current`    | `string \| number \| null`        | Yes      | ID of the expanded item |
| `setCurrent` | `(current: string \| number \| null) => void` | Yes | Set expanded item ID |
| `onItemClick`| `(url: string) => void`           | Yes      | Called when user clicks a link |

## Usage example

```tsx
import { AnnouncementCenterUI } from "./announcementCenter.ui";

const [current, setCurrent] = useState<string | number | null>(null);

<AnnouncementCenterUI
  dataSource={tips}
  current={current}
  setCurrent={setCurrent}
  onItemClick={(url) => window.open(url)}
/>
```
