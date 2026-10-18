# PortfolioLayout (layout.ui.tsx)

## PortfolioLayout responsibility

Desktop portfolio layout: renders a Scaffold with an optional left sidebar (SideBar) showing portfolio title and nav items. Connects sidebar item selection to `routerAdapter.onRouteChange`. Styling uses `cn` and fixed classNames for content, topNavbar, and leftSidebar.

## PortfolioLayout input and output

- **Input**: Props (PortfolioLayoutProps): ScaffoldProps plus `hideSideBar?`, `items?` (sidebar items).
- **Output**: UI: Scaffold with leftSidebar (or null), content area (children), and optional topNavbar from ScaffoldProps.

## PortfolioLayout Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| (ScaffoldProps) | ScaffoldProps | - | - | leftSidebar, routerAdapter, classNames, children, etc. |
| hideSideBar | boolean | No | - | If true, leftSidebar is null |
| items | SideBarProps["items"] | No | - | Sidebar menu items (from script typically) |
| leftSideProps | object | No | - | Passed to LeftSidebar (current, etc.) |
| routerAdapter | RouterAdapter | No | - | Used for onRouteChange on item select |
| classNames | object | No | - | Merged with default content/topNavbar/leftSidebar classes |

## PortfolioLayout dependency and rendering

- **Upstream**: Scaffold, SideBar, useScaffoldContext from @paxeer-network/ui-scaffold; useTranslation, cn from @paxeer-network/ui; RouterAdapter from @paxeer-network/types.
- **Downstream**: Used by PortfolioLayoutWidget when !isMobile.

## PortfolioLayout rendering flow

1. LeftSidebar receives current path, routerAdapter, items, and scaffold expand state.
2. On sidebar item select: onItemSelect then routerAdapter?.onRouteChange({ href, name }).
3. Scaffold classNames merge defaults (e.g. content: my-6 px-3, leftSidebar: rounded bg, border).

## PortfolioLayout Example

```tsx
<PortfolioLayout
  routerAdapter={adapter}
  items={sidebarItems}
  leftSideProps={{ current: "/portfolio" }}
  hideSideBar={false}
>
  <PageContent />
</PortfolioLayout>
```
