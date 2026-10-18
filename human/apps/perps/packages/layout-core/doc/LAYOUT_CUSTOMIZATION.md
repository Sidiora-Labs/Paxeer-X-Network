---
domain: layout-core
docType: guide
package: "@paxeer-network/layout-core"
intentTags:
  [
    layout,
    strategy,
    panel-registry,
    layout-host,
    customization,
    plugin-integration,
  ]
lang: en
---

# Layout Customization Guide

This document describes how to customize and extend the paxeer-network layout system using `@paxeer-network/layout-core`. It covers the architecture, implementing custom layout strategies, integrating with the trading page, and plugin patterns.

---

## 1. Overview

The layout system is built around a **strategy pattern**:

- **layout-core**: Defines the protocol (`LayoutStrategy`, `LayoutHost`, `PanelRegistry`) and utilities. It does not depend on any specific layout library.
- **layout-split**: Implements a split-pane strategy with resizable panels (uses `react-resizable-panels`).
- **layout-grid**: Implements a grid strategy with drag-and-drop (uses `react-grid-layout`).
- **trading**: Provides `DesktopLayout`, which builds the panel registry and renders `LayoutHost` with the strategy injected by host or plugin.

### Data Flow

```
Trading Page (trading.ui.desktop.tsx)
    ↓
DesktopLayout receives layoutStrategy, getInitialLayout, storageKey (from host or layout plugin)
    ↓
createTradingPanelRegistry() → PanelRegistry (Map<panelId, {node, props}>)
    ↓
LayoutHost(strategy, panels, initialLayout, storageKey)
    ↓
strategy.Renderer(layout, panels, onLayoutChange, onLayoutPersist)
```

---

## 2. Core Concepts

### 2.1 LayoutStrategy

Every layout strategy implements the `LayoutStrategy<TLayout>` interface:

```typescript
interface LayoutStrategy<TLayout extends LayoutModel = LayoutModel> {
  /** Unique identifier (e.g. "split", "grid") */
  id: string;
  /** Human-readable display name */
  displayName: string;
  /** Create default layout model for given panel IDs */
  defaultLayout: (panelIds: string[]) => TLayout;
  /** Serialize layout model to JSON for persistence */
  serialize: (layout: TLayout) => string;
  /** Deserialize JSON string back to layout model */
  deserialize: (json: string) => TLayout;
  /** Renderer component */
  Renderer: ComponentType<LayoutRendererProps<TLayout>>;
}
```

### 2.2 LayoutModel

Base type for strategy-specific layout data:

```typescript
type LayoutModel = Record<string, unknown>;
```

Each strategy extends this with its own shape. Examples:

- **Split**: `{ layouts: { lg, md, sm, xs }, breakpoints }` — tree of split/panel/sort nodes per breakpoint.
- **Grid**: `{ layouts: { lg?, md?, sm?, xs?, xxs? }, rowHeight, margin, ... }` — grid items per breakpoint.

### 2.3 PanelRegistry

Maps panel IDs to React content:

```typescript
type PanelRegistryEntry = {
  node: ReactNode;
  props?: Record<string, unknown>;
};
type PanelRegistry = Map<string, PanelRegistryEntry>;
```

`LayoutHost` accepts either `Map<string, PanelRegistryEntry>` or `Record<string, PanelRegistryEntry | ReactNode>` (plain objects are converted to `Map` internally).

### 2.4 LayoutHost

The host component that:

1. Resolves layout: persisted (localStorage) → `initialLayout` → `strategy.defaultLayout(panelIds)`
2. Renders `strategy.Renderer` with `layout`, `panels`, `onLayoutChange`, `onLayoutPersist`
3. Persists layout to localStorage when `storageKey` is provided (debounced writes)

```tsx
<LayoutHost
  strategy={myStrategy}
  panels={panelRegistry}
  initialLayout={optionalInitial}
  storageKey="my_layout_key"
  onLayoutChange={(layout) => console.log(layout)}
/>
```

---

## 3. Implementing a Custom Layout Strategy

### Step 1: Define the Layout Model

```typescript
// myLayout/types.ts
import type { LayoutModel } from "@paxeer-network/layout-core";

export interface MyLayoutNode {
  type: "panel" | "container";
  id: string;
  children?: MyLayoutNode[];
  size?: string;
}

export interface MyLayoutModel extends LayoutModel {
  root: MyLayoutNode;
  breakpoints?: { sm: number; md: number };
}
```

### Step 2: Implement Serialization

```typescript
// myLayout/utils/serialization.ts
import type { MyLayoutModel } from "../types";

export function serializeMyLayout(layout: MyLayoutModel): string {
  return JSON.stringify(layout);
}

export function deserializeMyLayout(json: string): MyLayoutModel {
  return JSON.parse(json) as MyLayoutModel;
}
```

### Step 3: Create Default Layout

```typescript
// myLayout/utils/defaultLayout.ts
import type { MyLayoutModel } from "../types";

export function createDefaultMyLayout(panelIds: string[]): MyLayoutModel {
  return {
    root: {
      type: "container",
      id: "root",
      children: panelIds.map((id) => ({ type: "panel", id })),
      size: "100%",
    },
    breakpoints: { sm: 640, md: 1024 },
  };
}
```

### Step 4: Implement the Renderer

```typescript
// myLayout/MyRenderer.tsx
import type { LayoutRendererProps } from "@paxeer-network/layout-core";
import type { MyLayoutModel } from "./types";

export function MyRenderer(props: LayoutRendererProps<MyLayoutModel>) {
  const { layout, panels, onLayoutChange, onLayoutPersist } = props;

  const renderNode = (node: MyLayoutModel["root"]) => {
    if (node.type === "panel") {
      const entry = panels.get(node.id);
      return entry ? <div key={node.id}>{entry.node}</div> : null;
    }
    return (
      <div key={node.id} className="my-container">
        {node.children?.map((c) => renderNode(c as MyLayoutModel["root"]))}
      </div>
    );
  };

  return <div className="my-layout">{renderNode(layout.root)}</div>;
}
```

### Step 5: Export the Strategy

```typescript
// myLayout/myStrategy.ts
import type { LayoutStrategy } from "@paxeer-network/layout-core";
import { MyRenderer } from "./MyRenderer";
import type { MyLayoutModel } from "./types";
import { createDefaultMyLayout } from "./utils/defaultLayout";
import { serializeMyLayout, deserializeMyLayout } from "./utils/serialization";

export const myStrategy: LayoutStrategy<MyLayoutModel> = {
  id: "my-layout",
  displayName: "My Custom Layout",
  defaultLayout: createDefaultMyLayout,
  serialize: serializeMyLayout,
  deserialize: deserializeMyLayout,
  Renderer: MyRenderer,
};
```

---

## 4. Trading Panel IDs

Layout strategies use panel IDs to build layouts. The trading package maps these IDs to actual widgets via `createTradingPanelRegistry`.

Standard IDs from `@paxeer-network/layout-core`:

```typescript
import {
  TRADING_PANEL_IDS,
  getTradingPanelIds,
} from "@paxeer-network/layout-core";

// TRADING_PANEL_IDS:
// SYMBOL_INFO_BAR, TRADING_VIEW, ORDERBOOK, DATA_LIST,
// ORDER_ENTRY, MARGIN, ASSETS, MARKETS, HORIZONTAL_MARKETS

const panelIds = getTradingPanelIds(); // ["symbolInfoBar", "tradingView", ...]
```

Use these IDs in your `defaultLayout` and Renderer so panels match the trading page registry.

---

## 5. Presets and LayoutRuleManager

Presets let users choose between multiple layout configurations (e.g. "Pro", "Basic", "Custom").

### LayoutPreset

```typescript
interface LayoutPreset<TRule = unknown> {
  id: string;
  name: string;
  rule: TRule; // Strategy-specific rule (e.g. SplitLayoutRule, GridLayoutRule)
}
```

### LayoutRuleManager

Manages preset selection and per-preset layout persistence:

```typescript
import { LayoutRuleManager } from "@paxeer-network/layout-core";

const manager = new LayoutRuleManager(presets, {
  presetIdStorageKey: "my_preset_id",
  layoutStorageKeyPrefix: "my_layout",
  persist: true, // default; false = in-memory only
});

// Get/set selected preset
manager.getSelectedPresetId();
manager.setSelectedPresetId("pro");

// Storage key for layout persistence: "my_layout_pro"
manager.getLayoutStorageKey();

// Reset current preset to default (clears persisted layout)
manager.reset();

// Check if user has customized layout
manager.hasPersistedLayout();
```

### useLayoutRuleManager

React hook that binds `LayoutRuleManager` to component state:

```typescript
import { useLayoutRuleManager } from "@paxeer-network/layout-core";

const {
  presets,
  selectedPresetId,
  setSelectedPresetId,
  layoutStorageKey,
  reset,
  getSelectedPreset,
  hasPersistedLayout,
} = useLayoutRuleManager(manager);
```

---

## 6. Plugin Integration

Layout plugins intercept `Trading.Layout.Desktop` and inject `layoutStrategy`, `getInitialLayout`, and `storageKey` into `DesktopLayout`.

### 6.1 Using Existing Plugins

**Split layout** (default):

```tsx
import { registerLayoutSplitPlugin } from "@paxeer-network/layout-split";

<PaxeerNetworkPluginProvider plugins={[registerLayoutSplitPlugin()]}>
  <TradingPage />
</PaxeerNetworkPluginProvider>;
```

**Grid layout**:

```tsx
import { registerLayoutGridPlugin } from "@paxeer-network/layout-grid";

<PaxeerNetworkPluginProvider
  plugins={[registerLayoutGridPlugin({ persistLayout: true })]}
>
  <TradingPage />
</PaxeerNetworkPluginProvider>;
```

### 6.2 Customizing Split Presets

```tsx
import { registerLayoutSplitPlugin } from "@paxeer-network/layout-split";
import { getDefaultSplitPresets } from "@paxeer-network/layout-split";

registerLayoutSplitPlugin({
  layouts: (builtIn) => [...builtIn, myCustomPreset],
  gap: 4,
  classNames: {
    /* ... */
  },
});
```

### 6.3 Host-Provided Strategy (No Plugin)

The host can pass strategy directly to `TradingPage`:

```tsx
<TradingPage
  layoutStrategy={myStrategy}
  getInitialLayout={() => createDefaultMyLayout(getTradingPanelIds())}
  storageKey="my_trading_layout"
/>
```

### 6.4 Creating a Layout Plugin

```tsx
import { getTradingPanelIds } from "@paxeer-network/layout-core";
import { createInterceptor } from "@paxeer-network/ui";
import { myStrategy } from "./myStrategy";
import { createDefaultMyLayout } from "./utils/defaultLayout";

export function registerMyLayoutPlugin() {
  return (SDK: PaxeerNetworkSDK) => {
    SDK.registerPlugin({
      id: "my-layout-plugin",
      name: "My Layout",
      version: "1.0.0",
      paxeerNetworkVersion: ">=1.0.0",
      interceptors: [
        createInterceptor("Trading.Layout.Desktop", (Original, props) => (
          <Original
            {...props}
            layoutStrategy={myStrategy}
            getInitialLayout={() =>
              createDefaultMyLayout([...getTradingPanelIds()])
            }
            storageKey="paxeer-network_my_layout"
          />
        )),
      ],
    });
  };
}
```

---

## 7. Strategy Resolution

When multiple strategies are available, use `resolveStrategy`:

```typescript
import { resolveStrategy } from "@paxeer-network/layout-core";

const { strategy, id } = resolveStrategy({
  preferredId: "split", // e.g. from user settings
  availableStrategies: [splitStrategy, gridStrategy, myStrategy],
  defaultStrategy: splitStrategy,
});
```

Resolution order: preferred by ID → defaultStrategy → first available.

---

## 8. Persistence

### LayoutHost Persistence

- Pass `storageKey` to enable persistence.
- Layout is read on mount and when `strategy.id` or `storageKey` changes.
- Writes are debounced (100ms) to avoid excessive localStorage access.

### useLayoutPersistence (Low-Level)

For custom persistence logic:

```typescript
const [layout, setLayout] = useLayoutPersistence(strategy, storageKey, dep);
```

---

## 9. Reference: layout-core Exports

| Category  | Export                                                                             | Description                               |
| --------- | ---------------------------------------------------------------------------------- | ----------------------------------------- |
| Types     | `LayoutModel`, `PanelRegistry`, `PanelRegistryEntry`                               | Core types                                |
| Types     | `LayoutStrategy`, `LayoutRendererProps`, `LayoutHostProps`                         | Strategy protocol                         |
| Types     | `OnLayoutChange`, `OnLayoutPersist`, `StrategyResolverOptions`                     | Callbacks and options                     |
| Constants | `TRADING_PANEL_IDS`, `getTradingPanelIds`                                          | Panel IDs                                 |
| Component | `LayoutHost`                                                                       | Host component                            |
| Hooks     | `useLayoutPersistence`, `useLayoutRuleManager`                                     | Persistence and preset management         |
| Utils     | `resolveStrategy`, `LayoutRuleManager`, `LayoutPreset`, `LayoutRuleManagerOptions` | Strategy resolution and preset management |

---

## 10. Examples in Codebase

- **layout-split**: `packages/layout-split/src/splitStrategy.ts`, `SplitRenderer.tsx`, `SplitPresetContext.tsx`
- **layout-grid**: `packages/layout-grid/src/gridStrategy.ts`, `GridRenderer.tsx`, `GridDesktopInjector.tsx`
- **trading**: `packages/trading/src/pages/trading/trading.ui.desktop.tsx`, `TradingPanelRegistry.tsx`
