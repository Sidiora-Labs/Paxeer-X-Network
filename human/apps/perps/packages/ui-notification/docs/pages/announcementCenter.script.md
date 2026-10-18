# announcementCenter.script

## Overview

Hook that provides the announcement center data source and expand/collapse state by combining `useAnnouncement().tips` with local `current`/`setCurrent` state.

## Exports

### `useAnnouncementCenterScript()`

Returns the data and state needed to drive `AnnouncementCenterUI` or the widget.

#### Return type

| Property     | Type                              | Description |
|-------------|------------------------------------|-------------|
| `dataSource`| `API.AnnouncementRow[]`            | Announcement list from `useAnnouncement().tips` |
| `current`   | `string \| number \| null`          | ID of the currently expanded item |
| `setCurrent`| `(current: string \| number \| null) => void` | Set expanded item (or null to collapse) |

## Usage example

```tsx
import { useAnnouncementCenterScript } from "./announcementCenter.script";
import { AnnouncementCenterUI } from "./announcementCenter.ui";

function Center() {
  const { dataSource, current, setCurrent } = useAnnouncementCenterScript();
  return (
    <AnnouncementCenterUI
      dataSource={dataSource}
      current={current}
      setCurrent={setCurrent}
      onItemClick={(url) => window.open(url)}
    />
  );
}
```
