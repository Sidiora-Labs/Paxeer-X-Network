# layout

## Directory Responsibility

Affiliate module layout: sidebar navigation (trading, affiliate), current route state, and main content area. Exposes AffiliateLayoutWidget which composes useLayoutBuilder and AffiliateLayout (Scaffold + SideBar from ui-scaffold). Optional LayoutProvider/context for sidebar state.

## Files

| File | Language | Summary | Entry symbols | Link |
|------|----------|---------|---------------|------|
| layout.widget.tsx | TSX | Widget that wires layout script to UI | AffiliateLayoutWidget | [layout.widget.md](layout.widget.md) |
| layout.ui.tsx | TSX | AffiliateLayout: Scaffold + LeftSidebar | AffiliateLayout, AffiliateLayoutProps | [layout.ui.md](layout.ui.md) |
| layout.script.tsx | TSX | useLayoutBuilder: sidebar items, current, onItemSelect | useLayoutBuilder | [layout.script.md](layout.script.md) |
| context.tsx | TSX | LayoutProvider (optional) | LayoutProvider | [context.md](context.md) |
| layoutBuilder.tsx | TSX | Not inferable from index | — | [layoutBuilder.md](layoutBuilder.md) |
| index.ts | TypeScript | Re-exports AffiliateLayoutWidget | AffiliateLayoutWidget | (this index) |

## Key Entities

| Entity | File | Responsibility |
|--------|------|----------------|
| AffiliateLayoutWidget | layout.widget.tsx | Public layout component |
| useLayoutBuilder | layout.script.tsx | Sidebar items and selection state |
