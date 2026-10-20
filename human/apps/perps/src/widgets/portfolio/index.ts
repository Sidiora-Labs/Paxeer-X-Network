// Barrel exports for the /portfolio page widgets.
// Consumed by `@/page-modules/portfolio/index.tsx` and any future surface
// (drawer, mini-portfolio popover) that wants to render portfolio panels.

// ── Layout / chrome ──────────────────────────────────────────────────
export { PortfolioSideNav, PORTFOLIO_NAV_ITEMS } from "./PortfolioSideNav";
export type { PortfolioNavKey } from "./PortfolioSideNav";
export { PortfolioMobileNav } from "./PortfolioMobileNav";
export { PortfolioTopBar } from "./PortfolioTopBar";

// ── Tab panels ───────────────────────────────────────────────────────
export { PortfolioOverviewPanel } from "./PortfolioOverviewPanel";
export { PortfolioPositionsPanel } from "./PortfolioPositionsPanel";
export { PortfolioFeeTierPanel } from "./PortfolioFeeTierPanel";
export { PortfolioSettingsPanel } from "./PortfolioSettingsPanel";

// ── Existing data widgets reused inside tabs ─────────────────────────
export { TradeSettlementHistory } from "./TradeSettlementHistory";
export { OnChainHistoryPanel } from "./OnChainHistoryPanel";
export { FeesBreakdownCard } from "./FeesBreakdownCard";
export { MobilePortfolioOverview } from "./MobilePortfolioOverview";

// ── Shared primitives ────────────────────────────────────────────────
export {
  SectionCard,
  EmptyState,
  PlaceholderPanel,
  ConnectGate,
} from "./PortfolioPrimitives";

// ── Types ────────────────────────────────────────────────────────────
export type { TokenBalance } from "./types";
