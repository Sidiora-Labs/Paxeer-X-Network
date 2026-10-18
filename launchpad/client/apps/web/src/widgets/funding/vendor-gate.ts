/** The frozen capability catalog contains no qualified chain125 fiat vendor.
 * Redirects, callbacks and a payment authorization cannot establish settlement. */
export const fiatFundingAvailability = Object.freeze({
  available: false as const,
  reason: 'Fiat funding unavailable: vendor delivery and settlement on chain 125 remain unqualified.',
  releaseBlocker: 'D09_008: qualified vendor sandbox payment and chain125 settlement proof required',
  alternative: 'Send PAX or USDL to your wallet from another wallet or exchange.',
});
export function openFiatCheckout(): never { throw new Error(fiatFundingAvailability.reason); }
export type FundingOrderStatus = 'quoted' | 'provider_checkout' | 'authorized' | 'settling' | 'onchain_confirmed' | 'failed' | 'cancelled' | 'kyc_rejected' | 'refunding' | 'refunded' | 'delayed';
