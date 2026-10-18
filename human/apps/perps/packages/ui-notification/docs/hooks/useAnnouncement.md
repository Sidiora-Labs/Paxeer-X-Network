# useAnnouncement

## Overview

Hook that loads announcements from the REST API and WebSocket, merges maintenance data from `useMaintenanceStatus`, filters listing/delisting by market list, and exposes tips, maintenance dialog text, and show/close state. Optionally integrates with app context to control global “show announcement” state.

## Exports

### `AnnouncementOptions`

| Property    | Type      | Required | Description                    |
|------------|-----------|----------|--------------------------------|
| `hideTips` | `boolean` | No       | If true, does not set global show state for tips |

### `useAnnouncement(options?: AnnouncementOptions)`

Returns announcement data and callbacks for the notification/announcement UI.

#### Return type

| Property               | Type                          | Description |
|------------------------|-------------------------------|-------------|
| `maintenanceDialogInfo` | `string \| undefined`        | Localized maintenance dialog message when status is Maintenance |
| `tips`                 | `API.AnnouncementRow[]`       | Merged, deduplicated, sorted list (maintenance + API + WS updates) |
| `closeTips`            | `() => void`                  | Sets stored “show” to false so the banner can hide |
| `showAnnouncement`     | `boolean`                     | Whether to show the announcement UI (respects `hideTips`) |

#### Behavior

- Fetches `/v2/public/announcement` (refresh ~1h), subscribes to `announcement` WebSocket.
- Merges maintenance from `useMaintenanceStatus` into tips.
- Optional `dataAdapter.announcementList` can transform the list.
- Filters out listing/delisting items whose `$TICKER` symbols are not in the current market list.
- Deduplicates by `announcement_id`, sorts by `updated_time`.
- Persists “show” and “lastUpdateTime” in localStorage (`paxeer-network_announcement`).
- When `options?.hideTips` is not set, syncs `showAnnouncement` with app context (`setShowAnnouncement`).

### `AnnouncementReturn`

Type alias for the return type of `useAnnouncement`.

## Usage example

```tsx
import { useAnnouncement } from "@paxeer-network/ui-notification";

function MyNotification() {
  const { tips, closeTips, showAnnouncement, maintenanceDialogInfo } = useAnnouncement();

  if (!showAnnouncement || tips.length === 0) return null;

  return (
    <NotificationUI
      dataSource={tips}
      onClose={closeTips}
      onItemClick={(url) => window.open(url)}
    />
  );
}
```
