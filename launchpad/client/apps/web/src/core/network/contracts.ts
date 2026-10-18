import { CORE_DEPLOYMENT, assertActiveApprovalSpender } from './deployment';
import {
  useReadContract,
  useWriteContract,
  useWatchContractEvent,
} from 'wagmi';

import RouterAbi from './abis/Router.json';
import QuoterAbi from './abis/Quoter.json';
import FeesRouterAbi from './abis/FeesRouter.json';
import OpticalRegistryAbi from './abis/OpticalRegistry.json';

export const ROUTER_ADDRESS = CORE_DEPLOYMENT.router;
export const QUOTER_ADDRESS = CORE_DEPLOYMENT.quoter;
export const FEES_ROUTER_ADDRESS = CORE_DEPLOYMENT.feesRouter;
export const OPTICAL_REGISTRY_ADDRESS = CORE_DEPLOYMENT.opticalRegistry;
export const USDL_ADDRESS = CORE_DEPLOYMENT.usdl;
export const PROTOCOL_CONFIG_ADDRESS = CORE_DEPLOYMENT.protocolConfig;

const ERC20_ABI = [
  { type: 'function', name: 'approve', stateMutability: 'nonpayable', inputs: [{ name: 'spender', type: 'address' }, { name: 'amount', type: 'uint256' }], outputs: [{ name: '', type: 'bool' }] },
  { type: 'function', name: 'allowance', stateMutability: 'view', inputs: [{ name: 'owner', type: 'address' }, { name: 'spender', type: 'address' }], outputs: [{ name: '', type: 'uint256' }] },
  { type: 'function', name: 'balanceOf', stateMutability: 'view', inputs: [{ name: 'account', type: 'address' }], outputs: [{ name: '', type: 'uint256' }] },
  { type: 'function', name: 'decimals', stateMutability: 'view', inputs: [], outputs: [{ name: '', type: 'uint8' }] },
] as const;

const PROTOCOL_CONFIG_ABI = [
  { type: 'function', name: 'creationFee', stateMutability: 'view', inputs: [], outputs: [{ name: '', type: 'uint256' }] },
] as const;

// ── ERC20 hooks ─────────────────────────────────────────────

export function useWriteErc20Approve() {
  const result = useWriteContract({ mutation: { onMutate: (variables) => {
    const spender = Array.isArray(variables.args) ? variables.args[0] : undefined;
    if (typeof spender !== 'string') throw new Error('Invalid approval spender');
    assertActiveApprovalSpender(spender);
  } } });
  const write = (args: { token: `0x${string}`; spender: `0x${string}`; amount: bigint }) =>
    result.writeContract({
      chainId: 125,
      address: args.token,
      abi: ERC20_ABI,
      functionName: 'approve',
      args: [args.spender, args.amount],
    });
  return { ...result, write };
}

export function useReadErc20Allowance(args: { token: `0x${string}`; owner: `0x${string}`; spender: `0x${string}` }) {
  return useReadContract({
    address: args.token,
    abi: ERC20_ABI,
    functionName: 'allowance',
    args: [args.owner, args.spender],
  });
}

export function useReadErc20Balance(args: { token: `0x${string}`; account: `0x${string}` }) {
  return useReadContract({
    address: args.token,
    abi: ERC20_ABI,
    functionName: 'balanceOf',
    args: [args.account],
  });
}

export function useReadErc20Decimals(args: { token: `0x${string}` }) {
  return useReadContract({
    address: args.token,
    abi: ERC20_ABI,
    functionName: 'decimals',
  });
}

export function useReadCreationFee() {
  return useReadContract({
    address: PROTOCOL_CONFIG_ADDRESS,
    abi: PROTOCOL_CONFIG_ABI,
    functionName: 'creationFee',
  });
}

// ── Router hooks ────────────────────────────────────────────

export function useWriteRouterCreateMarket() {
  const result = useWriteContract();
  const write = (args: {
    name: string;
    symbol: string;
    feeStrategy: number;
    optical: `0x${string}`;
  }) =>
    result.writeContract({
      chainId: 125,
      address: ROUTER_ADDRESS,
      abi: RouterAbi as any,
      functionName: 'createMarket',
      args: [args.name, args.symbol, args.feeStrategy, args.optical],
    });
  return { ...result, write };
}

export function useWriteRouterBuy() {
  const result = useWriteContract();
  const write = (args: {
    pool: `0x${string}`;
    usdlAmountIn: bigint;
    minTokensOut: bigint;
    deadline: bigint;
  }) =>
    result.writeContract({
      chainId: 125,
      address: ROUTER_ADDRESS,
      abi: RouterAbi as any,
      functionName: 'buy',
      args: [args.pool, args.usdlAmountIn, args.minTokensOut, args.deadline],
    });
  return { ...result, write };
}

export function useWriteRouterSell() {
  const result = useWriteContract();
  const write = (args: {
    pool: `0x${string}`;
    tokenAmountIn: bigint;
    minUsdlOut: bigint;
    deadline: bigint;
  }) =>
    result.writeContract({
      chainId: 125,
      address: ROUTER_ADDRESS,
      abi: RouterAbi as any,
      functionName: 'sell',
      args: [args.pool, args.tokenAmountIn, args.minUsdlOut, args.deadline],
    });
  return { ...result, write };
}

export function useWriteRouterMulticall() {
  const result = useWriteContract();
  const write = (args: { data: readonly `0x${string}`[] }) =>
    result.writeContract({
      chainId: 125,
      address: ROUTER_ADDRESS,
      abi: RouterAbi as any,
      functionName: 'multicall',
      args: [args.data],
    });
  return { ...result, write };
}

export function useWriteRouterSwapTokenForToken() {
  const result = useWriteContract();
  const write = (args: {
    tokenIn: `0x${string}`;
    tokenOut: `0x${string}`;
    amountIn: bigint;
    minAmountOut: bigint;
    deadline: bigint;
  }) =>
    result.writeContract({
      chainId: 125,
      address: ROUTER_ADDRESS,
      abi: RouterAbi as any,
      functionName: 'swapTokenForToken',
      args: [args.tokenIn, args.tokenOut, args.amountIn, args.minAmountOut, args.deadline],
    });
  return { ...result, write };
}

export function useWatchRouterMarketCreated(config: {
  onLogs: (logs: Array<{
    args: { token: `0x${string}`; pool: `0x${string}`; creator: `0x${string}`; nftId: bigint };
    blockNumber: bigint;
    transactionHash: `0x${string}`;
  }>) => void;
  enabled?: boolean;
}) {
  return useWatchContractEvent({
    address: ROUTER_ADDRESS,
    abi: RouterAbi as any,
    eventName: 'MarketCreated',
    onLogs: config.onLogs as any,
    enabled: config.enabled,
  });
}

// ── Quoter hooks ────────────────────────────────────────────

export function useReadQuoterGetPoolsByCreator(
  args: { creator: `0x${string}` },
) {
  return useReadContract({
    address: QUOTER_ADDRESS,
    abi: QuoterAbi as any,
    functionName: 'getPoolsByCreator',
    args: [args.creator],
  });
}

export function useReadQuoterQuoteExactInput(
  args: { pool: `0x${string}`; amountIn: bigint; isBuy: boolean },
) {
  return useReadContract({
    address: QUOTER_ADDRESS,
    abi: QuoterAbi as any,
    functionName: 'quoteExactInput',
    args: [args.pool, args.amountIn, args.isBuy],
  });
}

export function useReadQuoterGetPoolStats(
  args: { pool: `0x${string}` },
) {
  return useReadContract({
    address: QUOTER_ADDRESS,
    abi: QuoterAbi as any,
    functionName: 'getPoolStats',
    args: [args.pool],
  });
}

export function useReadQuoterGetPoolPrice(
  args: { pool: `0x${string}` },
) {
  return useReadContract({
    address: QUOTER_ADDRESS,
    abi: QuoterAbi as any,
    functionName: 'getPoolPrice',
    args: [args.pool],
  });
}

export function useReadQuoterGetMarketCap(
  args: { pool: `0x${string}` },
) {
  return useReadContract({
    address: QUOTER_ADDRESS,
    abi: QuoterAbi as any,
    functionName: 'getMarketCap',
    args: [args.pool],
  });
}

export function useReadQuoterQuoteMultihop(
  args: { tokenIn: `0x${string}`; tokenOut: `0x${string}`; amountIn: bigint },
  enabled = true,
) {
  return useReadContract({
    address: QUOTER_ADDRESS,
    abi: QuoterAbi as any,
    functionName: 'quoteMultihop',
    args: [args.tokenIn, args.tokenOut, args.amountIn],
    query: { enabled },
  });
}

export function useReadQuoterGetAllPools(
  args: { offset: bigint; limit: bigint },
) {
  return useReadContract({
    address: QUOTER_ADDRESS,
    abi: QuoterAbi as any,
    functionName: 'getAllPools',
    args: [args.offset, args.limit],
  });
}

// ── NFT ERC721Enumerable ABI (subset) ──────────────────────

const ERC721_ENUMERABLE_ABI = [
  { type: 'function', name: 'balanceOf', stateMutability: 'view', inputs: [{ name: 'owner', type: 'address' }], outputs: [{ name: '', type: 'uint256' }] },
  { type: 'function', name: 'tokenOfOwnerByIndex', stateMutability: 'view', inputs: [{ name: 'owner', type: 'address' }, { name: 'index', type: 'uint256' }], outputs: [{ name: '', type: 'uint256' }] },
  { type: 'function', name: 'ownerOf', stateMutability: 'view', inputs: [{ name: 'tokenId', type: 'uint256' }], outputs: [{ name: '', type: 'address' }] },
] as const;

// ── FeesRouter hooks ────────────────────────────────────────

export function useReadFeesRouterNftContract() {
  return useReadContract({
    address: FEES_ROUTER_ADDRESS,
    abi: FeesRouterAbi as any,
    functionName: 'nftContract',
  });
}

export function useReadNftBalanceOf(args: { nft: `0x${string}`; owner: `0x${string}` }) {
  return useReadContract({
    address: args.nft,
    abi: ERC721_ENUMERABLE_ABI,
    functionName: 'balanceOf',
    args: [args.owner],
    query: { enabled: !!args.nft && args.nft !== '0x0000000000000000000000000000000000000000' },
  });
}

export function useReadNftTokenOfOwnerByIndex(args: { nft: `0x${string}`; owner: `0x${string}`; index: bigint }) {
  return useReadContract({
    address: args.nft,
    abi: ERC721_ENUMERABLE_ABI,
    functionName: 'tokenOfOwnerByIndex',
    args: [args.owner, args.index],
    query: { enabled: !!args.nft && args.nft !== '0x0000000000000000000000000000000000000000' },
  });
}

export function useSimulateClaimFees(args: { nftId: bigint; enabled?: boolean }) {
  return useReadContract({
    address: FEES_ROUTER_ADDRESS,
    abi: FeesRouterAbi as any,
    functionName: 'claimFees',
    args: [args.nftId],
    query: { enabled: args.enabled !== false },
  });
}

export function useWriteFeesRouterClaimFees() {
  const result = useWriteContract();
  const write = (args: { nftId: bigint }) =>
    result.writeContract({
      chainId: 125,
      address: FEES_ROUTER_ADDRESS,
      abi: FeesRouterAbi as any,
      functionName: 'claimFees',
      args: [args.nftId],
    });
  return { ...result, write };
}

export function useWriteFeesRouterClaimAirdrop() {
  const result = useWriteContract();
  const write = (args: { nftId: bigint }) =>
    result.writeContract({
      chainId: 125,
      address: FEES_ROUTER_ADDRESS,
      abi: FeesRouterAbi as any,
      functionName: 'claimAirdrop',
      args: [args.nftId],
    });
  return { ...result, write };
}

export { ERC721_ENUMERABLE_ABI, FeesRouterAbi };

// ── OpticalRegistry hooks ───────────────────────────────────

export function useReadOpticalRegistryGetAllOpticals(
  args: { offset: bigint; limit: bigint },
) {
  return useReadContract({
    address: OPTICAL_REGISTRY_ADDRESS,
    abi: OpticalRegistryAbi as any,
    functionName: 'getAllOpticals',
    args: [args.offset, args.limit],
  });
}

export function useReadOpticalRegistryGetOpticalMetadata(
  args: { optical: `0x${string}` },
) {
  return useReadContract({
    address: OPTICAL_REGISTRY_ADDRESS,
    abi: OpticalRegistryAbi as any,
    functionName: 'getOpticalMetadata',
    args: [args.optical],
  });
}
