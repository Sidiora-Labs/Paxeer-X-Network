# layout

## Overview

Affiliate layout: scaffold with left sidebar, layout builder hook that returns sidebar props (items, current route), and widget that composes `AffiliateLayout` with builder state. Public API: `AffiliateLayoutWidget` from `./layout.widget`; `index.ts` re-exports it.

## Files

| File | Language | Description |
|------|----------|-------------|
| [layout.ui.tsx](layout.ui.md) | React | AffiliateLayout component and AffiliateLayoutProps |
| [layout.script.tsx](layout.script.md) | React | useLayoutBuilder hook (SideBarProps) |
| [layoutBuilder.tsx](layoutBuilder.md) | React | layoutBuilder (state for components/sideOpen) |
| [layout.widget.tsx](layout.widget.md) | React | AffiliateLayoutWidget |
| [context.tsx](context.md) | React | LayoutProvider, sideOpen state |
| [index.ts](layout.widget.md) | Re-export | AffiliateLayoutWidget |
