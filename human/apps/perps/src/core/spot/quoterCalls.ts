import type { AbiStateMutability, Address, ContractFunctionArgs, ContractFunctionName } from "viem";

import { PRECOMPILES } from "@/core/network/precompiles";
import { pecorQuoterAbi } from "@/core/network/spot_abi/PECORQuoter";

type QuoterAbi = typeof pecorQuoterAbi;
type QuoterFunction = ContractFunctionName<QuoterAbi, "view">;

/** A read at the spot quoter precompile whose args are checked against pecorQuoterAbi. */
export interface QuoterCall<
  F extends QuoterFunction,
  A extends ContractFunctionArgs<QuoterAbi, AbiStateMutability, F> = ContractFunctionArgs<
    QuoterAbi,
    AbiStateMutability,
    F
  >,
> {
  address: Address;
  abi: QuoterAbi;
  functionName: F;
  args: A;
}

export type BatchQuoteRequests = ContractFunctionArgs<QuoterAbi, "view", "batchQuote">[0];

function call<F extends QuoterFunction, A extends ContractFunctionArgs<QuoterAbi, AbiStateMutability, F>>(
  functionName: F,
  args: A,
): QuoterCall<F, A> {
  return { address: PRECOMPILES.spot.PECORQuoter, abi: pecorQuoterAbi, functionName, args };
}

export function buildQuoteMarketBuy(stablecoin: Address, token: Address, stablecoinAmount: bigint) {
  return call("quoteMarketBuy", [stablecoin, token, stablecoinAmount] as const);
}

export function buildQuoteMarketSell(token: Address, stablecoin: Address, tokenAmount: bigint) {
  return call("quoteMarketSell", [token, stablecoin, tokenAmount] as const);
}

export function buildQuoteExactIn(tokenIn: Address, tokenOut: Address, amountIn: bigint) {
  return call("quoteExactIn", [tokenIn, tokenOut, amountIn] as const);
}

export function buildQuoteExactOut(tokenIn: Address, tokenOut: Address, amountOut: bigint) {
  return call("quoteExactOut", [tokenIn, tokenOut, amountOut] as const);
}

export function buildBatchQuote(requests: BatchQuoteRequests) {
  return call("batchQuote", [requests] as const);
}

export function buildGetTokenPrice(token: Address) {
  return call("getTokenPrice", [token] as const);
}

export function buildGetLiquidityInfo(token: Address) {
  return call("getLiquidityInfo", [token] as const);
}

export function buildGetAllLiquidityInfo() {
  return call("getAllLiquidityInfo", [] as const);
}

export function buildGetTWAP(token: Address, period: bigint) {
  return call("getTWAP", [token, period] as const);
}
