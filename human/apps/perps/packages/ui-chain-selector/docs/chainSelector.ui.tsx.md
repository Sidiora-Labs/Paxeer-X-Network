# chainSelector.ui.tsx

## Overview

Presentational layer for the chain selector: `ChainSelector` (main component with tabs and lists), `ChainItem`, and `RecentChainItem`. Uses `ChainType` and `TChainItem` from `./type`, and `UseChainSelectorScriptReturn` from `./chainSelector.script`. Styling is done with `tv` variants (`wide` / `compact`).

## Exports

### ChainSelectorProps

Props for the main `ChainSelector` component. Extends `UseChainSelectorScriptReturn` and adds:

| Prop            | Type | Required | Default | Description |
|-----------------|------|----------|---------|-------------|
| `isWrongNetwork`| `boolean` | No | - | When true, shows wrong-network tip at bottom. |
| `variant`       | `"wide" \| "compact"` | No | `"wide"` | `wide`: desktop layout; `compact`: mobile. |
| `className`     | `string` | No | - | Extra class for the root container. |

Plus all fields from `UseChainSelectorScriptReturn`: `recentChains`, `chains`, `selectChainId`, `onChainClick`, `selectedTab`, `onTabChange`, `showTestnet`.

### ChainSelector

Main component: Tabs (Mainnet / Testnet), recent chains row, and grids of chains. Renders `RecentChainItem` for recent and `ChainItem` for mainnet/testnet lists. Shows a warning tip when `isWrongNetwork` is true.

### ChainItem

Single chain row with icon, name, and selected indicator.

| Prop       | Type | Required | Description |
|------------|------|----------|-------------|
| `selected`| `boolean` | Yes | Whether this chain is the current selection. |
| `item`    | `TChainItem` | Yes | Chain data. |
| `onClick` | `() => void` | No | Click handler. |
| `className` | `string` | No | Item container class. |

### RecentChainItem

Compact recent chain chip (icon only).

| Prop          | Type | Required | Description |
|---------------|------|----------|-------------|
| `item`       | `TChainItem` | Yes | Chain data. |
| `onClick`    | `() => void` | No | Click handler. |
| `iconClassName` | `string` | No | Class for the chain icon. |

## UI behavior

- **wide**: Larger icons, 3-column mainnet grid, 2-column testnet grid.
- **compact**: Smaller icons, 2-column mainnet, 1-column testnet, different background and spacing.
- Testnet tab is only rendered when `showTestnet` is true.
- Selected chain shows a small gradient dot indicator.

## Usage example

```tsx
import { ChainSelector, ChainItem, RecentChainItem } from "./chainSelector.ui";
import { ChainType, TChainItem } from "./type";

<ChainSelector
  recentChains={recentChains}
  chains={{ mainnet: [...], testnet: [...] }}
  selectChainId={selectChainId}
  onChainClick={onChainClick}
  selectedTab={ChainType.Mainnet}
  onTabChange={onTabChange}
  showTestnet={true}
  variant="wide"
  isWrongNetwork={false}
/>
```
