# @layerx/ui

A responsive React component library for **Next.js**, used by the human web application in `human/apps/web`. One component API renders the right
interaction per platform — bottom sheets on mobile, centered modals on desktop;
month-banded money lists on mobile, sortable tables on desktop; a bottom tab bar
on mobile, a sidebar on desktop.

## Install

The package is not published to a registry. `human/apps/web` depends on it as `"@layerx/ui": "file:packages/layerx-ui"` and builds it with `npm run build:ui` before `next build`. Peer dependencies: `react` >= 18.2, `react-dom` >= 18.2, `tailwindcss` >= 4.

## Setup (Tailwind v4)

In your app CSS, import the tokens after Tailwind:

```css
@import "tailwindcss";
@import "@layerx/ui/styles.css";
```

That registers every token (`bg-primary`, `text-muted-foreground`, `rounded-sheet`,
`shadow-card`, `animate-sheet-up`, …) as Tailwind utilities, backed by CSS
variables you can override in `:root` to re-theme (e.g. swap `--accent` to a new
brand color).

## The platform contract

Every pattern component resolves its variant from:

1. an explicit `platform="mobile" | "desktop"` prop, else
2. the nearest `<PlatformProvider value="…">`, else
3. the viewport (`<768px` = mobile).

| Pattern              | Mobile                                   | Desktop                                        |
| -------------------- | ---------------------------------------- | ---------------------------------------------- |
| Navigation           | Bottom tab bar + center action (FAB)     | Left sidebar: approval badge, Explorer, settings footer |
| Primary action       | Full-width pinned pill at thumb reach    | Fixed-width button in pane footer / page header |
| Confirmation         | Bottom sheet with consequence copy       | Centered modal, max 440px, Esc + overlay, focus-trapped |
| Detail / education   | Bottom sheet or pushed screen            | Right drawer or inline expanding section       |
| Filters              | Sheet with Clear + Apply                 | Popovers on filter chips; calendar range picker |
| Money lists          | Stacked rows, month bands with subtotals | Sortable table, hover, sticky group rows, export |
| Multi-step journeys  | Full-screen wizard, one decision/screen  | Split pane: form + live "what will happen" summary |
| Search               | Pushed search screen                     | Cmd+K command bar with type-ahead              |
| Code entry           | Tap-per-box kit + on-screen keypad       | Single segmented input, full paste, auto-advance |
| Notifications        | Pushed screen with recency segments      | Bell popover + full archive page               |

## Quick start

```tsx
"use client";
import { AppShell, MoneyList, ConfirmDialog } from "@layerx/ui";

const nav = [
  { id: "home", label: "Home" },
  { id: "agents", label: "Agents" },
  { id: "activity", label: "Activity", badge: 3 }, // approval badge
  { id: "more", label: "More" },
];

export default function Page() {
  return (
    <AppShell nav={nav} activeNav="home" title="Home">
      {/* your screen */}
    </AppShell>
  );
}
```

## Package layout

- `src/lib` — `cn`, money formatting, platform provider, shared types
- `src/components` — primitives, overlays, patterns (all client components)
- `src/styles/tokens.css` — the design tokens (`@layerx/ui/styles.css`)

## Build

```bash
npm run build   # tsup → dist/ (ESM + CJS + d.ts), "use client" banner included
```

Consumers import the built `dist/` output, so rebuild after changing a component (`npm run build:ui` from `human/apps/web`).
