# contentCard

## Overview

Content card components for each announcement type used inside the notification carousel and elsewhere. Each card renders message, optional time, optional cover image, and optional “Join now” link.

## Exports

### `CampaignContentCard`

Card for campaign announcements: message, cover image, update time, and optional “Join now” button that calls `onItemClick(url)`.

#### Props

| Prop         | Type                    | Required | Description |
|-------------|-------------------------|----------|-------------|
| `message`   | `string`                | Yes      | Campaign text |
| `coverImage`| `string`                | Yes      | Image URL for background |
| `updateTime`| `number`                | Yes      | Timestamp for display |
| `url`       | `string \| null \| undefined` | No  | Link URL; if set and `onItemClick` provided, shows “Join now” |
| `onItemClick` | `(url: string) => void` | No       | Called when “Join now” is clicked |

---

### `MaintenanceContentCard`

Card for maintenance: “Recently updated” label and message. Accepts start/end time (currently not rendered in the visible UI).

#### Props

| Prop        | Type     | Required | Description |
|------------|----------|----------|-------------|
| `message`  | `string` | Yes      | Maintenance description |
| `startTime`| `number` | Yes      | Start timestamp |
| `endTime`  | `number` | Yes      | End timestamp |

---

### `DelistingContentCard`

Card for delisting: formatted date/time and message.

#### Props

| Prop        | Type     | Required | Description |
|------------|----------|----------|-------------|
| `message`  | `string` | Yes      | Delisting text |
| `updateTime`| `number` | Yes      | Timestamp (formatted as `yyyy-MM-dd HH:mm:ss`) |

---

### `ListingContentCard`

Card for listing: formatted date/time and message.

#### Props

| Prop        | Type     | Required | Description |
|------------|----------|----------|-------------|
| `message`  | `string` | Yes      | Listing text |
| `updateTime`| `number` | Yes      | Timestamp (formatted as `yyyy-MM-dd HH:mm:ss`) |

---

### `CommunityVoteContentCard`

Card for community vote: message and optional “Join now” button that calls `onItemClick(url)`.

#### Props

| Prop         | Type                    | Required | Description |
|-------------|-------------------------|----------|-------------|
| `message`   | `string`                | Yes      | Vote description |
| `url`       | `string \| null \| undefined` | No  | Link URL for “Join now” |
| `onItemClick` | `(url: string) => void` | No       | Called when “Join now” is clicked |

## Usage example

```tsx
import {
  CampaignContentCard,
  MaintenanceContentCard,
  DelistingContentCard,
  ListingContentCard,
  CommunityVoteContentCard,
} from "./contentCard";

<CampaignContentCard
  message="Join the campaign"
  coverImage="https://..."
  updateTime={Date.now()}
  url="https://..."
  onItemClick={(url) => window.open(url)}
/>
```
