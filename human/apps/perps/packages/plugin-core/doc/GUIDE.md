---
domain: plugin-core
docType: guide
package: "@paxeer-network/plugin-core"
intentTags: [plugin, widget, layout, interceptor, target, integration]
lang: en
---

# paxeer-network SDK Plugin Development Guide

The paxeer-network SDK uses an advanced “OS-level” architecture that lets you deeply customize the trading terminal. You are no longer limited to patching the page—you participate directly in core business flows by intercepting the rendering pipeline.

## 1. Core concepts and plugin types

### 1.1 Core principles

Before you start building, understand these two principles:

#### 1.1.1 Use SDK packages directly

Plugins can use everything under `@paxeer-network/**` the same way the host app does—for example `@paxeer-network/hooks` for account, order book, and other data and business logic; `@paxeer-network/ui` for UI components; `@paxeer-network/utils` for helpers. The SDK also injects an `api` object in contexts such as interceptors (today mainly `api.events` for subscriptions), so you can use a single surface even outside components (e.g. in `setup`).

#### 1.1.2 Interception targets must be supported by the SDK

UI changes are not done with classic “slots”; they use **UI interceptors**. **Only components the SDK has declared as interceptable can be targeted.** The SDK registers internals via `injectable` in a path system; only registered paths are valid targets. By setting a **component path** (e.g. `Trading.OrderEntry.SubmitSection`), you can wrap, replace, or enhance that component. Use the Inspector to list all interceptable paths.

### 1.2 Supported plugin types

| Type       | Definition                                           | Integration                                                                  | Typical use cases                                 |
| :--------- | :--------------------------------------------------- | :--------------------------------------------------------------------------- | :------------------------------------------------ |
| **Page**   | Full page built with SDK UI and Hooks                | Host mounts it as an ordinary route component                                | Standalone pages (e.g. campaigns)                 |
| **Widget** | UI block anchored in the trading page                | Interceptor declares a `target` path and injects at that node                | PnL card beside order entry, custom buttons, etc. |
| **Layout** | Container that restructures the whole trading layout | Intercept `Trading.Layout.Desktop`, inject layout strategy, rearrange panels | Multi-column, sidebars, custom chrome             |

#### Widget (component plugin)

- **Definition**: A UI block anchored at a specific place in the trading page.
- **Integration**: Interceptor pattern—declare interception of a **path** in the `interceptors` array; `component` is `(Original, props, api) => ReactNode`. Your JSX can wrap, prepend, append, or fully replace `Original`.
- **Dynamics**: No pre-defined slots required; set `target` to the anchor path (e.g. `Trading.OrderEntry.SubmitSection`) and the SDK injects there.
- **Use cases**: PnL analyzers, one-click close buttons, fee overlays, top/bottom nav areas, etc.

#### Page (page plugin)

- **Definition**: A full page built with paxeer-network SDK UI and Hooks.
- **How to build**: Use SDK UI and Hooks directly—no extra interceptors or registry; styling can use paxeer-network’s Tailwind-style classes.
- **Integration**: Not via SDK interceptors/registry—the host mounts the page as a **normal route** (React Router, Next.js Router, etc.).
- **Use cases**: Asset overview, order history, competition leaderboards, profile/settings.

#### Layout (layout plugin)

- **Definition**: A container that restructures the trading page layout. It is implemented as a layout strategy defined by LayoutCore, then wrapped with the plugin mechanism for ease of use.
- **Scope**: **Only the trading page layout can be extended today**; other pages are not customizable this way.
- **Integration**: Intercept **`Trading.Layout.Desktop`**. When the host does not pass `layoutStrategy` / `getInitialLayout`, the plugin overrides props on `DesktopLayout`, injecting strategy and `getInitialLayout`, which still render `LayoutHost`. Strategy types come from `@paxeer-network/layout-core`.
- **Use cases**: Multi-column, sidebars, portrait mobile layouts, pro vs. minimal trading UIs.
- **Two usage patterns**:
  - **As a plugin**: `PaxeerNetworkPluginProvider` + `registerLayoutSplitPlugin()`—the host passes no layout props to `TradingPage`; the plugin owns split layout.
  - **Host props**: The host passes `layoutStrategy={gridStrategy}` and `getInitialLayout={() => createTradingGridLayout()}` for grid layout.
- **Example**: `@paxeer-network/layout-grid` exposes **`registerLayoutGridPlugin(options?)`**, intercepts `Trading.Layout.Desktop`, and injects the grid strategy when the host omits it; use `plugins={[registerLayoutGridPlugin()]}` or supply `getInitialLayout` for a custom initial grid.

---

## 2. Quick start

### 2.1 Plugin shape

A standard paxeer-network plugin is an **npm package plus a register function**: export a function (or default export) that calls `PaxeerNetworkSDK.registerPlugin`. It includes metadata, logic, and UI interception rules:

```typescript
// @paxeer-network/plugin-example/index.ts
import React from "react";
import type { PaxeerNetworkSDK } from "@paxeer-network/ui";
import { useOrderEntry } from "@paxeer-network/hooks";

/**
 * Registers a plugin that replaces the Long/Short combo in Order Entry with separate Buy / Sell buttons.
 * @param options Optional styling, etc.
 * @returns Function the SDK invokes at registration with the SDK instance and optional pluginState
 */
export default function registerBuySellButtonsPlugin(options?: { className?: string }) {
  return (SDK: PaxeerNetworkSDK, state?: unknown) =>
    SDK.registerPlugin({
      id: "my-buy-sell-buttons", // Globally unique ID (paxeer-network plugin dev Skill recommended—see below)
      name: "Buy / Sell Buttons",
      version: "1.0.0",
      paxeerNetworkVersion: ">=3.0.0", // Required SDK range

      interceptors: [
        {
          target: "Trading.OrderEntry.SubmitSection",
          // Note: component is a plain function—do not call Hooks here. Return a wrapper that uses Hooks.
          component: (Original, props, api) => {
            const Wrapper = () => {
              const symbol = (props as { symbol?: string }).symbol ?? "PERP_BTC_USDC";
              const { submit, setValue } = useOrderEntry(symbol);
              const placeOrder = (side: "BUY" | "SELL") => () => {
                setValue("side", side);
                submit();
              };
              return (
                <div className={options?.className ?? "flex gap-2"}>
                  <button type="button" onClick={placeOrder("BUY")}>Buy</button>
                  <button type="button" onClick={placeOrder("SELL")}>Sell</button>
                </div>
              );
            };
            return <Wrapper />;
          },
        },
      ],

      // Optional: non-UI entry (event subscriptions, etc.)
      setup: (api) => {
        api.events.on("place_order_success", (data) => {
          console.log("Order created", data);
        });
      },

      onInstall: () => {},
    });
}
```

### 2.1.1 Typing `props` in interceptors

You can give `props` explicit types in interceptor `component` functions for IntelliSense and type-checking.

**Option A: `createInterceptor` (recommended)**

After importing the target UI package, `createInterceptor` infers `props` from `target`:

```typescript
import { createInterceptor } from "@paxeer-network/plugin-core";
import "@paxeer-network/ui"; // pull in augmentation

interceptors: [
  createInterceptor("Deposit.DepositForm", (Original, props, api) => {
    // props inferred as DepositFormProps (onOk, position, etc.)
    return <div onClick={props.onOk}>Custom form</div>;
  }),
  createInterceptor("Table.EmptyDataIdentifier", (Original, props, api) => {
    // props inferred as EmptyDataStateProps (title, className)
    return <Original {...props} title={props.title ?? "No data"} />;
  }),
]
```

**Option B: explicit generic**

```typescript
import type { PluginInterceptor, DepositFormProps } from '@paxeer-network/ui';

const interceptor: PluginInterceptor<DepositFormProps> = {
  target: 'Deposit.DepositForm',
  component: (Original, props, api) => {
    // props: DepositFormProps
    return <div onClick={props.onOk}>...</div>;
  },
};
```

**Option C: inline annotation**

```typescript
import type { DepositFormProps } from '@paxeer-network/ui';

component: (Original, props: DepositFormProps, api) => { ... }
```

UI packages (`@paxeer-network/ui`, `@paxeer-network/ui-scaffold`, `@paxeer-network/trading`) export Props types for their injectable components—import as needed.

### 2.2 Developer tools

- **Inspector**: Enable the SDK Inspector in development. It highlights all interceptable paths; hover a control to see its **Target Path** (e.g. `Trading.OrderEntry.SubmitSection`), which you use in interceptors.
- **Types**: Full TypeScript support and path completion when writing interceptors.

### 2.3 paxeer-network plugin development Skill

paxeer-network ships an **paxeer-network plugin development Skill** (usable in Cursor and similar assistants) to speed up scaffolding—you do not have to hand-roll templates.

- **Plugin IDs**: Generate a spec-compliant global ID for `registerPlugin({ id: '...' })` locally—no web login or extra CLI required.
- **Scaffolding**: Page / Widget / Layout skeletons, interceptor samples, `api` / Hooks usage.
- **Typical use**: New plugin projects; backfilling missing IDs; minimal runnable Widget examples.

In Cursor, invoke it from the skills list or rules; phrases like “create paxeer-network plugin” or “generate plugin ID / scaffold” trigger it.

### 2.4 Plugin ID

The unique ID is used for API provenance (which plugin issued a request), per-plugin rate limits, usage, and auditing—a **prerequisite** for plugin work.

- **Generation**: Prefer the **paxeer-network plugin development Skill** (§2.3) for one-click local IDs; you may also define your own globally unique string per the rules below.
- **What developers see**: Set `id` in `registerPlugin({ id: '...' })`. When interceptor output renders, `PluginScopeProvider` wraps it; `@paxeer-network/hooks` hooks such as `useQuery` read `pluginId` via `usePluginScope()` and attach the `X-paxeer-network-Plugin-Id` header.

#### 2.4.1 ID format

IDs must match: `/^[a-zA-Z][a-zA-Z0-9]*$/`

| Part           | Meaning                           |
| :------------- | :-------------------------------- |
| `^`            | Start of string                   |
| `[a-zA-Z]`     | First character must be a letter  |
| `[a-zA-Z0-9]*` | Zero or more letters/digits after |
| `$`            | End of string                     |

**Rules**: First character is a letter; only letters and digits; no underscores, hyphens, dots, etc.

| Valid                                          | Invalid                                               |
| :--------------------------------------------- | :---------------------------------------------------- |
| `myPlugin`, `PnlWidget`, `LayoutSplit123`, `A` | `my-plugin`, `my_plugin`, `123plugin` (leading digit) |

---

## 3. Data and interaction: Plugin API and Hooks

Two complementary approaches:

- **`@paxeer-network/hooks`**: Import and use the same hooks as the host (`useAccount`, `useOrderBook`, …). **Prefer using hooks inside wrapper components returned from interceptors.**
- **Injected `api`**: Available in `setup` and similar non-component paths. **Today** this centers on `api.events`; `data`, `actions`, and `utils` are placeholders for future expansion.

### Interface (injected `api`)

**Currently implemented:**

```typescript
interface PaxeerNetworkPluginAPI {
  // Events – global EventEmitter, TrackerEventName events
  events: {
    on: (event: TrackerEventName, handler: (data: unknown) => void) => void;
    off: (event: TrackerEventName, handler: (data: unknown) => void) => void;
  };
  // Placeholders for future versions
  data?: Record<string, unknown>;
  actions?: Record<string, unknown>;
  utils?: Record<string, unknown>;
}
```

Supported events include `place_order_success`, `deposit_success`, `withdraw_success`, etc. (`TrackerEventName` in `@paxeer-network/types`).

### Examples

#### Hooks inside interceptors (recommended)

Interceptor `component` functions are plain—**do not call hooks in the outer function**. Return an inner component that uses hooks:

```tsx
// ✅ Correct: wrapper component uses hooks
interceptors: [
  {
    target: "Trading.OrderEntry.SubmitSection",
    component: (Original, props, api) => {
      const Wrapper = () => {
        const { balance } = useAccount();
        return (
          <div>
            <span>Balance: {balance}</span>
            <Original {...props} />
          </div>
        );
      };
      return <Wrapper />;
    },
  },
];
```

#### `setup` and `api` (non-UI)

```typescript
// ✅ Subscribe in setup
setup: (api) => {
  api.events.on("place_order_success", (data) => {
    console.log("Order created", data);
    // analytics, logging, etc.
  });
};
```

#### Anti-patterns

```typescript
// ❌ Wrong: hooks in the interceptor body (breaks Rules of Hooks)
// component: (Original, props, api) => {
//   const { balance } = useAccount();  // invalid
//   return <div>{balance}</div>;
// }

// ❌ Avoid: internal globals (unsupported, may change)
// const store = window.paxeer-network_internal_store;
```

---

## 4. UI customization: interceptors

Interceptors are the most powerful customization surface—you can change SDK-rendered UI.

### 4.1 Writing interceptors

`component` is `(Original, props, api) => ReactNode`: `Original` is the inner component, `props` are the anchor props, `api` is the plugin API. Wrap, prepend, append, or replace `Original` in JSX. **Again:** the outer function is not a component—return an inner component if you need hooks; data and actions come from `@paxeer-network/hooks`.

**Example: hint above the submit area.**

```tsx
import { useAccount } from "@paxeer-network/hooks";
import { Flex, Text } from "@paxeer-network/ui";

interceptors: [
  {
    target: "Trading.OrderEntry.SubmitSection",
    component: (Original, props, api) => {
      const BalanceHint = () => {
        const { balance } = useAccount();
        return (
          <Flex direction="column" gap={2}>
            {balance < 100 && (
              <Text className="oui-text-danger oui-text-sm">
                ⚠️ Low balance—please deposit
              </Text>
            )}
            <Original {...props} />
          </Flex>
        );
      };
      return <BalanceHint />;
    },
  },
];
```

> **Implementation note**: The SDK uses `injectable` HOCs to register paths. Multiple plugins can target the same path; interceptors chain in registration order (onion model), outer to inner at render time.

### 4.2 Three common strategies

1. **Enhance**: Extra UI beside `Original` (example above).
2. **Wrap**: `Provider` or layout wrapper around `Original`.
3. **Replace**: Omit `Original` entirely—ensure you preserve critical behavior.

> **Design**: Prefer paxeer-network base UI components for visual consistency.

### 4.3 OrderEntry intercept targets (split by region)

For fine-grained customization of the order entry area, `OrderEntry` exposes these targets:

| Target path                         | Region                                             | Typical use                         |
| :---------------------------------- | :------------------------------------------------- | :---------------------------------- |
| `Trading.OrderEntry.TypeTabs`       | Limit / Market / Advanced order type area          | Adjust labels, layout, extra hints  |
| `Trading.OrderEntry.BuySellSwitch`  | Buy / Sell toggle                                  | Custom toggles, risk hints          |
| `Trading.OrderEntry.Available`      | Available balance row                              | Extend balance info, add shortcuts  |
| `Trading.OrderEntry.QuantitySlider` | Size slider and Max buy/sell                       | Custom steps, quick percentages     |
| `Trading.OrderEntry.SubmitSection`  | Submit button + est. liquidation / slippage / fees | Wrap submit flow, replace submit UI |

> All of the above support typed `props` via `createInterceptor(target, (Original, props) => ...)` after importing `@paxeer-network/ui-order-entry`.

#### 4.3.1 Example: wrap SubmitSection and enhance Available

```tsx
import React from "react";
import { createInterceptor } from "@paxeer-network/plugin-core";
import "@paxeer-network/ui-order-entry";

interceptors: [
  createInterceptor("Trading.OrderEntry.SubmitSection", (Original, props) => {
    return (
      <div className="oui-space-y-1">
        <div className="oui-text-2xs oui-text-warning">
          Confirm order parameters before submitting
        </div>
        <Original {...props} />
      </div>
    );
  }),
  createInterceptor("Trading.OrderEntry.Available", (Original, props) => {
    return (
      <div className="oui-space-y-1">
        <Original {...props} />
        <div className="oui-text-2xs oui-text-base-contrast-54">
          Custom hint: deposit first when available balance is low
        </div>
      </div>
    );
  }),
];
```

---

## 5. UI/UX and theme consistency

### 5.1 Why UI and theme matter

Plugins render inside the host trading UI and should match the **runtime theme** (`PaxeerNetworkThemeProvider`, light/dark, profit/loss colors). If you use unprefixed generic Tailwind (`flex`, `bg-white`, `text-red-500`) or raw HTML instead of the UI kit, styles often **do not** flow through paxeer-network design tokens and `oui-` utilities, which can break on theme switch or host customization.

### 5.2 Prefer `@paxeer-network/ui`

When building plugin UI, **prefer** components from `@paxeer-network/ui`:

- **Layout**: `Box`, `Flex`, `Grid` (and `Grid.span`)
- **Interaction**: `Button`, `Input`, `Checkbox`, `Dialog`, `Sheet`, `Tabs`, etc.

See the **`@paxeer-network/ui`** package for the full list. Avoid bare `<button>`, `<input>`, or unstyled `<div>` stacks when a component exists—they bypass tokens and theming.

### 5.3 Layout examples: `Flex` and `Grid`

**Vertical stack, spacing, alignment** (`Flex` is built on `Box` and applies `oui-flex` styles):

```tsx
import { Flex, Button } from "@paxeer-network/ui";

<Flex direction="column" gap={3} itemAlign="stretch">
  <Button>Primary action</Button>
  <Button variant="outlined">Secondary</Button>
</Flex>;
```

**Grid layout** (cards, settings):

```tsx
import { Grid } from "@paxeer-network/ui";

<Grid cols={2} gap={3}>
  <Grid.span colSpan={1}>Left</Grid.span>
  <Grid.span colSpan={1}>Right</Grid.span>
</Grid>;
```

More layout props are defined in **`@paxeer-network/ui`** exports and types.

### 5.4 Custom `className` and the `oui-` prefix

Host and UI packages typically enable Tailwind with the **`oui-` prefix** (`OUITailwind` preset from **`@paxeer-network/ui`**). When adding styles in plugins:

- Use prefixed utilities: `oui-gap-2`, `oui-rounded-lg`, `oui-bg-base-8`, `oui-text-base-contrast`, `oui-text-danger`. Do not rely on unprefixed `gap-2`, `bg-white`, etc.—they may not compile or may diverge from the host.
- `Flex`, `Grid`, and similar components merge `oui-*` via `tailwind-variants`; extra `className` should still use `oui-*` utilities.

For semantic hooks that hosts or custom CSS can target, use meaningful names still under the `oui-` prefix, e.g. `oui-myPnlWidget-root`.

If the plugin package maintains its own `tailwind.config`, import paxeer-network’s preset and plugins (`import { OUITailwind } from "@paxeer-network/ui"`, `presets: [OUITailwind.theme]`, `OUITailwind.base()`, `OUITailwind.components()`, etc.) so `oui-*` output matches the host.

### 5.5 Common CSS variables (`--oui-*`)

A short reference for custom CSS; the **full set** lives in the installed **`@paxeer-network/ui`** light/dark themes (names are stable across themes, values change).

| Variable                                                                  | Meaning (brief)                                    |
| :------------------------------------------------------------------------ | :------------------------------------------------- |
| `--oui-font-family`                                                       | Font stack                                         |
| `--oui-color-primary`, `--oui-color-primary-contrast`                     | Primary and contrast                               |
| `--oui-color-link`, `--oui-color-link-light`                              | Link colors                                        |
| `--oui-color-secondary`, `--oui-color-tertiary`, `--oui-color-quaternary` | Secondary surfaces / text                          |
| `--oui-color-danger`, `--oui-color-success`, `--oui-color-warning`        | Semantic colors (+ light/darken/contrast variants) |
| `--oui-color-base-1` … `--oui-color-base-10`                              | Neutral ramp                                       |
| `--oui-color-base-foreground`                                             | Default foreground                                 |
| `--oui-color-line`                                                        | Borders / dividers                                 |
| `--oui-color-trading-profit`, `--oui-color-trading-loss`                  | P&L colors                                         |

Color variables use **RGB triplets (space-separated, no commas)** for `rgb(var(--oui-color-primary) / <alpha-value>)`, not `#RRGGBB` strings. Prefer `oui-*` Tailwind classes; if you write raw CSS, align with your **`@paxeer-network/ui`** version.

---

## 6. Plugin types and use cases

Choose a plugin shape based on your goal:

| Type       | Description                                                                                                                                                                                 | Typical use cases                                 |
| :--------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | :------------------------------------------------ |
| **Widget** | Declare a `target` path in an interceptor to mount a UI block at an anchor on the trading page.                                                                                             | PnL tools, one-click close, fee overlays.         |
| **Page**   | Full page from SDK UI/Hooks; the host mounts it as a normal route—no SDK interceptor/registry.                                                                                              | Assets, history, leaderboards, profile.           |
| **Layout** | Trading page layout only. Intercept **`Trading.Layout.Desktop`** and inject `layoutStrategy` / `getInitialLayout` when the host omits them; strategies from `@paxeer-network/layout-core`. | Multi-column, portrait mobile, pro/minimal modes. |

---

## 7. Lifecycle management

The SDK drives plugin lifecycle; hook into the stages you need.

> Details: [TECH.md](./TECH.md), section 5.

| Hook           | When                     | Typical use                      |
| :------------- | :----------------------- | :------------------------------- |
| `onInitialize` | After load, before mount | State init, bus subscriptions    |
| `onInstall`    | First install            | Version checks, capability gates |
| `onError`      | Interceptor throws       | Custom error handling            |
| `onFallback`   | Interceptor throws       | Custom fallback UI               |

**Note:** `onMount`, `onUnmount`, and `onDispose` are not implemented in plugin-core today.

`setup(api)` is optional and runs at registration for subscriptions and other non-UI logic.

### Example

```typescript
export default function registerMyPlugin(options?: { theme?: string }) {
  return (SDK: PaxeerNetworkSDK, state?: { paxeerNetworkVersion?: string }) =>
    SDK.registerPlugin({
      id: "my-plugin",
      // ... other fields

      setup: (api) => {
        api.events.on("place_order_success", (data) => {
          console.log("Order created", data);
        });
      },

      onInstall: () => {
        if (state && !isVersionCompatible(state.paxeerNetworkVersion)) {
          throw new Error("Incompatible SDK version");
        }
      },
    });
}
```

---

## 8. Security and best practices

### 8.1 Error isolation

- Each interceptor is wrapped in `PluginErrorBoundary`.
- Crashes localize to that slot (blank or fallback)—**not** a full trading page white screen.

### 8.2 Performance

- **Memoized interceptor pipeline**: multiple layers only re-render when dependent data changes—nesting does not necessarily melt performance.
- **Avoid heavy work** in hot interceptor paths.
- Use `React.memo` / `useMemo` when helpful.

### 8.5 Engineering benefits

| Aspect                  | Benefit                                                      |
| :---------------------- | :----------------------------------------------------------- |
| **No extra fetch**      | Plugins ship in the main bundle—no runtime HTTP plugin loads |
| **Tree shaking**        | Unused plugins drop out at build time                        |
| **Types / navigation**  | Jump-to-definition on registration APIs                      |
| **Debugging**           | Same as any package in `node_modules` or local path          |
| **Registration timing** | Register at startup or dynamically                           |

---

## 9. Host integration

For **host app developers**: how to wire a finished plugin into a DApp (separate from authoring the plugin).

Install the plugin via npm and call its register function during bootstrap; pass the **invoked** factories to `PaxeerNetworkPluginProvider` (`@paxeer-network/ui`) `plugins`—e.g. `registerPnlPlugin()` or `registerCustomThemePlugin({ primaryColor: '#00ff00' })`. The SDK passes the current instance and optional `pluginState`; hosts do not hand in `SDK` manually:

```tsx
// App.tsx
import registerPnlPlugin from "@paxeer-network/plugin-pnl";
import { PaxeerNetworkPluginProvider } from "@paxeer-network/ui";
import registerCustomThemePlugin from "./my-local-plugins/theme";

function App() {
  return (
    <PaxeerNetworkPluginProvider
      plugins={[
        registerPnlPlugin(),
        registerCustomThemePlugin({ primaryColor: "#00ff00" }),
      ]}
    >
      <TradingPage />
    </PaxeerNetworkPluginProvider>
  );
}
```

- Exported register functions accept options (theme, endpoints, …).
- Local dev: `import` a folder directly without publishing.
- **When**: Usually before `PaxeerNetworkPluginProvider` mounts so interceptors exist before render.

---

## Summary

The paxeer-network SDK plugin model combines **interceptors for UI flexibility** with **facade-style boundaries for data safety**. Plugins ship as **npm packages plus register functions** (optional config); hosts pass **called** factories to `PaxeerNetworkPluginProvider` `plugins` (e.g. `registerPnlPlugin()`, `registerCustomPositionsPlugin({ primaryColor: '...' })`); the SDK completes `registerPlugin` with the live instance and optional `pluginState`. Bundlers handle packing and tree shaking; the SDK owns the global registry and resolution/injection.
