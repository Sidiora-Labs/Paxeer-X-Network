import { PositionFacetABI as PositionFacetAbi } from "@paxeer-network/paxeer-backend/core/network/abi/PositionFacet";
import { DIAMOND_ADDRESS } from "@paxeer-network/paxeer-backend/core/perps/config";
import { getConnectedAddress } from "@paxeer-network/paxeer-backend/core/viem/clients";
import { ensureAllowance } from "@paxeer-network/paxeer-backend/hooks/spot/useSpotApprovals";
import { readContract, writeContract, waitForTx } from "./useContract";

/**
 * The Diamond pulls collateral from the user's wallet via
 * `transferFrom`, so every collateral-spending write needs an ERC-20
 * approval first — same approve → transferFrom flow as the spot rail.
 * Without this, openPosition reverts on zero allowance.
 */
async function ensureDiamondAllowance(
  token: `0x${string}`,
  amount: bigint,
): Promise<void> {
  const owner = getConnectedAddress();
  if (!owner) {
    throw new Error("Wallet not connected — please connect your wallet");
  }
  await ensureAllowance({ token, owner, spender: DIAMOND_ADDRESS, amount });
}

export function usePosition() {
  async function getPosition(positionId: bigint) {
    return readContract({
      address: DIAMOND_ADDRESS,
      abi: PositionFacetAbi,
      functionName: "getPosition",
      args: [positionId],
    });
  }

  async function getUserPositionIds(user: `0x${string}`): Promise<readonly bigint[]> {
    return readContract({
      address: DIAMOND_ADDRESS,
      abi: PositionFacetAbi,
      functionName: "getUserPositionIds",
      args: [user],
    }) as Promise<readonly bigint[]>;
  }

  async function getOpenInterest(marketId: bigint) {
    return readContract({
      address: DIAMOND_ADDRESS,
      abi: PositionFacetAbi,
      functionName: "getOpenInterest",
      args: [marketId],
    });
  }

  async function openPosition(params: {
    marketId: bigint;
    collateralToken: `0x${string}`;
    collateralAmount: bigint;
    leverage: bigint;
    isLong: boolean;
  }) {
    await ensureDiamondAllowance(params.collateralToken, params.collateralAmount);
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: PositionFacetAbi,
      functionName: "openPosition",
      args: [params.marketId, params.collateralToken, params.collateralAmount, params.leverage, params.isLong],
    });
    return waitForTx(hash);
  }

  /** Resolve a position's collateral token (getPosition output index 5). */
  async function getPositionCollateralToken(positionId: bigint): Promise<`0x${string}`> {
    const pos = (await getPosition(positionId)) as readonly unknown[];
    return pos[5] as `0x${string}`;
  }

  async function closePosition(positionId: bigint) {
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: PositionFacetAbi,
      functionName: "closePosition",
      args: [positionId],
    });
    return waitForTx(hash);
  }

  async function partialClose(positionId: bigint, closeSizeUsd: bigint) {
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: PositionFacetAbi,
      functionName: "partialClose",
      args: [positionId, closeSizeUsd],
    });
    return waitForTx(hash);
  }

  async function addCollateral(positionId: bigint, amount: bigint) {
    await ensureDiamondAllowance(await getPositionCollateralToken(positionId), amount);
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: PositionFacetAbi,
      functionName: "addCollateral",
      args: [positionId, amount],
    });
    return waitForTx(hash);
  }

  async function removeCollateral(positionId: bigint, amount: bigint) {
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: PositionFacetAbi,
      functionName: "removeCollateral",
      args: [positionId, amount],
    });
    return waitForTx(hash);
  }

  async function addSize(positionId: bigint, additionalCollateral: bigint, leverage: bigint) {
    await ensureDiamondAllowance(await getPositionCollateralToken(positionId), additionalCollateral);
    const hash = await writeContract({
      address: DIAMOND_ADDRESS,
      abi: PositionFacetAbi,
      functionName: "addSize",
      args: [positionId, additionalCollateral, leverage],
    });
    return waitForTx(hash);
  }

  return {
    getPosition, getUserPositionIds, getOpenInterest,
    openPosition, closePosition, partialClose,
    addCollateral, removeCollateral, addSize,
  };
}
