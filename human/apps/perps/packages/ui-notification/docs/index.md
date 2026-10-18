# ui-notification (src)

## Overview

This package provides notification and announcement UI components and hooks for the paxeer-network application. It includes announcement center UI, notification dropdown, content cards by announcement type, and data fetching/filtering utilities.

## Top-level files

| File | Language | Description |
|------|----------|-------------|
| `index.ts` | TypeScript | Main entry point; re-exports UI components, page, and hook (see Main exports below) |
| [version.ts](version.md) | TypeScript | Package version registration on `window.__PAXEER_NETWORK_VERSION__` |
| [utils.ts](utils.md) | TypeScript | Time formatting, sorting, deduplication, and listing/delisting filter helpers |

## Subdirectories

| Directory | Description |
|-----------|-------------|
| [hooks/](hooks/index.md) | React hooks for announcement data and visibility |
| [pages/](pages/index.md) | Announcement center page and widget |
| [components/](components/index.md) | Notification and announcement center components |

## Main exports (from index.ts)

- `AnnouncementCenterUI` – Announcement center panel UI
- `AnnouncementItem` – Single announcement row with expand/collapse
- `NotificationUI` – Notification dropdown with carousel
- `AnnouncementCenterPage` – Page that wires announcement center with router
- `useAnnouncement` – Hook for tips, maintenance dialog, and close/show state
