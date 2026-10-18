# layout – directory index

## Directory responsibility

Portfolio layout: root widget (desktop/mobile), sidebar navigation, layout script (sidebar items and current path), and layout context (sidebar open state, router). Consumed by the host app as the shell for portfolio routes.

## Key entities

| Entity | File | Responsibility | Dependency |
|--------|------|----------------|------------|
| PortfolioLayoutWidget | layout.widget.tsx | Chooses desktop or mobile layout; composes script + UI | usePortfolioLayoutScript, useScreen |
| PortfolioLayout | layout.ui.tsx | Desktop Scaffold + LeftSidebar | Scaffold, SideBar, useScaffoldContext |
| usePortfolioLayoutScript | layout.script.tsx | Sidebar items, current path, path enum | useScaffoldContext, useTranslation |
| PortfolioLeftSidebarPath | layout.script.tsx | Route paths for sidebar | - |
| LayoutProvider / useLayoutContext | context.tsx | Sidebar open state and routerAdapter | React context |

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|-----------------|------|
| index.ts | TypeScript | Re-exports widget, layout, script, path enum | PortfolioLayoutWidget, PortfolioLayoutProps, PortfolioLayout, usePortfolioLayoutScript, PortfolioLeftSidebarPath | [index.md](index.md) |
| layout.widget.tsx | TSX | Root layout widget (desktop/mobile) | PortfolioLayoutWidget, PortfolioLayoutWidgetProps | [layout.widget.md](layout.widget.md) |
| layout.ui.tsx | TSX | Desktop layout with sidebar | PortfolioLayout, PortfolioLayoutProps | [layout.ui.md](layout.ui.md) |
| layout.ui.mobile.tsx | TSX | Mobile layout | PortfolioLayoutMobile | [layout.ui.mobile.md](layout.ui.mobile.md) |
| layout.script.tsx | TSX | Sidebar items and current path | usePortfolioLayoutScript, PortfolioLeftSidebarPath, UseLayoutBuilderOptions | [layout.script.md](layout.script.md) |
| context.tsx | TSX | Layout context (side open, router) | LayoutProvider, useLayoutContext | [context.md](context.md) |

## Subdirectories

None.
