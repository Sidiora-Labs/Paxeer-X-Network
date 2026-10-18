import { OrderBookFacetABI as OrderBookFacetAbi } from "@paxeer-network/paxeer-backend/core/network/abi/OrderBookFacet";
import { DIAMOND_ADDRESS } from "@paxeer-network/paxeer-backend/core/perps/config";
import { readContract, writeContract, waitForTx } from "./useContract";

export function usePerpsOrders() {
  async function getOrder(orderId: bigint) {
    return readContract({
      address: DIAMOND_ADDRESS,
      abi: OrderBookFacetAbi,
      functionName: "getOrder",
      args: [orderId],
    });
  }

  async function getUserOrderIds(user: `0x${string}`): Promise<readonly bigint[]> {
    return readContract({
      address: DIAMOND_ADDRESS,
      abi: OrderBookFacetAbi,
      functionName: "getUserOrderIds",
      args: [user],
    }) as Promise<readonly bigint[]>;
  }

  async function placeLimitOrder(params: {
    marketId: bigint;
    isLong: boolean;
    triggerPrice: bigint;
    sizeUsd: bigint;
    leverage: bigint;
    collateralToken: `0x${string}`;
    collateralAmount: bigint;
  }) {
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: OrderBookFacetAbi,
      functionName: "placeLimitOrder",
      args: [
        params.marketId, params.isLong, params.triggerPrice,
        params.sizeUsd, params.leverage, params.collateralToken, params.collateralAmount,
      ],
    });
    return waitForTx(hash);
  }

  async function placeStopLimitOrder(params: {
    marketId: bigint;
    isLong: boolean;
    triggerPrice: bigint;
    limitPrice: bigint;
    sizeUsd: bigint;
    leverage: bigint;
    collateralToken: `0x${string}`;
    collateralAmount: bigint;
  }) {
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: OrderBookFacetAbi,
      functionName: "placeStopLimitOrder",
      args: [
        params.marketId, params.isLong, params.triggerPrice, params.limitPrice,
        params.sizeUsd, params.leverage, params.collateralToken, params.collateralAmount,
      ],
    });
    return waitForTx(hash);
  }

  async function cancelOrder(orderId: bigint) {
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: OrderBookFacetAbi,
      functionName: "cancelOrder",
      args: [orderId],
    });
    return waitForTx(hash);
  }

  async function placeTakeProfitOrder(positionId: bigint, triggerPrice: bigint) {
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: OrderBookFacetAbi,
      functionName: "placeTakeProfitOrder",
      args: [positionId, triggerPrice],
    });
    return waitForTx(hash);
  }

  async function placeStopLossOrder(positionId: bigint, triggerPrice: bigint) {
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: OrderBookFacetAbi,
      functionName: "placeStopLossOrder",
      args: [positionId, triggerPrice],
    });
    return waitForTx(hash);
  }

  return {
    getOrder, getUserOrderIds,
    placeLimitOrder, placeStopLimitOrder,
    placeTakeProfitOrder, placeStopLossOrder,
    cancelOrder,
  };
}
