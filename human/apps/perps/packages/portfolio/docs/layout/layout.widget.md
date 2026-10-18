# PortfolioLayoutWidget

## PortfolioLayoutWidget responsibility

Root layout component for the portfolio section. Uses `useScreen` to switch between desktop (`PortfolioLayout`) and mobile (`PortfolioLayoutMobile`), and wires `usePortfolioLayoutScript` state (including optional `leftSideProps?.current`) into the chosen layout.

## PortfolioLayoutWidget input and output

- **Input**: Props: `PortfolioLayoutWidgetProps` (= `PortfolioLayoutProps`), including `leftSideProps?.current`, `children`, and other scaffold props.
- **Output**: Renders either `PortfolioLayoutMobile` or `PortfolioLayout` with script state and props spread.

## PortfolioLayoutWidget Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| (inherited from PortfolioLayoutProps) | ScaffoldProps & { hideSideBar?, items? } | No | - | Same as PortfolioLayout (routerAdapter, leftSideProps, classNames, children, etc.) |
| leftSideProps?.current | string | No | - | Override current path for sidebar highlight |

## PortfolioLayoutWidget dependency and rendering

- **Upstream**: usePortfolioLayoutScript (layout.script), PortfolioLayout (layout.ui), PortfolioLayoutMobile (layout.ui.mobile), useScreen (@paxeer-network/ui).
- **Downstream**: Host app mounts this as the portfolio shell; sidebar items and routing come from script and routerAdapter.

## PortfolioLayoutWidget rendering flow

1. usePortfolioLayoutScript({ current: props.leftSideProps?.current }) → state (items, current, etc.).
2. useScreen() → isMobile.
3. If isMobile: render PortfolioLayoutMobile with state and props; else render PortfolioLayout with state and props.

## PortfolioLayoutWidget Example

```tsx
import { PortfolioLayoutWidget } from "@paxeer-network/portfolio";

<PortfolioLayoutWidget
  routerAdapter={routerAdapter}
  leftSideProps={{ current: "/portfolio/assets" }}
>
  {children}
</PortfolioLayoutWidget>
```
