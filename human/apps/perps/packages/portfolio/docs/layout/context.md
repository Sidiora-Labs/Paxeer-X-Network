# context.tsx (Layout context)

## context.tsx responsibility

Provides a React context for layout-level state: sidebar open state (`sideOpen`, `onSideOpenChange`) and optional `routerAdapter`. Exports `LayoutProvider` and `useLayoutContext` for consuming components.

## context.tsx exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| useLayoutContext | function | Hook | Returns LayoutContextValue from context |
| LayoutProvider | component | Provider | Provides sideOpen, onSideOpenChange, routerAdapter to children |

## LayoutContextValue (type)

| Property | Type | Description |
|----------|------|-------------|
| sideOpen | boolean | Whether sidebar is open |
| onSideOpenChange | (open: boolean) => void | Setter for sideOpen |
| routerAdapter | RouterAdapter \| undefined | Optional router adapter from @paxeer-network/types |

## LayoutProvider Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| routerAdapter | RouterAdapter | No | Passed through context |
| children | ReactNode | Yes | Tree under provider |

## context.tsx dependency and usage

- **Upstream**: React (createContext, useContext, useState, useMemo); RouterAdapter from @paxeer-network/types.
- **Downstream**: Any layout child that needs sidebar toggle or routerAdapter without prop drilling.

## context.tsx Example

```tsx
<LayoutProvider routerAdapter={adapter}>
  <App />
</LayoutProvider>

// In child:
const { sideOpen, onSideOpenChange, routerAdapter } = useLayoutContext();
```
