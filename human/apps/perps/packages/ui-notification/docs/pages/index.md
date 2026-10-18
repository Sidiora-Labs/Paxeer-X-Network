# pages

## Overview

Announcement center page, widget, and script hook. Wires data and routing.

## Files

| File | Language | Description |
|------|----------|-------------|
| [announcementCenter.page.md](announcementCenter.page.md) | TSX | Page component that uses `AnnouncementCenterWidget` with `RouterAdapter` for link navigation |
| [announcementCenter.widget.md](announcementCenter.widget.md) | TSX | Widget that connects script state to `AnnouncementCenterUI` and `onRouteChange` |
| [announcementCenter.ui.md](announcementCenter.ui.md) | TSX | Presentational UI for the announcement list inside a styled container |
| [announcementCenter.script.md](announcementCenter.script.md) | TSX | Hook that provides `dataSource` (tips) and `current`/`setCurrent` for the center |
