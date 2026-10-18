# PortfolioLayoutMobile (layout.ui.mobile.tsx)

## PortfolioLayoutMobile responsibility

Mobile portfolio layout: full-height column with sticky top header (MainNavMobile), main content area, and fixed bottom nav (BottomNav). Wraps children in LayoutProvider and passes script state (current, items) and routerAdapter to nav components.

## PortfolioLayoutMobile input and output

- **Input**: Props = ScaffoldProps & usePortfolioLayoutScriptType & { current?: string } (routerAdapter, items, current, mainNavProps, bottomNavProps, children).
- **Output**: UI: header with MainNavMobile, scrollable content Box, footer with BottomNav.

## PortfolioLayoutMobile Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| (ScaffoldProps) | ScaffoldProps | - | mainNavProps, bottomNavProps, routerAdapter, etc. |
| items | (from script) | No | Sidebar/sub items for MainNavMobile |
| current | string | No | Current path for highlight |
| children | ReactNode | Yes | Main content |

## PortfolioLayoutMobile dependency and rendering

- **Upstream**: LayoutProvider (context), usePortfolioLayoutScriptType (layout.script), MainNavMobile, BottomNav, ScaffoldProps from @paxeer-network/ui-scaffold; Flex, Box from @paxeer-network/ui.
- **Downstream**: Used by PortfolioLayoutWidget when isMobile.

## PortfolioLayoutMobile Example

```tsx
<PortfolioLayoutMobile
  current={current}
  items={items}
  routerAdapter={routerAdapter}
  mainNavProps={...}
  bottomNavProps={...}
>
  <PageContent />
</PortfolioLayoutMobile>
```
