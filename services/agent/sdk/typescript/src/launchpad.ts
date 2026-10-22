/**
 * Calldata builders, wallet send helpers and event decoders for the Launchpad
 * precompile. Every selector, argument type and event comes from
 * precompiles/launchpad/abi.json through src/generated/precompile-writes.ts.
 */

import type { Eip1193Requester } from "./account-derivation.js";
import {
  PrecompileAbiError,
  decodeEventFrom,
  encodeAbiCall,
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

/**
 * What the 0x1017 Router pulls from the caller by transferFrom: USDL for a
 * buy and for the creation fee of createMarket, the launch token for a sell.
 */
export type LaunchpadSpend =
  | Readonly<{ kind: "buy"; order: LaunchpadSwapOrder }>
  | Readonly<{ kind: "sell"; order: LaunchpadSwapOrder }>
  | Readonly<{ kind: "create" }>;

/** `approve(0x1017, amount)` on `token`: the exact allowance the Router spends, never an unlimited one. */
export function launchpadApproveCall(token: string, amount: bigint): PrecompileCall {
  return { to: token, data: encodeAbiCall("approve", ["address", "uint256"], [LAUNCHPAD_PRECOMPILE, amount]), value: 0n };
}

async function viewWord(rpc: Eip1193Requester, to: string, data: string): Promise<bigint> {
  const answer = await rpc.request({ method: "eth_call", params: [{ to, data }, "latest"] });
  if (typeof answer !== "string" || !/^0x[0-9a-fA-F]{64}/u.test(answer)) {
    throw new PrecompileAbiError("malformed_wallet_answer", `eth_call ${data.slice(0, 10)} at ${to}`);
  }
  return BigInt(answer.slice(0, 66));
}

const wordAddress = (word: bigint): string => `0x${word.toString(16).padStart(40, "0")}`;

/**
 * Reads the token and amount the Router will pull for `spend` and the
 * caller's allowance to 0x1017, and returns the approve call for the exact
 * amount when the allowance is short, or undefined when it already covers it.
 */
export async function launchpadApproval(rpc: Eip1193Requester, owner: string, spend: LaunchpadSpend): Promise<PrecompileCall | undefined> {
  const token = wordAddress(
    spend.kind === "sell"
      ? await viewWord(rpc, spend.order.pool, encodeAbiCall("tokenAddress", [], []))
      : await viewWord(rpc, LAUNCHPAD_PRECOMPILE, encodeAbiCall("usdlAddress", [], [])),
  );
  const amount = spend.kind === "create" ? await viewWord(rpc, LAUNCHPAD_PRECOMPILE, encodeAbiCall("creationFee", [], [])) : spend.order.amountIn;
  if (amount === 0n) {
    return undefined;
  }
  const allowance = await viewWord(rpc, token, encodeAbiCall("allowance", ["address", "address"], [owner, LAUNCHPAD_PRECOMPILE]));
  return allowance >= amount ? undefined : launchpadApproveCall(token, amount);
}

/** Waits for the approve transaction `hash` to be mined and throws unless it succeeded. */
export async function waitLaunchpadApproval(rpc: Eip1193Requester, hash: string, attempts = 60, intervalMs = 1_000): Promise<void> {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    const receipt = await rpc.request({ method: "eth_getTransactionReceipt", params: [hash] });
    if (typeof receipt === "object" && receipt !== null) {
      if ((receipt as { status?: unknown }).status === "0x1") {
        return;
      }
      throw new PrecompileAbiError("malformed_wallet_answer", `approve ${hash} reverted`);
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }
  throw new PrecompileAbiError("malformed_wallet_answer", `approve ${hash} not mined`);
}

/**
 * Sends `write` after granting 0x1017 the exact allowance `spend` needs:
 * the approve is sent and mined first when the allowance is short, and
 * `onApprove` is told before it is sent.
 */
export async function sendLaunchpadWithApproval(
  wallet: Eip1193Requester,
  from: string,
  spend: LaunchpadSpend,
  write: PrecompileCall,
  onApprove?: (approve: PrecompileCall) => void,
): Promise<string> {
  const approve = await launchpadApproval(wallet, from, spend);
  if (approve !== undefined) {
    onApprove?.(approve);
    await waitLaunchpadApproval(wallet, await sendPrecompileCall(wallet, from, approve));
  }
  return sendPrecompileCall(wallet, from, write);
}

export const sendLaunchpadBuy = (wallet: Eip1193Requester, from: string, order: LaunchpadSwapOrder): Promise<string> =>
  sendLaunchpadWithApproval(wallet, from, { kind: "buy", order }, launchpadBuyCall(order));

export const sendLaunchpadSell = (wallet: Eip1193Requester, from: string, order: LaunchpadSwapOrder): Promise<string> =>
  sendLaunchpadWithApproval(wallet, from, { kind: "sell", order }, launchpadSellCall(order));

export const sendLaunchpadCreateMarket = (
  wallet: Eip1193Requester,
  from: string,
  name: string,
  symbol: string,
  feeStrategy: number,
  optical: string = NO_OPTICAL,
): Promise<string> => sendLaunchpadWithApproval(wallet, from, { kind: "create" }, launchpadCreateMarketCall(name, symbol, feeStrategy, optical));

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
