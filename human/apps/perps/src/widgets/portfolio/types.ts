/**
 * Shared types for the /portfolio page widgets.
 *
 * `TokenBalance` is the canonical shape used across PortfolioOverviewPanel,
 * PortfolioSpotPanel, MobilePortfolioOverview, and the page-level balance
 * fetcher. Centralised here so widgets don't drift on field names.
 */

export interface TokenBalance {
  symbol: string;
  address: `0x${string}`;
  decimals: number;
  vaultBalance: bigint;
  lockedBalance: bigint;
  walletBalance: bigint;
  available: bigint;
}
