/**
 * Calldata builders, wallet send helpers and event decoders for the Launchpad
 * precompile. Every selector, argument type and event comes from
 * precompiles/launchpad/abi.json through src/generated/precompile-writes.ts.
 */

import type { Eip1193Requester } from "./account-derivation.js";
import {
  decodeEventFrom,
  precompileWriteCall,
  sendPrecompileCall,
  type AbiArgument,
  type DecodedPrecompileEvent,
  type PrecompileCall,
  type PrecompileLog,
} from "./exchange.js";
import { LAUNCHPAD_EVENTS, LAUNCHPAD_PRECOMPILE, LAUNCHPAD_WRITES } from "./generated/precompile-writes.js";

export { LAUNCHPAD_EVENTS, LAUNCHPAD_PRECOMPILE, LAUNCHPAD_WRITES } from "./generated/precompile-writes.js";

const NO_OPTICAL = "0x0000000000000000000000000000000000000000";

/** Decodes a Launchpad precompile log. */
export function decodeLaunchpadEvent(log: PrecompileLog): DecodedPrecompileEvent {
  return decodeEventFrom(LAUNCHPAD_EVENTS, log);
}

export type LaunchpadWrite = keyof typeof LAUNCHPAD_WRITES;

/** Builds a call of any launchpad write a direct call to the precompile resolves, by canonical signature. */
export function launchpadCall(method: LaunchpadWrite, args: readonly AbiArgument[], value = 0n): PrecompileCall {
  return precompileWriteCall(LAUNCHPAD_PRECOMPILE, method, LAUNCHPAD_WRITES[method], args, value);
}

/** A Router swap: `amountIn` is the USDL paid in for a buy and the tokens sold for a sell; the Router pays the caller. */
export interface LaunchpadSwapOrder {
  readonly pool: string;
  readonly amountIn: bigint;
  readonly minOut: bigint;
  readonly deadline: bigint;
}

/** `buy(address pool,uint256 usdlAmountIn,uint256 minTokensOut,uint256 deadline)`. */
export function launchpadBuyCall(order: LaunchpadSwapOrder): PrecompileCall {
  return launchpadCall("buy(address,uint256,uint256,uint256)", [order.pool, order.amountIn, order.minOut, order.deadline]);
}

/** `sell(address pool,uint256 tokenAmountIn,uint256 minUsdlOut,uint256 deadline)`. */
export function launchpadSellCall(order: LaunchpadSwapOrder): PrecompileCall {
  return launchpadCall("sell(address,uint256,uint256,uint256)", [order.pool, order.amountIn, order.minOut, order.deadline]);
}

/** `createMarket(string name,string symbol,uint8 feeStrategy,address optical)`; the zero optical means none. */
export function launchpadCreateMarketCall(name: string, symbol: string, feeStrategy: number, optical: string = NO_OPTICAL): PrecompileCall {
  return launchpadCall("createMarket(string,string,uint8,address)", [name, symbol, BigInt(feeStrategy), optical]);
}

/** `setFeeStrategy(uint256 nftId,uint8 newStrategy)` by the holder of the market's fee-rights NFT. */
export function launchpadSetFeeStrategyCall(nftId: bigint, feeStrategy: number): PrecompileCall {
  return launchpadCall("setFeeStrategy(uint256,uint8)", [nftId, BigInt(feeStrategy)]);
}

/** `claimFees(uint256 nftId)`; the fees go to the NFT holder. */
export function launchpadClaimFeesCall(nftId: bigint): PrecompileCall {
  return launchpadCall("claimFees(uint256)", [nftId]);
}

export type LaunchpadNftWrite = "claimAirdrop" | "executeAirdrop" | "executeBurn" | "executeLpRewards";

/** `claimAirdrop`, `executeAirdrop`, `executeBurn` or `executeLpRewards` on `(uint256 nftId)`. */
export function launchpadNftCall(write: LaunchpadNftWrite, nftId: bigint): PrecompileCall {
  return launchpadCall(`${write}(uint256)`, [nftId]);
}

export const sendLaunchpadBuy = (wallet: Eip1193Requester, from: string, order: LaunchpadSwapOrder): Promise<string> =>
  sendPrecompileCall(wallet, from, launchpadBuyCall(order));

export const sendLaunchpadSell = (wallet: Eip1193Requester, from: string, order: LaunchpadSwapOrder): Promise<string> =>
  sendPrecompileCall(wallet, from, launchpadSellCall(order));

export const sendLaunchpadCreateMarket = (
  wallet: Eip1193Requester,
  from: string,
  name: string,
  symbol: string,
  feeStrategy: number,
  optical: string = NO_OPTICAL,
): Promise<string> => sendPrecompileCall(wallet, from, launchpadCreateMarketCall(name, symbol, feeStrategy, optical));

export const sendLaunchpadSetFeeStrategy = (
  wallet: Eip1193Requester,
  from: string,
  nftId: bigint,
  feeStrategy: number,
): Promise<string> => sendPrecompileCall(wallet, from, launchpadSetFeeStrategyCall(nftId, feeStrategy));

export const sendLaunchpadClaimFees = (wallet: Eip1193Requester, from: string, nftId: bigint): Promise<string> =>
  sendPrecompileCall(wallet, from, launchpadClaimFeesCall(nftId));

export const sendLaunchpadNftWrite = (
  wallet: Eip1193Requester,
  from: string,
  write: LaunchpadNftWrite,
  nftId: bigint,
): Promise<string> => sendPrecompileCall(wallet, from, launchpadNftCall(write, nftId));
