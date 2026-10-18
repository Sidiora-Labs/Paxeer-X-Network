import { abiFields, abiUint, abiBoolean, abiHex, address, decodeDeployedOrder } from '../../widgets/meta-ag/domain';
import { CORE_DEPLOYMENT, OPTIONAL_CONTRACT_CAPABILITIES, assertOptionalContractCapability } from '../network/deployment';
/** Optional contracts have no admitted current deployment. Legacy address exports
 * remain inert for compatibility; reads and writes are blocked until serial
 * integration supplies reviewed bindings. Core USDL identity is manifest-driven. */
import {
  useReadContract,
  useWriteContract,
} from 'wagmi';

import MetaAGRouterAbi from '../network/abis/MetaAGRouter.json';
import MetaAGQuoterAbi from '../network/abis/MetaAGQuoter.json';
import PECOROrdersAbi from '../network/abis/PECOROrders.json';

// ── Inert optional bindings (no admitted manifest) ──────────

export const META_AG_ROUTER_ADDRESS =
  '0x0000000000000000000000000000000000000000' as const;
export const META_AG_QUOTER_ADDRESS =
  '0x0000000000000000000000000000000000000000' as const;
export const PECOR_ADDRESS =
  '0x0000000000000000000000000000000000000000' as const;
export const PECOR_ORDERS_ADDRESS =
  '0x0000000000000000000000000000000000000000' as const;
export const PECOR_VAULT_ADDRESS =
  '0x0000000000000000000000000000000000000000' as const;
export const ORACLE_HUB_ADDRESS =
  '0x0000000000000000000000000000000000000000' as const;
export const PRICE_ORACLE_ADDRESS =
  '0x0000000000000000000000000000000000000000' as const;
export const TRANSACTION_TRACKER_ADDRESS =
  '0x0000000000000000000000000000000000000000' as const;

// USDL is the canonical stablecoin for PECOROrders pairs.
export const META_AG_USDL_ADDRESS = CORE_DEPLOYMENT.usdl;
export const META_AG_WPAX_ADDRESS =
  '0x0000000000000000000000000000000000000000' as const;

// PECOROrders uses 18-dec USD prices via OracleHub.
export const META_AG_PRICE_DECIMALS = 18;

// Adapter identity is discovered from admitted chain records, never guessed.
// Deprecated inert sentinel: adapter IDs must come from admitted registry records.
export const ADAPTER_ID_VAULT = ('0x' + '0'.repeat(64)) as `0x${string}`;


// ── PECOROrders enums (mirror IPECOROrders.sol) ─────────────────

export enum OrderType {
  LimitBuy = 0,
  LimitSell = 1,
  StopLoss = 2,
  StopLimitBuy = 3,
  StopLimitSell = 4,
}

export enum OrderStatus {
  Active = 0,
  Filled = 1,
  Cancelled = 2,
  Expired = 3,
}

// ── Quote / order tuple types (mirror Solidity structs) ─────────

export interface MetaAGQuoteResult {
  amountIn: bigint;
  amountOut: bigint;
  grossAmountOut: bigint;
  executionPrice: bigint;
  spotPriceIn: bigint;
  spotPriceOut: bigint;
  feeAmount: bigint;
  feeBps: bigint;
  sufficientLiquidity: boolean;
  availableLiquidity: bigint;
  priceTimestampIn: bigint;
  priceTimestampOut: bigint;
  priceStaleIn: boolean;
  priceStaleOut: boolean;
}

export interface MetaAGBestQuote {
  amountOut: bigint;
  priceImpactBps: bigint;
  feeBps: bigint;
  feeAmount: bigint;
  adapterId: `0x${string}`;
  adapter: `0x${string}`;
  adapterData: `0x${string}`;
  found: boolean;
}

export interface MetaAGAdapterEntry {
  adapterId: `0x${string}`;
  adapter: `0x${string}`;
  active: boolean;
  name: string;
}

export interface PecorLimitOrder {
  id: bigint;
  user: `0x${string}`;
  stablecoin: `0x${string}`;
  token: `0x${string}`;
  amount: bigint;
  targetPrice: bigint;
  orderType: OrderType;
  status: OrderStatus;
  createdAt: bigint;
  expiresAt: bigint;
}

export interface PecorStopLimitOrder {
  id: bigint;
  user: `0x${string}`;
  stablecoin: `0x${string}`;
  token: `0x${string}`;
  amount: bigint;
  stopPrice: bigint;
  limitPrice: bigint;
  orderType: OrderType;
  status: OrderStatus;
  createdAt: bigint;
  expiresAt: bigint;
}

// ── MetaAGQuoter reads ──────────────────────────────────────────

export function useReadMetaAGQuoterQuoteExactIn(
  args: { tokenIn: `0x${string}`; tokenOut: `0x${string}`; amountIn: bigint },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: META_AG_QUOTER_ADDRESS,
    abi: MetaAGQuoterAbi as any,
    functionName: 'quoteExactIn',
    args: [args.tokenIn, args.tokenOut, args.amountIn],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.metaAg },
  });
}

export function useReadMetaAGQuoterGetTokenPrice(
  args: { token: `0x${string}` },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: META_AG_QUOTER_ADDRESS,
    abi: MetaAGQuoterAbi as any,
    functionName: 'getTokenPrice',
    args: [args.token],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.metaAg },
  });
}

export function useReadMetaAGQuoterGetLiquidityInfo(
  args: { token: `0x${string}` },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: META_AG_QUOTER_ADDRESS,
    abi: MetaAGQuoterAbi as any,
    functionName: 'getLiquidityInfo',
    args: [args.token],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.metaAg },
  });
}

// ── MetaAGRouter reads ──────────────────────────────────────────

export function useReadMetaAGRouterGetBestQuote(
  args: { tokenIn: `0x${string}`; tokenOut: `0x${string}`; amountIn: bigint },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: META_AG_ROUTER_ADDRESS,
    abi: MetaAGRouterAbi as any,
    functionName: 'getBestQuote',
    args: [args.tokenIn, args.tokenOut, args.amountIn],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.metaAg },
  });
}

export function useReadMetaAGRouterGetAllQuotes(
  args: { tokenIn: `0x${string}`; tokenOut: `0x${string}`; amountIn: bigint },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: META_AG_ROUTER_ADDRESS,
    abi: MetaAGRouterAbi as any,
    functionName: 'getAllQuotes',
    args: [args.tokenIn, args.tokenOut, args.amountIn],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.metaAg },
  });
}

export function useReadMetaAGRouterGetAdapters() {
  return useReadContract({
    chainId: 125,
    address: META_AG_ROUTER_ADDRESS,
    abi: MetaAGRouterAbi as any,
    functionName: 'getAdapters',
    query: { enabled: OPTIONAL_CONTRACT_CAPABILITIES.metaAg },
  });
}

export function useReadMetaAGRouterIsAdapterActive(
  args: { adapterId: `0x${string}` },
) {
  return useReadContract({
    chainId: 125,
    address: META_AG_ROUTER_ADDRESS,
    abi: MetaAGRouterAbi as any,
    functionName: 'isAdapterActive',
    query: { enabled: OPTIONAL_CONTRACT_CAPABILITIES.metaAg },
    args: [args.adapterId],
  });
}

// ── MetaAGRouter writes ─────────────────────────────────────────

export interface SwapBestRouteArgs {
  tokenIn: `0x${string}`;
  tokenOut: `0x${string}`;
  amountIn: bigint;
  amountOutMin: bigint;
  deadline: bigint;
}

export function useWriteMetaAGRouterSwapBestRoute() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('metaAg') } });
  const write = (args: SwapBestRouteArgs) =>
    (assertOptionalContractCapability('metaAg'), result.writeContract({
      chainId: 125,
      address: META_AG_ROUTER_ADDRESS,
      abi: MetaAGRouterAbi as any,
      functionName: 'swapBestRoute',
      args: [args.tokenIn, args.tokenOut, args.amountIn, args.amountOutMin, args.deadline],
    }));
  return { ...result, write };
}

export interface SwapViaAdapterArgs {
  adapterId: `0x${string}`;
  tokenIn: `0x${string}`;
  tokenOut: `0x${string}`;
  amountIn: bigint;
  amountOutMin: bigint;
  deadline: bigint;
}

export function useWriteMetaAGRouterSwapViaAdapter() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('metaAg') } });
  const write = (args: SwapViaAdapterArgs) =>
    (assertOptionalContractCapability('metaAg'), result.writeContract({
      chainId: 125,
      address: META_AG_ROUTER_ADDRESS,
      abi: MetaAGRouterAbi as any,
      functionName: 'swapViaAdapter',
      args: [
        args.adapterId,
        args.tokenIn,
        args.tokenOut,
        args.amountIn,
        args.amountOutMin,
        args.deadline,
      ],
    }));
  return { ...result, write };
}

export interface MetaAGHopParams {
  tokenIn: `0x${string}`;
  tokenOut: `0x${string}`;
  adapterId: `0x${string}`;
  minAmountOut: bigint;
}

export function useWriteMetaAGRouterSwapMultiHop() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('metaAg') } });
  const write = (args: {
    hops: MetaAGHopParams[];
    amountIn: bigint;
    amountOutMin: bigint;
    deadline: bigint;
  }) =>
    (assertOptionalContractCapability('metaAg'), result.writeContract({
      chainId: 125,
      address: META_AG_ROUTER_ADDRESS,
      abi: MetaAGRouterAbi as any,
      functionName: 'swapMultiHop',
      args: [args.hops, args.amountIn, args.amountOutMin, args.deadline],
    }));
  return { ...result, write };
}

// ── PECOROrders reads ───────────────────────────────────────────

export function useReadPecorOrdersGetUserLimitOrders(
  args: { user: `0x${string}` },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: PECOR_ORDERS_ADDRESS,
    abi: PECOROrdersAbi as any,
    functionName: 'getUserLimitOrders',
    args: [args.user],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.pecorOrders },
  });
}

export function useReadPecorOrdersGetUserStopLimitOrders(
  args: { user: `0x${string}` },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: PECOR_ORDERS_ADDRESS,
    abi: PECOROrdersAbi as any,
    functionName: 'getUserStopLimitOrders',
    args: [args.user],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.pecorOrders },
  });
}

export function useReadPecorOrdersGetLimitOrder(
  args: { orderId: bigint },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: PECOR_ORDERS_ADDRESS,
    abi: PECOROrdersAbi as any,
    functionName: 'getLimitOrder',
    args: [args.orderId],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.pecorOrders },
  });
}

export function useReadPecorOrdersGetStopLimitOrder(
  args: { orderId: bigint },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: PECOR_ORDERS_ADDRESS,
    abi: PECOROrdersAbi as any,
    functionName: 'getStopLimitOrder',
    args: [args.orderId],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.pecorOrders },
  });
}

export function useReadPecorOrdersCanExecuteLimitOrder(
  args: { orderId: bigint },
  enabled = true,
) {
  return useReadContract({
    chainId: 125,
    address: PECOR_ORDERS_ADDRESS,
    abi: PECOROrdersAbi as any,
    functionName: 'canExecuteLimitOrder',
    args: [args.orderId],
    query: { enabled: enabled && OPTIONAL_CONTRACT_CAPABILITIES.pecorOrders },
  });
}

// ── PECOROrders writes ──────────────────────────────────────────

export interface PlaceLimitBuyArgs {
  stablecoin: `0x${string}`;
  token: `0x${string}`;
  stablecoinAmount: bigint;
  targetPrice: bigint; // 18-dec USD
  expiresAt: bigint; // unix seconds
}

export function useWritePecorOrdersPlaceLimitBuy() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('pecorOrders') } });
  const write = (args: PlaceLimitBuyArgs) =>
    (assertOptionalContractCapability('pecorOrders'), result.writeContract({
      chainId: 125,
      address: PECOR_ORDERS_ADDRESS,
      abi: PECOROrdersAbi as any,
      functionName: 'placeLimitBuy',
      args: [
        args.stablecoin,
        args.token,
        args.stablecoinAmount,
        args.targetPrice,
        args.expiresAt,
      ],
    }));
  return { ...result, write };
}

export interface PlaceLimitSellArgs {
  token: `0x${string}`;
  stablecoin: `0x${string}`;
  tokenAmount: bigint;
  targetPrice: bigint; // 18-dec USD
  expiresAt: bigint;
}

export function useWritePecorOrdersPlaceLimitSell() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('pecorOrders') } });
  const write = (args: PlaceLimitSellArgs) =>
    (assertOptionalContractCapability('pecorOrders'), result.writeContract({
      chainId: 125,
      address: PECOR_ORDERS_ADDRESS,
      abi: PECOROrdersAbi as any,
      functionName: 'placeLimitSell',
      args: [
        args.token,
        args.stablecoin,
        args.tokenAmount,
        args.targetPrice,
        args.expiresAt,
      ],
    }));
  return { ...result, write };
}

export interface PlaceStopLossArgs {
  token: `0x${string}`;
  stablecoin: `0x${string}`;
  tokenAmount: bigint;
  triggerPrice: bigint; // 18-dec USD
  expiresAt: bigint;
}

export function useWritePecorOrdersPlaceStopLoss() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('pecorOrders') } });
  const write = (args: PlaceStopLossArgs) =>
    (assertOptionalContractCapability('pecorOrders'), result.writeContract({
      chainId: 125,
      address: PECOR_ORDERS_ADDRESS,
      abi: PECOROrdersAbi as any,
      functionName: 'placeStopLoss',
      args: [
        args.token,
        args.stablecoin,
        args.tokenAmount,
        args.triggerPrice,
        args.expiresAt,
      ],
    }));
  return { ...result, write };
}

export interface PlaceStopLimitBuyArgs {
  stablecoin: `0x${string}`;
  token: `0x${string}`;
  stablecoinAmount: bigint;
  stopPrice: bigint;
  limitPrice: bigint;
  expiresAt: bigint;
}

export function useWritePecorOrdersPlaceStopLimitBuy() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('pecorOrders') } });
  const write = (args: PlaceStopLimitBuyArgs) =>
    (assertOptionalContractCapability('pecorOrders'), result.writeContract({
      chainId: 125,
      address: PECOR_ORDERS_ADDRESS,
      abi: PECOROrdersAbi as any,
      functionName: 'placeStopLimitBuy',
      args: [
        args.stablecoin,
        args.token,
        args.stablecoinAmount,
        args.stopPrice,
        args.limitPrice,
        args.expiresAt,
      ],
    }));
  return { ...result, write };
}

export interface PlaceStopLimitSellArgs {
  token: `0x${string}`;
  stablecoin: `0x${string}`;
  tokenAmount: bigint;
  stopPrice: bigint;
  limitPrice: bigint;
  expiresAt: bigint;
}

export function useWritePecorOrdersPlaceStopLimitSell() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('pecorOrders') } });
  const write = (args: PlaceStopLimitSellArgs) =>
    (assertOptionalContractCapability('pecorOrders'), result.writeContract({
      chainId: 125,
      address: PECOR_ORDERS_ADDRESS,
      abi: PECOROrdersAbi as any,
      functionName: 'placeStopLimitSell',
      args: [
        args.token,
        args.stablecoin,
        args.tokenAmount,
        args.stopPrice,
        args.limitPrice,
        args.expiresAt,
      ],
    }));
  return { ...result, write };
}

export function useWritePecorOrdersCancelLimitOrder() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('pecorOrders') } });
  const write = (args: { orderId: bigint }) =>
    (assertOptionalContractCapability('pecorOrders'), result.writeContract({
      chainId: 125,
      address: PECOR_ORDERS_ADDRESS,
      abi: PECOROrdersAbi as any,
      functionName: 'cancelLimitOrder',
      args: [args.orderId],
    }));
  return { ...result, write };
}

export function useWritePecorOrdersCancelStopLimitOrder() {
  const result = useWriteContract({ mutation: { onMutate: () => assertOptionalContractCapability('pecorOrders') } });
  const write = (args: { orderId: bigint }) =>
    (assertOptionalContractCapability('pecorOrders'), result.writeContract({
      chainId: 125,
      address: PECOR_ORDERS_ADDRESS,
      abi: PECOROrdersAbi as any,
      functionName: 'cancelStopLimitOrder',
      args: [args.orderId],
    }));
  return { ...result, write };
}

// ── Tuple normalisers ───────────────────────────────────────────
//
// wagmi returns struct outputs as either tuples (array form) or named
// objects depending on solc / viem version. These helpers normalise to
// a typed object shape regardless of which form arrives.

export function normaliseQuote(raw: unknown): MetaAGQuoteResult | null {
  try {
    const fields = abiFields(raw, ['amountIn','amountOut','grossAmountOut','executionPrice','spotPriceIn','spotPriceOut','feeAmount','feeBps','sufficientLiquidity','availableLiquidity','priceTimestampIn','priceTimestampOut','priceStaleIn','priceStaleOut']);
    return { amountIn: abiUint(fields[0]), amountOut: abiUint(fields[1]), grossAmountOut: abiUint(fields[2]), executionPrice: abiUint(fields[3]), spotPriceIn: abiUint(fields[4]), spotPriceOut: abiUint(fields[5]), feeAmount: abiUint(fields[6]), feeBps: abiUint(fields[7]), sufficientLiquidity: abiBoolean(fields[8]), availableLiquidity: abiUint(fields[9]), priceTimestampIn: abiUint(fields[10]), priceTimestampOut: abiUint(fields[11]), priceStaleIn: abiBoolean(fields[12]), priceStaleOut: abiBoolean(fields[13]) };
  } catch { return null; }
}
export function normaliseBestQuote(raw: unknown): MetaAGBestQuote | null {
  try {
    const fields = abiFields(raw, ['amountOut','priceImpactBps','feeBps','feeAmount','adapterId','adapter','adapterData','found']);
    const found = abiBoolean(fields[7]);
    const adapter = abiHex(fields[5], 20);
    if (found) address(adapter);
    return { amountOut: abiUint(fields[0], found), priceImpactBps: abiUint(fields[1]), feeBps: abiUint(fields[2]), feeAmount: abiUint(fields[3]), adapterId: abiHex(fields[4],32), adapter, adapterData: abiHex(fields[6]), found };
  } catch { return null; }
}
export function normaliseLimitOrder(raw: unknown): PecorLimitOrder | null {
  try {
    const decoded = decodeDeployedOrder(raw, 'limit');
    if (decoded.family !== 'limit' || decoded.status !== 0) return null;
    return decoded;
  } catch { return null; }
}
export function normaliseStopLimitOrder(raw: unknown): PecorStopLimitOrder | null {
  try {
    const decoded = decodeDeployedOrder(raw, 'stop');
    if (decoded.family !== 'stop' || decoded.status !== 0) return null;
    return decoded;
  } catch { return null; }
}
export function normaliseAdapters(raw: unknown): MetaAGAdapterEntry[] {
  if (!Array.isArray(raw) || raw.length > 64) return [];
  try {
    const ids = new Set<string>(), targets = new Set<string>();
    return raw.map(entry => {
      const [id, target, enabled, name] = abiFields(entry, ['adapterId','adapter','active','name']);
      const adapterId = abiHex(id,32), adapter = address(target as string), active = abiBoolean(enabled);
      if (typeof name !== 'string' || name.length > 256 || ids.has(adapterId.toLowerCase()) || targets.has(adapter)) throw new Error('Malformed or duplicate adapter registry');
      ids.add(adapterId.toLowerCase()); targets.add(adapter);
      return { adapterId, adapter, active, name };
    });
  } catch { return []; }
}

// ── Re-exports for typing ───────────────────────────────────────

export { MetaAGRouterAbi, MetaAGQuoterAbi, PECOROrdersAbi };

export { createAdvancedProvider, advancedOrdersAbi } from '../../widgets/meta-ag/advanced-provider';
export type { AdvancedProvider, AdvancedManifest, AdvancedBinding } from '../../widgets/meta-ag/advanced-provider';
