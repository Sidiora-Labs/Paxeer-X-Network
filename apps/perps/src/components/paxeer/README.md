# Paxeer Components — Next.js Drop-in Package

39 production-ready React components built on the Paxeer brand system: trading surfaces, portfolio analytics, leaderboards, and novel primitives (gesture unlock, gas-weather widget, topographic orderbook, etc.). Every component is a named TSX export with `"use client"` ready for Next.js App Router.

---

## Install

### 1. Copy files

```
your-app/
├── app/
│   └── globals.css
├── components/
│   └── paxeer/              ← copy this folder
│       ├── _shared.tsx
│       ├── ActiveOrders.tsx
│       ├── … 38 more
│       └── index.ts
├── public/
│   └── fonts/               ← copy the 6 OTFs here
└── styles/
    └── paxeer-tokens.css    ← copy this file
```

### 2. Fonts

Drop the 6 `PPPangramSansRounded-*.otf` files into `public/fonts/`. The CSS already references them.

### 3. Wire the tokens stylesheet

In `app/layout.tsx` (or wherever your root styles are imported):

```tsx
import "@/styles/paxeer-tokens.css";
import "./globals.css";
```

You'll also want a minimal base:

```css
/* globals.css */
body {
  background: var(--bg);
  color: var(--fg);
  font-family: var(--font-body);
}
```

### 4. Dependencies

Only React 18+. No other runtime deps. TypeScript recommended.

```json
{
  "dependencies": {
    "react": "^18.3.0",
    "react-dom": "^18.3.0"
  }
}
```

---

## Usage

```tsx
"use client";
import { OrderBook, CumulativePnL, Leaderboard, Overview } from "@/components/paxeer";

export default function DashboardPage() {
  return (
    <main style={{ display: "grid", gap: 16, padding: 24 }}>
      <Overview />
      <CumulativePnL />
      <OrderBook />
      <Leaderboard />
    </main>
  );
}
```

---

## Wiring live data

Most components ship with **sensible placeholder data** so they render out-of-the-box. To plug in real data, you have three options:

### Option A: Fork the component (simplest)

Open e.g. `components/paxeer/OrderBook.tsx`, replace the hard-coded arrays with props or data fetched from your API / SDK. The data shapes are all at the top of each file.

### Option B: Pass props (recommended going forward)

Many components are structured so the data arrays sit at the top of the function — easy to lift into a `props` object. For example, for `Leaderboard.tsx`:

```tsx
type Row = { rank: number; addr: string; pnl: number; trades: number; wr: number; hold: string; vol: number; me?: boolean };
export function Leaderboard({ rows }: { rows?: Row[] }) {
  const data = rows ?? DEFAULT_ROWS;
  // …
}
```

Then in a Next.js server component:

```tsx
import { Leaderboard } from "@/components/paxeer";

export default async function Page() {
  const rows = await fetch("https://api.paxeer.io/leaderboard?range=7d").then(r => r.json());
  return <Leaderboard rows={rows} />;
}
```

### Option C: Wrap with a data-fetching HOC

For live-updating components (Orderbook, Recent Trades, Reaction Rail), wrap with a hook:

```tsx
"use client";
import useSWR from "swr";
import { OrderBook } from "@/components/paxeer";

export function LiveOrderBook({ pair }: { pair: string }) {
  const { data } = useSWR(`/api/orderbook/${pair}`, { refreshInterval: 1000 });
  return <OrderBook data={data} />;
}
```

---

## Component map

### Primitives
| Component | Props |
|---|---|
| `AddressChip` | `address`, `ens?` |
| `SmartButton` | `label`, `onClick?`, `variant?` |
| `DataInput` | `label`, `value`, `onChange` |
| `NetworkPill` | `chainId?` |
| `ConfirmSlider` | `onConfirm` |

### Trading
| Component | Description |
|---|---|
| `OrderEntry` | Buy/sell limit + market entry |
| `OrderBook` | Classic L2 book, bids + asks |
| `RecentTrades` | Trade tape |
| `DepthChart` | Bid/ask cumulative-depth curve |
| `PositionCard` | Position with liq distance, funding, P&L |
| `MagneticSwap` | Novel token swap with magnetized inputs |

### Analytics
| Component | Description |
|---|---|
| `Overview` | 4-stat portfolio strip |
| `Performance` | 7D KPIs + daily P&L bars |
| `CumulativePnL` | Area chart w/ 1D-ALL range toggle |
| `FeesBreakdown` | Donut of fee categories |
| `ActiveOrders` | Open-orders table |
| `ClosedPositions` | Realized-P&L log |
| `TradesTable` | Full trade history |
| `TransfersHistory` | Deposits + withdrawals |

### Social
| Component | Description |
|---|---|
| `Leaderboard` | Ranked table w/ medals + YOU row |
| `LeaderboardStats` | 4-tile cumulative totals |
| `TraderCard` | Profile + copy-trade |
| `ReactionRail` | Live emoji bursts |

### Novel / experimental
| Component | Description |
|---|---|
| `CommandPalette` | ⌘K fuzzy jump |
| `LeverageDial` | Rotary 1×–100× |
| `GestureUnlock` | 3×3 pattern signer |
| `TradePathSankey` | Value-flow ribbon diagram |
| `OrderbookTopo` | Orderbook as topographic heatmap |
| `GhostHold` | P&L vs. HODL-overlay |
| `GasWeather` | Gwei as weather |
| `TxSimulator` | Before/after balance diff |
| `RiskDial` | Composite 0–100 risk gauge |
| `EpochCountdown` | Concentric countdown rings |
| `TransferModal` | Deposit/withdraw modal (`initialMode?: "deposit"\|"withdraw"`) |

---

## Customization

All visual tokens are CSS variables on `:root` in `paxeer-tokens.css`. Override in your own stylesheet or set at runtime:

```ts
document.documentElement.style.setProperty("--accent", "#05C168");
```

### Dark/light mode

Everything's dark-mode by design. For a light variant, override `--bg`, `--fg`, and `--border*` under a `[data-theme="light"]` selector.

---

## Types

The `_shared.tsx` module exports a `Token` type and the `TOKENS` array used by most components:

```tsx
import { TOKENS, Token, fmt, shortAddr } from "@/components/paxeer";
```

---

## Notes / caveats

- **Placeholder data** is currently hard-coded in each component. Lift it to props before shipping.
- **Icons** are inline SVGs — no icon library required, but also not tree-shaken. If bundle size matters, pull the ones you need from `_shared.tsx`'s `Icon` map.
- **Tweaks** (grid / density) are baked into the demo gallery only, not the components.
- **SSR-safe**: every component is marked `"use client"` because most use hooks or event handlers.

---

Built on Paxeer Brand Identity System v2.0. For source gallery and design rationale, see the standalone `Paxeer Components Gallery.html`.
