# layout.script.tsx (usePortfolioLayoutScript)

## layout.script.tsx responsibility

Provides the sidebar menu configuration and current path for the portfolio layout. Exports `PortfolioLeftSidebarPath` enum (route paths) and `usePortfolioLayoutScript` hook that returns memoized sidebar items (with icons and i18n labels) and current path state synced from props or routerAdapter.

## layout.script.tsx exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| PortfolioLeftSidebarPath | enum | Route paths | Overview, Positions, Orders, Assets, FeeTier, ApiKey, Setting, History |
| UseLayoutBuilderOptions | type | Hook options | { current?: string } |
| usePortfolioLayoutScript | hook | Layout state | Returns { items, current, ... } for sidebar and path |

## PortfolioLeftSidebarPath values

| Value | Path |
|-------|------|
| Overview | /portfolio |
| Positions | /portfolio/positions |
| Orders | /portfolio/orders |
| Assets | /portfolio/assets |
| FeeTier | /portfolio/feeTier |
| ApiKey | /portfolio/apiKey |
| Setting | /portfolio/setting |
| History | /portfolio/history |

## usePortfolioLayoutScript parameters and return

- **Input**: `UseLayoutBuilderOptions`: `{ current?: string }`. If omitted, uses `routerAdapter?.currentPath ?? "/portfolio"`.
- **Output**: Object including `items` (sidebar item array with name, href, icon) and `current` (string path). Used by layout widget and UI.

## usePortfolioLayoutScript dependency and call relationship

- **Upstream**: useScaffoldContext (routerAdapter), useTranslation; props.current or routerAdapter.currentPath.
- **Downstream**: PortfolioLayoutWidget, PortfolioLayout / LeftSidebar consume items and current.

## usePortfolioLayoutScript execution flow

1. Read routerAdapter from useScaffoldContext; init current from props.current ?? routerAdapter?.currentPath ?? "/portfolio".
2. useEffect syncs current when props.current or routerAdapter.currentPath changes.
3. useMemo builds items array: each entry has name (t("common.overview") etc.), href (PortfolioLeftSidebarPath), icon (SVG). Order: Overview, Positions, Orders, Assets, FeeTier, ApiKey, Setting, History.
4. Return { items, current, ... }.

## usePortfolioLayoutScript Example

```typescript
const { items, current } = usePortfolioLayoutScript({ current: "/portfolio/assets" });
// items: { name, href, icon }[]
// current: string
```
