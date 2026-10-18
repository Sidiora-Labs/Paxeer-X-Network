# announcementCenter.ui

## Overview

Announcement center panel UI: title “Notifications”, scrollable list of announcement items with optional dividers, and expand/collapse per item. Exports both the list content component (`AnnouncementContent`) and the full panel (`AnnouncementCenterUI`).

## Exports

### `AnnouncementContent`

Renders the list of announcements. Shows an empty state via `ExtensionSlot` when `dataSource` is empty; otherwise maps each item to `AnnouncementItem` with expand state and click handlers.

#### Props

| Prop          | Type                              | Required | Description |
|---------------|-----------------------------------|----------|-------------|
| `dataSource`  | `API.AnnouncementRow[]`           | Yes      | List of announcements |
| `current`     | `string \| number \| null`        | Yes      | ID of the expanded item |
| `onExpandToggle` | `(id: string \| number \| null) => void` | Yes | Toggle expanded item (pass null to collapse) |
| `onItemClick` | `(url: string) => void`           | Yes      | Called when user clicks a link |
| `showDivider` | `boolean`                         | No       | If true, adds border and padding between items |

---

### `AnnouncementCenterUI`

Full panel: title from `t("notification.title")`, then a `ScrollArea` (height 300px) containing `AnnouncementContent`. Manages `expanded` state (single expanded item ID) and passes it to `AnnouncementContent`.

#### Props

| Prop         | Type                    | Required | Description |
|--------------|-------------------------|----------|-------------|
| `dataSource` | `API.AnnouncementRow[]` | Yes      | List of announcements |
| `onItemClick`| `(url: string) => void`  | Yes      | Called when user clicks a link |

**Note**: This is the component exported by the package as `AnnouncementCenterUI` (takes only `dataSource` and `onItemClick`). The **pages** layer has a different component also named `AnnouncementCenterUI` that accepts `dataSource`, `current`, `setCurrent`, and `onItemClick` and renders a styled container around `AnnouncementContent`; see [announcementCenter.ui (pages)](../../pages/announcementCenter.ui.md).

## Usage example

```tsx
import { AnnouncementCenterUI, AnnouncementContent } from "./announcementCenter.ui";

// Full panel (self-managed expand state)
<AnnouncementCenterUI
  dataSource={tips}
  onItemClick={(url) => window.open(url)}
/>

// List only (controlled expand state)
<AnnouncementContent
  dataSource={tips}
  current={expandedId}
  onExpandToggle={setExpandedId}
  onItemClick={onItemClick}
  showDivider
/>
```
