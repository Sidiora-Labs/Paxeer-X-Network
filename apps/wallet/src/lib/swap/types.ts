import type { SwapToken } from './constants';

export type SwapProtocol = 'pecor' | 'sidiora' | 'sidiora-multihop' | 'pecor-v4-multihop';

export interface SwapQuote {
  protocol: SwapProtocol;
  protocolLabel: string;
  tokenIn: SwapToken;
  tokenOut: SwapToken;
  amountIn: string;           // raw wei
  amountOut: string;          // raw wei
  amountOutDisplay: string;   // human-readable
  fee: string;                // raw wei fee
  feeBps: number;             // fee in bps
  priceImpact: number;        // estimated price impact %
  sufficient: boolean;        // whether liquidity is sufficient
  poolAddress?: string;       // Sidiora pool address (for direct buy/sell execution)
  intermediateAmount?: string; // raw wei USDL intermediate (for pecor-v4-multihop)
}

export interface SwapParams {
  tokenIn: SwapToken;
  tokenOut: SwapToken;
  amountIn: string;        // raw wei
  slippageBps: number;     // slippage tolerance in bps
  recipient: string;       // wallet address
  deadline?: number;       // unix timestamp
}
