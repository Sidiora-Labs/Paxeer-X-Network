import { ethers } from 'ethers';

export const MIN_SWAP_SLIPPAGE_BPS = 1;
export const MAX_SWAP_SLIPPAGE_BPS = 500;
export const HIGH_PRICE_IMPACT_PCT = 15;

export interface TokenLike {
  decimals: number;
  balanceRaw?: string;
}

export function validateEvmAddress(address: string): string {
  const trimmed = address.trim();
  if (!ethers.isAddress(trimmed)) {
    throw new Error('Enter a valid recipient address.');
  }
  return ethers.getAddress(trimmed);
}

export function validateTokenAmount(amount: string, decimals: number): bigint {
  const trimmed = amount.trim();
  if (!trimmed) throw new Error('Enter an amount.');
  if (!/^(?:\d+|\d*\.\d+)$/.test(trimmed)) {
    throw new Error('Enter a valid amount.');
  }

  let parsed: bigint;
  try {
    parsed = ethers.parseUnits(trimmed, decimals);
  } catch {
    throw new Error(`Amount supports up to ${decimals} decimals.`);
  }
  if (parsed <= BigInt(0)) throw new Error('Amount must be greater than zero.');
  return parsed;
}

export function validateSpendableBalance(
  amountRaw: bigint,
  balanceRaw: string | bigint | null | undefined,
): void {
  if (balanceRaw === null || balanceRaw === undefined) {
    throw new Error('Balance is still loading. Try again in a moment.');
  }
  const balance = typeof balanceRaw === 'bigint' ? balanceRaw : BigInt(balanceRaw || '0');
  if (amountRaw > balance) {
    throw new Error('Insufficient balance for this amount.');
  }
}

export function validateSlippageBps(slippageBps: number): void {
  if (!Number.isInteger(slippageBps) || slippageBps < MIN_SWAP_SLIPPAGE_BPS) {
    throw new Error('Set a valid slippage tolerance.');
  }
  if (slippageBps > MAX_SWAP_SLIPPAGE_BPS) {
    throw new Error('Slippage cannot exceed 5%.');
  }
}

export function validateSwapQuote(quote: {
  amountIn: string;
  amountOut: string;
  sufficient: boolean;
  priceImpact: number;
}): void {
  if (!quote.sufficient) throw new Error('Insufficient liquidity for this swap.');
  if (BigInt(quote.amountIn || '0') <= BigInt(0)) throw new Error('Enter an amount.');
  if (BigInt(quote.amountOut || '0') <= BigInt(0)) {
    throw new Error('No output liquidity for this route.');
  }
  if (quote.priceImpact >= HIGH_PRICE_IMPACT_PCT) {
    throw new Error('Price impact is too high for this swap.');
  }
}
