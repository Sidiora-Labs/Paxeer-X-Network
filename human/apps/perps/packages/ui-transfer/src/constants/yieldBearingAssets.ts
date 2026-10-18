/**
 * Yield-bearing collateral assets configuration
 * These assets earn APY rewards while being used as collateral for trading
 */

export interface YieldBearingAsset {
  /** Asset symbol (e.g., "YUSD", "deUSD") */
  symbol: string;
  /** Asset display name */
  displayName: string;
  /** API endpoint to fetch APY data */
  apyApiUrl: string;
  /** Path to extract APY value from API response (e.g., "data.efficient_apr") */
  apyPath: string;
  /** External link to the asset issuer's website */
  externalUrl: string;
}

/**
 * Configuration for all supported yield-bearing assets
 * Chain support is determined by the backend API (/v1/public/token)
 * via the chain_details array for each token
 */
export const YIELD_BEARING_ASSETS: YieldBearingAsset[] = [
  {
    symbol: "YUSD",
    displayName: "YUSD",
    apyApiUrl: "https://api.aegis.im/api/project-stats",
    apyPath: "data.efficient_apr",
    externalUrl: "https://aegis.im/",
  },
  {
    symbol: "deUSD",
    displayName: "deUSD",
    apyApiUrl: "https://api-deusd-prod-public.elixir.xyz/public/deusd_apy",
    apyPath: "deusd_apy",
    externalUrl: "https://www.elixir.xyz/deusd",
  },
];

/**
 * Check if a token is a yield-bearing asset
 * Note: This only checks if the token symbol is configured as yield-bearing.
 * Chain availability is determined by the backend API's chain_details.
 *
 * @param symbol - Token symbol to check
 * @returns true if the token is configured as yield-bearing
 */
export function isYieldBearingAsset(symbol?: string): boolean {
  if (!symbol) return false;
  return YIELD_BEARING_ASSETS.some(
    (asset) => asset.symbol.toLowerCase() === symbol.toLowerCase(),
  );
}

/**
 * Get yield-bearing asset configuration by symbol
 */
export function getYieldBearingAsset(
  symbol?: string,
): YieldBearingAsset | undefined {
  if (!symbol) return undefined;
  return YIELD_BEARING_ASSETS.find(
    (asset) => asset.symbol.toLowerCase() === symbol.toLowerCase(),
  );
}
