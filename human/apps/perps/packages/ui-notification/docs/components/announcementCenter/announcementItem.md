# announcementItem

## Overview

Single announcement row component: icon by type, localized title, formatted update time, expandable message body, optional “Join now” action for campaign/vote, and chevron that rotates on expand/collapse. Used inside the announcement center list and notification UI.

## Exports

### `AnnouncementItem`

React component that renders one announcement with expand/collapse and optional link.

#### Props

| Prop            | Type                    | Required | Description |
|----------------|-------------------------|----------|-------------|
| `expanded`     | `boolean`               | Yes      | Whether the message body is expanded |
| `url`          | `string \| null \| undefined` | No  | Link URL; for Campaign/Vote, shows “Join now” if set |
| `onItemClick`  | `(url: string) => void`  | No       | Called when “Join now” is clicked |
| `onExpandToggle` | `() => void`          | No       | Called when row is clicked to expand/collapse |
| `type`         | `AnnouncementType \| undefined \| null` | No | Announcement type (Campaign, Listing, Maintenance, Delisting, Vote, etc.) |
| `message`      | `string`                | Yes      | Body text |
| `updatedTime`  | `number`                | Yes      | Timestamp (formatted or “Recently updated” for maintenance) |
| `className`    | `string`                | No       | Extra CSS class |
| `showDivider`  | `boolean`                | No       | If true, adds border between items; affects hover/selected background |

## UI behavior

- **Icon**: Chosen by `type` (Campaign → CampaignIcon, Listing → FundIcon, Maintenance → SecurityIcon, Delisting/default → AnnouncementIcon).
- **Title**: Localized via `t("notification.campaign")`, `t("notification.listing")`, etc.
- **Time**: Maintenance shows “Recently updated”; others use `Text.formatted` with `yyyy-MM-dd HH:mm:ss`.
- **Action**: For Campaign or Vote with non-empty `url` and `onItemClick`, shows “Join now” + ArrowRightShortIcon; click calls `onItemClick(url)` and stops propagation.
- **Expand**: Row click toggles `onExpandToggle`. Message and action are in a grid that animates with `gridTemplateRows: 1fr | 0fr`. Chevron rotates 180° when expanded.

## Usage example

```tsx
import { AnnouncementItem } from "@paxeer-network/ui-notification";

<AnnouncementItem
  expanded={current === item.announcement_id}
  url={item.url}
  onItemClick={onItemClick}
  onExpandToggle={() => setCurrent(current === item.announcement_id ? null : item.announcement_id)}
  type={item.type}
  message={item.message}
  updatedTime={item.updated_time ?? 0}
  showDivider
/>
```
