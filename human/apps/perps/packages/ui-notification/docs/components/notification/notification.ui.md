# notification.ui

## Overview

Main notification dropdown component. Renders a header (icon + title by announcement type), an expandable/collapsible content area with a carousel of content cards, and a footer with prev/next and “close all”. Used for the global notification banner/dropdown.

## Exports

### `NotificationProps`

| Property    | Type                    | Required | Description |
|------------|-------------------------|----------|-------------|
| `className`| `string`                | No       | Extra CSS class |
| `dataSource` | `API.AnnouncementRow[]` | Yes      | List of announcements to show |
| `onClose`  | `() => void`            | No       | Called when user clicks “close all” |

### `NotificationUI`

Component also accepts extended props:

| Property             | Type                    | Required | Description |
|----------------------|-------------------------|----------|-------------|
| `maintenanceMessage` | `string`                | No       | Optional maintenance message override |
| `onItemClick`        | `(url: string) => void` | Yes      | Called when user clicks a link (e.g. “Join now”) |

### `NotificationItemProps` (internal/item)

| Property   | Type     | Required | Description |
|-----------|----------|----------|-------------|
| `id`      | `string` | Yes      | Unique id |
| `type`    | `"success" \| "error" \| "warning" \| "info"` | No | Visual type |
| `title`   | `string` | No       | Title text |
| `message` | `string` | Yes      | Body text |
| `onClose` | `() => void` | No    | Close handler |

### `NotificationListProps` (internal)

| Property        | Type                    | Description |
|----------------|-------------------------|-------------|
| `notifications` | `NotificationItemProps[]` | List of items |
| `onRemove`     | `(id: string) => void`  | Remove item by id |

## UI behavior

- **Header**: Icon and title depend on `AnnouncementType` (Campaign, Delisting, Listing, Maintenance, Vote, default). Chevron toggles expand/collapse.
- **Content**: One card per announcement type (`CampaignContentCard`, `MaintenanceContentCard`, `DelistingContentCard`, `CommunityVoteContentCard`, `ListingContentCard`). Current index is controlled by footer prev/next.
- **Footer**: Shows `current + 1 / total`, prev/next buttons, and “Close all” that calls `onClose`.
- **Expand/collapse**: Uses CSS grid (`gridTemplateRows: 1fr | 0fr`) for animation.

## Usage example

```tsx
import { NotificationUI } from "@paxeer-network/ui-notification";

<NotificationUI
  dataSource={tips}
  onClose={closeTips}
  onItemClick={(url) => window.open(url, "_blank")}
/>
```
