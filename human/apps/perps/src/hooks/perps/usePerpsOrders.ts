import type { Address, Hex } from "viem";

import {
  buildCancelByCloid,
  buildCancelOrder,
  buildGetOrder,
  buildGetUserOrderIds,
  buildPlaceLimitOrder,
  buildPlaceStopLimitOrder,
  buildSetPositionTpSl,
  type LimitOrderInput,
  type SetPositionTpSlInput,
  type StopLimitOrderInput,
} from "@/core/perps/calls";
import { planOrder, type PlaceOrderCall } from "@/core/perps/positionWrites";
import { readContract, writeContract, waitForTx, type WaitForTxResult } from "./useContract";
import { perpsAllowanceSource, sendPlan } from "./usePosition";

export function usePerpsOrders() {
  async function getOrder(orderId: bigint) {
    return readContract(buildGetOrder(orderId));
  }

  async function getUserOrderIds(account: Address): Promise<readonly bigint[]> {
    return (await readContract(buildGetUserOrderIds(account))) as readonly bigint[];
  }

  /** Send the placeOrder calls of an order plan behind the exact USDL approval of their collateral. */
  async function submitOrderPlan(calls: readonly PlaceOrderCall[]) {
    return sendPlan(await planOrder(calls, perpsAllowanceSource()));
  }

  async function placeLimitOrder(input: LimitOrderInput) {
    return submitOrderPlan([buildPlaceLimitOrder(input)]);
  }

  async function placeStopLimitOrder(input: StopLimitOrderInput) {
    return submitOrderPlan([buildPlaceStopLimitOrder(input)]);
  }

  /** One setPositionTpSl at 18 decimals; 0 clears a side. */
  async function setPositionTpSl(input: SetPositionTpSlInput) {
    return waitForTx(await writeContract(buildSetPositionTpSl(input)));
  }

  async function cancelOrder(orderId: bigint) {
    return waitForTx(await writeContract(buildCancelOrder(orderId)));
  }

  async function cancelByCloid(marketId: bigint, cloid: Hex) {
    return waitForTx(await writeContract(buildCancelByCloid(marketId, cloid)));
  }

  /** Cancel a scaled ladder rung by rung; returns the receipt of each rung in order. */
  async function cancelLadder(marketId: bigint, cloids: readonly Hex[]): Promise<WaitForTxResult[]> {
    const receipts: WaitForTxResult[] = [];
    for (const cloid of cloids) receipts.push(await cancelByCloid(marketId, cloid));
    return receipts;
  }

  return {
    getOrder,
    getUserOrderIds,
    submitOrderPlan,
    placeLimitOrder,
    placeStopLimitOrder,
    setPositionTpSl,
    cancelOrder,
    cancelByCloid,
    cancelLadder,
  };
}
